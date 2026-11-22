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
	for _, want := range []string{"Orders, bookings and payments in one place", "You get paid first. Then we do.", "الطلبات والحجوزات والمدفوعات في مكان واحد", "connects it to AI assistants", "وأريد عربوناً بقيمة ٥٠ درهماً عند الحجز", "I sell products", "I take bookings", "I send quotes", "Try a demo", "AED 99", "AED 99", "Larger businesses", "Contact Renoxium", "2% of card payments", "No credits. No limit on changes.", "Unlimited building and changes", "Are there credits, or a limit on changes?", "Claude's free plan is enough", "Renoxium takes AED 20", "Your customers can ask their own AI assistant", "One place behind every channel", "Coming next", `role="tab"`, "Property brokers", "You stay in control", "What every shop and work app has", "Not selling online? Run your work on it", "Clinic queue", "Staff attendance", "Members' portal", "A work app: AED 99 a month per app, 5 staff included.", `class="paidfirst"`, `href="/privacy"`, `href="/terms"`} {
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
	// The home page in Arabic: right to left, the same sections, Arabic demos, and a way back to English.
	r, _ = http.Get(e.base + "/ar")
	ar := read(t, r)
	if r.StatusCode != 200 || r.Header.Get("Content-Language") != "ar" {
		t.Fatalf("/ar: %d %s", r.StatusCode, r.Header.Get("Content-Language"))
	}
	for _, want := range []string{`<html lang="ar" dir="rtl">`, "الطلبات والحجوزات والمدفوعات في مكان واحد", "بلا أرصدة. بلا حد للتعديلات.", "تطبيق عمل", "الخطة المجانية في Claude تكفي", "أبيع منتجات", "أستقبل حجوزات", "أرسل عروض أسعار",
		"يدفع لك عميلك أولاً، ثم تدفع لنا.", "عملاؤك يستطيعون سؤال مساعدهم الذكي", "مكان واحد خلف كل قناة", "الوسطاء العقاريون", "التحكم بيدك", "ما يتضمنه كل متجر وكل تطبيق عمل", "لا تبيع عبر الإنترنت؟ أدِر عملك عليه", "طابور العيادة", "حضور الموظفين", "بوابة الأعضاء", "تطبيق عمل: 99 درهماً شهرياً لكل تطبيق، و5 موظفين ضمن السعر.", `class="paidfirst"`, "99 درهماً", "بالاتفاق", "?lang=ar",
		"screen-booking-ar.webp", `hreflang="en"`, `href="/" lang="en"`, `href="/signup"`} {
		if !strings.Contains(ar, want) {
			t.Errorf("/ar lacks %q", want)
		}
	}
	// Both pages have the same sections and tabs, and point to each other.
	for _, id := range []string{`id="hero-h"`, `id="how-h"`, `id="ai-h"`, `id="chan-h"`, `id="panel-h"`, `id="trade-h"`, `id="work-h"`, `id="has-h"`, `id="pricing-h"`, `id="faq-h"`, `id="end-h"`} {
		if !strings.Contains(page, id) || !strings.Contains(ar, id) {
			t.Errorf("section %s is not on both home pages", id)
		}
	}
	for _, n := range []string{`role="tab"`, "<details>", `class="chan"`, "<dt>", "<li data-r>"} {
		if a, b := strings.Count(page, n), strings.Count(ar, n); a != b || a == 0 {
			t.Errorf("%s: %d in English, %d in Arabic", n, a, b)
		}
	}
	if !strings.Contains(page, `href="/ar" lang="ar"`) || !strings.Contains(page, `hreflang="ar"`) {
		t.Error("the English home page does not link to the Arabic one")
	}
	// The old wording that a paid assistant is needed is gone, and the connect page says which free plan works.
	if strings.Contains(page, "a paid plan with developer mode") || strings.Contains(ar, "خطة مدفوعة مع وضع المطوّر") {
		t.Error("the home page still says a paid assistant plan is needed")
	}
	r, _ = http.Get(e.base + "/connect")
	if p := read(t, r); !strings.Contains(p, "Renoxium has no credits") || !strings.Contains(p, "Claude&#39;s free plan") {
		t.Errorf("connect page lacks the no-credits or free-plan line")
	}
	// Signed in, / is still the dashboard.
	c := e.browser()
	e.signup(c, "abe@shop.ae")
	// Signed in, every account page has the same side menu with its own entry marked; signed out, Connect and Templates stay public pages.
	for path, title := range map[string]string{"/": "Your apps", "/templates": "Templates", "/billing": "Plan and billing", "/account": "Account settings", "/connect": "Connect and help"} {
		r, _ := c.Get(e.base + path)
		p := read(t, r)
		if r.StatusCode != 200 || !strings.Contains(p, `aria-label="Account"`) || !strings.Contains(p, `<a href="`+path+`" aria-current="page">`+title+`</a>`) {
			t.Errorf("%s signed in: %d, no side menu with %q marked", path, r.StatusCode, title)
		}
		if strings.Contains(p, `class="app"`) || strings.Contains(p, "\n.app{") {
			t.Errorf("%s: a card style named .app would also style the page body", path)
		}
	}
	for _, path := range []string{"/templates", "/connect"} {
		r, _ := http.Get(e.base + path)
		if p := read(t, r); r.StatusCode != 200 || strings.Contains(p, `aria-label="Account"`) {
			t.Errorf("%s signed out: %d, or it shows the account menu", path, r.StatusCode)
		}
	}
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
