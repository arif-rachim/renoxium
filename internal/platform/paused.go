package platform

import (
	"net/http"
	"time"
)

// Accounts deactivated for inactivity (accounts.InactiveMonths without
// activity; see Config.OwnerDeactivated). Their live apps show pausedPage
// and their scheduled jobs do not run; nothing is deleted, and the owner
// brings everything back by reactivating the account.

type pausedState struct {
	paused bool
	at     time.Time
}

// pausedTTL is how long a lookup is reused; ForgetPaused ends it early.
const pausedTTL = 30 * time.Second

// ownerPaused reports whether the owner's account is deactivated.
func (p *Platform) ownerPaused(owner string) bool {
	if p.cfg.OwnerDeactivated == nil || p.cfg.SelfHosted || owner == "" || owner == DemoOwner {
		return false
	}
	if c, ok := p.paused.Load(owner); ok && time.Since(c.(pausedState).at) < pausedTTL {
		return c.(pausedState).paused
	}
	paused := p.cfg.OwnerDeactivated(owner)
	p.paused.Store(owner, pausedState{paused: paused, at: time.Now()})
	return paused
}

// ForgetPaused drops the cached state of an account after it was
// deactivated or reactivated, so its apps change at once.
func (p *Platform) ForgetPaused(owner string) { p.paused.Delete(owner) }

// OwnerPaused reports whether the owner's account is deactivated, as the
// apps see it.
func (p *Platform) OwnerPaused(owner string) bool { return p.ownerPaused(owner) }

// noteOwnerActive records activity of an account.
func (p *Platform) noteOwnerActive(owner string) {
	if p.cfg.OwnerActive != nil && owner != "" && owner != DemoOwner {
		p.cfg.OwnerActive(owner)
	}
}

// HasPaidWorkApp reports whether the owner has a work app with a
// subscription in force; such accounts are never deactivated.
func (p *Platform) HasPaidWorkApp(owner string) bool {
	var n int
	p.db.QueryRow(`SELECT count(*) FROM apps WHERE owner = ? AND plan_kind = ? AND work_status IN ('active', 'trialing', 'past_due')`,
		owner, KindWork).Scan(&n)
	return n > 0
}

// pausedPage is what visitors of a live app of a deactivated account see.
func (p *Platform) pausedPage(w http.ResponseWriter) {
	w.Header().Set("Retry-After", "86400")
	writeSysPage(w, http.StatusServiceUnavailable, sysPageData{Title: "Paused", Heading: "This site is paused",
		Body: "<p>Its owner's Renoxium account was paused after a long time without use. Nothing has been deleted: " +
			"the owner can bring it back at any time by signing in to Renoxium.</p>"})
}
