// Package whatsapp sends one-time sign-in codes through the WhatsApp Cloud
// API, or writes them to the log when it is not configured.
package whatsapp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// Sender delivers a one-time code to a phone number in E.164 form.
type Sender interface {
	SendCode(ctx context.Context, phone, code string) error
}

// CloudAPI sends an approved authentication template ("copy code" button)
// through Meta's WhatsApp Cloud API. The template must take the code as its
// only body parameter, as Meta's authentication templates do.
type CloudAPI struct {
	Token         string // system user access token
	PhoneNumberID string // the sending number's ID in WhatsApp Manager
	Template      string // name of the approved authentication template
	Language      string // template language code exactly as created in WhatsApp Manager, default "en"
	Version       string // Graph API version, default v23.0
	BaseURL       string // default https://graph.facebook.com
	Client        *http.Client
}

// SendCode implements Sender.
func (c *CloudAPI) SendCode(ctx context.Context, phone, code string) error {
	lang, version, base := c.Language, c.Version, c.BaseURL
	if lang == "" {
		lang = "en"
	}
	if version == "" {
		version = "v23.0"
	}
	if base == "" {
		base = "https://graph.facebook.com"
	}
	// The code appears twice: in the body and in the copy-code button.
	body := map[string]any{
		"messaging_product": "whatsapp",
		"recipient_type":    "individual",
		"to":                strings.TrimPrefix(phone, "+"),
		"type":              "template",
		"template": map[string]any{
			"name":     c.Template,
			"language": map[string]string{"code": lang},
			"components": []any{
				map[string]any{"type": "body", "parameters": []any{map[string]string{"type": "text", "text": code}}},
				map[string]any{"type": "button", "sub_type": "url", "index": "0", "parameters": []any{map[string]string{"type": "text", "text": code}}},
			},
		},
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("%s/%s/%s/messages", base, version, c.PhoneNumberID), bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "application/json")
	client := c.Client
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("send whatsapp code: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
		return parseError(resp.StatusCode, raw, c.Template, lang)
	}
	return nil
}

// Error is an error answer from the WhatsApp Cloud API.
type Error struct {
	Status  int    // HTTP status
	Code    int    // Meta error code, e.g. 132001
	Subcode int    // error_subcode, when Meta sends one
	Message string // Meta's message
	Details string // error_data.details
	TraceID string // fbtrace_id, for Meta support
	Hint    string // what to check, for known codes
}

func (e *Error) Error() string {
	s := fmt.Sprintf("whatsapp error %d (HTTP %d): %s", e.Code, e.Status, e.Message)
	if e.Details != "" && e.Details != e.Message {
		s += ": " + e.Details
	}
	if e.Hint != "" {
		s += ". Check: " + e.Hint
	}
	if e.TraceID != "" {
		s += " (fbtrace_id " + e.TraceID + ")"
	}
	return s
}

// hints explain the errors a first setup usually meets.
var hints = map[int]string{
	190:    "the access token is invalid or expired; create a new system user token with whatsapp_business_messaging",
	10:     "the token lacks permission; assign the system user to the app and the WhatsApp account, then generate the token again",
	200:    "the token lacks permission; assign the system user to the app and the WhatsApp account, then generate the token again",
	100:    "a field is wrong; check WHATSAPP_PHONE_NUMBER_ID",
	131042: "payment problem; add a valid payment method in WhatsApp Manager",
	132000: "the template expects a different number of parameters; it must be an Authentication template with a Copy code button",
	132001: "the template does not exist in this language; WHATSAPP_OTP_TEMPLATE and WHATSAPP_OTP_LANGUAGE must match the approved template exactly (English is \"en\", English (US) is \"en_US\")",
	132005: "the code is longer than the template allows",
	132007: "the template content breaks a WhatsApp policy",
	132012: "parameter format does not match the template",
	132015: "the template is paused because of low quality",
	132016: "the template is disabled",
	131030: "the recipient is not in the allowed list; a test number or an app in development mode can only message numbers added as recipients",
	133010: "the sending number is not registered with the Cloud API; register it (with its two-step PIN) in WhatsApp Manager",
	133000: "the sending number's registration did not finish; register it again",
	131026: "the message could not be delivered; the recipient may not use WhatsApp or may have an old app version",
	131031: "the WhatsApp account is restricted or disabled; check Account quality in WhatsApp Manager",
	131056: "too many messages to the same number in a short time",
	80007:  "rate limit of the WhatsApp account reached; wait and try again",
	130429: "throughput limit reached; wait and try again",
	368:    "the account is temporarily blocked for policy violations",
}

func parseError(status int, raw []byte, template, lang string) error {
	var body struct {
		Error struct {
			Message   string `json:"message"`
			Code      int    `json:"code"`
			Subcode   int    `json:"error_subcode"`
			TraceID   string `json:"fbtrace_id"`
			ErrorData struct {
				Details string `json:"details"`
			} `json:"error_data"`
		} `json:"error"`
	}
	if json.Unmarshal(raw, &body) != nil || body.Error.Code == 0 && body.Error.Message == "" {
		return fmt.Errorf("whatsapp answered HTTP %d: %s", status, strings.TrimSpace(string(raw)))
	}
	e := &Error{Status: status, Code: body.Error.Code, Subcode: body.Error.Subcode, Message: body.Error.Message,
		Details: body.Error.ErrorData.Details, TraceID: body.Error.TraceID, Hint: hints[body.Error.Code]}
	if e.Code == 132001 {
		e.Hint += fmt.Sprintf(" (sent template %q, language %q)", template, lang)
	}
	return e
}

// Log writes codes to the log instead of sending them (local development).
type Log struct{}

// SendCode implements Sender.
func (Log) SendCode(_ context.Context, phone, code string) error {
	slog.Info("whatsapp code not sent (WhatsApp is not configured)", "to", phone, "code", code)
	return nil
}

// Normalize turns a phone number as people type it into E.164 (+9715...).
// A local UAE mobile number such as 050 123 4567 gets the +971 prefix.
func Normalize(raw string) (string, bool) {
	var b strings.Builder
	for i, r := range strings.TrimSpace(raw) {
		switch {
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '+' && i == 0:
			b.WriteRune(r)
		case r == ' ' || r == '-' || r == '(' || r == ')' || r == '.':
		default:
			return "", false
		}
	}
	n := b.String()
	switch {
	case strings.HasPrefix(n, "00"):
		n = "+" + n[2:]
	case strings.HasPrefix(n, "05") && len(n) == 10:
		n = "+971" + n[1:]
	case !strings.HasPrefix(n, "+"):
		return "", false
	}
	digits := len(n) - 1
	return n, digits >= 8 && digits <= 15 && n[1] != '0'
}
