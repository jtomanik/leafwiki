<!-- leafwiki
version: 1
page:
  id: P12-uIavg
  title: Native Combined STDIO + HTTP MCP Implementation Plan
  created_at: "2026-06-15T05:44:44.856342248Z"
  updated_at: "2026-06-15T05:44:44.856342248Z"
  creator_id: system
  last_author_id: system
-->

# Native Combined STDIO + HTTP MCP Implementation Plan

**Goal:** Add a project-local `leafwiki` runtime where one process exposes MCP over STDIO for the agent and HTTP UI for the human.

**Reference Thread:** `codex://threads/019e88d1-e02b-7b93-bea6-13473fb08200`

**Architecture:** `scripts/run-mcp.sh` launches one `leafwiki --mcp-stdio --disable-auth=true` process. That process creates one `Wiki` instance, starts the HTTP web server in a goroutine, and serves MCP STDIO on stdin/stdout in the foreground. On STDIO disconnect, it gracefully stops HTTP.

**Tech Stack:** Go, Gin, Model Context Protocol Go SDK, Bash, Playwright E2E, TypeScript MCP SDK.

> **For agentic workers:** Use `superpowers:subagent-driven-development`. Suggested workers: native runtime/CLI, data-dir lock, wrapper/docs, Go tests, E2E tests.

## Key Changes

- Add native combined mode:
  - CLI flag/env: `--mcp-stdio`, `LEAFWIKI_MCP_STDIO=true`.
  - Requires disabled auth in v1; fail if auth is enabled.
  - Rejects `--log-target stdout`; stdout must be MCP JSON-RPC only.
  - Does not require `--enable-mcp`; HTTP `/mcp` remains separately controlled by `--enable-mcp`.

- Reuse the existing MCP tool server:
  - Extract a reusable MCP server builder from current `/mcp` route wiring.
  - Native STDIO and HTTP MCP must register the same tools.
  - Native STDIO uses disabled-auth actor semantics: `public-editor`.

- Run one process:
  - Create `Wiki` once.
  - Start HTTP server with `http.Server` so it can be gracefully shut down.
  - Run SDK STDIO transport on stdin/stdout.
  - On stdin close, STDIO error, SIGINT, or SIGTERM, close STDIO, stop HTTP, release lock, close `Wiki`.

- Protect shared data:
  - Keep current root/data validation.
  - Add exclusive lock at `<data-dir>/.leafwiki/leafwiki.lock`.
  - Startup fails clearly when another process owns the same data dir.

- Update `scripts/run-mcp.sh`:
  - Native combined mode is default.
  - Defaults: `--root-dir ./wiki`, `--data-dir ./.wiki`.
  - Add `--mode native|sidecar`; `sidecar` preserves the old server-plus-sidecar behavior.
  - Native mode rejects sidecar-only flags: `--mcp-stdio-bin`, `--endpoint`, `--api-key`, `--request-timeout`, `--shutdown-timeout`, `--max-frame-size`.

- Documentation:
  - Save this plan as `plans/native_mcp_stdio_combined.PLAN.md`.
  - Update `docs/mcp.md`, `scripts/README.md`, and `README.md`.
  - Include Codex config examples, defaults, disabled-auth v1 note, data-dir locking, stdout hygiene, and sidecar compatibility.

## Gherkin Test Suite

```gherkin
Feature: Native combined mode startup

  Scenario: Combined mode starts HTTP and STDIO in one process
    Given LeafWiki is started with "--mcp-stdio --disable-auth --host 127.0.0.1 --port <free-port> --data-dir <temp-data> --root-dir <temp-root>"
    When an MCP client initializes over STDIO
    Then initialization succeeds
    And "GET /api/health" over HTTP returns healthy
    And both transports use the same LeafWiki data directory

  Scenario: Combined mode does not require Streamable HTTP MCP
    Given LeafWiki is started with "--mcp-stdio --disable-auth" without "--enable-mcp"
    When an MCP client lists tools over STDIO
    Then the tool list is returned
    When an HTTP request is sent to "/mcp"
    Then the response is 404

  Scenario: Combined mode can also enable HTTP MCP explicitly
    Given LeafWiki is started with "--mcp-stdio --disable-auth --enable-mcp"
    When an MCP client lists tools over STDIO
    Then the tool list is returned
    When an MCP HTTP client connects to "/mcp"
    Then the HTTP MCP client also lists tools

  Scenario: Help documents native STDIO mode
    When "leafwiki --help" is run
    Then stdout contains "--mcp-stdio"
    And stdout contains "LEAFWIKI_MCP_STDIO"
    And no server log JSON is written to stdout

  Scenario: Environment enables native STDIO mode
    Given "LEAFWIKI_MCP_STDIO=true"
    And "LEAFWIKI_DISABLE_AUTH=true"
    When LeafWiki starts
    Then native STDIO mode is active

  Scenario: Combined mode rejects authenticated startup in v1
    Given LeafWiki is started with "--mcp-stdio --jwt-secret <secret> --admin-password <password>"
    When startup validation runs
    Then the process exits non-zero
    And stderr says native STDIO requires disabled auth in v1
    And stdout is empty

  Scenario: Combined mode rejects stdout logging
    Given LeafWiki is started with "--mcp-stdio --disable-auth --log-target stdout"
    When startup validation runs
    Then the process exits non-zero
    And stderr says stdout is reserved for MCP STDIO
    And stdout is empty

  Scenario: Root dir cannot contain default data dir
    Given LeafWiki is started with "--mcp-stdio --disable-auth --root-dir . --data-dir ./.wiki"
    When workspace validation runs
    Then startup fails
    And stderr explains that root dir must not contain data dir

  Scenario: Default project paths are accepted
    Given the current directory is a project root
    When "run-mcp.sh --dry-run" is run
    Then the planned command includes "--root-dir ./wiki"
    And the planned command includes "--data-dir ./.wiki"
```

