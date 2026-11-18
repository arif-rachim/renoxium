package web_test

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/arif-rachim/renoxium/internal/appdef"
)

func publishSalon(e *env, c *http.Client, app string) {
	e.t.Helper()
	e.publishDef(c, app, &appdef.App{
		Tables: []appdef.Table{
			{Name: "staff", Columns: []appdef.Column{{Name: "name", Type: "text"}}},
			{Name: "bookings", Label: "Bookings",
				Calendar: &appdef.Calendar{Date: "date", Start: "time", Duration: "duration_min", EndDate: "end_date", Group: "staff_id",
					Title: []string{"customer_name", "service"}, Status: "status", HideStatus: []string{"cancelled"}},
				Columns: []appdef.Column{
					{Name: "date", Type: "text"}, {Name: "end_date", Type: "text"}, {Name: "time", Type: "text"}, {Name: "duration_min", Type: "integer"},
					{Name: "staff_id", Type: "integer"}, {Name: "customer_name", Type: "text"}, {Name: "service", Type: "text"},
					{Name: "status", Type: "text", Options: []string{"confirmed", "cancelled", "completed"}},
				}},
			{Name: "notes", Columns: []appdef.Column{{Name: "body", Type: "text"}}},
		},
		Seed: map[string][]map[string]any{
			"staff": {{"name": "Aisha"}, {"name": "Huda"}},
			"bookings": {
				{"date": "2026-12-03", "time": "10:00", "duration_min": 60, "staff_id": 1, "customer_name": "Mariam", "service": "Haircut", "status": "confirmed"},
				{"date": "2026-12-03", "time": "10:30", "duration_min": 45, "staff_id": 1, "customer_name": "Noura", "service": "Blow-dry", "status": "completed"},
				{"date": "2026-12-03", "time": "14:00", "duration_min": 90, "staff_id": 2, "customer_name": "Fatima", "service": "Henna", "status": "confirmed"},
				{"date": "2026-12-03", "time": "16:00", "duration_min": 30, "staff_id": 2, "customer_name": "Layla", "service": "Nails", "status": "cancelled"},
				{"date": "2026-12-03", "customer_name": "Walk-in", "service": "Any", "status": "confirmed"},
				{"date": "2026-12-01", "end_date": "2026-12-05", "staff_id": 2, "customer_name": "Bridal", "service": "Package", "status": "confirmed"},
				{"date": "2026-12-10", "time": "09:00", "staff_id": 1, "customer_name": "Next week", "status": "confirmed"},
			},
		},
		Templates: map[string]string{"t": "salon"},
		Routes:    []appdef.Route{{Method: "GET", Path: "/", Template: "t"}},
	})
}

