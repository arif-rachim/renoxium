package platform_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/arif-rachim/renoxium/internal/appdef"
	"github.com/arif-rachim/renoxium/internal/applog"
	"github.com/arif-rachim/renoxium/internal/platform"
	"github.com/arif-rachim/renoxium/internal/sandbox"
)

// guardApp has a page, a form, and scripts that try to make the platform
// process do too much work.
func guardApp() *appdef.App {
	return &appdef.App{
		Tables: []appdef.Table{{Name: "items", Columns: []appdef.Column{{Name: "name", Type: "text"}}}},
		Templates: map[string]string{
			"home": `<!doctype html><html><head></head><body>home</body></html>`,
			// Each level includes the one below ten times: 10^8 copies of "x".
			"b0": `x`, "b1": `{{template "b0"}}{{template "b0"}}{{template "b0"}}{{template "b0"}}{{template "b0"}}{{template "b0"}}{{template "b0"}}{{template "b0"}}{{template "b0"}}{{template "b0"}}`,
			"b2": `{{template "b1"}}{{template "b1"}}{{template "b1"}}{{template "b1"}}{{template "b1"}}{{template "b1"}}{{template "b1"}}{{template "b1"}}{{template "b1"}}{{template "b1"}}`,
			"b3": `{{template "b2"}}{{template "b2"}}{{template "b2"}}{{template "b2"}}{{template "b2"}}{{template "b2"}}{{template "b2"}}{{template "b2"}}{{template "b2"}}{{template "b2"}}`,
			"b4": `{{template "b3"}}{{template "b3"}}{{template "b3"}}{{template "b3"}}{{template "b3"}}{{template "b3"}}{{template "b3"}}{{template "b3"}}{{template "b3"}}{{template "b3"}}`,
			"b5": `{{template "b4"}}{{template "b4"}}{{template "b4"}}{{template "b4"}}{{template "b4"}}{{template "b4"}}{{template "b4"}}{{template "b4"}}{{template "b4"}}{{template "b4"}}`,
			"b6": `{{template "b5"}}{{template "b5"}}{{template "b5"}}{{template "b5"}}{{template "b5"}}{{template "b5"}}{{template "b5"}}{{template "b5"}}{{template "b5"}}{{template "b5"}}`,
			"b7": `{{template "b6"}}{{template "b6"}}{{template "b6"}}{{template "b6"}}{{template "b6"}}{{template "b6"}}{{template "b6"}}{{template "b6"}}{{template "b6"}}{{template "b6"}}`,
			"b8": `{{template "b7"}}{{template "b7"}}{{template "b7"}}{{template "b7"}}{{template "b7"}}{{template "b7"}}{{template "b7"}}{{template "b7"}}{{template "b7"}}{{template "b7"}}`,
		},
		Scripts: map[string]string{
			"add":     `function handle(req) { db.exec("INSERT INTO items (name) VALUES (?)", req.form.name); return {text: "ok"} }`,
			"forever": `function handle(req) { return {json: db.query("WITH RECURSIVE c(x) AS (SELECT 1 UNION ALL SELECT x + 1 FROM c) SELECT count(*) AS n FROM c")} }`,
			"blob":    `function handle(req) { return {json: db.query("SELECT length(zeroblob(100000000)) AS n")} }`,
		},
		Routes: []appdef.Route{
			{Method: "GET", Path: "/", Template: "home"},
			{Method: "GET", Path: "/bomb/:n", Template: "b8"},
			{Method: "POST", Path: "/add", Script: "add"},
			{Method: "GET", Path: "/forever/:n", Script: "forever"},
			{Method: "GET", Path: "/blob/:n", Script: "blob"},
		},
	}
}

func publishedGuardApp(t *testing.T, p *platform.Platform) {
	t.Helper()
	if _, err := p.CreateApp("usr_test", "guard"); err != nil {
		t.Fatal(err)
	}
	if res, err := p.ApplyChange(context.Background(), "guard", guardApp()); err != nil || !res.OK {
		t.Fatalf("apply: %v %+v", err, res)
	}
	if _, err := p.Publish("guard", 0); err != nil {
		t.Fatal(err)
	}
}

func TestTemplateOutputIsCapped(t *testing.T) {
	p := newPlatform(t)
	publishedGuardApp(t, p)
	start := time.Now()
	r := do(t, p, "GET", "guard.renoxium.test", "/bomb/1", nil)
	if r.status != 500 || time.Since(start) > 10*time.Second {
		t.Fatalf("template bomb: %d after %s", r.status, time.Since(start))
	}
	logs, _ := p.Logs("guard", applog.Filter{Level: "error"})
	if len(logs) == 0 || !strings.Contains(logs[0].Message, "larger than") {
		t.Fatalf("logs: %+v", logs)
	}
}

