<!-- leafwiki
version: 1
page:
  id: semantic-hygiene-waivable-diagnostics-context-20260701
  title: Semantic Hygiene Waivable Diagnostics - Context
  created_at: "2026-07-01T00:00:00Z"
  updated_at: "2026-07-01T00:00:00Z"
  creator_id: system
  last_author_id: system
tags:
  - plans
  - context
fields:
  type: refactor
-->

# Semantic Hygiene Waivable Diagnostics - Context

## Problem Frame

The semantic hygiene checker has grown from a narrow semantic-boundary analyzer into a repo-specific semantic quality gate.

That is the right evolution, but it creates a policy problem.

Some rules identify contract-breaking behavior and should never be waived: raw semantic primitives, raw user-facing prose, weak error-string assertions, unsafe async assertions, focused or pending specs, and uncleaned global state changes.

Other rules protect BDD readability and test-quality judgment. These rules are valuable, but a hard-only model can force worse code when an unusual spec reads more clearly with an exception.

Ordinary non-failing warnings do not solve this. Agents will ignore them, or will learn that a green gate does not require acting on them.

The needed middle ground is an enforceable waiver system: a diagnostic still fails unless a local, rule-specific, explanation-bearing waiver suppresses it, and CI still fails when waivers are malformed, stale, or over budget.

## Policy Terms

Error:

- A non-waivable semantic hygiene diagnostic.
- It always reports and fails the checker.
- It represents a semantic leak, dangerous test pattern, or policy violation that should be fixed rather than justified.

Waivable diagnostic:

- A diagnostic that still fails by default.
- It may be suppressed only by a valid waiver for the exact rule ID and scope.
- It exists for judgment-heavy readability or maintainability rules where rare exceptions can preserve clarity.

Waiver:

- An inline source comment using the semantic hygiene waiver syntax.
- It names one rule ID and gives a required explanation.
- It suppresses one matching diagnostic in its allowed scope.

Stale waiver:

- A syntactically valid waiver that no longer suppresses any diagnostic.
- It fails the checker because it hides outdated policy context in source.

Budget:

- A hard maximum for active waivers in one analyzer package.
- Budgets apply per package and per rule because the current `singlechecker`
  runner creates analyzer state package-by-package.
- Budgets do not apply to errors because errors are never waivable.

## Scope

In scope:

- Add structured diagnostic metadata inside `internal/analysis/semantichygiene`.
- Add stable rule IDs for current and near-term rule families.
- Add rule metadata that records whether a rule is waivable and how waiver scope is matched.
- Parse `// semh:allow <rule-id> -- <explanation>` comments.
- Suppress matching waivable diagnostics.
- Report invalid waiver states as hard diagnostics.
- Enforce total and per-rule waiver budgets from Go policy tables.
- Print rule IDs in diagnostics so fixture tests and reviewers can rely on stable identifiers.
- Document the waiver policy in `docs/typed-ids.md`.
- Add analyzer fixture and focused unit coverage for all waiver behaviors.

Out of scope:

- Advisory non-failing warnings.
- External YAML, TOML, or JSON waiver budget config.
- Package-wide, file-wide, or directory-wide waivers.
- Baseline files for existing diagnostics.
- Auto-fixes.
- Frontend ESLint waiver support.
- New broad semantic policy changes unrelated to waiver infrastructure.
- Product/test cleanup beyond analyzer fixtures.

## Rule Taxonomy

Never-waivable rules:

- `semantic.string-leak`
- `semantic.direct-cast`
- `semantic.unchecked-constructor`
- `semantic.raw-signature`
- `semantic.raw-field`
- `semantic.raw-primitive`
- `i18n.raw-prose`
- `i18n.message-field`
- `contract.raw-literal`
- `dependency.e2e-proxy-internal-import`
- `ginkgo.focus`
- `ginkgo.pending`
- `ginkgo.flake-attempts`
- `ginkgo.goroutine-recover`
- `ginkgo.blocking-receive`
- `ginkgo.global-state-cleanup`
- `gomega.raw-string-match-error`
- `gomega.err-error-string`
- `gomega.async-context`
- `gomega.async-boolean`
- `gomega.async-negative-receive`
- `gomega.async-bare-value`
- `gomega.async-callback-expect`

Waivable rules:

- `ginkgo.top-level-it`
- `ginkgo.wide-entry`
- `gomega.helper-should-be-matcher`
- `gomega.repeated-field-assertions`
- `gomega.collection-index-assertion`
- `gomega.equal-empty`
- `gomega.equal-zero`
- `gomega.numeric-equivalent`
- `gomega.time-equal`

