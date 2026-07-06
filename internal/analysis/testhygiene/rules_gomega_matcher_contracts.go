package testhygiene

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"
)

func assertionUsesRenderedProseMatcherContract(ctx *analysisContext, assertion gomegaAssertion) (string, bool) {
	fieldName, ok := matcherTreeContainsNamedExpectedValue(ctx, assertion.matcher, renderedProseMatcherFieldName, anyMatcherContractValue)
	if !ok {
		return "", false
	}
	if existingFieldName, ok := assertionUsesStructuredFieldMatcher(ctx, assertion); ok &&
		canonicalName(existingFieldName) == canonicalName(fieldName) {
		return "", false
	}
	if existingKeyName, ok := assertionUsesStructuredProtocolKeyMatcher(ctx, assertion); ok &&
		canonicalName(existingKeyName) == canonicalName(fieldName) {
		return "", false
	}
	return fieldName, true
}

func assertionMatchesRenderedProseField(ctx *analysisContext, assertion gomegaAssertion) (string, bool) {
	selector, ok := unparenExpr(assertion.actual).(*ast.SelectorExpr)
	if !ok || !renderedProseMatcherFieldName(selector.Sel.Name) {
		return "", false
	}
	if structuredErrorFieldName(selector.Sel.Name) && exprSuggestsStructuredErrorValue(ctx, selector.X) {
		return "", false
	}
	if !renderedProseFieldNameAlwaysContract(selector.Sel.Name) &&
		!exprSuggestsStructuredErrorValue(ctx, selector.X) &&
		!exprSuggestsTestRenderedProseContract(selector.X) {
		return "", false
	}
	if !matcherTreeContainsStringContentMatcher(ctx, assertion.matcher) {
		return "", false
	}
	return selector.Sel.Name, true
}

func assertionUsesSemanticContractProbe(ctx *analysisContext, assertion gomegaAssertion) (string, bool) {
	if !exprSuggestsSemanticContractMatcherSubject(ctx, assertion.actual) {
		return "", false
	}
	return matcherTreeContainsNamedExpectedValue(ctx, assertion.matcher, func(name string) bool {
		return semanticContractMatcherName(ctx, assertion.actual, name)
	}, rawSemanticContractValue)
}

func assertionUsesMatcherTreeRawStatusCode(assertion gomegaAssertion) bool {
	_, ok := matcherTreeContainsNamedExpectedValue(nil, assertion.matcher, func(name string) bool {
		return nameSuggestsRawStatusCode(name)
	}, func(_ *analysisContext, expr ast.Expr) bool {
		return rawStatusContractValue(expr)
	})
	return ok
}

func matcherTreeContainsNamedExpectedValue(
	ctx *analysisContext,
	expr ast.Expr,
	nameAllowed func(string) bool,
	valueAllowed func(*analysisContext, ast.Expr) bool,
) (string, bool) {
	switch node := unparenExpr(expr).(type) {
	case *ast.CallExpr:
		if isMatcherNamed(node, "HaveField", "HaveKeyWithValue") && len(node.Args) > 1 {
			if name, ok := stringLiteralValue(node.Args[0]); ok && nameAllowed(name) && valueAllowed(ctx, node.Args[1]) {
				return name, true
			}
		}
		for _, arg := range node.Args {
			if name, ok := matcherTreeContainsNamedExpectedValue(ctx, arg, nameAllowed, valueAllowed); ok {
				return name, true
			}
		}
	case *ast.CompositeLit:
		for _, elt := range node.Elts {
			if name, ok := matcherTreeContainsNamedExpectedValue(ctx, elt, nameAllowed, valueAllowed); ok {
				return name, true
			}
		}
	case *ast.KeyValueExpr:
		if name, ok := stringLiteralValue(node.Key); ok && nameAllowed(name) && valueAllowed(ctx, node.Value) {
			return name, true
		}
		if name, ok := matcherTreeContainsNamedExpectedValue(ctx, node.Value, nameAllowed, valueAllowed); ok {
			return name, true
		}
	}
	return "", false
}

func anyMatcherContractValue(*analysisContext, ast.Expr) bool {
	return true
}

