<!-- leafwiki
version: 1
page:
  id: ab2aXIavR
  title: LeafWiki Transparent Per-Project Daemon Plan
  created_at: "2026-06-15T05:44:44.858067362Z"
  updated_at: "2026-06-15T05:44:44.858067362Z"
  creator_id: system
  last_author_id: system
-->

# LeafWiki Transparent Per-Project Daemon Plan

> Historical plan. This transparent per-project daemon design has since evolved
> into the federated `wikid`/`frontd`/`workspaced` runtime and the public
> `leafwiki daemon` service entrypoint. Keep this file as design history; use
> `docs/plans/federated-workspaces.PLAN.md`, `docs/mcp.md`, and
> `scripts/README.md` for the current runtime contract.

## Summary

Reference thread: `codex://threads/019e8df8-d944-71f3-956e-59eb999abdff`
Plan artifact: save as `plans/project_daemon.PLAN.md`.

Build a transparent per-project owner process so multiple humans and agents can share one LeafWiki project without lock/port conflicts. The project identity is the canonical resolved pair `(data-dir, root-dir)`. Every normal LeafWiki startup participates: the first compatible startup auto-starts a detached project owner daemon; later compatible startups attach to it. STDIO clients still get their own child process, but that child is a session frontend, not a second wiki server.

Key behavior:

- `stdio` means “this foreground process should speak MCP over stdin/stdout”; it is per-session, not daemon identity.
- `http` means “the project owner exposes public HTTP `/mcp`”; this is daemon-relevant and locked by the first startup.
- `LEAFWIKI_MCP_API_KEY` / `--api-key` is per-STDIO-session identity and must not be part of daemon config matching.
- The daemon shuts down after the last session handle exits plus `--daemon-idle-timeout`, default `10m`; `0` means immediate shutdown after the last handle.
- Existing `--mcp=stdio`, `--mcp=stdio,http`, disabled-auth, API-key STDIO, base-path, root-dir, logging, and `scripts/run.sh mcp` user-facing invocations must continue working.

## References

Use these as implementation context:

- `cmd/leafwiki/main.go`: CLI parsing, startup validation, locking, current native STDIO runtime.
- `internal/locking`: existing data/root lock semantics.
- `internal/wiki/mcp`: HTTP MCP route registration, `NewStdioServer`, API-key actor fallback.
- `scripts/run.sh mcp` and `scripts/test-run.sh`: keep external wrapper stable.
- `e2e/run.sh`, `e2e/tests/mcpClient.ts`, `mcp-stdio-disable-auth.spec.ts`, `mcp-stdio-api-keys.spec.ts`.
- This plan is the self-contained implementation contract for the transparent project daemon work.

## Implementation Changes

- Add an internal daemon package, for example `internal/projectdaemon`, owning daemon primitives while the CLI wires them into startup:
  - canonical project key resolution from normalized `DataDir` and `RootDir`;
  - daemon descriptor read/write at `<data-dir>/.leafwiki/project-daemon.json`;
  - config hashing/comparison with secret redaction;
  - session handle registration and heartbeat expiry;
  - control-server endpoints for health, session lifecycle, STDIO auth verification, and private MCP forwarding.

- Keep process orchestration in `cmd/leafwiki/main.go` where it can reuse CLI parsing, runtime validation, logging setup, HTTP router construction, lock acquisition, and hidden internal mode bootstrap. That orchestration should be thin around `internal/projectdaemon` primitives and covered by process tests.

- Add one public option:
  - `--daemon-idle-timeout`, env `LEAFWIKI_DAEMON_IDLE_TIMEOUT`, Go duration, default `10m`, `0` immediate after last handle.
  - Treat idle timeout as daemon-relevant config.

- Daemon descriptor must be written atomically with mode `0600` and include:
  - schema version, pid, started time, canonical data/root dirs, public URL, public MCP enabled flag, base path, control URL, config hash, idle timeout, and a random control token.
  - Do not store `LEAFWIKI_MCP_API_KEY`.

