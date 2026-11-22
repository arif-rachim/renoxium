package platform

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"html"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/arif-rachim/renoxium/internal/whatsapp"
)

// WhatsApp messages to customers (whatsapp.send). Live apps send approved
// Utility templates from the platform number to customers who ticked the
// platform's opt-in box on one of the app's forms. Every template names the
// business, links only to the app itself and has a "Stop messages" button.
// Preview never sends: messages go to the app log, the demo inbox and the
// test tools' results.

// WhatsAppCustomerPerPlan is how many whatsapp.send messages the live apps
// of one account may send per calendar month.
var WhatsAppCustomerPerPlan = map[string]int{planFree: 30, planPro: 500, planBusiness: 2000}

// Per plan: messages to one number from one app per day, and numbers that
// may newly opt in to one app per day.
var (
	waPerNumberPerDay   = map[string]int{planFree: 3, planPro: 5, planBusiness: 10}
	waNewConsentsPerDay = map[string]int{planFree: 10, planPro: 100, planBusiness: 500}
)

const (
	waBurstPerMinute = 20             // messages per app per minute
	waOptinTTL       = 48 * time.Hour // how long a rendered opt-in box can be posted
	waRetention      = 180 * 24 * time.Hour
	// An app whose customers stop its messages this often is paused: at
	// least waPauseMinSent messages in waPauseWindow, more than
	// waPausePercent of them followed by an opt-out.
	waPauseMinSent = 50
	waPausePercent = 5
	waPauseWindow  = 7 * 24 * time.Hour
)

// CustomerKinds are the kinds whatsapp.send accepts; each is one template.
var CustomerKinds = []string{"confirmation", "reminder", "update"}

// CustomerTemplateBodies are the bodies the operator creates in WhatsApp
// Manager (see docs/deploy-ubuntu.md). Preview renders them with the same
// parameters: {{1}} the business, {{2}} ref, then when (reminder) and text.
var CustomerTemplateBodies = map[string]string{
	"confirmation": "{{1}} confirmed your {{2}}: {{3}}. Tap View for the details.",
	"reminder":     "Reminder from {{1}}: {{2}} on {{3}}. {{4}}",
	"update":       "Update from {{1}} on your {{2}}: {{3}}",
}

const (
	optinField      = "rx_wa_optin"
	optinTokenField = "rx_wa_optin_token"
	stopPrefix      = "rx-stop:"
)

func (p *Platform) ensureWhatsAppTables() error {
	for _, q := range []string{
		`CREATE TABLE IF NOT EXISTS wa_messages (id TEXT PRIMARY KEY, app TEXT NOT NULL, stage TEXT NOT NULL, phone TEXT NOT NULL,
			kind TEXT NOT NULL, link TEXT NOT NULL DEFAULT '', wamid TEXT NOT NULL DEFAULT '', status TEXT NOT NULL,
			category TEXT NOT NULL DEFAULT '', pricing TEXT NOT NULL DEFAULT '', error TEXT NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL)`,
		`CREATE INDEX IF NOT EXISTS wa_messages_wamid ON wa_messages (wamid)`,
		`CREATE INDEX IF NOT EXISTS wa_messages_phone ON wa_messages (phone, created_at)`,
		`CREATE INDEX IF NOT EXISTS wa_messages_app ON wa_messages (app, created_at)`,
		`CREATE TABLE IF NOT EXISTS wa_optouts (app TEXT NOT NULL, phone TEXT NOT NULL, at INTEGER NOT NULL)`,
		`CREATE INDEX IF NOT EXISTS wa_optouts_app ON wa_optouts (app, at)`,
		`CREATE TABLE IF NOT EXISTS wa_paused (app TEXT PRIMARY KEY, reason TEXT NOT NULL, at INTEGER NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS wa_seen (wamid TEXT PRIMARY KEY, at INTEGER NOT NULL)`,
	} {
		if _, err := p.db.Exec(q); err != nil {
			return err
		}
	}
	return nil
}

