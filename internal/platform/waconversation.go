package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"log/slog"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/arif-rachim/renoxium/internal/appdef"
	"github.com/arif-rachim/renoxium/internal/sandbox"
	"github.com/arif-rachim/renoxium/internal/whatsapp"
)

// WhatsApp conversations (phase 2). A customer's message to the platform
// number is routed to one app. That app's automatic replies answer it, or
// its on_message script, which may reply with whatsapp.reply. Each app keeps
// its own thread per number and its own 24-hour reply window: the window
// Meta keeps is per business number, which every app shares, so Renoxium
// lets an app answer only customers who wrote to that app.

const (
	waWindow          = 24 * time.Hour
	waInPerNumberHour = 30  // messages from one number to one app handled per hour
	waInPerAppMinute  = 120 // messages handled per app per minute
	waRepliesPerDay   = 50  // replies from one app to one number per day
	btnPrefix         = "rx-btn:"
	pickPrefix        = "rx-pick:"
)

func (p *Platform) ensureConversationTables() error {
	_, err := p.db.Exec(`CREATE TABLE IF NOT EXISTS wa_routes (phone TEXT PRIMARY KEY, app TEXT NOT NULL, stage TEXT NOT NULL,
		pending TEXT NOT NULL DEFAULT '', until INTEGER NOT NULL)`)
	return err
}

// acceptsMessages reports whether an app answers customers' messages.
func acceptsMessages(def *appdef.App) bool {
	return def != nil && def.WhatsApp != nil && (def.WhatsApp.OnMessage != "" || len(def.WhatsApp.AutoReplies) > 0)
}

// threadNote records one message of a thread; "in" opens the reply window.
func (p *Platform) threadNote(ctx context.Context, st *stage, phone, name, userID, dir, kind, body string) {
	now := time.Now().Unix()
	in, out := int64(0), int64(0)
	if dir == "in" {
		in = now
	} else {
		out = now
	}
	st.admin.ExecContext(ctx, `INSERT INTO rx_wa_threads (phone, user_id, name, last_in_at, last_out_at, created_at) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (phone) DO UPDATE SET user_id = CASE WHEN excluded.user_id != '' THEN excluded.user_id ELSE user_id END,
			name = CASE WHEN excluded.name != '' THEN excluded.name ELSE name END,
			last_in_at = max(last_in_at, excluded.last_in_at), last_out_at = max(last_out_at, excluded.last_out_at)`,
		phone, userID, name, in, out, now)
	if r := []rune(body); len(r) > 4096 {
		body = string(r[:4096])
	}
	st.admin.ExecContext(ctx, `INSERT INTO rx_wa_messages (phone, dir, kind, body, at) VALUES (?, ?, ?, ?, ?)`, phone, dir, kind, body, now)
}

// windowUntil is when this app's reply window to phone closes; zero when
// the customer never wrote to the app or the window has closed.
func (p *Platform) windowUntil(ctx context.Context, st *stage, phone string) time.Time {
	var last int64
	st.admin.QueryRowContext(ctx, `SELECT last_in_at FROM rx_wa_threads WHERE phone = ?`, phone).Scan(&last)
	until := time.Unix(last, 0).Add(waWindow)
	if last == 0 || time.Now().After(until) {
		return time.Time{}
	}
	return until
}

// cleanConversations forgets old messages and threads of one stage.
func cleanConversations(ctx context.Context, st *stage) {
	old := time.Now().Add(-waRetention).Unix()
	st.admin.ExecContext(ctx, `DELETE FROM rx_wa_messages WHERE at < ?`, old)
	st.admin.ExecContext(ctx, `DELETE FROM rx_wa_threads WHERE max(last_in_at, last_out_at, created_at) < ?`, old)
}

// ---- inbound: routing on the platform number

// incoming is a customer's message as an app sees it.
type incoming struct {
	Phone, Name, UserID string
	Type                string // text, button or media
	Text                string
	ButtonID, ButtonTit string
}

var refPattern = regexp.MustCompile(`\s*\(ref ([a-z0-9][a-z0-9-]{0,62})\)\s*`)

