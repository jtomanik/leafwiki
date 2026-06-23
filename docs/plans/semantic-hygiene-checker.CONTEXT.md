<!-- leafwiki
version: 1
page:
  id: semantic-hygiene-checker-context-20260622
  title: Semantic Hygiene Checker - Context
  created_at: "2026-06-22T13:23:32Z"
  updated_at: "2026-06-22T13:23:32Z"
  creator_id: system
  last_author_id: system
tags:
  - plans
  - context
fields:
  type: refactor
-->

# Semantic Hygiene Checker - Context

## Problem Frame

The semantic-types work is intended to make primitive ambiguity structurally harder. The goal is not satisfied when code introduces `PageID`, `WorkspaceID`, or `RoutePath` types but then routinely unwraps them with `.String()` and calls raw-string helpers in the middle of domain logic.

The failure mode is subtle:

- The code appears to have semantic types.
- The compiler still permits most of the old stringly flows.
- Reviewers must manually recognize every bad conversion pattern.
- Implementers can fix one example while leaving the same abstraction leak elsewhere.
- Future coding agents learn from the mixed pattern and continue it.

The needed outcome is an executable review contract: after a raw value is parsed or assigned domain meaning, internal code should use semantic operations, not representation-aware string plumbing.

## Spirit Of The Law

The semantic-types plan is about semantic abstraction, not just renamed string aliases.

When code compares two page IDs, it should express "same page identity," not "same backing string." When code looks up a page by ID, the lookup API should accept a `PageID`, not force every caller to remember that `PageID` happens to serialize as a string. When code validates a route path, the validated result should be a `RoutePath`, not a string that another layer must recast.

The checker should therefore flag patterns that preserve primitive coupling even when the plan did not enumerate every specific field or function.

## Policy Terms

Semantic type:

- A defined Go type whose purpose is to carry domain meaning over a primitive representation.
- Examples: `tree.PageID`, `tree.RoutePath`, `tree.Slug`, `tree.AssetName`, `workspaceid.WorkspaceID`, runtime session IDs, MCP tool IDs, error codes, message IDs, validation issue codes, and similar local domain types.

Boundary:

- A place where values naturally enter or leave the typed domain.
- Examples: HTTP params, JSON DTOs, MCP payloads, CLI flags, env vars, database rows, filesystem paths, frontmatter, logs, and user-authored Markdown.

Semantic leak:

- A conversion, signature, field, comparison, or assignment that exposes a semantic value's primitive representation in internal domain code.

Allowed primitive escape:

- A conversion where the target is a true boundary sink, such as JSON output, URL generation, persistence, logging, an error string, or a test assertion over wire output.

Reviewer-owned policy:

- The rule set and allow categories that define what the checker reports. Implementers may propose changes, but reviewers decide whether those changes preserve the plan's intent.

## Scope

In scope:

- A repo-local Go analyzer built with `go/analysis`.
- A thin repo-local runner under `cmd/leafwiki-vet`.
- Analyzer unit tests using `analysistest`.
- A script that makes the checker easy to run during review.
- Policy tables in Go code for semantic types, edge packages, allowed sinks, and forbidden patterns.
- Docs explaining how reviewers extend the checker.
- Refactoring current semantic-types implementation leaks until the checker passes repo-wide.

Out of scope:

- `golangci-lint` integration.
- Auto-fixes.
- A global ID framework.
- Typing every local primitive.
- Replacing JSON, MCP, CLI, database, or file formats.
- Proving whole-program dataflow validation with SSA or taint tracking.

## Ownership Model

The checker is implemented in the repo because the implementer needs it to make the PR pass. The policy is reviewer-owned because it defines the review contract.

Practical ownership rules:

- Policy files are ordinary code and may change in the PR.
- Reviewers inspect policy diffs before accepting any "green" checker result.
- Implementers may add narrow allow categories when the checker is too noisy, but each allow category must include a fixture proving the allowed case and a fixture proving the nearby forbidden case.
- The checker must not add a broad per-package or per-directory silence to make current code pass.
- The checker must not introduce a baseline file of accepted violations.

## Rule Philosophy

