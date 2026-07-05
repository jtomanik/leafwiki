package testhygiene

import (
	"go/ast"
	"go/token"
	"go/types"
	"strconv"
	"strings"
)

func assertionUsesStructuredProtocolKeyMatcher(ctx *analysisContext, assertion gomegaAssertion) (string, bool) {
	if !exprSuggestsStructuredProtocolValue(ctx, assertion.actual) {
		return "", false
	}
	return matcherTreeContainsStructuredProtocolKeyMatcher(assertion.matcher)
}

func matcherTreeContainsStructuredProtocolKeyMatcher(expr ast.Expr) (string, bool) {
	call, ok := unparenExpr(expr).(*ast.CallExpr)
	if !ok {
		return "", false
	}
	if isMatcherNamed(call, "HaveKey", "HaveKeyWithValue") && len(call.Args) > 0 {
		if keyName, ok := stringLiteralValue(call.Args[0]); ok && structuredProtocolKeyName(keyName) {
			return keyName, true
		}
	}
	for _, arg := range call.Args {
		if keyName, ok := matcherTreeContainsStructuredProtocolKeyMatcher(arg); ok {
			return keyName, true
		}
	}
	return "", false
}

func assertionUsesStructuredProtocolPayloadMatcher(ctx *analysisContext, assertion gomegaAssertion) (string, bool) {
	if !exprSuggestsStructuredProtocolValue(ctx, assertion.actual) {
		return "", false
	}
	return matcherTreeContainsStructuredProtocolPayloadMatcher(assertion.matcher)
}

func assertionUsesStructuredProtocolStatusMatcher(ctx *analysisContext, assertion gomegaAssertion) bool {
	if assertionUsesDirectProtocolStatusBool(ctx, assertion) {
		return true
	}
	if !exprSuggestsStructuredProtocolValue(ctx, assertion.actual) {
		return false
	}
	return matcherTreeContainsStructuredProtocolStatusBool(assertion.matcher)
}

func assertionUsesDirectProtocolStatusBool(ctx *analysisContext, assertion gomegaAssertion) bool {
	selector, ok := unparenExpr(assertion.actual).(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != "IsError" || !isBooleanMatcher(assertion.matcher) {
		return false
	}
	return exprSuggestsStructuredProtocolValue(ctx, selector.X)
}

func matcherTreeContainsStructuredProtocolStatusBool(expr ast.Expr) bool {
	switch node := unparenExpr(expr).(type) {
	case *ast.CallExpr:
		if isMatcherNamed(node, "HaveField") && len(node.Args) > 1 {
			if fieldName, ok := stringLiteralValue(node.Args[0]); ok && fieldName == "IsError" && exprIsBooleanMatcher(node.Args[1]) {
				return true
			}
		}
		for _, arg := range node.Args {
			if matcherTreeContainsStructuredProtocolStatusBool(arg) {
				return true
			}
		}
	case *ast.CompositeLit:
		for _, elt := range node.Elts {
			keyValue, ok := elt.(*ast.KeyValueExpr)
			if !ok {
				if matcherTreeContainsStructuredProtocolStatusBool(elt) {
					return true
				}
				continue
			}
			if fieldName, ok := stringLiteralValue(keyValue.Key); ok && fieldName == "IsError" && exprIsBooleanMatcher(keyValue.Value) {
				return true
			}
			if matcherTreeContainsStructuredProtocolStatusBool(keyValue.Value) {
				return true
			}
		}
	}
	return false
}

func matcherTreeContainsStructuredProtocolPayloadMatcher(expr ast.Expr) (string, bool) {
	call, ok := unparenExpr(expr).(*ast.CallExpr)
	if !ok {
		return "", false
	}
	if isMatcherNamed(call, "MatchJSON", "MatchYAML", "MatchXML") && len(call.Args) > 0 {
		if literalValueSuggestsStructuredProtocol(call.Args[0]) {
			return callName(call), true
		}
	}
	for _, arg := range call.Args {
		if matcherName, ok := matcherTreeContainsStructuredProtocolPayloadMatcher(arg); ok {
			return matcherName, true
		}
	}
	return "", false
}

func structuredErrorFieldName(name string) bool {
	switch canonicalName(name) {
	case "code", "errorcode", "field", "fieldcode", "issuecode", "message", "messageid":
		return true
	default:
		return false
	}
}

func exprSuggestsStructuredErrorValue(ctx *analysisContext, expr ast.Expr) bool {
	typ := ctx.pass.TypesInfo.TypeOf(expr)
	if ptr, ok := typ.(*types.Pointer); ok {
		typ = ptr.Elem()
	}
	if named, ok := typ.(*types.Named); ok {
		if _, ok := named.Underlying().(*types.Struct); ok && isMessageBearingStructName(named.Obj().Name()) {
			return true
		}
	}
	canonical := canonicalName(exprName(expr))
	return strings.Contains(canonical, "error") ||
		strings.Contains(canonical, "validation") ||
		strings.Contains(canonical, "issue")
}

func exprSuggestsStructuredProtocolValue(ctx *analysisContext, expr ast.Expr) bool {
	canonical := canonicalName(exprName(expr))
	if strings.Contains(canonical, "payload") ||
		strings.Contains(canonical, "response") ||
		strings.Contains(canonical, "result") ||
		strings.Contains(canonical, "error") ||
		strings.Contains(canonical, "validation") ||
		strings.Contains(canonical, "issue") ||
		strings.Contains(canonical, "tool") {
		return true
	}
	typ := ctx.pass.TypesInfo.TypeOf(expr)
	if ptr, ok := typ.(*types.Pointer); ok {
		typ = ptr.Elem()
	}
	named, ok := typ.(*types.Named)
	if !ok {
		return false
	}
	typeName := canonicalName(named.Obj().Name())
	return strings.Contains(typeName, "payload") ||
		strings.Contains(typeName, "response") ||
		strings.Contains(typeName, "result") ||
		strings.Contains(typeName, "error") ||
		strings.Contains(typeName, "validation") ||
		strings.Contains(typeName, "issue") ||
		strings.Contains(typeName, "tool")
}

func structuredProtocolKeyName(name string) bool {
	switch canonicalName(name) {
	case "code", "error", "errorcode", "field", "fieldcode", "issuecode", "message", "messageid", "toolmessageid":
		return true
	default:
		return false
	}
}

func literalValueSuggestsStructuredProtocol(expr ast.Expr) bool {
	value, ok := stringLiteralValue(expr)
	if !ok {
		return false
	}
	canonical := canonicalName(value)
	return strings.Contains(canonical, "messageid") ||
		strings.Contains(canonical, "errorcode") ||
		strings.Contains(canonical, "fieldcode") ||
		strings.Contains(canonical, "issuecode")
}

func stringLiteralValue(expr ast.Expr) (string, bool) {
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

func funcLitHasGomegaParam(fn *ast.FuncLit) bool {
	if fn.Type.Params == nil {
		return false
	}
	for _, param := range fn.Type.Params.List {
		if exprName(param.Type) == "Gomega" {
			return true
		}
		for _, name := range param.Names {
			if name != nil && canonicalName(name.Name) == "g" && exprName(param.Type) == "Gomega" {
				return true
			}
		}
	}
	return false
}

func reportGlobalExpectCallsInAsyncCallback(ctx *analysisContext, fn *ast.FuncLit) {
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || !isGlobalGomegaExpectCall(call) {
			return true
		}
		ctx.report(ruleGomegaAsyncCallbackExpect, call, gomegaAsyncCallbackExpectDiagnostic())
		return true
	})
}

