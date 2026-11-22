# WhatsApp conversations: design

Date: 2026-11-22. Status: the product owner took every recommendation (section 8). Phase 1 is
implemented; phases 2 to 4 are designs only. Choices made in phase 1:
- Free plan: 30 customer messages a month as a trial.
- Opt-in: the checkbox only. WhatsApp sign-in does not count as consent.
- Templates: English only for now.
- Retention: 180 days.
- Pause: when more than 5% of at least 50 messages in 7 days are opted out.
- Inbound: a fixed answer once a day.

What phase 1 changed from the design:
- One ticked box is consent for one number per request (`reason: "one_number"`).
- A server without `WHATSAPP_APP_SECRET` keeps customer messages off, because opt-outs could not
  arrive.
- `whatsapp.send` refuses `name_review` for business names that look like banks, government
  offices, couriers or WhatsApp.

Goal: Renoxium becomes a conversation-focused app with automation. Customers and owners talk on
WhatsApp, and the app automates the conversation: confirmations, reminders, status updates,
automatic replies, and hand-over to the owner. A site with a chat button is not enough.

## 0. What exists today (read from the code)

| Use | Code | Sender | Notes |
| --- | --- | --- | --- |
| Visitor sign-in codes | `internal/platform/phone.go`, `internal/whatsapp/whatsapp.go` (`CloudAPI.SendCode`) | platform number, Authentication template | Preview writes the code to the app log and the demo inbox (`demoDeliver`). Limits: 3 per number per 15 min, 10 per IP per hour, daily per app and per server (`allowDurable`), monthly per account (`WhatsAppCodesPerPlan` 50/500/2000, in `usage.go`). Numbers without an account get only a share of the month. |
| Owner notices from `notify.owner` | `alerts.go`, `notify.go`, `web/alerts.go` | platform number, Utility template `owner_notice` with 2 parameters | The number is confirmed with a code. Same monthly counter (`usage` kind `whatsapp`), plus 100 per app per day. |
| Chat with the business | `wa.me/{{.wa}}` links in every `examples/*.json` | none; the customer's own app | Renoxium never sees the chat. |

Other facts that shape the design:
- **Send only, no receive.** `whatsapp.CloudAPI` only sends templates. There is no webhook handler,
  no app secret setting, and no stored messages.
- **Script host calls** are a `switch` in `scriptHost.Call` (`serve.go`): `email.send`,
  `notify.owner`, `payments.*`, `http.fetch`, `realtime.*`, `files.*`. A new `whatsapp.*` family
  fits there.
- **Hooks**: `on_payment` (`payments.go` `runHook`) and `on_admin_change` (`hooks.go`). Both name
  a script in the definition, get `req.body`, take a script slot (`acquireSlot`), log with a
  request ID, and queue nested hooks (`hookQueue`). `on_message` copies this.
- **Jobs** (`jobs.go`) run on live only. Their last run is stored, and they are queued per owner.
  They are the place for reminders ("send a reminder 24 h before"). `try_scheduled_jobs` runs them
  on preview.
- **Visitor accounts** (`rx_users`, `tenantdb.go`) have a unique `phone`, which is verified when
  the visitor signed in with a WhatsApp code. `rx_` tables are invisible to scripts.
- **Durable limits** (`limits.go`): `rate_windows` with windows of up to one day. Monthly counting
  goes in `usage`.
- **E2E**: `e2e/fakes.mjs` fakes `POST /meta/.../messages` and records templates in
  `state.whatsapp`. It has no inbound side yet.

## 1. Meta rules that constrain the design (checked 2026-11-22)

Sources were read on 2026-11-22 from developers.facebook.com and whatsappbusiness.com. Most pages
show no "last updated" date. **[unverified]** marks what I could not confirm on an official page.
Re-check pricing before setting plan prices.

**Customer service window.**
- A user message (or call) opens a 24-hour window, and every new user message restarts it.
- Inside the window a business may send free-form messages: text, media, reply buttons (up to 3),
  lists, a CTA URL.
