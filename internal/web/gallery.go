package web

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

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
	s.mux.HandleFunc("GET /templates/{example}/use", s.useTemplatePage)
	s.mux.HandleFunc("POST /templates/{example}/use", s.useTemplate)
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
		s.renderTemplatesApp(w, r, u)
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

// The template cards are one piece of markup and CSS for both pages that
// show them: the console (signed in) and the public gallery. They use the
// console's colour names; the public page maps its own onto them.
const galleryCardsCSS = `.tcards{display:grid;gap:14px;grid-template-columns:1fr}
@media(min-width:680px){.tcards{grid-template-columns:1fr 1fr}}@media(min-width:1000px){.tcards{grid-template-columns:repeat(3,minmax(0,1fr))}}@media(min-width:1180px){.tcards{grid-template-columns:repeat(4,minmax(0,1fr))}}
.tcard{background:var(--card);border:1px solid var(--line);border-radius:var(--radius);display:flex;flex-direction:column;overflow:hidden;scroll-margin-top:16px}
.tcard:target{border-color:var(--gold)}
.tcard .pic{display:block;flex:1 1 0;min-height:70px;background:var(--bg);border-bottom:1px solid var(--line);overflow:hidden}
.tcard .pic img{display:block;width:100%;height:100%;object-fit:cover;object-position:top}
.tcard .tx{padding:12px 14px 14px}
.tcard h3{font-size:16px;margin:0 0 2px}.tcard .job{font-size:12.5px;color:var(--muted);margin:0 0 6px}
.tcard p.sum{margin:0 0 8px;font-size:13.5px;line-height:1.45;color:var(--muted);display:-webkit-box;-webkit-box-orient:vertical;-webkit-line-clamp:3;overflow:hidden}
.tcard .tlinks{display:flex;flex-wrap:wrap;gap:2px 14px;font-size:13.5px;align-items:center}
.tcard .tlinks button{margin:0;min-height:0;padding:0;border:0;background:none;color:var(--fg);font-size:13.5px;font-weight:400;text-decoration:underline;text-decoration-color:var(--muted);text-underline-offset:3px}
.tcard.more{justify-content:center;padding:18px;background:var(--accent-soft);border-style:dashed}.tcard.more h3{font-size:17px;margin-bottom:6px}.tcard.more p{font-size:14px;color:var(--muted);margin:0 0 10px}
@media(max-width:999px){.tcard .pic{flex:none;height:auto;aspect-ratio:16/10}}`

const galleryCopyScript = `<script>document.addEventListener("click",function(e){var b=e.target.closest("[data-prompt]");if(!b)return;var t=b.getAttribute("data-prompt");if(navigator.clipboard)navigator.clipboard.writeText(t);b.textContent="Copied: paste it to your assistant"})</script>`

const galleryCardsHTML = `{{range .Groups}}{{$job := .Job}}{{range .Cards}}<article class="tcard" id="{{.Name}}">
<a class="pic" href="{{.DemoURL}}/" tabindex="-1">{{if .Wide.File}}<img src="{{.Wide.Src}}" width="{{.Wide.W}}" height="{{.Wide.H}}" alt="{{.Wide.Alt}}" loading="lazy">{{end}}</a>
<div class="tx"><h3>{{.Title}}</h3><p class="job">{{$job}}</p><p class="sum" title="{{.Summary}}">{{.Summary}}</p>
<p class="use"><a class="usebtn" href="/templates/{{.Name}}/use">Use this template</a></p>
<div class="tlinks"><a href="{{.DemoURL}}/">Try the demo</a><a href="{{.AdminURL}}">Admin panel</a><button type="button" data-prompt="{{.Prompt}}">Copy the request</button></div></div></article>{{end}}{{end}}
<article class="tcard more"><h3>Something in between?</h3><p>Start from the closest template and tell your assistant what to change: other fields on a form, your own sections and texts, your colours and photos.</p><a href="/connect">How to connect ChatGPT or Claude</a></article>`

func (s *Site) renderTemplatesApp(w http.ResponseWriter, r *http.Request, u accounts.User) {
	csrf := s.csrfToken(w, r)
	render(w, http.StatusOK, "templates_app", map[string]any{"Groups": galleryGroups(s.galleryCards()), "Shell": s.ownerShell(u, "Templates", "/templates", csrf)})
}

