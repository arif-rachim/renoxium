package sandbox

import (
	"context"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

// slowDBHost answers database calls after a pause, like a slow SQLite query
// in the server process.
type slowDBHost struct {
	fakeHost
	pause time.Duration
}

func (h *slowDBHost) Query(ctx context.Context, sql string, args []any) ([]map[string]any, error) {
	time.Sleep(h.pause)
	return h.fakeHost.Query(ctx, sql, args)
}

func TestPoolMeasuresCPUAndHostTime(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("CPU is measured on Linux only")
	}
	p := newPool(t, 1)
	var started atomic.Int32
	// A busy loop of about 300 ms: CPU follows wall time.
	busy := `function handle(req) { const end = Date.now() + 300; let n = 0; while (Date.now() < end) { n++ } return {text: "" + n} }`
	st := &RunStats{Started: func() { started.Add(1) }}
	if _, err := p.Run(WithStats(context.Background(), st), busy, Request{}, &fakeHost{}); err != nil {
		t.Fatal(err)
	}
	if !st.CPUKnown || st.CPU < 250*time.Millisecond || st.CPU > st.Wall || st.Wall < 300*time.Millisecond || started.Load() != 1 {
		t.Fatalf("busy run: %+v started %d", st, started.Load())
	}
	if st.HeapPeak <= 0 {
		t.Fatalf("no heap figure: %+v", st)
	}
	t.Logf("busy loop: wall %s, worker CPU %s, heap peak %d KiB", st.Wall, st.CPU, st.HeapPeak>>10)
	// A script that waits on the database: wall time but little CPU in the
	// worker, and the wait shows up as host time.
	wait := `function handle(req) { for (let i = 0; i < 4; i++) db.query("SELECT 1"); return {text: "ok"} }`
	st = &RunStats{}
	if _, err := p.Run(WithStats(context.Background(), st), wait, Request{}, &slowDBHost{pause: 60 * time.Millisecond}); err != nil {
		t.Fatal(err)
	}
	if st.HostDB < 240*time.Millisecond || st.CPU > 100*time.Millisecond || st.Wall < st.HostDB {
		t.Fatalf("waiting run: %+v", st)
	}
	t.Logf("database wait: wall %s, worker CPU %s, host DB %s", st.Wall, st.CPU, st.HostDB)
}

func TestPoolStatsForLimitsAndQueueing(t *testing.T) {
	p, err := NewPool(PoolConfig{Workers: 1, Timeout: 200 * time.Millisecond, HeapLimit: 16 << 20})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	loop := `function handle(req) { while (true) {} }`
	st := &RunStats{}
	if _, err := p.Run(WithStats(context.Background(), st), loop, Request{}, &fakeHost{}); err == nil || st.Stopped != "time" {
		t.Fatalf("timeout: err %v stats %+v", err, st)
	}
	// While one run holds the only worker, a second one queues.
	done := make(chan struct{})
	go func() {
		defer close(done)
		p.Run(context.Background(), loop, Request{}, &fakeHost{})
	}()
	time.Sleep(50 * time.Millisecond)
	var queued atomic.Int32
	st = &RunStats{Queued: func() { queued.Add(1) }}
	if _, err := p.Run(WithStats(context.Background(), st), `function handle() { return {text: "x"} }`, Request{}, &fakeHost{}); err != nil {
		t.Fatal(err)
	}
	<-done
	if queued.Load() != 1 || st.QueueWait < 50*time.Millisecond {
		t.Fatalf("queueing: queued %d stats %+v", queued.Load(), st)
	}
	if busy, total := p.Busy(); busy != 0 || total != 1 {
		t.Fatalf("busy %d of %d", busy, total)
	}
}

func TestGojaStatsWallOnly(t *testing.T) {
	g := NewGoja()
	g.Timeout = 100 * time.Millisecond
	st := &RunStats{}
	g.Run(WithStats(context.Background(), st), `function handle() { while (true) {} }`, Request{}, &fakeHost{})
	if st.CPUKnown || st.Wall < 100*time.Millisecond || st.Stopped != "time" {
		t.Fatalf("%+v", st)
	}
}
