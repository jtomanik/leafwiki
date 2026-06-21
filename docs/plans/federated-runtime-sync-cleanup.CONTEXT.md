<!-- leafwiki
version: 1
page:
  id: frsc-context-20260620
  title: Federated Runtime Sync Cleanup - Context
  created_at: "2026-06-20T22:13:23Z"
  updated_at: "2026-06-20T22:13:23Z"
  creator_id: system
  last_author_id: system
tags:
  - plans
  - context
fields:
  type: cleanup
-->

# Federated Runtime Sync Cleanup - Context

## Problem Frame

LeafWiki has completed enough of the federated daemon and Git-backed workspace sync work that the old modes are now architectural drag. The code still supports:

- Selecting a legacy runtime stack.
- Starting an old single-workspace project daemon path.
- Running without workspace sync.
- Running legacy page-snapshot revisions.
- UI/API branches that treat Git-backed history as optional.
- Old MCP boolean flags and sidecar-era wrapper flags.
- E2E helpers for legacy auth stores.

Those paths now obscure the current product contract. The desired contract is simpler:

```text
LeafWiki process
  -> federated runtime roles
      -> wikid owns identity, registry, grants, global runtime
      -> frontd owns public ingress
      -> workspaced owns workspace services
  -> every workspace runtime uses workspace sync
  -> every page/workspace history view uses Git-backed workspace revisions
```

The cleanup is deletion-oriented, but it must avoid removing current load-bearing compatibility surfaces by mistake.

## Why This Is More Than Flag Deletion

Deleting flags first is necessary but insufficient. The old modes are threaded through configuration identity, route registration, MCP tool registration, frontend routing, test harness defaults, and docs. A shallow cleanup would create failures like:

- History routes disappearing because they were only registered when a removed flag was true.
- Workspace sync endpoints returning 404 because route gates still check `EnableWorkspaceSync`.
- The frontend hiding validation errors because config no longer sends a flag it defaults to false.
- The wrapper no longer passing `--enable-workspace-sync`, but the backend not enabling sync by default.
- E2E silently testing the old revision mode because the harness still defaults to `--enable-revision=true`.

The correct posture is to remove mode selection at the source and then make all downstream behavior express the new default contract.

## Options Considered

### Keep Compatibility But Hide It

Rejected.

Keeping old flags parseable or ignored would preserve stale configs and old docs. The user explicitly chose unknown failures for old flags/envs.

### Keep Legacy Page-Snapshot Revisions As A Fallback

Rejected.

Git-backed workspace history is now the product path. Keeping a fallback means every revision route, MCP tool, and UI screen keeps mode branching. Old snapshot data can remain on disk but should be unread.

### Migrate Snapshot Revision Data To Git History

Rejected.

Migration would add risk and complexity that does not help the cleanup goal. Git history is authoritative after this change. Old snapshot data is ignored.

### Remove All Revision Model Code Immediately

Rejected as a blanket rule.

Git-backed workspace revisions reuse some `internal/core/revision` types. The legacy `Service`, filesystem stores, and page-save side effect should go, but shared DTO/model types may need to remain or move to a neutral package.

### Remove Descriptor Runtime Metadata

Deferred.

The descriptor `runtimeStack` field and constant `wikid-frontd` can stay as stable metadata. Removing it now risks descriptor hash churn and stale descriptor behavior unrelated to the goal.

### Remove Disabled Auth, Root `/mcp`, Or `leafwiki-local-mcp`

Out of scope.

These are compatibility or local-workflow decisions, not direct shadows of the legacy runtime and revision modes. They remain for this cleanup.

## Selected Strategy

The implementation should proceed in vertical slices:

1. Remove public/runtime mode selection knobs and make removed envs fail generically.
2. Collapse runtime startup to federated daemon only.
3. Make workspace sync unconditional for workspace runtimes.
4. Remove legacy snapshot revision service wiring and route/tool branches.
5. Collapse API/UI mode branching to Git-backed behavior.
6. Rewrite scripts/E2E harness defaults.
7. Rewrite active docs and add historical warnings.

This order prevents downstream work from preserving deleted inputs.

## Key Design Tensions

### Unknown Env Behavior

Flags and YAML keys naturally fail unknown after registration/config-key deletion. Environment variables do not. To honor the "old envs fail unknown" decision, implement a small removed-env validation pass. The wording should be generic, for example `unknown environment variable: LEAFWIKI_RUNTIME_STACK`, not a tailored migration guide.

### `enableWorkspaceSync` API Field

The frontend currently depends on the field. Removing it at the same time as the backend flag is possible but increases blast radius. The chosen transition is:

- Remove `enableRevision`.
- Keep `enableWorkspaceSync: true` in `/api/config` and MCP config for compatibility if clients still need a truthy capability signal.
- Remove frontend behavior branches that hide history/sync when the field is absent or false.

This lets API cleanup continue later without blocking the default-mode cleanup.

### Control-Plane-Only Runtime

Not every `Wiki` instance should open workspace sync. The global control-plane-only `wikid` role should keep identity/control routes without opening a workspace Git store. "Workspace sync is mandatory" means workspace-owning runtimes, not pure control-plane runtimes.

### Revision Assets

Legacy snapshot revisions tracked asset blobs. Git-backed workspace revisions currently do not track historical asset blobs. The cleanup should keep this limitation explicit:

- Page revision list/get/compare/restore use Git-backed history.
- Revision asset routes/tools return not found or remain unsupported.
- No new asset-history feature is added.

## Architecture Constraints

Keep:

- `internal/projectdaemon` descriptor/control package.
- `<data-dir>/.leafwiki/project-daemon.json`.
- Global runtime descriptors.
- Hidden internal process startup flags.
- `scripts/run.sh mcp`.
- `--mcp=stdio` and `--mcp=http`.
- Disabled-auth local workflows.
- Root `/mcp`.
- `wikid.CleanupLegacyAuthDBs`.

Remove:

- Legacy runtime stack selection.
- Legacy single-workspace owner startup.
- Workspace sync opt-in/opt-out flags and envs.
- Legacy page-snapshot revision service wiring.
- Old MCP boolean flags.
- Ignored wrapper compatibility flags.
- Legacy API-key seeding.

## Historical Documentation Posture

Active documentation should describe only the current contract. Historical plans should remain available because plan pages are source-of-truth artifacts, but when they include old examples or old compatibility promises they need visible warnings.

Current behavior after this cleanup:

- Old examples are historical.
- Removed flags/envs fail unknown.
- Sync and Git-backed history are not optional runtime modes.

## Implementation Risk Areas

- Route registration: do not remove gates without registering replacements.
- MCP tool listing: workspace sync and revision tools should be available by default.
- E2E harness: remove legacy defaults before trusting browser results.
- Wrapper dry-run: old ignored flags must no longer appear accepted.
- Descriptor config matching: removing fields can invalidate descriptors; treat this as acceptable only if tests cover stale descriptor cleanup or config mismatch behavior.
- Shared revision types: avoid deleting types used by Git-backed workspace revisions.

