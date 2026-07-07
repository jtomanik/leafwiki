package testhygiene

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("testhygiene diagnostic contracts", ginkgo.Label("unit"), func() {
	ginkgo.It("keeps Gomega diagnostic wording stable", func() {
		diagnostics := map[string]string{
			"renderedMessageContains": gomegaRenderedMessageStringsContainsDiagnostic(),
			"errorsIs":                gomegaErrorsIsMatcherDiagnostic(),
			"multiReturnError":        gomegaMultiReturnErrorMatcherDiagnostic(),
			"commaOK":                 gomegaCommaOKAssertionDiagnostic(),
			"stringPredicate":         gomegaStringPredicateMatcherDiagnostic("strings.HasPrefix", "HavePrefix"),
			"regexpMatch":             gomegaRegexpMatchStringDiagnostic(),
			"httpStatus":              gomegaHTTPStatusMatcherDiagnostic(),
			"httpBody":                gomegaHTTPBodyMatcherDiagnostic(),
			"httpHeader":              gomegaHTTPHeaderMatcherDiagnostic(),
			"httpRequestHeader":       gomegaHTTPHeaderResponseMatcherOnRequestDiagnostic(),
			"asyncCallbackExpect":     gomegaAsyncCallbackExpectDiagnostic(),
			"asyncNegativeReceive":    gomegaAsyncNegativeReceiveDiagnostic(),
			"asyncBareValue":          gomegaAsyncBareValueDiagnostic(),
			"helperOffset":            gomegaHelperOffsetDiagnostic("assertPage"),
			"numericEquivalent":       gomegaNumericEquivalentDiagnostic(),
			"timeEqual":               gomegaTimeEqualDiagnostic(),
			"positionalComposite":     gomegaPositionalCompositeAssertionDiagnostic(),
			"errorsAs":                gomegaErrorsAsMatcherDiagnostic(),
			"asyncBoolean":            gomegaAsyncBooleanMatcherDiagnostic(),
			"repeatedField":           gomegaRepeatedFieldAssertionDiagnostic(),
			"collectionIndex":         gomegaCollectionIndexAssertionDiagnostic(),
		}

		Expect(diagnostics).To(SatisfyAll(
			HaveKeyWithValue("renderedMessageContains", "do not assert rendered message text with strings.Contains; assert structured code, message ID, field, or path semantics instead"),
			HaveKeyWithValue("errorsIs", "use MatchError matcher instead of asserting errors.Is with BeTrue/BeFalse"),
			HaveKeyWithValue("multiReturnError", "use Error() or a captured error variable when asserting multi-return functions with HaveOccurred/Succeed"),
			HaveKeyWithValue("commaOK", "assert the decoded value or map contents with a semantic matcher instead of asserting comma-ok booleans"),
			HaveKeyWithValue("stringPredicate", "use HavePrefix matcher instead of asserting strings.HasPrefix with BeTrue/BeFalse"),
			HaveKeyWithValue("regexpMatch", "use MatchRegexp matcher instead of asserting regexp.MatchString with BeTrue/BeFalse"),
			HaveKeyWithValue("httpStatus", "use HaveHTTPStatus matcher instead of asserting response status fields directly"),
			HaveKeyWithValue("httpBody", "use HaveHTTPBody matcher instead of matching recorder body strings directly"),
			HaveKeyWithValue("httpHeader", "use HaveHTTPHeaderWithValue matcher instead of matching response header values directly"),
			HaveKeyWithValue("httpRequestHeader", "HaveHTTPHeaderWithValue matches HTTP responses; assert request headers with request-header semantics instead"),
			HaveKeyWithValue("asyncCallbackExpect", "use the Gomega value passed into Eventually/Consistently callbacks instead of global Expect"),
			HaveKeyWithValue("asyncNegativeReceive", "use Consistently(...).ShouldNot(Receive()) to prove a channel stays quiet"),
			HaveKeyWithValue("asyncBareValue", "wrap bare eventually-polled values in a function so polling re-reads changing state"),
			HaveKeyWithValue("helperOffset", "test assertion helper assertPage contains Gomega assertions without GinkgoHelper, WithOffset, ExpectWithOffset, or a Gomega parameter"),
			HaveKeyWithValue("numericEquivalent", "avoid BeEquivalentTo for numeric assertions; use Equal or BeNumerically"),
			HaveKeyWithValue("timeEqual", "use BeTemporally for time.Time equality assertions"),
			HaveKeyWithValue("positionalComposite", "do not compare multiple fields through a positional composite assertion; use HaveField/MatchFields or a named matcher"),
			HaveKeyWithValue("errorsAs", "assert error type semantics with MatchError/Satisfy instead of errors.As(...) with BeTrue/BeFalse"),
			HaveKeyWithValue("asyncBoolean", "poll a semantic value or assertion callback instead of Eventually/Consistently boolean results with BeTrue/BeFalse"),
			HaveKeyWithValue("repeatedField", "compose repeated field assertions on the same value into a semantic matcher or MatchFields"),
			HaveKeyWithValue("collectionIndex", "assert collections with ContainElement/ConsistOf/HaveExactElements instead of positional index field assertions"),
		))
	})

	ginkgo.It("keeps Ginkgo diagnostic wording stable", func() {
		diagnostics := map[string]string{
			"containerState": ginkgoContainerStateInitializationDiagnostic(),
			"helperFirst":    ginkgoHelperFirstDiagnostic("assertPage"),
			"booleanOutcome": ginkgoBooleanOutcomeNameDiagnostic("returns true"),
			"globalState":    ginkgoGlobalStateCleanupDiagnostic("DefaultTransport"),
		}

		Expect(diagnostics).To(SatisfyAll(
			HaveKeyWithValue("containerState", "move state initialization out of Ginkgo container body; declare variables in containers and initialize in setup nodes"),
			HaveKeyWithValue("helperFirst", "call GinkgoHelper() as the first statement in assertion helper assertPage"),
			HaveKeyWithValue("booleanOutcome", "Ginkgo node name \"returns true\" describes a boolean return value; document the observable behavior instead"),
			HaveKeyWithValue("globalState", "restore global state changes with DeferCleanup next to DefaultTransport"),
		))
	})
})
