package platform

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/arif-rachim/renoxium/internal/appdef"
)

// maxBrowserChecks is how many pages a change opens in a real browser.
const maxBrowserChecks = 3

// browserChecks opens, in a real browser, the GET pages a change touched
// that run browser scripts, and reports scripts that fail: the server-side
// checks render HTML without running it, so a page whose script has a
// syntax error passes them and shows nothing to visitors. Up to
// maxBrowserChecks pages; nothing without a browser on the server.
func (p *Platform) browserChecks(ctx context.Context, st *stage, prev, def *appdef.App) []string {
	if !p.ScreenshotsOn() {
		return nil
	}
	var warns []string
	var checkUser int64
	defer func() {
		if checkUser != 0 {
			st.admin.Exec(`DELETE FROM rx_sessions WHERE user_id = ?`, checkUser)
			st.admin.Exec(`DELETE FROM rx_users WHERE id = ?`, checkUser)
		}
	}()
	n := 0
	for _, r := range touchedScriptRoutes(prev, def) {
		if n == maxBrowserChecks {
			break
		}
		path, user := r.Path, int64(0)
		if r.HasParams() {
			cands := p.fillParams(ctx, st, def, r.Path)
			if len(cands) == 0 {
				continue
			}
			path, user = cands[0].path, cands[0].user
		}
		if r.Auth == "user" && user == 0 {
			if checkUser == 0 {
				checkUser = p.makeCheckVisitor(ctx, st)
			}
			user = checkUser
		}
		n++
		res, err := p.screenshotOne(ctx, st.app, ScreenshotInput{Path: path, visitorID: user}, false)
		if err != nil {
			if errors.Is(err, errNoBrowser) {
				return warns
			}
			continue // the server-side checks report pages that do not render
		}
		for _, c := range res.PageChecks {
			if strings.HasPrefix(c, "a script on this page failed") || strings.HasPrefix(c, "a script file of this page did not load") ||
				strings.HasPrefix(c, "a number did not format") {
				warns = append(warns, fmt.Sprintf("GET %s (opened in a browser): %s", path, c))
			}
		}
	}
	return warns
}

// touchedScriptRoutes are the GET routes whose page this change may have
// changed (their template, a template they include, their script or the
// shared lib) and that run browser scripts. Every such route on a first
// version.
func touchedScriptRoutes(prev, def *appdef.App) []appdef.Route {
	changedT := map[string]bool{}
	for name, src := range def.Templates {
		if prev == nil || prev.Templates[name] != src {
			changedT[name] = true
		}
	}
	changedS := map[string]bool{}
	for name, src := range def.Scripts {
		if prev == nil || prev.Scripts[name] != src {
			changedS[name] = true
		}
	}
	libChanged := prev == nil || fmt.Sprint(prev.Lib) != fmt.Sprint(def.Lib)
	var out []appdef.Route
	for _, r := range def.Routes {
		if r.Method != http.MethodGet {
			continue
		}
		var roots []string
		touched := false
		if r.Template != "" {
			roots = []string{r.Template}
		}
		if r.Script != "" {
			touched = changedS[r.Script] || libChanged
			src := def.Scripts[r.Script]
			for name := range def.Templates {
				for _, q := range []string{`"`, `'`, "`"} {
					if strings.Contains(src, q+name+q) {
						roots = append(roots, name)
					}
				}
			}
		}
		sort.Strings(roots)
		scripts := false
		for name := range includedTemplates(def, roots) {
			touched = touched || changedT[name]
			scripts = scripts || strings.Contains(strings.ToLower(def.Templates[name]), "<script")
		}
		if touched && scripts {
			out = append(out, r)
		}
	}
	return out
}

// includedTemplates is roots and every template they include, at any depth.
func includedTemplates(def *appdef.App, roots []string) map[string]bool {
	seen := map[string]bool{}
	queue := append([]string{}, roots...)
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		if _, ok := def.Templates[name]; !ok || seen[name] {
			continue
		}
		seen[name] = true
		for _, m := range templateCallRE.FindAllStringSubmatch(def.Templates[name], -1) {
			queue = append(queue, m[1])
		}
	}
	return seen
}
