<!-- leafwiki
version: 1
page:
  id: plan-federated-workspaces-context
  title: Federated Workspaces Context
  created_at: "2026-06-17T07:30:00Z"
  updated_at: "2026-06-17T07:30:00Z"
  creator_id: codex
  last_author_id: codex
-->

# Federated Workspaces Context

> **Historical note:** This plan predates the federated runtime cleanup. Runtime, MCP compatibility, revision-mode, or workspace-sync flag examples in this artifact are historical and do not describe current startup; current LeafWiki uses `--mcp` and always-on Git-backed workspace sync.


## Problem Frame

LeafWiki now has an extracted one-workspace runtime: `wikid` supervises, `frontd` owns stable public HTTP ingress, and `workspaced` owns workspace behavior. That landed cut removed the biggest architectural blocker, but the system still behaves as one workspace with one root/data pair and one page tree.

The product problem remains the original signal: humans need one stable frontend that can work across multiple workspaces, while agents need workspace-scoped MCP access. Running one frontend per workspace on different ports is operationally messy and makes workspace switching a lifecycle problem instead of an app interaction.

The implementation target is a federated local runtime:

```text
install-wide wikid
  -> one frontd at the stable public URL
  -> one home workspaced, always present
  -> zero or more non-home workspaced children, lazy-started
```

The system should retain per-workspace data/root boundaries and per-workspace MCP semantics. It should add the central registry, grants, lifecycle, routing, and UI composition needed to make many workspaces feel like one local LeafWiki installation.

## Orientation

### Runtime Orientation

The current project-scoped `wikid` is a stepping stone. It proves hidden same-binary role processes, private control, auth relocation, proxying, and child supervision for one workspace. The federated runtime should promote this into one install-wide `wikid` under `~/.leafwiki/`.

The target should not be:

```text
global wikid -> project wikid -> workspaced
```

That would keep the extra layer this discovery was trying to remove. The target should be:

```text
global wikid -> workspaced
global wikid -> frontd
```

This means `wikid` needs a durable workspace registry and a multi-child workspace supervisor keyed by workspace ID.

### Storage Orientation

The simplest global storage root is `~/.leafwiki`. That preserves the existing default workspace convention where a data dir owns a `root` child. The home workspace can therefore be:

```text
workspaceId: home
dataDir: ~/.leafwiki
rootDir: ~/.leafwiki/root
```

Global control-plane state should live in reserved subdirectories under the same root:

```text
~/.leafwiki/wikid/
~/.leafwiki/runtime/
```

This keeps compatibility with existing default workspace layout while making `wikid` and runtime state explicit. The implementation should add `wikid` and `runtime` to reserved workspace state entries so no workspace root can be placed inside those global state directories.

### Registry Orientation

The registry is declarative and durable. It says what workspaces exist and how to start them. It is not the same as a runtime descriptor.

The registry and grants should be SQLite-backed authority state:

```text
~/.leafwiki/wikid/wikid.db
```

SQLite is the v1 persistence contract for workspace existence and access grants. Use transactions and constraints for first-contact registration and grant upserts. Keep process rendezvous descriptors as JSON because those are runtime/debug facts, not durable authority data.

Registry entries should include:

- stable workspace ID
- display name
- root dir
- data dir
- markdown link root prefix
- created/updated timestamps
- source marker, such as `home`, `first-contact`, or `manual`
- disabled flag for later use

Workspace IDs should be URL-safe and stable after registration. The first generated non-home ID can be derived from the root basename plus a short hash of canonical root/data paths. Once persisted, it does not change if display name or paths are later edited.

### Descriptor Orientation

Runtime descriptors are facts about live processes. They should remain replaceable and stale-safe.

The implementation should use:

```text
~/.leafwiki/runtime/wikid.json
~/.leafwiki/runtime/workspaces/<workspace-id>.json
<workspace-data-dir>/.leafwiki/project-daemon.json
```

The global `wikid.json` descriptor is the install-wide rendezvous point and includes `frontd`/`workspaced` role health. Runtime workspace descriptors under `runtime/workspaces/` mirror each live `workspaced` descriptor for install-wide control/debug visibility. Workspace-local descriptors at `<workspace-data-dir>/.leafwiki/project-daemon.json` are the direct `workspaced` rendezvous points used by existing `run.sh mcp` style flows.

The workspace-local descriptor is a `workspaced` descriptor for federation. It includes a workspace ID and direct private MCP attach information, while global runtime role state is carried by `wikid.json`.

### Auth And Grants Orientation

Global identity is already separated by the extraction. Federation adds workspace grants, not per-workspace users.

The v1 grant store should be role-only:

```text
subject -> workspace -> role
```

Storage should be:

```text
~/.leafwiki/wikid/wikid.db
```

The daemon boundary should remain capability-shaped. `wikid` looks up the subject's role for a workspace, derives capabilities, intersects them with public OAuth/API-key scope limits, and emits actor context. `workspaced` validates that context and continues using existing role enforcement first.

This keeps storage minimal while leaving an extension point for capability overrides later.

### MCP Orientation

HTTP MCP and STDIO MCP have different routing needs.

HTTP MCP should go through stable `frontd`:

```text
/mcp
/mcp/workspaces/:workspaceId
```

`/mcp` performs workspace selection before the MCP session starts. `/mcp/workspaces/:workspaceId` is explicit for clients that already know the workspace. Once `mcp-session-id` exists, `frontd` must route that session to the same workspace.

STDIO MCP should stay workspace-scoped and attach directly to `workspaced` after descriptor lookup:

```text
run.sh mcp
  -> foreground adapter
  -> workspace-local descriptor
  -> direct workspaced private MCP
```

`wikid` remains in the lifecycle and auth-context path. If the descriptor is missing or stale, `wikid` ensures the workspace is running. Before opening direct private MCP, the foreground adapter obtains actor context from `wikid`.

