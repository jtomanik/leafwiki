package semantichygiene

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"
	"unicode"
)

func assertionUsesStringsContains(ctx *analysisContext, assertion gomegaAssertion) bool {
	return assertionUsesStringsPredicate(ctx, assertion, "Contains")
}

func assertionUsesRenderedMessageStringMatcher(ctx *analysisContext, assertion gomegaAssertion) bool {
	if assertionUsesErrError(ctx, assertion) || matcherTreeUsesErrError(ctx, assertion.matcher) || assertionTargetsLastError(assertion.actual) {
		return false
	}
	if !isStringType(ctx.pass, assertion.actual) {
		return false
	}
	if selector, ok := unparenExpr(assertion.actual).(*ast.SelectorExpr); ok &&
		structuredErrorFieldName(selector.Sel.Name) &&
		exprSuggestsStructuredErrorValue(ctx, selector.X) {
		return false
	}
	if !exprSuggestsTestRenderedProseContract(assertion.actual) {
		return false
	}
	return matcherTreeContainsStringContentMatcher(ctx, assertion.matcher)
}

func matcherTreeContainsStringContentMatcher(ctx *analysisContext, expr ast.Expr) bool {
	switch current := unparenExpr(expr).(type) {
	case *ast.CallExpr:
		if isMatcherNamed(current, "Equal", "ContainSubstring", "HavePrefix", "HaveSuffix", "MatchRegexp") {
			return callHasStringArg(ctx, current)
		}
		for _, arg := range current.Args {
			if matcherTreeContainsStringContentMatcher(ctx, arg) {
				return true
			}
		}
	case *ast.CompositeLit:
		for _, elt := range current.Elts {
			if matcherTreeContainsStringContentMatcher(ctx, elt) {
				return true
			}
		}
	case *ast.KeyValueExpr:
		return matcherTreeContainsStringContentMatcher(ctx, current.Value)
	case *ast.TypeAssertExpr:
		return matcherTreeContainsStringContentMatcher(ctx, current.X)
	}
	return false
}

func assertionUsesLastErrorNotEmpty(assertion gomegaAssertion) bool {
	if !assertionTargetsLastError(assertion.actual) {
		return false
	}
	if isNegativeAssertionMethod(assertion.method) {
		return isEmptyMatcher(assertion.matcher)
	}
	return isNegatedEmptyMatcher(assertion.matcher)
}

func assertionTargetsLastError(expr ast.Expr) bool {
	selector, ok := unparenExpr(expr).(*ast.SelectorExpr)
	return ok && selector.Sel.Name == "LastError"
}

func isEmptyMatcher(matcher *ast.CallExpr) bool {
	return isMatcherNamed(matcher, "BeEmpty") || isEqualEmptyStringMatcher(matcher)
}

func isNegatedEmptyMatcher(matcher *ast.CallExpr) bool {
	return isMatcherNamed(matcher, "Not") && len(matcher.Args) == 1 && exprIsEmptyMatcher(matcher.Args[0])
}

func assertionUsesNonEmptyCollection(ctx *analysisContext, assertion gomegaAssertion) bool {
	if !assertionActualIsCollection(ctx, assertion.actual) {
		return false
	}
	if isNegativeAssertionMethod(assertion.method) {
		return isEmptyMatcher(assertion.matcher)
	}
	return isNegatedEmptyMatcher(assertion.matcher)
}

func assertionUsesSemanticScalarNotEmpty(ctx *analysisContext, assertion gomegaAssertion) bool {
	if assertionActualIsCollection(ctx, assertion.actual) {
		return false
	}
	if !assertionActualIsSemanticScalar(assertion.actual) {
		return false
	}
	if isNegativeAssertionMethod(assertion.method) {
		return isEmptyMatcher(assertion.matcher)
	}
	return isNegatedEmptyMatcher(assertion.matcher)
}

