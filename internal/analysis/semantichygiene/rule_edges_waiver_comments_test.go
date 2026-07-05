package semantichygiene

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("semantichygiene waiver comments", func() {
	ginkgo.It("parses a valid rule-specific waiver with an explanation", ginkgo.Label("unit"), func() {
		h := newRuleHarness("/repo/internal/wiki/page_test.go", "example.com/p", `package p

// semh:allow ginkgo.top-level-it -- documents the package-level invariant
func TestPage() {}
`)

		waivers, diagnostics := h.ctx.collectWaivers()

		Expect(diagnostics).To(BeEmpty())
		Expect(waivers).To(ConsistOf(SatisfyAll(
			WithTransform(func(waiver parsedWaiver) ruleID { return waiver.rule }, Equal(ruleGinkgoTopLevelIt)),
			WithTransform(func(waiver parsedWaiver) string { return waiver.explanation }, Not(BeEmpty())),
			WithTransform(func(waiver parsedWaiver) int {
				return h.ctx.pass.Fset.Position(waiver.pos).Line
			}, Equal(3)),
		)))
	})

	ginkgo.It("reports a waiver that omits the required explanation delimiter", ginkgo.Label("unit"), func() {
		h := newRuleHarness("/repo/internal/wiki/page_test.go", "example.com/p", `package p

// semh:allow ginkgo.top-level-it
func TestPage() {}
`)

		waivers, diagnostics := h.ctx.collectWaivers()

		Expect(waivers).To(BeEmpty())
		Expect(diagnostics).To(ConsistOf(
			WithTransform(func(diagnostic semanticDiagnostic) ruleID { return diagnostic.rule }, Equal(ruleWaiverMissingExplanation)),
		))
	})

	ginkgo.It("reports a waiver for an unknown rule ID", ginkgo.Label("unit"), func() {
		h := newRuleHarness("/repo/internal/wiki/page_test.go", "example.com/p", `package p

// semh:allow ginkgo.made-up -- documents the package-level invariant
func TestPage() {}
`)

		waivers, diagnostics := h.ctx.collectWaivers()

		Expect(waivers).To(BeEmpty())
		Expect(diagnostics).To(ConsistOf(
			WithTransform(func(diagnostic semanticDiagnostic) ruleID { return diagnostic.rule }, Equal(ruleWaiverUnknownRule)),
		))
	})

	ginkgo.It("reports a malformed waiver without a rule ID", ginkgo.Label("unit"), func() {
		h := newRuleHarness("/repo/internal/wiki/page_test.go", "example.com/p", `package p

// semh:allow -- documents the package-level invariant
func TestPage() {}
`)

		waivers, diagnostics := h.ctx.collectWaivers()

		Expect(waivers).To(BeEmpty())
		Expect(diagnostics).To(ConsistOf(
			WithTransform(func(diagnostic semanticDiagnostic) ruleID { return diagnostic.rule }, Equal(ruleWaiverMalformed)),
		))
	})

	ginkgo.It("reports a bare semh allow directive as malformed", ginkgo.Label("unit"), func() {
		h := newRuleHarness("/repo/internal/wiki/page_test.go", "example.com/p", `package p

// semh:allow
func TestPage() {}
`)

		waivers, diagnostics := h.ctx.collectWaivers()

		Expect(waivers).To(BeEmpty())
		Expect(diagnostics).To(ConsistOf(
			WithTransform(func(diagnostic semanticDiagnostic) ruleID { return diagnostic.rule }, Equal(ruleWaiverMalformed)),
		))
	})

	ginkgo.It("reports a glued semh allow directive as malformed", ginkgo.Label("unit"), func() {
		h := newRuleHarness("/repo/internal/wiki/page_test.go", "example.com/p", `package p

// semh:allowginkgo.top-level-it -- documents the package-level invariant
func TestPage() {}
`)

		waivers, diagnostics := h.ctx.collectWaivers()

		Expect(waivers).To(BeEmpty())
		Expect(diagnostics).To(ConsistOf(
			WithTransform(func(diagnostic semanticDiagnostic) ruleID { return diagnostic.rule }, Equal(ruleWaiverMalformed)),
		))
	})

	ginkgo.It("reports semh directives that are not standalone comments as malformed", ginkgo.Label("unit"), func() {
		h := newRuleHarness("/repo/internal/wiki/page_test.go", "example.com/p", `package p

// TODO: revisit this exceptional shape semh:allow ginkgo.top-level-it -- package invariant
func TestPage() {}
`)

		waivers, diagnostics := h.ctx.collectWaivers()

		Expect(waivers).To(BeEmpty())
		Expect(diagnostics).To(ConsistOf(
			WithTransform(func(diagnostic semanticDiagnostic) ruleID { return diagnostic.rule }, Equal(ruleWaiverMalformed)),
		))
	})
})
