package semantichygiene

import (
	"go/ast"
	"go/token"
	"go/types"
	"regexp"
	"strconv"
	"strings"
)

func isStableLiteralAllowed(ctx *analysisContext, lit *ast.BasicLit) bool {
	filename := ctx.filename(lit.Pos())
	if isTestFile(filename) ||
		strings.Contains(filename, "/internal/localization/") ||
		strings.Contains(filename, "/docs/") ||
		strings.HasSuffix(filename, ".json") {
		return true
	}
	return isConstOrTypeDefinition(ctx, lit)
}

func isLocalizedProseLiteralAllowed(ctx *analysisContext, lit *ast.BasicLit) bool {
	filename := ctx.filename(lit.Pos())
	if isTestFile(filename) ||
		isGeneratedOrVendored(filename) ||
		strings.Contains(filename, "/docs/") ||
		strings.HasSuffix(filename, ".json") {
		return true
	}
	return isConstOrTypeDefinition(ctx, lit)
}

func looksLikeLocalizedProse(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || messageIDPattern.MatchString(value) || toolIDPattern.MatchString(value) || errorCodePattern.MatchString(value) {
		return false
	}
	return strings.ContainsAny(value, " \t\n")
}

var localizedProseContractCalls = map[string]bool{
	"apiSuccessMessage":          true,
	"abortWorkspaceSyncError":    true,
	"newMessageOutput":           true,
	"newToolDescriptor":          true,
	"Render":                     true,
	"writeControlError":          true,
	"writeFrontdError":           true,
	"writePrivateWorkspaceError": true,
	"writeRuntimeError":          true,
}

var localizedProseFirstArgContractCalls = map[string]bool{
	"fail":                 true,
	"failWithoutAgentHook": true,
}

var localizedProseConstructorCalls = map[string]bool{
	"NewLocalizedError":                     true,
	"NewLocalizedErrorFromCodeWithFallback": true,
	"NewLocalizedErrorDetail":               true,
	"NewFieldErrorWithCode":                 true,
	"AddWithCode":                           true,
}

var localizedProseStrictContractCalls = map[string]bool{
	"abortWorkspaceSyncError":    true,
	"writeControlError":          true,
	"writeFrontdError":           true,
	"writePrivateWorkspaceError": true,
}

var localizedProseSignatureSinkCalls = map[string]bool{
	"writeControlError":          true,
	"writeFrontdError":           true,
	"writePrivateWorkspaceError": true,
}

func isRawLocalizedProseContractLiteral(ctx *analysisContext, lit *ast.BasicLit) bool {
	for current := ast.Node(lit); current != nil; current = ctx.parent(current) {
		switch n := current.(type) {
		case *ast.CallExpr:
			if isRawLocalizedProseCallLiteral(ctx, n, lit) {
				return true
			}
		case *ast.KeyValueExpr:
			if isRawLocalizedProseFieldLiteral(ctx, n, lit) {
				return true
			}
		case *ast.FuncDecl:
			return false
		}
	}
	return false
}

func isRawLocalizedProseCallLiteral(ctx *analysisContext, call *ast.CallExpr, lit *ast.BasicLit) bool {
	name := callName(call)
	if localizedProseFirstArgContractCalls[name] {
		return callContainsFirstArg(call, lit)
	}
	if localizedProseConstructorCalls[name] {
		return callContainsArg(call, lit) && localizedProseConstructorRequiresCatalogOnly(name, ctx, call)
	}
	if localizedProseContractCalls[name] || isCLIControlProseCall(ctx, call) || isPrivateHTTPErrorProseCall(ctx, call) {
		return callContainsArg(call, lit)
	}
	return false
}

func isRawLocalizedProseFieldLiteral(ctx *analysisContext, kv *ast.KeyValueExpr, lit *ast.BasicLit) bool {
	fieldName := keyName(kv.Key)
	if !containsNode(kv.Value, lit) {
		return false
	}
	if fieldName == "message" {
		return isResponsePayloadLiteral(ctx, kv)
	}
	if messageFieldName(fieldName) {
		return isMessageFieldValueMissingMessageID(ctx, kv)
	}
	return warningStringsFieldName(fieldName) && isWarningFieldValueMissingMessageID(ctx, kv)
}

