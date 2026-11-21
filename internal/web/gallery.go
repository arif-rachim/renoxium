package web

import (
	"net/http"

	"github.com/arif-rachim/renoxium/examples"
	"github.com/arif-rachim/renoxium/internal/accounts"
	"github.com/arif-rachim/renoxium/internal/platform"
)

// The template gallery: every example with gallery words, each with a live
// demo (see platform/demo.go) and the owner's admin panel of that demo,
// read-only and open to everyone.
func (s *Site) registerGallery() {
	s.mux.HandleFunc("GET /templates", s.galleryPage)
	s.mux.HandleFunc("GET /templates/{example}", s.galleryDemo)
	s.mux.HandleFunc("GET /templates/{example}/admin", s.galleryAdmin)
}

type galleryCard struct {
	Name, Title, Summary, DemoURL, AdminURL, Prompt, Job, Suits string
	Screen, Wide                                                screen // a phone screenshot, and a desktop one for the console
}

// galleryGroup is the templates for one job of the owner, in gallery order.
type galleryGroup struct {
	Job   string
	Cards []galleryCard
}

func galleryGroups(cards []galleryCard) []galleryGroup {
	var out []galleryGroup
	for _, c := range cards {
		if n := len(out); n > 0 && out[n-1].Job == c.Job {
			out[n-1].Cards = append(out[n-1].Cards, c)
			continue
		}
		out = append(out, galleryGroup{Job: c.Job, Cards: []galleryCard{c}})
	}
	return out
}

func (s *Site) galleryCards() []galleryCard {
	var out []galleryCard
	for _, e := range examples.Gallery() {
		out = append(out, galleryCard{Name: e.Name, Title: e.Gallery.Title, Summary: e.Gallery.Summary, Suits: suits(e.Gallery.For),
			DemoURL: s.platform.DemoURL(e.Name), AdminURL: "/templates/" + e.Name + "/admin",
			Screen: screens[e.Name], Wide: screensWide[e.Name], Job: exampleJob[e.Name],
			Prompt: "Use Renoxium to build me a website from the " + e.Name + " template for my business, <your business name>."})
	}
	return out
}

func (s *Site) galleryPage(w http.ResponseWriter, r *http.Request) {
	groups := galleryGroups(s.galleryCards())
	// Signed in, the templates sit in the console like the other account
	// pages; the page differs by visitor, so it is not cached.
	if u, ok := s.currentUser(r); ok {
		render(w, http.StatusOK, "templates_app", map[string]any{"Groups": groups, "Shell": s.ownerShell(u, "Templates", "/templates", s.csrfToken(w, r))})
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	landingPages.ExecuteTemplate(w, "gallery", map[string]any{"Groups": groups, "Legal": s.legalOrDefaults(), "Base": s.base})
}

func galleryExample(r *http.Request) (examples.Example, bool) {
	e, ok := examples.Get(r.PathValue("example"))
	return e, ok && e.Gallery != nil
}

func (s *Site) galleryDemo(w http.ResponseWriter, r *http.Request) {
	e, ok := galleryExample(r)
	if !ok {
		notFound(w, nil)
		return
	}
	http.Redirect(w, r, s.platform.DemoURL(e.Name)+"/", http.StatusSeeOther)
}

func (s *Site) galleryAdmin(w http.ResponseWriter, r *http.Request) {
	e, ok := galleryExample(r)
	if !ok {
		notFound(w, nil)
		return
	}
	http.Redirect(w, r, "/apps/"+platform.DemoApp(e.Name), http.StatusSeeOther)
}

// viewer is member for the admin pages that only show data, and also lets
// anyone in, without signing in, to the admin panel of a template demo.
// demo tells the page to show no way to change anything; the routes that
// change data use owner or member, which no account passes for a demo.
func (s *Site) viewer(w http.ResponseWriter, r *http.Request) (u accounts.User, app string, demo, ok bool) {
	app = r.PathValue("app")
	if (r.Method == http.MethodGet || r.Method == http.MethodHead) && s.platform.IsDemo(app) {
		return accounts.User{}, app, true, true
	}
	u, app, ok = s.member(w, r)
	return u, app, false, ok
}
