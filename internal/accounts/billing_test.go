package accounts

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// A saved card makes the account a member; removing it, or its expiry,
// makes it free again. Saving a card is all it takes.
func TestCardMakesAMember(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "accounts.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	clock := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return clock }
	u, _ := s.CreateUser("card@example.com", "correct horse battery")
	if p := s.Plan(u.ID); p != PlanFree {
		t.Fatalf("new account %s", p)
	}
	if err := s.SetCard(u.ID, Card{Brand: "visa", Last4: "4242", ExpMonth: 12, ExpYear: 2026}); err != nil {
		t.Fatal(err)
	}
	b, _ := s.Billing(u.ID)
	if b.Plan != PlanMember || s.OwnerPlan(u.ID) != PlanMember || b.CardLast4 != "4242" || !b.CardExpires.Equal(time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("with a card: %+v", b)
	}
	// The card works through its expiry month, then the account is free.
	clock = time.Date(2026, 12, 31, 23, 59, 0, 0, time.UTC)
	if p := s.Plan(u.ID); p != PlanMember {
		t.Fatalf("last day of the expiry month: %s", p)
	}
	clock = time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	if p := s.Plan(u.ID); p != PlanFree {
		t.Fatalf("expired card: %s", p)
	}
	if _, perPlan, _, _ := s.PlanCounts(); perPlan[PlanFree] != 1 || perPlan[PlanMember] != 0 {
		t.Fatalf("counts with an expired card %v", perPlan)
	}
	// A card saved already expired does not count.
	s.SetCard(u.ID, Card{Brand: "visa", Last4: "0000", ExpMonth: 1, ExpYear: 2020})
	if p := s.Plan(u.ID); p != PlanFree {
		t.Fatalf("card saved expired: %s", p)
	}
	s.SetCard(u.ID, Card{Brand: "mastercard", Last4: "4444", ExpMonth: 6, ExpYear: 2030})
	if p := s.Plan(u.ID); p != PlanMember {
		t.Fatalf("replaced card: %s", p)
	}
	rows, _ := s.Users("", 10)
	if len(rows) != 1 || rows[0].Plan != PlanMember {
		t.Fatalf("operator list %+v", rows)
	}
	// Removed.
	s.SetCard(u.ID, Card{})
	if b, _ := s.Billing(u.ID); b.Plan != PlanFree || b.CardLast4 != "" || !b.CardExpires.IsZero() {
		t.Fatalf("card removed: %+v", b)
	}
}

// A grant makes a member by agreement, whatever the card; the platform sees
// it as granted.
func TestGrantedMember(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "accounts.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	u, _ := s.CreateUser("grant@example.com", "correct horse battery")
	if err := s.GrantPlan(u.ID, PlanMember); err != nil {
		t.Fatal(err)
	}
	if s.Plan(u.ID) != PlanMember || s.OwnerPlan(u.ID) != PlanGranted {
		t.Fatalf("granted: %s %s", s.Plan(u.ID), s.OwnerPlan(u.ID))
	}
	if _, perPlan, granted, _ := s.PlanCounts(); perPlan[PlanMember] != 1 || granted != 1 {
		t.Fatalf("counts %v %d", perPlan, granted)
	}
	// Older self-host licences name the former plans.
	for _, legacy := range []string{"pro", "business"} {
		if err := s.GrantPlan(u.ID, legacy); err != nil || s.OwnerPlan(u.ID) != PlanGranted {
			t.Fatalf("grant %s: %v %s", legacy, err, s.OwnerPlan(u.ID))
		}
	}
	if err := s.GrantPlan(u.ID, "gold"); err == nil {
		t.Fatal("unknown plan accepted")
	}
	if err := s.GrantPlan(u.ID, PlanFree); err != nil || s.OwnerPlan(u.ID) != PlanFree {
		t.Fatalf("grant removed: %v %s", err, s.OwnerPlan(u.ID))
	}
}

// A database from before card membership: business/granted rows become
// member + granted, and opening it again changes nothing more.
func TestMigrateOldPlans(t *testing.T) {
	path := filepath.Join(t.TempDir(), "accounts.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	g, _ := s.CreateUser("ops@example.com", "correct horse battery")
	f, _ := s.CreateUser("free@example.com", "correct horse battery")
	s.Close()

	// The billing table as it was, without the card columns.
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`DROP TABLE billing`,
		`CREATE TABLE billing (user_id TEXT PRIMARY KEY REFERENCES users(id), customer TEXT UNIQUE,
			plan TEXT NOT NULL DEFAULT 'free', status TEXT NOT NULL DEFAULT '', subscription TEXT NOT NULL DEFAULT '',
			period_end INTEGER NOT NULL DEFAULT 0, cancel_at_period_end INTEGER NOT NULL DEFAULT 0, updated_at TEXT NOT NULL)`,
		`INSERT INTO billing (user_id, customer, plan, status, updated_at) VALUES ('` + g.ID + `', 'cus_ops', 'business', 'granted', '2026-01-01T00:00:00Z')`,
		`INSERT INTO billing (user_id, customer, plan, status, updated_at) VALUES ('` + f.ID + `', 'cus_free', 'free', '', '2026-01-01T00:00:00Z')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(q, err)
		}
	}
	db.Close()

	for range 2 { // idempotent
		s, err = Open(path)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := s.Billing(g.ID)
		if b.Plan != PlanMember || b.Status != StatusGranted || b.Customer != "cus_ops" || s.OwnerPlan(g.ID) != PlanGranted {
			t.Fatalf("migrated grant: %+v", b)
		}
		var stored string
		s.db.QueryRow(`SELECT plan FROM billing WHERE user_id = ?`, g.ID).Scan(&stored)
		if stored != PlanMember {
			t.Fatalf("stored plan %q", stored)
		}
		if s.OwnerPlan(f.ID) != PlanFree {
			t.Fatalf("free account: %s", s.OwnerPlan(f.ID))
		}
		if list, _ := s.PaidPeriods(); len(list) != 0 {
			t.Fatalf("a grant became a paid period: %+v", list)
		}
		s.Close()
	}
}
