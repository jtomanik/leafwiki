package testhygiene

import (
	"go/ast"
	"go/token"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("testhygiene matcher factory and runtime contracts", ginkgo.Label("unit"), func() {
	ginkgo.It("classifies matcher factory predicates by the semantic contract they hide", func() {
		h := newRuleHarness("/repo/internal/wiki/matcher_test.go", "github.com/perber/wiki/internal/wiki", `package wiki

import "strings"

type GomegaMatcher interface{}
type toolResult struct {
	Count     int
	IsError   bool
	LastError string
	Ready     bool
}

func MakeMatcher(predicate any) GomegaMatcher { return nil }
func Wrap(matcher GomegaMatcher) GomegaMatcher { return matcher }

func matchGenericError() GomegaMatcher {
	return MakeMatcher(func(err error) bool { return err != nil })
}

func matchNestedGenericError() GomegaMatcher {
	return Wrap(MakeMatcher(func(err error) bool { return nil != err }))
}

func matchProtocolStatus() GomegaMatcher {
	return MakeMatcher(func(result *toolResult) bool { return result != nil && result.IsError })
}

func matchRenderedLastError() GomegaMatcher {
	return MakeMatcher(func(result toolResult) bool { return strings.Contains(result.LastError, "boom") })
}

func matchProxyState() GomegaMatcher {
	return MakeMatcher(func(result toolResult) bool { return result.Ready && !result.Ready })
}

func matchPredicateOnly() GomegaMatcher {
	return MakeMatcher(func(result toolResult) bool { return result.Count > 0 })
}

func matchLiteralPredicate() GomegaMatcher {
	return MakeMatcher(func(result toolResult) bool { return true })
}
`)
		calls := h.findCalls("MakeMatcher")
		returns := returnExpressionsByFunction(h.file)

		Expect(calls).To(HaveLen(7))
		Expect(observeHelperDecisions(
			gomegaGenericErrorPredicateMatcher(h.ctx, calls[0]) != nil,
			gomegaGenericErrorPredicateMatcherInExpr(h.ctx, returns["matchNestedGenericError"]) != nil,
			gomegaStructuredProtocolStatusPredicateMatcher(h.ctx, calls[2]) != nil,
			gomegaLastErrorRenderedPredicateMatcher(h.ctx, calls[3]) != nil,
			gomegaProxyBooleanPredicateMatcher(h.ctx, calls[4]) != nil,
			gomegaPredicateOnlyBooleanMatcher(h.ctx, calls[5]) != nil,
			gomegaPredicateOnlyBooleanMatcher(h.ctx, calls[4]) != nil,
			gomegaPredicateOnlyBooleanMatcher(h.ctx, calls[6]) != nil,
		)).To(Equal([]helperDecision{
			helperAccepted,
			helperAccepted,
			helperAccepted,
			helperAccepted,
			helperAccepted,
			helperAccepted,
			helperRejected,
			helperRejected,
		}))

		checkGomegaMatcherFactoryGenericHaveOccurred(h.ctx, h.findFunc("matchNestedGenericError"))

		Expect(h.diagnosticMessages()).To(ConsistOf(
			"semh:gomega.generic-have-occurred: assert expected error semantics with MatchError or a domain matcher instead of generic HaveOccurred",
		))
	})

	ginkgo.It("rejects generic matcher factory fallbacks without broadening accepted matcher contracts", func() {
		h := newRuleHarness("/repo/internal/wiki/matcher_edges_test.go", "github.com/perber/wiki/internal/wiki", `package wiki

import "strings"

type GomegaMatcher interface{}
type toolResult struct {
	Args      []string
	IsError   bool
	LastError string
	Ready     bool
}

var predicateValue any

func MakeMatcher(predicate any) GomegaMatcher { return nil }
func HaveField(name string, matcher any) GomegaMatcher { return nil }
func ContainSubstring(fragment string) GomegaMatcher { return nil }
func HavePrefix(fragment string) GomegaMatcher { return nil }
func MatchRegexp(pattern string) GomegaMatcher { return nil }
func HaveOccurred() GomegaMatcher { return nil }
func BeNil() GomegaMatcher { return nil }
func Not(matcher GomegaMatcher) GomegaMatcher { return nil }
func Or(matchers ...GomegaMatcher) GomegaMatcher { return nil }

func genericToolErrorArgs() GomegaMatcher {
	return HaveField("Args", ContainSubstring("secret"))
}

func genericToolErrorMatcher() GomegaMatcher {
	return nil
}

func matchNonFactory() GomegaMatcher {
	return HaveField("Args", HavePrefix("tool"))
}

func matchNonFuncArg() GomegaMatcher {
	return MakeMatcher(predicateValue)
}

func matchNoErrorParam() GomegaMatcher {
	return MakeMatcher(func(value string) bool { return value != "" })
}

func matchNestedLiteral() GomegaMatcher {
	return MakeMatcher(func(err error) bool {
		return func() bool { return err != nil }()
	})
}

func matchUnaryProxyFallback() GomegaMatcher {
	return MakeMatcher(func(result toolResult) bool { return +1 == 1 })
}

func matchProtocolFallback() GomegaMatcher {
	return MakeMatcher(func(result *toolResult) bool { return !result.Ready })
}

func matchProtocolRightSide() GomegaMatcher {
	return MakeMatcher(func(result *toolResult) bool { return result != nil || result.IsError })
}

func matchRenderedRightSide() GomegaMatcher {
	return MakeMatcher(func(result toolResult) bool { return result.Ready || strings.HasSuffix(result.LastError, "boom") })
}

func matchPositiveHaveOccurred() GomegaMatcher {
	return HaveOccurred()
}

func matchGenericPresence() GomegaMatcher {
	return Or(Not(BeNil()))
}
`)
		calls := h.findCalls("MakeMatcher")
		returns := returnExpressionsByFunction(h.file)

		Expect(calls).To(HaveLen(7))
		Expect(observeHelperDecisions(
			gomegaGenericErrorPredicateMatcher(h.ctx, h.findCall("HaveField")) != nil,
			matcherTreeContainsStringFragmentMatcher(h.ctx, returns["genericToolErrorArgs"]),
			gomegaGenericErrorPredicateMatcher(h.ctx, calls[0]) != nil,
			gomegaGenericErrorPredicateMatcher(h.ctx, calls[1]) != nil,
			gomegaGenericErrorPredicateMatcher(h.ctx, calls[2]) != nil,
			gomegaProxyBooleanPredicateMatcher(h.ctx, calls[3]) != nil,
			gomegaStructuredProtocolStatusPredicateMatcher(h.ctx, calls[4]) != nil,
			gomegaStructuredProtocolStatusPredicateMatcher(h.ctx, calls[5]) != nil,
			gomegaLastErrorRenderedPredicateMatcher(h.ctx, calls[6]) != nil,
		)).To(Equal([]helperDecision{
			helperRejected,
			helperAccepted,
			helperRejected,
			helperRejected,
			helperRejected,
			helperRejected,
			helperRejected,
			helperAccepted,
			helperAccepted,
		}))

		checkGomegaMatcherFactoryGenericToolErrorArgs(h.ctx, h.findFunc("genericToolErrorArgs"))
		checkGomegaMatcherFactoryGenericToolErrorArgs(h.ctx, h.findFunc("genericToolErrorMatcher"))
		checkGomegaMatcherFactoryGenericToolErrorArgs(h.ctx, h.findFunc("matchNonFactory"))
		checkGomegaMatcherFactoryGenericHaveOccurred(h.ctx, h.findFunc("matchPositiveHaveOccurred"))
		checkGomegaMatcherFactoryGenericHaveOccurred(h.ctx, h.findFunc("matchGenericPresence"))

		Expect(h.diagnosticMessages()).To(ConsistOf(
			"semh:gomega.generic-have-occurred: assert expected error semantics with MatchError or a domain matcher instead of generic HaveOccurred",
			"semh:gomega.generic-have-occurred: assert expected error semantics with MatchError or a domain matcher instead of generic HaveOccurred",
			"semh:gomega.generic-have-occurred: assert expected error semantics with MatchError or a domain matcher instead of generic HaveOccurred",
			"semh:gomega.generic-have-occurred: assert expected error semantics with MatchError or a domain matcher instead of generic HaveOccurred",
		))
	})

	ginkgo.It("keeps matcher factory predicate fallbacks tied to semantic signals", func() {
		h := newRuleHarness("/repo/internal/wiki/matcher_predicates_test.go", "github.com/perber/wiki/internal/wiki", `package wiki

type GomegaMatcher interface{}
type toolResult struct {
	IsError   bool
	LastError string
	Ready     bool
	Available bool
}

var predicateValue any

func MakeMatcher(predicate ...any) GomegaMatcher { return nil }
func Match(pattern string, text string) bool { return false }
func computeAvailable(result toolResult) bool { return result.Available }

func matchUnaryProtocol() GomegaMatcher {
	return MakeMatcher(func(result *toolResult) bool { return !result.IsError })
}

func matchProtocolWithRightNilGuard() GomegaMatcher {
	return MakeMatcher(func(result *toolResult) bool { return result.IsError && result != nil })
}

func matchProtocolWithPlainAnd() GomegaMatcher {
	return MakeMatcher(func(result *toolResult) bool { return result.Ready && result.IsError })
}

func matchRenderedMatch() GomegaMatcher {
	return MakeMatcher(func(result toolResult) bool { return Match("boom", result.LastError) })
}

func matchNestedRenderedFallback() GomegaMatcher {
	return MakeMatcher(func(result toolResult) bool {
		return func() bool { return result.LastError == "boom" }()
	})
}

func matchNoArgs() GomegaMatcher {
	return MakeMatcher()
}

func matchNonFuncArg() GomegaMatcher {
	return MakeMatcher(predicateValue)
}

func matchNoParams() GomegaMatcher {
	return MakeMatcher(func() bool { return true })
}

func matchUnnamedErrorParam() GomegaMatcher {
	return MakeMatcher(func(error) bool { return true })
}

func matchSelectorPredicate() GomegaMatcher {
	return MakeMatcher(func(result toolResult) bool { return result.Available })
}

func matchCallPredicate() GomegaMatcher {
	return MakeMatcher(func(result toolResult) bool { return computeAvailable(result) })
}

func matchUnaryCallPredicate() GomegaMatcher {
	return MakeMatcher(func(result toolResult) bool { return !computeAvailable(result) })
}

func matchBooleanChainPredicate() GomegaMatcher {
	return MakeMatcher(func(result toolResult) bool { return result.Available || computeAvailable(result) })
}
`)
		calls := callExpressionsByFunction(h.file, "MakeMatcher")

		Expect(observeHelperDecisions(
			gomegaStructuredProtocolStatusPredicateMatcher(h.ctx, calls["matchUnaryProtocol"]) != nil,
			gomegaStructuredProtocolStatusPredicateMatcher(h.ctx, calls["matchProtocolWithRightNilGuard"]) != nil,
			gomegaStructuredProtocolStatusPredicateMatcher(h.ctx, calls["matchProtocolWithPlainAnd"]) != nil,
			gomegaLastErrorRenderedPredicateMatcher(h.ctx, calls["matchRenderedMatch"]) != nil,
			gomegaLastErrorRenderedPredicateMatcher(h.ctx, calls["matchNestedRenderedFallback"]) != nil,
			gomegaPredicateOnlyBooleanMatcher(h.ctx, calls["matchNoArgs"]) != nil,
			gomegaPredicateOnlyBooleanMatcher(h.ctx, calls["matchNonFuncArg"]) != nil,
			gomegaGenericErrorPredicateMatcher(h.ctx, calls["matchNoParams"]) != nil,
			gomegaGenericErrorPredicateMatcher(h.ctx, calls["matchUnnamedErrorParam"]) != nil,
			gomegaPredicateOnlyBooleanMatcher(h.ctx, calls["matchSelectorPredicate"]) != nil,
			gomegaPredicateOnlyBooleanMatcher(h.ctx, calls["matchCallPredicate"]) != nil,
			gomegaPredicateOnlyBooleanMatcher(h.ctx, calls["matchUnaryCallPredicate"]) != nil,
			gomegaPredicateOnlyBooleanMatcher(h.ctx, calls["matchBooleanChainPredicate"]) != nil,
		)).To(Equal([]helperDecision{
			helperAccepted,
			helperAccepted,
			helperRejected,
			helperAccepted,
			helperRejected,
			helperRejected,
			helperRejected,
			helperRejected,
			helperRejected,
			helperAccepted,
			helperAccepted,
			helperAccepted,
			helperAccepted,
		}))
	})

	ginkgo.It("classifies runtime assertions and global mutations that need Ginkgo support", func() {
		h := newRuleHarness("/repo/internal/wiki/runtime_test.go", "github.com/perber/wiki/internal/wiki", `package wiki

import (
	"log/slog"
	"os"
)

type assertion struct{}
func Expect(actual any) assertion { return assertion{} }
func (assertion) To(matcher any, extra ...any) {}
func Equal(expected any) any { return nil }
func Fail(message string) {}
func GinkgoRecover() {}
func GinkgoHelper() {}
func DeferCleanup(cleanup any) {}
func SetDefaultEventuallyTimeout(timeout any) {}

func TestGoroutineRecovery(events chan string) {
	go func() {
		Fail("boom")
	}()
	go func() {
		defer GinkgoRecover()
		Fail("boom")
	}()
	go func() {
		func() {
			Fail("nested")
		}()
	}()
	<-events
	select {
	case <-events:
	default:
	}
	go func() {
		<-events
	}()
}

func TestGlobalState() {
	os.Setenv("LEAFWIKI_TEST", "1")
	slog.SetDefault(slog.Default())
	SetDefaultEventuallyTimeout(1)
}

func TestCoveredGlobalState() {
	DeferCleanup(func() {})
	os.Unsetenv("LEAFWIKI_TEST")
}

func assertLater(got string, want string) {
	Expect(got).To(Equal(want))
	GinkgoHelper()
}
`)
		for _, stmt := range goStatementsInFile(h.file) {
			checkGinkgoGoroutineAssertionRecovery(h.ctx, stmt)
		}
		for _, expr := range unaryExpressionsWithOperator(h.file, token.ARROW) {
			checkGinkgoBlockingReceive(h.ctx, expr)
		}
		for _, call := range h.findCalls("Setenv") {
			checkGinkgoGlobalStateCleanup(h.ctx, call)
		}
		for _, call := range h.findCalls("SetDefault") {
			checkGinkgoGlobalStateCleanup(h.ctx, call)
		}
		checkGinkgoGlobalStateCleanup(h.ctx, h.findCall("SetDefaultEventuallyTimeout"))
		checkGinkgoGlobalStateCleanup(h.ctx, h.findCall("Unsetenv"))
		checkGinkgoHelperFirst(h.ctx, h.findFunc("assertLater"))

		Expect(observeHelperDecisions(
			insideSelectStmt(h.ctx, unaryExpressionsWithOperator(h.file, token.ARROW)[1]),
			insideGoroutineFuncLit(h.ctx, unaryExpressionsWithOperator(h.file, token.ARROW)[2]),
			firstStatementIsGinkgoHelper(h.findFunc("assertLater").Body),
			funcHasGinkgoHelperCall(h.findFunc("assertLater").Body),
			ginkgoGlobalStateMutationAssignmentSucceeded(ast.NewIdent("notSelector")),
			ginkgoGlobalStateMutationAssignmentSucceeded(&ast.SelectorExpr{X: ast.NewIdent("format"), Sel: ast.NewIdent("MaxLength")}),
			ginkgoGlobalStateMutationAssignmentSucceeded(&ast.SelectorExpr{X: ast.NewIdent("other"), Sel: ast.NewIdent("MaxLength")}),
			isGomegaFormatField("MaxDepth"),
			isGomegaFormatField("Other"),
		)).To(Equal([]helperDecision{
			helperAccepted,
			helperAccepted,
			helperRejected,
			helperAccepted,
			helperRejected,
			helperAccepted,
			helperRejected,
			helperAccepted,
			helperRejected,
		}))
		Expect(h.diagnosticMessages()).To(ConsistOf(
			"semh:ginkgo.goroutine-recover: goroutine with assertions must defer GinkgoRecover() or use GinkgoHelperGo",
			"semh:ginkgo.blocking-receive: avoid blocking channel receives in specs; use Eventually(...).Should(Receive(...)) so failures surface",
			"semh:ginkgo.global-state-cleanup: restore global state changes with DeferCleanup next to os.Setenv",
			"semh:ginkgo.global-state-cleanup: restore global state changes with DeferCleanup next to slog.SetDefault",
			"semh:ginkgo.global-state-cleanup: restore global state changes with DeferCleanup next to SetDefaultEventuallyTimeout",
			"semh:ginkgo.helper-first: call GinkgoHelper() as the first statement in assertion helper assertLater",
		))
	})

	ginkgo.It("reports only reusable assertion helpers that should become matchers", func() {
		h := newRuleHarness("/repo/internal/wiki/helper_test.go", "github.com/perber/wiki/internal/wiki", `package wiki

type assertion struct{}
type GomegaMatcher interface{}

func Expect(actual any) assertion { return assertion{} }
func (assertion) To(matcher any, extra ...any) {}
func Equal(expected any) GomegaMatcher { return nil }
func GinkgoHelper() {}

func assertReusable(got string, want string) {
	GinkgoHelper()
	Expect(got).To(Equal(want))
}

func assertSingleUse(got string, want string) {
	GinkgoHelper()
	Expect(got).To(Equal(want))
}

func assertWithoutReporting(got string, want string) {
	Expect(got).To(Equal(want))
}

func assertMatcher(want string) GomegaMatcher {
	GinkgoHelper()
	return Equal(want)
}

func expectPlain(got string, want string) {
	GinkgoHelper()
	_ = got == want
}

func TestReusableHelpers() {
	assertReusable("one", "one")
	assertReusable("two", "two")
	assertSingleUse("one", "one")
	assertWithoutReporting("one", "one")
	assertWithoutReporting("two", "two")
	_ = assertMatcher("one")
	_ = assertMatcher("two")
	expectPlain("one", "one")
	expectPlain("two", "two")
}
`)
		for _, name := range []string{"assertReusable", "assertSingleUse", "assertWithoutReporting", "assertMatcher", "expectPlain"} {
			checkReusableAssertionHelper(h.ctx, h.findFunc(name))
		}

		Expect(h.diagnosticMessages()).To(ConsistOf(
			"semh:gomega.helper-should-be-matcher: prefer a custom Gomega matcher for reusable assertion helper assertReusable",
		))
	})
})

func returnExpressionsByFunction(file *ast.File) map[string]ast.Expr {
	returns := map[string]ast.Expr{}
	ast.Inspect(file, func(node ast.Node) bool {
		fn, ok := node.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			return true
		}
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			ret, ok := node.(*ast.ReturnStmt)
			if !ok || len(ret.Results) == 0 {
				return true
			}
			returns[fn.Name.Name] = ret.Results[0]
			return false
		})
		return true
	})
	return returns
}

