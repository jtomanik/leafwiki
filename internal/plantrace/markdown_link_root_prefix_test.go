package plantrace

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	"os"
	"path/filepath"
	"runtime"
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
	{"Prefixed asset links resolve as workspace assets", mlrpEvidence("internal/core/markdownvalidation/use_cases_test.go", "TestValidateMarkdownContent_ResolvesPrefixedAssetWithMarkdownLinkRootPrefix")},
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

var _ = ginkgo.It("TestMarkdownLinkRootPrefixPlanScenarioTitleAuditIndex", func() {
	t := ginkgo.GinkgoT()
	repoRoot := markdownLinkRootPrefixPlanRepoRoot(t)
	planTitles := markdownLinkRootPrefixPlanScenarioTitles(t, filepath.Join(repoRoot, "docs", "plans", "markdown-link-root-prefix.PLAN.md"))
	if len(planTitles) != len(markdownLinkRootPrefixPlanScenarioCoverage) {
		t.Fatalf("plan scenario title count = %d, want %d mapped scenarios", len(planTitles), len(markdownLinkRootPrefixPlanScenarioCoverage))
	}

	coverageByTitle := map[string]markdownLinkRootPrefixEvidence{}
	for _, coverage := range markdownLinkRootPrefixPlanScenarioCoverage {
		if coverage.title == "" {
			t.Fatalf("scenario title must not be empty")
		}
		if _, ok := coverageByTitle[coverage.title]; ok {
			t.Fatalf("duplicate scenario coverage title %q", coverage.title)
		}
		if coverage.evidence.file == "" || coverage.evidence.text == "" {
			t.Fatalf("scenario %q has empty evidence: %#v", coverage.title, coverage.evidence)
		}
		coverageByTitle[coverage.title] = coverage.evidence
	}

	planTitleSet := map[string]struct{}{}
	for _, title := range planTitles {
		planTitleSet[title] = struct{}{}
		evidence, ok := coverageByTitle[title]
		if !ok {
			t.Fatalf("plan scenario %q has no automated-test evidence mapping", title)
		}
		assertMarkdownLinkRootPrefixEvidenceExists(t, repoRoot, title, evidence)
		for _, extraEvidence := range markdownLinkRootPrefixExtraEvidenceForTitle(title) {
			assertMarkdownLinkRootPrefixEvidenceExists(t, repoRoot, title, extraEvidence)
		}
	}
	for title := range coverageByTitle {
		if _, ok := planTitleSet[title]; !ok {
			t.Fatalf("scenario coverage %q is not present in the plan", title)
		}
	}

})

var _ = ginkgo.It("TestMarkdownLinkRootPrefixFocusedE2ECommandsSetFeatureEnv", func() {
	t := ginkgo.GinkgoT()
	repoRoot := markdownLinkRootPrefixPlanRepoRoot(t)
	raw, err := os.ReadFile(filepath.Join(repoRoot, "docs", "plans", "markdown-link-root-prefix.PLAN.md"))
	if err != nil {
		t.Fatalf("read markdown link root prefix plan: %v", err)
	}

	found := 0
	for _, line := range strings.Split(string(raw), "\n") {
		if !strings.Contains(line, "./e2e/run.sh tests/") || !strings.Contains(line, `--grep "markdown link root prefix"`) {
			continue
		}
		found++
		if !strings.Contains(line, "E2E_MARKDOWN_LINK_ROOT_PREFIX=/docs") {
			t.Fatalf("focused E2E command does not set E2E_MARKDOWN_LINK_ROOT_PREFIX=/docs: %s", line)
		}
	}
	if found == 0 {
		t.Fatal("no focused markdown link root prefix E2E commands found in plan")
	}

})

var _ = ginkgo.It("TestMarkdownLinkRootPrefixPlanIncludesBasePathSeparationE2ECommand", func() {
	t := ginkgo.GinkgoT()
	repoRoot := markdownLinkRootPrefixPlanRepoRoot(t)
	raw, err := os.ReadFile(filepath.Join(repoRoot, "docs", "plans", "markdown-link-root-prefix.PLAN.md"))
	if err != nil {
		t.Fatalf("read markdown link root prefix plan: %v", err)
	}

	for _, line := range strings.Split(string(raw), "\n") {
		if strings.Contains(line, "./e2e/run.sh tests/page.spec.ts") &&
			strings.Contains(line, `--grep "markdown link root prefix remains separate from base path"`) &&
			strings.Contains(line, "E2E_MARKDOWN_LINK_ROOT_PREFIX=/docs") &&
			strings.Contains(line, "E2E_BASE_PATH=/wiki") {
			return
		}
	}
	t.Fatal("plan lacks a focused E2E command that runs the base-path separation scenario without skipping")

})

var _ = ginkgo.It("TestMarkdownLinkRootPrefixPlanIncludesSeparateRootE2EEnv", func() {
	t := ginkgo.GinkgoT()
	repoRoot := markdownLinkRootPrefixPlanRepoRoot(t)
	raw, err := os.ReadFile(filepath.Join(repoRoot, "docs", "plans", "markdown-link-root-prefix.PLAN.md"))
	if err != nil {
		t.Fatalf("read markdown link root prefix plan: %v", err)
	}

	for _, line := range strings.Split(string(raw), "\n") {
		if strings.Contains(line, "./e2e/run.sh tests/root-dir.spec.ts") &&
			strings.Contains(line, `--grep "markdown link root prefix"`) &&
			strings.Contains(line, "E2E_MARKDOWN_LINK_ROOT_PREFIX=/docs") &&
			strings.Contains(line, "E2E_ENABLE_SEPARATE_ROOT_DIR=1") {
			return
		}
	}
	t.Fatal("plan lacks E2E_ENABLE_SEPARATE_ROOT_DIR=1 on the root-dir markdown link root prefix command")

})

