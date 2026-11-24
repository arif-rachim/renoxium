package web

import (
	"context"
	"encoding/csv"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/arif-rachim/renoxium/internal/accounts"
	"github.com/arif-rachim/renoxium/internal/growth"
	"github.com/arif-rachim/renoxium/internal/platform"
)

// The Growth section of /operator: where owners drop off between signing
// up and paying, and what a paying owner is worth. Per-owner timelines are
// read from both databases at most every growthTTL; card fees, which need
// every card-taking live app's database, every feesTTL.
const (
	growthTTL = 2 * time.Minute
	feesTTL   = 10 * time.Minute
)

type growthCache struct {
	mu      sync.Mutex
	at      time.Time
	owners  []growth.Owner
	periods []growth.Period
	feesAt  time.Time
	fees    map[string]int64
	now     func() time.Time // tests
}

func (s *Site) growthNow() time.Time {
	if s.growth.now != nil {
		return s.growth.now()
	}
	return time.Now()
}

// forgetGrowth makes the next page view read fresh figures.
func (s *Site) forgetGrowth() {
	s.growth.mu.Lock()
	s.growth.at, s.growth.feesAt = time.Time{}, time.Time{}
	s.growth.mu.Unlock()
}

// growthData returns every owner's timeline (operators and owners of a
// suspended app left out) and the paid periods of everyone else.
func (s *Site) growthData(ctx context.Context) ([]growth.Owner, []growth.Period, error) {
	c := &s.growth
	c.mu.Lock()
	defer c.mu.Unlock()
	now := s.growthNow()
	if !c.at.IsZero() && now.Sub(c.at) < growthTTL {
		return c.owners, c.periods, nil
	}
	list, err := s.accounts.GrowthAccounts()
	if err != nil {
		return nil, nil, err
	}
	paid, err := s.accounts.PaidPeriods()
	if err != nil {
		return nil, nil, err
	}
	events, err := s.platform.FunnelEvents(ctx)
	if err != nil {
		return nil, nil, err
	}
	suspended, err := s.platform.SuspendedOwners(ctx)
	if err != nil {
		return nil, nil, err
	}
	operators := map[string]bool{}
	owners := make([]growth.Owner, 0, len(list))
	for _, a := range list {
		if s.isOperator(a.Email) {
			operators[a.ID] = true
			continue
		}
		if suspended[a.ID] {
			continue
		}
		o := growth.Owner{ID: a.ID, Email: a.Email, Plan: a.Plan}
		if a.Status == accounts.StatusGranted {
			o.Plan += " (granted)"
		}
		o.At[growth.SignedUp], o.At[growth.Confirmed], o.At[growth.AgentConnected] = a.CreatedAt, a.VerifiedAt, a.Agent
		ev := events[a.ID]
		o.At[growth.AppCreated] = ev[platform.FunnelApp].At
		o.At[growth.Published] = ev[platform.FunnelPublish].At
		o.At[growth.CustomerActivity] = ev[platform.FunnelActivity].At
		o.Template = ev[platform.FunnelPublish].Detail
		owners = append(owners, o)
	}
	periods := make([]growth.Period, 0, len(paid))
	for _, p := range paid {
		if !operators[p.UserID] {
			periods = append(periods, growth.Period{Owner: p.UserID, Plan: p.Plan, Start: p.Start, End: p.End})
		}
	}
	growth.ApplyPaid(owners, periods, now)
	sort.Slice(owners, func(i, j int) bool { return owners[i].At[growth.SignedUp].After(owners[j].At[growth.SignedUp]) })
	c.owners, c.periods, c.at = owners, periods, now
	return owners, periods, nil
}

// cardFees are the last 30 days' free-plan card fees, cached.
func (s *Site) cardFees(ctx context.Context) (map[string]int64, time.Time) {
	c := &s.growth
	c.mu.Lock()
	defer c.mu.Unlock()
	now := s.growthNow()
	if c.feesAt.IsZero() || now.Sub(c.feesAt) >= feesTTL {
		fees, err := s.platform.CardFees(ctx, now.Add(-30*24*time.Hour))
		if err != nil {
			slog.Error("card fees", "err", err)
		}
		c.fees, c.feesAt = fees, now
	}
	return c.fees, c.feesAt
}

