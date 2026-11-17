package platform

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/arif-rachim/renoxium/internal/whatsapp"
)

// Owners can get notify.owner notices, such as new orders, on WhatsApp as
// well as by email. The number is confirmed with a code first, and each
// notice counts towards the account's monthly WhatsApp messages.

// AlertPhone is an app's WhatsApp number for owner notices.
type AlertPhone struct {
	Phone     string
	Confirmed bool
}

func (p *Platform) ensureAlertTable() error {
	_, err := p.db.Exec(`CREATE TABLE IF NOT EXISTS owner_alerts (app TEXT PRIMARY KEY, phone TEXT NOT NULL,
		confirmed INTEGER NOT NULL DEFAULT 0, code_hash TEXT NOT NULL DEFAULT '', code_expires INTEGER NOT NULL DEFAULT 0)`)
	return err
}

// WhatsAppAlertsAvailable reports whether this server can send notices.
func (p *Platform) WhatsAppAlertsAvailable() bool {
	return p.cfg.WhatsApp != nil && p.cfg.WhatsAppNotices != nil
}

// AlertPhoneOf returns the app's notice number, or nil.
func (p *Platform) AlertPhoneOf(app string) *AlertPhone {
	var a AlertPhone
	var confirmed int
	if err := p.db.QueryRow(`SELECT phone, confirmed FROM owner_alerts WHERE app = ?`, app).Scan(&a.Phone, &confirmed); err != nil {
		return nil
	}
	a.Confirmed = confirmed == 1
	return &a
}

// StartAlertPhone sends a confirmation code to the number.
func (p *Platform) StartAlertPhone(ctx context.Context, app, raw string) error {
	if !p.WhatsAppAlertsAvailable() {
		return errors.New("WhatsApp notices are not available on this Renoxium server yet")
	}
	phone, ok := whatsapp.Normalize(raw)
	if !ok {
		return errors.New("enter the number with the country code, for example +971 50 123 4567")
	}
	owner := p.ownerOf(app)
	if !p.allow("alert-phone:"+app, 5, time.Hour) {
		return errors.New("too many codes requested; try again in an hour")
	}
	if p.cfg.OwnerPlan != nil {
		if used, quota := p.WhatsAppUsage(owner); used >= quota {
			return fmt.Errorf("your apps used all %d WhatsApp messages of this month; upgrade at %s/billing", quota, p.cfg.SiteURL)
		}
	}
	code := sixDigits()
	if _, err := p.db.Exec(`INSERT INTO owner_alerts (app, phone, confirmed, code_hash, code_expires) VALUES (?, ?, 0, ?, ?)
		ON CONFLICT (app) DO UPDATE SET phone = excluded.phone, confirmed = 0, code_hash = excluded.code_hash, code_expires = excluded.code_expires`,
		app, phone, sha(phone+":"+code), time.Now().Add(phoneCodeTTL).Unix()); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancel()
	if err := p.cfg.WhatsApp.SendCode(ctx, phone, code); err != nil {
		return errors.New("WhatsApp did not accept this number; check it and try again")
	}
	if p.cfg.OwnerPlan != nil {
		p.addUsage(owner, "whatsapp")
	}
	return nil
}

// ConfirmAlertPhone turns notices on when the code is right.
func (p *Platform) ConfirmAlertPhone(app, code string) error {
	if !p.allow("alert-confirm:"+app, 10, time.Hour) {
		return errors.New("too many attempts; try again in an hour")
	}
	var phone, hash string
	var expires int64
	err := p.db.QueryRow(`SELECT phone, code_hash, code_expires FROM owner_alerts WHERE app = ?`, app).Scan(&phone, &hash, &expires)
	if errors.Is(err, sql.ErrNoRows) || hash == "" || time.Now().Unix() > expires {
		return errors.New("the code expired; send a new one")
	}
	if err != nil {
		return err
	}
	if sha(phone+":"+strings.TrimSpace(code)) != hash {
		return errors.New("that code is not right")
	}
	_, err = p.db.Exec(`UPDATE owner_alerts SET confirmed = 1, code_hash = '' WHERE app = ?`, app)
	return err
}

// RemoveAlertPhone turns WhatsApp notices off.
func (p *Platform) RemoveAlertPhone(app string) error {
	_, err := p.db.Exec(`DELETE FROM owner_alerts WHERE app = ?`, app)
	return err
}

// whatsappOwner sends a notice to the owner's confirmed number, from live,
// within the account's monthly WhatsApp messages. It does not wait.
func (p *Platform) whatsappOwner(st *stage, subject string) {
	a := p.AlertPhoneOf(st.app)
	if a == nil || !a.Confirmed {
		return
	}
	if st.name == Preview {
		p.event(st, "whatsapp", "info", "", fmt.Sprintf("not sent from preview: WhatsApp notice to the owner, %q", subject))
		return
	}
	if p.cfg.WhatsAppNotices == nil {
		return
	}
	if !p.whatsappQuotaLeft(st) {
		p.event(st, "whatsapp", "warn", "", "owner WhatsApp notice not sent: the monthly WhatsApp messages are used up: "+subject)
		return
	}
	if !p.allow("owner-wa:"+st.app, 100, 24*time.Hour) || !p.allow("wa-day", p.cfg.Daily.WhatsApp, 24*time.Hour) {
		p.event(st, "whatsapp", "warn", "", "owner WhatsApp notice not sent: daily limit reached: "+subject)
		return
	}
	p.countWhatsApp(st)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := p.cfg.WhatsAppNotices.SendNotice(ctx, a.Phone, st.app, subject); err != nil {
			p.event(st, "whatsapp", "error", "", "owner WhatsApp notice failed: "+err.Error())
		}
	}()
}
