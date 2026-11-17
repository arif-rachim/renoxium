package platform

import (
	"context"
	"fmt"
	"time"
)

// WhatsAppCodesPerPlan is how many sign-in codes the live apps of one
// account may send per calendar month. Each code is a paid WhatsApp
// message; past the quota visitors sign in with email instead.
var WhatsAppCodesPerPlan = map[string]int{planFree: 50, planPro: 500, planBusiness: 2000}

// StorageMBPerPlan is how much file storage all apps of one account may use.
// A file used by several of the account's apps counts once.
var StorageMBPerPlan = map[string]int64{planFree: 200, planPro: 2048, planBusiness: 10240}

// StorageUsage is the bytes of files an account's apps use, and its quota.
func (p *Platform) StorageUsage(owner string) (used, quota int64) {
	p.db.QueryRow(`SELECT coalesce(sum(size), 0) FROM blobs WHERE hash IN
		(SELECT f.hash FROM app_files f JOIN apps a ON a.name = f.app WHERE a.owner = ?)`, owner).Scan(&used)
	return used, StorageMBPerPlan[p.planOf(owner)] << 20
}

func month(t time.Time) string { return t.UTC().Format("2006-01") }

func (p *Platform) ensureUsageTable() error {
	_, err := p.db.Exec(`CREATE TABLE IF NOT EXISTS usage (owner TEXT NOT NULL, month TEXT NOT NULL, kind TEXT NOT NULL,
		count INTEGER NOT NULL DEFAULT 0, PRIMARY KEY (owner, month, kind))`)
	return err
}

func (p *Platform) usage(owner, kind string) int {
	var n int
	p.db.QueryRow(`SELECT count FROM usage WHERE owner = ? AND month = ? AND kind = ?`, owner, month(time.Now()), kind).Scan(&n)
	return n
}

// addUsage counts one use and returns the new total for this month.
func (p *Platform) addUsage(owner, kind string) int {
	var n int
	p.db.QueryRow(`INSERT INTO usage (owner, month, kind, count) VALUES (?, ?, ?, 1)
		ON CONFLICT (owner, month, kind) DO UPDATE SET count = count + 1 RETURNING count`, owner, month(time.Now()), kind).Scan(&n)
	return n
}

// WhatsAppUsage is an account's sign-in codes sent this month and its quota.
func (p *Platform) WhatsAppUsage(owner string) (used, quota int) {
	return p.usage(owner, "whatsapp"), WhatsAppCodesPerPlan[p.planOf(owner)]
}

// whatsappQuotaLeft reports whether a live app's owner may send another code.
func (p *Platform) whatsappQuotaLeft(st *stage) bool {
	if st.name == Preview || p.cfg.OwnerPlan == nil {
		return true
	}
	used, quota := p.WhatsAppUsage(p.ownerOf(st.app))
	return used < quota
}

// countWhatsApp records a sent code and tells the owner once when the quota
// runs out.
func (p *Platform) countWhatsApp(st *stage) {
	if st.name == Preview || p.cfg.OwnerPlan == nil {
		return
	}
	owner := p.ownerOf(st.app)
	n := p.addUsage(owner, "whatsapp")
	if quota := WhatsAppCodesPerPlan[p.planOf(owner)]; n == quota {
		p.event(st, "whatsapp", "warn", "", fmt.Sprintf("WhatsApp sign-in quota reached (%d codes this month); visitors now sign in with email", quota))
		p.mailOwner(context.Background(), st, "WhatsApp sign-in paused until next month",
			fmt.Sprintf("Your apps sent %d WhatsApp sign-in codes this month, the limit of your plan. Visitors can still sign in with email.\n\n"+
				"Upgrade your plan for more codes: %s/billing", quota, p.cfg.SiteURL))
	}
}
