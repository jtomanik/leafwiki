<!-- leafwiki
version: 1
page:
  id: plan-federated-workspaces-observe
  title: Federated Workspaces Observations
  created_at: "2026-06-17T07:30:00Z"
  updated_at: "2026-06-17T07:30:00Z"
  creator_id: codex
  last_author_id: codex
-->

# Federated Workspaces Observations

## Source

- Workflow: `docs/plans/planning.aibasic.txt`
- Plan title: `federated-workspaces`
- Discovery source: `docs/discovery/federated-workspaces-session-daemon.md`
- Dependency plan: `docs/plans/wikid-frontd-extraction.PLAN.md`
- Runtime baseline: landed `wikid-frontd` extraction with one `frontd` and one private `workspaced`
- Current Codex thread ID: not exposed to the agent in this environment.
- Observation method: local code reading, LeafWiki MCP context readback, and four read-only explorer passes over backend runtime, auth/grants, MCP routing, and frontend UI.

## Conversation Decisions Captured

- Move from discovery into implementation planning for federated workspaces.
- Build on the landed `wikid`/`frontd`/`workspaced` extraction instead of reopening the extraction design.
- The target is one install-wide user-session `wikid`, not an install-wide `wikid` supervising many project-scoped `wikid` runtimes.
- `wikid` owns the global workspace registry, identity, grants, runtime descriptors, `frontd` lifecycle, and child `workspaced` lifecycle.
- `frontd` owns the stable public HTTP URL, multi-workspace frontend shell, workspace list/status API, and workspace-aware proxy routing.
- `workspaced` remains the workspace authority for pages, tree, search, import, tags, properties, assets, sync, revisions, and MCP tools.
- Home workspace has reserved ID `home`, root `~/.leafwiki/root`, and is always created, registered, started, and attached at user-session startup.
- Non-home workspaces are registered on first trusted contact, especially `run.sh mcp` or a local `leafwiki` start from a workspace.
- Non-home workspaces should lazy-start when selected in the web UI or attached by `run.sh`.
- V1 does not proactively keep every registered workspace alive.
- Central grants are role-only: `subject -> workspace -> role`.
- Stored grant roles are `viewer`, `editor`, and `admin`; no access is represented by no grant row.
- `wikid` derives capability-shaped actor context from roles before forwarding private requests.
- `workspaced` enforces workspace operations from private actor context and must not trust public credentials directly.
- Do not add signed workspace assertions in v1.
- Keep `/mcp` as the stable public HTTP MCP entrypoint and bind each HTTP MCP session to exactly one workspace.
- Support optional `/mcp/workspaces/:workspaceId` for clients that already know a workspace ID.
- Keep `run.sh mcp` as the stable STDIO MCP client surface.
- For federated STDIO MCP, use descriptor-first lookup and attach directly to `workspaced` private MCP when the descriptor is healthy.
- If the workspace descriptor is missing or stale, ask install-wide `wikid` to ensure/start/register the target `workspaced`, then retry descriptor attach.
- Keep `wikid` out of steady-state STDIO MCP transport and semantics.
- Make the federated/session model the only available model.
- Workspace sync and Git-backed revisions become mandatory globally.
- Do not add new filesystem-level isolation guarantees for local agents with OS access to workspace roots.

## Current Runtime Observations

- `LEAFWIKI_RUNTIME_STACK` defaults to `wikid-frontd`.
- The landed extraction still writes the compatibility descriptor at `<data-dir>/.leafwiki/project-daemon.json`.
- `cmd/leafwiki/main.go` still owns CLI parsing, daemon attach/start, hidden role dispatch, and runtime composition.
- The current `wikid-frontd` owner starts exactly one `frontd` role process and one `workspaced` role process.
- Superseded pre-federation observation: the one-workspace extraction descriptor belonged to the project-scoped `wikid` owner and proxied STDIO MCP through that owner.
- The implemented federated STDIO path is:

```text
run.sh mcp
  -> leafwiki --mcp=stdio
  -> read <workspace-data-dir>/.leafwiki/project-daemon.json
  -> attach directly to workspaced private /mcp when healthy
  -> ask install-wide wikid to register/ensure/start the workspace when missing, stale, or private-token-rejected
  -> retry direct workspaced private /mcp attach
```

