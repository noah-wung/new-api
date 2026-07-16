# new-api

new-api is an AI API gateway context for routing provider calls, tracking usage, and billing users.

## Language

**Quota**:
A billing consumption unit charged to users for API usage.
_Avoid_: Token usage, raw tokens

**Token usage**:
Raw statistical token volume consumed by API calls.
_Avoid_: Quota, cost

**Model usage summary**:
An aggregate of a user's total **Token usage** and **Quota** grouped by model for a selected time range; Gateway request count and external usage event count are separate supplementary metrics, and token subcategories are outside the summary.
_Avoid_: Usage total, model cost

**Gateway operating metrics**:
Runtime metrics derived from gateway-handled API requests, such as request count, RPM, and TPM.
_Avoid_: External tool usage analytics

**External tool usage**:
Raw token usage produced outside the gateway by coding tools and later reported into the system for analytics.
_Avoid_: Quota, billing charge

**Reporting credential**:
A user-scoped credential issued by the system for a client background service to upload external tool usage.
_Avoid_: API token, provider account

**Reporting device**:
A user-owned device authorized to report client-collected external tool usage.
_Avoid_: Session, provider account

**External usage event**:
An auditable usage fact imported or reported from an external coding tool before aggregation.
_Avoid_: Gateway consume log

**Imported Cursor usage**:
Cursor usage facts loaded from an administrator-provided Cursor export.
_Avoid_: Client-reported usage

**Cursor import batch**:
An administrator-created import unit for Cursor usage that can be audited, reverted, or replayed as a whole.
_Avoid_: Anonymous upload

**Usage status**:
The source-reported billing or execution status of an external usage fact.
_Avoid_: Billing decision

**Source model name**:
The model name exactly as reported by an external coding tool.
_Avoid_: Normalized model

**Normalized model name**:
The analytics model name after applying source-specific mapping rules.
_Avoid_: Raw source label

**Private tool context**:
Local coding context that could reveal project, repository, file, prompt, or response content.
_Avoid_: Usage metadata

**Client-reported usage**:
Usage facts uploaded by a user-owned background service from local coding tool caches.
_Avoid_: Administrator import

**Supported client source**:
A coding tool source accepted by the client reporting protocol.
_Avoid_: Implemented parser

**Usage reporting agent**:
A client-side background program that reads local coding tool caches and uploads normalized usage deltas.
_Avoid_: Gateway server process

**Client usage delta**:
A deduplicated incremental usage fact derived locally from one or more tool cache snapshots.
_Avoid_: Raw snapshot

**Client report batch**:
A device upload unit for client usage deltas, used for acknowledgement, diagnostics, and idempotency.
_Avoid_: User rollback unit

**Reporting cursor**:
The client-side progress marker for usage deltas that have been acknowledged by the gateway.
_Avoid_: Server aggregate state

**Source-aware usage aggregate**:
An analytics aggregate that preserves where external token usage came from.
_Avoid_: Source-less total

**External usage detail**:
The row-level imported or reported external usage fact used for audit and diagnostics.
_Avoid_: User dashboard metric

**External usage workspace**:
The primary application area where users and administrators manage and inspect external tool usage.
_Avoid_: API logs

**External usage feature flag**:
The site-wide switch that controls new external usage collection, reporting, and external-usage workspace actions; disabling it does not hide retained historical aggregates from administrators.
_Avoid_: Per-source policy

**Usage aggregate retention**:
The long-term preservation of analytics totals after row-level external usage details age out.
_Avoid_: Event retention

**Usage aggregate rebuild**:
An administrator-triggered recalculation of external usage aggregates from retained usage facts.
_Avoid_: Manual counter edit

**Usage occurrence time**:
The time an external tool produced the usage, used for analytics bucket assignment.
_Avoid_: Upload time

**External event identity**:
The stable identity used to make an external usage event idempotent for one user and source.
_Avoid_: Timestamp-only duplicate check

## Relationships

