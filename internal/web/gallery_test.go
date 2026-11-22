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

// "Use this template" opens a page that asks for the business's name and
// says what happens next; it makes the owner's own app from the template on
// its private preview, with that name, without an assistant. Signed out,
// it leads to sign-up and back to the same page.
func TestUseTemplate(t *testing.T) {
	e := newEnv(t)
	r, _ := http.Get(e.base + "/templates")
	if page := read(t, r); !strings.Contains(page, `href="/templates/store-uae/use"`) {
		t.Fatal("the public gallery has no Use this template link")
	}
	c := e.browser()
	r, _ = c.Get(e.base + "/templates/store-uae/use")
	if r.StatusCode != 303 || r.Header.Get("Location") != "/signup?next=%2Ftemplates%2Fstore-uae%2Fuse" {
		t.Fatalf("signed out: %d %s", r.StatusCode, r.Header.Get("Location"))
	}
	owner := e.browser()
	e.signup(owner, "huda@shop.ae")
	r, _ = owner.Get(e.base + "/templates/store-uae/use")
	page := read(t, r)
	for _, want := range []string{"Use the Online store template", `name="business"`, "What happens next", "Connect ChatGPT or Claude", "free plan is enough", "No credits", "Create my app"} {
		if !strings.Contains(page, want) {
			t.Errorf("use page lacks %q", want)
		}
	}
	if r, _ := owner.Get(e.base + "/templates/no-such/use"); r.StatusCode != 404 {
		t.Fatalf("an unknown template: %d", r.StatusCode)
	}
	csrf := e.csrf(owner, "/templates/store-uae/use")
	post := func(c *http.Client, csrf, example string, v url.Values) (*http.Response, string) {
		v.Set("csrf", csrf)
		r, err := c.PostForm(e.base+"/templates/"+example+"/use", v)
		if err != nil {
			t.Fatal(err)
		}
		return r, read(t, r)
	}
	if r, page := post(owner, csrf, "store-uae", url.Values{"business": {""}}); r.StatusCode != 400 || !strings.Contains(page, "Enter your business") {
		t.Fatalf("no name: %d", r.StatusCode)
	}
	// The address comes from the name when left empty, and the name is in the app's settings.
	r, _ = post(owner, csrf, "store-uae", url.Values{"business": {"Huda's Perfumes & Oud"}})
	if r.StatusCode != 303 || r.Header.Get("Location") != "/apps/huda-s-perfumes-oud?new=store-uae" {
		t.Fatalf("after Use: %d to %s", r.StatusCode, r.Header.Get("Location"))
	}
	r, _ = owner.Get(e.base + r.Header.Get("Location"))
	if page := read(t, r); !strings.Contains(page, "huda-s-perfumes-oud is ready on its private preview") || !strings.Contains(page, "Online store template with your business name") {
		t.Fatalf("the app page lacks the next steps: %d", r.StatusCode)
	}
	if info, err := e.p.Info("huda-s-perfumes-oud"); err != nil || info.PreviewVersion != 1 || info.LiveVersion != 0 {
		t.Fatalf("the new app: %+v %v", info, err)
	}
	if def, _, err := e.p.Definition("huda-s-perfumes-oud", 0); err != nil || def.Seed["store"][0]["name"] != "Huda's Perfumes & Oud" || def.Seed["store"][0]["name_ar"] != "Huda's Perfumes & Oud" {
		t.Errorf("the business name is not in the store settings: %v", err)
	}
	r, _ = owner.Get(e.base + "/apps/huda-s-perfumes-oud")
	if strings.Contains(read(t, r), "is ready on its private preview") {
		t.Error("the next steps show on every visit")
	}
	// The same name for another owner gets a number added instead of an error.
	other := e.browser()
	e.signup(other, "zaid@shop.ae")
	r, _ = post(other, e.csrf(other, "/templates/booking-uae/use"), "booking-uae", url.Values{"business": {"Huda's Perfumes & Oud"}})
	if r.StatusCode != 303 || r.Header.Get("Location") != "/apps/huda-s-perfumes-oud-2?new=booking-uae" {
		t.Fatalf("a taken name: %d to %s", r.StatusCode, r.Header.Get("Location"))
	}
	// Without the form's token nothing is made.
	if r, _ := owner.PostForm(e.base+"/templates/booking-uae/use", url.Values{"business": {"Two"}}); r.StatusCode != 403 {
		t.Fatalf("without csrf: %d", r.StatusCode)
	}
}
