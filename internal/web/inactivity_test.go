package web_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/arif-rachim/renoxium/internal/accounts"
	"github.com/arif-rachim/renoxium/internal/appdef"
	"github.com/arif-rachim/renoxium/internal/email"
)

// clock is a time the test moves forward.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time  { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) set(t time.Time) { c.mu.Lock(); c.t = t; c.mu.Unlock() }

func (e *env) login(c *http.Client, email string) {
	e.t.Helper()
	r, err := c.PostForm(e.base+"/login", url.Values{"csrf": {e.csrf(c, "/login")}, "email": {email}, "password": {"correct horse battery"}})
	if err != nil {
		e.t.Fatal(err)
	}
	if read(e.t, r); r.StatusCode != http.StatusSeeOther {
		e.t.Fatalf("login %s: %d", email, r.StatusCode)
	}
}

func (e *env) live(app string) (int, string) {
	w := httptest.NewRecorder()
	e.p.ServeApp(w, httptest.NewRequest("GET", "http://"+app+".renoxium.test/", nil))
	return w.Code, w.Body.String()
}

var staticSite = &appdef.App{Templates: map[string]string{"t": "Welcome to the shop"}, Routes: []appdef.Route{{Method: "GET", Path: "/", Template: "t"}}}

func TestInactiveAccountsAreWarnedDeactivatedAndReactivated(t *testing.T) {
	e := newEnvPlans(t, true)
	start := time.Now()
	clk := &clock{t: start}
	e.acc.SetClock(clk.now)
	ctx := context.Background()
	check := func(at time.Time) {
		t.Helper()
		clk.set(at)
		if _, err := e.site.CheckInactivity(ctx, at); err != nil {
			t.Fatal(err)
		}
	}
	act := func(email string) accounts.Activity {
		t.Helper()
		u, err := e.acc.UserByEmail(email)
		if err != nil {
			t.Fatal(err)
		}
		a, _ := e.acc.Activity(u.ID)
		return a
	}
	warnings := func(email string) int {
		n := 0
		for _, m := range e.mail.all() {
			if m.To == email && m.Tag == "inactivity_warning" {
				n++
			}
		}
		return n
	}

	alice := e.browser() // a free static site, then nothing
	e.signup(alice, "alice@example.com")
	e.publishDef(alice, "alice-site", staticSite)
	bob := e.browser() // signs in after the warning
	e.signup(bob, "bob@example.com")
	carol := e.browser() // granted a plan: exempt
	e.signup(carol, "carol@example.com")
	cu, _ := e.acc.UserByEmail("carol@example.com")
	e.acc.GrantPlan(cu.ID, accounts.PlanPro)
	dave := e.browser() // pays for a work app: exempt
	e.signup(dave, "dave@example.com")
	du, _ := e.acc.UserByEmail("dave@example.com")
	e.p.CreateApp(du.ID, "dave-crm")
	e.p.SetWorkSubscription("dave-crm", "sub_dave", "active", start.AddDate(1, 0, 0))
	hana := e.browser() // her agent keeps working for her
	e.signup(hana, "hana@example.com")
	hu, _ := e.acc.UserByEmail("hana@example.com")
	clientID := e.registerClient()

	// Four months on, Hana's agent calls the MCP server.
	clk.set(start.AddDate(0, 4, 0))
	verifier, challenge := pkce()
	authCode, _ := e.acc.CreateCode(clientID, hu.ID, redirectURI, challenge, "apps")
	tokens, err := e.acc.ExchangeCode(authCode, clientID, redirectURI, verifier)
	if err != nil {
		t.Fatal(err)
	}
	if _, errText := call(t, e.mcpSession(tokens.AccessToken), "list_apps", map[string]any{}); errText != "" {
		t.Fatal(errText)
	}

	// Nothing happens before the warning is due.
	check(start.AddDate(0, 6, -32))
	if n := warnings("alice@example.com"); n != 0 {
		t.Fatalf("warned %d times too early", n)
	}

	// 30 days before: the warning names the date and says signing in keeps the account.
	check(start.AddDate(0, 6, -29))
	// The promised date: six months after the last activity, and never less
	// than 30 days after the warning.
	due := act("alice@example.com").DeactivateOn
	if six := accounts.DeactivationDue(act("alice@example.com").ActiveAt); due.Before(six) || due.After(six.AddDate(0, 0, 1)) {
		t.Fatalf("deactivation promised on %v, six months is %v", due, six)
	}
	msg := e.mail.last(t, "alice@example.com", "inactivity_warning")
	if date := due.UTC().Format("2 January 2006"); !strings.Contains(msg.Subject, date) || !strings.Contains(msg.Text, "Signing in before then keeps it") {
		t.Fatalf("warning: %s\n%s", msg.Subject, msg.Text)
	}
	for _, who := range []string{"bob@example.com"} {
		if warnings(who) != 1 {
			t.Fatalf("%s not warned", who)
		}
	}
	for _, who := range []string{"carol@example.com", "dave@example.com", "hana@example.com"} {
		if warnings(who) != 0 {
			t.Fatalf("%s warned", who)
		}
	}
	// Sent once, however often the check runs.
	check(start.AddDate(0, 6, -28))
	check(start.AddDate(0, 6, -20))
	if n := warnings("alice@example.com"); n != 1 {
		t.Fatalf("warned %d times", n)
	}
	// Bob signs in: the warning is void.
	e.login(bob, "bob@example.com")
	if a := act("bob@example.com"); !a.WarnedAt.IsZero() {
		t.Fatalf("bob still warned: %+v", a)
	}
	if code, body := e.live("alice-site"); code != 200 || !strings.Contains(body, "Welcome") {
		t.Fatalf("live before the date: %d", code)
	}

	// The day before the date, nothing yet; on it, Alice's account is deactivated.
	check(due.Add(-time.Hour))
	if act("alice@example.com").Deactivated() {
		t.Fatal("deactivated before the date")
	}
	check(due.Add(time.Hour))
	if !act("alice@example.com").Deactivated() {
		t.Fatal("alice not deactivated")
	}
	for _, who := range []string{"bob@example.com", "carol@example.com", "dave@example.com", "hana@example.com"} {
		if act(who).Deactivated() {
			t.Fatalf("%s deactivated", who)
		}
	}
	code, body := e.live("alice-site")
	if code != http.StatusServiceUnavailable || !strings.Contains(body, "paused") || !strings.Contains(body, "Nothing has been deleted") {
		t.Fatalf("paused page: %d %s", code, body)
	}
	// Nothing was deleted, but nothing is published either until she is back.
	if info, err := e.p.Info("alice-site"); err != nil || info.LiveVersion != 1 {
		t.Fatalf("app after deactivation: %+v %v", info, err)
	}
	if _, err := e.p.Publish("alice-site", 0); err == nil || !strings.Contains(err.Error(), "Reactivate") {
		t.Fatalf("publish while deactivated: %v", err)
	}

	// Signing in alone does not bring it back; the dashboard offers Reactivate.
	e.login(alice, "alice@example.com")
	r, _ := alice.Get(e.base + "/")
	if page := read(t, r); !strings.Contains(page, "Your account is deactivated") || !strings.Contains(page, `action="/account/reactivate"`) {
		t.Fatal("dashboard lacks the Reactivate button")
	}
	if code, _ := e.live("alice-site"); code != http.StatusServiceUnavailable {
		t.Fatalf("live after sign-in only: %d", code)
	}
	r, _ = alice.PostForm(e.base+"/account/reactivate", url.Values{"csrf": {e.csrf(alice, "/")}})
	if read(t, r); r.StatusCode != http.StatusSeeOther {
		t.Fatalf("reactivate: %d", r.StatusCode)
	}
	if code, _ := e.live("alice-site"); code != 200 { // at once, not after a cache expires
		t.Fatalf("live after reactivating: %d", code)
	}
	if a := act("alice@example.com"); a.Deactivated() || a.ActiveAt.Before(due) {
		t.Fatalf("after reactivating: %+v", a)
	}
	r, _ = alice.Get(e.base + "/")
	if strings.Contains(read(t, r), "Your account is deactivated") {
		t.Fatal("dashboard still says deactivated")
	}
}

