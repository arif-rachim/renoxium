package web

import (
	"fmt"
	"math"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/arif-rachim/renoxium/internal/accounts"
	"github.com/arif-rachim/renoxium/internal/applog"
	"github.com/arif-rachim/renoxium/internal/platform"
)

// The owner's console for one app: a side navigation (a row of chips on a
// phone), a dashboard with charts, and tables that turn into cards on a
// small screen. Every list is paged in the database.

// shellLink is one entry of an app's side navigation.
type shellLink struct {
	Label, Href string
	Current     bool
}

// appShell is what every page of an app shows around its content.
type appShell struct {
	App, Title, Status, StatusText, SiteURL string
	Demo, IsOwner                           bool
	Throttle                                string // the operator's throttle notice, while it lasts
	Main, Data, Business, Settings          []shellLink
}

// shell builds the navigation of an app; current is the href of this page.
func (s *Site) shell(u accounts.User, app, title, current string, demo bool) appShell {
	sh := appShell{App: app, Title: title, Demo: demo, IsOwner: !demo && s.isOwnerOf(u, app)}
	if info, err := s.platform.Info(app); err == nil {
		sh.Status, sh.StatusText = appStatus(info)
		if info.LiveVersion > 0 || demo {
			sh.SiteURL = info.LiveURL
		}
		if demo {
			sh.Status, sh.StatusText = "plain", "Template demo, read-only"
		}
	}
	base := "/apps/" + app
	link := func(label, href string) shellLink {
		return shellLink{Label: label, Href: href, Current: href == current}
	}
	sh.Main = []shellLink{link("Overview", base)}
	if tables, err := s.platform.Tables(app); err == nil {
		for _, t := range tables {
			l := link(tableLabel(t), base+"/t/"+t.Name)
			l.Current = strings.HasPrefix(current, l.Href+"/") || current == l.Href
			sh.Data = append(sh.Data, l)
		}
	}
	if demo {
		return sh
	}
	if t, ok := s.platform.ThrottleOf(app); ok {
		sh.Throttle = t.Notice()
	}
	if sh.IsOwner {
		sh.Business = append(sh.Business, link("Payments", base+"/payments"))
	}
	sh.Business = append(sh.Business, link("Customers", base+"/visitors"))
	if sh.IsOwner {
		sh.Settings = []shellLink{link("Notifications", base+"/notifications"), link("Staff", base+"/staff"),
			link("Domain", base+"/domain"), link("Connections", base+"/connections")}
	}
	return sh
}

// --- pagination ---

type pageLink struct {
	Label, Href    string
	Current, Ellip bool
}

// pager describes one page of a list: "1–50 of 1,234" and the links to
// other pages, keeping every other query parameter.
type pager struct {
	Page, Pages, Total, From, To int
	Of                           string // the total, written 1,234
	Prev, Next                   string
	Links                        []pageLink
}

func newPager(r *http.Request, page, size, total int) pager {
	p := pager{Page: page, Total: total, Of: fmtInt(total), Pages: max(1, int(math.Ceil(float64(total)/float64(size))))}
	if total > 0 {
		p.From, p.To = (page-1)*size+1, min(page*size, total)
	}
	href := func(n int) string {
		q := r.URL.Query()
		q.Set("page", strconv.Itoa(n))
		return "?" + q.Encode()
	}
	if page > 1 {
		p.Prev = href(page - 1)
	}
	if page < p.Pages {
		p.Next = href(page + 1)
	}
	last := 0
	for n := 1; n <= p.Pages; n++ {
		if n == 1 || n == p.Pages || (n >= page-1 && n <= page+1) {
			if last != 0 && n > last+1 {
				p.Links = append(p.Links, pageLink{Ellip: true, Label: "…"})
			}
			p.Links = append(p.Links, pageLink{Label: strconv.Itoa(n), Href: href(n), Current: n == page})
			last = n
		}
	}
	return p
}

func pageParam(r *http.Request) int {
	n, _ := strconv.Atoi(r.URL.Query().Get("page"))
	return max(n, 1)
}

// --- charts ---

// bar is one day of a bar chart. Height is a percentage of the tallest.
type bar struct {
	Height float64
	Tip    string
	Today  bool
}

// chart is a daily bar chart drawn with HTML, with its axis labels and a
// table for screen readers and anyone who prefers numbers.
type chart struct {
	Title, Total, Max, Empty string
	Bars                     []bar
	Ticks                    []string // dates under the first, middle and last bar
	Rows                     [][2]string
}

func dayLabel(day string) string {
	t, err := time.Parse(time.DateOnly, day)
	if err != nil {
		return day
	}
	return t.Format("Mon 2 Jan")
}

