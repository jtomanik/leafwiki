package semantichygiene

import (
	"go/ast"
	"go/token"
	"strings"
)

func checkGinkgoGoroutineAssertionRecovery(ctx *analysisContext, stmt *ast.GoStmt) {
	if !isTestFile(ctx.filename(stmt.Pos())) || !insideGinkgoSpecOrGoTest(ctx, stmt) {
		return
	}
	call := stmt.Call
	if call == nil || callName(call) == "GinkgoHelperGo" {
		return
	}
	fn, ok := unparenExpr(call.Fun).(*ast.FuncLit)
	if !ok {
		return
	}
	if !funcLitContainsFailureAssertion(ctx, fn) || funcLitDefersGinkgoRecover(fn) {
		return
	}
	ctx.report(ruleGinkgoGoroutineRecover, stmt, ginkgoGoroutineRecoverDiagnostic())
}

func checkGinkgoBlockingReceive(ctx *analysisContext, expr *ast.UnaryExpr) {
	if expr.Op != token.ARROW ||
		!isTestFile(ctx.filename(expr.Pos())) ||
		!insideGinkgoSpecOrGoTest(ctx, expr) ||
		insideSelectStmt(ctx, expr) ||
		insideGoroutineFuncLit(ctx, expr) {
		return
	}
	ctx.report(ruleGinkgoBlockingReceive, expr, ginkgoBlockingReceiveDiagnostic())
}

func checkGinkgoHelperFirst(ctx *analysisContext, fn *ast.FuncDecl) {
	if fn.Body == nil ||
		!isTestFile(ctx.filename(fn.Pos())) ||
		!isTestAssertionHelperName(fn.Name.Name) ||
		!funcContainsGomegaAssertion(ctx, fn.Body) ||
		!funcHasGinkgoHelperCall(fn.Body) ||
		firstStatementIsGinkgoHelper(fn.Body) {
		return
	}
	ctx.report(ruleGinkgoHelperFirst, fn.Name, ginkgoHelperFirstDiagnostic(fn.Name.Name))
}

func checkReusableAssertionHelper(ctx *analysisContext, fn *ast.FuncDecl) {
	if fn.Body == nil ||
		!isTestAssertionHelperName(fn.Name.Name) ||
		!funcContainsGomegaAssertion(ctx, fn.Body) ||
		!funcHasGomegaHelperReporting(ctx, fn) ||
		funcReturnsGomegaMatcher(fn) ||
		countCallsToFunction(ctx, fn.Name.Name) < 2 {
		return
	}
	filename := ctx.filename(fn.Pos())
	if !isTestFile(filename) && !isTestSupportFile(filename) {
		return
	}
	ctx.report(ruleGomegaHelperShouldBeMatcher, fn.Name, ginkgoReusableHelperMatcherDiagnostic(fn.Name.Name))
}

func checkGinkgoGlobalStateCleanup(ctx *analysisContext, call *ast.CallExpr) {
	if !isTestFile(ctx.filename(call.Pos())) {
		return
	}
	name, ok := ginkgoGlobalStateMutationCall(ctx, call)
	if !ok || nodeIsInsideCallNamed(ctx, call, "DeferCleanup") || enclosingBlockHasDeferCleanup(ctx, call) {
		return
	}
	ctx.report(ruleGinkgoGlobalStateCleanup, call, ginkgoGlobalStateCleanupDiagnostic(name))
}

func checkGinkgoGlobalStateAssignment(ctx *analysisContext, stmt *ast.AssignStmt) {
	if !isTestFile(ctx.filename(stmt.Pos())) || enclosingBlockHasDeferCleanup(ctx, stmt) {
		return
	}
	for _, lhs := range stmt.Lhs {
		name, ok := ginkgoGlobalStateMutationAssignment(lhs)
		if !ok {
			continue
		}
		ctx.report(ruleGinkgoGlobalStateCleanup, lhs, ginkgoGlobalStateCleanupDiagnostic(name))
	}
}

func ginkgoGlobalStateMutationCall(ctx *analysisContext, call *ast.CallExpr) (string, bool) {
	packagePath, name := calleePackageAndName(ctx, call)
	switch {
	case packagePath == "os" && (name == "Setenv" || name == "Unsetenv" || name == "Clearenv"):
		return "os." + name, true
	case packagePath == "log/slog" && name == "SetDefault":
		return "slog.SetDefault", true
	}
	switch name {
	case "SetDefaultEventuallyTimeout", "SetDefaultEventuallyPollingInterval",
		"SetDefaultConsistentlyDuration", "SetDefaultConsistentlyPollingInterval",
		"RegisterCustomFormatter":
		return name, true
	default:
		return "", false
	}
}

