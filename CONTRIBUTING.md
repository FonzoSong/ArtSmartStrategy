# Contributing

Thanks for helping improve 艺数智策. Keep changes focused and preserve the
documented HTTP API, authorization rules, and PostgreSQL schema compatibility.

## Development setup

Use Python 3.10 or newer and PostgreSQL 16 or a compatible release. Copy
`.env.example` to `.env`, configure a dedicated development database, then run:

```bash
python -m venv .venv
. .venv/bin/activate
pip install -e ".[dev]"
alembic upgrade head
```

On Windows, use `py -3 -m venv .venv` and activate `.venv\\Scripts\\activate`.

## Checks

Before opening a pull request, run:

```bash
ruff check .
ruff format --check .
mypy backend
pytest -q --cov --cov-report=term-missing
```

Do not point tests or reset scripts at production data. Add or update tests for
behavior changes and migrations. Keep database changes additive where possible.
The current 80% gate measures the extracted settings, database, model, schema,
security, and status modules; the legacy route module is excluded until its
handlers have isolated PostgreSQL integration coverage.

## Pull requests

Explain the user-visible behavior, compatibility impact, migration requirements,
and checks run. Include screenshots for meaningful UI changes. Do not include
credentials, personal data, generated database files, or unrelated formatting.
