package platform_test

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/arif-rachim/renoxium/internal/appdef"
	"github.com/arif-rachim/renoxium/internal/applog"
	"github.com/arif-rachim/renoxium/internal/platform"
	"github.com/arif-rachim/renoxium/internal/sandbox"
)

func mailApp() *appdef.App {
	def := membersApp()
	def.Scripts["mail"] = `function handle(req) { try { return {json: email.send({to: req.form.to, subject: "Offer", text: "Hello"})} } catch (e) { return {text: String(e), status: 429} } }`
	def.Routes = append(def.Routes, appdef.Route{Method: "POST", Path: "/mail", Script: "mail"})
	return def
}

// Scripts send email from Renoxium's address. An account gets a daily
// number by plan over all its apps, and that budget is separate from the
// one sign-in emails use, so nobody's scripts can stop other apps'
// confirmation and reset emails.
func TestScriptEmailBudgetPerAccount(t *testing.T) {
	defer func(n int) { platform.AppEmailsPerPlan["member"] = n }(platform.AppEmailsPerPlan["member"])
	platform.AppEmailsPerPlan["member"] = 3
	box := &outbox{}
	p, err := platform.New(platform.Config{DataDir: t.TempDir(), AppsDomain: "renoxium.test", PublicScheme: "https", Mailer: box,
		OwnerPlan:  func(string) string { return "member" },
		OwnerEmail: func(string) (string, bool) { return "owner@club.ae", true },
		Daily:      platform.DailyLimits{Emails: 4}}, sandbox.NewGoja())
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	for _, app := range []string{"club", "club-two"} {
		p.CreateApp("usr_sender", app)
		p.ApplyChange(context.Background(), app, mailApp())
		p.SetAppKind(app, platform.KindShop) // a member chooses how each app is paid for, too
		if _, err := p.Publish(app, 0); err != nil {
			t.Fatal(err)
		}
	}
	send := func(app string, i int) int {
		return do(t, p, "POST", app+".renoxium.test", "/mail", url.Values{"to": {fmt.Sprintf("someone%d@example.com", i)}}).status
	}
	for i := 0; i < 3; i++ {
		if c := send([]string{"club", "club-two"}[i%2], i); c != 200 {
			t.Fatalf("email %d: %d", i, c)
		}
	}
	// The fourth is over the account's number, whichever app sends it, and
	// the script is told.
	for _, app := range []string{"club", "club-two"} {
		if r := do(t, p, "POST", app+".renoxium.test", "/mail", url.Values{"to": {"late@example.com"}}); r.status != 429 || !strings.Contains(r.body, "daily email limit") {
			t.Fatalf("over the limit on %s: %d %s", app, r.status, r.body)
		}
	}
	// Sign-in emails still go out: they have their own budget (4 a day here).
	v := newVisitor(t, p, "club.renoxium.test")
	v.submit("/_rx/auth/signup", "/_rx/auth/signup", url.Values{"email": {"new@club.ae"}, "password": {"correct horse battery"}})
	if m := box.last(t, "visitor_verify"); m.To != "new@club.ae" {
		t.Fatalf("confirmation email: %+v", m)
	}
	box.mu.Lock()
	sent := len(box.sent)
	box.mu.Unlock()
	if sent != 4 {
		t.Fatalf("%d emails sent, want 3 from scripts and 1 confirmation", sent)
	}
}

