package semantichygiene

import (
	"go/ast"
	"go/token"
	"go/types"
)

func assertionUsesHTTPStatusEqual(ctx *analysisContext, assertion gomegaAssertion) bool {
	if !isMatcherNamed(assertion.matcher, "Equal") {
		return false
	}
	selector, ok := unparenExpr(assertion.actual).(*ast.SelectorExpr)
	if !ok {
		return false
	}
	switch selector.Sel.Name {
	case "Code":
		return isNamedTypeFromPackage(ctx.pass.TypesInfo.TypeOf(selector.X), "net/http/httptest", "ResponseRecorder")
	case "StatusCode":
		return isNamedTypeFromPackage(ctx.pass.TypesInfo.TypeOf(selector.X), "net/http", "Response")
	default:
		return false
	}
}

func assertionUsesHTTPBodyString(ctx *analysisContext, assertion gomegaAssertion) bool {
	if !isMatcherNamed(assertion.matcher, "Equal", "ContainSubstring") {
		return false
	}
	call, ok := unparenExpr(assertion.actual).(*ast.CallExpr)
	if !ok || callName(call) != "String" {
		return false
	}
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	body, ok := unparenExpr(selector.X).(*ast.SelectorExpr)
	return ok && body.Sel.Name == "Body" &&
		isNamedTypeFromPackage(ctx.pass.TypesInfo.TypeOf(body.X), "net/http/httptest", "ResponseRecorder")
}

func assertionUsesRepeatedHTTPBodyMatcher(ctx *analysisContext, assertion gomegaAssertion) bool {
	if !isMatcherNamed(assertion.matcher, "HaveHTTPBody") {
		return false
	}
	stmt, ok := enclosingExprStmt(ctx, assertion.call)
	if !ok {
		return false
	}
	block, ok := ctx.parent(stmt).(*ast.BlockStmt)
	if !ok {
		return false
	}
	for index, candidate := range block.List {
		if candidate != stmt || index == 0 {
			continue
		}
		prev, ok := block.List[index-1].(*ast.ExprStmt)
		if !ok {
			return false
		}
		prevCall, ok := prev.X.(*ast.CallExpr)
		if !ok {
			return false
		}
		prevAssertion, ok := gomegaAssertionFromCall(ctx, prevCall)
		return ok &&
			isMatcherNamed(prevAssertion.matcher, "HaveHTTPBody") &&
			sameSimpleExpr(prevAssertion.actual, assertion.actual)
	}
	return false
}

func enclosingExprStmt(ctx *analysisContext, node ast.Node) (*ast.ExprStmt, bool) {
	for current := node; current != nil; current = ctx.parent(current) {
		if stmt, ok := current.(*ast.ExprStmt); ok {
			return stmt, true
		}
		if _, ok := current.(*ast.FuncDecl); ok {
			return nil, false
		}
	}
	return nil, false
}

func assertionUsesHTTPHeaderGet(ctx *analysisContext, assertion gomegaAssertion) bool {
	if !isMatcherNamed(assertion.matcher, "Equal", "ContainSubstring") {
		return false
	}
	call, ok := unparenExpr(assertion.actual).(*ast.CallExpr)
	if !ok || callName(call) != "Get" {
		return false
	}
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	header, ok := unparenExpr(selector.X).(*ast.SelectorExpr)
	if !ok || header.Sel.Name != "Header" {
		return false
	}
	return isNamedTypeFromPackage(ctx.pass.TypesInfo.TypeOf(header.X), "net/http", "Response") ||
		isNamedTypeFromPackage(ctx.pass.TypesInfo.TypeOf(header), "net/http", "Header")
}

func assertionUsesResponseHeaderMatcherOnRequest(ctx *analysisContext, assertion gomegaAssertion) bool {
	return isMatcherNamed(assertion.matcher, "HaveHTTPHeaderWithValue") &&
		isNamedTypeFromPackage(ctx.pass.TypesInfo.TypeOf(assertion.actual), "net/http", "Request")
}

func assertionUsesNumericBeEquivalentTo(ctx *analysisContext, assertion gomegaAssertion) bool {
	if !isMatcherNamed(assertion.matcher, "BeEquivalentTo") {
		return false
	}
	if isNumericType(ctx.pass.TypesInfo.TypeOf(assertion.actual)) {
		return true
	}
	return len(assertion.matcher.Args) > 0 && isNumericType(ctx.pass.TypesInfo.TypeOf(assertion.matcher.Args[0]))
}

func assertionUsesTimeEqual(ctx *analysisContext, assertion gomegaAssertion) bool {
	return isMatcherNamed(assertion.matcher, "Equal") &&
		len(assertion.matcher.Args) > 0 &&
		isTimeTimeType(ctx.pass.TypesInfo.TypeOf(assertion.actual)) &&
		isTimeTimeType(ctx.pass.TypesInfo.TypeOf(assertion.matcher.Args[0]))
}

func assertionUsesEqualEmpty(assertion gomegaAssertion) bool {
	if !isMatcherNamed(assertion.matcher, "Equal") || len(assertion.matcher.Args) != 1 {
		return false
	}
	switch arg := unparenExpr(assertion.matcher.Args[0]).(type) {
	case *ast.BasicLit:
		return arg.Kind == token.STRING && arg.Value == `""`
	case *ast.CompositeLit:
		switch arg.Type.(type) {
		case *ast.ArrayType, *ast.MapType:
			return len(arg.Elts) == 0
		default:
			return false
		}
	default:
		return false
	}
}

func isEqualZeroMatcherCall(call *ast.CallExpr) bool {
	if !isMatcherNamed(call, "Equal") || len(call.Args) != 1 {
		return false
	}
	lit, ok := unparenExpr(call.Args[0]).(*ast.BasicLit)
	return ok && lit.Kind == token.INT && lit.Value == "0"
}

