package resources

import (
	"math"
	"strings"
	"testing"

	"github.com/arif-rachim/renoxium/internal/platform"
)

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

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
		// A member's shop: its revenue is its own card fees.
		{Name: "shop-a", Owner: "o1", Plan: "member", Live: true, CardFeesAED: 49.5, Usage: platform.Usage{
			CPUUS: 1.8e9, DBUS: 1.8e9, ScriptUS: 3e9, BytesOut: 1 << 30, WhatsApp: 4, Emails: 100, RTConnSec: 7200, Runs: 10},
			Storage: platform.Storage{LiveDB: 1 << 29, Files: 1 << 29}},
		{Name: "shop-b", Owner: "o1", Plan: "member", Live: true, Usage: platform.Usage{ScriptUS: 1e9, Runs: 1}},
		// A free owner: only its card fees too.
		{Name: "free-shop", Owner: "o3", Plan: "free", Live: true, CardFeesAED: 12.5},
	}
	rows := byName(Compute(Input{Apps: apps, Days: 30, Prices: pr}))
	a := rows["shop-a"]
	// CPU: 1 hour of worker CPU + database = 1 CPU-hour at AED 1.
	if !near(a.CPUHours, 1) || !near(a.Cost.CPU, 1) || !near(a.Cost.Storage, 3) || !near(a.Cost.Egress, 2) || !near(a.Cost.WhatsApp, 2) ||
		!near(a.Cost.Email, 1) || !near(a.Cost.Realtime, 0.2) || !near(a.Cost.Total, 9.2) {
		t.Fatalf("shop-a cost %+v cpu %v", a.Cost, a.CPUHours)
	}
	if !near(a.Revenue, 49.5) || !near(a.FeeRevenue, 49.5) || rows["shop-b"].Revenue != 0 {
		t.Fatalf("revenue: %v %v", a.Revenue, rows["shop-b"].Revenue)
	}
	if !near(a.Margin, 49.5-9.2) || !near(a.CostShare, 0.75) || !near(rows["shop-b"].CostShare, 0.25) {
		t.Fatalf("margin %v share %v", a.Margin, a.CostShare)
	}
	if !near(rows["free-shop"].Revenue, 12.5) {
		t.Fatalf("free %v", rows["free-shop"].Revenue)
	}
	// A 7-day window takes 7/30 of a month's storage.
	week := byName(Compute(Input{Apps: apps, Days: 7, Prices: pr}))
	if !near(week["shop-a"].Cost.Storage, 3*7.0/30) {
		t.Fatalf("week: %v", week["shop-a"].Cost.Storage)
	}
}

func TestFlags(t *testing.T) {
	var apps []App
	// 120 quiet paying apps, so the top 1% is 2 apps and the top 10% is 12.
	for i := 0; i < 120; i++ {
		apps = append(apps, App{Name: "quiet-" + string(rune('a'+i%26)) + string(rune('a'+i/26)), Owner: "q" + string(rune(i)), Plan: "member", Live: true, CardFeesAED: 1,
			Usage: platform.Usage{Runs: 100, Requests: 100, ScriptUS: 1e6, CPUUS: 1e6}})
	}
	apps = append(apps,
		// The heaviest app, on Free, making others wait, failing often.
		App{Name: "hog", Owner: "h", Plan: "free", Live: true, Usage: platform.Usage{Runs: 1000, Requests: 1000, ScriptUS: 3600e6, CPUUS: 3600e6,
			CausedWaitUS: 30e6, HeldWhileWaiting: 40, Timeouts: 50, MemKills: 2, BytesOut: 2 << 30}},
		// Heavy too, but causes no waits: not a noisy neighbour.
		App{Name: "busy-pro", Owner: "b", Plan: "member", Live: true, CardFeesAED: 3.30, Usage: platform.Usage{Runs: 1000, ScriptUS: 2000e6, CPUUS: 1000e6}},
		// Fails a little: 2 incidents are below the minimum.
		App{Name: "flaky", Owner: "f", Plan: "member", Live: true, Usage: platform.Usage{Runs: 10, Timeouts: 2}},
		// A demo is never "unprofitable" or over fair use.
		App{Name: "demo-store", Owner: platform.DemoOwner, Plan: "demo", Demo: true, Usage: platform.Usage{Runs: 1000, ScriptUS: 2000e6, CPUUS: 900000e6}},
		// Idle: no flags at all.
		App{Name: "idle", Owner: "i", Plan: "free"},
	)
	pr := Defaults
	pr.CPUHour = 5 // make the hog expensive
	rows := byName(Compute(Input{Apps: apps, Days: 1, Prices: pr}))
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
		t.Errorf("busy-pro (AED 3.30 of card fees a day, costs AED 1.39) %+v margin %v", b.Flags, b.Margin)
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
	if byName(Compute(Input{Apps: apps, Days: 1, Prices: pr}))["hog"].Has(FlagFairUse) {
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
