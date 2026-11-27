package web_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/arif-rachim/renoxium/examples"
)

// The agent builds the tasks example and tests every CRUD route on preview
// through MCP, the flow agents could not verify before.
func TestAgentTestsCRUDOnPreview(t *testing.T) {
	e := newEnv(t)
	c := e.browser()
	e.signup(c, "jack@shop.ae")
	s := e.mcpSession(e.connect(c, e.registerClient()).AccessToken)

	list, _ := call(t, s, "get_example", map[string]any{})
	if names := list["examples"].([]any); len(names) != len(examples.All()) {
		t.Fatalf("examples = %v", list)
	}
	ex, errText := call(t, s, "get_example", map[string]any{"name": "tasks"})
	if errText != "" {
		t.Fatal(errText)
	}
	// The reference comes once: with the chosen example, or alone, never with the list.
	if _, has := list["reference"]; has {
		t.Error("the list of examples carries the reference")
	}
	// The reference comes as its core and an index (a free plan chat fills
	// up fast); other parts on request.
	if ref, _ := ex["reference"].(string); !strings.Contains(ref, "App definition") || !strings.Contains(ref, "- payments (") {
		t.Error("the example lacks the reference's core and index")
	}
	refOnly, _ := call(t, s, "get_example", map[string]any{"reference_only": true})
	ref, _ := refOnly["reference"].(string)
	if len(ref) > 8000 || strings.Contains(ref, "payments.checkout") || refOnly["definition"] != nil || refOnly["examples"] != nil {
		t.Errorf("reference_only: %d characters, %v keys", len(ref), len(refOnly))
	}
	pay, _ := call(t, s, "get_example", map[string]any{"reference_only": true, "reference_parts": []any{"payments", "visitor-accounts"}})
	if ref, _ := pay["reference"].(string); !strings.Contains(ref, "payments.checkout") || !strings.Contains(ref, "/_rx/auth/login") {
		t.Error("reference_parts did not return the parts")
	}
	if all, _ := call(t, s, "get_example", map[string]any{"reference_only": true, "reference_parts": []any{"all"}}); len(all["reference"].(string)) < 20000 {
		t.Error("reference_parts [all] is not the whole reference")
	}
	if _, errText := call(t, s, "get_example", map[string]any{"reference_only": true, "reference_parts": []any{"paymentz"}}); !strings.Contains(errText, "visitor-accounts") {
		t.Errorf("unknown part: %q", errText)
	}
	call(t, s, "create_app", map[string]any{"app": "jack-tasks"})
	applied, _ := call(t, s, "apply_change", map[string]any{"app": "jack-tasks", "definition": ex["definition"]})
	if applied["ok"] != true {
		t.Fatalf("apply: %v", applied)
	}

	req := func(args map[string]any) map[string]any {
		t.Helper()
		args["app"] = "jack-tasks"
		// A page is opened with view_page, a form sent with submit_form.
		tool := "view_page"
		if args["method"] == "POST" {
			tool = "submit_form"
		}
		delete(args, "method")
		out, errText := call(t, s, tool, args)
		if errText != "" {
			t.Fatalf("%s %v: %s", tool, args, errText)
		}
		return out
	}
	status := func(out map[string]any) int { return int(out["status"].(float64)) }

	created := req(map[string]any{"method": "POST", "path": "/tasks", "form": map[string]any{"title": "Call supplier"}})
	if status(created) != 303 || created["location"] != "/" {
		t.Fatalf("create: %v", created)
	}
	home := req(map[string]any{"path": "/"})
	body := home["body"].(string)
	if status(home) != 200 || !strings.Contains(body, "Call supplier") || !strings.Contains(body, "2 open, 1 done") {
		t.Fatalf("home: %v", body)
	}
	if strings.Contains(body, "sendBeacon") {
		t.Fatal("telemetry script left in the body")
	}
	if logs := home["logs"].([]any); len(logs) == 0 || !strings.Contains(logs[0].(map[string]any)["message"].(string), "GET / -> 200") {
		t.Fatalf("logs: %v", home["logs"])
	}
	if out := req(map[string]any{"method": "POST", "path": "/tasks/3/toggle"}); status(out) != 303 {
		t.Fatalf("toggle: %v", out)
	}
	if out := req(map[string]any{"path": "/?show=done"}); !strings.Contains(out["body"].(string), "Call supplier") {
		t.Fatalf("done filter: %v", out["body"])
	}
	if out := req(map[string]any{"method": "POST", "path": "/tasks/3", "form": map[string]any{"title": "Call supplier again"}}); status(out) != 303 {
		t.Fatalf("update: %v", out)
	}
	if out := req(map[string]any{"path": "/tasks/3"}); !strings.Contains(out["body"].(string), `value="Call supplier again"`) || strings.Contains(out["body"].(string), "checked") {
		t.Fatalf("edit form after unchecking: %v", out["body"])
	}
	if out := req(map[string]any{"method": "POST", "path": "/tasks/3/delete"}); status(out) != 303 {
		t.Fatalf("delete: %v", out)
	}
	if out := req(map[string]any{"path": "/tasks/3"}); status(out) != 404 {
		t.Fatalf("deleted task: %v", out)
	}
	if out := req(map[string]any{"method": "POST", "path": "/tasks", "form": map[string]any{"title": " "}}); status(out) != 400 {
		t.Fatalf("empty title: %v", out)
	}
	var tasks []map[string]any
	api := req(map[string]any{"path": "/api/tasks"})
	if json.Unmarshal([]byte(api["body"].(string)), &tasks); len(tasks) != 2 || tasks[0]["done"] != float64(0) {
		t.Fatalf("api: %v", api["body"])
	}

	// No tool sends a form to live: submit_form has no stage, and view_live_page no form.
	if _, errText := call(t, s, "submit_form", map[string]any{"app": "jack-tasks", "stage": "live", "path": "/tasks"}); errText == "" {
		t.Fatal("submit_form took a stage")
	}
	if _, errText := call(t, s, "view_live_page", map[string]any{"app": "jack-tasks", "path": "/tasks", "form": map[string]any{"title": "x"}}); errText == "" {
		t.Fatal("view_live_page took a form")
	}
	// Another user's app is not reachable.
	other := e.browser()
	e.signup(other, "kim@shop.ae")
	os := e.mcpSession(e.connect(other, e.registerClient()).AccessToken)
	if _, errText := call(t, os, "view_page", map[string]any{"app": "jack-tasks", "path": "/"}); !strings.Contains(errText, "not found") {
		t.Fatalf("other user: %q", errText)
	}
}

