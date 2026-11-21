package platform_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/arif-rachim/renoxium/internal/appdef"
	"github.com/arif-rachim/renoxium/internal/platform"
)

func calendarDef() *appdef.App {
	return &appdef.App{
		Tables: []appdef.Table{
			{Name: "items", Columns: []appdef.Column{{Name: "name", Type: "text"}}},
			{Name: "reservations", Calendar: &appdef.Calendar{Date: "start_date", EndDate: "end_date", Start: "time", Group: "item_id",
				Title: appdef.Columns{"customer_name"}, Status: "status", HideStatus: []string{"cancelled"}},
				Columns: []appdef.Column{
					{Name: "start_date", Type: "text"}, {Name: "end_date", Type: "text"}, {Name: "time", Type: "text"},
					{Name: "item_id", Type: "integer"}, {Name: "customer_name", Type: "text"},
					{Name: "status", Type: "text", Options: []string{"confirmed", "cancelled"}},
				}},
			{Name: "visits", Calendar: &appdef.Calendar{Date: "at"}, Columns: []appdef.Column{{Name: "at", Type: "datetime"}}},
			{Name: "notes", Columns: []appdef.Column{{Name: "body", Type: "text"}}},
		},
		Seed: map[string][]map[string]any{
			"items": {{"name": "Desert safari"}, {"name": "Jet ski"}},
			"reservations": {
				{"start_date": "2026-12-01", "time": "09:00", "item_id": 1, "customer_name": "Before", "status": "confirmed"},
				{"start_date": "2026-12-03", "time": "15:00", "item_id": 1, "customer_name": "Inside", "status": "confirmed"},
				{"start_date": "2026-11-28", "end_date": "2026-12-04", "item_id": 2, "customer_name": "Overlap", "status": "confirmed"},
				{"start_date": "2026-12-03", "end_date": "", "item_id": 2, "customer_name": "Empty end", "status": "confirmed"},
				{"start_date": "2026-12-08", "end_date": "2026-12-09", "item_id": 2, "customer_name": "After", "status": "confirmed"},
				{"start_date": "2026-12-03", "time": "10:00", "item_id": 1, "customer_name": "Gone", "status": "cancelled"},
			},
			"visits": {{"at": "2026-12-03 08:30:00"}, {"at": "2026-12-04 08:30:00"}},
		},
		Templates: map[string]string{"t": "x"},
		Routes:    []appdef.Route{{Method: "GET", Path: "/", Template: "t"}},
	}
}

func TestAdminCalendar(t *testing.T) {
	p := newPlatform(t)
	ctx := context.Background()
	p.CreateApp("usr_test", "tours")
	if res, err := p.ApplyChange(ctx, "tours", calendarDef()); err != nil || !res.OK {
		t.Fatal(err, res)
	}
	// Not published yet: like the admin list, the calendar reads live only.
	if _, err := p.AdminCalendar(ctx, "tours", "reservations", "2026-12-03", "2026-12-03", false); err == nil {
		t.Fatal("calendar of an unpublished app")
	}
	if _, err := p.Publish("tours", 0); err != nil {
		t.Fatal(err)
	}
	names := func(r platform.CalendarRows) string {
		var out []string
		for _, row := range r.Rows {
			out = append(out, row["customer_name"].(string))
		}
		return strings.Join(out, ",")
	}

	// One day: rows on it and ranges overlapping it, ordered by day and time, hidden status left out.
	res, err := p.AdminCalendar(ctx, "tours", "reservations", "2026-12-03", "2026-12-03", false)
	if err != nil {
		t.Fatal(err)
	}
	if got := names(res); got != "Overlap,Empty end,Inside" {
		t.Fatalf("day: %s", got)
	}
	if res.Capped || res.Lanes[1] != "Desert safari" || res.Lanes[2] != "Jet ski" || res.Table.Calendar == nil {
		t.Fatalf("day: %+v", res)
	}
	res, _ = p.AdminCalendar(ctx, "tours", "reservations", "2026-12-03", "2026-12-03", true)
	if got := names(res); got != "Overlap,Empty end,Gone,Inside" {
		t.Fatalf("with hidden: %s", got)
	}
	// A week: the range that ends before it is out, the one that starts after it too.
	res, _ = p.AdminCalendar(ctx, "tours", "reservations", "2026-11-30", "2026-12-06", false)
	if got := names(res); got != "Overlap,Before,Empty end,Inside" {
		t.Fatalf("week: %s", got)
	}
	res, _ = p.AdminCalendar(ctx, "tours", "reservations", "2026-12-05", "2026-12-07", false)
	if got := names(res); got != "" {
		t.Fatalf("empty days: %s", got)
	}

	// A datetime date column uses its date part.
	res, err = p.AdminCalendar(ctx, "tours", "visits", "2026-12-03", "2026-12-03", false)
	if err != nil || len(res.Rows) != 1 || res.Rows[0]["at"] != "2026-12-03 08:30:00" {
		t.Fatalf("visits: %v %v", res.Rows, err)
	}

	// Bad input and tables without a calendar.
	for _, c := range [][3]string{{"reservations", "3 Dec", "2026-12-03"}, {"reservations", "2026-12-04", "2026-12-03"}, {"notes", "2026-12-03", "2026-12-03"}, {"nope", "2026-12-03", "2026-12-03"}} {
		if _, err := p.AdminCalendar(ctx, "tours", c[0], c[1], c[2], false); err == nil {
			t.Errorf("%v: no error", c)
		}
	}
	if _, err := p.AdminCalendar(ctx, "tours", "notes", "2026-12-03", "2026-12-03", false); !errors.Is(err, platform.ErrNotFound) {
		t.Errorf("no calendar: %v", err)
	}

	// More rows than the limit: the first CalendarLimit, and Capped.
	for i := range platform.CalendarLimit + 5 {
		if _, err := p.AdminSave(ctx, "tours", "reservations", 0, map[string]string{"start_date": "2026-12-20", "time": fmt.Sprintf("%02d:%02d", 8+i/60%12, i%60),
			"item_id": "1", "customer_name": fmt.Sprint("Guest ", i), "status": "confirmed"}, "owner@tours.ae"); err != nil {
			t.Fatal(err)
		}
	}
	res, _ = p.AdminCalendar(ctx, "tours", "reservations", "2026-12-20", "2026-12-20", false)
	if len(res.Rows) != platform.CalendarLimit || !res.Capped {
		t.Fatalf("cap: %d %v", len(res.Rows), res.Capped)
	}
}
