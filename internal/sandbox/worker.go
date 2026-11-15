package sandbox

import (
	"bufio"
	"context"
	"errors"
	"os"
	"runtime/debug"
	"runtime/metrics"
	"strconv"
	"time"
)

// ServeWorkerIfRequested turns this process into a sandbox worker when it
// was started by a Pool, and never returns in that case. Call it first
// thing in main (and in TestMain of tests that use a Pool).
func ServeWorkerIfRequested() {
	if os.Getenv(workerEnv) != "1" {
		return
	}
	heapLimit, _ := strconv.ParseInt(os.Getenv("RENOXIUM_SANDBOX_HEAP_LIMIT"), 10, 64)
	timeoutMS, _ := strconv.ParseInt(os.Getenv("RENOXIUM_SANDBOX_TIMEOUT_MS"), 10, 64)
	if heapLimit <= 0 {
		heapLimit = 64 << 20
	}
	if timeoutMS <= 0 {
		timeoutMS = 2000
	}
	os.Exit(serveWorker(os.Stdin, os.Stdout, heapLimit, time.Duration(timeoutMS)*time.Millisecond))
}

func serveWorker(stdin *os.File, stdout *os.File, heapLimit int64, timeout time.Duration) int {
	in := bufio.NewReaderSize(stdin, 64<<10)
	out := bufio.NewWriterSize(stdout, 64<<10)
	g := NewGoja()
	g.Timeout = timeout
	g.Watch = heapWatch(heapLimit)
	for {
		m, err := receive(in)
		if err != nil {
			return 0 // parent closed the pipe
		}
		var reply message
		switch m.Type {
		case "check":
			reply = finish(nil, g.Check(m.Src))
		case "run":
			resp, err := g.Run(context.Background(), m.Src, *m.Req, &proxyHost{in: in, out: out})
			reply = finish(&resp, err)
		default:
			reply = message{Type: "done", Error: "unknown request " + m.Type}
		}
		if send(out, reply) != nil || out.Flush() != nil {
			return 1
		}
		// Give memory from a large script back before the next one.
		if heapBytes() > heapLimit/2 {
			debug.FreeOSMemory()
		}
	}
}

func finish(resp *Response, err error) message {
	m := message{Type: "done"}
	var se *ScriptError
	switch {
	case errors.As(err, &se):
		m.ScriptError = se.Message
	case err != nil:
		m.Error = err.Error()
	case resp != nil:
		m.Response = &wireResponse{Status: resp.Status, Template: resp.Template, Data: resp.Data,
			JSON: resp.JSON, HasJSON: resp.HasJSON, Text: resp.Text, Redirect: resp.Redirect}
	}
	return m
}

// heapWatch stops a script whose live heap grows past limit.
func heapWatch(limit int64) func(func(string)) func() {
	return func(interrupt func(string)) func() {
		done := make(chan struct{})
		go func() {
			t := time.NewTicker(2 * time.Millisecond)
			defer t.Stop()
			for {
				select {
				case <-done:
					return
				case <-t.C:
					if heapBytes() > limit {
						interrupt("memory limit exceeded")
						return
					}
				}
			}
		}()
		return func() { close(done) }
	}
}

var heapSample = []metrics.Sample{{Name: "/memory/classes/heap/objects:bytes"}}

func heapBytes() int64 {
	s := make([]metrics.Sample, 1)
	copy(s, heapSample)
	metrics.Read(s)
	return int64(s[0].Value.Uint64())
}

// proxyHost forwards host calls to the parent and waits for the answer.
type proxyHost struct {
	in  *bufio.Reader
	out *bufio.Writer
}

func (h *proxyHost) call(m message) (message, error) {
	if err := send(h.out, m); err != nil {
		return message{}, err
	}
	if err := h.out.Flush(); err != nil {
		return message{}, err
	}
	r, err := receive(h.in)
	if err != nil {
		return message{}, err
	}
	if r.Error != "" {
		return message{}, errors.New(r.Error)
	}
	return r, nil
}

func (h *proxyHost) Query(_ context.Context, sql string, args []any) ([]map[string]any, error) {
	r, err := h.call(message{Type: "query", SQL: sql, Args: args})
	if err != nil {
		return nil, err
	}
	if r.Rows == nil {
		r.Rows = []map[string]any{}
	}
	return r.Rows, nil
}

func (h *proxyHost) Exec(_ context.Context, sql string, args []any) (int64, int64, error) {
	r, err := h.call(message{Type: "exec", SQL: sql, Args: args})
	return r.RowsAffected, r.LastInsertID, err
}

func (h *proxyHost) Log(level, text string) {
	send(h.out, message{Type: "log", Level: level, Text: text})
}
