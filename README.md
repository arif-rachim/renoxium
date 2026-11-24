<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="brand/renoxium-logo-white.svg">
    <img src="brand/renoxium-logo.svg" alt="Renoxium" height="64">
  </picture>
</p>

<p align="center">
  <strong>Build, preview and host business web apps by chatting with an AI assistant.</strong><br>
  An MCP-native application platform for small businesses — designed for ChatGPT, Claude and any MCP-compatible agent.
</p>

<p align="center">
  <a href="https://renoxium.com">Website</a> ·
  <a href="https://renoxium.com/templates">Template gallery</a> ·
  <a href="docs/roadmap.md">Roadmap</a> ·
  <a href="docs/plans/2026-11-15-renoxium-platform-v1.md">Platform plan</a> ·
  <a href="deploy/README.md">Deployment</a> ·
  <a href="docs/self-host.md">Self-hosting one app</a>
</p>

---

## Overview

Renoxium lets a small-business owner get a working online store, booking system, quote portal or listings site without writing code or managing servers. The owner describes what they need to their AI assistant; the assistant builds the app through Renoxium's MCP tools; Renoxium hosts it, secures it and handles payments, sign-in, email and WhatsApp.

Under the hood, the agent submits a **declarative app definition** — tables, HTML templates, server scripts, routes, jobs and tools. Renoxium serves it with server-side rendering, gives each app stage its own SQLite database, runs scripts in a hardened sandbox and keeps a per-app log that the agent reads to diagnose and fix its own mistakes.

> **Status:** thin slice — the end-to-end platform is working and deployed; scope is expanding along the [roadmap](docs/roadmap.md).

### Why Renoxium

| For business owners | For AI agents | For the operator |
|---|---|---|
| A real, branded web app from a conversation | 25+ purpose-built MCP tools with structured feedback | Multi-tenant isolation and per-app resource metering |
| Card payments into their own Stripe account | Private preview stage with simulated payments and email | Abuse limits, suspension and audit logging |
| Admin panel, dashboard and calendar generated automatically | Automatic route checks after every change | Growth funnel, unit economics and cost per app |
| English and Arabic (RTL), AED with VAT | Ready-made UAE templates to start from | Monitoring, backups and alerting built in |

---

## How it works

```
 Owner ──chat──▶ ChatGPT / Claude ──MCP (OAuth 2.1)──▶ Renoxium /mcp
                                                           │
                         ┌─────────────────────────────────┼──────────────────────────────┐
                         ▼                                 ▼                              ▼
                 Preview stage                      Owner confirms                 Live stage
         (private, snapshot of live data,  ──▶   publish on renoxium.com  ──▶  (public, own domain,
          simulated payments & email)                                        real payments & email)
```

1. **Connect.** The owner adds `https://renoxium.com/mcp` as a connector in ChatGPT or Claude and signs in through OAuth.
2. **Build.** The agent creates an app — from scratch or from a template — and refines it with targeted patches.
3. **Test.** The agent browses the private preview as a visitor, submits forms, uploads files, runs scheduled jobs and takes screenshots.
4. **Publish.** The agent requests publication; the owner approves it on renoxium.com. Live data is backed up before every publish.
5. **Operate.** The owner manages data, payments, visitors and analytics from the admin panel, and asks the agent for changes at any time.

---

## Features

### Agent interface (MCP)

- **OAuth 2.1** as ChatGPT and Claude expect: protected-resource and authorization-server metadata, dynamic client registration, PKCE (S256 only), consent screen, rotating refresh tokens and revocation on code replay.
- **Builder tools** at `/mcp`: `list_apps`, `create_app` (optionally `from_example`), `get_app`, `get_app_outline`, `get_app_parts`, `get_example`, `apply_change`, `patch_app`, `edit_app`, `view_page`, `submit_form`, `upload_as_visitor`, `view_live_page`, `create_test_visitor`, `screenshot_preview`, `try_scheduled_jobs`, `change_preview_row`, `publish` (including rollback), `rename_app`, `delete_app`, `upload_file` and `get_logs`. See [docs/plans/2026-11-20-agent-tools.md](docs/plans/2026-11-20-agent-tools.md).
- **Automatic verification:** after every change, each GET route is rendered (filling `:id` from preview rows, signed-in routes as a visitor) and template errors or missing keys are reported back to the agent.
- **Owner-controlled actions:** publishing, renaming and deleting return a confirmation link the owner approves on renoxium.com. Text an agent reads in logs or pages cannot publish on its own.
- **Self-describing onboarding:** `/connect` and `/llms.txt` explain how to add the connector, so an assistant can guide the user; browser visits to `/mcp` get the same help.
- **Public app tools:** apps can expose their own MCP tools at `<app>/mcp`, so customers can add a shop to ChatGPT or Claude and ask about its products.

