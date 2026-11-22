package platform_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/arif-rachim/renoxium/internal/appdef"
	"github.com/arif-rachim/renoxium/internal/applog"
	"github.com/arif-rachim/renoxium/internal/platform"
	"github.com/arif-rachim/renoxium/internal/sandbox"
	"github.com/arif-rachim/renoxium/internal/whatsapp"
)

// customerBox is a fake whatsapp.CustomerSender.
type customerBox struct {
	mu        sync.Mutex
	templates []sentTemplate
	texts     []string
	n         int
}

type sentTemplate struct {
	phone string
	m     whatsapp.Message
}

func (c *customerBox) SendTemplate(_ context.Context, phone string, m whatsapp.Message) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.n++
	c.templates = append(c.templates, sentTemplate{phone, m})
	return fmt.Sprintf("wamid.%d", c.n), nil
}

func (c *customerBox) SendText(_ context.Context, phone, text string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.texts = append(c.texts, phone+" "+text)
	return "wamid.text", nil
}

func (c *customerBox) sent() ([]sentTemplate, []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]sentTemplate(nil), c.templates...), append([]string(nil), c.texts...)
}

const waBookScript = `function handle(req) {
  const f = req.form;
  const c = f.rx_wa_optin ? whatsapp.consent({phone: f.phone}) : {consented: false, reason: 'not asked'};
  const c2 = f.also ? whatsapp.consent({phone: f.also}) : null;
  let s;
  try {
    s = whatsapp.send({to: f.phone, kind: f.kind || 'confirmation', ref: 'Booking #12', text: f.text || 'Haircut on Tue 2 Dec, 10:00', when: f.when, link: f.link || '/b/12'});
  } catch (e) { s = {error: String(e)}; }
  return {json: {consent: c, consent2: c2, send: s, status: whatsapp.status({phone: f.phone})}};
}`

func waApp(name string) *appdef.App {
	return &appdef.App{
		Tables: []appdef.Table{{Name: "business", Columns: []appdef.Column{{Name: "name", Type: "text"}}}},
		Seed:   map[string][]map[string]any{"business": {{"name": name}}},
		Brand:  &appdef.Brand{Table: "business", Name: "name"},
		Templates: map[string]string{
			"form": `<!doctype html><html><head></head><body><form method="post" action="/book"><input name="phone"><rx-whatsapp-optin></rx-whatsapp-optin><rx-whatsapp-optin lang="ar"></rx-whatsapp-optin></form></body></html>`,
		},
		Scripts: map[string]string{"book": waBookScript},
		Routes:  []appdef.Route{{Method: "GET", Path: "/", Template: "form"}, {Method: "POST", Path: "/book", Script: "book"}},
	}
}

func newWAPlatform(t *testing.T, plan string) (*platform.Platform, *customerBox) {
	t.Helper()
	box := &customerBox{}
	p, err := platform.New(platform.Config{DataDir: t.TempDir(), AppsDomain: "renoxium.test", PublicScheme: "https",
		WhatsAppCustomers: box, WhatsAppPhoneNumberID: "100200300",
		WhatsAppCustomerTemplates: map[string]string{"confirmation": "rx_confirmation", "reminder": "rx_reminder", "update": "rx_update"},
		OwnerPlan:                 func(string) string { return plan }}, sandbox.NewGoja())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	platform.ForgetPlan("usr_wa") // plans are cached per account across platforms
	return p, box
}

func setupWAApp(t *testing.T, p *platform.Platform, app, business string) {
	t.Helper()
	ctx := context.Background()
	if _, err := p.CreateApp("usr_wa", app); err != nil {
		t.Fatal(err)
	}
	if res, err := p.ApplyChange(ctx, app, waApp(business)); err != nil || len(res.Problems) > 0 {
		t.Fatalf("apply: %v %v", err, res.Problems)
	}
	if _, err := p.Publish(app, 0); err != nil {
		t.Fatal(err)
	}
}

var optinToken = regexp.MustCompile(`name="rx_wa_optin_token" value="([^"]+)"`)

