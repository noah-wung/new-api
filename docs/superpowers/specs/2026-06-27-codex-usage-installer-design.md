# Codex External Usage One-Click Installer Design

- Date: 2026-06-27
- Status: Approved for planning
- Scope: Replace the current `Usage Reporter Setup` experience with a one-click Codex installer command flow for macOS and Windows

## 1. Background

The current `External Usage` page exposes Codex reporting through a manual setup flow:

- the user issues a reporting credential
- the page shows build and run commands for `cmd/usage-reporter`
- the user is responsible for local installation and recurring execution

This is too manual for the target workflow. The desired flow is:

- user opens `External Usage`
- user clicks `我要上报 Codex 用量`
- user selects `macOS` or `Windows`
- the page shows one executable command
- the command installs the local Codex collector, performs an initial upload, and registers recurring background reporting

The flow must work both for local development and for server deployments under arbitrary hostnames.

## 2. Goals

1. Remove the standalone `Usage Reporter Setup` card from the `External Usage` page.
2. Replace it with a guided Codex installer entry that produces one executable command per platform.
3. Support macOS and Windows in the first release.
4. Serve installer scripts from the current `new-api` service so the generated command automatically matches the deployment origin.
5. Keep the existing external usage reporting model, aggregate model, and device visibility model intact.
6. Ensure reinstall and upgrade do not create duplicate usage statistics.

## 3. Non-Goals

1. Do not implement `zcode` or `minimax_code` collectors in this scope.
2. Do not redesign the reporting data model for all external sources.
3. Do not add a browser-based native installer, signed package, or GUI wizard.
4. Do not add a user-facing uninstall page in this release.
5. Do not replace the current retained-fact deduplication model on the server.

## 4. User Experience

### 4.1 Page Changes

On `External Usage`:

- remove the `Usage Reporter Setup` card entirely
- keep the existing `Reporting Devices` section and device list
- add a new Codex installer section near device management

The new section contains:

- primary action button: `我要上报 Codex 用量`
- explicit platform switch: `macOS` / `Windows`
- read-only command block
- copy button
- short status/help text

### 4.2 Why Explicit Platform Switch

The page must not rely only on browser user-agent detection. Platform detection is not reliable enough for shell-vs-PowerShell command rendering, especially under remote desktop, embedded browsers, or cross-platform clients. The user explicitly chooses the target platform.

### 4.3 Admin Behavior

Only the current logged-in user may generate an installer command for themselves.

Rules:

- normal users can generate their own Codex installer command
- admins viewing another target user cannot generate a long-lived install flow for that target user
- when an admin is inspecting another user, the page shows a clear restriction message and disables installer command generation

The installer command flow is self-service, not an impersonation tool.

## 5. Recommended Architecture

This design uses a three-layer flow:

1. frontend command generation trigger
2. backend short-lived install token generation
3. backend-served platform installer script that exchanges the install token for durable reporter configuration

This is the recommended shape because it keeps the page simple while avoiding exposure of long-lived reporting credentials in the browser.

## 6. Backend API Design

### 6.1 Installer Command API

Add a self-service command generation endpoint:

- `POST /api/external-usage/self/codex-installer-command`

Request body:

```json
{
  "platform": "macos"
}
```

Supported values:

- `macos`
- `windows`

Response body:

```json
{
  "success": true,
  "data": {
    "platform": "macos",
    "command": "bash -c \"$(curl -fsSL 'https://example.com/api/external-usage/reporter/install.sh?token=...')\"",
    "expires_at": 1782556800
  }
}
```

Responsibilities:

- require logged-in user
- require external usage enabled
- require `codex` source enabled in site settings
- require self-only command generation
- mint a short-lived install token
- derive current service origin from request context
- render one complete executable command string for the requested platform

### 6.2 Installer Script Endpoints

Add two service-hosted dynamic script endpoints:

- `GET /api/external-usage/reporter/install.sh`
- `GET /api/external-usage/reporter/install.ps1`

These endpoints:

- accept the short-lived install token
- validate token scope and expiration
- return rendered script content for the requested platform

The script download URL always lives under the currently deployed service origin. This makes the feature naturally portable across local dev, staging, and production deployments.

### 6.3 Install Session Exchange API

Add a backend exchange endpoint used only by the installer script:

- `POST /api/external-usage/self/codex-installer/exchange`

Request shape:

```json
{
  "install_token": "...",
  "device_name": "...",
  "device_fingerprint": "...",
  "platform": "macos"
}
```

Response shape:

```json
{
  "success": true,
  "data": {
    "server": "https://example.com",
    "source": "codex",
    "credential": "eur_...",
    "state_path": "~/.new-api-usage-reporter/state.json",
    "codex_dir": "~/.codex/sessions",
    "poll_interval_seconds": 300
  }
}
```

Responsibilities:

- validate and consume install token
- enforce one user and one source scope
- create or reuse device registration
- issue or rotate the durable reporting credential for that device
- return durable reporter config to the local installer

## 7. Security Model

### 7.1 Install Token vs Reporting Credential

The command shown in the browser must not contain the durable reporting credential.

Instead:

- the page command contains a short-lived install token
- the installer script exchanges that token for a durable reporting credential
- the install token becomes invalid immediately after successful exchange

### 7.2 Install Token Constraints

Each install token is:

- scoped to the authenticated user
- scoped to source `codex`
- single-use
- short-lived, target 10 to 30 minutes

### 7.3 Durable Reporting Credential Handling

The durable reporting credential remains:

- hashed on the server
- stored locally in reporter config on the user machine
- never shown directly in the page UI for this flow

### 7.4 Admin Restrictions

Admins must not be able to generate a durable install flow for another user through the UI. Inspection remains allowed, installation remains self-service.

## 8. Local Installer Behavior

The service provides two scripts:

- `install.sh` for macOS
- `install.ps1` for Windows

The scripts share the same logical phases.

### 8.1 Phase Order

1. validate runtime prerequisites
2. create local install directory
3. download the correct `usage-reporter` binary
4. exchange install token for durable config
5. write local config and persistent device metadata
6. run one immediate incremental upload
7. register recurring background execution
8. print a human-readable summary

### 8.2 Local Paths

macOS:

- install root: `~/.new-api-usage-reporter/`
- binary: `~/.new-api-usage-reporter/usage-reporter`
- config: `~/.new-api-usage-reporter/config.json`
- state: `~/.new-api-usage-reporter/state.json`

Windows:

- install root: `%USERPROFILE%\.new-api-usage-reporter\`
- binary: `%USERPROFILE%\.new-api-usage-reporter\usage-reporter.exe`
- config: `%USERPROFILE%\.new-api-usage-reporter\config.json`
- state: `%USERPROFILE%\.new-api-usage-reporter\state.json`

### 8.3 Reporter Invocation Model

The background task must call the reporter with a config-driven invocation model rather than a long parameter list embedded entirely in the scheduler definition. This reduces secret sprawl and makes upgrades easier.

## 9. Background Execution Strategy

This release uses recurring scheduled execution instead of a continuously running daemon.

Rationale:

- the collector already works as incremental batch scan plus upload
- scheduled execution is easier to support across both macOS and Windows
- user-level registration avoids system-wide permission requirements

### 9.1 macOS

Register a LaunchAgent:

- location: `~/Library/LaunchAgents/pro.newapi.usage-reporter.codex.plist`
- cadence: every 300 seconds
- scope: current logged-in user only

### 9.2 Windows

Register a Scheduled Task:

- scope: current user
- cadence: every 5 minutes
- launch command: installed reporter binary with local config

No administrator privilege is required by default.

## 10. Relationship to Existing Device Model

The existing external usage device model stays in place.

What changes:

- manual setup is no longer the main installation path
- device registration is initiated by the installer flow instead of only through direct UI credential issuance

What stays:

- `Reporting Devices` list remains visible
- each device still has one durable reporting credential
- devices can still be revoked
- last report timestamp and status remain observable

## 11. Device Identity and Reinstall Semantics

### 11.1 Stable Device Identity

The installer creates and stores stable local device metadata, including:

- device fingerprint
- platform
- install root

On reinstall, the script reuses local device metadata if present. Reinstall does not automatically create a new logical device.

### 11.2 Upgrade and Reinstall Must Be Idempotent

Re-running the installer command must be safe.

Allowed reinstall actions:

- replace existing binary
- refresh config
- re-run one immediate incremental upload
- recreate scheduled background task

Disallowed reinstall effect:

- duplicate statistical counting for already reported events

## 12. Duplicate-Safe Reporting Rules

To prevent duplicate statistics during reinstall and upgrade, the following rules are mandatory.

### 12.1 Preserve Local State by Default

Upgrade and reinstall must preserve the existing reporter state file:

- do not delete `state.json`
- do not reinitialize scan state on ordinary reinstall
- reuse the previous incremental cursor

This ensures the post-install run is incremental rather than full replay.

### 12.2 Stable Event Identity

The collector must continue to compute deterministic event IDs from stable Codex usage fields. Re-scanning the same underlying Codex event must yield the same `event_id`.

### 12.3 Server-Side Deduplication Remains Mandatory

The server remains the final deduplication boundary. Replayed events must resolve as duplicates rather than new counted usage.

### 12.4 No Implicit Reset on Reinstall

Reinstall, upgrade, or repeated execution of the install command must never imply:

- state reset
- full rescan
- device recreation by default

Any future reset flow must be explicit and separate.

## 13. Failure Handling

The installer should behave in staged, mostly atomic steps.

### 13.1 Download Failure

- stop immediately
- do not write durable config
- do not register background task

### 13.2 Token Exchange Failure

- remove temporary download artifacts when practical
- do not leave partial scheduler state
- exit with a clear message

### 13.3 Initial Incremental Upload Failure

- keep installed binary and config
- do not register the recurring background task
- print a clear failure summary
- instruct the user to rerun the generated install command

### 13.4 Scheduler Registration Failure

- keep installed binary and config
- report that installation succeeded but recurring execution registration failed
- provide the exact local re-registration command if possible

## 14. Upgrade and Uninstall Strategy

### 14.1 Upgrade

Upgrades are performed by rerunning the generated install command. The installer:

- stops the old scheduled task
- replaces binary and refreshes config
- preserves local state
- performs one incremental upload
- recreates the scheduled task

### 14.2 Uninstall

This release does not require a page-level uninstall UX, but the installer should write enough local metadata to support a future uninstall script that:

- unregisters the scheduled task
- deletes local binary and config
- optionally revokes the device server-side

## 15. Frontend Changes

### 15.1 Remove

- current `Usage Reporter Setup` card
- current build/run/cron command blocks

### 15.2 Add

- installer section with platform tabs or segmented control
- `我要上报 Codex 用量` primary action
- read-only command output
- copy action
- self-only permission guard

### 15.3 Keep

- reporting device list
- device revoke actions
- existing summary, detail, batch, and admin inspection surfaces

## 16. Backend Changes

Add:

- command generation endpoint
- dynamic installer script endpoints
- install token model/service support
- exchange endpoint for token-to-device-config activation
- platform-specific script rendering support
- binary distribution endpoint or static binary serving path under current service origin

Reuse:

- existing device credential issuance logic where possible
- existing client report upload API and deduplication rules
- existing external usage settings and allowed source gating

## 17. Testing Strategy

### 17.1 Backend

- command generation permission tests
- token issue / consume / expire tests
- installer exchange tests
- self-only restriction tests
- codex source gating tests

### 17.2 Frontend

- platform switch command rendering tests
- self-only visibility and disabled-state tests
- command copy tests
- regression tests proving `Usage Reporter Setup` card is removed

### 17.3 Script-Level

At minimum, render tests for:

- shell script contains correct origin and exchange endpoints
- PowerShell script contains correct origin and exchange endpoints
- reinstall path preserves state file
- scheduled task definitions target the correct binary and config locations

### 17.4 End-to-End

- generate installer command as normal user
- execute install flow on macOS
- execute install flow on Windows
- verify first successful incremental upload
- verify recurring scheduler registration
- rerun install and confirm no duplicate usage counting

## 18. Rollout Notes

This change is intentionally Codex-only in the first release. The platform installer framework may later be reused by other external usage collectors, but that is not part of this spec.

## 19. Open Decisions Resolved In This Spec

The following decisions are fixed by this document:

- platform UX uses explicit switch, not auto-detection only
- scripts are served by the current `new-api` service
- install flow performs immediate incremental upload and background task registration
- displayed command uses short-lived install token, not durable reporting credential
- reinstall preserves state and must not duplicate usage statistics
