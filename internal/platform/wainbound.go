package platform

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/arif-rachim/renoxium/internal/whatsapp"
)

// The WhatsApp webhook of the platform number, after its signature was
// checked (web.Site). Phase 1 reads delivery statuses and opt-outs: the
// "Stop messages" button and STOP words. Any other message gets one fixed
// answer a day pointing to the business, because nobody reads the platform
// number.

// stopWords end WhatsApp messages from the apps that wrote to the number.
var stopWords = map[string]bool{"stop": true, "stop all": true, "unsubscribe": true, "stop messages": true,
	"الغاء": true, "إلغاء": true, "توقف": true, "ايقاف": true, "إيقاف": true}

// statusRank orders statuses: an older one arriving late never wins.
var statusRank = map[string]int{"queued": 0, "sent": 1, "delivered": 2, "read": 3, "failed": 4}

// HandleWhatsAppWebhook applies a verified webhook body. It returns at once
// (Meta wants a quick answer and retries otherwise); the work runs after.
func (p *Platform) HandleWhatsAppWebhook(body []byte) error {
	in, statuses, err := whatsapp.Events(body)
	if err != nil {
		return err
	}
	p.background(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		for _, s := range statuses {
			if p.ourNumber(s.PhoneNumberID) {
				p.waStatus(s)
			}
		}
		for _, m := range in {
			if p.ourNumber(m.PhoneNumberID) {
				p.waInbound(ctx, m)
			}
		}
	})
	return nil
}

func (p *Platform) ourNumber(id string) bool {
	if p.cfg.WhatsAppPhoneNumberID == "" || id == p.cfg.WhatsAppPhoneNumberID {
		return true
	}
	slog.Warn("whatsapp webhook for another phone number ignored", "phone_number_id", id)
	return false
}

func (p *Platform) waStatus(s whatsapp.Status) {
	if s.ID == "" {
		return
	}
	var id, cur string
	if p.db.QueryRow(`SELECT id, status FROM wa_messages WHERE wamid = ?`, s.ID).Scan(&id, &cur) != nil {
		return // a sign-in code or owner notice, or a message already forgotten
	}
	if statusRank[s.Status] <= statusRank[cur] {
		return
	}
	code := ""
	if len(s.Errors) > 0 {
		code = fmt.Sprint(s.Errors[0].Code)
	}
	p.db.Exec(`UPDATE wa_messages SET status = ?, category = CASE WHEN ? != '' THEN ? ELSE category END,
		pricing = CASE WHEN ? != '' THEN ? ELSE pricing END, error = CASE WHEN ? != '' THEN ? ELSE error END, updated_at = ? WHERE id = ?`,
		s.Status, s.Pricing.Category, s.Pricing.Category, s.Pricing.Type, s.Pricing.Type, code, code, time.Now().Unix(), id)
}

