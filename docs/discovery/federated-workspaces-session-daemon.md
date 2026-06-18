<!-- leafwiki
version: 1
page:
  id: Jcx5CS-Dg
  title: Federated Workspaces and Wikid
  created_at: "2026-06-15T08:07:21.745535Z"
  updated_at: "2026-06-15T20:53:56.611022Z"
  creator_id: public-editor
  last_author_id: public-editor
tags:
  - discovery
  - architecture
  - workspace-federation
  - mcp
  - wikid
  - sessiond
  - frontd
  - workspaced
fields:
  baseline_runtime: wikid-frontd-workspaced extraction
  created_from: codex-discussion
  extracted_discoveries: fosite-oauth-migration, sessiond-frontd-extraction, wikid-frontd-extraction-plan
  next_focus: federated-workspace-registry-and-grants
  phase: discovery
  status: active-federation-discovery
  topic: federated-workspaces
-->

# Federated Workspaces and Wikid

Working notes from the June 2026 architecture discussion. This is not a final spec. It captures what we have learned, what seems agreed, and which decisions still need design review.

This page started with the name `sessiond`. The parent daemon role is now called `wikid` because the role is broader than browser/login sessions: it owns runtime supervision, descriptors, health, identity services, and eventually the federated workspace registry.

## Problem

LeafWiki currently works well as a per-workspace daemon: one workspace has one root directory, one data directory, one daemon, and one MCP surface. That shape is good for LLM agents because agents need workspace-scoped context and tools.

It becomes awkward for humans when multiple workspaces are active at once. Each workspace can have its own frontend and port, which makes navigation and lifecycle management messy. Humans need one stable frontend for all workspaces, while agents should keep talking to scoped MCP servers.

## Extracted Discovery Tracks

This page is the parent discovery for the federated workspace model. During this discovery, important enabling tracks were identified and split out so they can be worked independently before the broader federation plan is ready.

- [Fosite OAuth Migration](/discovery/fosite-oauth-migration.md) answered the OAuth-library question and moved into planning/implementation. It replaces the pre-migration `go-oauth2` foundation with Fosite so later `wikid` work has a stronger auth seam.
- [Wikid And Frontd Extraction](/discovery/sessiond-frontd-extraction.md) captures the compatibility-preserving runtime split for one project workspace. It is a refactor/extraction track, not a federation feature, and should prove `wikid`, `frontd`, and `workspaced` boundaries before multi-workspace behavior is added.
- [Wikid Frontd Extraction Implementation Plan](/plans/wikid-frontd-extraction-plan.md) is the implementation track for that extraction. It owns proving the one-workspace runtime split, public URL compatibility, auth relocation, private workspace routing, and wrapper parity.

The remaining work in this parent discovery is the actual federation model: multiple registered workspaces, global workspace registry behavior, workspace grants, workspace-aware UI composition, routing, and the interface contracts that appear when one extracted `workspaced` becomes many.

## Extraction Baseline

The federated design should now assume the extraction baseline rather than re-litigate it:

- One executable, `leafwiki`, can run hidden same-binary roles.
- `wikid` is the parent daemon and sole supervisor.
- `frontd` owns the configured stable public HTTP URL.
- `workspaced` owns one workspace and listens on a private loopback or ephemeral address.
- `wikid` owns auth, users, sessions, API keys, OAuth state, descriptors, private control material, and child lifecycle.
- `frontd` terminates public HTTP auth and forwards verified actor context to `workspaced`.
- `frontd` proxies workspace APIs and HTTP MCP without owning workspace semantics.
- `workspaced` owns pages, tree, search, import, sync, revisions, and MCP tool implementation.
- STDIO MCP remains workspace-scoped and attaches through the wrapper/runtime lookup path.

That baseline changes this discovery from "should LeafWiki split into daemons?" to "how does the extracted runtime scale from one workspace to many?"

## Current Direction

Separate the system into three roles:

```text
leafwiki wikid
  global/top-level daemon: lifecycle, registry, identity, OAuth, global grants

leafwiki frontd
  stable human/HTTP ingress: multi-workspace UI shell and thin proxy

leafwiki workspaced
  workspace authority: data, root dir, sync, Git revisions, API, MCP
```

The responsibility split is now mostly clear:

```text
wikid = control plane and runtime identity
frontd = stable human/HTTP ingress
workspaced = workspace authority
```

The browser should normally talk only to `frontd`. HTTP MCP clients should also have a stable `frontd` URL, with `frontd` routing the selected workspace's MCP traffic to the matching `workspaced`. STDIO MCP remains workspace-scoped and can attach to the target `workspaced` through a thin launcher or lookup path managed by `wikid`.

Workspace daemons should remain scoped and authoritative for their own workspace. `frontd` should compose and route; it should not become a second workspace engine.

## Home Workspace

A home workspace still exists, but it should not host or own the global frontend.

The home workspace is a normal registered `workspaced` with reserved bootstrap behavior.

It has:

```text
workspaceId: home
rootDir: ~/.leafwiki/root
```

It registers like any other workspace and is managed by `wikid` like any other `workspaced`. The only special behavior is automatic creation, registration, startup, and attach during user-session startup. No external add/import flow is required for the home workspace to exist.

This keeps the home workspace normal in the backend model. If the home workspace is unhealthy, the global frontend and workspace registry should still be able to operate.

## Global Wikid

The federated `wikid` starts with the OS user session. At minimum it should start `frontd` and the home workspace daemon. In the fuller model it also tracks registered workspaces, starts child workspace daemons on demand, health-checks them, and records their descriptors.

The extraction plan proves a project-scoped `wikid` first. The federated target is one install-wide user-session `wikid` with storage under the user's home directory, likely `~/.leafwiki/`.

The target state is not a permanent hierarchy where an install-wide `wikid` coordinates many project-scoped `wikid` runtimes. Project-scoped `wikid` is an extraction step. Federation should extend `wikid` into the user-session daemon that owns the global workspace registry, identity, grants, `frontd`, and child `workspaced` lifecycle.

Likely responsibilities:

- Store the global workspace registry, probably under `~/.leafwiki/`.
- Start and monitor `frontd`.
- Start the home workspace daemon automatically.
- Start child workspace daemons lazily when the UI opens them or an agent attaches.
- Track health, control descriptors, actual listener ports, idle state, and process ownership.
- Own global identity, OAuth clients, API keys, sessions, and workspace grants.

It should not implement wiki tree editing, search indexing, import, revision restore, or page behavior.

## Workspace Registry

The workspace registry should be declarative, not a live process descriptor.

The registry answers:

```text
Which workspaces exist?
Where are their root and data directories?
How should they be started?
Which workspace is reserved as home?
```

Runtime descriptors answer:

```text
Which workspaced processes are running now?
What are their private URLs, pids, health states, and control material?
```

That split matters because registry entries should survive restarts, while descriptors are runtime facts that can be replaced when processes exit or restart.

Direction:

- Store global `wikid` state under `~/.leafwiki/`.
- Reuse the existing per-workspace `rootDir` and `dataDir` model instead of moving every workspace into one global database.
- Allow registry entries to point at existing project roots and data dirs.
- Keep grants separate from the registry: the registry says what exists; grants say who can see or use it.
- Keep runtime descriptors separate from the registry: descriptors say what is currently running.
- Reserve `home` as the home workspace ID.
- Use stable URL-safe workspace IDs for other workspaces. Display names and filesystem paths may change without changing the workspace ID.

A conceptual global layout could look like:

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

This is a shape, not a schema commitment. In the implemented v1 runtime, the home workspace writes its compatibility descriptor under the home data directory and non-home workspace children write runtime workspace descriptor mirrors. Exact file names, schema fields, descriptor structs, migration behavior, and compatibility pointers belong in implementation planning.

## Workspace Registration And Lifecycle

Workspace registration should be first-contact based.

`wikid` starts at user login and owns the global registry. A workspace enters that registry when `wikid` sees it for the first time through a trusted local attach/start path, especially `run.sh` from a specific workspace.

