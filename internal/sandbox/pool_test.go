package sandbox

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	ServeWorkerIfRequested()
	os.Exit(m.Run())
}

func newPool(t *testing.T, workers int) *Pool {
	t.Helper()
	// A small heap limit and a generous timeout keep memory tests from racing
	// the time limit, also under the race detector.
	p, err := NewPool(PoolConfig{Workers: workers, Timeout: 3 * time.Second, HeapLimit: 16 << 20})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	return p
}

const echo = `function handle(req) {
	const rows = db.query("SELECT ?", req.params.id);
	const r = db.exec("INSERT INTO t VALUES (?)", 1);
	console.log("hello", req.params.id);
	return {template: "page", data: {rows: rows, id: r.lastInsertId, big: 9007199254740993}, status: 201};
}`

func TestPoolRunsScriptsWithHostCalls(t *testing.T) {
	p := newPool(t, 2)
	h := &fakeHost{}
	r, err := p.Run(context.Background(), echo, Request{Method: "GET", Path: "/", Params: map[string]string{"id": "5"}}, h)
	if err != nil {
		t.Fatal(err)
	}
	data := r.Data.(map[string]any)
	if r.Template != "page" || r.Status != 201 || data["id"] != int64(7) || len(h.logs) != 1 || h.logs[0] != "info: hello 5" {
		t.Fatalf("response = %+v logs = %v", r, h.logs)
	}
	rows := data["rows"].([]any)
	if rows[0].(map[string]any)["n"] != int64(1) {
		t.Fatalf("rows = %v", rows)
	}
}

func TestPoolStopsRunawayScriptsAndKeepsServing(t *testing.T) {
	p := newPool(t, 1)
	for _, tc := range []struct{ src, want string }{
		{`function handle(req) { while (true) {} }`, "time limit exceeded"},
		{`function handle(req) { const a = []; for (let i = 0; ; i++) a.push({i: i, s: "abcdefghij" + i}); }`, "memory limit exceeded"},
		// goja may refuse the string length before the memory limit is reached.
		{`function handle(req) { let s = "x"; while (true) s = s + s; }`, ""},
	} {
		start := time.Now()
		_, err := p.Run(context.Background(), tc.src, Request{}, &fakeHost{})
		var se *ScriptError
		if !errors.As(err, &se) || !strings.Contains(se.Message, tc.want) {
			t.Fatalf("%s: err = %v", tc.src, err)
		}
		if time.Since(start) > 5*time.Second {
			t.Fatalf("%s: took %s", tc.src, time.Since(start))
		}
		// The single worker (or its replacement) still serves the next request.
		if _, err := p.Run(context.Background(), `function handle(req) { return {text: "ok"} }`, Request{}, &fakeHost{}); err != nil {
			t.Fatalf("after %s: %v", tc.src, err)
		}
	}
}

func TestPoolCheckRunsTopLevelCodeInAWorker(t *testing.T) {
	p := newPool(t, 1)
	if err := p.Check(`const a = []; for (let i = 0; ; i++) a.push({i: i, s: "x" + i}); function handle() {}`); err == nil ||
		!strings.Contains(err.Error(), "memory limit exceeded") {
		t.Fatalf("err = %v", err)
	}
	if err := p.Check(`function handle(req) { return {text: "x"} }`); err != nil {
		t.Fatal(err)
	}
}

func TestPoolReplacesKilledWorkers(t *testing.T) {
	p := newPool(t, 2)
	p.mu.Lock()
	for w := range p.all {
		syscall.Kill(w.pid, syscall.SIGKILL)
	}
	p.mu.Unlock()
	time.Sleep(50 * time.Millisecond)
	for i := 0; i < 4; i++ {
		if _, err := p.Run(context.Background(), `function handle(req) { return {text: "ok"} }`, Request{}, &fakeHost{}); err != nil {
			t.Fatalf("run %d after killing workers: %v", i, err)
		}
	}
}

func TestPoolRunsConcurrently(t *testing.T) {
	p := newPool(t, 2)
	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := p.Run(context.Background(), `function handle(req) { return {json: req.query.n} }`,
				Request{Query: map[string]string{"n": "7"}}, &fakeHost{})
			if err == nil && r.JSON != "7" {
				err = errors.New("wrong answer")
			}
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
}

// If the in-worker heap watch does not stop a script, the parent kills the
// worker when its resident memory passes RSSLimit.
func TestPoolKillsWorkerOverResidentLimit(t *testing.T) {
	p, err := NewPool(PoolConfig{Workers: 1, Timeout: 5 * time.Second, HeapLimit: 4 << 30, RSSLimit: 96 << 20})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	_, err = p.Run(context.Background(), `function handle(req) { const a = []; for (let i = 0; ; i++) a.push({i: i, s: "abcdefghij" + i}); }`, Request{}, &fakeHost{})
	var se *ScriptError
	if !errors.As(err, &se) || se.Message != "script stopped: memory limit exceeded" {
		t.Fatalf("err = %v", err)
	}
	if _, err := p.Run(context.Background(), `function handle(req) { return {text: "ok"} }`, Request{}, &fakeHost{}); err != nil {
		t.Fatalf("replacement worker: %v", err)
	}
}

func TestPoolPassesVisitorAndFormAll(t *testing.T) {
	p := newPool(t, 1)
	req := Request{User: &Visitor{ID: 7, Email: "a@b.ae", Name: "Ali", Verified: true}, FormAll: map[string][]string{"size": {"S", "M"}}}
	r, err := p.Run(context.Background(), `function handle(req) { return {json: {id: req.user.id, email: req.user.email, sizes: req.formAll.size}} }`, req, &fakeHost{})
	if err != nil {
		t.Fatal(err)
	}
	got := r.JSON.(map[string]any)
	if got["id"] != int64(7) || got["email"] != "a@b.ae" || len(got["sizes"].([]any)) != 2 {
		t.Fatalf("got %v", got)
	}
	r, _ = p.Run(context.Background(), `function handle(req) { return {json: req.user === null} }`, Request{}, &fakeHost{})
	if r.JSON != true {
		t.Fatalf("signed out user = %v", r.JSON)
	}
}
