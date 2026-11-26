package web_test

import (
	"fmt"
	"strings"
	"testing"
)

// Business data changed only in the preview's test data (change_preview_row)
// is reported by get_app, and publish makes no confirm link until it is
// saved in the definition or dropped on purpose. Saving the definition
// rebuilds the preview, which clears the report.
func TestUnsavedPreviewChangesBlockPublish(t *testing.T) {
	e := newEnv(t)
	c := e.browser()
	e.signup(c, "sara@salon.ae")
	s := e.mcpSession(e.connect(c, e.registerClient()).AccessToken)
	if _, errText := call(t, s, "create_app", map[string]any{"app": "sara-salon", "from_example": "booking-uae"}); errText != "" {
		t.Fatal(errText)
	}
	// A test booking is test data: nothing to report.
	if out, _ := call(t, s, "get_app_outline", map[string]any{"app": "sara-salon"}); out["unsaved_preview_changes"] != nil {
		t.Fatalf("fresh app: %v", out["unsaved_preview_changes"])
	}
	if _, errText := call(t, s, "change_preview_row", map[string]any{"app": "sara-salon", "action": "update", "table": "services", "id": 1,
		"values": map[string]any{"name": "Keratin blow-dry"}}); errText != "" {
		t.Fatal(errText)
	}
	app, _ := call(t, s, "get_app", map[string]any{"app": "sara-salon"})
	if got := fmt.Sprint(app["unsaved_preview_changes"]); !strings.Contains(got, "services") || !strings.Contains(got, "name") || app["unsaved_note"] == nil {
		t.Fatalf("get_app: %v %v", app["unsaved_preview_changes"], app["unsaved_note"])
	}
	pub, errText := call(t, s, "publish", map[string]any{"app": "sara-salon"})
	if errText != "" || pub["confirm_url"] != nil || pub["unsaved_preview_changes"] == nil || !strings.Contains(fmt.Sprint(pub["note"]), "discard_preview_changes") {
		t.Fatalf("publish with unsaved rows: %v %s", pub, errText)
	}
	// The owner's confirm page warns too.
	resp, _ := c.Get(e.base + "/apps/sara-salon/publish?version=1")
	if page := read(t, resp); !strings.Contains(page, "Not in this version") || !strings.Contains(page, "services #1") {
		t.Fatalf("publish page: %s", page)
	}
	// Saving in the definition clears it.
	version := int(app["version"].(float64))
	if out, errText := call(t, s, "edit_app", map[string]any{"app": "sara-salon", "base_version": version, "edits": []any{
		map[string]any{"target": "templates.css_extra", "old_text": "", "new_text": "<style>:root{--r:1}</style>"}}}); errText != "" || out["ok"] != true {
		t.Fatalf("edit: %v %s", out, errText)
	}
	if pub, _ := call(t, s, "publish", map[string]any{"app": "sara-salon"}); pub["confirm_url"] == nil {
		t.Fatalf("publish after saving: %v", pub)
	}
	// Dropping them on purpose works too.
	call(t, s, "change_preview_row", map[string]any{"app": "sara-salon", "action": "delete", "table": "staff", "id": 1})
	if pub, _ := call(t, s, "publish", map[string]any{"app": "sara-salon"}); pub["confirm_url"] != nil {
		t.Fatalf("publish after a deleted staff row: %v", pub)
	}
	if pub, _ := call(t, s, "publish", map[string]any{"app": "sara-salon", "discard_preview_changes": true}); pub["confirm_url"] == nil {
		t.Fatalf("publish discarding: %v", pub)
	}
}

// The results that start building repeat the photo rule, which models
// follow better there than in the server instructions.
func TestBuildResultsSayNoWebImages(t *testing.T) {
	e := newEnv(t)
	c := e.browser()
	e.signup(c, "lina@shop.ae")
	s := e.mcpSession(e.connect(c, e.registerClient()).AccessToken)
	for _, tc := range []struct {
		tool string
		args map[string]any
	}{
		{"get_example", map[string]any{}},
		{"get_example", map[string]any{"name": "tasks"}},
		{"create_app", map[string]any{"app": "lina-shop", "from_example": "store-uae"}},
		{"create_app", map[string]any{"app": "lina-empty"}},
	} {
		out, errText := call(t, s, tc.tool, tc.args)
		if errText != "" || !strings.Contains(fmt.Sprint(out["note"]), "do not search the web for images") {
			t.Errorf("%s %v: note %v %s", tc.tool, tc.args, out["note"], errText)
		}
	}
}
