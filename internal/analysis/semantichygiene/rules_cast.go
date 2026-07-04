package semantichygiene

import (
	"go/ast"
	"go/constant"
	"go/token"
	"strings"
)

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
	ctx.report(ruleDirectCast, call, directCastDiagnostic(typeName))
}

func checkUncheckedConstructorCall(ctx *analysisContext, call *ast.CallExpr) {
	funcName := callName(call)
	if len(call.Args) == 0 ||
		!strings.HasPrefix(funcName, "New") ||
		!strings.HasSuffix(funcName, "Unchecked") {
		return
	}
	typeName, ok := semanticTypeNameOf(ctx.pass.TypesInfo.TypeOf(call))
	if !ok || !callHasPrimitiveArg(ctx, call) || isAllowedUncheckedConstructorCall(ctx, call, typeName) {
		return
	}
	ctx.report(ruleSemanticUncheckedConstructor, call, uncheckedConstructorDiagnostic(funcName, typeName))
}

func checkFixtureSemanticConstructorCall(ctx *analysisContext, call *ast.CallExpr) {
	funcName := callName(call)
	if len(call.Args) == 0 || !isFixtureFunctionName(funcName) {
		return
	}
	if !isAllowedFixtureConstructorCallFile(ctx.filename(call.Pos())) {
		return
	}
	if _, ok := semanticTypeNameOf(ctx.pass.TypesInfo.TypeOf(call)); !ok {
		return
	}
	for _, arg := range call.Args {
		if !isStringType(ctx.pass, arg) || isStaticFixtureStringArg(ctx, arg) {
			continue
		}
		ctx.report(ruleSemanticFixtureRuntimeConstructor, call, fixtureRuntimeConstructorDiagnostic(funcName))
		return
	}
}

func isAllowedFixtureConstructorCallFile(filename string) bool {
	return isTestFile(filename) || strings.Contains(filename, "/e2e/")
}

func isStaticFixtureStringArg(ctx *analysisContext, arg ast.Expr) bool {
	if lit, ok := unparenExpr(arg).(*ast.BasicLit); ok {
		return lit.Kind == token.STRING
	}
	value := ctx.pass.TypesInfo.Types[arg].Value
	return value != nil && value.Kind() == constant.String
}

func callHasPrimitiveArg(ctx *analysisContext, call *ast.CallExpr) bool {
	for _, arg := range call.Args {
		if isStringType(ctx.pass, arg) {
			return true
		}
		if _, ok := primitiveCarrierTypeName(ctx.pass.TypesInfo.TypeOf(arg)); ok {
			return true
		}
	}
	return false
}

func isAllowedUncheckedConstructorCall(ctx *analysisContext, call *ast.CallExpr, typeName string) bool {
	filename := ctx.filename(call.Pos())
	if isGeneratedOrVendored(filename) {
		return true
	}
	if isAllowedFixtureDirectCastContext(filename, enclosingFuncName(ctx, call)) {
		return true
	}
	if isAllowedSemanticConstructorFunction(ctx, call, typeName) {
		return true
	}
	return isAllowedSemanticOwnerAdapterFunc(ctx, call, typeName) ||
		isAllowedUncheckedConstructorOwnerContext(ctx, call, typeName)
}

func isAllowedUncheckedConstructorOwnerContext(ctx *analysisContext, call *ast.CallExpr, typeName string) bool {
	if !isSemanticOwnerAdapterFile(ctx, call.Pos()) {
		return false
	}
	fn := enclosingFunc(ctx, call)
	return fn != nil && (functionReturnsSemanticType(ctx, fn, typeName) ||
		functionHasSemanticReceiver(ctx, fn, typeName) ||
		functionHasSemanticParameter(ctx, fn, typeName))
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
