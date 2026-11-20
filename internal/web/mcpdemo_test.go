package web_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// The agent tools for large apps through MCP: start from an example,
// read an outline and one part, edit two lines in a small call, make a test
// visitor, book and follow to the booking, cancel. Live never changes.
func TestAgentToolsForLargeApps(t *testing.T) {
	e := newEnv(t)
	c := e.browser()
	e.signup(c, "maya@clean.ae")
	s := e.mcpSession(e.connect(c, e.registerClient()).AccessToken)

	created, errText := call(t, s, "create_app", map[string]any{"app": "maya-clean", "from_example": "home-service-uae"})
	if errText != "" || created["version"] != float64(1) || created["apply"].(map[string]any)["ok"] != true || created["outline"] == nil {
		t.Fatalf("create_app from_example: %s %v", errText, created["apply"])
	}
	if raw, _ := json.Marshal(created); len(raw) > 12000 {
		t.Fatalf("create_app answered %d bytes", len(raw))
	}
	if _, errText := call(t, s, "create_app", map[string]any{"app": "maya-two", "from_example": "nope"}); !strings.Contains(errText, "home-service-uae") {
		t.Fatalf("unknown example: %s", errText)
	}

	outline, _ := call(t, s, "get_app", map[string]any{"app": "maya-clean", "outline": true})
	if outline["definition"] != nil || outline["outline"].(map[string]any)["templates"].(map[string]any)["css"] == nil {
		t.Fatalf("outline: %v", outline)
	}
	parts, _ := call(t, s, "get_app", map[string]any{"app": "maya-clean", "only": []string{"scripts.book", "routes"}})
	book, _ := parts["parts"].(map[string]any)["scripts.book"].(string)
	if !strings.Contains(book, "function handle") || parts["definition"] != nil {
		t.Fatalf("only: %v", parts)
	}
	if _, errText := call(t, s, "get_app", map[string]any{"app": "maya-clean", "only": []string{"scripts.nope"}}); !strings.Contains(errText, "scripts.nope") {
		t.Fatalf("bad path: %s", errText)
	}
	ex, _ := call(t, s, "get_example", map[string]any{"name": "store-uae", "only": []string{"routes"}})
	if ex["definition"] != nil || ex["reference"] != nil || ex["parts"].(map[string]any)["routes"] == nil {
		t.Fatalf("get_example only: %v", ex)
	}

	line := "const f = req.form, b = biz();"
	args := map[string]any{"app": "maya-clean", "base_version": 1, "edits": []map[string]string{
		{"target": "css", "old_text": "[hidden]{display:none!important}", "new_text": "[hidden]{display:none!important}\nh1{letter-spacing:-.02em}"},
		{"target": "scripts.book", "old_text": line, "new_text": line + " console.log('booking for', f.service);"},
	}}
	if raw, _ := json.Marshal(args); len(raw) >= 2048 {
		t.Fatalf("edit is %d bytes", len(raw))
	}
	edited, errText := call(t, s, "edit_app", args)
	if errText != "" || edited["ok"] != true || edited["version"] != float64(2) {
		t.Fatalf("edit_app: %s %v", errText, edited)
	}
	if stale, _ := call(t, s, "edit_app", args); stale["ok"] == true {
		t.Fatal("stale base_version accepted")
	}

	v, errText := call(t, s, "create_test_visitor", map[string]any{"app": "maya-clean", "name": "Fatima"})
	email, _ := v["email"].(string)
	if errText != "" || email == "" {
		t.Fatalf("create_test_visitor: %s %v", errText, v)
	}
	date := time.Now().UTC().Add(4*time.Hour).AddDate(0, 0, 3)
	if date.Weekday() == time.Sunday {
		date = date.AddDate(0, 0, 1)
	}
	booked, errText := call(t, s, "test_request", map[string]any{"app": "maya-clean", "method": "POST", "path": "/book", "user_email": email, "follow_redirects": true,
		"form": map[string]string{"service": "1", "qty": "2", "zone": "1", "date": date.Format("2006-01-02"), "slot": "10:00", "repeat": "once", "payment": "cash",
			"name": "Fatima", "phone": "+971501234567", "address": "Villa 12", "flat": "12"}})
	logs, _ := json.Marshal(booked["logs"])
	if errText != "" || booked["status"] != float64(200) || !strings.Contains(string(logs), "booking for 1") || !strings.Contains(booked["body"].(string), "Booking #1") {
		t.Fatalf("booking: %s %v %v", errText, booked["status"], booked["redirects"])
	}
	cancelled, _ := call(t, s, "test_request", map[string]any{"app": "maya-clean", "method": "POST", "path": "/bookings/1/cancel", "user_email": email, "follow_redirects": true})
	if !strings.Contains(cancelled["body"].(string), "Cancelled") {
		t.Fatalf("cancel: %v", cancelled["status"])
	}
	// Screenshots need a browser on the server; without one the tool says so.
	if _, errText := call(t, s, "screenshot_preview", map[string]any{"app": "maya-clean", "path": "/"}); !strings.Contains(errText, "not set up") {
		t.Fatalf("screenshot without a browser: %s", errText)
	}
	info, _ := call(t, s, "get_app", map[string]any{"app": "maya-clean", "outline": true})
	if info["live_version"] != float64(0) {
		t.Fatal("live changed")
	}
}
