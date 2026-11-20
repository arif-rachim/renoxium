# Agent tools for building and checking large apps

Date: 2026-11-20. Source: a session in which an agent built a ride-hailing app from
`home-service-uae` and redesigned it through the MCP tools. These notes cover the tool
signatures, limits and security of each priority. P1–P4 and the `css_extra` part of P7 are
implemented. P5, P6, the rest of P7 and P8 are designs only.

The rules every new tool keeps:
- A change reaches only the app's private preview. Live changes only when the owner confirms
  `publish` in the browser.
- Test visitors, test sessions and screenshots exist on preview only. They never read or write
  live data, and they go away with the preview data on the next change.
- Scripts still run only inside the app's sandbox.
- Every existing tool and field keeps working as before.

## P1. Partial reads and partial edits (done)

| Tool | New input | Output |
| --- | --- | --- |
| `create_app` | `from_example` | `version`, `apply` (checks), `outline` |
| `get_app` | `only: [path]`, `outline: true` | `parts` or `outline` instead of `definition` |
| `get_example` | `only`, `outline`, `with_reference` | the same |
| `edit_app` (new) | `app`, `base_version`, `edits: [{target, old_text, new_text}]` | apply result plus `applied` (one line per edit) |

**Paths:**
- Named parts: `templates[.name]`, `scripts[.name]`, `lib[.name]`, `tables[.name]`, `seed[.table]`.
- Whole parts: `routes`, `jobs`, `tools`, `hosts`, `secrets`, `on_payment`, `on_admin_change`,
  `brand`.
- Limits: at most 40 paths per read. An unknown path is an error that lists the names that do
  exist, so a typo never looks like an empty part.

**Outline:** the size of the definition, each template, script and lib entry with its size,
tables with their columns, seed row counts, and the routes (with `(auth user)`), jobs, tools and
hosts as one line each. For `home-service-uae` it is about 4 KB, against 183 KB for the whole
definition.

**edit_app rules:**
- `target` is `templates.X`, `scripts.X` or `lib.X`. A bare name means the template of that
  name if it exists, otherwise the script.
- `old_text` must occur exactly once in the target.
- An empty `old_text` creates a new entry or fills an empty one, such as `css_extra`.
- Edits apply in order, and all of them or none.
- `base_version` is required and must be the newest version. This guards against overwriting a
  change the agent has not seen, as patch_app does.
- At most 50 edits per call.
- When an edit misses, the error names the edit and says whether the text was not found (with a
  similar line if only the spacing differs) or found several times.
- The result is applied exactly like `apply_change`: validated, size-checked, a new version, a
  new preview, and the checks.

**Result:** the demo changes one CSS rule and one script line in a 350-byte call
(`e2e/agent-demo.mjs`).

## P2. Signed-in flows on preview (done)

**`create_test_visitor {app, email?, phone?, name?}`:**
- Adds a confirmed visitor without a password to the preview's `rx_users` table.
- Without an email or phone it makes up an `@example.com` address.
- An existing email or phone returns that visitor with `existed: true`.
- At most 50 per preview, tracked in `rx_test_visitors`. That table is created only in the
  preview database, and scripts cannot read `rx_` tables.
- Use the result with `test_request {user_email}` or `screenshot_preview {as_visitor}`.

**New `test_request` fields:**
- `session: "s1"` is a cookie jar per app and name, kept in memory per host:
  - It covers only the preview host and its sign-in host.
  - At most 10 per app, for 1 hour.
  - Jars are dropped when the preview is rebuilt.
  - In a session, a GET of `/_rx/auth/<page>` starts on the app host, as a link would, which
    sets the hand-over state. A POST goes to the sign-in host, where the form is. Posting
    straight away gets a consistent state, so the real sign-up finishes through
    `/_rx/auth/callback`.
- `follow_redirects: true`:
  - Follows up to 8 redirects as GET.
  - Only within the preview host and its sign-in host. Any other host stops the chain and is
    reported.
  - The answer is the last page. `redirects` lists the hops, and `logs` has every hop's log.
- `path` may also be a full URL on those two hosts, so a redirect target can be called by hand.
- `user_email`, `session` and `follow_redirects` are refused on live.

## P3. Screenshots of the preview (done)

**`screenshot_preview {app, path, viewport, width, height, full_page, as_visitor, wait_ms, motion}`:**
- Viewports: `mobile` 390x844 (default), `tablet` 820x1180, `desktop` 1366x900, or `width`
  320–1920 with `height` 320–2000.