// galleryInfo returns the title of a template shown in the gallery.
func galleryInfo(name string) (string, bool) {
	e, ok := examples.Get(name)
	if !ok || e.Gallery == nil {
		return "", false
	}
	return e.Gallery.Title, true
}

// useTemplatePage asks for the business's name before making the owner's
// own app from a template, and says what happens after.
func (s *Site) useTemplatePage(w http.ResponseWriter, r *http.Request) {
	u, ok := s.currentUser(r)
	if !ok {
		http.Redirect(w, r, "/signup?next="+url.QueryEscape("/templates/"+r.PathValue("example")+"/use"), http.StatusSeeOther)
		return
	}
	card, ok := s.galleryCard(r.PathValue("example"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	s.renderUseTemplate(w, r, u, http.StatusOK, card, "", "", "")
}

func (s *Site) renderUseTemplate(w http.ResponseWriter, r *http.Request, u accounts.User, status int, card galleryCard, business, app, msg string) {
	csrf := s.csrfToken(w, r)
	render(w, status, "use_template", map[string]any{"Card": card, "Business": business, "Error": msg, "CSRF": csrf,
		"Shell": s.ownerShell(u, "Use "+card.Title, "/templates", csrf)})
}

func (s *Site) galleryCard(name string) (galleryCard, bool) {
	for _, c := range s.galleryCards() {
		if c.Name == name {
			return c, true
		}
	}
	return galleryCard{}, false
}

// appAddress turns a business name into an app name: "Lamsa Beauty
// Lounge" is lamsa-beauty-lounge.
func appAddress(business string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(business) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		case b.Len() > 0 && !dash:
			b.WriteByte('-')
			dash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if len(out) > 40 {
		out = strings.Trim(out[:40], "-")
	}
	if out != "" && (out[0] < 'a' || out[0] > 'z') {
		out = "my-" + out
	}
	return out
}

// useTemplate makes the owner's own app from a template, with the
// business's name and the template's sample texts on its private preview,
// without any AI: the owner sees a working app first, then changes it with
// their assistant.
func (s *Site) useTemplate(w http.ResponseWriter, r *http.Request) {
	u, ok := s.currentUser(r)
	if !ok {
		http.Redirect(w, r, "/signup?next="+url.QueryEscape("/templates/"+r.PathValue("example")+"/use"), http.StatusSeeOther)
		return
	}
	if !s.checkCSRF(r) {
		http.Error(w, "the form expired; go back and try again", http.StatusForbidden)
		return
	}
	card, ok := s.galleryCard(r.PathValue("example"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	business := strings.TrimSpace(r.PostFormValue("business"))
	if business == "" {
		s.renderUseTemplate(w, r, u, http.StatusBadRequest, card, business, "", "Enter your business's name.")
		return
	}
	if len([]rune(business)) > 80 {
		s.renderUseTemplate(w, r, u, http.StatusBadRequest, card, business, "", "Keep the name to 80 characters; your assistant can add a longer one later.")
		return
	}
	// The app's name comes from the business name; when it is taken, a
	// number is added (lamsa-salon-2).
	base := appAddress(business)
	if len(base) < 3 {
		base = "my-" + strings.TrimPrefix(card.Name, "my-")
	}
	var name string
	var err error
	for i := 1; i <= 20; i++ {
		name = base
		if i > 1 {
			name = strings.TrimRight(base[:min(len(base), 37)], "-") + "-" + strconv.Itoa(i)
		}
		if _, _, err = s.platform.CreateFromTemplate(r.Context(), u.ID, name, card.Name, business); err == nil ||
			!(strings.Contains(err.Error(), "already taken") || strings.Contains(err.Error(), "UNIQUE") || strings.Contains(err.Error(), "reserved")) {
			break
		}
	}
	if err != nil {
		s.renderUseTemplate(w, r, u, http.StatusBadRequest, card, business, "", err.Error())
		return
	}
	http.Redirect(w, r, "/apps/"+name+"?new="+url.QueryEscape(card.Name), http.StatusSeeOther)
}
