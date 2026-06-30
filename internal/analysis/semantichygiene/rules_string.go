package semantichygiene

import (
	"go/ast"
	"go/token"
	"strings"
)

func checkStringLeak(ctx *analysisContext, call *ast.CallExpr) {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != "String" || len(call.Args) != 0 {
		return
	}
	typeName, ok := semanticExprTypeName(ctx.pass, selector.X)
	if !ok {
		return
	}
	reportSemanticStringEscape(ctx, call, typeName)
}

func checkStringConversionLeak(ctx *analysisContext, call *ast.CallExpr) {
	if len(call.Args) != 1 || !isBuiltinStringConversion(ctx, call) {
		return
	}
	typeName, ok := semanticExprTypeName(ctx.pass, call.Args[0])
	if !ok {
		return
	}
	if isSemanticStringMethod(ctx, call, typeName) {
		return
	}
	reportSemanticStringEscape(ctx, call, typeName)
}

func reportSemanticStringEscape(ctx *analysisContext, expr ast.Expr, typeName string) {
	if isAllowedStringBoundaryFile(ctx.filename(expr.Pos())) {
		return
	}
	if isAllowedSemanticOwnerAdapterFunc(ctx, expr, typeName) {
		return
	}
	for current := ctx.parent(expr); current != nil; current = ctx.parent(current) {
		switch p := current.(type) {
		case *ast.ParenExpr:
			continue
		case *ast.BinaryExpr:
			if handleStringBinaryEscape(ctx, expr, typeName, p) {
				return
			}
			if p.Op == token.ADD {
				continue
			}
		case *ast.CallExpr:
			if handleStringCallEscape(ctx, expr, typeName, p) {
				return
			}
			if isAllowedTerminalStringCall(ctx, p) {
				continue
			}
		case *ast.AssignStmt:
			checkStringAssignment(ctx, expr, typeName, p)
			return
		case *ast.ValueSpec:
			checkStringValueSpec(ctx, expr, typeName, p)
			return
		case *ast.KeyValueExpr:
			checkStringKeyValue(ctx, expr, typeName, p)
			return
		case *ast.CompositeLit:
			ctx.pass.Reportf(expr.Pos(), "%s", stringFieldDiagnostic(typeName, "composite literal"))
			return
		case *ast.IndexExpr:
			ctx.pass.Reportf(expr.Pos(), "%s", stringLocalDiagnostic(typeName, "map index"))
			return
		case *ast.ReturnStmt:
			if isAllowedAdapterStringReturn(ctx, expr) ||
				isAllowedErrorInterfaceStringReturn(ctx, expr) {
				return
			}
			checkStringReturn(ctx, expr, typeName)
			return
		case *ast.FuncDecl:
			return
		}
	}
}

func handleStringBinaryEscape(ctx *analysisContext, expr ast.Expr, typeName string, binary *ast.BinaryExpr) bool {
	if binary.Op != token.EQL && binary.Op != token.NEQ {
		return false
	}
	if !isAllowedSerializedTestComparison(ctx, expr, binary) && !isEmptyOrRootString(binary.X) && !isEmptyOrRootString(binary.Y) {
		ctx.pass.Reportf(expr.Pos(), "%s", stringComparisonDiagnostic(typeName))
	}
	return true
}

func handleStringCallEscape(ctx *analysisContext, expr ast.Expr, typeName string, call *ast.CallExpr) bool {
	if isAllowedSemanticStringConstructorCall(ctx, call, typeName) ||
		isAllowedSemanticConstructorTransform(ctx, call, typeName) ||
		isAllowedTestStringCall(ctx, call) {
		return true
	}
	if isAllowedTerminalStringCall(ctx, call) {
		return isAllowedTerminalCallBoundary(ctx, call)
	}
	ctx.pass.Reportf(expr.Pos(), "%s", stringCallDiagnostic(typeName, callName(call)))
	return true
}

func checkStringAssignment(ctx *analysisContext, expr ast.Expr, typeName string, stmt *ast.AssignStmt) {
	if isAllowedStringBoundaryFile(ctx.filename(expr.Pos())) {
		return
	}
	for i, rhs := range stmt.Rhs {
		if !containsNode(rhs, expr) || i >= len(stmt.Lhs) {
			continue
		}
		fieldName := exprName(stmt.Lhs[i])
		if fieldName == "" {
			continue
		}
		if semanticName(fieldName) {
			ctx.pass.Reportf(expr.Pos(), "%s", stringFieldDiagnostic(typeName, fieldName))
			continue
		}
		ctx.pass.Reportf(expr.Pos(), "%s", stringLocalDiagnostic(typeName, fieldName))
	}
	for _, lhs := range stmt.Lhs {
		if containsNode(lhs, expr) {
			ctx.pass.Reportf(expr.Pos(), "%s", stringLocalDiagnostic(typeName, "map index"))
			return
		}
	}
}

