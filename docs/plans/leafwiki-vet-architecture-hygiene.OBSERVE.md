<!-- leafwiki
version: 1
page:
  id: leafwiki-vet-architecture-hygiene-observe-20260705
  title: LeafWiki Vet Architecture Hygiene Observations
  created_at: "2026-07-05T00:00:00Z"
  updated_at: "2026-07-05T00:00:00Z"
  creator_id: codex
  last_author_id: codex
tags:
  - plans
fields:
  type: refactor
-->

# LeafWiki Vet Architecture Hygiene Observations

## Planning Source

- Workflow: `docs/plans/planning.aibasic.txt`.
- Thread ID: `019f2f0a-0ccf-76e0-b8e4-4d93bf45dd53`.
- Thread link: `codex://threads/019f2f0a-0ccf-76e0-b8e4-4d93bf45dd53`.
- Topic: split the current LeafWiki checker into policy-family modules, then extend the architecture module with project-specific dependency direction checks.
- Planning mode: docs-only. No implementation code was changed while producing this artifact.

## Conversation Observations

The discussion settled the high-level direction:

- `leafwiki-vet` should become the umbrella project policy checker rather than being treated as a pure semantic checker.
- The existing checker has already grown beyond its original semantic-value role.
- The desired families are `semantichygiene`, `architecturehygiene`, and `testhygiene`.
- The existing `dependency.e2e-proxy-internal-import` rule is the first architecture extraction proof.
- Architecture rules should start with basic project-tailored dependency direction checks, not a generic SOLID linter.
- Sentrux should eventually be removed, but not by cloning all of its metrics at once.
- The first Sentrux migration target should be import-boundary policy, not cycles, coupling grade, cyclomatic complexity, or god-file heuristics.
- Rule IDs, waiver behavior, and reviewer-owned policy should remain stable enough that the split does not weaken the current gate.

## Current Checker Shape

`internal/analysis/semantichygiene/analyzer.go` currently exports one `analysis.Analyzer` named `semantichygiene`. It uses the `inspect` analyzer and walks these node kinds:

- `*ast.AssignStmt`
- `*ast.CallExpr`
- `*ast.FuncDecl`
- `*ast.GoStmt`
- `*ast.ImportSpec`
- `*ast.KeyValueExpr`
- `*ast.TypeSpec`
- `*ast.UnaryExpr`
- `*ast.BasicLit`

The analyzer currently dispatches semantic-value rules, i18n/message rules, Ginkgo/Gomega rules, taxonomy checks, waiver finalization, and dependency direction checks from one package.

The checker source has already been split into cohesive files under `internal/analysis/semantichygiene`, including:

- `policy.go`
- `policy_waivers.go`
- `diagnostics.go`
- `rules_dependency.go`
- `rules_ginkgo*.go`
- `rules_gomega*.go`
- `rules_string.go`
- `rules_cast.go`
- `rules_signature.go`
- `rules_literal.go`
- `rules_message.go`

This plan is therefore a package and policy-family split, not a file-length split.

## Dependency Rule Observations

`internal/analysis/semantichygiene/rules_dependency.go` currently contains a small table-driven rule:

- importer path: `github.com/perber/wiki/e2e-proxy`
- forbidden import prefix: `github.com/perber/wiki/internal/`
- rule ID: `dependency.e2e-proxy-internal-import`
- diagnostic: `e2e-proxy must not import LeafWiki internal packages; assert protocol semantics or define local black-box test helpers`

The current fixture suite already includes `github.com/perber/wiki/e2e-proxy` through `internal/analysis/semantichygiene/analyzer_test.go`.

The fixture file `internal/analysis/semantichygiene/testdata/src/github.com/perber/wiki/e2e-proxy/proxy_auth_test.go` intentionally imports an internal package and expects the current dependency diagnostic. This is the right first extraction proof for `architecturehygiene`.

## Rule Policy And Waiver Observations

`internal/analysis/semantichygiene/policy.go` owns the current global rule ID table. The rule IDs already span several policy families:

- `semantic.*`
- `i18n.*`
- `contract.*`
- `dependency.*`
- `ginkgo.*`
- `ginkgo-linter.*`
- `gomega.*`
- `waiver.*`

`internal/analysis/semantichygiene/policy_waivers.go` owns structured diagnostic finalization, waiver parsing, waiver scope matching, stale-waiver detection, duplicate-waiver detection, non-waivable checks, and budget diagnostics.

Existing waiver syntax is `semh:allow <rule-id> -- <explanation>`. Some current rule families are waivable:

- `ginkgo.top-level-it`
- `ginkgo.wide-entry`
- `gomega.helper-should-be-matcher`
- `gomega.repeated-field-assertions`
- `gomega.collection-index-assertion`
- `gomega.non-empty-collection`
- `gomega.semantic-scalar-not-empty`
- `gomega.equal-empty`
- `gomega.equal-zero`
- `gomega.numeric-equivalent`
- `gomega.time-equal`

The dependency rule is hard and non-waivable.

The split must not accidentally make waiver validation per-family in a way that lets stale or malformed waivers escape.

## Runner And Gate Observations

`cmd/leafwiki-vet/main.go` currently uses `singlechecker.Main` and runs only `semantichygiene.Analyzer`.

`tools/golangci/leafwiki/plugin.go` currently exposes:

- `semantichygiene.Analyzer`
- `i18ncatalog.Analyzer`

It also reports `register.LoadModeTypesInfo`.

The repo-facing checker path is `scripts/golangci-lint.sh`, which executes the
custom LeafWiki golangci-lint binary.

`scripts/golangci-lint.sh` builds or reuses `.cache/tools/leafwiki-golangci-lint` and runs the custom golangci-lint binary over:

- `./cmd/...`
- `./internal/...`
- `./e2e/...`
- `./tools/...`
- `./...` from the `e2e-proxy` module

`scripts/test-golangci-lint.sh` verifies important wrapper contracts, including:

- root module invocation
- e2e-proxy module invocation
- rejection of package arguments
- rejection of policy-changing flags
- stale custom binary rebuild behavior
- absence of deprecated checker-specific compatibility wrappers

The plan must update both direct vet runner behavior and golangci plugin behavior. Otherwise the checker split can pass locally in one path but regress the repo-facing gate.

## Sentrux Rule Observations

`.sentrux/rules.toml` currently contains metric-style constraints and import-boundary rules.

Metric constraints:

- `max_cycles = 2`
- `max_coupling = "B"`
- `max_cc = 43`
- `no_god_files = true` is documented but disabled

Import boundaries:

| From | To | Reason |
|---|---|---|
| `internal/core/*` | `internal/wiki/*` | Core domain code must not depend on HTTP/wiki route adapters. |
| `internal/core/*` | `internal/http/*` | Core domain code must stay transport-agnostic. |
| `internal/core/*` | `internal/projectdaemon/*` | Core domain code must not know daemon/runtime orchestration. |
| `internal/wiki/*` | `cmd/leafwiki/*` | Route/domain assembly must not depend on CLI startup code. |
| `internal/projectdaemon/*` | `cmd/leafwiki/*` | Daemon primitives must not depend on the executable entrypoint. |
| `internal/workspaced/*` | `internal/frontd/*` | workspaced should expose workspace services without depending on the frontend proxy. |

The boundary rules map cleanly to a Go analyzer over import specs. The metric constraints do not map cleanly to a per-package `go/analysis` analyzer and should be deferred.

## Existing Plan And Memory Observations

`docs/plans/semantic-hygiene-checker.PLAN.md` established the original checker posture:

- use `go/analysis`
- keep policy in Go tables for v1
- fail repo-wide with no baseline
- allow primitives at real I/O boundaries
- treat policy diffs as reviewer-controlled

`docs/plans/semantic-hygiene-waivable-diagnostics.PLAN.md` established the structured diagnostic and waiver policy:

- stable dotted rule IDs
- rule metadata
- inline `semh:allow` comments
- hard versus waivable rule classification
- stale, duplicate, malformed, unknown, non-waivable, and over-budget waiver diagnostics

The current checker split must preserve those contracts rather than replacing them with a new warning or baseline model.

## Worktree Observations

At planning time the worktree had unrelated local changes:

- `docs/signals/.order.json`
- `e2e-proxy/proxy_auth_test.go`
- `go.mod`
- `go.sum`
- `.golangci.scout-*.yml`
- `docs/signals/file-not-picked-up.md`
- `docs/signals/wyswig-editor.md`

This planning work must not revert, stage, or fold those changes into the plan artifacts.

## External Research

No external research is needed for this plan. The work is a repo-local static-analysis refactor and policy migration. The relevant technology choices are already present:

- Go `go/analysis`
- `golang.org/x/tools`
- custom golangci-lint module plugin
- Ginkgo/Gomega analyzer fixture tests
- repo-local shell wrappers

## Planning Implications

- The implementation should be split into staged, reviewable units because moving rule families can create many import and fixture changes.
- The first architecture hygiene rule should be the existing e2e-proxy dependency rule because it is hard, narrow, and already covered.
- Shared policy extraction is the main risk. Rule IDs, diagnostic prefixing, and waiver finalization must not diverge across policy families.
- Sentrux boundary migration should happen after `architecturehygiene` exists and is wired into the same gates as the current checker.
- Sentrux metric removal should be explicitly deferred.
