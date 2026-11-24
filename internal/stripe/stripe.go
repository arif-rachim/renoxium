// Package stripe is a small client for the parts of the Stripe API that
// Renoxium uses: connected accounts for merchants, onboarding links,
// Checkout Sessions created directly on a merchant's account, and webhook
// signature checks.
package stripe

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Client calls the Stripe API with the platform's secret key.
type Client struct {
	Key     string
	BaseURL string // default https://api.stripe.com
	HTTP    *http.Client
}

// Error is an error answer from Stripe.
type Error struct {
	Status  int
	Type    string
	Code    string
	Message string
	Param   string
}

func (e *Error) Error() string {
	s := fmt.Sprintf("stripe error (HTTP %d", e.Status)
	if e.Code != "" {
		s += ", " + e.Code
	}
	s += "): " + e.Message
	if e.Param != "" {
		s += " (parameter " + e.Param + ")"
	}
	return s
}

// do sends a form-encoded request. account, when set, makes the call on
// behalf of that connected account (Stripe-Account header).
func (c *Client) do(ctx context.Context, method, path, account string, form url.Values, out any) error {
	base := c.BaseURL
	if base == "" {
		base = "https://api.stripe.com"
	}
	var body io.Reader
	target := base + path
	if method == http.MethodGet {
		if len(form) > 0 {
			target += "?" + form.Encode()
		}
	} else {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return err
	}
	req.SetBasicAuth(c.Key, "")
	if method != http.MethodGet {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		var key [16]byte
		rand.Read(key[:])
		req.Header.Set("Idempotency-Key", hex.EncodeToString(key[:]))
	}
	if account != "" && account != DirectAccount {
		req.Header.Set("Stripe-Account", account)
	}
	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("stripe: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("stripe: %w", err)
	}
	if resp.StatusCode/100 != 2 {
		var e struct {
			Error struct {
				Type, Code, Message, Param string
			} `json:"error"`
		}
		if json.Unmarshal(raw, &e) != nil || e.Error.Message == "" {
			return &Error{Status: resp.StatusCode, Message: strings.TrimSpace(string(raw))}
		}
		return &Error{Status: resp.StatusCode, Type: e.Error.Type, Code: e.Error.Code, Message: e.Error.Message, Param: e.Error.Param}
	}
	return json.Unmarshal(raw, out)
}

// Account is the part of a connected account Renoxium needs.
type Account struct {
	ID               string `json:"id"`
	ChargesEnabled   bool   `json:"charges_enabled"`
	PayoutsEnabled   bool   `json:"payouts_enabled"`
	DetailsSubmitted bool   `json:"details_submitted"`
	Email            string `json:"email"`
	Country          string `json:"country"`
	DefaultCurrency  string `json:"default_currency"`
}

// APIVersionV2 is the Stripe-Version sent with Accounts v2 calls.
const APIVersionV2 = "2026-10-28.endive"

// CreateAccount creates a merchant account that behaves like a Standard
// account, through Accounts v2 (Stripe refuses POST /v1/accounts for new
// Connect integrations): the merchant gets the full Stripe Dashboard, pays
// Stripe's fees, Stripe covers negative balances and collects requirements.
// The account works with the v1 calls used afterwards (account links,
// GET /v1/accounts/{id}, Checkout with Stripe-Account).
func (c *Client) CreateAccount(ctx context.Context, email, country string) (Account, error) {
	body := map[string]any{
		"dashboard": "full",
		"configuration": map[string]any{
			"merchant": map[string]any{"capabilities": map[string]any{"card_payments": map[string]any{"requested": true}}},
		},
		"defaults": map[string]any{
			"currency":         "aed",
			"responsibilities": map[string]any{"fees_collector": "stripe", "losses_collector": "stripe"},
		},
	}
	if email != "" {
		body["contact_email"] = email
	}
	if country != "" {
		body["identity"] = map[string]any{"country": country}
	}
	var out struct {
		ID           string `json:"id"`
		ContactEmail string `json:"contact_email"`
	}
	if err := c.doJSON(ctx, http.MethodPost, "/v2/core/accounts", body, &out); err != nil {
		return Account{}, err
	}
	return Account{ID: out.ID, Email: out.ContactEmail, Country: country}, nil
}

