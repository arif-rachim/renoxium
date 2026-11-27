package platform

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/arif-rachim/renoxium/internal/appdef"
)

var (
	idAttrRE     = regexp.MustCompile(`\bid\s*=\s*["']([A-Za-z][\w-]*)["']`)
	formActionRE = regexp.MustCompile(`<form\b[^>]*\baction\s*=\s*["']([^"'{}]+)["']`)
	jsFuncRE     = regexp.MustCompile(`\bfunction\s+([A-Za-z_$][\w$]*)\s*\(`)
)

// removedParts lists what def no longer has that prev had: routes,
// templates, scripts, elements with an id, form targets and script
// functions. A change meant to add something can drop working features
// (barakah-distribution's v11 lost the map, the km and "Optimize route"):
// the list lets the assistant see it and tell the owner.
func removedParts(prev, def *appdef.App) []string {
	if prev == nil {
		return nil
	}
	var out []string
	routes := map[string]bool{}
	for _, r := range def.Routes {
		routes[r.Method+" "+r.Path] = true
	}
	for _, r := range prev.Routes {
		if !routes[r.Method+" "+r.Path] {
			out = append(out, "route "+r.Method+" "+r.Path)
		}
	}
	for _, kind := range []struct {
		name      string
		old, next map[string]string
	}{{"template", prev.Templates, def.Templates}, {"script", prev.Scripts, def.Scripts}, {"lib", prev.Lib, def.Lib}} {
		for _, name := range sortedKeys(kind.old) {
			if _, ok := kind.next[name]; !ok {
				out = append(out, kind.name+" "+name)
			}
		}
	}
	all := func(m map[string]string) string {
		var b strings.Builder
		for _, k := range sortedKeys(m) {
			b.WriteString(m[k])
			b.WriteString("\n")
		}
		return b.String()
	}
	oldT, newT := all(prev.Templates), all(def.Templates)
	oldCode, newCode := oldT+all(prev.Scripts)+all(prev.Lib), newT+all(def.Scripts)+all(def.Lib)
	for _, f := range []struct {
		what string
		re   *regexp.Regexp
		old  string
		new  string
	}{
		{"element id", idAttrRE, oldT, newT},
		{"form sent to", formActionRE, oldT, newT},
		{"script function", jsFuncRE, oldCode, newCode},
	} {
		have := map[string]bool{}
		for _, m := range f.re.FindAllStringSubmatch(f.new, -1) {
			have[m[1]] = true
		}
		seen := map[string]bool{}
		var gone []string
		for _, m := range f.re.FindAllStringSubmatch(f.old, -1) {
			if !have[m[1]] && !seen[m[1]] {
				seen[m[1]] = true
				gone = append(gone, m[1])
			}
		}
		sort.Strings(gone)
		if len(gone) > 12 {
			gone = append(gone[:12], fmt.Sprintf("and %d more", len(gone)-12))
		}
		if len(gone) > 0 {
			out = append(out, f.what+" "+strings.Join(gone, ", "))
		}
	}
	return out
}

// changedParts lists what def added or changed since prev.
func changedParts(prev, def *appdef.App) []string {
	var out []string
	for _, kind := range []struct {
		name      string
		old, next map[string]string
	}{{"template", nil, def.Templates}, {"script", nil, def.Scripts}, {"lib", nil, def.Lib}} {
		var old map[string]string
		if prev != nil {
			switch kind.name {
			case "template":
				old = prev.Templates
			case "script":
				old = prev.Scripts
			case "lib":
				old = prev.Lib
			}
		}
		for _, name := range sortedKeys(kind.next) {
			if src, ok := old[name]; !ok {
				out = append(out, "added "+kind.name+" "+name)
			} else if src != kind.next[name] {
				out = append(out, "changed "+kind.name+" "+name)
			}
		}
	}
	routes := map[string]bool{}
	if prev != nil {
		for _, r := range prev.Routes {
			routes[r.Method+" "+r.Path] = true
		}
	}
	for _, r := range def.Routes {
		if !routes[r.Method+" "+r.Path] {
			out = append(out, "added route "+r.Method+" "+r.Path)
		}
	}
	if prev == nil || fmt.Sprint(prev.Tables) != fmt.Sprint(def.Tables) {
		out = append(out, "tables")
	}
	if prev == nil || fmt.Sprint(prev.Seed) != fmt.Sprint(def.Seed) {
		out = append(out, "seed rows")
	}
	return out
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// ChangeSummary is what the newest version saved, compared with the one
// before: after a broken turn ("Error in message stream" in the chat), the
// next turn sees what was stored.
type ChangeSummary struct {
	Version int      `json:"version"`
	SavedAt string   `json:"saved_at" jsonschema:"UTC"`
	Changed []string `json:"changed,omitempty" jsonschema:"what this version added or changed"`
	Removed []string `json:"removed,omitempty" jsonschema:"what the version before had and this one does not"`
}

// LastChange summarises the app's newest version; nil before the first.
func (p *Platform) LastChange(app string) *ChangeSummary {
	def, n, err := p.Definition(app, 0)
	if err != nil || def == nil || n == 0 {
		return nil
	}
	var prev *appdef.App
	if n > 1 {
		prev, _, _ = p.Definition(app, n-1)
	}
	out := &ChangeSummary{Version: n, Changed: changedParts(prev, def), Removed: removedParts(prev, def)}
	p.db.QueryRow(`SELECT created_at FROM versions WHERE app = ? AND n = ?`, app, n).Scan(&out.SavedAt)
	return out
}
