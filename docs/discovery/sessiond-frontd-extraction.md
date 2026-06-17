<!-- leafwiki
version: 1
page:
  id: carjYOaDRN
  title: Wikid And Frontd Extraction
  created_at: "2026-06-16T09:54:43.235192921Z"
  updated_at: "2026-06-16T10:21:51.049565Z"
  creator_id: system
  last_author_id: public-editor
-->

# Wikid And Frontd Extraction

Working notes for a compatibility-preserving runtime extraction before implementing federated workspaces.

This page is deliberately separate from [Federated Workspaces and Session Daemon](/discovery/federated-workspaces-session-daemon.md). The goal here is to reorganize the current one-workspace runtime into clearer daemon roles without adding multi-workspace product behavior yet.

## Core Question

Can LeafWiki introduce `wikid` and `frontd` as a compatibility-preserving runtime split before implementing federated workspaces?

The current answer is yes, if the cut is framed as a refactor/extraction and not as the federation feature itself.

## Why Split This Out

Federated workspaces will eventually require a control plane, a stable human-facing ingress, and per-workspace authorities. Proving all of that while also adding multi-workspace UI, workspace discovery, grants, and routing would make the work hard to review.

This extraction should prove the system divisions first:

- process lifecycle
- daemon descriptors
- private routing
- health and failure behavior
- auth termination
- verified context forwarding
- current CLI compatibility

It should not add new workspace functionality.

## Scope

The extraction slice should support exactly one project workspace.

In scope:

- Keep one executable: `leafwiki`.
- Add explicit runtime roles: `wikid`, `frontd`, and `workspaced`.
- Keep the `run.sh` surface stable for MCP clients and agent hooks.
- Allow deeper direct `leafwiki` CLI changes where `run.sh` can preserve the practical user contract.
- Have the default `leafwiki` command start the new internal stack.
- Put `frontd` on the configured stable host and port.
- Move `workspaced` to an internal loopback or ephemeral port.
- Move auth, user storage, browser sessions, OAuth/API-key storage, and user management out of `workspaced`.
- Put auth/user storage and management services in `wikid`.
- Put public auth/user-management HTTP routes on `frontd`.
- Have `frontd` terminate HTTP auth and forward verified context to `workspaced`.
- Have `frontd` proxy current API and HTTP MCP requests to the one `workspaced`.
- Keep workspace behavior, sync, revisions, page APIs, and MCP tools in `workspaced`.

Out of scope:

- Multiple registered workspaces.
- Multi-workspace accordion UI.
- Workspace grants or selected-workspace access.
- Workspace discovery.
- Cross-workspace search, activity, or navigation.
- Normalized federation APIs.
- Mandatory migration of all existing installs into a multi-workspace model.

## Runtime Shape

Use one binary with explicit internal roles:

```text
leafwiki
  public executable

leafwiki wikid
  top-level daemon and control-plane role

leafwiki frontd
  stable HTTP ingress role

leafwiki workspaced
  one-workspace authority role
```

The role commands may start as internal or hidden. The important part is that the code is organized as if these roles could become separate binaries later.

The stable user-facing agent command should remain today's wrapper shape:

```bash
run.sh mcp --data-dir /path/to/.wiki --root-dir /path/to/docs --markdown-link-root-prefix /docs
```

This matches the common MCP configuration shape used by downstream projects such as NOWATCH:

```json
{
  "command": "/opt/homebrew/bin/run.sh",
  "args": [
    "mcp",
    "--data-dir",
    "/Users/jakubtomanik/github/nowatch-ios-pr/.wiki",
    "--root-dir",
    "/Users/jakubtomanik/github/nowatch-ios-pr/docs",
    "--markdown-link-root-prefix",
    "/docs"
  ]
}
```

Internally the wrapper can map that stable surface onto the new runtime stack:

```text
run.sh mcp
  -> starts leafwiki in the needed internal/runtime mode
      -> attaches to or starts wikid
          -> starts frontd on configured host/port when needed
          -> starts workspaced on private loopback

leafwiki default/server command
  -> starts wikid role
      -> starts frontd on configured host/port
      -> starts workspaced on private loopback/ephemeral port
```