- Startup flow:
  - Existing data/root validation still runs before daemon attach.
  - Launcher resolves full config and canonical project key.
  - If healthy descriptor exists, compare daemon-relevant config; fail with redacted mismatch details if incompatible.
  - If no healthy owner exists, spawn detached internal daemon using a temporary `0600` startup config file; never pass API keys to the daemon.
  - Foreground process registers a session handle, then either blocks as an HTTP/server handle or bridges STDIO.

- Daemon owner flow:
  - Hidden internal daemon mode reads startup config, acquires existing data/root locks, initializes one `*wiki.Wiki`, starts the normal HTTP UI, and starts a private loopback control server.
  - Public `/mcp` is enabled only when first startup requested `--mcp=http` or `--mcp=stdio,http`.
  - A private control MCP endpoint is always available for STDIO frontends but protected by the control token; this must not make public `/mcp` reachable for plain `--mcp=stdio`.

- STDIO frontend bridge:
  - Reuse the existing malformed-JSON filter behavior so invalid STDIO frames return JSON-RPC parse errors and do not kill the session.
  - Connect stdin/stdout to the daemon private MCP endpoint by piping SDK `Connection` reads/writes between an `IOTransport` and `StreamableClientTransport`.
  - Use an `http.Client` wrapper to add the daemon control token to private control requests.
  - For auth-enabled STDIO, pass the per-session API key as bearer auth to the private MCP endpoint and preserve live revocation/role-change checks.
  - For disabled-auth STDIO, no API key is required and the effective actor remains `public-editor`.

- Config mismatch rules:
  - Must match for owner-affecting starts: canonical dirs, auth disabled/enabled mode, public HTTP MCP enabled/disabled, host, port, base path, public access, allow-insecure, token timeouts, injected header/custom stylesheet, hidden link-metadata setting, upload size, revision/link-refactor flags, max revision history, remote-user settings, request-log/log config, idle timeout.
  - STDIO-only attaches inherit the owner's host, public HTTP MCP, request-log, log target, and log file settings because they do not change the daemon's public bind address, public MCP surface, or logging.
  - Must not match: STDIO presence, `--api-key`, `LEAFWIKI_MCP_API_KEY`, per-client MCP name/version.
  - If first owner started without public HTTP MCP, later `--mcp=http` or `--mcp=stdio,http` fails and tells user to stop/restart the project owner with HTTP MCP enabled.
  - Later `--mcp=stdio` may attach even when public HTTP MCP is disabled.

- Documentation:
  - Update `docs/mcp.md`, `scripts/README.md`, README/help text, and troubleshooting.
  - Explain transparent project daemon, project identity, idle timeout, config mismatch, per-agent API keys, and that `--mcp=stdio` does not expose public `/mcp`.
  - Include the reference thread link.
  - Preserve `scripts/run.sh mcp` examples and explain that it still spawns the agent-owned STDIO process.

- Suggested subagents:
  - CLI/daemon lifecycle worker: config resolution, descriptor, spawn, handles, idle shutdown.
  - MCP bridge/auth worker: private control MCP endpoint, STDIO bridge, API-key/disabled-auth behavior.
  - Tests/E2E worker: Go integration tests, shell tests, Playwright scenarios.
  - Docs/scripts worker: help text, docs, wrapper tests.
  - Review worker: independent pass for lock safety, stdout hygiene, secret redaction, and stale daemon recovery.

## Gherkin Test Suite

```gherkin
Feature: Project daemon startup and identity

  Scenario: First normal startup auto-starts a project owner
    Given no daemon descriptor exists for canonical data-dir and root-dir
    When LeafWiki starts with "--host 127.0.0.1 --port <port> --data-dir <data> --root-dir <root>"
    Then a detached project owner is started
    And the foreground process blocks as a session handle
    And the HTTP UI is reachable
    And the daemon descriptor contains canonical data-dir and root-dir

  Scenario: Second normal startup attaches to the existing owner
    Given a healthy owner exists for canonical data-dir and root-dir
    When LeafWiki starts with the same daemon-relevant options
    Then it does not acquire data/root ownership itself
    And it registers a second session handle
    And it does not fail with data-dir or root-dir lock errors

  Scenario: Canonical path variants resolve to the same project
    Given a healthy owner started with clean absolute data-dir and root-dir
    When a second startup uses equivalent relative, cleaned, or symlink-resolved paths
    Then it attaches to the existing owner

  Scenario: Invalid workspace still fails before daemon attach
    When LeafWiki starts with "--data-dir <dir> --root-dir <dir>"
    Then startup exits non-zero
    And no daemon descriptor is written
    And stderr mentions "root dir must be different from data dir"

  Scenario: Stale descriptor is ignored when no owner is healthy
    Given a daemon descriptor points to a dead pid or unreachable control URL
    And the data/root locks are free
    When LeafWiki starts with matching options
    Then it starts a new owner
    And replaces the stale descriptor atomically

  Scenario: Lock held without healthy descriptor fails clearly
    Given the data or root lock is held by another process
    And no healthy descriptor is reachable
    When LeafWiki starts for that project
    Then startup exits non-zero
    And stderr explains that the project is locked but no attachable daemon was found
```

