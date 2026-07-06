package semantichygiene

import (
	"fmt"
	"go/ast"
	"go/types"
	"strings"
)

func isTestFile(filename string) bool {
	return strings.HasSuffix(filename, "_test.go")
}

func isAllowedTestDescriptionLiteral(ctx *analysisContext, lit *ast.BasicLit) bool {
	call, index, ok := directCallArg(ctx, lit)
	if !ok {
		return false
	}
	name := callName(call)
	return (index == 0 && isBDDDescriptionCall(name)) ||
		(index > 0 && isGomegaAnnotationCall(name))
}

func isStableTestContractLiteral(ctx *analysisContext, lit *ast.BasicLit, value string) bool {
	if strings.TrimSpace(value) == "" {
		return false
	}
	if isLeafWikiTrailerProtocolLiteral(value) {
		return isTestAssertionLiteralContext(ctx, lit) ||
			isTestTrailerIndexLiteralContext(ctx, lit) ||
			isTestContractLiteralContext(ctx, lit)
	}
	if isRuntimeRoleHealthWireLiteral(value) && isRuntimeRoleHealthWireLiteralContext(ctx, lit) {
		return true
	}
	if isLikelyErrorCodeLiteral(value) && isTestStringMatcherLiteralContext(ctx, lit) {
		return true
	}
	return isStableMessageLikeLiteral(value) && isTestContractLiteralContext(ctx, lit)
}

func isLikelyErrorCodeLiteral(value string) bool {
	if !errorCodePattern.MatchString(strings.TrimSpace(value)) {
		return false
	}
	canonical := canonicalName(value)
	return strings.Contains(canonical, "invalid") ||
		strings.Contains(canonical, "error") ||
		strings.Contains(canonical, "failed") ||
		strings.Contains(canonical, "failure") ||
		strings.Contains(canonical, "missing") ||
		strings.Contains(canonical, "required") ||
		strings.Contains(canonical, "conflict") ||
		strings.Contains(canonical, "forbidden") ||
		strings.Contains(canonical, "unauthorized") ||
		strings.Contains(canonical, "denied")
}

func isRuntimeRoleHealthWireLiteral(value string) bool {
	switch value {
	case "role_wikid",
		"role_frontd",
		"role_workspaced",
		"role_unknown",
		"crashed",
		"degraded",
		"failed",
		"indexing",
		"missing",
		"not_applicable",
		"ok",
		"restarting",
		"starting",
		"stopped",
		"unknown":
		return true
	default:
		return false
	}
}

func isRuntimeRoleHealthWireLiteralContext(ctx *analysisContext, lit *ast.BasicLit) bool {
	fieldSignal := false
	for current := ast.Node(lit); current != nil; current = ctx.parent(current) {
		switch n := current.(type) {
		case *ast.KeyValueExpr:
			if containsNode(n.Value, lit) && isRuntimeRoleHealthWireFieldName(keyName(n.Key)) {
				fieldSignal = true
			}
		case *ast.CompositeLit:
			if nameSuggestsRuntimeRoleHealth(exprName(n.Type)) {
				return true
			}
		case *ast.AssignStmt:
			if assignStmtValueNameSuggestsRuntimeRoleHealth(n, lit) {
				return true
			}
		case *ast.ValueSpec:
			if valueSpecNameSuggestsRuntimeRoleHealth(n, lit) {
				return true
			}
		case *ast.CallExpr:
			if fieldSignal && nameSuggestsRuntimeRoleHealth(callName(n)) {
				return true
			}
		case *ast.FuncDecl:
			return fieldSignal && nameSuggestsRuntimeRoleHealth(n.Name.Name)
		}
	}
	return false
}

func isRuntimeRoleHealthWireFieldName(name string) bool {
	switch canonicalName(name) {
	case "key", "state", "status", "health", "rolehealth":
		return true
	default:
		return false
	}
}

func assignStmtValueNameSuggestsRuntimeRoleHealth(stmt *ast.AssignStmt, lit *ast.BasicLit) bool {
	for i, rhs := range stmt.Rhs {
		if containsNode(rhs, lit) && i < len(stmt.Lhs) {
			return nameSuggestsRuntimeRoleHealth(exprName(stmt.Lhs[i]))
		}
	}
	return false
}

