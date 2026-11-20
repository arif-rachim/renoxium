# Renoxium template design system

Reference implementation: `examples/store-uae.json` (templates `css`, `head`, `top`, `bottom`; lib `common`, `theme`, `shop`).
Apply the same system to booking-uae, home-service-uae, reservation-uae, quote-portal-uae and listings-uae.
The pages belong to the merchant's brand, not to Renoxium: no Renoxium colours or fonts (Readex Pro is Renoxium's own; do not use it).

## 1. Principles

- Everything derives from one owner colour (`brand_color`) and one font choice (`font`). Never hard-code a brand hue in a template.
- Structure by spacing, type and at most one or two hairlines (`--line`). No shadows on cards, no gradients, no decorative borders.
- One radius per role: controls 8px (`--r-control`), media 4px (`--r-media`), sheets/panels 12px (`--r-sheet`).
- Brand colour is used as a fill (primary button, intro band, cart count, a 2px rule on invoices) and, via `--brand-text`, for
  selected states, focus rings and link hover. Body text is always `--ink`, never the raw brand colour.
- Phone first. No horizontal scroll at 360px. Fixed "action bar" at the bottom on phones for the page's main action.
- Light only (`color-scheme: light`). Dark mode was left out on purpose: owner colours are picked against white.
- Avoid the AI tells: no pill eyebrows, no ALL-CAPS labels, no 01/02/03 unless it is a real sequence, no arrows appended to links,
  no middle-dot meta strings ("A · B"), no emoji, no fake badges ("Bestseller", "Trusted by"), no identical shadowed cards.
  Sentence case everywhere. Meta goes in separate spans with a gap, or a `<dl>`.

## 2. Settings columns (add to the template's settings table: store / business / ...)

```json
{"name": "logo", "type": "image", "label": "Logo", "help": "Optional. A wide logo on a transparent or white background; it replaces the shop name in the header and on invoices."},
{"name": "brand_color", "type": "text", "label": "Brand colour", "help": "Your main colour as a hex code, e.g. #5C2A3D for deep plum or #1F4D3A for palm green. Buttons and highlights use it; text on it turns white or dark by itself."},
{"name": "font", "type": "text", "label": "Font", "help": "<the store-uae help text; under 300 characters>", "options": ["modern", "classic", "rounded", "heritage", "atelier", "boutique"]}
```
Also give every owner-facing text column an `_ar` twin where shoppers see it (name, tagline, address, legal_name, category, option names...).
Seed a tasteful brand colour and font that fit the example's business (store-uae: `#1F1B17` deep ink with `heritage`).
Templates that offer the luxury styles also add `hero_image` (wide photo), `story` / `story_ar` (one or two sentences) and
`story_image` (portrait photo); field help must stay under 300 characters (the platform refuses longer help).
Suggestions for the others (choose, do not copy blindly): salon/spa `#7A3E48` + classic or modern; home services `#1F4D3A` + modern;
tours/rentals `#8A4B1E` (desert amber, dark enough for white text) + modern; quote portal `#23395B` + modern; listings `#2F3E46` + modern.
Never teal, never terracotta #D97757, never acid green.

## 3. Fonts (Google Fonts, all checked for an Arabic subset)

| font value | Display (h1, h2, store name) | Text (body, UI, prices) | Stylesheet URL |
|---|---|---|---|
| modern (default) | IBM Plex Sans 600 | IBM Plex Sans + IBM Plex Sans Arabic 400/500 | `https://fonts.googleapis.com/css2?family=IBM+Plex+Sans+Arabic:wght@400;500;600&family=IBM+Plex+Sans:wght@400;500;600&display=swap` |
| classic | Markazi Text 600 (Latin and Arabic, one design) | IBM Plex Sans + IBM Plex Sans Arabic | `https://fonts.googleapis.com/css2?family=Markazi+Text:wght@500;600&family=IBM+Plex+Sans+Arabic:wght@400;500;600&family=IBM+Plex+Sans:wght@400;500;600&display=swap` |
| rounded | Rubik 600 | Rubik 400/500 (has Arabic) | `https://fonts.googleapis.com/css2?family=Rubik:wght@400;500;600&display=swap` |
| heritage (luxury) | Cormorant Garamond 300/400 (+ italic 300 for the story line); Arabic: Amiri 400 | Jost 400/500; Arabic: Noto Naskh Arabic 400/500 | see `head` below |
| atelier (luxury) | Bodoni Moda 400 (optical sizes); Arabic: Noto Naskh Arabic 400/500 | Hanken Grotesk 400/500; Arabic: Noto Kufi Arabic 400/500 | see `head` below |

The Latin face is listed first in each stack; Arabic glyphs fall through to the Arabic face, so one stack serves both languages.
Markazi has a small x-height, so `classic` sets `--display-boost: 1.16`, which every display size multiplies.
The luxury styles set their own sizes (section 3a) instead of the boost.

## 3a. Luxury styles: heritage and atelier

`font` chooses the type pair and, through `theme()`, the style: modern / classic / rounded give `data-style="standard"`,
heritage / atelier give `data-style="luxury"`. Templates render a luxury layout with `{{if .theme.lux}}` where the markup must
differ (header, home page) and otherwise style the same markup under `[data-style=luxury]` in CSS.

Why these faces: Cormorant Garamond is a light, high-contrast Garalde (the Amouage / Diptyque register) and Amiri is the classical
Naskh that matches its calligraphic contrast; Jost is a Futura-like geometric sans for small tracked labels; Noto Naskh Arabic
keeps Arabic body text calm and readable. Atelier pairs a Didone (Bodoni Moda, the Le Labo / Byredo fashion register) with a
plain grotesk (Hanken Grotesk); in Arabic the same logic gives Naskh for display (contrast) and Kufi for text (plain).
All five are on Google Fonts with the weights listed; the Latin faces have no Arabic, so every stack lists the Arabic face second.

| Token | heritage | atelier |
|---|---|---|
| `--paper` (page) | `#fbf8f2` ivory | `#ffffff` |
| `--well` (photo wells, visit block, notices) | `#f0eadf` stone | `#f3f2ef` |
| `--accent` (hairlines and underlines only) | `#a08048` antique gold | ink at 40% |
| `--brand` (owner colour: buttons, ink) | seed `#1F1B17` deep ink | `#000000` suggested |
| radii | 0 | 0 |

