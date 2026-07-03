<!-- leafwiki
version: 1
page:
  id: semantic-hygiene-waivable-diagnostics-decision-20260701
  title: Semantic Hygiene Waivable Diagnostics - Decision
  created_at: "2026-07-01T00:00:00Z"
  updated_at: "2026-07-01T00:00:00Z"
  creator_id: system
  last_author_id: system
tags:
  - plans
  - decision
fields:
  type: refactor
-->

# Semantic Hygiene Waivable Diagnostics - Decision

## Decision

Add analyzer-owned, budgeted, inline waivers to `internal/analysis/semantichygiene`.

The checker remains a hard CI gate. Waivable diagnostics still fail unless a valid waiver suppresses them. Invalid waivers and budget violations are hard failures.

## Selected Course Of Action

1. Introduce structured diagnostic metadata inside the analyzer.
   - Add stable rule IDs.
   - Add rule metadata for severity, waiver eligibility, and waiver scope.
   - Route every checker report through one reporting helper.

2. Keep the existing reviewer command.
   - Continue using `scripts/check-semantic-hygiene.sh`.
   - Continue delegating through `cmd/leafwiki-vet`.
   - Do not add an output-parsing wrapper unless implementation proves `singlechecker` cannot support the needed finalization.

3. Use inline-only waivers.
   - Use `// semh:allow <rule-id> -- <explanation>`.
   - Keep scope local to the next matching AST node or declaration.
   - Reject package-wide and file-wide waivers in v1.

4. Keep budgets in Go policy code.
   - Add a package-local total budget.
   - Add explicit package-local per-rule budgets.
   - Default per-rule budget to zero.
   - Repo-wide budget aggregation is deferred because the current
     `singlechecker` path evaluates packages independently.

5. Keep semantic leaks and dangerous test behavior non-waivable.
   - Waivers are for BDD/readability and quality rules where judgment is legitimate.
   - Hard semantic and safety rules stay hard.

6. Add waiver validation as first-class diagnostics.
   - Unknown rule.
   - Non-waivable rule.
   - Missing explanation.
   - Stale waiver.
   - Duplicate use.
   - Exceeded total budget.
   - Exceeded per-rule budget.

7. Add the first new BDD/readability rule only after waiver infrastructure exists.
   - Use `ginkgo.top-level-it` as the proving rule.
   - Make it waivable.
   - Enforce migrated `Test...` naming and `GinkgoT()`/`t.Fatalf`-style assertions as hard checker rules once supervisor feedback moves them into this slice.
   - Keep longer BDD style rules such as long specs, multiple behaviours, and vague container names deferred until they have accepted rule definitions.

8. Document policy in `docs/typed-ids.md`.
   - Describe errors, waivable diagnostics, waivers, stale waivers, and budgets.
   - Document rule ID stability.
   - Document reviewer ownership.

## Decision Table

| Topic | Decision | Reason |
|---|---|---|
| Suppression model | Inline rule-specific waiver | Keeps justification beside the exceptional code |
| Warning model | No advisory warnings | Non-failing warnings are ignored by agents |
| Rule identity | Stable dotted rule IDs | Diagnostic text can improve without invalidating waivers |
| Rule metadata | Go policy table | Keeps policy compiled, testable, and reviewer-owned |
| Budget storage | Go code in v1 | Avoids a config language before there is evidence for one |
| Scope | Next node or declaration by rule metadata | Local enough to prevent broad suppression |
| CLI shape | Keep `cmd/leafwiki-vet` and script | Preserves current reviewer workflow |
| First proving rule | `ginkgo.top-level-it` | It is clearly judgment-heavy and needs legitimate exceptions |
| Hard-rule boundary | Semantic leaks and unsafe tests are never-waivable | Waivers must not weaken the core contract |

## Alternatives Rejected

### Ordinary warnings

Rejected. Warnings would not fail CI, and agents will learn that they can be ignored.

### Output parser around `go vet` diagnostics

Rejected as the primary design. Waiver matching needs AST positions, comments, rule metadata, and stale-waiver accounting. Parsing formatted output loses the information needed for robust scope checks.

### External config for budgets

Rejected for v1. The waiver policy is reviewer-owned checker policy. Keeping budgets in Go code makes changes visible in the same review lane as the rule metadata and tests.

### File-wide or package-wide waivers

Rejected for v1. They would recreate the broad allowlist failure mode the semantic checker work has repeatedly avoided.

### Keying waivers by diagnostic text

Rejected. Diagnostic text should become more documentation-oriented over time. Waivers need stable identifiers that survive copy edits.

### Making all Ginkgo/Gomega style rules hard

Rejected. BDD readability includes real judgment calls, and a hard-only approach would force artificial containers or matchers in cases where the exception is clearer.

## Resolved Open Questions

- Q: Should waivers be allowed now or postponed?
  - A: Add them now, before adding higher-judgment BDD/readability rules.

- Q: Should semantic leaks be waivable?
  - A: No. Semantic leaks remain hard errors.

- Q: Should budgets live in config?
  - A: Not in v1. Use Go policy tables.

- Q: Should declaration-level diagnostics be waivable?
  - A: Yes, through rule metadata. Expression/statement rules use next-node scope; declaration rules use declaration scope.

- Q: Should suppressed diagnostics print as warnings?
  - A: No. Print a compact waiver summary instead.

- Q: Should rule IDs be generated from diagnostic messages?
  - A: No. Rule IDs are explicit policy constants.

## Implementation Posture

Use TDD.

The first failing tests should exercise waiver parsing and validation without touching existing rule callsites.

The second slice should route one small rule family through structured reporting to prove suppression and stale-waiver accounting.

Only after the infrastructure works should implementation migrate all current rule callsites to structured reports.

The final slice should add the first waivable BDD rule and documentation.
