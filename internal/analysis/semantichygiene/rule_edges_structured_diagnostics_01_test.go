package semantichygiene

import (
	"strings"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("semantichygiene structured diagnostics", func() {
	ginkgo.It("delays rule diagnostics until finalization and prefixes the stable rule ID", ginkgo.Label("unit"), func() {
		h := newRuleHarness("/repo/internal/wiki/page_test.go", "example.com/p", `package p
func TestPage() {}
`)

		h.ctx.report(ruleDirectCast, h.file.Name, directCastDiagnostic("WorkspaceID"))
		Expect(h.diagnostics).To(BeEmpty())

		h.ctx.finalizeDiagnostics()

		Expect(h.diagnosticMessages()).To(ConsistOf(
			"semh:semantic.direct-cast: direct cast to semantic type WorkspaceID outside parser or boundary; use a parser or typed input",
		))
	})

	ginkgo.It("emits every registered rule identifier as a structured diagnostic prefix", ginkgo.Label("unit"), func() {
		type formattedDiagnosticState uint8

		const (
			formattedDiagnosticMalformed formattedDiagnosticState = iota
			formattedDiagnosticMatched
		)

		type formattedDiagnosticObservation struct {
			State   formattedDiagnosticState
			Rule    ruleID
			Message string
		}

		observeFormattedDiagnostic := func(message string) formattedDiagnosticObservation {
			rulePrefix, body, ok := strings.Cut(message, ": ")
			if !ok {
				return formattedDiagnosticObservation{State: formattedDiagnosticMalformed}
			}
			rawRule, ok := strings.CutPrefix(rulePrefix, "semh:")
			if !ok {
				return formattedDiagnosticObservation{State: formattedDiagnosticMalformed}
			}
			return formattedDiagnosticObservation{
				State:   formattedDiagnosticMatched,
				Rule:    ruleID(rawRule),
				Message: body,
			}
		}

		const diagnosticPayload = "diagnostic.payload"

		for id := range allRuleMetadata() {
			message := formatDiagnosticMessage(semanticDiagnostic{
				rule:    id,
				message: diagnosticPayload,
			})

			Expect(observeFormattedDiagnostic(message)).To(Equal(formattedDiagnosticObservation{
				State:   formattedDiagnosticMatched,
				Rule:    id,
				Message: diagnosticPayload,
			}))
		}
	})

	ginkgo.It("reports matcher factories that accept rendered error output fragments as i18n message parameters", ginkgo.Label("unit"), func() {
		h := newRuleHarness("/repo/e2e/cmd/wikid-store/main_test.go", "github.com/perber/wiki/e2e/cmd/wikid-store", `package main

type GomegaMatcher interface{}

func WithTransform(transform any, matcher any) GomegaMatcher { return nil }
func Equal(expected any) GomegaMatcher { return nil }

func haveWikidStoreErrorContext(context string) GomegaMatcher {
	return WithTransform(func(raw string) string {
		return parseFatalOutput(raw, context)
	}, Equal(context))
}

func parseFatalOutput(raw string, context string) string {
	return context
}
`)
		checkSignature(h.ctx, h.findFunc("haveWikidStoreErrorContext"))

		Expect(h.diagnosticMessages()).To(ConsistOf(
			"semh:i18n.message-parameter: custom matcher haveWikidStoreErrorContext parameter context accepts rendered prose; assert MessageID/catalog semantics instead",
		))
	})
})
