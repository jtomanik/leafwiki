package workspacesync

import (
	"errors"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"

	wikivalidation "github.com/perber/wiki/internal/core/markdownvalidation"
	"github.com/perber/wiki/internal/core/tree"
	testmatchers "github.com/perber/wiki/internal/test_utils/matchers"
)

var _ = Describe("workspace sync path and validation helpers", Label("unit"), func() {
	It("selects markdown paths from source metadata route scans and revision routes", func() {
		rootDir := workspaceSyncTempDir()
		Expect(os.MkdirAll(filepath.Join(rootDir, "docs"), 0o755)).To(Succeed())
		Expect(os.MkdirAll(filepath.Join(rootDir, ".hidden"), 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(rootDir, ".hidden", "ignored.md"), []byte("# Hidden\n"), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(rootDir, "docs", "README.md"), []byte("# Readme\n"), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(rootDir, "docs", "Page.MD"), []byte("# Page\n"), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(rootDir, "docs", "route.md"), []byte("# Route\n"), 0o644)).To(Succeed())
		service := &Service{rootDir: rootDir}
		section := workspaceSyncEdgePage("docs", "Docs", "docs", tree.NodeKindSection)
		page := workspaceSyncEdgePage("page-1", "Page", "page", tree.NodeKindPage)
		page.Parent = &tree.PageNode{ID: "docs", Title: "Docs", Slug: "docs", Kind: tree.NodeKindSection}
		routePage := workspaceSyncEdgePage("route-1", "Route", "route", tree.NodeKindPage)
		routePage.Parent = page.Parent

		Expect(service.currentPageMarkdownPath(section)).To(Equal("docs/README.md"))
		Expect(service.currentPageMarkdownPath(page)).To(Equal("docs/Page.MD"))
		found, err := currentWorkspaceMarkdownPathByRouteResult(service, routePage)
		Expect(err).To(Succeed())
		Expect(found).To(Equal("docs/route.md"))
		_, err = currentWorkspaceMarkdownPathByRouteResult(&Service{rootDir: filepath.Join(rootDir, "missing")}, page)
		Expect(err).To(MatchError(errChangedContentMissing))
		Expect(service.currentSectionContentPath("docs", "fallback.md")).To(Equal("docs/README.md"))
		Expect(service.currentSectionContentPath("", "fallback.md")).To(Equal("fallback.md"))

		result, err := contentForPageAtCommitPathResult(rootDir, page, "preferred.md", map[string]string{
			"preferred.md": workspaceSyncEdgeMarkdown("other", "Other"),
			"docs/Page.MD": workspaceSyncEdgeMarkdown("page-1", "Page"),
		})
		Expect(err).To(Succeed())
		Expect(result).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"RelPath": Equal("docs/Page.MD"),
			"Content": ContainSubstring("Page"),
		}))
		_, err = changedContentForPageAtCommitResult(rootDir, page, "preferred.md", map[string]string{
			"preferred.md": workspaceSyncEdgeMarkdown("other", "Other"),
		})
		Expect(err).To(MatchError(errChangedContentMissing))
		_, err = leafWikiIDFromContentResult("---\nleafwiki_id: [broken\n---\n# Broken\n")
		Expect(err).To(MatchError(errLeafWikiIDMissing))

		routePath, kind := revisionRoutePathAndKind("", "docs/index.md", page)
		Expect(routePath.FilesystemPath()).To(Equal("docs"))
		Expect(kind).To(Equal(tree.NodeKindSection))
		routePath, kind = revisionRoutePathAndKind("", "weird///page.md", page)
		Expect(routePath.FilesystemPath()).To(Equal("weird/page"))
		Expect(kind).To(Equal(tree.NodeKindPage))
	})

	It("falls back to markdown validation issues and extracts markdown paths from errors", func() {
		rootDir := workspaceSyncTempDir()
		Expect(os.WriteFile(filepath.Join(rootDir, "bad.md"), []byte("[missing](/missing)\n"), 0o644)).To(Succeed())
		service := &Service{rootDir: rootDir, markdownLinkRootPrefix: "/"}

		errs := service.validationErrorsFromError(errors.New("generic failure"))
		Expect(errs).To(HaveLen(1))
		Expect(errs).To(testmatchers.HaveValidationIssue(wikivalidation.IssueCodeBrokenLink))
		Expect(errs).To(ContainElement(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"Path": Equal("bad"),
		})))

		Expect(markdownPathsInError(rootDir, "file=../outside.md file=. path=notes.txt")).To(BeEmpty())
	})

})
