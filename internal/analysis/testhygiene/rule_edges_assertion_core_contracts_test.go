package testhygiene

import (
	"go/ast"
	"go/types"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("testhygiene assertion core contracts", ginkgo.Label("unit"), func() {
	ginkgo.It("classifies error-oriented Gomega assertions over typed snippets", func() {
		h := newRuleHarness("/repo/internal/wiki/error_test.go", "github.com/perber/wiki/internal/wiki", `package wiki

import "strings"

type assertion struct{}
type issueReport struct{ Err error }

func Expect(actual any, extra ...any) assertion { return assertion{} }
func ExpectWithOffset(offset int, actual any, extra ...any) assertion { return assertion{} }
func Ω(actual any, extra ...any) assertion { return assertion{} }
func (assertion) To(matcher any, extra ...any) {}
func (assertion) NotTo(matcher any, extra ...any) {}
func (assertion) Error() assertion { return assertion{} }
func Equal(expected any) any { return nil }
func BeNil() any { return nil }
func HaveOccurred() any { return nil }
func Succeed() any { return nil }
func MatchError(expected any) any { return nil }
func HaveField(name string, matcher any) any { return nil }
func SatisfyAll(args ...any) any { return nil }
func Not(matcher any) any { return nil }

func readOne() error { return nil }
func readMany() (string, error) { return "", nil }
func TestAssertions(err error, renderedMessage string) {
	Expect(err.Error()).To(Equal("boom"))
	Expect(strings.Contains(err.Error(), "boom")).To(Equal(true))
	Expect(strings.Contains(renderedMessage, "boom")).To(Equal(true))
	Expect(err).To(Equal(nil))
	Expect(err).To(BeNil())
	Expect(readOne()).NotTo(HaveOccurred())
	Expect(err).To(HaveOccurred())
	ExpectWithOffset(1, err).To(MatchError("boom"))
	Ω(err).To(MatchError("boom"))
	Expect(err).Error().To(MatchError("boom"))
	Expect(readMany()).To(HaveOccurred())
	Expect(readMany()).To(Succeed())
	Expect(issueReport{Err: err}).To(HaveField("Err", Not(BeNil())))
	Expect(issueReport{Err: err}).To(SatisfyAll([]any{HaveField("Err", Not(Equal(nil)))}...))
}
`)
		toCalls := h.findCalls("To")
		notToCall := h.findCall("NotTo")
		assertions := make([]gomegaAssertion, 0, len(toCalls)+1)
		okValues := make([]bool, 0, len(toCalls)+1)
		for _, call := range append(toCalls, notToCall) {
			assertion, ok := gomegaAssertionFromCall(h.ctx, call)
			assertions = append(assertions, assertion)
			okValues = append(okValues, ok)
		}

		Expect(okValues).To(Equal([]bool{
			true, true, true, true, true, true, true, true, true, true, true, true, true, true,
		}))
		Expect([]bool{
			assertionUsesErrError(h.ctx, assertions[0]),
			assertionUsesStringsContains(h.ctx, assertions[1]),
			assertionUsesErrError(h.ctx, assertions[1]),
			assertionUsesStringsContains(h.ctx, assertions[2]),
			assertionUsesErrorNilMatcher(h.ctx, assertions[3]),
			assertionUsesErrorNilMatcher(h.ctx, assertions[4]),
			assertionUsesGenericHaveOccurred(h.ctx, assertions[5]),
			assertionUsesMultiReturnErrorMatcher(h.ctx, assertions[8]),
			assertionUsesMultiReturnErrorMatcher(h.ctx, assertions[9]),
			assertionUsesMultiReturnErrorMatcher(h.ctx, assertions[10]),
			assertionMatcherTreeUsesGenericErrorPresence(assertions[11]),
			assertionMatcherTreeUsesGenericErrorPresence(assertions[12]),
			assertionUsesInlineErrorReturnHaveOccurred(h.ctx, assertions[13]),
			isNegativeAssertionMethod(assertions[13].method),
			gomegaAssertionHasErrorProjection(assertions[8]),
			callResultTuple(h.ctx, assertions[10].actual.(*ast.CallExpr)) != nil,
			callResultTuple(h.ctx, &ast.CallExpr{Fun: ast.NewIdent("unknown")}) != nil,
		}).To(Equal([]bool{
			true, true, false, true, true, true, true, false, true, true, true, true, true, true, true, true, false,
		}))

		tupleCall := &ast.CallExpr{Fun: ast.NewIdent("tuple")}
		h.ctx.pass.TypesInfo.Types[tupleCall] = types.TypeAndValue{
			Type: types.NewTuple(types.NewVar(0, nil, "", types.Typ[types.String])),
		}
		Expect(callResultTuple(h.ctx, tupleCall)).NotTo(BeNil())

		for _, call := range h.findCalls("Contains") {
			checkErrorStringPredicate(h.ctx, call)
		}

		Expect(h.diagnosticMessages()).To(ContainElements(
			"semh:gomega.err-error-string: assert error values with MatchError instead of matching err.Error()",
			"semh:gomega.strings-contains: do not assert rendered message text with strings.Contains; assert structured code, message ID, field, or path semantics instead",
		))
	})

	ginkgo.It("classifies matcher-tree error assertions through composite matcher shapes", func() {
		h := newRuleHarness("/repo/internal/wiki/error_test.go", "github.com/perber/wiki/internal/wiki", `package wiki

type assertion struct{}
type issueReport struct{ Err error }

func Expect(actual any) assertion { return assertion{} }
func (assertion) To(matcher any, extra ...any) {}
func Equal(expected any) any { return nil }
func HaveOccurred() any { return nil }
func HaveField(name string, matcher any) any { return nil }
func MatchFields(options any, fields map[string]any) any { return nil }
func Not(matcher any) any { return nil }

func TestMatcherTrees(err error) {
	Expect(issueReport{Err: err}).To(MatchFields(nil, map[string]any{
		"Err": HaveOccurred(),
	}))
	Expect(issueReport{Err: err}).To(MatchFields(nil, map[string]any{
		"Err": Not(Equal(nil)),
	}))
	Expect(issueReport{Err: err}).To(HaveField("Err", Not(Equal(nil))))
}
`)
		calls := h.findCalls("To")
		assertions := make([]gomegaAssertion, 0, len(calls))
		for _, call := range calls {
			assertion, ok := gomegaAssertionFromCall(h.ctx, call)
			Expect(observeHelperDecision(ok)).To(Equal(helperAccepted))
			assertions = append(assertions, assertion)
		}

		Expect(observeHelperDecisions(
			assertionMatcherTreeUsesGenericHaveOccurred(assertions[0]),
			assertionMatcherTreeUsesGenericErrorPresence(assertions[1]),
			assertionMatcherTreeUsesGenericErrorPresence(assertions[2]),
			errorPresenceMatcherField(ast.NewIdent("lastErrors")),
			errorPresenceMatcherField(ast.NewIdent("status")),
		)).To(Equal([]helperDecision{helperAccepted, helperAccepted, helperAccepted, helperAccepted, helperRejected}))
	})

	ginkgo.It("rejects malformed Gomega assertion call shapes", func() {
		h := newRuleHarness("/repo/internal/wiki/assertion_test.go", "github.com/perber/wiki/internal/wiki", `package wiki

type assertion struct{}
func Expect(actual any) assertion { return assertion{} }
func Eventually(args ...any) assertion { return assertion{} }
func (assertion) WithContext(ctx any) assertion { return assertion{} }
func (assertion) To(matcher any, extra ...any) {}
func (assertion) Should(matcher any, extra ...any) {}
func Equal(expected any) any { return nil }

func TestParsing() {
	Expect("done").To(Equal("done"))
	Eventually(func() {}).WithContext(nil).Should(Equal("done"))
}
`)
		parsedAssertion, assertionOK := gomegaAssertionFromCall(h.ctx, h.findCall("To"))
		parsedAsync, asyncOK := gomegaAsyncAssertionFromCall(h.findCall("Should"))

		Expect(observeHelperDecisions(
			assertionOK,
			parsedAssertion.actual != nil,
			asyncOK,
			parsedAsync.actual != nil,
			gomegaSourceCallSucceeded(&ast.Ident{Name: "notCall"}),
			gomegaAsyncSourceCallSucceeded(&ast.Ident{Name: "notCall"}),
			gomegaExpectationActualSucceeded(&ast.CallExpr{Fun: ast.NewIdent("Expect")}),
			gomegaExpectationActualSucceeded(&ast.CallExpr{Fun: ast.NewIdent("ExpectWithOffset")}),
			gomegaExpectationActualSucceeded(&ast.CallExpr{Fun: ast.NewIdent("Unknown")}),
			gomegaAsyncActualSucceeded(&ast.CallExpr{Fun: ast.NewIdent("Eventually")}),
			gomegaAsyncActualSucceeded(&ast.CallExpr{Fun: ast.NewIdent("EventuallyWithOffset")}),
			gomegaAsyncActualSucceeded(&ast.CallExpr{Fun: ast.NewIdent("Unknown")}),
			gomegaAssertionCallSucceeded(h.ctx, &ast.CallExpr{Fun: ast.NewIdent("To")}),
			gomegaAssertionCallSucceeded(h.ctx, &ast.CallExpr{
				Fun:  ast.NewIdent("To"),
				Args: []ast.Expr{ast.NewIdent("notCall")},
			}),
			gomegaAsyncAssertionCallSucceeded(&ast.CallExpr{Fun: ast.NewIdent("Should")}),
			gomegaAsyncAssertionCallSucceeded(&ast.CallExpr{
				Fun:  ast.NewIdent("Should"),
				Args: []ast.Expr{ast.NewIdent("notCall")},
			}),
		)).To(Equal([]helperDecision{
			helperAccepted, helperAccepted, helperAccepted, helperAccepted,
			helperRejected, helperRejected, helperRejected, helperRejected,
			helperRejected, helperRejected, helperRejected, helperRejected,
			helperRejected, helperRejected, helperRejected, helperRejected,
		}))
	})
})

func gomegaSourceCallSucceeded(expr ast.Expr) bool {
	_, ok := gomegaSourceCall(expr)
	return ok
}

func gomegaAsyncSourceCallSucceeded(expr ast.Expr) bool {
	_, ok := gomegaAsyncSourceCall(expr)
	return ok
}

func gomegaExpectationActualSucceeded(call *ast.CallExpr) bool {
	_, ok := gomegaExpectationActual(call)
	return ok
}

func gomegaAsyncActualSucceeded(call *ast.CallExpr) bool {
	_, ok := gomegaAsyncActual(call)
	return ok
}

func gomegaAssertionCallSucceeded(ctx *analysisContext, call *ast.CallExpr) bool {
	_, ok := gomegaAssertionFromCall(ctx, call)
	return ok
}

func gomegaAsyncAssertionCallSucceeded(call *ast.CallExpr) bool {
	_, ok := gomegaAsyncAssertionFromCall(call)
	return ok
}
