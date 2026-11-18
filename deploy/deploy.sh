#!/usr/bin/env bash
# Auto-deploy of github.com/arif-rachim/renoxium from the binaries CI builds.
#
# When main has a new commit, wait for the release build-<12 characters of
# the commit>, which .github/workflows/ci.yml publishes after the Go tests and
# the end-to-end suite pass. Download it as the unprivileged renoxium-build
# user, check its checksum and that it was built from exactly that commit,
# back up, install, restart, and roll back if /healthz does not answer within
# 30 s. A commit whose binary fails the checks or the health check is not
# retried. Nothing is compiled or tested on the server.
#
# Installed as /opt/renoxium-deploy/deploy.sh, run by renoxium-deploy.timer
# every minute. Host settings are below and in the sed lines for the unit.
set -euo pipefail
exec 9>/run/renoxium-deploy.lock
flock -n 9 || { echo "another deploy is running"; exit 0; }

REPO=arif-rachim/renoxium
STATE=/var/lib/renoxium-build
SRC=$STATE/src
DL=$STATE/download
BIN=/opt/renoxium/renoxium
ASSET=renoxium-linux-amd64
as_build() { runuser -u renoxium-build -- env HOME=$STATE PATH=/usr/local/go/bin:/usr/bin:/bin GOTOOLCHAIN=local "$@"; }

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
as_build git -C "$SRC" checkout -q --detach "$NEW" || fail checkout

/opt/renoxium/backup.sh >/dev/null || fail backup
# The service unit follows the repository, with this host's two settings.
UNIT=/etc/systemd/system/renoxium.service
sed -e "s|-base-url https://renoxium.com|-base-url https://www.renoxium.com|" \
    -e "s|  -workers 2|  -workers 4|" \
    -e "s|# -workers 2 suits a 1 GB node; use 4 on 2 GB or more.|# -workers 4: this host has 8 GB of RAM.|" \
    "$SRC/deploy/renoxium.service" > $STATE/renoxium.service.new || fail unit
if ! cmp -s $STATE/renoxium.service.new "$UNIT"; then
  cp -p "$UNIT" "$UNIT.prev"
  install -m 644 $STATE/renoxium.service.new "$UNIT"
  systemctl daemon-reload
  echo "deploy: renoxium.service updated from the repository"
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
if [ -f "$UNIT.prev" ] && ! cmp -s "$UNIT.prev" "$UNIT"; then install -m 644 "$UNIT.prev" "$UNIT"; systemctl daemon-reload; fi
systemctl restart renoxium
fail health
