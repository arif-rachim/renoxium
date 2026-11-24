// Package resources turns metered usage into what each app costs the
// platform and brings in, and flags the apps an operator should look at:
// the ones that slow other apps down, cost more than they pay, fail often
// or use more than the Free plan's fair share.
package resources

import (
	"fmt"
	"math"
	"sort"
	"strconv"

	"github.com/arif-rachim/renoxium/internal/platform"
)

// Prices are the operator's unit costs in AED and the Free plan's fair-use
// thresholds. They are stored in the operator settings table.
type Prices struct {
	CPUHour        float64 // per hour of CPU: script CPU in the workers plus database time in the server
	StorageGBMonth float64 // per GB kept for a month (databases, files, visitor uploads)
	EgressGB       float64 // per GB of responses sent
	WhatsApp       float64 // per WhatsApp message
	Email          float64 // per email
	RTConnHour     float64 // per hour of one open realtime connection

	FairScriptSecDay float64 // Free plan: script seconds a day
	FairEgressMBDay  float64 // Free plan: MB of responses a day
}

// Defaults are reasonable for one small VPS in 2026: a 4 vCPU server at
// about AED 120 a month is AED 0.04 per vCPU-hour (0.05 leaves room for
// overheads), SSD and backups about AED 0.10 per GB-month, traffic through
// Cloudflare nearly free, a WhatsApp utility or authentication message in
// the UAE about AED 0.06, and transactional email about AED 0.002.
var Defaults = Prices{CPUHour: 0.05, StorageGBMonth: 0.10, EgressGB: 0.05, WhatsApp: 0.06, Email: 0.002, RTConnHour: 0.0005,
	FairScriptSecDay: 300, FairEgressMBDay: 500}

// Field describes one setting for the operator's form.
type Field struct {
	Key, Label, Unit string
	Get              func(*Prices) *float64
}

// Fields lists every setting in form order.
var Fields = []Field{
	{"cpu_hour", "CPU", "AED per CPU-hour", func(p *Prices) *float64 { return &p.CPUHour }},
	{"storage_gb_month", "Storage", "AED per GB-month", func(p *Prices) *float64 { return &p.StorageGBMonth }},
	{"egress_gb", "Bandwidth", "AED per GB sent", func(p *Prices) *float64 { return &p.EgressGB }},
	{"whatsapp", "WhatsApp", "AED per message", func(p *Prices) *float64 { return &p.WhatsApp }},
	{"email", "Email", "AED per email", func(p *Prices) *float64 { return &p.Email }},
	{"rt_conn_hour", "Realtime", "AED per connection-hour", func(p *Prices) *float64 { return &p.RTConnHour }},
	{"fair_script_sec_day", "Free fair use: scripts", "script seconds a day", func(p *Prices) *float64 { return &p.FairScriptSecDay }},
	{"fair_egress_mb_day", "Free fair use: bandwidth", "MB a day", func(p *Prices) *float64 { return &p.FairEgressMBDay }},
}

// SettingPrefix is the operator settings key prefix of every field.
const SettingPrefix = "resources:"

// FromSettings reads prices from stored settings; missing or invalid
// values keep the default.
func FromSettings(stored map[string]string) Prices {
	p := Defaults
	for _, f := range Fields {
		if v, ok := stored[SettingPrefix+f.Key]; ok {
			if n, err := strconv.ParseFloat(v, 64); err == nil && n >= 0 && !math.IsInf(n, 0) {
				*f.Get(&p) = n
			}
		}
	}
	return p
}

// App is one app as the model needs it.
type App struct {
	Name, Owner, OwnerEmail string
	Plan                    string // free or member ("demo" for a template demo)
	Live                    bool   // published and not suspended
	Demo                    bool   // a template demo of the platform itself
	Usage                   platform.Usage
	Storage                 platform.Storage
	CardFeesAED             float64 // Free-plan application fees from this app's live card payments in the window
}

// Cost is an app's estimated cost in AED over the window.
type Cost struct {
	CPU, Storage, Egress, WhatsApp, Email, Realtime, Total float64
}

// Flag is a reason to look at an app.
type Flag struct {
	Key, Label, Why string
}

