package platform_test

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/arif-rachim/renoxium/examples"
	"github.com/arif-rachim/renoxium/internal/appdef"
	"github.com/arif-rachim/renoxium/internal/platform"
)

func dubaiDayOf(now time.Time, offset int) string {
	return now.UTC().Add(4*time.Hour).AddDate(0, 0, offset).Format("2006-01-02")
}

// checkDemoPhotos asserts that every demo photo of an example is in its row
// and served by the demo's host.
func checkDemoPhotos(t *testing.T, p *platform.Platform, example string) {
	t.Helper()
	app := platform.DemoApp(example)
	v := newVisitor(t, p, app+".renoxium.test")
	for _, set := range examples.DemoImages(example) {
		for key, cols := range set.Rows {
			rows := platform.PreviewRows(t, p, app, fmt.Sprintf(`SELECT * FROM %q WHERE %q = ?`, set.Table, set.Key), key)
			if len(rows) != 1 {
				t.Fatalf("%s: %d %s rows for %q", example, len(rows), set.Table, key)
			}
			for col := range cols {
				u, _ := rows[0][col].(string)
				if !strings.HasPrefix(u, platform.FilesPrefix) {
					t.Fatalf("%s: %s.%s of %q is %q", example, set.Table, col, key, u)
				}
				if w := v.do("GET", u+"?w=640", nil); w.Code != 200 || !strings.HasPrefix(w.Header().Get("Content-Type"), "image/") {
					t.Fatalf("%s: %s: %d %s", example, u, w.Code, w.Header().Get("Content-Type"))
				}
			}
		}
	}
}

// count returns the number of rows a query finds on an app's preview.
func count(t *testing.T, p *platform.Platform, app, query string, args ...any) int64 {
	t.Helper()
	return platform.PreviewRows(t, p, app, query, args...)[0]["n"].(int64)
}

