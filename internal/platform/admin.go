package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/arif-rachim/renoxium/internal/appdef"
)

// AdminPageSize is the number of rows on one admin list page.
const AdminPageSize = 50

// Tables returns the tables of an app's live version, or nil if it has not
// been published yet.
func (p *Platform) Tables(app string) ([]appdef.Table, error) {
	st, err := p.loadStage(app, p.AdminStage(app))
	if err != nil || st == nil {
		return nil, err
	}
	return st.def.Tables, nil
}

func (p *Platform) liveTable(app, table string) (*stage, appdef.Table, error) {
	return p.stageTable(app, p.AdminStage(app), table)
}

// AdminStage is the stage the admin panel shows: live, or for a template
// demo (which has no live stage) its public preview.
func (p *Platform) AdminStage(app string) string {
	if p.IsDemo(app) {
		return Preview
	}
	return Live
}

// stageTable finds a table of a stage. The admin panel uses live only;
// test_request edits preview rows the same way.
func (p *Platform) stageTable(app, stageName, table string) (*stage, appdef.Table, error) {
	st, err := p.loadStage(app, stageName)
	if err != nil {
		return nil, appdef.Table{}, err
	}
	if st == nil {
		if stageName == Preview {
			return nil, appdef.Table{}, errors.New("preview has no version yet: call apply_change first")
		}
		return nil, appdef.Table{}, fmt.Errorf("app %q is not published yet", app)
	}
	for _, t := range st.def.Tables {
		if t.Name == table {
			return st, t, nil
		}
	}
	return nil, appdef.Table{}, fmt.Errorf("table %q: %w", table, ErrNotFound)
}

// AdminList returns one page of a live table, newest first. A non-empty
// search matches any text column.
func (p *Platform) AdminList(ctx context.Context, app, table, search string, page int) (rows []map[string]any, total int, err error) {
	st, t, err := p.liveTable(app, table)
	if err != nil {
		return nil, 0, err
	}
	where, args := "", []any{}
	if search = strings.TrimSpace(search); search != "" {
		var conds []string
		for _, c := range t.Columns {
			if c.Type == "text" || c.Type == "json" {
				conds = append(conds, quote(c.Name)+" LIKE ? ESCAPE '\\'")
				args = append(args, "%"+likeEscape(search)+"%")
			}
		}
		if id, err := strconv.ParseInt(search, 10, 64); err == nil {
			conds = append(conds, "id = ?")
			args = append(args, id)
		}
		if len(conds) == 0 {
			conds = []string{"0"}
		}
		where = " WHERE " + strings.Join(conds, " OR ")
	}
	counted, err := st.db.Query(ctx, "SELECT count(*) AS n FROM "+quote(t.Name)+where, args)
	if err != nil {
		return nil, 0, err
	}
	total = int(counted[0]["n"].(int64))
	if page < 1 {
		page = 1
	}
	args = append(args, AdminPageSize, (page-1)*AdminPageSize)
	rows, err = st.db.Query(ctx, "SELECT * FROM "+quote(t.Name)+where+" ORDER BY id DESC LIMIT ? OFFSET ?", args)
	return rows, total, err
}

// AdminGet returns one row of a live table.
func (p *Platform) AdminGet(ctx context.Context, app, table string, id int64) (map[string]any, error) {
	st, t, err := p.liveTable(app, table)
	if err != nil {
		return nil, err
	}
	rows, err := st.db.Query(ctx, "SELECT * FROM "+quote(t.Name)+" WHERE id = ?", []any{id})
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("row %d: %w", id, ErrNotFound)
	}
	return rows[0], nil
}

// FieldErrors maps a column name to what is wrong with the submitted value.
type FieldErrors map[string]string

func (f FieldErrors) Error() string {
	parts := make([]string, 0, len(f))
	for k, v := range f {
		parts = append(parts, k+": "+v)
	}
	return strings.Join(parts, "; ")
}

// AdminSave inserts (id == 0) or updates a row from submitted form values.
// Every column of the table is written; a missing value is stored as NULL.
// The app's on_admin_change script then runs; its failure is logged and
// does not undo or fail the save.
func (p *Platform) AdminSave(ctx context.Context, app, table string, id int64, values map[string]string, by string) (int64, error) {
	st, t, err := p.liveTable(app, table)
	if err != nil {
		return 0, err
	}
	id, _, err = p.adminSave(ctx, st, t, id, values, by, newRequestID())
	return id, err
}

