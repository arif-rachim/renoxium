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
- Connection help at `/connect` and `/llms.txt`, written so an assistant that reads them tells the user how to add the connector. A browser or fetch-tool visit to `/mcp` gets the same help with its 401, while MCP clients still get the plain OAuth challenge.
- Builder MCP endpoint at `/mcp`, OAuth-protected, with `list_apps`, `create_app`, `get_app`, `apply_change`, `publish` (also rollback), `upload_file` and `get_logs`. Every tool acts only on apps the signed-in user owns.
- Preview and live stages per app. Preview runs on a snapshot of live data; publish backs up live data first.
- Additive schema migrations, seed data.
- Server scripts in goja, run in a pool of separate worker processes: 2 s time limit, a heap watchdog inside each worker (64 MiB), and the parent kills any worker over 256 MiB resident and starts a new one. At most 2 scripts of one app run at once, so one app cannot occupy every worker. No filesystem, network or timers; a SQLite authorizer allows data statements only.
- Admin panel on the Renoxium site at `/apps/<app>`, generated from the live schema: tables with row counts, recent errors, a searchable and paginated list per table, and add/edit/delete forms whose inputs follow column types. Owner only; every change is written to the app log.
- Image uploads through the `upload_file` tool or the admin panel, stored on local disk under the SHA-256 of their content: identical images are stored once, even across apps, and uploading again returns the same URL. Each app serves only the files recorded for it, at `/_rx/files/<hash>.<ext>` with immutable cache headers. Columns of type `image` hold these URLs.
- Templates with any HTML/CSS/browser JavaScript, auto-escaped data.
- One log per app: requests, script errors with stack traces, script console output, and browser errors reported by an injected snippet, linked by request ID.

Not built yet (see the plan): email verification and password reset, staff accounts and column labels in the admin panel, per-app MCP, Stripe, WhatsApp, custom domains, quotas.

## Run

```bash
go run ./cmd/renoxium -addr :8080 -data data
```

Open `http://localhost:8080` to create an account, then add `http://localhost:8080/mcp` as a connector in an
MCP client that supports OAuth (ChatGPT and Claude need a public HTTPS address, see below).

Apps are served on `<app>.localhost:8080` and previews on `<app>--preview.localhost:8080`.
In production Renoxium runs behind Cloudflare Tunnel; see [deploy/README.md](deploy/README.md).
The connector address for ChatGPT and Claude is then `https://renoxium.com/mcp`.

`examples/catalog.json` is a complete small perfume shop (catalog, product page, order form, JSON API).

## Test

```bash
go test ./...
```

No cgo: the SQLite driver and the JavaScript engine are pure Go.
