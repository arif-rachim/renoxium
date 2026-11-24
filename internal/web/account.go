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
	"github.com/arif-rachim/renoxium/internal/whatsapp"
)

func (s *Site) registerAccount() {
	s.mux.HandleFunc("GET /account", s.accountPage)
	s.mux.HandleFunc("POST /account/password", s.changePassword)
	s.mux.HandleFunc("GET /account/export", s.exportAccount)
	s.mux.HandleFunc("POST /account/delete", s.deleteAccount)
	s.mux.HandleFunc("POST /account/assistants/disconnect", s.disconnectAssistant)
}

// disconnectAssistant ends one connected agent's access to the account.
func (s *Site) disconnectAssistant(w http.ResponseWriter, r *http.Request) {
	u, ok := s.currentUser(r)
	if !ok || !s.checkCSRF(r) {
		http.Error(w, "the form expired; go back and try again", http.StatusForbidden)
		return
	}
	client := r.PostFormValue("client")
	name := ""
	for _, c := range s.connections(u.ID) {
		if c.ClientID == client {
			name = c.Name
		}
	}
	if name == "" {
		http.Redirect(w, r, "/account#assistants", http.StatusSeeOther)
		return
	}
	if _, err := s.accounts.Disconnect(u.ID, client); err != nil {
		serverError(w, err)
		return
	}
	slog.Info("agent disconnected", "user", u.ID, "client", client)
	s.renderAccount(w, r, u, http.StatusOK, map[string]any{"Disconnected": name})
}

func (s *Site) connections(userID string) []accounts.Connection {
	list, err := s.accounts.Connections(userID)
	if err != nil {
		slog.Error("connections", "user", userID, "err", err)
	}
	return list
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
	workApps := 0
	for _, a := range apps {
		if pl, err := s.platform.AppPlan(a.App); err == nil && pl.WorkPaid {
			workApps++
		}
	}
	data := map[string]any{"User": u, "CSRF": csrf, "Apps": apps, "Billing": b, "PlanName": planName(b.Plan), "Connections": s.connections(u.ID), "WorkApps": workApps,
		"Shell": s.ownerShell(u, "Account", "/account", csrf), "Reauth": s.reauthView(r, u, "dpassword"), "WhatsAppOn": s.WhatsApp != nil,
		"PhoneLinked": r.URL.Query().Get("phone") == "1", "EmailAdded": r.URL.Query().Get("email") == "1"}
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
		enc.Encode(map[string]any{"email": u.Email, "email_confirmed": u.Verified, "whatsapp": u.Phone, "plan": b.Plan, "exported_at": time.Now().UTC(), "apps": len(apps)})
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
	if wantsReauthCode(r, u) {
		if err := s.sendReauthCode(r, u); err != nil {
			s.renderAccount(w, r, u, http.StatusTooManyRequests, map[string]any{"DeleteError": capitalize(err.Error()) + "."})
			return
		}
		s.renderAccount(w, r, u, http.StatusOK, nil)
		return
	}
	typed := strings.TrimSpace(r.PostFormValue("confirm"))
	if u.Email == "" {
		typed, _ = whatsapp.Normalize(typed)
	}
	if !strings.EqualFold(typed, u.Label()) {
		what := "email address"
		if u.Email == "" {
			what = "WhatsApp number"
		}
		s.renderAccount(w, r, u, http.StatusBadRequest, map[string]any{"DeleteError": "Type your " + what + " exactly to confirm."})
		return
	}
	if err := s.checkReauth(r, u); err != nil {
		s.renderAccount(w, r, u, http.StatusUnauthorized, map[string]any{"DeleteError": err.Error()})
		return
	}
	if err := s.cancelSubscriptions(r.Context(), u.ID); err != nil {
		slog.Error("cancel subscription before delete", "user", u.ID, "err", err)
		s.renderAccount(w, r, u, http.StatusServiceUnavailable, map[string]any{"DeleteError": "Your work app subscriptions could not be cancelled at Stripe, so nothing was deleted. Try again in a minute."})
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
	if s.Billing.Stripe == nil {
		return nil // nothing could have been subscribed on this server
	}
	subs, err := s.Billing.Stripe.Subscriptions(ctx, b.Customer) // its work apps, and any left from the former plans
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