func rawSemanticContractValue(ctx *analysisContext, expr ast.Expr) bool {
	if exprHasKnownSemanticType(ctx, expr) {
		return false
	}
	switch node := unparenExpr(expr).(type) {
	case *ast.BasicLit:
		return node.Kind == token.STRING
	case *ast.CallExpr:
		return rawSemanticContractMatcherCall(ctx, node)
	default:
		return false
	}
}

func rawSemanticContractMatcherCall(ctx *analysisContext, call *ast.CallExpr) bool {
	if exprHasKnownSemanticType(ctx, call) {
		return false
	}
	switch {
	case isMatcherNamed(call, "Equal", "BeEquivalentTo", "BeComparableTo"):
		return len(call.Args) > 0 && rawSemanticContractValue(ctx, call.Args[0])
	case isMatcherNamed(call, "HavePrefix", "HaveSuffix", "ContainSubstring", "MatchRegexp"):
		return len(call.Args) > 0 && rawSemanticContractValue(ctx, call.Args[0])
	case isMatcherNamed(call, "And", "Or", "SatisfyAll", "SatisfyAny"):
		for _, arg := range call.Args {
			if rawSemanticContractValue(ctx, arg) {
				return true
			}
		}
	}
	return false
}

func exprHasKnownSemanticType(ctx *analysisContext, expr ast.Expr) bool {
	if ctx == nil {
		return false
	}
	_, ok := semanticTypeNameOf(ctx.pass.TypesInfo.TypeOf(expr))
	return ok
}

func rawStatusContractValue(expr ast.Expr) bool {
	switch node := unparenExpr(expr).(type) {
	case *ast.BasicLit:
		return node.Kind == token.INT || node.Kind == token.FLOAT
	case *ast.SelectorExpr:
		return selectorSuggestsHTTPStatus(node)
	case *ast.CallExpr:
		if isStatusNumericConversion(node) && len(node.Args) == 1 {
			return rawStatusContractValue(node.Args[0])
		}
		if isMatcherNamed(node, "Equal", "BeEquivalentTo", "BeNumerically") {
			for _, arg := range node.Args {
				if rawStatusContractValue(arg) {
					return true
				}
			}
		}
	}
	return false
}

func selectorSuggestsHTTPStatus(selector *ast.SelectorExpr) bool {
	pkg, ok := unparenExpr(selector.X).(*ast.Ident)
	return ok && pkg.Name == "http" && strings.HasPrefix(selector.Sel.Name, "Status")
}

func isStatusNumericConversion(call *ast.CallExpr) bool {
	switch exprName(call.Fun) {
	case "float64", "float32", "int", "int64", "int32":
		return true
	default:
		return false
	}
}

func renderedProseMatcherFieldName(name string) bool {
	switch canonicalName(name) {
	case "error", "lasterror", "lasterrordetail", "message":
		return true
	default:
		return false
	}
}

func renderedProseFieldNameAlwaysContract(name string) bool {
	switch canonicalName(name) {
	case "lasterror", "lasterrordetail":
		return true
	default:
		return false
	}
}

func semanticContractMatcherName(ctx *analysisContext, actual ast.Expr, name string) bool {
	context := matcherContractContextName(ctx, actual)
	if typ, ok := semanticTypeForFieldName(name, context); ok && typ != "" {
		return true
	}
	if semanticName(name) {
		return true
	}
	switch canonicalName(name) {
	case "authmethod", "id", "subject":
		return true
	default:
		return false
	}
}

func matcherContractContextName(ctx *analysisContext, actual ast.Expr) string {
	typ := ctx.pass.TypesInfo.TypeOf(actual)
	if ptr, ok := typ.(*types.Pointer); ok {
		typ = ptr.Elem()
	}
	if named, ok := typ.(*types.Named); ok {
		return named.Obj().Name()
	}
	return exprName(actual)
}

func exprSuggestsSemanticContractMatcherSubject(ctx *analysisContext, expr ast.Expr) bool {
	context := canonicalName(matcherContractContextName(ctx, expr) + exprName(expr))
	for _, fragment := range []string{
		"actor", "auth", "contract", "descriptor", "dto", "grant", "page",
		"payload", "response", "result", "session", "status", "subject",
		"tool", "user", "workspace",
	} {
		if strings.Contains(context, fragment) {
			return true
		}
	}
	return false
}