```gherkin
Feature: STDIO protocol behavior in combined mode

  Scenario: STDIO stdout contains only JSON-RPC protocol frames
    Given LeafWiki is running in combined mode
    When an MCP client sends initialize and tools/list over STDIO
    Then every stdout line is valid JSON-RPC
    And stdout does not contain logs, banners, HTTP URLs, or shell prompts

  Scenario: Diagnostics are written away from stdout
    Given LeafWiki is running in combined mode with file logging
    When startup completes
    Then the HTTP URL is written to stderr
    And server logs are written to the configured log file
    And stdout remains empty until an MCP frame is sent

  Scenario: Malformed STDIO JSON fails as protocol error
    Given LeafWiki is running in combined mode
    When stdin receives a non-JSON line
    Then stdout contains a JSON-RPC parse error
    And the process remains able to handle a later valid initialize frame

  Scenario: Oversized STDIO frame is rejected
    Given LeafWiki is running in combined mode with a small max frame size if supported
    When stdin receives a JSON-RPC frame above the limit
    Then stdout contains a JSON-RPC error
    And HTTP remains healthy

  Scenario: Client closes stdin
    Given LeafWiki is running in combined mode
    When the MCP client closes stdin
    Then the STDIO session ends cleanly
    And the HTTP server stops
    And the process exits zero

  Scenario: SIGTERM shuts down cleanly
    Given LeafWiki is running in combined mode
    When the process receives SIGTERM
    Then HTTP shuts down
    And the data-dir lock is released
    And stdout contains no non-protocol text
```

```gherkin
Feature: Shared HTTP and STDIO state

  Scenario: STDIO-created page is visible in the HTTP UI
    Given LeafWiki is running in combined mode with root dir "<temp-root>"
    When an MCP STDIO client calls "create_page"
    And the browser opens the page URL over HTTP
    Then the page is visible in the UI
    And the markdown file exists under "<temp-root>"
    And no markdown file is created under "<data-dir>/root"

  Scenario: HTTP edit is visible through STDIO
    Given a page was created through MCP STDIO
    When the browser edits and saves the page over HTTP
    And the MCP STDIO client calls "get_page"
    Then the returned page content includes the HTTP edit

  Scenario: STDIO and HTTP share revision behavior
    Given revisions are enabled
    When STDIO creates a page
    And HTTP edits the page
    Then STDIO "list_revisions" includes both changes

  Scenario: STDIO and HTTP share search indexes
    Given LeafWiki is running in combined mode
    When STDIO creates a page containing a unique search term
    Then HTTP search finds the page
    When HTTP edits the page to add another unique term
    Then STDIO search finds the updated page

  Scenario: Current STDIO actor is disabled-auth public editor
    Given LeafWiki is running in combined mode
    When STDIO calls "get_current_user"
    Then the username is "public-editor"
    And the role is "editor"
```