func isCLIControlProseCall(ctx *analysisContext, call *ast.CallExpr) bool {
	if !isCLIControlProseFile(ctx.filename(call.Pos())) {
		return false
	}
	packagePath, name := calleePackageAndName(ctx, call)
	if packagePath != "fmt" {
		return false
	}
	switch name {
	case "Printf", "Println":
		return true
	case "Fprint", "Fprintf", "Fprintln":
		return len(call.Args) > 0 && isCLIOutputWriterExpr(call.Args[0])
	case "Errorf":
		return isCLIControlErrorfContext(ctx, call)
	default:
		return false
	}
}

func isCLIOutputWriterExpr(expr ast.Expr) bool {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	return exprName(sel.X) == "os" && (sel.Sel.Name == "Stdout" || sel.Sel.Name == "Stderr")
}

func isCLIControlErrorfContext(ctx *analysisContext, call *ast.CallExpr) bool {
	fnName := canonicalName(enclosingFuncName(ctx, call))
	return strings.Contains(fnName, "validatemcptransport") ||
		strings.Contains(fnName, "mcptransportvalidation")
}

func isCLIControlProseFile(filename string) bool {
	return strings.HasSuffix(filename, "/cmd/leafwiki/main.go") ||
		isSemanticHygienePolicyFixtureFile(filename)
}

func isPrivateHTTPErrorProseCall(ctx *analysisContext, call *ast.CallExpr) bool {
	if !isPrivateHTTPResponseFile(ctx.filename(call.Pos())) {
		return false
	}
	packagePath, name := calleePackageAndName(ctx, call)
	return packagePath == "net/http" && name == "Error"
}

func isPrivateHTTPResponseFile(filename string) bool {
	return strings.Contains(filename, "/internal/wikid/") ||
		strings.Contains(filename, "/internal/projectdaemon/") ||
		strings.Contains(filename, "/internal/frontd/") ||
		isSemanticHygienePolicyFixtureFile(filename)
}

func isStrictLocalizedProseContractLiteral(ctx *analysisContext, lit *ast.BasicLit, value string) bool {
	if isStableMessageLikeLiteral(value) {
		return false
	}
	for current := ast.Node(lit); current != nil; current = ctx.parent(current) {
		switch n := current.(type) {
		case *ast.CallExpr:
			if isStrictLocalizedProseCallLiteral(ctx, n, lit) {
				return true
			}
		case *ast.FuncDecl:
			return false
		}
	}
	return false
}

func isStableMessageLikeLiteral(value string) bool {
	value = strings.TrimSpace(value)
	return value == "" ||
		messageIDPattern.MatchString(value) ||
		toolIDPattern.MatchString(value) ||
		errorCodePattern.MatchString(value)
}

func isStrictLocalizedProseCallLiteral(ctx *analysisContext, call *ast.CallExpr, lit *ast.BasicLit) bool {
	if localizedProseStrictContractCalls[callName(call)] || isPrivateHTTPErrorProseCall(ctx, call) {
		return callContainsArg(call, lit)
	}
	return false
}

func localizedProseConstructorRequiresCatalogOnly(name string, ctx *analysisContext, call *ast.CallExpr) bool {
	switch name {
	case "NewLocalizedError", "NewLocalizedErrorFromCodeWithFallback", "NewLocalizedErrorDetail", "NewFieldErrorWithCode", "AddWithCode":
		return true
	default:
		return callHasRawStableContractArg(ctx, call)
	}
}

func callHasRawStableContractArg(ctx *analysisContext, call *ast.CallExpr) bool {
	for _, arg := range call.Args {
		lit, ok := arg.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			continue
		}
		value, err := strconv.Unquote(lit.Value)
		if err == nil && isStableContractLiteral(ctx, lit, value) {
			return true
		}
	}
	return false
}

func callContainsArg(call *ast.CallExpr, target ast.Node) bool {
	for _, arg := range call.Args {
		if containsNode(arg, target) {
			return true
		}
	}
	return false
}

func callContainsFirstArg(call *ast.CallExpr, target ast.Node) bool {
	return len(call.Args) > 0 && containsNode(call.Args[0], target)
}