func ginkgoGlobalStateMutationAssignment(expr ast.Expr) (string, bool) {
	selector, ok := unparenExpr(expr).(*ast.SelectorExpr)
	if !ok || !isGomegaFormatField(selector.Sel.Name) {
		return "", false
	}
	if ident, ok := unparenExpr(selector.X).(*ast.Ident); ok && ident.Name == "format" {
		return "format." + selector.Sel.Name, true
	}
	return "", false
}

func isGomegaFormatField(name string) bool {
	switch name {
	case "MaxLength", "MaxDepth", "TruncatedDiff", "UseStringerRepresentation", "PrintContextObjects":
		return true
	default:
		return false
	}
}

func firstFuncLitArg(call *ast.CallExpr) (*ast.FuncLit, bool) {
	for _, arg := range call.Args {
		if fn, ok := unparenExpr(arg).(*ast.FuncLit); ok {
			return fn, true
		}
	}
	return nil, false
}

func isFocusedGinkgoNodeName(name string) bool {
	return strings.HasPrefix(name, "F") && isGinkgoNodeBaseName(strings.TrimPrefix(name, "F"))
}

func isPendingGinkgoNodeName(name string) bool {
	return (strings.HasPrefix(name, "P") || strings.HasPrefix(name, "X")) &&
		isGinkgoNodeBaseName(name[1:])
}

func isGinkgoNodeBaseName(name string) bool {
	switch name {
	case "Describe", "Context", "When", "It", "Specify", "DescribeTable", "Entry":
		return true
	default:
		return false
	}
}

func isGinkgoContainerNodeName(name string) bool {
	switch name {
	case "Describe", "Context", "When",
		"FDescribe", "FContext", "FWhen",
		"PDescribe", "PContext", "PWhen",
		"XDescribe", "XContext", "XWhen":
		return true
	default:
		return false
	}
}

func isGinkgoRunnableNodeName(name string) bool {
	switch name {
	case "Describe", "Context", "When", "It", "Specify", "DescribeTable",
		"FDescribe", "FContext", "FWhen", "FIt", "FSpecify", "FDescribeTable",
		"PDescribe", "PContext", "PWhen", "PIt", "PSpecify", "PDescribeTable",
		"XDescribe", "XContext", "XWhen", "XIt", "XSpecify", "XDescribeTable":
		return true
	default:
		return false
	}
}

func isGinkgoSetupNodeName(name string) bool {
	switch name {
	case "BeforeEach", "JustBeforeEach", "BeforeAll", "BeforeSuite", "SynchronizedBeforeSuite":
		return true
	default:
		return false
	}
}

func isAllowedGinkgoContainerConstructionCall(name string) bool {
	return isGinkgoRunnableNodeName(name) ||
		isBDDEntryCall(name) ||
		isGinkgoSetupNodeName(name) ||
		isGinkgoTeardownNodeName(name) ||
		isGinkgoPolicyDecoratorName(name) ||
		name == "Label" ||
		name == "EntryDescription"
}

func isGinkgoTeardownNodeName(name string) bool {
	switch name {
	case "AfterEach", "JustAfterEach", "AfterAll", "AfterSuite", "SynchronizedAfterSuite":
		return true
	default:
		return false
	}
}

func isGinkgoPolicyDecoratorName(name string) bool {
	switch name {
	case "Focus", "Pending", "FlakeAttempts", "Serial", "Ordered", "SpecPriority":
		return true
	default:
		return false
	}
}

func isDisallowedGinkgoContainerCall(name string) bool {
	switch name {
	case "Expect", "ExpectWithOffset", "Ω", "Eventually", "Consistently", "By", "Skip", "DeferCleanup", "Fail":
		return true
	default:
		return false
	}
}

func insideGinkgoSpecOrGoTest(ctx *analysisContext, node ast.Node) bool {
	for current := node; current != nil; current = ctx.parent(current) {
		switch n := current.(type) {
		case *ast.FuncDecl:
			return strings.HasPrefix(n.Name.Name, "Test")
		case *ast.FuncLit:
			call, ok := ctx.parent(n).(*ast.CallExpr)
			if ok && isGinkgoSubjectBodyNodeName(callName(call)) {
				return true
			}
		}
	}
	return false
}

func isGinkgoSpecNodeName(name string) bool {
	switch name {
	case "It", "Specify", "FIt", "FSpecify", "PIt", "PSpecify", "XIt", "XSpecify":
		return true
	default:
		return false
	}
}

