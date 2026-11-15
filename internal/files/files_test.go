package files

import (
	"bytes"
	"encoding/base64"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func pngBytes(t *testing.T, c color.Color) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	img.Set(1, 1, c)
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func countFiles(t *testing.T, dir string) int {
	t.Helper()
	n := 0
	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		rel, _ := filepath.Rel(dir, path)
		if err == nil && !info.IsDir() && !strings.HasPrefix(rel, "tmp"+string(filepath.Separator)) {
			n++
		}
		return nil
	})
	return n
}

func TestSameContentIsStoredOnce(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	red := pngBytes(t, color.RGBA{255, 0, 0, 255})
	a, existed, err := s.Put(bytes.NewReader(red))
	if err != nil || existed || a.ContentType != "image/png" || a.Ext() != "png" || a.Size != int64(len(red)) {
		t.Fatalf("first put: %+v %v %v", a, existed, err)
	}
	b, existed, err := s.Put(bytes.NewReader(red))
	if err != nil || !existed || b.Hash != a.Hash {
		t.Fatalf("second put: %+v %v %v", b, existed, err)
	}
	c, existed, _ := s.Put(bytes.NewReader(pngBytes(t, color.RGBA{0, 0, 255, 255})))
	if existed || c.Hash == a.Hash {
		t.Fatal("different content shared a hash")
	}
	if n := countFiles(t, dir); n != 2 {
		t.Fatalf("%d files on disk, want 2", n)
	}
	f, err := s.Open(a.Hash)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var got bytes.Buffer
	got.ReadFrom(f)
	if !bytes.Equal(got.Bytes(), red) {
		t.Fatal("stored content differs")
	}
}

func TestConcurrentUploadsOfSameContent(t *testing.T) {
	dir := t.TempDir()
	s, _ := Open(dir)
	data := pngBytes(t, color.RGBA{0, 255, 0, 255})
	var wg sync.WaitGroup
	hashes := make(chan string, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			b, _, err := s.Put(bytes.NewReader(data))
			if err != nil {
				t.Error(err)
			}
			hashes <- b.Hash
		}()
	}
	wg.Wait()
	close(hashes)
	first := <-hashes
	for h := range hashes {
		if h != first {
			t.Fatal("hashes differ")
		}
	}
	if n := countFiles(t, dir); n != 1 {
		t.Fatalf("%d files on disk, want 1", n)
	}
	if left, _ := os.ReadDir(filepath.Join(dir, "tmp")); len(left) != 0 {
		t.Fatalf("%d temporary files left", len(left))
	}
}

func TestOnlyImagesAreAccepted(t *testing.T) {
	s, _ := Open(t.TempDir())
	for name, data := range map[string]string{
		"html": "<!doctype html><script>alert(1)</script>",
		"svg":  `<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`,
		"text": "hello",
		"pdf":  "%PDF-1.7\n",
	} {
		if _, _, err := s.Put(strings.NewReader(data)); !errors.Is(err, ErrType) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	// A PNG with a misleading name or claimed type is still detected by content.
	if b, _, err := s.Put(bytes.NewReader(pngBytes(t, color.Black))); err != nil || b.ContentType != "image/png" {
		t.Fatalf("png: %+v %v", b, err)
	}
}

func TestTooLarge(t *testing.T) {
	s, _ := Open(t.TempDir())
	big := append(pngBytes(t, color.White), make([]byte, MaxSize)...)
	if _, _, err := s.Put(bytes.NewReader(big)); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err = %v", err)
	}
}

func TestDetectAVIF(t *testing.T) {
	head, _ := base64.StdEncoding.DecodeString("AAAAHGZ0eXBhdmlmAAAAAGF2aWZtaWYxbWlhZg==")
	if got := Detect(head); got != "image/avif" {
		t.Fatalf("got %s", got)
	}
}

func TestOpenRejectsBadHashes(t *testing.T) {
	s, _ := Open(t.TempDir())
	for _, h := range []string{"../../etc/passwd", "abc", strings.Repeat("g", 64)} {
		if _, err := s.Open(h); err == nil {
			t.Errorf("%q opened", h)
		}
	}
}
