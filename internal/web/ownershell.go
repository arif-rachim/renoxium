package web

import (
	"html/template"
	"strconv"

	"github.com/arif-rachim/renoxium/internal/accounts"
	"github.com/arif-rachim/renoxium/internal/platform"
)

// ownerShell is what the account pages (your apps, plan and billing,
// account, operator) show around their content: the same side navigation
// as an app's admin panel, with the account instead of the app.
type ownerShell struct {
	Title, Email, Plan, PlanName, CSRF string
	Verified, Operator, SingleApp      bool
	Main, Account, Ops                 []shellLink
}

func planName(plan string) string {
	switch plan {
	case accounts.PlanPro:
		return "Pro"
	case accounts.PlanBusiness:
		return "Business"
	}
	return "Free"
}

// ownerShell builds the navigation of the account pages; current is the
// href of this page.
func (s *Site) ownerShell(u accounts.User, title, current, csrf string) ownerShell {
	plan := s.accounts.Plan(u.ID)
	link := func(label, href string) shellLink {
		return shellLink{Label: label, Href: href, Current: href == current}
	}
	sh := ownerShell{Title: title, Email: u.Email, Plan: plan, PlanName: planName(plan), CSRF: csrf, Verified: u.Verified,
		Main:    []shellLink{link("Your apps", "/"), link("Templates", "/templates")},
		Account: []shellLink{link("Plan and billing", "/billing"), link("Account settings", "/account"), link("Connect and help", "/connect")}}
	if s.SingleApp != "" { // a self-hosted server: one app, no Renoxium service pages
		sh.Main = []shellLink{link("Your app", "/apps/"+s.SingleApp)}
		sh.Account = []shellLink{link("Account settings", "/account")}
		sh.SingleApp = true
		return sh
	}
	if u.Verified && s.isOperator(u.Email) {
		sh.Operator = true
		sh.Ops = []shellLink{link("Platform", "/operator"), link("Resources", "/operator/resources"), link("Growth", "/operator/growth")}
	}
	return sh
}

// meter is one usage line on the billing page.
type meter struct {
	Label, Used, Quota, Unit string
	Pct                      int
	Warn                     bool
}

func newMeter(label string, used, quota int64, unit string) meter {
	m := meter{Label: label, Used: thousands(int(used)), Quota: thousands(int(quota)), Unit: unit}
	if quota > 0 {
		m.Pct = int(used * 100 / quota)
		if used > 0 && m.Pct == 0 {
			m.Pct = 1
		}
		if m.Pct > 100 {
			m.Pct = 100
		}
	}
	m.Warn = m.Pct >= 80
	return m
}

// planCard is one column of the plan comparison on the billing page.
type planCard struct {
	ID, Name, Price, Tagline string
	Features                 []string
}

// planCards are how an app can be paid for, as the billing page explains it.
func planCards() []planCard {
	fee, price, users := strconv.Itoa(platform.ShopFeePercent), strconv.Itoa(platform.WorkAppPriceAED), strconv.Itoa(platform.WorkAppUsers)
	return []planCard{
		{ID: "static", Name: "Static site", Price: "Free", Tagline: "Pages and photos.",
			Features: []string{"No database, server code or sign-in", "Stays at its Renoxium address"}},
		{ID: "shop", Name: "Shop", Price: "AED 0 + " + fee + "% of card payments", Tagline: "Takes money from its customers.",
			Features: []string{"Nothing to pay when nobody pays you", "Your own domain", strconv.Itoa(platform.ShopStaff) + " staff"}},
		{ID: "work", Name: "Work app", Price: "AED " + price + " a month", Tagline: "Does not charge its customers.",
			Features: []string{"CRM, queue, staff attendance, members' portal", "Or a shop paid only in cash or by transfer", "Your own domain", users + " staff included"}},
		{ID: "enterprise", Name: "Larger businesses", Price: "By agreement", Tagline: "Many staff or branches.",
			Features: []string{"A price agreed with Renoxium", "Higher limits"}},
	}
}

// appPlanRow is one app on the billing page.
type appPlanRow struct {
	App, Label, Status string
	Ready              bool
}

