<!-- leafwiki
version: 1
page:
  id: test-taxonomy-labeling-20260704
  title: Test Taxonomy Labeling
  created_at: "2026-07-04T12:00:00Z"
  updated_at: "2026-07-04T12:00:00Z"
  creator_id: system
  last_author_id: system
tags:
  - todo
  - testing
  - ginkgo
fields:
  type: checklist
-->

# Test Taxonomy Labeling

This ledger tracks rollout of the Go/Ginkgo `unit`, `integration`, and `e2e`
taxonomy defined in `docs/testing-taxonomy.md`.

Missing labels are not a failing condition yet. This file is a progress ledger,
not checker policy.

## Snapshot

- Snapshot date: 2026-07-04.
- Snapshot source: `rtk bash scripts/report-test-taxonomy.sh`.
- Report command: `rtk bash scripts/report-test-taxonomy.sh`.
- Report command status: implemented as report-only inventory.
- Go `_test.go` files scanned: 331.
- Runnable Go/Ginkgo specs scanned: 3619.
- Taxonomy-labeled runnable specs found: 158.
- Missing taxonomy labels reported: 3461.
- Exclusions used for the snapshot: `references/`, `ui/leafwiki-ui/node_modules/`, and `testdata/`.

Refresh this ledger from the report command as labeling progresses. Treat the
package groups below as review queues, not as a complete generated inventory.

## Rollout State

- [x] Taxonomy policy documented in `docs/testing-taxonomy.md`.
- [x] Semantic hygiene rejects unknown taxonomy labels.
- [x] Semantic hygiene rejects dynamic taxonomy labels.
- [x] Semantic hygiene rejects specs with more than one effective primary taxonomy label.
- [x] Report-only missing-label inventory exists.
- [x] Stable pilot label slice exists.
- [ ] Missing taxonomy labels are hard-failing.
- [ ] Label-filtered CI is enabled.

Do not check the last two items until every runnable Go/Ginkgo spec has exactly
one primary taxonomy label and the report command returns zero missing labels.

## Stable Unit Candidates

These files are likely low-conflict `unit` pilot candidates because they focus
on local package contracts and deterministic helper behavior. Verify before
labeling.

- [x] `internal/workspaceid/validate_test.go`
- [x] `internal/core/excerpt/excerpt_test.go`
- [x] `internal/agenthooks/normalize_test.go`
- [x] `internal/core/identity/semantic_types_test.go`
- [x] `internal/core/tree/semantic_types_test.go`
- [x] `internal/core/markdown/frontmatter_test.go`
- [x] `internal/core/markdown/metadata_codec_test.go`
- [x] `internal/localization/render_test.go`

Label these at the narrowest truthful container. Do not add a package-level
label unless every runnable spec in the package has the same taxonomy.

## Stable Integration Candidates

These areas likely contain integration specs because they cross real route,
middleware, store, OAuth, MCP, or multi-component boundaries. Verify each file
before labeling.

- [ ] `internal/http/middleware/auth/`
- [ ] `internal/http/middleware/security/`
- [ ] `internal/http/router_test.go`
- [ ] `internal/core/auth/session_store_test.go`
- [ ] `internal/core/auth/user_store_test.go`
- [ ] `internal/core/auth/auth_sql_store_edges_gomega_test.go`
- [ ] `internal/importer/importer_integration_test.go`
- [ ] `internal/workspacesync/gitrevisions/`
- [ ] `internal/projectdaemon/`

Some files in these areas may still contain pure helper specs. Split mixed
files by `Describe`, `DescribeTable`, `Entry`, or `It` rather than labeling the
whole package.

## Potential Go/Ginkgo E2E Candidates

No Go/Ginkgo e2e candidates are confirmed by this docs-only slice. Review these
areas for tests that build or start a LeafWiki product binary or command build
artifact:

- [ ] `cmd/leafwiki/`
- [ ] `e2e/cmd/grant-workspaces/`
- [ ] `e2e/cmd/wikid-store/`
- [ ] other Go/Ginkgo specs that use `gexec.Build`, `exec.Command`, or a
  started LeafWiki runtime

Do not label an in-process command helper test as `e2e`. If the test calls Go
functions directly, classify it as `unit` or `integration` based on the real
boundary it exercises.

## Mixed Packages Requiring Spec-Level Review

These packages are likely to contain more than one taxonomy and should not be
labeled only at package or suite level without review:

- [ ] `cmd/leafwiki`
- [ ] `internal/wiki/mcp`
- [ ] `internal/importer`
- [ ] `internal/wikid`
- [ ] `internal/wiki/health`
- [ ] `internal/http`
- [ ] `internal/frontd`
- [ ] `e2e/cmd/grant-workspaces`
- [ ] `e2e/cmd/wikid-store`

For mixed packages, use container-level or spec-level labels. Remember that
Ginkgo label inheritance is additive, not overriding.

## Active Cleanup Hotspots

Avoid these areas for the first taxonomy pilot unless the owning cleanup work
explicitly hands them over:

- [ ] `internal/wiki/mcp/`
- [ ] `internal/core/shared/errors/`
- [ ] `e2e/cmd/wikid-store/main_test.go`
- [ ] `go.mod`
- [ ] `go.sum`

The taxonomy rollout should not rewrite BDD names, matcher helpers, semantic
fixtures, or module metadata just to add labels.

## Adoption Gates

Do not make missing labels a hard failure until all of these are true:

- [ ] The report command exists and exits zero while listing missing labels.
- [ ] The report command shows zero missing primary taxonomy labels.
- [ ] Mixed packages have been reviewed at spec, container, table, or entry
  granularity.
- [ ] Semantic hygiene already rejects unknown, dynamic, and multiple primary
  taxonomy labels.
- [ ] The missing-label hard diagnostic has a focused fixture proving the final
  behavior.

Do not add label-filtered CI until missing-label enforcement is hard-failing.
Before that point, `--label-filter=unit`, `--label-filter=integration`, and
`--label-filter=e2e` can silently skip unlabeled specs.

## Update Procedure

1. Run the report command when it exists:

   ```sh
   rtk bash scripts/report-test-taxonomy.sh
   ```

2. Move files from review queues into labeled or deferred groups based on the
   report output.
3. Keep active cleanup hotspots separate from stable pilot candidates.
4. Add notes for mixed files that need spec-level review.
5. Do not mark completeness until the report reaches zero missing labels.
