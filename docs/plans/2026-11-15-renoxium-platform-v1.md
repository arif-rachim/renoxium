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
- Is enterprise a target? → No. Renoxium is for UMKM (small businesses) only; large-enterprise needs (HA, SSO, compliance certification) are not a goal, not just deferred.
- How wide is v1? → General app builder; the online store is the reference template and the acceptance test for v1. (User's choice. The store defines which primitives get built first; nothing is built that the store does not need.)
- How does the owner operate the app daily? → Auto-generated web admin panel, plus chat for building/changing, plus outbound WhatsApp notifications.

## Assumptions (not asked, simplest option taken — change here if wrong)
- Script language is JavaScript (LLMs write it best), run by an embedded engine behind a small `Sandbox` interface so the engine can be swapped. Start with goja; move to QuickJS-on-wazero if hard memory caps are needed.
- Templates are Go `html/template` (auto-escaping) stored per tenant; styling via one bundled CSS file, no client-side framework.
- Auth, sessions, payments, file upload, email and WhatsApp are platform primitives called from scripts. Agent-written code never handles passwords, card data or raw SQL connections to other tenants.
- Scripts reach the database only through a host API bound to the current tenant's SQLite file.
- Single node, single Go binary, SQLite in WAL mode, Litestream-style backup to object storage. No clustering in v1.
- One MCP server (remote, OAuth) serves ChatGPT, Claude and other agents. No separate per-vendor plugin.
- Apps are served on `<app>.renoxium.app`; custom domains later.
- Payments: one Indonesian gateway (Midtrans or Xendit, QRIS included) behind a `payments` primitive. Merchant uses their own gateway account.
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
- Versioning with preview URL and rollback, including schema migrations.

### Out (later or never)
- Agents writing or compiling Go.
- Enterprise features: SSO, audit log, HA/multi-region, compliance.
- Custom domains, marketplace of third-party templates, Renoxium billing.
- Two-way WhatsApp, mobile apps, client-side SPA features.
- A Renoxium-owned chat UI.

## Approach
1. **Repo skeleton + app definition format.** Go module, `cmd/renoxium`, `internal/appdef` with the JSON schema for an app (tables, routes, templates, scripts, jobs) and a validator. Document it in `docs/appdef.md`.
2. **Tenant store.** `internal/tenantdb`: open/pool SQLite per tenant, WAL, size quota, declarative migrations generated from table definitions (add table/column only; destructive changes require explicit flag).
3. **Runtime + SSR.** `internal/runtime`: host-based tenant routing, load active app version, render `html/template` with data from declarative queries. Benchmark target: p95 < 20 ms for a catalog page.
4. **Script sandbox.** `internal/sandbox`: `Sandbox` interface, goja implementation, host API (`db`, `http.respond`, `auth`, `log`), CPU-time and step limits, no filesystem/network. Isolation and runaway-script tests.
5. **Versioning.** Immutable app versions, preview URL per version, publish, rollback (schema snapshot + restore).
6. **MCP server.** `internal/mcp`: OAuth, tools `create_app`, `get_app`, `apply_change`, `preview`, `publish`, `rollback`, `list_templates`, `use_template`, `get_logs`. Tool descriptions teach the agent to ask the user for missing info and to offer templates first.
7. **Primitives.** `internal/primitives`: auth/sessions, uploads, email, payments (one gateway + webhook), WhatsApp notify, job scheduler.
8. **Admin panel.** `internal/admin`: generated list/detail/edit screens per table, owner login, order view.
9. **Store reference template.** `templates/store`: catalog, cart, checkout, order, stock, order-notification job. This is the v1 acceptance test.
10. **Hardening + launch.** Quotas, rate limits, per-tenant logs, backup/restore drill, deploy on one server, end-to-end test from ChatGPT and Claude.

## Risks and how we handle them
- Sandbox escape or noisy neighbour → no fs/network in scripts, tenant-bound DB handle, step/time limits, automated isolation tests; swap engine to WASM-hosted JS if memory caps prove insufficient.
- Agent-generated logic is wrong (prices, stock, auth) → money, auth and stock decrement live in platform primitives; preview before publish; one-call rollback.
- "General builder" spreads effort thin → the store template is the only acceptance test for v1; a primitive is built only if the store needs it.
- Schema change destroys live data → additive migrations by default, snapshot before every publish, destructive changes need an explicit confirmed flag.
- Dependence on ChatGPT/Claude distribution rules → plain MCP + OAuth, no vendor-specific features; admin panel works without any chat platform.
- SQLite single-writer limits → accept for v1; one busy tenant only slows itself.
- Competing with Shopify/Lovable/Replit → compete on local payments, WhatsApp, price and speed; revisit after first 10 real sellers.

## Verification
- Automated: Go unit tests per package; sandbox isolation and timeout tests; migration and rollback tests; an end-to-end test that applies the store template via the MCP tools and places an order over HTTP.
- Manual: from ChatGPT, install the MCP server, ask for a store, place a paid test order (gateway sandbox), see it in the admin panel and receive the WhatsApp message.

## Open questions deferred
- goja vs QuickJS-on-wazero as the long-term script engine.
- Midtrans vs Xendit; WhatsApp provider.
- Hosting provider and backup target.
- Renoxium's own pricing and free-tier quotas.
- When and whether to add custom domains and a template marketplace.
