package testhygiene

import (
	"go/ast"
	"go/types"
	"strings"
)

func gomegaAssertionFromCall(ctx *analysisContext, call *ast.CallExpr) (gomegaAssertion, bool) {
	method := callName(call)
	if !isGomegaAssertionMethod(method) || len(call.Args) == 0 {
		return gomegaAssertion{}, false
	}
	matcher, ok := call.Args[0].(*ast.CallExpr)
	if !ok {
		return gomegaAssertion{}, false
	}
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return gomegaAssertion{}, false
	}
	source, ok := gomegaSourceCall(selector.X)
	if !ok {
		return gomegaAssertion{}, false
	}
	actual, ok := gomegaExpectationActual(source)
	if !ok {
		return gomegaAssertion{}, false
	}
	return gomegaAssertion{
		call:    call,
		method:  method,
		source:  source,
		actual:  actual,
		matcher: matcher,
	}, true
}

type gomegaAsyncAssertion struct {
	call       *ast.CallExpr
	method     string
	source     *ast.CallExpr
	sourceName string
	actual     ast.Expr
	matcher    *ast.CallExpr
}

func gomegaAsyncAssertionFromCall(call *ast.CallExpr) (gomegaAsyncAssertion, bool) {
	method := callName(call)
	if !isGomegaAssertionMethod(method) || len(call.Args) == 0 {
		return gomegaAsyncAssertion{}, false
	}
	matcher, ok := call.Args[0].(*ast.CallExpr)
	if !ok {
		return gomegaAsyncAssertion{}, false
	}
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return gomegaAsyncAssertion{}, false
	}
	source, ok := gomegaAsyncSourceCall(selector.X)
	if !ok {
		return gomegaAsyncAssertion{}, false
	}
	actual, ok := gomegaAsyncActual(source)
	if !ok {
		return gomegaAsyncAssertion{}, false
	}
	return gomegaAsyncAssertion{
		call:       call,
		method:     method,
		source:     source,
		sourceName: callName(source),
		actual:     actual,
		matcher:    matcher,
	}, true
}

func gomegaSourceCall(expr ast.Expr) (*ast.CallExpr, bool) {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return nil, false
	}
	if isGomegaExpectationSource(callName(call)) {
		return call, true
	}
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return nil, false
	}
	return gomegaSourceCall(selector.X)
}

func gomegaAsyncSourceCall(expr ast.Expr) (*ast.CallExpr, bool) {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return nil, false
	}
	if isGomegaAsyncCall(callName(call)) {
		return call, true
	}
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return nil, false
	}
	return gomegaAsyncSourceCall(selector.X)
}

func gomegaExpectationActual(call *ast.CallExpr) (ast.Expr, bool) {
	switch callName(call) {
	case "Expect", "Ω":
		if len(call.Args) == 0 {
			return nil, false
		}
		return call.Args[0], true
	case "ExpectWithOffset":
		if len(call.Args) < 2 {
			return nil, false
		}
		return call.Args[1], true
	default:
		return nil, false
	}
}

func gomegaAsyncActual(call *ast.CallExpr) (ast.Expr, bool) {
	switch callName(call) {
	case "Eventually", "Consistently":
		if len(call.Args) == 0 {
			return nil, false
		}
		return call.Args[0], true
	case "EventuallyWithOffset", "ConsistentlyWithOffset":
		if len(call.Args) < 2 {
			return nil, false
		}
		return call.Args[1], true
	default:
		return nil, false
	}
}

func isGomegaExpectationSource(name string) bool {
	switch name {
	case "Expect", "ExpectWithOffset", "Ω":
		return true
	default:
		return false
	}
}

func isGomegaAsyncCall(name string) bool {
	switch name {
	case "Eventually", "EventuallyWithOffset", "Consistently", "ConsistentlyWithOffset":
		return true
	default:
		return false
	}
}

func isNegativeAssertionMethod(name string) bool {
	switch name {
	case "NotTo", "ToNot", "ShouldNot":
		return true
	default:
		return false
	}
}

func assertionUsesErrError(ctx *analysisContext, assertion gomegaAssertion) bool {
	return exprIsErrorStringCall(ctx, assertion.actual)
}

