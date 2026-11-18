package platform_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/arif-rachim/renoxium/internal/appdef"
	"github.com/arif-rachim/renoxium/internal/platform"
)

// hookApp records every admin change in an audit table, and edits the
// changed row itself, which must not run the hook again.
func hookApp() *appdef.App {
	return &appdef.App{
		Tables: []appdef.Table{
			{Name: "notes", Columns: []appdef.Column{{Name: "title", Type: "text", Required: true}, {Name: "status", Type: "text", Options: []string{"open", "done"}},
				{Name: "seen", Type: "boolean"}, {Name: "due", Type: "datetime"}, {Name: "price", Type: "real"}}},
			{Name: "audit", Columns: []appdef.Column{{Name: "line", Type: "text"}, {Name: "token", Type: "text"}}},
		},
		Lib: map[string]string{
			"b_audit": "function audit(line) {\n  db.exec('INSERT INTO audit (line, token) VALUES (?, ?)', line, crypto.randomId());\n}\n",
			"a_text":  "const SEP = ' | ';\nfunction describe(b) {\n  return [b.action, b.table + ' #' + b.id, b.by, b.before ? b.before.status : 'null', b.row ? b.row.status : 'null'].join(SEP);\n}\nfunction explode(o) {\n  return o.no.such;\n}\n",
		},
		Scripts: map[string]string{
			"home":  `function handle(req) { return {json: db.query('SELECT id, line, token FROM audit ORDER BY id')} }`,
			"crash": "function handle(req) {\n  return {text: explode({})};\n}",
			"on_change": `function handle(req) {
				const b = req.body;
				if (req.method !== 'ADMIN' || req.path !== '/_rx/admin/' + b.table || req.user !== null || !req.origin) throw new Error('bad request ' + JSON.stringify(req));
				if (b.table !== 'notes') return {text: 'ignored'};
				audit(describe(b));
				if (b.row && b.row.title === 'boom') throw new Error('the hook failed on purpose');
				// The hook's own change to the same row must not run the hook again.
				if (b.row) db.exec("UPDATE notes SET title = title || '!' WHERE id = ?", b.id);
				return {text: 'audited ' + b.action};
			}`,
		},
		Routes:        []appdef.Route{{Method: "GET", Path: "/", Script: "home"}, {Method: "GET", Path: "/crash", Script: "crash"}},
		OnAdminChange: "on_change",
	}
}

func auditLines(t *testing.T, p *platform.Platform, app string) []string {
	t.Helper()
	rows, _, err := p.AdminList(context.Background(), app, "audit", "", 1)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for i := len(rows) - 1; i >= 0; i-- {
		if tok, _ := rows[i]["token"].(string); len(tok) != 22 {
			t.Errorf("audit token %q is not a 22 character random id", tok)
		}
		out = append(out, rows[i]["line"].(string))
	}
	return out
}

func TestOnAdminChangeRunsAfterAdminChanges(t *testing.T) {
	p := newPlatform(t)
	ctx := context.Background()
	p.CreateApp("usr_test", "hooks")
	if res, err := p.ApplyChange(ctx, "hooks", hookApp()); err != nil || !res.OK {
		t.Fatal(err, res)
	}
	if _, err := p.Publish("hooks", 0); err != nil {
		t.Fatal(err)
	}
	const by = "staff@shop.ae"
	id, err := p.AdminSave(ctx, "hooks", "notes", 0, map[string]string{"title": "Call", "status": "open"}, by)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.AdminSave(ctx, "hooks", "notes", id, map[string]string{"title": "Call!", "status": "done"}, by); err != nil {
		t.Fatal(err)
	}
	// The hook ran once per change, not again for its own UPDATE of the row.
	want := []string{"insert | notes #1 | staff@shop.ae | null | open", "update | notes #1 | staff@shop.ae | open | done"}
	if got := auditLines(t, p, "hooks"); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("audit after insert and update:\n%s", strings.Join(got, "\n"))
	}
	if row, _ := p.AdminGet(ctx, "hooks", "notes", id); row["title"] != "Call!!" {
		t.Fatalf("hook's own change: %v", row)
	}
	if err := p.AdminDelete(ctx, "hooks", "notes", id, by); err != nil {
		t.Fatal(err)
	}
	if got := auditLines(t, p, "hooks"); len(got) != 3 || got[2] != "delete | notes #1 | staff@shop.ae | done | null" {
		t.Fatalf("audit after delete: %v", got)
	}
	// Changes to other tables reach the hook too; it ignores them here.
	if _, err := p.AdminSave(ctx, "hooks", "audit", 0, map[string]string{"line": "by hand", "token": "xxxxxxxxxxxxxxxxxxxxxx"}, by); err != nil {
		t.Fatal(err)
	}
	logs, _ := p.Logs("hooks", logFilterAll())
	if !anyLog(logs, "on_admin_change on_change after insert audit #4 by staff@shop.ae -> ok") || !anyLog(logs, "audited delete") {
		t.Error("hook runs are not logged")
	}
	if n := p.AdminNotice("hooks", by); n != "" {
		t.Fatalf("notice without a failure: %q", n)
	}
}