func shortDay(day string) string {
	t, err := time.Parse(time.DateOnly, day)
	if err != nil {
		return day
	}
	return t.Format("2 Jan")
}

func newChart(title string, days []platform.DayCount, value func(platform.DayCount) float64, tip func(platform.DayCount) string,
	format func(float64) string, empty string) chart {
	c := chart{Title: title, Empty: empty}
	var top, sum float64
	for _, d := range days {
		v := value(d)
		top = max(top, v)
		sum += v
	}
	c.Total = format(sum)
	if top == 0 {
		return c
	}
	c.Max = format(top)
	for i, d := range days {
		v := value(d)
		h := v / top * 100
		if v > 0 && h < 2 {
			h = 2 // a small day stays visible
		}
		c.Bars = append(c.Bars, bar{Height: math.Round(h*10) / 10, Tip: dayLabel(d.Day) + ": " + tip(d), Today: i == len(days)-1})
		c.Rows = append(c.Rows, [2]string{dayLabel(d.Day), tip(d)})
	}
	if n := len(days); n > 0 {
		c.Ticks = []string{shortDay(days[0].Day), shortDay(days[n/2].Day), "Today"}
	}
	return c
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmtInt(n) + " " + many
}

// fmtInt writes 12345 as 12,345.
func fmtInt(n int) string {
	s := strconv.Itoa(n)
	if n < 0 {
		return "-" + fmtInt(-n)
	}
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

func fmtMoney(cur string, v float64) string {
	whole := int(v)
	cents := int(math.Round((v - float64(whole)) * 100))
	if cents == 100 {
		whole, cents = whole+1, 0
	}
	return fmt.Sprintf("%s %s.%02d", cur, fmtInt(whole), cents)
}

// kpi is a headline figure with its change against the period before.
type kpi struct {
	Label, Value, Note, Change string
	Up, Down                   bool
}

func change(now, before int) (text string, up, down bool) {
	switch {
	case before == 0 && now == 0:
		return "", false, false
	case before == 0:
		return "new", true, false
	}
	pct := int(math.Round(float64(now-before) / float64(before) * 100))
	switch {
	case pct > 0:
		return fmt.Sprintf("+%d%%", pct), true, false
	case pct < 0:
		return fmt.Sprintf("%d%%", pct), false, true
	}
	return "0%", false, false
}

// share is a row of a top list with its width relative to the first.
type share struct {
	Name  string
	Count string
	Width float64
}

func shares(list []platform.Named) []share {
	var out []share
	for i, x := range list {
		w := 100.0
		if i > 0 && list[0].Count > 0 {
			w = math.Max(3, float64(x.Count)/float64(list[0].Count)*100)
		}
		out = append(out, share{Name: x.Name, Count: fmtInt(x.Count), Width: w})
	}
	return out
}

// --- the dashboard ---

var periods = []int{7, 30, 90}

func (s *Site) adminApp(w http.ResponseWriter, r *http.Request) {
	u, app, demo, ok := s.viewer(w, r)
	if !ok {
		return
	}
	info, err := s.platform.Info(app)
	if err != nil {
		serverError(w, err)
		return
	}
	days, _ := strconv.Atoi(r.URL.Query().Get("days"))
	if !slices.Contains(periods, days) {
		days = 30
	}
	ctx := r.Context()
	sh := s.shell(u, app, app, "/apps/"+app, demo)

	traffic := s.platform.Traffic(app, days)
	currency, sales, _ := s.platform.SalesDaily(ctx, app, days)
	signups, _ := s.platform.SignupsDaily(ctx, app, days)
	var salesSum float64
	var salesCount, signupSum int
	for _, d := range sales {
		salesSum += d.Value
		salesCount += d.Count
	}
	for _, d := range signups {
		signupSum += d.Count
	}
	_, customers, _ := s.platform.Visitors(ctx, app, "", 1)
	vc, vu, vd := change(traffic.Visitors, traffic.PrevVis)
	pc, pu, pd := change(traffic.Views, traffic.PrevViews)
	kpis := []kpi{
		{Label: "Visitors", Value: fmtInt(traffic.Visitors), Note: "people, counted once a day", Change: vc, Up: vu, Down: vd},
		{Label: "Page views", Value: fmtInt(traffic.Views), Change: pc, Up: pu, Down: pd},
		{Label: "Card sales", Value: fmtMoney(currency, salesSum), Note: plural(salesCount, "payment", "payments")},
		{Label: "New customer accounts", Value: fmtInt(signupSum), Note: fmtInt(customers) + " signed up in all"},
	}
	visitorsChart := newChart("Visitors per day", traffic.Days,
		func(d platform.DayCount) float64 { return float64(d.Count) },
		func(d platform.DayCount) string {
			return plural(d.Count, "visitor", "visitors") + ", " + plural(d.Views, "page view", "page views")
		},
		func(v float64) string { return fmtInt(int(v)) },
		"No visits yet. Share your site's address on Instagram, WhatsApp or Google Business to bring people in.")
	salesChart := newChart("Card sales per day", sales,
		func(d platform.DayCount) float64 { return d.Value },
		func(d platform.DayCount) string {
			return fmtMoney(currency, d.Value) + " from " + plural(d.Count, "payment", "payments")
		},
		func(v float64) string { return fmtMoney(currency, v) },
		"No card payments in this period. Cash-on-delivery orders are in your orders table.")

	stats, _ := s.platform.TableStats(ctx, app)
	tables, _ := s.platform.Tables(app)
	type tile struct {
		Label, Href, Rows, Week string
	}
	var tiles []tile
	for i, t := range tables {
		ti := tile{Label: tableLabel(t), Href: "/apps/" + app + "/t/" + t.Name}
		if i < len(stats) {
			ti.Rows = plural(stats[i].Rows, "row", "rows")
			if stats[i].Week > 0 {
				ti.Week = "+" + fmtInt(stats[i].Week) + " this week"
			}
		}
		tiles = append(tiles, ti)
	}
	var recent []platform.Payment
	if sh.IsOwner {
		recent, _, _ = s.platform.PaymentsPage(ctx, app, "", 1)
		if len(recent) > 5 {
			recent = recent[:5]
		}
	}
	var problems []applog.Event
	if !demo {
		problems, _ = s.platform.Logs(app, applog.Filter{Stage: platform.Live, Level: "error", Limit: 5})
	}
	periodLinks := []shellLink{}
	for _, p := range periods {
		periodLinks = append(periodLinks, shellLink{Label: fmt.Sprintf("%d days", p), Href: fmt.Sprintf("?days=%d", p), Current: p == days})
	}
	render(w, http.StatusOK, "admin_app", map[string]any{
		"Shell": sh, "Info": info, "Days": days, "Periods": periodLinks, "KPIs": kpis,
		"VisitorsChart": visitorsChart, "SalesChart": salesChart,
		"Pages": shares(traffic.Pages), "Sources": shares(traffic.Sources),
		"Tiles": tiles, "Recent": recent, "Problems": problems,
		"MCPURL": s.platform.LiveMCPURL(app), "Jobs": s.platform.JobStates(app),
		"Example": strings.TrimPrefix(app, platform.DemoPrefix),
	})
}

// --- tables ---

type column struct {
	Name, Label, Type string
	SortHref, SortDir string // the link that sorts by this column, and "asc"/"desc" when it is the sort
	Shown             bool
}

// cell is one value of a row as the list shows it.
type cell struct {
	Label, Text, Image, Pill string
	Title                    bool // the row's name in the card view
}

type listRow struct {
	ID    int64
	Cells []cell
}

// defaultColumns picks what a table shows before the owner chooses: up to
// five columns, skipping long text, JSON and links to other rows (*_id),
// which read badly in a row, with a name-like column first: on a phone it
// becomes the title of the row's card.
func defaultColumns(cols []string, types map[string]string) []string {
	var out []string
	for _, c := range cols {
		if t := types[c]; t == "json" || longText(c) || strings.HasSuffix(c, "_id") {
			continue
		}
		if out = append(out, c); len(out) == 5 {
			break
		}
	}
	for i, c := range out {
		if nameLike(c) {
			copy(out[1:i+1], out[:i])
			out[0] = c
			break
		}
	}
	return out
}

func nameLike(col string) bool {
	for _, w := range []string{"name", "title", "reference", "subject", "code"} {
		if strings.Contains(col, w) {
			return true
		}
	}
	return false
}

func (s *Site) adminList(w http.ResponseWriter, r *http.Request) {
	u, app, demo, ok := s.viewer(w, r)
	if !ok {
		return
	}
	table := r.PathValue("table")
	t, err := s.table(app, table)
	if err != nil {
		notFound(w, err)
		return
	}
	qv := r.URL.Query()
	if picked := qv["c"]; len(picked) > 0 {
		// The column picker sends one c per column; keep them as one cols= so every link carries the choice.
		qv.Del("c")
		qv.Del("page")
		qv.Set("cols", strings.Join(picked, ","))
		// An absolute path: Go resolves "?..." against the parent directory.
		http.Redirect(w, r, r.URL.Path+"?"+qv.Encode(), http.StatusSeeOther)
		return
	}
	q := platform.ListQuery{Search: qv.Get("q"), Page: pageParam(r), Sort: qv.Get("sort"), Asc: qv.Get("dir") == "asc"}
	q.PageSize, _ = strconv.Atoi(qv.Get("size"))
	if !slices.Contains(platform.PageSizes, q.PageSize) {
		q.PageSize = platform.AdminPageSize
	}
	rows, total, err := s.platform.AdminQuery(r.Context(), app, table, q)
	if err != nil {
		serverError(w, err)
		return
	}
	types := map[string]string{"id": "integer", "created_at": "datetime"}
	labels := map[string]string{"id": "#", "created_at": "Created"}
	options := map[string]bool{}
	var names []string
	for _, c := range t.Columns {
		types[c.Name], labels[c.Name] = c.Type, columnLabel(c)
		options[c.Name] = len(c.Options) > 0
		names = append(names, c.Name)
	}
	// The columns to show: the owner's choice (cols=a,b) or the default.
	var shown []string
	if raw := qv.Get("cols"); raw != "" {
		for _, c := range strings.Split(raw, ",") {
			if _, ok := types[c]; ok && c != "id" && !slices.Contains(shown, c) {
				shown = append(shown, c)
			}
		}
	}
	if len(shown) == 0 {
		shown = append(defaultColumns(names, types), "created_at")
	}
	sortHref := func(col string) (string, string) {
		v := r.URL.Query()
		v.Del("page")
		v.Set("sort", col)
		dir := ""
		if q.Sort == col || (q.Sort == "" && col == "id") {
			dir = "desc"
			if q.Asc {
				dir = "asc"
			}
		}
		if dir == "desc" {
			v.Set("dir", "asc")
		} else {
			v.Del("dir")
		}
		return "?" + v.Encode(), dir
	}
	var cols []column
	idHref, idDir := sortHref("id")
	cols = append(cols, column{Name: "id", Label: "#", SortHref: idHref, SortDir: idDir, Shown: true})
	for _, c := range shown {
		h, d := sortHref(c)
		cols = append(cols, column{Name: c, Label: labels[c], Type: types[c], SortHref: h, SortDir: d, Shown: true})
	}
	var choices []column
	for _, c := range append(slices.Clone(names), "created_at") {
		choices = append(choices, column{Name: c, Label: labels[c], Shown: slices.Contains(shown, c)})
	}
	isOwner := !demo && s.isOwnerOf(u, app)
	liveURL := ""
	if info, err := s.platform.Info(app); err == nil {
		liveURL = info.LiveURL
	}
	var list []listRow
	for _, row := range rows {
		lr := listRow{ID: row["id"].(int64)}
		for i, c := range cols[1:] {
			ce := cell{Label: c.Label, Title: i == 0}
			switch types[c.Name] {
			case "file":
				name := s.privateFileName(app, row[c.Name])
				if !isOwner && name != "" && name != "(file deleted)" {
					name = "Document" // staff do not see visitors' documents, not even their names
				}
				ce.Text = short(name)
			case "image":
				if v, _ := row[c.Name].(string); v != "" {
					ce.Image = liveURL + v + "?w=96"
				}
			default:
				text := short(display(row[c.Name], types[c.Name]))
				if (options[c.Name] || types[c.Name] == "boolean") && text != "" {
					ce.Pill = text
				} else {
					ce.Text = text
				}
			}
			lr.Cells = append(lr.Cells, ce)
		}
		list = append(list, lr)
	}
	keep := url.Values{}
	for _, k := range []string{"q", "sort", "dir", "size"} {
		if v := qv.Get(k); v != "" {
			keep.Set(k, v)
		}
	}
	sh := s.shell(u, app, tableLabel(t), "/apps/"+app+"/t/"+table, demo)
	render(w, http.StatusOK, "admin_list", map[string]any{
		"Shell": sh, "App": app, "Table": table, "Label": tableLabel(t), "Columns": cols, "Rows": list, "Choices": choices,
		"Query": q.Search, "Total": total, "Pager": newPager(r, q.Page, q.PageSize, total), "Size": q.PageSize, "Sizes": platform.PageSizes,
		"Keep": keep, "ColsParam": qv.Get("cols"),
		"HasCalendar": t.Calendar != nil, "Notice": s.hookNotice(r, app), "Demo": demo,
	})
}

func (s *Site) adminVisitors(w http.ResponseWriter, r *http.Request) {
	u, app, ok := s.member(w, r)
	if !ok {
		return
	}
	page := pageParam(r)
	q := r.URL.Query().Get("q")
	list, total, err := s.platform.Visitors(r.Context(), app, q, page)
	if err != nil {
		serverError(w, err)
		return
	}
	render(w, http.StatusOK, "admin_visitors", map[string]any{
		"Shell": s.shell(u, app, "Customers", "/apps/"+app+"/visitors", false),
		"App":   app, "Visitors": list, "Total": total, "Query": q, "Pager": newPager(r, page, platform.AdminPageSize, total), "CSRF": s.csrfToken(w, r),
	})
}