// doJSON sends a JSON request to a v2 endpoint (Bearer key, Stripe-Version).
func (c *Client) doJSON(ctx context.Context, method, path string, in, out any) error {
	base := c.BaseURL
	if base == "" {
		base = "https://api.stripe.com"
	}
	raw, err := json.Marshal(in)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, strings.NewReader(string(raw)))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Key)
	req.Header.Set("Stripe-Version", APIVersionV2)
	req.Header.Set("Content-Type", "application/json")
	var key [16]byte
	rand.Read(key[:])
	req.Header.Set("Idempotency-Key", hex.EncodeToString(key[:]))
	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("stripe: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("stripe: %w", err)
	}
	if resp.StatusCode/100 != 2 {
		var e struct {
			Error struct {
				Type, Code, Message, Param string
			} `json:"error"`
		}
		if json.Unmarshal(body, &e) != nil || e.Error.Message == "" {
			return &Error{Status: resp.StatusCode, Message: strings.TrimSpace(string(body))}
		}
		return &Error{Status: resp.StatusCode, Type: e.Error.Type, Code: e.Error.Code, Message: e.Error.Message, Param: e.Error.Param}
	}
	return json.Unmarshal(body, out)
}

// GetAccount reads a connected account.
func (c *Client) GetAccount(ctx context.Context, id string) (Account, error) {
	var a Account
	return a, c.do(ctx, http.MethodGet, "/v1/accounts/"+url.PathEscape(id), "", nil, &a)
}

// OnboardingLink returns a single-use URL where the merchant completes
// Stripe's onboarding.
func (c *Client) OnboardingLink(ctx context.Context, account, refreshURL, returnURL string) (string, error) {
	var out struct {
		URL string `json:"url"`
	}
	err := c.do(ctx, http.MethodPost, "/v1/account_links", "", url.Values{
		"account": {account}, "refresh_url": {refreshURL}, "return_url": {returnURL}, "type": {"account_onboarding"},
	}, &out)
	return out.URL, err
}

// LineItem is one product line; Amount is in the currency's smallest unit
// (fils for AED).
type LineItem struct {
	Name     string
	Amount   int64
	Quantity int64
}

// CheckoutParams describes a Checkout Session.
type CheckoutParams struct {
	Currency      string
	Items         []LineItem
	SuccessURL    string
	CancelURL     string
	CustomerEmail string
	Reference     string            // client_reference_id
	Metadata      map[string]string // copied to the session and its payment
	// ApplicationFee is the platform's cut in the smallest unit, taken by
	// Stripe from this direct charge into the platform account. 0 = none.
	ApplicationFee int64
	// Hold only authorises the card (capture_method manual, cards only);
	// the money is taken later with CapturePaymentIntent.
	Hold bool
}

// Session is the part of a Checkout Session Renoxium needs.
type Session struct {
	ID            string            `json:"id"`
	URL           string            `json:"url"`
	Mode          string            `json:"mode"`
	Subscription  string            `json:"subscription"`
	PaymentStatus string            `json:"payment_status"`
	Status        string            `json:"status"`
	AmountTotal   int64             `json:"amount_total"`
	Currency      string            `json:"currency"`
	Reference     string            `json:"client_reference_id"`
	PaymentIntent string            `json:"payment_intent"`
	Metadata      map[string]string `json:"metadata"`
	Customer      struct {
		Email string `json:"email"`
		Name  string `json:"name"`
	} `json:"customer_details"`
}

// CreateCheckout creates a Checkout Session as a direct charge on the
// merchant's account.
func (c *Client) CreateCheckout(ctx context.Context, account string, p CheckoutParams) (Session, error) {
	form := url.Values{
		"mode":                {"payment"},
		"success_url":         {p.SuccessURL},
		"cancel_url":          {p.CancelURL},
		"client_reference_id": {p.Reference},
	}
	for i, it := range p.Items {
		pre := fmt.Sprintf("line_items[%d]", i)
		form.Set(pre+"[quantity]", strconv.FormatInt(it.Quantity, 10))
		form.Set(pre+"[price_data][currency]", p.Currency)
		form.Set(pre+"[price_data][unit_amount]", strconv.FormatInt(it.Amount, 10))
		form.Set(pre+"[price_data][product_data][name]", it.Name)
	}
	if p.CustomerEmail != "" {
		form.Set("customer_email", p.CustomerEmail)
	}
	for k, v := range p.Metadata {
		form.Set("metadata["+k+"]", v)
		form.Set("payment_intent_data[metadata]["+k+"]", v)
	}
	if p.ApplicationFee > 0 {
		form.Set("payment_intent_data[application_fee_amount]", strconv.FormatInt(p.ApplicationFee, 10))
	}
	if p.Hold {
		form.Set("payment_intent_data[capture_method]", "manual")
		// allowed_payment_method_types, not payment_method_types: Stripe
		// refuses the latter from API version 2026-09-30.endive on, and
		// older versions accept the former too.
		form.Set("allowed_payment_method_types[0]", "card")
	}
	var s Session
	return s, c.do(ctx, http.MethodPost, "/v1/checkout/sessions", account, form, &s)
}

