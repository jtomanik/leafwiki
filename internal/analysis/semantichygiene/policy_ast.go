package semantichygiene

import (
	"go/ast"
	"go/types"
	"strings"
	"unicode"
)

func unparenExpr(expr ast.Expr) ast.Expr {
	for {
		paren, ok := expr.(*ast.ParenExpr)
		if !ok {
			return expr
		}
		expr = paren.X
	}
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

func structuredProtocolKeyName(name string) bool {
	switch canonicalName(name) {
	case "code", "error", "errorcode", "field", "fieldcode", "issuecode", "message", "messageid", "toolmessageid":
		return true
	default:
		return false
	}
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

func gomegaMatcherFactoryName(name string) bool {
	canonical := canonicalName(name)
	return strings.HasPrefix(canonical, "have") ||
		strings.HasPrefix(canonical, "contain") ||
		strings.HasPrefix(canonical, "match") ||
		strings.HasPrefix(canonical, "be")
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
