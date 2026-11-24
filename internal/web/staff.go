package web

import (
	"fmt"
	"log/slog"
	"net/http"

	"github.com/arif-rachim/renoxium/internal/email"
	"github.com/arif-rachim/renoxium/internal/platform"
)

func (s *Site) registerStaff() {
	s.mux.HandleFunc("GET /apps/{app}/staff", s.staffPage)
	s.mux.HandleFunc("POST /apps/{app}/staff", s.addStaff)
	s.mux.HandleFunc("POST /apps/{app}/staff/remove", s.removeStaff)
}

func (s *Site) renderStaff(w http.ResponseWriter, r *http.Request, app string, status int, errText string) {
	staff, _ := s.platform.Staff(app)
	u0, _ := s.currentUser(r)
	render(w, status, "admin_staff", map[string]any{"Shell": s.shell(u0, app, "Staff", "/apps/"+app+"/staff", false), "App": app, "Staff": staff, "Limit": s.platform.StaffLimit(app), "CSRF": s.csrfToken(w, r), "Error": errText,
		"SeatNote": s.seatNote(app, len(staff))})
}

// seatNote tells the owner of a paid work app what the next staff member costs.
func (s *Site) seatNote(app string, staff int) string {
	pl, err := s.platform.AppPlan(app)
	if err != nil || !pl.WorkPaid || pl.Included || s.Billing.PriceWorkSeat == "" {
		return ""
	}
	if staff < platform.WorkAppUsers {
		return fmt.Sprintf("%d staff members are included in the work app's price; each one more adds AED %d a month, up to %d.",
			platform.WorkAppUsers, platform.WorkSeatPriceAED, platform.WorkAppMaxUsers)
	}
	return fmt.Sprintf("Adding a staff member adds AED %d a month to this app's subscription, charged for the rest of this month now; removing one lowers it.",
		platform.WorkSeatPriceAED)
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
	// A work app pays for each staff member beyond the included ones.
	if err := s.syncWorkSeats(r.Context(), app); err != nil {
		slog.Error("work app seats", "app", app, "err", err)
		s.platform.RemoveStaff(app, addr)
		s.renderStaff(w, r, app, http.StatusServiceUnavailable, "Stripe could not add this staff member to the app's subscription; try again in a minute.")
		return
	}
	slog.Info("staff added", "app", app, "by", u.ID)
	if s.mailLimiter.allow("staff-invite:" + app) {
		s.send(r.Context(), email.Message{To: addr, Tag: "staff_invite", ReplyTo: u.Email, Subject: "You can now help run " + app + " on Renoxium",
			Text: u.Label() + " added you as staff of " + app + ".\n\nSign in or create a Renoxium account with this email address, confirm the address, " +
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
	if err := s.syncWorkSeats(r.Context(), app); err != nil {
		slog.Error("work app seats", "app", app, "err", err) // the next change of staff tries again
	}
	http.Redirect(w, r, "/apps/"+app+"/staff", http.StatusSeeOther)
}
