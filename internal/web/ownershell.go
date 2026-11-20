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
	Verified, Operator                 bool
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
	if u.Verified && s.isOperator(u.Email) {
		sh.Operator = true
		sh.Ops = []shellLink{link("Overview", "/operator"), link("Resources", "/operator/resources"), {Label: "Growth", Href: "/operator#growth"}}
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
	m := meter{Label: label, Used: thousands(used), Quota: thousands(quota), Unit: unit}
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

// thousands writes n with comma separators: 20000 → "20,000".
func thousands(n int64) string {
	s := strconv.FormatInt(n, 10)
	neg := n < 0
	if neg {
		s = s[1:]
	}
	out := make([]byte, 0, len(s)+len(s)/3)
	for i := range len(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, s[i])
	}
	if neg {
		return "-" + string(out)
	}
	return string(out)
}

// planCard is one column of the plan comparison on the billing page.
type planCard struct {
	ID, Name, Price, Tagline string
	Features                 []string
	Current, Popular         bool
}

func planCards(current string) []planCard {
	f := func(plan string) []string {
		staff := "No staff accounts"
		if n := platform.StaffPerPlan[plan]; n > 0 {
			staff = strconv.Itoa(n) + " staff per app"
		}
		apps := strconv.Itoa(platform.AppsPerPlan[plan]) + " apps"
		if platform.AppsPerPlan[plan] == 1 {
			apps = "1 app"
		}
		out := []string{apps,
			thousands(int64(platform.WhatsAppCodesPerPlan[plan])) + " WhatsApp sign-in codes a month",
			storageText(platform.StorageMBPerPlan[plan]) + " of images",
			thousands(int64(platform.FetchCallsPerPlan[plan])) + " external API calls a month",
			staff}
		if plan == accounts.PlanFree {
			return append(out, strconv.Itoa(platform.FreePlanFeePercent)+"% fee on card payments", `"Made with Renoxium" badge`)
		}
		return append(out, "Your own domain", "No fee on card payments", "No Renoxium badge")
	}
	cards := []planCard{
		{ID: accounts.PlanFree, Name: "Free", Price: "0", Tagline: "Try Renoxium with one app.", Features: f(accounts.PlanFree)},
		{ID: accounts.PlanPro, Name: "Pro", Price: strconv.Itoa(accounts.PlanPriceAED[accounts.PlanPro]), Tagline: "For a growing business with a few sites.", Features: f(accounts.PlanPro), Popular: true},
		{ID: accounts.PlanBusiness, Name: "Business", Price: strconv.Itoa(accounts.PlanPriceAED[accounts.PlanBusiness]), Tagline: "For teams running many sites.", Features: f(accounts.PlanBusiness)},
	}
	for i := range cards {
		cards[i].Current = cards[i].ID == current
	}
	return cards
}

func storageText(mb int64) string {
	if mb >= 1024 && mb%1024 == 0 {
		return strconv.FormatInt(mb/1024, 10) + " GB"
	}
	return strconv.FormatInt(mb, 10) + " MB"
}

// billingNotices are the messages /billing shows after a redirect.
var billingNotices = map[string]string{
	"granted":     "Your plan was given to you by Renoxium, so there is no card, invoice or subscription to manage. To pay for a plan yourself, choose one below.",
	"no-customer": "There is no Stripe subscription on this account yet, so there is nothing to manage. Choose a plan below to subscribe.",
	"unavailable": "Paid plans are not available yet. Try again later.",
}

const ownerTemplates = `
{{define "owntop"}}{{template "head" .Shell.Title}}<body class="app own">{{template "nav"}}<div class="shell">{{template "ownnav" .Shell}}<main class="appmain">{{end}}
{{define "ownnav"}}<aside class="sidenav" aria-label="Account"><div class="who"><span class="avatar" aria-hidden="true">{{slice .Email 0 1}}</span><div class="id"><strong>{{.Email}}</strong><a class="pill plan-{{.Plan}}" href="/billing">{{.PlanName}} plan</a></div></div>
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
.meters{display:grid;gap:18px 28px}@media(min-width:700px){.meters{grid-template-columns:1fr 1fr}}
.meter .top{display:flex;justify-content:space-between;gap:12px;font-size:14px;margin-bottom:6px}.meter .top span:last-child{font-variant-numeric:tabular-nums;color:var(--muted);white-space:nowrap}
.meter .top b{color:var(--fg);font-weight:600}.meter .tr{height:8px;border-radius:4px;background:var(--bg);overflow:hidden}.meter .tr i{display:block;height:100%;background:var(--series);border-radius:4px}.meter.warn .tr i{background:var(--sun)}
.tiers{display:grid;gap:14px}@media(min-width:900px){.tiers{grid-template-columns:repeat(3,1fr)}}
.tier{position:relative;display:flex;flex-direction:column;background:var(--card);border:1px solid var(--line);border-radius:var(--radius);padding:22px}
.tier.popular{border-color:var(--fg);box-shadow:0 10px 30px -18px rgb(19 32 58 / .45)}.tier.current{border-color:var(--gold)}
.tier .badge{position:absolute;top:-11px;inset-inline-start:18px;font-size:12px;font-weight:500;padding:2px 10px;border-radius:999px;background:var(--fg);color:var(--bg)}.tier.current .badge{background:var(--gold);color:#fff}
.tier h3{font-size:18px;margin:0}.tier .tag{border:0;background:none;padding:0;font-size:14px;color:var(--muted);white-space:normal}
.tier .price{font-size:30px;font-weight:600;letter-spacing:-.02em;margin:14px 0 2px}.tier .price small{font-size:14px;font-weight:400;color:var(--muted)}
.tier ul{list-style:none;padding:0;margin:14px 0 18px;font-size:14px;display:grid;gap:7px;flex:1}.tier li{display:flex;gap:8px;align-items:flex-start}
.tier li::before{content:"";flex:none;width:16px;height:16px;margin-top:2px;border-radius:50%;background:var(--accent-soft) url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 16 16'%3E%3Cpath d='M4.5 8.2l2.2 2.2 4.8-4.8' fill='none' stroke='%231e8a50' stroke-width='1.8' stroke-linecap='round' stroke-linejoin='round'/%3E%3C/svg%3E") center/16px no-repeat}
.tier form{margin:0}.tier button,.tier .button{width:100%;margin:0;text-align:center}.tier .is{display:block;text-align:center;padding:11px;border-radius:8px;background:var(--bg);color:var(--muted);font-size:15px}
.fine{font-size:13px;color:var(--muted);margin-top:14px}
.kv{display:grid;grid-template-columns:auto 1fr;gap:10px 20px;margin:0;font-size:15px}.kv dt{color:var(--muted);font-size:14px}.kv dd{margin:0;min-width:0;overflow-wrap:anywhere}
.danger-zone{border-color:color-mix(in srgb,var(--bad) 45%,var(--line))}.danger-zone h2{color:var(--bad)}
.settings-grid{display:grid;gap:16px;max-width:820px}.settings-grid .panel+.panel{margin-top:0}
.settings-grid label:first-of-type{margin-top:4px}.settings-grid button{margin-top:16px}
.appmain .card{border-radius:var(--radius)}.appmain .card+.card,.appmain .card+.panel,.appmain .panel+.card{margin-top:16px}
</style>{{end}}`

func init() {
	template.Must(pages.Parse(ownerTemplates))
}
