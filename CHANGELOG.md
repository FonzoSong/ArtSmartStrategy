# Changelog

Notable project changes are recorded here.

## Unreleased

- Add Go API and independent TypeScript/Vite frontend.
- Add PostgreSQL migrations and an explicit SQLite history importer.
- Update Docker Compose deployment and Go/Node CI checks.

# v0.4.0

- Load and validate the read-only project ruleset for content actions and
  schedule recommendations.
- Remove first-start demo data and require explicit, idempotent test-data
  injection from a versioned fixture.
- Add optional Docker test-data injection and persistent application data mount.
- Keep published performance history separate from unpublished schedule
  candidates.
