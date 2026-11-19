package platform_test

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/dop251/goja"

	"github.com/arif-rachim/renoxium/internal/platform"
)

// Running a page's own script: the price a visitor sees is often worked
// out in the browser (the store's cart, a service page's total), while the
// server works out what is charged. These helpers run the inline script of
// a page rendered by the app, with a small fake DOM and a fetch that asks
// the same app, so a test can compare what the page shows with what the
// server charges.

var inlineScriptRE = regexp.MustCompile(`(?s)<script(?:\s+nonce="[^"]*")?>(.*?)</script>`)

// fakeDOM is just enough of the browser for the templates' page scripts:
// elements by id that keep textContent, value, checked and hidden; forms
// and lists that accept children; fetch answered synchronously.
const fakeDOM = `
var __els = {};
function __node(tag, id) {
  var n = {tagName: (tag || 'div').toUpperCase(), id: id || '', textContent: '', value: '', checked: false, hidden: false, disabled: false,
    readOnly: false, required: false, className: '', type: '', name: '', href: '', src: '', alt: '', min: '', max: '',
    dataset: {}, style: {}, children: [], options: [], selectedIndex: 0, parentNode: null,
    classList: {add: function() {}, remove: function() {}, toggle: function() {}, contains: function() { return false; }},
    appendChild: function(c) { c.parentNode = this; this.children.push(c); return c; },
    removeChild: function(c) { this.children = this.children.filter(function(x) { return x !== c; }); return c; },
    setAttribute: function(k, v) { this['attr_' + k] = v; }, getAttribute: function(k) { return this['attr_' + k] || null; },
    addEventListener: function(ev, f) { (this.__on = this.__on || {})[ev] = f; },
    querySelector: function() { return null; }, querySelectorAll: function() { return []; },
    focus: function() {}, scrollIntoView: function() {}, closest: function() { return null; }};
  return n;
}
function __el(id) { if (!__els[id]) __els[id] = __node('div', id); return __els[id]; }
var __bodyNode = __node('body');
var document = {
  getElementById: __el,
  createElement: function(tag) { return __node(tag); },
  querySelector: function(sel) { var m = /^#([\w-]+)$/.exec(sel); if (m) return __el(m[1]); if (sel in __selectors) return __selectors[sel]; return __selectors[sel] = /:checked$/.test(sel) ? null : __node('div'); },
  querySelectorAll: function(sel) { return __lists[sel] || []; },
  addEventListener: function() {},
  body: __bodyNode, documentElement: __node('html')
};
var __selectors = {}, __lists = {};
var window = {addEventListener: function() {}, location: {href: '', search: ''}, document: document};
var navigator = {language: 'en'};
var location = window.location;
function setTimeout(f) { f(); return 0; }
function clearTimeout() {}
function __thenable(v) {
  if (v && typeof v.then === 'function') return v;
  return {then: function(f) { return __thenable(f ? f(v) : v); }, catch: function() { return this; }, finally: function(f) { if (f) f(); return this; }};
}
function fetch(url) {
  var r = __fetch(String(url));
  return __thenable({ok: r.status < 400, status: r.status, json: function() { return __thenable(JSON.parse(r.body)); }, text: function() { return __thenable(r.body); }});
}
function URLSearchParams(o) { this.o = o || {}; this.toString = function() { var p = [], o = this.o; for (var k in o) p.push(encodeURIComponent(k) + '=' + encodeURIComponent(o[k])); return p.join('&'); }; }
`

// pageScript runs, in a fresh VM, setup (to give elements their starting
// values) and then the inline script of html that contains marker. fetch
// requests go to the app as GET test requests, as the visitor in email.
func pageScript(t *testing.T, p *platform.Platform, app, email, html, marker, setup string) *goja.Runtime {
	t.Helper()
	var script string
	for _, m := range inlineScriptRE.FindAllStringSubmatch(html, -1) {
		if strings.Contains(m[1], marker) {
			script = m[1]
		}
	}
	if script == "" {
		t.Fatalf("no inline script with %q on the page", marker)
	}
	vm := goja.New()
	vm.Set("__fetch", func(url string) map[string]any {
		r, err := p.TestRequest(context.Background(), app, platform.TestRequestInput{Path: url, UserEmail: email})
		if err != nil {
			t.Fatalf("page fetch %s: %v", url, err)
		}
		return map[string]any{"status": r.Status, "body": r.Body}
	})
	if _, err := vm.RunString(fakeDOM); err != nil {
		t.Fatal(err)
	}
	if _, err := vm.RunString(setup); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if _, err := vm.RunString(script); err != nil {
		t.Fatalf("page script: %v", err)
	}
	return vm
}

// text is the textContent the page script gave the element with id.
func text(t *testing.T, vm *goja.Runtime, id string) string {
	t.Helper()
	v, err := vm.RunString(`__el(` + quoteJS(id) + `).textContent`)
	if err != nil {
		t.Fatal(err)
	}
	return v.String()
}

func quoteJS(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}
