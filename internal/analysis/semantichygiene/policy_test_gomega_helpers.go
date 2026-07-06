package semantichygiene

import (
	"go/ast"
	"go/token"
	"strconv"
	"strings"
)

func isGomegaAnnotationCall(name string) bool {
	switch name {
	case "To", "NotTo", "ToNot", "Should", "ShouldNot", "Error":
		return true
	default:
		return false
	}
}

func isTestAssertionMatcherCall(name string) bool {
	switch name {
	case "Equal", "ContainSubstring", "HavePrefix", "HaveSuffix", "HaveKeyWithValue", "HaveField", "MatchError", "PanicWith":
		return true
	default:
		return false
	}
}

func isTestAssertionMatcherContractContext(ctx *analysisContext, matcher *ast.CallExpr) bool {
	if !isTestAssertionMatcherCall(callName(matcher)) {
		return false
	}
	if matcherCallHasStructuredProtocolKey(matcher) {
		return true
	}
	subject, ok := gomegaExpectationSubjectForMatcher(ctx, matcher)
	if !ok {
		return false
	}
	return exprSuggestsTestContract(subject)
}

func isTestAssertionMatcherLocalizedProseContext(ctx *analysisContext, matcher *ast.CallExpr) bool {
	if !isTestAssertionMatcherCall(callName(matcher)) {
		return false
	}
	if matcherCallHasRenderedProseProtocolKey(matcher) {
		return true
	}
	subject, ok := gomegaExpectationSubjectForMatcher(ctx, matcher)
	if !ok {
		return false
	}
	return exprSuggestsTestRenderedProseContract(subject)
}

func gomegaExpectationSubjectForMatcher(ctx *analysisContext, matcher *ast.CallExpr) (ast.Expr, bool) {
	for current := ctx.parent(matcher); current != nil; current = ctx.parent(current) {
		call, ok := current.(*ast.CallExpr)
		if !ok {
			if _, ok := current.(*ast.FuncDecl); ok {
				return nil, false
			}
			continue
		}
		if isGomegaAssertionMethod(callName(call)) {
			return gomegaAssertionSubject(call)
		}
	}
	return nil, false
}

func gomegaAssertionSubject(call *ast.CallExpr) (ast.Expr, bool) {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return nil, false
	}
	expectCall, ok := selector.X.(*ast.CallExpr)
	if !ok || !isGomegaExpectCall(callName(expectCall)) || len(expectCall.Args) == 0 {
		return nil, false
	}
	return expectCall.Args[0], true
}

func matcherCallHasStructuredProtocolKey(call *ast.CallExpr) bool {
	key, ok := matcherCallProtocolKey(call)
	return ok && structuredProtocolKeyName(key)
}

func matcherCallHasRenderedProseProtocolKey(call *ast.CallExpr) bool {
	key, ok := matcherCallProtocolKey(call)
	return ok && canonicalMatchesAny(canonicalName(key), renderedProseProtocolKeys)
}

func matcherCallProtocolKey(call *ast.CallExpr) (string, bool) {
	if !isMatcherNamed(call, "HaveKeyWithValue") || len(call.Args) == 0 {
		return "", false
	}
	return stringArgValue(call.Args[0])
}

