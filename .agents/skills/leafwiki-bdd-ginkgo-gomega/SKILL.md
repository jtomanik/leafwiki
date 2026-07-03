---
name: leafwiki-bdd-ginkgo-gomega
description: Use when writing, reviewing, or fixing LeafWiki Ginkgo/Gomega tests, especially semantic-hygiene failures, migrated Test-style specs, weak Gomega assertions, table specs, or specs that should read as system behaviour documentation.
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
4. Convert wide table rows to struct rows with named fields.
5. Extract semantic matchers where repeated field assertions obscure behaviour.

Do not add baselines, broad allowlists, `ginkgo-linter:ignore-*`, or checker relaxations to make the gate pass.

## Acceptance

Before claiming a LeafWiki Ginkgo/Gomega cleanup is done, run:

```sh
rtk bash scripts/check-semantic-hygiene.sh
rtk go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2 run --config=.golangci.ginkgolinter.yml --timeout=5m --output.text.colors=false --max-issues-per-linter=0 --max-same-issues=0
rtk go test ./...
rtk git diff --check
```

The semantic gate may be red only when the supervising thread explicitly accepts the remaining rule counts as follow-up scope.
