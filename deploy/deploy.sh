#!/usr/bin/env bash
# Auto-deploy of github.com/arif-rachim/renoxium from the binaries CI builds.
#
# When main has a new commit, wait for the release build-<12 characters of
# the commit>, which .github/workflows/ci.yml publishes after the Go tests and
# the end-to-end suite pass. Download it as the unprivileged renoxium-build
# user, check its checksum and its signed build provenance (it was built by
# ci.yml on main from exactly that commit, on a GitHub-hosted runner), back
# up, install, restart, and roll back if /healthz does not answer within
# 30 s. A commit whose binary fails the checks or the health check is not
# retried. Nothing is compiled or tested on the server.
#
# The service unit is NOT installed from the repository: this script runs as
# root, and a unit can name the user and the commands systemd runs, so
# installing it automatically would let any push to main run commands as root.
# When the repository's unit differs from the installed one, the difference
# is logged and kept in /etc/renoxium/renoxium.service.pending (root only);
# review it and run
#   sudo /opt/renoxium-deploy/deploy.sh --install-unit
#
# Needs gh 2.49 or newer (GitHub's apt repository; Ubuntu's own package is
# older). This script itself is not updated from the repository either:
# copy a new version to /opt/renoxium-deploy/ by hand after reading it.
#
# Installed as /opt/renoxium-deploy/deploy.sh, run by renoxium-deploy.timer
# every minute. Host settings are below and in the sed lines for the unit.
set -euo pipefail
exec 9>/run/renoxium-deploy.lock
flock -n 9 || { echo "another deploy is running"; exit 0; }

REPO=arif-rachim/renoxium
WORKFLOW=$REPO/.github/workflows/ci.yml
STATE=/var/lib/renoxium-build
SRC=$STATE/src
DL=$STATE/download
BIN=/opt/renoxium/renoxium
ASSET=renoxium-linux-amd64
as_build() { runuser -u renoxium-build -- env HOME=$STATE PATH=/usr/local/go/bin:/usr/bin:/bin GOTOOLCHAIN=local GH_NO_UPDATE_NOTIFIER=1 "$@"; }

UNIT=/etc/systemd/system/renoxium.service
PENDING=/etc/renoxium/renoxium.service.pending # root only: the build user must not be able to change it

# The service unit as this host wants it: the repository's, with this host's
# two settings. The file is read as the build user, so a symlink in the
# repository cannot make root read a file of its own.
render_unit() {
  as_build cat "$SRC/deploy/renoxium.service" | sed -e "s|-base-url https://renoxium.com|-base-url https://www.renoxium.com|" \
      -e "s|  -workers 2|  -workers 4|" \
      -e "s|# -workers 2 suits a 1 GB node; use 4 on 2 GB or more.|# -workers 4: this host has 8 GB of RAM.|"
}

# --install-unit: a person installs the unit the last deploy left pending.
if [ "${1:-}" = "--install-unit" ]; then
  [ -f "$PENDING" ] || { echo "no unit is pending"; exit 0; }
  # What is shown is exactly what is installed.
  review=$(mktemp)
  cp "$PENDING" "$review"
  diff -u "$UNIT" "$review" || true
  read -r -p "Install this unit and restart renoxium? [y/N] " answer
  [ "$answer" = "y" ] || { rm -f "$review"; echo "not installed"; exit 1; }
  cp -p "$UNIT" "$UNIT.prev"
  install -m 644 "$review" "$UNIT"
  rm -f "$review" "$PENDING"
  systemctl daemon-reload
  systemctl restart renoxium
  echo "unit installed; the previous one is $UNIT.prev"
  exit 0
fi

if ! command -v gh >/dev/null || ! gh attestation verify --help >/dev/null 2>&1; then
  echo "deploy: gh 2.49 or newer is needed to verify builds; nothing deployed" >&2
  exit 1
fi

