# WhatsApp ordering: build the roadmap's first item

You are working in the Renoxium repository (Go monolith, one binary, SQLite per app). Renoxium builds and hosts
websites for UAE small businesses (shops, salons, home services, tours, agencies, brokers). Your task is the
**first item of `docs/roadmap.md`: WhatsApp ordering**. It is a key feature of the product: a customer messages the
business's own WhatsApp number, gets answers from the app's real data, and ends with a confirmed order or booking
(and a payment link when they pay by card). The order lands in the same admin panel and calendar as a website order.

Read `docs/roadmap.md` section 1 first: it holds the goal, the "done when" list, the approach and the risks. This
prompt adds what you need to know about the code and how to work.

## How to work

1. **Plan first, then ask.** Read the code listed below, then write `docs/plans/<today>-whatsapp-ordering.md`
   in the style of `docs/plans/2026-11-22-self-hosted-app.md` (goal, done when, what exists, what is missing, scope
   in/out, approach, risks, verification, order of work). Then ask the owner the decisions only they can make (see
   "Decisions to ask"), as multiple choice with your recommendation first. Write their answers into the plan.
2. **Build in several pull requests**, in the plan's order of work, each one useful on its own and green in CI.
   Branch from the latest `main`. Do not merge: the owner merges.
3. **Everything works without Meta.** Meta's Tech Provider approval and Embedded Signup are not available yet. Build
   against the WhatsApp Cloud API shape, and test with the fake in `e2e/fakes.mjs` (`/meta/...` already fakes sending)
   and Go test doubles. Real Meta onboarding becomes a small last step behind a configuration switch.
4. **Tests are part of every PR:** Go tests (`go vet ./... && go test ./...`) and the Playwright e2e suite
   (`e2e/run.sh`, see `e2e/README.md`). Add an e2e test that walks a full chat: a customer browses, orders, pays with
   the fake Stripe, and the owner sees the conversation next to the order and takes over.

## What exists today (read these)

- `internal/whatsapp/whatsapp.go`: Cloud API client that sends **outbound templates only** (sign-in codes,
  owner notices), `Normalize` for UAE numbers, error hints for Meta's error codes. Renoxium sends from **its own**
  number today. Configuration is in `cmd/renoxium/whatsapp.go` and `cmd/renoxium/main.go` (env `WHATSAPP_*`).
- `internal/platform/notify.go`, `phone.go`, `auth.go`, `authpages.go`: owner notices and visitor sign-in by code.
- **Script hooks**, the pattern to copy for `on_whatsapp`: `on_payment` and `on_admin_change` in
  `internal/appdef/appdef.go` (field, schema text, validation) and `internal/platform/hooks.go`, `payments.go`,
  `admin.go` (how a hook script runs with `req.body`). Scripts run in goja (`internal/sandbox`), synchronous
  `db.query`, `handle(req)` returning `{template, data}` or a JSON body.
- **Payments:** `payments.checkout` in scripts, `internal/platform/payments.go`, Stripe Connect per app
  (`payment_accounts`), the self-hosted direct mode (`stripe.DirectAccount`). Reuse the checkout link for card
  payments; cash on delivery and pay at the venue need no link.
- **Secrets per app, encrypted:** `internal/platform/fetch.go` (`SetSecret`, `secretKey`). Store each app's
  WhatsApp number ID and access token the same way.
- **Quotas and metering:** `internal/platform/limits.go` (`DailyLimits.WhatsAppPerApp`, `WhatsApp`),
  `meter.go` (the `whatsapp` counter), `usage.go`, `plans.go`. Every inbound reply and outbound message must count.
