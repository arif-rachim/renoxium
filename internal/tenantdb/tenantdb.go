// Package tenantdb manages one SQLite file per app stage. Scripts reach the
// database only through a restricted connection pool: no ATTACH, no PRAGMA,
// no schema changes. Schema changes come from the app definition via Migrate.
package tenantdb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"

	"github.com/arif-rachim/renoxium/internal/appdef"
)

const (
	restrictedDriver = "rx_tenant"
	// MaxRows caps the rows a single script query may return.
	MaxRows = 5000
	// maxPages caps a tenant database at about 200 MB (4 KiB pages).
	maxPages = 51200
	// QueryTimeout stops one script statement that runs too long, such as a
	// runaway recursive query. It runs in the platform process, so the
	// script worker's own time limit does not reach it.
	QueryTimeout = 2 * time.Second
)

// scriptLimits bound what one script statement can make SQLite allocate in
// the platform process.
var scriptLimits = map[int]int{
	sqlite3.SQLITE_LIMIT_LENGTH:              4 << 20, // largest string or blob value
	sqlite3.SQLITE_LIMIT_SQL_LENGTH:          100 << 10,
	sqlite3.SQLITE_LIMIT_EXPR_DEPTH:          100,
	sqlite3.SQLITE_LIMIT_COMPOUND_SELECT:     50,
	sqlite3.SQLITE_LIMIT_LIKE_PATTERN_LENGTH: 1000,
	sqlite3.SQLITE_LIMIT_VARIABLE_NUMBER:     999,
	sqlite3.SQLITE_LIMIT_FUNCTION_ARG:        32,
}

var registerOnce sync.Once

func register() {
	registerOnce.Do(func() {
		d := &sqlite.Driver{}
		d.RegisterConnectionHook(func(conn sqlite.ExecQuerierContext, _ string) error {
			for _, pragma := range []string{
				"PRAGMA busy_timeout = 5000",
				"PRAGMA foreign_keys = ON",
				// Sorting and temporary results spill to disk instead of memory.
				"PRAGMA temp_store = FILE",
				"PRAGMA cache_size = -8000",
				fmt.Sprintf("PRAGMA max_page_count = %d", maxPages),
			} {
				if _, err := conn.ExecContext(context.Background(), pragma, nil); err != nil {
					return err
				}
			}
			reg, ok := conn.(sqlite.AuthorizerRegisterer)
			if !ok {
				return errors.New("tenantdb: driver does not support an authorizer")
			}
			return reg.RegisterAuthorizer(authorize)
		})
		sql.Register(restrictedDriver, d)
	})
}

// authorize is the policy for script-issued SQL: data access only, and
// never to the platform's own rx_ tables (visitor accounts and sessions).
func authorize(action sqlite.AuthorizerActionCode, arg1, _, _, _ string) sqlite.AuthorizerReturnCode {
	switch action {
	case sqlite.AuthRead, sqlite.AuthInsert, sqlite.AuthUpdate, sqlite.AuthDelete:
		if strings.HasPrefix(strings.ToLower(arg1), "rx_") {
			return sqlite.AuthorizerDeny
		}
		return sqlite.AuthorizerOK
	case sqlite.AuthSelect, sqlite.AuthTransaction, sqlite.AuthSavepoint, sqlite.AuthRecursive:
		return sqlite.AuthorizerOK
	case sqlite.AuthFunction:
		if strings.EqualFold(arg1, "load_extension") {
			return sqlite.AuthorizerDeny
		}
		return sqlite.AuthorizerOK
	}
	return sqlite.AuthorizerDeny
}

// DB is the restricted handle scripts use.
type DB struct {
	pool *sql.DB
}

// Open opens the restricted pool for an existing tenant database file.
func Open(path string) (*DB, error) {
	register()
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}
	pool, err := sql.Open(restrictedDriver, "file:"+path)
	if err != nil {
		return nil, err
	}
	pool.SetMaxOpenConns(4)
	if err := pool.Ping(); err != nil {
		pool.Close()
		return nil, err
	}
	return &DB{pool: pool}, nil
}

// conn takes a connection from the pool with the script limits applied.
func (d *DB) conn(ctx context.Context) (*sql.Conn, error) {
	c, err := d.pool.Conn(ctx)
	if err != nil {
		return nil, err
	}
	for id, v := range scriptLimits {
		if _, err := sqlite.Limit(c, id, v); err != nil {
			c.Close()
			return nil, err
		}
	}
	return c, nil
}

// Close releases the pool.
func (d *DB) Close() error { return d.pool.Close() }