This proves lifecycle, descriptors, routing, health, and process ownership while keeping the practical MCP and agent entrypoint stable. Direct `leafwiki` flags can be simplified more aggressively if `run.sh`, config-file mode, installation docs, and tests absorb the compatibility burden.

## Responsibility Split

`wikid` owns:

- Starting and supervising the stack.
- Starting `frontd`.
- Starting the one project `workspaced`.
- Minimal one-workspace registry.
- Process descriptors.
- Health and lifecycle coordination.
- Auth and user storage for this runtime.
- User, session, API-key, and OAuth management services.
- Browser session and refresh-token state.
- Auth material needed by `frontd`.
- Private control tokens or signing material used between daemons.

Name decision: use `wikid` for this parent role. It is the global/top-level wiki daemon for the local runtime: the process users and wrappers attach to, the owner of child role lifecycle, descriptors, health, and runtime identity services. `controld` was considered, but it reads narrower than the actual role because this daemon owns auth/user services and runtime visibility as well as control-plane coordination.

`frontd` owns:

- The configured stable host and port.
- Serving the current frontend.
- Global branding/favicon routes and frontend HTML branding injection.
- Public login/logout/session HTTP routes.
- Public user-management and MCP API-key HTTP routes.
- Public OAuth/DCR/token HTTP routes.
- HTTP OAuth/API-key termination for browser/API/MCP requests.
- Reverse proxying current workspace API requests to `workspaced`.
- Reverse proxying HTTP MCP to `workspaced`.
- Forwarding verified actor context to `workspaced`.
- User-visible failure states when `workspaced` is starting or unhealthy.

`workspaced` owns:

- One workspace root directory and data directory.
- Page/tree/search/import/link/tag/property behavior.
- Workspace sync.
- Git-backed revisions.
- Workspace API implementation.
- MCP tool implementation.
- STDIO MCP for that workspace.
- Local operation enforcement using verified actor context supplied by `frontd` or `wikid`.
- No direct ownership of users, browser sessions, API keys, OAuth clients, or auth storage.
- No branding APIs, branding asset serving, frontend branding injection, or per-workspace branding state.

## Auth Boundary

Existing web auth should move to `frontd`/`wikid` as part of this extraction.

Auth and user management should move fully out of `workspaced`, not only the public login endpoints. `workspaced` should not open user/session/API-key/OAuth stores to authenticate requests. It should only enforce workspace operations from a verified actor context supplied over the private daemon channel.

For HTTP flows:

```text
browser or HTTP MCP client
  -> frontd authenticates request
  -> frontd resolves actor/session/API key/OAuth token
  -> frontd forwards request to workspaced with verified context
  -> workspaced enforces workspace operation rules
```

User-management flows stop at `frontd`/`wikid`:

```text
browser or API client
  -> frontd authenticates request
  -> frontd calls wikid user/API-key/OAuth services
  -> no workspaced hop
```

For v1 extraction, `frontd` can forward verified context over a private loopback connection protected by a per-workspace control token. The interface should be shaped so short-lived signed workspace assertions can replace this later if federation needs them.

`workspaced` must not blindly trust arbitrary public headers. Any forwarded actor context must be accepted only from `frontd` over the private daemon channel.

## HTTP MCP Boundary

HTTP MCP should be reached through the stable `frontd` URL in this extraction.

`frontd` terminates auth and proxies the stream/request to `workspaced`. `workspaced` still owns the MCP server and tool implementation.

This keeps HTTP MCP and future federated HTTP MCP aligned:

```text
HTTP MCP client
  -> frontd stable URL
  -> auth termination
  -> verified context
  -> workspaced MCP implementation
```

STDIO MCP remains workspace-scoped and can attach to `workspaced` directly or through a thin launcher that asks `wikid` for the current descriptor.

## Compatibility Contract

The first extraction should preserve today's behavior as much as possible.