```gherkin
Feature: Data directory locking

  Scenario: Second process with same data dir is rejected
    Given one LeafWiki combined process owns "<data-dir>"
    When a second LeafWiki process starts with the same "--data-dir <data-dir>"
    Then the second process exits non-zero
    And stderr says the data directory is already in use

  Scenario: Different data dirs can run simultaneously
    Given one LeafWiki combined process owns "<data-dir-a>"
    When another LeafWiki combined process starts with "<data-dir-b>" and a different port
    Then both processes start successfully

  Scenario: Lock is released after clean exit
    Given LeafWiki combined mode starts with "<data-dir>"
    When the MCP client closes stdin and the process exits
    Then starting LeafWiki again with "<data-dir>" succeeds

  Scenario: Lock is released after SIGTERM
    Given LeafWiki combined mode starts with "<data-dir>"
    When the process receives SIGTERM
    Then starting LeafWiki again with "<data-dir>" succeeds

  Scenario: Lock directory is created automatically
    Given "<data-dir>/.leafwiki" does not exist
    When LeafWiki starts
    Then the lock file parent directory is created
    And the process starts successfully
```

```gherkin
Feature: run-mcp native wrapper

  Scenario: Native mode is the default
    When "scripts/run-mcp.sh --dry-run" is run
    Then stderr shows one "leafwiki" command
    And the command includes "--mcp-stdio"
    And the command includes "--disable-auth=true"
    And the command does not include "leafwiki-mcp-stdio"

  Scenario: Sidecar mode preserves old wrapper behavior
    When "scripts/run-mcp.sh --mode sidecar --dry-run" is run
    Then stderr shows a LeafWiki server command
    And stderr shows a "leafwiki-mcp-stdio" command
    And the sidecar command includes "--endpoint"

  Scenario: Native mode rejects sidecar endpoint
    When "scripts/run-mcp.sh --mode native --endpoint http://127.0.0.1:8080/mcp --dry-run" is run
    Then the wrapper exits non-zero
    And stderr says "--endpoint requires --mode sidecar"

  Scenario: Native mode rejects API key
    When "scripts/run-mcp.sh --mode native --api-key lwk_fake --dry-run" is run
    Then the wrapper exits non-zero
    And stderr says "--api-key requires --mode sidecar"

  Scenario: Native mode allows extra server args
    When "scripts/run-mcp.sh --mode native --server-arg --enable-revision --dry-run" is run
    Then the planned LeafWiki command includes "--enable-revision"

  Scenario: Native mode supports custom paths
    When "scripts/run-mcp.sh --mode native --data-dir /tmp/project-state --root-dir /tmp/project-wiki --dry-run" is run
    Then the planned command includes "--data-dir /tmp/project-state"
    And the planned command includes "--root-dir /tmp/project-wiki"

  Scenario: Native wrapper keeps stdout for protocol
    Given "run-mcp.sh" starts a fake native leafwiki process
    When the fake process writes protocol output to stdout and diagnostics to stderr
    Then wrapper stdout contains only the protocol output
    And wrapper stderr contains diagnostics

  Scenario: Native wrapper remains compatible with macOS Bash 3.2
    When "bash -n scripts/run-mcp.sh" is run
    And "scripts/test-run-mcp.sh" is run under Bash 3.2
    Then both commands pass
```

```gherkin
Feature: Documentation and configuration

  Scenario: MCP docs explain native per-project setup
    Given "docs/mcp.md" is opened
    Then it contains a combined STDIO plus HTTP section
    And it documents "codex://threads/019e88d1-e02b-7b93-bea6-13473fb08200"
    And it shows a Codex MCP config using "run-mcp.sh"

  Scenario: Docs explain path safety
    Given "docs/mcp.md" is opened
    Then it documents "--root-dir ./wiki"
    And it documents "--data-dir ./.wiki"
    And it explains that "--root-dir . --data-dir ./.wiki" is invalid

  Scenario: Docs keep sidecar compatibility clear
    Given "docs/mcp.md" is opened
    Then it says sidecar mode is still available
    And it explains when to use "--mode sidecar"

  Scenario: README contains concise project-local MCP setup
    Given "README.md" is opened
    Then it links to "docs/mcp.md"
    And it includes a short "run-mcp.sh" project-local example
```

## Implementation Tasks

- Task 1: Add native mode CLI parsing and validation.
  - Modify `cmd/leafwiki/main.go`.
  - Tests first in `cmd/leafwiki/main_test.go`.
  - Cover help, env resolution, disabled-auth requirement, stdout logging rejection, and root/data containment.

- Task 2: Extract reusable runtime helpers.
  - Add a small `runServer(ctx, router, addr)` helper for graceful HTTP lifecycle.
  - Keep normal server mode behavior unchanged.
  - Add process tests proving HTTP startup and shutdown.

- Task 3: Expose native STDIO server wiring.
  - Refactor MCP server creation so HTTP `/mcp` and native STDIO share one builder.
  - Add `RunStdio(ctx, wiki, opts, stdin, stdout, stderr)` or equivalent.
  - Use SDK `IOTransport` in tests and `StdioTransport` or `IOTransport` in production.
  - Prove tool-list parity and `public-editor` actor behavior.

