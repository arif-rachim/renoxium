package web

import (
	"cmp"
	"fmt"
	"hash/fnv"
	"html/template"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/arif-rachim/renoxium/internal/appdef"
	"github.com/arif-rachim/renoxium/internal/platform"
)

// calendarZone is the time zone "today" is taken in. Stored dates are
// local strings and are shown as stored.
const calendarZone = "Asia/Dubai"

const calendarColours = 8

type calItem struct {
	ID                       int64
	Title, Time, Lane, Range string
	Status, Class, Link      string
	ShowLane                 bool
	Top, Height, Left, Width int // day view: px from the axis start, and percent of the lane
	ColStart, ColSpan        int // week view ranges: grid columns 1-7
	first, last              string
	start, end               int // minutes after midnight; -1 when unknown
	laneKey                  string
	laneOrder                int64
	unassigned, hidden       bool
}

type calLane struct {
	Name          string
	AllDay, Timed []calItem
	key           string
	order         int64
	unassigned    bool
}

type calHour struct {
	Label string
	Top   int
}

type calDay struct {
	Date, Label, Link string
	Today             bool
	Items             []calItem
}

type calStatus struct{ Name, Class string }

// adminCalendar shows the rows of a table with calendar metadata as a day
// (lanes by group, along a time axis) or a week (seven columns).
func (s *Site) adminCalendar(w http.ResponseWriter, r *http.Request) {
	u, app, demo, ok := s.viewer(w, r)
	if !ok {
		return
	}
	table := r.PathValue("table")
	t, err := s.table(app, table)
	if err != nil || t.Calendar == nil {
		notFound(w, err)
		return
	}
	q := r.URL.Query()
	view := "day"
	if q.Get("view") == "week" {
		view = "week"
	}
	all := q.Get("all") == "1"
	loc, err := time.LoadLocation(calendarZone)
	if err != nil {
		loc = time.UTC
	}
	now := time.Now().In(loc)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	day, err := time.Parse(time.DateOnly, q.Get("date"))
	if err != nil || day.Year() < 2000 || day.Year() > 2200 {
		day = today
	}
	from, to, step := day, day, 1
	if view == "week" {
		from = day.AddDate(0, 0, -((int(day.Weekday()) + 6) % 7)) // Monday
		to, step = from.AddDate(0, 0, 6), 7
	}
	res, err := s.platform.AdminCalendar(r.Context(), app, table, from.Format(time.DateOnly), to.Format(time.DateOnly), all)
	if err != nil {
		serverError(w, err)
		return
	}
	base := "/apps/" + app + "/t/" + table + "/calendar"
	link := func(view string, d time.Time, all bool) string {
		v := url.Values{"view": {view}, "date": {d.Format(time.DateOnly)}}
		if all {
			v.Set("all", "1")
		}
		return base + "?" + v.Encode()
	}
	self := link(view, day, all)
	items, statuses := calendarItems(app, t, res, self)

	title := tableLabel(t)
	if view == "week" {
		title += ", week of " + from.Format("2 Jan 2006")
	} else {
		title += ", " + day.Format("Monday 2 Jan 2006")
	}
	data := map[string]any{
		"App": app, "Table": table, "Label": tableLabel(t), "Title": title, "View": view, "Date": day.Format(time.DateOnly),
		"Self": self, "Prev": link(view, day.AddDate(0, 0, -step), all), "Next": link(view, day.AddDate(0, 0, step), all),
		"Today": link(view, today, all), "DayLink": link("day", day, all), "WeekLink": link("week", day, all),
		"Toggle": link(view, day, !all), "All": all, "Hidden": strings.Join(t.Calendar.HideStatus, ", "),
		"Capped": res.Capped, "Limit": platform.CalendarLimit, "Statuses": statuses, "Empty": len(items) == 0,
		"AddLink": "/apps/" + app + "/t/" + table + "/new?back=" + url.QueryEscape(self),
		"Demo":    demo,
		"Shell":   s.shell(u, app, tableLabel(t), "/apps/"+app+"/t/"+table, demo),
	}
	if view == "day" {
		lanes, hours, height := dayView(items, day.Format(time.DateOnly))
		data["Lanes"], data["Hours"], data["AxisHeight"] = lanes, hours, height
		data["Grouped"] = t.Calendar.Group != ""
	} else {
		data["Days"], data["Ranges"] = weekView(items, from, today, func(d time.Time) string { return link("day", d, all) })
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	calendarPages.ExecuteTemplate(w, "admin_calendar", data)
}

// calendarItems turns rows into items, sorted by day, start and id.
func calendarItems(app string, t appdef.Table, res platform.CalendarRows, back string) ([]calItem, []calStatus) {
	c := t.Calendar
	var statusOptions []string
	for _, col := range t.Columns {
		if col.Name == c.Status {
			statusOptions = col.Options
		}
	}
	seen := map[string]bool{}
	var statuses []calStatus
	var items []calItem
	for _, row := range res.Rows {
		id, _ := row["id"].(int64)
		raw := display(row[c.Date], t.ColumnType(c.Date))
		if len(raw) < 10 {
			continue
		}
		it := calItem{ID: id, first: raw[:10], start: -1, end: -1}
		it.last = it.first
		if c.EndDate != "" {
			if e := display(row[c.EndDate], ""); len(e) >= 10 && e[:10] > it.first {
				it.last = e[:10]
			}
		}
		switch {
		case c.Start != "":
			it.start = clock(display(row[c.Start], ""))
		case t.ColumnType(c.Date) == "datetime" && len(raw) >= 16:
			it.start = clock(raw[11:16])
		}
		if it.start >= 0 {
			if c.End != "" {
				it.end = clock(display(row[c.End], ""))
			} else if c.Duration != "" {
				switch d := row[c.Duration].(type) {
				case int64:
					it.end = it.start + int(d)
				case float64:
					it.end = it.start + int(d)
				}
			}
			if it.end <= it.start {
				it.end = -1
			}
			it.end = min(it.end, 24*60)
		}
		if it.start >= 0 {
			it.Time = hhmm(it.start)
			if it.end > 0 {
				it.Time += "–" + hhmm(it.end)
			}
		}
		if it.last != it.first {
			it.Range = shortDate(it.first) + " → " + shortDate(it.last)
		}
		var parts []string
		for _, col := range c.TitleColumns() {
			if v := strings.TrimSpace(display(row[col], t.ColumnType(col))); v != "" {
				parts = append(parts, v)
			}
		}
		it.Title = short(strings.Join(parts, " · "))
		if it.Title == "" {
			it.Title = tableLabel(t) + " #" + strconv.FormatInt(id, 10)
		}
		it.Lane = tableLabel(t)
		if c.Group != "" {
			switch g := row[c.Group].(type) {
			case nil:
				it.unassigned = true
			case int64:
				it.laneKey, it.laneOrder = "i"+strconv.FormatInt(g, 10), g
				it.Lane = res.Lanes[g]
				if it.Lane == "" {
					it.Lane = label(c.Group) + " " + strconv.FormatInt(g, 10)
				}
			default:
				it.Lane = strings.TrimSpace(display(g, t.ColumnType(c.Group)))
				it.laneKey = "s" + it.Lane
				it.unassigned = it.Lane == ""
			}
			if it.unassigned {
				it.Lane, it.laneKey = "Not assigned", ""
			}
		}
		if c.Status != "" {
			it.Status = display(row[c.Status], "")
			it.hidden = slices.Contains(c.HideStatus, it.Status)
			if it.Status != "" {
				it.Class = statusClass(it.Status, statusOptions)
				if !seen[it.Status] {
					seen[it.Status] = true
					statuses = append(statuses, calStatus{Name: it.Status, Class: it.Class})
				}
			}
			if it.hidden {
				it.Class += " gone"
			}
		}
		it.Link = "/apps/" + app + "/t/" + t.Name + "/" + strconv.FormatInt(id, 10) + "?back=" + url.QueryEscape(back)
		items = append(items, it)
	}
	slices.SortStableFunc(items, func(a, b calItem) int {
		return cmp.Or(cmp.Compare(a.first, b.first), cmp.Compare(a.start, b.start), cmp.Compare(a.ID, b.ID))
	})
	slices.SortFunc(statuses, func(a, b calStatus) int {
		return cmp.Or(cmp.Compare(slices.Index(statusOptions, a.Name), slices.Index(statusOptions, b.Name)), cmp.Compare(a.Name, b.Name))
	})
	return items, statuses
}

const pxPerMinute = 1

// dayView splits a day's items into lanes, each with untimed items on top
// and timed items positioned along one shared time axis.
func dayView(items []calItem, day string) (lanes []*calLane, hours []calHour, height int) {
	byKey := map[string]*calLane{}
	lo, hi := 24*60, 0
	for _, it := range items {
		if it.first > day || it.last < day {
			continue
		}
		l := byKey[it.laneKey]
		if l == nil {
			l = &calLane{Name: it.Lane, key: it.laneKey, order: it.laneOrder, unassigned: it.unassigned}
			byKey[it.laneKey] = l
			lanes = append(lanes, l)
		}
		if it.start < 0 || it.first != it.last {
			l.AllDay = append(l.AllDay, it)
			continue
		}
		l.Timed = append(l.Timed, it)
		lo, hi = min(lo, it.start), max(hi, max(it.end, it.start+30))
	}
	slices.SortFunc(lanes, func(a, b *calLane) int {
		if a.unassigned != b.unassigned {
			if a.unassigned {
				return 1
			}
			return -1
		}
		return cmp.Or(cmp.Compare(a.order, b.order), cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)))
	})
	if hi == 0 {
		return lanes, nil, 0
	}
	lo, hi = lo/60*60, min(24*60, (hi+59)/60*60)
	for m := lo; m <= hi; m += 60 {
		hours = append(hours, calHour{Label: hhmm(m % (24 * 60)), Top: (m - lo) * pxPerMinute})
	}
	for _, l := range lanes {
		layout(l.Timed, lo)
	}
	return lanes, hours, (hi - lo) * pxPerMinute
}

