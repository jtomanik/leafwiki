package semantichygiene

import (
	"go/ast"
	"go/token"
	"go/types"
	"strconv"
	"strings"
)

type gomegaAssertion struct {
	call    *ast.CallExpr
	method  string
	source  *ast.CallExpr
	actual  ast.Expr
	matcher *ast.CallExpr
}

func checkGomegaSemanticMatcher(ctx *analysisContext, call *ast.CallExpr) {
	if !isTestFile(ctx.filename(call.Pos())) {
		return
	}
	assertion, ok := gomegaAssertionFromCall(ctx, call)
	if !ok {
		return
	}
	if assertionUsesErrError(ctx, assertion) && isMatcherNamed(assertion.matcher, "Equal", "ContainSubstring") {
		ctx.pass.Reportf(assertion.actual.Pos(), "%s", gomegaErrorStringMatcherDiagnostic())
	}
	if assertionUsesRawStringMatchError(ctx, assertion) {
		ctx.pass.Reportf(assertion.matcher.Pos(), "%s", gomegaRawStringMatchErrorDiagnostic())
	}
	if assertionUsesErrorNilMatcher(ctx, assertion) {
		ctx.pass.Reportf(assertion.actual.Pos(), "%s", gomegaErrorNilMatcherDiagnostic())
	}
	if assertionUsesInlineErrorReturnHaveOccurred(ctx, assertion) {
		ctx.pass.Reportf(assertion.actual.Pos(), "%s", gomegaInlineErrorSucceedDiagnostic())
	}
	if assertionUsesMultiReturnErrorMatcher(ctx, assertion) {
		ctx.pass.Reportf(assertion.actual.Pos(), "%s", gomegaMultiReturnErrorMatcherDiagnostic())
	}
	if assertionUsesStringsContains(ctx, assertion) && isBooleanMatcher(assertion.matcher) {
		ctx.pass.Reportf(assertion.actual.Pos(), "%s", gomegaStringsContainsMatcherDiagnostic())
	}
	if assertionUsesStringsPredicate(ctx, assertion, "HasPrefix") && isBooleanMatcher(assertion.matcher) {
		ctx.pass.Reportf(assertion.actual.Pos(), "%s", gomegaStringPredicateMatcherDiagnostic("strings.HasPrefix", "HavePrefix"))
	}
	if assertionUsesStringsPredicate(ctx, assertion, "HasSuffix") && isBooleanMatcher(assertion.matcher) {
		ctx.pass.Reportf(assertion.actual.Pos(), "%s", gomegaStringPredicateMatcherDiagnostic("strings.HasSuffix", "HaveSuffix"))
	}
	if assertionUsesRegexpMatchString(ctx, assertion) && isBooleanMatcher(assertion.matcher) {
		ctx.pass.Reportf(assertion.actual.Pos(), "%s", gomegaRegexpMatchStringDiagnostic())
	}
	if assertionUsesErrorsIs(ctx, assertion) && isBooleanMatcher(assertion.matcher) {
		ctx.pass.Reportf(assertion.actual.Pos(), "%s", gomegaErrorsIsMatcherDiagnostic())
	}
	if assertionUsesErrorsAs(ctx, assertion) && isBooleanMatcher(assertion.matcher) {
		ctx.pass.Reportf(assertion.actual.Pos(), "%s", gomegaErrorsAsMatcherDiagnostic())
	}
	if assertionUsesOSIsNotExist(ctx, assertion) && isBooleanMatcher(assertion.matcher) {
		ctx.pass.Reportf(assertion.actual.Pos(), "%s", gomegaOSIsNotExistMatcherDiagnostic())
	}
	if assertionUsesLenEqual(assertion) {
		ctx.pass.Reportf(assertion.actual.Pos(), "%s", gomegaLenMatcherDiagnostic())
	}
	if assertionUsesBinaryBoolean(assertion) {
		ctx.pass.Reportf(assertion.actual.Pos(), "%s", gomegaBinaryBooleanMatcherDiagnostic())
	}
	if assertionUsesMapIndexEqual(ctx, assertion) {
		ctx.pass.Reportf(assertion.actual.Pos(), "%s", gomegaMapIndexMatcherDiagnostic())
	}
	if assertionUsesHTTPStatusEqual(ctx, assertion) {
		ctx.pass.Reportf(assertion.actual.Pos(), "%s", gomegaHTTPStatusMatcherDiagnostic())
	}
	if assertionUsesHTTPBodyString(ctx, assertion) {
		ctx.pass.Reportf(assertion.actual.Pos(), "%s", gomegaHTTPBodyMatcherDiagnostic())
	}
	if assertionUsesRepeatedHTTPBodyMatcher(ctx, assertion) {
		ctx.pass.Reportf(assertion.matcher.Pos(), "%s", gomegaRepeatedHTTPBodyMatcherDiagnostic())
	}
	if assertionUsesHTTPHeaderGet(ctx, assertion) {
		ctx.pass.Reportf(assertion.actual.Pos(), "%s", gomegaHTTPHeaderMatcherDiagnostic())
	}
	if assertionUsesNumericBeEquivalentTo(ctx, assertion) {
		ctx.pass.Reportf(assertion.matcher.Pos(), "%s", gomegaNumericEquivalentDiagnostic())
	}
	if assertionUsesTimeEqual(ctx, assertion) {
		ctx.pass.Reportf(assertion.actual.Pos(), "%s", gomegaTimeEqualDiagnostic())
	}
	if assertionUsesEqualEmpty(assertion) {
		ctx.pass.Reportf(assertion.matcher.Pos(), "%s", gomegaEqualEmptyDiagnostic())
	}
	if assertionUsesEqualZero(assertion) {
		ctx.pass.Reportf(assertion.matcher.Pos(), "%s", gomegaEqualZeroDiagnostic())
	}
	if assertionUsesRepeatedFieldAssertion(ctx, assertion) {
		ctx.pass.Reportf(assertion.actual.Pos(), "%s", gomegaRepeatedFieldAssertionDiagnostic())
	}
	if assertionUsesCollectionIndexAssertion(ctx, assertion) {
		ctx.pass.Reportf(assertion.actual.Pos(), "%s", gomegaCollectionIndexAssertionDiagnostic())
	}
	if fieldName, ok := assertionMatchesStructuredErrorField(ctx, assertion); ok {
		ctx.pass.Reportf(assertion.actual.Pos(), "%s", gomegaStructuredErrorMatcherDiagnostic(fieldName))
	}
	if fieldName, ok := assertionUsesStructuredFieldMatcher(ctx, assertion); ok {
		ctx.pass.Reportf(assertion.matcher.Pos(), "%s", gomegaStructuredErrorMatcherDiagnostic(fieldName))
	}
	if keyName, ok := assertionUsesStructuredProtocolKeyMatcher(ctx, assertion); ok {
		ctx.pass.Reportf(assertion.matcher.Pos(), "%s", gomegaStructuredProtocolKeyMatcherDiagnostic(keyName))
	}
	if matcherName, ok := assertionUsesStructuredProtocolPayloadMatcher(ctx, assertion); ok {
		ctx.pass.Reportf(assertion.matcher.Pos(), "%s", gomegaStructuredProtocolPayloadMatcherDiagnostic(matcherName))
	}
}

