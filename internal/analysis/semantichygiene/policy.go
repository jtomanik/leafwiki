package semantichygiene

import "github.com/perber/wiki/internal/analysis/checkerpolicy"

type ruleID string

const (
	ruleSemanticStringLeak                ruleID = "semantic.string-leak"
	ruleDirectCast                        ruleID = "semantic.direct-cast"
	ruleSemanticUncheckedConstructor      ruleID = "semantic.unchecked-constructor"
	ruleSemanticFixtureRuntimeConstructor ruleID = "semantic.fixture-runtime-constructor"
	ruleSemanticTestRawLiteral            ruleID = "semantic.test-raw-literal"
	ruleSemanticRawSignature              ruleID = "semantic.raw-signature"
	ruleSemanticRawField                  ruleID = "semantic.raw-field"
	ruleSemanticRawPrimitive              ruleID = "semantic.raw-primitive"
	ruleSemanticValidatorReturn           ruleID = "semantic.validator-return"
	ruleI18nRawProse                      ruleID = "i18n.raw-prose"
	ruleI18nRawProseSink                  ruleID = "i18n.raw-prose-sink"
	ruleI18nLocalizedErrorPassthrough     ruleID = "i18n.localized-error-passthrough"
	ruleI18nResponseStatusForward         ruleID = "i18n.response-status-forward"
	ruleI18nMessageField                  ruleID = "i18n.message-field"
	ruleI18nMessageParameter              ruleID = "i18n.message-parameter"
	ruleI18nRenderedErrorPresence         ruleID = "i18n.rendered-error-presence"
	ruleContractRawLiteral                ruleID = "contract.raw-literal"
)

type waiverScopeKind int

const (
	waiverScopeNone waiverScopeKind = iota
)

type ruleMetadata struct {
	messagePrefix string
	waivable      bool
	scope         waiverScopeKind
}

var ruleMetadataByID = map[ruleID]ruleMetadata{
	ruleSemanticStringLeak:                hardRule(ruleSemanticStringLeak),
	ruleDirectCast:                        hardRule(ruleDirectCast),
	ruleSemanticUncheckedConstructor:      hardRule(ruleSemanticUncheckedConstructor),
	ruleSemanticFixtureRuntimeConstructor: hardRule(ruleSemanticFixtureRuntimeConstructor),
	ruleSemanticTestRawLiteral:            hardRule(ruleSemanticTestRawLiteral),
	ruleSemanticRawSignature:              hardRule(ruleSemanticRawSignature),
	ruleSemanticRawField:                  hardRule(ruleSemanticRawField),
	ruleSemanticRawPrimitive:              hardRule(ruleSemanticRawPrimitive),
	ruleSemanticValidatorReturn:           hardRule(ruleSemanticValidatorReturn),
	ruleI18nRawProse:                      hardRule(ruleI18nRawProse),
	ruleI18nRawProseSink:                  hardRule(ruleI18nRawProseSink),
	ruleI18nLocalizedErrorPassthrough:     hardRule(ruleI18nLocalizedErrorPassthrough),
	ruleI18nResponseStatusForward:         hardRule(ruleI18nResponseStatusForward),
	ruleI18nMessageField:                  hardRule(ruleI18nMessageField),
	ruleI18nMessageParameter:              hardRule(ruleI18nMessageParameter),
	ruleI18nRenderedErrorPresence:         hardRule(ruleI18nRenderedErrorPresence),
	ruleContractRawLiteral:                hardRule(ruleContractRawLiteral),
}

func hardRule(id ruleID) ruleMetadata {
	return ruleMetadata{messagePrefix: string(id), waivable: false, scope: waiverScopeNone}
}

func metadataForRule(id ruleID) (ruleMetadata, bool) {
	metadata, ok := ruleMetadataByID[id]
	return metadata, ok
}

func allRuleMetadata() map[ruleID]ruleMetadata {
	metadata := make(map[ruleID]ruleMetadata, len(ruleMetadataByID))
	for id, value := range ruleMetadataByID {
		metadata[id] = value
	}
	return metadata
}

func semanticRuleSet() checkerpolicy.RuleSet {
	return checkerpolicy.NewRuleSet(semanticRuleMetadata(), nil, 0)
}

func semanticRuleMetadata() map[checkerpolicy.RuleID]checkerpolicy.RuleMetadata {
	metadata := map[checkerpolicy.RuleID]checkerpolicy.RuleMetadata{}
	for _, id := range []ruleID{
		ruleSemanticStringLeak,
		ruleDirectCast,
		ruleSemanticUncheckedConstructor,
		ruleSemanticFixtureRuntimeConstructor,
		ruleSemanticTestRawLiteral,
		ruleSemanticRawSignature,
		ruleSemanticRawField,
		ruleSemanticRawPrimitive,
		ruleSemanticValidatorReturn,
		ruleI18nRawProse,
		ruleI18nRawProseSink,
		ruleI18nLocalizedErrorPassthrough,
		ruleI18nResponseStatusForward,
		ruleI18nMessageField,
		ruleI18nMessageParameter,
		ruleI18nRenderedErrorPresence,
		ruleContractRawLiteral,
	} {
		metadata[checkerpolicy.RuleID(id)] = checkerpolicy.HardRule(checkerpolicy.RuleID(id))
	}
	return metadata
}
