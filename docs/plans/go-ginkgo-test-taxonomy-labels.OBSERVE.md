<!-- leafwiki
version: 1
page:
  id: go-ginkgo-test-taxonomy-labels-observe-20260704
  title: Go Ginkgo Test Taxonomy Labels - Observations
  created_at: "2026-07-04T11:30:00Z"
  updated_at: "2026-07-04T11:30:00Z"
  creator_id: system
  last_author_id: system
tags:
  - plans
  - observations
fields:
  type: refactor
-->

# Go Ginkgo Test Taxonomy Labels - Observations

## Origin Conversation

Current thread/session ID: `019f2c2a-d4d8-7c21-9828-e262612ea50c`.

Current thread link: `codex://threads/019f2c2a-d4d8-7c21-9828-e262612ea50c`.

The user wants to introduce explicit `unit`, `integration`, and `e2e` divisions for Go/Ginkgo tests using the Ginkgo `Label` decorator while the broader Ginkgo/Gomega cleanup is still in progress.

The user accepted these definitions:

- `unit`: one component or package contract, in-process and deterministic. Temporary files are acceptable when the file format or file-backed component is the subject under test. No real HTTP route, MCP client/server, SQLite adapter boundary, subprocess, daemon, or multi-service `wiki.NewWiki` stack.
- `integration`: Go code called in-process while wiring multiple LeafWiki components or a real adapter boundary. Examples include route handlers, Gin middleware, SQLite stores, Fosite/OAuth flow, MCP server objects, `wiki.NewWiki`, importer plus wiki plus tags/properties, workspace sync/git revision storage, and daemon registry behavior.
- `e2e`: Go/Ginkgo tests that act on the LeafWiki product binary or command build artifact. The test drives args, env, stdin/stdout, HTTP, MCP transport, filesystem effects, or exit status through the thing LeafWiki ships or runs.

The user explicitly narrowed `e2e`:

- Playwright/browser tests are outside this Go/Ginkgo taxonomy.
- Docker proxy tests are outside this Go/Ginkgo taxonomy unless they are later represented by Go/Ginkgo tests acting on a product binary.
- The important boundary is binary/build artifact versus in-process Go code.

The user wants this work to proceed in parallel with current cleanup. The accepted shape is a separate taxonomy worktree or branch, small commits, and integration back into the active cleanup branch in pieces rather than one large final merge.

## Current Repo State

Branch observed: `codex/recover-ginkgo-conversion`.

The worktree is dirty with unrelated active cleanup work, including files under:

- `internal/wiki/mcp/`
- `internal/core/shared/errors/`
- `e2e/cmd/wikid-store/main_test.go`
- `go.mod`
- `go.sum`
- `docs/signals/`

This plan must not require touching those active cleanup files for its first infrastructure slice.

## Test Surface Facts

Live scan during the taxonomy discussion found:

- `326` Go `_test.go` files after excluding `references/`, `ui/leafwiki-ui/node_modules/`, and `testdata/`.
- `64` test directories.
- No existing `Label(...)` usage in runnable Go tests.
- The Go suite is already Ginkgo-runner based. Apparent extra `func Test...` hits are analyzer fixtures or suite bootstrap functions.
- `e2e-proxy` is a separate Go module.
- Playwright tests live under `e2e/tests/*.spec.ts` and must stay outside the Go/Ginkgo `e2e` label.

## Ginkgo Label Semantics

Local docs from `go doc github.com/onsi/ginkgo/v2.Label` on the repo dependency state:

- `Label(labels ...string)` decorates specs with labels.
- Labels can be applied to container and subject nodes, not setup nodes.
- A spec's labels are the union of labels in its node hierarchy.
- Label strings must not include `&|!,()/`.

The union rule is the main implementation risk. A broad parent `Label("unit")` combined with a child `Label("integration")` creates an effective multi-label spec, not an override.

## Existing Checker Pattern

The repo already has a semantic hygiene analyzer:

- Analyzer package: `internal/analysis/semantichygiene`.
- Ginkgo-specific rules: `internal/analysis/semantichygiene/rules_ginkgo.go`.
- Rule IDs, metadata, waiver budgets, and AST helpers: `internal/analysis/semantichygiene/policy.go`.
- Diagnostics: `internal/analysis/semantichygiene/diagnostics.go`.
- Fixture runner: `internal/analysis/semantichygiene/analyzer_test.go`.
- Command wrapper: `cmd/leafwiki-vet`.
- Review script: `scripts/check-semantic-hygiene.sh`.

The review script already runs the analyzer against:

- `internal/...`
- `cmd/...`
- `e2e/...`
- `e2e-proxy/...`

Therefore the safest hard-fail surface for bad labels is the existing semantic hygiene analyzer, not a second checker.

## Existing Planning And Cleanup Patterns

Relevant plan conventions:

- `docs/plans/*.OBSERVE.md`, `*.CONTEXT.md`, `*.DECISION.md`, `*.PLAN.md`, and `*.planning.aibasic.json` are established artifacts for AIBASIC planning.
- Plans include canonical LeafWiki metadata comments.
- `docs/todo/ginkgo-gomega-second-pass.md` is the precedent for a repo-local checklist artifact tied to a broad Ginkgo cleanup.

Relevant institutional learning:

- For Ginkgo cleanup, track large repo sweeps with a repo-local checklist artifact instead of chat state only.
- Do not mix unrelated cleanup with checker or taxonomy work.
- Keep broad hygiene gates strict, and use report-only or explicit waivers where enforcement would otherwise collide with active migration.

## Mixed Package Evidence

Package-only labeling would be lossy in at least these areas:

- `cmd/leafwiki`: helper and flag tests can be unit, while daemon/runtime/stdio tests can be integration or e2e depending on whether they execute a product binary.
- `internal/wiki/mcp`: helper/schema tests can be unit or integration, while local MCP registration, OAuth, and parity tests are integration unless they drive a product binary.
- `internal/importer`: content transformer and planner tests can be unit; `importer_integration_test.go` wires importer, wiki, tags, properties, and storage.
- `internal/wikid`: supervisor decision tests can be unit; registry/store/private handler tests cross storage or HTTP boundaries.

## Constraints For The Plan

- First slice must be parallel-safe with active BDD cleanup.
- Missing labels must not fail the semantic hygiene gate initially.
- Bad labels must fail early so future label debt does not accumulate.
- Completeness enforcement must wait until the inventory reaches zero.
- Label-only diffs should be small and reviewable.
- Implementation commands should be run through `rtk`.
- Any helper script with possible data loss or overwrites must require explicit approval before use; report scripts should print to stdout by default.
