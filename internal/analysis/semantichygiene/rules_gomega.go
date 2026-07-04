package semantichygiene

import (
	"go/ast"
	"go/token"
	"go/types"
	"strconv"
	"strings"
	"unicode"
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
	if callLaundersBooleanToStringState(ctx, call) {
		ctx.report(ruleGomegaProxyBoolean, call, gomegaBooleanStateStringDiagnostic())
	}
	if matcherCallUsesMatcherValueAsExpected(ctx, call) {
		ctx.report(ruleGomegaMatcherAsValue, call, gomegaMatcherAsValueDiagnostic(callName(call)))
	}
	if rawStringMatchErrorMatcherCall(ctx, call) && !callIsInsideGomegaAssertion(ctx, call) {
		ctx.report(ruleGomegaRawStringMatchError, call, gomegaRawStringMatchErrorDiagnostic())
	}
	if matcherUsesPositionalTransform(ctx, call) {
		ctx.report(ruleGomegaPositionalTransform, call, gomegaPositionalTransformDiagnostic())
	}
	if matcherUsesOSIsNotExistTransform(ctx, call) {
		ctx.report(ruleGomegaOSIsNotExistMatcher, call, gomegaOSIsNotExistMatcherDiagnostic())
	}
	if isEqualZeroMatcherCall(call) {
		ctx.report(ruleGomegaEqualZero, call, gomegaEqualZeroDiagnostic())
	}
	if isEqualBooleanLiteralMatcherCall(call) {
		ctx.report(ruleGomegaBooleanLiteral, call, gomegaBooleanLiteralEqualDiagnostic())
	}
	if matcherCallIsNestedBooleanMatcherValue(ctx, call) {
		ctx.report(ruleGomegaProxyBoolean, call, gomegaProxyBooleanDiagnostic())
	}
	assertion, ok := gomegaAssertionFromCall(ctx, call)
	if !ok {
		return
	}
	if assertionUsesErrError(ctx, assertion) && isMatcherNamed(assertion.matcher, "Equal", "ContainSubstring") {
		ctx.report(ruleGomegaErrorString, assertion.actual, gomegaErrorStringMatcherDiagnostic())
	}
	if matcherTreeUsesErrError(ctx, assertion.matcher) {
		ctx.report(ruleGomegaErrorString, assertion.matcher, gomegaErrorStringMatcherDiagnostic())
	}
	if assertionUsesRawStringMatchError(ctx, assertion) {
		ctx.report(ruleGomegaRawStringMatchError, assertion.matcher, gomegaRawStringMatchErrorDiagnostic())
	}
	if assertionUsesErrorNilMatcher(ctx, assertion) {
		ctx.report(ruleGomegaErrorNilMatcher, assertion.actual, gomegaErrorNilMatcherDiagnostic())
	}
	if assertionUsesGenericHaveOccurred(ctx, assertion) {
		ctx.report(ruleGomegaGenericHaveOccurred, assertion.matcher, gomegaGenericHaveOccurredDiagnostic())
	}
	if assertionMatcherTreeUsesGenericHaveOccurred(assertion) {
		ctx.report(ruleGomegaGenericHaveOccurred, assertion.matcher, gomegaGenericHaveOccurredDiagnostic())
	}
	if assertionMatcherTreeUsesGenericErrorPresence(assertion) {
		ctx.report(ruleGomegaGenericHaveOccurred, assertion.matcher, gomegaGenericHaveOccurredDiagnostic())
	}
	if assertionUsesInlineErrorReturnHaveOccurred(ctx, assertion) {
		ctx.report(ruleGomegaInlineErrorSucceed, assertion.actual, gomegaInlineErrorSucceedDiagnostic())
	}
	if assertionUsesMultiReturnErrorMatcher(ctx, assertion) {
		ctx.report(ruleGomegaMultiReturnErrorMatcher, assertion.actual, gomegaMultiReturnErrorMatcherDiagnostic())
	}
	if assertionUsesStringsContains(ctx, assertion) && isBooleanMatcher(assertion.matcher) {
		ctx.report(ruleGomegaStringsContains, assertion.actual, gomegaStringsContainsMatcherDiagnostic())
	}
	if assertionUsesRenderedMessageStringMatcher(ctx, assertion) {
		ctx.report(ruleGomegaStringsContains, assertion.actual, gomegaRenderedMessageStringsContainsDiagnostic())
	}
	if assertionUsesLastErrorNotEmpty(assertion) {
		ctx.report(ruleGomegaLastErrorNotEmpty, assertion.actual, gomegaLastErrorNotEmptyDiagnostic())
	}
	if assertionUsesStringsPredicate(ctx, assertion, "HasPrefix") && isBooleanMatcher(assertion.matcher) {
		ctx.report(ruleGomegaStringPredicate, assertion.actual, gomegaStringPredicateMatcherDiagnostic("strings.HasPrefix", "HavePrefix"))
	}
	if assertionUsesStringsPredicate(ctx, assertion, "HasSuffix") && isBooleanMatcher(assertion.matcher) {
		ctx.report(ruleGomegaStringPredicate, assertion.actual, gomegaStringPredicateMatcherDiagnostic("strings.HasSuffix", "HaveSuffix"))
	}
	if assertionUsesRegexpMatchString(ctx, assertion) && isBooleanMatcher(assertion.matcher) {
		ctx.report(ruleGomegaRegexpMatchString, assertion.actual, gomegaRegexpMatchStringDiagnostic())
	}
	if assertionUsesErrorsIs(ctx, assertion) && isBooleanMatcher(assertion.matcher) {
		ctx.report(ruleGomegaErrorsIsMatcher, assertion.actual, gomegaErrorsIsMatcherDiagnostic())
	}
	if assertionUsesErrorsAs(ctx, assertion) && isBooleanMatcher(assertion.matcher) {
		ctx.report(ruleGomegaErrorsAsMatcher, assertion.actual, gomegaErrorsAsMatcherDiagnostic())
	}
	if assertionUsesControlStatus(assertion) && isBooleanMatcher(assertion.matcher) {
		ctx.report(ruleGomegaControlStatusMatcher, assertion.actual, gomegaControlStatusMatcherDiagnostic())
	}
	if assertionUsesOSIsNotExist(ctx, assertion) && isBooleanMatcher(assertion.matcher) {
		ctx.report(ruleGomegaOSIsNotExistMatcher, assertion.actual, gomegaOSIsNotExistMatcherDiagnostic())
	}
	if assertionUsesLenEqual(assertion) {
		ctx.report(ruleGomegaLenEqual, assertion.actual, gomegaLenMatcherDiagnostic())
	}
	if assertionUsesBinaryBoolean(assertion) {
		ctx.report(ruleGomegaBinaryBoolean, assertion.actual, gomegaBinaryBooleanMatcherDiagnostic())
	}
	if assertionUsesBooleanLiteral(assertion) {
		ctx.report(ruleGomegaBooleanLiteral, assertion.actual, gomegaBooleanLiteralMatcherDiagnostic())
	}
	if assertionUsesCommaOKBoolean(ctx, assertion) {
		ctx.report(ruleGomegaCommaOKAssertion, assertion.actual, gomegaCommaOKAssertionDiagnostic())
	}
	if assertionUsesProxyBoolean(ctx, assertion) {
		ctx.report(ruleGomegaProxyBoolean, assertion.actual, gomegaProxyBooleanDiagnostic())
	}
	if assertionUsesMapIndexEqual(ctx, assertion) {
		ctx.report(ruleGomegaMapIndex, assertion.actual, gomegaMapIndexMatcherDiagnostic())
	}
	if assertionUsesHTTPStatusEqual(ctx, assertion) {
		ctx.report(ruleGomegaHTTPStatus, assertion.actual, gomegaHTTPStatusMatcherDiagnostic())
	}
	if assertionUsesHTTPBodyString(ctx, assertion) {
		ctx.report(ruleGomegaHTTPBody, assertion.actual, gomegaHTTPBodyMatcherDiagnostic())
	}
	if assertionUsesRepeatedHTTPBodyMatcher(ctx, assertion) {
		ctx.report(ruleGomegaRepeatedHTTPBody, assertion.matcher, gomegaRepeatedHTTPBodyMatcherDiagnostic())
	}
	if assertionUsesHTTPHeaderGet(ctx, assertion) {
		ctx.report(ruleGomegaHTTPHeader, assertion.actual, gomegaHTTPHeaderMatcherDiagnostic())
	}
	if assertionUsesResponseHeaderMatcherOnRequest(ctx, assertion) {
		ctx.report(ruleGomegaHTTPHeader, assertion.matcher, gomegaHTTPHeaderResponseMatcherOnRequestDiagnostic())
	}
	if assertionUsesNumericBeEquivalentTo(ctx, assertion) {
		ctx.report(ruleGomegaNumericEquivalent, assertion.matcher, gomegaNumericEquivalentDiagnostic())
	}
	if assertionUsesTimeEqual(ctx, assertion) {
		ctx.report(ruleGomegaTimeEqual, assertion.actual, gomegaTimeEqualDiagnostic())
	}
	if assertionUsesEqualEmpty(assertion) {
		ctx.report(ruleGomegaEqualEmpty, assertion.matcher, gomegaEqualEmptyDiagnostic())
	}
	if assertionUsesNonEmptyCollection(ctx, assertion) {
		ctx.report(ruleGomegaNonEmptyCollection, assertion.matcher, gomegaNonEmptyCollectionDiagnostic())
	}
	if assertionUsesSemanticScalarNotEmpty(ctx, assertion) {
		ctx.report(ruleGomegaSemanticScalarNotEmpty, assertion.matcher, gomegaSemanticScalarNotEmptyDiagnostic())
	}
	if assertionUsesRepeatedFieldAssertion(ctx, assertion) {
		ctx.report(ruleGomegaRepeatedFieldAssertions, assertion.actual, gomegaRepeatedFieldAssertionDiagnostic())
	}
	if assertionUsesPositionalCompositeAssertion(assertion) {
		ctx.report(ruleGomegaPositionalCompositeAssertion, assertion.actual, gomegaPositionalCompositeAssertionDiagnostic())
	}
	if assertionUsesCollectionIndexAssertion(ctx, assertion) {
		ctx.report(ruleGomegaCollectionIndexAssertion, assertion.actual, gomegaCollectionIndexAssertionDiagnostic())
	}
	if fieldName, ok := assertionMatchesStructuredErrorField(ctx, assertion); ok {
		ctx.report(ruleGomegaStructuredErrorMatcher, assertion.actual, gomegaStructuredErrorMatcherDiagnostic(fieldName))
	}
	if fieldName, ok := assertionUsesStructuredFieldMatcher(ctx, assertion); ok {
		ctx.report(ruleGomegaStructuredErrorMatcher, assertion.matcher, gomegaStructuredErrorMatcherDiagnostic(fieldName))
	}
	if keyName, ok := assertionUsesStructuredProtocolKeyMatcher(ctx, assertion); ok {
		ctx.report(ruleGomegaStructuredProtocolKey, assertion.matcher, gomegaStructuredProtocolKeyMatcherDiagnostic(keyName))
	}
	if matcherName, ok := assertionUsesStructuredProtocolPayloadMatcher(ctx, assertion); ok {
		ctx.report(ruleGomegaStructuredProtocolPayload, assertion.matcher, gomegaStructuredProtocolPayloadMatcherDiagnostic(matcherName))
	}
	if assertionUsesStructuredProtocolStatusMatcher(ctx, assertion) {
		ctx.report(ruleGomegaStructuredProtocolStatus, assertion.actual, gomegaStructuredProtocolStatusMatcherDiagnostic())
	}
}

