package server

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/abjelosevic88/pocket-homelab-projects/internal/store"
)

// Issue attachments can live in Pocket Files instead of Pocket Projects'
// own disk — a team setting. The team admin who switches it on lends their
// Pocket Core link to Pocket Files (files:read/write); every read and write
// of the team's attachments goes through it, so team members need nothing
// in Pocket Files themselves. Files go to <folder>/<ISSUE-ID>/<name>
// (usually a shared mount, e.g. /Mounts/Pocket Projects/AS); removing an
// attachment moves the file to <folder>/Deleted/ (deletes in mounts are
// permanent in Pocket Files, so Projects keeps them recoverable itself).
// Pocket Files is the only copy: when it can't be reached, the attachment
// says so instead of showing a broken file.

var filesApp = PocketApp{ID: "files", Name: "Pocket Files", Slug: "files", Scopes: []string{"files:read", "files:write"},
	Reason:      "Store a team's issue attachments in a shared Pocket Files folder (a team setting)",
	Description: "When a team admin switches it on in Team settings → Attachments, the team's uploads are kept in Pocket Files (one place for all files) through the admin's link."}

func init() { pocketApps = append(pocketApps, filesApp) }

const filesMode = "pocket_files"

// fileMeta is where an attachment's file is when it is in Pocket Files.
type fileMeta struct {
	FilesPath  string `json:"files_path,omitempty"`
	FilesOwner int64  `json:"files_owner,omitempty"`
}

func metaOf(a *store.Attachment) fileMeta {
	var m fileMeta
	_ = json.Unmarshal(a.Meta, &m)
	return m
}

// ---------- WebDAV client ----------

type davClient struct {
	base  string // https://drive…/dav
	token string
}

var davHTTP = &http.Client{Timeout: 10 * time.Minute}

// escape builds the URL of a WebDAV path, segment by segment.
func (c *davClient) url(p string) string {
	parts := strings.Split(strings.Trim(p, "/"), "/")
	for i, s := range parts {
		parts[i] = url.PathEscape(s)
	}
	return c.base + "/" + strings.Join(parts, "/")
}

func (c *davClient) do(ctx context.Context, method, p string, body io.Reader, hdr map[string]string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.url(p), body)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth("projects", c.token)
	req.Header.Set("User-Agent", "PocketProjects/"+Version)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	res, err := davHTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Pocket Files is unreachable: %w", err)
	}
	return res, nil
}

func davFail(op, p string, res *http.Response) error {
	b, _ := io.ReadAll(io.LimitReader(res.Body, 300))
	res.Body.Close()
	msg := strings.TrimSpace(string(b))
	switch res.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Errorf("Pocket Files refused to %s %s (%d %s): the link may not allow it or the folder is read-only", op, p, res.StatusCode, msg)
	case http.StatusNotFound:
		return fmt.Errorf("%s is not in Pocket Files", p)
	}
	return fmt.Errorf("Pocket Files could not %s %s: %d %s", op, p, res.StatusCode, msg)
}

// stat says whether p exists and how big it is.
func (c *davClient) stat(ctx context.Context, p string) (exists, dir bool, size int64, err error) {
	res, err := c.do(ctx, "PROPFIND", p, strings.NewReader(`<?xml version="1.0"?><d:propfind xmlns:d="DAV:"><d:prop><d:getcontentlength/><d:resourcetype/></d:prop></d:propfind>`),
		map[string]string{"Depth": "0", "Content-Type": "application/xml"})
	if err != nil {
		return false, false, 0, err
	}
	if res.StatusCode == http.StatusNotFound {
		res.Body.Close()
		return false, false, 0, nil
	}
	if res.StatusCode != http.StatusMultiStatus {
		return false, false, 0, davFail("look at", p, res)
	}
	defer res.Body.Close()
	var ms struct {
		Responses []struct {
			Props []struct {
				Length string `xml:"prop>getcontentlength"`
				Type   struct {
					Collection *struct{} `xml:"collection"`
				} `xml:"prop>resourcetype"`
			} `xml:"propstat"`
		} `xml:"response"`
	}
	if err := xml.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&ms); err != nil {
		return true, false, 0, nil
	}
	for _, r := range ms.Responses {
		for _, p := range r.Props {
			if p.Type.Collection != nil {
				dir = true
			}
			if p.Length != "" {
				size, _ = strconv.ParseInt(p.Length, 10, 64)
			}
		}
	}
	return true, dir, size, nil
}

