package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/arif-rachim/renoxium/internal/accounts"
	"github.com/arif-rachim/renoxium/internal/platform"
	"github.com/arif-rachim/renoxium/internal/stripe"
)

// Billing configures what Renoxium charges on the platform's own Stripe
// account: the card an owner saves (it makes the account a member and is
// never charged for that), and work app subscriptions.
type Billing struct {
	Stripe        *stripe.Client
	WebhookSecret string // signing secret of the webhook for the platform account's own events
	// PriceWorkApp is the Stripe price ID of a work app's monthly subscription
	// (platform.WorkAppPriceAED).
	PriceWorkApp string
	// PriceWorkSeat is the Stripe price ID of each staff member of a work app
	// beyond the included ones (platform.WorkSeatPriceAED a month).
	PriceWorkSeat string
}

// ready reports whether owners can save a card and manage billing.
func (b Billing) ready() bool { return b.Stripe != nil }

// workReady reports whether work app subscriptions can be sold.
func (b Billing) workReady() bool { return b.Stripe != nil && b.PriceWorkApp != "" }

func (s *Site) registerBilling() {
	s.mux.HandleFunc("GET /billing", s.billingPage)
	s.mux.HandleFunc("POST /billing/card", s.billingCard)
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
		// Back from Stripe: read the card and subscriptions now rather than
		// wait for the webhook.
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
	csrf := s.csrfToken(w, r)
	granted := b.Granted()
	limit := platform.AppsPerPlan[s.accounts.OwnerPlan(u.ID)]
	meters := []meter{
		newMeter("Apps", int64(len(apps)), int64(limit), ""),
		newMeter("WhatsApp messages this month (sign-in codes and notices)", int64(waUsed), int64(waQuota), ""),
		newMeter("Image storage", stUsed>>20, stQuota>>20, " MB"),
		newMeter("External API calls this month", int64(fetchUsed), int64(fetchQuota), ""),
	}
	now := time.Now()
	render(w, http.StatusOK, "billing", map[string]any{"User": u, "Billing": b, "Ready": s.Billing.ready(), "CSRF": csrf,
		"Fee": platform.ShopFeePercent, "PlanName": planName(b.Plan), "Member": b.Plan == accounts.PlanMember, "Granted": granted,
		"HasCard": b.HasCard(now), "Card": cardText(b), "CardExpired": !b.CardExpires.IsZero() && !b.HasCard(now),
		"FreeApps": platform.AppsPerPlan[accounts.PlanFree], "MaxApps": platform.MaxAppsPerOwner, "AppCount": len(apps),
		"CanManage": s.Billing.ready() && b.Customer != "" && !granted,
		"Meters":    meters, "Plans": planCards(), "AppPlans": appPlanRows(s.platform, apps), "WorkPrice": platform.WorkAppPriceAED,
		"Notice": billingNotices[r.URL.Query().Get("notice")], "Shell": s.ownerShell(u, "Plan and billing", "/billing", csrf)})
}

