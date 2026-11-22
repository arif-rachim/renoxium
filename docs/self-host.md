# Running your app on your own server

This guide moves one app from Renoxium to a server you control. The same pages, visitor sign-in, admin
panel, scheduled jobs and photo sizes keep working; card payments go to your own Stripe account, and
emails and WhatsApp codes go out through your own accounts.

**Who can do this.** Self-hosting needs an export of a **paid work app** or from a **Pro or Business** account: the export then
carries a signed licence (`licence.json`) that the server checks at every start, offline. Exports from a
Free account hold the same data and can still be downloaded as a backup, but the server does not start
with them.

**Licence terms.** The Renoxium binary is source-available. You may run it for your own business, with
the one app your licence names. You may not offer it to others as a hosting service.

**Support.** There is no support for self-hosted sites beyond this guide. If you want Renoxium to run the
site for you, stay on Renoxium.

**What does not move.**
- Editing the app with ChatGPT or Claude. On your server you change the definition file by hand and apply
  it (see [Changing the app](#changing-the-app)).
- Visitors' passwords: exports never contain them. Visitors keep their accounts and orders, and set a new
  password with "Forgot password" (an emailed link) or sign in with a code.
- Custom domains, billing, templates, the gallery: your server serves only your one app.

```
Visitors ──HTTPS──► Caddy (certificates) ──► renoxium on 127.0.0.1:8080 ──► /var/lib/renoxium
                    www.yourshop.com             (your app and its admin panel)
                    admin.yourshop.com
```

## Before you start

- An Ubuntu 22.04 or 24.04 server with SSH and `sudo`, 1 GB RAM or more, with ports 80 and 443 open.
- A domain, for example `yourshop.com`, whose DNS you can change.
- A computer with Go 1.26 or newer to build the binary from the Renoxium source.
- The export: in the Renoxium console open the app, then **Download export**. It holds the
  definition, the live data, the uploaded photos, visitors' private files and the licence.

Three names point to the server (add each as an `A` record with the server's address):

| Name | What it serves |
| --- | --- |
| `www.yourshop.com` | your site (the address you give customers) |
| `APP.yourshop.com` | the same site at its app name, e.g. `noor-shop.yourshop.com` |
| `admin.yourshop.com` | the admin panel: orders, products, staff and settings |

## 1. Prepare the server

```bash
sudo apt-get update && sudo apt-get upgrade -y && sudo apt-get install -y sqlite3 caddy
```

```bash
sudo useradd --system --home /var/lib/renoxium --shell /usr/sbin/nologin renoxium
```

```bash
sudo ufw default deny incoming && sudo ufw allow OpenSSH && sudo ufw allow 80,443/tcp && sudo ufw --force enable
```

## 2. Build and copy the binary

On your computer, from the Renoxium source folder (`GOARCH=arm64` for an ARM server):

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o renoxium ./cmd/renoxium
```

```bash
scp renoxium deploy/renoxium.service deploy/backup.sh noor-shop.zip USER@SERVER-IP:~
```

On the server:

```bash
sudo install -D -m 755 ~/renoxium /opt/renoxium/renoxium && sudo install -m 755 ~/backup.sh /opt/renoxium/backup.sh
```

## 3. Import the export

The import fills the data directory from the export, once; the directory must be empty. Create it, then
import with the email of the admin account and your site's own address:

```bash
sudo install -d -o renoxium -g renoxium -m 700 /var/lib/renoxium
```

```bash
sudo -u renoxium /opt/renoxium/renoxium -data /var/lib/renoxium -apps-domain yourshop.com import ~/noor-shop.zip you@yourshop.com www.yourshop.com
```

The import prints the site addresses and a password for the admin account. Write the password down: it is
shown once. If you lose it, make a new one:

```bash
sudo -u renoxium /opt/renoxium/renoxium -data /var/lib/renoxium admin-password you@yourshop.com
```

## 4. Run it as a service

Edit `renoxium.service` before installing it. Replace the `ExecStart` lines with:

```
ExecStart=/opt/renoxium/renoxium \
  -addr 127.0.0.1:8080 \
  -data /var/lib/renoxium \
  -base-url https://admin.yourshop.com \
  -apps-domain yourshop.com \
  -trusted-ip-header X-Real-IP \
  -email-from "Noor Perfumes <orders@yourshop.com>" \
  -workers 2 \
  -self-hosted
```

and change `Description=` to your shop's name. Then:

```bash
sudo cp ~/renoxium.service /etc/systemd/system/renoxium.service && sudo systemctl daemon-reload && sudo systemctl enable --now renoxium
```

```bash
sudo journalctl -u renoxium -n 20 --no-pager
```

Without `-self-hosted` the binary starts the full Renoxium product instead; with it, it refuses to start
unless the data directory holds one imported app and its valid licence.

## 5. HTTPS with Caddy

Write `/etc/caddy/Caddyfile` (Caddy gets the certificates by itself):

```
www.yourshop.com, noor-shop.yourshop.com, admin.yourshop.com {
	reverse_proxy 127.0.0.1:8080 {
		header_up X-Real-IP {remote_host}
	}
}

yourshop.com {
	redir https://www.yourshop.com{uri} permanent
}
```

```bash
sudo systemctl reload caddy
```

Open `https://www.yourshop.com`: the site should look as it did on Renoxium, photos included (they keep
their addresses). Open `https://admin.yourshop.com`, sign in with the admin email and password, and change
the password under **Account settings**.

The admin panel has your app, its tables, staff, notifications and connections. Pages that belong to the
Renoxium service (templates, billing, renaming the app, custom domains) are not there.

## 6. Email

Receipts, sign-in links and owner notices go out through [Resend](https://resend.com). In Resend, verify
the domain of your sender address (`yourshop.com` for `orders@yourshop.com`), create an API key, and store
it on the server, readable only by root:

```bash
sudo install -d -m 700 /etc/renoxium && echo 'RESEND_API_KEY=re_your_key' | sudo tee /etc/renoxium/env >/dev/null && sudo chmod 600 /etc/renoxium/env
```

```bash
sudo systemctl restart renoxium
```

Without a key the server still runs, but it writes emails to its log instead of sending them, so visitors
cannot receive sign-in links.

## 7. Card payments with your Stripe account (optional)

On your server, payments are charged on your own Stripe account. There is no Renoxium fee and no Stripe
Connect.

1. In your Stripe Dashboard (start in the sandbox), copy the secret key from Developers, API keys.
2. In Developers, Webhooks, add an endpoint:
   - URL: `https://admin.yourshop.com/stripe/webhook`
   - Listen to: **Events on Your account**
   - Events: `checkout.session.completed`, `checkout.session.async_payment_succeeded`,
     `checkout.session.async_payment_failed`, `checkout.session.expired`, `charge.refunded`,
     `payment_intent.succeeded`, `payment_intent.canceled`, `invoice.paid`, `invoice.payment_failed`,
     `customer.subscription.created`, `customer.subscription.updated`, `customer.subscription.deleted`
   - Copy its signing secret (`whsec_...`).
3. Add both and restart:

```bash
sudo tee -a /etc/renoxium/env >/dev/null <<'ENV'
STRIPE_SECRET_KEY=sk_test_your_key
STRIPE_WEBHOOK_SECRET=whsec_your_secret
ENV
```

```bash
sudo systemctl restart renoxium && sudo journalctl -u renoxium -n 20 --no-pager | grep -i stripe
```

Place a test order with card `4242 4242 4242 4242` and check that it shows as paid in the admin panel.
Then switch to your live key and create the live webhook the same way. Without the webhook secret,
payments are taken but orders are not marked paid.

## 8. WhatsApp sign-in codes and order notices (optional)

Without WhatsApp, visitors sign in by email and order notices go by email. To send codes on WhatsApp you
need your own WhatsApp Business account in Meta's WhatsApp Cloud API, with an approved **Authentication**
template (and a **Utility** template for order notices). The steps in Meta are the same as in
[the Renoxium deployment guide](deploy-ubuntu.md#whatsapp-sign-in-codes-optional); then add to
`/etc/renoxium/env`:

```
WHATSAPP_TOKEN=your_permanent_token
WHATSAPP_PHONE_NUMBER_ID=your_number_id
WHATSAPP_OTP_TEMPLATE=your_code_template
WHATSAPP_NOTICE_TEMPLATE=your_notice_template
```

and restart the service.

## Changing the app

On your server there is one stage, live. To change the app, edit its definition and apply it. The current
definition is `<app>/definition-vN.json` in your export (or a newer one you applied). Apply checks every
page first, as Renoxium does, and changes nothing if a check fails:

```bash
sudo -u renoxium /opt/renoxium/renoxium -data /var/lib/renoxium -apps-domain yourshop.com apply ~/definition.json
```

Every applied version is kept. To go back to the version before the live one, or to a given version:

```bash
sudo -u renoxium /opt/renoxium/renoxium -data /var/lib/renoxium -apps-domain yourshop.com rollback
```

```bash
sudo -u renoxium /opt/renoxium/renoxium -data /var/lib/renoxium -apps-domain yourshop.com rollback 3
```

The site picks up the change at once; no restart is needed. The format of a definition is described in the
`reference` text in `internal/mcpserver/mcpserver.go` of the source.

## Backups

Everything is in `/var/lib/renoxium`: the databases, the photos and the licence. `backup.sh` copies it
safely while the server runs and keeps 14 days of archives in `/var/backups/renoxium`:

```bash
echo '17 3 * * * root /opt/renoxium/backup.sh >/dev/null' | sudo tee /etc/cron.d/renoxium-backup
```

Copy the archives off the server regularly (another server or object storage, with `rsync` or `rclone`).
A backup on the same server does not help if the server is lost.

## Updates

Security fixes come in new versions of the binary; nothing updates by itself. Build the new version as in
step 2, then:

```bash
sudo /opt/renoxium/backup.sh && sudo install -m 755 ~/renoxium /opt/renoxium/renoxium && sudo systemctl restart renoxium
```

`https://admin.yourshop.com/version` shows the running version.

## Troubleshooting

- **The service does not start, log says the licence is not signed or not valid**: the data directory
  needs the `licence.json` from the export of a paid work app or a Pro or Business account. Make a new export on Renoxium and import it into
  an empty directory.
- **"a self-hosted data directory holds one app"**: the directory was used by a full Renoxium server.
  Import into a new, empty directory.
- **Visitors cannot sign in**: set `RESEND_API_KEY` (section 6); visitors who moved with the app set a new
  password with "Forgot password".
- **Orders stay unpaid after a card payment**: check `STRIPE_WEBHOOK_SECRET` and that the webhook in
  Stripe listens to events on your account and points to `https://admin.yourshop.com/stripe/webhook`.
