package web

import (
	"context"
	"fmt"
	"html"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/arif-rachim/renoxium/internal/accounts"
	"github.com/arif-rachim/renoxium/internal/email"
)

// Accounts without activity for accounts.InactiveMonths are deactivated;
// see internal/accounts/inactivity.go for the rule and what counts.

// noteActivity records that u used Renoxium: their own account, and the
// accounts of the apps they are staff of (a staff member signing in keeps
// the shop of their employer).
func (s *Site) noteActivity(u accounts.User) {
	if !s.accounts.NoteActivity(u.ID) {
		return // within the hour of the last one: already recorded
	}
	if !u.Verified { // Authenticate does not read it
		if full, err := s.accounts.User(u.ID); err == nil {
			u = full
		}
	}
	if !u.Verified {
		return // staff access needs a confirmed email, as on the dashboard
	}
	apps, err := s.platform.StaffApps(u.Email)
	if err != nil {
		return
	}
	seen := map[string]bool{u.ID: true}
	for _, a := range apps {
		if owner := s.platform.OwnerOf(a.App); owner != "" && !seen[owner] {
			seen[owner] = true
			s.accounts.NoteActivity(owner)
		}
	}
}

// inactivityExempt reports whether an account is never deactivated: it is
// on a paid or granted plan, or pays for a work app.
func (s *Site) inactivityExempt(userID string) bool {
	return s.accounts.Plan(userID) != accounts.PlanFree || s.platform.HasPaidWorkApp(userID)
}

// InactivityResult counts what one CheckInactivity did.
type InactivityResult struct{ Warned, Deactivated, Reactivated int }

// CheckInactivity warns accounts that will be deactivated in
// accounts.WarnDays, deactivates warned accounts whose date has come, and
// reactivates deactivated accounts that have become exempt. Run it daily;
// it is safe to repeat. now is the time to judge by (time.Now on the server).
func (s *Site) CheckInactivity(ctx context.Context, now time.Time) (InactivityResult, error) {
	var res InactivityResult
	if s.SingleApp != "" {
		return res, nil // self-hosted: the owner's own server
	}
	// Accounts whose deactivation is due within WarnDays (a day's margin
	// here for months of different lengths; the exact test is below).
	warnBy := now.AddDate(0, 0, accounts.WarnDays)
	idle, err := s.accounts.IdleAccounts(now.AddDate(0, -accounts.InactiveMonths, accounts.WarnDays+1))
	if err != nil {
		return res, err
	}
	for _, a := range idle {
		if ctx.Err() != nil {
			return res, ctx.Err()
		}
		if s.inactivityExempt(a.UserID) {
			if !a.WarnedAt.IsZero() {
				s.accounts.ClearWarning(a.UserID)
			}
			continue
		}
		if a.WarnedAt.IsZero() && accounts.DeactivationDue(a.ActiveAt).After(warnBy) {
			continue // not due yet
		}
		if a.WarnedAt.IsZero() {
			// The owner always gets the full WarnDays, even when the warning
			// is late (the server was down, or activity predates recording).
			on := accounts.DeactivationDue(a.ActiveAt)
			if on.Before(warnBy) {
				on = warnBy
			}
			if err := s.sendInactivityWarning(ctx, a, on); err != nil {
				slog.Error("inactivity warning not sent; tried again tomorrow", "user", a.UserID, "err", err)
				continue
			}
			if ok, err := s.accounts.MarkWarned(a.UserID, a.ActiveAt, on); err != nil {
				slog.Error("inactivity warning not recorded", "user", a.UserID, "err", err)
			} else if ok {
				res.Warned++
			}
			continue
		}
		if now.Before(a.DeactivateOn) {
			continue
		}
		ok, err := s.accounts.Deactivate(a.UserID)
		if err != nil {
			slog.Error("deactivate account", "user", a.UserID, "err", err)
			continue
		}
		if ok {
			s.platform.ForgetPaused(a.UserID)
			slog.Info("account deactivated after inactivity", "user", a.UserID, "last_active", a.ActiveAt.UTC().Format(time.DateOnly))
			res.Deactivated++
		}
	}
	// An account that started paying (or was given a plan) is never
	// deactivated, so it comes back without the owner having to press anything.
	gone, err := s.accounts.DeactivatedAccounts()
	if err != nil {
		return res, err
	}
	for _, a := range gone {
		if s.inactivityExempt(a.UserID) && s.accounts.Reactivate(a.UserID) == nil {
			s.platform.ForgetPaused(a.UserID)
			res.Reactivated++
		}
	}
	return res, nil
}

func (s *Site) sendInactivityWarning(ctx context.Context, a accounts.Activity, on time.Time) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancel()
	date := on.UTC().Format("2 January 2006")
	link := s.base + "/login"
	return s.Mailer.Send(ctx, email.Message{To: a.Email, Tag: "inactivity_warning",
		Subject: "Your Renoxium account will be deactivated on " + date,
		Text: fmt.Sprintf("Your Renoxium account %s has not been used for a long time. It will be deactivated on %s.\n\n"+
			"Signing in before then keeps it:\n\n%s\n\n"+
			"When an account is deactivated, its live apps show a \"paused\" page to visitors. Nothing is deleted: you can bring it back "+
			"at any time by signing in and pressing Reactivate on your dashboard.\n", a.Email, date, link),
		HTML: fmt.Sprintf("<p>Your Renoxium account %s has not been used for a long time. It will be deactivated on <strong>%s</strong>.</p>"+
			"<p><a href=\"%s\">Sign in</a> before then to keep it.</p>"+
			"<p>When an account is deactivated, its live apps show a \"paused\" page to visitors. Nothing is deleted: you can bring it back "+
			"at any time by signing in and pressing Reactivate on your dashboard.</p>", html.EscapeString(a.Email), date, link)})
}

// reactivate is the owner's Reactivate button on the dashboard.
func (s *Site) reactivate(w http.ResponseWriter, r *http.Request) {
	u, ok := s.currentUser(r)
	if !ok || !s.checkCSRF(r) {
		http.Error(w, "the form expired; go back and try again", http.StatusForbidden)
		return
	}
	if err := s.accounts.Reactivate(u.ID); err != nil {
		serverError(w, err)
		return
	}
	s.platform.ForgetPaused(u.ID)
	slog.Info("account reactivated by its owner", "user", u.ID)
	http.Redirect(w, r, "/?reactivated=1", http.StatusSeeOther)
}

// operatorReactivate lets an operator bring a deactivated account back.
func (s *Site) operatorReactivate(w http.ResponseWriter, r *http.Request) {
	op, ok := s.operator(w, r)
	if !ok {
		return
	}
	if !s.checkCSRF(r) {
		http.Error(w, "the form expired; go back and try again", http.StatusForbidden)
		return
	}
	user := r.PostFormValue("user")
	if err := s.accounts.Reactivate(user); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.platform.ForgetPaused(user)
	slog.Info("operator reactivated account", "operator", op.Email, "user", user)
	back := "/operator/accounts?q=" + url.QueryEscape(r.PostFormValue("q"))
	if r.PostFormValue("deactivated") == "1" {
		back += "&deactivated=1"
	}
	http.Redirect(w, r, back, http.StatusSeeOther)
}
