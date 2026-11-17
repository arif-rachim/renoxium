package platform_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/arif-rachim/renoxium/internal/appdef"
	"github.com/arif-rachim/renoxium/internal/platform"
	"github.com/arif-rachim/renoxium/internal/sandbox"
)

type noticeBox struct {
	mu      sync.Mutex
	notices []string
}

func (n *noticeBox) SendNotice(_ context.Context, phone string, texts ...string) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.notices = append(n.notices, phone+" "+strings.Join(texts, " | "))
	return nil
}

func (n *noticeBox) list() []string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]string(nil), n.notices...)
}

func TestOwnerWhatsAppNotices(t *testing.T) {
	codes := &codeBox{codes: map[string]string{}}
	notices := &noticeBox{}
	p, err := platform.New(platform.Config{DataDir: t.TempDir(), AppsDomain: "renoxium.test", PublicScheme: "https",
		WhatsApp: codes, WhatsAppNotices: notices}, sandbox.NewGoja())
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	ctx := context.Background()
	p.CreateApp("usr_test", "oud")
	p.ApplyChange(ctx, "oud", &appdef.App{
		Scripts:   map[string]string{"order": `function handle(req) { notify.owner({subject: 'New order #7: AED 120', text: 'line one\nline two'}); return {text: 'ok'} }`},
		Templates: map[string]string{"t": "shop"},
		Routes:    []appdef.Route{{Method: "GET", Path: "/", Template: "t"}, {Method: "POST", Path: "/order", Script: "order"}}})
	p.Publish("oud", 0)

	if err := p.StartAlertPhone(ctx, "oud", "not a number"); err == nil {
		t.Error("bad number accepted")
	}
	if err := p.StartAlertPhone(ctx, "oud", "050 123 4567"); err != nil {
		t.Fatal(err)
	}
	if a := p.AlertPhoneOf("oud"); a == nil || a.Phone != "+971501234567" || a.Confirmed {
		t.Fatalf("pending phone = %+v", a)
	}
	// Not confirmed yet: no notice.
	do(t, p, "POST", "oud.renoxium.test", "/order", nil)
	if err := p.ConfirmAlertPhone("oud", "000000x"); err == nil {
		t.Error("wrong code accepted")
	}
	if err := p.ConfirmAlertPhone("oud", codes.codes["+971501234567"]); err != nil {
		t.Fatal(err)
	}
	if res := do(t, p, "POST", "oud.renoxium.test", "/order", nil); res.status != 200 {
		t.Fatalf("order: %d %s", res.status, res.body)
	}
	deadline := time.Now().Add(2 * time.Second)
	for len(notices.list()) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := notices.list(); len(got) != 1 || got[0] != "+971501234567 oud | New order #7: AED 120" {
		t.Fatalf("notices = %q", got)
	}
	// Preview never sends.
	p.TestRequest(ctx, "oud", platform.TestRequestInput{Method: "POST", Path: "/order"})
	if err := p.RemoveAlertPhone("oud"); err != nil || p.AlertPhoneOf("oud") != nil {
		t.Fatal("remove failed")
	}
	do(t, p, "POST", "oud.renoxium.test", "/order", nil)
	time.Sleep(50 * time.Millisecond)
	if got := notices.list(); len(got) != 1 {
		t.Fatalf("notices after preview and removal = %q", got)
	}
}
