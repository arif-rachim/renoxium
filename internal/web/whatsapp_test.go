package web_test

import (
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/arif-rachim/renoxium/internal/accounts"
	"github.com/arif-rachim/renoxium/internal/platform"
	"github.com/arif-rachim/renoxium/internal/sandbox"
	"github.com/arif-rachim/renoxium/internal/web"
	"github.com/arif-rachim/renoxium/internal/whatsapp"
)

// The WhatsApp webhook reads nothing before the signature is checked, and
// answers Meta's set-up check only with the verify token.
func TestWhatsAppWebhookEndpoint(t *testing.T) {
	dir := t.TempDir()
	p, _ := platform.New(platform.Config{DataDir: dir, AppsDomain: "renoxium.test", PublicScheme: "https"}, sandbox.NewGoja())
	acc, _ := accounts.Open(filepath.Join(dir, "accounts.db"))
	defer p.Close()
	defer acc.Close()
	site := web.New("https://renoxium.test", acc, p)

	post := func(body, sig string) int {
		r := httptest.NewRequest("POST", "https://renoxium.test/whatsapp/webhook", strings.NewReader(body))
		if sig != "" {
			r.Header.Set(whatsapp.SignatureHeader, sig)
		}
		w := httptest.NewRecorder()
		site.ServeHTTP(w, r)
		return w.Code
	}
	get := func(q string) (int, string) {
		w := httptest.NewRecorder()
		site.ServeHTTP(w, httptest.NewRequest("GET", "https://renoxium.test/whatsapp/webhook?"+q, nil))
		return w.Code, w.Body.String()
	}
	body := `{"object":"whatsapp_business_account","entry":[]}`
	if c := post(body, whatsapp.Sign("s3cret", []byte(body))); c != 503 {
		t.Errorf("not configured: %d", c)
	}
	if c, _ := get("hub.mode=subscribe&hub.verify_token=x&hub.challenge=42"); c != 403 {
		t.Errorf("verify without a token set: %d", c)
	}

	site.WhatsAppAppSecret, site.WhatsAppVerifyToken = "s3cret", "verify-me"
	for _, sig := range []string{"", "sha256=00", whatsapp.Sign("other", []byte(body))} {
		if c := post(body, sig); c != 401 {
			t.Errorf("signature %q: %d", sig, c)
		}
	}
	if c := post(body, whatsapp.Sign("s3cret", []byte(body))); c != 200 {
		t.Errorf("signed: %d", c)
	}
	if c := post("not json", whatsapp.Sign("s3cret", []byte("not json"))); c != 200 {
		t.Errorf("signed but unreadable: %d (Meta must not retry it)", c)
	}
	if c, out := get("hub.mode=subscribe&hub.verify_token=verify-me&hub.challenge=42"); c != 200 || out != "42" {
		t.Errorf("verify: %d %q", c, out)
	}
	if c, _ := get("hub.mode=subscribe&hub.verify_token=wrong&hub.challenge=42"); c != 403 {
		t.Errorf("wrong verify token: %d", c)
	}
	w := httptest.NewRecorder()
	site.ServeHTTP(w, httptest.NewRequest("GET", "https://renoxium.test/w/unknown", nil))
	if w.Code != 404 {
		t.Errorf("unknown link: %d", w.Code)
	}
}
