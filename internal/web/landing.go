package web

import (
	"html/template"
	"net/http"
)

// Legal holds the company details shown on the Privacy Policy and Terms.
// Until Reviewed is true, both pages say they are drafts.
type Legal struct {
	Company  string // legal name as on the trade license
	Address  string
	Email    string // contact for privacy and legal questions
	Reviewed bool
}

func (s *Site) registerLanding() {
	s.mux.HandleFunc("GET /privacy", s.legalPage("privacy"))
	s.mux.HandleFunc("GET /terms", s.legalPage("terms"))
}

func (s *Site) landingData() map[string]any {
	return map[string]any{"MCP": s.base + "/mcp", "Fee": 2, "Legal": s.legalOrDefaults(), "Cards": s.galleryCards()}
}

func (s *Site) legalOrDefaults() Legal {
	l := s.Legal
	if l.Company == "" {
		l.Company = "Renoxium"
	}
	if l.Email == "" {
		l.Email = s.AbuseEmail
	}
	return l
}

func (s *Site) legalPage(name string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		landingPages.ExecuteTemplate(w, name, map[string]any{"Legal": s.legalOrDefaults(), "Base": s.base})
	}
}

func renderLanding(w http.ResponseWriter, data map[string]any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	landingPages.ExecuteTemplate(w, "landing", data)
}