// mkdirAll makes p and its parents; folders that exist (or are Pocket
// Files' own, like /Mounts) are fine.
func (c *davClient) mkdirAll(ctx context.Context, p string) error {
	parts := strings.Split(strings.Trim(p, "/"), "/")
	cur := ""
	for _, s := range parts {
		cur += "/" + s
		if ok, dir, _, err := c.stat(ctx, cur); err != nil {
			return err
		} else if ok && dir {
			continue
		}
		res, err := c.do(ctx, "MKCOL", cur, nil, nil)
		if err != nil {
			return err
		}
		if res.StatusCode != http.StatusCreated && res.StatusCode != http.StatusMethodNotAllowed {
			return davFail("make the folder", cur, res)
		}
		res.Body.Close()
	}
	return nil
}

// put writes a file (r is read to the end).
func (c *davClient) put(ctx context.Context, p string, r io.Reader) error {
	res, err := c.do(ctx, "PUT", p, r, map[string]string{"Content-Type": "application/octet-stream"})
	if err != nil {
		return err
	}
	if res.StatusCode != http.StatusCreated && res.StatusCode != http.StatusNoContent && res.StatusCode != http.StatusOK {
		return davFail("save", p, res)
	}
	res.Body.Close()
	return nil
}

func (c *davClient) get(ctx context.Context, p string) (*http.Response, error) {
	res, err := c.do(ctx, "GET", p, nil, nil)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		return nil, davFail("read", p, res)
	}
	return res, nil
}

func (c *davClient) move(ctx context.Context, from, to string) error {
	res, err := c.do(ctx, "MOVE", from, nil, map[string]string{"Destination": c.url(to), "Overwrite": "F"})
	if err != nil {
		return err
	}
	if res.StatusCode != http.StatusCreated && res.StatusCode != http.StatusNoContent {
		return davFail("move", from, res)
	}
	res.Body.Close()
	return nil
}

func (c *davClient) remove(ctx context.Context, p string) {
	if res, err := c.do(ctx, "DELETE", p, nil, nil); err == nil {
		res.Body.Close()
	}
}

// freeName is name in dir, or "name (2).ext", "name (3).ext"… when taken.
func (c *davClient) freeName(ctx context.Context, dir, name string) (string, error) {
	ext := path.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for i := 1; i < 200; i++ {
		cand := name
		if i > 1 {
			cand = fmt.Sprintf("%s (%d)%s", stem, i, ext)
		}
		ok, _, _, err := c.stat(ctx, dir+"/"+cand)
		if err != nil {
			return "", err
		}
		if !ok {
			return dir + "/" + cand, nil
		}
	}
	return "", errors.New("too many files with that name")
}

// filesClient reaches Pocket Files as a person (the team's storage owner).
func (s *Server) filesClient(ctx context.Context, ownerID int64) (*davClient, error) {
	u, err := s.store.GetUser(ctx, ownerID)
	if err != nil {
		return nil, errors.New("the person whose Pocket Files link stores these attachments is gone; a team admin has to switch storage on again")
	}
	sb, err := s.siblingFor(ctx, u, filesApp)
	if err != nil {
		return nil, fmt.Errorf("Pocket Files (through %s): %w", firstNonEmpty(u.Name, u.Email), err)
	}
	return &davClient{base: sb.base + "/dav", token: sb.token}, nil
}

