package testhygiene

import (
	"fmt"
	"strings"
)

func gomegaErrorStringMatcherDiagnostic() string {
	return "assert error values with MatchError instead of matching err.Error()"
}

func gomegaRawStringMatchErrorDiagnostic() string {
	return "assert error semantics with a typed/domain matcher or injected error value instead of raw string MatchError"
}

func gomegaStringsContainsMatcherDiagnostic() string {
	return "use ContainSubstring matcher instead of asserting strings.Contains with BeTrue/BeFalse"
}

func gomegaRenderedMessageStringsContainsDiagnostic() string {
	return "do not assert rendered message text with strings.Contains; assert structured code, message ID, field, or path semantics instead"
}

func gomegaLastErrorNotEmptyDiagnostic() string {
	return "assert specific LastError semantics instead of only checking for non-empty rendered text"
}

func gomegaErrorsIsMatcherDiagnostic() string {
	return "use MatchError matcher instead of asserting errors.Is with BeTrue/BeFalse"
}

func gomegaErrorNilMatcherDiagnostic() string {
	return "use HaveOccurred matcher instead of nil assertions on error values"
}

func gomegaGenericHaveOccurredDiagnostic() string {
	return "assert expected error semantics with MatchError or a domain matcher instead of generic HaveOccurred"
}

func gomegaInlineErrorSucceedDiagnostic() string {
	return "use Succeed matcher for inline single-error calls instead of NotTo(HaveOccurred())"
}

func gomegaMultiReturnErrorMatcherDiagnostic() string {
	return "use Error() or a captured error variable when asserting multi-return functions with HaveOccurred/Succeed"
}

func gomegaBinaryBooleanMatcherDiagnostic() string {
	return "use semantic Gomega matchers instead of asserting binary expressions with BeTrue/BeFalse"
}

func gomegaBooleanLiteralMatcherDiagnostic() string {
	return "use semantic Gomega assertions instead of forcing pass/fail with boolean literals"
}

func gomegaBooleanLiteralEqualDiagnostic() string {
	return "use BeTrue/BeFalse instead of Equal(true/false) for boolean values"
}

func gomegaCommaOKAssertionDiagnostic() string {
	return "assert the decoded value or map contents with a semantic matcher instead of asserting comma-ok booleans"
}

func gomegaIgnoredSemanticBooleanDiagnostic() string {
	return "assert the semantic presence/status result instead of discarding a semantic boolean return with _"
}

func gomegaProxyBooleanDiagnostic() string {
	return "assert a semantic value or domain outcome instead of proxy boolean variables with boolean matchers"
}

func gomegaBooleanStateStringDiagnostic() string {
	return "do not convert boolean variables into string states for assertions; assert the semantic value or outcome directly"
}

func gomegaMatcherFactoryBooleanErrorGateDiagnostic() string {
	return "matcher factory captures boolean state while matching domain semantics; assert the semantic result directly or split into explicit domain matchers"
}

func gomegaMatcherFactoryTransformedBooleanContractDiagnostic() string {
	return "matcher factory feeds raw boolean parameters into transformed expected contracts; expose semantic matcher variants or typed outcome values instead"
}

func gomegaMatcherFactoryProxyBooleanPredicateDiagnostic() string {
	return "matcher factory returns proxy boolean fields as the matcher oracle; assert a semantic value or include the domain outcome in the matcher"
}

func gomegaMatcherFactoryPredicateOnlyBooleanDiagnostic() string {
	return "matcher factory returns a predicate-only boolean oracle; assert a semantic value or compose structured Gomega matchers instead"
}

func gomegaControlStatusMatcherDiagnostic() string {
	return "assert project daemon control errors with MatchError or a domain matcher instead of IsControlStatus with boolean matchers"
}

func gomegaRawStatusCodeDiagnostic() string {
	return "assert process/domain status with a semantic matcher or named status value instead of raw numeric status codes"
}

func gomegaStringPredicateMatcherDiagnostic(predicate string, matcher string) string {
	return fmt.Sprintf("use %s matcher instead of asserting %s with BeTrue/BeFalse", matcher, predicate)
}

func gomegaRegexpMatchStringDiagnostic() string {
	return "use MatchRegexp matcher instead of asserting regexp.MatchString with BeTrue/BeFalse"
}

func gomegaLenMatcherDiagnostic() string {
	return "use HaveLen or a collection matcher instead of asserting len() directly"
}

func gomegaMapIndexMatcherDiagnostic() string {
	return "use HaveKeyWithValue matcher instead of asserting a direct map index value"
}

func gomegaHTTPStatusMatcherDiagnostic() string {
	return "use HaveHTTPStatus matcher instead of asserting response status fields directly"
}

func gomegaHTTPBodyMatcherDiagnostic() string {
	return "use HaveHTTPBody matcher instead of matching recorder body strings directly"
}

