package semantichygiene

import (
	"go/ast"
	"go/token"
	"strconv"
	"strings"
)

func isGomegaAnnotationCall(name string) bool {
	switch name {
	case "To", "NotTo", "ToNot", "Should", "ShouldNot", "Error":
		return true
	default:
		return false
	}
}

func isTestAssertionMatcherCall(name string) bool {
	switch name {
	case "Equal", "ContainSubstring", "HavePrefix", "HaveSuffix", "HaveKeyWithValue", "HaveField", "MatchError", "PanicWith":
		return true
	default:
		return false
	}
}

func isTestAssertionMatcherContractContext(ctx *analysisContext, matcher *ast.CallExpr) bool {
	if !isTestAssertionMatcherCall(callName(matcher)) {
		return false
	}
	if matcherCallHasStructuredProtocolKey(matcher) {
		return true
	}
	for current := ctx.parent(matcher); current != nil; current = ctx.parent(current) {
		call, ok := current.(*ast.CallExpr)
		if !ok {
			if _, ok := current.(*ast.FuncDecl); ok {
				return false
			}
			continue
		}
		if !isGomegaAssertionMethod(callName(call)) {
			continue
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return false
		}
		expectCall, ok := selector.X.(*ast.CallExpr)
		if !ok || !isGomegaExpectCall(callName(expectCall)) || len(expectCall.Args) == 0 {
			return false
		}
		return exprSuggestsTestContract(expectCall.Args[0])
	}
	return false
}

func isTestAssertionMatcherLocalizedProseContext(ctx *analysisContext, matcher *ast.CallExpr) bool {
	if !isTestAssertionMatcherCall(callName(matcher)) {
		return false
	}
	if matcherCallHasRenderedProseProtocolKey(matcher) {
		return true
	}
	for current := ctx.parent(matcher); current != nil; current = ctx.parent(current) {
		call, ok := current.(*ast.CallExpr)
		if !ok {
			if _, ok := current.(*ast.FuncDecl); ok {
				return false
			}
			continue
		}
		if !isGomegaAssertionMethod(callName(call)) {
			continue
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return false
		}
		expectCall, ok := selector.X.(*ast.CallExpr)
		if !ok || !isGomegaExpectCall(callName(expectCall)) || len(expectCall.Args) == 0 {
			return false
		}
		return exprSuggestsTestRenderedProseContract(expectCall.Args[0])
	}
	return false
}

func matcherCallHasStructuredProtocolKey(call *ast.CallExpr) bool {
	if !isMatcherNamed(call, "HaveKeyWithValue") || len(call.Args) == 0 {
		return false
	}
	key, ok := stringArgValue(call.Args[0])
	return ok && structuredProtocolKeyName(key)
}

func matcherCallHasRenderedProseProtocolKey(call *ast.CallExpr) bool {
	if !isMatcherNamed(call, "HaveKeyWithValue") || len(call.Args) == 0 {
		return false
	}
	key, ok := stringArgValue(call.Args[0])
	if !ok {
		return false
	}
	switch canonicalName(key) {
	case "error", "message":
		return true
	default:
		return false
	}
}