func isGinkgoSubjectBodyNodeName(name string) bool {
	return isGinkgoSpecNodeName(name) || isDescribeTableCall(name)
}

func isTopLevelItCandidateName(name string) bool {
	return name == "It" || name == "Specify"
}

func isGinkgoDSLCall(ctx *analysisContext, call *ast.CallExpr) bool {
	packagePath, _ := calleePackageAndName(ctx, call)
	return packagePath == "github.com/onsi/ginkgo/v2"
}

func insideSelectStmt(ctx *analysisContext, node ast.Node) bool {
	for current := node; current != nil; current = ctx.parent(current) {
		switch current.(type) {
		case *ast.SelectStmt, *ast.CommClause:
			return true
		case *ast.FuncDecl:
			return false
		}
	}
	return false
}

func insideGoroutineFuncLit(ctx *analysisContext, node ast.Node) bool {
	for current := node; current != nil; current = ctx.parent(current) {
		fn, ok := current.(*ast.FuncLit)
		if !ok {
			if _, ok := current.(*ast.FuncDecl); ok {
				return false
			}
			continue
		}
		call, ok := ctx.parent(fn).(*ast.CallExpr)
		if !ok {
			continue
		}
		_, ok = ctx.parent(call).(*ast.GoStmt)
		return ok
	}
	return false
}

func funcLitContainsFailureAssertion(ctx *analysisContext, fn *ast.FuncLit) bool {
	found := false
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		if found || node == nil {
			return false
		}
		if nested, ok := node.(*ast.FuncLit); ok && nested != fn {
			return false
		}
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		if _, ok := gomegaAssertionFromCall(ctx, call); ok ||
			isGlobalGomegaExpectCall(call) ||
			callName(call) == "Fail" ||
			isTestAssertionHelperName(callName(call)) {
			found = true
			return false
		}
		return true
	})
	return found
}

func funcLitDefersGinkgoRecover(fn *ast.FuncLit) bool {
	found := false
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		if found || node == nil {
			return false
		}
		if nested, ok := node.(*ast.FuncLit); ok && nested != fn {
			return false
		}
		deferStmt, ok := node.(*ast.DeferStmt)
		if !ok || deferStmt.Call == nil {
			return true
		}
		found = callName(deferStmt.Call) == "GinkgoRecover"
		return !found
	})
	return found
}

func funcHasGinkgoHelperCall(body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(node ast.Node) bool {
		if found || node == nil {
			return false
		}
		call, ok := node.(*ast.CallExpr)
		if ok && callName(call) == "GinkgoHelper" {
			found = true
			return false
		}
		return true
	})
	return found
}

func firstStatementIsGinkgoHelper(body *ast.BlockStmt) bool {
	if len(body.List) == 0 {
		return false
	}
	exprStmt, ok := body.List[0].(*ast.ExprStmt)
	if !ok {
		return false
	}
	call, ok := unparenExpr(exprStmt.X).(*ast.CallExpr)
	return ok && callName(call) == "GinkgoHelper"
}

func countCallsToFunction(ctx *analysisContext, name string) int {
	count := 0
	for _, file := range ctx.pass.Files {
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if ok && callName(call) == name {
				count++
			}
			return true
		})
	}
	return count
}

func nodeIsInsideCallNamed(ctx *analysisContext, node ast.Node, name string) bool {
	for current := node; current != nil; current = ctx.parent(current) {
		call, ok := current.(*ast.CallExpr)
		if ok && callName(call) == name {
			return true
		}
		if _, ok := current.(*ast.FuncDecl); ok {
			return false
		}
	}
	return false
}

func enclosingBlockHasDeferCleanup(ctx *analysisContext, node ast.Node) bool {
	block := enclosingBlock(ctx, node)
	if block == nil {
		return false
	}
	for _, stmt := range block.List {
		if stmtContainsCallNamed(stmt, "DeferCleanup") {
			return true
		}
	}
	return false
}

func enclosingBlock(ctx *analysisContext, node ast.Node) *ast.BlockStmt {
	for current := node; current != nil; current = ctx.parent(current) {
		if block, ok := current.(*ast.BlockStmt); ok {
			return block
		}
		if _, ok := current.(*ast.FuncDecl); ok {
			return nil
		}
	}
	return nil
}

func stmtContainsCallNamed(stmt ast.Stmt, name string) bool {
	found := false
	ast.Inspect(stmt, func(node ast.Node) bool {
		if found || node == nil {
			return false
		}
		call, ok := node.(*ast.CallExpr)
		if ok && callName(call) == name {
			found = true
			return false
		}
		return true
	})
	return found
}
