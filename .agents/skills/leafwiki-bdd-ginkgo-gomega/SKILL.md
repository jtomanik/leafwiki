---
name: leafwiki-bdd-ginkgo-gomega
description: Use when writing, reviewing, or fixing LeafWiki Ginkgo/Gomega tests, especially semantic-hygiene failures, taxonomy labels, migrated Test-style specs, weak Gomega assertions, table specs, or specs that should read as system behaviour documentation.
---

# LeafWiki BDD Ginkgo/Gomega

LeafWiki specs are behaviour documentation. A reader should understand the system contract from the concatenated Ginkgo name without reverse-engineering helper code or raw fields.

## Target Shape

Use containers for subject and conditions, and `It` for the observable outcome:

```go
var _ = Describe("import plan creation", func() {
	When("the upload is not a zip", func() {
		It("returns a localized invalid-upload error", func() {
			// arrange / act / assert
		})
	})
})
```

The full name should read like: `import plan creation when the upload is not a zip returns a localized invalid-upload error`.

## Taxonomy Labels

Use exactly one primary taxonomy label for each runnable Go/Ginkgo spec when
the boundary is clear. Missing labels are rollout debt. Invalid, dynamic, or
multiple effective primary labels are checker failures.

Ginkgo labels inherit. Label the narrowest truthful `Describe`,
`DescribeTable`, `Entry`, or `It`; do not add a child label that conflicts with
an inherited parent label. Mixed packages need spec/table/container labels, not
a package-wide label.

| Label | Use for | Do not use for |
|---|---|---|
| `unit` | one package/component contract, in process, deterministic; temp files are fine when the file format or layout is under test | real HTTP routes, MCP client/server, SQLite adapters, subprocesses, daemons, Fosite/OAuth, git-backed sync, or multi-service `wiki.NewWiki` stacks |
| `integration` | multiple LeafWiki components wired together, or one component through a real in-process adapter boundary such as `httptest`, Gin middleware, SQLite stores, OAuth, MCP SDK/server, importer+wiki, workspace sync, daemon registries, or `wiki.NewWiki` | command/runtime build artifacts driven as a shipped product boundary |
| `e2e` | Go/Ginkgo specs that act on a LeafWiki binary/build artifact through CLI, process stdio/status, API, MCP transport, started runtime HTTP/MCP, or filesystem effects | in-process command helper tests, Playwright browser tests, or Docker/reverse-proxy tests outside Go/Ginkgo |

Bad taxonomy shape:

```go
var _ = Describe("page routes", Label("integration"), func() {
	It("normalizes route paths", Label("unit"), func() {
		// invalid: effective labels are integration and unit
	})
})
```

Good mixed-file shape:

```go
var _ = Describe("page routes", func() {
	Describe("route path parsing", Label("unit"), func() {
		It("normalizes repeated slashes", func() {})
	})

	Describe("HTTP route handling", Label("integration"), func() {
		It("serves the normalized page", func() {})
	})
})
```

## Bad -> Good

Bad migrated shape:

```go
var _ = It("TestNormalizeCodexToolEventSanitizesSessionAndMetadata", func() {
	t := GinkgoT()
	got := normalize(event)
	if got.SessionID == "" {
		t.Fatalf("session ID was not preserved")
	}
	Expect(got.Metadata["tool"]).To(Equal("shell"))
})
```

Good behaviour shape:

```go
var _ = Describe("agent hook event normalization", func() {
	When("Codex sends tool metadata with session identifiers", func() {
		It("preserves semantic session identity and redacts unsafe metadata", func() {
			got := normalize(event)

			Expect(got).To(HaveSessionID(wantSessionID))
			Expect(got).To(ExposeSafeToolMetadata(toolmetadata.Shell))
		})
	})
})
```

Bad table shape:

```go
DescribeTable("TestApplyConfig",
	func(input string, flag string, env string, want int, message string) {},
	Entry(nil, "bad", "--port", "PORT", 1, "invalid port"),
)
```

Good table shape:

```go
type configCase struct {
	source configSource
	want   configValidationIssue
}

DescribeTable("configuration validation",
	func(row configCase) {
		Expect(validate(row.source)).To(ReportConfigIssue(row.want))
	},
	Entry("rejects an invalid port from CLI flags", configCase{
		source: configSourceFromCLI("--port=bad"),
		want:   configValidationIssueInvalidPort,
	}),
)
```