// cleanName is a file name safe as one path segment.
func cleanName(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.Map(func(r rune) rune {
		if r < 32 || r == '/' {
			return '-'
		}
		return r
	}, name)
	if name == "." || name == ".." || name == "" {
		return "file"
	}
	return name
}

// ---------- attachments ----------

// countingReader counts what passes through.
type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

// saveToFiles writes an issue's upload into the team's Pocket Files folder
// and returns its size and the meta to store.
func (s *Server) saveToFiles(ctx context.Context, tf *store.TeamFiles, is *store.Issue, name string, r io.Reader) (int64, []byte, error) {
	if tf.OwnerID == nil {
		return 0, nil, errors.New("this team's attachments go to Pocket Files, but nobody's link is set; a team admin has to switch it on again")
	}
	c, err := s.filesClient(ctx, *tf.OwnerID)
	if err != nil {
		return 0, nil, err
	}
	dir := strings.TrimRight(tf.Folder, "/") + "/" + is.Identifier
	if err := c.mkdirAll(ctx, dir); err != nil {
		return 0, nil, err
	}
	p, err := c.freeName(ctx, dir, cleanName(name))
	if err != nil {
		return 0, nil, err
	}
	cr := &countingReader{r: io.LimitReader(r, maxUpload+1)}
	if err := c.put(ctx, p, cr); err != nil {
		return 0, nil, err
	}
	if cr.n > maxUpload {
		c.remove(ctx, p)
		return 0, nil, errTooLarge
	}
	meta, _ := json.Marshal(fileMeta{FilesPath: p, FilesOwner: *tf.OwnerID})
	return cr.n, meta, nil
}

var errTooLarge = errors.New("the file is larger than 100 MB")

// openAttachment opens an attachment's file wherever it is.
func (s *Server) openAttachment(ctx context.Context, a *store.Attachment) (io.ReadCloser, int64, time.Time, error) {
	if m := metaOf(a); m.FilesPath != "" {
		c, err := s.filesClient(ctx, m.FilesOwner)
		if err != nil {
			return nil, 0, time.Time{}, err
		}
		res, err := c.get(ctx, m.FilesPath)
		if err != nil {
			return nil, 0, time.Time{}, err
		}
		mod, _ := http.ParseTime(res.Header.Get("Last-Modified"))
		return res.Body, res.ContentLength, mod, nil
	}
	f, err := os.Open(s.filePath(a))
	if err != nil {
		return nil, 0, time.Time{}, errors.New("the file is missing on the server")
	}
	fi, _ := f.Stat()
	return f, fi.Size(), fi.ModTime(), nil
}

// readAttachment reads a whole attachment (for the agent hand-off).
func (s *Server) readAttachment(ctx context.Context, a *store.Attachment) ([]byte, error) {
	rc, _, _, err := s.openAttachment(ctx, a)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(io.LimitReader(rc, maxUpload+1))
}

// removeAttachmentFile drops the file of a deleted attachment: from disk,
// or into the Deleted folder in Pocket Files.
func (s *Server) removeAttachmentFile(ctx context.Context, a *store.Attachment) error {
	m := metaOf(a)
	if m.FilesPath == "" {
		_ = os.Remove(s.filePath(a))
		return nil
	}
	c, err := s.filesClient(ctx, m.FilesOwner)
	if err != nil {
		return err
	}
	// <folder>/<ISSUE>/<name> → <folder>/Deleted/<ISSUE>/<date> <name>
	issueDir := path.Dir(m.FilesPath)
	dir := path.Dir(issueDir) + "/Deleted/" + path.Base(issueDir)
	if err := c.mkdirAll(ctx, dir); err != nil {
		return err
	}
	to, err := c.freeName(ctx, dir, time.Now().Format("2006-01-02")+" "+path.Base(m.FilesPath))
	if err != nil {
		return err
	}
	return c.move(ctx, m.FilesPath, to)
}

