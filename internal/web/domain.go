package web

import (
	"net/http"
)

func (s *Site) registerDomain() {
	s.mux.HandleFunc("GET /apps/{app}/domain", s.domainPage)
	s.mux.HandleFunc("POST /apps/{app}/domain", s.domainAction)
}

func (s *Site) renderDomain(w http.ResponseWriter, r *http.Request, app string, status int, errText string) {
	d, _ := s.platform.CustomDomain(app)
	u0, _ := s.currentUser(r)
	render(w, status, "admin_domain", map[string]any{"Shell": s.shell(u0, app, "Domain", "/apps/"+app+"/domain", false), "App": app, "Domain": d, "CSRF": s.csrfToken(w, r), "Error": errText,
		"Available": s.platform.CustomDomainsAvailable()})
}

func (s *Site) domainPage(w http.ResponseWriter, r *http.Request) {
	u, app, ok := s.owner(w, r)
	if !ok {
		return
	}
	if d, _ := s.platform.CustomDomain(app); d != nil && d.Status != "active" && s.apiLimiter.allow("domain:"+u.ID) {
		s.platform.RefreshCustomDomain(r.Context(), app) // show the newest state on every visit
	}
	s.renderDomain(w, r, app, http.StatusOK, "")
}

// domainAction adds, checks or removes the app's domain.
func (s *Site) domainAction(w http.ResponseWriter, r *http.Request) {
	u, app, ok := s.owner(w, r)
	if !ok {
		return
	}
	if !s.checkCSRF(r) {
		http.Error(w, "the form expired; go back and try again", http.StatusForbidden)
		return
	}
	if !s.apiLimiter.allow("domain:" + u.ID) {
		s.renderDomain(w, r, app, http.StatusTooManyRequests, "Too many checks. Try again in a few minutes.")
		return
	}
	var err error
	switch r.PostFormValue("action") {
	case "add":
		_, err = s.platform.SetCustomDomain(r.Context(), app, r.PostFormValue("host"))
	case "check":
		_, err = s.platform.RefreshCustomDomain(r.Context(), app)
	case "remove":
		err = s.platform.RemoveCustomDomain(r.Context(), app)
	}
	if err != nil {
		s.renderDomain(w, r, app, http.StatusBadRequest, err.Error())
		return
	}
	http.Redirect(w, r, "/apps/"+app+"/domain", http.StatusSeeOther)
}
