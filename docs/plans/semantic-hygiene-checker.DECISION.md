<!-- leafwiki
version: 1
page:
  id: semantic-hygiene-checker-decision-20260622
  title: Semantic Hygiene Checker - Decision
  created_at: "2026-06-22T13:23:32Z"
  updated_at: "2026-06-22T13:23:32Z"
  creator_id: system
  last_author_id: system
tags:
  - plans
  - decision
fields:
  type: refactor
-->

# Semantic Hygiene Checker - Decision

## Decision

Create a new implementation plan, `semantic-hygiene-checker`, that adds a repo-local, reviewer-owned Go `go/analysis` analyzer and uses it to enforce the semantic-types contract repo-wide.

The checker will be part of the same semantic-types PR review cycle. It is not a later cleanup ticket and it does not use a violation baseline.

## Selected Course Of Action

1. Add a repo-local analyzer package.
   - Use `internal/analysis/semantichygiene`.
   - Use `golang.org/x/tools/go/analysis`.
   - Use `inspect.Analyzer` and `pass.TypesInfo` so rules understand actual named types.

2. Add a thin runner.
   - Use `cmd/leafwiki-vet`.
   - Start with one analyzer.
   - Prefer a runner name that can grow into multiple repo-local checks later.

3. Keep policy code-first for v1.
   - Store semantic type definitions, allowed edge categories, and forbidden sinks in Go tables.
   - Do not introduce YAML in this slice.
   - Reason: policy code is compiled, tested, easy to review in diffs, and less likely to become a second weak configuration language.

4. Add hygiene tests.
   - Use `analysistest` fixtures.
   - Every rule class needs a failing fixture and at least one nearby allowed fixture.
   - Fixtures should demonstrate intent, not just current implementation lines.

5. Add a reviewer-facing script.
   - Use `scripts/check-semantic-hygiene.sh`.
   - It should run the analyzer with `rtk` and fail on diagnostics.
   - It should be documented as a review check.

6. Re-scope the existing shell oracle.
   - Move Go semantic-boundary enforcement into the analyzer.
   - Keep `scripts/check-typed-id-oracles.sh` for non-Go text assertions and any transitional checks that are still valuable.
   - Avoid duplicate, divergent policies.

7. Refactor current semantic leaks until the checker passes.
   - Do not satisfy diagnostics by adding casts or broader allowlists.
   - Prefer typed service APIs, typed validators, typed setters, and semantic comparison methods.

8. Document the policy.
   - Update `docs/typed-ids.md`.
   - Explain how to extend the checker during review.
   - State clearly that reviewer approval is required for policy weakening.

## Decision Table

| Topic | Decision | Reason |
|---|---|---|
| Tool family | Go `go/analysis` | It can use AST and type information instead of brittle regex |
| Location | `internal/analysis/semantichygiene` | Keeps checker internal and separate from runtime domains |
| Runner | `cmd/leafwiki-vet` | Leaves room for future repo-local analyzers |
| Policy format | Go tables, not YAML | Easier to compile-test and harder to quietly weaken |
| First integration | Script plus direct `go run` | Fits current repo script conventions without adding golangci-lint |
| Golangci-lint | Defer | Useful later, not needed for the review tool |
| Baseline | None | The user explicitly wants linting for the codebase to pass |
| Auto-fixes | None | Correct fixes often require API design, not mechanical replacement |
| Analyzer strength | Hard fail for known leak classes | Reviewer needs an enforceable gate |
| Policy ownership | Reviewer-controlled by review | Implementer may propose changes but cannot redefine done |
| Shell oracle | Narrow or delegate | Regex is still useful for non-Go text assertions, not Go semantic boundaries |

## Alternatives Rejected

### Keep improving `scripts/check-typed-id-oracles.sh`

Rejected. Regex can catch obvious text but cannot reliably know whether `.String()` is serializing a response or feeding an internal domain lookup. The review loop has already shown that regex and manual inspection are not enough.

### Add `golangci-lint` now

Rejected for this slice. It is a good future integration path, but the immediate need is a small repository-owned analyzer that reviewers can evolve during this review.

### Use YAML policy

Rejected for v1. YAML is easier to read, but it introduces another parser and makes it easier to pass the tool by editing data instead of rule intent. Go policy tables are simple enough here and keep policy behavior testable.

### Add a baseline file

Rejected. A baseline would turn the tool into "do not make this worse." The user wants the codebase to meet the semantic-types contract.

### Ban all `.String()` calls on semantic types

Rejected. Serialization, persistence, URL construction, logs, errors, and wire-output tests legitimately convert semantic values to strings. The rule must identify representation leaks, not serialization.

### Add a global semantic values package

Rejected for this plan. The semantic-types plan chose domain-owned types. The checker should enforce that direction rather than re-open type ownership.

## Resolved Open Questions

- Q: Should this be part of the current PR or a separate later ticket?
  - A: Current PR. The checker is a review tool needed to finish the semantic-types implementation truthfully.

- Q: Should the checker scan only changed files?
  - A: No. It must scan the repo without a baseline.

- Q: Can implementers change checker rules?
  - A: They can propose changes in the PR, but reviewers decide whether the policy still matches the contract. Policy diffs are review-critical.

- Q: Should policy live in YAML or code?
  - A: Code-first in v1.

- Q: Are direct casts ever allowed?
  - A: Yes, but only in approved boundary/parser/test contexts. Internal domain code should not cast raw strings into semantic meaning.

- Q: Is `.String()` ever allowed?
  - A: Yes, at serialization and other true primitive sinks. It should not be used to call internal APIs that should accept semantic values.

## Implementation Posture

This is a guardrail implementation plus follow-on refactor. The checker should be written first with fixtures that reproduce known bad patterns. Then the current semantic-types implementation should be refactored until the checker passes repo-wide.

The plan should be executed as test-driven tool work:

1. Add failing analyzer fixtures.
2. Implement enough analyzer logic for those fixtures.
3. Run the analyzer against the real repo.
4. Refactor code, not policy, until diagnostics are resolved.
5. Add narrowly reviewed policy exceptions only when the diagnostic is truly a boundary.