Expected compatibility:

- `run.sh mcp` keeps its current command shape for MCP clients.
- `run.sh agent-hook` keeps its current command shape for agent presence hooks.
- Existing MCP JSON config entries that invoke `/opt/homebrew/bin/run.sh mcp` keep working.
- The default `leafwiki` command still starts a usable local wiki, but exact direct-flag parity is lower priority than wrapper stability.
- The browser still opens the configured host/port.
- Existing frontend routes continue to work.
- Existing workspace APIs continue to behave the same from the user's point of view.
- Existing HTTP MCP behavior continues to work through the stable URL.
- Existing STDIO MCP behavior remains available for the current workspace.
- Current one-workspace installs do not need to understand federation concepts.

Internal behavior may change:

- The stable URL is served by `frontd`, not `workspaced`.
- `workspaced` listens on a private/internal address.
- Auth is terminated before requests reach `workspaced`.
- `workspaced` receives verified actor context instead of owning the whole browser auth surface.

## What Seems Agreed

- This should be an extraction/refactor discovery, not a federation feature.
- Use one executable, `leafwiki`, with explicit runtime roles.
- Structure the roles so they can become separate binaries later.
- Treat `run.sh` as the stable public agent/MCP surface.
- Allow direct `leafwiki` CLI simplification when the wrapper preserves existing practical usage.
- Have the default command start the stack internally.
- `wikid` is the only supervisor/restart authority for child roles; `frontd` surfaces state but does not spawn or replace `workspaced`.
- `frontd` keeps today's configured stable host and port.
- `workspaced` moves to an internal loopback or ephemeral port.
- Existing web auth moves to `frontd`/`wikid`.
- Auth/user storage and user-management operations move out of `workspaced`.
- `frontd` terminates HTTP MCP auth and forwards verified context.
- `workspaced` keeps page behavior, workspace APIs, sync, revisions, and MCP implementation.
- Branding/favicon configuration is global to `wikid`/`frontd`; no per-workspace branding is allowed in this extraction.
- The slice supports one project workspace only.
- Do not add multi-workspace UI, workspace grants, or federation APIs in this cut.

## Answered Questions

These answers are intentionally scoped to the compatibility-preserving extraction. They should not solve federated workspace registry, grants, multi-workspace UI, or global identity migration yet.

### Role Command Visibility

`wikid`, `frontd`, and `workspaced` should start as hidden/internal roles, not public CLI commands.

The public wrapper command stays:

```bash
run.sh mcp --data-dir <data-dir> --root-dir <root-dir> --markdown-link-root-prefix <prefix>
```

Direct `leafwiki` server startup should remain usable, but the implementation does not need to preserve every direct flag and mode as a first-class compatibility promise if `run.sh` continues to expose the stable agent/MCP contract.

Internally, the implementation can use hidden flags or hidden subcommands such as `--internal-role=wikid`, `--internal-role=frontd`, and `--internal-role=workspaced`. They should be absent from normal user docs, except in maintainer/debug notes.

This follows the existing `--internal-project-daemon` pattern and keeps the public surface compatible while making the code organize around real roles.

### Stable Wrapper Surface

The stable compatibility boundary for this extraction should be `run.sh`, not the full direct `leafwiki` CLI.

Must remain stable:

- `run.sh mcp`
- `run.sh mcp --data-dir <dir>`
- `run.sh mcp --root-dir <dir>`
- `run.sh mcp --markdown-link-root-prefix <prefix>`
- `run.sh mcp --config <file>`
- `run.sh mcp --api-key <key>` and `LEAFWIKI_MCP_API_KEY`
- `run.sh agent-hook <provider>`
- wrapper environment overrides documented in `scripts/README.md`
- stdout hygiene for MCP STDIO
- dry-run behavior and redaction

May be simplified behind the wrapper:

- direct `leafwiki --mcp=stdio` invocation details
- hidden internal role flags
- exact direct flag names passed from wrapper to binary
- compatibility flags that only exist to keep direct CLI invocations working
- direct help text for internal runtime composition

