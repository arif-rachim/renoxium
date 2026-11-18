# Deploying Renoxium on a small Ubuntu VPS

This guide is for one small Ubuntu 22.04 or 24.04 VPS (about 1 vCPU and 1–2 GB RAM), with HTTPS
provided by Cloudflare Tunnel. The VPS opens no ports other than SSH.

```
Visitors / ChatGPT / Claude
        │ HTTPS
        ▼
   Cloudflare  (certificates for renoxium.com and *.renoxium.app)
        │ tunnel, opened outbound from the VPS
        ▼
   cloudflared ──► Renoxium on 127.0.0.1:8080 ──► /var/lib/renoxium (SQLite)
```

## Before you start

- An Ubuntu VPS with SSH access and `sudo`.
- `renoxium.com` and `renoxium.app` are both zones in the same Cloudflare account.
- A computer with Go 1.26 or newer to build the binary. `go.mod` names the exact patch release (1.26.11 now),
  and Go downloads it by itself when the installed one is older, so builds always include Go's security fixes.
  After `git pull`, check for new ones with `go run golang.org/x/vuln/cmd/govulncheck@latest ./...`; when it
  reports standard library issues, raise the release with `go get go@<version> toolchain@go<version>`.

## Memory budget

| Part | Normal | Worst case |
| --- | --- | --- |
| Main process | 30–60 MB | grows with the number of active apps |
| Each script worker | about 24 MB | 256 MB, then it is killed and replaced |
| cloudflared | 30–50 MB | |

With `-workers 2` (the setting in `deploy/renoxium.service`), normal use is about 150 MB and the worst
case about 650 MB. That fits a 1 GB VPS with 1 GB of swap as a safety margin. On 2 GB or more, use
`-workers 4`.

## 1. Prepare the server

Log in to the VPS, update packages and install `sqlite3` (used for backups):

```bash
sudo apt-get update && sudo apt-get upgrade -y && sudo apt-get install -y sqlite3
```

Add 1 GB of swap if the VPS has only 1 GB of RAM:

```bash
sudo fallocate -l 1G /swapfile && sudo chmod 600 /swapfile && sudo mkswap /swapfile && sudo swapon /swapfile
```

```bash
echo '/swapfile none swap sw 0 0' | sudo tee -a /etc/fstab
```

Close every inbound port except SSH. The tunnel only makes outbound connections, so ports 80 and 443 stay closed:

```bash
sudo ufw default deny incoming && sudo ufw default allow outgoing && sudo ufw allow OpenSSH && sudo ufw --force enable
```

Create a system user and the data directory:

```bash
sudo useradd --system --home /var/lib/renoxium --shell /usr/sbin/nologin renoxium
```

```bash
sudo install -d -o renoxium -g renoxium -m 700 /var/lib/renoxium
```

## 2. Build and copy the binary