type cohortLink struct {
	Label, Href string
	Current     bool
}

type funnelRow struct {
	Name, Count, OfPrev, OfSignups, Median, Note string
	Width                                        int // bar width, % of sign-ups
}

type weekRow struct {
	Label                        string
	Signups, Published, Activity int
	SW, PW, AW                   int // bar widths in %
}

type monthRow struct {
	growth.MonthCAC
	Label, SpendText, CACText string
}

type growthView struct {
	Tab                 string // funnel, weeks, economics or templates
	Tabs                []shellLink
	Cohort, CohortLabel string
	CSRF                string
	Cohorts             []cohortLink
	Owners              int
	Funnel              []funnelRow
	Weeks               []weekRow
	E                   growth.Economics
	ARPA, Churn, LTV    string
	CAC, Ratio          string
	RatioGood           bool
	Months              []monthRow
	Fees                []string
	FeesAt              string
	FeePct              int
	Templates           []growth.TemplateRow
	ThisMonth           string
}

// cohortParam is a sign-up month from the query ("" = all time).
func cohortParam(r *http.Request) string {
	c := r.URL.Query().Get("cohort")
	if t, err := time.Parse("2006-01", c); err == nil && len(c) == 7 {
		return t.Format("2006-01")
	}
	return ""
}

func (s *Site) growthView(r *http.Request) (*growthView, error) {
	ctx := r.Context()
	owners, periods, err := s.growthData(ctx)
	if err != nil {
		return nil, err
	}
	now := s.growthNow().UTC()
	v := &growthView{Cohort: cohortParam(r), ThisMonth: now.Format("2006-01"), FeePct: platform.FreePlanFeePercent}
	month := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	v.Cohorts = append(v.Cohorts, cohortLink{Label: "All time", Href: "/operator/growth", Current: v.Cohort == ""})
	for i := 5; i >= 0; i-- {
		m := month.AddDate(0, -i, 0)
		val := m.Format("2006-01")
		v.Cohorts = append(v.Cohorts, cohortLink{Label: m.Format("Jan 2006"), Href: "/operator/growth?cohort=" + val, Current: v.Cohort == val})
	}
	v.CohortLabel = "all time"
	if v.Cohort != "" {
		t, _ := time.Parse("2006-01", v.Cohort)
		v.CohortLabel = "signed up in " + t.Format("January 2006")
	}

	cohort := growth.Cohort(owners, v.Cohort)
	v.Owners = len(cohort)
	for _, row := range growth.Funnel(cohort, now) {
		f := funnelRow{Name: row.Name, Count: strconv.Itoa(row.Count), OfPrev: pct(row.OfPrev), OfSignups: pct(row.OfSignups), Median: "—"}
		if row.OfSignups > 0 {
			f.Width = max(int(row.OfSignups+0.5), 1)
		}
		if row.HasMedian {
			f.Median = humanDuration(row.Median)
		}
		switch row.Stage {
		case growth.SignedUp:
			f.OfPrev = ""
		case growth.AgentConnected:
			f.Note = "accounts that connected before this was recorded: time of their oldest agent sign-in still valid"
		case growth.Published:
			f.Note = "earlier publishes: time of the earliest version kept"
		case growth.CustomerActivity:
			f.Note = "an owner notice for a new order, booking, lead or enquiry, or a paid card payment, on live; earlier notices are not known"
		case growth.PaidStarted:
			f.Note = "the former account-wide Pro or Business plans, history only: accounts are now free or members (a card saved, charging nothing); work app subscriptions do not count"
		case growth.RetainedM3:
			f.Note = fmt.Sprintf("of the %d who started paying 3 or more months ago", row.Base)
			f.Median = "3 months"
		}
		v.Funnel = append(v.Funnel, f)
	}

	weeks := growth.Weekly(owners, now, 12)
	top := 1
	for _, w := range weeks {
		top = max(top, w.Signups, w.Published, w.Activity)
	}
	for _, w := range weeks {
		v.Weeks = append(v.Weeks, weekRow{Label: w.Start.Format("2 Jan"), Signups: w.Signups, Published: w.Published, Activity: w.Activity,
			SW: 100 * w.Signups / top, PW: 100 * w.Published / top, AW: 100 * w.Activity / top})
	}
	v.Templates = growth.ByTemplate(cohort)

	spend, err := s.platform.MarketingSpend()
	if err != nil {
		return nil, err
	}
	// No account plan is paid for any more: MRR and paying accounts are 0,
	// and the paid periods are the history of the former Pro and Business plans.
	v.E = growth.Compute(growth.EconomicsInput{Now: now, Periods: periods,
		GrossMarginPct: s.platform.GrossMargin(), Spend: spend, Months: 6})
	v.ARPA = aed(v.E.ARPA)
	v.Churn = "—"
	if v.E.ChurnBase > 0 {
		v.Churn = pct(100 * v.E.Churn)
	}
	if v.E.LTVKnown {
		v.LTV = aed(v.E.LTV)
	}
	if v.E.CACKnown {
		v.CAC = aed(v.E.CAC)
	}
	if v.E.RatioOK {
		v.Ratio = strconv.FormatFloat(v.E.Ratio, 'f', 1, 64)
		v.RatioGood = v.E.Ratio >= 3
	}
	for i := len(v.E.Months) - 1; i >= 0; i-- {
		m := v.E.Months[i]
		t, _ := time.Parse("2006-01", m.Month)
		row := monthRow{MonthCAC: m, Label: t.Format("Jan 2006"), SpendText: "not entered", CACText: "—"}
		if m.HasSpend {
			row.SpendText = "AED " + strconv.FormatInt(m.Spend, 10)
		}
		if m.Known {
			row.CACText = aed(m.CAC)
		}
		v.Months = append(v.Months, row)
	}

	fees, at := s.cardFees(ctx)
	curs := make([]string, 0, len(fees))
	for c := range fees {
		curs = append(curs, c)
	}
	sort.Strings(curs)
	for _, c := range curs {
		v.Fees = append(v.Fees, fmt.Sprintf("%s %.2f", strings.ToUpper(c), float64(fees[c])/100))
	}
	if len(v.Fees) == 0 {
		v.Fees = []string{"AED 0.00"}
	}
	v.FeesAt = at.UTC().Format("15:04 UTC")
	return v, nil
}

