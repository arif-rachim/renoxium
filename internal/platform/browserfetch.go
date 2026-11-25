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
// site directly. Browsers block most such calls (CORS), screenshots never
// reach them and view_page does not run scripts, so the page looks fine to
// every check while visitors see nothing. The way that works is http.fetch
// in a script, through the owner's allowed hosts.
func (p *Platform) browserFetchWarnings(app string, def *appdef.App) []string {
	own := strings.ToLower(p.cfg.AppsDomain)
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
		out = append(out, fmt.Sprintf("templates %s: the browser calls %s directly. Browsers block most calls to other sites, "+
			"screenshot_preview does not reach them and view_page does not run scripts, so this data will be missing for visitors "+
			"while checks pass. Call %s from a script with http.fetch instead (add it to hosts; the owner allows it and turns on "+
			"test calls for the preview) and give the page the result", strings.Join(names, ", "), h, h))
	}
	sort.Strings(out)
	return out
}