// SubscriptionParams describes a Checkout Session in subscription mode on a
// merchant's account, with inline recurring prices.
type SubscriptionParams struct {
	Currency      string
	Items         []LineItem // Amount is per interval
	Interval      string     // week, month or year
	IntervalCount int64
	SuccessURL    string
	CancelURL     string
	CustomerEmail string
	Reference     string
	Metadata      map[string]string // copied to the session and the subscription
	TrialDays     int64
	// FeePercent is the platform's share of every invoice
	// (subscription_data[application_fee_percent]). 0 = none.
	FeePercent float64
}

// CreateSubscriptionCheckout creates a Checkout Session that starts a
// subscription on the merchant's account (a direct charge per invoice).
func (c *Client) CreateSubscriptionCheckout(ctx context.Context, account string, p SubscriptionParams) (Session, error) {
	form := url.Values{
		"mode":                {"subscription"},
		"success_url":         {p.SuccessURL},
		"cancel_url":          {p.CancelURL},
		"client_reference_id": {p.Reference},
	}
	for i, it := range p.Items {
		pre := fmt.Sprintf("line_items[%d]", i)
		form.Set(pre+"[quantity]", strconv.FormatInt(it.Quantity, 10))
		form.Set(pre+"[price_data][currency]", p.Currency)
		form.Set(pre+"[price_data][unit_amount]", strconv.FormatInt(it.Amount, 10))
		form.Set(pre+"[price_data][product_data][name]", it.Name)
		form.Set(pre+"[price_data][recurring][interval]", p.Interval)
		form.Set(pre+"[price_data][recurring][interval_count]", strconv.FormatInt(max(p.IntervalCount, 1), 10))
	}
	if p.CustomerEmail != "" {
		form.Set("customer_email", p.CustomerEmail)
	}
	for k, v := range p.Metadata {
		form.Set("metadata["+k+"]", v)
		form.Set("subscription_data[metadata]["+k+"]", v)
	}
	if p.TrialDays > 0 {
		form.Set("subscription_data[trial_period_days]", strconv.FormatInt(p.TrialDays, 10))
	}
	if p.FeePercent > 0 {
		form.Set("subscription_data[application_fee_percent]", strconv.FormatFloat(p.FeePercent, 'f', -1, 64))
	}
	var s Session
	return s, c.do(ctx, http.MethodPost, "/v1/checkout/sessions", account, form, &s)
}

// Refund is the part of a refund Renoxium needs.
type Refund struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Amount int64  `json:"amount"`
}

// RefundReasons are the reasons Stripe accepts.
var RefundReasons = map[string]bool{"duplicate": true, "fraudulent": true, "requested_by_customer": true}

// CreateRefund refunds part or all of a payment on a merchant's account.
// amount 0 refunds what is left. refundFee also returns the platform's
// application fee, in proportion to the refunded amount.
func (c *Client) CreateRefund(ctx context.Context, account, paymentIntent string, amount int64, reason string, refundFee bool, metadata map[string]string) (Refund, error) {
	form := url.Values{"payment_intent": {paymentIntent}}
	if amount > 0 {
		form.Set("amount", strconv.FormatInt(amount, 10))
	}
	if RefundReasons[reason] {
		form.Set("reason", reason)
	}
	if refundFee {
		form.Set("refund_application_fee", "true")
	}
	for k, v := range metadata {
		form.Set("metadata["+k+"]", v)
	}
	var r Refund
	return r, c.do(ctx, http.MethodPost, "/v1/refunds", account, form, &r)
}

