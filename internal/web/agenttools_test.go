package web_test

import (
	"encoding/json"
	"strings"
	"testing"
)

// The agent builds the tasks example and tests every CRUD route on preview
// through MCP, the flow agents could not verify before.
func TestAgentTestsCRUDOnPreview(t *testing.T) {
	e := newEnv(t)
	c := e.browser()
	e.signup(c, "jack@shop.ae")
	s := e.mcpSession(e.connect(c, e.registerClient()).AccessToken)

	list, _ := call(t, s, "get_example", map[string]any{})
	if names := list["examples"].([]any); len(names) != 4 {
		t.Fatalf("examples = %v", list)
	}
	ex, errText := call(t, s, "get_example", map[string]any{"name": "tasks"})
	if errText != "" {
		t.Fatal(errText)
	}
	call(t, s, "create_app", map[string]any{"app": "jack-tasks"})
	applied, _ := call(t, s, "apply_change", map[string]any{"app": "jack-tasks", "definition": ex["definition"]})
	if applied["ok"] != true {
		t.Fatalf("apply: %v", applied)
	}

	req := func(args map[string]any) map[string]any {
		t.Helper()
		args["app"] = "jack-tasks"
		out, errText := call(t, s, "test_request", args)
		if errText != "" {
			t.Fatalf("test_request %v: %s", args, errText)
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

	// Live never takes a POST, and nothing is published yet.
	if _, errText := call(t, s, "test_request", map[string]any{"app": "jack-tasks", "stage": "live", "method": "POST", "path": "/tasks"}); !strings.Contains(errText, "only allowed on preview") {
		t.Fatalf("live POST: %q", errText)
	}
	// Another user's app is not reachable.
	other := e.browser()
	e.signup(other, "kim@shop.ae")
	os := e.mcpSession(e.connect(other, e.registerClient()).AccessToken)
	if _, errText := call(t, os, "test_request", map[string]any{"app": "jack-tasks", "path": "/"}); !strings.Contains(errText, "not found") {
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
	home, _ := call(t, s, "test_request", map[string]any{"app": "lia-tasks", "path": "/"})
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
