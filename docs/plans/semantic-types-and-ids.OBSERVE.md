<!-- leafwiki
version: 1
page:
  id: semantic-types-ids-observe-20260622
  title: Semantic Types And IDs - Observe
  created_at: "2026-06-21T22:19:24Z"
  updated_at: "2026-06-21T22:19:24Z"
  creator_id: system
  last_author_id: system
tags:
  - plans
  - observe
fields:
  type: refactor
-->

# Semantic Types And IDs - Observe

## Purpose

This document captures current facts behind the implementation plan for replacing high-value primitive strings, numbers, and time values with semantic types and stable IDs. The plan supersedes `docs/plans/semantic-ids.PLAN.md` while preserving its original direction: introduce typed, stable identifiers before any later `go-i18n` migration.

## Planning Input

The user wants LeafWiki to make the codebase easier for both humans and coding agents to modify correctly by reducing "primitive ambiguity." The concrete goal is broader than the old typed stable IDs plan:

- Keep stable contract IDs separate from rendered English text.
- Add semantic type aliases or defined types for common string, numeric, and date/time inputs.
- Give LLM coding agents stronger local clues about what a value means, not just that it is a `string`, `int`, or `time.Time`.
- Preserve current product behavior and current English output unless a test is deliberately changed to assert stable IDs instead of copy.
- Avoid overbuilding a universal ID framework or trying to type every local primitive.

The assumed plan title is `semantic-types-and-ids`.

## Workflow Evidence

The plan follows `docs/plans/planning.aibasic.txt` and creates the expected observe, context, decision, plan, and workflow-state artifacts:

- `docs/plans/semantic-types-and-ids.OBSERVE.md`
- `docs/plans/semantic-types-and-ids.CONTEXT.md`
- `docs/plans/semantic-types-and-ids.DECISION.md`
- `docs/plans/semantic-types-and-ids.PLAN.md`
- `docs/plans/semantic-types-and-ids.planning.aibasic.json`

Four read-only explorer slices were run:

- Backend/core/API semantic stable IDs and primitive type candidates.
- MCP, agent, presence, workspace routing, and federated runtime surfaces.
- Frontend, TypeScript branded type candidates, and E2E string dependency risks.
- Historical plan, original conversation context, recent commits, and workflow posture.

## Original Plan And Conversation Facts

`docs/plans/semantic-ids.PLAN.md` already contains the core split this plan keeps:

- Contract IDs: error codes, validation codes, MCP tool IDs, provider/event/mode/source/status IDs.
- Message IDs: stable future-localization handles.
- Rendered text: current English output, retained as default/compatibility output.

The initial conversation reference remains `codex://threads/019ea755-a3e3-7571-a8cf-c70a33fea656`. In that conversation, the user first discussed extracting user/agent-facing messages with `nicksnyder/go-i18n`, then narrowed the work to typed/static IDs first. The old plan was created for that narrower goal.

The old plan is now stale because it predates current federated runtime work:

- `2bdd2e05` added the original semantic IDs plan.
- `9ab75bad` moved planning workflow artifacts under `docs/plans`.
- `e7939650` and `8b8facfe` introduced or expanded the `wikid`/`frontd`/`workspaced` split.
- `8ba4ee88` removed legacy runtime/sync/revision paths and established the current federated runtime contract.

## Current Backend Error Contract

Shared errors are still stringly:

- `internal/core/shared/errors/localized_error.go` defines `LocalizedError.Code`, `Message`, `Template`, and `Args` as plain strings.
- `NewLocalizedError` accepts raw strings.
- `internal/core/shared/errors/field_error.go` defines `FieldError` with only `field` and `message`.
- `ValidationErrors.Add` accepts raw field/message strings and has no stable field-level code or message ID.

Wiki domain responders already expose structured `code/message/template/args`, but each domain duplicates its shape:

- `internal/wiki/pages/errors.go`
- `internal/wiki/auth/errors.go`
- `internal/wiki/assets/errors.go`
- `internal/wiki/branding/errors.go`
- `internal/wiki/importer/errors.go`
- `internal/wiki/revisions/errors.go`
- `internal/wiki/search/errors.go`
- `internal/wiki/tags/errors.go`
- `internal/wiki/properties/errors.go`
- `internal/wiki/links/errors.go`

Observation: the codebase already has many stable error code constants, but the type system cannot distinguish an error code from any other string. It also lacks a `MessageID` field in shared and frontend API error details.

## Current Typed Pockets To Reuse

The repo already uses defined string or int types in several domains. These should guide the style instead of inventing a monolithic replacement:

- `internal/core/tree/page_node.go`: `NodeKind`.
- `internal/core/revision/types.go`: `RevisionType`.
- `internal/workspacesync/gitrevisions/store.go`: `Reason` and `Source`.
- `internal/projectdaemon/roles.go`: `RoleName` and `RoleState`.
- `internal/wikid/grants.go`: `GrantRole`.
- `internal/wikid/workspace_supervisor.go`: `WorkspaceState`.
- `internal/importer/planner.go`: `PlanAction`.
- `internal/importer/executor.go`: `ExecutionAction`.
- `internal/importer/plan_store.go`: `ExecutionStatus`.
- `internal/core/markdownlinks/markdownlinks.go`: `EntryKind` and `TargetKind`.
- `internal/links/link_refactor.go`: `MarkdownSourceKind`.

