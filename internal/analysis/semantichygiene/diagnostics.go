package semantichygiene

import "fmt"

func stringCallDiagnostic(typeName string, callee string) string {
	return fmt.Sprintf("semantic value %s converted to string before internal call %s; make the callee accept %s", typeName, callee, typeName)
}

func stringComparisonDiagnostic(typeName string) string {
	return fmt.Sprintf("semantic value %s converted to string for comparison; compare %s values directly or parse the primitive first", typeName, typeName)
}

func stringFieldDiagnostic(typeName string, field string) string {
	return fmt.Sprintf("semantic value %s converted to string for semantic field %s; keep the field typed or convert only at a boundary", typeName, field)
}

func stringLocalDiagnostic(typeName string, name string) string {
	return fmt.Sprintf("semantic value %s converted to string into local %s; keep %s typed until an explicit boundary", typeName, name, typeName)
}

func stringReturnDiagnostic(typeName string, funcName string) string {
	return fmt.Sprintf("semantic value %s returned as string from internal function %s; return %s or serialize only at a boundary", typeName, funcName, typeName)
}

func directCastDiagnostic(typeName string) string {
	return fmt.Sprintf("direct cast to semantic type %s outside parser or boundary; use a parser or typed input", typeName)
}

func validatorReturnDiagnostic(funcName string, semanticName string, typeName string) string {
	return fmt.Sprintf("validator %s returns primitive string for semantic %s; return %s or rename the function if it does not validate a semantic value", funcName, semanticName, typeName)
}

func fieldDiagnostic(fieldName string, typeName string, semanticType string) string {
	return fmt.Sprintf("semantic-looking field %s uses string in domain/service type %s; use %s or mark the type as a DTO boundary", fieldName, typeName, semanticType)
}

func parameterDiagnostic(paramName string, funcName string, semanticType string) string {
	return fmt.Sprintf("semantic-looking parameter %s uses string in internal function %s; use %s or accept a DTO boundary value", paramName, funcName, semanticType)
}

func primitiveParameterDiagnostic(paramName string, funcName string, primitiveType string) string {
	return fmt.Sprintf("semantic-looking parameter %s uses %s in internal function %s; introduce a typed value after parsing", paramName, primitiveType, funcName)
}

func primitiveFieldDiagnostic(fieldName string, typeName string, primitiveType string) string {
	return fmt.Sprintf("semantic-looking field %s uses %s in domain/service type %s; introduce a typed value after parsing", fieldName, primitiveType, typeName)
}

func uncheckedConstructorDiagnostic(funcName string, typeName string) string {
	return fmt.Sprintf("unchecked constructor %s creates %s from primitive in internal code; use a parser or narrow derived-value helper", funcName, typeName)
}

func messageFieldDiagnostic(fieldName string, typeName string) string {
	return fmt.Sprintf("message-bearing struct %s exposes %s string without MessageID; add a catalog-backed MessageID", typeName, fieldName)
}

func warningStringsFieldDiagnostic(fieldName string, typeName string) string {
	return fmt.Sprintf("message-bearing struct %s exposes %s strings without MessageID; use catalog-backed warning IDs", typeName, fieldName)
}

func messageFieldValueDiagnostic(fieldName string, typeName string) string {
	return fmt.Sprintf("message field %s is populated without a MessageID in %s; render through a catalog-backed message", fieldName, typeName)
}

func messageFieldPassthroughDiagnostic(fieldName string, typeName string) string {
	return fmt.Sprintf("message field %s in %s passes through free-form text despite MessageID; render through the catalog instead", fieldName, typeName)
}

func warningFieldValueDiagnostic(fieldName string, typeName string) string {
	return fmt.Sprintf("warning field %s is populated without a MessageID in %s; render through catalog-backed warning IDs", fieldName, typeName)
}

func responseStatusForwardDiagnostic(fieldName string) string {
	return fmt.Sprintf("HTTP/private response field %s forwards message-bearing status text without stable message metadata; render through catalog-backed status details", fieldName)
}

func localizedProseSinkSignatureDiagnostic(funcName string) string {
	return fmt.Sprintf("localized prose sink %s accepts free-form message string; accept MessageID/catalog args instead", funcName)
}

func localizedErrorConstructorPassthroughDiagnostic(funcName string) string {
	return fmt.Sprintf("localized error constructor %s receives free-form message fallback; render through the catalog instead", funcName)
}

func stableLiteralDiagnostic(value string) string {
	return fmt.Sprintf("raw stable contract literal %q used in production code; use the typed constant or definition", value)
}

func rawLocalizedProseDiagnostic(value string) string {
	return fmt.Sprintf("raw localized prose %q used in Go contract code; use a catalog-backed message ID or definition", value)
}
