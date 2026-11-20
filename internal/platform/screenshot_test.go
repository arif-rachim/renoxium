package platform_test

import (
	"bytes"
	"context"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/arif-rachim/renoxium/internal/appdef"
	"github.com/arif-rachim/renoxium/internal/platform"
	"github.com/arif-rachim/renoxium/internal/sandbox"
)

// chromium finds a browser for screenshot tests: RENOXIUM_TEST_CHROMIUM, or
// the one this repository's end-to-end setup uses. Without one the tests skip.
func chromium(t *testing.T) string {
	for _, c := range []string{os.Getenv("RENOXIUM_TEST_CHROMIUM"), "/opt/pw-browsers/chromium", "/usr/bin/chromium", "/usr/bin/chromium-browser"} {
		if c == "" {
			continue
		}
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	t.Skip("no Chromium for screenshot tests; set RENOXIUM_TEST_CHROMIUM")
	return ""
}

// The agent sees its preview: the home page and a service page on a phone,
// and a booking page signed in as a test visitor. The browser loads
// nothing from addresses other than the preview and public CDNs.
func TestScreenshotPreview(t *testing.T) {
	bin := chromium(t)
	p, err := platform.New(platform.Config{DataDir: t.TempDir(), AppsDomain: "renoxium.test", PublicScheme: "https",
		PreviewKey: []byte("0123456789abcdef0123456789abcdef"), Chromium: bin, ChromiumNoSandbox: true}, sandbox.NewGoja())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	ctx := context.Background()
	if _, res, err := p.CreateFromExample(ctx, "usr_test", "bayti", "home-service-uae"); err != nil || !res.OK {
		t.Fatal(err, res)
	}
	shot := func(in platform.ScreenshotInput) platform.ScreenshotResult {
		t.Helper()
		r, err := p.ScreenshotPreview(ctx, "bayti", in)
		if err != nil {
			t.Fatalf("%+v: %v", in, err)
		}
		img, err := png.Decode(bytes.NewReader(r.PNG))
		if err != nil {
			t.Fatalf("%s: not a PNG: %v", in.Path, err)
		}
		if b := img.Bounds(); b.Dx() != r.Width || b.Dy() != r.Height {
			t.Fatalf("%s: image %v, reported %dx%d", in.Path, b, r.Width, r.Height)
		}
		if dir := os.Getenv("RENOXIUM_SHOT_DIR"); dir != "" { // to look at them
			name := strings.NewReplacer("/", "_", "?", "_").Replace(in.Path) + "-" + in.Viewport + in.AsVisitor + ".png"
			os.WriteFile(filepath.Join(dir, name), r.PNG, 0o644)
		}
		return r
	}
	home := shot(platform.ScreenshotInput{Path: "/"})
	if home.Status != 200 || home.Width != 390 || home.Height != 844 {
		t.Fatalf("home: %+v", home)
	}
	full := shot(platform.ScreenshotInput{Path: "/s/1", FullPage: true})
	if full.Status != 200 || full.Height <= 844 {
		t.Fatalf("full service page: %d %dx%d %v", full.Status, full.Width, full.Height, full.BrowserLog)
	}
	if d := shot(platform.ScreenshotInput{Path: "/", Viewport: "desktop"}); d.Width != 1366 {
		t.Fatalf("desktop: %d", d.Width)
	}

	// Signed in as a test visitor with a booking of theirs.
	v, _ := p.CreateTestVisitor(ctx, "bayti", platform.TestVisitorInput{Name: "Fatima"})
	r, _ := p.TestRequest(ctx, "bayti", platform.TestRequestInput{Method: "POST", Path: "/book", UserEmail: v.Email, Form: map[string]string{
		"service": "1", "qty": "2", "zone": "1", "date": bookableDay(), "slot": "10:00", "repeat": "once", "payment": "cash",
		"name": "Fatima", "phone": "+971501234567", "address": "Villa 12", "flat": "12"}})
	if !strings.HasPrefix(r.Location, "/bookings/1") {
		t.Fatalf("booking: %+v", r)
	}
	if b := shot(platform.ScreenshotInput{Path: "/bookings/1", AsVisitor: v.Email}); b.Status != 200 {
		t.Fatalf("signed-in booking page: %+v", b)
	}
	// Signed out, the same page sends the browser to sign in, which a
	// screenshot does not open.
	if b := shot(platform.ScreenshotInput{Path: "/bookings/1"}); b.Status != 401 || !strings.Contains(strings.Join(b.BrowserLog, " "), "as_visitor") {
		t.Fatalf("signed-out booking page answered %d", b.Status)
	}

	for _, in := range []platform.ScreenshotInput{
		{Path: "https://example.com/"},
		{Path: "/_rx/auth/login"},
		{Path: "/", Viewport: "watch"},
		{Path: "/", Width: 100, Height: 100},
		{Path: "/", AsVisitor: "nobody@example.com"},
	} {
		if _, err := p.ScreenshotPreview(ctx, "bayti", in); err == nil {
			t.Errorf("accepted %+v", in)
		}
	}
}

// A page cannot make the server's browser load internal addresses.
func TestScreenshotBlocksOtherHosts(t *testing.T) {
	bin := chromium(t)
	p, err := platform.New(platform.Config{DataDir: t.TempDir(), AppsDomain: "renoxium.test", PublicScheme: "https", Chromium: bin, ChromiumNoSandbox: true}, sandbox.NewGoja())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	ctx := context.Background()
	p.CreateApp("usr_test", "probe")
	def := catalog(t)
	def.Templates["probe"] = `<!doctype html><html><head><title>p</title></head><body><img src="http://169.254.169.254/latest/meta-data/"><img src="http://127.0.0.1:1/x.png"><script src="https://evil.example/x.js"></script><p>hello</p></body></html>`
	def.Routes = append(def.Routes, appdef.Route{Method: "GET", Path: "/probe", Template: "probe"})
	if res, err := p.ApplyChange(ctx, "probe", def); err != nil || !res.OK {
		t.Fatal(err, res)
	}
	r, err := p.ScreenshotPreview(ctx, "probe", platform.ScreenshotInput{Path: "/probe"})
	if err != nil || r.Status != 200 {
		t.Fatal(err, r.Status)
	}
	got := strings.Join(r.BlockedHosts, " ")
	for _, h := range []string{"169.254.169.254", "127.0.0.1", "evil.example"} {
		if !strings.Contains(got, h) {
			t.Errorf("%s not reported as blocked: %v", h, r.BlockedHosts)
		}
	}
}

// Without a browser configured, the tool says so.
func TestScreenshotNeedsBrowser(t *testing.T) {
	p := newPlatform(t)
	p.CreateApp("usr_test", "nobrowser")
	if _, err := p.ScreenshotPreview(context.Background(), "nobrowser", platform.ScreenshotInput{Path: "/"}); err == nil || !strings.Contains(err.Error(), "not set up") {
		t.Fatal(err)
	}
}
