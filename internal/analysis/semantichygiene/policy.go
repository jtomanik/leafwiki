package semantichygiene

type ruleID string

const (
	ruleSemanticStringLeak                 ruleID = "semantic.string-leak"
	ruleDirectCast                         ruleID = "semantic.direct-cast"
	ruleSemanticUncheckedConstructor       ruleID = "semantic.unchecked-constructor"
	ruleSemanticFixtureRuntimeConstructor  ruleID = "semantic.fixture-runtime-constructor"
	ruleSemanticTestRawLiteral             ruleID = "semantic.test-raw-literal"
	ruleSemanticRawSignature               ruleID = "semantic.raw-signature"
	ruleSemanticRawField                   ruleID = "semantic.raw-field"
	ruleSemanticRawPrimitive               ruleID = "semantic.raw-primitive"
	ruleSemanticValidatorReturn            ruleID = "semantic.validator-return"
	ruleI18nRawProse                       ruleID = "i18n.raw-prose"
	ruleI18nRawProseSink                   ruleID = "i18n.raw-prose-sink"
	ruleI18nLocalizedErrorPassthrough      ruleID = "i18n.localized-error-passthrough"
	ruleI18nResponseStatusForward          ruleID = "i18n.response-status-forward"
	ruleI18nMessageField                   ruleID = "i18n.message-field"
	ruleI18nMessageParameter               ruleID = "i18n.message-parameter"
	ruleContractRawLiteral                 ruleID = "contract.raw-literal"
	ruleDependencyE2EProxyInternalImport   ruleID = "dependency.e2e-proxy-internal-import"
	ruleGinkgoFocus                        ruleID = "ginkgo.focus"
	ruleGinkgoPending                      ruleID = "ginkgo.pending"
	ruleGinkgoFlakeAttempts                ruleID = "ginkgo.flake-attempts"
	ruleGinkgoRestrictedDecorator          ruleID = "ginkgo.restricted-decorator"
	ruleGinkgoTaxonomyMissingLabel         ruleID = "ginkgo.taxonomy-label.missing"
	ruleGinkgoTaxonomyUnknownLabel         ruleID = "ginkgo.taxonomy-label.unknown"
	ruleGinkgoTaxonomyDynamicLabel         ruleID = "ginkgo.taxonomy-label.dynamic"
	ruleGinkgoTaxonomyMultipleLabels       ruleID = "ginkgo.taxonomy-label.multiple"
	ruleGinkgoContainerCall                ruleID = "ginkgo.container-call"
	ruleGinkgoContainerStateInitialization ruleID = "ginkgo.container-state-initialization"
	ruleGinkgoEntrySetupValue              ruleID = "ginkgo.entry-setup-value"
	ruleGinkgoSemanticEntryData            ruleID = "ginkgo.semantic-entry-data"
	ruleGinkgoGoroutineRecover             ruleID = "ginkgo.goroutine-recover"
	ruleGinkgoBlockingReceive              ruleID = "ginkgo.blocking-receive"
	ruleGinkgoHelperFirst                  ruleID = "ginkgo.helper-first"
	ruleGinkgoGlobalStateCleanup           ruleID = "ginkgo.global-state-cleanup"
	ruleGinkgoTestName                     ruleID = "ginkgo.test-name"
	ruleGinkgoCoverageName                 ruleID = "ginkgo.coverage-name"
	ruleGinkgoVagueName                    ruleID = "ginkgo.vague-name"
	ruleGinkgoBooleanOutcomeName           ruleID = "ginkgo.boolean-outcome-name"
	ruleGinkgoTestingTInSpec               ruleID = "ginkgo.testing-t-in-spec"
	ruleGinkgoFailInSpec                   ruleID = "ginkgo.fail-in-spec"
	ruleGinkgoLinterRawIgnore              ruleID = "ginkgo-linter.raw-ignore"
	ruleGomegaErrorString                  ruleID = "gomega.err-error-string"
	ruleGomegaRawStringMatchError          ruleID = "gomega.raw-string-match-error"
	ruleGomegaErrorNilMatcher              ruleID = "gomega.error-nil-matcher"
	ruleGomegaGenericHaveOccurred          ruleID = "gomega.generic-have-occurred"
	ruleGomegaInlineErrorSucceed           ruleID = "gomega.inline-error-succeed"
	ruleGomegaMultiReturnErrorMatcher      ruleID = "gomega.multi-return-error-matcher"
	ruleGomegaStringsContains              ruleID = "gomega.strings-contains"
	ruleGomegaLastErrorNotEmpty            ruleID = "gomega.last-error-not-empty"
	ruleGomegaStringPredicate              ruleID = "gomega.string-predicate"
	ruleGomegaRegexpMatchString            ruleID = "gomega.regexp-match-string"
	ruleGomegaErrorsIsMatcher              ruleID = "gomega.errors-is-matcher"
	ruleGomegaErrorsAsMatcher              ruleID = "gomega.errors-as-matcher"
	ruleGomegaOSIsNotExistMatcher          ruleID = "gomega.os-is-not-exist-matcher"
	ruleGomegaLenEqual                     ruleID = "gomega.len-equal"
	ruleGomegaBinaryBoolean                ruleID = "gomega.binary-boolean"
	ruleGomegaBooleanLiteral               ruleID = "gomega.boolean-literal"
	ruleGomegaCommaOKAssertion             ruleID = "gomega.comma-ok-assertion"
	ruleGomegaIgnoredSemanticBoolean       ruleID = "gomega.ignored-semantic-boolean"
	ruleGomegaProxyBoolean                 ruleID = "gomega.proxy-boolean"
	ruleGomegaControlStatusMatcher         ruleID = "gomega.control-status-matcher"
	ruleGomegaRawStatusCode                ruleID = "gomega.raw-status-code"
	ruleGomegaMapIndex                     ruleID = "gomega.map-index"
	ruleGomegaHTTPStatus                   ruleID = "gomega.http-status"
	ruleGomegaHTTPBody                     ruleID = "gomega.http-body"
	ruleGomegaRepeatedHTTPBody             ruleID = "gomega.repeated-http-body"
	ruleGomegaHTTPHeader                   ruleID = "gomega.http-header"
	ruleGomegaStructuredErrorMatcher       ruleID = "gomega.structured-error-matcher"
	ruleGomegaStructuredProtocolKey        ruleID = "gomega.structured-protocol-key"
	ruleGomegaStructuredProtocolPayload    ruleID = "gomega.structured-protocol-payload"
	ruleGomegaStructuredProtocolStatus     ruleID = "gomega.structured-protocol-status"
	ruleGomegaAsyncContext                 ruleID = "gomega.async-context"
	ruleGomegaAsyncBoolean                 ruleID = "gomega.async-boolean"
	ruleGomegaAsyncNegativeReceive         ruleID = "gomega.async-negative-receive"
	ruleGomegaAsyncBareValue               ruleID = "gomega.async-bare-value"
	ruleGomegaAsyncCallbackExpect          ruleID = "gomega.async-callback-expect"
	ruleGomegaHelperOffset                 ruleID = "gomega.helper-offset"
	ruleGinkgoTopLevelIt                   ruleID = "ginkgo.top-level-it"
	ruleGinkgoWideEntry                    ruleID = "ginkgo.wide-entry"
	ruleGomegaHelperShouldBeMatcher        ruleID = "gomega.helper-should-be-matcher"
	ruleGomegaRepeatedFieldAssertions      ruleID = "gomega.repeated-field-assertions"
	ruleGomegaCollectionIndexAssertion     ruleID = "gomega.collection-index-assertion"
	ruleGomegaNonEmptyCollection           ruleID = "gomega.non-empty-collection"
	ruleGomegaSemanticScalarNotEmpty       ruleID = "gomega.semantic-scalar-not-empty"
	ruleGomegaEqualEmpty                   ruleID = "gomega.equal-empty"
	ruleGomegaEqualZero                    ruleID = "gomega.equal-zero"
	ruleGomegaNumericEquivalent            ruleID = "gomega.numeric-equivalent"
	ruleGomegaTimeEqual                    ruleID = "gomega.time-equal"
	ruleGomegaMatcherAsValue               ruleID = "gomega.matcher-as-value"
	ruleGomegaPositionalTransform          ruleID = "gomega.positional-transform"
	ruleGomegaPositionalCompositeAssertion ruleID = "gomega.positional-composite-assertion"
	ruleWaiverBudgetExceeded               ruleID = "waiver.budget-exceeded"
	ruleWaiverDuplicate                    ruleID = "waiver.duplicate"
	ruleWaiverMalformed                    ruleID = "waiver.malformed"
	ruleWaiverMissingExplanation           ruleID = "waiver.missing-explanation"
	ruleWaiverNonWaivableRule              ruleID = "waiver.non-waivable-rule"
	ruleWaiverStale                        ruleID = "waiver.stale"
	ruleWaiverUnknownRule                  ruleID = "waiver.unknown-rule"
)

