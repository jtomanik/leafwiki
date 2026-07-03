package plantrace

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"os"
	"path/filepath"
	"strings"
)

type markdownLinkRootPrefixEvidence struct {
	file string
	text string
}

type markdownLinkRootPrefixScenarioCoverage struct {
	title    string
	evidence markdownLinkRootPrefixEvidence
}

var markdownLinkRootPrefixPlanScenarioCoverage = []markdownLinkRootPrefixScenarioCoverage{
	{"Prefixed absolute page link resolves inside wiki root", mlrpEvidence("internal/core/markdownlinks/markdownlinks_test.go", "TestResolveCanonicalLink_WithRootPrefixResolvesPageInsideWikiRoot")},
	{"Unprefixed absolute page link still resolves but canonicalizes to the configured prefix", mlrpEvidence("internal/workspacesync/service_test.go", "ServiceSyncNowCanonicalizesAbsoluteLinksWithMarkdownLinkRootPrefix")},
	{"Configured prefix root resolves to the wiki root section", mlrpEvidence("internal/core/markdownlinks/markdownlinks_test.go", "TestResolveCanonicalLink_WithRootPrefixResolvesPrefixRootToWikiRoot")},
	{"Configured prefix distinguishes section and page syntax", mlrpEvidence("internal/core/markdownlinks/markdownlinks_test.go", "TestResolveCanonicalLink_WithRootPrefixDistinguishesSectionAndPageSyntax")},
	{"Relative links ignore the configured prefix", mlrpEvidence("internal/core/markdownlinks/markdownlinks_test.go", "TestResolveCanonicalLink_WithRootPrefixLeavesRelativeLinkUnchanged")},
	{"External and hash links ignore the configured prefix", mlrpEvidence("internal/core/markdownlinks/markdownlinks_test.go", "TestResolveCanonicalLink_WithRootPrefixLeavesExternalAndHashLinksUnchanged")},
	{"Prefixed asset links resolve as workspace assets", mlrpEvidence("internal/core/markdownvalidation/use_cases_test.go", "resolves prefixed assets through the markdown link root prefix")},
	{"Workspace sync coerces absolute links to the configured prefix", mlrpEvidence("e2e/tests/workspace-sync.spec.ts", "markdown link root prefix rewrites unprefixed absolute links")},
	{"Generated editor links include the configured prefix", mlrpEvidence("e2e/tests/page.spec.ts", "markdown link root prefix autocomplete inserts prefixed page links")},
	{"Importer and refactor generated absolute links include the configured prefix", mlrpEvidence("internal/importer/content_transformer_test.go", "TestContentTransformer_UsesMarkdownLinkRootPrefixForGeneratedPageLinks")},
	{"CLI env YAML and run wrapper expose the same prefix setting", mlrpEvidence("cmd/leafwiki/main_test.go", "TestApplyYAMLConfigFile_ResolutionPrecedenceAndExplicitScalars")},
	{"Daemon identity changes when markdown link root prefix changes", mlrpEvidence("cmd/leafwiki/main_test.go", "TestCompareProjectDaemonConfigForRequestCoversDaemonRelevantFields")},
	{"MCP and HTTP config report markdownLinkRootPrefix", mlrpEvidence("internal/wiki/mcp/mcp_integration_test.go", "config[\"markdownLinkRootPrefix\"]")},
	{"LeafWiki preview navigates prefixed Markdown hrefs without changing route identity", mlrpEvidence("e2e/tests/page.spec.ts", "markdown link root prefix preview click navigates to unprefixed route")},
	{"Base path and markdown link root prefix remain separate", mlrpEvidence("e2e/tests/page.spec.ts", "markdown link root prefix remains separate from base path")},
	{"Plan scenarios are covered by automated evidence", mlrpEvidence("internal/plantrace/markdown_link_root_prefix_test.go", "TestMarkdownLinkRootPrefixPlanScenarioTitleAuditIndex")},
}

var markdownLinkRootPrefixPlanExtraEvidence = map[string][]markdownLinkRootPrefixEvidence{
	"Importer and refactor generated absolute links include the configured prefix": {
		mlrpEvidence("internal/links/link_refactor_test.go", "TestMarkdownRefactorEngine_UsesMarkdownLinkRootPrefixForPrefixedAbsoluteInput"),
	},
	"CLI env YAML and run wrapper expose the same prefix setting": {
		mlrpEvidence("scripts/test-run.sh", "native prefix dry-run"),
		mlrpEvidence("scripts/test-run.sh", "native env prefix dry-run"),
	},
}

func mlrpEvidence(file string, text string) markdownLinkRootPrefixEvidence {
	return markdownLinkRootPrefixEvidence{file: file, text: text}
}

