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

func stableLiteralDiagnostic(value string) string {
	return fmt.Sprintf("raw stable contract literal %q used in production code; use the typed constant or definition", value)
}
