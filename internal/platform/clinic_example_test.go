package platform_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/arif-rachim/renoxium/internal/appdef"
	"github.com/arif-rachim/renoxium/internal/platform"
)

// clinicEnv is the clinic-queue-uae example open around the clock, so the
// tests do not depend on the time they run at.
func clinicEnv(t *testing.T, change func(c map[string]any)) *exampleEnv {
	return newExampleEnv(t, "clinic-queue-uae", func(def *appdef.App) {
		c := def.Seed["clinic"][0]
		c["opens"], c["closes"], c["last_ticket_minutes"] = "00:00", "24:00", 0
		if change != nil {
			change(c)
		}
	})
}

// take sends the take-a-number form and returns the ticket's secret.
func (e *exampleEnv) take(service, name, phone, email string) string {
	e.t.Helper()
	r := e.post("/take", "", map[string]string{"service": service, "name": name, "phone": phone, "email": email, "lang": "en"})
	if r.Status != 303 || !strings.HasPrefix(r.Location, "/ticket?k=") {
		e.t.Fatalf("take %s: %d %s", name, r.Status, r.Location)
	}
	return strings.TrimPrefix(r.Location, "/ticket?k=")
}

// appLog is everything the preview logged so far, messages not sent included.
func (e *exampleEnv) appLog() string {
	e.t.Helper()
	logs, err := e.p.Logs(e.app, logFilterAll())
	if err != nil {
		e.t.Fatal(err)
	}
	var b strings.Builder
	for _, l := range logs {
		b.WriteString(l.Message + "\n")
	}
	return b.String()
}

func dubaiToday() string { return time.Now().In(time.FixedZone("GST", 4*3600)).Format("2006-01-02") }

