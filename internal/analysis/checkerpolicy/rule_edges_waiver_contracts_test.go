package checkerpolicy_test

import (
	"go/ast"
	"go/parser"
	"go/token"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/perber/wiki/internal/analysis/checkerpolicy"
	"golang.org/x/tools/go/analysis"
)

type waiverScopeObservation struct {
	First policyBoolObservation
	Later policyBoolObservation
	Nil   policyBoolObservation
}

var _ = ginkgo.Describe("checker policy waiver contracts", ginkgo.Label("unit"), func() {
	ginkgo.It("reports diagnostics directly when waiver processing is disabled", func() {
		ctx, file, diagnostics := newReportingGinkgoPolicyHarnessWithProcess(`package p

// semh:allow ginkgo.top-level-it -- ignored while waiver processing is disabled
var _ = It("documents invariant", func() {})
`, testRuleSet(), false)

		ctx.Report(ruleGinkgoTopLevelIt, findPolicyCall(file, "It"), "diagnostic.payload")
		ctx.FinalizeDiagnostics()

		Expect(observedDiagnosticRules(*diagnostics)).To(Equal([]checkerpolicy.RuleID{ruleGinkgoTopLevelIt}))
	})

	ginkgo.It("reports diagnostics whose rules are outside waiver metadata", func() {
		rules := checkerpolicy.NewRuleSet(map[checkerpolicy.RuleID]checkerpolicy.RuleMetadata{}, nil, 0)
		ctx, file, diagnostics := newReportingGinkgoPolicyHarness(`package p

var _ = Equal(0)
`, rules)

		ctx.Report(ruleGomegaEqualZero, findPolicyCall(file, "Equal"), "diagnostic.payload")
		ctx.FinalizeDiagnostics()

		Expect(observedDiagnosticRules(*diagnostics)).To(Equal([]checkerpolicy.RuleID{ruleGomegaEqualZero}))
	})

	ginkgo.It("reports raw ignore comments and embedded semh waiver text as policy diagnostics", func() {
		ctx, _, _ := newGinkgoPolicyHarness(`package p

// ginkgo-linter:ignore-gomega
// package note with semh:allow gomega.equal-zero -- embedded fixture
func ordinary() {}
`)
		waivers, diagnostics := ctx.CollectWaivers()

		Expect(waivers).To(BeEmpty())
		Expect(diagnosticRules(diagnostics)).To(Equal([]checkerpolicy.RuleID{
			checkerpolicy.RuleGinkgoLinterRawIgnoreRule,
			checkerpolicy.RuleWaiverMalformed,
		}))
	})

	ginkgo.It("leaves semh text in block comments outside waiver processing", func() {
		ctx, _, _ := newGinkgoPolicyHarness(`package p

/* semh:allow gomega.equal-zero -- block comment fixture */
func ordinary() {}
`)
		waivers, diagnostics := ctx.CollectWaivers()

		Expect(waivers).To(BeEmpty())
		Expect(diagnostics).To(BeEmpty())
	})

	ginkgo.It("reports malformed waiver directives before rule lookup", func() {
		ctx, _, _ := newGinkgoPolicyHarness(`package p

// semh:disable gomega.equal-zero
// semh:allow -- missing rule fixture
// semh:allow --
func ordinary() {}
`)
		waivers, diagnostics := ctx.CollectWaivers()

		Expect(waivers).To(BeEmpty())
		Expect(diagnosticRules(diagnostics)).To(Equal([]checkerpolicy.RuleID{
			checkerpolicy.RuleWaiverMalformed,
			checkerpolicy.RuleWaiverMalformed,
			checkerpolicy.RuleWaiverMalformed,
		}))
	})

	ginkgo.It("matches next-node waivers only to the immediately following node", func() {
		ctx, _, file := newGinkgoPolicyHarness(`package p

// semh:allow gomega.equal-zero -- next node fixture
var _ = Equal(0)
var _ = Equal(1)
`)
		calls := findCalls(file, "Equal")
		Expect(calls).To(HaveLen(2))
		waiver := checkerpolicy.ParsedWaiver{
			Rule: ruleGomegaEqualZero,
			Pos:  file.Comments[0].List[0].Slash,
		}

		Expect(observeNextNodeWaiver(ctx, waiver, calls)).To(Equal(waiverScopeObservation{
			First: policyBoolAccepted,
			Later: policyBoolRejected,
			Nil:   policyBoolRejected,
		}))
	})

	ginkgo.It("keeps waiver matching local to the file that declares it", func() {
		ctx, files := newGinkgoPolicyHarnessWithFiles(map[string]string{
			"/repo/a_test.go": `package p

// semh:allow gomega.equal-zero -- file-local fixture
func first() {}
`,
			"/repo/b_test.go": `package p

var _ = Equal(0)
`,
		})
		waiver := checkerpolicy.ParsedWaiver{
			Rule: ruleGomegaEqualZero,
			Pos:  files["/repo/a_test.go"].Comments[0].List[0].Slash,
		}
		call := findPolicyCall(files["/repo/b_test.go"], "Equal")

		Expect(policyBoolState(ctx.WaiverMatchesDiagnostic(
			waiver,
			checkerpolicy.Diagnostic{Rule: ruleGomegaEqualZero, Pos: call.Pos(), Node: call},
			checkerpolicy.WaiverScopeCall,
		))).To(Equal(policyBoolRejected))
	})

	ginkgo.It("rejects call-scoped waivers for declaration diagnostics", func() {
		ctx, _, file := newGinkgoPolicyHarness(`package p

// semh:allow gomega.equal-zero -- declaration diagnostic fixture
func check() {}
`)
		waiver := checkerpolicy.ParsedWaiver{
			Rule: ruleGomegaEqualZero,
			Pos:  file.Comments[0].List[0].Slash,
		}
		decl := findFuncDecl(file, "check")

		Expect(policyBoolState(ctx.WaiverMatchesDiagnostic(
			waiver,
			checkerpolicy.Diagnostic{Rule: ruleGomegaEqualZero, Pos: decl.Pos(), Node: decl},
			checkerpolicy.WaiverScopeCall,
		))).To(Equal(policyBoolRejected))
	})

	ginkgo.It("matches declaration-scoped waivers to diagnostics in the next declaration", func() {
		ctx, _, file := newGinkgoPolicyHarness(`package p

// semh:allow ginkgo.top-level-it -- declaration-scoped package invariant
func check() {
	Equal(0)
}
`)
		call := findPolicyCall(file, "Equal")
		waiver := checkerpolicy.ParsedWaiver{
			Rule: ruleGinkgoTopLevelIt,
			Pos:  file.Comments[0].List[0].Slash,
		}

		Expect(policyBoolState(ctx.WaiverMatchesDiagnostic(
			waiver,
			checkerpolicy.Diagnostic{Rule: ruleGinkgoTopLevelIt, Pos: call.Pos(), Node: call},
			checkerpolicy.WaiverScopeDeclaration,
		))).To(Equal(policyBoolAccepted))
	})

	ginkgo.It("rejects declaration-scoped waivers when no declaration encloses the diagnostic", func() {
		ctx, _, file := newGinkgoPolicyHarness(`package p

// semh:allow ginkgo.top-level-it -- declaration fixture
func check() {}
`)
		waiver := checkerpolicy.ParsedWaiver{
			Rule: ruleGinkgoTopLevelIt,
			Pos:  file.Comments[0].List[0].Slash,
		}

		Expect(policyBoolState(ctx.WaiverMatchesDiagnostic(
			waiver,
			checkerpolicy.Diagnostic{Rule: ruleGinkgoTopLevelIt, Pos: file.Pos(), Node: file},
			checkerpolicy.WaiverScopeDeclaration,
		))).To(Equal(policyBoolRejected))
	})

	ginkgo.It("rejects unsupported waiver scopes", func() {
		ctx, _, file := newGinkgoPolicyHarness(`package p

// semh:allow ginkgo.top-level-it -- unsupported scope fixture
func check() {}
`)
		waiver := checkerpolicy.ParsedWaiver{
			Rule: ruleGinkgoTopLevelIt,
			Pos:  file.Comments[0].List[0].Slash,
		}
		call := findFuncDecl(file, "check")

		Expect(policyBoolState(ctx.WaiverMatchesDiagnostic(
			waiver,
			checkerpolicy.Diagnostic{Rule: ruleGinkgoTopLevelIt, Pos: call.Pos(), Node: call},
			checkerpolicy.WaiverScopeNone,
		))).To(Equal(policyBoolRejected))
	})

	ginkgo.It("allows one waiver to suppress only one matching diagnostic", func() {
		ctx, file, diagnostics := newReportingGinkgoPolicyHarness(`package p

// semh:allow gomega.equal-zero -- single-use fixture
var _ = Equal(0)
`, testRuleSet())
		call := findPolicyCall(file, "Equal")

		ctx.Report(ruleGomegaEqualZero, call, "diagnostic.payload")
		ctx.Report(ruleGomegaEqualZero, call, "diagnostic.payload")
		ctx.FinalizeDiagnostics()

		Expect(observedDiagnosticRules(*diagnostics)).To(Equal([]checkerpolicy.RuleID{ruleGomegaEqualZero}))
	})

	ginkgo.It("reports unrelated diagnostics while unmatched waivers become stale", func() {
		ctx, file, diagnostics := newReportingGinkgoPolicyHarness(`package p

// semh:allow gomega.equal-zero -- unrelated fixture
var _ = It("documents invariant", func() {})
`, testRuleSet())

		ctx.Report(ruleGinkgoTopLevelIt, findPolicyCall(file, "It"), "diagnostic.payload")
		ctx.FinalizeDiagnostics()

		Expect(observedDiagnosticRules(*diagnostics)).To(Equal([]checkerpolicy.RuleID{
			ruleGinkgoTopLevelIt,
			checkerpolicy.RuleWaiverStale,
		}))
	})

	ginkgo.It("reports per-rule waiver budget pressure after matching diagnostics", func() {
		rules := checkerpolicy.NewRuleSet(map[checkerpolicy.RuleID]checkerpolicy.RuleMetadata{
			ruleSemanticDirectCast: checkerpolicy.HardRule(ruleSemanticDirectCast),
			ruleGinkgoTopLevelIt:   checkerpolicy.WaivableRule(ruleGinkgoTopLevelIt, checkerpolicy.WaiverScopeCall),
			ruleGomegaEqualZero:    checkerpolicy.WaivableRule(ruleGomegaEqualZero, checkerpolicy.WaiverScopeCall),
		}, map[checkerpolicy.RuleID]int{ruleGomegaEqualZero: 1}, 0)
		ctx, file, diagnostics := newReportingGinkgoPolicyHarness(`package p

// semh:allow gomega.equal-zero -- first equal-zero fixture
var _ = Equal(0)
// semh:allow gomega.equal-zero -- second equal-zero fixture
var _ = Equal(0)
`, rules)

		for _, call := range findCalls(file, "Equal") {
			ctx.Report(ruleGomegaEqualZero, call, "diagnostic.payload")
		}
		ctx.FinalizeDiagnostics()

		Expect(observedDiagnosticRules(*diagnostics)).To(Equal([]checkerpolicy.RuleID{
			checkerpolicy.RuleWaiverBudgetExceeded,
		}))
	})
})

