# Contributing

Thanks for helping improve 艺数智策. Keep changes focused and preserve the
documented HTTP API, authorization rules, and PostgreSQL schema compatibility.

## Development setup

Use Go 1.26+, Node.js 22+, npm, and Docker Compose. Copy `.env.example` to `.env`,
set the secret values, then start the services:

```bash
docker compose up --build
```

On Windows, run `start.bat`; it starts the same Compose services.

## Checks

Before opening a pull request, run:

```bash
go test ./cmd/api ./internal/config ./internal/domain ./internal/httpapi ./internal/migrate ./internal/repository ./internal/service
go vet ./cmd/api ./internal/config ./internal/domain ./internal/httpapi ./internal/migrate ./internal/repository ./internal/service
cd web && npm ci && npm run typecheck && npm run build
```

Do not run schema migrations against production data during development. Add or
update tests for behavior changes and database migrations. Keep schema changes
additive where possible. PostgreSQL integration checks require an isolated
database; unit tests must not use production credentials.

## Pull requests

Explain the user-visible behavior, compatibility impact, migration requirements,
and checks run. Include screenshots for meaningful UI changes. Do not include
credentials, personal data, generated database files, or unrelated formatting.