var _ = ginkgo.It("TestMarkdownLinkRootPrefixPlanUsesDefaultWorkspaceSyncE2E", func() {
	t := ginkgo.GinkgoT()
	repoRoot := markdownLinkRootPrefixPlanRepoRoot(t)
	raw, err := os.ReadFile(filepath.Join(repoRoot, "docs", "plans", "markdown-link-root-prefix.PLAN.md"))
	if err != nil {
		t.Fatalf("read markdown link root prefix plan: %v", err)
	}

	for _, line := range strings.Split(string(raw), "\n") {
		if strings.Contains(line, "./e2e/run.sh tests/mcp-agent-context.spec.ts") &&
			strings.Contains(line, `--grep "markdown link root prefix"`) &&
			strings.Contains(line, "E2E_MARKDOWN_LINK_ROOT_PREFIX=/docs") &&
			strings.Contains(line, "E2E_ENABLE_MCP_LOCAL=1") &&
			!strings.Contains(line, "E2E_ENABLE_WORKSPACE_SYNC=1") {
			return
		}
	}
	t.Fatal("plan should run the MCP markdown link root prefix command with default workspace sync and without E2E_ENABLE_WORKSPACE_SYNC=1")

})

var _ = ginkgo.It("TestMarkdownLinkRootPrefixMCPDocsKeepRoutePathsSeparateFromMarkdownHrefs", func() {
	t := ginkgo.GinkgoT()
	repoRoot := markdownLinkRootPrefixPlanRepoRoot(t)
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
		if err != nil {
			t.Fatalf("read %s: %v", docCheck.path, err)
		}
		docs := string(raw)
		for _, routeExample := range []string{
			`{"tool":"wiki_validate_page","arguments":{"path":"/docs/api"}`,
			`{"tool":"wiki_get_page_by_path","arguments":{"path":"/docs/api"}`,
			`{"tool":"wiki_update_page_metadata","arguments":{"path":"/docs/api"}`,
			`{"tool":"wiki_replace_page_section","arguments":{"path":"/docs/api"}`,
		} {
			if strings.Contains(docs, routeExample) {
				t.Fatalf("%s route-oriented example still uses markdown href prefix: %s", docCheck.path, routeExample)
			}
		}
		if !strings.Contains(docs, docCheck.required) {
			t.Fatalf("%s does not contain expected route-prefix separation evidence %q", docCheck.path, docCheck.required)
		}
	}

})

var _ = ginkgo.It("TestMarkdownLinkRootPrefixRootReadmeDocumentsPublicConfigSurface", func() {
	t := ginkgo.GinkgoT()
	repoRoot := markdownLinkRootPrefixPlanRepoRoot(t)
	raw, err := os.ReadFile(filepath.Join(repoRoot, "README.md"))
	if err != nil {
		t.Fatalf("read root README: %v", err)
	}
	readme := string(raw)

	for _, required := range []string{
		"`--markdown-link-root-prefix`",
		"`LEAFWIKI_MARKDOWN_LINK_ROOT_PREFIX`",
		"markdown-link-root-prefix: /docs",
		"does not change LeafWiki route paths, API paths, MCP path inputs, or HTTP base paths",
	} {
		if !strings.Contains(readme, required) {
			t.Fatalf("root README does not document markdown link root prefix config surface: missing %q", required)
		}
	}

})

var _ = ginkgo.It("TestRepoLocalLLMWikiSkillArtifactExposesInstallableSkill", func() {
	t := ginkgo.GinkgoT()
	repoRoot := markdownLinkRootPrefixPlanRepoRoot(t)
	raw, err := os.ReadFile(filepath.Join(repoRoot, "skills", "llmwiki", "SKILL.md"))
	if err != nil {
		t.Fatalf("read tracked llmwiki SKILL.md: %v", err)
	}
	if !strings.HasPrefix(string(raw), "---\nname: llmwiki\n") {
		t.Fatalf("tracked llmwiki skill does not expose installable skill frontmatter")
	}

})

func markdownLinkRootPrefixExtraEvidenceForTitle(title string) []markdownLinkRootPrefixEvidence {
	return markdownLinkRootPrefixPlanExtraEvidence[title]
}

func markdownLinkRootPrefixPlanRepoRoot(t plantraceTestT) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func markdownLinkRootPrefixPlanScenarioTitles(t plantraceTestT, planPath string) []string {
	t.Helper()
	raw, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatalf("read markdown link root prefix plan: %v", err)
	}
	titles := []string{}
	for _, line := range strings.Split(string(raw), "\n") {
		title, ok := strings.CutPrefix(line, "  Scenario: ")
		if ok {
			titles = append(titles, title)
		}
	}
	return titles
}

func assertMarkdownLinkRootPrefixEvidenceExists(t plantraceTestT, repoRoot string, title string, evidence markdownLinkRootPrefixEvidence) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repoRoot, evidence.file))
	if err != nil {
		t.Fatalf("scenario %q evidence file %s cannot be read: %v", title, evidence.file, err)
	}
	if !strings.Contains(string(raw), evidence.text) {
		t.Fatalf("scenario %q evidence %q not found in %s", title, evidence.text, evidence.file)
	}
}