var _ = ginkgo.Describe("markdown link root prefix plan traceability", func() {
	ginkgo.It("maps every plan scenario title to automated evidence", func() {
		repoRoot := markdownLinkRootPrefixPlanRepoRoot()
		planTitles := markdownLinkRootPrefixPlanScenarioTitles(filepath.Join(repoRoot, "docs", "plans", "markdown-link-root-prefix.PLAN.md"))
		Expect(planTitles).To(HaveLen(len(markdownLinkRootPrefixPlanScenarioCoverage)), "plan scenarios should match mapped evidence")

		coverageByTitle := map[string]markdownLinkRootPrefixEvidence{}
		for _, coverage := range markdownLinkRootPrefixPlanScenarioCoverage {
			Expect(coverage).To(haveMarkdownLinkRootPrefixScenarioMapping())
			Expect(coverageByTitle).NotTo(HaveKey(coverage.title), "duplicate scenario evidence title %q", coverage.title)
			coverageByTitle[coverage.title] = coverage.evidence
		}

		for _, title := range planTitles {
			evidence, err := markdownLinkRootPrefixEvidenceForTitle(coverageByTitle, title)
			Expect(err).NotTo(HaveOccurred())
			Expect(evidence).To(existInMarkdownLinkRootPrefixEvidenceFile(repoRoot, title))
			for _, extraEvidence := range markdownLinkRootPrefixExtraEvidenceForTitle(title) {
				Expect(extraEvidence).To(existInMarkdownLinkRootPrefixEvidenceFile(repoRoot, title))
			}
		}
		for title := range coverageByTitle {
			Expect(planTitles).To(ContainElement(title), "scenario evidence %q is not present in the plan", title)
		}

	})
})

var _ = ginkgo.Describe("markdown link root prefix focused E2E commands", func() {
	ginkgo.It("sets the markdown link root prefix environment for every focused command", func() {
		repoRoot := markdownLinkRootPrefixPlanRepoRoot()
		raw, err := os.ReadFile(filepath.Join(repoRoot, "docs", "plans", "markdown-link-root-prefix.PLAN.md"))
		Expect(err).NotTo(HaveOccurred(), "read markdown link root prefix plan")

		focusedCommands := markdownLinkRootPrefixFocusedCommandLines(string(raw))
		for _, line := range focusedCommands {
			Expect(line).To(ContainSubstring("E2E_MARKDOWN_LINK_ROOT_PREFIX=/docs"), "focused E2E command should set markdown link root prefix")
		}
		Expect(focusedCommands).NotTo(BeEmpty(), "focused markdown link root prefix E2E commands should exist")

	})

	ginkgo.It("keeps a focused base-path separation command unskipped", func() {
		repoRoot := markdownLinkRootPrefixPlanRepoRoot()
		raw, err := os.ReadFile(filepath.Join(repoRoot, "docs", "plans", "markdown-link-root-prefix.PLAN.md"))
		Expect(err).NotTo(HaveOccurred(), "read markdown link root prefix plan")

		Expect(strings.Split(string(raw), "\n")).To(ContainElement(SatisfyAll(
			ContainSubstring("./e2e/run.sh tests/page.spec.ts"),
			ContainSubstring(`--grep "markdown link root prefix remains separate from base path"`),
			ContainSubstring("E2E_MARKDOWN_LINK_ROOT_PREFIX=/docs"),
			ContainSubstring("E2E_BASE_PATH=/wiki"),
		)), "plan should run the base-path separation scenario without skipping")

	})

	ginkgo.It("keeps separate-root E2E commands tied to the separate-root environment", func() {
		repoRoot := markdownLinkRootPrefixPlanRepoRoot()
		raw, err := os.ReadFile(filepath.Join(repoRoot, "docs", "plans", "markdown-link-root-prefix.PLAN.md"))
		Expect(err).NotTo(HaveOccurred(), "read markdown link root prefix plan")

		Expect(strings.Split(string(raw), "\n")).To(ContainElement(SatisfyAll(
			ContainSubstring("./e2e/run.sh tests/root-dir.spec.ts"),
			ContainSubstring(`--grep "markdown link root prefix"`),
			ContainSubstring("E2E_MARKDOWN_LINK_ROOT_PREFIX=/docs"),
			ContainSubstring("E2E_ENABLE_SEPARATE_ROOT_DIR=1"),
		)), "root-dir command should set separate-root markdown link root prefix env")

	})

	ginkgo.It("keeps MCP commands on the default workspace sync path", func() {
		repoRoot := markdownLinkRootPrefixPlanRepoRoot()
		raw, err := os.ReadFile(filepath.Join(repoRoot, "docs", "plans", "markdown-link-root-prefix.PLAN.md"))
		Expect(err).NotTo(HaveOccurred(), "read markdown link root prefix plan")

		Expect(strings.Split(string(raw), "\n")).To(ContainElement(SatisfyAll(
			ContainSubstring("./e2e/run.sh tests/mcp-agent-context.spec.ts"),
			ContainSubstring(`--grep "markdown link root prefix"`),
			ContainSubstring("E2E_MARKDOWN_LINK_ROOT_PREFIX=/docs"),
			ContainSubstring("E2E_ENABLE_MCP_LOCAL=1"),
			Not(ContainSubstring("E2E_ENABLE_WORKSPACE_SYNC=1")),
		)), "MCP command should use default workspace sync without legacy workspace sync env")

	})
})