// Preview sends real requests from Renoxium's address too, so it counts
// towards the same monthly number as live.
func TestPreviewFetchCountsTowardsTheQuota(t *testing.T) {
	defer func(n int) { platform.FetchCallsPerPlan["free"] = n }(platform.FetchCallsPerPlan["free"])
	platform.FetchCallsPerPlan["free"] = 3
	_, tr := fakeAPI(t)
	p, err := platform.New(platform.Config{DataDir: t.TempDir(), AppsDomain: "renoxium.test", PublicScheme: "https", SiteURL: "https://renoxium.test",
		FetchTransport: tr, OwnerPlan: func(string) string { return "free" }}, sandbox.NewGoja())
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	ctx := context.Background()
	p.CreateApp("usr_test", "shop")
	p.ApplyChange(ctx, "shop", fetchApp("api.courier.ae"))
	p.AllowHost("shop", "api.courier.ae", "owner@shop.ae")
	p.SetPreviewFetch("shop", true)
	call := func() string {
		r, err := p.TestRequest(ctx, "shop", platform.TestRequestInput{Method: "POST", Path: "/call", JSON: map[string]any{"url": "https://api.courier.ae/x"}})
		if err != nil {
			t.Fatal(err)
		}
		return r.Body
	}
	for i := 0; i < 3; i++ {
		if b := call(); !strings.Contains(b, `"status":200`) {
			t.Fatalf("call %d: %s", i, b)
		}
	}
	if used, quota := p.FetchUsage("usr_test"); used != 3 || quota != 3 {
		t.Fatalf("usage %d of %d after three preview calls", used, quota)
	}
	if b := call(); !strings.Contains(b, "used all 3 external calls") {
		t.Fatalf("call past the quota: %s", b)
	}
	// Turning test calls off stops preview at once.
	p.SetPreviewFetch("shop", false)
	if b := call(); !strings.Contains(b, "until the owner turns on test calls") {
		t.Fatalf("call with test calls off: %s", b)
	}
}

// The owner's download of an app must not hold visitors' password hashes,
// sessions or sign-in links.
func TestExportLeavesOutVisitorSecrets(t *testing.T) {
	p, _ := newAuthPlatform(t)
	p.CreateApp("usr_test", "club")
	p.ApplyChange(context.Background(), "club", membersApp())
	p.Publish("club", 0)
	v := newVisitor(t, p, "club.renoxium.test")
	v.submit("/_rx/auth/signup", "/_rx/auth/signup", url.Values{"email": {"huda@club.ae"}, "name": {"Huda"}, "password": {"correct horse battery"}})
	v.submit("/_rx/auth/forgot", "/_rx/auth/forgot", url.Values{"email": {"huda@club.ae"}})

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	if err := p.ExportApp(context.Background(), "club", zw); err != nil {
		t.Fatal(err)
	}
	zw.Close()
	zr, _ := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	var raw []byte
	for _, f := range zr.File {
		if f.Name == "club/live.db" {
			rc, _ := f.Open()
			raw, _ = io.ReadAll(rc)
			rc.Close()
		}
	}
	if len(raw) == 0 {
		t.Fatal("no live.db in the export")
	}
	if bytes.Contains(raw, []byte("argon2id$")) {
		t.Fatal("the export holds a password hash")
	}
	path := filepath.Join(t.TempDir(), "live.db")
	os.WriteFile(path, raw, 0o600)
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var email, name string
	var hash sql.NullString
	if err := db.QueryRow(`SELECT email, name, password_hash FROM rx_users`).Scan(&email, &name, &hash); err != nil || email != "huda@club.ae" || name != "Huda" || hash.Valid {
		t.Fatalf("visitor in the export: %q %q hash=%v %v", email, name, hash.Valid, err)
	}
	for _, table := range []string{"rx_sessions", "rx_email_tokens", "rx_phone_codes"} {
		var n int
		if db.QueryRow(`SELECT count(*) FROM ` + table).Scan(&n); n != 0 {
			t.Fatalf("%s has %d rows in the export", table, n)
		}
	}
	// The live app is untouched: the visitor is still signed in.
	if w := v.do("GET", "/account", nil); !strings.Contains(w.Body.String(), "huda@club.ae") {
		t.Fatalf("visitor signed out by the export: %s", w.Body.String())
	}
}

// failingWhatsApp refuses every message with the provider's own words.
type failingWhatsApp struct{}

func (failingWhatsApp) SendCode(context.Context, string, string) error {
	return errors.New("whatsapp error 131042 (HTTP 400): Business eligibility payment issue (fbtrace_id AbC123)")
}

