package web

import (
	"html/template"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Legal holds the company details shown on the Privacy Policy and Terms.
// Until Reviewed is true, both pages say they are drafts.
type Legal struct {
	Company  string // legal name as on the trade license
	Address  string
	Email    string // contact for privacy and legal questions
	Reviewed bool
	WhatsApp string   // the company's WhatsApp number, e.g. +9715...; shown in the footer when set
	Social   []string // the company's profile pages (LinkedIn, Instagram, ...); shown in the footer and as sameAs
}

// WA is the WhatsApp number as digits, for a wa.me link.
func (l Legal) WA() string {
	var b strings.Builder
	for _, r := range l.WhatsApp {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// SocialLink is one profile page with the name of its network.
type SocialLink struct{ Name, URL string }

// Socials names each profile page by its host.
func (l Legal) Socials() []SocialLink {
	names := map[string]string{"linkedin.com": "LinkedIn", "instagram.com": "Instagram", "x.com": "X", "twitter.com": "X", "github.com": "GitHub",
		"facebook.com": "Facebook", "youtube.com": "YouTube", "tiktok.com": "TikTok"}
	var out []SocialLink
	for _, raw := range l.Social {
		u, err := url.Parse(strings.TrimSpace(raw))
		if err != nil || u.Scheme != "https" || u.Host == "" {
			continue
		}
		host := strings.TrimPrefix(strings.ToLower(u.Host), "www.")
		name := names[host]
		if name == "" {
			name = host
		}
		out = append(out, SocialLink{name, u.String()})
	}
	return out
}

func (s *Site) registerLanding() {
	s.mux.HandleFunc("GET /ar", s.landingAR)
	s.mux.HandleFunc("GET /privacy", s.legalPage("privacy"))
	s.mux.HandleFunc("GET /terms", s.legalPage("terms"))
	s.mux.HandleFunc("GET /support", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		landingPages.ExecuteTemplate(w, "support", map[string]any{"Legal": s.legalOrDefaults(), "Base": s.base, "MCP": s.base + "/mcp", "Abuse": s.AbuseEmail})
	})
	s.mux.HandleFunc("GET /.well-known/openai-apps-challenge", func(w http.ResponseWriter, r *http.Request) {
		if s.OpenAIAppsChallenge == "" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		io.WriteString(w, s.OpenAIAppsChallenge)
	})
}

func (s *Site) landingData(ar bool) map[string]any {
	cards := s.galleryCards()
	// The pricing example: what the Free plan's fee costs on AED 10,000 of
	// card sales, and the card sales above which Pro (AED 99) is cheaper.
	const fee, pro = 2, 99
	calendar := screens["admin-calendar"]
	if ar {
		calendar = screensAR["admin-calendar"]
	}
	return map[string]any{"MCP": s.base + "/mcp", "Fee": fee, "FeeOn10k": 10000 * fee / 100, "ProFrom": thousands(pro * 100 / fee),
		"Legal": s.legalOrDefaults(), "Paths": s.sitePaths(cards, ar), "Trades": s.siteTrades(cards, ar),
		"Calendar": calendar, "Base": s.base}
}

// thousands writes n with a comma between thousands: 4950 is "4,950".
func thousands(n int) string {
	out := strconv.Itoa(n)
	for i := len(out) - 3; i > 0; i -= 3 {
		out = out[:i] + "," + out[i:]
	}
	return out
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

// landingAR is the home page in Arabic, for everyone: a signed-in owner
// reaches the console from its Sign in link.
func (s *Site) landingAR(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Language", "ar")
	w.Header().Set("Cache-Control", "no-store")
	landingPages.ExecuteTemplate(w, "landing-ar", s.landingData(true))
}

// The public pages share one small design system: navy ink on white with
// mist bands, gold only for the hero rule and the Pro plan, one family
// (Readex Pro) for English and Arabic, and real screenshots of the demos.
var landingPages = template.Must(template.New("").Parse(brandDefines + `
{{define "lhead"}}<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.}}</title><meta name="description" content="UAE online store, booking and WhatsApp orders on your own website, with card payments. Built by describing your business to ChatGPT or Claude.">
{{template "lcommon"}}{{end}}

{{define "lcommon"}}{{template "icons"}}
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
.logo{display:inline-flex;align-items:center;gap:9px;font-weight:600;font-size:22px;color:var(--ink);text-decoration:none;letter-spacing:-.02em}.logo .mark{height:30px;width:auto;flex:none}
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
footer.site .links{display:flex;gap:6px 20px;flex-wrap:wrap}
footer.site .contact{flex-basis:100%;display:flex;gap:6px 20px;flex-wrap:wrap;font-style:normal;font-size:14px}footer.site a{color:var(--slate)}footer.site a:hover{color:var(--ink)}
</style>{{end}}

{{define "nav"}}<a class="skip" href="#main">Skip to content</a><header class="wrap"><nav class="top" aria-label="Main"><a class="logo" href="/" aria-label="Renoxium home">{{template "brandlogo"}}</a><div class="links">
<a href="/templates"{{if eq . "templates"}} aria-current="page"{{end}}>Templates</a><a class="wide" href="/#pricing">Pricing</a><a class="wide" href="/connect">Connect</a><a href="/ar" lang="ar">العربية</a><a href="/login">Sign in</a><a class="btn sm" href="/signup">Create account</a></div></nav></header>{{end}}

{{define "contact"}}{{if or .Email .WA .Address .Socials}}<address class="contact">{{with .Email}}<a href="mailto:{{.}}">{{.}}</a>{{end}}{{with .WA}}<a href="https://wa.me/{{.}}" rel="noopener">WhatsApp <bdi dir="ltr">{{$.WhatsApp}}</bdi></a>{{end}}{{with .Address}}<span>{{.}}</span>{{end}}{{range .Socials}}<a href="{{.URL}}" rel="me noopener">{{.Name}}</a>{{end}}</address>{{end}}{{end}}

{{define "footer"}}<div class="wrap"><footer class="site"><span>© {{.Company}}</span><div class="links"><a href="/templates">Templates</a><a href="/connect">Connect</a><a href="/privacy">Privacy</a><a href="/terms">Terms</a><a href="/support">Support</a><a href="/abuse">Report a site</a><a href="/login">Sign in</a></div>{{template "contact" .}}</footer></div>{{end}}

{{define "og"}}<meta property="og:type" content="website"><meta property="og:site_name" content="Renoxium"><meta property="og:locale" content="en_AE"><meta property="og:locale:alternate" content="ar_AE"><meta property="og:title" content="Renoxium: orders, bookings and payments on your own website">
<meta property="og:description" content="UAE online store, booking and WhatsApp orders on your own website, with card payments. Built by describing your business to ChatGPT or Claude.">
<meta property="og:image" content="{{.}}/og-image.png"><meta property="og:image:width" content="1200"><meta property="og:image:height" content="630"><meta name="twitter:card" content="summary_large_image">
<meta name="twitter:title" content="Renoxium: orders, bookings and payments on your own website"><meta name="twitter:description" content="UAE online store, booking and WhatsApp orders on your own website, with card payments. Built by describing your business to ChatGPT or Claude."><meta name="twitter:image" content="{{.}}/og-image.png">{{end}}

{{define "landing"}}{{template "lhead" "Renoxium: orders, bookings and payments on your own website"}}{{template "landing-css"}}<link rel="alternate" hreflang="en" href="{{.Base}}/"><link rel="alternate" hreflang="ar" href="{{.Base}}/ar"><link rel="alternate" hreflang="x-default" href="{{.Base}}/">{{template "og" .Base}}</head><body>
{{template "nav" "home"}}
<main id="main">
<section class="hero" aria-labelledby="hero-h"><div class="wrap">
<div class="sign"><h1 id="hero-h" class="en">Orders, bookings and payments in one place, from your website to your customers' AI assistants.</h1><span class="rule" aria-hidden="true"></span><p class="ar" lang="ar" dir="rtl">الطلبات والحجوزات والمدفوعات في مكان واحد، من موقعك إلى مساعدي الذكاء الاصطناعي لدى عملائك.</p></div>
<p class="how-it">Describe your business to ChatGPT or Claude. Renoxium builds your website and connects it to AI assistants, so customers can find you, book and pay. Every order lands in one panel, and you get a WhatsApp message for each.</p>
<div class="cta"><a class="btn" href="/signup">Create your account</a><a class="btn line" href="/templates">Try a demo</a></div>
<div class="ways">{{range .Paths}}<section class="way" id="{{.ID}}" aria-labelledby="{{.ID}}-h" data-r>
<img class="shot" src="{{.Screen.Src}}" width="{{.Screen.W}}" height="{{.Screen.H}}" alt="{{.Screen.Alt}}">
<h2 id="{{.ID}}-h">{{.Title}}</h2><p>{{.Text}}</p>
<ul>{{range .Demos}}<li><a href="{{.URL}}">{{.Label}}</a></li>{{end}}</ul></section>{{end}}</div>
<p class="also">Delivering too? <a href="/templates#delivery-tracking">Live delivery tracking</a> lets customers follow the courier on a map. Or <a href="/templates">see every template</a> with its owner's admin panel.</p>
</div></section>

<section class="block" aria-labelledby="how-h"><div class="wrap split">
<div data-r><h2 id="how-h">How it works</h2><p class="sub">You describe, the assistant builds, you decide what goes live.</p>
<ol class="steps" data-w>
<li><div><h3>Connect Renoxium</h3><p>Add Renoxium to ChatGPT or Claude once and sign in. The <a href="/connect">connect page</a> shows the steps.</p></div></li>
<li><div><h3>Describe your business</h3><p>Your name, what you sell or book, prices, delivery areas or opening hours, in English or Arabic. The assistant builds the site and tests every page.</p></div></li>
<li><div><h3>Check the preview and publish</h3><p>Open the private preview on your phone. When you are happy, press Publish. Connect Stripe to take cards.</p></div></li>
</ol></div>
<figure class="chat" data-seq>
<p class="me">Make a booking site for Lamsa, my salon in Jumeirah. Haircuts, colour and nails, three stylists.</p>
<p class="me" lang="ar">وأريد عربوناً بقيمة ٥٠ درهماً عند الحجز</p>
<p class="used">Used Renoxium</p>
<p class="reply">Done. Customers pick a service, a stylist and a free time, and pay a AED 50 deposit by card. I tested every page. Your private preview is at lamsa.renoxium.app; check it on your phone, then tell me to publish.</p>
<p class="me">Close on Fridays until 2pm.</p>
<figcaption>An example conversation in ChatGPT with Renoxium connected.</figcaption></figure>
</div></section>

<section class="block band" aria-labelledby="ai-h"><div class="wrap">
<div data-r><h2 id="ai-h">Your customers can ask their own AI assistant</h2>
<p class="sub">Every Renoxium site also answers ChatGPT and Claude with your real prices, stock and free times. The assistant sends the customer to the right page to book or pay, and the booking reaches you like any other.</p></div>
<div class="relay">
<figure class="chat" data-seq>
<p class="me">Can I get a haircut at Lamsa Beauty Lounge tomorrow after 5?</p>
<p class="used">Used Lamsa Beauty Lounge</p>
<p class="reply">Yes. Tomorrow Layla is free at 17:30 and 18:15, and Priya at 19:00. A haircut and blow-dry takes an hour and costs AED 150; a AED 50 deposit holds your time. <u>Book with Layla at Lamsa</u></p>
<figcaption>An example: a customer in ChatGPT with the salon added. She opens the link, picks 18:15 and pays the deposit on the salon's site.</figcaption></figure>
<div class="lands" data-seq>
<p class="lab">What reaches you a minute later</p>
<div class="card"><span class="from">WhatsApp message to the owner</span><p>New booking: Haircut and blow-dry, tomorrow 18:15 with Layla. Sara A., deposit AED 50 paid by card.</p></div>
<div class="card slot"><span class="t">18:15</span><span>Haircut and blow-dry</span><span class="tag">Confirmed</span><span class="who">Sara A. · Layla's lane in your calendar</span></div>
<p class="fine">The same happens when she books from your website's home page or a link in your Instagram bio.</p>
</div></div>
</div></section>

<section class="block" aria-labelledby="chan-h"><div class="wrap">
<div data-r><h2 id="chan-h">One place behind every channel</h2>
<p class="sub">Customers reach you in more than one way. What they order and book ends up in the same panel, whichever way they came.</p></div>
<div class="chans">
<article class="chan" data-r><h3>Your website</h3><dl><div><dt>Today</dt><dd>Customers browse, order, book and pay, by card or cash on delivery.</dd></div></dl></article>
<article class="chan" data-r><h3>Customers' AI assistants</h3><dl><div><dt>Today</dt><dd>ChatGPT and Claude read your products, prices and free times, and send the customer to the page to book or pay.</dd></div><div class="next"><dt>Coming next</dt><dd>Booking and paying without leaving the chat.</dd></div></dl></article>
<article class="chan" data-r><h3>WhatsApp</h3><dl><div><dt>Today</dt><dd>Customers sign in with a code on WhatsApp, and you get a WhatsApp message for each order.</dd></div><div class="next"><dt>Coming next</dt><dd>Customers order and book by chatting with your own WhatsApp number.</dd></div></dl></article>
</div>
<div class="join" data-w aria-hidden="true"><i></i><i></i><i></i><b></b><u></u></div>
<div class="panel-head" data-r><h3 id="panel-h">Every order and booking in one panel</h3>
<p class="sub">Your site comes with an admin panel made from your own data. Change a price or a photo yourself, on your phone or computer, or ask your assistant.</p></div>
<figure class="panel-shot" data-r><img src="{{.Calendar.Src}}" width="{{.Calendar.W}}" height="{{.Calendar.H}}" alt="{{.Calendar.Alt}}" loading="lazy">
<figcaption>The bookings calendar of the salon demo, one lane per stylist. <a href="/templates/booking-uae/admin">Open the demo's admin panel</a>; it is read-only.</figcaption></figure>
<h3 class="after-h" data-r>After an order or booking comes in</h3>
<ol class="after">
<li data-r><h4>You are told</h4><p>A WhatsApp message and an email, with the customer's name and what they ordered.</p></li>
<li data-r><h4>It is in your panel</h4><p>In the orders list, or in the calendar lane of the stylist, crew or agent.</p></li>
<li data-r><h4>Your customer is told</h4><p>A confirmation by email, and for bookings a reminder the day before.</p></li>
<li data-r><h4>You can change it</h4><p>Move, cancel or refund from the panel. The customer gets an email, and a card refund goes back at once.</p></li>
</ol>
</div></section>

<section class="block band" aria-labelledby="trade-h"><div class="wrap" id="trades">
<div data-r><h2 id="trade-h">Made for your kind of business</h2>
<p class="sub">Each template is a complete, working site. You change the names, prices and photos; the booking rules and payments are already there.</p></div>
<div class="tabs" role="tablist" aria-label="Kinds of business">{{range .Trades}}<button type="button" role="tab" id="tab-{{.ID}}" aria-controls="trade-{{.ID}}" aria-selected="false">{{.Tab}}</button>{{end}}</div>
{{range .Trades}}<div class="trade" role="tabpanel" id="trade-{{.ID}}" aria-labelledby="tab-{{.ID}}">
<img class="shot" src="{{.Screen.Src}}" width="{{.Screen.W}}" height="{{.Screen.H}}" alt="{{.Screen.Alt}}" loading="lazy">
<div><h3>{{.Tab}}</h3><div class="cols">
<div><h4>Your customers</h4><ul>{{range .Customers}}<li>{{.}}</li>{{end}}</ul></div>
<div><h4>You</h4><ul>{{range .Owner}}<li>{{.}}</li>{{end}}</ul></div></div>
<p class="go"><a href="{{.DemoURL}}">Try the demo as a customer</a><a href="{{.AdminURL}}">See the owner's panel</a></p></div>
</div>{{end}}
</div></section>

<section class="block" aria-labelledby="has-h"><div class="wrap">
<div data-r><h2 id="has-h">What every site has, on every plan</h2></div>
<div class="groups">
<section class="group" data-r><h3>Take the money</h3>
<div class="spec" aria-hidden="true"><div class="row"><span>Deposit, haircut</span><span class="num">AED 50.00</span><span class="tag">Paid</span></div><div class="row"><span class="dim">Cancelled in time</span><span class="num dim">− AED 50.00</span><span class="tag">Refunded</span></div></div>
<dl><div><dt>Card payments</dt><dd>Customers pay on Stripe's page and the money goes to your own Stripe account.</dd></div>
<div><dt>Cash on delivery</dt><dd>Or pay at the venue. Works without Stripe.</dd></div>
<div><dt>Deposits and refunds</dt><dd>Take a deposit when someone books, and refund from your panel in one step.</dd></div></dl></section>
<section class="group" data-r><h3>Run the day</h3>
<div class="spec" aria-hidden="true"><div class="lanes"><div><b>Layla</b><span>10:00 Colour</span><span>13:30 Haircut</span></div><div><b>Maryam</b><span class="gap">.</span><span>12:00 Manicure</span></div><div><b>Priya</b><span>11:00 Facial</span><span>15:00 Haircut</span></div></div></div>
<dl><div><dt>Admin panel</dt><dd>Products, photos, orders and customers, on any phone.</dd></div>
<div><dt>Calendar</dt><dd>Day and week views, one lane per stylist, crew or item.</dd></div>
<div><dt>Staff access</dt><dd>Helpers manage orders without seeing your payments or documents.</dd></div>
<div><dt>New order alerts</dt><dd>On WhatsApp and by email, with a daily summary.</dd></div></dl></section>
<section class="group" data-r><h3>Made for the UAE</h3>
<div class="spec" aria-hidden="true"><div class="bi"><span>AED 250.00</span> <span lang="ar" dir="rtl">250.00 د.إ</span></div><div class="row dim"><span>Includes 5% VAT</span><span lang="ar" dir="rtl">شامل ضريبة القيمة المضافة 5%</span></div></div>
<dl><div><dt>AED with 5% VAT</dt><dd>Tax invoices once you add your TRN.</dd></div>
<div><dt>English and Arabic</dt><dd>Right-to-left layout in Arabic, switched with one tap.</dd></div>
<div><dt>Customer sign-in</dt><dd>By email or a code sent on WhatsApp. Customers see their orders and bookings.</dd></div></dl></section>
<section class="group" data-r><h3>You stay in control</h3>
<div class="spec" aria-hidden="true"><div class="flow"><span class="tag">Preview</span><i></i><span class="tag">You press Publish</span><i></i><span class="tag">Version 12 live</span></div><div class="row dim"><span>Backup taken before publishing</span><span>Roll back to version 11</span></div></div>
<dl><div><dt>Preview first</dt><dd>Nothing goes live until you press Publish, and you can roll back to an earlier version.</dd></div>
<div><dt>Backups</dt><dd>Your live data is backed up before every publish.</dd></div>
<div><dt>Your data</dt><dd>Download everything your site holds at any time, or delete it.</dd></div>
<div><dt>Your own domain</dt><dd>www.yourshop.ae on Pro and Business.</dd></div></dl></section>
</div></div></section>

<section class="block band" id="pricing" aria-labelledby="pricing-h"><div class="wrap">
<div data-r><h2 id="pricing-h">Pricing</h2><p class="sub">Start free. Upgrade when you need more sites, your own domain or no fee on card payments.</p></div>
<div class="prices">
<div class="plan" data-r><h3>Free</h3><p class="for">To try it with real customers</p><p class="amount">AED 0</p><ul><li>1 site</li><li>Renoxium takes {{.Fee}}% of card payments</li><li>50 WhatsApp sign-in codes a month</li><li>200 MB of images</li><li>Small "Made with Renoxium" badge on your site</li></ul><a class="btn line" href="/signup">Create your account</a></div>
<div class="plan pick" data-r><h3>Pro</h3><p class="for">For one growing business</p><p class="amount">AED 99 <span>a month</span></p><ul><li>3 sites</li><li>No Renoxium fee on card payments</li><li>500 WhatsApp sign-in codes a month</li><li>2 GB of images</li><li>Your own domain, 2 staff per site</li><li>No badge</li></ul><a class="btn" href="/signup">Choose Pro</a></div>
<div class="plan" data-r><h3>Business</h3><p class="for">For several branches or brands</p><p class="amount">AED 299 <span>a month</span></p><ul><li>10 sites</li><li>No Renoxium fee on card payments</li><li>2,000 WhatsApp sign-in codes a month</li><li>10 GB of images</li><li>Your own domain, 10 staff per site</li><li>No badge</li></ul><a class="btn line" href="/signup">Choose Business</a></div>
</div>
<p class="sum" data-r><b>When does Pro pay for itself?</b> On Free, AED 10,000 of card sales in a month costs AED {{.FeeOn10k}} in Renoxium fees. Pro is AED 99, so it is cheaper once your card sales pass AED {{.ProFrom}} a month. Cash on delivery never has a Renoxium fee.</p>
<p class="note">Stripe charges its own card fees to your Stripe account on every plan. Building and changing your site through your assistant is unlimited on every plan; you need your own ChatGPT or Claude account.</p></div></section>

<section class="block" aria-labelledby="faq-h"><div class="wrap"><h2 id="faq-h" data-r>Questions owners ask</h2><div class="faq" data-r>
<details><summary>Do I need to know anything technical?</summary><p>No. You describe your business in your own words, in English or Arabic. The assistant does the building and testing, and you check the result in a preview before it goes live.</p></details>
<details><summary>Which AI assistant do I need?</summary><p>ChatGPT (a paid plan with developer mode) or Claude, with your own account. You add Renoxium to it once; the <a href="/connect">connect page</a> shows the steps.</p></details>
<details><summary>I already talk to customers on WhatsApp and Instagram. Where does the site fit?</summary><p>Keep chatting with them there. Share your site's link in your bio and your replies: the site and its AI-assistant tools take the order, the payment and the booking and put them in your panel and schedule, so nothing gets lost in a chat.</p></details>
<details><summary>Can my customers use ChatGPT or Claude to order?</summary><p>They can add your business to ChatGPT or Claude and ask about your products, prices and free times. To book or pay, the assistant sends them to the right page of your site.</p></details>
<details><summary>What if the assistant makes a mistake?</summary><p>Changes always go to a private preview first, and only you can publish. If something slips through, roll back to an earlier version from your panel; live data is backed up before every publish.</p></details>
<details><summary>Do I need Stripe?</summary><p>Only for card payments. Cash on delivery and pay at the venue work without it. When you connect Stripe, payments go straight to your own Stripe account.</p></details>
<details><summary>Can I use my own domain?</summary><p>Yes, on Pro and Business, for example www.yourshop.ae. On Free your site lives at yourname.renoxium.app.</p></details>
<details><summary>Who owns the customers and orders?</summary><p>You do. Download everything your site holds at any time, or delete your account and all of its data.</p></details>
</div></div></section>

<section class="block end" aria-labelledby="end-h"><div class="wrap" data-r>
<h2 id="end-h">Try a demo before you sign up</h2><p class="sub">Book a haircut, order a perfume or ask for a quote on a demo, then open the owner's admin panel to see what you would get.</p>
<div class="cta"><a class="btn" href="/templates">Try a demo</a><a class="btn line" href="/signup">Create your account</a></div></div></section>
</main>
{{template "footer" .Legal}}
{{template "landing-js"}}
</body></html>{{end}}

{{define "landing-css"}}<style>
[hidden]{display:none!important}
html,body{overflow-x:clip}
@media(min-width:760px){header.wrap{position:sticky;top:0;z-index:10}
header.wrap::before{content:"";position:absolute;inset:0 calc(50% - 50vw);z-index:-1;background:var(--paper);border-bottom:1px solid var(--line);opacity:0;transition:opacity .25s}
.scrolled header.wrap::before{opacity:1}}
.hero{position:relative;isolation:isolate;overflow-x:clip;padding:clamp(32px,6vw,80px) 0 0}
.hero::before,.end::before{content:"";position:absolute;z-index:-1;pointer-events:none;
background:url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='72' height='72'%3E%3Cpath d='M18 18H54V54H18ZM36 10.5L61.5 36L36 61.5L10.5 36ZM36 0V10.5M36 61.5V72M0 36H10.5M61.5 36H72' fill='none' stroke='%23b8892e' stroke-width='1'/%3E%3C/svg%3E")}
.hero::before{inset:-96px 0 auto 0;height:760px;opacity:.34;-webkit-mask-image:radial-gradient(ellipse 62% 78% at 92% 4%,#000 0,transparent 72%);mask-image:radial-gradient(ellipse 62% 78% at 92% 4%,#000 0,transparent 72%)}
.hero::after{content:"";position:absolute;z-index:-1;pointer-events:none;top:-260px;right:-8%;width:min(720px,90vw);aspect-ratio:1;border-radius:50%;
background:radial-gradient(closest-side,color-mix(in srgb,var(--gold) 15%,transparent),transparent);animation:drift 64s ease-in-out infinite alternate}
@keyframes drift{from{transform:translate(0,0)}to{transform:translate(-14%,9%)}}
.sign{display:grid;grid-template-columns:1fr 1px 1fr;column-gap:clamp(32px,4vw,64px);align-items:start;font-size:clamp(30px,3.3vw,48px)}
.sign>h1,.sign>p{margin:0;font-size:inherit;font-weight:600;text-wrap:balance}
.sign .en{line-height:1.08;letter-spacing:-.03em}
.sign .ar{font-size:.9em;line-height:1.4;letter-spacing:0;text-align:right;font-weight:600}
.sign .rule{align-self:stretch;background:var(--gold)}
@media(max-width:760px){.sign{grid-template-columns:1fr;row-gap:20px}.sign .rule{height:2px;width:56px;justify-self:end}}
.how-it{font-size:19px;line-height:1.55;color:var(--slate);max-width:40em;margin:clamp(28px,4vw,44px) 0 28px}
.ways{display:grid;gap:64px;margin-top:clamp(64px,8vw,104px)}
@media(min-width:900px){.ways{grid-template-columns:repeat(3,1fr);gap:48px}}
.way .shot{width:100%;max-width:300px;aspect-ratio:600/1040;object-fit:cover;object-position:top;transition:transform .35s cubic-bezier(.2,.7,.2,1),box-shadow .35s}
@media(hover:hover){.way:hover .shot{transform:translateY(-6px)}}
.way h2{font-size:26px;margin:32px 0 8px}
.way p{margin:0;color:var(--slate);max-width:28em}
.way ul{list-style:none;padding:0;margin:14px 0 0;display:grid;gap:6px}
.also{margin:64px 0 0;color:var(--slate);border-top:1px solid var(--line);padding-top:24px}
.split{display:grid;gap:48px;align-items:start}@media(min-width:960px){.split{grid-template-columns:1fr 1fr;gap:80px}}
.steps{list-style:none;padding:0;margin:40px 0 0;display:grid;gap:32px;counter-reset:s;position:relative}
.steps::before{content:"";position:absolute;left:17px;top:18px;bottom:18px;width:1px;background:var(--line);transform-origin:top}
.steps li{counter-increment:s;display:grid;grid-template-columns:36px 1fr;gap:18px;position:relative}
.steps li::before{content:counter(s);width:36px;height:36px;border-radius:50%;border:1px solid var(--line);background:var(--paper);display:grid;place-items:center;font-size:16px;font-weight:600;line-height:1;color:var(--ink)}
.steps h3{font-size:20px;margin:4px 0 4px}.steps p{margin:0;color:var(--slate)}
.chat{margin:0;background:var(--paper);border:1px solid var(--line);border-radius:12px;padding:24px;display:flex;flex-direction:column;gap:14px;font-size:16px;max-width:540px}
.chat .me{align-self:flex-end;max-width:82%;background:var(--mist);border-radius:18px;padding:10px 16px;margin:0}
.band .chat .me{background:color-mix(in srgb,var(--mist) 60%,var(--line))}
.chat .me[lang=ar]{direction:rtl}
.chat .used{font-size:14px;color:var(--slate);margin:6px 0 -8px}
.chat .reply{margin:0;max-width:92%}
.chat .reply u{text-decoration-color:var(--gold);text-decoration-thickness:2px;text-underline-offset:4px}
.chat figcaption{font-size:14px;color:var(--slate);border-top:1px solid var(--line);padding-top:12px;margin-top:4px}
.relay{display:grid;gap:40px;margin-top:40px;align-items:start}
@media(min-width:960px){.relay{grid-template-columns:minmax(0,540px) 1fr;gap:80px}}
.lands{display:grid;gap:16px;max-width:460px}
.lands .lab{margin:0;font-weight:500}
.card{background:var(--paper);border:1px solid var(--line);border-radius:12px;padding:18px 20px;font-size:16px}
.card .from{display:block;font-size:14px;color:var(--slate);margin-bottom:4px}
.card p{margin:0}
.slot{display:grid;grid-template-columns:auto 1fr auto;gap:4px 16px;align-items:start;border-left:3px solid var(--gold);border-radius:4px 12px 12px 4px}
.slot .t{font-weight:600;font-variant-numeric:tabular-nums}.slot .who{color:var(--slate);font-size:15px;grid-column:2}
.tag{font-size:13px;line-height:1;padding:5px 9px;border-radius:99px;border:1px solid var(--line);color:var(--slate);white-space:nowrap}
.lands .fine{margin:4px 0 0;color:var(--slate);font-size:15px}
.chans{display:grid;gap:16px;margin-top:40px}@media(min-width:900px){.chans{grid-template-columns:repeat(3,1fr);gap:24px}}
.chan{border:1px solid var(--line);border-radius:12px;padding:24px;background:var(--paper)}
.chan h3{font-size:20px}
.chan dl{margin:14px 0 0;display:grid;gap:14px}
.chan dt{font-size:14px;font-weight:500;color:var(--slate)}.chan dd{margin:2px 0 0}
.chan .next{border-top:1px dashed var(--line);padding-top:14px}.chan .next dd{color:var(--slate)}
.join{display:none}
@media(min-width:900px){.join{display:grid;grid-template-columns:repeat(3,1fr);column-gap:24px;height:72px;position:relative}
.join i{justify-self:center;width:1px;height:36px;background:var(--line);transform-origin:top}
.join b{position:absolute;top:36px;left:calc((100% - 48px)/6);right:calc((100% - 48px)/6);height:1px;background:var(--line);transform-origin:center}
.join u{position:absolute;top:36px;left:50%;width:1px;height:36px;background:var(--line);transform-origin:top}
.join u::after{content:"";position:absolute;bottom:-4px;left:-4px;width:9px;height:9px;border-radius:50%;background:var(--gold)}}
.panel-head{margin-top:56px}@media(min-width:900px){.panel-head{margin-top:20px;text-align:center}.panel-head .sub{margin-inline:auto}}
.panel-head h3{font-size:clamp(24px,2.4vw,30px);letter-spacing:-.02em}
.panel-shot{margin:40px 0 0}.panel-shot img{border-radius:12px;border:1px solid var(--line);width:100%;box-shadow:var(--shot-shadow)}
.panel-shot figcaption{font-size:15px;color:var(--slate);margin-top:12px;max-width:48em}
.after{list-style:none;padding:0;margin:56px 0 0;display:grid;gap:24px;counter-reset:a}
@media(min-width:700px){.after{grid-template-columns:1fr 1fr}}@media(min-width:1000px){.after{grid-template-columns:repeat(4,1fr);gap:32px}}
.after li{counter-increment:a;border-top:1px solid var(--ink);padding-top:14px}
.after li::before{content:counter(a);display:block;font-size:14px;font-weight:600;color:var(--slate);margin-bottom:6px}
.after h4{margin:0 0 4px;font-size:18px;font-weight:600;line-height:1.3}.after p{margin:0;color:var(--slate);font-size:16px}
.after-h{margin:64px 0 0;font-size:22px}
.tabs{display:none;gap:8px;margin-top:32px;overflow-x:auto;padding:4px 20px 6px;margin-inline:-20px;scrollbar-width:none}.tabs::-webkit-scrollbar{display:none}
.tabbed .tabs{display:flex}
.tabs button{flex:none;font:inherit;font-size:16px;font-weight:500;min-height:44px;padding:8px 16px;border-radius:99px;border:1px solid var(--line);background:transparent;color:var(--slate);cursor:pointer;transition:background .2s,color .2s,border-color .2s}
.tabs button:hover{color:var(--ink);border-color:var(--slate)}
.tabs button[aria-selected=true]{background:var(--ink);color:var(--on-ink);border-color:var(--ink)}
.trade{display:grid;gap:32px;margin-top:40px;align-items:start}
.trade+.trade{border-top:1px solid var(--line);padding-top:40px}.tabbed .trade+.trade{border-top:0;padding-top:0}
@media(min-width:900px){.trade{grid-template-columns:240px 1fr;gap:72px}}
.trade .shot{width:100%;max-width:240px;aspect-ratio:600/900;object-fit:cover;object-position:top}
@media(max-width:899px){.trade .shot{max-width:200px}}
.trade h3{font-size:26px}.tabbed .trade h3{position:absolute;left:-9999px}
.trade .cols{display:grid;gap:32px}@media(min-width:640px){.trade .cols{grid-template-columns:1fr 1fr;gap:48px}}
.trade h4{margin:0;font-size:15px;font-weight:500;color:var(--slate)}
.trade ul{list-style:none;padding:0;margin:10px 0 0}
.trade li{padding:13px 0;border-top:1px solid var(--line)}
.trade .go{margin:28px 0 0;display:flex;gap:10px 24px;flex-wrap:wrap}
.tabbed .trade:not([hidden]){animation:tabin .35s cubic-bezier(.2,.7,.2,1)}
@keyframes tabin{from{opacity:0;transform:translateY(8px)}}
.groups{display:grid;gap:16px;margin-top:40px}@media(min-width:900px){.groups{grid-template-columns:1fr 1fr;gap:24px}}
.group{border:1px solid var(--line);border-radius:12px;padding:28px;display:flex;flex-direction:column}
.group h3{font-size:22px}
.group dl{margin:20px 0 0}
.group dl div{display:grid;grid-template-columns:minmax(120px,34%) 1fr;gap:4px 20px;padding:13px 0;border-top:1px solid var(--line)}
@media(max-width:520px){.group dl div{grid-template-columns:1fr}}
.group dt{font-weight:500}.group dd{margin:0;color:var(--slate)}
.spec{margin:20px 0 4px;background:var(--mist);border-radius:8px;padding:16px 18px;font-size:15px;display:grid;gap:10px}
.spec .row{display:flex;justify-content:space-between;align-items:center;gap:12px}
.spec .num{font-variant-numeric:tabular-nums;font-weight:500;margin-inline-start:auto;white-space:nowrap}
.spec .tag{background:var(--paper)}
.spec .lanes{display:grid;grid-template-columns:repeat(3,1fr);gap:8px}
.spec .lanes b{font-weight:500;font-size:13px;color:var(--slate)}
.spec .lanes span{display:block;background:var(--paper);border-left:3px solid var(--gold);border-radius:3px 6px 6px 3px;padding:6px 8px;font-size:13px;line-height:1.3;margin-top:6px}
.spec .lanes span.gap{visibility:hidden}
.spec .bi{display:flex;justify-content:space-between;gap:12px;font-size:20px;font-weight:600;letter-spacing:-.01em}
.spec .flow{display:flex;align-items:center;gap:8px;flex-wrap:wrap}
.spec .flow i{flex:1;min-width:12px;height:1px;background:var(--slate);opacity:.5}
.spec .dim{color:var(--slate)}
.prices{display:grid;gap:16px;margin-top:40px}@media(min-width:900px){.prices{grid-template-columns:repeat(3,1fr);gap:24px}}
.plan{padding:28px;border:2px solid transparent;border-radius:12px;display:flex;flex-direction:column}
.plan.pick{border-color:var(--gold);background:var(--paper)}
@media(max-width:899px){.plan{border-color:var(--line)}.plan.pick{border-color:var(--gold)}}
.plan h3{font-size:22px}.plan .for{color:var(--slate);font-size:16px;margin:4px 0 0}
.amount{font-size:40px;font-weight:600;letter-spacing:-.02em;margin:20px 0 0;line-height:1.1}.amount span{font-size:16px;font-weight:400;color:var(--slate);letter-spacing:0}
.plan ul{padding:0;margin:20px 0 28px;list-style:none;display:grid;gap:8px;font-size:16px}
.plan .btn{margin-top:auto}.note{color:var(--slate);font-size:16px;margin:32px 0 0;max-width:48em}
.sum{margin:32px 0 0;padding:20px 24px;border-left:3px solid var(--gold);background:var(--paper);border-radius:4px 12px 12px 4px;max-width:48em}
.sum b{font-weight:600}
.note+.note,.sum+.note{margin-top:16px}
.faq{max-width:820px;margin-top:32px;border-bottom:1px solid var(--line)}
.faq details{border-top:1px solid var(--line)}
.faq summary{cursor:pointer;font-weight:500;font-size:19px;padding:18px 0;list-style:none;display:flex;justify-content:space-between;gap:16px}
.faq summary::-webkit-details-marker{display:none}
.faq summary::after{content:"+";color:var(--slate);font-weight:400}.faq details[open] summary::after{content:"\2212"}
.faq p{margin:0 0 20px;color:var(--slate);max-width:44em}
.end{position:relative;isolation:isolate}
.end::before{inset:0;opacity:.3;-webkit-mask-image:radial-gradient(ellipse 55% 110% at 100% 100%,#000 0,transparent 70%);mask-image:radial-gradient(ellipse 55% 110% at 100% 100%,#000 0,transparent 70%)}
.end .cta{margin-top:28px}
.js [data-r]{opacity:0;transform:translateY(12px)}
.js [data-r].in{opacity:1;transform:none;transition:opacity .55s cubic-bezier(.2,.7,.2,1),transform .55s cubic-bezier(.2,.7,.2,1);transition-delay:calc(var(--i,0)*70ms)}
.js [data-seq]>*{opacity:0;transform:translateY(8px)}
.js [data-seq]>.in{opacity:1;transform:none;transition:opacity .4s ease-out,transform .4s cubic-bezier(.2,.7,.2,1)}
.js .steps::before{transform:scaleY(0)}.js .steps.in::before{transform:none;transition:transform 1.1s cubic-bezier(.4,0,.2,1) .15s}
.js .join i,.js .join u{transform:scaleY(0)}.js .join b{transform:scaleX(0)}.js .join u::after{opacity:0}
.js .join.in i,.js .join.in b,.js .join.in u{transform:none;transition:transform .45s cubic-bezier(.4,0,.2,1)}
.js .join.in b{transition-delay:.4s}.js .join.in u{transition-delay:.8s}.js .join.in u::after{opacity:1;transition:opacity .3s 1.2s}
.js .now,.js .now *{transition:none!important}
@media(prefers-reduced-motion:reduce){.hero::after{animation:none}.way .shot,.tabs button,header.wrap::before{transition:none}.tabbed .trade:not([hidden]){animation:none}.way:hover .shot{transform:none}}
</style><script>(function(){var h=document.documentElement;if("IntersectionObserver" in window&&!matchMedia("(prefers-reduced-motion: reduce)").matches){h.classList.add("js");setTimeout(function(){if(!window.__rv)h.classList.remove("js")},3000)}})()</script>{{end}}

{{define "landing-js"}}<script>(function(){var d=document,h=d.documentElement;window.__rv=1;
var tabs=[].slice.call(d.querySelectorAll("[role=tab]"));
function pick(t,f){tabs.forEach(function(x){var s=x===t;x.setAttribute("aria-selected",s?"true":"false");x.tabIndex=s?0:-1;d.getElementById(x.getAttribute("aria-controls")).hidden=!s});if(f){t.focus();t.scrollIntoView({block:"nearest",inline:"center"})}}
tabs.forEach(function(t,i){t.addEventListener("click",function(){pick(t)});t.addEventListener("keydown",function(e){var n=tabs.length,r=h.dir==="rtl",j=e.key===(r?"ArrowLeft":"ArrowRight")?(i+1)%n:e.key===(r?"ArrowRight":"ArrowLeft")?(i+n-1)%n:e.key==="Home"?0:e.key==="End"?n-1:-1;if(j>=0){e.preventDefault();pick(tabs[j],true)}})});
if(tabs.length){d.getElementById("trades").classList.add("tabbed");pick(tabs[0])}
function sc(){h.classList.toggle("scrolled",window.scrollY>8)}sc();addEventListener("scroll",sc,{passive:true});
if(!h.classList.contains("js"))return;
var first=true,io=new IntersectionObserver(function(es){var n=0;es.forEach(function(e){if(!e.isIntersecting)return;var t=e.target;io.unobserve(t);
if(t.hasAttribute("data-seq")){[].forEach.call(t.children,function(c,i){setTimeout(function(){c.classList.add("in")},first?0:200+i*520)});if(first)t.classList.add("now");return}
if(first)t.classList.add("now");else t.style.setProperty("--i",Math.min(n++,5));t.classList.add("in")});first=false},{rootMargin:"0px 0px -8% 0px",threshold:.12});
[].forEach.call(d.querySelectorAll("[data-r],[data-w],[data-seq]"),function(x){io.observe(x)})})()</script>{{end}}

{{define "landing-ar"}}<!doctype html><html lang="ar" dir="rtl"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>Renoxium: الطلبات والحجوزات والمدفوعات على موقعك</title><meta name="description" content="متجر إلكتروني وحجوزات وطلبات واتساب في الإمارات على موقعك الخاص، مع الدفع بالبطاقة. يُبنى بوصف نشاطك لـ ChatGPT أو Claude.">
{{template "lcommon"}}{{template "landing-css"}}<style>
h1,h2,h3,h4,.amount{letter-spacing:0}h2{line-height:1.4}h3{line-height:1.5}
.sign .en{font-size:.82em;line-height:1.15;text-align:left;font-weight:600}
.sign .ar{font-size:1em;line-height:1.45}
.hero::before{-webkit-mask-image:radial-gradient(ellipse 62% 78% at 8% 4%,#000 0,transparent 72%);mask-image:radial-gradient(ellipse 62% 78% at 8% 4%,#000 0,transparent 72%)}
.hero::after{right:auto;left:-8%;animation-name:drift-r}
@keyframes drift-r{from{transform:translate(0,0)}to{transform:translate(14%,9%)}}
.end::before{-webkit-mask-image:radial-gradient(ellipse 55% 110% at 0 100%,#000 0,transparent 70%);mask-image:radial-gradient(ellipse 55% 110% at 0 100%,#000 0,transparent 70%)}
.steps::before{left:auto;right:17px}
.slot,.sum{border-left:0;border-right:3px solid var(--gold);border-radius:12px 4px 4px 12px}
.spec .lanes span{border-left:0;border-right:3px solid var(--gold);border-radius:6px 3px 3px 6px}
.skip{left:auto;right:-9999px}.skip:focus{right:16px}
.tabbed .trade h3{left:auto;right:-9999px}
.amount{font-size:36px}
</style>
<link rel="alternate" hreflang="en" href="{{.Base}}/"><link rel="alternate" hreflang="ar" href="{{.Base}}/ar"><link rel="alternate" hreflang="x-default" href="{{.Base}}/">
<meta property="og:type" content="website"><meta property="og:site_name" content="Renoxium"><meta property="og:locale" content="ar_AE"><meta property="og:locale:alternate" content="en_AE"><meta property="og:title" content="Renoxium: الطلبات والحجوزات والمدفوعات على موقعك">
<meta property="og:description" content="متجر إلكتروني وحجوزات وطلبات واتساب في الإمارات على موقعك الخاص، مع الدفع بالبطاقة. يُبنى بوصف نشاطك لـ ChatGPT أو Claude.">
<meta property="og:image" content="{{.Base}}/og-image.png"><meta property="og:image:width" content="1200"><meta property="og:image:height" content="630"><meta name="twitter:card" content="summary_large_image">
<meta name="twitter:title" content="Renoxium: الطلبات والحجوزات والمدفوعات على موقعك"><meta name="twitter:description" content="متجر إلكتروني وحجوزات وطلبات واتساب في الإمارات على موقعك الخاص، مع الدفع بالبطاقة. يُبنى بوصف نشاطك لـ ChatGPT أو Claude."><meta name="twitter:image" content="{{.Base}}/og-image.png"></head><body>
<a class="skip" href="#main">انتقل إلى المحتوى</a><header class="wrap"><nav class="top" aria-label="القائمة الرئيسية"><a class="logo" href="/ar" aria-label="الصفحة الرئيسية لـ Renoxium" dir="ltr">{{template "brandlogo"}}</a><div class="links">
<a class="wide" href="/templates">القوالب</a><a class="wide" href="#pricing">الأسعار</a><a class="wide" href="/connect">الربط</a><a href="/" lang="en" dir="ltr">English</a><a href="/login">تسجيل الدخول</a><a class="btn sm" href="/signup">إنشاء حساب</a></div></nav></header>
<main id="main">
<section class="hero" aria-labelledby="hero-h"><div class="wrap">
<div class="sign"><h1 id="hero-h" class="ar">الطلبات والحجوزات والمدفوعات في مكان واحد، من موقعك إلى مساعدي الذكاء الاصطناعي لدى عملائك.</h1><span class="rule" aria-hidden="true"></span><p class="en" lang="en" dir="ltr">Orders, bookings and payments in one place, from your website to your customers' AI assistants.</p></div>
<p class="how-it">صِف نشاطك التجاري لـ ChatGPT أو Claude. يبني Renoxium موقعك ويربطه بمساعدي الذكاء الاصطناعي، ليجدك العملاء ويحجزوا ويدفعوا. كل طلب يصل إلى لوحة واحدة، وتصلك رسالة واتساب عن كل طلب.</p>
<div class="cta"><a class="btn" href="/signup">أنشئ حسابك</a><a class="btn line" href="/templates">جرّب نسخة تجريبية</a></div>
<div class="ways">{{range .Paths}}<section class="way" id="{{.ID}}" aria-labelledby="{{.ID}}-h" data-r>
<img class="shot" src="{{.Screen.Src}}" width="{{.Screen.W}}" height="{{.Screen.H}}" alt="{{.Screen.Alt}}">
<h2 id="{{.ID}}-h">{{.Title}}</h2><p>{{.Text}}</p>
<ul>{{range .Demos}}<li><a href="{{.URL}}">{{.Label}}</a></li>{{end}}</ul></section>{{end}}</div>
<p class="also">هل لديك توصيل أيضاً؟ <a href="/templates#delivery-tracking">التتبع المباشر للتوصيل</a> يتيح للعملاء متابعة المندوب على الخريطة. أو <a href="/templates">شاهد كل القوالب</a> مع لوحة تحكم صاحب النشاط.</p>
</div></section>

<section class="block" aria-labelledby="how-h"><div class="wrap split">
<div data-r><h2 id="how-h">كيف يعمل</h2><p class="sub">أنت تصف، والمساعد يبني، وأنت تقرر ما يُنشر.</p>
<ol class="steps" data-w>
<li><div><h3>اربط Renoxium</h3><p>أضف Renoxium إلى ChatGPT أو Claude مرة واحدة وسجّل الدخول. <a href="/connect">صفحة الربط</a> تشرح الخطوات.</p></div></li>
<li><div><h3>صِف نشاطك</h3><p>اسمك، وما تبيعه أو تحجزه، والأسعار، ومناطق التوصيل أو ساعات العمل، بالعربية أو الإنجليزية. يبني المساعد الموقع ويختبر كل صفحة.</p></div></li>
<li><div><h3>راجع المعاينة وانشر</h3><p>افتح المعاينة الخاصة على هاتفك. عندما يعجبك الموقع اضغط «نشر». اربط Stripe لقبول البطاقات.</p></div></li>
</ol></div>
<figure class="chat" data-seq>
<p class="me">أنشئ موقع حجوزات لـ«لمسة»، صالوني في جميرا. قص شعر وصبغة وأظافر، وثلاث مصففات.</p>
<p class="me">وأريد عربوناً بقيمة ٥٠ درهماً عند الحجز</p>
<p class="used">تم استخدام Renoxium</p>
<p class="reply">تم. يختار العملاء الخدمة والمصففة والوقت المتاح، ويدفعون عربوناً قدره ٥٠ درهماً بالبطاقة. اختبرتُ كل الصفحات. معاينتك الخاصة على <span dir="ltr">lamsa.renoxium.app</span>؛ راجعها على هاتفك ثم اطلب مني النشر.</p>
<p class="me">أغلق يوم الجمعة حتى الساعة ٢ ظهراً.</p>
<figcaption>مثال لمحادثة في ChatGPT بعد ربط Renoxium.</figcaption></figure>
</div></section>

<section class="block band" aria-labelledby="ai-h"><div class="wrap">
<div data-r><h2 id="ai-h">عملاؤك يستطيعون سؤال مساعدهم الذكي</h2>
<p class="sub">كل موقع على Renoxium يجيب ChatGPT و Claude بأسعارك ومخزونك وأوقاتك المتاحة الفعلية. يوجّه المساعد العميل إلى الصفحة المناسبة للحجز أو الدفع، ويصلك الحجز كأي حجز آخر.</p></div>
<div class="relay">
<figure class="chat" data-seq>
<p class="me">هل أستطيع حجز قص شعر في لمسة بيوتي لاونج غداً بعد الخامسة؟</p>
<p class="used">تم استخدام لمسة بيوتي لاونج</p>
<p class="reply">نعم. ليلى متاحة غداً الساعة 17:30 و18:15، وبريا الساعة 19:00. قص الشعر مع السشوار يستغرق ساعة وسعره 150 درهماً، وعربون 50 درهماً يحجز موعدك. <u>احجزي مع ليلى في لمسة</u></p>
<figcaption>مثال: عميلة في ChatGPT أضافت الصالون. تفتح الرابط وتختار 18:15 وتدفع العربون على موقع الصالون.</figcaption></figure>
<div class="lands" data-seq>
<p class="lab">ما يصلك بعد دقيقة</p>
<div class="card"><span class="from">رسالة واتساب إلى صاحب النشاط</span><p>حجز جديد: قص شعر مع سشوار، غداً 18:15 مع ليلى. سارة أ.، دُفع عربون 50 درهماً بالبطاقة.</p></div>
<div class="card slot"><span class="t">18:15</span><span>قص شعر مع سشوار</span><span class="tag">مؤكد</span><span class="who">سارة أ. · عمود ليلى في تقويمك</span></div>
<p class="fine">يحدث الشيء نفسه عندما تحجز من الصفحة الرئيسية لموقعك أو من رابط في حسابك على إنستغرام.</p>
</div></div>
</div></section>

<section class="block" aria-labelledby="chan-h"><div class="wrap">
<div data-r><h2 id="chan-h">مكان واحد خلف كل قناة</h2>
<p class="sub">يصل إليك العملاء بأكثر من طريقة. ما يطلبونه ويحجزونه ينتهي في اللوحة نفسها، أياً كانت الطريقة.</p></div>
<div class="chans">
<article class="chan" data-r><h3>موقعك</h3><dl><div><dt>اليوم</dt><dd>يتصفح العملاء ويطلبون ويحجزون ويدفعون، بالبطاقة أو نقداً عند الاستلام.</dd></div></dl></article>
<article class="chan" data-r><h3>مساعدو الذكاء الاصطناعي لدى العملاء</h3><dl><div><dt>اليوم</dt><dd>يقرأ ChatGPT و Claude منتجاتك وأسعارك وأوقاتك المتاحة، ويوجّهان العميل إلى صفحة الحجز أو الدفع.</dd></div><div class="next"><dt>قريباً</dt><dd>الحجز والدفع دون مغادرة المحادثة.</dd></div></dl></article>
<article class="chan" data-r><h3>واتساب</h3><dl><div><dt>اليوم</dt><dd>يسجّل العملاء الدخول برمز يصلهم على واتساب، وتصلك رسالة واتساب عن كل طلب.</dd></div><div class="next"><dt>قريباً</dt><dd>يطلب العملاء ويحجزون بمحادثة رقم واتساب الخاص بنشاطك.</dd></div></dl></article>
</div>
<div class="join" data-w aria-hidden="true"><i></i><i></i><i></i><b></b><u></u></div>
<div class="panel-head" data-r><h3 id="panel-h">كل طلب وحجز في لوحة واحدة</h3>
<p class="sub">يأتي موقعك مع لوحة تحكم مبنية من بياناتك. غيّر سعراً أو صورة بنفسك، من هاتفك أو حاسوبك، أو اطلب ذلك من مساعدك.</p></div>
<figure class="panel-shot" data-r><img src="{{.Calendar.Src}}" width="{{.Calendar.W}}" height="{{.Calendar.H}}" alt="{{.Calendar.Alt}}" loading="lazy">
<figcaption>تقويم الحجوزات في تجربة الصالون، عمود لكل مصففة. <a href="/templates/booking-uae/admin">افتح لوحة التحكم التجريبية</a>؛ وهي للقراءة فقط وبالإنجليزية.</figcaption></figure>
<h3 class="after-h" data-r>بعد وصول طلب أو حجز</h3>
<ol class="after">
<li data-r><h4>يصلك إشعار</h4><p>رسالة واتساب وبريد إلكتروني، فيهما اسم العميل وما طلبه.</p></li>
<li data-r><h4>يظهر في لوحتك</h4><p>في قائمة الطلبات، أو في عمود المصففة أو الفريق أو الوكيل في التقويم.</p></li>
<li data-r><h4>يصل العميلَ تأكيد</h4><p>تأكيد بالبريد الإلكتروني، وللحجوزات تذكير قبل الموعد بيوم.</p></li>
<li data-r><h4>يمكنك تعديله</h4><p>انقل الموعد أو ألغِه أو استردّ المبلغ من اللوحة. يصل العميلَ بريد إلكتروني، ويعود مبلغ البطاقة فوراً.</p></li>
</ol>
</div></section>

<section class="block band" aria-labelledby="trade-h"><div class="wrap" id="trades">
<div data-r><h2 id="trade-h">مصمَّم لنوع نشاطك</h2>
<p class="sub">كل قالب موقع كامل وجاهز للعمل. تغيّر الأسماء والأسعار والصور؛ أما قواعد الحجز والمدفوعات فجاهزة.</p></div>
<div class="tabs" role="tablist" aria-label="أنواع الأنشطة">{{range .Trades}}<button type="button" role="tab" id="tab-{{.ID}}" aria-controls="trade-{{.ID}}" aria-selected="false">{{.Tab}}</button>{{end}}</div>
{{range .Trades}}<div class="trade" role="tabpanel" id="trade-{{.ID}}" aria-labelledby="tab-{{.ID}}">
<img class="shot" src="{{.Screen.Src}}" width="{{.Screen.W}}" height="{{.Screen.H}}" alt="{{.Screen.Alt}}" loading="lazy">
<div><h3>{{.Tab}}</h3><div class="cols">
<div><h4>عملاؤك</h4><ul>{{range .Customers}}<li>{{.}}</li>{{end}}</ul></div>
<div><h4>أنت</h4><ul>{{range .Owner}}<li>{{.}}</li>{{end}}</ul></div></div>
<p class="go"><a href="{{.DemoURL}}">جرّب الموقع كعميل</a><a href="{{.AdminURL}}">شاهد لوحة صاحب النشاط</a></p></div>
</div>{{end}}
</div></section>

<section class="block" aria-labelledby="has-h"><div class="wrap">
<div data-r><h2 id="has-h">ما يتضمنه كل موقع، في كل الخطط</h2></div>
<div class="groups">
<section class="group" data-r><h3>استلام المال</h3>
<div class="spec" aria-hidden="true"><div class="row"><span>عربون، قص شعر</span><span class="num">50.00 د.إ</span><span class="tag">مدفوع</span></div><div class="row"><span class="dim">أُلغي في الوقت المحدد</span><span class="num dim">− 50.00 د.إ</span><span class="tag">مُستردّ</span></div></div>
<dl><div><dt>الدفع بالبطاقة</dt><dd>يدفع العملاء على صفحة Stripe ويذهب المال إلى حسابك أنت في Stripe.</dd></div>
<div><dt>الدفع عند الاستلام</dt><dd>أو الدفع في المكان. يعمل دون Stripe.</dd></div>
<div><dt>العربون والاسترداد</dt><dd>خذ عربوناً عند الحجز، واستردّه من لوحتك بخطوة واحدة.</dd></div></dl></section>
<section class="group" data-r><h3>إدارة اليوم</h3>
<div class="spec" aria-hidden="true"><div class="lanes"><div><b>ليلى</b><span>10:00 صبغة</span><span>13:30 قص شعر</span></div><div><b>مريم</b><span class="gap">.</span><span>12:00 مانيكير</span></div><div><b>بريا</b><span>11:00 عناية بالبشرة</span><span>15:00 قص شعر</span></div></div></div>
<dl><div><dt>لوحة التحكم</dt><dd>المنتجات والصور والطلبات والعملاء، من أي هاتف.</dd></div>
<div><dt>التقويم</dt><dd>عرض يومي وأسبوعي، بعمود لكل مصففة أو فريق أو عنصر.</dd></div>
<div><dt>صلاحيات الموظفين</dt><dd>يدير المساعدون الطلبات دون رؤية مدفوعاتك أو مستنداتك.</dd></div>
<div><dt>تنبيهات الطلبات</dt><dd>على واتساب وبالبريد الإلكتروني، مع ملخص يومي.</dd></div></dl></section>
<section class="group" data-r><h3>مصمَّم للإمارات</h3>
<div class="spec" aria-hidden="true"><div class="bi"><span>250.00 د.إ</span><span lang="en" dir="ltr">AED 250.00</span></div><div class="row dim"><span>شامل ضريبة القيمة المضافة 5%</span><span lang="en" dir="ltr">Includes 5% VAT</span></div></div>
<dl><div><dt>الدرهم مع ضريبة 5%</dt><dd>فواتير ضريبية بعد إضافة رقم التسجيل الضريبي.</dd></div>
<div><dt>العربية والإنجليزية</dt><dd>تخطيط من اليمين إلى اليسار بالعربية، والتبديل بنقرة واحدة.</dd></div>
<div><dt>تسجيل دخول العملاء</dt><dd>بالبريد الإلكتروني أو برمز يصل على واتساب. يرى العملاء طلباتهم وحجوزاتهم.</dd></div></dl></section>
<section class="group" data-r><h3>التحكم بيدك</h3>
<div class="spec" aria-hidden="true"><div class="flow"><span class="tag">معاينة</span><i></i><span class="tag">تضغط «نشر»</span><i></i><span class="tag">الإصدار 12 منشور</span></div><div class="row dim"><span>نسخة احتياطية قبل النشر</span><span>العودة إلى الإصدار 11</span></div></div>
<dl><div><dt>المعاينة أولاً</dt><dd>لا يُنشر شيء حتى تضغط «نشر»، ويمكنك العودة إلى إصدار سابق.</dd></div>
<div><dt>النسخ الاحتياطية</dt><dd>تُحفظ نسخة احتياطية من بياناتك قبل كل نشر.</dd></div>
<div><dt>بياناتك</dt><dd>نزّل كل ما يحتويه موقعك في أي وقت، أو احذفه.</dd></div>
<div><dt>نطاقك الخاص</dt><dd><span dir="ltr">www.yourshop.ae</span> في خطتي Pro و Business.</dd></div></dl></section>
</div></div></section>

<section class="block band" id="pricing" aria-labelledby="pricing-h"><div class="wrap">
<div data-r><h2 id="pricing-h">الأسعار</h2><p class="sub">ابدأ مجاناً. انتقل إلى خطة أعلى عندما تحتاج مواقع أكثر، أو نطاقك الخاص، أو إلغاء الرسوم على مدفوعات البطاقة.</p></div>
<div class="prices">
<div class="plan" data-r><h3>Free</h3><p class="for">لتجربته مع عملاء حقيقيين</p><p class="amount">0 درهم</p><ul><li>موقع واحد</li><li>يأخذ Renoxium {{.Fee}}% من مدفوعات البطاقة</li><li>50 رمز دخول عبر واتساب شهرياً</li><li>200 ميغابايت للصور</li><li>شارة صغيرة «صُنع بـ Renoxium» على موقعك</li></ul><a class="btn line" href="/signup">أنشئ حسابك</a></div>
<div class="plan pick" data-r><h3>Pro</h3><p class="for">لنشاط واحد ينمو</p><p class="amount">99 درهماً <span>شهرياً</span></p><ul><li>3 مواقع</li><li>لا رسوم من Renoxium على مدفوعات البطاقة</li><li>500 رمز دخول عبر واتساب شهرياً</li><li>2 غيغابايت للصور</li><li>نطاقك الخاص، وموظفان لكل موقع</li><li>بلا شارة</li></ul><a class="btn" href="/signup">اختر Pro</a></div>
<div class="plan" data-r><h3>Business</h3><p class="for">لعدة فروع أو علامات تجارية</p><p class="amount">299 درهماً <span>شهرياً</span></p><ul><li>10 مواقع</li><li>لا رسوم من Renoxium على مدفوعات البطاقة</li><li>2,000 رمز دخول عبر واتساب شهرياً</li><li>10 غيغابايت للصور</li><li>نطاقك الخاص، و10 موظفين لكل موقع</li><li>بلا شارة</li></ul><a class="btn line" href="/signup">اختر Business</a></div>
</div>
<p class="sum" data-r><b>متى تغطي خطة Pro تكلفتها؟</b> في خطة Free، مبيعات بالبطاقة قدرها 10,000 درهم في الشهر تكلّف {{.FeeOn10k}} درهم رسوماً لـ Renoxium. سعر Pro هو 99 درهماً، فتصبح أوفر عندما تتجاوز مبيعاتك بالبطاقة {{.ProFrom}} درهماً في الشهر. الدفع عند الاستلام لا رسوم عليه من Renoxium أبداً.</p>
<p class="note">تخصم Stripe رسوم البطاقات الخاصة بها من حسابك في Stripe في كل الخطط. بناء موقعك وتعديله عبر مساعدك غير محدود في كل الخطط؛ وتحتاج إلى حسابك الخاص في ChatGPT أو Claude.</p></div></section>

<section class="block" aria-labelledby="faq-h"><div class="wrap"><h2 id="faq-h" data-r>أسئلة يطرحها أصحاب الأنشطة</h2><div class="faq" data-r>
<details><summary>هل أحتاج إلى معرفة تقنية؟</summary><p>لا. تصف نشاطك بكلماتك، بالعربية أو الإنجليزية. يتولى المساعد البناء والاختبار، وتراجع النتيجة في معاينة قبل نشرها.</p></details>
<details><summary>أي مساعد ذكاء اصطناعي أحتاج؟</summary><p>ChatGPT (خطة مدفوعة مع وضع المطوّر) أو Claude، بحسابك الخاص. تضيف Renoxium إليه مرة واحدة؛ و<a href="/connect">صفحة الربط</a> تشرح الخطوات.</p></details>
<details><summary>أتواصل مع عملائي على واتساب وإنستغرام. أين مكان الموقع؟</summary><p>واصل محادثتهم هناك. شارك رابط موقعك في نبذة حسابك وفي ردودك: الموقع وأدوات المساعد الذكي تستقبل الطلب والدفع والحجز وتضعها في لوحتك وجدولك، فلا يضيع شيء في المحادثات.</p></details>
<details><summary>هل يستطيع عملائي الطلب عبر ChatGPT أو Claude؟</summary><p>يمكنهم إضافة نشاطك إلى ChatGPT أو Claude والسؤال عن منتجاتك وأسعارك وأوقاتك المتاحة. وللحجز أو الدفع يوجّههم المساعد إلى الصفحة المناسبة في موقعك.</p></details>
<details><summary>ماذا لو أخطأ المساعد؟</summary><p>تذهب التغييرات دائماً إلى معاينة خاصة أولاً، ولا ينشر أحد غيرك. وإن فاتك شيء، عُد إلى إصدار سابق من لوحتك؛ فبياناتك تُنسخ احتياطياً قبل كل نشر.</p></details>
<details><summary>هل أحتاج إلى Stripe؟</summary><p>لمدفوعات البطاقة فقط. الدفع عند الاستلام والدفع في المكان يعملان دونه. وعند ربط Stripe تذهب المدفوعات مباشرة إلى حسابك أنت.</p></details>
<details><summary>هل أستطيع استخدام نطاقي الخاص؟</summary><p>نعم، في خطتي Pro و Business، مثل <span dir="ltr">www.yourshop.ae</span>. وفي خطة Free يكون موقعك على <span dir="ltr">yourname.renoxium.app</span>.</p></details>
<details><summary>من يملك بيانات العملاء والطلبات؟</summary><p>أنت. نزّل كل ما يحتويه موقعك في أي وقت، أو احذف حسابك وكل بياناته.</p></details>
</div></div></section>

<section class="block end" aria-labelledby="end-h"><div class="wrap" data-r>
<h2 id="end-h">جرّب نسخة تجريبية قبل التسجيل</h2><p class="sub">احجز قص شعر، أو اطلب عطراً، أو اطلب عرض سعر في إحدى التجارب، ثم افتح لوحة تحكم صاحب النشاط لترى ما ستحصل عليه.</p>
<div class="cta"><a class="btn" href="/templates">جرّب نسخة تجريبية</a><a class="btn line" href="/signup">أنشئ حسابك</a></div>
<p class="note">صفحات إنشاء الحساب والقوالب والربط ولوحة التحكم متاحة بالإنجليزية حالياً. أما المواقع التي تبنيها فتعمل بالعربية والإنجليزية.</p></div></section>
</main>
<div class="wrap"><footer class="site"><span>© {{.Legal.Company}}</span><div class="links"><a href="/templates">القوالب</a><a href="/connect">الربط</a><a href="/privacy">الخصوصية</a><a href="/terms">الشروط</a><a href="/support">الدعم</a><a href="/abuse">الإبلاغ عن موقع</a><a href="/" lang="en" dir="ltr">English</a><a href="/login">تسجيل الدخول</a></div>{{template "contact" .Legal}}</footer></div>
{{template "landing-js"}}
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
</style>{{template "og" .Base}}</head><body>
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

{{define "support"}}{{template "lhead" "Support | Renoxium"}}{{template "legaltop" .}}
<h1>Support</h1><p class="meta">Help with your Renoxium account, your apps and the Renoxium connector in ChatGPT and Claude.</p>
<h2>Contact us</h2><p>{{with .Legal.Email}}Write to <a href="mailto:{{.}}">{{.}}</a>{{else}}Sign in and write to us from your account{{end}} with your account email and, if it is about an app, its address. We answer within two working days (UAE time), in English or Arabic.</p>
<h2>Connect Renoxium to your AI assistant</h2><p>Add Renoxium as a connector (in ChatGPT it is listed under apps and plugins) with this address, then sign in with your Renoxium account when asked:</p>
<p><code>{{.MCP}}</code></p><p>Step-by-step instructions for each assistant are on <a href="/connect">Connect</a>.</p>
<h2>Common questions</h2>
<p><b>My changes are not on my site.</b> Your assistant changes the preview only. Open the confirm link it gives you and choose Publish, signed in to Renoxium, to put the new version live.</p>
<p><b>My assistant cannot see my apps.</b> Disconnect and connect Renoxium again in the assistant's settings, and sign in with the same email you use on Renoxium.</p>
<p><b>Something on my site does not work.</b> Ask your assistant to read the app's log and fix it; it can see errors from your visitors' pages. You can also undo a change by publishing an earlier version in your admin panel.</p>
<p><b>I want to stop using Renoxium.</b> On the Account page you can download all your data and delete your account. Removing the connector from your assistant ends its access right away.</p>
<h2>More</h2><p><a href="/privacy">Privacy Policy</a> · <a href="/terms">Terms of Service</a> · <a href="/abuse">Report a site</a>{{with .Abuse}} (or <a href="mailto:{{.}}">{{.}}</a>){{end}}</p></main></div>
{{template "footer" .Legal}}</body></html>{{end}}

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
