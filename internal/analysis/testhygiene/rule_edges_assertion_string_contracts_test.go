package testhygiene

import (
	"go/ast"
	"go/token"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("testhygiene rendered string assertion contracts", ginkgo.Label("unit"), func() {
	ginkgo.It("classifies nested rendered-prose matchers and semantic scalar empty checks", func() {
		h := newRuleHarness("/repo/internal/wiki/string_assertion_test.go", "github.com/perber/wiki/internal/wiki", `package wiki

type assertion struct{}
type validationIssue struct {
	Message string
	Code    string
}
type protocolResult struct {
	Message      string
	LastError    string
	StatusCode   int
}
type pageRecord struct{ ID string }
type revisionRecord struct{ Hash string }
type sessionState struct{ ContextToken string }

func Expect(actual any) assertion { return assertion{} }
func (assertion) To(matcher any, extra ...any) {}
func (assertion) NotTo(matcher any, extra ...any) {}
func Equal(expected any) any { return nil }
func BeEmpty() any { return nil }
func Not(matcher any) any { return nil }
func ContainSubstring(expected string) any { return nil }
func HavePrefix(expected string) any { return nil }
func Wrap(matcher any) any { return nil }
func WithTransform(transform any, matcher any) any { return nil }
func BeTrue() any { return nil }
func StatusCode() int { return 200 }
func localPredicate(err error) bool { return err != nil }
func IsReady() bool { return true }
func PlainBool() bool { return true }

func TestRenderedStrings(
	result protocolResult,
	issue validationIssue,
	page pageRecord,
	revision revisionRecord,
	session sessionState,
	matcher any,
) {
	Expect(result.Message).To(Wrap([]any{ContainSubstring("boom")}))
	Expect(result.Message).To(Wrap(map[string]any{"message": HavePrefix("boom")}))
	Expect(result.Message).To(Wrap(matcher.(any)))
	Expect(issue.Message).To(ContainSubstring("boom"))
	Expect(page.ID).NotTo(Equal(""))
	Expect(revision.Hash).NotTo(Equal(""))
	Expect(session.ContextToken).NotTo(Equal(""))
	Expect(StatusCode()).To(Equal(500))
	_ = WithTransform(localPredicate, BeTrue())
	_ = IsReady()
	_ = PlainBool()
}
`)
		assertions := assertionsFromCalls(h)
		withTransformCall := h.findCall("WithTransform")

		Expect(assertions).To(HaveLen(8))
		Expect(observeHelperDecisions(
			assertionUsesRenderedMessageStringMatcher(h.ctx, assertions[0]),
			assertionUsesRenderedMessageStringMatcher(h.ctx, assertions[1]),
			assertionUsesRenderedMessageStringMatcher(h.ctx, assertions[2]),
			assertionUsesRenderedMessageStringMatcher(h.ctx, assertions[3]),
			assertionUsesSemanticScalarNotEmpty(h.ctx, assertions[4]),
			assertionUsesSemanticScalarNotEmpty(h.ctx, assertions[5]),
			assertionUsesSemanticScalarNotEmpty(h.ctx, assertions[6]),
			assertionUsesRawStatusCode(assertions[7]),
			matcherUsesOSIsNotExistTransform(h.ctx, withTransformCall),
		)).To(Equal([]helperDecision{
			helperAccepted,
			helperAccepted,
			helperRejected,
			helperRejected,
			helperAccepted,
			helperAccepted,
			helperAccepted,
			helperAccepted,
			helperRejected,
		}))
		Expect(observeHelperDecisions(
			isEqualEmptyStringMatcher(&ast.CallExpr{Fun: ast.NewIdent("NotEqual")}),
			numericLiteralArg(ast.NewIdent("statusCode")),
			exprSuggestsRawStatusCode(&ast.CallExpr{Fun: ast.NewIdent("StatusCode")}),
			semanticScalarTokenName("token"),
			semanticScalarTokenName("ContextToken"),
			callReturnsProxyBoolean(h.ctx, h.findCall("IsReady")),
			callReturnsProxyBoolean(h.ctx, h.findCall("PlainBool")),
		)).To(Equal([]helperDecision{
			helperRejected,
			helperRejected,
			helperAccepted,
			helperRejected,
			helperAccepted,
			helperAccepted,
			helperRejected,
		}))
		Expect(referencedFunctionPackageName(h.ctx, ast.NewIdent("localPredicate"))).To(Equal(functionPackageName{
			PackagePath: "",
			Name:        "localPredicate",
		}))
		Expect(referencedFunctionPackageName(h.ctx, &ast.SelectorExpr{Sel: ast.NewIdent("Fallback")})).To(Equal(functionPackageName{
			PackagePath: "",
			Name:        "Fallback",
		}))
		Expect(referencedFunctionPackageName(h.ctx, &ast.BasicLit{Kind: token.STRING, Value: `"literal"`})).To(Equal(functionPackageName{}))
	})
})

type functionPackageName struct {
	PackagePath string
	Name        string
}

func assertionsFromCalls(h *ruleHarness) []gomegaAssertion {
	ginkgo.GinkgoHelper()

	var calls []*ast.CallExpr
	ast.Inspect(h.file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if ok && (callName(call) == "To" || callName(call) == "NotTo") {
			calls = append(calls, call)
		}
		return true
	})
	assertions := make([]gomegaAssertion, 0, len(calls))
	for _, call := range calls {
		assertion, ok := gomegaAssertionFromCall(h.ctx, call)
		Expect(observeHelperDecision(ok)).To(Equal(helperAccepted))
		assertions = append(assertions, assertion)
	}
	return assertions
}

func referencedFunctionPackageName(ctx *analysisContext, expr ast.Expr) functionPackageName {
	packagePath, name := referencedFunctionPackageAndName(ctx, expr)
	return functionPackageName{PackagePath: packagePath, Name: name}
}
