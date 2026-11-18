package sandbox

import (
	"context"
	"regexp"
	"strings"
	"testing"
)

type libHost struct {
	fakeHost
	lib []Lib
}

func (h *libHost) Lib() []Lib { return h.lib }

var testLib = []Lib{
	{Name: "a_money", Src: "const VAT = 0.05;\nfunction money(n) { return (Math.round(n * 100) / 100).toFixed(2); }\n"},
	{Name: "b_quote", Src: "// uses a_money, which runs first\nfunction total(n) { return money(n * (1 + VAT)); }\nfunction boom(o) {\n  return o.missing.field;\n}\n"},
}

// Both sandboxes behave the same with libraries.
func sandboxes(t *testing.T) map[string]Sandbox {
	return map[string]Sandbox{"goja": NewGoja(), "pool": newPool(t, 1)}
}

func TestLibIsAvailableToScripts(t *testing.T) {
	for name, sb := range sandboxes(t) {
		h := &libHost{lib: testLib}
		r, err := sb.Run(context.Background(), `function handle(req) { return {text: total(100) + ' ' + VAT} }`, Request{Method: "GET", Path: "/"}, h)
		if err != nil || r.Text != "105.00 0.05" {
			t.Errorf("%s: %+v %v", name, r, err)
		}
		// Without a LibHost the helpers are not there.
		if _, err := sb.Run(context.Background(), `function handle(req) { return {text: total(1)} }`, Request{}, &h.fakeHost); err == nil || !strings.Contains(err.Error(), "total is not defined") {
			t.Errorf("%s without lib: %v", name, err)
		}
	}
}

func TestLibErrorsPointAtTheLibraryAndLine(t *testing.T) {
	for name, sb := range sandboxes(t) {
		h := &libHost{lib: testLib}
		_, err := sb.Run(context.Background(), "function handle(req) {\n  return {text: boom({})};\n}", Request{}, h)
		if err == nil || !strings.Contains(err.Error(), "at boom (lib/b_quote:4:") || !strings.Contains(err.Error(), "at handle (script:2:") {
			t.Errorf("%s: stack does not point at lib and script lines: %v", name, err)
		}
		// Top-level failures of a library name it.
		h.lib = []Lib{{Name: "bad", Src: "const a = 1;\n\nnull.x;"}}
		_, err = sb.Run(context.Background(), "function handle(req) { return {text: 'x'} }", Request{}, h)
		if err == nil || !strings.Contains(err.Error(), `lib "bad"`) || !strings.Contains(err.Error(), "lib/bad:3:") {
			t.Errorf("%s: top-level lib error: %v", name, err)
		}
	}
}

func TestCheckLib(t *testing.T) {
	for name, sb := range sandboxes(t) {
		if err := sb.CheckLib(testLib, `function handle(req) { return {text: total(1)} }`); err != nil {
			t.Errorf("%s: %v", name, err)
		}
		if err := sb.CheckLib(testLib, ""); err != nil {
			t.Errorf("%s: lib alone: %v", name, err)
		}
		err := sb.CheckLib([]Lib{{Name: "ok", Src: "function f() {}"}, {Name: "broken", Src: "function f2() {\n  return 1 +;\n}"}}, "")
		if err == nil || !strings.Contains(err.Error(), `lib "broken"`) || !strings.Contains(err.Error(), "lib/broken:2:") {
			t.Errorf("%s: syntax error: %v", name, err)
		}
		err = sb.CheckLib([]Lib{{Name: "h", Src: "function handle(req) { return {text: 'lib'} }"}}, "")
		if err == nil || !strings.Contains(err.Error(), `lib "h" defines handle`) {
			t.Errorf("%s: handle in lib: %v", name, err)
		}
		// A script that redeclares a library constant is refused.
		if err := sb.CheckLib(testLib, "const VAT = 1;\nfunction handle(req) { return {text: ''} }"); err == nil || !strings.Contains(err.Error(), "VAT") {
			t.Errorf("%s: redeclared constant: %v", name, err)
		}
	}
}

func TestCrypto(t *testing.T) {
	for name, sb := range sandboxes(t) {
		r, err := sb.Run(context.Background(), `function handle(req) {
			return {json: {a: crypto.randomId(), b: crypto.randomId(), short: crypto.randomId(8), long: crypto.randomId(64),
				hash: crypto.sha256('abc'), empty: crypto.sha256(''), num: crypto.sha256(12)}};
		}`, Request{}, &fakeHost{})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		m := r.JSON.(map[string]any)
		b64 := regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
		for k, n := range map[string]int{"a": 22, "b": 22, "short": 11, "long": 86} {
			if s, _ := m[k].(string); len(s) != n || !b64.MatchString(s) {
				t.Errorf("%s: %s = %q, want %d url-safe characters", name, k, s, n)
			}
		}
		if m["a"] == m["b"] {
			t.Errorf("%s: two random ids are equal", name)
		}
		if m["hash"] != "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" ||
			m["empty"] != "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" ||
			m["num"] != "6b51d431df5d7f141cbececcf79edf3dd861c3b4069f0b11661a3eefacbba918" {
			t.Errorf("%s: sha256 = %v", name, m)
		}
		for _, bad := range []string{"crypto.randomId(7)", "crypto.randomId(65)", "crypto.randomId(10.5)", "crypto.randomId('x')", "crypto.sha256()"} {
			_, err := sb.Run(context.Background(), `function handle(req) { return {text: `+bad+`} }`, Request{}, &fakeHost{})
			if err == nil || !strings.Contains(err.Error(), "TypeError") {
				t.Errorf("%s: %s: %v", name, bad, err)
			}
		}
	}
}
