// Package sandbox runs tenant server scripts. Scripts get no filesystem and
// no network; they see only the request and the host API passed in.
package sandbox

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/dop251/goja"
)

// Host is everything a script can reach outside itself. Implementations are
// bound to one app stage.
type Host interface {
	Query(ctx context.Context, sql string, args []any) ([]map[string]any, error)
	Exec(ctx context.Context, sql string, args []any) (rowsAffected, lastInsertID int64, err error)
	Log(level, message string)
}

// Request is what a script's handle(req) receives.
type Request struct {
	Method string            `json:"method"`
	Path   string            `json:"path"`
	Params map[string]string `json:"params"`
	Query  map[string]string `json:"query"`
	Form   map[string]string `json:"form"`
	Body   any               `json:"body"`
}

// Response is what a script returns. Exactly one of Template, JSON, Text or
// Redirect is used, in that order of precedence.
type Response struct {
	Status   int
	Template string
	Data     any
	JSON     any
	HasJSON  bool
	Text     string
	Redirect string
}

// Sandbox compiles and runs scripts.
type Sandbox interface {
	// Check reports whether src compiles and defines handle.
	Check(src string) error
	// Run calls handle(req) in src.
	Run(ctx context.Context, src string, req Request, host Host) (Response, error)
}

// ScriptError is a failure inside tenant code (as opposed to a platform fault).
type ScriptError struct{ Message string }

func (e *ScriptError) Error() string { return e.Message }

// Goja is a Sandbox backed by the goja interpreter, in-process.
type Goja struct {
	// Timeout is the wall-clock limit for one handle call.
	Timeout time.Duration
	// Watch, if set, is started for every script run with a function that
	// stops the script; it returns a function that ends the watch. Workers use
	// it to stop scripts that use too much memory.
	Watch func(interrupt func(reason string)) (stop func())

	mu       sync.Mutex
	programs map[[32]byte]*goja.Program
}

// NewGoja returns a sandbox with a 2 second timeout.
func NewGoja() *Goja {
	return &Goja{Timeout: 2 * time.Second, programs: map[[32]byte]*goja.Program{}}
}

func (g *Goja) compile(src string) (*goja.Program, error) {
	key := sha256.Sum256([]byte(src))
	g.mu.Lock()
	p := g.programs[key]
	g.mu.Unlock()
	if p != nil {
		return p, nil
	}
	p, err := goja.Compile("script", src, true)
	if err != nil {
		return nil, &ScriptError{Message: err.Error()}
	}
	g.mu.Lock()
	if len(g.programs) > 4096 {
		g.programs = map[[32]byte]*goja.Program{}
	}
	g.programs[key] = p
	g.mu.Unlock()
	return p, nil
}

// Check implements Sandbox.
func (g *Goja) Check(src string) error {
	p, err := g.compile(src)
	if err != nil {
		return err
	}
	vm := goja.New()
	stop := time.AfterFunc(g.Timeout, func() { vm.Interrupt("time limit exceeded") })
	defer stop.Stop()
	if g.Watch != nil {
		defer g.Watch(func(reason string) { vm.Interrupt(reason) })()
	}
	if _, err := vm.RunProgram(p); err != nil {
		return &ScriptError{Message: describe(err)}
	}
	if _, ok := goja.AssertFunction(vm.Get("handle")); !ok {
		return &ScriptError{Message: "script must define function handle(req)"}
	}
	return nil
}

// Run implements Sandbox.
func (g *Goja) Run(ctx context.Context, src string, req Request, host Host) (Response, error) {
	p, err := g.compile(src)
	if err != nil {
		return Response{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, g.Timeout)
	defer cancel()

	vm := goja.New()
	vm.SetMaxCallStackSize(256)
	stop := context.AfterFunc(ctx, func() { vm.Interrupt("time limit exceeded") })
	defer stop()
	if g.Watch != nil {
		defer g.Watch(func(reason string) { vm.Interrupt(reason) })()
	}

	throw := func(err error) { panic(vm.NewGoError(err)) }
	db := vm.NewObject()
	db.Set("query", func(call goja.FunctionCall) goja.Value {
		rows, err := host.Query(ctx, call.Argument(0).String(), exportArgs(call))
		if err != nil {
			throw(err)
		}
		return vm.ToValue(rows)
	})
	db.Set("exec", func(call goja.FunctionCall) goja.Value {
		affected, lastID, err := host.Exec(ctx, call.Argument(0).String(), exportArgs(call))
		if err != nil {
			throw(err)
		}
		return vm.ToValue(map[string]any{"rowsAffected": affected, "lastInsertId": lastID})
	})
	vm.Set("db", db)

	console := vm.NewObject()
	for _, level := range []string{"log", "info", "warn", "error"} {
		lvl := level
		if lvl == "log" {
			lvl = "info"
		}
		console.Set(level, func(call goja.FunctionCall) goja.Value {
			parts := make([]string, len(call.Arguments))
			for i, a := range call.Arguments {
				parts[i] = a.String()
			}
			host.Log(lvl, strings.Join(parts, " "))
			return goja.Undefined()
		})
	}
	vm.Set("console", console)

	if _, err := vm.RunProgram(p); err != nil {
		return Response{}, &ScriptError{Message: describe(err)}
	}
	handle, ok := goja.AssertFunction(vm.Get("handle"))
	if !ok {
		return Response{}, &ScriptError{Message: "script must define function handle(req)"}
	}
	val, err := handle(goja.Undefined(), vm.ToValue(map[string]any{
		"method": req.Method, "path": req.Path, "params": strMap(req.Params),
		"query": strMap(req.Query), "form": strMap(req.Form), "body": req.Body,
	}))
	if err != nil {
		return Response{}, &ScriptError{Message: describe(err)}
	}
	return toResponse(val)
}

func exportArgs(call goja.FunctionCall) []any {
	if len(call.Arguments) < 2 {
		return nil
	}
	args := make([]any, 0, len(call.Arguments)-1)
	for _, a := range call.Arguments[1:] {
		args = append(args, a.Export())
	}
	return args
}

func strMap(m map[string]string) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func toResponse(val goja.Value) (Response, error) {
	if val == nil || goja.IsUndefined(val) || goja.IsNull(val) {
		return Response{}, &ScriptError{Message: "handle(req) returned nothing; return {template, data}, {json}, {text} or {redirect}"}
	}
	m, ok := val.Export().(map[string]any)
	if !ok {
		return Response{}, &ScriptError{Message: "handle(req) must return an object: {template, data}, {json}, {text} or {redirect}"}
	}
	var r Response
	switch s := m["status"].(type) {
	case int64:
		r.Status = int(s)
	case float64:
		r.Status = int(s)
	}
	r.Template, _ = m["template"].(string)
	r.Data = m["data"]
	r.JSON, r.HasJSON = m["json"]
	r.Text, _ = m["text"].(string)
	r.Redirect, _ = m["redirect"].(string)
	_, hasText := m["text"]
	if r.Template == "" && !r.HasJSON && !hasText && r.Redirect == "" {
		return Response{}, &ScriptError{Message: "handle(req) returned an object without template, json, text or redirect"}
	}
	return r, nil
}

// describe turns a goja error into a message with a script stack trace.
func describe(err error) string {
	var ex *goja.Exception
	if errors.As(err, &ex) {
		return ex.String()
	}
	var in *goja.InterruptedError
	if errors.As(err, &in) {
		return fmt.Sprintf("script stopped: %v", in.Value())
	}
	return err.Error()
}
