package appdef

import (
	"strings"
	"testing"
)

func TestValidateReportsProblems(t *testing.T) {
	a := &App{
		Tables:    []Table{{Name: "Products", Columns: []Column{{Name: "id", Type: "text"}, {Name: "x", Type: "money"}}}},
		Templates: map[string]string{"home": "{{.Broken"},
		Routes: []Route{
			{Method: "GET", Path: "/", Template: "missing"},
			{Method: "PUT", Path: "nope", Script: "s"},
			{Method: "GET", Path: "/_rx/log", Template: "home"},
		},
	}
	got := strings.Join(a.Validate(), "\n")
	for _, want := range []string{"table \"Products\"", "added automatically", "unknown type \"money\"", "templates:", "method must be GET or POST",
		"path must start with /", "reserved", "script \"s\" is not defined"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing problem %q in:\n%s", want, got)
		}
	}
}

func TestMatch(t *testing.T) {
	a := &App{Routes: []Route{{Method: "GET", Path: "/"}, {Method: "GET", Path: "/products/:id"}}}
	if r, _ := a.Match("GET", "/"); r == nil || r.Path != "/" {
		t.Fatal("root not matched")
	}
	r, p := a.Match("GET", "/products/42")
	if r == nil || p["id"] != "42" {
		t.Fatalf("got %v %v", r, p)
	}
	if r, _ := a.Match("POST", "/"); r != nil {
		t.Fatal("method ignored")
	}
}

func TestFixStyleSlots(t *testing.T) {
	head := `<head>{{template "css" .}}{{template "css_extra" .}}</head>`
	a := &App{Templates: map[string]string{"page": head, "css": "<style>a{}</style>", "css_extra": ".hero{color:red}"}}
	fixed, problems := a.FixStyleSlots()
	if len(fixed) != 1 || len(problems) != 0 || a.Templates["css_extra"] != "<style>\n.hero{color:red}\n</style>" {
		t.Fatalf("bare CSS: %q %q %q", fixed, problems, a.Templates["css_extra"])
	}
	a.Templates["css_extra"] = "<style>a{}</style>\n.hero{color:red}"
	if _, problems := a.FixStyleSlots(); len(problems) != 1 || !strings.Contains(problems[0], "show as text") {
		t.Fatalf("text after style: %q", problems)
	}
	a.Templates["css_extra"] = "{{if .x}}<style>a{}</style>{{end}}<link rel=\"stylesheet\" href=\"https://fonts.googleapis.com/x\">"
	if fixed, problems := a.FixStyleSlots(); len(fixed)+len(problems) != 0 {
		t.Fatalf("good slot: %q %q", fixed, problems)
	}
	// Included inside <style> by the page: the slot is CSS by design.
	a.Templates["page"] = `<style>{{template "css_extra" .}}</style>`
	a.Templates["css_extra"] = ".hero{color:red}"
	if fixed, problems := a.FixStyleSlots(); len(fixed)+len(problems) != 0 || a.Templates["css_extra"] != ".hero{color:red}" {
		t.Fatalf("inside style: %q %q", fixed, problems)
	}
}
