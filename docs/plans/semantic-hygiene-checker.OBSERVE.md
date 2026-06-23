<!-- leafwiki
version: 1
page:
  id: semantic-hygiene-checker-observe-20260622
  title: Semantic Hygiene Checker - Observe
  created_at: "2026-06-22T13:23:32Z"
  updated_at: "2026-06-22T13:23:32Z"
  creator_id: system
  last_author_id: system
tags:
  - plans
  - observe
fields:
  type: refactor
-->

# Semantic Hygiene Checker - Observe

## Purpose

This document captures the facts behind a follow-on implementation plan for a repo-local semantic hygiene checker. The checker exists to make the `semantic-types-and-ids` contract enforceable during review, especially after repeated review findings showed that manual inspection alone misses primitive leaks.

The plan title is `semantic-hygiene-checker`.

## Planning Input

The user accepted the direction of introducing an automatic reviewer-owned checker and clarified the operating model:

- The checker must live in this repository.
- The checker is part of the same semantic-types PR review cycle, not a separate later cleanup.
- It should be a small Go `go/analysis` analyzer rather than `golangci-lint` in this slice.
- `golangci-lint` integration can be a separate future ticket.
- Hygiene tests are acceptable and desired.
- The checker must not rely on a baseline or "new code only" mode. The repo should pass the semantic hygiene policy.
- Policy updates may be suggested by implementers, but the reviewer controls whether rules are changed, weakened, or extended.
- Review instructions are enough for policy ownership for now.

## Workflow Evidence

This plan follows `docs/plans/planning.aibasic.txt` and creates the expected OODA planning artifacts:

- `docs/plans/semantic-hygiene-checker.OBSERVE.md`
- `docs/plans/semantic-hygiene-checker.CONTEXT.md`
- `docs/plans/semantic-hygiene-checker.DECISION.md`
- `docs/plans/semantic-hygiene-checker.PLAN.md`
- `docs/plans/semantic-hygiene-checker.planning.aibasic.json`

Four read-only explorer slices were used:

- Codebase tooling patterns, scripts, command layout, and Go module constraints.
- Semantic leak taxonomy from the current semantic-types implementation and review loop.
- Docs and OODA artifact conventions.
- Go `go/analysis` design, analyzer testing, and runner integration.

## Source Inputs

- `docs/plans/planning.aibasic.txt`
- `docs/plans/semantic-types-and-ids.OBSERVE.md`
- `docs/plans/semantic-types-and-ids.CONTEXT.md`
- `docs/plans/semantic-types-and-ids.DECISION.md`
- `docs/plans/semantic-types-and-ids.PLAN.md`
- `docs/plans/semantic-types-and-ids.SCAN.md`
- `docs/typed-ids.md`
- `scripts/check-typed-id-oracles.sh`
- `scripts/README.md`
- `go.mod`
- `cmd/leafwiki/main.go`
- `cmd/leafwiki/main_test.go`
- `internal/core/tree/semantic_types.go`
- `internal/core/tree/tree_service.go`
- `internal/wiki/assets/use_cases.go`

## Current Repo Facts

`go.mod` declares module `github.com/perber/wiki`, uses Go `1.25.4`, and already includes `golang.org/x/tools v0.44.0` indirectly. Implementing a `go/analysis` checker will make that dependency direct.

The repo already has script-based review checks. `scripts/check-typed-id-oracles.sh` currently acts as a grep/awk oracle for typed-ID drift, English contract assertions, and selected stringly boundary patterns. It is useful as a seed, but it is not type-aware and cannot reliably distinguish a DTO edge from a domain leak.

The repo has no existing `analysistest` convention. A local analyzer should use the standard Go fixture layout under its own package, for example:

```text
internal/analysis/semantichygiene/
  analyzer.go
  analyzer_test.go
  policy.go
  testdata/src/...
```

The production binary lives under `cmd/leafwiki`. E2E helper binaries live under `e2e/cmd`. A hygiene checker should not be hidden inside either runtime area. A thin `cmd/leafwiki-vet` runner matches the repo better and leaves room for future repo-local analyzers.

## Current Semantic Contract

`docs/typed-ids.md` and the semantic-types plan define the core contract:

- Raw primitives are acceptable at I/O boundaries.
- Once a value has validated or assigned domain meaning, internal code should carry that meaning in the type.
- Machine contracts such as error codes, message IDs, MCP tool IDs, workspace IDs, page IDs, route paths, asset names, issue codes, and runtime states are not arbitrary strings.
- The purpose is not to wrap every primitive. The purpose is to prevent adjacent same-shaped values from being mixed and to make the code easier for humans and coding agents to modify correctly.

## Observed Leak Categories

The review loop exposed recurring classes of mistakes that a type-aware analyzer can flag:

1. Raw semantic parameters after a boundary, such as service/use-case methods accepting `string` where the name and call graph indicate `PageID`, `WorkspaceID`, `RoutePath`, `Slug`, or `AssetName`.

2. Validators that return a raw primitive after validation, forcing later code to cast back into a semantic type.

3. Direct semantic casts from raw input, such as `tree.PageID(raw)` or `workspaceid.WorkspaceID(raw)`, outside parser/constructor or edge adapter functions.

4. Semantic `.String()` round trips into internal helpers or services, such as `getNodeByIDLocked(currentID.String())`, where the callee should accept the semantic value.

5. Comparisons and assignments that expose representation details, such as `if e.ID == id.String()` or `node.Metadata.LastAuthorID = userID.String()`, when the domain operation is identity comparison or metadata assignment.

6. Adjacent semantic siblings where one value was typed but a nearby same-shaped value stayed raw.

7. DTO, JSON, storage, and metadata strings leaking back into domain logic without parsing.

8. Sentinel literals such as `"root"` or `""` in semantic-ID logic where typed constants or constructors should centralize meaning.

9. Stable contract IDs emitted as raw string literals when typed constants exist or should exist.

10. English text assertions used as machine-contract tests.

## Analyzer Fit

The Go `go/analysis` package is a good fit because it provides:

- AST traversal.
- Type information through `pass.TypesInfo`.
- Standard diagnostics through `pass.Report`.
- Test fixtures through `analysistest`.
- Simple CLI runners through `singlechecker` or `multichecker`.

The analyzer should be diagnostic-only in the first version. Replacing a `string` with a semantic type often requires introducing a parser, constructor, or domain method. That is not safe to auto-fix.

## Constraints

- The implementation must not fix issues by weakening policy.
- There is no baseline exemption file for legacy debt.
- False positives must be resolved by improving edge classification or by moving code to a clearer boundary, not by globally silencing a package.
- The checker must be usable by reviewers during an active review loop.
- Existing behavior and wire formats must remain compatible.
- The old shell oracle may remain for English assertion checks and transitional output, but semantic boundary policy should move into the type-aware analyzer.

## Risks

- A purely name-based rule will be noisy. The analyzer must combine type information, package classification, function context, and explicit allow categories.
- A too-permissive allowlist will recreate the current manual-review problem.
- A too-broad "ban `.String()`" rule will block legitimate serialization, logging, URL construction, and test assertions.
- The current semantic-types implementation may need additional refactoring to satisfy a repo-wide no-baseline checker.
- Implementers may attempt to pass the checker by moving strings around. Tests must cover rule intent, not just current examples.
