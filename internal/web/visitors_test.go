package web_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/arif-rachim/renoxium/internal/appdef"
)

// signUpVisitor creates a visitor account on an app host and returns its
// session cookie.
func (e *env) signUpVisitor(host, addr, name string) *http.Cookie {
	e.t.Helper()
	w := e.appGet(host, "/_rx/auth/signup")
	csrf := csrfRE.FindStringSubmatch(w.Body.String())[1]
	var csrfCookie *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == "__Host-rx_csrf" {
			csrfCookie = c
		}
	}
	form := url.Values{"csrf": {csrf}, "email": {addr}, "name": {name}, "password": {"visitor password"}}
	r := httptest.NewRequest("POST", "http://"+host+"/_rx/auth/signup", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.AddCookie(csrfCookie)
	rec := httptest.NewRecorder()
	e.p.ServeApp(rec, r)
	for _, c := range rec.Result().Cookies() {
		if c.Name == "__Host-rx_session" {
			return c
		}
	}
	e.t.Fatalf("signup %s: %d %s", addr, rec.Code, rec.Body.String())
	return nil
}

func TestAdminVisitors(t *testing.T) {
	e := newEnv(t)
	owner := e.browser()
	e.signup(owner, "pia@shop.ae")
	e.publishDef(owner, "pia-club", &appdef.App{
		Scripts: map[string]string{"me": "function handle(req) { return {json: req.user} }"},
		Routes:  []appdef.Route{{Method: "GET", Path: "/me", Script: "me", Auth: "user"}},
	})
	host := "pia-club.renoxium.test"
	ali := e.signUpVisitor(host, "ali@mail.ae", "Ali")
	e.signUpVisitor(host, "bea@mail.ae", "Bea")

	get := func(c *http.Client, path string) (int, string) {
		r, _ := c.Get(e.base + path)
		return r.StatusCode, read(t, r)
	}
	if _, page := get(owner, "/apps/pia-club"); !strings.Contains(page, "2 signed up") {
		t.Fatalf("overview: %s", page)
	}
	code, page := get(owner, "/apps/pia-club/visitors")
	if code != 200 || !strings.Contains(page, "ali@mail.ae") || !strings.Contains(page, "Bea") || strings.Contains(page, "argon2id") {
		t.Fatalf("visitors: %d %s", code, page)
	}
	if _, page := get(owner, "/apps/pia-club/visitors?q=bea"); strings.Contains(page, "ali@mail.ae") || !strings.Contains(page, "bea@mail.ae") {
		t.Fatal("search")
	}

	// Deleting Ali signs Ali out.
	r := httptest.NewRequest("GET", "http://"+host+"/me", nil)
	r.AddCookie(ali)
	w := httptest.NewRecorder()
	e.p.ServeApp(w, r)
	if w.Code != 200 {
		t.Fatalf("ali before delete: %d", w.Code)
	}
	resp, _ := owner.PostForm(e.base+"/apps/pia-club/visitors/1/delete", url.Values{"csrf": {e.csrf(owner, "/")}})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("delete: %d", resp.StatusCode)
	}
	w = httptest.NewRecorder()
	e.p.ServeApp(w, r)
	if w.Code != 303 {
		t.Fatalf("ali after delete: %d", w.Code)
	}
	if _, page := get(owner, "/apps/pia-club/visitors"); strings.Contains(page, "ali@mail.ae") {
		t.Fatal("ali still listed")
	}

	// Another owner sees nothing.
	other := e.browser()
	e.signup(other, "quinn@shop.ae")
	if code, page := get(other, "/apps/pia-club/visitors"); code != 404 || strings.Contains(page, "bea@mail.ae") {
		t.Fatalf("other owner: %d", code)
	}
	resp, _ = other.PostForm(e.base+"/apps/pia-club/visitors/2/delete", url.Values{"csrf": {e.csrf(other, "/")}})
	if resp.StatusCode != 404 {
		t.Fatalf("other owner delete: %d", resp.StatusCode)
	}
}
