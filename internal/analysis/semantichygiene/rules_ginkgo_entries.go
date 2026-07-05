package semantichygiene

import (
	"go/ast"
	"go/token"
	"go/types"
)

func checkGinkgoContainerBody(ctx *analysisContext, call *ast.CallExpr) {
	body, ok := firstFuncLitArg(call)
	if !ok {
		return
	}
	for _, stmt := range body.Body.List {
		switch s := stmt.(type) {
		case *ast.AssignStmt:
			if s.Tok == token.DEFINE {
				ctx.report(ruleGinkgoContainerStateInitialization, s, ginkgoContainerStateInitializationDiagnostic())
			}
		case *ast.DeclStmt:
			if ginkgoDeclInitializesState(s) {
				ctx.report(ruleGinkgoContainerStateInitialization, s, ginkgoContainerStateInitializationDiagnostic())
			}
		case *ast.ExprStmt:
			checkGinkgoContainerExpr(ctx, s.X)
		}
	}
}

func ginkgoDeclInitializesState(stmt *ast.DeclStmt) bool {
	decl, ok := stmt.Decl.(*ast.GenDecl)
	if !ok || decl.Tok != token.VAR {
		return false
	}
	for _, spec := range decl.Specs {
		valueSpec, ok := spec.(*ast.ValueSpec)
		if ok && len(valueSpec.Values) > 0 {
			return true
		}
	}
	return false
}

func checkGinkgoContainerExpr(ctx *analysisContext, expr ast.Expr) {
	call, ok := unparenExpr(expr).(*ast.CallExpr)
	if !ok {
		return
	}
	if isAllowedGinkgoContainerConstructionCall(callName(call)) {
		return
	}
	reportDisallowedContainerCall(ctx, call)
}

func reportDisallowedContainerCall(ctx *analysisContext, root *ast.CallExpr) {
	reported := false
	ast.Inspect(root, func(node ast.Node) bool {
		if reported || node == nil {
			return false
		}
		if _, ok := node.(*ast.FuncLit); ok {
			return false
		}
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		name := callName(call)
		if !isDisallowedGinkgoContainerCall(name) {
			return true
		}
		ctx.report(ruleGinkgoContainerCall, call, ginkgoContainerCallDiagnostic(name))
		reported = true
		return false
	})
}

func checkGinkgoDecoratorArgs(ctx *analysisContext, call *ast.CallExpr) {
	for _, arg := range call.Args {
		name, ok := ginkgoDecoratorName(arg)
		if !ok {
			continue
		}
		switch name {
		case "Focus":
			ctx.report(ruleGinkgoFocus, arg, ginkgoFocusDiagnostic())
		case "Pending":
			ctx.report(ruleGinkgoPending, arg, ginkgoPendingDiagnostic())
		case "FlakeAttempts":
			ctx.report(ruleGinkgoFlakeAttempts, arg, ginkgoFlakeAttemptsDiagnostic())
		case "Serial", "Ordered", "SpecPriority":
			ctx.report(ruleGinkgoRestrictedDecorator, arg, ginkgoRestrictedDecoratorDiagnostic(name))
		}
	}
}

func ginkgoDecoratorName(expr ast.Expr) (string, bool) {
	switch e := unparenExpr(expr).(type) {
	case *ast.Ident:
		return e.Name, isGinkgoPolicyDecoratorName(e.Name)
	case *ast.SelectorExpr:
		return e.Sel.Name, isGinkgoPolicyDecoratorName(e.Sel.Name)
	case *ast.CallExpr:
		name := callName(e)
		return name, isGinkgoPolicyDecoratorName(name)
	default:
		return "", false
	}
}

func checkGinkgoEntryPolicy(ctx *analysisContext, call *ast.CallExpr) {
	if ginkgoEntryDataArgCount(ctx, call) > 4 {
		ctx.report(ruleGinkgoWideEntry, call, ginkgoWideEntryDiagnostic())
	}
	checkGinkgoSemanticEntryData(ctx, call)
	setupNames := enclosingGinkgoSetupAssignedNames(ctx, call)
	if len(setupNames) == 0 {
		return
	}
	for _, arg := range call.Args[1:] {
		for name := range identifiersInExpr(arg) {
			if setupNames[name] {
				ctx.report(ruleGinkgoEntrySetupValue, arg, ginkgoEntrySetupValueDiagnostic())
				return
			}
		}
	}
}

