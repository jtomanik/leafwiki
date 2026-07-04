<!-- leafwiki
version: 1
page:
  id: golangci-lint-local-gate-observe-20260704
  title: Golangci Lint Local Gate - Observe
  created_at: "2026-07-04T00:00:00Z"
  updated_at: "2026-07-04T00:00:00Z"
  creator_id: system
  last_author_id: system
tags:
  - plans
  - observe
fields:
  type: refactor
-->

# Golangci Lint Local Gate - Observe

## Purpose

This document captures the facts behind a local-only implementation plan for making `golangci-lint` the primary source-policy gate for LeafWiki.

The plan title is `golangci-lint-local-gate`.

## Planning Input

This plan follows `docs/plans/planning.aibasic.txt` and creates the expected OODA planning artifacts:

- `docs/plans/golangci-lint-local-gate.OBSERVE.md`
- `docs/plans/golangci-lint-local-gate.CONTEXT.md`
- `docs/plans/golangci-lint-local-gate.DECISION.md`
- `docs/plans/golangci-lint-local-gate.PLAN.md`
- `docs/plans/golangci-lint-local-gate.planning.aibasic.json`

The source thread is `019f2d28-549f-7343-a07c-02d87e169ee5`.

Source thread link: `codex://threads/019f2d28-549f-7343-a07c-02d87e169ee5`.

## Thread Decisions Captured

The planning thread established these decisions:

- Scope is local tooling only; GitHub Actions and CI wiring are out of scope.
- The long-term source-policy acceptance phrase should be "the LeafWiki golangci-lint gate reports no issues".
- `golangci-lint` should own Go/static policy checks, including stock Go linters, `ginkgolinter`, LeafWiki semantic hygiene, and eventually Go-side i18n/catalog policy.
- Runtime gates such as `go test ./...`, frontend lint/build, E2E tests, and install/runtime smoke tests remain separate acceptance gates.
- Shell checker scripts should disappear after parity is proven, or remain only as thin orchestration wrappers with no policy logic.
- LeafWiki should not use a lint baseline. A linter enters the enabled config only once the repo is clean for that linter.
- Implementation must happen in a separate worktree because the current local checkout is still undergoing Ginkgo cleanup.

## Current Worktree Constraint

At planning time, the checkout is on `codex/recover-ginkgo-conversion` and has unrelated dirty files from ongoing Ginkgo cleanup.

The implementation for this plan must not start from the current dirty checkout. It needs a separate worktree or equivalent isolated checkout so the golangci-lint migration can be reviewed independently.

## Current Golangci-Lint State

The repository already has partial golangci-lint artifacts:

- `.github/workflows/backend.yml` invokes `golangci/golangci-lint-action`, but CI use is out of scope for this plan.
- `.golangci.ginkgolinter.yml` is a valid golangci-lint v2 config that enables only `ginkgolinter`.
- `.golangci-lint` exists but is not a valid golangci-lint v2 config filename or file type.

Validation observed during planning:

- `golangci-lint v2.12.2 config verify --config=.golangci.ginkgolinter.yml` succeeds.
- `golangci-lint v2.12.2 run --config=.golangci.ginkgolinter.yml` reports `0 issues` in the root module.
- The same `ginkgolinter` config also reports `0 issues` in `e2e-proxy`.
- `golangci-lint v2.12.2 config verify --config=.golangci-lint` fails with unsupported config type.

The project should replace `.golangci-lint` with a real `.golangci.leafwiki.yml` instead of relying on defaults.

## Current Default Lint Noise

A default `golangci-lint v2.12.2 run` is not clean today.

Observed root-module default findings in the dirty planning checkout:

- `errcheck`: 83
- `govet`: 2
- `ineffassign`: 2
- `staticcheck`: 150
- `unused`: 46

The two `govet` findings came from checked-in frontend `node_modules` Go code under `ui/leafwiki-ui/node_modules/flatted`, which reinforces that local tooling should run against explicit project package sets rather than blanket `./...`.

Observed `e2e-proxy` default findings:

- `errcheck`: 5

Focused observed results:

- Root project packages are clean for `govet` when scoped to `./cmd/... ./internal/... ./e2e/...`.
- Root project packages currently have `ineffassign`, `errcheck`, `unused`, `gocritic`, and `staticcheck` findings.

These findings make a no-baseline rollout necessary. The first gate should enable only clean checks and then add noisy stock linters one cleanup slice at a time.

## Module Boundaries

LeafWiki has two Go modules relevant to this gate:

- Root module: `github.com/perber/wiki` in `go.mod`.
- Nested module: `github.com/perber/wiki/e2e-proxy` in `e2e-proxy/go.mod`.

