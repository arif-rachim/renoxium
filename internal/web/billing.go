package web

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/arif-rachim/renoxium/internal/accounts"
	"github.com/arif-rachim/renoxium/internal/platform"
	"github.com/arif-rachim/renoxium/internal/stripe"
)

// Billing configures Renoxium's own subscriptions, charged on the platform's
// Stripe account.
type Billing struct {
	Stripe        *stripe.Client
	PricePro      string // Stripe price IDs of the monthly plans
	PriceBusiness string
	WebhookSecret string // signing secret of the webhook for the platform account's own events
}

func (b Billing) ready() bool { return b.Stripe != nil && b.PricePro != "" && b.PriceBusiness != "" }

func (b Billing) price(plan string) string {
	switch plan {
	case accounts.PlanPro:
		return b.PricePro
	case accounts.PlanBusiness:
		return b.PriceBusiness
	}
	return ""
}

func (b Billing) planOfPrice(price string) string {
	switch price {
	case b.PricePro:
		return accounts.PlanPro
	case b.PriceBusiness:
		return accounts.PlanBusiness
	}
	return ""
}

func (s *Site) registerBilling() {
	s.mux.HandleFunc("GET /billing", s.billingPage)
	s.mux.HandleFunc("POST /billing/checkout", s.billingCheckout)
	s.mux.HandleFunc("POST /billing/portal", s.billingPortal)
	s.mux.HandleFunc("POST /stripe/billing-webhook", s.billingWebhook)
}

func (s *Site) billingPage(w http.ResponseWriter, r *http.Request) {
	u, ok := s.currentUser(r)
	if !ok {
		http.Redirect(w, r, "/login?next=%2Fbilling", http.StatusSeeOther)
		return
	}
	if r.URL.Query().Get("done") == "1" && s.Billing.ready() && s.apiLimiter.allow("stripe:"+u.ID) {
		// Back from Stripe: read the subscription now rather than wait for the webhook.
		if err := s.syncBilling(r.Context(), u.ID); err != nil {
			slog.Error("billing sync", "user", u.ID, "err", err)
		}
	}
	b, err := s.accounts.Billing(u.ID)
	if err != nil {
		serverError(w, err)
		return
	}
	apps, _ := s.platform.Apps(u.ID)
	waUsed, waQuota := s.platform.WhatsAppUsage(u.ID)
	stUsed, stQuota := s.platform.StorageUsage(u.ID)
	fetchUsed, fetchQuota := s.platform.FetchUsage(u.ID)
	render(w, http.StatusOK, "billing", map[string]any{"User": u, "Billing": b, "Ready": s.Billing.ready(), "CSRF": s.csrfToken(w, r), "Prices": accounts.PlanPriceAED,
		"Apps": len(apps), "Limit": platform.AppsPerPlan[b.Plan], "Fee": platform.FreePlanFeePercent,
		"Paying": b.Plan != accounts.PlanFree, "PeriodEnd": formatDate(b.PeriodEnd),
		"WAUsed": waUsed, "WAQuota": waQuota, "StorageMB": stUsed >> 20, "StorageQuotaMB": stQuota >> 20,
		"FetchUsed": fetchUsed, "FetchQuota": fetchQuota})
}

func formatDate(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format("2 Jan 2006")
}

// customer returns the account's Stripe customer, creating it once.
func (s *Site) customer(ctx context.Context, u accounts.User) (string, error) {
	b, err := s.accounts.Billing(u.ID)
	if err != nil {
		return "", err
	}
	if b.Customer != "" {
		return b.Customer, nil
	}
	id, err := s.Billing.Stripe.CreateCustomer(ctx, u.Email, map[string]string{"rx_user": u.ID})
	if err != nil {
		return "", err
	}
	return id, s.accounts.SetCustomer(u.ID, id)
}

func (s *Site) billingCheckout(w http.ResponseWriter, r *http.Request) {
	u, ok := s.currentUser(r)
	if !ok || !s.checkCSRF(r) {
		http.Error(w, "the form expired; go back and try again", http.StatusForbidden)
		return
	}
	if !s.apiLimiter.allow("stripe:" + u.ID) {
		http.Error(w, "too many attempts; try again in a few minutes", http.StatusTooManyRequests)
		return
	}
	plan := r.PostFormValue("plan")
	price := s.Billing.price(plan)
	if !s.Billing.ready() || price == "" {
		http.Error(w, "this plan is not available", http.StatusBadRequest)
		return
	}
	b, _ := s.accounts.Billing(u.ID)
	if b.Plan != accounts.PlanFree {
		// Already subscribed: changing plans happens in the billing portal,
		// so nobody pays for two subscriptions.
		s.billingPortal(w, r)
		return
	}
	cus, err := s.customer(r.Context(), u)
	if err != nil {
		slog.Error("billing customer", "user", u.ID, "err", err)
		render(w, http.StatusBadGateway, "error", map[string]any{"Message": "Stripe is not reachable right now. Try again in a minute."})
		return
	}
	url, err := s.Billing.Stripe.SubscriptionCheckout(r.Context(), cus, price, s.base+"/billing?done=1", s.base+"/billing",
		map[string]string{"rx_user": u.ID, "rx_plan": plan})
	if err != nil {
		slog.Error("billing checkout", "user", u.ID, "err", err)
		render(w, http.StatusBadGateway, "error", map[string]any{"Message": "Stripe could not start the payment: " + err.Error()})
		return
	}
	http.Redirect(w, r, url, http.StatusSeeOther)
}