- `full_page` is capped at 8000 px.
- By default the page gets `prefers-reduced-motion: reduce`, so animations do not blur the shot.
- `wait_ms` is at most 5000.
- Returns an MCP image (PNG) and `{status, url, width, height, bytes, browser_log, blocked_hosts}`.
  `browser_log` holds script errors and console errors.

**How it works:**
- Headless Chromium is driven over the DevTools protocol on a pipe
  (`--remote-debugging-pipe`, fd 3 and 4). No port is opened and no dependency is added.
- Every request the browser makes is paused (`Fetch.requestPaused`) and answered:
  - **The preview host:** served in this process through `ServeApp`, GETs only, with the
    owner's preview cookie and, with `as_visitor`, a one-minute visitor session.
  - **Not served:** `/_rx/live/*` (long-lived streams), `/_rx/log` and sign-in pages. A
    redirect to sign in shows a short explanatory page and status 401.
  - **Public HTTPS CDNs:** allowed to go out. These are fonts.googleapis.com,
    fonts.gstatic.com, unpkg, jsDelivr, cdnjs and OpenStreetMap/Carto tiles.
  - **Anything else:** fails, and its host is reported in `blocked_hosts`.

**Security (SSRF):**
- An app's HTML cannot make the server's browser load internal addresses
  (`169.254.169.254`, `127.0.0.1`, private networks). Requests are decided by host before
  anything is fetched.
- As a second line, the browser's DNS resolves nothing but the CDN list
  (`--host-resolver-rules`).
- A test checks this with a page that loads 169.254.169.254, 127.0.0.1 and an unknown host.

**Limits:**
- 2 browsers at once on the server.
- 10 shots a minute per app.
- 25 s per shot, and 8 s per preview request.
- Each shot gets a temporary profile directory, deleted afterwards. `--no-sandbox` is used only
  when running as root.

**Setup:** opt in with `RENOXIUM_CHROMIUM=/path/to/chrome`. Without it the tool answers
"screenshots are not set up on this server". The hardened systemd unit (`MemoryDenyWriteExecute`,
`RestrictNamespaces`, `MemoryMax=800M`) stops Chromium, so turning it on needs the drop-in in
`docs/deploy-ubuntu.md`. That drop-in has not been tried on the production unit yet. Expect about
150 MB of memory per running shot.

**Not built:** `preview_link(app, minutes)`. The owner's existing preview link
(`/apps/<app>/preview`) covers sharing. A signed link an agent's own browser could open would
hand out a bearer to a copy of live data, so it is left for later with an explicit owner
confirmation.

## P4. Broader automatic checks (done)

Every GET route is now rendered after a change, up to 60 routes.

**Parameters** are filled from the first row of the matching table, in seed rows or preview
data:
- The segment before `:id` names the table: `s` matches `services` by prefix, `crew` matches
  `crews`, `bookings` matches `bookings`.
- A `:booking_id` parameter names its table directly.
- If no segment matches, an earlier segment is tried (`/services/by/:slug`), then a table with a
  column named like the parameter (`:slug`).
- If a row gives 404, up to three candidate tables are tried.
- With no rows, the check says so instead of failing.

**`auth: user` routes** are rendered signed in:
- As the visitor who owns the row, when the table has `user_id` and that visitor exists.
- Otherwise as a check visitor, which is deleted after the checks.

**Each check returns** `route`, `path` (as rendered), `as`, `status`, `error` and `warnings`,
plus a `note` for routes that were not rendered or answered 404.

**Template errors** come with the template name and line from Go (`broken:3:...`).

**Missing keys:**
- `html/template` renders a missing map key as an empty string, not `<no value>`, so a
  `<no value>` scan finds almost nothing. The scan is kept for the rare cases where it does
  appear.
- During checks, each rendered page is rendered again with `missingkey=error`:
  - Keys missing inside `if`, `with`, `range`, `else` or an assignment are how templates handle
    optional values, so they are rewritten to a lenient lookup and the render continues.
  - A key that is *printed* but missing is reported, for example
    `service:1:67: {{.s.price}} prints nothing because the data has no key "price"`.
- All bundled examples pass with no warnings.

## P5. Delete and rename apps (design)

**`delete_app {app}`:**
- Returns `confirm_url` (`/apps/<app>/delete`, which already exists in the admin panel) and
  changes nothing.
- The owner confirms there while signed in. The existing deletion runs: data and files are
  removed, and the name is held for the owner for 90 days.

**`rename_app {app, new_name}`:**
- Returns `confirm_url` to `/apps/<app>/rename?to=<new_name>`.
- Before returning, it validates `new_name` with the `create_app` rules and checks that it is
  free and not held.

