<!-- leafwiki
version: 1
page:
  id: plan-federated-workspaces-decision
  title: Federated Workspaces Decision
  created_at: "2026-06-17T07:30:00Z"
  updated_at: "2026-06-17T07:30:00Z"
  creator_id: codex
  last_author_id: codex
-->

# Federated Workspaces Decision

## Decision

Implement federated workspaces by promoting the landed project-scoped `wikid-frontd-workspaced` runtime into one install-wide local runtime:

- `wikid`: install-wide control plane, identity owner, grant owner, registry owner, descriptor owner, and supervisor for `frontd` plus many `workspaced` children.
- `frontd`: stable public HTTP ingress, multi-workspace app shell, small workspace list/status API, and transparent workspace-aware reverse proxy.
- `workspaced`: per-workspace authority for page content, tree, search, tags, properties, import, sync, revisions, assets, and MCP tools.

The implementation should keep workspace data/root directories scoped, use durable global workspace/grant authority state under `~/.leafwiki/wikid/wikid.db`, route browser workspace pages under `/w/:workspaceId/...`, and keep `run.sh mcp` stable with descriptor-first direct attach to `workspaced`.

## Key Decisions

| Decision | Rationale |
|---|---|
| Use one install-wide `wikid` | Avoids a permanent hierarchy of global `wikid` supervising project-scoped `wikid` runtimes |
| Keep one `leafwiki` binary with hidden role modes | Preserves current packaging while allowing future binary split |
| Use `~/.leafwiki` as global data root | Matches the settled home workspace model and current default workspace layout |
| Make home workspace data dir `~/.leafwiki` and root `~/.leafwiki/root` | Reuses existing `DefaultWorkspace(dataDir)` behavior |
| Store registry and grants at `~/.leafwiki/wikid/wikid.db` | Keeps durable authority state transactional and separate from runtime descriptors |
| Store global runtime descriptors under `~/.leafwiki/runtime` | Keeps install-wide process facts out of workspace registry |
| Keep workspace-local descriptor path `<data-dir>/.leafwiki/project-daemon.json` | Preserves `run.sh mcp` and existing descriptor-first attach semantics |
| Make federated workspace-local descriptor describe `workspaced` | Enables direct STDIO attach to workspace MCP |
| Add `workspaceId` to descriptor identity | Prevents ambiguity when multiple workspaces share similar paths or descriptors |
| Use stable URL-safe workspace IDs | Allows browser routes and API routes to carry workspace identity safely |
| Generate non-home IDs from name plus short hash, then persist | Gives deterministic first IDs without changing IDs after later path/display edits |
| Lazy-start non-home workspaces | Avoids starting all watchers/listeners at login |
| Keep started non-home workspaces alive for the user session | Avoids idle-stop complexity in v1 |
| Use role-only grants | Painfully minimal and easy to extend later |
| Derive capabilities into actor context | Keeps the daemon boundary extensible without adding grant overrides |
| Keep current role enforcement in workspace handlers for v1 | Avoids a broad handler rewrite while preserving current behavior |
| Keep `/mcp` as public HTTP MCP bootstrap | Clients should not need workspace ID before first auth/session setup |
| Add `/mcp/workspaces/:workspaceId` as optional explicit route | Gives deterministic clients a stable workspace-specific endpoint |
| Bind HTTP MCP sessions to exactly one workspace | Prevents MCP session drift across workspaces |
| Keep steady-state STDIO MCP out of `wikid` | `wikid` ensures lifecycle and actor context; `workspaced` carries MCP tools |
| Add `/api/workspaces` and optional ensure/status APIs to `frontd` | The UI needs shell-level workspace data |
| Keep workspace content APIs proxied | `frontd` should not own workspace semantics |
| Make workspace sync mandatory for federated runtime | Aligns federation with Git-backed workspaces and revisions |
| Do not add filesystem isolation | Local OS-level access remains outside LeafWiki's auth boundary |

## Rejected Alternatives