### Frontend Orientation

The frontend is currently deeply single-workspace. The plan must treat frontend changes as first-class work, not a thin route tweak.

The target UI is not a full app switcher. It is a multi-workspace shell:

```text
sidebar
  workspace home
    tree
  workspace leafwiki
    tree
  workspace nowatch
    tree

main pane
  route /w/:workspaceId/...
```

The route, store, and API layers need workspace IDs:

- Browser page routes: `/w/:workspaceId/...`
- Editor routes: `/w/:workspaceId/e/...`
- History routes: `/w/:workspaceId/history/...`
- Permalinks: `/w/:workspaceId/p/:id/:slug?`
- Workspace APIs: `/api/workspaces/:workspaceId/...`

Global routes remain global:

- `/login`
- `/oauth/*`
- `/users`
- `/settings/branding`
- `/api/auth/*`
- `/api/users*`
- `/api/config`
- `/api/branding*`

### Mandatory Sync Orientation

Discovery settled that federated/session runtime becomes the only runtime and workspace sync/Git revisions become mandatory globally. The code still supports `--enable-revision` and `--enable-workspace-sync` as mutually exclusive modes.

Implementation should not try to preserve legacy snapshot revision mode for federation. It should make federated workspaces run with workspace sync enabled and should keep legacy flags only as compatibility shims until the wider product removes them.

## Alternatives Considered

### Build Federation On Project-Scoped Wikids

Rejected. It preserves an unnecessary hierarchy and makes STDIO, auth, descriptors, and lifecycle harder to reason about.

### Keep `frontd` As Only A Reverse Proxy With No Native API

Rejected for v1 because the UI needs at least a workspace list/status API. The native API budget should remain small, but `GET /api/workspaces` is necessary.

### Put Every Workspace Into A Global Database

Rejected. It fights the existing `rootDir`/`dataDir` model and weakens workspace-scoped agents. The registry should point at existing workspace roots and data dirs.

### Start Every Registered Workspace At Login

Rejected. It makes login cost scale with workspace count and starts watchers/listeners for workspaces the user may not touch.

### Store Capability Overrides In V1 Grants

Rejected. Role-only grants are easier to explain and test. The actor context can still expose derived capabilities.

### Route STDIO MCP Through `wikid` Permanently

Rejected for the federated target. It is acceptable in the extraction stepping stone, but direct attach to `workspaced` keeps STDIO workspace-scoped and avoids making `wikid` part of the steady-state MCP data path.

### Require HTTP MCP Clients To Know Workspace ID Up Front

Rejected. `/mcp` remains the bootstrap/compatibility endpoint and selects a workspace during auth/session setup.

### Make A Normalized Multi-Workspace Content API Now

Rejected. V1 should proxy workspace APIs and add only shell-level workspace list/status/ensure APIs. Cross-workspace search and activity can wait.

## Selected Strategy

Implement federation in vertical phases:

1. Add global path/layout primitives and workspace registry storage.
2. Add role-only grants and capability derivation.
3. Promote `wikid` into an install-wide runtime with home workspace bootstrap.
4. Add multi-workspace child supervision and workspace ensure APIs.
5. Make workspaced descriptors directly attachable for STDIO MCP.
6. Replace single-upstream `frontd` proxying with workspace resolver routing.
7. Add HTTP MCP workspace selection and session binding.
8. Refactor frontend routes, stores, and API helpers to carry workspace IDs.
9. Build the accordion multi-workspace sidebar.
10. Make workspace sync/Git revisions mandatory in federated runtime.
11. Add E2E coverage with two workspaces through one `frontd`.

Each phase should preserve a working local LeafWiki instance and keep `run.sh mcp` stable.

## Risks

- Large blast radius across runtime, auth, MCP, frontend, and sync.
- Confusing registry entries with runtime descriptors.
- Accidentally retaining project-scoped `wikid` as a permanent layer.
- Making `frontd` semantic instead of keeping it proxy-first.
- Breaking `run.sh mcp` clients.
- Losing live API-key revocation semantics in direct STDIO attach.
- Accepting spoofed actor context from public requests.
- Routing same `mcp-session-id` across two workspaces.
- Breaking root/base-path route helpers while adding `/w/:workspaceId`.
- Mixing global auth routes and workspace-scoped routes in the frontend.
- Starting all workspaces and making login slow.
- Accidentally disabling workspace sync or falling back to legacy revisions in federation.

## Planning Assumptions

- The landed `wikid-frontd` extraction is correct enough to build on.
- `~/.leafwiki` is the global default data root for the install-wide runtime.
- `home` is a reserved workspace ID.
- Non-home workspace IDs are generated once and persisted.
- SQLite transactions and constraints back the registry and grant stores in v1.
- Exact OS login integration can be a small launcher/install follow-up; the implementation must make `leafwiki` capable of starting install-wide `wikid`.
- Local filesystem access is not sandboxed by LeafWiki.
- The UI can be changed in this plan; it is not a backend-only feature.
- Existing frontend lint/build are the frontend unit verification boundary unless a test runner is added separately.

## Implementation Biases

- Write tests for storage and routing boundaries before changing runtime behavior.
- Keep compatibility paths explicit and temporary rather than implicit.
- Favor small types and stores in `internal/wikid` over adding global concerns to `internal/wiki`.
- Keep workspace semantics in existing workspace route packages.
- Keep `frontd` as route resolver and proxy.
- Keep direct STDIO attach simple, but resolve actor context through `wikid` to preserve central auth decisions.
- Keep runtime descriptors human-inspectable JSON, while registry and grant authority state uses SQLite.
- Add broad E2E only after lower-level tests lock down descriptor, grant, and proxy contracts.
