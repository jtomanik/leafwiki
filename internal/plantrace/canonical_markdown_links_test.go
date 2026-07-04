package plantrace

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"os"
	"path/filepath"
	"strings"
)

type canonicalPlanEvidence struct {
	file string
	text string
}

type canonicalPlanScenarioCoverage struct {
	title    string
	evidence canonicalPlanEvidence
}

// Exact scenario titles from docs/plans/canonical_markdown_links.PLAN.md, each with
// a stable automated-test evidence string that must exist in the repo.
var canonicalMarkdownLinksPlanScenarioCoverage = []canonicalPlanScenarioCoverage{
	{"Autocomplete inserts a canonical page link", evidence("e2e/tests/page.spec.ts", "markdown link autocomplete works")},
	{"Autocomplete inserts a canonical section link", evidence("e2e/tests/page.spec.ts", "autocomplete-emits-section-links-without-md")},
	{"Relative page links are resolved from the source file directory", evidence("internal/core/markdownlinks/markdownlinks_test.go", "TestResolveCanonicalLink_UsesFilesystemRelativeSemanticsNotPageAsFolder")},
	{"Relative section links are resolved from the source file directory", evidence("internal/links/link_service_test.go", "resolves relative section links from the source file directory")},
	{"Section trailing slash is accepted but canonicalized away", evidence("internal/core/markdownlinks/markdownlinks_test.go", "TestResolveCanonicalLink_SectionTrailingSlashIsAcceptedButCanonicalizedAway")},
	{"Root section link remains slash", evidence("internal/core/markdownlinks/markdownlinks_test.go", "TestResolveCanonicalLink_RelativeRootSectionCanonicalizesToSlash")},
	{"Query string and fragment are preserved byte-for-byte", evidence("internal/core/markdownlinks/markdownlinks_test.go", "TestCanonicalizeMarkdownLinks_PreservesAngleDestinationsTitleQueryAndFragment")},
	{"Link title and angle-bracket destination syntax are preserved", evidence("internal/core/markdownlinks/markdownlinks_test.go", "TestCanonicalizeMarkdownLinks_PreservesAngleDestinationsTitleQueryAndFragment")},
	{"Parentheses in destinations do not corrupt the rewrite", evidence("internal/links/link_refactor_replace_test.go", "rewrites destinations containing parentheses without corrupting them")},
	{"Percent-encoded paths use exact filesystem matching", evidence("internal/core/markdownlinks/markdownlinks_test.go", "TestResolveCanonicalLink_PreservesPercentEncodedPathStyle")},
	{"External and non-page links are ignored", evidence("internal/core/markdownlinks/markdownlinks_test.go", "TestResolveCanonicalLink_TreatsProtocolRelativeAndSchemedURLsAsExternal")},
	{"Link-like text in inline code and fenced code is ignored", evidence("internal/core/markdownlinks/markdownlinks_test.go", "TestCanonicalizeMarkdownLinks_SkipsMultiBacktickCodeSpans")},
	{"Malformed percent-encoding reports an invalid link instead of panicking", evidence("internal/core/markdownvalidation/use_cases_test.go", "reports invalid canonical links without panicking")},
	{"Relative link cannot escape the workspace root", evidence("internal/workspacesync/service_test.go", "leaves invalid canonical links unchanged and reports invalid link issues")},
	{"Existing canonical relative page links preserve relative style", evidence("internal/links/link_refactor_test.go", "keeps canonical page markdown extensions on absolute and relative rewritten links")},
	{"Existing canonical absolute page links preserve absolute style", evidence("internal/links/link_refactor_test.go", "keeps canonical page markdown extensions on absolute and relative rewritten links")},
	{"Section move keeps section links extensionless", evidence("internal/links/link_refactor_test.go", "uses source-file directory semantics for moved section links")},
	{"Page and section with the same basename are not cross-rewritten", evidence("internal/links/link_refactor_test.go", "does not cross-rewrite a same-basename section link during a page refactor")},
	{"Broken non-canonical links are not silently rewritten by refactor", evidence("internal/links/link_refactor_test.go", "rewrites canonical page links without healing legacy extensionless page links")},
	{"Source page move recalculates relative links without changing absolute links", evidence("internal/links/link_refactor_replace_test.go", "preserves canonical page markdown extensions after the source page moves")},
	{"Refactor preview reports conflicts without mutating content", evidence("internal/wiki/pages/pages_test.go", "leaves incoming links unchanged when rename target conflicts")},
	{"New section creates index.md", evidence("internal/core/tree/tree_service_test.go", "TestTreeService_CreateNode_Section_CreatesIndexWithFrontmatter")},
	{"index.md has precedence over README.md", evidence("internal/core/tree/node_store_reconstruct_test.go", "TestNodeStore_ReconstructTreeFromFS_IndexBeatsReadmeAndReadmeIsSeparatePage")},
	{"README.md is fallback section default", evidence("internal/core/tree/node_store_reconstruct_test.go", "TestNodeStore_ReconstructTreeFromFS_ReadmeFallbackSectionWhenNoIndexExists")},
	{"root README.md is fallback only without root index.md", evidence("internal/core/tree/node_store_reconstruct_test.go", "TestNodeStore_ReconstructTreeFromFS_RootReadmeFallbackSectionWhenNoIndexExists")},
	{"root index.md has precedence over root README.md", evidence("internal/core/tree/node_store_reconstruct_test.go", "TestNodeStore_ReconstructTreeFromFS_RootIndexBeatsRootReadme")},
	{"README.md as normal page keeps its filesystem casing in generated links", evidence("e2e/tests/root-dir.spec.ts", "[README Page](/${fixture.indexedSlug}/README.md)")},
	{"Uppercase INDEX.MD does not become a second child page when accepted by current index lookup rules", evidence("internal/core/tree/node_store_reconstruct_test.go", "TestNodeStore_ReconstructTreeFromFS_UsesUppercaseSectionIndex")},
	{"Old extensionless page link migrates to .md", evidence("internal/workspacesync/service_test.go", "rewrites resolvable legacy page links before validation")},
	{"Old extensionless section link remains extensionless", evidence("internal/workspacesync/service_test.go", "canonicalizes section trailing slashes without repeat revisions")},
	{"Unresolved old extensionless page link becomes validation error", evidence("internal/workspacesync/service_test.go", "keeps unresolved legacy links in content and reports a broken link issue")},
	{"Query and fragment survive migration", evidence("e2e/tests/workspace-sync.spec.ts", "workspace-sync-preserves-query-fragment-and-leaves-assets-code-blocks-unchanged")},
	{"Relative old page link migrates to relative .md", evidence("internal/workspacesync/service_test.go", "migrates relative legacy page links while preserving canonical relative links")},
	{"Existing canonical .md page link is not rewritten", evidence("internal/workspacesync/service_test.go", "migrates relative legacy page links while preserving canonical relative links")},
	{"Ambiguous extensionless link is left as validation error", evidence("internal/workspacesync/service_test.go", "keeps ambiguous extensionless links unchanged and reports an ambiguity issue")},
	{"Explicit index.md section link canonicalizes to the section", evidence("internal/core/markdownlinks/markdownlinks_test.go", "TestResolveCanonicalLink_ExplicitSectionDefaultFilesCanonicalizeToSection")},
	{"Explicit README.md section fallback link canonicalizes to the section", evidence("internal/core/markdownlinks/markdownlinks_test.go", "TestResolveCanonicalLink_ExplicitSectionDefaultFilesCanonicalizeToSection")},
	{"Explicit README.md page link stays a page when index.md exists", evidence("internal/http/router_test.go", "serves README.md as a section fallback only when no explicit page exists")},
	{"Migration is idempotent", evidence("internal/workspacesync/service_test.go", "does not create another migration revision after links are canonical")},
	{"Migration writeback is captured in revision history", evidence("internal/workspacesync/service_test.go", "keeps raw incoming content and canonical writeback revisions")},
	{"Migration write failure reports sync validation state without losing raw content", evidence("internal/workspacesync/service_test.go", "reports rewrite failure before derived indexes rebuild")},
	{"Canonical .md page link indexes as outgoing link", evidence("internal/links/link_service_test.go", "resolves canonical relative page markdown links from the source file directory")},
	{"Canonical section link indexes as outgoing link", evidence("internal/links/link_service_test.go", "prefers canonical section targets over same-basename page twins for extensionless links")},
	{"Case mismatch is invalid", evidence("internal/core/markdownvalidation/use_cases_test.go", "uses exact case-sensitive target matching for workspace links")},
	{"Assets are not coerced", evidence("internal/links/link_service_test.go", "leaves asset destinations out of wiki link extraction")},
	{"Broken canonical .md page link is reported as broken", evidence("internal/links/link_service_test.go", "returns broken targets with normalized paths for missing destinations")},
	{"Old extensionless page link is reported as non-canonical when migration cannot resolve it", evidence("internal/core/markdownvalidation/use_cases_test.go", "rejects unmigrated extensionless page links as non-canonical")},
	{"Duplicate syntaxes do not create duplicate target identities after migration", evidence("internal/workspacesync/service_test.go", "indexes duplicate legacy and canonical syntaxes as a single healthy target")},
	{"Reference-style link definitions are rewritten", evidence("internal/importer/content_transformer_test.go", "rewrites reference definitions while preserving titles and image assets")},
	{"Image links remain governed by existing image and asset validation", evidence("internal/links/link_service_test.go", "ignores image links even when they point at page destinations")},
	{"GitHub .md page links remain GitHub-compatible", evidence("e2e/tests/importer.spec.ts", "importer-ui-canonical-page-links-navigate-in-preview")},
	{"GitHub README section import becomes section link", evidence("e2e/tests/importer.spec.ts", "importer-ui-readme-only-folder-imports-as-section")},
	{"Importer does not rewrite code examples", evidence("internal/importer/content_transformer_test.go", "indented code stays unchanged")},
	{"Importer migrates old route-style page link to .md", evidence("internal/importer/executor_test.go", "rewrites imported markdown links to created wiki routes")},
	{"Importer leaves unresolved internal links as validation errors", evidence("e2e/tests/importer.spec.ts", "importer-ui-unresolved-extensionless-page-link-surfaces-validation-error")},
	{"Importer distinguishes folder README section from README child page", evidence("e2e/tests/importer.spec.ts", "importer-ui-canonical-page-links-navigate-in-preview")},
	{"Importer preserves query and fragment while canonicalizing", evidence("internal/importer/content_transformer_test.go", "markdown link preserves query and fragment")},
	{"Importer does not coerce assets with .md extension under asset namespaces", evidence("internal/importer/content_transformer_test.go", "asset markdown path remains unchanged")},
	{"User can click a canonical page link in preview", evidence("e2e/tests/page.spec.ts", "preview-clicks-canonical-absolute-page-link-with-query-fragment")},
	{"Workspace sync repairs links and shows validation errors for the rest", evidence("e2e/tests/workspace-sync.spec.ts", "workspace-sync-repairs-link-and-keeps-remaining-validation-error-in-same-sync")},
	{"MCP agent context returns canonical examples", evidence("internal/wiki/mcp/mcp_integration_test.go", "returns agent-ready context state and canonical link examples")},
	{"User can click a canonical section link in preview", evidence("e2e/tests/page.spec.ts", "preview-clicks-section-link-with-trailing-slash-and-canonicalizes-url")},
	{"Preview shows a broken-link state for unresolved canonical page links", evidence("e2e/tests/page.spec.ts", "preview-shows-broken-state-for-unresolved-canonical-page-md-link")},
	{"Direct browser route opens canonical .md page deep link", evidence("e2e/tests/page.spec.ts", "direct-browser-deep-link-page-md-opens-viewer-page")},
	{"Direct browser route does not alias old extensionless page path", evidence("e2e/tests/page.spec.ts", "direct-extensionless-page-route-does-not-open-old-page-alias")},
	{"Direct browser route canonicalizes section trailing slash", evidence("e2e/tests/page.spec.ts", "preview-clicks-section-link-with-trailing-slash-and-canonicalizes-url")},
	{"Exact-case mismatch is visible to the user", evidence("e2e/tests/page.spec.ts", "direct-browser-deep-link-case-mismatch-shows-not-found")},
	{"Workspace sync UI shows both automatic repairs and remaining errors", evidence("e2e/tests/workspace-sync.spec.ts", "workspace-sync-repairs-link-and-keeps-remaining-validation-error-in-same-sync")},
	{"Importer UI creates GitHub-compatible links from a README-based zip", evidence("e2e/tests/root-dir.spec.ts", "separate-root-readme-fallback-and-index-precedence-match-default-root")},
	{"MCP validation reports canonical and non-canonical links consistently", evidence("e2e/tests/mcp-agent-context.spec.ts", "mcp-validate-content-reports-canonical-legacy-and-asset-links-consistently")},
	{"MCP refactor preview and apply preserve canonical page and section syntax", evidence("e2e/tests/mcp-safe-edits.spec.ts", "safe edit tools patch sections and metadata with version checks")},
	{"Separate root-dir mode migrates content in root dir only", evidence("e2e/tests/root-dir.spec.ts", "separate-root-workspace-sync-rewrites-links-only-inside-configured-root")},
}

