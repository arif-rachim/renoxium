package sandbox

import (
	"context"
	"strings"
	"time"
)

// RunStats is what one Run cost. A caller that wants it puts a RunStats in
// the context with WithStats; Run fills it in, whether the script succeeded
// or not. The platform's resource meter reads it.
type RunStats struct {
	// Queued is called when no worker is free and the run has to wait for
	// one; Started when the run got a worker (or, in process, when it
	// starts). Either may be nil. They let the caller see who holds workers
	// while others wait.
	Queued  func()
	Started func()

	// QueueWait is the time spent waiting for a free worker.
	QueueWait time.Duration
	// Wall is the time the run held a worker, host calls included.
	Wall time.Duration
	// CPU is the worker process's CPU time (user + system) during the run,
	// measured inside the worker with getrusage. CPUKnown is false where
	// that is not available (the in-process sandbox, non-Linux systems, a
	// worker killed before it could answer); callers then use Wall.
	CPU      time.Duration
	CPUKnown bool
	// HostDB is the time spent in the parent answering db.query and
	// db.exec: SQLite runs in the server process, not in the worker.
	HostDB time.Duration
	// HeapPeak is the largest Go heap the worker saw during the run,
	// sampled every 2 ms (garbage not yet collected included).
	HeapPeak int64
	// Stopped is "time" or "memory" when the run was stopped for a limit.
	Stopped string
	// Restarted is true when the parent killed the worker (hard time or
	// resident memory limit) and started a new one.
	Restarted bool
}

type statsKey struct{}

// WithStats returns a context that makes Run fill in s.
func WithStats(ctx context.Context, s *RunStats) context.Context {
	return context.WithValue(ctx, statsKey{}, s)
}

func statsFrom(ctx context.Context) *RunStats {
	s, _ := ctx.Value(statsKey{}).(*RunStats)
	return s
}

// stopReason classifies a script error message as a limit stop.
func stopReason(msg string) string {
	switch {
	case strings.Contains(msg, "time limit exceeded"):
		return "time"
	case strings.Contains(msg, "memory limit exceeded"):
		return "memory"
	}
	return ""
}
