# Renoxium

Build and host web apps for small businesses from a chat with ChatGPT, Claude or any MCP agent.
The agent sends a declarative app definition (tables, HTML templates, server scripts, routes);
Renoxium serves it with server-side rendering, one SQLite database per app stage, and a log
the agent can read to fix its own mistakes.

Plan: [docs/plans/2026-11-15-renoxium-platform-v1.md](docs/plans/2026-11-15-renoxium-platform-v1.md)

## Status: thin slice

Working today:

- Accounts on the Renoxium site (email and password, argon2id), a dashboard listing your apps, email confirmation, and password reset by emailed link (sent through Resend; a reset signs out every session and agent token).
- OAuth 2.1 for agents, as ChatGPT and Claude expect: protected resource and authorization server metadata, dynamic client registration, PKCE (S256 only), consent screen, rotating refresh tokens, revocation on code replay.
- Connection help at `/connect` and `/llms.txt`, written so an assistant that reads them tells the user how to add the connector. A browser or fetch-tool visit to `/mcp` gets the same help with its 401, while MCP clients still get the plain OAuth challenge.
- Builder MCP endpoint at `/mcp`, OAuth-protected, with `list_apps`, `create_app`, `get_app`, `get_example`, `apply_change`, `patch_app`, `test_request`, `publish` (also rollback), `upload_file` and `get_logs`. `get_example` returns complete working definitions from `examples/`: `store-uae` (a finished UAE online store: catalog, cart, card payment or cash on delivery, AED with VAT, private orders, English and Arabic, with an order-ahead mode for made-to-order goods), `booking-uae` (appointments and classes), `home-service-uae` (bookings by arrival window with crew dispatch and repeat visits), `reservation-uae` (tour departures and date-range rentals), `quote-portal-uae` (quotes, deposits and a client portal), `listings-uae` (property listings with permit numbers and leads), `catalog` and a CRUD task list; `patch_app` changes one template, script, route list or table without resending the whole definition; `test_request` sends GET or POST requests to preview (GET only to live) and returns the response with that request's log, so agents can verify forms and parameter routes themselves. Every tool acts only on apps the signed-in user owns.
- Preview and live stages per app. Preview runs on a snapshot of live data; publish backs up live data first (the newest 3 backups are kept). Previews are private: the owner opens them from `/apps/<app>/preview` on the Renoxium site, which unlocks the preview in that browser for 12 hours. Publishing needs a confirmed email.
- Additive schema migrations, seed data.
- Server scripts in goja, run in a pool of separate worker processes: 2 s time limit, a heap watchdog inside each worker (64 MiB), and the parent kills any worker over 256 MiB resident and starts a new one. At most 2 scripts of one app run at once, so one app cannot occupy every worker. No filesystem, network or timers; a SQLite authorizer allows data statements only.
- Admin panel on the Renoxium site at `/apps/<app>`, generated from the live schema: tables with row counts, recent errors, a searchable and paginated list per table, and add/edit/delete forms whose inputs follow column types. Owner only; every change is written to the app log.
- Image uploads through the `upload_file` tool or the admin panel, stored on local disk under the SHA-256 of their content: identical images are stored once, even across apps, and uploading again returns the same URL. Each app serves only the files recorded for it, at `/_rx/files/<hash>.<ext>` with immutable cache headers. Columns of type `image` hold these URLs.
- Sign-in for an app's own visitors, built in: sign-up, sign-in, sign-out, email confirmation and password reset pages at `/_rx/auth/*` on the app's domain, restylable with `auth_*` templates. Visitors can also sign in with a 6-digit WhatsApp code (WhatsApp Cloud API), which creates the account on first use. Routes marked `"auth": "user"` require a signed-in visitor, and scripts get `req.user`. Accounts live in `rx_` tables in the app database that scripts cannot read or write (enforced by the SQLite authorizer). Live sends emails through Resend; preview writes them, links included, to the app log.
- Card payments through Stripe Connect: owners connect their own Stripe account from the admin panel (created with the full Stripe Dashboard, merchant-paid Stripe fees and Stripe-covered losses); scripts call `payments.checkout` for a direct charge on that account and `payments.get` to read status; Stripe webhooks (signature-checked) run the app's `on_payment` script once per status change. Previews simulate checkout, so agents can test the whole flow without money.
- Renoxium plans billed on the platform's own Stripe account: Free (2% application fee on card payments, a "Made with Renoxium" badge on live pages, 1 app), Pro (AED 99/month, 3 apps) and Business (AED 299/month, 10 apps). Owners subscribe and manage billing at `/billing` (Stripe Checkout and Customer Portal); a signature-checked billing webhook keeps plans in sync, and `renoxium plan EMAIL PLAN` grants a plan without payment.
- Templates with any HTML/CSS/browser JavaScript, auto-escaped data.
- One log per app: requests, script errors with stack traces, script console output, and browser errors reported by an injected snippet, linked by request ID.