```gherkin
Feature: Daemon config locking

  Scenario: Matching config attaches
    Given an owner started with public HTTP MCP disabled
    When a second startup uses the same daemon-relevant options
    Then it attaches successfully

  Scenario: Different port is rejected
    Given an owner started on port 8080
    When a second startup uses the same project with "--port 8081"
    Then startup fails with a redacted config mismatch mentioning "port"

  Scenario: Different base path is rejected
    Given an owner started with "--base-path /wiki"
    When a second startup uses the same project without that base path
    Then startup fails with a redacted config mismatch mentioning "base-path"

  Scenario: Different auth mode is rejected
    Given an owner started with auth enabled
    When a second startup uses "--disable-auth"
    Then startup fails with a config mismatch mentioning auth mode

  Scenario: Different API key is accepted for STDIO
    Given an auth-enabled owner exists
    And API keys exist for users Codex and Claude
    When Codex starts STDIO with key A
    And Claude starts STDIO with key B
    Then both attach successfully
    And each session reports its own user from "get_current_user"

  Scenario: Public HTTP MCP cannot be enabled after owner start
    Given an owner started without public HTTP MCP
    When a second startup uses "--mcp=http"
    Then startup fails with a config mismatch mentioning public MCP

  Scenario: STDIO can attach when public HTTP MCP is disabled
    Given an owner started without public HTTP MCP
    When a second startup uses "--mcp=stdio --disable-auth"
    Then STDIO attach succeeds
    And public "/mcp" remains not found

  Scenario: Secrets are never printed on mismatch
    Given a startup includes jwt secret, admin password, and API key
    When config mismatch causes startup failure
    Then stderr does not contain any raw secret or raw API key
```

```gherkin
Feature: STDIO daemon attach behavior

  Scenario: First STDIO startup auto-starts owner and bridges protocol
    Given no owner exists
    When an MCP client spawns "leafwiki --mcp=stdio --disable-auth"
    Then initialize succeeds over STDIO
    And the HTTP UI is reachable from the owner
    And stdout contains only JSON-RPC frames

  Scenario: Multiple disabled-auth STDIO clients collaborate
    Given an owner exists for the project
    When two STDIO clients attach with "--mcp=stdio --disable-auth"
    And client A creates a page
    Then client B can read that page
    And the web UI shows that page

  Scenario: STDIO close does not immediately stop the owner
    Given an owner exists with daemon idle timeout "10m"
    When one STDIO client closes stdin
    Then that client process exits zero
    And the HTTP UI remains reachable

  Scenario: Owner exits after idle timeout
    Given an owner exists with daemon idle timeout "1s"
    And the last session handle disconnects
    Then the HTTP UI becomes unreachable after the timeout
    And data/root locks are released

  Scenario: Idle timeout zero stops immediately
    Given an owner exists with daemon idle timeout "0"
    When the last session handle disconnects
    Then the owner stops without waiting for a grace period

  Scenario: Crashed frontend handle expires
    Given an owner has a registered session handle
    When the frontend process dies without deleting the handle
    Then daemon heartbeat expiry removes the handle
    And idle shutdown begins if no handles remain

  Scenario: Malformed STDIO JSON returns parse error and continues
    Given an attached STDIO client sends a non-JSON line
    Then stdout contains a JSON-RPC parse error
    When the client later sends a valid initialize frame
    Then initialize succeeds

  Scenario: STDIO diagnostics stay off stdout
    Given a STDIO frontend starts or attaches
    Then startup URLs, daemon messages, and logs go to stderr or configured log file
    And stdout is reserved for JSON-RPC only
```

