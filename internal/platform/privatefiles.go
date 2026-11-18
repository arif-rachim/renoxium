package platform

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/arif-rachim/renoxium/internal/files"
)

// Private visitor uploads: personal documents such as a driving licence or
// a passport photo. Unlike the public, content-addressed images in
// /_rx/files/, each private file has a random id, belongs to one app stage
// and one visitor, and is never deduplicated, so one person's document can
// never be reached through another's upload of the same bytes.
const (
	UploadPath     = "/_rx/upload"
	UploadJSPath   = "/_rx/upload.js"
	PrivatePrefix  = "/_rx/private/"
	privateMaxBody = files.MaxSize + 64<<10 // the file plus multipart framing

	// UploadsPerVisitorPerDay caps upload attempts of one visitor.
	UploadsPerVisitorPerDay = 20
	// UploadsPerAppPerDay caps upload attempts on one app stage.
	UploadsPerAppPerDay = 500
	// MaxPrivateFilesPerApp caps the private files one app stage keeps.
	MaxPrivateFilesPerApp = 5000
)

var privateIDRE = regexp.MustCompile(`^pf_[0-9a-f]{32}$`)

// PrivateFile describes a visitor's private upload.
type PrivateFile struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Size        int64  `json:"size"`
	ContentType string `json:"type"`
	UserID      int64  `json:"user_id"`
	CreatedAt   string `json:"created_at"`
}

// Map is the value scripts see from files.get.
func (f *PrivateFile) Map() map[string]any {
	return map[string]any{"id": f.ID, "name": f.Name, "size": f.Size, "type": f.ContentType, "user_id": f.UserID,
		"created_at": f.CreatedAt, "url": PrivatePrefix + f.ID}
}

func (p *Platform) ensurePrivateFileTable() error {
	for _, stmt := range []string{
		`CREATE TABLE IF NOT EXISTS private_files (id TEXT PRIMARY KEY, app TEXT NOT NULL, stage TEXT NOT NULL, user_id INTEGER NOT NULL,
			name TEXT NOT NULL, size INTEGER NOT NULL, content_type TEXT NOT NULL, created_at TEXT NOT NULL)`,
		`CREATE INDEX IF NOT EXISTS private_files_user ON private_files (app, stage, user_id)`,
	} {
		if _, err := p.db.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}

func (p *Platform) privateDir(app, stageName string) string {
	return filepath.Join(p.appDir(app), "private", stageName)
}

func newPrivateID() string {
	var b [16]byte
	rand.Read(b[:])
	return "pf_" + hex.EncodeToString(b[:])
}

// ValidPrivateID reports whether id looks like a private file id.
func ValidPrivateID(id string) bool { return privateIDRE.MatchString(id) }

// PrivateFileInfo returns a private file of an app stage, or ErrNotFound.
func (p *Platform) PrivateFileInfo(app, stageName, id string) (*PrivateFile, error) {
	if !ValidPrivateID(id) {
		return nil, fmt.Errorf("file %q: %w", id, ErrNotFound)
	}
	var f PrivateFile
	err := p.db.QueryRow(`SELECT id, name, size, content_type, user_id, created_at FROM private_files WHERE id = ? AND app = ? AND stage = ?`,
		id, app, stageName).Scan(&f.ID, &f.Name, &f.Size, &f.ContentType, &f.UserID, &f.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("file %q: %w", id, ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	return &f, nil
}

// OpenPrivateFile returns a private file and its content.
func (p *Platform) OpenPrivateFile(app, stageName, id string) (*PrivateFile, *os.File, error) {
	f, err := p.PrivateFileInfo(app, stageName, id)
	if err != nil {
		return nil, nil, err
	}
	fh, err := os.Open(filepath.Join(p.privateDir(app, stageName), f.ID))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil, fmt.Errorf("file %q: %w", id, ErrNotFound)
		}
		return nil, nil, err
	}
	return f, fh, nil
}

// DeletePrivateFile removes a private file. It returns ErrNotFound when the
// stage has no such file.
func (p *Platform) DeletePrivateFile(app, stageName, id string) error {
	if !ValidPrivateID(id) {
		return fmt.Errorf("file %q: %w", id, ErrNotFound)
	}
	res, err := p.db.Exec(`DELETE FROM private_files WHERE id = ? AND app = ? AND stage = ?`, id, app, stageName)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("file %q: %w", id, ErrNotFound)
	}
	os.Remove(filepath.Join(p.privateDir(app, stageName), id))
	return nil
}