- Task 4: Add data-dir locking.
  - Add `internal/wiki/lock` or `internal/locking`.
  - Use `golang.org/x/sys/unix.Flock` on Unix and a Windows-safe fallback if needed.
  - Acquire after data-dir resolution and before `wiki.NewWiki`.
  - Release on process exit.
  - Tests cover second-process rejection and release after exit.

- Task 5: Update `scripts/run-mcp.sh`.
  - Default native mode.
  - Keep `--mode sidecar`.
  - Update defaults to `./.wiki` and `./wiki`.
  - Preserve old sidecar path in `--mode sidecar`.
  - Add shell tests for dry-run, rejected flags, stdout hygiene, and Bash compatibility.

- Task 6: Update E2E runner and tests.
  - Add `E2E_MCP_STDIO_NATIVE=1`.
  - In native mode, build/run one `leafwiki` command as `E2E_MCP_STDIO_COMMAND`.
  - Reuse disabled-auth STDIO tests.
  - Keep API-key STDIO tests sidecar-only.
  - Add at least one native-only lifecycle test proving stdin close stops HTTP.

- Task 7: Update docs.
  - Save this plan to `plans/native_mcp_stdio_combined.PLAN.md`.
  - Update `docs/mcp.md`, `scripts/README.md`, and `README.md`.
  - Include all defaults, examples, limitations, troubleshooting, and verification commands.

## Verification Commands

```bash
rtk go test ./cmd/leafwiki ./internal/wiki ./internal/wiki/mcp ./internal/http
rtk go test ./...
rtk bash -n scripts/run-mcp.sh scripts/test-run-mcp.sh
rtk bash scripts/test-run-mcp.sh
rtk npm --prefix e2e run lint
rtk npm --prefix e2e run format:check
rtk npm --prefix ui/leafwiki-ui run lint
rtk npm --prefix ui/leafwiki-ui run build
rtk env E2E_RUN_MODE=local E2E_ENABLE_MCP_LOCAL=1 E2E_MCP_CLIENT_TRANSPORT=stdio E2E_MCP_STDIO_NATIVE=1 ./e2e/run.sh tests/mcp-stdio-disable-auth.spec.ts
rtk env E2E_RUN_MODE=local E2E_ENABLE_MCP_LOCAL=1 E2E_ENABLE_SEPARATE_ROOT_DIR=1 E2E_MCP_CLIENT_TRANSPORT=stdio E2E_MCP_STDIO_NATIVE=1 ./e2e/run.sh tests/mcp-stdio-disable-auth.spec.ts --grep "mcp stdio seeds"
```

## Definition Of Done

- [ ] `leafwiki --help` documents `--mcp-stdio` and `LEAFWIKI_MCP_STDIO`.
- [ ] `leafwiki --mcp-stdio --disable-auth` starts one process with HTTP UI and MCP STDIO.
- [ ] STDIO and HTTP share the same `Wiki` instance and observe each other’s page changes.
- [ ] Native STDIO works without `--enable-mcp`.
- [ ] HTTP `/mcp` remains disabled unless `--enable-mcp` is explicitly set.
- [ ] Native STDIO v1 rejects authenticated startup with a clear error.
- [ ] Native STDIO rejects `--log-target stdout`.
- [ ] stdout is protocol-only in all native STDIO tests.
- [ ] Closing STDIO stdin stops HTTP and exits the process cleanly.
- [ ] SIGTERM/SIGINT shut down HTTP and release resources.
- [ ] Data-dir locking prevents two active LeafWiki processes using the same data dir.
- [ ] Lock is released after clean exit and SIGTERM.
- [ ] `scripts/run-mcp.sh` defaults to native combined mode.
- [ ] `scripts/run-mcp.sh --mode sidecar` preserves old sidecar behavior.
- [ ] `run-mcp.sh` defaults are `--root-dir ./wiki` and `--data-dir ./.wiki`.
- [ ] `--root-dir . --data-dir ./.wiki` fails as invalid.
- [ ] Go unit, process, shell, and E2E tests cover happy paths, errors, edge cases, and lifecycle.
- [ ] Docs include the thread link, native setup, sidecar compatibility, path safety, stdout hygiene, troubleshooting, and verification commands.
- [ ] All verification commands pass.
- [ ] `git status --short` contains only intentional implementation, tests, docs, and plan files.

## Assumptions

- V1 native combined mode is disabled-auth only.
- Project content default is `./wiki`; app state default is `./.wiki`.
- Sidecar support remains available for clients that need to bridge to an already-running HTTP MCP server.
- No OAuth/API-key native STDIO support is implemented in this slice.
- No project-root markdown mode (`--root-dir .`) is added in this slice.