// layout positions timed items, putting overlapping ones side by side.
func layout(items []calItem, lo int) {
	endOf := func(it calItem) int { return max(it.end, it.start+30) }
	var cluster []int // indexes of the current group of overlapping items
	var colEnds []int
	flush := func() {
		for _, i := range cluster {
			items[i].Width = 100 / len(colEnds)
			items[i].Left = items[i].Left * items[i].Width
		}
		cluster, colEnds = nil, nil
	}
	clusterEnd := -1
	for i := range items {
		it := &items[i]
		if it.start >= clusterEnd {
			flush()
		}
		col := slices.IndexFunc(colEnds, func(e int) bool { return e <= it.start })
		if col < 0 {
			col = len(colEnds)
			colEnds = append(colEnds, 0)
		}
		colEnds[col] = endOf(*it)
		it.Left = col // a column number until flush
		it.Top = (it.start - lo) * pxPerMinute
		it.Height = max(24, (endOf(*it)-it.start)*pxPerMinute)
		cluster = append(cluster, i)
		clusterEnd = max(clusterEnd, endOf(*it))
	}
	flush()
}

// weekView puts single-day items in their day and returns multi-day items
// as ranges across the week (and copies in each day for narrow screens).
func weekView(items []calItem, monday, today time.Time, dayLink func(time.Time) string) ([]calDay, []calItem) {
	var days []calDay
	for i := range 7 {
		d := monday.AddDate(0, 0, i)
		days = append(days, calDay{Date: d.Format(time.DateOnly), Label: d.Format("Mon 2 Jan"), Today: d.Equal(today), Link: dayLink(d)})
	}
	var ranges []calItem
	first, last := days[0].Date, days[6].Date
	for _, it := range items {
		if it.first != it.last {
			r := it
			r.ShowLane = it.laneKey != "" || it.unassigned
			r.ColStart, r.ColSpan = 1, 7
			for i, d := range days {
				if d.Date == max(it.first, first) {
					r.ColStart = i + 1
				}
				if d.Date == min(it.last, last) {
					r.ColSpan = i + 2 - r.ColStart
				}
			}
			ranges = append(ranges, r)
		}
		it.ShowLane = it.laneKey != "" || it.unassigned
		for i := range days {
			if it.first <= days[i].Date && it.last >= days[i].Date {
				days[i].Items = append(days[i].Items, it)
			}
		}
	}
	for i := range days {
		slices.SortStableFunc(days[i].Items, func(a, b calItem) int {
			return cmp.Or(cmp.Compare(a.start, b.start), cmp.Compare(a.ID, b.ID))
		})
	}
	return days, ranges
}