- Outside the window it may send only approved templates. Free-form messages "cannot be used to
  reach out to users".
  (developers.facebook.com/docs/whatsapp/pricing; …/whatsapp/messages/send-messages)
- **The window belongs to the pair (business phone number, user).** On a shared platform number,
  a reply to app A opens the window for app B too. Renoxium must track its own window per (app,
  customer) and refuse free-form messages from an app the customer did not write to.

**Templates.**
- There are three categories: marketing, utility and authentication.
- Utility must be non-promotional and either specific to the user or essential. Mixed content counts
  as marketing.
- Since 2025-04-09, Meta approves a "utility" template it judges to be marketing as MARKETING, and
  may recategorise templates later (`template_category_update` webhook). Repeated misuse can restrict
  utility sending.
- Review is automatic and "can take up to 24 hours". Parameters are positional `{{1}}` or named.
- Consequence: a template must be specific (booking confirmed, reminder, order status). A
  catch-all template made mostly of parameters is the pattern Meta rejects or recategorises.
  **[unverified]**: the exact rule on the ratio of parameters to fixed text.

**Pricing.**
- Pricing is per delivered message since 2025-07-01, no longer per conversation.
- The developers pricing page (which says it reflects changes effective 2026-10-01) states:
  - Utility messages sent inside an open window are charged again from 2026-10-01. Their pricing
    type changed from `free_customer_service` to `regular`.
  - Service (free-form) messages: 1,000 free per business number per month, then charged.
  - The whatsappbusiness.com pricing page still says service messages and utility replies are free.
    It is stale. Follow the developers page.
- The UAE (+971) has its own rate-card row. AED pricing exists since 2026-04-01.
- Volume tiers apply to utility and authentication messages, counted per business portfolio.
- **[unverified]**: the actual UAE amounts per category (the rate card CSV was not read) and the
  volume-tier thresholds. They must be read from the rate card before plan prices are set.

**Opt-in and opt-out** (…/overview/getting-opt-in; Business Messaging Policy, updated 2026-09-23).
- A business may contact a person only if (a) the person gave their number and (b) the business has
  opt-in.
- The opt-in must "clearly state that a person is opting in to receive communication from the
  business" and "clearly state the business's name".
- A website opt-in is allowed, and the method is up to the business.
- Opt-out requests must be honoured.
- **[unverified]**: whether a website checkbox must be unticked by default. We will make it unticked
  anyway.

**Automation and human hand-over.**
- Policy: automation is allowed inside the window, "but must also have available prompt, clear,
  and direct escalation paths" to a human.
- Hand-over to the owner is therefore required, not a nice-to-have.
- **[unverified]**: the 2026 rule on "general-purpose AI assistants". The pricing summary mentioned
  an AI Providers policy page dated 2026-05-12 that I did not read. Business-specific automated
  replies are not affected in any reading I saw.

**Messaging limits** (…/messaging-limits).
- Limits are per business portfolio, shared by all its numbers. They count unique users reached
  outside a window in a moving 24 hours.
- A new portfolio starts at 250. It reaches 2,000 by business verification (or 2,000 delivered
  messages in 30 days), then rises automatically to 10k, 100k and unlimited with good quality.
- **On the platform number, all tenants share Renoxium's limit and quality rating.** One spamming
  tenant lowers it for everyone.

**Webhooks** (…/graph-api/webhooks/getting-started; …/cloud-api/guides/set-up-webhooks).
- Verification is a GET with `hub.mode=subscribe`, `hub.verify_token` and `hub.challenge`.
- Every POST is signed: `X-Hub-Signature-256: sha256=<HMAC-SHA256(raw body, app secret)>`.
- Answer 200. Meta retries for up to 7 days. Delivery order is not guaranteed and duplicates
  happen, so de-duplicate by `wamid`. Payloads can be up to 3 MB.
- Routing: `entry[].id` is the WABA, and `changes[].value.metadata.phone_number_id` is the business
  number.
