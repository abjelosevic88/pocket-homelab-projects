package server

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/abjelosevic88/pocket-homelab-projects/internal/store"
)

// fakeFiles is enough of Pocket Files' WebDAV (/dav/…) on a folder: PROPFIND
// depth 0, MKCOL, PUT, GET, MOVE, DELETE, Basic auth with one token.
type fakeFiles struct {
	root  string
	token string
	down  atomic.Bool
}

func (f *fakeFiles) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if f.down.Load() {
		http.Error(w, "down", http.StatusServiceUnavailable)
		return
	}
	if _, pw, ok := r.BasicAuth(); !ok || pw != f.token {
		http.Error(w, "no", http.StatusUnauthorized)
		return
	}
	rel, _ := url.PathUnescape(strings.TrimPrefix(r.URL.EscapedPath(), "/dav"))
	p := filepath.Join(f.root, filepath.FromSlash(rel))
	switch r.Method {
	case "PROPFIND":
		fi, err := os.Stat(p)
		if err != nil {
			http.Error(w, "nf", http.StatusNotFound)
			return
		}
		rt := ""
		if fi.IsDir() {
			rt = "<d:collection/>"
		}
		w.WriteHeader(http.StatusMultiStatus)
		fmt.Fprintf(w, `<?xml version="1.0"?><d:multistatus xmlns:d="DAV:"><d:response><d:href>%s</d:href><d:propstat><d:prop><d:getcontentlength>%d</d:getcontentlength><d:resourcetype>%s</d:resourcetype></d:prop></d:propstat></d:response></d:multistatus>`, r.URL.Path, fi.Size(), rt)
	case "MKCOL":
		if _, err := os.Stat(p); err == nil {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if err := os.Mkdir(p, 0o755); err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		w.WriteHeader(http.StatusCreated)
	case "PUT":
		out, err := os.Create(p)
		if err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		io.Copy(out, r.Body)
		out.Close()
		w.WriteHeader(http.StatusCreated)
	case "GET":
		http.ServeFile(w, r, p)
	case "MOVE":
		du, _ := url.Parse(r.Header.Get("Destination"))
		drel, _ := url.PathUnescape(strings.TrimPrefix(du.EscapedPath(), "/dav"))
		if err := os.Rename(p, filepath.Join(f.root, filepath.FromSlash(drel))); err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		w.WriteHeader(http.StatusCreated)
	case "DELETE":
		os.RemoveAll(p)
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func uploadFile(t *testing.T, c *apiClient, ref, name string, data []byte) (int, []byte) {
	var b bytes.Buffer
	mw := multipart.NewWriter(&b)
	fw, _ := mw.CreateFormFile("file", name)
	fw.Write(data)
	mw.Close()
	return c.raw("POST", "/api/v1/issues/"+ref+"/attachments", mw.FormDataContentType(), b.Bytes())
}

func TestTeamAttachmentsInPocketFiles(t *testing.T) {
	s, h := apiServer(t)
	alex := person(t, s, h, "alex@example.com", "Alex", "admin")
	ben := person(t, s, h, "ben@example.com", "Ben", "member")
	var team store.Team
	alex.do("POST", "/api/v1/teams", map[string]any{"name": "Athena", "key": "as", "preset": "software"}, 201, &team)
	alex.do("POST", "/api/v1/teams/AS/members", map[string]any{"user_id": ben.user.ID, "role": "member"}, 200, nil)
	var is store.Issue
	alex.do("POST", "/api/v1/issues", map[string]any{"team_id": team.ID, "title": "Rules attach to a workflow"}, 201, &is)

	// a file uploaded before the switch stays on Pocket Projects' disk
	if code, b := uploadFile(t, alex, "AS-1", "before.png", []byte("\x89PNG old")); code != 201 {
		t.Fatalf("upload: %d %s", code, b)
	}

	files := &fakeFiles{root: t.TempDir(), token: "pcx_files"}
	_ = os.MkdirAll(filepath.Join(files.root, "Mounts", "Pocket Projects"), 0o755)
	srv := httptest.NewServer(files)
	t.Cleanup(srv.Close)
	ctx := context.Background()
	for k, v := range map[string]string{"app.mode.files": modeDirect, "app.url.files": srv.URL, "app.token.files": "pcx_files"} {
		if err := s.store.SetUserSetting(ctx, alex.user.ID, k, v); err != nil {
			t.Fatal(err)
		}
	}
	var v teamFilesView
	alex.do("GET", "/api/v1/teams/AS/files", nil, 200, &v)
	if v.Mode != "" || v.Folder != "/Mounts/Pocket Projects/AS" || v.Local != 1 {
		t.Fatalf("before: %+v", v)
	}
	if ben.status("PUT", "/api/v1/teams/AS/files", map[string]any{"mode": filesMode}) != 403 {
		t.Fatal("a member switched storage")
	}
	alex.do("PUT", "/api/v1/teams/AS/files", map[string]any{"mode": filesMode}, 200, &v)
	if v.Mode != filesMode || !v.OK || v.Owner != "Alex" {
		t.Fatalf("switched: %+v", v)
	}
	if _, err := os.Stat(filepath.Join(files.root, "Mounts", "Pocket Projects", "AS")); err != nil {
		t.Fatalf("team folder not made: %v", err)
	}

	// new uploads go straight to Pocket Files; a member uploads through the admin's link
	if code, b := uploadFile(t, ben, "AS-1", "shot.png", []byte("\x89PNG new")); code != 201 {
		t.Fatalf("upload to files: %d %s", code, b)
	}
	if code, _ := uploadFile(t, ben, "AS-1", "shot.png", []byte("\x89PNG second")); code != 201 {
		t.Fatal("second upload")
	}
	dir := filepath.Join(files.root, "Mounts", "Pocket Projects", "AS", "AS-1")
	if got, _ := os.ReadFile(filepath.Join(dir, "shot.png")); string(got) != "\x89PNG new" {
		t.Fatalf("in files: %q", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "shot (2).png")); err != nil {
		t.Fatal("a second file with the same name should not overwrite the first")
	}
	var atts []store.Attachment
	ben.do("GET", "/api/v1/issues/AS-1/attachments", nil, 200, &atts)
	var inFiles *store.Attachment
	for i := range atts {
		if atts[i].FileName == "shot.png" && inFiles == nil {
			inFiles = &atts[i]
		}
	}
	if inFiles == nil || metaOf(inFiles).FilesPath != "/Mounts/Pocket Projects/AS/AS-1/shot.png" || inFiles.Size != int64(len("\x89PNG new")) {
		t.Fatalf("attachment row: %+v", inFiles)
	}
	// it is served through Projects to anyone on the team
	if code, b := ben.raw("GET", "/api/v1/attachments/"+strconv.FormatInt(inFiles.ID, 10)+"/file", "", nil); code != 200 || string(b) != "\x89PNG new" {
		t.Fatalf("serve: %d %q", code, b)
	}
	// and the agent hand-off reads it from there
	if data, err := s.readAttachment(ctx, inFiles); err != nil || string(data) != "\x89PNG new" {
		t.Fatalf("read for the agent: %q %v", data, err)
	}

	// move the old one: copied, checked, removed from disk
	var mv struct {
		Moved  int
		Failed []string
	}
	alex.do("POST", "/api/v1/teams/AS/files/move", nil, 200, &mv)
	if mv.Moved != 1 || len(mv.Failed) != 0 {
		t.Fatalf("move: %+v", mv)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "before.png")); string(got) != "\x89PNG old" {
		t.Fatalf("moved file: %q", got)
	}
	alex.do("GET", "/api/v1/teams/AS/files", nil, 200, &v)
	if v.Local != 0 || v.InFiles != 3 {
		t.Fatalf("after move: %+v", v)
	}
	if entries, _ := os.ReadDir(filepath.Join(s.cfg.DataDir, "files", strconv.FormatInt(is.ID, 10))); len(entries) != 0 {
		t.Fatalf("local copy left behind: %v", entries)
	}

	// Pocket Files down: the attachment says so, nothing is lost or removed
	files.down.Store(true)
	if code, b := ben.raw("GET", "/api/v1/attachments/"+strconv.FormatInt(inFiles.ID, 10)+"/file", "", nil); code != 502 || !strings.Contains(string(b), "Pocket Files") {
		t.Fatalf("down: %d %s", code, b)
	}
	if ben.status("DELETE", "/api/v1/attachments/"+strconv.FormatInt(inFiles.ID, 10), nil) != 502 {
		t.Fatal("removed while Pocket Files was down")
	}
	if code, _ := uploadFile(t, ben, "AS-1", "x.png", []byte("x")); code != 502 {
		t.Fatalf("upload while down: %d", code)
	}
	files.down.Store(false)

	// removing moves the file to Deleted/, recoverable
	if ben.status("DELETE", "/api/v1/attachments/"+strconv.FormatInt(inFiles.ID, 10), nil) != 204 {
		t.Fatal("delete")
	}
	if _, err := os.Stat(filepath.Join(dir, "shot.png")); !os.IsNotExist(err) {
		t.Fatal("still in the issue folder")
	}
	del, _ := os.ReadDir(filepath.Join(files.root, "Mounts", "Pocket Projects", "AS", "Deleted", "AS-1"))
	if len(del) != 1 || !strings.HasSuffix(del[0].Name(), " shot.png") {
		t.Fatalf("deleted folder: %v", del)
	}

	// back to Pocket Projects: new uploads go to its disk, files already in Pocket Files keep working
	alex.do("PUT", "/api/v1/teams/AS/files", map[string]any{"mode": ""}, 200, &v)
	if code, _ := uploadFile(t, alex, "AS-1", "local.png", []byte("local")); code != 201 {
		t.Fatal("local upload")
	}
	ben.do("GET", "/api/v1/issues/AS-1/attachments", nil, 200, &atts)
	for _, a := range atts {
		if a.Kind != "file" {
			continue
		}
		if code, _ := ben.raw("GET", "/api/v1/attachments/"+strconv.FormatInt(a.ID, 10)+"/file", "", nil); code != 200 {
			t.Fatalf("%s unreadable after switching back: %d", a.FileName, code)
		}
	}
}