- The admin panel lists an app's visitor accounts (email, WhatsApp number, name, confirmed) with search and delete; visitor emails reply to the owner's confirmed address.

- Abuse limits: 10 apps per account, 2000 files per app, image storage per account by plan (200 MB Free, 2 GB Pro, 10 GB Business), the newest 50 versions per app; 300 requests per minute per visitor address (IPv6 grouped per /64); daily caps on visitor emails (300 per app, 3000 in all) and WhatsApp codes (100 per app, 1000 in all); at most 10 code guesses per number per hour.
- Platform-side limits on tenant code: pages at most 4 MB, script statements at most 2 s with SQLite length and depth limits, script messages at most 8 MB, at most 4 password hashes at once.
- Publishing from an agent needs the owner's confirmation: `publish` returns a link to `/apps/<app>/publish`, where the owner (signed in) chooses Publish. Text an agent reads in logs or pages cannot publish by itself.
- One account runs at most half of the script workers at once, across all its apps. Script workers lock themselves down with Landlock: no files and no TCP, even if the interpreter had a bug.
- Anyone can report an app at `/abuse`; the operator takes it offline with `renoxium suspend APP "reason"`.
- Sign-in limits are per address and per account from that address, with a much higher per-account cap over all addresses, so strangers cannot lock someone out.
- Browser protections: POSTs from other origins are refused on app hosts and on the site (other apps share the renoxium.app site), cookies use the `__Host-` prefix on HTTPS, and HSTS. `/_rx/log` only accepts reports for pages the app served.

- Store owners on Pro and Business can add their own domain (Cloudflare for SaaS custom hostnames); once active it becomes the app's live address.

- Owners can get `notify.owner` notices and card payments on WhatsApp (a Utility template), after confirming their number with a code; notices count towards the monthly WhatsApp messages.
- Images resize on request (`?w=480`), and the admin panel shows table and column labels, help text and drop-down options from the app definition.

- Monitoring: every 5 minutes the server checks disk space, backup age (stamps written by `backup.sh`) and error spikes, emails the operators, and shows the state on `/operator`; owners get an email on their live app's first error, then at most daily. `/healthz` answers 503 when the disk is almost full.
- The store template has product options (sizes, colours) with their own price and stock.

- External APIs: scripts call `http.fetch({url, method, headers, body, secret})` to HTTPS hosts listed in the definition's `hosts` and allowed by the owner at `/apps/<app>/connections`. API keys (`secrets`) are entered by the owner, stored encrypted (AES-GCM, key derived from `preview.key`), bound to one host, added by the platform and never visible to scripts or agents; keys echoed back are masked. The request runs in the server process (workers stay without network), private and local addresses are refused after DNS, redirects are not followed, and waiting does not count towards the script time limit. Limits: 5 calls per run, 10 s each, 60 a minute per app, monthly quota by plan (1,000 / 20,000 / 100,000).
- Realtime: scripts call `realtime.publish({channel, data})` and pages listen with `EventSource('/_rx/live/<channel>')` (server-sent events served by Go, so a waiting browser costs a goroutine, not a script worker). Channels are private unless named `public:...`, with tokens from `realtime.token(channel)`; each channel keeps its last message; connections are limited per plan. The `delivery-tracking` example has a courier page sharing GPS and a live customer map.
- Scheduled jobs (`jobs` in the definition: `every: "15m"` or `daily_at: "21:00"` in Asia/Dubai by default) run scripts on live apps, with their last run stored so restarts neither skip nor repeat; agents run them on preview with `test_request {job}`. The store template sends the owner a daily sales and low-stock report.
- Apps can define public MCP tools (`tools` in the definition, backed by scripts) served at `<app>/mcp`, so shoppers can add a shop to ChatGPT or Claude and ask about its products. The store template has search_products, get_product and store_info.

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
