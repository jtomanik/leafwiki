<!-- leafwiki
version: 1
page:
  id: go-ginkgo-test-taxonomy-labels-context-20260704
  title: Go Ginkgo Test Taxonomy Labels - Context
  created_at: "2026-07-04T11:30:00Z"
  updated_at: "2026-07-04T11:30:00Z"
  creator_id: system
  last_author_id: system
tags:
  - plans
  - context
fields:
  type: refactor
-->

# Go Ginkgo Test Taxonomy Labels - Context

## Problem Frame

LeafWiki is close to completing a broad migration to Ginkgo/Gomega. The suite now needs a stable execution taxonomy: `unit`, `integration`, and Go/Ginkgo `e2e`.

The taxonomy has two jobs:

- Communicate the test boundary to humans and implementation agents.
- Eventually allow reliable `--label-filter` selection.

The risk is that labels are tiny edits spread through exactly the files currently being cleaned up. A repo-wide label pass during BDD cleanup would create high-conflict diffs, and a final big-bang merge would be hard to review.

The first useful slice is therefore not "label everything." It is a correctness scaffold:

- Define the taxonomy.
- Reject bad labels.
- Report missing labels.
- Label a small stable pilot slice.
- Defer completeness enforcement and CI split until the active cleanup stabilizes.

## Key Interpretation

The distinction between `integration` and `e2e` is not whether the test is written in Go, whether it uses HTTP-looking objects, or whether it exercises a broad behavior.

The distinction is:

- `integration`: calls Go code in-process.
- `e2e`: acts on the LeafWiki product binary or command build artifact.

This makes the taxonomy sharp enough to guide future changes:

- `httptest.NewRecorder` against a handler is integration.
- A full in-process router built in Go is still integration.
- A compiled `leafwiki` binary started by the test and reached over HTTP or MCP is e2e.
- A command main seam invoked through helper functions is not e2e.
- A compiled command run through `os/exec` with args/stdin/stdout is e2e.

## Approach Analysis

### Approach A: Label packages wholesale

This is tempting because it minimizes edit count, but it is unsafe.

Ginkgo labels union down the hierarchy. A package or top-level `Describe` label cannot be "overridden" by a child. Mixed packages would either be mislabeled or accumulate multiple taxonomy labels on the effective spec.

Rejected except for packages/files that are demonstrably homogeneous.

### Approach B: Label every current test in one branch

This would produce the final desired state faster, but it conflicts with active cleanup:

- BDD cleanup rewrites Ginkgo containers and names.
- Matcher cleanup rewrites assertions and helper files.
- Taxonomy labels touch the same node calls and files.

A single final merge would be fragile and hard to review.

Rejected for the parallel phase.

### Approach C: Add docs only and wait

This avoids conflicts but does not prevent new label debt. Agents could start adding labels inconsistently, or implementers could choose package-level labels in mixed packages.

Rejected because it provides guidance without enforcement.

### Approach D: Add semantic-hygiene hard checks for bad labels plus a separate missing-label report

This fits the repo:

- `semantichygiene` already owns LeafWiki-specific Ginkgo policy.
- The gate already covers the relevant Go packages and modules.
- Bad labels can fail now without requiring a complete migration.
- Missing labels can be reported separately until the suite is fully labeled.

Selected.

## Enforcement Philosophy

There are two different states:

- Invalid taxonomy state: dynamic label, unknown label under the current label policy, or more than one effective taxonomy label. This must fail immediately.
- Incomplete taxonomy state: a runnable spec has no taxonomy label yet. This must be visible but non-failing during parallel cleanup.

The analyzer should hard-fail invalid states. A report-only command should expose incomplete state.

This avoids the main failure modes:

- Label debt does not grow silently.
- Existing unlabeled tests do not block cleanup.
- The repo cannot accidentally start using filtered CI while most specs are still unlabeled.

## Worktree Strategy

A taxonomy worktree is appropriate, but the work should land in small commits:

1. Taxonomy docs and planning artifacts.
2. Bad-label checker rules and fixtures.
3. Report-only inventory command/script.
4. Small stable pilot label slice.
5. Later label slices as cleanup files stabilize.
6. Final missing-label enforcement after the report reaches zero.

The worktree should be rebased or merged from `codex/recover-ginkgo-conversion` regularly so conflicts surface early.

The plan should not require a single "commit all at once" merge.

## Reporting Shape

The missing-label report should not overwrite a tracked file by default.

Reason:

- The user's machine has a hard safety rule for helpers that could overwrite data.
- Active cleanup changes the test surface frequently.
- A stale generated checklist can look authoritative when it is only a snapshot.

The report command should print to stdout by default. A tracked `docs/todo/test-taxonomy-labeling.md` can be updated manually or by an explicit, reviewed write path after the first infrastructure slice is stable.

## External Research Decision

No web research is needed for the plan.

The load-bearing external/library fact is Ginkgo's local `Label` API behavior, verified from the version in the repo. The rest is repo-specific policy and active cleanup coordination.

## Planning Risks

| Risk | Mitigation |
|---|---|
| Checker reports missing labels as hard failures too early | Keep missing-label detection out of `semantichygiene` hard diagnostics until final adoption |
| Labels conflict through inheritance | Add fixtures for parent/child conflicts and table/entry conflicts |
| Reporter duplicates checker logic and drifts | Share taxonomy constants/policy between reporter and checker |
| Pilot labels conflict with cleanup | Restrict pilot to stable low-touch packages and keep it in a separate commit |
| Future orthogonal labels are blocked forever | Keep an explicit allowed-label policy and document that new non-taxonomy labels require policy review |
| CI split hides unlabeled specs | Do not add label-filtered CI until completeness is enforced |

## Success Shape

After this plan's first phase:

- Developers have a documented test taxonomy.
- `scripts/check-semantic-hygiene.sh` fails bad taxonomy labels.
- A report command shows missing labels without failing the gate.
- At least one stable real test slice demonstrates the label style.
- The rest of the suite can continue BDD cleanup without being blocked by missing labels.

After later completion:

- Every runnable Go/Ginkgo spec has exactly one taxonomy label.
- Completeness is a hard checker rule.
- Label-filtered local or CI runs can be trusted.
