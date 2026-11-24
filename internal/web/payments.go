package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strings"
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
	// Refund, capture or release one live payment; cancel a subscription.
	s.mux.HandleFunc("POST /apps/{app}/payments/{id}/{action}", s.paymentAction)
	s.mux.HandleFunc("POST /apps/{app}/subscriptions/{id}/cancel", s.cancelSubscription)
	s.mux.HandleFunc("POST /stripe/webhook", s.stripeWebhook)
}

func (s *Site) adminPayments(w http.ResponseWriter, r *http.Request) {
	u, app, ok := s.owner(w, r)
	if !ok {
		return
	}
	s.renderPayments(w, r, u, app, http.StatusOK, "", "")
}

func (s *Site) renderPayments(w http.ResponseWriter, r *http.Request, u accounts.User, app string, status int, notice, problem string) {
	fee := 0
	// Shops pay the fee on free and member accounts alike; a paid work app
	// and a granted account's apps do not.
	if b, _ := s.accounts.Billing(u.ID); !b.Granted() {
		if pl, _ := s.platform.AppPlan(app); !pl.WorkPaid {
			fee = platform.ShopFeePercent
		}
	}
	pa, err := s.platform.PaymentAccount(app)
	if err == nil && pa.StripeAccount != "" && r.URL.Query().Get("returned") == "1" && s.apiLimiter.allow("stripe:"+u.ID) {
		// Back from Stripe: read the account's state now rather than wait for the webhook.
		pa, err = s.platform.RefreshPaymentAccount(r.Context(), app)
	}
	if err != nil {
		slog.Error("payment account", "app", app, "err", err)
	}
	filter := r.URL.Query().Get("status")
	if !slices.Contains([]string{"", "paid", "pending", "authorized", "refunded", "released", "failed", "expired"}, filter) {
		filter = ""
	}
	page := pageParam(r)
	list, total, _ := s.platform.PaymentsPage(r.Context(), app, filter, page)
	subs, _, _ := s.platform.SubscriptionsPage(r.Context(), app, 1)
	var filters []shellLink
	for _, f := range []struct{ v, l string }{{"", "All"}, {"paid", "Paid"}, {"pending", "Waiting"}, {"authorized", "Card holds"}, {"refunded", "Refunded"}} {
		filters = append(filters, shellLink{Label: f.l, Href: "?status=" + f.v, Current: f.v == filter})
	}
	render(w, status, "admin_payments", map[string]any{"Shell": s.shell(u, app, "Payments", "/apps/"+app+"/payments", false),
		"App": app, "Account": pa, "Payments": list, "Subscriptions": subs, "Total": total, "Pager": newPager(r, page, platform.AdminPageSize, total),
		"Filters": filters, "Filter": filter, "CSRF": s.csrfToken(w, r), "Fee": fee, "Notice": notice, "Error": problem})
}

// paymentAction refunds a paid payment (all, or the amount typed), or
// captures (all or part) or releases a card hold. Owner only: staff cannot
// move money. Every action is written to the app log with the owner's email.
func (s *Site) paymentAction(w http.ResponseWriter, r *http.Request) {
	u, app, ok := s.owner(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
	if !s.checkCSRF(r) {
		http.Error(w, "the form expired; go back and try again", http.StatusForbidden)
		return
	}
	id, amount := r.PathValue("id"), r.PostFormValue("amount")
	var pm platform.Payment
	var err error
	notice := ""
	switch r.PathValue("action") {
	case "refund":
		pm, err = s.platform.AdminRefund(r.Context(), app, id, amount, u.Label())
		notice = fmt.Sprintf("Refunded. %.2f %s of the payment for %s is refunded in all.", pm.Refunded, strings.ToUpper(pm.Currency), pm.Reference)
	case "capture":
		pm, err = s.platform.AdminCapture(r.Context(), app, id, amount, u.Label())
		notice = fmt.Sprintf("Captured %.2f %s for %s; the rest of the hold is released.", pm.Amount, strings.ToUpper(pm.Currency), pm.Reference)
	case "release":
		pm, err = s.platform.AdminRelease(r.Context(), app, id, u.Label())
		notice = "The card hold for " + pm.Reference + " is released."
	default:
		http.NotFound(w, r)
		return
	}
	if err != nil {
		slog.Info("payment action refused", "app", app, "action", r.PathValue("action"), "err", err)
		s.renderPayments(w, r, u, app, http.StatusBadRequest, "", moneyError(err))
		return
	}
	s.renderPayments(w, r, u, app, http.StatusOK, notice, "")
}

// cancelSubscription cancels a live subscription now or at the end of the
// paid period. Owner only.
func (s *Site) cancelSubscription(w http.ResponseWriter, r *http.Request) {
	u, app, ok := s.owner(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
	if !s.checkCSRF(r) {
		http.Error(w, "the form expired; go back and try again", http.StatusForbidden)
		return
	}
	atEnd := r.PostFormValue("when") != "now"
	sub, err := s.platform.AdminCancelSubscription(r.Context(), app, r.PathValue("id"), atEnd, u.Label())
	if err != nil {
		s.renderPayments(w, r, u, app, http.StatusBadRequest, "", moneyError(err))
		return
	}
	notice := "The subscription for " + sub.Reference + " is cancelled."
	if atEnd && sub.Status != "canceled" {
		notice = "The subscription for " + sub.Reference + " ends on " + sub.CurrentPeriodEnd + " UTC; no further payments are taken."
	}
	s.renderPayments(w, r, u, app, http.StatusOK, notice, "")
}

func moneyError(err error) string {
	if errors.Is(err, platform.ErrNotFound) {
		return "That payment is not in this app."
	}
	return "Not done: " + err.Error()
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
	if !s.apiLimiter.allow("stripe:" + u.ID) {
		http.Error(w, "too many attempts; try again in a few minutes", http.StatusTooManyRequests)
		return
	}
	page := s.base + "/apps/" + app + "/payments"
	link, err := s.platform.StripeOnboardingURL(r.Context(), app, u.Email, page+"?returned=1", page+"/connect")
	if err != nil {
		slog.Error("stripe onboarding", "app", app, "err", err)
		render(w, http.StatusServiceUnavailable, "error", map[string]any{"Message": "Stripe could not start the setup: " + err.Error()})
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