func callExpressionsByFunction(file *ast.File, targetCallName string) map[string]*ast.CallExpr {
	calls := map[string]*ast.CallExpr{}
	ast.Inspect(file, func(node ast.Node) bool {
		fn, ok := node.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			return true
		}
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok || callName(call) != targetCallName {
				return true
			}
			calls[fn.Name.Name] = call
			return false
		})
		return true
	})
	return calls
}

func goStatementsInFile(file *ast.File) []*ast.GoStmt {
	var statements []*ast.GoStmt
	ast.Inspect(file, func(node ast.Node) bool {
		stmt, ok := node.(*ast.GoStmt)
		if ok {
			statements = append(statements, stmt)
		}
		return true
	})
	return statements
}

func unaryExpressionsWithOperator(file *ast.File, op token.Token) []*ast.UnaryExpr {
	var expressions []*ast.UnaryExpr
	ast.Inspect(file, func(node ast.Node) bool {
		expr, ok := node.(*ast.UnaryExpr)
		if ok && expr.Op == op {
			expressions = append(expressions, expr)
		}
		return true
	})
	return expressions
}

func ginkgoGlobalStateMutationAssignmentSucceeded(expr ast.Expr) bool {
	_, ok := ginkgoGlobalStateMutationAssignment(expr)
	return ok
}