// Query runs a single read statement and returns rows as maps.
func (d *DB) Query(ctx context.Context, query string, args []any) ([]map[string]any, error) {
	if err := CheckScriptSQL(query); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, QueryTimeout)
	defer cancel()
	c, err := d.conn(ctx)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	rows, err := c.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	out := []map[string]any{}
	for rows.Next() {
		if len(out) >= MaxRows {
			return nil, fmt.Errorf("query returned more than %d rows; add a LIMIT", MaxRows)
		}
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		row := make(map[string]any, len(cols))
		for i, c := range cols {
			if b, ok := vals[i].([]byte); ok {
				row[c] = string(b)
			} else {
				row[c] = vals[i]
			}
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// Exec runs a single write statement.
func (d *DB) Exec(ctx context.Context, query string, args []any) (rowsAffected, lastInsertID int64, err error) {
	if err := CheckScriptSQL(query); err != nil {
		return 0, 0, err
	}
	ctx, cancel := context.WithTimeout(ctx, QueryTimeout)
	defer cancel()
	c, err := d.conn(ctx)
	if err != nil {
		return 0, 0, err
	}
	defer c.Close()
	res, err := c.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, 0, err
	}
	rowsAffected, _ = res.RowsAffected()
	lastInsertID, _ = res.LastInsertId()
	return rowsAffected, lastInsertID, nil
}

var allowedVerbs = map[string]bool{"select": true, "insert": true, "update": true, "delete": true, "with": true, "replace": true}

// CheckScriptSQL accepts exactly one SELECT/INSERT/UPDATE/DELETE/WITH/REPLACE
// statement. It is a second line of defence in front of the authorizer.
func CheckScriptSQL(q string) error {
	i, n := 0, len(q)
	// Skip leading whitespace and comments to find the first keyword.
	for i < n {
		switch {
		case q[i] == ' ' || q[i] == '\t' || q[i] == '\n' || q[i] == '\r':
			i++
		case strings.HasPrefix(q[i:], "--"):
			if j := strings.IndexByte(q[i:], '\n'); j >= 0 {
				i += j + 1
			} else {
				i = n
			}
		case strings.HasPrefix(q[i:], "/*"):
			if j := strings.Index(q[i+2:], "*/"); j >= 0 {
				i += j + 4
			} else {
				i = n
			}
		default:
			goto verb
		}
	}
verb:
	j := i
	for j < n && (q[j] >= 'a' && q[j] <= 'z' || q[j] >= 'A' && q[j] <= 'Z') {
		j++
	}
	if !allowedVerbs[strings.ToLower(q[i:j])] {
		return errors.New("only SELECT, INSERT, UPDATE, DELETE, WITH and REPLACE statements are allowed")
	}
	// Reject a second statement: any ';' outside quotes and comments must be trailing.
	for i = j; i < n; i++ {
		switch c := q[i]; {
		case c == '\'' || c == '"' || c == '`':
			for i++; i < n && q[i] != c; i++ {
			}
		case c == '[':
			for i++; i < n && q[i] != ']'; i++ {
			}
		case strings.HasPrefix(q[i:], "--"):
			if k := strings.IndexByte(q[i:], '\n'); k >= 0 {
				i += k
			} else {
				i = n
			}
		case strings.HasPrefix(q[i:], "/*"):
			if k := strings.Index(q[i+2:], "*/"); k >= 0 {
				i += k + 3
			} else {
				i = n
			}
		case c == ';':
			if strings.TrimSpace(q[i+1:]) != "" {
				return errors.New("only one SQL statement per call is allowed")
			}
		}
	}
	return nil
}

func openAdmin(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	return db, db.Ping()
}

// Migrate brings the database at path to the given schema, creating the file
// if needed. It only adds tables and columns; nothing is ever dropped. Seed
// rows are inserted into tables created by this call. It returns a list of
// the changes applied.
func Migrate(path string, tables []appdef.Table, seed map[string][]map[string]any) ([]string, error) {
	db, err := openAdmin(path)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	if err := upgradePlatformTables(tx); err != nil {
		return nil, fmt.Errorf("platform tables: %w", err)
	}
	var applied []string
	for _, t := range tables {
		existing, err := columnsOf(tx, t.Name)
		if err != nil {
			return nil, err
		}
		if len(existing) == 0 {
			defs := []string{"id INTEGER PRIMARY KEY", "created_at TEXT NOT NULL DEFAULT (datetime('now'))"}
			for _, c := range t.Columns {
				def := quote(c.Name) + " " + appdef.ColumnTypes[c.Type]
				if c.Required {
					def += " NOT NULL"
				}
				if c.Unique {
					def += " UNIQUE"
				}
				defs = append(defs, def)
			}
			if _, err := tx.Exec("CREATE TABLE " + quote(t.Name) + " (" + strings.Join(defs, ", ") + ")"); err != nil {
				return nil, fmt.Errorf("create table %s: %w", t.Name, err)
			}
			applied = append(applied, "created table "+t.Name)
			for i, row := range seed[t.Name] {
				if err := insertRow(tx, t.Name, row); err != nil {
					return nil, fmt.Errorf("seed %s row %d: %w", t.Name, i, err)
				}
			}
			if n := len(seed[t.Name]); n > 0 {
				applied = append(applied, fmt.Sprintf("seeded %s with %d rows", t.Name, n))
			}
			continue
		}
		for _, c := range t.Columns {
			if existing[c.Name] {
				continue
			}
			// Added columns are always nullable: existing rows have no value for them.
			if _, err := tx.Exec("ALTER TABLE " + quote(t.Name) + " ADD COLUMN " + quote(c.Name) + " " + appdef.ColumnTypes[c.Type]); err != nil {
				return nil, fmt.Errorf("add column %s.%s: %w", t.Name, c.Name, err)
			}
			applied = append(applied, "added column "+t.Name+"."+c.Name)
		}
	}
	return applied, tx.Commit()
}

// usersTable is the current rx_users definition. A visitor has an email and
// password, a WhatsApp number, or both.
const usersTable = `CREATE TABLE IF NOT EXISTS rx_users (id INTEGER PRIMARY KEY, email TEXT UNIQUE, phone TEXT UNIQUE,
	name TEXT NOT NULL DEFAULT '', password_hash TEXT, verified_at TEXT, created_at TEXT NOT NULL DEFAULT (datetime('now')))`

// platformTables hold the app's visitor accounts. Scripts cannot reach them.
var platformTables = []string{
	usersTable,
	`CREATE TABLE IF NOT EXISTS rx_sessions (hash TEXT PRIMARY KEY, user_id INTEGER NOT NULL REFERENCES rx_users(id) ON DELETE CASCADE,
		expires_at INTEGER NOT NULL)`,
	`CREATE TABLE IF NOT EXISTS rx_email_tokens (hash TEXT PRIMARY KEY, user_id INTEGER NOT NULL REFERENCES rx_users(id) ON DELETE CASCADE,
		purpose TEXT NOT NULL, expires_at INTEGER NOT NULL, used INTEGER NOT NULL DEFAULT 0)`,
	`CREATE TABLE IF NOT EXISTS rx_phone_codes (phone TEXT PRIMARY KEY, code_hash TEXT NOT NULL, expires_at INTEGER NOT NULL,
		attempts INTEGER NOT NULL DEFAULT 0)`,
}

type execer interface {
	Exec(query string, args ...any) (sql.Result, error)
	QueryRow(query string, args ...any) *sql.Row
}

// upgradePlatformTables creates the rx_ tables and rebuilds an rx_users
// table from before phone sign-in, keeping its rows. Foreign keys must be
// off on the connection, or dropping the old table would delete sessions.
func upgradePlatformTables(db execer) error {
	var def string
	err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'rx_users'`).Scan(&def)
	if err == nil && !strings.Contains(def, "phone TEXT") {
		for _, stmt := range []string{
			strings.Replace(usersTable, "IF NOT EXISTS rx_users", "rx_users_new", 1),
			`INSERT INTO rx_users_new (id, email, name, password_hash, verified_at, created_at)
				SELECT id, email, name, password_hash, verified_at, created_at FROM rx_users`,
			`DROP TABLE rx_users`,
			`ALTER TABLE rx_users_new RENAME TO rx_users`,
		} {
			if _, err := db.Exec(stmt); err != nil {
				return fmt.Errorf("upgrade rx_users: %w", err)
			}
		}
	}
	for _, stmt := range platformTables {
		if _, err := db.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}

// EnsurePlatformTables brings the rx_ tables of the database at path up to
// date, for databases created by an older platform version.
func EnsurePlatformTables(path string) error {
	db, err := openAdmin(path) // foreign keys off, as the upgrade needs
	if err != nil {
		return err
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := upgradePlatformTables(tx); err != nil {
		return err
	}
	return tx.Commit()
}

// OpenAdmin opens a database for platform code, without the script
// restrictions. Never hand it to a script.
func OpenAdmin(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(2)
	return db, db.Ping()
}

func columnsOf(tx *sql.Tx, table string) (map[string]bool, error) {
	rows, err := tx.Query("SELECT name FROM pragma_table_info(?)", table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		cols[name] = true
	}
	return cols, rows.Err()
}

func insertRow(tx *sql.Tx, table string, row map[string]any) error {
	if len(row) == 0 {
		return nil
	}
	cols := make([]string, 0, len(row))
	for c := range row {
		cols = append(cols, c)
	}
	sort.Strings(cols)
	quoted := make([]string, len(cols))
	args := make([]any, len(cols))
	for i, c := range cols {
		quoted[i] = quote(c)
		args[i] = row[c]
	}
	marks := strings.TrimSuffix(strings.Repeat("?,", len(cols)), ",")
	_, err := tx.Exec("INSERT INTO "+quote(table)+" ("+strings.Join(quoted, ",")+") VALUES ("+marks+")", args...)
	return err
}

// Snapshot writes a consistent copy of the database at src to dst.
func Snapshot(src, dst string) error {
	if err := os.Remove(dst); err != nil && !os.IsNotExist(err) {
		return err
	}
	db, err := openAdmin(src)
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.Exec("VACUUM INTO ?", dst)
	return err
}

// quote wraps an identifier already validated by appdef.ValidIdent.
func quote(ident string) string { return `"` + ident + `"` }