Rules of the luxury style:
- One bold moment: the full-bleed `hero_image` (min(84svh, 880px), 78svh on phones) with the tagline set large and light over a
  bottom gradient, and one text call to action (`.cta`: tracked label with a 1px accent underline). Without a photo the tagline
  stands alone on paper.
- Header: language switch at start, centred name as a tracked-capitals logotype (Arabic: Amiri, no tracking), account and cart at
  end; the cart count reads "Cart (2)", never a badge. Phones show only the other language and hide the account link (it is in the
  footer). The delivery note sits above, small and quiet, with no band of colour.
- Labels (navigation, buttons, legends, `dt`): Latin 12px, capitals, 0.16em tracking, weight 400/500. Arabic: 15px, no tracking,
  no capitals. This is the only place capitals are allowed, and only because it is the established language of the genre.
- Display type at 300 (heritage) / 400 (atelier), letter-spacing 0. Arabic display about 0.85x the Latin size, line-height 1.55-1.9.
  Body 16px / 1.65 (Arabic 17px / 1.9). Product names in the display face, prices 13px tracked in `--ink-2`.
- No boxes: no shadows, no radii, no tinted cards; hairlines (`--line`) and space instead. Grids get wide gutters (up to 40px
  across, 96px down) and tall 3:4 images. `.wrap` widens to 1360px.
- Buttons are square solid ink blocks (52-56px high) or 1px ink outlines; options are words with a 1px underline when chosen.
- heritage centres the hero, story, collection heading and product text and draws a 40px gold hairline above the story;
  atelier aligns everything to the start edge and drops the hairline.
- Home order: hero, story line (`story`), collection (heading, categories, grid), visit block (`story_image` beside the
  address and WhatsApp link), footer (centred name, address, links).
- Motion: only the slow 1.4s image zoom on tile hover; `prefers-reduced-motion` turns it off.

## 3b. Boutique style: boutique