## LeafWiki Rules

| Do | Avoid |
|---|---|
| `Describe(subject) / When(condition) / It(outcome)` | top-level `It` or migrated `Test...` names |
| One effective `Label("unit"|"integration"|"e2e")` at the narrowest truthful node | dynamic labels, unknown labels, or conflicting inherited labels |
| Semantic domain matchers: `HaveWorkspaceID`, `ReportConfigIssue` | repeated field assertions and raw map/index assertions |
| Typed IDs, semantic values, message IDs, validation codes | hardcoded prose, raw IDs, raw error strings |
| `Expect(err).To(Succeed())` or meaningful `MatchError`/domain matcher | generic `HaveOccurred()` for meaningful failures |
| Struct table rows with behaviour entry names | wide positional `Entry` rows or `Entry(nil, ...)` |
| `GinkgoHelper()` in assertion helpers | helper failures pointing at helper internals |
| Custom/composed matchers for reused oracles | reusable `expect*`/`assert*` functions with many field checks |
| `Eventually(...).WithContext(ctx)` and callback polling | boolean polling, blocking receives, goroutine assertions without recovery |

## Matcher Pressure

If a spec repeats three or more field assertions against the same domain object, create or reuse a matcher. The spec should say what behaviour is true, not how a DTO is laid out.

Prefer:

```go
Expect(response).To(DescribeInvalidUpload(messageid.ImportInvalidArchive))
```

Over:

```go
Expect(response.Code).To(Equal("invalid_upload"))
Expect(response.Message).To(Equal("invalid upload"))
Expect(response.Status).To(Equal(http.StatusBadRequest))
```

Use composed matchers first (`And`, `HaveField`, `WithTransform`, `SatisfyAll`). Write a custom matcher when the same behavioural oracle appears in multiple specs or failure messages need domain vocabulary.

## Remediation Order

When fixing semantic-hygiene findings, keep the checker strict and fix tests in this order:

1. Replace `GinkgoT()` and `testing.T` failures inside specs with Gomega/Ginkgo-native assertions.
2. Rename `Test...` Ginkgo nodes and table entries into behaviour descriptions.
3. Move top-level `It`/`Specify` into meaningful containers, or use a tiny budgeted waiver only for a real package invariant.
4. Add or correct taxonomy labels only after the spec boundary is understood.
5. Convert wide table rows to struct rows with named fields.
6. Extract semantic matchers where repeated field assertions obscure behaviour.

Do not add baselines, broad allowlists, `//nolint`, `ginkgo-linter:ignore-*`,
or checker relaxations to make the gate pass. Change the checker only to make a
newly observed bad pattern fail, and add the red fixture first.

## Fixer Contract

A fixer thread should treat semantic-hygiene output as product-quality
feedback, not as a linter to appease. The acceptable path is to improve the
spec narrative, assertions, helpers, labels, or fixtures until the gate reflects
real behaviour documentation.

Reject changes that:

- weaken or bypass semantic hygiene, ginkgolinter, or taxonomy policy
- add labels by directory guess instead of reading the spec boundary
- replace one weak assertion with another semantically equivalent weak form
- rename specs to hide `Test...` without producing behaviour names
- introduce broad helper abstractions whose only purpose is avoiding a rule

## Acceptance

Before claiming a LeafWiki Ginkgo/Gomega cleanup is done, run the unified
static source-policy gate plus the relevant runtime tests:

```sh
rtk bash scripts/golangci-lint.sh --output.text.colors=false --max-issues-per-linter=0 --max-same-issues=0
rtk go test ./...
rtk git diff --check
```

`ginkgolinter` runs through `.golangci.leafwiki.yml` together with LeafWiki's
semantic hygiene and i18n analyzers. Do not run or require a standalone
ginkgolinter gate unless the supervisor is explicitly debugging that linter.

For an in-progress package slice, the supervisor may use the same config against
the assigned package path, plus the package's focused `go test`, taxonomy report,
and `git diff --check`. The unified gate may be red only when the supervising
thread explicitly accepts the remaining rule counts as follow-up scope.
