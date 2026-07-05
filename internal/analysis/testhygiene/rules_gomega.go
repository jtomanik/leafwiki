package testhygiene

import (
	"go/ast"
	"go/types"
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
	if assertionUsesRawStatusCode(assertion) {
		ctx.report(ruleGomegaRawStatusCode, assertion.actual, gomegaRawStatusCodeDiagnostic())
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
