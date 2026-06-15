<!-- leafwiki
version: 1
page:
  id: bJh-uI-vg
  title: LeafWiki MCP Transport Unification and Sidecar Removal Plan
  created_at: "2026-06-15T05:44:44.856263666Z"
  updated_at: "2026-06-15T05:44:44.856263666Z"
  creator_id: system
  last_author_id: system
-->

# LeafWiki MCP Transport Unification and Sidecar Removal Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` to implement this plan task-by-task. Suggested workers: CLI/config, native STDIO auth, sidecar/removal scripts, E2E, docs/release cleanup.

**Goal:** Replace boolean MCP flags and the `leafwiki-mcp-stdio` sidecar with one native `--mcp` transport selector that supports HTTP MCP, STDIO MCP, and native STDIO API-key identity.

**Reference Thread:** `codex://threads/019e8a0f-9674-7780-b6ee-1cfd7be07f67`

**Plan Artifact:** save this as `plans/mcp_transport_unification.PLAN.md`.

**Primary References:** current native STDIO plan `plans/stdio.PLAN.md`; sidecar plan `plans/mcp_stdio_sidecar.PLAN.md`; current entrypoints `cmd/leafwiki/main.go`, `internal/wiki/wiki.go`, `internal/wiki/mcp/helpers.go`, `internal/wiki/mcp/routes.go`, `scripts/run-mcp.sh`, `e2e/run.sh`.

---

## Summary

- Add `--mcp=<none|http|stdio|http,stdio|stdio,http>` and `LEAFWIKI_MCP`.
- Remove the `leafwiki-mcp-stdio` sidecar binary/package, sidecar scripts, sidecar release/install surfaces, and sidecar E2E mode.
- Remove public use of `--enable-mcp`, `--mcp-stdio`, `LEAFWIKI_ENABLE_MCP`, and `LEAFWIKI_MCP_STDIO`; keep old flags/env parseable/ignored without errors.
- Add native STDIO API-key actor support with `LEAFWIKI_MCP_API_KEY` preferred and `--api-key` as a convenience. Warn in docs that CLI secrets can leak through process listings.
- Keep HTTP `/mcp` and native STDIO auth separate: `--api-key` authenticates the STDIO actor only and must not grant HTTP `/mcp` access.

## Public Contract

- `--mcp` grammar:
  - Default: `none`.
  - Valid: `none`, `http`, `stdio`, `http,stdio`, `stdio,http`.
  - Invalid values fail startup.
  - `none` cannot be combined with another transport.
  - CLI value overrides `LEAFWIKI_MCP`.
- Old public flags/env:
  - `--enable-mcp`, `--mcp-stdio`, `LEAFWIKI_ENABLE_MCP`, and `LEAFWIKI_MCP_STDIO` are ignored silently.
  - Do not document them in help, README, or MCP docs.
- STDIO identity:
  - `--mcp=stdio --disable-auth` uses `public-editor`.
  - `--mcp=stdio --api-key lwk_...` or `LEAFWIKI_MCP_API_KEY=lwk_... leafwiki --mcp=stdio` uses the API key owner.
  - `--disable-auth` plus API key fails.
  - Auth-enabled STDIO without API key fails.
  - API key without `stdio` enabled is ignored and must not affect HTTP MCP.
- Auth-enabled STDIO still requires normal auth config: `--jwt-secret` and `--admin-password`, because the user/API-key store is auth-backed.
- `--mcp=stdio` starts the native combined runtime: HTTP UI plus STDIO MCP in one process, stdout protocol-only, HTTP UI stops when STDIO exits.
- `--mcp=http` registers HTTP `/mcp` only.
- `--mcp=stdio,http` enables both native STDIO MCP and HTTP `/mcp`.

## Implementation Changes

- CLI/config worker:
  - Replace `enableMCP`/`mcpStdio` runtime decisions with a parsed `mcpTransports` value.
  - Add `--mcp` and `LEAFWIKI_MCP`; keep old flags registered only so they parse and are ignored.
  - Add `--api-key` and `LEAFWIKI_MCP_API_KEY`; read it only for STDIO mode.
  - Update validation: loopback required for `http` or `stdio`; `--log-target stdout` rejected when `stdio` is enabled; invalid `--mcp` values rejected.