Flow:

```text
user login
  -> wikid starts
  -> frontd starts
  -> home workspaced starts

run.sh from a workspace
  -> resolves that workspace's root/data/config
  -> reads the workspace-local descriptor
  -> attaches directly to the selected workspaced when healthy
  -> asks install-wide wikid to register/ensure/start the workspace when the descriptor is missing or stale
  -> retries direct workspaced attach

web UI selects a registered workspace
  -> frontd asks wikid to ensure workspaced is running
  -> wikid starts it if stopped
  -> frontd routes workspace API/MCP traffic to it
```

V1 should use lazy workspace startup rather than keeping every registered workspace daemon alive.

The home workspace is the exception: it is always created, registered, started, and attached automatically during user-session startup. Other registered workspaces can appear in the UI while stopped. Selecting or expanding one starts it on demand.

This keeps `registered` separate from `running`:

```text
registered = known durable workspace entry
running    = live workspaced process with descriptor and health state
```

Do not proactively keep all registered `workspaced` processes alive in v1. Registered workspaces may have filesystem watchers, sync state, private listeners, logs, descriptors, and MCP sessions; starting all of them at login would make the system slower and more resource-heavy as the registry grows.

For v1, once a non-home `workspaced` has been started, it can stay alive for the current user session. Idle-stop policy can be added later if resource usage justifies it.

## Frontend Daemon

`frontd` owns the stable human-facing URL, the multi-workspace app shell, workspace-aware routes, and thin proxy routing to workspace daemons.

The important UI decision is that the whole app does not switch between workspaces. The sidebar becomes multi-workspace:

```text
workspace accordion
  workspace A
    normal page tree/navigation for workspace A
  workspace B
    normal page tree/navigation for workspace B
  workspace C
    normal page tree/navigation for workspace C
```

Expanding a workspace reveals that workspace's normal tree and navigation. Clicking a page opens it in the main pane, but the rest of the UI remains multi-workspace. The selected page route should include the workspace ID.

V1 route shape:

```text
GET /api/workspaces
GET /api/workspaces/:workspaceId/status
GET /api/workspaces/:workspaceId/tree
GET /w/:workspaceId/...
ANY /api/workspaces/:workspaceId/*
ANY /mcp
ANY /mcp/workspaces/:workspaceId
```

For v1, `frontd` should be a thin reverse proxy plus app shell, not a normalized federation API. It only needs enough native behavior to render the workspace list, route workspace-aware frontend URLs, and proxy workspace-specific API/MCP requests to the right `workspaced`.

The native `frontd` API budget should stay painfully small:

```text
GET /api/workspaces
  list registered workspaces, display labels, status, capabilities, and route roots

optional POST /api/workspaces/:workspaceId/ensure
  explicitly ask wikid to start or attach a workspace, if proxy-on-first-request is too implicit
```

Everything workspace-semantic should remain proxied to the selected `workspaced`: page tree, page reads/writes, search, tags, properties, imports, sync, revisions, assets, and MCP tools. The accordion UI needs `frontd` to know which workspaces exist and where to route, but it does not require `frontd` to understand workspace content.

`frontd` owns:

- One stable human-facing URL.
- One multi-workspace frontend shell.
- Workspace accordion/sidebar composition.
- Minimal shell-level workspace list/status/ensure APIs.
- Workspace-aware page routes.
- Thin proxy routing to `workspaced` APIs.
- Thin proxy routing to workspace HTTP MCP.

`frontd` does not own:

- Page, tree, search, import, link, tag, or property semantics.
- Workspace sync.
- Git-backed revisions.
- MCP tool implementation.
- Workspace authorization policy.
- Workspace storage.

Longer term, `frontd` may expose normalized multi-workspace APIs for global search, recent activity, cross-workspace navigation, or batch operations. Those should be deferred until repeated needs appear.

## Workspace Daemons

Each `workspaced` instance owns exactly one workspace.

Responsibilities:

- Own one root directory and one data directory.
- Watch and sync the root directory.
- Own Git-backed workspace revisions.
- Own page/tree/import/search/link/tag/property behavior for that workspace.
- Serve the workspace-scoped API.
- Own the workspace-scoped MCP tool implementation.
- Serve workspace-scoped STDIO MCP.
- Serve workspace-scoped HTTP MCP, either directly for local/debug use or behind the stable `frontd` proxy.
- Enforce workspace operations based on verified identity, scopes, and grants.

Workspace daemons should not own global users, browser sessions, OAuth clients, or API-key databases.

Keeping an embedded workspace UI as a temporary fallback may be useful during migration and debugging, but the long-term model is one normal human frontend via `frontd`.

## Identity And Authorization

Authentication should move to `wikid`/`frontd`.

`wikid` should own stable identities for humans and agents, such as:

```text
human:jakub
agent:codex
agent:cursor
agent:claude
```

It should also own login, OAuth authorization, API-key management, browser sessions, refresh tokens, and client registrations.

Federation does not migrate old workspace-scoped auth stores. `wikid` creates the global auth/session/API-key/OAuth stores fresh under the global data area. `workspaced` should delete or ignore legacy workspace-local user, session, auth, API-key, and OAuth stores so there is one canonical identity domain after the split.

That means humans and agents recreate credentials centrally. This is intentional: merging per-workspace users, sessions, OAuth clients, and API keys would create conflict and trust questions that are not needed for the federated v1.

Authorization data should also be central, because users and agents should not need to be recreated per workspace. The central grant model can look like:

```text
jakub -> all workspaces -> admin
codex -> leafwiki-docs -> editor
claude -> client-notes -> viewer
cursor -> private-journal -> no access
```

Workspace daemons should still enforce authorization locally. The split is:

```text
wikid:
  who is this?
  what role/capabilities does this subject have for this workspace?

workspaced:
  given this verified actor and capabilities, is this operation allowed here?
```

That keeps identity stable while avoiding a giant central policy engine that knows every page, import, revision, and workspace-sync rule.

### Grant Model

Use role assignments only for the minimum central grant model.

The stored grant shape should be:

```text
subject -> workspace -> role
```

Examples:

```text
human:jakub  -> home          -> admin
agent:codex  -> leafwiki-docs -> editor
agent:claude -> client-notes  -> viewer
```

V1 stored grant roles:

```text
viewer  read pages/tree/search/revisions, use read-only MCP tools
editor  viewer + write/import/sync/editing MCP tools
admin   editor + workspace admin/settings/grants
```

No access is represented by the absence of a grant row, not by storing a `none` role.

Do not add per-workspace allow/deny capability overrides in v1. Overrides are flexible, but they make authorization harder to reason about before there is evidence that role assignments are insufficient.

To keep the enforcement boundary extensible, `wikid` should derive explicit capabilities from the role before forwarding actor context to `workspaced`.

Conceptually:

```text
role: editor
capabilities:
  - workspace:read
  - workspace:write
  - mcp:read
  - mcp:write
```

Storage stays role-only. The daemon boundary is capability-shaped. Later, if needed, the grant record can evolve to:

```text
subject -> workspace -> role + allow[] + deny[]
```

OAuth scopes remain the outer capability envelope. A token can say what kind of LeafWiki access it may attempt, such as `leafwiki:mcp` or `leafwiki:workspace:write`; central workspace grants decide which workspaces and effective capabilities the actor actually receives.

### Actor Context And Assertions

Do not introduce short-lived signed workspace assertions in v1.

For the federated v1, keep the extraction actor-context envelope and send it over the private daemon channel from `frontd`/`wikid` to `workspaced`, protected by daemon control auth. The envelope should remain versioned and capability-shaped, but it does not need to become a separately signed token while all involved daemons are local children of `wikid`.

This avoids adding key rotation, issuer/audience validation, replay handling, and assertion debugging before there is a concrete need.

Signed workspace assertions become relevant when `workspaced` needs to validate actor context without relying on a private local daemon channel. Examples:

- remote workspace daemons
- cross-machine federation
- workspace daemons not directly supervised by the same `wikid`
- any path where actor assertions leave the local `wikid`/`frontd` controlled runtime

### Actor Context Forwarding

Use the same actor-context forwarding mechanism for browser/API requests and HTTP MCP requests.

Public authentication can differ at the edge:

```text
browser UI/API request -> cookie/session auth at frontd
API request            -> API key or other HTTP auth at frontd
HTTP MCP request       -> OAuth/API-key auth and MCP metadata at frontd
```

After public auth terminates, the private request to `workspaced` should look the same from an authorization point of view:

```text
frontd or wikid
  -> validates public credentials
  -> resolves subject, workspace, role, and capabilities
  -> sends private daemon-authenticated HTTP request to workspaced
  -> includes normalized actor context
```

`workspaced` should explicitly trust HTTP traffic only when it comes from `wikid` or `frontd` over the private daemon channel and carries valid daemon auth plus the normalized actor context. It should not trust arbitrary public headers, browser cookies, API keys, OAuth bearer tokens, or unauthenticated loopback requests.

This keeps `workspaced` independent of the public authentication method. It receives one normalized view: subject, workspace ID, role-derived capabilities, auth method, expiry, and transport/audit metadata where useful. Browser/API and HTTP MCP can have different public edge behavior, but they should share one private authorization contract.

### HTTP MCP Workspace Selection

Keep `/mcp` as the stable public HTTP MCP entrypoint.

Do not require a client to know a `workspaceId` before its first connection. Instead, `frontd`/`wikid` should resolve the target workspace during auth or session setup, then bind the resulting HTTP MCP session to exactly one workspace for v1.

Workspace selection order:

1. If the token or API key is already workspace-bound, use that workspace.
2. If the actor has exactly one accessible workspace, use that workspace.
3. If an explicit workspace hint is present and authorized, use that workspace.
4. If the request is ambiguous, the OAuth/authorization flow should ask the user which workspace to grant, similar to GitHub asking which repository to authorize.
5. If the request is ambiguous and cannot interactively choose, reject before starting an MCP session with a workspace-required error.

The resulting MCP session must not depend on the human frontend's currently selected workspace. Once the MCP session is established, its workspace is fixed.

Use `/mcp/workspaces/:workspaceId` as an optional explicit route for clients or configs that already know the workspace ID. It is useful for deep links and deterministic agent configuration, but it is not required for bootstrap.

Summary:

```text
/mcp
  stable bootstrap and compatibility endpoint
  resolves to exactly one workspace during auth/session setup

/mcp/workspaces/:workspaceId
  optional explicit workspace endpoint
  must still pass auth and grant checks
```

### HTTP MCP Proxying

`frontd` should proxy HTTP MCP as a transparent streaming reverse proxy with minimal MCP-session routing state.

It must preserve:

- request methods used by HTTP MCP, including session termination requests
- request bodies without buffering entire streams
- response streaming and flush behavior
- request cancellation through the HTTP request context
- `mcp-session-id`
- `mcp-protocol-version`

It should not parse MCP JSON-RPC frames or duplicate MCP server state. `workspaced` owns the MCP implementation.

Workspace selection happens before an MCP session starts. Once an `mcp-session-id` exists, `frontd` should route every request for that session to the same bound workspace. If the request uses `/mcp/workspaces/:workspaceId` and also carries an existing MCP session, the route workspace must match the session's bound workspace.

If the target `workspaced` is starting, restarting, unhealthy, or crash-looping, `frontd` should fail the proxied MCP request with a clear HTTP failure before or during the stream as appropriate. V1 does not need transparent replay or reconnect for active MCP sessions. If `workspaced` crashes, active streams may fail and clients should start a new MCP session after the workspace becomes healthy again.

This keeps `frontd` thin, preserves streaming correctness, and prevents an MCP session from drifting across workspaces.

### STDIO MCP Launcher Shape

The stable MCP client configuration should continue to use `run.sh mcp`.

Typical workspace-scoped MCP client config:

```json
{
  "mcpServers": {
    "leafwiki-nowatch": {
      "command": "/opt/homebrew/bin/run.sh",
      "args": [
        "mcp",
        "--data-dir",
        "/Users/jakubtomanik/github/nowatch-ios-pr/.wiki",
        "--root-dir",
        "/Users/jakubtomanik/github/nowatch-ios-pr/docs",
        "--markdown-link-root-prefix",
        "/docs"
      ],
      "env": {
        "LEAFWIKI_MCP_API_KEY": "..."
      }
    }
  }
}
```

The theoretical direct `leafwiki` invocation behind that wrapper is:

```bash
/opt/homebrew/bin/leafwiki \
  --mcp=stdio \
  --data-dir /Users/jakubtomanik/github/nowatch-ios-pr/.wiki \
  --root-dir /Users/jakubtomanik/github/nowatch-ios-pr/docs \
  --markdown-link-root-prefix /docs
```

That foreground `leafwiki --mcp=stdio` process is the STDIO frontend. In the federated runtime it reads the workspace-local descriptor as its rendezvous point and attaches directly to the selected `workspaced` private MCP endpoint when the descriptor is healthy.

The federated target should keep descriptor-first lookup, but make the healthy descriptor attach path direct to the selected `workspaced` private MCP endpoint. `wikid` participates when lifecycle or registration is needed; it should not remain in the steady-state STDIO MCP transport once the target `workspaced` is running.

The intended control/data split is:

```text
leafwiki --mcp=stdio
  -> resolves root/data/config from cwd, flags, or config file
  -> reads the workspace-local descriptor at <workspace-data-dir>/.leafwiki/project-daemon.json

  if descriptor is present, healthy, and config-compatible:
    -> attach directly to workspaced private MCP

  if descriptor is missing or stale:
    -> ask install-wide wikid to ensure/start/register the target workspaced
    -> wikid starts or attaches the workspaced
    -> workspaced writes or updates its workspace-local descriptor
    -> foreground leafwiki retries and attaches to workspaced private MCP

  if descriptor is healthy but config-incompatible:
    -> fail fast rather than reconciling or restarting automatically in v1

  after attachment:
    -> MCP client <-> foreground STDIO adapter <-> workspaced private MCP
```

So `wikid` is in the lifecycle/control path for STDIO MCP, not the steady-state MCP transport path. `workspaced` owns the MCP server, MCP tools, private MCP endpoint, and workspace enforcement.

The exact global `wikid` descriptor/socket path under `~/.leafwiki`, the exact shape of an ensure-workspace API, and whether that API returns a descriptor directly or simply causes the workspace descriptor to appear are implementation-planning details. For discovery, the important contract is descriptor-first attach, `wikid` fallback for lifecycle, hard fail on healthy config mismatch, and stable `run.sh mcp` compatibility.

## OAuth Model

The GitHub analogy fits well:

```text
GitHub account/org        -> LeafWiki wikid identity domain
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

A workspace-scoped actor context could look conceptually like:

```text
sub: agent:codex
aud: workspace:leafwiki-docs
scope: leafwiki:mcp leafwiki:workspace:read leafwiki:workspace:write
```

`frontd`/`wikid` derive this context from public auth plus central grants, then send it to `workspaced` over the private daemon channel. `workspaced` validates the private daemon auth and actor context, then enforces the resulting permissions locally.

### Related Discovery

The OAuth-library migration question is split into [Fosite OAuth Migration](/discovery/fosite-oauth-migration.md). That page records the historical decision to move the pre-migration `go-oauth2` implementation to ORY Fosite as a clean enabling cut before this broader daemon split.

The compatibility-preserving runtime extraction is split into [Wikid And Frontd Extraction](/discovery/sessiond-frontd-extraction.md). That page focuses on introducing `wikid`, `frontd`, and `workspaced` for one project workspace before adding federated workspace behavior.

## Mandatory Workspace Sync And Git Revisions

The federated/session model should become the only available runtime model. Workspace sync and Git-backed revisions should therefore become mandatory globally, not only when a special federation mode is enabled.

That gives every workspace the same durable resource model:

- The root directory is the source of truth.
- The workspace daemon reconciles the filesystem into LeafWiki state.
- Git-backed revisions are always available.
- Every write can be attributed to a stable global actor.
- History, restore, validation, and MCP context have one consistent backend.
- The system no longer has to branch between legacy page-snapshot revisions and Git-backed workspace revisions for this mode.

This also aligns LeafWiki workspaces more closely with GitHub repositories: a workspace is a protected Git-backed resource, and the central identity system grants users or clients access to selected workspaces.

Important caveat: OAuth controls LeafWiki API, MCP, and frontend access. It does not by itself sandbox raw filesystem access to the root directory. LeafWiki does not currently provide filesystem-level isolation for a local agent that already has OS-level access to a workspace root, and federation should not add a new guarantee here in v1. A local agent with OS-level write access can still edit files directly. Hard isolation would remain an OS permissions, sandboxing, separate-users, or filesystem-boundary concern outside this discovery.

## What Seems Agreed

- Keep multiple workspace daemons for workspace-scoped data and MCP.
- Treat `wikid` as the control plane and runtime identity owner, `frontd` as stable human/HTTP ingress, and `workspaced` as the workspace authority.
- Extend `wikid` into the install-wide user-session daemon with a global workspace registry.
- Do not make the target architecture an install-wide `wikid` supervising many project-scoped `wikid` runtimes.
- Provide one stable human frontend for all workspaces.
- Decouple the global frontend from ordinary workspace daemons.
- Build federation on top of the extracted `wikid`/`frontd`/`workspaced` runtime split rather than solving the split here.
- Introduce a user-session `wikid` responsible for lifecycle and registry.
- Treat the home workspace as a normal registered `workspaced` with reserved ID `home`, root `~/.leafwiki/root`, and automatic creation/start/attach behavior.
- Keep the workspace registry declarative and separate from runtime descriptors.
- Reuse the existing per-workspace `rootDir`/`dataDir` model for registered workspaces.
- Keep grants separate from the registry.
- Use stable URL-safe workspace IDs; exact ID generation belongs to implementation planning.
- Register non-home workspaces on first trusted contact, such as `run.sh` starting or attaching a workspace.
- Start only the home workspace at user login; lazy-start other registered workspaces when `run.sh` attaches or the web UI selects them.
- Keep `registered` separate from `running`.
- Do not proactively keep every registered `workspaced` alive in v1.
- Use role-only central workspace grants for v1.
- Derive explicit capabilities from roles before forwarding actor context to `workspaced`.
- Do not add allow/deny capability overrides until role grants prove insufficient.
- Do not introduce signed workspace assertions in v1; use the versioned actor-context envelope over private daemon channels.
- Reserve signed assertions for remote or cross-runtime federation where `workspaced` cannot rely on the private local channel.
- Use the same private actor-context forwarding mechanism for browser/API and HTTP MCP requests after public auth terminates at `frontd`.
- Make `workspaced` trust HTTP traffic only from `wikid` or `frontd` over the private daemon channel with valid daemon auth and normalized actor context.
- Keep `/mcp` as the stable public HTTP MCP entrypoint; choose the workspace during auth/session setup.
- Bind each HTTP MCP session to exactly one workspace in v1.
- Use `/mcp/workspaces/:workspaceId` only as an optional explicit route for clients that already know the workspace.
- Reject ambiguous non-interactive `/mcp` requests before starting an MCP session.
- Keep `frontd` as a transparent streaming reverse proxy for HTTP MCP.
- Preserve HTTP MCP streaming, cancellation, protocol/session headers, and termination requests.
- Route existing `mcp-session-id` traffic to the workspace bound at session setup.
- Do not have `frontd` parse MCP JSON-RPC frames or duplicate MCP server state.
- Do not transparently replay or reconnect MCP sessions after `workspaced` crashes in v1.
- Move global authentication, user management, OAuth, sessions, and API keys out of workspace daemons.
- Do not migrate old workspace-scoped auth stores into global identity; create global `wikid` stores fresh and remove or ignore legacy `workspaced` auth/session/API-key/OAuth state.
- Make the `frontd` UI genuinely multi-workspace: the sidebar contains workspace accordion sections instead of switching the whole app into one active workspace.
- Keep `frontd` thin for v1: it owns the app shell, workspace-aware routes, and reverse proxying, not workspace semantics.
- Route HTTP MCP through stable `frontd` workspace URLs, but keep the MCP tool implementation in `workspaced`.
- Limit native `frontd` APIs to shell-level workspace listing, status/capability display, route roots, and possibly explicit ensure/start.
- Keep page, tree, search, tags, properties, imports, sync, revisions, assets, and MCP tool semantics in `workspaced` behind the proxy.
- Keep STDIO MCP workspace-scoped and attached directly to the target `workspaced` private MCP endpoint once its descriptor is healthy.
- Keep `run.sh mcp` as the stable MCP client surface.
- Use the workspace-local descriptor as the primary STDIO MCP rendezvous point.
- If the workspace descriptor is missing or stale, have the STDIO foreground process ask global `wikid` to ensure/start/register the target `workspaced`, then retry attachment.
- If the workspace descriptor is healthy but config-incompatible, hard fail in v1 rather than reconciling or restarting automatically.
- Keep `wikid` out of the steady-state STDIO MCP transport and semantics. Its role is lifecycle, registration, descriptor trust, and ensure/start fallback.
- Keep workspace daemons as the enforcement point for workspace operations.
- Use OAuth scopes for capability classes and central grants for selected-workspace access.
- Treat workspaces similarly to repositories in the GitHub access model.
- Make the federated/session model the only available runtime model.
- Make workspace sync and Git-backed revisions mandatory globally.
- Do not add new filesystem-level guarantees for local agents that already have OS-level access to workspace roots.
- Defer a normalized federation API until proxy-based v1 shows which cross-workspace operations are actually needed.

## Open Questions

No unresolved discovery-level questions remain on this page.

The one-workspace runtime split is handled by the extraction plan. The federated discovery now has settled direction for process roles, home workspace behavior, registry versus descriptor boundaries, workspace lifecycle, grants, actor context, HTTP MCP, STDIO MCP, `frontd` API scope, auth-store reset/no-migration, mandatory sync/revisions, and filesystem-access limits.

## Deferred To Planning

These are accepted as implementation-planning details rather than open discovery questions:

- Exact workspace registry schema and file format.
- Exact descriptor schema and compatibility pointer shape.
- Exact workspace ID generation algorithm beyond stable, URL-safe IDs and reserved `home`.
- Exact global data-dir subpaths under `~/.leafwiki/`.
- Exact global `wikid` descriptor/socket location under `~/.leafwiki/`, keeping it as close as practical to the current descriptor/control pattern.
- Exact ensure-workspace API shape, including whether it returns descriptor data directly or causes the workspace-local descriptor to become attachable.
- Migration mechanics from project-scoped extraction state to install-wide federation state.
- Exact idle-stop policy for non-home workspaces, if v1 session-long processes become too expensive.

## Design Bias

Use a narrow practical first version without painting the architecture into a corner.

A reasonable v1 could be:

1. Finish the compatibility-preserving `wikid`/`frontd`/`workspaced` extraction for one workspace.
2. Promote or extend `wikid` into a user-session daemon with a global workspace registry.
3. Add global identity and workspace grants to `wikid`.
4. Register multiple workspaces and route them through the existing `frontd` proxy shape.
5. Add a single frontend with a multi-workspace accordion sidebar and workspace-aware routes.
6. Route workspace API and HTTP MCP traffic through `frontd` as a thin proxy.
7. Keep MCP tool implementation and workspace enforcement inside each `workspaced`.
8. Keep STDIO MCP workspace-scoped.
9. Keep workspace sync and Git-backed revisions enabled for every registered workspace.
10. Defer a cleaner normalized multi-workspace API until the proxy-based version proves the boundaries.