var _ = ginkgo.Describe("markdown link root prefix documentation", func() {
	ginkgo.It("keeps MCP route paths separate from Markdown href examples", func() {
		repoRoot := markdownLinkRootPrefixPlanRepoRoot()
		docChecks := []struct {
			path     string
			required string
		}{
			{
				path:     filepath.Join("docs", "mcp.md"),
				required: "Markdown hrefs may include `/docs/`",
			},
			{
				path:     filepath.Join("docs", "agent-collaboration.md"),
				required: `{"tool":"wiki_get_page_by_path","arguments":{"path":"api"}}`,
			},
		}

		for _, docCheck := range docChecks {
			raw, err := os.ReadFile(filepath.Join(repoRoot, docCheck.path))
			Expect(err).NotTo(HaveOccurred(), "read %s", docCheck.path)
			docs := string(raw)
			for _, routeExample := range []string{
				`{"tool":"wiki_validate_page","arguments":{"path":"/docs/api"}`,
				`{"tool":"wiki_get_page_by_path","arguments":{"path":"/docs/api"}`,
				`{"tool":"wiki_update_page_metadata","arguments":{"path":"/docs/api"}`,
				`{"tool":"wiki_replace_page_section","arguments":{"path":"/docs/api"}`,
			} {
				Expect(docs).NotTo(ContainSubstring(routeExample), "%s route-oriented example should not use markdown href prefix", docCheck.path)
			}
			Expect(docs).To(ContainSubstring(docCheck.required), "%s should contain route-prefix separation evidence", docCheck.path)
		}

	})

	ginkgo.It("documents the public README configuration surface", func() {
		repoRoot := markdownLinkRootPrefixPlanRepoRoot()
		raw, err := os.ReadFile(filepath.Join(repoRoot, "README.md"))
		Expect(err).NotTo(HaveOccurred(), "read root README")
		readme := string(raw)

		for _, required := range []string{
			"`--markdown-link-root-prefix`",
			"`LEAFWIKI_MARKDOWN_LINK_ROOT_PREFIX`",
			"markdown-link-root-prefix: /docs",
			"does not change LeafWiki route paths, API paths, MCP path inputs, or HTTP base paths",
		} {
			Expect(readme).To(ContainSubstring(required), "root README should document markdown link root prefix config surface")
		}

	})

	ginkgo.It("keeps the tracked LLM wiki skill artifact installable", func() {
		repoRoot := markdownLinkRootPrefixPlanRepoRoot()
		raw, err := os.ReadFile(filepath.Join(repoRoot, "skills", "llmwiki", "SKILL.md"))
		Expect(err).NotTo(HaveOccurred(), "read tracked llmwiki SKILL.md")
		Expect(string(raw)).To(HavePrefix("---\nname: llmwiki\n"))

	})
})

func markdownLinkRootPrefixExtraEvidenceForTitle(title string) []markdownLinkRootPrefixEvidence {
	return markdownLinkRootPrefixPlanExtraEvidence[title]
}

func markdownLinkRootPrefixPlanRepoRoot() string {
	ginkgo.GinkgoHelper()
	file, err := plantraceSourceFile()
	Expect(err).NotTo(HaveOccurred(), "runtime.Caller should locate the markdown link root prefix test")
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func markdownLinkRootPrefixPlanScenarioTitles(planPath string) []string {
	ginkgo.GinkgoHelper()
	raw, err := os.ReadFile(planPath)
	Expect(err).NotTo(HaveOccurred(), "read markdown link root prefix plan")
	titles := []string{}
	for _, line := range strings.Split(string(raw), "\n") {
		title, ok := strings.CutPrefix(line, "  Scenario: ")
		if ok {
			titles = append(titles, title)
		}
	}
	return titles
}