- **Public MCP tools of an app** (customers' AI assistants find products and free times): `internal/platform/appmcp.go`.
  The same read functions are a good base for the chat's answers.
- **Admin panel and console:** `internal/web/console.go`, `ownershell.go`, `web.go` (templates), `internal/platform/admin.go`.
  The owner inbox belongs in the app's admin panel.
- **Templates:** `examples/store-uae.json`, `booking-uae.json`, `home-service-uae.json`, `reservation-uae.json`
  (each has tables, scripts, lib, seed, texts in English and Arabic). Examples' tests are in `internal/platform/*_example_test.go`
  and `examples/`. The agent-facing reference text for definitions is the `reference` const in
  `internal/mcpserver/mcpserver.go`: document any new script API (`whatsapp.reply`, `on_whatsapp`) there.
- **Visitor data rules:** `internal/tenantdb` (scrubbed export, PDPL). Conversations are personal data: include them
  in export and deletion, with a retention period.
- **Deployment docs:** `docs/deploy-ubuntu.md` (WhatsApp sign-in section), `docs/self-host.md` (self-hosted owners
  use their own WhatsApp account: keep that path working).

## The approach from the roadmap, made concrete

1. **Merchant numbers.** One WhatsApp number per app, owned by the business. Admin page "WhatsApp" to connect it:
   for now by entering number ID and access token (encrypted), later by Meta Embedded Signup (behind config).
   Show the connection state and the last error in Meta's words with the existing hints.
2. **Inbound webhook.** One endpoint for all apps (e.g. `/whatsapp/webhook` on the main site): verify Meta's
   `X-Hub-Signature-256` with the app secret, answer the `hub.challenge` GET, route by `phone_number_id` to the app,
   store each message (dedupe by message id; Meta retries), and run the app's `on_whatsapp` script with the message.
   Return 200 fast; run scripts off the request path with the existing per-owner script limits.
3. **Script API.** `whatsapp.reply({to, text})`, plus interactive `buttons` (up to 3) and `list` (sections, up to
   10 rows) messages. Only inside the 24-hour customer service window; outside it, only approved templates. Enforce
   this in the platform, not in the scripts. Keep per-conversation state available to the script (a small JSON per
   conversation is enough; tables of the app hold the real data).
4. **Ready-made flows in the four templates**, without any AI: menu → browse or pick a service → options and quantity
   or a free time → delivery area or address → confirm → payment link or cash/pay-at-venue → confirmation with the
   order number. Prices, delivery fee, free-delivery threshold, VAT and free times must be **the same functions the
   website uses** (see `lib` in each example and the consistency tests: shown price == charged price). English and Arabic,
   following the customer's language.
5. **Owner inbox** in the admin panel: conversations list, a conversation with its linked orders/bookings, a reply box,
   and a "take over" switch that stops automatic replies for that conversation (and "hand back").
6. **Free-text understanding with an AI model** is optional and later (roadmap step 4): leave a hook for it, do not
   build it unless the owner says so.

## Rules

- Never send a message outside WhatsApp's rules. Never act on payment without the customer's confirmation in the chat.
- Rate-limit per sender number; ignore and log abuse. No government ID numbers or documents asked in any flow.
- Preview stage: the admin preview must be testable without a real number (simulate a chat from the console, the same
  way checkout is simulated in preview).
- Keep the existing sign-in codes and owner notices working unchanged.
- Follow the repository's conventions: plain English texts and docs, English + Arabic texts in templates with RTL,
  mobile first, logical CSS properties.
- Commit only on your own branch, never commit secrets.

## Decisions to ask the owner (with your recommendation)

- Whose number sends: only each business's own number (recommended), or also a shared Renoxium number as a start.
- Which plans get WhatsApp ordering (e.g. Pro and Business only, or Free with a small quota).
- Who pays Meta's per-conversation price: included in the plan's quota, or billed on.
- Which templates first (recommended: `store-uae` and `booking-uae`, then the other two).
- Retention period of conversations (e.g. 12 months).
- Whether the AI free-text step is in scope now (recommended: later).

## Done when

The "done when" list in `docs/roadmap.md` section 1, plus: the plan document exists with the owner's decisions, the
reference in `mcpserver.go` documents the new API, `docs/deploy-ubuntu.md` and `docs/self-host.md` say how to set up
the webhook and a number, and CI is green on every PR.
