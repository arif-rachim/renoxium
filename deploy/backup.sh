#!/usr/bin/env bash
# Consistent backup of every Renoxium SQLite database while the server runs.
# Usage: backup.sh [data-dir] [backup-root] [days-to-keep]
set -euo pipefail
# Archives hold every visitor account and order: readable by root only.
umask 077

DATA="${1:-/var/lib/renoxium}"
ROOT="${2:-/var/backups/renoxium}"
KEEP_DAYS="${3:-14}"
STAMP="$(date -u +%Y%m%d-%H%M%S)"
DEST="$ROOT/$STAMP"

mkdir -p "$DEST"
cd "$DATA"
# .backup reads through SQLite, so it is safe while the databases are in use.
# Preview databases are throwaway copies of live data and are skipped.
find . -name '*.db' ! -name 'preview.db' -print0 | while IFS= read -r -d '' db; do
  mkdir -p "$DEST/$(dirname "$db")"
  sqlite3 "$db" ".backup '$DEST/$db'"
done
tar -C "$ROOT" -czf "$ROOT/renoxium-$STAMP.tar.gz" "$STAMP"
rm -rf "$DEST"
find "$ROOT" -name 'renoxium-*.tar.gz' -mtime +"$KEEP_DAYS" -delete
ARCHIVE="$ROOT/renoxium-$STAMP.tar.gz"
echo "$ARCHIVE"

# Off-site copy: set RENOXIUM_BACKUP_REMOTE (an rclone remote and path, e.g.
# "r2:renoxium-backups") in /etc/renoxium/env and configure that remote with
# `sudo rclone config`. Database archives go to <remote>/db and are kept 30
# days there; uploaded images go to <remote>/files. Images never change, so
# only new ones are sent.
ENV_FILE="${RENOXIUM_ENV_FILE:-/etc/renoxium/env}"
if [ -z "${RENOXIUM_BACKUP_REMOTE:-}" ] && [ -r "$ENV_FILE" ]; then
  RENOXIUM_BACKUP_REMOTE="$(sed -n 's/^RENOXIUM_BACKUP_REMOTE=//p' "$ENV_FILE" | tail -n1)"
fi
if [ -n "${RENOXIUM_BACKUP_REMOTE:-}" ]; then
  REMOTE="${RENOXIUM_BACKUP_REMOTE%/}"
  if ! command -v rclone >/dev/null; then
    echo "RENOXIUM_BACKUP_REMOTE is set but rclone is not installed: sudo apt-get install -y rclone" >&2
    exit 1
  fi
  rclone copy --no-traverse "$ARCHIVE" "$REMOTE/db/"
  rclone delete --min-age 30d "$REMOTE/db/" || true
  if [ -d "$DATA/files" ]; then
    rclone copy --ignore-existing --exclude 'tmp/**' "$DATA/files" "$REMOTE/files"
  fi
  echo "copied to $REMOTE"
fi
