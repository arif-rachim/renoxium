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
	for _, want := range []string{"Orders, bookings and payments in one place", "الطلبات والحجوزات والمدفوعات في مكان واحد", "connects it to AI assistants", "وأريد عربوناً بقيمة ٥٠ درهماً عند الحجز", "I sell products", "I take bookings", "I send quotes", "Try a demo", "AED 99", "AED 299", "2% of card payments", "costs AED 200 in Renoxium fees", "pass AED 4,950 a month", "Your customers can ask their own AI assistant", "One place behind every channel", "Coming next", `role="tab"`, "Property brokers", "You stay in control", `href="/privacy"`, `href="/terms"`} {
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

// Directory listings (ChatGPT apps) need a support page and a domain
// verification token served on /.well-known/openai-apps-challenge.
func TestSupportAndOpenAIChallenge(t *testing.T) {
	e := newEnv(t)
	r, _ := http.Get(e.base + "/.well-known/openai-apps-challenge")
	if read(t, r); r.StatusCode != 404 {
		t.Fatalf("challenge without token: %d", r.StatusCode)
	}
	e.site.OpenAIAppsChallenge = "tok_abc123"
	r, _ = http.Get(e.base + "/.well-known/openai-apps-challenge")
	if p := read(t, r); r.StatusCode != 200 || p != "tok_abc123" || !strings.HasPrefix(r.Header.Get("Content-Type"), "text/plain") {
		t.Fatalf("challenge: %d %q %s", r.StatusCode, p, r.Header.Get("Content-Type"))
	}
	e.site.Legal = web.Legal{Email: "help@renoxium.test"}
	r, _ = http.Get(e.base + "/support")
	p := read(t, r)
	for _, want := range []string{"Support", "mailto:help@renoxium.test", e.base + "/mcp", `href="/privacy"`, `href="/terms"`, `href="/connect"`} {
		if r.StatusCode != 200 || !strings.Contains(p, want) {
			t.Fatalf("support: %d, missing %q", r.StatusCode, want)
		}
	}
	r, _ = http.Get(e.base + "/")
	if !strings.Contains(read(t, r), `href="/support"`) {
		t.Fatal("no support link in the footer")
	}
}
