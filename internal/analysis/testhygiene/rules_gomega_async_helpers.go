package testhygiene

import (
	"go/ast"
	"go/types"
)

func asyncAssertionRequiresSpecContext(ctx *analysisContext, assertion gomegaAsyncAssertion) bool {
	contextNames := enclosingSpecContextNames(ctx, assertion.call)
	if len(contextNames) == 0 {
		return false
	}
	return !asyncAssertionUsesContext(ctx, assertion, contextNames)
}

func enclosingSpecContextNames(ctx *analysisContext, node ast.Node) map[string]bool {
	for current := node; current != nil; current = ctx.parent(current) {
		fn, ok := current.(*ast.FuncLit)
		if !ok {
			if _, ok := current.(*ast.FuncDecl); ok {
				return nil
			}
			continue
		}
		call, ok := ctx.parent(fn).(*ast.CallExpr)
		if !ok || !isGinkgoSubjectBodyNodeName(callName(call)) {
			continue
		}
		return funcLitContextParamNames(ctx, fn)
	}
	return nil
}

func funcLitContextParamNames(ctx *analysisContext, fn *ast.FuncLit) map[string]bool {
	names := map[string]bool{}
	if fn.Type.Params == nil {
		return names
	}
	for _, field := range fn.Type.Params.List {
		if !isContextParamType(ctx, field.Type) {
			continue
		}
		for _, name := range field.Names {
			if name != nil {
				names[name.Name] = true
			}
		}
	}
	return names
}

func isContextParamType(ctx *analysisContext, expr ast.Expr) bool {
	if isContextType(ctx.pass.TypesInfo.TypeOf(expr)) {
		return true
	}
	switch exprName(expr) {
	case "Context", "SpecContext":
		return true
	default:
		return false
	}
}

func asyncAssertionUsesContext(ctx *analysisContext, assertion gomegaAsyncAssertion, contextNames map[string]bool) bool {
	if asyncCallChainUsesWithContext(assertion.call) {
		return true
	}
	for _, arg := range assertion.source.Args {
		if ident, ok := unparenExpr(arg).(*ast.Ident); ok && contextNames[ident.Name] {
			return true
		}
		if isContextType(ctx.pass.TypesInfo.TypeOf(arg)) {
			return true
		}
	}
	return false
}

func asyncCallChainUsesWithContext(call *ast.CallExpr) bool {
	found := false
	ast.Inspect(call, func(node ast.Node) bool {
		if found || node == nil {
			return false
		}
		if _, ok := node.(*ast.FuncLit); ok {
			return false
		}
		candidate, ok := node.(*ast.CallExpr)
		if ok && callName(candidate) == "WithContext" {
			found = true
			return false
		}
		return true
	})
	return found
}

func assertionUsesAsyncBooleanMatcher(ctx *analysisContext, assertion gomegaAsyncAssertion) bool {
	return isBooleanMatcher(assertion.matcher) && asyncActualProducesBool(ctx, assertion.actual)
}

func asyncActualProducesBool(ctx *analysisContext, actual ast.Expr) bool {
	actual = unparenExpr(actual)
	if fn, ok := actual.(*ast.FuncLit); ok {
		return funcLitReturnsBool(ctx, fn)
	}
	return isBoolType(ctx.pass.TypesInfo.TypeOf(actual))
}

func funcLitReturnsBool(ctx *analysisContext, fn *ast.FuncLit) bool {
	if fn.Type.Results == nil || len(fn.Type.Results.List) != 1 {
		return false
	}
	return isBoolType(ctx.pass.TypesInfo.TypeOf(fn.Type.Results.List[0].Type))
}

func isBoolType(typ types.Type) bool {
	if typ == nil {
		return false
	}
	basic, ok := types.Unalias(typ).Underlying().(*types.Basic)
	return ok && (basic.Kind() == types.Bool || basic.Kind() == types.UntypedBool)
}

func isContextType(typ types.Type) bool {
	named := namedType(typ)
	if named == nil {
		return false
	}
	if named.Obj().Name() == "SpecContext" {
		return true
	}
	return named.Obj().Name() == "Context" &&
		named.Obj().Pkg() != nil &&
		named.Obj().Pkg().Path() == "context"
}

func unparenExpr(expr ast.Expr) ast.Expr {
	for {
		paren, ok := expr.(*ast.ParenExpr)
		if !ok {
			return expr
		}
		expr = paren.X
	}
}

func isBooleanMatcher(matcher *ast.CallExpr) bool {
	if isMatcherNamed(matcher, "BeTrue", "BeFalse", "BeTrueBecause", "BeFalseBecause") {
		return true
	}
	if !isMatcherNamed(matcher, "Equal") || len(matcher.Args) != 1 {
		return false
	}
	ident, ok := unparenExpr(matcher.Args[0]).(*ast.Ident)
	return ok && (ident.Name == "true" || ident.Name == "false")
}