func TestAdminCalendarPage(t *testing.T) {
	e := newEnv(t)
	owner := e.browser()
	e.signup(owner, "salma@salon.ae")
	publishSalon(e, owner, "salma-salon")
	get := func(c *http.Client, path string) (int, string) {
		t.Helper()
		r, err := c.Get(e.base + path)
		if err != nil {
			t.Fatal(err)
		}
		return r.StatusCode, read(t, r)
	}
	cal := "/apps/salma-salon/t/bookings/calendar"

	// The list links to the calendar; tables without one do not.
	if _, page := get(owner, "/apps/salma-salon/t/bookings"); !strings.Contains(page, `href="`+cal+`"`) {
		t.Fatal("list has no calendar tab")
	}
	if _, page := get(owner, "/apps/salma-salon/t/notes"); strings.Contains(page, "/calendar") {
		t.Fatal("calendar tab on a table without calendar")
	}

	// Day view: one lane per staff member plus unassigned, items with times and links back.
	code, page := get(owner, cal+"?view=day&date=2026-12-03")
	if code != 200 {
		t.Fatalf("day: %d %s", code, page)
	}
	for _, want := range []string{"<h2>Aisha</h2>", "<h2>Huda</h2>", "<h2>Not assigned</h2>", "Mariam · Haircut", "10:00–11:00", "10:30–11:15",
		"Fatima · Henna", "14:00–15:30", "Walk-in · Any", "Bridal · Package", "1 Dec → 5 Dec", "Thursday 3 Dec 2026", "Show cancelled",
		`href="/apps/salma-salon/t/bookings/1?back=%2Fapps%2Fsalma-salon%2Ft%2Fbookings%2Fcalendar%3Fdate%3D2026-12-03%26view%3Dday"`,
		`href="/apps/salma-salon/t/bookings/calendar?date=2026-12-02&amp;view=day"`, `href="/apps/salma-salon/t/bookings/calendar?date=2026-12-04&amp;view=day"`,
		`value="2026-12-03"`} {
		if !strings.Contains(page, want) {
			t.Errorf("day view lacks %q", want)
		}
	}
	for _, unwanted := range []string{"Layla", "Next week"} {
		if strings.Contains(page, unwanted) {
			t.Errorf("day view shows %q", unwanted)
		}
	}
	// Lanes are in staff order, unassigned last; overlapping items share the lane side by side.
	if a, h, n := strings.Index(page, "<h2>Aisha"), strings.Index(page, "<h2>Huda"), strings.Index(page, "<h2>Not assigned"); !(a < h && h < n) {
		t.Errorf("lane order: %d %d %d", a, h, n)
	}
	if !strings.Contains(page, "top:0px;height:60px;left:0%;width:50%") || !strings.Contains(page, "top:30px;height:45px;left:50%;width:50%") {
		t.Errorf("overlap layout wrong:\n%s", page)
	}

	// Hidden statuses can be shown.
	if _, page := get(owner, cal+"?view=day&date=2026-12-03&all=1"); !strings.Contains(page, "Layla · Nails") || !strings.Contains(page, "Hide cancelled") {
		t.Error("all=1 does not show cancelled bookings")
	}

	// Week view: Monday to Sunday, the range spans its days, next week stays out.
	code, page = get(owner, cal+"?view=week&date=2026-12-03")
	if code != 200 {
		t.Fatalf("week: %d", code)
	}
	for _, want := range []string{"Mon 30 Nov", "Sun 6 Dec", "week of 30 Nov 2026", "grid-column:2 / span 5", "Mariam · Haircut", "Aisha", "Walk-in"} {
		if !strings.Contains(page, want) {
			t.Errorf("week view lacks %q", want)
		}
	}
	if strings.Contains(page, "Next week") || strings.Contains(page, "Layla") {
		t.Error("week view shows rows outside the week or hidden ones")
	}
	// Without a date the calendar opens on today; a bad date falls back to today.
	for _, path := range []string{cal, cal + "?date=tomorrow&view=month"} {
		if code, page := get(owner, path); code != 200 || !strings.Contains(page, `aria-current="page">Calendar`) {
			t.Errorf("%s: %d", path, code)
		}
	}
	if code, page := get(owner, cal+"?date=2027-01-01"); code != 200 || !strings.Contains(page, "Nothing on this day") {
		t.Errorf("empty day: %d", code)
	}

	// Unknown tables and tables without calendar are not found.
	for _, path := range []string{"/apps/salma-salon/t/nope/calendar", "/apps/salma-salon/t/notes/calendar"} {
		if code, _ := get(owner, path); code != 404 {
			t.Errorf("%s: %d", path, code)
		}
	}

	// The edit form goes back to the same calendar view after saving or deleting.
	back := cal + "?date=2026-12-03&view=day"
	code, page = get(owner, "/apps/salma-salon/t/bookings/1?back="+url.QueryEscape(back))
	if code != 200 || !strings.Contains(page, "Back to calendar") || !strings.Contains(page, `name="back" value="`+strings.ReplaceAll(back, "&", "&amp;")+`"`) {
		t.Fatalf("form with back: %d %s", code, page)
	}
	form := url.Values{"csrf": {e.csrf(owner, "/")}, "date": {"2026-12-03"}, "time": {"11:00"}, "duration_min": {"60"}, "staff_id": {"1"},
		"customer_name": {"Mariam"}, "service": {"Colour"}, "status": {"confirmed"}, "back": {back}}
	r, _ := owner.PostForm(e.base+"/apps/salma-salon/t/bookings/1", form)
	if loc := r.Header.Get("Location"); r.StatusCode != 303 || loc != "/apps/salma-salon/t/bookings/1?saved=1&back="+url.QueryEscape(back) {
		t.Fatalf("save with back: %d %s", r.StatusCode, loc)
	}
	form.Set("back", "//evil.example/apps/salma-salon/t/bookings/calendar")
	r, _ = owner.PostForm(e.base+"/apps/salma-salon/t/bookings/1", form)
	if loc := r.Header.Get("Location"); loc != "/apps/salma-salon/t/bookings/1?saved=1" {
		t.Fatalf("foreign back kept: %s", loc)
	}
	r, _ = owner.PostForm(e.base+"/apps/salma-salon/t/bookings/2/delete", url.Values{"csrf": {e.csrf(owner, "/")}, "back": {back}})
	if loc := r.Header.Get("Location"); r.StatusCode != 303 || loc != back {
		t.Fatalf("delete with back: %d %s", r.StatusCode, loc)
	}
	if _, page := get(owner, back); !strings.Contains(page, "Mariam · Colour") || !strings.Contains(page, "11:00–12:00") || strings.Contains(page, "Noura") {
		t.Error("calendar does not show the changes")
	}

	// Staff see the calendar; other accounts do not.
	owner.PostForm(e.base+"/apps/salma-salon/staff", url.Values{"csrf": {e.csrf(owner, "/apps/salma-salon/staff")}, "email": {"rana@salon.ae"}})
	staff := e.browser()
	e.signup(staff, "rana@salon.ae")
	http.Get(linkRE.FindString(e.mail.last(t, "rana@salon.ae", "verify_email").Text))
	if code, page := get(staff, back); code != 200 || !strings.Contains(page, "Fatima · Henna") {
		t.Fatalf("staff calendar: %d", code)
	}
	bob := e.browser()
	e.signup(bob, "bob@salon.ae")
	if code, page := get(bob, back); code != 404 || strings.Contains(page, "Fatima") {
		t.Fatalf("stranger calendar: %d", code)
	}
	signedOut := e.browser()
	if r, _ := signedOut.Get(e.base + back); r.StatusCode != 303 || !strings.HasPrefix(r.Header.Get("Location"), "/login?next=") {
		t.Fatalf("signed out: %d", r.StatusCode)
	}
}