// routeInbound finds the app a message is for and hands it over. It is
// called for messages that are not opt-outs.
func (p *Platform) routeInbound(ctx context.Context, m whatsapp.Inbound, phone string) {
	in := incoming{Phone: phone, Name: m.Name, UserID: m.FromUserID, Type: m.Type, Text: strings.TrimSpace(m.Text.Body)}
	if m.Type != "text" && m.Type != "interactive" {
		in.Type, in.Text = "media", ""
	}
	now := time.Now()
	setRoute := func(a appStage, pending string) {
		p.db.Exec(`INSERT INTO wa_routes (phone, app, stage, pending, until) VALUES (?, ?, ?, ?, ?)
			ON CONFLICT (phone) DO UPDATE SET app = excluded.app, stage = excluded.stage, pending = excluded.pending, until = excluded.until`,
			phone, a.app, a.stage, pending, now.Add(waWindow).Unix())
	}
	if m.Type == "interactive" {
		id := m.Interactive.ButtonReply.ID
		switch {
		case strings.HasPrefix(id, btnPrefix): // a button an app sent with whatsapp.reply
			msgID, btn, _ := strings.Cut(strings.TrimPrefix(id, btnPrefix), ":")
			var a appStage
			var to string
			if p.db.QueryRow(`SELECT app, stage, phone FROM wa_messages WHERE id = ?`, msgID).Scan(&a.app, &a.stage, &to) != nil || to != phone {
				return
			}
			in.Type, in.ButtonID, in.ButtonTit, in.Text = "button", btn, m.Interactive.ButtonReply.Title, m.Interactive.ButtonReply.Title
			setRoute(a, "")
			p.deliverLive(ctx, a, in)
		case strings.HasPrefix(id, pickPrefix): // the answer to "which business?"
			app := strings.TrimPrefix(id, pickPrefix)
			var pending string
			var a appStage
			if p.db.QueryRow(`SELECT app, stage, pending FROM wa_routes WHERE phone = ?`, phone).Scan(&a.app, &a.stage, &pending) != nil {
				return
			}
			ok := false
			for _, r := range p.recentApps(phone) {
				ok = ok || r.app == app
			}
			if !ok {
				return
			}
			a = appStage{app, Live}
			setRoute(a, "")
			if pending != "" {
				in.Type, in.Text = "text", pending
			} else {
				in.Type, in.Text = "text", m.Interactive.ButtonReply.Title
			}
			p.deliverLive(ctx, a, in)
		}
		return
	}
	// A "Chat on WhatsApp" link of an app starts the message with its reference.
	if sub := refPattern.FindStringSubmatch(in.Text); sub != nil {
		if st, err := p.loadStage(sub[1], Live); err == nil && st != nil {
			in.Text = strings.TrimSpace(refPattern.ReplaceAllString(in.Text, " "))
			a := appStage{sub[1], Live}
			setRoute(a, "")
			p.deliverLive(ctx, a, in)
			return
		}
	}
	var a appStage
	var until int64
	if p.db.QueryRow(`SELECT app, stage, until FROM wa_routes WHERE phone = ? AND app != ''`, phone).Scan(&a.app, &a.stage, &until) == nil && until > now.Unix() {
		setRoute(a, "")
		p.deliverLive(ctx, a, in)
		return
	}
	apps := p.recentApps(phone)
	switch {
	case len(apps) == 1:
		setRoute(apps[0], "")
		p.deliverLive(ctx, apps[0], in)
	case len(apps) > 1:
		// Ask once which business the message is for, and keep the message.
		if !p.allowDurable("wa-pick:"+phone, 3, time.Hour) {
			return
		}
		var buttons []whatsapp.Button
		for _, r := range apps {
			if len(buttons) == 3 {
				break
			}
			st, err := p.loadStage(r.app, r.stage)
			if err != nil || st == nil {
				continue
			}
			buttons = append(buttons, whatsapp.Button{ID: pickPrefix + r.app, Title: truncRunes(p.businessName(ctx, st), 20)})
		}
		p.db.Exec(`INSERT INTO wa_routes (phone, app, stage, pending, until) VALUES (?, '', '', ?, ?)
			ON CONFLICT (phone) DO UPDATE SET app = '', stage = '', pending = excluded.pending, until = excluded.until`, phone, truncRunes(in.Text, 1000), now.Add(time.Hour).Unix())
		if p.cfg.WhatsAppCustomers != nil && p.allowDurable("wa-day", p.cfg.Daily.WhatsApp, 24*time.Hour) {
			if _, err := p.cfg.WhatsAppCustomers.SendButtons(ctx, phone, "Which business is your message for?", buttons); err != nil {
				slog.Error("whatsapp pick", "err", err)
			}
		}
	default:
		if p.allowDurable("wa-answer:"+phone, 1, 24*time.Hour) {
			p.waReply(ctx, phone, "This WhatsApp number only sends updates for businesses that use Renoxium, and replies here are not read.")
		}
	}
}

