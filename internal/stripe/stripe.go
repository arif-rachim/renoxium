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
	if account != "" {
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

// CreateAccount creates a merchant account that behaves like a Standard
// account: the merchant gets the full Stripe Dashboard, pays Stripe's fees,
// Stripe covers negative balances and collects requirements. These four
// values are always sent, so the platform's Dashboard defaults never apply.
func (c *Client) CreateAccount(ctx context.Context, email, country string) (Account, error) {
	form := url.Values{
		"controller[stripe_dashboard][type]": {"full"},
		"controller[fees][payer]":            {"account"},
		"controller[losses][payments]":       {"stripe"},
		"controller[requirement_collection]": {"stripe"},
	}
	if email != "" {
		form.Set("email", email)
	}
	if country != "" {
		form.Set("country", country)
	}
	var a Account
	return a, c.do(ctx, http.MethodPost, "/v1/accounts", "", form, &a)
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
}

// Session is the part of a Checkout Session Renoxium needs.
type Session struct {
	ID            string            `json:"id"`
	URL           string            `json:"url"`
	PaymentStatus string            `json:"payment_status"`
	Status        string            `json:"status"`
	AmountTotal   int64             `json:"amount_total"`
	Currency      string            `json:"currency"`
	Reference     string            `json:"client_reference_id"`
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
	var s Session
	return s, c.do(ctx, http.MethodPost, "/v1/checkout/sessions", account, form, &s)
}

// Event is a webhook event. Account is set for events from connected
// accounts.
type Event struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Account string `json:"account"`
	Data    struct {
		Object json.RawMessage `json:"object"`
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
