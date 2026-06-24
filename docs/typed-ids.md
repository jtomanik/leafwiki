<!-- leafwiki
version: 1
page:
  id: YHo0qU-vgP
  title: Typed IDs and Semantic Values
  created_at: "2026-06-21T22:54:02.860183502Z"
  updated_at: "2026-06-21T22:54:02.860183502Z"
  creator_id: system
  last_author_id: system
-->

# Typed IDs and Semantic Values

LeafWiki uses stable semantic identifiers for machine-facing contracts and semantic value types for high-risk values that otherwise look like plain strings. This keeps English copy mutable while giving Go, TypeScript, MCP clients, and E2E tests stable handles.

This document covers the typed-ID implementation from `docs/plans/semantic-types-and-ids.PLAN.md` and the originating thread `codex://threads/019ea755-a3e3-7571-a8cf-c70a33fea656`.

The backend i18n layer built on these IDs is documented in `docs/i18n.md`.
It renders English messages from stable IDs while preserving the typed-ID
contract and compatibility fields described here.

## Rules

- Add stable IDs for errors, validation issues, MCP tools, MCP result messages, and state/status UI that agents or tests consume.
- Keep existing compatibility fields. Adding `messageId` must not remove `message`, `template`, or existing `code` fields.
- Do not use English sentences as IDs. IDs describe semantics, not rendered copy.
- Do not turn user-authored content, page titles, slugs, tags, or property keys into localization IDs.
- Preserve existing `data-testid` locators. Add semantic attributes only where state, status, validation, or error meaning matters.
- Raw primitives are acceptable at I/O boundaries. Parse or wrap into semantic values once the boundary value is validated or assigned domain meaning.

## Backend Contracts

Shared localized API errors use:

```json
{
  "error": {
    "code": "page_version_conflict",
    "messageId": "errors.page.version_conflict",
    "message": "The page has been modified since you started editing",
    "template": "page.version_conflict",
    "args": []
  }
}
```

Field validation errors use stable field-level codes and message IDs:

```json
{
  "error": "validation_error",
  "fields": [
    {
      "field": "slug",
      "code": "page_slug_invalid",
      "messageId": "validation.page.slug_invalid",
      "message": "slug must not be empty"
    }
  ]
}
```

Workspace validation issues use typed issue codes and severities. Serialize them as strings in JSON, but keep domain code in Go as `markdownvalidation.IssueCode` and `markdownvalidation.IssueSeverity`.

## MCP Contracts

MCP tool names are `ToolID` values in Go and continue to serialize as canonical `wiki_*` strings. Tool descriptors also carry typed description IDs internally.

Message-only success responses include both `messageId` and `message`:

```json
{
  "messageId": "mcp.tools.wiki_move_page.success",
  "message": "Page moved"
}
```

Tests and clients should assert `messageId` when they are checking result semantics. Use `message` only when testing user-visible rendering or backward compatibility.

HTTP API success payloads use API-scoped message IDs, such as
`api.pages.sort.success` or `api.assets.delete.success`. MCP tool success
payloads keep the `mcp.tools.*` namespace so agent-facing tool contracts remain
separate from HTTP route contracts.

Tool errors keep human-readable text content and expose stable detail under MCP `_meta.error`:

```json
{
  "_meta": {
    "error": {
      "code": "page_version_conflict",
      "messageId": "errors.page.version_conflict",
      "message": "Page was changed by another request"
    }
  }
}
```

LeafWiki tool-handler errors use this structured `_meta.error` shape. Domain
errors, target helper validation, and auth/role failures use specific codes.
Some low-level tool-specific guardrails still use the stable generic
`mcp_tool_error` fallback until promoted. Protocol-level MCP failures such as
unknown tool names or JSON schema decode failures may still be emitted by the MCP
SDK as protocol errors.

## Semantic Values

Use defined or branded types for values that are easy to mix up:

- Page, revision, workspace, user, MCP API key, and commit IDs.
- Page versions.
- Route paths, Markdown paths, slugs, and asset names.
- Error codes, field validation codes, message IDs, tool IDs, validation issue codes, and validation severities.

Do not introduce broad wrappers for every string. Prefer high-value boundaries and adjacent parameters with the same primitive type.

