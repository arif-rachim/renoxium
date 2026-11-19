package sandbox

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// workerEnv marks a process started as a sandbox worker.
const workerEnv = "RENOXIUM_SANDBOX_WORKER"

// PoolConfig configures a worker pool.
type PoolConfig struct {
	Workers int           // number of worker processes; default 4
	Timeout time.Duration // per script run; default 2s
	// HeapLimit is the Go heap size at which a worker stops the running
	// script (default 64 MiB). RSSLimit is the resident memory at which the
	// parent kills the worker outright (default 256 MiB). Both are per worker.
	HeapLimit int64
	RSSLimit  int64
	// RequireSandbox makes a worker exit instead of running scripts when
	// the kernel sandbox (Landlock) cannot be applied.
	RequireSandbox bool
	// Command is the worker executable; default os.Executable(). The binary
	// must call ServeWorkerIfRequested at the start of main.
	Command string
}

// Pool is a Sandbox that runs scripts in separate worker processes, so a
// script that exhausts memory or crashes takes down only its worker.
type Pool struct {
	cfg     PoolConfig
	idle    chan *worker
	mu      sync.Mutex
	all     map[*worker]bool
	done    bool
	spawned int
}

// NewPool starts the worker processes.
func NewPool(cfg PoolConfig) (*Pool, error) {
	if cfg.Workers <= 0 {
		cfg.Workers = 4
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 2 * time.Second
	}
	if cfg.HeapLimit <= 0 {
		cfg.HeapLimit = 64 << 20
	}
	if cfg.RSSLimit <= 0 {
		cfg.RSSLimit = 256 << 20
	}
	if cfg.Command == "" {
		exe, err := os.Executable()
		if err != nil {
			return nil, err
		}
		cfg.Command = exe
	}
	p := &Pool{cfg: cfg, idle: make(chan *worker, cfg.Workers), all: map[*worker]bool{}}
	for i := 0; i < cfg.Workers; i++ {
		w, err := p.spawn()
		if err != nil {
			p.Close()
			return nil, err
		}
		p.idle <- w
	}
	return p, nil
}

// Close stops every worker.
func (p *Pool) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.done = true
	for w := range p.all {
		w.kill()
	}
}

type worker struct {
	cmd *exec.Cmd
	in  io.WriteCloser
	out *bufio.Reader
	pid int
}

func (w *worker) kill() {
	w.cmd.Process.Kill()
	w.cmd.Wait()
}

func (p *Pool) spawn() (*worker, error) {
	p.mu.Lock()
	index := p.spawned
	p.spawned++
	p.mu.Unlock()
	cmd := exec.Command(p.cfg.Command)
	cmd.Env = []string{
		workerEnv + "=1",
		"RENOXIUM_SANDBOX_HEAP_LIMIT=" + strconv.FormatInt(p.cfg.HeapLimit, 10),
		"RENOXIUM_SANDBOX_TIMEOUT_MS=" + strconv.FormatInt(p.cfg.Timeout.Milliseconds(), 10),
		"GOMEMLIMIT=" + strconv.FormatInt(p.cfg.HeapLimit*2, 10),
		"GOMAXPROCS=1",
		"RENOXIUM_SANDBOX_WORKER_INDEX=" + strconv.Itoa(index),
	}
	if p.cfg.RequireSandbox {
		cmd.Env = append(cmd.Env, "RENOXIUM_SANDBOX_REQUIRE=1")
	}
	cmd.Dir = os.TempDir()
	cmd.Stderr = os.Stderr
	setProcAttr(cmd)
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start sandbox worker: %w", err)
	}
	w := &worker{cmd: cmd, in: in, out: bufio.NewReaderSize(out, 64<<10), pid: cmd.Process.Pid}
	p.mu.Lock()
	p.all[w] = true
	p.mu.Unlock()
	return w, nil
}