- Native STDIO auth worker:
  - Add a STDIO-only MCP server auth option, for example `StdioAuth{DisabledAuth bool, APIKey string}`.
  - Add `Routes.NewStdioServer(opts, auth)` or equivalent that copies `Routes` and applies STDIO fallback auth only to that copied server.
  - In STDIO API-key mode, verify the key before starting the HTTP listener, then verify again in `actorForMissingTokenInfo` on actor-bound tool calls so revocation, deletion, and role changes take effect during a live session.
  - Do not put the STDIO API-key fallback on HTTP `/mcp` route registration.
- Sidecar removal worker:
  - Remove `cmd/leafwiki-mcp-stdio` and `internal/wiki/mcpstdio`.
  - Remove `make build-sidecar`, sidecar build/install scripts, sidecar install tests, and sidecar release artifact references.
  - Update `scripts/install-all-macos.sh` to install only `leafwiki` and `run-mcp.sh`.
  - Update `scripts/run-mcp.sh` to be native-only: no `--mode`, no sidecar lifecycle, no `--endpoint`, no sidecar binary lookup. Accept removed wrapper flags without errors; ignore removed sidecar-only flags. If `--api-key` or `LEAFWIKI_RUN_MCP_API_KEY` is supplied, pass it to child `leafwiki` via `LEAFWIKI_MCP_API_KEY`, not as a child CLI arg.
- E2E worker:
  - Remove `E2E_MCP_STDIO_NATIVE`; `E2E_MCP_CLIENT_TRANSPORT=stdio` always uses native `leafwiki --mcp=stdio`.
  - For STDIO API-key E2E, add an E2E seed helper that creates auth users and API keys directly in the temp data dir before Playwright starts. Use `internal/core/auth.UserService` and `APIKeyService`.
  - Update `mcpClient.ts` so STDIO clients pass `LEAFWIKI_MCP_API_KEY` to the spawned native LeafWiki process and no longer pass endpoint env.
  - Keep browser assertions by using the HTTP UI served by the currently running STDIO process.
- Documentation worker:
  - Rewrite `docs/mcp.md`, `README.md`, and `scripts/README.md` around `--mcp`.
  - Include `codex://threads/019e8a0f-9674-7780-b6ee-1cfd7be07f67`.
  - Remove sidecar examples, sidecar troubleshooting, sidecar release/install notes, and old flag/env tables.
  - Document secret handling: prefer env, CLI `--api-key` exists but can leak via process listings.

## Gherkin Test Suite

```gherkin
Feature: MCP transport option parsing

  Scenario: Default MCP mode is none
    Given no --mcp flag and no LEAFWIKI_MCP env var
    When LeafWiki startup config is resolved
    Then HTTP MCP is disabled
    And STDIO MCP is disabled

  Scenario: CLI --mcp enables HTTP MCP
    Given LeafWiki starts with "--mcp=http --host 127.0.0.1"
    When startup config is resolved
    Then HTTP MCP is enabled
    And STDIO MCP is disabled

  Scenario: CLI --mcp enables STDIO MCP
    Given LeafWiki starts with "--mcp=stdio --disable-auth --host 127.0.0.1"
    When startup config is resolved
    Then STDIO MCP is enabled
    And HTTP MCP is disabled

  Scenario: CLI --mcp enables both transports
    Given LeafWiki starts with "--mcp=stdio,http --disable-auth --host 127.0.0.1"
    When startup config is resolved
    Then STDIO MCP is enabled
    And HTTP MCP is enabled

  Scenario: Env var enables transport
    Given LEAFWIKI_MCP is "http"
    When LeafWiki starts without --mcp
    Then HTTP MCP is enabled

  Scenario: CLI overrides env var
    Given LEAFWIKI_MCP is "http"
    When LeafWiki starts with "--mcp=stdio --disable-auth"
    Then STDIO MCP is enabled
    And HTTP MCP is disabled

  Scenario: Unknown transport fails
    Given LeafWiki starts with "--mcp=websocket"
    Then startup exits non-zero
    And stderr says the MCP transport is invalid
    And stdout is empty

  Scenario: none cannot be combined
    Given LeafWiki starts with "--mcp=none,stdio"
    Then startup exits non-zero
    And stderr says none cannot be combined with other MCP transports

  Scenario: Old flags are ignored
    Given LeafWiki starts with "--enable-mcp --mcp-stdio --disable-auth"
    And no --mcp flag is provided
    When startup config is resolved
    Then HTTP MCP is disabled
    And STDIO MCP is disabled

  Scenario: Old env vars are ignored
    Given LEAFWIKI_ENABLE_MCP is "true"
    And LEAFWIKI_MCP_STDIO is "true"
    And LEAFWIKI_MCP is unset
    When LeafWiki startup config is resolved
    Then HTTP MCP is disabled
    And STDIO MCP is disabled
```

