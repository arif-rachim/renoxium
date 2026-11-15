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