func ginkgoEntryDataArgCount(ctx *analysisContext, call *ast.CallExpr) int {
	count := 0
	for _, arg := range call.Args[1:] {
		if !isGinkgoEntryDecoratorArg(ctx, arg) {
			count++
		}
	}
	return count
}

func checkGinkgoSemanticEntryData(ctx *analysisContext, call *ast.CallExpr) {
	dataIndex := 0
	for _, arg := range call.Args[1:] {
		if isGinkgoEntryDecoratorArg(ctx, arg) {
			continue
		}
		lit, ok := unparenExpr(arg).(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			dataIndex++
			continue
		}
		paramName, semanticType, ok := ginkgoSemanticEntryParamType(ctx, call, dataIndex)
		if !ok {
			dataIndex++
			continue
		}
		ctx.report(ruleGinkgoSemanticEntryData, lit, ginkgoSemanticEntryDataDiagnostic(paramName, semanticType))
		dataIndex++
	}
}

func isGinkgoEntryDecoratorArg(ctx *analysisContext, expr ast.Expr) bool {
	switch e := unparenExpr(expr).(type) {
	case *ast.Ident:
		return isGinkgoEntryDecoratorName(e.Name)
	case *ast.SelectorExpr:
		return isGinkgoEntryDecoratorName(e.Sel.Name)
	case *ast.CallExpr:
		return isGinkgoEntryDecoratorName(callName(e))
	default:
		return isGinkgoEntryDecoratorType(ctx, expr)
	}
}

func isGinkgoEntryDecoratorName(name string) bool {
	switch name {
	case "Focus", "Pending", "Serial", "Ordered", "ContinueOnFailure", "OncePerOrdered",
		"Label", "Offset", "FlakeAttempts", "MustPassRepeatedly", "SpecPriority",
		"NodeTimeout", "SpecTimeout", "GracePeriod", "PollProgressAfter", "PollProgressInterval",
		"SemVerConstraint", "ComponentSemVerConstraint", "SuppressProgressReporting", "AroundNode",
		"CodeLocation":
		return true
	default:
		return false
	}
}

func isGinkgoEntryDecoratorType(ctx *analysisContext, expr ast.Expr) bool {
	named, ok := ctx.pass.TypesInfo.TypeOf(expr).(*types.Named)
	if !ok || named.Obj() == nil || named.Obj().Pkg() == nil {
		return false
	}
	return named.Obj().Pkg().Path() == "github.com/onsi/ginkgo/v2/types" &&
		named.Obj().Name() == "CodeLocation"
}

func ginkgoSemanticEntryParamType(ctx *analysisContext, call *ast.CallExpr, dataIndex int) (string, string, bool) {
	paramName, paramType, ok := bddEntryTableParam(ctx, call, dataIndex)
	if !ok {
		return "", "", false
	}
	if semanticType, ok := semanticTypeNameOf(paramType); ok {
		return paramName, semanticType, true
	}
	if !isRawStringCarrier(paramType) {
		return "", "", false
	}
	semanticType, ok := semanticTypeForTestHelperParamName(paramName, describeTableDescriptionContext(ctx, call))
	if !ok || !rawStringEntryParamIsSemantic(ctx, call, semanticType) {
		return "", "", false
	}
	return paramName, semanticType, true
}

func rawStringEntryParamIsSemantic(ctx *analysisContext, entry *ast.CallExpr, semanticType string) bool {
	if !semanticEntryTypeRequiresContext(semanticType) {
		return true
	}
	return tableHasStrongSemanticEntryParam(ctx, entry) ||
		describeTableDescriptionSuggestsContract(ctx, entry)
}