- There are per-WABA and per-number callback overrides. **[unverified]**: their details.
- Inbound messages: `messages[]` (from, id, timestamp, type, text/button/interactive) and
  `contacts[]` (wa_id, profile.name, user_id).
- Statuses: `statuses[]` (sent, delivered, read, failed) carry a `pricing` object (category, type),
  and `failed` ones carry `errors[]`.

**Business-scoped user IDs (BSUID) and usernames.**
- Since April 2026 webhooks carry `user_id` / `from_user_id`. Users may adopt usernames (claims
  opened 2026-06-29).
- For a user with a username, the phone number is left out of a webhook unless the business number
  exchanged a message with them in the last 30 days, or the user is in the contact book.
- "An integration that relies only on phone number fields will not be able to process messages
  from these users." Store `user_id` next to the phone from day one.
- **[unverified]**: the 2026-11-05 changelog on that page.

**Tech Provider, Embedded Signup and coexistence** (…/embedded-signup; …/solution-providers;
…/embedded-signup/onboarding-business-app-users).
- A Tech Provider needs App Review and Advanced access to `whatsapp_business_management` and
  `whatsapp_business_messaging`.
- Tech Providers have **no credit line**: each onboarded business adds its own payment method in
  Meta. Solution Partners bill customers directly, but becoming one is a much bigger step.
- In Embedded Signup the owner signs in with Facebook, picks or creates a business portfolio and a
  WhatsApp account, verifies the number and enters a display name. The provider gets `waba_id`,
  `phone_number_id` and a code it exchanges for a business token.
- By default a provider can onboard 10 new accounts per rolling 7 days. That rises to 200 after
  business verification, App Review and access verification.
- **Embedded Signup v2 and v3 were deprecated on 2026-10-15.** Build on v4 or later.
- **Coexistence**: a business keeps using the WhatsApp Business app on the same number, with history
  synced (180 days) and message echoes by webhook. It needs app version 2.24.17 or later.
  Throughput is fixed at 20 mps. Groups, broadcast lists and disappearing messages are unsupported,
  and companion devices are unlinked.
- **[unverified]**: whether coexistence is available in the UAE. No country list was found.
- **[unverified]**: the exact Tech Provider application steps.

## 2. Feature map, most valuable first

| # | Feature | Why it matters | Needs inbound? | Needs owner number? |
| --- | --- | --- | --- | --- |
| A | **Automatic messages to customers**: confirmation, reminder, status (booking confirmed, order out for delivery, quote ready) | Replaces the owner's manual "your booking is confirmed" chats, the work they do most. Fewer no-shows. | No (STOP handling only) | No |
| B | **Inbound messages handled by app scripts** (`on_message`): "1 to confirm, 2 to cancel", "status of order 12" | Turns one-way notices into conversations the app can act on. | Yes | No |
| C | **Automatic replies** (declarative: opening hours, away message, FAQ keywords) | Works without writing a script. Most owners only need this. | Yes | No |
| D | **Inbox and hand-over** in the admin panel: threads, the bot paused per thread, the owner replies, notice to the owner | Required by Meta policy for automation, and the owner's daily work surface. | Yes | No |
| E | **Owner's own number** (Embedded Signup, coexistence) | Customers chat with the shop's own name and number, and the owner keeps the WhatsApp Business app. | Yes | Yes |

A gives the most value for the least risk, and works on the platform number with templates only. B,
C and D share one piece of plumbing: the signed webhook, threads and the per-app window. E changes
who owns the sender, not the API: the script API below is the same on both paths.

## 3. Two ways to choose the sending number

### Path 1: Renoxium's platform number, with templates approved by Meta

Renoxium owns one WABA (later a few), already used for sign-in codes and owner notices. It sends
utility templates that name the business in a parameter.

Advantages:
- **Zero setup for the owner.** It works the moment the app is published, which fits non-technical
  owners.
- One payment method and one rate card, Renoxium's. The cost is predictable and resold within plan
  quotas.
