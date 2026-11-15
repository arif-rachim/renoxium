# Renoxium

Build and host web apps for small businesses from a chat with ChatGPT, Claude or any MCP agent.
The agent sends a declarative app definition (tables, HTML templates, server scripts, routes);
Renoxium serves it with server-side rendering, one SQLite database per app stage, and a log
the agent can read to fix its own mistakes.

Plan: [docs/plans/2026-11-15-renoxium-platform-v1.md](docs/plans/2026-11-15-renoxium-platform-v1.md)

## Status: thin slice

Working today:

- Builder MCP endpoint at `/mcp` with `create_app`, `get_app`, `apply_change`, `publish` (also rollback) and `get_logs`.
- Preview and live stages per app. Preview runs on a snapshot of live data; publish backs up live data first.
- Additive schema migrations, seed data.
- Server scripts in goja with a 2 s limit, no filesystem, network or timers, and a SQLite authorizer that allows data statements only.
- Templates with any HTML/CSS/browser JavaScript, auto-escaped data.
- One log per app: requests, script errors with stack traces, script console output, and browser errors reported by an injected snippet, linked by request ID.

Not built yet (see the plan): OAuth and per-user ownership (the builder endpoint uses one shared
bearer token), script worker processes with OS memory limits (scripts run in-process today),
admin panel, per-app MCP, Stripe, WhatsApp, custom domains, quotas.

## Run

```bash
RENOXIUM_MCP_TOKEN=devtoken go run ./cmd/renoxium -addr :8080 -data data
```

Apps are served on `<app>.localhost:8080` and previews on `<app>--preview.localhost:8080`.
In production use `-apps-domain renoxium.app -scheme https` behind a TLS terminator.

`examples/catalog.json` is a complete small perfume shop (catalog, product page, order form, JSON API).

## Test

```bash
go test ./...
```

No cgo: the SQLite driver and the JavaScript engine are pure Go.
