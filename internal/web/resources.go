package web

import (
	"context"
	"fmt"
	"html/template"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/arif-rachim/renoxium/internal/accounts"
	"github.com/arif-rachim/renoxium/internal/applog"
	"github.com/arif-rachim/renoxium/internal/platform"
	"github.com/arif-rachim/renoxium/internal/resources"
)

// /operator/resources: which apps use the shared server most, which slow
// other apps down, and which cost more than they bring in; with actions to
// email the owner, throttle the app for a while or suspend it.

func (s *Site) registerResources() {
	s.mux.HandleFunc("GET /operator/resources", s.resourcesPage)
	s.mux.HandleFunc("GET /operator/resources/{app}", s.resourceAppPage)
	s.mux.HandleFunc("POST /operator/resources/prices", s.resourcePrices)
	s.mux.HandleFunc("POST /operator/resources/{app}/action", s.resourceAction)
}

type resWindow struct {
	Key, Label string
	Dur        time.Duration
	Bins       int
}

var resWindows = []resWindow{{"24h", "24 hours", 24 * time.Hour, 24}, {"7d", "7 days", 7 * 24 * time.Hour, 28}, {"30d", "30 days", 30 * 24 * time.Hour, 30}}

func windowParam(r *http.Request) resWindow {
	for _, w := range resWindows {
		if r.URL.Query().Get("window") == w.Key {
			return w
		}
	}
	return resWindows[0]
}

// resFees caches Free-plan card fees per app, which need every card-taking
// app's database.
type resFees struct {
	mu  sync.Mutex
	at  map[string]time.Time
	fee map[string]map[string]map[string]int64
}

func (s *Site) resourceFees(ctx context.Context, w resWindow, now time.Time) map[string]map[string]int64 {
	c := &s.resFees
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.at == nil {
		c.at, c.fee = map[string]time.Time{}, map[string]map[string]map[string]int64{}
	}
	if at, ok := c.at[w.Key]; ok && now.Sub(at) < feesTTL {
		return c.fee[w.Key]
	}
	fees, err := s.platform.CardFeesByApp(ctx, now.Add(-w.Dur))
	if err != nil {
		slog.Error("card fees per app", "err", err)
	}
	c.at[w.Key], c.fee[w.Key] = now, fees
	return fees
}

func (s *Site) resourcePricesNow() resources.Prices {
	stored, err := s.platform.OperatorSettings(resources.SettingPrefix)
	if err != nil {
		slog.Error("resource prices", "err", err)
	}
	return resources.FromSettings(stored)
}

// resourceData is every app's figures over a window, plus the server row.
type resourceData struct {
	Rows     []resources.Row
	Server   platform.Usage
	Infos    map[string]platform.AppRow
	Prices   resources.Prices
	Days     float64
	Measured time.Time
}

func (s *Site) resourceData(ctx context.Context, w resWindow) (*resourceData, error) {
	now := time.Now()
	from := now.Add(-w.Dur)
	usage, err := s.platform.UsageByStage(from, now.Add(time.Hour))
	if err != nil {
		return nil, err
	}
	apps, err := s.platform.AllApps("", 100000)
	if err != nil {
		return nil, err
	}
	accs, err := s.accounts.GrowthAccounts()
	if err != nil {
		return nil, err
	}
	byID := map[string]accounts.GrowthAccount{}
	for _, a := range accs {
		byID[a.ID] = a
	}
	storage, err := s.platform.StorageByApp()
	if err != nil {
		return nil, err
	}
	fees := s.resourceFees(ctx, w, now)
	d := &resourceData{Infos: map[string]platform.AppRow{}, Prices: s.resourcePricesNow(), Days: w.Dur.Hours() / 24, Measured: now}
	perApp := map[string]*platform.Usage{}
	for _, u := range usage {
		if u.App == platform.PlatformRow {
			d.Server.Add(u.Usage)
			continue
		}
		if perApp[u.App] == nil {
			perApp[u.App] = &platform.Usage{}
		}
		perApp[u.App].Add(u.Usage)
	}
	in := resources.Input{Days: d.Days, Prices: d.Prices, PlanPrice: accounts.PlanPriceAED}
	seen := map[string]bool{}
	for _, a := range apps {
		seen[a.Name] = true
		d.Infos[a.Name] = a
		acc := byID[a.Owner]
		app := resources.App{Name: a.Name, Owner: a.Owner, OwnerEmail: acc.Email, Plan: acc.Plan, Live: a.LiveVersion > 0 && a.Suspended == "",
			Demo: a.Owner == platform.DemoOwner, Storage: storage[a.Name]}
		if app.Plan == "" {
			app.Plan = accounts.PlanFree
		}
		app.PaysPlan = app.Plan != accounts.PlanFree && acc.Status != accounts.StatusGranted
		if app.Demo {
			app.Plan, app.OwnerEmail = "demo", "template demo"
		}
		if u := perApp[a.Name]; u != nil {
			app.Usage = *u
		}
		app.CardFeesAED = float64(fees[a.Name]["aed"]) / 100
		in.Apps = append(in.Apps, app)
	}
	for name, u := range perApp { // deleted since, but they used the server
		if !seen[name] {
			in.Apps = append(in.Apps, resources.App{Name: name, OwnerEmail: "deleted app", Plan: accounts.PlanFree, Usage: *u})
		}
	}
	d.Rows = resources.Compute(in)
	return d, nil
}

// --- list page ---

type resSort struct {
	Key, Label string
	less       func(a, b resources.Row) bool
}

var resSorts = []resSort{
	{"script", "Script time", func(a, b resources.Row) bool { return a.Usage.ScriptUS > b.Usage.ScriptUS }},
	{"cpu", "CPU", func(a, b resources.Row) bool { return a.CPUHours > b.CPUHours }},
	{"waits", "Slot wait caused", func(a, b resources.Row) bool { return a.Usage.CausedWaitUS > b.Usage.CausedWaitUS }},
	{"refused", "Refused", func(a, b resources.Row) bool { return a.Usage.Refused() > b.Usage.Refused() }},
	{"incidents", "Timeouts and kills", func(a, b resources.Row) bool { return a.Incidents > b.Incidents }},
	{"bandwidth", "Bandwidth", func(a, b resources.Row) bool { return a.Usage.BytesOut > b.Usage.BytesOut }},
	{"storage", "Storage", func(a, b resources.Row) bool { return a.Storage.Total() > b.Storage.Total() }},
	{"cost", "Estimated cost", func(a, b resources.Row) bool { return a.Cost.Total > b.Cost.Total }},
	{"revenue", "Revenue", func(a, b resources.Row) bool { return a.Revenue > b.Revenue }},
	{"margin", "Margin (worst first)", func(a, b resources.Row) bool { return a.Margin < b.Margin }},
}