func (s *Site) billingPortal(w http.ResponseWriter, r *http.Request) {
	u, ok := s.currentUser(r)
	if !ok || !s.checkCSRF(r) {
		http.Error(w, "the form expired; go back and try again", http.StatusForbidden)
		return
	}
	b, _ := s.accounts.Billing(u.ID)
	if !s.Billing.ready() || b.Customer == "" {
		http.Redirect(w, r, "/billing", http.StatusSeeOther)
		return
	}
	if !s.apiLimiter.allow("stripe-portal:" + u.ID) {
		http.Error(w, "too many attempts; try again in a few minutes", http.StatusTooManyRequests)
		return
	}
	url, err := s.Billing.Stripe.BillingPortal(r.Context(), b.Customer, s.base+"/billing?done=1")
	if err != nil {
		slog.Error("billing portal", "user", u.ID, "err", err)
		render(w, http.StatusBadGateway, "error", map[string]any{"Message": "Stripe could not open billing: " + err.Error()})
		return
	}
	http.Redirect(w, r, url, http.StatusSeeOther)
}

// paying statuses keep the paid plan; past_due gives Stripe's retries time.
var paying = map[string]bool{"active": true, "trialing": true, "past_due": true}

// syncBilling reads the account's subscriptions from Stripe and records the
// best paid one, or the free plan.
func (s *Site) syncBilling(ctx context.Context, userID string) error {
	b, err := s.accounts.Billing(userID)
	if err != nil || b.Customer == "" {
		return err
	}
	subs, err := s.Billing.Stripe.Subscriptions(ctx, b.Customer)
	if err != nil {
		return err
	}
	best := accounts.Billing{Plan: accounts.PlanFree}
	rank := map[string]int{accounts.PlanFree: 0, accounts.PlanPro: 1, accounts.PlanBusiness: 2}
	for _, sub := range subs {
		plan := s.Billing.planOfPrice(sub.PriceID())
		if plan == "" || !paying[sub.Status] || rank[plan] <= rank[best.Plan] {
			if best.Status == "" && plan != "" {
				best.Status, best.Subscription = sub.Status, sub.ID // remember the latest non-paying one for display
			}
			continue
		}
		best = accounts.Billing{Plan: plan, Status: sub.Status, Subscription: sub.ID, PeriodEnd: sub.PeriodEnd(), CancelAtPeriodEnd: sub.CancelAtPeriodEnd}
	}
	if best.Plan == accounts.PlanFree && b.Status == accounts.StatusGranted {
		return nil // an operator's grant stays until a paid plan replaces it
	}
	if err := s.accounts.SetSubscription(userID, best); err != nil {
		return err
	}
	platform.ForgetPlan(userID)
	return nil
}

// billingWebhook receives the platform account's own subscription events.
func (s *Site) billingWebhook(w http.ResponseWriter, r *http.Request) {
	if s.Billing.WebhookSecret == "" || !s.Billing.ready() {
		http.Error(w, "billing webhooks are not configured", http.StatusServiceUnavailable)
		return
	}
	payload, err := readLimited(w, r, 1<<20)
	if err != nil {
		return
	}
	e, err := stripe.VerifyWebhook(payload, r.Header.Get("Stripe-Signature"), s.Billing.WebhookSecret, time.Now())
	if err != nil {
		http.Error(w, "bad signature", http.StatusBadRequest)
		return
	}
	if e.Account != "" {
		w.WriteHeader(http.StatusNoContent) // a connected account's event: not ours
		return
	}
	var obj struct {
		Customer string `json:"customer"`
	}
	switch e.Type {
	case "customer.subscription.created", "customer.subscription.updated", "customer.subscription.deleted",
		"customer.subscription.paused", "customer.subscription.resumed", "checkout.session.completed":
		decodeJSON(e.Data.Object, &obj)
	default:
		w.WriteHeader(http.StatusNoContent)
		return
	}
	user, err := s.accounts.UserByCustomer(obj.Customer)
	if errors.Is(err, accounts.ErrNotFound) || obj.Customer == "" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := s.syncBilling(r.Context(), user); err != nil {
		slog.Error("billing sync", "user", user, "event", e.ID, "err", err)
		http.Error(w, "try again", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