Plain root-module `go list ./...` does not include `e2e-proxy`.

The local golangci-lint entrypoint must run both modules explicitly.

## Current Semantic Hygiene Shape

The semantic hygiene checker already uses the Go analyzer model:

- `internal/analysis/semantichygiene/analyzer.go` exports `semantichygiene.Analyzer` as `*analysis.Analyzer`.
- `cmd/leafwiki-vet/main.go` is a thin `singlechecker.Main(semantichygiene.Analyzer)` wrapper.
- `cmd/leafwiki-vet/main_test.go` verifies that the command delegates to `semantichygiene.Analyzer`.
- `internal/analysis/semantichygiene/analyzer_test.go` uses `analysistest` fixtures.

The analyzer requires type information:

- The analyzer imports `inspect.Analyzer`.
- Rule code uses `pass.TypesInfo`, `pass.Pkg`, and package-path checks throughout `internal/analysis/semantichygiene`.

A golangci-lint module plugin adapter must return `register.LoadModeTypesInfo`.

## Current Semantic Gate Shell Shape

`scripts/check-semantic-hygiene.sh` currently:

- Creates a temporary `go.work` containing the root module and `e2e-proxy`.
- Runs `go run cmd/leafwiki-vet` over `internal/...`, `cmd/...`, `e2e/...`, and `e2e-proxy/...`.
- Runs `scripts/check-i18n-catalog.sh`.

`scripts/check-typed-id-oracles.sh` delegates to `scripts/check-semantic-hygiene.sh`.

This means the current semantic gate mixes three concerns:

- LeafWiki semantic hygiene analyzer.
- Go i18n/catalog checks.
- Non-Go policy scans embedded inside `scripts/check-i18n-catalog.sh`.

The golangci-lint migration should split these into first-class analyzer/plugin responsibilities before deleting policy-bearing shell scripts.

## Current I18n Catalog Gate Shape

`scripts/check-i18n-catalog.sh` currently enforces:

- Focused localization Go tests.
- `goi18n extract` output matches `internal/localization/locales/active.en.toml`.
- No committed `translate.*` files in the English-only catalog phase.
- E2E TypeScript assertions do not pin behavior to migrated localized prose.
- `gin.H` API payloads include structured localized error/message metadata.
- `scripts/run.sh` failure bodies use generated catalog messages.

The built-in golangci-lint `gosmopolitan` linter does not cover this policy.

Observed during planning:

- `golangci-lint v2.12.2 run --enable-only=gosmopolitan` over focused localization/MCP/CLI packages reported `0 issues`.
- That result is expected because `gosmopolitan` checks broad i18n/l10n anti-patterns such as watched Unicode-script literals and local-time usage, not go-i18n catalog drift or LeafWiki message-ID policy.

## External Tooling Facts

Official golangci-lint module plugin documentation states:

- Define `.custom-gcl.yml`.
- Run `golangci-lint custom`.
- Configure the plugin under `linters.settings.custom` with `type: module`.
- Run the resulting custom binary, `./custom-gcl` by default.

The module plugin registration contract comes from `github.com/golangci/plugin-module-register/register`:

- `register.Plugin(name, New)` registers a plugin.
- `New(settings any) (register.LinterPlugin, error)` constructs it.
- `BuildAnalyzers() ([]*analysis.Analyzer, error)` returns analyzers.
- `GetLoadMode() string` returns either syntax-only or type-info load mode.

The example module plugin returns a custom `analysis.Analyzer` and uses `register.LoadModeSyntax` for syntax-only checks. LeafWiki needs `register.LoadModeTypesInfo`.

## Existing Documentation Contracts

`docs/typed-ids.md` currently points reviewers to `scripts/check-semantic-hygiene.sh`.

`docs/i18n.md` currently points contributors to:

- `goi18n extract`.
- `scripts/check-i18n-catalog.sh`.
- `scripts/check-semantic-hygiene.sh`.

These docs will need to move toward the local golangci-lint entrypoint after parity is proven.

## Constraints

- Do not implement this migration in the dirty Ginkgo cleanup checkout.
- Do not add CI wiring in this plan.
- Do not introduce a lint baseline file.
- Do not remove shell checkers until their diagnostics are reproduced by golangci-lint.
- Do not weaken `semantichygiene` policy while adapting it to golangci-lint.
- Do not treat `golangci-lint` as a replacement for runtime tests, frontend lint/build, E2E tests, or install smoke tests.
- Keep file paths repo-relative in planning artifacts so the plan works from a separate worktree.