`font = boutique` keeps `data-style="standard"` (every template's normal layout works) and sets `theme.boutique`. Its look is the
`boutique` template (the same copy in every template, included from `head` only for this font; `TestThemeIsTheSameEverywhere`):
cream paper `#fbf6f1`, deep brown ink `#3b2a22`, the brand colour as the accent (default terracotta `#b4643f`), Gilda Display
headings (Amiri in Arabic) and DM Sans text (IBM Plex Sans Arabic), 4px buttons in small tracked capitals, 16px photos, arched
tiles (`.arch`), ovals (`.oval`), a small tracked label (`.eyebrow`), a short rule (`.rule`) and arrows that turn in Arabic (`.arr`).

A template may add a boutique layout with `{{if .theme.boutique}}`. booking-uae does (`home_boutique`): a split hero with a label
(`business.eyebrow`), an accent line (`tagline_accent`), an intro and a turning stamp; highlights with line icons (table
`highlights`, template `icon`); arched service cards with their category; a brand-colour band (story); a closing call to book with
an oval photo (`interior_image`); a "Book" button in the header and a three-column footer. A rating shows only when the owner sets
a real `rating` and `review_count`; never seed invented reviews.

home-service-uae has its own (`home_boutique`, classes `h-…`), seeded in forest green `#1F3A2E`: a split hero with a big
headline (`headline` + `tagline_accent`, capitals in Latin script only), the label (`eyebrow`) and `intro`; service cards with a
round icon (`services.icon`, template `icon`); highlights next to `story_image` (table `highlights`); three steps in texts
(`step1_title` ... `step3_text`) next to `interior_image`; one customer review from table `testimonials` only when the owner adds
a real one (empty in the seed); the areas and facts; a brand-colour closing band; header links, a "Book a visit" button and a
three-column footer. It has no motion kit, so its boutique motion is only hover transforms.

store-uae has its own (`home_boutique`, classes `e-…`), seeded in forest green `#1F3A2E`: a hero with a label (`store.eyebrow`),
the tagline with an accent line (`tagline_accent`), `intro` and an arched `hero_image` in a gold outline; the categories as
"families" with an arched photo (each category's first product photo), filtered in place by the motion kit; a search field and the
products in arched cards with an optional label (`products.badge`: bestseller, new) and a quick-add button for products without
variants (opens the cart drawer); a promo band only for a coupon the owner marks `promote` (active, not expired, not used up);
the story next to `story_image`, with one customer quote from table `testimonials` only when the owner adds a real one (empty in
the seed); and a three-column footer.

## 4. Type scale

Sizes: 14 / 16 / 18 / 22 / 28 / 36 / 48 px (`--fs-14` ... `--fs-48`). Nothing smaller than 14px except the cart count (13px).

| Role | Size | Weight | Latin line-height / tracking | Arabic |
|---|---|---|---|---|
| Intro band headline (home only) | 36 phone, 48 from 720px | 600 display | 1.15 / -0.015em | 1.45 / 0 |
| Page title h1 | 28 phone, 36 from 720px | 600 display | 1.15 / -0.015em | 1.45 / 0 |
| Section h2, form legends | 22 | 600 display | 1.25 / -0.01em | 1.45 / 0 |
| h3, summary titles | 18 | 600 text | 1.35 | 1.45 |
| Body | 16 | 400 | 1.55 | 1.75 |
| Labels, legends of options, `dt` | 14 | 500 | | |
| Hints, meta, notes | 14 | 400, `--ink-2` | | |
| Prices | 16 (lists), 22 (product page), 18 total rows | 500 / 600 | `tabular-nums lining-nums`, `white-space: nowrap` | same |

Arabic rules: no negative letter-spacing, no `text-transform: uppercase`, more line-height (`body:lang(ar)`, `h1:lang(ar)`).
Western digits everywhere (normal in UAE commerce).

## 5. Money, dates, texts

- Every visible string comes from the `texts` table (en + ar), including category names (`category_ar`), emirates
  (`emirate_dubai` ...), status names (`status_<value>`), month names, emails, aria labels and hints.
- Currency: texts key `money` = `AED {n}` / `{n} د.إ`. `price(n, t)` gives 2 decimals ("AED 250.00", "250.00 د.إ") for prices
  and totals; `amount(n, t)` drops ".00" and uses non-breaking spaces for sentences ("Free delivery over AED 200").
- Dates: texts key `months` = `Jan|Feb|...` / `يناير|فبراير|...`; `longDate('2026-11-19', t)` -> "19 Nov 2026" / "19 نوفمبر 2026".
  Wrap machine dates as `<time datetime="2026-11-19">`.
- Numbers like order ids: a texts key with a placeholder, e.g. `order_no` = `Order #{id}` / `الطلب رقم {id}` (avoids "#2" bidi flips).
- Phone numbers and codes inside Arabic text: `<bdi dir="ltr">`; phone/coupon inputs get `dir="ltr"` (CSS right-aligns them in RTL).
- Language switch: both endonyms always visible, `English` (lang="en") and `العربية` (lang="ar"), current one `aria-current="page"`;
  links built by `langLink(req, lang)` so query parameters survive.

## 6. Layout and components (class names from store-uae; reuse them)

- `.wrap` max 1180px, gutters 16 / 24 / 32px. `main` is not wrapped so full-bleed bands are possible.
- `.bar`: thin tinted top bar with one useful fact (delivery note) at start and `.langs` at end.
- `.site-head`: sticky, white, hairline bottom; `.mark` (logo image max 40px high, else name in display face) and `.acts`
  (account link, cart/booking link with `.count`).
- `.intro`: the single bold moment, a solid `--brand` band with the tagline as h1 in `--on-brand`. Home page only.
- `.cats`: category tabs as text with a 2px `--brand-text` underline for the current one; scrolls sideways inside itself.
- `.tiles` / `.tile` / `.well`: 2 columns on phones, 3 from 720px, 4 from 1080px; images 4:5 in a `--well` background, name 500,
  price, stock note (`.stock`, `.stock.out`). No card borders or shadows.
- Detail page `.pdp`: media full-bleed and 1:1 on phones, 4:5 and 7/5 columns from 900px with the info column sticky.
  Options as radio boxes `.opt-list > label.opt > input[type=radio]`, quantity `.stepper`, primary action in `.buybar`
  (fixed bottom bar with the price on phones, add `has-buybar` to body), `.toast` (role=status) after the action, then `.facts`.
- Forms: `fieldset.group > legend` per section (Contact details, Delivery details, Payment), `.field > label + input`, labels
  above fields, `.hint` under the field (`aria-describedby`), `.pair` for two short fields from 720px, `.choice` radio rows for
  payment methods with a one-line hint. One full-width primary `.btn.wide` at the end.
- Totals: `.rows > .row` (label start, amount end, tabular), `.row.total` with a hairline above, then `.row.sub` "Includes VAT (5%)".
  `.sheet` (tinted, 12px radius) for the summary next to a form; sticky from 960px (`.co`, `.co-side`).
- Lists of records (`.orders`): link rows with id + amount on line one and `.o-meta` (date, `.status`) on line two, hairlines between.
- `.notice` (brand-soft) for success, `.alert` (danger-soft, role=alert) for errors, `.empty` for empty/404 states: a heading that says
  what is missing, one line on what to do, one button.
- Invoices/receipts: own page with `{{template "head" .}}`, a 2px `--brand` rule under the head, table with numeric columns `td.n`,
  totals as `.rows` below the table (so phones never scroll sideways), print button `.noprint`, `@page {margin:16mm}`.
- Footer `.site-foot`: business name and address, WhatsApp link, account links, sign out as `button.linkbtn` in a form.
- Buttons: `.btn` (brand fill, `--on-brand` text, 48px high, no wrap), `.btn.line` (outline), `.linkbtn` (text). Visible focus via
  `:focus-visible` outline in `--brand-text`.

## 7. RTL rules

- `<html lang="{{.lang}}" dir="{{.dir}}">`; never set `dir` on blocks except `dir="ltr"` on phones/codes.
- Only logical properties: `margin-inline-*`, `padding-inline-*`, `inset-inline-*`, `border-inline-start`, `text-align: start/end`.
  No `left`/`right` in layout CSS.
- Flex/grid mirror by themselves; do not reverse them manually. Steppers mirror too (minus on the start side).
- Do not use arrow characters for direction; if one is ever needed, flip it with `:dir(rtl)`.

## 8. Wiring (copy these)

### lib `theme` (copy verbatim)
```js
// Merchant branding from the settings row (brand_color, font, logo), shared by every Renoxium template.
// theme(row) returns {brand, onBrand, brandText, font, style, lux, logo}; the "top" template puts them on <html>
// (data-font, data-style and CSS variables).
// brand: the owner's colour (#rrggbb); when it is empty or not a colour, a quiet slate (or a deep ink for the luxury styles).
// onBrand: white or near-black, whichever reads better on brand (buttons, the intro band).
// brandText: brand darkened until it passes WCAG AA (4.5:1) as text on white, for links and selected states.
// font picks the type pair and the style:
//   modern (IBM Plex Sans + IBM Plex Sans Arabic), classic (Markazi Text headings + IBM Plex), rounded (Rubik): style "standard".
//   heritage (Cormorant Garamond + Jost, Amiri + Noto Naskh Arabic; ivory, antique gold hairlines) and
//   atelier (Bodoni Moda + Hanken Grotesk, Noto Naskh Arabic + Noto Kufi Arabic; white, near monochrome): style "luxury",
//   which switches to the luxury layout (centred name, full-bleed photo, no boxes, square buttons).
const THEME_DEFAULT_BRAND = '#334155';
const THEME_DEFAULT_INK = '#1f1b17';
const THEME_FONTS = ['modern', 'classic', 'rounded', 'heritage', 'atelier'];
const THEME_LUXURY = ['heritage', 'atelier'];
function themeHex(v) {
  let s = String(v || '').trim().toLowerCase();
  if (/^#?[0-9a-f]{3}$/.test(s)) s = s.replace('#', '').split('').map(c => c + c).join('');
  s = s.replace('#', '');
  return /^[0-9a-f]{6}$/.test(s) ? '#' + s : '';
}
function themeRgb(hex) { return [1, 3, 5].map(i => parseInt(hex.slice(i, i + 2), 16)); }
function themeLum(rgb) {
  const c = rgb.map(v => { v /= 255; return v <= 0.03928 ? v / 12.92 : Math.pow((v + 0.055) / 1.055, 2.4); });
  return 0.2126 * c[0] + 0.7152 * c[1] + 0.0722 * c[2];
}
function themeContrast(a, b) { const x = themeLum(a), y = themeLum(b); return (Math.max(x, y) + 0.05) / (Math.min(x, y) + 0.05); }
function themeToHex(rgb) { return '#' + rgb.map(v => (Math.round(v) < 16 ? '0' : '') + Math.round(v).toString(16)).join(''); }
function theme(row) {
  row = row || {};
  const font = THEME_FONTS.indexOf(row.font) >= 0 ? row.font : 'modern';
  const lux = THEME_LUXURY.indexOf(font) >= 0;
  const brand = themeHex(row.brand_color) || (lux ? THEME_DEFAULT_INK : THEME_DEFAULT_BRAND);
  const rgb = themeRgb(brand), white = [255, 255, 255], ink = [26, 23, 22];
  const onBrand = themeContrast(rgb, white) >= themeContrast(rgb, ink) ? '#ffffff' : '#1a1716';
  let text = rgb;
  for (let i = 0; i < 20 && themeContrast(text, white) < 4.5; i++) text = text.map(v => v * 0.9);
  return {brand: brand, onBrand: onBrand, brandText: themeToHex(text), font: font, style: lux ? 'luxury' : 'standard', lux: lux, logo: row.logo || ''};
}
```

### lib `common`: page() must add these keys
`theme: theme(settingsRow)`, `qs` ('?lang=ar' or ''), `amp` ('&lang=ar' or ''), `here` (path + qs, for sign-in `next`),
`langEn`, `langAr` (from `langLink`), and the helpers `price`, `amount`, `longDate`, `pick`. Store-uae's version:
```js
function langOf(req) { return req.query.lang === 'ar' ? 'ar' : 'en'; }
function texts(lang) {
  const t = {};
  for (const r of db.query('SELECT key, en, ar FROM texts')) t[r.key] = (lang === 'ar' ? r.ar : r.en) || r.en;
  return t;
}
function store() { return db.query('SELECT * FROM store ORDER BY id LIMIT 1')[0]; }
function money(n) { return (Math.round((n || 0) * 100) / 100).toFixed(2); }
// An amount with its currency in the page's language, from texts.money: "AED 250.00" or "250.00 د.إ".
function price(n, t) { return (t.money || 'AED {n}').replace('{n}', money(n)); }
// The same for notes and sentences: whole amounts without decimals ("AED 200"), kept on one line.
function amount(n, t) { const v = Math.round((n || 0) * 100) / 100; return (t.money || 'AED {n}').replace('{n}', v % 1 === 0 ? String(v) : v.toFixed(2)).replace(/ /g, '\u00a0'); }
// 'YYYY-MM-DD' as "19 Nov 2026" or "19 نوفمبر 2026" (month names from texts.months, Western digits).
function longDate(ymd, t) {
  const p = String(ymd || '').slice(0, 10).split('-'), m = String(t.months || '').split('|');
  if (p.length !== 3 || m.length !== 12) return String(ymd || '');
  return Number(p[2]) + ' ' + m[Number(p[1]) - 1] + ' ' + p[0];
}
// The Arabic value of a column pair such as name / name_ar, or the English one when there is none.
function pick(row, field, lang) { return lang === 'ar' && row[field + '_ar'] ? row[field + '_ar'] : row[field]; }
// The current address in the other language, keeping the other query parameters (category, error).
function langLink(req, lang) {
  const q = [];
  for (const k in req.query) if (k !== 'lang') q.push(encodeURIComponent(k) + '=' + encodeURIComponent(req.query[k]));
  if (lang === 'ar') q.push('lang=ar');
  return req.path + (q.length ? '?' + q.join('&') : '');
}
function page(req, extra) {
  const lang = langOf(req), s = store(), t = texts(lang);
  const qs = lang === 'ar' ? '?lang=ar' : '';
  const base = {lang: lang, dir: lang === 'ar' ? 'rtl' : 'ltr', other: lang === 'ar' ? 'en' : 'ar', t: t, user: req.user,
    store: s, theme: theme(s), storeName: pick(s, 'name', lang), tagline: pick(s, 'tagline', lang), story: pick(s, 'story', lang) || '', address: pick(s, 'address', lang) || '',
    path: req.path, qs: qs, amp: lang === 'ar' ? '&lang=ar' : '', here: req.path + qs,
    langEn: langLink(req, 'en'), langAr: langLink(req, 'ar'), wa: (s.whatsapp || '').replace(/[^0-9]/g, ''),
    freeOver: s.free_delivery_over > 0,
    shipNote: s.free_delivery_over > 0 ? t.free_delivery_over + ' ' + amount(s.free_delivery_over, t) : t.delivery + ' ' + amount(s.delivery_fee, t)};
  for (const k in extra) base[k] = extra[k];
  return base;
}
```

### template `head` (copy verbatim; it pulls in `css`)
```html
<meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><meta name="theme-color" content="{{.theme.brand}}">
<link rel="preconnect" href="https://fonts.googleapis.com"><link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
{{if eq .theme.font "classic"}}<link rel="stylesheet" href="https://fonts.googleapis.com/css2?family=Markazi+Text:wght@500;600&family=IBM+Plex+Sans+Arabic:wght@400;500;600&family=IBM+Plex+Sans:wght@400;500;600&display=swap">
{{else if eq .theme.font "heritage"}}<link rel="stylesheet" href="https://fonts.googleapis.com/css2?family=Cormorant+Garamond:ital,wght@0,300;0,400;0,500;1,300;1,400&family=Jost:wght@300;400;500&family=Amiri:wght@400;700&family=Noto+Naskh+Arabic:wght@400;500&display=swap">
{{else if eq .theme.font "atelier"}}<link rel="stylesheet" href="https://fonts.googleapis.com/css2?family=Bodoni+Moda:ital,opsz,wght@0,6..96,400..600;1,6..96,400&family=Hanken+Grotesk:wght@300;400;500&family=Noto+Naskh+Arabic:wght@400;500&family=Noto+Kufi+Arabic:wght@300;400;500&display=swap">
{{else if eq .theme.font "rounded"}}<link rel="stylesheet" href="https://fonts.googleapis.com/css2?family=Rubik:wght@400;500;600&display=swap">
{{else}}<link rel="stylesheet" href="https://fonts.googleapis.com/css2?family=IBM+Plex+Sans+Arabic:wght@400;500;600&family=IBM+Plex+Sans:wght@400;500;600&display=swap">{{end}}
{{template "css" .}}
```

### `<html>` line of `top` and of standalone pages (invoice, receipt)
```html
<!doctype html><html lang="{{.lang}}" dir="{{.dir}}" data-font="{{.theme.font}}" data-style="{{.theme.style}}" style="--brand:{{.theme.brand}};--on-brand:{{.theme.onBrand}};--brand-text:{{.theme.brandText}}"><head>
<title>{{with .title}}{{.}} | {{end}}{{.storeName}}</title>{{template "head" .}}
```
Then `.skip` link, `.bar`, `.site-head`, `<main id="main">` as in store-uae `top`. Each page wraps its content in `<div class="wrap">`.

### template `css`: the shared block (copy verbatim, then append the template's own components below a `/* <Template> components */` comment)
```html
<style>
[hidden]{display:none!important}
/* Renoxium template design system: shared base. The theme (brand colour, font, logo) comes from theme() in lib and sits on <html>. */
:root{color-scheme:light;--brand:#334155;--on-brand:#fff;--brand-text:#334155;
--paper:#fff;--ink:color-mix(in oklab,var(--brand) 14%,#121212);--ink-2:color-mix(in oklab,var(--ink) 74%,#fff);
--line:color-mix(in oklab,var(--ink) 12%,#fff);--line-strong:color-mix(in oklab,var(--ink) 28%,#fff);
--well:color-mix(in oklab,var(--brand) 5%,#f4f3f1);--brand-soft:color-mix(in oklab,var(--brand) 8%,#fff);
--brand-hover:color-mix(in oklab,var(--brand) 84%,#000);--danger:#a3261d;--danger-soft:#fbeeec;
--r-control:8px;--r-media:4px;--r-sheet:12px;
--fs-14:.875rem;--fs-16:1rem;--fs-18:1.125rem;--fs-22:1.375rem;--fs-28:1.75rem;--fs-36:2.25rem;--fs-48:3rem;
--font-text:"IBM Plex Sans","IBM Plex Sans Arabic",system-ui,-apple-system,"Segoe UI",Tahoma,sans-serif;--font-display:var(--font-text);--display-boost:1}
[data-font=classic]{--font-display:"Markazi Text","Noto Naskh Arabic",Georgia,serif;--display-boost:1.16}
[data-font=rounded]{--font-text:"Rubik",system-ui,-apple-system,"Segoe UI",Tahoma,sans-serif}
/* Luxury type pairs (font = heritage or atelier; theme() then sets data-style=luxury). Arabic falls through to the Arabic face. */
[data-font=heritage]{--paper:#fbf8f2;--well:#f0eadf;--accent:#a08048;--font-display:"Cormorant Garamond","Amiri",Georgia,serif;--font-text:"Jost","Noto Naskh Arabic",system-ui,sans-serif}
[data-font=atelier]{--paper:#fff;--well:#f3f2ef;--accent:color-mix(in oklab,var(--ink) 40%,#fff);--font-display:"Bodoni Moda","Noto Naskh Arabic",Didot,Georgia,serif;--font-text:"Hanken Grotesk","Noto Kufi Arabic",system-ui,sans-serif}
*,*::before,*::after{box-sizing:border-box}
html{-webkit-text-size-adjust:100%}
body{margin:0;background:var(--paper);color:var(--ink);font-family:var(--font-text);font-size:var(--fs-16);line-height:1.55;-webkit-font-smoothing:antialiased;overflow-wrap:break-word}
body:lang(ar){line-height:1.75}
img{max-width:100%;display:block}
h1,h2,h3{margin:0;font-weight:600;color:var(--ink);text-wrap:balance}
h1,h2{font-family:var(--font-display)}
h1{font-size:calc(var(--fs-28) * var(--display-boost));line-height:1.15;letter-spacing:-.015em}
h2{font-size:calc(var(--fs-22) * var(--display-boost));line-height:1.25;letter-spacing:-.01em}
h3{font-size:var(--fs-18);line-height:1.35}
h1:lang(ar),h2:lang(ar),h3:lang(ar){letter-spacing:0;line-height:1.45}
@media(min-width:720px){h1{font-size:calc(var(--fs-36) * var(--display-boost))}}
p{margin:0}
a{color:inherit;text-decoration-thickness:1px;text-underline-offset:.22em}
a:hover{color:var(--brand-text)}
:focus-visible{outline:2px solid var(--brand-text);outline-offset:2px}
.num,.price{font-variant-numeric:tabular-nums lining-nums}
.price{white-space:nowrap}
.sr{position:absolute;width:1px;height:1px;overflow:hidden;clip:rect(0 0 0 0);white-space:nowrap}
.small{font-size:var(--fs-14)}.quiet{color:var(--ink-2)}
.wrap{--gutter:16px;max-width:1180px;margin-inline:auto;padding-inline:var(--gutter)}
@media(min-width:720px){.wrap{--gutter:24px}}
@media(min-width:1080px){.wrap{--gutter:32px}}
.skip{position:absolute;inset-inline-start:-9999px;top:8px}.skip:focus{inset-inline-start:16px;z-index:50;background:var(--paper);padding:8px 14px;border-radius:var(--r-control)}

/* Top bar, header, footer */
.bar{background:var(--well);font-size:var(--fs-14);color:var(--ink-2)}
.bar .wrap{display:flex;align-items:center;justify-content:space-between;gap:8px 16px;min-height:36px;flex-wrap:wrap}
.langs{display:flex;align-items:center}
.langs a{text-decoration:none;padding:6px 0;color:var(--ink-2)}
.langs a+a{margin-inline-start:12px;padding-inline-start:12px;border-inline-start:1px solid var(--line-strong)}
.langs a[aria-current]{color:var(--ink);font-weight:500}
.site-head{position:sticky;top:0;z-index:10;background:var(--paper);border-bottom:1px solid var(--line)}
.site-head .wrap{display:flex;align-items:center;justify-content:space-between;gap:16px;min-height:64px}
.mark{font-family:var(--font-display);font-weight:600;font-size:calc(var(--fs-22) * var(--display-boost));line-height:1.2;text-decoration:none;color:var(--ink)}
.mark img{max-height:40px;width:auto}
.acts{display:flex;align-items:center;gap:20px;font-size:var(--fs-16)}
.acts a{text-decoration:none;display:inline-flex;align-items:center;gap:6px;min-height:44px}
.acts a:hover{text-decoration:underline}
.count{display:inline-grid;place-items:center;min-width:22px;height:22px;padding-inline:6px;border-radius:11px;background:var(--brand);color:var(--on-brand);font-size:.8125rem;font-weight:500;line-height:1}
.count[hidden]{display:none}
.site-foot{margin-top:72px;border-top:1px solid var(--line);font-size:var(--fs-14);color:var(--ink-2)}
.site-foot .wrap{display:flex;flex-wrap:wrap;justify-content:space-between;gap:16px 40px;padding-block:32px 40px}
.site-foot strong{display:block;color:var(--ink);font-weight:500;font-size:var(--fs-16)}
.foot-links{display:flex;flex-wrap:wrap;align-items:center;gap:8px 24px}
.foot-links form{margin:0}

/* Page heads, buttons, notices */
.page-head{padding-block:32px 24px}
.page-head p{margin-top:8px;color:var(--ink-2)}
.btn{display:inline-flex;align-items:center;justify-content:center;gap:8px;min-height:48px;padding:12px 24px;border-radius:var(--r-control);border:1px solid transparent;font:inherit;font-weight:500;line-height:1.2;text-decoration:none;white-space:nowrap;cursor:pointer;background:var(--brand);color:var(--on-brand)}
.btn:hover{background:var(--brand-hover);color:var(--on-brand)}
.btn.line{background:transparent;color:var(--ink);border-color:var(--line-strong)}
.btn.line:hover{border-color:var(--ink);background:transparent;color:var(--ink)}
.btn.wide{width:100%}
.linkbtn{background:none;border:0;padding:0;font:inherit;color:inherit;text-decoration:underline;text-decoration-thickness:1px;text-underline-offset:.22em;cursor:pointer}
.linkbtn:hover{color:var(--brand-text)}
.alert{background:var(--danger-soft);color:var(--danger);padding:12px 16px;border-radius:var(--r-control);margin-bottom:24px}
.notice{background:var(--brand-soft);padding:20px;border-radius:var(--r-sheet);margin-bottom:8px}
.notice p+p{margin-top:4px;color:var(--ink-2)}
.empty{padding-block:48px 24px;max-width:34em}
.empty p{margin:8px 0 24px;color:var(--ink-2)}

/* Forms: labels above fields, grouped in sections */
.group{border:0;padding:0;margin:0 0 32px;min-width:0}
.group>legend{font-family:var(--font-display);font-weight:600;font-size:calc(var(--fs-22) * var(--display-boost));line-height:1.3;padding:0;margin-bottom:16px}
.field{display:grid;gap:6px;margin-bottom:16px}
.field>label,.lab{font-size:var(--fs-14);font-weight:500}
.hint{font-size:var(--fs-14);color:var(--ink-2)}
input,select,textarea{font:inherit;color:var(--ink);background:var(--paper);border:1px solid var(--line-strong);border-radius:var(--r-control);min-height:48px;padding:11px 14px;width:100%}
textarea{min-height:88px;resize:vertical}
[dir=rtl] input[dir=ltr]{text-align:right}
input:focus,select:focus,textarea:focus{border-color:var(--brand-text);outline:2px solid color-mix(in oklab,var(--brand-text) 35%,transparent);outline-offset:0}
input[type=radio],input[type=checkbox]{width:20px;height:20px;min-height:0;margin:0;accent-color:var(--brand-text);flex:none}
@media(min-width:720px){.pair{display:grid;grid-template-columns:1fr 1fr;gap:0 16px}}
.choice{display:flex;align-items:flex-start;gap:12px;padding:14px 16px;border:1px solid var(--line-strong);border-radius:var(--r-control);margin-bottom:8px;cursor:pointer}
.choice input{margin-top:3px}
.choice:has(input:checked){border-color:var(--brand-text);background:var(--brand-soft)}
.choice .hint{display:block}

/* Totals and key-value lists */
.rows{display:grid}
.row{display:flex;justify-content:space-between;align-items:baseline;gap:16px;padding-block:6px}
.row>:last-child{font-variant-numeric:tabular-nums lining-nums;text-align:end;white-space:nowrap}
.row.total{border-top:1px solid var(--line-strong);margin-top:8px;padding-top:14px;font-weight:600;font-size:var(--fs-18)}
.row.sub{font-size:var(--fs-14);color:var(--ink-2);padding-top:0}
.facts{margin:0;border-top:1px solid var(--line)}
.facts>div{display:grid;grid-template-columns:minmax(6.5em,30%) 1fr;gap:4px 16px;padding-block:12px;border-bottom:1px solid var(--line)}
.facts dt{font-weight:500}.facts dd{margin:0;color:var(--ink-2)}
.facts dd span{display:block}
.sheet{background:var(--well);border-radius:var(--r-sheet);padding:20px}
.sheet h2{font-size:var(--fs-18);font-family:var(--font-text);letter-spacing:0;margin-bottom:12px}

@media (prefers-reduced-motion:reduce){*,*::before,*::after{transition:none!important;animation:none!important;scroll-behavior:auto!important}}
@media print{.bar,.site-head,.site-foot,.noprint,.buybar,.skip{display:none!important}body{font-size:11pt}a{text-decoration:none}.sheet{background:none;padding:0}}

/* Luxury style (data-style=luxury): paper instead of white, no radii, hairlines only, light display type, small tracked labels in Latin. */
[data-style=luxury]{--ink-2:color-mix(in oklab,var(--ink) 68%,var(--paper));--line:color-mix(in oklab,var(--ink) 11%,var(--paper));--line-strong:color-mix(in oklab,var(--ink) 30%,var(--paper));
--brand-soft:var(--well);--r-control:0;--r-media:0;--r-sheet:0;--label:.75rem;--label-ar:.9375rem}
[data-style=luxury] body{line-height:1.65}
[data-style=luxury] main>.wrap{padding-bottom:120px}
[data-style=luxury] :is(h1,h2,.tname,.line-name,.o-id,.mark){font-variant-numeric:lining-nums}
[data-style=luxury] .wrap{max-width:1360px}
[data-style=luxury] body:lang(ar){font-size:1.0625rem;line-height:1.9}
[data-style=luxury] h1,[data-style=luxury] h2,[data-style=luxury] .group>legend{font-weight:300;letter-spacing:0}
[data-style=luxury] h1{font-size:2.25rem;line-height:1.1}
[data-style=luxury] h2{font-size:1.75rem;line-height:1.2}
@media(min-width:720px){[data-style=luxury] h1{font-size:2.875rem}}
[data-style=luxury] h1:lang(ar){font-size:2rem;line-height:1.6}[data-style=luxury] h2:lang(ar){font-size:1.5rem;line-height:1.6}
@media(min-width:720px){[data-style=luxury] h1:lang(ar){font-size:2.375rem}}
[data-font=heritage] h1:lang(ar),[data-font=heritage] h2:lang(ar),[data-font=heritage] .group>legend:lang(ar){font-weight:400}
[data-font=atelier] h1,[data-font=atelier] h2,[data-font=atelier] .group>legend{font-weight:400}
[data-font=atelier] h1{letter-spacing:-.01em}
/* Labels: navigation, buttons, legends. Latin small and tracked in capitals; Arabic never. */
[data-style=luxury] :is(.bar,.langs,.acts,.btn,.cta,.cats,.lab,.field>label,.opts legend,.facts dt,.foot-links,.meta dt){font-size:var(--label);letter-spacing:.16em;text-transform:uppercase;font-weight:400}
[data-style=luxury] :is(.btn,.cta,.lab,.field>label,.opts legend,.facts dt){font-weight:500}
[data-style=luxury]:lang(ar) :is(.bar,.langs,.acts,.btn,.cta,.cats,.lab,.field>label,.opts legend,.facts dt,.foot-links,.meta dt){font-size:var(--label-ar);letter-spacing:0;text-transform:none}
[data-style=luxury] .langs a:lang(ar){letter-spacing:0;font-size:var(--label-ar);text-transform:none}
[data-style=luxury] .langs a:lang(en){letter-spacing:.16em;font-size:var(--label);text-transform:uppercase}
[data-style=luxury] .bar{background:var(--paper);color:var(--ink-2)}
[data-style=luxury] .bar .wrap{justify-content:center;min-height:36px;padding-top:6px;text-align:center}
[data-style=luxury] .site-head{background:var(--paper);border-bottom:1px solid var(--line)}
[data-style=luxury] .site-head .wrap{display:grid;grid-template-columns:1fr auto 1fr;gap:12px;min-height:64px}
@media(min-width:720px){[data-style=luxury] .site-head .wrap{min-height:84px}}
[data-style=luxury] .site-head .langs{justify-self:start}
[data-style=luxury] .site-head .acts{justify-self:end;gap:28px}
[data-style=luxury] .langs a+a{border:0;padding-inline-start:0;margin-inline-start:20px}
[data-style=luxury] .langs a[aria-current]{color:var(--ink);font-weight:400;text-decoration:underline;text-decoration-color:var(--accent);text-underline-offset:.5em}
@media(max-width:719px){[data-style=luxury] .langs a[aria-current]{display:none}[data-style=luxury] .langs a+a{margin:0}[data-style=luxury] .acts .acct{display:none}}
[data-style=luxury] .mark{font-weight:400;font-size:1rem;letter-spacing:.17em;text-transform:uppercase;text-align:center;line-height:1.1;white-space:nowrap}
@media(min-width:720px){[data-style=luxury] .mark{font-size:1.625rem;letter-spacing:.24em}}
[data-style=luxury] .mark:lang(ar){letter-spacing:0;font-size:1.625rem;line-height:1.4}
@media(min-width:720px){[data-style=luxury] .mark:lang(ar){font-size:2.125rem}}
[data-font=heritage] .mark:lang(ar){font-family:"Amiri",serif}
[data-font=atelier] .mark{font-family:var(--font-text);font-weight:500;font-size:.875rem;letter-spacing:.4em}
@media(min-width:720px){[data-font=atelier] .mark{font-size:1rem}}
[data-font=atelier] .mark:lang(ar){font-size:1.25rem;letter-spacing:0;font-weight:500}
[data-style=luxury] .acts a{gap:0}
[data-style=luxury] .count{display:inline;min-width:0;height:auto;padding:0;border-radius:0;background:none;color:inherit;font-size:inherit;font-weight:inherit;letter-spacing:inherit}
[data-style=luxury] .count{margin-inline-start:.45em}[data-style=luxury] .count::before{content:"("}[data-style=luxury] .count::after{content:")"}
[data-style=luxury] .count[hidden]{display:none}
[data-style=luxury] .site-foot{margin-top:0;border-top:1px solid var(--line);background:var(--paper);text-align:center}
[data-style=luxury] .site-foot .wrap{flex-direction:column;align-items:center;gap:20px;padding-block:72px 64px}
[data-style=luxury] .site-foot strong{font-family:var(--font-display);font-weight:400;font-size:1.375rem;letter-spacing:.2em;text-transform:uppercase}
[data-style=luxury] .site-foot strong:lang(ar){letter-spacing:0;font-size:1.5rem}
[data-font=atelier] .site-foot strong{font-family:var(--font-text);font-size:.875rem;font-weight:500;letter-spacing:.4em}
[data-style=luxury] .foot-links{justify-content:center;gap:12px 32px}
[data-style=luxury] .foot-links a,[data-style=luxury] .foot-links .linkbtn{text-decoration:none}
[data-style=luxury] .foot-links .linkbtn{letter-spacing:inherit;text-transform:inherit}
[data-style=luxury] .page-head{padding-block:48px 32px}
@media(min-width:720px){[data-style=luxury] .page-head{padding-block:72px 40px}}
[data-style=luxury] .btn{min-height:52px;padding:14px 32px;background:var(--brand);border-color:var(--brand)}
[data-style=luxury] .btn:hover{background:var(--brand-hover);border-color:var(--brand-hover)}
[data-style=luxury] .btn.line{background:transparent;color:var(--ink);border-color:var(--ink)}
[data-style=luxury] .btn.line:hover{background:var(--ink);color:var(--paper)}
[data-style=luxury] .cta{display:inline-block;padding-block:10px 6px;color:inherit;text-decoration:none;border-bottom:1px solid var(--accent)}
[data-style=luxury] .cta:hover{color:inherit;border-bottom-color:currentColor}
[data-style=luxury] .notice{background:var(--well)}
[data-style=luxury] .empty{padding-block:72px 120px}
[data-style=luxury] .group>legend{font-size:1.625rem;margin-bottom:20px}
[data-style=luxury] .group>legend:lang(ar){font-size:1.375rem}
[data-style=luxury] input,[data-style=luxury] select,[data-style=luxury] textarea{background:transparent;border-color:var(--line-strong)}
[data-style=luxury] input:focus,[data-style=luxury] select:focus,[data-style=luxury] textarea:focus{border-color:var(--ink);outline:1px solid var(--ink)}
[data-style=luxury] input[type=radio],[data-style=luxury] input[type=checkbox]{accent-color:var(--ink)}
[data-style=luxury] .choice{border-color:var(--line)}
[data-style=luxury] .choice:has(input:checked){border-color:var(--ink);background:transparent}
[data-style=luxury] .sheet{background:transparent;border:1px solid var(--line);padding:28px 24px}
[data-style=luxury] .sheet h2{font-family:var(--font-display);font-weight:300;font-size:1.5rem;margin-bottom:16px}
[data-font=atelier] .sheet h2{font-weight:400}
[data-style=luxury] .row.total{border-top-color:var(--line);font-weight:500;font-size:var(--fs-16)}
[data-style=luxury] .facts dt{color:var(--ink)}
:root[data-style=luxury] :focus-visible{outline:1px solid var(--ink);outline-offset:3px}
</style>
```
Store-uae's own components (intro, cats, tiles, pdp, options, stepper, buybar, cart lines, orders, and the luxury hero, story,
collection and visit blocks) follow in its `css` template;
reuse them for similar pieces (service grids = tiles, booking detail = pdp, booking/quote lists = orders) rather than inventing new ones.

## 8a. Motion kit (store-uae first; copy it to the other templates)

store-uae and booking-uae carry the kit as two named templates, so `patch_app` can replace either one alone. **Every template
carries the same copy** (`TestMotionKitIsTheSameEverywhere`): change the kit in all templates at once, and put a template's own
motion in its own `css`. Parts that need a component only switch on where it exists (the cart drawer needs `rxCart` in `top`,
the gallery needs `.gal`, in-place search needs `#results`).

- `motion` (CSS, included from `head` after `css`) and `motion_js` (one inline script, included from `bottom` before `</body>`).
- `top` adds the `js` class on `<html>` before anything paints and fires a `rxcart` event from `rxSave`; motion styles that hide
  something until it animates are scoped to `.js`, so pages work without JavaScript.

Rules:

- **Only `transform` and `opacity` animate.** Nothing reflows, so CLS stays 0. One easing (`--ease-out: cubic-bezier(.2,.7,.2,1)`)
  and three durations (`--t-fast` .18s, `--t-mid` .35s, `--t-slow` .7s).
- **The first screen never waits.** Reveal-on-scroll (`.rv` → `.rv.in`, IntersectionObserver, staggered by 70 ms) only applies to
  blocks below the fold at load, and only `loading="lazy"` images fade in over a shimmer (`.well.sk`). The hero and the first
  product photo paint at once, so LCP is unchanged.
- **Reduced motion turns it all off**: the kit adds no `.rv`, the CSS resets animations, and the view transition is disabled.
- **RTL**: the drawer uses `inset-inline-end` and slides from the end side; directions never assume left-to-right.
- **Progressive enhancement**: every part checks for the browser feature it needs (View Transitions, IntersectionObserver,
  `<dialog>`, DOMParser) and leaves a working page without it.

Parts:

| Part | How |
| --- | --- |
| List → product transition | `@view-transition {navigation: auto}`; the tile photo and the product's first photo share `view-transition-name: pimg-<id>` |
| Luxury hero | Ken Burns zoom in CSS (`rx-kb`), parallax with the `translate` property (it does not fight the animation's `transform`) |
| Prefetch | `<link rel=prefetch>` on `pointerover`/`touchstart` of same-origin links (not `/_rx/`, not `/checkout`) |
| Cart drawer | `rxDrawer.open()`; totals from `/api/products` with `rxDrawer.sum()`, the same delivery rule as `deliveryFor` in lib `common` (tested in `store_delivery_test.go`); Checkout goes to `/cart` |
| Filters and search | `#results` is swapped with the server-rendered results of the new URL (no client templates), `history.pushState`/`replaceState`, `popstate` reloads the results |
| Gallery and zoom | `products.image2`/`image3` (optional); scroll-snap carousel with dots, a `<dialog>` for full screen, tap to zoom |
| PWA | `/manifest.webmanifest` from the store's name, brand colour and logo |
| Choices and time slots | press feedback on `.opt`, `.day` and `.slot`; time slots pop in (`rx-pop`) as they load |
| Print | everything waiting for a reveal is shown, the drawer is hidden |

Budget (checked for the store and booking demos by `e2e/tests/11-performance.spec.mjs` on an emulated Pixel 7, fast 4G, CPU ×4, third-party requests blocked):
LCP ≤ 2.5 s, CLS ≤ 0.1, inline JS ≤ 30 KB, non-image bytes ≤ 150 KB per page.

## 9. Checklist per template

1. Settings columns above added with labels/help, seeded with a fitting colour + font; logo seed empty.
2. `theme`, `common` helpers in lib; scripts call `page(req, ...)` and format money/dates with `price`/`amount`/`longDate`.
3. Every string in texts (en + ar), including categories, statuses, emirates, hints, aria labels, emails.
4. Tests in internal/platform/*_example_test.go still pass; update assertions only where markup really changed.
5. Screenshots at 390 and 1440, en and ar, plus one other brand_color/font (and heritage or atelier when the template offers
   the luxury layout): no horizontal scroll, AA contrast, focus visible.
6. test_request shortens inline `<style>` blocks over 1000 bytes to a note of their size, so a long stylesheet no longer hides
   the page content from the agent (bodies are still cut at 20000 characters).
