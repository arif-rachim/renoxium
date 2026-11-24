package accounts

import (
	"crypto/sha256"
	"encoding/base64"
	"path/filepath"
	"testing"
	"time"
)

func connectAgent(t *testing.T, s *Store, userID string) {
	t.Helper()
	c, err := s.RegisterClient("Claude", []string{"https://claude.ai/cb"})
	if err != nil {
		t.Fatal(err)
	}
	verifier := "0123456789012345678901234567890123456789abcdef"
	sum := sha256.Sum256([]byte(verifier))
	code, err := s.CreateCode(c.ID, userID, "https://claude.ai/cb", base64.RawURLEncoding.EncodeToString(sum[:]), "apps")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ExchangeCode(code, c.ID, "https://claude.ai/cb", verifier); err != nil {
		t.Fatal(err)
	}
}

func growthOf(t *testing.T, s *Store, id string) GrowthAccount {
	t.Helper()
	list, err := s.GrowthAccounts()
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range list {
		if a.ID == id {
			return a
		}
	}
	t.Fatalf("no account %s", id)
	return GrowthAccount{}
}

func TestAgentConnectedIsRecordedOnce(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "accounts.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	u, _ := s.CreateUser("a@example.com", "correct horse battery")
	if !growthOf(t, s, u.ID).Agent.IsZero() {
		t.Fatal("agent before connecting")
	}
	first := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return first }
	connectAgent(t, s, u.ID)
	s.now = func() time.Time { return first.Add(48 * time.Hour) }
	connectAgent(t, s, u.ID)
	if got := growthOf(t, s, u.ID).Agent; !got.Equal(first) {
		t.Fatalf("agent connected at %v, want the first grant", got)
	}
}

func TestAgentBackfillFromTokens(t *testing.T) {
	path := filepath.Join(t.TempDir(), "accounts.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	u, _ := s.CreateUser("a@example.com", "correct horse battery")
	issued := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	s.now = func() time.Time { return issued }
	connectAgent(t, s, u.ID)
	// A database from before the column existed: only the token is left.
	s.db.Exec(`UPDATE users SET agent_connected_at = NULL`)
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if got := growthOf(t, s, u.ID).Agent; !got.Equal(issued) {
		t.Fatalf("backfilled %v, want %v", got, issued)
	}
}

// Paid periods of the former Pro and Business plans stay as history: a
// grant or the account's deletion closes an open one, and nothing opens a
// new one (saving a card charges nothing).
func TestPaidPeriods(t *testing.T) {
	path := filepath.Join(t.TempDir(), "accounts.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	clock := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return clock }
	u, _ := s.CreateUser("a@example.com", "correct horse battery")
	g, _ := s.CreateUser("b@example.com", "correct horse battery")
	for _, id := range []string{u.ID, g.ID} {
		if _, err := s.db.Exec(`INSERT INTO paid_periods (user_id, plan, started_at) VALUES (?, 'pro', ?)`, id, clock.Format(time.RFC3339)); err != nil {
			t.Fatal(err)
		}
	}
	clock = clock.AddDate(0, 1, 0)
	s.SetCard(u.ID, Card{Brand: "visa", Last4: "4242", ExpMonth: 12, ExpYear: 2030})
	s.GrantPlan(g.ID, PlanMember)
	clock = clock.AddDate(0, 1, 0)
	if err := s.DeleteUser(u.ID); err != nil {
		t.Fatal(err)
	}
	list, err := s.PaidPeriods()
	if err != nil || len(list) != 2 {
		t.Fatalf("periods %+v %v", list, err)
	}
	feb, mar := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	for _, p := range list {
		want := map[string]time.Time{u.ID: mar, g.ID: feb}[p.UserID]
		if p.Plan != "pro" || !p.End.Equal(want) {
			t.Errorf("period %+v, want end %v", p, want)
		}
	}
}