func appPlanRows(p *platform.Platform, apps []platform.AppInfo) []appPlanRow {
	var out []appPlanRow
	for _, a := range apps {
		pl, err := p.AppPlan(a.App)
		if err != nil {
			continue
		}
		row := appPlanRow{App: a.App, Ready: pl.Ready}
		switch {
		case pl.Included:
			row.Label, row.Status = "Included", "covered by your plan"
		case pl.Static:
			row.Label, row.Status = "Static site", "free"
		case pl.Kind == platform.KindShop:
			row.Label, row.Status = "Shop", map[bool]string{true: "taking card payments", false: "connect Stripe"}[pl.StripeReady]
		case pl.Kind == platform.KindWork && pl.WorkPaid:
			row.Label, row.Status = "Work app", pl.WorkStatus
			if d := formatDate(pl.WorkPeriodEnd); d != "" {
				row.Status += " · renews " + d
			}
		case pl.Kind == platform.KindWork:
			row.Label, row.Status = "Work app", "not subscribed"
		default:
			row.Label, row.Status = "Not chosen", "choose before it goes live"
		}
		out = append(out, row)
	}
	return out
}

// billingNotices are the messages /billing shows after a redirect.
var billingNotices = map[string]string{
	"granted":     "Your plan was given to you by Renoxium, so there is no card, invoice or subscription to manage. To pay for a plan yourself, choose one below.",
	"no-customer": "There is no Stripe subscription on this account yet, so there is nothing to manage. Choose a plan below to subscribe.",
	"unavailable": "Paid plans are not available yet. Try again later.",
}