On your computer, from the repository folder. Use `GOARCH=arm64` if the VPS is ARM (run `uname -m` on
the VPS: `x86_64` means amd64, `aarch64` means arm64).

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o renoxium ./cmd/renoxium
```

```bash
scp renoxium deploy/renoxium.service deploy/backup.sh deploy/cloudflared-config.yml USER@VPS-IP:~
```

## 3. Run Renoxium as a service

On the VPS:

```bash
sudo install -D -m 755 ~/renoxium /opt/renoxium/renoxium && sudo install -m 755 ~/backup.sh /opt/renoxium/backup.sh
```

```bash
sudo cp ~/renoxium.service /etc/systemd/system/renoxium.service && sudo systemctl daemon-reload && sudo systemctl enable --now renoxium
```

### Email (Resend)

Renoxium sends account emails (email confirmation and password reset) through Resend. In the Resend
dashboard, verify the domain of the sender address first (by default `no-reply@renoxium.com`, so the
`renoxium.com` domain). Then store the API key on the VPS, readable only by root:

```bash
sudo install -d -m 700 /etc/renoxium && echo 'RESEND_API_KEY=re_your_key' | sudo tee /etc/renoxium/env >/dev/null && sudo chmod 600 /etc/renoxium/env
```

```bash
sudo systemctl restart renoxium
```

Without a key Renoxium still runs, but it writes emails to its log instead of sending them. To use
another sender address, add `-email-from "Renoxium <hello@renoxium.com>"` to `ExecStart`.

### Card payments with Stripe (optional)

Store owners connect their own Stripe account in the admin panel (Payments). Renoxium creates each
merchant account with the full Stripe Dashboard, Stripe fees paid by the merchant and Stripe covering
negative balances, and charges shoppers directly on the merchant's account. Renoxium takes no fee.

1. In the Stripe Dashboard of the Renoxium platform account (start in the sandbox), copy the secret key from
   Developers, API keys.
2. In Developers, Webhooks, add an endpoint:
   - URL: `https://www.renoxium.com/stripe/webhook` (the site address in `-base-url`)
   - Listen to: **Events on Connected accounts**
   - Events: `checkout.session.completed`, `checkout.session.async_payment_succeeded`,
     `checkout.session.async_payment_failed`, `checkout.session.expired`, `account.updated`, `charge.refunded`
     (marks orders refunded when an owner refunds in their Stripe Dashboard), `payment_intent.succeeded`,
     `payment_intent.canceled` (card holds captured or expired), `invoice.paid`, `invoice.payment_failed`,
     `customer.subscription.created`, `customer.subscription.updated`, `customer.subscription.deleted` (recurring payments)
   Copy its signing secret (`whsec_...`).
3. Add both to `/etc/renoxium/env` and restart:

```bash
sudo tee -a /etc/renoxium/env >/dev/null <<'ENV'
STRIPE_SECRET_KEY=sk_test_your_key
STRIPE_WEBHOOK_SECRET=whsec_your_secret
ENV
```

```bash
sudo systemctl restart renoxium && sudo journalctl -u renoxium -n 20 --no-pager | grep -i stripe
```

The log line says `Stripe payments on` with `mode=test` or `mode=live` and whether the webhook secret is
set. Without `STRIPE_SECRET_KEY`, live apps cannot take card payments; previews always simulate checkout.
Without the webhook secret, payments are taken but orders are not marked paid. Move to live keys only after
testing a full payment in the sandbox, and create the live webhook the same way.

### Renoxium plans: Free, Pro and Business (optional)

Accounts pay Renoxium through subscriptions on the Renoxium platform's own Stripe account. Free accounts
pay a 2% Renoxium fee on their shops' card payments (taken by Stripe as an application fee), show a small
"Made with Renoxium" badge on live pages and may create 1 app; Pro (AED 99 a month, 3 apps) and Business
(AED 299 a month, 10 apps) pay no fee and show no badge. Owners manage their plan at `/billing`.

In the Stripe Dashboard of the platform account (start in the sandbox):

1. **Product catalog:** create the products "Renoxium Pro" and "Renoxium Business", each with a recurring
   monthly price in AED (99 and 299). Copy both price IDs (`price_...`).
2. **Settings, Billing, Customer portal:** activate it. Allow updating payment methods, viewing invoices,
   cancelling, and switching between the two prices.
3. **Developers, Webhooks:** add a second endpoint, separate from the payments one:
   - URL: `https://www.renoxium.com/stripe/billing-webhook`
   - Listen to: **Events on your account**
   - Events: `checkout.session.completed`, `customer.subscription.created`, `customer.subscription.updated`,
     `customer.subscription.deleted`, `customer.subscription.paused`, `customer.subscription.resumed`
   Copy its signing secret.

Add them to `/etc/renoxium/env` and restart:

```bash
sudo tee -a /etc/renoxium/env >/dev/null <<'ENV'
STRIPE_PRICE_PRO=price_your_pro_price
STRIPE_PRICE_BUSINESS=price_your_business_price
STRIPE_BILLING_WEBHOOK_SECRET=whsec_your_billing_secret
ENV
```

```bash
sudo systemctl restart renoxium && sudo journalctl -u renoxium -n 30 --no-pager | grep -i plans
```

