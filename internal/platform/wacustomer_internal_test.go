package platform

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/arif-rachim/renoxium/internal/appdef"
	"github.com/arif-rachim/renoxium/internal/sandbox"
	"github.com/arif-rachim/renoxium/internal/whatsapp"
)

func newWAInternal(t *testing.T) *Platform {
	t.Helper()
	p, err := New(Config{DataDir: t.TempDir(), AppsDomain: "renoxium.test", PublicScheme: "https"}, sandbox.NewGoja())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	ctx := context.Background()
	p.CreateApp("usr_wa", "oud")
	if _, err := p.ApplyChange(ctx, "oud", &appdef.App{Templates: map[string]string{"t": "x"}, Routes: []appdef.Route{{Method: "GET", Path: "/", Template: "t"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Publish("oud", 0); err != nil {
		t.Fatal(err)
	}
	return p
}

func addSent(t *testing.T, p *Platform, n int) {
	t.Helper()
	now := time.Now().Unix()
	for i := 0; i < n; i++ {
		if _, err := p.db.Exec(`INSERT INTO wa_messages (id, app, stage, phone, kind, status, created_at, updated_at) VALUES (?, 'oud', 'live', ?, 'confirmation', 'delivered', ?, ?)`,
			fmt.Sprintf("m%d-%d", n, i), fmt.Sprintf("+97150%07d", i), now, now); err != nil {
			t.Fatal(err)
		}
	}
}

// Customers stopping an app's messages often pause it; a few opt-outs in
// a small number of messages do not.
func TestWhatsAppPauseOnOptOuts(t *testing.T) {
	p := newWAInternal(t)
	ctx := context.Background()
	addSent(t, p, 40)
	for i := 0; i < 10; i++ { // 25%, but fewer than 50 messages
		p.optOut(ctx, "oud", Live, fmt.Sprintf("+97150%07d", i), "test")
	}
	if r := p.WhatsAppPaused("oud"); r != "" {
		t.Fatalf("paused after 40 messages: %s", r)
	}
	p.db.Exec(`DELETE FROM wa_optouts`)
	addSent(t, p, 20) // 60 in all
	for i := 0; i < 3; i++ { // 5% exactly: not more than the limit
		p.optOut(ctx, "oud", Live, fmt.Sprintf("+97150%07d", i), "test")
	}
	if r := p.WhatsAppPaused("oud"); r != "" {
		t.Fatalf("paused at 5%%: %s", r)
	}
	p.optOut(ctx, "oud", Live, "+971500000003", "test")
	if r := p.WhatsAppPaused("oud"); !strings.Contains(r, "4 of 60") {
		t.Fatalf("not paused at 4 of 60: %q", r)
	}
	if got := p.PausedWhatsApp(); got["oud"] == "" {
		t.Fatalf("operator list = %v", got)
	}
	if err := p.ResumeWhatsApp("oud"); err != nil || p.WhatsAppPaused("oud") != "" {
		t.Fatal("resume failed")
	}
}

// Statuses arrive out of order: a late "sent" never replaces "read".
func TestWhatsAppStatusOrder(t *testing.T) {
	p := newWAInternal(t)
	now := time.Now().Unix()
	p.db.Exec(`INSERT INTO wa_messages (id, app, stage, phone, kind, wamid, status, created_at, updated_at) VALUES ('m1', 'oud', 'live', '+971501234567', 'update', 'wamid.1', 'sent', ?, ?)`, now, now)
	st := func(status, category string) {
		s := whatsapp.Status{ID: "wamid.1", Status: status}
		s.Pricing.Category, s.Pricing.Type = category, "regular"
		p.waStatus(s)
	}
	st("read", "utility")
	st("delivered", "")
	var status, category, pricing string
	p.db.QueryRow(`SELECT status, category, pricing FROM wa_messages WHERE id = 'm1'`).Scan(&status, &category, &pricing)
	if status != "read" || category != "utility" || pricing != "regular" {
		t.Fatalf("status = %s %s %s", status, category, pricing)
	}
}

func TestOptinToken(t *testing.T) {
	p := newWAInternal(t)
	live, _ := p.loadStage("oud", Live)
	preview, _ := p.loadStage("oud", Preview)
	now := time.Now()
	tok := p.optinToken(live, now)
	switch {
	case !p.validOptinToken(live, tok, now.Add(47*time.Hour)):
		t.Error("fresh token refused")
	case p.validOptinToken(live, tok, now.Add(49*time.Hour)):
		t.Error("expired token accepted")
	case p.validOptinToken(preview, tok, now):
		t.Error("live token accepted on preview")
	case p.validOptinToken(live, p.optinToken(live, now.Add(time.Hour)), now):
		t.Error("token from the future accepted")
	}
}

func TestOptinBoxEscapesName(t *testing.T) {
	p := newWAInternal(t)
	live, _ := p.loadStage("oud", Live)
	live.def.Brand = nil
	out := string(p.injectOptin(live, []byte(`<p><rx-whatsapp-optin/></p>`)))
	if !strings.Contains(out, "from oud (sent via Renoxium)") || strings.Contains(out, "rx-whatsapp-optin/") {
		t.Fatalf("box = %s", out)
	}
	if got := optinWording("<b>x</b>", "en"); !strings.Contains(got, "<b>") {
		t.Fatal("wording is plain text; injectOptin escapes it")
	}
}
