package platform

import (
	"context"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/arif-rachim/renoxium/internal/appdef"
	"github.com/arif-rachim/renoxium/internal/applog"
	"github.com/arif-rachim/renoxium/internal/sandbox"
)

func meterPlatform(t *testing.T, sb sandbox.Sandbox, cfg Config) *Platform {
	t.Helper()
	cfg.DataDir, cfg.AppsDomain, cfg.PublicScheme = t.TempDir(), "renoxium.test", "https"
	p, err := New(cfg, sb)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	return p
}

// scriptApp has a cheap page, a busy loop of ?ms milliseconds and a script
// that never ends.
var scriptApp = &appdef.App{
	Scripts: map[string]string{
		"page": `function handle(req) { return {text: "x".repeat(100)} }`,
		"busy": `function handle(req) { const end = Date.now() + Number(req.params.ms); while (Date.now() < end) {} return {text: "done"} }`,
		"loop": `function handle(req) { while (true) {} }`,
	},
	// Routes with parameters are not run when a change is applied.
	Routes: []appdef.Route{{Method: "GET", Path: "/", Script: "page"}, {Method: "GET", Path: "/busy/:ms", Script: "busy"}, {Method: "GET", Path: "/loop/:x", Script: "loop"}},
}

func publishApp(t *testing.T, p *Platform, owner, name string, def *appdef.App) {
	t.Helper()
	if _, err := p.CreateApp(owner, name); err != nil {
		t.Fatal(err)
	}
	if res, err := p.ApplyChange(context.Background(), name, def); err != nil || !res.OK {
		t.Fatal(err, res.Problems)
	}
	if _, err := p.Publish(name, 0); err != nil {
		t.Fatal(err)
	}
}

func get(p *Platform, app, path string) int {
	w := httptest.NewRecorder()
	p.ServeApp(w, httptest.NewRequest("GET", "http://"+app+".renoxium.test"+path, nil))
	return w.Code
}

func stageUsage(t *testing.T, p *Platform, app, stage string) Usage {
	t.Helper()
	rows, err := p.UsageByStage(time.Now().Add(-24*time.Hour), time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.App == app && r.Stage == stage {
			return r.Usage
		}
	}
	return Usage{}
}

func TestMeterAttributesToTheRightAppUnderConcurrency(t *testing.T) {
	p := meterPlatform(t, sandbox.NewGoja(), Config{ScriptsPerApp: 4, ScriptsPerOwner: 8})
	page := &appdef.App{Scripts: map[string]string{"page": scriptApp.Scripts["page"]}, Routes: scriptApp.Routes[:1]}
	publishApp(t, p, "usr_a", "alpha-shop", page)
	publishApp(t, p, "usr_b", "beta-shop", page)
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i%4 == 0 {
				get(p, "beta-shop", "/")
			} else {
				get(p, "alpha-shop", "/")
			}
		}(i)
		if i%10 == 9 {
			p.flushMeter(false) // flushing while requests run loses nothing
		}
	}
	wg.Wait()
	a, b := stageUsage(t, p, "alpha-shop", Live), stageUsage(t, p, "beta-shop", Live)
	if a.Requests != 30 || a.Runs != 30 || b.Requests != 10 || b.Runs != 10 {
		t.Fatalf("alpha %d requests %d runs, beta %d requests %d runs", a.Requests, a.Runs, b.Requests, b.Runs)
	}
	if a.BytesOut != 30*100 || a.ScriptUS <= 0 || a.CPUUS < a.ScriptUS {
		t.Fatalf("alpha bytes %d script %d cpu %d (CPU falls back to wall time in process)", a.BytesOut, a.ScriptUS, a.CPUUS)
	}
	names, _ := p.UsageNames("alpha-shop", time.Now().Add(-time.Hour), 10)
	// 30 on live and 1 on preview, where the change was checked.
	if len(names) != 1 || names[0].Name != "GET /" || names[0].Runs != 31 {
		t.Fatalf("names %+v", names)
	}
	// The same counts after everything is written, and nothing twice.
	p.flushMeter(true)
	if a2 := stageUsage(t, p, "alpha-shop", Live); a2 != a {
		t.Fatalf("after flush %+v, before %+v", a2, a)
	}
}

