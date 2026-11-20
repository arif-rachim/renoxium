package platform

import (
	"context"
	"encoding/xml"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/arif-rachim/renoxium/internal/appdef"
	"github.com/arif-rachim/renoxium/internal/seo"
)

// Every app answers these three addresses unless it has its own route there.
const (
	RobotsPath  = "/robots.txt"
	SitemapPath = "/sitemap.xml"
	LLMsPath    = "/llms.txt"
)

const (
	sitemapRowsPerQuery = 2000
	sitemapMaxPages     = 10000
	sitemapCacheFor     = 5 * time.Minute
	llmsMaxPages        = 200
)

// aiCrawlers are the user agents of AI companies' crawlers, for apps whose
// owner chose seo.ai_crawlers "block".
var aiCrawlers = []string{"GPTBot", "OAI-SearchBot", "ChatGPT-User", "ClaudeBot", "Claude-User", "Claude-SearchBot",
	"anthropic-ai", "PerplexityBot", "Perplexity-User", "Google-Extended", "Applebot-Extended", "CCBot", "Bytespider",
	"Amazonbot", "meta-externalagent", "cohere-ai", "DuckAssistBot"}

// trackingParams never change what a page shows, so they are left out of
// its canonical address.
var trackingParams = map[string]bool{"fbclid": true, "gclid": true, "gbraid": true, "wbraid": true, "msclkid": true,
	"igshid": true, "mc_cid": true, "mc_eid": true, "ref": true, "pv": true}

type sitePage struct {
	Path, Title, Updated string
}

type sitemapCache struct {
	mu      sync.Mutex
	entries map[string]sitemapCached // app + "\x00" + stage
}

type sitemapCached struct {
	version int
	at      time.Time
	pages   []sitePage
}

func isSEOPath(path string) bool {
	return path == RobotsPath || path == SitemapPath || path == LLMsPath
}

// serveSEO answers /robots.txt, /sitemap.xml and /llms.txt for a stage.
func (p *Platform) serveSEO(w http.ResponseWriter, r *http.Request, st *stage) {
	h := w.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	indexed := st.name == Live
	if !indexed {
		h.Set("X-Robots-Tag", "noindex")
	} else {
		h.Set("Cache-Control", "public, max-age=300")
	}
	var body string
	switch r.URL.Path {
	case RobotsPath:
		h.Set("Content-Type", "text/plain; charset=utf-8")
		body = p.robotsTxt(st, indexed)
	case SitemapPath:
		h.Set("Content-Type", "application/xml; charset=utf-8")
		body = p.sitemapXML(r.Context(), st)
	default:
		h.Set("Content-Type", "text/markdown; charset=utf-8")
		body = p.llmsTxt(r.Context(), st)
	}
	h.Set("Content-Length", fmt.Sprint(len(body)))
	if r.Method != http.MethodHead {
		w.Write([]byte(body))
	}
}

func (p *Platform) robotsTxt(st *stage, indexed bool) string {
	if !indexed {
		return "User-agent: *\nDisallow: /\n"
	}
	var b strings.Builder
	if st.def.SEO != nil && st.def.SEO.AICrawlers == "block" {
		for _, ua := range aiCrawlers {
			b.WriteString("User-agent: " + ua + "\n")
		}
		b.WriteString("Disallow: /\n\n")
	}
	b.WriteString("User-agent: *\nAllow: " + FilesPrefix + "\nDisallow: /_rx/\n")
	// Pages behind sign-in only ever show crawlers the sign-in form.
	seen := map[string]bool{}
	for _, route := range st.def.Routes {
		if route.Method != http.MethodGet || route.Auth != "user" {
			continue
		}
		prefix, _, _ := strings.Cut(route.Path, "/:")
		if prefix == "" || prefix == "/" || seen[prefix] {
			continue
		}
		seen[prefix] = true
		b.WriteString("Disallow: " + prefix + "\n")
	}
	b.WriteString("\nSitemap: " + p.URL(st.app, st.name) + SitemapPath + "\n")
	return b.String()
}

// sitePages lists the stage's public pages: seo.sitemap when the app has
// one, otherwise its public GET routes without parameters.
func (p *Platform) sitePages(ctx context.Context, st *stage) []sitePage {
	key := st.app + "\x00" + st.name
	p.sitemaps.mu.Lock()
	c, ok := p.sitemaps.entries[key]
	p.sitemaps.mu.Unlock()
	if ok && c.version == st.version && time.Since(c.at) < sitemapCacheFor {
		return c.pages
	}
	pages := p.buildSitePages(ctx, st)
	p.sitemaps.mu.Lock()
	if p.sitemaps.entries == nil {
		p.sitemaps.entries = map[string]sitemapCached{}
	}
	p.sitemaps.entries[key] = sitemapCached{version: st.version, at: time.Now(), pages: pages}
	p.sitemaps.mu.Unlock()
	return pages
}

