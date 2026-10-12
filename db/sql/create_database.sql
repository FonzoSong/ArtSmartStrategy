-- Run with psql while connected to the maintenance database (usually postgres).
-- Example: psql -U "$POSTGRES_USER" -d postgres -v ON_ERROR_STOP=1 -f this-file
\getenv target_database POSTGRES_DB
SELECT format('CREATE DATABASE %I WITH ENCODING %L TEMPLATE template0', :'target_database', 'UTF8')
WHERE NOT EXISTS (
    SELECT 1 FROM pg_database WHERE datname = :'target_database'
)
\gexec
