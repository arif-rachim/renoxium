package accounts

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// Accounts from before activity was recorded start from their latest
// session or agent token, or their sign-up.
func TestActivityBackfillOnUpgrade(t *testing.T) {
	path := filepath.Join(t.TempDir(), "accounts.db")
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	signup := time.Date(2025, 1, 10, 9, 0, 0, 0, time.UTC)
	session := time.Date(2025, 8, 1, 9, 0, 0, 0, time.UTC)
	for _, stmt := range []string{
		`CREATE TABLE users (id TEXT PRIMARY KEY, email TEXT NOT NULL UNIQUE, password_hash TEXT NOT NULL, created_at TEXT NOT NULL)`,
		`CREATE TABLE sessions (hash TEXT PRIMARY KEY, user_id TEXT NOT NULL REFERENCES users(id), expires_at INTEGER NOT NULL)`,
		`INSERT INTO users VALUES ('usr_old', 'old@example.com', 'x', '` + signup.Format(time.RFC3339) + `')`,
		`INSERT INTO users VALUES ('usr_seen', 'seen@example.com', 'x', '` + signup.Format(time.RFC3339) + `')`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	db.Exec(`INSERT INTO sessions VALUES ('h', 'usr_seen', ?)`, session.Add(SessionTTL).Unix())
	db.Close()

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for id, want := range map[string]time.Time{"usr_old": signup, "usr_seen": session} {
		a, err := s.Activity(id)
		if err != nil || !a.ActiveAt.Equal(want) || a.Deactivated() {
			t.Fatalf("%s: %+v %v, want active at %v", id, a, err, want)
		}
	}
	// Reopening does not backfill again.
	s.db.Exec(`UPDATE users SET active_at = 5 WHERE id = 'usr_old'`)
	s.Close()
	s, _ = Open(path)
	if a, _ := s.Activity("usr_old"); a.ActiveAt.Unix() != 5 {
		t.Fatalf("backfilled again: %v", a.ActiveAt)
	}
}

func TestNoteActivityClearsTheWarningButNotDeactivation(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "accounts.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s.SetClock(func() time.Time { return now })
	u, _ := s.CreateUser("a@example.com", "correct horse battery")
	if s.NoteActivity(u.ID) {
		t.Fatal("wrote again within the hour")
	}
	now = now.AddDate(0, 5, 2)
	a, _ := s.Activity(u.ID)
	if ok, _ := s.MarkWarned(u.ID, a.ActiveAt, now.AddDate(0, 0, WarnDays)); !ok {
		t.Fatal("not warned")
	}
	if ok, _ := s.MarkWarned(u.ID, a.ActiveAt, now.AddDate(0, 0, WarnDays)); ok {
		t.Fatal("warned twice")
	}
	if ok, _ := s.Deactivate(u.ID); ok {
		t.Fatal("deactivated before the promised date")
	}
	now = now.AddDate(0, 0, WarnDays)
	if ok, _ := s.Deactivate(u.ID); !ok || !s.IsDeactivated(u.ID) {
		t.Fatal("not deactivated on the promised date")
	}
	// Activity is recorded, but only Reactivate brings the account back.
	now = now.Add(2 * time.Hour)
	if !s.NoteActivity(u.ID) || !s.IsDeactivated(u.ID) {
		t.Fatal("activity reactivated the account by itself")
	}
	if err := s.Reactivate(u.ID); err != nil || s.IsDeactivated(u.ID) {
		t.Fatal("not reactivated", err)
	}
	// A warning followed by activity cannot deactivate.
	now = now.AddDate(0, 6, 0)
	a, _ = s.Activity(u.ID)
	s.MarkWarned(u.ID, a.ActiveAt, now)
	s.NoteActivity(u.ID)
	if ok, _ := s.Deactivate(u.ID); ok {
		t.Fatal("deactivated after activity")
	}
}