func TestFailingOnAdminChangeKeepsTheChange(t *testing.T) {
	p := newPlatform(t)
	ctx := context.Background()
	p.CreateApp("usr_test", "hooks")
	if res, err := p.ApplyChange(ctx, "hooks", hookApp()); err != nil || !res.OK {
		t.Fatal(err, res)
	}
	p.Publish("hooks", 0)
	id, err := p.AdminSave(ctx, "hooks", "notes", 0, map[string]string{"title": "boom", "status": "open"}, "owner@shop.ae")
	if err != nil || id == 0 {
		t.Fatalf("a failing hook failed the save: %v", err)
	}
	if row, err := p.AdminGet(ctx, "hooks", "notes", id); err != nil || row["title"] != "boom" {
		t.Fatalf("the change was undone: %v %v", row, err)
	}
	// Database changes the script made before failing stay, as in any script.
	if got := auditLines(t, p, "hooks"); len(got) != 1 {
		t.Fatalf("audit: %v", got)
	}
	logs, _ := p.Logs("hooks", logFilterAll())
	var failure string
	for _, e := range logs {
		if strings.Contains(e.Message, "on_admin_change on_change after insert notes #1 by owner@shop.ae failed") {
			failure = e.Message
			if e.Level != "error" || e.RequestID == "" || e.Stage != platform.Live {
				t.Errorf("failure event: %+v", e)
			}
		}
	}
	if !strings.Contains(failure, "the hook failed on purpose") || !strings.Contains(failure, "at handle (script:") || !strings.Contains(failure, "the change itself was saved") {
		t.Fatalf("failure is not logged with its stack: %q", failure)
	}
	if n := p.AdminNotice("hooks", "owner@shop.ae"); !strings.Contains(n, "was saved") || !strings.Contains(n, "on_change") {
		t.Fatalf("notice: %q", n)
	}
	if n := p.AdminNotice("hooks", "owner@shop.ae"); n != "" {
		t.Fatalf("the notice is shown once: %q", n)
	}
}