func checkGomegaAsyncAssertion(ctx *analysisContext, call *ast.CallExpr) {
	if !isTestFile(ctx.filename(call.Pos())) {
		return
	}
	assertion, ok := gomegaAsyncAssertionFromCall(call)
	if !ok {
		return
	}
	if assertion.sourceName == "Eventually" && isNegativeAssertionMethod(assertion.method) && isMatcherNamed(assertion.matcher, "Receive") {
		ctx.pass.Reportf(assertion.call.Pos(), "%s", gomegaAsyncNegativeReceiveDiagnostic())
	}
	if matcherTreeContainsRawStringMatchError(ctx, assertion.matcher) {
		ctx.pass.Reportf(assertion.matcher.Pos(), "%s", gomegaRawStringMatchErrorDiagnostic())
	}
	if assertion.sourceName == "Eventually" && !eventuallyBareActualAllowed(ctx, assertion.actual) {
		ctx.pass.Reportf(assertion.actual.Pos(), "%s", gomegaAsyncBareValueDiagnostic())
	}
	if asyncAssertionRequiresSpecContext(ctx, assertion) {
		ctx.pass.Reportf(assertion.source.Pos(), "%s", ginkgoAsyncContextDiagnostic())
	}
	if assertionUsesAsyncBooleanMatcher(ctx, assertion) {
		ctx.pass.Reportf(assertion.actual.Pos(), "%s", gomegaAsyncBooleanMatcherDiagnostic())
	}
}

func checkGomegaAsyncCallback(ctx *analysisContext, call *ast.CallExpr) {
	if !isTestFile(ctx.filename(call.Pos())) || !isGomegaAsyncCall(callName(call)) {
		return
	}
	for _, arg := range call.Args {
		fn, ok := arg.(*ast.FuncLit)
		if !ok || !funcLitHasGomegaParam(fn) {
			continue
		}
		reportGlobalExpectCallsInAsyncCallback(ctx, fn)
	}
}

func checkGomegaAssertionHelperOffset(ctx *analysisContext, fn *ast.FuncDecl) {
	if fn.Body == nil ||
		!isTestFile(ctx.filename(fn.Pos())) ||
		!isTestAssertionHelperName(fn.Name.Name) ||
		!funcContainsGomegaAssertion(ctx, fn.Body) ||
		funcHasGomegaHelperReporting(ctx, fn) {
		return
	}
	ctx.pass.Reportf(fn.Name.Pos(), "%s", gomegaHelperOffsetDiagnostic(fn.Name.Name))
}

func checkGomegaMatcherFactorySignature(ctx *analysisContext, fn *ast.FuncDecl) {
	if fn.Body == nil ||
		fn.Type.Params == nil ||
		!gomegaMatcherFactoryName(fn.Name.Name) ||
		!funcReturnsGomegaMatcher(fn) {
		return
	}
	filename := ctx.filename(fn.Pos())
	if !isTestFile(filename) && !isTestSupportFile(filename) {
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
			if semanticType, ok := semanticTypeForTestHelperParamName(name.Name, fn.Name.Name); ok {
				ctx.pass.Reportf(name.Pos(), "%s", customMatcherSemanticParameterDiagnostic(fn.Name.Name, name.Name, semanticType))
				continue
			}
			if testHelperMessageParamName(name.Name, fn.Name.Name) {
				ctx.pass.Reportf(name.Pos(), "%s", customMatcherMessageParameterDiagnostic(fn.Name.Name, name.Name))
				continue
			}
			if testHelperFieldParamName(name.Name, fn.Name.Name) {
				ctx.pass.Reportf(name.Pos(), "%s", customMatcherFieldParameterDiagnostic(fn.Name.Name, name.Name))
			}
		}
	}
}

func gomegaAssertionFromCall(ctx *analysisContext, call *ast.CallExpr) (gomegaAssertion, bool) {
	method := callName(call)
	if !isGomegaAssertionMethod(method) || len(call.Args) == 0 {
		return gomegaAssertion{}, false
	}
	matcher, ok := call.Args[0].(*ast.CallExpr)
	if !ok {
		return gomegaAssertion{}, false
	}
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return gomegaAssertion{}, false
	}
	source, ok := gomegaSourceCall(selector.X)
	if !ok {
		return gomegaAssertion{}, false
	}
	actual, ok := gomegaExpectationActual(source)
	if !ok {
		return gomegaAssertion{}, false
	}
	return gomegaAssertion{
		call:    call,
		method:  method,
		source:  source,
		actual:  actual,
		matcher: matcher,
	}, true
}

type gomegaAsyncAssertion struct {
	call       *ast.CallExpr
	method     string
	source     *ast.CallExpr
	sourceName string
	actual     ast.Expr
	matcher    *ast.CallExpr
}