func sortParam(r *http.Request) resSort {
	for _, s := range resSorts {
		if r.URL.Query().Get("sort") == s.Key {
			return s
		}
	}
	return resSorts[0]
}

type resLink struct {
	Label, Href string
	Current     bool
}

type resCell struct {
	Text  string
	Width int // bar width in % of the column's largest value
	Bad   bool
}

type resFlag struct {
	Key, Label, Why string
}

type resRow struct {
	App, OwnerEmail, Plan, LiveURL, Detail, Throttle string
	Suspended                                        bool
	Spark                                            []int
	Script, CPU, Share, Caused, Refused, Incidents   resCell
	Bandwidth, Storage, Cost, Revenue, Margin        resCell
	Flags                                            []resFlag
}

type resTile struct {
	Label, Value, Note string
	Bad                bool
}

// num2 is an AED amount without the currency, for table cells.
func num2(v float64) string {
	return strings.Replace(aed2(v), "AED ", "", 1)
}

func aed2(v float64) string {
	if math.Round(v*100) == 0 {
		return "AED 0.00"
	}
	if v < 0 {
		return "−AED " + strconv.FormatFloat(-v, 'f', 2, 64)
	}
	return "AED " + strconv.FormatFloat(v, 'f', 2, 64)
}

func cpuText(h float64) string {
	if h < 1 {
		return fmt.Sprintf("%.1f min", h*60)
	}
	return fmt.Sprintf("%.2f h", h)
}

func width(v, top float64) int {
	if top <= 0 || v <= 0 {
		return 0
	}
	return max(2, int(math.Round(100*v/top)))
}

func resFlags(r resources.Row) []resFlag {
	out := make([]resFlag, len(r.Flags))
	for i, f := range r.Flags {
		out[i] = resFlag(f)
	}
	return out
}

// healthTiles is the strip at the top: the last 24 hours of the server.
func (s *Site) healthTiles(rows []resources.Row, server platform.Usage) []resTile {
	busy, total := s.platform.Workers()
	var all platform.Usage
	for _, r := range rows {
		all.Add(r.Usage)
	}
	now := resTile{Label: "Workers busy now", Value: strconv.Itoa(busy), Note: "scripts running now"}
	util := resTile{Label: "Peak in 24 h", Value: strconv.FormatInt(server.PeakScripts, 10), Note: "scripts at the same time"}
	if total > 0 {
		now.Value = fmt.Sprintf("%d of %d", busy, total)
		now.Note = fmt.Sprintf("%.0f%% of the worker pool", 100*float64(busy)/float64(total))
		avg := float64(server.ScriptUS) / (float64(total) * 24 * 3.6e9) * 100
		util.Value = fmt.Sprintf("%d of %d", server.PeakScripts, total)
		util.Note = fmt.Sprintf("average use %.1f%% of the pool", avg)
		util.Bad = server.PeakScripts >= int64(total)
	}
	return []resTile{now, util,
		{Label: "Slot waits", Value: fmtInt(int(all.Waits)), Note: resources.Seconds(all.SlotWaitUS+all.WorkerWaitUS) + " waited in all; " +
			resources.Seconds(all.WorkerWaitUS) + " for a free worker", Bad: all.WorkerWaitUS > 60e6},
		{Label: "Refused", Value: fmtInt(int(all.Refused())), Note: fmt.Sprintf("app limit %d · account %d · throttle %d", all.RefusedApp, all.RefusedOwner, all.RefusedThrottle), Bad: all.Refused() > 0},
		{Label: "Memory kills", Value: fmtInt(int(all.MemKills)), Note: plural(int(all.Restarts), "worker", "workers") + " restarted", Bad: all.MemKills > 0},
		{Label: "Timeouts", Value: fmtInt(int(all.Timeouts)), Note: "runs stopped at the 2 s limit", Bad: all.Timeouts > 0},
	}
}