func gomegaRepeatedHTTPBodyMatcherDiagnostic() string {
	return "compose repeated HaveHTTPBody assertions for the same response into one matcher"
}

func gomegaHTTPHeaderMatcherDiagnostic() string {
	return "use HaveHTTPHeaderWithValue matcher instead of matching response header values directly"
}

func gomegaHTTPHeaderResponseMatcherOnRequestDiagnostic() string {
	return "HaveHTTPHeaderWithValue matches HTTP responses; assert request headers with request-header semantics instead"
}

func gomegaAsyncCallbackExpectDiagnostic() string {
	return "use the Gomega value passed into Eventually/Consistently callbacks instead of global Expect"
}

func gomegaAsyncNegativeReceiveDiagnostic() string {
	return "use Consistently(...).ShouldNot(Receive()) to prove a channel stays quiet"
}

func gomegaAsyncBareValueDiagnostic() string {
	return "wrap bare eventually-polled values in a function so polling re-reads changing state"
}

func gomegaHelperOffsetDiagnostic(funcName string) string {
	return fmt.Sprintf("test assertion helper %s contains Gomega assertions without GinkgoHelper, WithOffset, ExpectWithOffset, or a Gomega parameter", funcName)
}

func gomegaStructuredErrorMatcherDiagnostic(fieldName string) string {
	return fmt.Sprintf("assert structured error semantics with a typed domain matcher/helper instead of matching %s directly", fieldName)
}

func gomegaStructuredProtocolKeyMatcherDiagnostic(fieldName string) string {
	return fmt.Sprintf("assert structured protocol semantics with a typed domain matcher/helper instead of matching key %s directly", fieldName)
}

func gomegaStructuredProtocolPayloadMatcherDiagnostic(matcherName string) string {
	return fmt.Sprintf("assert structured protocol semantics with a typed domain matcher/helper instead of matching raw %s payload directly", matcherName)
}

func gomegaStructuredProtocolStatusMatcherDiagnostic() string {
	return "assert MCP tool-result success or error semantics with a domain matcher instead of matching IsError as a raw boolean"
}

func gomegaNumericEquivalentDiagnostic() string {
	return "avoid BeEquivalentTo for numeric assertions; use Equal or BeNumerically"
}

func gomegaTimeEqualDiagnostic() string {
	return "use BeTemporally for time.Time equality assertions"
}

func gomegaMatcherAsValueDiagnostic(matcherName string) string {
	return fmt.Sprintf("do not pass a Gomega matcher as an expected value to %s; compose or apply the matcher directly", matcherName)
}

func gomegaPositionalTransformDiagnostic() string {
	return "do not collapse multiple fields into a positional WithTransform assertion; use HaveField/MatchFields or a named matcher"
}

func gomegaPositionalCompositeAssertionDiagnostic() string {
	return "do not compare multiple fields through a positional composite assertion; use HaveField/MatchFields or a named matcher"
}

func ginkgoContainerCallDiagnostic(name string) string {
	return fmt.Sprintf("move %s out of Ginkgo container body; containers should only declare specs and setup nodes", name)
}

func ginkgoContainerStateInitializationDiagnostic() string {
	return "move state initialization out of Ginkgo container body; declare variables in containers and initialize in setup nodes"
}

func ginkgoFocusDiagnostic() string {
	return "do not commit focused Ginkgo specs; remove Focus/F-prefixed node"
}

func ginkgoPendingDiagnostic() string {
	return "do not commit pending Ginkgo specs; finish or delete the spec instead"
}

func ginkgoFlakeAttemptsDiagnostic() string {
	return "do not commit Ginkgo flake retries; fix the flake or quarantine it outside the suite"
}

func ginkgoRestrictedDecoratorDiagnostic(name string) string {
	return fmt.Sprintf("avoid Ginkgo %s decorator unless the test suite policy explicitly allows it", name)
}

func ginkgoTaxonomyUnknownLabelDiagnostic(label string) string {
	return fmt.Sprintf("Ginkgo label %q is not allowed by the LeafWiki test taxonomy; use unit, integration, or e2e", label)
}

func ginkgoTaxonomyDynamicLabelDiagnostic() string {
	return "Ginkgo taxonomy labels must be static string literals"
}

func ginkgoTaxonomyMissingLabelDiagnostic() string {
	return "Ginkgo spec must have exactly one primary taxonomy label: unit, integration, or e2e"
}

func ginkgoTaxonomyMultipleLabelsDiagnostic(labels []string) string {
	return fmt.Sprintf("Ginkgo spec has multiple primary taxonomy labels (%s); use exactly one of unit, integration, or e2e", strings.Join(labels, ", "))
}

func ginkgoWideEntryDiagnostic() string {
	return "use a row struct for Ginkgo table entries with many parameters"
}

