<!-- leafwiki
version: 1
page:
  id: go-ginkgo-test-taxonomy-labels-decision-20260704
  title: Go Ginkgo Test Taxonomy Labels - Decision
  created_at: "2026-07-04T11:30:00Z"
  updated_at: "2026-07-04T11:30:00Z"
  creator_id: system
  last_author_id: system
tags:
  - plans
  - decision
fields:
  type: refactor
-->

# Go Ginkgo Test Taxonomy Labels - Decision

## Decision

Create a new implementation plan, `go-ginkgo-test-taxonomy-labels`, that introduces Go/Ginkgo test taxonomy labels in a parallel-safe way.

The plan will add documentation, hard-fail rules for invalid labels, a report-only inventory for missing labels, and a small stable pilot label slice. It will not require repo-wide label completeness while Ginkgo/Gomega cleanup is still active.

## Selected Course Of Action

1. Define the taxonomy in docs.
   - Add `docs/testing-taxonomy.md`.
   - State the accepted `unit`, `integration`, and Go/Ginkgo `e2e` boundaries.
   - Explicitly exclude Playwright and Docker proxy tests from this label taxonomy.

2. Extend the existing semantic hygiene checker for invalid labels.
   - Add hard rules under `internal/analysis/semantichygiene`.
   - Reuse `rules_ginkgo.go`, `policy.go`, and `diagnostics.go`.
   - Fail unknown labels, dynamic labels, and multiple effective taxonomy labels.
   - Keep missing labels out of hard diagnostics during the parallel phase.

3. Add fixtures before implementation.
   - Prove direct multiple labels.
   - Prove parent/child inherited conflicts.
   - Prove `DescribeTable` plus `Entry` conflicts.
   - Prove dynamic labels are rejected.
   - Prove unlabeled specs are allowed in the hard gate during the parallel phase.

4. Add a report-only inventory.
   - Add a small Go command and script that report missing taxonomy labels.
   - Print to stdout by default.
   - Generate counts grouped by package/file/taxonomy state.
   - Do not overwrite `docs/todo` automatically.

5. Add a tracked checklist snapshot.
   - Add `docs/todo/test-taxonomy-labeling.md`.
   - Mark it clearly as a snapshot and progress ledger, not the hard gate.
   - Group mixed packages explicitly so later label slices are reviewable.

6. Label a stable pilot slice.
   - Start with low-conflict packages that are not currently dirty.
   - Keep pilot label changes separate from checker infrastructure.
   - Do not label active MCP cleanup files in the first implementation slice.

7. Defer full completeness and CI filtering.
   - Only add a hard missing-label rule after the report reaches zero.
   - Only add `--label-filter` CI jobs after hard completeness exists.

## Key Decisions

| Topic | Decision | Reason |
|---|---|---|
| Taxonomy labels | `unit`, `integration`, `e2e` | Matches the accepted discussion and keeps the primary tier exclusive |
| E2E boundary | Product binary/build artifact | Separates true black-box artifact tests from in-process integration tests |
| Playwright/Docker | Out of this taxonomy | They are separate CI/test surfaces, not Go/Ginkgo label targets |
| Enforcement host | Existing `semantichygiene` analyzer | The repo already owns LeafWiki-specific Ginkgo policy there |
| Missing labels | Report-only initially | Avoids blocking active cleanup |
| Bad labels | Hard failure immediately | Prevents new bad taxonomy debt |
| Work mode | Separate taxonomy worktree/branch, small commits | Avoids final giant merge conflicts with cleanup |
| First real labels | Stable pilot only | Proves style without touching active cleanup hotspots |
| Completeness | Deferred hard gate | Label-filtered runs are not trustworthy until every spec is labeled |

## Alternatives Rejected

### Label every package at suite level

Rejected. Ginkgo labels union down the hierarchy, so broad parent labels become wrong in mixed packages.

### Label everything in one huge branch

Rejected. This collides with active BDD and matcher cleanup and creates a difficult final merge.

### Wait until cleanup is fully complete

Rejected. Waiting prevents the repo from establishing the contract and allows bad labels to appear before enforcement exists.

### Add a standalone checker only

Rejected. A second analyzer would duplicate the existing semantic hygiene Ginkgo policy surface.

### Fail missing labels immediately

Rejected for the parallel phase. It would block unrelated cleanup before the inventory is labeled.

### Add label-filtered CI immediately

Rejected. Filtered runs silently skip unlabeled specs and would create false confidence.

## Resolved Open Questions

- Q: Is `e2e` about public-looking APIs or build artifacts?
  - A: Build artifacts. Go/Ginkgo `e2e` acts on the LeafWiki product binary or command binary.

- Q: Are Playwright and Docker proxy tests part of this label taxonomy?
  - A: No. They stay in their existing CI surfaces.

- Q: Can this proceed in parallel with cleanup?
  - A: Yes, if the first slice is docs/checker/report plus a small stable pilot, and missing labels stay report-only.

- Q: Should the work happen in a worktree?
  - A: Yes. A taxonomy worktree is appropriate, but integration should happen in small commits, not one final large merge.

- Q: Should package-level labels be allowed?
  - A: Only for homogeneous package/file groups. Mixed packages require `Describe`, `DescribeTable`, `Entry`, or spec-level labels.

## Implementation Posture

This is checker-first infrastructure work followed by label-only slices.

The implementation should start with failing analyzer fixtures for bad taxonomy states, then add the checker logic, then add the report-only inventory, and only then label a small stable pilot. The implementation must not rewrite active cleanup files to make the taxonomy look complete.