func gomegaAsyncAssertionFromCall(call *ast.CallExpr) (gomegaAsyncAssertion, bool) {
	method := callName(call)
	if !isGomegaAssertionMethod(method) || len(call.Args) == 0 {
		return gomegaAsyncAssertion{}, false
	}
	matcher, ok := call.Args[0].(*ast.CallExpr)
	if !ok {
		return gomegaAsyncAssertion{}, false
	}
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return gomegaAsyncAssertion{}, false
	}
	source, ok := gomegaAsyncSourceCall(selector.X)
	if !ok {
		return gomegaAsyncAssertion{}, false
	}
	actual, ok := gomegaAsyncActual(source)
	if !ok {
		return gomegaAsyncAssertion{}, false
	}
	return gomegaAsyncAssertion{
		call:       call,
		method:     method,
		source:     source,
		sourceName: callName(source),
		actual:     actual,
		matcher:    matcher,
	}, true
}

func gomegaSourceCall(expr ast.Expr) (*ast.CallExpr, bool) {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return nil, false
	}
	if isGomegaExpectationSource(callName(call)) {
		return call, true
	}
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return nil, false
	}
	return gomegaSourceCall(selector.X)
}

func gomegaAsyncSourceCall(expr ast.Expr) (*ast.CallExpr, bool) {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return nil, false
	}
	if isGomegaAsyncCall(callName(call)) {
		return call, true
	}
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return nil, false
	}
	return gomegaAsyncSourceCall(selector.X)
}

func gomegaExpectationActual(call *ast.CallExpr) (ast.Expr, bool) {
	switch callName(call) {
	case "Expect", "Ω":
		if len(call.Args) == 0 {
			return nil, false
		}
		return call.Args[0], true
	case "ExpectWithOffset":
		if len(call.Args) < 2 {
			return nil, false
		}
		return call.Args[1], true
	default:
		return nil, false
	}
}

func gomegaAsyncActual(call *ast.CallExpr) (ast.Expr, bool) {
	switch callName(call) {
	case "Eventually", "Consistently":
		if len(call.Args) == 0 {
			return nil, false
		}
		return call.Args[0], true
	case "EventuallyWithOffset", "ConsistentlyWithOffset":
		if len(call.Args) < 2 {
			return nil, false
		}
		return call.Args[1], true
	default:
		return nil, false
	}
}

func isGomegaExpectationSource(name string) bool {
	switch name {
	case "Expect", "ExpectWithOffset", "Ω":
		return true
	default:
		return false
	}
}

func isGomegaAsyncCall(name string) bool {
	switch name {
	case "Eventually", "EventuallyWithOffset", "Consistently", "ConsistentlyWithOffset":
		return true
	default:
		return false
	}
}

func isNegativeAssertionMethod(name string) bool {
	switch name {
	case "NotTo", "ToNot", "ShouldNot":
		return true
	default:
		return false
	}
}

func assertionUsesErrError(ctx *analysisContext, assertion gomegaAssertion) bool {
	call, ok := assertion.actual.(*ast.CallExpr)
	if !ok || callName(call) != "Error" {
		return false
	}
	selector, ok := call.Fun.(*ast.SelectorExpr)
	return ok && typeImplementsError(ctx.pass.TypesInfo.TypeOf(selector.X))
}

func typeImplementsError(typ types.Type) bool {
	if typ == nil {
		return false
	}
	errorType, ok := types.Universe.Lookup("error").Type().Underlying().(*types.Interface)
	if !ok {
		return false
	}
	return types.Implements(typ, errorType)
}

func assertionUsesErrorNilMatcher(ctx *analysisContext, assertion gomegaAssertion) bool {
	if !assertionTargetsError(ctx, assertion) {
		return false
	}
	return isMatcherNamed(assertion.matcher, "BeNil") || isEqualNilMatcher(assertion.matcher)
}

func assertionTargetsError(ctx *analysisContext, assertion gomegaAssertion) bool {
	return gomegaAssertionHasErrorProjection(assertion) || typeImplementsError(ctx.pass.TypesInfo.TypeOf(assertion.actual))
}

func gomegaAssertionHasErrorProjection(assertion gomegaAssertion) bool {
	for current := assertion.call; current != nil; {
		selector, ok := current.Fun.(*ast.SelectorExpr)
		if !ok {
			return false
		}
		source, ok := selector.X.(*ast.CallExpr)
		if !ok {
			return false
		}
		if callName(source) == "Error" {
			return true
		}
		if source == assertion.source {
			return false
		}
		current = source
	}
	return false
}

func isEqualNilMatcher(matcher *ast.CallExpr) bool {
	return isMatcherNamed(matcher, "Equal") && len(matcher.Args) == 1 && isNilExpr(matcher.Args[0])
}

func isNilExpr(expr ast.Expr) bool {
	ident, ok := expr.(*ast.Ident)
	return ok && ident.Name == "nil"
}

func assertionUsesInlineErrorReturnHaveOccurred(ctx *analysisContext, assertion gomegaAssertion) bool {
	if !isMatcherNamed(assertion.matcher, "HaveOccurred") ||
		!isNegativeAssertionMethod(assertion.method) ||
		gomegaAssertionHasErrorProjection(assertion) {
		return false
	}
	call, ok := assertion.actual.(*ast.CallExpr)
	return ok && callReturnsSingleError(ctx, call)
}

func assertionUsesMultiReturnErrorMatcher(ctx *analysisContext, assertion gomegaAssertion) bool {
	if !isMatcherNamed(assertion.matcher, "HaveOccurred", "Succeed") || gomegaAssertionHasErrorProjection(assertion) {
		return false
	}
	call, ok := assertion.actual.(*ast.CallExpr)
	return ok && callReturnsMultipleWithError(ctx, call)
}

func callReturnsSingleError(ctx *analysisContext, call *ast.CallExpr) bool {
	results := callResultTuple(ctx, call)
	return results != nil && results.Len() == 1 && typeImplementsError(results.At(0).Type())
}