func (p *Pool) acquire(ctx context.Context, st *RunStats) (*worker, error) {
	select {
	case w := <-p.idle:
		return w, nil
	default:
	}
	if st != nil && st.Queued != nil {
		st.Queued()
	}
	start := time.Now()
	select {
	case w := <-p.idle:
		if st != nil {
			st.QueueWait += time.Since(start)
		}
		return w, nil
	case <-ctx.Done():
		if st != nil {
			st.QueueWait += time.Since(start)
		}
		return nil, fmt.Errorf("all script workers are busy: %w", ctx.Err())
	}
}

// Busy reports how many workers are running a script now, of how many.
func (p *Pool) Busy() (busy, total int) {
	return max(p.cfg.Workers-len(p.idle), 0), p.cfg.Workers
}

// release returns a healthy worker, or replaces a dead one.
func (p *Pool) release(w *worker, healthy bool) {
	if healthy {
		p.idle <- w
		return
	}
	w.kill()
	p.mu.Lock()
	delete(p.all, w)
	done := p.done
	p.mu.Unlock()
	if done {
		return
	}
	for {
		nw, err := p.spawn()
		if err == nil {
			p.idle <- nw
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// message is the line-delimited JSON protocol between parent and worker.
type message struct {
	Type string `json:"type"`
	// run / check
	Src string   `json:"src,omitempty"`
	Req *Request `json:"req,omitempty"`
	Lib []Lib    `json:"lib,omitempty"`
	// host calls from the worker
	Op    string `json:"op,omitempty"`
	SQL   string `json:"sql,omitempty"`
	Args  []any  `json:"args,omitempty"`
	Level string `json:"level,omitempty"`
	Text  string `json:"text,omitempty"`
	// platform function calls (Host.Call) and their results
	Fn    string `json:"fn,omitempty"`
	Value any    `json:"value,omitempty"`
	// host results and final results
	Rows         []map[string]any `json:"rows,omitempty"`
	RowsAffected int64            `json:"rows_affected,omitempty"`
	LastInsertID int64            `json:"last_insert_id,omitempty"`
	Error        string           `json:"error,omitempty"`
	ScriptError  string           `json:"script_error,omitempty"`
	Response     *wireResponse    `json:"response,omitempty"`
	// What a run cost, measured by the worker (see RunStats).
	CPUUS    int64 `json:"cpu_us,omitempty"`
	CPUOK    bool  `json:"cpu_ok,omitempty"`
	HeapPeak int64 `json:"heap_peak,omitempty"`
}

type wireResponse struct {
	Status   int    `json:"status,omitempty"`
	Template string `json:"template,omitempty"`
	Data     any    `json:"data,omitempty"`
	JSON     any    `json:"json,omitempty"`
	HasJSON  bool   `json:"has_json,omitempty"`
	Text     string `json:"text,omitempty"`
	Redirect string `json:"redirect,omitempty"`
}

// maxLine caps one protocol message, such as a script's response or a
// query result, so a worker cannot make the parent parse huge JSON.
const maxLine = 8 << 20

var errTooLarge = errors.New("message too large")

func send(w io.Writer, m message) error {
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	if len(b) > maxLine {
		return errTooLarge
	}
	_, err = w.Write(append(b, '\n'))
	return err
}

func receive(r *bufio.Reader) (message, error) {
	var line []byte
	for {
		chunk, isPrefix, err := r.ReadLine()
		if err != nil {
			return message{}, err
		}
		line = append(line, chunk...)
		if len(line) > maxLine {
			return message{}, errors.New("message too large")
		}
		if !isPrefix {
			break
		}
	}
	dec := json.NewDecoder(strings.NewReader(string(line)))
	dec.UseNumber()
	var m message
	if err := dec.Decode(&m); err != nil {
		return message{}, err
	}
	m.Args = normalizeSlice(m.Args)
	m.Value = normalize(m.Value)
	for i, row := range m.Rows {
		m.Rows[i] = normalize(row).(map[string]any)
	}
	if m.Response != nil {
		m.Response.Data = normalize(m.Response.Data)
		m.Response.JSON = normalize(m.Response.JSON)
	}
	return m, nil
}

// normalize turns json.Number into int64 when integral, else float64, so
// values behave the same as they did inside the script.
func normalize(v any) any {
	switch x := v.(type) {
	case json.Number:
		if i, err := x.Int64(); err == nil {
			return i
		}
		f, _ := x.Float64()
		return f
	case map[string]any:
		for k, e := range x {
			x[k] = normalize(e)
		}
		return x
	case []any:
		return normalizeSlice(x)
	}
	return v
}

func normalizeSlice(s []any) []any {
	for i, e := range s {
		s[i] = normalize(e)
	}
	return s
}

// Check implements Sandbox. Top-level script code runs during the check, so
// it runs in a worker too.
func (p *Pool) Check(src string) error { return p.CheckLib(nil, src) }

// CheckLib implements Sandbox.
func (p *Pool) CheckLib(lib []Lib, src string) error {
	_, err := p.exchange(context.Background(), message{Type: "check", Src: src, Lib: lib}, nil)
	return err
}

// Run implements Sandbox.
func (p *Pool) Run(ctx context.Context, src string, req Request, host Host) (Response, error) {
	m, err := p.exchange(ctx, message{Type: "run", Src: src, Req: &req, Lib: libOf(host)}, host)
	if err != nil {
		return Response{}, err
	}
	r := m.Response
	if r == nil {
		return Response{}, errors.New("sandbox worker returned no response")
	}
	return Response{Status: r.Status, Template: r.Template, Data: r.Data, JSON: r.JSON, HasJSON: r.HasJSON, Text: r.Text, Redirect: r.Redirect}, nil
}

// errStale means the worker was already dead before it got the request.
var errStale = errors.New("stale worker")

func (p *Pool) exchange(ctx context.Context, msg message, host Host) (message, error) {
	m, err := p.exchangeOnce(ctx, msg, host)
	// A worker that died while idle never saw the request: retry on another.
	// Each stale worker is replaced, so this ends within Workers+1 tries.
	for i := 0; i < p.cfg.Workers && errors.Is(err, errStale); i++ {
		m, err = p.exchangeOnce(ctx, msg, host)
	}
	if errors.Is(err, errStale) {
		err = errors.New("sandbox worker failed: no live worker")
	}
	return m, err
}

func (p *Pool) exchangeOnce(ctx context.Context, msg message, host Host) (message, error) {
	var stats *RunStats
	if msg.Type == "run" {
		stats = statsFrom(ctx)
	}
	w, err := p.acquire(ctx, stats)
	if err != nil {
		return message{}, err
	}
	if w.cmd.ProcessState != nil || !alive(w.pid) {
		p.release(w, false)
		return message{}, errStale
	}
	healthy := false
	defer func() { p.release(w, healthy) }()
	var final *message // the worker's "done" answer, for stats
	if stats != nil {
		if stats.Started != nil {
			stats.Started()
		}
		began := time.Now()
		defer func() {
			stats.Wall = time.Since(began)
			if final != nil && final.CPUOK {
				// The worker measures itself; a broken worker could lie, so
				// its figure never exceeds what the run could have used.
				stats.CPU = min(time.Duration(final.CPUUS)*time.Microsecond, stats.Wall)
				stats.CPUKnown = true
			}
			if final != nil {
				stats.HeapPeak = final.HeapPeak
				stats.Stopped = stopReason(final.ScriptError)
			}
		}()
	}

	// The parent enforces the limits a misbehaving worker cannot be trusted
	// to enforce on itself: a hard deadline and resident memory.
	var killMu sync.Mutex
	var killed string
	killFor := func(why string) {
		killMu.Lock()
		defer killMu.Unlock()
		if killed == "" {
			killed = why
			w.cmd.Process.Kill()
		}
	}
	killedFor := func() string {
		killMu.Lock()
		defer killMu.Unlock()
		return killed
	}
	deadline := newBudget(p.cfg.Timeout+time.Second, func() { killFor("time limit exceeded") })
	defer deadline.stop()
	stopWatch := make(chan struct{})
	defer close(stopWatch)
	go func() {
		t := time.NewTicker(20 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-stopWatch:
				return
			case <-t.C:
				if rss := residentBytes(w.pid); rss > p.cfg.RSSLimit {
					killFor("memory limit exceeded")
					return
				}
			}
		}
	}()

	fail := func(err error) (message, error) {
		if why := killedFor(); why != "" {
			if stats != nil {
				stats.Restarted, stats.Stopped = true, stopReason(why)
			}
			return message{}, &ScriptError{Message: "script stopped: " + why}
		}
		return message{}, fmt.Errorf("sandbox worker failed: %w", err)
	}
	if err := send(w.in, msg); err != nil {
		return message{}, errStale
	}
	for {
		m, err := receive(w.out)
		if err != nil {
			return fail(err)
		}
		switch m.Type {
		case "query":
			reply := message{Type: "result"}
			t0 := time.Now()
			if host == nil {
				reply.Error = "no database here"
			} else if rows, err := host.Query(ctx, m.SQL, m.Args); err != nil {
				reply.Error = err.Error()
			} else {
				reply.Rows = rows
			}
			if stats != nil {
				stats.HostDB += time.Since(t0)
			}
			err := send(w.in, reply)
			if errors.Is(err, errTooLarge) {
				err = send(w.in, message{Type: "result", Error: fmt.Sprintf("query result is larger than %d MB; select fewer rows or columns", maxLine>>20)})
			}
			if err != nil {
				return fail(err)
			}
		case "exec":
			reply := message{Type: "result"}
			t0 := time.Now()
			if host == nil {
				reply.Error = "no database here"
			} else if n, id, err := host.Exec(ctx, m.SQL, m.Args); err != nil {
				reply.Error = err.Error()
			} else {
				reply.RowsAffected, reply.LastInsertID = n, id
			}
			if stats != nil {
				stats.HostDB += time.Since(t0)
			}
			if err := send(w.in, reply); err != nil {
				return fail(err)
			}
		case "log":
			if host != nil {
				host.Log(m.Level, m.Text)
			}
		case "call":
			reply := message{Type: "result"}
			if SlowFunctions[m.Fn] {
				deadline.pause()
			}
			if host == nil {
				reply.Error = "no platform here"
			} else if v, err := host.Call(ctx, m.Fn, m.Value); err != nil {
				reply.Error = err.Error()
			} else {
				reply.Value = v
			}
			deadline.resume()
			if err := send(w.in, reply); err != nil {
				return fail(err)
			}
		case "done":
			if killedFor() != "" {
				return fail(nil)
			}
			final = &m
			healthy = true
			if m.ScriptError != "" {
				return message{}, &ScriptError{Message: m.ScriptError}
			}
			if m.Error != "" {
				return message{}, errors.New(m.Error)
			}
			return m, nil
		default:
			return fail(fmt.Errorf("unexpected message %q", m.Type))
		}
	}
}

// alive reports whether a process exists and is not a zombie.
func alive(pid int) bool {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return !os.IsNotExist(err) // non-Linux: assume alive
	}
	// Field 3 is the state; it follows the ")" that ends the command name.
	s := string(b)
	i := strings.LastIndexByte(s, ')')
	return i < 0 || i+2 >= len(s) || (s[i+2] != 'Z' && s[i+2] != 'X')
}

// residentBytes reads a process's resident memory on Linux; 0 elsewhere.
func residentBytes(pid int) int64 {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/statm")
	if err != nil {
		return 0
	}
	f := strings.Fields(string(b))
	if len(f) < 2 {
		return 0
	}
	pages, _ := strconv.ParseInt(f[1], 10, 64)
	return pages * int64(os.Getpagesize())
}