func TestMeterRollsOverHoursAndKeeps90Days(t *testing.T) {
	p := meterPlatform(t, sandbox.NewGoja(), Config{})
	clock := time.Date(2026, 11, 19, 10, 59, 0, 0, time.UTC)
	var mu sync.Mutex
	p.meter.now = func() time.Time { mu.Lock(); defer mu.Unlock(); return clock }
	advance := func(d time.Duration) { mu.Lock(); clock = clock.Add(d); mu.Unlock() }

	p.meterRequest("shop", Live, 10)
	p.meterMax("shop", Live, mRTPeak, 4)
	advance(2 * time.Minute) // 11:01: a new hour
	p.meterRequest("shop", Live, 20)
	p.meterRequest("shop", Live, 20)
	p.meterMax("shop", Live, mRTPeak, 2)
	check := func(when string) {
		t.Helper()
		hours, err := p.UsageHours("shop", clock.Add(-time.Hour), clock)
		if err != nil {
			t.Fatal(err)
		}
		if len(hours) != 2 || hours[0].Requests != 1 || hours[0].BytesOut != 10 || hours[0].RTPeak != 4 ||
			hours[1].Requests != 2 || hours[1].BytesOut != 40 || hours[1].RTPeak != 2 {
			t.Fatalf("%s: %+v", when, hours)
		}
	}
	check("in memory")
	p.flushMeter(false) // retires the current bucket; the 10:00 one is written
	check("partly written")
	p.meterRequest("shop", Live, 0) // the same hour continues in a new bucket
	p.flushMeter(false)
	p.flushMeter(false)
	var rows int
	p.db.QueryRow(`SELECT count(*) FROM app_usage_hourly WHERE app = 'shop'`).Scan(&rows)
	if rows != 2 {
		t.Fatalf("%d hourly rows", rows)
	}
	hours, _ := p.UsageHours("shop", clock.Add(-time.Hour), clock)
	if hours[1].Requests != 3 || hours[1].RTPeak != 2 {
		t.Fatalf("hour added up across flushes: %+v", hours[1])
	}

	// Retention: 90 days.
	old, kept := hourOf(clock.AddDate(0, 0, -91)), hourOf(clock.AddDate(0, 0, -89))
	for _, h := range []int64{old, kept} {
		p.db.Exec(`INSERT INTO app_usage_hourly (app, stage, hour, requests) VALUES ('shop', 'live', ?, 1)`, h)
		p.db.Exec(`INSERT INTO app_usage_names (app, stage, hour, name, runs, wall_us, cpu_us) VALUES ('shop', 'live', ?, 'GET /', 1, 1, 1)`, h)
	}
	p.cleanMeter()
	var n, names int
	p.db.QueryRow(`SELECT count(*) FROM app_usage_hourly WHERE hour <= ?`, kept).Scan(&n)
	p.db.QueryRow(`SELECT count(*) FROM app_usage_names WHERE hour <= ?`, kept).Scan(&names)
	if n != 1 || names != 1 {
		t.Fatalf("after cleaning: %d hourly rows, %d name rows older than now", n, names)
	}
}

