# Typed Stable IDs Implementation Plan

> **For agentic workers:** Use `superpowers:subagent-driven-development` or `superpowers:executing-plans`. Follow TDD: write failing identifier-contract tests first, then implementation, then full verification.
>
> **Conversation reference:** `codex://threads/019ea755-a3e3-7571-a8cf-c70a33fea656`

## Summary

Introduce semantic, typed, stable IDs for user/agent-facing contracts before any `go-i18n` migration. Keep existing rendered English output for now, but stop tests from depending on it.

The implementation must distinguish:
- **Contract IDs:** error codes, validation codes, MCP tool IDs, provider/event/mode/source/status IDs.
- **Message IDs:** stable future-localization handles such as `errors.page.version_conflict`.
- **Rendered text:** current English copy, kept as compatibility/default output but no longer the primary test oracle.

## Key Changes

### Shared Backend ID Contract

Add shared typed primitives in `internal/core/shared/errors`:

```go
type ErrorCode string
type MessageID string

type ErrorDefinition struct {
	Code           ErrorCode
	MessageID      MessageID
	DefaultMessage string
	DefaultTemplate string
}
```

Update `LocalizedError` to use `ErrorCode` and `MessageID` while keeping existing `message`, `template`, and `args` fields during migration. Add constructors that accept `ErrorDefinition`; keep the old constructor only as a compatibility wrapper.

Extend field validation errors:

```go
type FieldError struct {
	Field     string    `json:"field"`
	Code      ErrorCode `json:"code,omitempty"`
	Message   string    `json:"message"`
	MessageID MessageID `json:"messageId,omitempty"`
}
```

Do not remove existing JSON fields in this slice.

### API, CLI, MCP, Agent, UI IDs

- Convert existing `ErrCode*` constants in `internal/wiki/*/errors.go` to `sharederrors.ErrorCode`.
- Add `MessageID*` constants beside those error codes.
- Replace raw code literals such as `asset_page_not_found`, importer codes, validation issue codes, partial-edit codes, and workspace-sync validation codes with typed constants.
- Add shared HTTP error helpers so middleware stops returning only `{"error":"English"}` and instead returns structured `{"error":{"code","messageId","message","template","args"}}`.
- Add CLI typed errors in `cmd/leafwiki`: `CLIErrorCode`, `CLIMessageID`, and `CLIError`. Keep stderr text unchanged, but unit tests assert codes.
- Strengthen MCP descriptors: keep `Tool*` names as typed tool IDs, add `DescriptionID MessageID`, and retain rendered `Description`.
- Change MCP message-only outputs from only `{ "message": "Page moved" }` to `{ "messageId": "...", "message": "Page moved" }`.
- Type agent/provider/event/mode/source constants in `internal/agenthooks`, `internal/wiki/presence`, `internal/projectdaemon`, and workspace-sync aliases.
- Add frontend stable locator/message IDs where tests currently assert app copy: `data-testid`, `data-l10n-id`, `data-error-code`, `data-validation-code`, `data-import-status`, `data-history-change`, and `data-revision-badge`.

### Documentation

Create `docs/typed-ids.md` covering:
- ID taxonomy: `ErrorCode`, `MessageID`, `ToolID`, validation issue codes, provider/event/mode/source IDs.
- Naming rules: semantic IDs only, never English sentence IDs.
- Compatibility rule: keep rendered English fields until the later i18n migration.
- Testing rule: tests assert IDs/codes/args, not English copy.
- Migration note: this prepares for `go-i18n`; no `go-i18n` dependency is required in this plan.

Update `docs/mcp.md` only where MCP result/error payload examples gain `messageId`.

## Implementation Steps

1. Add failing tests for shared types and constructors in `internal/core/shared/errors`.
2. Add typed error/message definitions and update `LocalizedError`, `FieldError`, and test helpers.
3. Convert API error responders domain by domain: pages, auth, assets, branding, links, search, tags, properties, revisions, importer, workspace-sync, middleware.
4. Convert MCP tool descriptors and message outputs, including schema changes for `messageId`.
5. Convert validation, workspace-sync, presence, agent-hook, and provider/event/source/mode strings to typed constants.
6. Convert CLI parse/startup/config errors to typed `CLIError` paths while preserving stderr rendering.
7. Add frontend stable locator/message ID constants and update high-risk E2E page objects/tests.
8. Add docs and scan tests for remaining fragile English assertions.
9. Run the full verification suite.

## Gherkin Test Suite

