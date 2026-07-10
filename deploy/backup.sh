#!/usr/bin/env bash
set -euo pipefail

APP_DIR="${APP_DIR:-/opt/musicweb/app}"
BACKUP_DIR="${BACKUP_DIR:-/opt/musicweb/backups}"
KEEP_DAYS="${KEEP_DAYS:-14}"
stamp="$(date -u +%Y%m%dT%H%M%SZ)"
mkdir -p "$BACKUP_DIR"

for db in "$APP_DIR"/data/*.db "$APP_DIR"/*.db; do
  [[ -f "$db" ]] || continue
  sqlite3 "$db" ".backup '$BACKUP_DIR/$(basename "$db").$stamp'"
done

tar -C "$APP_DIR" -czf "$BACKUP_DIR/config.$stamp.tar.gz" config.ini 2>/dev/null || true
find "$BACKUP_DIR" -type f -mtime "+$KEEP_DAYS" -delete
