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

func testStableLiteralDiagnostic(value string) string {
	return fmt.Sprintf("raw stable contract literal %q used in test assertion code; use the typed constant or semantic helper", value)
}

func testRawLocalizedProseDiagnostic(value string) string {
	return fmt.Sprintf("raw localized prose %q used in test assertion code; assert a semantic code/message ID instead", value)
}

func testHelperSemanticParameterDiagnostic(funcName string, paramName string, semanticType string) string {
	return fmt.Sprintf("test helper %s parameter %s uses string for %s; use the semantic type in test helpers", funcName, paramName, semanticType)
}

func testHelperMessageParameterDiagnostic(funcName string, paramName string) string {
	return fmt.Sprintf("test helper %s parameter %s accepts rendered prose; assert MessageID/catalog semantics instead", funcName, paramName)
}

func testHelperFieldParameterDiagnostic(funcName string, paramName string) string {
	return fmt.Sprintf("test helper %s parameter %s uses string for validation field identity; use a semantic field-name type or helper constant", funcName, paramName)
}

func gomegaErrorStringMatcherDiagnostic() string {
	return "assert error values with MatchError instead of matching err.Error()"
}

func gomegaRawStringMatchErrorDiagnostic() string {
	return "assert error semantics with a typed/domain matcher or injected error value instead of raw string MatchError"
}

func dependencyDirectionDiagnostic() string {
	return "e2e-proxy must not import LeafWiki internal packages; assert protocol semantics or define local black-box test helpers"
}

func gomegaStringsContainsMatcherDiagnostic() string {
	return "use ContainSubstring matcher instead of asserting strings.Contains with BeTrue/BeFalse"
}

func gomegaRenderedMessageStringsContainsDiagnostic() string {
	return "do not assert rendered message text with strings.Contains; assert structured code, message ID, field, or path semantics instead"
}

func gomegaErrorsIsMatcherDiagnostic() string {
	return "use MatchError matcher instead of asserting errors.Is with BeTrue/BeFalse"
}

func gomegaErrorNilMatcherDiagnostic() string {
	return "use HaveOccurred matcher instead of nil assertions on error values"
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

func gomegaControlStatusMatcherDiagnostic() string {
	return "assert project daemon control errors with MatchError or a domain matcher instead of IsControlStatus with boolean matchers"
}

func gomegaStringPredicateMatcherDiagnostic(predicate string, matcher string) string {
	return fmt.Sprintf("use %s matcher instead of asserting %s with BeTrue/BeFalse", matcher, predicate)
}

func gomegaRegexpMatchStringDiagnostic() string {
	return "use MatchRegexp matcher instead of asserting regexp.MatchString with BeTrue/BeFalse"
}

func gomegaLenMatcherDiagnostic() string {
	return "use HaveLen matcher instead of asserting len() with Equal"
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

func customMatcherSemanticParameterDiagnostic(funcName string, paramName string, semanticType string) string {
	return fmt.Sprintf("custom matcher %s parameter %s uses string for %s; use the semantic type in matcher constructors", funcName, paramName, semanticType)
}

func customMatcherMessageParameterDiagnostic(funcName string, paramName string) string {
	return fmt.Sprintf("custom matcher %s parameter %s accepts rendered prose; assert MessageID/catalog semantics instead", funcName, paramName)
}

func customMatcherFieldParameterDiagnostic(funcName string, paramName string) string {
	return fmt.Sprintf("custom matcher %s parameter %s uses string for validation field identity; use a semantic field-name type or helper constant", funcName, paramName)
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

func ginkgoWideEntryDiagnostic() string {
	return "use a row struct for Ginkgo table entries with many parameters"
}

func ginkgoEntrySetupValueDiagnostic() string {
	return "Ginkgo Entry arguments are evaluated at construction time; pass stable row data instead of setup-initialized variables"
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

func ginkgoLinterRawIgnoreDiagnostic() string {
	return "raw ginkgolinter ignore comments are not allowed; fix the generic lint or use semh waivers only for waivable semantic-hygiene rules"
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
