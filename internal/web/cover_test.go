package web_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/arif-rachim/renoxium/internal/appdef"
)

// Without a browser for screenshots the app cards show the app's initial:
// no cover picture is linked, and its address answers not found, also for
// people who are not the app's owner.
func TestAppCoverWithoutBrowser(t *testing.T) {
	e := newEnv(t)
	owner := e.browser()
	e.signup(owner, "jana@shop.ae")
	e.publishDef(owner, "jana-shop", &appdef.App{Templates: map[string]string{"t": "shop"}, Routes: []appdef.Route{{Method: "GET", Path: "/", Template: "t"}}})
	r, _ := owner.Get(e.base + "/")
	page := read(t, r)
	if !strings.Contains(page, `class="appcard"`) || !strings.Contains(page, `class="cover"`) || strings.Contains(page, "cover.jpg") {
		t.Fatalf("app card without a browser: %v %v %v", strings.Contains(page, `class="appcard"`), strings.Contains(page, `class="cover"`), strings.Contains(page, "cover.jpg"))
	}
	if r, _ := owner.Get(e.base + "/apps/jana-shop/cover.jpg"); r.StatusCode != 404 {
		t.Fatalf("cover without a browser: %d", r.StatusCode)
	}
	// Signed out, the address leads to the sign-in page, never to a picture.
	if r, _ := http.Get(e.base + "/apps/jana-shop/cover.jpg"); strings.HasPrefix(r.Header.Get("Content-Type"), "image/") || !strings.Contains(r.Request.URL.Path, "/login") {
		t.Fatalf("cover for a stranger: %d %s at %s", r.StatusCode, r.Header.Get("Content-Type"), r.Request.URL.Path)
	}
}