// PaymentIntent is the part of a PaymentIntent Renoxium needs.
type PaymentIntent struct {
	ID               string            `json:"id"`
	Status           string            `json:"status"` // requires_capture, succeeded, canceled, ...
	Amount           int64             `json:"amount"`
	AmountCapturable int64             `json:"amount_capturable"`
	AmountReceived   int64             `json:"amount_received"`
	Metadata         map[string]string `json:"metadata"`
	// LatestCharge is the charge id, or the charge when expanded.
	LatestCharge json.RawMessage `json:"latest_charge"`
}

// CaptureBefore is when an authorisation expires (the expanded latest
// charge's payment_method_details.card.capture_before), or zero.
func (pi PaymentIntent) CaptureBefore() time.Time {
	var ch struct {
		PaymentMethodDetails struct {
			Card struct {
				CaptureBefore int64 `json:"capture_before"`
			} `json:"card"`
		} `json:"payment_method_details"`
	}
	if json.Unmarshal(pi.LatestCharge, &ch) != nil || ch.PaymentMethodDetails.Card.CaptureBefore == 0 {
		return time.Time{}
	}
	return time.Unix(ch.PaymentMethodDetails.Card.CaptureBefore, 0)
}

// GetPaymentIntent reads a PaymentIntent on a merchant's account, with its
// latest charge expanded.
func (c *Client) GetPaymentIntent(ctx context.Context, account, id string) (PaymentIntent, error) {
	var pi PaymentIntent
	return pi, c.do(ctx, http.MethodGet, "/v1/payment_intents/"+url.PathEscape(id), account, url.Values{"expand[]": {"latest_charge"}}, &pi)
}

// CapturePaymentIntent takes amount (0 = all) of an authorised payment; the
// rest of the hold is released. fee replaces the application fee (0 = keep
// none set here).
func (c *Client) CapturePaymentIntent(ctx context.Context, account, id string, amount, fee int64) (PaymentIntent, error) {
	form := url.Values{}
	if amount > 0 {
		form.Set("amount_to_capture", strconv.FormatInt(amount, 10))
	}
	if fee > 0 {
		form.Set("application_fee_amount", strconv.FormatInt(fee, 10))
	}
	var pi PaymentIntent
	return pi, c.do(ctx, http.MethodPost, "/v1/payment_intents/"+url.PathEscape(id)+"/capture", account, form, &pi)
}

// CancelPaymentIntent releases an authorisation.
func (c *Client) CancelPaymentIntent(ctx context.Context, account, id string) (PaymentIntent, error) {
	var pi PaymentIntent
	return pi, c.do(ctx, http.MethodPost, "/v1/payment_intents/"+url.PathEscape(id)+"/cancel", account, url.Values{}, &pi)
}

// GetConnectedSubscription reads a subscription on a merchant's account.
func (c *Client) GetConnectedSubscription(ctx context.Context, account, id string) (Subscription, error) {
	var s Subscription
	return s, c.do(ctx, http.MethodGet, "/v1/subscriptions/"+url.PathEscape(id), account, nil, &s)
}

// SetCancelAtPeriodEnd ends a subscription on a merchant's account when the
// current period ends (or undoes that).
func (c *Client) SetCancelAtPeriodEnd(ctx context.Context, account, id string, cancel bool) (Subscription, error) {
	var s Subscription
	return s, c.do(ctx, http.MethodPost, "/v1/subscriptions/"+url.PathEscape(id), account,
		url.Values{"cancel_at_period_end": {strconv.FormatBool(cancel)}}, &s)
}

// CancelConnectedSubscription ends a subscription on a merchant's account
// at once.
func (c *Client) CancelConnectedSubscription(ctx context.Context, account, id string) (Subscription, error) {
	var s Subscription
	return s, c.do(ctx, http.MethodDelete, "/v1/subscriptions/"+url.PathEscape(id), account, nil, &s)
}

// InvoicePaymentIntent finds the PaymentIntent that paid an invoice on a
// merchant's account (GET /v1/invoice_payments), or "".
func (c *Client) InvoicePaymentIntent(ctx context.Context, account, invoice string) (string, error) {
	var out struct {
		Data []struct {
			Payment struct {
				PaymentIntent string `json:"payment_intent"`
			} `json:"payment"`
		} `json:"data"`
	}
	if err := c.do(ctx, http.MethodGet, "/v1/invoice_payments", account, url.Values{"invoice": {invoice}, "status": {"paid"}}, &out); err != nil {
		return "", err
	}
	for _, d := range out.Data {
		if d.Payment.PaymentIntent != "" {
			return d.Payment.PaymentIntent, nil
		}
	}
	return "", nil
}

