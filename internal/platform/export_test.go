package platform

import (
	"fmt"
	"testing"
	"time"
)

// SeedFiles records n placeholder files for an app in one transaction, so a
// test can reach the file limit without n uploads that each sync the disk.
func SeedFiles(t *testing.T, p *Platform, app string, n int) {
	t.Helper()
	tx, err := p.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for i := 0; i < n; i++ {
		hash := fmt.Sprintf("seed%060d", i)
		if _, err := tx.Exec(`INSERT INTO blobs (hash, size, content_type, created_at) VALUES (?, 1, 'image/png', ?)`, hash, now()); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(`INSERT INTO app_files (app, hash, created_at) VALUES (?, ?, ?)`, app, hash, now()); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

// SetDemoClock moves the time demo schedules are built around, until the
// test ends.
func SetDemoClock(t *testing.T, now func() time.Time) {
	old := demoClock
	demoClock = now
	t.Cleanup(func() { demoClock = old })
}

// PreviewRows runs a query on an app's preview database, as platform code.
func PreviewRows(t *testing.T, p *Platform, app, query string, args ...any) []map[string]any {
	t.Helper()
	st, err := p.loadStage(app, Preview)
	if err != nil || st == nil {
		t.Fatalf("preview of %s: %v", app, err)
	}
	rows, err := st.admin.Query(query, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	cols, _ := rows.Columns()
	var out []map[string]any
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			t.Fatal(err)
		}
		m := map[string]any{}
		for i, c := range cols {
			m[c] = vals[i]
		}
		out = append(out, m)
	}
	return out
}

// CurrentNameForTest follows renames from an old app name.
func CurrentNameForTest(p *Platform, name string) string { return p.currentName(name) }

// DemoExtraRows returns the rows a demo gets on top of its template's seed
// rows in one table, so a test can build the same site.
func DemoExtraRows(example, table string) []map[string]any { return demoExtras[example][table].rows }

// SetMaxTestBody changes where a test response's body is cut and returns a
// function that puts the old limit back, for a test that needs a page's
// last inline script.
func SetMaxTestBody(n int) (restore func()) {
	old := maxTestBody
	maxTestBody = n
	return func() { maxTestBody = old }
}
