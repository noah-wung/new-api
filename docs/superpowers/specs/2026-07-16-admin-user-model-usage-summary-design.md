# Admin User Model Usage Summary Design

- Date: 2026-07-16
- Status: Approved (amended: model identity is case-insensitive)
- Scope: Add per-user, per-model usage summaries to the administrator User Analytics dashboard in `web/default`

## 1. Background

The administrator User Analytics dashboard currently shows user-level consumption ranking and trends. The underlying data already preserves Gateway usage by user, model, and hourly bucket, while external usage aggregates preserve user, concrete source, normalized model, event count, and token volume.

Administrators need to select a user and inspect that user's usage grouped by a case-insensitive canonical model identity, without losing the distinction between Gateway, Cursor, Codex, and future external sources.

## 2. Goals

1. Let administrators select any current, disabled, or soft-deleted user.
2. Show that user's usage grouped by case-insensitive canonical model identity for a selected time range.
3. Keep total Token usage and Quota visibly distinct.
4. Preserve concrete usage sources in both filtering and row-level detail.
5. Keep Gateway request count separate from external usage event count.
6. Support accurate full-result sorting, searching, totals, and pagination.
7. Keep the new query compatible with SQLite, MySQL >= 5.7.8, and PostgreSQL >= 9.6.

## 3. Non-Goals

1. Do not implement this feature in `web/classic`.
2. Do not add CSV or other exports in the first release.
3. Do not add per-model time-series charts in the first release.
4. Do not expose input, cached-input, output, or reasoning-token breakdowns.
5. Do not heuristically combine similar or versioned model names; casing variants intentionally share one identity.
6. Do not redesign or repair asynchronous task settlement aggregation in this scope.
7. Do not replace the existing periodic dashboard aggregation path with log scans or real-time analytics.

## 4. Domain Semantics

The canonical terms and relationships live in `CONTEXT.md`.

### 4.1 Metrics

- **Token usage** is the total raw token volume. It includes Gateway and retained external usage.
- **Quota** is billing consumption. External usage contributes no Quota.
- **Gateway request count** counts Gateway requests only.
- **External usage event count** counts retained external usage events only and is never added to Gateway request count.

The model summary exposes four headline totals:

1. total Token usage;
2. total Quota;
3. Gateway request count;
4. external usage event count.

### 4.2 Model Identity

- Gateway usage groups by a lowercase canonical form of the recorded Gateway model name.
- External usage groups by a lowercase canonical form of its normalized model name.
- Casing differences merge; similar names and version suffixes remain separate.
- An external model joins another model identity only through an explicit administrator model mapping.
- Unmapped external models remain visible under their source-qualified fallback names and are marked as unmapped.

### 4.3 Source Identity

Concrete sources are preserved, including Gateway, Cursor, Codex, and future external sources. A combined model total never erases its source contribution.

Disabling external usage stops new collection and workspace actions but does not hide retained historical external aggregates from administrators.

## 5. Administrator Experience

### 5.1 Entry and Selection

The existing User Analytics section remains the entry point.

- No user is selected by default.
- Existing global user ranking and trend charts remain visible.
- Administrators can select a user through a searchable selector.
- Clicking a user in the ranking chart selects the same user as the selector.
- Search and chart selection synchronize to one selected-user state.
- The selector includes active, disabled, and soft-deleted users and displays their status.

### 5.2 Shared Filters

The entire User Analytics view shares one time range and one primary metric.

- Existing presets remain: 1, 7, 14, 30, and 90 days.
- A custom start and end date is added.
- A single query range cannot exceed 90 days.
- Custom dates use the administrator browser's local timezone.
- The UI displays the active timezone next to the date control.
- Hour/day/week granularity affects trend charts only; the model table always aggregates the complete selected range.
- The existing Quota/Tokens switch controls global charts and the model table's default sort.
- Initial metric and sort remain Quota descending.
- An explicit table-header sort overrides automatic metric-linked sorting until the administrator changes it again.

### 5.3 Source Filter

- Add a multi-select concrete-source filter.
- All available sources are selected by default.
- Source options come from data actually present for the selected user and time range, not only currently allowed collection sources.
- The source filter affects the headline totals and model rows.
- Source selection is persisted in the URL.

