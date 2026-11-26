package platform

import (
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/arif-rachim/renoxium/internal/appdef"
)

// Preview edits: rows of the business's own data (tables with starting
// rows, such as services, staff, products and the settings row) changed
// on the preview with change_preview_row or the admin panel's preview
// data. They live only in the preview's database, which publish never
// copies to live and every change replaces, so an assistant that keeps
// the owner's data there loses it. They are recorded until the next
// change rebuilds the preview, and publish asks for them to be saved in
// the definition or dropped on purpose.

// PreviewEdit is one row of the business's data changed only on the preview.
type PreviewEdit struct {
	Table  string   `json:"table"`
	Row    int64    `json:"row"`
	Action string   `json:"action" jsonschema:"insert, update or delete"`
	Fields []string `json:"fields" jsonschema:"the columns changed"`
}

// UnsavedNote is said with every list of preview edits.
const UnsavedNote = "These rows were changed only in the preview's test data (change_preview_row or the admin panel's preview data). " +
	"They are not in the app's definition, so publish does not take them and the next change replaces them. " +
	"To keep them, put the same values in the definition's seed rows (or the settings row) with edit_app or patch_app."

func (p *Platform) initPreviewEdits() error {
	_, err := p.db.Exec(`CREATE TABLE IF NOT EXISTS preview_edits (app TEXT NOT NULL, tbl TEXT NOT NULL, row_id INTEGER NOT NULL,
		action TEXT NOT NULL, fields TEXT NOT NULL, at TEXT NOT NULL, PRIMARY KEY (app, tbl, row_id))`)
	return err
}

// businessTable reports whether t holds the business's own data: the
// definition gives it starting rows, or it is the settings (brand) table.
func businessTable(def *appdef.App, t string) bool {
	if def == nil {
		return false
	}
	if def.Brand != nil && def.Brand.Table == t {
		return true
	}
	return len(def.Seed[t]) > 0
}

// notePreviewEdit records a change to a preview row of the business's data.
// before and after are the row before and after (nil for insert/delete).
func (p *Platform) notePreviewEdit(st *stage, t appdef.Table, id int64, action string, before, after map[string]any) {
	if !tracksPreviewEdit(st, t) {
		return
	}
	var fields []string
	for _, c := range t.Columns {
		switch action {
		case "insert":
			if v, ok := after[c.Name]; ok && v != nil && fmt.Sprint(v) != "" {
				fields = append(fields, c.Name)
			}
		case "update":
			if fmt.Sprint(before[c.Name]) != fmt.Sprint(after[c.Name]) {
				fields = append(fields, c.Name)
			}
		}
	}
	if action == "update" && len(fields) == 0 {
		return
	}
	var prevAction, prevFields string
	p.db.QueryRow(`SELECT action, fields FROM preview_edits WHERE app = ? AND tbl = ? AND row_id = ?`, st.app, t.Name, id).Scan(&prevAction, &prevFields)
	var old []string
	json.Unmarshal([]byte(prevFields), &old)
	for _, f := range old {
		if !slices.Contains(fields, f) {
			fields = append(fields, f)
		}
	}
	sort.Strings(fields)
	switch {
	case prevAction == "insert" && action == "delete":
		p.db.Exec(`DELETE FROM preview_edits WHERE app = ? AND tbl = ? AND row_id = ?`, st.app, t.Name, id) // added and removed: nothing left
		return
	case prevAction == "insert":
		action = "insert" // still a new row, with more fields
	}
	if action == "delete" {
		fields = []string{}
	}
	raw, _ := json.Marshal(fields)
	p.db.Exec(`INSERT OR REPLACE INTO preview_edits (app, tbl, row_id, action, fields, at) VALUES (?, ?, ?, ?, ?, ?)`,
		st.app, t.Name, id, action, string(raw), now())
}

// UnsavedPreviewChanges lists the preview edits not in the definition.
func (p *Platform) UnsavedPreviewChanges(app string) []PreviewEdit {
	rows, err := p.db.Query(`SELECT tbl, row_id, action, fields FROM preview_edits WHERE app = ? ORDER BY tbl, row_id`, app)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := []PreviewEdit{}
	for rows.Next() {
		var e PreviewEdit
		var fields string
		if rows.Scan(&e.Table, &e.Row, &e.Action, &fields) != nil {
			continue
		}
		json.Unmarshal([]byte(fields), &e.Fields)
		if e.Fields == nil {
			e.Fields = []string{}
		}
		out = append(out, e)
	}
	return out
}

// DiscardPreviewChanges forgets the recorded preview edits: the owner chose
// to publish without them.
func (p *Platform) DiscardPreviewChanges(app string) {
	p.db.Exec(`DELETE FROM preview_edits WHERE app = ?`, app)
}

// UnsavedSummary is a one-line list of preview edits, e.g. for a refusal.
func UnsavedSummary(edits []PreviewEdit) string {
	var parts []string
	for _, e := range edits {
		s := fmt.Sprintf("%s #%d (%s", e.Table, e.Row, e.Action)
		if len(e.Fields) > 0 {
			s += ": " + strings.Join(e.Fields, ", ")
		}
		parts = append(parts, s+")")
	}
	return strings.Join(parts, "; ")
}

// tracksPreviewEdit reports whether a change to t on st is recorded.
func tracksPreviewEdit(st *stage, t appdef.Table) bool {
	return st != nil && st.name == Preview && businessTable(st.def, t.Name)
}
