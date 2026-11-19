// Package growth computes the operator's funnel and unit economics from
// per-owner timelines: where owners drop off between signing up and paying,
// and what a paying owner is worth (LTV) against what one costs to win (CAC).
// It only does arithmetic; the accounts and platform packages record the
// timestamps.
package growth

import (
	"sort"
	"time"
)

// Stage is one step of an owner's way from signing up to paying.
type Stage int

// The funnel stages, in order.
const (
	SignedUp Stage = iota
	Confirmed
	AgentConnected
	AppCreated
	Published
	CustomerActivity
	PaidStarted
	RetainedM3
	NumStages
)

// StageNames are the labels shown on the operator page.
var StageNames = [NumStages]string{"Signed up", "Email confirmed", "Agent connected", "First app created",
	"First publish to live", "First customer activity", "Paid plan started", "Still paying after 3 months"}

// RetentionAge is how long after starting a paid plan RetainedM3 is measured.
const RetentionAge = 90 * 24 * time.Hour

// Owner is one account's timeline. A zero time means the stage was not
// reached (or is not known).
type Owner struct {
	ID, Email, Plan string
	Template        string // example the first published app was built from, "" if unknown
	At              [NumStages]time.Time
}

// Period is one continuous paid subscription of an account. End is zero
// while it is still paying.
type Period struct {
	Owner, Plan string
	Start, End  time.Time
}

func (p Period) covers(t time.Time) bool {
	return !p.Start.After(t) && (p.End.IsZero() || p.End.After(t))
}

// ApplyPaid fills PaidStarted (first paid period) and RetainedM3 (paying
// RetentionAge after that start) from the accounts' paid periods.
func ApplyPaid(owners []Owner, periods []Period, now time.Time) {
	byOwner := map[string][]Period{}
	for _, p := range periods {
		byOwner[p.Owner] = append(byOwner[p.Owner], p)
	}
	for i := range owners {
		list := byOwner[owners[i].ID]
		if len(list) == 0 {
			continue
		}
		first := list[0].Start
		for _, p := range list[1:] {
			if p.Start.Before(first) {
				first = p.Start
			}
		}
		owners[i].At[PaidStarted] = first
		at := first.Add(RetentionAge)
		if at.After(now) {
			continue
		}
		for _, p := range list {
			if p.covers(at) {
				owners[i].At[RetainedM3] = at
				break
			}
		}
	}
}

// Cohort keeps the owners who signed up in month ("2006-01"); "" keeps all.
func Cohort(owners []Owner, month string) []Owner {
	if month == "" {
		return owners
	}
	var out []Owner
	for _, o := range owners {
		if o.At[SignedUp].UTC().Format("2006-01") == month {
			out = append(out, o)
		}
	}
	return out
}

// StageRow is one funnel line.
type StageRow struct {
	Stage Stage
	Name  string
	// Count is how many owners reached the stage.
	Count int
	// Base is how many owners could have reached it from the previous
	// stage: those who reached the previous one (for RetainedM3, those who
	// started paying at least RetentionAge ago). Reached is how many of the
	// Base also reached this stage.
	Base, Reached int
	// OfPrev is Reached/Base and OfSignups Count/sign-ups, in percent; -1
	// when the base is empty.
	OfPrev, OfSignups float64
	// Median time from the previous stage, over owners who reached both;
	// HasMedian is false when nobody did.
	Median    time.Duration
	HasMedian bool
}