- The existing code is reused: `CloudAPI.send`, the error hints, quota counting, the fake Meta in
  e2e.
- Renoxium controls the templates, so their wording is specific and they pass review once for all
  tenants.

Disadvantages:
- **The customer sees "Renoxium" (the platform's display name), not the business**, in the chat
  header. The business is named only in the text. Conversational features (B to D) feel less
  natural, and some customers may distrust it.
- **Shared fate.** All tenants share the messaging limit (250, then 2k, … unique users per day) and
  the quality rating. One abusive app can get the number restricted for every app, so strong
  safeguards (section 6) are needed.
- **Shared window and routing.** A reply must be routed to the right app. Section 4 covers this.
- Opt-in must name the business ("messages from Oud Al Noor, sent through Renoxium").
- **Risk [unverified]**: Meta could judge one number messaging for many businesses as impersonation
  or poor quality. Nothing I read forbids it, and Renoxium already does it for owner notices, but the
  scale is new.

### Path 2: the owner's number (WhatsApp Business API, Embedded Signup, Renoxium as Tech Provider)

Advantages:
- The customer chats with the shop's name and number, which is the real "conversation app" end
  state.
- With coexistence, the owner keeps chatting from the WhatsApp Business app on their phone, and
  Renoxium sees the messages (echoes) and automates them.
- Each owner has their own messaging limit and quality rating, so tenants are isolated.

Disadvantages (expensive for a non-technical owner):
- The owner needs a Facebook login and a Meta business portfolio, must accept Meta's terms, verify
  the number and get a display name approved, and **add their own payment method in Meta**.
  Business verification is needed to send to more than 250 users a day.
- Renoxium must first pass App Review and Advanced access, become a Tech Provider (with business
  verification of Renoxium itself), and stay on the current Embedded Signup version (v2 and v3 died
  on 2026-10-15). Onboarding is capped at 10 new accounts per 7 days until verified.
- Each owner has their own templates. Renoxium must create them in the owner's WABA through the API
  and wait for review per owner.
- Coexistence limits (no broadcast lists or groups, companion devices unlinked) will surprise
  owners. UAE availability is not confirmed.
- Renoxium stores a business token per owner. That is a secret per tenant, encrypted at rest and
  never visible to scripts.

### Recommendation

**Phase 1 uses the platform number**, for feature A only. In parallel, start the Meta paperwork for
Path 2 now: Renoxium's business verification, App Review and Tech Provider status. It takes weeks
and costs no code. Offer Path 2 as an option on Pro and Business plans once inbound (B to D) is
proven on the platform number. The script API is designed so that an app does not change when its
owner moves to their own number.

## 4. API shape

### 4.1 Definition

```json
"whatsapp": {
  "on_message": "wa_in",
  "auto_replies": [
    {"match": ["hours", "open", "timing"], "reply": "We are open 10:00-22:00 every day."},
    {"outside_hours": {"days": "sat-thu", "from": "10:00", "to": "22:00", "timezone": "Asia/Dubai"},
     "reply": "We are closed now and will reply in the morning."}
  ],
  "handoff_keywords": ["agent", "human", "owner"]
}
```

- `whatsapp` is optional. Without it an app can still call `whatsapp.send` (phase 1 needs no
  definition change).
- `on_message` (phase 2) names a script, as `on_payment` does. It is validated in `appdef` (the
  script must exist).
- `auto_replies` (phase 2) has at most 20 entries. Each `match` is a list of lowercase words
  matched on whole words, and each `reply` is at most 1,000 characters. The first match wins.
  Auto-replies run before `on_message`; if one matches, the script is not run.
- `handoff_keywords` (phase 3) pause the bot for the thread and notify the owner.
- The app's business name comes from the existing settings/brand name. It is never free text in the
  call, so an app cannot claim to be another business.

### 4.2 Script calls

**`whatsapp.send({to, kind, ref, text, when?, link?})`** (phase 1). It sends a template to a customer
from the platform number.
- `to`: a number in any form `whatsapp.Normalize` accepts.
- `kind`: one of `"confirmation"`, `"reminder"` or `"update"`. Each maps to one platform template
  (section 4.4). This is a closed list: scripts never name Meta templates.
- `ref`: the short reference the customer sees, such as `"Booking #12"` (at most 40 characters).
- `text`: one line, at most 200 characters. New lines are folded, as `NoticeText` does, and URLs are
  refused (section 6).
- `when`: for `reminder`, the formatted date and time (at most 60 characters).
- `link`: an optional path on the app itself, such as `/bookings/12`. The platform turns it into the
  template's URL button through a short redirect on the platform host. Absolute URLs are refused.
- Result: `{sent: true|false, id, reason?}`. `sent: false` without an exception for an expected
  refusal: `"no_consent"`, `"opted_out"`, `"quota"`, `"limit"` or `"not_live"`. An exception only
  for a programming error, such as bad arguments. Scripts thus need no try/catch for common cases.
- Preview never sends. It logs `not sent from preview: WhatsApp confirmation to +9715…4567:
  "<rendered text>"`, puts the message in the demo inbox, and returns `{sent: false, preview: true,
  id}`.

**`whatsapp.consent({phone})`** (phase 1). It records the customer's opt-in, for this app only.
- It succeeds only inside a **visitor request** (a route script), not in a job, hook or tool, and
  only when the submitted form contains the platform's own opt-in checkbox, checked.
  - The checkbox is rendered by the template helper `{{rx_whatsapp_optin}}`. Its label is fixed by
    the platform and names the business: "Send me updates about this booking on WhatsApp from <Business
    name> (via Renoxium). Reply STOP any time." It is unticked by default.
  - The checkbox has a hidden HMAC field bound to the app and the stage, so a script cannot invent
    consent and a form on another site cannot post it.
- It returns `{consented: true}`, or `{consented: false, reason: "box_not_ticked"}`.
- A visitor signed in with a WhatsApp code whose `phone` matches is recorded as a verified number,
  with the same consent rule.
- Stored in the platform table `rx_wa_consent (phone, user_id, source, wording_version, at,
  opted_out_at)` in the tenant admin database. Scripts cannot read it, but
  `whatsapp.status({phone})` returns `{consented, opted_out}`.

**`whatsapp.reply({to, text, buttons?})`** (phase 2). It sends free-form text inside the window.
- It succeeds only if this customer wrote to **this app's thread** in the last 24 hours (Renoxium's
  own window, section 1).
