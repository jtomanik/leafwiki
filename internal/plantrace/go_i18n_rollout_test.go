package plantrace

import (
	"fmt"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"os"
	"path/filepath"
	"strings"
)

var _ = ginkgo.Describe("go-i18n rollout plan traceability", func() {
	ginkgo.It("maps every rollout scenario to repository evidence", func() {
		repoRoot := canonicalPlanRepoRoot()
		planPath := filepath.Join(repoRoot, "docs", "plans", "go-i18n-rollout.PLAN.md")
		titles := goI18nScenarioTitles(planPath)
		Expect(titles).To(SatisfyAll(
			ContainElement("Renderer resolves an English catalog message by message ID"),
			ContainElement("Plantrace maps every Gherkin scenario to evidence"),
		), "go-i18n rollout scenarios should exist")
		for _, title := range titles {
			evidence, err := goI18nRolloutEvidenceResult(title)
			Expect(err).NotTo(HaveOccurred())
			Expect(evidence).To(existInCanonicalPlanEvidenceFile(repoRoot, title))
		}

	})

	ginkgo.It("keeps CLI startup stderr evidence tied to the catalog-backed failure test", func() {
		evidence, err := goI18nRolloutEvidenceResult("CLI startup error renders catalog-backed text to stderr")
		Expect(err).NotTo(HaveOccurred())
		Expect(evidence).To(Equal(canonicalPlanEvidence{
			file: "cmd/leafwiki/main_test.go",
			text: "TestFailureMessageRendersCatalogBackedErrorBody",
		}))

	})

	ginkgo.It("keeps the catalog gate running the CLI stderr evidence test", func() {
		repoRoot := canonicalPlanRepoRoot()
		raw, err := os.ReadFile(filepath.Join(repoRoot, "scripts", "check-i18n-catalog.sh"))
		Expect(err).NotTo(HaveOccurred(), "read check-i18n-catalog.sh")
		Expect(string(raw)).To(ContainSubstring("TestFailureMessageRendersCatalogBackedErrorBody"))

	})
})

func goI18nScenarioTitles(planPath string) []string {
	ginkgo.GinkgoHelper()
	raw, err := os.ReadFile(planPath)
	Expect(err).NotTo(HaveOccurred(), "read go-i18n rollout plan")
	titles := []string{}
	for _, line := range strings.Split(string(raw), "\n") {
		if title, ok := strings.CutPrefix(line, "Scenario: "); ok {
			titles = append(titles, title)
			continue
		}
		if title, ok := strings.CutPrefix(line, "  Scenario: "); ok {
			titles = append(titles, title)
		}
	}
	return titles
}

