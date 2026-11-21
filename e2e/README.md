# End-to-end tests

The suite builds `renoxium` from this checkout and starts it on a fresh data
directory at `http://localhost:18080`, with apps at `http://<app>.localhost:18080`.
Playwright then drives Chromium through every flow an owner, a shopper, a staff
member and the operator use.

Stripe, Resend, the WhatsApp Cloud API and Cloudflare are replaced by fakes in
`fakes.mjs`, so the run needs no keys and no network, and nothing leaves the
machine:

- **Fake Stripe** shows its own onboarding, Checkout and Customer Portal pages.
- Paying, finishing onboarding or changing a plan there sends the **signed
  webhooks** Stripe would send.

The binary reads the fakes' addresses from these test-only variables. They are
unset in production, so the real services are used there:

- `STRIPE_API_BASE`
- `RESEND_API_URL`
- `WHATSAPP_API_BASE`
- `CLOUDFLARE_API_BASE`
- `FETCH_API_BASE`: every `http.fetch` an app makes goes to `<base>/<host>/<path>` (the fake API in
  `fakes.mjs`) instead of the internet. The server logs a warning when it is set.

`JOBS_CHECK_EVERY=1s` makes the server look for due scheduled jobs every second instead of every 30,
so the job scenario waits about a minute, not a minute and a half.

## Run

```sh
cd e2e
npm ci
npx playwright install chromium   # once
./run.sh                          # all of it, about 4 minutes
./run.sh tests/04                 # one file
./run.sh -g "refund"              # tests whose name matches
E2E_VIDEO=1 ./run.sh              # record a video of every test
E2E_CHROMIUM=/path/to/chrome ./run.sh   # use a Chromium already installed
```

You need Go and Node 20 or newer.

`run.sh` prints PASS or FAILED per test. The server log is in
`e2e/.run/renoxium.log`. For a failed test, there are traces and screenshots in
`e2e/test-results/` (`npx playwright show-trace <file>`), and the HTML report
opens with `npx playwright show-report`.

## What is covered

| File | Scenarios |
| --- | --- |
| `01-site-and-account` | Landing, connect, legal pages, health, OAuth metadata; sign-up with input checks, email confirmation, resending the link, duplicate address; sign-in with a wrong password, `next=` back to `/account` and `/billing`, outside `next=` ignored; sign-out; forgotten password (same answer for unknown addresses, single-use link, old password refused); change password signs other sessions out; data export zip; abuse report email; CSRF |
| `02-agent-and-publish` | OAuth dynamic registration (https or loopback only), sign-in on the way to consent, Cancel (`access_denied`), PKCE (wrong verifier refused), refresh tokens, a replayed code revokes its tokens, 401 with resource metadata; every builder tool (`get_example`, `create_app` with reserved, malformed and taken names, `apply_change` problems, `submit_form` and `view_page` on preview only, `patch_app` with a stale `base_version`, `upload_file`, `get_logs`, `get_app`, `list_apps`); publishing needs the owner's confirmation; roll-back; another owner's agent and admin panel cannot reach the app (404); the Free plan's one-app limit; registration rate limit |
| `03-shop-and-shoppers` | Catalogue, Arabic (right to left), product options with their own price, 404, favicon, cart price API, badge on Free; visitor sign-up with an email in the app's name, confirmation, sign-out, sign-in, password reset; WhatsApp sign-in code (wrong code refused, quota counted); cash on delivery with a wrong and a right discount code, totals, emails to the shopper and the owner, order list, invoice, another visitor gets 404, the owner sets it shipped; card refused while Stripe is not connected; the shop's own MCP (`search_products`, `get_product`, `store_info`); the preview's simulated checkout (pay and fail) |
| `04-card-payments` | Payments page explains Stripe's documents and the 2% fee; onboarding left halfway, under review, ready; one Stripe account per app; WhatsApp notices confirmed with a code; card payment as a direct charge with a 2% application fee, the webhook marks it paid, owner email and WhatsApp, a repeated webhook changes nothing, payments list and sales tiles; declined card, back, paying again; abandoned and expired Checkout; partial and full refunds; forged webhook signatures refused; COD orders on WhatsApp; turning notices off |
| `05-plans-and-billing` | Free limits shown; Pro through Checkout (price, metadata, one customer); badge gone and no fee on Pro; more apps, staff and a domain allowed; no second subscription while paying; switching to Business, cancelling at period end, renewing, a failed renewal (`past_due`) keeps the plan, cancelling now returns to Free with the badge back and new apps refused; closing Checkout without paying |
| `06-admin-panel` | Products: add with a photo, field errors, edit, search, delete, and the live shop follows; staff: invitation email, signing in, editing data, no access to payments, staff, domain, notifications or publishing, removal; custom domain: refused names, proof of ownership (TXT record, looked up through a fake DNS-over-HTTPS API), DNS records, active after Cloudflare validates, served on the domain, removed; deleting a visitor ends their session |
| `07-operator` | 404 for owners and for an unconfirmed operator address; figures; search; giving Pro without payment (it survives a billing sync); suspending an app (451) and bringing it back; abuse reports marked handled; back to Free |
| `08-account-deletion` | Wrong confirmation and wrong password change nothing; a paid plan is cancelled at Stripe at once; the apps go offline; the agent's token dies; the app name is free again |
| `09-jobs-realtime-fetch` | Scheduled job: rejected schedules, tried on preview with `try_scheduled_jobs` (it takes only the app), runs on live by itself after its interval, shown on the app page and in the log; realtime: the job's message updates an open page, a courier's phone (emulated GPS) moves the marker on the customer's map in the delivery-tracking example, the live channel refuses a missing or forged token, the owner is emailed on delivery; external API: `owner_actions` from `apply_change`, calls refused until the owner allows the host and enters the key on the Connections page (404 for another owner), the key reaches the API but not the script, the page, `get_app` or the log, unlisted hosts refused, the monthly meter on `/billing`, Stop refuses calls again |
| `12-agent-tools` | The building agent's tools for large apps, from an empty account (`agent-demo.mjs`, also runnable against any server with `RENOXIUM_URL` and `RENOXIUM_TOKEN`): `create_app` from an example, `get_app {only}`, one CSS rule and one script line in a 350-byte `edit_app`, `create_test_visitor`, a cash booking followed to its page, `screenshot_preview` of it at 390 px signed in, cancelling; live untouched |
| `13-view-transitions` | Every demo with the motion kit: a click on a card reveals the detail page with a cross-document view transition that includes the card's name, and so does Back; the named element carries the mask (border-radius or clip-path) on both pages and no name is used twice; with reduced motion there is no transition; a card without a photo (fresh apps from each example, on the owner's preview) moves its title into the page's `h1` and back, in a box of class `vtt` that hugs the text, and no photo frame is named |

The Go tests (`go test ./...`) cover the rest at unit level, such as rate
limits, quotas, the sandbox, migrations and monitoring.
