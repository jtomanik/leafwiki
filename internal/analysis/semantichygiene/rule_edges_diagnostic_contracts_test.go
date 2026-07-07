package semantichygiene

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("semantichygiene diagnostic contracts", ginkgo.Label("unit"), func() {
	ginkgo.It("keeps fixture and test literal diagnostic wording stable", func() {
		diagnostics := map[string]string{
			"fixtureRuntime": fixtureRuntimeConstructorDiagnostic("newFixtureSlug"),
			"fieldLiteral":   testRawSemanticFieldLiteralDiagnostic("Slug", "Slug"),
		}

		Expect(diagnostics).To(SatisfyAll(
			HaveKeyWithValue("fixtureRuntime", "fixture semantic constructor newFixtureSlug receives runtime string; fixture constructors should only wrap static test values"),
			HaveKeyWithValue("fieldLiteral", "raw string literal assigned to Slug field Slug in test code; use a semantic fixture/helper value"),
		))
	})

	ginkgo.It("keeps matcher signature diagnostic wording stable", func() {
		diagnostics := map[string]string{
			"semanticParam": customMatcherSemanticParameterDiagnostic("matchPage", "slug", "Slug"),
			"fieldParam":    customMatcherFieldParameterDiagnostic("matchErrorField", "field"),
		}

		Expect(diagnostics).To(SatisfyAll(
			HaveKeyWithValue("semanticParam", "custom matcher matchPage parameter slug uses string for Slug; use the semantic type in matcher constructors"),
			HaveKeyWithValue("fieldParam", "custom matcher matchErrorField parameter field uses string for validation field identity; use a semantic field-name type or helper constant"),
		))
	})
})
