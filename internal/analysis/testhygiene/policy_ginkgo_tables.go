package testhygiene

import (
	"fmt"
	"go/ast"
	"go/types"
)

func isBDDEntryCall(name string) bool {
	switch name {
	case "Entry", "FEntry", "PEntry", "XEntry":
		return true
	default:
		return false
	}
}

func bddEntryTableParam(ctx *analysisContext, entry *ast.CallExpr, dataIndex int) (string, types.Type, bool) {
	table, ok := enclosingDescribeTableCall(ctx, entry)
	if !ok {
		return "", nil, false
	}
	body, ok := describeTableBody(table)
	if !ok || body.Type.Params == nil {
		return "", nil, false
	}
	current := 0
	for _, field := range body.Type.Params.List {
		if len(field.Names) == 0 {
			if current == dataIndex {
				return fmt.Sprintf("param%d", current+1), ctx.pass.TypesInfo.TypeOf(field.Type), true
			}
			current++
			continue
		}
		for _, name := range field.Names {
			if current == dataIndex {
				return bddEntryTableParamDisplayName(name, current), ctx.pass.TypesInfo.TypeOf(field.Type), true
			}
			current++
		}
	}
	return "", nil, false
}

func bddEntryTableParamDisplayName(name *ast.Ident, index int) string {
	if name == nil || name.Name == "_" {
		return fmt.Sprintf("param%d", index+1)
	}
	return name.Name
}

func enclosingDescribeTableCall(ctx *analysisContext, entry *ast.CallExpr) (*ast.CallExpr, bool) {
	for current := ctx.parent(entry); current != nil; current = ctx.parent(current) {
		switch n := current.(type) {
		case *ast.CallExpr:
			if isDescribeTableCall(callName(n)) {
				return n, true
			}
		case *ast.FuncDecl:
			return nil, false
		}
	}
	return nil, false
}

func isDescribeTableCall(name string) bool {
	switch name {
	case "DescribeTable", "FDescribeTable", "PDescribeTable", "XDescribeTable":
		return true
	default:
		return false
	}
}

func describeTableBody(call *ast.CallExpr) (*ast.FuncLit, bool) {
	for _, arg := range call.Args {
		body, ok := arg.(*ast.FuncLit)
		if ok {
			return body, true
		}
	}
	return nil, false
}

func isRawStringCarrier(typ types.Type) bool {
	if typ == nil {
		return false
	}
	if isBuiltinString(typ) {
		return true
	}
	switch underlying := typ.Underlying().(type) {
	case *types.Slice:
		return isBuiltinString(underlying.Elem())
	case *types.Array:
		return isBuiltinString(underlying.Elem())
	case *types.Map:
		return isBuiltinString(underlying.Key())
	default:
		return false
	}
}

func isBuiltinString(typ types.Type) bool {
	basic, ok := typ.(*types.Basic)
	return ok && basic.Kind() == types.String
}
