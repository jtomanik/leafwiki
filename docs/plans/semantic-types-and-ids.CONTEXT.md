<!-- leafwiki
version: 1
page:
  id: semantic-types-ids-context-20260622
  title: Semantic Types And IDs - Context
  created_at: "2026-06-21T22:19:24Z"
  updated_at: "2026-06-21T22:19:24Z"
  creator_id: system
  last_author_id: system
tags:
  - plans
  - context
fields:
  type: refactor
-->

# Semantic Types And IDs - Context

## Problem Frame

LeafWiki has many values that share primitive shapes while representing different concepts:

- A page ID, workspace ID, user ID, revision ID, session ID, and MCP API key ID are all strings.
- A browser route path, wiki route path, Markdown path, slug, asset name, and API path are all strings.
- A search limit, revision limit, offset, port, byte size, retry count, and history depth are all ints.
- An issued-at timestamp, expiry timestamp, heartbeat timestamp, sync timestamp, and revision timestamp are all time values.
- An error code, message ID, MCP tool ID, provider ID, event ID, status, state, and mode are all strings.

This is manageable for a human with full context, but it is fragile for coding agents. An LLM sees a local call site, a few adjacent strings, and nearby tests. If the type system and names do not encode meaning, the agent must infer intent from prose and conventions. That increases the chance of subtle bugs, especially in broad refactors.

The target is not "more types everywhere." The target is to encode semantic intent at boundaries and high-risk call sites so the compiler, tests, and future coding agents all have better handles.

## Product Rationale

The work supports three goals at once:

1. More stable contracts for users, APIs, MCP clients, and E2E tests.
2. A future localization path where message IDs exist before message catalogs.
3. Better code alignment with LLM coding agents by making the intended meaning of a primitive value explicit.

Stable IDs should be durable and semantic. Rendered English copy should remain mutable. User-authored content should remain user-authored content.

## Conceptual Taxonomy

### Public Protocol IDs

Values that cross API, MCP, CLI, or persisted-diagnostic boundaries:

- `ErrorCode`
- `MessageID`
- `FieldErrorCode`
- `ValidationIssueCode`
- `ToolID`
- `ToolDescriptionID`
- `ToolMessageID`
- `ProviderID`
- `EventID`
- `PresenceMode`
- `WorkspaceState`
- `ExecutionStatus`

These values should use defined types and constants. Public JSON may still serialize as strings, but internal code should not treat them as arbitrary strings.

### Domain Value Types

Values that identify or address domain objects:

- `PageID`
- `WorkspaceID`
- `UserID`
- `RevisionID`
- `CommitHash`
- `SessionID`
- `MCPAPIKeyID`
- `PageVersion`
- `RoutePath`
- `MarkdownPath`
- `WorkspaceSourcePath`
- `Slug`
- `AssetName`

These should be introduced where they reduce real ambiguity, especially in service inputs, actor contexts, route parsing, and store contracts.

### Numeric And Time Setting Types

Values whose primitive shape does not explain units or semantics:

- `Port`
- `ByteSize`
- `SearchLimit`
- `SearchOffset`
- `RevisionLimit`
- `TreeDepth`
- `IdleTimeout`
- `AccessTokenTTL`
- `RefreshTokenTTL`
- `IssuedAt`
- `ExpiresAt`
- `LastSeenAt`
- `SyncedAt`

These do not all need rich wrappers in the first slice. Start with the cases where adjacent numeric parameters, external parsing, or unit confusion create risk.

### UI And Test Contract IDs

Values that tests or UI state machines depend on:

- Existing `data-testid`.
- New `data-error-code`.
- New `data-l10n-id`.
- New `data-validation-code`.
- New `data-import-status`.
- New `data-history-change`.
- New typed frontend IDs such as `PageID`, `WorkspaceID`, `RevisionID`, `ApiErrorCode`, `MessageID`, and `MCPToolID`.

Existing test IDs stay. New semantic attributes are added only where state, status, validation, or error semantics are currently asserted through English copy.

## Type Strength Ladder

The implementation should use the lightest mechanism that gives meaningful safety.