func (p *Platform) buildSitePages(ctx context.Context, st *stage) []sitePage {
	var pages []sitePage
	seen := map[string]bool{}
	add := func(pg sitePage) {
		if len(pages) >= sitemapMaxPages || seen[pg.Path] || !sitemapPath(pg.Path) {
			return
		}
		seen[pg.Path] = true
		pages = append(pages, pg)
	}
	if st.def.SEO == nil || len(st.def.SEO.Sitemap) == 0 {
		for _, route := range st.def.Routes {
			last := route.Path[strings.LastIndex(route.Path, "/")+1:]
			if route.Method == http.MethodGet && route.Auth == "" && !route.HasParams() &&
				!strings.HasPrefix(route.Path, "/api/") && !strings.Contains(last, ".") {
				add(sitePage{Path: route.Path})
			}
		}
		return pages
	}
	add(sitePage{Path: "/"})
	for i, entry := range st.def.SEO.Sitemap {
		if !appdef.SitemapQuery(entry) {
			add(sitePage{Path: entry})
			continue
		}
		rows, err := sitemapRows(ctx, st, entry)
		if err != nil {
			p.event(st, "seo", "error", "", fmt.Sprintf("seo.sitemap %d: %v", i, err))
			continue
		}
		for _, row := range rows {
			pg := sitePage{Path: fmt.Sprint(row["path"])}
			if t, ok := row["title"].(string); ok {
				if pg.Title = strings.TrimSpace(t); len(pg.Title) > 200 {
					pg.Title = strings.ToValidUTF8(pg.Title[:200], "")
				}
			}
			if u, ok := row["updated"].(string); ok && len(u) >= 10 {
				if _, err := time.Parse("2006-01-02", u[:10]); err == nil {
					pg.Updated = u[:10]
				}
			}
			add(pg)
		}
	}
	return pages
}

// sitemapRows runs one seo.sitemap query, read-only.
func sitemapRows(ctx context.Context, st *stage, query string) ([]map[string]any, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	rows, err := st.db.QueryReadOnly(ctx, query, nil)
	if err != nil {
		return nil, err
	}
	if len(rows) > 0 {
		if _, ok := rows[0]["path"]; !ok {
			return nil, fmt.Errorf("the SELECT must return a column named path")
		}
	}
	if len(rows) > sitemapRowsPerQuery {
		rows = rows[:sitemapRowsPerQuery]
	}
	return rows, nil
}

// checkSitemap reports what is wrong with the app's seo.sitemap queries,
// for the building agent.
func checkSitemap(ctx context.Context, st *stage) []string {
	if st.def.SEO == nil {
		return nil
	}
	var problems []string
	for i, entry := range st.def.SEO.Sitemap {
		if !appdef.SitemapQuery(entry) {
			continue
		}
		rows, err := sitemapRows(ctx, st, entry)
		if err != nil {
			problems = append(problems, fmt.Sprintf("seo.sitemap %d: %v", i, err))
			continue
		}
		for _, row := range rows {
			if path := fmt.Sprint(row["path"]); !sitemapPath(path) {
				problems = append(problems, fmt.Sprintf("seo.sitemap %d: path %q must start with / and contain no spaces", i, path))
				break
			}
		}
	}
	return problems
}

func sitemapPath(path string) bool {
	return strings.HasPrefix(path, "/") && !strings.HasPrefix(path, "//") && len(path) <= 500 &&
		!strings.HasPrefix(path, "/_rx/") && !strings.ContainsAny(path, " \t\r\n<>\"\\")
}

// seoLanguages returns the app's languages when it has more than one.
func seoLanguages(def *appdef.App) []string {
	if def.SEO == nil || len(def.SEO.Languages) < 2 {
		return nil
	}
	return def.SEO.Languages
}

// langURL is the address of a page in one language: the default language
// has no parameter.
func langURL(address string, langs []string, lang string) string {
	if len(langs) == 0 || lang == langs[0] {
		return address
	}
	if strings.Contains(address, "?") {
		return address + "&lang=" + lang
	}
	return address + "?lang=" + lang
}

func alternates(address string, langs []string) []seo.Alternate {
	if len(langs) == 0 {
		return nil
	}
	out := make([]seo.Alternate, 0, len(langs)+1)
	for _, l := range langs {
		out = append(out, seo.Alternate{Lang: l, URL: langURL(address, langs, l)})
	}
	return append(out, seo.Alternate{Lang: "x-default", URL: address})
}

func xmlText(s string) string {
	var b strings.Builder
	xml.EscapeText(&b, []byte(s))
	return b.String()
}

