<!-- leafwiki
version: 1
page:
  id: Or_xQO-vg
  title: Wikid Frontd Extraction Observations
  created_at: "2026-06-16T10:58:35.728889933Z"
  updated_at: "2026-06-16T10:58:35.728889933Z"
  creator_id: system
  last_author_id: system
-->

# Wikid Frontd Extraction Observations

## Source

- Workflow: `docs/plans/planning.aibasic.txt`
- Discovery source: `docs/discovery/sessiond-frontd-extraction.md`
- Plan title: `wikid-frontd-extraction`
- Current Codex thread ID: not exposed to the agent in this environment.
- Observation method: local code reading, LeafWiki context readback, and four read-only explorer passes over runtime, routing, wrapper, and planning conventions.

## Conversation Decisions Captured

- This is a compatibility-preserving extraction, not the federated workspaces feature.
- Use one executable, `leafwiki`, with hidden/internal role processes named `wikid`, `frontd`, and `workspaced`.
- Use package boundaries `internal/wikid`, `internal/frontd`, `internal/workspaced`, and shared runtime primitives in or near `internal/projectdaemon`.
- Use `wikid` for the parent daemon name. Do not use `sessiond` in the implementation plan.
- Drop "home workspace" wording. The slice supports one project workspace only.
- Treat `scripts/run.sh` as the practical public compatibility boundary for MCP and agent-hook usage.
- Direct `leafwiki` CLI surfaces can be simplified when `run.sh`, config-file mode, docs, and tests preserve real user behavior.
- Move auth, user storage, session storage, API-key storage, OAuth storage, and user management out of `workspaced`.
- `workspaced` must not open or own active auth stores. It receives verified actor context over private daemon channels.
- Do not migrate old auth data. `wikid` creates fresh stores. Admins recreate users and API keys.
- On launch, `wikid` detects and deletes only known legacy auth DBs at `<data-dir>/users.db`, `<data-dir>/sessions.db`, and `<data-dir>/api_keys.db`.
- Admin bootstrap behavior should stay the same as current LeafWiki behavior after stores move to `wikid`.
- Branding and favicon ownership are global to `wikid`/`frontd`. Do not allow per-workspace branding in this extraction.
- OAuth public resource remains the single public MCP resource `<public-origin><base-path>/mcp`.
- `frontd`/`wikid` compute and validate public OAuth URLs and resource values. `workspaced` must not publish metadata or validate public OAuth resources.
- `frontd` reverse-proxies HTTP MCP without parsing MCP frames and preserves stream semantics, cancellation, `mcp-session-id`, `mcp-protocol-version`, and `DELETE /mcp`.
- `wikid` is the only supervisor. `frontd` reports degraded state but does not spawn or replace `workspaced`.
- Failure handling uses role states, bounded backoff, `503 Retry-After` for unavailable workspace routes, descriptor trust rules, child parent leases, and non-leaking logs.
- Remaining questions are implementation-planning details: exact Go types, schema fields, registrar migration sequence, focused tests, and rollout order.

## Current Runtime Patterns

- `cmd/leafwiki/main.go` is currently both CLI entrypoint and runtime composition point.
- The existing project daemon uses an internal owner mode through `--internal-project-daemon`.
- `attachOrStartProjectDaemon` resolves runtime config, reads a trusted descriptor, checks health, or spawns the same executable in internal mode.
- The descriptor attach contract is `<data-dir>/.leafwiki/project-daemon.json`.
- `internal/projectdaemon/descriptor.go` writes descriptors atomically with mode `0600`, rejects non-regular files, and checks same-user ownership before trusting a descriptor.
- Descriptor health currently requires:
  - matching schema version
  - matching process ID
  - matching canonical data/root directories
  - matching config hash
  - trusted loopback control URL
  - project locks held by the owner
- `internal/projectdaemon/control.go` gates private control requests with `X-LeafWiki-Daemon-Token`.
- Private control currently handles health, sessions, session heartbeat/release, STDIO auth verification, agent presence, and private MCP forwarding.
- Foreground sessions and agent presence keep the owner alive. Idle timeout defaults to `10m`; `0` means immediate shutdown after activity drops to zero.
- STDIO MCP attaches through private loopback control even when the public web owner is on a non-loopback host.
- Per-session API keys are actor identity material and must not become part of daemon identity or descriptor matching.