- **Token usage** can contribute to **Quota** through model pricing and billing rules.
- **Quota** is the user-facing billing consumption value; **Token usage** is the raw usage statistic.
- A **Model usage summary** keeps **Token usage** and **Quota** distinct and may include request count as supporting context.
- A **Model usage summary** includes gateway and **External tool usage** token volume while preserving each concrete source, such as Gateway, Cursor, or Codex; external usage contributes no **Quota**.
- Request count in a **Model usage summary** counts Gateway requests only and never combines **External usage events** with API calls.
- A **Model usage summary** may display the number of **External usage events** in its own column, separate from Gateway request count.
- A **Model usage summary** groups model identities case-insensitively using a lowercase canonical name; similar names and versioned names remain separate unless an explicit external model mapping assigns the same **Normalized model name**.
- **Gateway operating metrics** are derived only from gateway-handled requests.
- **External tool usage** contributes to **Token usage** analytics but does not directly consume **Quota**.
- **External tool usage** is recorded as **External usage events** before being aggregated into analytics.
- **Imported Cursor usage** and **Client-reported usage** are separate fact sources with separate storage.
- **Client-reported usage** is uploaded as **Client usage deltas**, not as raw cache snapshots.
- **Source-aware usage aggregates** preserve external tool source while existing total usage analytics remain compatible.
- Each **External usage event** has one **External event identity** scoped by user and source.
- **Cursor** usage exports are imported only by administrators and assigned to an explicit **User**.
- **Imported Cursor usage** with valid positive tokens contributes to analytics regardless of **Usage status**.
- A client background service reports **External tool usage** only through a **Reporting credential** owned by the target **User** and bound to a **Reporting device**.
- A **User** can have at most three **Reporting devices**.
- **External tool usage** is assigned to analytics buckets by **Usage occurrence time**, while upload/import time is retained for audit.
- **External tool usage** retains **Source model name** and aggregates by **Normalized model name**.
- **Private tool context** is not uploaded; reporting uses hashes, aliases, or omitted values instead.
- **Imported Cursor usage** belongs to a **Cursor import batch** for audit and rollback.
- **Client-reported usage** belongs to a **Client report batch** for upload accounting, not ordinary user rollback.
- **External usage detail** is available for administrator diagnostics but ordinary users see devices and aggregates by default.
- The **External usage workspace** is a standalone General navigation item, not part of gateway usage logs.
- The **External usage feature flag** gates external-usage workspace access, device credential creation, imports, and report ingestion; administrators can still inspect retained historical totals through a **Model usage summary**.
- **Supported client sources** can be accepted by the protocol before each source has a bundled local parser.
- A **Usage reporting agent** runs on the user's device and communicates with the gateway through reporting APIs.
- A **Reporting cursor** advances only for client usage deltas acknowledged as accepted or duplicate.
- **External tool usage** does not change **Gateway operating metrics**.
- **Usage aggregate retention** outlives row-level **External usage detail** retention.
- Normal external usage ingestion updates aggregates immediately, and **Usage aggregate rebuild** repairs affected ranges when facts or mappings change.

## Example dialogue

> **Dev:** "Should the dashboard's token chart use quota?"
> **Domain expert:** "No. Token usage is raw token volume; quota is the billing consumption unit."

## Flagged ambiguities

- "Token 消耗" was clarified as **Token usage**, not API key tokens and not **Quota**.
- "并入用户的 token 用量" was clarified as statistical aggregation, not **Quota** billing.
- "Cursor 数据导入" was clarified as an administrator-only operation; self-service user import is out of scope.
- External coding tool usage is not represented as gateway consume logs; it has its own usage-event fact source.
- Duplicate external usage imports/uploads are resolved by **External event identity**, not by timestamp and token counts alone.
- Cursor-imported usage and client-reported usage are intentionally stored separately before shared analytics aggregation.
- Client-collected tool cache snapshots are normalized locally into **Client usage deltas** before upload.
- External usage analytics preserve source dimensions and also feed compatible total token aggregates.
- Reporting credentials are scoped to a user device, and each user is limited to three reporting devices.
- External usage is normally accepted for the most recent 90 days by **Usage occurrence time**.
- Cursor usage statuses such as included, free, errored, or aborted are retained for filtering but do not decide whether positive token usage is counted.
- Unmapped external model names aggregate under a source-qualified fallback name until an administrator configures a mapping.
- Client reporting excludes plaintext local paths, repository URLs, messages, tool-call content, filenames, prompts, responses, and raw tool cache files.
- Cursor imports support batch-level audit, revert, and replay; reverting a batch removes its contribution from analytics without erasing the audit trail.
- Client report batches track accepted, duplicate, and rejected deltas; ordinary users do not self-revert them.
- User-facing external usage views show reporting devices, upload status, and source-aware aggregates, not internal identities or row-level details by default.
- External usage is exposed as its own General menu entry; administrators see import and diagnostics tools inside the same workspace.
- External usage is controlled by one site-wide feature flag in the first implementation.
- The first implementation scope is Cursor import plus Codex client collection; ZCode and Minimax Code are protocol-supported placeholders until local parsers are specified.
- Client-side collection is performed by a **Usage reporting agent**, not by the gateway server process.
- Client upload failures do not advance the **Reporting cursor**; rejected deltas are retained locally as errors and not retried unchanged.
- External usage does not affect user request counts, RPM/TPM, API rate limits, subscriptions, balances, or quota consumption reports.
- Source-aware and compatible total aggregates are retained long term; external usage details are retained for 180 days; Cursor import batch and client report batch summaries are retained long term.
- Cursor batch reverts, model mapping changes, and parser fixes are reconciled through range-based **Usage aggregate rebuild** rather than manual counter edits.
