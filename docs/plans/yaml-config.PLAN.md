# YAML Configuration File Support Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` or `superpowers:executing-plans`. Use TDD first. Suggested workers: CLI/config parser, wrapper/docs, Go process tests, E2E.

**Goal:** Add explicit `leafwiki --config <path>` support for a flat YAML config file whose keys mirror public CLI flag names without `--`.

**Architecture:** Treat YAML values as supplied CLI flag values by applying them into the existing `cliFlags` pointers and `visited` map before current resolver logic runs. Keep the existing startup, validation, daemon matching, and env/default fallback flow intact.

**Tech Stack:** Go standard `flag`, `gopkg.in/yaml.v3`, Bash wrapper tests, Playwright E2E.

**Reference Thread:** `codex://threads/019eb504-6cf3-7803-b033-bee91c3b028b`

---

## Summary

Add config-file startup with these semantics:

- `--config <path>` is mutually exclusive with normal CLI flags.
- YAML keys mirror public CLI flags exactly.
- YAML behaves like supplied CLI flags: `YAML > env > defaults`.
- Omitted YAML keys continue to use env/defaults.
- Secrets are allowed in YAML.
- Unknown and duplicate YAML keys fail.
- Explicit `""`, `false`, and `0` override env/defaults.
- Obvious scalar coercion is allowed.
- Relative paths behave like current CLI paths from process cwd.
- Config file path does not affect daemon identity or config matching.
- `--config <path> agent-hook <provider>` preserves hook fail-open behavior.

## Key Changes

- In `cmd/leafwiki/main.go`, add `config *string` to `cliFlags`, register `--config`, document it in help, and add `config` to `valueTakingFlagNames()`.
- Add YAML helpers near the existing resolver code:
  - Load root YAML as a mapping using `yaml.Node`.
  - Detect duplicate keys manually.
  - Reject unknown keys, hidden compatibility keys, internal-only keys, `null`, mappings, and sequences.
  - Apply valid YAML values into existing flag pointers and mark those keys as visited.
- Valid YAML keys are current public CLI flags only, excluding `config`, `internal-project-daemon`, `enable-mcp`, and `mcp-stdio`.
- Preserve existing validation after resolution: workspace, logging, MCP, trusted proxies, upload size, revision/workspace-sync exclusivity.
- Do not add config path to `leafwikiRuntimeConfig` or `projectdaemon.Config`.
- Do not add `log-level` YAML support in v1 because there is no public `--log-level`.
- Update `scripts/run.sh` with passthrough config mode:
  - `scripts/run.sh mcp --config ./leafwiki.yml` invokes `leafwiki --config ./leafwiki.yml`.
  - `scripts/run.sh agent-hook codex --config ./leafwiki.yml` invokes `leafwiki --config ./leafwiki.yml agent-hook codex`.
  - Wrapper defaults are not appended in config mode.
  - Normal wrapper config flags are rejected with `--config`.
- Update `README.md`, `docs/mcp.md`, `docs/agent-hooks.md`, and `scripts/README.md`.

## Gherkin Test Suite

```gherkin
Feature: YAML config resolution

  Scenario: YAML value overrides environment
    Given LEAFWIKI_PORT is "9999"
    And config.yml contains "port: 8088"
    When LeafWiki starts with "--config config.yml"
    Then the resolved port is "8088"

  Scenario: Environment fills omitted YAML key
    Given LEAFWIKI_PORT is "9999"
    And config.yml omits "port"
    When LeafWiki starts with "--config config.yml"
    Then the resolved port is "9999"

  Scenario: Defaults fill omitted YAML and env keys
    Given LEAFWIKI_PORT is unset
    And config.yml omits "port"
    When LeafWiki starts with "--config config.yml"
    Then the resolved port is "8080"

  Scenario: Explicit empty string overrides env
    Given LEAFWIKI_BASE_PATH is "/wiki"
    And config.yml contains "base-path: ""
    When LeafWiki starts with "--config config.yml"
    Then the resolved base path is ""

  Scenario: Explicit false overrides env
    Given LEAFWIKI_PUBLIC_ACCESS is "true"
    And config.yml contains "public-access: false"
    When LeafWiki resolves startup config
    Then public access is false unless disable-auth later forces it true

  Scenario: Explicit zero overrides env
    Given LEAFWIKI_MAX_REVISION_HISTORY is "100"
    And config.yml contains "max-revision-history: 0"
    When LeafWiki resolves startup config
    Then max revision history is 0

  Scenario: Unknown YAML key fails
    Given config.yml contains "unknown-option: true"
    When LeafWiki starts with "--config config.yml"
    Then startup fails
    And stderr names "unknown-option"

  Scenario: Duplicate YAML key fails
    Given config.yml contains two "port" keys
    When LeafWiki starts with "--config config.yml"
    Then startup fails
    And stderr mentions duplicate key "port"

  Scenario: Non-scalar YAML value fails
    Given config.yml contains "trusted-proxy-ips: [127.0.0.1]"
    When LeafWiki starts with "--config config.yml"
    Then startup fails
    And stderr explains scalar values are required

  Scenario: Config and normal CLI flags are mutually exclusive
    Given config.yml is valid
    When LeafWiki starts with "--config config.yml --port 8081"
    Then startup fails
    And stderr says "--config cannot be combined with --port"

  Scenario: Config path does not affect daemon identity
    Given two config files at different paths have equivalent effective values
    When the first startup creates the project daemon
    And the second startup uses the other config path
    Then the second startup attaches without config mismatch

  Scenario: Config works with reset-admin-password
    Given config.yml contains a data-dir with an existing admin user
    When LeafWiki runs "--config config.yml reset-admin-password"
    Then the password reset command uses that data-dir

  Scenario: Agent hook fail-open is preserved with config
    Given config.yml contains an invalid workspace
    When LeafWiki runs "--config config.yml agent-hook codex"
    Then the process exits 0
    And stdout contains the provider allow response
    And stderr does not leak hook payload secrets

  Scenario: Config path value is not mistaken for an agent-hook command
    When LeafWiki runs "--config agent-hook --not-a-real-flag"
    Then "agent-hook" is treated as the config path
    And the invalid flag behavior is not hook fail-open
```