// clock parses H:MM or HH:MM (seconds ignored) into minutes after midnight,
// or -1.
func clock(s string) int {
	h, m, ok := strings.Cut(strings.TrimSpace(s), ":")
	if !ok || len(m) < 2 {
		return -1
	}
	hour, err1 := strconv.Atoi(h)
	minute, err2 := strconv.Atoi(m[:2])
	if err1 != nil || err2 != nil || hour < 0 || hour > 24 || minute < 0 || minute > 59 || hour*60+minute > 24*60 {
		return -1
	}
	return hour*60 + minute
}

func hhmm(m int) string { return fmt.Sprintf("%02d:%02d", m/60, m%60) }

func shortDate(d string) string {
	t, err := time.Parse(time.DateOnly, d)
	if err != nil {
		return d
	}
	return t.Format("2 Jan")
}

// statusClass gives a status value a stable colour: its place in the
// column's options, or a hash of the value.
func statusClass(v string, options []string) string {
	n := slices.Index(options, v)
	if n < 0 {
		h := fnv.New32a()
		h.Write([]byte(v))
		n = int(h.Sum32())
	}
	return "st" + strconv.Itoa(n%calendarColours)
}

// calendarBack returns the calendar page a row form was opened from (the
// back parameter), if it is this table's calendar, else "".
func calendarBack(r *http.Request, app, table string) string {
	b := r.FormValue("back")
	prefix := "/apps/" + app + "/t/" + table + "/calendar"
	if (b == prefix || strings.HasPrefix(b, prefix+"?")) && !strings.ContainsAny(b, "\\\r\n#") {
		return b
	}
	return ""
}

