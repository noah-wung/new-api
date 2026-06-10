# new-api

new-api is an AI API gateway context for routing provider calls, tracking usage, and billing users.

## Language

**Quota**:
A billing consumption unit charged to users for API usage.
_Avoid_: Token usage, raw tokens

**Token usage**:
Raw statistical token volume consumed by API calls.
_Avoid_: Quota, cost

## Relationships

- **Token usage** can contribute to **Quota** through model pricing and billing rules.
- **Quota** is the user-facing billing consumption value; **Token usage** is the raw usage statistic.

## Example dialogue

> **Dev:** "Should the dashboard's token chart use quota?"
> **Domain expert:** "No. Token usage is raw token volume; quota is the billing consumption unit."

## Flagged ambiguities

- "Token 消耗" was clarified as **Token usage**, not API key tokens and not **Quota**.
