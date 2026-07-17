# User Usage Summary Tab Design

## Goal

Keep the existing administrator-facing **User Analytics** dashboard focused on
its filters and charts, and expose the per-user model-usage summary in a new,
dedicated **User Usage Summary** dashboard tab.

## Scope

- Add an administrator-only `/dashboard/user-usage` section titled **User
  Usage Summary**.
- Remove the model-usage summary from `/dashboard/users` without changing its
  existing controls or charts.
- Give the new section its own user selection, date range, source, model,
  sort, and pagination state.
- Reuse the existing model-usage API and summary/table components. No backend,
  data model, or permission changes are required.

## Layout and Behavior

### User Analytics (`/dashboard/users`)

- Retains its existing date range, chart granularity, metric, top-user limit,
  user-search/selection controls, and user charts.
- Does not render `UserModelUsageSummary`.
- Its URL search state remains local to this route and keeps the existing
  semantics.

### User Usage Summary (`/dashboard/user-usage`)

- Renders a focused filter card with user search/selection and date range
  controls only.
- Renders the existing `UserModelUsageSummary` below that card. The summary
  continues to own source filtering, model-name filtering, sorting, pagination,
  refresh, empty, error, and aggregation-status states.
- Does not render chart-only controls: time granularity, metric, or top-user
  limit.
- Uses the same query-key names as the existing summary because the route path
  is different. Therefore selection and filters are retained independently for
  each tab and cannot leak when switching tabs.

## Architecture

- Register a `user-usage` dashboard section alongside `models` and `users`.
  It is administrator-only and appears as a peer tab in the dashboard tab bar.
- Split the current `UserAnalytics` composition into two page components:
  `UserAnalytics` keeps chart analytics, while a new user-usage page component
  manages the summary tab's independent route state and selected-user metadata.
- Extract or parameterize the reusable user/date selection controls so the
  two pages share user-search behavior without coupling their state or showing
  chart-only controls in the summary tab.
- Continue using `userAnalyticsSearchSchema` for both routes. It validates the
  common summary parameters and keeps deep links valid without a new backend
  contract.

## Access and Error Handling

- The new section is visible only to administrators, matching the current user
  analytics and server-side summary authorization.
- An unselected user shows the existing summary empty state.
- Existing API errors, disabled-collection warnings, and empty-result behavior
  remain unchanged inside `UserModelUsageSummary`.

## Verification

- Add a focused regression test that asserts the dashboard registry exposes
  `user-usage` as an administrator-only section.
- Add a focused composition test that asserts the User Analytics page no
  longer renders the summary and the dedicated page does.
- Run the focused Bun tests, TypeScript typecheck, and production build.

## Non-Goals

- No backend endpoint, database, data migration, or API response change.
- No change to chart calculations, summary aggregation semantics, permissions,
  or existing query parameter meanings.
