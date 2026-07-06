package testhygiene

import (
	"go/types"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("testhygiene CRAP contracts", ginkgo.Label("unit"), func() {
	ginkgo.It("reports assertions hidden inside Ginkgo container construction expressions", func() {
		h := newRuleHarness("/repo/internal/branding/branding_test.go", "github.com/perber/wiki/internal/branding", `package branding

type assertion struct{}
func Expect(actual any) assertion { return assertion{} }
func (assertion) To(matcher any) any { return nil }
func BeTrue() any { return nil }
func buildContainer(args ...any) any { return nil }

var _ = buildContainer(Expect(true).To(BeTrue()))
`)

		reportDisallowedContainerCall(h.ctx, h.findCall("buildContainer"))

		Expect(h.diagnosticMessages()).To(ConsistOf(
			"semh:ginkgo.container-call: move Expect out of Ginkgo container body; containers should only declare specs and setup nodes",
		))
	})

	ginkgo.It("recognizes whether a node is enclosed by a named cleanup call", func() {
		h := newRuleHarness("/repo/internal/branding/branding_test.go", "github.com/perber/wiki/internal/branding", `package branding

func DeferCleanup(cleanup any) {}
func mutateGlobalState() {}

func TestGlobalState() {
	DeferCleanup(func() {
		mutateGlobalState()
	})
	mutateGlobalState()
}
`)

		calls := h.findCalls("mutateGlobalState")

		Expect(nodeIsInsideCallNamed(h.ctx, calls[0], "DeferCleanup")).To(BeTrue())
		Expect(nodeIsInsideCallNamed(h.ctx, calls[1], "DeferCleanup")).To(BeFalse())
	})

	ginkgo.It("reports reusable assertion helpers that should become matchers", func() {
		h := newRuleHarness("/repo/internal/branding/branding_test.go", "github.com/perber/wiki/internal/branding", `package branding

type assertion struct{}
type Gomega interface {
	Expect(actual any) assertion
}
func (assertion) To(matcher any, extra ...any) {}
func Equal(expected any) any { return nil }

func assertBrandState(g Gomega, got string, want string) {
	g.Expect(got).To(Equal(want))
}

func TestBrandOne(g Gomega) {
	assertBrandState(g, "LeafWiki", "LeafWiki")
}

func TestBrandTwo(g Gomega) {
	assertBrandState(g, "LeafWiki", "LeafWiki")
}
`)

		checkReusableAssertionHelper(h.ctx, h.findFunc("assertBrandState"))

		Expect(h.diagnosticMessages()).To(ConsistOf(
			"semh:gomega.helper-should-be-matcher: prefer a custom Gomega matcher for reusable assertion helper assertBrandState",
		))
	})

	ginkgo.It("reports positional transform matchers built from field tuples", func() {
		h := newRuleHarness("/repo/internal/wiki/page_test.go", "github.com/perber/wiki/internal/wiki", `package wiki

type pageRecord struct {
	Page struct {
		ID string
	}
	Title string
}
func WithTransform(transform any, matcher any) any { return nil }
func Equal(expected any) any { return nil }

func matchPageIdentity() any {
	return WithTransform(func(record pageRecord) []string {
		return []string{record.Page.ID, record.Title}
	}, Equal([]string{"docs", "Docs"}))
}
`)

		checkGomegaSemanticMatcher(h.ctx, h.findCall("WithTransform"))

		Expect(h.diagnosticMessages()).To(ConsistOf(
			"semh:gomega.positional-transform: do not collapse multiple fields into a positional WithTransform assertion; use HaveField/MatchFields or a named matcher",
		))
	})

	ginkgo.It("reports matcher values reused as equality expectations through local aliases", func() {
		h := newRuleHarness("/repo/internal/wiki/page_test.go", "github.com/perber/wiki/internal/wiki", `package wiki

func Equal(expected any) any { return nil }

func TestMatcherValue() {
	var expected = Equal("Docs")
	_ = Equal(expected)
}
`)
		calls := h.findCalls("Equal")

		checkGomegaSemanticMatcher(h.ctx, calls[len(calls)-1])

		Expect(h.diagnosticMessages()).To(ConsistOf(
			"semh:gomega.matcher-as-value: do not pass a Gomega matcher as an expected value to Equal; compose or apply the matcher directly",
		))
	})

	ginkgo.It("reports matcher factories that match generic tool error args with raw string fragments", func() {
		h := newRuleHarness("/repo/internal/wiki/mcp/tools_test.go", "github.com/perber/wiki/internal/wiki/mcp", `package mcp

type GomegaMatcher interface{}
func HaveField(name string, matcher any) GomegaMatcher { return nil }
func ContainSubstring(needle string) GomegaMatcher { return nil }

func matchToolArgumentFailure() GomegaMatcher {
	return HaveField("Args", ContainSubstring("missing tool name"))
}
`)

		checkGomegaMatcherFactoryGenericToolErrorArgs(h.ctx, h.findFunc("matchToolArgumentFailure"))

		Expect(h.diagnosticMessages()).To(ConsistOf(
			"semh:gomega.generic-have-occurred: assert expected error semantics with MatchError or a domain matcher instead of generic HaveOccurred",
		))
	})

	ginkgo.It("reports raw error prose inside MatchError matcher contracts", func() {
		h := newRuleHarness("/repo/internal/wiki/page_test.go", "github.com/perber/wiki/internal/wiki", `package wiki

import (
	"errors"
	"fmt"
)

type assertion struct{}
func Expect(actual any) assertion { return assertion{} }
func (assertion) To(matcher any, extra ...any) {}
func MatchError(expected any) any { return nil }
func ContainSubstring(needle string) any { return nil }

func TestErrors(err error) {
	Expect(err).To(MatchError("missing page"))
	Expect(err).To(MatchError(ContainSubstring("missing page")))
	Expect(err).To(MatchError(errors.New("missing page")))
	Expect(err).To(MatchError(fmt.Errorf("missing page: %w", err)))
}
`)

		for _, call := range h.findCalls("To") {
			checkGomegaSemanticMatcher(h.ctx, call)
		}

		Expect(h.diagnosticMessages()).To(ConsistOf(
			"semh:gomega.raw-string-match-error: assert error semantics with a typed/domain matcher or injected error value instead of raw string MatchError",
			"semh:gomega.raw-string-match-error: assert error semantics with a typed/domain matcher or injected error value instead of raw string MatchError",
			"semh:gomega.raw-string-match-error: assert error semantics with a typed/domain matcher or injected error value instead of raw string MatchError",
			"semh:gomega.raw-string-match-error: assert error semantics with a typed/domain matcher or injected error value instead of raw string MatchError",
		))
	})

	ginkgo.It("reports repeated HTTP body matchers on the same response", func() {
		h := newRuleHarness("/repo/internal/http/handler_test.go", "github.com/perber/wiki/internal/http", `package http

type assertion struct{}
func Expect(actual any) assertion { return assertion{} }
func (assertion) To(matcher any, extra ...any) {}
func HaveHTTPBody(body string) any { return nil }

func TestResponseBody(response any) {
	Expect(response).To(HaveHTTPBody("created"))
	Expect(response).To(HaveHTTPBody("id"))
}
`)
		calls := h.findCalls("To")

		checkGomegaSemanticMatcher(h.ctx, calls[1])

		Expect(h.diagnosticMessages()).To(ConsistOf(
			"semh:gomega.repeated-http-body: compose repeated HaveHTTPBody assertions for the same response into one matcher",
		))
	})

	ginkgo.It("reports structured protocol payload matchers that assert raw message keys", func() {
		h := newRuleHarness("/repo/internal/wiki/mcp/tools_test.go", "github.com/perber/wiki/internal/wiki/mcp", `package mcp

type assertion struct{}
func Expect(actual any) assertion { return assertion{} }
func (assertion) To(matcher any, extra ...any) {}
func MatchJSON(raw string) any { return nil }

func TestToolResult(toolResult any) {
	Expect(toolResult).To(MatchJSON(`+"`"+`{"messageId":"wiki.tool.failed"}`+"`"+`))
}
`)

		checkGomegaSemanticMatcher(h.ctx, h.findCall("To"))

		Expect(h.diagnosticMessages()).To(ConsistOf(
			"semh:gomega.structured-protocol-payload: assert structured protocol semantics with a typed domain matcher/helper instead of matching raw MatchJSON payload directly",
		))
	})

	ginkgo.It("reports map index aliases from multi-value assignments and var specs", func() {
		h := newRuleHarness("/repo/internal/http/router_test.go", "github.com/perber/wiki/internal/http", `package http

type assertion struct{}
func Expect(actual any) assertion { return assertion{} }
func (assertion) To(matcher any, extra ...any) {}
func Equal(expected any) any { return nil }

func TestRouterResponse() {
	response := map[string]any{"content": "Root README", "status": 200, "title": "Docs"}
	first, second := response["content"], response["status"]
	_ = first
	Expect(second).To(Equal(200))
	var title = response["title"]
	Expect(title).To(Equal("Docs"))
}
`)

		for _, call := range h.findCalls("To") {
			checkGomegaSemanticMatcher(h.ctx, call)
		}

		Expect(h.diagnosticMessages()).To(ConsistOf(
			"semh:gomega.map-index: use HaveKeyWithValue matcher instead of asserting a direct map index value",
			"semh:gomega.map-index: use HaveKeyWithValue matcher instead of asserting a direct map index value",
		))
	})

	ginkgo.It("reports semantic scalar empty-string assertions", func() {
		h := newRuleHarness("/repo/internal/session/session_test.go", "github.com/perber/wiki/internal/session", `package session

type SessionToken string
type assertion struct{}
func Expect(actual any) assertion { return assertion{} }
func (assertion) NotTo(matcher any, extra ...any) {}
func Equal(expected any) any { return nil }

func TestSession() {
	session := struct{ ContextToken SessionToken }{}
	Expect(session.ContextToken).NotTo(Equal(""))
}
`)

		checkGomegaSemanticMatcher(h.ctx, h.findCall("NotTo"))

		Expect(h.diagnosticMessages()).To(ConsistOf(
			"semh:gomega.equal-empty: use BeEmpty matcher instead of Equal(empty) for empty collection/string assertions",
			"semh:gomega.semantic-scalar-not-empty: assert semantic scalar value meaning instead of only checking for non-empty text",
		))
	})

	ginkgo.It("allows bare Eventually values only for pollable types", func() {
		channelType := types.NewChan(types.SendRecv, types.Typ[types.String])
		signatureType := types.NewSignatureType(nil, nil, nil, nil, nil, false)

		Expect([]bool{
			eventuallyBareTypeAllowed(nil),
			eventuallyBareTypeAllowed(types.Typ[types.String]),
			eventuallyBareTypeAllowed(channelType),
			eventuallyBareTypeAllowed(signatureType),
		}).To(Equal([]bool{false, false, true, true}))
	})
})
