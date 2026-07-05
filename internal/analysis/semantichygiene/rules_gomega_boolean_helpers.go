package semantichygiene

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"
)

func callLaundersBooleanToStringState(ctx *analysisContext, call *ast.CallExpr) bool {
	if !isString(ctx.pass.TypesInfo.TypeOf(call)) || !testLocalBooleanStateHelperCall(ctx, call) {
		return false
	}
	for _, arg := range call.Args {
		if isBoolType(ctx.pass.TypesInfo.TypeOf(arg)) {
			return true
		}
	}
	return testLocalBooleanStateHelperBranchesOnPredicate(ctx, call)
}

func testLocalBooleanStateHelperCall(ctx *analysisContext, call *ast.CallExpr) bool {
	name := strings.ToLower(callName(call))
	if !strings.Contains(name, "state") && !strings.Contains(name, "status") && !strings.Contains(name, "label") && !strings.Contains(name, "scope") {
		return false
	}
	ident, ok := unparenExpr(call.Fun).(*ast.Ident)
	if !ok {
		return false
	}
	obj := ctx.pass.TypesInfo.ObjectOf(ident)
	if obj == nil {
		return false
	}
	filename := ctx.filename(obj.Pos())
	return isTestFile(filename) || isTestSupportFile(filename)
}

func testLocalBooleanStateHelperBranchesOnPredicate(ctx *analysisContext, call *ast.CallExpr) bool {
	fn := testLocalBooleanStateHelperDecl(ctx, call)
	if fn == nil || fn.Body == nil {
		return false
	}
	found := false
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		if found || node == nil {
			return false
		}
		if _, ok := node.(*ast.FuncLit); ok {
			return false
		}
		ifStmt, ok := node.(*ast.IfStmt)
		if !ok {
			return true
		}
		if exprTreeContainsBoolCall(ctx, ifStmt.Cond) {
			found = true
			return false
		}
		return true
	})
	return found
}

func testLocalBooleanStateHelperDecl(ctx *analysisContext, call *ast.CallExpr) *ast.FuncDecl {
	if !testLocalBooleanStateHelperCall(ctx, call) {
		return nil
	}
	ident, ok := unparenExpr(call.Fun).(*ast.Ident)
	if !ok {
		return nil
	}
	target := ctx.pass.TypesInfo.ObjectOf(ident)
	if target == nil {
		return nil
	}
	for _, file := range ctx.pass.Files {
		var found *ast.FuncDecl
		ast.Inspect(file, func(node ast.Node) bool {
			if found != nil || node == nil {
				return false
			}
			fn, ok := node.(*ast.FuncDecl)
			if !ok || fn.Name == nil {
				return true
			}
			if ctx.pass.TypesInfo.ObjectOf(fn.Name) == target {
				found = fn
				return false
			}
			return true
		})
		if found != nil {
			return found
		}
	}
	return nil
}

func exprTreeContainsBoolCall(ctx *analysisContext, expr ast.Expr) bool {
	found := false
	ast.Inspect(unparenExpr(expr), func(node ast.Node) bool {
		if found || node == nil {
			return false
		}
		if _, ok := node.(*ast.FuncLit); ok {
			return false
		}
		call, ok := node.(*ast.CallExpr)
		if ok && isBoolType(ctx.pass.TypesInfo.TypeOf(call)) {
			found = true
			return false
		}
		return true
	})
	return found
}

func identIsCommaOKResult(ctx *analysisContext, ident *ast.Ident) bool {
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
		assign, ok := node.(*ast.AssignStmt)
		if !ok || assign.Pos() > ident.Pos() || len(assign.Lhs) != 2 || len(assign.Rhs) != 1 {
			return true
		}
		lhsIdent, ok := unparenExpr(assign.Lhs[1]).(*ast.Ident)
		if !ok || !sameIdentifierObject(ctx, lhsIdent, ident, targetObject) {
			return true
		}
		found = exprIsCommaOKSource(ctx, assign.Rhs[0])
		return !found
	})
	return found
}

func identIsSemanticBooleanResult(ctx *analysisContext, ident *ast.Ident) bool {
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
			if candidate.Pos() > ident.Pos() || len(candidate.Rhs) != 1 {
				return true
			}
			found = semanticBoolAssignmentNamesIdent(ctx, candidate.Lhs, candidate.Rhs[0], ident, targetObject)
			return !found
		case *ast.ValueSpec:
			if candidate.Pos() > ident.Pos() || len(candidate.Values) != 1 {
				return true
			}
			lhs := make([]ast.Expr, 0, len(candidate.Names))
			for _, name := range candidate.Names {
				lhs = append(lhs, name)
			}
			found = semanticBoolAssignmentNamesIdent(ctx, lhs, candidate.Values[0], ident, targetObject)
			return !found
		}
		return true
	})
	return found
}

