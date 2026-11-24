package web

import (
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/arif-rachim/renoxium/internal/platform"
)

// Renaming an app: only its owner, signed in, with their password. An
// agent's rename_app tool only links here (renameLink), so text an agent
// read somewhere cannot move an app's address.
func (s *Site) registerRenameApp() {
	s.mux.HandleFunc("GET /apps/{app}/rename", s.renameAppPage)
	s.mux.HandleFunc("POST /apps/{app}/rename", s.renameApp)
}

func (s *Site) renameLink(app, to string) string {
	return s.base + "/apps/" + app + "/rename?to=" + url.QueryEscape(to)
}

func (s *Site) deleteLink(app string) string { return s.base + "/apps/" + app + "/delete" }

func (s *Site) renderRenameApp(w http.ResponseWriter, r *http.Request, app, to string, status int, errText string) {
	u, _ := s.currentUser(r)
	info, _ := s.platform.Info(app)
	data := map[string]any{"Shell": s.shell(u, app, "Rename app", "/apps/"+app+"/rename", false), "App": app, "Info": info,
		"To": to, "CSRF": s.csrfToken(w, r), "Error": errText, "Domain": s.platform.AppsDomain(), "Email": u.Email,
		"RedirectDays": int(platform.RenameRedirect.Hours() / 24)}
	if to != "" && errText == "" {
		if err := s.platform.CheckNewName(app, to); err != nil {
			data["Error"] = err.Error()
		} else {
			data["Ready"] = true
		}
	}
	render(w, status, "admin_rename", data)
}

func (s *Site) renameAppPage(w http.ResponseWriter, r *http.Request) {
	if _, app, ok := s.owner(w, r); ok {
		s.renderRenameApp(w, r, app, strings.TrimSpace(r.URL.Query().Get("to")), http.StatusOK, "")
	}
}

func (s *Site) renameApp(w http.ResponseWriter, r *http.Request) {
	u, app, ok := s.owner(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
	if !s.checkCSRF(r) {
		http.Error(w, "the form expired; go back and try again", http.StatusForbidden)
		return
	}
	to := strings.ToLower(strings.TrimSpace(r.PostFormValue("to")))
	if !s.limiter.allow("rename:" + u.ID) {
		s.renderRenameApp(w, r, app, to, http.StatusTooManyRequests, "Too many attempts. Try again later.")
		return
	}
	if _, err := s.accounts.Authenticate(u.Email, r.PostFormValue("password")); err != nil {
		s.renderRenameApp(w, r, app, to, http.StatusUnauthorized, "Your password is not right.")
		return
	}
	info, err := s.platform.RenameApp(r.Context(), app, to)
	if err != nil {
		s.renderRenameApp(w, r, app, to, http.StatusBadRequest, err.Error())
		return
	}
	slog.Info("app renamed", "user", u.ID, "app", app, "to", to)
	render(w, http.StatusOK, "notice", map[string]any{"Signed": true, "Title": "App renamed",
		"Message": app + " is now " + info.App + " at " + info.LiveURL + ". The old addresses send visitors to the new ones for a year.",
		"Link":    "/apps/" + info.App})
}