// cleanWhatsApp forgets old messages and webhook IDs.
func (p *Platform) cleanWhatsApp() {
	now := time.Now()
	p.db.Exec(`DELETE FROM wa_messages WHERE created_at < ?`, now.Add(-waRetention).Unix())
	p.db.Exec(`DELETE FROM wa_optouts WHERE at < ?`, now.Add(-waRetention).Unix())
	p.db.Exec(`DELETE FROM wa_seen WHERE at < ?`, now.Add(-8*24*time.Hour).Unix())
}

// MaskPhone shortens a number for logs: +971501234567 -> +9715…4567.
func MaskPhone(phone string) string {
	if len(phone) <= 9 {
		return phone
	}
	return phone[:5] + "…" + phone[len(phone)-4:]
}

func newMessageID() string {
	var b [16]byte
	rand.Read(b[:])
	return base64.RawURLEncoding.EncodeToString(b[:])
}

// ---- the opt-in box

// optinMarker is where a template wants the opt-in box:
// <rx-whatsapp-optin></rx-whatsapp-optin>, or with lang="ar".
var optinMarker = regexp.MustCompile(`<rx-whatsapp-optin(?:\s+lang="(en|ar)")?\s*(?:/>|>\s*</rx-whatsapp-optin>)`)

func (p *Platform) optinToken(st *stage, now time.Time) string {
	ts := strconv.FormatInt(now.Unix(), 36)
	return ts + "." + p.optinMAC(st, ts)
}

