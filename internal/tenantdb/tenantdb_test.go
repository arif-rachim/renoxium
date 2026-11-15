package tenantdb

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/arif-rachim/renoxium/internal/appdef"
)

func TestCheckScriptSQL(t *testing.T) {
	ok := []string{
		"SELECT * FROM products",
		"  -- comment\n select 1",
		"/* c */ INSERT INTO t (a) VALUES (?)",
		"select ';' as semi",
		"WITH x AS (SELECT 1) SELECT * FROM x;",
		"UPDATE t SET a = 'x;y' WHERE id = ?",
	}
	for _, q := range ok {
		if err := CheckScriptSQL(q); err != nil {
			t.Errorf("%q: unexpected error %v", q, err)
		}
	}
	bad := []string{
		"ATTACH DATABASE '/etc/passwd' AS x",
		"PRAGMA table_info(t)",
		"DROP TABLE products",
		"CREATE TABLE x (a)",
		"SELECT 1; DROP TABLE products",
		"VACUUM INTO '/tmp/x'",
		"",
	}
	for _, q := range bad {
		if err := CheckScriptSQL(q); err == nil {
			t.Errorf("%q: expected rejection", q)
		}
	}
}

func setup(t *testing.T) (*DB, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "app.db")
	tables := []appdef.Table{{Name: "products", Columns: []appdef.Column{{Name: "name", Type: "text", Required: true}, {Name: "price", Type: "real"}}}}
	seed := map[string][]map[string]any{"products": {{"name": "Oud", "price": 120.0}, {"name": "Musk", "price": 45.5}}}
	applied, err := Migrate(path, tables, seed)
	if err != nil {
		t.Fatal(err)
	}
	if len(applied) != 2 {
		t.Fatalf("applied = %v", applied)
	}
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db, path
}

func TestQueryAndExec(t *testing.T) {
	db, _ := setup(t)
	ctx := context.Background()
	rows, err := db.Query(ctx, "SELECT name, price FROM products ORDER BY price", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0]["name"] != "Musk" {
		t.Fatalf("rows = %v", rows)
	}
	n, id, err := db.Exec(ctx, "INSERT INTO products (name, price) VALUES (?, ?)", []any{"Amber", 60})
	if err != nil || n != 1 || id != 3 {
		t.Fatalf("exec = %d %d %v", n, id, err)
	}
}

// The authorizer is the real boundary: it must hold even for SQL that slips
// past the textual check.
func TestAuthorizerDeniesEscapes(t *testing.T) {
	db, _ := setup(t)
	ctx := context.Background()
	other := filepath.Join(t.TempDir(), "other.db")
	for _, q := range []string{
		"ATTACH DATABASE '" + other + "' AS x",
		"PRAGMA writable_schema = 1",
		"DROP TABLE products",
		"CREATE TABLE evil (a)",
		"SELECT load_extension('x')",
		"DELETE FROM sqlite_master",
	} {
		if _, err := db.pool.ExecContext(ctx, q); err == nil {
			t.Errorf("%q was allowed by the authorizer", q)
		}
	}
	// Reading the schema table directly is the one read we still allow; writes are not.
	if _, _, err := db.Exec(ctx, "UPDATE sqlite_master SET sql = 'x'", nil); err == nil {
		t.Error("writing sqlite_master was allowed")
	}
}

func TestMigrateIsAdditive(t *testing.T) {
	_, path := setup(t)
	tables := []appdef.Table{
		{Name: "products", Columns: []appdef.Column{{Name: "name", Type: "text"}, {Name: "stock", Type: "integer", Required: true}}},
		{Name: "orders", Columns: []appdef.Column{{Name: "total", Type: "real"}}},
	}
	applied, err := Migrate(path, tables, map[string][]map[string]any{"products": {{"name": "ignored"}}})
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(applied, "; ")
	if got != "added column products.stock; created table orders" {
		t.Fatalf("applied = %s", got)
	}
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := db.Query(context.Background(), "SELECT name, price, stock FROM products", nil)
	if err != nil {
		t.Fatal(err)
	}
	// Existing rows and the column dropped from the definition are kept; seed is not re-run.
	if len(rows) != 2 || rows[0]["price"] == nil {
		t.Fatalf("rows = %v", rows)
	}
}
