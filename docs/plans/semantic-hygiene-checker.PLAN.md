<!-- leafwiki
version: 1
page:
  id: semantic-hygiene-checker-plan-20260622
  title: Semantic Hygiene Checker Implementation Plan
  created_at: "2026-06-22T13:23:32Z"
  updated_at: "2026-06-22T13:23:32Z"
  creator_id: system
  last_author_id: system
tags:
  - plans
fields:
  type: refactor
-->

# Semantic Hygiene Checker Implementation Plan

## Goal & Context

### Objective

Add a repo-local semantic hygiene checker that enforces the `semantic-types-and-ids` contract automatically during review. The checker must catch primitive leaks such as semantic `.String()` round trips into internal APIs, raw semantic parameters in service/use-case boundaries, direct casts outside parser edges, and raw stable contract IDs.

The checker is part of the current semantic-types review scope. It must pass repo-wide without a baseline.

### Context

- Workflow: `docs/plans/planning.aibasic.txt`.
- Parent plan: `docs/plans/semantic-types-and-ids.PLAN.md`.
- Policy doc: `docs/typed-ids.md`.
- Current shell oracle: `scripts/check-typed-id-oracles.sh`.
- Observation artifact: `docs/plans/semantic-hygiene-checker.OBSERVE.md`.
- Context artifact: `docs/plans/semantic-hygiene-checker.CONTEXT.md`.
- Decision artifact: `docs/plans/semantic-hygiene-checker.DECISION.md`.
- Implementation commands should follow `@/Users/jakubtomanik/.codex/RTK.md` and run through `rtk`.

### Decisions from Discussion

**Key Decisions:**

1. Build a small repo-local Go analyzer with `go/analysis`.
   - Reason: the recurring mistakes are structural Go code patterns that need AST and type information.

2. Keep this in the same PR review cycle.
   - Reason: the semantic-types work is not complete if reviewers cannot enforce the abstraction boundary.

3. Do not introduce `golangci-lint` in this slice.
   - Reason: the immediate need is the analyzer and rule policy; lint integration can follow later.

4. Use hygiene tests.
   - Reason: checker rules are review contract code and need regression tests.

5. Use Go policy tables rather than YAML in v1.
   - Reason: policy becomes compiled and testable with the analyzer.

6. Do not use a baseline.
   - Reason: the codebase should meet the semantic hygiene contract, not merely avoid additional violations.

7. Treat policy diffs as reviewer-controlled.
   - Reason: implementers may suggest rule changes, but the reviewer decides whether they preserve the plan's spirit.

8. Keep raw primitives at real I/O boundaries.
   - Reason: JSON, HTTP, MCP, CLI, env, DB, filesystem, logs, and fixtures naturally serialize to primitive values.

9. Refactor code to satisfy the analyzer instead of adding casts.
   - Reason: casts and `.String()` calls in middle layers are the abstraction leak this checker exists to prevent.

**Alternatives Considered:**

- Extend the shell oracle only.
  - Rejected because regex cannot distinguish serialization from a domain leak.

- Add YAML policy.
  - Rejected for v1 because a code-first policy is simpler and easier to test.

- Run only on changed files.
  - Rejected because the desired standard is repo-wide hygiene.

- Ban all semantic `.String()` calls.
  - Rejected because boundary serialization is legitimate.

## Summary

Implement `internal/analysis/semantichygiene`, expose it through `cmd/leafwiki-vet`, add `scripts/check-semantic-hygiene.sh`, and refactor current semantic-types code until the analyzer passes repo-wide.

The end state:

- Reviewers can run one command to detect semantic abstraction leaks.
- The checker has unit fixtures for forbidden and allowed patterns.
- The current semantic-types implementation passes without a baseline.
- `scripts/check-typed-id-oracles.sh` no longer carries the main Go semantic-boundary policy.
- `docs/typed-ids.md` documents the checker and reviewer-owned policy.

## Requirements

