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
	for _, want := range []string{"Your online store, built in a chat.", "AED 99", "AED 299", "2% of card payments", `href="/privacy"`, `href="/terms"`} {
		if !strings.Contains(page, want) {
			t.Errorf("landing lacks %q", want)
		}
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