### Application runtime

- **Declarative definitions** with additive schema migrations and seed data.
- **Templates** with any HTML, CSS and browser JavaScript, with auto-escaped data.
- **Server scripts** in JavaScript (goja), executed in a pool of isolated worker processes with Landlock, time and memory limits, and a SQLite authorizer that permits data statements only.
- **Shared libraries** (`lib`) compiled per entry, with accurate stack traces and caching.
- **Scheduled jobs** (`every: "15m"`, `daily_at: "21:00"`, Asia/Dubai by default) with persisted last-run state and per-account queues.
- **Admin-change hook** (`on_admin_change`) to react when the owner or staff edits data.
- **Realtime** server-sent events via `realtime.publish` and `EventSource('/_rx/live/<channel>')`, with private and public channels.
- **External APIs** via `http.fetch` to owner-approved HTTPS hosts, with encrypted, host-bound secrets that scripts and agents never see.
- **Preview and live stages** per app; publishing keeps the three newest backups.

### Commerce and payments

- **Stripe Connect:** owners connect their own Stripe account; scripts create direct charges with `payments.checkout`.
- **Refunds, card holds and subscriptions:** `payments.refund`, `capture` / `release`, and `subscribe` / `cancel_subscription`, with a Payments page for the owner.
- **Signature-checked webhooks** run the app's `on_payment` script exactly once per status change.
- **Simulated checkout** on preview, so agents can test the full flow without real money.

### Identity and visitors

- **Built-in visitor accounts:** sign-up, sign-in, email confirmation, password reset and 6-digit WhatsApp codes.
- **Isolated sign-in host** per app stage (`<app>--id.<apps domain>`) with a script-free Content-Security-Policy, so app owners cannot read what visitors type.
- **Branded, bilingual sign-in pages** that follow the business's name and colour and switch to Arabic (RTL) automatically.
- **Private visitor uploads** (images and PDFs) visible only to the uploader and the owner.

### Owner experience

- **Admin panel** generated from the live schema: searchable, paginated tables, typed forms, labels and help text from the definition, and a full change log.
- **Calendar view** for bookings, crews and rentals, with day and week views.
- **Dashboard** with visitors, page views, referrers, card sales and new customers — privacy-preserving, with no visitor identifiers stored.
- **Images** deduplicated by content hash, auto-oriented, resized to fit and served on demand at any width (`?w=480`).
- **Custom domains** on paid plans through Cloudflare for SaaS, with TXT-record ownership proof.
- **WhatsApp notifications** for owner notices and card payments.

---

## Templates