func TestAgentPatchesOnePart(t *testing.T) {
	e := newEnv(t)
	c := e.browser()
	e.signup(c, "lia@shop.ae")
	s := e.mcpSession(e.connect(c, e.registerClient()).AccessToken)
	ex, _ := call(t, s, "get_example", map[string]any{"name": "tasks"})
	call(t, s, "create_app", map[string]any{"app": "lia-tasks"})
	call(t, s, "apply_change", map[string]any{"app": "lia-tasks", "definition": ex["definition"]})

	// Replace one template and add a column; everything else is kept.
	top := `<!doctype html><html><head><title>Lia</title></head><body><h1>Lia's tasks</h1>`
	out, errText := call(t, s, "patch_app", map[string]any{"app": "lia-tasks", "base_version": 1,
		"templates": map[string]any{"top": top},
		"tables":    []any{map[string]any{"name": "tasks", "columns": []any{map[string]any{"name": "priority", "type": "integer"}}}}})
	if errText != "" || out["ok"] != true || out["version"] != float64(2) {
		t.Fatalf("patch: %v %s", out, errText)
	}
	// Not published yet, so preview is rebuilt from seed data with the new column.
	if m := strings.Join(toStrings(out["migrations"]), "; "); m != "created table tasks; seeded tasks with 2 rows" {
		t.Fatalf("migrations: %s", m)
	}
	home, _ := call(t, s, "view_page", map[string]any{"app": "lia-tasks", "path": "/"})
	if body := home["body"].(string); !strings.Contains(body, "Lia's tasks") || !strings.Contains(body, "Order new oud stock") {
		t.Fatalf("after patch: %s", body)
	}
	got, _ := call(t, s, "get_app", map[string]any{"app": "lia-tasks"})
	def := got["definition"].(map[string]any)
	if cols := def["tables"].([]any)[0].(map[string]any)["columns"].([]any); len(cols) != 4 {
		t.Fatalf("columns: %v", cols)
	}
	if len(def["scripts"].(map[string]any)) != 7 || len(def["routes"].([]any)) != 7 {
		t.Fatalf("patch dropped parts: %v", def)
	}

	// A stale base version is refused; so is deleting something that does not exist.
	stale, _ := call(t, s, "patch_app", map[string]any{"app": "lia-tasks", "base_version": 1, "templates": map[string]any{"top": "x"}})
	if stale["ok"] != false || !strings.Contains(stale["problems"].([]any)[0].(string), "version 2 is the newest") {
		t.Fatalf("stale: %v", stale)
	}
	missing, _ := call(t, s, "patch_app", map[string]any{"app": "lia-tasks", "delete_scripts": []any{"nope"}})
	if missing["ok"] != false {
		t.Fatalf("delete missing: %v", missing)
	}
	// Deleting a script a route still uses is caught by validation.
	broken, _ := call(t, s, "patch_app", map[string]any{"app": "lia-tasks", "delete_scripts": []any{"toggle"}})
	if broken["ok"] != false || !strings.Contains(strings.Join(toStrings(broken["problems"]), " "), `script "toggle" is not defined`) {
		t.Fatalf("broken route: %v", broken)
	}
}

