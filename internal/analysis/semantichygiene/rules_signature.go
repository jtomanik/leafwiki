package semantichygiene

import (
	"go/ast"
	"go/types"
	"strings"
)

func checkSignature(ctx *analysisContext, fn *ast.FuncDecl) {
	filename := ctx.filename(fn.Pos())
	if isAllowedSignatureFile(filename) || !isSemanticBoundaryFunc(fn.Name.Name) {
		return
	}
	if fn.Type.Params == nil {
		return
	}
	for _, field := range fn.Type.Params.List {
		if !isRawStringCarrier(ctx.pass.TypesInfo.TypeOf(field.Type)) {
			continue
		}
		for _, name := range field.Names {
			if name == nil {
				continue
			}
			typeName, ok := semanticTypeForParamName(name.Name, semanticContextName(fn))
			if !ok {
				continue
			}
			ctx.pass.Reportf(name.Pos(), "%s", parameterDiagnostic(name.Name, fn.Name.Name, typeName))
		}
	}
}

func semanticContextName(fn *ast.FuncDecl) string {
	if fn == nil {
		return ""
	}
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return fn.Name.Name
	}
	return exprName(fn.Recv.List[0].Type) + fn.Name.Name
}

func checkStructFields(ctx *analysisContext, spec *ast.TypeSpec) {
	filename := ctx.filename(spec.Pos())
	if isAllowedStructFieldFile(filename) || isPersistenceRowStruct(ctx, spec) {
		return
	}
	strct, ok := spec.Type.(*ast.StructType)
	if !ok {
		return
	}
	for _, field := range strct.Fields.List {
		if !isRawStringCarrier(ctx.pass.TypesInfo.TypeOf(field.Type)) {
			continue
		}
		if isAllowedWireStructField(ctx, spec, field) {
			continue
		}
		for _, name := range field.Names {
			if name == nil {
				continue
			}
			typeName, ok := semanticTypeForFieldName(name.Name, spec.Name.Name)
			if !ok {
				continue
			}
			ctx.pass.Reportf(name.Pos(), "%s", fieldDiagnostic(name.Name, spec.Name.Name, typeName))
		}
	}
}

func checkValidatorReturn(ctx *analysisContext, fn *ast.FuncDecl) {
	if fn.Type.Results == nil || !strings.HasPrefix(fn.Name.Name, "Validate") {
		return
	}
	if isTestOrGeneratedFile(ctx.filename(fn.Pos())) {
		return
	}
	semanticName, typeName, ok := validatorSemanticTarget(fn)
	if !ok {
		return
	}
	for _, result := range fn.Type.Results.List {
		if typ := ctx.pass.TypesInfo.TypeOf(result.Type); typ != nil && isPrimitiveStringResult(typ) {
			ctx.pass.Reportf(result.Pos(), "%s", validatorReturnDiagnostic(fn.Name.Name, semanticName, typeName))
			return
		}
	}
}

func validatorSemanticTarget(fn *ast.FuncDecl) (string, string, bool) {
	for _, param := range fn.Type.Params.List {
		for _, name := range param.Names {
			if name != nil && semanticName(name.Name) {
				return name.Name, semanticTypeForName(name.Name), true
			}
		}
	}
	fromFuncName := strings.TrimPrefix(fn.Name.Name, "Validate")
	if typ, ok := semanticTypeForCanonicalName(canonicalName(fromFuncName)); ok {
		return fromFuncName, typ, true
	}
	return "", "", false
}

func isPrimitiveStringResult(typ types.Type) bool {
	basic, ok := typ.(*types.Basic)
	return ok && basic.Kind() == types.String
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

func isSemanticBoundaryFunc(name string) bool {
	lower := strings.ToLower(name)
	return strings.HasPrefix(lower, "get") ||
		strings.HasPrefix(lower, "list") ||
		strings.HasPrefix(lower, "load") ||
		strings.HasPrefix(lower, "find") ||
		strings.HasPrefix(lower, "resolve") ||
		strings.HasPrefix(lower, "update") ||
		strings.HasPrefix(lower, "create") ||
		strings.HasPrefix(lower, "delete") ||
		strings.HasPrefix(lower, "move") ||
		strings.HasPrefix(lower, "sort") ||
		strings.HasPrefix(lower, "ensure") ||
		strings.HasPrefix(lower, "restore") ||
		strings.HasPrefix(lower, "handle")
}
