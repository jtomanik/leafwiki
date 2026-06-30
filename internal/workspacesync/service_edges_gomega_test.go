package workspacesync

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"

	wikivalidation "github.com/perber/wiki/internal/core/markdownvalidation"
	"github.com/perber/wiki/internal/core/tree"
)

const (
	syncPrimaryError   = "primary"
	syncSecondaryError = "secondary"
)

var errWatcherFactoryFailed = errors.New("watcher factory failed")

var _ = Describe("workspace sync service edges", func() {
	It("passes through actor IDs and validates enabled service dependencies", func() {
		actorID := ActorIDFromUserID(tree.UserIDFromString(" actor-1 "))
		expectedActorID := ActorIDFromUserID(tree.UserIDFromString("actor-1"))
		Expect(actorID).To(Equal(expectedActorID))
		Expect(ActorIDFromUserID(actorID)).To(Equal(expectedActorID))

		service, err := NewService(ServiceOptions{Enabled: false})
		Expect(err).NotTo(HaveOccurred())
		called := false
		service.SetAfterSync(func() error {
			called = true
			return nil
		})
		Expect(service.afterSync()).To(Succeed())
		Expect(called).To(BeTrue())

		_, err = NewService(ServiceOptions{Enabled: true})
		Expect(err).To(MatchError(ErrTreeServiceRequired))
	})

	It("records watcher factory failures in status", func() {
		service, err := NewService(ServiceOptions{
			Enabled: true,
			RootDir: "/workspace",
			Tree:    &fakeTreeReconstructor{},
			Store:   &fakeRevisionStore{},
			WatcherFactory: func(string) (fileWatcher, error) {
				return nil, errWatcherFactoryFailed
			},
		})
		Expect(err).NotTo(HaveOccurred())

		err = service.StartWatcher(context.Background())
		Expect(err).To(MatchError(errWatcherFactoryFailed))
		status := service.Status()
		Expect(status).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"WatcherEnabled": BeTrue(),
			"WatcherRunning": BeFalse(),
			"LastError":      Equal(errWatcherFactoryFailed.Error()),
		}))
	})

	It("aggregates sync errors without adding blank fragments", func() {
		Expect(appendSyncError("", syncSecondaryError)).To(Equal(syncSecondaryError))
		Expect(appendSyncError(syncPrimaryError, "")).To(Equal(syncPrimaryError))
		Expect(appendSyncError(syncPrimaryError, syncSecondaryError)).To(Equal(syncPrimaryError + "; " + syncSecondaryError))
	})

	DescribeTable("filters managed markdown watcher paths",
		func(rootDir string, eventPath string, expectedPath string, expectedOK bool) {
			rel, ok := managedMarkdownEventPath(rootDir, eventPath)
			Expect(ok).To(Equal(expectedOK))
			Expect(rel).To(Equal(expectedPath))
		},
		Entry("absolute markdown under root", "/workspace", "/workspace/docs/page.md", "docs/page.md", true),
		Entry("uppercase markdown extension", "/workspace", "/workspace/Page.MD", "Page.MD", true),
		Entry("outside root remains absolute", "/workspace", "/outside/page.md", "/outside/page.md", true),
		Entry("git directory", "/workspace", "/workspace/.git/config.md", "", false),
		Entry("leafwiki directory", "/workspace", "/workspace/.leafwiki/state.md", "", false),
		Entry("hidden file", "/workspace", "/workspace/.page.md", "", false),
		Entry("backup suffix", "/workspace", "/workspace/page.md~", "", false),
		Entry("temporary suffix", "/workspace", "/workspace/page.tmp", "", false),
		Entry("download suffix", "/workspace", "/workspace/page.md.download", "", false),
		Entry("partial suffix", "/workspace", "/workspace/page.md.partial", "", false),
		Entry("chrome download suffix", "/workspace", "/workspace/page.md.crdownload", "", false),
	)

	It("uses workspace source paths before default route paths", func() {
		rootDir := GinkgoT().TempDir()
		Expect(os.MkdirAll(filepath.Join(rootDir, "Imported", "Section"), 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(rootDir, "Imported", "Section", "README.md"), []byte("# Readme\n"), 0o644)).To(Succeed())
		service := &Service{rootDir: rootDir}

		page := workspaceSyncEdgePage("page-1", "Imported Page", "imported-page", tree.NodeKindPage)
		page.WorkspaceSourcePath = tree.WorkspaceSourcePathFromString("Imported/Page.MD")
		Expect(pageMarkdownPath(page)).To(Equal("Imported/Page.MD"))
		Expect(service.currentPageMarkdownPath(page)).To(Equal("Imported/Page.MD"))

		section := workspaceSyncEdgePage("section-1", "Imported Section", "section", tree.NodeKindSection)
		section.WorkspaceSourcePath = tree.WorkspaceSourcePathFromString("Imported/Section")
		Expect(pageMarkdownPath(section)).To(Equal("Imported/Section/index.md"))
		Expect(service.currentPageMarkdownPath(section)).To(Equal("Imported/Section/README.md"))
	})

	It("finds historical page content by preferred path, route path, and metadata ID", func() {
		parent := &tree.PageNode{ID: "docs", Title: "Docs", Slug: "docs", Kind: tree.NodeKindSection}
		page := workspaceSyncEdgePage("page-1", "Page One", "page-one", tree.NodeKindPage)
		page.Parent = parent

		content, relPath, ok := contentForPageAtCommit("", page, map[string]string{
			"docs/page-one.md": workspaceSyncEdgeMarkdown("page-1", "Page One"),
		})
		Expect(ok).To(BeTrue())
		Expect(relPath).To(Equal("docs/page-one.md"))
		Expect(content).To(ContainSubstring("leafwiki_id: page-1"))

		content, relPath, ok = contentForPageAtCommit("", page, map[string]string{
			"archive/old.md": workspaceSyncEdgeMarkdown("page-1", "Historical Page One"),
		})
		Expect(ok).To(BeTrue())
		Expect(relPath).To(Equal("archive/old.md"))
		Expect(content).To(ContainSubstring("Historical Page One"))

		_, _, ok = contentForPageAtCommit("", page, map[string]string{
			"docs/page-one.md": workspaceSyncEdgeMarkdown("other-page", "Other"),
		})
		Expect(ok).To(BeFalse())
	})

	It("recognizes README fallback sections when deriving revision paths", func() {
		section := workspaceSyncEdgePage("section-1", "Docs", "docs", tree.NodeKindSection)

		Expect(isRevisionReadmeFallbackSection("docs/README.md", section, "docs")).To(BeTrue())
		Expect(isRevisionReadmeFallbackSection("docs/README.md", nil, "docs")).To(BeFalse())
		Expect(isRevisionReadmeFallbackSection("docs/README.md", workspaceSyncEdgePage("page-1", "Docs", "docs", tree.NodeKindPage), "docs")).To(BeFalse())
		Expect(isRevisionReadmeFallbackSection("docs/readme.md", section, "docs")).To(BeFalse())

		routePath, kind := revisionRoutePathAndKind("", "docs/README.md", section)
		Expect(routePath.FilesystemPath()).To(Equal("docs"))
		Expect(kind).To(Equal(tree.NodeKindSection))
	})

	It("extracts validation errors from generic sync errors", func() {
		rootDir := filepath.Join(GinkgoT().TempDir(), "workspace")
		service := &Service{rootDir: rootDir}

		errs := service.validationErrorsFromError(errors.New("open " + filepath.Join(rootDir, "docs", "page.md") + ": permission denied"))
		Expect(errs).To(ConsistOf(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"Code":     Equal(wikivalidation.IssueCodeWorkspaceSyncError),
			"Path":     Equal("docs/page.md"),
			"Severity": Equal(wikivalidation.IssueSeverityError),
		})))

		errs = service.validationErrorsFromError(errors.New("sync failed without markdown path"))
		Expect(errs).To(ConsistOf(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"Path": Equal("workspace"),
		})))

		Expect(validationErrorsIncludeActionableMarkdownCode([]ValidationError{
			{Code: ""},
			{Code: wikivalidation.IssueCodeWorkspaceScanError},
			{Code: wikivalidation.IssueCodeWorkspaceSyncError},
			{Code: wikivalidation.IssueCodeWorkspaceSyncValidation},
		})).To(BeFalse())
		Expect(validationErrorsIncludeActionableMarkdownCode([]ValidationError{
			{Code: wikivalidation.IssueCodeBrokenLink},
		})).To(BeTrue())
	})
})

func workspaceSyncEdgePage(id tree.PageID, title string, slug tree.Slug, kind tree.NodeKind) *tree.Page {
	GinkgoHelper()

	return &tree.Page{PageNode: &tree.PageNode{
		ID:    id,
		Title: title,
		Slug:  slug,
		Kind:  kind,
	}}
}

func workspaceSyncEdgeMarkdown(id tree.PageID, title string) string {
	GinkgoHelper()

	return "---\nleafwiki_id: " + id.MetadataValue() + "\nleafwiki_title: " + title + "\n---\n# " + title + "\n"
}

func validationErrorPaths(errors []ValidationError) []string {
	GinkgoHelper()

	paths := make([]string, 0, len(errors))
	for _, validationError := range errors {
		paths = append(paths, validationError.Path)
	}
	return paths
}
