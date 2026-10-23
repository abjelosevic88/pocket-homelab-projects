package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/abjelosevic88/pocket-homelab-projects/internal/store"
)

// Files attached to issues live in DATA_DIR/files/<issue id>/<attachment id>;
// links and items in other apps are rows only.

const maxUpload = 100 << 20

func (s *Server) filePath(a *store.Attachment) string {
	return filepath.Join(s.cfg.DataDir, "files", strconv.FormatInt(derefID(a.IssueID), 10), strconv.FormatInt(a.ID, 10))
}

func derefID(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}

func (s *Server) handleListAttachments(w http.ResponseWriter, r *http.Request) {
	a := s.accessOf(r)
	is, err := s.loadIssue(r, a, chi.URLParam(r, "ref"), needRead)
	if err != nil {
		s.fail(w, err, "issue")
		return
	}
	list, err := s.store.Attachments(r.Context(), is.ID, 0)
	if err != nil {
		s.fail(w, err, "attachments")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// handleAddAttachment takes a multipart file (field "file") or a JSON link {url, title}.
func (s *Server) handleAddAttachment(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	a := s.accessOf(r)
	is, err := s.loadIssue(r, a, chi.URLParam(r, "ref"), needRead) // guests may attach to their comments
	if err != nil {
		s.fail(w, err, "issue")
		return
	}
	att := &store.Attachment{IssueID: &is.ID, CreatedBy: ptr(me(r))}
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
		r.Body = http.MaxBytesReader(w, r.Body, maxUpload+1<<20)
		f, hdr, err := r.FormFile("file")
		if err != nil {
			writeErr(w, http.StatusBadRequest, "send the file as the multipart field \"file\" (at most 100 MB)")
			return
		}
		defer f.Close()
		name := filepath.Base(strings.ReplaceAll(hdr.Filename, "\\", "/"))
		if name == "." || name == "/" || name == "" {
			name = "file"
		}
		att.Kind, att.Title, att.FileName = "file", firstNonEmpty(r.FormValue("title"), name), name
		att.Mime = hdr.Header.Get("Content-Type")
		if att.Mime == "" || att.Mime == "application/octet-stream" {
			if t := mime.TypeByExtension(filepath.Ext(name)); t != "" {
				att.Mime = t
			}
		}
		// the team keeps attachments in Pocket Files: straight there
		if tf, _ := s.store.GetTeamFiles(ctx, is.TeamID); tf.Mode == filesMode {
			if err := s.store.SaveAttachment(ctx, att); err != nil {
				s.fail(w, err, "attachment")
				return
			}
			n, meta, err := s.saveToFiles(ctx, tf, is, name, f)
			if err != nil {
				_ = s.store.DeleteAttachment(ctx, att.ID)
				code := http.StatusBadGateway
				if errors.Is(err, errTooLarge) {
					code = http.StatusBadRequest
				}
				writeErr(w, code, "Not attached: "+err.Error())
				return
			}
			att.Size, att.Meta = n, meta
			att.URL = "/api/v1/attachments/" + strconv.FormatInt(att.ID, 10) + "/file"
			_ = s.store.SetAttachmentFile(ctx, att.ID, n, att.URL, meta)
			_ = s.store.AddHistory(ctx, store.History{IssueID: &is.ID, ActorID: ptr(me(r)), ActorApp: s.linkApp(r), Field: "attachment_added", ToLabel: att.Title})
			s.changed(r, "issue", []int64{is.TeamID}, is.ID)
			writeJSON(w, http.StatusCreated, att)
			return
		}
		if err := s.store.SaveAttachment(ctx, att); err != nil {
			s.fail(w, err, "attachment")
			return
		}
		path := s.filePath(att)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			_ = s.store.DeleteAttachment(ctx, att.ID)
			s.fail(w, err, "attachment")
			return
		}
		out, err := os.Create(path)
		if err != nil {
			_ = s.store.DeleteAttachment(ctx, att.ID)
			s.fail(w, err, "attachment")
			return
		}
		n, err := io.Copy(out, io.LimitReader(f, maxUpload+1))
		cerr := out.Close()
		if err != nil || cerr != nil || n > maxUpload {
			_ = os.Remove(path)
			_ = s.store.DeleteAttachment(ctx, att.ID)
			writeErr(w, http.StatusBadRequest, "the upload failed or is larger than 100 MB")
			return
		}
		att.Size = n
		att.URL = "/api/v1/attachments/" + strconv.FormatInt(att.ID, 10) + "/file"
		_, _ = s.store.DB.ExecContext(ctx, `UPDATE attachments SET size = ?, url = ? WHERE id = ?`, n, att.URL, att.ID)
	} else {
		var in struct {
			URL      string          `json:"url"`
			Title    string          `json:"title"`
			Subtitle string          `json:"subtitle"`
			Kind     string          `json:"kind"`
			Meta     json.RawMessage `json:"meta"`
		}
		if err := decode(r, &in); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid json")
			return
		}
		u, err := url.Parse(strings.TrimSpace(in.URL))
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			writeErr(w, http.StatusBadRequest, "a link needs an http(s) address")
			return
		}
		// a pull request address becomes a tracked pull request (people who may edit the issue)
		if in.Kind == "" {
			if _, ok := parsePRURL(u.String()); ok {
				if wis, err := s.loadIssue(r, a, chi.URLParam(r, "ref"), needWrite); err == nil {
					s.linkPR(w, r, wis, u.String(), in.Title, "", "", 0)
					return
				}
			}
		}
		att.Kind, att.URL, att.Title, att.Subtitle, att.Meta = "link", u.String(), strings.TrimSpace(in.Title), in.Subtitle, in.Meta
		if in.Kind == "note" || in.Kind == "review" || in.Kind == "document" || in.Kind == "file_link" {
			att.Kind = in.Kind
		}
		if att.Title == "" {
			att.Title = u.Host + u.Path
		}
		att.ExtApp = appOfURL(u)
		if err := s.store.SaveAttachment(ctx, att); err != nil {
			s.fail(w, err, "attachment")
			return
		}
	}
	_ = s.store.AddHistory(ctx, store.History{IssueID: &is.ID, ActorID: ptr(me(r)), ActorApp: s.linkApp(r), Field: "attachment_added", ToLabel: att.Title})
	s.changed(r, "issue", []int64{is.TeamID}, is.ID)
	writeJSON(w, http.StatusCreated, att)
}

