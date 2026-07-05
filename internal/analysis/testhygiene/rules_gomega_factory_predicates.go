package testhygiene

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"
)

func checkGomegaMatcherFactoryGenericHaveOccurred(ctx *analysisContext, fn *ast.FuncDecl) {
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		switch current := node.(type) {
		case nil:
			return false
		case *ast.FuncLit:
			return false
		case *ast.CallExpr:
			if predicate := gomegaGenericErrorPredicateMatcher(ctx, current); predicate != nil {
				ctx.report(ruleGomegaGenericHaveOccurred, predicate, gomegaGenericHaveOccurredDiagnostic())
				return false
			}
		case *ast.ReturnStmt:
			for _, result := range current.Results {
				if predicate := gomegaGenericErrorPredicateMatcherInExpr(ctx, result); predicate != nil {
					ctx.report(ruleGomegaGenericHaveOccurred, predicate, gomegaGenericHaveOccurredDiagnostic())
				}
				if matcherTreeContainsPositiveHaveOccurred(result) {
					ctx.report(ruleGomegaGenericHaveOccurred, result, gomegaGenericHaveOccurredDiagnostic())
				}
				if matcherTreeContainsGenericErrorPresence(result) {
					ctx.report(ruleGomegaGenericHaveOccurred, result, gomegaGenericHaveOccurredDiagnostic())
				}
			}
			return false
		}
		return true
	})
}

func checkGomegaMatcherFactoryGenericToolErrorArgs(ctx *analysisContext, fn *ast.FuncDecl) {
	if matcherFactoryGenericToolErrorName(fn.Name.Name) {
		ctx.report(ruleGomegaGenericHaveOccurred, fn.Name, gomegaGenericHaveOccurredDiagnostic())
		return
	}
	var offender ast.Node
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		if offender != nil || node == nil {
			return false
		}
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		if !isMatcherNamed(call, "HaveField") || len(call.Args) < 2 {
			return true
		}
		fieldName, ok := stringLiteralValue(call.Args[0])
		if !ok || canonicalName(fieldName) != "args" {
			return true
		}
		if matcherTreeContainsStringFragmentMatcher(ctx, call.Args[1]) {
			offender = call
			return false
		}
		return true
	})
	if offender != nil {
		ctx.report(ruleGomegaGenericHaveOccurred, offender, gomegaGenericHaveOccurredDiagnostic())
	}
}

func matcherFactoryGenericToolErrorName(name string) bool {
	canonical := canonicalName(name)
	return strings.Contains(canonical, "generic") &&
		strings.Contains(canonical, "tool") &&
		strings.Contains(canonical, "error")
}

func matcherTreeContainsStringFragmentMatcher(ctx *analysisContext, expr ast.Expr) bool {
	found := false
	ast.Inspect(expr, func(node ast.Node) bool {
		if found || node == nil {
			return false
		}
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		if isMatcherNamed(call, "ContainSubstring", "HavePrefix", "HaveSuffix", "MatchRegexp") && callHasStringArg(ctx, call) {
			found = true
			return false
		}
		return true
	})
	return found
}

func gomegaGenericErrorPredicateMatcherInExpr(ctx *analysisContext, expr ast.Expr) ast.Expr {
	var predicate ast.Expr
	ast.Inspect(expr, func(node ast.Node) bool {
		if predicate != nil || node == nil {
			return false
		}
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		predicate = gomegaGenericErrorPredicateMatcher(ctx, call)
		return predicate == nil
	})
	return predicate
}

func gomegaGenericErrorPredicateMatcher(ctx *analysisContext, call *ast.CallExpr) ast.Expr {
	if callName(call) != "MakeMatcher" || len(call.Args) == 0 {
		return nil
	}
	fn, ok := call.Args[0].(*ast.FuncLit)
	if !ok {
		return nil
	}
	errorParams := gomegaMatcherErrorParams(ctx, fn)
	if len(errorParams) == 0 {
		return nil
	}
	return genericErrorPredicateReturn(ctx, fn.Body, errorParams)
}