func (s *Site) resourcesPage(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.operator(w, r); !ok {
		return
	}
	win, sorter := windowParam(r), sortParam(r)
	showAll := r.URL.Query().Get("all") == "1"
	ctx := r.Context()
	d, err := s.resourceData(ctx, win)
	if err != nil {
		serverError(w, err)
		return
	}
	day := d
	if win.Key != "24h" {
		if day, err = s.resourceData(ctx, resWindows[0]); err != nil {
			serverError(w, err)
			return
		}
	}
	rows := d.Rows
	idle := 0
	var list []resources.Row
	var total platform.Usage
	var cost, revenue float64
	for _, row := range rows {
		total.Add(row.Usage)
		cost += row.Cost.Total
		revenue += row.Revenue
		if row.Active() || showAll {
			list = append(list, row)
		} else {
			idle++
		}
	}
	sort.SliceStable(list, func(i, j int) bool {
		if sorter.less(list[i], list[j]) {
			return true
		}
		if sorter.less(list[j], list[i]) {
			return false
		}
		return list[i].Name < list[j].Name
	})
	const maxRows = 300
	more := 0
	if len(list) > maxRows {
		more = len(list) - maxRows
		list = list[:maxRows]
	}
	var top struct{ script, cpu, caused, refused, inc, bw, st, cost, rev, loss float64 }
	for _, row := range list {
		top.script = math.Max(top.script, float64(row.Usage.ScriptUS))
		top.cpu = math.Max(top.cpu, row.CPUHours)
		top.caused = math.Max(top.caused, float64(row.Usage.CausedWaitUS))
		top.refused = math.Max(top.refused, float64(row.Usage.Refused()))
		top.inc = math.Max(top.inc, float64(row.Incidents))
		top.bw = math.Max(top.bw, float64(row.Usage.BytesOut))
		top.st = math.Max(top.st, float64(row.Storage.Total()))
		top.cost = math.Max(top.cost, row.Cost.Total)
		top.rev = math.Max(top.rev, row.Revenue)
		top.loss = math.Max(top.loss, math.Abs(row.Margin))
	}
	series, err := s.platform.ScriptSeries(d.Measured.Add(-win.Dur), d.Measured, win.Bins)
	if err != nil {
		slog.Error("script series", "err", err)
	}
	view := make([]resRow, 0, len(list))
	flagged := 0
	for _, row := range list {
		info := d.Infos[row.Name]
		v := resRow{App: row.Name, OwnerEmail: row.OwnerEmail, Plan: row.Plan, Detail: "/operator/resources/" + url.PathEscape(row.Name) + "?window=" + win.Key,
			Suspended: info.Suspended != "", Flags: resFlags(row)}
		if len(v.Flags) > 0 {
			flagged++
		}
		if appInfo, err := s.platform.Info(row.Name); err == nil && appInfo.LiveVersion > 0 {
			v.LiveURL = appInfo.LiveURL
		}
		if t, ok := s.platform.ThrottleOf(row.Name); ok {
			v.Throttle = "throttled until " + t.Until.UTC().Format("2 Jan 15:04") + " UTC"
		}
		if sp := series[row.Name]; sp != nil {
			var m int64
			for _, x := range sp {
				m = max(m, x)
			}
			for _, x := range sp {
				v.Spark = append(v.Spark, width(float64(x), float64(m)))
			}
		}
		u := row.Usage
		v.Script = resCell{Text: resources.Seconds(u.ScriptUS), Width: width(float64(u.ScriptUS), top.script)}
		v.CPU = resCell{Text: cpuText(row.CPUHours), Width: width(row.CPUHours, top.cpu)}
		v.Share = resCell{Text: pct(100 * row.CostShare)}
		v.Caused = resCell{Text: resources.Seconds(u.CausedWaitUS), Width: width(float64(u.CausedWaitUS), top.caused), Bad: row.Has(resources.FlagNoisy)}
		if u.CausedWaitUS == 0 {
			v.Caused.Text = "—"
		}
		v.Refused = resCell{Text: fmtInt(int(u.Refused())), Width: width(float64(u.Refused()), top.refused)}
		v.Incidents = resCell{Text: fmt.Sprintf("%d / %d", u.Timeouts, u.MemKills), Width: width(float64(row.Incidents), top.inc), Bad: row.Has(resources.FlagErrors)}
		v.Bandwidth = resCell{Text: resources.Bytes(u.BytesOut), Width: width(float64(u.BytesOut), top.bw)}
		v.Storage = resCell{Text: resources.Bytes(row.Storage.Total()), Width: width(float64(row.Storage.Total()), top.st)}
		v.Cost = resCell{Text: num2(row.Cost.Total), Width: width(row.Cost.Total, top.cost)}
		v.Revenue = resCell{Text: num2(row.Revenue), Width: width(row.Revenue, top.rev)}
		v.Margin = resCell{Text: num2(row.Margin), Bad: math.Round(row.Margin*100) < 0, Width: width(math.Abs(row.Margin), top.loss)}
		view = append(view, v)
	}
	var windows, sorts []resLink
	q := func(win, sort string) string {
		v := url.Values{"window": {win}, "sort": {sort}}
		if showAll {
			v.Set("all", "1")
		}
		return "/operator/resources?" + v.Encode()
	}
	for _, x := range resWindows {
		windows = append(windows, resLink{Label: x.Label, Href: q(x.Key, sorter.Key), Current: x.Key == win.Key})
	}
	for _, x := range resSorts {
		sorts = append(sorts, resLink{Label: x.Label, Href: q(win.Key, x.Key), Current: x.Key == sorter.Key})
	}
	allHref := q(win.Key, sorter.Key)
	if showAll {
		allHref = strings.Replace(allHref, "&all=1", "", 1)
	} else {
		allHref += "&all=1"
	}
	var prices []priceField
	for _, f := range resources.Fields {
		v := *f.Get(&d.Prices)
		prices = append(prices, priceField{Key: f.Key, Label: f.Label, Unit: f.Unit, Value: strconv.FormatFloat(v, 'f', -1, 64)})
	}
	render(w, http.StatusOK, "operator_resources", map[string]any{
		"CSRF": s.csrfToken(w, r), "Window": win, "Windows": windows, "Sorts": sorts, "Sort": sorter, "Rows": view, "Idle": idle, "More": more,
		"ShowAll": showAll, "AllHref": allHref, "Health": s.healthTiles(day.Rows, day.Server), "Flagged": flagged, "Prices": prices,
		"Totals": []resTile{
			{Label: "Script time", Value: resources.Seconds(total.ScriptUS), Note: "time scripts held a worker"},
			{Label: "CPU", Value: cpuText(float64(total.CPUUS+total.DBUS) / 3.6e9), Note: resources.Seconds(total.DBUS) + " of it database work in the server"},
			{Label: "Requests", Value: fmtInt(int(total.Requests)), Note: resources.Bytes(total.BytesOut) + " sent"},
			{Label: "Estimated cost", Value: aed2(cost), Note: "at the unit prices below"},
			{Label: "Revenue", Value: aed2(revenue), Note: "plans and Free-plan card fees"},
			{Label: "Margin", Value: aed2(revenue - cost), Bad: revenue < cost},
		},
		"Measured": d.Measured.UTC().Format("15:04 UTC"),
	})
}

type priceField struct {
	Key, Label, Unit, Value string
}

// resourcePrices saves the unit prices and fair-use thresholds.
func (s *Site) resourcePrices(w http.ResponseWriter, r *http.Request) {
	op, ok := s.operator(w, r)
	if !ok {
		return
	}
	if !s.checkCSRF(r) {
		http.Error(w, "the form expired; go back and try again", http.StatusForbidden)
		return
	}
	for _, f := range resources.Fields {
		raw := strings.TrimSpace(r.PostFormValue(f.Key))
		if raw == "" {
			continue
		}
		n, err := strconv.ParseFloat(raw, 64)
		if err != nil || n < 0 || n > 1e6 || math.IsNaN(n) {
			http.Error(w, f.Label+" must be a number of 0 or more, e.g. 0.05", http.StatusBadRequest)
			return
		}
		if err := s.platform.SetResourceSetting(resources.SettingPrefix+f.Key, strconv.FormatFloat(n, 'f', -1, 64)); err != nil {
			serverError(w, err)
			return
		}
	}
	slog.Info("operator changed resource prices", "operator", op.Email)
	http.Redirect(w, r, "/operator/resources?window="+url.QueryEscape(r.PostFormValue("window"))+"#prices", http.StatusSeeOther)
}