func goI18nRolloutEvidence(title string) (canonicalPlanEvidence, bool) {
	switch title {
	case "Renderer resolves an English catalog message by message ID",
		"Renderer bridges positional args into go-i18n template data",
		"Missing catalog entry falls back at runtime but fails coverage",
		"Duplicate message ID with different English is rejected",
		"Template data mismatch preserves fallback behavior",
		"Empty message ID is not localized",
		"Extra positional args are preserved",
		"Percent templates remain compatibility data",
		"Renderer is safe for concurrent HTTP requests":
		return evidence("internal/localization/render_test.go", "EnglishRenderer"), true
	case "Shared localized error detail keeps compatibility fields",
		"API fallback preserves old clients when catalog rendering fails":
		return evidence("internal/core/shared/errors/localized_error_test.go", "LocalizedErrorDetail"), true
	case "API error response uses catalog-backed message":
		return evidence("internal/wiki/workspacesync/routes_test.go", "returns a localized structured error when snapshot listing is disabled"), true
	case "Field validation error renders from field message ID",
		"API validation error exposes field codes not prose-only assertions":
		return evidence("internal/core/shared/errors/field_error_test.go", "ValidationErrors"), true
	case "API success response includes messageId and catalog-backed message":
		return evidence("internal/wiki/pages/i18n_success_test.go", "API success message catalog rendering"), true
	case "MCP tool descriptor description renders from catalog",
		"MCP message-only output uses catalog-backed message",
		"MCP unknown tool protocol errors are not over-wrapped":
		return evidence("internal/wiki/mcp/tool_contracts_test.go", "renders descriptions from the catalog"), true
	case "MCP structured error keeps _meta.error compatibility":
		return evidence("internal/wiki/mcp/mcp_integration_test.go", "assertMCPStructuredError"), true
	case "CLI help renders catalog-backed text to stdout":
		return evidence("cmd/leafwiki/main_test.go", "catalog-backed usage line"), true
	case "run.sh help uses generated catalog text",
		"Shell wrapper redacts secrets after message generation":
		return evidence("scripts/test-run.sh", "scripts/run_messages.sh"), true
	case "CLI startup error renders catalog-backed text to stderr":
		return evidence("cmd/leafwiki/main_test.go", "TestFailureMessageRendersCatalogBackedErrorBody"), true
	case "MCP STDIO keeps stdout protocol-only":
		return evidence("scripts/test-run.sh", "native stdout was not direct protocol"), true
	case "OAuth RFC error fields remain compatible":
		return evidence("internal/wiki/oauth/service_test.go", "unsupported_response_type"), true
	case "Frontend API error mapper displays backend-rendered message":
		return evidence("ui/leafwiki-ui/src/lib/api/errors.test.ts", "uses backend-rendered message instead of translating the compatibility template"), true
	case "Login invalid credentials test asserts semantic error identity":
		return evidence("e2e/pages/LoginPage.ts", "errors.auth.invalid_credentials"), true
	case "MCP API key validation test asserts field IDs":
		return evidence("e2e/tests/mcp-api-keys.spec.ts", "data-l10n-id"), true
	case "Workspace sync banner test asserts semantic status":
		return evidence("e2e/tests/workspace-sync.spec.ts", "data-validation-code"), true
	case "Browser save success test does not assert toast prose":
		return evidence("e2e/tests/page.spec.ts", "ui.page.save.success"), true
	case "Importer success test does not assert toast prose":
		return evidence("e2e/tests/importer.spec.ts", "expectPlanStatus('Completed')"), true
	case "User-authored Markdown text remains assertable":
		return evidence("e2e/tests/page.spec.ts", "This paragraph creates a footnote reference."), true
	case "Accessibility name assertions remain allowed when text is the feature":
		return evidence("docs/i18n.md", "accessibility text when text is the behavior"), true
	case "Version conflict toast keeps existing semantic assertion pattern":
		return evidence("e2e/tests/page.spec.ts", "data-error-code', 'page_version_conflict"), true
	case "Test prose checker rejects behavior tests that assert localized copy":
		return evidence("scripts/check-i18n-catalog.sh", "E2E behavior tests must assert semantic IDs/status"), true
	case "Catalog extraction is reproducible",
		"Every emitted message ID has English catalog coverage":
		return evidence("scripts/check-i18n-catalog.sh", "goi18n extract"), true
	case "Semantic analyzer rejects raw user-facing prose in Go contracts":
		return evidence("internal/analysis/semantichygiene/testdata/src/github.com/perber/wiki/internal/analysis/semantichygiene/testdata/semanticcases/semanticcases.go", "raw localized prose"), true
	case "Plantrace maps every Gherkin scenario to evidence":
		return evidence("internal/plantrace/go_i18n_rollout_test.go", "TestGoI18nRolloutPlanScenarioEvidence"), true
	default:
		return canonicalPlanEvidence{}, false
	}
}

func goI18nRolloutEvidenceResult(title string) (canonicalPlanEvidence, error) {
	evidence, ok := goI18nRolloutEvidence(title)
	if !ok {
		return canonicalPlanEvidence{}, fmt.Errorf("scenario %q has no evidence mapping", title)
	}
	return evidence, nil
}

var _ = ginkgo.Describe("go-i18n public documentation", func() {
	ginkgo.It("preserves the English-only rollout limits and compatibility fields", func() {
		repoRoot := canonicalPlanRepoRoot()
		raw, err := os.ReadFile(filepath.Join(repoRoot, "docs", "i18n.md"))
		Expect(err).NotTo(HaveOccurred(), "read docs/i18n.md")
		content := string(raw)
		for _, want := range []string{
			"English-only",
			"does not negotiate locale",
			"Arg0",
			"scripts/run_messages.sh",
			"code`, `messageId`, rendered `message`, `template`, and `args`",
		} {
			Expect(content).To(ContainSubstring(want), "docs/i18n.md should preserve rollout limit")
		}

	})
})