func checkGomegaIgnoredSemanticBoolean(ctx *analysisContext, assign *ast.AssignStmt) {
	if !isTestFile(ctx.filename(assign.Pos())) || len(assign.Lhs) < 2 || len(assign.Rhs) != 1 {
		return
	}
	call, ok := unparenExpr(assign.Rhs[0]).(*ast.CallExpr)
	if !ok {
		return
	}
	results, ok := ctx.pass.TypesInfo.TypeOf(call).(*types.Tuple)
	if !ok {
		return
	}
	for i, lhs := range assign.Lhs {
		if i >= results.Len() || !isBoolType(results.At(i).Type()) {
			continue
		}
		ident, ok := unparenExpr(lhs).(*ast.Ident)
		if !ok || ident.Name != "_" {
			continue
		}
		if !callReturnsSemanticBoolean(ctx, call, results, i) {
			continue
		}
		ctx.report(ruleGomegaIgnoredSemanticBoolean, ident, gomegaIgnoredSemanticBooleanDiagnostic())
	}
}

func callReturnsSemanticBoolean(ctx *analysisContext, call *ast.CallExpr, results *types.Tuple, index int) bool {
	if callTargetsLeafWikiProduction(ctx, call) {
		return true
	}
	return callTargetsTestLocalSemanticLookup(ctx, call, results, index)
}

func callTargetsLeafWikiProduction(ctx *analysisContext, call *ast.CallExpr) bool {
	fn := calledFunctionObject(ctx, call)
	if fn == nil || fn.Pkg() == nil {
		return false
	}
	if fn.Pos().IsValid() {
		filename := ctx.filename(fn.Pos())
		if isTestFile(filename) || isTestSupportFile(filename) {
			return false
		}
	}
	path := fn.Pkg().Path()
	return path == ctx.pass.Pkg.Path() || strings.HasPrefix(path, "github.com/perber/wiki/")
}

func callTargetsTestLocalSemanticLookup(ctx *analysisContext, call *ast.CallExpr, results *types.Tuple, ignoredBoolIndex int) bool {
	fn := calledFunctionObject(ctx, call)
	if fn == nil || !fn.Pos().IsValid() {
		return false
	}
	filename := ctx.filename(fn.Pos())
	if !isTestFile(filename) && !isTestSupportFile(filename) {
		return false
	}
	if !semanticLookupFunctionName(fn.Name()) {
		return false
	}
	return tupleHasLeafWikiDomainResult(ctx, results, ignoredBoolIndex)
}

func semanticLookupFunctionName(name string) bool {
	canonical := canonicalName(name)
	return strings.HasPrefix(canonical, "find") ||
		strings.HasPrefix(canonical, "lookup") ||
		strings.HasPrefix(canonical, "resolve") ||
		strings.Contains(canonical, "registered") ||
		strings.Contains(canonical, "provider") ||
		strings.Contains(canonical, "grant") ||
		strings.Contains(canonical, "remoteuser")
}

func tupleHasLeafWikiDomainResult(ctx *analysisContext, results *types.Tuple, ignoredBoolIndex int) bool {
	for i := 0; i < results.Len(); i++ {
		if i == ignoredBoolIndex {
			continue
		}
		typ := results.At(i).Type()
		if isBoolType(typ) || typeImplementsError(typ) {
			continue
		}
		if typeContainsLeafWikiDomainType(ctx, typ) {
			return true
		}
	}
	return false
}

func typeContainsLeafWikiDomainType(ctx *analysisContext, typ types.Type) bool {
	if typ == nil {
		return false
	}
	typ = types.Unalias(typ)
	switch t := typ.(type) {
	case *types.Pointer:
		return typeContainsLeafWikiDomainType(ctx, t.Elem())
	case *types.Named:
		return namedTypeIsLeafWikiDomain(ctx, t)
	case *types.Slice:
		return typeContainsLeafWikiDomainType(ctx, t.Elem())
	case *types.Array:
		return typeContainsLeafWikiDomainType(ctx, t.Elem())
	case *types.Map:
		return typeContainsLeafWikiDomainType(ctx, t.Key()) ||
			typeContainsLeafWikiDomainType(ctx, t.Elem())
	default:
		return false
	}
}

func namedTypeIsLeafWikiDomain(ctx *analysisContext, named *types.Named) bool {
	if named == nil || named.Obj() == nil || named.Obj().Pkg() == nil {
		return false
	}
	path := named.Obj().Pkg().Path()
	return path == ctx.pass.Pkg.Path() || strings.HasPrefix(path, "github.com/perber/wiki/")
}

func calledFunctionObject(ctx *analysisContext, call *ast.CallExpr) *types.Func {
	switch fun := unparenExpr(call.Fun).(type) {
	case *ast.Ident:
		fn, _ := ctx.pass.TypesInfo.ObjectOf(fun).(*types.Func)
		return fn
	case *ast.SelectorExpr:
		if selection := ctx.pass.TypesInfo.Selections[fun]; selection != nil {
			fn, _ := selection.Obj().(*types.Func)
			return fn
		}
		fn, _ := ctx.pass.TypesInfo.ObjectOf(fun.Sel).(*types.Func)
		return fn
	default:
		return nil
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
		ctx.report(ruleGomegaAsyncNegativeReceive, assertion.call, gomegaAsyncNegativeReceiveDiagnostic())
	}
	if matcherTreeContainsRawStringMatchError(ctx, assertion.matcher) {
		ctx.report(ruleGomegaRawStringMatchError, assertion.matcher, gomegaRawStringMatchErrorDiagnostic())
	}
	if assertion.sourceName == "Eventually" && !eventuallyBareActualAllowed(ctx, assertion.actual) {
		ctx.report(ruleGomegaAsyncBareValue, assertion.actual, gomegaAsyncBareValueDiagnostic())
	}
	if asyncAssertionRequiresSpecContext(ctx, assertion) {
		ctx.report(ruleGomegaAsyncContext, assertion.source, ginkgoAsyncContextDiagnostic())
	}
	if assertionUsesAsyncBooleanMatcher(ctx, assertion) {
		ctx.report(ruleGomegaAsyncBoolean, assertion.actual, gomegaAsyncBooleanMatcherDiagnostic())
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
	ctx.report(ruleGomegaHelperOffset, fn.Name, gomegaHelperOffsetDiagnostic(fn.Name.Name))
}

func checkGomegaMatcherFactorySignature(ctx *analysisContext, fn *ast.FuncDecl) {
	if fn.Body == nil ||
		!funcReturnsGomegaMatcher(fn) {
		return
	}
	filename := ctx.filename(fn.Pos())
	if !isTestFile(filename) && !isTestSupportFile(filename) {
		return
	}
	checkGomegaMatcherFactoryGenericHaveOccurred(ctx, fn)
	checkGomegaMatcherFactoryGenericToolErrorArgs(ctx, fn)
	checkGomegaMatcherFactoryBooleanErrorGate(ctx, fn)
	checkGomegaMatcherFactoryTransformedBooleanContract(ctx, fn)
	checkGomegaMatcherFactoryProxyBooleanPredicate(ctx, fn)
	checkGomegaMatcherFactoryLastErrorRenderedText(ctx, fn)
	checkGomegaMatcherFactoryStructuredProtocolStatus(ctx, fn)
	checkGomegaMatcherFactoryPredicateOnlyBoolean(ctx, fn)
	isMatcherFactory := gomegaMatcherFactoryName(fn.Name.Name)
	isRenderedOutputMatcherFactory := gomegaRenderedOutputMatcherFactoryName(fn.Name.Name)
	if fn.Type.Params == nil || !isMatcherFactory && !isRenderedOutputMatcherFactory {
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
				if isMatcherFactory {
					ctx.report(ruleSemanticRawSignature, name, customMatcherSemanticParameterDiagnostic(fn.Name.Name, name.Name, semanticType))
				}
				continue
			}
			if testHelperMessageParamName(name.Name, fn.Name.Name) {
				ctx.report(ruleI18nMessageParameter, name, customMatcherMessageParameterDiagnostic(fn.Name.Name, name.Name))
				continue
			}
			if testHelperFieldParamName(name.Name, fn.Name.Name) {
				if isMatcherFactory {
					ctx.report(ruleSemanticRawField, name, customMatcherFieldParameterDiagnostic(fn.Name.Name, name.Name))
				}
			}
		}
	}
}

func checkGomegaMatcherFactoryBooleanErrorGate(ctx *analysisContext, fn *ast.FuncDecl) {
	boolParams := matcherFactoryBoolParams(ctx, fn)
	proxyBoolParams := matcherFactoryProxyBoolParams(ctx, fn)
	if len(boolParams) == 0 ||
		!matcherFactoryCombinesBoolParamWithErrorPredicate(ctx, fn.Body, boolParams) &&
			!matcherFactoryUsesProxyBoolParam(ctx, fn.Body, proxyBoolParams) {
		return
	}
	ctx.report(ruleGomegaProxyBoolean, fn.Name, gomegaMatcherFactoryBooleanErrorGateDiagnostic())
}

func checkGomegaMatcherFactoryTransformedBooleanContract(ctx *analysisContext, fn *ast.FuncDecl) {
	boolParams := matcherFactoryBoolParams(ctx, fn)
	if len(boolParams) == 0 ||
		!matcherFactoryContainsWithTransform(fn.Body) ||
		!matcherFactoryUsesBoolParam(ctx, fn.Body, boolParams) {
		return
	}
	ctx.report(ruleGomegaProxyBoolean, fn.Name, gomegaMatcherFactoryTransformedBooleanContractDiagnostic())
}

func checkGomegaMatcherFactoryProxyBooleanPredicate(ctx *analysisContext, fn *ast.FuncDecl) {
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		switch current := node.(type) {
		case nil:
			return false
		case *ast.FuncLit:
			return false
		case *ast.CallExpr:
			if predicate := gomegaProxyBooleanPredicateMatcher(ctx, current); predicate != nil {
				ctx.report(ruleGomegaProxyBoolean, predicate, gomegaMatcherFactoryProxyBooleanPredicateDiagnostic())
				return false
			}
		}
		return true
	})
}

func checkGomegaMatcherFactoryLastErrorRenderedText(ctx *analysisContext, fn *ast.FuncDecl) {
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		switch current := node.(type) {
		case nil:
			return false
		case *ast.FuncLit:
			return false
		case *ast.CallExpr:
			if predicate := gomegaLastErrorRenderedPredicateMatcher(ctx, current); predicate != nil {
				ctx.report(ruleGomegaLastErrorNotEmpty, predicate, gomegaLastErrorNotEmptyDiagnostic())
				return false
			}
		}
		return true
	})
}

func checkGomegaMatcherFactoryStructuredProtocolStatus(ctx *analysisContext, fn *ast.FuncDecl) {
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		switch current := node.(type) {
		case nil:
			return false
		case *ast.FuncLit:
			return false
		case *ast.CallExpr:
			if predicate := gomegaStructuredProtocolStatusPredicateMatcher(ctx, current); predicate != nil {
				ctx.report(ruleGomegaStructuredProtocolStatus, predicate, gomegaStructuredProtocolStatusMatcherDiagnostic())
				return false
			}
		}
		return true
	})
}