### 5.4 Model Summary

The selected user's model summary is a server-driven table.

- One parent row per case-insensitive canonical model identity, returned in lowercase.
- Columns: model, Token usage, Quota, Gateway requests, external usage events.
- Parent rows contain totals across selected concrete sources.
- Expanding a parent row shows one row per contributing concrete source.
- Gateway source rows show Token usage, Quota, and Gateway requests.
- External source rows show Token usage and external usage events.
- A non-applicable source metric displays `—`, not `0`.
- Real zero values on parent rows and headline totals display as `0`.
- Source detail for the current model page arrives inline with the page response.

### 5.5 Searching, Sorting, and Pagination

- Model search is a case-insensitive literal substring search.
- `%`, `_`, and other SQL wildcard characters are treated as ordinary characters.
- Search, source filtering, and sorting are performed over the complete matching result set.
- Headline totals include all matching rows across every page.
- Default page size is 20; supported sizes are 20, 50, and 100.
- Supported sorts include model name, Token usage, Quota, Gateway requests, and external usage events.
- Numeric sorts default to descending; model name defaults to ascending.

### 5.6 URL State

The selected `user_id`, time range, primary metric, metric-linked versus manual sort mode, source selection, model search, sort, and page state are represented in URL query parameters.

- Refreshing restores the same analysis view.
- Links can be shared with another authorized administrator.
- Stable user ID is used instead of username.

### 5.7 Refresh and Status

- Selection or filter changes request data automatically.
- Client data is cached for 60 seconds.
- A manual refresh action is available.
- No short-interval polling is added because dashboard aggregation is periodic and defaults to five minutes.

The page displays a data-completeness warning when any relevant collection path is disabled:

- dashboard aggregation disabled;
- consume-log recording disabled;
- external usage collection disabled.

Historical data remains visible when collection is disabled. A warning and an empty state may appear together so incomplete collection is not mistaken for zero use.

## 6. Empty and Error States

The UI distinguishes:

1. no selected user — prompt the administrator to search or click the ranking chart;
2. no usage in the selected range — show no usage records;
3. no models matching current filters — offer to clear model or source filters;
4. request failure — show the error and a retry action.

An invalid or inaccessible `user_id` in the URL produces a clear not-found state rather than silently selecting another user.

## 7. Backend API

Add an administrator-only endpoint:

```text
GET /api/data/users/:user_id/models
```

It uses `middleware.AdminAuth()` and does not modify the existing `/api/data/users` contract.

### 7.1 Query Parameters

- `start_timestamp` — required Unix seconds;
- `end_timestamp` — required Unix seconds;
- `sources` — optional repeated or comma-separated concrete source identifiers;
- `model_search` — optional literal substring;
- `sort_by` — `model_name`, `token_usage`, `quota`, `gateway_requests`, or `external_events`;
- `sort_order` — `asc` or `desc`;
- `p` — one-based page number;
- `page_size` — 20, 50, or 100.

The endpoint rejects malformed ranges, reversed ranges, and ranges over 90 days with a clear client error. It does not silently clamp input.

### 7.2 Response Shape

The response includes:

- minimal target user identity: ID, username, display name, status, and deleted state;
- normalized effective filters;
- available concrete sources for the user and time range;
- full-filter totals independent of pagination;
- paginated model rows;
- inline source rows for each returned model;
- collection-status metadata and configured dashboard refresh interval;
- pagination metadata.

Conceptual shape:

```json
{
  "success": true,
  "data": {
    "user": {
      "id": 42,
      "username": "alice",
      "display_name": "Alice",
      "status": 1,
      "deleted": false
    },
    "available_sources": ["gateway", "cursor", "codex"],
    "totals": {
      "token_usage": 120000,
      "quota": 85000,
      "gateway_requests": 91,
      "external_events": 37
    },
    "items": [
      {
        "model_name": "gpt-5",
        "unmapped": false,
        "token_usage": 70000,
        "quota": 60000,
        "gateway_requests": 60,
        "external_events": 20,
        "sources": [
          {
            "source": "gateway",
            "token_usage": 50000,
            "quota": 60000,
            "gateway_requests": 60,
            "external_events": null
          },
          {
            "source": "codex",
            "token_usage": 20000,
            "quota": null,
            "gateway_requests": null,
            "external_events": 20
          }
        ]
      }
    ],
    "collection_status": {
      "data_export_enabled": true,
      "consume_log_enabled": true,
      "external_usage_enabled": true,
      "refresh_interval_minutes": 5
    },
    "page": 1,
    "page_size": 20,
    "total": 1
  }
}
```