// Flag keys.
const (
	FlagNoisy        = "noisy"
	FlagUnprofitable = "unprofitable"
	FlagErrors       = "errors"
	FlagFairUse      = "fairuse"
)

// Row is one app's figures over the window.
type Row struct {
	App
	CPUHours    float64 // CPU in the workers plus database time
	ScriptHours float64 // time holding a worker
	EgressGB    float64
	StorageGB   float64
	Cost        Cost
	FeeRevenue  float64 // shop fees on the app's card payments
	Revenue     float64
	Margin      float64
	CostShare   float64 // share of all apps' script time, 0..1
	Incidents   int64   // timeouts and memory kills
	Flags       []Flag
}

// Active is true when the app used anything in the window.
func (r Row) Active() bool { return r.Usage.Runs > 0 || r.Usage.Requests > 0 || r.Usage.RTConnSec > 0 }

// Has reports whether the row carries a flag.
func (r Row) Has(key string) bool {
	for _, f := range r.Flags {
		if f.Key == key {
			return true
		}
	}
	return false
}

// Input is everything Compute needs.
type Input struct {
	Apps   []App // every app, idle ones too
	Days   float64
	Prices Prices
}

const gb = 1 << 30

// Thresholds of the flags.
const (
	NoisyTopShare       = 0.01 // top 1% of active apps by script time ...
	NoisyMinCausedUS    = 1e6  // ... whose runs made others wait at least 1 s in the window
	HeavyTopShare       = 0.10 // top 10% by estimated cost ...
	HeavyMinCost        = 0.01 // ... costing at least AED 0.01, with a negative margin
	ErrorMinIncidents   = 3    // at least 3 timeouts or memory kills ...
	ErrorMinRate        = 0.02 // ... in at least 2% of runs
	MinCountedForRankBy = 1    // the top shares always include at least one app
)

// Compute works out cost, revenue, margin and flags for every app.
//
// An app's revenue is the shop fees on its own card payments; account plans
// cost nothing.
func Compute(in Input) []Row {
	days := math.Max(in.Days, 1.0/24)
	pr := in.Prices
	rows := make([]Row, len(in.Apps))
	var totalScript float64
	for i, a := range in.Apps {
		u := a.Usage
		r := Row{App: a}
		r.CPUHours = float64(u.CPUUS+u.DBUS) / 3.6e9
		r.ScriptHours = float64(u.ScriptUS) / 3.6e9
		r.EgressGB = float64(u.BytesOut) / gb
		r.StorageGB = float64(a.Storage.Total()) / gb
		r.Cost.CPU = r.CPUHours * pr.CPUHour
		r.Cost.Storage = r.StorageGB * pr.StorageGBMonth * days / 30
		r.Cost.Egress = r.EgressGB * pr.EgressGB
		r.Cost.WhatsApp = float64(u.WhatsApp) * pr.WhatsApp
		r.Cost.Email = float64(u.Emails) * pr.Email
		r.Cost.Realtime = float64(u.RTConnSec) / 3600 * pr.RTConnHour
		r.Cost.Total = r.Cost.CPU + r.Cost.Storage + r.Cost.Egress + r.Cost.WhatsApp + r.Cost.Email + r.Cost.Realtime
		r.FeeRevenue = a.CardFeesAED
		r.Revenue = r.FeeRevenue
		r.Margin = r.Revenue - r.Cost.Total
		r.Incidents = u.Timeouts + u.MemKills
		totalScript += float64(u.ScriptUS)
		rows[i] = r
	}
	if totalScript > 0 {
		for i := range rows {
			rows[i].CostShare = float64(rows[i].Usage.ScriptUS) / totalScript
		}
	}
	flag(rows, days, pr)
	return rows
}

// topN is how many apps make the top share of n active apps.
func topN(n int, share float64) int {
	if n == 0 {
		return 0
	}
	return max(MinCountedForRankBy, int(math.Ceil(float64(n)*share)))
}

