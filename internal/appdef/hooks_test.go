package appdef

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

func TestValidateLibAndOnAdminChange(t *testing.T) {
	base := func() *App {
		return &App{
			Scripts: map[string]string{"s": "function handle(req) { return {text: ''} }"},
			Routes:  []Route{{Method: "GET", Path: "/", Script: "s"}},
		}
	}
	ok := base()
	ok.OnAdminChange = "s"
	ok.Lib = map[string]string{"money": "function money(n) {}", "a_first": ""}
	if p := ok.Validate(); len(p) > 0 {
		t.Fatalf("valid definition refused: %v", p)
	}
	if got := ok.LibNames(); !slices.Equal(got, []string{"a_first", "money"}) {
		t.Fatalf("LibNames = %v", got)
	}

	bad := base()
	bad.OnAdminChange = "missing"
	bad.Lib = map[string]string{"Bad-Name": "", "": ""}
	got := strings.Join(bad.Validate(), "\n")
	for _, want := range []string{`on_admin_change: script "missing" is not defined`, `lib "Bad-Name": name must match`, `lib "": name must match`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing problem %q in:\n%s", want, got)
		}
	}

	many := base()
	many.Lib = map[string]string{}
	for i := 0; i <= MaxLibs; i++ {
		many.Lib[fmt.Sprintf("l%d", i)] = ""
	}
	if got := strings.Join(many.Validate(), "\n"); !strings.Contains(got, "lib: at most") {
		t.Errorf("too many libs accepted: %s", got)
	}
}

func TestParseCountsLibTowardsTheSizeLimit(t *testing.T) {
	big := `{"routes": [{"method": "GET", "path": "/", "template": "t"}], "templates": {"t": "x"}, "lib": {"big": "` + strings.Repeat("x", MaxSize) + `"}}`
	if _, p := Parse([]byte(big)); len(p) == 0 || !strings.Contains(p[0], "the limit is") {
		t.Fatalf("oversized lib accepted: %v", p)
	}
}