func semanticBoolAssignmentNamesIdent(ctx *analysisContext, lhs []ast.Expr, rhs ast.Expr, ident *ast.Ident, targetObject types.Object) bool {
	call, ok := unparenExpr(rhs).(*ast.CallExpr)
	if !ok {
		return false
	}
	results, ok := ctx.pass.TypesInfo.TypeOf(call).(*types.Tuple)
	if !ok {
		return false
	}
	for i, lhsExpr := range lhs {
		if i >= results.Len() || !isBoolType(results.At(i).Type()) {
			continue
		}
		lhsIdent, ok := unparenExpr(lhsExpr).(*ast.Ident)
		if !ok || !sameIdentifierObject(ctx, lhsIdent, ident, targetObject) {
			continue
		}
		return callReturnsSemanticBoolean(ctx, call, results, i)
	}
	return false
}

func exprTreeContainsIdent(expr ast.Expr, predicate func(*ast.Ident) bool) bool {
	found := false
	ast.Inspect(unparenExpr(expr), func(node ast.Node) bool {
		if found || node == nil {
			return false
		}
		if _, ok := node.(*ast.FuncLit); ok {
			return false
		}
		ident, ok := node.(*ast.Ident)
		if ok && predicate(ident) {
			found = true
			return false
		}
		return true
	})
	return found
}

func compositeActualContainsIdent(expr ast.Expr, predicate func(*ast.Ident) bool) bool {
	switch actual := unparenExpr(expr).(type) {
	case *ast.CompositeLit:
		return exprTreeContainsIdent(actual, predicate)
	case *ast.UnaryExpr:
		if actual.Op == token.AND {
			return compositeActualContainsIdent(actual.X, predicate)
		}
	}
	return false
}

func exprIsCommaOKSource(ctx *analysisContext, expr ast.Expr) bool {
	switch source := unparenExpr(expr).(type) {
	case *ast.TypeAssertExpr:
		return true
	case *ast.IndexExpr:
		return typeIsMap(ctx.pass.TypesInfo.TypeOf(source.X))
	case *ast.UnaryExpr:
		return source.Op == token.ARROW
	default:
		return false
	}
}

func isComparisonOp(op token.Token) bool {
	switch op {
	case token.EQL, token.NEQ, token.LSS, token.LEQ, token.GTR, token.GEQ:
		return true
	default:
		return false
	}
}

func isBooleanProducingBinaryOp(op token.Token) bool {
	return isComparisonOp(op) || op == token.LAND || op == token.LOR
}

func assertionUsesMapIndexEqual(ctx *analysisContext, assertion gomegaAssertion) bool {
	if mapIndexAssertionActualIsMap(ctx, assertion.actual) {
		return true
	}
	ident, ok := unparenExpr(assertion.actual).(*ast.Ident)
	return ok && identAliasesMapIndex(ctx, ident)
}

func mapIndexAssertionActualIsMap(ctx *analysisContext, expr ast.Expr) bool {
	index, ok := mapIndexAssertionActual(expr)
	return ok && typeIsMap(ctx.pass.TypesInfo.TypeOf(index.X))
}

func mapIndexAssertionActual(expr ast.Expr) (*ast.IndexExpr, bool) {
	switch actual := unparenExpr(expr).(type) {
	case *ast.IndexExpr:
		return actual, true
	case *ast.TypeAssertExpr:
		return mapIndexAssertionActual(actual.X)
	default:
		return nil, false
	}
}

func identAliasesMapIndex(ctx *analysisContext, ident *ast.Ident) bool {
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
				if !ok || !sameIdentifierObject(ctx, lhsIdent, ident, targetObject) {
					continue
				}
				if len(candidate.Rhs) == 1 {
					found = i == 0 && mapIndexAssertionActualIsMap(ctx, candidate.Rhs[0])
				} else if i < len(candidate.Rhs) {
					found = mapIndexAssertionActualIsMap(ctx, candidate.Rhs[i])
				}
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
				found = mapIndexAssertionActualIsMap(ctx, candidate.Values[i])
				if found {
					return false
				}
			}
		}
		return true
	})
	return found
}

func typeIsMap(typ types.Type) bool {
	if typ == nil {
		return false
	}
	_, ok := types.Unalias(typ).Underlying().(*types.Map)
	return ok
}

func typeIsSliceOrArray(typ types.Type) bool {
	if typ == nil {
		return false
	}
	switch types.Unalias(typ).Underlying().(type) {
	case *types.Slice, *types.Array:
		return true
	default:
		return false
	}
}
