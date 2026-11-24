package web_test

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/arif-rachim/renoxium/internal/appdef"
)

// waBox records the WhatsApp codes the site sends.
type waBox struct {
	mu    sync.Mutex
	codes map[string]string
	sent  int
}

func (b *waBox) SendCode(_ context.Context, phone, code string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.codes == nil {
		b.codes = map[string]string{}
	}
	b.codes[phone] = code
	b.sent++
	return nil
}

func (b *waBox) code(phone string) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.codes[phone]
}

func (e *env) withWhatsApp() *waBox {
	b := &waBox{}
	e.site.WhatsApp = b
	return b
}

// waSignIn signs in with a WhatsApp code, creating the account on first use.
func (e *env) waSignIn(c *http.Client, wa *waBox, phone string) *http.Response {
	e.t.Helper()
	token := e.csrf(c, "/login/whatsapp")
	r, err := c.PostForm(e.base+"/login/whatsapp", url.Values{"csrf": {token}, "phone": {phone}, "next": {"/account"}})
	if err != nil {
		e.t.Fatal(err)
	}
	page := read(e.t, r)
	if r.StatusCode != 200 || !strings.Contains(page, "Enter your code") {
		e.t.Fatalf("send code: %d %s", r.StatusCode, page)
	}
	norm := "+" + strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, phone)
	r, err = c.PostForm(e.base+"/login/whatsapp/verify", url.Values{"csrf": {token}, "phone": {norm}, "code": {wa.code(norm)}, "next": {"/account"}})
	if err != nil {
		e.t.Fatal(err)
	}
	return r
}

func TestWhatsAppSignInIsHiddenWithoutSender(t *testing.T) {
	e := newEnv(t)
	c := e.browser()
	if r, _ := c.Get(e.base + "/login"); strings.Contains(read(t, r), "Continue with WhatsApp") {
		t.Fatal("WhatsApp offered on a server that cannot send codes")
	}
	if r, _ := c.Get(e.base + "/login/whatsapp"); r.StatusCode != http.StatusSeeOther {
		t.Fatalf("WhatsApp page without sender: %d", r.StatusCode)
	}
}

func TestOwnerSignsUpAndInWithWhatsApp(t *testing.T) {
	e := newEnv(t)
	wa := e.withWhatsApp()
	c := e.browser()
	for _, page := range []string{"/login", "/signup"} {
		if r, _ := c.Get(e.base + page); !strings.Contains(read(t, r), `href="/login/whatsapp?next=`) {
			t.Fatalf("%s does not offer WhatsApp", page)
		}
	}

	// A wrong code does not sign in.
	token := e.csrf(c, "/login/whatsapp")
	c.PostForm(e.base+"/login/whatsapp", url.Values{"csrf": {token}, "phone": {"+971 50 123 4567"}})
	r, _ := c.PostForm(e.base+"/login/whatsapp/verify", url.Values{"csrf": {token}, "phone": {"+971501234567"}, "code": {"999999x"}})
	if r.StatusCode != http.StatusUnauthorized || !strings.Contains(read(t, r), "not right") {
		t.Fatalf("wrong code: %d", r.StatusCode)
	}

	// The first right code creates the account, with no email.
	r = e.waSignIn(c, wa, "+971 50 123 4567")
	if r.StatusCode != http.StatusSeeOther || r.Header.Get("Location") != "/account" {
		t.Fatalf("verify: %d %s", r.StatusCode, read(t, r))
	}
	u, err := e.acc.UserByPhone("+971501234567")
	if err != nil || u.Email != "" || u.HasPassword {
		t.Fatalf("account: %+v %v", u, err)
	}
	r, _ = c.Get(e.base + "/account")
	page := read(t, r)
	for _, want := range []string{"971501234567", "Add email and password", "none yet"} {
		if !strings.Contains(page, want) {
			t.Fatalf("account page lacks %q", want)
		}
	}
	if strings.Contains(page, "Change password") {
		t.Fatal("password change offered to an account without password")
	}
	if r, _ := c.Get(e.base + "/"); strings.Contains(read(t, r), "Confirm your email.") {
		t.Fatal("asked to confirm an email the account does not have")
	}

	// Connecting an AI assistant shows the account by its number.
	_, challenge := pkce()
	r, _ = c.Get(e.base + e.authorizeURL(e.registerClient(), challenge))
	if page := read(t, r); r.StatusCode != 200 || !strings.Contains(page, "971501234567") {
		t.Fatalf("consent for a WhatsApp account: %d %s", r.StatusCode, page)
	}

	// Signing in again on another device finds the same account.
	other := e.browser()
	e.waSignIn(other, wa, "+971501234567")
	if r, _ := other.Get(e.base + "/account"); !strings.Contains(read(t, r), "971501234567") {
		t.Fatal("second sign-in did not reach the account")
	}

	// Adding an email and password lets the owner sign in with them too.
	token = e.csrf(c, "/account")
	r, _ = c.PostForm(e.base+"/account/email", url.Values{"csrf": {token}, "email": {"Omar@Shop.ae"}, "password": {"correct horse battery"}})
	if r.StatusCode != http.StatusSeeOther {
		t.Fatalf("add email: %d %s", r.StatusCode, read(t, r))
	}
	if m := e.mail.last(t, "omar@shop.ae", "verify_email"); m.To != "omar@shop.ae" || !strings.Contains(m.Text, "/verify?token=") {
		t.Fatalf("no confirmation email: %+v", m)
	}
	if _, err := e.acc.Authenticate("omar@shop.ae", "correct horse battery"); err != nil {
		t.Fatal(err)
	}
}

