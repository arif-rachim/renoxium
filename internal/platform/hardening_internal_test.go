package platform

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/arif-rachim/renoxium/internal/appdef"
	"github.com/arif-rachim/renoxium/internal/sandbox"
)

// An app that publishes to ever new channel names must not grow the memory
// held for "last message per channel".
func TestRealtimeKeepsFewLastMessages(t *testing.T) {
	var h liveHub
	h.init(nil)
	for i := 0; i < 5*liveLastChannels; i++ {
		h.publish("shop/live", fmt.Sprintf("shop/live/c%d", i), make([]byte, liveMaxMessage))
	}
	if n := len(h.last["shop/live"]); n != liveLastChannels {
		t.Fatalf("%d last messages kept, want %d", n, liveLastChannels)
	}
	if _, kept := h.last["shop/live"][fmt.Sprintf("shop/live/c%d", 5*liveLastChannels-1)]; !kept {
		t.Fatal("the newest channel lost its message")
	}
	if _, kept := h.last["shop/live"]["shop/live/c0"]; kept {
		t.Fatal("the oldest channel kept its message")
	}
	// A channel that is published to again keeps its place.
	h.publish("shop/live", "shop/live/steady", []byte("1"))
	for i := 0; i < liveLastChannels-1; i++ {
		h.publish("shop/live", fmt.Sprintf("shop/live/n%d", i), []byte("x"))
		h.publish("shop/live", "shop/live/steady", []byte("2"))
	}
	if m := h.last["shop/live"]["shop/live/steady"]; string(m.data) != "2" {
		t.Fatal("a channel in use lost its message")
	}
	// Another app is not affected by this one's channels.
	h.publish("other/live", "other/live/a", []byte("x"))
	if len(h.last["other/live"]) != 1 {
		t.Fatal("apps share the cap")
	}
}

type slowAPI time.Duration

func (d slowAPI) RoundTrip(r *http.Request) (*http.Response, error) {
	select {
	case <-time.After(time.Duration(d)):
	case <-r.Context().Done():
		return nil, r.Context().Err()
	}
	return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("ok")), Request: r}, nil
}

// A script that calls a slow service holds a script worker. One run may
// wait only fetchWaitPerRun in all, and one account only fetchWaitPerOwner
// per minute.
func TestFetchWaitIsBounded(t *testing.T) {
	defer func(a, b time.Duration) { fetchWaitPerRun, fetchWaitPerOwner = a, b }(fetchWaitPerRun, fetchWaitPerOwner)
	fetchWaitPerRun, fetchWaitPerOwner = 600*time.Millisecond, 900*time.Millisecond

	p, err := New(Config{DataDir: t.TempDir(), AppsDomain: "renoxium.test", PublicScheme: "https", SiteURL: "https://renoxium.test",
		FetchTransport: slowAPI(400 * time.Millisecond)}, sandbox.NewGoja())
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	def := &appdef.App{Scripts: map[string]string{"slow": `function handle(req) {
  const out = [];
  for (let i = 0; i < 5; i++) { try { http.fetch({url: 'https://slow.example/x', timeout_ms: 10000}); out.push('ok'); } catch (e) { out.push(String(e)); } }
  return {text: out.join('|')} }`},
		Routes: []appdef.Route{{Method: "POST", Path: "/slow", Script: "slow"}},
		Hosts:  []appdef.Host{{Host: "slow.example", Purpose: "x"}}}
	p.CreateApp("usr_a", "relay")
	if res, err := p.ApplyChange(context.Background(), "relay", def); err != nil || !res.OK {
		t.Fatal(err, res)
	}
	p.AllowHost("relay", "slow.example", "owner")
	p.SetPreviewFetch("relay", true)
	run := func() (string, time.Duration) {
		start := time.Now()
		w := httptest.NewRecorder()
		p.ServeApp(w, httptest.NewRequest("POST", "http://relay--preview.renoxium.test/slow", nil))
		return w.Body.String(), time.Since(start)
	}
	body, took := run()
	// Two calls of 400 ms fit in 600 ms of waiting (the second is cut at the
	// limit); the rest are refused without waiting.
	if took > 1500*time.Millisecond || strings.Count(body, "already took") != 3 {
		t.Fatalf("one run waited %s: %s", took, body)
	}
	// The first run used 600 ms of the account's 900 ms; the next run gets a
	// little, and then the account has to wait for the next minute.
	run()
	if body, took = run(); took > 300*time.Millisecond || strings.Count(body, "waited more than") != 5 {
		t.Fatalf("after the account's waiting time: %s in %s", body, took)
	}
}

