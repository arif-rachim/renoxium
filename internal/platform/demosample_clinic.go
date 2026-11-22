package platform

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"fmt"
	"time"
)

// clinic-queue-uae: the demo is a clinic open around the clock (so visitors
// can take a number at any hour), with a queue under way today, two weeks of
// history for the report, sample reviews and a staff list that lets anyone
// signed in try the staff page.
func init() {
	demoSamples["clinic-queue-uae"] = map[string][]map[string]any{"testimonials": {
		{"quote": "I took a number from the car park, bought a coffee and got the WhatsApp message just in time. Twelve minutes, not an hour on a plastic chair.",
			"quote_ar": "أخذت رقماً من موقف السيارات واشتريت قهوة، ووصلتني رسالة واتساب في الوقت المناسب. اثنتا عشرة دقيقة بدل ساعة على كرسي.",
			"name":     "Khalid R. (sample)", "area": "General practice", "area_ar": "الطب العام"},
		{"quote": "With two children, waiting outside the clinic is everything. The screen shows the room, so nobody shouts names.",
			"quote_ar": "مع طفلين، الانتظار خارج العيادة يعني الكثير. الشاشة تُظهر الغرفة فلا أحد ينادي الأسماء.",
			"name":     "Maria S. (sample)", "area": "Blood tests and vaccines", "area_ar": "تحاليل الدم والتطعيمات"},
		{"quote": "The estimate was right to the minute for my physio session, and I could follow it in Arabic.",
			"quote_ar": "كان الوقت المتوقع دقيقاً لجلسة العلاج الطبيعي، وتابعته بالعربية.",
			"name":     "Noura A. (sample)", "area": "Physiotherapy", "area_ar": "العلاج الطبيعي"},
	}}
	demoSettings["clinic-queue-uae"] = map[string]map[string]any{"clinic": {"opens": "00:00", "closes": "24:00", "last_ticket_minutes": 0,
		"tagline": "See a doctor today, without the waiting room", "tagline_ar": "قابل الطبيب اليوم دون الجلوس في قاعة الانتظار"}}
	demoSchedules["clinic-queue-uae"] = clinicDemoSchedule
}