func matcherTreeUsesErrError(ctx *analysisContext, expr ast.Expr) bool {
	found := false
	ast.Inspect(expr, func(node ast.Node) bool {
		if found || node == nil {
			return false
		}
		call, ok := node.(*ast.CallExpr)
		if ok && exprIsErrorStringCall(ctx, call) {
			found = true
			return false
		}
		return true
	})
	return found
}

func checkErrorStringPredicate(ctx *analysisContext, call *ast.CallExpr) {
	if !isTestFile(ctx.filename(call.Pos())) {
		return
	}
	packagePath, name := calleePackageAndName(ctx, call)
	if packagePath != "strings" || name != "Contains" || len(call.Args) == 0 {
		return
	}
	if exprIsErrorStringCall(ctx, call.Args[0]) {
		ctx.report(ruleGomegaErrorString, call.Args[0], gomegaErrorStringMatcherDiagnostic())
		return
	}
	if exprSuggestsTestRenderedProseContract(call.Args[0]) {
		ctx.report(ruleGomegaStringsContains, call, gomegaRenderedMessageStringsContainsDiagnostic())
	}
}

func exprIsErrorStringCall(ctx *analysisContext, expr ast.Expr) bool {
	expr = unparenExpr(expr)
	call, ok := expr.(*ast.CallExpr)
	if !ok || callName(call) != "Error" {
		return false
	}
	selector, ok := call.Fun.(*ast.SelectorExpr)
	return ok && typeImplementsError(ctx.pass.TypesInfo.TypeOf(selector.X))
}

func typeImplementsError(typ types.Type) bool {
	if typ == nil {
		return false
	}
	errorType, ok := types.Universe.Lookup("error").Type().Underlying().(*types.Interface)
	if !ok {
		return false
	}
	return types.Implements(typ, errorType)
}

func assertionUsesErrorNilMatcher(ctx *analysisContext, assertion gomegaAssertion) bool {
	if !assertionTargetsError(ctx, assertion) {
		return false
	}
	return isMatcherNamed(assertion.matcher, "BeNil") || isEqualNilMatcher(assertion.matcher)
}

func assertionTargetsError(ctx *analysisContext, assertion gomegaAssertion) bool {
	return gomegaAssertionHasErrorProjection(assertion) || typeImplementsError(ctx.pass.TypesInfo.TypeOf(assertion.actual))
}

func gomegaAssertionHasErrorProjection(assertion gomegaAssertion) bool {
	for current := assertion.call; current != nil; {
		selector, ok := current.Fun.(*ast.SelectorExpr)
		if !ok {
			return false
		}
		source, ok := selector.X.(*ast.CallExpr)
		if !ok {
			return false
		}
		if callName(source) == "Error" {
			return true
		}
		if source == assertion.source {
			return false
		}
		current = source
	}
	return false
}

func isEqualNilMatcher(matcher *ast.CallExpr) bool {
	return isMatcherNamed(matcher, "Equal") && len(matcher.Args) == 1 && isNilExpr(matcher.Args[0])
}

func isNilExpr(expr ast.Expr) bool {
	ident, ok := expr.(*ast.Ident)
	return ok && ident.Name == "nil"
}

func assertionUsesInlineErrorReturnHaveOccurred(ctx *analysisContext, assertion gomegaAssertion) bool {
	if !isMatcherNamed(assertion.matcher, "HaveOccurred") ||
		!isNegativeAssertionMethod(assertion.method) ||
		gomegaAssertionHasErrorProjection(assertion) {
		return false
	}
	call, ok := assertion.actual.(*ast.CallExpr)
	return ok && callReturnsSingleError(ctx, call)
}

func assertionUsesGenericHaveOccurred(ctx *analysisContext, assertion gomegaAssertion) bool {
	if !isMatcherNamed(assertion.matcher, "HaveOccurred") || isNegativeAssertionMethod(assertion.method) {
		return false
	}
	return assertionTargetsError(ctx, assertion)
}

func assertionMatcherTreeUsesGenericHaveOccurred(assertion gomegaAssertion) bool {
	if isNegativeAssertionMethod(assertion.method) || isMatcherNamed(assertion.matcher, "HaveOccurred") {
		return false
	}
	return matcherTreeContainsPositiveHaveOccurred(assertion.matcher)
}

func assertionMatcherTreeUsesGenericErrorPresence(assertion gomegaAssertion) bool {
	if isNegativeAssertionMethod(assertion.method) {
		return false
	}
	return matcherTreeContainsGenericErrorPresence(assertion.matcher)
}

