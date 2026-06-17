<!-- leafwiki
version: 1
page:
  id: Rr_xwdavgv
  title: Wikid Frontd Extraction Context
  created_at: "2026-06-16T10:58:35.729189768Z"
  updated_at: "2026-06-16T10:58:35.729189768Z"
  creator_id: system
  last_author_id: system
-->

# Wikid Frontd Extraction Context

## Problem Frame

LeafWiki currently has a transparent project daemon, private control channel, public HTTP server, auth routes, workspace routes, branding, OAuth, API keys, and MCP implementation composed in one runtime shape. The discovery session concluded that federated workspaces should not be attempted on top of this coupled shape.

The next step is not federation. The next step is a compatibility-preserving extraction that proves the runtime roles needed by federation while still serving exactly one project workspace:

- `wikid`: top-level daemon, runtime supervisor, descriptor authority, auth/user/API-key/OAuth owner.
- `frontd`: stable public HTTP ingress, frontend/static/auth/OAuth/branding/config/health owner, public MCP auth terminator and proxy.
- `workspaced`: private workspace authority for pages, tree, search, sync, revisions, assets, and MCP tools.

The implementation must preserve the user-visible one-workspace behavior and the practical MCP wrapper contract while changing internal ownership.

## Orientation

### Compatibility Boundary

The most important compatibility boundary is `scripts/run.sh`, not every direct `leafwiki` CLI detail. This matters because the common MCP configuration invokes `run.sh mcp` directly. The plan can simplify internal CLI surfaces, role flags, and direct invocation details if `run.sh`, config-file mode, docs, and tests preserve real usage.

This makes wrapper tests first-class implementation work, not a cleanup task.

### Process Boundary

The extraction should use separate same-binary role processes. A goroutine-only split would make code organization nicer, but it would not prove descriptor health, restart behavior, child role failure, parent leases, private routing, or CLI attach semantics. The existing project-daemon implementation already has a process-shaped pattern to extend.

### Auth Boundary

Moving only the login route would leave `workspaced` semantically responsible for identity. The discovery decision is stronger: `workspaced` must not own users, sessions, API keys, OAuth clients/tokens, or public auth verification. It should receive a verified actor context from private daemon channels and enforce workspace-local authorization from that context.

This requires auth storage relocation and route split tests before the implementation can claim the extraction is real.

### Public Ingress Boundary

The stable host/port should be owned by `frontd`, not `workspaced`. Browser routes, public API routes, OAuth metadata, public `/mcp`, global branding, and health must remain on the public URL. Workspace routes can be proxied privately to `workspaced`.

`frontd` should not become a second workspace engine. It authenticates and routes; it does not implement page storage, sync, revisions, or MCP tools.

### OAuth Resource Boundary

The public OAuth issuer/resource stays stable:

```text
issuer:   <public-origin><base-path>
resource: <public-origin><base-path>/mcp
```

This avoids leaking workspace federation concepts into v1 and keeps existing clients pointed at the same public resource. `workspaced` should never derive OAuth metadata from its private listener.

### Failure Boundary

Only `wikid` should supervise child roles. `frontd` should stay up and report degraded workspace state, but it must not spawn or restart `workspaced`. A single supervisor avoids split-brain lifecycle behavior and keeps descriptor replacement rules close to the current project-daemon model.

## Alternatives Considered

### Keep The Current Single Runtime

Rejected because it leaves auth, frontend ingress, workspace behavior, and process lifecycle coupled. Federation would then need to solve product behavior and runtime architecture at the same time.

### Implement Federated Workspaces Directly

Rejected because the discovery explicitly scoped this extraction to one project workspace. Multi-workspace UI, registry expansion, grants, discovery, cross-workspace search, and federation APIs are deferred.

### Use `sessiond` As The Parent Name

Rejected because it overemphasizes browser/login sessions and underrepresents supervision, descriptors, health, auth/user/API-key/OAuth services, and runtime visibility.

### Use `controld` As The Parent Name

Rejected for v1 naming because it reads narrower than the actual role. `wikid` better communicates the primary LeafWiki daemon.

### Preserve Every Direct `leafwiki` CLI Flag As Public API

Rejected as the main compatibility strategy. Direct server startup should remain usable, but `run.sh` is the stable surface that real MCP configs depend on.

### Migrate Old Auth Data

Rejected. The accepted behavior is fresh `wikid` auth stores and admin recreation of users/API keys. Deleting known legacy DB files is part of cleanup and must be bounded to those paths.

### Let `frontd` Parse MCP Frames

Rejected. `frontd` should terminate auth and proxy HTTP MCP while preserving streaming and session headers. `workspaced` remains the MCP implementation owner.

### Add Workspace-Specific OAuth Resources Now

Rejected. The v1 extraction keeps a single public MCP resource. Workspace-specific resources belong to a later federation design.

### Allow Per-Workspace Branding

Rejected. Branding and favicon state are global to `wikid`/`frontd` in this extraction.

## Selected Strategy

Use a staged hidden-runtime extraction:

1. Keep the legacy project-daemon stack as a testable baseline.
2. Add role-aware runtime primitives and a hidden extracted stack.
3. Move auth storage and services to `wikid`.
4. Extract `workspaced` as a private workspace authority without auth/branding/frontend ownership.
5. Extract `frontd` as the stable public ingress and move public auth/config/OAuth/branding/static routes there.
6. Add verified actor context and reverse proxying to `workspaced`.
7. Preserve HTTP MCP through `frontd` and STDIO MCP through `run.sh`.
8. Implement bounded `wikid` supervision and failure reporting.
9. Run parity tests in both modes.
10. Flip the default runtime when wrapper, route, auth, MCP, workspace, and failure tests are green.

## Risks

- Scope creep into federation.
- Accidentally leaving auth store ownership in `workspaced`.
- Header spoofing if actor context is accepted from public traffic.
- Breaking public OAuth metadata or resource URLs during route split.
- Breaking HTTP MCP streaming by buffering or parsing in `frontd`.
- Breaking STDIO MCP because wrapper compatibility is not tested with a real client path.
- Descriptor split-brain if old and new descriptors are not related carefully.
- Child role crash loops if `wikid` supervision lacks bounded backoff.
- User-visible confusion if `frontd` and `workspaced` both expose supported UIs.
- Data-loss risk if legacy auth cleanup deletes anything beyond known old auth DB paths.

## Planning Assumptions

- The existing project-daemon descriptor and private control patterns are the best local pattern to extend.
- The extracted runtime can temporarily coexist with the legacy runtime behind an env switch.
- It is acceptable to add new internal packages before moving every route into its final package.
- Some route implementations may initially remain in existing subpackages but be registered through `frontd` or `workspaced` composition roots.
- The implementation should prefer adapters and registrar lists over a large one-shot package move.
- Public route compatibility is more important than final file placement in the first implementation PR.
- Current validation noise from unrelated missing assets may remain outside this plan's scope if it predates the plan.

## Implementation Biases

- Tests first for every behavior boundary: descriptor trust, actor context, route absence, proxying, storage relocation, wrapper compatibility, and failure states.
- Small units that can run independently and be reviewed separately.
- Preserve existing code patterns where they are sound: descriptor trust, atomic writes, loopback control, token gating, and config-hash mismatch reporting.
- Move ownership before deleting old paths, so tests can prove the old owner no longer opens the state.
- Keep old runtime paths until extracted parity is proven.
- Prefer simple v1 contracts with clear seams over general federation machinery.
