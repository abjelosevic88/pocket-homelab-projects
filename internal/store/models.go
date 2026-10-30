package store

import "encoding/json"

// User is an account on this box.
type User struct {
	ID           int64  `json:"id"`
	Email        string `json:"email"`
	Name         string `json:"name"`
	Username     string `json:"username"`
	PasswordHash string `json:"-"`
	Role         string `json:"role"` // admin | member (the box, not a team)
	FeedToken    string `json:"-"`
	Color        string `json:"color"`
	CreatedAt    string `json:"created_at"`
}

// Person is the public face of a user: what teammates see.
type Person struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Email    string `json:"email"`
	Username string `json:"username"`
	Color    string `json:"color"`
}

// APIToken is a personal API token (the plaintext is shown once).
type APIToken struct {
	ID         int64   `json:"id"`
	UserID     int64   `json:"user_id"`
	Name       string  `json:"name"`
	Prefix     string  `json:"prefix"`
	LastUsedAt *string `json:"last_used_at"`
	CreatedAt  string  `json:"created_at"`
}

// Team owns issues, a workflow, labels, cycles and templates.
type Team struct {
	ID                 int64   `json:"id"`
	Key                string  `json:"key"`
	Name               string  `json:"name"`
	Description        string  `json:"description"`
	Color              string  `json:"color"`
	Icon               string  `json:"icon"`
	IssueSeq           int64   `json:"issue_seq"`
	TriageEnabled      bool    `json:"triage_enabled"`
	CyclesEnabled      bool    `json:"cycles_enabled"`
	CycleWeeks         int     `json:"cycle_weeks"`
	CycleStartDay      int     `json:"cycle_start_day"`
	CycleCooldownWeeks int     `json:"cycle_cooldown_weeks"`
	CycleUpcoming      int     `json:"cycle_upcoming"`
	CycleRollover      bool    `json:"cycle_rollover"`
	EstimateScale      string  `json:"estimate_scale"`
	AutoArchiveMonths  int     `json:"auto_archive_months"`
	DefaultStateID     *int64  `json:"default_state_id"`
	DefaultAssignee    string  `json:"default_assignee"` // creator | none | a member's user id
	GitSecret          string  `json:"-"`
	CreatedBy          *int64  `json:"created_by"`
	CreatedAt          string  `json:"created_at"`
	UpdatedAt          string  `json:"updated_at"`
	ArchivedAt         *string `json:"archived_at"`

	// Filled for the person asking.
	Role    string         `json:"role,omitempty"`
	Members []TeamMember   `json:"members,omitempty"`
	States  []State        `json:"states,omitempty"`
	Labels  []Label        `json:"labels,omitempty"`
	Counts  map[string]int `json:"counts,omitempty"`
	// The team's folder in Pocket Notes ("" = none).
	NotesFolder string `json:"notes_folder"`
}

// TeamMember is a person on a team with their role there.
type TeamMember struct {
	Person
	Role     string `json:"role"`
	JoinedAt string `json:"joined_at"`
}

// State is a column of a team's workflow.
type State struct {
	ID          int64   `json:"id"`
	TeamID      int64   `json:"team_id"`
	Name        string  `json:"name"`
	Type        string  `json:"type"`
	Color       string  `json:"color"`
	Description string  `json:"description"`
	Position    float64 `json:"position"`
}

// StateTypes in workflow order.
var StateTypes = []string{"triage", "backlog", "unstarted", "started", "completed", "canceled"}

// Label tags issues; a group label holds labels of which an issue may carry one.
type Label struct {
	ID          int64   `json:"id"`
	TeamID      int64   `json:"team_id"`
	ParentID    *int64  `json:"parent_id"`
	Name        string  `json:"name"`
	Color       string  `json:"color"`
	Description string  `json:"description"`
	IsGroup     bool    `json:"is_group"`
	Position    float64 `json:"position"`
	Issues      int     `json:"issues"`
}

