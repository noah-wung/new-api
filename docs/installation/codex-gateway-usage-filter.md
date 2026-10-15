# Excluding gateway-metered Codex usage

The usage reporter must upload only external usage. A request already metered by the reporting new-api gateway must not also become a client-reported event. Model names cannot identify this: the same model can be reached through the gateway or directly.

## Collection policy

The installed reporter always treats its `server` URL as a gateway route. It reads the Codex `config.toml` next to the configured sessions directory and joins each session's `model_provider` with `model_providers.<id>.base_url`. HTTP(S) origins are compared exactly after hostname/default-port normalization; API path suffixes are ignored. Different ports and lookalike hostnames do not match. Other gateway origins must be declared explicitly; no DNS discovery or model-catalog inference is performed.

Add these optional fields to the existing reporter configuration; retain its existing credential, source, paths and acceptance window:

```json
{
  "gateway_urls": ["https://token.einyun.com", "http://172.16.0.37:3000"],
  "excluded_codex_providers": ["company-token-gateway"],
  "codex_config_path": "~/.codex/config.toml"
}
```

`gateway_urls` supplies aliases for the same metering gateway. `excluded_codex_providers` explicitly excludes provider IDs, including old gateway IDs no longer present in Codex configuration. Use actual provider IDs from local configuration, not model names. `codex_config_path` overrides automatic configuration discovery, including custom CODEX_HOME or relocated session storage. The reporter does not read auth files. Only routing fields are decoded from Codex configuration; keys, headers, prompts, responses, project names and provider endpoints are never retained in uploaded events. Routing data stays local.

Known gateway usage is skipped while its cumulative Token baseline and scan position advance locally. Otherwise a later direct request could include the skipped gateway tokens in its delta. When the entire batch is excluded, progress is still persisted without making an upload. For real external events, existing ACK/retry semantics and event identities remain unchanged. Reinstallation preserves the optional gateway policy fields on macOS and Windows.

Missing or unmapped provider metadata is deferred, with a count-only warning. Deferred files are rescanned rather than acknowledged or silently discarded. Restoring an unmapped provider's routing configuration allows pending usage to be reconsidered. A legacy session with no provider metadata cannot be classified just from the current default provider; do not infer its route from its model name.

## Provenance limits

Codex session metadata records provider identity, not a trustworthy historical endpoint for every request. Current configuration alone cannot prove where a reused provider ID routed months ago. Do not reuse provider IDs when switching between a metered gateway and direct access; preserve old gateway IDs in `excluded_codex_providers`. Profiles, command-line overrides, process-only environment overrides, redirects and intermediate proxies may change effective routing without appearing in the scanned configuration. Such deployments need explicit provider exclusions and a verified routing policy; the reporter does not claim automatic request-level reconciliation for them. The built-in `openai` route uses `openai_base_url` or a configured `openai` provider URL, falling back to the official API endpoint.

This fix prevents future uploads; it does not remove client events already accepted by the server. Old external records lack provider provenance, so matching only model, timestamp and token count is not reliable evidence for deletion. Historical cleanup requires a separate audited plan based on recoverable client metadata and a database backup. No historical data is changed by installing the new reporter.

## Rollout and validation

Build and distribute the updated reporter binaries together with the matching installer scripts. Restarting only the new-api server does not upgrade reporters already installed on user devices. Preserve existing device identity, credentials and state when replacing a binary. No server schema or report protocol change is required for this filter.

Validate one gateway request and one direct request using the same model: the gateway request remains visible through gateway analytics and creates no external client event; the direct request creates exactly one external event. Repeating the scheduled run, losing an ACK, resuming a session, and reinstalling must not increase the external total. Check count-only excluded/deferred diagnostics before enabling broad collection. Windows PowerShell runtime behavior should additionally be checked on a Windows device.

For future Claude Code collection, apply the same rule before creating an external event. CLI/VS Code entrypoint and model name do not identify the billing route; the collector must use verified effective routing or explicit exclusions. Do not add OTel events and JSONL backfill for an already gateway-metered request.

Official configuration reference: https://developers.openai.com/codex/config-reference/ (`model_provider`, `model_providers.<id>.base_url`, `openai_base_url`).

## Verified checks

The backport was verified on `ein` after integrating the changes:

- `go test ./... -count=1`
- `go test -race ./pkg/usagereporter ./cmd/usage-reporter -count=1`
- `go vet ./pkg/usagereporter ./cmd/usage-reporter ./service ./controller ./model`
- Reporter builds for darwin/arm64, darwin/amd64, and windows/amd64 with CGO disabled.
- Native server build and Linux/amd64 server build with CGO disabled.
- Default frontend typecheck/production build and classic frontend production build, using frozen dependencies. Classic dependencies were installed separately as in the existing Dockerfile.

Two existing Cursor CSV tests used fixed dates outside the 90-day acceptance window. Their fixtures now use recent dates and explicit test state; CSV production behavior is unchanged. Existing `ein` reporting endpoints, credential issuance, and database code were retained.

These checks use synthetic metering fixtures and local HTTP test servers. They do not modify the production gateway or inspect conversation content. Windows installation has not been exercised in a Windows runtime.
