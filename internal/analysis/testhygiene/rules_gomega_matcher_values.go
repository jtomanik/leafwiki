package testhygiene

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"
)

func enclosingFunctionBody(ctx *analysisContext, node ast.Node) *ast.BlockStmt {
	for current := node; current != nil; current = ctx.parent(current) {
		switch fn := current.(type) {
		case *ast.FuncDecl:
			return fn.Body
		case *ast.FuncLit:
			return fn.Body
		}
	}
	return nil
}

func sameIdentifierObject(ctx *analysisContext, candidate *ast.Ident, target *ast.Ident, targetObject types.Object) bool {
	candidateObject := ctx.pass.TypesInfo.ObjectOf(candidate)
	if candidateObject != nil && targetObject != nil {
		return candidateObject == targetObject
	}
	return candidate.Name == target.Name
}

func exprTreeContainsGomegaMatcherProducer(ctx *analysisContext, expr ast.Expr) bool {
	found := false
	ast.Inspect(expr, func(node ast.Node) bool {
		if found || node == nil {
			return false
		}
		current, ok := node.(ast.Expr)
		if !ok {
			return true
		}
		switch candidate := unparenExpr(current).(type) {
		case *ast.CallExpr:
			if isKnownGomegaMatcherFactory(candidate) || typeIsGomegaMatcher(ctx.pass.TypesInfo.TypeOf(candidate)) {
				found = true
				return false
			}
		case *ast.TypeAssertExpr:
			if exprName(candidate.Type) == "GomegaMatcher" || typeIsGomegaMatcher(ctx.pass.TypesInfo.TypeOf(candidate)) {
				found = true
				return false
			}
		default:
			if typeIsGomegaMatcher(ctx.pass.TypesInfo.TypeOf(candidate)) {
				found = true
				return false
			}
		}
		return true
	})
	return found
}

func isKnownGomegaMatcherFactory(call *ast.CallExpr) bool {
	switch callName(call) {
	case "And",
		"BeAssignableToTypeOf",
		"BeClosed",
		"BeComparableTo",
		"BeElementOf",
		"BeEmpty",
		"BeEquivalentTo",
		"BeFalse",
		"BeIdenticalTo",
		"BeNumerically",
		"BeSent",
		"BeTemporally",
		"BeTrue",
		"BeZero",
		"ContainElement",
		"ContainElements",
		"ContainSubstring",
		"ConsistOf",
		"Equal",
		"HaveCap",
		"HaveEach",
		"HaveExactElements",
		"HaveExistingField",
		"HaveField",
		"HaveHTTPBody",
		"HaveHTTPHeaderWithValue",
		"HaveHTTPStatus",
		"HaveKey",
		"HaveKeyWithValue",
		"HaveLen",
		"HaveOccurred",
		"HavePrefix",
		"HaveSuffix",
		"HaveValue",
		"MatchAllElements",
		"MatchAllFields",
		"MatchAllKeys",
		"MatchError",
		"MatchFields",
		"MatchJSON",
		"MatchRegexp",
		"MatchXML",
		"MatchYAML",
		"Not",
		"Or",
		"Panic",
		"Receive",
		"Satisfy",
		"SatisfyAll",
		"SatisfyAny",
		"Succeed",
		"WithTransform":
		return true
	default:
		return false
	}
}

func typeIsGomegaMatcher(typ types.Type) bool {
	named := namedType(typ)
	if named == nil || named.Obj().Name() != "GomegaMatcher" {
		return typeStringSuggestsGomegaMatcher(typ)
	}
	pkg := named.Obj().Pkg()
	if pkg == nil {
		return true
	}
	return pkg.Path() == "github.com/onsi/gomega/types" ||
		strings.Contains(pkg.Path(), "/gomega")
}

func typeStringSuggestsGomegaMatcher(typ types.Type) bool {
	if typ == nil {
		return false
	}
	typeName := types.TypeString(types.Unalias(typ), nil)
	return typeName == "GomegaMatcher" || strings.HasSuffix(typeName, ".GomegaMatcher")
}

func assertionUsesRawStringMatchError(ctx *analysisContext, assertion gomegaAssertion) bool {
	return matcherTreeContainsRawStringMatchError(ctx, assertion.matcher)
}

func rawStringMatchErrorMatcherCall(ctx *analysisContext, call *ast.CallExpr) bool {
	if !isMatcherNamed(call, "MatchError") || len(call.Args) == 0 {
		return false
	}
	return matchErrorArgumentUsesRawString(ctx, call.Args[0])
}

