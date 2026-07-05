package testhygiene

import (
	"go/ast"
	"go/types"
	"strings"
)

func checkGomegaAsyncAssertion(ctx *analysisContext, call *ast.CallExpr) {
	if !isTestFile(ctx.filename(call.Pos())) {
		return
	}
	assertion, ok := gomegaAsyncAssertionFromCall(call)
	if !ok {
		return
	}
	if assertion.sourceName == "Eventually" && isNegativeAssertionMethod(assertion.method) && isMatcherNamed(assertion.matcher, "Receive") {
		ctx.report(ruleGomegaAsyncNegativeReceive, assertion.call, gomegaAsyncNegativeReceiveDiagnostic())
	}
	if matcherTreeContainsRawStringMatchError(ctx, assertion.matcher) {
		ctx.report(ruleGomegaRawStringMatchError, assertion.matcher, gomegaRawStringMatchErrorDiagnostic())
	}
	if assertion.sourceName == "Eventually" && !eventuallyBareActualAllowed(ctx, assertion.actual) {
		ctx.report(ruleGomegaAsyncBareValue, assertion.actual, gomegaAsyncBareValueDiagnostic())
	}
	if asyncAssertionRequiresSpecContext(ctx, assertion) {
		ctx.report(ruleGomegaAsyncContext, assertion.source, ginkgoAsyncContextDiagnostic())
	}
	if assertionUsesAsyncBooleanMatcher(ctx, assertion) {
		ctx.report(ruleGomegaAsyncBoolean, assertion.actual, gomegaAsyncBooleanMatcherDiagnostic())
	}
}

func checkGomegaAsyncCallback(ctx *analysisContext, call *ast.CallExpr) {
	if !isTestFile(ctx.filename(call.Pos())) || !isGomegaAsyncCall(callName(call)) {
		return
	}
	for _, arg := range call.Args {
		fn, ok := arg.(*ast.FuncLit)
		if !ok || !funcLitHasGomegaParam(fn) {
			continue
		}
		reportGlobalExpectCallsInAsyncCallback(ctx, fn)
	}
}

func checkGomegaAssertionHelperOffset(ctx *analysisContext, fn *ast.FuncDecl) {
	if fn.Body == nil ||
		!isTestFile(ctx.filename(fn.Pos())) ||
		!isTestAssertionHelperName(fn.Name.Name) ||
		!funcContainsGomegaAssertion(ctx, fn.Body) ||
		funcHasGomegaHelperReporting(ctx, fn) {
		return
	}
	ctx.report(ruleGomegaHelperOffset, fn.Name, gomegaHelperOffsetDiagnostic(fn.Name.Name))
}

func checkGomegaMatcherFactorySignature(ctx *analysisContext, fn *ast.FuncDecl) {
	if fn.Body == nil ||
		!funcReturnsGomegaMatcher(fn) {
		return
	}
	filename := ctx.filename(fn.Pos())
	if !isTestFile(filename) && !isTestSupportFile(filename) {
		return
	}
	checkGomegaMatcherFactoryGenericHaveOccurred(ctx, fn)
	checkGomegaMatcherFactoryGenericToolErrorArgs(ctx, fn)
	checkGomegaMatcherFactoryBooleanErrorGate(ctx, fn)
	checkGomegaMatcherFactoryTransformedBooleanContract(ctx, fn)
	checkGomegaMatcherFactoryProxyBooleanPredicate(ctx, fn)
	checkGomegaMatcherFactoryLastErrorRenderedText(ctx, fn)
	checkGomegaMatcherFactoryStructuredProtocolStatus(ctx, fn)
	checkGomegaMatcherFactoryPredicateOnlyBoolean(ctx, fn)
	isMatcherFactory := gomegaMatcherFactoryName(fn.Name.Name)
	isRenderedOutputMatcherFactory := gomegaRenderedOutputMatcherFactoryName(fn.Name.Name)
	if fn.Type.Params == nil || !isMatcherFactory && !isRenderedOutputMatcherFactory {
		return
	}
}

