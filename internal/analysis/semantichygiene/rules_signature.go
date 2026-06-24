package semantichygiene

import (
	"go/ast"
	"go/types"
	"strings"
)

func checkTypeSpec(ctx *analysisContext, spec *ast.TypeSpec) {
	checkStructFields(ctx, spec)
	checkInterfaceSignatures(ctx, spec)
}

func checkSignature(ctx *analysisContext, fn *ast.FuncDecl) {
	filename := ctx.filename(fn.Pos())
	if isAllowedSignatureFile(filename) {
		return
	}
	checkLocalizedProseSinkSignature(ctx, fn)
	checkSignatureParams(ctx, fn.Name.Name, semanticContextName(fn), fn.Type.Params)
}

func checkLocalizedProseSinkSignature(ctx *analysisContext, fn *ast.FuncDecl) {
	if !localizedProseSignatureSinkCalls[fn.Name.Name] || fn.Type.Params == nil {
		return
	}
	for _, field := range fn.Type.Params.List {
		if !isString(ctx.pass.TypesInfo.TypeOf(field.Type)) {
			continue
		}
		for _, name := range field.Names {
			if name == nil || canonicalName(name.Name) != "message" {
				continue
			}
			ctx.pass.Reportf(name.Pos(), "%s", localizedProseSinkSignatureDiagnostic(fn.Name.Name))
		}
	}
}

func checkSignatureParams(ctx *analysisContext, funcName string, semanticContext string, params *ast.FieldList) {
	if params == nil {
		return
	}
	rawStringOrdinal := 0
	for _, field := range params.List {
		if !isRawStringCarrier(ctx.pass.TypesInfo.TypeOf(field.Type)) {
			checkPrimitiveSignatureField(ctx, field, funcName, semanticContext)
			continue
		}
		checkStringSignatureField(ctx, field, funcName, semanticContext, rawStringOrdinal)
		rawStringOrdinal++
	}
}

func checkStringSignatureField(ctx *analysisContext, field *ast.Field, funcName string, semanticContext string, rawStringOrdinal int) {
	if !isSemanticBoundaryFunc(funcName) {
		return
	}
	if len(field.Names) == 0 {
		paramName, typeName, ok := semanticTypeForUnnamedParam(funcName, semanticContext, rawStringOrdinal)
		if !ok {
			return
		}
		ctx.pass.Reportf(field.Type.Pos(), "%s", parameterDiagnostic(paramName, funcName, typeName))
		return
	}
	for _, name := range field.Names {
		if name == nil {
			continue
		}
		typeName, ok := semanticTypeForParamName(name.Name, semanticContext)
		if !ok {
			continue
		}
		ctx.pass.Reportf(name.Pos(), "%s", parameterDiagnostic(name.Name, funcName, typeName))
	}
}

func checkPrimitiveSignatureField(ctx *analysisContext, field *ast.Field, funcName string, semanticContext string) {
	primitiveType, ok := primitiveCarrierTypeName(ctx.pass.TypesInfo.TypeOf(field.Type))
	if !ok {
		return
	}
	for _, name := range field.Names {
		if name == nil || !semanticPrimitiveNameInContext(name.Name, semanticContext) {
			continue
		}
		ctx.pass.Reportf(name.Pos(), "%s", primitiveParameterDiagnostic(name.Name, funcName, primitiveType))
	}
}

func checkInterfaceSignatures(ctx *analysisContext, spec *ast.TypeSpec) {
	filename := ctx.filename(spec.Pos())
	if isAllowedSignatureFile(filename) {
		return
	}
	iface, ok := spec.Type.(*ast.InterfaceType)
	if !ok {
		return
	}
	for _, method := range iface.Methods.List {
		methodType, ok := method.Type.(*ast.FuncType)
		if !ok {
			continue
		}
		for _, name := range method.Names {
			if name == nil {
				continue
			}
			checkSignatureParams(ctx, name.Name, spec.Name.Name+name.Name, methodType.Params)
		}
	}
}

func semanticTypeForUnnamedParam(funcName string, semanticContext string, rawStringOrdinal int) (string, string, bool) {
	if rawStringOrdinal != 0 {
		return "", "", false
	}
	canonicalFunc := canonicalName(funcName)
	canonicalContext := canonicalName(semanticContext)
	if strings.Contains(canonicalFunc, "commit") ||
		strings.Contains(canonicalContext, "revisionstore") ||
		strings.HasPrefix(canonicalFunc, "changedmarkdown") ||
		strings.HasPrefix(canonicalFunc, "filesat") {
		return "commitHash", "CommitHash", true
	}
	return "", "", false
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
	messageBearing := astStructIsMessageBearing(spec.Name.Name, strct)
	hasMessageID := astStructHasField(strct, "messageID")
	for _, field := range strct.Fields.List {
		if primitiveType, ok := primitiveCarrierTypeName(ctx.pass.TypesInfo.TypeOf(field.Type)); ok {
			if isAllowedWireStructField(ctx, spec, field) {
				continue
			}
			for _, name := range field.Names {
				if name == nil || !semanticPrimitiveNameInContext(name.Name, spec.Name.Name) {
					continue
				}
				ctx.pass.Reportf(name.Pos(), "%s", primitiveFieldDiagnostic(name.Name, spec.Name.Name, primitiveType))
			}
			continue
		}
		if !isRawStringCarrier(ctx.pass.TypesInfo.TypeOf(field.Type)) {
			continue
		}
		for _, name := range field.Names {
			if name == nil {
				continue
			}
			if messageBearing && !hasMessageID {
				if messageFieldName(name.Name) {
					ctx.pass.Reportf(name.Pos(), "%s", messageFieldDiagnostic(name.Name, spec.Name.Name))
					continue
				}
				if warningStringsFieldName(name.Name) {
					ctx.pass.Reportf(name.Pos(), "%s", warningStringsFieldDiagnostic(name.Name, spec.Name.Name))
					continue
				}
			}
			if isAllowedWireStructField(ctx, spec, field) {
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
	return strings.HasPrefix(lower, "bind") ||
		strings.HasPrefix(lower, "changed") ||
		strings.HasPrefix(lower, "files") ||
		strings.HasPrefix(lower, "get") ||
		strings.HasPrefix(lower, "heartbeat") ||
		strings.HasPrefix(lower, "list") ||
		strings.HasPrefix(lower, "load") ||
		strings.HasPrefix(lower, "find") ||
		strings.HasPrefix(lower, "mark") ||
		strings.HasPrefix(lower, "record") ||
		strings.HasPrefix(lower, "release") ||
		strings.HasPrefix(lower, "resolve") ||
		strings.HasPrefix(lower, "update") ||
		strings.HasPrefix(lower, "create") ||
		strings.HasPrefix(lower, "delete") ||
		strings.HasPrefix(lower, "move") ||
		strings.HasPrefix(lower, "save") ||
		strings.HasPrefix(lower, "search") ||
		strings.HasPrefix(lower, "sort") ||
		strings.HasPrefix(lower, "ensure") ||
		strings.HasPrefix(lower, "restore") ||
		strings.HasPrefix(lower, "handle") ||
		strings.HasPrefix(lower, "unbind")
}