// cardText describes the saved card, e.g. "Visa ending 4242, expires 12/2030".
func cardText(b accounts.Billing) string {
	if b.CardExpires.IsZero() {
		return ""
	}
	brand := b.CardBrand
	if brand == "" {
		brand = "card"
	}
	last := b.CardExpires.AddDate(0, 0, -1)
	return fmt.Sprintf("%s ending %s, expires %02d/%d", strings.ToUpper(brand[:1])+brand[1:], b.CardLast4, int(last.Month()), last.Year())
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

// billingCard sends the owner to Stripe Checkout (mode setup) to save a
// card. Saving it charges nothing and makes the account a member.
func (s *Site) billingCard(w http.ResponseWriter, r *http.Request) {
	u, ok := s.currentUser(r)
	if !ok || !s.checkCSRF(r) {
		http.Error(w, "the form expired; go back and try again", http.StatusForbidden)
		return
	}
	if !s.Billing.ready() {
		http.Redirect(w, r, "/billing?notice=unavailable", http.StatusSeeOther)
		return
	}
	if !s.apiLimiter.allow("stripe:" + u.ID) {
		http.Error(w, "too many attempts; try again in a few minutes", http.StatusTooManyRequests)
		return
	}
	cus, err := s.customer(r.Context(), u)
	if err != nil {
		slog.Error("billing customer", "user", u.ID, "err", err)
		render(w, http.StatusServiceUnavailable, "error", map[string]any{"Message": "Stripe is not reachable right now. Try again in a minute."})
		return
	}
	url, err := s.Billing.Stripe.SetupCheckout(r.Context(), cus, s.base+"/billing?done=1", s.base+"/billing",
		map[string]string{"rx_user": u.ID, "rx_purpose": "card_on_file"})
	if err != nil {
		slog.Error("billing card checkout", "user", u.ID, "err", err)
		render(w, http.StatusServiceUnavailable, "error", map[string]any{"Message": "Stripe could not open the card form: " + err.Error()})
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
	switch {
	case !s.Billing.ready():
		http.Redirect(w, r, "/billing?notice=unavailable", http.StatusSeeOther)
		return
	case b.Granted():
		// A membership given by the operator has nothing at Stripe.
		http.Redirect(w, r, "/billing?notice=granted", http.StatusSeeOther)
		return
	case b.Customer == "":
		http.Redirect(w, r, "/billing?notice=no-customer", http.StatusSeeOther)
		return
	}
	if !s.apiLimiter.allow("stripe-portal:" + u.ID) {
		http.Error(w, "too many attempts; try again in a few minutes", http.StatusTooManyRequests)
		return
	}
	url, err := s.Billing.Stripe.BillingPortal(r.Context(), b.Customer, s.base+"/billing?done=1")
	if err != nil {
		slog.Error("billing portal", "user", u.ID, "err", err)
		render(w, http.StatusServiceUnavailable, "error", map[string]any{"Message": "Stripe could not open billing: " + err.Error()})
		return
	}
	http.Redirect(w, r, url, http.StatusSeeOther)
}

// syncBilling reads the account's work app subscriptions and saved cards
// from Stripe and records them: an unexpired card makes the account a
// member, and without one it is free (a grant stays either way).
func (s *Site) syncBilling(ctx context.Context, userID string) error {
	b, err := s.accounts.Billing(userID)
	if err != nil || b.Customer == "" {
		return err
	}
	var errs []error
	if s.Billing.workReady() {
		if subs, err := s.Billing.Stripe.Subscriptions(ctx, b.Customer); err != nil {
			errs = append(errs, err)
		} else {
			s.syncWorkApps(userID, subs)
		}
	}
	if pms, err := s.Billing.Stripe.CardPaymentMethods(ctx, b.Customer); err != nil {
		errs = append(errs, err)
	} else if err := s.accounts.SetCard(userID, bestCard(pms)); err != nil {
		errs = append(errs, err)
	}
	platform.ForgetPlan(userID)
	return errors.Join(errs...)
}

// bestCard is the saved card that works longest; a zero Card when there is
// none. An expired card is kept only when there is no other, so the billing
// page can say it expired; it does not make the account a member.
func bestCard(pms []stripe.PaymentMethod) accounts.Card {
	var best stripe.PaymentMethod
	for _, pm := range pms {
		if (pm.Type == "" || pm.Type == "card") && pm.Expires().After(best.Expires()) {
			best = pm
		}
	}
	if best.Expires().IsZero() {
		return accounts.Card{}
	}
	return accounts.Card{Brand: best.Card.Brand, Last4: best.Card.Last4, ExpMonth: best.Card.ExpMonth, ExpYear: best.Card.ExpYear}
}

// billingEvents are the platform account's own events that change what
// syncBilling reads: work app subscriptions, and cards saved, changed or
// removed.
var billingEvents = map[string]bool{
	"customer.subscription.created": true, "customer.subscription.updated": true, "customer.subscription.deleted": true,
	"customer.subscription.paused": true, "customer.subscription.resumed": true,
	"checkout.session.completed": true, "setup_intent.succeeded": true,
	"payment_method.attached": true, "payment_method.detached": true, "payment_method.updated": true,
	"payment_method.automatically_updated": true, "customer.updated": true,
}

// eventCustomer is the Stripe customer an event is about.
func eventCustomer(e stripe.Event) string {
	var obj struct {
		ID       string          `json:"id"`
		Object   string          `json:"object"`
		Customer json.RawMessage `json:"customer"`
	}
	decodeJSON(e.Data.Object, &obj)
	if obj.Object == "customer" || e.Type == "customer.updated" {
		return obj.ID
	}
	if id := customerID(obj.Customer); id != "" {
		return id
	}
	// A detached payment method no longer names its customer; the event's
	// previous attributes do.
	var prev struct {
		Customer json.RawMessage `json:"customer"`
	}
	decodeJSON(e.Data.PreviousAttributes, &prev)
	return customerID(prev.Customer)
}

// customerID reads a customer field: an ID, an expanded object or null.
func customerID(raw json.RawMessage) string {
	var id string
	if json.Unmarshal(raw, &id) == nil {
		return id
	}
	var obj struct {
		ID string `json:"id"`
	}
	json.Unmarshal(raw, &obj)
	return obj.ID
}

// billingWebhook receives the platform account's own billing events.
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
	if e.Account != "" || !billingEvents[e.Type] {
		w.WriteHeader(http.StatusNoContent) // a connected account's event, or one that changes nothing here
		return
	}
	customer := eventCustomer(e)
	if customer == "" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	user, err := s.accounts.UserByCustomer(customer)
	if errors.Is(err, accounts.ErrNotFound) {
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
