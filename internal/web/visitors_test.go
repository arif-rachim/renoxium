package web_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/arif-rachim/renoxium/internal/appdef"
)

// signUpVisitor creates a visitor account as a browser would and returns
// the session cookie of the app's host: the app's host sends the browser
// to the sign-in host, the form is posted there, and the finished sign-up
// comes back to the app's host through /_rx/auth/callback.
func (e *env) signUpVisitor(host, addr, name string) *http.Cookie {
	e.t.Helper()
	send := func(method, target string, form url.Values, cookies []*http.Cookie) *httptest.ResponseRecorder {
		var body io.Reader
		if form != nil {
			body = strings.NewReader(form.Encode())
		}
		r := httptest.NewRequest(method, target, body)
		if form != nil {
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		for _, c := range cookies {
			r.AddCookie(c)
		}
		rec := httptest.NewRecorder()
		e.p.ServeApp(rec, r)
		return rec
	}
	start := send("GET", "http://"+host+"/_rx/auth/signup", nil, nil)
	signIn, err := url.Parse(start.Header().Get("Location"))
	if err != nil || start.Code != 303 || !strings.Contains(signIn.Host, "--id.") {
		e.t.Fatalf("start of sign-up: %d %s", start.Code, start.Header().Get("Location"))
	}
	page := send("GET", "http://"+signIn.Host+signIn.RequestURI(), nil, nil)
	m := csrfRE.FindStringSubmatch(page.Body.String())
	if m == nil {
		e.t.Fatalf("sign-up page: %d %s", page.Code, page.Body.String())
	}
	form := url.Values{"csrf": {m[1]}, "email": {addr}, "name": {name}, "password": {"visitor password"}}
	done := send("POST", "http://"+signIn.Host+"/_rx/auth/signup", form, page.Result().Cookies())
	back, err := url.Parse(done.Header().Get("Location"))
	if err != nil || done.Code != 303 || back.Host != host {
		e.t.Fatalf("signup %s: %d %s", addr, done.Code, done.Body.String())
	}
	rec := send("GET", "http://"+host+back.RequestURI(), nil, start.Result().Cookies())
	for _, c := range rec.Result().Cookies() {
		if c.Name == "__Host-rx_session" && c.MaxAge >= 0 {
			return c
		}
	}
	e.t.Fatalf("callback for %s: %d %s", addr, rec.Code, rec.Body.String())
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