// Invoice is the part of an invoice Renoxium needs. Both the current shape
// (parent.subscription_details) and the older top-level fields are read.
type Invoice struct {
	ID            string `json:"id"`
	Status        string `json:"status"`
	AmountPaid    int64  `json:"amount_paid"`
	AmountDue     int64  `json:"amount_due"`
	Currency      string `json:"currency"`
	BillingReason string `json:"billing_reason"`
	CustomerEmail string `json:"customer_email"`
	CustomerName  string `json:"customer_name"`
	PeriodStart   int64  `json:"period_start"`
	PeriodEnd     int64  `json:"period_end"`
	// Older API versions.
	LegacySubscription        string `json:"subscription"`
	LegacyPaymentIntent       string `json:"payment_intent"`
	LegacySubscriptionDetails struct {
		Metadata map[string]string `json:"metadata"`
	} `json:"subscription_details"`
	Parent struct {
		SubscriptionDetails struct {
			Subscription string            `json:"subscription"`
			Metadata     map[string]string `json:"metadata"`
		} `json:"subscription_details"`
	} `json:"parent"`
	Lines struct {
		Data []struct {
			Period struct {
				Start int64 `json:"start"`
				End   int64 `json:"end"`
			} `json:"period"`
		} `json:"data"`
	} `json:"lines"`
}

// SubscriptionID is the subscription that created the invoice, or "".
func (in Invoice) SubscriptionID() string {
	if s := in.Parent.SubscriptionDetails.Subscription; s != "" {
		return s
	}
	return in.LegacySubscription
}

// SubscriptionMetadata is the subscription's metadata when it was invoiced.
func (in Invoice) SubscriptionMetadata() map[string]string {
	if m := in.Parent.SubscriptionDetails.Metadata; len(m) > 0 {
		return m
	}
	return in.LegacySubscriptionDetails.Metadata
}

// ServicePeriod is the period the invoice pays for: its first line's
// period, else the invoice's own.
func (in Invoice) ServicePeriod() (start, end time.Time) {
	s, e := in.PeriodStart, in.PeriodEnd
	if len(in.Lines.Data) > 0 && in.Lines.Data[0].Period.End > 0 {
		s, e = in.Lines.Data[0].Period.Start, in.Lines.Data[0].Period.End
	}
	return time.Unix(s, 0), time.Unix(e, 0)
}

// DirectAccount stands for the key's own Stripe account where a connected
// account is expected: an app running on its owner's server charges on the
// owner's account directly, with no Stripe-Account header and no Connect.
const DirectAccount = "direct"

// --- Billing: the platform's own subscriptions (no Stripe-Account header) ---

// CreateCustomer creates a customer on the platform account.
func (c *Client) CreateCustomer(ctx context.Context, email string, metadata map[string]string) (string, error) {
	form := url.Values{"email": {email}}
	for k, v := range metadata {
		form.Set("metadata["+k+"]", v)
	}
	var out struct {
		ID string `json:"id"`
	}
	err := c.do(ctx, http.MethodPost, "/v1/customers", "", form, &out)
	return out.ID, err
}

// SubscriptionCheckout returns a Checkout URL that subscribes a customer
// to one recurring price.
func (c *Client) SubscriptionCheckout(ctx context.Context, customer, price, successURL, cancelURL string, metadata map[string]string) (string, error) {
	form := url.Values{
		"mode": {"subscription"}, "customer": {customer}, "success_url": {successURL}, "cancel_url": {cancelURL},
		"line_items[0][price]": {price}, "line_items[0][quantity]": {"1"},
	}
	for k, v := range metadata {
		form.Set("metadata["+k+"]", v)
		form.Set("subscription_data[metadata]["+k+"]", v)
	}
	var out struct {
		URL string `json:"url"`
	}
	err := c.do(ctx, http.MethodPost, "/v1/checkout/sessions", "", form, &out)
	return out.URL, err
}

// Subscription is the part of a subscription Renoxium needs.
type Subscription struct {
	ID                string            `json:"id"`
	Customer          string            `json:"customer"`
	Status            string            `json:"status"`
	CancelAtPeriodEnd bool              `json:"cancel_at_period_end"`
	Metadata          map[string]string `json:"metadata"`
	// LegacyPeriodEnd is current_period_end of older API versions.
	LegacyPeriodEnd int64 `json:"current_period_end"`
	Items           struct {
		Data []struct {
			ID    string `json:"id"`
			Price struct {
				ID string `json:"id"`
			} `json:"price"`
			Quantity         int64 `json:"quantity"`
			CurrentPeriodEnd int64 `json:"current_period_end"`
		} `json:"data"`
	} `json:"items"`
}