// Project spans teams, with milestones and status updates.
type Project struct {
	ID                int64   `json:"id"`
	Name              string  `json:"name"`
	Summary           string  `json:"summary"`
	Content           string  `json:"content"`
	Icon              string  `json:"icon"`
	Color             string  `json:"color"`
	Status            string  `json:"status"`
	Health            string  `json:"health"`
	Priority          int     `json:"priority"`
	LeadID            *int64  `json:"lead_id"`
	StartDate         string  `json:"start_date"`
	TargetDate        string  `json:"target_date"`
	UpdateCadenceDays int     `json:"update_cadence_days"`
	SortOrder         float64 `json:"sort_order"`
	SourceApp         string  `json:"source_app"`
	CreatedBy         *int64  `json:"created_by"`
	CreatedAt         string  `json:"created_at"`
	UpdatedAt         string  `json:"updated_at"`
	StartedAt         *string `json:"started_at"`
	CompletedAt       *string `json:"completed_at"`
	CanceledAt        *string `json:"canceled_at"`
	DeletedAt         *string `json:"deleted_at,omitempty"`

	TeamIDs    []int64         `json:"team_ids"`
	MemberIDs  []int64         `json:"member_ids"`
	Progress   ProjectProgress `json:"progress"`
	LastUpdate *ProjectUpdate  `json:"last_update"`
	Milestones []Milestone     `json:"milestones,omitempty"`
}

// ProjectProgress counts a project's issues by state type.
type ProjectProgress struct {
	Total           int     `json:"total"`
	Started         int     `json:"started"`
	Completed       int     `json:"completed"`
	Canceled        int     `json:"canceled"`
	Points          float64 `json:"points"`
	CompletedPoints float64 `json:"completed_points"`
	Percent         float64 `json:"percent"`
}

