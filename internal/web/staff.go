package web

import (
	"log/slog"
	"net/http"

	"github.com/arif-rachim/renoxium/internal/email"
)

func (s *Site) registerStaff() {
	s.mux.HandleFunc("GET /apps/{app}/staff", s.staffPage)
	s.mux.HandleFunc("POST /apps/{app}/staff", s.addStaff)
	s.mux.HandleFunc("POST /apps/{app}/staff/remove", s.removeStaff)
}

func (s *Site) renderStaff(w http.ResponseWriter, r *http.Request, app string, status int, errText string) {
	staff, _ := s.platform.Staff(app)
	u0, _ := s.currentUser(r)
	render(w, status, "admin_staff", map[string]any{"Shell": s.shell(u0, app, "Staff", "/apps/"+app+"/staff", false), "App": app, "Staff": staff, "Limit": s.platform.StaffLimit(app), "CSRF": s.csrfToken(w, r), "Error": errText})
}

func (s *Site) staffPage(w http.ResponseWriter, r *http.Request) {
	_, app, ok := s.owner(w, r)
	if !ok {
		return
	}
	s.renderStaff(w, r, app, http.StatusOK, "")
}

func (s *Site) addStaff(w http.ResponseWriter, r *http.Request) {
	u, app, ok := s.owner(w, r)
	if !ok {
		return
	}
	if !s.checkCSRF(r) {
		http.Error(w, "the form expired; go back and try again", http.StatusForbidden)
		return
	}
	addr := r.PostFormValue("email")
	if err := s.platform.AddStaff(app, addr, u.ID); err != nil {
		s.renderStaff(w, r, app, http.StatusBadRequest, err.Error())
		return
	}
	slog.Info("staff added", "app", app, "by", u.ID)
	if s.mailLimiter.allow("staff-invite:" + app) {
		s.send(r.Context(), email.Message{To: addr, Tag: "staff_invite", ReplyTo: u.Email, Subject: "You can now help run " + app + " on Renoxium",
			Text: u.Email + " added you as staff of " + app + ".\n\nSign in or create a Renoxium account with this email address, confirm the address, " +
				"and open:\n\n" + s.base + "/apps/" + app + "\n"})
	}
	http.Redirect(w, r, "/apps/"+app+"/staff", http.StatusSeeOther)
}

func (s *Site) removeStaff(w http.ResponseWriter, r *http.Request) {
	_, app, ok := s.owner(w, r)
	if !ok {
		return
	}
	if !s.checkCSRF(r) {
		http.Error(w, "the form expired; go back and try again", http.StatusForbidden)
		return
	}
	if err := s.platform.RemoveStaff(app, r.PostFormValue("email")); err != nil {
		serverError(w, err)
		return
	}
	http.Redirect(w, r, "/apps/"+app+"/staff", http.StatusSeeOther)
}
