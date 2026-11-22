package web_test

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/arif-rachim/renoxium/examples"
)

func TestTemplateGallery(t *testing.T) {
	e := newEnv(t)
	if err := e.p.EnsureDemos(context.Background(), examples.Gallery()); err != nil {
		t.Fatal(err)
	}
	c := e.browser()

	// Every gallery template is on the page with its demo and admin links, without signing in.
	r, err := c.Get(e.base + "/templates")
	if err != nil {
		t.Fatal(err)
	}
	page := read(t, r)
	if r.StatusCode != 200 {
		t.Fatalf("gallery: %d", r.StatusCode)
	}
	for _, ex := range examples.Gallery() {
		for _, want := range []string{ex.Gallery.Title, "https://demo-" + ex.Name + ".renoxium.test/", "/templates/" + ex.Name + "/admin", "the " + ex.Name + " template"} {
			if !strings.Contains(page, want) {
				t.Errorf("gallery lacks %q", want)
			}
		}
	}
	// The same cards as the console shows when signed in: what the owner does, a
	// wide screenshot of the demo, the request to copy, and the card for anything in between.
	for _, want := range []string{"I sell products", "I take bookings", "I send quotes", "I deliver", "/static/img/wide-booking.webp?v=",
		`<div class="tcards">`, `<article class="tcard" id="booking-uae">`, `data-prompt="Use Renoxium to build me a website from the booking-uae template`,
		"Copy the request</button>", "Something in between?", `href="/connect">How to connect</a>`, `href="/signup"`, `rel="canonical"`} {
		if !strings.Contains(page, want) {
			t.Errorf("gallery lacks %q", want)
		}
	}
	for _, path := range []string{"/", "/llms.txt"} {
		r, _ := c.Get(e.base + path)
		if body := read(t, r); !strings.Contains(body, "/templates") {
			t.Errorf("%s does not point to the templates", path)
		}
	}
	if r, _ := c.Get(e.base + "/templates/tasks/admin"); r.StatusCode != 404 {
		t.Fatalf("a template outside the gallery: %d", r.StatusCode)
	}
	if r, _ := c.Get(e.base + "/templates/booking-uae"); r.StatusCode != 303 || r.Header.Get("Location") != "https://demo-booking-uae.renoxium.test/" {
		t.Fatalf("demo link: %d %s", r.StatusCode, r.Header.Get("Location"))
	}
}

func TestDemoAdminPanelIsReadOnly(t *testing.T) {
	e := newEnv(t)
	ex, _ := examples.Get("booking-uae")
	if err := e.p.EnsureDemos(context.Background(), []examples.Example{ex}); err != nil {
		t.Fatal(err)
	}
	c := e.browser()
	get := func(path string) (int, string) {
		t.Helper()
		r, err := c.Get(e.base + path)
		if err != nil {
			t.Fatal(err)
		}
		return r.StatusCode, read(t, r)
	}
	if code, _ := get("/templates/booking-uae/admin"); code != 303 {
		t.Fatalf("admin link: %d", code)
	}
	code, page := get("/apps/demo-booking-uae")
	if code != 200 || !strings.Contains(page, "/apps/demo-booking-uae/t/bookings") || !strings.Contains(page, "read-only") {
		t.Fatalf("demo admin: %d %s", code, page)
	}
	if strings.Contains(page, "/publish") || strings.Contains(page, `demo-booking-uae/staff"`) || strings.Contains(page, `demo-booking-uae/payments"`) {
		t.Fatalf("demo admin offers owner actions: %s", page)
	}
	code, page = get("/apps/demo-booking-uae/t/services")
	if code != 200 || strings.Contains(page, "/t/services/new") || !strings.Contains(page, "/apps/demo-booking-uae/t/services/1") {
		t.Fatalf("demo list: %d %s", code, page)
	}
	code, page = get("/apps/demo-booking-uae/t/services/1")
	if code != 200 || !strings.Contains(page, "<fieldset disabled") || strings.Contains(page, "<button>Save</button>") || strings.Contains(page, "/delete") {
		t.Fatalf("demo row: %d %s", code, page)
	}
	if code, _ := get("/apps/demo-booking-uae/t/services/new"); code != 303 {
		t.Fatalf("new row form in a demo: %d", code)
	}
	if code, page = get("/apps/demo-booking-uae/t/bookings/calendar"); code != 200 || strings.Contains(page, ">Add</a>") {
		t.Fatalf("demo calendar: %d", code)
	}
	// Nothing can be changed, even by hand-made requests, and owner pages stay closed.
	r, err := c.PostForm(e.base+"/apps/demo-booking-uae/t/services/1", url.Values{"name": {"Hacked"}})
	if err != nil {
		t.Fatal(err)
	}
	if r.StatusCode == http.StatusSeeOther && !strings.HasPrefix(r.Header.Get("Location"), "/login") {
		t.Fatalf("POST to a demo row: %d %s", r.StatusCode, r.Header.Get("Location"))
	}
	if _, body := get("/apps/demo-booking-uae/t/services/1"); strings.Contains(body, "Hacked") {
		t.Fatal("the demo row changed")
	}
	for _, path := range []string{"/apps/demo-booking-uae/visitors", "/apps/demo-booking-uae/payments", "/apps/demo-booking-uae/staff", "/apps/demo-booking-uae/publish?version=1"} {
		if code, _ := get(path); code == 200 {
			t.Errorf("%s is open on a demo", path)
		}
	}
	// A signed-in account gets no more than anyone else.
	e.signup(c, "someone@example.ae")
	if code, _ := get("/apps/demo-booking-uae/visitors"); code == 200 {
		t.Fatal("visitors of a demo for a signed-in account")
	}
}