func TestTestRequestAdminChangesPreview(t *testing.T) {
	p := newPlatform(t)
	ctx := context.Background()
	p.CreateApp("usr_test", "hooks")
	if res, err := p.ApplyChange(ctx, "hooks", hookApp()); err != nil || !res.OK {
		t.Fatal(err, res)
	}
	admin := func(in platform.TestAdminInput) platform.TestResponse {
		t.Helper()
		r, err := p.TestRequest(ctx, "hooks", platform.TestRequestInput{Admin: &in})
		if err != nil {
			t.Fatalf("%+v: %v", in, err)
		}
		return r
	}
	r := admin(platform.TestAdminInput{Action: "insert", Table: "notes", Values: map[string]any{"title": "Ship", "status": "open", "seen": true, "price": 12.5, "due": "2026-12-01T09:30"}})
	if r.Status != 200 || r.Body != "audited insert" || r.Admin == nil || r.Admin.ID != 1 || r.Admin.Hook != "on_change" {
		t.Fatalf("insert: %+v", r)
	}
	if row := r.Admin.Row; row["title"] != "Ship!" || row["seen"] != int64(1) || row["price"] != 12.5 || row["due"] != "2026-12-01 09:30:00" {
		t.Fatalf("row after insert: %v", row)
	}
	if !anyLog(r.Logs, "owner@example.com added notes #1") || !anyLog(r.Logs, "on_admin_change on_change after insert notes #1") {
		t.Fatalf("logs: %s", logText(r))
	}
	// An update changes only the given columns, as the pre-filled admin form does.
	r = admin(platform.TestAdminInput{Action: "update", Table: "notes", ID: 1, Values: map[string]any{"status": "done"}, By: "staff@shop.ae"})
	if r.Status != 200 || r.Admin.Row["title"] != "Ship!!" || r.Admin.Row["status"] != "done" || r.Admin.Row["seen"] != int64(1) || r.Admin.Row["price"] != 12.5 {
		t.Fatalf("update: %+v", r)
	}
	// The same checks as the admin panel.
	if r := admin(platform.TestAdminInput{Action: "update", Table: "notes", ID: 1, Values: map[string]any{"status": "lost", "price": "abc"}}); r.Status != 422 ||
		!strings.Contains(r.Body, "choose one of: open, done") || !strings.Contains(r.Body, "enter a number") {
		t.Fatalf("invalid values: %+v", r)
	}
	if r := admin(platform.TestAdminInput{Action: "insert", Table: "notes", Values: map[string]any{"status": "open"}}); r.Status != 422 || !strings.Contains(r.Body, "title: required") {
		t.Fatalf("missing required value: %+v", r)
	}
	// A failing hook: 500 with the error, and the row is saved.
	r = admin(platform.TestAdminInput{Action: "insert", Table: "notes", Values: map[string]any{"title": "boom"}})
	if r.Status != 500 || !strings.Contains(r.Body, "the hook failed on purpose") || r.Admin == nil || r.Admin.Row["title"] != "boom" {
		t.Fatalf("failing hook: %+v", r)
	}
	r = admin(platform.TestAdminInput{Action: "delete", Table: "notes", ID: 1})
	if r.Status != 200 || r.Body != "audited delete" || r.Admin.Row != nil {
		t.Fatalf("delete: %+v", r)
	}
	if r := admin(platform.TestAdminInput{Action: "delete", Table: "notes", ID: 1}); r.Status != 404 {
		t.Fatalf("deleting twice: %+v", r)
	}
	home, _ := p.TestRequest(ctx, "hooks", platform.TestRequestInput{Path: "/"})
	var audit []map[string]any
	json.Unmarshal([]byte(home.Body), &audit)
	if len(audit) != 4 || audit[1]["line"] != "update | notes #1 | staff@shop.ae | open | done" || audit[3]["line"] != "delete | notes #1 | owner@example.com | done | null" {
		t.Fatalf("preview audit: %s", home.Body)
	}
	// Live data is not touched, and live is refused.
	for _, in := range []platform.TestRequestInput{
		{Stage: "live", Admin: &platform.TestAdminInput{Action: "insert", Table: "notes", Values: map[string]any{"title": "x"}}},
		{Admin: &platform.TestAdminInput{Action: "insert", Table: "nope"}},
		{Admin: &platform.TestAdminInput{Action: "insert", Table: "notes", Values: map[string]any{"id": 4}}},
		{Admin: &platform.TestAdminInput{Action: "upsert", Table: "notes"}},
		{Admin: &platform.TestAdminInput{Action: "update", Table: "notes", ID: 99}},
	} {
		if _, err := p.TestRequest(ctx, "hooks", in); err == nil {
			t.Errorf("%+v accepted", in.Admin)
		}
	}
}