```gherkin
Feature: Wrapper config mode

  Scenario: Wrapper mcp dry-run passes only config
    When "scripts/run.sh mcp --dry-run --config ./leafwiki.yml" runs
    Then the planned command contains "--config ./leafwiki.yml"
    And it does not contain generated "--data-dir", "--root-dir", "--disable-auth", or "--enable-workspace-sync"

  Scenario: Wrapper rejects config mixed with normal wrapper options
    When "scripts/run.sh mcp --config ./leafwiki.yml --root-dir ./wiki" runs
    Then it fails
    And stderr says "--config cannot be combined with --root-dir"

  Scenario: Wrapper agent hook uses config
    When "scripts/run.sh agent-hook codex --dry-run --config ./leafwiki.yml" runs
    Then the planned command contains "--config ./leafwiki.yml agent-hook codex"
```

```gherkin
Feature: E2E config startup

  Scenario: Local HTTP server starts from YAML config
    Given E2E_RUN_MODE is local
    And E2E_USE_CONFIG_FILE is 1
    When the E2E runner starts LeafWiki
    Then "/api/health" returns 200
    And the browser can log in with configured auth

  Scenario: Native STDIO MCP starts from YAML config
    Given E2E_RUN_MODE is local
    And E2E_MCP_CLIENT_TRANSPORT is stdio
    And E2E_USE_CONFIG_FILE is 1
    When the MCP stdio smoke test creates a page
    Then the UI can read the page
    And MCP can read UI edits back
```

## Test Plan

- Go unit/process tests in `cmd/leafwiki/main_test.go`:
  - YAML/env/default precedence.
  - explicit empty/false/zero.
  - scalar coercion.
  - unknown key, duplicate key, non-scalar value.
  - config/CLI mutual exclusion.
  - hidden/internal key rejection.
  - daemon identity ignores config path.
  - `reset-admin-password` with config.
  - `agent-hook` fail-open with config.
- Script tests in `scripts/test-run.sh`:
  - help includes `--config`.
  - mcp config dry-run omits generated defaults.
  - agent-hook config dry-run preserves command shape.
  - config mixed with normal wrapper options fails.
- E2E tests:
  - local HTTP health/login via config file.
  - local native STDIO MCP smoke via config file.

Verification commands:

```bash
rtk go test ./cmd/leafwiki
rtk go test ./internal/projectdaemon
rtk ./scripts/test-run.sh
rtk make test
rtk env E2E_RUN_MODE=local E2E_USE_CONFIG_FILE=1 ./e2e/run.sh tests/health.spec.ts
rtk env E2E_RUN_MODE=local E2E_ENABLE_MCP_LOCAL=1 E2E_MCP_CLIENT_TRANSPORT=stdio E2E_USE_CONFIG_FILE=1 ./e2e/run.sh tests/mcp-stdio-disable-auth.spec.ts --grep "mcp stdio seeds"
rtk git diff --check
```

## Definition of Done

- [ ] `leafwiki --help` documents `--config`.
- [ ] YAML supports every public current CLI flag except `--config` itself.
- [ ] YAML rejects unknown keys, duplicate keys, non-scalar values, hidden compatibility keys, and internal-only keys.
- [ ] YAML values override env; omitted YAML values use existing env/default behavior.
- [ ] Config mode rejects normal CLI flag mixing.
- [ ] Agent hook fail-open behavior is covered and still works.
- [ ] Reset password with config is covered.
- [ ] Wrapper `scripts/run.sh` supports config mode without appending defaults.
- [ ] README, MCP docs, agent-hook docs, and scripts docs include accurate examples and precedence rules.
- [ ] Go unit tests, script tests, and local HTTP plus local STDIO E2E config-file paths pass.
- [ ] Config path is not included in daemon identity or descriptor matching.
- [ ] Plan or implementation docs include `codex://threads/019eb504-6cf3-7803-b033-bee91c3b028b`.
