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
	cards := s.galleryCards()
	return map[string]any{"MCP": s.base + "/mcp", "Fee": 2, "Legal": s.legalOrDefaults(), "Cards": cards, "Paths": s.sitePaths(cards),
		"Hero": photos["quote-skyline"], "Courier": photos["deliver-courier"]}
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
<title>{{.}}</title><meta name="description" content="Websites for UAE small businesses that take orders, bookings and payments, built by chatting with ChatGPT or Claude: online stores, bookings, rentals, quotes and listings, with card payments, cash on delivery, English and Arabic.">
<meta name="theme-color" content="#0e6f66">
<link rel="preconnect" href="https://fonts.googleapis.com"><link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
<link href="https://fonts.googleapis.com/css2?family=Readex+Pro:wght@300;400;500;600;700&display=swap" rel="stylesheet">
<style>
:root{color-scheme:light dark;--logo-a:#0f1b2d;--logo-b:#2d4470;--logo-c:#d4af5a;--deep-1:#15233b;--deep-2:#0a1322;--deep-soft:#b7c0d0;--gold:#d4af5a;--gold-hover:#e2c27a;--gold-ink:#0f1b2d;--overlay:10 19 34;--ink:#0f1b2d;--soft:#55627a;--paper:#f6f5f1;--paper-2:#eceae3;--panel:#ffffff;--panel-2:#faf9f6;--rule:#e2e0d8;--rule-strong:#cfcbbf;
--blue:#1b2a4a;--blue-hover:#13203a;--blue-ink:#ffffff;--blue-soft:#e6e9f0;--live:#2e7d5b;--bubble:#e9ecf3;--sun:#c9a227;--sun-soft:#f6eed3;--sun-ink:#2b2206;--deep:#0f1b2d;--deep-ink:#eef1f6;
--shade:15 27 45;--shadow-sm:0 1px 2px rgb(var(--shade) / .06),0 1px 1px rgb(var(--shade) / .03);--shadow:0 1px 2px rgb(var(--shade) / .05),0 10px 28px -14px rgb(var(--shade) / .22);--shadow-lg:0 2px 4px rgb(var(--shade) / .04),0 28px 56px -28px rgb(var(--shade) / .38)}
@media (prefers-color-scheme:dark){:root{--ink:#e8ecf3;--soft:#9aa6ba;--paper:#0a111d;--paper-2:#0e1726;--panel:#121c2d;--panel-2:#172235;--rule:#223049;--rule-strong:#2e3f5e;
--blue:#d4af5a;--blue-hover:#e2c27a;--blue-ink:#0f1b2d;--blue-soft:#2a2616;--live:#4cc88a;--bubble:#1a2740;--sun:#d4af5a;--sun-soft:#2e2715;--sun-ink:#1d1606;--deep:#0c1525;--deep-1:#111d33;--deep-2:#08101d;--deep-ink:#e8ecf3;--deep-soft:#aab5c8;--overlay:6 12 22;
--shadow-sm:0 1px 2px rgba(0,0,0,.35);--shadow:0 1px 2px rgba(0,0,0,.3),0 12px 30px -14px rgba(0,0,0,.6);--shadow-lg:0 2px 4px rgba(0,0,0,.25),0 30px 60px -28px rgba(0,0,0,.75)}
img{filter:brightness(.94)}}
*{box-sizing:border-box}html{-webkit-text-size-adjust:100%}
body{margin:0;position:relative;isolation:isolate;background:var(--paper);color:var(--ink);font:17px/1.6 "Readex Pro",system-ui,-apple-system,"Segoe UI",sans-serif;-webkit-font-smoothing:antialiased;overflow-wrap:break-word}
img{max-width:100%;height:auto;display:block}
a{color:var(--blue);text-underline-offset:3px}
:focus-visible{outline:3px solid var(--blue);outline-offset:3px;border-radius:8px}
.wrap{max-width:1180px;margin:0 auto;padding:0 20px}
.skip{position:absolute;left:-9999px;top:12px}.skip:focus{left:16px;z-index:20;background:var(--panel);color:var(--ink);padding:8px 14px;border-radius:10px;box-shadow:var(--shadow)}
.hero-bg{position:absolute;inset:0 0 auto;height:min(1180px,100%);z-index:-1;overflow:hidden;pointer-events:none;
background:radial-gradient(52% 46% at 88% 4%,color-mix(in srgb,var(--sun) 24%,transparent),transparent 72%),radial-gradient(48% 52% at 4% 12%,color-mix(in srgb,var(--blue) 20%,transparent),transparent 72%),linear-gradient(180deg,var(--paper-2),var(--paper) 78%)}
.hero-bg::after{content:"";position:absolute;inset:0;background-image:linear-gradient(var(--rule) 1px,transparent 1px),linear-gradient(90deg,var(--rule) 1px,transparent 1px);background-size:56px 56px;opacity:.55;-webkit-mask-image:radial-gradient(70% 55% at 50% 0%,#000,transparent);mask-image:radial-gradient(70% 55% at 50% 0%,#000,transparent)}
nav.top{display:flex;align-items:center;justify-content:space-between;gap:10px 20px;padding:18px 0;flex-wrap:wrap}
.logo{display:inline-flex;align-items:center;gap:10px;font-weight:700;font-size:20px;color:var(--ink);text-decoration:none;letter-spacing:-.015em}
.logo i{width:30px;height:30px;border-radius:9px;display:grid;place-items:center;font-style:normal;font-size:16px;font-weight:700;color:#fff;background:linear-gradient(135deg,var(--logo-a),var(--logo-b) 60%,var(--logo-c));box-shadow:inset 0 0 0 1px rgba(255,255,255,.2),var(--shadow-sm)}
nav.top .links{display:flex;align-items:center;gap:2px 4px;font-size:15px;flex-wrap:wrap}
nav.top .links a{color:var(--ink);text-decoration:none;padding:8px 12px;border-radius:10px}
nav.top .links a:hover{background:color-mix(in srgb,var(--ink) 7%,transparent)}
nav.top .links a[aria-current]{color:var(--blue);font-weight:500}
nav.top .links a.join{background:var(--ink);color:var(--paper);font-weight:500;padding:8px 14px;margin-inline-start:6px}nav.top .links a.join:hover{background:var(--blue);color:var(--blue-ink)}
@media(max-width:560px){nav.top .links .wide{display:none}nav.top .links a{padding:8px 9px}}
.btn{display:inline-flex;align-items:center;justify-content:center;gap:8px;min-height:48px;padding:12px 22px;border-radius:12px;font:inherit;font-weight:500;font-size:16px;line-height:1.2;text-decoration:none;cursor:pointer;
background:var(--blue);color:var(--blue-ink);border:1px solid var(--blue);box-shadow:inset 0 1px 0 rgba(255,255,255,.16),0 8px 18px -10px color-mix(in srgb,var(--blue) 80%,transparent);transition:background-color .15s,border-color .15s,color .15s,transform .15s}
.btn:hover{background:var(--blue-hover);border-color:var(--blue-hover)}.btn:active{transform:translateY(1px)}
.btn.quiet{background:var(--panel);color:var(--ink);border-color:var(--rule-strong);box-shadow:var(--shadow-sm)}.btn.quiet:hover{border-color:var(--blue);color:var(--blue)}
.btn.lg{min-height:54px;padding:14px 26px;font-size:17px;border-radius:14px}.btn.sm{min-height:40px;padding:8px 15px;font-size:15px;border-radius:10px}
.kicker{display:inline-flex;align-items:center;gap:8px;margin:0 0 14px;font-size:13.5px;font-weight:500;letter-spacing:.06em;text-transform:uppercase;color:var(--blue)}
footer.site{border-top:1px solid var(--rule);padding:32px 0 48px;font-size:14px;color:var(--soft);display:flex;gap:14px 24px;flex-wrap:wrap;justify-content:space-between;align-items:center}
footer.site .links{display:flex;gap:6px 18px;flex-wrap:wrap}footer.site a{color:var(--soft)}footer.site a:hover{color:var(--ink)}
@media (prefers-reduced-motion:reduce){*{animation:none!important;transition:none!important}}
</style>{{end}}

{{define "nav"}}<a class="skip" href="#main">Skip to content</a><header class="wrap"><nav class="top" aria-label="Main"><a class="logo" href="/"><i aria-hidden="true">R</i>Renoxium</a><div class="links">
<a href="/templates"{{if eq . "templates"}} aria-current="page"{{end}}>Templates</a><a class="wide" href="/#pricing">Pricing</a><a class="wide" href="/connect">Connect</a><a href="/login">Sign in</a><a class="join" href="/signup">Start free</a></div></nav></header>{{end}}

{{define "landing"}}{{template "lhead" "Renoxium: orders, bookings and payments online, built in a chat"}}<style>
.hero{padding:28px 0 36px;text-align:center}
.eyebrow{display:inline-flex;align-items:center;gap:10px;margin:0 0 22px;padding:6px 14px 6px 8px;border-radius:999px;background:var(--panel);border:1px solid var(--rule);box-shadow:var(--shadow-sm);font-size:14px;color:var(--soft)}
@media(max-width:480px){.eyebrow{font-size:13px}}.eyebrow b{font-weight:500;color:var(--blue-ink);background:var(--blue);border-radius:999px;padding:2px 9px;font-size:12.5px}
h1{margin:0 auto 20px;max-width:16em;font-weight:600;letter-spacing:-.035em;line-height:1.05;font-size:clamp(36px,5.4vw,64px);text-wrap:balance}
h1 .ar{display:block;margin:16px auto 0;direction:rtl;font-weight:400;font-size:.46em;letter-spacing:0;line-height:1.4;color:var(--blue)}
.lede{font-size:clamp(17px,1.5vw,19px);color:var(--soft);max-width:38em;margin:0 auto 30px;text-wrap:pretty}
.cta{display:flex;gap:12px;flex-wrap:wrap;align-items:center;justify-content:center}
.small{font-size:14px;color:var(--soft);margin:16px 0 0}
.paths{display:grid;gap:22px;margin-top:48px;text-align:start}
@media(min-width:960px){.paths{grid-template-columns:repeat(3,1fr);gap:24px}}
.path{background:var(--panel);border:1px solid var(--rule);border-radius:24px;overflow:hidden;box-shadow:var(--shadow-lg);display:flex;flex-direction:column;transition:transform .2s,box-shadow .2s}
.path:hover{transform:translateY(-3px)}
@media(min-width:640px) and (max-width:959px){.path{display:grid;grid-template-columns:1fr 1.1fr}.path .ph{aspect-ratio:auto;min-height:100%}}
.ph{position:relative;aspect-ratio:16/10;background:var(--paper-2)}
.ph>img{width:100%;height:100%;object-fit:cover}
.ph .n{position:absolute;inset-inline-start:14px;top:14px;padding:4px 11px;border-radius:999px;font-size:12.5px;font-weight:500;color:#fff;background:rgb(var(--overlay) / .55);-webkit-backdrop-filter:blur(8px);backdrop-filter:blur(8px)}
.extra{position:absolute;inset-inline-end:16px;bottom:-24px;display:flex}
.extra img{width:66px;height:66px;object-fit:cover;border-radius:16px;border:3px solid var(--panel);box-shadow:var(--shadow);margin-inline-start:-16px;transform:rotate(-6deg)}
.extra img+img{transform:rotate(5deg)}
@media(min-width:640px) and (max-width:959px){.extra{bottom:16px}}
.pb{padding:26px 24px 24px;display:flex;flex-direction:column;gap:12px;flex:1}
.pb h2{font-size:27px;line-height:1.15;letter-spacing:-.02em;margin:0;font-weight:600}
.promise{margin:0;color:var(--soft);font-size:16px}.inc{margin:0;font-size:14px;color:var(--soft);display:flex;flex-wrap:wrap;gap:4px 12px}.inc span{display:inline-flex;align-items:center;gap:6px}.inc span::before{content:"";width:6px;height:6px;border-radius:2px;background:var(--sun)}
.suits{list-style:none;padding:0;margin:2px 0 0;display:flex;flex-wrap:wrap;gap:6px}.suits li{font-size:13px;padding:3px 10px;border-radius:999px;background:var(--blue-soft);color:var(--ink)}
.demos{margin-top:auto;padding-top:10px;display:grid;gap:8px}
.demos a{display:flex;align-items:center;gap:10px;padding:11px 14px;border:1px solid var(--rule);border-radius:12px;text-decoration:none;color:var(--ink);font-weight:500;font-size:15.5px;background:var(--panel-2);transition:border-color .15s,background-color .15s}
.demos a small{margin-inline-start:auto;color:var(--blue);font-size:13.5px;white-space:nowrap}.demos a:hover{border-color:var(--blue);background:var(--blue-soft)}
.also{margin:28px 0 0;color:var(--soft);font-size:15.5px;text-align:center}
.trust{border-block:1px solid var(--rule);background:var(--panel)}
.trust ul{list-style:none;margin:0;padding:22px 0;display:grid;grid-template-columns:1fr 1fr;gap:14px 20px}
@media(min-width:900px){.trust ul{display:flex;justify-content:space-between}}
.trust li{display:flex;align-items:center;gap:10px;font-weight:500;font-size:15px}
.trust svg{flex:none;width:34px;height:34px;padding:7px;border-radius:10px;background:var(--blue-soft);color:var(--blue)}
.block{padding:clamp(64px,8vw,92px) 0}.block.alt{background:var(--panel);border-block:1px solid var(--rule)}
h2{font-size:clamp(28px,3.8vw,42px);letter-spacing:-.025em;margin:0 0 14px;line-height:1.1;font-weight:600;max-width:20em;text-wrap:balance}
.sub{color:var(--soft);max-width:38em;margin:0;font-size:18px}
.head{margin-bottom:44px}
.versus{display:grid;gap:20px}@media(min-width:860px){.versus{grid-template-columns:1fr 1fr;gap:24px}}
.vs{border-radius:22px;padding:28px;border:1px solid var(--rule)}
.vs h3{margin:0 0 16px;font-size:19px;font-weight:600;display:flex;align-items:center;gap:10px}
.vs ul{list-style:none;margin:0;padding:0;display:grid;gap:14px}
.vs li{display:grid;grid-template-columns:24px 1fr;gap:12px;align-items:start}
.vs li::before{content:"";width:24px;height:24px;border-radius:50%;margin-top:1px}
.before{background:color-mix(in srgb,var(--panel) 55%,transparent);color:var(--soft)}
.before li::before{background:var(--paper-2) url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 24 24' fill='none' stroke='%23889c98' stroke-width='2.4' stroke-linecap='round'%3E%3Cpath d='M8 8l8 8M16 8l-8 8'/%3E%3C/svg%3E") center/18px no-repeat}
.after{background:var(--panel);box-shadow:var(--shadow-lg);border-color:color-mix(in srgb,var(--blue) 35%,var(--rule))}
.after li::before{background:var(--blue) url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 24 24' fill='none' stroke='white' stroke-width='2.6' stroke-linecap='round' stroke-linejoin='round'%3E%3Cpath d='M6.5 12.5l3.5 3.5 7.5-8'/%3E%3C/svg%3E") center/18px no-repeat}
.after h3 .tag{white-space:nowrap;font-size:12px;font-weight:500;padding:2px 9px;border-radius:999px;background:var(--sun-soft);color:var(--ink)}
.split{display:grid;gap:48px;align-items:center}@media(min-width:960px){.split{grid-template-columns:1fr 1fr;gap:72px}}
.steps{list-style:none;padding:0;margin:0;display:grid;gap:26px;counter-reset:s}
.steps li{counter-increment:s;display:grid;grid-template-columns:44px 1fr;gap:16px}
.steps li::before{content:counter(s);width:44px;height:44px;border-radius:14px;display:grid;place-items:center;font-weight:600;font-size:18px;color:var(--blue);background:var(--blue-soft);border:1px solid color-mix(in srgb,var(--blue) 25%,transparent)}
.steps h3{margin:6px 0 4px;font-size:19px;font-weight:600}.steps p{margin:0;color:var(--soft)}.steps q{display:inline-block;margin-top:10px;font-size:14.5px;padding:6px 12px;border-radius:12px 12px 12px 4px;background:var(--bubble);color:var(--ink);quotes:"\201C" "\201D"}
.chat{background:var(--panel);border:1px solid var(--rule);border-radius:24px;padding:20px;display:flex;flex-direction:column;gap:10px;font-size:15.5px;box-shadow:var(--shadow-lg);position:relative}
.chat .bar{display:flex;align-items:center;gap:8px;padding:0 4px 12px;margin-bottom:4px;border-bottom:1px solid var(--rule);font-size:13.5px;color:var(--soft)}.chat .bar i{width:10px;height:10px;border-radius:50%;background:var(--rule-strong)}.chat .bar span{margin-inline-start:6px}
.msg{max-width:88%;padding:11px 15px;border-radius:18px;line-height:1.5}
.me{align-self:flex-end;background:var(--blue);color:var(--blue-ink);border-end-end-radius:5px}.me.ar{direction:rtl}
.agent{align-self:flex-start;background:var(--bubble);border-end-start-radius:5px}
.who{font-size:12.5px;color:var(--soft);margin:6px 2px -4px}.who.right{align-self:flex-end}
.url{display:flex;align-items:center;gap:10px;margin-top:10px;padding:10px 12px;border-radius:12px;background:var(--panel);font-size:14.5px;box-shadow:var(--shadow-sm)}
.url{flex-wrap:wrap}.url b{font-weight:600;white-space:nowrap}.url .dot{width:9px;height:9px;border-radius:50%;background:var(--sun);flex:none}.url span:last-child{color:var(--soft);margin-inline-start:auto;font-size:13px;white-space:nowrap}
.day{background:radial-gradient(60% 80% at 100% 0%,color-mix(in srgb,var(--sun) 16%,transparent),transparent 70%),linear-gradient(160deg,var(--deep-1),var(--deep-2));color:var(--deep-ink);border-radius:28px;padding:clamp(24px,5vw,56px);display:grid;gap:36px;box-shadow:var(--shadow-lg)}
@media(min-width:960px){.day{grid-template-columns:.9fr 1.1fr;align-items:center}}
.day .kicker{color:var(--gold)}.day h2{color:inherit}.day .sub{color:var(--deep-soft);margin-bottom:22px}
.day ul{margin:0;padding:0;list-style:none;display:grid;gap:12px}.day li{padding-inline-start:28px;position:relative}.day li::before{content:"";position:absolute;inset-inline-start:2px;top:.5em;width:12px;height:12px;border-radius:4px;background:var(--gold)}
.panelmock{background:var(--panel);color:var(--ink);border-radius:20px;padding:18px;display:grid;gap:12px;font-size:14.5px;box-shadow:0 30px 60px -30px rgba(0,0,0,.6)}
.panelmock .top{display:flex;justify-content:space-between;align-items:center;gap:8px}.panelmock .top b{font-size:17px;font-weight:600}
.pill{display:inline-flex;align-items:center;gap:6px;font-size:12.5px;font-weight:500;padding:3px 10px;border-radius:999px;border:1px solid var(--rule)}.pill::before{content:"";width:7px;height:7px;border-radius:50%;background:var(--live)}
.figs{display:grid;grid-template-columns:repeat(3,1fr);gap:8px}.figs div{border:1px solid var(--rule);border-radius:12px;padding:10px 12px;background:var(--panel-2)}.figs small{display:block;color:var(--soft);font-size:12.5px}.figs b{font-size:18px;font-weight:600;font-variant-numeric:tabular-nums;white-space:nowrap}
@media(max-width:420px){.figs b{font-size:15.5px}}
.sched{border:1px solid var(--rule);border-radius:12px;overflow:hidden}.sched div{display:grid;grid-template-columns:48px 1fr auto;gap:10px;padding:9px 12px;border-top:1px solid var(--rule);align-items:center}.sched div:first-child{border-top:0}
.sched time{font-variant-numeric:tabular-nums;color:var(--soft)}.sched em{font-style:normal;font-size:12.5px;padding:2px 8px;border-radius:999px;background:var(--bubble);white-space:nowrap}.sched em.wait{background:var(--sun-soft)}
.wa{background:var(--bubble);border-radius:14px 14px 14px 4px;padding:10px 12px;font-size:14px}.wa small{display:block;color:var(--soft);font-size:12px;margin-bottom:2px}
.groups{display:grid;gap:18px}@media(min-width:760px){.groups{grid-template-columns:1fr 1fr}}
.group{background:var(--panel);border:1px solid var(--rule);border-radius:20px;padding:26px;box-shadow:var(--shadow)}
.group h3{margin:0 0 16px;font-size:19px;font-weight:600;display:flex;align-items:center;gap:12px}
.group h3 svg{flex:none;width:38px;height:38px;padding:8px;border-radius:12px;background:var(--blue-soft);color:var(--blue)}
.group ul{list-style:none;margin:0;padding:0;display:grid;gap:12px}.group li strong{display:block;font-weight:500}.group li span{color:var(--soft);font-size:15.5px}
.prices{display:grid;gap:18px;align-items:stretch}@media(min-width:900px){.prices{grid-template-columns:repeat(3,1fr)}}
.plan{position:relative;background:var(--panel);border:1px solid var(--rule);border-radius:22px;padding:28px;display:flex;flex-direction:column;box-shadow:var(--shadow)}
.plan.pick{border:2px solid var(--blue);box-shadow:var(--shadow-lg)}
@media(min-width:900px){.plan.pick{transform:translateY(-10px)}}
.plan .badge{position:absolute;top:-13px;inset-inline-start:26px;font-size:12.5px;font-weight:500;padding:4px 12px;border-radius:999px;background:var(--sun);color:var(--sun-ink)}
.plan h3{margin:0;font-size:20px;font-weight:600}.plan .for{color:var(--soft);font-size:15px;margin:4px 0 0}
.amount{font-size:42px;font-weight:600;letter-spacing:-.03em;margin:16px 0 4px;line-height:1.1}.amount span{font-size:16px;font-weight:400;color:var(--soft);letter-spacing:0}
.plan ul{padding:18px 0 0;margin:14px 0 24px;border-top:1px solid var(--rule);list-style:none;display:grid;gap:10px;font-size:15.5px}
.plan li{padding-inline-start:28px;position:relative}.plan li::before{content:"";position:absolute;inset-inline-start:0;top:.2em;width:18px;height:18px;border-radius:50%;background:var(--blue-soft) url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 24 24' fill='none' stroke='%230e6f66' stroke-width='3' stroke-linecap='round' stroke-linejoin='round'%3E%3Cpath d='M6.5 12.5l3.5 3.5 7.5-8'/%3E%3C/svg%3E") center/13px no-repeat}
.plan .btn{margin-top:auto;width:100%}.note{color:var(--soft);font-size:15px;margin-top:22px;max-width:46em}
.faq{max-width:860px;background:var(--panel);border:1px solid var(--rule);border-radius:22px;padding:6px 26px;box-shadow:var(--shadow)}
.faq details{border-top:1px solid var(--rule);padding:18px 0}.faq details:first-child{border-top:0}
.faq summary{cursor:pointer;font-weight:500;font-size:18px;list-style:none;display:flex;justify-content:space-between;gap:16px;align-items:center}
.faq summary::after{content:"+";flex:none;width:30px;height:30px;border-radius:50%;display:grid;place-items:center;color:var(--blue);background:var(--blue-soft);font-size:20px;line-height:1}.faq details[open] summary::after{content:"\2212"}
.faq summary::-webkit-details-marker{display:none}.faq p{margin:10px 0 0;color:var(--soft);max-width:42em}
.end{position:relative;isolation:isolate;overflow:hidden;border-radius:28px;padding:clamp(40px,7vw,88px) clamp(22px,5vw,64px);color:#fff;text-align:center;margin-bottom:64px;box-shadow:var(--shadow-lg)}
.end>img{position:absolute;inset:0;width:100%;height:100%;object-fit:cover;z-index:-2;filter:none}
.end::before{content:"";position:absolute;inset:0;z-index:-1;background:linear-gradient(180deg,rgb(var(--overlay) / .5),rgb(var(--overlay) / .82))}
.end h2{margin:0 auto 14px;color:#fff}.end .sub{margin:0 auto 30px;color:var(--deep-soft)}.end .cta{justify-content:center}
.end .btn{background:var(--gold);border-color:var(--gold);color:var(--gold-ink);box-shadow:0 10px 24px -12px rgba(0,0,0,.6)}.end .btn:hover{background:var(--gold-hover);border-color:var(--gold-hover)}
.end .btn.quiet{background:rgba(255,255,255,.08);border-color:rgba(255,255,255,.5);color:#fff}.end .btn.quiet:hover{background:rgba(255,255,255,.16)}
.end :focus-visible{outline-color:var(--gold)}
.bhero{position:relative;isolation:isolate;color:#fff;overflow:hidden;padding-bottom:52px;background:var(--deep-2)}
.bhero>.bg{position:absolute;inset:0;width:100%;height:100%;object-fit:cover;z-index:-2;filter:saturate(.7) contrast(1.05)}
.bhero::before{content:"";position:absolute;inset:0;z-index:-1;background:linear-gradient(90deg,rgb(var(--overlay) / .88) 0%,rgb(var(--overlay) / .62) 55%,rgb(var(--overlay) / .42) 100%),linear-gradient(0deg,rgb(var(--overlay) / .9) 0%,transparent 50%)}
body:has(.bhero)>header.wrap{position:absolute;inset:0 0 auto;z-index:3}
body:has(.bhero)>header nav.top .logo,body:has(.bhero)>header nav.top .links a{color:#fff}body:has(.bhero)>header nav.top .links a:hover{background:rgba(255,255,255,.12)}body:has(.bhero)>header nav.top .links a.join{background:var(--gold);color:var(--gold-ink)}
.bhero{padding-top:84px}.btext{padding:44px 0 40px;max-width:760px;text-align:start}
.bhero .eyebrow{background:rgba(255,255,255,.1);border-color:rgba(255,255,255,.22);color:#fff;box-shadow:none}.bhero .eyebrow b{background:var(--gold);color:var(--gold-ink)}
.bhero h1{margin:0 0 18px;color:#fff;max-width:none;text-align:start;font-size:clamp(34px,4.6vw,60px)}
.bhero h1 .ar{margin:12px 0 0;width:fit-content;color:var(--gold)}
.bhero .lede{margin:0 0 26px;font-size:18px;color:rgba(255,255,255,.84);max-width:34em;text-align:start}
.bhero .cta{justify-content:flex-start}.bhero .small{color:rgba(255,255,255,.7);text-align:start}
.bhero .btn{background:var(--gold);border-color:var(--gold);color:var(--gold-ink)}.bhero .btn:hover{background:var(--gold-hover);border-color:var(--gold-hover)}
.bhero .btn.quiet{background:rgba(255,255,255,.1);border-color:rgba(255,255,255,.55);color:#fff}.bhero .btn.quiet:hover{background:rgba(255,255,255,.18)}
.bhero :focus-visible{outline-color:var(--gold)}
.bpaths{display:grid;gap:16px}@media(min-width:960px){.bpaths{grid-template-columns:repeat(3,1fr)}}
.bpath{background:rgba(255,255,255,.1);border:1px solid rgba(255,255,255,.22);-webkit-backdrop-filter:blur(16px) saturate(1.3);backdrop-filter:blur(16px) saturate(1.3);border-radius:22px;padding:18px;display:grid;grid-template-columns:84px 1fr;gap:14px 16px;align-items:start;box-shadow:0 24px 48px -28px rgba(0,0,0,.6)}
.bpath img{width:84px;height:84px;object-fit:cover;border-radius:16px;filter:none}
.bpath h2{font-size:22px;margin:2px 0 4px;color:#fff;letter-spacing:-.015em}.bpath p{margin:0;color:rgba(255,255,255,.82);font-size:15px;line-height:1.5}
.bpath p.for{margin-top:6px;font-size:13.5px;color:var(--gold)}
.bpath .demos{grid-column:1/-1;display:flex;flex-direction:column;gap:8px;margin:0;padding:0}
.bpath .demos a{display:flex;justify-content:space-between;gap:10px;background:rgba(255,255,255,.94);color:var(--ink);padding:9px 14px;font-size:14.5px;font-weight:500;border-radius:999px;text-decoration:none}
.bpath .demos a small{color:var(--blue);font-size:13.5px}.bpath .demos a:hover{background:#fff}
.bhero .also{color:rgba(255,255,255,.74);text-align:start;margin-top:22px}.bhero .also a{color:#fff}
@media(max-width:700px){.bhero{padding-top:132px}.bhero::before{background:linear-gradient(180deg,rgb(var(--overlay) / .82),rgb(var(--overlay) / .7) 60%,rgb(var(--overlay) / .92))}}
@media(max-width:600px){.btext{padding:32px 0 32px}.bpath{grid-template-columns:64px 1fr;padding:14px}.bpath img{width:64px;height:64px}}
@media (prefers-color-scheme:dark){.bpath .demos a{background:rgba(18,28,45,.92);color:var(--ink)}.bpath .demos a:hover{background:var(--panel-2)}}
</style></head><body>
{{template "nav" "home"}}
<main id="main"><section class="bhero" aria-labelledby="hero-h">{{with .Hero}}<img class="bg" src="{{.Src}}" srcset="{{.SrcSet}}" sizes="100vw" width="1600" height="1067" alt="" fetchpriority="high">{{end}}
<div class="wrap"><div class="btext">
<p class="eyebrow"><b>UAE</b>For businesses that sell, book and quote</p>
<h1 id="hero-h">Get orders, bookings and payments online, set up in a chat. <span class="ar" lang="ar">موقع عملك، جاهز من محادثة.</span></h1>
<p class="lede">Move your business out of WhatsApp and Instagram DMs. Tell ChatGPT or Claude what you sell or book: Renoxium builds the site from a finished UAE template, hosts it, and takes card payments and cash on delivery, in English and Arabic.</p>
<div class="cta"><a class="btn lg" href="/signup">Create your account</a><a class="btn quiet lg" href="/templates">Try the templates</a></div>
<p class="small">Free to start. No code, no designer, nothing to install.</p></div>
<div class="bpaths">{{range .Paths}}<article class="bpath" id="{{.ID}}" aria-labelledby="{{.ID}}-h"><img src="{{.Photo.Small}}" width="400" height="267" alt="{{.Photo.Alt}}">
<div><h2 id="{{.ID}}-h">{{.Title}}</h2><p>{{.Promise}}</p><p class="for">{{range $i, $s := .Suits}}{{if $i}} · {{end}}{{$s}}{{end}}</p></div>
<div class="demos">{{range .Demos}}<a href="{{.DemoURL}}/">{{.Title}}<small>Try the demo →</small></a>{{end}}</div></article>{{end}}</div>
<p class="also">Delivering too? <a href="/templates#delivery-tracking">Live delivery tracking</a> shows customers the courier on a map. <a href="/templates">See every template</a></p>
</div></section>

<section class="trust" aria-label="Included on every plan"><div class="wrap"><ul>
<li><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><rect x="2.5" y="5" width="19" height="14" rx="2.5"/><path d="M2.5 10h19M6.5 15h4"/></svg>Cards into your own Stripe</li>
<li><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><rect x="2.5" y="6" width="19" height="12" rx="2"/><circle cx="12" cy="12" r="2.6"/><path d="M6 9.5v5M18 9.5v5"/></svg>Cash on delivery</li>
<li><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M19 5L5 19"/><circle cx="7" cy="7" r="2.5"/><circle cx="17" cy="17" r="2.5"/></svg>AED with 5% VAT</li>
<li><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M3 5h8M7 3v2M9.5 5c-.8 4-3.2 7-6.5 8.5M5.5 9c1 2 2.8 3.6 5 4.5"/><path d="M12.5 21l4-9 4 9M14 17.5h5"/></svg>English and Arabic</li>
<li><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M4 20l1.3-3.9A8 8 0 1 1 8 19z"/><path d="M9 9.5c0 3 2.5 5.5 5.5 5.5l1-1.5-2-1-1 1c-1-.5-2-1.5-2.5-2.5l1-1-1-2z"/></svg>New orders on WhatsApp</li>
</ul></div></section>

<section class="block" aria-labelledby="dm-h"><div class="wrap">
<div class="head"><p class="kicker">Why a website</p><h2 id="dm-h">Your customers already message you. Give them a place to pay.</h2>
<p class="sub">Keep WhatsApp for the conversation. Let your site show the prices and free times, take the order or the booking, and collect the money, day and night.</p></div>
<div class="versus"><div class="vs before"><h3>Running it in the DMs</h3><ul>
<li>“How much?” answered one message at a time</li><li>Bank transfer screenshots to check by hand</li><li>Double bookings and missed messages after hours</li><li>Orders scattered across chats and notebooks</li></ul></div>
<div class="vs after"><h3>On your Renoxium site <span class="tag">Same day</span></h3><ul>
<li>Prices, sizes and free times on the page, in English and Arabic</li><li>Card payments into your own Stripe, or cash on delivery</li><li>Bookings checked against your calendar, with deposits</li><li>Every order and booking in one panel, with a WhatsApp alert</li></ul></div></div>
</div></section>

<section class="block alt" aria-labelledby="how"><div class="wrap split">
<div><div class="head"><p class="kicker">How it works</p><h2 id="how">From a message to your first order</h2><p class="sub">You talk, the assistant builds, you decide what goes live.</p></div>
<ol class="steps">
<li><div><h3>Connect Renoxium</h3><p>Add Renoxium to ChatGPT or Claude once and sign in. It takes two minutes.</p></div></li>
<li><div><h3>Describe your business</h3><p>Name, what you sell or book, prices, delivery or opening hours. The assistant builds it and tests every page.</p><q>Add a gift set for AED 320.</q></div></li>
<li><div><h3>Check, publish, sell</h3><p>Open the private preview on your phone. When you are happy, press Publish. Connect Stripe to take cards.</p></div></li>
</ol></div>
<div class="chat" aria-label="Example conversation"><div class="bar" aria-hidden="true"><i></i><i></i><i></i><span>ChatGPT · Renoxium connected</span></div>
<div class="who right">You</div>
<div class="msg me">Make a booking site for Lamsa, my salon in Jumeirah. Haircuts, colour and nails, three stylists.</div>
<div class="msg me ar" lang="ar">وأريد عربون ٥٠ درهم عند الحجز</div>
<div class="who">Renoxium</div>
<div class="msg agent">Done. Customers pick a service, a stylist and a free time, pay a AED 50 deposit by card, and get a reminder the day before. I tested every page. Check the preview, then publish.
<div class="url"><span class="dot" aria-hidden="true"></span><b>lamsa.renoxium.app</b><span>preview ready</span></div></div>
<div class="who right">You</div>
<div class="msg me">Close on Fridays until 2pm.</div>
</div></div></section>

<section class="block" aria-labelledby="day"><div class="wrap"><div class="day"><div><p class="kicker">Your admin panel</p><h2 id="day">Run it from your phone</h2>
<p class="sub">Every site comes with an admin panel made from your own data: products, orders, bookings, customers. Change a price yourself, or ask your assistant.</p>
<ul><li>New orders and bookings by email and WhatsApp</li><li>A calendar for appointments, crews and rentals</li><li>Card sales today, this month and the last 30 days</li><li>Staff who can help, without seeing your payments</li><li>A daily report at closing time</li></ul></div>
<div class="panelmock" aria-label="Example admin panel"><div class="top"><b>lamsa-salon</b><span class="pill">Live</span></div>
<div class="figs"><div><small>Today</small><b>AED 640</b></div><div><small>Bookings</small><b>9</b></div><div><small>Customers</small><b>214</b></div></div>
<div class="sched"><div><time>10:00</time><span>Haircut &amp; blow-dry · Mariam</span><em>Paid</em></div><div><time>11:30</time><span>Root colour · Huda</span><em>Paid</em></div><div><time>13:00</time><span>Gel manicure · Sara</span><em class="wait">Deposit due</em></div></div>
<div class="wa"><small>WhatsApp · Renoxium</small>New booking: Classic manicure tomorrow 16:00 with Noor, AED 70.</div></div></div></div></section>

<section class="block alt" aria-labelledby="built"><div class="wrap"><div class="head"><p class="kicker">Included</p><h2 id="built">Built in, on every plan</h2><p class="sub">The parts small businesses usually pay a developer for, already working and tested.</p></div><div class="groups">
<div class="group"><h3><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><rect x="2.5" y="5" width="19" height="14" rx="2.5"/><path d="M2.5 10h19M6.5 15h4"/></svg>Getting paid</h3><ul><li><strong>Cards into your own Stripe</strong><span>Customers pay on Stripe's page; the money goes to your account.</span></li><li><strong>Cash on delivery and pay at the venue</strong><span>Works without Stripe.</span></li><li><strong>Deposits, card holds, refunds and monthly plans</strong><span>Refund from your panel in one step.</span></li><li><strong>AED with 5% VAT</strong><span>Tax invoices once you add your TRN.</span></li></ul></div>
<div class="group"><h3><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><circle cx="9" cy="8" r="3.5"/><path d="M2.5 20c.8-3.6 3.4-5.5 6.5-5.5s5.7 1.9 6.5 5.5"/><path d="M16 4.8a3.5 3.5 0 0 1 0 6.4M18 14.8c1.8.7 3 2.4 3.5 5.2"/></svg>Your customers</h3><ul><li><strong>Accounts with email or a WhatsApp code</strong><span>They see their orders and bookings and pay again if a card failed.</span></li><li><strong>English and Arabic</strong><span>Right-to-left layout, switched with one tap.</span></li><li><strong>Private document uploads</strong><span>Passport or licence copies only you can open.</span></li><li><strong>Ask ChatGPT about your shop</strong><span>Customers can add your site to their AI assistant to find products.</span></li></ul></div>
<div class="group"><h3><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><rect x="3" y="4.5" width="18" height="16" rx="2.5"/><path d="M3 9.5h18M8 2.5v4M16 2.5v4M7.5 13.5h3M7.5 17h6"/></svg>Running the business</h3><ul><li><strong>Admin panel on any phone</strong><span>Products, photos, orders, customers.</span></li><li><strong>Calendar</strong><span>Day and week views by stylist, crew or item.</span></li><li><strong>Staff access</strong><span>Helpers manage orders without your payments or documents.</span></li><li><strong>Notices and reports</strong><span>New orders on WhatsApp, a daily summary by email.</span></li></ul></div>
<div class="group"><h3><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M12 2.8l7.5 3v5.6c0 4.6-3.1 8.3-7.5 9.8-4.4-1.5-7.5-5.2-7.5-9.8V5.8z"/><path d="M8.8 12.2l2.2 2.2 4.3-4.6"/></svg>Safe to change</h3><ul><li><strong>Preview first</strong><span>Every change goes to a private preview. Nothing is live until you press Publish.</span></li><li><strong>Roll back</strong><span>Go back to an earlier version; live data is backed up first.</span></li><li><strong>Your own domain</strong><span>www.yourshop.ae on Pro and Business.</span></li><li><strong>Your data stays yours</strong><span>Download everything, or delete your account and all of it.</span></li></ul></div>
</div></div></section>

<section class="block" id="pricing" aria-labelledby="pricing-h"><div class="wrap"><div class="head"><p class="kicker">Pricing</p><h2 id="pricing-h">Start free, upgrade when it pays</h2><p class="sub">Upgrade when you need more sites, your own domain or no fee on card payments.</p></div><div class="prices">
<div class="plan"><h3>Free</h3><p class="for">To try it with real customers</p><p class="amount">AED 0</p><ul><li>1 site</li><li>Renoxium takes {{.Fee}}% of card payments</li><li>50 WhatsApp sign-in codes a month</li><li>200 MB of images</li><li>Small "Made with Renoxium" badge</li></ul><a class="btn quiet" href="/signup">Start free</a></div>
<div class="plan pick"><span class="badge">Most popular</span><h3>Pro</h3><p class="for">For one growing business</p><p class="amount">AED 99 <span>/ month</span></p><ul><li>3 sites</li><li>No Renoxium fee on card payments</li><li>500 WhatsApp sign-in codes a month</li><li>2 GB of images</li><li>Your own domain, 2 staff per site</li><li>No badge</li></ul><a class="btn" href="/signup">Choose Pro</a></div>
<div class="plan"><h3>Business</h3><p class="for">For several branches or brands</p><p class="amount">AED 299 <span>/ month</span></p><ul><li>10 sites</li><li>No Renoxium fee on card payments</li><li>2,000 WhatsApp sign-in codes a month</li><li>10 GB of images</li><li>Your own domain, 10 staff per site</li><li>No badge</li></ul><a class="btn quiet" href="/signup">Choose Business</a></div>
</div><p class="note">Stripe charges its own card processing fees to your Stripe account on every plan. Building and changing your site through your AI assistant is unlimited on every plan; you need your own ChatGPT or Claude account.</p></div></section>

<section class="block alt" aria-labelledby="faq"><div class="wrap"><div class="head"><p class="kicker">FAQ</p><h2 id="faq">Questions owners ask</h2></div><div class="faq">
<details><summary>Do I need to know anything technical?</summary><p>No. You describe your business in your own words, in English or Arabic. The assistant does the building and testing, and you check the result in a preview before it goes live.</p></details>
<details><summary>Which AI assistant do I need?</summary><p>ChatGPT (a paid plan with developer mode) or Claude, with your own account. You add Renoxium to it once; the <a href="/connect">connect page</a> shows the steps.</p></details>
<details><summary>I already sell on WhatsApp and Instagram. Why a site?</summary><p>Keep chatting with customers there. The site answers the price and availability questions for you, takes the order, booking or deposit, and puts it in one list, so nothing gets lost in a chat. Share its link in your bio and your replies.</p></details>
<details><summary>What if the assistant makes a mistake?</summary><p>Changes always go to a private preview first, and only you can publish. If something slips through, roll back to an earlier version from your panel; live data is backed up before every publish.</p></details>
<details><summary>Do I need Stripe?</summary><p>Only for card payments. Cash on delivery and pay-at-the-venue work without it. When you connect Stripe, payments go straight to your own Stripe account.</p></details>
<details><summary>Can I use my own domain?</summary><p>Yes, on Pro and Business, for example www.yourshop.ae. On Free your site lives at yourname.renoxium.app.</p></details>
<details><summary>Who owns the customers and orders?</summary><p>You do. Download everything your site holds at any time, or delete your account and all of its data.</p></details>
</div></div></section>

<div class="wrap" style="padding-top:96px"><section class="end" aria-labelledby="end-h"><img src="{{.Hero.Src}}" srcset="{{.Hero.SrcSet}}" sizes="(min-width:1180px) 1140px, 100vw" width="800" height="533" alt="" loading="lazy">
<h2 id="end-h">See it working before you sign up</h2><p class="sub">Book an appointment, order from a shop or request a viewing on a demo, then open the owner's panel to see what you would get.</p>
<div class="cta"><a class="btn lg" href="/templates">Try the templates</a><a class="btn quiet lg" href="/signup">Create your account</a></div></section></div>
</main>
<div class="wrap"><footer class="site"><span>© {{.Legal.Company}}</span><div class="links"><a href="/templates">Templates</a><a href="/connect">Connect</a><a href="/privacy">Privacy</a><a href="/terms">Terms</a><a href="/abuse">Report a site</a><a href="/login">Sign in</a></div></footer></div>
</body></html>{{end}}

{{define "gallery"}}{{template "lhead" "Templates · Renoxium"}}<style>
.intro{padding:28px 0 8px;max-width:760px}.intro h1{font-size:clamp(34px,5vw,56px);line-height:1.05;letter-spacing:-.03em;margin:0 0 16px;font-weight:600;text-wrap:balance}
.intro>p:not(.kicker){font-size:18px;color:var(--soft);margin:0}
.jobs{display:flex;flex-wrap:wrap;gap:8px;list-style:none;padding:0;margin:24px 0 0}.jobs li{font-size:14px;padding:6px 14px;border-radius:999px;background:var(--panel);border:1px solid var(--rule);box-shadow:var(--shadow-sm)}
.jobs b{font-weight:500}.jobs span{color:var(--soft)}
.cards{display:grid;gap:22px;padding:40px 0 72px}@media(min-width:720px){.cards{grid-template-columns:1fr 1fr}}@media(min-width:1040px){.cards{grid-template-columns:repeat(3,1fr)}}
.tcard{background:var(--panel);border:1px solid var(--rule);border-radius:22px;overflow:hidden;display:flex;flex-direction:column;box-shadow:var(--shadow);transition:transform .2s,box-shadow .2s;scroll-margin-top:24px}
.tcard:hover{transform:translateY(-3px);box-shadow:var(--shadow-lg)}.tcard:target{outline:3px solid var(--blue);outline-offset:3px}
.tph{position:relative;aspect-ratio:16/10;background:var(--paper-2)}.tph img{width:100%;height:100%;object-fit:cover}.tph img.right{object-position:72% 50%}
.tph .job{position:absolute;inset-inline-start:14px;top:14px;padding:4px 11px;border-radius:999px;font-size:12.5px;font-weight:500;color:#fff;background:rgb(var(--overlay) / .58);-webkit-backdrop-filter:blur(8px);backdrop-filter:blur(8px)}
.tb{padding:22px 22px 20px;display:flex;flex-direction:column;gap:10px;flex:1}
.tb h2{font-size:22px;margin:0;letter-spacing:-.015em;font-weight:600}.tb p{margin:0;color:var(--soft);font-size:15.5px}
.tags{display:flex;flex-wrap:wrap;gap:6px;list-style:none;padding:0;margin:2px 0}.tags li{font-size:13px;padding:3px 10px;border-radius:999px;background:var(--blue-soft);color:var(--ink)}
.actions{display:flex;flex-wrap:wrap;gap:8px;align-items:center;margin-top:auto;padding-top:10px}.actions .btn{flex:1 1 auto;padding:8px 12px;font-size:14.5px}
.more{border:2px dashed var(--rule-strong);background:transparent;box-shadow:none;justify-content:center}.more:hover{transform:none;box-shadow:none}.more .tb{justify-content:center}.more h2{font-size:21px}
details{font-size:14px;color:var(--soft);border-top:1px solid var(--rule);padding-top:12px;margin-top:4px}details summary{cursor:pointer;color:var(--blue);font-weight:500}
details code{display:block;margin-top:8px;padding:10px 12px;border-radius:10px;background:var(--panel-2);border:1px solid var(--rule);color:var(--ink);white-space:normal;font-size:13.5px;overflow-wrap:anywhere}
.how{margin-bottom:64px;border-radius:26px;padding:clamp(26px,5vw,52px);background:radial-gradient(60% 90% at 100% 0%,color-mix(in srgb,var(--sun) 18%,transparent),transparent 70%),linear-gradient(160deg,var(--deep-1),var(--deep-2));color:var(--deep-ink);box-shadow:var(--shadow-lg);display:grid;gap:22px}
@media(min-width:900px){.how{grid-template-columns:1.2fr auto;align-items:center}}
.how h2{font-size:clamp(26px,3vw,34px);letter-spacing:-.02em;margin:0 0 10px;font-weight:600}.how p{color:var(--deep-soft);max-width:40em;margin:0}
.how .cta{display:flex;flex-wrap:wrap;gap:10px}.how .btn{background:var(--gold);border-color:var(--gold);color:var(--gold-ink)}.how .btn:hover{background:var(--gold-hover);border-color:var(--gold-hover)}
.how .btn.quiet{background:rgba(255,255,255,.08);border-color:rgba(255,255,255,.5);color:#fff}.how :focus-visible{outline-color:var(--gold)}
</style></head><body>
<div class="hero-bg" aria-hidden="true" style="height:640px"></div>
{{template "nav" "templates"}}
<main id="main" class="wrap">
<div class="intro"><p class="kicker">Templates</p><h1>Templates you can try right now</h1>
<p>Every template is a finished website for a UAE business: payments, bookings, customer accounts, English and Arabic. Open the demo and use it like a customer, then look at the owner's admin panel. Nothing is charged or sent; demo data is deleted every hour.</p>
<ul class="jobs" aria-label="What the templates do"><li><b>Sell products</b> <span>· shops, cakes, flowers</span></li><li><b>Take bookings</b> <span>· salons, home services, tours</span></li><li><b>Send quotes</b> <span>· consultants, property</span></li></ul></div>
<div class="cards">{{range .Cards}}<article class="tcard" id="{{.Name}}">{{if .Photo.Name}}<div class="tph"><img{{if .Photo.Right}} class="right"{{end}} src="{{.Photo.Src}}" srcset="{{.Photo.SrcSet}}" sizes="(min-width:1180px) 370px, (min-width:1040px) 31vw, (min-width:720px) 46vw, 100vw" width="800" height="533" alt="{{.Photo.Alt}}" loading="lazy">{{with .Job}}<span class="job">{{.}}</span>{{end}}</div>{{end}}
<div class="tb"><h2>{{.Title}}</h2><p>{{.Summary}}</p>
<ul class="tags" aria-label="Suits">{{range .For}}<li>{{.}}</li>{{end}}</ul>
<div class="actions"><a class="btn sm" href="{{.DemoURL}}/">Try the demo</a><a class="btn quiet sm" href="{{.AdminURL}}">Owner's admin panel</a></div>
<details><summary>Use this template</summary><p>Connect Renoxium to ChatGPT or Claude, then ask:</p><code>{{.Prompt}}</code></details></div></article>{{end}}
<article class="tcard more"><div class="tb"><p class="kicker">Something in between?</p><h2>Ask for a mix</h2><p>Start from the closest template and tell your assistant what to change: other fields on a form, your own sections and texts, your colours and photos.</p><div class="actions"><a class="btn quiet sm" href="/connect">How to connect</a></div></div></article></div>
<section class="how" aria-labelledby="mine"><div><h2 id="mine">Make it yours</h2><p>Connect Renoxium to ChatGPT or Claude and ask for the template by name with your business name, services and prices. Your assistant builds your own copy, you check the preview and publish it.</p></div>
<div class="cta"><a class="btn" href="/signup">Create your account</a><a class="btn quiet" href="/connect">Connect ChatGPT or Claude</a></div></section>
</main>
<div class="wrap"><footer class="site"><span>© {{.Legal.Company}}</span><div class="links"><a href="/connect">Connect</a><a href="/privacy">Privacy</a><a href="/terms">Terms</a><a href="/abuse">Report a store</a><a href="/login">Sign in</a></div></footer></div>
</body></html>{{end}}

{{define "legaltop"}}<style>.doc{max-width:760px;margin:8px auto 64px;padding:clamp(22px,5vw,48px);background:var(--panel);border:1px solid var(--rule);border-radius:22px;box-shadow:var(--shadow)}
.doc h1{font-size:clamp(30px,4vw,38px);letter-spacing:-.025em;margin:0 0 8px;font-weight:600;line-height:1.1}
.doc h2{font-size:20px;margin:32px 0 8px;font-weight:600}.doc p,.doc li{color:var(--ink)}.doc .meta{color:var(--soft)}
.draft{background:var(--sun-soft);color:var(--ink);border:1px solid color-mix(in srgb,var(--sun) 45%,transparent);border-radius:12px;padding:12px 16px;margin:0 0 20px}</style></head><body>
<div class="hero-bg" aria-hidden="true" style="height:420px"></div><a class="skip" href="#main">Skip to content</a><header class="wrap"><nav class="top" aria-label="Main"><a class="logo" href="/"><i aria-hidden="true">R</i>Renoxium</a><div class="links"><a href="/privacy">Privacy</a><a href="/terms">Terms</a></div></nav></header><div class="wrap"><main class="doc" id="main">
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
<h2>Changes</h2><p>We will post changes on this page and tell store owners by email about important ones.</p></main></div></body></html>{{end}}

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
<h2>Law</h2><p>These terms are governed by the laws of the United Arab Emirates.{{with .Legal.Email}} Questions: <a href="mailto:{{.}}">{{.}}</a>.{{end}}</p></main></div></body></html>{{end}}
`))