func callReturnsMultipleWithError(ctx *analysisContext, call *ast.CallExpr) bool {
	results := callResultTuple(ctx, call)
	if results == nil || results.Len() < 2 {
		return false
	}
	for i := 0; i < results.Len(); i++ {
		if typeImplementsError(results.At(i).Type()) {
			return true
		}
	}
	return false
}

func callResultTuple(ctx *analysisContext, call *ast.CallExpr) *types.Tuple {
	if sig, ok := ctx.pass.TypesInfo.TypeOf(call.Fun).(*types.Signature); ok {
		return sig.Results()
	}
	if tuple, ok := ctx.pass.TypesInfo.TypeOf(call).(*types.Tuple); ok {
		return tuple
	}
	return nil
}

func assertionUsesStringsContains(ctx *analysisContext, assertion gomegaAssertion) bool {
	return assertionUsesStringsPredicate(ctx, assertion, "Contains")
}

func assertionUsesStringsPredicate(ctx *analysisContext, assertion gomegaAssertion, predicate string) bool {
	call, ok := assertion.actual.(*ast.CallExpr)
	if !ok {
		return false
	}
	packagePath, name := calleePackageAndName(ctx, call)
	return packagePath == "strings" && name == predicate
}

func assertionUsesRegexpMatchString(ctx *analysisContext, assertion gomegaAssertion) bool {
	call, ok := assertion.actual.(*ast.CallExpr)
	if !ok {
		return false
	}
	packagePath, name := calleePackageAndName(ctx, call)
	return packagePath == "regexp" && name == "MatchString"
}

func assertionUsesErrorsIs(ctx *analysisContext, assertion gomegaAssertion) bool {
	call, ok := assertion.actual.(*ast.CallExpr)
	if !ok {
		return false
	}
	packagePath, name := calleePackageAndName(ctx, call)
	return packagePath == "errors" && name == "Is"
}

func assertionUsesErrorsAs(ctx *analysisContext, assertion gomegaAssertion) bool {
	call, ok := assertion.actual.(*ast.CallExpr)
	if !ok {
		return false
	}
	packagePath, name := calleePackageAndName(ctx, call)
	return packagePath == "errors" && name == "As"
}

func assertionUsesOSIsNotExist(ctx *analysisContext, assertion gomegaAssertion) bool {
	call, ok := assertion.actual.(*ast.CallExpr)
	if !ok {
		return false
	}
	packagePath, name := calleePackageAndName(ctx, call)
	return packagePath == "os" && name == "IsNotExist"
}

func assertionUsesLenEqual(assertion gomegaAssertion) bool {
	call, ok := assertion.actual.(*ast.CallExpr)
	return ok && callName(call) == "len" && isMatcherNamed(assertion.matcher, "Equal")
}

func assertionUsesBinaryBoolean(assertion gomegaAssertion) bool {
	binary, ok := assertion.actual.(*ast.BinaryExpr)
	return ok && isComparisonOp(binary.Op) && isBooleanMatcher(assertion.matcher)
}

func isComparisonOp(op token.Token) bool {
	switch op {
	case token.EQL, token.NEQ, token.LSS, token.LEQ, token.GTR, token.GEQ:
		return true
	default:
		return false
	}
}

func assertionUsesMapIndexEqual(ctx *analysisContext, assertion gomegaAssertion) bool {
	if !isMatcherNamed(assertion.matcher, "Equal") {
		return false
	}
	index, ok := unparenExpr(assertion.actual).(*ast.IndexExpr)
	if !ok {
		return false
	}
	return typeIsMap(ctx.pass.TypesInfo.TypeOf(index.X))
}

func typeIsMap(typ types.Type) bool {
	if typ == nil {
		return false
	}
	_, ok := types.Unalias(typ).Underlying().(*types.Map)
	return ok
}

func typeIsSliceOrArray(typ types.Type) bool {
	if typ == nil {
		return false
	}
	switch types.Unalias(typ).Underlying().(type) {
	case *types.Slice, *types.Array:
		return true
	default:
		return false
	}
}

func assertionUsesHTTPStatusEqual(ctx *analysisContext, assertion gomegaAssertion) bool {
	if !isMatcherNamed(assertion.matcher, "Equal") {
		return false
	}
	selector, ok := unparenExpr(assertion.actual).(*ast.SelectorExpr)
	if !ok {
		return false
	}
	switch selector.Sel.Name {
	case "Code":
		return isNamedTypeFromPackage(ctx.pass.TypesInfo.TypeOf(selector.X), "net/http/httptest", "ResponseRecorder")
	case "StatusCode":
		return isNamedTypeFromPackage(ctx.pass.TypesInfo.TypeOf(selector.X), "net/http", "Response")
	default:
		return false
	}
}

func assertionUsesHTTPBodyString(ctx *analysisContext, assertion gomegaAssertion) bool {
	if !isMatcherNamed(assertion.matcher, "Equal", "ContainSubstring") {
		return false
	}
	call, ok := unparenExpr(assertion.actual).(*ast.CallExpr)
	if !ok || callName(call) != "String" {
		return false
	}
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	body, ok := unparenExpr(selector.X).(*ast.SelectorExpr)
	return ok && body.Sel.Name == "Body" &&
		isNamedTypeFromPackage(ctx.pass.TypesInfo.TypeOf(body.X), "net/http/httptest", "ResponseRecorder")
}

func assertionUsesRepeatedHTTPBodyMatcher(ctx *analysisContext, assertion gomegaAssertion) bool {
	if !isMatcherNamed(assertion.matcher, "HaveHTTPBody") {
		return false
	}
	stmt, ok := enclosingExprStmt(ctx, assertion.call)
	if !ok {
		return false
	}
	block, ok := ctx.parent(stmt).(*ast.BlockStmt)
	if !ok {
		return false
	}
	for index, candidate := range block.List {
		if candidate != stmt || index == 0 {
			continue
		}
		prev, ok := block.List[index-1].(*ast.ExprStmt)
		if !ok {
			return false
		}
		prevCall, ok := prev.X.(*ast.CallExpr)
		if !ok {
			return false
		}
		prevAssertion, ok := gomegaAssertionFromCall(ctx, prevCall)
		return ok &&
			isMatcherNamed(prevAssertion.matcher, "HaveHTTPBody") &&
			sameSimpleExpr(prevAssertion.actual, assertion.actual)
	}
	return false
}