func truncRunes(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

// deliverLive hands a routed message to a live app, or gives the fixed
// answer when the app does not take messages.
func (p *Platform) deliverLive(ctx context.Context, a appStage, in incoming) {
	st, err := p.loadStage(a.app, a.stage)
	if err != nil || st == nil {
		return
	}
	if !acceptsMessages(st.def) {
		if p.allowDurable("wa-answer:"+in.Phone, 1, 24*time.Hour) {
			p.waReply(ctx, in.Phone, "This WhatsApp number only sends updates from "+p.businessName(ctx, st)+
				", and replies here are not read. To contact them, use the details on "+p.URL(a.app, a.stage)+" . Reply STOP to stop these messages.")
		}
		return
	}
	if !p.allow("wa-in:"+a.app+":"+in.Phone, waInPerNumberHour, time.Hour) || !p.allow("wa-in-app:"+a.app, waInPerAppMinute, time.Minute) {
		p.event(st, "whatsapp", "warn", "", "a WhatsApp message from "+MaskPhone(in.Phone)+" was not handled: too many messages")
		return
	}
	p.handleIncoming(ctx, st, in, newRequestID())
}

// handleIncoming records a message in the app's thread and answers it:
// the first automatic reply that applies, else the on_message script.
// It returns what the script answered, for the test tool.
func (p *Platform) handleIncoming(ctx context.Context, st *stage, in incoming, rid string) (string, error) {
	body := in.Text
	if in.Type == "media" {
		body = "(a photo, file or other media; Renoxium does not keep it)"
	}
	p.threadNote(ctx, st, in.Phone, in.Name, in.UserID, "in", in.Type, body)
	what := "WhatsApp message from " + MaskPhone(in.Phone)
	if st.name == Preview {
		what = fmt.Sprintf("WhatsApp message from %s: %q", in.Phone, body)
	}
	w := st.def.WhatsApp
	if w == nil {
		return "the app has no whatsapp section, so live customers get Renoxium's fixed answer", nil
	}
	now := time.Now()
	for i, r := range w.AutoReplies {
		if !autoReplyApplies(r, in, now) {
			continue
		}
		// The same answer at most once in 10 minutes, so two bots never loop.
		if !p.allowDurable(fmt.Sprintf("wa-auto:%s:%s:%s:%d", st.app, st.name, in.Phone, i), 1, 10*time.Minute) {
			p.event(st, "whatsapp", "info", rid, what+": automatic reply "+fmt.Sprint(i+1)+" already sent in the last 10 minutes")
			return "", nil
		}
		p.event(st, "whatsapp", "info", rid, what+": answered by automatic reply "+fmt.Sprint(i+1))
		res, err := p.waSendReply(ctx, st, rid, in.Phone, waReplyInput{Text: r.Reply}, "auto")
		return fmt.Sprint(res), err
	}
	if w.OnMessage == "" {
		p.event(st, "whatsapp", "info", rid, what+": no automatic reply applies and the app has no on_message script")
		return "no automatic reply applies and the app has no on_message script", nil
	}
	var consented, optedOut int64
	st.admin.QueryRowContext(ctx, `SELECT consented_at, opted_out_at FROM rx_wa_consent WHERE phone = ?`, in.Phone).Scan(&consented, &optedOut)
	var thread struct{ status string }
	thread.status = "bot"
	st.admin.QueryRowContext(ctx, `SELECT status FROM rx_wa_threads WHERE phone = ?`, in.Phone).Scan(&thread.status)
	var button any
	if in.ButtonID != "" {
		button = map[string]any{"id": in.ButtonID, "title": in.ButtonTit}
	}
	reqBody := map[string]any{"from": in.Phone, "name": in.Name, "type": in.Type, "text": in.Text, "button": button,
		"thread":    map[string]any{"status": thread.status, "window_until": now.Add(waWindow).UTC().Format("2006-01-02 15:04:05")},
		"consented": consented > 0 && optedOut == 0}
	// Anyone with WhatsApp can start this script: like public tools it may
	// not move money.
	host := &scriptHost{st: st, p: p, rid: rid, noMoney: true}
	defer host.runDeferred(ctx)
	release, err := p.acquireSlot(ctx, st)
	if err != nil {
		p.event(st, "whatsapp", "error", rid, what+": on_message not run: "+err.Error())
		return "", err
	}
	defer release()
	req := sandbox.Request{Method: "WHATSAPP", Path: "/_rx/whatsapp", Origin: p.URL(st.app, st.name), Body: reqBody,
		Params: map[string]string{}, Query: map[string]string{}, Form: map[string]string{}, FormAll: map[string][]string{}}
	resp, err := p.runScript(ctx, st, "whatsapp on_message", st.def.Scripts[w.OnMessage], req, host)
	if err != nil {
		var se *sandbox.ScriptError
		if errors.As(err, &se) {
			err = fmt.Errorf("script %s: %s", w.OnMessage, se.Message)
		}
		p.event(st, "whatsapp", "error", rid, what+": on_message failed: "+err.Error())
		return "", err
	}
	text := strings.TrimSpace(hookText(resp))
	p.event(st, "whatsapp", "info", rid, what+": on_message "+w.OnMessage+" -> ok")
	if resp.HasJSON {
		raw, _ := json.Marshal(resp.JSON)
		var out struct {
			Reply   string            `json:"reply"`
			Buttons []waReplyButtonIn `json:"buttons"`
		}
		if json.Unmarshal(raw, &out) == nil && strings.TrimSpace(out.Reply) != "" {
			if _, err := p.waSendReply(ctx, st, rid, in.Phone, waReplyInput{Text: out.Reply, Buttons: out.Buttons}, "reply"); err != nil {
				p.event(st, "whatsapp", "error", rid, "the on_message reply was not sent: "+err.Error())
				return text, err
			}
		}
	}
	return text, nil
}

func autoReplyApplies(r appdef.AutoReply, in incoming, now time.Time) bool {
	if r.OutsideHours != nil {
		return !r.OutsideHours.Open(now)
	}
	if in.Type != "text" && in.Type != "button" {
		return false
	}
	words := " " + strings.Join(strings.FieldsFunc(strings.ToLower(in.Text), func(c rune) bool {
		return !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c > 127)
	}), " ") + " "
	for _, m := range r.Match {
		if strings.Contains(words, " "+strings.Join(strings.Fields(m), " ")+" ") {
			return true
		}
	}
	return false
}