// ---------- team setting ----------

func defaultFilesFolder(teamKey string) string { return "/Mounts/Pocket Projects/" + teamKey }

type teamFilesView struct {
	Mode          string `json:"mode"`
	Folder        string `json:"folder"`
	DefaultFolder string `json:"default_folder"`
	Owner         string `json:"owner"`
	OwnerIsMe     bool   `json:"owner_is_me"`
	OK            bool   `json:"ok"`
	Error         string `json:"error,omitempty"`
	Local         int    `json:"local"`    // files still on Pocket Projects' disk
	InFiles       int    `json:"in_files"` // files in Pocket Files
}

func (s *Server) teamFilesView(ctx context.Context, t *store.Team, uid int64, check bool) teamFilesView {
	tf, _ := s.store.GetTeamFiles(ctx, t.ID)
	v := teamFilesView{Mode: tf.Mode, Folder: firstNonEmpty(tf.Folder, defaultFilesFolder(t.Key)), DefaultFolder: defaultFilesFolder(t.Key), OK: true}
	if tf.OwnerID != nil {
		if u, err := s.store.GetUser(ctx, *tf.OwnerID); err == nil {
			v.Owner, v.OwnerIsMe = firstNonEmpty(u.Name, u.Email), u.ID == uid
		}
	}
	atts, _ := s.store.TeamFileAttachments(ctx, t.ID)
	for i := range atts {
		if metaOf(&atts[i]).FilesPath != "" {
			v.InFiles++
		} else {
			v.Local++
		}
	}
	if check && tf.Mode == filesMode {
		v.OK, v.Error = false, "nobody's Pocket Files link is set"
		if tf.OwnerID != nil {
			if c, err := s.filesClient(ctx, *tf.OwnerID); err != nil {
				v.Error = err.Error()
			} else if ok, dir, _, err := c.stat(ctx, tf.Folder); err != nil {
				v.Error = err.Error()
			} else if !ok || !dir {
				v.Error = tf.Folder + " is not in Pocket Files any more"
			} else {
				v.OK, v.Error = true, ""
			}
		}
	}
	return v
}

func (s *Server) handleTeamFiles(w http.ResponseWriter, r *http.Request) {
	a := s.accessOf(r)
	t, err := s.loadTeam(r, a, chi.URLParam(r, "ref"), needAdmin)
	if err != nil {
		s.fail(w, err, "team")
		return
	}
	writeJSON(w, http.StatusOK, s.teamFilesView(r.Context(), t, a.uid, true))
}