func enclosingExprStmt(ctx *analysisContext, node ast.Node) (*ast.ExprStmt, bool) {
	for current := node; current != nil; current = ctx.parent(current) {
		if stmt, ok := current.(*ast.ExprStmt); ok {
			return stmt, true
		}
		if _, ok := current.(*ast.FuncDecl); ok {
			return nil, false
		}
	}
	return nil, false
}

func assertionUsesHTTPHeaderGet(ctx *analysisContext, assertion gomegaAssertion) bool {
	if !isMatcherNamed(assertion.matcher, "Equal", "ContainSubstring") {
		return false
	}
	call, ok := unparenExpr(assertion.actual).(*ast.CallExpr)
	if !ok || callName(call) != "Get" {
		return false
	}
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	header, ok := unparenExpr(selector.X).(*ast.SelectorExpr)
	if !ok || header.Sel.Name != "Header" {
		return false
	}
	return isNamedTypeFromPackage(ctx.pass.TypesInfo.TypeOf(header.X), "net/http", "Response") ||
		isNamedTypeFromPackage(ctx.pass.TypesInfo.TypeOf(header), "net/http", "Header")
}

func assertionUsesNumericBeEquivalentTo(ctx *analysisContext, assertion gomegaAssertion) bool {
	if !isMatcherNamed(assertion.matcher, "BeEquivalentTo") {
		return false
	}
	if isNumericType(ctx.pass.TypesInfo.TypeOf(assertion.actual)) {
		return true
	}
	return len(assertion.matcher.Args) > 0 && isNumericType(ctx.pass.TypesInfo.TypeOf(assertion.matcher.Args[0]))
}

func assertionUsesTimeEqual(ctx *analysisContext, assertion gomegaAssertion) bool {
	return isMatcherNamed(assertion.matcher, "Equal") &&
		len(assertion.matcher.Args) > 0 &&
		isTimeTimeType(ctx.pass.TypesInfo.TypeOf(assertion.actual)) &&
		isTimeTimeType(ctx.pass.TypesInfo.TypeOf(assertion.matcher.Args[0]))
}

func assertionUsesEqualEmpty(assertion gomegaAssertion) bool {
	if !isMatcherNamed(assertion.matcher, "Equal") || len(assertion.matcher.Args) != 1 {
		return false
	}
	switch arg := unparenExpr(assertion.matcher.Args[0]).(type) {
	case *ast.BasicLit:
		return arg.Kind == token.STRING && arg.Value == `""`
	case *ast.CompositeLit:
		switch arg.Type.(type) {
		case *ast.ArrayType, *ast.MapType:
			return len(arg.Elts) == 0
		default:
			return false
		}
	default:
		return false
	}
}

func assertionUsesEqualZero(assertion gomegaAssertion) bool {
	if !isMatcherNamed(assertion.matcher, "Equal") || len(assertion.matcher.Args) != 1 {
		return false
	}
	lit, ok := unparenExpr(assertion.matcher.Args[0]).(*ast.BasicLit)
	return ok && lit.Kind == token.INT && lit.Value == "0"
}

func assertionUsesRepeatedFieldAssertion(ctx *analysisContext, assertion gomegaAssertion) bool {
	selector, ok := assertedStructField(assertion.actual)
	if !ok {
		return false
	}
	if exprSuggestsStructuredErrorValue(ctx, selector.X) {
		return false
	}
	stmt, ok := enclosingExprStmt(ctx, assertion.call)
	if !ok {
		return false
	}
	block, ok := ctx.parent(stmt).(*ast.BlockStmt)
	if !ok {
		return false
	}
	for _, candidate := range block.List {
		if candidate == stmt {
			continue
		}
		candidateStmt, ok := candidate.(*ast.ExprStmt)
		if !ok {
			continue
		}
		candidateCall, ok := candidateStmt.X.(*ast.CallExpr)
		if !ok {
			continue
		}
		candidateAssertion, ok := gomegaAssertionFromCall(ctx, candidateCall)
		if !ok {
			continue
		}
		candidateSelector, ok := assertedStructField(candidateAssertion.actual)
		if !ok || candidateSelector.Sel.Name == selector.Sel.Name {
			continue
		}
		if sameSimpleExpr(candidateSelector.X, selector.X) {
			return true
		}
	}
	return false
}

func assertedStructField(expr ast.Expr) (*ast.SelectorExpr, bool) {
	selector, ok := unparenExpr(expr).(*ast.SelectorExpr)
	if !ok {
		return nil, false
	}
	if _, ok := unparenExpr(selector.X).(*ast.IndexExpr); ok {
		return nil, false
	}
	return selector, true
}

func assertionUsesCollectionIndexAssertion(ctx *analysisContext, assertion gomegaAssertion) bool {
	return exprUsesCollectionIndex(ctx, assertion.actual)
}

func exprUsesCollectionIndex(ctx *analysisContext, expr ast.Expr) bool {
	switch e := unparenExpr(expr).(type) {
	case *ast.IndexExpr:
		return typeIsSliceOrArray(ctx.pass.TypesInfo.TypeOf(e.X))
	case *ast.SelectorExpr:
		return exprUsesCollectionIndex(ctx, e.X)
	default:
		return false
	}
}

func isNumericType(typ types.Type) bool {
	if typ == nil {
		return false
	}
	basic, ok := types.Unalias(typ).Underlying().(*types.Basic)
	return ok && basic.Info()&types.IsNumeric != 0
}

func isTimeTimeType(typ types.Type) bool {
	named := namedType(typ)
	return named != nil && named.Obj().Name() == "Time" && named.Obj().Pkg() != nil && named.Obj().Pkg().Path() == "time"
}

func isNamedTypeFromPackage(typ types.Type, packagePath string, name string) bool {
	named := namedType(typ)
	return named != nil && named.Obj().Name() == name && named.Obj().Pkg() != nil && named.Obj().Pkg().Path() == packagePath
}