const ownerTemplates = `
{{define "owntop"}}{{template "head" .Shell.Title}}<body class="app own">{{if .Shell.SingleApp}}{{template "nav" "single"}}{{else}}{{template "nav"}}{{end}}<div class="shell">{{template "ownnav" .Shell}}<main class="appmain">{{end}}
{{define "ownnav"}}<aside class="sidenav" id="sidemenu" aria-label="Account"><div class="who"><span class="avatar" aria-hidden="true">{{slice .Email 0 1}}</span><div class="id"><strong>{{.Email}}</strong>{{if .SingleApp}}<span class="pill plan-{{.Plan}}">{{.PlanName}} plan</span>{{else}}<a class="pill plan-{{.Plan}}" href="/billing">{{.PlanName}} plan</a>{{end}}</div></div>
<nav><h3>Workspace</h3>{{template "navlist" .Main}}<h3>Account</h3>{{template "navlist" .Account}}{{with .Ops}}<h3>Operator</h3>{{template "navlist" .}}{{end}}</nav>
<form class="signout" method="post" action="/logout"><input type="hidden" name="csrf" value="{{.CSRF}}"><button class="secondary">Sign out</button></form></aside>{{end}}
{{define "owncss"}}<style>
body.own .sidenav .who{display:flex;gap:10px;align-items:center}body.own .sidenav .who .id{min-width:0}
body.own .sidenav .who strong{font-size:14px;font-weight:500;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.avatar{flex:none;width:38px;height:38px;border-radius:50%;display:inline-flex;align-items:center;justify-content:center;background:var(--fg);color:var(--bg);font-weight:600;text-transform:uppercase}
.pill.plan-pro,.pill.plan-business{background:var(--sun-soft);border-color:transparent}.pill.plan-pro::before,.pill.plan-business::before{background:var(--sun)}a.pill{text-decoration:none}
.signout button{width:100%;margin-top:22px;font-size:14px;padding:8px 12px}
@media(max-width:999px){body.own .sidenav .who .id{display:flex;gap:8px;align-items:center;flex-wrap:wrap}.signout button{width:auto;margin:10px 0 0;padding:6px 12px}}
.panel h2{font-size:16px}.panel .ph h2{margin:0}.panel p:last-child{margin-bottom:0}
.flash{display:flex;gap:10px;align-items:flex-start;border:1px solid var(--line);border-inline-start:4px solid var(--sun);background:var(--card);border-radius:10px;padding:12px 16px;margin:0 0 16px}
.flash.ok{border-inline-start-color:var(--live)}.flash.bad{border-inline-start-color:var(--bad)}
.hero{display:grid;gap:18px 28px;align-items:center}@media(min-width:760px){.hero{grid-template-columns:minmax(0,1fr) auto}}
.hero .name{font-size:30px;font-weight:600;letter-spacing:-.02em;line-height:1.15;margin:2px 0 6px}.hero .name .per{font-size:15px;font-weight:400;color:var(--muted)}
.hero .acts{display:flex;gap:8px;flex-wrap:wrap}.hero .acts button,.hero .acts .button{margin:0}
.meters{display:grid;gap:14px 28px}@media(min-width:700px){.meters{grid-template-columns:1fr 1fr}}
.meter .top{display:flex;justify-content:space-between;gap:12px;font-size:14px;margin-bottom:6px}.meter .top span:last-child{font-variant-numeric:tabular-nums;color:var(--muted);white-space:nowrap}
.meter .top b{color:var(--fg);font-weight:600}.meter .tr{height:8px;border-radius:4px;background:var(--bg);overflow:hidden}.meter .tr i{display:block;height:100%;background:var(--series);border-radius:4px}.meter.warn .tr i{background:var(--sun)}
.tiers{display:grid;gap:14px}@media(min-width:900px){.tiers{grid-template-columns:repeat(3,1fr)}}
.tier{position:relative;display:flex;flex-direction:column;background:var(--card);border:1px solid var(--line);border-radius:var(--radius);padding:18px}
.tier.popular{border-color:var(--fg);box-shadow:0 10px 30px -18px rgb(19 32 58 / .45)}.tier.current{border-color:var(--gold)}
.tier .badge{position:absolute;top:-11px;inset-inline-start:18px;font-size:12px;font-weight:500;padding:2px 10px;border-radius:999px;background:var(--fg);color:var(--bg)}.tier.current .badge{background:var(--gold);color:#fff}
.tier h3{font-size:18px;margin:0}.tier .tag{border:0;background:none;padding:0;font-size:14px;color:var(--muted);white-space:normal}
.tier .price{font-size:26px;font-weight:600;letter-spacing:-.02em;margin:8px 0 2px}.tier .price small{font-size:14px;font-weight:400;color:var(--muted)}
.tier ul{list-style:none;padding:0;margin:8px 0 12px;font-size:13.5px;display:grid;gap:3px;flex:1;align-content:start}.tier li{display:flex;gap:8px;align-items:flex-start}
.tier li::before{content:"";flex:none;width:16px;height:16px;margin-top:2px;border-radius:50%;background:var(--accent-soft) url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 16 16'%3E%3Cpath d='M4.5 8.2l2.2 2.2 4.8-4.8' fill='none' stroke='%231e8a50' stroke-width='1.8' stroke-linecap='round' stroke-linejoin='round'/%3E%3C/svg%3E") center/16px no-repeat}
.tier form{margin:0}.tier button,.tier .button{width:100%;margin:0;text-align:center}.tier .is{display:block;text-align:center;padding:11px;border-radius:8px;background:var(--bg);color:var(--muted);font-size:15px}
.fine{font-size:13px;color:var(--muted);margin-top:14px}
.kv{display:grid;grid-template-columns:auto 1fr;gap:10px 20px;margin:0;font-size:15px}.kv dt{color:var(--muted);font-size:14px}.kv dd{margin:0;min-width:0;overflow-wrap:anywhere}
.danger-zone{border-color:color-mix(in srgb,var(--bad) 45%,var(--line))}.danger-zone h2{color:var(--bad)}
.settings-grid{display:grid;gap:16px;align-items:start}@media(min-width:1100px){.settings-grid{grid-template-columns:1fr 1fr}}.settings-grid .panel+.panel{margin-top:0}
.settings-grid label:first-of-type{margin-top:4px}.settings-grid button{margin-top:16px}
.appmain .card{border-radius:var(--radius)}.appmain .card+.card,.appmain .card+.panel,.appmain .panel+.card{margin-top:16px}
</style>{{end}}`

func init() {
	template.Must(pages.Parse(ownerTemplates))
}
