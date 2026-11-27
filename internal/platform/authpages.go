package platform

import (
	"context"
	"html/template"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

// The visitor sign-in pages are the platform's own (see idhost.go): nothing
// of the app runs there. They still look like the business: its name and
// brand colour come from the row the app's definition points to (Brand),
// and they speak Arabic when the visitor came from the Arabic site.

// authLang is "ar" when the visitor came from the app's Arabic pages
// (?lang=ar on the page or in the page to return to) or their browser asks
// for Arabic, else "en".
func authLang(r *http.Request, next string) string {
	q := r.URL.Query()
	switch {
	case q.Get("lang") == "ar", r.PostFormValue("lang") == "ar", strings.Contains(next, "lang=ar"):
		return "ar"
	case q.Get("lang") == "en", r.PostFormValue("lang") == "en", strings.Contains(next, "lang=en"):
		return "en"
	}
	if al := strings.ToLower(r.Header.Get("Accept-Language")); strings.HasPrefix(al, "ar") {
		return "ar"
	}
	return "en"
}

var hexColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// brandOf reads the business's name and colour for the platform's pages.
// Anything missing or malformed falls back to the app's address and navy.
func (p *Platform) brandOf(ctx context.Context, st *stage, lang string) (name, color string) {
	color = "#13203a"
	b := st.def.Brand
	if b == nil {
		return "", color
	}
	cols := []string{quote(b.Name)}
	if b.NameAr != "" {
		cols = append(cols, quote(b.NameAr)+" AS name_ar")
	}
	if b.Color != "" {
		cols = append(cols, quote(b.Color)+" AS color")
	}
	rows, err := st.db.Query(ctx, "SELECT "+strings.Join(cols, ", ")+" FROM "+quote(b.Table)+" ORDER BY id LIMIT 1", nil)
	if err != nil || len(rows) == 0 {
		return "", color
	}
	name, _ = rows[0][b.Name].(string)
	if ar, _ := rows[0]["name_ar"].(string); lang == "ar" && strings.TrimSpace(ar) != "" {
		name = ar
	}
	if c, _ := rows[0]["color"].(string); hexColor.MatchString(c) {
		color = c
	}
	if r := []rune(strings.TrimSpace(name)); len(r) > 60 {
		name = string(r[:60])
	}
	return strings.TrimSpace(name), color
}

// darkVariant keeps a brand colour that would vanish on the dark theme's
// background (near black) readable: such colours become a light neutral.
func darkVariant(hex string) string {
	if luminance(hex) < 0.03 {
		return "#e7ecf4"
	}
	return hex
}

func luminance(hex string) float64 {
	v, err := strconv.ParseUint(strings.TrimPrefix(hex, "#"), 16, 32)
	if err != nil {
		return 0
	}
	lin := func(c uint64) float64 {
		x := float64(c) / 255
		if x <= 0.04045 {
			return x / 12.92
		}
		return math.Pow((x+0.055)/1.055, 2.4)
	}
	return 0.2126*lin(v>>16&255) + 0.7152*lin(v>>8&255) + 0.0722*lin(v&255)
}

// inkOn is the text colour that reads on a background colour: white on
// dark colours, near-black on light ones.
func inkOn(hex string) string {
	if luminance(hex) > 0.4 {
		return "#111827"
	}
	return "#ffffff"
}

// authText is every word of the sign-in pages, English then Arabic.
var authText = map[string][2]string{
	"sign_in":         {"Sign in", "تسجيل الدخول"},
	"email":           {"Email", "البريد الإلكتروني"},
	"password":        {"Password", "كلمة المرور"},
	"forgot_q":        {"Forgot your password?", "نسيت كلمة المرور؟"},
	"new_here":        {"New here?", "جديد هنا؟"},
	"create_account":  {"Create an account", "إنشاء حساب"},
	"create_btn":      {"Create account", "إنشاء الحساب"},
	"or":              {"or", "أو"},
	"with_whatsapp":   {"Sign in with WhatsApp", "الدخول عبر واتساب"},
	"with_email":      {"Use email and password", "استخدم البريد الإلكتروني وكلمة المرور"},
	"wa_intro":        {"We send a 6-digit code to your WhatsApp. No password needed.", "نرسل رمزًا من 6 أرقام إلى واتساب. لا حاجة إلى كلمة مرور."},
	"wa_number":       {"WhatsApp number", "رقم واتساب"},
	"send_code":       {"Send code", "إرسال الرمز"},
	"enter_code":      {"Enter the code", "أدخل الرمز"},
	"code_sent":       {"We sent a code to %s on WhatsApp. It works for 10 minutes.", "أرسلنا رمزًا إلى %s عبر واتساب. صالح لمدة 10 دقائق."},
	"code_preview":    {"Preview: no WhatsApp message is sent to %s. Ask your assistant for the code: it is in the app's logs. It works for 10 minutes.", "معاينة: لا تُرسل رسالة واتساب إلى %s. اطلب الرمز من مساعدك: إنه في سجلات التطبيق. صالح لمدة 10 دقائق."},
	"code":            {"Code", "الرمز"},
	"your_name_first": {"Your name (first time only)", "اسمك (أول مرة فقط)"},
	"new_code":        {"Send a new code", "إرسال رمز جديد"},
	"name":            {"Name", "الاسم"},
	"pw_new":          {"Password (at least 10 characters)", "كلمة المرور (10 أحرف على الأقل)"},
	"have_account":    {"Already have an account?", "لديك حساب؟"},
	"forgot_title":    {"Forgot your password?", "نسيت كلمة المرور؟"},
	"forgot_intro":    {"Enter your email and we will send you a link to choose a new one.", "أدخل بريدك الإلكتروني وسنرسل لك رابطًا لاختيار كلمة مرور جديدة."},
	"send_link":       {"Send me a link", "أرسل لي الرابط"},
	"back_sign_in":    {"Back to sign in", "العودة إلى تسجيل الدخول"},
	"new_password":    {"Choose a new password", "اختر كلمة مرور جديدة"},
	"pw_new_field":    {"New password (at least 10 characters)", "كلمة المرور الجديدة (10 أحرف على الأقل)"},
	"save_sign_in":    {"Save and sign in", "حفظ وتسجيل الدخول"},
	"sign_out_q":      {"Sign out?", "تسجيل الخروج؟"},
	"sign_out":        {"Sign out", "تسجيل الخروج"},
	"continue":        {"Continue", "متابعة"},
	"back_to":         {"Back to", "العودة إلى"},
	"secured":         {"Secure sign-in by Renoxium. %s never sees your password.", "تسجيل دخول آمن عبر Renoxium. لا يرى %s كلمة المرور الخاصة بك."},
	"this_site":       {"This site", "هذا الموقع"},
	"show":            {"Show", "إظهار"},
}

// authArabic translates the messages the handlers write in English.
var authArabic = map[string]string{
	"Too many new WhatsApp sign-ups right now. Please sign up with your email instead.":                              "عدد كبير من التسجيلات عبر واتساب الآن. يرجى التسجيل بالبريد الإلكتروني.",
	"We could not send a WhatsApp code just now. Check the number and try again, or continue with your email below.": "تعذر إرسال رمز واتساب الآن. تحقق من الرقم وحاول مرة أخرى، أو تابع باستخدام بريدك الإلكتروني أدناه.",
	"Too many attempts. Try again in an hour.":                                                                       "محاولات كثيرة. حاول مرة أخرى بعد ساعة.",
	"The form expired. Go back, reload the page and try again.":                                                      "انتهت صلاحية النموذج. ارجع وأعد تحميل الصفحة ثم حاول مرة أخرى.",
	"Please try again":                                                  "يرجى المحاولة مرة أخرى",
	"This code has expired. Ask for a new one.":                         "انتهت صلاحية هذا الرمز. اطلب رمزًا جديدًا.",
	"That code is not right. Check the WhatsApp message and try again.": "الرمز غير صحيح. تحقق من رسالة واتساب وحاول مرة أخرى.",
	"This link has expired or was already used. Ask for a new one.":     "انتهت صلاحية هذا الرابط أو استُخدم من قبل. اطلب رابطًا جديدًا.",
	"Link expired": "انتهت صلاحية الرابط",
	"Too many attempts. Try again in a few minutes.":              "محاولات كثيرة. حاول مرة أخرى بعد بضع دقائق.",
	"Enter a valid email address.":                                "أدخل بريدًا إلكترونيًا صحيحًا.",
	"The password must be at least 10 characters.":                "يجب أن تتكون كلمة المرور من 10 أحرف على الأقل.",
	"An account with this email already exists. Sign in instead.": "يوجد حساب بهذا البريد الإلكتروني. سجّل الدخول بدلًا من ذلك.",
	"Too many attempts. Try again in 15 minutes.":                 "محاولات كثيرة. حاول مرة أخرى بعد 15 دقيقة.",
	"Wrong email or password.":                                    "البريد الإلكتروني أو كلمة المرور غير صحيحة.",
	"Try again later.":                                            "حاول مرة أخرى لاحقًا.",
	"Too many attempts":                                           "محاولات كثيرة",
	"Check your email":                                            "تحقق من بريدك الإلكتروني",
	"This confirmation link has expired or was already used.":     "انتهت صلاحية رابط التأكيد أو استُخدم من قبل.",
	"Thank you. Your email address is confirmed.":                 "شكرًا. تم تأكيد بريدك الإلكتروني.",
	"Email confirmed":                                             "تم تأكيد البريد الإلكتروني",
	"Not available":                                               "غير متاح",
	"Sign-in with WhatsApp is not available right now. Please sign in with your email instead.": "تسجيل الدخول عبر واتساب غير متاح الآن. يرجى تسجيل الدخول بالبريد الإلكتروني.",
	"Enter your WhatsApp number with the country code, for example +971 50 123 4567.":           "أدخل رقم واتساب مع رمز الدولة، مثل ‎+971 50 123 4567.",
	"Too many codes requested. Try again in 15 minutes.":                                        "طلبت رموزًا كثيرة. حاول مرة أخرى بعد 15 دقيقة.",
}

var checkEmailRE = regexp.MustCompile(`^If an account exists for (.+), we sent it a link to choose a new password\.$`)

func translateAuth(s, lang string) string {
	if lang != "ar" || s == "" {
		return s
	}
	if t, ok := authArabic[s]; ok {
		return t
	}
	if m := checkEmailRE.FindStringSubmatch(s); m != nil {
		return "إذا كان هناك حساب لـ " + m[1] + "، فقد أرسلنا إليه رابطًا لاختيار كلمة مرور جديدة."
	}
	return s
}

// authWords is the page's dictionary in one language.
func authWords(lang string) map[string]string {
	i := 0
	if lang == "ar" {
		i = 1
	}
	out := make(map[string]string, len(authText))
	for k, v := range authText {
		out[k] = v[i]
	}
	return out
}

var authFuncs = template.FuncMap{
	"fill": func(format, value string) string { return strings.Replace(format, "%s", value, 1) },
}

var defaultAuthPages = template.Must(template.New("").Funcs(authFuncs).Parse(`
{{define "top"}}<!doctype html><html lang="{{.lang}}" dir="{{.dir}}"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="robots" content="noindex"><title>{{.pageTitle}}{{with .brand}} · {{.}}{{end}}</title>
<style>
:root{color-scheme:light dark;--brand:{{.color}};--on-brand:{{.ink}};--fg:#111827;--muted:#5b6474;--bg:#f4f5f7;--card:#fff;--line:#dcdfe5;--field:#fff;--bad:#b42318;--ok:#167a43}
@media (prefers-color-scheme:dark){:root{--brand:{{.colorDark}};--on-brand:{{.inkDark}};--fg:#eef0f4;--muted:#a3abb9;--bg:#0f1218;--card:#181c24;--line:#2c3240;--field:#11151c;--bad:#ff8a80;--ok:#5fd394}}
*{box-sizing:border-box}html{-webkit-text-size-adjust:100%}
body{margin:0;min-height:100vh;display:flex;flex-direction:column;align-items:center;background:var(--bg);color:var(--fg);
font:16px/1.55 system-ui,-apple-system,"Segoe UI",Roboto,"Noto Sans Arabic","Geeza Pro",Tahoma,sans-serif;padding:24px 16px 32px}
.top{width:100%;max-width:420px;display:flex;align-items:center;gap:12px;margin:8px 0 20px}
.mono{width:44px;height:44px;border-radius:12px;background:var(--brand);color:var(--on-brand);display:flex;align-items:center;justify-content:center;font-weight:700;font-size:20px;flex:none}
.top strong{display:block;font-size:17px;line-height:1.25}.top .addr{display:block;font-size:13px;color:var(--muted);overflow-wrap:anywhere}
main{width:100%;max-width:420px;background:var(--card);border:1px solid var(--line);border-radius:18px;padding:28px 24px;box-shadow:0 1px 2px rgb(0 0 0 / .04),0 12px 32px -18px rgb(0 0 0 / .25)}
h1{font-size:24px;line-height:1.25;margin:0 0 6px;letter-spacing:-.01em}.lead{color:var(--muted);margin:0 0 18px;font-size:15px}
label{display:block;font-size:14px;font-weight:600;margin:16px 0 6px}
input{width:100%;padding:12px 14px;border:1px solid var(--line);border-radius:10px;background:var(--field);color:var(--fg);font:inherit;font-size:16px}
input:focus{outline:none;border-color:var(--brand);box-shadow:0 0 0 3px color-mix(in srgb,var(--brand) 25%,transparent)}
input[dir=ltr]{text-align:start}
.code{letter-spacing:.4em;font-size:22px;text-align:center;font-variant-numeric:tabular-nums}
button,.btn{display:flex;width:100%;align-items:center;justify-content:center;gap:8px;margin-top:22px;min-height:48px;padding:12px 16px;border:1px solid var(--brand);border-radius:10px;background:var(--brand);color:var(--on-brand);font:inherit;font-weight:600;cursor:pointer;text-decoration:none}
button:hover,.btn:hover{filter:brightness(1.08)}.btn.alt{background:transparent;color:var(--fg);border-color:var(--line);margin-top:0}
.wa{display:inline-block;width:20px;height:20px;border-radius:50%;background:#25d366;flex:none}
:focus-visible{outline:3px solid var(--brand);outline-offset:2px}
.links{display:flex;flex-wrap:wrap;justify-content:space-between;gap:8px;margin-top:16px;font-size:14px}
a{color:var(--fg);text-decoration-color:var(--muted);text-underline-offset:3px}a:hover{text-decoration-color:var(--brand)}
.or{display:flex;align-items:center;gap:12px;color:var(--muted);font-size:13px;margin:22px 0 14px}.or::before,.or::after{content:"";flex:1;height:1px;background:var(--line)}
.alert{border-radius:10px;padding:10px 12px;font-size:14px;margin:12px 0 0;background:color-mix(in srgb,var(--bad) 10%,transparent);color:var(--bad);border:1px solid color-mix(in srgb,var(--bad) 30%,transparent)}
.okmsg{font-size:15px;margin:0 0 6px}.okmsg.ok{color:var(--ok)}
.foot{max-width:420px;width:100%;margin-top:18px;font-size:13px;color:var(--muted);text-align:center}.foot a{color:var(--muted)}
.foot p{margin:4px 0}
</style></head><body>
<header class="top"><span class="mono" aria-hidden="true">{{.initial}}</span><div><strong>{{if .brand}}{{.brand}}{{else}}{{.t.this_site}}{{end}}</strong><span class="addr" dir="ltr">{{.host}}</span></div></header><main>{{end}}

{{define "end"}}</main>{{with .demo}}{{.}}{{end}}{{if .home}}<footer class="foot"><p>{{fill .t.secured (or .brand .host)}}</p><p><a href="{{.home}}">{{.t.back_to}} {{.host}}</a></p></footer>{{end}}</body></html>{{end}}

{{define "error"}}{{with .error}}<p class="alert error" role="alert">{{.}}</p>{{end}}{{end}}
{{define "hidden"}}<input type="hidden" name="csrf" value="{{.csrf}}"><input type="hidden" name="next" value="{{.next}}"><input type="hidden" name="lang" value="{{.lang}}">{{end}}

{{define "login"}}{{template "top" .}}<h1>{{.t.sign_in}}</h1>{{template "error" .}}
<form method="post" action="/_rx/auth/login">{{template "hidden" .}}
<label for="email">{{.t.email}}</label><input id="email" name="email" type="email" dir="ltr" autocomplete="email" required value="{{.email}}">
<label for="password">{{.t.password}}</label><input id="password" name="password" type="password" dir="ltr" autocomplete="current-password" required>
<button>{{.t.sign_in}}</button></form>
<div class="links"><a href="/_rx/auth/forgot?lang={{.lang}}">{{.t.forgot_q}}</a><span>{{.t.new_here}} <a href="/_rx/auth/signup?next={{.next}}&amp;lang={{.lang}}">{{.t.create_account}}</a></span></div>
{{if .whatsapp}}<div class="or">{{.t.or}}</div><a class="btn alt" href="/_rx/auth/phone?next={{.next}}&amp;lang={{.lang}}"><span class="wa" aria-hidden="true"></span>{{.t.with_whatsapp}}</a>{{end}}{{template "end" .}}{{end}}

{{define "phone"}}{{template "top" .}}<h1>{{.t.with_whatsapp}}</h1><p class="lead">{{.t.wa_intro}}</p>{{template "error" .}}
<form method="post" action="/_rx/auth/phone">{{template "hidden" .}}
<label for="phone">{{.t.wa_number}}</label><input id="phone" name="phone" type="tel" dir="ltr" autocomplete="tel" inputmode="tel" placeholder="+971 50 123 4567" required value="{{.phone}}">
<button>{{.t.send_code}}</button></form><div class="or">{{.t.or}}</div><a class="btn alt" href="/_rx/auth/login?next={{.next}}&amp;lang={{.lang}}">{{.t.with_email}}</a>{{template "end" .}}{{end}}

{{define "phone_code"}}{{template "top" .}}<h1>{{.t.enter_code}}</h1><p class="lead">{{if .preview}}{{fill .t.code_preview .phone}}{{else}}{{fill .t.code_sent .phone}}{{end}}</p>{{template "error" .}}
<form method="post" action="/_rx/auth/phone/verify">{{template "hidden" .}}<input type="hidden" name="phone" value="{{.phone}}">
<label for="code">{{.t.code}}</label><input id="code" class="code" name="code" dir="ltr" inputmode="numeric" autocomplete="one-time-code" pattern="[0-9]{6}" maxlength="6" required autofocus>
<label for="name">{{.t.your_name_first}}</label><input id="name" name="name" autocomplete="name">
<button>{{.t.sign_in}}</button></form><div class="links"><a href="/_rx/auth/phone?next={{.next}}&amp;lang={{.lang}}">{{.t.new_code}}</a></div>{{template "end" .}}{{end}}

{{define "signup"}}{{template "top" .}}<h1>{{.t.create_account}}</h1>{{template "error" .}}
<form method="post" action="/_rx/auth/signup">{{template "hidden" .}}
<label for="name">{{.t.name}}</label><input id="name" name="name" autocomplete="name" value="{{.name}}">
<label for="email">{{.t.email}}</label><input id="email" name="email" type="email" dir="ltr" autocomplete="email" required value="{{.email}}">
<label for="password">{{.t.pw_new}}</label><input id="password" name="password" type="password" dir="ltr" autocomplete="new-password" minlength="10" required>
<button>{{.t.create_btn}}</button></form>
<div class="links"><span>{{.t.have_account}} <a href="/_rx/auth/login?next={{.next}}&amp;lang={{.lang}}">{{.t.sign_in}}</a></span></div>
{{if .whatsapp}}<div class="or">{{.t.or}}</div><a class="btn alt" href="/_rx/auth/phone?next={{.next}}&amp;lang={{.lang}}"><span class="wa" aria-hidden="true"></span>{{.t.with_whatsapp}}</a>{{end}}{{template "end" .}}{{end}}

{{define "forgot"}}{{template "top" .}}<h1>{{.t.forgot_title}}</h1><p class="lead">{{.t.forgot_intro}}</p>
<form method="post" action="/_rx/auth/forgot">{{template "hidden" .}}
<label for="email">{{.t.email}}</label><input id="email" name="email" type="email" dir="ltr" autocomplete="email" required><button>{{.t.send_link}}</button></form>
<div class="links"><a href="/_rx/auth/login?lang={{.lang}}">{{.t.back_sign_in}}</a></div>{{template "end" .}}{{end}}

{{define "reset"}}{{template "top" .}}<h1>{{.t.new_password}}</h1>{{template "error" .}}
<form method="post" action="/_rx/auth/reset">{{template "hidden" .}}<input type="hidden" name="token" value="{{.token}}">
<label for="password">{{.t.pw_new_field}}</label><input id="password" name="password" type="password" dir="ltr" autocomplete="new-password" minlength="10" required>
<button>{{.t.save_sign_in}}</button></form>{{template "end" .}}{{end}}

{{define "logout"}}{{template "top" .}}<h1>{{.t.sign_out_q}}</h1>
<form method="post" action="/_rx/auth/logout"><button>{{.t.sign_out}}</button></form>{{template "end" .}}{{end}}

{{define "message"}}{{template "top" .}}<h1>{{.title}}</h1><p class="okmsg">{{.message}}</p>{{with .link}}<a class="btn" href="{{.}}">{{$.t.continue}}</a>{{end}}{{template "end" .}}{{end}}
`))