This lets the implementation cut more cleanly through CLI parsing and startup while preserving the command shape actually embedded in MCP client configs.

### Process Model

Use separate same-binary role processes for v1, not only goroutines.

The current project daemon already proves that LeafWiki can spawn the same executable in an internal owner mode, write a trusted descriptor, and attach foreground sessions. This extraction should extend that model:

```text
leafwiki default command
  -> attaches to or starts wikid
      -> starts frontd on the configured public host/port
      -> starts workspaced on private loopback
```

Each role may still use goroutines internally for HTTP serving, health checks, watchers, and MCP transport handling. The boundary between roles should be process-shaped from the start so lifecycle, descriptors, restart behavior, and failure reporting are real.

### Parent Daemon Name

Use `wikid` as the parent daemon role name.

`sessiond` is rejected because it overemphasizes browser/login sessions and undersells the role's process-supervision, descriptor, health, registry, and visibility responsibilities. `controld` is acceptable but narrower than the chosen name. `wikid` matches the Unix-style daemon naming convention while making it clear that this is the primary LeafWiki daemon above `frontd` and `workspaced`.

The likely package split becomes:

- `internal/wikid`
- `internal/frontd`
- `internal/workspaced`
- shared descriptor/control primitives in or near `internal/projectdaemon`

### Descriptor Model

Use one `wikid` runtime descriptor plus child role descriptors. The descriptor schema can evolve from the existing trusted `project-daemon.json` contract:

```json
{
  "schemaVersion": 2,
  "runtime": "wikid-frontd-workspaced",
  "pid": 1234,
  "startedAt": "2026-06-16T09:00:00Z",
  "dataDir": "/abs/data",
  "rootDir": "/abs/root",
  "publicUrl": "http://127.0.0.1:8080",
  "basePath": "",
  "configHash": "...",
  "controlUrl": "http://127.0.0.1:49152",
  "controlToken": "...",
  "roles": {
    "frontd": {
      "pid": 1235,
      "publicUrl": "http://127.0.0.1:8080",
      "controlUrl": "http://127.0.0.1:49153",
      "configHash": "...",
      "state": "ready"
    },
    "workspaced": {
      "pid": 1236,
      "workspaceId": "current",
      "dataDir": "/abs/data",
      "rootDir": "/abs/root",
      "privateUrl": "http://127.0.0.1:49154",
      "controlUrl": "http://127.0.0.1:49155",
      "configHash": "...",
      "state": "ready"
    }
  }
}
```

Minimum required fields:

- `schemaVersion`
- runtime name
- parent `wikid` pid and start time
- canonical data/root dirs
- public URL and base path
- private control URL
- config hash
- random control token
- role pid, URL, config hash, and health state for `frontd` and `workspaced`

Do not add separate pid files unless an OS integration later needs them. The descriptor already carries pids.

### Descriptor, Log, And Token Storage

For this extraction, keep storage project-scoped under the current data dir:

```text
<data-dir>/.leafwiki/project-daemon.json
<data-dir>/.leafwiki/runtime/wikid.json
<data-dir>/.leafwiki/runtime/frontd.json
<data-dir>/.leafwiki/runtime/workspaced-current.json
<data-dir>/.leafwiki/logs/leafwiki.log
```

The existing `project-daemon.json` path should remain as the compatibility attach point for current wrappers, tests, and startup behavior. It can either become the `wikid` descriptor or a small compatibility pointer to the `wikid` descriptor.

All descriptor/token files must be written atomically with mode `0600` and same-user ownership validation. Logs can remain in the existing log directory, with each entry tagged by role. Role-specific log files are optional, but not required for v1.

Private control tokens can live in the `0600` descriptor because this is already the trusted local attach contract. They must not be printed in config mismatch errors, normal logs, or help output.

### V1 Auth And User Storage

Move auth and user storage out of `workspaced` for v1.

The storage should be `wikid`-owned even in the one-workspace extraction. A practical v1 location is project-scoped `wikid` storage under the current data dir:

```text
<data-dir>/.leafwiki/wikid/auth/users.db
<data-dir>/.leafwiki/wikid/auth/sessions.db
<data-dir>/.leafwiki/wikid/auth/api_keys.db
<data-dir>/.leafwiki/wikid/oauth/
```

This is central relative to `workspaced`, but still project-scoped so the extraction does not need to solve global federation registry and grants yet. The later federated design can choose a global `wikid` storage location without changing the rule that `workspaced` does not own identity.

Do not migrate existing workspace-scoped auth data into `wikid`:

- `wikid` creates fresh user, session, OAuth, and API-key databases.
- Existing workspace-scoped auth stores such as `<data-dir>/users.db`, `<data-dir>/sessions.db`, and `<data-dir>/api_keys.db` are not imported.
- `workspaced` ignores legacy auth stores if they remain on disk.
- On launch, `wikid` should detect and delete legacy auth stores at `<data-dir>/users.db`, `<data-dir>/sessions.db`, and `<data-dir>/api_keys.db` as cleanup.
- Legacy auth-store deletion must happen only for these known old auth database paths. It must not delete the new `wikid` stores or unrelated workspace data.
- Admins recreate users and MCP API keys in the new `wikid` store after the extraction.
- Admin bootstrap behavior should otherwise stay the same as the current LeafWiki implementation.
- `frontd` and `wikid` are the only roles allowed to open active auth stores.
- `workspaced` receives only actor-context envelopes and should not validate browser cookies, OAuth tokens, or API keys directly.
- User-management routes, MCP API-key management routes, OAuth registration, OAuth authorize, and OAuth token routes should terminate at `frontd`/`wikid` and should not proxy to `workspaced`.

### Verified Context Format

Forward a versioned actor-context envelope, not arbitrary public request headers.

V1 can send it as a private header over the loopback daemon channel, protected by the per-workspace control token:

```http
X-LeafWiki-Daemon-Token: <private-token>
X-LeafWiki-Actor-Context: <base64url-json>
```

The decoded JSON shape should be explicit and future-compatible:

```json
{
  "version": 1,
  "issuer": "wikid",
  "subject": "user:<id>",
  "username": "admin",
  "email": "",
  "role": "admin",
  "scopes": ["leafwiki:workspace:read", "leafwiki:workspace:write", "leafwiki:mcp"],
  "workspaceId": "current",
  "authMethod": "cookie|oauth|api_key|disabled",
  "sessionId": "<optional-session-id>",
  "issuedAt": "2026-06-16T09:00:00Z",
  "expiresAt": "2026-06-16T09:05:00Z"
}
```

`workspaced` accepts this only when the daemon token is valid and the request comes through the private channel. Public clients must not be able to spoof these headers directly.

The `role` field preserves current admin/editor/viewer behavior. `scopes` give the later federation work a place to add capability envelopes without changing the transport contract.

### Route Split

`frontd` should own the stable public ingress, auth, OAuth, config, global branding/favicon, and static frontend shell. `workspaced` should own workspace semantics and storage-backed workspace routes.

Served by `frontd` and backed by `wikid` or `frontd` services:

- SPA/static assets: `/`, route fallbacks, `/static/*`, frontend shell responses
- public runtime config: `GET /api/config`
- browser auth: `/api/auth/login`, `/api/auth/logout`, `/api/auth/refresh-token`, `/api/auth/me`
- user and API-key management: `/api/users*`, `/api/users/*/mcp-api-keys*`
- OAuth and MCP auth metadata: `/.well-known/*`, `/oauth/*`
- global branding and favicon routes: `/api/branding`, `/api/branding/*`, `/branding/*`, `/favicon.ico`, `/favicon.svg`
- frontend HTML branding injection: site name, favicon href, base path, and custom stylesheet link
- aggregate health/readiness: `/api/health`
- HTTP MCP ingress: `/mcp`, after auth termination

Proxied by `frontd` to `workspaced` with verified context:

- page and tree APIs: `/api/tree`, `/api/pages*`
- page assets: `/assets/*`, `/api/pages/*/assets*`
- workspace search, links, tags, properties: `/api/search*`, `/api/pages/*/links`, `/api/tags*`, `/api/properties*`
- importer APIs: `/api/import/*`
- revisions and workspace sync: `/api/pages/*/revisions*`, `/api/workspace-sync/*`
- presence routes that refer to workspace pages: `/api/presence/*`

This route split maps naturally onto the current registrar structure: auth/OAuth/config and global branding move to front/`wikid` registrars, workspace registrars stay behind a reverse-proxy boundary.

### Branding And Favicon Ownership

Branding and favicon configuration are global to the `wikid`/`frontd` runtime. Do not support per-workspace branding in this extraction.

`frontd` owns the public branding surface: `/api/branding`, branding asset serving, favicon serving, and initial HTML injection of site name and favicon href. `wikid`/`frontd` own the branding service and storage access. `workspaced` should not expose branding APIs, serve branding assets, inject frontend branding, or carry per-workspace branding state.

For v1, the on-disk branding files can remain project-scoped under the current data dir for compatibility, but they are runtime/frontend state rather than workspace page semantics:

```text
<data-dir>/branding.json
<data-dir>/branding/
```

Later federation can revisit whether global daemon branding should become install-wide or profile-specific, but workspace-specific branding is out of scope for this extraction.

### OAuth Public URL And Resource Behavior

Accepted v1 decision: preserve today's single public MCP resource, and make `frontd`/`wikid` the only components that compute or validate it.

Current LeafWiki OAuth behavior publishes the public MCP resource as the public origin plus base path plus `/mcp`, for example:

```text
issuer:  http://leafwiki.local[/base-path]
resource: http://leafwiki.local[/base-path]/mcp
```

Concrete v1 rules:

- `issuer` remains `<public-origin><base-path>`.
- `resource` remains `<public-origin><base-path>/mcp`.
- Protected-resource metadata remains available at `/.well-known/oauth-protected-resource`, `/.well-known/oauth-protected-resource/mcp`, and, when configured, `/.well-known/oauth-protected-resource/<base-path>/mcp`.
- Authorization-server metadata remains available at `/.well-known/oauth-authorization-server` and, when configured, `/.well-known/oauth-authorization-server/<base-path>`.
- `WWW-Authenticate` on public `/mcp` challenges points to the public protected-resource metadata URL, not a private `workspaced` URL.
- Authorization requests may omit `resource`; if they include it, it must equal the public MCP resource exactly and must appear at most once.
- OAuth tokens and client records may keep an internal audience/resource placeholder for future federation, but v1 should not add workspace-specific public resources.
- `workspaced` receives verified actor context only. It should not publish OAuth metadata, validate OAuth `resource`, or derive public OAuth URLs from its private listener.

Rejected alternatives:

- Do not introduce workspace-specific resource URLs now, such as `<frontd-public-origin><base-path>/workspaces/current/mcp`; that would leak federation concepts into the one-workspace extraction.
- Do not accept old/new resource aliases for this extraction; the public URL should not change because `frontd` takes over the same configured host/port.
- Do not use private `workspaced` URLs as OAuth resources; clients cannot use them and they leak private topology.
- Do not stop enforcing the `resource` parameter; the current narrow rule remains the compatibility contract.

Public-origin source:

- For public HTTP requests, `frontd` should derive the public origin from the incoming request the same way LeafWiki does today: scheme/host plus `base-path`, honoring the existing reverse-proxy behavior for `X-Forwarded-Proto`.
- For internal daemon calls and descriptors, `wikid`/`frontd` should carry the effective public URL so child roles never need to infer it from private loopback requests.

### HTTP MCP Proxy Contract

`frontd` should reverse-proxy HTTP MCP without parsing MCP frames.

The proxy must preserve:

- methods `GET`, `POST`, and `DELETE`
- request bodies without buffering entire streams
- `mcp-session-id` and `mcp-protocol-version`
- response streaming and flush behavior
- cancellation through `req.Context()`
- `DELETE /mcp` session termination

