package web

import (
	"net/http"
)

func (s *Site) registerAlerts() {
	s.mux.HandleFunc("GET /apps/{app}/notifications", s.alertsPage)
	s.mux.HandleFunc("POST /apps/{app}/notifications", s.alertsAction)
}

func (s *Site) renderAlerts(w http.ResponseWriter, r *http.Request, app string, status int, errText string) {
	u, _ := s.currentUser(r)
	used, quota := s.platform.WhatsAppUsage(u.ID)
	u0, _ := s.currentUser(r)
	render(w, status, "admin_alerts", map[string]any{"Shell": s.shell(u0, app, "Notifications", "/apps/"+app+"/notifications", false), "App": app, "Phone": s.platform.AlertPhoneOf(app), "CSRF": s.csrfToken(w, r),
		"Error": errText, "Available": s.platform.WhatsAppAlertsAvailable(), "Email": u.Email, "Used": used, "Quota": quota})
}

func (s *Site) alertsPage(w http.ResponseWriter, r *http.Request) {
	if _, app, ok := s.owner(w, r); ok {
		s.renderAlerts(w, r, app, http.StatusOK, "")
	}
}

// alertsAction sends a confirmation code, confirms it, or turns notices off.
func (s *Site) alertsAction(w http.ResponseWriter, r *http.Request) {
	_, app, ok := s.owner(w, r)
	if !ok {
		return
	}
	if !s.checkCSRF(r) {
		http.Error(w, "the form expired; go back and try again", http.StatusForbidden)
		return
	}
	var err error
	switch r.PostFormValue("action") {
	case "send":
		err = s.platform.StartAlertPhone(r.Context(), app, r.PostFormValue("phone"))
	case "confirm":
		err = s.platform.ConfirmAlertPhone(app, r.PostFormValue("code"))
	case "remove":
		err = s.platform.RemoveAlertPhone(app)
	}
	if err != nil {
		s.renderAlerts(w, r, app, http.StatusBadRequest, err.Error())
		return
	}
	http.Redirect(w, r, "/apps/"+app+"/notifications", http.StatusSeeOther)
}
