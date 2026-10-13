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
- Fix performance booking: `POST /api/performances` now accepts the browser's
  `datetime-local` start time (it previously rejected every submission as invalid
  JSON), and the booking list returns real linked-content counters.
- Add 正式演出 management on the existing `performance_booking` entity: detail,
  update and delete endpoints, `performance_type` / `ticket_link` fields, and the
  canonical 筹备中 / 预热中 / 已演出 / 已取消 status set (migration
  `0008_performance_management`). A show whose start time has passed is settled to
  已演出 server-side; 已取消 is terminal and is never rewritten.
- Add the 正式演出 page: show list with 距开演天数 and persisted 预热视频/排期 counts,
  a detail view that aggregates published assets with their real effect data, the
  unpublished asset list, the pre-heat timeline, and a copy of the link/unlink
  and create-video actions.
- Add whole-show scheduling (`POST /api/performances/{id}/plan`): plans only the
  show's unpublished 待排期 videos, never past the curtain, spread across days and
  platforms, with reasons that cite the team's own history and that state
  "样本不足" when the history is missing. Plans are stored as normal
  recommendations and adopted through the existing `POST /api/schedule/adopt`, so
  there is no second scheduling model.
- Record manual plan changes: an edited plan is marked `source=manual`, keeps its
  before/after values in the audit log, and can be reviewed in the UI
  (`GET /api/schedule/{id}/adjustments`).
- Add the 演出 dimension to the operations overview (筹备中演出数、最近一场距开演天数、
  未发布预热视频数、整场排期完成率), a per-show comparison on the analysis page, and a
  演出 filter on both the suggestion page and the calendar.
- Add an independent Home page (`#home`) as the product front page: brand
  navigation, editorial layout, two real entry points, and four indicators that
  come from existing statistics APIs. The page ships a locally maintained
  cyan-green ink-wash SVG (`web/public/art-home-ink-ridge.svg`).
- Fix the API client so `204 No Content` responses no longer surface as failures:
  deleting content, a tag, a rule set, a rule or a schedule item now reports
  success, and the lists refresh.
- Fix deleting a scheduled content record: the record's plans are removed in the
  same transaction (previously a foreign-key violation returned HTTP 500).
- Fix the planning-window boundary in a non-UTC deployment: the last day of the
  horizon is no longer generated and then rejected at adoption, because the date
  is now compared in the window's own timezone.
- Make the rule-set endpoints unambiguous: custom and system rule sets have
  independent id sequences, so mutations accept an explicit `kind` and refuse
  `kind=system` outright. The API client sends `kind=custom` for custom sets.
- Align the deployment timezone (`TZ`, default `Asia/Shanghai`) across the API and
  database, and compare show start times against the application clock, so the
  automatic 已演出 transition follows the team's wall clock.
- Configure the frontend backend address in one place: `VITE_API_BASE_URL` (empty
  means same-origin `/api`) plus `VITE_DEV_API_TARGET` for the development proxy.
  `web/.env` no longer ships a Docker-internal service name, and the client
  normalizes a bare host into a real URL.
- Add Windows `web/start.bat`: environment checks, dependency install only when
  missing, `.env` creation without overwriting, dev server on a free port, and a
  browser that opens only after the server is actually ready.
- Rewrite `db/README.md` from the real schema and add `web/README.md`.
- Home 首页口号改为「以策传艺 / 以数入戏」。
- Add `db/sql/03_example_team_contents.sql`: an idempotent, team-scoped script that
  tops a team up to 120 example content records (marked `source='example'`), so a
  freshly created team can be inspected with realistic data volume without
  touching existing rows, accounts, shows or plans.

# v0.4.0

- Load and validate the read-only project ruleset for content actions and
  schedule recommendations.
- Remove first-start demo data and require explicit, idempotent test-data
  injection from a versioned fixture.
- Add optional Docker test-data injection and persistent application data mount.
- Keep published performance history separate from unpublished schedule
  candidates.