**On confirmation (one transaction):**
- Move the app directory.
- Rename the `apps` row and its `versions`.
- Re-point the `domains` rows (live, preview and sign-in hosts).
- Keep a `redirects` row from the old name.

**Old address and limits:**
- The old host answers `301` to the same path on the new host for 1 year. Its name stays
  reserved for the owner, like a deleted name.
- Visitor sessions are per host, so visitors sign in again after a rename. The confirmation
  page says so.
- Custom domains keep working because they map to the app, not the name.
- Stripe, jobs and realtime channels are keyed by app name, so they move in the same
  transaction.
- At most one rename per app per 30 days.

## P6. Locales beyond en/ar (design)

**Definition:**
- New field `locales: ["id"]` or `["en", "ar"]`, default `["en", "ar"]`.
- The first locale is the default.
- `ar` turns on right-to-left layout.

**Sign-in pages:**
- The platform's built-in texts (`authpages.go`) are moved into a table keyed by locale, with
  en, ar and id first.
- The language is chosen in this order: `lang=` on the page that sent the visitor, the
  browser's `Accept-Language` among the app's locales, then the app's default.

**Overrides:**
- With `brand.texts: {table, key_column, locale_columns}`, the sign-in pages read
  `auth_<key>` rows from the app's texts table. The platform reads this table, never scripts.
  Keys left out use the built-in text.
- Overrides are plain text and escaped. No HTML is allowed, because the sign-in host runs no
  app code.

**Examples:** the texts tables keep the `en` and `ar` columns. The reference explains adding an
`id` column and `locales: ["id"]`, and the examples' `t()` helpers fall back to `en`.

## P7. Theme overrides (css_extra done; the rest is in place)

**Done:**
- Every example with a `css` template now has an empty `css_extra` template, included in
  `head` right after `css`.
- `edit_app` with an empty `old_text` fills it.
- The reference and the server instructions name it as the place for redesigns.
- A test checks that its rules reach the page.

**Brand colour and font:** in the UAE examples these already come from the settings row
through the `theme()` lib helper, which writes CSS variables on `<html>`. That stays the place
for colour, font and logo.

**Next step (not built):** a check that warns when `css_extra` sets `--brand` while the
settings row has a brand colour, since the owner's admin-panel change would then not show.

## P8. On-demand and dispatch apps (design)

**`geo` helper in scripts:**
- `geo.distance({from: {lat, lng}, to: {lat, lng}})` returns `{km}` (haversine, no network).
- `geo.route({from, to})` returns `{km, minutes}` through a provider the platform holds, such
  as an OSRM or Mapbox key on the operator's account. It is billed against the existing
  `http.fetch` monthly quota, cached for 10 minutes per rounded coordinate pair, and limited to
  5 calls per request.
- Addresses are geocoded only through `geo.geocode(text)`, with the same limits. The key is
  never visible to scripts or agents.

**`dispatch` example:**
- **Customer:** requests a ride or courier now, with pickup and drop-off pins (Leaflet tiles
  from the CDN list).
- **Pricing:** the server prices it with `geo.route` and a per-km tariff in the settings row.
- **Matching:**
  - Drivers have a private link (the crew-link pattern from `home-service-uae`) and share
    their position every 10 s with `POST /driver/:token/position`. This is stored and
    published on `realtime`.
  - A `dispatch` lib picks the nearest available driver by `geo.distance` and offers the job
    for 30 s (`realtime` to the driver's page).
  - The offer moves to the next driver on decline or timeout. A job running every minute
    re-offers stuck requests.
- **Tracking:** the customer page subscribes to `job-<id>` (token from `realtime.token`) and
  shows the driver moving, as `delivery-tracking` does.
- **Payment:** cash, or a card hold at request and capture on completion (existing
  `payments.checkout {hold}` and `payments.capture`).

## Demo

`e2e/agent-demo.mjs` runs the whole flow through MCP tools from an empty account:
1. Create the app from `home-service-uae`.
2. Read one script.
3. Edit one CSS rule and one script line (350 B).
4. Create a test visitor.
5. Place a cash booking and follow it to its page.
6. Take a 390 px full-page screenshot signed in.
7. Cancel the booking.

It runs against any server with `RENOXIUM_URL` and `RENOXIUM_TOKEN`, and on the test server as
`e2e/tests/11-agent-tools.spec.mjs`. The same steps run in Go, without a browser, in
`internal/web/mcpdemo_test.go`.
