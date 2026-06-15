# Agent Presence Hooks Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: use `superpowers:subagent-driven-development` or `superpowers:executing-plans`. Use TDD first. Save this as `plans/agent_hooks.PLAN.md`. Thread context: `codex://threads/019ea3a5-4240-7b80-b5f0-76c459294370`.

## Summary

Implement v1 agent hook presence only. No web heartbeat, no public `wiki_get_context.activeSessions` surface yet, no spoofing hardening beyond hashing provider session IDs.

Hooks are passive observers. Every hook path must exit `0`, return the provider allow response, and never block agent work, including malformed JSON, daemon startup failure, control API failure, unsupported provider, panic, timeout, or missing fields.

Primary references:
- Current seams: `cmd/leafwiki/main.go`, `internal/projectdaemon/*`, `scripts/run-mcp.sh`, `e2e/run.sh`.
- Local `agent-gate` references: `references/agent-gate/internal/hook/{codex,claude,cursor,payload_*}.go`, `references/agent-gate/HOOKS.md`, `references/agent-gate/docs/hook-schemas.md`.
- External docs: [Codex hooks](https://developers.openai.com/codex/hooks), [Claude Code hooks](https://code.claude.com/docs/en/hooks), [Cursor hooks](https://cursor.com/docs/hooks).

Suggested workers:
- Parser/CLI worker: `internal/agenthooks`, `leafwiki agent-hook`.
- Daemon worker: `internal/projectdaemon` presence registry/control/lifetime.
- Wrapper/docs worker: `scripts/run.sh`, installer/docs.
- Test/E2E worker: Go, Bash, Playwright coverage.
- Review worker: privacy, fail-open, stdout hygiene, repo fit.

## Implementation

- Add `internal/agenthooks` with provider constants, minimal payload envelopes, normalization, hashing, and allow responses. Adapt only small transport/parser pieces from `agent-gate`; do not copy rules, audit, daemon, gRPC, provider detection, or tool-input extraction.
- Normalize to:
  ```go
  type Event struct {
      Provider string
      SessionIDHash string
      EventName string
      Model string
      Source string
      ToolName string
      IsMCPTool bool
      SubagentDelta int
      EndsSession bool
      SeenAt time.Time
  }
  ```
  Hash as `sha256(provider + "\x00" + rawSessionID)`, rendered `sha256:<hex>`.
- Never store raw session IDs, prompts, transcript paths, cwd, tool input/output, user email, API keys, JWT/admin secrets, or control tokens.
- Provider behavior:
  - Codex: `SessionStart`, `PreToolUse`, `PermissionRequest`, `PostToolUse`, `UserPromptSubmit`, `Stop`, plus `SubagentStart/Stop` if present.
  - Claude/Cursor: session start/end, tool events, prompt/stop events, subagent start/stop.
  - Cursor session key: prefer `session_id`, fallback to `conversation_id`, otherwise ignore.
  - Subagents are not first-class sessions; they update parent `lastSeenAt` and `activeSubagents`, never below zero.
- Add `internal/projectdaemon/agent_presence.go` with in-memory `AgentPresenceRegistry`: `Record`, `List`, `Count`, `PruneExpired`, `RunExpiryLoop`, injectable clock. Default TTL is `projectdaemon.DefaultIdleTimeout` (`10m`).
- Extend private control only:
  - `POST /agent-presence/events` records normalized presence.
  - `GET /agent-presence` returns sanitized sessions for tests/future context tooling.
  - Both require `X-LeafWiki-Daemon-Token` before routing.
- Change daemon lifetime:
  - Existing `SessionRegistry` remains foreground-process/MCP handle tracking.
  - Owner activity count is `sessions.Count() + agentPresence.Count()`.
  - First-activity startup grace and idle shutdown must consider both.
  - Presence must not enter `projectdaemon.Config`, descriptor, config hash, or `compareProjectDaemonConfigForRequest`.
- Add `leafwiki agent-hook <codex|claude|cursor|unknown> [flags]`.
  - Dispatch before normal positional command handling.
  - Reuse existing flag resolution, `daemonRequestConfigForRuntime`, `attachOrStartProjectDaemon`, and `projectdaemon.Client`.
  - Read stdin, normalize, start/attach daemon, post to control, write allow, exit `0`.
  - Any error returns provider allow: Codex `{}` newline, Claude `{}` newline, Cursor `{"permission":"allow"}` newline, unknown empty stdout.
- Rename wrapper with no compatibility shim:
  - `scripts/run-mcp.sh` -> `scripts/run.sh`
  - `scripts/test-run-mcp.sh` -> `scripts/test-run.sh`
  - `scripts/run.sh mcp [options]` keeps existing MCP STDIO behavior.
  - `scripts/run.sh agent-hook <provider> [options]` invokes `leafwiki agent-hook <provider>` and forwards stdin.
  - Shared defaults stay `--data-dir ./.wiki`, `--root-dir ./wiki`, `--host 127.0.0.1`, `--port 8080`, `--enable-workspace-sync`, `--log-target file`, disabled auth unless API-key mode is configured.
- Documentation:
  - Add `docs/agent-hooks.md` with the thread link, privacy contract, fail-open contract, wrapper commands, and user-managed Codex/Claude/Cursor examples.
  - Update `docs/mcp.md`, `docs/workspace-sync.md`, `scripts/README.md`, installer help/tests, and current setup examples from `run-mcp.sh` to `run.sh mcp`.
  - Leave historical plan files unchanged.

## Gherkin Test Suite

```gherkin
Feature: Provider payload normalization
  Scenario Outline: Supported provider events normalize safely
    Given a <provider> hook payload for <event>
    When the payload is normalized
    Then provider, hashed session id, event name, model/source/tool metadata are captured when available
    And raw session id, prompt, transcript path, cwd, tool input/output, and user email are absent
    Examples:
      | provider | event |
      | codex | SessionStart |
      | codex | PreToolUse |
      | codex | PermissionRequest |
      | codex | PostToolUse |
      | codex | UserPromptSubmit |
      | codex | Stop |
      | claude | SessionStart |
      | claude | SessionEnd |
      | claude | PreToolUse |
      | claude | PostToolUse |
      | claude | UserPromptSubmit |
      | claude | Stop |
      | claude | SubagentStart |
      | claude | SubagentStop |
      | cursor | sessionStart |
      | cursor | sessionEnd |
      | cursor | preToolUse |
      | cursor | postToolUse |
      | cursor | beforeMCPExecution |
      | cursor | afterMCPExecution |
      | cursor | beforeSubmitPrompt |
      | cursor | stop |
      | cursor | subagentStart |
      | cursor | subagentStop |

  Scenario: Unknown and incomplete payloads fail open
    Given malformed JSON, missing hook_event_name, null session_id, missing session_id, or unknown event name
    When the hook command runs
    Then it exits 0 with the provider allow response
    And no presence session is recorded

  Scenario: Cursor conversation fallback is used
    Given a Cursor payload with no session_id and a conversation_id
    When it is normalized
    Then the conversation_id is hashed as the session key
    And the raw conversation_id is not stored

  Scenario: Subagent counts are bounded
    Given a parent presence session
    When subagent stop arrives before any subagent start
    Then activeSubagents remains 0
```

```gherkin
Feature: Hook command fail-open behavior
  Scenario Outline: Hook failures never block
    Given the hook command receives a <failure>
    When it runs for <provider>
    Then it exits 0
    And stdout is the <allow_response>
    And stderr/logs do not contain raw payloads or secrets
    Examples:
      | provider | failure | allow_response |
      | codex | stdin read error | "{}\n" |
      | codex | oversized stdin | "{}\n" |
      | codex | daemon start failure | "{}\n" |
      | codex | stale descriptor | "{}\n" |
      | codex | locked project | "{}\n" |
      | codex | control timeout | "{}\n" |
      | codex | control 401 | "{}\n" |
      | codex | control 400 | "{}\n" |
      | codex | control 500 | "{}\n" |
      | codex | panic recovery | "{}\n" |
      | claude | malformed JSON | "{}\n" |
      | cursor | malformed JSON | "{\"permission\":\"allow\"}\n" |
      | unknown | unsupported provider | "" |

  Scenario: Hook command does not pollute MCP stdout
    Given an MCP STDIO client and a hook invocation share the same project
    When both run
    Then MCP stdout contains only JSON-RPC frames
    And hook stdout contains only the provider allow response
```

```gherkin
Feature: Agent presence registry and private control
  Scenario: Private control token gates presence
    Given an owner daemon control server
    When POST or GET /agent-presence is called without the token or with the wrong token
    Then it returns 401
    And the presence registry is not touched

  Scenario: Presence updates are idempotent
    Given SessionStart records a provider session
    When PreToolUse and PostToolUse arrive for the same session
    Then firstSeenAt stays stable
    And lastSeenAt and lastEvent advance

  Scenario: Presence expires and notifies idle shutdown
    Given a presence session with TTL 10m
    When the clock advances past TTL and pruning runs
    Then the session is removed
    And activity count transitions to zero

  Scenario: SessionEnd removes presence
    Given an active Claude or Cursor presence session
    When SessionEnd arrives
    Then the session is removed or marked expired immediately

  Scenario: Presence is not daemon identity
    Given an owner exists for a canonical data-dir and root-dir
    When hooks report different providers, models, events, or session hashes
    Then no project daemon config mismatch occurs
```

```gherkin
Feature: Hook-first daemon lifecycle
  Scenario: Hook starts daemon before MCP
    Given no daemon descriptor exists
    When scripts/run.sh agent-hook codex receives SessionStart
    Then it exits 0 with "{}"
    And the owner descriptor is written securely
    And GET /agent-presence through private control returns one hashed codex session
    When scripts/run.sh mcp starts for the same data/root
    Then it attaches without lock, port, or config mismatch errors

  Scenario: MCP starts daemon before hook
    Given scripts/run.sh mcp already started the owner
    When scripts/run.sh agent-hook codex receives PreToolUse
    Then private presence updates
    And MCP tool calls still succeed

  Scenario Outline: Daemon lifetime handles mixed activity
    Given owner activity includes <activity>
    When <event> happens
    Then shutdown behavior is <result>
    Examples:
      | activity | event | result |
      | presence only | daemon-idle-timeout=0 after presence expires | owner exits |
      | presence only | presence still active | owner stays alive |
      | MCP session plus presence | MCP session closes | owner stays alive until presence expires |
      | MCP session plus presence | presence expires while MCP session alive | owner stays alive |
      | startup race | hook arrives during first-activity grace | owner does not cancel |
```

```gherkin
Feature: Wrapper migration
  Scenario: MCP mode preserves existing behavior
    Given a fake leafwiki binary
    When scripts/run.sh mcp receives stdin
    Then stdin reaches leafwiki --mcp=stdio
    And stdout is direct protocol output
    And dry-run redacts API keys, JWT secrets, and admin passwords

  Scenario: Hook mode forwards stdin
    Given a fake leafwiki binary
    When scripts/run.sh agent-hook codex receives hook JSON
    Then it invokes leafwiki agent-hook codex with shared data/root/host/port options
    And hook stdin is forwarded unchanged
    And stdout contains only "{}"

  Scenario: Breaking rename is complete
    Given the repository scripts, installers, docs, and examples are scanned
    Then live setup paths use run.sh
    And no compatibility run-mcp.sh shim remains
```

```gherkin
Feature: End-to-end agent hook presence
  Scenario: Hook-started daemon accepts later MCP attach
    Given E2E_RUN_MODE=local and agent hook E2E is enabled
    When a Codex SessionStart is sent through scripts/run.sh agent-hook codex
    Then the daemon becomes reachable through its descriptor
    And private /agent-presence lists a sanitized hashed session
    When the MCP STDIO client connects
    Then create_page and get_page succeed

  Scenario: Malformed hook does not prevent later MCP work
    Given no owner is running
    When malformed hook JSON is sent through scripts/run.sh agent-hook codex
    Then the command exits 0
    When a normal MCP STDIO client starts
    Then it can create and read a page

  Scenario: Private presence is not public
    Given an owner has recorded hook presence
    When the browser or public HTTP client requests /agent-presence
    Then it receives not found or unauthorized
    And only private control with the descriptor token can read presence

  Scenario: Presence expiry allows daemon shutdown
    Given a hook-started owner with short daemon idle timeout
    When presence TTL expires and no MCP session is active
    Then /api/health eventually becomes unavailable
```

## Verification Commands

```bash
rtk go test ./internal/agenthooks ./internal/projectdaemon ./cmd/leafwiki
rtk go test ./...
rtk bash -n scripts/run.sh scripts/test-run.sh scripts/install-all-macos.sh scripts/test-install-all-macos.sh
rtk bash scripts/test-run.sh
rtk bash scripts/test-install-all-macos.sh
rtk rg -n "run-mcp\\.sh" docs scripts README.md .codex --glob '!plans/**' --glob '!references/**'
rtk npm --prefix e2e run lint
rtk npm --prefix e2e run format:check
rtk env E2E_RUN_MODE=local E2E_ENABLE_AGENT_HOOKS_LOCAL=1 E2E_ENABLE_MCP_LOCAL=1 E2E_MCP_CLIENT_TRANSPORT=stdio ./e2e/run.sh tests/agent-hooks.spec.ts
rtk env E2E_RUN_MODE=local E2E_ENABLE_MCP_LOCAL=1 E2E_MCP_CLIENT_TRANSPORT=stdio ./e2e/run.sh tests/mcp-stdio-disable-auth.spec.ts
rtk env E2E_RUN_MODE=local E2E_ENABLE_MCP_API_KEYS_LOCAL=1 E2E_MCP_CLIENT_TRANSPORT=stdio ./e2e/run.sh tests/mcp-stdio-api-keys.spec.ts
```

## Definition of Done

- `plans/agent_hooks.PLAN.md` exists and includes `codex://threads/019ea3a5-4240-7b80-b5f0-76c459294370`.
- Codex, Claude, and Cursor supported events parse, normalize, and record presence without sensitive fields.
- All hook command failures exit `0` with correct provider allow output.
- Hook-first and MCP-first daemon startup both work against the same canonical data/root project.
- Agent presence is in-memory, private-control-only, TTL-expiring, and excluded from daemon identity/config hashing.
- `run.sh mcp` replaces `run-mcp.sh`; there is no compatibility shim.
- Docs include hook integration examples but do not auto-install hooks.
- The complete Gherkin suite above is represented by Go unit/process tests, Bash script tests, installer tests, and Playwright E2E.
- All verification commands pass.
- Final review explicitly checks fail-open behavior, non-happy-path coverage, privacy, stdout hygiene, descriptor safety, stale docs, and repo fit.