- `buttons` holds up to 3 `{id, title}` (titles of at most 20 characters).
- Outside the window it returns `{sent: false, reason: "window_closed"}`.

**`whatsapp.thread({phone, limit?})`** (phase 3). It returns the last messages of the thread (at most
50) for scripts that need context.

### 4.3 The `on_message` hook (phase 2)

```
req.method = "WHATSAPP", req.path = "/_rx/whatsapp"   (internal, as hooks use today)
req.body = {
  from: "+971501234567", user_id: "AE.123…", name: "Sara",
  type: "text" | "button" | "interactive" | "media",
  text: "1", button: {id, title}?,
  thread: {id, status: "bot" | "owner", window_until: "2026-11-22 10:00:00"},
  consented: true
}
```

- The script answers with `res.json({reply: "…", buttons?: [...]})`, which is sent like
  `whatsapp.reply`, or with `{handoff: true, note: "…"}`, or with nothing.
- It runs with the normal script limits and a slot of its own. Errors go to the app log with a
  request ID, as `on_admin_change` does.
- It does not run while the thread is in `owner` status (hand-over).

### 4.4 Platform templates (owned by the operator, created once in WhatsApp Manager)

All of them are Utility, in English first, with Arabic later as separate languages under the same
names. Each has a URL button "View" to `https://<platform host>/w/{{1}}` and a quick-reply button
"Stop messages".

