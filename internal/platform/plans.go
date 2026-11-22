package platform

import (
	"github.com/arif-rachim/renoxium/brand"
	"math"
	"sync"
	"time"
)

// Plan names, as accounts.Plan* (repeated so platform does not import accounts).
const (
	planFree     = "free"
	planPro      = "pro"
	planBusiness = "business"
)

// FreePlanFeePercent is Renoxium's cut of card payments in shops of accounts
// on the free plan. Work apps and accounts on a paid or granted plan pay no
// cut.
const FreePlanFeePercent = 2

// AppsPerPlan is how many apps an account may create on each plan.
var AppsPerPlan = map[string]int{planFree: 10, planPro: 10, planBusiness: 50}

type cachedPlan struct {
	plan string
	at   time.Time
}

var planCache sync.Map // owner -> cachedPlan

// planOf is the owner's plan, cached for 30 seconds. Without a plan lookup
// configured every owner counts as paying, so nothing is limited or charged.
func (p *Platform) planOf(owner string) string {
	if p.cfg.OwnerPlan == nil || owner == DemoOwner {
		return planBusiness
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

// ForgetPlan drops a cached plan after it changed.
func ForgetPlan(owner string) { planCache.Delete(owner) }

// applicationFee is Renoxium's cut of a card payment, in the smallest unit:
// shops pay it; work apps pay their subscription instead, and accounts on a
// paid or granted plan pay nothing.
func (p *Platform) applicationFee(app string, total int64) int64 {
	if p.cfg.SelfHosted || p.planOf(p.ownerOf(app)) != planFree || p.workPaid(app) { // a direct charge has no platform to pay
		return 0
	}
	return int64(math.Round(float64(total) * FreePlanFeePercent / 100))
}

// brandMark is the Renoxium mark as inline SVG.
func brandMark(height, fill string) string {
	return `<svg viewBox="` + brand.MarkViewBox + `" style="height:` + height + `;width:auto;flex:none" aria-hidden="true" focusable="false"><path fill="` + fill +
		`" fill-rule="evenodd" d="` + brand.MarkPath + `"/></svg>`
}
