# store-uae premium: motion and app feel

**Goal:** make the store-uae example (and so its gallery demo and every new shop) look and feel like a modern premium storefront, with tasteful motion and app-like interactions, without making it slower, and turn the result into a motion kit the other examples can copy.

**Done when:**
- Every store-uae theme (modern, classic, rounded, heritage, atelier) has scroll reveals, smooth list→product transitions, a swipeable/zoomable product gallery, a slide-in cart drawer, a sticky buy bar on phones and skeleton loading; heritage and atelier add a parallax/Ken Burns hero and bigger transitions.
- Filters, search and add-to-cart work without a page reload; links are prefetched; the shop is installable (PWA manifest + icons).
- `prefers-reduced-motion` turns motion off, and every page works and looks right in Arabic (RTL).
- A CI test fails when the demo store on an emulated phone exceeds the performance budget.

## Decisions
- Where the motion kit lives → **inside the template** (lib/templates of store-uae, copied into each app); no new platform API; existing apps don't change automatically.
- Which themes → **all store-uae themes, one kit**; luxury themes (heritage, atelier) get extra hero effects.
- Video and 3D (Tier 3) → **not in v1**; photos only. Video hero and GLB/model-viewer are v2 after v1 is measured.
- Performance guard → **a failing CI test** (Playwright in e2e, emulated phone) with fixed limits.

## Assumptions (not asked, simplest option taken — change here if wrong)
- No external animation library in v1: CSS animations/transitions, `IntersectionObserver`, the View Transitions API (progressive: browsers without it just navigate) and small vanilla JS. GSAP/Three.js stay for v2 if needed.
- Budget (demo store home and product page, Playwright mobile emulation with 4G throttling): LCP ≤ 2.5 s, CLS ≤ 0.1, own inline JS ≤ 30 KB, page weight without images ≤ 150 KB. Tune once after the first measurement, then fix.
- The motion kit is one `lib`-free set: a `motion` template (CSS) and a `motion_js` template (JS) included from `head`/`bottom`; nothing new on the server side except what Tier 2 needs.
- No-reload filters/search reuse `/api/products` (extend it with category/query parameters) instead of a new endpoint.
- Cart drawer reuses the existing `rxCart()` / `rxSave()` localStorage cart and the `/cart` checkout form; checkout stays a real page (no payment flow change).
- PWA = `manifest.webmanifest` route + icons from the store logo; no service worker / offline mode in v1.
- The existing price consistency tests (`consistency_test.go`, `store_delivery_test.go`) must keep passing unchanged: the drawer shows the same totals as the cart page.
- Owners' existing live stores are untouched; they can ask their assistant to adopt the new version (patch_app).

## Scope
### In (v1)
- Motion kit (CSS + JS) in store-uae, used by every page.
- Tier 1: scroll reveals, hero parallax/Ken Burns (luxury), View Transitions list↔product, swipe + pinch/zoom gallery, slide-in cart drawer, sticky buy bar (phones), skeletons, hover/press micro-interactions.
- Tier 2: no-reload category filter + instant search on home, add-to-cart without leaving the product page, link prefetch on hover/touch, PWA manifest.
- reduced-motion + RTL for all of the above.
- Performance budget test in CI; screenshot review (desktop, phone, Arabic) before merging.
- Short "motion kit" section in `docs/design/templates.md` so other examples can copy it.

### Out (later or never)
- Video hero, video upload, GLB/3D, AR, Three.js, Lottie (v2).
- Service worker / offline.
- Applying the kit to the other five examples (separate plans, one per example, after v1 is measured).
- A platform-served motion library.

## Approach
1. **Measure first**: add `e2e/tests/11-performance.spec.mjs` that loads the demo store (home, `/p/1`) on an emulated phone and records LCP, CLS, JS and page weight; commit with report-only output to get the baseline.
2. **Motion kit**: `motion` (CSS: reveal, fade/slide keyframes, durations as CSS variables, `prefers-reduced-motion` reset, RTL-aware directions) and `motion_js` (IntersectionObserver reveals, prefetch on hover/touchstart, View Transition names on product images) in `examples/store-uae.json`; include them from `head`/`bottom`.
3. **Product page**: swipe/zoom gallery, add-to-cart without navigation (updates `rxCart` + a badge), sticky buy bar on phones, view-transition from the home card image.
4. **Cart drawer**: slide-in drawer on every page using the cart page's own price logic (shared `lib` already holds `deliveryFor`); "Checkout" goes to `/cart`.
5. **Home**: hero parallax/Ken Burns for heritage/atelier, staggered card reveals, no-reload category filter + instant search via `/api/products` (add `c` and `q` parameters), skeleton cards while loading.
6. **PWA**: `/manifest.webmanifest` route and icons from `store.logo` (fallback to the brand colour), `<link rel="manifest">` in `head`.
7. **Budget on**: switch the performance test to failing limits (see Assumptions), and add a check that pages render with `prefers-reduced-motion: reduce` and `dir="rtl"`.
8. **Docs + review**: motion-kit section in `docs/design/templates.md`; screenshots desktop/phone/Arabic in the PR.

## Risks and how we handle them
- Motion makes pages slower on phones → budget test fails the PR; animate only `transform`/`opacity`; lazy-load below-the-fold images.
- Drawer totals drift from the cart/checkout → reuse `deliveryFor` and the existing consistency tests; add one drawer case.
- View Transitions unsupported (older Safari/Firefox) → progressive enhancement; plain navigation still works.
- RTL animations slide the wrong way → directions use logical properties / `dir`-aware variables; Arabic screenshots in review.
- The JSON grows and gets harder for assistants to patch → keep the kit in its own named templates so `patch_app` can replace them one by one.

## Verification
- Automated: Go tests (`TestStoreUAEExample`, consistency and delivery tests) unchanged and passing; new e2e performance test with failing limits; e2e checks for reduced-motion and RTL rendering; existing gallery e2e still passes.
- Manual: open the demo store on a phone and desktop in English and Arabic — browse, filter, open a product (transition), swipe the gallery, add to cart (drawer), check out with cash on delivery.

## Open questions deferred
- Exact easing curves and durations (implementer picks one consistent set and records it in the docs section).
- Whether to self-host a small library for the gallery gestures if vanilla JS proves too fragile (decide during step 3; keep within the JS budget).
- Order of rolling the kit out to the other examples (after v1 numbers are in).
