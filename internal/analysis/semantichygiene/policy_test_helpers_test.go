package semantichygiene

import (
	"go/ast"
	"go/types"
)

type fileBoundaryCase struct {
	input             string
	testFile          bool
	generatedVendored bool
	persistence       bool
	edge              bool
}

type ruleMetadataRegistrationState uint8

const (
	ruleMetadataMissing ruleMetadataRegistrationState = iota
	ruleMetadataRegistered
)

type ruleMetadataObservation struct {
	State    ruleMetadataRegistrationState
	Metadata ruleMetadata
}

func observeRuleMetadata(id ruleID) ruleMetadataObservation {
	metadata, ok := metadataForRule(id)
	if !ok {
		return ruleMetadataObservation{State: ruleMetadataMissing}
	}
	return ruleMetadataObservation{State: ruleMetadataRegistered, Metadata: metadata}
}

type semanticTypeLookupState uint8

const (
	semanticTypeMissing semanticTypeLookupState = iota
	semanticTypeResolved
)

type semanticTypeLookupObservation struct {
	State semanticTypeLookupState
	Type  string
}

func semanticTypeObservation(got string, ok bool) semanticTypeLookupObservation {
	if !ok {
		return semanticTypeLookupObservation{State: semanticTypeMissing}
	}
	return semanticTypeLookupObservation{State: semanticTypeResolved, Type: got}
}

type semanticPrimitiveRecognitionState uint8

const (
	semanticPrimitiveRejected semanticPrimitiveRecognitionState = iota
	semanticPrimitiveRecognized
)

func classifySemanticPrimitiveInContext(name string, context string) semanticPrimitiveRecognitionState {
	if semanticPrimitiveNameInContext(name, context) {
		return semanticPrimitiveRecognized
	}
	return semanticPrimitiveRejected
}

func classifySemanticPrimitive(name string) semanticPrimitiveRecognitionState {
	if semanticPrimitiveName(name) {
		return semanticPrimitiveRecognized
	}
	return semanticPrimitiveRejected
}

type messageBearingStructState uint8

const (
	messageBearingStructAbsent messageBearingStructState = iota
	messageBearingStructPresent
)

func classifyMessageBearingStruct(named *types.Named, strct *types.Struct) messageBearingStructState {
	if namedStructIsMessageBearing(named, strct) {
		return messageBearingStructPresent
	}
	return messageBearingStructAbsent
}

type messageIDFieldState uint8

const (
	messageIDFieldAbsent messageIDFieldState = iota
	messageIDFieldPresent
)

func classifyMessageIDField(named *types.Named, strct *types.Struct) messageIDFieldState {
	if namedStructHasMessageID(named, strct) {
		return messageIDFieldPresent
	}
	return messageIDFieldAbsent
}

type stableStringLiteralState uint8

const (
	stableStringLiteralRejected stableStringLiteralState = iota
	stableStringLiteralEmptyOrRoot
)

func classifyEmptyOrRootString(lit *ast.BasicLit) stableStringLiteralState {
	if isEmptyOrRootString(lit) {
		return stableStringLiteralEmptyOrRoot
	}
	return stableStringLiteralRejected
}

type stableContractContextState uint8

const (
	stableContractContextRejected stableContractContextState = iota
	stableContractContextSuggested
)

func classifyAssignStableContractContext(assign *ast.AssignStmt, lit *ast.BasicLit) stableContractContextState {
	if assignStmtValueNameSuggestsStableContract(assign, lit) {
		return stableContractContextSuggested
	}
	return stableContractContextRejected
}

func classifyValueSpecStableContractContext(spec *ast.ValueSpec, lit *ast.BasicLit) stableContractContextState {
	if valueSpecNameSuggestsStableContract(spec, lit) {
		return stableContractContextSuggested
	}
	return stableContractContextRejected
}

type callArgumentContainmentState uint8

const (
	callArgumentAbsent callArgumentContainmentState = iota
	callArgumentContained
)

func classifyCallArgumentContainment(call *ast.CallExpr, target ast.Expr) callArgumentContainmentState {
	if callContainsArg(call, target) {
		return callArgumentContained
	}
	return callArgumentAbsent
}

func classifyFirstCallArgumentContainment(call *ast.CallExpr, target ast.Expr) callArgumentContainmentState {
	if callContainsFirstArg(call, target) {
		return callArgumentContained
	}
	return callArgumentAbsent
}

type mapKeyTypeState uint8

const (
	mapKeyTypeOther mapKeyTypeState = iota
	mapKeyTypeString
)

func classifyMapKeyType(typ types.Type) mapKeyTypeState {
	if isStringKeyedMap(typ) {
		return mapKeyTypeString
	}
	return mapKeyTypeOther
}
