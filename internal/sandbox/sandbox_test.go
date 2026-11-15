package sandbox

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type fakeHost struct{ logs []string }

func (f *fakeHost) Query(ctx context.Context, sql string, args []any) ([]map[string]any, error) {
	return []map[string]any{{"sql": sql, "n": len(args)}}, nil
}
func (f *fakeHost) Exec(ctx context.Context, sql string, args []any) (int64, int64, error) {
	return 1, 7, nil
}
func (f *fakeHost) Log(level, message string) { f.logs = append(f.logs, level+": "+message) }

func run(t *testing.T, src string) (Response, error, *fakeHost) {
	t.Helper()
	g := NewGoja()
	g.Timeout = 300 * time.Millisecond
	h := &fakeHost{}
	r, err := g.Run(context.Background(), src, Request{Method: "GET", Path: "/", Params: map[string]string{"id": "5"}}, h)
	return r, err, h
}

func TestTemplateResponse(t *testing.T) {
	r, err, h := run(t, `function handle(req) {
		const rows = db.query("SELECT 1 WHERE ? = ?", req.params.id, 5);
		const res = db.exec("INSERT INTO t VALUES (?)", 1);
		console.log("id", req.params.id);
		return {template: "page", data: {rows, id: res.lastInsertId}, status: 201};
	}`)
	if err != nil {
		t.Fatal(err)
	}
	if r.Template != "page" || r.Status != 201 || len(h.logs) != 1 || h.logs[0] != "info: id 5" {
		t.Fatalf("response = %+v logs = %v", r, h.logs)
	}
}

func TestJSONResponse(t *testing.T) {
	r, err, _ := run(t, `function handle(req) { return {json: {ok: true}} }`)
	if err != nil || !r.HasJSON {
		t.Fatalf("response = %+v err = %v", r, err)
	}
}

func TestInfiniteLoopIsStopped(t *testing.T) {
	start := time.Now()
	_, err, _ := run(t, `function handle(req) { while (true) {} }`)
	var se *ScriptError
	if !errors.As(err, &se) || !strings.Contains(se.Message, "time limit") {
		t.Fatalf("err = %v", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("loop was not stopped promptly")
	}
}

func TestDeepRecursionIsStopped(t *testing.T) {
	_, err, _ := run(t, `function f(n) { return f(n + 1) } function handle(req) { return f(0) }`)
	if err == nil {
		t.Fatal("expected stack overflow error")
	}
}

func TestErrorsCarryAStack(t *testing.T) {
	_, err, _ := run(t, "function handle(req) {\n  const x = null;\n  return x.boom;\n}")
	if err == nil || !strings.Contains(err.Error(), "script:3") {
		t.Fatalf("err = %v", err)
	}
}

func TestNoAmbientAuthority(t *testing.T) {
	for _, src := range []string{
		`function handle(req) { return {json: typeof require} }`,
		`function handle(req) { return {json: typeof fetch} }`,
		`function handle(req) { return {json: typeof setTimeout} }`,
	} {
		r, err, _ := run(t, src)
		if err != nil || r.JSON != "undefined" {
			t.Errorf("%s: got %v %v", src, r.JSON, err)
		}
	}
}

func TestCheck(t *testing.T) {
	g := NewGoja()
	if err := g.Check(`function handle(req) { return {text: "hi"} }`); err != nil {
		t.Fatal(err)
	}
	if err := g.Check(`const x = 1;`); err == nil {
		t.Fatal("missing handle accepted")
	}
	if err := g.Check(`function handle( {`); err == nil {
		t.Fatal("syntax error accepted")
	}
}