// What the mail and WhatsApp services say about the platform's own account
// stays out of the app log, which every app owner and their agent can read.
func TestProviderErrorsStayOutOfTheAppLog(t *testing.T) {
	p, err := platform.New(platform.Config{DataDir: t.TempDir(), AppsDomain: "renoxium.test", PublicScheme: "https", WhatsApp: failingWhatsApp{}}, sandbox.NewGoja())
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	p.CreateApp("usr_test", "club")
	p.ApplyChange(context.Background(), "club", membersApp())
	p.Publish("club", 0)
	v := newVisitor(t, p, "club.renoxium.test")
	if w := v.submit("/_rx/auth/phone", "/_rx/auth/phone", url.Values{"phone": {"+971501234567"}}); w.Code != 503 {
		t.Fatalf("send: %d", w.Code)
	}
	logs, _ := p.Logs("club", applog.Filter{Source: "whatsapp"})
	if len(logs) != 1 || strings.Contains(logs[0].Message, "131042") || strings.Contains(logs[0].Message, "payment") || !strings.Contains(logs[0].Message, "could not be delivered") {
		t.Fatalf("app log: %+v", logs)
	}
}

// Numbers without an account get a share of the month's messages per day,
// and part of the month is kept for customers who already have an account,
// so a stranger typing numbers cannot lock a shop's customers out.
func TestWhatsAppNewNumbers(t *testing.T) {
	codes := &codeBox{codes: map[string]string{}}
	p, err := platform.New(platform.Config{DataDir: t.TempDir(), AppsDomain: "renoxium.test", PublicScheme: "https", WhatsApp: codes,
		OwnerPlan: func(string) string { return "free" }, OwnerEmail: func(string) (string, bool) { return "owner@club.ae", true }}, sandbox.NewGoja())
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	p.CreateApp("usr_free", "club")
	p.ApplyChange(context.Background(), "club", membersApp())
	p.SetAppKind("club", platform.KindShop) // a shop without Stripe keeps the free limits
	if _, err := p.Publish("club", 0); err != nil {
		t.Fatal(err)
	}
	ask := func(i int, phone string) int {
		v := newVisitor(t, p, "club.renoxium.test")
		v.ip = fmt.Sprintf("198.51.100.%d", i+1)
		return v.submit("/_rx/auth/phone", "/_rx/auth/phone", url.Values{"phone": {phone}}).Code
	}
	// A customer signs in once, so their number has an account.
	customer := newVisitor(t, p, "club.renoxium.test")
	customer.ip = "203.0.113.9"
	customer.submit("/_rx/auth/phone", "/_rx/auth/phone", url.Values{"phone": {"+971507000000"}})
	if w := customer.submit("/_rx/auth/phone", "/_rx/auth/phone/verify", url.Values{"phone": {"+971507000000"}, "code": {codes.codes["+971507000000"]}}); w.Code != 303 {
		t.Fatalf("customer sign-in: %d", w.Code)
	}
	// Free: 50 a month, so 12 new numbers a day. The customer was the first.
	sent := 1
	for i := 0; i < 20; i++ {
		if ask(i, fmt.Sprintf("+9715010000%02d", i)) == 200 {
			sent++
		}
	}
	if sent != 12 {
		t.Fatalf("%d codes went to new numbers in a day, want 12", sent)
	}
	if used, _ := p.WhatsAppUsage("usr_free"); used != 12 {
		t.Fatalf("usage %d", used)
	}
	// The customer still gets a code.
	if c := ask(100, "+971507000000"); c != 200 {
		t.Fatalf("known number after the daily share of new ones: %d", c)
	}
}

// A deleted app's name can be taken again by the account that had it, not
// by another one.
func TestDeletedAppNameIsHeld(t *testing.T) {
	p := newPlatform(t)
	if _, err := p.CreateApp("usr_a", "corner-shop"); err != nil {
		t.Fatal(err)
	}
	if err := p.DeleteApp("corner-shop"); err != nil {
		t.Fatal(err)
	}
	if _, err := p.CreateApp("usr_b", "corner-shop"); err == nil || !strings.Contains(err.Error(), "already taken") {
		t.Fatalf("another account took the name: %v", err)
	}
	if _, err := p.CreateApp("usr_a", "corner-shop"); err != nil {
		t.Fatalf("the former owner could not take the name back: %v", err)
	}
}

