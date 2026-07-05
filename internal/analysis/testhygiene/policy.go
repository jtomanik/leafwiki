package testhygiene

import (
	"go/ast"
	"go/token"

	"github.com/perber/wiki/internal/analysis/checkerpolicy"
	"golang.org/x/tools/go/analysis"
)

type ruleID = checkerpolicy.RuleID

const (
	ruleSemanticStringLeak                     ruleID = "semantic.string-leak"
	ruleDirectCast                             ruleID = "semantic.direct-cast"
	ruleSemanticUncheckedConstructor           ruleID = "semantic.unchecked-constructor"
	ruleSemanticFixtureRuntimeConstructor      ruleID = "semantic.fixture-runtime-constructor"
	ruleSemanticTestRawLiteral                 ruleID = "semantic.test-raw-literal"
	ruleSemanticRawSignature                   ruleID = "semantic.raw-signature"
	ruleSemanticRawField                       ruleID = "semantic.raw-field"
	ruleSemanticRawPrimitive                   ruleID = "semantic.raw-primitive"
	ruleSemanticValidatorReturn                ruleID = "semantic.validator-return"
	ruleI18nRawProse                           ruleID = "i18n.raw-prose"
	ruleI18nRawProseSink                       ruleID = "i18n.raw-prose-sink"
	ruleI18nLocalizedErrorPassthrough          ruleID = "i18n.localized-error-passthrough"
	ruleI18nResponseStatusForward              ruleID = "i18n.response-status-forward"
	ruleI18nMessageField                       ruleID = "i18n.message-field"
	ruleI18nMessageParameter                   ruleID = "i18n.message-parameter"
	ruleContractRawLiteral                     ruleID = "contract.raw-literal"
	ruleDependencyE2EProxyInternalImport       ruleID = "dependency.e2e-proxy-internal-import"
	ruleArchitectureCoreToWiki                 ruleID = "architecture.import-boundary.core-to-wiki"
	ruleArchitectureCoreToHTTP                 ruleID = "architecture.import-boundary.core-to-http"
	ruleArchitectureCoreToProjectDaemon        ruleID = "architecture.import-boundary.core-to-projectdaemon"
	ruleArchitectureWikiToCmdLeafwiki          ruleID = "architecture.import-boundary.wiki-to-cmd-leafwiki"
	ruleArchitectureProjectDaemonToCmdLeafwiki ruleID = "architecture.import-boundary.projectdaemon-to-cmd-leafwiki"
	ruleArchitectureWorkspacedToFrontd         ruleID = "architecture.import-boundary.workspaced-to-frontd"
	ruleGinkgoFocus                            ruleID = "ginkgo.focus"
	ruleGinkgoPending                          ruleID = "ginkgo.pending"
	ruleGinkgoFlakeAttempts                    ruleID = "ginkgo.flake-attempts"
	ruleGinkgoRestrictedDecorator              ruleID = "ginkgo.restricted-decorator"
	ruleGinkgoTaxonomyMissingLabel             ruleID = "ginkgo.taxonomy-label.missing"
	ruleGinkgoTaxonomyUnknownLabel             ruleID = "ginkgo.taxonomy-label.unknown"
	ruleGinkgoTaxonomyDynamicLabel             ruleID = "ginkgo.taxonomy-label.dynamic"
	ruleGinkgoTaxonomyMultipleLabels           ruleID = "ginkgo.taxonomy-label.multiple"
	ruleGinkgoContainerCall                    ruleID = "ginkgo.container-call"
	ruleGinkgoContainerStateInitialization     ruleID = "ginkgo.container-state-initialization"
	ruleGinkgoEntrySetupValue                  ruleID = "ginkgo.entry-setup-value"
	ruleGinkgoSemanticEntryData                ruleID = "ginkgo.semantic-entry-data"
	ruleGinkgoGoroutineRecover                 ruleID = "ginkgo.goroutine-recover"
	ruleGinkgoBlockingReceive                  ruleID = "ginkgo.blocking-receive"
	ruleGinkgoHelperFirst                      ruleID = "ginkgo.helper-first"
	ruleGinkgoGlobalStateCleanup               ruleID = "ginkgo.global-state-cleanup"
	ruleGinkgoTestName                         ruleID = "ginkgo.test-name"
	ruleGinkgoCoverageName                     ruleID = "ginkgo.coverage-name"
	ruleGinkgoVagueName                        ruleID = "ginkgo.vague-name"
	ruleGinkgoBooleanOutcomeName               ruleID = "ginkgo.boolean-outcome-name"
	ruleGinkgoTestingTInSpec                   ruleID = "ginkgo.testing-t-in-spec"
	ruleGinkgoFailInSpec                       ruleID = "ginkgo.fail-in-spec"
	ruleGinkgoLinterRawIgnore                  ruleID = "ginkgo-linter.raw-ignore"
	ruleGinkgoTopLevelIt                       ruleID = "ginkgo.top-level-it"
	ruleGinkgoWideEntry                        ruleID = "ginkgo.wide-entry"
	ruleGomegaErrorString                      ruleID = "gomega.err-error-string"
	ruleGomegaRawStringMatchError              ruleID = "gomega.raw-string-match-error"
	ruleGomegaErrorNilMatcher                  ruleID = "gomega.error-nil-matcher"
	ruleGomegaGenericHaveOccurred              ruleID = "gomega.generic-have-occurred"
	ruleGomegaInlineErrorSucceed               ruleID = "gomega.inline-error-succeed"
	ruleGomegaMultiReturnErrorMatcher          ruleID = "gomega.multi-return-error-matcher"
	ruleGomegaStringsContains                  ruleID = "gomega.strings-contains"
	ruleGomegaLastErrorNotEmpty                ruleID = "gomega.last-error-not-empty"
	ruleGomegaStringPredicate                  ruleID = "gomega.string-predicate"
	ruleGomegaRegexpMatchString                ruleID = "gomega.regexp-match-string"
	ruleGomegaErrorsIsMatcher                  ruleID = "gomega.errors-is-matcher"
	ruleGomegaErrorsAsMatcher                  ruleID = "gomega.errors-as-matcher"
	ruleGomegaOSIsNotExistMatcher              ruleID = "gomega.os-is-not-exist-matcher"
	ruleGomegaLenEqual                         ruleID = "gomega.len-equal"
	ruleGomegaBinaryBoolean                    ruleID = "gomega.binary-boolean"
	ruleGomegaBooleanLiteral                   ruleID = "gomega.boolean-literal"
	ruleGomegaCommaOKAssertion                 ruleID = "gomega.comma-ok-assertion"
	ruleGomegaIgnoredSemanticBoolean           ruleID = "gomega.ignored-semantic-boolean"
	ruleGomegaProxyBoolean                     ruleID = "gomega.proxy-boolean"
	ruleGomegaControlStatusMatcher             ruleID = "gomega.control-status-matcher"
	ruleGomegaRawStatusCode                    ruleID = "gomega.raw-status-code"
	ruleGomegaMapIndex                         ruleID = "gomega.map-index"
	ruleGomegaHTTPStatus                       ruleID = "gomega.http-status"
	ruleGomegaHTTPBody                         ruleID = "gomega.http-body"
	ruleGomegaRepeatedHTTPBody                 ruleID = "gomega.repeated-http-body"
	ruleGomegaHTTPHeader                       ruleID = "gomega.http-header"
	ruleGomegaStructuredErrorMatcher           ruleID = "gomega.structured-error-matcher"
	ruleGomegaStructuredProtocolKey            ruleID = "gomega.structured-protocol-key"
	ruleGomegaStructuredProtocolPayload        ruleID = "gomega.structured-protocol-payload"
	ruleGomegaStructuredProtocolStatus         ruleID = "gomega.structured-protocol-status"
	ruleGomegaAsyncContext                     ruleID = "gomega.async-context"
	ruleGomegaAsyncBoolean                     ruleID = "gomega.async-boolean"
	ruleGomegaAsyncNegativeReceive             ruleID = "gomega.async-negative-receive"
	ruleGomegaAsyncBareValue                   ruleID = "gomega.async-bare-value"
	ruleGomegaAsyncCallbackExpect              ruleID = "gomega.async-callback-expect"
	ruleGomegaHelperOffset                     ruleID = "gomega.helper-offset"
	ruleGomegaHelperShouldBeMatcher            ruleID = "gomega.helper-should-be-matcher"
	ruleGomegaRepeatedFieldAssertions          ruleID = "gomega.repeated-field-assertions"
	ruleGomegaCollectionIndexAssertion         ruleID = "gomega.collection-index-assertion"
	ruleGomegaNonEmptyCollection               ruleID = "gomega.non-empty-collection"
	ruleGomegaSemanticScalarNotEmpty           ruleID = "gomega.semantic-scalar-not-empty"
	ruleGomegaEqualEmpty                       ruleID = "gomega.equal-empty"
	ruleGomegaEqualZero                        ruleID = "gomega.equal-zero"
	ruleGomegaNumericEquivalent                ruleID = "gomega.numeric-equivalent"
	ruleGomegaTimeEqual                        ruleID = "gomega.time-equal"
	ruleGomegaMatcherAsValue                   ruleID = "gomega.matcher-as-value"
	ruleGomegaPositionalTransform              ruleID = "gomega.positional-transform"
	ruleGomegaPositionalCompositeAssertion     ruleID = "gomega.positional-composite-assertion"
)