- Superseded pre-implementation observation: `projectdaemon.Config` had data/root/config fields without explicit workspace ID. Implemented v1 carries workspace identity through the runtime config and descriptor path.
- Superseded pre-implementation observation: `projectdaemon.Descriptor` had role health and control URL fields without a dedicated workspace ID or direct private MCP endpoint. Implemented v1 exposes workspace identity plus private workspaced MCP attachment details for descriptor-first STDIO.
- Superseded pre-implementation observation: `frontd.NewWorkspaceProxy` took one upstream URL. Implemented v1 uses workspace-ID resolver routing for public workspace API and MCP proxying.
- Superseded pre-implementation observation: `internal/wikid.AuthStoragePaths` was data-dir scoped. Implemented v1 stores install-wide auth paths under `~/.leafwiki/wikid`.
- `wiki.Workspace` is intentionally small: `ID`, `DataDir`, and `RootDir`. Registry metadata should sit above this type.

## Current Backend Patterns To Reuse

- `internal/projectdaemon` descriptor trust rules:
  - regular files only
  - `0600` descriptor mode
  - same-user owner checks
  - atomic descriptor writes
  - loopback private control URL
  - daemon token header
  - config hash comparison
- Same-binary hidden role processes and readiness files.
- `wikid.Supervisor` role state and bounded restart pattern.
- `WikiOptions.WorkspaceOnly` and `WikiOptions.ControlPlaneOnly` split.
- `frontd` private reverse proxy stripping public credentials before forwarding.
- `workspaced` private daemon-token plus actor-context guard.
- Existing `run.sh` compatibility and dry-run tests.
- Existing workspace sync service and Git-backed revision model.

## Current Auth And Grant Observations

- Actor context already carries:
  - `subject`
  - `username`
  - `email`
  - `role`
  - `scopes`
  - `workspaceId`
  - `authMethod`
  - issue and expiry times
- Actor context validation already rejects missing, expired, wrong-workspace, wrong-version, and wrong-issuer contexts.
- `frontd` already strips `Authorization` and `Cookie` before private workspace proxying.
- Current actor-context creation hard-codes scopes such as `leafwiki:workspace:read`, `leafwiki:workspace:write`, and `leafwiki:mcp`.
- Current route and MCP enforcement remains role-based. V1 can make the daemon boundary capability-shaped without rewriting every handler to consume fine-grained capabilities.
- API keys currently do not model workspace binding.
- OAuth currently advertises `leafwiki:mcp`. Workspace scopes become a public OAuth surface and should be introduced deliberately.
- Grants must remain separate from the registry and descriptors.

## Current Frontend Observations

- Implemented v1 has `/w/:workspaceId` routes for viewer, editor, history, permalink, importer, and workspace-specific API flows.
- Implemented v1 has a workspace accordion sidebar and workspace store, with route changes expanding and loading the selected workspace tree.
- `useTreeStore` is workspace-scoped through `workspaceTrees`, while retaining home-compatible selectors for legacy callers.
- Browser API helpers route workspace calls through `/api/workspaces/:id/*` and keep global control-plane routes global.
- Route helpers parse the workspace prefix and preserve workspace identity for view/edit/history, breadcrumbs, page moves/refactors, imports, assets, links, metadata, search, tags, and quick-switcher flows.
- Remaining frontend risk is accidental fallback to singleton/home helpers in new UI code; E2E coverage now includes direct `/w/:id` route loading, importer, breadcrumbs, metadata, link suggestions/status, presence, and editor async races.
- There is no `ui/leafwiki-ui` test script today; frontend verification is lint, build, and E2E.

## Route And API Observations

- Existing global control-plane routes must remain global:
  - `/login`
  - `/oauth/*`
  - `/.well-known/*`
  - `/api/auth/*`
  - `/api/users*`
  - `/api/config`
  - `/api/branding*`
  - `/api/health`
- Workspace routes need a workspace ID:
  - `/w/:workspaceId/...` for browser page routes
  - `/api/workspaces/:workspaceId/*` for workspace APIs
  - `/mcp/workspaces/:workspaceId` for explicit HTTP MCP