// appOfURL guesses which Pocket app (or service) a link points to from its host.
func appOfURL(u *url.URL) string {
	host := strings.ToLower(u.Hostname())
	for _, app := range []string{"notes", "drive", "files", "documents", "collab", "office", "calendar", "tasks", "contacts", "git", "github", "figma", "docs"} {
		if strings.HasPrefix(host, app+".") || strings.Contains(host, "."+app+".") || host == app+".com" {
			return app
		}
	}
	return ""
}

func (s *Server) attachmentFor(r *http.Request, need int) (*store.Attachment, *store.Issue, error) {
	att, err := s.store.GetAttachment(r.Context(), idParam(r, "id"))
	if err != nil {
		return nil, nil, err
	}
	a := s.accessOf(r)
	if att.IssueID == nil {
		if att.ProjectID == nil {
			return nil, nil, store.ErrNotFound
		}
		if _, err := s.loadProject(r, a, *att.ProjectID, need); err != nil {
			return nil, nil, err
		}
		return att, nil, nil
	}
	is, err := s.loadIssue(r, a, strconv.FormatInt(*att.IssueID, 10), need)
	if err != nil {
		return nil, nil, err
	}
	return att, is, nil
}

func (s *Server) handleAttachmentFile(w http.ResponseWriter, r *http.Request) {
	att, _, err := s.attachmentFor(r, needRead)
	if err != nil || att.Kind != "file" {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	rc, size, mod, err := s.openAttachment(r.Context(), att)
	if err != nil {
		code := http.StatusBadGateway // Pocket Files unreachable or the link is off
		if metaOf(att).FilesPath == "" {
			code = http.StatusNotFound
		}
		writeErr(w, code, err.Error())
		return
	}
	defer rc.Close()
	ct := att.Mime
	if ct == "" {
		ct = "application/octet-stream"
	}
	// Only images, PDFs and plain text open inline; everything else downloads.
	disp := "attachment"
	if strings.HasPrefix(ct, "image/") && ct != "image/svg+xml" || ct == "application/pdf" || ct == "text/plain" {
		disp = "inline"
	}
	if queryBool(r, "download") {
		disp = "attachment"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; img-src 'self'; style-src 'unsafe-inline'; sandbox")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`%s; filename*=UTF-8''%s`, disp, url.PathEscape(att.FileName)))
	w.Header().Set("Cache-Control", "private, max-age=86400")
	if f, ok := rc.(*os.File); ok {
		http.ServeContent(w, r, "", mod, f) // ranges and If-Modified-Since for files on disk
		return
	}
	if !mod.IsZero() {
		w.Header().Set("Last-Modified", mod.UTC().Format(http.TimeFormat))
	}
	if size > 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	}
	if r.Method == http.MethodHead {
		return
	}
	_, _ = io.Copy(w, rc)
}

func (s *Server) handleDeleteAttachment(w http.ResponseWriter, r *http.Request) {
	att, is, err := s.attachmentFor(r, needRead)
	if err != nil {
		s.fail(w, err, "attachment")
		return
	}
	a := s.accessOf(r)
	mine := att.CreatedBy != nil && *att.CreatedBy == a.uid
	if is != nil && !mine && !a.can(is.TeamID, needWrite) {
		writeErr(w, http.StatusForbidden, "guests can remove only what they attached")
		return
	}
	// the file first: in Pocket Files it moves to Deleted/, and when that
	// can't happen right now the attachment stays rather than leaving a stray file
	if att.Kind == "file" {
		if err := s.removeAttachmentFile(r.Context(), att); err != nil {
			writeErr(w, http.StatusBadGateway, "Not removed: "+err.Error())
			return
		}
	}
	if err := s.store.DeleteAttachment(r.Context(), att.ID); err != nil {
		s.fail(w, err, "attachment")
		return
	}
	if is != nil {
		_ = s.store.AddHistory(r.Context(), store.History{IssueID: &is.ID, ActorID: ptr(me(r)), Field: "attachment_removed", FromLabel: att.Title})
		s.changed(r, "issue", []int64{is.TeamID}, is.ID)
	}
	w.WriteHeader(http.StatusNoContent)
}
