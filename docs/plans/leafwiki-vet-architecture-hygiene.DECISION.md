<!-- leafwiki
version: 1
page:
  id: leafwiki-vet-architecture-hygiene-decision-20260705
  title: LeafWiki Vet Architecture Hygiene Decision
  created_at: "2026-07-05T00:00:00Z"
  updated_at: "2026-07-05T00:00:00Z"
  creator_id: codex
  last_author_id: codex
tags:
  - plans
fields:
  type: refactor
-->

# LeafWiki Vet Architecture Hygiene Decision

## Decision

Create an architecture hygiene policy family for `leafwiki-vet`, move the existing dependency-direction rule into it, extract test-specific checker rules into a `testhygiene` family, keep semantic-value and i18n policy in `semantichygiene`, and then port the Sentrux import-boundary rules into `architecturehygiene`.

The project checker should become an umbrella gate. The plan should not add more architecture policy to the current semantic package.

## Chosen Shape

The target module families are:

- `semantichygiene`: semantic values, typed IDs, raw primitives, direct casts, i18n/message/catalog contract policy, and stable literals.
- `testhygiene`: Ginkgo/Gomega/taxonomy/BDD readability policy and waivable test-quality diagnostics.
- `architecturehygiene`: import boundaries, dependency direction, black-box E2E boundaries, and later dependency-inversion-shaped checks.
- `checkerpolicy`: shared rule metadata, diagnostic formatting, waiver parsing, waiver finalization, and helper infrastructure used by policy families.

`cmd/leafwiki-vet` remains the command. `scripts/check-semantic-hygiene.sh` remains a compatibility wrapper during the migration.

## Key Decisions

1. Use `leafwiki-vet` as the stable public gate.
   - Reason: scripts, reviewers, and local habits already depend on that surface.

2. Split by policy family, not by implementation file size.
   - Reason: the current `semantichygiene` package already has cohesive files; the remaining problem is conceptual ownership.

3. Keep rule IDs stable during the split.
   - Reason: diagnostics, fixtures, docs, and waivers already use dotted rule IDs.

4. Keep `semh:allow` waiver syntax for this implementation.
   - Reason: changing waiver syntax while moving rule families adds avoidable migration risk.

5. Move the existing `dependency.e2e-proxy-internal-import` rule before adding new architecture rules.
   - Reason: it proves the architecture family with an existing hard rule and existing fixture coverage.

6. Port only Sentrux import-boundary rules in this plan.
   - Reason: those rules are architectural contracts and fit `go/analysis`; metric constraints are deferred.

7. Do not remove Sentrux in this plan.
   - Reason: the first objective is to move high-value checks into LeafWiki's checker, not to remove every Sentrux signal at once.

8. Update both direct and golangci runner surfaces.
   - Reason: `cmd/leafwiki-vet` and `tools/golangci/leafwiki` must agree on the analyzer set.

9. Use analyzer fixtures as the acceptance proof for rule migration.
   - Reason: checker policy is code; every moved or new hard rule needs red/green coverage.

10. Treat dependency-inversion rules as follow-on after import-boundary parity.
    - Reason: import boundaries are objective and already configured; DIP-shaped checks need more classification and exceptions.

## Alternatives Considered

### Keep everything in `semantichygiene`

Rejected. This preserves short-term convenience but makes the checker name increasingly misleading. Adding Sentrux-derived architecture rules there would make the package an unstructured grab bag.

### Rename `semantichygiene` to `leafwikihygiene`

Rejected for this slice. A broad rename would create churn before the rule-family split proves itself. A later naming cleanup can be planned after the package boundaries are real.

### Add only `architecturehygiene` and leave test rules in `semantichygiene`

Partially accepted as an intermediate state, not as the target. It is a good first implementation checkpoint because architecture rules are hard and narrow. It does not complete the conceptual split.

### Implement all Sentrux metrics in Go

Rejected. The Sentrux boundary table is a good architecture policy source. Cycle counts, coupling grades, cyclomatic complexity, and god-file detection are metric ratchets and need a separate decision.

### Remove Sentrux immediately

Rejected. The plan should first move the checks the team actually wants to own in LeafWiki. Tool removal belongs after parity and confidence are established.

### Change diagnostic prefix and waiver syntax now

Rejected. Naming cleanup is real but secondary. Current syntax is part of reviewer muscle memory and fixture expectations.

## Open Questions Resolved

- Q: Should the split happen before architecture extension?
  - A: Yes, but the first split proof should be the existing dependency rule so `architecturehygiene` is real before new boundary rules land.

- Q: Should `architecturehygiene` enforce generic SOLID dependency inversion?
  - A: No. It should enforce project-specific architecture contracts. DIP-shaped checks can follow once package roles and composition roots are classified.

- Q: Should Sentrux metric constraints be ported now?
  - A: No. Port import-boundary rules first and defer metric parity.

- Q: Should rule IDs change when rules move packages?
  - A: No. Ownership changes should not change the reviewer-facing rule contract.

## Deferred Questions

- Whether the final diagnostic prefix should remain `semh:` or become a broader project-checker prefix.
- Whether `leafwiki-vet` should eventually expose separate analyzer names or one umbrella analyzer name.
- Whether Sentrux metrics should be replaced, dropped, or delegated to another local gate.
- Which dependency-inversion-shaped checks are precise enough to hard-fail after import-boundary parity is in place.

## Implementation Strategy

The implementation should be delivered as a sequence of narrow, reviewable units:

1. Extract or introduce shared checker policy infrastructure while preserving current behavior.
2. Create `architecturehygiene` and move the current e2e-proxy dependency rule there.
3. Update `cmd/leafwiki-vet` and `tools/golangci/leafwiki` to run the expanded checker set.
4. Extract Ginkgo/Gomega/taxonomy rules into `testhygiene` after shared policy is available.
5. Leave semantic and i18n rules in `semantichygiene`.
6. Port Sentrux import-boundary rules to `architecturehygiene`.
7. Update docs and verification guidance.

## Success Criteria

- `leafwiki-vet` is visibly a project policy checker, not a single semantic analyzer.
- Existing dependency-rule behavior is preserved under `architecturehygiene`.
- `testhygiene` owns repo-specific test policy.
- `semantichygiene` is narrowed to semantic/i18n/contract policy.
- Sentrux import-boundary rules exist as tested hard rules in `architecturehygiene`.
- Sentrux itself remains installed/configured until a later removal decision.