The log line `Renoxium plans` must show all three as set. Subscription revenue appears in Stripe under
Billing, Subscriptions; the 2% fees under Connect, Collected fees.

To give an account a plan without payment (your own account, partners, trials):

```bash
sudo /opt/renoxium/renoxium -data /var/lib/renoxium plan owner@example.com business
```

`plan EMAIL free` removes such a grant.

### Privacy Policy and Terms

`/privacy` and `/terms` are drafts until a lawyer has reviewed them. They show the company details from
these settings, and a "Draft" notice until `RENOXIUM_LEGAL_REVIEWED=1`:

```bash
sudo tee -a /etc/renoxium/env >/dev/null <<'ENV'
RENOXIUM_LEGAL_NAME=Renoxium (FZC)
RENOXIUM_LEGAL_ADDRESS=your registered address
RENOXIUM_LEGAL_EMAIL=privacy@renoxium.com
ENV
```

The texts live in `internal/web/landing.go`.

### Operator panel

`https://www.renoxium.com/operator` shows totals (accounts per plan, monthly subscription revenue, apps,
WhatsApp codes), lists accounts with a plan grant, apps with suspend and bring back, and abuse reports.
Only confirmed accounts whose email is listed in `RENOXIUM_OPERATORS` can open it; everyone else gets 404.

```bash
echo 'RENOXIUM_OPERATORS=you@renoxium.com' | sudo tee -a /etc/renoxium/env >/dev/null && sudo systemctl restart renoxium
```

### WhatsApp sign-in codes (optional)

Visitors of an app can sign in with a 6-digit code sent on WhatsApp, through Meta's WhatsApp Cloud API.
The setup happens in Meta Business Suite, WhatsApp Manager and developers.facebook.com, in this order:

1. **Business portfolio and WhatsApp account.** Verify the business, then add the sending number to the
   WhatsApp Business Account. Its display name must be approved before messages go out.
2. **Payment method.** Add one in WhatsApp Manager (the "Missing valid payment method" banner must be
   gone). Without it Meta refuses every template message with error 131042.
