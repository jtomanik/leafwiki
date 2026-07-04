package semantichygiene

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"
)

func checkGinkgoSpecQualityCall(ctx *analysisContext, call *ast.CallExpr) {
	if !isTestFile(ctx.filename(call.Pos())) {
		return
	}
	name := callName(call)
	checkGinkgoTopLevelIt(ctx, call, name)
	checkGinkgoTestName(ctx, call, name)
	checkGinkgoCoverageName(ctx, call, name)
	checkGinkgoVagueName(ctx, call, name)
	checkGinkgoBooleanOutcomeName(ctx, call, name)
	checkGinkgoTestingTInSpec(ctx, call, name)
	checkGinkgoFailInSpec(ctx, call, name)
	checkGinkgoTaxonomyLabels(ctx, call, name)
	switch {
	case isFocusedGinkgoNodeName(name):
		ctx.report(ruleGinkgoFocus, call, ginkgoFocusDiagnostic())
	case isPendingGinkgoNodeName(name):
		ctx.report(ruleGinkgoPending, call, ginkgoPendingDiagnostic())
	}
	if isGinkgoContainerNodeName(name) {
		checkGinkgoContainerBody(ctx, call)
	}
	if isGinkgoRunnableNodeName(name) || isDescribeTableCall(name) {
		checkGinkgoDecoratorArgs(ctx, call)
	}
	if isBDDEntryCall(name) {
		checkGinkgoEntryPolicy(ctx, call)
	}
}

func checkGinkgoTopLevelIt(ctx *analysisContext, call *ast.CallExpr, name string) {
	if !isTopLevelItCandidateName(name) || !isGinkgoDSLCall(ctx, call) {
		return
	}
	if enclosingGinkgoContainerBlock(ctx, call) != nil {
		return
	}
	ctx.report(ruleGinkgoTopLevelIt, call, ginkgoTopLevelItDiagnostic())
}

func checkGinkgoTestName(ctx *analysisContext, call *ast.CallExpr, name string) {
	if !isGinkgoNameCarrier(name) || !isGinkgoDSLCall(ctx, call) || len(call.Args) == 0 {
		return
	}
	reportGinkgoTestName(ctx, call.Args[0])
	if isDescribeTableCall(name) {
		for _, arg := range call.Args[1:] {
			reportGinkgoTableEntryDescriptionName(ctx, arg)
		}
	}
}

func checkGinkgoCoverageName(ctx *analysisContext, call *ast.CallExpr, name string) {
	if !isGinkgoNameCarrier(name) || !isGinkgoDSLCall(ctx, call) || len(call.Args) == 0 {
		return
	}
	reportGinkgoCoverageName(ctx, call.Args[0])
	if isDescribeTableCall(name) {
		for _, arg := range call.Args[1:] {
			reportGinkgoTableEntryCoverageName(ctx, arg)
		}
	}
}

func checkGinkgoVagueName(ctx *analysisContext, call *ast.CallExpr, name string) {
	if !isGinkgoNameCarrier(name) || !isGinkgoDSLCall(ctx, call) || len(call.Args) == 0 {
		return
	}
	reportGinkgoVagueName(ctx, call.Args[0])
	if isDescribeTableCall(name) {
		for _, arg := range call.Args[1:] {
			reportGinkgoTableEntryVagueName(ctx, arg)
		}
	}
}

func checkGinkgoBooleanOutcomeName(ctx *analysisContext, call *ast.CallExpr, name string) {
	if !isGinkgoNameCarrier(name) || !isGinkgoDSLCall(ctx, call) || len(call.Args) == 0 {
		return
	}
	reportGinkgoBooleanOutcomeName(ctx, call.Args[0])
	if isDescribeTableCall(name) {
		for _, arg := range call.Args[1:] {
			reportGinkgoTableEntryBooleanOutcomeName(ctx, arg)
		}
	}
}

func reportGinkgoTestName(ctx *analysisContext, expr ast.Expr) {
	description, ok := ginkgoStaticDescription(expr)
	if !ok || !ginkgoDescriptionLooksMigratedTestName(description) {
		return
	}
	ctx.report(ruleGinkgoTestName, expr, ginkgoTestNameDiagnostic(description))
}

func reportGinkgoCoverageName(ctx *analysisContext, expr ast.Expr) {
	description, ok := ginkgoStaticDescription(expr)
	if !ok || !ginkgoDescriptionUsesCoverageBucket(description) {
		return
	}
	ctx.report(ruleGinkgoCoverageName, expr, ginkgoCoverageNameDiagnostic(description))
}