func TestOperatorSeesAndReactivatesDeactivatedAccounts(t *testing.T) {
	e := newEnvPlans(t, true)
	e.site.Operators = []string{"ops@renoxium.ae"}
	start := time.Now()
	clk := &clock{t: start}
	e.acc.SetClock(clk.now)
	ctx := context.Background()

	zara := e.browser()
	e.signup(zara, "zara@shop.ae")
	e.publishDef(zara, "zara-site", staticSite)
	zu, _ := e.acc.UserByEmail("zara@shop.ae")

	ops := e.browser()
	e.signup(ops, "ops@renoxium.ae")
	ou, _ := e.acc.UserByEmail("ops@renoxium.ae")
	http.Get(linkRE.FindString(e.mail.last(t, "ops@renoxium.ae", "verify_email").Text))
	e.acc.GrantPlan(ou.ID, accounts.PlanBusiness)

	for _, at := range []time.Time{start.AddDate(0, 6, -29), start.AddDate(0, 6, 2)} {
		clk.set(at)
		e.site.CheckInactivity(ctx, at)
	}
	if !e.acc.IsDeactivated(zu.ID) {
		t.Fatal("zara not deactivated")
	}
	e.login(ops, "ops@renoxium.ae")
	for path, want := range map[string]string{"/operator": "1 deactivated", "/operator/accounts?deactivated=1": "zara@shop.ae", "/operator/apps": "paused: account deactivated"} {
		r, _ := ops.Get(e.base + path)
		if page := read(t, r); r.StatusCode != 200 || !strings.Contains(page, want) {
			t.Fatalf("%s lacks %q", path, want)
		}
	}
	r, _ := ops.PostForm(e.base+"/operator/reactivate", url.Values{"csrf": {e.csrf(ops, "/operator")}, "user": {zu.ID}, "deactivated": {"1"}})
	if read(t, r); r.StatusCode != http.StatusSeeOther {
		t.Fatalf("operator reactivate: %d", r.StatusCode)
	}
	if e.acc.IsDeactivated(zu.ID) {
		t.Fatal("still deactivated")
	}
	if code, _ := e.live("zara-site"); code != 200 {
		t.Fatalf("live after the operator reactivated: %d", code)
	}
	// Not an operator: 404.
	e.login(zara, "zara@shop.ae")
	r, _ = zara.PostForm(e.base+"/operator/reactivate", url.Values{"csrf": {e.csrf(zara, "/")}, "user": {zu.ID}})
	if r.StatusCode != 404 {
		t.Fatalf("non-operator: %d", r.StatusCode)
	}
}