func TestEmailOwnerLinksWhatsApp(t *testing.T) {
	e := newEnv(t)
	wa := e.withWhatsApp()
	c := e.browser()
	e.signup(c, "lina@shop.ae")
	token := e.csrf(c, "/account")
	r, _ := c.PostForm(e.base+"/account/whatsapp", url.Values{"csrf": {token}, "action": {"send"}, "phone": {"+971 55 000 1111"}})
	if page := read(t, r); r.StatusCode != 200 || !strings.Contains(page, "Confirm number") {
		t.Fatalf("send: %d %s", r.StatusCode, page)
	}
	r, _ = c.PostForm(e.base+"/account/whatsapp", url.Values{"csrf": {token}, "action": {"confirm"}, "phone": {"+971550001111"}, "code": {wa.code("+971550001111")}})
	if r.StatusCode != http.StatusSeeOther {
		t.Fatalf("confirm: %d %s", r.StatusCode, read(t, r))
	}
	// Now a WhatsApp sign-in reaches the email account, not a new one.
	other := e.browser()
	e.waSignIn(other, wa, "+971550001111")
	if r, _ := other.Get(e.base + "/account"); !strings.Contains(read(t, r), "lina@shop.ae") {
		t.Fatal("WhatsApp sign-in did not reach the linked account")
	}

	// A number already linked cannot be taken by another account.
	sam := e.browser()
	e.signup(sam, "sam@shop.ae")
	token = e.csrf(sam, "/account")
	sam.PostForm(e.base+"/account/whatsapp", url.Values{"csrf": {token}, "action": {"send"}, "phone": {"+971550001111"}})
	r, _ = sam.PostForm(e.base+"/account/whatsapp", url.Values{"csrf": {token}, "action": {"confirm"}, "phone": {"+971550001111"}, "code": {wa.code("+971550001111")}})
	if r.StatusCode != http.StatusConflict {
		t.Fatalf("took a linked number: %d", r.StatusCode)
	}

	// A code sent for one account does not link the number to another.
	token = e.csrf(c, "/account")
	c.PostForm(e.base+"/account/whatsapp", url.Values{"csrf": {token}, "action": {"send"}, "phone": {"+971550002222"}})
	samToken := e.csrf(sam, "/account")
	r, _ = sam.PostForm(e.base+"/account/whatsapp", url.Values{"csrf": {samToken}, "action": {"confirm"}, "phone": {"+971550002222"}, "code": {wa.code("+971550002222")}})
	if r.StatusCode != http.StatusUnauthorized {
		t.Fatalf("used another account's code: %d", r.StatusCode)
	}

	// The email account can stop WhatsApp sign-in.
	r, _ = c.PostForm(e.base+"/account/whatsapp", url.Values{"csrf": {token}, "action": {"remove"}})
	if u, _ := e.acc.UserByEmail("lina@shop.ae"); r.StatusCode != http.StatusSeeOther || u.Phone != "" {
		t.Fatalf("remove: %d %+v", r.StatusCode, u)
	}
}

