package accounts

import (
	"path/filepath"
	"testing"
	"time"
)

func TestCleanupDeletesExpiredRows(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "accounts.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	u, err := s.CreateUser("a@example.com", "correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	s.CreateSession(u.ID)
	s.CreateEmailToken(u.ID, PurposeReset)
	count := func(table string) (n int) {
		s.db.QueryRow(`SELECT count(*) FROM ` + table).Scan(&n)
		return n
	}
	s.Cleanup()
	if count("sessions") != 1 || count("email_tokens") != 1 {
		t.Fatal("live rows were deleted")
	}
	s.now = func() time.Time { return time.Now().Add(SessionTTL + time.Hour) }
	s.Cleanup()
	if count("sessions") != 0 || count("email_tokens") != 0 {
		t.Fatalf("expired rows kept: %d sessions, %d tokens", count("sessions"), count("email_tokens"))
	}
}