func stringArgValue(expr ast.Expr) (string, bool) {
	lit, ok := unparenExpr(expr).(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	value, err := strconv.Unquote(lit.Value)
	if err != nil {
		return "", false
	}
	return value, true
}

func isGomegaAssertionMethod(name string) bool {
	switch name {
	case "To", "NotTo", "ToNot", "Should", "ShouldNot", "Error":
		return true
	default:
		return false
	}
}

func isGomegaExpectCall(name string) bool {
	switch name {
	case "Expect", "ExpectWithOffset", "Ω":
		return true
	default:
		return false
	}
}

func exprSuggestsTestContract(expr ast.Expr) bool {
	found := false
	ast.Inspect(expr, func(node ast.Node) bool {
		if found || node == nil {
			return false
		}
		switch n := node.(type) {
		case *ast.Ident:
			found = nameSuggestsTestSubjectContract(n.Name)
		case *ast.SelectorExpr:
			found = nameSuggestsTestSubjectContract(n.Sel.Name)
		case *ast.CallExpr:
			found = nameSuggestsTestSubjectContract(callName(n))
		}
		return !found
	})
	return found
}

func exprSuggestsTestRenderedProseContract(expr ast.Expr) bool {
	found := false
	ast.Inspect(expr, func(node ast.Node) bool {
		if found || node == nil {
			return false
		}
		switch n := node.(type) {
		case *ast.Ident:
			found = nameSuggestsTestRenderedProseSubject(n.Name)
		case *ast.SelectorExpr:
			found = nameSuggestsTestRenderedProseSubject(n.Sel.Name)
		case *ast.CallExpr:
			found = nameSuggestsTestRenderedProseSubject(callName(n))
		}
		return !found
	})
	return found
}

func nameSuggestsTestSubjectContract(name string) bool {
	canonical := canonicalName(name)
	if strings.Contains(canonical, "diagnostic") {
		return false
	}
	return canonical == "body" ||
		strings.Contains(canonical, "code") ||
		strings.Contains(canonical, "error") ||
		strings.Contains(canonical, "message") ||
		strings.Contains(canonical, "messageid") ||
		strings.Contains(canonical, "toolid") ||
		strings.Contains(canonical, "issue") ||
		strings.Contains(canonical, "validation")
}

func nameSuggestsTestRenderedProseSubject(name string) bool {
	canonical := canonicalName(name)
	if strings.Contains(canonical, "diagnostic") {
		return false
	}
	if identifierHasWord(name, "err") {
		return true
	}
	return canonical == "err" ||
		canonical == "error" ||
		canonical == "logs" ||
		canonical == "message" ||
		canonical == "output" ||
		identifierHasWord(name, "log") ||
		identifierHasWord(name, "logs") ||
		identifierHasWord(name, "panic") ||
		identifierHasWord(name, "fatal") ||
		strings.Contains(canonical, "error") ||
		strings.Contains(canonical, "message") ||
		strings.Contains(canonical, "output") ||
		strings.Contains(canonical, "stderr") ||
		strings.Contains(canonical, "stdout")
}

func isTestContractAssertionCall(ctx *analysisContext, call *ast.CallExpr) bool {
	name := callName(call)
	if !strings.HasPrefix(name, "assert") && !strings.HasPrefix(name, "expect") {
		return false
	}
	return nameSuggestsTestContract(name)
}

func isTestSemanticAssertionHelper(name string) bool {
	if !strings.HasPrefix(name, "assert") && !strings.HasPrefix(name, "expect") {
		return false
	}
	return nameSuggestsTestContract(name)
}

func nameSuggestsTestContract(name string) bool {
	canonical := canonicalName(name)
	return strings.Contains(canonical, "structured") ||
		strings.Contains(canonical, "localized") ||
		strings.Contains(canonical, "error") ||
		strings.Contains(canonical, "message") ||
		strings.Contains(canonical, "code") ||
		strings.Contains(canonical, "validation") ||
		strings.Contains(canonical, "issue") ||
		strings.Contains(canonical, "tool")
}

func semanticTypeForTestHelperParamName(paramName string, funcName string) (string, bool) {
	if nameIsFilename(paramName) && !nameSuggestsAssetNameContext(funcName) {
		return "", false
	}
	if typ, ok := semanticTypeForParamName(paramName, funcName); ok {
		return typ, true
	}
	canonicalParam := canonicalName(paramName)
	canonicalFunc := canonicalName(funcName)
	switch {
	case strings.Contains(canonicalParam, "messageid"):
		return "MessageID", true
	case strings.Contains(canonicalParam, "toolid") || strings.Contains(canonicalParam, "toolname"):
		return "ToolID", true
	case strings.Contains(canonicalParam, "code"):
		switch {
		case strings.Contains(canonicalFunc, "field"):
			return "FieldErrorCode", true
		case strings.Contains(canonicalFunc, "issue") || strings.Contains(canonicalFunc, "validationissue"):
			return "IssueCode", true
		default:
			return "ErrorCode", true
		}
	default:
		return "", false
	}
}

func nameSuggestsAssetNameContext(name string) bool {
	return strings.Contains(canonicalName(name), "asset")
}

func nameIsFilename(name string) bool {
	switch canonicalName(name) {
	case "filename", "filenames":
		return true
	default:
		return false
	}
}

func testHelperMessageParamName(paramName string, funcName string) bool {
	canonicalParam := canonicalName(paramName)
	canonicalFunc := canonicalName(funcName)
	if canonicalParam == "message" {
		return strings.Contains(canonicalFunc, "structured") ||
			strings.Contains(canonicalFunc, "localized") ||
			strings.Contains(canonicalFunc, "error")
	}
	if !isRenderedOutputParamName(canonicalParam) {
		return false
	}
	return strings.Contains(canonicalFunc, "structured") ||
		strings.Contains(canonicalFunc, "localized") ||
		strings.Contains(canonicalFunc, "error") ||
		strings.Contains(canonicalFunc, "fatal") ||
		strings.Contains(canonicalFunc, "message") ||
		strings.Contains(canonicalFunc, "output") ||
		strings.Contains(canonicalFunc, "stderr") ||
		strings.Contains(canonicalFunc, "stdout")
}

func isRenderedOutputParamName(canonicalParam string) bool {
	switch canonicalParam {
	case "context", "fragment", "prefix", "suffix", "text", "output", "stderr", "stdout", "cause", "reason":
		return true
	default:
		return false
	}
}

func testHelperFieldParamName(paramName string, funcName string) bool {
	if canonicalName(paramName) != "field" {
		return false
	}
	canonicalFunc := canonicalName(funcName)
	return strings.Contains(canonicalFunc, "field") ||
		strings.Contains(canonicalFunc, "validation") ||
		strings.Contains(canonicalFunc, "error")
}

func testTableParamSuggestsContract(paramName string) bool {
	if _, ok := semanticTypeForTestHelperParamName(paramName, ""); ok {
		return true
	}
	canonical := canonicalName(paramName)
	return strings.Contains(canonical, "error") ||
		strings.Contains(canonical, "message")
}

func testTableParamSuggestsRenderedProseContract(paramName string) bool {
	canonical := canonicalName(paramName)
	if strings.Contains(canonical, "diagnostic") {
		return false
	}
	return strings.Contains(canonical, "error") ||
		strings.Contains(canonical, "message") ||
		strings.Contains(canonical, "stderr") ||
		strings.Contains(canonical, "stdout")
}
