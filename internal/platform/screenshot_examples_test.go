package platform_test

import (
	"context"
	"testing"

	"github.com/arif-rachim/renoxium/internal/platform"
	"github.com/arif-rachim/renoxium/internal/sandbox"
)

// The finished examples pass their own page checks: a check that fires on
// them would teach assistants to ignore page_checks.
func TestScreenshotPageChecksQuietOnExamples(t *testing.T) {
	bin := chromium(t)
	p, err := platform.New(platform.Config{DataDir: t.TempDir(), AppsDomain: "renoxium.test", PublicScheme: "https", Chromium: bin, ChromiumNoSandbox: true}, sandbox.NewGoja())
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	ctx := context.Background()
	for i, ex := range []string{"store-uae", "booking-uae", "home-service-uae", "reservation-uae", "quote-portal-uae", "listings-uae", "clinic-queue-uae"} {
		name := "ex" + string(rune('a'+i))
		if _, res, err := p.CreateFromExample(ctx, "usr_test", name, ex); err != nil || !res.OK {
			t.Fatal(err)
		}
		for _, vp := range []string{"mobile", "desktop"} {
			r, err := p.ScreenshotPreview(ctx, name, platform.ScreenshotInput{Path: "/", Viewport: vp, FullPage: true})
			if err != nil {
				t.Fatal(ex, err)
			}
			if len(r.PageChecks) > 0 {
				t.Errorf("%s on %s: %q", ex, vp, r.PageChecks)
			}
		}
	}
}