`frontd` terminates public auth, converts the authenticated request to the actor-context envelope, strips public bearer/cookie credentials from the upstream request, and forwards only private daemon auth plus verified context.

For v1 there is one target `workspaced`, so session affinity is trivial. The implementation should still keep the route keyed by workspace ID or current workspace alias internally so federation can later route `mcp-session-id` traffic to the correct workspace daemon.

### STDIO MCP Attach Contract

Keep `scripts/run.sh mcp` as the stable thin foreground launcher. Direct `leafwiki --mcp=stdio` can remain available as an implementation/debug path, but it is not the primary user contract for this extraction.

The foreground STDIO process should:

1. Resolve the same `run.sh` inputs as today.
2. Attach to or start `wikid` through the trusted descriptor.
3. Ask `wikid` to ensure the current `workspaced` is running.
4. Bridge stdin/stdout to a private MCP endpoint for the current workspace.

The bridge can route through `wikid` first, even if `wikid` then proxies to `workspaced`. That keeps STDIO discovery aligned with the future multi-workspace attach API and avoids teaching clients about workspaced descriptors.

Per-session API keys remain actor/session identity, not daemon identity. They must not become part of descriptor matching.

### Direct Workspaced UI

`workspaced` should not expose a supported public UI in this extraction.

The user-visible UI should be served by `frontd`. During migration, `workspaced` may keep a private loopback debug UI or the old router behind its private listener, but only as a maintainer/debug fallback protected by daemon control auth. Product behavior and compatibility tests should target the stable `frontd` URL.

This prevents the old per-workspace UI from becoming a second supported ingress.

### Migration And Test Strategy

Use a hidden runtime switch while building, then flip the default when parity is proven.

Suggested sequence:

1. Keep the current project-daemon stack as `legacy`.
2. Add the extracted stack as a hidden/test runtime mode, for example `LEAFWIKI_RUNTIME_STACK=wikid-frontd`.
3. Keep `run.sh` as the stable wrapper and let it translate the old public wrapper options into the new runtime.
4. Run focused Go integration tests against both modes for startup, descriptors, auth, route parity, MCP, STDIO, logging, stale descriptors, and lock behavior.
5. Run wrapper tests for `run.sh mcp`, `run.sh agent-hook`, dry-run output, redaction, config mode, and the NOWATCH-style `--data-dir`/`--root-dir`/`--markdown-link-root-prefix` invocation.
6. Use `run.sh` in at least one representative local E2E path so wrapper behavior is tested with a real client flow, not only shell-level dry-run tests.
7. Run selected Playwright E2E tests against both modes for page CRUD, auth, OAuth MCP, API keys, STDIO MCP, workspace sync, revisions, and root-dir behavior.
8. Flip the default runtime to the extracted stack once wrapper and runtime parity are green.
9. Keep the legacy path briefly as rollback/test scaffolding, then remove it when it stops carrying unique risk.

The implementation plan should include an explicit parity matrix instead of relying only on new role-specific tests.

### Failure Reporting

Accepted v1 decision: use a bounded supervisor model owned only by `wikid`.

`wikid` should own child role state and expose it through descriptor/control health responses. `frontd` should translate those states into stable user-visible responses, but it must not spawn, restart, or replace `workspaced`. This keeps one lifecycle authority for the stack.

Minimum role states:

- `starting`
- `ready`
- `degraded`
- `restarting`
- `crashed`
- `stopped`

Role health should include enough detail for diagnostics and UI/API reporting without creating more states:

- role name
- pid
- URL or listener address where applicable
- current state
- readiness timestamp
- restart count
- last exit status or signal
- last error summary
- next retry time when backing off

Expected behavior:

