package platform

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/arif-rachim/renoxium/internal/files"
)

// FilesPrefix is the path under which every app serves its files.
const FilesPrefix = "/_rx/files/"

// FileInfo describes a file an app can use.
type FileInfo struct {
	URL         string `json:"url" jsonschema:"path to use in templates, e.g. <img src=\"{{.image}}\">; works on live, preview and custom domains"`
	Size        int64  `json:"size"`
	ContentType string `json:"content_type"`
	Reused      bool   `json:"reused" jsonschema:"true when identical content was already stored, so nothing new was written"`
}

func fileURL(b files.Blob) string { return FilesPrefix + b.Hash + "." + b.Ext() }

// AddFile stores a file for an app. Identical content is stored once on disk
// and recorded once per app.
func (p *Platform) AddFile(app string, r io.Reader) (FileInfo, error) {
	if _, err := p.Info(app); err != nil {
		return FileInfo{}, err
	}
	var count int64
	if err := p.db.QueryRow(`SELECT count(*) FROM app_files WHERE app = ?`, app).Scan(&count); err != nil {
		return FileInfo{}, err
	}
	if count >= MaxFilesPerApp {
		return FileInfo{}, fmt.Errorf("this app already has %d files; the limit is %d", count, MaxFilesPerApp)
	}
	b, existed, err := p.files.PutImage(r) // a photo wider than the largest size ever shown is stored at that size
	if err != nil {
		return FileInfo{}, err
	}
	owner := p.ownerOf(app)
	var mine int
	p.db.QueryRow(`SELECT count(*) FROM app_files f JOIN apps a ON a.name = f.app WHERE a.owner = ? AND f.hash = ?`, owner, b.Hash).Scan(&mine)
	if used, quota := p.StorageUsage(owner); mine == 0 && used+b.Size > quota {
		return FileInfo{}, fmt.Errorf("storage is full: %d MB of %d MB used, and this file is %.1f MB; delete unused images, or for more, %s",
			used>>20, quota>>20, float64(b.Size)/(1<<20), p.moreHint(p.quotaPlan(owner), app))
	}
	if _, err := p.db.Exec(`INSERT OR IGNORE INTO blobs (hash, size, content_type, created_at, fitted) VALUES (?, ?, ?, ?, 1)`,
		b.Hash, b.Size, b.ContentType, now()); err != nil {
		return FileInfo{}, err
	}
	if _, err := p.db.Exec(`INSERT OR IGNORE INTO app_files (app, hash, created_at) VALUES (?, ?, ?)`, app, b.Hash, now()); err != nil {
		return FileInfo{}, err
	}
	return FileInfo{URL: fileURL(b), Size: b.Size, ContentType: b.ContentType, Reused: existed}, nil
}

// Files lists an app's files, newest first.
func (p *Platform) Files(app string) ([]FileInfo, error) {
	rows, err := p.db.Query(`SELECT b.hash, b.size, b.content_type FROM app_files f JOIN blobs b ON b.hash = f.hash
		WHERE f.app = ? ORDER BY f.created_at DESC, f.rowid DESC`, app)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []FileInfo{}
	for rows.Next() {
		var b files.Blob
		if err := rows.Scan(&b.Hash, &b.Size, &b.ContentType); err != nil {
			return nil, err
		}
		out = append(out, FileInfo{URL: fileURL(b), Size: b.Size, ContentType: b.ContentType, Reused: true})
	}
	return out, rows.Err()
}

// serveFile serves /_rx/files/<hash>.<ext> for one app. An app can only serve
// files recorded for it, even though content is shared on disk.
func (p *Platform) serveFile(w http.ResponseWriter, r *http.Request, app string) {
	name := strings.TrimPrefix(r.URL.Path, FilesPrefix)
	hash, ext, _ := strings.Cut(name, ".")
	var b files.Blob
	err := p.db.QueryRow(`SELECT b.hash, b.size, b.content_type FROM app_files f JOIN blobs b ON b.hash = f.hash
		WHERE f.app = ? AND f.hash = ?`, app, hash).Scan(&b.Hash, &b.Size, &b.ContentType)
	if err != nil || ext != b.Ext() || (r.Method != http.MethodGet && r.Method != http.MethodHead) {
		http.NotFound(w, r)
		return
	}
	ctype, etag := b.ContentType, b.Hash
	var f *os.File
	if want, werr := strconv.Atoi(r.URL.Query().Get("w")); werr == nil && want > 0 && files.Resizable(b.ContentType) {
		width := files.SnapWidth(want)
		// New sizes are made at most 30 times a minute per app; past that,
		// and when the resizer is busy, this request gets the original.
		deferred := false
		f, ctype, err = p.files.Variant(b, width, func() bool {
			deferred = !p.allow("resize:"+app, 30, time.Minute)
			return !deferred
		})
		etag = fmt.Sprintf("%s-%d", b.Hash, width)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			// A file the resizer cannot read (stored before damaged uploads
			// were refused) is still served as it is: the browser may manage,
			// and a server error would hide the image for certain.
			slog.Warn("resize stored file", "app", app, "hash", b.Hash, "width", width, "err", err)
			f, err = p.files.Open(hash)
			ctype, etag = b.ContentType, b.Hash
		} else if err == nil && (deferred || files.Deferred(f, b, width)) {
			// Not the final answer for this address: do not let it be cached.
			defer f.Close()
			h := w.Header()
			h.Set("Content-Type", ctype)
			h.Set("Cache-Control", "no-store")
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("Content-Security-Policy", "default-src 'none'; sandbox")
			http.ServeContent(w, r, "", time.Time{}, f)
			return
		}
	} else {
		f, err = p.files.Open(hash)
	}
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "file unavailable", http.StatusInternalServerError)
		return
	}
	defer f.Close()
	h := w.Header()
	h.Set("Content-Type", ctype)
	h.Set("Cache-Control", "public, max-age=31536000, immutable")
	h.Set("ETag", fmt.Sprintf("%q", etag))
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	http.ServeContent(w, r, "", time.Time{}, f)
}