var landingPages = template.Must(template.New("").Parse(`
{{define "lhead"}}<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.}}</title><meta name="description" content="Websites for UAE businesses, built by chatting with ChatGPT or Claude: online stores, bookings, rentals, quotes and listings, with card payments, cash on delivery, English and Arabic.">
<link rel="preconnect" href="https://fonts.googleapis.com"><link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
<link href="https://fonts.googleapis.com/css2?family=Readex+Pro:wght@300;400;500;600;700&display=swap" rel="stylesheet">
<style>
:root{color-scheme:light dark;--ink:#0f2a2a;--soft:#4d6563;--paper:#eef3f1;--panel:#ffffff;--rule:#d3dfdb;--blue:#0e6f66;--blue-ink:#ffffff;--live:#1f8a4c;--bubble:#dcefeb;--sun:#e9a825;--sun-soft:#fbefd2;--deep:#0d3b37;--deep-ink:#e6f2ef}
@media (prefers-color-scheme:dark){:root{--ink:#e3eeeb;--soft:#93a9a5;--paper:#0b1615;--panel:#11201e;--rule:#21403b;--blue:#3fc2b2;--blue-ink:#06201d;--live:#4cc88a;--bubble:#163a35;--sun:#f2bd4b;--sun-soft:#3a3016;--deep:#0f2b28;--deep-ink:#e3eeeb}}
*{box-sizing:border-box}html{-webkit-text-size-adjust:100%}
body{margin:0;background:var(--paper);color:var(--ink);font:17px/1.6 "Readex Pro",system-ui,-apple-system,"Segoe UI",sans-serif}
a{color:var(--blue);text-underline-offset:3px}a:focus-visible,button:focus-visible,summary:focus-visible{outline:3px solid var(--blue);outline-offset:3px;border-radius:6px}
.wrap{max-width:1120px;margin:0 auto;padding:0 20px}
nav{display:flex;align-items:center;justify-content:space-between;gap:16px;padding:20px 0;flex-wrap:wrap}
.logo{font-weight:700;font-size:21px;color:var(--ink);text-decoration:none;letter-spacing:-.01em}
nav .links{display:flex;gap:6px 20px;font-size:15px;flex-wrap:wrap}nav .links a{color:var(--ink);text-decoration:none}nav .links a:hover{color:var(--blue)}
.btn{display:inline-block;background:var(--blue);color:var(--blue-ink);padding:13px 22px;border-radius:12px;font-weight:500;text-decoration:none;font-size:16px;border:1px solid var(--blue)}
.btn.quiet{background:transparent;color:var(--ink);border-color:var(--rule)}
footer a{color:var(--soft)}
@media (prefers-reduced-motion:reduce){*{animation:none!important;transition:none!important}}
</style>{{end}}

{{define "landing"}}{{template "lhead" "Renoxium: your business website, built in a chat"}}<style>
.hero{display:grid;gap:40px;padding:28px 0 72px;align-items:center}@media(min-width:960px){.hero{grid-template-columns:1.1fr 1fr;gap:56px;padding:48px 0 96px}}
h1{margin:0 0 22px;font-weight:600;letter-spacing:-.03em;line-height:1.02;font-size:clamp(40px,6.4vw,70px)}
h1 .ar{display:block;width:fit-content;max-width:100%;direction:rtl;font-weight:400;font-size:.5em;letter-spacing:0;line-height:1.35;color:var(--blue);margin-top:14px}
.lede{font-size:19px;color:var(--soft);max-width:31em;margin:0 0 28px}
.cta{display:flex;gap:12px;flex-wrap:wrap;align-items:center}.small{font-size:14px;color:var(--soft);margin-top:14px}
.chat{background:var(--panel);border:1px solid var(--rule);border-radius:22px;padding:22px;display:flex;flex-direction:column;gap:10px;font-size:15.5px;box-shadow:0 30px 60px -40px rgba(13,59,55,.45)}
.msg{max-width:88%;padding:11px 15px;border-radius:16px;line-height:1.5}
.me{align-self:flex-end;background:var(--blue);color:var(--blue-ink);border-bottom-right-radius:5px}.me.ar{direction:rtl}
.agent{align-self:flex-start;background:var(--bubble);border-bottom-left-radius:5px}
.who{font-size:12.5px;color:var(--soft);margin:6px 2px -4px}.who.right{align-self:flex-end}
.site{display:flex;align-items:center;gap:10px;margin-top:10px;padding:10px 12px;border-radius:12px;background:var(--panel);font-size:14.5px}
.site b{font-weight:600}.site .dot{width:9px;height:9px;border-radius:50%;background:var(--sun);flex:none}.site span{color:var(--soft);margin-inline-start:auto;font-size:13px}
section{padding:72px 0;border-top:1px solid var(--rule)}
h2{font-size:clamp(28px,3.8vw,40px);letter-spacing:-.02em;margin:0 0 14px;line-height:1.12;font-weight:600;max-width:20em}
.sub{color:var(--soft);max-width:38em;margin:0 0 36px;font-size:18px}
.kinds{display:grid;gap:1px;background:var(--rule);border:1px solid var(--rule);border-radius:18px;overflow:hidden}@media(min-width:720px){.kinds{grid-template-columns:1fr 1fr}}@media(min-width:1040px){.kinds{grid-template-columns:repeat(3,1fr)}}
.kind{background:var(--panel);padding:22px 22px 20px;display:flex;flex-direction:column;gap:8px}
.kind h3{margin:0;font-size:20px;font-weight:600}.kind p{margin:0;color:var(--soft);font-size:15.5px}.kind .for{font-size:14px;color:var(--ink)}
.kind a{margin-top:auto;padding-top:6px;font-weight:500;font-size:15.5px}
.steps{list-style:none;padding:0;margin:0;display:grid;gap:28px;counter-reset:s}@media(min-width:800px){.steps{grid-template-columns:repeat(3,1fr)}}
.steps li{counter-increment:s;padding-top:58px;position:relative}
.steps li::before{content:counter(s);position:absolute;top:0;left:0;font-size:40px;line-height:1;font-weight:300;color:var(--blue)}
.steps h3{margin:0 0 6px;font-size:19px;font-weight:600}.steps p{margin:0;color:var(--soft)}.steps q{display:block;margin-top:10px;font-size:15px;color:var(--ink);quotes:"\201C" "\201D"}
.day{background:var(--deep);color:var(--deep-ink);border-radius:24px;padding:clamp(24px,4vw,48px);display:grid;gap:32px}@media(min-width:960px){.day{grid-template-columns:.9fr 1.1fr;align-items:center}}
.day h2{color:inherit}.day .sub{color:color-mix(in srgb,var(--deep-ink) 75%,transparent);margin-bottom:20px}
.day ul{margin:0;padding:0;list-style:none;display:grid;gap:12px}.day li{padding-inline-start:26px;position:relative}.day li::before{content:"";position:absolute;inset-inline-start:2px;top:.55em;width:10px;height:10px;border-radius:3px;background:var(--sun)}
.panelmock{background:var(--panel);color:var(--ink);border-radius:18px;padding:18px;display:grid;gap:12px;font-size:14.5px}
.panelmock .top{display:flex;justify-content:space-between;align-items:center;gap:8px}.panelmock .top b{font-size:17px;font-weight:600}
.pill{display:inline-flex;align-items:center;gap:6px;font-size:12.5px;font-weight:500;padding:3px 10px;border-radius:999px;border:1px solid var(--rule)}.pill::before{content:"";width:7px;height:7px;border-radius:50%;background:var(--live)}
.figs{display:grid;grid-template-columns:repeat(3,1fr);gap:8px}.figs div{border:1px solid var(--rule);border-radius:12px;padding:10px 12px}.figs small{display:block;color:var(--soft);font-size:12.5px}.figs b{font-size:18px;font-weight:600;font-variant-numeric:tabular-nums}
.sched{border:1px solid var(--rule);border-radius:12px;overflow:hidden}.sched div{display:grid;grid-template-columns:52px 1fr auto;gap:10px;padding:9px 12px;border-top:1px solid var(--rule);align-items:center}.sched div:first-child{border-top:0}
.sched time{font-variant-numeric:tabular-nums;color:var(--soft)}.sched em{font-style:normal;font-size:12.5px;padding:2px 8px;border-radius:999px;background:var(--bubble)}.sched em.wait{background:var(--sun-soft)}
.wa{background:var(--bubble);border-radius:14px 14px 14px 4px;padding:10px 12px;font-size:14px}.wa small{display:block;color:var(--soft);font-size:12px;margin-bottom:2px}
.groups{display:grid;gap:36px 48px}@media(min-width:760px){.groups{grid-template-columns:1fr 1fr}}
.groups h3{margin:0 0 10px;font-size:19px;font-weight:600;padding-bottom:10px;border-bottom:2px solid var(--blue);display:inline-block}
.groups ul{list-style:none;margin:0;padding:0;display:grid;gap:10px}.groups li strong{display:block;font-weight:500}.groups li span{color:var(--soft);font-size:15.5px}
.prices{display:grid;gap:16px}@media(min-width:860px){.prices{grid-template-columns:repeat(3,1fr)}}
.plan{background:var(--panel);border:1px solid var(--rule);border-radius:18px;padding:24px;display:flex;flex-direction:column}.plan.pick{border:2px solid var(--blue)}
.plan h3{margin:0;font-size:20px;font-weight:600}.plan .for{color:var(--soft);font-size:15px;margin:4px 0 0}.amount{font-size:36px;font-weight:600;letter-spacing:-.02em;margin:12px 0 2px}.amount span{font-size:16px;font-weight:400;color:var(--soft)}
.plan ul{padding:0;margin:16px 0 22px;list-style:none;display:grid;gap:8px;font-size:15.5px}
.plan li{padding-inline-start:22px;position:relative}.plan li::before{content:"";position:absolute;inset-inline-start:2px;top:.55em;width:9px;height:9px;border-radius:2px;background:var(--blue);opacity:.8}
.plan .btn{margin-top:auto;text-align:center}.note{color:var(--soft);font-size:15px;margin-top:18px;max-width:46em}
.faq{display:grid;gap:0;max-width:820px;border-top:1px solid var(--rule)}.faq details{border-bottom:1px solid var(--rule);padding:16px 0}
.faq summary{cursor:pointer;font-weight:500;font-size:18px;list-style:none;display:flex;justify-content:space-between;gap:16px}.faq summary::after{content:"+";color:var(--blue);font-size:22px;line-height:1}.faq details[open] summary::after{content:"\2212"}
.faq summary::-webkit-details-marker{display:none}.faq p{margin:10px 0 0;color:var(--soft);max-width:40em}
.end{text-align:center;padding:80px 0}.end h2{margin:0 auto 14px}.end .sub{margin:0 auto 28px}.end .cta{justify-content:center}
footer{border-top:1px solid var(--rule);padding:28px 0 48px;font-size:14px;color:var(--soft);display:flex;gap:18px;flex-wrap:wrap;justify-content:space-between}
footer .links{display:flex;gap:18px;flex-wrap:wrap}
</style></head><body><div class="wrap">
<nav aria-label="Main"><a class="logo" href="/">Renoxium</a><div class="links"><a href="/templates">Templates</a><a href="#pricing">Pricing</a><a href="/connect">Connect</a><a href="/login">Sign in</a></div></nav>

<div class="hero"><div>
<h1>Your business online, built in a chat. <span class="ar" lang="ar">موقع عملك، جاهز من محادثة.</span></h1>
<p class="lede">Tell ChatGPT or Claude what you sell or book. Renoxium builds the site from a finished UAE template, hosts it, and takes card payments and cash on delivery, in English and Arabic.</p>
<div class="cta"><a class="btn" href="/signup">Create your account</a><a class="btn quiet" href="/templates">Try the templates</a></div>
<p class="small">Free to start. No code, no designer, nothing to install.</p>
</div>
<div class="chat" aria-label="Example conversation">
<div class="who right">You, in ChatGPT</div>
<div class="msg me">Make a booking site for Lamsa, my salon in Jumeirah. Haircuts, colour and nails, three stylists.</div>
<div class="msg me ar" lang="ar">وأريد عربون ٥٠ درهم عند الحجز</div>
<div class="who">Renoxium</div>
<div class="msg agent">Done. Customers pick a service, a stylist and a free time, pay a AED 50 deposit by card, and get a reminder the day before. I tested every page. Check the preview, then publish.
<div class="site"><span class="dot" aria-hidden="true"></span><b>lamsa.renoxium.app</b><span>preview ready</span></div></div>
<div class="who right">You</div>
<div class="msg me">Close on Fridays until 2pm.</div>
</div></div>

<section aria-labelledby="kinds"><h2 id="kinds">Start from a site that already works</h2>
<p class="sub">Each template is a finished website with payments, customer accounts and an admin panel. Your assistant changes the names, prices and texts to yours. Open any demo and use it like a customer.</p>
<div class="kinds">{{range $i, $c := .Cards}}{{if lt $i 6}}<div class="kind"><h3>{{.Title}}</h3><p>{{.Summary}}</p><p class="for">{{range $i, $f := .For}}{{if $i}}, {{end}}{{$f}}{{end}}</p><a href="{{.DemoURL}}/">Try the demo</a></div>{{end}}{{end}}</div>
<p style="margin-top:20px"><a href="/templates">All templates, with the owner's admin panel of each</a></p></section>

<section aria-labelledby="how"><h2 id="how">From a message to your first order</h2><p class="sub">You talk, the assistant builds, you decide what goes live.</p><ol class="steps">
<li><h3>Connect Renoxium</h3><p>Add Renoxium to ChatGPT or Claude once and sign in. It takes two minutes.</p></li>
<li><h3>Describe your business</h3><p>Name, what you sell or book, prices, delivery or opening hours. The assistant builds it and tests every page.</p><q>Add a gift set for AED 320.</q></li>
<li><h3>Check, publish, sell</h3><p>Open the private preview on your phone. When you are happy, press Publish. Connect Stripe to take cards.</p></li>
</ol></section>

<section aria-labelledby="day"><div class="day"><div><h2 id="day">Run it from your phone</h2>
<p class="sub">Every site comes with an admin panel made from your own data: products, orders, bookings, customers. Change a price yourself, or ask your assistant.</p>
<ul><li>New orders and bookings by email and WhatsApp</li><li>A calendar for appointments, crews and rentals</li><li>Card sales today, this month and the last 30 days</li><li>Staff who can help, without seeing your payments</li><li>A daily report at closing time</li></ul></div>
<div class="panelmock" aria-label="Example admin panel"><div class="top"><b>lamsa-salon</b><span class="pill">Live</span></div>
<div class="figs"><div><small>Today</small><b>AED 640</b></div><div><small>Bookings</small><b>9</b></div><div><small>Customers</small><b>214</b></div></div>
<div class="sched"><div><time>10:00</time><span>Haircut &amp; blow-dry · Mariam</span><em>Paid</em></div><div><time>11:30</time><span>Root colour · Huda</span><em>Paid</em></div><div><time>13:00</time><span>Gel manicure · Sara</span><em class="wait">Deposit due</em></div></div>
<div class="wa"><small>WhatsApp · Renoxium</small>New booking: Classic manicure tomorrow 16:00 with Noor, AED 70.</div></div></div></section>

<section aria-labelledby="built"><h2 id="built">Built in, on every plan</h2><p class="sub">The parts small businesses usually pay a developer for, already working and tested.</p><div class="groups">
<div><h3>Getting paid</h3><ul><li><strong>Cards into your own Stripe</strong><span>Customers pay on Stripe's page; the money goes to your account.</span></li><li><strong>Cash on delivery and pay at the venue</strong><span>Works without Stripe.</span></li><li><strong>Deposits, card holds, refunds and monthly plans</strong><span>Refund from your panel in one step.</span></li><li><strong>AED with 5% VAT</strong><span>Tax invoices once you add your TRN.</span></li></ul></div>
<div><h3>Your customers</h3><ul><li><strong>Accounts with email or a WhatsApp code</strong><span>They see their orders and bookings and pay again if a card failed.</span></li><li><strong>English and Arabic</strong><span>Right-to-left layout, switched with one tap.</span></li><li><strong>Private document uploads</strong><span>Passport or licence copies only you can open.</span></li><li><strong>Ask ChatGPT about your shop</strong><span>Customers can add your site to their AI assistant to find products.</span></li></ul></div>
<div><h3>Running the business</h3><ul><li><strong>Admin panel on any phone</strong><span>Products, photos, orders, customers.</span></li><li><strong>Calendar</strong><span>Day and week views by stylist, crew or item.</span></li><li><strong>Staff access</strong><span>Helpers manage orders without your payments or documents.</span></li><li><strong>Notices and reports</strong><span>New orders on WhatsApp, a daily summary by email.</span></li></ul></div>
<div><h3>Safe to change</h3><ul><li><strong>Preview first</strong><span>Every change goes to a private preview. Nothing is live until you press Publish.</span></li><li><strong>Roll back</strong><span>Go back to an earlier version; live data is backed up first.</span></li><li><strong>Your own domain</strong><span>www.yourshop.ae on Pro and Business.</span></li><li><strong>Your data stays yours</strong><span>Download everything, or delete your account and all of it.</span></li></ul></div>
</div></section>

<section id="pricing" aria-labelledby="pricing-h"><h2 id="pricing-h">Pricing</h2><p class="sub">Start free. Upgrade when you need more sites, your own domain or no fee on card payments.</p><div class="prices">
<div class="plan"><h3>Free</h3><p class="for">To try it with real customers</p><p class="amount">AED 0</p><ul><li>1 site</li><li>Renoxium takes {{.Fee}}% of card payments</li><li>50 WhatsApp sign-in codes a month</li><li>200 MB of images</li><li>Small "Made with Renoxium" badge</li></ul><a class="btn quiet" href="/signup">Start free</a></div>
<div class="plan pick"><h3>Pro</h3><p class="for">For one growing business</p><p class="amount">AED 99 <span>/ month</span></p><ul><li>3 sites</li><li>No Renoxium fee on card payments</li><li>500 WhatsApp sign-in codes a month</li><li>2 GB of images</li><li>Your own domain, 2 staff per site</li><li>No badge</li></ul><a class="btn" href="/signup">Choose Pro</a></div>
<div class="plan"><h3>Business</h3><p class="for">For several branches or brands</p><p class="amount">AED 299 <span>/ month</span></p><ul><li>10 sites</li><li>No Renoxium fee on card payments</li><li>2,000 WhatsApp sign-in codes a month</li><li>10 GB of images</li><li>Your own domain, 10 staff per site</li><li>No badge</li></ul><a class="btn quiet" href="/signup">Choose Business</a></div>
</div><p class="note">Stripe charges its own card processing fees to your Stripe account on every plan. Building and changing your site through your AI assistant is unlimited on every plan; you need your own ChatGPT or Claude account.</p></section>

<section aria-labelledby="faq"><h2 id="faq">Questions owners ask</h2><div class="faq">
<details><summary>Do I need to know anything technical?</summary><p>No. You describe your business in your own words, in English or Arabic. The assistant does the building and testing, and you check the result in a preview before it goes live.</p></details>
<details><summary>Which AI assistant do I need?</summary><p>ChatGPT (a paid plan with developer mode) or Claude, with your own account. You add Renoxium to it once; the <a href="/connect">connect page</a> shows the steps.</p></details>
<details><summary>What if the assistant makes a mistake?</summary><p>Changes always go to a private preview first, and only you can publish. If something slips through, roll back to an earlier version from your panel; live data is backed up before every publish.</p></details>
<details><summary>Do I need Stripe?</summary><p>Only for card payments. Cash on delivery and pay-at-the-venue work without it. When you connect Stripe, payments go straight to your own Stripe account.</p></details>
<details><summary>Can I use my own domain?</summary><p>Yes, on Pro and Business, for example www.yourshop.ae. On Free your site lives at yourname.renoxium.app.</p></details>
<details><summary>Who owns the customers and orders?</summary><p>You do. Download everything your site holds at any time, or delete your account and all of its data.</p></details>
</div></section>

<section class="end"><h2>See it working before you sign up</h2><p class="sub">Book an appointment, order a perfume or request a viewing on a demo, then open the owner's panel to see what you would get.</p>
<div class="cta"><a class="btn" href="/templates">Try the templates</a><a class="btn quiet" href="/signup">Create your account</a></div></section>

<footer><span>© {{.Legal.Company}}</span><div class="links"><a href="/templates">Templates</a><a href="/connect">Connect</a><a href="/privacy">Privacy</a><a href="/terms">Terms</a><a href="/abuse">Report a site</a><a href="/login">Sign in</a></div></footer>
</div></body></html>{{end}}

{{define "gallery"}}{{template "lhead" "Templates · Renoxium"}}<style>
.intro{padding:24px 0 8px}.intro h1{font-size:clamp(32px,5vw,48px);line-height:1.08;letter-spacing:-.025em;margin:0 0 14px}
.intro p{font-size:18px;color:var(--soft);max-width:38em;margin:0}
.cards{display:grid;gap:18px;padding:32px 0 64px}@media(min-width:760px){.cards{grid-template-columns:1fr 1fr}}@media(min-width:1040px){.cards{grid-template-columns:repeat(3,1fr)}}
.tcard{background:var(--panel);border:1px solid var(--rule);border-radius:16px;padding:22px;display:flex;flex-direction:column;gap:10px}
.tcard h2{font-size:21px;margin:0;letter-spacing:-.01em}.tcard p{margin:0;color:var(--soft);font-size:15.5px}
.tags{display:flex;flex-wrap:wrap;gap:6px;list-style:none;padding:0;margin:2px 0}.tags li{font-size:13px;padding:2px 10px;border-radius:999px;background:var(--bubble);color:var(--ink)}
.actions{display:flex;flex-wrap:wrap;gap:8px 14px;align-items:center;margin-top:auto;padding-top:8px}.actions .btn{padding:10px 16px;font-size:15px}
details{font-size:14px;color:var(--soft)}details summary{cursor:pointer;color:var(--blue)}details code{display:block;margin-top:6px;padding:8px 10px;border-radius:8px;background:var(--paper);border:1px solid var(--rule);color:var(--ink);white-space:normal;font-size:13.5px}
.how{border-top:1px solid var(--rule);padding:40px 0 56px}.how h2{font-size:26px;margin:0 0 10px}.how p{color:var(--soft);max-width:44em;margin:0 0 16px}
footer{border-top:1px solid var(--rule);padding:28px 0 48px;font-size:14px;color:var(--soft);display:flex;gap:18px;flex-wrap:wrap;justify-content:space-between}footer .links{display:flex;gap:18px;flex-wrap:wrap}
</style></head><body><div class="wrap">
<nav><a class="logo" href="/">Renoxium</a><div class="links"><a href="/templates" aria-current="page">Templates</a><a href="/#pricing">Pricing</a><a href="/connect">Connect</a><a href="/login">Sign in</a></div></nav>
<div class="intro"><h1>Templates you can try right now</h1>
<p>Every template is a finished website for a UAE business: payments, bookings, customer accounts, English and Arabic. Open the demo and use it like a customer, then look at the owner's admin panel. Nothing is charged or sent; demo data is deleted every hour.</p></div>
<div class="cards">{{range .Cards}}<article class="tcard"><h2>{{.Title}}</h2><p>{{.Summary}}</p>
<ul class="tags" aria-label="Suits">{{range .For}}<li>{{.}}</li>{{end}}</ul>
<div class="actions"><a class="btn" href="{{.DemoURL}}/">Try the demo</a><a href="{{.AdminURL}}">Owner's admin panel</a></div>
<details><summary>Use this template</summary><p>Connect Renoxium to ChatGPT or Claude, then ask:</p><code>{{.Prompt}}</code></details></article>{{end}}</div>
<section class="how"><h2>Make it yours</h2><p>Connect Renoxium to ChatGPT or Claude and ask for the template by name with your business name, services and prices. Your assistant builds your own copy, you check the preview and publish it.</p>
<a class="btn" href="/signup">Create your account</a> <a class="btn quiet" href="/connect">Connect ChatGPT or Claude</a></section>
<footer><span>© {{.Legal.Company}}</span><div class="links"><a href="/connect">Connect</a><a href="/privacy">Privacy</a><a href="/terms">Terms</a><a href="/abuse">Report a store</a><a href="/login">Sign in</a></div></footer>
</div></body></html>{{end}}

{{define "legaltop"}}<style>.doc{max-width:720px;margin:0 auto;padding:8px 20px 64px}.doc h1{font-size:34px;letter-spacing:-.02em;margin:24px 0 8px}
.doc h2{font-size:20px;margin:32px 0 8px}.doc p,.doc li{color:var(--ink)}.doc .meta{color:var(--soft)}
.draft{background:#fff3cd;color:#5c4400;border-radius:10px;padding:12px 16px;margin:16px 0}</style></head><body><div class="wrap"><nav><a class="logo" href="/">Renoxium</a><div class="links"><a href="/privacy">Privacy</a><a href="/terms">Terms</a></div></nav></div><main class="doc">
{{if not .Legal.Reviewed}}<p class="draft">Draft. This text has not yet been reviewed by a lawyer and may change.</p>{{end}}{{end}}

{{define "privacy"}}{{template "lhead" "Privacy Policy · Renoxium"}}{{template "legaltop" .}}
<h1>Privacy Policy</h1><p class="meta">{{.Legal.Company}}{{with .Legal.Address}}, {{.}}{{end}}</p>
<p>Renoxium is a platform that hosts online stores and other web apps that store owners build with AI assistants. This policy explains what personal data Renoxium handles, why, and the choices you have. "Store owner" means a person with a Renoxium account; "shopper" means a visitor of a store hosted on Renoxium.</p>
<h2>Data we handle</h2><ul>
<li>Store owners: email address, password (stored only as a one-way hash), plan and billing status, and the stores you build, including their content and settings.</li>
<li>Shoppers: what a store collects, for example name, email, mobile number, delivery address and orders; sign-in details if a shopper creates an account (password stored only as a one-way hash, or a WhatsApp number used for sign-in codes).</li>
<li>Payments: card payments are made on Stripe's pages. Renoxium never sees full card numbers; it receives the payment's status, amount and the buyer's name and email from Stripe.</li>
<li>Technical data: request logs and error reports from stores, kept to run and secure the service and to help owners fix their stores.</li></ul>
<h2>Why we use it</h2><p>To provide and secure the service, to send account and store emails (such as order confirmations and password resets), to process subscriptions for paid plans, and to meet legal obligations. We do not sell personal data and do not use it for advertising.</p>
<h2>Who processes it for us</h2><p>Stripe (payments and subscriptions), Meta (WhatsApp sign-in codes), Resend (email delivery), Cloudflare (network and security) and our hosting provider. Each processes data only to provide its service.</p>
<h2>Store owners are responsible for their stores</h2><p>For data collected by a store, the store owner decides what is collected and why, and Renoxium processes it on the owner's behalf. Shoppers should contact the store first about their orders and data.</p>
<h2>How long we keep it</h2><p>As long as an account or store exists. Store owners can delete stores and their account at any time; backups that contain deleted data are overwritten within 14 days.</p>
<h2>Your choices and rights</h2><p>Store owners can download all data of their stores and delete their account from the Account page. Anyone can ask us to access, correct or delete their personal data{{with .Legal.Email}} by writing to <a href="mailto:{{.}}">{{.}}</a>{{end}}. Renoxium uses only the cookies needed to keep you signed in and to protect forms.</p>
<h2>Changes</h2><p>We will post changes on this page and tell store owners by email about important ones.</p></main></body></html>{{end}}

{{define "terms"}}{{template "lhead" "Terms of Service · Renoxium"}}{{template "legaltop" .}}
<h1>Terms of Service</h1><p class="meta">{{.Legal.Company}}{{with .Legal.Address}}, {{.}}{{end}}</p>
<p>These terms apply when you create a Renoxium account or use a store hosted on Renoxium. By creating an account you agree to them.</p>
<h2>The service</h2><p>Renoxium lets you build web apps, such as online stores, through AI assistants you use with your own accounts (for example ChatGPT or Claude), and hosts them. AI assistants can make mistakes: check every change in the preview before you publish it.</p>
<h2>Your account</h2><p>Keep your password safe and your email address current. You are responsible for what is done with your account and the agents you connect to it.</p>
<h2>Your store</h2><p>You are the seller in your store. You are responsible for your products, prices, descriptions, taxes and VAT, deliveries, refunds and customer service, and for following the laws that apply to your business, including consumer and data protection law.</p>
<h2>Payments</h2><p>Card payments go to your own Stripe account and are subject to Stripe's terms and fees. On the Free plan, Renoxium takes {{.Fee}}% of each card payment as a platform fee. Renoxium does not hold shoppers' money.</p>
<h2>Plans and billing</h2><p>Paid plans are billed monthly in advance through Stripe and renew until you cancel. You can cancel at any time in Plan and billing; the plan stays active until the end of the paid month. Prices may change with 30 days' notice.</p>
<h2>Acceptable use</h2><p>Do not use Renoxium for illegal goods or services, fraud, phishing, malware, content that infringes others' rights, or to send spam. We may suspend a store or account that breaks these rules or that is reported and found to be harmful, and we will tell you why.</p>
<h2>Availability and liability</h2><p>We work to keep the service running and your data safe, but the service is provided as it is, without a guarantee that it will always be available or error-free. To the extent the law allows, Renoxium is not liable for indirect losses or lost profits, and its total liability is limited to the fees you paid in the three months before the claim.</p>
<h2>Ending</h2><p>You can delete your account at any time. We may end the service for an account with notice, or right away for serious breaches of these terms.</p>
<h2>Law</h2><p>These terms are governed by the laws of the United Arab Emirates.{{with .Legal.Email}} Questions: <a href="mailto:{{.}}">{{.}}</a>.{{end}}</p></main></body></html>{{end}}
`))