3. **Template.** Create a template of category **Authentication** (one-time passcode) with a **Copy code**
   button, for example `login_code`, with the security recommendation and a 10-minute expiry (the same as
   Renoxium's code lifetime). Wait until it is **Approved**. Note its language code exactly as shown:
   "English" is `en`, "English (US)" is `en_US`. The language Renoxium sends must match, or Meta answers
   132001.
4. **App.** Create or open the app at developers.facebook.com/apps. If it says "Account confirmation
   needed", the app admin must confirm their account there first; tokens cannot be generated until then.
5. **System user and token.** In Business settings, System users: create a system user (Admin), then
   assign it to the app (Manage app) **and** to the WhatsApp account (Full control). Only after both
   assignments does Generate token offer permissions; otherwise it shows "No permissions available".
   Generate a token with expiration Never and the `whatsapp_business_messaging` and
   `whatsapp_business_management` permissions. Keep it secret.
6. **Phone number ID.** Copy it from WhatsApp Manager (Phone numbers) or the app's WhatsApp, API setup page.

Add the settings to `/etc/renoxium/env` next to the Resend key:

```bash
sudo tee -a /etc/renoxium/env >/dev/null <<'ENV'
WHATSAPP_TOKEN=your_access_token
WHATSAPP_PHONE_NUMBER_ID=your_phone_number_id
WHATSAPP_OTP_TEMPLATE=login_code
WHATSAPP_OTP_LANGUAGE=en
ENV
```

`WHATSAPP_OTP_LANGUAGE` defaults to `en`; set it to the template's language code if that is different.
`WHATSAPP_API_VERSION` (default `v23.0`) is optional.

Before restarting, send one test code to your own WhatsApp from the VPS. This uses the same settings and
prints Meta's answer, with a hint for the usual setup errors:

```bash
sudo bash -c 'set -a; . /etc/renoxium/env; /opt/renoxium/renoxium whatsapp-test +971501234567'
```

When it prints `OK`, restart Renoxium so live apps offer "Sign in with WhatsApp":

```bash
sudo systemctl restart renoxium
```

Every code is a paid WhatsApp message. Renoxium allows three codes per number per 15 minutes, and each account's live apps may send 50 (Free), 500 (Pro) or 2,000 (Business) codes per calendar month; past that, sign-in pages offer email only and the owner gets one email. Without
the three required settings, WhatsApp sign-in is off on live apps; previews always offer it and write
the code to the app log instead of sending it.

Errors you may meet on the first test:

| Meta error | Meaning | Fix |
| --- | --- | --- |
| 190 | Token invalid or expired | Generate a new system user token |
| 10 or 200 | Token has no permission for this number | Assign the system user to the app and the WhatsApp account, then generate the token again |
| 131042 | Payment problem | Add a valid payment method in WhatsApp Manager |
| 132001 | Template not found in this language | Template name and `WHATSAPP_OTP_LANGUAGE` must match the approved template exactly |
| 132000 or 132012 | Template parameters do not match | The template must be Authentication with a Copy code button |
| 131030 | Recipient not in the allowed list | The app is limited to test recipients: add your number as a recipient, or finish the setup so the number can message anyone |
| 133010 | Sending number not registered with the Cloud API | Register the number (with its two-step PIN) in WhatsApp Manager |

#### Order notices to owners on WhatsApp (optional)

Owners can get `notify.owner` notices (new cash-on-delivery orders, card payments received) on WhatsApp
as well as by email, from **Notifications** on the app's page, after confirming their number with a code.
This needs a second template, because a business-initiated message that is not a code must use an
approved **Utility** template:

1. In WhatsApp Manager, create a template of category **Utility**, for example `owner_notice`, language
   English (`en`), with this body and no buttons:

   ```
   Renoxium: your app {{1}} has a new notice: {{2}}. The details are in your email and in your Renoxium admin panel.
   ```

   Sample values: `oud-al-noor` and `New order #12 (cash on delivery): AED 240.00`. Wait until it is Approved.
2. Add to `/etc/renoxium/env`:

   ```
   WHATSAPP_NOTICE_TEMPLATE=owner_notice
   WHATSAPP_NOTICE_LANGUAGE=en
   ```

3. Test it, then restart Renoxium:

   ```bash
   sudo bash -c 'set -a; . /etc/renoxium/env; /opt/renoxium/renoxium whatsapp-test +971501234567 notice'
   ```

Each notice, and each confirmation code, counts towards the account's monthly WhatsApp messages
(the same quota as sign-in codes), with at most 100 notices per app per day.

Check that the server is up. This must print `ok`:

```bash
curl -s 127.0.0.1:8080/healthz
```

If it does not, read the log:

```bash
sudo journalctl -u renoxium -n 50 --no-pager
```

## 4. Set up Cloudflare Tunnel

Install `cloudflared` from Cloudflare's official repository:

```bash
sudo mkdir -p --mode=0755 /usr/share/keyrings && curl -fsSL https://pkg.cloudflare.com/cloudflare-main.gpg | sudo tee /usr/share/keyrings/cloudflare-main.gpg >/dev/null
```

```bash
echo 'deb [signed-by=/usr/share/keyrings/cloudflare-main.gpg] https://pkg.cloudflare.com/cloudflared any main' | sudo tee /etc/apt/sources.list.d/cloudflared.list
```

```bash
sudo apt-get update && sudo apt-get install -y cloudflared
```

Log in to Cloudflare. The command prints a link; open it in a browser and choose the `renoxium.com` zone:

```bash
cloudflared tunnel login
```

Create the tunnel and note the ID it prints (it looks like `6ff42ae2-765d-4adf-8112-31c55c1551ef`):

```bash
cloudflared tunnel create renoxium
```

Move the configuration and credentials to `/etc/cloudflared`. Replace `TUNNEL_ID` and `YOUR-TUNNEL-ID`
with that ID:

```bash
sudo mkdir -p /etc/cloudflared && sudo cp ~/.cloudflared/TUNNEL_ID.json /etc/cloudflared/
```

```bash
sed 's/TUNNEL_ID/YOUR-TUNNEL-ID/g' ~/cloudflared-config.yml | sudo tee /etc/cloudflared/config.yml
```

Point both domains at the tunnel:

```bash
cloudflared tunnel route dns renoxium renoxium.com
```

```bash
cloudflared tunnel route dns renoxium '*.renoxium.app'
```

If the second command refuses because `renoxium.app` is a different zone, add the DNS record by hand
in the Cloudflare dashboard for `renoxium.app`: type `CNAME`, name `*`, target
`TUNNEL_ID.cfargotunnel.com`, proxy on (orange cloud).

Run the tunnel as a service:

```bash
sudo cloudflared service install && sudo systemctl enable --now cloudflared
```

In the Cloudflare dashboard, for both zones, open **SSL/TLS → Edge Certificates** and turn on
**Always Use HTTPS**.

### Serving the site on www

If the site should live on `www.renoxium.com` (for example because a Cloudflare rule redirects
`renoxium.com` to `www.renoxium.com`), use the www address everywhere: `-base-url https://www.renoxium.com`
in the service, `hostname: www.renoxium.com` in the tunnel configuration, and
`cloudflared tunnel route dns renoxium www.renoxium.com`. The site answers only on the host in
`-base-url`, and the connector address becomes `https://www.renoxium.com/mcp`. In the checks below,
replace `renoxium.com` with `www.renoxium.com`.

### Store owners' own domains (optional)

Owners on Pro and Business can serve their app on a domain they own, such as `www.oudshop.ae`, from
**Domain** on the app's page. Renoxium registers the domain as a Cloudflare for SaaS custom hostname
on the `renoxium.app` zone; Cloudflare issues the certificate, and the request reaches this server
through the tunnel. The tunnel configuration above ends with a catch-all to Renoxium for this reason.

1. In the Cloudflare dashboard for `renoxium.app`, add the fallback origin's DNS record:

   ```bash
   cloudflared tunnel route dns renoxium customers.renoxium.app
   ```

2. Open **SSL/TLS → Custom Hostnames**, enable Cloudflare for SaaS if asked (the first 100 hostnames
   are free), and set the **fallback origin** to `customers.renoxium.app`. Wait until it shows Active.
3. Create an API token at **My Profile → API Tokens → Create Token → Custom token** with the permission
   **Zone → SSL and Certificates → Edit**, limited to the `renoxium.app` zone. Copy the zone ID from the
   zone's **Overview** page.
4. Add to `/etc/renoxium/env` and restart Renoxium:

   ```
   CLOUDFLARE_API_TOKEN=...
   CLOUDFLARE_ZONE_ID=...
   CUSTOM_DOMAIN_TARGET=customers.renoxium.app
   ```

   The log shows `custom domains on target=customers.renoxium.app`.

The owner first proves the domain is theirs: the page shows a `TXT` record for `_renoxium.<domain>`,
made from their account, and nothing is registered with Cloudflare until it is found in DNS. Renoxium
looks it up through Cloudflare's DNS over HTTPS (`https://cloudflare-dns.com/dns-query`; set
`RENOXIUM_DNS_API` to use another JSON API). This is what stops another account from claiming a domain
whose DNS already points at Renoxium.