func formToken(t *testing.T, p *platform.Platform, host string) string {
	t.Helper()
	res := do(t, p, "GET", host, "/", nil)
	m := optinToken.FindStringSubmatch(res.body)
	if res.status != 200 || m == nil {
		t.Fatalf("form page: %d %s", res.status, res.body)
	}
	return m[1]
}

type bookResult struct {
	Consent  map[string]any `json:"consent"`
	Consent2 map[string]any `json:"consent2"`
	Send     map[string]any `json:"send"`
	Status   map[string]any `json:"status"`
}

func book(t *testing.T, p *platform.Platform, host string, form url.Values) bookResult {
	t.Helper()
	res := do(t, p, "POST", host, "/book", form)
	var out bookResult
	if res.status != 200 || json.Unmarshal([]byte(res.body), &out) != nil {
		t.Fatalf("book: %d %s", res.status, res.body)
	}
	return out
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func webhook(t *testing.T, p *platform.Platform, phoneID string, messages ...map[string]any) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"object": "whatsapp_business_account", "entry": []any{map[string]any{"id": "waba", "changes": []any{
		map[string]any{"field": "messages", "value": map[string]any{"metadata": map[string]any{"phone_number_id": phoneID}, "messages": messages}}}}}})
	if err := p.HandleWhatsAppWebhook(body); err != nil {
		t.Fatal(err)
	}
	p.WaitBackground()
}

