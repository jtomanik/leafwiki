<!-- leafwiki
version: 1
page:
  id: Jcx5CS-Dg
  title: Federated Workspaces and Session Daemon
  created_at: "2026-06-15T08:07:21.745535Z"
  updated_at: "2026-06-15T08:08:23.634043Z"
  creator_id: public-editor
  last_author_id: public-editor
tags:
  - idea
  - architecture
  - workspace-federation
  - mcp
fields:
  created_from: codex-discussion
  status: working-notes
  topic: federated-workspaces
-->

# Federated Workspaces and Session Daemon

Working notes from the June 2026 architecture discussion. This is not a final spec. It captures what we have learned, what seems agreed, and which decisions still need design review.

## Problem

LeafWiki currently works well as a per-workspace daemon: one workspace has one root directory, one data directory, one daemon, and one MCP surface. That shape is good for LLM agents because agents need workspace-scoped context and tools.

It becomes awkward for humans when multiple workspaces are active at once. Each workspace can have its own frontend and port, which makes navigation and lifecycle management messy. Humans need one stable frontend for all workspaces, while agents should keep talking to scoped MCP servers.

## Current Direction

Separate the system into three roles:

```text
leafwiki-sessiond
  process lifecycle, registry, identity, OAuth, global grants

leafwiki-frontd
  one stable human frontend and workspace gateway/proxy

leafwiki-workspaced
  one daemon per workspace: data, root dir, sync, Git revisions, API, MCP
```

The browser should normally talk only to `frontd`. Workspace daemons should remain scoped and authoritative for their own workspace.

## Home Workspace

A home workspace still exists, but it should not host or own the global frontend.

The home workspace is just a normal workspace whose root lives under the user's home folder. It may be pinned as the default workspace and started automatically with the user session.

This avoids making the home workspace special in the backend model. If the home workspace is unhealthy, the global frontend and workspace registry should still be able to operate.

## Session Daemon

`sessiond` starts with the OS user session. At minimum it should start the home workspace daemon. In the fuller model it also starts `frontd`, tracks registered workspaces, starts child workspace daemons on demand, health-checks them, and records their descriptors.

Likely responsibilities:

- Store the global workspace registry, probably under `~/.leafwiki/`.
- Start and monitor `frontd`.
- Start the home workspace daemon automatically.
- Start child workspace daemons lazily when the UI opens them or an agent attaches.
- Track health, control descriptors, actual listener ports, idle state, and process ownership.
- Own global identity, OAuth clients, API keys, sessions, and workspace grants.

It should not implement wiki tree editing, search indexing, import, revision restore, or page behavior.

## Frontend Daemon

`frontd` owns the human app shell. It serves one stable frontend and acts as a workspace-aware gateway.

Example route shape:

```text
GET /api/workspaces
GET /api/workspaces/:workspaceId/status
GET /api/workspaces/:workspaceId/api/tree
POST /api/workspaces/:workspaceId/api/pages
GET /w/:workspaceId/...
```

For the first implementation, a transparent proxy to existing workspace APIs may be simpler than immediately designing a clean new gateway API. Longer term, `frontd` can expose a normalized multi-workspace API for workspace lists, recent activity, global search, health, and navigation.

## Workspace Daemons

Each `workspaced` instance owns exactly one workspace.

Responsibilities:

- Own one root directory and one data directory.
- Watch and sync the root directory.
- Own Git-backed workspace revisions.
- Own page/tree/import/search/link/tag/property behavior for that workspace.
- Serve the workspace-scoped API.
- Serve the workspace-scoped MCP endpoint.
- Enforce workspace operations based on verified identity, scopes, and grants.

Workspace daemons should not own global users, browser sessions, OAuth clients, or API-key databases.

Keeping an embedded workspace UI as a temporary fallback may be useful during migration and debugging, but the long-term model is one normal human frontend via `frontd`.

## Identity And Authorization

Authentication should move to `sessiond`.

`sessiond` should own stable identities for humans and agents, such as:

```text
human:jakub
agent:codex
agent:cursor
agent:claude
```

It should also own login, OAuth authorization, API-key management, browser sessions, refresh tokens, and client registrations.

Authorization data should also be central, because users and agents should not need to be recreated per workspace. The central grant model can look like:

```text
jakub -> all workspaces -> admin
codex -> leafwiki-docs -> editor + mcp
claude -> client-notes -> viewer + mcp
cursor -> private-journal -> no access
```