```gherkin
Feature: API-key STDIO identity

  Scenario: Admin API key authenticates STDIO session
    Given an auth-enabled owner exists
    And an admin API key exists
    When STDIO attaches with that key
    Then "get_current_user" returns the admin user and role

  Scenario: Editor and viewer keys enforce permissions
    Given editor and viewer API keys exist
    When editor attaches over STDIO
    Then editor can create a page
    When viewer attaches over STDIO
    Then viewer can read the tree
    And viewer cannot create a page

  Scenario: Revoked key fails new STDIO attach
    Given an auth-enabled owner exists
    And an API key has been revoked
    When STDIO attaches with that key
    Then startup exits non-zero
    And stdout is empty
    And stderr does not contain the raw key

  Scenario: Revoked key affects live STDIO session
    Given a live STDIO session uses an editor API key
    When an admin revokes that key through the UI
    And the live session calls "create_page"
    Then the call fails with an authenticated MCP user error

  Scenario: Role downgrade affects live STDIO session
    Given a live STDIO session uses an editor API key
    When an admin changes that user to viewer
    Then write tools fail
    And read tools still succeed

  Scenario: Disabled-auth owner rejects API-key STDIO attach
    Given an owner started with "--disable-auth"
    When a STDIO frontend attaches with an API key
    Then startup fails with an auth-mode mismatch
```

```gherkin
Feature: Public HTTP MCP and private STDIO MCP separation

  Scenario: Plain STDIO does not expose public HTTP MCP
    Given the first startup used "--mcp=stdio --disable-auth"
    When a HTTP client requests "/mcp"
    Then the response is 404
    And STDIO MCP still works through the private control endpoint

  Scenario: Combined stdio,http exposes public HTTP MCP
    Given the first startup used "--mcp=stdio,http --disable-auth"
    When a HTTP MCP client connects to "/mcp"
    Then the HTTP MCP client succeeds
    And a STDIO client can also attach

  Scenario: HTTP-only owner supports later private STDIO attach
    Given the first startup used "--mcp=http --jwt-secret <secret> --admin-password <password>"
    When a STDIO client attaches with a valid API key
    Then STDIO MCP succeeds
    And a HTTP MCP client also succeeds through public "/mcp"

  Scenario: Plain web UI owner supports later private STDIO attach
    Given the first startup used no MCP transport and auth is disabled
    When a STDIO client attaches with "--mcp=stdio --disable-auth"
    Then STDIO MCP succeeds
    And public "/mcp" remains 404
```

```gherkin
Feature: Wrapper and E2E behavior

  Scenario: run.sh mcp dry-run remains stable
    When "scripts/run.sh mcp --dry-run" is run
    Then stderr shows a LeafWiki command with "--mcp=stdio"
    And it does not mention "leafwiki-mcp-stdio"
    And API keys are redacted

  Scenario: run.sh mcp runtime preserves protocol passthrough
    Given "scripts/run.sh mcp" starts a fake leafwiki binary
    When stdin receives a JSON-RPC initialize frame
    Then wrapper stdout contains only the fake protocol stdout
    And wrapper stderr contains diagnostics
    And raw API keys are not printed

  Scenario: E2E disabled-auth STDIO collaboration
    Given local E2E runs with E2E_MCP_CLIENT_TRANSPORT=stdio
    When MCP STDIO creates a page
    Then the web UI can edit it
    And MCP STDIO reads the UI edit back

  Scenario: E2E API-key STDIO with multiple users
    Given local E2E seeds admin, editor, viewer, revoked, and deleted-user API keys
    When multiple STDIO clients attach with different keys
    Then current user, permissions, revocation, deleted-user, and role-downgrade behavior match HTTP MCP

  Scenario: E2E base path and separate root dir still work
    Given local E2E runs with E2E_BASE_PATH=/wiki and separate root dir
    When STDIO creates and updates a page
    Then MCP config reports "/wiki"
    And markdown files are written under configured root dir, not data-dir/root
```

