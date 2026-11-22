package platform_test

import (
	"context"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/arif-rachim/renoxium/internal/appdef"
	"github.com/arif-rachim/renoxium/internal/platform"
	"github.com/arif-rachim/renoxium/internal/sandbox"
)

const waInScript = `function handle(req) {
  const m = req.body;
  db.exec('INSERT INTO inbox (phone, text, kind) VALUES (?, ?, ?)', m.from, m.text, m.type);
  if (m.type === 'button' && m.button.id === 'book') return {json: {reply: 'Booked for you, ' + m.name + '.'}};
  if (m.text === 'menu') return {json: {reply: 'What would you like?', buttons: [{id: 'book', title: 'Book'}, {id: 'agent', title: 'Talk to us'}]}};
  if (m.text === 'link') { try { whatsapp.reply({to: m.from, text: 'See https://evil.example/x'}); } catch (e) { return {json: {reply: 'refused: ' + (String(e).indexOf('may only link to the app itself') >= 0)}}; } }
  return {json: {reply: 'Thanks, we got: ' + m.text}};
}`

const waNudgeScript = `function handle(req) { return {json: whatsapp.reply({to: req.form.phone, text: 'Hello again'})} }`

func convApp(name, hello string) *appdef.App {
	def := waApp(name)
	def.Tables = append(def.Tables, appdef.Table{Name: "inbox", Columns: []appdef.Column{{Name: "phone", Type: "text"}, {Name: "text", Type: "text"}, {Name: "kind", Type: "text"}}})
	def.Scripts["wa_in"] = waInScript
	def.Scripts["nudge"] = waNudgeScript
	def.Routes = append(def.Routes, appdef.Route{Method: "POST", Path: "/nudge", Script: "nudge"})
	def.Templates["form"] = strings.Replace(def.Templates["form"], "</form>", "</form><rx-whatsapp-chat>Chat with us</rx-whatsapp-chat>", 1)
	def.WhatsApp = &appdef.WhatsApp{OnMessage: "wa_in", AutoReplies: []appdef.AutoReply{
		{Match: []string{"hours", "opening time"}, Reply: "We are open 10:00 to 22:00."},
		{Match: []string{hello}, Reply: "Hi from " + name + "!"},
	}}
	return def
}

func setupConvApp(t *testing.T, p *platform.Platform, app, business, hello string) {
	t.Helper()
	ctx := context.Background()
	if _, err := p.CreateApp("usr_wa", app); err != nil {
		t.Fatal(err)
	}
	if res, err := p.ApplyChange(ctx, app, convApp(business, hello)); err != nil || len(res.Problems) > 0 {
		t.Fatalf("apply: %v %v", err, res.Problems)
	}
	if _, err := p.Publish(app, 0); err != nil {
		t.Fatal(err)
	}
}

func waText(from, wamid, body string) map[string]any {
	return map[string]any{"from": from, "id": wamid, "type": "text", "text": map[string]any{"body": body}}
}

func tap(from, wamid, id, title string) map[string]any {
	return map[string]any{"from": from, "id": wamid, "type": "interactive",
		"interactive": map[string]any{"type": "button_reply", "button_reply": map[string]any{"id": id, "title": title}}}
}

func lastText(t *testing.T, box *customerBox) string {
	t.Helper()
	_, texts := box.sent()
	if len(texts) == 0 {
		t.Fatal("no reply sent")
	}
	return texts[len(texts)-1]
}

var buttonIDs = regexp.MustCompile(`(rx-(?:btn|pick):[^=\s]+)=([^|\]]+)`)

