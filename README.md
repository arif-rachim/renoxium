# Renoxium

Build and host web apps for small businesses from a chat with ChatGPT, Claude or any MCP agent.
The agent sends a declarative app definition (tables, HTML templates, server scripts, routes);
Renoxium serves it with server-side rendering, one SQLite database per app stage, and a log
the agent can read to fix its own mistakes.

Plan: [docs/plans/2026-11-15-renoxium-platform-v1.md](docs/plans/2026-11-15-renoxium-platform-v1.md)

## Status: thin slice

Working today:

- Accounts on the Renoxium site (email and password, argon2id), a dashboard listing your apps.
- OAuth 2.1 for agents, as ChatGPT and Claude expect: protected resource and authorization server metadata, dynamic client registration, PKCE (S256 only), consent screen, rotating refresh tokens, revocation on code replay.
- Builder MCP endpoint at `/mcp`, OAuth-protected, with `list_apps`, `create_app`, `get_app`, `apply_change`, `publish` (also rollback) and `get_logs`. Every tool acts only on apps the signed-in user owns.
- Preview and live stages per app. Preview runs on a snapshot of live data; publish backs up live data first.
- Additive schema migrations, seed data.
- Server scripts in goja with a 2 s limit, no filesystem, network or timers, and a SQLite authorizer that allows data statements only.
- Templates with any HTML/CSS/browser JavaScript, auto-escaped data.
- One log per app: requests, script errors with stack traces, script console output, and browser errors reported by an injected snippet, linked by request ID.

Not built yet (see the plan): email verification and password reset, script worker processes with OS memory limits (scripts run in-process today),
admin panel, per-app MCP, Stripe, WhatsApp, custom domains, quotas.

## Run

```bash
go run ./cmd/renoxium -addr :8080 -data data
```

Open `http://localhost:8080` to create an account, then add `http://localhost:8080/mcp` as a connector in an
MCP client that supports OAuth (ChatGPT and Claude need a public HTTPS address, see below).

Apps are served on `<app>.localhost:8080` and previews on `<app>--preview.localhost:8080`.
In production run with `-base-url https://renoxium.com -apps-domain renoxium.app` behind a TLS terminator.
The connector address for ChatGPT and Claude is then `https://renoxium.com/mcp`.

`examples/catalog.json` is a complete small perfume shop (catalog, product page, order form, JSON API).

## Test

```bash
go test ./...
```

No cgo: the SQLite driver and the JavaScript engine are pure Go.