func gomegaStructuredProtocolStatusPredicateMatcher(ctx *analysisContext, call *ast.CallExpr) ast.Expr {
	if callName(call) != "MakeMatcher" || len(call.Args) == 0 {
		return nil
	}
	fn, ok := call.Args[0].(*ast.FuncLit)
	if !ok {
		return nil
	}
	return rawStructuredProtocolStatusPredicateReturn(ctx, fn.Body)
}

func gomegaLastErrorRenderedPredicateMatcher(ctx *analysisContext, call *ast.CallExpr) ast.Expr {
	if callName(call) != "MakeMatcher" || len(call.Args) == 0 {
		return nil
	}
	fn, ok := call.Args[0].(*ast.FuncLit)
	if !ok {
		return nil
	}
	return lastErrorRenderedPredicateReturn(ctx, fn.Body)
}

func gomegaProxyBooleanPredicateMatcher(ctx *analysisContext, call *ast.CallExpr) ast.Expr {
	if callName(call) != "MakeMatcher" || len(call.Args) == 0 {
		return nil
	}
	fn, ok := call.Args[0].(*ast.FuncLit)
	if !ok {
		return nil
	}
	return proxyBooleanPredicateReturn(ctx, fn.Body)
}

func gomegaPredicateOnlyBooleanMatcher(ctx *analysisContext, call *ast.CallExpr) ast.Expr {
	if callName(call) != "MakeMatcher" || len(call.Args) == 0 {
		return nil
	}
	fn, ok := call.Args[0].(*ast.FuncLit)
	if !ok {
		return nil
	}
	if gomegaGenericErrorPredicateMatcher(ctx, call) != nil ||
		gomegaProxyBooleanPredicateMatcher(ctx, call) != nil ||
		gomegaLastErrorRenderedPredicateMatcher(ctx, call) != nil ||
		gomegaStructuredProtocolStatusPredicateMatcher(ctx, call) != nil {
		return nil
	}
	return predicateOnlyBooleanReturn(ctx, fn.Body)
}

func predicateOnlyBooleanReturn(ctx *analysisContext, body *ast.BlockStmt) ast.Expr {
	var predicate ast.Expr
	ast.Inspect(body, func(node ast.Node) bool {
		if predicate != nil || node == nil {
			return false
		}
		switch current := node.(type) {
		case *ast.FuncLit:
			return false
		case *ast.ReturnStmt:
			if len(current.Results) > 0 && exprIsPredicateOnlyBoolean(ctx, current.Results[0]) {
				predicate = current.Results[0]
			}
			return false
		}
		return true
	})
	return predicate
}

func exprIsPredicateOnlyBoolean(ctx *analysisContext, expr ast.Expr) bool {
	expr = unparenExpr(expr)
	if ident, ok := expr.(*ast.Ident); ok && (ident.Name == "true" || ident.Name == "false") {
		return false
	}
	if !isBoolType(ctx.pass.TypesInfo.TypeOf(expr)) {
		return false
	}
	switch current := expr.(type) {
	case *ast.BinaryExpr:
		return current.Op == token.LAND ||
			current.Op == token.LOR ||
			isBooleanProducingBinaryOp(current.Op)
	case *ast.CallExpr:
		return true
	case *ast.SelectorExpr:
		return true
	case *ast.UnaryExpr:
		return current.Op == token.NOT && exprIsPredicateOnlyBoolean(ctx, current.X)
	default:
		return false
	}
}

func proxyBooleanPredicateReturn(ctx *analysisContext, body *ast.BlockStmt) ast.Expr {
	var predicate ast.Expr
	ast.Inspect(body, func(node ast.Node) bool {
		if predicate != nil || node == nil {
			return false
		}
		switch current := node.(type) {
		case *ast.FuncLit:
			return false
		case *ast.ReturnStmt:
			if len(current.Results) > 0 && exprIsPureProxyBooleanPredicate(ctx, current.Results[0]) {
				predicate = current.Results[0]
			}
			return false
		}
		return true
	})
	return predicate
}

func exprIsPureProxyBooleanPredicate(ctx *analysisContext, expr ast.Expr) bool {
	pure, hasProxy := proxyBooleanPredicateParts(ctx, expr)
	return pure && hasProxy
}

