package accounts

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func openTest(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "accounts.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestOlderDatabaseGetsOptionalEmail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "accounts.db")
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`CREATE TABLE users (id TEXT PRIMARY KEY, email TEXT NOT NULL UNIQUE, password_hash TEXT NOT NULL, created_at TEXT NOT NULL)`,
		`ALTER TABLE users ADD COLUMN verified_at TEXT`,
		`CREATE TABLE sessions (hash TEXT PRIMARY KEY, user_id TEXT NOT NULL REFERENCES users(id), expires_at INTEGER NOT NULL)`,
		`INSERT INTO users (id, email, password_hash, created_at, verified_at) VALUES ('usr_old', 'old@example.com', 'x', '2026-01-01T00:00:00Z', '2026-01-02T00:00:00Z')`,
		`INSERT INTO sessions VALUES ('h', 'usr_old', 9999999999)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(stmt, err)
		}
	}
	db.Close()

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	u, err := s.User("usr_old")
	if err != nil || u.Email != "old@example.com" || !u.Verified || !u.HasPassword || u.Phone != "" {
		t.Fatalf("old account after migration: %+v, %v", u, err)
	}
	var n int
	s.db.QueryRow(`SELECT count(*) FROM sessions`).Scan(&n)
	if n != 1 {
		t.Fatal("sessions lost")
	}
	if _, _, err := s.PhoneUser("+971501234567"); err != nil {
		t.Fatal("phone account in migrated database:", err)
	}
	// Opening again does not rebuild or fail.
	s.Close()
	if s, err = Open(path); err != nil {
		t.Fatal(err)
	}
}

func TestPhoneCodes(t *testing.T) {
	s := openTest(t)
	const phone = "+971501234567"
	code, err := s.NewPhoneCode(phone, PhoneSignIn, "")
	if err != nil || len(code) != 6 {
		t.Fatal(code, err)
	}
	if _, err := s.UsePhoneCode(phone, PhoneConfirm, code); !errors.Is(err, ErrCodeGone) {
		t.Fatal("a code of another purpose worked:", err)
	}
	wrong := "000000"
	if code == wrong {
		wrong = "111111"
	}
	if _, err := s.UsePhoneCode(phone, PhoneSignIn, wrong); !errors.Is(err, ErrCodeWrong) {
		t.Fatal(err)
	}
	if _, err := s.UsePhoneCode(phone, PhoneSignIn, " "+code+" "); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UsePhoneCode(phone, PhoneSignIn, code); !errors.Is(err, ErrCodeGone) {
		t.Fatal("a code worked twice:", err)
	}

	// Five guesses, then even the right code fails.
	code, _ = s.NewPhoneCode(phone, PhoneLink, "usr_x")
	for range 5 {
		s.UsePhoneCode(phone, PhoneLink, "bad")
	}
	if _, err := s.UsePhoneCode(phone, PhoneLink, code); !errors.Is(err, ErrCodeGone) {
		t.Fatal("more than five guesses allowed:", err)
	}

	// Codes expire, and carry the account they were made for.
	code, _ = s.NewPhoneCode(phone, PhoneLink, "usr_x")
	if id, err := s.UsePhoneCode(phone, PhoneLink, code); err != nil || id != "usr_x" {
		t.Fatal(id, err)
	}
	code, _ = s.NewPhoneCode(phone, PhoneSignIn, "")
	s.now = func() time.Time { return time.Now().Add(PhoneCodeTTL + time.Minute) }
	if _, err := s.UsePhoneCode(phone, PhoneSignIn, code); !errors.Is(err, ErrCodeGone) {
		t.Fatal("expired code worked:", err)
	}
}

func TestPhoneAccounts(t *testing.T) {
	s := openTest(t)
	const phone = "+971501234567"
	u, created, err := s.PhoneUser(phone)
	if err != nil || !created || u.Email != "" || u.HasPassword || u.Verified || u.Phone != phone || u.Label() != phone {
		t.Fatalf("%+v %v %v", u, created, err)
	}
	again, created, err := s.PhoneUser(phone)
	if err != nil || created || again.ID != u.ID {
		t.Fatal("second sign-in made another account", err)
	}
	// Two accounts without email can exist.
	if _, _, err := s.PhoneUser("+971509999999"); err != nil {
		t.Fatal(err)
	}
	// No password: email sign-in never matches it.
	if _, err := s.Authenticate("", ""); !errors.Is(err, ErrBadCredentials) {
		t.Fatal(err)
	}
	if err := s.UnlinkPhone(u.ID); !errors.Is(err, ErrLastSignIn) {
		t.Fatal("removed the only way to sign in:", err)
	}

	other, err := s.CreateUser("b@example.com", "correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddEmail(u.ID, "B@example.com", "correct horse battery"); !errors.Is(err, ErrEmailTaken) {
		t.Fatal(err)
	}
	u, err = s.AddEmail(u.ID, "a@example.com", "correct horse battery")
	if err != nil || u.Email != "a@example.com" || !u.HasPassword || u.Verified || u.Label() != "a@example.com" {
		t.Fatalf("%+v %v", u, err)
	}
	if _, err := s.AddEmail(u.ID, "c@example.com", "correct horse battery"); !errors.Is(err, ErrHasEmail) {
		t.Fatal(err)
	}
	if _, err := s.Authenticate("a@example.com", "correct horse battery"); err != nil {
		t.Fatal(err)
	}

	if err := s.LinkPhone(other.ID, phone); !errors.Is(err, ErrPhoneTaken) {
		t.Fatal(err)
	}
	if err := s.LinkPhone(other.ID, "+971508888888"); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.UserByPhone("+971508888888"); got.ID != other.ID {
		t.Fatal("linked number does not find the account")
	}
	if err := s.UnlinkPhone(other.ID); err != nil {
		t.Fatal(err)
	}
	if rows, err := s.Users("+97150", 10); err != nil || len(rows) != 2 {
		t.Fatalf("operator search by number: %v %v", rows, err)
	}
	if err := s.DeleteUser(u.ID); err != nil {
		t.Fatal(err)
	}
}