// --- one app ---

type resNameRow struct {
	Name, Runs, Time, CPU, Avg, Share string
	Width                             int
}

type resEvent struct {
	Time, Stage, Message string
}

func (s *Site) resourceAppPage(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.operator(w, r); !ok {
		return
	}
	app := r.PathValue("app")
	info, err := s.platform.Info(app)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	win := windowParam(r)
	ctx := r.Context()
	d, err := s.resourceData(ctx, win)
	if err != nil {
		serverError(w, err)
		return
	}
	var row resources.Row
	found := false
	for _, x := range d.Rows {
		if x.Name == app {
			row, found = x, true
		}
	}
	if !found {
		http.NotFound(w, r)
		return
	}
	now := time.Now()
	hours, err := s.platform.UsageHours(app, now.Add(-7*24*time.Hour+time.Hour), now)
	if err != nil {
		serverError(w, err)
		return
	}
	hourCharts := []chart{
		hourChart("Script time per hour", hours, func(u platform.Usage) float64 { return float64(u.ScriptUS) },
			func(u platform.Usage) string {
				return resources.Seconds(u.ScriptUS) + " held · CPU " + resources.Seconds(u.CPUUS+u.DBUS) + " · " + fmtInt(int(u.Runs)) + " runs"
			}, func(v float64) string { return resources.Seconds(int64(v)) }),
		hourChart("Requests per hour", hours, func(u platform.Usage) float64 { return float64(u.Requests) },
			func(u platform.Usage) string {
				return fmtInt(int(u.Requests)) + " requests · " + resources.Bytes(u.BytesOut)
			},
			func(v float64) string { return fmtInt(int(v)) }),
		hourChart("Other apps' wait for a worker it held", hours, func(u platform.Usage) float64 { return float64(u.CausedWaitUS) },
			func(u platform.Usage) string {
				return resources.Seconds(u.CausedWaitUS) + " caused · own wait " + resources.Seconds(u.SlotWaitUS+u.WorkerWaitUS) + " · refused " + fmtInt(int(u.Refused()))
			}, func(v float64) string { return resources.Seconds(int64(v)) }),
		hourChart("Timeouts and memory kills per hour", hours, func(u platform.Usage) float64 { return float64(u.Timeouts + u.MemKills) },
			func(u platform.Usage) string {
				return fmt.Sprintf("%d timeouts · %d memory kills · %d restarts", u.Timeouts, u.MemKills, u.Restarts)
			}, func(v float64) string { return fmtInt(int(v)) }),
	}
	names, err := s.platform.UsageNames(app, now.Add(-win.Dur), 15)
	if err != nil {
		serverError(w, err)
		return
	}
	var nameRows []resNameRow
	var topWall int64
	for _, n := range names {
		topWall = max(topWall, n.WallUS)
	}
	for _, n := range names {
		nr := resNameRow{Name: n.Name, Runs: fmtInt(int(n.Runs)), Time: resources.Seconds(n.WallUS), CPU: resources.Seconds(n.CPUUS),
			Width: width(float64(n.WallUS), float64(topWall))}
		if n.Runs > 0 {
			nr.Avg = resources.Seconds(n.WallUS / n.Runs)
		}
		if row.Usage.ScriptUS > 0 {
			nr.Share = pct(100 * float64(n.WallUS) / float64(row.Usage.ScriptUS))
		}
		nameRows = append(nameRows, nr)
	}
	var incidents []resEvent
	if evs, err := s.platform.Logs(app, applog.Filter{Level: "error", Limit: 200}); err == nil {
		for _, e := range evs {
			if strings.Contains(e.Message, "time limit exceeded") || strings.Contains(e.Message, "memory limit exceeded") {
				t, _ := time.Parse(time.RFC3339Nano, e.Time)
				incidents = append(incidents, resEvent{Time: t.UTC().Format("2 Jan 15:04:05"), Stage: e.Stage, Message: shortText(e.Message, 400)})
				if len(incidents) == 15 {
					break
				}
			}
		}
	}
	stages, _ := s.platform.UsageByStage(now.Add(-win.Dur), now.Add(time.Hour))
	var stageRows [][]string
	for _, st := range stages {
		if st.App == app {
			stageRows = append(stageRows, []string{st.Stage, fmtInt(int(st.Requests)), fmtInt(int(st.Runs)), resources.Seconds(st.ScriptUS),
				resources.Seconds(st.CPUUS), resources.Seconds(st.DBUS), resources.Bytes(st.BytesOut), resources.Bytes(st.HeapPeak)})
		}
	}
	audit, _ := s.platform.AuditLog(app, 20)
	u := row.Usage
	tiles := []resTile{
		{Label: "Script time", Value: resources.Seconds(u.ScriptUS), Note: pct(100*row.CostShare) + " of all apps · " + fmtInt(int(u.Runs)) + " runs"},
		{Label: "CPU", Value: cpuText(row.CPUHours), Note: resources.Seconds(u.CPUUS) + " in workers · " + resources.Seconds(u.DBUS) + " database"},
		{Label: "Others waited for it", Value: resources.Seconds(u.CausedWaitUS), Note: fmtInt(int(u.HeldWhileWaiting)) + " times it held a worker someone waited for", Bad: row.Has(resources.FlagNoisy)},
		{Label: "Its own waits", Value: resources.Seconds(u.SlotWaitUS + u.WorkerWaitUS), Note: fmt.Sprintf("refused %d (app %d · account %d · throttle %d)", u.Refused(), u.RefusedApp, u.RefusedOwner, u.RefusedThrottle)},
		{Label: "Timeouts / memory kills", Value: fmt.Sprintf("%d / %d", u.Timeouts, u.MemKills), Note: fmt.Sprintf("%d restarts · %d other script errors · peak heap %s", u.Restarts, u.ScriptErrors, resources.Bytes(u.HeapPeak)), Bad: row.Has(resources.FlagErrors)},
		{Label: "Requests", Value: fmtInt(int(u.Requests)), Note: resources.Bytes(u.BytesOut) + " sent · peak " + strconv.FormatInt(u.PeakScripts, 10) + " scripts at once"},
		{Label: "Realtime", Value: strconv.FormatInt(u.RTPeak, 10) + " peak", Note: fmt.Sprintf("%.1f connection-hours · %d messages", float64(u.RTConnSec)/3600, u.RTMessages)},
		{Label: "Messages and calls", Value: fmtInt(int(u.Emails + u.WhatsApp)), Note: fmt.Sprintf("%d emails · %d WhatsApp · %d http.fetch · %d job runs (%s)", u.Emails, u.WhatsApp, u.Fetches, u.Jobs, resources.Seconds(u.JobUS))},
		{Label: "Storage", Value: resources.Bytes(row.Storage.Total()), Note: "live DB " + resources.Bytes(row.Storage.LiveDB) + " · preview DB " + resources.Bytes(row.Storage.PreviewDB) + " · files " + resources.Bytes(row.Storage.Files) + " · visitor uploads " + resources.Bytes(row.Storage.Private)},
		{Label: "Estimated cost", Value: aed2(row.Cost.Total), Note: fmt.Sprintf("CPU %s · storage %s · bandwidth %s · messages %s · realtime %s", aed2(row.Cost.CPU), aed2(row.Cost.Storage), aed2(row.Cost.Egress), aed2(row.Cost.WhatsApp+row.Cost.Email), aed2(row.Cost.Realtime))},
		{Label: "Revenue", Value: aed2(row.Revenue), Note: "plan share " + aed2(row.PlanRevenue) + " · card fees " + aed2(row.FeeRevenue)},
		{Label: "Margin", Value: aed2(row.Margin), Bad: row.Margin < 0, Note: "revenue minus estimated cost"},
	}
	var windows []resLink
	for _, x := range resWindows {
		windows = append(windows, resLink{Label: x.Label, Href: "/operator/resources/" + url.PathEscape(app) + "?window=" + x.Key, Current: x.Key == win.Key})
	}
	throttle, throttled := s.platform.ThrottleOf(app)
	appRow := d.Infos[app]
	render(w, http.StatusOK, "operator_resource_app", map[string]any{
		"CSRF": s.csrfToken(w, r), "App": app, "Info": info, "Row": row, "Flags": resFlags(row), "Tiles": tiles, "Charts": hourCharts,
		"Names": nameRows, "Incidents": incidents, "Stages": stageRows, "Audit": audit, "Window": win, "Windows": windows,
		"Throttle": throttle, "Throttled": throttled, "ThrottleNotice": throttle.Notice(), "Suspended": appRow.Suspended,
		"Notice": noticeText(row, win, s.base), "NoticeSubject": "Your app " + app + " uses a lot of the shared server",
		"MaxSlots": s.platform.ScriptsPerApp(),
	})
}

