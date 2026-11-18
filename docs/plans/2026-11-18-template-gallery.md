# Template gallery with live demos

**Goal:** prospective customers browse every Renoxium template at `renoxium.com/templates` and try each one live (customer site
and a read-only owner admin panel), so they see what they would get before signing up. Adding a template to `examples/` is all
it takes for it to appear in the gallery.

**Done when:**
- `https://renoxium.com/templates` lists every gallery template with a short summary, a "Try the demo" link, a "See the admin
  panel" link and a "Use this template" link, and needs no sign-in.
- Each demo runs at `https://demo-<example>.renoxium.app`: visitors can browse, book, check out (simulated card payment), upload
  and sign in; nothing is ever really sent or charged; data returns to the template's seed rows every hour.
- Sign-in codes, confirmation links and emails the demo would send appear in an on-screen demo inbox, only for the browser
  whose request caused them.
- A new example added to `examples/examples.go` with a gallery summary shows up in the gallery and gets its demo after the next
  deploy, with no manual step; a test fails if an example is added without the gallery fields.

## Decisions
- How far can visitors go in a demo? → Full use, one shared database per demo, reset every hour (simplest; vandalism lasts at most an hour).
- Visitor sign-in in a demo? → Demo inbox: emails and WhatsApp codes are shown on screen, never sent.
- Owner admin panel in the gallery? → Yes, read-only: lists, calendar and row details, no add/edit/delete.
- Adding templates must need no per-template work → demos are provisioned from `examples.All()` at startup and re-applied when a definition changes.

## Assumptions (not asked, simplest option taken — change here if wrong)
- Demos are apps owned by a system account (`usr_demo`, created at startup) that is exempt from app, storage and plan limits
  and never appears in any user's dashboard or `list_apps`.
- Demos use **preview-stage behaviour** (simulated payments, no real email/WhatsApp, `http.fetch` off, scheduled jobs not run)
  but without the preview lock. Implementation: map the host `demo-<example>.<apps domain>` to the demo app's preview stage
  and skip `guardPreview` for demo apps. They are never published to live.
- App names starting with `demo-` are reserved for users (added next to the existing reserved names in `internal/platform/platform.go`).
- Gallery visibility is opt-in per example: a new `Gallery` field on `examples.Example` (`Title`, `Summary`, `Category`); examples
  without it (`tasks`, `catalog`) are not shown. `delivery-tracking` and every `*-uae` example get one.
- "Use this template" links to `/connect` with a ready-to-paste prompt ("Build me a <title> using the <example> template").
  Creating an app directly from the gallery is out of scope.
- Every demo page shows a slim platform banner: "Demo — data is public and deleted every hour. Don't enter real personal
  details." with links back to the gallery and to the demo inbox.
- Demo inbox messages are kept in memory per demo for the hour (lost on restart; acceptable), tied to the browser by a demo
  cookie and the request that produced them. Messages produced outside a visitor request (admin hook, jobs) are dropped.
- The admin read-only view lives on the site at `/templates/<example>/admin...`, reusing the existing admin list and calendar
  rendering with a read-only flag; the visitors page and private files are not shown.

## Scope
### In (v1)
- System account and automatic provisioning of one demo app per gallery example.
- Public demo hosts with preview behaviour, demo banner and hourly reset.
- Demo inbox (email and WhatsApp code) per browser.
- Gallery page `/templates` and read-only admin `/templates/<example>/admin`.
- `demo-` prefix reserved; metadata test for new examples.

### Out (later or never)
- Per-visitor private copies of demo data.
- Automatic screenshots or thumbnails on gallery cards.
- One-click "create my app from this template" from the gallery.
- Editable admin in demos; Arabic version of the gallery page itself (demos already switch language).

## Approach
1. **Example metadata.** Add `Gallery *GalleryInfo` to `examples.Example` (`examples/examples.go`), fill it for the six UAE
   examples and `delivery-tracking`, add `examples.Gallery()` returning them in a stable order, and a test that every example
   whose name ends in `-uae` has gallery info.
2. **System account and provisioning.** In `internal/platform`, add `EnsureDemos(ctx)` called from `cmd/renoxium/main.go` at
   startup: create `usr_demo` if missing, create `demo-<example>` for each gallery example, and `ApplyChange` when the stored
   definition differs from the embedded one. Exempt `usr_demo` from app/storage/plan limits. Reserve the `demo-` prefix for
   everyone else.
3. **Public demo hosts.** Register `demo-<example>.<apps domain>` as a host of the preview stage (domains table or `Resolve`
   rule), skip `guardPreview` for demo apps in `internal/platform/serve.go`, and inject the demo banner into HTML responses of
   demo apps.
4. **Hourly reset.** Rebuild each demo's preview data from seed every hour (reuse the preview rebuild that `ApplyChange` does),
   removing preview uploads, sessions and visitor accounts; run it from the existing scheduler loop in `internal/platform/jobs.go`.
5. **Demo inbox.** Where preview currently writes emails and WhatsApp codes to the app log (`internal/platform/notify.go`,
   `auth.go`, `phone.go`), also store them in a per-demo, per-browser inbox when the app is a demo; serve it at
   `/_rx/demo/inbox` on the demo host (a page the banner links to).
6. **Gallery and read-only admin.** In `internal/web`, add `GET /templates` (cards from `examples.Gallery()`), and
   `GET /templates/{example}/admin`, `/t/{table}`, `/t/{table}/calendar`, `/t/{table}/{id}` reusing the admin renderers with
   a read-only flag (no forms, no POST routes). Link the gallery from the landing page and `/llms.txt`.

## Risks and how we handle them
- Visitors type real personal data that other visitors then see (shared data, public admin) → banner warning, hourly wipe, no
  real messages sent; accept.
- Offensive content shown to others until the next reset → hourly reset, existing per-IP rate limits, and `/abuse` reports; accept.
- A demo bug or heavy use takes script workers from paying customers → `usr_demo` is one account, already capped at half the
  workers and 2 concurrent scripts per app.
- A demo leaks preview-only privileges (e.g. preview test-call toggle for `http.fetch`) → demos never get the toggle; test it.
- A demo sign-in code reaches another visitor → inbox is keyed to the browser that made the request; test it.
- Disk growth from demo uploads → existing per-app upload limits plus hourly removal.

## Verification
- Automated: `internal/platform` tests for provisioning (new/changed example re-applies, unchanged does not), reset to seed,
  public access without preview cookie, no real email/WhatsApp/payment/fetch from a demo, inbox isolation between two browsers,
  `demo-` prefix refused for users; `internal/web` tests that `/templates` lists every gallery example and the read-only admin
  has no forms and refuses POSTs; one e2e spec in `e2e/tests/` that opens the gallery, books on `booking-uae`'s demo, signs in
  via the demo inbox and sees the booking in the read-only calendar.
- Manual: after deploy, open `https://renoxium.com/templates` on a phone, try two demos end to end, and wait one hour to see
  the data reset.

## Open questions deferred
- Gallery card visuals (screenshots vs. icons): start with text and a category icon.
- Whether demos should also expose their public MCP tools in the gallery ("ask ChatGPT about this shop"): leave them reachable,
  do not advertise in v1.
- Reset interval tuning (hourly vs. every 30 min) once real traffic is seen.
