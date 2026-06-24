package semantichygiene

import (
	"go/ast"
	"go/types"
)

func checkMessageFieldValue(ctx *analysisContext, kv *ast.KeyValueExpr) {
	if isTestOrGeneratedFile(ctx.filename(kv.Pos())) {
		return
	}
	if checkResponseStatusForward(ctx, kv) {
		return
	}
	fieldName := keyName(kv.Key)
	switch {
	case messageFieldName(fieldName):
		if isMessageFieldValueFreeFormPassthrough(ctx, kv) {
			named, _, ok := enclosingNamedCompositeStruct(ctx, kv)
			if !ok {
				return
			}
			ctx.pass.Reportf(kv.Key.Pos(), "%s", messageFieldPassthroughDiagnostic(fieldName, named.Obj().Name()))
			return
		}
		if !isMessageFieldValueMissingMessageID(ctx, kv) {
			return
		}
		named, _, ok := enclosingNamedCompositeStruct(ctx, kv)
		if !ok {
			return
		}
		ctx.pass.Reportf(kv.Key.Pos(), "%s", messageFieldValueDiagnostic(fieldName, named.Obj().Name()))
	case warningStringsFieldName(fieldName):
		if !isWarningFieldValueMissingMessageID(ctx, kv) {
			return
		}
		named, _, ok := enclosingNamedCompositeStruct(ctx, kv)
		if !ok {
			return
		}
		ctx.pass.Reportf(kv.Key.Pos(), "%s", warningFieldValueDiagnostic(fieldName, named.Obj().Name()))
	default:
		return
	}
}

func checkMessagePassthroughCall(ctx *analysisContext, call *ast.CallExpr) {
	if isTestOrGeneratedFile(ctx.filename(call.Pos())) {
		return
	}
	name := callName(call)
	if !localizedProseConstructorCalls[name] || !callHasFreeFormMessageArg(ctx, call) {
		return
	}
	ctx.pass.Reportf(call.Fun.Pos(), "%s", localizedErrorConstructorPassthroughDiagnostic(name))
}

func checkResponseStatusForward(ctx *analysisContext, kv *ast.KeyValueExpr) bool {
	fieldName := keyName(kv.Key)
	if !responseMessageStatusFieldName(fieldName) {
		return false
	}
	if !isResponsePayloadLiteral(ctx, kv) || !exprSuggestsMessageStatusForward(kv.Value) {
		return false
	}
	ctx.pass.Reportf(kv.Key.Pos(), "%s", responseStatusForwardDiagnostic(fieldName))
	return true
}

func isMessageFieldValueMissingMessageID(ctx *analysisContext, node ast.Node) bool {
	named, strct, lit, ok := enclosingNamedCompositeStructLiteral(ctx, node)
	return ok && namedStructIsMessageBearing(named, strct) && !compositeLiteralHasKey(lit, "messageID")
}

func isMessageFieldValueFreeFormPassthrough(ctx *analysisContext, kv *ast.KeyValueExpr) bool {
	named, strct, lit, ok := enclosingNamedCompositeStructLiteral(ctx, kv)
	return ok &&
		namedStructIsMessageBearing(named, strct) &&
		compositeLiteralHasKey(lit, "messageID") &&
		exprIsFreeFormMessageParam(ctx, kv.Value)
}

func isWarningFieldValueMissingMessageID(ctx *analysisContext, node ast.Node) bool {
	named, strct, lit, ok := enclosingNamedCompositeStructLiteral(ctx, node)
	return ok && namedStructIsMessageBearing(named, strct) && !compositeLiteralHasKey(lit, "messageID")
}

func compositeLiteralHasKey(lit *ast.CompositeLit, fieldName string) bool {
	if lit == nil {
		return false
	}
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		if canonicalName(keyName(kv.Key)) == canonicalName(fieldName) {
			return true
		}
	}
	return false
}

func callHasFreeFormMessageArg(ctx *analysisContext, call *ast.CallExpr) bool {
	for _, arg := range call.Args {
		if exprIsFreeFormMessageParam(ctx, arg) {
			return true
		}
	}
	return false
}

func exprIsFreeFormMessageParam(ctx *analysisContext, expr ast.Expr) bool {
	ident, ok := expr.(*ast.Ident)
	if !ok || canonicalName(ident.Name) != "message" {
		return false
	}
	obj := ctx.pass.TypesInfo.Uses[ident]
	if obj == nil {
		obj = ctx.pass.TypesInfo.Defs[ident]
	}
	param, ok := obj.(*types.Var)
	if !ok || !isString(param.Type()) {
		return false
	}
	fn := enclosingFunc(ctx, ident)
	if fn == nil || fn.Type.Params == nil {
		return false
	}
	for _, field := range fn.Type.Params.List {
		for _, name := range field.Names {
			if name != nil && ctx.pass.TypesInfo.Defs[name] == param {
				return true
			}
		}
	}
	return false
}

func responseMessageStatusFieldName(name string) bool {
	canonical := canonicalName(name)
	return canonical == "lasterror" || canonical == "validationerrors"
}

func exprSuggestsMessageStatusForward(expr ast.Expr) bool {
	switch e := expr.(type) {
	case *ast.Ident:
		return responseMessageStatusFieldName(e.Name)
	case *ast.SelectorExpr:
		return responseMessageStatusFieldName(e.Sel.Name)
	case *ast.CallExpr:
		for _, arg := range e.Args {
			if exprSuggestsMessageStatusForward(arg) {
				return true
			}
		}
		return false
	default:
		return false
	}
}
