# Deploying Renoxium behind Cloudflare Tunnel

Cloudflare terminates HTTPS and forwards requests through `cloudflared` to
Renoxium on `127.0.0.1:8080`. The VPS needs no open ports and no reverse proxy.

Requirements: `renoxium.com` and `renoxium.app` are both zones in your Cloudflare account.

## 1. Build (on any machine with Go)

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o renoxium ./cmd/renoxium
```

## 2. Install on the VPS

```bash
sudo useradd --system --home /var/lib/renoxium --shell /usr/sbin/nologin renoxium
```

```bash
sudo install -d -o renoxium -g renoxium -m 700 /var/lib/renoxium
```

```bash
sudo install -D -m 755 renoxium /opt/renoxium/renoxium
```

```bash
sudo cp deploy/renoxium.service /etc/systemd/system/renoxium.service
```

```bash
sudo systemctl daemon-reload && sudo systemctl enable --now renoxium
```

Check it: `curl -s localhost:8080/healthz` prints `ok`.

## 3. Create the tunnel

```bash
cloudflared tunnel login
```

```bash
cloudflared tunnel create renoxium
```

Copy `deploy/cloudflared-config.yml` to `/etc/cloudflared/config.yml`, put the tunnel ID in it,
and move the credentials file `cloudflared tunnel create` wrote into `/etc/cloudflared/`.

```bash
cloudflared tunnel route dns renoxium renoxium.com
```

```bash
cloudflared tunnel route dns renoxium "*.renoxium.app"
```

```bash
sudo cloudflared service install
```

## 4. Check from outside

- `https://renoxium.com/healthz` prints `ok`.
- `https://renoxium.com/.well-known/oauth-authorization-server` returns JSON with `"issuer":"https://renoxium.com"`.
- Create an account at `https://renoxium.com`, then add `https://renoxium.com/mcp` as a connector in ChatGPT
  (developer mode) or Claude.

## Notes

- HTTPS for `*.renoxium.app` comes from Cloudflare's free Universal SSL, which covers one level of
  subdomain. That is why previews use `<app>--preview.renoxium.app` rather than a nested subdomain.
- Renoxium listens on `127.0.0.1` only, so the `CF-Connecting-IP` header it trusts for rate limits can
  only come from `cloudflared`.
- Data lives in `/var/lib/renoxium`: `platform.db`, `accounts.db` and one folder per app. Back up that
  directory; backups are not automated yet.
