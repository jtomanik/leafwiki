package testhygiene

import (
	"go/ast"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("testhygiene matcher value and protocol contracts", ginkgo.Label("unit"), func() {
	ginkgo.It("classifies structured protocol and raw matcher-value assertions", func() {
		h := newRuleHarness("/repo/internal/wiki/protocol_test.go", "github.com/perber/wiki/internal/wiki", `package wiki

import (
	"errors"
	"fmt"
)

type assertion struct{}
type GomegaMatcher interface{}
type WorkspaceID string
type validationResult struct {
	Code      string
	IsError   bool
	MessageID string
}
type validationIssue struct {
	Code    string
	Message string
}

func Expect(actual any) assertion { return assertion{} }
func Eventually(actual any) assertion { return assertion{} }
func (assertion) To(matcher any, extra ...any) {}
func (assertion) Should(matcher any, extra ...any) {}
func Equal(expected any) GomegaMatcher { return nil }
func BeEquivalentTo(expected any) GomegaMatcher { return nil }
func BeTrue() GomegaMatcher { return nil }
func Not(matcher any) GomegaMatcher { return nil }
func HaveField(name string, matcher any) GomegaMatcher { return nil }
func MatchFields(options any, fields map[string]any) GomegaMatcher { return nil }
func HaveKey(key any) GomegaMatcher { return nil }
func HaveKeyWithValue(key any, value any) GomegaMatcher { return nil }
func MatchJSON(raw string) GomegaMatcher { return nil }
func MatchError(expected any) GomegaMatcher { return nil }
func And(matchers ...any) GomegaMatcher { return nil }
func Or(matchers ...any) GomegaMatcher { return nil }
func ContainSubstring(value string) GomegaMatcher { return nil }
func HavePrefix(value string) GomegaMatcher { return nil }

func TestProtocolMatchers(err error, result validationResult, issue validationIssue, raw map[string]any, expectedMatcher GomegaMatcher) {
	Expect(result).To(HaveKey("messageId"))
	Expect(result).To(HaveKeyWithValue("code", Equal("bad")))
	Expect(result).To(MatchFields(nil, map[string]any{"IsError": BeTrue()}))
	Expect(result.IsError).To(Equal(true))
	Expect(result).To(MatchJSON(`+"`"+`{"messageId":"wiki.tool.failed"}`+"`"+`))
	Expect(issue.Code).To(Equal("bad"))
	Expect(issue).To(HaveField("MessageID", Equal("wiki.tool.failed")))
	Expect(err).To(MatchError(And(HavePrefix("missing"))))
	Expect(err).To(MatchError(errors.New("missing")))
	Expect(err).To(MatchError(fmt.Errorf("missing: %w", err)))
	Eventually(err).Should(MatchError(expectedMatcher))
	_ = MatchError(expectedMatcher)
	Expect("Docs").To(Equal(expectedMatcher))
	localMatcher := Equal("Docs")
	Expect("Docs").To(Equal(localMatcher))
	Expect(raw).To(HaveKeyWithValue("message", Equal("raw")))
	_ = BeEquivalentTo("raw-id")
	_ = HavePrefix("raw-prefix")
	_ = Or(Equal("raw-a"), Equal(WorkspaceID("typed")))
}
`)
		calls := h.findCalls("To")
		assertions := make([]gomegaAssertion, 0, len(calls))
		for _, call := range calls {
			assertion, ok := gomegaAssertionFromCall(h.ctx, call)
			Expect(observeHelperDecision(ok)).To(Equal(helperAccepted))
			assertions = append(assertions, assertion)
		}

		keyName, hasProtocolKey := assertionUsesStructuredProtocolKeyMatcher(h.ctx, assertions[0])
		valueKeyName, hasProtocolValueKey := assertionUsesStructuredProtocolKeyMatcher(h.ctx, assertions[1])
		statusField := assertionUsesStructuredProtocolStatusMatcher(h.ctx, assertions[2])
		directStatus := assertionUsesStructuredProtocolStatusMatcher(h.ctx, assertions[3])
		payloadMatcher, hasPayloadMatcher := assertionUsesStructuredProtocolPayloadMatcher(h.ctx, assertions[4])
		structuredField, hasStructuredField := assertionMatchesStructuredErrorField(h.ctx, assertions[5])
		structuredMatcher, hasStructuredMatcher := assertionUsesStructuredFieldMatcher(h.ctx, assertions[6])

		Expect([]string{keyName, valueKeyName, payloadMatcher, structuredField, structuredMatcher}).To(Equal(
			[]string{"messageId", "code", "MatchJSON", "Code", "MessageID"},
		))
		Expect(observeHelperDecisions(
			hasProtocolKey,
			hasProtocolValueKey,
			statusField,
			directStatus,
			hasPayloadMatcher,
			hasStructuredField,
			hasStructuredMatcher,
			assertionUsesRawStringMatchError(h.ctx, assertions[7]),
			assertionUsesRawStringMatchError(h.ctx, assertions[8]),
			assertionUsesRawStringMatchError(h.ctx, assertions[9]),
			matcherCallUsesMatcherValueAsExpected(h.ctx, assertions[10].matcher),
			matcherCallUsesMatcherValueAsExpected(h.ctx, assertions[11].matcher),
			assertionUsesStructuredProtocolKeyMatcherSucceeded(h.ctx, assertions[12]),
			callIsInsideGomegaAssertion(h.ctx, h.findCalls("MatchError")[0]),
			callIsInsideGomegaAssertion(h.ctx, h.findCalls("MatchError")[3]),
			callIsInsideGomegaAssertion(h.ctx, h.findCalls("MatchError")[4]),
			rawSemanticContractMatcherCall(h.ctx, h.findCall("BeEquivalentTo")),
			rawSemanticContractMatcherCall(h.ctx, h.findCall("HavePrefix")),
			rawSemanticContractMatcherCall(h.ctx, h.findCall("Or")),
			rawSemanticContractMatcherCall(h.ctx, &ast.CallExpr{Fun: ast.NewIdent("Equal")}),
		)).To(Equal([]helperDecision{
			helperAccepted, helperAccepted, helperAccepted, helperAccepted,
			helperAccepted, helperAccepted, helperAccepted, helperAccepted,
			helperAccepted, helperAccepted, helperAccepted, helperAccepted, helperRejected,
			helperAccepted, helperAccepted, helperRejected,
			helperAccepted, helperAccepted, helperAccepted, helperRejected,
		}))
	})

	ginkgo.It("reports global Expect inside async callbacks that receive Gomega", func() {
		h := newRuleHarness("/repo/internal/wiki/async_test.go", "github.com/perber/wiki/internal/wiki", `package wiki

type assertion struct{}
type Gomega interface {
	Expect(actual any) assertion
}

func Expect(actual any) assertion { return assertion{} }
func Eventually(actual any) assertion { return assertion{} }
func (assertion) To(matcher any, extra ...any) {}
func Equal(expected any) any { return nil }

func TestAsyncCallback() {
	Eventually(func(g Gomega) {
		Expect("global").To(Equal("global"))
		g.Expect("scoped").To(Equal("scoped"))
	}).To(Equal("done"))
}

func assertPlain(got string) {
	Expect(got).To(Equal("plain"))
}
`)
		asyncCall := h.findCall("Eventually")
		fn, ok := asyncCall.Args[0].(*ast.FuncLit)
		Expect(observeHelperDecision(ok)).To(Equal(helperAccepted))

		checkGomegaAsyncCallback(h.ctx, asyncCall)

		Expect([]bool{
			funcLitHasGomegaParam(fn),
			isGlobalGomegaExpectCall(h.findCalls("Expect")[0]),
			isGlobalGomegaExpectCall(h.findCalls("Expect")[1]),
			funcHasGomegaHelperReporting(h.ctx, h.findFunc("TestAsyncCallback")),
			funcHasGomegaHelperReporting(h.ctx, h.findFunc("assertPlain")),
			isGomegaHelperReportingCall("WithOffset"),
			isGomegaHelperReportingCall("PlainHelper"),
		}).To(Equal([]bool{true, true, false, false, false, true, false}))
		Expect(h.diagnosticMessages()).To(ConsistOf(
			"semh:gomega.async-callback-expect: use the Gomega value passed into Eventually/Consistently callbacks instead of global Expect",
		))
	})
})

func assertionUsesStructuredProtocolKeyMatcherSucceeded(ctx *analysisContext, assertion gomegaAssertion) bool {
	_, ok := assertionUsesStructuredProtocolKeyMatcher(ctx, assertion)
	return ok
}
