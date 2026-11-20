// Package seo holds what search engines, link previews and AI assistants
// need from a page, shared by the Renoxium site and the apps it hosts: the
// tags a page's <head> should carry, and response compression.
package seo

import (
	"bytes"
	"encoding/json"
	"html"
	"regexp"
	"sort"
	"strings"
)

// Alternate is the same page in another language.
type Alternate struct {
	Lang string // e.g. "ar", or "x-default"
	URL  string
}

// Head is what Inject adds to a page. Every field is optional, and a tag
// the page already has is never added again: the page's own tags win.
type Head struct {
	Canonical   string            // absolute address search engines should index
	SiteName    string            // og:site_name
	Description string            // used when the page has no meta description
	Image       string            // absolute og:image used when the page has none
	Alternates  []Alternate       // hreflang links
	Verify      map[string]string // meta name → content, e.g. google-site-verification
	JSONLD      any               // structured data, added when the page has none
}

var (
	tagRE   = regexp.MustCompile(`(?is)<(meta|link)\b[^>]*>`)
	attrRE  = regexp.MustCompile(`(?is)([a-z][a-z0-9:_-]*)\s*=\s*(?:"([^"]*)"|'([^']*)')`)
	titleRE = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
)

// Inject adds the missing tags before </head>. Pages that are not full
// HTML documents, and pages that ask not to be indexed, come back as given.
func Inject(page []byte, h Head) []byte {
	lower := bytes.ToLower(page)
	start := bytes.Index(lower, []byte("<head"))
	end := bytes.Index(lower, []byte("</head>"))
	if start < 0 || end < start {
		return page
	}
	head := page[start:end]

	meta := map[string]string{} // name or property → content, as written (escaped)
	canonical, hreflang := false, false
	for _, tag := range tagRE.FindAll(head, -1) {
		attrs := map[string]string{}
		for _, m := range attrRE.FindAllSubmatch(tag, -1) {
			v := m[2]
			if len(v) == 0 {
				v = m[3]
			}
			attrs[strings.ToLower(string(m[1]))] = string(v)
		}
		if bytes.HasPrefix(bytes.ToLower(tag), []byte("<link")) {
			canonical = canonical || strings.EqualFold(attrs["rel"], "canonical")
			hreflang = hreflang || attrs["hreflang"] != ""
			continue
		}
		for _, key := range []string{"name", "property"} {
			if n := strings.ToLower(attrs[key]); n != "" {
				meta[n] = attrs["content"]
			}
		}
	}
	if strings.Contains(strings.ToLower(meta["robots"]), "noindex") {
		return page
	}

	var b strings.Builder
	tag := func(kind, name, content string) {
		if _, has := meta[name]; has || content == "" {
			return
		}
		b.WriteString(`<meta ` + kind + `="` + name + `" content="` + html.EscapeString(content) + `">`)
		meta[name] = content
	}
	text := func(escaped string) string { return strings.TrimSpace(html.UnescapeString(escaped)) }

	tag("name", "description", h.Description)
	description := text(meta["description"])
	if !canonical && h.Canonical != "" {
		b.WriteString(`<link rel="canonical" href="` + html.EscapeString(h.Canonical) + `">`)
	}
	if !hreflang {
		for _, a := range h.Alternates {
			b.WriteString(`<link rel="alternate" hreflang="` + html.EscapeString(a.Lang) + `" href="` + html.EscapeString(a.URL) + `">`)
		}
	}
	if m := titleRE.FindSubmatch(head); m != nil {
		tag("property", "og:title", text(string(m[1])))
	}
	tag("property", "og:description", description)
	tag("property", "og:url", h.Canonical)
	tag("property", "og:type", "website")
	tag("property", "og:site_name", h.SiteName)
	tag("property", "og:image", h.Image)
	if meta["og:image"] != "" {
		tag("name", "twitter:card", "summary_large_image")
	} else {
		tag("name", "twitter:card", "summary")
	}
	names := make([]string, 0, len(h.Verify))
	for name := range h.Verify {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		tag("name", name, h.Verify[name])
	}
	if h.JSONLD != nil && !bytes.Contains(lower[start:end], []byte("application/ld+json")) {
		// json.Marshal writes <, > and & as <…, so the data cannot end
		// the script element.
		if data, err := json.Marshal(h.JSONLD); err == nil {
			b.WriteString(`<script type="application/ld+json">` + string(data) + `</script>`)
		}
	}
	if b.Len() == 0 {
		return page
	}
	out := make([]byte, 0, len(page)+b.Len())
	out = append(out, page[:end]...)
	out = append(out, b.String()...)
	return append(out, page[end:]...)
}

// Description returns the page's meta description, or "".
func Description(page []byte) string {
	for _, tag := range tagRE.FindAll(page, -1) {
		var name, content string
		for _, m := range attrRE.FindAllSubmatch(tag, -1) {
			v := m[2]
			if len(v) == 0 {
				v = m[3]
			}
			switch strings.ToLower(string(m[1])) {
			case "name":
				name = string(v)
			case "content":
				content = string(v)
			}
		}
		if strings.EqualFold(name, "description") {
			return strings.TrimSpace(html.UnescapeString(content))
		}
	}
	return ""
}
