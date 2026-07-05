<!-- leafwiki
version: 1
page:
  id: leafwiki-vet-architecture-hygiene-context-20260705
  title: LeafWiki Vet Architecture Hygiene Context
  created_at: "2026-07-05T00:00:00Z"
  updated_at: "2026-07-05T00:00:00Z"
  creator_id: codex
  last_author_id: codex
tags:
  - plans
fields:
  type: refactor
-->

# LeafWiki Vet Architecture Hygiene Context

## Problem Frame

LeafWiki has a checker that began as a semantic-value hygiene analyzer. It now enforces more than semantic primitive boundaries:

- i18n and catalog-backed message policy
- stable contract literal policy
- Ginkgo and Gomega test-shape policy
- taxonomy label policy
- waiver accounting
- dependency direction for `e2e-proxy`

The implementation reality no longer matches the name `semantichygiene`. Adding Sentrux-derived architecture rules directly into that package would deepen the mismatch.

The durable goal is a project policy checker that controls basic architectural direction. The code should make that goal visible.

## Important Distinction

There are two different concepts that should not be conflated:

1. `leafwiki-vet` as the command and gate surface.
2. Policy-family modules that implement specific classes of checks.

The command can remain stable while internals split. This is preferable because users and scripts already know `leafwiki-vet` and `scripts/check-semantic-hygiene.sh` as the review surface.

## Architectural Shape To Prefer

The clean internal model is:

```text
cmd/leafwiki-vet
  -> LeafWiki analyzer suite
       -> semantichygiene
       -> testhygiene
       -> architecturehygiene
       -> i18ncatalog
```

There are two ways to implement that model:

1. Multiple `analysis.Analyzer` values, one per policy family.
2. One umbrella `analysis.Analyzer` that delegates to policy-family packages.

Multiple analyzers are natural for golangci-lint and `multichecker`, but they complicate global waiver accounting because each analyzer runs separately.

One umbrella analyzer preserves a single traversal and finalization pass, but it hides the family split from the tool runner.

The least risky staged approach is:

- use a shared `checkerpolicy` package before moving waivable rules
- allow `architecturehygiene` to begin as a hard-rule analyzer because dependency rules are non-waivable
- do not move waivable Ginkgo/Gomega rules into `testhygiene` until shared policy and waiver ownership are explicit

## Shared Policy Risk

The current waiver model is more than diagnostic formatting. It includes:

- rule metadata
- hard versus waivable classification
- waiver parsing
- stale waiver detection
- duplicate waiver detection
- non-waivable waiver rejection
- budget enforcement
- scoped matching to calls or declarations

If each future analyzer owns its own independent copy of that behavior, the checker will drift. A stale test waiver might be ignored by architecture rules, an architecture waiver might be unknown to test rules, and budgets may become less meaningful.

Therefore shared rule metadata and reporting infrastructure should move before large waivable-rule extraction. For hard dependency rules, the implementation can move earlier because they do not use waivers.

## Diagnostic Naming

Existing diagnostics use a `semh:` display prefix and `semh:allow` waiver comments. The prefix is semantically stale, but changing it is not required to achieve the architecture goal.

The split should preserve current rule IDs and waiver syntax. A later naming cleanup can decide whether to introduce `leafwiki-vet:` or another display prefix.

Changing names during the split would combine two risky moves:

- package ownership changes
- diagnostic or waiver surface changes

The implementation should avoid that combination.

## Architecture Hygiene Scope

`architecturehygiene` should start with checks that answer a concrete architecture question:

- which package is importing which package?
- is this package depending upward on an executable or adapter?
- is this test surface violating the black-box boundary?
- is concrete wiring leaking outside composition roots?

The first implemented scope should be import direction. This maps directly to `ImportSpec`, `pass.Pkg.Path()`, and import path matching.

More nuanced dependency inversion rules should be added only after import-boundary parity is stable.

## Sentrux Migration Scope

The useful migration target is the Sentrux boundary table, not all Sentrux behavior.

Import boundaries are architectural contracts. They belong in the project checker.

Metric ratchets are health signals. They may be useful, but they do not have the same clarity as a dependency direction violation. Porting them now would add implementation cost without strengthening the architecture contract as much as boundary rules.

The plan should therefore defer:

- cycle count parity
- coupling grade parity
- cyclomatic complexity parity
- god-file detection

If those checks are still valuable later, they can be planned as a separate `metricshygiene` or tool-wrapper decision.

## Test Hygiene Scope

Ginkgo/Gomega policy is already repo-specific and reviewer-owned. It is not generic linter policy.

The split should eventually move this family into `testhygiene`:

- Ginkgo focus/pending/flakes/restricted decorators
- taxonomy labels
- migrated-test residue rules
- Ginkgo body-shape rules
- Gomega assertion quality rules
- waivable BDD/readability diagnostics

Generic Ginkgo/Gomega mechanics should remain in `ginkgolinter` where possible. `testhygiene` should own LeafWiki-specific policy and BDD documentation quality.

## Sequencing Tradeoffs

### Big-bang split

Moving semantic, test, architecture, waiver policy, runner, plugin, and Sentrux rules in one commit would create a high-review-cost diff. It would also make failures hard to attribute.

Reject this approach.

### Architecture first, then test split

Moving the existing e2e-proxy dependency rule into `architecturehygiene` creates a small proof point. It can update `cmd/leafwiki-vet`, the golangci plugin, and fixtures before the larger Ginkgo/Gomega extraction starts.

Accept this as the first visible split.

### Shared policy before all extraction

Extracting shared policy first reduces duplication and protects waiver behavior. It may touch many files, but it can be behavior-preserving if it keeps the current analyzer running.

Accept this as the safest foundation for moving waivable rules.

### Extend architecture after the split proof

Porting the Sentrux boundary table after `architecturehygiene` exists keeps the rule family honest. It avoids adding more architecture policy to the old semantic package.

Accept this sequencing.

## Review Posture

This work changes the review gate. The plan should assume:

- analyzer policy diffs are reviewer-owned
- failing fixtures come before hardening rules
- hard rules stay hard
- no broad baselines or suppressions are introduced
- unrelated current working-tree dirt stays out of the accepted slice

## Implementation Style

The implementation should be test-first at the analyzer fixture level:

- add fixture coverage for moved behavior before deleting the old rule path
- prove `cmd/leafwiki-vet` invokes the expected analyzer set
- prove the golangci plugin exposes the same policy through the repo-facing gate
- add architecture boundary fixtures before adding the Sentrux-derived hard rules

The implementation should not change runtime behavior.