func namedType(typ types.Type) *types.Named {
	if typ == nil {
		return nil
	}
	typ = types.Unalias(typ)
	if ptr, ok := typ.(*types.Pointer); ok {
		typ = ptr.Elem()
	}
	named, _ := typ.(*types.Named)
	return named
}

func eventuallyBareActualAllowed(ctx *analysisContext, actual ast.Expr) bool {
	actual = unparenExpr(actual)
	switch expr := actual.(type) {
	case *ast.FuncLit:
		return true
	case *ast.Ident, *ast.SelectorExpr:
		return eventuallyBareTypeAllowed(ctx.pass.TypesInfo.TypeOf(expr))
	default:
		return eventuallyBareTypeAllowed(ctx.pass.TypesInfo.TypeOf(actual))
	}
}

func eventuallyBareTypeAllowed(typ types.Type) bool {
	if typ == nil {
		return false
	}
	typ = types.Unalias(typ)
	if _, ok := typ.Underlying().(*types.Chan); ok {
		return true
	}
	if _, ok := typ.Underlying().(*types.Signature); ok {
		return true
	}
	if isNamedTypeFromPackage(typ, "github.com/onsi/gomega/gbytes", "Buffer") {
		return true
	}
	if isNamedTypeFromPackage(typ, "github.com/onsi/gomega/gexec", "Session") {
		return true
	}
	return false
}

func asyncAssertionRequiresSpecContext(ctx *analysisContext, assertion gomegaAsyncAssertion) bool {
	contextNames := enclosingSpecContextNames(ctx, assertion.call)
	if len(contextNames) == 0 {
		return false
	}
	return !asyncAssertionUsesContext(ctx, assertion, contextNames)
}

func enclosingSpecContextNames(ctx *analysisContext, node ast.Node) map[string]bool {
	for current := node; current != nil; current = ctx.parent(current) {
		fn, ok := current.(*ast.FuncLit)
		if !ok {
			if _, ok := current.(*ast.FuncDecl); ok {
				return nil
			}
			continue
		}
		call, ok := ctx.parent(fn).(*ast.CallExpr)
		if !ok || !isGinkgoSpecNodeName(callName(call)) {
			continue
		}
		return funcLitContextParamNames(ctx, fn)
	}
	return nil
}

func funcLitContextParamNames(ctx *analysisContext, fn *ast.FuncLit) map[string]bool {
	names := map[string]bool{}
	if fn.Type.Params == nil {
		return names
	}
	for _, field := range fn.Type.Params.List {
		if !isContextParamType(ctx, field.Type) {
			continue
		}
		for _, name := range field.Names {
			if name != nil {
				names[name.Name] = true
			}
		}
	}
	return names
}

func isContextParamType(ctx *analysisContext, expr ast.Expr) bool {
	if isContextType(ctx.pass.TypesInfo.TypeOf(expr)) {
		return true
	}
	switch exprName(expr) {
	case "Context", "SpecContext":
		return true
	default:
		return false
	}
}

func asyncAssertionUsesContext(ctx *analysisContext, assertion gomegaAsyncAssertion, contextNames map[string]bool) bool {
	if asyncCallChainUsesWithContext(assertion.call) {
		return true
	}
	for _, arg := range assertion.source.Args {
		if ident, ok := unparenExpr(arg).(*ast.Ident); ok && contextNames[ident.Name] {
			return true
		}
		if isContextType(ctx.pass.TypesInfo.TypeOf(arg)) {
			return true
		}
	}
	return false
}

func asyncCallChainUsesWithContext(call *ast.CallExpr) bool {
	found := false
	ast.Inspect(call, func(node ast.Node) bool {
		if found || node == nil {
			return false
		}
		if _, ok := node.(*ast.FuncLit); ok {
			return false
		}
		candidate, ok := node.(*ast.CallExpr)
		if ok && callName(candidate) == "WithContext" {
			found = true
			return false
		}
		return true
	})
	return found
}

func assertionUsesAsyncBooleanMatcher(ctx *analysisContext, assertion gomegaAsyncAssertion) bool {
	return isBooleanMatcher(assertion.matcher) && asyncActualProducesBool(ctx, assertion.actual)
}

func asyncActualProducesBool(ctx *analysisContext, actual ast.Expr) bool {
	actual = unparenExpr(actual)
	if fn, ok := actual.(*ast.FuncLit); ok {
		return funcLitReturnsBool(ctx, fn)
	}
	return isBoolType(ctx.pass.TypesInfo.TypeOf(actual))
}

func funcLitReturnsBool(ctx *analysisContext, fn *ast.FuncLit) bool {
	if fn.Type.Results == nil || len(fn.Type.Results.List) != 1 {
		return false
	}
	return isBoolType(ctx.pass.TypesInfo.TypeOf(fn.Type.Results.List[0].Type))
}

func isBoolType(typ types.Type) bool {
	if typ == nil {
		return false
	}
	basic, ok := types.Unalias(typ).Underlying().(*types.Basic)
	return ok && (basic.Kind() == types.Bool || basic.Kind() == types.UntypedBool)
}

func isContextType(typ types.Type) bool {
	named := namedType(typ)
	if named == nil {
		return false
	}
	if named.Obj().Name() == "SpecContext" {
		return true
	}
	return named.Obj().Name() == "Context" &&
		named.Obj().Pkg() != nil &&
		named.Obj().Pkg().Path() == "context"
}

func unparenExpr(expr ast.Expr) ast.Expr {
	for {
		paren, ok := expr.(*ast.ParenExpr)
		if !ok {
			return expr
		}
		expr = paren.X
	}
}

func isBooleanMatcher(matcher *ast.CallExpr) bool {
	return isMatcherNamed(matcher, "BeTrue", "BeFalse", "BeTrueBecause", "BeFalseBecause")
}

func assertionUsesRawStringMatchError(ctx *analysisContext, assertion gomegaAssertion) bool {
	return matcherTreeContainsRawStringMatchError(ctx, assertion.matcher)
}