- CLI startup waits for `frontd` and `workspaced` readiness and fails with a role-specific error if startup times out.
- `GET /api/health` on `frontd` returns aggregate status and per-role details from `wikid`.
- If `workspaced` is starting, restarting, or crash-looping, `frontd` stays up and keeps frontend shell, config, branding, auth, and health routes reachable.
- While `workspaced` is unavailable, proxied workspace routes and public `/mcp` return `503 Service Unavailable` with `Retry-After`; unexpected upstream proxy failures can return `502`.
- If `workspaced` crashes, `wikid` restarts it with bounded backoff. Existing HTTP MCP streams and STDIO bridge sessions do not need transparent reconnect in v1; they may fail and clients can start new sessions once `workspaced` is ready.
- If `frontd` crashes, `wikid` restarts it. During that window the public UI is unavailable, but `run.sh` and direct startup paths can still attach to `wikid` and report that `frontd` is restarting.
- If `wikid` dies, the next wrapper/default startup treats the descriptor like today's project-daemon descriptor: reuse only if trusted and healthy, and replace only when project locks are free.
- Child roles should have a parent lease or heartbeat and self-exit if they lose `wikid` long enough, so orphaned `frontd` or `workspaced` processes do not become supported runtimes.
- Repeated crashes must not create a tight restart loop. After the crash-loop budget is exhausted, the role remains visibly `crashed` until a fresh explicit start/attach or manual restart asks `wikid` to retry.
- Logs include role, pid, workspace ID, state transitions, restart count, and startup error details without leaking control tokens or auth secrets.

This keeps failures visible without exposing private daemon endpoints or creating a second supervisor in `frontd`.

## Remaining Non-Blocking Risks

These should be handled in implementation planning, but they no longer block moving out of discovery:

- Exact descriptor Go structs, schema versioning details, and pointer-vs-inline compatibility shape belong to implementation planning.
- The actor-context envelope needs concrete type definitions, encoding helpers, expiry checks, and spoofing tests in implementation planning.
- The route split should be implemented registrar-by-registrar during planning/execution so compatibility failures are easy to isolate.
- Auth store relocation needs tests for fresh `wikid` stores, launch-time deletion of legacy `users.db`, `sessions.db`, and `api_keys.db`, and unchanged admin bootstrap behavior.
- Wrapper compatibility needs explicit tests because it becomes the public compatibility boundary, including at least one E2E path that exercises `run.sh`.
- OAuth metadata URLs and `resource` handling need focused tests after `frontd` becomes the public URL owner.
- Branding/favicon tests should assert global `frontd` ownership, no per-workspace branding, preserved existing on-disk files, and no `workspaced` branding routes.
- Failure/restart tests should cover `workspaced` crash and restart, `frontd` crash and restart, `wikid` descriptor replacement rules, crash-loop backoff, orphan child self-exit, stable `503`/`Retry-After` behavior, and non-leaking logs.

## Graduation Criteria

This discovery is ready to become a plan when we can state:

- The exact role/process model for v1.
- The stable `run.sh` behavior and any hidden direct `leafwiki` role commands.
- The minimal descriptor and private control-token model.
- The route split between `frontd` and `workspaced`.
- The auth/user storage ownership and reset/no-migration contract.
- The verified-context forwarding contract.
- The HTTP MCP proxying contract.
- The failure/restart behavior and health-state contract.
- The compatibility tests that prove today's one-workspace behavior still works.
- The wrapper tests that prove common MCP configs still work unchanged.
- The migration/testing strategy while old and extracted runtime shapes coexist.

All discovery-level decisions above are now settled for the compatibility-preserving extraction. The remaining items are implementation-planning details: concrete Go types, exact schema fields, registrar migration sequence, focused test matrix, and rollout order.

## Relationship To Federated Workspaces

This extraction is an enabling step, not the federated workspace feature.

If it succeeds, the later federation work can focus on multiple workspaces, workspace registry expansion, grants, workspace-aware UI composition, and cross-workspace routing without also proving the basic runtime split.

Likely sequence:

```text
discovery/sessiond-frontd-extraction
  prove runtime boundaries for one workspace

plans/wikid-frontd-extraction
  implement compatibility-preserving stack extraction

discovery/federated-workspaces-session-daemon
  continue shaping multi-workspace behavior on top of proven boundaries
```
