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
echo "$ROOT/renoxium-$STAMP.tar.gz"