func reportGinkgoVagueName(ctx *analysisContext, expr ast.Expr) {
	description, ok := ginkgoStaticDescription(expr)
	if !ok || !ginkgoDescriptionIsVague(description) {
		return
	}
	ctx.report(ruleGinkgoVagueName, expr, ginkgoVagueNameDiagnostic(description))
}

func reportGinkgoBooleanOutcomeName(ctx *analysisContext, expr ast.Expr) {
	description, ok := ginkgoStaticDescription(expr)
	if !ok || !ginkgoDescriptionUsesBooleanOutcome(description) {
		return
	}
	ctx.report(ruleGinkgoBooleanOutcomeName, expr, ginkgoBooleanOutcomeNameDiagnostic(description))
}

func reportGinkgoTableEntryDescriptionName(ctx *analysisContext, expr ast.Expr) {
	description, ok := ginkgoEntryDescriptionValue(expr)
	if !ok || !ginkgoDescriptionLooksMigratedTestName(description) {
		return
	}
	ctx.report(ruleGinkgoTestName, expr, ginkgoTestNameDiagnostic(description))
}

func reportGinkgoTableEntryCoverageName(ctx *analysisContext, expr ast.Expr) {
	description, ok := ginkgoEntryDescriptionValue(expr)
	if !ok || !ginkgoDescriptionUsesCoverageBucket(description) {
		return
	}
	ctx.report(ruleGinkgoCoverageName, expr, ginkgoCoverageNameDiagnostic(description))
}

func reportGinkgoTableEntryVagueName(ctx *analysisContext, expr ast.Expr) {
	description, ok := ginkgoEntryDescriptionValue(expr)
	if !ok || !ginkgoDescriptionIsVague(description) {
		return
	}
	ctx.report(ruleGinkgoVagueName, expr, ginkgoVagueNameDiagnostic(description))
}

func reportGinkgoTableEntryBooleanOutcomeName(ctx *analysisContext, expr ast.Expr) {
	description, ok := ginkgoEntryDescriptionValue(expr)
	if !ok || !ginkgoDescriptionUsesBooleanOutcome(description) {
		return
	}
	ctx.report(ruleGinkgoBooleanOutcomeName, expr, ginkgoBooleanOutcomeNameDiagnostic(description))
}

func ginkgoDescriptionUsesCoverageBucket(description string) bool {
	fields := strings.FieldsFunc(strings.ToLower(description), func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9')
	})
	for i, field := range fields {
		if field == "coverage" {
			return true
		}
		if i == 0 && isCoverageBucketVerb(field) {
			return true
		}
		if i > 0 && isCoverageBucketBranchPhrase(fields[i-1], field) {
			return true
		}
	}
	return false
}

func isCoverageBucketVerb(field string) bool {
	switch field {
	case "cover", "covers", "covered", "covering",
		"exercise", "exercises", "exercised", "exercising":
		return true
	default:
		return false
	}
}

func isCoverageBucketBranchPhrase(previous string, current string) bool {
	return previous == "edge" && (current == "branch" || current == "branches")
}

func ginkgoDescriptionIsVague(description string) bool {
	normalized := strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(description))), " ")
	if normalized == "helper behavior" ||
		normalized == "helper behaviour" ||
		strings.HasSuffix(normalized, " helper behavior") ||
		strings.HasSuffix(normalized, " helper behaviour") {
		return true
	}
	switch normalized {
	case "preserves behavior",
		"preserves existing behavior",
		"keeps behavior",
		"keeps existing behavior",
		"works",
		"handles cases",
		"handles edge cases",
		"does the right thing",
		"missing required",
		"parse",
		"read",
		"seed",
		"marshal",
		"write",
		"write output":
		return true
	default:
		return false
	}
}

func ginkgoDescriptionUsesBooleanOutcome(description string) bool {
	fields := strings.FieldsFunc(strings.ToLower(description), func(r rune) bool {
		return !(r >= 'a' && r <= 'z')
	})
	if len(fields) < 2 {
		return false
	}
	if fields[0] != "return" && fields[0] != "returns" {
		return false
	}
	return fields[1] == "true" || fields[1] == "false"
}

