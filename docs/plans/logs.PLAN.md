<!-- leafwiki
version: 1
page:
  id: 6A2auS-Dg
  title: LeafWiki Logging Targets Implementation Plan
  created_at: "2026-06-15T05:44:44.855530421Z"
  updated_at: "2026-06-15T05:44:44.855530421Z"
  creator_id: system
  last_author_id: system
-->

# LeafWiki Logging Targets Implementation Plan

**Thread:** [codex://threads/019e82ba-6a29-7502-a63c-c68792356356](codex://threads/019e82ba-6a29-7502-a63c-c68792356356)
**Goal:** Decouple LeafWiki server logs from terminal output by defaulting logs to a file in the data directory, while supporting explicit `stderr`/`stdout` targets and preserving a future path for `system` targets.

## Summary

Add a small `internal/logging` package and wire `cmd/leafwiki` through it. Support exactly `file`, `stderr`, and `stdout` in v1. Default server logs to `<data-dir>/.leafwiki/logs/leafwiki.log`. Do not implement in-process rotation; document external rotation and recommend `stderr` for Docker/systemd-managed logs.

Keep stdout intentional: `--help`, unknown-command usage, and `reset-admin-password` may write user-facing text to stdout; normal server logs must not use stdout unless `--log-target stdout` is explicitly selected.

## Key Changes

- Add public config:
  - `--log-target file|stderr|stdout`, `LEAFWIKI_LOG_TARGET`, default `file`.
  - `--log-file <path>`, `LEAFWIKI_LOG_FILE`, default `<normalized-data-dir>/.leafwiki/logs/leafwiki.log`.
  - Preserve `LEAFWIKI_LOG_LEVEL=debug|info|warn|error`.
- Add `internal/logging` with target parsing, default path resolution, sink opening, and `slog` setup.
- File target:
  - relative `--log-file` resolves under `workspace.DataDir`;
  - absolute `--log-file` is used as-is;
  - parent dirs `0755`, new log file `0600`, append JSON lines;
  - startup fails with stderr error if log file cannot be opened.
- `stderr`/`stdout` targets:
  - write JSON logs to selected stream;
  - reject explicit `--log-file` unless target is `file`.
- Startup:
  - keep a bootstrap stderr logger until CLI/env/workspace resolution is complete;
  - install final logger before warnings, directory creation logs, `wiki.NewWiki`, and router construction;
  - call `slog.SetDefault(logger)` so existing stdlib `log.Print*` calls route through the chosen sink.
- Scripts/docs:
  - `scripts/run-mcp.sh` should pass `--log-target stderr` to preserve existing `--server-log` wrapper capture and MCP stdout hygiene.
  - `install.sh`, `.env.example`, `README.md`, `docs/logging.md`, `docs/mcp.md`, and `scripts/README.md` must document the new behavior and no-rotation policy.
- References:
  - Go `log/slog`: `slog.NewJSONHandler`, `slog.HandlerOptions`, `slog.SetDefault`.
  - Existing Gin bridge: `internal/http/router.go` already routes Gin output through `slog.Default()`.

## Gherkin Test Suite

### CLI And Config Resolution

```gherkin
Feature: Logging configuration resolution

  Scenario: Default server logging uses a file under the data directory
    Given LeafWiki is started with "--disable-auth" and "--data-dir <temp-data>"
    And no log target or log file is configured
    When the server starts successfully
    Then "<temp-data>/.leafwiki/logs/leafwiki.log" exists
    And the log file contains a JSON log entry with message "Starting LeafWiki"
    And stdout does not contain "Starting LeafWiki"

  Scenario: CLI log target overrides environment log target
    Given LEAFWIKI_LOG_TARGET is "file"
    When LeafWiki is started with "--log-target stderr"
    Then logs are written to stderr
    And the default data-dir log file is not created

  Scenario: Environment log target is used when CLI flag is absent
    Given LEAFWIKI_LOG_TARGET is "stderr"
    When LeafWiki is started without "--log-target"
    Then logs are written to stderr

  Scenario: Invalid log target fails startup
    Given LEAFWIKI_LOG_TARGET is "syslog"
    When LeafWiki startup begins
    Then the process exits non-zero
    And stderr contains "invalid log target"
    And stdout is empty

  Scenario: Explicit log file is rejected for stderr target
    When LeafWiki is started with "--log-target stderr --log-file custom.log"
    Then the process exits non-zero
    And stderr contains "--log-file requires --log-target file"

  Scenario: Relative log file resolves under data directory
    Given LeafWiki is started with "--data-dir <temp-data> --log-file logs/custom.log"
    When the server starts successfully
    Then "<temp-data>/logs/custom.log" exists
    And "<temp-data>/.leafwiki/logs/leafwiki.log" does not exist

  Scenario: Absolute log file is used as-is
    Given an absolute path "<temp-dir>/leafwiki.log"
    When LeafWiki is started with "--log-file <temp-dir>/leafwiki.log"
    Then "<temp-dir>/leafwiki.log" contains JSON logs

  Scenario: Unwritable log file fails visibly
    Given "<unwritable-dir>" cannot be written by the process
    When LeafWiki is started with "--log-file <unwritable-dir>/leafwiki.log"
    Then the process exits non-zero
    And stderr contains "failed to open log file"
```

### Stdout And User-Facing Output

```gherkin
Feature: Stdout safety

  Scenario: Help output remains on stdout
    When LeafWiki is run with "--help"
    Then stdout contains "Usage:"
    And stdout contains "--log-target"
    And stderr does not contain server log JSON

  Scenario: Unknown command remains user-facing
    When LeafWiki is run with "unknown-command"
    Then stdout contains "Unknown command"
    And stdout contains "Usage:"
    And no server log file is created

  Scenario: Reset admin password keeps credentials on stdout
    Given an initialized data directory
    When LeafWiki is run with "reset-admin-password"
    Then stdout contains "Admin password reset successfully"
    And stdout contains "New password"
    And server logs are not interleaved into stdout

  Scenario: Explicit stdout target writes server logs to stdout
    When LeafWiki is started with "--log-target stdout"
    Then stdout contains JSON log entries
    And this behavior only occurs because stdout was explicitly selected
```

### File Logging

```gherkin
Feature: File log target

  Scenario: Log directory is created automatically
    Given "<temp-data>/.leafwiki/logs" does not exist
    When LeafWiki starts with default logging
    Then the directory exists
    And the log file exists

  Scenario: Existing log file is appended
    Given the configured log file already contains "previous line"
    When LeafWiki starts and stops
    Then the file still contains "previous line"
    And the file also contains a new "Starting LeafWiki" entry

  Scenario: Logs are structured JSON
    Given LeafWiki starts with file logging
    When a startup log is written
    Then each log line is valid JSON
    And each log line includes "time", "level", "msg", and "source"

  Scenario: Stdlib log calls are bridged into selected sink
    Given the final logger is configured with a test sink
    When code calls "log.Print"
    Then the test sink receives the log entry
```

### HTTP And Gin Logging

```gherkin
Feature: HTTP request logging

  Scenario: Request logs go to configured sink
    Given LeafWiki is running with file logging
    When a request is made to "/api/health"
    Then the log file contains an "http request" entry
    And the entry includes method, path, status, latency, and ip

  Scenario: Request logs can still be disabled
    Given LeafWiki is running with "--disable-request-log"
    When a request is made to "/api/health"
    Then the configured log sink does not contain an "http request" entry

  Scenario: Gin recovery logs use configured sink
    Given a test route panics
    When the route is requested
    Then the recovery log is written to the configured sink
    And stdout is not used unless "--log-target stdout" is selected
```

### MCP Wrapper And Protocol Hygiene

```gherkin
Feature: MCP wrapper logging

  Scenario: run-mcp reserves stdout for the stdio proxy
    Given "scripts/run-mcp.sh" starts a fake LeafWiki server and fake stdio proxy
    When JSON-RPC is sent to wrapper stdin
    Then wrapper stdout contains only fake stdio proxy stdout
    And LeafWiki server logs do not appear on wrapper stdout

  Scenario: run-mcp captures server logs in server-log
    Given "scripts/run-mcp.sh --server-log <server-log>"
    When the wrapper starts LeafWiki
    Then the LeafWiki command includes "--log-target stderr"
    And "<server-log>" captures LeafWiki stderr/stdout

  Scenario: run-mcp dry-run shows logging target
    When "scripts/run-mcp.sh --dry-run" is executed
    Then stderr output includes "--log-target stderr"
    And no log files are created
```

### Installer And Documentation

```gherkin
Feature: Installer logging configuration

  Scenario: Interactive installer writes logging defaults
    Given interactive install validation mode
    When the user accepts defaults
    Then the generated env file contains "LEAFWIKI_LOG_TARGET=\"file\""
    And the generated env file contains "LEAFWIKI_LOG_FILE=\"\""

  Scenario: Non-interactive installer accepts valid logging env
    Given an env file with "LEAFWIKI_LOG_TARGET=stderr"
    When install validation runs
    Then validation succeeds

  Scenario: Non-interactive installer rejects invalid logging env
    Given an env file with "LEAFWIKI_LOG_TARGET=system"
    When install validation runs
    Then validation fails
    And output contains "invalid LEAFWIKI_LOG_TARGET"
```

## Required Test Files And Commands

- Add/extend Go tests:
  - `internal/logging/logging_test.go`
  - `cmd/leafwiki/main_test.go`
  - focused HTTP logging tests in `internal/http/router_test.go` only if current coverage cannot observe sink behavior from process tests.
- Add/extend shell tests:
  - `scripts/test-run-mcp.sh`
  - `scripts/test-install.sh`
- Verification commands:

```bash
rtk go test ./cmd/leafwiki ./internal/logging ./internal/http
rtk go test ./...
rtk bash -n install.sh scripts/run-mcp.sh
rtk bash scripts/test-install.sh
rtk bash scripts/test-run-mcp.sh
rtk npm --prefix e2e run lint
rtk npm --prefix e2e run format:check
```

## Definition Of Done

- [ ] `leafwiki --help` documents `--log-target`, `--log-file`, `LEAFWIKI_LOG_TARGET`, and `LEAFWIKI_LOG_FILE`.
- [ ] Default server startup creates and writes JSON logs to `<data-dir>/.leafwiki/logs/leafwiki.log`.
- [ ] Normal server logs do not appear on stdout unless `--log-target stdout` is explicitly selected.
- [ ] `stderr` and `stdout` targets work and respect `LEAFWIKI_LOG_LEVEL`.
- [ ] Invalid target and invalid target/file combinations fail startup with a clear stderr error.
- [ ] Existing stdlib `log.Print*`, Gin logs, request logs, and app `slog` logs all flow through the configured sink.
- [ ] `scripts/run-mcp.sh` preserves MCP stdout hygiene and still supports `--server-log`.
- [ ] Installer and `.env.example` expose logging settings.
- [ ] README, `docs/logging.md`, `docs/mcp.md`, and `scripts/README.md` document defaults, examples, no in-process rotation, and external rotation guidance.
- [ ] All Gherkin scenarios above are covered by automated tests or explicitly mapped to a verification command.
- [ ] All verification commands listed above pass.
- [ ] `git status --short` shows only intentional changes for this feature.

## Assumptions

- No native `system`, journald, syslog, macOS Unified Logging, or Windows Event Log target in this slice.
- No in-process log rotation, retention cleanup, SIGHUP reopen, compression, or UI log viewer.
- Default target is `file`.
- `run-mcp.sh` intentionally overrides the server to `stderr` to keep its existing wrapper log capture behavior.
- Future `system` support will be added as a new `internal/logging` target implementation.