func observedDiagnosticRules(diagnostics []formattedDiagnosticObservation) []checkerpolicy.RuleID {
	ginkgo.GinkgoHelper()

	rules := make([]checkerpolicy.RuleID, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		Expect(diagnostic.State).To(Equal(formattedDiagnosticObserved))
		rules = append(rules, diagnostic.Rule)
	}
	return rules
}

func diagnosticRules(diagnostics []checkerpolicy.Diagnostic) []checkerpolicy.RuleID {
	rules := make([]checkerpolicy.RuleID, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		rules = append(rules, diagnostic.Rule)
	}
	return rules
}

func observeNextNodeWaiver(
	ctx *checkerpolicy.Context,
	waiver checkerpolicy.ParsedWaiver,
	calls []*ast.CallExpr,
) waiverScopeObservation {
	ginkgo.GinkgoHelper()

	return waiverScopeObservation{
		First: policyBoolState(ctx.WaiverMatchesDiagnostic(
			waiver,
			checkerpolicy.Diagnostic{Rule: ruleGomegaEqualZero, Pos: calls[0].Pos(), Node: calls[0]},
			checkerpolicy.WaiverScopeNextNode,
		)),
		Later: policyBoolState(ctx.WaiverMatchesDiagnostic(
			waiver,
			checkerpolicy.Diagnostic{Rule: ruleGomegaEqualZero, Pos: calls[1].Pos(), Node: calls[1]},
			checkerpolicy.WaiverScopeNextNode,
		)),
		Nil: policyBoolState(ctx.WaiverMatchesDiagnostic(
			waiver,
			checkerpolicy.Diagnostic{Rule: ruleGomegaEqualZero, Pos: calls[0].Pos()},
			checkerpolicy.WaiverScopeNextNode,
		)),
	}
}

