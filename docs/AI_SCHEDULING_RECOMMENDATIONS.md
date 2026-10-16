# AI scheduling recommendations

## Scope and eligibility

The application has no separate project table. In this API, `projectId` is the
active team ID and must match the authenticated user's team. The server, not the
browser, selects records in the `未发布 + 待排期` state and excludes records
that already have a pending schedule. All content and performance queries are
team-scoped.

Published history is sent only when it shares at least one Tag with a candidate.
The AI input contains available plays, likes, comments, shares, favorites,
completion, and conversion metrics; means, medians, and recent-window trends are
computed from stored values. Unsupported metrics such as average watch time are
not inferred. Prompt data is bounded and long titles/tags are truncated.

## Generate a draft

```http
POST /api/ai/recommend
Authorization: Bearer <JWT>
X-Team-Id: <teamId>
Content-Type: application/json
```

```json
{
  "projectId": 12,
  "contentIds": [],
  "strategy": "balanced",
  "performanceId": "optional-performance-id",
  "constraints": {
    "startDate": "2026-10-17",
    "workingDays": [1, 2, 3, 4, 5],
    "dailyCapacityHours": 8,
    "includeWeekends": false
  }
}
```

`projectId` and a JSON body are required. `contentIds` is optional; when empty,
the backend resolves all eligible content under the optional performance filter.
When provided, every ID must be eligible and owned by the authenticated team or
the whole request is rejected. `workingDays` uses Monday=1 through Sunday=7 and
can only narrow the team's schedule policy. `startDate` must fall inside the
policy window. The configured per-day and per-platform limits remain
authoritative; `dailyCapacityHours` is contextual input because this product
does not model per-content production duration.

The response is a persisted draft with `id`, `analysis_status`, `source_label`,
`fallback_reason`, and `recommendations`. Each recommendation contains the
target content ID, date, time slot, priority order and level, reason, confidence,
same-Tag sample count, and risk notes. Invalid model JSON is retried once. If it
still fails schema or policy validation, capped raw model output is retained in
server diagnostics and the endpoint falls back to the local scheduling rules.

## Read, edit, apply, and revert

- `GET /api/ai/recommend/{id}` reads one team-scoped draft.
- `POST /api/ai/recommend/{id}/regenerate` accepts the same JSON request and
  creates a new draft while preserving the earlier one.
- `PATCH /api/recommendations/{id}` saves user-edited recommendation items.
- `POST /api/ai/recommend/{id}/apply` applies confirmed `items` transactionally.
- `POST /api/ai/recommend/{id}/revert` reverses only schedule rows that have
  not since been changed or published.

The existing `POST /api/schedule/adopt` and
`POST /api/recommendations/{id}/revert` paths remain available for older clients.

Generation, edits, application, and reversion write audit entries. Set
`AI_MOCK=true` for deterministic local/CI recommendations; `AI_MOCK_MODE` is
accepted as a backward-compatible alias. Configure a real provider with
`AI_BASE_URL`, `AI_MODEL`, and `AI_API_KEY`; the key is read only by the backend.
Set `AI_RECOMMENDATION_TIMEOUT` to control the maximum model wait (Go duration,
default `30s`, accepted range `1s` to `10m`). When the provider exceeds this
limit, the API uses the already-built local rule suggestions, saves the draft
and audit entry, and returns the generated recommendation successfully. The
generation request receives two additional `REQUEST_TIMEOUT` windows for
loading request data and persisting the fallback after the model wait expires.