Observation: the existing local pattern is domain-owned defined types near the code that owns the vocabulary.

## Current Primitive Ambiguity Hotspots

High-risk backend string clusters:

- Page mutation methods take adjacent `userID`, `id`, `title`, `slug`, `content`, and `expectedVersion` primitives.
- `VersionUnchecked` is a raw string sentinel used as an internal bypass token.
- Workspace IDs are validated by `internal/workspaceid/validate.go`, but the validated value remains a plain string.
- `wiki.Workspace.ID`, actor contexts, descriptors, frontd routes, workspaced auth, and MCP session bindings pass workspace IDs as raw strings.
- Route paths, Markdown paths, workspace source paths, slugs, asset names, and content paths are all plain strings across validators and mappers.
- Search offset/limit, revision list limits, upload byte sizes, ports, and timeout settings are plain numeric values at several boundaries.
- Some timestamps are `time.Time` internally but serialized as strings in MCP context and descriptor surfaces.

Observation: the most valuable semantic types are cross-boundary values and adjacent parameters with the same primitive type.

## Current MCP And Agent Contracts

MCP tool names are centralized but not typed:

- `internal/wiki/mcp/tool_descriptors.go` defines `Tool*` constants as plain strings.
- `ToolDescriptor` has `Name string` and `Description string`.
- `internal/wiki/mcp/schema.go` declares message-only success schemas with only `message`.
- `internal/wiki/mcp/types.go` defines `messageOutput` with only `Message string`.

MCP context and agent presence still expose several semantic values as strings or loose values:

- `syncMode`, validation `severity` and `code`.
- Recent-change `source` and `reason` after conversion.
- Recommended tool names.
- Presence status, type, mode, source, state, and last event.
- Agent hook `Provider` has constants, but event names, sources, and tool names are stringly.

Observation: MCP is an agent-facing protocol. Stable semantic IDs there are not only for future localization; they directly improve agent reliability and test quality.

## Current Runtime Boundary Contracts

The current runtime split is:

- `wikid` owns registry, auth, grants, supervision, and global runtime state.
- `frontd` owns public ingress and workspace routing.
- `workspaced` owns workspace services.
- `projectdaemon` still owns descriptor/control primitives and STDIO attach behavior.

Relevant current gaps:

- Frontd/workspaced/projectdaemon routing errors often return English-only bodies.
- Workspace IDs are checked but not represented as a type once validated.
- Descriptor config surfaces serialize host, port, URLs, timeouts, and workspace identity with primitives.
- Existing role/state types should be reused rather than retyped.

Observation: the new plan must include federated runtime boundaries that the old plan did not know about.

## Current Frontend And E2E Contracts

Frontend API types are mostly plain strings:

- `ui/leafwiki-ui/src/lib/api/pages.ts`: page IDs, versions, workspace IDs.
- `ui/leafwiki-ui/src/lib/api/revisions.ts`: revision IDs, page IDs, workspace IDs.
- `ui/leafwiki-ui/src/lib/api/users.ts`: user IDs and MCP API key IDs.
- `ui/leafwiki-ui/src/lib/api/workspaces.ts`: workspace IDs and workspace state.
- `ui/leafwiki-ui/src/lib/api/workspaceSync.ts`: validation code/severity/message shapes.
- `ui/leafwiki-ui/src/lib/api/errors.ts`: API error `code` without `messageId`.

Some frontend types already exist:

- `PresenceMode`.
- `AppMode`.
- `ImportExecutionStatus`.
- `PageRefactorKind`.
- `WikiNodeKind`.
- `HistoryTab`.

Stable `data-testid` locators are widespread but not semantic enough for state/error contracts:

- Dialog buttons and tree/search/workspace rows have stable test IDs.
- There are effectively no `data-error-code`, `data-l10n-id`, `data-validation-code`, or `data-import-status` hooks.
- Workspace sync banners, frontmatter errors, MCP API key empty/error states, importer statuses, and history change labels still rely heavily on English copy in tests.

Observation: frontend work should preserve existing `data-testid`s and add semantic state/error/message attributes where tests currently assert prose or where a primitive string carries domain meaning.

## Text-Assertion Risk

Scans found common copy-bound assertions:

- E2E tests use `getByText`, `getByRole({ name })`, `hasText`, `toHaveText`, and `toContainText`.
- MCP integration tests often assert error strings contain expected fragments.
- Go tests use `strings.Contains(err.Error(), ...)` across CLI, runtime, sync, registry, and middleware tests.
- Frontend code reads `error.message` and maps API errors to copy without message IDs.

Observation: not every text assertion is wrong. User-authored page content, accessibility behavior, Markdown rendering, and visible-copy tests may keep text assertions. Error/status/protocol tests should prefer stable IDs.

## Current Workflow State

`docs/plans/planning.aibasic.txt` is currently modified in the working tree before this task. This planning run must not overwrite or revert that user/workflow change.