// checkDemoSchedule asserts that the scheduling demos have rows on the given
// Dubai days and none that break the templates' rules.
func checkDemoSchedule(t *testing.T, p *platform.Platform, today, tomorrow string) {
	t.Helper()
	salon := "demo-booking-uae"
	if n := count(t, p, salon, `SELECT count(*) AS n FROM bookings WHERE date = ?`, today); n < 5 {
		t.Errorf("salon bookings today (%s): %d", today, n)
	}
	if n := count(t, p, salon, `SELECT count(*) AS n FROM bookings WHERE date = ?`, tomorrow); n < 3 {
		t.Errorf("salon bookings tomorrow: %d", n)
	}
	if n := count(t, p, salon, `SELECT count(*) AS n FROM bookings a JOIN bookings b ON a.id < b.id AND a.staff_id = b.staff_id AND a.date = b.date
		AND a.time < b.end_time AND b.time < a.end_time`); n != 0 {
		t.Errorf("%d double bookings in the salon", n)
	}
	if n := count(t, p, salon, `SELECT count(*) AS n FROM bookings WHERE time < '10:00' OR end_time > '21:00' OR status NOT IN ('confirmed', 'pending')
		OR (payment_method = 'card' AND status = 'confirmed' AND paid_amount != round(price * 0.5, 2)) OR (payment_method = 'venue' AND paid_amount != 0)`); n != 0 {
		t.Errorf("%d salon bookings outside opening hours or with wrong amounts", n)
	}
	if n := count(t, p, salon, `SELECT count(*) AS n FROM bookings b JOIN services s ON s.id = b.service_id JOIN staff st ON st.id = b.staff_id
		WHERE b.price != s.price OR b.item != s.name OR b.staff_name != st.name OR (',' || st.services || ',') NOT LIKE ('%,' || s.id || ',%')`); n != 0 {
		t.Errorf("%d salon bookings that do not match their service and staff", n)
	}

	home := "demo-home-service-uae"
	if n := count(t, p, home, `SELECT count(*) AS n FROM bookings WHERE date IN (?, ?)`, today, tomorrow); n < 3 {
		t.Errorf("home-service visits today and tomorrow: %d", n)
	}
	if n := count(t, p, home, `SELECT count(*) AS n FROM (SELECT date, slot FROM bookings GROUP BY date, slot HAVING count(*) > 2)`); n != 0 {
		t.Errorf("%d arrival windows with more visits than crews", n)
	}
	if n := count(t, p, home, `SELECT count(*) AS n FROM (SELECT 1 FROM bookings WHERE crew_id > 0 GROUP BY date, slot, crew_id HAVING count(*) > 1)`); n != 0 {
		t.Errorf("%d crews booked twice in one window", n)
	}

	tours := "demo-reservation-uae"
	if n := count(t, p, tours, `SELECT count(*) AS n FROM reservations WHERE start_date = ?`, today); n < 2 {
		t.Errorf("reservations starting today: %d", n)
	}
	if n := count(t, p, tours, `SELECT count(*) AS n FROM reservations r JOIN items i ON i.id = r.item_id WHERE r.mode = 'range' AND r.quantity > i.stock`); n != 0 {
		t.Errorf("%d rentals over stock", n)
	}
	if n := count(t, p, "demo-listings-uae", `SELECT count(*) AS n FROM leads WHERE preferred_date = ?`, today); n < 1 {
		t.Errorf("viewings today: %d", n)
	}
	clinic := "demo-clinic-queue-uae"
	if n := count(t, p, clinic, `SELECT count(*) AS n FROM tickets WHERE day = ? AND status = 'waiting'`, today); n < 5 {
		t.Errorf("patients waiting today: %d", n)
	}
	if n := count(t, p, clinic, `SELECT count(*) AS n FROM tickets WHERE day = ? AND status = 'called' AND room_id > 0`, today); n != 3 {
		t.Errorf("patients in rooms: %d", n)
	}
	if n := count(t, p, clinic, `SELECT count(*) AS n FROM (SELECT day, code FROM tickets GROUP BY day, code HAVING count(*) > 1)`); n != 0 {
		t.Errorf("%d ticket codes used twice in a day", n)
	}
	if n := count(t, p, clinic, `SELECT count(*) AS n FROM (SELECT room_id FROM tickets WHERE day = ? AND status = 'called' GROUP BY room_id HAVING count(*) > 1)`, today); n != 0 {
		t.Errorf("%d rooms with two patients", n)
	}
	if n := count(t, p, clinic, `SELECT count(*) AS n FROM daily_stats`); n != 13 {
		t.Errorf("days of history: %d", n)
	}
}