func checkStringValueSpec(ctx *analysisContext, expr ast.Expr, typeName string, spec *ast.ValueSpec) {
	if isAllowedStringBoundaryFile(ctx.filename(expr.Pos())) {
		return
	}
	for i, value := range spec.Values {
		if !containsNode(value, expr) || i >= len(spec.Names) {
			continue
		}
		ctx.pass.Reportf(expr.Pos(), "%s", stringLocalDiagnostic(typeName, spec.Names[i].Name))
	}
}

func checkStringKeyValue(ctx *analysisContext, expr ast.Expr, typeName string, kv *ast.KeyValueExpr) {
	if isAllowedStringBoundaryFile(ctx.filename(expr.Pos())) ||
		inJSONCompositeLiteral(ctx, expr) ||
		isAllowedPersistenceRowKeyValue(ctx, expr) ||
		isAllowedAdapterStringKeyValue(ctx, expr) ||
		isAllowedGinRouteParamKeyValue(ctx, kv) {
		return
	}
	if containsNode(kv.Key, expr) {
		ctx.pass.Reportf(expr.Pos(), "%s", stringLocalDiagnostic(typeName, "map key"))
		return
	}
	fieldName := keyName(kv.Key)
	if fieldName == "" {
		return
	}
	ctx.pass.Reportf(expr.Pos(), "%s", stringFieldDiagnostic(typeName, fieldName))
}

func isAllowedGinRouteParamKeyValue(ctx *analysisContext, kv *ast.KeyValueExpr) bool {
	if !isTestFile(ctx.filename(kv.Pos())) || keyName(kv.Key) != "Value" {
		return false
	}
	lit, ok := ctx.parent(kv).(*ast.CompositeLit)
	return ok && isNamedTypeFromPackage(ctx.pass.TypesInfo.TypeOf(lit), "github.com/gin-gonic/gin", "Param")
}

func checkStringReturn(ctx *analysisContext, expr ast.Expr, typeName string) {
	if isAllowedStringBoundaryFile(ctx.filename(expr.Pos())) {
		return
	}
	ctx.pass.Reportf(expr.Pos(), "%s", stringReturnDiagnostic(typeName, enclosingFuncName(ctx, expr)))
}

func isBuiltinStringConversion(ctx *analysisContext, call *ast.CallExpr) bool {
	target, ok := ctx.pass.TypesInfo.Types[call.Fun]
	return ok && target.IsType() && isBuiltinString(target.Type)
}

func isSemanticStringMethod(ctx *analysisContext, node ast.Node, typeName string) bool {
	fn := enclosingFunc(ctx, node)
	if fn == nil || fn.Name.Name != "String" || fn.Recv == nil || len(fn.Recv.List) == 0 {
		return false
	}
	recvTypeName, ok := semanticTypeNameOf(ctx.pass.TypesInfo.TypeOf(fn.Recv.List[0].Type))
	return ok && recvTypeName == typeName
}

func isAllowedSemanticStringConstructorCall(ctx *analysisContext, call *ast.CallExpr, sourceTypeName string) bool {
	targetTypeName, ok := conversionSemanticTypeName(ctx.pass, call.Fun)
	if !ok || !semanticConstructorAllowsSource(targetTypeName, sourceTypeName) {
		return false
	}
	return isAllowedDirectCastContext(ctx, call, targetTypeName)
}

func isAllowedTerminalStringCall(ctx *analysisContext, call *ast.CallExpr) bool {
	pkgPath, name := calleePackageAndName(ctx, call)
	switch pkgPath {
	case "fmt":
		switch name {
		case "Errorf", "Fprint", "Fprintf", "Fprintln", "Print", "Printf", "Println":
			return true
		default:
			return false
		}
	case "log":
		switch name {
		case "Fatal", "Fatalf", "Fatalln", "Panic", "Panicf", "Panicln", "Print", "Printf", "Println":
			return true
		default:
			return false
		}
	case "log/slog":
		switch name {
		case "Debug", "Error", "Info", "Log", "Warn":
			return true
		default:
			return false
		}
	case "net/url":
		return name == "PathEscape" || name == "QueryEscape"
	default:
		return false
	}
}