| ID | Requirement |
|---|---|
| R1 | Add a Go `go/analysis` analyzer for LeafWiki semantic hygiene. |
| R2 | Add a runner command under `cmd/leafwiki-vet`. |
| R3 | Add analyzer tests with `analysistest` fixtures for every hard-fail rule class. |
| R4 | Add a review script that runs the analyzer through `rtk` and fails on diagnostics. |
| R5 | Require repo-wide pass with no baseline or changed-file limitation. |
| R6 | Detect semantic `.String()` leaks into internal APIs, comparisons, and domain assignments. |
| R7 | Detect direct casts to semantic types outside approved parser, adapter, or fixture contexts. |
| R8 | Detect raw semantic-looking parameters and fields in internal service/use-case/domain boundaries. |
| R9 | Detect validators that return primitives after validating known semantic values. |
| R10 | Detect raw stable contract ID literals when typed constants or definitions exist. |
| R11 | Preserve allowed primitive escapes at serialization, persistence, logging, URL, config, fixture, and wire-format edges. |
| R12 | Update docs so reviewers know how to extend rules and how to review policy diffs. |
| R13 | Keep the existing shell oracle only as a compatibility wrapper around the semantic hygiene script. |

## Scope Boundaries

In scope:

- Go analyzer package and tests.
- Thin Go runner.
- Shell script integration.
- Docs updates.
- Current semantic-types implementation fixes needed to pass the checker.

Out of scope:

- `golangci-lint` integration.
- Auto-fixes.
- TypeScript analyzer.
- Broad SSA or taint analysis.
- Global ID package redesign.
- Translation runtime or `go-i18n`.
- Changing public JSON/MCP formats beyond existing semantic-types contract work.

## Assumptions

- `golang.org/x/tools v0.44.0` can be promoted from indirect to direct dependency.
- The analyzer can start with local AST/type rules and explicit allow categories.
- Some current code must change to make internal APIs accept semantic types rather than raw strings.
- Reviewer policy ownership is enforced through review instructions and diff review, not CODEOWNERS.

## Impact Analysis

| Area | Impact | Notes |
|---|---|---|
| `internal/analysis/semantichygiene` | New package | Analyzer implementation, policy tables, fixtures |
| `cmd/leafwiki-vet` | New command | Thin runner for review checks |
| `scripts/check-semantic-hygiene.sh` | New script | Standard reviewer entrypoint |
| `scripts/check-typed-id-oracles.sh` | Update | Compatibility wrapper only; not a separate reviewer command |
| `docs/typed-ids.md` | Update | Document checker, policy ownership, and allowed boundary escapes |
| Domain/service APIs | Refactor | Replace raw-string semantic APIs with typed APIs where diagnostics require |
| Tests | Add/update | Analyzer fixtures plus affected domain regression tests |
| CI/manual verification | Update docs | No CI integration required unless existing workflow scripts already include the check |

## Architecture & Design

### Module Structure

```text
internal/analysis/semantichygiene/
  analyzer.go          # analysis.Analyzer definition and rule orchestration
  diagnostics.go       # diagnostic IDs and messages
  policy.go            # reviewer-owned semantic policy tables
  rules_string.go      # .String(), comparison, and assignment rules
  rules_cast.go        # direct semantic cast rules
  rules_signature.go   # raw semantic parameter/field/validator rules
  rules_literal.go     # raw stable contract literal rules
  analyzer_test.go     # analysistest fixture runner
  testdata/src/...     # positive and negative fixtures

cmd/leafwiki-vet/
  main.go              # singlechecker or multichecker entrypoint

scripts/
  check-semantic-hygiene.sh
```

`singlechecker` is enough if `leafwiki-vet` contains only this analyzer. If a second repo-local analyzer is added later, switch the runner to `multichecker`.

### Dependency Graph

```text
scripts/check-semantic-hygiene.sh
  -> temporary go.work including repo root and e2e-proxy
  -> rtk go run $repo_root/cmd/leafwiki-vet $repo_root/internal/... $repo_root/cmd/... $repo_root/e2e/... $repo_root/e2e-proxy/...
      -> cmd/leafwiki-vet
          -> internal/analysis/semantichygiene
              -> golang.org/x/tools/go/analysis
              -> golang.org/x/tools/go/analysis/passes/inspect
```

Runtime code must not import the analyzer.

### Policy Shape

