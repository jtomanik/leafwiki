package checkerpolicy_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/perber/wiki/internal/analysis/checkerpolicy"
	"golang.org/x/tools/go/analysis"
)

type policyBoolObservation uint8

const (
	policyBoolRejected policyBoolObservation = iota
	policyBoolAccepted
)

type formattedDiagnosticState uint8

const (
	formattedDiagnosticMalformed formattedDiagnosticState = iota
	formattedDiagnosticObserved
)

type formattedDiagnosticObservation struct {
	State   formattedDiagnosticState
	Rule    checkerpolicy.RuleID
	Payload string
}

var _ = ginkgo.Describe("checker policy context contracts", ginkgo.Label("unit"), func() {
	ginkgo.It("formats unknown diagnostics with a stable semh fallback prefix", func() {
		message := testRuleSet().FormatDiagnosticMessage(checkerpolicy.Diagnostic{
			Rule:    checkerpolicy.RuleID("policy.unknown"),
			Message: "diagnostic.payload",
		})

		Expect(observeFormattedDiagnostic(message)).To(Equal(formattedDiagnosticObservation{
			State:   formattedDiagnosticObserved,
			Rule:    checkerpolicy.RuleID("policy.unknown"),
			Payload: "diagnostic.payload",
		}))
	})

	ginkgo.It("keeps exported rule metadata snapshots detached from the rule set", func() {
		rules := testRuleSet()

		metadata := rules.AllRuleMetadata()
		metadata[ruleSemanticDirectCast] = checkerpolicy.WaivableRule(ruleSemanticDirectCast, checkerpolicy.WaiverScopeCall)

		stored, ok := rules.MetadataForRule(ruleSemanticDirectCast)
		Expect(policyBoolState(ok)).To(Equal(policyBoolAccepted))
		Expect(stored).To(Equal(checkerpolicy.HardRule(ruleSemanticDirectCast)))
	})

	ginkgo.It("exposes the owning analysis pass without hiding AST ancestry", func() {
		ctx, pass, file := newGinkgoPolicyHarness(`package p

func check() {
	Equal(0)
}
`)
		call := findPolicyCall(file, "Equal")

		Expect(ctx.Pass()).To(Equal(pass))
		Expect(ctx.EnclosingDeclaration(call)).To(BeAssignableToTypeOf(&ast.FuncDecl{}))
	})

	ginkgo.It("finds value and type declarations around diagnostic nodes", func() {
		ctx, _, file := newGinkgoPolicyHarness(`package p

type policyType struct{}
var policyValue = Equal(0)
`)

		Expect(ctx.EnclosingDeclaration(findTypeName(file, "policyType"))).To(BeAssignableToTypeOf(&ast.TypeSpec{}))
		Expect(ctx.EnclosingDeclaration(findPolicyCall(file, "Equal"))).To(BeAssignableToTypeOf(&ast.ValueSpec{}))
		Expect(ctx.EnclosingDeclaration(file)).To(BeNil())
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
})

func policyBoolState(value bool) policyBoolObservation {
	if value {
		return policyBoolAccepted
	}
	return policyBoolRejected
}

func observeFormattedDiagnostic(message string) formattedDiagnosticObservation {
	rulePrefix, payload, ok := strings.Cut(message, ": ")
	if !ok {
		return formattedDiagnosticObservation{State: formattedDiagnosticMalformed}
	}
	rawRule, ok := strings.CutPrefix(rulePrefix, "semh:")
	if !ok {
		return formattedDiagnosticObservation{State: formattedDiagnosticMalformed}
	}
	return formattedDiagnosticObservation{
		State:   formattedDiagnosticObserved,
		Rule:    checkerpolicy.RuleID(rawRule),
		Payload: payload,
	}
}

func newGinkgoPolicyHarness(src string) (*checkerpolicy.Context, *analysis.Pass, *ast.File) {
	ginkgo.GinkgoHelper()

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "/repo/policy_test.go", src, parser.ParseComments)
	Expect(err).To(Succeed())

	pass := &analysis.Pass{
		Fset:  fset,
		Files: []*ast.File{file},
		Report: func(analysis.Diagnostic) {
		},
	}
	return checkerpolicy.NewContext(pass, testRuleSet(), true), pass, file
}

func findPolicyCall(file *ast.File, name string) *ast.CallExpr {
	ginkgo.GinkgoHelper()

	var found *ast.CallExpr
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		if ident, ok := call.Fun.(*ast.Ident); ok && ident.Name == name {
			found = call
			return false
		}
		return true
	})
	Expect(found).NotTo(BeNil(), "call %q should exist", name)
	return found
}

func findTypeName(file *ast.File, name string) *ast.Ident {
	ginkgo.GinkgoHelper()

	var found *ast.Ident
	ast.Inspect(file, func(node ast.Node) bool {
		spec, ok := node.(*ast.TypeSpec)
		if !ok || spec.Name.Name != name {
			return true
		}
		found = spec.Name
		return false
	})
	Expect(found).NotTo(BeNil(), "type %q should exist", name)
	return found
}