func matcherTreeContainsRawStringMatchError(ctx *analysisContext, expr ast.Expr) bool {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return false
	}
	if isMatcherNamed(call, "MatchError") && len(call.Args) > 0 {
		if matchErrorArgumentUsesRawString(ctx, call.Args[0]) {
			return true
		}
	}
	for _, arg := range call.Args {
		if matcherTreeContainsRawStringMatchError(ctx, arg) {
			return true
		}
	}
	return false
}

func matchErrorArgumentUsesRawString(ctx *analysisContext, expr ast.Expr) bool {
	expr = unparenExpr(expr)
	lit, ok := expr.(*ast.BasicLit)
	if ok && lit.Kind == token.STRING {
		return true
	}
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return false
	}
	if isMatcherNamed(call, "Equal", "ContainSubstring", "HavePrefix", "HaveSuffix", "MatchRegexp") {
		return callHasStringArg(ctx, call)
	}
	if isMatcherNamed(call, "And", "Or", "SatisfyAll", "SatisfyAny") {
		for _, arg := range call.Args {
			if matchErrorArgumentUsesRawString(ctx, arg) {
				return true
			}
		}
	}
	return false
}

func callHasStringArg(ctx *analysisContext, call *ast.CallExpr) bool {
	for _, arg := range call.Args {
		lit, ok := unparenExpr(arg).(*ast.BasicLit)
		if ok && lit.Kind == token.STRING {
			return true
		}
		if isStringType(ctx.pass, arg) {
			return true
		}
	}
	return false
}

func isMatcherNamed(matcher *ast.CallExpr, names ...string) bool {
	name := callName(matcher)
	for _, candidate := range names {
		if name == candidate {
			return true
		}
	}
	return false
}

func sameSimpleExpr(left ast.Expr, right ast.Expr) bool {
	left = unparenExpr(left)
	right = unparenExpr(right)
	switch l := left.(type) {
	case *ast.Ident:
		r, ok := right.(*ast.Ident)
		return ok && l.Name == r.Name
	case *ast.SelectorExpr:
		r, ok := right.(*ast.SelectorExpr)
		return ok && l.Sel.Name == r.Sel.Name && sameSimpleExpr(l.X, r.X)
	default:
		return false
	}
}

func assertionMatchesStructuredErrorField(ctx *analysisContext, assertion gomegaAssertion) (string, bool) {
	if !isMatcherNamed(assertion.matcher, "Equal", "BeEquivalentTo", "ContainSubstring") {
		return "", false
	}
	selector, ok := assertion.actual.(*ast.SelectorExpr)
	if !ok || !structuredErrorFieldName(selector.Sel.Name) {
		return "", false
	}
	if !exprSuggestsStructuredErrorValue(ctx, selector.X) {
		return "", false
	}
	return selector.Sel.Name, true
}

func assertionUsesStructuredFieldMatcher(ctx *analysisContext, assertion gomegaAssertion) (string, bool) {
	if !exprSuggestsStructuredErrorValue(ctx, assertion.actual) {
		return "", false
	}
	return matcherTreeContainsStructuredFieldMatcher(assertion.matcher)
}

func matcherTreeContainsStructuredFieldMatcher(expr ast.Expr) (string, bool) {
	call, ok := unparenExpr(expr).(*ast.CallExpr)
	if !ok {
		return "", false
	}
	if isMatcherNamed(call, "HaveField") && len(call.Args) > 0 {
		if fieldName, ok := stringLiteralValue(call.Args[0]); ok && structuredErrorFieldName(fieldName) {
			return fieldName, true
		}
	}
	for _, arg := range call.Args {
		if fieldName, ok := matcherTreeContainsStructuredFieldMatcher(arg); ok {
			return fieldName, true
		}
	}
	return "", false
}

func assertionUsesStructuredProtocolKeyMatcher(ctx *analysisContext, assertion gomegaAssertion) (string, bool) {
	if !exprSuggestsStructuredProtocolValue(ctx, assertion.actual) {
		return "", false
	}
	return matcherTreeContainsStructuredProtocolKeyMatcher(assertion.matcher)
}

func matcherTreeContainsStructuredProtocolKeyMatcher(expr ast.Expr) (string, bool) {
	call, ok := unparenExpr(expr).(*ast.CallExpr)
	if !ok {
		return "", false
	}
	if isMatcherNamed(call, "HaveKey", "HaveKeyWithValue") && len(call.Args) > 0 {
		if keyName, ok := stringLiteralValue(call.Args[0]); ok && structuredProtocolKeyName(keyName) {
			return keyName, true
		}
	}
	for _, arg := range call.Args {
		if keyName, ok := matcherTreeContainsStructuredProtocolKeyMatcher(arg); ok {
			return keyName, true
		}
	}
	return "", false
}

func assertionUsesStructuredProtocolPayloadMatcher(ctx *analysisContext, assertion gomegaAssertion) (string, bool) {
	if !exprSuggestsStructuredProtocolValue(ctx, assertion.actual) {
		return "", false
	}
	return matcherTreeContainsStructuredProtocolPayloadMatcher(assertion.matcher)
}

func matcherTreeContainsStructuredProtocolPayloadMatcher(expr ast.Expr) (string, bool) {
	call, ok := unparenExpr(expr).(*ast.CallExpr)
	if !ok {
		return "", false
	}
	if isMatcherNamed(call, "MatchJSON", "MatchYAML", "MatchXML") && len(call.Args) > 0 {
		if literalValueSuggestsStructuredProtocol(call.Args[0]) {
			return callName(call), true
		}
	}
	for _, arg := range call.Args {
		if matcherName, ok := matcherTreeContainsStructuredProtocolPayloadMatcher(arg); ok {
			return matcherName, true
		}
	}
	return "", false
}

func structuredErrorFieldName(name string) bool {
	switch canonicalName(name) {
	case "code", "errorcode", "field", "fieldcode", "issuecode", "message", "messageid":
		return true
	default:
		return false
	}
}

