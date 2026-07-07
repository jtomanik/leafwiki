package testhygiene

import (
	"go/ast"
	"go/token"
	"go/types"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("testhygiene boolean helper contracts", ginkgo.Label("unit"), func() {
	ginkgo.It("classifies string state helpers and semantic boolean aliases", func() {
		h := newRuleHarness("/repo/internal/wiki/boolean_state_test.go", "github.com/perber/wiki/internal/wiki", `package wiki

type WorkspaceID string

func IsReady() bool { return true }
func readyState(ok bool) string {
	if ok {
		return "ready"
	}
	return "blocked"
}
func statusLabel() string {
	if IsReady() {
		return "ready"
	}
	return "blocked"
}
func plainName(ok bool) string {
	if ok {
		return "ready"
	}
	return "blocked"
}
func findWorkspace() (WorkspaceID, bool) { return "", false }
func findPlain() (string, bool) { return "", false }

func TestBooleanHelpers(raw map[string]string, rawAny map[string]any, value any, events chan string) {
	_ = readyState(IsReady())
	_ = statusLabel()
	_ = plainName(IsReady())
	_, ok := raw["value"]
	alias := raw["value"]
	typedAlias := rawAny["typed"].(string)
	var declaredAlias = raw["declared"]
	firstAlias, secondAlias := raw["first"], raw["second"]
	_, asserted := value.(string)
	_, received := <-events
	workspace, found := findWorkspace()
	var _, available = findWorkspace()
	_, present := findPlain()
	_ = ok
	_ = alias
	_ = typedAlias
	_ = declaredAlias
	_ = firstAlias
	_ = secondAlias
	_ = asserted
	_ = received
	_ = workspace
	_ = found
	_ = available
	_ = present
}
`)
		readyStateCall := h.findCall("readyState")
		statusLabelCall := h.findCall("statusLabel")
		plainNameCall := h.findCall("plainName")
		okIdent := lastIdentifierNamed(h.file, "ok")
		aliasIdent := lastIdentifierNamed(h.file, "alias")
		typedAliasIdent := lastIdentifierNamed(h.file, "typedAlias")
		declaredAliasIdent := lastIdentifierNamed(h.file, "declaredAlias")
		firstAliasIdent := lastIdentifierNamed(h.file, "firstAlias")
		secondAliasIdent := lastIdentifierNamed(h.file, "secondAlias")
		assertedIdent := lastIdentifierNamed(h.file, "asserted")
		receivedIdent := lastIdentifierNamed(h.file, "received")
		foundIdent := lastIdentifierNamed(h.file, "found")
		availableIdent := lastIdentifierNamed(h.file, "available")
		presentIdent := lastIdentifierNamed(h.file, "present")

		Expect(observeHelperDecisions(
			callLaundersBooleanToStringState(h.ctx, readyStateCall),
			callLaundersBooleanToStringState(h.ctx, statusLabelCall),
			callLaundersBooleanToStringState(h.ctx, plainNameCall),
			testLocalBooleanStateHelperCall(h.ctx, &ast.CallExpr{Fun: &ast.SelectorExpr{Sel: ast.NewIdent("readyState")}}),
			testLocalBooleanStateHelperDecl(h.ctx, &ast.CallExpr{Fun: ast.NewIdent("missingState")}) != nil,
			identIsCommaOKResult(h.ctx, okIdent),
			identIsCommaOKResult(h.ctx, assertedIdent),
			identIsCommaOKResult(h.ctx, receivedIdent),
			identIsSemanticBooleanResult(h.ctx, foundIdent),
			identIsSemanticBooleanResult(h.ctx, availableIdent),
			identIsSemanticBooleanResult(h.ctx, presentIdent),
			assertionUsesMapIndexEqual(h.ctx, gomegaAssertion{actual: aliasIdent}),
			assertionUsesMapIndexEqual(h.ctx, gomegaAssertion{actual: typedAliasIdent}),
			assertionUsesMapIndexEqual(h.ctx, gomegaAssertion{actual: declaredAliasIdent}),
			assertionUsesMapIndexEqual(h.ctx, gomegaAssertion{actual: firstAliasIdent}),
			assertionUsesMapIndexEqual(h.ctx, gomegaAssertion{actual: secondAliasIdent}),
			assertionUsesMapIndexEqual(h.ctx, gomegaAssertion{actual: okIdent}),
			compositeActualContainsIdent(&ast.UnaryExpr{Op: token.AND, X: &ast.CompositeLit{Elts: []ast.Expr{aliasIdent}}}, func(ident *ast.Ident) bool {
				return ident.Name == "alias"
			}),
			exprTreeContainsBoolCall(h.ctx, &ast.FuncLit{}),
			typeIsMap(nil),
			typeIsSliceOrArray(nil),
		)).To(Equal([]helperDecision{
			helperAccepted,
			helperAccepted,
			helperRejected,
			helperRejected,
			helperRejected,
			helperAccepted,
			helperAccepted,
			helperAccepted,
			helperAccepted,
			helperAccepted,
			helperRejected,
			helperAccepted,
			helperAccepted,
			helperAccepted,
			helperAccepted,
			helperAccepted,
			helperRejected,
			helperAccepted,
			helperRejected,
			helperRejected,
			helperRejected,
		}))
	})

	ginkgo.It("keeps low-level boolean source classifiers narrow", func() {
		Expect(observeHelperDecisions(
			exprIsCommaOKSource(nilAnalysisContext(), &ast.TypeAssertExpr{}),
			exprIsCommaOKSource(nilAnalysisContext(), &ast.UnaryExpr{Op: token.ARROW}),
			exprIsCommaOKSource(nilAnalysisContext(), &ast.UnaryExpr{Op: token.NOT}),
			isBooleanProducingBinaryOp(token.EQL),
			isBooleanProducingBinaryOp(token.ADD),
			typeIsMap(types.NewMap(types.Typ[types.String], types.Typ[types.String])),
			typeIsSliceOrArray(types.NewSlice(types.Typ[types.String])),
			typeIsSliceOrArray(types.NewArray(types.Typ[types.String], 2)),
			typeIsSliceOrArray(types.Typ[types.String]),
		)).To(Equal([]helperDecision{
			helperAccepted,
			helperAccepted,
			helperRejected,
			helperAccepted,
			helperRejected,
			helperAccepted,
			helperAccepted,
			helperAccepted,
			helperRejected,
		}))
	})
})

func nilAnalysisContext() *analysisContext {
	return &analysisContext{}
}

func lastIdentifierNamed(file *ast.File, name string) *ast.Ident {
	var found *ast.Ident
	ast.Inspect(file, func(node ast.Node) bool {
		ident, ok := node.(*ast.Ident)
		if ok && ident.Name == name {
			found = ident
		}
		return true
	})
	Expect(found).NotTo(BeNil())
	return found
}
