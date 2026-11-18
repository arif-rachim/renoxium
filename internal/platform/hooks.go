package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/arif-rachim/renoxium/internal/appdef"
	"github.com/arif-rachim/renoxium/internal/applog"
	"github.com/arif-rachim/renoxium/internal/sandbox"
)

// ---- lib: shared script code

// libOf returns an app's libraries in evaluation (name) order.
func libOf(def *appdef.App) []sandbox.Lib {
	if def == nil || len(def.Lib) == 0 {
		return nil
	}
	out := make([]sandbox.Lib, 0, len(def.Lib))
	for _, n := range def.LibNames() {
		out = append(out, sandbox.Lib{Name: n, Src: def.Lib[n]})
	}
	return out
}

// Lib implements sandbox.LibHost: every script of the stage gets the app's
// libraries, whatever runs it (route, job, tool or hook).
func (h *scriptHost) Lib() []sandbox.Lib { return libOf(h.st.def) }

// checkScripts compiles the libraries and every script, with top-level code
// run, and reports problems by library or script name.
func (p *Platform) checkScripts(def *appdef.App) []string {
	var problems []string
	lib := libOf(def)
	if len(lib) > 0 {
		if err := p.sb.CheckLib(lib, ""); err != nil {
			problems = append(problems, err.Error())
			lib = nil // scripts are still checked on their own
		}
	}
	names := make([]string, 0, len(def.Scripts))
	for n := range def.Scripts {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if err := p.sb.CheckLib(lib, def.Scripts[n]); err != nil {
			problems = append(problems, fmt.Sprintf("script %q: %v", n, err))
		}
	}
	return problems
}

// ---- on_admin_change: a script run after a row is changed in the admin panel

// hookRun is one run of an app's on_admin_change script.
type hookRun struct {
	Script string
	Resp   sandbox.Response
	Err    error
}

// inAdminHook marks the context of a hook run, so changes it makes never
// run the hook again.
type inAdminHook struct{}

// hookRow reads a row for req.body.before/row; nil without a hook.
func (p *Platform) hookRow(ctx context.Context, st *stage, t appdef.Table, id int64) map[string]any {
	if st.def.OnAdminChange == "" || id == 0 {
		return nil
	}
	return p.rowOf(ctx, st, t, id)
}

// afterAdminChange runs on_admin_change synchronously after a saved admin
// change, with the normal script limits. A failure is logged with the
// request ID and kept as a notice for the admin panel; the change stays.
func (p *Platform) afterAdminChange(ctx context.Context, st *stage, t appdef.Table, action string, id int64, before map[string]any, by, rid string) *hookRun {
	script := st.def.OnAdminChange
	if script == "" || ctx.Value(inAdminHook{}) != nil {
		return nil
	}
	// The change is saved: finish the hook even if the owner's browser goes away.
	ctx = context.WithValue(context.WithoutCancel(ctx), inAdminHook{}, true)
	var row, prev any
	if action != "delete" {
		if r := p.hookRow(ctx, st, t, id); r != nil {
			row = r
		}
	}
	if before != nil {
		prev = before
	}
	run := &hookRun{Script: script}
	what := fmt.Sprintf("%s %s #%d", action, t.Name, id)
	fail := func(err error) *hookRun {
		run.Err = err
		p.event(st, "server", "error", rid, fmt.Sprintf("on_admin_change %s after %s by %s failed: %v (the change itself was saved)", script, what, by, err))
		if st.name == Live {
			p.adminNotices.Store(st.app+"\x00"+by, fmt.Sprintf("Your change to %s #%d was saved, but the app's follow-up script (%s) failed, so emails or updates it sends may be missing. The error is in the app log, request %s.", t.Name, id, script, rid))
		}
		return run
	}
	host := &scriptHost{st: st, p: p, rid: rid}
	defer host.runDeferred(ctx) // after release: on_payment from money calls needs a slot of its own
	release, ok := p.acquireSlot(ctx, st.app)
	if !ok {
		return fail(errors.New("too many requests running at once for this app; the script was not run"))
	}
	defer release()
	start := time.Now()
	req := sandbox.Request{Method: "ADMIN", Path: "/_rx/admin/" + t.Name, Origin: p.URL(st.app, st.name),
		Body:   map[string]any{"action": action, "table": t.Name, "id": id, "row": row, "before": prev, "by": by},
		Params: map[string]string{}, Query: map[string]string{}, Form: map[string]string{}, FormAll: map[string][]string{}}
	resp, err := p.sb.Run(ctx, st.def.Scripts[script], req, host)
	took := time.Since(start).Round(time.Microsecond)
	if err != nil {
		var se *sandbox.ScriptError
		if errors.As(err, &se) {
			err = fmt.Errorf("script %s: %s", script, se.Message)
		}
		return fail(err)
	}
	run.Resp = resp
	msg := fmt.Sprintf("on_admin_change %s after %s by %s -> ok in %s", script, what, by, took)
	if text := strings.TrimSpace(hookText(resp)); text != "" {
		msg += ": " + text
	}
	p.event(st, "server", "info", rid, msg)
	return run
}

func hookText(r sandbox.Response) string {
	if r.HasJSON {
		b, err := json.Marshal(r.JSON)
		if err != nil {
			return fmt.Sprint(r.JSON)
		}
		return string(b)
	}
	return r.Text
}