Workspace IDs are parsed into `workspaceid.WorkspaceID` at service boundaries
that need validation detail. Frontd/wikid route parsing intentionally treats
malformed workspace path segments as route misses or workspace-not-found results
instead of returning public validation JSON; that keeps invalid route probing out
of the API error contract while preserving typed validation in service code.

## Semantic Hygiene Checker

Run the Go semantic-boundary checker before review:

```sh
rtk bash scripts/check-semantic-hygiene.sh
```

The script runs `cmd/leafwiki-vet`, which currently contains the
`internal/analysis/semantichygiene` analyzer. The analyzer checks for:

- Semantic values converted with `.String()` before internal calls,
  comparisons, or domain assignments.
- Direct casts to semantic types outside approved parser/constructor code and
  semantic-owner representation helpers.
- Unchecked semantic constructors called from ordinary internal code instead
  of parser, owner-adapter, or narrow fixture-builder contexts.
- Raw `string`, `[]string`, or `map[string]...` carriers with semantic names in
  the internal service, use-case, or domain signatures, interface methods, and
  fields covered by the current analyzer policy.
- Raw numeric semantic carriers such as pagination offsets/limits, tree depth,
  and byte limits after boundary parsing.
- Message-bearing Go contract structs and composites that expose or populate
  rendered `message`/warning strings without a catalog-backed `messageId`.
- Direct HTTP/private status response maps and CLI/control prose sinks that
  forward rendered status/error text instead of catalog-backed metadata.
- Validators that return primitive strings after validating known semantic
  values.
- Raw stable contract literals for error codes, field validation codes,
  message IDs, validation issue codes, and MCP tool IDs.

Primitive values remain valid at real I/O edges: HTTP and MCP payloads, JSON
DTOs, CLI/env/config parsing, persistence adapters, filesystem/frontmatter
serialization, URL construction, and logging. Semantic-owner adapter functions
are explicit representation helpers in the file that owns the semantic type,
for example route-to-markdown or filesystem-path conversion methods on the
semantic value itself. These allowances should stay narrow in the checker: MCP
raw fields belong in MCP wire payload type files, frontmatter raw fields belong
in Markdown metadata codec files, and URL escapes must go through
package-qualified `net/url` helpers rather than local wrappers with similar
names. Tests and fixtures should use semantic values through helper APIs unless
they are asserting an actual wire, persistence, or rendered-output boundary.
Tightening
those allowances is reviewer-owned policy work and should be paired with focused
fixtures before implementers are held to the stricter gate. Product code should
parse or wrap boundary values before passing them into domain or service code.

Policy changes under `internal/analysis/semantichygiene` are reviewer-owned.
If a new middle-layer boundary is not covered yet, treat that as a checker-owner
follow-up rather than duplicating semantic policy in shell scripts or product
code.
Adding a broad package allowlist, hiding a diagnostic behind a helper, or
fixing a middle-layer diagnostic by adding a cast or `.String()` wrapper does
not preserve the contract. Prefer changing the callee to accept the semantic
type, adding a parser that returns the semantic type, or moving primitive
serialization to an explicit boundary.

## Frontend Contracts

Frontend API types use branded or narrow types in `ui/leafwiki-ui/src/lib/semanticTypes.ts` for API-facing values such as `PageID`, `RevisionID`, `WorkspaceID`, `ApiErrorCode`, `MessageID`, `WorkspaceSyncIssueCode`, and `MCPToolID`.

State/error/status UI should expose semantic attributes when tests or agents need stable meaning:

- `data-error-code`
- `data-l10n-id`
- `data-validation-code`
- `data-validation-severity`
- `data-import-status`
- `data-history-change`
- `data-revision-badge`

Existing `data-testid` attributes remain for locating stable widgets. Semantic attributes answer what state or error the widget represents.

## Scan Guidance

Use `rtk bash scripts/check-semantic-hygiene.sh` as the only semantic hygiene
review command. The older `scripts/check-typed-id-oracles.sh` entrypoint exists
only as a compatibility wrapper for this command.

When a machine-facing contract assertion is too dependent on visible copy,
prefer a stable code, message ID, typed helper value, or semantic `data-*`
attribute.
