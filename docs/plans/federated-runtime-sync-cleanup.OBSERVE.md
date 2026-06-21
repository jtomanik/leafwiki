<!-- leafwiki
version: 1
page:
  id: frsc-observe-20260620
  title: Federated Runtime Sync Cleanup - Observe
  created_at: "2026-06-20T22:13:23Z"
  updated_at: "2026-06-20T22:13:23Z"
  creator_id: system
  last_author_id: system
tags:
  - plans
  - observe
fields:
  type: cleanup
-->

# Federated Runtime Sync Cleanup - Observe

## Purpose

This document captures the observations behind the implementation plan for making the federated daemon and Git-backed workspace sync the only supported runtime modes, while removing the legacy functionality they now overshadow.

## Planning Input

The user wants LeafWiki to stop carrying compatibility branches that were only useful while the federated daemon and Git-backed workspace sync were optional. The agreed removals are:

- Remove `RuntimeStackLegacy`, `LEAFWIKI_RUNTIME_STACK` runtime selection, and the legacy branch in daemon startup.
- Remove workspace-sync opt-in and opt-out behavior. Sync is default and mandatory for workspace runtimes.
- Remove legacy page-snapshot revision mode. Old snapshot data is dropped or ignored; no migration to Git history.
- Remove UI/API mode branching between legacy page snapshots and workspace sync.
- Remove old MCP compatibility flags.
- Remove legacy API-key seeding support.
- Remove ignored wrapper compatibility flags.
- Make old flags/envs fail as unknown instead of being accepted, ignored, or translated.
- Keep legacy auth DB cleanup.
- Keep historical plan docs, but mark them as historical when they mention stale behavior.

The assumed plan title is `federated-runtime-sync-cleanup`.

## Workflow Evidence

The plan follows `docs/plans/planning.aibasic.txt`. The workflow calls for observation, orientation, decision, and action artifacts:

- `docs/plans/federated-runtime-sync-cleanup.OBSERVE.md`
- `docs/plans/federated-runtime-sync-cleanup.CONTEXT.md`
- `docs/plans/federated-runtime-sync-cleanup.DECISION.md`
- `docs/plans/federated-runtime-sync-cleanup.PLAN.md`
- `docs/plans/federated-runtime-sync-cleanup.planning.aibasic.json`

Four read-only explorer slices were run:

- Runtime startup, descriptor, and API-key seeding.
- Backend workspace sync and legacy revision removal.
- Frontend, API config, and docs impact.
- Scripts, E2E harness, and wrapper compatibility.

## Technology Stack

- Backend: Go module `github.com/perber/wiki`.
- Runtime roles: `wikid`, `frontd`, and `workspaced`.
- HTTP server/router: Gin under `internal/http` and `internal/wiki`.
- Runtime descriptors/control: `internal/projectdaemon`.
- Durable federated workspace registry/auth stores: `internal/wikid`.
- Workspace sync and Git history: `internal/workspacesync` and `internal/workspacesync/gitrevisions`.
- Legacy snapshot revisions: `internal/core/revision` service and filesystem stores.
- MCP: `github.com/modelcontextprotocol/go-sdk/mcp` under `internal/wiki/mcp`.
- Frontend: Vite, React, TypeScript in `ui/leafwiki-ui`.
- E2E: Playwright under `e2e`.
- Shell wrappers: `scripts/run.sh` and `scripts/test-run.sh`.
- Repo commands should be run through `rtk`.

## Runtime Startup Observations

### Runtime Stack Selector

Current runtime selection still exists even though the default is already federated:

- `internal/projectdaemon/roles.go` defines `RuntimeStackLegacy` and `RuntimeStackWikidFrontd`.
- `cmd/leafwiki/main.go` resolves `LEAFWIKI_RUNTIME_STACK`.
- `resolveRuntimeStackForStartup` still delegates to the selector for non-service starts.
- `reset-admin-password` still resolves the runtime stack before choosing the auth store.
- `RuntimeStack` is part of `runtimeconfig.LeafWikiRuntimeConfig`, `projectdaemon.Config`, and `projectdaemon.Descriptor`.

Observation: the legacy selector can go, but descriptor metadata should stay as the constant `wikid-frontd` in this slice to avoid unnecessary descriptor/hash churn.

### Daemon Startup Branches

Current startup still has two attach/start paths:

- `attachOrStartRuntimeDaemon` chooses federated versus legacy.
- `attachOrStartProjectDaemon` is the old single-workspace owner startup path.
- `runProjectDaemonOwner` still contains the old `wiki.NewWiki` server branch after checking for federated runtime.
- Federated startup uses `attachOrStartFederatedProjectDaemon`, global `wikid`, and per-workspace `workspaced`.

Observation: collapse runtime startup onto the federated path and delete the old single-owner branch, while keeping internal same-binary role startup.