// AdminNotice returns, once, a notice for the admin panel about the last
// change by that person whose on_admin_change script failed, or "".
func (p *Platform) AdminNotice(app, by string) string {
	v, ok := p.adminNotices.LoadAndDelete(app + "\x00" + by)
	if !ok {
		return ""
	}
	return v.(string)
}

// ---- test_request {admin: ...}: an admin panel change on preview

// TestAdminInput is a change to a preview row made as the admin panel does.
type TestAdminInput struct {
	Action string         `json:"action" jsonschema:"insert, update or delete"`
	Table  string         `json:"table"`
	ID     int64          `json:"id,omitempty" jsonschema:"row id, for update and delete"`
	Values map[string]any `json:"values,omitempty" jsonschema:"column values; on update, columns left out keep their current value, as in the admin panel form"`
	By     string         `json:"by,omitempty" jsonschema:"email of the owner or staff member, given to the script as req.body.by; default owner@example.com"`
}

// TestAdminResult is the row after a test_request admin change.
type TestAdminResult struct {
	Action string         `json:"action"`
	Table  string         `json:"table"`
	ID     int64          `json:"id"`
	Row    map[string]any `json:"row" jsonschema:"the row after the change; null after delete"`
	Hook   string         `json:"hook,omitempty" jsonschema:"the on_admin_change script that ran"`
}

// testAdmin changes a preview row exactly like the admin panel (same
// validation and type conversion), then runs on_admin_change.
func (p *Platform) testAdmin(ctx context.Context, app, stageName string, in TestAdminInput) (TestResponse, error) {
	if stageName != Preview {
		return TestResponse{}, errors.New("admin changes can only be tested on preview; on live the owner makes them in the admin panel")
	}
	st, t, err := p.stageTable(app, Preview, in.Table)
	if err != nil {
		return TestResponse{}, err
	}
	by := in.By
	if by == "" {
		by = "owner@example.com"
	}
	values := map[string]string{}
	switch in.Action {
	case "update":
		row := p.rowOf(ctx, st, t, in.ID)
		if row == nil {
			return TestResponse{}, fmt.Errorf("%s has no row %d on preview", t.Name, in.ID)
		}
		for _, c := range t.Columns {
			values[c.Name] = adminFormValue(row[c.Name], c.Type)
		}
	case "insert", "delete":
	default:
		return TestResponse{}, errors.New(`admin.action must be "insert", "update" or "delete"`)
	}
	if in.Action != "delete" {
		for k, v := range in.Values {
			if !hasCol(t, k) {
				return TestResponse{}, fmt.Errorf("table %s has no column %q (id and created_at are set by the platform)", t.Name, k)
			}
			values[k] = formText(v)
		}
	}
	rid := newRequestID()
	out := TestResponse{Status: http.StatusOK, ContentType: "text/plain; charset=utf-8", RequestID: rid}
	var run *hookRun
	id := in.ID
	if in.Action == "delete" {
		run, err = p.adminDelete(ctx, st, t, id, by, rid)
	} else {
		if in.Action == "insert" {
			id = 0
		}
		id, run, err = p.adminSave(ctx, st, t, id, values, by, rid)
	}
	var ferr FieldErrors
	switch {
	case errors.As(err, &ferr):
		out.Status, out.Body = http.StatusUnprocessableEntity, "the admin panel would refuse this: "+ferr.Error()
	case errors.Is(err, ErrNotFound):
		out.Status, out.Body = http.StatusNotFound, err.Error()
	case err != nil:
		out.Status, out.Body = http.StatusConflict, err.Error()
	default:
		res := &TestAdminResult{Action: in.Action, Table: t.Name, ID: id}
		if in.Action != "delete" {
			res.Row = p.rowOf(ctx, st, t, id)
		}
		out.Admin = res
		switch {
		case run == nil:
			out.Body = "saved; the app has no on_admin_change script"
		case run.Err != nil:
			res.Hook = run.Script
			out.Status, out.Body = http.StatusInternalServerError, run.Err.Error()+" (the change itself was saved)"
		default:
			res.Hook = run.Script
			out.Body = hookText(run.Resp)
			if run.Resp.HasJSON {
				out.ContentType = "application/json"
			}
		}
	}
	out.Logs = []applog.Event{}
	if l, err := p.log(app); err == nil {
		if events, err := l.Query(applog.Filter{RequestID: rid, Limit: 50}); err == nil {
			out.Logs = events
		}
	}
	return out, nil
}

func (p *Platform) rowOf(ctx context.Context, st *stage, t appdef.Table, id int64) map[string]any {
	rows, err := st.db.Query(ctx, "SELECT * FROM "+quote(t.Name)+" WHERE id = ?", []any{id})
	if err != nil || len(rows) == 0 {
		return nil
	}
	return rows[0]
}

func hasCol(t appdef.Table, name string) bool {
	for _, c := range t.Columns {
		if c.Name == name {
			return true
		}
	}
	return false
}

// adminFormValue is what the admin panel's form holds for a stored value.
func adminFormValue(v any, typ string) string {
	switch typ {
	case "boolean":
		if x, ok := v.(int64); ok && x != 0 {
			return "1"
		}
		return ""
	case "datetime":
		s, _ := v.(string)
		if len(s) >= 16 {
			return strings.Replace(s[:16], " ", "T", 1)
		}
		return s
	}
	return formText(v)
}

// formText turns a JSON value into the text a form field would send.
func formText(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case bool:
		if x {
			return "1"
		}
		return ""
	case int64:
		return strconv.FormatInt(x, 10)
	case int:
		return strconv.Itoa(x)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case json.Number:
		return x.String()
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}
