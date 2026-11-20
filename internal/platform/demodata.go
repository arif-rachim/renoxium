package platform

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/arif-rachim/renoxium/examples"
)

// Demo data: after every reset (and when a demo is created or brought up to
// date) a demo gets what its template's seed rows cannot hold:
//
//   - photos for its products, services, items and listings
//     (examples.DemoImages), uploaded to the demo app and written into the
//     image columns of those rows. The templates handed to users keep empty
//     image seeds, so their own agent uploads the owner's photos.
//   - a schedule around today: bookings, visits, reservations and viewings
//     dated today and the next days in Dubai time, so the owner's calendar
//     and the customer pages look like a running business whenever someone
//     opens the demo. Seed rows cannot do this because their dates are fixed.
//
// Both only ever touch the preview database of an app owned by DemoOwner.

// dubai is the UAE's time zone (no daylight saving).
var dubai = time.FixedZone("GST", 4*3600)

// demoClock is the time the demo schedule is built around; tests move it.
var demoClock = time.Now

func (p *Platform) demoData(ctx context.Context, name string) error {
	if !p.IsDemo(name) {
		return nil
	}
	example := strings.TrimPrefix(name, DemoPrefix)
	st, err := p.loadStage(name, Preview)
	if err != nil || st == nil {
		return err
	}
	if err := p.demoPhotos(ctx, name, example, st.admin); err != nil {
		return fmt.Errorf("demo photos: %w", err)
	}
	if fill := demoSchedules[example]; fill != nil {
		if err := fill(ctx, st.admin, demoClock().In(dubai)); err != nil {
			return fmt.Errorf("demo schedule: %w", err)
		}
	}
	return nil
}

