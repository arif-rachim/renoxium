# Deploying Renoxium behind Cloudflare Tunnel

A full step-by-step guide for a small Ubuntu VPS, including backups and updates, is in
[docs/deploy-ubuntu.md](../docs/deploy-ubuntu.md).

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
  (Plugins, then Add custom MCP server; its free plan is enough) or Claude (its free plan allows one custom connector).

## Notes

- Visitors sign in on `<app>--id.renoxium.app` (and `<app>--preview--id.renoxium.app`), a host per app where only
  the platform's sign-in pages are served. The wildcard DNS record, the tunnel's `*.renoxium.app` rule and the
  Universal SSL certificate already cover it: nothing to set up. App names cannot contain `--`, so no app can take
  such a name.
- HTTPS for `*.renoxium.app` comes from Cloudflare's free Universal SSL, which covers one level of
  subdomain. That is why previews use `<app>--preview.renoxium.app` rather than a nested subdomain.
- Renoxium listens on `127.0.0.1` only, so the `CF-Connecting-IP` header it trusts for rate limits can
  only come from `cloudflared`.
- Add `renoxium.app` to the [Public Suffix List](https://github.com/publicsuffix/list) (private section) once the
  domain is live, so browsers treat every app as its own site. Until then the platform refuses cross-origin POSTs itself.
- In Cloudflare, add a rate limiting rule for `*.renoxium.app` and `renoxium.com/login` as an outer layer.
- Script workers lock themselves down with Landlock (no files, no network). This needs a binary built with
  `CGO_ENABLED=0` (as above) and Linux 5.13 or newer with Landlock enabled. After starting, check
  `journalctl -u renoxium | grep "kernel sandbox"`: no output means it works; then add `-require-sandbox` to the
  service so scripts never run without it.
- Abuse reports from `https://renoxium.com/abuse` go to `-abuse-email` (and the journal). Take an app offline with
  `sudo -u renoxium /opt/renoxium/renoxium -data /var/lib/renoxium suspend APP "reason"`; running servers notice
  within 30 seconds. `unsuspend APP` brings it back.
- Agents cannot publish on their own: `publish` returns a link the owner opens on the Renoxium site to confirm.
- `preview.key` in the data directory signs preview links. It is created on first start; keep it with the backups.
- Data lives in `/var/lib/renoxium`: `platform.db`, `accounts.db` and one folder per app. Back up that
  directory; backups are not automated yet.

## Automatic deploys from CI

`.github/workflows/ci.yml` runs `gofmt`, `go vet`, the Go tests and the
end-to-end suite on every push and pull request. On `main`, when they pass, it
builds the linux/amd64 binary and publishes it as the pre-release
`build-<first 12 characters of the commit>`, with a SHA-256 checksum and the
signed build provenance as `renoxium-linux-amd64.sigstore.json`. The 30 newest
builds are kept. The workflow's actions are pinned to commit SHAs.

On the server, `renoxium-deploy.timer` runs `deploy.sh` (installed as
`/opt/renoxium-deploy/deploy.sh`) every minute. When `main` has moved, it:

1. waits until CI has published that commit's binary;
2. downloads it as the unprivileged `renoxium-build` user;
3. checks the checksum, that the binary says it was built from exactly that
   commit with no local changes (`go version -m`), and the signed provenance:
   `gh attestation verify --bundle` proves it was built by `ci.yml` from that
   commit of `main` on a GitHub-hosted runner. Someone who can only upload a
   release asset cannot produce that;
4. backs up, installs the binary and restarts;
5. rolls back if `/healthz` does not answer within 30 s.

Nothing is compiled or tested on the server. A commit whose tests fail never
gets a binary, so it is never deployed. A binary that fails a check or the
health check is not retried: the next commit is.

**The service unit is not installed automatically.** The script runs as root,
and a unit names the user and the commands systemd runs, so installing it from
the repository would let any push to `main` run commands as root. When
`deploy/renoxium.service` changes, the deploy logs the difference and keeps the
new unit in `/etc/renoxium/renoxium.service.pending`. Read it, then:

```bash
sudo /opt/renoxium-deploy/deploy.sh --install-unit
```

It shows the difference again and installs exactly that after you answer `y`.
`deploy.sh` itself is not updated from the repository either: after reading a
new version, copy it by hand:

```bash
sudo install -m 755 deploy/deploy.sh /opt/renoxium-deploy/deploy.sh
```

`deploy.sh` needs `gh` 2.49 or newer for the provenance check. Ubuntu's own
package is older; install it from GitHub's apt repository
(https://github.com/cli/cli/blob/trunk/docs/install_linux.md). It needs no
GitHub login: the check runs offline against the bundle. Without a new enough
`gh`, nothing is deployed and the journal says why.

`journalctl -u renoxium-deploy` shows what happened.

### Settings in GitHub

Three settings only the repository's admin can make:

- **Settings, Environments, `production`**: add yourself as a required
  reviewer, and allow only the `main` branch. Every build the server would
  install then waits for your approval in the Actions tab.
- **Settings, Branches**: protect `main` (require a pull request and the
  `Go tests` and `End-to-end` checks), so a stolen token or a careless agent
  cannot push straight to production.
- **Settings, General, Releases**: turn on immutable releases, so a published
  build cannot be replaced.
