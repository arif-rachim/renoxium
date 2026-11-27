package platform

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// Test rows: rows the assistant's test tools wrote on the preview (orders
// from submit_form, visits a page script records, rows a job makes, the
// accounts of create_test_visitor). They stay in the preview's data until
// the next change, and got in the owner's way: a test order made a shop
// look already ordered for the day, test counts made a stock plan wrong.
// They are recorded so they can be shown as test rows in the admin panel
// and removed with clear_test_rows.

// visitorsTable is how test visitor accounts are recorded in test_rows.
const visitorsTable = "rx_users"

func (p *Platform) initTestRows() error {
	_, err := p.db.Exec(`CREATE TABLE IF NOT EXISTS test_rows (app TEXT NOT NULL, tbl TEXT NOT NULL, row_id INTEGER NOT NULL,
		at TEXT NOT NULL, PRIMARY KEY (app, tbl, row_id))`)
	return err
}

// maxIDs is the highest row id of every table of the preview.
func (p *Platform) maxIDs(ctx context.Context, st *stage) map[string]int64 {
	out := map[string]int64{}
	for _, t := range st.def.Tables {
		rows, err := st.db.Query(ctx, "SELECT coalesce(max(id), 0) AS n FROM "+quote(t.Name), nil)
		if err == nil && len(rows) == 1 {
			out[t.Name] = toInt64(rows[0]["n"])
		}
	}
	return out
}

func toInt64(v any) int64 {
	switch x := v.(type) {
	case int64:
		return x
	case float64:
		return int64(x)
	case int:
		return int64(x)
	}
	return 0
}

// recordTestRows runs a test on the preview and records the rows it added,
// returned as table#id. Rows someone else adds to the preview at the same
// moment are recorded too; the preview is the owner's alone, so that is rare.
func (p *Platform) recordTestRows(ctx context.Context, app string, test func() error) ([]string, error) {
	st, err := p.loadStage(app, Preview)
	if err != nil || st == nil {
		return nil, test()
	}
	before := p.maxIDs(ctx, st)
	err = test()
	var added []string
	for _, t := range st.def.Tables {
		rows, qerr := st.db.Query(ctx, "SELECT id FROM "+quote(t.Name)+" WHERE id > ? ORDER BY id LIMIT 200", []any{before[t.Name]})
		if qerr != nil {
			continue
		}
		for _, r := range rows {
			id := toInt64(r["id"])
			p.db.Exec(`INSERT OR IGNORE INTO test_rows (app, tbl, row_id, at) VALUES (?, ?, ?, ?)`, app, t.Name, id, now())
			added = append(added, fmt.Sprintf("%s#%d", t.Name, id))
		}
	}
	return added, err
}

// markTestVisitor records a visitor create_test_visitor made.
func (p *Platform) markTestVisitor(app string, id int64) {
	p.db.Exec(`INSERT OR IGNORE INTO test_rows (app, tbl, row_id, at) VALUES (?, ?, ?, ?)`, app, visitorsTable, id, now())
}

// TestRowIDs are the recorded test rows of one preview table.
func (p *Platform) TestRowIDs(app, table string) map[int64]bool {
	out := map[int64]bool{}
	rows, err := p.db.Query(`SELECT row_id FROM test_rows WHERE app = ? AND tbl = ?`, app, table)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			out[id] = true
		}
	}
	return out
}

// TestRowsNote is said with the rows a test wrote.
func TestRowsNote(rows []string) string {
	if len(rows) == 0 {
		return ""
	}
	return "this test wrote rows " + strings.Join(rows, ", ") + " to the preview's data; they stay there and can change what the owner sees " +
		"when testing (an order already booked, wrong totals) until you remove them with clear_test_rows"
}

// ClearedTestRows says what ClearTestRows removed.
type ClearedTestRows struct {
	Removed  map[string]int `json:"removed" jsonschema:"rows removed per table (rx_users: test visitor accounts)"`
	Note     string         `json:"note"`
	Messages []string       `json:"problems,omitempty"`
}

// ClearTestRows deletes the rows recorded as test rows from the preview,
// and test visitor accounts with their sessions. Rows of the business's
// data, the owner's own testing and everything on live are not touched.
func (p *Platform) ClearTestRows(ctx context.Context, app string) (ClearedTestRows, error) {
	out := ClearedTestRows{Removed: map[string]int{}}
	if _, err := p.Info(app); err != nil {
		return out, err
	}
	st, err := p.loadStage(app, Preview)
	if err != nil || st == nil {
		return out, err
	}
	rows, err := p.db.Query(`SELECT tbl, row_id FROM test_rows WHERE app = ? ORDER BY tbl, row_id DESC`, app)
	if err != nil {
		return out, err
	}
	byTable := map[string][]int64{}
	for rows.Next() {
		var t string
		var id int64
		if rows.Scan(&t, &id) == nil {
			byTable[t] = append(byTable[t], id)
		}
	}
	rows.Close()
	tables := make([]string, 0, len(byTable))
	for t := range byTable {
		tables = append(tables, t)
	}
	sort.Strings(tables)
	known := map[string]bool{visitorsTable: true}
	for _, t := range st.def.Tables {
		known[t.Name] = true
	}
	for _, t := range tables {
		if !known[t] {
			continue
		}
		for _, id := range byTable[t] {
			var n int64
			if t == visitorsTable {
				st.admin.ExecContext(ctx, `DELETE FROM rx_sessions WHERE user_id = ?`, id)
				st.admin.ExecContext(ctx, `DELETE FROM rx_test_visitors WHERE user_id = ?`, id)
				res, err := st.admin.ExecContext(ctx, `DELETE FROM rx_users WHERE id = ?`, id)
				if err == nil {
					n, _ = res.RowsAffected()
				}
			} else {
				var derr error
				n, _, derr = st.db.Exec(ctx, "DELETE FROM "+quote(t)+" WHERE id = ?", []any{id})
				if derr != nil {
					out.Messages = append(out.Messages, fmt.Sprintf("%s#%d: %v", t, id, friendly(derr)))
					continue
				}
			}
			if n > 0 {
				out.Removed[t]++
			}
		}
	}
	p.db.Exec(`DELETE FROM test_rows WHERE app = ?`, app)
	p.event(st, "admin", "info", "", "test rows removed from the preview")
	out.Note = "The preview's test rows are gone; the business's data and the owner's own test rows are as they were."
	return out, nil
}

// forgetTestRows is called when the preview's data is rebuilt: its rows are
// gone, and only test visitor accounts are carried over.
func (p *Platform) forgetTestRows(app string) {
	p.db.Exec(`DELETE FROM test_rows WHERE app = ? AND tbl != ?`, app, visitorsTable)
}