var calendarPages = template.Must(template.Must(pages.Clone()).Parse(`
{{define "cal_item"}}<a class="ev {{.Class}}" href="{{.Link}}"{{if .Height}} style="top:{{.Top}}px;height:{{.Height}}px;left:{{.Left}}%;width:{{.Width}}%"{{end}}>
{{with .Time}}<span class="t">{{.}}</span> {{end}}{{with .Range}}<span class="t">{{.}}</span> {{end}}<span class="n">{{.Title}}</span>{{with .Status}} <span class="s">{{.}}</span>{{end}}{{if .ShowLane}}<span class="l">{{.Lane}}</span>{{end}}</a>{{end}}

{{define "admin_calendar"}}{{template "apptop" .}}<style>
.tabs{display:flex;gap:16px;margin:-8px 0 16px;font-size:15px}.tabs a{text-decoration:none;padding-bottom:2px}.tabs [aria-current]{font-weight:700;color:var(--fg);border-bottom:2px solid var(--accent)}
.calbar{display:flex;gap:8px;flex-wrap:wrap;align-items:center;margin-bottom:12px}.calbar .button,.calbar button{margin:0;padding:8px 12px;font-size:15px}.calbar .secondary{background:transparent;color:var(--fg);border:1px solid var(--line)}
.calbar a.on{background:var(--accent);color:#fff;border-color:var(--accent)}.calbar form{display:flex;gap:6px}.calbar input{width:auto;padding:7px 8px}
.legend{display:flex;flex-wrap:wrap;gap:6px;margin:8px 0;font-size:13px}.legend span{padding:2px 8px;border-radius:10px;background:color-mix(in srgb,var(--c) 18%,var(--card));border:1px solid var(--c)}
.ev{display:block;font-size:13px;line-height:1.3;padding:4px 6px;border-radius:6px;border-left:4px solid var(--c,var(--accent));background:color-mix(in srgb,var(--c,var(--accent)) 14%,var(--card));color:var(--fg);text-decoration:none;overflow:hidden;margin-bottom:4px}
.ev .t{font-weight:600}.ev .s,.ev .l{color:var(--muted)}.ev .l{display:block;font-size:12px}.ev.gone{opacity:.55}.ev.gone .n{text-decoration:line-through}
.st0{--c:#2f5bea}.st1{--c:#2e7d32}.st2{--c:#c62828}.st3{--c:#8e24aa}.st4{--c:#ef6c00}.st5{--c:#00838f}.st6{--c:#6d4c41}.st7{--c:#ad1457}
.day{display:grid;grid-auto-flow:column;grid-template-rows:auto auto auto;grid-auto-columns:minmax(150px,1fr);overflow-x:auto}.day.axised{grid-template-columns:44px}.lane{display:contents}
.lane>*{border-left:1px solid var(--line);padding:0 4px}.lane h2{font-size:15px;margin:0;padding:4px;border-bottom:1px solid var(--line)}.allday{padding-top:6px!important}
.timed{position:relative}.timed .ev{position:absolute;margin:0}.h{position:absolute;left:0;right:0;border-top:1px dashed var(--line);font-size:11px;color:var(--muted)}
.week{display:grid;grid-template-columns:repeat(7,minmax(0,1fr));gap:4px}.week .d{border:1px solid var(--line);border-radius:8px;padding:4px;min-height:120px;min-width:0}
.week .d h2{font-size:14px;margin:0 0 4px}.week .d h2 a{color:var(--fg);text-decoration:none}.week .d.today{border-color:var(--accent)}.week .d.today h2 a{color:var(--accent)}
.ranges{display:grid;grid-template-columns:repeat(7,minmax(0,1fr));gap:2px 4px;margin-bottom:6px}.week .copy{display:none}
@media (max-width:640px){.day{display:block}.day .axis,.timed .h{display:none}.lane>*{border-left:0;padding:0}.lane h2{margin-top:12px}.timed{height:auto!important}
.timed .ev{position:static;width:auto!important;height:auto!important;margin-bottom:4px}.week{grid-template-columns:1fr}.week .d{min-height:0}.ranges{display:none}.week .copy{display:block}}
</style>
<div class="card">
<h1>{{.Label}}</h1>
<nav class="tabs"><a href="/apps/{{.App}}/t/{{.Table}}">List</a><a href="{{.Self}}" aria-current="page">Calendar</a></nav>
<div class="calbar"><a class="button secondary{{if eq .View "day"}} on{{end}}" href="{{.DayLink}}">Day</a><a class="button secondary{{if eq .View "week"}} on{{end}}" href="{{.WeekLink}}">Week</a>
<a class="button secondary" href="{{.Prev}}" aria-label="Previous" rel="prev">‹</a><a class="button secondary" href="{{.Today}}">Today</a><a class="button secondary" href="{{.Next}}" aria-label="Next" rel="next">›</a>
<form method="get"><input type="hidden" name="view" value="{{.View}}">{{if .All}}<input type="hidden" name="all" value="1">{{end}}<input type="date" name="date" value="{{.Date}}" aria-label="Date" onchange="this.form.submit()"><button class="secondary">Go</button></form>
{{if not .Demo}}<a class="button" href="{{.AddLink}}">Add</a>{{end}}</div>
<p><strong>{{.Title}}</strong>{{with .Hidden}} · <a href="{{$.Toggle}}">{{if $.All}}Hide {{.}}{{else}}Show {{.}}{{end}}</a>{{end}}</p>
{{if .Capped}}<p class="note">Showing the first {{.Limit}} items only. Use the day view to see the rest.</p>{{end}}
{{with .Statuses}}<div class="legend">{{range .}}<span class="{{.Class}}">{{.Name}}</span>{{end}}</div>{{end}}
{{if eq .View "day"}}{{if .Empty}}<p>Nothing on this day.</p>{{else}}<div class="day{{if .Hours}} axised{{end}}">
{{if .Hours}}<div class="axis"></div><div class="axis"></div><div class="axis timed" style="height:{{.AxisHeight}}px">{{range .Hours}}<div class="h" style="top:{{.Top}}px">{{.Label}}</div>{{end}}</div>{{end}}
{{range .Lanes}}<section class="lane"><h2>{{.Name}}</h2><div class="allday">{{range .AllDay}}{{template "cal_item" .}}{{end}}</div>
<div class="timed"{{if $.Hours}} style="height:{{$.AxisHeight}}px"{{end}}>{{range $.Hours}}<div class="h" style="top:{{.Top}}px"></div>{{end}}{{range .Timed}}{{template "cal_item" .}}{{end}}</div></section>{{end}}</div>{{end}}
{{else}}{{with .Ranges}}<div class="ranges">{{range .}}<div style="grid-column:{{.ColStart}} / span {{.ColSpan}}">{{template "cal_item" .}}</div>{{end}}</div>{{end}}
<div class="week">{{range .Days}}<div class="d{{if .Today}} today{{end}}"><h2><a href="{{.Link}}">{{.Label}}</a></h2>{{range .Items}}<div{{if .Range}} class="copy"{{end}}>{{template "cal_item" .}}</div>{{end}}</div>{{end}}</div>{{end}}
</div>{{template "appbottom"}}{{end}}
`))
