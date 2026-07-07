package testhygiene

import (
	"go/token"
	"go/types"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("testhygiene Ginkgo entry contracts", ginkgo.Label("unit"), func() {
	ginkgo.It("classifies container body, decorator, and semantic table-entry data policies", func() {
		h := newRuleHarness("/repo/internal/wiki/node_table_test.go", "github.com/perber/wiki/internal/wiki", `package wiki

type Slug string
type assertion struct{}

var Focus any
var Pending any
var Serial any
var Ordered any

func Describe(args ...any) any { return nil }
func BeforeEach(args ...any) any { return nil }
func DescribeTable(args ...any) any { return nil }
func Entry(args ...any) any { return nil }
func Label(values ...string) any { return nil }
func FlakeAttempts(count int) any { return nil }
func SpecPriority(priority int) any { return nil }
func Expect(actual any) assertion { return assertion{} }
func (assertion) To(matcher any, extra ...any) {}
func BeTrue() any { return nil }

var _ = Describe("node creation", Focus, Pending, FlakeAttempts(2), Serial, Ordered, SpecPriority(1), func() {
	setupSlug := "from setup"
	var setupTitle = "Docs"
	BeforeEach(func() {
		setupSlug = "from setup"
		setupTitle = "Docs"
	})
		DescribeTable("node slug validation",
		func(slug Slug, code string, title string) {},
		Entry("uses raw semantic row", "docs", "invalid_slug", setupSlug, Label("unit")),
		Entry("uses too many positional values", "one", "two", "three", "four", "five"),
	)
	Expect(true).To(BeTrue())
	_ = setupTitle
})
`)
		describeCall := h.findCall("Describe")
		checkGinkgoDecoratorArgs(h.ctx, describeCall)
		checkGinkgoContainerBody(h.ctx, describeCall)
		for _, entry := range h.findCalls("Entry") {
			checkGinkgoEntryPolicy(h.ctx, entry)
		}

		Expect(diagnosticRuleIDs(h.diagnosticMessages())).To(ContainElements(
			"semh:ginkgo.focus",
			"semh:ginkgo.pending",
			"semh:ginkgo.flake-attempts",
			"semh:ginkgo.restricted-decorator",
			"semh:ginkgo.container-state-initialization",
			"semh:ginkgo.container-call",
			"semh:ginkgo.semantic-entry-data",
			"semh:ginkgo.entry-setup-value",
			"semh:ginkgo.wide-entry",
		))
	})

	ginkgo.It("counts table data after decorator values and still reports semantic raw data", func() {
		h := newRuleHarness("/repo/internal/wiki/node_table_test.go", "github.com/perber/wiki/internal/wiki", `package wiki

type Slug string

func DescribeTable(args ...any) any { return nil }
func Entry(args ...any) any { return nil }
func Offset(value int) any { return nil }

type codeLocation struct{}

var _ = DescribeTable("node slugs",
	func(slug Slug) {},
	Entry("uses typed decorator", codeLocation{}, Offset(1), "docs"),
)
`)
		entry := h.findCall("Entry")
		ginkgoTypesPackage := types.NewPackage("github.com/onsi/ginkgo/v2/types", "types")
		codeLocationType := types.NewNamed(
			types.NewTypeName(token.NoPos, ginkgoTypesPackage, "CodeLocation", nil),
			types.NewStruct(nil, nil),
			nil,
		)
		h.pass.TypesInfo.Types[entry.Args[1]] = types.TypeAndValue{Type: codeLocationType}

		Expect(ginkgoEntryDataArgCount(h.ctx, entry)).To(Equal(1))

		checkGinkgoEntryPolicy(h.ctx, entry)
		Expect(h.diagnosticMessages()).To(ConsistOf(
			"semh:ginkgo.semantic-entry-data: Ginkgo Entry data for slug uses raw string for Slug; use a semantic fixture value or row struct",
		))
	})

	ginkgo.It("requires contract context before treating raw code table parameters as semantic", func() {
		h := newRuleHarness("/repo/internal/wiki/node_table_test.go", "github.com/perber/wiki/internal/wiki", `package wiki

type PageID string

func DescribeTable(args ...any) any { return nil }
func Entry(args ...any) any { return nil }

var _ = DescribeTable("page rendering",
	func(code string) {},
	Entry("keeps ordinary code prose", "invalid"),
)

var _ = DescribeTable("validation issue contract",
	func(code string) {},
	Entry("uses raw validation code", "invalid"),
)

var _ = DescribeTable("page rendering",
	func(pageID PageID, code string) {},
	Entry("uses raw code with semantic neighbor", PageID("page-1"), "invalid"),
)
`)
		for _, entry := range h.findCalls("Entry") {
			checkGinkgoEntryPolicy(h.ctx, entry)
		}

		Expect(h.diagnosticMessages()).To(ConsistOf(
			"semh:ginkgo.semantic-entry-data: Ginkgo Entry data for code uses raw string for IssueCode; use a semantic fixture value or row struct",
			"semh:ginkgo.semantic-entry-data: Ginkgo Entry data for code uses raw string for ErrorCode; use a semantic fixture value or row struct",
		))
	})

	ginkgo.It("collects setup identifiers while ignoring nested function bodies and blanks", func() {
		h := newRuleHarness("/repo/internal/wiki/setup_names_test.go", "github.com/perber/wiki/internal/wiki", `package wiki

func pair() (string, string) { return "", "" }

func collectSetupNames() {
	kept := "one"
	var declared = "two"
	_, alsoKept := pair()
	func() {
		nested := "ignored"
		_ = nested
	}()
	_ = kept
	_ = declared
	_ = alsoKept
}
`)
		names := map[string]bool{}

		collectAssignedIdentifiers(h.findFunc("collectSetupNames").Body, names)

		Expect(names).To(Equal(map[string]bool{
			"alsoKept": true,
			"declared": true,
			"kept":     true,
		}))
	})
})