```gherkin
Feature: MCP startup validation

  Scenario: HTTP MCP requires loopback host
    Given LeafWiki starts with "--mcp=http --host 0.0.0.0"
    Then startup exits non-zero
    And stderr says MCP requires a loopback host

  Scenario: STDIO MCP requires loopback host
    Given LeafWiki starts with "--mcp=stdio --disable-auth --host 0.0.0.0"
    Then startup exits non-zero
    And stderr says MCP requires a loopback host
    And stdout is empty

  Scenario: STDIO MCP rejects stdout logging
    Given LeafWiki starts with "--mcp=stdio --disable-auth --log-target stdout"
    Then startup exits non-zero
    And stderr says stdout is reserved for MCP STDIO
    And stdout is empty

  Scenario: API key without STDIO is ignored
    Given LeafWiki starts with "--mcp=http --api-key lwk_invalid --jwt-secret secret --admin-password admin"
    When startup validation runs
    Then startup does not validate the API key
    And HTTP MCP remains protected by HTTP bearer auth

  Scenario: STDIO auth-enabled mode requires API key
    Given LeafWiki starts with "--mcp=stdio --jwt-secret secret --admin-password admin"
    Then startup exits non-zero
    And stderr says native STDIO requires either disabled auth or an API key
    And stdout is empty

  Scenario: Disabled auth and API key are mutually exclusive
    Given LeafWiki starts with "--mcp=stdio --disable-auth --api-key lwk_fake"
    Then startup exits non-zero
    And stderr says disabled auth and API-key STDIO identity cannot be combined
    And stdout is empty

  Scenario: API key is redacted from startup errors
    Given LeafWiki starts with "--mcp=stdio --api-key lwk_secret_bad --jwt-secret secret --admin-password admin"
    Then startup exits non-zero
    And stdout does not contain "lwk_secret_bad"
    And stderr does not contain "lwk_secret_bad"
```

```gherkin
Feature: Native STDIO disabled-auth behavior

  Scenario: STDIO disabled-auth uses public editor
    Given LeafWiki is started with "--mcp=stdio --disable-auth"
    When an MCP STDIO client calls "get_current_user"
    Then the returned username is "public-editor"
    And the returned role is "editor"

  Scenario: STDIO disabled-auth can mutate pages
    Given LeafWiki is started with "--mcp=stdio --disable-auth"
    When an MCP STDIO client calls "create_page"
    Then the page is created
    And the page is visible through the HTTP UI served by the same process

  Scenario: STDIO mode does not expose HTTP MCP unless requested
    Given LeafWiki is started with "--mcp=stdio --disable-auth"
    When an HTTP request is sent to "/mcp"
    Then the response is 404

  Scenario: STDIO plus HTTP exposes both transports
    Given LeafWiki is started with "--mcp=stdio,http --disable-auth"
    When a STDIO MCP client lists tools
    Then tools are returned
    When an HTTP MCP client lists tools
    Then the same tool names are returned
```

```gherkin
Feature: Native STDIO API-key identity

  Scenario: Admin API key authenticates STDIO actor
    Given an admin API key exists in the data dir
    When LeafWiki starts with "--mcp=stdio" and LEAFWIKI_MCP_API_KEY set to that key
    And the MCP STDIO client calls "get_current_user"
    Then the returned username is "admin"
    And the returned role is "admin"

  Scenario: Editor API key can mutate
    Given an editor API key exists in the data dir
    When the MCP STDIO client calls "create_page"
    Then the page is created
    And the revision/user metadata uses the editor user id

  Scenario: Viewer API key can read but cannot mutate
    Given a viewer API key exists in the data dir
    When the MCP STDIO client calls "get_tree"
    Then the call succeeds
    When the MCP STDIO client calls "create_page"
    Then the call fails with an editor-or-admin role error

  Scenario: Revoked API key fails at startup
    Given a revoked API key exists in the data dir
    When LeafWiki starts with "--mcp=stdio" and that API key
    Then startup exits non-zero
    And stdout is empty
    And stderr does not contain the raw API key

  Scenario: Deleted-user API key fails at startup
    Given an API key exists for a deleted user
    When LeafWiki starts with "--mcp=stdio" and that API key
    Then startup exits non-zero
    And stdout is empty
    And stderr does not contain the raw API key

  Scenario: Revoked key fails during a live STDIO session
    Given an editor API key is used for a live STDIO session
    When an admin revokes that key through the HTTP UI served by the same process
    And the STDIO client calls "create_page" again
    Then the call fails with an authenticated MCP user error

  Scenario: Role downgrade takes effect during a live STDIO session
    Given an editor API key is used for a live STDIO session
    When an admin changes the key owner role to viewer through the HTTP UI
    And the STDIO client calls "create_page"
    Then the call fails with an editor-or-admin role error
    When the STDIO client calls "get_tree"
    Then the call succeeds
```

