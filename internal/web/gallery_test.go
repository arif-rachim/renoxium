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
	// Grouped by what the owner does, each with a screenshot of its demo.
	for _, want := range []string{"I sell products", "I take bookings", "I send quotes", "I deliver", "/static/img/screen-booking-slots.webp?v="} {
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
