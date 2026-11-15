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
- A computer with Go 1.26 or newer to build the binary. An older Go downloads 1.26 by itself.

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

```bash
sudo rsync -a --delete /tmp/restore/STAMP/ /var/lib/renoxium/ && sudo chown -R renoxium:renoxium /var/lib/renoxium
```

```bash
sudo systemctl start renoxium
```

Preview databases are not backed up because they only hold a copy of live data. The next
`apply_change` creates the preview again.

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
| Out of memory | Too many workers for the VPS size | lower `-workers`, check swap is on with `free -h` |