func checkGomegaMatcherFactoryBooleanErrorGate(ctx *analysisContext, fn *ast.FuncDecl) {
	boolParams := matcherFactoryBoolParams(ctx, fn)
	proxyBoolParams := matcherFactoryProxyBoolParams(ctx, fn)
	if len(boolParams) == 0 ||
		!matcherFactoryCombinesBoolParamWithErrorPredicate(ctx, fn.Body, boolParams) &&
			!matcherFactoryUsesProxyBoolParam(ctx, fn.Body, proxyBoolParams) {
		return
	}
	ctx.report(ruleGomegaProxyBoolean, fn.Name, gomegaMatcherFactoryBooleanErrorGateDiagnostic())
}

func checkGomegaMatcherFactoryTransformedBooleanContract(ctx *analysisContext, fn *ast.FuncDecl) {
	boolParams := matcherFactoryBoolParams(ctx, fn)
	if len(boolParams) == 0 ||
		!matcherFactoryContainsWithTransform(fn.Body) ||
		!matcherFactoryUsesBoolParam(ctx, fn.Body, boolParams) {
		return
	}
	ctx.report(ruleGomegaProxyBoolean, fn.Name, gomegaMatcherFactoryTransformedBooleanContractDiagnostic())
}

func checkGomegaMatcherFactoryProxyBooleanPredicate(ctx *analysisContext, fn *ast.FuncDecl) {
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		switch current := node.(type) {
		case nil:
			return false
		case *ast.FuncLit:
			return false
		case *ast.CallExpr:
			if predicate := gomegaProxyBooleanPredicateMatcher(ctx, current); predicate != nil {
				ctx.report(ruleGomegaProxyBoolean, predicate, gomegaMatcherFactoryProxyBooleanPredicateDiagnostic())
				return false
			}
		}
		return true
	})
}

func checkGomegaMatcherFactoryLastErrorRenderedText(ctx *analysisContext, fn *ast.FuncDecl) {
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		switch current := node.(type) {
		case nil:
			return false
		case *ast.FuncLit:
			return false
		case *ast.CallExpr:
			if predicate := gomegaLastErrorRenderedPredicateMatcher(ctx, current); predicate != nil {
				ctx.report(ruleGomegaLastErrorNotEmpty, predicate, gomegaLastErrorNotEmptyDiagnostic())
				return false
			}
		}
		return true
	})
}

func checkGomegaMatcherFactoryStructuredProtocolStatus(ctx *analysisContext, fn *ast.FuncDecl) {
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		switch current := node.(type) {
		case nil:
			return false
		case *ast.FuncLit:
			return false
		case *ast.CallExpr:
			if predicate := gomegaStructuredProtocolStatusPredicateMatcher(ctx, current); predicate != nil {
				ctx.report(ruleGomegaStructuredProtocolStatus, predicate, gomegaStructuredProtocolStatusMatcherDiagnostic())
				return false
			}
		}
		return true
	})
}

func checkGomegaMatcherFactoryPredicateOnlyBoolean(ctx *analysisContext, fn *ast.FuncDecl) {
	boolParams := matcherFactoryBoolParams(ctx, fn)
	proxyBoolParams := matcherFactoryProxyBoolParams(ctx, fn)
	if len(boolParams) > 0 &&
		(matcherFactoryCombinesBoolParamWithErrorPredicate(ctx, fn.Body, boolParams) ||
			matcherFactoryUsesProxyBoolParam(ctx, fn.Body, proxyBoolParams)) {
		return
	}
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		switch current := node.(type) {
		case nil:
			return false
		case *ast.FuncLit:
			return false
		case *ast.CallExpr:
			if predicate := gomegaPredicateOnlyBooleanMatcher(ctx, current); predicate != nil {
				ctx.report(ruleGomegaProxyBoolean, predicate, gomegaMatcherFactoryPredicateOnlyBooleanDiagnostic())
				return false
			}
		}
		return true
	})
}

func matcherFactoryBoolParams(ctx *analysisContext, fn *ast.FuncDecl) map[types.Object]bool {
	if fn.Type.Params == nil {
		return nil
	}
	params := map[types.Object]bool{}
	for _, field := range fn.Type.Params.List {
		if !isBoolType(ctx.pass.TypesInfo.TypeOf(field.Type)) {
			continue
		}
		for _, name := range field.Names {
			if name == nil {
				continue
			}
			if object := ctx.pass.TypesInfo.Defs[name]; object != nil {
				params[object] = true
			}
		}
	}
	return params
}

