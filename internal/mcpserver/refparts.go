package mcpserver

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// The reference is long (about 28 KB), and a free ChatGPT chat filled up
// after a few from-scratch changes. get_example sends its core (what every
// definition needs) and an index; the other parts come on request.

type refPart struct {
	Key, Title, Text string
}

// refCore are the parts sent with every reference.
var refCore = map[string]bool{"intro": true, "app-definition": true, "data-types": true, "security": true}

var refKeys = []struct{ prefix, key string }{
	{"App definition", "app-definition"}, {"Exact data types", "data-types"}, {"Working on a large definition", "large-definitions"},
	{"Testing signed-in flows", "testing"}, {"Visitor accounts", "visitor-accounts"}, {"Payments", "payments"}, {"Emails", "emails"},
	{"WhatsApp messages", "whatsapp"}, {"Calling other services", "http-fetch"}, {"Realtime", "realtime"}, {"Scheduled jobs", "jobs"},
	{"Admin changes", "admin-changes"}, {"Search engines", "seo"}, {"Tools for shoppers", "mcp-tools"}, {"Images", "images"},
	{"Visitor uploads", "uploads"}, {"Security rules", "security"},
}

var refHeadingRE = regexp.MustCompile(`^[A-Z][^\n]*:$`)

// refParts splits the reference at its headings (a line starting at the
// margin and ending with a colon, or with a colon after a parenthesis).
var refParts = func() []refPart {
	parts := []refPart{{Key: "intro", Title: "Introduction"}}
	for _, line := range strings.Split(reference, "\n") {
		key := ""
		if !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "-") && (refHeadingRE.MatchString(line) || strings.HasSuffix(line, "):")) {
			for _, k := range refKeys {
				if strings.HasPrefix(line, k.prefix) {
					key = k.key
				}
			}
		}
		if key != "" {
			parts = append(parts, refPart{Key: key, Title: strings.TrimSuffix(line, ":")})
		}
		last := &parts[len(parts)-1]
		last.Text += line + "\n"
	}
	return parts
}()

// referenceFor returns the reference's core and index, or the parts asked
// for ("all" for the whole reference).
func referenceFor(want []string) (string, error) {
	if len(want) == 1 && want[0] == "all" {
		return reference, nil
	}
	byKey := map[string]refPart{}
	var keys []string
	for _, p := range refParts {
		byKey[p.Key] = p
		keys = append(keys, p.Key)
	}
	var b strings.Builder
	if len(want) == 0 {
		for _, p := range refParts {
			if refCore[p.Key] {
				b.WriteString(p.Text)
			}
		}
		b.WriteString("\nMore of the reference, each part on request with get_example {reference_only: true, reference_parts: [\"<part>\"]} " +
			"(several at once, or [\"all\"]):\n")
		for _, p := range refParts {
			if !refCore[p.Key] {
				fmt.Fprintf(&b, "- %s (%.1f KB): %s\n", p.Key, float64(len(p.Text))/1024, p.Title)
			}
		}
		return b.String(), nil
	}
	sort.SliceStable(want, func(i, j int) bool { return indexOf(keys, want[i]) < indexOf(keys, want[j]) })
	for _, k := range want {
		p, ok := byKey[k]
		if !ok {
			return "", fmt.Errorf("no reference part %q; the parts are: %s, or all", k, strings.Join(keys, ", "))
		}
		b.WriteString(p.Text)
	}
	return b.String(), nil
}

func indexOf(list []string, s string) int {
	for i, x := range list {
		if x == s {
			return i
		}
	}
	return len(list)
}