func flag(rows []Row, days float64, pr Prices) {
	var active []int
	for i, r := range rows {
		if r.Active() {
			active = append(active, i)
		}
	}
	byScript := append([]int(nil), active...)
	sort.SliceStable(byScript, func(i, j int) bool { return rows[byScript[i]].Usage.ScriptUS > rows[byScript[j]].Usage.ScriptUS })
	byCost := append([]int(nil), active...)
	sort.SliceStable(byCost, func(i, j int) bool { return rows[byCost[i]].Cost.Total > rows[byCost[j]].Cost.Total })

	for _, i := range byScript[:topN(len(byScript), NoisyTopShare)] {
		r := &rows[i]
		if float64(r.Usage.CausedWaitUS) >= NoisyMinCausedUS {
			r.Flags = append(r.Flags, Flag{FlagNoisy, "Noisy neighbour", fmt.Sprintf(
				"Among the top 1%% of apps by script time (%.0f%% of all script time), and while its scripts held workers, other apps waited %s in all for one (%d times).",
				100*r.CostShare, Seconds(r.Usage.CausedWaitUS), r.Usage.HeldWhileWaiting)})
		}
	}
	for _, i := range byCost[:topN(len(byCost), HeavyTopShare)] {
		r := &rows[i]
		if r.Margin < 0 && r.Cost.Total >= HeavyMinCost && !r.Demo {
			r.Flags = append(r.Flags, Flag{FlagUnprofitable, "Unprofitable heavy", fmt.Sprintf(
				"Among the 10%% most expensive apps: it costs about AED %.2f and brings in AED %.2f, a loss of AED %.2f in this period.",
				r.Cost.Total, r.Revenue, -r.Margin)})
		}
	}
	for i := range rows {
		r := &rows[i]
		u := r.Usage
		if r.Incidents >= ErrorMinIncidents && u.Runs > 0 && float64(r.Incidents)/float64(u.Runs) >= ErrorMinRate {
			r.Flags = append(r.Flags, Flag{FlagErrors, "Error-prone", fmt.Sprintf(
				"%d of %d script runs (%.0f%%) were stopped: %d at the time limit, %d at the memory limit%s. Each one holds a worker until it is stopped.",
				r.Incidents, u.Runs, 100*float64(r.Incidents)/float64(u.Runs), u.Timeouts, u.MemKills, restarts(u.Restarts))})
		}
		if r.Plan == "free" && !r.Demo {
			sec := float64(u.ScriptUS) / 1e6 / days
			mb := float64(u.BytesOut) / (1 << 20) / days
			var over []string
			if pr.FairScriptSecDay > 0 && sec > pr.FairScriptSecDay {
				over = append(over, fmt.Sprintf("%.0f script seconds a day (fair use %.0f)", sec, pr.FairScriptSecDay))
			}
			if pr.FairEgressMBDay > 0 && mb > pr.FairEgressMBDay {
				over = append(over, fmt.Sprintf("%.0f MB sent a day (fair use %.0f)", mb, pr.FairEgressMBDay))
			}
			if len(over) > 0 {
				why := "On the Free plan and above its fair use: " + over[0]
				if len(over) > 1 {
					why += " and " + over[1]
				}
				r.Flags = append(r.Flags, Flag{FlagFairUse, "Free plan over fair use", why + "."})
			}
		}
	}
}

func restarts(n int64) string {
	if n == 0 {
		return ""
	}
	return fmt.Sprintf("; the server killed and restarted the worker %d time%s", n, map[bool]string{true: "", false: "s"}[n == 1])
}

// Seconds formats microseconds for people.
func Seconds(us int64) string {
	s := float64(us) / 1e6
	switch {
	case s < 1:
		return fmt.Sprintf("%.0f ms", s*1000)
	case s < 120:
		return fmt.Sprintf("%.1f s", s)
	case s < 7200:
		return fmt.Sprintf("%.0f min", s/60)
	default:
		return fmt.Sprintf("%.1f h", s/3600)
	}
}

// Bytes formats a byte count.
func Bytes(n int64) string {
	switch {
	case n < 1<<10:
		return fmt.Sprintf("%d B", n)
	case n < 1<<20:
		return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
	case n < 1<<30:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	default:
		return fmt.Sprintf("%.2f GB", float64(n)/(1<<30))
	}
}
