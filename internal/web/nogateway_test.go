package web_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Renoxium runs behind Cloudflare, which replaces a 502 or 504 from the
// origin with its own "Bad gateway" page: a page the platform answers with
// those statuses never reaches the person who should read it. Platform
// code answers 503 instead (Cloudflare passes it through).
func TestNoBadGatewayStatuses(t *testing.T) {
	for _, dir := range []string{".", "../platform", "../mcpserver", "../accounts"} {
		files, _ := filepath.Glob(filepath.Join(dir, "*.go"))
		for _, f := range files {
			if strings.HasSuffix(f, "_test.go") {
				continue
			}
			src, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			for i, line := range strings.Split(string(src), "\n") {
				if strings.Contains(line, "http.StatusBadGateway") && !strings.Contains(line, "==") ||
					strings.Contains(line, "http.StatusGatewayTimeout") && !strings.Contains(line, "==") {
					t.Errorf("%s:%d answers 502 or 504, which Cloudflare replaces with its own page: %s", f, i+1, strings.TrimSpace(line))
				}
			}
		}
	}
}
