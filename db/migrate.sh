#!/usr/bin/env sh
# Applies db/migrations/*.sql in filename order (forward-only, numbered: 0001_name.sql).
# Each file runs in a single transaction; applied versions tracked in schema_migrations.
set -eu

COMPOSE="${COMPOSE:-docker compose}"
PSQL="$COMPOSE exec -T postgres psql -v ON_ERROR_STOP=1 -U ${POSTGRES_USER:-pulsar} -d ${POSTGRES_DB:-pulsar}"

$PSQL -q -c "CREATE TABLE IF NOT EXISTS schema_migrations (version text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now());"

applied_any=
for f in db/migrations/*.sql; do
  [ -e "$f" ] || { echo "migrate: no migrations in db/migrations/"; exit 0; }
  v=$(basename "$f")
  if [ -z "$($PSQL -tA -c "SELECT 1 FROM schema_migrations WHERE version = '$v'")" ]; then
    echo "migrate: applying $v"
    $PSQL -q -1 -f - < "$f"
    $PSQL -q -c "INSERT INTO schema_migrations (version) VALUES ('$v');"
    applied_any=1
  fi
done
[ -n "$applied_any" ] || echo "migrate: up to date"