func TestWhatsAppConversation(t *testing.T) {
	p, box := newWAPlatform(t, "pro")
	setupConvApp(t, p, "oud", "Oud Al Noor", "salam")
	host := "oud.renoxium.test"
	const from = "971501234567"

	// The customer got a confirmation from oud, so their messages go to oud.
	book(t, p, host, url.Values{"phone": {"+" + from}, "rx_wa_optin": {"1"}, "rx_wa_optin_token": {formToken(t, p, host)}})
	p.WaitBackground()

	webhook(t, p, "100200300", waText(from, "c1", "What are your opening time today?"))
	if got := lastText(t, box); got != "+971501234567 We are open 10:00 to 22:00." {
		t.Fatalf("automatic reply = %q", got)
	}
	webhook(t, p, "100200300", waText(from, "c2", "menu"))
	got := lastText(t, box)
	ids := buttonIDs.FindAllStringSubmatch(got, -1)
	if !strings.HasPrefix(got, "+971501234567 What would you like? [") || len(ids) != 2 || strings.TrimSpace(ids[0][2]) != "Book" || !strings.HasSuffix(ids[0][1], ":book") {
		t.Fatalf("buttons = %q", got)
	}
	// The tap comes back to the same app with the button's id.
	webhook(t, p, "100200300", tap(from, "c3", ids[0][1], "Book"))
	if got := lastText(t, box); got != "+971501234567 Booked for you, ." {
		t.Fatalf("after the tap: %q", got)
	}
	// A tap on another customer's button is ignored.
	n := len(func() []string { _, x := box.sent(); return x }())
	webhook(t, p, "100200300", tap("971509999999", "c4", ids[0][1], "Book"))
	if _, x := box.sent(); len(x) != n {
		t.Fatalf("a stranger's tap was answered: %q", x[len(x)-1])
	}
	// Replies link only to the app itself.
	webhook(t, p, "100200300", waText(from, "c5", "link"))
	if got := lastText(t, box); got != "+971501234567 refused: true" {
		t.Fatalf("link reply = %q", got)
	}

	// The reply window: a number that wrote may get whatsapp.reply; another may not.
	if r := do(t, p, "POST", host, "/nudge", url.Values{"phone": {"+" + from}}); !strings.Contains(r.body, `"sent":true`) {
		t.Fatalf("nudge in the window: %s", r.body)
	}
	if r := do(t, p, "POST", host, "/nudge", url.Values{"phone": {"+971507777777"}}); !strings.Contains(r.body, `"reason":"window_closed"`) {
		t.Fatalf("nudge outside the window: %s", r.body)
	}

	// A second app wrote to the same number: the platform asks which business, and keeps the message.
	setupConvApp(t, p, "rose", "Rose Salon", "hello")
	rhost := "rose.renoxium.test"
	book(t, p, rhost, url.Values{"phone": {"+" + from}, "rx_wa_optin": {"1"}, "rx_wa_optin_token": {formToken(t, p, rhost)}})
	p.WaitBackground()
	// oud's route from the last messages is still fresh: the next message goes to oud.
	webhook(t, p, "100200300", waText(from, "c6", "salam"))
	if got := lastText(t, box); got != "+971501234567 Hi from Oud Al Noor!" {
		t.Fatalf("routed by the last conversation: %q", got)
	}

	// A new number that has two apps writing to it, and no conversation yet.
	const two = "971502222222"
	for _, h := range []string{host, rhost} {
		book(t, p, h, url.Values{"phone": {"+" + two}, "rx_wa_optin": {"1"}, "rx_wa_optin_token": {formToken(t, p, h)}})
	}
	p.WaitBackground()
	webhook(t, p, "100200300", waText(two, "d1", "hello"))
	got = lastText(t, box)
	picks := buttonIDs.FindAllStringSubmatch(got, -1)
	if !strings.HasPrefix(got, "+971502222222 Which business is your message for?") || len(picks) != 2 {
		t.Fatalf("pick = %q", got)
	}
	var rose string
	for _, m := range picks {
		if strings.TrimSpace(m[2]) == "Rose Salon" {
			rose = m[1]
		}
	}
	webhook(t, p, "100200300", tap(two, "d2", rose, "Rose Salon"))
	if got := lastText(t, box); got != "+971502222222 Hi from Rose Salon!" {
		t.Fatalf("after choosing Rose: %q", got)
	}
	webhook(t, p, "100200300", tap(two, "d3", "rx-pick:elsewhere", "Elsewhere"))
	if got := lastText(t, box); got != "+971502222222 Hi from Rose Salon!" {
		t.Fatalf("an app that never wrote was chosen: %q", got)
	}

	// The chat link names the app, so a stranger's first message reaches it.
	page := do(t, p, "GET", host, "/", nil).body
	if !strings.Contains(page, `href="https://wa.me/971500000000?text=Hi+Oud+Al+Noor+%28ref+oud%29"`) {
		t.Fatalf("chat link: %s", page)
	}
	webhook(t, p, "100200300", waText("971503333333", "e1", "Hi Oud Al Noor (ref oud) do you deliver?"))
	if got := lastText(t, box); got != "+971503333333 Thanks, we got: Hi Oud Al Noor do you deliver?" {
		t.Fatalf("first message from the chat link: %q", got)
	}
	// Replies count towards the month's messages.
	if used, _ := p.WhatsAppCustomerUsage("usr_wa"); used < 10 {
		t.Errorf("usage = %d", used)
	}
	// The app's live log masks the numbers and keeps no message text.
	logs, _ := p.Logs("oud", logFilterAll())
	for _, e := range logs {
		if strings.Contains(e.Message, "501234567") || strings.Contains(e.Message, "opening time") {
			t.Errorf("live log shows a number or a message: %s", e.Message)
		}
	}
}