func isGlobalGomegaExpectCall(call *ast.CallExpr) bool {
	if _, ok := call.Fun.(*ast.Ident); !ok {
		return false
	}
	return isGomegaExpectationSource(callName(call))
}

func isTestAssertionHelperName(name string) bool {
	return strings.HasPrefix(name, "assert") || strings.HasPrefix(name, "expect")
}

func gomegaMatcherFactoryName(name string) bool {
	canonical := canonicalName(name)
	return strings.HasPrefix(canonical, "have") ||
		strings.HasPrefix(canonical, "contain") ||
		strings.HasPrefix(canonical, "match") ||
		strings.HasPrefix(canonical, "be")
}

func gomegaRenderedOutputMatcherFactoryName(name string) bool {
	canonical := canonicalName(name)
	if !strings.HasPrefix(canonical, "have") &&
		!strings.HasPrefix(canonical, "contain") &&
		!strings.HasPrefix(canonical, "match") &&
		!strings.HasPrefix(canonical, "be") {
		return false
	}
	return strings.Contains(canonical, "error") ||
		strings.Contains(canonical, "fatal") ||
		strings.Contains(canonical, "message") ||
		strings.Contains(canonical, "output") ||
		strings.Contains(canonical, "stderr") ||
		strings.Contains(canonical, "stdout")
}

func funcReturnsGomegaMatcher(fn *ast.FuncDecl) bool {
	if fn.Type.Results == nil {
		return false
	}
	for _, field := range fn.Type.Results.List {
		if exprName(field.Type) == "GomegaMatcher" {
			return true
		}
		if selector, ok := field.Type.(*ast.SelectorExpr); ok && selector.Sel.Name == "GomegaMatcher" {
			return true
		}
	}
	return false
}

func funcContainsGomegaAssertion(ctx *analysisContext, body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(node ast.Node) bool {
		if found || node == nil {
			return false
		}
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		_, found = gomegaAssertionFromCall(ctx, call)
		return !found
	})
	return found
}

func funcHasGomegaHelperReporting(ctx *analysisContext, fn *ast.FuncDecl) bool {
	if funcUsesGomegaParam(ctx, fn) {
		return true
	}
	found := false
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		if found || node == nil {
			return false
		}
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		if isGomegaHelperReportingCall(callName(call)) {
			found = true
			return false
		}
		return true
	})
	return found
}

func isGomegaHelperReportingCall(name string) bool {
	switch name {
	case "GinkgoHelper", "ExpectWithOffset", "EventuallyWithOffset", "ConsistentlyWithOffset", "WithOffset":
		return true
	default:
		return false
	}
}

func funcUsesGomegaParam(ctx *analysisContext, fn *ast.FuncDecl) bool {
	names := gomegaParamNames(fn)
	if len(names) == 0 {
		return false
	}
	used := false
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		if used || node == nil {
			return false
		}
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != "Expect" {
			return true
		}
		if names[exprName(selector.X)] {
			used = true
			return false
		}
		return true
	})
	return used
}

func gomegaParamNames(fn *ast.FuncDecl) map[string]bool {
	names := map[string]bool{}
	if fn.Type.Params == nil {
		return names
	}
	for _, param := range fn.Type.Params.List {
		if exprName(param.Type) != "Gomega" {
			continue
		}
		for _, name := range param.Names {
			if name != nil {
				names[name.Name] = true
			}
		}
	}
	return names
}