func TestWhatsAppCustomerMessages(t *testing.T) {
	p, box := newWAPlatform(t, "pro")
	setupWAApp(t, p, "oud", "Oud Al Noor")
	host := "oud.renoxium.test"

	page := do(t, p, "GET", host, "/", nil).body
	for _, want := range []string{`type="checkbox" name="rx_wa_optin" value="1"`, "on WhatsApp from Oud Al Noor (sent via Renoxium)", "على واتساب من Oud Al Noor"} {
		if !strings.Contains(page, want) {
			t.Errorf("opt-in box lacks %q:\n%s", want, page)
		}
	}
	if strings.Contains(page, "checked") || strings.Contains(page, "<rx-whatsapp-optin") {
		t.Errorf("box must be unticked and the marker replaced:\n%s", page)
	}

	// No box, a forged token, a ticked box without its token: no consent, nothing sent.
	if r := book(t, p, host, url.Values{"phone": {"050 123 4567"}}); r.Send["reason"] != "no_consent" || r.Send["sent"] != false {
		t.Fatalf("without consent: %+v", r)
	}
	for _, tok := range []string{"", "x.y", strings.Replace(formToken(t, p, host), ".", ".A", 1)} {
		if r := book(t, p, host, url.Values{"phone": {"0501234567"}, "rx_wa_optin": {"1"}, "rx_wa_optin_token": {tok}}); r.Consent["reason"] != "box_not_ticked" {
			t.Fatalf("token %q: %+v", tok, r.Consent)
		}
	}

	// A ticked box: consent for that number only, and the confirmation goes out.
	r := book(t, p, host, url.Values{"phone": {"0501234567"}, "rx_wa_optin": {"1"}, "rx_wa_optin_token": {formToken(t, p, host)}, "also": {"+971509999999"}})
	if r.Consent["consented"] != true || r.Consent2["reason"] != "one_number" || r.Send["sent"] != true || r.Status["consented"] != true {
		t.Fatalf("with consent: %+v", r)
	}
	p.WaitBackground()
	sent, _ := box.sent()
	if len(sent) != 1 {
		t.Fatalf("sent = %+v", sent)
	}
	id := r.Send["id"].(string)
	got := sent[0]
	if got.phone != "+971501234567" || got.m.Template != "rx_confirmation" || got.m.URLParam != id || got.m.StopPayload != "rx-stop:"+id ||
		strings.Join(got.m.Body, "|") != "Oud Al Noor|Booking #12|Haircut on Tue 2 Dec, 10:00" {
		t.Fatalf("template = %+v", got)
	}
	if to, ok := p.WhatsAppLink(id); !ok || to != "https://oud.renoxium.test/b/12" {
		t.Fatalf("link = %q %v", to, ok)
	}
	if _, ok := p.WhatsAppLink("nope"); ok {
		t.Error("unknown link resolved")
	}
	if used, quota := p.WhatsAppCustomerUsage("usr_wa"); used != 1 || quota != 500 {
		t.Errorf("usage = %d of %d", used, quota)
	}
	logs, _ := p.Logs("oud", applog.Filter{Limit: 200})
	for _, e := range logs {
		if strings.Contains(e.Message, "501234567") || strings.Contains(e.Message, "Haircut") {
			t.Errorf("live log shows the number or the text: %s", e.Message)
		}
	}

	// Bad arguments are errors, not refusals.
	for _, f := range []url.Values{{"kind": {"promo"}}, {"text": {"Pay at https://evil.example"}}, {"text": {"Call 050 123 4567 now"}}, {"text": {"Or +971 50 123 4567"}}, {"text": {"Mail sara@shop.ae"}},
		{"link": {"https://evil.example/x"}}, {"link": {"//evil.example"}}, {"kind": {"reminder"}}} {
		f.Set("phone", "0501234567")
		if r := book(t, p, host, f); !strings.Contains(fmt.Sprint(r.Send["error"]), "whatsapp.send") {
			t.Errorf("%v accepted: %+v", f, r.Send)
		}
	}

	// Dates and times are not phone numbers.
	if r := book(t, p, host, url.Values{"phone": {"0501234567"}, "text": {"Haircut on 2026-12-02 10:00 (2 Dec)"}}); r.Send["sent"] != true {
		t.Fatalf("a date refused: %+v", r.Send)
	}

	// The Stop button: from another number it does nothing; from the customer it stops the app's messages.
	stop := func(from, wamid string) map[string]any {
		return map[string]any{"from": from, "id": wamid, "type": "button", "button": map[string]any{"payload": "rx-stop:" + id, "text": "Stop messages"}}
	}
	webhook(t, p, "100200300", stop("971500000000", "in.1"))
	webhook(t, p, "999", stop("971501234567", "in.2")) // another business number
	if r := book(t, p, host, url.Values{"phone": {"0501234567"}}); r.Send["sent"] != true {
		t.Fatalf("stopped by a stranger: %+v", r.Send)
	}
	webhook(t, p, "100200300", stop("971501234567", "in.3"))
	webhook(t, p, "100200300", stop("971501234567", "in.3")) // delivered twice
	if r := book(t, p, host, url.Values{"phone": {"0501234567"}}); r.Send["reason"] != "opted_out" || r.Status["opted_out"] != true {
		t.Fatalf("after Stop: %+v", r)
	}
	_, texts := box.sent()
	if len(texts) != 1 || !strings.HasPrefix(texts[0], "+971501234567 You will not get more WhatsApp messages from Oud Al Noor") {
		t.Fatalf("replies = %q", texts)
	}
	// Ticking the box again is a new consent.
	r = book(t, p, host, url.Values{"phone": {"0501234567"}, "rx_wa_optin": {"1"}, "rx_wa_optin_token": {formToken(t, p, host)}})
	if r.Send["sent"] != true {
		t.Fatalf("after consenting again: %+v", r)
	}

	// Any other message: one answer a day that names the business.
	hello := func(wamid, text string) map[string]any {
		return map[string]any{"from": "971501234567", "id": wamid, "type": "text", "text": map[string]any{"body": text}}
	}
	webhook(t, p, "100200300", hello("in.4", "hi, is 10am free?"))
	webhook(t, p, "100200300", hello("in.5", "hello?"))
	_, texts = box.sent()
	if len(texts) != 2 || !strings.Contains(texts[1], "only sends updates from Oud Al Noor") || !strings.Contains(texts[1], "https://oud.renoxium.test") {
		t.Fatalf("answers = %q", texts)
	}
	// STOP as text stops every app that wrote to the number.
	webhook(t, p, "100200300", hello("in.6", "STOP"))
	if r := book(t, p, host, url.Values{"phone": {"0501234567"}}); r.Send["reason"] != "opted_out" {
		t.Fatalf("after STOP: %+v", r.Send)
	}
}

