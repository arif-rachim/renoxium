package growth

import (
	"math"
	"testing"
	"time"
)

var now = time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)

func day(n int) time.Time { return now.AddDate(0, 0, -n) }

// owner builds a timeline from days-ago offsets; -1 means not reached.
func owner(id string, ago ...int) Owner {
	o := Owner{ID: id}
	for i, a := range ago {
		if a >= 0 {
			o.At[i] = day(a)
		}
	}
	return o
}

func TestFunnelCountsAndMedians(t *testing.T) {
	owners := []Owner{
		owner("a", 30, 30, 29, 29, 28, 20),
		owner("b", 30, 29, 27, 27, 26, -1),
		owner("c", 20, 19, -1, -1, -1, -1),
		owner("d", 10, -1, 9, 9, -1, -1), // agent connected before confirming
	}
	rows := Funnel(owners, now)
	want := []struct{ count, base, reached int }{
		{4, 4, 4}, {3, 4, 3}, {3, 3, 2}, {3, 3, 3}, {2, 3, 2}, {1, 2, 1}, {0, 1, 0}, {0, 0, 0},
	}
	for i, w := range want {
		r := rows[i]
		if r.Count != w.count || r.Base != w.base || r.Reached != w.reached {
			t.Errorf("%s: count %d base %d reached %d, want %+v", r.Name, r.Count, r.Base, r.Reached, w)
		}
	}
	if rows[Confirmed].OfPrev != 75 || rows[AppCreated].OfSignups != 75 {
		t.Errorf("percentages: %v %v", rows[Confirmed].OfPrev, rows[AppCreated].OfSignups)
	}
	// Confirmed: a 0 days, b 1 day, c 1 day -> median 1 day.
	if !rows[Confirmed].HasMedian || rows[Confirmed].Median != 24*time.Hour {
		t.Errorf("confirm median %v", rows[Confirmed].Median)
	}
	// Agent: a 1 day, b 2 days -> median 1.5 days (d has no confirmation).
	if rows[AgentConnected].Median != 36*time.Hour {
		t.Errorf("agent median %v", rows[AgentConnected].Median)
	}
	if rows[CustomerActivity].Median != 8*24*time.Hour {
		t.Errorf("activity median %v", rows[CustomerActivity].Median)
	}
	if rows[PaidStarted].HasMedian || rows[RetainedM3].OfPrev != -1 {
		t.Errorf("empty stages: %+v %+v", rows[PaidStarted], rows[RetainedM3])
	}
	if got := Funnel(nil, now); got[SignedUp].OfSignups != -1 || got[Confirmed].OfPrev != -1 {
		t.Errorf("empty funnel: %+v", got[:2])
	}
}

func TestCohort(t *testing.T) {
	owners := []Owner{owner("a", 0), owner("b", 40), owner("c", 1)}
	if got := Cohort(owners, "2026-11"); len(got) != 2 {
		t.Fatalf("november cohort: %d", len(got))
	}
	if got := Cohort(owners, ""); len(got) != 3 {
		t.Fatalf("all time: %d", len(got))
	}
}

func TestPaidAndRetention(t *testing.T) {
	owners := []Owner{owner("kept", 200), owner("left", 200), owner("young", 50), owner("back", 300), owner("free", 10)}
	periods := []Period{
		{Owner: "kept", Plan: "pro", Start: day(150)},
		{Owner: "left", Plan: "pro", Start: day(150), End: day(100)},
		{Owner: "young", Plan: "business", Start: day(40)},
		// Cancelled, then came back before day 90 of the first start.
		{Owner: "back", Plan: "pro", Start: day(250), End: day(220)},
		{Owner: "back", Plan: "pro", Start: day(200)},
	}
	ApplyPaid(owners, periods, now)
	if !owners[0].At[PaidStarted].Equal(day(150)) || owners[0].At[RetainedM3].IsZero() {
		t.Errorf("kept: %v", owners[0].At)
	}
	if !owners[1].At[RetainedM3].IsZero() {
		t.Error("left counted as retained")
	}
	if !owners[2].At[RetainedM3].IsZero() || owners[2].At[PaidStarted].IsZero() {
		t.Error("young")
	}
	if !owners[3].At[PaidStarted].Equal(day(250)) || owners[3].At[RetainedM3].IsZero() {
		t.Errorf("back: %v", owners[3].At)
	}
	// Retention is measured only for accounts that started paying 90+ days ago.
	for i := range owners {
		for s := Confirmed; s < PaidStarted; s++ {
			owners[i].At[s] = owners[i].At[SignedUp]
		}
	}
	owners[4].At[PaidStarted] = time.Time{}
	r := Funnel(owners, now)[RetainedM3]
	if r.Base != 3 || r.Reached != 2 || r.Count != 2 || math.Abs(r.OfPrev-66.666) > 0.01 {
		t.Errorf("retention row %+v", r)
	}
}

