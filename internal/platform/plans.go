package platform

import (
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

// FreePlanFeePercent is Renoxium's cut of card payments in apps whose owner
// is on the free plan. Paid plans pay no cut.
const FreePlanFeePercent = 2

// AppsPerPlan is how many apps an account may create on each plan.
var AppsPerPlan = map[string]int{planFree: 1, planPro: 3, planBusiness: 10}

type cachedPlan struct {
	plan string
	at   time.Time
}

var planCache sync.Map // owner -> cachedPlan

// planOf is the owner's plan, cached for 30 seconds. Without a plan lookup
// configured every owner counts as paying, so nothing is limited or charged.
func (p *Platform) planOf(owner string) string {
	if p.cfg.OwnerPlan == nil {
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

// applicationFee is Renoxium's cut of a card payment, in the smallest unit.
func (p *Platform) applicationFee(app string, total int64) int64 {
	if p.planOf(p.ownerOf(app)) != planFree {
		return 0
	}
	return int64(math.Round(float64(total) * FreePlanFeePercent / 100))
}

// badge is the small notice on live pages of free-plan apps.
const badge = `<a href="https://www.renoxium.com" target="_blank" rel="noopener" style="position:fixed;bottom:8px;inset-inline-start:8px;z-index:2147483647;` +
	`font:12px/1.4 system-ui,sans-serif;background:#111;color:#fff;padding:4px 8px;border-radius:6px;text-decoration:none;opacity:.85">Made with Renoxium</a>`