func TestScriptQueriesAreBounded(t *testing.T) {
	g := sandbox.NewGoja()
	g.Timeout = 10 * time.Second // longer than the query timeout, which must stop the query on its own
	p := newPlatformWith(t, g)
	publishedGuardApp(t, p)
	start := time.Now()
	if r := do(t, p, "GET", "guard.renoxium.test", "/forever/1", nil); r.status != 500 {
		t.Fatalf("runaway query: %d", r.status)
	}
	if took := time.Since(start); took > 5*time.Second {
		t.Fatalf("runaway query ran %s; the query timeout is %s", took, 2*time.Second)
	}
	if r := do(t, p, "GET", "guard.renoxium.test", "/blob/1", nil); r.status != 500 {
		t.Fatalf("100 MB blob: %d", r.status)
	}
	logs, _ := p.Logs("guard", applog.Filter{Level: "error", Limit: 1})
	if len(logs) == 0 || !strings.Contains(logs[0].Message, "too big") {
		t.Fatalf("logs: %+v", logs)
	}
}

func TestCrossSitePostsAreRefused(t *testing.T) {
	p := newPlatform(t)
	publishedGuardApp(t, p)
	post := func(set func(h http.Header)) int {
		r := httptest.NewRequest("POST", "http://guard.renoxium.test/add", strings.NewReader("name=a"))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		set(r.Header)
		w := httptest.NewRecorder()
		p.ServeApp(w, r)
		return w.Code
	}
	if c := post(func(h http.Header) { h.Set("Sec-Fetch-Site", "same-site") }); c != 403 {
		t.Fatalf("post from a sibling app: %d", c)
	}
	if c := post(func(h http.Header) { h.Set("Origin", "https://evil.renoxium.test") }); c != 403 {
		t.Fatalf("post with another origin: %d", c)
	}
	if c := post(func(h http.Header) { h.Set("Sec-Fetch-Site", "same-origin") }); c != 200 {
		t.Fatalf("post from the app itself: %d", c)
	}
	if c := post(func(h http.Header) { h.Set("Origin", "https://guard.renoxium.test") }); c != 200 {
		t.Fatalf("post with the app's origin: %d", c)
	}
	if c := post(func(h http.Header) {}); c != 200 {
		t.Fatalf("post without browser headers: %d", c)
	}
}

func TestBrowserLogNeedsAServedPage(t *testing.T) {
	p := newPlatform(t)
	publishedGuardApp(t, p)
	beacon := func(rid string) {
		r := httptest.NewRequest("POST", "http://guard.renoxium.test/_rx/log", strings.NewReader(`{"rid":"`+rid+`","message":"boom","page":"/"}`))
		p.ServeApp(httptest.NewRecorder(), r)
	}
	beacon("made-up")
	if logs, _ := p.Logs("guard", applog.Filter{Source: "browser"}); len(logs) != 0 {
		t.Fatalf("report with an unknown request ID was stored: %+v", logs)
	}
	home := do(t, p, "GET", "guard.renoxium.test", "/", nil)
	beacon(home.header.Get("X-Request-Id"))
	if logs, _ := p.Logs("guard", applog.Filter{Source: "browser"}); len(logs) != 1 || !strings.Contains(logs[0].Message, "untrusted") {
		t.Fatalf("report of a served page: %+v", logs)
	}
}

func TestPrivatePreview(t *testing.T) {
	p, err := platform.New(platform.Config{DataDir: t.TempDir(), AppsDomain: "renoxium.test", PublicScheme: "https",
		SiteURL: "https://site.test", PreviewKey: []byte("0123456789abcdef0123456789abcdef")}, sandbox.NewGoja())
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	p.CreateApp("usr_test", "guard")
	res, _ := p.ApplyChange(context.Background(), "guard", guardApp())
	if res.PreviewURL != "https://site.test/apps/guard/preview" {
		t.Fatalf("preview url: %s", res.PreviewURL)
	}
	host := "guard--preview.renoxium.test"
	if r := do(t, p, "GET", host, "/", nil); r.status != 403 || !strings.Contains(r.body, "https://site.test/apps/guard/preview") {
		t.Fatalf("preview without access: %d %s", r.status, r.body)
	}
	if r := do(t, p, "GET", host, "/_rx/preview?t=1.bad", nil); r.status != 403 {
		t.Fatalf("forged link: %d", r.status)
	}
	link, _ := url.Parse(p.PreviewLink("guard"))
	v := newVisitor(t, p, host)
	if w := v.do("GET", link.RequestURI(), nil); w.Code != 303 {
		t.Fatalf("preview link: %d", w.Code)
	}
	if w := v.do("GET", "/", nil); w.Code != 200 {
		t.Fatalf("preview with access: %d", w.Code)
	}
	// A link for one app does not open another.
	p.CreateApp("usr_test", "other")
	p.ApplyChange(context.Background(), "other", guardApp())
	if r := do(t, p, "GET", "other--preview.renoxium.test", link.RequestURI(), nil); r.status != 403 {
		t.Fatalf("link reused for another app: %d", r.status)
	}
	// The agent's own test requests still reach the preview.
	out, err := p.TestRequest(context.Background(), "guard", platform.TestRequestInput{Path: "/"})
	if err != nil || out.Status != 200 {
		t.Fatalf("test_request: %v %+v", err, out)
	}
}

