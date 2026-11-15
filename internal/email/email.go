// Package email sends transactional email through Resend, or writes it to
// the log when no API key is configured (local development).
package email

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/mail"
	"strings"
	"time"
)

// Message is one email to one recipient.
type Message struct {
	To      string
	Subject string
	Text    string
	HTML    string
	ReplyTo string
	// Tag labels the message in Resend, e.g. "password_reset".
	Tag string
	// FromName replaces the display name of the configured sender, keeping
	// its address, e.g. "Al Noor Perfumes".
	FromName string
}

// Sender delivers messages.
type Sender interface {
	Send(ctx context.Context, m Message) error
}

// Resend sends through the Resend API.
type Resend struct {
	Key      string
	From     string // e.g. "Renoxium <no-reply@renoxium.com>"; the domain must be verified in Resend
	Endpoint string // default https://api.resend.com/emails
	Client   *http.Client
}

// Send implements Sender.
func (r *Resend) Send(ctx context.Context, m Message) error {
	from := r.From
	if m.FromName != "" {
		if addr, err := mail.ParseAddress(r.From); err == nil {
			name := strings.Map(func(c rune) rune {
				if strings.ContainsRune("<>\"\r\n,;", c) {
					return -1
				}
				return c
			}, m.FromName)
			from = (&mail.Address{Name: name, Address: addr.Address}).String()
		}
	}
	body := map[string]any{"from": from, "to": []string{m.To}, "subject": m.Subject, "text": m.Text}
	if m.HTML != "" {
		body["html"] = m.HTML
	}
	if m.ReplyTo != "" {
		body["reply_to"] = m.ReplyTo
	}
	if m.Tag != "" {
		body["tags"] = []map[string]string{{"name": "kind", "value": m.Tag}}
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	endpoint := r.Endpoint
	if endpoint == "" {
		endpoint = "https://api.resend.com/emails"
	}
	client := r.Client
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	var key [16]byte
	rand.Read(key[:])
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+r.Key)
	req.Header.Set("Content-Type", "application/json")
	// Lets Resend drop a duplicate if this request is ever retried.
	req.Header.Set("Idempotency-Key", hex.EncodeToString(key[:]))
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("send email: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("send email: resend answered %d: %s", resp.StatusCode, detail)
	}
	return nil
}

// Log writes messages to the log instead of sending them. Links in the
// message are visible there, which is what local development needs.
type Log struct{}

// Send implements Sender.
func (Log) Send(_ context.Context, m Message) error {
	slog.Info("email not sent (no RESEND_API_KEY)", "to", m.To, "subject", m.Subject, "text", m.Text)
	return nil
}
