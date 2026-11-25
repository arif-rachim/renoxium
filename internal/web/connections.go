package web

import (
	"net/http"
	"time"
)

func (s *Site) registerConnections() {
	s.mux.HandleFunc("GET /apps/{app}/connections", s.connectionsPage)
	s.mux.HandleFunc("POST /apps/{app}/connections", s.connectionsAction)
}

func (s *Site) renderConnections(w http.ResponseWriter, r *http.Request, app string, status int, errText string) {
	hosts, secrets, err := s.platform.Connections(app)
	if err != nil {
		serverError(w, err)
		return
	}
	used, quota := s.platform.FetchUsage(s.platform.OwnerOf(app))
	previewUntil := ""
	if t := s.platform.PreviewFetchUntil(app); time.Now().Before(t) {
		previewUntil = t.UTC().Format("15:04")
	}
	u0, _ := s.currentUser(r)
	render(w, status, "admin_connections", map[string]any{"Shell": s.shell(r, u0, app, "Connections", "/apps/"+app+"/connections", false), "App": app, "Hosts": hosts, "Secrets": secrets, "CSRF": s.csrfToken(w, r),
		"Error": errText, "Used": used, "Quota": quota, "PreviewUntil": previewUntil})
}

func (s *Site) connectionsPage(w http.ResponseWriter, r *http.Request) {
	if _, app, ok := s.owner(w, r); ok {
		s.renderConnections(w, r, app, http.StatusOK, "")
	}
}

// connectionsAction allows or revokes a host, or saves or removes a key.
// Only the owner decides: these let the app send data to other sites.
func (s *Site) connectionsAction(w http.ResponseWriter, r *http.Request) {
	u, app, ok := s.owner(w, r)
	if !ok {
		return
	}
	if !s.checkCSRF(r) {
		http.Error(w, "the form expired; go back and try again", http.StatusForbidden)
		return
	}
	var err error
	switch r.PostFormValue("action") {
	case "allow":
		err = s.platform.AllowHost(app, r.PostFormValue("host"), u.Email)
	case "revoke":
		err = s.platform.RevokeHost(app, r.PostFormValue("host"))
	case "secret":
		err = s.platform.SetSecret(app, r.PostFormValue("name"), r.PostFormValue("value"))
	case "delete-secret":
		err = s.platform.DeleteSecret(app, r.PostFormValue("name"))
	case "preview-on":
		err = s.platform.SetPreviewFetch(app, true)
	case "preview-off":
		err = s.platform.SetPreviewFetch(app, false)
	}
	if err != nil {
		s.renderConnections(w, r, app, http.StatusBadRequest, err.Error())
		return
	}
	http.Redirect(w, r, "/apps/"+app+"/connections", http.StatusSeeOther)
}