func newMeterPool(t *testing.T, workers int, timeout time.Duration) *sandbox.Pool {
	t.Helper()
	pool, err := sandbox.NewPool(sandbox.PoolConfig{Workers: workers, Timeout: timeout, HeapLimit: 16 << 20})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func TestMeterNoisyNeighbourTimeoutsAndKills(t *testing.T) {
	pool := newMeterPool(t, 1, time.Second)
	p := meterPlatform(t, pool, Config{ScriptsPerApp: 2, ScriptsPerOwner: 2})
	hog := &appdef.App{Scripts: map[string]string{
		"busy": scriptApp.Scripts["busy"], "loop": scriptApp.Scripts["loop"],
		"mem": `function handle(req) { const a = []; for (let i = 0; ; i++) a.push({i: i, s: "abcdefghij" + i}) }`,
	}, Routes: []appdef.Route{{Method: "GET", Path: "/busy/:ms", Script: "busy"}, {Method: "GET", Path: "/loop/:x", Script: "loop"}, {Method: "GET", Path: "/mem/:x", Script: "mem"}}}
	publishApp(t, p, "usr_hog", "hog-shop", hog)
	publishApp(t, p, "usr_ok", "calm-shop", scriptApp)

	// The hog holds the only worker for 250 ms; the calm app's request waits.
	done := make(chan struct{})
	go func() { defer close(done); get(p, "hog-shop", "/busy/250") }()
	for deadline := time.Now().Add(5 * time.Second); p.Busy() == 0; time.Sleep(time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the hog never got a worker")
		}
	}
	if code := get(p, "calm-shop", "/"); code != 200 {
		t.Fatalf("calm: %d", code)
	}
	<-done
	h, c := stageUsage(t, p, "hog-shop", Live), stageUsage(t, p, "calm-shop", Live)
	if c.WorkerWaitUS < 100_000 || c.Waits != 1 || h.CausedWaitUS < 100_000 || h.HeldWhileWaiting != 1 || c.CausedWaitUS != 0 || h.WorkerWaitUS != 0 {
		t.Fatalf("hog %+v\ncalm %+v", h, c)
	}
	if h.CPUUS < 200_000 || h.CPUUS > h.ScriptUS {
		t.Fatalf("hog CPU %d µs of %d µs held", h.CPUUS, h.ScriptUS)
	}
	con := p.RecentContention(15)
	if con.Caused["hog-shop"] < 100_000 || con.Wait < 100*time.Millisecond {
		t.Fatalf("recent contention %+v", con)
	}

	// Limits: a timeout and a memory kill, both on the hog.
	get(p, "hog-shop", "/loop/1")
	get(p, "hog-shop", "/mem/1")
	h = stageUsage(t, p, "hog-shop", Live)
	if h.Timeouts != 1 || h.MemKills != 1 || h.HeapPeak < 8<<20 {
		t.Fatalf("limits: %+v", h)
	}
	if c2 := stageUsage(t, p, "calm-shop", Live); c2.Timeouts+c2.MemKills != 0 {
		t.Fatalf("calm got the hog's incidents: %+v", c2)
	}
	if con := p.RecentContention(15); con.MemKills["hog-shop"] != 1 {
		t.Fatalf("recent kills %+v", con.MemKills)
	}
	var server Usage
	rows, _ := p.UsageByStage(time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	for _, r := range rows {
		if r.App == PlatformRow {
			server = r.Usage
		}
	}
	if server.PeakScripts != 1 || server.ScriptUS < h.ScriptUS {
		t.Fatalf("server row %+v", server)
	}
}

func TestThrottleLimitsOnlyThatAppAndExpires(t *testing.T) {
	p := meterPlatform(t, sandbox.NewGoja(), Config{ScriptsPerApp: 2, ScriptsPerOwner: 4})
	publishApp(t, p, "usr_hog", "hog-shop", scriptApp)
	publishApp(t, p, "usr_ok", "calm-shop", scriptApp)
	if err := p.SetThrottle(Throttle{App: "hog-shop", Slots: 1, PerMinute: 3, Until: time.Now().Add(time.Hour), Reason: "slow loops", By: "ops@renoxium.ae"}); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []Throttle{{App: "hog-shop", Until: time.Now().Add(time.Hour)}, {App: "hog-shop", Slots: 1, Until: time.Now().Add(-time.Minute)},
		{App: "hog-shop", Slots: 9, Until: time.Now().Add(time.Hour)}, {App: "hog-shop", Slots: 1, Until: time.Now().Add(40 * 24 * time.Hour)},
		{App: "no-such-app", Slots: 1, Until: time.Now().Add(time.Hour)}} {
		if p.SetThrottle(bad) == nil {
			t.Errorf("throttle %+v accepted", bad)
		}
	}
	// Two at once: with one slot the second waits for the first.
	var wg sync.WaitGroup
	var slow atomic.Int64
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			start := time.Now()
			if get(p, "hog-shop", "/busy/200") == 200 {
				slow.Store(max(slow.Load(), int64(time.Since(start))))
			}
		}()
	}
	// Meanwhile the other app runs two at once without waiting.
	calmStart := time.Now()
	var cw sync.WaitGroup
	for range 2 {
		cw.Add(1)
		go func() { defer cw.Done(); get(p, "calm-shop", "/busy/200") }()
	}
	cw.Wait()
	calmTook := time.Since(calmStart)
	wg.Wait()
	if time.Duration(slow.Load()) < 380*time.Millisecond {
		t.Fatalf("the throttled app ran two scripts at once (slowest took %s)", time.Duration(slow.Load()))
	}
	if calmTook > 350*time.Millisecond {
		t.Fatalf("the other app was slowed down: %s", calmTook)
	}
	// The per-minute budget: 3 runs, then refused, for this app only.
	if code := get(p, "hog-shop", "/"); code != 200 {
		t.Fatalf("third run: %d", code)
	}
	if code := get(p, "hog-shop", "/"); code != 503 {
		t.Fatalf("over budget: %d", code)
	}
	for range 5 {
		if code := get(p, "calm-shop", "/"); code != 200 {
			t.Fatalf("other app: %d", code)
		}
	}
	h, c := stageUsage(t, p, "hog-shop", Live), stageUsage(t, p, "calm-shop", Live)
	if h.RefusedThrottle != 1 || h.SlotWaitUS < 100_000 || c.Refused() != 0 || c.SlotWaitUS > 50_000 {
		t.Fatalf("hog %+v\ncalm %+v", h, c)
	}
	logs, _ := p.Logs("hog-shop", applog.Filter{Level: "error"})
	found := false
	for _, e := range logs {
		found = found || strings.Contains(e.Message, "throttled by Renoxium")
	}
	if !found {
		t.Fatal("the refusal is not explained in the app log")
	}
	tr, ok := p.ThrottleOf("hog-shop")
	if !ok || !strings.Contains(tr.Notice("https://www.renoxium.test"), "1 script at a time and 3 script runs a minute") || !strings.Contains(tr.Notice("https://www.renoxium.test"), "slow loops") ||
		!strings.Contains(tr.Notice("https://www.renoxium.test"), "https://www.renoxium.test/support") {
		t.Fatalf("notice %q", tr.Notice("https://www.renoxium.test"))
	}

	// It ends by itself at its time.
	p.db.Exec(`UPDATE app_throttles SET until = ? WHERE app = 'hog-shop'`, time.Now().Add(-time.Second).Unix())
	p.loadThrottles()
	if _, ok := p.ThrottleOf("hog-shop"); ok {
		t.Fatal("expired throttle still applies")
	}
	p.limits = map[string]*window{} // a new minute
	if code := get(p, "hog-shop", "/"); code != 200 {
		t.Fatalf("after expiry: %d", code)
	}
	p.cleanThrottles()
	var left int
	p.db.QueryRow(`SELECT count(*) FROM app_throttles`).Scan(&left)
	if left != 0 {
		t.Fatal("expired throttle not cleaned")
	}
	// Ending it by hand works too.
	p.SetThrottle(Throttle{App: "hog-shop", Slots: 1, Until: time.Now().Add(time.Hour)})
	p.Unthrottle("hog-shop")
	if _, ok := p.ThrottleOf("hog-shop"); ok {
		t.Fatal("unthrottle")
	}
}