// Item finds the subscription's item for a price: its id and quantity.
func (s Subscription) Item(price string) (id string, quantity int64, ok bool) {
	for _, it := range s.Items.Data {
		if price != "" && it.Price.ID == price {
			return it.ID, it.Quantity, true
		}
	}
	return "", 0, false
}

// PriceID is the price of the subscription's first item.
func (s Subscription) PriceID() string {
	if len(s.Items.Data) == 0 {
		return ""
	}
	return s.Items.Data[0].Price.ID
}

// PeriodEnd is when the current paid period ends, or zero.
func (s Subscription) PeriodEnd() time.Time {
	if len(s.Items.Data) == 0 || s.Items.Data[0].CurrentPeriodEnd == 0 {
		if s.LegacyPeriodEnd > 0 {
			return time.Unix(s.LegacyPeriodEnd, 0)
		}
		return time.Time{}
	}
	return time.Unix(s.Items.Data[0].CurrentPeriodEnd, 0)
}

// Subscriptions lists a customer's subscriptions in every status.
func (c *Client) Subscriptions(ctx context.Context, customer string) ([]Subscription, error) {
	var out struct {
		Data []Subscription `json:"data"`
	}
	err := c.do(ctx, http.MethodGet, "/v1/subscriptions", "", url.Values{"customer": {customer}, "status": {"all"}, "limit": {"20"}}, &out)
	return out.Data, err
}

// Subscription reads one subscription.
func (c *Client) Subscription(ctx context.Context, id string) (Subscription, error) {
	var out Subscription
	err := c.do(ctx, http.MethodGet, "/v1/subscriptions/"+url.PathEscape(id), "", nil, &out)
	return out, err
}

// SetSubscriptionQuantity makes the subscription bill quantity of a price,
// prorated: it adds the item when it is missing, changes its quantity, or
// removes it at 0. itemID is the item's id, or "" when it has none yet.
func (c *Client) SetSubscriptionQuantity(ctx context.Context, subscription, itemID, price string, quantity int64) error {
	var out struct {
		ID string `json:"id"`
	}
	switch {
	case itemID == "" && quantity <= 0:
		return nil
	case itemID == "":
		return c.do(ctx, http.MethodPost, "/v1/subscription_items", "", url.Values{"subscription": {subscription}, "price": {price},
			"quantity": {strconv.FormatInt(quantity, 10)}, "proration_behavior": {"create_prorations"}}, &out)
	case quantity <= 0:
		return c.do(ctx, http.MethodDelete, "/v1/subscription_items/"+url.PathEscape(itemID), "",
			url.Values{"proration_behavior": {"create_prorations"}}, &out)
	}
	return c.do(ctx, http.MethodPost, "/v1/subscription_items/"+url.PathEscape(itemID), "",
		url.Values{"quantity": {strconv.FormatInt(quantity, 10)}, "proration_behavior": {"create_prorations"}}, &out)
}

// CancelSubscription ends a subscription at once, without proration
// credit. Used when an owner deletes their account.
func (c *Client) CancelSubscription(ctx context.Context, id string) error {
	var out struct {
		ID string `json:"id"`
	}
	return c.do(ctx, http.MethodDelete, "/v1/subscriptions/"+url.PathEscape(id), "", nil, &out)
}

// BillingPortal returns a Customer Portal URL where the customer manages
// payment methods, invoices and cancellation.
func (c *Client) BillingPortal(ctx context.Context, customer, returnURL string) (string, error) {
	var out struct {
		URL string `json:"url"`
	}
	err := c.do(ctx, http.MethodPost, "/v1/billing_portal/sessions", "", url.Values{"customer": {customer}, "return_url": {returnURL}}, &out)
	return out.URL, err
}

