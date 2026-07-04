<!-- leafwiki
version: 1
page:
  id: golangci-lint-local-gate-decision-20260704
  title: Golangci Lint Local Gate - Decision
  created_at: "2026-07-04T00:00:00Z"
  updated_at: "2026-07-04T00:00:00Z"
  creator_id: system
  last_author_id: system
tags:
  - plans
  - decision
fields:
  type: refactor
-->

# Golangci Lint Local Gate - Decision

## Decision

Introduce a local-only LeafWiki golangci-lint gate built around a custom module-plugin binary, then migrate static source-policy shell checkers into that gate without using a baseline.

Implementation must happen in a separate worktree because the current checkout is still carrying unrelated Ginkgo cleanup work.

## Selected Course Of Action

1. Create a local custom golangci-lint binary.
   - Add `.custom-gcl.yml`.
   - Name the binary `leafwiki-golangci-lint`.
   - Store generated binaries under `.cache/tools` or an equivalent ignored cache path.
   - Pin the golangci-lint version.

2. Add a LeafWiki module plugin adapter.
   - Use `tools/golangci/leafwiki`.
   - Register a custom plugin named `leafwiki`.
   - Return `semantichygiene.Analyzer` first.
   - Use `register.LoadModeTypesInfo`.
   - Keep analyzer policy under `internal/analysis`.

3. Replace the invalid broad config with `.golangci.yml`.
   - Use golangci-lint v2 config.
   - Enable only clean checks at first.
   - Keep `.golangci.ginkgolinter.yml` only until the unified config has proven parity.

4. Add one local entrypoint.
   - Prefer `make lint` backed by `scripts/golangci-lint.sh`.
   - Run both root and `e2e-proxy` modules.
   - Keep the wrapper free of policy logic.

5. Migrate source-policy shell checkers by parity.
   - First migrate `semantichygiene`.
   - Then migrate Go-side i18n/catalog checks.
   - Then decide whether the non-Go i18n scans belong in a custom analyzer or a separate non-golangci quality gate.
   - Remove or demote shell checkers only after parity is proven.

6. Roll stock linters in without a baseline.
   - Enable one noisy linter only after its current findings are fixed.
   - Use separate cleanup slices for large categories.

7. Keep CI out of scope.
   - Do not modify `.github/workflows/`.
   - Do not plan around the existing GitHub Actions invocation.
   - The plan is local-tooling only.

## Decision Table

| Topic | Decision | Reason |
|---|---|---|
| Scope | Local tooling only | User explicitly excluded CI |
| Custom analyzer integration | Golangci-lint module plugin | Existing checker is already `go/analysis` shaped |
| Plugin package location | `tools/golangci/leafwiki` | Importable by custom binary while still able to import LeafWiki `internal` analyzer code |
| Plugin name | `leafwiki` | Allows multiple LeafWiki analyzers behind one config entry |
| Load mode | Type info | `semantichygiene` relies on `pass.TypesInfo` and `pass.Pkg` |
| Config file | `.golangci.yml` | Current `.golangci-lint` file is invalid for v2 |
| Baseline | None | User wants the repo to meet enabled lint contracts, not freeze existing violations |
| Module execution | Explicit root plus `e2e-proxy` runs | Root `./...` does not include nested module |
| i18n built-in support | Enable `gosmopolitan` only as a smell check | It does not validate LeafWiki's go-i18n catalog/message policy |
| Shell checker removal | After parity | Prevents losing diagnostics during migration |
| Implementation checkout | Separate worktree | Current local checkout is dirty with unrelated Ginkgo cleanup |

## Alternatives Rejected

### Enable golangci-lint defaults and clean everything immediately

Rejected. The current default run has hundreds of findings, and a large all-at-once cleanup would collide with the active Ginkgo cleanup branch.

### Use a baseline file

Rejected. A baseline would weaken the acceptance phrase into "no new issues" instead of "the gate reports no issues".

### Keep `cmd/leafwiki-vet` as a permanent sibling command

Rejected as the end state. It is acceptable during transition, but the goal is one local static-policy gate.

### Move analyzer code out of `internal/analysis`

Rejected. Only the golangci-lint adapter needs to be importable from the custom binary. The policy analyzer should stay internal and reviewer-owned.

### Use `gosmopolitan` as the go-i18n catalog checker

Rejected. It detects broad i18n/l10n anti-patterns and can exempt `i18n.Message` literals, but it does not compare `active.en.toml`, validate `messageId` payload policy, or enforce LeafWiki's English-only catalog phase.

### Delete shell checkers before parity

Rejected. The current shell scripts encode real policy. Deleting them first would create a silent coverage regression.

### Put cross-language checks into golangci-lint at any cost

Rejected. Static Go policy belongs in golangci-lint. Non-Go checks can move there only when the analyzer shape is clear and not more fragile than the script it replaces.

## Resolved Open Questions

- Q: Is CI part of this migration?
  - A: No. Local tooling only.

- Q: Should `semantichygiene` become a golangci-lint plugin?
  - A: Yes, through a module plugin adapter.

- Q: Should `cmd/leafwiki-vet` disappear immediately?
  - A: No. Keep it during parity and remove or demote it later.

- Q: Should all existing linter findings be baselined?
  - A: No.

- Q: Does golangci-lint already support `go-i18n` catalog checks?
  - A: No. `gosmopolitan` is related but not equivalent.

- Q: Should runtime tests become part of golangci-lint?
  - A: No. Tests remain separate gates.

- Q: Can implementation happen in the current checkout?
  - A: No. Use a separate worktree for the implementation.

## Implementation Posture

Use a reversible first slice.

Start by proving the custom binary can run the existing semantic analyzer through golangci-lint and report the same classes of diagnostics as `cmd/leafwiki-vet`.

Do not start by cleaning noisy stock linters or rewriting i18n policy. Those become follow-up units after the custom gate exists.