func pct(v float64) string {
	if v < 0 {
		return "—"
	}
	if v >= 10 || v == 0 {
		return strconv.FormatFloat(v, 'f', 0, 64) + "%"
	}
	return strconv.FormatFloat(v, 'f', 1, 64) + "%"
}

func aed(v float64) string { return "AED " + strconv.FormatFloat(v, 'f', 0, 64) }

func humanDuration(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "under a minute"
	case d < time.Hour:
		return fmt.Sprintf("%d min", int(d/time.Minute))
	case d < 48*time.Hour:
		return fmt.Sprintf("%.0f h", d.Hours())
	default:
		return fmt.Sprintf("%.0f days", d.Hours()/24)
	}
}

// growthPage is the operator's growth page: the funnel, cohorts and unit
// economics.
func (s *Site) growthPage(tab string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) { s.growthTab(w, r, tab) }
}

func (s *Site) growthTab(w http.ResponseWriter, r *http.Request, tab string) {
	op, ok := s.operator(w, r)
	if !ok {
		return
	}
	csrf := s.csrfToken(w, r)
	view, err := s.growthView(r)
	if err != nil {
		slog.Error("operator growth figures", "err", err)
	} else {
		view.CSRF = csrf
	}
	here := growthPaths[tab]
	q := ""
	if view != nil {
		view.Tab = tab
		if view.Cohort != "" {
			q = "?cohort=" + url.QueryEscape(view.Cohort)
		}
		for i := range view.Cohorts { // the cohort links stay on this tab
			view.Cohorts[i].Href = here + strings.TrimPrefix(view.Cohorts[i].Href, "/operator/growth")
		}
	}
	var tabs []shellLink
	for _, t := range growthTabs {
		tabs = append(tabs, shellLink{Label: t.Label, Href: growthPaths[t.Key] + q, Current: t.Key == tab})
	}
	if view != nil {
		view.Tabs = tabs
	}
	render(w, http.StatusOK, "operator_growth", map[string]any{"Growth": view, "Shell": s.ownerShell(op, "Growth", "/operator/growth", csrf)})
}

