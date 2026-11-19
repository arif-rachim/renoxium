package resources

import (
	"math"
	"strings"
	"testing"

	"github.com/arif-rachim/renoxium/internal/platform"
)

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

var prices = map[string]int{"pro": 99, "business": 299}

func byName(rows []Row) map[string]Row {
	out := map[string]Row{}
	for _, r := range rows {
		out[r.Name] = r
	}
	return out
}

func TestCostRevenueAndMargin(t *testing.T) {
	pr := Prices{CPUHour: 1, StorageGBMonth: 3, EgressGB: 2, WhatsApp: 0.5, Email: 0.01, RTConnHour: 0.1}
	apps := []App{
		// A Pro owner with three apps, two of them live: 99 a month split over the two live ones.
		{Name: "pro-a", Owner: "o1", Plan: "pro", PaysPlan: true, Live: true, Usage: platform.Usage{
			CPUUS: 1.8e9, DBUS: 1.8e9, ScriptUS: 3e9, BytesOut: 1 << 30, WhatsApp: 4, Emails: 100, RTConnSec: 7200, Runs: 10},
			Storage: platform.Storage{LiveDB: 1 << 29, Files: 1 << 29}},
		{Name: "pro-b", Owner: "o1", Plan: "pro", PaysPlan: true, Live: true, Usage: platform.Usage{ScriptUS: 1e9, Runs: 1}},
		{Name: "pro-draft", Owner: "o1", Plan: "pro", PaysPlan: true},
		// A granted Business plan brings in nothing.
		{Name: "granted", Owner: "o2", Plan: "business", PaysPlan: false, Live: true},
		// A Free owner: only its card fees.
		{Name: "free-shop", Owner: "o3", Plan: "free", Live: true, CardFeesAED: 12.5},
		// A paying owner with no live app: split over all of their apps.
		{Name: "biz-1", Owner: "o4", Plan: "business", PaysPlan: true},
		{Name: "biz-2", Owner: "o4", Plan: "business", PaysPlan: true},
	}
	rows := byName(Compute(Input{Apps: apps, Days: 30, Prices: pr, PlanPrice: prices}))
	a := rows["pro-a"]
	// CPU: 1 hour of worker CPU + database = 1 CPU-hour at AED 1.
	if !near(a.CPUHours, 1) || !near(a.Cost.CPU, 1) || !near(a.Cost.Storage, 3) || !near(a.Cost.Egress, 2) || !near(a.Cost.WhatsApp, 2) ||
		!near(a.Cost.Email, 1) || !near(a.Cost.Realtime, 0.2) || !near(a.Cost.Total, 9.2) {
		t.Fatalf("pro-a cost %+v cpu %v", a.Cost, a.CPUHours)
	}
	if !near(a.PlanRevenue, 49.5) || !near(rows["pro-b"].PlanRevenue, 49.5) || rows["pro-draft"].PlanRevenue != 0 {
		t.Fatalf("plan split: %v %v %v", a.PlanRevenue, rows["pro-b"].PlanRevenue, rows["pro-draft"].PlanRevenue)
	}
	if !near(a.Margin, 49.5-9.2) || !near(a.CostShare, 0.75) || !near(rows["pro-b"].CostShare, 0.25) {
		t.Fatalf("margin %v share %v", a.Margin, a.CostShare)
	}
	if rows["granted"].Revenue != 0 || !near(rows["free-shop"].Revenue, 12.5) || !near(rows["free-shop"].FeeRevenue, 12.5) {
		t.Fatalf("granted %v free %v", rows["granted"].Revenue, rows["free-shop"].Revenue)
	}
	if !near(rows["biz-1"].PlanRevenue, 149.5) || !near(rows["biz-2"].PlanRevenue, 149.5) {
		t.Fatalf("no live app: %v", rows["biz-1"].PlanRevenue)
	}
	// A 7-day window takes 7/30 of the price and of a month's storage.
	week := byName(Compute(Input{Apps: apps, Days: 7, Prices: pr, PlanPrice: prices}))
	if !near(week["pro-a"].PlanRevenue, 99*7.0/30/2) || !near(week["pro-a"].Cost.Storage, 3*7.0/30) {
		t.Fatalf("week: %v %v", week["pro-a"].PlanRevenue, week["pro-a"].Cost.Storage)
	}
}

