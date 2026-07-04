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

The first migration slice is tooling-complete when it installs the local gate
and reports current findings without hiding them. It does not fix the findings
reported by `leafwiki`; making the acceptance phrase "the LeafWiki golangci-lint
gate reports no issues" true belongs to later cleanup work. Do not narrow the
package surface, add suppressions, or add a baseline to make this first branch
look green.

Deferred stock-linter cleanup order:

1. `ineffassign`
2. `unused`
3. `errcheck`
4. `gocritic`
5. `staticcheck`

The i18n/catalog policy remains in `scripts/check-i18n-catalog.sh` until the
catalog and non-Go scans can move into analyzer-backed checks without weakening
coverage. Do not replace this with a lint baseline.