func shortText(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

// hourChart draws hourly bars with the existing chart styles.
func hourChart(title string, hours []platform.HourUsage, value func(platform.Usage) float64, tip func(platform.Usage) string, format func(float64) string) chart {
	c := chart{Title: title, Empty: "Nothing in the last 7 days."}
	var top, sum float64
	for _, h := range hours {
		v := value(h.Usage)
		top = math.Max(top, v)
		sum += v
	}
	c.Total = format(sum)
	if top == 0 {
		return c
	}
	c.Max = format(top)
	for i, h := range hours {
		v := value(h.Usage)
		height := v / top * 100
		if v > 0 && height < 2 {
			height = 2
		}
		label := h.Hour.Format("Mon 2 Jan 15:00") + " UTC"
		c.Bars = append(c.Bars, bar{Height: math.Round(height*10) / 10, Tip: label + ": " + tip(h.Usage), Today: i == len(hours)-1})
		if v > 0 {
			c.Rows = append(c.Rows, [2]string{label, tip(h.Usage)})
		}
	}
	if n := len(hours); n > 0 {
		c.Ticks = []string{hours[0].Hour.Format("2 Jan"), hours[n/2].Hour.Format("2 Jan"), "Now"}
	}
	return c
}

// noticeText is the polite email the operator can send the owner; it is
// shown in a text box to edit before sending.
func noticeText(r resources.Row, w resWindow, base string) string {
	u := r.Usage
	var b strings.Builder
	fmt.Fprintf(&b, "Hello,\n\nRenoxium runs every app on shared servers, so that each one stays fast. Over the last %s, %s used more of them than most apps:\n\n", w.Label, r.Name)
	fmt.Fprintf(&b, "- %s of script time in %s runs (%s of the time of all apps together)\n", resources.Seconds(u.ScriptUS), fmtInt(int(u.Runs)), pct(100*r.CostShare))
	if u.CausedWaitUS > 0 {
		fmt.Fprintf(&b, "- while its scripts ran, other apps had to wait %s in all for their turn\n", resources.Seconds(u.CausedWaitUS))
	}
	if r.Incidents > 0 {
		fmt.Fprintf(&b, "- %d script runs were stopped because they took longer than 2 seconds or used too much memory\n", r.Incidents)
	}
	if u.BytesOut > 1<<30 {
		fmt.Fprintf(&b, "- it sent %s to visitors\n", resources.Bytes(u.BytesOut))
	}
	b.WriteString("\nNothing has been changed yet. You can usually make an app much lighter by asking the assistant that built it (ChatGPT or Claude) to " +
		"\"check the Renoxium logs and make the slow scripts faster\": read fewer rows per request, avoid long loops, and move heavy work into a scheduled job.\n\n")
	if r.Plan == "free" {
		fmt.Fprintf(&b, "If your business needs this much, a paid plan covers it and removes the Free plan's limits: %s/billing\n\n", base)
	} else {
		fmt.Fprintf(&b, "If your business needs this much, the Business plan has the most room: %s/billing\n\n", base)
	}
	b.WriteString("Reply to this email if you have questions.\n\nThank you,\nRenoxium")
	return b.String()
}

// resourceAction runs an operator action on one app. Every action is
// written to the operator audit and the app's own log.
func (s *Site) resourceAction(w http.ResponseWriter, r *http.Request) {
	op, ok := s.operator(w, r)
	if !ok {
		return
	}
	if !s.checkCSRF(r) {
		http.Error(w, "the form expired; go back and try again", http.StatusForbidden)
		return
	}
	app := r.PathValue("app")
	if _, err := s.platform.Info(app); err != nil {
		http.NotFound(w, r)
		return
	}
	action := r.PostFormValue("action")
	var detail string
	var err error
	switch action {
	case "notify":
		subject := strings.TrimSpace(r.PostFormValue("subject"))
		text := strings.TrimSpace(r.PostFormValue("text"))
		if subject == "" || text == "" || len(subject) > 150 || len(text) > 8000 {
			err = fmt.Errorf("write a subject (at most 150 characters) and a message (at most 8000)")
			break
		}
		if s.platform.OwnerOf(app) == platform.DemoOwner {
			err = fmt.Errorf("template demos have no owner to email")
			break
		}
		s.platform.MailOwner(app, subject, text)
		detail = "emailed the owner: " + subject
	case "throttle":
		slots, _ := strconv.Atoi(r.PostFormValue("slots"))
		perMin, _ := strconv.Atoi(strings.TrimSpace(r.PostFormValue("per_minute")))
		hours, _ := strconv.Atoi(r.PostFormValue("hours"))
		if hours <= 0 {
			err = fmt.Errorf("choose how long the throttle lasts")
			break
		}
		t := platform.Throttle{App: app, Slots: slots, PerMinute: perMin, Until: time.Now().Add(time.Duration(hours) * time.Hour),
			Reason: shortText(strings.TrimSpace(r.PostFormValue("reason")), 200), By: op.Email}
		if err = s.platform.SetThrottle(t); err == nil {
			detail = fmt.Sprintf("throttled until %s UTC: %d concurrent scripts (0 = normal), %d runs a minute (0 = no budget)",
				t.Until.UTC().Format("2 Jan 15:04"), t.Slots, t.PerMinute)
			if t.Reason != "" {
				detail += "; " + t.Reason
			}
		}
	case "unthrottle":
		err = s.platform.Unthrottle(app)
		detail = "throttle ended"
	case "suspend":
		reason := strings.TrimSpace(r.PostFormValue("reason"))
		if reason == "" {
			reason = "suspended by Renoxium: the app used too much of the shared server"
		}
		err = s.platform.Suspend(app, reason)
		detail = "suspended: " + reason
		s.forgetGrowth()
	case "unsuspend":
		err = s.platform.Unsuspend(app)
		detail = "brought back"
		s.forgetGrowth()
	default:
		err = fmt.Errorf("unknown action")
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.platform.Audit(op.Email, app, action, detail)
	slog.Warn("operator resource action", "operator", op.Email, "app", app, "action", action)
	http.Redirect(w, r, "/operator/resources/"+url.PathEscape(app)+"?window="+url.QueryEscape(r.PostFormValue("window"))+"&done="+url.QueryEscape(action), http.StatusSeeOther)
}

func init() {
	template.Must(pages.Parse(resourcesTemplate))
}

const resourcesCSS = `<style>
main.wide{max-width:1400px}.res th,.res td{padding-left:7px;padding-right:7px}
.res td.n,.res th.n{text-align:end;font-variant-numeric:tabular-nums;white-space:nowrap}.res table{font-size:13px}.res th{font-size:12.5px;white-space:normal}
.res .cell{display:flex;flex-direction:column;gap:3px;align-items:flex-end}.res .cb{display:block;height:5px;border-radius:3px;background:var(--series);min-width:0;align-self:flex-end}
.res .bad{color:var(--bad);font-weight:500}.res .cb.bad{background:var(--bad)}.res .good{color:var(--live)}
.res .spark{display:flex;align-items:flex-end;gap:1px;height:26px;width:72px}.res .spark i{flex:1;background:var(--series);border-radius:1px;min-height:1px;opacity:.85}
.res .flag{display:inline-block;font-size:12px;font-weight:500;padding:2px 8px;border-radius:999px;margin:2px 4px 0 0;background:var(--sun-soft);color:var(--fg);white-space:nowrap}
.res .flag.noisy,.res .flag.errors{background:color-mix(in srgb,var(--bad) 16%,transparent)}.res td.app{min-width:170px;max-width:240px}.res td.app .muted{font-size:12.5px;overflow-wrap:anywhere}
.res .tile.bad .big{color:var(--bad)}.res .seg{flex-wrap:wrap;margin:4px 8px 6px 0}.res .controls{display:flex;flex-wrap:wrap;align-items:center;gap:6px 14px;margin:6px 0 10px}
.res .why{margin:0;padding:0;list-style:none}.res .why li{padding:10px 0;border-top:1px solid var(--line)}.res .why li:first-child{border-top:0}
.res .forms{display:grid;grid-template-columns:repeat(auto-fit,minmax(300px,1fr));gap:16px}.res .forms form{border:1px solid var(--line);border-radius:12px;padding:16px}
.res .forms h2{margin-bottom:6px}.res .forms button{margin-top:12px}.res .prices{display:grid;grid-template-columns:repeat(auto-fit,minmax(200px,1fr));gap:4px 16px}
.res .chart{margin-bottom:22px}.res .chart .plot{height:120px;gap:1px}.res .chart .b i{border-radius:1px 1px 0 0}.res h2.ch{font-size:16px;margin:4px 0 6px}
.res .namebar{display:block;height:6px;border-radius:3px;background:var(--series)}.res pre.msg{white-space:pre-wrap;font:12.5px/1.45 ui-monospace,monospace;margin:4px 0 0;overflow-wrap:anywhere}
.res .notice{border:1px solid var(--sun);background:var(--sun-soft);border-radius:10px;padding:10px 14px;margin:10px 0}
</style>`

const resourcesTemplate = `{{define "operator_resources"}}{{template "wide" "Resources"}}` + resourcesCSS + `<p class="crumbs"><a href="/operator">Operator</a></p>
<div class="card res"><div class="bar"><h1 style="margin:0">Resources</h1><span class="muted">measured {{.Measured}} · current hour included</span></div>
<p class="muted">Which apps use the shared server most, which make other apps wait, and which cost more than they bring in. Both stages count towards an app's use; revenue comes from live only.</p>
<h2>Last 24 hours</h2><div class="tiles">{{range .Health}}<div class="tile{{if .Bad}} bad{{end}}"><div class="muted">{{.Label}}</div><div class="big">{{.Value}}</div><div class="muted">{{.Note}}</div></div>{{end}}</div>
<h2>Over {{.Window.Label}}</h2><div class="tiles">{{range .Totals}}<div class="tile{{if .Bad}} bad{{end}}"><div class="muted">{{.Label}}</div><div class="big">{{.Value}}</div><div class="muted">{{.Note}}</div></div>{{end}}</div>
<div class="controls"><nav class="seg" aria-label="Period">{{range .Windows}}<a href="{{.Href}}"{{if .Current}} aria-current="true"{{end}}>{{.Label}}</a>{{end}}</nav>
<span class="muted">Sort by</span><nav class="seg" aria-label="Sort by">{{range .Sorts}}<a href="{{.Href}}"{{if .Current}} aria-current="true"{{end}}>{{.Label}}</a>{{end}}</nav></div>
<p class="muted">{{len .Rows}} apps shown{{if .Idle}} · {{.Idle}} idle apps hidden (<a href="{{.AllHref}}">show all</a>){{else if .ShowAll}} · <a href="{{.AllHref}}">hide idle apps</a>{{end}}{{if .More}} · {{.More}} more not shown{{end}} · {{.Flagged}} flagged · sorted by {{.Sort.Label}}</p>
<div class="scroll"><table><thead><tr><th>App</th><th>Script time</th><th class="n">Script</th><th class="n">CPU</th><th class="n">Share</th><th class="n">Wait caused</th><th class="n">Refused</th><th class="n" title="timeouts / memory kills">Timeouts / kills</th><th class="n">Bandwidth</th><th class="n">Storage</th><th class="n">Est. cost, AED</th><th class="n">Revenue, AED</th><th class="n">Margin, AED</th></tr></thead><tbody>
{{range .Rows}}<tr><td class="app"><a href="{{.Detail}}"><strong>{{.App}}</strong></a>{{if .Suspended}} <span class="error">suspended</span>{{end}}<div class="muted">{{.OwnerEmail}} · {{.Plan}}{{with .LiveURL}} · <a href="{{.}}" target="_blank" rel="noopener">live</a>{{end}}</div>{{with .Throttle}}<div class="muted bad">{{.}}</div>{{end}}
{{range .Flags}}<span class="flag {{.Key}}" title="{{.Why}}">{{.Label}}</span>{{end}}</td>
<td><span class="spark" aria-hidden="true">{{range .Spark}}<i style="height:{{.}}%"></i>{{end}}</span></td>
{{template "rescell" .Script}}{{template "rescell" .CPU}}<td class="n">{{.Share.Text}}</td>{{template "rescell" .Caused}}{{template "rescell" .Refused}}{{template "rescell" .Incidents}}{{template "rescell" .Bandwidth}}{{template "rescell" .Storage}}{{template "rescell" .Cost}}{{template "rescell" .Revenue}}{{template "rescell" .Margin}}</tr>
{{else}}<tr><td colspan="13" class="muted">No app used the server in this period.</td></tr>{{end}}
</tbody></table></div>
<h2 style="margin-top:22px">What the flags mean</h2><ul class="why">
<li><span class="flag noisy">Noisy neighbour</span> Among the top 1% of active apps by script time, and other apps waited at least a second in all for a worker while its scripts held one. These apps make other customers' pages slow.</li>
<li><span class="flag">Unprofitable heavy</span> Among the 10% of apps with the highest estimated cost, costing at least AED 0.01, and bringing in less than they cost.</li>
<li><span class="flag errors">Error-prone</span> At least 3 runs, and at least 2% of runs, stopped at the 2 s time limit or a memory limit. Each holds a worker until it is stopped, and a memory kill restarts the worker.</li>
<li><span class="flag">Free plan over fair use</span> A Free-plan app above the fair-use thresholds below (script seconds a day or MB sent a day, averaged over the period).</li></ul>
<p class="muted">Script time is how long scripts held a worker, database calls and waiting included; CPU is measured inside the worker process (user and system time from the kernel) plus the time the server spent on the app's database calls. Wait caused: when a run had to wait for a free worker, the wait is shared out over the apps whose runs held the workers. Cost share is the app's part of all script time. Revenue: the owner's plan price for the period (paid plans only, not granted ones), split equally over the owner's live apps (all their apps if none is live), plus the Free-plan card fees of the app's own live payments (AED only).</p>
<h2 style="margin-top:22px" id="prices">Unit prices and fair use</h2>
<form method="post" action="/operator/resources/prices"><input type="hidden" name="csrf" value="{{.CSRF}}"><input type="hidden" name="window" value="{{.Window.Key}}"><div class="prices">
{{range .Prices}}<div><label for="p-{{.Key}}">{{.Label}} <span class="muted">({{.Unit}})</span></label><input id="p-{{.Key}}" name="{{.Key}}" inputmode="decimal" value="{{.Value}}"></div>{{end}}</div>
<button class="secondary">Save prices</button></form></div>{{template "bottom"}}{{end}}

{{define "rescell"}}<td class="n"><span class="cell"><span{{if .Bad}} class="bad"{{end}}>{{.Text}}</span>{{if .Width}}<span class="cb{{if .Bad}} bad{{end}}" style="width:{{.Width}}%"></span>{{end}}</span></td>{{end}}

{{define "operator_resource_app"}}{{template "wide" .App}}` + resourcesCSS + `<p class="crumbs"><a href="/operator">Operator</a> / <a href="/operator/resources?window={{.Window.Key}}">Resources</a></p>
<div class="card res"><div class="bar"><h1 style="margin:0">{{.App}}</h1><nav class="seg" aria-label="Period">{{range .Windows}}<a href="{{.Href}}"{{if .Current}} aria-current="true"{{end}}>{{.Label}}</a>{{end}}</nav></div>
<p class="muted">{{.Row.OwnerEmail}} · {{.Row.Plan}} plan{{if .Info.LiveVersion}} · <a href="{{.Info.LiveURL}}" target="_blank" rel="noopener">{{.Info.LiveURL}}</a> (v{{.Info.LiveVersion}}){{else}} · not published{{end}}{{with .Suspended}} · <span class="error">suspended: {{.}}</span>{{end}}</p>
{{if .Throttled}}<p class="notice"><strong>Throttled</strong> until {{.Throttle.Until.UTC.Format "2 Jan 15:04"}} UTC by {{.Throttle.By}}. The owner sees: {{.ThrottleNotice}}</p>{{end}}
{{if .Flags}}<ul class="why">{{range .Flags}}<li><span class="flag {{.Key}}">{{.Label}}</span> {{.Why}}</li>{{end}}</ul>{{else}}<p class="muted">No flags in this period.</p>{{end}}
<h2>Over {{.Window.Label}}</h2><div class="tiles">{{range .Tiles}}<div class="tile{{if .Bad}} bad{{end}}"><div class="muted">{{.Label}}</div><div class="big">{{.Value}}</div><div class="muted">{{.Note}}</div></div>{{end}}</div>
<h2 style="margin-top:18px">Last 7 days, per hour <span class="muted">(UTC · gold is the current hour)</span></h2>
{{range .Charts}}<h2 class="ch">{{.Title}} <span class="muted">· total {{.Total}}</span></h2>{{template "chart" .}}{{end}}
<h2>Top routes, jobs and tools <span class="muted">· {{.Window.Label}}, by time held</span></h2>
{{if .Names}}<div class="scroll"><table><thead><tr><th>Route, job, hook or tool</th><th class="n">Runs</th><th class="n">Time</th><th class="n">Share</th><th></th><th class="n">CPU</th><th class="n">Average</th></tr></thead><tbody>
{{range .Names}}<tr><td><code>{{.Name}}</code></td><td class="n">{{.Runs}}</td><td class="n">{{.Time}}</td><td class="n">{{.Share}}</td><td style="width:22%"><span class="namebar" style="width:{{.Width}}%"></span></td><td class="n">{{.CPU}}</td><td class="n">{{.Avg}}</td></tr>{{end}}
</tbody></table></div>{{else}}<p class="muted">No script ran in this period.</p>{{end}}
{{if .Stages}}<h2 style="margin-top:18px">By stage</h2><div class="scroll"><table><thead><tr><th>Stage</th><th class="n">Requests</th><th class="n">Runs</th><th class="n">Script time</th><th class="n">Worker CPU</th><th class="n">Database</th><th class="n">Sent</th><th class="n">Peak heap</th></tr></thead><tbody>
{{range .Stages}}<tr>{{range $i, $c := .}}{{if $i}}<td class="n">{{$c}}</td>{{else}}<td>{{$c}}</td>{{end}}{{end}}</tr>{{end}}</tbody></table></div>{{end}}
<h2 style="margin-top:18px">Recent timeouts and memory kills <span class="muted">· from the app log</span></h2>
{{if .Incidents}}<ul class="why">{{range .Incidents}}<li><span class="muted">{{.Time}} UTC · {{.Stage}}</span><pre class="msg">{{.Message}}</pre></li>{{end}}</ul>{{else}}<p class="muted">None in the app log.</p>{{end}}

<h2 style="margin-top:22px">Actions</h2><p class="muted">Each action is written to the app's log, which the owner and their agent read, and to the operator audit below.</p><div class="forms">
<form method="post" action="/operator/resources/{{.App}}/action"><input type="hidden" name="csrf" value="{{.CSRF}}"><input type="hidden" name="window" value="{{.Window.Key}}"><input type="hidden" name="action" value="notify">
<h2>Email the owner</h2><p class="muted">A polite notice of what is heavy, with the upgrade link. Edit it before sending.</p>
<label for="n-subject">Subject</label><input id="n-subject" name="subject" value="{{.NoticeSubject}}" maxlength="150">
<label for="n-text">Message</label><textarea id="n-text" name="text" rows="12">{{.Notice}}</textarea><button>Send email</button></form>
<form method="post" action="/operator/resources/{{.App}}/action"><input type="hidden" name="csrf" value="{{.CSRF}}"><input type="hidden" name="window" value="{{.Window.Key}}">
<h2>Throttle for a while</h2><p class="muted">Only this app is limited; other apps keep their slots. The owner sees a notice in the admin panel until it ends.</p>
<label for="t-slots">Scripts at the same time</label><select id="t-slots" name="slots"><option value="1">1</option><option value="0">normal ({{.MaxSlots}})</option></select>
<label for="t-min">Script runs a minute <span class="muted">(empty: no budget)</span></label><input id="t-min" name="per_minute" inputmode="numeric" placeholder="e.g. 30">
<label for="t-hours">Until</label><select id="t-hours" name="hours"><option value="1">1 hour from now</option><option value="6">6 hours from now</option><option value="24" selected>24 hours from now</option><option value="72">3 days from now</option><option value="168">7 days from now</option></select>
<label for="t-reason">Reason <span class="muted">(the owner sees it)</span></label><input id="t-reason" name="reason" maxlength="200" placeholder="long-running scripts slowed other apps">
<button name="action" value="throttle">Throttle</button>{{if .Throttled}} <button class="secondary" name="action" value="unthrottle">End throttle now</button>{{end}}</form>
<form method="post" action="/operator/resources/{{.App}}/action"><input type="hidden" name="csrf" value="{{.CSRF}}"><input type="hidden" name="window" value="{{.Window.Key}}">
<h2>Suspend</h2><p class="muted">Takes the app offline on every host; its data is kept. Use it for abuse, or when a throttle is not enough.</p>
{{if .Suspended}}<button class="secondary" name="action" value="unsuspend">Bring back</button>{{else}}<label for="s-reason">Reason</label><input id="s-reason" name="reason" maxlength="200" placeholder="suspended by Renoxium"><button class="danger" name="action" value="suspend">Suspend</button>{{end}}</form></div>
<h2 style="margin-top:22px">Operator audit</h2>{{if .Audit}}<div class="scroll"><table><thead><tr><th>When</th><th>Operator</th><th>Action</th><th>Detail</th></tr></thead><tbody>
{{range .Audit}}<tr><td>{{.At}}</td><td>{{.Operator}}</td><td>{{.Action}}</td><td>{{.Detail}}</td></tr>{{end}}</tbody></table></div>{{else}}<p class="muted">No operator action on this app yet.</p>{{end}}
</div>{{template "bottom"}}{{end}}`