func TestWhatsAppCodesAreLimited(t *testing.T) {
	e := newEnv(t)
	wa := e.withWhatsApp()
	c := e.browser()
	token := e.csrf(c, "/login/whatsapp")
	last := 0
	for range 4 {
		r, _ := c.PostForm(e.base+"/login/whatsapp", url.Values{"csrf": {token}, "phone": {"+971501234567"}})
		read(t, r)
		last = r.StatusCode
	}
	if last != http.StatusTooManyRequests || wa.sent != 3 {
		t.Fatalf("codes to one number: %d sent, last answer %d", wa.sent, last)
	}
	r, _ := c.PostForm(e.base+"/login/whatsapp", url.Values{"csrf": {token}, "phone": {"12"}})
	if r.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad number: %d", r.StatusCode)
	}
}

func TestWhatsAppOwnerConfirmsWithCode(t *testing.T) {
	e := newEnv(t)
	wa := e.withWhatsApp()
	c := e.browser()
	e.waSignIn(c, wa, "+971501234567")
	def := &appdef.App{Templates: map[string]string{"t": "hi"}, Routes: []appdef.Route{{Method: "GET", Path: "/", Template: "t"}}}
	e.publishDef(c, "omar-shop", def)

	r, _ := c.Get(e.base + "/apps/omar-shop/delete")
	if page := read(t, r); !strings.Contains(page, "Send code to WhatsApp") || strings.Contains(page, `name="password"`) {
		t.Fatal("delete page does not confirm with WhatsApp")
	}
	token := e.csrf(c, "/apps/omar-shop/delete")
	// Without a code nothing is deleted.
	r, _ = c.PostForm(e.base+"/apps/omar-shop/delete", url.Values{"csrf": {token}, "confirm": {"omar-shop"}, "code": {"123"}})
	if r.StatusCode != http.StatusUnauthorized {
		t.Fatalf("deleted without code: %d", r.StatusCode)
	}
	r, _ = c.PostForm(e.base+"/apps/omar-shop/delete", url.Values{"csrf": {token}, "send_code": {"1"}})
	if page := read(t, r); r.StatusCode != 200 || !strings.Contains(page, "We sent a code") {
		t.Fatalf("send code: %d %s", r.StatusCode, page)
	}
	r, _ = c.PostForm(e.base+"/apps/omar-shop/delete", url.Values{"csrf": {token}, "confirm": {"omar-shop"}, "code": {wa.code("+971501234567")}})
	if page := read(t, r); r.StatusCode != 200 || !strings.Contains(page, "deleted") {
		t.Fatalf("delete with code: %d %s", r.StatusCode, page)
	}

	// Deleting the account: type the number, confirm with a code.
	token = e.csrf(c, "/account")
	c.PostForm(e.base+"/account/delete", url.Values{"csrf": {token}, "send_code": {"1"}})
	r, _ = c.PostForm(e.base+"/account/delete", url.Values{"csrf": {token}, "confirm": {"+971 50 123 4567"}, "code": {wa.code("+971501234567")}})
	if page := read(t, r); r.StatusCode != 200 || !strings.Contains(page, "Account deleted") {
		t.Fatalf("delete account: %d %s", r.StatusCode, page)
	}
	if _, err := e.acc.UserByPhone("+971501234567"); err == nil {
		t.Fatal("account still there")
	}
}