func checkGomegaMatcherFactoryPredicateOnlyBoolean(ctx *analysisContext, fn *ast.FuncDecl) {
	boolParams := matcherFactoryBoolParams(ctx, fn)
	proxyBoolParams := matcherFactoryProxyBoolParams(ctx, fn)
	if len(boolParams) > 0 &&
		(matcherFactoryCombinesBoolParamWithErrorPredicate(ctx, fn.Body, boolParams) ||
			matcherFactoryUsesProxyBoolParam(ctx, fn.Body, proxyBoolParams)) {
		return
	}
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		switch current := node.(type) {
		case nil:
			return false
		case *ast.FuncLit:
			return false
		case *ast.CallExpr:
			if predicate := gomegaPredicateOnlyBooleanMatcher(ctx, current); predicate != nil {
				ctx.report(ruleGomegaProxyBoolean, predicate, gomegaMatcherFactoryPredicateOnlyBooleanDiagnostic())
				return false
			}
		}
		return true
	})
}

func matcherFactoryBoolParams(ctx *analysisContext, fn *ast.FuncDecl) map[types.Object]bool {
	if fn.Type.Params == nil {
		return nil
	}
	params := map[types.Object]bool{}
	for _, field := range fn.Type.Params.List {
		if !isBoolType(ctx.pass.TypesInfo.TypeOf(field.Type)) {
			continue
		}
		for _, name := range field.Names {
			if name == nil {
				continue
			}
			if object := ctx.pass.TypesInfo.Defs[name]; object != nil {
				params[object] = true
			}
		}
	}
	return params
}

func matcherFactoryProxyBoolParams(ctx *analysisContext, fn *ast.FuncDecl) map[types.Object]bool {
	if fn.Type.Params == nil {
		return nil
	}
	params := map[types.Object]bool{}
	for _, field := range fn.Type.Params.List {
		if !isBoolType(ctx.pass.TypesInfo.TypeOf(field.Type)) {
			continue
		}
		for _, name := range field.Names {
			if name == nil || !isProxyBooleanName(name.Name) {
				continue
			}
			if object := ctx.pass.TypesInfo.Defs[name]; object != nil {
				params[object] = true
			}
		}
	}
	return params
}

func matcherFactoryUsesProxyBoolParam(ctx *analysisContext, body *ast.BlockStmt, proxyBoolParams map[types.Object]bool) bool {
	if len(proxyBoolParams) == 0 {
		return false
	}
	return matcherFactoryUsesBoolParam(ctx, body, proxyBoolParams)
}

func matcherFactoryUsesBoolParam(ctx *analysisContext, body *ast.BlockStmt, boolParams map[types.Object]bool) bool {
	if len(boolParams) == 0 {
		return false
	}
	found := false
	ast.Inspect(body, func(node ast.Node) bool {
		if found || node == nil {
			return false
		}
		ident, ok := node.(*ast.Ident)
		if !ok {
			return true
		}
		object := ctx.pass.TypesInfo.Uses[ident]
		if object == nil {
			object = ctx.pass.TypesInfo.Defs[ident]
		}
		if boolParams[object] {
			found = true
			return false
		}
		return true
	})
	return found
}

func matcherFactoryContainsWithTransform(body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(node ast.Node) bool {
		if found || node == nil {
			return false
		}
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		if callName(call) == "WithTransform" {
			found = true
			return false
		}
		return true
	})
	return found
}

func matcherFactoryCombinesBoolParamWithErrorPredicate(ctx *analysisContext, body *ast.BlockStmt, boolParams map[types.Object]bool) bool {
	found := false
	ast.Inspect(body, func(node ast.Node) bool {
		if found || node == nil {
			return false
		}
		expr, ok := node.(ast.Expr)
		if !ok {
			return true
		}
		if exprTreeContainsObject(ctx, expr, boolParams) && exprTreeContainsErrorPredicate(ctx, expr) {
			found = true
			return false
		}
		return true
	})
	return found
}

func exprTreeContainsObject(ctx *analysisContext, expr ast.Expr, objects map[types.Object]bool) bool {
	found := false
	ast.Inspect(expr, func(node ast.Node) bool {
		if found || node == nil {
			return false
		}
		ident, ok := node.(*ast.Ident)
		if !ok {
			return true
		}
		object := ctx.pass.TypesInfo.Uses[ident]
		if object == nil {
			object = ctx.pass.TypesInfo.Defs[ident]
		}
		if objects[object] {
			found = true
			return false
		}
		return true
	})
	return found
}

func exprTreeContainsErrorPredicate(ctx *analysisContext, expr ast.Expr) bool {
	found := false
	ast.Inspect(expr, func(node ast.Node) bool {
		if found || node == nil {
			return false
		}
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		if callIsErrorPredicate(ctx, call) {
			found = true
			return false
		}
		return true
	})
	return found
}

func callIsErrorPredicate(ctx *analysisContext, call *ast.CallExpr) bool {
	packagePath, name := calleePackageAndName(ctx, call)
	if packagePath == "errors" && (name == "Is" || name == "As") {
		return true
	}
	if len(call.Args) == 0 {
		return false
	}
	if typeImplementsError(ctx.pass.TypesInfo.TypeOf(call.Args[0])) {
		canonical := canonicalName(name)
		return strings.HasPrefix(canonical, "is") && strings.Contains(canonical, "error") ||
			strings.HasSuffix(canonical, "error") ||
			strings.Contains(canonical, "status")
	}
	return false
}

func checkGomegaMatcherFactoryGenericHaveOccurred(ctx *analysisContext, fn *ast.FuncDecl) {
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		switch current := node.(type) {
		case nil:
			return false
		case *ast.FuncLit:
			return false
		case *ast.CallExpr:
			if predicate := gomegaGenericErrorPredicateMatcher(ctx, current); predicate != nil {
				ctx.report(ruleGomegaGenericHaveOccurred, predicate, gomegaGenericHaveOccurredDiagnostic())
				return false
			}
		case *ast.ReturnStmt:
			for _, result := range current.Results {
				if predicate := gomegaGenericErrorPredicateMatcherInExpr(ctx, result); predicate != nil {
					ctx.report(ruleGomegaGenericHaveOccurred, predicate, gomegaGenericHaveOccurredDiagnostic())
				}
				if matcherTreeContainsPositiveHaveOccurred(result) {
					ctx.report(ruleGomegaGenericHaveOccurred, result, gomegaGenericHaveOccurredDiagnostic())
				}
				if matcherTreeContainsGenericErrorPresence(result) {
					ctx.report(ruleGomegaGenericHaveOccurred, result, gomegaGenericHaveOccurredDiagnostic())
				}
			}
			return false
		}
		return true
	})
}

func checkGomegaMatcherFactoryGenericToolErrorArgs(ctx *analysisContext, fn *ast.FuncDecl) {
	if matcherFactoryGenericToolErrorName(fn.Name.Name) {
		ctx.report(ruleGomegaGenericHaveOccurred, fn.Name, gomegaGenericHaveOccurredDiagnostic())
		return
	}
	var offender ast.Node
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		if offender != nil || node == nil {
			return false
		}
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		if !isMatcherNamed(call, "HaveField") || len(call.Args) < 2 {
			return true
		}
		fieldName, ok := stringLiteralValue(call.Args[0])
		if !ok || canonicalName(fieldName) != "args" {
			return true
		}
		if matcherTreeContainsStringFragmentMatcher(ctx, call.Args[1]) {
			offender = call
			return false
		}
		return true
	})
	if offender != nil {
		ctx.report(ruleGomegaGenericHaveOccurred, offender, gomegaGenericHaveOccurredDiagnostic())
	}
}

func matcherFactoryGenericToolErrorName(name string) bool {
	canonical := canonicalName(name)
	return strings.Contains(canonical, "generic") &&
		strings.Contains(canonical, "tool") &&
		strings.Contains(canonical, "error")
}

func matcherTreeContainsStringFragmentMatcher(ctx *analysisContext, expr ast.Expr) bool {
	found := false
	ast.Inspect(expr, func(node ast.Node) bool {
		if found || node == nil {
			return false
		}
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		if isMatcherNamed(call, "ContainSubstring", "HavePrefix", "HaveSuffix", "MatchRegexp") && callHasStringArg(ctx, call) {
			found = true
			return false
		}
		return true
	})
	return found
}

func gomegaGenericErrorPredicateMatcherInExpr(ctx *analysisContext, expr ast.Expr) ast.Expr {
	var predicate ast.Expr
	ast.Inspect(expr, func(node ast.Node) bool {
		if predicate != nil || node == nil {
			return false
		}
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		predicate = gomegaGenericErrorPredicateMatcher(ctx, call)
		return predicate == nil
	})
	return predicate
}

func gomegaGenericErrorPredicateMatcher(ctx *analysisContext, call *ast.CallExpr) ast.Expr {
	if callName(call) != "MakeMatcher" || len(call.Args) == 0 {
		return nil
	}
	fn, ok := call.Args[0].(*ast.FuncLit)
	if !ok {
		return nil
	}
	errorParams := gomegaMatcherErrorParams(ctx, fn)
	if len(errorParams) == 0 {
		return nil
	}
	return genericErrorPredicateReturn(ctx, fn.Body, errorParams)
}

func gomegaStructuredProtocolStatusPredicateMatcher(ctx *analysisContext, call *ast.CallExpr) ast.Expr {
	if callName(call) != "MakeMatcher" || len(call.Args) == 0 {
		return nil
	}
	fn, ok := call.Args[0].(*ast.FuncLit)
	if !ok {
		return nil
	}
	return rawStructuredProtocolStatusPredicateReturn(ctx, fn.Body)
}

func gomegaLastErrorRenderedPredicateMatcher(ctx *analysisContext, call *ast.CallExpr) ast.Expr {
	if callName(call) != "MakeMatcher" || len(call.Args) == 0 {
		return nil
	}
	fn, ok := call.Args[0].(*ast.FuncLit)
	if !ok {
		return nil
	}
	return lastErrorRenderedPredicateReturn(ctx, fn.Body)
}

func gomegaProxyBooleanPredicateMatcher(ctx *analysisContext, call *ast.CallExpr) ast.Expr {
	if callName(call) != "MakeMatcher" || len(call.Args) == 0 {
		return nil
	}
	fn, ok := call.Args[0].(*ast.FuncLit)
	if !ok {
		return nil
	}
	return proxyBooleanPredicateReturn(ctx, fn.Body)
}

func gomegaPredicateOnlyBooleanMatcher(ctx *analysisContext, call *ast.CallExpr) ast.Expr {
	if callName(call) != "MakeMatcher" || len(call.Args) == 0 {
		return nil
	}
	fn, ok := call.Args[0].(*ast.FuncLit)
	if !ok {
		return nil
	}
	if gomegaGenericErrorPredicateMatcher(ctx, call) != nil ||
		gomegaProxyBooleanPredicateMatcher(ctx, call) != nil ||
		gomegaLastErrorRenderedPredicateMatcher(ctx, call) != nil ||
		gomegaStructuredProtocolStatusPredicateMatcher(ctx, call) != nil {
		return nil
	}
	return predicateOnlyBooleanReturn(ctx, fn.Body)
}

func predicateOnlyBooleanReturn(ctx *analysisContext, body *ast.BlockStmt) ast.Expr {
	var predicate ast.Expr
	ast.Inspect(body, func(node ast.Node) bool {
		if predicate != nil || node == nil {
			return false
		}
		switch current := node.(type) {
		case *ast.FuncLit:
			return false
		case *ast.ReturnStmt:
			if len(current.Results) > 0 && exprIsPredicateOnlyBoolean(ctx, current.Results[0]) {
				predicate = current.Results[0]
			}
			return false
		}
		return true
	})
	return predicate
}

