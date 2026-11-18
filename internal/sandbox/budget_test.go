package sandbox

import (
	"context"
	"strings"
	"testing"
	"time"
)

// slowHost waits in http.fetch, like a slow external API.
type slowHost struct {
	fakeHost
	wait time.Duration
}

func (s *slowHost) Call(ctx context.Context, fn string, arg any) (any, error) {
	if fn == "http.fetch" {
		time.Sleep(s.wait)
	}
	return map[string]any{"fn": fn}, nil
}

const waitsTwice = `function handle(req) { http.fetch({}); const r = http.fetch({}); return {text: r.fn} }`

func TestSlowCallsDoNotCountTowardsTheTimeLimit(t *testing.T) {
	// Worker pool: 1s limit in the worker, 2s in the parent; the calls wait 2.4s in all.
	p, err := NewPool(PoolConfig{Workers: 1, Timeout: time.Second, HeapLimit: 16 << 20})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	r, err := p.Run(context.Background(), waitsTwice, Request{Method: "GET", Path: "/"}, &slowHost{wait: 1200 * time.Millisecond})
	if err != nil || r.Text != "http.fetch" {
		t.Fatalf("pool: %+v %v", r, err)
	}
	// Script time still counts: a busy loop after a slow call is stopped.
	_, err = p.Run(context.Background(), `function handle(req) { http.fetch({}); while (true) {} }`, Request{Method: "GET", Path: "/"}, &slowHost{wait: 800 * time.Millisecond})
	if err == nil || !strings.Contains(err.Error(), "time limit") {
		t.Fatalf("busy loop after a slow call: %v", err)
	}

	g := NewGoja()
	g.Timeout = 300 * time.Millisecond
	r, err = g.Run(context.Background(), waitsTwice, Request{Method: "GET", Path: "/"}, &slowHost{wait: 400 * time.Millisecond})
	if err != nil || r.Text != "http.fetch" {
		t.Fatalf("in process: %+v %v", r, err)
	}
	// Other host calls still count.
	_, err = g.Run(context.Background(), `function handle(req) { notify.owner({}); return {text: 'x'} }`, Request{Method: "GET", Path: "/"},
		&slowNotify{wait: 600 * time.Millisecond})
	if err == nil {
		t.Fatal("a slow non-network call was not stopped")
	}
}

type slowNotify struct {
	fakeHost
	wait time.Duration
}

func (s *slowNotify) Call(ctx context.Context, fn string, arg any) (any, error) {
	select {
	case <-time.After(s.wait):
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return nil, nil
}