// The whole walk-in flow: patients take numbers and see their place and the
// estimate, staff call them to rooms, the screen and the live channel follow,
// the patient two places from the front is told once on WhatsApp (and by
// email), no-shows and leaving the queue, the admin panel, the nightly job
// and the report.
func TestClinicQueueFlow(t *testing.T) {
	e := clinicEnv(t, nil)
	home := e.get("/", "").Body
	for _, want := range []string{`data-font="boutique"`, "Shifa Family Clinic", "General practice", `href="/s/1"`, "Nobody waiting", "Nobody has been called yet",
		"Taking numbers now, day and night", `class="q-svc"`, `view-transition-name:sttl-1`} {
		if !strings.Contains(home, want) {
			t.Errorf("home lacks %q", want)
		}
	}
	if s := e.get("/s/1", "").Body; !strings.Contains(s, `action="/take"`) || !strings.Contains(s, "when 2 patients are ahead of you") || !strings.Contains(s, `<span class="vtt" style="view-transition-name:sttl-1">General practice</span>`) {
		t.Error("take-a-number page")
	}
	if r := e.get("/s/99", ""); r.Status != 404 {
		t.Errorf("unknown service: %d", r.Status)
	}

	// Wrong forms come back with the visitor's words kept.
	r := e.post("/take", "", map[string]string{"service": "1", "name": "Omar", "phone": "12", "lang": "en"})
	if r.Status != 303 || !strings.Contains(r.Location, "error=error_phone") || !strings.Contains(r.Location, "name=Omar") {
		t.Fatalf("bad phone: %d %s", r.Status, r.Location)
	}
	if r := e.get(r.Location, ""); !strings.Contains(r.Body, "Type your mobile number") || !strings.Contains(r.Body, `value="Omar"`) {
		t.Error("the error or the typed name is not shown")
	}

	k1 := e.take("1", "Omar Saleh", "050 000 1001", "")
	k2 := e.take("1", "Priya Nair", "+971500001002", "")
	k3 := e.take("1", "James Carter", "0500001003", "james@example.com")
	k4 := e.take("1", "Fatma Al Zaabi", "0500001004", "fatma@example.com")
	kd := e.take("2", "Liza Mendoza", "0500001005", "")
	if r := e.post("/take", "", map[string]string{"service": "2", "name": "Omar again", "phone": "+971 50 000 1001", "lang": "en"}); !strings.Contains(r.Location, "error=error_already") {
		t.Errorf("a second number for one mobile: %s", r.Location)
	}
	if row := e.row(`SELECT code, phone, status, source, alerted FROM tickets WHERE token = ?`, k1); row["code"] != "A001" || row["phone"] != "+971500001001" || row["status"] != "waiting" || row["source"] != "phone" || row["alerted"] != "near" {
		t.Fatalf("first ticket: %v", row)
	}
	if row := e.row(`SELECT code, alerted FROM tickets WHERE token = ?`, k4); row["code"] != "A004" || row["alerted"] != "" {
		t.Fatalf("fourth ticket: %v", row)
	}
	if row := e.row(`SELECT code FROM tickets WHERE token = ?`, kd); row["code"] != "B001" {
		t.Fatalf("dental numbers start at B001: %v", row)
	}
	// Three ahead, 12 minutes each, two doctors: 18 minutes.
	tk := e.get("/ticket?k="+k4, "").Body
	for _, want := range []string{"A004", "3 patients ahead of you", "about 18 min", "Fatma Al Zaabi", `data-status="waiting"`, "Emirates ID", `<meta name="robots" content="noindex">`} {
		if !strings.Contains(tk, want) {
			t.Errorf("ticket lacks %q", want)
		}
	}
	if e.get("/ticket?k=nope", "").Status != 404 || e.get("/ticket?k=nope&json=1", "").Status != 404 {
		t.Error("a wrong ticket link is found")
	}
	if r := e.get("/ticket", ""); r.Status != 200 || !strings.Contains(r.Body, "Open the link on the phone you took your number with") {
		t.Errorf("ticket page without a link: %d", r.Status)
	}
	var api map[string]any
	json.Unmarshal([]byte(e.get("/ticket?json=1&k="+k4, "").Body), &api)
	if api["ahead"] != 3.0 || api["code"] != "A004" || api["name"] != nil {
		t.Errorf("api ticket: %v", api)
	}

	// The live channel: the queue as everyone sees it, without names.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Host = "shop--preview.renoxium.test"
		if !e.p.ServeApp(w, r) {
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	defer srv.CloseClientConnections()
	live, code := subscribe(t, srv.URL, "/_rx/live/public:queue")
	if code != 200 {
		t.Fatalf("subscribe: %d", code)
	}
	first := live.next(t)
	if !strings.Contains(first, `"w":["A001","A002","A003","A004"]`) || strings.Contains(first, "Omar") || strings.Contains(first, "0500") {
		t.Fatalf("last board: %s", first)
	}

	// Staff: only the emails on the staff list.
	const nurse = "nurse@shifa.ae"
	e.signup(nurse)
	if r := e.get("/staff", nurse); strings.Contains(r.Body, "Call next patient") || !strings.Contains(r.Body, "not on the clinic&#39;s staff list") {
		t.Fatalf("staff page for a stranger: %d", r.Status)
	}
	if r := e.post("/staff/act", nurse, map[string]string{"action": "next", "room": "1"}); r.Status != 403 {
		t.Fatalf("a stranger called a patient: %d", r.Status)
	}
	e.exec(`INSERT INTO staff (email, name, active) VALUES (?, 'Nurse', 1)`, nurse)
	if s := e.get("/staff", nurse).Body; !strings.Contains(s, "Room 1") || !strings.Contains(s, "Call next patient") || !strings.Contains(s, "Omar Saleh") {
		t.Fatal("staff page")
	}
	act := func(form map[string]string) {
		t.Helper()
		form["ajax"] = "1"
		if r := e.post("/staff/act", nurse, form); r.Status != 200 {
			t.Fatalf("%v: %d %s", form, r.Status, r.Body)
		}
	}
	act(map[string]string{"action": "next", "room": "1"})
	if row := e.row(`SELECT status, room_id, calls, called_at FROM tickets WHERE token = ?`, k1); row["status"] != "called" || num(row["room_id"]) != 1 || num(row["calls"]) != 1 || row["called_at"] == nil {
		t.Fatalf("called: %v", row)
	}
	if b := live.next(t); !strings.Contains(b, `"c":"A001","k":1,"r":"Room 1","ra":"الغرفة 1","s":1`) || !strings.Contains(b, `"w":["A002","A003","A004"]`) {
		t.Fatalf("board after the call: %s", b)
	}
	if s := e.get("/ticket?k="+k1, "").Body; !strings.Contains(s, `data-status="called"`) || !strings.Contains(s, "Room 1") {
		t.Error("the called patient's ticket")
	}
	if s := e.get("/screen", "").Body; !strings.Contains(s, `<b id="tv-code">A001</b><span class="q-room" id="tv-room">Room 1</span>`) || !strings.Contains(s, "<b>A002</b><b>A003</b><b>A004</b>") {
		t.Error("screen after the call")
	}
	// A004 now has two ahead: one WhatsApp message and one email, and only once.
	if row := e.row(`SELECT alerted FROM tickets WHERE token = ?`, k4); row["alerted"] != "whatsapp+email" {
		t.Fatalf("A004 told: %v", row)
	}
	act(map[string]string{"action": "next", "room": "2"})
	if row := e.row(`SELECT status, room_id FROM tickets WHERE token = ?`, k2); row["status"] != "called" || num(row["room_id"]) != 2 {
		t.Fatalf("second call: %v", row)
	}
	if row := e.row(`SELECT alerted FROM tickets WHERE token = ?`, k4); row["alerted"] != "whatsapp+email" {
		t.Fatalf("A004 told twice: %v", row)
	}
	// A room that sees only dentistry calls B001, not the next general practice patient.
	act(map[string]string{"action": "next", "room": "3"})
	if row := e.row(`SELECT status, room_id FROM tickets WHERE token = ?`, kd); row["status"] != "called" || num(row["room_id"]) != 3 {
		t.Fatalf("dental call: %v", row)
	}
	// Room 1 finishes A001 and calls A003; A001 is done with its times.
	act(map[string]string{"action": "next", "room": "1"})
	if row := e.row(`SELECT status, done_at FROM tickets WHERE token = ?`, k1); row["status"] != "done" || row["done_at"] == nil {
		t.Fatalf("finished: %v", row)
	}
	if row := e.row(`SELECT status FROM tickets WHERE token = ?`, k3); row["status"] != "called" {
		t.Fatalf("A003: %v", row)
	}
	id3 := e.row(`SELECT id FROM tickets WHERE token = ?`, k3)["id"]
	act(map[string]string{"action": "recall", "ticket": idText(id3)})
	if row := e.row(`SELECT calls FROM tickets WHERE token = ?`, k3); num(row["calls"]) != 2 {
		t.Fatalf("recall: %v", row)
	}
	act(map[string]string{"action": "no_show", "ticket": idText(id3)})
	if s := e.get("/ticket?k="+k3, "").Body; !strings.Contains(s, "Missed (not there when called)") {
		t.Error("no-show ticket")
	}
	act(map[string]string{"action": "back", "ticket": idText(id3)})
	if row := e.row(`SELECT status FROM tickets WHERE token = ?`, k3); row["status"] != "waiting" {
		t.Fatalf("back to the queue: %v", row)
	}
	// The patient leaves the queue themselves.
	if r := e.post("/ticket/leave", "", map[string]string{"k": k4, "lang": "en"}); r.Status != 303 {
		t.Fatalf("leave: %d", r.Status)
	}
	if s := e.get("/ticket?k="+k4, "").Body; !strings.Contains(s, "You left the queue") {
		t.Error("left ticket")
	}

	// The owner calls from the admin panel: a room that sees the service, the times, the screens.
	id3n := int64(num(id3))
	a, err := e.p.TestRequest(context.Background(), e.app, platform.TestRequestInput{Admin: &platform.TestAdminInput{Action: "update", Table: "tickets", ID: id3n, Values: map[string]any{"status": "called"}}})
	if err != nil {
		t.Fatal(err)
	}
	if row := e.row(`SELECT status, room_id, called_at, wait_minutes FROM tickets WHERE id = ?`, id3n); row["status"] != "called" || num(row["room_id"]) != 1 || row["called_at"] == nil {
		t.Fatalf("admin call: %v %s", row, logText(a))
	}
	// Room 1's previous patient (A003 was called there before) is the same ticket: nobody else is finished by mistake.
	if row := e.row(`SELECT status FROM tickets WHERE token = ?`, k2); row["status"] != "called" {
		t.Fatalf("room 2's patient changed: %v", row)
	}

	// The report and the API for the screens.
	if s := e.get("/staff/report", nurse).Body; !strings.Contains(s, "Patients per day") || !strings.Contains(s, "General practice") {
		t.Error("report")
	}
	var board map[string]any
	json.Unmarshal([]byte(e.get("/api/queue", "").Body), &board)
	if board["day"] != dubaiToday() || board["taken"] != 5.0 {
		t.Errorf("api queue: %v", board)
	}

	// The night job: yesterday's leftovers close, the day's numbers are written, old contacts wiped, the owner told.
	yesterday := time.Now().In(time.FixedZone("GST", 4*3600)).AddDate(0, 0, -1).Format("2006-01-02")
	old := time.Now().In(time.FixedZone("GST", 4*3600)).AddDate(0, 0, -40).Format("2006-01-02")
	for i, st := range []string{"done", "done", "no_show", "waiting", "called"} {
		e.exec(`INSERT INTO tickets (day, number, code, service_id, name, phone, status, token, taken_at, called_at, wait_minutes) VALUES (?, ?, ?, 1, 'Old patient', '+971500009999', ?, 'tok', ?, ?, ?)`,
			yesterday, i+1, "A00"+string(rune('1'+i)), st, yesterday+" 09:00:00", yesterday+" 09:20:00", 20)
	}
	e.exec(`INSERT INTO tickets (day, number, code, service_id, name, phone, status, token, taken_at) VALUES (?, 1, 'A001', 1, 'Very old', '+971500008888', 'done', 'oldtok', ?)`, old, old+" 10:00:00")
	runs, err := e.p.TryScheduledJobs(context.Background(), e.app)
	if err != nil || len(runs) != 1 || runs[0].Status != 200 {
		t.Fatalf("job: %v %+v", err, runs)
	}
	if body := runs[0].Body; !strings.Contains(body, "1 left waiting closed") || !strings.Contains(body, "days summed up") {
		t.Errorf("job answer: %s", body)
	}
	if row := e.row(`SELECT taken, seen, no_shows, left_queue, avg_wait FROM daily_stats WHERE day = ?`, yesterday); num(row["taken"]) != 5 || num(row["seen"]) != 3 || num(row["no_shows"]) != 1 || num(row["left_queue"]) != 1 || num(row["avg_wait"]) != 20 {
		t.Errorf("daily stats: %v", row)
	}
	if row := e.row(`SELECT name, phone, token FROM tickets WHERE day = ?`, old); row["name"] != "" || row["phone"] != "" || row["token"] != "" {
		t.Errorf("old contacts kept: %v", row)
	}
	if row := e.row(`SELECT name FROM tickets WHERE day = ? LIMIT 1`, yesterday); row["name"] != "Old patient" {
		t.Errorf("recent contacts wiped: %v", row)
	}
	if l := e.appLog(); !strings.Contains(l, "Yesterday: 5 patients, average wait 20 min") {
		t.Errorf("owner not told: %s", l)
	}
	// Today's queue is untouched.
	if row := e.row(`SELECT status FROM tickets WHERE token = ?`, k2); row["status"] != "called" {
		t.Errorf("today's queue changed by the job: %v", row)
	}
}

func idText(v any) string {
	return strings.TrimSuffix(strings.TrimSuffix(url.QueryEscape(jsonNum(v)), ".0"), ".00")
}
func jsonNum(v any) string { b, _ := json.Marshal(num(v)); return string(b) }

// The WhatsApp message itself: the patient's language, the code and the estimate, and email as well when given.
func TestClinicQueueMessages(t *testing.T) {
	e := clinicEnv(t, nil)
	e.take("1", "A", "0500002001", "")
	e.take("1", "B", "0500002002", "")
	e.take("1", "C", "0500002003", "")
	r := e.post("/take", "", map[string]string{"service": "1", "name": "Mona", "phone": "0500002004", "email": "mona@example.com", "lang": "ar"})
	if r.Status != 303 {
		t.Fatal(r.Status)
	}
	const nurse = "nurse@shifa.ae"
	e.signup(nurse)
	e.exec(`INSERT INTO staff (email, active) VALUES (?, 1)`, nurse)
	e.post("/staff/act", nurse, map[string]string{"action": "next", "room": "1"})
	log := e.appLog()
	for _, want := range []string{"WhatsApp to +971500002004: التذكرة A004: قبلك 2 مرضى، حوالي 12 دقيقة.", "/ticket?k=", "to mona@example.com"} {
		if !strings.Contains(log, want) {
			t.Errorf("log lacks %q:\n%s", want, log)
		}
	}
	// WhatsApp off: email only; notify_ahead 0: nothing at all.
	quiet := clinicEnv(t, func(c map[string]any) { c["whatsapp_alerts"] = 0 })
	quiet.take("1", "A", "0500003001", "")
	quiet.take("1", "B", "0500003002", "")
	quiet.take("1", "C", "0500003003", "")
	quiet.take("1", "D", "0500003004", "d@example.com")
	quiet.signup(nurse)
	quiet.exec(`INSERT INTO staff (email, active) VALUES (?, 1)`, nurse)
	quiet.post("/staff/act", nurse, map[string]string{"action": "next", "room": "1"})
	if l := quiet.appLog(); strings.Contains(l, "WhatsApp to") || !strings.Contains(l, "to d@example.com") {
		t.Errorf("whatsapp off:\n%s", l)
	}
	off := clinicEnv(t, func(c map[string]any) { c["notify_ahead"] = 0 })
	for _, ph := range []string{"0500004001", "0500004002", "0500004003", "0500004004"} {
		off.take("1", "X", ph, "x"+ph+"@example.com")
	}
	off.signup(nurse)
	off.exec(`INSERT INTO staff (email, active) VALUES (?, 1)`, nurse)
	off.post("/staff/act", nurse, map[string]string{"action": "next", "room": "1"})
	if l := off.appLog(); strings.Contains(l, "WhatsApp to") || strings.Contains(l, "not sent from preview: to x") {
		t.Errorf("messages off:\n%s", l)
	}
}

// Hours, closed days and the daily limit decide whether numbers can be taken.
func TestClinicQueueOpeningHours(t *testing.T) {
	wd := int(time.Now().In(time.FixedZone("GST", 4*3600)).Weekday())
	closed := clinicEnv(t, func(c map[string]any) { c["closed_days"] = string(rune('0' + wd)) })
	if h := closed.get("/", "").Body; !strings.Contains(h, "We are closed today.") {
		t.Error("closed day not shown")
	}
	if r := closed.post("/take", "", map[string]string{"service": "1", "name": "A", "phone": "0500005001"}); !strings.Contains(r.Location, "error=error_closed_today") {
		t.Errorf("number taken on a closed day: %s", r.Location)
	}
	full := clinicEnv(t, func(c map[string]any) { c["daily_limit"] = 1 })
	full.take("1", "A", "0500006001", "")
	if r := full.post("/take", "", map[string]string{"service": "1", "name": "B", "phone": "0500006002"}); !strings.Contains(r.Location, "error=error_full_today") {
		t.Errorf("over the daily limit: %s", r.Location)
	}
	// Normal hours: the home page names them.
	day := newExampleEnv(t, "clinic-queue-uae", nil)
	if h := day.get("/", "").Body; !strings.Contains(h, "08:00 – 22:00") {
		t.Error("opening hours")
	}
}

// Arabic pages: right to left, Arabic texts and names, Western digits.
func TestClinicQueueArabic(t *testing.T) {
	e := clinicEnv(t, nil)
	k := e.take("1", "Mona", "0500007001", "")
	for path, wants := range map[string][]string{
		"/?lang=ar":                   {`dir="rtl"`, "عيادة شفاء العائلية", "اختر الخدمة", "الطب العام", "لا أحد ينتظر"},
		"/s/1?lang=ar":                {`dir="rtl"`, "أعطني رقمي", "رقم الجوال", `dir="ltr"`},
		"/ticket?k=" + k + "&lang=ar": {`dir="rtl"`, "رقمك", "أنت التالي", "A001"},
		"/screen?lang=ar":             {`dir="rtl"`, "يُنادى الآن", "امسح الرمز لتحصل على رقم"},
		"/kiosk?lang=ar":              {`dir="rtl"`, "احصل على رقمك هنا", "طب الأسنان"},
	} {
		body := e.get(path, "").Body
		for _, w := range wants {
			if !strings.Contains(body, w) {
				t.Errorf("%s lacks %q", path, w)
			}
		}
	}
	// The reception tablet: the number shown, then back to the start.
	r := e.post("/take", "", map[string]string{"service": "2", "name": "Ali", "phone": "0500007002", "kiosk": "1", "lang": "ar"})
	if r.Status != 303 || !strings.HasPrefix(r.Location, "/kiosk?k=") || !strings.HasSuffix(r.Location, "&lang=ar") {
		t.Fatalf("kiosk: %d %s", r.Status, r.Location)
	}
	if s := e.get(r.Location, "").Body; !strings.Contains(s, "B001") || !strings.Contains(s, "هذا رقمك") || !strings.Contains(s, "data-qr=") {
		t.Error("kiosk number")
	}
	if row := e.row(`SELECT source, lang FROM tickets WHERE code = 'B001'`); row["source"] != "reception" || row["lang"] != "ar" {
		t.Errorf("kiosk ticket: %v", row)
	}
}

// Read-only tools for patients' assistants.
func TestClinicQueueTools(t *testing.T) {
	e := clinicEnv(t, nil)
	e.take("1", "A", "0500008001", "")
	r, err := e.p.TestRequest(context.Background(), e.app, platform.TestRequestInput{Method: "POST", Path: "/mcp",
		JSON: map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": "queue_status", "arguments": map[string]any{}}}})
	if err != nil || !strings.Contains(r.Body, `\"waiting\":1`) || !strings.Contains(r.Body, "estimated_wait_minutes") || strings.Contains(r.Body, "0500008001") {
		t.Fatalf("queue_status: %v %s", err, r.Body)
	}
}
