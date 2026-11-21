package web_test

import (
	"context"
	"testing"
)

// Every builder tool says what it does to the user's apps: ChatGPT's and
// Claude's directories refuse tools without annotations, and assistants use
// them to ask before changing anything.
func TestBuilderToolsAreAnnotated(t *testing.T) {
	e := newEnv(t)
	c := e.browser()
	e.signup(c, "owner@shop.ae")
	sess := e.mcpSession(e.connect(c, e.registerClient()).AccessToken)
	res, err := sess.ListTools(context.Background(), nil)
	if err != nil || len(res.Tools) < 10 {
		t.Fatalf("tools: %v %d", err, len(res.Tools))
	}
	readOnly := map[string]bool{"list_apps": true, "get_app": true, "get_example": true, "get_logs": true, "view_live_page": true}
	for _, tool := range res.Tools {
		a := tool.Annotations
		if a == nil || a.Title == "" || a.DestructiveHint == nil || a.OpenWorldHint == nil {
			t.Errorf("%s: incomplete annotations %+v", tool.Name, a)
			continue
		}
		if a.ReadOnlyHint != readOnly[tool.Name] {
			t.Errorf("%s: readOnlyHint = %v", tool.Name, a.ReadOnlyHint)
		}
		if a.ReadOnlyHint && *a.DestructiveHint {
			t.Errorf("%s: read-only and destructive", tool.Name)
		}
	}
}