// growthTabs are the parts of the growth section, each on its own address.
var growthTabs = []struct{ Key, Label string }{{"funnel", "Funnel"}, {"weeks", "Last 12 weeks"}, {"economics", "Unit economics"}, {"templates", "By template"}}

var growthPaths = map[string]string{"funnel": "/operator/growth", "weeks": "/operator/growth/weeks", "economics": "/operator/growth/economics", "templates": "/operator/growth/templates"}

// operatorGrowth saves the operator's marketing spend for a month or the
// gross margin.
func (s *Site) operatorGrowth(w http.ResponseWriter, r *http.Request) {
	op, ok := s.operator(w, r)
	if !ok {
		return
	}
	if !s.checkCSRF(r) {
		http.Error(w, "the form expired; go back and try again", http.StatusForbidden)
		return
	}
	var err error
	switch r.PostFormValue("action") {
	case "spend":
		month := strings.TrimSpace(r.PostFormValue("month"))
		raw := strings.NewReplacer(",", "", " ", "").Replace(r.PostFormValue("spend"))
		n, perr := strconv.ParseInt(raw, 10, 64)
		switch t, merr := time.Parse("2006-01", month); {
		case merr != nil || len(month) != 7:
			err = fmt.Errorf("month must look like 2026-01")
		case t.After(s.growthNow()):
			err = fmt.Errorf("enter spend for this month or an earlier one")
		case perr != nil:
			err = fmt.Errorf("enter the spend as a whole number of AED, e.g. 1500")
		default:
			err = s.platform.SetMarketingSpend(month, n)
		}
	case "margin":
		n, perr := strconv.Atoi(strings.TrimSuffix(strings.TrimSpace(r.PostFormValue("margin")), "%"))
		if perr != nil {
			err = fmt.Errorf("gross margin must be a whole number of percent")
		} else {
			err = s.platform.SetGrossMargin(n)
		}
	default:
		err = fmt.Errorf("unknown action")
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	slog.Info("operator changed growth settings", "operator", op.Email, "action", r.PostFormValue("action"))
	back := "/operator/growth/economics"
	if c := r.PostFormValue("cohort"); c != "" {
		back += "?cohort=" + url.QueryEscape(c)
	}
	http.Redirect(w, r, back, http.StatusSeeOther)
}

// operatorGrowthCSV downloads the per-owner funnel. It changes nothing, so
// it needs no CSRF token; only the operator gets it.
func (s *Site) operatorGrowthCSV(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.operator(w, r); !ok {
		return
	}
	owners, _, err := s.growthData(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	cohort := cohortParam(r)
	name := "renoxium-funnel"
	if cohort != "" {
		name += "-" + cohort
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`.csv"`)
	w.Header().Set("Cache-Control", "no-store")
	cw := csv.NewWriter(w)
	head := []string{"owner_id", "email", "plan", "template"}
	for _, n := range []string{"signed_up", "email_confirmed", "agent_connected", "first_app", "first_publish", "first_customer_activity",
		"paid_started", "retained_m3"} {
		head = append(head, n)
	}
	cw.Write(head)
	for _, o := range growth.Cohort(owners, cohort) {
		row := []string{o.ID, csvSafe(o.Email), o.Plan, o.Template}
		for _, t := range o.At {
			if t.IsZero() {
				row = append(row, "")
			} else {
				row = append(row, t.UTC().Format(time.RFC3339))
			}
		}
		cw.Write(row)
	}
	cw.Flush()
}

// csvSafe keeps a spreadsheet from reading a cell as a formula.
func csvSafe(v string) string {
	if v != "" && strings.ContainsRune("=+-@\t\r", rune(v[0])) {
		return "'" + v
	}
	return v
}

func init() {
	template.Must(pages.Parse(growthTemplate))
}

const growthTemplate = `{{define "growth"}}<style>
.growth h2{margin:26px 0 8px}.growth .seg{flex-wrap:wrap;margin:4px 0 6px}.growth td.n,.growth th.n{text-align:end;font-variant-numeric:tabular-nums}
.growth .gb{display:block;height:10px;min-width:2px;border-radius:3px;background:var(--series)}.growth .gb.p{background:var(--gold)}.growth .gb.a{background:var(--live)}
.growth td.barcell{width:30%;min-width:120px}.growth td.stage{white-space:normal;min-width:260px;max-width:440px}.growth .snote{color:var(--muted);font-size:13px;line-height:1.4;margin-top:2px}
.growth .weeks td.barcell{width:24%}.growth .weeks .gb{height:8px}
.growth .tile .hint{font-size:13px}.growth .good{color:var(--live)}.growth .warn{color:var(--sun)}
@media(min-width:1000px) and (min-height:600px){.growth .econ{display:grid;grid-template-columns:minmax(0,1.15fr) minmax(0,1fr);gap:24px;overflow:hidden}.growth .econ .tiles{grid-template-columns:repeat(auto-fit,minmax(150px,1fr))}.growth td{padding-top:7px;padding-bottom:7px}.growth .snote{font-size:12px;line-height:1.3}.growth td.stage{max-width:none}}
.growth .forms{display:flex;gap:16px 28px;flex-wrap:wrap;align-items:flex-end;margin:14px 0 12px}.growth .forms form{display:flex;gap:8px;align-items:flex-end;flex-wrap:wrap}
.growth .forms label{margin:0 0 4px}.growth .forms input{width:190px;padding:8px 10px}.growth .forms button{margin:0;padding:8px 14px}
</style><div class="pagehead"><div><h1>Growth</h1><p class="sub">Owner accounts only: operators, the template demos and owners of a suspended app are left out. Figures refresh every few minutes.</p></div>
<div class="acts"><a class="button secondary" href="/operator/growth.csv{{with .Cohort}}?cohort={{.}}{{end}}">Download funnel CSV</a></div></div>{{template "tabs" .Tabs}}
<div class="card growth fill col" id="growth">
{{if or (eq .Tab "funnel") (eq .Tab "templates")}}<nav class="seg" aria-label="Sign-up cohort">{{range .Cohorts}}<a href="{{.Href}}"{{if .Current}} aria-current="true"{{end}}>{{.Label}}</a>{{end}}</nav>{{end}}
{{if eq .Tab "funnel"}}
<h2 style="margin-top:8px">Funnel <span class="muted">· {{.Owners}} owners, {{.CohortLabel}}</span></h2>
<div class="scroll grow"><table><thead><tr><th>Stage</th><th class="n">Owners</th><th></th><th class="n">Of previous</th><th class="n">Of sign-ups</th><th class="n">Median from previous</th></tr></thead><tbody>
{{range .Funnel}}<tr><td class="stage">{{.Name}}{{with .Note}}<div class="snote">{{.}}</div>{{end}}</td><td class="n">{{.Count}}</td><td class="barcell"><span class="gb" style="width:{{.Width}}%"></span></td><td class="n">{{.OfPrev}}</td><td class="n">{{.OfSignups}}</td><td class="n">{{.Median}}</td></tr>{{end}}
</tbody></table></div>
<p class="muted">Owners: how many reached the stage. Of previous: of those who reached the stage before, the share who reached this one too.</p>

{{else if eq .Tab "weeks"}}<h2 style="margin-top:0">Last 12 weeks <span class="muted">· all owners</span></h2>
<div class="scroll grow"><table class="weeks"><thead><tr><th>Week of</th><th class="n">Sign-ups</th><th></th><th class="n">First publish</th><th></th><th class="n">First customer</th><th></th></tr></thead><tbody>
{{range .Weeks}}<tr><td>{{.Label}}</td><td class="n">{{.Signups}}</td><td class="barcell"><span class="gb" style="width:{{.SW}}%"></span></td><td class="n">{{.Published}}</td><td class="barcell"><span class="gb p" style="width:{{.PW}}%"></span></td><td class="n">{{.Activity}}</td><td class="barcell"><span class="gb a" style="width:{{.AW}}%"></span></td></tr>{{end}}
</tbody></table></div>

{{else if eq .Tab "economics"}}<div class="grow econ"><div class="col"><h2 style="margin-top:0">Revenue and unit economics</h2><div class="tiles grow">
<div class="tile"><div class="muted">MRR</div><div class="big">AED {{.E.MRR}}</div><div class="muted">account plans cost nothing; work app subscriptions are on the Platform page</div></div>
<div class="tile"><div class="muted">Paying accounts</div><div class="big">{{.E.PayingAccounts}}</div><div class="muted">ARPA {{.ARPA}} a month</div></div>
<div class="tile"><div class="muted">Logo churn, last 30 days</div><div class="big">{{.Churn}}</div><div class="muted">{{.E.Churned}} of {{.E.ChurnBase}} paying 30 days ago stopped</div></div>
<div class="tile"><div class="muted">Card fees, last 30 days</div><div class="big">{{index .Fees 0}}</div><div class="muted">{{.FeePct}}% of Free-plan live card payments{{range $i, $f := .Fees}}{{if $i}} · {{$f}}{{end}}{{end}} · as of {{.FeesAt}}</div></div>
<div class="tile"><div class="muted">LTV</div>{{if .E.LTVKnown}}<div class="big">{{.LTV}}</div><div class="muted">ARPA × {{.E.GrossMarginPct}}% margin ÷ monthly churn</div>{{else}}<div class="big">—</div><div class="muted">{{.E.LTVWhy}}</div>{{end}}</div>
<div class="tile"><div class="muted">CAC, last 3 months</div>{{if .E.CACKnown}}<div class="big">{{.CAC}}</div><div class="muted">marketing spend ÷ new paying accounts</div>{{else}}<div class="big">—</div><div class="muted">{{.E.CACWhy}}</div>{{end}}</div>
<div class="tile"><div class="muted">LTV : CAC</div>{{if .E.RatioOK}}<div class="big {{if .RatioGood}}good{{else}}warn{{end}}">{{.Ratio}} : 1</div>{{else}}<div class="big">—</div>{{end}}<div class="muted hint">aim for 3 or more</div></div>
</div></div>
<div class="col"><h2 style="margin-top:0">Marketing spend and CAC</h2><div class="scroll grow"><table><thead><tr><th>Month</th><th class="n">Marketing spend</th><th class="n">New paying accounts</th><th class="n">CAC</th></tr></thead><tbody>
{{range .Months}}<tr><td>{{.Label}}</td><td class="n">{{.SpendText}}</td><td class="n">{{.NewPaying}}</td><td class="n">{{.CACText}}</td></tr>{{end}}
</tbody></table></div>
<div class="forms"><form method="post" action="/operator/growth"><input type="hidden" name="csrf" value="{{.CSRF}}"><input type="hidden" name="action" value="spend"><input type="hidden" name="cohort" value="{{.Cohort}}">
<div><label for="g-month">Month</label><input id="g-month" name="month" type="month" value="{{.ThisMonth}}" max="{{.ThisMonth}}" required></div>
<div><label for="g-spend">Marketing spend (AED)</label><input id="g-spend" name="spend" inputmode="numeric" pattern="[0-9, ]+" placeholder="1500" required></div><button class="secondary">Save spend</button></form>
<form method="post" action="/operator/growth"><input type="hidden" name="csrf" value="{{.CSRF}}"><input type="hidden" name="action" value="margin"><input type="hidden" name="cohort" value="{{.Cohort}}">
<div><label for="g-margin">Gross margin (%)</label><input id="g-margin" name="margin" type="number" min="1" max="100" value="{{.E.GrossMarginPct}}" required></div><button class="secondary">Save margin</button></form></div>
<p class="muted">MRR counts paid account plans, of which there are none now; ARPA leaves out card fees. LTV needs at least 5 paying accounts and at least one cancellation in the last 30 days.</p></div></div>

{{else}}<h2 style="margin-top:8px">Activation by template <span class="muted">· {{.CohortLabel}}</span></h2>
{{if .Templates}}<div class="scroll grow"><table><thead><tr><th>Example the first live app came from</th><th class="n">Published</th><th class="n">Reached first customer</th></tr></thead><tbody>
{{range .Templates}}<tr><td>{{.Template}}</td><td class="n">{{.Published}}</td><td class="n">{{.Activity}}</td></tr>{{end}}
</tbody></table></div>{{else}}<p class="muted grow">Nobody in this cohort has published yet.</p>{{end}}
<p class="muted">The example is recognised by the app's table names when it is first published; "custom" means no example was close.</p>
{{end}}</div>{{end}}`
