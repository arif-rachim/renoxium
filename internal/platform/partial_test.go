package platform_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/arif-rachim/renoxium/internal/platform"
)

// An agent starts from an example without sending it, reads only the parts
// it needs, and changes one CSS rule and one line of a script in one small
// call that applies all edits or none.
func TestPartialReadsAndEdits(t *testing.T) {
	p := newPlatform(t)
	ctx := context.Background()
	info, res, err := p.CreateFromExample(ctx, "usr_test", "sparkle", "home-service-uae")
	if err != nil || !res.OK || res.Version != 1 || info.PreviewVersion != 1 {
		t.Fatalf("create from example: %v %+v %+v", err, res, info)
	}
	if _, _, err := p.CreateFromExample(ctx, "usr_test", "sparkle-two", "no-such-example"); err == nil || !strings.Contains(err.Error(), "home-service-uae") {
		t.Fatalf("unknown example: %v", err)
	}

	def, n, _ := p.Definition("sparkle", 0)
	o := platform.OutlineOf(def)
	if o.Templates["css"] < 10000 || o.Scripts["book"] == 0 || o.Seed["texts"] == 0 || len(o.Routes) == 0 || o.Bytes < 100000 {
		t.Fatalf("outline: %+v", o)
	}
	if raw, _ := json.Marshal(o); len(raw) > 6000 {
		t.Fatalf("outline is %d bytes", len(raw))
	}
	parts, err := platform.SelectParts(def, []string{"templates.css", "scripts.book", "lib.dates", "seed.texts", "tables"})
	if err != nil || parts["scripts.book"] != def.Scripts["book"] || len(parts) != 5 {
		t.Fatalf("parts: %v %d", err, len(parts))
	}
	if _, err := platform.SelectParts(def, []string{"scripts.nope", "templatez"}); err == nil || !strings.Contains(err.Error(), "scripts.nope") || !strings.Contains(err.Error(), "templatez") {
		t.Fatalf("bad paths: %v", err)
	}

	// One CSS rule and one script line, in a call far under 2 KB.
	css, book := def.Templates["css"], def.Scripts["book"]
	cssOld := firstLine(t, css, "--paper:")
	bookOld := firstLine(t, book, "const f = req.form")
	edits := []platform.Edit{
		{Target: "css", OldText: cssOld, NewText: cssOld + " /* edited */"},
		{Target: "scripts.book", OldText: bookOld, NewText: bookOld + " // edited"},
	}
	if raw, _ := json.Marshal(map[string]any{"app": "sparkle", "base_version": n, "edits": edits}); len(raw) > 2048 {
		t.Fatalf("edit call is %d bytes", len(raw))
	}
	er, err := p.ApplyEdits(ctx, "sparkle", n, edits)
	if err != nil || !er.OK || er.Version != n+1 || len(er.Applied) != 2 || len(er.Checks) == 0 {
		t.Fatalf("edits: %v %+v", err, er)
	}
	after, _, _ := p.Definition("sparkle", 0)
	if !strings.Contains(after.Templates["css"], cssOld+" /* edited */") || !strings.Contains(after.Scripts["book"], bookOld+" // edited") ||
		len(after.Templates["css"]) != len(css)+len(" /* edited */") {
		t.Fatal("edits not in the new version")
	}

	// A stale base version, a missing text, an ambiguous text: nothing changes.
	for _, c := range []struct {
		base  int
		edits []platform.Edit
		want  string
	}{
		{n, edits, "base_version must be the newest version"},
		{n + 1, []platform.Edit{{Target: "css", OldText: "--radius:-1px", NewText: "x"}}, "was not found"},
		{n + 1, []platform.Edit{{Target: "templates.css", OldText: "{", NewText: "x"}}, "occurs"},
		{n + 1, []platform.Edit{{Target: "lib.dates", OldText: "function", NewText: "function"}, {Target: "scripts.missing", OldText: "a", NewText: "b"}}, "no scripts.missing"},
		{n + 1, []platform.Edit{{Target: "routes.x", OldText: "a", NewText: "b"}}, "templates.<name>"},
		{n + 1, []platform.Edit{{Target: "templates.css", OldText: "", NewText: "b"}}, "old_text is empty"},
	} {
		r, err := p.ApplyEdits(ctx, "sparkle", c.base, c.edits)
		if err != nil || r.OK || !strings.Contains(strings.Join(r.Problems, " "), c.want) {
			t.Fatalf("want %q: %v %+v", c.want, err, r)
		}
	}
	if _, v, _ := p.Definition("sparkle", 0); v != n+1 {
		t.Fatalf("a refused edit made version %d", v)
	}
	// Spacing that differs is pointed out.
	r, _ := p.ApplyEdits(ctx, "sparkle", n+1, []platform.Edit{{Target: "scripts.book", OldText: "  " + strings.ReplaceAll(strings.TrimSpace(bookOld), " ", "  "), NewText: "x"}})
	if !strings.Contains(strings.Join(r.Problems, " "), "is similar") {
		t.Fatalf("near miss: %+v", r.Problems)
	}

	// A new entry, and an edit that breaks a template is caught by the checks.
	r2, err := p.ApplyEdits(ctx, "sparkle", n+1, []platform.Edit{{Target: "templates.css_extra", NewText: ":root{--brand:#0a7}"}})
	if err != nil || !r2.OK || !strings.Contains(strings.Join(r2.Applied, " "), "filled") {
		t.Fatalf("css_extra: %v %+v", err, r2)
	}
	// Every example's head includes css_extra after the base css.
	if home, _ := p.TestRequest(ctx, "sparkle", platform.TestRequestInput{Path: "/"}); !strings.Contains(home.Body, ":root{--brand:#0a7}") {
		t.Fatal("css_extra is not on the page")
	}
	r3, _ := p.ApplyEdits(ctx, "sparkle", n+2, []platform.Edit{{Target: "templates.css_extra", OldText: ":root", NewText: "{{if}}:root"}})
	if r3.OK {
		t.Fatal("broken template accepted")
	}
}

// firstLine returns the first line of text containing want, which must
// occur once.
func firstLine(t *testing.T, text, want string) string {
	t.Helper()
	for _, l := range strings.Split(text, "\n") {
		if strings.Contains(l, want) && strings.Count(text, l) == 1 && strings.TrimSpace(l) != "" {
			return l
		}
	}
	t.Fatalf("no unique line with %q", want)
	return ""
}