Deferred readability candidates that are not part of the current gate:

- `ginkgo.long-it`
- `ginkgo.multiple-behaviours`
- `ginkgo.vague-container-name`

Those IDs should not be documented as active until they have accepted rule
definitions, fixtures, diagnostics, metadata, and budgets.

## Waiver Syntax

The v1 waiver syntax should be:

```go
// semh:allow ginkgo.top-level-it -- package-level invariant reads clearer without an artificial container
```

Syntax rules:

- The comment must start with `semh:allow`.
- The rule ID must be a single known dotted identifier.
- The separator must be `--`.
- Explanation text after `--` is required.
- Explanation text must contain non-whitespace content.
- One waiver suppresses one diagnostic.
- Multiple waivers require multiple comments.

Rejected syntax for v1:

- `semh:ignore`
- `semh:allow *`
- `semh:allow file`
- `semh:allow ginkgo.*`
- Block comments
- Trailing comments attached to arbitrary expressions
- Waivers without explanations

## Scope Model

Waiver scope should be rule-metadata-driven.

Statement or expression rules use next-node scope:

- A waiver on the line immediately before the statement/expression line may suppress a diagnostic for that node.
- The waiver should not suppress a second diagnostic on the same or later node.

Declaration rules use declaration scope:

- A waiver on the line immediately before a `func`, `type`, or `var` declaration may suppress a matching diagnostic emitted on that declaration.

Ginkgo node rules use call scope:

- A waiver on the line immediately before a Ginkgo DSL call may suppress a matching diagnostic emitted on the call or the call's description literal.

No rule should get file-wide scope in v1.

## Rule ID Stability

Rule IDs should be stable API for the checker.

Diagnostic messages can improve over time. Rule IDs should not change unless a deliberate migration updates all fixtures, docs, and active waivers in the same slice.

Rule IDs should be data, not inferred from function names or diagnostic strings.

The implementation should use a typed rule ID constant set so a callsite cannot accidentally report an unregistered rule.

## Budget Model

Budgets should live in analyzer policy code for v1.

This keeps the mechanism reviewer-owned and avoids adding a config language before the project has evidence that it needs one.

Recommended initial package-local budgets:

- Total active waiver budget per package: `10`.
- Per-rule default budget: `0`.
- Explicit per-rule budgets only for known waivable rules.

Suggested initial per-rule budgets:

- `ginkgo.top-level-it`: `3`
- `ginkgo.wide-entry`: `3`
- `gomega.helper-should-be-matcher`: `3`
- `gomega.repeated-field-assertions`: `3`
- `gomega.collection-index-assertion`: `3`
- `gomega.equal-empty`: `3`
- `gomega.equal-zero`: `3`
- `gomega.numeric-equivalent`: `3`
- `gomega.time-equal`: `3`

The package-local total budget prevents the per-rule limits from normalizing too
many exceptions inside one analyzer package. Repo-wide exception totals require a
separate aggregation layer and are deferred from v1.

## Reporting Model

Normal diagnostics should include the rule ID:

```text
semh:ginkgo.top-level-it: top-level It reads like a migrated unit test; place it under a behavior container or waive with a specific reason
```

Waiver validation diagnostics should also include rule IDs for the waiver subsystem:

- `semh:waiver.unknown-rule`
- `semh:waiver.non-waivable-rule`
- `semh:waiver.missing-explanation`
- `semh:waiver.stale`
- `semh:waiver.duplicate`
- `semh:waiver.budget-exceeded`

Suppressed diagnostics should not print as advisory warnings during normal runs.

Budget summaries should be emitted only when there is at least one active waiver or when a budget failure occurs.

## Implementation Posture

This should be implemented test-first.

Start with focused parser and matching tests, then add fixture tests that prove the analyzer accepts valid waivers and rejects invalid waiver states.

Only after waiver infrastructure is in place should implementation add new BDD/readability rules such as `ginkgo.top-level-it`.

The plan should not ask implementers to fix existing product/test diagnostics in the same slice unless those diagnostics are analyzer fixture cases.

## Risks

The main risk is accidentally making waivers into a broad suppressions system.

The second risk is making diagnostic callsites too noisy to maintain by passing rule metadata manually everywhere.

The third risk is making stale-waiver detection depend on analysis package order in a way that produces unstable output.

The plan should therefore introduce one reporting helper and one finalization step in the analyzer run, then route all diagnostic emissions through that helper.