func proxyBooleanPredicateParts(ctx *analysisContext, expr ast.Expr) (pure bool, hasProxy bool) {
	expr = unparenExpr(expr)
	switch current := expr.(type) {
	case *ast.Ident:
		isProxy := isProxyBooleanName(current.Name) && isBoolType(ctx.pass.TypesInfo.TypeOf(current))
		return isProxy, isProxy
	case *ast.SelectorExpr:
		isProxy := selectorIsProxyBoolean(ctx, current)
		return isProxy, isProxy
	case *ast.UnaryExpr:
		if current.Op != token.NOT {
			return false, false
		}
		return proxyBooleanPredicateParts(ctx, current.X)
	case *ast.BinaryExpr:
		if current.Op != token.LAND && current.Op != token.LOR {
			return false, false
		}
		leftPure, leftProxy := proxyBooleanPredicateParts(ctx, current.X)
		rightPure, rightProxy := proxyBooleanPredicateParts(ctx, current.Y)
		return leftPure && rightPure, leftProxy || rightProxy
	default:
		return false, false
	}
}

func lastErrorRenderedPredicateReturn(ctx *analysisContext, body *ast.BlockStmt) ast.Expr {
	var predicate ast.Expr
	ast.Inspect(body, func(node ast.Node) bool {
		if predicate != nil || node == nil {
			return false
		}
		switch current := node.(type) {
		case *ast.FuncLit:
			return false
		case *ast.ReturnStmt:
			if len(current.Results) > 0 {
				predicate = lastErrorRenderedPredicate(ctx, current.Results[0])
			}
			return false
		}
		return true
	})
	return predicate
}

func lastErrorRenderedPredicate(ctx *analysisContext, expr ast.Expr) ast.Expr {
	expr = unparenExpr(expr)
	switch current := expr.(type) {
	case *ast.BinaryExpr:
		if current.Op == token.LAND || current.Op == token.LOR {
			if predicate := lastErrorRenderedPredicate(ctx, current.X); predicate != nil {
				return predicate
			}
			return lastErrorRenderedPredicate(ctx, current.Y)
		}
		if (current.Op == token.EQL || current.Op == token.NEQ) &&
			(exprContainsLastErrorSelector(current.X) || exprContainsLastErrorSelector(current.Y)) {
			return current
		}
	case *ast.CallExpr:
		if callUsesLastErrorRenderedText(ctx, current) {
			return current
		}
	}
	return nil
}

func callUsesLastErrorRenderedText(ctx *analysisContext, call *ast.CallExpr) bool {
	if callName(call) == "Match" {
		return callArgsContainLastErrorSelector(call)
	}
	packagePath, name := calleePackageAndName(ctx, call)
	if packagePath == "strings" && (name == "Contains" || name == "HasPrefix" || name == "HasSuffix") {
		return callArgsContainLastErrorSelector(call)
	}
	return false
}

func callArgsContainLastErrorSelector(call *ast.CallExpr) bool {
	for _, arg := range call.Args {
		if exprContainsLastErrorSelector(arg) {
			return true
		}
	}
	return false
}

func exprContainsLastErrorSelector(expr ast.Expr) bool {
	found := false
	ast.Inspect(expr, func(node ast.Node) bool {
		if found || node == nil {
			return false
		}
		if _, ok := node.(*ast.FuncLit); ok {
			return false
		}
		selector, ok := node.(*ast.SelectorExpr)
		if ok && selector.Sel.Name == "LastError" {
			found = true
			return false
		}
		return true
	})
	return found
}

func rawStructuredProtocolStatusPredicateReturn(ctx *analysisContext, body *ast.BlockStmt) ast.Expr {
	var predicate ast.Expr
	ast.Inspect(body, func(node ast.Node) bool {
		if predicate != nil || node == nil {
			return false
		}
		switch current := node.(type) {
		case *ast.FuncLit:
			return false
		case *ast.ReturnStmt:
			if len(current.Results) > 0 {
				predicate = rawStructuredProtocolStatusPredicate(ctx, current.Results[0])
			}
			return false
		}
		return true
	})
	return predicate
}