func TestPublishNeedsAConfirmedOwner(t *testing.T) {
	p, err := platform.New(platform.Config{DataDir: t.TempDir(), AppsDomain: "renoxium.test", PublicScheme: "https",
		OwnerEmail: func(id string) (string, bool) { return "", false }}, sandbox.NewGoja())
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	p.CreateApp("usr_new", "guard")
	p.ApplyChange(context.Background(), "guard", guardApp())
	if _, err := p.Publish("guard", 0); err == nil || !strings.Contains(err.Error(), "confirm your Renoxium email") {
		t.Fatalf("publish by an unconfirmed owner: %v", err)
	}
}

func TestQuotas(t *testing.T) {
	p := newPlatform(t)
	for _, name := range []string{"paypal", "login", "support"} {
		if _, err := p.CreateApp("usr_test", name); err == nil || !strings.Contains(err.Error(), "reserved") {
			t.Fatalf("reserved name %q: %v", name, err)
		}
	}
	for i := 0; i < platform.MaxAppsPerOwner; i++ {
		if _, err := p.CreateApp("usr_test", fmt.Sprintf("shop-%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := p.CreateApp("usr_test", "one-too-many"); err == nil {
		t.Fatal("app over the per-account limit was created")
	}
	if _, err := p.CreateApp("usr_other", "other-shop"); err != nil {
		t.Fatalf("limit is per account: %v", err)
	}
}

func TestOldBackupsArePruned(t *testing.T) {
	dir := t.TempDir()
	p, err := platform.New(platform.Config{DataDir: dir, AppsDomain: "renoxium.test", PublicScheme: "https"}, sandbox.NewGoja())
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	p.CreateApp("usr_test", "guard")
	for i := 0; i < 6; i++ {
		p.ApplyChange(context.Background(), "guard", guardApp())
		if _, err := p.Publish("guard", 0); err != nil {
			t.Fatal(err)
		}
	}
	backups, _ := filepath.Glob(filepath.Join(dir, "apps", "guard", "backup-before-v*.db"))
	if len(backups) != 3 {
		t.Fatalf("backups kept: %v", backups)
	}
}

func TestPhoneCodeGuessesAreLimitedPerNumber(t *testing.T) {
	codes := &codeBox{codes: map[string]string{}}
	p, err := platform.New(platform.Config{DataDir: t.TempDir(), AppsDomain: "renoxium.test", PublicScheme: "https", WhatsApp: codes}, sandbox.NewGoja())
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	p.CreateApp("usr_test", "club")
	p.ApplyChange(context.Background(), "club", membersApp())
	p.Publish("club", 0)
	v := newVisitor(t, p, "club.renoxium.test")
	phone := "+971507777777"
	csrf := regexp.MustCompile(`name="csrf" value="([^"]+)"`).FindStringSubmatch(v.do("GET", "/_rx/auth/phone", nil).Body.String())[1]
	v.submit("/_rx/auth/phone", "/_rx/auth/phone", url.Values{"phone": {phone}})
	good := codes.codes[phone]
	bad := "000000"
	if good == bad {
		bad = "111111"
	}

	// Parallel wrong guesses cannot get past the per-code limit of 5.
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			form := url.Values{"csrf": {csrf}, "phone": {phone}, "code": {bad}}
			r := httptest.NewRequest("POST", "http://club.renoxium.test/_rx/auth/phone/verify", strings.NewReader(form.Encode()))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			for _, c := range v.cookies {
				r.AddCookie(c)
			}
			p.ServeApp(httptest.NewRecorder(), r)
		}()
	}
	wg.Wait()
	if w := v.submit("/_rx/auth/phone", "/_rx/auth/phone/verify", url.Values{"phone": {phone}, "code": {good}}); w.Code == 303 {
		t.Fatal("the right code still worked after 20 parallel wrong guesses")
	}

	// A new code does not reset the guesses allowed for the number.
	v.submit("/_rx/auth/phone", "/_rx/auth/phone", url.Values{"phone": {phone}})
	if w := v.submit("/_rx/auth/phone", "/_rx/auth/phone/verify", url.Values{"phone": {phone}, "code": {codes.codes[phone]}}); w.Code != 429 {
		t.Fatalf("guess after the hourly limit: %d", w.Code)
	}
}

func TestRateKeyGroupsIPv6Networks(t *testing.T) {
	if a, b := platform.RateKey("2001:db8:1:2::1"), platform.RateKey("2001:db8:1:2:ffff::9"); a != b {
		t.Fatalf("same /64: %s %s", a, b)
	}
	if a, b := platform.RateKey("2001:db8:1:2::1"), platform.RateKey("2001:db8:1:3::1"); a == b {
		t.Fatal("different /64 share a key")
	}
	if k := platform.RateKey("203.0.113.7"); k != "203.0.113.7" {
		t.Fatalf("ipv4: %s", k)
	}
}
