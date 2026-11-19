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
	return map[string]any{"MCP": s.base + "/mcp", "Fee": 2, "Legal": s.legalOrDefaults(), "Paths": s.sitePaths(s.galleryCards()),
		"Calendar": screens["admin-calendar"]}
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

// The public pages share one small design system: navy ink on white with
// mist bands, gold only for the hero rule and the Pro plan, one family
// (Readex Pro) for English and Arabic, and real screenshots of the demos.
var landingPages = template.Must(template.New("").Parse(`
{{define "lhead"}}<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.}}</title><meta name="description" content="Websites for UAE small businesses that take orders, bookings and payments, built by describing your business to ChatGPT or Claude: online stores, bookings, rentals, quotes and listings, with card payments, cash on delivery, English and Arabic.">
<meta name="theme-color" content="#13203a">
<link rel="preconnect" href="https://fonts.googleapis.com"><link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
<link href="https://fonts.googleapis.com/css2?family=Readex+Pro:wght@400;500;600&display=swap" rel="stylesheet">
<style>
:root{color-scheme:light dark;--paper:#ffffff;--mist:#eef1f5;--ink:#13203a;--slate:#4b5870;--gold:#b8892e;--line:#d6dce5;--on-ink:#ffffff;
--shot-shadow:0 1px 2px rgb(19 32 58 / .08),0 24px 48px -24px rgb(19 32 58 / .45)}
@media (prefers-color-scheme:dark){:root{--paper:#0b1424;--mist:#121d33;--ink:#e7ecf4;--slate:#a3afc4;--gold:#d2a955;--line:#263552;--on-ink:#0b1424;
--shot-shadow:0 24px 48px -24px rgb(0 0 0 / .8)}}
*{box-sizing:border-box}html{-webkit-text-size-adjust:100%}
body{margin:0;background:var(--paper);color:var(--ink);font:17px/1.6 "Readex Pro",system-ui,-apple-system,"Segoe UI",sans-serif;-webkit-font-smoothing:antialiased;overflow-wrap:break-word}
img{max-width:100%;height:auto;display:block}
a{color:var(--ink);text-decoration-color:var(--slate);text-decoration-thickness:1px;text-underline-offset:4px}
a:hover{text-decoration-color:var(--gold);text-decoration-thickness:2px}
:focus-visible{outline:2px solid var(--ink);outline-offset:3px;border-radius:4px}
h1,h2,h3{font-weight:600;margin:0;text-wrap:balance}
h2{font-size:clamp(28px,3vw,36px);line-height:1.15;letter-spacing:-.02em}
h3{font-size:22px;line-height:1.25;letter-spacing:-.01em}
.wrap{max-width:1200px;margin:0 auto;padding:0 20px}
.skip{position:absolute;left:-9999px;top:12px}.skip:focus{left:16px;z-index:20;background:var(--paper);color:var(--ink);padding:8px 14px;border:1px solid var(--ink);border-radius:8px}
nav.top{display:flex;align-items:center;justify-content:space-between;gap:8px 20px;padding:20px 0;flex-wrap:wrap}
.logo{font-weight:600;font-size:21px;color:var(--ink);text-decoration:none;letter-spacing:-.02em}
nav.top .links{display:flex;align-items:center;gap:4px 22px;font-size:15px;flex-wrap:wrap}
nav.top .links a{color:var(--slate);text-decoration:none}nav.top .links a:hover{color:var(--ink)}
nav.top .links a[aria-current]{color:var(--ink);text-decoration:underline;text-decoration-color:var(--gold);text-decoration-thickness:2px}
nav.top .links a.btn{color:var(--on-ink)}
@media(max-width:600px){nav.top .links .wide{display:none}nav.top .links{gap:4px 16px}}
.btn{display:inline-flex;align-items:center;justify-content:center;min-height:48px;padding:12px 22px;border-radius:8px;font:inherit;font-weight:500;font-size:16px;line-height:1.2;text-decoration:none;cursor:pointer;background:var(--ink);color:var(--on-ink);border:1px solid var(--ink)}
.btn:hover{background:color-mix(in srgb,var(--ink) 86%,var(--paper))}
.btn.line{background:transparent;color:var(--ink);border-color:var(--slate)}.btn.line:hover{border-color:var(--ink);background:var(--mist)}
.btn.sm{min-height:40px;padding:8px 16px;font-size:15px}
.cta{display:flex;gap:12px;flex-wrap:wrap;align-items:center}
.band{background:var(--mist)}
.block{padding:clamp(64px,8vw,104px) 0}
.sub{color:var(--slate);max-width:36em;margin:12px 0 0;font-size:19px;line-height:1.55}
.shot{border-radius:22px;border:1px solid var(--line);box-shadow:var(--shot-shadow);background:var(--paper)}
footer.site{border-top:1px solid var(--line);padding:32px 0 48px;font-size:15px;color:var(--slate);display:flex;gap:14px 24px;flex-wrap:wrap;justify-content:space-between;align-items:center}
footer.site .links{display:flex;gap:6px 20px;flex-wrap:wrap}footer.site a{color:var(--slate)}footer.site a:hover{color:var(--ink)}
</style>{{end}}

{{define "nav"}}<a class="skip" href="#main">Skip to content</a><header class="wrap"><nav class="top" aria-label="Main"><a class="logo" href="/">Renoxium</a><div class="links">
<a href="/templates"{{if eq . "templates"}} aria-current="page"{{end}}>Templates</a><a class="wide" href="/#pricing">Pricing</a><a class="wide" href="/connect">Connect</a><a href="/login">Sign in</a><a class="btn sm" href="/signup">Create account</a></div></nav></header>{{end}}

{{define "footer"}}<div class="wrap"><footer class="site"><span>© {{.Company}}</span><div class="links"><a href="/templates">Templates</a><a href="/connect">Connect</a><a href="/privacy">Privacy</a><a href="/terms">Terms</a><a href="/abuse">Report a site</a><a href="/login">Sign in</a></div></footer></div>{{end}}

{{define "landing"}}{{template "lhead" "Renoxium: orders, bookings and payments on your own website"}}<style>
.hero{padding:clamp(32px,6vw,80px) 0 0}
.sign{display:grid;grid-template-columns:1fr 1px 1fr;column-gap:clamp(32px,4vw,64px);align-items:start;font-size:clamp(34px,4.3vw,60px)}
.sign .en{line-height:1.08;letter-spacing:-.03em}
.sign .ar{font-size:.9em;line-height:1.4;letter-spacing:0;text-align:right;font-weight:600}
.sign .rule{align-self:stretch;background:var(--gold)}
@media(max-width:760px){.sign{grid-template-columns:1fr;row-gap:20px}.sign .rule{height:2px;width:56px;justify-self:end}}
.how-it{font-size:19px;line-height:1.55;color:var(--slate);max-width:40em;margin:clamp(28px,4vw,44px) 0 28px}
.ways{display:grid;gap:64px;margin-top:clamp(64px,8vw,104px)}
@media(min-width:900px){.ways{grid-template-columns:repeat(3,1fr);gap:48px}}
.way .shot{width:100%;max-width:300px;aspect-ratio:600/1040;object-fit:cover;object-position:top}
.way h2{font-size:26px;margin:32px 0 8px}
.way p{margin:0;color:var(--slate);max-width:28em}
.way ul{list-style:none;padding:0;margin:14px 0 0;display:grid;gap:6px}
.also{margin:64px 0 0;color:var(--slate);border-top:1px solid var(--line);padding-top:24px}
.split{display:grid;gap:48px;align-items:start}@media(min-width:960px){.split{grid-template-columns:1fr 1fr;gap:80px}}
.steps{list-style:none;padding:0;margin:40px 0 0;display:grid;gap:28px;counter-reset:s}
.steps li{counter-increment:s;display:grid;grid-template-columns:40px 1fr;gap:4px}
.steps li::before{content:counter(s);font-size:24px;font-weight:600;line-height:1.2;color:var(--slate)}
.steps h3{font-size:20px;margin:0 0 4px}.steps p{margin:0;color:var(--slate)}
.chat{margin:0;background:var(--paper);border:1px solid var(--line);border-radius:12px;padding:24px;display:flex;flex-direction:column;gap:14px;font-size:16px;max-width:540px}
.chat .me{align-self:flex-end;max-width:82%;background:var(--mist);border-radius:18px;padding:10px 16px;margin:0}
.chat .me[lang=ar]{direction:rtl}
.chat .used{font-size:14px;color:var(--slate);margin:6px 0 -8px}
.chat .reply{margin:0;max-width:92%}
.chat figcaption{font-size:14px;color:var(--slate);border-top:1px solid var(--line);padding-top:12px;margin-top:4px}
.panel-shot{margin:40px 0 0}.panel-shot img{border-radius:12px;border:1px solid var(--line);width:100%}
.panel-shot figcaption{font-size:15px;color:var(--slate);margin-top:12px;max-width:48em}
.facts{margin:40px 0 0;display:grid;column-gap:64px}@media(min-width:900px){.facts{grid-template-columns:1fr 1fr}}
.facts div{display:grid;grid-template-columns:minmax(150px,38%) 1fr;gap:4px 20px;padding:16px 0;border-top:1px solid var(--line)}
@media(max-width:520px){.facts div{grid-template-columns:1fr}}
.facts dt{font-weight:500}.facts dd{margin:0;color:var(--slate)}
.prices{display:grid;gap:16px;margin-top:40px}@media(min-width:900px){.prices{grid-template-columns:repeat(3,1fr);gap:24px}}
.plan{padding:28px;border:2px solid transparent;border-radius:12px;display:flex;flex-direction:column}
.plan.pick{border-color:var(--gold);background:var(--paper)}
@media(max-width:899px){.plan{border-color:var(--line)}.plan.pick{border-color:var(--gold)}}
.plan h3{font-size:22px}.plan .for{color:var(--slate);font-size:16px;margin:4px 0 0}
.amount{font-size:40px;font-weight:600;letter-spacing:-.02em;margin:20px 0 0;line-height:1.1}.amount span{font-size:16px;font-weight:400;color:var(--slate);letter-spacing:0}
.plan ul{padding:0;margin:20px 0 28px;list-style:none;display:grid;gap:8px;font-size:16px}
.plan .btn{margin-top:auto}.note{color:var(--slate);font-size:16px;margin:32px 0 0;max-width:48em}
.faq{max-width:820px;margin-top:32px;border-bottom:1px solid var(--line)}
.faq details{border-top:1px solid var(--line)}
.faq summary{cursor:pointer;font-weight:500;font-size:19px;padding:18px 0;list-style:none;display:flex;justify-content:space-between;gap:16px}
.faq summary::-webkit-details-marker{display:none}
.faq summary::after{content:"+";color:var(--slate);font-weight:400}.faq details[open] summary::after{content:"\2212"}
.faq p{margin:0 0 20px;color:var(--slate);max-width:44em}
.end .cta{margin-top:28px}
</style></head><body>
{{template "nav" "home"}}
<main id="main">
<section class="hero" aria-labelledby="hero-h"><div class="wrap">
<h1 id="hero-h" class="sign"><span class="en">Take orders and bookings on your own website, not in WhatsApp.</span><span class="rule" aria-hidden="true"></span><span class="ar" lang="ar" dir="rtl">استقبل الطلبات والحجوزات على موقعك الخاص، لا في واتساب.</span></h1>
<p class="how-it">Describe your business to ChatGPT or Claude. Renoxium builds the site from a finished UAE template and hosts it, with card payments, cash on delivery and prices in AED with VAT.</p>
<div class="cta"><a class="btn" href="/signup">Create your account</a><a class="btn line" href="/templates">Try a demo</a></div>
<div class="ways">{{range .Paths}}<section class="way" id="{{.ID}}" aria-labelledby="{{.ID}}-h">
<img class="shot" src="{{.Screen.Src}}" width="{{.Screen.W}}" height="{{.Screen.H}}" alt="{{.Screen.Alt}}">
<h2 id="{{.ID}}-h">{{.Title}}</h2><p>{{.Text}}</p>
<ul>{{range .Demos}}<li><a href="{{.URL}}">{{.Label}}</a></li>{{end}}</ul></section>{{end}}</div>
<p class="also">Delivering too? <a href="/templates#delivery-tracking">Live delivery tracking</a> lets customers follow the courier on a map. Or <a href="/templates">see every template</a> with its owner's admin panel.</p>
</div></section>

<section class="block" aria-labelledby="how-h"><div class="wrap split">
<div><h2 id="how-h">How it works</h2><p class="sub">You describe, the assistant builds, you decide what goes live.</p>
<ol class="steps">
<li><div><h3>Connect Renoxium</h3><p>Add Renoxium to ChatGPT or Claude once and sign in. The <a href="/connect">connect page</a> shows the steps.</p></div></li>
<li><div><h3>Describe your business</h3><p>Your name, what you sell or book, prices, delivery areas or opening hours, in English or Arabic. The assistant builds the site and tests every page.</p></div></li>
<li><div><h3>Check the preview and publish</h3><p>Open the private preview on your phone. When you are happy, press Publish. Connect Stripe to take cards.</p></div></li>
</ol></div>
<figure class="chat">
<p class="me">Make a booking site for Lamsa, my salon in Jumeirah. Haircuts, colour and nails, three stylists.</p>
<p class="me" lang="ar">وأريد عربون ٥٠ درهم عند الحجز</p>
<p class="used">Used Renoxium</p>
<p class="reply">Done. Customers pick a service, a stylist and a free time, and pay a AED 50 deposit by card. I tested every page. Your private preview is at lamsa.renoxium.app; check it on your phone, then tell me to publish.</p>
<p class="me">Close on Fridays until 2pm.</p>
<figcaption>An example conversation in ChatGPT with Renoxium connected.</figcaption></figure>
</div></section>

<section class="block band" aria-labelledby="panel-h"><div class="wrap">
<h2 id="panel-h">Every order and booking in one panel</h2>
<p class="sub">Your site comes with an admin panel made from your own data. Change a price or a photo yourself, on your phone or computer, or ask your assistant.</p>
<figure class="panel-shot"><img src="{{.Calendar.Src}}" width="{{.Calendar.W}}" height="{{.Calendar.H}}" alt="{{.Calendar.Alt}}" loading="lazy">
<figcaption>The bookings calendar of the salon demo, one lane per stylist. <a href="/templates/booking-uae/admin">Open the demo's admin panel</a>; it is read-only.</figcaption></figure>
</div></section>

<section class="block" aria-labelledby="has-h"><div class="wrap">
<h2 id="has-h">What every site has, on every plan</h2>
<dl class="facts">
<div><dt>Card payments</dt><dd>Customers pay on Stripe's page and the money goes to your own Stripe account.</dd></div>
<div><dt>Cash on delivery</dt><dd>Or pay at the venue. Works without Stripe.</dd></div>
<div><dt>Deposits and refunds</dt><dd>Take a deposit when someone books, and refund from your panel in one step.</dd></div>
<div><dt>AED with 5% VAT</dt><dd>Tax invoices once you add your TRN.</dd></div>
<div><dt>English and Arabic</dt><dd>Right-to-left layout in Arabic, switched with one tap.</dd></div>
<div><dt>Customer sign-in</dt><dd>By email or a code sent on WhatsApp. Customers see their orders and bookings.</dd></div>
<div><dt>Admin panel</dt><dd>Products, photos, orders and customers, on any phone.</dd></div>
<div><dt>Calendar</dt><dd>Day and week views, one lane per stylist, crew or item.</dd></div>
<div><dt>Staff access</dt><dd>Helpers manage orders without seeing your payments or documents.</dd></div>
<div><dt>New order alerts</dt><dd>On WhatsApp and by email, with a daily summary.</dd></div>
<div><dt>Preview first</dt><dd>Nothing goes live until you press Publish, and you can roll back to an earlier version.</dd></div>
<div><dt>Your own domain</dt><dd>www.yourshop.ae on Pro and Business.</dd></div>
</dl></div></section>

<section class="block band" id="pricing" aria-labelledby="pricing-h"><div class="wrap">
<h2 id="pricing-h">Pricing</h2><p class="sub">Start free. Upgrade when you need more sites, your own domain or no fee on card payments.</p>
<div class="prices">
<div class="plan"><h3>Free</h3><p class="for">To try it with real customers</p><p class="amount">AED 0</p><ul><li>1 site</li><li>Renoxium takes {{.Fee}}% of card payments</li><li>50 WhatsApp sign-in codes a month</li><li>200 MB of images</li><li>Small "Made with Renoxium" badge on your site</li></ul><a class="btn line" href="/signup">Create your account</a></div>
<div class="plan pick"><h3>Pro</h3><p class="for">For one growing business</p><p class="amount">AED 99 <span>a month</span></p><ul><li>3 sites</li><li>No Renoxium fee on card payments</li><li>500 WhatsApp sign-in codes a month</li><li>2 GB of images</li><li>Your own domain, 2 staff per site</li><li>No badge</li></ul><a class="btn" href="/signup">Choose Pro</a></div>
<div class="plan"><h3>Business</h3><p class="for">For several branches or brands</p><p class="amount">AED 299 <span>a month</span></p><ul><li>10 sites</li><li>No Renoxium fee on card payments</li><li>2,000 WhatsApp sign-in codes a month</li><li>10 GB of images</li><li>Your own domain, 10 staff per site</li><li>No badge</li></ul><a class="btn line" href="/signup">Choose Business</a></div>
</div><p class="note">Stripe charges its own card fees to your Stripe account on every plan. Building and changing your site through your assistant is unlimited on every plan; you need your own ChatGPT or Claude account.</p></div></section>

<section class="block" aria-labelledby="faq-h"><div class="wrap"><h2 id="faq-h">Questions owners ask</h2><div class="faq">
<details><summary>Do I need to know anything technical?</summary><p>No. You describe your business in your own words, in English or Arabic. The assistant does the building and testing, and you check the result in a preview before it goes live.</p></details>
<details><summary>Which AI assistant do I need?</summary><p>ChatGPT (a paid plan with developer mode) or Claude, with your own account. You add Renoxium to it once; the <a href="/connect">connect page</a> shows the steps.</p></details>
<details><summary>I already sell on WhatsApp and Instagram. Why a site?</summary><p>Keep chatting with customers there. The site answers the price and availability questions for you, takes the order, booking or deposit, and puts it in one list, so nothing gets lost in a chat. Share its link in your bio and your replies.</p></details>
<details><summary>What if the assistant makes a mistake?</summary><p>Changes always go to a private preview first, and only you can publish. If something slips through, roll back to an earlier version from your panel; live data is backed up before every publish.</p></details>
<details><summary>Do I need Stripe?</summary><p>Only for card payments. Cash on delivery and pay at the venue work without it. When you connect Stripe, payments go straight to your own Stripe account.</p></details>
<details><summary>Can I use my own domain?</summary><p>Yes, on Pro and Business, for example www.yourshop.ae. On Free your site lives at yourname.renoxium.app.</p></details>
<details><summary>Who owns the customers and orders?</summary><p>You do. Download everything your site holds at any time, or delete your account and all of its data.</p></details>
</div></div></section>

<section class="block end" aria-labelledby="end-h"><div class="wrap">
<h2 id="end-h">Try a demo before you sign up</h2><p class="sub">Book a haircut, order a perfume or ask for a quote on a demo, then open the owner's admin panel to see what you would get.</p>
<div class="cta"><a class="btn" href="/templates">Try a demo</a><a class="btn line" href="/signup">Create your account</a></div></div></section>
</main>
{{template "footer" .Legal}}
</body></html>{{end}}

{{define "gallery"}}{{template "lhead" "Templates | Renoxium"}}<style>
.intro{padding:clamp(32px,5vw,64px) 0 0}.intro h1{font-size:clamp(36px,4.5vw,56px);line-height:1.08;letter-spacing:-.03em}
.group{padding:56px 0 0}.group>h2{padding-top:28px;border-top:1px solid var(--line)}
.cards{display:grid;gap:56px 48px;margin-top:36px}@media(min-width:720px){.cards{grid-template-columns:1fr 1fr}}@media(min-width:1040px){.cards{grid-template-columns:repeat(3,1fr)}}
.tcard{scroll-margin-top:24px}.tcard:target h3{text-decoration:underline;text-decoration-color:var(--gold);text-decoration-thickness:2px;text-underline-offset:6px}
.tcard .shot{width:100%;max-width:280px;aspect-ratio:600/1000;object-fit:cover;object-position:top}
.tcard h3{margin:28px 0 8px}.tcard p{margin:0 0 8px;color:var(--slate);font-size:16px}
.tlinks{display:flex;flex-wrap:wrap;gap:6px 24px;margin:14px 0 18px}
.prompt{font-size:15px;color:var(--slate);margin:0}
.prompt q{display:block;margin-top:6px;padding:12px 14px;border-radius:12px;background:var(--mist);color:var(--ink);quotes:none}
.mix{padding:72px 0 0}.mix p{color:var(--slate);max-width:40em;margin:12px 0 0}
.yours{margin:72px 0 0;padding:56px 0 72px;border-top:1px solid var(--line)}.yours .cta{margin-top:28px}
</style></head><body>
{{template "nav" "templates"}}
<main id="main" class="wrap">
<div class="intro"><h1>Templates</h1>
<p class="sub">Each template is a finished website for a UAE business, in English and Arabic, taking card payments or cash. Use the demo like a customer, then open the owner's admin panel. Nothing is charged or sent, and demo data is deleted every hour.</p></div>
{{range .Groups}}<section class="group" aria-label="{{.Job}}"><h2>{{.Job}}</h2><div class="cards">{{range .Cards}}<article class="tcard" id="{{.Name}}">
{{if .Screen.File}}<img class="shot" src="{{.Screen.Src}}" width="{{.Screen.W}}" height="{{.Screen.H}}" alt="{{.Screen.Alt}}" loading="lazy">{{end}}
<h3>{{.Title}}</h3><p>{{.Summary}}</p><p>For {{.Suits}}.</p>
<div class="tlinks"><a href="{{.DemoURL}}/">Try the demo</a><a href="{{.AdminURL}}">Owner's admin panel</a></div>
<p class="prompt">To build yours, ask your assistant:<q>{{.Prompt}}</q></p></article>{{end}}</div></section>{{end}}
<section class="mix" aria-labelledby="mix-h"><h2 id="mix-h">Something in between?</h2><p>Start from the closest template and tell your assistant what to change: other fields on a form, your own sections and texts, your colours and photos.</p></section>
<section class="yours" aria-labelledby="yours-h"><h2 id="yours-h">Make it yours</h2><p class="sub">Connect Renoxium to ChatGPT or Claude and ask for a template by name with your business name, services and prices. Your assistant builds your own copy; you check the preview and publish it.</p>
<div class="cta"><a class="btn" href="/signup">Create your account</a><a class="btn line" href="/connect">Connect ChatGPT or Claude</a></div></section>
</main>
{{template "footer" .Legal}}
</body></html>{{end}}

{{define "legaltop"}}<style>.doc{max-width:720px;padding:clamp(24px,5vw,56px) 0 72px}
.doc h1{font-size:clamp(32px,4vw,44px);letter-spacing:-.025em;margin:0 0 8px;line-height:1.1}
.doc h2{font-size:21px;margin:36px 0 8px;letter-spacing:-.01em}.doc .meta{color:var(--slate)}
.draft{background:var(--mist);border-radius:12px;padding:12px 16px;margin:0 0 24px}</style></head><body>
{{template "nav" "legal"}}<div class="wrap"><main class="doc" id="main">
{{if not .Legal.Reviewed}}<p class="draft">Draft. This text has not yet been reviewed by a lawyer and may change.</p>{{end}}{{end}}

{{define "privacy"}}{{template "lhead" "Privacy Policy | Renoxium"}}{{template "legaltop" .}}
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
<h2>Changes</h2><p>We will post changes on this page and tell store owners by email about important ones.</p></main></div></body></html>{{end}}

{{define "terms"}}{{template "lhead" "Terms of Service | Renoxium"}}{{template "legaltop" .}}
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
<h2>Law</h2><p>These terms are governed by the laws of the United Arab Emirates.{{with .Legal.Email}} Questions: <a href="mailto:{{.}}">{{.}}</a>.{{end}}</p></main></div></body></html>{{end}}
`))