The checker should prefer rules that are local, explainable, and hard to bypass accidentally.

Good diagnostics:

- Point to a specific primitive leak.
- Name the semantic type or policy category involved.
- Explain the expected shape, such as "make the callee accept tree.PageID" or "parse at the boundary and carry RoutePath."
- Avoid telling the implementer to add a direct cast as the fix.

Bad diagnostics:

- Ban all strings in a package.
- Ban every `.String()` call.
- Require a semantic type for user-authored copy.
- Fire on DTO output without showing a domain leak.

## Rule Categories

Hard-fail rules for v1:

- Semantic `.String()` passed into an internal domain/service/helper call where the callee should accept the semantic type.
- Semantic `.String()` used in equality comparisons against semantic-looking fields in domain code.
- Semantic `.String()` assigned into semantic-looking domain fields.
- Direct semantic type casts outside approved parser, constructor, edge adapter, or test fixture contexts.
- Validators for known semantic values returning raw primitives instead of semantic types.
- Internal service/use-case/helper function parameters named like semantic values but typed as `string`, `[]string`, or `map[string]...`.
- Internal structs named like domain data carrying semantic-looking fields as raw strings outside DTO, storage, or wire packages.
- Stable error/message/tool/issue IDs emitted as raw literals where typed constants or definitions exist.

Review-output or later hardening rules:

- Adjacent raw primitive siblings where naming suggests a missing semantic type but the correct target type is not yet known.
- Sentinel string literals in semantic ID logic.
- English-copy assertions used as machine contracts outside Go code, kept initially in the shell oracle.

## Allow Categories

Allowed primitive conversions must be explicit enough to review:

- JSON response/request DTO construction.
- MCP wire payload construction and parsing.
- HTTP route path, query, and form extraction before parsing.
- CLI/env/config parsing before validation.
- Database or filesystem persistence formats.
- Markdown/frontmatter/user-content serialization.
- URL construction.
- Logging and error text.
- Tests that assert exact wire output, persisted output, user-visible copy, or fixture contents.

The analyzer should encode these as policy predicates rather than blanket ignores. For example, `.String()` in a JSON struct literal can be allowed, while `.String()` passed to `FindPageByID` is not.

## Integration Model

The initial runner should be:

```text
cmd/leafwiki-vet
```

The initial analyzer package should be:

```text
internal/analysis/semantichygiene
```

The review script should be:

```text
scripts/check-semantic-hygiene.sh
```

`scripts/check-typed-id-oracles.sh` should remain only for checks that are not yet type-aware, such as English-copy assertions in frontend and E2E files, or it should call the new script as a compatibility wrapper. The semantic-boundary rules should not remain primarily regex-based.

## Compatibility

The checker should not force wire-format changes. JSON and MCP output can still serialize semantic values as strings. The important distinction is where conversion happens.

Valid shape:

```text
HTTP input string -> parse/validate -> semantic type through domain code -> serialize string at output
```

Invalid shape:

```text
HTTP input string -> cast to semantic type -> immediately .String() -> raw-string service API -> compare strings in domain code
```

## Risks And Mitigations

| Risk | Mitigation |
|---|---|
| False positives block useful review progress | Add narrow allow categories with positive and negative fixtures |
| Policy is weakened to pass the PR | Make policy diffs explicit review items and avoid baselines |
| Analyzer duplicates the brittle shell oracle | Use `go/types` for Go rules and leave regex only for non-Go text assertions |
| Implementer fixes examples but not pattern | Require repo-wide checker pass and fixture coverage for rule classes |
| Refactor becomes too broad | Focus on middle-layer semantic leaks first, not every primitive |
| Checker is hard to run | Provide `scripts/check-semantic-hygiene.sh` and document exact `rtk` commands |

## Success Shape

Maintainable code after this plan should read as if semantic types are real domain abstractions:

- Lookups take semantic IDs.
- Equality is expressed through typed comparison or methods.
- Metadata writes accept semantic values or typed setters.
- Validators return semantic values.
- Boundary code performs parsing and serialization.
- Tests assert stable machine IDs instead of English where the behavior is machine-contract behavior.