func ginkgoEntrySetupValueDiagnostic() string {
	return "Ginkgo Entry arguments are evaluated at construction time; pass stable row data instead of setup-initialized variables"
}

func ginkgoSemanticEntryDataDiagnostic(paramName string, semanticType string) string {
	return fmt.Sprintf("Ginkgo Entry data for %s uses raw string for %s; use a semantic fixture value or row struct", paramName, semanticType)
}

func ginkgoAsyncContextDiagnostic() string {
	return "propagate the spec context into Eventually/Consistently with WithContext or positional context"
}

func ginkgoGoroutineRecoverDiagnostic() string {
	return "goroutine with assertions must defer GinkgoRecover() or use GinkgoHelperGo"
}

func ginkgoBlockingReceiveDiagnostic() string {
	return "avoid blocking channel receives in specs; use Eventually(...).Should(Receive(...)) so failures surface"
}

func ginkgoHelperFirstDiagnostic(funcName string) string {
	return fmt.Sprintf("call GinkgoHelper() as the first statement in assertion helper %s", funcName)
}

func ginkgoReusableHelperMatcherDiagnostic(funcName string) string {
	return fmt.Sprintf("prefer a custom Gomega matcher for reusable assertion helper %s", funcName)
}

func ginkgoTopLevelItDiagnostic() string {
	return "top-level It reads like a migrated unit test; place it under a behavior container or waive with a specific reason"
}

func ginkgoTestNameDiagnostic(name string) string {
	return fmt.Sprintf("Ginkgo node name %q preserves a migrated testing.T name; describe observable behavior instead", name)
}

func ginkgoCoverageNameDiagnostic(name string) string {
	return fmt.Sprintf("Ginkgo node name %q reads like a coverage bucket; describe observable behavior instead", name)
}

func ginkgoVagueNameDiagnostic(name string) string {
	return fmt.Sprintf("Ginkgo node name %q is too vague to document behavior; describe the observable outcome instead", name)
}

func ginkgoBooleanOutcomeNameDiagnostic(name string) string {
	return fmt.Sprintf("Ginkgo node name %q describes a boolean return value; document the observable behavior instead", name)
}

func ginkgoTestingTInSpecDiagnostic(name string) string {
	return fmt.Sprintf("avoid %s adapter inside Ginkgo specs; use Gomega expectations and Ginkgo helpers", name)
}

func ginkgoTestingTAssertionDiagnostic(name string) string {
	return fmt.Sprintf("avoid %s assertion inside Ginkgo specs; use Gomega expectations and Ginkgo helpers", name)
}

func ginkgoFailInSpecDiagnostic() string {
	return "avoid direct ginkgo.Fail inside specs; use Gomega expectations so assertions read semantically"
}

func ginkgoFailureHelperInSpecDiagnostic(name string) string {
	return fmt.Sprintf("avoid failure helper %s inside specs; use Gomega expectations so assertions read semantically", name)
}

func ginkgoHiddenFailHelperInSpecDiagnostic(name string) string {
	return fmt.Sprintf("avoid helper %s that calls ginkgo.Fail inside specs; use Gomega expectations so assertions read semantically", name)
}

func ginkgoGlobalStateCleanupDiagnostic(name string) string {
	return fmt.Sprintf("restore global state changes with DeferCleanup next to %s", name)
}

func gomegaErrorsAsMatcherDiagnostic() string {
	return "assert error type semantics with MatchError/Satisfy instead of errors.As(...) with BeTrue/BeFalse"
}

func gomegaOSIsNotExistMatcherDiagnostic() string {
	return "assert error semantics with MatchError instead of os.IsNotExist(...) with BeTrue/BeFalse"
}

func gomegaAsyncBooleanMatcherDiagnostic() string {
	return "poll a semantic value or assertion callback instead of Eventually/Consistently boolean results with BeTrue/BeFalse"
}

func gomegaEqualEmptyDiagnostic() string {
	return "use BeEmpty matcher instead of Equal(empty) for empty collection/string assertions"
}

func gomegaEqualZeroDiagnostic() string {
	return "use BeZero matcher instead of Equal(0) for zero-value assertions"
}

func gomegaRepeatedFieldAssertionDiagnostic() string {
	return "compose repeated field assertions on the same value into a semantic matcher or MatchFields"
}

func gomegaCollectionIndexAssertionDiagnostic() string {
	return "assert collections with ContainElement/ConsistOf/HaveExactElements instead of positional index field assertions"
}

func gomegaNonEmptyCollectionDiagnostic() string {
	return "assert collection contents or cardinality semantics instead of only NotTo(BeEmpty())"
}

func gomegaSemanticScalarNotEmptyDiagnostic() string {
	return "assert semantic scalar value meaning instead of only checking for non-empty text"
}