func newGinkgoPolicyHarnessWithFiles(sources map[string]string) (*checkerpolicy.Context, map[string]*ast.File) {
	ginkgo.GinkgoHelper()

	fset := token.NewFileSet()
	files := make([]*ast.File, 0, len(sources))
	byPath := make(map[string]*ast.File, len(sources))
	for path, src := range sources {
		file, err := parser.ParseFile(fset, path, src, parser.ParseComments)
		Expect(err).To(Succeed())
		files = append(files, file)
		byPath[path] = file
	}

	pass := &analysis.Pass{
		Fset:  fset,
		Files: files,
		Report: func(analysis.Diagnostic) {
		},
	}
	return checkerpolicy.NewContext(pass, testRuleSet(), true), byPath
}

func newReportingGinkgoPolicyHarness(
	src string,
	rules checkerpolicy.RuleSet,
) (*checkerpolicy.Context, *ast.File, *[]formattedDiagnosticObservation) {
	ginkgo.GinkgoHelper()

	return newReportingGinkgoPolicyHarnessWithProcess(src, rules, true)
}

func newReportingGinkgoPolicyHarnessWithProcess(
	src string,
	rules checkerpolicy.RuleSet,
	processWaivers bool,
) (*checkerpolicy.Context, *ast.File, *[]formattedDiagnosticObservation) {
	ginkgo.GinkgoHelper()

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "/repo/policy_test.go", src, parser.ParseComments)
	Expect(err).To(Succeed())

	var diagnostics []formattedDiagnosticObservation
	pass := &analysis.Pass{
		Fset:  fset,
		Files: []*ast.File{file},
		Report: func(diagnostic analysis.Diagnostic) {
			diagnostics = append(diagnostics, observeFormattedDiagnostic(diagnostic.Message))
		},
	}
	return checkerpolicy.NewContext(pass, rules, processWaivers), file, &diagnostics
}

func findFuncDecl(file *ast.File, name string) *ast.FuncDecl {
	ginkgo.GinkgoHelper()

	var found *ast.FuncDecl
	ast.Inspect(file, func(node ast.Node) bool {
		fn, ok := node.(*ast.FuncDecl)
		if !ok || fn.Name.Name != name {
			return true
		}
		found = fn
		return false
	})
	Expect(found).NotTo(BeNil(), "function %q should exist", name)
	return found
}
