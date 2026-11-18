package web

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/arif-rachim/renoxium/internal/accounts"
	"github.com/arif-rachim/renoxium/internal/platform"
	"github.com/arif-rachim/renoxium/internal/stripe"
)

// readLimited reads a request body of at most max bytes, answering 413 when
// it is larger.
func readLimited(w http.ResponseWriter, r *http.Request, max int64) ([]byte, error) {
	payload, err := io.ReadAll(http.MaxBytesReader(w, r.Body, max))
	if err != nil {
		http.Error(w, "too large", http.StatusRequestEntityTooLarge)
	}
	return payload, err
}

func decodeJSON(raw []byte, out any) error { return json.Unmarshal(raw, out) }

func (s *Site) registerPayments() {
	s.mux.HandleFunc("GET /apps/{app}/payments", s.adminPayments)
	s.mux.HandleFunc("POST /apps/{app}/payments/connect", s.connectStripe)
	// Stripe sends the owner here when an onboarding link expired.
	s.mux.HandleFunc("GET /apps/{app}/payments/connect", s.connectStripe)
	s.mux.HandleFunc("POST /stripe/webhook", s.stripeWebhook)
}

func (s *Site) adminPayments(w http.ResponseWriter, r *http.Request) {
	u, app, ok := s.owner(w, r)
	if !ok {
		return
	}
	fee := 0
	if b, _ := s.accounts.Billing(u.ID); b.Plan == accounts.PlanFree {
		fee = platform.FreePlanFeePercent
	}
	pa, err := s.platform.PaymentAccount(app)
	if err == nil && pa.StripeAccount != "" && r.URL.Query().Get("returned") == "1" {
		// Back from Stripe: read the account's state now rather than wait for the webhook.
		pa, err = s.platform.RefreshPaymentAccount(r.Context(), app)
	}
	if err != nil {
		slog.Error("payment account", "app", app, "err", err)
	}
	list, _ := s.platform.Payments(r.Context(), app, 50)
	render(w, http.StatusOK, "admin_payments", map[string]any{"App": app, "Account": pa, "Payments": list, "CSRF": s.csrfToken(w, r), "Fee": fee})
}

// connectStripe sends the owner to Stripe's onboarding, creating the
// account on first use. GET is only Stripe's refresh link for an existing
// account; creating one needs the POST with its CSRF token.
func (s *Site) connectStripe(w http.ResponseWriter, r *http.Request) {
	u, app, ok := s.owner(w, r)
	if !ok {
		return
	}
	if r.Method == http.MethodPost && !s.checkCSRF(r) {
		http.Error(w, "the form expired; go back and try again", http.StatusForbidden)
		return
	}
	if r.Method == http.MethodGet {
		if pa, err := s.platform.PaymentAccount(app); err != nil || pa.StripeAccount == "" {
			http.Redirect(w, r, "/apps/"+app+"/payments", http.StatusSeeOther)
			return
		}
	}
	page := s.base + "/apps/" + app + "/payments"
	link, err := s.platform.StripeOnboardingURL(r.Context(), app, u.Email, page+"?returned=1", page+"/connect")
	if err != nil {
		slog.Error("stripe onboarding", "app", app, "err", err)
		render(w, http.StatusBadGateway, "error", map[string]any{"Message": "Stripe could not start the setup: " + err.Error()})
		return
	}
	http.Redirect(w, r, link, http.StatusSeeOther)
}

// stripeWebhook receives events from connected accounts. Only events with
// a valid signature are applied.
func (s *Site) stripeWebhook(w http.ResponseWriter, r *http.Request) {
	if s.StripeWebhookSecret == "" {
		http.Error(w, "webhooks are not configured", http.StatusServiceUnavailable)
		return
	}
	payload, err := readLimited(w, r, 1<<20)
	if err != nil {
		return
	}
	e, err := stripe.VerifyWebhook(payload, r.Header.Get("Stripe-Signature"), s.StripeWebhookSecret, time.Now())
	if err != nil {
		http.Error(w, "bad signature", http.StatusBadRequest)
		return
	}
	if err := s.platform.HandleStripeEvent(r.Context(), e); err != nil {
		slog.Error("stripe event", "id", e.ID, "type", e.Type, "err", err)
		http.Error(w, "try again", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