// ---- whatsapp.reply

type waReplyButtonIn struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

type waReplyInput struct {
	To      string            `json:"to"`
	Text    string            `json:"text"`
	Buttons []waReplyButtonIn `json:"buttons"`
}

var (
	buttonID = regexp.MustCompile(`^[A-Za-z0-9_\-]{1,40}$`)
	anyURL   = regexp.MustCompile(`(?i)(https?://|www\.)[^\s]+`)
)

// whatsappReply answers whatsapp.reply({to, text, buttons}).
func (h *scriptHost) whatsappReply(ctx context.Context, arg any) (any, error) {
	var in waReplyInput
	if err := decodeArg(arg, &in); err != nil || in.To == "" {
		return nil, errors.New("whatsapp.reply expects {to, text, buttons?}")
	}
	phone, ok := whatsapp.Normalize(in.To)
	if !ok {
		return nil, fmt.Errorf("whatsapp.reply: %q is not a WhatsApp number with a country code", in.To)
	}
	return h.p.waSendReply(ctx, h.st, h.rid, phone, in, "reply")
}

// waSendReply sends free text, with up to three buttons, inside this app's
// window with the customer. Expected refusals are answers.
func (p *Platform) waSendReply(ctx context.Context, st *stage, rid, phone string, in waReplyInput, kind string) (map[string]any, error) {
	in.Text = strings.TrimSpace(in.Text)
	if in.Text == "" || utf8.RuneCountInString(in.Text) > 1000 {
		return nil, errors.New("whatsapp.reply text is required, at most 1000 characters")
	}
	if len(in.Buttons) > 3 {
		return nil, errors.New("whatsapp.reply takes at most 3 buttons")
	}
	for _, b := range in.Buttons {
		if !buttonID.MatchString(b.ID) || strings.TrimSpace(b.Title) == "" || utf8.RuneCountInString(b.Title) > 20 {
			return nil, errors.New("whatsapp.reply buttons are {id, title}: id of letters, digits, - and _ (at most 40), title at most 20 characters")
		}
	}
	own := p.URL(st.app, st.name)
	for _, u := range anyURL.FindAllString(in.Text, -1) {
		if !strings.HasPrefix(u, own+"/") && u != own {
			return nil, fmt.Errorf("whatsapp.reply may only link to the app itself (%s), not %s", own, u)
		}
	}
	refuse := func(reason, why string) (map[string]any, error) {
		p.event(st, "whatsapp", "warn", rid, fmt.Sprintf("WhatsApp reply to %s not sent: %s", MaskPhone(phone), why))
		return map[string]any{"sent": false, "reason": reason}, nil
	}
	if p.windowUntil(ctx, st, phone).IsZero() {
		return refuse("window_closed", "the customer has not written to this app in the last 24 hours; use whatsapp.send with a template")
	}
	var titles []string
	for _, b := range in.Buttons {
		titles = append(titles, b.Title)
	}
	if st.name == Preview {
		p.event(st, "whatsapp", "info", rid, fmt.Sprintf("not sent from preview: WhatsApp %s to %s: %q", kind, phone, in.Text))
		p.waOutbox.add(rid, TestWhatsApp{To: phone, Kind: kind, Text: in.Text, Buttons: titles})
		p.threadNote(ctx, st, phone, "", "", "out", kind, in.Text)
		return map[string]any{"sent": false, "preview": true}, nil
	}
	if reason := p.WhatsAppPaused(st.app); reason != "" {
		return refuse("paused", "WhatsApp messages of this app are paused: "+reason)
	}
	if p.cfg.WhatsAppCustomers == nil {
		return refuse("not_available", "WhatsApp is not set up on this Renoxium server")
	}
	owner := p.ownerOf(st.app)
	quota := WhatsAppCustomerPerPlan[p.planOf(owner)]
	if p.cfg.OwnerPlan != nil && p.usage(owner, "wa_customer") >= quota {
		return refuse("quota", fmt.Sprintf("this month's %d WhatsApp messages of the plan are used up", quota))
	}
	switch {
	case !p.allow("wa-burst:"+st.app, waBurstPerMinute, time.Minute):
		return refuse("limit", "more than 20 messages in a minute")
	case !p.allowDurable("wa-reply:"+st.app+":"+phone, waRepliesPerDay, 24*time.Hour):
		return refuse("limit", "too many replies to this number today")
	case !p.allowDurable("wa-day", p.cfg.Daily.WhatsApp, 24*time.Hour):
		return refuse("limit", "Renoxium's daily WhatsApp limit was reached")
	}
	if p.cfg.OwnerPlan != nil {
		p.addUsage(owner, "wa_customer")
	}
	id := newMessageID()
	now := time.Now().Unix()
	if _, err := p.db.Exec(`INSERT INTO wa_messages (id, app, stage, phone, kind, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, 'queued', ?, ?)`,
		id, st.app, st.name, phone, kind, now, now); err != nil {
		return nil, fmt.Errorf("whatsapp.reply: %v", err)
	}
	p.threadNote(ctx, st, phone, "", "", "out", kind, in.Text)
	var buttons []whatsapp.Button
	for _, b := range in.Buttons {
		buttons = append(buttons, whatsapp.Button{ID: btnPrefix + id + ":" + b.ID, Title: b.Title})
	}
	text := in.Text
	p.background(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		var wamid string
		var err error
		if len(buttons) > 0 {
			wamid, err = p.cfg.WhatsAppCustomers.SendButtons(ctx, phone, text, buttons)
		} else {
			wamid, err = p.cfg.WhatsAppCustomers.SendText(ctx, phone, text)
		}
		if err != nil {
			slog.Error("whatsapp reply", "app", st.app, "err", err)
			p.db.Exec(`UPDATE wa_messages SET status = 'failed', error = ?, updated_at = ? WHERE id = ?`, waErrorCode(err), time.Now().Unix(), id)
			p.event(st, "whatsapp", "error", "", "a WhatsApp reply to "+MaskPhone(phone)+" could not be delivered; Renoxium has logged the reason")
			return
		}
		p.db.Exec(`UPDATE wa_messages SET wamid = ?, status = 'sent', updated_at = ? WHERE id = ?`, wamid, time.Now().Unix(), id)
		p.meterAdd(st.app, st.name, mWhatsApp, 1)
	})
	p.event(st, "whatsapp", "info", rid, fmt.Sprintf("WhatsApp %s to %s sent (message %s)", kind, MaskPhone(phone), id))
	return map[string]any{"sent": true, "id": id}, nil
}