// Milestone is a dated stage of a project.
type Milestone struct {
	ID          int64           `json:"id"`
	ProjectID   int64           `json:"project_id"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	TargetDate  string          `json:"target_date"`
	SortOrder   float64         `json:"sort_order"`
	CreatedAt   string          `json:"created_at"`
	Progress    ProjectProgress `json:"progress"`
}

// ProjectUpdate is a status post on a project.
type ProjectUpdate struct {
	ID        int64   `json:"id"`
	ProjectID int64   `json:"project_id"`
	UserID    *int64  `json:"user_id"`
	Health    string  `json:"health"`
	Body      string  `json:"body"`
	SourceApp string  `json:"source_app"`
	CreatedAt string  `json:"created_at"`
	EditedAt  *string `json:"edited_at"`
	Comments  int     `json:"comments"`
}

// Doc is a Markdown page of a project.
type Doc struct {
	ID        int64  `json:"id"`
	ProjectID int64  `json:"project_id"`
	Title     string `json:"title"`
	Body      string `json:"body,omitempty"`
	Icon      string `json:"icon"`
	CreatedBy *int64 `json:"created_by"`
	UpdatedBy *int64 `json:"updated_by"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// ProjectLink is a resource of a project.
type ProjectLink struct {
	ID        int64   `json:"id"`
	ProjectID int64   `json:"project_id"`
	Title     string  `json:"title"`
	URL       string  `json:"url"`
	App       string  `json:"app"`
	SortOrder float64 `json:"sort_order"`
	CreatedBy *int64  `json:"created_by"`
	CreatedAt string  `json:"created_at"`
}

// ContactLink is a person from Pocket Contacts on a project or an issue.
type ContactLink struct {
	ID        int64  `json:"id"`
	ProjectID *int64 `json:"project_id"`
	IssueID   *int64 `json:"issue_id"`
	ContactID string `json:"contact_id"`
	Name      string `json:"name"`
	Email     string `json:"email"`
	Org       string `json:"org"`
	Role      string `json:"role"`
	URL       string `json:"url"`
	CreatedAt string `json:"created_at"`
}

// Cycle is a team's time box.
type Cycle struct {
	ID          int64   `json:"id"`
	TeamID      int64   `json:"team_id"`
	Number      int     `json:"number"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	StartsOn    string  `json:"starts_on"`
	EndsOn      string  `json:"ends_on"`
	CompletedAt *string `json:"completed_at"`
	CreatedAt   string  `json:"created_at"`

	Status   string          `json:"status"` // past | current | upcoming
	Progress ProjectProgress `json:"progress"`
}

// CycleSnapshot is one day of a cycle's burn-up.
type CycleSnapshot struct {
	Day             string  `json:"day"`
	Scope           int     `json:"scope"`
	Started         int     `json:"started"`
	Completed       int     `json:"completed"`
	ScopePoints     float64 `json:"scope_points"`
	StartedPoints   float64 `json:"started_points"`
	CompletedPoints float64 `json:"completed_points"`
}

// Issue is a unit of work.
type Issue struct {
	ID           int64    `json:"id"`
	TeamID       int64    `json:"team_id"`
	Number       int64    `json:"number"`
	Identifier   string   `json:"identifier"`
	Title        string   `json:"title"`
	Description  string   `json:"description,omitempty"`
	StateID      int64    `json:"state_id"`
	Priority     int      `json:"priority"`
	Estimate     *float64 `json:"estimate"`
	AssigneeID   *int64   `json:"assignee_id"`
	CreatorID    *int64   `json:"creator_id"`
	ProjectID    *int64   `json:"project_id"`
	MilestoneID  *int64   `json:"milestone_id"`
	CycleID      *int64   `json:"cycle_id"`
	ParentID     *int64   `json:"parent_id"`
	DueDate      string   `json:"due_date"`
	SortOrder    float64  `json:"sort_order"`
	SnoozedUntil string   `json:"snoozed_until"`
	SourceApp    string   `json:"source_app"`
	SourceRef    string   `json:"source_ref"`
	SourceURL    string   `json:"source_url"`
	CreatedAt    string   `json:"created_at"`
	UpdatedAt    string   `json:"updated_at"`
	StartedAt    *string  `json:"started_at"`
	CompletedAt  *string  `json:"completed_at"`
	CanceledAt   *string  `json:"canceled_at"`
	ArchivedAt   *string  `json:"archived_at"`
	DeletedAt    *string  `json:"deleted_at,omitempty"`

	StateType   string  `json:"state_type"`
	LabelIDs    []int64 `json:"label_ids"`
	SubTotal    int     `json:"sub_total"`
	SubDone     int     `json:"sub_done"`
	Comments    int     `json:"comments"`
	Attachments int     `json:"attachments"`
	BlockedBy   int     `json:"blocked_by"`
	Blocking    int     `json:"blocking"`
	Subscribed  bool    `json:"subscribed"`
}

// Relation links two issues.
type Relation struct {
	ID        int64  `json:"id"`
	IssueID   int64  `json:"issue_id"`
	RelatedID int64  `json:"related_id"`
	Type      string `json:"type"`
	CreatedAt string `json:"created_at"`
}

// Comment is a message on an issue (or on a project update).
type Comment struct {
	ID              int64      `json:"id"`
	IssueID         *int64     `json:"issue_id"`
	ProjectUpdateID *int64     `json:"project_update_id"`
	ParentID        *int64     `json:"parent_id"`
	UserID          *int64     `json:"user_id"`
	Body            string     `json:"body"`
	SourceApp       string     `json:"source_app"`
	CreatedAt       string     `json:"created_at"`
	EditedAt        *string    `json:"edited_at"`
	ResolvedAt      *string    `json:"resolved_at"`
	Reactions       []Reaction `json:"reactions"`
}

// Reaction is one emoji with the people who chose it.
type Reaction struct {
	Emoji   string  `json:"emoji"`
	UserIDs []int64 `json:"user_ids"`
}

// Attachment is a file, link or item in another app on an issue or project.
type Attachment struct {
	ID        int64           `json:"id"`
	IssueID   *int64          `json:"issue_id"`
	ProjectID *int64          `json:"project_id"`
	Kind      string          `json:"kind"`
	Title     string          `json:"title"`
	Subtitle  string          `json:"subtitle"`
	URL       string          `json:"url"`
	FileName  string          `json:"file_name"`
	Mime      string          `json:"mime"`
	Size      int64           `json:"size"`
	ExtApp    string          `json:"ext_app"`
	ExtID     string          `json:"ext_id"`
	State     string          `json:"state"`
	Meta      json.RawMessage `json:"meta"`
	CreatedBy *int64          `json:"created_by"`
	CreatedAt string          `json:"created_at"`
	UpdatedAt string          `json:"updated_at"`
}

// History is one change of an issue or a project.
type History struct {
	ID        int64  `json:"id"`
	IssueID   *int64 `json:"issue_id"`
	ProjectID *int64 `json:"project_id"`
	ActorID   *int64 `json:"actor_id"`
	ActorApp  string `json:"actor_app"`
	Field     string `json:"field"`
	FromValue string `json:"from_value"`
	ToValue   string `json:"to_value"`
	FromLabel string `json:"from_label"`
	ToLabel   string `json:"to_label"`
	CreatedAt string `json:"created_at"`
}

// TimeEntry is time spent on an issue (a running timer has StartedAt).
type TimeEntry struct {
	ID        int64  `json:"id"`
	IssueID   int64  `json:"issue_id"`
	UserID    int64  `json:"user_id"`
	SpentOn   string `json:"spent_on"`
	Minutes   int    `json:"minutes"`
	Note      string `json:"note"`
	StartedAt string `json:"started_at"`
	CreatedAt string `json:"created_at"`
	// Source is the Pocket app that logged it ("" = here), ExternalID its id
	// there; BeganAt when the time began (kept after a timer stops).
	Source     string `json:"source"`
	ExternalID string `json:"external_id"`
	BeganAt    string `json:"began_at"`
}

// Notification is an entry of a person's inbox.
type Notification struct {
	ID           int64   `json:"id"`
	UserID       int64   `json:"-"`
	Kind         string  `json:"kind"`
	IssueID      *int64  `json:"issue_id"`
	ProjectID    *int64  `json:"project_id"`
	TeamID       *int64  `json:"team_id"`
	CommentID    *int64  `json:"comment_id"`
	ActorID      *int64  `json:"actor_id"`
	ActorApp     string  `json:"actor_app"`
	Title        string  `json:"title"`
	Body         string  `json:"body"`
	ReadAt       *string `json:"read_at"`
	SnoozedUntil string  `json:"snoozed_until"`
	CreatedAt    string  `json:"created_at"`

	Issue   *IssueRef   `json:"issue,omitempty"`
	Project *ProjectRef `json:"project,omitempty"`
}

// IssueRef is the little an inbox row or a link needs of an issue.
type IssueRef struct {
	ID         int64  `json:"id"`
	Identifier string `json:"identifier"`
	Title      string `json:"title"`
	TeamID     int64  `json:"team_id"`
	StateID    int64  `json:"state_id"`
	StateType  string `json:"state_type"`
	Priority   int    `json:"priority"`
}

// ProjectRef is the little a link needs of a project.
type ProjectRef struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Icon   string `json:"icon"`
	Color  string `json:"color"`
	Status string `json:"status"`
}

// View is a saved filter.
type View struct {
	ID          int64           `json:"id"`
	UserID      int64           `json:"user_id"`
	TeamID      *int64          `json:"team_id"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Icon        string          `json:"icon"`
	Color       string          `json:"color"`
	Kind        string          `json:"kind"`
	Filters     json.RawMessage `json:"filters"`
	Display     json.RawMessage `json:"display"`
	Shared      bool            `json:"shared"`
	CreatedAt   string          `json:"created_at"`
	UpdatedAt   string          `json:"updated_at"`
}

// Favorite pins something to a person's sidebar.
type Favorite struct {
	ID        int64   `json:"id"`
	Kind      string  `json:"kind"`
	RefID     int64   `json:"ref_id"`
	SortOrder float64 `json:"sort_order"`
}

// Template prefills a new issue; with Recur it makes one on a schedule.
type Template struct {
	ID            int64           `json:"id"`
	TeamID        int64           `json:"team_id"`
	Name          string          `json:"name"`
	Title         string          `json:"title"`
	Description   string          `json:"description"`
	Data          json.RawMessage `json:"data"`
	Recur         string          `json:"recur"`
	RecurInterval int             `json:"recur_interval"`
	NextAt        string          `json:"next_at"`
	LastIssueID   *int64          `json:"last_issue_id"`
	CreatedBy     *int64          `json:"created_by"`
	CreatedAt     string          `json:"created_at"`
	UpdatedAt     string          `json:"updated_at"`
}

// TemplateData is what a template sets on the issue.
type TemplateData struct {
	StateID    *int64   `json:"state_id,omitempty"`
	Priority   *int     `json:"priority,omitempty"`
	Estimate   *float64 `json:"estimate,omitempty"`
	AssigneeID *int64   `json:"assignee_id,omitempty"`
	ProjectID  *int64   `json:"project_id,omitempty"`
	LabelIDs   []int64  `json:"label_ids,omitempty"`
	DueInDays  *int     `json:"due_in_days,omitempty"`
	SubIssues  []string `json:"sub_issues,omitempty"`
}

// Webhook sends a team's changes somewhere as JSON.
type Webhook struct {
	ID         int64  `json:"id"`
	TeamID     int64  `json:"team_id"`
	Name       string `json:"name"`
	URL        string `json:"url"`
	Secret     string `json:"-"`
	HasSecret  bool   `json:"has_secret"`
	Events     string `json:"events"`
	Active     bool   `json:"active"`
	LastStatus int    `json:"last_status"`
	LastError  string `json:"last_error"`
	LastAt     string `json:"last_at"`
	CreatedAt  string `json:"created_at"`
}
