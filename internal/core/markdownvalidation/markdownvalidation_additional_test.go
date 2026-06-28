package markdownvalidation

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/perber/wiki/internal/core/markdownlinks"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/core/tree"
)

var _ = ginkgo.Describe("markdownvalidation additional coverage", func() {
	ginkgo.It("ValidateMarkdownContent legacy wrapper returns OK for simple canonical content", func() {
		result := ValidateMarkdownContent("docs/page", string(canonicalValidationMarkdown("page-1", "Page", "# Page\n")), "page-1")

		Expect(result.OK).To(BeTrue())
		Expect(result.Issues).To(BeEmpty())
	})

	ginkgo.It("ValidateMarkdownContent legacy wrapper reports invalid paths and metadata parse errors", func() {
		result := ValidateMarkdownContent("../bad", "---\nleafwiki_id: [broken\n---\nBody", "page-1")

		Expect(result.OK).To(BeFalse())
		Expect(result.Issues).To(ConsistOf(
			WithTransform(func(issue Issue) IssueCode { return issue.Code }, Equal(IssueCodeInvalidPath)),
			WithTransform(func(issue Issue) IssueCode { return issue.Code }, Equal(IssueCodeMetadataParseError)),
		))
	})

	ginkgo.It("ValidateMarkdownContentWithOptions reports root routes, path conflicts, and reserved extra metadata", func() {
		root := ValidateMarkdownContentWithOptions("", string(canonicalValidationMarkdown("root-page", "Root", "# Root\n")), ContentValidationOptions{})
		Expect(issueCodes(root)).To(ContainElement(IssueCodeInvalidPath))

		allowedRoot := ValidateMarkdownContentWithOptions("", string(canonicalValidationMarkdown("root-page", "Root", "# Root\n")), ContentValidationOptions{
			AllowRootRoute: true,
		})
		Expect(allowedRoot.OK).To(BeTrue())

		conflict := ValidateMarkdownContentWithOptions("docs/page", string(canonicalValidationMarkdown("current-page", "Page", "# Page\n")), ContentValidationOptions{
			ExistingPageID: "current-page",
			ResolvePageID: func(routePath tree.RoutePath) (tree.PageID, bool) {
				Expect(routePath).To(Equal(tree.RoutePath("docs/page")))
				return "other-page", true
			},
		})
		Expect(issueCodes(conflict)).To(ContainElement(IssueCodePathConflict))

		reservedExtra := ValidateMarkdownContentWithOptions("docs/page", "<!-- leafwiki\nversion: 1\npage:\n  id: current-page\n  title: Page\nextra:\n  leafwiki_shadow: value\n-->\n\n# Page\n", ContentValidationOptions{
			ExistingPageID: "current-page",
		})
		Expect(issueCodes(reservedExtra)).To(ContainElement(IssueCodeReservedMetadata))
	})

	ginkgo.It("ValidateMarkdownContentWithOptions covers markdown link resolver fallbacks", func() {
		markdownResolver := ValidateMarkdownContentWithOptions("docs/source", "[Missing](/missing)\n", ContentValidationOptions{
			ResolveMarkdownLink: func(destination string) (tree.PageID, tree.NodeKind, bool, IssueCode) {
				Expect(destination).To(Equal("/missing"))
				return "", "", false, ""
			},
		})
		Expect(issueCodes(markdownResolver)).To(Equal([]IssueCode{IssueCodeBrokenLink}))

		legacyResolver := ValidateMarkdownContentWithOptions("docs/source", "[Bad](bad)\n[Missing](missing)\n[Page](page)\n", ContentValidationOptions{
			ResolvePageID: func(tree.RoutePath) (tree.PageID, bool) {
				return "", false
			},
			ResolveReferencePath: func(destination string) string {
				switch destination {
				case "bad":
					return `/bad\slug`
				case "missing":
					return "/missing"
				case "page":
					return "/page"
				default:
					return ""
				}
			},
			ResolveLinkTarget: func(routePath tree.RoutePath) (tree.PageID, tree.NodeKind, bool) {
				if routePath == "page" {
					return "page-id", tree.NodeKindPage, true
				}
				return "", "", false
			},
		})
		Expect(issueCodes(legacyResolver)).To(ConsistOf(IssueCodeBrokenLink, IssueCodeBrokenLink, IssueCodeNonCanonicalLink))
	})

	ginkgo.It("ValidateMarkdownContentWithOptions ignores external links and uses legacy page resolution", func() {
		external := ValidateMarkdownContentWithOptions("docs/source", "[External](https://example.com)\n[Anchor](#local)\n", ContentValidationOptions{
			ResolvePageID: func(routePath tree.RoutePath) (tree.PageID, bool) {
				return "", false
			},
		})
		Expect(external.OK).To(BeTrue())

		noLegacyResolver := ValidateMarkdownContentWithOptions("docs/source", "[Wiki](target)\n", ContentValidationOptions{
			AssetExists: func(string) bool {
				return true
			},
		})
		Expect(noLegacyResolver.OK).To(BeTrue())

		rootReference := ValidateMarkdownContentWithOptions("", "[Root](/)\n", ContentValidationOptions{
			AllowRootRoute: true,
			ResolvePageID: func(routePath tree.RoutePath) (tree.PageID, bool) {
				return "", false
			},
		})
		Expect(rootReference.OK).To(BeTrue())

		var resolvedRoute tree.RoutePath
		missing := ValidateMarkdownContentWithOptions("docs/source", "[Missing](missing)\n", ContentValidationOptions{
			ResolvePageID: func(routePath tree.RoutePath) (tree.PageID, bool) {
				resolvedRoute = routePath
				return "", false
			},
		})
		Expect(resolvedRoute).NotTo(BeEmpty())
		Expect(issueCodes(missing)).To(Equal([]IssueCode{IssueCodeBrokenLink}))
	})

	ginkgo.It("Combine preserves issue order and recomputes summary", func() {
		first := Result{Issues: []Issue{
			{Severity: IssueSeverityWarning, Code: IssueCodeHiddenMarkdownPath, Message: "hidden"},
		}}
		second := Result{Issues: []Issue{
			{Severity: IssueSeverityError, Code: IssueCodeBrokenLink, Message: "broken"},
			{Severity: IssueSeverityWarning, Code: IssueCodeWorkspaceSyncValidation, Message: "sync"},
		}}

		result := Combine(first, second)

		Expect(result.OK).To(BeFalse())
		Expect(result.Summary.Errors).To(Equal(1))
		Expect(result.Summary.Warnings).To(Equal(2))
		Expect(result.Issues).To(Equal([]Issue{
			{Severity: IssueSeverityWarning, Code: IssueCodeHiddenMarkdownPath, Message: "hidden"},
			{Severity: IssueSeverityError, Code: IssueCodeBrokenLink, Message: "broken"},
			{Severity: IssueSeverityWarning, Code: IssueCodeWorkspaceSyncValidation, Message: "sync"},
		}))
	})

	ginkgo.It("resolveReferencePath resolves relative, parent, absolute, and ignored destinations", func() {
		route := tree.RoutePath("docs/source")

		Expect(resolveReferencePath(route, "target")).To(Equal("/docs/source/target"))
		Expect(resolveReferencePath(route, "../target?view=1#intro")).To(Equal("/docs/target"))
		Expect(resolveReferencePath(route, "/absolute/page.md#intro")).To(Equal("/absolute/page.md"))
		Expect(resolveReferencePath(route, "")).To(BeEmpty())
		Expect(resolveReferencePath(route, "#intro")).To(BeEmpty())
	})

	ginkgo.It("resolveWorkspaceReferencePath maps markdown file destinations through markdown path semantics", func() {
		relPath := tree.MarkdownPath("docs/source.md")
		routePath := tree.RoutePath("docs/source")

		Expect(resolveWorkspaceReferencePath(relPath, routePath, "")).To(BeEmpty())
		Expect(resolveWorkspaceReferencePath(relPath, routePath, "target.md#intro")).To(Equal("/docs/target"))
		Expect(resolveWorkspaceReferencePath(relPath, routePath, "../guide/README.md?view=1")).To(Equal("/guide/README"))
		Expect(resolveWorkspaceReferencePath(relPath, routePath, "/docs/section/index.md")).To(Equal("/docs/section"))
		Expect(resolveWorkspaceReferencePath(relPath, routePath, "target")).To(Equal("/docs/source/target"))
	})

	ginkgo.It("link helper edge cases preserve root prefixes and reject empty extensionless destinations", func() {
		Expect(stripMarkdownLinkRootPrefix("/docs", "docs")).To(Equal("/"))
		Expect(stripMarkdownLinkRootPrefix("/docs/page.md", "docs")).To(Equal("/page.md"))
		Expect(stripMarkdownLinkRootPrefix("/other/page.md", "docs")).To(Equal("/other/page.md"))

		Expect(isExtensionlessWikiDestination("")).To(BeFalse())
		Expect(isExtensionlessWikiDestination("section/")).To(BeFalse())
		Expect(isExtensionlessWikiDestination("page")).To(BeTrue())

		pageID, kind, ok, code := newWorkspaceMarkdownLinkResolver("source.md", nil, nil)("missing.md")
		Expect(pageID).To(BeEmpty())
		Expect(kind).To(BeEmpty())
		Expect(ok).To(BeFalse())
		Expect(code).To(Equal(IssueCodeBrokenLink))

		index := markdownlinks.NewIndex([]markdownlinks.Entry{
			{Kind: markdownlinks.EntryKindPage, RoutePath: "docs/page", Path: "docs/page.md"},
			{Kind: markdownlinks.EntryKindSection, RoutePath: "docs/section"},
		})
		resolver := newWorkspaceMarkdownLinkResolver("docs/source.md", index, map[workspaceValidationRouteKey]tree.PageID{})
		pageID, kind, ok, code = resolver("/docs/page.md")
		Expect(pageID).To(BeEmpty())
		Expect(kind).To(Equal(tree.NodeKindPage))
		Expect(ok).To(BeFalse())
		Expect(code).To(Equal(IssueCodeBrokenLink))

		pageID, kind, ok, code = resolver("/docs/section")
		Expect(pageID).To(BeEmpty())
		Expect(kind).To(Equal(tree.NodeKindSection))
		Expect(ok).To(BeFalse())
		Expect(code).To(Equal(IssueCodeBrokenLink))

		Expect(resolveReferencePath(tree.RoutePath("docs/source"), "%zz")).To(BeEmpty())
		Expect(resolveReferencePath("", "/")).To(BeEmpty())
	})

	ginkgo.It("isMarkdownFileDestination detects markdown destinations after cleaning query and fragment", func() {
		Expect(isMarkdownFileDestination("page.md#intro")).To(BeTrue())
		Expect(isMarkdownFileDestination("page.MARKDOWN?view=1")).To(BeFalse())
		Expect(isMarkdownFileDestination("asset.png")).To(BeFalse())
		Expect(isMarkdownFileDestination("#intro")).To(BeFalse())
	})

	ginkgo.It("ValidateWorkspaceStatus filters warnings and preserves explicit message IDs", func() {
		result := ValidateWorkspaceStatus([]WorkspaceStatusIssue{
			{Path: "warn.md", Severity: IssueSeverityWarning, Message: "warning"},
			{Path: "error.md", Code: IssueCodeBrokenLink, MessageID: sharederrors.MessageID("custom.message"), Severity: IssueSeverityError, Message: "error"},
		}, false)

		Expect(result.Issues).To(HaveLen(1))
		Expect(result.Issues[0].SourcePath).To(Equal(tree.MarkdownPath("error.md")))
		Expect(result.Issues[0].Code).To(Equal(IssueCodeBrokenLink))
		Expect(result.Issues[0].MessageID).To(Equal(sharederrors.MessageID("custom.message")))
	})

	ginkgo.It("ValidateWorkspaceMarkdownFiles handles empty roots, hidden markdown warnings, and workspace asset callbacks", func() {
		Expect(ValidateWorkspaceMarkdownFiles(WorkspaceMarkdownValidationOptions{RootDir: "  "}).OK).To(BeTrue())

		hiddenRoot := ginkgo.GinkgoT().TempDir()
		Expect(os.WriteFile(filepath.Join(hiddenRoot, ".hidden.md"), canonicalValidationMarkdown("hidden", "Hidden", "# Hidden\n"), 0o644)).To(Succeed())
		hiddenResult := ValidateWorkspaceMarkdownFiles(WorkspaceMarkdownValidationOptions{
			RootDir:         hiddenRoot,
			IncludeWarnings: true,
		})
		Expect(hiddenResult.OK).To(BeTrue())
		Expect(hiddenResult.Summary.Warnings).To(Equal(1))
		Expect(issueCodes(hiddenResult)).To(ContainElement(IssueCodeHiddenMarkdownPath))

		assetRoot := ginkgo.GinkgoT().TempDir()
		Expect(os.WriteFile(filepath.Join(assetRoot, "source.md"), canonicalValidationMarkdown("source-page", "Source", "# Source\n\n![Logo](assets/logo.png)\n"), 0o644)).To(Succeed())
		var seenPageID tree.PageID
		var seenDestination string
		assetResult := ValidateWorkspaceMarkdownFiles(WorkspaceMarkdownValidationOptions{
			RootDir: assetRoot,
			AssetExists: func(pageID tree.PageID, destination string) bool {
				seenPageID = pageID
				seenDestination = destination
				return false
			},
		})
		Expect(assetResult.OK).To(BeFalse())
		Expect(issueCodes(assetResult)).To(ContainElement(IssueCodeMissingAsset))
		Expect(seenPageID).To(Equal(tree.PageID("source-page")))
		Expect(seenDestination).To(Equal("assets/logo.png"))
	})

	ginkgo.It("ValidateWorkspaceMarkdownFiles reports invalid workspace directory and file routes", func() {
		rootDir := ginkgo.GinkgoT().TempDir()
		Expect(os.Mkdir(filepath.Join(rootDir, "!!!"), 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(rootDir, "!!!.md"), canonicalValidationMarkdown("invalid-file", "Invalid File", "# Invalid\n"), 0o644)).To(Succeed())

		result := ValidateWorkspaceMarkdownFiles(WorkspaceMarkdownValidationOptions{RootDir: rootDir})

		Expect(result.OK).To(BeFalse())
		Expect(issueCodes(result)).To(ContainElement(IssueCodeInvalidSlug))
	})

	ginkgo.It("ValidateWorkspaceMarkdownFiles reports callback and terminal walk errors", func() {
		rootDir := ginkgo.GinkgoT().TempDir()
		originalWalkWorkspaceDir := walkWorkspaceDir
		ginkgo.DeferCleanup(func() {
			walkWorkspaceDir = originalWalkWorkspaceDir
		})
		walkWorkspaceDir = func(root string, walkFn fs.WalkDirFunc) error {
			Expect(root).To(Equal(rootDir))
			Expect(walkFn(filepath.Join(root, "broken.md"), nil, errors.New("stat failed"))).To(Succeed())
			return errors.New("walk failed")
		}

		result := ValidateWorkspaceMarkdownFiles(WorkspaceMarkdownValidationOptions{RootDir: rootDir})

		Expect(result.OK).To(BeFalse())
		Expect(issueCodes(result)).To(ContainElements(IssueCodeWorkspaceScanError, IssueCodeWorkspaceScanError))
		Expect(result.Issues[0].SourcePath).To(Equal(tree.MarkdownPath("broken.md")))
		Expect(result.Issues[1].SourcePath).To(Equal(tree.MarkdownPath("workspace")))
	})

	ginkgo.It("ValidateWorkspaceMarkdownFiles skips hidden, static, and non-markdown entries while reporting scan failures", func() {
		rootDir := ginkgo.GinkgoT().TempDir()
		Expect(os.Mkdir(filepath.Join(rootDir, ".drafts"), 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(rootDir, ".drafts", "ignored.md"), canonicalValidationMarkdown("draft", "Draft", "# Draft\n"), 0o644)).To(Succeed())
		Expect(os.Mkdir(filepath.Join(rootDir, "assets"), 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(rootDir, "assets", "ignored.md"), canonicalValidationMarkdown("asset", "Asset", "# Asset\n"), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(rootDir, "notes.txt"), []byte("not markdown"), 0o644)).To(Succeed())

		blockedDir := filepath.Join(rootDir, "blocked")
		Expect(os.Mkdir(blockedDir, 0o755)).To(Succeed())
		Expect(os.Chmod(blockedDir, 0)).To(Succeed())
		ginkgo.DeferCleanup(func() {
			_ = os.Chmod(blockedDir, 0o755)
		})
		if _, err := os.ReadDir(blockedDir); err == nil {
			ginkgo.Skip("filesystem permits reading chmod 000 directories")
		}

		unreadableFile := filepath.Join(rootDir, "unreadable.md")
		Expect(os.WriteFile(unreadableFile, canonicalValidationMarkdown("unreadable", "Unreadable", "# Unreadable\n"), 0o644)).To(Succeed())
		Expect(os.Chmod(unreadableFile, 0)).To(Succeed())
		ginkgo.DeferCleanup(func() {
			_ = os.Chmod(unreadableFile, 0o644)
		})
		if _, err := os.ReadFile(unreadableFile); err == nil {
			ginkgo.Skip("filesystem permits reading chmod 000 files")
		}

		result := ValidateWorkspaceMarkdownFiles(WorkspaceMarkdownValidationOptions{RootDir: rootDir})

		Expect(result.OK).To(BeFalse())
		Expect(issueCodes(result)).To(ContainElement(IssueCodeWorkspaceScanError))
		Expect(issueCodes(result)).NotTo(ContainElement(IssueCodeBrokenLink))
	})

	ginkgo.It("IssueCode and IssueSeverity normalize empty and unknown values predictably", func() {
		Expect(IssueSeverity("  ").Normalize(IssueSeverityWarning)).To(Equal(IssueSeverityWarning))
		Expect(IssueSeverity(" notice ").Normalize(IssueSeverityError)).To(Equal(IssueSeverity("notice")))
		Expect(IssueCode("unknown_code").MessageID()).To(Equal(MessageIDWorkspaceSyncValidation))
	})

	ginkgo.It("helper fallbacks return stable values for invalid relative and URL inputs", func() {
		absoluteRoot := filepath.Join(string(filepath.Separator), "abs", "root")
		Expect(workspaceValidationRelPath(absoluteRoot, "relative.md")).To(Equal("relative.md"))
		Expect(resolveReferencePath(tree.RoutePath("%zz"), "target")).To(BeEmpty())
	})
})

func issueCodes(result Result) []IssueCode {
	codes := make([]IssueCode, 0, len(result.Issues))
	for _, issue := range result.Issues {
		codes = append(codes, issue.Code)
	}
	return codes
}
