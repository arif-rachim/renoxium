package platform

import (
	"context"
	"database/sql"
	"sort"
)

// demoSamples holds sample rows for the demos only: example -> table ->
// rows. They fill sections that a real site shows only with the owner's own
// content (client reviews, a rating), so a demo's home page looks like a
// running business. The templates handed to owners keep these tables empty,
// and every sample name is marked "(sample)". Each example adds its rows in
// its own demosample_<example>.go file.
var demoSamples = map[string]map[string][]map[string]any{}

// demoSettings holds sample values for the demos' settings row only:
// example -> table -> column -> value, written to every row of that table.
var demoSettings = map[string]map[string]map[string]any{}

// demoExtras holds rows a demo gets on top of its template's seed rows:
// example -> table -> rows, each added unless a row with the same key value
// is already there.
var demoExtras = map[string]map[string]demoExtra{}

type demoExtra struct {
	key  string
	rows []map[string]any
	// photos gives those rows demo photos: key value -> image column -> file in examples/demo-images.
	photos map[string]map[string]string
}

// demoExtraRows adds an example's extra demo rows, with their photos.
func (p *Platform) demoExtraRows(ctx context.Context, db *sql.DB, app, example string) error {
	for table, x := range demoExtras[example] {
		for _, row := range x.rows {
			var n int
			if err := db.QueryRowContext(ctx, `SELECT count(*) FROM "`+table+`" WHERE "`+x.key+`" = ?`, row[x.key]).Scan(&n); err != nil || n > 0 {
				continue
			}
			if err := insertDemoRow(ctx, db, table, row); err != nil {
				return err
			}
		}
		for key, cols := range x.photos {
			for col, file := range cols {
				url, err := p.demoPhoto(app, file)
				if err != nil {
					return err
				}
				if _, err := db.ExecContext(ctx, `UPDATE "`+table+`" SET "`+col+`" = ? WHERE "`+x.key+`" = ?`, url, key); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// demoSampleRows inserts an example's sample rows into tables that are
// still empty, and writes its sample settings. A table or column an older
// demo does not have is skipped.
func demoSampleRows(ctx context.Context, db *sql.DB, example string) error {
	tables := make([]string, 0, len(demoSamples[example]))
	for t := range demoSamples[example] {
		tables = append(tables, t)
	}
	sort.Strings(tables)
	for _, table := range tables {
		var n int
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM "`+table+`"`).Scan(&n); err != nil || n > 0 {
			continue
		}
		for i, row := range demoSamples[example][table] {
			r := map[string]any{"position": i + 1, "active": 1}
			for k, v := range row {
				r[k] = v
			}
			if err := insertDemoRow(ctx, db, table, r); err != nil {
				return err
			}
		}
	}
	for table, cols := range demoSettings[example] {
		for col, v := range cols {
			db.ExecContext(ctx, `UPDATE "`+table+`" SET "`+col+`" = ?`, v) // a missing column is skipped
		}
	}
	return nil
}