func exprIsBooleanMatcher(expr ast.Expr) bool {
	call, ok := unparenExpr(expr).(*ast.CallExpr)
	return ok && isBooleanMatcher(call)
}

func matcherCallIsNestedBooleanMatcherValue(ctx *analysisContext, call *ast.CallExpr) bool {
	if !isMatcherNamed(call, "BeTrue", "BeFalse", "BeTrueBecause", "BeFalseBecause") {
		return false
	}
	for current := ctx.parent(call); current != nil; current = ctx.parent(current) {
		parentCall, ok := current.(*ast.CallExpr)
		if !ok {
			continue
		}
		if assertion, ok := gomegaAssertionFromCall(ctx, parentCall); ok {
			return assertion.matcher != call
		}
		if assertion, ok := gomegaAsyncAssertionFromCall(parentCall); ok {
			return assertion.matcher != call
		}
		if isKnownGomegaMatcherFactory(parentCall) || typeIsGomegaMatcher(ctx.pass.TypesInfo.TypeOf(parentCall)) {
			return true
		}
	}
	return false
}

func matcherCallUsesMatcherValueAsExpected(ctx *analysisContext, call *ast.CallExpr) bool {
	if !isValueComparisonMatcher(call) {
		return false
	}
	for _, arg := range call.Args {
		if exprTreeContainsGomegaMatcherValue(ctx, arg) {
			return true
		}
	}
	return false
}

func matcherUsesPositionalTransform(ctx *analysisContext, call *ast.CallExpr) bool {
	if !isMatcherNamed(call, "WithTransform") || len(call.Args) < 2 {
		return false
	}
	if !matcherIsEqualCompositeLiteral(call.Args[1]) {
		return false
	}
	fn, ok := unparenExpr(call.Args[0]).(*ast.FuncLit)
	if !ok {
		return false
	}
	return funcLitReturnsFieldTuple(ctx, fn)
}

func matcherIsEqualCompositeLiteral(expr ast.Expr) bool {
	call, ok := unparenExpr(expr).(*ast.CallExpr)
	if !ok || !isMatcherNamed(call, "Equal") || len(call.Args) != 1 {
		return false
	}
	lit, ok := unparenExpr(call.Args[0]).(*ast.CompositeLit)
	if !ok || len(lit.Elts) < 2 {
		return false
	}
	return compositeLiteralIsPositional(lit)
}

func funcLitReturnsFieldTuple(ctx *analysisContext, fn *ast.FuncLit) bool {
	paramObjects := funcLitParamObjects(ctx, fn)
	if len(paramObjects) == 0 {
		return false
	}
	found := false
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		if found || node == nil {
			return false
		}
		if nested, ok := node.(*ast.FuncLit); ok && nested != fn {
			return false
		}
		ret, ok := node.(*ast.ReturnStmt)
		if !ok || len(ret.Results) != 1 {
			return true
		}
		lit, ok := unparenExpr(ret.Results[0]).(*ast.CompositeLit)
		if !ok || len(lit.Elts) < 2 || !compositeLiteralIsPositional(lit) {
			return true
		}
		for _, elt := range lit.Elts {
			if !exprIsFieldSelectorFromParam(ctx, elt, paramObjects) {
				return true
			}
		}
		found = true
		return false
	})
	return found
}

func funcLitParamObjects(ctx *analysisContext, fn *ast.FuncLit) map[types.Object]struct{} {
	objects := map[types.Object]struct{}{}
	if fn.Type.Params == nil {
		return objects
	}
	for _, field := range fn.Type.Params.List {
		for _, name := range field.Names {
			if name == nil {
				continue
			}
			if obj := ctx.pass.TypesInfo.ObjectOf(name); obj != nil {
				objects[obj] = struct{}{}
			}
		}
	}
	return objects
}

func compositeLiteralIsPositional(lit *ast.CompositeLit) bool {
	switch lit.Type.(type) {
	case *ast.ArrayType, *ast.StructType, *ast.Ident, *ast.SelectorExpr:
	default:
		return false
	}
	for _, elt := range lit.Elts {
		if _, ok := unparenExpr(elt).(*ast.KeyValueExpr); ok {
			return false
		}
	}
	return true
}

func exprIsFieldSelectorFromParam(ctx *analysisContext, expr ast.Expr, paramObjects map[types.Object]struct{}) bool {
	ident := baseIdentForSelector(expr)
	if ident == nil {
		return false
	}
	obj := ctx.pass.TypesInfo.ObjectOf(ident)
	if obj == nil {
		return false
	}
	_, ok := paramObjects[obj]
	return ok
}