The owner then adds a `CNAME` from their domain to `customers.renoxium.app` at their domain provider
(the page lists any extra TXT records Cloudflare asks for). The page checks again on every visit; once
the domain is active, the app's live address, payment return links and emails use it. Subdomains such
as `www` work with every provider; a bare domain (`oudshop.ae`) needs a provider that allows CNAME
flattening or ALIAS records.

## 5. Check from outside

From any computer:

```bash
curl -s https://renoxium.com/healthz
```

```bash
curl -s https://renoxium.com/.well-known/oauth-authorization-server
```

The first prints `ok`. The second returns JSON containing `"issuer":"https://renoxium.com"`.

Then try the whole flow:

1. Open `https://renoxium.com` and create an account.
2. In ChatGPT (with developer mode on) or Claude, add a custom connector with the address
   `https://renoxium.com/mcp`. The agent opens the Renoxium sign-in and consent screens.
3. Ask the agent: "Build an online perfume store with three products, then publish it."
4. Open the store at `https://STORE-NAME.renoxium.app` and manage its data at
   `https://renoxium.com/apps/STORE-NAME`.

## 6. Automatic backups

`backup.sh` copies every database safely while the server runs, writes one `.tar.gz` archive to
`/var/backups/renoxium` and deletes archives older than 14 days. Run it once:

```bash
sudo /opt/renoxium/backup.sh
```

Schedule it every day at 03:17 server time:

```bash
echo '17 3 * * * root /opt/renoxium/backup.sh >/dev/null' | sudo tee /etc/cron.d/renoxium-backup
```

A backup on the same VPS does not help if the VPS is lost. Copy the archives somewhere else regularly,
for example to another server or object storage with `rsync` or `rclone`.

### Off-site copies (recommended)

`backup.sh` can also copy every archive and all uploaded images to another place with rclone, for
example a Cloudflare R2 bucket, any S3 storage or another server over SFTP. Install rclone and configure a
remote as root (the setup asks for the provider and its keys):

```bash
sudo apt-get install -y rclone && sudo rclone config
```

Then name the remote and path in `/etc/renoxium/env`, for example a remote called `r2` and a bucket
`renoxium-backups`:

```bash
echo 'RENOXIUM_BACKUP_REMOTE=r2:renoxium-backups' | sudo tee -a /etc/renoxium/env >/dev/null
```

From the next run, archives go to `<remote>/db` (kept 30 days there) and images to `<remote>/files`; images
never change, so only new ones are sent. Run `sudo /opt/renoxium/backup.sh` once to check: it ends with
`copied to r2:renoxium-backups`.

### Uploaded images

Images live in `/var/lib/renoxium/files`, one file per distinct content, and are not part of the daily
archive. A stored file never changes, so an incremental copy is enough and only sends new files:

```bash
sudo rsync -a /var/lib/renoxium/files/ BACKUP-HOST:/backups/renoxium-files/
```

Schedule it next to the database backup, for example every hour. To restore, copy the folder back before
starting Renoxium and run `sudo chown -R renoxium:renoxium /var/lib/renoxium`.

### Restoring from a backup

```bash
sudo systemctl stop renoxium
```

```bash
sudo mkdir -p /tmp/restore && sudo tar -C /tmp/restore -xzf /var/backups/renoxium/renoxium-STAMP.tar.gz
```

Remove the write-ahead files of the current databases, so they are not applied to the restored ones:

```bash
sudo find /var/lib/renoxium \( -name '*.db-wal' -o -name '*.db-shm' \) -delete
```

Copy the databases back. Do not use `--delete`: the archive holds only databases, and the data folder
also holds the uploaded images (`files/`) and `preview.key`.

```bash
sudo rsync -a /tmp/restore/STAMP/ /var/lib/renoxium/ && sudo chown -R renoxium:renoxium /var/lib/renoxium
```

```bash
sudo systemctl start renoxium
```

Preview databases are not backed up because they only hold a copy of live data. The next
`apply_change` creates the preview again.

### Keep preview.key safe

`/var/lib/renoxium/preview.key` signs preview links and encrypts the API keys store owners enter for their
apps (`/apps/<app>/connections`). It is not in the database backups on purpose, so an off-site copy alone
cannot reveal those keys. Copy it once to a safe place, such as a password manager:

```bash
sudo base64 /var/lib/renoxium/preview.key
```

When moving to a new server, write it back before the first start:

```bash
echo 'PASTE-THE-BASE64-HERE' | base64 -d | sudo tee /var/lib/renoxium/preview.key >/dev/null && sudo chown renoxium:renoxium /var/lib/renoxium/preview.key && sudo chmod 600 /var/lib/renoxium/preview.key
```

Without it, Renoxium creates a new key: preview links change and owners must enter their API keys again.

## Monitoring and alerts

Renoxium checks itself every 5 minutes and emails the addresses in `RENOXIUM_OPERATORS` (through
Resend) when:

