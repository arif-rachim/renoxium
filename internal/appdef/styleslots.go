package appdef

import (
	"fmt"
	"regexp"
	"strings"
)

// styleSlots are the templates that hold a page's style. Every example
// includes them in <head>, outside any <style> element, so what they hold
// must itself be markup: CSS written there without <style> is shown as
// text at the top of every page.
var styleSlots = []string{"css", "css_extra"}

var (
	blockRE   = regexp.MustCompile(`(?is)<(style|script)\b.*?</(style|script)\s*>|<!--.*?-->|\{\{.*?\}\}`)
	tagRE     = regexp.MustCompile(`(?s)<[^>]*>`)
	includeRE = regexp.MustCompile(`\{\{-?\s*template\s+"([^"]+)"`)
)

// FixStyleSlots repairs style templates whose CSS would show as text: a
// slot holding only CSS (no tags) is wrapped in <style>...</style>, which
// is what was meant. A slot with text outside its tags, which cannot be
// repaired without guessing, is a problem. Slots included inside a <style>
// element by another template are left as they are.
func (a *App) FixStyleSlots() (fixed, problems []string) {
	inStyle := map[string]bool{}
	for _, src := range a.Templates {
		for _, m := range includeRE.FindAllStringSubmatchIndex(src, -1) {
			before := strings.ToLower(src[:m[0]])
			if strings.LastIndex(before, "<style") > strings.LastIndex(before, "</style") {
				inStyle[src[m[2]:m[3]]] = true
			}
		}
	}
	for _, name := range styleSlots {
		src, ok := a.Templates[name]
		if !ok || inStyle[name] || strings.TrimSpace(src) == "" {
			continue
		}
		left := strings.TrimSpace(tagRE.ReplaceAllString(blockRE.ReplaceAllString(src, ""), ""))
		if left == "" {
			continue
		}
		if !strings.Contains(src, "<") {
			a.Templates[name] = "<style>\n" + strings.TrimSpace(src) + "\n</style>"
			fixed = append(fixed, fmt.Sprintf("templates.%s held CSS without <style>...</style>, which would show as text on every page; it is now wrapped in <style>", name))
			continue
		}
		if len(left) > 60 {
			left = left[:57] + "..."
		}
		problems = append(problems, fmt.Sprintf("templates.%s: %q is outside any tag and would show as text at the top of every page; put CSS inside <style>...</style>", name, left))
	}
	return fixed, problems
}