Nullable source metrics represent not-applicable values and render as `—`.

## 8. Aggregation Design

Use two GORM aggregation queries and merge their results in Go.

### 8.1 Gateway Query

Read `quota_data` for the selected user and time range, grouped by recorded `model_name`, then fold aggregate rows by lowercase model name in Go so database collation differences cannot change the result.

- `sum(token_used)` becomes Gateway Token usage;
- `sum(quota)` becomes Quota;
- `sum(count)` becomes Gateway request count.

### 8.2 External Query

Read `external_usage_aggregates` for the selected user and time range, grouped by `normalized_model_name` and concrete `source`.

Fold aggregate rows by concrete source plus lowercase model name in Go so database collation differences cannot change the result.

- `sum(total_tokens)` becomes external Token usage;
- `sum(event_count)` becomes external usage event count;
- no Quota or Gateway requests are produced.

### 8.3 Merge and Page

In Go:

1. combine source contributions by lowercase canonical model identity;
2. apply concrete source selection;
3. apply case-insensitive literal model search;
4. calculate totals across all matches;
5. apply deterministic sorting with model name as a tie-breaker;
6. paginate the sorted result;
7. attach the current page's already-aggregated source rows.

This avoids database-specific `UNION`, JSON aggregation, collation behavior, and pagination differences.

## 9. Database Changes

Add GORM-managed composite indexes:

- `quota_data(user_id, created_at, model_name)`;
- `external_usage_aggregates(user_id, bucket_at, normalized_model_name, source)`.

The indexes trade small write and storage overhead for bounded single-user range scans. No database-specific DDL is introduced.

## 10. Frontend Scope

Only `web/default` changes.

- Reuse the existing admin user search API and user types where practical.
- Extend the User Analytics state so chart clicks and user search share one selected user.
- Use the existing API client, TanStack Query, table primitives, date controls, translation hooks, and URL router state.
- Keep responsive behavior through the existing table pattern; narrow screens may horizontally scroll while model identity remains readable.
- Add English source keys and synchronize zh, fr, ru, ja, and vi translations using the project Bun i18n workflow.

## 11. Known Limitation

Asynchronous task settlement adjustments and refunds do not currently update `quota_data`. This feature deliberately inherits the existing dashboard aggregation semantics and does not claim to repair final-settlement reporting for those task paths.

## 12. Verification Expectations

Backend coverage must include:

- Admin authorization and non-admin rejection;
- active, disabled, and soft-deleted user lookup;
- 90-day validation and invalid range rejection;
- Gateway-only, external-only, and mixed-source aggregation;
- case-insensitive canonical model identity and explicit mapped identity behavior;
- unmapped external model visibility;
- separate Gateway request and external event counts;
- source filtering and dynamic available sources;
- literal case-insensitive model search including `%` and `_`;
- full-result totals independent of pagination;
- deterministic sorting and supported page sizes;
- collection-status metadata;
- SQLite-backed model/controller tests with cross-database-safe query construction.

Frontend coverage must include:

- chart click and search selector synchronization;
- URL state restoration;
- shared metric and time-range behavior;
- source filters and model search;
- parent rows and inline source expansion;
- `—` for non-applicable source metrics and `0` for real zeros;
- all empty, warning, error, and retry states;
- successful `web/default` type checking, tests, and production build with Bun.

## 13. Documentation Decision

No new ADR is proposed. The decisions are scoped API, query, and interface choices that are reversible and unsurprising in the context of the existing dashboard aggregation architecture. The durable domain language and source-accounting rules are recorded in `CONTEXT.md`.