func semanticEntryTypeRequiresContext(semanticType string) bool {
	switch semanticType {
	case "ErrorCode", "FieldErrorCode", "IssueCode", "ImportErrorCode", "SectionEditErrorCode":
		return true
	default:
		return false
	}
}

func tableHasStrongSemanticEntryParam(ctx *analysisContext, entry *ast.CallExpr) bool {
	table, ok := enclosingDescribeTableCall(ctx, entry)
	if !ok {
		return false
	}
	body, ok := describeTableBody(table)
	if !ok || body.Type.Params == nil {
		return false
	}
	for _, field := range body.Type.Params.List {
		if semanticType, ok := semanticTypeNameOf(ctx.pass.TypesInfo.TypeOf(field.Type)); ok && !semanticEntryTypeRequiresContext(semanticType) {
			return true
		}
		if !isRawStringCarrier(ctx.pass.TypesInfo.TypeOf(field.Type)) {
			continue
		}
		for _, name := range field.Names {
			if name == nil {
				continue
			}
			semanticType, ok := semanticTypeForTestHelperParamName(name.Name, describeTableDescriptionContext(ctx, entry))
			if ok && !semanticEntryTypeRequiresContext(semanticType) {
				return true
			}
		}
	}
	return false
}

func describeTableDescriptionSuggestsContract(ctx *analysisContext, entry *ast.CallExpr) bool {
	return nameSuggestsTestContract(describeTableDescriptionContext(ctx, entry))
}

func describeTableDescriptionContext(ctx *analysisContext, entry *ast.CallExpr) string {
	table, ok := enclosingDescribeTableCall(ctx, entry)
	if !ok || len(table.Args) == 0 {
		return ""
	}
	description, ok := ginkgoStaticDescription(table.Args[0])
	if !ok {
		return ""
	}
	return description
}

func enclosingGinkgoSetupAssignedNames(ctx *analysisContext, node ast.Node) map[string]bool {
	block := enclosingGinkgoContainerBlock(ctx, node)
	if block == nil {
		return nil
	}
	names := map[string]bool{}
	for _, stmt := range block.List {
		expr, ok := stmt.(*ast.ExprStmt)
		if !ok {
			continue
		}
		call, ok := unparenExpr(expr.X).(*ast.CallExpr)
		if !ok || !isGinkgoSetupNodeName(callName(call)) {
			continue
		}
		body, ok := firstFuncLitArg(call)
		if !ok {
			continue
		}
		collectAssignedIdentifiers(body.Body, names)
	}
	return names
}

func enclosingGinkgoContainerBlock(ctx *analysisContext, node ast.Node) *ast.BlockStmt {
	for current := node; current != nil; current = ctx.parent(current) {
		block, ok := current.(*ast.BlockStmt)
		if !ok {
			if _, ok := current.(*ast.FuncDecl); ok {
				return nil
			}
			continue
		}
		fn, ok := ctx.parent(block).(*ast.FuncLit)
		if !ok {
			continue
		}
		call, ok := ctx.parent(fn).(*ast.CallExpr)
		if ok && isGinkgoContainerNodeName(callName(call)) {
			return block
		}
	}
	return nil
}

func collectAssignedIdentifiers(body *ast.BlockStmt, names map[string]bool) {
	ast.Inspect(body, func(node ast.Node) bool {
		if node == nil {
			return false
		}
		if _, ok := node.(*ast.FuncLit); ok {
			return false
		}
		switch n := node.(type) {
		case *ast.AssignStmt:
			for _, lhs := range n.Lhs {
				if ident, ok := unparenExpr(lhs).(*ast.Ident); ok && ident.Name != "_" {
					names[ident.Name] = true
				}
			}
		case *ast.ValueSpec:
			for _, ident := range n.Names {
				if ident != nil && ident.Name != "_" {
					names[ident.Name] = true
				}
			}
		}
		return true
	})
}

func identifiersInExpr(expr ast.Expr) map[string]bool {
	names := map[string]bool{}
	ast.Inspect(expr, func(node ast.Node) bool {
		ident, ok := node.(*ast.Ident)
		if ok {
			names[ident.Name] = true
		}
		return true
	})
	return names
}