func assertionActualIsSemanticScalar(expr ast.Expr) bool {
	names := semanticScalarExprNames(expr)
	if len(names) == 0 {
		return false
	}
	hasSelectorContext := len(names) > 1
	for i, name := range names {
		context := semanticScalarContext(names, i)
		if typ, ok := semanticTypeForFieldName(name, context); ok && typ != "" {
			return true
		}
		if canonicalName(name) == "id" {
			if typ, ok := semanticTypeForBareIDContext(context); ok && typ != "" {
				return true
			}
		}
		if canonicalName(name) == "hash" {
			if typ, ok := semanticTypeForBareHashContext(context); ok && typ != "" {
				return true
			}
		}
		if hasSelectorContext && semanticScalarTokenName(name) {
			return true
		}
		if semanticName(name) {
			return true
		}
	}
	return false
}

func semanticScalarExprNames(expr ast.Expr) []string {
	switch e := unparenExpr(expr).(type) {
	case *ast.Ident:
		return []string{e.Name}
	case *ast.SelectorExpr:
		names := semanticScalarExprNames(e.X)
		return append(names, e.Sel.Name)
	default:
		return nil
	}
}

func semanticScalarContext(names []string, index int) string {
	if index <= 0 || index > len(names) {
		return ""
	}
	return strings.Join(names[:index], "")
}

func semanticScalarTokenName(name string) bool {
	canonical := canonicalName(name)
	return canonical != "token" && strings.HasSuffix(canonical, "token")
}

func assertionActualIsCollection(ctx *analysisContext, expr ast.Expr) bool {
	typ := ctx.pass.TypesInfo.TypeOf(expr)
	return typeIsSliceOrArray(typ) || typeIsMap(typ)
}

func exprIsEmptyMatcher(expr ast.Expr) bool {
	call, ok := unparenExpr(expr).(*ast.CallExpr)
	return ok && isEmptyMatcher(call)
}

func isEqualEmptyStringMatcher(matcher *ast.CallExpr) bool {
	if !isMatcherNamed(matcher, "Equal") || len(matcher.Args) != 1 {
		return false
	}
	lit, ok := unparenExpr(matcher.Args[0]).(*ast.BasicLit)
	return ok && lit.Kind == token.STRING && lit.Value == `""`
}

func assertionUsesStringsPredicate(ctx *analysisContext, assertion gomegaAssertion, predicate string) bool {
	call, ok := assertion.actual.(*ast.CallExpr)
	if !ok {
		return false
	}
	packagePath, name := calleePackageAndName(ctx, call)
	return packagePath == "strings" && name == predicate
}

func assertionUsesRegexpMatchString(ctx *analysisContext, assertion gomegaAssertion) bool {
	call, ok := assertion.actual.(*ast.CallExpr)
	if !ok {
		return false
	}
	packagePath, name := calleePackageAndName(ctx, call)
	return packagePath == "regexp" && name == "MatchString"
}

func assertionUsesErrorsIs(ctx *analysisContext, assertion gomegaAssertion) bool {
	call, ok := assertion.actual.(*ast.CallExpr)
	if !ok {
		return false
	}
	packagePath, name := calleePackageAndName(ctx, call)
	return packagePath == "errors" && name == "Is"
}

func assertionUsesErrorsAs(ctx *analysisContext, assertion gomegaAssertion) bool {
	call, ok := assertion.actual.(*ast.CallExpr)
	if !ok {
		return false
	}
	packagePath, name := calleePackageAndName(ctx, call)
	return packagePath == "errors" && name == "As"
}

func assertionUsesOSIsNotExist(ctx *analysisContext, assertion gomegaAssertion) bool {
	call, ok := assertion.actual.(*ast.CallExpr)
	if !ok {
		return false
	}
	packagePath, name := calleePackageAndName(ctx, call)
	return packagePath == "os" && name == "IsNotExist"
}

func matcherUsesOSIsNotExistTransform(ctx *analysisContext, matcher *ast.CallExpr) bool {
	if !isMatcherNamed(matcher, "WithTransform") || len(matcher.Args) < 2 || !exprIsBooleanMatcher(matcher.Args[1]) {
		return false
	}
	packagePath, name := referencedFunctionPackageAndName(ctx, matcher.Args[0])
	return packagePath == "os" && name == "IsNotExist"
}

