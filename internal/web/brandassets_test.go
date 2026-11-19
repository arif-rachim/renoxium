package web_test

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestBrandAssets(t *testing.T) {
	e := newEnv(t)
	for path, ct := range map[string]string{"/favicon.ico": "image/x-icon", "/favicon.svg": "image/svg+xml", "/apple-touch-icon.png": "image/png",
		"/icon-512.png": "image/png", "/og-image.png": "image/png", "/brand/renoxium-logo.svg": "image/svg+xml", "/site.webmanifest": "application/manifest+json"} {
		r, err := http.Get(e.base + path)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(r.Body)
		r.Body.Close()
		if r.StatusCode != 200 || !strings.HasPrefix(r.Header.Get("Content-Type"), ct) || len(b) == 0 {
			t.Errorf("%s: %d %s (%d bytes)", path, r.StatusCode, r.Header.Get("Content-Type"), len(b))
		}
	}
	for _, path := range []string{"/brand/missing.png", "/brand/..%2fbrand.go"} {
		if r, _ := http.Get(e.base + path); r.StatusCode != 404 {
			t.Errorf("%s: %d", path, r.StatusCode)
		}
	}
	// Every page carries the mark, the icons and, on the public pages, the link preview.
	for _, path := range []string{"/", "/templates", "/login"} {
		r, _ := http.Get(e.base + path)
		page := read(t, r)
		if !strings.Contains(page, `class="mark"`) || !strings.Contains(page, `href="/favicon.svg"`) {
			t.Errorf("%s lacks the logo or icons", path)
		}
		if path != "/login" && !strings.Contains(page, `og:image" content="`+e.base+`/og-image.png"`) {
			t.Errorf("%s lacks the link preview image", path)
		}
	}
}
