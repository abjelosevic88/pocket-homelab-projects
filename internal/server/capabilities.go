package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-projects/internal/store"
)

// What Pocket Search, Pocket Day and Pocket Hub ask Projects (the capabilities
// value in links.go): one route each, in the shape every Pocket app answers
// in. They read what /search, /day and /dashboard read, as the request's
// person and with that person's teams; only the capture adds something, one
// issue, the way POST /issues does.
//
// All of it is this instance's own workspace. A connected instance is another
// server's data, reached with the token the person stored for it: nothing
// here asks one, and /r/{id}/api/v1/pocket/… answers 404 (remotes.go).

// issuePath opens an issue in the web app.
func issuePath(ident string) string { return "/issue/" + ident }

// dots joins the parts of a line that are there: "WEB-12 · In Progress · Relaunch".
func dots(parts ...string) string {
	out := parts[:0:0]
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, " · ")
}

// counted: "1 issue", "3 issues".
func counted(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}

// issueNames resolves what a line about an issue says besides its title:
// the status it is in and where it belongs (its project, else its team).
type issueNames struct {
	states   map[int64]string
	teams    map[int64]string
	projects map[int64]store.ProjectRef
}

func (s *Server) issueNames(ctx context.Context, uid int64, issues []*store.Issue) (*issueNames, error) {
	n := &issueNames{states: map[int64]string{}, teams: map[int64]string{}}
	teams, err := s.store.ListTeams(ctx, uid)
	if err != nil {
		return nil, err
	}
	for _, t := range teams {
		n.teams[t.ID] = t.Name
	}
	var pids []int64
	for _, is := range issues {
		if _, ok := n.states[is.StateID]; !ok {
			n.states[is.StateID] = ""
			if st, err := s.store.GetState(ctx, is.StateID); err == nil {
				n.states[is.StateID] = st.Name
			}
		}
		if is.ProjectID != nil {
			pids = append(pids, *is.ProjectID)
		}
	}
	if n.projects, err = s.store.ProjectRefs(ctx, pids); err != nil {
		return nil, err
	}
	return n, nil
}

func (n *issueNames) state(is *store.Issue) string { return n.states[is.StateID] }

func (n *issueNames) place(is *store.Issue) string {
	if is.ProjectID != nil && n.projects[*is.ProjectID].Name != "" {
		return n.projects[*is.ProjectID].Name
	}
	return n.teams[is.TeamID]
}

// ---------- search ----------

// pocketSearch answers Pocket Search with the issues GET /search finds: by
// identifier, title and description, in the person's teams, archived ones
// too, the latest change first.
func (s *Server) pocketSearch(r *http.Request, q string, limit int) ([]core.SearchItem, error) {
	ctx := r.Context()
	// the store looks for words (letters, digits, _); given none it would not
	// narrow the list at all and every issue would count as found
	if strings.IndexFunc(q, func(c rune) bool { return c == '_' || c > 127 || unicode.IsLetter(c) || unicode.IsDigit(c) }) < 0 {
		return nil, nil
	}
	a := s.accessOf(r)
	issues, _, err := s.store.ListIssues(ctx, store.IssueFilter{AllowedTeamIDs: a.ids(), ViewerID: a.uid, Q: q, Archived: "include", Sort: "updated", Limit: limit, Snoozed: "include"})
	if err != nil {
		return nil, err
	}
	names, err := s.issueNames(ctx, a.uid, issues)
	if err != nil {
		return nil, err
	}
	out := make([]core.SearchItem, 0, len(issues))
	for _, is := range issues {
		out = append(out, core.SearchItem{ID: is.Identifier, Title: is.Title, Kind: "issue", Date: is.UpdatedAt, URL: issuePath(is.Identifier),
			Snippet: dots(is.Identifier, names.state(is), names.place(is))})
	}
	return out, nil
}

// ---------- day ----------

// dayKind says in one word what the person did with an issue that day, the
// most telling first: completed, created, commented (nothing but comments),
// tracked (nothing but time), else updated (moved, edited, assigned, files,
// Pocket AI conversations, commits and pull requests that landed on it).
func dayKind(is *dayIssue) string {
	switch {
	case is.Done:
		return "completed"
	case is.Created:
		return "created"
	}
	only := func(kind string) bool {
		for _, e := range is.Events {
			if e.Kind != kind {
				return false
			}
		}
		return len(is.Events) > 0
	}
	switch {
	case only("comment"):
		return "commented"
	case only("time"):
		return "tracked"
	}
	return "updated"
}

