package sandbox

import (
	"sync"
	"time"
)

// SlowFunctions are host calls that wait on the network (http.fetch). Time
// spent in them does not count towards the script's time limit; the
// platform limits it separately.
var SlowFunctions = map[string]bool{"http.fetch": true}

// budget is a time limit that can be paused while a slow host call runs.
type budget struct {
	mu        sync.Mutex
	remaining time.Duration
	started   time.Time
	timer     *time.Timer
	running   bool
}

func newBudget(d time.Duration, fire func()) *budget {
	return &budget{remaining: d, started: time.Now(), timer: time.AfterFunc(d, fire), running: true}
}

func (b *budget) pause() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.running && b.timer.Stop() {
		b.remaining -= time.Since(b.started)
		b.running = false
	}
}

func (b *budget) resume() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.running {
		b.started, b.running = time.Now(), true
		b.timer.Reset(max(b.remaining, time.Millisecond))
	}
}

func (b *budget) stop() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.timer.Stop()
	b.running = false
}