func TestFlags(t *testing.T) {
	var apps []App
	// 120 quiet paying apps, so the top 1% is 2 apps and the top 10% is 12.
	for i := 0; i < 120; i++ {
		apps = append(apps, App{Name: "quiet-" + string(rune('a'+i%26)) + string(rune('a'+i/26)), Owner: "q" + string(rune(i)), Plan: "pro", PaysPlan: true, Live: true,
			Usage: platform.Usage{Runs: 100, Requests: 100, ScriptUS: 1e6, CPUUS: 1e6}})
	}
	apps = append(apps,
		// The heaviest app, on Free, making others wait, failing often.
		App{Name: "hog", Owner: "h", Plan: "free", Live: true, Usage: platform.Usage{Runs: 1000, Requests: 1000, ScriptUS: 3600e6, CPUUS: 3600e6,
			CausedWaitUS: 30e6, HeldWhileWaiting: 40, Timeouts: 50, MemKills: 2, BytesOut: 2 << 30}},
		// Heavy too, but causes no waits: not a noisy neighbour.
		App{Name: "busy-pro", Owner: "b", Plan: "pro", PaysPlan: true, Live: true, Usage: platform.Usage{Runs: 1000, ScriptUS: 2000e6, CPUUS: 1000e6}},
		// Fails a little: 2 incidents are below the minimum.
		App{Name: "flaky", Owner: "f", Plan: "pro", PaysPlan: true, Live: true, Usage: platform.Usage{Runs: 10, Timeouts: 2}},
		// A demo is never "unprofitable" or over fair use.
		App{Name: "demo-store", Owner: platform.DemoOwner, Plan: "demo", Demo: true, Usage: platform.Usage{Runs: 1000, ScriptUS: 2000e6, CPUUS: 900000e6}},
		// Idle: no flags at all.
		App{Name: "idle", Owner: "i", Plan: "free"},
	)
	pr := Defaults
	pr.CPUHour = 5 // make the hog expensive
	rows := byName(Compute(Input{Apps: apps, Days: 1, Prices: pr, PlanPrice: prices}))
	h := rows["hog"]
	for _, f := range []string{FlagNoisy, FlagUnprofitable, FlagErrors, FlagFairUse} {
		if !h.Has(f) {
			t.Errorf("hog lacks %s: %+v", f, h.Flags)
		}
	}
	for _, f := range h.Flags {
		if f.Why == "" || f.Label == "" {
			t.Errorf("flag without explanation: %+v", f)
		}
	}
	if !strings.Contains(flagWhy(h, FlagErrors), "52 of 1000") || !strings.Contains(flagWhy(h, FlagFairUse), "3600 script seconds a day") ||
		!strings.Contains(flagWhy(h, FlagFairUse), "2048 MB sent a day") || !strings.Contains(flagWhy(h, FlagNoisy), "30.0 s") {
		t.Errorf("explanations: %+v", h.Flags)
	}
	if b := rows["busy-pro"]; b.Has(FlagNoisy) || b.Has(FlagUnprofitable) {
		t.Errorf("busy-pro (AED 3.30 of plan a day, costs AED 1.39) %+v margin %v", b.Flags, b.Margin)
	}
	if rows["flaky"].Has(FlagErrors) {
		t.Error("flaky below the minimum incidents")
	}
	if d := rows["demo-store"]; d.Has(FlagUnprofitable) || d.Has(FlagFairUse) {
		t.Errorf("demo flagged: %+v", d.Flags)
	}
	if len(rows["idle"].Flags) != 0 || rows["idle"].Active() {
		t.Error("idle app flagged")
	}
	for name, r := range rows {
		if strings.HasPrefix(name, "quiet-") && len(r.Flags) > 0 {
			t.Fatalf("%s flagged: %+v", name, r.Flags)
		}
	}
	// Fair use follows the operator's thresholds.
	pr.FairScriptSecDay, pr.FairEgressMBDay = 1e9, 1e9
	if byName(Compute(Input{Apps: apps, Days: 1, Prices: pr, PlanPrice: prices}))["hog"].Has(FlagFairUse) {
		t.Error("fair use ignores the thresholds")
	}
}

func flagWhy(r Row, key string) string {
	for _, f := range r.Flags {
		if f.Key == key {
			return f.Why
		}
	}
	return ""
}

func TestTopN(t *testing.T) {
	for _, c := range []struct {
		n    int
		s    float64
		want int
	}{{0, 0.01, 0}, {1, 0.01, 1}, {99, 0.01, 1}, {101, 0.01, 2}, {10, 0.1, 1}, {11, 0.1, 2}} {
		if got := topN(c.n, c.s); got != c.want {
			t.Errorf("topN(%d, %v) = %d, want %d", c.n, c.s, got, c.want)
		}
	}
}

func TestFromSettings(t *testing.T) {
	p := FromSettings(map[string]string{SettingPrefix + "cpu_hour": "0.2", SettingPrefix + "email": "oops", SettingPrefix + "egress_gb": "-1"})
	if p.CPUHour != 0.2 || p.Email != Defaults.Email || p.EgressGB != Defaults.EgressGB || p.WhatsApp != Defaults.WhatsApp {
		t.Fatalf("%+v", p)
	}
}
