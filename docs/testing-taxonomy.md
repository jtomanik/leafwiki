<!-- leafwiki
version: 1
page:
  id: testing-taxonomy-20260704
  title: Testing Taxonomy
  created_at: "2026-07-04T12:00:00Z"
  updated_at: "2026-07-04T12:00:00Z"
  creator_id: system
  last_author_id: system
tags:
  - testing
  - ginkgo
fields:
  type: policy
-->

# Testing Taxonomy

LeafWiki Go/Ginkgo specs use one primary taxonomy label to describe the
behavioral boundary under test:

- `unit`
- `integration`
- `e2e`

The labels are mutually exclusive. Each runnable Go/Ginkgo spec should
eventually have exactly one of them, either directly or inherited from the
narrowest truthful container.

During the rollout, missing taxonomy labels are allowed so the active
Ginkgo/Gomega cleanup can continue in parallel. Invalid labels, dynamic labels,
and specs that inherit more than one primary taxonomy label are policy errors
once checker support exists.

## Label Rules

Use Ginkgo's `Label` decorator in the style already used by the file:

```go
var _ = Describe("workspace ID validation", Label("unit"), func() {
    // specs
})
```

or, in files that import Ginkgo without dot imports:

```go
var _ = ginkgo.Describe("workspace ID validation", ginkgo.Label("unit"), func() {
    // specs
})
```

Ginkgo labels are inherited. A spec's effective label set is the union of the
labels on its subject node and every enclosing container. A child label does not
override a parent label.

That means this is invalid because the spec effectively has both primary labels:

```go
var _ = Describe("page routes", Label("integration"), func() {
    It("normalizes a route path", Label("unit"), func() {
        // invalid: inherited integration plus direct unit
    })
})
```

Use the narrowest container whose descendants all share the same taxonomy. For
mixed files, label separate `Describe`, `DescribeTable`, `Entry`, or `It` nodes
instead of labeling the whole package or file.

Primary taxonomy labels are separate from execution decorators such as
`Serial`, `Ordered`, or `FlakeAttempts`. They are also separate from future
routing labels such as `slow`, `network`, `oauth`, or `mcp`. Non-taxonomy labels
must be added to checker policy before use.

## Unit

`unit` specs exercise one component or package contract in process. They should
be deterministic and should not cross a real adapter, process, network, or
multi-service boundary.

Use `unit` when the behavior under test is local to a package or a narrow
component contract, such as:

- semantic value parsing and validation
- Markdown, frontmatter, tree, route-path, or metadata helpers
- pure service decisions where dependencies are fakes or in-memory seams
- matcher, codec, planner, normalizer, or formatting behavior
- file-format behavior where temp files are the thing being tested

Temp files are allowed when the file format, file layout, or file-backed
component is the subject under test. They do not automatically make a spec
integration-level.

Do not use `unit` for specs that exercise:

- real HTTP routes or Gin middleware chains
- MCP client/server or SDK/server boundaries
- SQLite adapter behavior
- subprocesses, daemons, or shipped command binaries
- Fosite/OAuth flows
- workspace sync through git-backed revision storage
- a multi-service `wiki.NewWiki` stack

## Integration

`integration` specs exercise multiple LeafWiki components wired together, or
one component through a real adapter boundary, while still running inside the Go
test process.

Use `integration` for specs that cover boundaries such as:

- `httptest` route handlers or Gin middleware
- SQLite-backed stores and repository adapters
- Fosite/OAuth behavior
- MCP SDK/server interaction inside the test process
- importer plus wiki plus tags/properties behavior
- workspace sync through git revision storage
- daemon descriptors, registries, and supervisor coordination objects
- `wiki.NewWiki` stacks that wire multiple LeafWiki services together

These specs may look product-facing because they use HTTP handlers, MCP server
objects, stores, or realistic service wiring. They are still integration specs
when the test reaches into Go packages and constructs the system in process.

## E2E

`e2e` specs exercise LeafWiki through a product binary or command build
artifact. The test acts on what LeafWiki ships or starts instead of reaching
inside packages to construct the system.

Use Go/Ginkgo `e2e` when the spec builds or starts a LeafWiki command/runtime
artifact and drives it through external product boundaries, such as:

- command arguments and environment variables
- stdin, stdout, stderr, and exit status
- HTTP exposed by a started LeafWiki runtime
- MCP transport exposed by a started LeafWiki runtime
- filesystem effects produced by the started binary

The important boundary is the build artifact. A Go test that calls a command's
internal `run` function is not e2e just because the package lives under `e2e/`.
Classify that as `unit` or `integration` based on the in-process boundaries it
actually exercises.

## Separate Test Surfaces

This taxonomy covers Go/Ginkgo specs only.

Playwright browser tests under `e2e/tests/*.spec.ts` are outside this Go/Ginkgo
label taxonomy. Docker and reverse-proxy stack tests are also outside this
taxonomy unless they are represented by Go/Ginkgo tests that act on a LeafWiki
product binary or command build artifact.

Those test surfaces may keep their own naming, tagging, and CI structure.

## Rollout Rules

The rollout order matters:

1. Document the taxonomy and the binary/build-artifact boundary.
2. Make invalid taxonomy labels fail in semantic hygiene.
3. Add a report-only inventory for missing labels.
4. Label stable packages in small reviewable slices.
5. Turn missing labels into a hard failure only after the report reaches zero.
6. Add label-filtered CI only after completeness is enforced.

Until that final adoption point, missing labels are migration debt, not a
failing condition. Label-filtered runs such as `--label-filter=unit` are not a
complete signal while unlabeled specs still exist, because Ginkgo will silently
skip them.

## Classification Checklist

When labeling a spec, ask these questions in order:

1. Does the test act on a LeafWiki binary or command build artifact?
   - If yes, use `e2e`.
2. Does the test cross a real adapter boundary or wire multiple LeafWiki
   components in process?
   - If yes, use `integration`.
3. Is the test limited to one component or package contract in process?
   - If yes, use `unit`.
4. Does a parent container already provide a primary taxonomy label?
   - If yes, do not add a conflicting child label.
5. Is the file or package mixed?
   - If yes, label the narrowest truthful containers or specs.

When the answer is unclear, leave the spec unlabeled during the rollout and add
it to the labeling ledger for review instead of guessing.
