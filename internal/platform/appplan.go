package platform

import (
	"fmt"
	"net/http"
	"time"

	"github.com/arif-rachim/renoxium/internal/appdef"
)

// How an app is paid for. Every app of an account on the free plan is one of:
//   - static: only pages and pictures (appdef.App.Static). Free, and it keeps
//     its Renoxium address;
//   - a shop: it takes money from its customers through payments.checkout,
//     and Renoxium takes ShopFeePercent of each card payment (it may go live
//     before Stripe is connected);
//   - a work app: it does not charge its customers (a CRM, a queue, staff
//     attendance) and pays a monthly subscription, WorkAppPriceAED.
//
// Accounts on a paid or granted plan (pro, business: Renoxium's agreement
// with the owner) need neither. The owner chooses shop or work app before a
// non-static app first goes live; apps that were live before this rule keep
// running until they are published again.
const (
	KindShop = "shop"
	KindWork = "work"

	// ShopFeePercent is Renoxium's cut of a shop's card payments.
	ShopFeePercent = FreePlanFeePercent

	// WorkAppPriceAED is a work app's monthly price; keep the Stripe price in
	// step with it.
	WorkAppPriceAED = 99
	// WorkAppUsers is how many staff members a work app's price includes.
	WorkAppUsers = 5
	// WorkSeatPriceAED is the monthly price of each staff member beyond
	// WorkAppUsers; keep the Stripe price in step with it.
	WorkSeatPriceAED = 25
	// WorkAppMaxUsers is the most staff members a work app may have, until
	// staff can be given roles of their own.
	WorkAppMaxUsers = 10
	// ShopStaff is how many staff members a shop may have.
	ShopStaff = 2
)

// paying statuses keep a work app's subscription in force; past_due gives
// Stripe's retries time.
var workPaying = map[string]bool{"active": true, "trialing": true, "past_due": true}

// AppPlan is how an app is paid for and whether it may go live.
type AppPlan struct {
	Kind     string // KindShop, KindWork or "" (not chosen)
	Static   bool   // the definition is only pages and pictures
	Included bool   // the account's own plan covers the app
	// StripeReady is set when the app's Stripe account can take card payments.
	StripeReady bool
	// The work app subscription, as last seen from Stripe.
	WorkSubscription string
	WorkStatus       string
	WorkPeriodEnd    time.Time
	WorkPaid         bool
	// WorkSeats is how many staff members beyond WorkAppUsers the
	// subscription pays for, as last recorded.
	WorkSeats int
	// Ready is whether the app may be published; Problem says what to do
	// when it may not.
	Ready   bool
	Problem string
}

type appPlanRow struct {
	kind, sub, status string
	end               int64
	seats             int
}

func (p *Platform) appPlanRow(app string) appPlanRow {
	var r appPlanRow
	p.db.QueryRow(`SELECT plan_kind, work_sub, work_status, work_period_end, work_seats FROM apps WHERE name = ?`, app).
		Scan(&r.kind, &r.sub, &r.status, &r.end, &r.seats)
	return r
}

func (p *Platform) appKind(app string) string { return p.appPlanRow(app).kind }

// workPaid reports whether the app is a work app with a subscription in force.
func (p *Platform) workPaid(app string) bool {
	r := p.appPlanRow(app)
	return r.kind == KindWork && workPaying[r.status]
}

// AppPlan describes how the app is paid for, judged on its newest version.
func (p *Platform) AppPlan(app string) (AppPlan, error) {
	if _, err := p.Info(app); err != nil {
		return AppPlan{}, err
	}
	def, _, err := p.Definition(app, 0)
	if err != nil {
		def = &appdef.App{} // nothing applied yet
	}
	return p.planFor(app, def), nil
}

// planFor judges the app as if def were live.
func (p *Platform) planFor(app string, def *appdef.App) AppPlan {
	r := p.appPlanRow(app)
	pl := AppPlan{Kind: r.kind, Static: def.Static(), WorkSubscription: r.sub, WorkStatus: r.status,
		WorkPaid: r.kind == KindWork && workPaying[r.status], WorkSeats: r.seats}
	if r.end > 0 {
		pl.WorkPeriodEnd = time.Unix(r.end, 0)
	}
	if pa, err := p.PaymentAccount(app); err == nil {
		pl.StripeReady = pa.StripeAccount != "" && pa.ChargesEnabled
	}
	if p.cfg.OwnerPlan == nil || p.cfg.SelfHosted || p.planOf(p.ownerOf(app)) != planFree {
		pl.Included, pl.Ready = true, true
		return pl
	}
	site := p.cfg.SiteURL
	switch {
	case pl.Static:
		pl.Ready = true
	case r.kind == KindShop:
		// A shop may go live before Stripe is connected: until then it takes
		// cash on delivery only, and its Plan page asks for Stripe.
		pl.Ready = true
	case pl.WorkPaid:
		pl.Ready = true
	case r.kind == KindWork:
		pl.Problem = fmt.Sprintf("%s is a work app: subscribe (AED %d a month) at %s/apps/%s/plan before it goes live", app, WorkAppPriceAED, site, app)
	default:
		pl.Problem = fmt.Sprintf("before %s goes live, the owner chooses at %s/apps/%s/plan how it is paid for: "+
			"a shop (Renoxium takes %d%% of card payments) or a work app (AED %d a month)", app, site, app, ShopFeePercent, WorkAppPriceAED)
	}
	return pl
}