func baseIdentForSelector(expr ast.Expr) *ast.Ident {
	selector, ok := unparenExpr(expr).(*ast.SelectorExpr)
	if !ok {
		return nil
	}
	for {
		switch x := unparenExpr(selector.X).(type) {
		case *ast.Ident:
			return x
		case *ast.SelectorExpr:
			selector = x
		default:
			return nil
		}
	}
}

func isValueComparisonMatcher(call *ast.CallExpr) bool {
	return isMatcherNamed(call, "Equal", "BeEquivalentTo", "BeComparableTo", "BeIdenticalTo")
}

func exprTreeContainsGomegaMatcherValue(ctx *analysisContext, expr ast.Expr) bool {
	found := false
	ast.Inspect(expr, func(node ast.Node) bool {
		if found || node == nil {
			return false
		}
		current, ok := node.(ast.Expr)
		if !ok {
			return true
		}
		if exprIsGomegaMatcherValue(ctx, current) {
			found = true
			return false
		}
		return true
	})
	return found
}

func exprIsGomegaMatcherValue(ctx *analysisContext, expr ast.Expr) bool {
	expr = unparenExpr(expr)
	if call, ok := expr.(*ast.CallExpr); ok && isKnownGomegaMatcherFactory(call) {
		return true
	}
	if ident, ok := expr.(*ast.Ident); ok {
		if obj := ctx.pass.TypesInfo.ObjectOf(ident); obj != nil && typeIsGomegaMatcher(obj.Type()) {
			return true
		}
		if identDeclaredAsGomegaMatcherParameter(ctx, ident) {
			return true
		}
		if identInitializedWithGomegaMatcher(ctx, ident) {
			return true
		}
	}
	return typeIsGomegaMatcher(ctx.pass.TypesInfo.TypeOf(expr))
}

func identDeclaredAsGomegaMatcherParameter(ctx *analysisContext, ident *ast.Ident) bool {
	targetObject := ctx.pass.TypesInfo.ObjectOf(ident)
	for current := ast.Node(ident); current != nil; current = ctx.parent(current) {
		switch fn := current.(type) {
		case *ast.FuncDecl:
			return fieldListDeclaresGomegaMatcherName(ctx, fn.Type.Params, ident, targetObject)
		case *ast.FuncLit:
			return fieldListDeclaresGomegaMatcherName(ctx, fn.Type.Params, ident, targetObject)
		}
	}
	return false
}

func fieldListDeclaresGomegaMatcherName(ctx *analysisContext, fields *ast.FieldList, ident *ast.Ident, targetObject types.Object) bool {
	if fields == nil {
		return false
	}
	for _, field := range fields.List {
		if !exprNamesGomegaMatcherType(ctx, field.Type) {
			continue
		}
		for _, name := range field.Names {
			if name != nil && sameIdentifierObject(ctx, name, ident, targetObject) {
				return true
			}
		}
	}
	return false
}

func exprNamesGomegaMatcherType(ctx *analysisContext, expr ast.Expr) bool {
	if exprName(expr) == "GomegaMatcher" {
		return true
	}
	return typeIsGomegaMatcher(ctx.pass.TypesInfo.TypeOf(expr))
}

func identInitializedWithGomegaMatcher(ctx *analysisContext, ident *ast.Ident) bool {
	body := enclosingFunctionBody(ctx, ident)
	if body == nil {
		return false
	}
	targetObject := ctx.pass.TypesInfo.ObjectOf(ident)
	found := false
	ast.Inspect(body, func(node ast.Node) bool {
		if found || node == nil {
			return false
		}
		if _, ok := node.(*ast.FuncLit); ok {
			return false
		}
		switch candidate := node.(type) {
		case *ast.AssignStmt:
			if candidate.Pos() > ident.Pos() {
				return false
			}
			for i, lhs := range candidate.Lhs {
				lhsIdent, ok := unparenExpr(lhs).(*ast.Ident)
				if !ok || !sameIdentifierObject(ctx, lhsIdent, ident, targetObject) || i >= len(candidate.Rhs) {
					continue
				}
				found = exprTreeContainsGomegaMatcherProducer(ctx, candidate.Rhs[i])
				if found {
					return false
				}
			}
		case *ast.ValueSpec:
			if candidate.Pos() > ident.Pos() {
				return false
			}
			for i, name := range candidate.Names {
				if name == nil || !sameIdentifierObject(ctx, name, ident, targetObject) || i >= len(candidate.Values) {
					continue
				}
				found = exprTreeContainsGomegaMatcherProducer(ctx, candidate.Values[i])
				if found {
					return false
				}
			}
		}
		return true
	})
	return found
}