func exprSuggestsStructuredErrorValue(ctx *analysisContext, expr ast.Expr) bool {
	typ := ctx.pass.TypesInfo.TypeOf(expr)
	if ptr, ok := typ.(*types.Pointer); ok {
		typ = ptr.Elem()
	}
	if named, ok := typ.(*types.Named); ok {
		if _, ok := named.Underlying().(*types.Struct); ok && isMessageBearingStructName(named.Obj().Name()) {
			return true
		}
	}
	canonical := canonicalName(exprName(expr))
	return strings.Contains(canonical, "error") ||
		strings.Contains(canonical, "validation") ||
		strings.Contains(canonical, "issue")
}

func exprSuggestsStructuredProtocolValue(ctx *analysisContext, expr ast.Expr) bool {
	canonical := canonicalName(exprName(expr))
	if strings.Contains(canonical, "payload") ||
		strings.Contains(canonical, "response") ||
		strings.Contains(canonical, "result") ||
		strings.Contains(canonical, "error") ||
		strings.Contains(canonical, "validation") ||
		strings.Contains(canonical, "issue") ||
		strings.Contains(canonical, "tool") {
		return true
	}
	typ := ctx.pass.TypesInfo.TypeOf(expr)
	if ptr, ok := typ.(*types.Pointer); ok {
		typ = ptr.Elem()
	}
	named, ok := typ.(*types.Named)
	if !ok {
		return false
	}
	typeName := canonicalName(named.Obj().Name())
	return strings.Contains(typeName, "payload") ||
		strings.Contains(typeName, "response") ||
		strings.Contains(typeName, "result") ||
		strings.Contains(typeName, "error") ||
		strings.Contains(typeName, "validation") ||
		strings.Contains(typeName, "issue") ||
		strings.Contains(typeName, "tool")
}

func structuredProtocolKeyName(name string) bool {
	switch canonicalName(name) {
	case "code", "error", "errorcode", "field", "fieldcode", "issuecode", "message", "messageid", "toolmessageid":
		return true
	default:
		return false
	}
}

func literalValueSuggestsStructuredProtocol(expr ast.Expr) bool {
	value, ok := stringLiteralValue(expr)
	if !ok {
		return false
	}
	canonical := canonicalName(value)
	return strings.Contains(canonical, "messageid") ||
		strings.Contains(canonical, "errorcode") ||
		strings.Contains(canonical, "fieldcode") ||
		strings.Contains(canonical, "issuecode")
}

func stringLiteralValue(expr ast.Expr) (string, bool) {
	lit, ok := unparenExpr(expr).(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	value, err := strconv.Unquote(lit.Value)
	if err != nil {
		return "", false
	}
	return value, true
}

func funcLitHasGomegaParam(fn *ast.FuncLit) bool {
	if fn.Type.Params == nil {
		return false
	}
	for _, param := range fn.Type.Params.List {
		if exprName(param.Type) == "Gomega" {
			return true
		}
		for _, name := range param.Names {
			if name != nil && canonicalName(name.Name) == "g" && exprName(param.Type) == "Gomega" {
				return true
			}
		}
	}
	return false
}

func reportGlobalExpectCallsInAsyncCallback(ctx *analysisContext, fn *ast.FuncLit) {
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || !isGlobalGomegaExpectCall(call) {
			return true
		}
		ctx.pass.Reportf(call.Pos(), "%s", gomegaAsyncCallbackExpectDiagnostic())
		return true
	})
}

func isGlobalGomegaExpectCall(call *ast.CallExpr) bool {
	if _, ok := call.Fun.(*ast.Ident); !ok {
		return false
	}
	return isGomegaExpectationSource(callName(call))
}

func isTestAssertionHelperName(name string) bool {
	return strings.HasPrefix(name, "assert") || strings.HasPrefix(name, "expect")
}

func gomegaMatcherFactoryName(name string) bool {
	return strings.HasPrefix(name, "Have") ||
		strings.HasPrefix(name, "Contain") ||
		strings.HasPrefix(name, "Match") ||
		strings.HasPrefix(name, "Be")
}

func funcReturnsGomegaMatcher(fn *ast.FuncDecl) bool {
	if fn.Type.Results == nil {
		return false
	}
	for _, field := range fn.Type.Results.List {
		if exprName(field.Type) == "GomegaMatcher" {
			return true
		}
		if selector, ok := field.Type.(*ast.SelectorExpr); ok && selector.Sel.Name == "GomegaMatcher" {
			return true
		}
	}
	return false
}

func funcContainsGomegaAssertion(ctx *analysisContext, body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(node ast.Node) bool {
		if found || node == nil {
			return false
		}
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		_, found = gomegaAssertionFromCall(ctx, call)
		return !found
	})
	return found
}

func funcHasGomegaHelperReporting(ctx *analysisContext, fn *ast.FuncDecl) bool {
	if funcUsesGomegaParam(ctx, fn) {
		return true
	}
	found := false
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		if found || node == nil {
			return false
		}
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		if isGomegaHelperReportingCall(callName(call)) {
			found = true
			return false
		}
		return true
	})
	return found
}

func isGomegaHelperReportingCall(name string) bool {
	switch name {
	case "GinkgoHelper", "ExpectWithOffset", "EventuallyWithOffset", "ConsistentlyWithOffset", "WithOffset":
		return true
	default:
		return false
	}
}

func funcUsesGomegaParam(ctx *analysisContext, fn *ast.FuncDecl) bool {
	names := gomegaParamNames(fn)
	if len(names) == 0 {
		return false
	}
	used := false
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		if used || node == nil {
			return false
		}
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != "Expect" {
			return true
		}
		if names[exprName(selector.X)] {
			used = true
			return false
		}
		return true
	})
	return used
}

func gomegaParamNames(fn *ast.FuncDecl) map[string]bool {
	names := map[string]bool{}
	if fn.Type.Params == nil {
		return names
	}
	for _, param := range fn.Type.Params.List {
		if exprName(param.Type) != "Gomega" {
			continue
		}
		for _, name := range param.Names {
			if name != nil {
				names[name.Name] = true
			}
		}
	}
	return names
}