func TestStaffSignInAndPublishKeepTheAccount(t *testing.T) {
	e := newEnvPlans(t, true)
	start := time.Now()
	clk := &clock{t: start}
	e.acc.SetClock(clk.now)

	owner := e.browser()
	e.signup(owner, "owner@shop.ae")
	uo, _ := e.acc.UserByEmail("owner@shop.ae")
	e.p.CreateApp(uo.ID, "oud-shop")
	if err := e.p.SetAppKind("oud-shop", "shop"); err != nil {
		t.Fatal(err)
	}
	staff := e.browser()
	e.signup(staff, "staff@shop.ae")
	http.Get(linkRE.FindString(e.mail.last(t, "staff@shop.ae", "verify_email").Text))
	if err := e.p.AddStaff("oud-shop", "staff@shop.ae", uo.ID); err != nil {
		t.Fatal(err)
	}

	// Three months on the staff member signs in: the owner's account is active then.
	later := start.AddDate(0, 3, 0)
	clk.set(later)
	e.login(staff, "staff@shop.ae")
	if a, _ := e.acc.Activity(uo.ID); a.ActiveAt.Before(later.Truncate(time.Second)) {
		t.Fatalf("staff sign-in not counted for the owner: %v", a.ActiveAt)
	}

	// A publish counts too (here from the platform, as a confirmed publish does).
	other := e.browser()
	e.signup(other, "maker@site.ae")
	e.publishDef(other, "maker-site", staticSite)
	um, _ := e.acc.UserByEmail("maker@site.ae")
	clk.set(start.AddDate(0, 4, 0))
	e.p.ApplyChange(context.Background(), "maker-site", staticSite)
	if _, err := e.p.Publish("maker-site", 0); err != nil {
		t.Fatal(err)
	}
	if a, _ := e.acc.Activity(um.ID); a.ActiveAt.Before(start.AddDate(0, 4, 0).Truncate(time.Second)) {
		t.Fatalf("publish not counted: %v", a.ActiveAt)
	}
	// Visitors opening the site do not count.
	clk.set(start.AddDate(0, 5, 0))
	e.live("maker-site")
	if a, _ := e.acc.Activity(um.ID); !a.ActiveAt.Before(start.AddDate(0, 5, 0)) {
		t.Fatal("a visit counted as activity")
	}
}

func (m *mailbox) all() []email.Message {
	if m.wait != nil {
		m.wait()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]email.Message(nil), m.sent...)
}

// A late warning (the server was down, or the account's activity predates
// recording) still gives the owner the full 30 days; an account that starts
// paying comes back by itself.
func TestLateWarningKeepsThirtyDaysAndPayingReactivates(t *testing.T) {
	e := newEnvPlans(t, true)
	start := time.Now()
	clk := &clock{t: start}
	e.acc.SetClock(clk.now)
	ctx := context.Background()
	c := e.browser()
	e.signup(c, "late@example.com")
	u, _ := e.acc.UserByEmail("late@example.com")

	first := start.AddDate(1, 0, 0) // a year without a check
	clk.set(first)
	e.site.CheckInactivity(ctx, first)
	a, _ := e.acc.Activity(u.ID)
	if a.WarnedAt.IsZero() || a.Deactivated() || a.DeactivateOn.Before(first.AddDate(0, 0, accounts.WarnDays).Truncate(time.Second)) {
		t.Fatalf("late warning: %+v", a)
	}
	for _, at := range []time.Time{first.AddDate(0, 0, accounts.WarnDays-1), first.AddDate(0, 0, accounts.WarnDays).Add(time.Hour)} {
		clk.set(at)
		e.site.CheckInactivity(ctx, at)
	}
	if !e.acc.IsDeactivated(u.ID) {
		t.Fatal("not deactivated after the 30 days")
	}
	e.acc.GrantPlan(u.ID, accounts.PlanPro)
	res, _ := e.site.CheckInactivity(ctx, clk.now())
	if res.Reactivated != 1 || e.acc.IsDeactivated(u.ID) {
		t.Fatalf("paying account still deactivated: %+v", res)
	}
}

func TestTermsExplainDeactivation(t *testing.T) {
	e := newEnv(t)
	r, _ := http.Get(e.base + "/terms")
	page := read(t, r)
	for _, want := range []string{"no activity for 6 months", "an email 30 days before", "press Reactivate", "takes 2% of each card payment"} {
		if !strings.Contains(page, want) {
			t.Errorf("terms lack %q", want)
		}
	}
}
