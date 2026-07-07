package testhygiene

import (
	"go/ast"
	"go/types"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("testhygiene async helper contracts", ginkgo.Label("unit"), func() {
	ginkgo.It("classifies async assertions that use or omit spec context", func() {
		h := newRuleHarness("/repo/internal/wiki/page_test.go", "github.com/perber/wiki/internal/wiki", `package wiki

type SpecContext struct{}
type AsyncAssertion struct{}
func It(description string, body func(SpecContext)) {}
func Eventually(args ...any) AsyncAssertion { return AsyncAssertion{} }
func (AsyncAssertion) WithContext(ctx any) AsyncAssertion { return AsyncAssertion{} }
func (AsyncAssertion) Should(matcher any, extra ...any) {}
func (AsyncAssertion) ShouldNot(matcher any, extra ...any) {}
func BeTrue() any { return nil }
func Receive(args ...any) any { return nil }

func specs() {
	It("polls semantic state", func(ctx SpecContext) {
		flag := false
		events := make(chan string)
		Eventually(flag).Should(BeTrue())
		Eventually(ctx, func() bool { return flag }).Should(BeTrue())
		Eventually(func() bool { return flag }).WithContext(ctx).Should(BeTrue())
		Eventually(events).ShouldNot(Receive())
	})
}
`)
		shouldCalls := h.findCalls("Should")
		firstAssertion, firstOK := gomegaAsyncAssertionFromCall(shouldCalls[0])
		secondAssertion, secondOK := gomegaAsyncAssertionFromCall(shouldCalls[1])
		thirdAssertion, thirdOK := gomegaAsyncAssertionFromCall(shouldCalls[2])
		negativeAssertion, negativeOK := gomegaAsyncAssertionFromCall(h.findCall("ShouldNot"))

		Expect(observeHelperDecisions(firstOK, secondOK, thirdOK, negativeOK)).To(Equal(
			[]helperDecision{helperAccepted, helperAccepted, helperAccepted, helperAccepted},
		))
		Expect([]bool{
			asyncAssertionRequiresSpecContext(h.ctx, firstAssertion),
			asyncAssertionRequiresSpecContext(h.ctx, secondAssertion),
			asyncAssertionRequiresSpecContext(h.ctx, thirdAssertion),
			assertionUsesAsyncBooleanMatcher(h.ctx, firstAssertion),
			assertionUsesAsyncBooleanMatcher(h.ctx, secondAssertion),
			assertionUsesAsyncBooleanMatcher(h.ctx, thirdAssertion),
			isNegativeAssertionMethod(negativeAssertion.method),
		}).To(Equal([]bool{true, false, false, true, false, true, true}))

		for _, call := range append(shouldCalls, h.findCall("ShouldNot")) {
			checkGomegaAsyncAssertion(h.ctx, call)
		}

		Expect(h.diagnosticMessages()).To(ContainElements(
			"semh:gomega.async-context: propagate the spec context into Eventually/Consistently with WithContext or positional context",
			"semh:gomega.async-bare-value: wrap bare eventually-polled values in a function so polling re-reads changing state",
			"semh:gomega.async-boolean: poll a semantic value or assertion callback instead of Eventually/Consistently boolean results with BeTrue/BeFalse",
			"semh:gomega.async-negative-receive: use Consistently(...).ShouldNot(Receive()) to prove a channel stays quiet",
		))
	})

	ginkgo.It("classifies async helper fallback branches", func() {
		h := newRuleHarness("/repo/internal/wiki/page_test.go", "github.com/perber/wiki/internal/wiki", `package wiki

type AsyncAssertion struct{}
func Eventually(args ...any) AsyncAssertion { return AsyncAssertion{} }
func (AsyncAssertion) Should(matcher any, extra ...any) {}
func Equal(expected any) any { return nil }

func TestPolling() {
	Eventually(func() {}).Should(Equal("done"))
}
`)
		assertion, ok := gomegaAsyncAssertionFromCall(h.findCall("Should"))
		contextExpr := ast.NewIdent("contextAlias")
		otherContextExpr := ast.NewIdent("otherContextAlias")
		contextPackage := types.NewPackage("context", "context")
		otherPackage := types.NewPackage("example.com/context", "context")
		h.ctx.pass.TypesInfo.Types[contextExpr] = types.TypeAndValue{
			Type: types.NewNamed(types.NewTypeName(0, contextPackage, "Context", nil), types.NewInterfaceType(nil, nil), nil),
		}
		h.ctx.pass.TypesInfo.Types[otherContextExpr] = types.TypeAndValue{
			Type: types.NewNamed(types.NewTypeName(0, otherPackage, "Context", nil), types.NewInterfaceType(nil, nil), nil),
		}

		Expect(observeHelperDecision(ok)).To(Equal(helperAccepted))
		Expect([]bool{
			asyncAssertionRequiresSpecContext(h.ctx, assertion),
			asyncActualProducesBool(h.ctx, assertion.actual),
			funcLitReturnsBool(h.ctx, assertion.actual.(*ast.FuncLit)),
			isContextParamType(h.ctx, ast.NewIdent("Context")),
			isContextParamType(h.ctx, ast.NewIdent("SpecContext")),
			isContextParamType(h.ctx, ast.NewIdent("Other")),
			isContextParamType(h.ctx, contextExpr),
			isContextParamType(h.ctx, otherContextExpr),
			isBoolType(nil),
			isBoolType(types.Typ[types.UntypedBool]),
		}).To(Equal([]bool{false, false, false, true, true, false, true, false, false, true}))
	})

	ginkgo.It("classifies nested matcher values and positional transform edge shapes", func() {
		h := newRuleHarness("/repo/internal/wiki/matcher_test.go", "github.com/perber/wiki/internal/wiki", `package wiki

type assertion struct{}
type AsyncAssertion struct{}
type GomegaMatcher interface{}
type pageSummary struct {
	Title string
	Path  string
}

func Eventually(args ...any) AsyncAssertion { return AsyncAssertion{} }
func (AsyncAssertion) Should(matcher any, extra ...any) {}
func Equal(expected any) GomegaMatcher { return nil }
func Not(matcher any) GomegaMatcher { return nil }
func BeTrue() GomegaMatcher { return nil }
func WithTransform(transform any, matcher any) GomegaMatcher { return nil }

func TestMatchers(page pageSummary, matcher GomegaMatcher) {
	_ = Equal(BeTrue())
	Eventually(func() bool { return true }).Should(Not(BeTrue()))
	_ = WithTransform(page.Title, Equal([]string{"Docs", "/docs"}))
	_ = WithTransform(func(page pageSummary) []string { return []string{page.Title} }, Equal([]string{"Docs"}))
	_ = WithTransform(func(page pageSummary) struct{ Title string } {
		return struct{ Title string }{Title: page.Title}
	}, Equal(struct{ Title string }{Title: "Docs"}))
	_ = WithTransform(func() []string { return []string{"Docs", "/docs"} }, Equal([]string{"Docs", "/docs"}))
	_ = WithTransform(func(page pageSummary) []string {
		_ = func() []string { return []string{page.Title, page.Path} }
		return []string{page.Title, "literal"}
	}, Equal([]string{"Docs", "/docs"}))
	_ = WithTransform(func(page pageSummary) []string {
		return []string{page.Title, page.Path}
	}, matcher)
	named := BeTrue()
	var declared = BeTrue()
	_ = Equal(named)
	_ = Equal(declared)
	_ = Equal(matcher)
	_ = Equal(page.Title)
}
`)
		transforms := h.findCalls("WithTransform")
		booleanMatchers := h.findCalls("BeTrue")
		equalCalls := h.findCalls("Equal")

		Expect(transforms).To(HaveLen(6))
		Expect(booleanMatchers).To(HaveLen(4))
		Expect(observeHelperDecisions(
			matcherCallIsNestedBooleanMatcherValue(h.ctx, booleanMatchers[0]),
			matcherCallIsNestedBooleanMatcherValue(h.ctx, booleanMatchers[1]),
			matcherUsesPositionalTransform(h.ctx, transforms[0]),
			matcherUsesPositionalTransform(h.ctx, transforms[1]),
			matcherUsesPositionalTransform(h.ctx, transforms[2]),
			matcherUsesPositionalTransform(h.ctx, transforms[3]),
			matcherUsesPositionalTransform(h.ctx, transforms[4]),
			matcherUsesPositionalTransform(h.ctx, transforms[5]),
			compositeLiteralIsPositional(&ast.CompositeLit{Type: ast.NewIdent("row"), Elts: []ast.Expr{
				&ast.KeyValueExpr{Key: ast.NewIdent("Title"), Value: ast.NewIdent("value")},
			}}),
			compositeLiteralIsPositional(&ast.CompositeLit{}),
			matcherCallUsesMatcherValueAsExpected(h.ctx, equalCalls[len(equalCalls)-4]),
			matcherCallUsesMatcherValueAsExpected(h.ctx, equalCalls[len(equalCalls)-3]),
			matcherCallUsesMatcherValueAsExpected(h.ctx, equalCalls[len(equalCalls)-2]),
			matcherCallUsesMatcherValueAsExpected(h.ctx, equalCalls[len(equalCalls)-1]),
		)).To(Equal([]helperDecision{
			helperAccepted,
			helperAccepted,
			helperRejected,
			helperRejected,
			helperRejected,
			helperRejected,
			helperRejected,
			helperRejected,
			helperRejected,
			helperRejected,
			helperAccepted,
			helperAccepted,
			helperAccepted,
			helperRejected,
		}))
	})
})
