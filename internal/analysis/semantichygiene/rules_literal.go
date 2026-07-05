package semantichygiene

import (
	"go/ast"
	"go/token"
	"go/types"
	"strconv"
)

func checkStableLiteral(ctx *analysisContext, lit *ast.BasicLit) {
	if lit.Kind != token.STRING {
		return
	}
	value, err := strconv.Unquote(lit.Value)
	if err != nil {
		return
	}
	if isTestFile(ctx.filename(lit.Pos())) {
		if isAllowedTestDescriptionLiteral(ctx, lit) ||
			!isStableTestContractLiteral(ctx, lit, value) {
			return
		}
		ctx.report(ruleContractRawLiteral, lit, testStableLiteralDiagnostic(value))
		return
	}
	if isStableLiteralAllowed(ctx, lit) || !isStableContractLiteral(ctx, lit, value) {
		return
	}
	ctx.report(ruleContractRawLiteral, lit, stableLiteralDiagnostic(value))
}

func checkLocalizedProseLiteral(ctx *analysisContext, lit *ast.BasicLit) {
	if lit.Kind != token.STRING {
		return
	}
	value, err := strconv.Unquote(lit.Value)
	if err != nil {
		return
	}
	if isTestFile(ctx.filename(lit.Pos())) {
		if isAllowedTestDescriptionLiteral(ctx, lit) ||
			!looksLikeLocalizedProse(value) ||
			!isTestLocalizedProseContractLiteralContext(ctx, lit) {
			return
		}
		ctx.report(ruleI18nRawProse, lit, testRawLocalizedProseDiagnostic(value))
		return
	}
	if isLocalizedProseLiteralAllowed(ctx, lit) || !isRawLocalizedProseContractLiteral(ctx, lit) {
		return
	}
	if !looksLikeLocalizedProse(value) && !isStrictLocalizedProseContractLiteral(ctx, lit, value) {
		return
	}
	ctx.report(ruleI18nRawProse, lit, rawLocalizedProseDiagnostic(value))
}

func checkTestRawSemanticLiteral(ctx *analysisContext, lit *ast.BasicLit) {
	if lit.Kind != token.STRING || !isTestFile(ctx.filename(lit.Pos())) {
		return
	}
	if isAllowedTestDescriptionLiteral(ctx, lit) {
		return
	}
	if _, err := strconv.Unquote(lit.Value); err != nil {
		return
	}
	if typeName, ok := testLiteralSemanticCallParam(ctx, lit); ok {
		ctx.report(ruleSemanticTestRawLiteral, lit, testRawSemanticParamLiteralDiagnostic(typeName))
		return
	}
	if fieldName, typeName, ok := testLiteralSemanticCompositeField(ctx, lit); ok {
		ctx.report(ruleSemanticTestRawLiteral, lit, testRawSemanticFieldLiteralDiagnostic(typeName, fieldName))
		return
	}
	if typeName, ok := testLiteralSemanticAssignedValue(ctx, lit); ok {
		ctx.report(ruleSemanticTestRawLiteral, lit, testRawSemanticAssignedLiteralDiagnostic(typeName))
	}
}

func testLiteralSemanticCallParam(ctx *analysisContext, lit *ast.BasicLit) (string, bool) {
	call, index, ok := directCallArg(ctx, lit)
	if !ok {
		return "", false
	}
	if _, ok := conversionSemanticTypeName(ctx.pass, call.Fun); ok {
		return "", false
	}
	if isBDDDescriptionCall(callName(call)) {
		return "", false
	}
	sig, ok := ctx.pass.TypesInfo.TypeOf(call.Fun).(*types.Signature)
	if !ok || sig.Params() == nil {
		return "", false
	}
	paramType, ok := signatureParamTypeAt(sig, index)
	if !ok {
		return "", false
	}
	return semanticTypeNameOf(paramType)
}

func signatureParamTypeAt(sig *types.Signature, index int) (types.Type, bool) {
	params := sig.Params()
	if params == nil || params.Len() == 0 || index < 0 {
		return nil, false
	}
	if sig.Variadic() && index >= params.Len()-1 {
		slice, ok := params.At(params.Len() - 1).Type().(*types.Slice)
		if !ok {
			return nil, false
		}
		return slice.Elem(), true
	}
	if index >= params.Len() {
		return nil, false
	}
	return params.At(index).Type(), true
}

