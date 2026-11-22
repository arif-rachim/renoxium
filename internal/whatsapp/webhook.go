package whatsapp

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
)

// Meta signs every webhook POST with the app secret:
// X-Hub-Signature-256: sha256=<hex HMAC-SHA256 of the raw body>.

// SignatureHeader is the header that carries a webhook's signature.
const SignatureHeader = "X-Hub-Signature-256"

// Sign returns the signature header value for body. Tests and the e2e fake
// use it; Meta computes the same.
func Sign(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// VerifySignature reports whether header is the signature of body made with
// secret. An empty secret or header never verifies.
func VerifySignature(secret string, body []byte, header string) bool {
	if secret == "" || !strings.HasPrefix(header, "sha256=") {
		return false
	}
	got, err := hex.DecodeString(strings.TrimPrefix(header, "sha256="))
	if err != nil || len(got) != sha256.Size {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hmac.Equal(got, mac.Sum(nil))
}

// Webhook is the part of a WhatsApp Cloud API webhook Renoxium reads.
type Webhook struct {
	Object string `json:"object"`
	Entry  []struct {
		ID      string `json:"id"` // the WhatsApp Business Account
		Changes []struct {
			Field string `json:"field"`
			Value struct {
				Metadata struct {
					PhoneNumberID string `json:"phone_number_id"`
				} `json:"metadata"`
				Contacts []struct {
					WaID    string `json:"wa_id"`
					UserID  string `json:"user_id"`
					Profile struct {
						Name string `json:"name"`
					} `json:"profile"`
				} `json:"contacts"`
				Messages []Inbound `json:"messages"`
				Statuses []Status  `json:"statuses"`
			} `json:"value"`
		} `json:"changes"`
	} `json:"entry"`
}

// Inbound is a message a customer sent to a business number.
type Inbound struct {
	ID         string `json:"id"` // wamid
	From       string `json:"from"`
	FromUserID string `json:"from_user_id"`
	Timestamp  string `json:"timestamp"`
	Type       string `json:"type"`
	Text       struct {
		Body string `json:"body"`
	} `json:"text"`
	// Button is set when the customer taps a template's quick-reply button.
	Button struct {
		Payload string `json:"payload"`
		Text    string `json:"text"`
	} `json:"button"`
	// Interactive is set when the customer taps a reply button the
	// business sent (SendButtons).
	Interactive struct {
		Type        string `json:"type"`
		ButtonReply struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		} `json:"button_reply"`
	} `json:"interactive"`
	// Name is the sender's WhatsApp profile name (copied from contacts by Events).
	Name string `json:"-"`
	// PhoneNumberID is the business number it was sent to (copied from the
	// change's metadata by Events).
	PhoneNumberID string `json:"-"`
}

// Status reports what happened to a message the business sent.
type Status struct {
	ID          string `json:"id"` // wamid
	Status      string `json:"status"`
	Timestamp   string `json:"timestamp"`
	RecipientID string `json:"recipient_id"`
	Pricing     struct {
		Category string `json:"category"`
		Type     string `json:"type"`
	} `json:"pricing"`
	Errors []struct {
		Code  int    `json:"code"`
		Title string `json:"title"`
	} `json:"errors"`
	PhoneNumberID string `json:"-"`
}

// Phone returns the inbound sender as E.164 (+9715...), or "" when the
// webhook left the number out (a user known only by a username).
func (m Inbound) Phone() string {
	if m.From == "" {
		return ""
	}
	n, ok := Normalize("+" + strings.TrimPrefix(m.From, "+"))
	if !ok {
		return ""
	}
	return n
}

// Events parses a webhook body into its inbound messages and statuses.
func Events(body []byte) (in []Inbound, st []Status, err error) {
	var w Webhook
	if err := json.Unmarshal(body, &w); err != nil {
		return nil, nil, err
	}
	for _, e := range w.Entry {
		for _, c := range e.Changes {
			if c.Field != "" && c.Field != "messages" {
				continue
			}
			id := c.Value.Metadata.PhoneNumberID
			for _, m := range c.Value.Messages {
				m.PhoneNumberID = id
				for _, ct := range c.Value.Contacts {
					if ct.WaID != "" && ct.WaID == m.From {
						m.Name = ct.Profile.Name
					}
				}
				in = append(in, m)
			}
			for _, s := range c.Value.Statuses {
				s.PhoneNumberID = id
				st = append(st, s)
			}
		}
	}
	return in, st, nil
}
