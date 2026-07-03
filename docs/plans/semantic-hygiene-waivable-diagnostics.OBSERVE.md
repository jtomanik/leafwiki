<!-- leafwiki
version: 1
page:
  id: semantic-hygiene-waivable-diagnostics-observe-20260701
  title: Semantic Hygiene Waivable Diagnostics - Observe
  created_at: "2026-07-01T00:00:00Z"
  updated_at: "2026-07-01T00:00:00Z"
  creator_id: system
  last_author_id: system
tags:
  - plans
  - observe
fields:
  type: refactor
-->

# Semantic Hygiene Waivable Diagnostics - Observe

## Purpose

This document captures the facts behind a follow-up implementation plan for adding waivable diagnostics to LeafWiki's semantic hygiene checker.

The plan title is `semantic-hygiene-waivable-diagnostics`.

## Planning Input

This plan follows `docs/plans/planning.aibasic.txt` and creates the expected OODA planning artifacts:

- `docs/plans/semantic-hygiene-waivable-diagnostics.OBSERVE.md`
- `docs/plans/semantic-hygiene-waivable-diagnostics.CONTEXT.md`
- `docs/plans/semantic-hygiene-waivable-diagnostics.DECISION.md`
- `docs/plans/semantic-hygiene-waivable-diagnostics.PLAN.md`
- `docs/plans/semantic-hygiene-waivable-diagnostics.planning.aibasic.json`

The current discussion thread is `019f1c68-23fd-7bf3-8825-53ecb49465f3`.

The originating source thread is `019f1527-f284-7ed3-8b91-072d53850114`.

## Source Thread Evidence

The source thread created this thread after the user asked to keep the waivable-diagnostics design separate from the broader semantic-checker coverage discussion.

The source thread established these relevant points:

- The checker is not a general linter replacement.
- It is a repo-specific semantic quality gate for code standards normal linters cannot know.
- The checker should protect raw primitive discipline, i18n message discipline, semantic test oracles, Ginkgo/Gomega safety, and BDD specs as behavior documentation.
- Advisory warnings are the wrong model because non-failing warnings will be ignored by agents.
- Budgeted, rule-specific waivers are the preferred model for judgment-heavy rules.
- True semantic leaks and dangerous test patterns should remain hard failures.

Source thread link: `codex://threads/019f1527-f284-7ed3-8b91-072d53850114`.

Current thread link: `codex://threads/019f1c68-23fd-7bf3-8825-53ecb49465f3`.

## Current Checker Shape

The checker lives in `internal/analysis/semantichygiene`.

The analyzer is a single `go/analysis` analyzer exposed through `cmd/leafwiki-vet`.

`scripts/check-semantic-hygiene.sh` runs `cmd/leafwiki-vet` against:

- `internal/...`
- `cmd/...`
- `e2e/...`
- `e2e-proxy/...`

The analyzer currently walks these AST node kinds:

- `AssignStmt`
- `CallExpr`
- `FuncDecl`
- `GoStmt`
- `ImportSpec`
- `KeyValueExpr`
- `TypeSpec`
- `UnaryExpr`
- `BasicLit`

Current rules report diagnostics directly with `ctx.pass.Reportf`.

There is no structured diagnostic type, no stable rule ID registry, no severity model, and no waiver parser.

## Current Rule Families

The current checker already covers these hard semantic and test-quality categories:

- Semantic `.String()` leaks before internal calls, assignments, comparisons, returns, and composite literals.
- Direct casts to semantic types outside approved parser, owner-adapter, or fixture-builder contexts.
- Unchecked semantic constructors in ordinary internal code.
- Raw semantic-looking function parameters and struct fields.
- Raw numeric semantic primitives such as pagination offsets, limits, tree depth, and byte limits.
- Message-bearing structs and payloads that expose rendered prose without stable message IDs.
- Raw stable contract literals.
- Raw localized prose in Go contract code and test assertion code.
- Weak Gomega assertions around errors, strings, lengths, HTTP status/body/header, structured payloads, time, repeated fields, and positional collections.
- Ginkgo construction-time purity, focus/pending/flake decorators, async context, goroutine recovery, blocking channel receives, helper reporting, reusable assertion helpers, and global state cleanup.
- `e2e-proxy` dependency direction.

## Existing Tests and Fixtures

Analyzer fixture tests are run from `internal/analysis/semantichygiene/analyzer_test.go` with `analysistest`.

The central fixture package is `internal/analysis/semantichygiene/testdata/src/github.com/perber/wiki/internal/analysis/semantichygiene/testdata/repotests`.

Focused helper tests live in `internal/analysis/semantichygiene/rule_edges_gomega_test.go` and exercise internal predicates and edge behavior directly.

The current fixture style uses `// want` comments that match diagnostic text.

Adding stable rule IDs will affect fixture expectations because diagnostic text should include the rule ID or otherwise expose it in a stable way.

## Current Documentation Contract

`docs/typed-ids.md` documents `rtk bash scripts/check-semantic-hygiene.sh` as the semantic hygiene review command.

It also states:

- Primitive values remain valid at real I/O edges.
- Tests and fixtures should use semantic values through helper APIs unless asserting actual wire, persistence, or rendered-output boundaries.
- Tightening allowances is reviewer-owned policy work.
- Policy changes under `internal/analysis/semantichygiene` are reviewer-owned.
- Broad package allowlists, helper hiding, casts, and `.String()` wrappers do not preserve the contract.

## Design Conclusions From This Thread

This thread settled these planning inputs:

- Use inline-only waivers for v1.
- Use analyzer-code budgets for v1, not external config.
- Waivers must be rule-specific and explanation-required.
- Unknown waivers, non-waivable waivers, waivers missing explanations, stale waivers, and budget violations fail CI.
- Rule IDs must be stable dotted identifiers and must not be derived from diagnostic text.
- The waiver mechanism should live inside the analyzer instead of parsing CLI output after the fact.
- The first implementation should add waiver infrastructure before adding new BDD/readability rules.
- Declaration-scoped waivers are acceptable for declaration diagnostics, while statement/expression rules should use next-line or node-local scope.

## Current Worktree Notes

At planning time, `go.mod` and `go.sum` already have unstaged changes that are unrelated to this plan.

This planning task must not modify those files.

## Constraints

- Do not implement code during planning.
- Do not weaken checker policy as part of adding waivers.
- Keep semantic leaks and dangerous test patterns non-waivable.
- Keep the existing reviewer command stable unless implementation proves a wrapper is necessary.
- Keep policy reviewer-owned.
- Use `rtk` for shell commands.