// ---- the "Chat on WhatsApp" link

var chatMarker = regexp.MustCompile(`(?s)<rx-whatsapp-chat>(.*?)</rx-whatsapp-chat>`)

// injectChat turns <rx-whatsapp-chat>label</rx-whatsapp-chat> into a link
// that opens WhatsApp with a first message naming the app, so the platform
// number knows which app the customer means.
func (p *Platform) injectChat(st *stage, page []byte) []byte {
	if !strings.Contains(string(page), "<rx-whatsapp-chat>") {
		return page
	}
	number := strings.TrimPrefix(p.cfg.WhatsAppPlatformNumber, "+")
	name := p.businessName(context.Background(), st)
	return chatMarker.ReplaceAllFunc(page, func(m []byte) []byte {
		label := chatMarker.FindSubmatch(m)[1]
		if number == "" || !acceptsMessages(st.def) {
			return append(append([]byte(`<span class="rx-wa-chat">`), label...), "</span>"...)
		}
		text := "Hi " + name + " (ref " + st.app + ")"
		href := "https://wa.me/" + number + "?text=" + url.QueryEscape(text)
		return append(append([]byte(`<a class="rx-wa-chat" href="`+html.EscapeString(href)+`" target="_blank" rel="noopener">`), label...), "</a>"...)
	})
}

