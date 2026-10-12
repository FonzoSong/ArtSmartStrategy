#!/bin/sh
set -eu

if [ "${LOAD_EXAMPLE_DATA:-true}" = "true" ]; then
  psql \
    --username "$POSTGRES_USER" \
    --password "APP_DB_PASSWORD" \
    --dbname "$POSTGRES_DB" \
    --set ON_ERROR_STOP=1 \
    --file /docker-entrypoint-sql/02_seed_data.sql
else
  echo "Skipping optional example rows; application migrations remain enabled."
fi