Workspace daemons should still enforce authorization locally. The split is:

```text
sessiond:
  who is this?
  what grants/scopes does this subject have for this workspace?

workspaced:
  given this verified actor and grants, is this operation allowed here?
```

That keeps identity stable while avoiding a giant central policy engine that knows every page, import, revision, and workspace-sync rule.

## OAuth Model

The GitHub analogy fits well:

```text
GitHub account/org        -> LeafWiki sessiond identity domain
GitHub repository         -> LeafWiki workspace
GitHub OAuth app/client   -> frontd, Codex, Cursor, Claude, etc.
GitHub app installation   -> selected-workspace grant
GitHub repo permissions   -> workspace roles/capabilities
```

OAuth scopes should be used as capability envelopes, not as the entire ACL database.

Good scope examples:

```text
leafwiki:workspaces:list
leafwiki:workspace:read
leafwiki:workspace:write
leafwiki:workspace:admin
leafwiki:mcp
```

The list of workspaces a user or client can see should come from central grants. For example, `leafwiki:workspaces:list` allows calling the listing endpoint, but the returned workspaces are filtered by grants.

A workspace-scoped token or assertion could look conceptually like:

```text
sub: agent:codex
aud: workspace:leafwiki-docs
scope: leafwiki:mcp leafwiki:workspace:read leafwiki:workspace:write
```

`workspaced` validates the token or assertion, then enforces the resulting permissions locally.

## Mandatory Workspace Sync And Git Revisions

For this federated model, workspace sync and Git-backed revisions should become mandatory.

That gives every workspace the same durable resource model:

- The root directory is the source of truth.
- The workspace daemon reconciles the filesystem into LeafWiki state.
- Git-backed revisions are always available.
- Every write can be attributed to a stable global actor.
- History, restore, validation, and MCP context have one consistent backend.
- The system no longer has to branch between legacy page-snapshot revisions and Git-backed workspace revisions for this mode.

This also aligns LeafWiki workspaces more closely with GitHub repositories: a workspace is a protected Git-backed resource, and the central identity system grants users or clients access to selected workspaces.

Important caveat: OAuth controls LeafWiki API, MCP, and frontend access. It does not by itself sandbox raw filesystem access to the root directory. A local agent with OS-level write access can still edit files directly. That is acceptable for trusted local agents, but hard isolation would require OS permissions, sandboxing, separate users, or another filesystem-level boundary.

## What Seems Agreed

- Keep multiple workspace daemons for workspace-scoped data and MCP.
- Provide one stable human frontend for all workspaces.
- Decouple the global frontend from ordinary workspace daemons.
- Introduce a user-session daemon responsible for lifecycle and registry.
- Move global authentication, user management, OAuth, sessions, and API keys out of workspace daemons.
- Keep workspace daemons as the enforcement point for workspace operations.
- Use OAuth scopes for capability classes and central grants for selected-workspace access.
- Treat workspaces similarly to repositories in the GitHub access model.
- Make workspace sync and Git-backed revisions mandatory for the federated model.

## Open Questions

- Should `sessiond` and `frontd` be separate processes from the start, or one process with clear internal modules first?
- What is the exact workspace registry format and where does it live under `~/.leafwiki/`?
- How are workspaces discovered: explicit add only, auto-discovery under the home folder, or both?
- What is the minimum grant model: role-based only, capability-based only, or role plus capability overrides?
- How should workspace-scoped tokens/assertions be represented and rotated?
- Should browser requests be authorized by `frontd` only, or should every proxied request carry a short-lived signed assertion to `workspaced`?
- How much of the existing workspace UI should remain available directly on each workspace daemon during migration?
- What is the migration path from existing per-workspace `users.db`, `sessions.db`, and API keys to central identity?
- Does mandatory workspace sync apply globally, or only when running under the new federated/session model?
- What filesystem-level guarantees, if any, are needed for untrusted agents?

## Design Bias

Use a narrow practical first version without painting the architecture into a corner.

A reasonable v1 could be:

1. Add a session daemon and registry.
2. Add global identity and workspace grants to the session layer.
3. Keep existing workspace daemons mostly intact, but let them delegate identity and API-key verification.
4. Add a single frontend daemon that lists workspaces and proxies into selected workspace APIs.
5. Keep workspace sync and Git-backed revisions enabled for every registered workspace.
6. Defer a cleaner normalized multi-workspace API until the proxy-based version proves the boundaries.
