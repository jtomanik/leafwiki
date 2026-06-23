<!-- leafwiki
version: 1
page:
  id: semantic-types-ids-decision-20260622
  title: Semantic Types And IDs - Decision
  created_at: "2026-06-21T22:19:24Z"
  updated_at: "2026-06-21T22:19:24Z"
  creator_id: system
  last_author_id: system
tags:
  - plans
  - decision
fields:
  type: refactor
-->

# Semantic Types And IDs - Decision

## Decision

Create a new implementation plan, `semantic-types-and-ids`, that supersedes `docs/plans/semantic-ids.PLAN.md`.

The new plan keeps the old typed stable ID goal but broadens it to semantic primitive types for common string, numeric, and time/date values. The implementation should introduce typed/static IDs and semantic domain value types incrementally, starting at shared contracts and high-risk boundaries.

## Selected Course Of Action

1. Evolve existing shared error types.
   - Add `ErrorCode`, `MessageID`, `ErrorDefinition`, and typed field-validation support in `internal/core/shared/errors`.
   - Keep existing `message`, `template`, and `args` fields during the migration.
   - Add `messageId` as a new compatibility-safe field.

2. Convert existing domain error constants without changing rendered English output.
   - Convert `ErrCode*` constants to `sharederrors.ErrorCode`.
   - Add `MessageID*` constants and definition helpers per domain.
   - Reuse existing responder shapes where possible, but remove duplicated untyped contract structs over time.

3. Add semantic primitives at the boundaries that create real confusion.
   - Prioritize `WorkspaceID`, `PageID`, `UserID`, `RevisionID`, `PageVersion`, route/path/slug types, validation issue codes, tool IDs, provider/event/mode/source/status IDs, and high-risk numeric units.
   - Use defined Go types and constructors for high-risk values.
   - Use TypeScript branded types at API/helper/store boundaries.

4. Treat MCP as a first-class agent contract.
   - Make tool IDs typed.
   - Add description/message IDs alongside rendered descriptions/messages.
   - Add `messageId` to message-only success payloads.
   - Update MCP tests to extract and assert codes/message IDs rather than English fragments.

5. Include federated runtime boundaries.
   - Add typed workspace identity, agent/presence semantics, descriptor/control error contracts, and frontd/workspaced structured diagnostics where currently stringly.
   - Reuse existing `projectdaemon`, `wikid`, and `workspacesync` typed pockets.

6. Strengthen frontend and E2E contracts selectively.
   - Keep existing `data-testid`s.
   - Add semantic `data-*` attributes on stateful/error/status UI.
   - Add branded API/domain types where they protect boundaries.
   - Keep text assertions only where visible copy or user-authored content is the behavior under test.

7. Document the policy.
   - Create `docs/typed-ids.md`.
   - Explain taxonomy, ownership, compatibility, exclusions, examples, and test rules.
   - Preserve the original conversation link.

## Decision Table

| Topic | Decision | Reason |
|---|---|---|
| Existing `semantic-ids.PLAN.md` | Supersede with new plan | It predates federated runtime cleanup and omits semantic primitive types |
| `go-i18n` | Out of scope | This slice prepares IDs; it does not add catalogs or translation runtime |
| Error codes | Defined type | They are machine contracts, not arbitrary strings |
| Message IDs | Defined type | They are future localization handles and test oracles |
| Rendered English | Keep | Compatibility and current UX should not change in this slice |
| Field validation | Add code/messageId | Field messages currently have no stable machine contract |
| Domain IDs | Defined types at boundaries | Prevents adjacent string mixups and improves agent comprehension |
| Numeric units | Type selectively | Only high-risk limits, offsets, byte sizes, ports, and TTLs need first-pass coverage |
| Time/date values | Type selectively or name strongly | Use semantic wrappers when serialization or units are ambiguous |
| MCP tool names | Typed `ToolID` | MCP is an agent-facing protocol |
| MCP success messages | Add `messageId` | Message-only payloads currently force tests to assert English |
| Frontend test IDs | Keep existing | They are useful structural locators |
| Frontend semantic attrs | Add selectively | State/error/status tests need stable non-English oracles |
| Global IDs package | Avoid initially | Domain-owned types match current repo patterns and avoid dependency sinks |
| TypeScript brands | Use at API/helper boundaries | Gives compile-time separation without excessive component churn |
| Raw primitives | Allowed at I/O edges | JSON, CLI, env, DB, MCP, and route params start as primitives |

## Alternatives Rejected

### Patch Only The Old Plan

Rejected. The old plan has the right core idea but lacks current runtime context and the user's expanded semantic primitive goal.

### Add `go-i18n` Now

Rejected. Translation catalogs would increase blast radius and distract from the foundational contract work. This plan should make a later i18n pass easier.

### Create One Global `internal/core/shared/ids` Package

Rejected as the default. A global package could work for a small set of truly shared contracts, but making it the default would centralize unrelated domain vocabularies and risk import cycles.

### Type Every Primitive

Rejected. Over-typing local values creates churn without improving correctness. The plan prioritizes values that cross boundaries, appear in adjacent same-primitive parameters, or form user/agent-facing contracts.

### Replace Existing `data-testid`s

Rejected. Existing stable test IDs are already useful. The frontend work should add semantic state/error/status hooks, not churn all locators.

### Remove Existing English Fields

Rejected. Existing clients and UI behavior rely on `message`, `template`, and `args`. This migration adds stable IDs first.

## Resolved Open Questions

- Q: Is this only an i18n preparation?
  - A: No. It also improves compiler guarantees, local code meaning, and LLM-agent reliability.

- Q: Should aliases or defined types be used in Go?
  - A: Use defined types for high-risk values. Use aliases only as temporary compatibility bridges.

- Q: Should route paths, slugs, and JSON keys be message IDs?
  - A: No. They are not localization IDs. Route paths and slugs may still have semantic value types after parsing.

- Q: Should user-authored content be typed?
  - A: Not as stable contract IDs. User-authored content remains mutable content.

- Q: Should tests stop using text everywhere?
  - A: No. Tests should stop using English text as the oracle for protocol/status/error behavior. Accessibility and content tests can keep text assertions.

- Q: Should workspace-sync `Reason` and `Source` be retyped?
  - A: No. They already have defined types in `gitrevisions`; focus on validation issue codes, severity, and MCP/frontend conversion.

## Implementation Posture

The implementation should be test-driven and vertical:

- First prove shared error/message contracts.
- Then migrate one complete API plus MCP plus frontend path.
- Then roll the pattern through the remaining domains.
- Keep compatibility fields until a later i18n or API-cleanup plan removes them.
- Use repo scans to find risky English assertions, but manually classify every remaining hit.