Use Go structs in `policy.go`, not YAML:

```text
Semantic types:
  package path + type name + category

Allowed boundaries:
  package/function/file/context predicates

Forbidden sinks:
  internal domain/service/helper calls where raw strings should not enter

Contract literals:
  known error/message/tool/issue IDs that must be constants
```

Policy code should be boring data plus small predicates. Complex rule logic belongs in rule files and must be covered by fixtures.

### Diagnostic Principles

Diagnostics should be explicit and corrective:

- "semantic value tree.PageID converted to string before internal call getNodeByIDLocked; make the callee accept tree.PageID"
- "direct cast to tree.RoutePath outside approved parser or edge adapter"
- "field PageID in domain struct uses string; use tree.PageID or mark as DTO boundary"
- "validator ValidateRoutePath returns string after validation; return tree.RoutePath"

Diagnostics should not suggest adding `tree.PageID(raw)` unless the location is an approved parser boundary.

### Allowed Edge Model

Allowed edge contexts should be narrow:

- JSON struct literals and response encoders.
- MCP request/response payloads.
- HTTP path/query/form extraction before parse.
- CLI/env/config parse functions.
- Database row structs and persistence adapters.
- Filesystem/frontmatter/Markdown serialization.
- URL path construction.
- Logging and error text.
- Tests asserting wire output, persisted output, visible copy, or fixture contents.

Allowed does not mean globally ignored. A DTO field can be raw, but code that reuses the DTO field in domain logic should parse it before use.

## Implementation

### U1 - Add Analyzer Skeleton And Tests

Create the analyzer package, runner, and fixture harness.

Files:

- `internal/analysis/semantichygiene/analyzer.go`
- `internal/analysis/semantichygiene/diagnostics.go`
- `internal/analysis/semantichygiene/policy.go`
- `internal/analysis/semantichygiene/analyzer_test.go`
- `internal/analysis/semantichygiene/testdata/src/...`
- `cmd/leafwiki-vet/main.go`
- `go.mod`
- `go.sum` if dependency metadata changes

Acceptance:

- `rtk go test ./internal/analysis/semantichygiene` runs.
- A trivial fixture diagnostic fails before the skeleton rule exists and passes after the rule is implemented.
- Runner smoke test only: `rtk go run ./cmd/leafwiki-vet ./internal/analysis/semantichygiene` runs without loading runtime code. Reviewers still use `rtk bash scripts/check-semantic-hygiene.sh` for semantic hygiene coverage.

### U2 - Implement Semantic `.String()` Leak Rules

Detect semantic `.String()` calls used as internal-domain arguments, equality operands, and semantic-looking domain assignments.

Forbidden examples:

- `getNodeByIDLocked(currentID.String())`
- `FindPageByID(in.PageID.String())`
- `if e.ID == id.String()`
- `node.Metadata.LastAuthorID = userID.String()`

Allowed examples:

- JSON response fields.
- MCP payload fields.
- URL construction.
- Log fields and error strings.
- Tests asserting serialized output.

Expected implementation fixes:

- Change internal callees to accept semantic types.
- Add typed comparison helpers or methods where Go operators are not enough.
- Add typed setters for domain metadata when direct fields are storage-shaped.

Acceptance:

- Fixtures include forbidden and allowed `.String()` examples.
- Real repo diagnostics in this category are fixed without adding broad allowlists.

### U3 - Implement Direct Cast And Parser Boundary Rules

Detect direct casts to semantic types outside approved constructors, parsers, edge adapters, and test fixtures.

Forbidden examples:

- `tree.PageID(raw)` in middle-layer use cases.
- `workspaceid.WorkspaceID(raw)` after only trimming or copying.
- `tree.RoutePath(raw)` where `ValidateSemanticRoutePath` or equivalent should be used.

Allowed examples:

- Parser/constructor implementation internals.
- HTTP/MCP/CLI adapters immediately after validation.
- Test fixtures constructing known-valid semantic values.

Expected implementation fixes:

- Introduce or use parser functions that return semantic values.
- Rename validators that return semantic values to make the contract clear.
- Avoid validating into `string` and recasting later.

Acceptance:

- Fixtures prove both approved parser casts and forbidden casts.
- Current semantic implementation contains no unapproved direct casts.

### U4 - Implement Raw Signature, Field, And Validator Rules

Detect raw semantic-looking fields and parameters in internal service/use-case/domain code.

Hard-fail targets:

- Function parameters named `id`, `pageID`, `workspaceID`, `revisionID`, `routePath`, `slug`, `assetName`, `filename`, `sourcePath`, `userID`, `apiKeyID`, or close variants where policy maps the name to a semantic type.
- `[]string` or `map[string]...` carrying semantic IDs.
- Struct fields with semantic names in domain structs outside DTO/storage/wire boundaries.
- Validators for known semantic values that return `string`.

Allowed targets:

- HTTP request DTOs.
- JSON/MCP DTOs.
- Storage/frontmatter structs.
- Test fixtures.

Expected implementation fixes:

- Change service and use-case APIs to semantic types.
- Change validators to return semantic types.
- Add DTO-to-domain conversion functions where needed.

Acceptance:

- Fixtures include service boundary failures, DTO allowed cases, and validator return failures.
- Real repo pass is achieved without a baseline.

### U5 - Implement Stable Contract Literal Rules

Detect raw stable contract IDs in production code where typed constants or error definitions should be used.

Targets:

- Error codes.
- Message IDs.
- Field validation codes.
- Markdown validation issue codes.
- MCP tool IDs.
- Success message IDs.

Allowed:

- Constant declarations themselves.
- JSON fixture files.
- Tests asserting wire values.
- Docs.

Expected implementation fixes:

- Add or reuse typed constants.
- Update constructors and response helpers to accept typed ID values.

Acceptance:

- Fixtures include raw literal failure and constant declaration allowed case.
- Existing repeated literals are replaced by typed constants.

### U6 - Integrate Review Script And Existing Oracle

Add the reviewer entrypoint and update the shell oracle relationship.

Files:

- `scripts/check-semantic-hygiene.sh`
- `scripts/check-typed-id-oracles.sh`
- `scripts/README.md` if script conventions need documentation

Required behavior:

- `scripts/check-semantic-hygiene.sh` runs the Go analyzer through `rtk`.
- It exits non-zero on analyzer diagnostics.
- It prints enough context for implementers to fix the first failure class.
- `scripts/check-typed-id-oracles.sh` delegates to the new script for compatibility with old callers.
- Reviewers use `scripts/check-semantic-hygiene.sh` only.

Acceptance:

- `rtk bash scripts/check-semantic-hygiene.sh` passes.
- `scripts/check-typed-id-oracles.sh` no longer duplicates main Go semantic-boundary policy.

### U7 - Refactor Current Semantic Implementation To Pass

Run the checker against the repo and fix every diagnostic by improving semantic abstractions.

Expected refactor patterns:

- Internal lookup helpers take typed IDs.
- Service/use-case APIs accept semantic values.
- Domain structs expose typed methods or typed fields where they carry semantic meaning.
- DTO/storage structs stay raw only at boundaries.
- Validators return semantic types.
- Comparison logic uses typed helpers or methods.
- Assignment logic uses typed setters or typed fields.

Do not:

- Add a baseline file.
- Add a broad package allowlist.
- Replace diagnostics with casts.
- Move string leaks to helper functions to hide them.

Acceptance:

- Repo-wide checker pass.
- Existing semantic behavior tests still pass.
- New focused tests cover any public behavior touched during refactor.

### U8 - Document Reviewer Policy And Close The Loop

Update docs so reviewers and implementers know how to use and extend the checker.

Files:

- `docs/typed-ids.md`
- `docs/plans/semantic-types-and-ids.PLAN.md` if the parent plan's Definition of Done needs to name the checker
- `docs/plans/semantic-hygiene-checker.PLAN.md` only if implementation discovers a plan correction

Required documentation:

- How to run the checker.
- What the checker enforces.
- How policy changes are reviewed.
- What counts as an allowed primitive boundary.
- Why adding casts or `.String()` wrappers is not a valid fix for middle-layer diagnostics.

Acceptance:

- Docs match implemented command names and file paths.
- Reviewer-owned policy language is explicit.