func stringArgValue(expr ast.Expr) (string, bool) {
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

func isGomegaAssertionMethod(name string) bool {
	switch name {
	case "To", "NotTo", "ToNot", "Should", "ShouldNot", "Error":
		return true
	default:
		return false
	}
}

func isGomegaExpectCall(name string) bool {
	switch name {
	case "Expect", "ExpectWithOffset", "Ω":
		return true
	default:
		return false
	}
}

func exprSuggestsTestContract(expr ast.Expr) bool {
	return exprHasNameMatching(expr, nameSuggestsTestSubjectContract)
}

func exprSuggestsTestRenderedProseContract(expr ast.Expr) bool {
	return exprHasNameMatching(expr, nameSuggestsTestRenderedProseSubject)
}

func exprHasNameMatching(expr ast.Expr, match func(string) bool) bool {
	found := false
	ast.Inspect(expr, func(node ast.Node) bool {
		if found || node == nil {
			return false
		}
		switch n := node.(type) {
		case *ast.Ident:
			found = match(n.Name)
		case *ast.SelectorExpr:
			found = match(n.Sel.Name)
		case *ast.CallExpr:
			found = match(callName(n))
		}
		return !found
	})
	return found
}

func nameSuggestsTestSubjectContract(name string) bool {
	canonical := canonicalName(name)
	if strings.Contains(canonical, "diagnostic") {
		return false
	}
	return canonical == "body" || containsAnyCanonical(canonical, testSubjectContractFragments)
}

func nameSuggestsTestRenderedProseSubject(name string) bool {
	canonical := canonicalName(name)
	if strings.Contains(canonical, "diagnostic") {
		return false
	}
	return canonicalMatchesAny(canonical, testRenderedProseExactNames) ||
		identifierHasAnyWord(name, testRenderedProseWords) ||
		containsAnyCanonical(canonical, testRenderedProseFragments)
}

func isTestContractAssertionCall(ctx *analysisContext, call *ast.CallExpr) bool {
	name := callName(call)
	if !strings.HasPrefix(name, "assert") && !strings.HasPrefix(name, "expect") {
		return false
	}
	return nameSuggestsTestContract(name)
}

func isTestSemanticAssertionHelper(name string) bool {
	if !strings.HasPrefix(name, "assert") && !strings.HasPrefix(name, "expect") {
		return false
	}
	return nameSuggestsTestContract(name)
}

func nameSuggestsTestContract(name string) bool {
	return containsAnyCanonical(canonicalName(name), testContractFragments)
}

func semanticTypeForTestHelperParamName(paramName string, funcName string) (string, bool) {
	if nameIsFilename(paramName) && !nameSuggestsAssetNameContext(funcName) {
		return "", false
	}
	if typ, ok := semanticTypeForParamName(paramName, funcName); ok {
		return typ, true
	}
	canonicalParam := canonicalName(paramName)
	canonicalFunc := canonicalName(funcName)
	switch {
	case strings.Contains(canonicalParam, "messageid"):
		return "MessageID", true
	case containsAnyCanonical(canonicalParam, toolParamFragments):
		return "ToolID", true
	case strings.Contains(canonicalParam, "code"):
		return semanticCodeTypeForTestHelperFunction(canonicalFunc), true
	default:
		return "", false
	}
}

func semanticCodeTypeForTestHelperFunction(canonicalFunc string) string {
	switch {
	case strings.Contains(canonicalFunc, "field"):
		return "FieldErrorCode"
	case containsAnyCanonical(canonicalFunc, issueCodeFunctionFragments):
		return "IssueCode"
	default:
		return "ErrorCode"
	}
}

func nameSuggestsAssetNameContext(name string) bool {
	return strings.Contains(canonicalName(name), "asset")
}

func nameIsFilename(name string) bool {
	switch canonicalName(name) {
	case "filename", "filenames":
		return true
	default:
		return false
	}
}

func testHelperMessageParamName(paramName string, funcName string) bool {
	canonicalParam := canonicalName(paramName)
	canonicalFunc := canonicalName(funcName)
	if canonicalParam == "message" {
		return containsAnyCanonical(canonicalFunc, testHelperMessageFunctionFragments)
	}
	if !isRenderedOutputParamName(canonicalParam) {
		return false
	}
	return containsAnyCanonical(canonicalFunc, testHelperRenderedOutputFunctionFragments)
}

func isRenderedOutputParamName(canonicalParam string) bool {
	switch canonicalParam {
	case "context", "fragment", "prefix", "suffix", "text", "output", "stderr", "stdout", "cause", "reason":
		return true
	default:
		return false
	}
}

func testHelperFieldParamName(paramName string, funcName string) bool {
	if canonicalName(paramName) != "field" {
		return false
	}
	return containsAnyCanonical(canonicalName(funcName), testHelperFieldFunctionFragments)
}

func testTableParamSuggestsContract(paramName string) bool {
	if _, ok := semanticTypeForTestHelperParamName(paramName, ""); ok {
		return true
	}
	canonical := canonicalName(paramName)
	return strings.Contains(canonical, "error") ||
		strings.Contains(canonical, "message")
}

func testTableParamSuggestsRenderedProseContract(paramName string) bool {
	canonical := canonicalName(paramName)
	if strings.Contains(canonical, "diagnostic") {
		return false
	}
	return containsAnyCanonical(canonical, testTableRenderedProseFragments)
}

func canonicalMatchesAny(canonical string, candidates []string) bool {
	for _, candidate := range candidates {
		if canonical == candidate {
			return true
		}
	}
	return false
}

func identifierHasAnyWord(name string, words []string) bool {
	for _, word := range words {
		if identifierHasWord(name, word) {
			return true
		}
	}
	return false
}

var renderedProseProtocolKeys = []string{
	"error",
	"message",
}

var testSubjectContractFragments = []string{
	"code",
	"error",
	"issue",
	"message",
	"messageid",
	"toolid",
	"validation",
}

var testRenderedProseExactNames = []string{
	"err",
	"error",
	"logs",
	"message",
	"output",
}

var testRenderedProseWords = []string{
	"err",
	"fatal",
	"log",
	"logs",
	"panic",
}

var testRenderedProseFragments = []string{
	"error",
	"message",
	"output",
	"stderr",
	"stdout",
}

var testContractFragments = []string{
	"code",
	"error",
	"issue",
	"localized",
	"message",
	"structured",
	"tool",
	"validation",
}

var toolParamFragments = []string{
	"toolid",
	"toolname",
}

var issueCodeFunctionFragments = []string{
	"issue",
	"validationissue",
}

var testHelperMessageFunctionFragments = []string{
	"error",
	"localized",
	"structured",
}

var testHelperRenderedOutputFunctionFragments = []string{
	"error",
	"fatal",
	"localized",
	"message",
	"output",
	"stderr",
	"stdout",
	"structured",
}

var testHelperFieldFunctionFragments = []string{
	"error",
	"field",
	"validation",
}

var testTableRenderedProseFragments = []string{
	"error",
	"message",
	"stderr",
	"stdout",
}
