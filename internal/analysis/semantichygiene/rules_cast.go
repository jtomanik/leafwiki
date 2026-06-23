package semantichygiene

import "go/ast"

func checkDirectCast(ctx *analysisContext, call *ast.CallExpr) {
	typeName, ok := conversionSemanticTypeName(ctx.pass, call.Fun)
	if !ok || len(call.Args) != 1 {
		return
	}
	if !isStringType(ctx.pass, call.Args[0]) {
		return
	}
	if containsSemanticStringEscape(ctx, call.Args[0]) {
		return
	}
	if isAllowedDirectCastContext(ctx, call, typeName) {
		return
	}
	ctx.pass.Reportf(call.Pos(), "%s", directCastDiagnostic(typeName))
}

func containsSemanticStringEscape(ctx *analysisContext, expr ast.Expr) bool {
	found := false
	ast.Inspect(expr, func(node ast.Node) bool {
		if found || node == nil {
			return false
		}
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		if selector, ok := call.Fun.(*ast.SelectorExpr); ok &&
			selector.Sel.Name == "String" &&
			len(call.Args) == 0 {
			_, found = semanticExprTypeName(ctx.pass, selector.X)
			return !found
		}
		if len(call.Args) == 1 && isBuiltinStringConversion(ctx, call) {
			_, found = semanticExprTypeName(ctx.pass, call.Args[0])
			return !found
		}
		return true
	})
	return found
}
