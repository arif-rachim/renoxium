package web

import (
	"archive/zip"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/arif-rachim/renoxium/internal/platform"
)

// Deleting one app: only its owner, from the console (never from an agent's
// tools), after typing the app's name and their password. An app whose
// customers still have running subscriptions cannot be deleted, so nobody
// keeps being charged by an app that is gone.
func (s *Site) registerDeleteApp() {
	s.mux.HandleFunc("GET /apps/{app}/delete", s.deleteAppPage)
	s.mux.HandleFunc("POST /apps/{app}/delete", s.deleteApp)
	s.mux.HandleFunc("GET /apps/{app}/export", s.exportApp)
}

func (s *Site) renderDeleteApp(w http.ResponseWriter, r *http.Request, app string, status int, errText string) {
	u, _ := s.currentUser(r)
	running, err := s.platform.RunningSubscriptions(r.Context(), app)
	if err != nil {
		slog.Error("count subscriptions", "app", app, "err", err)
	}
	info, _ := s.platform.Info(app)
	render(w, status, "admin_delete", map[string]any{"Shell": s.shell(u, app, "Delete app", "/apps/"+app+"/delete", false), "App": app,
		"Info": info, "Running": running, "Losses": s.appLosses(r, app), "CSRF": s.csrfToken(w, r), "Error": errText, "Reauth": s.reauthView(r, u, "dpassword")})
}

// loss is one kind of data a delete destroys, with how much of it there is.
type loss struct {
	Label string
	Count int
}

// appLosses counts what the owner would lose: customer accounts and the
// rows of each table that has any.
func (s *Site) appLosses(r *http.Request, app string) []loss {
	var out []loss
	if _, n, err := s.platform.Visitors(r.Context(), app, "", 1); err == nil && n > 0 {
		out = append(out, loss{"Customer accounts", n})
	}
	tables, err := s.platform.Tables(app)
	if err != nil {
		return out
	}
	for _, t := range tables {
		if _, n, err := s.platform.AdminQuery(r.Context(), app, t.Name, platform.ListQuery{PageSize: 25}); err == nil && n > 0 {
			out = append(out, loss{tableLabel(t), n})
		}
	}
	return out
}

func (s *Site) deleteAppPage(w http.ResponseWriter, r *http.Request) {
	if _, app, ok := s.owner(w, r); ok {
		s.renderDeleteApp(w, r, app, http.StatusOK, "")
	}
}

func (s *Site) deleteApp(w http.ResponseWriter, r *http.Request) {
	u, app, ok := s.owner(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
	if !s.checkCSRF(r) {
		http.Error(w, "the form expired; go back and try again", http.StatusForbidden)
		return
	}
	if !s.limiter.allow("delete:" + u.ID) {
		s.renderDeleteApp(w, r, app, http.StatusTooManyRequests, "Too many attempts. Try again later.")
		return
	}
	if wantsReauthCode(r, u) {
		if err := s.sendReauthCode(r, u); err != nil {
			s.renderDeleteApp(w, r, app, http.StatusTooManyRequests, capitalize(err.Error())+".")
			return
		}
		s.renderDeleteApp(w, r, app, http.StatusOK, "")
		return
	}
	if strings.TrimSpace(r.PostFormValue("confirm")) != app {
		s.renderDeleteApp(w, r, app, http.StatusBadRequest, "Type the app's name exactly to confirm.")
		return
	}
	if err := s.checkReauth(r, u); err != nil {
		s.renderDeleteApp(w, r, app, http.StatusUnauthorized, err.Error())
		return
	}
	running, err := s.platform.RunningSubscriptions(r.Context(), app)
	if err != nil {
		serverError(w, err)
		return
	}
	if running > 0 {
		s.renderDeleteApp(w, r, app, http.StatusConflict, "Cancel the running subscriptions in Payments first.")
		return
	}
	if pl, err := s.platform.AppPlan(app); err == nil && pl.WorkPaid && pl.WorkSubscription != "" {
		if s.Billing.Stripe == nil {
			s.renderDeleteApp(w, r, app, http.StatusConflict, "This work app's subscription could not be cancelled here; cancel it in Plan and billing first.")
			return
		}
		if err := s.Billing.Stripe.CancelSubscription(r.Context(), pl.WorkSubscription); err != nil {
			slog.Error("cancel work app subscription", "user", u.ID, "app", app, "err", err)
			s.renderDeleteApp(w, r, app, http.StatusServiceUnavailable, "Stripe could not cancel this app's subscription; try again in a minute.")
			return
		}
		slog.Info("work app subscription cancelled for app deletion", "user", u.ID, "app", app, "subscription", pl.WorkSubscription)
	}
	if err := s.platform.DeleteApp(app); err != nil {
		slog.Error("delete app", "user", u.ID, "app", app, "err", err)
		s.renderDeleteApp(w, r, app, http.StatusServiceUnavailable, "The app could not be deleted: "+err.Error())
		return
	}
	slog.Info("app deleted", "user", u.ID, "app", app)
	render(w, http.StatusOK, "notice", map[string]any{"Title": "App deleted",
		"Message": fmt.Sprintf("%s and all its data were deleted. Your Stripe account is yours and was not changed.", app), "Link": "/"})
}

// exportApp downloads one app's definition, live data and file list.
func (s *Site) exportApp(w http.ResponseWriter, r *http.Request) {
	u, app, ok := s.owner(w, r)
	if !ok {
		return
	}
	if !s.limiter.allow("export:" + u.ID) {
		http.Error(w, "too many exports; try again later", http.StatusTooManyRequests)
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s-%s.zip"`, app, time.Now().UTC().Format("2006-01-02")))
	zw := zip.NewWriter(w)
	defer zw.Close()
	if err := s.platform.ExportApp(r.Context(), app, zw); err != nil {
		slog.Error("export", "user", u.ID, "app", app, "err", err)
	}
}