func testLiteralSemanticCompositeField(ctx *analysisContext, lit *ast.BasicLit) (string, string, bool) {
	kv, ok := ctx.parent(lit).(*ast.KeyValueExpr)
	if !ok || kv.Value != lit {
		return "", "", false
	}
	fieldName := keyName(kv.Key)
	if fieldName == "" {
		return "", "", false
	}
	_, strct, _, ok := enclosingNamedCompositeStructLiteral(ctx, kv)
	if !ok {
		return "", "", false
	}
	for i := 0; i < strct.NumFields(); i++ {
		field := strct.Field(i)
		if field.Name() != fieldName {
			continue
		}
		typeName, ok := semanticTypeNameOf(field.Type())
		return fieldName, typeName, ok
	}
	return "", "", false
}

func testLiteralSemanticAssignedValue(ctx *analysisContext, lit *ast.BasicLit) (string, bool) {
	switch parent := ctx.parent(lit).(type) {
	case *ast.ValueSpec:
		return testLiteralSemanticValueSpecType(ctx, parent, lit)
	case *ast.AssignStmt:
		return testLiteralSemanticAssignmentType(ctx, parent, lit)
	case *ast.CompositeLit:
		if !compositeLiteralContainsDirectValue(parent, lit) {
			return "", false
		}
		return semanticCompositeLiteralValueType(ctx, parent)
	case *ast.KeyValueExpr:
		return testLiteralSemanticMapValueType(ctx, parent, lit)
	default:
		return "", false
	}
}

func testLiteralSemanticValueSpecType(ctx *analysisContext, spec *ast.ValueSpec, lit *ast.BasicLit) (string, bool) {
	for index, value := range spec.Values {
		if value != lit {
			continue
		}
		if typeName, ok := semanticTypeNameOf(ctx.pass.TypesInfo.TypeOf(spec.Type)); ok {
			return typeName, true
		}
		if index >= len(spec.Names) || spec.Names[index] == nil {
			return "", false
		}
		obj := ctx.pass.TypesInfo.Defs[spec.Names[index]]
		if obj == nil {
			return "", false
		}
		return semanticTypeNameOf(obj.Type())
	}
	return "", false
}

func testLiteralSemanticAssignmentType(ctx *analysisContext, assign *ast.AssignStmt, lit *ast.BasicLit) (string, bool) {
	for index, value := range assign.Rhs {
		if value != lit || index >= len(assign.Lhs) {
			continue
		}
		return semanticTypeNameOf(ctx.pass.TypesInfo.TypeOf(assign.Lhs[index]))
	}
	return "", false
}

func testLiteralSemanticMapValueType(ctx *analysisContext, kv *ast.KeyValueExpr, lit *ast.BasicLit) (string, bool) {
	if kv.Value != lit {
		return "", false
	}
	composite, ok := ctx.parent(kv).(*ast.CompositeLit)
	if !ok {
		return "", false
	}
	return semanticCompositeLiteralMapValueType(ctx, composite)
}

func compositeLiteralContainsDirectValue(composite *ast.CompositeLit, lit *ast.BasicLit) bool {
	for _, elt := range composite.Elts {
		if elt == lit {
			return true
		}
	}
	return false
}

func semanticCompositeLiteralValueType(ctx *analysisContext, composite *ast.CompositeLit) (string, bool) {
	typ := ctx.pass.TypesInfo.TypeOf(composite)
	if typ == nil {
		return "", false
	}
	switch underlying := typ.Underlying().(type) {
	case *types.Slice:
		return semanticTypeNameOf(underlying.Elem())
	case *types.Array:
		return semanticTypeNameOf(underlying.Elem())
	default:
		return "", false
	}
}

func semanticCompositeLiteralMapValueType(ctx *analysisContext, composite *ast.CompositeLit) (string, bool) {
	typ := ctx.pass.TypesInfo.TypeOf(composite)
	if typ == nil {
		return "", false
	}
	mapping, ok := typ.Underlying().(*types.Map)
	if !ok {
		return "", false
	}
	return semanticTypeNameOf(mapping.Elem())
}