// Agents add a calendar to an existing table with patch_app; title may be a
// single column name, and invalid metadata is reported.
func TestCalendarPatch(t *testing.T) {
	e := newEnv(t)
	owner := e.browser()
	e.signup(owner, "salma@salon.ae")
	publishSalon(e, owner, "salma-salon")
	sess := e.mcpSession(e.connect(owner, e.registerClient()).AccessToken)
	notes := func(cal map[string]any) map[string]any {
		return map[string]any{"app": "salma-salon", "tables": []any{map[string]any{"name": "notes", "columns": []any{}, "calendar": cal}}}
	}
	out, errText := call(t, sess, "patch_app", notes(map[string]any{"date": "day", "title": "body"}))
	if errText != "" || out["ok"] == true || !strings.Contains(fmt.Sprint(out["problems"]), `table "notes": calendar: date: column "day" does not exist`) {
		t.Fatalf("invalid calendar: %v %s", out, errText)
	}
	if out, errText = call(t, sess, "patch_app", notes(map[string]any{"date": "created_at", "title": "body"})); errText != "" || out["ok"] != true {
		t.Fatalf("patch: %v %s", out, errText)
	}
	pub, errText := call(t, sess, "publish", map[string]any{"app": "salma-salon"})
	if errText != "" {
		t.Fatal(errText)
	}
	e.confirmPublish(owner, pub)
	r, _ := owner.Get(e.base + "/apps/salma-salon/t/notes/calendar")
	if page := read(t, r); r.StatusCode != 200 || !strings.Contains(page, "Nothing on this day") {
		t.Fatalf("notes calendar: %d", r.StatusCode)
	}
	// The bookings calendar is kept.
	if r, _ := owner.Get(e.base + "/apps/salma-salon/t/bookings/calendar"); r.StatusCode != 200 {
		t.Fatalf("bookings calendar: %d", r.StatusCode)
	}
}
