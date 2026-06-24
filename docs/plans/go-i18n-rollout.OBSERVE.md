<!-- leafwiki
version: 1
page:
  id: go-i18n-rollout-observe-20260623
  title: Go i18n Rollout Observations
  created_at: "2026-06-23T21:20:00Z"
  updated_at: "2026-06-23T21:20:00Z"
  creator_id: system
  last_author_id: system
tags:
  - plans
fields:
  type: observe
-->

# Go i18n Rollout Observations

## Source Context

- Workflow: `docs/plans/planning.aibasic.txt`.
- Original i18n and typed-ID discussion: `codex://threads/019ea755-a3e3-7571-a8cf-c70a33fea656`.
- Current planning goal: implement English-only `go-i18n` localization for all LeafWiki user/agent-facing CLI, API, and MCP messages while preserving typed IDs and compatibility fields.
- Explicit user decisions:
  - Backend-originated messages are localized by the backend.
  - Scope includes CLI, API, and MCP user/agent-facing messages.
  - Code remains the source of emitted IDs.
  - Catalog coverage must be enforced by checker/tests.
  - `internal/analysis` semantic hygiene should participate in enforcement.
  - Existing positional template arguments are bridged rather than migrated all at once.
  - Locale resolution is English-only for now.
  - Behavior tests must assert IDs, codes, semantic attributes, or typed IDs instead of prose, except renderer/catalog compatibility and user-authored content.

## Current Foundation

The typed-ID and semantic-type slice has shipped enough foundation to begin i18n:

- `internal/core/shared/errors/localized_error.go` defines typed `ErrorCode`, `MessageID`, `ErrorDefinition`, `LocalizedError`, `LocalizedErrorDetail`, and default `MessageIDForCode`.
- `internal/core/shared/errors/field_error.go` defines typed `FieldErrorCode`, field-level `MessageID`, and validation payload fields.
- Domain responders already return `code`, `messageId`, `message`, `template`, and `args` in many API error paths.
- MCP tools have typed `ToolID`, typed `ToolDescriptionID`, typed tool success message IDs, output schemas with `messageId`, and structured `_meta.error`.
- Frontend API error types carry branded `ApiErrorCode` and `MessageID`.
- E2E examples already assert semantic attributes for version-conflict and MCP API-key load errors.
- `docs/typed-ids.md` documents the split between stable IDs, message IDs, and rendered text.

## Local go-i18n Reference

The repository has a local copy of `nicksnyder/go-i18n` under `references/go-i18n`.

Relevant facts from that copy:

- `references/go-i18n/README.md` documents the long-lived `i18n.Bundle`, `i18n.NewLocalizer`, TOML message files, `LoadMessageFileFS`, and `goi18n extract` / `goi18n merge`.
- `references/go-i18n/i18n/localizer.go` shows:
  - `LocalizeConfig.MessageID` is ignored when `DefaultMessage` is set, unless it matches `DefaultMessage.ID`.
  - `DefaultMessage` is the fallback when a message is absent.
  - `TemplateData` uses Go `text/template`.
  - `LocalizeWithTag` may return a best-effort string with an error.
  - `MustLocalize` panics on errors and should not be used on user-facing request paths.
- `references/go-i18n/i18n/bundlefs.go` provides `LoadMessageFileFS` for embedded catalogs.
- `references/go-i18n/goi18n/extract_command.go` extracts only `i18n.Message` and `i18n.LocalizeConfig` composite literals from Go source.
- The extractor supports string literals, string concatenation, same-file constants, and `string(Const)` casts.
- The extractor skips `_test.go`.
- Custom LeafWiki structs are not extracted unless they contain real `i18n.Message` or `i18n.LocalizeConfig` literals.

Conclusion: the implementation should create real `i18n.Message` literals in Go source as the extractable message registry. A custom LeafWiki catalog struct alone is not enough for `goi18n extract`.

## CLI Surfaces

Application CLI:

- `cmd/leafwiki/main.go`
  - `writeUsage` owns help/usage prose.
  - `fail`, `failWithoutAgentHook`, and `failureMessage` own startup/config/runtime failure output.
  - User-facing outputs include daemon/config errors, unknown commands, reset-admin-password output, MCP STDIO validation, and agent-hook fail-open behavior.