// FitStoredPhotos makes photos stored before uploads were shrunk fit
// files.MaxSide, one at a time with a pause between them, until none is left
// or ctx ends, and returns how many files got smaller. A file keeps its
// address; its recorded size, which storage usage sums, follows the file.
// Each file is looked at once.
func (p *Platform) FitStoredPhotos(ctx context.Context, pause time.Duration) int {
	changed := 0
	for ctx.Err() == nil {
		var b files.Blob
		err := p.db.QueryRow(`SELECT hash, size, content_type FROM blobs WHERE fitted = 0 ORDER BY size DESC LIMIT 1`).Scan(&b.Hash, &b.Size, &b.ContentType)
		if err != nil {
			break // none left
		}
		size, did, err := p.files.FitStored(b)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			slog.Error("fit stored photo", "hash", b.Hash, "err", err)
		}
		if _, err := p.db.Exec(`UPDATE blobs SET size = ?, fitted = 1 WHERE hash = ?`, size, b.Hash); err != nil {
			break
		}
		if did {
			changed++
			select {
			case <-ctx.Done():
			case <-time.After(pause):
			}
		}
	}
	return changed
}

// AddFileFrom downloads an image from a public https address and stores it
// for an app like AddFile. It is how an assistant hands over a photo the
// user attached in the chat (ChatGPT gives a temporary download address)
// or one already on the web, instead of typing the file out as base64. The
// download uses the same guarded client as http.fetch: public addresses
// only, at most a few redirects, each checked again.
func (p *Platform) AddFileFrom(ctx context.Context, app, address string) (FileInfo, error) {
	if _, err := p.Info(app); err != nil {
		return FileInfo{}, err
	}
	if err := downloadable(address); err != nil {
		return FileInfo{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return FileInfo{}, fmt.Errorf("the file address cannot be used: %v", err)
	}
	req.Header.Set("Accept", "image/*")
	client := p.fetchClient()
	client.CheckRedirect = func(r *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many redirects")
		}
		return downloadable(r.URL.String())
	}
	resp, err := client.Do(req)
	if err != nil {
		return FileInfo{}, fmt.Errorf("the file could not be downloaded from %s: %v", req.URL.Host, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return FileInfo{}, fmt.Errorf("the file could not be downloaded from %s: it answered %d %s (a chat attachment's address expires after a while: attach the file again)",
			req.URL.Host, resp.StatusCode, http.StatusText(resp.StatusCode))
	}
	if resp.ContentLength > files.MaxUpload {
		return FileInfo{}, files.ErrPhotoTooLarge
	}
	return p.AddFile(app, resp.Body)
}

// downloadable checks an address AddFileFrom may download from.
func downloadable(address string) error {
	u, err := url.Parse(address)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Hostname() == "" || (u.Port() != "" && u.Port() != "443") {
		return errors.New("the file address must be a public https:// address on port 443")
	}
	return nil
}

// UploadedFile is a file of an app as the assistant sees it: its address
// and where the preview's rows use it.
type UploadedFile struct {
	URL       string   `json:"url"`
	UsedIn    []string `json:"used_in,omitempty" jsonschema:"preview rows whose image columns hold this url, as table#id.column; rows the owner edited in the admin panel's preview data are replaced on the next change, so copy the url into the seed rows or settings to keep it"`
	CreatedAt string   `json:"created_at"`
}

// RecentFiles lists an app's newest uploaded files (at most limit) and the
// preview rows that use each one, so an assistant can pick up photos the
// owner added in the admin panel.
func (p *Platform) RecentFiles(ctx context.Context, app string, limit int) ([]UploadedFile, error) {
	rows, err := p.db.Query(`SELECT b.hash, b.content_type, f.created_at FROM app_files f JOIN blobs b ON b.hash = f.hash
		WHERE f.app = ? ORDER BY f.created_at DESC, f.rowid DESC LIMIT ?`, app, limit)
	if err != nil {
		return nil, err
	}
	var out []UploadedFile
	index := map[string]int{}
	for rows.Next() {
		var b files.Blob
		var at string
		if err := rows.Scan(&b.Hash, &b.ContentType, &at); err != nil {
			rows.Close()
			return nil, err
		}
		index[fileURL(b)] = len(out)
		out = append(out, UploadedFile{URL: fileURL(b), CreatedAt: at})
	}
	rows.Close()
	if len(out) == 0 {
		return out, rows.Err()
	}
	st, err := p.loadStage(app, Preview)
	if err != nil || st == nil {
		return out, err
	}
	for _, t := range st.def.Tables {
		for _, c := range t.Columns {
			if c.Type != "image" {
				continue
			}
			found, err := st.db.Query(ctx, "SELECT id, "+quote(c.Name)+" AS v FROM "+quote(t.Name)+" WHERE "+quote(c.Name)+" LIKE ? LIMIT 200", []any{FilesPrefix + "%"})
			if err != nil {
				continue
			}
			for _, r := range found {
				v, _ := r["v"].(string)
				if i, ok := index[strings.SplitN(v, "?", 2)[0]]; ok && len(out[i].UsedIn) < 10 {
					out[i].UsedIn = append(out[i].UsedIn, fmt.Sprintf("%s#%v.%s", t.Name, r["id"], c.Name))
				}
			}
		}
	}
	return out, nil
}