func isResponsePayloadLiteral(ctx *analysisContext, node ast.Node) bool {
	for current := node; current != nil; current = ctx.parent(current) {
		switch n := current.(type) {
		case *ast.CompositeLit:
			return exprName(n.Type) == "H" || isStringKeyedMap(ctx.pass.TypesInfo.TypeOf(n))
		case *ast.FuncDecl:
			return false
		}
	}
	return false
}

func isStringKeyedMap(typ types.Type) bool {
	if typ == nil {
		return false
	}
	m, ok := types.Unalias(typ).Underlying().(*types.Map)
	return ok && isString(m.Key())
}

var (
	errorCodePattern = regexp.MustCompile(`^[a-z][a-z0-9]*(_[a-z0-9]+){1,}$`)
	toolIDPattern    = regexp.MustCompile(`^wiki_[a-z0-9_]+$`)
	messageIDPattern = regexp.MustCompile(`^(errors|validation|api|mcp|cli|shell|ui)(\.[a-z0-9]+(?:_[a-z0-9]+)*)+$`)
	trailerPattern   = regexp.MustCompile(`^LeafWiki-[A-Za-z0-9-]+(?::.*)?$`)
)

func isLeafWikiTrailerProtocolLiteral(value string) bool {
	return trailerPattern.MatchString(strings.TrimSpace(value))
}

func isStableContractLiteral(ctx *analysisContext, lit *ast.BasicLit, value string) bool {
	return toolIDPattern.MatchString(value) ||
		messageIDPattern.MatchString(value) ||
		(errorCodePattern.MatchString(value) && stableLiteralContextSuggestsContract(ctx, lit))
}

func stableLiteralContextSuggestsContract(ctx *analysisContext, lit *ast.BasicLit) bool {
	for current := ast.Node(lit); current != nil; current = ctx.parent(current) {
		matches, terminal := stableLiteralContextDecision(ctx, current, lit)
		if matches || terminal {
			return matches
		}
	}
	return false
}

func stableLiteralContextDecision(ctx *analysisContext, node ast.Node, lit *ast.BasicLit) (matches bool, terminal bool) {
	switch n := node.(type) {
	case *ast.CallExpr:
		return nameSuggestsStableContract(callName(n)), false
	case *ast.KeyValueExpr:
		return nameSuggestsStableContract(keyName(n.Key)), false
	case *ast.AssignStmt:
		return assignStmtValueNameSuggestsStableContract(n, lit), false
	case *ast.ValueSpec:
		return valueSpecNameSuggestsStableContract(n, lit), false
	case *ast.ReturnStmt:
		return nameSuggestsStableContract(enclosingFuncName(ctx, n)), false
	case *ast.FuncDecl:
		return nameSuggestsStableContract(n.Name.Name), true
	default:
		return false, false
	}
}

func assignStmtValueNameSuggestsStableContract(stmt *ast.AssignStmt, lit *ast.BasicLit) bool {
	for i, rhs := range stmt.Rhs {
		if containsNode(rhs, lit) && i < len(stmt.Lhs) {
			return nameSuggestsStableContract(exprName(stmt.Lhs[i]))
		}
	}
	return false
}

func valueSpecNameSuggestsStableContract(spec *ast.ValueSpec, lit *ast.BasicLit) bool {
	for i, value := range spec.Values {
		if containsNode(value, lit) && i < len(spec.Names) {
			return nameSuggestsStableContract(spec.Names[i].Name)
		}
	}
	return false
}

func nameSuggestsStableContract(name string) bool {
	canonical := canonicalName(name)
	return canonical == "code" ||
		strings.HasSuffix(canonical, "code") ||
		strings.Contains(canonical, "error") ||
		strings.Contains(canonical, "messageid") ||
		strings.Contains(canonical, "toolid") ||
		strings.Contains(canonical, "issue") ||
		strings.Contains(canonical, "validation")
}

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

func containsNode(root ast.Node, target ast.Node) bool {
	found := false
	ast.Inspect(root, func(node ast.Node) bool {
		if node == target {
			found = true
			return false
		}
		return !found
	})
	return found
}