func isAllowedTestStringCall(ctx *analysisContext, call *ast.CallExpr) bool {
	if !isRepoTestBoundaryFile(ctx.filename(call.Pos())) {
		return false
	}
	if isAllowedTestAssertionCall(call) {
		return true
	}
	pkgPath, name := calleePackageAndName(ctx, call)
	switch pkgPath {
	case "path", "path/filepath", "strings":
		return true
	case "net/http", "net/http/httptest":
		return name == "NewRequest" || name == "NewRequestWithContext"
	}
	switch callName(call) {
	case "append":
		return true
	default:
		return strings.HasPrefix(callName(call), "assert") ||
			strings.HasPrefix(callName(call), "require")
	}
}

func isAllowedTestAssertionCall(call *ast.CallExpr) bool {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	switch selector.Sel.Name {
	case "Error", "Errorf", "Fatal", "Fatalf", "Log", "Logf", "Skip", "Skipf":
		return true
	default:
		return false
	}
}

func isAllowedTerminalCallBoundary(ctx *analysisContext, call *ast.CallExpr) bool {
	for current := ctx.parent(call); current != nil; current = ctx.parent(current) {
		switch n := current.(type) {
		case *ast.ParenExpr:
			continue
		case *ast.BinaryExpr:
			if n.Op == token.ADD {
				continue
			}
			return false
		case *ast.ReturnStmt, *ast.ExprStmt:
			return true
		case *ast.AssignStmt, *ast.ValueSpec:
			return !isStringType(ctx.pass, call)
		case *ast.KeyValueExpr:
			return inJSONCompositeLiteral(ctx, call) ||
				isAllowedPersistenceRowKeyValue(ctx, call) ||
				isAllowedAdapterStringKeyValue(ctx, call)
		case *ast.CallExpr:
			return false
		case *ast.FuncDecl:
			return false
		default:
			return false
		}
	}
	return false
}

func isAllowedSemanticConstructorTransform(ctx *analysisContext, call *ast.CallExpr, sourceTypeName string) bool {
	typeName, ok := enclosingSemanticConstructorType(ctx, call)
	if !ok ||
		!semanticConstructorAllowsSource(typeName, sourceTypeName) ||
		!isAllowedSemanticConstructorFunction(ctx, call, typeName) {
		return false
	}
	pkgPath, name := calleePackageAndName(ctx, call)
	if pkgPath != "strings" {
		return false
	}
	switch name {
	case "Trim", "TrimPrefix", "TrimSpace", "TrimSuffix", "ToLower", "ToUpper", "ReplaceAll":
		return true
	default:
		return false
	}
}

func isAllowedSerializedTestComparison(ctx *analysisContext, expr ast.Expr, binary *ast.BinaryExpr) bool {
	if !isTestFile(ctx.filename(expr.Pos())) {
		return false
	}
	if containsNode(binary.X, expr) {
		return isStringLiteral(binary.Y)
	}
	if containsNode(binary.Y, expr) {
		return isStringLiteral(binary.X)
	}
	return false
}

func isStringLiteral(expr ast.Expr) bool {
	lit, ok := expr.(*ast.BasicLit)
	return ok && lit.Kind == token.STRING
}

func enclosingSemanticConstructorType(ctx *analysisContext, node ast.Node) (string, bool) {
	fn := enclosingFunc(ctx, node)
	if fn == nil || fn.Type.Results == nil {
		return "", false
	}
	for _, result := range fn.Type.Results.List {
		if typeName, ok := semanticTypeNameOf(ctx.pass.TypesInfo.TypeOf(result.Type)); ok {
			return typeName, true
		}
	}
	return "", false
}

func isAllowedAdapterStringReturn(ctx *analysisContext, node ast.Node) bool {
	if !isEdgeAdapterFile(ctx.filename(node.Pos())) {
		return false
	}
	switch enclosingFuncName(ctx, node) {
	case "ID", "Metadata":
		return true
	default:
		return false
	}
}

func isAllowedErrorInterfaceStringReturn(ctx *analysisContext, node ast.Node) bool {
	fn := enclosingFunc(ctx, node)
	if fn == nil || fn.Name.Name != "Error" || fn.Recv == nil || len(fn.Recv.List) == 0 {
		return false
	}
	if fn.Type.Params != nil && len(fn.Type.Params.List) != 0 {
		return false
	}
	if fn.Type.Results == nil || len(fn.Type.Results.List) != 1 {
		return false
	}
	return isBuiltinString(ctx.pass.TypesInfo.TypeOf(fn.Type.Results.List[0].Type))
}

func isAllowedAdapterStringKeyValue(ctx *analysisContext, node ast.Node) bool {
	return isEdgeAdapterFile(ctx.filename(node.Pos())) && enclosingFuncName(ctx, node) == "Metadata"
}
