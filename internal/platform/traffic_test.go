package platform_test

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/arif-rachim/renoxium/internal/platform"
)

// Visits to live pages are counted per person per day, with the pages and
// where people came from; bots, previews and the platform's own pages are not.
func TestTrafficCountsPeopleOnLivePages(t *testing.T) {
	p := newPlatform(t)
	ctx := context.Background()
	p.CreateApp("usr_test", "al-noor")
	p.ApplyChange(ctx, "al-noor", catalog(t))
	if _, err := p.Publish("al-noor", 0); err != nil {
		t.Fatal(err)
	}
	visit := func(host, path, ua, ref, ip string) {
		r := httptest.NewRequest("GET", "http://"+host+path, nil)
		r.Header.Set("User-Agent", ua)
		if ref != "" {
			r.Header.Set("Referer", ref)
		}
		r.RemoteAddr = ip + ":1234"
		w := httptest.NewRecorder()
		p.ServeApp(w, r)
	}
	const chrome = "Mozilla/5.0 (iPhone) Safari/604.1"
	visit(liveHost, "/", chrome, "https://l.instagram.com/x", "10.0.0.1")
	visit(liveHost, "/", chrome, "", "10.0.0.1") // the same person again
	visit(liveHost, "/products/1", chrome, "https://"+liveHost+"/", "10.0.0.1")
	visit(liveHost, "/?utm_source=WhatsApp", chrome, "", "10.0.0.2")
	visit(liveHost, "/", "Googlebot/2.1", "", "10.0.0.3")      // a bot
	visit(previewHost, "/", chrome, "", "10.0.0.4")            // preview
	visit(liveHost, "/no-such-page", chrome, "", "10.0.0.5")   // not found
	visit(liveHost, "/_rx/auth/login", chrome, "", "10.0.0.6") // the platform's own page

	rep := p.Traffic("al-noor", 7)
	if len(rep.Days) != 7 || rep.Visitors != 2 || rep.Views != 4 {
		t.Fatalf("visitors %d, views %d over %d days", rep.Visitors, rep.Views, len(rep.Days))
	}
	if today := rep.Days[6]; today.Count != 2 || today.Views != 4 {
		t.Fatalf("today: %+v", today)
	}
	pages := map[string]int{}
	for _, x := range rep.Pages {
		pages[x.Name] = x.Count
	}
	if pages["/"] != 3 || pages["/products/1"] != 1 || len(pages) != 2 {
		t.Fatalf("pages: %+v", rep.Pages)
	}
	sources := map[string]int{}
	for _, x := range rep.Sources {
		sources[x.Name] = x.Count
	}
	if sources["instagram.com"] != 1 || sources["whatsapp"] != 1 || sources["direct"] != 2 {
		t.Fatalf("sources: %+v", rep.Sources)
	}
	// Counted once: reading again adds nothing.
	if again := p.Traffic("al-noor", 7); again.Views != 4 {
		t.Fatalf("views read twice: %d", again.Views)
	}
}

// A table is paged, sorted and sized in the database.
func TestAdminQuery(t *testing.T) {
	p := newPlatform(t)
	ctx := context.Background()
	p.CreateApp("usr_test", "al-noor")
	p.ApplyChange(ctx, "al-noor", catalog(t))
	p.Publish("al-noor", 0)
	for i := 0; i < 60; i++ {
		if _, err := p.AdminSave(ctx, "al-noor", "products", 0, map[string]string{"name": string(rune('A' + i%26)), "price_aed": "1"}, "owner@x.ae"); err != nil {
			t.Fatal(err)
		}
	}
	rows, total, err := p.AdminQuery(ctx, "al-noor", "products", platform.ListQuery{Page: 2, PageSize: 25})
	if err != nil || total < 60 || len(rows) != 25 {
		t.Fatalf("page 2: %d rows of %d: %v", len(rows), total, err)
	}
	rows, _, _ = p.AdminQuery(ctx, "al-noor", "products", platform.ListQuery{Sort: "name", Asc: true, PageSize: 25})
	if rows[0]["name"].(string) > rows[24]["name"].(string) {
		t.Fatalf("not sorted by name: %v .. %v", rows[0]["name"], rows[24]["name"])
	}
	// An unknown column or page size falls back to the defaults instead of reaching the SQL.
	rows, _, err = p.AdminQuery(ctx, "al-noor", "products", platform.ListQuery{Sort: "name; DROP TABLE products", PageSize: 7})
	if err != nil || len(rows) != platform.AdminPageSize {
		t.Fatalf("bad sort or size: %d rows, %v", len(rows), err)
	}
}