### Auth Storage

Current auth storage is runtime-stack dependent:

- `authStorageDirForRuntime` chooses legacy data-dir auth DBs versus `wikid` auth paths.
- `wikid.CleanupLegacyAuthDBs` deletes known legacy root auth DBs: `users.db`, `sessions.db`, and `api_keys.db`.
- E2E seed helper can still open legacy auth stores directly in the data dir.

Observation: keep cleanup of known legacy DBs for upgraded installs, but make all runtime auth storage use the `wikid` layout.

## Workspace Sync Observations

### Backend Sync Gate

`WikiOptions` still has `EnableWorkspaceSync`. This flag controls:

- Whether `NewWiki` starts the workspace sync service.
- Whether startup calls `SyncNow`.
- Whether the watcher is started.
- Whether tree loading follows the workspace-sync path or direct `LoadTree`.
- Whether workspace sync HTTP and MCP tools are registered.

Observation: workspace sync should be unconditional for workspace runtimes. Control-plane-only runtimes should not be forced to open a workspace Git store.

### Wrapper Opt-Out

`scripts/run.sh` still has:

- `LEAFWIKI_RUN_MCP_ENABLE_WORKSPACE_SYNC`, defaulting to `1`.
- `--enable-workspace-sync`.
- `--disable-workspace-sync`.
- Code that appends `--enable-workspace-sync` to the child command.

Observation: wrapper sync flags should be removed. `run.sh mcp` remains, but it should no longer pass a sync flag because the child always enables sync.

### E2E Harness Gate

`e2e/run.sh` still defaults to legacy page-snapshot revisions unless `E2E_ENABLE_WORKSPACE_SYNC=1` is set. Config-file generation mirrors the same split.

Observation: E2E should default to Git-backed workspace sync everywhere. Existing E2E skip logic around `E2E_ENABLE_WORKSPACE_SYNC` should be removed or inverted into default coverage.

## Legacy Revision Observations

### Snapshot Revision Service

Legacy page-snapshot revisions are still implemented by:

- `internal/core/revision/service.go`.
- `internal/core/revision/fs_store.go`.
- `internal/wiki/pagesave/revision_effect.go`.
- `ensureBaselineRevisions` in `internal/wiki/wiki.go`.
- `NewRestoreRevisionUseCase` and other legacy use cases in `internal/wiki/revisions/use_cases.go`.

Legacy data lives under `.leafwiki/revisions`, `.leafwiki/blobs`, and `.leafwiki/manifests`.

Observation: old snapshot data is intentionally ignored. The implementation should not migrate or delete it automatically.

### Shared Revision DTOs

Git-backed workspace revision APIs currently reuse types from `internal/core/revision`, such as `Revision`, `RevisionSnapshot`, and `RevisionComparison`.

Observation: do not delete the whole package blindly. Either keep shared DTO/model types while removing the legacy `Service` and filesystem store, or move the shared types to a neutral package as part of the cleanup.

### HTTP and MCP Branching

HTTP revision routes and MCP tools still choose between legacy revision service and workspace sync:

- Revision route registration checks `EnableRevision || EnableWorkspaceSync`.
- Route handlers call `usesWorkspaceRevisions`.
- MCP optional gates register revision tools when either flag is true.
- MCP revision tools branch on `opts.EnableWorkspaceSync`.

Observation: revision routes and tools should be registered by default for workspace runtimes and use Git-backed workspace revision callbacks only. Revision asset retrieval should remain unsupported for Git-backed history unless a new asset-history plan exists.

## API and UI Observations

### Config Fields

`/api/config` and MCP `wiki_get_config` currently expose:

- `enableRevision`
- `enableWorkspaceSync`

The frontend config store defaults both to `false`. App routing and toolbar buttons use `enableRevision || enableWorkspaceSync`.

Observation: remove `enableRevision`. Keep `enableWorkspaceSync: true` temporarily as a compatibility/capability signal only if needed by clients, but remove frontend mode branching and make history/sync UI assume Git-backed behavior.

### Frontend History

`PageHistoryContent.tsx` branches on `enableWorkspaceSync` for:

- "Version" versus "Revision" labels.
- Asset tab visibility.
- Asset diff chips.
- Restore copy.
- Loading state.

Observation: collapse to Git-backed history language and behavior. Legacy asset revision UI paths should be removed or made unreachable.

### Tree Workspace Sync UI

`TreeView.tsx` branches on `enableWorkspaceSync` for:

- Sync status polling.
- Validation banner display.
- Snapshot restore action.

Observation: these should be default workspace capabilities. A missing/stale config field must not hide sync errors.

## MCP and Wrapper Compatibility Observations

### Old MCP Flags

The current `leafwiki` CLI still registers old compatibility flags:

- `--enable-mcp`
- `--mcp-stdio`

Current transport selection is already `--mcp=none|http|stdio|http,stdio`.

Observation: remove the old flag registrations so the normal Go flag parser rejects them as unknown.

### Ignored Wrapper Flags

`scripts/run.sh` still accepts and ignores old sidecar-era flags:

- `--mode`
- `--endpoint`
- `--health-url`
- `--mcp-stdio-bin`
- `--request-timeout`
- `--shutdown-timeout`
- `--max-frame-size`
- `--stdio-arg`
- `--server-log`

Observation: remove these from `wrapper_flag_takes_value` and argument parsing so the wrapper reports unknown options.

### Removed Environment Variables

Unknown environment variables do not fail automatically. Current code explicitly reads some old envs:

- `LEAFWIKI_RUNTIME_STACK`
- `LEAFWIKI_RUN_MCP_RUNTIME_STACK`
- `LEAFWIKI_ENABLE_REVISION`
- `LEAFWIKI_ENABLE_WORKSPACE_SYNC`
- `LEAFWIKI_MAX_REVISION_HISTORY`
- `LEAFWIKI_RUN_MCP_ENABLE_WORKSPACE_SYNC`
- `LEAFWIKI_RUN_MCP_SERVER_LOG`

Observation: if the desired behavior is "old envs fail unknown", implement explicit env rejection for removed env names with generic unknown/unsupported wording.

## Docs Observations

Active docs still describe optional sync and legacy revision mode:

- `README.md`
- `docs/README.md`
- `docs/revisions.md`
- `docs/workspace-sync.md`
- `docs/mcp.md`
- `scripts/README.md`
- `config/leafwiki.service.example.yml`

Historical plans still mention old behavior:

- `docs/plans/mcp_transport_unification.PLAN.md`
- `docs/plans/remove.sidecar.PLAN.md`
- `docs/plans/stdio.PLAN.md`
- `docs/plans/native_mcp_stdio_combined.PLAN.md`
- `docs/plans/local_mcp.PLAN.md`
- `docs/plans/workspace-sync.PLAN.md`

Observation: rewrite active docs. Keep historical plans but make visible warnings truthful: old examples are historical, and current behavior fails unknown rather than accepting or ignoring removed flags.

## Test Surface Observations

High-signal test targets:

- `cmd/leafwiki/main_test.go`
- `internal/runtimeconfig/*_test.go`
- `internal/http/router_test.go`
- `internal/wiki/wiki_test.go`
- `internal/wiki/revisions/routes_test.go`
- `internal/wiki/mcp/mcp_integration_test.go`
- `internal/wiki/mcp/tools_revisions_test.go`
- `internal/workspacesync/service_test.go`
- `internal/workspacesync/gitrevisions/store_test.go`
- `e2e/seed_mcp_api_keys_test.go`
- `scripts/test-run.sh`
- `e2e/run.sh`
- Playwright specs that currently require `E2E_ENABLE_WORKSPACE_SYNC`

Important verification commands:

```bash
rtk go test ./cmd/leafwiki ./internal/runtimeconfig ./internal/projectdaemon ./internal/wikid ./internal/wiki ./internal/wiki/revisions ./internal/wiki/mcp ./internal/workspacesync
rtk bash scripts/test-run.sh
rtk npm --prefix ui/leafwiki-ui run lint
rtk npm --prefix ui/leafwiki-ui run build
rtk env E2E_RUN_MODE=local E2E_ENABLE_MCP_LOCAL=1 ./e2e/run.sh tests/mcp-disable-auth.spec.ts
rtk env E2E_RUN_MODE=local E2E_ENABLE_MCP_LOCAL=1 E2E_MCP_CLIENT_TRANSPORT=stdio ./e2e/run.sh tests/mcp-stdio-disable-auth.spec.ts
rtk env E2E_RUN_MODE=local E2E_ENABLE_MCP_API_KEYS_LOCAL=1 E2E_MCP_CLIENT_TRANSPORT=stdio ./e2e/run.sh tests/mcp-stdio-api-keys.spec.ts
rtk env E2E_RUN_MODE=local ./e2e/run.sh tests/workspace-sync.spec.ts
rtk env E2E_RUN_MODE=local ./e2e/run.sh tests/history.spec.ts
```

## Constraints

- Do not remove `scripts/run.sh mcp`.
- Do not remove direct `--mcp=stdio`.
- Do not remove descriptor/control primitives in `internal/projectdaemon`.
- Do not remove `<data-dir>/.leafwiki/project-daemon.json`.
- Do not remove hidden same-binary internal startup flags used by role supervision.
- Do not remove disabled-auth local workflows in this cleanup.
- Do not remove root `/mcp`.
- Do not migrate old page-snapshot revisions.
- Do not delete historical plan docs; update warnings where needed.