// ---- test tool: a customer's message on preview

// TestWhatsAppInput is a WhatsApp message as a customer would send it.
type TestWhatsAppInput struct {
	From     string `json:"from" jsonschema:"the customer's WhatsApp number with the country code, e.g. +971501234567"`
	Name     string `json:"name,omitempty" jsonschema:"the customer's WhatsApp profile name, default Test Customer"`
	Text     string `json:"text,omitempty" jsonschema:"what the customer writes"`
	ButtonID string `json:"button_id,omitempty" jsonschema:"instead of text: the id of a whatsapp.reply button the customer taps"`
}

// TestWhatsAppMessage runs the preview's automatic replies and on_message
// for a customer's message, as live would, and returns the replies that
// would have been sent. Nothing is sent.
func (p *Platform) TestWhatsAppMessage(ctx context.Context, app string, in TestWhatsAppInput) (TestResponse, error) {
	if _, err := p.Info(app); err != nil {
		return TestResponse{}, err
	}
	st, err := p.loadStage(app, Preview)
	if err != nil || st == nil {
		return TestResponse{}, errors.New("preview has no version yet: call apply_change first")
	}
	phone, ok := whatsapp.Normalize(in.From)
	if !ok {
		return TestResponse{}, fmt.Errorf("from %q is not a WhatsApp number with a country code", in.From)
	}
	msg := incoming{Phone: phone, Name: in.Name, Type: "text", Text: strings.TrimSpace(in.Text)}
	if msg.Name == "" {
		msg.Name = "Test Customer"
	}
	if in.ButtonID != "" {
		msg.Type, msg.ButtonID = "button", in.ButtonID
	} else if msg.Text == "" {
		return TestResponse{}, errors.New("give text or button_id")
	}
	if utf8.RuneCountInString(msg.Text) > 4096 {
		return TestResponse{}, errors.New("text is longer than WhatsApp allows (4096 characters)")
	}
	rid := newRequestID()
	out := TestResponse{Status: 200, ContentType: "text/plain; charset=utf-8", RequestID: rid}
	text, err := p.handleIncoming(ctx, st, msg, rid)
	if err != nil {
		out.Status, text = 500, err.Error()
	}
	out.Body = text
	out.WhatsApp = p.waOutbox.take(rid)
	out.Logs = p.requestLogs(app, rid)
	return out, nil
}
