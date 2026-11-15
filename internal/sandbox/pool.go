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
	// Command is the worker executable; default os.Executable(). The binary
	// must call ServeWorkerIfRequested at the start of main.
	Command string
}

// Pool is a Sandbox that runs scripts in separate worker processes, so a
// script that exhausts memory or crashes takes down only its worker.
type Pool struct {
	cfg  PoolConfig
	idle chan *worker
	mu   sync.Mutex
	all  map[*worker]bool
	done bool
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
	cmd := exec.Command(p.cfg.Command)
	cmd.Env = []string{
		workerEnv + "=1",
		"RENOXIUM_SANDBOX_HEAP_LIMIT=" + strconv.FormatInt(p.cfg.HeapLimit, 10),
		"RENOXIUM_SANDBOX_TIMEOUT_MS=" + strconv.FormatInt(p.cfg.Timeout.Milliseconds(), 10),
		"GOMEMLIMIT=" + strconv.FormatInt(p.cfg.HeapLimit*2, 10),
		"GOMAXPROCS=1",
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

func (p *Pool) acquire(ctx context.Context) (*worker, error) {
	select {
	case w := <-p.idle:
		return w, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("all script workers are busy: %w", ctx.Err())
	}
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
	// host calls from the worker
	Op    string `json:"op,omitempty"`
	SQL   string `json:"sql,omitempty"`
	Args  []any  `json:"args,omitempty"`
	Level string `json:"level,omitempty"`
	Text  string `json:"text,omitempty"`
	// host results and final results
	Rows         []map[string]any `json:"rows,omitempty"`
	RowsAffected int64            `json:"rows_affected,omitempty"`
	LastInsertID int64            `json:"last_insert_id,omitempty"`
	Error        string           `json:"error,omitempty"`
	ScriptError  string           `json:"script_error,omitempty"`
	Response     *wireResponse    `json:"response,omitempty"`
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

const maxLine = 64 << 20

func send(w io.Writer, m message) error {
	b, err := json.Marshal(m)
	if err != nil {
		return err
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
func (p *Pool) Check(src string) error {
	_, err := p.exchange(context.Background(), message{Type: "check", Src: src}, nil)
	return err
}

// Run implements Sandbox.
func (p *Pool) Run(ctx context.Context, src string, req Request, host Host) (Response, error) {
	m, err := p.exchange(ctx, message{Type: "run", Src: src, Req: &req}, host)
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
	w, err := p.acquire(ctx)
	if err != nil {
		return message{}, err
	}
	if w.cmd.ProcessState != nil || !alive(w.pid) {
		p.release(w, false)
		return message{}, errStale
	}
	healthy := false
	defer func() { p.release(w, healthy) }()

	// The parent enforces the limits a misbehaving worker cannot be trusted
	// to enforce on itself: a hard deadline and resident memory.
	var reason sync.Once
	var killedFor string
	killFor := func(why string) {
		reason.Do(func() { killedFor = why; w.cmd.Process.Kill() })
	}
	deadline := time.AfterFunc(p.cfg.Timeout+time.Second, func() { killFor("time limit exceeded") })
	defer deadline.Stop()
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
		if killedFor != "" {
			return message{}, &ScriptError{Message: "script stopped: " + killedFor}
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
			if host == nil {
				reply.Error = "no database here"
			} else if rows, err := host.Query(ctx, m.SQL, m.Args); err != nil {
				reply.Error = err.Error()
			} else {
				reply.Rows = rows
			}
			if err := send(w.in, reply); err != nil {
				return fail(err)
			}
		case "exec":
			reply := message{Type: "result"}
			if host == nil {
				reply.Error = "no database here"
			} else if n, id, err := host.Exec(ctx, m.SQL, m.Args); err != nil {
				reply.Error = err.Error()
			} else {
				reply.RowsAffected, reply.LastInsertID = n, id
			}
			if err := send(w.in, reply); err != nil {
				return fail(err)
			}
		case "log":
			if host != nil {
				host.Log(m.Level, m.Text)
			}
		case "done":
			if killedFor != "" {
				return fail(nil)
			}
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
