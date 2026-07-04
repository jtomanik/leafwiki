<!-- leafwiki
version: 1
page:
  id: golangci-lint-local-gate-context-20260704
  title: Golangci Lint Local Gate - Context
  created_at: "2026-07-04T00:00:00Z"
  updated_at: "2026-07-04T00:00:00Z"
  creator_id: system
  last_author_id: system
tags:
  - plans
  - context
fields:
  type: refactor
-->

# Golangci Lint Local Gate - Context

## Problem Frame

LeafWiki has accumulated several policy checkers that behave like source linters but are not presented through one local acceptance gate.

The current user-facing goal is not "add golangci-lint somewhere". The goal is a clear local source-policy gate where new work can be judged by one sentence: the LeafWiki golangci-lint gate reports no issues.

That gate must be honest. It must not hide existing findings behind a baseline, and it must not claim to replace tests or runtime checks that golangci-lint cannot reasonably own.

## Strategic Direction

Use golangci-lint as the local static-policy runner.

This gives contributors and agents one command surface for:

- Stock Go linters.
- Ginkgo/Gomega mechanics.
- LeafWiki semantic hygiene.
- LeafWiki Go-side i18n and catalog policy.
- Eventually selected cross-file source-policy scans that are currently embedded in shell scripts.

Keep runtime and behavioral gates separate:

- `go test ./...`
- frontend lint/build
- E2E tests
- installer/runtime smoke tests

The final model is "golangci-lint owns static source-policy checks; tests own behavior".

## Why a Custom Module Plugin Is the Right Shape

LeafWiki's semantic checker is already a `go/analysis` analyzer.

That makes a golangci-lint module plugin a packaging change rather than a rewrite:

- The existing analyzer remains in `internal/analysis/semantichygiene`.
- A small adapter package registers it with `github.com/golangci/plugin-module-register`.
- `.custom-gcl.yml` builds a custom `leafwiki-golangci-lint` binary.
- `.golangci.leafwiki.yml` enables the custom linter like any other golangci-lint linter.

This keeps the analyzer reviewer-owned, typed, and fixture-tested while moving execution into the standard local lint gate.

## Package Visibility Consideration

The adapter package should live outside `internal/`.

The generated custom golangci-lint binary imports the plugin package from outside the `github.com/perber/wiki` tree. A package such as `tools/golangci/leafwiki` is importable by that custom binary and can still import `github.com/perber/wiki/internal/analysis/semantichygiene` because it is inside the LeafWiki module.

The analyzer package itself should stay under `internal/analysis/semantichygiene`.

## Plugin Shape

Register one LeafWiki plugin rather than one plugin per policy rule family.

Recommended shape:

- Plugin name: `leafwiki`.
- Adapter package: `tools/golangci/leafwiki`.
- Initial analyzers returned by `BuildAnalyzers()`:
  - `semantichygiene.Analyzer`.
- Later analyzers returned by the same plugin:
  - `i18ncatalog.Analyzer` or equivalent.

This keeps `.golangci.leafwiki.yml` readable and avoids a proliferation of custom linter names.

The current analyzer name `semantichygiene` should remain stable because diagnostics, tests, and existing vocabulary already use it.

## Local Entrypoint Shape

Use a repo-local wrapper script or Makefile target to hide custom-binary build details.

The direct binary cannot be assumed to exist globally. The local entrypoint should:

- Ensure or build the pinned custom golangci-lint binary.
- Run the root module against explicit project package patterns.
- Run `e2e-proxy` from its module directory.
- Use the same config file for both module runs.

This wrapper is orchestration only. Policy lives in `.golangci.leafwiki.yml` and analyzer code.

## Config Shape

Replace the invalid `.golangci-lint` file with `.golangci.leafwiki.yml`.

The first enabled set should be green:

- `leafwiki` custom plugin.
- `ginkgolinter`.
- `govet`, scoped through the wrapper to project packages.
- `gosmopolitan`, if it remains green and its limited scope is documented.

Do not enable `errcheck`, `unused`, `staticcheck`, `gocritic`, or `ineffassign` until their current findings are fixed.

Do not carry a baseline file.

## I18n Migration Shape

There are two separate i18n concerns:

1. General i18n anti-patterns.
2. LeafWiki's catalog/message-ID contract.

`gosmopolitan` covers only the first concern. It can be enabled as a broad smell check, but it does not replace `scripts/check-i18n-catalog.sh`.

LeafWiki-specific i18n migration should move in stages:

- Move Go emission policy into `semantichygiene` where it already belongs.
- Add a catalog analyzer that compares extractable message registry data to `active.en.toml`.
- Add a repo-file policy analyzer for non-Go scans only if the team still wants those checks inside golangci-lint after the Go-side checks are stable.

The catalog analyzer should not shell out to `goi18n extract` from inside golangci-lint. It should parse the relevant Go source and catalog files directly, or use importable library functionality if implementation finds a reliable import path.

## Shell Script Retirement Path

The shell checkers should not be deleted first.

Use this transition sequence:

1. Add golangci-lint execution with the same semantic analyzer.
2. Prove semantic diagnostics match the current shell gate.
3. Move i18n/catalog policy into analyzers.
4. Convert shell checkers into compatibility wrappers that call the golangci-lint entrypoint.
5. Delete wrappers only after documentation and local workflows have stopped depending on them.

This prevents a silent coverage regression while still moving toward one local static-policy gate.

## Stock Linter Rollout Shape

Noisy stock linters are cleanup slices, not config toggles.

Recommended enablement order:

1. `ineffassign`, because the observed count is small.
2. `unused`, because many findings are likely Ginkgo migration residue.
3. `errcheck`, because it has behavior-risk implications but requires deliberate close/error handling policy.
4. `gocritic`, because it is mixed style/performance/readability.
5. `staticcheck`, because it is large and includes both useful bugs and style suggestions.

Each linter should enter `.golangci.leafwiki.yml` only after the current repo is clean for that linter in both Go modules or after the scope is intentionally limited with a documented rationale.

## Worktree Strategy

The implementation must start in a separate worktree.

The plan should not prescribe the exact branch name, but it should require:

- Isolated worktree from an appropriate base branch.
- No dependence on the current dirty Ginkgo cleanup checkout.
- No staging or reverting unrelated Ginkgo cleanup changes.
- Validation from the implementation worktree before any commit.

## Architecture Consequences

The plan introduces a local static-policy toolchain layer:

- `.custom-gcl.yml` defines how to build the custom binary.
- `.golangci.leafwiki.yml` defines the enabled lint contract.
- `tools/golangci/leafwiki` adapts LeafWiki analyzers to golangci-lint.
- `scripts/golangci-lint.sh` or `make lint` becomes the local source-policy command.

The analyzer layer remains reviewer-owned under `internal/analysis`.

The shell-script layer moves down to temporary orchestration only.

## Open Planning Questions

No launch-blocking architectural questions remain.

Implementation-time decisions remain:

- Exact custom binary cache path and rebuild invalidation details.
- Whether the i18n catalog analyzer imports reusable go-i18n code or implements a small purpose-built extractor.
- Whether non-Go i18n scans are worth moving into a Go analyzer or should remain outside the golangci-lint static-policy gate until later.

These are implementation details because they can be resolved against the code once the first plugin slice exists.