func referencedFunctionPackageAndName(ctx *analysisContext, expr ast.Expr) (string, string) {
	switch fun := unparenExpr(expr).(type) {
	case *ast.Ident:
		if fn, ok := ctx.pass.TypesInfo.Uses[fun].(*types.Func); ok && fn.Pkg() != nil {
			return fn.Pkg().Path(), fun.Name
		}
		return "", fun.Name
	case *ast.SelectorExpr:
		if fn, ok := ctx.pass.TypesInfo.Uses[fun.Sel].(*types.Func); ok && fn.Pkg() != nil {
			return fn.Pkg().Path(), fun.Sel.Name
		}
		return "", fun.Sel.Name
	default:
		return "", ""
	}
}

func assertionUsesControlStatus(assertion gomegaAssertion) bool {
	call, ok := assertion.actual.(*ast.CallExpr)
	return ok && callName(call) == "IsControlStatus"
}

func assertionUsesRawStatusCode(assertion gomegaAssertion) bool {
	if !exprSuggestsRawStatusCode(assertion.actual) {
		return false
	}
	if !isMatcherNamed(assertion.matcher, "Equal", "BeEquivalentTo", "BeNumerically") {
		return false
	}
	for _, arg := range assertion.matcher.Args {
		if numericLiteralArg(arg) {
			return true
		}
	}
	return false
}

func exprSuggestsRawStatusCode(expr ast.Expr) bool {
	if call, ok := unparenExpr(expr).(*ast.CallExpr); ok && nameSuggestsRawStatusCode(callName(call)) {
		return true
	}
	return nameSuggestsRawStatusCode(exprName(expr))
}

func nameSuggestsRawStatusCode(name string) bool {
	name = canonicalName(name)
	switch name {
	case "code", "exitcode", "exitstatus", "status", "statuscode", "returncode":
		return true
	default:
		return strings.Contains(name, "exitcode") ||
			strings.Contains(name, "exitstatus") ||
			strings.Contains(name, "statuscode")
	}
}

func numericLiteralArg(expr ast.Expr) bool {
	lit, ok := unparenExpr(expr).(*ast.BasicLit)
	if !ok {
		return false
	}
	return lit.Kind == token.INT || lit.Kind == token.FLOAT
}

func assertionUsesLenEqual(assertion gomegaAssertion) bool {
	call, ok := assertion.actual.(*ast.CallExpr)
	return ok && callName(call) == "len" && isMatcherNamed(assertion.matcher, "Equal", "BeNumerically")
}

func assertionUsesBinaryBoolean(assertion gomegaAssertion) bool {
	binary, ok := assertion.actual.(*ast.BinaryExpr)
	return ok && isBooleanProducingBinaryOp(binary.Op) && isBooleanMatcher(assertion.matcher)
}

func assertionUsesBooleanLiteral(assertion gomegaAssertion) bool {
	ident, ok := unparenExpr(assertion.actual).(*ast.Ident)
	return ok && (ident.Name == "true" || ident.Name == "false") && isBooleanMatcher(assertion.matcher)
}

func assertionUsesCommaOKBoolean(ctx *analysisContext, assertion gomegaAssertion) bool {
	ident, ok := unparenExpr(assertion.actual).(*ast.Ident)
	if ok && isBooleanMatcher(assertion.matcher) && identIsCommaOKResult(ctx, ident) {
		return true
	}
	return compositeActualContainsIdent(assertion.actual, func(ident *ast.Ident) bool {
		return identIsCommaOKResult(ctx, ident)
	})
}

func assertionUsesProxyBoolean(ctx *analysisContext, assertion gomegaAssertion) bool {
	if assertionUsesDedicatedBooleanPredicate(ctx, assertion) {
		return false
	}
	ident, ok := unparenExpr(assertion.actual).(*ast.Ident)
	if ok && isBooleanMatcher(assertion.matcher) && (isProxyBooleanName(ident.Name) || identIsSemanticBooleanResult(ctx, ident)) {
		if identIsCommaOKResult(ctx, ident) {
			return false
		}
		return isBoolType(ctx.pass.TypesInfo.TypeOf(ident))
	}
	selector, ok := unparenExpr(assertion.actual).(*ast.SelectorExpr)
	if ok && isBooleanMatcher(assertion.matcher) && selectorIsProxyBoolean(ctx, selector) {
		return true
	}
	call, ok := unparenExpr(assertion.actual).(*ast.CallExpr)
	if ok && isBooleanMatcher(assertion.matcher) && callReturnsProxyBoolean(ctx, call) {
		return true
	}
	return compositeActualContainsIdent(assertion.actual, func(ident *ast.Ident) bool {
		return (isProxyBooleanName(ident.Name) || identIsSemanticBooleanResult(ctx, ident)) &&
			isBoolType(ctx.pass.TypesInfo.TypeOf(ident)) &&
			!identIsCommaOKResult(ctx, ident)
	})
}

