package platform

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Traffic: the owner's dashboard shows how many people visit an app, which
// pages they open and where they come from. Only live pages (and template
// demos) that answer a person's GET with HTML count; bots, the platform's
// own /_rx addresses and errors do not.
//
// Nothing that identifies a visitor is stored: a visitor is counted once a
// day by a hash of a secret that lives only in this process, the app, the
// day, the address and the browser, and the hashes are deleted with the day.
// Hits are added up in memory and written once a minute.
const (
	trafficPagesPerDay   = 300 // further pages of an app in a day count as "(other)"
	trafficSourcesPerDay = 100
	trafficVisitorsCap   = 50000 // per app and day, in memory
	trafficKeepDays      = 400
)

type trafficKey struct{ app, day string }

type trafficDay struct {
	views    int
	visitors map[string]bool
	pages    map[string]int
	sources  map[string]int
}

type trafficBuffer struct {
	mu   sync.Mutex
	salt []byte
	days map[trafficKey]*trafficDay
}

func (p *Platform) ensureTrafficTables() error {
	for _, stmt := range []string{
		`CREATE TABLE IF NOT EXISTS traffic_days (app TEXT NOT NULL, day TEXT NOT NULL, views INTEGER NOT NULL, PRIMARY KEY (app, day))`,
		`CREATE TABLE IF NOT EXISTS traffic_visitors (app TEXT NOT NULL, day TEXT NOT NULL, hash TEXT NOT NULL, PRIMARY KEY (app, day, hash))`,
		`CREATE TABLE IF NOT EXISTS traffic_pages (app TEXT NOT NULL, day TEXT NOT NULL, path TEXT NOT NULL, views INTEGER NOT NULL, PRIMARY KEY (app, day, path))`,
		`CREATE TABLE IF NOT EXISTS traffic_sources (app TEXT NOT NULL, day TEXT NOT NULL, source TEXT NOT NULL, views INTEGER NOT NULL, PRIMARY KEY (app, day, source))`,
	} {
		if _, err := p.db.Exec(stmt); err != nil {
			return err
		}
	}
	p.traffic.salt = make([]byte, 32)
	rand.Read(p.traffic.salt)
	p.traffic.days = map[trafficKey]*trafficDay{}
	return nil
}

// dubaiDay is the UAE date (UTC+4, no daylight saving) of t.
func dubaiDay(t time.Time) string { return t.UTC().Add(4 * time.Hour).Format(time.DateOnly) }

var botWords = []string{"bot", "crawl", "spider", "slurp", "preview", "facebookexternalhit", "headless", "lighthouse", "curl", "wget", "python", "go-http"}

func isBot(ua string) bool {
	ua = strings.ToLower(ua)
	if ua == "" {
		return true
	}
	for _, w := range botWords {
		if strings.Contains(ua, w) {
			return true
		}
	}
	return false
}

// trafficHit counts one answered request, if it was a person viewing a page.
func (p *Platform) trafficHit(app string, r *http.Request, out result) {
	if r.Method != http.MethodGet || out.status != http.StatusOK || !strings.HasPrefix(out.contentType, "text/html") ||
		strings.HasPrefix(r.URL.Path, "/_rx/") || r.Header.Get("Purpose") == "prefetch" || isBot(r.UserAgent()) {
		return
	}
	day := dubaiDay(time.Now())
	path := r.URL.Path
	if len(path) > 120 {
		path = path[:120]
	}
	source := trafficSource(r)
	h := sha256.New()
	h.Write(p.traffic.salt)
	h.Write([]byte(app + "\x00" + day + "\x00" + p.clientIP(r) + "\x00" + r.UserAgent()))
	visitor := hex.EncodeToString(h.Sum(nil)[:12])

	b := &p.traffic
	b.mu.Lock()
	defer b.mu.Unlock()
	k := trafficKey{app, day}
	d := b.days[k]
	if d == nil {
		d = &trafficDay{visitors: map[string]bool{}, pages: map[string]int{}, sources: map[string]int{}}
		b.days[k] = d
	}
	d.views++
	if len(d.visitors) < trafficVisitorsCap {
		d.visitors[visitor] = true
	}
	addCapped(d.pages, path, trafficPagesPerDay)
	addCapped(d.sources, source, trafficSourcesPerDay)
}

func addCapped(m map[string]int, k string, max int) {
	if _, ok := m[k]; !ok && len(m) >= max {
		k = "(other)"
	}
	m[k]++
}

// trafficSource names where a visit came from: an utm_source in the
// address, else the referring site without "www.", else "direct". Visits
// from the app's own pages are not a source.
func trafficSource(r *http.Request) string {
	if s := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("utm_source"))); s != "" {
		if len(s) > 40 {
			s = s[:40]
		}
		return s
	}
	ref, err := url.Parse(r.Referer())
	if err != nil || ref.Host == "" {
		return "direct"
	}
	host := strings.ToLower(ref.Hostname())
	if own, _, _ := strings.Cut(strings.ToLower(r.Host), ":"); host == own {
		return "direct"
	}
	host = strings.TrimPrefix(host, "www.")
	switch {
	case strings.HasPrefix(host, "l.instagram.") || strings.HasPrefix(host, "instagram."):
		return "instagram.com"
	case strings.HasPrefix(host, "l.facebook.") || strings.HasPrefix(host, "m.facebook.") || strings.HasPrefix(host, "lm.facebook."):
		return "facebook.com"
	case host == "t.co":
		return "x.com"
	case strings.HasPrefix(host, "google."):
		return "google"
	}
	return host
}