type waiverScopeKind int

const (
	waiverScopeNone waiverScopeKind = iota
	waiverScopeNextNode
	waiverScopeCall
	waiverScopeDeclaration
)

type ruleMetadata struct {
	messagePrefix string
	waivable      bool
	scope         waiverScopeKind
}

var ruleMetadataByID = map[ruleID]ruleMetadata{
	ruleSemanticStringLeak:                 hardRule(ruleSemanticStringLeak),
	ruleDirectCast:                         hardRule(ruleDirectCast),
	ruleSemanticUncheckedConstructor:       hardRule(ruleSemanticUncheckedConstructor),
	ruleSemanticFixtureRuntimeConstructor:  hardRule(ruleSemanticFixtureRuntimeConstructor),
	ruleSemanticTestRawLiteral:             hardRule(ruleSemanticTestRawLiteral),
	ruleSemanticRawSignature:               hardRule(ruleSemanticRawSignature),
	ruleSemanticRawField:                   hardRule(ruleSemanticRawField),
	ruleSemanticRawPrimitive:               hardRule(ruleSemanticRawPrimitive),
	ruleSemanticValidatorReturn:            hardRule(ruleSemanticValidatorReturn),
	ruleI18nRawProse:                       hardRule(ruleI18nRawProse),
	ruleI18nRawProseSink:                   hardRule(ruleI18nRawProseSink),
	ruleI18nLocalizedErrorPassthrough:      hardRule(ruleI18nLocalizedErrorPassthrough),
	ruleI18nResponseStatusForward:          hardRule(ruleI18nResponseStatusForward),
	ruleI18nMessageField:                   hardRule(ruleI18nMessageField),
	ruleI18nMessageParameter:               hardRule(ruleI18nMessageParameter),
	ruleContractRawLiteral:                 hardRule(ruleContractRawLiteral),
	ruleDependencyE2EProxyInternalImport:   hardRule(ruleDependencyE2EProxyInternalImport),
	ruleGinkgoFocus:                        hardRule(ruleGinkgoFocus),
	ruleGinkgoPending:                      hardRule(ruleGinkgoPending),
	ruleGinkgoFlakeAttempts:                hardRule(ruleGinkgoFlakeAttempts),
	ruleGinkgoRestrictedDecorator:          hardRule(ruleGinkgoRestrictedDecorator),
	ruleGinkgoTaxonomyMissingLabel:         hardRule(ruleGinkgoTaxonomyMissingLabel),
	ruleGinkgoTaxonomyUnknownLabel:         hardRule(ruleGinkgoTaxonomyUnknownLabel),
	ruleGinkgoTaxonomyDynamicLabel:         hardRule(ruleGinkgoTaxonomyDynamicLabel),
	ruleGinkgoTaxonomyMultipleLabels:       hardRule(ruleGinkgoTaxonomyMultipleLabels),
	ruleGinkgoContainerCall:                hardRule(ruleGinkgoContainerCall),
	ruleGinkgoContainerStateInitialization: hardRule(ruleGinkgoContainerStateInitialization),
	ruleGinkgoEntrySetupValue:              hardRule(ruleGinkgoEntrySetupValue),
	ruleGinkgoSemanticEntryData:            hardRule(ruleGinkgoSemanticEntryData),
	ruleGinkgoGoroutineRecover:             hardRule(ruleGinkgoGoroutineRecover),
	ruleGinkgoBlockingReceive:              hardRule(ruleGinkgoBlockingReceive),
	ruleGinkgoHelperFirst:                  hardRule(ruleGinkgoHelperFirst),
	ruleGinkgoGlobalStateCleanup:           hardRule(ruleGinkgoGlobalStateCleanup),
	ruleGinkgoTestName:                     hardRule(ruleGinkgoTestName),
	ruleGinkgoCoverageName:                 hardRule(ruleGinkgoCoverageName),
	ruleGinkgoVagueName:                    hardRule(ruleGinkgoVagueName),
	ruleGinkgoBooleanOutcomeName:           hardRule(ruleGinkgoBooleanOutcomeName),
	ruleGinkgoTestingTInSpec:               hardRule(ruleGinkgoTestingTInSpec),
	ruleGinkgoFailInSpec:                   hardRule(ruleGinkgoFailInSpec),
	ruleGinkgoLinterRawIgnore:              hardRule(ruleGinkgoLinterRawIgnore),
	ruleGomegaErrorString:                  hardRule(ruleGomegaErrorString),
	ruleGomegaRawStringMatchError:          hardRule(ruleGomegaRawStringMatchError),
	ruleGomegaErrorNilMatcher:              hardRule(ruleGomegaErrorNilMatcher),
	ruleGomegaGenericHaveOccurred:          hardRule(ruleGomegaGenericHaveOccurred),
	ruleGomegaInlineErrorSucceed:           hardRule(ruleGomegaInlineErrorSucceed),
	ruleGomegaMultiReturnErrorMatcher:      hardRule(ruleGomegaMultiReturnErrorMatcher),
	ruleGomegaStringsContains:              hardRule(ruleGomegaStringsContains),
	ruleGomegaLastErrorNotEmpty:            hardRule(ruleGomegaLastErrorNotEmpty),
	ruleGomegaStringPredicate:              hardRule(ruleGomegaStringPredicate),
	ruleGomegaRegexpMatchString:            hardRule(ruleGomegaRegexpMatchString),
	ruleGomegaErrorsIsMatcher:              hardRule(ruleGomegaErrorsIsMatcher),
	ruleGomegaErrorsAsMatcher:              hardRule(ruleGomegaErrorsAsMatcher),
	ruleGomegaOSIsNotExistMatcher:          hardRule(ruleGomegaOSIsNotExistMatcher),
	ruleGomegaLenEqual:                     hardRule(ruleGomegaLenEqual),
	ruleGomegaBinaryBoolean:                hardRule(ruleGomegaBinaryBoolean),
	ruleGomegaBooleanLiteral:               hardRule(ruleGomegaBooleanLiteral),
	ruleGomegaCommaOKAssertion:             hardRule(ruleGomegaCommaOKAssertion),
	ruleGomegaIgnoredSemanticBoolean:       hardRule(ruleGomegaIgnoredSemanticBoolean),
	ruleGomegaProxyBoolean:                 hardRule(ruleGomegaProxyBoolean),
	ruleGomegaControlStatusMatcher:         hardRule(ruleGomegaControlStatusMatcher),
	ruleGomegaRawStatusCode:                hardRule(ruleGomegaRawStatusCode),
	ruleGomegaMapIndex:                     hardRule(ruleGomegaMapIndex),
	ruleGomegaHTTPStatus:                   hardRule(ruleGomegaHTTPStatus),
	ruleGomegaHTTPBody:                     hardRule(ruleGomegaHTTPBody),
	ruleGomegaRepeatedHTTPBody:             hardRule(ruleGomegaRepeatedHTTPBody),
	ruleGomegaHTTPHeader:                   hardRule(ruleGomegaHTTPHeader),
	ruleGomegaStructuredErrorMatcher:       hardRule(ruleGomegaStructuredErrorMatcher),
	ruleGomegaStructuredProtocolKey:        hardRule(ruleGomegaStructuredProtocolKey),
	ruleGomegaStructuredProtocolPayload:    hardRule(ruleGomegaStructuredProtocolPayload),
	ruleGomegaStructuredProtocolStatus:     hardRule(ruleGomegaStructuredProtocolStatus),
	ruleGomegaAsyncContext:                 hardRule(ruleGomegaAsyncContext),
	ruleGomegaAsyncBoolean:                 hardRule(ruleGomegaAsyncBoolean),
	ruleGomegaAsyncNegativeReceive:         hardRule(ruleGomegaAsyncNegativeReceive),
	ruleGomegaAsyncBareValue:               hardRule(ruleGomegaAsyncBareValue),
	ruleGomegaAsyncCallbackExpect:          hardRule(ruleGomegaAsyncCallbackExpect),
	ruleGomegaHelperOffset:                 hardRule(ruleGomegaHelperOffset),
	ruleGinkgoTopLevelIt:                   waivableRule(ruleGinkgoTopLevelIt, waiverScopeCall),
	ruleGinkgoWideEntry:                    waivableRule(ruleGinkgoWideEntry, waiverScopeCall),
	ruleGomegaHelperShouldBeMatcher:        waivableRule(ruleGomegaHelperShouldBeMatcher, waiverScopeDeclaration),
	ruleGomegaRepeatedFieldAssertions:      waivableRule(ruleGomegaRepeatedFieldAssertions, waiverScopeCall),
	ruleGomegaCollectionIndexAssertion:     waivableRule(ruleGomegaCollectionIndexAssertion, waiverScopeCall),
	ruleGomegaNonEmptyCollection:           waivableRule(ruleGomegaNonEmptyCollection, waiverScopeCall),
	ruleGomegaSemanticScalarNotEmpty:       waivableRule(ruleGomegaSemanticScalarNotEmpty, waiverScopeCall),
	ruleGomegaEqualEmpty:                   waivableRule(ruleGomegaEqualEmpty, waiverScopeCall),
	ruleGomegaEqualZero:                    waivableRule(ruleGomegaEqualZero, waiverScopeCall),
	ruleGomegaNumericEquivalent:            waivableRule(ruleGomegaNumericEquivalent, waiverScopeCall),
	ruleGomegaTimeEqual:                    waivableRule(ruleGomegaTimeEqual, waiverScopeCall),
	ruleGomegaMatcherAsValue:               hardRule(ruleGomegaMatcherAsValue),
	ruleGomegaPositionalTransform:          hardRule(ruleGomegaPositionalTransform),
	ruleGomegaPositionalCompositeAssertion: hardRule(ruleGomegaPositionalCompositeAssertion),
	ruleWaiverBudgetExceeded:               hardRule(ruleWaiverBudgetExceeded),
	ruleWaiverDuplicate:                    hardRule(ruleWaiverDuplicate),
	ruleWaiverMalformed:                    hardRule(ruleWaiverMalformed),
	ruleWaiverMissingExplanation:           hardRule(ruleWaiverMissingExplanation),
	ruleWaiverNonWaivableRule:              hardRule(ruleWaiverNonWaivableRule),
	ruleWaiverStale:                        hardRule(ruleWaiverStale),
	ruleWaiverUnknownRule:                  hardRule(ruleWaiverUnknownRule),
}

const totalWaiverBudget = 10

var waiverBudgetsByRule = map[ruleID]int{
	ruleGinkgoTopLevelIt:               3,
	ruleGinkgoWideEntry:                3,
	ruleGomegaHelperShouldBeMatcher:    3,
	ruleGomegaRepeatedFieldAssertions:  3,
	ruleGomegaCollectionIndexAssertion: 3,
	ruleGomegaNonEmptyCollection:       3,
	ruleGomegaSemanticScalarNotEmpty:   3,
	ruleGomegaEqualEmpty:               3,
	ruleGomegaEqualZero:                3,
	ruleGomegaNumericEquivalent:        3,
	ruleGomegaTimeEqual:                3,
}

func hardRule(id ruleID) ruleMetadata {
	return ruleMetadata{messagePrefix: string(id), waivable: false, scope: waiverScopeNone}
}

func waivableRule(id ruleID, scope waiverScopeKind) ruleMetadata {
	return ruleMetadata{messagePrefix: string(id), waivable: true, scope: scope}
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

func waiverBudgetForRule(id ruleID) (int, bool) {
	budget, ok := waiverBudgetsByRule[id]
	return budget, ok
}
