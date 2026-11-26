package platform

import (
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/arif-rachim/renoxium/internal/appdef"
)

// browserCallRE finds a page's browser script calling another site:
// fetch("https://host/..."), XMLHttpRequest.open("GET", "https://host/..."),
// new EventSource or WebSocket to a full address.
var browserCallRE = regexp.MustCompile("(?i)(?:fetch\\s*\\(|\\.open\\s*\\(\\s*[\"'`][A-Z]+[\"'`]\\s*,|new\\s+(?:EventSource|WebSocket)\\s*\\()\\s*[\"'`](?:https?|wss?)://([a-z0-9.-]+)")

// browserFetchWarnings names templates whose browser script calls another
// site directly. Such a call may work (the site sends CORS headers) or
// fail for visitors (no CORS headers, the site refuses or limits it, an
// ad blocker), and none of the checks can tell: view_page does not run
// scripts and screenshot_preview does not reach other sites. The
// supported way is http.fetch in a script with the host declared.
func (p *Platform) browserFetchWarnings(app string, def *appdef.App) []string {
	own := strings.ToLower(p.cfg.AppsDomain)
	declared := map[string]bool{}
	for _, h := range def.Hosts {
		declared[strings.ToLower(h.Host)] = true
	}
	hosts := map[string][]string{}
	for name, src := range def.Templates {
		for _, m := range browserCallRE.FindAllStringSubmatch(src, -1) {
			h := strings.ToLower(m[1])
			if own != "" && (h == own || strings.HasSuffix(h, "."+own)) {
				continue
			}
			if !slices.Contains(hosts[h], name) {
				hosts[h] = append(hosts[h], name)
			}
		}
	}
	var out []string
	for h, names := range hosts {
		sort.Strings(names)
		undeclared := ""
		if !declared[h] {
			undeclared = fmt.Sprintf(" %s is not in the app's hosts.", h)
		}
		out = append(out, fmt.Sprintf("templates %s: the browser calls %s directly. This is not checked: view_page does not run "+
			"scripts and screenshot_preview does not reach other sites, so do not report it as working. It may fail for visitors "+
			"(the site sends no CORS headers, refuses or limits calls, or is blocked).%s The supported way is http.fetch in a "+
			"script, with %s in hosts (the owner allows it and turns on test calls for the preview), giving the page the result",
			strings.Join(names, ", "), h, undeclared, h))
	}
	sort.Strings(out)
	return out
}

var templateCallRE = regexp.MustCompile(`\{\{-?\s*(?:template|block)\s+"([^"]+)"`)

// reachableTemplates are the templates a page can render: those routes or
// scripts name, and those they include, at any depth.
func reachableTemplates(def *appdef.App) map[string]bool {
	used := map[string]bool{}
	var queue []string
	mark := func(name string) {
		if _, ok := def.Templates[name]; ok && !used[name] {
			used[name] = true
			queue = append(queue, name)
		}
	}
	for _, r := range def.Routes {
		mark(r.Template)
	}
	var code strings.Builder
	for _, src := range def.Scripts {
		code.WriteString(src)
	}
	for _, src := range def.Lib {
		code.WriteString(src)
	}
	scripts := code.String()
	for name := range def.Templates {
		for _, q := range []string{`"`, `'`, "`"} {
			if strings.Contains(scripts, q+name+q) {
				mark(name)
			}
		}
	}
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		for _, m := range templateCallRE.FindAllStringSubmatch(def.Templates[name], -1) {
			mark(m[1])
		}
	}
	return used
}

// unusedTemplateWarnings names templates this change added or changed
// (compared with prev) that visitors will not see: no page can render them
// at all, or none of the checked pages did (seen; nil when unknown), as
// when the template is included only under a condition that is off, such
// as store-uae's home_boutique without theme.boutique.
//
// A first version has nothing to compare with: an example's spare
// templates (other home page layouts) are not the assistant's edits.
func unusedTemplateWarnings(prev, def *appdef.App, seen map[string]bool) []string {
	if prev == nil {
		return nil
	}
	reach := reachableTemplates(def)
	var names []string
	for name, src := range def.Templates {
		if old, ok := prev.Templates[name]; (ok && old == src) || strings.TrimSpace(src) == "" {
			continue // not touched by this change
		}
		if !reach[name] || (seen != nil && !seen[name]) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	var out []string
	for _, name := range names {
		if !reach[name] {
			out = append(out, fmt.Sprintf("templates.%s was added or changed, but no page renders it: no route or script names it and no "+
				"rendered template includes it with {{template %q .}}. Visitors will not see this change; put it in a template a page uses "+
				"(see routes in get_app_outline)", name, name))
			continue
		}
		out = append(out, fmt.Sprintf("templates.%s was added or changed, but none of the checked pages rendered it: it is only included "+
			"under a condition that is off with the preview's data (for example {{if .theme.boutique}}), or only shown after a form is sent. "+
			"If it should be on a page, check that condition, then look at the page with screenshot_preview", name))
	}
	return out
}
