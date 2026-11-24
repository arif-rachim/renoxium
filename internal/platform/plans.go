package platform

import (
	"fmt"
	"math"

	"github.com/arif-rachim/renoxium/brand"
	"sync"
	"time"
)

// Plan names, as accounts.Plan* (repeated so platform does not import
// accounts). An account is free, or a member: it has a card saved with
// Renoxium (saving one charges nothing). planGranted is a member by
// agreement with Renoxium, whose apps pay no shop fee and need no work app
// subscription. The plans set the limits that protect real costs; how each
// app is paid for is the same on free and member accounts (appplan.go).
const (
	planFree    = "free"
	planMember  = "member"
	planGranted = "granted"
)

// FreePlanFeePercent is Renoxium's cut of card payments in shops. Work apps
// pay their subscription instead; apps of granted accounts pay neither.
const FreePlanFeePercent = 2

// AppsPerPlan is how many apps an account may create on each plan.
var AppsPerPlan = map[string]int{planFree: 3, planMember: MaxAppsPerOwner, planGranted: MaxAppsPerOwner}

// limitPlan is the plan whose limits a plan's apps get: a grant has the
// member limits.
func limitPlan(plan string) string {
	if plan == planGranted {
		return planMember
	}
	return plan
}

type cachedPlan struct {
	plan string
	at   time.Time
}

var planCache sync.Map // owner -> cachedPlan

// planOf is the owner's plan, cached for 30 seconds. Without a plan lookup
// configured, on a self-hosted server and for the template demos every
// owner counts as granted, so nothing is limited or charged.
func (p *Platform) planOf(owner string) string {
	if p.cfg.OwnerPlan == nil || p.cfg.SelfHosted || owner == DemoOwner {
		return planGranted
	}
	if c, ok := planCache.Load(owner); ok && time.Since(c.(cachedPlan).at) < 30*time.Second {
		return c.(cachedPlan).plan
	}
	plan := p.cfg.OwnerPlan(owner)
	if _, ok := AppsPerPlan[plan]; !ok {
		plan = planFree
	}
	planCache.Store(owner, cachedPlan{plan: plan, at: time.Now()})
	return plan
}

// granted reports whether the app's owner is a member by agreement.
func (p *Platform) granted(app string) bool { return p.planOf(p.ownerOf(app)) == planGranted }

// ForgetPlan drops a cached plan after it changed.
func ForgetPlan(owner string) { planCache.Delete(owner) }

// applicationFee is Renoxium's cut of a card payment, in the smallest unit:
// shops pay it, free and member accounts alike; work apps pay their
// subscription instead, and apps of granted accounts pay nothing.
func (p *Platform) applicationFee(app string, total int64) int64 {
	if p.cfg.SelfHosted || p.granted(app) || p.workPaid(app) { // a direct charge has no platform to pay
		return 0
	}
	return int64(math.Round(float64(total) * FreePlanFeePercent / 100))
}

// brandMark is the Renoxium mark as inline SVG.
func brandMark(height, fill string) string {
	return `<svg viewBox="` + brand.MarkViewBox + `" style="height:` + height + `;width:auto;flex:none" aria-hidden="true" focusable="false"><path fill="` + fill +
		`" fill-rule="evenodd" d="` + brand.MarkPath + `"/></svg>`
}

// appLimitError says why an owner with count apps may not create another,
// or is nil. Apps beyond the limit that the owner already has (a free
// account from before the limit, a member whose card was removed or
// expired) keep running; only new ones are refused.
func (p *Platform) appLimitError(owner string, count int) error {
	plan := planGranted
	if p.cfg.OwnerPlan != nil {
		plan = p.planOf(owner)
	}
	limit := AppsPerPlan[plan]
	if count < limit {
		return nil
	}
	site := p.cfg.SiteURL
	if plan == planFree {
		return fmt.Errorf("A free account may have %d apps. Save a card at %s/billing to create up to %d (saving a card charges nothing), "+
			"or delete an app you no longer need at %s/apps/<app>/delete.", limit, site, MaxAppsPerOwner, site)
	}
	return fmt.Errorf("this account has the maximum of %d apps; delete one you no longer need, or contact Renoxium at %s/support", limit, site)
}