// adminSave is AdminSave on any stage. It also returns the on_admin_change
// run, nil when the app has none.
func (p *Platform) adminSave(ctx context.Context, st *stage, t appdef.Table, id int64, values map[string]string, by, rid string) (int64, *hookRun, error) {
	cols, args, ferr := convert(t, values)
	if len(ferr) > 0 {
		return 0, nil, ferr
	}
	quoted := make([]string, len(cols))
	for i, c := range cols {
		quoted[i] = quote(c)
	}
	if id == 0 {
		marks := strings.TrimSuffix(strings.Repeat("?,", len(cols)), ",")
		_, newID, err := st.db.Exec(ctx, "INSERT INTO "+quote(t.Name)+" ("+strings.Join(quoted, ",")+") VALUES ("+marks+")", args)
		if err != nil {
			return 0, nil, friendly(err)
		}
		p.event(st, "admin", "info", rid, fmt.Sprintf("%s added %s #%d", by, t.Name, newID))
		return newID, p.afterAdminChange(ctx, st, t, "insert", newID, nil, by, rid), nil
	}
	before := p.hookRow(ctx, st, t, id)
	sets := make([]string, len(cols))
	for i, q := range quoted {
		sets[i] = q + " = ?"
	}
	n, _, err := st.db.Exec(ctx, "UPDATE "+quote(t.Name)+" SET "+strings.Join(sets, ", ")+" WHERE id = ?", append(args, id))
	if err != nil {
		return 0, nil, friendly(err)
	}
	if n == 0 {
		return 0, nil, fmt.Errorf("row %d: %w", id, ErrNotFound)
	}
	p.event(st, "admin", "info", rid, fmt.Sprintf("%s changed %s #%d", by, t.Name, id))
	return id, p.afterAdminChange(ctx, st, t, "update", id, before, by, rid), nil
}

// AdminDelete deletes one row, then runs the app's on_admin_change script.
func (p *Platform) AdminDelete(ctx context.Context, app, table string, id int64, by string) error {
	st, t, err := p.liveTable(app, table)
	if err != nil {
		return err
	}
	_, err = p.adminDelete(ctx, st, t, id, by, newRequestID())
	return err
}

func (p *Platform) adminDelete(ctx context.Context, st *stage, t appdef.Table, id int64, by, rid string) (*hookRun, error) {
	before := p.hookRow(ctx, st, t, id)
	n, _, err := st.db.Exec(ctx, "DELETE FROM "+quote(t.Name)+" WHERE id = ?", []any{id})
	if err != nil {
		return nil, friendly(err)
	}
	if n == 0 {
		return nil, fmt.Errorf("row %d: %w", id, ErrNotFound)
	}
	p.event(st, "admin", "warn", rid, fmt.Sprintf("%s deleted %s #%d", by, t.Name, id))
	return p.afterAdminChange(ctx, st, t, "delete", id, before, by, rid), nil
}

func convert(t appdef.Table, values map[string]string) ([]string, []any, FieldErrors) {
	ferr := FieldErrors{}
	var cols []string
	var args []any
	for _, c := range t.Columns {
		raw := values[c.Name]
		if c.Type != "text" && c.Type != "json" {
			raw = strings.TrimSpace(raw)
		}
		var v any
		switch {
		case c.Type == "boolean":
			v = int64(0)
			if raw == "1" || raw == "on" || raw == "true" {
				v = int64(1)
			}
		case raw == "":
			if c.Required {
				ferr[c.Name] = "required"
				continue
			}
			v = nil
		case c.Type == "integer":
			i, err := strconv.ParseInt(raw, 10, 64)
			if err != nil {
				ferr[c.Name] = "enter a whole number"
				continue
			}
			v = i
		case c.Type == "real":
			f, err := strconv.ParseFloat(raw, 64)
			if err != nil {
				ferr[c.Name] = "enter a number"
				continue
			}
			v = f
		case c.Type == "datetime":
			tm, err := time.Parse("2006-01-02T15:04", raw)
			if err != nil {
				if tm, err = time.Parse("2006-01-02 15:04:05", raw); err != nil {
					ferr[c.Name] = "enter a date and time"
					continue
				}
			}
			v = tm.Format("2006-01-02 15:04:05")
		case c.Type == "json":
			if !json.Valid([]byte(raw)) {
				ferr[c.Name] = "enter valid JSON"
				continue
			}
			v = raw
		case len(c.Options) > 0 && !slices.Contains(c.Options, raw):
			ferr[c.Name] = "choose one of: " + strings.Join(c.Options, ", ")
			continue
		default:
			v = raw
		}
		cols = append(cols, c.Name)
		args = append(args, v)
	}
	if len(ferr) > 0 {
		return nil, nil, ferr
	}
	return cols, args, nil
}

// friendly turns constraint failures into messages an owner understands.
func friendly(err error) error {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "UNIQUE constraint failed"):
		col := msg[strings.LastIndex(msg, ".")+1:]
		return FieldErrors{col: "another row already has this value"}
	case strings.Contains(msg, "NOT NULL constraint failed"):
		col := msg[strings.LastIndex(msg, ".")+1:]
		return FieldErrors{col: "required"}
	case strings.Contains(msg, "FOREIGN KEY"):
		return errors.New("other rows still refer to this one")
	}
	return err
}

func likeEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

func quote(ident string) string { return `"` + ident + `"` }