func valueSpecNameSuggestsRuntimeRoleHealth(spec *ast.ValueSpec, lit *ast.BasicLit) bool {
	for i, value := range spec.Values {
		if containsNode(value, lit) && i < len(spec.Names) {
			return nameSuggestsRuntimeRoleHealth(spec.Names[i].Name)
		}
	}
	return false
}

func nameSuggestsRuntimeRoleHealth(name string) bool {
	canonical := canonicalName(name)
	return strings.Contains(canonical, "rolehealth") ||
		strings.Contains(canonical, "runtimehealth") ||
		strings.Contains(canonical, "healthwire") ||
		strings.Contains(canonical, "healthcheck")
}

func isTestAssertionLiteralContext(ctx *analysisContext, lit *ast.BasicLit) bool {
	return literalHasAncestorCallBeforeFunc(ctx, lit, func(call *ast.CallExpr) bool {
		return isGomegaAssertionMethod(callName(call))
	})
}

func isTestTrailerIndexLiteralContext(ctx *analysisContext, lit *ast.BasicLit) bool {
	for current := ast.Node(lit); current != nil; current = ctx.parent(current) {
		switch n := current.(type) {
		case *ast.IndexExpr:
			return containsNode(n.Index, lit)
		case *ast.FuncDecl:
			return false
		}
	}
	return false
}

func isTestContractLiteralContext(ctx *analysisContext, lit *ast.BasicLit) bool {
	for current := ast.Node(lit); current != nil; current = ctx.parent(current) {
		switch n := current.(type) {
		case *ast.CallExpr:
			name := callName(n)
			if isBDDDescriptionCall(name) {
				return isBDDContractDataLiteral(ctx, n, lit)
			}
			if isTestAssertionMatcherContractContext(ctx, n) ||
				isTestSemanticAssertionHelper(name) ||
				isTestContractAssertionCall(ctx, n) {
				return true
			}
		case *ast.FuncDecl:
			return false
		}
	}
	return false
}

func isTestStringMatcherLiteralContext(ctx *analysisContext, lit *ast.BasicLit) bool {
	return literalHasAncestorCallBeforeFunc(ctx, lit, func(call *ast.CallExpr) bool {
		return isTestAssertionMatcherCall(callName(call)) &&
			callContainsArg(call, lit) &&
			matcherEventuallyUsedByTestAssertionOrHelper(ctx, call)
	})
}

func matcherEventuallyUsedByTestAssertionOrHelper(ctx *analysisContext, matcher *ast.CallExpr) bool {
	return callHasAncestorOrFunc(ctx, matcher, func(call *ast.CallExpr) bool {
		return isGomegaAssertionMethod(callName(call))
	}, func(fn *ast.FuncDecl) bool {
		return isTestSemanticAssertionHelper(fn.Name.Name)
	})
}

func isTestLocalizedProseContractLiteralContext(ctx *analysisContext, lit *ast.BasicLit) bool {
	for current := ast.Node(lit); current != nil; current = ctx.parent(current) {
		switch n := current.(type) {
		case *ast.CallExpr:
			name := callName(n)
			if isBDDDescriptionCall(name) {
				return isBDDRenderedProseDataLiteral(ctx, n, lit)
			}
			if isTestAssertionMatcherLocalizedProseContext(ctx, n) ||
				isTestSemanticAssertionHelper(name) ||
				isTestContractAssertionCall(ctx, n) {
				return true
			}
		case *ast.KeyValueExpr:
			if isTestRenderedProseFieldLiteral(ctx, n, lit) {
				return true
			}
		case *ast.FuncDecl:
			return false
		}
	}
	return false
}

func isTestRenderedProseFieldLiteral(ctx *analysisContext, kv *ast.KeyValueExpr, lit *ast.BasicLit) bool {
	if !containsNode(kv.Value, lit) || !nameSuggestsTestRenderedProseSubject(keyName(kv.Key)) {
		return false
	}
	return isTestAssertionLiteralContext(ctx, lit)
}

func directCallArg(ctx *analysisContext, lit *ast.BasicLit) (*ast.CallExpr, int, bool) {
	call, ok := ctx.parent(lit).(*ast.CallExpr)
	if !ok {
		return nil, 0, false
	}
	index, ok := directArgIndex(call, lit)
	if !ok {
		return nil, 0, false
	}
	return call, index, true
}

func directArgIndex(call *ast.CallExpr, lit *ast.BasicLit) (int, bool) {
	for i, arg := range call.Args {
		if arg == lit {
			return i, true
		}
	}
	return 0, false
}