func (p *Platform) waInbound(ctx context.Context, m whatsapp.Inbound) {
	if m.ID == "" {
		return
	}
	// Meta may deliver a message more than once.
	res, err := p.db.Exec(`INSERT OR IGNORE INTO wa_seen (wamid, at) VALUES (?, ?)`, m.ID, time.Now().Unix())
	if n, _ := rowsAffected(res, err); n == 0 {
		return
	}
	phone := m.Phone()
	if phone == "" {
		return // known by username only; replies need phase 2
	}
	if m.Type == "button" && strings.HasPrefix(m.Button.Payload, stopPrefix) {
		var app, stageName, to string
		err := p.db.QueryRow(`SELECT app, stage, phone FROM wa_messages WHERE id = ?`, strings.TrimPrefix(m.Button.Payload, stopPrefix)).Scan(&app, &stageName, &to)
		if err != nil || to != phone {
			return
		}
		name := p.optOut(ctx, app, stageName, phone, "the Stop button")
		p.waReply(ctx, phone, "You will not get more WhatsApp messages from "+name+". If you book or order again, you can choose to get them again.")
		return
	}
	text := strings.ToLower(strings.Join(strings.Fields(m.Text.Body), " "))
	if m.Type == "text" && stopWords[strings.Trim(text, ".!")] {
		var names []string
		for _, a := range p.recentApps(phone) {
			names = append(names, p.optOut(ctx, a.app, a.stage, phone, "a STOP message"))
		}
		if len(names) == 0 {
			return
		}
		p.waReply(ctx, phone, "You will not get more WhatsApp messages from "+strings.Join(names, ", ")+".")
		return
	}
	// One answer a day: the platform number is not read by anyone.
	if !p.allowDurable("wa-answer:"+phone, 1, 24*time.Hour) {
		return
	}
	apps := p.recentApps(phone)
	if len(apps) == 0 {
		p.waReply(ctx, phone, "This WhatsApp number only sends updates for businesses that use Renoxium, and replies here are not read.")
		return
	}
	a := apps[0]
	st, err := p.loadStage(a.app, a.stage)
	if err != nil || st == nil {
		return
	}
	p.waReply(ctx, phone, "This WhatsApp number only sends updates from "+p.businessName(ctx, st)+
		", and replies here are not read. To contact them, use the details on "+p.URL(a.app, a.stage)+" . Reply STOP to stop these messages.")
}

type appStage struct{ app, stage string }

// recentApps are the apps that wrote to phone in the last 30 days, newest first.
func (p *Platform) recentApps(phone string) []appStage {
	rows, err := p.db.Query(`SELECT app, stage FROM wa_messages WHERE phone = ? AND created_at >= ? GROUP BY app, stage ORDER BY max(created_at) DESC LIMIT 10`,
		phone, time.Now().Add(-30*24*time.Hour).Unix())
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []appStage
	for rows.Next() {
		var a appStage
		if rows.Scan(&a.app, &a.stage) == nil {
			out = append(out, a)
		}
	}
	return out
}

// optOut stops an app's messages to phone and returns the business name.
func (p *Platform) optOut(ctx context.Context, app, stageName, phone, how string) string {
	st, err := p.loadStage(app, stageName)
	if err != nil || st == nil {
		return app
	}
	now := time.Now().Unix()
	_, err = st.admin.ExecContext(ctx, `INSERT INTO rx_wa_consent (phone, source, opted_out_at) VALUES (?, 'opt_out', ?)
		ON CONFLICT (phone) DO UPDATE SET opted_out_at = excluded.opted_out_at WHERE opted_out_at = 0`, phone, now)
	if err != nil && err != sql.ErrNoRows {
		slog.Error("whatsapp opt-out", "app", app, "err", err)
	}
	p.db.Exec(`INSERT INTO wa_optouts (app, phone, at) VALUES (?, ?, ?)`, app, phone, now)
	p.event(st, "whatsapp", "info", "", fmt.Sprintf("customer %s stopped WhatsApp messages from this app (%s)", MaskPhone(phone), how))
	p.checkOptOutRate(app)
	return p.businessName(ctx, st)
}

// waReply answers inside the 24-hour window the customer's message opened.
func (p *Platform) waReply(ctx context.Context, phone, text string) {
	if p.cfg.WhatsAppCustomers == nil || !p.allowDurable("wa-day", p.cfg.Daily.WhatsApp, 24*time.Hour) {
		return
	}
	if _, err := p.cfg.WhatsAppCustomers.SendText(ctx, phone, text); err != nil {
		slog.Error("whatsapp reply", "err", err)
	}
}

// WhatsAppLink is where a message's View button leads: the app page the
// script named, on the app that sent it.
func (p *Platform) WhatsAppLink(id string) (string, bool) {
	var app, stageName, link string
	if p.db.QueryRow(`SELECT app, stage, link FROM wa_messages WHERE id = ?`, id).Scan(&app, &stageName, &link) != nil {
		return "", false
	}
	if _, err := p.Info(app); err != nil {
		return "", false
	}
	return p.URL(app, stageName) + link, true
}