func ginkgoDescriptionLooksMigratedTestName(description string) bool {
	if strings.HasPrefix(description, "Test") {
		return true
	}
	fields := strings.Fields(description)
	for _, field := range fields {
		if hasMigratedIdentifierFragment(field) {
			return true
		}
	}
	if len(fields) < 2 {
		return false
	}

	titleCaseCount := 0
	for _, field := range fields {
		if isMigratedTitleCaseFragment(field) {
			titleCaseCount++
		}
	}
	if titleCaseCount >= 3 {
		return true
	}
	return len(fields) <= 4 &&
		titleCaseCount == len(fields)-1 &&
		isLowercaseWord(fields[0])
}

func hasMigratedIdentifierFragment(field string) bool {
	field = trimGinkgoNamePunctuation(field)
	if field == "" {
		return false
	}
	if isGoCodeSymbolFragment(field) {
		return true
	}
	if strings.Contains(field, "_") {
		parts := strings.FieldsFunc(field, func(r rune) bool {
			return r == '_'
		})
		if len(parts) > 1 {
			return true
		}
	}
	return isLowerCamelIdentifierFragment(field)
}

func isGoCodeSymbolFragment(field string) bool {
	parts := strings.Split(field, ".")
	if len(parts) < 2 {
		return false
	}
	hasExportedOrCamelPart := false
	for _, part := range parts {
		if !isGoIdentifierLike(part) {
			return false
		}
		if isExportedIdentifierFragment(part) || isLowerCamelIdentifierFragment(part) {
			hasExportedOrCamelPart = true
		}
	}
	return hasExportedOrCamelPart
}

func isGoIdentifierLike(field string) bool {
	if field == "" {
		return false
	}
	for i, r := range field {
		if i == 0 {
			if r == '_' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' {
				continue
			}
			return false
		}
		if r == '_' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			continue
		}
		return false
	}
	return true
}

func isExportedIdentifierFragment(field string) bool {
	return len(field) >= 2 &&
		field[0] >= 'A' && field[0] <= 'Z' &&
		field[1] >= 'a' && field[1] <= 'z'
}

func isLowerCamelIdentifierFragment(field string) bool {
	if len(field) < 4 || field[0] < 'a' || field[0] > 'z' {
		return false
	}
	for i := 1; i < len(field); i++ {
		if field[i] >= 'A' && field[i] <= 'Z' {
			return true
		}
	}
	return false
}

func isMigratedTitleCaseFragment(field string) bool {
	field = trimGinkgoNamePunctuation(field)
	if len(field) < 4 {
		return false
	}
	return field[0] >= 'A' && field[0] <= 'Z' &&
		field[1] >= 'a' && field[1] <= 'z'
}

func isLowercaseWord(field string) bool {
	field = trimGinkgoNamePunctuation(field)
	if field == "" {
		return false
	}
	for _, r := range field {
		if r < 'a' || r > 'z' {
			return false
		}
	}
	return true
}

func trimGinkgoNamePunctuation(field string) string {
	return strings.Trim(field, " \t\r\n.,:;!?()[]{}\"'`")
}

func isGinkgoNameCarrier(name string) bool {
	return isGinkgoSpecNodeName(name) ||
		isGinkgoContainerNodeName(name) ||
		isDescribeTableCall(name) ||
		isBDDEntryCall(name)
}

func ginkgoStaticDescription(expr ast.Expr) (string, bool) {
	if value, ok := stringLiteralValue(expr); ok {
		return value, true
	}
	return ginkgoEntryDescriptionValue(expr)
}

func ginkgoEntryDescriptionValue(expr ast.Expr) (string, bool) {
	call, ok := unparenExpr(expr).(*ast.CallExpr)
	if !ok || callName(call) != "EntryDescription" || len(call.Args) == 0 {
		return "", false
	}
	return stringLiteralValue(call.Args[0])
}

func checkGinkgoTestingTInSpec(ctx *analysisContext, call *ast.CallExpr, name string) {
	if !isGinkgoSubjectBodyNodeName(name) || !isGinkgoDSLCall(ctx, call) {
		return
	}
	body, ok := firstFuncLitArg(call)
	if !ok {
		return
	}
	ast.Inspect(body.Body, func(node ast.Node) bool {
		if node == nil {
			return false
		}
		if nested, ok := node.(*ast.FuncLit); ok && nested != body {
			return false
		}
		candidate, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		if name, ok := ginkgoTestingTAssertion(ctx, candidate); ok {
			ctx.report(ruleGinkgoTestingTInSpec, candidate, ginkgoTestingTAssertionDiagnostic(name))
			return true
		}
		if name, ok := ginkgoTestingTAdapterMethod(ctx, candidate); ok {
			ctx.report(ruleGinkgoTestingTInSpec, candidate, ginkgoTestingTInSpecDiagnostic(name))
			return true
		}
		name, ok := ginkgoTestingTAdapter(ctx, candidate)
		if !ok {
			return true
		}
		ctx.report(ruleGinkgoTestingTInSpec, candidate, ginkgoTestingTInSpecDiagnostic(name))
		return true
	})
}