func matcherTreeContainsPositiveHaveOccurred(expr ast.Expr) bool {
	switch current := unparenExpr(expr).(type) {
	case *ast.CallExpr:
		if isMatcherNamed(current, "Not") {
			return false
		}
		if isMatcherNamed(current, "HaveOccurred") {
			return true
		}
		for _, arg := range current.Args {
			if matcherTreeContainsPositiveHaveOccurred(arg) {
				return true
			}
		}
	case *ast.CompositeLit:
		for _, elt := range current.Elts {
			if matcherTreeContainsPositiveHaveOccurred(elt) {
				return true
			}
		}
	case *ast.KeyValueExpr:
		return matcherTreeContainsPositiveHaveOccurred(current.Value)
	case *ast.TypeAssertExpr:
		return matcherTreeContainsPositiveHaveOccurred(current.X)
	}
	return false
}

func matcherTreeContainsGenericErrorPresence(expr ast.Expr) bool {
	switch current := unparenExpr(expr).(type) {
	case *ast.CallExpr:
		if matcherCallContainsGenericErrorPresence(current) {
			return true
		}
		for _, arg := range current.Args {
			if matcherTreeContainsGenericErrorPresence(arg) {
				return true
			}
		}
	case *ast.CompositeLit:
		for _, elt := range current.Elts {
			if matcherTreeContainsGenericErrorPresence(elt) {
				return true
			}
		}
	case *ast.KeyValueExpr:
		if errorPresenceMatcherField(current.Key) && matcherIsGenericErrorPresence(current.Value) {
			return true
		}
		return matcherTreeContainsGenericErrorPresence(current.Value)
	case *ast.TypeAssertExpr:
		return matcherTreeContainsGenericErrorPresence(current.X)
	}
	return false
}

func matcherCallContainsGenericErrorPresence(call *ast.CallExpr) bool {
	if !isMatcherNamed(call, "HaveField") || len(call.Args) < 2 {
		return false
	}
	return errorPresenceMatcherField(call.Args[0]) && matcherIsGenericErrorPresence(call.Args[1])
}

func errorPresenceMatcherField(expr ast.Expr) bool {
	name := canonicalName(keyName(expr))
	return name == "err" ||
		name == "error" ||
		strings.HasSuffix(name, "err") ||
		strings.HasSuffix(name, "error") ||
		strings.HasSuffix(name, "errors")
}

func matcherIsGenericErrorPresence(expr ast.Expr) bool {
	call, ok := unparenExpr(expr).(*ast.CallExpr)
	if !ok || !isMatcherNamed(call, "Not") || len(call.Args) != 1 {
		return false
	}
	inner, ok := unparenExpr(call.Args[0]).(*ast.CallExpr)
	return ok && (isMatcherNamed(inner, "BeNil") || isEqualNilMatcher(inner))
}

func assertionUsesMultiReturnErrorMatcher(ctx *analysisContext, assertion gomegaAssertion) bool {
	if !isMatcherNamed(assertion.matcher, "HaveOccurred", "Succeed") || gomegaAssertionHasErrorProjection(assertion) {
		return false
	}
	call, ok := assertion.actual.(*ast.CallExpr)
	return ok && callReturnsMultipleWithError(ctx, call)
}

func callReturnsSingleError(ctx *analysisContext, call *ast.CallExpr) bool {
	results := callResultTuple(ctx, call)
	return results != nil && results.Len() == 1 && typeImplementsError(results.At(0).Type())
}

func callReturnsMultipleWithError(ctx *analysisContext, call *ast.CallExpr) bool {
	results := callResultTuple(ctx, call)
	if results == nil || results.Len() < 2 {
		return false
	}
	for i := 0; i < results.Len(); i++ {
		if typeImplementsError(results.At(i).Type()) {
			return true
		}
	}
	return false
}

func callResultTuple(ctx *analysisContext, call *ast.CallExpr) *types.Tuple {
	if sig, ok := ctx.pass.TypesInfo.TypeOf(call.Fun).(*types.Signature); ok {
		return sig.Results()
	}
	if tuple, ok := ctx.pass.TypesInfo.TypeOf(call).(*types.Tuple); ok {
		return tuple
	}
	return nil
}