```gherkin
Feature: Structured backend error identifiers
  Scenario: Page version conflict exposes stable IDs
    Given a stale page update request
    When the API returns a conflict
    Then error.code is "page_version_conflict"
    And error.messageId is "errors.page.version_conflict"
    And tests do not assert the English message

  Scenario: Middleware authentication failure is structured
    Given a private API request without a token
    When auth middleware rejects it
    Then the response contains error.code
    And the response is not a raw string error

  Scenario: Field validation exposes stable codes
    Given invalid frontmatter metadata
    When validation fails
    Then each field error contains field and code
    And the rendered message may change without breaking tests
```

```gherkin
Feature: MCP identifiers and message IDs
  Scenario: Tool descriptors keep stable tool IDs
    Given the MCP server lists tools
    When descriptors are returned
    Then each tool name matches the typed ToolID constants
    And each descriptor has a stable description message ID in code

  Scenario: Message-only MCP success response carries messageId
    Given wiki_move_page succeeds
    When the MCP tool returns
    Then structuredContent.messageId is "mcp.pages.move.success"
    And structuredContent.message may be rendered English

  Scenario: MCP tool error exposes stable code
    Given wiki_update_page receives a stale version
    When the tool fails
    Then the error helper can extract "page_version_conflict"
    And tests do not compare "code: English message"
```

```gherkin
Feature: Typed operational identifiers
  Scenario: Workspace sync validation issue is typed
    Given duplicate leafwiki IDs exist on disk
    When wiki_validate_wiki runs
    Then the issue code is the typed duplicate-leafwiki-id constant
    And severity is the typed error severity

  Scenario: Agent hook provider and event are typed
    Given a Codex PreToolUse hook payload
    When it is normalized
    Then provider and event use typed constants
    And unsupported events are rejected without stringly comparisons

  Scenario: Presence mode validation is typed
    Given a web heartbeat with an invalid mode
    When the registry validates it
    Then it returns a typed validation code
```

```gherkin
Feature: UI and E2E tests use stable IDs
  Scenario: Save conflict toast is independent of English copy
    Given a page save returns page_version_conflict
    When the editor shows the conflict toast
    Then E2E locates data-error-code="page_version_conflict"
    And data-l10n-id identifies the displayed message

  Scenario: MCP API key dialog error state is stable
    Given API key loading fails
    When the dialog renders
    Then E2E locates mcp-api-keys-dialog-load-error
    And retry is located by mcp-api-keys-dialog-retry

  Scenario: Importer states are stable
    Given an import plan is running, failed, canceled, or complete
    When the importer renders status
    Then E2E asserts data-import-status
    And does not assert "Running", "Failed", "Canceled", or "Completed"

  Scenario: History structure changes are stable
    Given title and slug changed between revisions
    When the history structure view renders
    Then E2E asserts data-history-change="title" and "slug"
    And does not assert localized labels
```

## Verification Commands

Run from repo root with `rtk`:

```bash
rtk go test ./internal/core/shared/errors ./internal/wiki/... ./internal/http/... ./internal/projectdaemon ./internal/agenthooks ./cmd/leafwiki
rtk go test ./...
rtk npm --prefix ui/leafwiki-ui run lint
rtk npm --prefix ui/leafwiki-ui run build
rtk npm --prefix e2e run lint
rtk make run-e2e-local-fast GREP="MCP API Keys|Workspace Sync|history|page"
rtk make run-e2e-local-fast GREP="mcp|MCP"
rtk bash scripts/test-run.sh
```

Add a repo scan check before completion:

```bash
rtk rg -n 'error\\.message|\\["message"\\]|getByText\\(|toHaveText\\(|toContainText\\(|ErrorContains|EqualError|strings\\.Contains\\([^\\n]*err\\.Error' internal cmd e2e ui/leafwiki-ui/src
```

Every remaining hit must be either user-authored content, accessibility behavior intentionally testing visible copy, or documented in `docs/typed-ids.md`.

## Definition of Done

- All highest-priority and also-worth-typing categories have typed constants or explicit documented exclusions.
- API, MCP, CLI, validation, agent, presence, and frontend test contracts expose stable IDs.
- Existing rendered English behavior remains compatible unless explicitly documented.
- Tests assert IDs/codes/args instead of prose wherever prose is not the behavior under test.
- Gherkin scenarios above are represented by unit, integration, and E2E tests.
- `docs/typed-ids.md` exists and references this thread.
- All verification commands pass.
- No new untyped user/agent-facing error/message contract is introduced without an ID.