func isBDDContractDataLiteral(ctx *analysisContext, call *ast.CallExpr, lit *ast.BasicLit) bool {
	paramName, ok := bddEntryDataParamName(ctx, call, lit)
	if !ok {
		return false
	}
	return testTableParamSuggestsContract(paramName)
}

func isBDDRenderedProseDataLiteral(ctx *analysisContext, call *ast.CallExpr, lit *ast.BasicLit) bool {
	paramName, ok := bddEntryDataParamName(ctx, call, lit)
	if !ok {
		return false
	}
	return testTableParamSuggestsRenderedProseContract(paramName)
}

func bddEntryDataParamName(ctx *analysisContext, call *ast.CallExpr, lit *ast.BasicLit) (string, bool) {
	if !isBDDEntryCall(callName(call)) {
		return "", false
	}
	if keyName, ok := bddEntryDataKeyName(ctx, call, lit); ok {
		return keyName, true
	}
	index, ok := bddEntryDataArgIndex(ctx, call, lit)
	if !ok || index == 0 {
		return "", false
	}
	return bddEntryTableParamName(ctx, call, index-1)
}

func bddEntryDataKeyName(ctx *analysisContext, call *ast.CallExpr, lit *ast.BasicLit) (string, bool) {
	key, ok := enclosingKeyValueWithin(ctx, lit, call)
	if !ok {
		return "", false
	}
	return exprName(key.Key), true
}

func enclosingKeyValueWithin(ctx *analysisContext, node ast.Node, stop ast.Node) (*ast.KeyValueExpr, bool) {
	for current := ast.Node(node); current != nil && current != stop; current = ctx.parent(current) {
		key, ok := current.(*ast.KeyValueExpr)
		if ok {
			return key, true
		}
	}
	return nil, false
}

func bddEntryDataArgIndex(ctx *analysisContext, call *ast.CallExpr, node ast.Node) (int, bool) {
	child := directChildWithin(ctx, node, call)
	if child == nil {
		return 0, false
	}
	for i, arg := range call.Args {
		if arg == child {
			return i, true
		}
	}
	return 0, false
}

func directChildWithin(ctx *analysisContext, node ast.Node, parent ast.Node) ast.Node {
	current := ast.Node(node)
	for current != nil {
		next := ctx.parent(current)
		if next == parent {
			return current
		}
		current = next
	}
	return nil
}

func literalHasAncestorCallBeforeFunc(ctx *analysisContext, lit *ast.BasicLit, accept func(*ast.CallExpr) bool) bool {
	return callHasAncestorBeforeFunc(ctx, lit, accept)
}

func callHasAncestorBeforeFunc(ctx *analysisContext, node ast.Node, accept func(*ast.CallExpr) bool) bool {
	return callHasAncestorOrFunc(ctx, node, accept, func(*ast.FuncDecl) bool {
		return false
	})
}

func callHasAncestorOrFunc(
	ctx *analysisContext,
	node ast.Node,
	acceptCall func(*ast.CallExpr) bool,
	acceptFunc func(*ast.FuncDecl) bool,
) bool {
	for current := ast.Node(node); current != nil; current = ctx.parent(current) {
		switch n := current.(type) {
		case *ast.CallExpr:
			if acceptCall(n) {
				return true
			}
		case *ast.FuncDecl:
			return acceptFunc(n)
		}
	}
	return false
}

func isBDDEntryCall(name string) bool {
	switch name {
	case "Entry", "FEntry", "PEntry", "XEntry":
		return true
	default:
		return false
	}
}

func isBDDDescriptionCall(name string) bool {
	switch name {
	case "Describe", "Context", "When", "It", "Specify", "DescribeTable", "Entry",
		"FDescribe", "FContext", "FWhen", "FIt", "FSpecify", "FDescribeTable", "FEntry",
		"PDescribe", "PContext", "PWhen", "PIt", "PSpecify", "PDescribeTable", "PEntry",
		"XDescribe", "XContext", "XWhen", "XIt", "XSpecify", "XDescribeTable", "XEntry",
		"By", "Label", "EntryDescription":
		return true
	default:
		return false
	}
}

func bddEntryTableParamName(ctx *analysisContext, entry *ast.CallExpr, dataIndex int) (string, bool) {
	name, _, ok := bddEntryTableParam(ctx, entry, dataIndex)
	return name, ok
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
