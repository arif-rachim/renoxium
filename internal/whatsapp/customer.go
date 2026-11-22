package whatsapp

import (
	"context"
	"log/slog"
	"strings"
)

// Message is a template message to a customer, such as a booking
// confirmation sent for one of the platform's apps.
type Message struct {
	Template string   // name of the approved template
	Language string   // template language code, default "en"
	Body     []string // body parameters in order; each is cleaned with NoticeText
	// URLParam fills the dynamic part of the template's URL button (button
	// index 0); empty when the template has no URL button.
	URLParam string
	// StopPayload is the payload of the template's quick-reply button
	// (button index 1), returned in the webhook when the customer taps it;
	// empty when the template has no quick-reply button.
	StopPayload string
}

// CustomerSender delivers messages to customers: templates at any time,
// plain text only inside the 24-hour window after the customer wrote.
// Both return the WhatsApp message ID (wamid).
type CustomerSender interface {
	SendTemplate(ctx context.Context, phone string, m Message) (string, error)
	SendText(ctx context.Context, phone, text string) (string, error)
	// SendButtons sends text with up to three reply buttons; a tap comes
	// back in the webhook as an interactive button_reply with the ID.
	SendButtons(ctx context.Context, phone, text string, buttons []Button) (string, error)
}

// Button is a reply button: ID comes back when it is tapped, Title (at
// most 20 characters) is what the customer sees.
type Button struct {
	ID    string
	Title string
}

// SendTemplate implements CustomerSender.
func (c *CloudAPI) SendTemplate(ctx context.Context, phone string, m Message) (string, error) {
	lang := m.Language
	if lang == "" {
		lang = "en"
	}
	params := []any{}
	for _, t := range m.Body {
		params = append(params, map[string]string{"type": "text", "text": NoticeText(t)})
	}
	components := []any{map[string]any{"type": "body", "parameters": params}}
	if m.URLParam != "" {
		components = append(components, map[string]any{"type": "button", "sub_type": "url", "index": "0",
			"parameters": []any{map[string]string{"type": "text", "text": m.URLParam}}})
	}
	if m.StopPayload != "" {
		components = append(components, map[string]any{"type": "button", "sub_type": "quick_reply", "index": "1",
			"parameters": []any{map[string]string{"type": "payload", "payload": m.StopPayload}}})
	}
	return c.post(ctx, phone, m.Template, lang, map[string]any{
		"type": "template",
		"template": map[string]any{
			"name":       m.Template,
			"language":   map[string]string{"code": lang},
			"components": components,
		},
	})
}

// SendText implements CustomerSender. WhatsApp accepts it only inside the
// customer service window.
func (c *CloudAPI) SendText(ctx context.Context, phone, text string) (string, error) {
	if r := []rune(text); len(r) > 4096 {
		text = string(r[:4095]) + "…"
	}
	return c.post(ctx, phone, "", "", map[string]any{
		"type": "text",
		"text": map[string]any{"body": text, "preview_url": false},
	})
}

// SendButtons implements CustomerSender.
func (c *CloudAPI) SendButtons(ctx context.Context, phone, text string, buttons []Button) (string, error) {
	if r := []rune(text); len(r) > 1024 {
		text = string(r[:1023]) + "…"
	}
	list := []any{}
	for _, b := range buttons {
		list = append(list, map[string]any{"type": "reply", "reply": map[string]string{"id": b.ID, "title": b.Title}})
	}
	return c.post(ctx, phone, "", "", map[string]any{
		"type": "interactive",
		"interactive": map[string]any{
			"type":   "button",
			"body":   map[string]string{"text": text},
			"action": map[string]any{"buttons": list},
		},
	})
}

// SendButtons implements CustomerSender.
func (Log) SendButtons(_ context.Context, phone, text string, buttons []Button) (string, error) {
	slog.Info("whatsapp buttons not sent (WhatsApp is not configured)", "to", phone, "text", text, "buttons", len(buttons))
	return "", nil
}

// SendTemplate implements CustomerSender.
func (Log) SendTemplate(_ context.Context, phone string, m Message) (string, error) {
	slog.Info("whatsapp template not sent (WhatsApp is not configured)", "to", phone, "template", m.Template, "body", strings.Join(m.Body, " | "))
	return "", nil
}

// SendText implements CustomerSender.
func (Log) SendText(_ context.Context, phone, text string) (string, error) {
	slog.Info("whatsapp text not sent (WhatsApp is not configured)", "to", phone, "text", text)
	return "", nil
}