func (p *Platform) demoPhotos(ctx context.Context, app, example string, db *sql.DB) error {
	for _, set := range examples.DemoImages(example) {
		keys := make([]string, 0, len(set.Rows))
		for k := range set.Rows {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, key := range keys {
			for col, file := range set.Rows[key] {
				url, err := p.demoPhoto(app, file)
				if err != nil {
					return err
				}
				if _, err := db.ExecContext(ctx, `UPDATE "`+set.Table+`" SET "`+col+`" = ? WHERE "`+set.Key+`" = ?`, url, key); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// demoPhoto uploads a demo photo to a demo app once per process; the
// hourly reset then reuses its address (p.demoURLs) without hashing again.
func (p *Platform) demoPhoto(app, file string) (string, error) {
	if u, ok := p.demoURLs.Load(app + "/" + file); ok {
		return u.(string), nil
	}
	b, err := examples.DemoImage(file)
	if err != nil {
		return "", err
	}
	fi, err := p.AddFile(app, bytes.NewReader(b))
	if err != nil {
		return "", fmt.Errorf("%s: %w", file, err)
	}
	p.demoURLs.Store(app+"/"+file, fi.URL)
	return fi.URL, nil
}

// --- the schedule around today ---

type demoSchedule func(ctx context.Context, db *sql.DB, now time.Time) error

var demoSchedules = map[string]demoSchedule{
	"booking-uae":      bookingDemoSchedule,
	"home-service-uae": homeServiceDemoSchedule,
	"reservation-uae":  reservationDemoSchedule,
	"listings-uae":     listingsDemoSchedule,
}

// demoPerson is a made-up customer. Phone numbers use the 0000 range and
// addresses example.com, so nothing reaches a real person.
type demoPerson struct{ name, lang string }

func (d demoPerson) phone(i int) string { return fmt.Sprintf("+97150000%04d", 1000+i) }
func (d demoPerson) email() string {
	return strings.ToLower(strings.NewReplacer(" ", ".", "'", "").Replace(d.name)) + "@example.com"
}

func day(now time.Time, offset int) string { return now.AddDate(0, 0, offset).Format("2006-01-02") }
func clockMins(hhmm string) int {
	var h, m int
	fmt.Sscanf(hhmm, "%d:%d", &h, &m)
	return h*60 + m
}
func hhmm(m int) string           { return fmt.Sprintf("%02d:%02d", m/60, m%60) }
func round2(x float64) float64    { return math.Round(x*100) / 100 }
func vatOf(total float64) float64 { return round2(total * 5 / 105) }

func demoCrewKey() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func jsonText(v any) string { b, _ := json.Marshal(v); return string(b) }

func insertDemoRow(ctx context.Context, db *sql.DB, table string, row map[string]any) error {
	cols := make([]string, 0, len(row))
	for c := range row {
		cols = append(cols, c)
	}
	sort.Strings(cols)
	args := make([]any, len(cols))
	for i, c := range cols {
		args[i] = row[c]
		cols[i] = `"` + c + `"`
	}
	_, err := db.ExecContext(ctx, `INSERT INTO "`+table+`" (`+strings.Join(cols, ",")+`) VALUES (`+
		strings.TrimSuffix(strings.Repeat("?,", len(cols)), ",")+`)`, args...)
	return err
}

// booking-uae: appointments with each stylist, inside opening hours, on the
// template's slot grid, never two at once for one staff member.
func bookingDemoSchedule(ctx context.Context, db *sql.DB, now time.Time) error {
	type appt struct {
		day           int
		time, service string
		staff         string
		who           demoPerson
		card, pending bool
	}
	list := []appt{
		{0, "10:00", "Haircut & Blow-dry", "Layla", demoPerson{"Fatima Al Mansoori", "ar"}, true, false},
		{0, "11:30", "Root Colour", "Layla", demoPerson{"Sarah Thompson", "en"}, true, false},
		{0, "15:00", "Haircut & Blow-dry", "Layla", demoPerson{"Aisha Rahman", "en"}, false, false},
		{0, "10:30", "Gel Manicure & Pedicure", "Maryam", demoPerson{"Joanna Reyes", "en"}, true, false},
		{0, "13:00", "Classic Manicure", "Maryam", demoPerson{"Noor Haddad", "ar"}, false, false},
		{0, "17:30", "Gel Manicure & Pedicure", "Maryam", demoPerson{"Hessa Al Ketbi", "ar"}, true, true},
		{0, "11:00", "Signature Facial", "Priya", demoPerson{"Emma Clarke", "en"}, true, false},
		{0, "14:00", "Moroccan Bath", "Priya", demoPerson{"Reem Al Suwaidi", "ar"}, false, false},
		{0, "18:00", "Signature Facial", "Priya", demoPerson{"Divya Menon", "en"}, true, false},
		{1, "10:00", "Root Colour", "Layla", demoPerson{"Mariam Al Falasi", "ar"}, true, false},
		{1, "12:00", "Classic Manicure", "Maryam", demoPerson{"Grace Dela Cruz", "en"}, true, false},
		{1, "11:00", "Moroccan Bath", "Priya", demoPerson{"Sophie Martin", "en"}, true, true},
		{1, "15:30", "Signature Facial", "Priya", demoPerson{"Shaikha Al Nuaimi", "ar"}, true, false},
		{3, "12:00", "Haircut & Blow-dry", "Layla", demoPerson{"Anjali Nair", "en"}, true, false},
		{4, "17:00", "Gel Manicure & Pedicure", "Priya", demoPerson{"Hannah Weber", "en"}, false, false},
	}
	var opens, closes, closed string
	var slot, deposit int
	if err := db.QueryRowContext(ctx, `SELECT opens, closes, coalesce(closed_days, ''), coalesce(slot_minutes, 30), coalesce(deposit_percent, 0)
		FROM business ORDER BY id LIMIT 1`).Scan(&opens, &closes, &closed, &slot, &deposit); err != nil {
		return err
	}
	if slot < 5 {
		slot = 5
	}
	busy := map[string][][2]int{} // staff id and date -> taken minutes
	for i, a := range list {
		date := day(now, a.day)
		if strings.Contains(","+strings.ReplaceAll(closed, " ", "")+",", fmt.Sprintf(",%d,", int(now.AddDate(0, 0, a.day).Weekday()))) {
			continue
		}
		var sID, dur int
		var price float64
		var item, itemAr string
		if err := db.QueryRowContext(ctx, `SELECT id, duration_min, price, name, coalesce(name_ar, '') FROM services WHERE name = ? AND active = 1`, a.service).
			Scan(&sID, &dur, &price, &item, &itemAr); err != nil {
			continue
		}
		var stID int
		var stName, stAr, offers string
		if err := db.QueryRowContext(ctx, `SELECT id, name, coalesce(name_ar, ''), coalesce(services, '') FROM staff WHERE name = ? AND active = 1`, a.staff).
			Scan(&stID, &stName, &stAr, &offers); err != nil {
			continue
		}
		if offers != "" && !strings.Contains(","+strings.ReplaceAll(offers, " ", "")+",", fmt.Sprintf(",%d,", sID)) {
			continue
		}
		start := clockMins(a.time)
		end := start + dur
		if start < clockMins(opens) || end > clockMins(closes) || (start-clockMins(opens))%slot != 0 {
			continue
		}
		key := fmt.Sprintf("%d/%s", stID, date)
		clash := false
		for _, x := range busy[key] {
			clash = clash || (start < x[1] && end > x[0])
		}
		var n int
		db.QueryRowContext(ctx, `SELECT count(*) FROM bookings WHERE kind = 'appointment' AND staff_id = ? AND date = ? AND time < ? AND end_time > ?
			AND status IN ('confirmed', 'pending')`, stID, date, hhmm(end), hhmm(start)).Scan(&n)
		if clash || n > 0 {
			continue
		}
		busy[key] = append(busy[key], [2]int{start, end})
		method, status, due, paid := "venue", "confirmed", 0.0, 0.0
		if a.card && deposit > 0 {
			method, due = "card", round2(price*float64(deposit)/100)
			if a.pending {
				status = "pending"
			} else {
				paid = due
			}
		}
		if err := insertDemoRow(ctx, db, "bookings", map[string]any{
			"user_id": 0, "kind": "appointment", "customer_name": a.who.name, "phone": a.who.phone(i), "email": a.who.email(),
			"item": item, "item_ar": itemAr, "service_id": sID, "class_id": 0, "staff_id": stID, "staff_name": stName, "staff_name_ar": stAr,
			"participant": "", "date": date, "time": a.time, "end_time": hhmm(end), "price": price, "deposit_due": due, "paid_amount": paid,
			"vat": vatOf(price), "payment_method": method, "status": status, "lang": a.who.lang, "reminded": 0,
		}); err != nil {
			return err
		}
	}
	return nil
}

// home-service-uae: visits in the template's arrival windows, at most one
// per crew and window and never more than crews_per_slot in a window. Visits
// already over today are completed, the one under way is in progress.
func homeServiceDemoSchedule(ctx context.Context, db *sql.DB, now time.Time) error {
	type visit struct {
		day           int
		slot, service string
		qty           int
		materials     bool
		zone, crew    string
		who           demoPerson
		address, flat string
		card, pending bool
	}
	list := []visit{
		{0, "08:00", "Home cleaning", 4, true, "Dubai Marina", "Maria", demoPerson{"Olga Petrova", "en"}, "Marina Gate 2", "Apt 1204", true, false},
		{0, "10:00", "AC service", 3, false, "JLT", "Imran", demoPerson{"Rashid Al Zaabi", "ar"}, "Cluster D, Lake Terrace", "Apt 905", false, false},
		{0, "12:00", "Deep cleaning (1-bedroom apartment)", 1, false, "Al Barsha", "Maria", demoPerson{"Priyanka Sharma", "en"}, "Al Barsha 1, Street 23", "Villa 7", true, false},
		{0, "14:00", "Handyman", 2, false, "Downtown Dubai", "Imran", demoPerson{"James O'Connor", "en"}, "Boulevard Heights, Tower 1", "Apt 2310", true, false},
		{0, "16:00", "Sofa shampoo cleaning", 4, false, "Arabian Ranches", "Maria", demoPerson{"Mona Saleh", "ar"}, "Alvorada 3, Street 5", "Villa 41", false, false},
		{1, "08:00", "Pest control (apartment)", 1, false, "Dubai Marina", "Imran", demoPerson{"Khalid Al Marri", "ar"}, "Princess Tower", "Apt 3302", true, false},
		{1, "10:00", "Home cleaning", 3, false, "JLT", "", demoPerson{"Jennifer Aquino", "en"}, "Cluster X, Jumeirah Bay", "Apt 1507", true, true},
		{1, "14:00", "AC service", 2, false, "Downtown Dubai", "Imran", demoPerson{"Faisal Qureshi", "en"}, "South Ridge 4", "Apt 1102", false, false},
		{2, "10:00", "Home cleaning", 4, true, "Al Barsha", "Maria", demoPerson{"Rania Aziz", "ar"}, "Al Barsha South, Street 12", "Villa 18", true, false},
	}
	var first, last, closedDay string
	var hours, crewsPer int
	var minOrder float64
	if err := db.QueryRowContext(ctx, `SELECT first_slot, last_slot, coalesce(slot_hours, 2), coalesce(crews_per_slot, 1), coalesce(closed_weekday, ''), coalesce(min_order, 0)
		FROM business ORDER BY id LIMIT 1`).Scan(&first, &last, &hours, &crewsPer, &closedDay, &minOrder); err != nil {
		return err
	}
	materialsEn, materialsAr := "Materials", "مواد"
	db.QueryRowContext(ctx, `SELECT en, ar FROM texts WHERE key = 'materials'`).Scan(&materialsEn, &materialsAr)
	step := hours * 60
	if step < 30 {
		step = 30
	}
	nowMins := now.Hour()*60 + now.Minute()
	used := map[string]map[int]bool{} // date and slot -> crew ids
	for i, v := range list {
		d := now.AddDate(0, 0, v.day)
		date := d.Format("2006-01-02")
		if strings.EqualFold(d.Weekday().String(), closedDay) {
			continue
		}
		start := clockMins(v.slot)
		if start < clockMins(first) || start > clockMins(last) || (start-clockMins(first))%step != 0 {
			continue
		}
		var sID int
		var sName, sAr, unit string
		var price, materialsPrice float64
		if err := db.QueryRowContext(ctx, `SELECT id, name, coalesce(name_ar, ''), unit, price, coalesce(materials_price, 0) FROM services WHERE name = ? AND active = 1`, v.service).
			Scan(&sID, &sName, &sAr, &unit, &price, &materialsPrice); err != nil {
			continue
		}
		var zID int
		var zName string
		var surcharge float64
		if err := db.QueryRowContext(ctx, `SELECT id, name, coalesce(surcharge, 0) FROM zones WHERE name = ? AND active = 1`, v.zone).Scan(&zID, &zName, &surcharge); err != nil {
			continue
		}
		crewID := 0
		if v.crew != "" {
			if err := db.QueryRowContext(ctx, `SELECT id FROM crews WHERE name LIKE ? AND active = 1 ORDER BY id LIMIT 1`, v.crew+"%").Scan(&crewID); err != nil {
				continue
			}
		}
		k := date + " " + v.slot
		var taken int
		db.QueryRowContext(ctx, `SELECT count(*) FROM bookings WHERE date = ? AND slot = ? AND status NOT IN ('cancelled', 'failed', 'expired', 'refunded')`, date, v.slot).Scan(&taken)
		if used[k] == nil {
			used[k] = map[int]bool{}
		}
		if taken >= crewsPer || (crewID != 0 && used[k][crewID]) {
			continue
		}
		used[k][crewID] = true
		qty := v.qty
		if unit == "fixed" {
			qty = 1
		}
		lines := []map[string]any{{"name": sName, "name_ar": sAr, "price": price, "qty": qty}}
		subtotal := price * float64(qty)
		if v.materials && materialsPrice > 0 {
			mq := 1
			if unit == "per_hour" {
				mq = qty
			}
			lines = append(lines, map[string]any{"name": materialsEn, "name_ar": materialsAr, "price": materialsPrice, "qty": mq})
			subtotal += materialsPrice * float64(mq)
		}
		subtotal = round2(subtotal)
		if subtotal < minOrder {
			continue
		}
		total := round2(subtotal + surcharge)
		method, status, paid := "cash", "confirmed", 0
		if v.card {
			method = "card"
		}
		if crewID != 0 {
			status = "assigned"
		}
		end := start + step
		if v.day == 0 && end <= nowMins {
			status, paid = "completed", 1
		} else if v.day == 0 && start <= nowMins {
			status = "in_progress"
		}
		if v.card && status != "completed" {
			paid = 1
		}
		if v.pending {
			status, paid = "pending", 0
		}
		if err := insertDemoRow(ctx, db, "bookings", map[string]any{
			"user_id": 0, "customer_name": v.who.name, "phone": v.who.phone(i), "email": v.who.email(), "zone_id": zID, "zone": zName,
			"address": v.address, "flat": v.flat, "notes": "", "service_id": sID, "service": sName, "quantity": qty, "materials": len(lines) > 1,
			"date": date, "slot": v.slot, "slot_end": hhmm(end), "repeat": "once", "lines": jsonText(lines), "subtotal": subtotal,
			"surcharge": surcharge, "vat": vatOf(total), "total": total, "payment_method": method, "status": status, "paid": paid,
			"crew_id": crewID, "crew_key": demoCrewKey(), "lang": v.who.lang, "parent_id": 0, "repeat_done": 0, "reminded": 0, "autopay": 0,
		}); err != nil {
			return err
		}
	}
	return nil
}

// reservation-uae: seats on the day's departures (the departure row that runs
// on that weekday) and rentals from a start to an end date, within the
// departures' capacity and the items' stock.
func reservationDemoSchedule(ctx context.Context, db *sql.DB, now time.Time) error {
	type res struct {
		day, days             int
		item                  string
		adults, children, qty int
		who                   demoPerson
		later, pending        bool
	}
	list := []res{
		{0, 0, "Evening Desert Safari with BBQ Dinner", 2, 2, 0, demoPerson{"Ahmed Siddiqui", "en"}, false, false},
		{0, 0, "Evening Desert Safari with BBQ Dinner", 2, 0, 0, demoPerson{"Laura Bianchi", "en"}, true, false},
		{0, 0, "Marina Dhow Dinner Cruise", 4, 0, 0, demoPerson{"Mohammed Al Shamsi", "ar"}, false, false},
		{0, 0, "Sunset Yacht Trip", 6, 0, 0, demoPerson{"Chen Wei", "en"}, false, true},
		{1, 0, "Evening Desert Safari with BBQ Dinner", 3, 0, 0, demoPerson{"Ravi Patel", "en"}, false, false},
		{1, 0, "Marina Dhow Dinner Cruise", 2, 1, 0, demoPerson{"Anna Kowalski", "en"}, false, false},
		{0, 3, "Nissan Sunny", 0, 0, 1, demoPerson{"Omar Farouk", "ar"}, false, false},
		{1, 3, "Nissan Patrol", 0, 0, 1, demoPerson{"Tom Harris", "en"}, false, false},
		{2, 1, "Bouncy Castle 4 x 4 m", 0, 0, 1, demoPerson{"Huda Al Ali", "ar"}, false, false},
		{2, 1, "Gold Chiavari Chair", 0, 0, 60, demoPerson{"Jessa Mae Ramos", "en"}, true, false},
	}
	var deposit int
	var card, hold bool
	if err := db.QueryRowContext(ctx, `SELECT coalesce(deposit_percent, 0), coalesce(card_enabled, 0), coalesce(deposit_hold_enabled, 0) FROM business ORDER BY id LIMIT 1`).
		Scan(&deposit, &card, &hold); err != nil {
		return err
	}
	weekdays := []string{"sun", "mon", "tue", "wed", "thu", "fri", "sat"}
	stamp := now.UTC().Format("2006-01-02 15:04:05")
	for i, r := range list {
		var id, minDays int
		var name, nameAr, mode, category string
		var adultPrice, childPrice, dayPrice, weekPrice, security sql.NullFloat64
		var stock sql.NullInt64
		var docs bool
		if err := db.QueryRowContext(ctx, `SELECT id, name, coalesce(name_ar, name), mode, coalesce(category, ''), price_adult, price_child, price_day, price_week,
			stock, coalesce(min_days, 1), security_deposit, coalesce(needs_documents, 0) FROM items WHERE name = ? AND active = 1`, r.item).
			Scan(&id, &name, &nameAr, &mode, &category, &adultPrice, &childPrice, &dayPrice, &weekPrice, &stock, &minDays, &security, &docs); err != nil {
			continue
		}
		start := day(now, r.day)
		row := map[string]any{"user_id": 0, "customer_name": r.who.name, "phone": r.who.phone(i), "email": r.who.email(), "item_id": id,
			"item_name": name, "mode": mode, "extras": "[]", "lang": r.who.lang, "notes": "", "pending_since": stamp, "reminded": 0,
			"deposit_link_sent": 0, "documents_required": docs || category == "Cars"}
		var lines []map[string]any
		var securityTotal float64
		if mode == "departure" {
			wd := weekdays[now.AddDate(0, 0, r.day).Weekday()]
			deps, err := db.QueryContext(ctx, `SELECT id, coalesce(date, ''), coalesce(weekdays, ''), time, capacity, price FROM departures WHERE item_id = ? AND active = 1 ORDER BY id`, id)
			if err != nil {
				return err
			}
			depID, capacity, depTime, depPrice := 0, 0, "", sql.NullFloat64{}
			for deps.Next() {
				var dID, c int
				var d, w, t string
				var pr sql.NullFloat64
				if err := deps.Scan(&dID, &d, &w, &t, &c, &pr); err != nil {
					deps.Close()
					return err
				}
				w = strings.ToLower(w)
				if depID == 0 && ((d != "" && d == start) || (d == "" && (w == "" || strings.Contains(w, "daily") || strings.Contains(w, wd)))) {
					depID, capacity, depTime, depPrice = dID, c, t, pr
				}
			}
			deps.Close()
			if depID == 0 {
				continue
			}
			var taken int
			db.QueryRowContext(ctx, `SELECT coalesce(sum(adults + children), 0) FROM reservations WHERE departure_id = ? AND start_date = ?
				AND status IN ('pending', 'confirmed', 'completed', 'no_show')`, depID, start).Scan(&taken)
			if taken+r.adults+r.children > capacity {
				continue
			}
			adult := adultPrice.Float64
			if depPrice.Valid {
				adult = depPrice.Float64
			}
			child := adult
			if childPrice.Valid && childPrice.Float64 < adult {
				child = childPrice.Float64
			}
			lines = append(lines, map[string]any{"name": name + " – Adult", "name_ar": nameAr + " – بالغ", "qty": r.adults, "unit": adult, "amount": round2(adult * float64(r.adults))})
			if r.children > 0 {
				lines = append(lines, map[string]any{"name": name + " – Child", "name_ar": nameAr + " – طفل", "qty": r.children, "unit": child, "amount": round2(child * float64(r.children))})
			}
			for k, v := range map[string]any{"departure_id": depID, "start_date": start, "end_date": start, "time": depTime, "days": 1,
				"adults": r.adults, "children": r.children, "quantity": 1} {
				row[k] = v
			}
		} else {
			days := r.days
			if days < minDays {
				days = minDays
			}
			end := day(now, r.day+days)
			var inUse int
			for d := 0; d <= days; d++ { // the return day counts as taken
				var n int
				db.QueryRowContext(ctx, `SELECT coalesce(sum(quantity), 0) FROM reservations WHERE item_id = ? AND mode = 'range' AND start_date <= ? AND end_date >= ?
					AND status IN ('pending', 'confirmed', 'completed', 'no_show')`, id, day(now, r.day+d), day(now, r.day+d)).Scan(&n)
				inUse = max(inUse, n)
			}
			if !stock.Valid || inUse+r.qty > int(stock.Int64) {
				continue
			}
			unit := dayPrice.Float64 * float64(days)
			if weekPrice.Valid && weekPrice.Float64 > 0 {
				weeks := days / 7
				unit = math.Min(float64(weeks)*weekPrice.Float64+float64(days%7)*dayPrice.Float64, float64(weeks+1)*weekPrice.Float64)
			}
			word, wordAr := "days", "أيام"
			if days == 1 {
				word, wordAr = "day", "يوم"
			}
			lines = append(lines, map[string]any{"name": fmt.Sprintf("%s – %d %s", name, days, word), "name_ar": fmt.Sprintf("%s – %d %s", nameAr, days, wordAr),
				"qty": r.qty, "unit": unit, "amount": round2(unit * float64(r.qty))})
			securityTotal = round2(security.Float64 * float64(r.qty))
			for k, v := range map[string]any{"start_date": start, "end_date": end, "time": "", "days": days, "adults": 0, "children": 0, "quantity": r.qty} {
				row[k] = v
			}
		}
		var total float64
		for _, l := range lines {
			total += l["amount"].(float64)
		}
		total = round2(total)
		online := 0.0
		if card && deposit > 0 {
			online = round2(total * float64(deposit) / 100)
		}
		method, status, paid := "card", "confirmed", online
		if r.later || online == 0 {
			method, online, paid = "later", 0, 0
		}
		if r.pending {
			status, paid = "pending", 0
		}
		row["lines"] = jsonText(lines)
		row["vat"] = vatOf(total)
		row["subtotal"] = round2(total - vatOf(total))
		row["total"] = total
		row["due_online"] = online
		row["paid_amount"] = paid
		row["balance_due"] = round2(total - paid)
		row["security_deposit"] = securityTotal
		if securityTotal > 0 && hold {
			row["deposit_status"] = "due"
		}
		row["payment_method"] = method
		row["status"] = status
		if err := insertDemoRow(ctx, db, "reservations", row); err != nil {
			return err
		}
	}
	return nil
}

// listings-uae: viewing requests in the agents' calendar lanes.
func listingsDemoSchedule(ctx context.Context, db *sql.DB, now time.Time) error {
	type lead struct {
		day       int
		time, ref string
		status    string
		who       demoPerson
		message   string
	}
	list := []lead{
		{0, "11:00", "MH-1001", "viewing_booked", demoPerson{"Abdullah Al Mazrouei", "ar"}, "Can we see it this morning? I am looking to move in two months."},
		{0, "16:30", "MH-1004", "viewing_booked", demoPerson{"Rebecca Lewis", "en"}, "We are relocating from London with two children and want to see the garden."},
		{0, "18:00", "MH-1002", "viewing_booked", demoPerson{"Arjun Mehta", "en"}, "Is the flat available from next month? I can view after work."},
		{1, "10:30", "MH-1006", "viewing_booked", demoPerson{"Yousef Darwish", "ar"}, "Interested in the payment plan; please bring the brochure."},
		{1, "13:00", "MH-1008", "new", demoPerson{"Kristine Bautista", "en"}, "Looking for an office for 12 people. Is parking included?"},
		{2, "17:00", "MH-1011", "new", demoPerson{"Daniel Fischer", "en"}, "Is chiller really free? Would like to view on the weekend too."},
	}
	for i, l := range list {
		var id, agent int
		if err := db.QueryRowContext(ctx, `SELECT id, coalesce(agent_id, 0) FROM listings WHERE reference = ?`, l.ref).Scan(&id, &agent); err != nil {
			continue
		}
		var n int
		db.QueryRowContext(ctx, `SELECT count(*) FROM leads WHERE agent_id = ? AND preferred_date = ? AND preferred_time = ? AND status NOT IN ('spam', 'closed_lost')`,
			agent, day(now, l.day), l.time).Scan(&n)
		if n > 0 {
			continue
		}
		if err := insertDemoRow(ctx, db, "leads", map[string]any{"listing_id": id, "listing_ref": l.ref, "agent_id": agent, "kind": "viewing",
			"name": l.who.name, "phone": l.who.phone(i), "email": l.who.email(), "message": l.message, "preferred_date": day(now, l.day),
			"preferred_time": l.time, "source": "website", "status": l.status, "notes": "", "lang": l.who.lang}); err != nil {
			return err
		}
	}
	return listingsDemoReviews(ctx, db)
}

// Sample client reviews for the demo only, so its home page shows the reviews
// section. The template handed to owners keeps the table empty: a real site
// shows reviews only when its owner adds real ones.
func listingsDemoReviews(ctx context.Context, db *sql.DB) error {
	var n int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM testimonials`).Scan(&n); err != nil || n > 0 {
		return nil // an older demo without the table, or already filled
	}
	for i, r := range []map[string]any{
		{"quote": "They showed us three villas in one afternoon and told us plainly which one was overpriced. We moved in six weeks later.",
			"quote_ar": "عرضوا علينا ثلاث فلل في ظهيرة واحدة وأخبرونا بصراحة أيها سعرها مبالغ فيه. انتقلنا بعد ستة أسابيع.",
			"name":     "Rebecca L. (sample)", "area": "Arabian Ranches", "area_ar": "المرابع العربية"},
		{"quote": "Every advert had its permit number and the flat looked exactly like the photos. No surprises at the viewing.",
			"quote_ar": "كل إعلان يحمل رقم التصريح والشقة كانت تماماً كما في الصور. لا مفاجآت عند المعاينة.",
			"name":     "Yousef D. (sample)", "area": "Dubai Marina", "area_ar": "دبي مارينا"},
		{"quote": "I booked the viewing online at night and had a WhatsApp confirmation before breakfast.",
			"quote_ar": "حجزت المعاينة عبر الإنترنت ليلاً ووصلني التأكيد عبر واتساب قبل الإفطار.",
			"name":     "Arjun M. (sample)", "area": "Business Bay", "area_ar": "الخليج التجاري"},
	} {
		r["position"], r["active"] = i+1, 1
		if err := insertDemoRow(ctx, db, "testimonials", r); err != nil {
			return err
		}
	}
	return nil
}