// SetAppKind records whether the app is a shop or a work app ("" clears it).
func (p *Platform) SetAppKind(app, kind string) error {
	if kind != KindShop && kind != KindWork && kind != "" {
		return fmt.Errorf("kind must be %q or %q", KindShop, KindWork)
	}
	if _, err := p.Info(app); err != nil {
		return err
	}
	_, err := p.db.Exec(`UPDATE apps SET plan_kind = ? WHERE name = ?`, kind, app)
	p.lapsed.Delete(app)
	return err
}

// SetWorkSubscription records the app's work app subscription as Stripe has
// it. A subscription in force makes the app a work app.
func (p *Platform) SetWorkSubscription(app, subscription, status string, periodEnd time.Time) error {
	var end int64
	if !periodEnd.IsZero() {
		end = periodEnd.Unix()
	}
	_, err := p.db.Exec(`UPDATE apps SET work_sub = ?, work_status = ?, work_period_end = ?,
		plan_kind = CASE WHEN ? THEN 'work' ELSE plan_kind END WHERE name = ?`, subscription, status, end, workPaying[status], app)
	p.lapsed.Delete(app)
	return err
}

// SetWorkSeats records how many extra staff members the app's work app
// subscription pays for.
func (p *Platform) SetWorkSeats(app string, seats int) error {
	_, err := p.db.Exec(`UPDATE apps SET work_seats = ? WHERE name = ?`, max(seats, 0), app)
	return err
}

// WorkSeatsNeeded is how many extra staff members a work app with its
// current staff must pay for.
func (p *Platform) WorkSeatsNeeded(app string) int {
	staff, _ := p.Staff(app)
	return max(len(staff)-WorkAppUsers, 0)
}

// AppOfWorkSubscription is the app a work app subscription pays for, or "".
func (p *Platform) AppOfWorkSubscription(subscription string) string {
	var app string
	if subscription != "" {
		p.db.QueryRow(`SELECT name FROM apps WHERE work_sub = ?`, subscription).Scan(&app)
	}
	return app
}

// PaidWorkApps is how many work apps have a subscription in force.
func (p *Platform) PaidWorkApps() int {
	var n int
	p.db.QueryRow(`SELECT count(*) FROM apps WHERE plan_kind = ? AND work_status IN ('active', 'trialing', 'past_due')`, KindWork).Scan(&n)
	return n
}

// quotaPlan is the plan whose limits an account's apps get: an account on
// the free plan that pays for a work app gets the Pro limits.
func (p *Platform) quotaPlan(owner string) string {
	plan := p.planOf(owner)
	if plan != planFree {
		return plan
	}
	var n int
	p.db.QueryRow(`SELECT count(*) FROM apps WHERE owner = ? AND plan_kind = ? AND work_status IN ('active', 'trialing', 'past_due')`,
		owner, KindWork).Scan(&n)
	if n > 0 {
		return planPro
	}
	return plan
}

// appQuotaPlan is the plan whose per-app limits the app gets.
func (p *Platform) appQuotaPlan(app string) string {
	if plan := p.planOf(p.ownerOf(app)); plan != planFree {
		return plan
	}
	if p.workPaid(app) {
		return planPro
	}
	return planFree
}

type lapsedState struct {
	lapsed bool
	at     time.Time
}

// planLapsed reports whether the live app is a work app whose subscription
// ended: it then shows a notice instead of its pages. Cached for 30 seconds.
func (p *Platform) planLapsed(app string) bool {
	if p.cfg.OwnerPlan == nil || p.cfg.SelfHosted {
		return false
	}
	if c, ok := p.lapsed.Load(app); ok && time.Since(c.(lapsedState).at) < 30*time.Second {
		return c.(lapsedState).lapsed
	}
	r := p.appPlanRow(app)
	lapsed := r.kind == KindWork && !workPaying[r.status] && p.planOf(p.ownerOf(app)) == planFree
	p.lapsed.Store(app, lapsedState{lapsed: lapsed, at: time.Now()})
	return lapsed
}

// lapsedPage is what visitors of a work app whose subscription ended see.
func (p *Platform) lapsedPage(w http.ResponseWriter) {
	writeSysPage(w, http.StatusPaymentRequired, sysPageData{Title: "Paused", Heading: "This app is paused",
		Body: "<p>Its owner's subscription has ended. It comes back as soon as it is renewed.</p>"})
}