- the data disk has less than 10% or 2 GB free;
- the last backup is more than 26 hours old, or the last off-site copy is (backup checks start once
  `/etc/cron.d/renoxium-backup` exists; `backup.sh` writes `.last-backup` and `.last-offsite` time
  stamps in the data folder for this);
- live apps log 200 or more server errors in 5 minutes.

Each kind of alert is sent at most every 6 hours, and the latest state is on the operator panel
(`/operator`). App owners get an email on their live app's first server error, then at most one a day,
with the latest error and a link to the app page.

Renoxium cannot report its own crash or a dead VPS. For that, add an external uptime check (for example
UptimeRobot or Better Stack, both free for a few checks) on:

```
https://renoxium.com/healthz
```

It answers `ok`, or HTTP 503 when the disk is almost full (under 3% free). In Cloudflare,
**Notifications → Add → Tunnel Health Alert** also emails you when the tunnel goes down.

## 7. Updating to a new version

Build and copy the new binary as in step 2, then on the VPS:

```bash
sudo /opt/renoxium/backup.sh && sudo install -m 755 ~/renoxium /opt/renoxium/renoxium && sudo systemctl restart renoxium
```

A restart takes a few seconds. Requests in flight at that moment can fail, so update at a quiet time.

## Everyday commands

| For | Command |
| --- | --- |
| Follow the Renoxium log | `sudo journalctl -u renoxium -f` |
| Follow the tunnel log | `sudo journalctl -u cloudflared -f` |
| Status | `systemctl status renoxium cloudflared` |
| Running version | `curl -s localhost:8080/version` on the VPS, or open `/version` signed in as an operator (the commit the server was built from; hidden from everyone else) |
| Memory use | `ps -o rss,args -u renoxium` (RSS in KB) |
| Data size | `sudo du -sh /var/lib/renoxium` |
| Image storage size | `sudo du -sh /var/lib/renoxium/files` |

## Troubleshooting

| Symptom | Likely cause | What to check |
| --- | --- | --- |
| Cloudflare error page 1033 or 502 | `cloudflared` or Renoxium is down | `systemctl status cloudflared renoxium` |
| `https://renoxium.com` works, stores do not | No DNS record for `*.renoxium.app` | the `*` record in the `renoxium.app` zone |
| A store shows "not published yet" | The app was never published | ask the agent to run `publish` |
| Sign-in keeps returning to the sign-in form | `-base-url` differs from the address in the browser | it must be exactly `https://renoxium.com` |
| The agent cannot connect, `/mcp` returns 401 | Normal before sign-in; the problem is in the OAuth steps | open `/.well-known/oauth-authorization-server` and check it uses `https://renoxium.com` |
| The site returns 404 "not found" | The browser address differs from the host in `-base-url` (for example with or without www) | use the address in `-base-url`, or fix the redirect in Cloudflare |
| Everyone gets "too many attempts" | `-trusted-ip-header` is missing from the service | `systemctl cat renoxium` |
| No confirmation or reset email arrives | Missing `RESEND_API_KEY`, or the sender domain is not verified in Resend | `sudo journalctl -u renoxium \| grep -i email` |
| "Sign in with WhatsApp" is missing on live apps | The three `WHATSAPP_` settings are not all set | `sudo journalctl -u renoxium \| grep -i whatsapp` |
| WhatsApp codes do not arrive | Template not approved, wrong template name or language, no payment method, or an expired token | run `whatsapp-test` (section WhatsApp); the app log in the admin panel shows Meta's error code and a hint |
| Payments taken but orders stay "awaiting payment" | Webhook missing, not listening to connected accounts, or a wrong `STRIPE_WEBHOOK_SECRET` | Stripe Dashboard, Webhooks: the endpoint's recent deliveries and their responses |
| An owner paid but still shows Free | Billing webhook missing, scoped to connected accounts, or a wrong `STRIPE_BILLING_WEBHOOK_SECRET` | Stripe, Webhooks: the billing endpoint's deliveries; opening `/billing` after paying also re-reads the plan |
| Out of memory | Too many workers for the VPS size | lower `-workers`, check swap is on with `free -h` |
