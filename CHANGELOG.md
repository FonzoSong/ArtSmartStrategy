# Changelog

Notable project changes are recorded here.

## Unreleased

- Add Go API and independent TypeScript/Vite frontend.
- Add PostgreSQL migrations and an explicit SQLite history importer.
- Update Docker Compose deployment and Go/Node CI checks.
- Fix the operations overview contract: `/api/stats/overview` now returns
  `total_posts`, `platform_count`, `total_interactions` and a ranked
  `top_contents` list, and the page normalizes missing or empty values instead of
  failing at runtime.
- Serialize calendar plan dates as pure `YYYY-MM-DD` values at the API boundary
  and normalize API dates in the client, so existing plans appear in the right
  date cell and monthly list.
- Define `planning_horizon_days` as "today through today + N - 1" and share one
  implementation across recommendation generation, manual scheduling, adoption
  validation and AI output validation.
- Add per-suggestion adoption, server-side suggestion pre-validation with
  readable reasons, business titles on suggestion cards, and conflict feedback
  when a target already has a plan.
- Expose the read-only system rule set (`GET /api/rules/system`) next to the
  team rules, including each team rule's most recent execution.
- Format every percentage-valued metric with two decimals and a clear missing state.
- Accept `datetime-local` timestamps when creating content, matching the update API.
- Restore the team id sequence for demo-seeded databases (migration
  `0004_restore_team_id_sequence`) so new team registrations succeed.
- Add `POST /api/contents/advance-bulk` and a batch workflow action on the content
  records page: selected 待制作 records advance to 已制作 with per-record results,
  refreshed lists and failed records kept selected.
- Replace every browser-native `alert`/`confirm`/`prompt` with the shared modal
  confirmation component, including bulk actions and Tag merge.
- Restrict schedule suggestions to `已制作` content in the query, the adoption
  validation and the client pre-check, and show the `当前无制作完毕视频` empty state
  when no finished video can be suggested.
- Reorder the content records filter tabs to 全部 → 待制作 → 已制作 → 待排期 → 已排期
  → 未发布 → 已发布.
- Reload the suggestion list from the server after generate, remove, adopt and
  reschedule operations instead of mutating the client copy.

# v0.4.0

- Load and validate the read-only project ruleset for content actions and
  schedule recommendations.
- Remove first-start demo data and require explicit, idempotent test-data
  injection from a versioned fixture.
- Add optional Docker test-data injection and persistent application data mount.
- Keep published performance history separate from unpublished schedule
  candidates.