func evidence(file string, text string) canonicalPlanEvidence {
	return canonicalPlanEvidence{file: file, text: text}
}

var _ = ginkgo.Describe("canonical Markdown link plan traceability", ginkgo.Label("integration"), func() {
	ginkgo.It("maps every plan scenario title to automated evidence", func() {
		repoRoot := canonicalPlanRepoRoot()
		planTitles := canonicalPlanScenarioTitles(filepath.Join(repoRoot, "docs", "plans", "canonical_markdown_links.PLAN.md"))
		Expect(planTitles).To(HaveLen(len(canonicalMarkdownLinksPlanScenarioCoverage)), "plan scenarios should match mapped evidence")

		coverageByTitle := map[string]canonicalPlanEvidence{}
		for _, coverage := range canonicalMarkdownLinksPlanScenarioCoverage {
			Expect(coverage).To(haveCanonicalPlanScenarioMapping())
			Expect(coverageByTitle).NotTo(HaveKey(coverage.title), "duplicate scenario evidence title %q", coverage.title)
			coverageByTitle[coverage.title] = coverage.evidence
		}

		for _, title := range planTitles {
			evidence, err := canonicalPlanEvidenceForTitle(coverageByTitle, title)
			Expect(err).NotTo(HaveOccurred())
			Expect(evidence).To(existInCanonicalPlanEvidenceFile(repoRoot, title))
		}
		for title := range coverageByTitle {
			Expect(planTitles).To(ContainElement(title), "scenario evidence %q is not present in the plan", title)
		}

	})
})

func canonicalPlanRepoRoot() string {
	ginkgo.GinkgoHelper()
	file, err := plantraceSourceFile()
	Expect(err).NotTo(HaveOccurred(), "runtime.Caller should locate the plantrace source file")
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func canonicalPlanScenarioTitles(planPath string) []string {
	ginkgo.GinkgoHelper()
	raw, err := os.ReadFile(planPath)
	Expect(err).NotTo(HaveOccurred(), "read canonical Markdown links plan")
	titles := []string{}
	for _, line := range strings.Split(string(raw), "\n") {
		title, ok := strings.CutPrefix(line, "  Scenario: ")
		if ok {
			titles = append(titles, title)
		}
	}
	return titles
}