// The scheduler does not wait for jobs: a slow job of one account must not
// delay the check that starts everyone else's.
func TestSlowJobsDoNotBlockTheScheduler(t *testing.T) {
	p, err := New(Config{DataDir: t.TempDir(), AppsDomain: "renoxium.test", PublicScheme: "https", ScriptsPerApp: 2, ScriptsPerOwner: 2}, sandbox.NewGoja())
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	ctx := context.Background()
	app := func(script string) *appdef.App {
		return &appdef.App{
			Tables:    []appdef.Table{{Name: "runs", Columns: []appdef.Column{{Name: "job", Type: "text"}}}},
			Templates: map[string]string{"t": "x"},
			Scripts:   map[string]string{"work": script},
			Routes:    []appdef.Route{{Method: "GET", Path: "/", Template: "t"}},
			Jobs:      []appdef.Job{{Name: "one", Script: "work", Every: "1m"}, {Name: "two", Script: "work", Every: "1m"}},
		}
	}
	slow := `function handle(req) { var t = Date.now(); while (Date.now() - t < 700) {} db.exec("INSERT INTO runs (job) VALUES (?)", req.body.job); return {text: "ok"} }`
	fast := `function handle(req) { db.exec("INSERT INTO runs (job) VALUES (?)", req.body.job); return {text: "ok"} }`
	for _, a := range []struct{ owner, name, script string }{{"usr_slow", "slow-shop", slow}, {"usr_fast", "fast-shop", fast}} {
		p.CreateApp(a.owner, a.name)
		if res, err := p.ApplyChange(ctx, a.name, app(a.script)); err != nil || !res.OK {
			t.Fatal(err, res)
		}
		if _, err := p.Publish(a.name, 0); err != nil {
			t.Fatal(err)
		}
	}
	runs := func(app string) int {
		_, n, _ := p.AdminList(ctx, app, "runs", "", 1)
		return n
	}
	now := time.Now()
	p.runDueJobs(ctx, now, false) // first sight starts the clocks
	now = now.Add(2 * time.Minute)
	start := time.Now()
	if n := p.runDueJobs(ctx, now, false); n != 4 {
		t.Fatalf("started %d jobs, want 4", n)
	}
	if d := time.Since(start); d > 300*time.Millisecond {
		t.Fatalf("the scheduler waited %s for the jobs", d)
	}
	// While the slow account's queue runs, the next check skips it (its
	// jobs must not run twice) and the fast account is long done.
	time.Sleep(300 * time.Millisecond)
	if n := p.runDueJobs(ctx, now, false); n != 0 {
		t.Fatalf("a check during a running queue started %d jobs", n)
	}
	if runs("fast-shop") != 2 {
		t.Fatalf("the fast account ran %d of 2 jobs while the slow one was busy", runs("fast-shop"))
	}
	deadline := time.Now().Add(10 * time.Second)
	for runs("slow-shop") < 2 && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if runs("slow-shop") != 2 {
		t.Fatalf("the slow account ran %d of 2 jobs", runs("slow-shop"))
	}
	for i := 0; i < 100; i++ { // let the queue goroutine finish before Close
		if _, busy := p.jobs.running.Load("usr_slow"); !busy {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// Limits that cap money or guesses are stored, so a restart (or a crash
// someone causes on purpose) does not hand out a fresh allowance.
func TestDurableLimitsSurviveARestart(t *testing.T) {
	dir := t.TempDir()
	open := func() *Platform {
		p, err := New(Config{DataDir: dir, AppsDomain: "renoxium.test", PublicScheme: "https"}, sandbox.NewGoja())
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	p := open()
	for i := 0; i < 3; i++ {
		if !p.allowDurable("phone:shop:+971500000000", 3, 15*time.Minute) {
			t.Fatalf("use %d refused", i)
		}
	}
	p.Close()
	p = open()
	defer p.Close()
	if p.allowDurable("phone:shop:+971500000000", 3, 15*time.Minute) {
		t.Fatal("the limit started again after a restart")
	}
	if p.usedDurable("phone:shop:+971500000000", 15*time.Minute) != 4 {
		t.Fatal("uses were not counted")
	}
	// A window that has ended starts again.
	p.db.Exec(`UPDATE rate_windows SET start = start - 3600`)
	if !p.allowDurable("phone:shop:+971500000000", 3, 15*time.Minute) {
		t.Fatal("the limit did not start again after its window")
	}
}