| Name | Body (proposed, needs Meta approval) |
| --- | --- |
| `rx_confirmation` | `{{1}} confirmed your {{2}}: {{3}}. Tap View for the details.` |
| `rx_reminder` | `Reminder from {{1}}: {{2}} on {{3}}. {{4}}` |
| `rx_update` | `Update from {{1}} on your {{2}}: {{3}}` |

`{{1}}` is always the business name set by the platform. The others are `ref`, `when` and `text`.
Settings: `WHATSAPP_CUSTOMER_TEMPLATES=confirmation:rx_confirmation,…` and
`WHATSAPP_CUSTOMER_LANGUAGE`, parsed in `cmd/renoxium/whatsapp.go` like the existing ones. The
`whatsapp-test` command gets a `customer` mode.

### 4.5 Inbound routing on the platform number (phase 2)

A message to the platform number from customer X:
1. **Button replies carry their app.** The "Stop messages" quick reply and buttons from
   `whatsapp.reply` hold a payload `rx:<app-id>:<signed>`, so they route exactly.
2. Otherwise, if exactly one app sent X a message in the last 30 days, the message goes to that
   app's thread.
3. Otherwise (several apps), the platform answers once with a list "Which business is this
   about?" and remembers the choice for 24 hours.
4. Otherwise (none, e.g. a stranger writes to the platform number), the platform gives a fixed
   reply saying this number sends updates only and does not receive messages. No app sees the
   message.
5. A "Chat on WhatsApp" link from an app (phase 2) uses
   `wa.me/<platform number>?text=<business name> (ref <code>)`. The code routes the first message.
   The existing `wa.me` links to the owner's own number stay as they are.

### 4.6 Testing on preview without real messages

- **Phase 1.**
  - `whatsapp.send` on preview logs the rendered template text and fills the demo inbox. The agent
    sees it in the `logs` of `submit_form`, `try_scheduled_jobs` and `change_preview_row` results,
    and in `get_logs`.
  - `TestResponse` gains `whatsapp: [{to, kind, text, link}]`, so the agent sees what would have been
    sent without parsing the logs.
  - `whatsapp.consent` on preview works as on live (with the checkbox), so the agent tests the form.
  - No new MCP tool is needed in phase 1.
- **Phase 2.** A new MCP tool, `send_test_whatsapp_message {app, from, text, button_id?}`:
  - Preview only. It runs the auto-replies and `on_message` as if the customer wrote, and returns
    the replies that would have been sent, plus logs.
  - One operation, and every parameter has a type and a description. Annotations as
    `submit_form` has (`readOnly: false, destructive: true, openWorld: true`), because the script
    may change preview rows.
  - The description does not mention `/_rx/`. It is added to `review_test.go`'s `want` table.
- The reference (`mcpserver` `reference`) gains a WhatsApp section: the calls, the opt-in helper,
  the refusal reasons, and "preview never sends".

## 5. Costs and quotas per plan

**Cost to Renoxium.**
- Every delivered template is a paid utility message at the UAE rate. **[unverified]**: the amount,
  to be read from the AED rate card.
- Free-form replies inside the window are service messages: 1,000 free per number per month, then
  paid. That free tier is shared by all tenants on the platform number.
- The `statuses[].pricing` webhook field reports what Meta actually charged. Store it per app to
  check real cost against the quotas.

**Proposed quotas, separate from sign-in codes** (a new `usage` kind `wa_customer`; the existing
`whatsapp` kind stays for codes and owner notices):

| Plan | Customer messages per month | New consenting numbers per day | Per number per app per day |
| --- | --- | --- | --- |
| Free | 0, or 30 to try the feature (a decision) | 10 | 3 |
| Pro | 500 | 100 | 5 |
| Business | 2,000 | 500 | 10 |

- Past the monthly quota, `whatsapp.send` returns `{sent: false, reason: "quota"}`. The owner gets one
  email, as `countWhatsApp` does now, and scripts fall back to `email.send`.