func (p *Platform) optinMAC(st *stage, ts string) string {
	mac := hmac.New(sha256.New, p.live.key)
	fmt.Fprintf(mac, "wa-optin|%s|%s|%s", st.app, st.name, ts)
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// validOptinToken reports whether tok came from an opt-in box this stage
// rendered in the last waOptinTTL.
func (p *Platform) validOptinToken(st *stage, tok string, now time.Time) bool {
	ts, sig, ok := strings.Cut(tok, ".")
	if !ok {
		return false
	}
	n, err := strconv.ParseInt(ts, 36, 64)
	if err != nil {
		return false
	}
	at := time.Unix(n, 0)
	if at.After(now.Add(time.Minute)) || now.Sub(at) > waOptinTTL {
		return false
	}
	return hmac.Equal([]byte(sig), []byte(p.optinMAC(st, ts)))
}

// optinWording is the box's label. It names the business, as Meta's opt-in
// rules require, and how to stop.
func optinWording(name, lang string) string {
	if lang == "ar" {
		return "أرسلوا لي تحديثات طلبي على واتساب من " + name + " (عبر Renoxium). أرسلوا STOP في أي وقت لإيقافها."
	}
	return "Send me updates about my request on WhatsApp from " + name + " (sent via Renoxium). Reply STOP any time to stop them."
}

// injectOptin replaces each opt-in marker with the platform's box: an
// unticked checkbox, its label and a signed field that proves the box was
// shown by this app.
func (p *Platform) injectOptin(st *stage, page []byte) []byte {
	if !bytes.Contains(page, []byte("<rx-whatsapp-optin")) {
		return page
	}
	name := p.businessName(context.Background(), st)
	tok := p.optinToken(st, time.Now())
	return optinMarker.ReplaceAllFunc(page, func(m []byte) []byte {
		lang := "en"
		if sub := optinMarker.FindSubmatch(m); len(sub) > 1 && string(sub[1]) == "ar" {
			lang = "ar"
		}
		return []byte(`<label class="rx-wa-optin"><input type="checkbox" name="` + optinField + `" value="1"> <span>` +
			html.EscapeString(optinWording(name, lang)) + `</span></label><input type="hidden" name="` + optinTokenField + `" value="` + tok + `">`)
	})
}

// businessName is the name customers see: the brand row's name, or the
// app's own name.
func (p *Platform) businessName(ctx context.Context, st *stage) string {
	if name, _ := p.brandOf(ctx, st, "en"); name != "" {
		return name
	}
	return st.app
}

// reviewNames are words a small business's name should not carry in a
// message sent from the platform number: they are how phishing poses as a
// bank, a government office, a courier or WhatsApp itself.
var reviewNames = regexp.MustCompile(`(?i)\b(bank|ministry|government|police|municipality|customs|immigration|dewa|sewa|addc|etisalat|aramex|dhl|fedex|emirates post|uae ?pass|whatsapp|facebook|instagram|renoxium|paypal|mastercard|adcb|emirates nbd|mashreq)\b`)

// ---- whatsapp.consent and whatsapp.status

type waPhoneInput struct {
	Phone string `json:"phone"`
}

// whatsappConsent answers whatsapp.consent({phone}): it records that the
// customer agreed to WhatsApp messages from this app. Only a visitor's form
// post with the platform's opt-in box ticked can do that.
func (h *scriptHost) whatsappConsent(ctx context.Context, arg any) (any, error) {
	var in waPhoneInput
	if err := decodeArg(arg, &in); err != nil || in.Phone == "" {
		return nil, errors.New("whatsapp.consent expects {phone}")
	}
	p, st := h.p, h.st
	phone, ok := whatsapp.Normalize(in.Phone)
	if !ok {
		return map[string]any{"consented": false, "reason": "bad_number"}, nil
	}
	if !h.waOptin {
		return map[string]any{"consented": false, "reason": "box_not_ticked"}, nil
	}
	var optedOut int64 = -1
	st.admin.QueryRowContext(ctx, `SELECT opted_out_at FROM rx_wa_consent WHERE phone = ? AND consented_at > 0`, phone).Scan(&optedOut)
	if optedOut != 0 { // not consenting yet: a new number counts against the day's limit
		if !p.allowDurable("wa-consent:"+st.app+":"+st.name, waNewConsentsPerDay[p.planOf(p.ownerOf(st.app))], 24*time.Hour) {
			p.event(st, "whatsapp", "warn", h.rid, "WhatsApp opt-in of "+MaskPhone(phone)+" not recorded: too many new numbers today")
			return map[string]any{"consented": false, "reason": "limit"}, nil
		}
	}
	var user any
	if h.waUser > 0 {
		user = h.waUser
	}
	name := p.businessName(ctx, st)
	_, err := st.admin.ExecContext(ctx, `INSERT INTO rx_wa_consent (phone, user_id, source, wording, consented_at, opted_out_at)
		VALUES (?, ?, 'optin_box', ?, ?, 0)
		ON CONFLICT (phone) DO UPDATE SET user_id = coalesce(excluded.user_id, user_id), source = excluded.source,
			wording = excluded.wording, consented_at = excluded.consented_at, opted_out_at = 0`,
		phone, user, optinWording(name, "en"), time.Now().Unix())
	if err != nil {
		return nil, fmt.Errorf("whatsapp.consent: %v", err)
	}
	p.event(st, "whatsapp", "info", h.rid, "WhatsApp opt-in recorded for "+MaskPhone(phone))
	return map[string]any{"consented": true}, nil
}

// whatsappStatus answers whatsapp.status({phone}).
func (h *scriptHost) whatsappStatus(ctx context.Context, arg any) (any, error) {
	var in waPhoneInput
	if err := decodeArg(arg, &in); err != nil || in.Phone == "" {
		return nil, errors.New("whatsapp.status expects {phone}")
	}
	phone, ok := whatsapp.Normalize(in.Phone)
	if !ok {
		return map[string]any{"consented": false, "opted_out": false}, nil
	}
	var consented, optedOut int64
	h.st.admin.QueryRowContext(ctx, `SELECT consented_at, opted_out_at FROM rx_wa_consent WHERE phone = ?`, phone).Scan(&consented, &optedOut)
	return map[string]any{"consented": consented > 0 && optedOut == 0, "opted_out": optedOut > 0}, nil
}

// ---- whatsapp.send

type waSendInput struct {
	To   string `json:"to"`
	Kind string `json:"kind"`
	Ref  string `json:"ref"`
	Text string `json:"text"`
	When string `json:"when"`
	Link string `json:"link"`
}

var (
	// waLinkish refuses addresses, long numbers and handles in message text:
	// the only link a message carries is its button to the app itself.
	waLinkish   = regexp.MustCompile(`(?i)(https?:|www\.|\b[a-z0-9-]+\.(com|net|org|ae|io|me|ly|co|xyz|info|biz|link|app|site|online|shop|store|top|click)\b|@[a-z0-9_]|\d[\d \-]{7,}\d)`)
	waPathChars = regexp.MustCompile(`^/[A-Za-z0-9\-._~/?=&%#+]*$`)
)

func (in *waSendInput) clean() error {
	if !contains(CustomerKinds, in.Kind) {
		return fmt.Errorf("whatsapp.send kind must be one of %s", strings.Join(CustomerKinds, ", "))
	}
	in.Ref, in.Text, in.When = whatsapp.NoticeText(in.Ref), whatsapp.NoticeText(in.Text), whatsapp.NoticeText(in.When)
	for _, f := range []struct {
		name, v string
		max     int
	}{{"ref", in.Ref, 40}, {"text", in.Text, 200}, {"when", in.When, 60}} {
		if f.v == "-" && (f.name != "when" || in.Kind == "reminder") {
			return fmt.Errorf("whatsapp.send needs %s", f.name)
		}
		if utf8.RuneCountInString(f.v) > f.max {
			return fmt.Errorf("whatsapp.send %s is longer than %d characters", f.name, f.max)
		}
		if waLinkish.MatchString(f.v) {
			return fmt.Errorf("whatsapp.send %s must not contain web addresses, phone numbers or @handles; the message's View button opens link on the app", f.name)
		}
	}
	if in.Link == "" {
		in.Link = "/"
	}
	if strings.HasPrefix(in.Link, "//") || !waPathChars.MatchString(in.Link) || len(in.Link) > 300 {
		return errors.New("whatsapp.send link must be a path on this app, such as /bookings/12")
	}
	return nil
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// customerParams are a template's body parameters.
func customerParams(name string, in waSendInput) []string {
	if in.Kind == "reminder" {
		return []string{name, in.Ref, in.When, in.Text}
	}
	return []string{name, in.Ref, in.Text}
}

// RenderCustomerText is the message text a customer would read.
func RenderCustomerText(kind string, params []string) string {
	s := CustomerTemplateBodies[kind]
	for i, v := range params {
		s = strings.ReplaceAll(s, "{{"+strconv.Itoa(i+1)+"}}", whatsapp.NoticeText(v))
	}
	return s
}

// whatsappSend answers whatsapp.send({to, kind, ref, text, when, link}).
// Expected refusals are answers, not errors: {sent: false, reason}.
func (h *scriptHost) whatsappSend(ctx context.Context, arg any) (any, error) {
	var in waSendInput
	if err := decodeArg(arg, &in); err != nil || in.To == "" {
		return nil, errors.New("whatsapp.send expects {to, kind, ref, text, when?, link?}")
	}
	if err := in.clean(); err != nil {
		return nil, err
	}
	p, st := h.p, h.st
	phone, ok := whatsapp.Normalize(in.To)
	if !ok {
		return nil, fmt.Errorf("whatsapp.send: %q is not a WhatsApp number with a country code", in.To)
	}
	name := p.businessName(ctx, st)
	params := customerParams(name, in)
	refuse := func(reason, why string) (any, error) {
		p.event(st, "whatsapp", "warn", h.rid, fmt.Sprintf("WhatsApp %s to %s not sent: %s", in.Kind, MaskPhone(phone), why))
		return map[string]any{"sent": false, "reason": reason}, nil
	}
	if reviewNames.MatchString(name) {
		return refuse("name_review", fmt.Sprintf("the business name %q needs a check by Renoxium before WhatsApp messages can carry it; contact support", name))
	}
	var consented, optedOut int64
	st.admin.QueryRowContext(ctx, `SELECT consented_at, opted_out_at FROM rx_wa_consent WHERE phone = ?`, phone).Scan(&consented, &optedOut)
	switch {
	case optedOut > 0:
		return refuse("opted_out", "the customer stopped WhatsApp messages from this app")
	case consented == 0:
		return refuse("no_consent", "the customer has not ticked the WhatsApp opt-in box (whatsapp.consent)")
	}
	id := newMessageID()
	if st.name == Preview {
		text := RenderCustomerText(in.Kind, params)
		p.event(st, "whatsapp", "info", h.rid, fmt.Sprintf("not sent from preview: WhatsApp %s to %s: %q (View opens %s)", in.Kind, phone, text, in.Link))
		p.demoDeliver(ctx, st, DemoMessage{Kind: "whatsapp", To: phone, Subject: "WhatsApp " + in.Kind, Text: text})
		p.waOutbox.add(h.rid, TestWhatsApp{To: phone, Kind: in.Kind, Text: text, Link: in.Link})
		return map[string]any{"sent": false, "preview": true, "id": id}, nil
	}
	if reason := p.WhatsAppPaused(st.app); reason != "" {
		return refuse("paused", "WhatsApp messages of this app are paused: "+reason)
	}
	template := p.cfg.WhatsAppCustomerTemplates[in.Kind]
	if p.cfg.WhatsAppCustomers == nil || template == "" {
		return refuse("not_available", "WhatsApp messages to customers are not set up on this Renoxium server")
	}
	owner := p.ownerOf(st.app)
	plan := p.planOf(owner)
	quota := WhatsAppCustomerPerPlan[plan]
	if p.cfg.OwnerPlan != nil && p.usage(owner, "wa_customer") >= quota {
		return refuse("quota", fmt.Sprintf("this month's %d WhatsApp messages of the plan are used up", quota))
	}
	switch {
	case !p.allow("wa-burst:"+st.app, waBurstPerMinute, time.Minute):
		return refuse("limit", "more than 20 messages in a minute")
	case !p.allowDurable("wa-num:"+st.app+":"+phone, waPerNumberPerDay[plan], 24*time.Hour):
		return refuse("limit", "too many messages to this number today")
	case !p.allowDurable("wa-app-day:"+st.app, max(10, quota/10), 24*time.Hour):
		return refuse("limit", "the app's daily WhatsApp limit was reached")
	case !p.allowDurable("wa-day", p.cfg.Daily.WhatsApp, 24*time.Hour):
		return refuse("limit", "Renoxium's daily WhatsApp limit was reached")
	}
	if p.cfg.OwnerPlan != nil {
		if n := p.addUsage(owner, "wa_customer"); n == quota {
			p.event(st, "whatsapp", "warn", h.rid, fmt.Sprintf("WhatsApp messages to customers: the plan's %d for this month are used up", quota))
			p.mailOwner(context.Background(), st, "WhatsApp messages to customers paused until next month",
				fmt.Sprintf("Your apps sent %d WhatsApp messages to customers this month, the limit of your plan. Until next month they are not sent; emails still are.\n\n"+
					"Upgrade your plan for more: %s/billing", quota, p.cfg.SiteURL))
		}
	}
	now := time.Now().Unix()
	if _, err := p.db.Exec(`INSERT INTO wa_messages (id, app, stage, phone, kind, link, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, 'queued', ?, ?)`,
		id, st.app, st.name, phone, in.Kind, in.Link, now, now); err != nil {
		return nil, fmt.Errorf("whatsapp.send: %v", err)
	}
	msg := whatsapp.Message{Template: template, Language: p.cfg.WhatsAppCustomerLanguage, Body: params, URLParam: id, StopPayload: stopPrefix + id}
	p.background(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		wamid, err := p.cfg.WhatsAppCustomers.SendTemplate(ctx, phone, msg)
		if err != nil {
			slog.Error("whatsapp customer message", "app", st.app, "err", err)
			p.db.Exec(`UPDATE wa_messages SET status = 'failed', error = ?, updated_at = ? WHERE id = ?`, waErrorCode(err), time.Now().Unix(), id)
			p.event(st, "whatsapp", "error", "", fmt.Sprintf("WhatsApp %s to %s could not be delivered; Renoxium has logged the reason", in.Kind, MaskPhone(phone)))
			return
		}
		p.db.Exec(`UPDATE wa_messages SET wamid = ?, status = 'sent', updated_at = ? WHERE id = ?`, wamid, time.Now().Unix(), id)
		p.meterAdd(st.app, st.name, mWhatsApp, 1)
	})
	p.event(st, "whatsapp", "info", h.rid, fmt.Sprintf("WhatsApp %s to %s sent (message %s)", in.Kind, MaskPhone(phone), id))
	return map[string]any{"sent": true, "id": id}, nil
}

func waErrorCode(err error) string {
	var e *whatsapp.Error
	if errors.As(err, &e) {
		return strconv.Itoa(e.Code)
	}
	return "send"
}

// ---- preview outbox for the test tools

// TestWhatsApp is a WhatsApp message preview did not send.
type TestWhatsApp struct {
	To   string `json:"to"`
	Kind string `json:"kind" jsonschema:"confirmation, reminder or update"`
	Text string `json:"text" jsonschema:"the message as the customer would read it"`
	Link string `json:"link" jsonschema:"the app path the message's View button opens"`
}

// waOutbox keeps preview's messages per request ID until a test tool reads
// them; entries nobody reads go after ten minutes.
type waOutbox struct {
	mu sync.Mutex
	m  map[string]waOutboxEntry
}

type waOutboxEntry struct {
	at   time.Time
	msgs []TestWhatsApp
}

func (o *waOutbox) add(rid string, m TestWhatsApp) {
	if rid == "" {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.m == nil {
		o.m = map[string]waOutboxEntry{}
	}
	for k, e := range o.m {
		if time.Since(e.at) > 10*time.Minute {
			delete(o.m, k)
		}
	}
	e := o.m[rid]
	if len(e.msgs) < 50 {
		e.msgs = append(e.msgs, m)
	}
	e.at = time.Now()
	o.m[rid] = e
}

func (o *waOutbox) take(rid string) []TestWhatsApp {
	o.mu.Lock()
	defer o.mu.Unlock()
	e := o.m[rid]
	delete(o.m, rid)
	return e.msgs
}

// ---- pausing an app

// WhatsAppPaused returns why an app's WhatsApp messages are paused, or "".
func (p *Platform) WhatsAppPaused(app string) string {
	var reason string
	p.db.QueryRow(`SELECT reason FROM wa_paused WHERE app = ?`, app).Scan(&reason)
	return reason
}

// ResumeWhatsApp lets a paused app send again (operator).
func (p *Platform) ResumeWhatsApp(app string) error {
	_, err := p.db.Exec(`DELETE FROM wa_paused WHERE app = ?`, app)
	return err
}

// PausedWhatsApp lists the paused apps and why, for the operator.
func (p *Platform) PausedWhatsApp() map[string]string {
	out := map[string]string{}
	rows, err := p.db.Query(`SELECT app, reason FROM wa_paused ORDER BY at DESC LIMIT 200`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var app, reason string
		if rows.Scan(&app, &reason) == nil {
			out[app] = reason
		}
	}
	return out
}

// WhatsAppCustomerUsage is an account's whatsapp.send messages this month
// and its quota.
func (p *Platform) WhatsAppCustomerUsage(owner string) (used, quota int) {
	return p.usage(owner, "wa_customer"), WhatsAppCustomerPerPlan[p.planOf(owner)]
}

// checkOptOutRate pauses an app whose customers stop its messages too often.
func (p *Platform) checkOptOutRate(app string) {
	since := time.Now().Add(-waPauseWindow).Unix()
	var sent, outs int
	p.db.QueryRow(`SELECT count(*) FROM wa_messages WHERE app = ? AND stage = ? AND created_at >= ? AND status NOT IN ('queued', 'failed')`, app, Live, since).Scan(&sent)
	p.db.QueryRow(`SELECT count(*) FROM wa_optouts WHERE app = ? AND at >= ?`, app, since).Scan(&outs)
	if sent < waPauseMinSent || outs*100 <= sent*waPausePercent {
		return
	}
	reason := fmt.Sprintf("%d of %d customers who got a message in the last 7 days stopped them", outs, sent)
	res, err := p.db.Exec(`INSERT OR IGNORE INTO wa_paused (app, reason, at) VALUES (?, ?, ?)`, app, reason, time.Now().Unix())
	if n, _ := rowsAffected(res, err); n == 0 {
		return
	}
	slog.Warn("whatsapp: app paused for opt-outs", "app", app, "sent", sent, "optouts", outs)
	if st, err := p.loadStage(app, Live); err == nil && st != nil {
		p.event(st, "whatsapp", "error", "", "WhatsApp messages to customers are paused: "+reason)
		p.mailOwner(context.Background(), st, "WhatsApp messages to customers paused",
			"Many customers stopped your app's WhatsApp messages ("+reason+"), so Renoxium has paused them to protect the WhatsApp number all apps share. "+
				"Emails are still sent. Reply to this email to ask for them to be turned on again.")
	}
}