func exprIsPredicateOnlyBoolean(ctx *analysisContext, expr ast.Expr) bool {
	expr = unparenExpr(expr)
	if ident, ok := expr.(*ast.Ident); ok && (ident.Name == "true" || ident.Name == "false") {
		return false
	}
	if !isBoolType(ctx.pass.TypesInfo.TypeOf(expr)) {
		return false
	}
	switch current := expr.(type) {
	case *ast.BinaryExpr:
		return current.Op == token.LAND ||
			current.Op == token.LOR ||
			isBooleanProducingBinaryOp(current.Op)
	case *ast.CallExpr:
		return true
	case *ast.SelectorExpr:
		return true
	case *ast.UnaryExpr:
		return current.Op == token.NOT && exprIsPredicateOnlyBoolean(ctx, current.X)
	default:
		return false
	}
}

func proxyBooleanPredicateReturn(ctx *analysisContext, body *ast.BlockStmt) ast.Expr {
	var predicate ast.Expr
	ast.Inspect(body, func(node ast.Node) bool {
		if predicate != nil || node == nil {
			return false
		}
		switch current := node.(type) {
		case *ast.FuncLit:
			return false
		case *ast.ReturnStmt:
			if len(current.Results) > 0 && exprIsPureProxyBooleanPredicate(ctx, current.Results[0]) {
				predicate = current.Results[0]
			}
			return false
		}
		return true
	})
	return predicate
}

func exprIsPureProxyBooleanPredicate(ctx *analysisContext, expr ast.Expr) bool {
	pure, hasProxy := proxyBooleanPredicateParts(ctx, expr)
	return pure && hasProxy
}

func proxyBooleanPredicateParts(ctx *analysisContext, expr ast.Expr) (pure bool, hasProxy bool) {
	expr = unparenExpr(expr)
	switch current := expr.(type) {
	case *ast.Ident:
		isProxy := isProxyBooleanName(current.Name) && isBoolType(ctx.pass.TypesInfo.TypeOf(current))
		return isProxy, isProxy
	case *ast.SelectorExpr:
		isProxy := selectorIsProxyBoolean(ctx, current)
		return isProxy, isProxy
	case *ast.UnaryExpr:
		if current.Op != token.NOT {
			return false, false
		}
		return proxyBooleanPredicateParts(ctx, current.X)
	case *ast.BinaryExpr:
		if current.Op != token.LAND && current.Op != token.LOR {
			return false, false
		}
		leftPure, leftProxy := proxyBooleanPredicateParts(ctx, current.X)
		rightPure, rightProxy := proxyBooleanPredicateParts(ctx, current.Y)
		return leftPure && rightPure, leftProxy || rightProxy
	default:
		return false, false
	}
}

func lastErrorRenderedPredicateReturn(ctx *analysisContext, body *ast.BlockStmt) ast.Expr {
	var predicate ast.Expr
	ast.Inspect(body, func(node ast.Node) bool {
		if predicate != nil || node == nil {
			return false
		}
		switch current := node.(type) {
		case *ast.FuncLit:
			return false
		case *ast.ReturnStmt:
			if len(current.Results) > 0 {
				predicate = lastErrorRenderedPredicate(ctx, current.Results[0])
			}
			return false
		}
		return true
	})
	return predicate
}

func lastErrorRenderedPredicate(ctx *analysisContext, expr ast.Expr) ast.Expr {
	expr = unparenExpr(expr)
	switch current := expr.(type) {
	case *ast.BinaryExpr:
		if current.Op == token.LAND || current.Op == token.LOR {
			if predicate := lastErrorRenderedPredicate(ctx, current.X); predicate != nil {
				return predicate
			}
			return lastErrorRenderedPredicate(ctx, current.Y)
		}
		if (current.Op == token.EQL || current.Op == token.NEQ) &&
			(exprContainsLastErrorSelector(current.X) || exprContainsLastErrorSelector(current.Y)) {
			return current
		}
	case *ast.CallExpr:
		if callUsesLastErrorRenderedText(ctx, current) {
			return current
		}
	}
	return nil
}

func callUsesLastErrorRenderedText(ctx *analysisContext, call *ast.CallExpr) bool {
	if callName(call) == "Match" {
		return callArgsContainLastErrorSelector(call)
	}
	packagePath, name := calleePackageAndName(ctx, call)
	if packagePath == "strings" && (name == "Contains" || name == "HasPrefix" || name == "HasSuffix") {
		return callArgsContainLastErrorSelector(call)
	}
	return false
}

func callArgsContainLastErrorSelector(call *ast.CallExpr) bool {
	for _, arg := range call.Args {
		if exprContainsLastErrorSelector(arg) {
			return true
		}
	}
	return false
}

func exprContainsLastErrorSelector(expr ast.Expr) bool {
	found := false
	ast.Inspect(expr, func(node ast.Node) bool {
		if found || node == nil {
			return false
		}
		if _, ok := node.(*ast.FuncLit); ok {
			return false
		}
		selector, ok := node.(*ast.SelectorExpr)
		if ok && selector.Sel.Name == "LastError" {
			found = true
			return false
		}
		return true
	})
	return found
}

func rawStructuredProtocolStatusPredicateReturn(ctx *analysisContext, body *ast.BlockStmt) ast.Expr {
	var predicate ast.Expr
	ast.Inspect(body, func(node ast.Node) bool {
		if predicate != nil || node == nil {
			return false
		}
		switch current := node.(type) {
		case *ast.FuncLit:
			return false
		case *ast.ReturnStmt:
			if len(current.Results) > 0 {
				predicate = rawStructuredProtocolStatusPredicate(ctx, current.Results[0])
			}
			return false
		}
		return true
	})
	return predicate
}

func rawStructuredProtocolStatusPredicate(ctx *analysisContext, expr ast.Expr) ast.Expr {
	expr = unparenExpr(expr)
	if rawStructuredProtocolStatusNilGuard(ctx, expr) {
		return nil
	}
	if rawStructuredProtocolStatusSelector(ctx, expr) {
		return expr
	}
	switch current := expr.(type) {
	case *ast.UnaryExpr:
		if current.Op != token.NOT {
			return nil
		}
		if rawStructuredProtocolStatusSelector(ctx, current.X) {
			return current
		}
	case *ast.BinaryExpr:
		if current.Op != token.LAND && current.Op != token.LOR {
			return nil
		}
		left := rawStructuredProtocolStatusPredicate(ctx, current.X)
		right := rawStructuredProtocolStatusPredicate(ctx, current.Y)
		if left == nil && !rawStructuredProtocolStatusNilGuard(ctx, current.X) {
			return nil
		}
		if right == nil && !rawStructuredProtocolStatusNilGuard(ctx, current.Y) {
			return nil
		}
		if left != nil {
			return left
		}
		return right
	}
	return nil
}

func rawStructuredProtocolStatusSelector(ctx *analysisContext, expr ast.Expr) bool {
	selector, ok := unparenExpr(expr).(*ast.SelectorExpr)
	return ok && selector.Sel.Name == "IsError" && exprSuggestsStructuredProtocolValue(ctx, selector.X)
}

func rawStructuredProtocolStatusNilGuard(ctx *analysisContext, expr ast.Expr) bool {
	binary, ok := unparenExpr(expr).(*ast.BinaryExpr)
	if !ok || binary.Op != token.EQL && binary.Op != token.NEQ {
		return false
	}
	return isNilExpr(binary.X) && exprSuggestsStructuredProtocolValue(ctx, binary.Y) ||
		exprSuggestsStructuredProtocolValue(ctx, binary.X) && isNilExpr(binary.Y)
}

func gomegaMatcherErrorParams(ctx *analysisContext, fn *ast.FuncLit) map[types.Object]bool {
	if fn.Type.Params == nil {
		return nil
	}
	errorParams := map[types.Object]bool{}
	for _, field := range fn.Type.Params.List {
		if !typeImplementsError(ctx.pass.TypesInfo.TypeOf(field.Type)) {
			continue
		}
		for _, name := range field.Names {
			if name == nil {
				continue
			}
			if object := ctx.pass.TypesInfo.Defs[name]; object != nil {
				errorParams[object] = true
			}
		}
	}
	return errorParams
}

func genericErrorPredicateReturn(ctx *analysisContext, body *ast.BlockStmt, errorParams map[types.Object]bool) ast.Expr {
	var predicate ast.Expr
	ast.Inspect(body, func(node ast.Node) bool {
		if predicate != nil || node == nil {
			return false
		}
		switch current := node.(type) {
		case *ast.FuncLit:
			return false
		case *ast.ReturnStmt:
			if len(current.Results) > 0 && isErrorParamNotNilCheck(ctx, current.Results[0], errorParams) {
				predicate = current.Results[0]
			}
			return false
		}
		return true
	})
	return predicate
}

func isErrorParamNotNilCheck(ctx *analysisContext, expr ast.Expr, errorParams map[types.Object]bool) bool {
	binary, ok := unparenExpr(expr).(*ast.BinaryExpr)
	if !ok || binary.Op != token.NEQ {
		return false
	}
	return isErrorParamIdent(ctx, binary.X, errorParams) && isNilExpr(binary.Y) ||
		isNilExpr(binary.X) && isErrorParamIdent(ctx, binary.Y, errorParams)
}

func isErrorParamIdent(ctx *analysisContext, expr ast.Expr, errorParams map[types.Object]bool) bool {
	ident, ok := unparenExpr(expr).(*ast.Ident)
	if !ok {
		return false
	}
	object := ctx.pass.TypesInfo.Uses[ident]
	if object == nil {
		object = ctx.pass.TypesInfo.Defs[ident]
	}
	return errorParams[object]
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
	return exprIsErrorStringCall(ctx, assertion.actual)
}

func matcherTreeUsesErrError(ctx *analysisContext, expr ast.Expr) bool {
	found := false
	ast.Inspect(expr, func(node ast.Node) bool {
		if found || node == nil {
			return false
		}
		call, ok := node.(*ast.CallExpr)
		if ok && exprIsErrorStringCall(ctx, call) {
			found = true
			return false
		}
		return true
	})
	return found
}

func checkErrorStringPredicate(ctx *analysisContext, call *ast.CallExpr) {
	if !isTestFile(ctx.filename(call.Pos())) {
		return
	}
	packagePath, name := calleePackageAndName(ctx, call)
	if packagePath != "strings" || name != "Contains" || len(call.Args) == 0 {
		return
	}
	if exprIsErrorStringCall(ctx, call.Args[0]) {
		ctx.report(ruleGomegaErrorString, call.Args[0], gomegaErrorStringMatcherDiagnostic())
		return
	}
	if exprSuggestsTestRenderedProseContract(call.Args[0]) {
		ctx.report(ruleGomegaStringsContains, call, gomegaRenderedMessageStringsContainsDiagnostic())
	}
}

