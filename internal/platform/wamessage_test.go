package platform_test

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/arif-rachim/renoxium/internal/appdef"
	"github.com/arif-rachim/renoxium/internal/platform"
	"github.com/arif-rachim/renoxium/internal/sandbox"
)

// whatsapp.send: a short notice to one number from a script, within the
// account's monthly WhatsApp messages and a daily limit per number; preview
// only logs it.
func TestScriptWhatsAppMessages(t *testing.T) {
	notices := &noticeBox{}
	p, err := platform.New(platform.Config{DataDir: t.TempDir(), AppsDomain: "renoxium.test", PublicScheme: "https",
		WhatsAppNotices: notices, OwnerPlan: func(string) string { return "free" }}, sandbox.NewGoja())
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	ctx := context.Background()
	p.CreateApp("usr_test", "clinic")
	res, err := p.ApplyChange(ctx, "clinic", &appdef.App{
		Scripts: map[string]string{"tell": `function handle(req) {
  try { const r = whatsapp.send({to: req.form.to, text: req.form.text}); return {text: 'sent ' + r.sent} }
  catch (e) { return {status: 400, text: String(e.message || e)} } }`},
		Templates: map[string]string{"t": "queue"},
		Routes:    []appdef.Route{{Method: "GET", Path: "/", Template: "t"}, {Method: "POST", Path: "/tell", Script: "tell"}}})
	if err != nil || !res.OK {
		t.Fatal(err, res)
	}
	p.SetAppKind("clinic", platform.KindWork)
	platform.PaidWorkApp(t, p, "clinic")
	if _, err := p.Publish("clinic", 0); err != nil {
		t.Fatal(err)
	}
	tell := func(to, text string) response {
		return do(t, p, "POST", "clinic.renoxium.test", "/tell", url.Values{"to": {to}, "text": {text}})
	}
	if r := tell("050 123 4567", "Two patients are\nahead of you (A012)."); r.status != 200 || r.body != "sent true" {
		t.Fatalf("send: %d %s", r.status, r.body)
	}
	deadline := time.Now().Add(2 * time.Second)
	for len(notices.list()) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := notices.list(); len(got) != 1 || got[0] != "+971501234567 clinic | Two patients are ahead of you (A012)." {
		t.Fatalf("notices = %q", got)
	}
	for _, bad := range [][2]string{{"hello", "x"}, {"+971501234567", ""}, {"+971501234567", strings.Repeat("a", 201)}} {
		if r := tell(bad[0], bad[1]); r.status != 400 {
			t.Errorf("whatsapp.send(%q, %d chars) = %d %s", bad[0], len(bad[1]), r.status, r.body)
		}
	}
	// One number gets at most WhatsAppPerRecipient messages a day.
	for i := 1; i < platform.WhatsAppPerRecipient; i++ {
		tell("+971501234567", "again")
	}
	if r := tell("+971501234567", "once more"); r.status != 400 || !strings.Contains(r.body, "too many WhatsApp messages to this number") {
		t.Fatalf("per number limit: %d %s", r.status, r.body)
	}
	// The monthly WhatsApp messages, shared with sign-in codes and owner notices.
	used, quota := p.WhatsAppUsage("usr_test")
	platform.UseWhatsApp(p, "usr_test", quota-used-1)
	if r := tell("+971550000001", "last one"); r.status != 200 {
		t.Fatalf("within the quota: %d %s", r.status, r.body)
	}
	if r := tell("+971550000002", "over"); r.status != 400 || !strings.Contains(r.body, "monthly WhatsApp messages") {
		t.Fatalf("quota answer: %d %s", r.status, r.body)
	}
	if used, quota := p.WhatsAppUsage("usr_test"); used != quota {
		t.Fatalf("used %d of %d", used, quota)
	}
	// Preview sends nothing.
	before := len(notices.list())
	out, _ := p.TestRequest(ctx, "clinic", platform.TestRequestInput{Method: "POST", Path: "/tell", Form: map[string]string{"to": "+971509999999", "text": "preview"}})
	time.Sleep(50 * time.Millisecond)
	if len(notices.list()) != before || !strings.Contains(fmt.Sprint(out), "sent false") {
		t.Fatalf("preview: %+v", out)
	}
}