// dayTime is when it happened: the moment the issue was created, moved to
// done or last commented on, else the last thing that happened to it.
func dayTime(is *dayIssue, kind string) string {
	want := map[string]string{"completed": "state", "created": "created", "commented": "comment"}[kind]
	at := is.Last
	for _, e := range is.Events { // in the order of the day
		if e.Kind == want && e.Time != "" {
			at = e.Time
			if kind == "created" {
				break
			}
		}
	}
	return at
}

// pocketDay answers Pocket Day with the person's work that day, as GET
// /day/{date} collects it on this instance: an item per issue they created,
// completed, changed, commented on or tracked time on (and the issues
// assigned to them a commit or pull request landed on), and one per project
// they posted an update to.
func (s *Server) pocketDay(r *http.Request, date string, tz *time.Location) (*core.Day, error) {
	ctx := r.Context()
	a := s.accessOf(r)
	loc := s.loc(ctx, a.uid)
	if tz != nil {
		loc = tz
	}
	start, err := time.ParseInLocation("2006-01-02", date, loc)
	if err != nil {
		return nil, nil
	}
	work, err := s.dayWork(ctx, r, a.uid, a.ids(), start, loc)
	if err != nil {
		return nil, err
	}
	if len(work.Issues)+len(work.Projects) == 0 {
		return nil, nil
	}
	day := &core.Day{Date: date}
	count := map[string]int{}
	for _, is := range work.Issues {
		kind := dayKind(is)
		count[kind]++
		day.Items = append(day.Items, core.DayItem{ID: "issue:" + is.Identifier, Time: dayTime(is, kind), Title: is.Identifier + " " + is.Title,
			Sub: firstNonEmpty(is.Project, is.Team), Kind: kind, Done: kind == "completed", Minutes: float64(is.Minutes), URL: issuePath(is.Identifier)})
	}
	updates := 0
	for _, p := range work.Projects {
		if len(p.Events) == 0 {
			continue
		}
		last := p.Events[len(p.Events)-1]
		updates += len(p.Events)
		day.Items = append(day.Items, core.DayItem{ID: "project:" + strconv.FormatInt(p.id, 10), Time: last.Time, Title: p.Name, Sub: last.Text, Kind: "update",
			URL: "/project/" + strconv.FormatInt(p.id, 10)})
	}

	// "2 issues completed, 1 created, 1 commented on, 1 h 20 min tracked"
	var parts []string
	for _, k := range []struct{ kind, word string }{{"completed", "completed"}, {"created", "created"}, {"updated", "updated"}, {"commented", "commented on"}} {
		n := count[k.kind]
		switch {
		case n == 0:
		case len(parts) == 0:
			parts = append(parts, counted(n, "issue", "issues")+" "+k.word)
		default:
			parts = append(parts, strconv.Itoa(n)+" "+k.word)
		}
	}
	if updates > 0 {
		parts = append(parts, counted(updates, "project update", "project updates"))
	}
	if work.Minutes > 0 {
		parts = append(parts, fmtMinutes(work.Minutes)+" tracked")
	}
	day.Summary = strings.Join(parts, ", ")
	for _, m := range []struct{ label, kind string }{{"Completed", "completed"}, {"Created", "created"}} {
		if n := count[m.kind]; n > 0 {
			day.Metrics = append(day.Metrics, core.Metric{Label: m.label, Value: strconv.Itoa(n)})
		}
	}
	if work.Minutes > 0 {
		day.Metrics = append(day.Metrics, core.Metric{Label: "Time tracked", Value: fmtMinutes(work.Minutes)})
	}
	return day, nil
}

// ---------- widget ----------

// dueWords says when an issue is due, next to its status: "due today",
// "due tomorrow", "due 3 Dec", "was due 20 Nov".
func dueWords(due, today string) string {
	d, err := time.Parse("2006-01-02", due)
	if err != nil {
		return ""
	}
	t, _ := time.Parse("2006-01-02", today)
	on := d.Format("2 Jan")
	if d.Year() != t.Year() {
		on = d.Format("2 Jan 2006")
	}
	switch {
	case d.Before(t):
		return "was due " + on
	case d.Equal(t):
		return "due today"
	case d.Equal(t.AddDate(0, 0, 1)):
		return "due tomorrow"
	}
	return "due " + on
}

// byPriorityAndDue orders issues the way the widget lists them: the highest
// priority first (none last), then the nearest due date (none last).
func byPriorityAndDue(list []*store.Issue) {
	rank := func(is *store.Issue) int {
		if is.Priority == 0 {
			return 9
		}
		return is.Priority
	}
	sort.SliceStable(list, func(i, j int) bool {
		a, b := list[i], list[j]
		if rank(a) != rank(b) {
			return rank(a) < rank(b)
		}
		if (a.DueDate == "") != (b.DueDate == "") {
			return a.DueDate != ""
		}
		return a.DueDate < b.DueDate
	})
}

