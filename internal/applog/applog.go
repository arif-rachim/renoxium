// Package applog stores one size-capped event log per app, covering server,
// browser and job events, in its own SQLite file away from app data.
package applog

import (
	"database/sql"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

const (
	maxEvents  = 5000
	maxMessage = 4000
)

// Event is one log entry.
type Event struct {
	ID        int64  `json:"id"`
	Time      string `json:"time" jsonschema:"when, in UTC (RFC 3339, ending in Z)"`
	LocalTime string `json:"local_time,omitempty" jsonschema:"the same moment in the app's time zone, with its offset, e.g. 2026-11-29 00:01 +04:00 Asia/Dubai: use this when telling the user a time"`
	Stage     string `json:"stage"`
	Version   int    `json:"version"`
	Source    string `json:"source" jsonschema:"server or browser"`
	Level     string `json:"level" jsonschema:"info, warn or error"`
	RequestID string `json:"request_id,omitempty"`
	Message   string `json:"message"`
}

// Filter narrows a Query. Zero values mean "any".
type Filter struct {
	Stage        string
	Level        string
	Source       string
	SinceVersion int
	RequestID    string
	Limit        int
}

// Log is an open event log.
type Log struct {
	db *sql.DB
	mu sync.Mutex
	n  int
}

// Open opens or creates the log at path.
func Open(path string) (*Log, error) {
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS events (
		id INTEGER PRIMARY KEY, time TEXT NOT NULL, stage TEXT NOT NULL, version INTEGER NOT NULL,
		source TEXT NOT NULL, level TEXT NOT NULL, request_id TEXT NOT NULL, message TEXT NOT NULL)`)
	if err != nil {
		db.Close()
		return nil, err
	}
	return &Log{db: db}, nil
}

// Close closes the log.
func (l *Log) Close() error { return l.db.Close() }

// Add appends an event. Failures are dropped: logging must never break a request.
func (l *Log) Add(e Event) {
	if len(e.Message) > maxMessage {
		e.Message = e.Message[:maxMessage] + "…"
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.db.Exec(`INSERT INTO events (time, stage, version, source, level, request_id, message) VALUES (?,?,?,?,?,?,?)`,
		time.Now().UTC().Format(time.RFC3339Nano), e.Stage, e.Version, e.Source, e.Level, e.RequestID, e.Message)
	if l.n++; l.n%200 == 0 {
		l.db.Exec(`DELETE FROM events WHERE id <= (SELECT max(id) FROM events) - ?`, maxEvents)
	}
}

// Query returns matching events, newest first.
func (l *Log) Query(f Filter) ([]Event, error) {
	var where []string
	var args []any
	cond := func(expr string, v any) { where = append(where, expr); args = append(args, v) }
	if f.Stage != "" {
		cond("stage = ?", f.Stage)
	}
	if f.Level != "" {
		cond("level = ?", f.Level)
	}
	if f.Source != "" {
		cond("source = ?", f.Source)
	}
	if f.SinceVersion > 0 {
		cond("version >= ?", f.SinceVersion)
	}
	if f.RequestID != "" {
		cond("request_id = ?", f.RequestID)
	}
	q := "SELECT id, time, stage, version, source, level, request_id, message FROM events"
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	if f.Limit <= 0 || f.Limit > 200 {
		f.Limit = 50
	}
	q += " ORDER BY id DESC LIMIT ?"
	args = append(args, f.Limit)
	rows, err := l.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.ID, &e.Time, &e.Stage, &e.Version, &e.Source, &e.Level, &e.RequestID, &e.Message); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