var ruleSet = checkerpolicy.NewRuleSet(testRuleMetadata(), testWaiverBudgets(), 10)

func testRuleMetadata() map[checkerpolicy.RuleID]checkerpolicy.RuleMetadata {
	metadata := map[checkerpolicy.RuleID]checkerpolicy.RuleMetadata{
		ruleGinkgoTopLevelIt:               checkerpolicy.WaivableRule(ruleGinkgoTopLevelIt, checkerpolicy.WaiverScopeCall),
		ruleGinkgoWideEntry:                checkerpolicy.WaivableRule(ruleGinkgoWideEntry, checkerpolicy.WaiverScopeCall),
		ruleGomegaHelperShouldBeMatcher:    checkerpolicy.WaivableRule(ruleGomegaHelperShouldBeMatcher, checkerpolicy.WaiverScopeDeclaration),
		ruleGomegaRepeatedFieldAssertions:  checkerpolicy.WaivableRule(ruleGomegaRepeatedFieldAssertions, checkerpolicy.WaiverScopeCall),
		ruleGomegaCollectionIndexAssertion: checkerpolicy.WaivableRule(ruleGomegaCollectionIndexAssertion, checkerpolicy.WaiverScopeCall),
		ruleGomegaNonEmptyCollection:       checkerpolicy.WaivableRule(ruleGomegaNonEmptyCollection, checkerpolicy.WaiverScopeCall),
		ruleGomegaSemanticScalarNotEmpty:   checkerpolicy.WaivableRule(ruleGomegaSemanticScalarNotEmpty, checkerpolicy.WaiverScopeCall),
		ruleGomegaEqualEmpty:               checkerpolicy.WaivableRule(ruleGomegaEqualEmpty, checkerpolicy.WaiverScopeCall),
		ruleGomegaEqualZero:                checkerpolicy.WaivableRule(ruleGomegaEqualZero, checkerpolicy.WaiverScopeCall),
		ruleGomegaNumericEquivalent:        checkerpolicy.WaivableRule(ruleGomegaNumericEquivalent, checkerpolicy.WaiverScopeCall),
		ruleGomegaTimeEqual:                checkerpolicy.WaivableRule(ruleGomegaTimeEqual, checkerpolicy.WaiverScopeCall),
	}
	for _, id := range []checkerpolicy.RuleID{
		ruleSemanticStringLeak, ruleDirectCast, ruleSemanticUncheckedConstructor, ruleSemanticFixtureRuntimeConstructor,
		ruleSemanticTestRawLiteral, ruleSemanticRawSignature, ruleSemanticRawField, ruleSemanticRawPrimitive, ruleSemanticValidatorReturn,
		ruleI18nRawProse, ruleI18nRawProseSink, ruleI18nLocalizedErrorPassthrough, ruleI18nResponseStatusForward,
		ruleI18nMessageField, ruleI18nMessageParameter, ruleContractRawLiteral, ruleDependencyE2EProxyInternalImport,
		ruleArchitectureCoreToWiki, ruleArchitectureCoreToHTTP, ruleArchitectureCoreToProjectDaemon, ruleArchitectureWikiToCmdLeafwiki,
		ruleArchitectureProjectDaemonToCmdLeafwiki, ruleArchitectureWorkspacedToFrontd,
		ruleGinkgoFocus, ruleGinkgoPending, ruleGinkgoFlakeAttempts, ruleGinkgoRestrictedDecorator,
		ruleGinkgoTaxonomyMissingLabel, ruleGinkgoTaxonomyUnknownLabel, ruleGinkgoTaxonomyDynamicLabel, ruleGinkgoTaxonomyMultipleLabels,
		ruleGinkgoContainerCall, ruleGinkgoContainerStateInitialization, ruleGinkgoEntrySetupValue, ruleGinkgoSemanticEntryData,
		ruleGinkgoGoroutineRecover, ruleGinkgoBlockingReceive, ruleGinkgoHelperFirst, ruleGinkgoGlobalStateCleanup,
		ruleGinkgoTestName, ruleGinkgoCoverageName, ruleGinkgoVagueName, ruleGinkgoBooleanOutcomeName, ruleGinkgoTestingTInSpec,
		ruleGinkgoFailInSpec, ruleGinkgoLinterRawIgnore,
		ruleGomegaErrorString, ruleGomegaRawStringMatchError, ruleGomegaErrorNilMatcher, ruleGomegaGenericHaveOccurred,
		ruleGomegaInlineErrorSucceed, ruleGomegaMultiReturnErrorMatcher, ruleGomegaStringsContains, ruleGomegaLastErrorNotEmpty,
		ruleGomegaStringPredicate, ruleGomegaRegexpMatchString, ruleGomegaErrorsIsMatcher, ruleGomegaErrorsAsMatcher,
		ruleGomegaOSIsNotExistMatcher, ruleGomegaLenEqual, ruleGomegaBinaryBoolean, ruleGomegaBooleanLiteral,
		ruleGomegaCommaOKAssertion, ruleGomegaIgnoredSemanticBoolean, ruleGomegaProxyBoolean, ruleGomegaControlStatusMatcher,
		ruleGomegaRawStatusCode, ruleGomegaMapIndex, ruleGomegaHTTPStatus, ruleGomegaHTTPBody, ruleGomegaRepeatedHTTPBody,
		ruleGomegaHTTPHeader, ruleGomegaStructuredErrorMatcher, ruleGomegaStructuredProtocolKey, ruleGomegaStructuredProtocolPayload,
		ruleGomegaStructuredProtocolStatus, ruleGomegaAsyncContext, ruleGomegaAsyncBoolean, ruleGomegaAsyncNegativeReceive,
		ruleGomegaAsyncBareValue, ruleGomegaAsyncCallbackExpect, ruleGomegaHelperOffset, ruleGomegaMatcherAsValue,
		ruleGomegaPositionalTransform, ruleGomegaPositionalCompositeAssertion,
	} {
		metadata[id] = checkerpolicy.HardRule(id)
	}
	return metadata
}

func testWaiverBudgets() map[checkerpolicy.RuleID]int {
	return map[checkerpolicy.RuleID]int{
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
}

type analysisContext struct {
	pass   *analysis.Pass
	policy *checkerpolicy.Context
}

func newAnalysisContext(pass *analysis.Pass) *analysisContext {
	return &analysisContext{
		pass:   pass,
		policy: checkerpolicy.NewContext(pass, ruleSet, true),
	}
}

func (ctx *analysisContext) report(rule ruleID, node ast.Node, message string) {
	ctx.policy.Report(rule, node, message)
}

func (ctx *analysisContext) parent(node ast.Node) ast.Node {
	return ctx.policy.Parent(node)
}

func (ctx *analysisContext) filename(pos token.Pos) string {
	return ctx.policy.Filename(pos)
}

func (ctx *analysisContext) finalizeDiagnostics() {
	ctx.policy.FinalizeDiagnostics()
}