- `cmd/leafwiki/main_test.go`
  - Existing tests assert stdout/stderr placement and exact compatibility fragments for help, daemon errors, reset-admin-password, logging targets, STDIO constraints, and removed env/flag behavior.

Shell wrapper and install helpers:

- `scripts/run.sh`
  - User/agent-facing wrapper for MCP STDIO and agent-hook invocation.
  - Owns help, dry-run/status text, `Error:` output, native STDIO diagnostics, secret redaction, stdout hygiene, and fail-open hook JSON.
- `scripts/test-run.sh`
  - Tests wrapper help, dry-run, legacy option rejection, stdout/stderr discipline, secret redaction, and stdin forwarding.
- `scripts/install-macos.sh`, `scripts/install-all-macos.sh`, `scripts/changelog.sh`
  - Developer-facing CLI text. Include only messages that users see when invoking these scripts.

Current CLI gap:

- There is no CLI message ID abstraction today.
- The Go CLI can use the shared localization renderer directly.
- Shell scripts cannot call `go-i18n` directly. They either need generated English message variables from the Go catalog or a deliberate explicit exception.

## API Surfaces

Existing structured coverage:

- Shared errors:
  - `internal/core/shared/errors/localized_error.go`
  - `internal/core/shared/errors/field_error.go`
- Domain error responders:
  - `internal/wiki/pages/errors.go`
  - `internal/wiki/assets/errors.go`
  - `internal/wiki/search/errors.go`
  - `internal/wiki/links/errors.go`
  - `internal/wiki/branding/errors.go`
  - `internal/wiki/properties/errors.go`
  - `internal/wiki/revisions/errors.go`
  - `internal/wiki/auth/errors.go`
- Private/control structured errors:
  - `internal/projectdaemon/server.go`
  - `internal/frontd/workspace_proxy.go`
  - `internal/frontd/mcp_sessions.go`
  - `internal/workspaced/actor_context.go`
  - `internal/wikid/private_workspace_api.go`

Existing success message coverage:

- Page delete/move/sort responses return API-scoped `messageId` plus `message`.
- Asset delete returns an API-scoped `messageId` plus `message`.
- Cross API/MCP parity tests exist in `internal/wiki/mcp/mcp_integration_test.go`.

Gaps:

- Object-returning mutations generally lack success IDs:
  - page create/update/ensure/copy/refactor apply
  - asset upload/rename
  - auth/user/API-key mutations
  - importer actions
  - workspace-sync refresh/status-style operations
- `NoContent` routes such as some convert flows may have no success payload.
- Presence routes return simple `{"ok": true}` and still have some plain-string fallback errors.
- OAuth endpoints use RFC fields such as `error` and `error_description`; those must not be replaced incompatibly.

Compatibility fields to preserve:

- `error`
- `fields`
- `code`
- `messageId`
- `message`
- `template`
- `args`
- OAuth RFC fields such as `error`, `error_description`, and token response fields.

## MCP Surfaces

Existing structured coverage:

- `internal/wiki/mcp/tool_descriptors.go`
  - `ToolID`
  - `ToolDescriptionID`
  - rendered English `Description`
- `internal/wiki/mcp/types.go`
  - `ToolMessageID`
  - `messageOutput{messageId,message}`
- `internal/wiki/mcp/schema.go`
  - output schema includes `messageId` for message-only outputs.
- `internal/wiki/mcp/helpers.go`
  - structured `_meta.error` with `code`, `messageId`, `message`, `template`, `args`.
- Tests:
  - `internal/wiki/mcp/tool_contracts_test.go`
  - `internal/wiki/mcp/helpers_test.go`
  - `internal/wiki/mcp/mcp_integration_test.go`

Current MCP gaps:

- Message-only success IDs exist for delete/move/sort/convert/delete-asset.
- Object-returning tools generally do not expose success IDs:
  - create/update/ensure/copy page
  - update metadata
  - replace section
  - refresh
  - upload/rename asset
  - restore revision
  - apply refactor
- Tool descriptions have `DescriptionID`, but descriptor output still sends only rendered `Description`.
- Tool input aliases such as `id` and `pageId` are compatibility surfaces and must remain.

## Frontend and E2E Test Surfaces

Model examples already in place:

- `e2e/tests/editor.spec.ts` and `e2e/tests/page.spec.ts` assert version-conflict UI using `data-error-code="page_version_conflict"` and `data-l10n-id="errors.page.version_conflict"`.
- `e2e/tests/mcp-api-keys.spec.ts` asserts retry-state error with `data-error-code` and `data-l10n-id`.
- `ui/leafwiki-ui/src/lib/api/errors.test.ts` asserts `code` and `messageId`, not rendered prose.

Behavior-test assertions that should migrate:

- `e2e/pages/LoginPage.ts` asserts `Invalid credentials`.
- `e2e/tests/mcp-api-keys.spec.ts` asserts validation and remote-user prose.
- `e2e/tests/workspace-sync.spec.ts` asserts banner/error prose.
- `e2e/tests/editor.spec.ts` asserts field error text contains `empty`.
- Success toast prose appears in page/editor/importer/federated tests.
- `internal/runtimeconfig/mcp_test.go` asserts config parser fragments if those errors become user-facing CLI copy.

Assertions that should remain allowed:

- User-authored page content, headings, Markdown rendering, imported content, raw file content, and accessibility-visible content that is itself the feature under test.
- Renderer/catalog compatibility tests that intentionally prove English output.
- API/MCP compatibility tests that assert `message` is present, but only after asserting `code` and `messageId`.
- Logs, redaction, and sanitization tests when the prose is evidence of leakage behavior rather than localized user copy.

## Semantic Hygiene Enforcement

Current checker:

- `scripts/check-semantic-hygiene.sh` runs `cmd/leafwiki-vet` over `internal/...`, `cmd/...`, `e2e/...`, and `e2e-proxy/...`.
- `scripts/check-typed-id-oracles.sh` delegates to `scripts/check-semantic-hygiene.sh`.
- Analyzer files:
  - `internal/analysis/semantichygiene/analyzer.go`
  - `internal/analysis/semantichygiene/rules_literal.go`
  - `internal/analysis/semantichygiene/policy.go`
  - `internal/analysis/semantichygiene/diagnostics.go`
  - `internal/analysis/semantichygiene/analyzer_test.go`
- Fixture style:
  - `internal/analysis/semantichygiene/testdata/src/github.com/perber/wiki/internal/analysis/semantichygiene/testdata/semanticcases/semanticcases.go`
  - Expected diagnostics use inline `// want "..."`.

Analyzer opportunities:

- Flag raw user-facing prose passed to known contract constructors/calls:
  - `NewLocalizedError`
  - `NewLocalizedErrorDetail`
  - `ValidationErrors.AddWithCode`
  - `newToolDescriptor`
  - `newMessageOutput`
  - `gin.H{"message": "..."}`
- Do not ban arbitrary English literals in tests.
- Static analyzer should catch Go call sites. Scripts or tests should cover:
  - catalog completeness
  - frontend/test prose heuristics
  - shell script messages
  - generated/build artifact exclusions

## Documentation Conventions

Plans that follow `planning.aibasic.txt` emit:

- `*.OBSERVE.md`
- `*.CONTEXT.md`
- `*.DECISION.md`
- `*.PLAN.md`
- `*.planning.aibasic.json`

The plan should:

- Put provenance/thread links up front.
- Preserve decisions from discussion.
- Include architecture diagram, module tree, dependency graph, and test target locations.
- Include Gherkin scenarios by test type.
- Include explicit Definition of Done that can be used by goal mode.
- Include plantrace-style evidence requirements so Gherkin scenarios map to automated tests.
- Reference `docs/typed-ids.md` and `docs/mcp.md` as contract documents to update.

## Verification Baseline

Fresh read-only checks from the readiness audit:

- `rtk bash scripts/check-semantic-hygiene.sh` passed.
- Focused Go tests for shared errors, MCP, auth/security middleware, and domains passed: 353 tests in 12 packages.
- `rtk npm test -- --run` in `ui/leafwiki-ui` passed: 6 files, 15 tests.

The new plan should require broader implementation verification, including:

- focused localization package tests
- catalog extraction/diff checks
- semantic hygiene analyzer tests
- CLI package tests
- script wrapper tests
- API/MCP package tests
- UI unit tests
- targeted E2E for auth, editor conflict, API-key validation, workspace-sync banner, importer toasts, and MCP STDIO/API-key flows
- broad Go/UI/E2E lint/build gates before declaring done
