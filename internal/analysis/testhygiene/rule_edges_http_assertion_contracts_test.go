package testhygiene

import (
	"go/ast"
	"go/types"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("testhygiene HTTP assertion helper contracts", ginkgo.Label("unit"), func() {
	ginkgo.It("classifies HTTP and value-shape assertion helpers over real standard-library types", func() {
		h := newRuleHarness("/repo/internal/http/handler_test.go", "github.com/perber/wiki/internal/http", `package http

import (
	stdhttp "net/http"
	"net/http/httptest"
	"time"
)

type assertion struct{}
type pageSummary struct {
	Title string
	Path string
}
type validationIssue struct {
	Code string
	Message string
}

func Expect(actual any) assertion { return assertion{} }
func (assertion) To(matcher any, extra ...any) {}
func Equal(expected any) any { return nil }
func ContainSubstring(expected string) any { return nil }
func BeEquivalentTo(expected any) any { return nil }
func HaveHTTPHeaderWithValue(name string, value string) any { return nil }

func TestResponse(
	recorder *httptest.ResponseRecorder,
	response *stdhttp.Response,
	request *stdhttp.Request,
	summary pageSummary,
	items []pageSummary,
	validationError validationIssue,
	events chan string,
	callback func() bool,
	ready bool,
) {
	Expect(recorder.Code).To(Equal(200))
	Expect(response.StatusCode).To(Equal(200))
	Expect(recorder.Body.String()).To(ContainSubstring("created"))
	Expect(response.Header.Get("X-LeafWiki")).To(Equal("workspace"))
	Expect(request).To(HaveHTTPHeaderWithValue("X-LeafWiki", "workspace"))
	Expect(response.StatusCode).To(BeEquivalentTo(200))
	now := time.Now()
	Expect(now).To(Equal(now))
	Expect([]string{}).To(Equal([]string{}))
	Expect([]string{response.Status, response.Proto}).To(Equal([]string{"200 OK", "HTTP/2.0"}))
	Expect(map[string]string{}).To(Equal(map[string]string{}))
	Expect(summary.Title).To(Equal("Docs"))
	Expect(summary.Path).To(Equal("/docs"))
	Expect(validationError.Code).To(Equal("code"))
	Expect(validationError.Message).To(Equal("message"))
	Expect(items[0].Title).To(Equal("Docs"))
	_ = Equal(0)
	_ = Equal(false)
	_ = events
	_ = callback
	_ = ready
}
`)
		calls := h.findCalls("To")
		assertions := make([]gomegaAssertion, 0, len(calls))
		okValues := make([]bool, 0, len(calls))
		for _, call := range calls {
			assertion, ok := gomegaAssertionFromCall(h.ctx, call)
			assertions = append(assertions, assertion)
			okValues = append(okValues, ok)
		}
		equalCalls := h.findCalls("Equal")

		Expect(okValues).To(Equal([]bool{true, true, true, true, true, true, true, true, true, true, true, true, true, true, true}))
		Expect([]bool{
			assertionUsesHTTPStatusEqual(h.ctx, assertions[0]),
			assertionUsesHTTPStatusEqual(h.ctx, assertions[1]),
			assertionUsesHTTPBodyString(h.ctx, assertions[2]),
			assertionUsesHTTPHeaderGet(h.ctx, assertions[3]),
			assertionUsesResponseHeaderMatcherOnRequest(h.ctx, assertions[4]),
			assertionUsesNumericBeEquivalentTo(h.ctx, assertions[5]),
			assertionUsesTimeEqual(h.ctx, assertions[6]),
			assertionUsesEqualEmpty(assertions[7]),
			assertionUsesPositionalCompositeAssertion(assertions[8]),
			assertionUsesEqualEmpty(assertions[9]),
			assertionUsesRepeatedFieldAssertion(h.ctx, assertions[10]),
			assertionUsesRepeatedFieldAssertion(h.ctx, assertions[11]),
			assertionUsesRepeatedFieldAssertion(h.ctx, assertions[12]),
			assertionUsesCollectionIndexAssertion(h.ctx, assertions[14]),
			isEqualZeroMatcherCall(equalCalls[len(equalCalls)-2]),
			isEqualBooleanLiteralMatcherCall(equalCalls[len(equalCalls)-1]),
			eventuallyBareActualAllowed(h.ctx, lastIdentifierNamed(h.file, "events")),
			eventuallyBareActualAllowed(h.ctx, lastIdentifierNamed(h.file, "callback")),
			eventuallyBareActualAllowed(h.ctx, lastIdentifierNamed(h.file, "ready")),
		}).To(Equal([]bool{true, true, true, true, true, true, true, true, true, true, true, true, false, true, true, true, true, true, false}))

		Expect([]bool{
			isNumericType(nil),
			isNumericType(types.Typ[types.Int]),
			compositeLiteralContainsMultipleFieldSelectors(assertions[8].actual.(*ast.CompositeLit)),
		}).To(Equal([]bool{false, true, true}))
	})

	ginkgo.It("detects only adjacent repeated HTTP body matcher assertions", func() {
		h := newRuleHarness("/repo/internal/http/body_test.go", "github.com/perber/wiki/internal/http", `package http

type assertion struct{}
func Expect(actual any) assertion { return assertion{} }
func (assertion) To(matcher any, extra ...any) {}
func HaveHTTPBody(value string) any { return nil }
func noop() {}

func TestHTTPBody(response any, other any) {
	Expect(response).To(HaveHTTPBody("first"))
	Expect(response).To(HaveHTTPBody("second"))
	Expect(other).To(HaveHTTPBody("third"))
	noop()
	Expect(response).To(HaveHTTPBody("fourth"))
}
`)
		calls := h.findCalls("To")
		assertions := make([]gomegaAssertion, 0, len(calls))
		for _, call := range calls {
			assertion, ok := gomegaAssertionFromCall(h.ctx, call)
			Expect(observeHelperDecision(ok)).To(Equal(helperAccepted))
			assertions = append(assertions, assertion)
		}

		Expect(observeHelperDecisions(
			assertionUsesRepeatedHTTPBodyMatcher(h.ctx, assertions[0]),
			assertionUsesRepeatedHTTPBodyMatcher(h.ctx, assertions[1]),
			assertionUsesRepeatedHTTPBodyMatcher(h.ctx, assertions[2]),
			assertionUsesRepeatedHTTPBodyMatcher(h.ctx, assertions[3]),
		)).To(Equal([]helperDecision{
			helperRejected,
			helperAccepted,
			helperRejected,
			helperRejected,
		}))
	})
})
