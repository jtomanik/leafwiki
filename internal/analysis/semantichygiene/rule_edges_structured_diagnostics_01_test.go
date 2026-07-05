package semantichygiene

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go/ast"
	"go/token"
	"go/types"
	"strings"
)

var _ = ginkgo.Describe("semantichygiene structured diagnostics", func() {
	ginkgo.It("delays rule diagnostics until finalization and prefixes the stable rule ID", ginkgo.Label("unit"), func() {
		h := newRuleHarness("/repo/internal/wiki/page_test.go", "example.com/p", `package p
func TestPage() {}
`)

		h.ctx.report(ruleDirectCast, h.file.Name, directCastDiagnostic("WorkspaceID"))
		Expect(h.diagnostics).To(BeEmpty())

		h.ctx.finalizeDiagnostics()

		Expect(h.diagnosticMessages()).To(ConsistOf(
			"semh:semantic.direct-cast: direct cast to semantic type WorkspaceID outside parser or boundary; use a parser or typed input",
		))
	})

	ginkgo.It("emits every registered rule identifier as a structured diagnostic prefix", ginkgo.Label("unit"), func() {
		type formattedDiagnosticState uint8

		const (
			formattedDiagnosticMalformed formattedDiagnosticState = iota
			formattedDiagnosticMatched
		)

		type formattedDiagnosticObservation struct {
			State   formattedDiagnosticState
			Rule    ruleID
			Message string
		}

		observeFormattedDiagnostic := func(message string) formattedDiagnosticObservation {
			rulePrefix, body, ok := strings.Cut(message, ": ")
			if !ok {
				return formattedDiagnosticObservation{State: formattedDiagnosticMalformed}
			}
			rawRule, ok := strings.CutPrefix(rulePrefix, "semh:")
			if !ok {
				return formattedDiagnosticObservation{State: formattedDiagnosticMalformed}
			}
			return formattedDiagnosticObservation{
				State:   formattedDiagnosticMatched,
				Rule:    ruleID(rawRule),
				Message: body,
			}
		}

		const diagnosticPayload = "diagnostic.payload"

		for id := range allRuleMetadata() {
			message := formatDiagnosticMessage(semanticDiagnostic{
				rule:    id,
				message: diagnosticPayload,
			})

			Expect(observeFormattedDiagnostic(message)).To(Equal(formattedDiagnosticObservation{
				State:   formattedDiagnosticMatched,
				Rule:    id,
				Message: diagnosticPayload,
			}))
		}
	})

	ginkgo.It("suppresses one matching call-scoped waivable diagnostic", ginkgo.Label("unit"), func() {
		h := newRuleHarness("/repo/internal/wiki/page_test.go", "example.com/p", `package p

func It(text string, body func()) bool { return true }

// semh:allow ginkgo.top-level-it -- package-level invariant reads clearer here
var _ = It("documents package invariant", func() {})
`)

		h.ctx.report(ruleGinkgoTopLevelIt, h.findCall("It"), "top-level It reads like a migrated unit test; place it under a behavior container")
		h.ctx.finalizeDiagnostics()

		Expect(h.diagnostics).To(BeEmpty())
	})

	ginkgo.It("suppresses a matcher diagnostic when the waiver is before the outer assertion call", ginkgo.Label("unit"), func() {
		h := newRuleHarness("/repo/internal/wiki/page_test.go", "example.com/p", `package p

func Expect(actual any) assertion { return assertion{} }
type assertion struct{}
func (assertion) To(matcher any) {}
func Equal(expected any) any { return nil }

func TestPage() {
	// semh:allow gomega.equal-zero -- zero literal reads clearer in this compatibility assertion
	Expect(0).To(
		Equal(0),
	)
}
`)
		equal := h.findCall("Equal")

		h.ctx.report(ruleGomegaEqualZero, equal, gomegaEqualZeroDiagnostic())
		h.ctx.finalizeDiagnostics()

		Expect(h.diagnostics).To(BeEmpty())
	})

	ginkgo.It("does not let a call-scoped waiver before a spec suppress diagnostics inside the spec body", ginkgo.Label("unit"), func() {
		h := newRuleHarness("/repo/internal/wiki/page_test.go", "example.com/p", `package p

func It(text string, body func()) bool { return true }
func Expect(actual any) assertion { return assertion{} }
type assertion struct{}
func (assertion) To(matcher any) {}
func Equal(expected any) any { return nil }

// semh:allow gomega.equal-zero -- the spec node must not waive assertions in its body
var _ = It("documents behavior", func() {
	Expect(0).To(Equal(0))
})
`)

		h.ctx.report(ruleGomegaEqualZero, h.findCall("Equal"), gomegaEqualZeroDiagnostic())
		h.ctx.finalizeDiagnostics()

		Expect(h.diagnosticMessages()).To(ConsistOf(
			"semh:waiver.stale: semh waiver for gomega.equal-zero did not match any diagnostic",
			"semh:gomega.equal-zero: use BeZero matcher instead of Equal(0) for zero-value assertions",
		))
	})

	ginkgo.It("suppresses one matching declaration-scoped waivable diagnostic", ginkgo.Label("unit"), func() {
		h := newRuleHarness("/repo/internal/wiki/page_test.go", "example.com/p", `package p

// semh:allow gomega.helper-should-be-matcher -- compact helper reads clearer than a matcher here
func assertResponse() {}
`)

		h.ctx.report(ruleGomegaHelperShouldBeMatcher, h.findFunc("assertResponse").Name, "prefer a custom Gomega matcher for reusable assertion helper assertResponse")
		h.ctx.finalizeDiagnostics()

		Expect(h.diagnostics).To(BeEmpty())
	})

	ginkgo.It("matches next-node waivers only against the immediately following node", ginkgo.Label("unit"), func() {
		h := newRuleHarness("/repo/internal/wiki/page_test.go", "example.com/p", `package p

// semh:allow ginkgo.top-level-it -- exercises next-node scope
func TestPage() {}
func TestLater() {}
`)
		waivers, diagnostics := h.ctx.collectWaivers()
		Expect(diagnostics).To(BeEmpty())
		Expect(waivers).To(HaveLen(1))

		immediate := h.findFunc("TestPage")
		later := h.findFunc("TestLater")
		Expect(h.ctx.waiverMatchesDiagnostic(waivers[0], semanticDiagnostic{
			rule: ruleGinkgoTopLevelIt,
			pos:  immediate.Pos(),
			node: immediate,
		}, waiverScopeNextNode)).To(BeTrue())
		Expect(h.ctx.waiverMatchesDiagnostic(waivers[0], semanticDiagnostic{
			rule: ruleGinkgoTopLevelIt,
			pos:  later.Pos(),
			node: later,
		}, waiverScopeNextNode)).To(BeFalse())
	})

	ginkgo.It("reports a valid waiver that matches no diagnostic as stale", ginkgo.Label("unit"), func() {
		h := newRuleHarness("/repo/internal/wiki/page_test.go", "example.com/p", `package p

// semh:allow ginkgo.top-level-it -- package-level invariant reads clearer here
func TestPage() {}
`)

		h.ctx.finalizeDiagnostics()

		Expect(h.diagnosticMessages()).To(ConsistOf(
			"semh:waiver.stale: semh waiver for ginkgo.top-level-it did not match any diagnostic",
		))
	})

	ginkgo.It("reports adjacent same-rule waivers for one diagnostic as duplicate", ginkgo.Label("unit"), func() {
		h := newRuleHarness("/repo/internal/wiki/page_test.go", "example.com/p", `package p

func It(text string, body func()) bool { return true }

// semh:allow ginkgo.top-level-it -- first explanation
// semh:allow ginkgo.top-level-it -- duplicate explanation
var _ = It("documents package invariant", func() {})
`)

		h.ctx.report(ruleGinkgoTopLevelIt, h.findCall("It"), "top-level It reads like a migrated unit test; place it under a behavior container")
		h.ctx.finalizeDiagnostics()

		Expect(h.diagnosticMessages()).To(ConsistOf(
			"semh:waiver.duplicate: duplicate semh waiver for ginkgo.top-level-it; one waiver can suppress one diagnostic",
		))
	})

	ginkgo.It("reports non-waivable rule waivers and leaves the hard diagnostic active", ginkgo.Label("unit"), func() {
		h := newRuleHarness("/repo/internal/wiki/page_test.go", "example.com/p", `package p

// semh:allow semantic.direct-cast -- this must stay hard
func TestPage() {}
`)

		h.ctx.report(ruleDirectCast, h.file.Name, directCastDiagnostic("WorkspaceID"))
		h.ctx.finalizeDiagnostics()

		Expect(h.diagnosticMessages()).To(ConsistOf(
			"semh:waiver.non-waivable-rule: semh waiver for semantic.direct-cast cannot suppress hard diagnostics",
			"semh:semantic.direct-cast: direct cast to semantic type WorkspaceID outside parser or boundary; use a parser or typed input",
		))
	})

	ginkgo.It("reports active waivers that exceed a per-rule budget", ginkgo.Label("unit"), func() {
		h := newRuleHarness("/repo/internal/wiki/page_test.go", "example.com/p", `package p

func It(text string, body func()) bool { return true }

// semh:allow ginkgo.top-level-it -- first package-level invariant
var _ = It("documents first package invariant", func() {})
// semh:allow ginkgo.top-level-it -- second package-level invariant
var _ = It("documents second package invariant", func() {})
// semh:allow ginkgo.top-level-it -- third package-level invariant
var _ = It("documents third package invariant", func() {})
// semh:allow ginkgo.top-level-it -- fourth package-level invariant
var _ = It("documents fourth package invariant", func() {})
`)
		for _, call := range h.findCalls("It") {
			h.ctx.report(ruleGinkgoTopLevelIt, call, "top-level It reads like a migrated unit test; place it under a behavior container")
		}

		h.ctx.finalizeDiagnostics()

		Expect(h.diagnosticMessages()).To(ConsistOf(
			"semh:waiver.budget-exceeded: waiver budget exceeded for ginkgo.top-level-it: used 4, budget 3",
		))
	})

	ginkgo.It("reports active waivers that exceed the total budget", ginkgo.Label("unit"), func() {
		h := newRuleHarness("/repo/internal/wiki/page_test.go", "example.com/p", `package p

func Node(text string, body func()) bool { return true }

// semh:allow ginkgo.top-level-it -- first package-level invariant
var _ = Node("node 1", func() {})
// semh:allow ginkgo.top-level-it -- second package-level invariant
var _ = Node("node 2", func() {})
// semh:allow ginkgo.top-level-it -- third package-level invariant
var _ = Node("node 3", func() {})
// semh:allow ginkgo.wide-entry -- first wide entry exception
var _ = Node("node 4", func() {})
// semh:allow ginkgo.wide-entry -- second wide entry exception
var _ = Node("node 5", func() {})
// semh:allow ginkgo.wide-entry -- third wide entry exception
var _ = Node("node 6", func() {})
// semh:allow gomega.equal-empty -- first empty matcher exception
var _ = Node("node 7", func() {})
// semh:allow gomega.equal-empty -- second empty matcher exception
var _ = Node("node 8", func() {})
// semh:allow gomega.equal-empty -- third empty matcher exception
var _ = Node("node 9", func() {})
// semh:allow gomega.equal-zero -- first zero matcher exception
var _ = Node("node 10", func() {})
// semh:allow gomega.equal-zero -- second zero matcher exception
var _ = Node("node 11", func() {})
`)
		rules := []ruleID{
			ruleGinkgoTopLevelIt,
			ruleGinkgoTopLevelIt,
			ruleGinkgoTopLevelIt,
			ruleGinkgoWideEntry,
			ruleGinkgoWideEntry,
			ruleGinkgoWideEntry,
			ruleGomegaEqualEmpty,
			ruleGomegaEqualEmpty,
			ruleGomegaEqualEmpty,
			ruleGomegaEqualZero,
			ruleGomegaEqualZero,
		}
		for i, call := range h.findCalls("Node") {
			h.ctx.report(rules[i], call, "waivable readability diagnostic")
		}

		h.ctx.finalizeDiagnostics()

		Expect(h.diagnosticMessages()).To(ConsistOf(
			"semh:waiver.budget-exceeded: total waiver budget exceeded: used 11, budget 10",
		))
	})

	ginkgo.It("reports package-qualified top-level It calls without a file-wide container prerequisite", ginkgo.Label("unit"), func() {
		h := newRuleHarness("/repo/internal/wiki/page_test.go", "github.com/perber/wiki/internal/analysis/semantichygiene/testdata/repotests", `package p

type bddDSL struct{}
var ginkgo bddDSL
func (bddDSL) It(text string, body func()) bool { return true }

var _ = ginkgo.It("documents package invariant", func() {})
`)
		call := h.findCall("It")
		h.ctx.pass.TypesInfo.Uses[call.Fun.(*ast.SelectorExpr).Sel] = types.NewFunc(
			token.NoPos,
			types.NewPackage("github.com/onsi/ginkgo/v2", "ginkgo"),
			"It",
			types.NewSignatureType(nil, nil, nil, nil, nil, false),
		)

		checkGinkgoSpecQualityCall(h.ctx, call)

		Expect(h.diagnosticMessages()).To(ConsistOf(
			"semh:ginkgo.top-level-it: top-level It reads like a migrated unit test; place it under a behavior container or waive with a specific reason",
		))
	})

	ginkgo.It("reports dot-imported top-level It calls", ginkgo.Label("unit"), func() {
		h := newRuleHarness("/repo/internal/wiki/page_test.go", "github.com/perber/wiki/internal/analysis/semantichygiene/testdata/repotests", `package p

func It(text string, body func()) bool { return true }

var _ = It("documents package invariant", func() {})
`)
		call := h.findCall("It")
		h.ctx.pass.TypesInfo.Uses[call.Fun.(*ast.Ident)] = types.NewFunc(
			token.NoPos,
			types.NewPackage("github.com/onsi/ginkgo/v2", "ginkgo"),
			"It",
			types.NewSignatureType(nil, nil, nil, nil, nil, false),
		)

		checkGinkgoSpecQualityCall(h.ctx, call)

		Expect(h.diagnosticMessages()).To(ConsistOf(
			"semh:ginkgo.top-level-it: top-level It reads like a migrated unit test; place it under a behavior container or waive with a specific reason",
		))
	})

	ginkgo.It("reports migrated GinkgoT wrappers", ginkgo.Label("unit"), func() {
		h := newRuleHarness("/repo/internal/wiki/page_test.go", "github.com/perber/wiki/internal/analysis/semantichygiene/testdata/repotests", `package p

type bddDSL struct{}
type fakeT struct{}
var ginkgo bddDSL
func (bddDSL) It(text string, body func()) bool { return true }
func (bddDSL) GinkgoT() fakeT { return fakeT{} }
func (fakeT) Helper() {}

var _ = ginkgo.It("TestExistingMigratedSpec", func() {
	t := ginkgo.GinkgoT()
	t.Helper()
})
`)
		call := h.findCall("It")
		h.ctx.pass.TypesInfo.Uses[call.Fun.(*ast.SelectorExpr).Sel] = types.NewFunc(
			token.NoPos,
			types.NewPackage("github.com/onsi/ginkgo/v2", "ginkgo"),
			"It",
			types.NewSignatureType(nil, nil, nil, nil, nil, false),
		)

		checkGinkgoSpecQualityCall(h.ctx, call)

		Expect(h.diagnosticMessages()).To(ConsistOf(
			"semh:ginkgo.top-level-it: top-level It reads like a migrated unit test; place it under a behavior container or waive with a specific reason",
			`semh:ginkgo.test-name: Ginkgo node name "TestExistingMigratedSpec" preserves a migrated testing.T name; describe observable behavior instead`,
			"semh:ginkgo.testing-t-in-spec: avoid testing.T-like.Helper adapter inside Ginkgo specs; use Gomega expectations and Ginkgo helpers",
		))
	})

	ginkgo.It("reports top-level It in ordinary repo packages", ginkgo.Label("unit"), func() {
		h := newRuleHarness("/repo/internal/branding/branding_test.go", "github.com/perber/wiki/internal/branding", `package branding

type bddDSL struct{}
var ginkgo bddDSL
func (bddDSL) It(text string, body func()) bool { return true }

var _ = ginkgo.It("documents existing migrated behavior", func() {})
`)
		call := h.findCall("It")
		h.ctx.pass.TypesInfo.Uses[call.Fun.(*ast.SelectorExpr).Sel] = types.NewFunc(
			token.NoPos,
			types.NewPackage("github.com/onsi/ginkgo/v2", "ginkgo"),
			"It",
			types.NewSignatureType(nil, nil, nil, nil, nil, false),
		)

		checkGinkgoSpecQualityCall(h.ctx, call)

		Expect(h.diagnosticMessages()).To(ConsistOf(
			"semh:ginkgo.top-level-it: top-level It reads like a migrated unit test; place it under a behavior container or waive with a specific reason",
		))
	})
})