// A tool marked read_only is called by assistants without asking the
// person, so the platform makes sure it cannot change anything.
func TestReadOnlyToolsCannotChangeAnything(t *testing.T) {
	box := &outbox{}
	p, err := platform.New(platform.Config{DataDir: t.TempDir(), AppsDomain: "renoxium.test", PublicScheme: "https", Mailer: box}, sandbox.NewGoja())
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	def := &appdef.App{
		Tables:    []appdef.Table{{Name: "notes", Columns: []appdef.Column{{Name: "body", Type: "text"}}}},
		Templates: map[string]string{"t": "x"},
		Scripts: map[string]string{
			"exec":   `function handle(req) { db.exec("INSERT INTO notes (body) VALUES ('by exec')"); return {text: "wrote"} }`,
			"sneaky": `function handle(req) { const r = db.query("INSERT INTO notes (body) VALUES ('by query') RETURNING id"); return {json: r} }`,
			"mail":   `function handle(req) { email.send({to: "x@example.com", subject: "s", text: "t"}); return {text: "sent"} }`,
			"read":   `function handle(req) { return {json: db.query("SELECT count(*) AS n FROM notes")[0]} }`,
		},
		Routes: []appdef.Route{{Method: "GET", Path: "/", Template: "t"}},
		Tools: []appdef.Tool{
			{Name: "ro_exec", Description: "d", Script: "exec", ReadOnly: true},
			{Name: "ro_sneaky", Description: "d", Script: "sneaky", ReadOnly: true},
			{Name: "ro_mail", Description: "d", Script: "mail", ReadOnly: true},
			{Name: "count", Description: "d", Script: "read", ReadOnly: true},
			{Name: "add", Description: "d", Script: "exec"},
		},
	}
	p.CreateApp("usr_test", "notes")
	if res, err := p.ApplyChange(context.Background(), "notes", def); err != nil || !res.OK {
		t.Fatal(err, res)
	}
	p.Publish("notes", 0)
	tool := func(name string) (text string, isError bool) {
		t.Helper()
		body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": name, "arguments": map[string]any{}}})
		req := httptest.NewRequest("POST", "http://notes.renoxium.test/mcp", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		w := httptest.NewRecorder()
		p.ServeApp(w, req)
		r := w.Body.String()
		var out struct {
			Result struct {
				IsError bool `json:"isError"`
				Content []struct{ Text string }
			}
		}
		if err := json.Unmarshal([]byte(r), &out); err != nil || len(out.Result.Content) == 0 {
			t.Fatalf("%s: %s", name, r)
		}
		return out.Result.Content[0].Text, out.Result.IsError
	}
	for _, name := range []string{"ro_exec", "ro_mail"} {
		if _, isErr := tool(name); !isErr {
			t.Errorf("%s ran", name)
		}
	}
	tool("ro_sneaky") // a write through db.query is rolled back
	if text, _ := tool("count"); text != `{"n":0}` {
		t.Fatalf("after the read-only tools: %s", text)
	}
	box.mu.Lock()
	sent := len(box.sent)
	box.mu.Unlock()
	if sent != 0 {
		t.Fatal("a read-only tool sent an email")
	}
	// A tool that is not read-only works as before.
	if text, isErr := tool("add"); isErr || text != "wrote" {
		t.Fatalf("add: %s %v", text, isErr)
	}
	if text, _ := tool("count"); text != `{"n":1}` {
		t.Fatalf("after add: %s", text)
	}
	logs, _ := p.Logs("notes", applog.Filter{Level: "error"})
	if !anyLog(logs, "marked read_only") {
		t.Fatalf("the owner's log does not explain the refusal: %+v", logs)
	}
}