func checkGinkgoFailInSpec(ctx *analysisContext, call *ast.CallExpr, name string) {
	if !isGinkgoSubjectBodyNodeName(name) || !isGinkgoDSLCall(ctx, call) {
		return
	}
	body, ok := firstFuncLitArg(call)
	if !ok {
		return
	}
	ast.Inspect(body.Body, func(node ast.Node) bool {
		if node == nil {
			return false
		}
		candidate, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		if isGinkgoFailCall(ctx, candidate) {
			ctx.report(ruleGinkgoFailInSpec, candidate, ginkgoFailInSpecDiagnostic())
			return true
		}
		if name, ok := ginkgoFailureHelperCall(ctx, candidate); ok {
			ctx.report(ruleGinkgoFailInSpec, candidate, ginkgoFailureHelperInSpecDiagnostic(name))
			return true
		}
		if name, ok := ginkgoHiddenFailHelperCall(ctx, candidate); ok {
			ctx.report(ruleGinkgoFailInSpec, candidate, ginkgoHiddenFailHelperInSpecDiagnostic(name))
			return true
		}
		return true
	})
}

func isGinkgoFailCall(ctx *analysisContext, call *ast.CallExpr) bool {
	packagePath, name := calleePackageAndName(ctx, call)
	return packagePath == "github.com/onsi/ginkgo/v2" && name == "Fail"
}

func ginkgoFailureHelperCall(ctx *analysisContext, call *ast.CallExpr) (string, bool) {
	if _, ok := unparenExpr(call.Fun).(*ast.Ident); !ok {
		return "", false
	}
	_, name := calleePackageAndName(ctx, call)
	if !isFailureHelperName(name) {
		return "", false
	}
	return name, true
}

func isFailureHelperName(name string) bool {
	return hasFailureHelperPrefix(name, "fail") || hasFailureHelperPrefix(name, "fatal")
}

func ginkgoHiddenFailHelperCall(ctx *analysisContext, call *ast.CallExpr) (string, bool) {
	if _, ok := unparenExpr(call.Fun).(*ast.Ident); !ok {
		return "", false
	}
	fn := localFuncDeclForCall(ctx, call)
	if fn == nil ||
		fn.Body == nil ||
		fn.Name == nil ||
		!funcHasGinkgoHelperCall(fn.Body) ||
		!funcBodyContainsGinkgoFail(ctx, fn.Body) {
		return "", false
	}
	return fn.Name.Name, true
}

func localFuncDeclForCall(ctx *analysisContext, call *ast.CallExpr) *ast.FuncDecl {
	target := calledFunctionObject(ctx, call)
	if target == nil || target.Pkg() == nil || target.Pkg().Path() != ctx.pass.Pkg.Path() {
		return nil
	}
	for _, file := range ctx.pass.Files {
		var found *ast.FuncDecl
		ast.Inspect(file, func(node ast.Node) bool {
			if found != nil || node == nil {
				return false
			}
			fn, ok := node.(*ast.FuncDecl)
			if !ok || fn.Name == nil {
				return true
			}
			if ctx.pass.TypesInfo.ObjectOf(fn.Name) == target {
				found = fn
				return false
			}
			return true
		})
		if found != nil {
			return found
		}
	}
	return nil
}

func funcBodyContainsGinkgoFail(ctx *analysisContext, body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(node ast.Node) bool {
		if found || node == nil {
			return false
		}
		if _, ok := node.(*ast.FuncLit); ok {
			return false
		}
		call, ok := node.(*ast.CallExpr)
		if ok && isGinkgoFailCall(ctx, call) {
			found = true
			return false
		}
		return true
	})
	return found
}