// "Use this template" makes the owner's own app from a template on its
// private preview, without an assistant, and the app page says what to do
// next. Signed out, the button leads to sign-up and back to the template.
func TestUseTemplate(t *testing.T) {
	e := newEnv(t)
	r, _ := http.Get(e.base + "/templates")
	if page := read(t, r); !strings.Contains(page, `href="/signup?next=%2Ftemplates%3Fuse%3Dstore-uae"`) {
		t.Fatal("signed out, the template has no Use link to sign-up")
	}
	owner := e.browser()
	e.signup(owner, "huda@shop.ae")
	r, _ = owner.Get(e.base + "/templates?use=store-uae")
	if page := read(t, r); !strings.Contains(page, `action="/templates/store-uae/use"`) || !strings.Contains(page, `id="n-store-uae" name="app"`) {
		t.Fatal("signed in, the template has no Use form")
	}
	csrf := e.csrf(owner, "/templates")
	use := func(example, name string) (*http.Response, string) {
		r, err := owner.PostForm(e.base+"/templates/"+example+"/use", url.Values{"csrf": {csrf}, "app": {name}})
		if err != nil {
			t.Fatal(err)
		}
		return r, read(t, r)
	}
	if r, _ := use("store-uae", "Bad Name!"); r.StatusCode != 400 {
		t.Fatalf("a bad name: %d", r.StatusCode)
	}
	if r, _ := use("no-such-template", "huda-perfumes"); r.StatusCode != 404 {
		t.Fatalf("an unknown template: %d", r.StatusCode)
	}
	r, _ = use("store-uae", "huda-perfumes")
	if r.StatusCode != 303 || r.Header.Get("Location") != "/apps/huda-perfumes?new=store-uae" {
		t.Fatalf("after Use: %d to %s", r.StatusCode, r.Header.Get("Location"))
	}
	r, _ = owner.Get(e.base + r.Header.Get("Location"))
	if page := read(t, r); !strings.Contains(page, "huda-perfumes is ready on its private preview") || !strings.Contains(page, "Online store template") {
		t.Fatalf("the app page lacks the next steps: %d", r.StatusCode)
	}
	r, _ = owner.Get(e.base + "/apps/huda-perfumes")
	if strings.Contains(read(t, r), "is ready on its private preview") {
		t.Error("the next steps show on every visit")
	}
	info, err := e.p.Info("huda-perfumes")
	if err != nil || info.PreviewVersion != 1 || info.LiveVersion != 0 {
		t.Fatalf("the new app: %+v %v", info, err)
	}
	// A name in use is refused, with the reason on the card the owner chose.
	other := e.browser()
	e.signup(other, "zaid@shop.ae")
	r, err = other.PostForm(e.base+"/templates/booking-uae/use", url.Values{"csrf": {e.csrf(other, "/templates")}, "app": {"huda-perfumes"}})
	if err != nil {
		t.Fatal(err)
	}
	if page := read(t, r); r.StatusCode != 400 || !strings.Contains(page, "That name is taken") || !strings.Contains(page, `value="huda-perfumes" autofocus`) {
		t.Fatalf("a taken name: %d", r.StatusCode)
	}
	// Without the form's token nothing is made.
	if r, _ := owner.PostForm(e.base+"/templates/booking-uae/use", url.Values{"app": {"huda-two"}}); r.StatusCode != 403 {
		t.Fatalf("without csrf: %d", r.StatusCode)
	}
}
