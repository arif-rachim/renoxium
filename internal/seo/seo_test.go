package seo

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestInjectKeepsThePagesOwnTags(t *testing.T) {
	page := `<!doctype html><html><head><title>Tea &amp; "Cake"</title><meta name="description" content="Own text">` +
		`<meta property="og:image" content="https://x.test/a.png"><link rel="canonical" href="https://x.test/own"></head><body></body></html>`
	out := string(Inject([]byte(page), Head{Canonical: "https://x.test/other", Description: "Fallback", Image: "https://x.test/b.png",
		SiteName: "X", Alternates: []Alternate{{Lang: "ar", URL: "https://x.test/?lang=ar"}}}))
	for _, want := range []string{`<meta property="og:title" content="Tea &amp; &#34;Cake&#34;">`, `<meta property="og:description" content="Own text">`,
		`<meta property="og:site_name" content="X">`, `<meta name="twitter:card" content="summary_large_image">`, `hreflang="ar"`} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %s in\n%s", want, out)
		}
	}
	for _, not := range []string{"Fallback", "b.png", "https://x.test/other\">\n", `rel="canonical" href="https://x.test/other"`} {
		if strings.Contains(out, not) {
			t.Errorf("%s replaced the page's own tag", not)
		}
	}
	if strings.Count(out, `rel="canonical"`) != 1 || strings.Count(out, "og:image") != 1 {
		t.Errorf("tags added twice:\n%s", out)
	}
}

func TestInjectLeavesFragmentsAndNoindexPages(t *testing.T) {
	for _, page := range []string{`<p>part of a page</p>`, `<html><head><meta name="robots" content="noindex"><title>x</title></head></html>`} {
		if out := string(Inject([]byte(page), Head{Canonical: "https://x.test/", Description: "d"})); out != page {
			t.Errorf("changed: %s", out)
		}
	}
}

func TestInjectStructuredDataCannotLeaveItsScript(t *testing.T) {
	out := string(Inject([]byte(`<html><head><title>x</title></head></html>`), Head{JSONLD: map[string]any{"name": "</script><script>alert(1)</script>"}}))
	if strings.Count(out, "</script>") != 1 {
		t.Fatalf("structured data ends its script element:\n%s", out)
	}
}

func TestCompress(t *testing.T) {
	big := strings.Repeat("<p>hello</p>", 500)
	h := Compress(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/page":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write([]byte(big))
		case "/small":
			http.Error(w, "not found", http.StatusNotFound)
		case "/nothing":
			w.WriteHeader(http.StatusNoContent)
		case "/image":
			w.Header().Set("Content-Type", "image/png")
			w.Write([]byte(big))
		case "/stream":
			w.Header().Set("Content-Type", "text/event-stream")
			w.Write([]byte("data: 1\n\n"))
			w.(http.Flusher).Flush()
		}
	}))
	get := func(path, accept string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", path, nil)
		r.Header.Set("Accept-Encoding", accept)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	w := get("/page", "gzip, br")
	if w.Header().Get("Content-Encoding") != "gzip" || w.Header().Get("Vary") != "Accept-Encoding" || w.Body.Len() >= len(big)/4 {
		t.Fatalf("page not compressed: %v, %d bytes", w.Header(), w.Body.Len())
	}
	zr, err := gzip.NewReader(w.Body)
	if err != nil {
		t.Fatal(err)
	}
	if plain, _ := io.ReadAll(zr); string(plain) != big {
		t.Fatal("the compressed page does not read back")
	}
	for path, accept := range map[string]string{"/page": "identity", "/small": "gzip", "/image": "gzip", "/stream": "gzip"} {
		if w := get(path, accept); w.Header().Get("Content-Encoding") != "" {
			t.Errorf("%s with %q was compressed", path, accept)
		}
	}
	if w := get("/small", "gzip"); w.Code != 404 || w.Body.String() != "not found\n" {
		t.Errorf("short answer: %d %q", w.Code, w.Body.String())
	}
	if w := get("/nothing", "gzip"); w.Code != 204 || w.Body.Len() != 0 {
		t.Errorf("empty answer: %d", w.Code)
	}
	if w := get("/stream", "gzip"); w.Body.String() != "data: 1\n\n" || !w.Flushed {
		t.Errorf("stream: %q flushed=%v", w.Body.String(), w.Flushed)
	}
}