// PrivateFiles lists the private files of an app stage, newest first; a
// userID above 0 keeps only that visitor's.
func (p *Platform) PrivateFiles(app, stageName string, userID int64) ([]PrivateFile, error) {
	rows, err := p.db.Query(`SELECT id, name, size, content_type, user_id, created_at FROM private_files
		WHERE app = ? AND stage = ? AND (? = 0 OR user_id = ?) ORDER BY created_at DESC, rowid DESC`, app, stageName, userID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PrivateFile{}
	for rows.Next() {
		var f PrivateFile
		if err := rows.Scan(&f.ID, &f.Name, &f.Size, &f.ContentType, &f.UserID, &f.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// PrivateFileCounts maps visitor ids of an app stage to how many private
// files each has.
func (p *Platform) PrivateFileCounts(app, stageName string) map[int64]int {
	out := map[int64]int{}
	rows, err := p.db.Query(`SELECT user_id, count(*) FROM private_files WHERE app = ? AND stage = ? GROUP BY user_id`, app, stageName)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var n int
		if rows.Scan(&id, &n) == nil {
			out[id] = n
		}
	}
	return out
}

// deleteVisitorFiles removes every private file of one visitor of a stage.
func (p *Platform) deleteVisitorFiles(app, stageName string, userID int64) error {
	list, err := p.PrivateFiles(app, stageName, userID)
	if err != nil {
		return err
	}
	for _, f := range list {
		if err := p.DeletePrivateFile(app, stageName, f.ID); err != nil && !errors.Is(err, ErrNotFound) {
			return err
		}
	}
	return nil
}

// dropPrivateFiles removes every private file of a stage (a stage ""
// means both), e.g. when preview data is rebuilt from live.
func (p *Platform) dropPrivateFiles(app, stageName string) error {
	if stageName == "" {
		if _, err := p.db.Exec(`DELETE FROM private_files WHERE app = ?`, app); err != nil {
			return err
		}
		return os.RemoveAll(filepath.Join(p.appDir(app), "private"))
	}
	if _, err := p.db.Exec(`DELETE FROM private_files WHERE app = ? AND stage = ?`, app, stageName); err != nil {
		return err
	}
	return os.RemoveAll(p.privateDir(app, stageName))
}

// SanitizeFileName keeps a visitor's file name readable and harmless: no
// path, no control or quote characters, at most 100 characters, and the
// extension of the detected type.
func SanitizeFileName(name, contentType string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	if i := strings.LastIndex(name, "."); i >= 0 {
		name = name[:i]
	}
	var b strings.Builder
	for _, r := range name {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' || r == '(' || r == ')':
			b.WriteRune(r)
		case r == ' ' || r == '.':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	base := strings.Trim(b.String(), " ._")
	if r := []rune(base); len(r) > 90 {
		base = strings.TrimSpace(string(r[:90]))
	}
	if base == "" {
		base = "file"
	}
	return base + "." + files.PrivateTypes[contentType]
}

// headBuffer keeps the first 512 bytes written to it, for type sniffing.
type headBuffer struct{ buf *bytes.Buffer }

func (h *headBuffer) Write(p []byte) (int, error) {
	if room := 512 - h.buf.Len(); room > 0 {
		h.buf.Write(p[:min(room, len(p))])
	}
	return len(p), nil
}

// UploadError is a refused visitor upload; Status is the HTTP status.
type UploadError struct {
	Status  int
	Message string
}

func (e *UploadError) Error() string { return e.Message }

// AddPrivateFile stores a visitor's upload for an app stage. The content
// type comes from the bytes, never from the name or the request.
func (p *Platform) AddPrivateFile(app, stageName string, userID int64, name string, r io.Reader) (*PrivateFile, error) {
	var count int
	if err := p.db.QueryRow(`SELECT count(*) FROM private_files WHERE app = ? AND stage = ?`, app, stageName).Scan(&count); err != nil {
		return nil, err
	}
	if count >= MaxPrivateFilesPerApp {
		return nil, &UploadError{http.StatusInsufficientStorage, fmt.Sprintf("this app already keeps %d uploaded files, its limit", count)}
	}
	dir := p.privateDir(app, stageName)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	tmp, err := os.CreateTemp(dir, "upload-*")
	if err != nil {
		return nil, err
	}
	defer os.Remove(tmp.Name()) // no-op once renamed
	defer tmp.Close()
	var head bytes.Buffer
	n, err := io.Copy(io.MultiWriter(tmp, &headBuffer{buf: &head}), io.LimitReader(r, files.MaxSize+1))
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			return nil, &UploadError{http.StatusRequestEntityTooLarge, files.ErrTooLarge.Error()}
		}
		return nil, &UploadError{http.StatusBadRequest, "the upload was interrupted"}
	}
	if n > files.MaxSize {
		return nil, &UploadError{http.StatusRequestEntityTooLarge, files.ErrTooLarge.Error()}
	}
	if n == 0 {
		return nil, &UploadError{http.StatusBadRequest, "the file is empty"}
	}
	ctype := files.DetectPrivate(head.Bytes())
	if ctype == "" {
		return nil, &UploadError{http.StatusUnsupportedMediaType, files.ErrPrivateType.Error()}
	}
	owner := p.ownerOf(app)
	if used, quota := p.StorageUsage(owner); used+n > quota {
		return nil, &UploadError{http.StatusInsufficientStorage, "this app's storage is full; tell the business"}
	}
	f := &PrivateFile{ID: newPrivateID(), Name: SanitizeFileName(name, ctype), Size: n, ContentType: ctype, UserID: userID,
		CreatedAt: time.Now().UTC().Format("2006-01-02 15:04:05")}
	if err := tmp.Sync(); err != nil {
		return nil, err
	}
	if err := tmp.Close(); err != nil {
		return nil, err
	}
	if err := os.Rename(tmp.Name(), filepath.Join(dir, f.ID)); err != nil {
		return nil, err
	}
	if _, err := p.db.Exec(`INSERT INTO private_files (id, app, stage, user_id, name, size, content_type, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		f.ID, app, stageName, userID, f.Name, f.Size, f.ContentType, f.CreatedAt); err != nil {
		os.Remove(filepath.Join(dir, f.ID))
		return nil, err
	}
	return f, nil
}

// serveUpload handles POST /_rx/upload: one file in the multipart field
// "file", from a signed-in visitor. ServeApp has already refused requests
// from other origins, and the session cookie is SameSite=Lax.
func (p *Platform) serveUpload(w http.ResponseWriter, r *http.Request, st *stage) {
	w.Header().Set("Cache-Control", "no-store")
	rid := newRequestID()
	w.Header().Set("X-Request-Id", rid)
	fail := func(status int, msg string) {
		if status != http.StatusUnauthorized && status < 500 {
			p.event(st, "server", "warn", rid, fmt.Sprintf("POST %s -> %d: %s", UploadPath, status, msg))
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		json.NewEncoder(w).Encode(map[string]string{"error": msg})
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		fail(http.StatusMethodNotAllowed, "send the file with POST")
		return
	}
	user := p.visitor(st, r)
	if user == nil {
		fail(http.StatusUnauthorized, "sign in to upload files")
		return
	}
	if !p.allow(fmt.Sprintf("upload:%s:%s:%d", st.app, st.name, user.ID), UploadsPerVisitorPerDay, 24*time.Hour) ||
		!p.allow("upload:"+st.app+":"+st.name, UploadsPerAppPerDay, 24*time.Hour) {
		w.Header().Set("Retry-After", "3600")
		fail(http.StatusTooManyRequests, "too many uploads today; try again tomorrow")
		return
	}
	// A 10 MB photo from a phone can take longer than the server's usual
	// read timeout.
	http.NewResponseController(w).SetReadDeadline(time.Now().Add(3 * time.Minute))
	r.Body = http.MaxBytesReader(w, r.Body, privateMaxBody)
	mr, err := r.MultipartReader()
	if err != nil {
		fail(http.StatusBadRequest, "send the file as multipart/form-data in a field named file")
		return
	}
	for {
		part, err := mr.NextPart()
		if err != nil {
			var tooBig *http.MaxBytesError
			if errors.As(err, &tooBig) {
				fail(http.StatusRequestEntityTooLarge, files.ErrTooLarge.Error())
				return
			}
			fail(http.StatusBadRequest, "send the file as multipart/form-data in a field named file")
			return
		}
		if part.FormName() != "file" || part.FileName() == "" {
			part.Close()
			continue
		}
		f, err := p.AddPrivateFile(st.app, st.name, user.ID, part.FileName(), part)
		part.Close()
		if err != nil {
			var ue *UploadError
			if errors.As(err, &ue) {
				fail(ue.Status, ue.Message)
				return
			}
			p.event(st, "server", "error", rid, "upload failed: "+err.Error())
			fail(http.StatusInternalServerError, "the file could not be stored; try again")
			return
		}
		p.event(st, "server", "info", rid, fmt.Sprintf("visitor #%d uploaded %s (%s, %d bytes)", user.ID, f.ID, f.ContentType, f.Size))
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"id": f.ID, "name": f.Name, "size": f.Size, "type": f.ContentType})
		return
	}
}

// servePrivate handles GET /_rx/private/<id> on an app host: only the
// visitor who uploaded the file may open it there. Everyone else gets 404,
// as if it did not exist.
func (p *Platform) servePrivate(w http.ResponseWriter, r *http.Request, st *stage) {
	id := strings.TrimPrefix(r.URL.Path, PrivatePrefix)
	user := p.visitor(st, r)
	if (r.Method != http.MethodGet && r.Method != http.MethodHead) || user == nil {
		privateNotFound(w)
		return
	}
	f, fh, err := p.OpenPrivateFile(st.app, st.name, id)
	if err != nil || f.UserID != user.ID {
		if fh != nil {
			fh.Close()
		}
		privateNotFound(w)
		return
	}
	defer fh.Close()
	ServePrivateFile(w, r, f, fh)
}

func privateNotFound(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	http.Error(w, "not found", http.StatusNotFound)
}

// ServePrivateFile writes a private file with headers that keep it private
// and harmless: the exact stored type, never sniffed; PDFs as downloads;
// no scripts, no framing, no shared caches.
func ServePrivateFile(w http.ResponseWriter, r *http.Request, f *PrivateFile, content io.ReadSeeker) {
	h := w.Header()
	h.Set("Content-Type", f.ContentType)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "default-src 'none'; sandbox; frame-ancestors 'none'")
	h.Set("Cross-Origin-Resource-Policy", "same-origin")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("X-Robots-Tag", "noindex")
	h.Set("Vary", "Cookie")
	disposition := "inline"
	if f.ContentType == "application/pdf" {
		disposition = "attachment"
		h.Set("Cache-Control", "private, no-store")
	} else {
		h.Set("Cache-Control", "private, max-age=3600")
	}
	h.Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": f.Name}))
	http.ServeContent(w, r, "", time.Time{}, content)
}

// filesCall implements files.get, files.delete and files.url for scripts.
// Scripts see the records, never the bytes.
func (p *Platform) filesCall(_ context.Context, st *stage, fn string, arg any) (any, error) {
	id, ok := arg.(string)
	if !ok {
		return nil, fmt.Errorf("%s expects a file id (a string such as the id /_rx/upload returned)", fn)
	}
	f, err := p.PrivateFileInfo(st.app, st.name, strings.TrimSpace(id))
	if errors.Is(err, ErrNotFound) {
		if fn == "files.delete" {
			return false, nil
		}
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	switch fn {
	case "files.get":
		return f.Map(), nil
	case "files.url":
		return PrivatePrefix + f.ID, nil
	case "files.delete":
		if err := p.DeletePrivateFile(st.app, st.name, f.ID); err != nil {
			return nil, err
		}
		return true, nil
	}
	return nil, fmt.Errorf("unknown platform function %s", fn)
}

// uploadJS upgrades <input type="file" data-rx-upload name="licence">: the
// file is uploaded when chosen, and a hidden field with the same name
// carries the returned id when the form is sent.
const uploadJS = `(function(){"use strict";
function up(input){if(input.dataset.rxReady)return;input.dataset.rxReady="1";var name=input.getAttribute("name")||"file";input.removeAttribute("name");
var hidden=document.createElement("input");hidden.type="hidden";hidden.name=name;hidden.value=input.getAttribute("data-value")||"";input.insertAdjacentElement("afterend",hidden);
var label=document.createElement("span");label.className="rx-upload-status";label.setAttribute("aria-live","polite");hidden.insertAdjacentElement("afterend",label);
var busy=false;
if(input.form)input.form.addEventListener("submit",function(e){if(busy){e.preventDefault();label.textContent="Please wait until the upload finishes."}});
input.addEventListener("change",function(){var file=input.files&&input.files[0];hidden.value="";if(!file){label.textContent="";return}
var body=new FormData();body.append("file",file);var x=new XMLHttpRequest();busy=true;label.textContent="Uploading "+file.name+"...";
x.upload.onprogress=function(e){if(e.lengthComputable)label.textContent="Uploading "+file.name+": "+Math.round(e.loaded*100/e.total)+"%"};
x.onload=function(){busy=false;var r={};try{r=JSON.parse(x.responseText)}catch(err){}
if(x.status===200&&r.id){hidden.value=r.id;label.textContent=r.name+" uploaded";input.dispatchEvent(new CustomEvent("rx-uploaded",{detail:r,bubbles:true}))}
else{input.value="";label.textContent=r.error||("Upload failed ("+x.status+")")}};
x.onerror=function(){busy=false;input.value="";label.textContent="Upload failed; check your connection and try again."};
x.open("POST","/_rx/upload");x.send(body)})}
function scan(){var l=document.querySelectorAll('input[type="file"][data-rx-upload]');for(var i=0;i<l.length;i++)up(l[i])}
if(document.readyState==="loading")document.addEventListener("DOMContentLoaded",scan);else scan()})();`

func serveUploadJS(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	io.WriteString(w, uploadJS)
}

func (p *Platform) servePrivateRoutes(w http.ResponseWriter, r *http.Request, st *stage) {
	switch {
	case r.URL.Path == UploadJSPath:
		serveUploadJS(w, r)
	case r.URL.Path == UploadPath:
		p.serveUpload(w, r, st)
	default:
		p.servePrivate(w, r, st)
	}
}

// exportPrivateFiles adds the live private files of an app to an export:
// private-files.json and each file under private-files/<id>/<name>.
func (p *Platform) exportPrivateFiles(app string, zw *zip.Writer, add func(string, any) error) error {
	list, err := p.PrivateFiles(app, Live, 0)
	if err != nil || len(list) == 0 {
		return err
	}
	if err := add("private-files.json", list); err != nil {
		return err
	}
	for _, f := range list {
		fh, err := os.Open(filepath.Join(p.privateDir(app, Live), f.ID))
		if err != nil {
			continue // a file removed while exporting
		}
		w, err := zw.Create(app + "/private-files/" + f.ID + "/" + f.Name)
		if err == nil {
			_, err = io.Copy(w, fh)
		}
		fh.Close()
		if err != nil {
			return err
		}
	}
	return nil
}