func isEqualBooleanLiteralMatcherCall(call *ast.CallExpr) bool {
	if !isMatcherNamed(call, "Equal") || len(call.Args) != 1 {
		return false
	}
	ident, ok := unparenExpr(call.Args[0]).(*ast.Ident)
	return ok && (ident.Name == "true" || ident.Name == "false")
}

func assertionUsesRepeatedFieldAssertion(ctx *analysisContext, assertion gomegaAssertion) bool {
	selector, ok := assertedStructField(assertion.actual)
	if !ok {
		return false
	}
	if exprSuggestsStructuredErrorValue(ctx, selector.X) {
		return false
	}
	stmt, ok := enclosingExprStmt(ctx, assertion.call)
	if !ok {
		return false
	}
	block, ok := ctx.parent(stmt).(*ast.BlockStmt)
	if !ok {
		return false
	}
	for _, candidate := range block.List {
		if candidate == stmt {
			continue
		}
		candidateStmt, ok := candidate.(*ast.ExprStmt)
		if !ok {
			continue
		}
		candidateCall, ok := candidateStmt.X.(*ast.CallExpr)
		if !ok {
			continue
		}
		candidateAssertion, ok := gomegaAssertionFromCall(ctx, candidateCall)
		if !ok {
			continue
		}
		candidateSelector, ok := assertedStructField(candidateAssertion.actual)
		if !ok || candidateSelector.Sel.Name == selector.Sel.Name {
			continue
		}
		if sameSimpleExpr(candidateSelector.X, selector.X) {
			return true
		}
	}
	return false
}

func assertedStructField(expr ast.Expr) (*ast.SelectorExpr, bool) {
	selector, ok := unparenExpr(expr).(*ast.SelectorExpr)
	if !ok {
		return nil, false
	}
	if _, ok := unparenExpr(selector.X).(*ast.IndexExpr); ok {
		return nil, false
	}
	return selector, true
}

func assertionUsesCollectionIndexAssertion(ctx *analysisContext, assertion gomegaAssertion) bool {
	return exprUsesCollectionIndex(ctx, assertion.actual)
}

func assertionUsesPositionalCompositeAssertion(assertion gomegaAssertion) bool {
	if !isValueComparisonMatcher(assertion.matcher) || len(assertion.matcher.Args) != 1 {
		return false
	}
	actual, ok := unparenExpr(assertion.actual).(*ast.CompositeLit)
	if !ok || len(actual.Elts) < 2 || !compositeLiteralIsPositional(actual) {
		return false
	}
	expected, ok := unparenExpr(assertion.matcher.Args[0]).(*ast.CompositeLit)
	if !ok || len(expected.Elts) < 2 || !compositeLiteralIsPositional(expected) {
		return false
	}
	return compositeLiteralContainsMultipleFieldSelectors(actual)
}

func compositeLiteralContainsMultipleFieldSelectors(lit *ast.CompositeLit) bool {
	fieldSelectors := 0
	for _, elt := range lit.Elts {
		if _, ok := unparenExpr(elt).(*ast.SelectorExpr); ok {
			fieldSelectors++
		}
	}
	return fieldSelectors >= 2
}

func exprUsesCollectionIndex(ctx *analysisContext, expr ast.Expr) bool {
	switch e := unparenExpr(expr).(type) {
	case *ast.IndexExpr:
		return typeIsSliceOrArray(ctx.pass.TypesInfo.TypeOf(e.X))
	case *ast.SelectorExpr:
		return exprUsesCollectionIndex(ctx, e.X)
	default:
		return false
	}
}

func isNumericType(typ types.Type) bool {
	if typ == nil {
		return false
	}
	basic, ok := types.Unalias(typ).Underlying().(*types.Basic)
	return ok && basic.Info()&types.IsNumeric != 0
}

func isTimeTimeType(typ types.Type) bool {
	named := namedType(typ)
	return named != nil && named.Obj().Name() == "Time" && named.Obj().Pkg() != nil && named.Obj().Pkg().Path() == "time"
}

func isNamedTypeFromPackage(typ types.Type, packagePath string, name string) bool {
	named := namedType(typ)
	return named != nil && named.Obj().Name() == name && named.Obj().Pkg() != nil && named.Obj().Pkg().Path() == packagePath
}

func namedType(typ types.Type) *types.Named {
	if typ == nil {
		return nil
	}
	typ = types.Unalias(typ)
	if ptr, ok := typ.(*types.Pointer); ok {
		typ = ptr.Elem()
	}
	named, _ := typ.(*types.Named)
	return named
}

func eventuallyBareActualAllowed(ctx *analysisContext, actual ast.Expr) bool {
	actual = unparenExpr(actual)
	switch expr := actual.(type) {
	case *ast.FuncLit:
		return true
	case *ast.Ident, *ast.SelectorExpr:
		return eventuallyBareTypeAllowed(ctx.pass.TypesInfo.TypeOf(expr))
	default:
		return eventuallyBareTypeAllowed(ctx.pass.TypesInfo.TypeOf(actual))
	}
}

func eventuallyBareTypeAllowed(typ types.Type) bool {
	if typ == nil {
		return false
	}
	typ = types.Unalias(typ)
	if _, ok := typ.Underlying().(*types.Chan); ok {
		return true
	}
	if _, ok := typ.Underlying().(*types.Signature); ok {
		return true
	}
	if isNamedTypeFromPackage(typ, "github.com/onsi/gomega/gbytes", "Buffer") {
		return true
	}
	if isNamedTypeFromPackage(typ, "github.com/onsi/gomega/gexec", "Session") {
		return true
	}
	return false
}