func demoToken() string {
	b := make([]byte, 16)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// clinicDemoSchedule fills today's queue around now (seen patients, two in
// rooms, others waiting) and the last 13 days for the report.
func clinicDemoSchedule(ctx context.Context, db *sql.DB, now time.Time) error {
	if _, err := db.ExecContext(ctx, `INSERT INTO staff (email, name, active) VALUES ('*', 'Demo visitors', 1)`); err != nil {
		return err
	}
	svc := map[string]int{}
	rows, err := db.QueryContext(ctx, `SELECT id, prefix FROM services`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id int
		var p string
		rows.Scan(&id, &p)
		svc[p] = id
	}
	rows.Close()
	room := map[string]int{}
	rows, err = db.QueryContext(ctx, `SELECT id, name FROM rooms`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id int
		var n string
		rows.Scan(&id, &n)
		room[n] = id
	}
	rows.Close()
	today := now.Format("2006-01-02")
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	at := func(minAgo int) string {
		t := now.Add(-time.Duration(minAgo) * time.Minute)
		if t.Before(midnight) {
			t = midnight.Add(time.Duration(60-minAgo%60) * time.Second)
		}
		return t.Format("2006-01-02 15:04:05")
	}
	people := []demoPerson{{"Omar Al Hashimi", "ar"}, {"Priya Nair", "en"}, {"James Carter", "en"}, {"Fatma Al Zaabi", "ar"}, {"Ahmed Saleh", "ar"},
		{"Liza Mendoza", "en"}, {"Rania Khoury", "ar"}, {"Tom Becker", "en"}, {"Sana Iqbal", "en"}, {"Hamad Al Shamsi", "ar"}, {"Elena Petrova", "en"},
		{"Yasmin Farouk", "ar"}, {"Ravi Kumar", "en"}, {"Mona Al Ali", "ar"}, {"Chloe Martin", "en"}, {"Saeed Al Nuaimi", "ar"}, {"Grace Wanjiru", "en"},
		{"Bilal Qureshi", "en"}, {"Layla Haddad", "ar"}, {"Nikhil Shah", "en"}}
	type tk struct {
		prefix, status, room string
		taken, called, done  int // minutes ago; -1 for none
	}
	list := []tk{
		{"A", "done", "Room 1", 130, 118, 106}, {"A", "done", "Room 2", 125, 110, 99}, {"D", "done", "Nurse station", 120, 112, 107},
		{"B", "done", "Room 3", 115, 100, 80}, {"A", "done", "Room 1", 105, 96, 84}, {"A", "no_show", "", 100, 84, 82}, {"C", "done", "Room 4", 95, 70, 41},
		{"A", "done", "Room 2", 90, 72, 60}, {"D", "done", "Nurse station", 70, 66, 60}, {"A", "done", "Room 1", 66, 50, 37}, {"B", "done", "Room 3", 64, 48, 27},
		{"A", "called", "Room 2", 52, 9, -1}, {"A", "called", "Room 1", 48, 4, -1}, {"B", "called", "Room 3", 40, 6, -1}, {"D", "left", "", 35, -1, 20},
		{"A", "waiting", "", 30, -1, -1}, {"A", "waiting", "", 24, -1, -1}, {"C", "waiting", "", 22, -1, -1}, {"A", "waiting", "", 17, -1, -1},
		{"B", "waiting", "", 14, -1, -1}, {"D", "waiting", "", 10, -1, -1}, {"A", "waiting", "", 6, -1, -1}, {"A", "waiting", "", 2, -1, -1},
	}
	number := map[string]int{}
	for i, x := range list {
		number[x.prefix]++
		p := people[i%len(people)]
		row := map[string]any{"day": today, "number": number[x.prefix], "code": fmt.Sprintf("%s%03d", x.prefix, number[x.prefix]),
			"service_id": svc[x.prefix], "name": p.name, "phone": p.phone(i), "email": "", "lang": p.lang, "status": x.status,
			"source": map[bool]string{true: "reception", false: "phone"}[i%3 == 0], "token": demoToken(), "taken_at": at(x.taken),
			"calls": 0, "alerted": "near", "wait_minutes": nil, "called_at": nil, "done_at": nil, "room_id": nil}
		if x.called >= 0 {
			row["called_at"], row["calls"], row["wait_minutes"] = at(x.called), 1, x.taken-x.called
		}
		if x.done >= 0 {
			row["done_at"] = at(x.done)
		}
		if x.room != "" {
			row["room_id"] = room[x.room]
		}
		if x.status == "waiting" {
			row["alerted"] = ""
		}
		if err := insertDemoRow(ctx, db, "tickets", row); err != nil {
			return err
		}
	}
	// The first two waiting for general practice are within two of the rooms: they were told on WhatsApp.
	db.ExecContext(ctx, `UPDATE tickets SET alerted = 'whatsapp' WHERE id IN (SELECT id FROM tickets WHERE day = ? AND status = 'waiting' AND service_id = ? ORDER BY id LIMIT 3)`, today, svc["A"])
	// The last 13 days, without names: the counts the report and daily_stats show.
	prefixes := []string{"A", "A", "A", "B", "C", "D", "A", "D"}
	for d := 1; d <= 13; d++ {
		date := now.AddDate(0, 0, -d).Format("2006-01-02")
		n := 26 + (d*7)%17 + map[bool]int{true: 8, false: 0}[now.AddDate(0, 0, -d).Weekday() == time.Sunday]
		taken, seen, noShows, waits, maxWait := 0, 0, 0, 0, 0
		numbers := map[string]int{}
		for i := 0; i < n; i++ {
			p := prefixes[(i+d)%len(prefixes)]
			numbers[p]++
			minute := 8*60 + i*(13*60)/n
			wait := 6 + (i*11+d*5)%28
			status := "done"
			if (i+d)%13 == 0 {
				status = "no_show"
				noShows++
			} else {
				seen++
			}
			taken++
			waits += wait
			maxWait = max(maxWait, wait)
			stamp := func(m int) string { return fmt.Sprintf("%s %02d:%02d:00", date, (m/60)%24, m%60) }
			if err := insertDemoRow(ctx, db, "tickets", map[string]any{"day": date, "number": numbers[p], "code": fmt.Sprintf("%s%03d", p, numbers[p]),
				"service_id": svc[p], "name": "", "phone": "", "email": "", "lang": "en", "status": status, "source": "phone", "token": "",
				"taken_at": stamp(minute), "called_at": stamp(minute + wait), "done_at": stamp(minute + wait + 10), "wait_minutes": wait, "calls": 1, "alerted": "near",
				"room_id": nil}); err != nil {
				return err
			}
		}
		if err := insertDemoRow(ctx, db, "daily_stats", map[string]any{"day": date, "taken": taken, "seen": seen, "no_shows": noShows, "left_queue": 0,
			"avg_wait": waits / taken, "max_wait": maxWait, "busiest_hour": "18:00"}); err != nil {
			return err
		}
	}
	return nil
}