- The server-wide daily cap (`cfg.Daily.WhatsApp`) stays above all of this, and must stay below
  Renoxium's Meta messaging limit (250 or 2,000 unique users per day until the limit rises).
- Extra packs could be sold later.

## 6. Safeguards against abuse

**Spam to other people's numbers.**
- No send without a consent record for (app, number). Consent can only come from a visitor's form
  with the platform checkbox ticked.
- The caps per number, per app per day, and for new consenting numbers per day (section 5).
- Every template has "Stop messages". Tapping it, or replying STOP / "الغاء" / UNSUBSCRIBE, sets
  `opted_out_at` for that app at once, and the platform confirms once.
- A number can still be typed by a stranger. The damage is bounded by "at most N messages, each one
  transactional, each with Stop".

**Phishing or impersonation through an app.**
- The business name in templates is the app's configured name. A name that matches a well-known
  brand list (banks, government, couriers) is held for operator review.
- `text` and `ref` refuse URLs, phone numbers longer than 7 digits and `@` handles. The only link is
  the platform redirect to the app's own path.
- Sending starts only on live apps, after the owner's email is confirmed. It is paused for apps
  suspended through the existing abuse report flow (`suspended` on `apps`).

**Quality protection per app.** The platform measures per app:
- The rate of opt-outs per message sent.
- Failed statuses with spam-related error codes.

If an app's opt-out rate passes a threshold (proposed: 5% over at least 50 messages in 7 days), its
WhatsApp sending pauses automatically. The owner is emailed, and the operator panel shows it with
a switch to turn it back on. Renoxium's own quality rating (from the `account_update` and
`phone_number_quality_update` webhooks) is shown to the operator.

**An owner using up the platform's capacity.**
- The monthly quota per account, and a daily cap per app at 1/10 of the monthly quota.
- Jobs already run on per-owner queues, so a reminder job cannot flood ahead of other tenants.
- A burst cap of 20 messages per minute per app; the rest are refused with `reason: "limit"`.

**Isolation between tenants on the inbound webhook.**
- One endpoint on the platform host. The raw body is limited to 3 MB, and `X-Hub-Signature-256` is
  checked with HMAC-SHA256 and `WHATSAPP_APP_SECRET` (constant-time compare) **before** parsing.
  Unsigned or mis-signed requests get 401 and no work.
- `metadata.phone_number_id` must be one of Renoxium's numbers (or, in phase 4, an owner number
  mapped to exactly one app). Anything else is dropped and logged on the server.
- Routing follows section 4.5. An app's script only ever sees messages routed to its own thread, and
  only through `on_message`. A thread lives in that app's own database, not a shared table.
- De-duplication by `wamid`. Processing runs on a background queue after a quick 200, so a slow
  script cannot make Meta retry and replay.
- Webhook payloads are never written to app logs as a whole. Meta's errors go to the server log, as
  the existing code does.

**Personal data (phone numbers, chat content).**
- Chat content lives only in the tenant database (`rx_wa_messages`, scripts cannot read it except
  through `whatsapp.thread` for their own app). It is deleted with the app, and kept for 180 days by
  default (a decision).
- App logs mask numbers (`+9715…4567`) and never contain message bodies on live. Preview logs show
  the full text, because preview data is test data.
- A visitor deleting their account deletes their consent and thread rows. The owner can delete one
  thread from the inbox.
- Secrets: `WHATSAPP_APP_SECRET`, `WHATSAPP_VERIFY_TOKEN`, the existing `WHATSAPP_TOKEN` and, in
  phase 4, per-owner business tokens (encrypted at rest with the existing server key) are env or
  database only. They are never in definitions, logs or MCP output.
- Privacy page text: name WhatsApp (Meta) as a processor for apps that use it. The UAE PDPL
  (Federal Decree-Law 45/2021) obligations are to be checked by counsel. I have not verified them.

## 7. Phases

Each phase is small and ships with tests. Phase 1 needs no inbound conversation features.