func TestWhatsAppConversationPreview(t *testing.T) {
	p, box := newWAPlatform(t, "free")
	ctx := context.Background()
	p.CreateApp("usr_wa", "oud")
	if res, err := p.ApplyChange(ctx, "oud", convApp("Oud Al Noor", "salam")); err != nil || len(res.Problems) > 0 {
		t.Fatalf("apply: %v %v", err, res.Problems)
	}
	send := func(in platform.TestWhatsAppInput) platform.TestResponse {
		t.Helper()
		out, err := p.TestWhatsAppMessage(ctx, "oud", in)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	if out := send(platform.TestWhatsAppInput{From: "0501234567", Text: "HOURS?"}); len(out.WhatsApp) != 1 || out.WhatsApp[0].Kind != "auto" ||
		out.WhatsApp[0].Text != "We are open 10:00 to 22:00." {
		t.Fatalf("auto reply = %+v", out)
	}
	out := send(platform.TestWhatsAppInput{From: "0501234567", Name: "Sara", Text: "menu"})
	if len(out.WhatsApp) != 1 || out.WhatsApp[0].Kind != "reply" || strings.Join(out.WhatsApp[0].Buttons, ",") != "Book,Talk to us" {
		t.Fatalf("menu = %+v", out)
	}
	if !anyLogEvent(out, `WhatsApp message from +971501234567: "menu"`) {
		t.Errorf("preview log lacks the message: %+v", out.Logs)
	}
	if out := send(platform.TestWhatsAppInput{From: "0501234567", Name: "Sara", ButtonID: "book"}); len(out.WhatsApp) != 1 || out.WhatsApp[0].Text != "Booked for you, Sara." {
		t.Fatalf("button = %+v", out)
	}
	if _, err := p.TestWhatsAppMessage(ctx, "oud", platform.TestWhatsAppInput{From: "nope", Text: "x"}); err == nil {
		t.Error("a bad number was accepted")
	}
	p.WaitBackground()
	if sent, texts := box.sent(); len(sent)+len(texts) != 0 {
		t.Fatalf("preview sent %v %v", sent, texts)
	}
}

func anyLogEvent(out platform.TestResponse, want string) bool {
	for _, e := range out.Logs {
		if strings.Contains(e.Message, want) {
			return true
		}
	}
	return false
}

func TestWhatsAppDefinitionChecks(t *testing.T) {
	base := func(w *appdef.WhatsApp) []string {
		def := &appdef.App{Scripts: map[string]string{"in": "function handle(req) { return {text: ''} }"}, Templates: map[string]string{"t": "x"},
			Routes: []appdef.Route{{Method: "GET", Path: "/", Template: "t"}}, WhatsApp: w}
		return def.Validate()
	}
	if p := base(&appdef.WhatsApp{OnMessage: "in", AutoReplies: []appdef.AutoReply{{Match: []string{"hours"}, Reply: "10-22"},
		{OutsideHours: &appdef.Hours{Days: []string{"mon", "tue"}, From: "09:00", To: "18:00"}, Reply: "Closed now"}}}); len(p) > 0 {
		t.Fatalf("valid section refused: %v", p)
	}
	for _, w := range []*appdef.WhatsApp{
		{OnMessage: "missing"},
		{AutoReplies: []appdef.AutoReply{{Match: []string{"Hours"}, Reply: "x"}}},
		{AutoReplies: []appdef.AutoReply{{Match: []string{"hours"}}}},
		{AutoReplies: []appdef.AutoReply{{Reply: "x"}}},
		{AutoReplies: []appdef.AutoReply{{Match: []string{"a"}, OutsideHours: &appdef.Hours{Days: []string{"mon"}, From: "09:00", To: "18:00"}, Reply: "x"}}},
		{AutoReplies: []appdef.AutoReply{{OutsideHours: &appdef.Hours{Days: []string{"monday"}, From: "09:00", To: "18:00"}, Reply: "x"}}},
		{AutoReplies: []appdef.AutoReply{{OutsideHours: &appdef.Hours{Days: []string{"mon"}, From: "18:00", To: "09:00"}, Reply: "x"}}},
		{AutoReplies: []appdef.AutoReply{{OutsideHours: &appdef.Hours{Days: []string{"mon"}, From: "09:00", To: "18:00", Timezone: "Mars/Base"}, Reply: "x"}}},
	} {
		if p := base(w); len(p) == 0 {
			t.Errorf("%+v accepted", w)
		}
	}
	h := appdef.Hours{Days: []string{"mon"}, From: "09:00", To: "18:00"}
	monday := time.Date(2026, 11, 23, 6, 0, 0, 0, time.UTC) // 10:00 in Dubai
	if !h.Open(monday) || h.Open(monday.Add(9*time.Hour)) || h.Open(monday.AddDate(0, 0, 1)) {
		t.Error("opening hours are wrong")
	}
}

// Without a platform number the chat marker keeps only its label.
func TestWhatsAppChatWithoutNumber(t *testing.T) {
	p, err := platform.New(platform.Config{DataDir: t.TempDir(), AppsDomain: "renoxium.test", PublicScheme: "https"}, sandbox.NewGoja())
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	setupConvApp(t, p, "oud", "Oud Al Noor", "salam")
	page := do(t, p, "GET", "oud.renoxium.test", "/", nil).body
	if !strings.Contains(page, `<span class="rx-wa-chat">Chat with us</span>`) || strings.Contains(page, "wa.me") {
		t.Fatalf("page = %s", page)
	}
}