// flushTraffic writes the counts gathered in memory.
func (p *Platform) flushTraffic() {
	b := &p.traffic
	b.mu.Lock()
	days := b.days
	b.days = map[trafficKey]*trafficDay{}
	b.mu.Unlock()
	if len(days) == 0 {
		return
	}
	tx, err := p.db.Begin()
	if err != nil {
		return
	}
	defer tx.Rollback()
	for k, d := range days {
		tx.Exec(`INSERT INTO traffic_days (app, day, views) VALUES (?, ?, ?) ON CONFLICT (app, day) DO UPDATE SET views = views + excluded.views`, k.app, k.day, d.views)
		for v := range d.visitors {
			tx.Exec(`INSERT OR IGNORE INTO traffic_visitors (app, day, hash) VALUES (?, ?, ?)`, k.app, k.day, v)
		}
		for path, n := range d.pages {
			tx.Exec(`INSERT INTO traffic_pages (app, day, path, views) VALUES (?, ?, ?, ?) ON CONFLICT (app, day, path) DO UPDATE SET views = views + excluded.views`, k.app, k.day, path, n)
		}
		for s, n := range d.sources {
			tx.Exec(`INSERT INTO traffic_sources (app, day, source, views) VALUES (?, ?, ?, ?) ON CONFLICT (app, day, source) DO UPDATE SET views = views + excluded.views`, k.app, k.day, s, n)
		}
	}
	tx.Commit()
}

// RunTraffic writes traffic counts every interval until ctx ends.
func (p *Platform) RunTraffic(ctx context.Context, every time.Duration) {
	if every <= 0 {
		every = time.Minute
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			p.flushTraffic()
			return
		case <-t.C:
			p.flushTraffic()
		}
	}
}

func (p *Platform) cleanTraffic() {
	cut := dubaiDay(time.Now().AddDate(0, 0, -trafficKeepDays))
	for _, t := range []string{"traffic_days", "traffic_visitors", "traffic_pages", "traffic_sources"} {
		p.db.Exec(`DELETE FROM `+t+` WHERE day < ?`, cut)
	}
}

// DayCount is one day of a daily series, oldest first.
type DayCount struct {
	Day   string  // YYYY-MM-DD, UAE date
	Count int     // visitors, payments, sign-ups ...
	Views int     // page views (traffic only)
	Value float64 // amount (sales only)
}

// Named is a label with a count, for top lists.
type Named struct {
	Name  string
	Count int
}

// TrafficReport is what the dashboard shows about visits over a period.
type TrafficReport struct {
	Days               []DayCount // one per day, oldest first, zeros included
	Views, Visitors    int        // totals over the period (visitors summed per day)
	PrevViews, PrevVis int        // the same for the period before, for the change
	Pages, Sources     []Named    // the most viewed pages and the biggest sources
}

// Traffic reports an app's visits over the last days days, today included.
func (p *Platform) Traffic(app string, days int) TrafficReport {
	p.flushTraffic()
	now := time.Now()
	from := dubaiDay(now.AddDate(0, 0, -(days - 1)))
	prevFrom := dubaiDay(now.AddDate(0, 0, -(2*days - 1)))
	rep := TrafficReport{Days: emptyDays(now, days)}
	idx := map[string]int{}
	for i, d := range rep.Days {
		idx[d.Day] = i
	}
	rows, err := p.db.Query(`SELECT d.day, d.views, (SELECT count(*) FROM traffic_visitors v WHERE v.app = d.app AND v.day = d.day)
		FROM traffic_days d WHERE d.app = ? AND d.day >= ? ORDER BY d.day`, app, prevFrom)
	if err == nil {
		for rows.Next() {
			var day string
			var views, vis int
			if rows.Scan(&day, &views, &vis) != nil {
				continue
			}
			if i, ok := idx[day]; ok {
				rep.Days[i].Views, rep.Days[i].Count = views, vis
				rep.Views += views
				rep.Visitors += vis
			} else if day < from {
				rep.PrevViews += views
				rep.PrevVis += vis
			}
		}
		rows.Close()
	}
	rep.Pages = p.topTraffic("traffic_pages", "path", app, from, 8)
	rep.Sources = p.topTraffic("traffic_sources", "source", app, from, 8)
	return rep
}

func (p *Platform) topTraffic(table, col, app, from string, n int) []Named {
	rows, err := p.db.Query(`SELECT `+col+`, sum(views) AS n FROM `+table+` WHERE app = ? AND day >= ? GROUP BY `+col+` ORDER BY n DESC, `+col+` LIMIT ?`, app, from, n)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []Named
	for rows.Next() {
		var x Named
		if rows.Scan(&x.Name, &x.Count) == nil {
			out = append(out, x)
		}
	}
	return out
}

// emptyDays is days zeroed DayCounts ending today (UAE date).
func emptyDays(now time.Time, days int) []DayCount {
	out := make([]DayCount, days)
	for i := range out {
		out[i].Day = dubaiDay(now.AddDate(0, 0, i-(days-1)))
	}
	return out
}