## Verification Commands

Run the focused suite first, then the full suite:

```bash
rtk go test ./cmd/leafwiki ./internal/projectdaemon ./internal/locking ./internal/wiki ./internal/wiki/mcp ./internal/http
rtk go test ./...
rtk bash -n scripts/run.sh scripts/test-run.sh scripts/install-all-macos.sh scripts/test-install-all-macos.sh
rtk bash scripts/test-run.sh
rtk bash scripts/test-install-all-macos.sh
rtk npm --prefix e2e run lint
rtk npm --prefix e2e run format:check
rtk npm --prefix ui/leafwiki-ui run lint
rtk npm --prefix ui/leafwiki-ui run build
rtk env E2E_RUN_MODE=local E2E_ENABLE_MCP_LOCAL=1 E2E_MCP_CLIENT_TRANSPORT=stdio ./e2e/run.sh tests/mcp-stdio-disable-auth.spec.ts
rtk env E2E_RUN_MODE=local E2E_ENABLE_MCP_LOCAL=1 E2E_BASE_PATH=/wiki E2E_MCP_CLIENT_TRANSPORT=stdio ./e2e/run.sh tests/mcp-stdio-disable-auth.spec.ts --grep "base-path"
rtk env E2E_RUN_MODE=local E2E_ENABLE_MCP_LOCAL=1 E2E_ENABLE_SEPARATE_ROOT_DIR=1 E2E_MCP_CLIENT_TRANSPORT=stdio ./e2e/run.sh tests/mcp-stdio-disable-auth.spec.ts --grep "mcp stdio seeds"
rtk env E2E_RUN_MODE=local E2E_ENABLE_MCP_API_KEYS_LOCAL=1 E2E_MCP_CLIENT_TRANSPORT=stdio ./e2e/run.sh tests/mcp-stdio-api-keys.spec.ts
rtk env E2E_RUN_MODE=local E2E_ENABLE_MCP_LOCAL=1 ./e2e/run.sh tests/mcp-disable-auth.spec.ts
rtk env E2E_RUN_MODE=local E2E_ENABLE_MCP_API_KEYS_LOCAL=1 ./e2e/run.sh tests/mcp-api-keys.spec.ts
rtk env E2E_RUN_MODE=local E2E_ENABLE_MCP_OAUTH_LOCAL=1 ./e2e/run.sh tests/mcp-oauth.spec.ts
```

## Definition Of Done

- [ ] Plan is saved at `plans/project_daemon.PLAN.md` and includes `codex://threads/019e8df8-d944-71f3-956e-59eb999abdff`.
- [ ] First startup for a canonical data/root pair starts one detached project owner and writes a secure descriptor.
- [ ] Later compatible startups attach without lock or port failure.
- [ ] Later incompatible startups fail with clear redacted config mismatch.
- [ ] `--mcp=stdio` and `--mcp=stdio,http` both use transparent STDIO session frontends.
- [ ] Plain non-STDIO server startup also participates as a foreground session handle.
- [ ] Public `/mcp` is exposed only when first startup requested HTTP MCP.
- [ ] Private STDIO MCP attach works even when public `/mcp` is disabled.
- [ ] Different STDIO API keys attach concurrently and preserve per-user identity.
- [ ] Disabled-auth STDIO still uses `public-editor`.
- [ ] API-key STDIO still supports admin/editor/viewer permissions, revocation, deletion, and role downgrade.
- [ ] Malformed STDIO JSON returns parse errors and subsequent valid frames still work.
- [ ] STDIO stdout remains protocol-only.
- [ ] Daemon idle timeout works, including `0`, heartbeat expiry, and lock release.
- [ ] `scripts/run.sh mcp` remains externally compatible and keeps redaction/stdout hygiene.
- [ ] Docs and help explain transparent daemon behavior, project identity, config mismatch, idle timeout, per-session API keys, and troubleshooting.
- [ ] All verification commands pass.
- [ ] Independent review confirms no stale sidecar packages, no secret leakage, no public `/mcp` regression for `--mcp=stdio`, and no data/root lock bypass.
