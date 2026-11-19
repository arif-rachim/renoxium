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

func TestPaidPeriods(t *testing.T) {
	path := filepath.Join(t.TempDir(), "accounts.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	clock := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return clock }
	u, _ := s.CreateUser("a@example.com", "correct horse battery")
	s.SetCustomer(u.ID, "cus_1")
	s.SetSubscription(u.ID, Billing{Plan: PlanPro, Status: "active", Subscription: "sub_1"})
	clock = clock.AddDate(0, 1, 0)
	s.SetSubscription(u.ID, Billing{Plan: PlanBusiness, Status: "active", Subscription: "sub_1"}) // upgrade: same period
	clock = clock.AddDate(0, 1, 0)
	s.SetSubscription(u.ID, Billing{Plan: PlanFree, Status: "canceled"})
	clock = clock.AddDate(0, 1, 0)
	s.SetSubscription(u.ID, Billing{Plan: PlanPro, Status: "active", Subscription: "sub_2"})
	clock = clock.AddDate(0, 1, 0)
	s.GrantPlan(u.ID, PlanBusiness) // a grant is not paying

	granted, _ := s.CreateUser("b@example.com", "correct horse battery")
	s.GrantPlan(granted.ID, PlanPro)

	list, err := s.PaidPeriods()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("periods %+v", list)
	}
	jan, mar, apr, may := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	byStart := map[time.Time]PaidPeriod{}
	for _, p := range list {
		byStart[p.Start] = p
	}
	if p := byStart[jan]; p.Plan != PlanBusiness || !p.End.Equal(mar) {
		t.Errorf("first period %+v", p)
	}
	if p := byStart[apr]; p.Plan != PlanPro || !p.End.Equal(may) {
		t.Errorf("second period %+v", p)
	}

	// Paying again, then the account is deleted: the period closes and stays.
	s.SetSubscription(u.ID, Billing{Plan: PlanPro, Status: "active", Subscription: "sub_3"})
	clock = clock.AddDate(0, 1, 0)
	if err := s.DeleteUser(u.ID); err != nil {
		t.Fatal(err)
	}
	list, _ = s.PaidPeriods()
	if len(list) != 3 {
		t.Fatalf("after delete %+v", list)
	}
	for _, p := range list {
		if p.End.IsZero() {
			t.Errorf("open period after delete: %+v", p)
		}
	}

	// Accounts paying before paid periods existed are backfilled.
	c, _ := s.CreateUser("c@example.com", "correct horse battery")
	s.SetCustomer(c.ID, "cus_c")
	s.SetSubscription(c.ID, Billing{Plan: PlanPro, Status: "active", Subscription: "sub_c"})
	s.db.Exec(`DELETE FROM paid_periods WHERE user_id = ?`, c.ID)
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	list, _ = s.PaidPeriods()
	found := false
	for _, p := range list {
		if p.UserID == c.ID && p.End.IsZero() && p.Plan == PlanPro {
			found = true
		}
		if p.UserID == granted.ID {
			t.Error("granted plan backfilled as paying")
		}
	}
	if !found {
		t.Fatalf("not backfilled: %+v", list)
	}
}
