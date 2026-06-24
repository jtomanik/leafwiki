<!-- leafwiki
version: 1
page:
  id: go-i18n-rollout-decision-20260623
  title: Go i18n Rollout Decisions
  created_at: "2026-06-23T21:20:00Z"
  updated_at: "2026-06-23T21:20:00Z"
  creator_id: system
  last_author_id: system
tags:
  - plans
fields:
  type: decision
-->

# Go i18n Rollout Decisions

## Chosen Approach

Implement an English-only `go-i18n` rollout for LeafWiki's user/agent-facing CLI, API, and MCP messages by adding a shared Go localization layer, an extractable code-owned message registry, an embedded English catalog, and enforcement that keeps behavior tests pinned to stable IDs instead of prose.

## Key Decisions

1. Backend-originated messages are localized by backend code.
   - Reason: API, MCP, and CLI all originate in Go/backend surfaces; frontend translation would duplicate source of truth and leave CLI/MCP unsolved.

2. Scope includes CLI, API, and MCP user/agent-facing messages.
   - Reason: these are the contract surfaces users, scripts, agents, and tests consume.

3. English is the only runtime locale in this phase.
   - Reason: the user explicitly chose no locale resolution yet. The goal is catalog-backed English, not multi-language behavior.

4. Code remains the source of emitted IDs.
   - Reason: this preserves the typed-ID design and allows compiler/review pressure around message contracts.

5. The message registry must contain real `i18n.Message` literals.
   - Reason: local `goi18n extract` scans `i18n.Message` and `i18n.LocalizeConfig` composite literals, not LeafWiki-specific catalog structs.

6. `active.en.toml` is generated/checked from Go source and embedded at runtime.
   - Reason: this uses the standard `go-i18n` workflow while keeping code-owned IDs.

7. Existing `message`, `template`, and `args` compatibility fields remain.
   - Reason: current API/MCP/frontend clients and tests rely on those fields. Removing them is a separate API cleanup.

8. Existing positional `args` are bridged to named template data.
   - Reason: this avoids a large constructor migration. Catalog messages can use `{{.Arg0}}`, `{{.Arg1}}`, etc. Existing wire `args` remain unchanged.

9. Frontend API error rendering should trust backend-rendered `message`.
   - Reason: backend is the localization boundary. Frontend should carry `code/messageId` for tests and semantic attributes, not retranslate server messages by `template`.

10. Shell wrapper messages are included through a generated English shell catalog.
    - Reason: shell scripts are CLI/user surfaces, but they cannot directly use `go-i18n` without risking stdout/stderr behavior. A generated English shell catalog keeps runtime simple while keeping message text catalog-backed.

11. Semantic hygiene grows from typed IDs into i18n enforcement.
    - Reason: the existing analyzer is already the right place to prevent raw stable-contract literals and can flag common raw prose emissions in Go user-facing surfaces.

12. Behavior tests must stop asserting prose where IDs exist.
    - Reason: copy changes should not break behavior tests. Allowed prose assertions are renderer/catalog tests, backward compatibility tests, and user-authored content tests.

13. MCP remains a first-class agent-facing contract.
    - Reason: tool descriptions, tool responses, and `_meta.error` are consumed by coding agents and must retain stable semantic IDs.

## Alternatives Rejected

| Alternative | Rejected Because |
|---|---|
| Keep CLI out of scope | User explicitly included CLI. |
| Keep shell scripts as unmanaged English | It would leave a real CLI surface outside the catalog policy. |
| Runtime shell calls into `leafwiki` for every message | It risks stdout hygiene, startup failure loops, and poor behavior when the binary path is invalid. |
| Frontend owns backend API translation | It duplicates catalogs and does not solve MCP/CLI. |
| Use only a TOML catalog as source of truth | It breaks the code-owned ID requirement. |
| Migrate all templates to named domain args first | Too large and not needed for English-only rollout. |
| Add locale negotiation now | Explicitly out of scope and increases blast radius. |
| Ban all English strings in tests | Too blunt; it would catch user content, Markdown rendering, accessibility tests, and assertion messages. |

## Open Questions Resolved

- Q: Should the first rollout include all CLI/API/MCP messages?
  - A: Yes, with shell wrapper messages backed by a generated English shell catalog rather than runtime `go-i18n` calls.

- Q: Should the backend or frontend render API messages?
  - A: Backend.

- Q: Should catalog coverage be advisory or enforced?
  - A: Enforced by tests/checkers.

- Q: Should behavior tests assert rendered English?
  - A: No, except renderer/catalog compatibility, backward compatibility, and user-authored content.

- Q: Should we support non-English locales now?
  - A: No. English only.

## Implementation Strategy

Use a vertical rollout:

1. Create localization runtime and extractable message registry.
2. Prove one API error, one field validation error, one API success message, one MCP descriptor/result/error, and one CLI error through the new renderer.
3. Add catalog and semantic hygiene gates.
4. Expand by surface/domain.
5. Convert tests from prose to IDs/semantic attrs.
6. Document the contract and verification workflow.

This prevents a catalog-only migration that looks complete but leaves render paths or tests coupled to prose.