func toStrings(v any) []string {
	var out []string
	for _, x := range v.([]any) {
		out = append(out, x.(string))
	}
	return out
}

// Jobs and admin-panel changes are tested through tools of their own, each
// on preview only; the visitor tools take neither.
func TestAgentRunsJobAndChangesRowOnPreview(t *testing.T) {
	e := newEnv(t)
	c := e.browser()
	e.signup(c, "noor@shop.ae")
	s := e.mcpSession(e.connect(c, e.registerClient()).AccessToken)
	ex, _ := call(t, s, "get_example", map[string]any{"name": "store-uae"})
	call(t, s, "create_app", map[string]any{"app": "noor-shop"})
	if applied, _ := call(t, s, "apply_change", map[string]any{"app": "noor-shop", "definition": ex["definition"]}); applied["ok"] != true {
		t.Fatalf("apply: %v", applied)
	}
	out, errText := call(t, s, "try_scheduled_jobs", map[string]any{"app": "noor-shop"})
	runs, _ := out["runs"].([]any)
	if errText != "" || len(runs) != 1 || runs[0].(map[string]any)["job"] != "daily_report" || runs[0].(map[string]any)["body"] != "no orders today" ||
		runs[0].(map[string]any)["schedule"] != "daily at 21:00 Asia/Dubai" {
		t.Fatalf("try_scheduled_jobs: %v %s", out, errText)
	}
	// It takes only the app: no job name, stage or command chooses what runs.
	for _, extra := range []string{"job", "stage"} {
		if _, errText := call(t, s, "try_scheduled_jobs", map[string]any{"app": "noor-shop", extra: "x"}); errText == "" {
			t.Fatalf("try_scheduled_jobs took %s", extra)
		}
	}
	out, errText = call(t, s, "change_preview_row", map[string]any{"app": "noor-shop", "action": "update", "table": "products", "id": 2, "values": map[string]any{"price": 99}})
	if errText != "" || out["admin"].(map[string]any)["row"].(map[string]any)["price"] != float64(99) {
		t.Fatalf("change_preview_row: %v %s", out, errText)
	}
	// view_page only opens a page.
	for _, extra := range []string{"job", "method", "form"} {
		if _, errText := call(t, s, "view_page", map[string]any{"app": "noor-shop", "path": "/", extra: "POST"}); errText == "" {
			t.Fatalf("view_page accepted %s", extra)
		}
	}
	if _, errText := call(t, s, "view_page", map[string]any{"app": "noor-shop", "path": "/p/2"}); errText != "" {
		t.Fatal(errText)
	}
}

// A file a visitor shares and a look at the live site are tools of their
// own: upload_as_visitor needs a preview visitor, and view_live_page only reads.
func TestAgentUploadsAsVisitorAndViewsLive(t *testing.T) {
	e := newEnv(t)
	c := e.browser()
	e.signup(c, "noor@shop.ae")
	s := e.mcpSession(e.connect(c, e.registerClient()).AccessToken)
	if out, errText := call(t, s, "create_app", map[string]any{"app": "noor-shop", "from_example": "store-uae"}); errText != "" {
		t.Fatal(out, errText)
	}
	visitor, errText := call(t, s, "create_test_visitor", map[string]any{"app": "noor-shop", "name": "Fatima"})
	if errText != "" {
		t.Fatal(errText)
	}
	png := "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="
	up, errText := call(t, s, "upload_as_visitor", map[string]any{"app": "noor-shop", "as_visitor": visitor["email"], "name": "room.png", "data_base64": png})
	if errText != "" || up["status"] != float64(200) || !strings.Contains(up["body"].(string), `"id"`) {
		t.Fatalf("upload_as_visitor: %v %s", up, errText)
	}
	if _, errText := call(t, s, "upload_as_visitor", map[string]any{"app": "noor-shop", "name": "room.png", "data_base64": png}); errText == "" {
		t.Fatal("an upload without a visitor was accepted")
	}
	// Nothing is published yet, so the live site has no page to show.
	live, errText := call(t, s, "view_live_page", map[string]any{"app": "noor-shop", "path": "/"})
	if errText != "" || live["status"] != float64(404) {
		t.Fatalf("view_live_page before publishing: %v %s", live, errText)
	}
	preview, _ := call(t, s, "view_page", map[string]any{"app": "noor-shop", "path": "/"})
	if preview["status"] != float64(200) {
		t.Fatalf("view_page: %v", preview["status"])
	}
}