func TestMeterCountsMessagesJobsAndStorage(t *testing.T) {
	p := meterPlatform(t, sandbox.NewGoja(), Config{})
	def := &appdef.App{
		Tables:  []appdef.Table{{Name: "items", Columns: []appdef.Column{{Name: "n", Type: "text"}}}},
		Scripts: map[string]string{"pub": `function handle(req) { realtime.publish({channel: "public:x", data: {a: 1}}); return {text: "ok"} }`},
		Routes:  []appdef.Route{{Method: "GET", Path: "/", Script: "pub"}},
		Jobs:    []appdef.Job{{Name: "tick", Script: "pub", Every: "15m"}},
	}
	publishApp(t, p, "usr_a", "rt-shop", def)
	get(p, "rt-shop", "/")
	st, _ := p.loadStage("rt-shop", Live)
	if _, err := p.runJob(context.Background(), st, def.Jobs[0], time.Now(), "rid"); err != nil {
		t.Fatal(err)
	}
	u := stageUsage(t, p, "rt-shop", Live)
	if u.RTMessages != 2 || u.Jobs != 1 || u.JobUS <= 0 || u.Runs != 2 {
		t.Fatalf("%+v", u)
	}
	names, _ := p.UsageNames("rt-shop", time.Now().Add(-time.Hour), 0)
	got := map[string]int64{}
	for _, n := range names {
		got[n.Name] = n.Runs
	}
	if got["GET /"] != 2 || got["job tick"] != 1 { // one GET on preview when the change was applied
		t.Fatalf("names %v", got)
	}
	storage, err := p.StorageByApp()
	if err != nil || storage["rt-shop"].LiveDB <= 0 || storage["rt-shop"].PreviewDB <= 0 {
		t.Fatalf("storage %+v %v", storage, err)
	}
	p.Audit("ops@renoxium.ae", "rt-shop", "notify", "emailed the owner")
	if a, _ := p.AuditLog("rt-shop", 5); len(a) != 1 || a[0].Operator != "ops@renoxium.ae" {
		t.Fatalf("audit %+v", a)
	}
}