func assertionUsesDedicatedBooleanPredicate(ctx *analysisContext, assertion gomegaAssertion) bool {
	return assertionUsesStringsContains(ctx, assertion) ||
		assertionUsesStringsPredicate(ctx, assertion, "HasPrefix") ||
		assertionUsesStringsPredicate(ctx, assertion, "HasSuffix") ||
		assertionUsesRegexpMatchString(ctx, assertion) ||
		assertionUsesErrorsIs(ctx, assertion) ||
		assertionUsesErrorsAs(ctx, assertion) ||
		assertionUsesControlStatus(assertion) ||
		assertionUsesOSIsNotExist(ctx, assertion)
}

func selectorIsProxyBoolean(ctx *analysisContext, selector *ast.SelectorExpr) bool {
	return isProxyBooleanName(selector.Sel.Name) &&
		isBoolType(ctx.pass.TypesInfo.TypeOf(selector))
}

func callReturnsProxyBoolean(ctx *analysisContext, call *ast.CallExpr) bool {
	if !isBoolType(ctx.pass.TypesInfo.TypeOf(call)) {
		return false
	}
	name := callName(call)
	if semanticBooleanCallName(name) {
		return true
	}
	if fn := calledFunctionObject(ctx, call); fn != nil {
		return semanticBooleanCallName(fn.Name())
	}
	return false
}

func semanticBooleanCallName(name string) bool {
	if isProxyBooleanName(name) {
		return true
	}
	canonical := canonicalName(name)
	for _, prefix := range []string{
		"is", "has", "can", "should", "supports", "allows", "contains", "get", "seen",
	} {
		if strings.HasPrefix(canonical, prefix) {
			return true
		}
	}
	for _, marker := range []string{
		"allowed", "heartbeat", "enabled", "disabled", "public", "running", "ready",
	} {
		if strings.Contains(canonical, marker) {
			return true
		}
	}
	return false
}

func isProxyBooleanName(name string) bool {
	switch strings.ToLower(name) {
	case "ok", "found", "exists", "present", "matched", "valid", "success", "done", "called",
		"changed", "created", "updated", "modified", "deleted", "removed", "rewritten", "applied",
		"accepted", "rejected", "renamed", "enabled", "disabled", "ready", "started", "stopped", "invoked",
		"running", "canceled", "cancelled", "healthy", "home", "parsed":
		return true
	}
	for _, suffix := range []string{
		"OK", "Ok", "Found", "Exists", "Present", "Matched", "Valid", "Success", "Done", "Called",
		"Changed", "Created", "Updated", "Modified", "Deleted", "Removed", "Rewritten", "Applied",
		"Accepted", "Rejected", "Renamed", "Enabled", "Disabled", "Ready", "Started", "Stopped", "Invoked",
		"Running", "Canceled", "Cancelled", "Healthy", "Home", "Parsed",
	} {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return identifierHasWord(name, "set")
}

func identifierHasWord(name string, want string) bool {
	for _, word := range identifierWords(name) {
		if word == want {
			return true
		}
	}
	return false
}

func identifierWords(name string) []string {
	var words []string
	var current []rune
	flush := func() {
		if len(current) == 0 {
			return
		}
		words = append(words, string(current))
		current = nil
	}
	runes := []rune(name)
	for i, r := range runes {
		if r == '_' || r == '-' {
			flush()
			continue
		}
		if len(current) > 0 && unicode.IsUpper(r) {
			previous := runes[i-1]
			nextIsLower := i+1 < len(runes) && unicode.IsLower(runes[i+1])
			if unicode.IsLower(previous) || unicode.IsDigit(previous) || unicode.IsUpper(previous) && nextIsLower {
				flush()
			}
		}
		current = append(current, unicode.ToLower(r))
	}
	flush()
	return words
}
