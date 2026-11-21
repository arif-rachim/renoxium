package platform

import (
	"errors"
	"fmt"
	"io"
	"net/http"
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
		return FileInfo{}, fmt.Errorf("storage is full: %d MB of %d MB used, and this file is %.1f MB; delete unused images or upgrade at %s/billing",
			used>>20, quota>>20, float64(b.Size)/(1<<20), p.cfg.SiteURL)
	}
	if _, err := p.db.Exec(`INSERT OR IGNORE INTO blobs (hash, size, content_type, created_at) VALUES (?, ?, ?, ?)`,
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
		if err == nil && (deferred || files.Deferred(f, b, width)) {
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
