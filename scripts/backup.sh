#!/bin/sh
# Backup SQLite DB — run daily via cron
# Usage: ./scripts/backup.sh /path/to/data/dir /path/to/backup/dir

DATA_DIR="${1:-/app/data}"
BACKUP_DIR="${2:-/app/backups}"
DB="$DATA_DIR/app.db"

if [ ! -f "$DB" ]; then
  echo "DB not found: $DB"
  exit 1
fi

mkdir -p "$BACKUP_DIR"
TIMESTAMP=$(date +%Y%m%d-%H%M%S)
cp "$DB" "$BACKUP_DIR/app-$TIMESTAMP.db"

# Keep only last 30 days
find "$BACKUP_DIR" -name "app-*.db" -mtime +30 -delete
echo "Backup saved: $BACKUP_DIR/app-$TIMESTAMP.db"
