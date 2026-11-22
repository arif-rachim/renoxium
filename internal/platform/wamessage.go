package platform

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/arif-rachim/renoxium/internal/whatsapp"
)

// Scripts can send a short WhatsApp message to one number, such as "two
// patients are ahead of you" to a patient who gave their mobile number. It
// goes out with Renoxium's approved notice template (the app's name, then
// the text), so it is a one-way notice, never a chat. Each message counts
// towards the account's monthly WhatsApp messages, the same quota as
// sign-in codes and owner notices, and is limited per number and per app
// per day.

// WhatsAppPerRecipient is how many messages one app may send one number a day.
var WhatsAppPerRecipient = 5

// whatsappMaxText is the longest text whatsapp.send accepts: a template
// parameter holds at most about 200 characters.
const whatsappMaxText = 200

type waInput struct {
	To   string `json:"to"`
	Text string `json:"text"`
}

// whatsappSend answers whatsapp.send({to, text}). Live sends it in the
// background within the quota and returns {sent: true}; preview writes it
// to the app log (and to a demo's inbox) and returns {sent: false}. It
// throws when the number or text is wrong and when a limit is reached, so
// a script can fall back to email.
func (p *Platform) whatsappSend(ctx context.Context, st *stage, arg any) (any, error) {
	var in waInput
	if err := decodeArg(arg, &in); err != nil {
		return nil, errors.New("whatsapp.send expects {to, text}")
	}
	phone, ok := whatsapp.Normalize(in.To)
	if !ok {
		return nil, fmt.Errorf("whatsapp.send: to %q is not a phone number with its country code, e.g. +971501234567", in.To)
	}
	text := strings.Join(strings.Fields(in.Text), " ")
	if text == "" || utf8.RuneCountInString(text) > whatsappMaxText {
		return nil, fmt.Errorf("whatsapp.send: text is required, on one line, at most %d characters", whatsappMaxText)
	}
	if !p.allowDurable("wa-to:"+st.app+":"+st.name+":"+phone, WhatsAppPerRecipient, 24*time.Hour) {
		return nil, errors.New("whatsapp.send: too many WhatsApp messages to this number today")
	}
	if st.name == Preview {
		p.event(st, "whatsapp", "info", "", fmt.Sprintf("not sent from preview: WhatsApp to %s: %s", phone, text))
		p.demoDeliver(ctx, st, DemoMessage{Kind: "whatsapp", To: phone, Subject: "WhatsApp message", Text: text})
		return map[string]any{"sent": false}, nil
	}
	if p.cfg.WhatsAppNotices == nil {
		p.event(st, "whatsapp", "warn", "", "WhatsApp message not sent: WhatsApp is not set up on this Renoxium server")
		return nil, errors.New("whatsapp.send: WhatsApp messages are not available on this Renoxium server")
	}
	if !p.whatsappQuotaLeft(st) {
		p.event(st, "whatsapp", "warn", "", "WhatsApp message not sent: the monthly WhatsApp messages are used up")
		return nil, errors.New("whatsapp.send: the monthly WhatsApp messages of this account are used up")
	}
	if !p.allowDurable("wa-day:"+st.app, p.cfg.Daily.WhatsAppPerApp, 24*time.Hour) || !p.allowDurable("wa-day", p.cfg.Daily.WhatsApp, 24*time.Hour) {
		p.event(st, "whatsapp", "warn", "", "WhatsApp message not sent: the daily WhatsApp limit was reached")
		return nil, errors.New("whatsapp.send: the daily WhatsApp limit was reached")
	}
	p.countWhatsApp(st)
	app := st.app
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := p.cfg.WhatsAppNotices.SendNotice(ctx, phone, app, text); err != nil {
			// Meta's answer can describe the platform's own account.
			slog.Error("whatsapp message", "app", app, "err", err)
			p.event(st, "whatsapp", "error", "", "a WhatsApp message could not be delivered; Renoxium has logged the reason")
			return
		}
		p.meterAdd(app, st.name, mWhatsApp, 1)
	}()
	return map[string]any{"sent": true}, nil
}
