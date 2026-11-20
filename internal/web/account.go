package web

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/arif-rachim/renoxium/internal/accounts"
)

func (s *Site) registerAccount() {
	s.mux.HandleFunc("GET /account", s.accountPage)
	s.mux.HandleFunc("POST /account/password", s.changePassword)
	s.mux.HandleFunc("GET /account/export", s.exportAccount)
	s.mux.HandleFunc("POST /account/delete", s.deleteAccount)
}

func (s *Site) accountPage(w http.ResponseWriter, r *http.Request) {
	u, ok := s.currentUser(r)
	if !ok {
		http.Redirect(w, r, "/login?next=%2Faccount", http.StatusSeeOther)
		return
	}
	s.renderAccount(w, r, u, http.StatusOK, map[string]any{"Changed": r.URL.Query().Get("changed") == "1"})
}

func (s *Site) renderAccount(w http.ResponseWriter, r *http.Request, u accounts.User, status int, extra map[string]any) {
	apps, _ := s.platform.Apps(u.ID)
	b, _ := s.accounts.Billing(u.ID)
	csrf := s.csrfToken(w, r)
	data := map[string]any{"User": u, "CSRF": csrf, "Apps": apps, "Billing": b, "PlanName": planName(b.Plan),
		"Shell": s.ownerShell(u, "Account", "/account", csrf)}
	for k, v := range extra {
		data[k] = v
	}
	render(w, status, "account", data)
}

func (s *Site) changePassword(w http.ResponseWriter, r *http.Request) {
	u, ok := s.currentUser(r)
	if !ok || !s.checkCSRF(r) {
		http.Error(w, "the form expired; go back and try again", http.StatusForbidden)
		return
	}
	if !s.limiter.allow("password:" + u.ID) {
		http.Error(w, "too many attempts; try again later", http.StatusTooManyRequests)
		return
	}
	err := s.accounts.ChangePassword(u.ID, r.PostFormValue("current"), r.PostFormValue("password"))
	switch {
	case errors.Is(err, accounts.ErrBadCredentials):
		s.renderAccount(w, r, u, http.StatusUnauthorized, map[string]any{"PasswordError": "Your current password is not right."})
	case err != nil:
		s.renderAccount(w, r, u, http.StatusBadRequest, map[string]any{"PasswordError": err.Error()})
	default:
		s.startSession(w, r, u, "/account?changed=1")
	}
}

// exportAccount sends a zip with the account's details and, per app, its
// definition, live database and file list.
func (s *Site) exportAccount(w http.ResponseWriter, r *http.Request) {
	u, ok := s.currentUser(r)
	if !ok {
		http.Redirect(w, r, "/login?next=%2Faccount", http.StatusSeeOther)
		return
	}
	if !s.limiter.allow("export:" + u.ID) {
		http.Error(w, "too many exports; try again later", http.StatusTooManyRequests)
		return
	}
	apps, err := s.platform.Apps(u.ID)
	if err != nil {
		serverError(w, err)
		return
	}
	b, _ := s.accounts.Billing(u.ID)
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="renoxium-export-%s.zip"`, time.Now().UTC().Format("2006-01-02")))
	zw := zip.NewWriter(w)
	defer zw.Close()
	if f, err := zw.Create("account.json"); err == nil {
		enc := json.NewEncoder(f)
		enc.SetIndent("", " ")
		enc.Encode(map[string]any{"email": u.Email, "email_confirmed": u.Verified, "plan": b.Plan, "exported_at": time.Now().UTC(), "apps": len(apps)})
	}
	for _, a := range apps {
		if err := s.platform.ExportApp(r.Context(), a.App, zw); err != nil {
			slog.Error("export", "user", u.ID, "app", a.App, "err", err)
			return
		}
	}
}

// deleteAccount removes the account and all its apps after the owner
// confirms with their password and email. A running paid subscription is
// cancelled at Stripe first, so nobody is billed for a deleted account.
func (s *Site) deleteAccount(w http.ResponseWriter, r *http.Request) {
	u, ok := s.currentUser(r)
	if !ok || !s.checkCSRF(r) {
		http.Error(w, "the form expired; go back and try again", http.StatusForbidden)
		return
	}
	if !s.limiter.allow("delete:" + u.ID) {
		http.Error(w, "too many attempts; try again later", http.StatusTooManyRequests)
		return
	}
	if !strings.EqualFold(strings.TrimSpace(r.PostFormValue("confirm")), u.Email) {
		s.renderAccount(w, r, u, http.StatusBadRequest, map[string]any{"DeleteError": "Type your email address exactly to confirm."})
		return
	}
	if _, err := s.accounts.Authenticate(u.Email, r.PostFormValue("password")); err != nil {
		s.renderAccount(w, r, u, http.StatusUnauthorized, map[string]any{"DeleteError": "Your password is not right."})
		return
	}
	if err := s.cancelSubscriptions(r.Context(), u.ID); err != nil {
		slog.Error("cancel subscription before delete", "user", u.ID, "err", err)
		s.renderAccount(w, r, u, http.StatusBadGateway, map[string]any{"DeleteError": "Your paid plan could not be cancelled at Stripe, so nothing was deleted. Try again in a minute."})
		return
	}
	apps, err := s.platform.Apps(u.ID)
	if err != nil {
		serverError(w, err)
		return
	}
	for _, a := range apps {
		if err := s.platform.DeleteApp(a.App); err != nil {
			slog.Error("delete app", "user", u.ID, "app", a.App, "err", err)
			serverError(w, err)
			return
		}
	}
	if err := s.accounts.DeleteUser(u.ID); err != nil {
		serverError(w, err)
		return
	}
	slog.Info("account deleted", "user", u.ID, "apps", len(apps))
	s.setCookie(w, sessionCookie, "", -time.Second)
	render(w, http.StatusOK, "notice", map[string]any{"Title": "Account deleted",
		"Message": fmt.Sprintf("Your account and %d app(s) were deleted. Your Stripe accounts are yours and were not changed.", len(apps)), "Link": "/"})
}

// cancelSubscriptions ends every subscription of the account that Stripe
// would still bill, including one already set to end at the period's end.
func (s *Site) cancelSubscriptions(ctx context.Context, userID string) error {
	b, err := s.accounts.Billing(userID)
	if err != nil || b.Customer == "" {
		return err
	}
	if !s.Billing.ready() {
		if b.Plan != accounts.PlanFree && b.Status != accounts.StatusGranted {
			return errors.New("billing is not configured on this server")
		}
		return nil
	}
	subs, err := s.Billing.Stripe.Subscriptions(ctx, b.Customer)
	if err != nil {
		return err
	}
	for _, sub := range subs {
		switch sub.Status {
		case "canceled", "incomplete_expired":
			continue
		}
		if err := s.Billing.Stripe.CancelSubscription(ctx, sub.ID); err != nil {
			return err
		}
		slog.Info("subscription cancelled for account deletion", "user", userID, "subscription", sub.ID)
	}
	return nil
}
