package testhygiene

import (
	"go/ast"
	"strings"
)

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
	if isExportedCodeIdentifierFragment(field) {
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

func isExportedCodeIdentifierFragment(field string) bool {
	field = trimGinkgoNamePunctuation(field)
	if !isGoIdentifierLike(field) || !isExportedIdentifierFragment(field) || !hasCamelCaseWordBoundary(field) {
		return false
	}
	for _, suffix := range []string{
		"UseCase",
		"Routes",
		"Router",
		"Middleware",
		"Handler",
		"Service",
		"Store",
		"Repository",
		"DTO",
		"Config",
		"Command",
	} {
		if strings.HasSuffix(field, suffix) && len(field) > len(suffix) {
			return true
		}
	}
	return false
}

func hasCamelCaseWordBoundary(field string) bool {
	previousLower := false
	for _, r := range field {
		if r >= 'A' && r <= 'Z' && previousLower {
			return true
		}
		previousLower = r >= 'a' && r <= 'z'
	}
	return false
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