```gherkin
Feature: STDIO protocol and lifecycle

  Scenario: STDIO stdout contains only JSON-RPC
    Given LeafWiki is running with "--mcp=stdio --disable-auth"
    When the MCP client sends initialize and tools/list
    Then every stdout line is valid JSON-RPC
    And stdout contains no logs, URLs, banners, or shell output

  Scenario: Diagnostics are away from stdout
    Given LeafWiki is running with "--mcp=stdio --disable-auth --log-target stderr"
    When startup completes
    Then the HTTP URL is written to stderr
    And stdout remains empty until a protocol frame is sent

  Scenario: Client closes stdin
    Given LeafWiki is running with "--mcp=stdio --disable-auth"
    When the MCP client closes stdin
    Then the STDIO session ends cleanly
    And the HTTP UI stops
    And the data-dir lock is released

  Scenario: SIGTERM shuts down cleanly
    Given LeafWiki is running with "--mcp=stdio --disable-auth"
    When the process receives SIGTERM
    Then HTTP shuts down
    And the data-dir lock is released
    And stdout contains no non-protocol text

  Scenario: Malformed STDIO JSON returns protocol error
    Given LeafWiki is running with "--mcp=stdio --disable-auth"
    When stdin receives a non-JSON line
    Then stdout contains a JSON-RPC parse error
    And the process remains able to handle a later valid initialize frame
```

```gherkin
Feature: HTTP MCP separation

  Scenario: HTTP MCP remains bearer-auth protected
    Given LeafWiki starts with "--mcp=http --api-key lwk_valid --jwt-secret secret --admin-password admin"
    When an HTTP MCP request is sent without Authorization
    Then the request is rejected
    And the CLI API key is not used for HTTP authentication

  Scenario: API-key bearer still works over HTTP MCP
    Given LeafWiki starts with "--mcp=http --jwt-secret secret --admin-password admin"
    And a valid MCP API key exists
    When an HTTP MCP client sends Authorization Bearer with that key
    Then MCP initialization succeeds

  Scenario: OAuth HTTP MCP remains HTTP-only
    Given LeafWiki starts with "--mcp=http --jwt-secret secret --admin-password admin"
    When an OAuth-capable MCP client authenticates over HTTP
    Then OAuth MCP works
    And no STDIO process is started
```

```gherkin
Feature: run-mcp wrapper

  Scenario: Wrapper defaults to native STDIO
    When "scripts/run-mcp.sh --dry-run" is run
    Then stderr shows one "leafwiki" command
    And the command includes "--mcp=stdio"
    And the command does not include "leafwiki-mcp-stdio"

  Scenario: Wrapper passes API key through environment
    Given LEAFWIKI_RUN_MCP_API_KEY is "lwk_secret"
    When "scripts/run-mcp.sh --dry-run" is run
    Then the planned child command does not include "lwk_secret"
    And the wrapper plans LEAFWIKI_MCP_API_KEY for the child environment

  Scenario: Wrapper accepts old sidecar flags without errors
    When "scripts/run-mcp.sh --mode sidecar --endpoint http://127.0.0.1:8080/mcp --mcp-stdio-bin /tmp/old --dry-run" is run
    Then the wrapper exits zero
    And stderr shows native "--mcp=stdio"
    And stderr does not show "leafwiki-mcp-stdio"

  Scenario: Wrapper preserves stdout hygiene
    Given "run-mcp.sh" starts a fake native leafwiki process
    When the fake process writes protocol output to stdout and diagnostics to stderr
    Then wrapper stdout contains only the protocol output
    And wrapper stderr contains diagnostics
```

