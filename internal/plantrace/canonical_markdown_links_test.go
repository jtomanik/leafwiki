package plantrace

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type canonicalPlanEvidence struct {
	file string
	text string
}

type canonicalPlanScenarioCoverage struct {
	title    string
	evidence canonicalPlanEvidence
}

// Exact scenario titles from plans/canonical_markdown_links.PLAN.md, each with
// a stable automated-test evidence string that must exist in the repo.
var canonicalMarkdownLinksPlanScenarioCoverage = []canonicalPlanScenarioCoverage{
	{"Autocomplete inserts a canonical page link", evidence("e2e/tests/page.spec.ts", "markdown link autocomplete works")},
	{"Autocomplete inserts a canonical section link", evidence("e2e/tests/page.spec.ts", "autocomplete-emits-section-links-without-md")},
	{"Relative page links are resolved from the source file directory", evidence("internal/core/markdownlinks/markdownlinks_test.go", "TestResolveCanonicalLink_UsesFilesystemRelativeSemanticsNotPageAsFolder")},
	{"Relative section links are resolved from the source file directory", evidence("internal/links/link_service_test.go", "TestResolveTargetLinks_ResolvesRelativeSectionLinkFromSourceFileDirectory")},
	{"Section trailing slash is accepted but canonicalized away", evidence("internal/core/markdownlinks/markdownlinks_test.go", "TestResolveCanonicalLink_SectionTrailingSlashIsAcceptedButCanonicalizedAway")},
	{"Root section link remains slash", evidence("internal/core/markdownlinks/markdownlinks_test.go", "TestResolveCanonicalLink_RelativeRootSectionCanonicalizesToSlash")},
	{"Query string and fragment are preserved byte-for-byte", evidence("internal/core/markdownlinks/markdownlinks_test.go", "TestCanonicalizeMarkdownLinks_PreservesAngleDestinationsTitleQueryAndFragment")},
	{"Link title and angle-bracket destination syntax are preserved", evidence("internal/core/markdownlinks/markdownlinks_test.go", "TestCanonicalizeMarkdownLinks_PreservesAngleDestinationsTitleQueryAndFragment")},
	{"Parentheses in destinations do not corrupt the rewrite", evidence("internal/links/link_refactor_replace_test.go", "TestMarkdownRefactorEngine_Rewrite_SupportsParenthesesInDestination")},
	{"Percent-encoded paths use exact filesystem matching", evidence("internal/core/markdownlinks/markdownlinks_test.go", "TestResolveCanonicalLink_PreservesPercentEncodedPathStyle")},
	{"External and non-page links are ignored", evidence("internal/core/markdownlinks/markdownlinks_test.go", "TestResolveCanonicalLink_TreatsProtocolRelativeAndSchemedURLsAsExternal")},
	{"Link-like text in inline code and fenced code is ignored", evidence("internal/core/markdownlinks/markdownlinks_test.go", "TestCanonicalizeMarkdownLinks_SkipsMultiBacktickCodeSpans")},
	{"Malformed percent-encoding reports an invalid link instead of panicking", evidence("internal/core/markdownvalidation/use_cases_test.go", "TestValidateWorkspaceMarkdownFilesReportsInvalidCanonicalLinks")},
	{"Relative link cannot escape the workspace root", evidence("internal/workspacesync/service_test.go", "TestServiceSyncNowLeavesInvalidCanonicalLinksUnchangedAndReportsValidation")},
	{"Existing canonical relative page links preserve relative style", evidence("internal/links/link_refactor_test.go", "TestMarkdownRefactorEngine_RewriteCanonicalPageLinksKeepsMdAbsoluteAndRelative")},
	{"Existing canonical absolute page links preserve absolute style", evidence("internal/links/link_refactor_test.go", "TestMarkdownRefactorEngine_RewriteCanonicalPageLinksKeepsMdAbsoluteAndRelative")},
	{"Section move keeps section links extensionless", evidence("internal/links/link_refactor_test.go", "TestMarkdownRefactorEngine_RewriteSectionLinksUsesFilesystemRelativeSemantics")},
	{"Page and section with the same basename are not cross-rewritten", evidence("internal/links/link_refactor_test.go", "TestMarkdownRefactorEngine_PageRefactorDoesNotRewriteSameBasenameSectionLink")},
	{"Broken non-canonical links are not silently rewritten by refactor", evidence("internal/links/link_refactor_test.go", "TestMarkdownRefactorEngine_DoesNotRewriteLegacyExtensionlessPageLinks")},
	{"Source page move recalculates relative links without changing absolute links", evidence("internal/links/link_refactor_replace_test.go", "TestMarkdownRefactorEngine_RewriteRelativeLinksForPathChange_PreservesCanonicalPageMdFromMovedSource")},
	{"Refactor preview reports conflicts without mutating content", evidence("internal/wiki/pages/pages_test.go", "TestApplyPageRefactorUseCase_TargetConflictDoesNotRewriteIncomingLinks")},
	{"New section creates index.md", evidence("internal/core/tree/tree_service_test.go", "TestTreeService_CreateNode_Section_CreatesIndexWithFrontmatter")},
	{"index.md has precedence over README.md", evidence("internal/core/tree/node_store_reconstruct_test.go", "TestNodeStore_ReconstructTreeFromFS_IndexBeatsReadmeAndReadmeIsSeparatePage")},
	{"README.md is fallback section default", evidence("internal/core/tree/node_store_reconstruct_test.go", "TestNodeStore_ReconstructTreeFromFS_ReadmeFallbackSectionWhenNoIndexExists")},
	{"root README.md is fallback only without root index.md", evidence("internal/core/tree/node_store_reconstruct_test.go", "TestNodeStore_ReconstructTreeFromFS_RootReadmeFallbackSectionWhenNoIndexExists")},
	{"root index.md has precedence over root README.md", evidence("internal/core/tree/node_store_reconstruct_test.go", "TestNodeStore_ReconstructTreeFromFS_RootIndexBeatsRootReadme")},
	{"README.md as normal page keeps its filesystem casing in generated links", evidence("e2e/tests/root-dir.spec.ts", "[README Page](/${fixture.indexedSlug}/README.md)")},
	{"Uppercase INDEX.MD does not become a second child page when accepted by current index lookup rules", evidence("internal/core/tree/node_store_reconstruct_test.go", "TestNodeStore_ReconstructTreeFromFS_UsesUppercaseSectionIndex")},
	{"Old extensionless page link migrates to .md", evidence("internal/workspacesync/service_test.go", "TestServiceSyncNowRewritesResolvableLegacyPageLinkBeforeValidation")},
	{"Old extensionless section link remains extensionless", evidence("internal/workspacesync/service_test.go", "TestServiceSyncNowCanonicalizesSectionTrailingSlashWithoutRevisionLoop")},
	{"Unresolved old extensionless page link becomes validation error", evidence("internal/workspacesync/service_test.go", "TestServiceSyncNowLeavesUnresolvedLegacyPageLinkAndReportsValidationError")},
	{"Query and fragment survive migration", evidence("e2e/tests/workspace-sync.spec.ts", "workspace-sync-preserves-query-fragment-and-leaves-assets-code-blocks-unchanged")},
	{"Relative old page link migrates to relative .md", evidence("internal/workspacesync/service_test.go", "TestServiceSyncNowRelativeLegacyPageLinkMigratesAndCanonicalRelativeLinkStaysCanonical")},
	{"Existing canonical .md page link is not rewritten", evidence("internal/workspacesync/service_test.go", "TestServiceSyncNowRelativeLegacyPageLinkMigratesAndCanonicalRelativeLinkStaysCanonical")},
	{"Ambiguous extensionless link is left as validation error", evidence("internal/workspacesync/service_test.go", "TestServiceSyncNowReportsAmbiguousLegacyLinkWhenMigrationCannotRewrite")},
	{"Explicit index.md section link canonicalizes to the section", evidence("internal/core/markdownlinks/markdownlinks_test.go", "TestResolveCanonicalLink_ExplicitSectionDefaultFilesCanonicalizeToSection")},
	{"Explicit README.md section fallback link canonicalizes to the section", evidence("internal/core/markdownlinks/markdownlinks_test.go", "TestResolveCanonicalLink_ExplicitSectionDefaultFilesCanonicalizeToSection")},
	{"Explicit README.md page link stays a page when index.md exists", evidence("internal/http/router_test.go", "TestGetPageByPathEndpoint_ReadmeMarkdownPathUsesFallbackOnlyWhenActive")},
	{"Migration is idempotent", evidence("internal/workspacesync/service_test.go", "TestServiceSyncNowCanonicalMigrationSecondRunCreatesNoNewRevision")},
	{"Migration writeback is captured in revision history", evidence("internal/workspacesync/service_test.go", "TestServiceSyncNowKeepsRawAndCanonicalMigrationPageRevisions")},
	{"Migration write failure reports sync validation state without losing raw content", evidence("internal/workspacesync/service_test.go", "TestServiceSyncNowStopsBeforeDerivedRebuildsWhenCanonicalMigrationWriteFails")},
	{"Canonical .md page link indexes as outgoing link", evidence("internal/links/link_service_test.go", "TestResolveTargetLinks_ResolvesCanonicalRelativePageMdFromSourceFileDirectory")},
	{"Canonical section link indexes as outgoing link", evidence("internal/links/link_service_test.go", "TestResolveTargetLinks_ResolvesCanonicalSectionLinkForSameBasenameTwin")},
	{"Case mismatch is invalid", evidence("internal/core/markdownvalidation/use_cases_test.go", "TestValidateWorkspaceMarkdownFiles_UsesExactCaseSensitiveTargetMatching")},
	{"Assets are not coerced", evidence("internal/links/link_service_test.go", "TestExtractLinksFromMarkdown_IgnoresAssetDestinations")},
	{"Broken canonical .md page link is reported as broken", evidence("internal/links/link_service_test.go", "TestResolveTargetLinks_ReturnsBrokenTargetsForNonExisting")},
	{"Old extensionless page link is reported as non-canonical when migration cannot resolve it", evidence("internal/core/markdownvalidation/use_cases_test.go", "TestValidateWorkspaceMarkdownFiles_RejectsUnmigratedExtensionlessPageLink")},
	{"Duplicate syntaxes do not create duplicate target identities after migration", evidence("internal/workspacesync/service_test.go", "TestServiceSyncNowMigratedDuplicateSyntaxesIndexAsSinglePageTargetIdentity")},
	{"Reference-style link definitions are rewritten", evidence("internal/importer/content_transformer_test.go", "TestContentTransformer_RewritesReferenceDefinitions")},
	{"Image links remain governed by existing image and asset validation", evidence("internal/links/link_service_test.go", "TestExtractLinksFromMarkdown_IgnoresImageLinksToPageDestinations")},
	{"GitHub .md page links remain GitHub-compatible", evidence("e2e/tests/importer.spec.ts", "importer-ui-canonical-page-links-navigate-in-preview")},
	{"GitHub README section import becomes section link", evidence("e2e/tests/importer.spec.ts", "importer-ui-readme-only-folder-imports-as-section")},
	{"Importer does not rewrite code examples", evidence("internal/importer/content_transformer_test.go", "indented code stays unchanged")},
	{"Importer migrates old route-style page link to .md", evidence("internal/importer/executor_test.go", "TestExecutor_Create_RewritesMarkdownAndWikiLinksToImportedPages")},
	{"Importer leaves unresolved internal links as validation errors", evidence("e2e/tests/importer.spec.ts", "importer-ui-unresolved-extensionless-page-link-surfaces-validation-error")},
	{"Importer distinguishes folder README section from README child page", evidence("e2e/tests/importer.spec.ts", "importer-ui-canonical-page-links-navigate-in-preview")},
	{"Importer preserves query and fragment while canonicalizing", evidence("internal/importer/content_transformer_test.go", "markdown link preserves query and fragment")},
	{"Importer does not coerce assets with .md extension under asset namespaces", evidence("internal/importer/content_transformer_test.go", "asset markdown path remains unchanged")},
	{"User can click a canonical page link in preview", evidence("e2e/tests/page.spec.ts", "preview-clicks-canonical-absolute-page-link-with-query-fragment")},
	{"Workspace sync repairs links and shows validation errors for the rest", evidence("e2e/tests/workspace-sync.spec.ts", "workspace-sync-repairs-link-and-keeps-remaining-validation-error-in-same-sync")},
	{"MCP agent context returns canonical examples", evidence("internal/wiki/mcp/mcp_integration_test.go", "TestLocalMCPGetContext_ReturnsAgentReadyContext")},
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

func TestCanonicalMarkdownLinksPlanScenarioTitleAuditIndex(t *testing.T) {
	repoRoot := canonicalPlanRepoRoot(t)
	planTitles := canonicalPlanScenarioTitles(t, filepath.Join(repoRoot, "plans", "canonical_markdown_links.PLAN.md"))
	if len(planTitles) != len(canonicalMarkdownLinksPlanScenarioCoverage) {
		t.Fatalf("plan scenario title count = %d, want %d mapped scenarios", len(planTitles), len(canonicalMarkdownLinksPlanScenarioCoverage))
	}

	coverageByTitle := map[string]canonicalPlanEvidence{}
	for _, coverage := range canonicalMarkdownLinksPlanScenarioCoverage {
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
		assertCanonicalPlanEvidenceExists(t, repoRoot, title, evidence)
	}
	for title := range coverageByTitle {
		if _, ok := planTitleSet[title]; !ok {
			t.Fatalf("scenario coverage %q is not present in the plan", title)
		}
	}
}

func canonicalPlanRepoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func canonicalPlanScenarioTitles(t *testing.T, planPath string) []string {
	t.Helper()
	raw, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatalf("read canonical Markdown links plan: %v", err)
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

func assertCanonicalPlanEvidenceExists(t *testing.T, repoRoot string, title string, evidence canonicalPlanEvidence) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repoRoot, evidence.file))
	if err != nil {
		t.Fatalf("scenario %q evidence file %s cannot be read: %v", title, evidence.file, err)
	}
	content := string(raw)
	if !strings.Contains(content, evidence.text) {
		t.Fatalf("scenario %q evidence %q not found in %s", title, evidence.text, evidence.file)
	}
}
