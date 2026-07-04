# Golangci-Lint Rollout

The local LeafWiki source-policy gate must become clean without a baseline.
Noisy stock linters must enter the gate one cleanup slice at a time after both
the root module and `e2e-proxy` are clean for that linter.

Current gate status:

| Check | Status | Notes |
| --- | --- | --- |
| `leafwiki` | Configured, red | The custom module plugin runs semantic hygiene through golangci-lint, but the broad gate currently reports existing diagnostics in the root module and `e2e-proxy`. Do not narrow the package surface, add suppressions, or add a baseline to hide them. |
| `ginkgolinter` | Clean | Verified through the transition config in both modules. |
| `govet` | Clean when scoped | Scoped to project package patterns by `scripts/golangci-lint.sh` to avoid unrelated frontend dependency trees. |

The first migration slice is therefore infrastructure-complete but not DoD-clean:
`leafwiki` must be made green before the acceptance phrase "the LeafWiki
golangci-lint gate reports no issues" is true. Some current diagnostics are
under `internal/analysis/`, which is outside this thread's allowed write scope.

Deferred stock-linter cleanup order:

1. `ineffassign`
2. `unused`
3. `errcheck`
4. `gocritic`
5. `staticcheck`

The i18n/catalog policy remains in `scripts/check-i18n-catalog.sh` until the
catalog and non-Go scans can move into analyzer-backed checks without weakening
coverage. Do not replace this with a lint baseline.
