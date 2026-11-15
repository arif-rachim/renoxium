# Renoxium platform v1

**Goal:** Let a non-technical seller build and run a real web app (first: an online store) purely by chatting with ChatGPT/Claude, on a multi-tenant Go SSR runtime with one SQLite database per tenant.

**Done when:**
- From ChatGPT or Claude, with the Renoxium MCP server installed, a user can say "build me a store for my products" and get a live URL with a working storefront in one conversation.
- A test order placed on that storefront appears in the generated admin panel and triggers a WhatsApp notification to the owner.
- A script from tenant A cannot read tenant B's data, and an infinite loop in a tenant script is killed without affecting other tenants (proven by automated tests).
- A bad change made by the agent can be rolled back to the previous version with one tool call, including the schema.

## Decisions
- How is agent-submitted logic executed? → Declarative schema + routes + HTML templates + small sandboxed scripts inside the Go runtime. Agents do not write Go. (Only option that is both safe and memory-cheap in one multi-tenant process.)
- Who is v1 for? → Non-technical sellers using ChatGPT/Claude. (Platform must own everything technical.)
- Is enterprise a target? → No. Renoxium is for small businesses only; large-enterprise needs (HA, SSO, compliance certification) are not a goal, not just deferred.
- May templates contain arbitrary client-side JavaScript? → Yes. Agents can put any HTML/JS in templates; it runs in the visitor's browser, not on the server. Guard rails: one origin per app, admin panel and Renoxium dashboard on a separate domain, auto-escaping of visitor data, card data only on the gateway's page.
- How does the agent find out what broke? → One unified log per app covering server and browser, readable through the MCP `get_logs` tool (see Approach step 6a).
- Which environments does an app have? → Two stages, not three: **preview** (draft version, where the agent develops and automatic checks run) and **live**. Preview runs on a snapshot copy of the live database (or template sample data for a new app), and all outside effects are sandboxed: payment gateway in sandbox mode, WhatsApp/email captured to the log instead of sent, jobs not scheduled. Migrations are proven on the copy before they touch live data.
- What telemetry exists? → One event pipeline feeds three consumers: agent-readable logs, per-tenant usage counters (quotas/billing), and operator health metrics.
- Can other agents use a built app? → Yes. Every app serves its own MCP endpoint at `<app>.renoxium.app/mcp`, separate from the builder MCP at renoxium.com. Tools are generated from the app's **actions** (named, typed operations with a permission level); the admin panel, HTTP routes and the app MCP all call the same actions, so there is one permission model. v1: owner tools behind OAuth (orders, stock, products) and public read-only tools (search/get products). Buyer agents get a checkout link; they never pay on the buyer's behalf.
- Custom domains (e.g. `www.tokoku.com` bound to `tokoku.renoxium.app`, Netlify-style)? → Planned right after v1, as a paid feature. v1 must not block it: tenant routing looks the request host up in a domain table (never parses the subdomain), and all generated links, redirects, payment callbacks and the app MCP URL come from one "canonical host" helper. Later design: merchant adds CNAME (`www`) or A record (apex) plus a TXT ownership record; platform verifies, then issues TLS automatically with a pure-Go ACME library (CertMagic), only for domains present in the domain table; the renoxium.app address redirects to the custom domain.
- Which script engine? → goja (pure Go JavaScript interpreter), behind a `Sandbox` interface, executed in a small pool of separate worker processes with OS memory limits (goja has no hard memory cap). QuickJS-on-wazero is the upgrade path if in-process memory caps or newer JS syntax become necessary.
- Build constraints → no cgo anywhere: pure-Go SQLite driver (`modernc.org/sqlite`), no `v8go`, no embedded V8/Deno. Keeps builds fast and cross-compilation trivial.
- Which market? → United Arab Emirates (not Indonesia). Consequences for v1: prices in AED, 5% UAE VAT on orders and invoices, English and Arabic including right-to-left layouts in templates and the admin panel, cash on delivery as an order payment method, personal data handled under the UAE data protection law (PDPL).
- Payments → Stripe. Each merchant connects their own Stripe account (Stripe Connect), checkout happens on Stripe-hosted pages, the platform receives webhooks. Connect also leaves the door open for a per-transaction platform fee later.
- Already in place (owner confirmed) → domains `renoxium.com` (site, dashboard, builder MCP) and `renoxium.app` (tenant apps), a VPS, a Stripe account, a WhatsApp Business setup, and a registered legal entity.
- Client-side libraries → none bundled or required. The agent writes its own browser JavaScript. Routes can return a full page, an HTML fragment (a partial template) or JSON. Builder tool guidance tells the agent to render visitor-supplied data through server templates (auto-escaped) or `textContent`, never by building HTML strings in the browser.
- Build order → a thin slice first (app definition, per-tenant SQLite, SSR runtime, goja sandbox, builder MCP with a few tools) to prove that an agent can build a working catalog and that tenants are isolated, before the rest of the plan.
- Entry point → the MCP server only, used first by paid ChatGPT users (developer mode) and Claude users, then submitted to the ChatGPT and Claude app directories. No Renoxium-owned chat in v1 (custom MCP in ChatGPT appears limited to paid plans).
- How wide is v1? → General app builder; the online store is the reference template and the acceptance test for v1. (User's choice. The store defines which primitives get built first; nothing is built that the store does not need.)
- How does the owner operate the app daily? → Auto-generated web admin panel, plus chat for building/changing, plus outbound WhatsApp notifications.

## Assumptions (not asked, simplest option taken — change here if wrong)
- Templates are Go `html/template` (auto-escaping) stored per tenant; styling via one bundled CSS file, no client-side framework.
- Auth, sessions, payments, file upload, email and WhatsApp are platform primitives called from scripts. Agent-written code never handles passwords, card data or raw SQL connections to other tenants.
- Scripts reach the database only through a host API bound to the current tenant's SQLite file.
- Single node, single Go binary, SQLite in WAL mode, Litestream-style backup to object storage. No clustering in v1.
- One MCP server (remote, OAuth) serves ChatGPT, Claude and other agents. No separate per-vendor plugin.
- Apps are served on `<app>.renoxium.app`; custom domains later.
- WhatsApp is outbound notifications only in v1 (no two-way bot).
- Every change by the agent creates a new immutable app version; production points at one version.
- Pricing/billing for Renoxium itself is out of v1; hard quotas per tenant are in.

## Scope
### In (v1)
- Go runtime: tenant routing, SSR, sandboxed script execution, per-tenant SQLite, quotas.
- App definition format: tables, routes, templates, scripts, jobs, modules.
- MCP server with tools to create/inspect/change/preview/publish/rollback an app and to list/apply templates.
- Platform primitives: auth, payments, uploads, email, WhatsApp notify, scheduled jobs.
- Auto-generated admin panel from the schema.
- Store reference template (catalog, cart, checkout, orders, stock).
- Unified per-app logs (server + browser) readable by the agent.
- Per-app MCP endpoint (owner tools + public read-only catalog tools).
- Versioning with preview URL and rollback, including schema migrations.

### Out (later or never)
- Agents writing or compiling Go.
- Enterprise features: SSO, audit log, HA/multi-region, compliance.
- Custom domains (first feature after v1; routing is prepared for it), selling domains, marketplace of third-party templates, Renoxium billing.
- Buyer agents completing payment inside the chat (agentic checkout protocols); v1 returns a checkout link instead.
- Two-way WhatsApp, mobile apps, client-side SPA features.
- A Renoxium-owned chat UI.

## Approach
1. **Repo skeleton + app definition format.** Go module, `cmd/renoxium`, `internal/appdef` with the JSON schema for an app (tables, routes, templates, scripts, jobs) and a validator. Document it in `docs/appdef.md`.
2. **Tenant store.** `internal/tenantdb`: open/pool SQLite per tenant, WAL, size quota, declarative migrations generated from table definitions (add table/column only; destructive changes require explicit flag).
3. **Runtime + SSR.** `internal/runtime`: host-based tenant routing, load active app version, render `html/template` with data from declarative queries. Benchmark target: p95 < 20 ms for a catalog page.
4. **Script sandbox.** `internal/sandbox`: `Sandbox` interface, goja implementation running in worker processes (memory-limited by the OS, restarted on crash), host API (`db`, `http.respond`, `auth`, `log`), CPU-time and step limits, no filesystem/network. Isolation and runaway-script tests.
5. **Versioning and stages.** Immutable app versions; preview URL per version backed by a snapshot copy of the live database with sandboxed primitives; automatic checks on preview; publish; rollback (schema snapshot + restore).
6. **MCP server.** `internal/mcp`: OAuth, tools `create_app`, `get_app`, `apply_change`, `preview`, `publish`, `rollback`, `list_templates`, `use_template`, `get_logs`. Tool descriptions teach the agent to ask the user for missing info and to offer templates first.
6c. **Per-app MCP endpoint.** Add `actions` to the app definition (name, description, input schema, permission: `public`/`customer`/`staff`/`owner`, handler). Serve `/mcp` per app from the same MCP code as the builder server: tool list derived from actions, OAuth for non-public tools, per-app rate limits, calls logged and counted like HTTP requests. Text returned from tenant data (product descriptions, reviews) is marked as untrusted content.
6a. **Unified logs.** `internal/applog`: one append-only, size-capped log per app, stored in a separate SQLite file from the app data. Every event carries app version, request ID, source (`server`/`browser`/`job`), level and timestamp.
   - Server events: request (route, status, duration, script CPU ms), script errors with stack and script line, `console.log` from scripts, DB and migration errors, job runs, primitive failures (payment webhook, WhatsApp, email).
   - Browser events: a small platform-injected snippet reports `window.onerror`, unhandled promise rejections, `console.error`, failed fetches/resources and CSP violations to `/_rx/log` via `sendBeacon`, tagged with the request ID of the page render so server and browser events line up.
   - Intake is public, so: rate limit, size cap, group identical errors with a counter, redact secrets and personal data.
   - `get_logs` filters by version, time, level, source and request ID, and returns "new errors since version N". `preview` and `publish` render every route once and return any errors directly in the tool result.
   - Log text is untrusted input: tool results mark it as data, never as instructions to the agent.
6b. **Usage counters and operator metrics.** From the same event pipeline: per-tenant counters (requests, script CPU ms, storage bytes, messages sent) for quotas, and platform metrics (error rate, latency, per-tenant CPU, slow queries) exposed for the operator.
7. **Primitives.** `internal/primitives`: auth/sessions, uploads, email, payments (one gateway + webhook), WhatsApp notify, job scheduler.
8. **Admin panel.** `internal/admin`: generated list/detail/edit screens per table, owner login, order view.
9. **Store reference template.** `templates/store`: catalog, cart, checkout, order, stock, order-notification job. This is the v1 acceptance test.
10. **Hardening + launch.** Quotas, rate limits, per-tenant logs, backup/restore drill, deploy on one server, end-to-end test from ChatGPT and Claude.

## Risks and how we handle them
- Sandbox escape or noisy neighbour → no fs/network in scripts, tenant-bound DB handle, step/time limits, automated isolation tests; swap engine to WASM-hosted JS if memory caps prove insufficient.
- Agent-generated logic is wrong (prices, stock, auth) → money, auth and stock decrement live in platform primitives; preview before publish; one-call rollback.
- "General builder" spreads effort thin → the store template is the only acceptance test for v1; a primitive is built only if the store needs it.
- Browser log intake abused (spam, fake errors, text aimed at steering the agent) → rate limits, grouping, size caps, and logs always presented to the agent as untrusted data.
- Free tenants host phishing/abuse pages using free-form HTML/JS → report button, basic scanning, app domain on the Public Suffix List, fast takedown.
- App MCP used to scrape, spam or inject instructions into other agents via product text → rate limits, public tools read-only, no payment or destructive action without a human-confirmed step, tenant text returned as data.
- Custom domain hijack (stale DNS pointing at Renoxium claimed by another tenant) → TXT ownership proof before binding, binding removed when an app is deleted, certificates only for verified domains.
- Schema change destroys live data → additive migrations by default, snapshot before every publish, destructive changes need an explicit confirmed flag.
- Dependence on ChatGPT/Claude distribution rules → plain MCP + OAuth, no vendor-specific features; admin panel works without any chat platform.
- SQLite single-writer limits → accept for v1; one busy tenant only slows itself.
- Competing with Shopify/Lovable/Replit → compete on a store that sells out of the box for the UAE (AED, VAT, Arabic/RTL, cash on delivery, WhatsApp), no build credits, price and speed; revisit after first 10 real sellers.

## Verification
- Automated: Go unit tests per package; sandbox isolation and timeout tests; migration and rollback tests; an end-to-end test that applies the store template via the MCP tools and places an order over HTTP.
- Manual: from ChatGPT, install the MCP server, ask for a store, place a paid test order (gateway sandbox), see it in the admin panel and receive the WhatsApp message.

## Open questions deferred
- Backup target (object storage) and whether the existing VPS has a fixed IP.
- Which UAE shipping carriers to integrate first.
- Renoxium's own pricing and free-tier quotas.
- When and whether to add custom domains and a template marketplace.