- Keep one frontend per workspace.
- Implement federation by nesting project-scoped `wikid` runtimes under a global `wikid`.
- Move all workspaces into one global data store.
- Start every registered workspace at login.
- Store capability allow/deny overrides in v1.
- Make `frontd` parse MCP JSON-RPC frames.
- Route STDIO MCP through `wikid` permanently.
- Require HTTP MCP clients to call `/mcp/workspaces/:id` before first connection.
- Add cross-workspace search or a normalized content API in v1.
- Migrate old workspace-scoped auth stores.
- Add signed workspace assertions in v1.
- Add filesystem sandboxing for local agents.

## Concrete V1 Contracts

### Global Layout

```text
~/.leafwiki/
  root/
    # home workspace root
  .leafwiki/
    project-daemon.json
    # home workspace-local workspaced descriptor
  wikid/
    auth/
    oauth/
    wikid.db
  runtime/
    wikid.json
    workspaces/
      <workspace-id>.json
```

`wikid.json` is the global runtime descriptor and carries `frontd` plus home `workspaced` role health; v1 does not persist a separate descriptor for `frontd`. Home startup writes the compatibility descriptor at `<home-data-dir>/.leafwiki/project-daemon.json`, which resolves to `~/.leafwiki/.leafwiki/project-daemon.json` for the default home workspace. Lazily started non-home `workspaced` children write their workspace-local compatibility descriptor to `<data-dir>/.leafwiki/project-daemon.json` and mirror the same descriptor to `~/.leafwiki/runtime/workspaces/<workspace-id>.json` for debugging and install-wide runtime visibility.

### Registry Entry

```json
{
  "id": "home",
  "displayName": "Home",
  "dataDir": "~/.leafwiki",
  "rootDir": "~/.leafwiki/root",
  "markdownLinkRootPrefix": "",
  "createdAt": "2026-06-17T00:00:00Z",
  "updatedAt": "2026-06-17T00:00:00Z"
}
```

### Grant Entry

```json
{
  "subject": "user:admin",
  "workspaceId": "home",
  "role": "admin"
}
```

### Public Routes

```text
GET  /api/workspaces
GET  /api/workspaces/:workspaceId/status
POST /api/workspaces/:workspaceId/ensure
ANY  /api/workspaces/:workspaceId/*
ANY  /mcp
ANY  /mcp/workspaces/:workspaceId
GET  /w/:workspaceId/*
```

### STDIO MCP Target

```text
run.sh mcp
  -> foreground leafwiki STDIO adapter
  -> resolve workspace from args/config/cwd
  -> read <workspace-data-dir>/.leafwiki/project-daemon.json
  -> healthy descriptor attaches directly to workspaced private MCP
  -> missing/stale descriptor asks install-wide wikid to ensure workspace
  -> retry descriptor attach
```

## Acceptance Gates

- `wikid` can start as an install-wide daemon under `~/.leafwiki`.
- `wikid` creates and registers home workspace automatically.
- `wikid` registers a non-home workspace on first trusted `run.sh` contact.
- Registry persists across daemon restarts; runtime descriptors can be replaced independently.
- Grants filter workspace listing and actor-context issuance.
- `frontd` serves one stable frontend with multiple workspaces in the sidebar.
- `frontd` proxies workspace APIs by workspace ID.
- `frontd` keeps public auth/OAuth/config/branding routes global.
- HTTP MCP `/mcp` binds to exactly one workspace.
- HTTP MCP `/mcp/workspaces/:workspaceId` requires grants and rejects session mismatches.
- STDIO MCP attaches directly to `workspaced` once the descriptor is healthy.
- `run.sh mcp` command shape remains stable.
- Workspace sync and Git-backed revisions are enabled for every federated workspace.
- Two registered workspaces can be used through one frontend without switching the whole UI.

## Residual Implementation Details

These are not open discovery questions. They are implementation details for the plan tasks:

- Exact Go struct names.
- Exact JSON version constants.
- Exact hash length for generated workspace IDs.
- Exact response field ordering.
- Exact CSS class names for accordion rows.
- Exact E2E fixture setup helpers.