// pocketWidget answers Pocket Hub with the person's open work, the numbers
// the dashboard and My issues show: what is assigned to them and not done,
// what is in progress first, then the rest, each by priority and due date.
// Someone on no team gets nothing to show.
func (s *Server) pocketWidget(r *http.Request) (*core.WidgetData, error) {
	ctx := r.Context()
	a := s.accessOf(r)
	if len(a.teams) == 0 {
		return nil, nil
	}
	today := s.today(ctx, a.uid)
	base := myOpenIssues(a, today)
	started := withSort(base, "priority", 100)
	started.StateTypes = []string{"started"}
	doing, inProgress, err := s.store.ListIssues(ctx, started)
	if err != nil {
		return nil, err
	}
	waiting := withSort(base, "priority", 100)
	waiting.StateTypes = []string{"triage", "backlog", "unstarted"}
	next, notStarted, err := s.store.ListIssues(ctx, waiting)
	if err != nil {
		return nil, err
	}
	count := func(due string) (int, error) {
		f := withSort(base, "due", 1)
		f.Due = due
		_, n, err := s.store.ListIssues(ctx, f)
		return n, err
	}
	overdue, err := count("overdue")
	if err != nil {
		return nil, err
	}
	week, err := count("week") // due within seven days, the overdue ones too
	if err != nil {
		return nil, err
	}
	open := inProgress + notStarted

	w := &core.WidgetData{URL: "/my-issues", Metrics: []core.Metric{
		// the dashboard's numbers under the names My issues shows them with
		{Label: "Assigned, open", Value: strconv.Itoa(open)},
		{Label: "In progress", Value: strconv.Itoa(inProgress)},
		{Label: "Overdue", Value: strconv.Itoa(overdue)},
		{Label: "Due this week", Value: strconv.Itoa(week)},
	}}
	var parts []string
	if inProgress > 0 {
		parts = append(parts, strconv.Itoa(inProgress)+" in progress")
	}
	switch {
	case week > 0 && overdue >= week:
		parts = append(parts, strconv.Itoa(overdue)+" overdue")
	case week > 0 && overdue > 0:
		parts = append(parts, fmt.Sprintf("%d due this week (%d overdue)", week, overdue))
	case week > 0:
		parts = append(parts, strconv.Itoa(week)+" due this week")
	}
	switch {
	case len(parts) > 0:
		w.Line = strings.Join(parts, ", ")
	case open > 0:
		w.Line = counted(open, "open issue", "open issues")
	default:
		w.Line = "Nothing assigned to you"
	}

	const rows = 8
	byPriorityAndDue(doing)
	byPriorityAndDue(next)
	list := append(doing, next...)
	if len(list) > rows {
		list = list[:rows]
	}
	names, err := s.issueNames(ctx, a.uid, list)
	if err != nil {
		return nil, err
	}
	for _, is := range list {
		w.Items = append(w.Items, core.WidgetItem{ID: is.Identifier, Title: is.Identifier + " " + is.Title, Detail: dots(names.state(is), dueWords(is.DueDate, today)),
			URL: issuePath(is.Identifier)})
	}
	return w, nil
}

// ---------- capture ----------

// captureKeyRe is a quick line that starts with a team's key: "WEB: Fix login".
var captureKeyRe = regexp.MustCompile(`^([A-Za-z][A-Za-z0-9]{0,6})\s*:\s*(.*)$`)

// captureLines splits a quick line into an issue's title and description:
// the title when one was given (the text is then the description), else the
// text's first line.
func captureLines(in core.CaptureInput) (title, desc string) {
	title, desc = in.Title, in.Text
	if title == "" {
		title, desc, _ = strings.Cut(in.Text, "\n")
	}
	return strings.Join(strings.Fields(title), " "), strings.TrimSpace(desc)
}

