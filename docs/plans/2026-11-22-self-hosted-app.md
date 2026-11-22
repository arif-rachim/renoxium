# Run one Renoxium app on the owner's own server

**Goal:** an owner can take one app out of Renoxium (definition, data and photos) and run it on a server they control, with the same pages and admin panel, without Renoxium's account system.

**Status:** v1 built: export with photos and licence, `import`, `-self-hosted`, `apply`, `rollback`, `admin-password`, and the guide in `docs/self-host.md`. Commands differ from the first sketch below: `import EXPORT.zip ADMIN_EMAIL [DOMAIN]` creates the admin account, and the server is started with `-self-hosted`.

**Done when:**
- An owner downloads one file from the console that holds everything the app needs: definition, live data, uploaded photos and visitors' private files.
- The same `renoxium` binary, started in a single-app mode with that file, serves the site on the owner's domain: public pages, visitor sign-in, the admin panel, scheduled jobs and image resizing.
- Card payments, email and (optionally) WhatsApp sign-in codes work once the owner enters their own keys.
- A written guide takes an owner with a fresh Ubuntu server from the export to a working site.

## What exists today
- **An app is three things:** a definition (one JSON document), one SQLite database per stage, and content-addressed files on disk. The platform is one Go binary with no required outside service.
- **Export per app** (`Platform.ExportApp`, the console's app page): a zip with `app.json`, the newest definition, a scrubbed copy of the live database, visitors' private files, and `files.json`, which lists the uploaded photos but does not contain them.
- **The export is scrubbed** (`tenantdb.ScrubForExport`): visitors' password hashes, sessions, emailed links and sign-in codes are removed. Visitors keep their rows.
- **The whole platform can already be installed** on a server (`docs/deploy-ubuntu.md`), but only as the full product: accounts, console, gallery, billing and operator pages.

## What is missing
- Photos in the export.
- A mode of the binary that serves one app without accounts, billing, the gallery or the operator pages.
- A way to sign in to the admin panel without a Renoxium account.
- Settings for the outside services the app uses, held by the owner instead of Renoxium.
- A licence that says who may run the binary.

## What depends on Renoxium today
| Part of an app | Today | On the owner's server |
|---|---|---|
| Pages, scripts, admin panel, jobs, image sizes | In the binary | Unchanged |
| Card payments | The owner's Stripe account, connected through Renoxium's platform account | The owner's own Stripe secret key and webhook secret |
| Email (receipts, sign-in links, notices) | Renoxium's Resend account and sender domain | The owner's own sender (Resend key, or SMTP if added) |
| WhatsApp sign-in codes and order notices | Renoxium's WhatsApp Business account | The owner's own WhatsApp Business API account, or none (email sign-in only) |
| Domain and HTTPS | Renoxium's domains, Cloudflare for custom domains | The owner's reverse proxy (Caddy or nginx) and certificate |
| Changing the site with ChatGPT or Claude | Renoxium's MCP server and sign-in | Not available in single-app mode (see Decisions) |
| Outside API calls from scripts (`http.fetch`) | Hosts and keys the owner approved in the console | A settings file with the same hosts and keys |
| Visitors' passwords | Stored hashed; removed from exports | Visitors set a new password by emailed link after the move |

## Decisions (made by the owner on 2026-11-22)
1. **Who may self-host:** paid plans only. Export of data stays free for everyone; the single-app mode runs only with an export from a Pro or Business account.
2. **Licence of the binary:** source-available. It may be run for one's own business and may not be offered as a hosting service.
3. **Support:** none for self-hosted sites beyond the written guide.
4. **Editing with an AI assistant after the move:** not available in single-app mode. The owner changes the definition file by hand, or stays on Renoxium.
5. **Build:** all of v1, in several pull requests, in the order of work below.

## Scope
### In (v1)
- The export holds the photos.
- Single-app mode of the binary, with its own admin sign-in.
- Settings for Stripe, email and WhatsApp from the environment.
- Import on start: the binary reads the export once and creates its data directory.
- A guide for Ubuntu with Caddy.
- Tests that an exported demo app serves the same pages in single-app mode.

### Out (later or never)
- Moving an app back into Renoxium from a self-hosted copy.
- Several apps in one self-hosted process.
- The console, gallery, billing, operator pages and growth reports in self-hosted mode.
- Editing through an AI assistant (decision 4).
- Automatic updates of the binary.
- SMTP, unless Resend proves to be a real obstacle for owners.

## Approach
1. **Complete export.** `ExportApp` writes each uploaded file into the zip under `files/<hash>.<ext>`, next to `files.json`. The export limit per account stays; a size note on the console's page says how large the download is. This step is useful on its own as a backup and decides nothing about self-hosting.
2. **Import.** A new command, `renoxium import <export.zip> -data <dir>`, creates a data directory with the app, its newest definition as live, the live database and the files. It refuses a directory that already holds an app.
3. **Single-app mode.** `renoxium serve-app -data <dir> -base-url https://shop.example`: the app is served at the base URL itself instead of a subdomain, and nothing of the Renoxium site is registered. The request path to an app (`Platform` serving, auth pages, files, jobs) is reused as it is; what changes is routing and where the owner comes from.
4. **Admin sign-in.** The admin panel today requires a Renoxium account that owns the app. In single-app mode the owner is one local account: `renoxium set-admin -data <dir> -email …` sets its email and password, and the existing admin pages check that account. Staff accounts from the export are kept and get a password the same way.
5. **Services.** Stripe: the owner's secret key charges directly on their account, without the platform fee and without Connect; the webhook secret comes from the environment. Email: `RESEND_API_KEY` and a sender address. WhatsApp: optional; without it the sign-in pages offer email only and order notices go by email.
6. **Preview.** Single-app mode has only the live stage. A changed definition is checked with the same checks as today and applied with `renoxium apply <definition.json> -data <dir>`, which keeps the previous version for a rollback.
7. **Licence check:** the export carries a signed note of the account's plan and date; `serve-app` refuses an export without one. It is checked offline, so a self-hosted site never depends on Renoxium being reachable.
8. **Guide.** `docs/self-host.md`: server, binary, import, admin account, Caddy, Stripe webhook, sender domain, backups (copy the data directory), updates.
9. **Tests.** Export a demo app, import it, serve it, and compare its pages with the hosted ones; sign in to the admin panel; run a job; pay with the fake Stripe; confirm that no Renoxium address is called.

## Risks and how we handle them
- **Income:** owners leave with their site. → Decision 1; export of data stays free either way, because that is the trust promise.
- **Support load from servers we do not run.** → Decision 3, stated on the download page and in the guide.
- **Security updates:** a self-hosted site stays on an old binary. → The guide says how to update; the admin panel shows the running version and its date.
- **Visitors' accounts:** passwords are not exported. → Stated on the export page; the sign-in page of a freshly imported site says "set a new password" with the emailed link.
- **Stripe differences:** direct charges instead of Connect change refunds and fee lines. → One code path with the account as a parameter where possible; the payment tests run in both modes.
- **Two modes to keep working.** → Single-app mode reuses the app-serving code and has its own end-to-end test in CI, so a change that breaks it fails the build.
- **Licence check as a weak lock.** → It is a statement of terms, not protection; no effort beyond a signature check.

## Verification
- Automated: Go tests for the complete export and the import; an end-to-end test that serves an imported demo in single-app mode and walks one order, one admin change and one job.
- Manual: follow the guide on a fresh Ubuntu server with a real domain, a Stripe test key and a Resend sender; place an order as a visitor and see it in the admin panel.

## Order of work
1. Complete export (photos in the zip). Small, independent, useful as a backup.
2. Import and single-app serving of public pages.
3. Admin sign-in.
4. Stripe, email and WhatsApp settings.
5. Apply and rollback of a definition.
6. Licence check, guide and end-to-end test.

## Open questions deferred
- Whether a self-hosted site may show "Made with Renoxium" or must.
- Price of a self-host licence, if it is sold separately from the plans.
- Whether to offer the full platform (console and AI editing) as a white-label product for agencies; that is a different product from this plan.