## Current Ownership Coupling

- `internal/wiki/wiki.go` is the main coupling point. It constructs workspace services, auth/session/API-key stores, OAuth, branding, MCP, presence, sync, and all route registrars.
- `cmd/leafwiki/main.go` constructs `wiki.NewWiki`, passes `w.Registrars()` and `w.FrontendConfig()` into `internal/http/router.NewRouter`, then serves public HTTP plus private control.
- `internal/http/router.go` owns Gin engine setup, embedded frontend/static routes, `/custom.css`, favicons, and SPA fallback.
- `internal/wiki/auth/routes.go` owns `/api/auth/*`, `/api/config`, `/api/users`, and MCP API-key management routes.
- `internal/wiki/oauth/routes.go` registers OAuth metadata on the root Gin engine and OAuth routes under the base path.
- `internal/wiki/mcp/routes.go` owns public `/mcp`, public auth verification for API keys/OAuth, and MCP request handling.
- `internal/wiki/branding/routes.go` and `internal/branding/branding_store.go` currently store and serve branding from workspace data paths even though the desired behavior is global to the runtime.
- `internal/wiki/presence/routes.go` mixes session-facing heartbeat behavior with workspace page resolution and needs a split between `frontd` session surface and `workspaced` page-context adapter.
- `internal/wiki/workspace.go` reserves current workspace data paths, including auth-related paths. The implementation should ensure old auth DB paths are handled explicitly and safely.

## Proposed Route Ownership From Discovery

`frontd` should own:

- `/`, route fallbacks, `/static/*`, frontend shell responses
- `/custom.css`, `/favicon.ico`, `/favicon.svg`
- `/api/config`
- `/api/auth/login`, `/api/auth/logout`, `/api/auth/refresh-token`, `/api/auth/me`
- `/api/users*`
- `/api/users/*/mcp-api-keys*`
- `/.well-known/*`
- `/oauth/*`
- `/api/branding`, `/api/branding/*`, `/branding/*`
- `/api/health` aggregate health/readiness
- public `/mcp` auth termination and proxying

`workspaced` should own workspace semantics behind private routing:

- `/api/tree`
- `/api/pages*`
- `/assets/*`
- `/api/pages/*/assets*`
- `/api/search*`
- `/api/pages/*/links`
- `/api/tags*`
- `/api/properties*`
- `/api/import/*`
- `/api/pages/*/revisions*`
- `/api/workspace-sync/*`
- workspace page-context portions of `/api/presence/*`
- MCP tool implementation

`wikid` should own backing services for:

- runtime supervision
- descriptors and private control authority
- auth/user/session/API-key/OAuth stores and services
- global branding state if it remains storage-backed
- current one-workspace registry

## Wrapper And CLI Observations

- `scripts/run.sh` is the stable user surface for `mcp` and `agent-hook`.
- `scripts/run.sh` maps wrapper flags and env vars into `leafwiki --mcp=stdio`, config mode, API-key env, logging, workspace sync defaults, dry-run/redaction, and stdout hygiene.
- `scripts/test-run.sh` is the strongest wrapper parity test. It covers help text, dry-run command shape, config conflicts, secret redaction, stdin/stdout hygiene, non-loopback STDIO attach, and hook fail-open behavior.
- `scripts/README.md`, `docs/mcp.md`, `README.md`, `docs/agent-hooks.md`, and `docs/workspace-sync.md` are public contract surfaces that need update or explicit verification.
- The common downstream MCP config shape invokes `/opt/homebrew/bin/run.sh mcp --data-dir ... --root-dir ... --markdown-link-root-prefix ...`.

## Test Anchors Identified

Shell and command tests:

```bash
rtk bash scripts/test-run.sh
rtk bash -n scripts/run.sh scripts/test-run.sh scripts/install-all-macos.sh scripts/test-install-all-macos.sh
```

Focused Go tests:

```bash
rtk go test ./cmd/leafwiki ./internal/projectdaemon ./internal/wiki ./internal/wiki/mcp ./internal/locking
```

Broader Go verification:

```bash
rtk go test ./...
```

Frontend and E2E hygiene:

```bash
rtk npm --prefix e2e run lint
rtk npm --prefix e2e run format:check
rtk npm --prefix ui/leafwiki-ui run lint
rtk npm --prefix ui/leafwiki-ui run build
```

Focused E2E parity paths already exist or should be extended:

```bash
rtk env E2E_RUN_MODE=local E2E_ENABLE_MCP_LOCAL=1 E2E_MCP_CLIENT_TRANSPORT=stdio ./e2e/run.sh tests/mcp-stdio-disable-auth.spec.ts
rtk env E2E_RUN_MODE=local E2E_ENABLE_MCP_API_KEYS_LOCAL=1 E2E_MCP_CLIENT_TRANSPORT=stdio ./e2e/run.sh tests/mcp-stdio-api-keys.spec.ts
rtk env E2E_RUN_MODE=local E2E_ENABLE_MCP_LOCAL=1 ./e2e/run.sh tests/mcp-disable-auth.spec.ts
rtk env E2E_RUN_MODE=local E2E_ENABLE_MCP_API_KEYS_LOCAL=1 ./e2e/run.sh tests/mcp-api-keys.spec.ts
rtk env E2E_RUN_MODE=local E2E_ENABLE_MCP_OAUTH_LOCAL=1 ./e2e/run.sh tests/mcp-oauth.spec.ts
rtk env E2E_RUN_MODE=local E2E_ENABLE_MCP_LOCAL=1 E2E_ENABLE_WORKSPACE_SYNC=1 ./e2e/run.sh tests/mcp-agent-context.spec.ts
rtk env E2E_RUN_MODE=local E2E_ENABLE_MCP_LOCAL=1 E2E_ENABLE_WORKSPACE_SYNC=1 ./e2e/run.sh tests/mcp-safe-edits.spec.ts
rtk env E2E_RUN_MODE=local E2E_ENABLE_AGENT_HOOKS_LOCAL=1 E2E_ENABLE_MCP_LOCAL=1 E2E_MCP_CLIENT_TRANSPORT=stdio ./e2e/run.sh tests/agent-hooks.spec.ts
```

## Constraints That Shape The Plan

- Keep the first implementation behind a hidden runtime switch such as `LEAFWIKI_RUNTIME_STACK=wikid-frontd`.
- Preserve the existing `project-daemon.json` path as the attach compatibility point, either as the evolved `wikid` descriptor or as a trusted pointer to it.
- Use same-binary child role processes for `wikid`, `frontd`, and `workspaced`; do not make this a goroutine-only refactor.
- Keep one project workspace. Avoid registry, grants, multi-workspace UI, cross-workspace search, workspace discovery, and normalized federation APIs.
- Keep old and extracted runtime paths available long enough to run parity tests, then flip the default after the test matrix is green.
- Use private loopback and daemon tokens for frontd-to-workspaced traffic.
- Strip public credentials before proxying to `workspaced`.
- Never let public actor-context headers authorize workspace behavior.
- Store new auth data under `<data-dir>/.leafwiki/wikid/auth/`.
- Delete only the known legacy auth DB paths at launch.
- Preserve admin bootstrap semantics.
- Keep public OAuth resource and issuer stable through the same public `frontd` URL.
- Keep `frontd` as the only supported UI ingress.
- Expect unrelated wiki validation noise if pre-existing missing assets remain.

## Immediate Planning Implications

- The implementation plan needs explicit architecture, module tree, dependency graph, state machine, test matrix, and verification commands.
- The plan should be test-first and keep each implementation unit small enough to be executed independently.
- The first units should build shared role descriptor/control primitives and test harnesses before moving routes.
- Auth storage relocation should happen before public route migration, so `workspaced` can be tested as auth-store-free.
- HTTP MCP proxying and STDIO bridging require focused compatibility tests because they are easy to regress despite unchanged URLs.
- Failure/restart behavior needs tests early enough to avoid bolting on supervision after routing has already moved.