func TestLibProblemsAndPatch(t *testing.T) {
	p := newPlatform(t)
	ctx := context.Background()
	p.CreateApp("usr_test", "hooks")
	def := hookApp()
	def.Lib["c_broken"] = "function ok() {}\n\nconst x = 1 +;\n"
	res, err := p.ApplyChange(ctx, "hooks", def)
	if err != nil || res.OK || !strings.Contains(fmt.Sprint(res.Problems), `lib "c_broken"`) || !strings.Contains(fmt.Sprint(res.Problems), "lib/c_broken:3:") {
		t.Fatalf("broken lib: %v %+v", err, res)
	}
	def.Lib["c_broken"] = "function handle(req) { return {text: 'x'} }"
	if res, _ := p.ApplyChange(ctx, "hooks", def); res.OK || !strings.Contains(fmt.Sprint(res.Problems), `lib "c_broken" defines handle`) {
		t.Fatalf("handle in lib: %+v", res)
	}
	delete(def.Lib, "c_broken")
	def.Scripts["clash"] = "const SEP = 1;\nfunction handle(req) { return {text: ''} }"
	if res, _ := p.ApplyChange(ctx, "hooks", def); res.OK || !strings.Contains(fmt.Sprint(res.Problems), `script "clash"`) {
		t.Fatalf("script redeclaring a lib constant: %+v", res)
	}
	delete(def.Scripts, "clash")
	if res, err := p.ApplyChange(ctx, "hooks", def); err != nil || !res.OK {
		t.Fatal(err, res)
	}

	// Errors inside a lib function point at the library and line in the log.
	r, _ := p.TestRequest(ctx, "hooks", platform.TestRequestInput{Path: "/crash"})
	if r.Status != 500 || !anyLog(r.Logs, "at explode (lib/a_text:6:") || !anyLog(r.Logs, "at handle (script:2:") {
		t.Fatalf("crash: %d\n%s", r.Status, logText(r))
	}

	// patch_app changes one lib entry and keeps the others.
	res, err = p.ApplyPatch(ctx, "hooks", platform.Patch{Lib: map[string]string{"a_text": "const SEP = ' / ';\nfunction describe(b) { return b.action + SEP + b.table; }\nfunction explode(o) { return 'calm'; }"}})
	if err != nil || !res.OK {
		t.Fatal(err, res)
	}
	got, _, _ := p.Definition("hooks", 0)
	if len(got.Lib) != 2 || !strings.Contains(got.Lib["b_audit"], "crypto.randomId") {
		t.Fatalf("lib after patch: %v", got.Lib)
	}
	if r, _ := p.TestRequest(ctx, "hooks", platform.TestRequestInput{Path: "/crash"}); r.Status != 200 || r.Body != "calm" {
		t.Fatalf("patched lib: %+v", r)
	}
	if r, _ := p.TestRequest(ctx, "hooks", platform.TestRequestInput{Admin: &platform.TestAdminInput{Action: "insert", Table: "notes", Values: map[string]any{"title": "a"}}}); r.Body != "audited insert" {
		t.Fatalf("hook after patch: %+v", r)
	}
	if res, _ := p.ApplyPatch(ctx, "hooks", platform.Patch{DeleteLib: []string{"nope"}}); res.OK {
		t.Fatal("deleting a missing lib entry was accepted")
	}
	// Deleting the lib a script needs is caught when the script runs, and on_admin_change can be turned off.
	off := ""
	if res, err := p.ApplyPatch(ctx, "hooks", platform.Patch{DeleteLib: []string{"b_audit"}, OnAdminChange: &off}); err != nil || !res.OK {
		t.Fatal(err, res)
	}
	if r, _ := p.TestRequest(ctx, "hooks", platform.TestRequestInput{Admin: &platform.TestAdminInput{Action: "insert", Table: "notes", Values: map[string]any{"title": "b"}}}); r.Status != 200 || r.Admin.Hook != "" || !strings.Contains(r.Body, "no on_admin_change") {
		t.Fatalf("without a hook: %+v", r)
	}
	def.OnAdminChange = "missing"
	if res, _ := p.ApplyChange(ctx, "hooks", def); res.OK || !strings.Contains(fmt.Sprint(res.Problems), `on_admin_change: script "missing" is not defined`) {
		t.Fatalf("missing hook script: %+v", res)
	}
}