// The demos show photos and a schedule around today, both renewed by the
// hourly reset; apps made from the same templates get neither.
func TestDemoPhotosAndSchedule(t *testing.T) {
	p, _ := newDemoPlatform(t)
	ctx := context.Background()
	if err := p.EnsureDemos(ctx, examples.Gallery()); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	today, tomorrow := dubaiDayOf(now, 0), dubaiDayOf(now, 1)
	for _, e := range examples.Gallery() {
		checkDemoPhotos(t, p, e.Name)
	}
	checkDemoSchedule(t, p, today, tomorrow)

	// Visitors still see the free times around the booked ones, and book them.
	v := newVisitor(t, p, "demo-booking-uae.renoxium.test")
	slots := v.do("GET", "/api/slots?service=2&staff=1&date="+tomorrow, nil).Body.String() // Layla has a root colour at 10:00 tomorrow
	if strings.Contains(slots, `"time":"10:00"`) || strings.Contains(slots, `"time":"11:00"`) || !strings.Contains(slots, `"time":"12:00"`) {
		t.Fatalf("free times around tomorrow's bookings: %s", slots)
	}
	if w := v.submit("/_rx/auth/signup", "/_rx/auth/signup", url.Values{"email": {"guest@example.ae"}, "password": {"demo password 1"}}); w.Code != 303 {
		t.Fatalf("signup: %d", w.Code)
	}
	if w := v.do("POST", "/book", url.Values{"service": {"2"}, "staff": {"1"}, "date": {tomorrow}, "time": {"10:00"}, "name": {"Guest"},
		"phone": {"+971500000000"}, "payment": {"venue"}}); !strings.Contains(w.Header().Get("Location"), "error_slot") {
		t.Fatalf("booked a time a demo booking holds: %d %s", w.Code, w.Header().Get("Location"))
	}
	if w := v.do("POST", "/book", url.Values{"service": {"2"}, "staff": {"1"}, "date": {tomorrow}, "time": {"12:00"}, "name": {"Guest"},
		"phone": {"+971500000000"}, "payment": {"venue"}}); w.Code >= 400 || strings.Contains(w.Header().Get("Location"), "error") {
		t.Fatalf("booking a free time: %d %s", w.Code, w.Header().Get("Location"))
	}
	if n := count(t, p, "demo-booking-uae", `SELECT count(*) AS n FROM bookings WHERE customer_name = 'Guest' AND time = '12:00'`); n != 1 {
		t.Fatalf("visitor's booking: %d", n)
	}

	// The next reset builds the schedule around the new day, and keeps the photos.
	platform.SetDemoClock(t, func() time.Time { return now.AddDate(0, 0, 2) })
	salonToday := count(t, p, "demo-booking-uae", `SELECT count(*) AS n FROM bookings WHERE date = ?`, today)
	if err := p.ResetDemos(ctx); err != nil {
		t.Fatal(err)
	}
	if n := count(t, p, "demo-booking-uae", `SELECT count(*) AS n FROM bookings WHERE date = ?`, today); n != 0 {
		t.Errorf("bookings of the old day after the reset: %d", n)
	}
	if n := count(t, p, "demo-booking-uae", `SELECT count(*) AS n FROM bookings WHERE date = ?`, dubaiDayOf(now, 2)); n != salonToday {
		t.Errorf("bookings of the new day: %d, want the %d of the first day", n, salonToday)
	}
	if n := count(t, p, "demo-booking-uae", `SELECT count(*) AS n FROM bookings WHERE customer_name = 'Guest'`); n != 0 {
		t.Error("the visitor's booking survived the reset")
	}
	for _, e := range examples.Gallery() {
		checkDemoPhotos(t, p, e.Name)
	}
	checkDemoSchedule(t, p, dubaiDayOf(now, 2), dubaiDayOf(now, 3))

	// An owner's app from the same templates: no demo photos, no schedule.
	for _, name := range []string{"store-uae", "booking-uae", "listings-uae", "reservation-uae"} {
		ex, _ := examples.Get(name)
		def, _ := appdef.Parse(ex.JSON)
		app := "my-" + strings.TrimSuffix(name, "-uae")
		p.CreateApp("usr_test", app)
		if res, err := p.ApplyChange(ctx, app, def); err != nil || !res.OK {
			t.Fatal(err, res)
		}
		if files, _ := p.Files(app); len(files) != 0 {
			t.Errorf("%s has %d files", app, len(files))
		}
		for _, set := range examples.DemoImages(name) {
			for col := range set.Rows[firstKey(set.Rows)] {
				if n := count(t, p, app, fmt.Sprintf(`SELECT count(*) AS n FROM %q WHERE coalesce(%q, '') != ''`, set.Table, col)); n != 0 {
					t.Errorf("%s: %d rows of %s with a %s", app, n, set.Table, col)
				}
			}
		}
	}
	if n := count(t, p, "my-booking", `SELECT count(*) AS n FROM bookings`); n != 0 {
		t.Errorf("an owner's booking app has %d bookings", n)
	}
	if n := count(t, p, "my-reservation", `SELECT count(*) AS n FROM reservations`); n != 0 {
		t.Errorf("an owner's reservation app has %d reservations", n)
	}
	if n := count(t, p, "my-listings", `SELECT count(*) AS n FROM leads`); n != 0 {
		t.Errorf("an owner's listings app has %d leads", n)
	}
	// What agents copy (get_example) is the template as it was: no demo photos, no schedule.
	for _, e := range examples.All() {
		if strings.Contains(string(e.JSON), platform.FilesPrefix) || strings.Contains(string(e.JSON), "Fatima Al Mansoori") {
			t.Errorf("%s: the example handed to agents carries demo data", e.Name)
		}
	}
}

func firstKey(m map[string]map[string]string) string {
	for k := range m {
		return k
	}
	return ""
}