Every template is a complete, working app definition the agent can start from. Each has a live, resettable demo in the [template gallery](https://renoxium.com/templates).

| Template | Use case |
|---|---|
| `store-uae` | Online store: catalog, product options, cart, card or cash on delivery, AED with VAT, order-ahead mode, EN/AR |
| `booking-uae` | Appointments and classes |
| `home-service-uae` | Bookings by arrival window, crew dispatch, repeat visits |
| `reservation-uae` | Tour departures and date-range rentals |
| `quote-portal-uae` | Quotes, deposits and a client portal |
| `listings-uae` | Property listings with permit numbers and lead capture |
| `clinic-queue-uae` | Walk-in queue for clinics: take a number, live place in the queue, TV screen, WhatsApp when two are ahead |
| `catalog` | Small product catalog with order form and JSON API |
| Task list | Minimal CRUD reference app |

---

## Security and isolation

Renoxium runs untrusted, agent-written code for many tenants on shared infrastructure. Isolation is enforced in layers:

- **Process sandbox:** scripts run in separate workers locked down with Landlock (no filesystem, no TCP), with a 2 s time limit, 64 MiB heap watchdog and 256 MiB resident cap.
- **Fair scheduling:** at most two concurrent scripts per app and half the worker pool per account.
- **Database guardrails:** a SQLite authorizer allows data statements only and hides platform tables (`rx_*`) from app code.
- **Browser protections:** cross-origin POSTs refused, `__Host-` cookies, HSTS and strict CSP on sign-in pages.
- **Secrets handling:** API keys encrypted with AES-GCM, bound to one host and injected by the platform.
- **Abuse limits:** per-visitor request rates, daily email and WhatsApp caps, guess limits and storage quotas — persisted, so restarts do not reset them.
- **Operator controls:** public abuse reporting at `/abuse`, per-app throttling and suspension, all logged and audited.

---

## Plans

Each app is paid for by what it does, the same on every account; no app carries Renoxium branding.

An account is **free** (no card saved: at most 3 apps) or a **member** (the owner saved a card at `/billing`: at most
50 apps). Saving a card charges nothing; it is Stripe Checkout in setup mode on the account's Stripe customer, and the
account is a member while an unexpired card is saved. Members get higher anti-abuse limits (WhatsApp 2,000 a month, app
emails 2,000 a day, storage 10 GB, 2,000 live connections), which a free account also gets once it pays for a work app or
has a shop whose Stripe takes card payments. When the card is removed or expires, nothing stops; only new apps beyond 3
are refused.

| App | Price | Notes |
|---|---|---|
| Static site | Free | Only pages and photos (no tables, scripts, jobs or sign-in); keeps its Renoxium address |
| Shop | AED 0 + 2% of card payments | Takes money from its customers; the fee is a Stripe application fee. Custom domain, 2 staff |
| Work app | AED 99 / month per app | Does not charge its customers (CRM, queue, attendance), or a shop paid only in cash or by transfer. Custom domain, 5 staff included and AED 25 a month for each one more (up to 10, billed as a second item of the same subscription), member limits; paused when its subscription ends |
| Larger businesses | By agreement | An operator grants the account membership (`status granted`): every app covered, no fee, 10 staff per app |

The owner chooses shop or work app on the app's Plan page (`/apps/<app>/plan`) before a non-static app first goes live.
Apps that were live before this rule keep running until they are published again. Members pay the shop fee and work app
subscriptions like everyone else; only granted accounts are exempt.

**No credits.** Building and changing apps through the agent is unlimited on every plan, the Free plan included, and is never
charged per request: owners use their own ChatGPT or Claude account, so Renoxium has no model costs to pass on. Claude's free
plan is enough (it allows one custom connector); in ChatGPT, adding the connector by its address needs developer mode, which is
on its paid plans.

Billing runs on Stripe Checkout and the Stripe Customer Portal at `/billing`.

Owners of a paid work app, or of an account by agreement with Renoxium, can also run it on their own server: the export carries a signed licence, and the same binary with `-self-hosted` serves that one app with its admin panel, payments on the owner's own Stripe account and email through their own sender. See [docs/self-host.md](docs/self-host.md).

---

## Operations

- **Monitoring:** disk space, backup age and error spikes checked every five minutes, with operator email alerts and a `/healthz` endpoint.
- **Resource metering:** requests, CPU, memory, worker time, emails, WhatsApp and API calls per app per hour, with estimated cost, revenue and margin at `/operator/usage`.
- **Growth analytics:** sign-up-to-paid funnel, MRR, ARPA, churn, LTV and CAC at `/operator/growth`.
- **Owner alerts:** email on a live app's first error, then at most daily.

---

## Architecture

- **Language:** Go — a single binary with no cgo.
- **Storage:** SQLite (pure-Go driver), one database per app stage; content-addressed file storage on local disk.
- **Scripting:** goja JavaScript engine in sandboxed worker processes.
- **Edge:** Cloudflare Tunnel and Cloudflare for SaaS custom hostnames.
- **Integrations:** Stripe (Connect and Billing), Resend (email), WhatsApp Cloud API, headless Chromium for preview screenshots.

---

## Getting started

### Run locally

```bash
go run ./cmd/renoxium -addr :8080 -data data
```

Open <http://localhost:8080> and create an account. Then add `http://localhost:8080/mcp` as a connector in an MCP client that supports OAuth.

> ChatGPT and Claude require a public HTTPS address. In production the connector address is `https://renoxium.com/mcp`.

Apps are served at `<app>.localhost:8080` and previews at `<app>--preview.localhost:8080`.

### Test

```bash
go test ./...
```

### Deploy

Renoxium runs behind Cloudflare Tunnel in production. See [deploy/README.md](deploy/README.md).

---

## Repository layout

| Path | Contents |
|---|---|
| `cmd/renoxium` | Server entry point |
| `internal/` | Platform packages |
| `examples/` | App templates and gallery metadata |
| `docs/` | Plans, roadmap and design notes |
| `deploy/` | Production deployment |
| `e2e/` | End-to-end suite against fake Stripe, Resend and WhatsApp |
| `brand/` | Logo, icons, social images and usage rules |

---

## Brand

Logos, icons and social images are in [`brand/`](brand/), together with the rules for using them.

---

<p align="center">
  © Renoxium FZC · SRTI Park, Sharjah, UAE · <a href="https://renoxium.com">renoxium.com</a>
</p>