// The hot path: a request adds two counters; a script run adds a dozen and
// one entry in its route list. Both must cost nothing next to serving.
func BenchmarkMeterRequest(b *testing.B) {
	p := meterPlatform(&testing.T{}, sandbox.NewGoja(), Config{})
	defer p.Close()
	apps := make([]string, 100)
	for i := range apps {
		apps[i] = "app-" + string(rune('a'+i%26)) + string(rune('a'+i/26))
	}
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			p.meterRequest(apps[i%len(apps)], Live, 1234)
			i++
		}
	})
}

func BenchmarkMeterRun(b *testing.B) {
	p := meterPlatform(&testing.T{}, sandbox.NewGoja(), Config{})
	defer p.Close()
	s := &sandbox.RunStats{Wall: 3 * time.Millisecond, CPU: 2 * time.Millisecond, CPUKnown: true, HostDB: time.Millisecond, HeapPeak: 4 << 20}
	k := meterKey{"shop", Live}
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			p.meter.hold(k)
			p.meter.unhold(k)
			p.recordRun(k, "GET /products/:id", s, nil, nil)
		}
	})
}

// BenchmarkServeTemplatePage is a whole request of a template page, with
// the meter on, for scale.
func BenchmarkServeTemplatePage(b *testing.B) {
	t := &testing.T{}
	p := meterPlatform(t, sandbox.NewGoja(), Config{})
	defer p.Close()
	publishApp(t, p, "usr_a", "bench-shop", &appdef.App{Templates: map[string]string{"t": "<p>hello</p>"}, Routes: []appdef.Route{{Method: "GET", Path: "/", Template: "t"}}})
	b.ReportAllocs()
	for b.Loop() {
		get(p, "bench-shop", "/")
	}
}
