package mcpserver

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// What directory reviewers (OpenAI's scan) read: the server instructions
// and every tool's description, input schema and annotations.
func TestWhatReviewersRead(t *testing.T) {
	s := New(nil, Options{})
	ct, st := mcp.NewInMemoryTransports()
	ctx := context.Background()
	if _, err := s.Connect(ctx, st, nil); err != nil {
		t.Fatal(err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "review"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	instr := cs.InitializeResult().Instructions
	res, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Short instructions: the workflow, not the reference (that is in get_example).
	if len(instr) > 4000 || strings.Contains(instr, "payments.checkout") || !strings.Contains(reference, "payments.checkout") {
		t.Errorf("instructions are %d characters; the reference belongs in get_example", len(instr))
	}
	const idRule = "Never ask users for government-issued ID numbers or ID documents (passport, Emirates ID, driver's licence, visa)."
	for _, want := range []string{idRule, "there are no other operations", "You never execute them"} {
		if !strings.Contains(instr, want) {
			t.Errorf("instructions lack %q", want)
		}
	}
	// No government ID words anywhere else that reviewers read.
	idWords := regexp.MustCompile(`(?i)passport|emirates id|national id|licen[cs]e|\bssn\b|\bvisas?\b|\bkyc\b`)
	all, _ := json.Marshal(res.Tools)
	for _, text := range []string{strings.Replace(instr, idRule, "", 1), string(all)} {
		if m := idWords.FindString(text); m != "" {
			i := strings.Index(text, m)
			t.Errorf("reviewers read %q: …%s…", m, text[max(0, i-80):min(len(text), i+40)])
		}
	}
	if m := idWords.FindAllString(strings.Replace(reference, "(passport, Emirates ID, driver's licence, visa)", "", 1), -1); len(m) > 0 {
		t.Errorf("the reference mentions %v", m)
	}

	want := map[string]struct{ readOnly, destructive, openWorld bool }{
		"list_apps": {true, false, false}, "get_app": {true, false, false}, "get_example": {true, false, false}, "get_logs": {true, false, false},
		"create_app": {false, false, false}, "apply_change": {false, true, true}, "patch_app": {false, true, true},
		"upload_file": {false, false, true}, "test_request": {false, false, true}, "publish": {false, false, false},
	}
	if len(res.Tools) != len(want) {
		t.Fatalf("%d tools, want %d", len(res.Tools), len(want))
	}
	for _, tool := range res.Tools {
		w, a := want[tool.Name], tool.Annotations
		if a == nil || a.Title == "" || a.ReadOnlyHint != w.readOnly || a.DestructiveHint == nil || *a.DestructiveHint != w.destructive ||
			a.OpenWorldHint == nil || *a.OpenWorldHint != w.openWorld {
			t.Errorf("%s annotations %+v, want %+v", tool.Name, a, w)
		}
		// Every input property is described and no other property is accepted.
		var schema map[string]any
		b, _ := json.Marshal(tool.InputSchema)
		json.Unmarshal(b, &schema)
		checkSchema(t, tool.Name, schema)
	}
	says := map[string]string{"create_app": "never executed by you", "apply_change": "never executed by you", "patch_app": "never executed by you",
		"publish": "changes nothing live", "test_request": "nothing on the live site is modified"}
	for _, tool := range res.Tools {
		if want, ok := says[tool.Name]; ok && !strings.Contains(tool.Description, want) {
			t.Errorf("%s's description does not say %q", tool.Name, want)
		}
	}
}

func checkSchema(t *testing.T, path string, s map[string]any) {
	t.Helper()
	if props, ok := s["properties"].(map[string]any); ok {
		if s["additionalProperties"] != false {
			t.Errorf("%s accepts other properties", path)
		}
		for k, v := range props {
			p, _ := v.(map[string]any)
			if d, _ := p["description"].(string); d == "" {
				t.Errorf("%s.%s has no description", path, k)
			}
			checkSchema(t, path+"."+k, p)
		}
	}
	if items, ok := s["items"].(map[string]any); ok {
		checkSchema(t, path+"[]", items)
	}
}