```gherkin
Feature: Sidecar removal and install surface

  Scenario: Sidecar command package is gone
    When "go list ./cmd/leafwiki-mcp-stdio" is run
    Then the package does not exist

  Scenario: Sidecar internal proxy package is gone
    When "go list ./internal/wiki/mcpstdio" is run
    Then the package does not exist

  Scenario: Makefile no longer advertises sidecar build
    When "make help" is run
    Then output does not contain "build-sidecar"
    And output does not contain "leafwiki-mcp-stdio"

  Scenario: macOS all installer omits sidecar
    When "scripts/install-all-macos.sh --dry-run" is run
    Then output says it would install LeafWiki and run-mcp.sh
    And output does not mention leafwiki-mcp-stdio
```

```gherkin
Feature: Documentation

  Scenario: README documents new MCP option
    Given README.md is opened
    Then it documents "--mcp=none|http|stdio|http,stdio"
    And it documents LEAFWIKI_MCP
    And it does not document "--enable-mcp" or "--mcp-stdio"

  Scenario: MCP docs include this thread link
    Given docs/mcp.md is opened
    Then it contains "codex://threads/019e8a0f-9674-7780-b6ee-1cfd7be07f67"

  Scenario: MCP docs explain API-key STDIO
    Given docs/mcp.md is opened
    Then it recommends LEAFWIKI_MCP_API_KEY over --api-key
    And it warns that CLI secrets can leak through process listings
    And it says --api-key applies only to native STDIO

  Scenario: Sidecar docs are removed
    Given docs/mcp.md and scripts/README.md are opened
    Then neither file contains "leafwiki-mcp-stdio"
    And neither file contains sidecar setup examples
```

## Verification Commands

```bash
rtk go test ./cmd/leafwiki ./internal/wiki ./internal/wiki/mcp ./internal/locking
rtk go test ./...
rtk bash -n scripts/run-mcp.sh scripts/test-run-mcp.sh scripts/install-all-macos.sh scripts/test-install-all-macos.sh
rtk bash scripts/test-run-mcp.sh
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

- [ ] Plan is saved at `plans/mcp_transport_unification.PLAN.md` and includes the thread link.
- [ ] `leafwiki --help` documents `--mcp`, `LEAFWIKI_MCP`, `--api-key`, and `LEAFWIKI_MCP_API_KEY`.
- [ ] `--enable-mcp`, `--mcp-stdio`, `LEAFWIKI_ENABLE_MCP`, and `LEAFWIKI_MCP_STDIO` are ignored without startup errors and are not documented.
- [ ] `--mcp` supports exactly `none`, `http`, `stdio`, and both combined orderings.
- [ ] Native STDIO disabled-auth still works as `public-editor`.
- [ ] Native STDIO API-key auth works for admin/editor/viewer users with current role enforcement.
- [ ] Revoked/deleted API keys fail, and live revocation/role changes affect later actor-bound STDIO tool calls.
- [ ] `--api-key` and `LEAFWIKI_MCP_API_KEY` never affect HTTP `/mcp` authentication.
- [ ] `--mcp=stdio` keeps stdout protocol-only and shuts down HTTP UI on STDIO disconnect.
- [ ] `--mcp=stdio,http` exposes both transports with the same tool surface.
- [ ] `leafwiki-mcp-stdio` command and `internal/wiki/mcpstdio` package are removed.
- [ ] Build, install, release, docs, and tests no longer mention sidecar artifacts.
- [ ] `scripts/run-mcp.sh` is native-only, accepts old sidecar flags without errors, and passes API keys through env.
- [ ] E2E STDIO disabled-auth and API-key suites use native STDIO only.
- [ ] HTTP MCP OAuth/API-key/disabled-auth E2E suites still pass with `--mcp=http`.
- [ ] All verification commands pass.
- [ ] `git status --short` contains only intentional implementation, tests, docs, and plan changes.

## Assumptions

- This is a breaking MCP CLI cleanup; no compatibility aliases are provided.
- Old flags/env are parseable and ignored silently because that was explicitly chosen.
- CLI still follows repo precedent: CLI value overrides env, env overrides default.
- API-key STDIO identity is a single process-level actor, not per-client dynamic auth.
- API-key secrets may live in process memory, but must not be logged or written to stdout.
- The sidecar bridge to an already-running HTTP `/mcp` server is intentionally removed.