| Level | Use When | Example |
|---|---|---|
| Constant only | The value is stable but low-risk or already isolated | UI test ID constants |
| Defined type | A value crosses modules or is easy to mix up | `type WorkspaceID string` |
| Constructor/validator | A raw input must be normalized or rejected once | `ParseWorkspaceID(raw)` |
| Rich struct | Multiple fields are inseparable or unit-sensitive | `ByteSize`, token TTL config |

Go aliases such as `type WorkspaceID = string` should be avoided for high-risk values because they do not create compile-time separation. They can be used only as temporary compatibility bridges when a direct migration would be too disruptive.

TypeScript should use branded types or narrow unions at API/helper/store boundaries. Do not require every component prop to become branded in the first pass if the value has already been parsed and remains local.

## Ownership Rule

Types should live near the domain that owns the vocabulary:

- Shared error/message contract types belong in `internal/core/shared/errors`.
- Workspace ID parsing can evolve in `internal/workspaceid`.
- Tree-owned concepts such as page IDs, route paths, slugs, and page versions should be introduced from `internal/core/tree` or a clearly named adjacent package if import cycles require it.
- Workspace sync validation issue codes should belong with `internal/core/markdownvalidation` and be reused by `internal/workspacesync` and MCP adapters.
- MCP tool IDs and message IDs should belong in `internal/wiki/mcp`.
- Runtime role/state types should stay in `internal/projectdaemon` and `internal/wikid`.
- Frontend branded types should live under `ui/leafwiki-ui/src/lib/api` or `ui/leafwiki-ui/src/lib/domainTypes.ts`, not scattered through components.

Do not create a catch-all global `ids` package unless implementation proves import cycles or duplication make it necessary.

## Boundary Rule

Raw primitives are allowed at I/O boundaries:

- HTTP JSON decode/encode.
- URL path and query parsing.
- CLI flags and env vars.
- YAML config parse.
- Database scans.
- Filesystem path reads.
- MCP request/response serialization.
- Browser route params.

After parsing and validation, code should carry semantic values internally. When a value leaves the process or crosses JSON/MCP/CLI boundaries, it can serialize back to a primitive.

## Compatibility Rule

This is a contract-hardening migration, not a product-copy rewrite:

- Keep existing JSON fields such as `message`, `template`, and `args`.
- Add `messageId` where a rendered message can change later.
- Keep English output as default output.
- Keep existing `data-testid`s.
- Do not change public route paths, JSON field names, CLI flags, env var names, or CSS classes just to make them typed.
- Add tests that assert new codes/message IDs while preserving compatibility for old fields during the migration.

## Exclusions

Do not turn these into localization IDs or over-typed contracts in this plan:

- User-authored Markdown, titles, slugs, tags, and property values as content.
- JSON field names.
- CSS class names.
- Public route literal strings.
- CLI flag names and env var names as externally documented inputs.
- Visible copy when the test is intentionally checking accessibility text or user-visible documentation.
- Short-lived local variables with no cross-boundary ambiguity.

Some of these values may still get semantic wrapper types at parser boundaries, for example a validated slug or route path. The exclusion is specifically about treating them as stable message IDs or rewriting every literal.

## Architecture Constraints

The plan must preserve current runtime decisions from recent cleanup work:

- Federated `wikid`/`frontd`/`workspaced` runtime is the current contract.
- Git-backed workspace sync and history are the current contract.
- Old runtime/revision/sync modes should not be reintroduced as compatibility surfaces.
- `scripts/run.sh mcp` and current MCP tool names remain load-bearing user/agent surfaces.
- Existing role/state and workspace-sync reason/source defined types should be reused.

## Implementation Posture

This is a broad refactor with many contract surfaces. The implementation should be vertical and test-led:

- Start with shared contracts and one representative API/MCP/frontend path.
- Preserve behavior while adding IDs.
- Expand by domain only after the pattern is proven.
- Treat scan output as triage, not an automatic "fix every match" mandate.
- Prefer domain-owned types and small constructors over a framework.

## Main Risks

- Creating a global type package that introduces import cycles or a dependency sink.
- Breaking JSON compatibility by removing `message`, `template`, or `args` too early.
- Making tests worse by replacing useful accessibility assertions with opaque test hooks.
- Typing public strings too aggressively and making integration code verbose without added safety.
- Introducing duplicate semantic types for concepts that already have domain-owned defined types.
- Letting old English-message assertions remain in protocol tests where stable IDs should be the oracle.