func exprIsErrorStringCall(ctx *analysisContext, expr ast.Expr) bool {
	expr = unparenExpr(expr)
	call, ok := expr.(*ast.CallExpr)
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

func assertionUsesGenericHaveOccurred(ctx *analysisContext, assertion gomegaAssertion) bool {
	if !isMatcherNamed(assertion.matcher, "HaveOccurred") || isNegativeAssertionMethod(assertion.method) {
		return false
	}
	return assertionTargetsError(ctx, assertion)
}

func assertionMatcherTreeUsesGenericHaveOccurred(assertion gomegaAssertion) bool {
	if isNegativeAssertionMethod(assertion.method) || isMatcherNamed(assertion.matcher, "HaveOccurred") {
		return false
	}
	return matcherTreeContainsPositiveHaveOccurred(assertion.matcher)
}

func assertionMatcherTreeUsesGenericErrorPresence(assertion gomegaAssertion) bool {
	if isNegativeAssertionMethod(assertion.method) {
		return false
	}
	return matcherTreeContainsGenericErrorPresence(assertion.matcher)
}

func matcherTreeContainsPositiveHaveOccurred(expr ast.Expr) bool {
	switch current := unparenExpr(expr).(type) {
	case *ast.CallExpr:
		if isMatcherNamed(current, "Not") {
			return false
		}
		if isMatcherNamed(current, "HaveOccurred") {
			return true
		}
		for _, arg := range current.Args {
			if matcherTreeContainsPositiveHaveOccurred(arg) {
				return true
			}
		}
	case *ast.CompositeLit:
		for _, elt := range current.Elts {
			if matcherTreeContainsPositiveHaveOccurred(elt) {
				return true
			}
		}
	case *ast.KeyValueExpr:
		return matcherTreeContainsPositiveHaveOccurred(current.Value)
	case *ast.TypeAssertExpr:
		return matcherTreeContainsPositiveHaveOccurred(current.X)
	}
	return false
}

func matcherTreeContainsGenericErrorPresence(expr ast.Expr) bool {
	switch current := unparenExpr(expr).(type) {
	case *ast.CallExpr:
		if matcherCallContainsGenericErrorPresence(current) {
			return true
		}
		for _, arg := range current.Args {
			if matcherTreeContainsGenericErrorPresence(arg) {
				return true
			}
		}
	case *ast.CompositeLit:
		for _, elt := range current.Elts {
			if matcherTreeContainsGenericErrorPresence(elt) {
				return true
			}
		}
	case *ast.KeyValueExpr:
		if errorPresenceMatcherField(current.Key) && matcherIsGenericErrorPresence(current.Value) {
			return true
		}
		return matcherTreeContainsGenericErrorPresence(current.Value)
	case *ast.TypeAssertExpr:
		return matcherTreeContainsGenericErrorPresence(current.X)
	}
	return false
}

func matcherCallContainsGenericErrorPresence(call *ast.CallExpr) bool {
	if !isMatcherNamed(call, "HaveField") || len(call.Args) < 2 {
		return false
	}
	return errorPresenceMatcherField(call.Args[0]) && matcherIsGenericErrorPresence(call.Args[1])
}

func errorPresenceMatcherField(expr ast.Expr) bool {
	name := canonicalName(keyName(expr))
	return name == "err" ||
		name == "error" ||
		strings.HasSuffix(name, "err") ||
		strings.HasSuffix(name, "error") ||
		strings.HasSuffix(name, "errors")
}

func matcherIsGenericErrorPresence(expr ast.Expr) bool {
	call, ok := unparenExpr(expr).(*ast.CallExpr)
	if !ok || !isMatcherNamed(call, "Not") || len(call.Args) != 1 {
		return false
	}
	inner, ok := unparenExpr(call.Args[0]).(*ast.CallExpr)
	return ok && (isMatcherNamed(inner, "BeNil") || isEqualNilMatcher(inner))
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

func assertionUsesRenderedMessageStringMatcher(ctx *analysisContext, assertion gomegaAssertion) bool {
	if assertionUsesErrError(ctx, assertion) || matcherTreeUsesErrError(ctx, assertion.matcher) || assertionTargetsLastError(assertion.actual) {
		return false
	}
	if !isStringType(ctx.pass, assertion.actual) {
		return false
	}
	if selector, ok := unparenExpr(assertion.actual).(*ast.SelectorExpr); ok &&
		structuredErrorFieldName(selector.Sel.Name) &&
		exprSuggestsStructuredErrorValue(ctx, selector.X) {
		return false
	}
	if !exprSuggestsTestRenderedProseContract(assertion.actual) {
		return false
	}
	return matcherTreeContainsStringContentMatcher(ctx, assertion.matcher)
}

func matcherTreeContainsStringContentMatcher(ctx *analysisContext, expr ast.Expr) bool {
	switch current := unparenExpr(expr).(type) {
	case *ast.CallExpr:
		if isMatcherNamed(current, "Equal", "ContainSubstring", "HavePrefix", "HaveSuffix", "MatchRegexp") {
			return callHasStringArg(ctx, current)
		}
		for _, arg := range current.Args {
			if matcherTreeContainsStringContentMatcher(ctx, arg) {
				return true
			}
		}
	case *ast.CompositeLit:
		for _, elt := range current.Elts {
			if matcherTreeContainsStringContentMatcher(ctx, elt) {
				return true
			}
		}
	case *ast.KeyValueExpr:
		return matcherTreeContainsStringContentMatcher(ctx, current.Value)
	case *ast.TypeAssertExpr:
		return matcherTreeContainsStringContentMatcher(ctx, current.X)
	}
	return false
}

func assertionUsesLastErrorNotEmpty(assertion gomegaAssertion) bool {
	if !assertionTargetsLastError(assertion.actual) {
		return false
	}
	if isNegativeAssertionMethod(assertion.method) {
		return isEmptyMatcher(assertion.matcher)
	}
	return isNegatedEmptyMatcher(assertion.matcher)
}

func assertionTargetsLastError(expr ast.Expr) bool {
	selector, ok := unparenExpr(expr).(*ast.SelectorExpr)
	return ok && selector.Sel.Name == "LastError"
}

func isEmptyMatcher(matcher *ast.CallExpr) bool {
	return isMatcherNamed(matcher, "BeEmpty") || isEqualEmptyStringMatcher(matcher)
}

func isNegatedEmptyMatcher(matcher *ast.CallExpr) bool {
	return isMatcherNamed(matcher, "Not") && len(matcher.Args) == 1 && exprIsEmptyMatcher(matcher.Args[0])
}

func assertionUsesNonEmptyCollection(ctx *analysisContext, assertion gomegaAssertion) bool {
	if !assertionActualIsCollection(ctx, assertion.actual) {
		return false
	}
	if isNegativeAssertionMethod(assertion.method) {
		return isEmptyMatcher(assertion.matcher)
	}
	return isNegatedEmptyMatcher(assertion.matcher)
}

func assertionUsesSemanticScalarNotEmpty(ctx *analysisContext, assertion gomegaAssertion) bool {
	if assertionActualIsCollection(ctx, assertion.actual) {
		return false
	}
	if !assertionActualIsSemanticScalar(assertion.actual) {
		return false
	}
	if isNegativeAssertionMethod(assertion.method) {
		return isEmptyMatcher(assertion.matcher)
	}
	return isNegatedEmptyMatcher(assertion.matcher)
}

func assertionActualIsSemanticScalar(expr ast.Expr) bool {
	names := semanticScalarExprNames(expr)
	if len(names) == 0 {
		return false
	}
	hasSelectorContext := len(names) > 1
	for i, name := range names {
		context := semanticScalarContext(names, i)
		if typ, ok := semanticTypeForFieldName(name, context); ok && typ != "" {
			return true
		}
		if canonicalName(name) == "id" {
			if typ, ok := semanticTypeForBareIDContext(context); ok && typ != "" {
				return true
			}
		}
		if canonicalName(name) == "hash" {
			if typ, ok := semanticTypeForBareHashContext(context); ok && typ != "" {
				return true
			}
		}
		if hasSelectorContext && semanticScalarTokenName(name) {
			return true
		}
		if semanticName(name) {
			return true
		}
	}
	return false
}

func semanticScalarExprNames(expr ast.Expr) []string {
	switch e := unparenExpr(expr).(type) {
	case *ast.Ident:
		return []string{e.Name}
	case *ast.SelectorExpr:
		names := semanticScalarExprNames(e.X)
		return append(names, e.Sel.Name)
	default:
		return nil
	}
}

func semanticScalarContext(names []string, index int) string {
	if index <= 0 || index > len(names) {
		return ""
	}
	return strings.Join(names[:index], "")
}

func semanticScalarTokenName(name string) bool {
	canonical := canonicalName(name)
	return canonical != "token" && strings.HasSuffix(canonical, "token")
}

func assertionActualIsCollection(ctx *analysisContext, expr ast.Expr) bool {
	typ := ctx.pass.TypesInfo.TypeOf(expr)
	return typeIsSliceOrArray(typ) || typeIsMap(typ)
}

func exprIsEmptyMatcher(expr ast.Expr) bool {
	call, ok := unparenExpr(expr).(*ast.CallExpr)
	return ok && isEmptyMatcher(call)
}

func isEqualEmptyStringMatcher(matcher *ast.CallExpr) bool {
	if !isMatcherNamed(matcher, "Equal") || len(matcher.Args) != 1 {
		return false
	}
	lit, ok := unparenExpr(matcher.Args[0]).(*ast.BasicLit)
	return ok && lit.Kind == token.STRING && lit.Value == `""`
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

func matcherUsesOSIsNotExistTransform(ctx *analysisContext, matcher *ast.CallExpr) bool {
	if !isMatcherNamed(matcher, "WithTransform") || len(matcher.Args) < 2 || !exprIsBooleanMatcher(matcher.Args[1]) {
		return false
	}
	packagePath, name := referencedFunctionPackageAndName(ctx, matcher.Args[0])
	return packagePath == "os" && name == "IsNotExist"
}

func referencedFunctionPackageAndName(ctx *analysisContext, expr ast.Expr) (string, string) {
	switch fun := unparenExpr(expr).(type) {
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

func assertionUsesControlStatus(assertion gomegaAssertion) bool {
	call, ok := assertion.actual.(*ast.CallExpr)
	return ok && callName(call) == "IsControlStatus"
}

func assertionUsesLenEqual(assertion gomegaAssertion) bool {
	call, ok := assertion.actual.(*ast.CallExpr)
	return ok && callName(call) == "len" && isMatcherNamed(assertion.matcher, "Equal", "BeNumerically")
}

func assertionUsesBinaryBoolean(assertion gomegaAssertion) bool {
	binary, ok := assertion.actual.(*ast.BinaryExpr)
	return ok && isBooleanProducingBinaryOp(binary.Op) && isBooleanMatcher(assertion.matcher)
}

func assertionUsesBooleanLiteral(assertion gomegaAssertion) bool {
	ident, ok := unparenExpr(assertion.actual).(*ast.Ident)
	return ok && (ident.Name == "true" || ident.Name == "false") && isBooleanMatcher(assertion.matcher)
}

func assertionUsesCommaOKBoolean(ctx *analysisContext, assertion gomegaAssertion) bool {
	ident, ok := unparenExpr(assertion.actual).(*ast.Ident)
	if ok && isBooleanMatcher(assertion.matcher) && identIsCommaOKResult(ctx, ident) {
		return true
	}
	return compositeActualContainsIdent(assertion.actual, func(ident *ast.Ident) bool {
		return identIsCommaOKResult(ctx, ident)
	})
}

func assertionUsesProxyBoolean(ctx *analysisContext, assertion gomegaAssertion) bool {
	if assertionUsesDedicatedBooleanPredicate(ctx, assertion) {
		return false
	}
	ident, ok := unparenExpr(assertion.actual).(*ast.Ident)
	if ok && isBooleanMatcher(assertion.matcher) && (isProxyBooleanName(ident.Name) || identIsSemanticBooleanResult(ctx, ident)) {
		if identIsCommaOKResult(ctx, ident) {
			return false
		}
		return isBoolType(ctx.pass.TypesInfo.TypeOf(ident))
	}
	selector, ok := unparenExpr(assertion.actual).(*ast.SelectorExpr)
	if ok && isBooleanMatcher(assertion.matcher) && selectorIsProxyBoolean(ctx, selector) {
		return true
	}
	call, ok := unparenExpr(assertion.actual).(*ast.CallExpr)
	if ok && isBooleanMatcher(assertion.matcher) && callReturnsProxyBoolean(ctx, call) {
		return true
	}
	return compositeActualContainsIdent(assertion.actual, func(ident *ast.Ident) bool {
		return (isProxyBooleanName(ident.Name) || identIsSemanticBooleanResult(ctx, ident)) &&
			isBoolType(ctx.pass.TypesInfo.TypeOf(ident)) &&
			!identIsCommaOKResult(ctx, ident)
	})
}

func assertionUsesDedicatedBooleanPredicate(ctx *analysisContext, assertion gomegaAssertion) bool {
	return assertionUsesStringsContains(ctx, assertion) ||
		assertionUsesStringsPredicate(ctx, assertion, "HasPrefix") ||
		assertionUsesStringsPredicate(ctx, assertion, "HasSuffix") ||
		assertionUsesRegexpMatchString(ctx, assertion) ||
		assertionUsesErrorsIs(ctx, assertion) ||
		assertionUsesErrorsAs(ctx, assertion) ||
		assertionUsesControlStatus(assertion) ||
		assertionUsesOSIsNotExist(ctx, assertion)
}

func selectorIsProxyBoolean(ctx *analysisContext, selector *ast.SelectorExpr) bool {
	return isProxyBooleanName(selector.Sel.Name) &&
		isBoolType(ctx.pass.TypesInfo.TypeOf(selector))
}

func callReturnsProxyBoolean(ctx *analysisContext, call *ast.CallExpr) bool {
	if !isBoolType(ctx.pass.TypesInfo.TypeOf(call)) {
		return false
	}
	name := callName(call)
	if semanticBooleanCallName(name) {
		return true
	}
	if fn := calledFunctionObject(ctx, call); fn != nil {
		return semanticBooleanCallName(fn.Name())
	}
	return false
}

func semanticBooleanCallName(name string) bool {
	if isProxyBooleanName(name) {
		return true
	}
	canonical := canonicalName(name)
	for _, prefix := range []string{
		"is", "has", "can", "should", "supports", "allows", "contains", "get", "seen",
	} {
		if strings.HasPrefix(canonical, prefix) {
			return true
		}
	}
	for _, marker := range []string{
		"allowed", "heartbeat", "enabled", "disabled", "public", "running", "ready",
	} {
		if strings.Contains(canonical, marker) {
			return true
		}
	}
	return false
}

func isProxyBooleanName(name string) bool {
	switch strings.ToLower(name) {
	case "ok", "found", "exists", "present", "matched", "valid", "success", "done", "called",
		"changed", "created", "updated", "modified", "deleted", "removed", "rewritten", "applied",
		"accepted", "rejected", "renamed", "enabled", "disabled", "ready", "started", "stopped", "invoked",
		"running", "canceled", "cancelled", "healthy", "home":
		return true
	}
	for _, suffix := range []string{
		"OK", "Ok", "Found", "Exists", "Present", "Matched", "Valid", "Success", "Done", "Called",
		"Changed", "Created", "Updated", "Modified", "Deleted", "Removed", "Rewritten", "Applied",
		"Accepted", "Rejected", "Renamed", "Enabled", "Disabled", "Ready", "Started", "Stopped", "Invoked",
		"Running", "Canceled", "Cancelled", "Healthy", "Home",
	} {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return identifierHasWord(name, "set")
}

func identifierHasWord(name string, want string) bool {
	for _, word := range identifierWords(name) {
		if word == want {
			return true
		}
	}
	return false
}

func identifierWords(name string) []string {
	var words []string
	var current []rune
	flush := func() {
		if len(current) == 0 {
			return
		}
		words = append(words, string(current))
		current = nil
	}
	runes := []rune(name)
	for i, r := range runes {
		if r == '_' || r == '-' {
			flush()
			continue
		}
		if len(current) > 0 && unicode.IsUpper(r) {
			previous := runes[i-1]
			nextIsLower := i+1 < len(runes) && unicode.IsLower(runes[i+1])
			if unicode.IsLower(previous) || unicode.IsDigit(previous) || unicode.IsUpper(previous) && nextIsLower {
				flush()
			}
		}
		current = append(current, unicode.ToLower(r))
	}
	flush()
	return words
}

func callLaundersBooleanToStringState(ctx *analysisContext, call *ast.CallExpr) bool {
	if !isString(ctx.pass.TypesInfo.TypeOf(call)) || !testLocalBooleanStateHelperCall(ctx, call) {
		return false
	}
	for _, arg := range call.Args {
		if isBoolType(ctx.pass.TypesInfo.TypeOf(arg)) {
			return true
		}
	}
	return testLocalBooleanStateHelperBranchesOnPredicate(ctx, call)
}

func testLocalBooleanStateHelperCall(ctx *analysisContext, call *ast.CallExpr) bool {
	name := strings.ToLower(callName(call))
	if !strings.Contains(name, "state") && !strings.Contains(name, "status") && !strings.Contains(name, "label") && !strings.Contains(name, "scope") {
		return false
	}
	ident, ok := unparenExpr(call.Fun).(*ast.Ident)
	if !ok {
		return false
	}
	obj := ctx.pass.TypesInfo.ObjectOf(ident)
	if obj == nil {
		return false
	}
	filename := ctx.filename(obj.Pos())
	return isTestFile(filename) || isTestSupportFile(filename)
}

func testLocalBooleanStateHelperBranchesOnPredicate(ctx *analysisContext, call *ast.CallExpr) bool {
	fn := testLocalBooleanStateHelperDecl(ctx, call)
	if fn == nil || fn.Body == nil {
		return false
	}
	found := false
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		if found || node == nil {
			return false
		}
		if _, ok := node.(*ast.FuncLit); ok {
			return false
		}
		ifStmt, ok := node.(*ast.IfStmt)
		if !ok {
			return true
		}
		if exprTreeContainsBoolCall(ctx, ifStmt.Cond) {
			found = true
			return false
		}
		return true
	})
	return found
}

func testLocalBooleanStateHelperDecl(ctx *analysisContext, call *ast.CallExpr) *ast.FuncDecl {
	if !testLocalBooleanStateHelperCall(ctx, call) {
		return nil
	}
	ident, ok := unparenExpr(call.Fun).(*ast.Ident)
	if !ok {
		return nil
	}
	target := ctx.pass.TypesInfo.ObjectOf(ident)
	if target == nil {
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

func exprTreeContainsBoolCall(ctx *analysisContext, expr ast.Expr) bool {
	found := false
	ast.Inspect(unparenExpr(expr), func(node ast.Node) bool {
		if found || node == nil {
			return false
		}
		if _, ok := node.(*ast.FuncLit); ok {
			return false
		}
		call, ok := node.(*ast.CallExpr)
		if ok && isBoolType(ctx.pass.TypesInfo.TypeOf(call)) {
			found = true
			return false
		}
		return true
	})
	return found
}

func identIsCommaOKResult(ctx *analysisContext, ident *ast.Ident) bool {
	body := enclosingFunctionBody(ctx, ident)
	if body == nil {
		return false
	}
	targetObject := ctx.pass.TypesInfo.ObjectOf(ident)
	found := false
	ast.Inspect(body, func(node ast.Node) bool {
		if found || node == nil {
			return false
		}
		if _, ok := node.(*ast.FuncLit); ok {
			return false
		}
		assign, ok := node.(*ast.AssignStmt)
		if !ok || assign.Pos() > ident.Pos() || len(assign.Lhs) != 2 || len(assign.Rhs) != 1 {
			return true
		}
		lhsIdent, ok := unparenExpr(assign.Lhs[1]).(*ast.Ident)
		if !ok || !sameIdentifierObject(ctx, lhsIdent, ident, targetObject) {
			return true
		}
		found = exprIsCommaOKSource(ctx, assign.Rhs[0])
		return !found
	})
	return found
}

func identIsSemanticBooleanResult(ctx *analysisContext, ident *ast.Ident) bool {
	body := enclosingFunctionBody(ctx, ident)
	if body == nil {
		return false
	}
	targetObject := ctx.pass.TypesInfo.ObjectOf(ident)
	found := false
	ast.Inspect(body, func(node ast.Node) bool {
		if found || node == nil {
			return false
		}
		if _, ok := node.(*ast.FuncLit); ok {
			return false
		}
		switch candidate := node.(type) {
		case *ast.AssignStmt:
			if candidate.Pos() > ident.Pos() || len(candidate.Rhs) != 1 {
				return true
			}
			found = semanticBoolAssignmentNamesIdent(ctx, candidate.Lhs, candidate.Rhs[0], ident, targetObject)
			return !found
		case *ast.ValueSpec:
			if candidate.Pos() > ident.Pos() || len(candidate.Values) != 1 {
				return true
			}
			lhs := make([]ast.Expr, 0, len(candidate.Names))
			for _, name := range candidate.Names {
				lhs = append(lhs, name)
			}
			found = semanticBoolAssignmentNamesIdent(ctx, lhs, candidate.Values[0], ident, targetObject)
			return !found
		}
		return true
	})
	return found
}

func semanticBoolAssignmentNamesIdent(ctx *analysisContext, lhs []ast.Expr, rhs ast.Expr, ident *ast.Ident, targetObject types.Object) bool {
	call, ok := unparenExpr(rhs).(*ast.CallExpr)
	if !ok {
		return false
	}
	results, ok := ctx.pass.TypesInfo.TypeOf(call).(*types.Tuple)
	if !ok {
		return false
	}
	for i, lhsExpr := range lhs {
		if i >= results.Len() || !isBoolType(results.At(i).Type()) {
			continue
		}
		lhsIdent, ok := unparenExpr(lhsExpr).(*ast.Ident)
		if !ok || !sameIdentifierObject(ctx, lhsIdent, ident, targetObject) {
			continue
		}
		return callReturnsSemanticBoolean(ctx, call, results, i)
	}
	return false
}

func exprTreeContainsIdent(expr ast.Expr, predicate func(*ast.Ident) bool) bool {
	found := false
	ast.Inspect(unparenExpr(expr), func(node ast.Node) bool {
		if found || node == nil {
			return false
		}
		if _, ok := node.(*ast.FuncLit); ok {
			return false
		}
		ident, ok := node.(*ast.Ident)
		if ok && predicate(ident) {
			found = true
			return false
		}
		return true
	})
	return found
}

func compositeActualContainsIdent(expr ast.Expr, predicate func(*ast.Ident) bool) bool {
	switch actual := unparenExpr(expr).(type) {
	case *ast.CompositeLit:
		return exprTreeContainsIdent(actual, predicate)
	case *ast.UnaryExpr:
		if actual.Op == token.AND {
			return compositeActualContainsIdent(actual.X, predicate)
		}
	}
	return false
}

func exprIsCommaOKSource(ctx *analysisContext, expr ast.Expr) bool {
	switch source := unparenExpr(expr).(type) {
	case *ast.TypeAssertExpr:
		return true
	case *ast.IndexExpr:
		return typeIsMap(ctx.pass.TypesInfo.TypeOf(source.X))
	case *ast.UnaryExpr:
		return source.Op == token.ARROW
	default:
		return false
	}
}

func isComparisonOp(op token.Token) bool {
	switch op {
	case token.EQL, token.NEQ, token.LSS, token.LEQ, token.GTR, token.GEQ:
		return true
	default:
		return false
	}
}

func isBooleanProducingBinaryOp(op token.Token) bool {
	return isComparisonOp(op) || op == token.LAND || op == token.LOR
}

func assertionUsesMapIndexEqual(ctx *analysisContext, assertion gomegaAssertion) bool {
	if mapIndexAssertionActualIsMap(ctx, assertion.actual) {
		return true
	}
	ident, ok := unparenExpr(assertion.actual).(*ast.Ident)
	return ok && identAliasesMapIndex(ctx, ident)
}

func mapIndexAssertionActualIsMap(ctx *analysisContext, expr ast.Expr) bool {
	index, ok := mapIndexAssertionActual(expr)
	return ok && typeIsMap(ctx.pass.TypesInfo.TypeOf(index.X))
}

func mapIndexAssertionActual(expr ast.Expr) (*ast.IndexExpr, bool) {
	switch actual := unparenExpr(expr).(type) {
	case *ast.IndexExpr:
		return actual, true
	case *ast.TypeAssertExpr:
		return mapIndexAssertionActual(actual.X)
	default:
		return nil, false
	}
}

func identAliasesMapIndex(ctx *analysisContext, ident *ast.Ident) bool {
	body := enclosingFunctionBody(ctx, ident)
	if body == nil {
		return false
	}
	targetObject := ctx.pass.TypesInfo.ObjectOf(ident)
	found := false
	ast.Inspect(body, func(node ast.Node) bool {
		if found || node == nil {
			return false
		}
		if _, ok := node.(*ast.FuncLit); ok {
			return false
		}
		switch candidate := node.(type) {
		case *ast.AssignStmt:
			if candidate.Pos() > ident.Pos() {
				return false
			}
			for i, lhs := range candidate.Lhs {
				lhsIdent, ok := unparenExpr(lhs).(*ast.Ident)
				if !ok || !sameIdentifierObject(ctx, lhsIdent, ident, targetObject) {
					continue
				}
				if len(candidate.Rhs) == 1 {
					found = i == 0 && mapIndexAssertionActualIsMap(ctx, candidate.Rhs[0])
				} else if i < len(candidate.Rhs) {
					found = mapIndexAssertionActualIsMap(ctx, candidate.Rhs[i])
				}
				if found {
					return false
				}
			}
		case *ast.ValueSpec:
			if candidate.Pos() > ident.Pos() {
				return false
			}
			for i, name := range candidate.Names {
				if name == nil || !sameIdentifierObject(ctx, name, ident, targetObject) || i >= len(candidate.Values) {
					continue
				}
				found = mapIndexAssertionActualIsMap(ctx, candidate.Values[i])
				if found {
					return false
				}
			}
		}
		return true
	})
	return found
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

func assertionUsesResponseHeaderMatcherOnRequest(ctx *analysisContext, assertion gomegaAssertion) bool {
	return isMatcherNamed(assertion.matcher, "HaveHTTPHeaderWithValue") &&
		isNamedTypeFromPackage(ctx.pass.TypesInfo.TypeOf(assertion.actual), "net/http", "Request")
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

func isEqualZeroMatcherCall(call *ast.CallExpr) bool {
	if !isMatcherNamed(call, "Equal") || len(call.Args) != 1 {
		return false
	}
	lit, ok := unparenExpr(call.Args[0]).(*ast.BasicLit)
	return ok && lit.Kind == token.INT && lit.Value == "0"
}

func isEqualBooleanLiteralMatcherCall(call *ast.CallExpr) bool {
	if !isMatcherNamed(call, "Equal") || len(call.Args) != 1 {
		return false
	}
	ident, ok := unparenExpr(call.Args[0]).(*ast.Ident)
	return ok && (ident.Name == "true" || ident.Name == "false")
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

func assertionUsesPositionalCompositeAssertion(assertion gomegaAssertion) bool {
	if !isValueComparisonMatcher(assertion.matcher) || len(assertion.matcher.Args) != 1 {
		return false
	}
	actual, ok := unparenExpr(assertion.actual).(*ast.CompositeLit)
	if !ok || len(actual.Elts) < 2 || !compositeLiteralIsPositional(actual) {
		return false
	}
	expected, ok := unparenExpr(assertion.matcher.Args[0]).(*ast.CompositeLit)
	if !ok || len(expected.Elts) < 2 || !compositeLiteralIsPositional(expected) {
		return false
	}
	return compositeLiteralContainsMultipleFieldSelectors(actual)
}

func compositeLiteralContainsMultipleFieldSelectors(lit *ast.CompositeLit) bool {
	fieldSelectors := 0
	for _, elt := range lit.Elts {
		if _, ok := unparenExpr(elt).(*ast.SelectorExpr); ok {
			fieldSelectors++
		}
	}
	return fieldSelectors >= 2
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
		if !ok || !isGinkgoSubjectBodyNodeName(callName(call)) {
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
	if isMatcherNamed(matcher, "BeTrue", "BeFalse", "BeTrueBecause", "BeFalseBecause") {
		return true
	}
	if !isMatcherNamed(matcher, "Equal") || len(matcher.Args) != 1 {
		return false
	}
	ident, ok := unparenExpr(matcher.Args[0]).(*ast.Ident)
	return ok && (ident.Name == "true" || ident.Name == "false")
}

func exprIsBooleanMatcher(expr ast.Expr) bool {
	call, ok := unparenExpr(expr).(*ast.CallExpr)
	return ok && isBooleanMatcher(call)
}

func matcherCallIsNestedBooleanMatcherValue(ctx *analysisContext, call *ast.CallExpr) bool {
	if !isMatcherNamed(call, "BeTrue", "BeFalse", "BeTrueBecause", "BeFalseBecause") {
		return false
	}
	for current := ctx.parent(call); current != nil; current = ctx.parent(current) {
		parentCall, ok := current.(*ast.CallExpr)
		if !ok {
			continue
		}
		if assertion, ok := gomegaAssertionFromCall(ctx, parentCall); ok {
			return assertion.matcher != call
		}
		if assertion, ok := gomegaAsyncAssertionFromCall(parentCall); ok {
			return assertion.matcher != call
		}
		if isKnownGomegaMatcherFactory(parentCall) || typeIsGomegaMatcher(ctx.pass.TypesInfo.TypeOf(parentCall)) {
			return true
		}
	}
	return false
}

func matcherCallUsesMatcherValueAsExpected(ctx *analysisContext, call *ast.CallExpr) bool {
	if !isValueComparisonMatcher(call) {
		return false
	}
	for _, arg := range call.Args {
		if exprTreeContainsGomegaMatcherValue(ctx, arg) {
			return true
		}
	}
	return false
}

func matcherUsesPositionalTransform(ctx *analysisContext, call *ast.CallExpr) bool {
	if !isMatcherNamed(call, "WithTransform") || len(call.Args) < 2 {
		return false
	}
	if !matcherIsEqualCompositeLiteral(call.Args[1]) {
		return false
	}
	fn, ok := unparenExpr(call.Args[0]).(*ast.FuncLit)
	if !ok {
		return false
	}
	return funcLitReturnsFieldTuple(ctx, fn)
}

func matcherIsEqualCompositeLiteral(expr ast.Expr) bool {
	call, ok := unparenExpr(expr).(*ast.CallExpr)
	if !ok || !isMatcherNamed(call, "Equal") || len(call.Args) != 1 {
		return false
	}
	lit, ok := unparenExpr(call.Args[0]).(*ast.CompositeLit)
	if !ok || len(lit.Elts) < 2 {
		return false
	}
	return compositeLiteralIsPositional(lit)
}

func funcLitReturnsFieldTuple(ctx *analysisContext, fn *ast.FuncLit) bool {
	paramObjects := funcLitParamObjects(ctx, fn)
	if len(paramObjects) == 0 {
		return false
	}
	found := false
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		if found || node == nil {
			return false
		}
		if nested, ok := node.(*ast.FuncLit); ok && nested != fn {
			return false
		}
		ret, ok := node.(*ast.ReturnStmt)
		if !ok || len(ret.Results) != 1 {
			return true
		}
		lit, ok := unparenExpr(ret.Results[0]).(*ast.CompositeLit)
		if !ok || len(lit.Elts) < 2 || !compositeLiteralIsPositional(lit) {
			return true
		}
		for _, elt := range lit.Elts {
			if !exprIsFieldSelectorFromParam(ctx, elt, paramObjects) {
				return true
			}
		}
		found = true
		return false
	})
	return found
}

func funcLitParamObjects(ctx *analysisContext, fn *ast.FuncLit) map[types.Object]struct{} {
	objects := map[types.Object]struct{}{}
	if fn.Type.Params == nil {
		return objects
	}
	for _, field := range fn.Type.Params.List {
		for _, name := range field.Names {
			if name == nil {
				continue
			}
			if obj := ctx.pass.TypesInfo.ObjectOf(name); obj != nil {
				objects[obj] = struct{}{}
			}
		}
	}
	return objects
}

func compositeLiteralIsPositional(lit *ast.CompositeLit) bool {
	switch lit.Type.(type) {
	case *ast.ArrayType, *ast.StructType, *ast.Ident, *ast.SelectorExpr:
	default:
		return false
	}
	for _, elt := range lit.Elts {
		if _, ok := unparenExpr(elt).(*ast.KeyValueExpr); ok {
			return false
		}
	}
	return true
}

func exprIsFieldSelectorFromParam(ctx *analysisContext, expr ast.Expr, paramObjects map[types.Object]struct{}) bool {
	ident := baseIdentForSelector(expr)
	if ident == nil {
		return false
	}
	obj := ctx.pass.TypesInfo.ObjectOf(ident)
	if obj == nil {
		return false
	}
	_, ok := paramObjects[obj]
	return ok
}

func baseIdentForSelector(expr ast.Expr) *ast.Ident {
	selector, ok := unparenExpr(expr).(*ast.SelectorExpr)
	if !ok {
		return nil
	}
	for {
		switch x := unparenExpr(selector.X).(type) {
		case *ast.Ident:
			return x
		case *ast.SelectorExpr:
			selector = x
		default:
			return nil
		}
	}
}

func isValueComparisonMatcher(call *ast.CallExpr) bool {
	return isMatcherNamed(call, "Equal", "BeEquivalentTo", "BeComparableTo", "BeIdenticalTo")
}

func exprTreeContainsGomegaMatcherValue(ctx *analysisContext, expr ast.Expr) bool {
	found := false
	ast.Inspect(expr, func(node ast.Node) bool {
		if found || node == nil {
			return false
		}
		current, ok := node.(ast.Expr)
		if !ok {
			return true
		}
		if exprIsGomegaMatcherValue(ctx, current) {
			found = true
			return false
		}
		return true
	})
	return found
}

func exprIsGomegaMatcherValue(ctx *analysisContext, expr ast.Expr) bool {
	expr = unparenExpr(expr)
	if call, ok := expr.(*ast.CallExpr); ok && isKnownGomegaMatcherFactory(call) {
		return true
	}
	if ident, ok := expr.(*ast.Ident); ok {
		if obj := ctx.pass.TypesInfo.ObjectOf(ident); obj != nil && typeIsGomegaMatcher(obj.Type()) {
			return true
		}
		if identDeclaredAsGomegaMatcherParameter(ctx, ident) {
			return true
		}
		if identInitializedWithGomegaMatcher(ctx, ident) {
			return true
		}
	}
	return typeIsGomegaMatcher(ctx.pass.TypesInfo.TypeOf(expr))
}

func identDeclaredAsGomegaMatcherParameter(ctx *analysisContext, ident *ast.Ident) bool {
	targetObject := ctx.pass.TypesInfo.ObjectOf(ident)
	for current := ast.Node(ident); current != nil; current = ctx.parent(current) {
		switch fn := current.(type) {
		case *ast.FuncDecl:
			return fieldListDeclaresGomegaMatcherName(ctx, fn.Type.Params, ident, targetObject)
		case *ast.FuncLit:
			return fieldListDeclaresGomegaMatcherName(ctx, fn.Type.Params, ident, targetObject)
		}
	}
	return false
}

func fieldListDeclaresGomegaMatcherName(ctx *analysisContext, fields *ast.FieldList, ident *ast.Ident, targetObject types.Object) bool {
	if fields == nil {
		return false
	}
	for _, field := range fields.List {
		if !exprNamesGomegaMatcherType(ctx, field.Type) {
			continue
		}
		for _, name := range field.Names {
			if name != nil && sameIdentifierObject(ctx, name, ident, targetObject) {
				return true
			}
		}
	}
	return false
}

func exprNamesGomegaMatcherType(ctx *analysisContext, expr ast.Expr) bool {
	if exprName(expr) == "GomegaMatcher" {
		return true
	}
	return typeIsGomegaMatcher(ctx.pass.TypesInfo.TypeOf(expr))
}

func identInitializedWithGomegaMatcher(ctx *analysisContext, ident *ast.Ident) bool {
	body := enclosingFunctionBody(ctx, ident)
	if body == nil {
		return false
	}
	targetObject := ctx.pass.TypesInfo.ObjectOf(ident)
	found := false
	ast.Inspect(body, func(node ast.Node) bool {
		if found || node == nil {
			return false
		}
		if _, ok := node.(*ast.FuncLit); ok {
			return false
		}
		switch candidate := node.(type) {
		case *ast.AssignStmt:
			if candidate.Pos() > ident.Pos() {
				return false
			}
			for i, lhs := range candidate.Lhs {
				lhsIdent, ok := unparenExpr(lhs).(*ast.Ident)
				if !ok || !sameIdentifierObject(ctx, lhsIdent, ident, targetObject) || i >= len(candidate.Rhs) {
					continue
				}
				found = exprTreeContainsGomegaMatcherProducer(ctx, candidate.Rhs[i])
				if found {
					return false
				}
			}
		case *ast.ValueSpec:
			if candidate.Pos() > ident.Pos() {
				return false
			}
			for i, name := range candidate.Names {
				if name == nil || !sameIdentifierObject(ctx, name, ident, targetObject) || i >= len(candidate.Values) {
					continue
				}
				found = exprTreeContainsGomegaMatcherProducer(ctx, candidate.Values[i])
				if found {
					return false
				}
			}
		}
		return true
	})
	return found
}

func enclosingFunctionBody(ctx *analysisContext, node ast.Node) *ast.BlockStmt {
	for current := node; current != nil; current = ctx.parent(current) {
		switch fn := current.(type) {
		case *ast.FuncDecl:
			return fn.Body
		case *ast.FuncLit:
			return fn.Body
		}
	}
	return nil
}

func sameIdentifierObject(ctx *analysisContext, candidate *ast.Ident, target *ast.Ident, targetObject types.Object) bool {
	candidateObject := ctx.pass.TypesInfo.ObjectOf(candidate)
	if candidateObject != nil && targetObject != nil {
		return candidateObject == targetObject
	}
	return candidate.Name == target.Name
}

func exprTreeContainsGomegaMatcherProducer(ctx *analysisContext, expr ast.Expr) bool {
	found := false
	ast.Inspect(expr, func(node ast.Node) bool {
		if found || node == nil {
			return false
		}
		current, ok := node.(ast.Expr)
		if !ok {
			return true
		}
		switch candidate := unparenExpr(current).(type) {
		case *ast.CallExpr:
			if isKnownGomegaMatcherFactory(candidate) || typeIsGomegaMatcher(ctx.pass.TypesInfo.TypeOf(candidate)) {
				found = true
				return false
			}
		case *ast.TypeAssertExpr:
			if exprName(candidate.Type) == "GomegaMatcher" || typeIsGomegaMatcher(ctx.pass.TypesInfo.TypeOf(candidate)) {
				found = true
				return false
			}
		default:
			if typeIsGomegaMatcher(ctx.pass.TypesInfo.TypeOf(candidate)) {
				found = true
				return false
			}
		}
		return true
	})
	return found
}

func isKnownGomegaMatcherFactory(call *ast.CallExpr) bool {
	switch callName(call) {
	case "And",
		"BeAssignableToTypeOf",
		"BeClosed",
		"BeComparableTo",
		"BeElementOf",
		"BeEmpty",
		"BeEquivalentTo",
		"BeFalse",
		"BeIdenticalTo",
		"BeNumerically",
		"BeSent",
		"BeTemporally",
		"BeTrue",
		"BeZero",
		"ContainElement",
		"ContainElements",
		"ContainSubstring",
		"ConsistOf",
		"Equal",
		"HaveCap",
		"HaveEach",
		"HaveExactElements",
		"HaveExistingField",
		"HaveField",
		"HaveHTTPBody",
		"HaveHTTPHeaderWithValue",
		"HaveHTTPStatus",
		"HaveKey",
		"HaveKeyWithValue",
		"HaveLen",
		"HaveOccurred",
		"HavePrefix",
		"HaveSuffix",
		"HaveValue",
		"MatchAllElements",
		"MatchAllFields",
		"MatchAllKeys",
		"MatchError",
		"MatchFields",
		"MatchJSON",
		"MatchRegexp",
		"MatchXML",
		"MatchYAML",
		"Not",
		"Or",
		"Panic",
		"Receive",
		"Satisfy",
		"SatisfyAll",
		"SatisfyAny",
		"Succeed",
		"WithTransform":
		return true
	default:
		return false
	}
}

func typeIsGomegaMatcher(typ types.Type) bool {
	named := namedType(typ)
	if named == nil || named.Obj().Name() != "GomegaMatcher" {
		return typeStringSuggestsGomegaMatcher(typ)
	}
	pkg := named.Obj().Pkg()
	if pkg == nil {
		return true
	}
	return pkg.Path() == "github.com/onsi/gomega/types" ||
		strings.Contains(pkg.Path(), "/gomega")
}

func typeStringSuggestsGomegaMatcher(typ types.Type) bool {
	if typ == nil {
		return false
	}
	typeName := types.TypeString(types.Unalias(typ), nil)
	return typeName == "GomegaMatcher" || strings.HasSuffix(typeName, ".GomegaMatcher")
}

func assertionUsesRawStringMatchError(ctx *analysisContext, assertion gomegaAssertion) bool {
	return matcherTreeContainsRawStringMatchError(ctx, assertion.matcher)
}

func rawStringMatchErrorMatcherCall(ctx *analysisContext, call *ast.CallExpr) bool {
	if !isMatcherNamed(call, "MatchError") || len(call.Args) == 0 {
		return false
	}
	return matchErrorArgumentUsesRawString(ctx, call.Args[0])
}

func callIsInsideGomegaAssertion(ctx *analysisContext, call *ast.CallExpr) bool {
	for parent := ctx.parent(call); parent != nil; parent = ctx.parent(parent) {
		parentCall, ok := parent.(*ast.CallExpr)
		if !ok {
			continue
		}
		if _, ok := gomegaAssertionFromCall(ctx, parentCall); ok {
			return true
		}
		if _, ok := gomegaAsyncAssertionFromCall(parentCall); ok {
			return true
		}
	}
	return false
}

func matcherTreeContainsRawStringMatchError(ctx *analysisContext, expr ast.Expr) bool {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return false
	}
	if rawStringMatchErrorMatcherCall(ctx, call) {
		return true
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
	if callConstructsRawErrorMessage(ctx, call) {
		return true
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

func callConstructsRawErrorMessage(ctx *analysisContext, call *ast.CallExpr) bool {
	fn := calledFunctionObject(ctx, call)
	if fn == nil || fn.Pkg() == nil {
		return false
	}
	switch fn.Pkg().Path() {
	case "errors":
		return fn.Name() == "New" && callHasStringArg(ctx, call)
	case "fmt":
		return fn.Name() == "Errorf" && callHasStringArg(ctx, call)
	default:
		return false
	}
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

func assertionUsesStructuredProtocolStatusMatcher(ctx *analysisContext, assertion gomegaAssertion) bool {
	if assertionUsesDirectProtocolStatusBool(ctx, assertion) {
		return true
	}
	if !exprSuggestsStructuredProtocolValue(ctx, assertion.actual) {
		return false
	}
	return matcherTreeContainsStructuredProtocolStatusBool(assertion.matcher)
}

func assertionUsesDirectProtocolStatusBool(ctx *analysisContext, assertion gomegaAssertion) bool {
	selector, ok := unparenExpr(assertion.actual).(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != "IsError" || !isBooleanMatcher(assertion.matcher) {
		return false
	}
	return exprSuggestsStructuredProtocolValue(ctx, selector.X)
}

func matcherTreeContainsStructuredProtocolStatusBool(expr ast.Expr) bool {
	switch node := unparenExpr(expr).(type) {
	case *ast.CallExpr:
		if isMatcherNamed(node, "HaveField") && len(node.Args) > 1 {
			if fieldName, ok := stringLiteralValue(node.Args[0]); ok && fieldName == "IsError" && exprIsBooleanMatcher(node.Args[1]) {
				return true
			}
		}
		for _, arg := range node.Args {
			if matcherTreeContainsStructuredProtocolStatusBool(arg) {
				return true
			}
		}
	case *ast.CompositeLit:
		for _, elt := range node.Elts {
			keyValue, ok := elt.(*ast.KeyValueExpr)
			if !ok {
				if matcherTreeContainsStructuredProtocolStatusBool(elt) {
					return true
				}
				continue
			}
			if fieldName, ok := stringLiteralValue(keyValue.Key); ok && fieldName == "IsError" && exprIsBooleanMatcher(keyValue.Value) {
				return true
			}
			if matcherTreeContainsStructuredProtocolStatusBool(keyValue.Value) {
				return true
			}
		}
	}
	return false
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
		ctx.report(ruleGomegaAsyncCallbackExpect, call, gomegaAsyncCallbackExpectDiagnostic())
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

func gomegaRenderedOutputMatcherFactoryName(name string) bool {
	canonical := canonicalName(name)
	if !strings.HasPrefix(canonical, "have") &&
		!strings.HasPrefix(canonical, "contain") &&
		!strings.HasPrefix(canonical, "match") &&
		!strings.HasPrefix(canonical, "be") {
		return false
	}
	return strings.Contains(canonical, "error") ||
		strings.Contains(canonical, "fatal") ||
		strings.Contains(canonical, "message") ||
		strings.Contains(canonical, "output") ||
		strings.Contains(canonical, "stderr") ||
		strings.Contains(canonical, "stdout")
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
