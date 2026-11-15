package web_test

import (
	"context"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/arif-rachim/renoxium/internal/email"
)

type mailbox struct {
	mu   sync.Mutex
	sent []email.Message
}

func (m *mailbox) Send(_ context.Context, msg email.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sent = append(m.sent, msg)
	return nil
}

func (m *mailbox) last(t *testing.T, to, tag string) email.Message {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := len(m.sent) - 1; i >= 0; i-- {
		if m.sent[i].To == to && m.sent[i].Tag == tag {
			return m.sent[i]
		}
	}
	t.Fatalf("no %s email to %s", tag, to)
	return email.Message{}
}

func (m *mailbox) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.sent)
}

var linkRE = regexp.MustCompile(`https?://\S+token=\S+`)

// path turns an emailed absolute link into a path on the test server.
func (e *env) path(link string) string {
	return strings.TrimPrefix(link, e.base)
}

func TestSignupSendsVerificationEmail(t *testing.T) {
	e := newEnv(t)
	c := e.browser()
	e.signup(c, "mona@shop.ae")
	msg := e.mail.last(t, "mona@shop.ae", "verify_email")
	link := linkRE.FindString(msg.Text)
	if link == "" || !strings.Contains(msg.HTML, link) {
		t.Fatalf("email: %+v", msg)
	}
	r, _ := c.Get(e.base + "/")
	if page := read(t, r); !strings.Contains(page, "Confirm your email") {
		t.Fatal("dashboard does not ask to confirm the email")
	}
	r, _ = http.Get(link)
	if page := read(t, r); r.StatusCode != 200 || !strings.Contains(page, "Email confirmed") {
		t.Fatalf("verify: %d", r.StatusCode)
	}
	if r.Header.Get("Referrer-Policy") != "no-referrer" {
		t.Fatal("token page leaks referrer")
	}
	r, _ = c.Get(e.base + "/")
	if page := read(t, r); strings.Contains(page, "Confirm your email") {
		t.Fatal("dashboard still asks to confirm")
	}
	if r, _ = http.Get(link); r.StatusCode != 400 {
		t.Fatalf("link used twice: %d", r.StatusCode)
	}
}

func TestPasswordReset(t *testing.T) {
	e := newEnv(t)
	old := e.browser()
	e.signup(old, "nora@shop.ae")
	agentToken := e.connect(old, e.registerClient()).AccessToken

	ask := func(addr string) string {
		t.Helper()
		c := e.browser()
		token := e.csrf(c, "/forgot")
		r, _ := c.PostForm(e.base+"/forgot", url.Values{"csrf": {token}, "email": {addr}})
		return read(t, r)
	}
	before := e.mail.count()
	unknown := ask("nobody@shop.ae")
	if e.mail.count() != before {
		t.Fatal("email sent for an unknown address")
	}
	known := ask("nora@shop.ae")
	if strings.Replace(unknown, "nobody@shop.ae", "X", 1) != strings.Replace(known, "nora@shop.ae", "X", 1) {
		t.Fatal("the answer reveals whether an account exists")
	}
	link := linkRE.FindString(e.mail.last(t, "nora@shop.ae", "password_reset").Text)

	// Opening the link (or a mail scanner prefetching it) does not use it.
	c := e.browser()
	r, _ := c.Get(link)
	r, _ = c.Get(link)
	page := read(t, r)
	if r.StatusCode != 200 || !strings.Contains(page, "Choose a new password") || r.Header.Get("Referrer-Policy") != "no-referrer" {
		t.Fatalf("reset form: %d", r.StatusCode)
	}
	form := url.Values{"csrf": {csrfRE.FindStringSubmatch(page)[1]}, "token": {strings.Split(link, "token=")[1]}}
	form.Set("password", "short")
	r, _ = c.PostForm(e.base+"/reset", form)
	if page := read(t, r); r.StatusCode != 400 || !strings.Contains(page, "at least 10") {
		t.Fatalf("short password: %d", r.StatusCode)
	}
	form.Set("password", "a brand new password")
	r, _ = c.PostForm(e.base+"/reset", form)
	if r.StatusCode != http.StatusSeeOther || r.Header.Get("Location") != "/" {
		t.Fatalf("reset: %d", r.StatusCode)
	}
	if page := func() string { r, _ := c.Get(e.base + "/"); return read(t, r) }(); !strings.Contains(page, "nora@shop.ae") || strings.Contains(page, "Confirm your email") {
		t.Fatal("not signed in as a verified user after reset")
	}

	// Everything signed in before is signed out.
	r, _ = old.Get(e.base + "/")
	if page := read(t, r); strings.Contains(page, "nora@shop.ae") {
		t.Fatal("old browser session survived the reset")
	}
	req, _ := http.NewRequest("POST", e.base+"/mcp", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer "+agentToken)
	if r, _ := http.DefaultClient.Do(req); r.StatusCode != 401 {
		t.Fatalf("agent token survived the reset: %d", r.StatusCode)
	}
	// The link works once; the old password is gone.
	if r, _ := c.PostForm(e.base+"/reset", form); r.StatusCode != 400 {
		t.Fatalf("link reused: %d", r.StatusCode)
	}
	login := func(pw string) int {
		b := e.browser()
		token := e.csrf(b, "/login")
		r, _ := b.PostForm(e.base+"/login", url.Values{"csrf": {token}, "email": {"nora@shop.ae"}, "password": {pw}})
		return r.StatusCode
	}
	if login("correct horse battery") != 401 || login("a brand new password") != http.StatusSeeOther {
		t.Fatal("password not changed")
	}
}

func TestResetEmailsAreRateLimited(t *testing.T) {
	e := newEnv(t)
	e.signup(e.browser(), "olga@shop.ae")
	c := e.browser()
	token := e.csrf(c, "/forgot")
	codes := []int{}
	for i := 0; i < 4; i++ {
		r, _ := c.PostForm(e.base+"/forgot", url.Values{"csrf": {token}, "email": {"olga@shop.ae"}})
		read(t, r)
		codes = append(codes, r.StatusCode)
	}
	if codes[2] != 200 || codes[3] != 429 {
		t.Fatalf("codes = %v", codes)
	}
}