**Phase 1: automatic messages to customers (feature A), platform number.**
- `whatsapp.send`, `whatsapp.consent` and `whatsapp.status` host calls. The
  `{{rx_whatsapp_optin}}` helper. The `rx_wa_consent` table.
- Three customer templates in config. `CloudAPI.SendTemplate(ctx, phone, name, lang, params,
  buttonParam)` is generalised from `send`.
- The `/w/<token>` link redirect, signed and limited to the app's own host.
- A minimal signed webhook: verification GET, signature check, `statuses` (store delivery status
  and pricing per message) and opt-out (the Stop button and STOP words). Any other inbound text gets
  one fixed reply pointing to the business's own WhatsApp or email. It is a free-form service
  message, allowed because the customer just wrote.
- The `wa_customer` quotas, caps and per-app pause on opt-out rate. The usage line on the owner's
  app page.
- Preview: log, demo inbox, `TestResponse.whatsapp`. A reference section in the MCP server.
- Examples: `booking-uae` sends a confirmation on booking and a reminder from a daily job. Others
  later.
- Tests:
  - Go unit tests: consent rules, refusal reasons, caps, signature check (good, bad, missing,
    replayed `wamid`), routing of the Stop button, masking.
  - Platform tests with a fake Cloud API.
  - E2E: `fakes.mjs` records templates and can POST signed webhooks to the server, to cover the full
    path book → consent → template → Stop → no more messages.
- Ops: `docs/deploy-ubuntu.md` covers the templates, webhook URL, app secret and verify token.

**Phase 2: inbound handled by the app, plus automatic replies (B, C).**
- Threads and messages tables, the per-app window, routing (section 4.5), `on_message`,
  `auto_replies`, `whatsapp.reply`.
- The `send_test_whatsapp_message` MCP tool. The "Chat on WhatsApp (platform)" link helper.

**Phase 3: inbox and hand-over (D).**
- An admin panel inbox page (threads, search, reply box limited to the window, a "follow up"
  button that uses the `rx_update` template outside it).
- Hand-over status per thread, `handoff_keywords`, and a notice to the owner through the existing
  `notify.owner` WhatsApp/email path. `whatsapp.thread`. Retention cleanup.

**Phase 4: the owner's own number (E).**
- Embedded Signup (current version) and coexistence. A per-owner encrypted token. Templates created
  in the owner's WABA through the API.
- Phone-number-ID routing. Billing explained to the owner (they pay Meta).
- This needs Renoxium's Tech Provider status first.

## 8. Decisions for the product owner

1. **Sending number for phase 1**: the platform number (recommended), or wait for owner numbers.
2. **Free plan**: no customer WhatsApp messages, or a small trial (30 per month)? Pro 500 and
   Business 2,000 per month as proposed, or other numbers once the UAE utility rate is known?
3. **Opt-in model**: an explicit unticked checkbox only (recommended, follows Meta's wording rules),
   or also treat a number verified by WhatsApp sign-in as consent?
4. **Template wording** (section 4.4): approve or change the three bodies. The operator then creates
   them in WhatsApp Manager and waits for approval (up to 24 hours each). Arabic versions now or
   later?
5. **Start the Meta Tech Provider application now** (business verification of Renoxium, App Review)
   so that phase 4 is not blocked later?
6. **Retention of chat content**: 180 days (proposed), or another period?
7. **Opt-out threshold** that pauses an app's sending automatically: 5% over 50 messages in 7 days
   (proposed)?
8. **Inbound in phase 1**: the fixed reply to customers who write to the platform number ("this
   number only sends updates; contact <business> at …"), as proposed, or no answer at all?

## 9. What was not verified

- No real Meta account was used. Every Meta behaviour above comes from the documentation as read on
  2026-11-22, and the items marked [unverified] are open.
- The most important open points:
  - The UAE prices.
  - Whether Meta accepts the three multi-business utility templates as Utility.
  - Coexistence availability in the UAE.
  - The current AI-assistant policy.