// Funnel counts each stage over owners. now decides who is old enough for
// RetainedM3.
func Funnel(owners []Owner, now time.Time) []StageRow {
	rows := make([]StageRow, NumStages)
	signups := 0
	for _, o := range owners {
		if !o.At[SignedUp].IsZero() {
			signups++
		}
	}
	for s := SignedUp; s < NumStages; s++ {
		r := StageRow{Stage: s, Name: StageNames[s], OfPrev: -1, OfSignups: -1}
		var gaps []time.Duration
		for _, o := range owners {
			at := o.At[s]
			if !at.IsZero() {
				r.Count++
			}
			if s == SignedUp {
				continue
			}
			prev := o.At[s-1]
			if s == RetainedM3 && (prev.IsZero() || prev.Add(RetentionAge).After(now)) {
				continue // not old enough to tell
			}
			if prev.IsZero() {
				continue
			}
			r.Base++
			if at.IsZero() {
				continue
			}
			r.Reached++
			if s != RetainedM3 { // always RetentionAge by definition
				gaps = append(gaps, max(at.Sub(prev), 0))
			}
		}
		if s == SignedUp {
			r.Base, r.Reached = r.Count, r.Count
		}
		if r.Base > 0 {
			r.OfPrev = 100 * float64(r.Reached) / float64(r.Base)
		}
		if signups > 0 {
			r.OfSignups = 100 * float64(r.Count) / float64(signups)
		}
		if len(gaps) > 0 {
			r.Median, r.HasMedian = median(gaps), true
		}
		rows[s] = r
	}
	return rows
}

func median(d []time.Duration) time.Duration {
	sort.Slice(d, func(i, j int) bool { return d[i] < d[j] })
	n := len(d)
	if n%2 == 1 {
		return d[n/2]
	}
	return (d[n/2-1] + d[n/2]) / 2
}

// Week is one row of the weekly chart.
type Week struct {
	Start                        time.Time // Monday 00:00 UTC
	Signups, Published, Activity int
}

// WeekStart is the Monday 00:00 UTC of t's week.
func WeekStart(t time.Time) time.Time {
	t = t.UTC()
	day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	return day.AddDate(0, 0, -((int(day.Weekday()) + 6) % 7))
}

// Weekly counts sign-ups, first publishes (activations) and first customer
// activity per week, for the n weeks up to and including now's, oldest first.
func Weekly(owners []Owner, now time.Time, n int) []Week {
	first := WeekStart(now).AddDate(0, 0, -7*(n-1))
	weeks := make([]Week, n)
	for i := range weeks {
		weeks[i].Start = first.AddDate(0, 0, 7*i)
	}
	idx := func(t time.Time) int {
		if t.IsZero() || t.Before(first) {
			return -1
		}
		i := int(t.Sub(first) / (7 * 24 * time.Hour))
		if i >= n {
			return -1
		}
		return i
	}
	for _, o := range owners {
		if i := idx(o.At[SignedUp]); i >= 0 {
			weeks[i].Signups++
		}
		if i := idx(o.At[Published]); i >= 0 {
			weeks[i].Published++
		}
		if i := idx(o.At[CustomerActivity]); i >= 0 {
			weeks[i].Activity++
		}
	}
	return weeks
}

// TemplateRow counts owners who published from one example.
type TemplateRow struct {
	Template            string
	Published, Activity int
}

// ByTemplate groups owners who published by the example of their app,
// most published first.
func ByTemplate(owners []Owner) []TemplateRow {
	m := map[string]*TemplateRow{}
	for _, o := range owners {
		if o.At[Published].IsZero() {
			continue
		}
		name := o.Template
		if name == "" {
			name = "custom"
		}
		r := m[name]
		if r == nil {
			r = &TemplateRow{Template: name}
			m[name] = r
		}
		r.Published++
		if !o.At[CustomerActivity].IsZero() {
			r.Activity++
		}
	}
	out := make([]TemplateRow, 0, len(m))
	for _, r := range m {
		out = append(out, *r)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Published != out[j].Published {
			return out[i].Published > out[j].Published
		}
		return out[i].Template < out[j].Template
	})
	return out
}

// MinPayingForLTV is how many paying accounts LTV needs to mean anything.
const MinPayingForLTV = 5

// EconomicsInput are the figures unit economics are computed from.
type EconomicsInput struct {
	Now            time.Time
	Prices         map[string]int // monthly price in AED per paid plan
	Paying         map[string]int // accounts paying per plan now (granted plans excluded)
	Periods        []Period       // every paid period, for churn and new paying accounts
	GrossMarginPct int            // 1-100
	Spend          map[string]int64
	Months         int // months of CAC history, newest last
}