func matcherFactoryProxyBoolParams(ctx *analysisContext, fn *ast.FuncDecl) map[types.Object]bool {
	if fn.Type.Params == nil {
		return nil
	}
	params := map[types.Object]bool{}
	for _, field := range fn.Type.Params.List {
		if !isBoolType(ctx.pass.TypesInfo.TypeOf(field.Type)) {
			continue
		}
		for _, name := range field.Names {
			if name == nil || !isProxyBooleanName(name.Name) {
				continue
			}
			if object := ctx.pass.TypesInfo.Defs[name]; object != nil {
				params[object] = true
			}
		}
	}
	return params
}

func matcherFactoryUsesProxyBoolParam(ctx *analysisContext, body *ast.BlockStmt, proxyBoolParams map[types.Object]bool) bool {
	if len(proxyBoolParams) == 0 {
		return false
	}
	return matcherFactoryUsesBoolParam(ctx, body, proxyBoolParams)
}

func matcherFactoryUsesBoolParam(ctx *analysisContext, body *ast.BlockStmt, boolParams map[types.Object]bool) bool {
	if len(boolParams) == 0 {
		return false
	}
	found := false
	ast.Inspect(body, func(node ast.Node) bool {
		if found || node == nil {
			return false
		}
		ident, ok := node.(*ast.Ident)
		if !ok {
			return true
		}
		object := ctx.pass.TypesInfo.Uses[ident]
		if object == nil {
			object = ctx.pass.TypesInfo.Defs[ident]
		}
		if boolParams[object] {
			found = true
			return false
		}
		return true
	})
	return found
}

func matcherFactoryContainsWithTransform(body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(node ast.Node) bool {
		if found || node == nil {
			return false
		}
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		if callName(call) == "WithTransform" {
			found = true
			return false
		}
		return true
	})
	return found
}

func matcherFactoryCombinesBoolParamWithErrorPredicate(ctx *analysisContext, body *ast.BlockStmt, boolParams map[types.Object]bool) bool {
	found := false
	ast.Inspect(body, func(node ast.Node) bool {
		if found || node == nil {
			return false
		}
		expr, ok := node.(ast.Expr)
		if !ok {
			return true
		}
		if exprTreeContainsObject(ctx, expr, boolParams) && exprTreeContainsErrorPredicate(ctx, expr) {
			found = true
			return false
		}
		return true
	})
	return found
}

func exprTreeContainsObject(ctx *analysisContext, expr ast.Expr, objects map[types.Object]bool) bool {
	found := false
	ast.Inspect(expr, func(node ast.Node) bool {
		if found || node == nil {
			return false
		}
		ident, ok := node.(*ast.Ident)
		if !ok {
			return true
		}
		object := ctx.pass.TypesInfo.Uses[ident]
		if object == nil {
			object = ctx.pass.TypesInfo.Defs[ident]
		}
		if objects[object] {
			found = true
			return false
		}
		return true
	})
	return found
}

func exprTreeContainsErrorPredicate(ctx *analysisContext, expr ast.Expr) bool {
	found := false
	ast.Inspect(expr, func(node ast.Node) bool {
		if found || node == nil {
			return false
		}
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		if callIsErrorPredicate(ctx, call) {
			found = true
			return false
		}
		return true
	})
	return found
}

func callIsErrorPredicate(ctx *analysisContext, call *ast.CallExpr) bool {
	packagePath, name := calleePackageAndName(ctx, call)
	if packagePath == "errors" && (name == "Is" || name == "As") {
		return true
	}
	if len(call.Args) == 0 {
		return false
	}
	if typeImplementsError(ctx.pass.TypesInfo.TypeOf(call.Args[0])) {
		canonical := canonicalName(name)
		return strings.HasPrefix(canonical, "is") && strings.Contains(canonical, "error") ||
			strings.HasSuffix(canonical, "error") ||
			strings.Contains(canonical, "status")
	}
	return false
}