## Test Specifications

### Analyzer Unit Fixtures

```gherkin
Feature: semantic String escapes
  Scenario: semantic ID passed to internal raw-string lookup
    Given a variable of type tree.PageID
    When code calls an internal helper with pageID.String()
    Then semantichygiene reports a diagnostic telling the callee to accept tree.PageID

  Scenario: semantic ID serialized to JSON output
    Given a variable of type tree.PageID
    When code assigns pageID.String() to a JSON response field
    Then semantichygiene does not report a diagnostic
```

```gherkin
Feature: direct semantic casts
  Scenario: raw route path cast in domain code
    Given a raw string in an internal use case
    When code casts it with tree.RoutePath(raw)
    Then semantichygiene reports a diagnostic requiring a parser or typed input

  Scenario: parser constructs a semantic route path
    Given a parser function validates a route path
    When the parser returns tree.RoutePath(value)
    Then semantichygiene does not report a diagnostic
```

```gherkin
Feature: raw semantic signatures
  Scenario: service method accepts page ID as string
    Given an internal service method has a parameter named pageID
    When the parameter type is string
    Then semantichygiene reports a diagnostic requiring tree.PageID

  Scenario: wire request DTO accepts page ID as string
    Given an HTTP request DTO has a JSON pageId field
    When the field type is string
    Then semantichygiene does not report a diagnostic
```

```gherkin
Feature: validator return types
  Scenario: route validator returns raw string
    Given a function named ValidateRoutePath validates route paths
    When it returns string
    Then semantichygiene reports a diagnostic requiring tree.RoutePath or a clearly named parser return
```

```gherkin
Feature: stable contract literals
  Scenario: production code emits raw error code literal
    Given a typed error code constant exists
    When production code passes the raw string literal to a response helper
    Then semantichygiene reports a diagnostic requiring the typed constant

  Scenario: test asserts wire error code
    Given a test decodes JSON output
    When the test compares the wire code string
    Then semantichygiene does not report a diagnostic
```

### Integration Tests

```gherkin
Feature: repo-wide semantic hygiene
  Scenario: reviewer runs semantic hygiene script
    Given the repository checkout includes the semantic-types implementation
    When the reviewer runs rtk bash scripts/check-semantic-hygiene.sh
    Then the command exits successfully with no semantic diagnostics
```

## Verification

Required commands:

```bash
rtk go test ./internal/analysis/semantichygiene
rtk bash scripts/check-semantic-hygiene.sh
rtk go test ./...
rtk bash scripts/test-run.sh
rtk proxy git diff --check
rtk proxy git diff --cached --check
```

Run additional focused tests for any domain package refactored to satisfy diagnostics. If frontend or E2E files are changed while narrowing text assertions, also run:

```bash
rtk npm --prefix ui/leafwiki-ui run test
rtk npm --prefix ui/leafwiki-ui run lint
rtk npm --prefix e2e run lint
```

## Definition Of Done

- `internal/analysis/semantichygiene` exists and has fixture coverage for every hard-fail rule category.
- `cmd/leafwiki-vet` runs the analyzer.
- `scripts/check-semantic-hygiene.sh` is the standard reviewer entrypoint.
- The checker passes repo-wide with no baseline.
- Current semantic-types code has no remaining unchecked middle-layer semantic leaks in the categories covered by this plan.
- Any allow category is narrow, documented, and covered by both allowed and forbidden fixtures.
- `scripts/check-typed-id-oracles.sh` is a compatibility wrapper only and is not a separate semantic review command.
- `docs/typed-ids.md` documents semantic hygiene policy, reviewer ownership, and the command to run.
- The verification commands above pass.

## Reviewer Checklist

Before accepting the implementation:

- Inspect every diff under `internal/analysis/semantichygiene` before trusting a green checker result.
- Reject broad package or directory allowlists unless the plan is explicitly updated.
- Reject fixes that add direct casts or `.String()` wrappers in middle-layer code.
- Check that each newly discovered semantic leak class either has an analyzer rule or a documented reason it cannot be automated yet.
- Confirm the parent semantic-types plan's Definition of Done now includes the checker where appropriate.