// MonthCAC is one month's acquisition cost.
type MonthCAC struct {
	Month     string // "2006-01"
	Spend     int64  // AED
	HasSpend  bool
	NewPaying int // accounts whose first paid plan started this month
	CAC       float64
	Known     bool // spend entered and at least one new paying account
}

// Economics are the subscription figures.
type Economics struct {
	MRR, PayingAccounts int
	ARPA                float64
	// Logo churn over the last 30 days: accounts paying 30 days ago that
	// pay no longer, of those paying 30 days ago.
	ChurnBase, Churned int
	Churn              float64 // fraction per month
	GrossMarginPct     int
	LTV                float64
	LTVKnown           bool
	LTVWhy             string // why LTV is unknown
	Months             []MonthCAC
	// CAC over the last three months (spend ÷ new paying accounts).
	CAC      float64
	CACKnown bool
	CACWhy   string
	Ratio    float64 // LTV ÷ CAC
	RatioOK  bool    // both known
}

// Compute works out MRR, ARPA, churn, LTV and CAC.
func Compute(in EconomicsInput) Economics {
	e := Economics{GrossMarginPct: in.GrossMarginPct}
	for plan, n := range in.Paying {
		e.MRR += in.Prices[plan] * n
		e.PayingAccounts += n
	}
	if e.PayingAccounts > 0 {
		e.ARPA = float64(e.MRR) / float64(e.PayingAccounts)
	}
	start := in.Now.Add(-30 * 24 * time.Hour)
	then, nowPaying := map[string]bool{}, map[string]bool{}
	firstStart := map[string]time.Time{}
	for _, p := range in.Periods {
		if p.covers(start) {
			then[p.Owner] = true
		}
		if p.covers(in.Now) {
			nowPaying[p.Owner] = true
		}
		if f, ok := firstStart[p.Owner]; !ok || p.Start.Before(f) {
			firstStart[p.Owner] = p.Start
		}
	}
	e.ChurnBase = len(then)
	for o := range then {
		if !nowPaying[o] {
			e.Churned++
		}
	}
	if e.ChurnBase > 0 {
		e.Churn = float64(e.Churned) / float64(e.ChurnBase)
	}
	switch {
	case e.PayingAccounts < MinPayingForLTV:
		e.LTVWhy = "not enough data: fewer than 5 paying accounts"
	case e.Churned == 0:
		e.LTVWhy = "not enough data: no paying account cancelled in the last 30 days"
	default:
		e.LTV = e.ARPA * float64(in.GrossMarginPct) / 100 / e.Churn
		e.LTVKnown = true
	}

	months := in.Months
	if months <= 0 {
		months = 6
	}
	cur := time.Date(in.Now.UTC().Year(), in.Now.UTC().Month(), 1, 0, 0, 0, 0, time.UTC)
	newPer := map[string]int{}
	for _, t := range firstStart {
		newPer[t.UTC().Format("2006-01")]++
	}
	var spend3 int64
	new3, spendMonths3 := 0, 0
	for i := months - 1; i >= 0; i-- {
		m := cur.AddDate(0, -i, 0).Format("2006-01")
		c := MonthCAC{Month: m, NewPaying: newPer[m]}
		c.Spend, c.HasSpend = in.Spend[m]
		if c.HasSpend && c.NewPaying > 0 {
			c.CAC, c.Known = float64(c.Spend)/float64(c.NewPaying), true
		}
		if i < 3 {
			spend3 += c.Spend
			new3 += c.NewPaying
			if c.HasSpend {
				spendMonths3++
			}
		}
		e.Months = append(e.Months, c)
	}
	switch {
	case spendMonths3 == 0:
		e.CACWhy = "enter marketing spend for the last three months"
	case new3 == 0:
		e.CACWhy = "no new paying accounts in the last three months"
	default:
		e.CAC, e.CACKnown = float64(spend3)/float64(new3), true
	}
	if e.LTVKnown && e.CACKnown && e.CAC > 0 {
		e.Ratio, e.RatioOK = e.LTV/e.CAC, true
	}
	return e
}