func TestWeeklyAndTemplates(t *testing.T) {
	if ws := WeekStart(time.Date(2026, 11, 22, 23, 0, 0, 0, time.UTC)); ws.Weekday() != time.Monday || ws.Day() != 16 {
		t.Fatalf("week start %v", ws)
	}
	o1 := owner("a", 0, 0, 0, 0, 0, 0)
	o1.Template = "store-uae"
	o2 := owner("b", 8, -1, -1, -1, 8)
	o2.Template = "store-uae"
	o3 := owner("c", 100, -1, -1, -1, 100)
	weeks := Weekly([]Owner{o1, o2, o3}, now, 12)
	if len(weeks) != 12 || weeks[11].Signups != 1 || weeks[11].Activity != 1 || weeks[10].Published != 1 {
		t.Fatalf("weeks %+v", weeks[10:])
	}
	total := 0
	for _, w := range weeks {
		total += w.Signups
	}
	if total != 2 {
		t.Errorf("an old sign-up was counted: %d", total)
	}
	tr := ByTemplate([]Owner{o1, o2, o3})
	if len(tr) != 2 || tr[0] != (TemplateRow{"store-uae", 2, 1}) || tr[1] != (TemplateRow{"custom", 1, 0}) {
		t.Errorf("templates %+v", tr)
	}
}

func TestEconomics(t *testing.T) {
	prices := map[string]int{"pro": 99, "business": 299}
	in := EconomicsInput{Now: now, Prices: prices, Paying: map[string]int{"pro": 3, "business": 1}, GrossMarginPct: 80}
	e := Compute(in)
	if e.MRR != 3*99+299 || e.PayingAccounts != 4 || e.ARPA != 149 {
		t.Fatalf("mrr %+v", e)
	}
	if e.LTVKnown || e.LTVWhy == "" || e.CACKnown || e.RatioOK {
		t.Fatalf("not enough data: %+v", e)
	}

	// Ten paying 30 days ago, two cancelled since; one new this month.
	var periods []Period
	for i := 0; i < 10; i++ {
		p := Period{Owner: string(rune('a' + i)), Plan: "pro", Start: day(120)}
		if i < 2 {
			p.End = day(10)
		}
		periods = append(periods, p)
	}
	periods = append(periods, Period{Owner: "new", Plan: "pro", Start: day(5)}, Period{Owner: "oct", Plan: "pro", Start: time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC)})
	in.Periods = periods
	in.Paying = map[string]int{"pro": 10}
	// No cancellations yet means no LTV, even with enough accounts.
	e = Compute(EconomicsInput{Now: now, Prices: prices, Paying: in.Paying, GrossMarginPct: 80, Periods: periods[2:]})
	if e.LTVKnown || e.Churned != 0 {
		t.Fatalf("zero churn: %+v", e)
	}
	in.Spend = map[string]int64{"2026-11": 300, "2026-10": 600, "2026-06": 1000}
	e = Compute(in)
	if e.ChurnBase != 10 || e.Churned != 2 || e.Churn != 0.2 {
		t.Fatalf("churn %+v", e)
	}
	// ARPA 99 × 80% ÷ 20% = 396.
	if !e.LTVKnown || math.Abs(e.LTV-396) > 1e-9 {
		t.Fatalf("ltv %v %s", e.LTV, e.LTVWhy)
	}
	if len(e.Months) != 6 || e.Months[5].Month != "2026-11" || e.Months[5].NewPaying != 1 || e.Months[5].CAC != 300 || !e.Months[5].Known {
		t.Fatalf("november %+v", e.Months)
	}
	if e.Months[0].Month != "2026-06" || e.Months[0].Known { // spend but nobody started paying
		t.Fatalf("june %+v", e.Months[0])
	}
	// Last three months: (300 + 600) ÷ (1 + 1) = 450; 396 ÷ 450 = 0.88.
	if !e.CACKnown || e.CAC != 450 || !e.RatioOK || math.Abs(e.Ratio-0.88) > 1e-9 {
		t.Fatalf("cac %v ratio %v (%s)", e.CAC, e.Ratio, e.CACWhy)
	}
	in.Spend = map[string]int64{"2026-06": 1000}
	if e = Compute(in); e.CACKnown || e.CACWhy == "" {
		t.Fatalf("no recent spend: %+v", e)
	}
}