// SetupCheckout returns a Checkout URL (mode setup) where the customer
// saves a card for later payments. Nothing is charged.
func (c *Client) SetupCheckout(ctx context.Context, customer, successURL, cancelURL string, metadata map[string]string) (string, error) {
	form := url.Values{
		"mode": {"setup"}, "customer": {customer}, "success_url": {successURL}, "cancel_url": {cancelURL},
		"allowed_payment_method_types[0]": {"card"}, // see Checkout: payment_method_types is refused by newer API versions
	}
	for k, v := range metadata {
		form.Set("metadata["+k+"]", v)
		form.Set("setup_intent_data[metadata]["+k+"]", v)
	}
	var out struct {
		URL string `json:"url"`
	}
	err := c.do(ctx, http.MethodPost, "/v1/checkout/sessions", "", form, &out)
	return out.URL, err
}

// PaymentMethod is the part of a card payment method Renoxium needs.
type PaymentMethod struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Customer string `json:"customer"`
	Card     struct {
		Brand    string `json:"brand"`
		Last4    string `json:"last4"`
		ExpMonth int    `json:"exp_month"`
		ExpYear  int    `json:"exp_year"`
	} `json:"card"`
}

// Expires is the first moment the card no longer works: the start of the
// month after its expiry month, UTC. Zero when the expiry is unknown.
func (pm PaymentMethod) Expires() time.Time {
	if pm.Card.ExpYear == 0 || pm.Card.ExpMonth < 1 || pm.Card.ExpMonth > 12 {
		return time.Time{}
	}
	return time.Date(pm.Card.ExpYear, time.Month(pm.Card.ExpMonth)+1, 1, 0, 0, 0, 0, time.UTC)
}

// CardPaymentMethods lists the cards saved on a customer of the platform
// account.
func (c *Client) CardPaymentMethods(ctx context.Context, customer string) ([]PaymentMethod, error) {
	var out struct {
		Data []PaymentMethod `json:"data"`
	}
	err := c.do(ctx, http.MethodGet, "/v1/customers/"+url.PathEscape(customer)+"/payment_methods", "",
		url.Values{"type": {"card"}, "limit": {"20"}}, &out)
	return out.Data, err
}

// Charge is the part of a charge Renoxium needs for refunds.
type Charge struct {
	ID             string `json:"id"`
	PaymentIntent  string `json:"payment_intent"`
	Amount         int64  `json:"amount"`
	AmountRefunded int64  `json:"amount_refunded"`
	Refunded       bool   `json:"refunded"` // fully refunded
}

// Event is a webhook event. Account is set for events from connected
// accounts.
type Event struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Account string `json:"account"`
	Data    struct {
		Object json.RawMessage `json:"object"`
		// PreviousAttributes holds the fields an update changed, with their
		// old values (payment_method.detached: the customer it left).
		PreviousAttributes json.RawMessage `json:"previous_attributes"`
	} `json:"data"`
}

// Tolerance is how old a webhook signature may be.
const Tolerance = 5 * time.Minute

var errSignature = errors.New("stripe webhook: bad signature")

// VerifyWebhook checks the Stripe-Signature header against the endpoint's
// signing secret and returns the event.
func VerifyWebhook(payload []byte, header, secret string, now time.Time) (Event, error) {
	var ts int64
	var sigs []string
	for _, part := range strings.Split(header, ",") {
		k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		switch k {
		case "t":
			ts, _ = strconv.ParseInt(v, 10, 64)
		case "v1":
			sigs = append(sigs, v)
		}
	}
	if ts == 0 || len(sigs) == 0 || secret == "" {
		return Event{}, errSignature
	}
	if d := now.Sub(time.Unix(ts, 0)); d > Tolerance || d < -Tolerance {
		return Event{}, errors.New("stripe webhook: signature too old")
	}
	mac := hmac.New(sha256.New, []byte(secret))
	fmt.Fprintf(mac, "%d.", ts)
	mac.Write(payload)
	want := hex.EncodeToString(mac.Sum(nil))
	ok := false
	for _, s := range sigs {
		if subtle.ConstantTimeCompare([]byte(s), []byte(want)) == 1 {
			ok = true
		}
	}
	if !ok {
		return Event{}, errSignature
	}
	var e Event
	if err := json.Unmarshal(payload, &e); err != nil {
		return Event{}, err
	}
	return e, nil
}

// Sign builds a Stripe-Signature header, for tests.
func Sign(payload []byte, secret string, t time.Time) string {
	mac := hmac.New(sha256.New, []byte(secret))
	fmt.Fprintf(mac, "%d.", t.Unix())
	mac.Write(payload)
	return fmt.Sprintf("t=%d,v1=%s", t.Unix(), hex.EncodeToString(mac.Sum(nil)))
}