// captureTeam finds the team a quick line goes to and what is left as the
// title. The line may start with the key of a team the person can add issues
// to ("WEB: Fix login", capitals do not matter); the key is then dropped. A
// first word that is not such a key stays part of the title ("Note: …"), so
// a team the person is not on is never told apart from no team at all.
// Without a key the issue goes where the new-issue dialog starts: the
// person's default team (Settings → Preferences), else their only team.
func (s *Server) captureTeam(ctx context.Context, a *access, title string) (*store.Team, string, error) {
	teams, err := s.store.ListTeams(ctx, a.uid)
	if err != nil {
		return nil, "", err
	}
	var writable []*store.Team
	var keys []string
	for i := range teams {
		if t := &teams[i]; t.ArchivedAt == nil && a.can(t.ID, needWrite) {
			writable = append(writable, t)
			keys = append(keys, t.Key)
		}
	}
	if len(writable) == 0 {
		if len(teams) > 0 {
			return nil, "", fmt.Errorf("%w: you can read your teams here but not add issues to them", core.ErrCapture)
		}
		return nil, "", fmt.Errorf("%w: create or join a team in Pocket Projects first", core.ErrCapture)
	}
	if m := captureKeyRe.FindStringSubmatch(title); m != nil && !strings.HasPrefix(m[2], "//") {
		for _, t := range writable {
			switch {
			case !strings.EqualFold(t.Key, m[1]):
			case m[2] == "":
				return nil, "", fmt.Errorf("%w: say what the issue is after \"%s:\"", core.ErrCapture, t.Key)
			default:
				return t, m[2], nil
			}
		}
	}
	if st, _ := s.store.GetSettings(ctx, a.uid); st.DefaultTeamID > 0 {
		for _, t := range writable {
			if t.ID == st.DefaultTeamID {
				return t, title, nil
			}
		}
	}
	if len(writable) == 1 {
		return writable[0], title, nil
	}
	sort.Strings(keys)
	if len(keys) > 6 {
		keys = append(keys[:6], "…")
	}
	return nil, "", fmt.Errorf("%w: start with the team's key, e.g. \"%s: Fix login\" (your teams: %s), or choose a default team in Settings → Preferences",
		core.ErrCapture, keys[0], strings.Join(keys, ", "))
}

// captured is the answer for a kept line: the issue with its identifier in
// front, so the person sees which team it went to.
func captured(is *store.Issue) *core.CaptureResult {
	return &core.CaptureResult{ID: is.Identifier, Title: is.Identifier + " " + is.Title, URL: issuePath(is.Identifier)}
}

// pocketCapture keeps a quick line as a new issue, made the way POST /issues
// makes one: the team's status for new issues and its default assignee, the
// history entry, the Core event, the inbox and the team's webhooks, with the
// acting app as its source when it came through a link. The same request_id
// again answers the issue made the first time and adds nothing: the receipt
// is written in the transaction that adds the issue, as for Idempotency-Key.
func (s *Server) pocketCapture(r *http.Request, in core.CaptureInput) (*core.CaptureResult, error) {
	ctx := r.Context()
	a := s.accessOf(r)
	op, err := core.NewOperation(fmt.Sprintf("%d:pocket:capture", a.uid), in.RequestID, in)
	if err != nil {
		return nil, err
	}
	kept := func() (*core.CaptureResult, error) {
		var receipt struct {
			ID int64 `json:"id"`
		}
		found, err := op.Replay(ctx, s.store.DB, &receipt)
		switch {
		case errors.Is(err, core.ErrOperationConflict):
			return nil, fmt.Errorf("%w: this was already kept with other words", core.ErrCapture)
		case err != nil || !found:
			return nil, err
		}
		// what the person may see now, not what they could when it was kept
		is, err := s.store.GetIssue(ctx, receipt.ID, a.uid)
		if err != nil || a.teams[is.TeamID] == "" {
			return nil, fmt.Errorf("%w: this was kept before, as an issue you can no longer open", core.ErrCapture)
		}
		return captured(is), nil
	}
	if res, err := kept(); res != nil || err != nil {
		return res, err
	}

	title, desc := captureLines(in)
	team, title, err := s.captureTeam(ctx, a, title)
	if err != nil {
		return nil, err
	}
	if n := []rune(title); len(n) > 200 {
		// a long paragraph: its start is the title, all of it the description
		// (a title is kept up to 500 bytes, whatever the letters)
		cut := n[:199]
		for len(string(cut)) > 490 {
			cut = cut[:len(cut)-1]
		}
		title, desc = strings.TrimSpace(string(cut))+"…", strings.TrimSpace(title+"\n\n"+desc)
	}
	is, err := s.createIssue(ctx, a, s.actorOf(r), issueInput{TeamID: &team.ID, Title: &title, Description: &desc}, opHook(ctx, op))
	if err != nil {
		// the same line sent twice at once: the other one kept it
		if res, rerr := kept(); res != nil || rerr != nil {
			return res, rerr
		}
		var ue userError
		var fb forbidden
		switch {
		case errors.As(err, &ue):
			return nil, fmt.Errorf("%w: %s", core.ErrCapture, ue.msg)
		case errors.As(err, &fb):
			return nil, fmt.Errorf("%w: %s", core.ErrCapture, fb.msg)
		}
		return nil, err
	}
	return captured(is), nil
}