func (p *Platform) sitemapXML(ctx context.Context, st *stage) string {
	base, langs := p.URL(st.app, st.name), seoLanguages(st.def)
	var b strings.Builder
	b.WriteString(xml.Header + `<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"`)
	if langs != nil {
		b.WriteString(` xmlns:xhtml="http://www.w3.org/1999/xhtml"`)
	}
	b.WriteString(">\n")
	for _, pg := range p.sitePages(ctx, st) {
		address := base + pg.Path
		// Each language is a page of its own, listing all its versions.
		versions := []string{address}
		for _, l := range langs[min(1, len(langs)):] {
			versions = append(versions, langURL(address, langs, l))
		}
		for _, loc := range versions {
			b.WriteString("<url><loc>" + xmlText(loc) + "</loc>")
			if pg.Updated != "" {
				b.WriteString("<lastmod>" + pg.Updated + "</lastmod>")
			}
			for _, a := range alternates(address, langs) {
				b.WriteString(`<xhtml:link rel="alternate" hreflang="` + xmlText(a.Lang) + `" href="` + xmlText(a.URL) + `"/>`)
			}
			b.WriteString("</url>\n")
		}
	}
	b.WriteString("</urlset>\n")
	return b.String()
}

// llmsTxt describes the app to AI assistants: what the business is, its
// pages, and the tools they can call at /mcp.
func (p *Platform) llmsTxt(ctx context.Context, st *stage) string {
	base := p.URL(st.app, st.name)
	name, _ := p.brandOf(ctx, st, "")
	if name == "" {
		name = st.app
	}
	line := func(s string) string { return strings.Join(strings.Fields(s), " ") }
	var b strings.Builder
	b.WriteString("# " + line(name) + "\n")
	description := ""
	if st.def.SEO != nil {
		description = st.def.SEO.Description
	}
	if description == "" {
		// The home page usually describes the business in its own words.
		if req, err := http.NewRequestWithContext(ctx, http.MethodGet, "/", nil); err == nil {
			if out := p.dispatch(st, req, newRequestID()); out.status == http.StatusOK {
				description = seo.Description(out.body)
			}
		}
	}
	if description != "" {
		b.WriteString("\n> " + line(description) + "\n")
	}
	if langs := seoLanguages(st.def); langs != nil {
		b.WriteString("\nPages are available in: " + strings.Join(langs, ", ") + ". The first is the default; add ?lang=<code> to an address for another, e.g. ?lang=" + langs[1] + ".\n")
	}
	pages := p.sitePages(ctx, st)
	if len(pages) > 0 {
		b.WriteString("\n## Pages\n\n")
		for i, pg := range pages {
			if i == llmsMaxPages {
				fmt.Fprintf(&b, "- [All %d pages](%s)\n", len(pages), base+SitemapPath)
				break
			}
			title := line(pg.Title)
			if title == "" {
				if title = pg.Path; title == "/" {
					title = "Home"
				}
			}
			b.WriteString("- [" + strings.NewReplacer("[", "(", "]", ")").Replace(title) + "](" + base + pg.Path + ")\n")
		}
	}
	if len(st.def.Tools) > 0 {
		b.WriteString("\n## Tools for AI assistants\n\nThis site has an MCP server at " + base + MCPPath +
			" (Streamable HTTP, no sign-in needed). Its tools:\n\n")
		for _, t := range st.def.Tools {
			b.WriteString("- " + t.Name + ": " + line(t.Description) + "\n")
		}
	}
	b.WriteString("\n## More\n\n- [Sitemap](" + base + SitemapPath + ")\n")
	return b.String()
}

// seoHead completes the <head> of a live page: canonical address, Open
// Graph tags for link previews, hreflang links and verification codes.
func (p *Platform) seoHead(st *stage, r *http.Request, page []byte) []byte {
	base, langs := p.URL(st.app, st.name), seoLanguages(st.def)
	query := url.Values{}
	lang := ""
	for k, v := range r.URL.Query() {
		switch {
		case trackingParams[k] || strings.HasPrefix(k, "utm_"):
		case k == "lang" && langs != nil:
			lang = v[0]
		default:
			query[k] = v
		}
	}
	address := base + r.URL.EscapedPath()
	if len(query) > 0 {
		address += "?" + query.Encode() // sorted by key
	}
	known := false
	for _, l := range langs {
		known = known || l == lang
	}
	if !known && langs != nil {
		lang = langs[0]
	}
	name, _ := p.brandOf(r.Context(), st, lang)
	h := seo.Head{Canonical: langURL(address, langs, lang), SiteName: name, Alternates: alternates(address, langs)}
	if s := st.def.SEO; s != nil {
		h.Description = s.Description
		if h.Image = s.Image; strings.HasPrefix(h.Image, "/") {
			h.Image = base + h.Image
		}
		// Search consoles read the code from the home page.
		if r.URL.Path == "/" && len(s.Verify) > 0 {
			h.Verify = map[string]string{}
			for service, code := range s.Verify {
				h.Verify[appdef.VerifyMeta[service]] = code
			}
		}
	}
	return seo.Inject(page, h)
}
