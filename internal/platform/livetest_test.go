package platform_test

import (
	"context"
	"strings"
	"testing"

	"github.com/arif-rachim/renoxium/internal/appdef"
	"github.com/arif-rachim/renoxium/internal/platform"
)

// test_request never changes the live app: only GET may go to live, the
// route's script then runs read-only, and sign-in links stay on preview.
func TestTestRequestNeverChangesLive(t *testing.T) {
	p := newPlatform(t)
	ctx := context.Background()
	p.CreateApp("usr_test", "live1")
	def := &appdef.App{
		Tables: []appdef.Table{{Name: "hits", Columns: []appdef.Column{{Name: "n", Type: "integer"}}}},
		Scripts: map[string]string{
			"count": `function handle(req) { const r = db.query('SELECT count(*) AS n FROM hits')[0]; return {text: 'hits ' + r.n}; }`,
			"hit":   `function handle(req) { db.exec('INSERT INTO hits (n) VALUES (1)'); return {text: 'ok'}; }`,
			"mail":  `function handle(req) { email.send({to: 'a@b.ae', subject: 'x', text: 'y'}); return {text: 'sent'}; }`,
		},
		Routes: []appdef.Route{{Method: "GET", Path: "/", Script: "count"}, {Method: "GET", Path: "/hit", Script: "hit"},
			{Method: "GET", Path: "/mail", Script: "mail"}, {Method: "POST", Path: "/hit", Script: "hit"}},
	}
	if res, err := p.ApplyChange(ctx, "live1", def); err != nil || !res.OK {
		t.Fatal(err, res)
	}
	if _, err := p.Publish("live1", 0); err != nil {
		t.Fatal(err)
	}
	live := func(in platform.TestRequestInput) (platform.TestResponse, error) {
		in.Stage = platform.Live
		return p.TestRequest(ctx, "live1", in)
	}
	// Reading live works.
	if r, err := live(platform.TestRequestInput{Path: "/"}); err != nil || r.Body != "hits 0" {
		t.Fatalf("GET live: %v %+v", err, r)
	}
	// A GET whose script writes or sends is refused inside the script, and nothing changes.
	for _, path := range []string{"/hit", "/mail"} {
		r, err := live(platform.TestRequestInput{Path: path})
		if err != nil || r.Status != 500 || !strings.Contains(logText(r), "view_live_page is read-only") {
			t.Fatalf("GET live %s: %v %d %s", path, err, r.Status, logText(r))
		}
	}
	// Other methods and sign-in links never reach live.
	for _, in := range []platform.TestRequestInput{
		{Method: "POST", Path: "/hit"}, {Method: "DELETE", Path: "/hit"},
		{Path: "/_rx/auth/logout"}, {Job: "x"}, {Admin: &platform.TestAdminInput{Action: "insert", Table: "hits"}},
	} {
		if _, err := live(in); err == nil {
			t.Fatalf("%+v reached live", in)
		}
	}
	if r, _ := live(platform.TestRequestInput{Path: "/"}); r.Body != "hits 0" {
		t.Fatalf("live changed: %s", r.Body)
	}
	// The same GET on preview may write: preview data is a throwaway copy.
	if r, err := p.TestRequest(ctx, "live1", platform.TestRequestInput{Path: "/hit"}); err != nil || r.Body != "ok" {
		t.Fatalf("preview GET: %v %+v", err, r)
	}
	// And a real visitor's GET on live is not read-only.
	v := newVisitor(t, p, "live1.renoxium.test")
	if w := v.do("GET", "/hit", nil); w.Code != 200 {
		t.Fatalf("visitor GET /hit on live: %d", w.Code)
	}
	if r, _ := live(platform.TestRequestInput{Path: "/"}); r.Body != "hits 1" {
		t.Fatalf("visitor write on live: %s", r.Body)
	}
}