// handlePutTeamFiles switches storage; turning Pocket Files on lends the
// admin's own link and makes sure the folder is there and writable.
func (s *Server) handlePutTeamFiles(w http.ResponseWriter, r *http.Request) {
	a := s.accessOf(r)
	t, err := s.loadTeam(r, a, chi.URLParam(r, "ref"), needAdmin)
	if err != nil {
		s.fail(w, err, "team")
		return
	}
	var in struct {
		Mode   string `json:"mode"`
		Folder string `json:"folder"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	ctx := r.Context()
	tf, _ := s.store.GetTeamFiles(ctx, t.ID)
	switch in.Mode {
	case "":
		tf.Mode = ""
	case filesMode:
		folder := "/" + strings.Trim(path.Clean("/"+strings.TrimSpace(firstNonEmpty(in.Folder, defaultFilesFolder(t.Key)))), "/")
		if folder == "/" {
			writeErr(w, http.StatusBadRequest, "pick a folder in Pocket Files, e.g. "+defaultFilesFolder(t.Key))
			return
		}
		c, err := s.filesClient(ctx, a.uid)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := c.mkdirAll(ctx, folder); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		// prove it can write there before anything is stored
		probe := folder + "/.pocket-projects-check"
		if err := c.put(ctx, probe, bytes.NewReader([]byte("ok"))); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		c.remove(ctx, probe)
		uid := a.uid
		tf.Mode, tf.Folder, tf.OwnerID = filesMode, folder, &uid
	default:
		writeErr(w, http.StatusBadRequest, "mode is \"\" (Pocket Projects) or pocket_files")
		return
	}
	if err := s.store.SaveTeamFiles(ctx, tf); err != nil {
		s.fail(w, err, "team files")
		return
	}
	writeJSON(w, http.StatusOK, s.teamFilesView(ctx, t, a.uid, true))
}

// handleMoveTeamFiles moves the team's attachments to where the setting
// says: each file is copied, checked (size) and only then removed from the
// old place, so a failure halfway leaves every attachment readable.
func (s *Server) handleMoveTeamFiles(w http.ResponseWriter, r *http.Request) {
	a := s.accessOf(r)
	t, err := s.loadTeam(r, a, chi.URLParam(r, "ref"), needAdmin)
	if err != nil {
		s.fail(w, err, "team")
		return
	}
	ctx := r.Context()
	tf, _ := s.store.GetTeamFiles(ctx, t.ID)
	if tf.Mode != filesMode || tf.OwnerID == nil {
		writeErr(w, http.StatusBadRequest, "switch the team's attachments to Pocket Files first")
		return
	}
	c, err := s.filesClient(ctx, *tf.OwnerID)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	atts, err := s.store.TeamFileAttachments(ctx, t.ID)
	if err != nil {
		s.fail(w, err, "attachments")
		return
	}
	moved, failed := 0, []string{}
	issues := map[int64]*store.Issue{}
	for i := range atts {
		att := &atts[i]
		if metaOf(att).FilesPath != "" || att.IssueID == nil {
			continue
		}
		is := issues[*att.IssueID]
		if is == nil {
			if is, err = s.store.GetIssue(ctx, *att.IssueID, a.uid); err != nil {
				failed = append(failed, att.FileName+": its issue is gone")
				continue
			}
			issues[is.ID] = is
		}
		if err := s.moveOne(ctx, c, tf, is, att); err != nil {
			failed = append(failed, att.FileName+": "+err.Error())
			continue
		}
		moved++
	}
	if moved > 0 {
		s.send(ctx, change{Kind: "team", TeamIDs: []int64{t.ID}})
	}
	writeJSON(w, http.StatusOK, map[string]any{"moved": moved, "failed": failed, "settings": s.teamFilesView(ctx, t, a.uid, false)})
}

func (s *Server) moveOne(ctx context.Context, c *davClient, tf *store.TeamFiles, is *store.Issue, att *store.Attachment) error {
	local := s.filePath(att)
	f, err := os.Open(local)
	if err != nil {
		return errors.New("the file is missing on Pocket Projects' disk")
	}
	defer f.Close()
	fi, _ := f.Stat()
	dir := strings.TrimRight(tf.Folder, "/") + "/" + is.Identifier
	if err := c.mkdirAll(ctx, dir); err != nil {
		return err
	}
	p, err := c.freeName(ctx, dir, cleanName(firstNonEmpty(att.FileName, att.Title)))
	if err != nil {
		return err
	}
	if err := c.put(ctx, p, f); err != nil {
		return err
	}
	if ok, _, size, err := c.stat(ctx, p); err != nil || !ok || size != fi.Size() {
		c.remove(ctx, p)
		return fmt.Errorf("the copy in Pocket Files did not check out (%d of %d bytes); the file stays in Pocket Projects", size, fi.Size())
	}
	meta, _ := json.Marshal(fileMeta{FilesPath: p, FilesOwner: *tf.OwnerID})
	if err := s.store.SetAttachmentFile(ctx, att.ID, fi.Size(), att.URL, meta); err != nil {
		c.remove(ctx, p)
		return err
	}
	_ = os.Remove(local)
	return nil
}
