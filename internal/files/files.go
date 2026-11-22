// Package files is a content-addressed file store on local disk. A file is
// stored once under the SHA-256 of its content, however many times and by
// however many apps it is uploaded.
package files

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
)

// MaxSize is the largest accepted file.
const MaxSize = 10 << 20

// Allowed maps accepted content types to the extension used in URLs.
var Allowed = map[string]string{
	"image/jpeg": "jpg",
	"image/png":  "png",
	"image/gif":  "gif",
	"image/webp": "webp",
	"image/avif": "avif",
}

var (
	// ErrTooLarge is returned for files over MaxSize.
	ErrTooLarge = fmt.Errorf("the file is larger than %d MB", MaxSize>>20)
	// ErrType is returned for content that is not an accepted image.
	ErrType = errors.New("only JPEG, PNG, GIF, WebP and AVIF images are accepted")

	hashRE = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// Store keeps files under dir/<hash[0:2]>/<hash[2:4]>/<hash>.
type Store struct {
	dir string
}

// Open prepares the store directory.
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(filepath.Join(dir, "tmp"), 0o755); err != nil {
		return nil, err
	}
	return &Store{dir: dir}, nil
}

// Blob describes stored content.
type Blob struct {
	Hash        string
	Size        int64
	ContentType string
}

// Ext is the URL extension for the blob's content type.
func (b Blob) Ext() string { return Allowed[b.ContentType] }

// ValidHash reports whether h looks like a hash produced by Put.
func ValidHash(h string) bool { return hashRE.MatchString(h) }

func (s *Store) path(hash string) string {
	return filepath.Join(s.dir, hash[0:2], hash[2:4], hash)
}

// Put stores the content of r unless identical content is already stored.
// The content type is detected from the bytes, never taken from the caller.
func (s *Store) Put(r io.Reader) (b Blob, existed bool, err error) {
	tmp, err := os.CreateTemp(filepath.Join(s.dir, "tmp"), "upload-*")
	if err != nil {
		return Blob{}, false, err
	}
	defer os.Remove(tmp.Name()) // no-op once renamed
	defer tmp.Close()

	h := sha256.New()
	var head bytes.Buffer
	n, err := io.Copy(io.MultiWriter(tmp, h, &limitedBuffer{buf: &head, max: 512}), io.LimitReader(r, MaxSize+1))
	if err != nil {
		return Blob{}, false, err
	}
	if n > MaxSize {
		return Blob{}, false, ErrTooLarge
	}
	ctype := Detect(head.Bytes())
	if _, ok := Allowed[ctype]; !ok {
		return Blob{}, false, ErrType
	}
	b = Blob{Hash: hex.EncodeToString(h.Sum(nil)), Size: n, ContentType: ctype}

	final := s.path(b.Hash)
	if _, err := os.Stat(final); err == nil {
		return b, true, nil
	}
	if err := tmp.Sync(); err != nil {
		return Blob{}, false, err
	}
	if err := tmp.Close(); err != nil {
		return Blob{}, false, err
	}
	if err := os.MkdirAll(filepath.Dir(final), 0o755); err != nil {
		return Blob{}, false, err
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return Blob{}, false, err
	}
	// Rename is atomic; two uploads of the same content end with the same file.
	if err := os.Rename(tmp.Name(), final); err != nil {
		return Blob{}, false, err
	}
	return b, false, nil
}

// Open returns the stored content for a hash.
func (s *Store) Open(hash string) (*os.File, error) {
	if !ValidHash(hash) {
		return nil, os.ErrNotExist
	}
	return os.Open(s.path(hash))
}

// Detect identifies the content type from the first bytes of a file.
func Detect(head []byte) string {
	// net/http does not know AVIF: an ISO-BMFF "ftyp" box with an avif brand.
	if len(head) >= 12 && string(head[4:8]) == "ftyp" && (string(head[8:12]) == "avif" || string(head[8:12]) == "avis") {
		return "image/avif"
	}
	return http.DetectContentType(head)
}

// limitedBuffer keeps only the first max bytes written to it.
type limitedBuffer struct {
	buf *bytes.Buffer
	max int
}

func (l *limitedBuffer) Write(p []byte) (int, error) {
	if room := l.max - l.buf.Len(); room > 0 {
		if len(p) < room {
			room = len(p)
		}
		l.buf.Write(p[:room])
	}
	return len(p), nil
}

// PutAs stores the content of r under a given hash instead of the hash of
// its bytes. It is for an app moved from another server: the app's rows
// hold its files' addresses, which name the hash they had there (a photo
// shrunk in place keeps the address of the file it replaced). The content
// type is detected from the bytes, as in Put.
func (s *Store) PutAs(hash string, r io.Reader) (Blob, error) {
	if !ValidHash(hash) {
		return Blob{}, errors.New("not a file address")
	}
	tmp, err := os.CreateTemp(filepath.Join(s.dir, "tmp"), "import-*")
	if err != nil {
		return Blob{}, err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	var head bytes.Buffer
	n, err := io.Copy(io.MultiWriter(tmp, &limitedBuffer{buf: &head, max: 512}), io.LimitReader(r, MaxSize+1))
	if err != nil {
		return Blob{}, err
	}
	if n > MaxSize {
		return Blob{}, ErrTooLarge
	}
	ctype := Detect(head.Bytes())
	if _, ok := Allowed[ctype]; !ok {
		return Blob{}, ErrType
	}
	b := Blob{Hash: hash, Size: n, ContentType: ctype}
	final := s.path(hash)
	if err := tmp.Sync(); err != nil {
		return Blob{}, err
	}
	if err := tmp.Close(); err != nil {
		return Blob{}, err
	}
	if err := os.MkdirAll(filepath.Dir(final), 0o755); err != nil {
		return Blob{}, err
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return Blob{}, err
	}
	return b, os.Rename(tmp.Name(), final)
}