- `/api/workspaces/:workspaceId/*` must not collide with legacy `/api/workspace-sync/*`. The plural `workspaces` prefix is acceptable and distinct.
- Root routes can redirect to `/w/home/` for browser compatibility.
- Root `/p/:id/:slug?` permalinks become ambiguous. V1 should prefer `/w/:workspaceId/p/:id/:slug?` and route legacy root permalinks to home only.

## MCP Observations

- Implemented HTTP MCP public paths are `/mcp` and `/mcp/workspaces/:workspaceId`.
- The HTTP MCP proxy strips public credentials and injects daemon token plus actor context.
- The HTTP MCP proxy does not parse JSON-RPC frames.
- Federation implements HTTP MCP session routing state:
  - selected workspace before session start
  - `mcp-session-id -> workspaceId`
  - route/session mismatch rejection
  - cleanup on termination requests
- Direct STDIO-to-`workspaced` resolves actor context through `wikid` before opening the private MCP stream.
- Descriptor-first STDIO direct attach uses descriptor workspace identity and private `workspaced` MCP details; missing, stale, or private-token-rejected descriptors fall back through `wikid` ensure/register before retry. User/workspace grant denial is handled during actor-context resolution and remains an authorization failure.

## Test Anchors Identified

Focused Go tests:

```bash
rtk go test ./cmd/leafwiki ./internal/projectdaemon ./internal/wikid ./internal/frontd ./internal/workspaced ./internal/wiki ./internal/wiki/mcp
```

Wrapper tests:

```bash
rtk bash scripts/test-run.sh
rtk bash -n scripts/run.sh scripts/test-run.sh e2e/run.sh
```

Broader Go verification:

```bash
rtk go test ./...
```

Frontend verification:

```bash
rtk npm --prefix ui/leafwiki-ui run lint
rtk npm --prefix ui/leafwiki-ui run build
```

E2E MCP verification:

```bash
rtk env E2E_RUN_MODE=local E2E_ENABLE_MCP_LOCAL=1 E2E_MCP_CLIENT_TRANSPORT=stdio ./e2e/run.sh tests/mcp-stdio-disable-auth.spec.ts
rtk env E2E_RUN_MODE=local E2E_ENABLE_MCP_API_KEYS_LOCAL=1 E2E_MCP_CLIENT_TRANSPORT=stdio ./e2e/run.sh tests/mcp-stdio-api-keys.spec.ts
rtk env E2E_RUN_MODE=local E2E_ENABLE_MCP_OAUTH_LOCAL=1 ./e2e/run.sh tests/mcp-oauth.spec.ts
rtk env E2E_RUN_MODE=local E2E_ENABLE_MCP_LOCAL=1 E2E_ENABLE_WORKSPACE_SYNC=1 ./e2e/run.sh tests/mcp-agent-context.spec.ts tests/mcp-safe-edits.spec.ts
```

New federation E2E coverage should use two workspaces and one `frontd` URL.

## Constraints That Shape The Plan

- Do not implement a hierarchy of global `wikid` plus many project-scoped `wikid` daemons.
- Preserve `run.sh mcp`.
- Preserve direct config mismatch hard-fail for healthy descriptors.
- Do not migrate legacy auth data.
- Keep `frontd` thin.
- Keep `workspaced` as the enforcement point.
- Keep `registered` and `running` separate.
- Keep non-home lazy startup.
- Keep home workspace always present.
- Do not add cross-workspace search or normalized multi-workspace content APIs in v1.
- Do not add filesystem sandboxing.
- Use workspace sync and Git-backed revisions for every federated workspace.

## Immediate Planning Implications

- The plan must define global storage paths concretely.
- The plan must add workspace ID to descriptor/runtime identity.
- The plan must split registry entries from runtime descriptors.
- The plan must add install-wide `wikid` descriptor lookup for fallback lifecycle operations.
- The plan must extend supervisor state from a single `workspaced` role to per-workspace child runtimes.
- The plan must replace single-upstream `frontd` proxying with resolver-based workspace routing.
- The plan must refactor frontend state from singleton tree to workspace-scoped tree maps.
- The plan must include direct STDIO-to-`workspaced` attach and actor-context acquisition.
- The plan must include HTTP MCP workspace selection and session binding.
- The plan should be executed in phases because the feature touches process lifecycle, auth, MCP, HTTP routing, sync defaults, and most frontend page APIs.