func TestWhatsAppCustomerPreviewAndLimits(t *testing.T) {
	p, box := newWAPlatform(t, "free")
	setupWAApp(t, p, "oud", "Oud Al Noor")
	ctx := context.Background()

	// Preview: the agent sees the message, nothing is sent.
	page, err := p.TestRequest(ctx, "oud", platform.TestRequestInput{Method: "GET", Path: "/"})
	if err != nil {
		t.Fatal(err)
	}
	tok := optinToken.FindStringSubmatch(page.Body)
	if tok == nil {
		t.Fatalf("preview page: %s", page.Body)
	}
	out, err := p.TestRequest(ctx, "oud", platform.TestRequestInput{Method: "POST", Path: "/book",
		Form: map[string]string{"phone": "0501234567", "rx_wa_optin": "1", "rx_wa_optin_token": tok[1], "kind": "reminder", "when": "Tue 2 Dec, 10:00"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.WhatsApp) != 1 || out.WhatsApp[0].Text != "Reminder from Oud Al Noor: Booking #12 on Tue 2 Dec, 10:00. Haircut on Tue 2 Dec, 10:00" || out.WhatsApp[0].Link != "/b/12" {
		t.Fatalf("preview whatsapp = %+v (%s)", out.WhatsApp, out.Body)
	}
	if !strings.Contains(out.Body, `"preview":true`) {
		t.Errorf("preview answer: %s", out.Body)
	}
	found := false
	for _, e := range out.Logs {
		found = found || strings.Contains(e.Message, "not sent from preview: WhatsApp reminder to +971501234567")
	}
	if !found {
		t.Errorf("preview log: %+v", out.Logs)
	}
	// A preview token does not work on live.
	if r := book(t, p, "oud.renoxium.test", url.Values{"phone": {"0501234567"}, "rx_wa_optin": {"1"}, "rx_wa_optin_token": {tok[1]}}); r.Consent["reason"] != "box_not_ticked" {
		t.Fatalf("preview token on live: %+v", r.Consent)
	}
	p.WaitBackground()
	if sent, _ := box.sent(); len(sent) != 0 {
		t.Fatalf("preview sent %+v", sent)
	}

	// Free plan: 3 messages a day to one number.
	host := "oud.renoxium.test"
	book(t, p, host, url.Values{"phone": {"0501234567"}, "rx_wa_optin": {"1"}, "rx_wa_optin_token": {formToken(t, p, host)}})
	for i := 0; i < 2; i++ {
		if r := book(t, p, host, url.Values{"phone": {"0501234567"}}); r.Send["sent"] != true {
			t.Fatalf("message %d: %+v", i+2, r.Send)
		}
	}
	if r := book(t, p, host, url.Values{"phone": {"0501234567"}}); r.Send["reason"] != "limit" {
		t.Fatalf("4th message: %+v", r.Send)
	}
}

func TestWhatsAppCustomerRefusals(t *testing.T) {
	p, box := newWAPlatform(t, "pro")
	setupWAApp(t, p, "nbd", "Emirates NBD Rewards")
	host := "nbd.renoxium.test"
	if r := book(t, p, host, url.Values{"phone": {"0501234567"}, "rx_wa_optin": {"1"}, "rx_wa_optin_token": {formToken(t, p, host)}}); r.Send["reason"] != "name_review" {
		t.Fatalf("bank-like name: %+v", r.Send)
	}

	// A server without customer templates: not_available, nothing sent.
	r, err := platform.New(platform.Config{DataDir: t.TempDir(), AppsDomain: "renoxium.test", PublicScheme: "https"}, sandbox.NewGoja())
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	setupWAApp(t, r, "oud", "Oud Al Noor")
	if res := book(t, r, "oud.renoxium.test", url.Values{"phone": {"0501234567"}, "rx_wa_optin": {"1"}, "rx_wa_optin_token": {formToken(t, r, "oud.renoxium.test")}}); res.Send["reason"] != "not_available" {
		t.Fatalf("not configured: %+v", res.Send)
	}
	if sent, _ := box.sent(); len(sent) != 0 {
		t.Fatalf("sent %+v", sent)
	}
}

func TestMaskPhone(t *testing.T) {
	if got := platform.MaskPhone("+971501234567"); got != "+9715…4567" {
		t.Errorf("mask = %q", got)
	}
}
