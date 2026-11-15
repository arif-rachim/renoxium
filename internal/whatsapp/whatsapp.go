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
	Language      string // template language code, default en_US
	Version       string // Graph API version, default v23.0
	BaseURL       string // default https://graph.facebook.com
	Client        *http.Client
}

// SendCode implements Sender.
func (c *CloudAPI) SendCode(ctx context.Context, phone, code string) error {
	lang, version, base := c.Language, c.Version, c.BaseURL
	if lang == "" {
		lang = "en_US"
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
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("send whatsapp code: whatsapp answered %d: %s", resp.StatusCode, detail)
	}
	return nil
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