func callIsInsideGomegaAssertion(ctx *analysisContext, call *ast.CallExpr) bool {
	for parent := ctx.parent(call); parent != nil; parent = ctx.parent(parent) {
		parentCall, ok := parent.(*ast.CallExpr)
		if !ok {
			continue
		}
		if _, ok := gomegaAssertionFromCall(ctx, parentCall); ok {
			return true
		}
		if _, ok := gomegaAsyncAssertionFromCall(parentCall); ok {
			return true
		}
	}
	return false
}

func matcherTreeContainsRawStringMatchError(ctx *analysisContext, expr ast.Expr) bool {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return false
	}
	if rawStringMatchErrorMatcherCall(ctx, call) {
		return true
	}
	for _, arg := range call.Args {
		if matcherTreeContainsRawStringMatchError(ctx, arg) {
			return true
		}
	}
	return false
}

func matchErrorArgumentUsesRawString(ctx *analysisContext, expr ast.Expr) bool {
	expr = unparenExpr(expr)
	lit, ok := expr.(*ast.BasicLit)
	if ok && lit.Kind == token.STRING {
		return true
	}
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return false
	}
	if isMatcherNamed(call, "Equal", "ContainSubstring", "HavePrefix", "HaveSuffix", "MatchRegexp") {
		return callHasStringArg(ctx, call)
	}
	if callConstructsRawErrorMessage(ctx, call) {
		return true
	}
	if isMatcherNamed(call, "And", "Or", "SatisfyAll", "SatisfyAny") {
		for _, arg := range call.Args {
			if matchErrorArgumentUsesRawString(ctx, arg) {
				return true
			}
		}
	}
	return false
}

func callConstructsRawErrorMessage(ctx *analysisContext, call *ast.CallExpr) bool {
	fn := calledFunctionObject(ctx, call)
	if fn == nil || fn.Pkg() == nil {
		return false
	}
	switch fn.Pkg().Path() {
	case "errors":
		return fn.Name() == "New" && callHasStringArg(ctx, call)
	case "fmt":
		return fn.Name() == "Errorf" && callHasStringArg(ctx, call)
	default:
		return false
	}
}

func callHasStringArg(ctx *analysisContext, call *ast.CallExpr) bool {
	for _, arg := range call.Args {
		lit, ok := unparenExpr(arg).(*ast.BasicLit)
		if ok && lit.Kind == token.STRING {
			return true
		}
		if isStringType(ctx.pass, arg) {
			return true
		}
	}
	return false
}

func isMatcherNamed(matcher *ast.CallExpr, names ...string) bool {
	name := callName(matcher)
	for _, candidate := range names {
		if name == candidate {
			return true
		}
	}
	return false
}

func sameSimpleExpr(left ast.Expr, right ast.Expr) bool {
	left = unparenExpr(left)
	right = unparenExpr(right)
	switch l := left.(type) {
	case *ast.Ident:
		r, ok := right.(*ast.Ident)
		return ok && l.Name == r.Name
	case *ast.SelectorExpr:
		r, ok := right.(*ast.SelectorExpr)
		return ok && l.Sel.Name == r.Sel.Name && sameSimpleExpr(l.X, r.X)
	default:
		return false
	}
}

func assertionMatchesStructuredErrorField(ctx *analysisContext, assertion gomegaAssertion) (string, bool) {
	if !isMatcherNamed(assertion.matcher, "Equal", "BeEquivalentTo", "ContainSubstring") {
		return "", false
	}
	selector, ok := assertion.actual.(*ast.SelectorExpr)
	if !ok || !structuredErrorFieldName(selector.Sel.Name) {
		return "", false
	}
	if !exprSuggestsStructuredErrorValue(ctx, selector.X) {
		return "", false
	}
	return selector.Sel.Name, true
}

func assertionUsesStructuredFieldMatcher(ctx *analysisContext, assertion gomegaAssertion) (string, bool) {
	if !exprSuggestsStructuredErrorValue(ctx, assertion.actual) {
		return "", false
	}
	return matcherTreeContainsStructuredFieldMatcher(assertion.matcher)
}

func matcherTreeContainsStructuredFieldMatcher(expr ast.Expr) (string, bool) {
	call, ok := unparenExpr(expr).(*ast.CallExpr)
	if !ok {
		return "", false
	}
	if isMatcherNamed(call, "HaveField") && len(call.Args) > 0 {
		if fieldName, ok := stringLiteralValue(call.Args[0]); ok && structuredErrorFieldName(fieldName) {
			return fieldName, true
		}
	}
	for _, arg := range call.Args {
		if fieldName, ok := matcherTreeContainsStructuredFieldMatcher(arg); ok {
			return fieldName, true
		}
	}
	return "", false
}
