package testhygiene

import (
	"go/ast"
	"go/token"
	"go/types"
	"strconv"
)

func keyName(expr ast.Expr) string {
	if lit, ok := expr.(*ast.BasicLit); ok && lit.Kind == token.STRING {
		if value, err := strconv.Unquote(lit.Value); err == nil {
			return value
		}
	}
	return exprName(expr)
}

func calleePackageAndName(ctx *analysisContext, call *ast.CallExpr) (string, string) {
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		if fn, ok := ctx.pass.TypesInfo.Uses[fun].(*types.Func); ok && fn.Pkg() != nil {
			return fn.Pkg().Path(), fun.Name
		}
		return "", fun.Name
	case *ast.SelectorExpr:
		if fn, ok := ctx.pass.TypesInfo.Uses[fun.Sel].(*types.Func); ok && fn.Pkg() != nil {
			return fn.Pkg().Path(), fun.Sel.Name
		}
		return "", fun.Sel.Name
	default:
		return "", ""
	}
}