func hasFailureHelperPrefix(name string, prefix string) bool {
	lower := strings.ToLower(name)
	if lower == prefix || lower == prefix+"f" || lower == prefix+"now" {
		return true
	}
	if !strings.HasPrefix(lower, prefix) || len(name) <= len(prefix) {
		return false
	}
	next := name[len(prefix)]
	return next >= 'A' && next <= 'Z'
}

func ginkgoTestingTAssertion(ctx *analysisContext, call *ast.CallExpr) (string, bool) {
	selector, ok := unparenExpr(call.Fun).(*ast.SelectorExpr)
	if !ok || !isTestingTFailureMethod(selector.Sel.Name) {
		return "", false
	}
	receiverType := ctx.pass.TypesInfo.TypeOf(selector.X)
	if isTestingTType(receiverType) {
		return "testing.T." + selector.Sel.Name, true
	}
	if isTestingTLikeAdapterType(receiverType) {
		return "testing.T-like." + selector.Sel.Name, true
	}
	return "", false
}

func ginkgoTestingTAdapterMethod(ctx *analysisContext, call *ast.CallExpr) (string, bool) {
	selector, ok := unparenExpr(call.Fun).(*ast.SelectorExpr)
	if !ok || !isTestingTFixtureMethod(selector.Sel.Name) {
		return "", false
	}
	receiverType := ctx.pass.TypesInfo.TypeOf(selector.X)
	if isTestingTType(receiverType) {
		return "testing.T." + selector.Sel.Name, true
	}
	if isTestingTLikeAdapterType(receiverType) {
		return "testing.T-like." + selector.Sel.Name, true
	}
	return "", false
}

func isTestingTFixtureMethod(name string) bool {
	switch name {
	case "Cleanup", "Helper", "Log", "Logf", "Name", "Setenv", "Skip", "Skipf", "SkipNow", "TempDir":
		return true
	default:
		return false
	}
}

func isTestingTLikeAdapterType(typ types.Type) bool {
	if typ == nil || isTestingTType(typ) {
		return false
	}
	methods := map[string]struct{}{}
	collectTestingTLikeMethods(types.Unalias(typ), methods)
	if testingTLikeMethodsHaveStrongSignal(methods) {
		return true
	}
	named := namedType(typ)
	return named != nil && strings.Contains(strings.ToLower(named.Obj().Name()), "testt") && len(methods) >= 1
}

func testingTLikeMethodsHaveStrongSignal(methods map[string]struct{}) bool {
	for _, name := range []string{
		"Fatalf",
		"Fatal",
		"Errorf",
		"Helper",
		"Cleanup",
		"Setenv",
		"Skip",
		"Skipf",
		"SkipNow",
		"TempDir",
	} {
		if _, ok := methods[name]; ok {
			return true
		}
	}
	return false
}

func collectTestingTLikeMethods(typ types.Type, methods map[string]struct{}) {
	if typ == nil {
		return
	}
	methodSet := types.NewMethodSet(typ)
	for i := 0; i < methodSet.Len(); i++ {
		name := methodSet.At(i).Obj().Name()
		if isTestingTFailureMethod(name) || isTestingTFixtureMethod(name) {
			methods[name] = struct{}{}
		}
	}
	if _, ok := typ.(*types.Pointer); ok {
		return
	}
	if named, ok := typ.(*types.Named); ok {
		collectTestingTLikeMethods(types.NewPointer(named), methods)
	}
}

func isTestingTFailureMethod(name string) bool {
	switch name {
	case "Fatalf", "Fatal", "Errorf", "Error":
		return true
	default:
		return false
	}
}

func isTestingTType(typ types.Type) bool {
	typ = types.Unalias(typ)
	if ptr, ok := typ.(*types.Pointer); ok {
		typ = ptr.Elem()
	}
	named, ok := typ.(*types.Named)
	if !ok || named.Obj().Pkg() == nil {
		return false
	}
	return named.Obj().Pkg().Path() == "testing" && named.Obj().Name() == "T"
}

func ginkgoTestingTAdapter(ctx *analysisContext, call *ast.CallExpr) (string, bool) {
	packagePath, name := calleePackageAndName(ctx, call)
	if packagePath != "github.com/onsi/ginkgo/v2" {
		return "", false
	}
	switch name {
	case "GinkgoT", "GinkgoTB":
		return name, true
	default:
		return "", false
	}
}

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
	if len(call.Args) > 5 {
		ctx.report(ruleGinkgoWideEntry, call, ginkgoWideEntryDiagnostic())
	}
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