func rawStructuredProtocolStatusPredicate(ctx *analysisContext, expr ast.Expr) ast.Expr {
	expr = unparenExpr(expr)
	if rawStructuredProtocolStatusNilGuard(ctx, expr) {
		return nil
	}
	if rawStructuredProtocolStatusSelector(ctx, expr) {
		return expr
	}
	switch current := expr.(type) {
	case *ast.UnaryExpr:
		if current.Op != token.NOT {
			return nil
		}
		if rawStructuredProtocolStatusSelector(ctx, current.X) {
			return current
		}
	case *ast.BinaryExpr:
		if current.Op != token.LAND && current.Op != token.LOR {
			return nil
		}
		left := rawStructuredProtocolStatusPredicate(ctx, current.X)
		right := rawStructuredProtocolStatusPredicate(ctx, current.Y)
		if left == nil && !rawStructuredProtocolStatusNilGuard(ctx, current.X) {
			return nil
		}
		if right == nil && !rawStructuredProtocolStatusNilGuard(ctx, current.Y) {
			return nil
		}
		if left != nil {
			return left
		}
		return right
	}
	return nil
}

func rawStructuredProtocolStatusSelector(ctx *analysisContext, expr ast.Expr) bool {
	selector, ok := unparenExpr(expr).(*ast.SelectorExpr)
	return ok && selector.Sel.Name == "IsError" && exprSuggestsStructuredProtocolValue(ctx, selector.X)
}

func rawStructuredProtocolStatusNilGuard(ctx *analysisContext, expr ast.Expr) bool {
	binary, ok := unparenExpr(expr).(*ast.BinaryExpr)
	if !ok || binary.Op != token.EQL && binary.Op != token.NEQ {
		return false
	}
	return isNilExpr(binary.X) && exprSuggestsStructuredProtocolValue(ctx, binary.Y) ||
		exprSuggestsStructuredProtocolValue(ctx, binary.X) && isNilExpr(binary.Y)
}

func gomegaMatcherErrorParams(ctx *analysisContext, fn *ast.FuncLit) map[types.Object]bool {
	if fn.Type.Params == nil {
		return nil
	}
	errorParams := map[types.Object]bool{}
	for _, field := range fn.Type.Params.List {
		if !typeImplementsError(ctx.pass.TypesInfo.TypeOf(field.Type)) {
			continue
		}
		for _, name := range field.Names {
			if name == nil {
				continue
			}
			if object := ctx.pass.TypesInfo.Defs[name]; object != nil {
				errorParams[object] = true
			}
		}
	}
	return errorParams
}

func genericErrorPredicateReturn(ctx *analysisContext, body *ast.BlockStmt, errorParams map[types.Object]bool) ast.Expr {
	var predicate ast.Expr
	ast.Inspect(body, func(node ast.Node) bool {
		if predicate != nil || node == nil {
			return false
		}
		switch current := node.(type) {
		case *ast.FuncLit:
			return false
		case *ast.ReturnStmt:
			if len(current.Results) > 0 && isErrorParamNotNilCheck(ctx, current.Results[0], errorParams) {
				predicate = current.Results[0]
			}
			return false
		}
		return true
	})
	return predicate
}

func isErrorParamNotNilCheck(ctx *analysisContext, expr ast.Expr, errorParams map[types.Object]bool) bool {
	binary, ok := unparenExpr(expr).(*ast.BinaryExpr)
	if !ok || binary.Op != token.NEQ {
		return false
	}
	return isErrorParamIdent(ctx, binary.X, errorParams) && isNilExpr(binary.Y) ||
		isNilExpr(binary.X) && isErrorParamIdent(ctx, binary.Y, errorParams)
}

func isErrorParamIdent(ctx *analysisContext, expr ast.Expr, errorParams map[types.Object]bool) bool {
	ident, ok := unparenExpr(expr).(*ast.Ident)
	if !ok {
		return false
	}
	object := ctx.pass.TypesInfo.Uses[ident]
	if object == nil {
		object = ctx.pass.TypesInfo.Defs[ident]
	}
	return errorParams[object]
}
