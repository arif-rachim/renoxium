package web_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/arif-rachim/renoxium/internal/web"
)

func TestLandingAndLegalPages(t *testing.T) {
	e := newEnv(t)
	r, _ := http.Get(e.base + "/")
	page := read(t, r)
	for _, want := range []string{"Take orders and bookings on your own website", "استقبل الطلبات والحجوزات", "I sell products", "I take bookings", "I send quotes", "Try a demo", "AED 99", "AED 299", "2% of card payments", `href="/privacy"`, `href="/terms"`} {
		if !strings.Contains(page, want) {
			t.Errorf("landing lacks %q", want)
		}
	}
	// The screenshots are served from the binary, cached for good under a versioned URL.
	i := strings.Index(page, "/static/img/")
	if i < 0 {
		t.Fatal("landing has no screenshots")
	}
	src := page[i : i+strings.IndexByte(page[i:], '"')]
	r, _ = http.Get(e.base + strings.ReplaceAll(src, "&amp;", "&"))
	if read(t, r); r.StatusCode != 200 || r.Header.Get("Content-Type") != "image/webp" || !strings.Contains(r.Header.Get("Cache-Control"), "immutable") {
		t.Fatalf("photo %s: %d %s %s", src, r.StatusCode, r.Header.Get("Content-Type"), r.Header.Get("Cache-Control"))
	}
	if r, _ := http.Get(e.base + "/static/img/nothing.webp"); r.StatusCode != 404 {
		t.Fatalf("missing photo: %d", r.StatusCode)
	}
	r, _ = http.Get(e.base + "/privacy")
	if p := read(t, r); r.StatusCode != 200 || !strings.Contains(p, "Draft.") || !strings.Contains(p, "Privacy Policy") {
		t.Fatalf("privacy: %d", r.StatusCode)
	}
	e.site.Legal = web.Legal{Company: "Renoxium (FZC)", Email: "legal@renoxium.test", Reviewed: true}
	r, _ = http.Get(e.base + "/terms")
	if p := read(t, r); strings.Contains(p, "Draft.") || !strings.Contains(p, "Renoxium (FZC)") || !strings.Contains(p, "mailto:legal@renoxium.test") {
		t.Fatalf("reviewed terms: %s", p)
	}
	// Signed in, / is still the dashboard.
	c := e.browser()
	e.signup(c, "abe@shop.ae")
	if r, _ := c.Get(e.base + "/"); !strings.Contains(read(t, r), "Your apps") {
		t.Fatal("dashboard for signed-in owners")
	}
}