as_build git -C "$SRC" fetch -q origin main
NEW=$(as_build git -C "$SRC" rev-parse origin/main)
CUR=$(cat $STATE/deployed 2>/dev/null || true)
[ "$NEW" = "$CUR" ] && exit 0
if grep -qx "$NEW" $STATE/failed 2>/dev/null; then exit 0; fi

TAG=build-${NEW:0:12}
URL=https://github.com/$REPO/releases/download/$TAG
as_build rm -rf "$DL"
as_build mkdir -p "$DL"
# Until CI has published the binary (or when its tests failed) there is none:
# say so once per commit and look again next minute.
if ! as_build curl -fsSL --retry 2 -o "$DL/$ASSET.sha256" "$URL/$ASSET.sha256" 2>/dev/null; then
  if [ "$(cat $STATE/waiting 2>/dev/null || true)" != "$NEW" ]; then
    echo "deploy: waiting for CI to publish $TAG ($(as_build git -C "$SRC" log -1 --format=%s "$NEW"))"
    echo "$NEW" > $STATE/waiting
  fi
  exit 0
fi

echo "deploy: ${CUR:0:8} -> ${NEW:0:8} ($(as_build git -C "$SRC" log -1 --format=%s "$NEW"))"
fail() { echo "deploy: FAILED at $1 for ${NEW:0:8}; staying on ${CUR:0:8}"; echo "$NEW" >> $STATE/failed; exit 1; }

as_build curl -fsSL --retry 3 -o "$DL/$ASSET" "$URL/$ASSET" || { echo "deploy: download of $TAG failed; trying again next minute"; exit 1; }
( cd "$DL" && as_build sha256sum -c --quiet "$ASSET.sha256" ) || fail checksum
# The binary must say it was built from this very commit, unmodified.
INFO=$(as_build go version -m "$DL/$ASSET") || fail buildinfo
grep -qP "\tvcs.revision=$NEW$" <<<"$INFO" || fail revision
grep -qP "\tvcs.modified=false$" <<<"$INFO" || fail revision
# The checksum comes from the same release as the binary, and the build info
# is whatever the binary says about itself: neither proves who built it.
# The signed provenance does: built by ci.yml, from this commit of main, on a
# GitHub-hosted runner. Anyone able to upload a release asset cannot sign
# that. Verified offline against the bundle; no GitHub login needed.
as_build curl -fsSL --retry 3 -o "$DL/$ASSET.sigstore.json" "$URL/$ASSET.sigstore.json" || fail provenance-download
as_build gh attestation verify "$DL/$ASSET" --bundle "$DL/$ASSET.sigstore.json" \
  --repo "$REPO" --signer-workflow "$WORKFLOW" --source-ref refs/heads/main --source-digest "$NEW" \
  --deny-self-hosted-runners >/dev/null || fail provenance
as_build git -C "$SRC" checkout -q --detach "$NEW" || fail checkout

/opt/renoxium/backup.sh >/dev/null || fail backup
# A changed unit is only reported: see the top of this file.
(umask 077 && render_unit > "$PENDING.new") || fail unit
if cmp -s "$PENDING.new" "$UNIT"; then
  rm -f "$PENDING.new" "$PENDING"
else
  mv "$PENDING.new" "$PENDING"
  echo "deploy: the repository's renoxium.service differs from the installed one; it is NOT installed. Review:"
  diff -u "$UNIT" "$PENDING" | sed 's/^/deploy:   /' || true
  echo "deploy: then install it with: sudo /opt/renoxium-deploy/deploy.sh --install-unit"
fi
cp -p "$BIN" "$BIN.prev"
install -m 755 "$DL/$ASSET" "$BIN"
systemctl restart renoxium
for i in $(seq 1 30); do
  if curl -fsS -m 2 http://127.0.0.1:8080/healthz >/dev/null 2>&1; then
    echo "$NEW" > $STATE/deployed
    echo "deploy: OK ${NEW:0:8} is live"
    exit 0
  fi
  sleep 1
done
echo "deploy: ${NEW:0:8} not healthy after 30 s; rolling back"
install -m 755 "$BIN.prev" "$BIN"
systemctl restart renoxium
fail health
