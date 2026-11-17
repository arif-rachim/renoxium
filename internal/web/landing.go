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
	return map[string]any{"MCP": s.base + "/mcp", "Fee": 2, "Legal": s.legalOrDefaults()}
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
<title>{{.}}</title><meta name="description" content="Build and run your online store by chatting with ChatGPT or Claude. Card payments, cash on delivery, English and Arabic, made for the UAE.">
<link rel="preconnect" href="https://fonts.googleapis.com"><link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
<link href="https://fonts.googleapis.com/css2?family=IBM+Plex+Sans+Arabic:wght@400;500;600;700&display=swap" rel="stylesheet">
<style>
:root{--ink:#14171f;--soft:#5a6170;--paper:#f5f7fa;--panel:#ffffff;--rule:#dde2ea;--blue:#2f5bea;--blue-ink:#ffffff;--live:#1f9d55;--bubble:#e9eefc}
@media (prefers-color-scheme:dark){:root{--ink:#e9ecf2;--soft:#a1a9b8;--paper:#121620;--panel:#1a1f2b;--rule:#2b3240;--blue:#7d9bff;--blue-ink:#0f1320;--live:#4cc88a;--bubble:#243055}}
*{box-sizing:border-box}html{-webkit-text-size-adjust:100%}
body{margin:0;background:var(--paper);color:var(--ink);font:17px/1.6 "IBM Plex Sans Arabic",system-ui,-apple-system,"Segoe UI",sans-serif}
a{color:var(--blue)}a:focus-visible,button:focus-visible{outline:3px solid var(--blue);outline-offset:3px;border-radius:6px}
.wrap{max-width:1080px;margin:0 auto;padding:0 20px}
nav{display:flex;align-items:center;justify-content:space-between;gap:16px;padding:20px 0}
.logo{font-weight:700;font-size:20px;color:var(--ink);text-decoration:none;letter-spacing:-.01em}
nav .links{display:flex;gap:18px;font-size:15px}nav .links a{color:var(--ink);text-decoration:none}nav .links a:hover{text-decoration:underline}
.btn{display:inline-block;background:var(--blue);color:var(--blue-ink);padding:13px 22px;border-radius:10px;font-weight:600;text-decoration:none;font-size:16px}
.btn.quiet{background:transparent;color:var(--ink);border:1px solid var(--rule)}
footer a{color:var(--soft)}
</style>{{end}}

{{define "landing"}}{{template "lhead" "Renoxium: your online store, built in a chat"}}<style>
.hero{display:grid;grid-template-columns:1fr;gap:40px;padding:32px 0 72px;align-items:center}
@media(min-width:900px){.hero{grid-template-columns:1.05fr 1fr;gap:56px;padding:56px 0 96px}}
h1{font-size:clamp(36px,6vw,58px);line-height:1.06;letter-spacing:-.025em;margin:0 0 20px;font-weight:700;max-width:12em}
.lede{font-size:19px;color:var(--soft);max-width:30em;margin:0 0 28px}
.cta{display:flex;gap:12px;flex-wrap:wrap;align-items:center}.small{font-size:14px;color:var(--soft);margin-top:14px}
.chat{background:var(--panel);border:1px solid var(--rule);border-radius:20px;padding:22px;display:flex;flex-direction:column;gap:12px;font-size:15.5px}
.msg{max-width:88%;padding:11px 15px;border-radius:16px;line-height:1.5}
.me{align-self:flex-end;background:var(--blue);color:var(--blue-ink);border-bottom-right-radius:5px}
.me.ar{direction:rtl;text-align:right}
.agent{align-self:flex-start;background:var(--bubble);border-bottom-left-radius:5px}
.who{font-size:12.5px;color:var(--soft);margin:4px 2px -6px}.who.right{align-self:flex-end}
.store{display:flex;align-items:center;gap:8px;margin-top:8px;font-weight:600}
.dot{width:9px;height:9px;border-radius:50%;background:var(--live);flex:none}
.typing{display:inline-flex;gap:4px;align-items:center;padding:4px 0}.typing i{width:6px;height:6px;border-radius:50%;background:var(--soft);opacity:.5}
section{padding:64px 0;border-top:1px solid var(--rule)}
h2{font-size:clamp(26px,3.6vw,36px);letter-spacing:-.02em;margin:0 0 28px;line-height:1.15;max-width:22em}
.steps{list-style:none;padding:0;margin:0;display:grid;gap:28px;counter-reset:s}
@media(min-width:800px){.steps{grid-template-columns:repeat(3,1fr)}}
.steps li{counter-increment:s;padding-top:52px;position:relative}
.steps li::before{content:counter(s);position:absolute;top:0;left:0;width:38px;height:38px;border-radius:50%;border:2px solid var(--blue);color:var(--blue);display:flex;align-items:center;justify-content:center;font-weight:700}
.steps h3,.gets h3{margin:0 0 6px;font-size:19px}.steps p,.gets p{margin:0;color:var(--soft)}
.gets{display:grid;gap:26px 40px}@media(min-width:760px){.gets{grid-template-columns:1fr 1fr}}
.prices{display:grid;gap:16px}@media(min-width:860px){.prices{grid-template-columns:repeat(3,1fr)}}
.plan{background:var(--panel);border:1px solid var(--rule);border-radius:16px;padding:24px;display:flex;flex-direction:column}
.plan.pick{border:2px solid var(--blue)}
.plan h3{margin:0;font-size:20px}.amount{font-size:34px;font-weight:700;letter-spacing:-.02em;margin:10px 0 2px}.amount span{font-size:16px;font-weight:500;color:var(--soft)}
.plan ul{padding:0;margin:16px 0 22px;list-style:none;display:grid;gap:8px;font-size:15.5px}
.plan li{padding-left:22px;position:relative}.plan li::before{content:"";position:absolute;left:2px;top:.55em;width:9px;height:9px;border-radius:2px;background:var(--blue);opacity:.75}
.plan .btn{margin-top:auto;text-align:center}.note{color:var(--soft);font-size:15px;margin-top:18px;max-width:46em}
footer{border-top:1px solid var(--rule);padding:28px 0 48px;font-size:14px;color:var(--soft);display:flex;gap:18px;flex-wrap:wrap;justify-content:space-between}
footer .links{display:flex;gap:18px;flex-wrap:wrap}
</style></head><body><div class="wrap">
<nav><a class="logo" href="/">Renoxium</a><div class="links"><a href="#pricing">Pricing</a><a href="/connect">Connect</a><a href="/login">Sign in</a></div></nav>

<div class="hero"><div>
<h1>Your online store, built in a chat.</h1>
<p class="lede">Tell ChatGPT or Claude what you sell. Renoxium builds the shop, hosts it, and takes card payments and cash on delivery, in English and Arabic.</p>
<div class="cta"><a class="btn" href="/signup">Create your account</a><a class="btn quiet" href="/connect">Connect ChatGPT or Claude</a></div>
<p class="small">Free to start. No code, no designer, no plugins to install on a server.</p>
</div>
<div class="chat" aria-label="Example conversation">
<div class="who right">You, in ChatGPT</div>
<div class="msg me">Build me an online store for my perfume shop, Al Noor. I sell oud, musk and amber in Dubai.</div>
<div class="msg me ar">أريد الدفع عند الاستلام أيضاً</div>
<div class="who">Renoxium</div>
<div class="msg agent">Done. Your store has a catalog, cart, card payments and cash on delivery, prices in AED with VAT, in English and Arabic. Check the preview, then publish when you are happy.
<div class="store"><span class="dot" aria-hidden="true"></span>al-noor.renoxium.app</div></div>
<div class="who right">You</div>
<div class="msg me">Add a gift set for AED 320.</div>
<div class="msg agent" aria-label="Renoxium is replying"><span class="typing"><i></i><i></i><i></i></span></div>
</div></div>

<section><h2>Three steps from idea to orders</h2><ol class="steps">
<li><h3>Connect Renoxium</h3><p>Add Renoxium to ChatGPT or Claude once and sign in. Your agent can then build and change your store for you.</p></li>
<li><h3>Describe your shop</h3><p>Name, products, prices, delivery fee. The agent starts from a finished UAE store and tests every page before you see it.</p></li>
<li><h3>Publish and sell</h3><p>Check the preview, press Publish, connect Stripe for cards. Orders and payments arrive by email; manage everything in your panel.</p></li>
</ol></section>

<section><h2>What your store comes with</h2><div class="gets">
<div><h3>Payments into your own Stripe</h3><p>Shoppers pay by card on Stripe's page, straight into your Stripe account. Cash on delivery works without Stripe.</p></div>
<div><h3>Made for the UAE</h3><p>Prices in AED with 5% VAT shown on every order, delivery fees by threshold, English and Arabic with right-to-left layout.</p></div>
<div><h3>Customer accounts</h3><p>Shoppers sign in with email or a WhatsApp code, see their orders and pay again if a card payment did not go through.</p></div>
<div><h3>Your own admin panel</h3><p>Edit products and photos, see orders, card sales and customers on your phone, without asking the agent.</p></div>
<div><h3>Change it any time</h3><p>Ask for a new page, a sale banner or a new product. Every change goes to a preview first, and you can roll back.</p></div>
<div><h3>Your data stays yours</h3><p>Download everything your store holds whenever you want, or delete your account and all of it.</p></div>
</div></section>

<section id="pricing"><h2>Pricing</h2><div class="prices">
<div class="plan"><h3>Free</h3><p class="amount">AED 0</p><ul><li>1 store</li><li>Renoxium takes {{.Fee}}% of card payments</li><li>50 WhatsApp sign-in codes a month</li><li>Small "Made with Renoxium" badge</li></ul><a class="btn quiet" href="/signup">Start free</a></div>
<div class="plan pick"><h3>Pro</h3><p class="amount">AED 99 <span>/ month</span></p><ul><li>3 stores</li><li>No Renoxium fee on card payments</li><li>500 WhatsApp sign-in codes a month</li><li>No badge</li></ul><a class="btn" href="/signup">Choose Pro</a></div>
<div class="plan"><h3>Business</h3><p class="amount">AED 299 <span>/ month</span></p><ul><li>10 stores</li><li>No Renoxium fee on card payments</li><li>2,000 WhatsApp sign-in codes a month</li><li>No badge</li></ul><a class="btn quiet" href="/signup">Choose Business</a></div>
</div><p class="note">Stripe charges its own card processing fees to your Stripe account on every plan. Building and changing your store through your AI assistant is unlimited on every plan; you need your own ChatGPT or Claude account.</p></section>

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
