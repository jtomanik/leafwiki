package testhygiene

import (
	"go/ast"
	"strings"
)

func isGomegaAssertionMethod(name string) bool {
	switch name {
	case "To", "NotTo", "ToNot", "Should", "ShouldNot", "Error":
		return true
	default:
		return false
	}
}

func exprSuggestsTestRenderedProseContract(expr ast.Expr) bool {
	found := false
	ast.Inspect(expr, func(node ast.Node) bool {
		if found || node == nil {
			return false
		}
		switch n := node.(type) {
		case *ast.Ident:
			found = nameSuggestsTestRenderedProseSubject(n.Name)
		case *ast.SelectorExpr:
			found = nameSuggestsTestRenderedProseSubject(n.Sel.Name)
		case *ast.CallExpr:
			found = nameSuggestsTestRenderedProseSubject(callName(n))
		}
		return !found
	})
	return found
}

func nameSuggestsTestRenderedProseSubject(name string) bool {
	canonical := canonicalName(name)
	if strings.Contains(canonical, "diagnostic") {
		return false
	}
	if identifierHasWord(name, "err") {
		return true
	}
	return canonical == "err" ||
		canonical == "error" ||
		canonical == "logs" ||
		canonical == "message" ||
		canonical == "output" ||
		identifierHasWord(name, "log") ||
		identifierHasWord(name, "logs") ||
		identifierHasWord(name, "panic") ||
		identifierHasWord(name, "fatal") ||
		strings.Contains(canonical, "error") ||
		strings.Contains(canonical, "message") ||
		strings.Contains(canonical, "output") ||
		strings.Contains(canonical, "stderr") ||
		strings.Contains(canonical, "stdout")
}

func nameSuggestsTestContract(name string) bool {
	canonical := canonicalName(name)
	return strings.Contains(canonical, "structured") ||
		strings.Contains(canonical, "localized") ||
		strings.Contains(canonical, "error") ||
		strings.Contains(canonical, "message") ||
		strings.Contains(canonical, "code") ||
		strings.Contains(canonical, "validation") ||
		strings.Contains(canonical, "issue") ||
		strings.Contains(canonical, "tool")
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
	case strings.Contains(canonicalParam, "toolid") || strings.Contains(canonicalParam, "toolname"):
		return "ToolID", true
	case strings.Contains(canonicalParam, "code"):
		switch {
		case strings.Contains(canonicalFunc, "field"):
			return "FieldErrorCode", true
		case strings.Contains(canonicalFunc, "issue") || strings.Contains(canonicalFunc, "validationissue"):
			return "IssueCode", true
		default:
			return "ErrorCode", true
		}
	default:
		return "", false
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
