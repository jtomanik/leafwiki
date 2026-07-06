package workspacesync

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"

	wikivalidation "github.com/perber/wiki/internal/core/markdownvalidation"
	"github.com/perber/wiki/internal/core/tree"
)

const (
	syncPrimaryError   = "primary"
	syncSecondaryError = "secondary"
)

var (
	errAfterSyncCallbackRan        = errors.New("after sync callback ran")
	errManagedMarkdownEventIgnored = errors.New("managed markdown event ignored")
	errHistoricalContentMissing    = errors.New("historical page content missing")
	errWatcherFactoryFailed        = errors.New("watcher factory failed")
)

var _ = Describe("workspace sync service edges", Label("unit"), func() {
	It("passes through actor IDs and validates enabled service dependencies", func() {
		actorID := ActorIDFromUserID(tree.UserIDFromString(" actor-1 "))
		expectedActorID := ActorIDFromUserID(tree.UserIDFromString("actor-1"))
		Expect(actorID).To(Equal(expectedActorID))
		Expect(ActorIDFromUserID(actorID)).To(Equal(expectedActorID))

		service, err := NewService(ServiceOptions{Enabled: false})
		Expect(err).To(Succeed())
		service.SetAfterSync(func() error {
			return errAfterSyncCallbackRan
		})
		Expect(service.afterSync()).To(MatchError(errAfterSyncCallbackRan))

		_, err = NewService(ServiceOptions{Enabled: true})
		Expect(err).To(MatchError(ErrTreeServiceRequired))
	})

	It("reports the watcher as stopped when the factory fails", func() {
		service, err := NewService(ServiceOptions{
			Enabled: true,
			RootDir: "/workspace",
			Tree:    &fakeTreeReconstructor{},
			Store:   &fakeRevisionStore{},
			WatcherFactory: func(string) (fileWatcher, error) {
				return nil, errWatcherFactoryFailed
			},
		})
		Expect(err).To(Succeed())

		err = service.StartWatcher(context.Background())
		Expect(err).To(MatchError(errWatcherFactoryFailed))
		status := service.Status()
		Expect(status).To(matchWatcherFactoryFailureStatus())
	})

	It("aggregates sync errors without adding blank fragments", func() {
		Expect(appendSyncError("", syncSecondaryError)).To(matchSyncErrorFragments(syncSecondaryError))
		Expect(appendSyncError(syncPrimaryError, "")).To(matchSyncErrorFragments(syncPrimaryError))
		Expect(appendSyncError(syncPrimaryError, syncSecondaryError)).To(matchSyncErrorFragments(syncPrimaryError, syncSecondaryError))
	})

	DescribeTable("accepts managed markdown watcher paths",
		func(rootDir string, eventPath string, expectedPath string) {
			rel, err := managedMarkdownEventPathResult(rootDir, eventPath)
			Expect(err).To(Succeed())
			Expect(rel).To(Equal(expectedPath))
		},
		Entry("absolute markdown under root", "/workspace", "/workspace/docs/page.md", "docs/page.md"),
		Entry("uppercase markdown extension", "/workspace", "/workspace/Page.MD", "Page.MD"),
		Entry("outside root remains absolute", "/workspace", "/outside/page.md", "/outside/page.md"),
	)

	DescribeTable("rejects unmanaged watcher paths",
		func(rootDir string, eventPath string) {
			_, err := managedMarkdownEventPathResult(rootDir, eventPath)
			Expect(err).To(MatchError(errManagedMarkdownEventIgnored))
		},
		Entry("git directory", "/workspace", "/workspace/.git/config.md"),
		Entry("leafwiki directory", "/workspace", "/workspace/.leafwiki/state.md"),
		Entry("hidden file", "/workspace", "/workspace/.page.md"),
		Entry("backup suffix", "/workspace", "/workspace/page.md~"),
		Entry("temporary suffix", "/workspace", "/workspace/page.tmp"),
		Entry("download suffix", "/workspace", "/workspace/page.md.download"),
		Entry("partial suffix", "/workspace", "/workspace/page.md.partial"),
		Entry("chrome download suffix", "/workspace", "/workspace/page.md.crdownload"),
	)

	It("uses workspace source paths before default route paths", func() {
		rootDir := workspaceSyncTempDir()
		Expect(os.MkdirAll(filepath.Join(rootDir, "Imported", "Section"), 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(rootDir, "Imported", "Section", "README.md"), []byte("# Readme\n"), 0o644)).To(Succeed())
		service := &Service{rootDir: rootDir}

		page := workspaceSyncEdgePage(newFixturePageID("page-1"), "Imported Page", newFixtureSlug("imported-page"), tree.NodeKindPage)
		page.WorkspaceSourcePath = tree.WorkspaceSourcePathFromString("Imported/Page.MD")
		Expect(pageMarkdownPath(page)).To(Equal("Imported/Page.MD"))
		Expect(service.currentPageMarkdownPath(page)).To(Equal("Imported/Page.MD"))

		section := workspaceSyncEdgePage(newFixturePageID("section-1"), "Imported Section", newFixtureSlug("section"), tree.NodeKindSection)
		section.WorkspaceSourcePath = tree.WorkspaceSourcePathFromString("Imported/Section")
		Expect(pageMarkdownPath(section)).To(Equal("Imported/Section/index.md"))
		Expect(service.currentPageMarkdownPath(section)).To(Equal("Imported/Section/README.md"))
	})

	It("finds historical page content by preferred path, route path, and metadata ID", func() {
		parent := &tree.PageNode{ID: newFixturePageID("docs"), Title: "Docs", Slug: newFixtureSlug("docs"), Kind: tree.NodeKindSection}
		page := workspaceSyncEdgePage(newFixturePageID("page-1"), "Page One", newFixtureSlug("page-one"), tree.NodeKindPage)
		page.Parent = parent

		result, err := contentForPageAtCommitResult("", page, map[string]string{
			"docs/page-one.md": workspaceSyncEdgeMarkdown(newFixturePageID("page-1"), "Page One"),
		})
		Expect(err).To(Succeed())
		Expect(result).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"RelPath": Equal("docs/page-one.md"),
			"Content": ContainSubstring("leafwiki_id: page-1"),
		}))

		result, err = contentForPageAtCommitResult("", page, map[string]string{
			"archive/old.md": workspaceSyncEdgeMarkdown(newFixturePageID("page-1"), "Historical Page One"),
		})
		Expect(err).To(Succeed())
		Expect(result).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"RelPath": Equal("archive/old.md"),
			"Content": ContainSubstring("Historical Page One"),
		}))

		_, err = contentForPageAtCommitResult("", page, map[string]string{
			"docs/page-one.md": workspaceSyncEdgeMarkdown(newFixturePageID("other-page"), "Other"),
		})
		Expect(err).To(MatchError(errHistoricalContentMissing))
	})

	It("recognizes README fallback sections when deriving revision paths", func() {
		section := workspaceSyncEdgePage(newFixturePageID("section-1"), "Docs", newFixtureSlug("docs"), tree.NodeKindSection)

		Expect(revisionReadmeFallbackObservationFor("docs/README.md", section, "docs")).To(Equal(revisionReadmeFallbackObservation{
			Outcome:       revisionReadmeFallbackAccepted,
			RelPath:       tree.MarkdownPathFromString("docs/README.md"),
			PageKind:      tree.NodeKindSection,
			PageRoutePath: newFixtureRoutePath("docs"),
			Directory:     newFixtureRoutePath("docs"),
		}))
		Expect(revisionReadmeFallbackObservationFor("docs/README.md", nil, "docs")).To(Equal(revisionReadmeFallbackObservation{
			Outcome:   revisionReadmeFallbackRejected,
			RelPath:   tree.MarkdownPathFromString("docs/README.md"),
			Directory: newFixtureRoutePath("docs"),
		}))
		Expect(revisionReadmeFallbackObservationFor("docs/README.md", workspaceSyncEdgePage(newFixturePageID("page-1"), "Docs", newFixtureSlug("docs"), tree.NodeKindPage), "docs")).To(Equal(revisionReadmeFallbackObservation{
			Outcome:       revisionReadmeFallbackRejected,
			RelPath:       tree.MarkdownPathFromString("docs/README.md"),
			PageKind:      tree.NodeKindPage,
			PageRoutePath: newFixtureRoutePath("docs"),
			Directory:     newFixtureRoutePath("docs"),
		}))
		Expect(revisionReadmeFallbackObservationFor("docs/readme.md", section, "docs")).To(Equal(revisionReadmeFallbackObservation{
			Outcome:       revisionReadmeFallbackRejected,
			RelPath:       tree.MarkdownPathFromString("docs/readme.md"),
			PageKind:      tree.NodeKindSection,
			PageRoutePath: newFixtureRoutePath("docs"),
			Directory:     newFixtureRoutePath("docs"),
		}))

		Expect(revisionRouteObservationFor("", "docs/README.md", section)).To(Equal(revisionRouteObservation{
			RoutePath: newFixtureRoutePath("docs"),
			Kind:      tree.NodeKindSection,
		}))
	})

	It("extracts validation errors from generic sync errors", func() {
		rootDir := filepath.Join(workspaceSyncTempDir(), "workspace")
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

		Expect([]ValidationError{
			{Code: newFixtureValidationIssueCode("")},
			{Code: wikivalidation.IssueCodeWorkspaceScanError},
			{Code: wikivalidation.IssueCodeWorkspaceSyncError},
			{Code: wikivalidation.IssueCodeWorkspaceSyncValidation},
		}).To(reportWorkspaceSyncValidationActionability(workspaceSyncValidationIgnored))
		Expect([]ValidationError{
			{Code: wikivalidation.IssueCodeBrokenLink},
		}).To(reportWorkspaceSyncValidationActionability(workspaceSyncValidationActionable))
	})
})

func matchSyncErrorFragments(fragments ...string) types.GomegaMatcher {
	return WithTransform(func(status string) []string {
		if strings.TrimSpace(status) == "" {
			return nil
		}
		return strings.Split(status, "; ")
	}, Equal(fragments))
}

func managedMarkdownEventPathResult(rootDir string, eventPath string) (string, error) {
	GinkgoHelper()

	rel, ok := managedMarkdownEventPath(rootDir, eventPath)
	if !ok {
		return "", errManagedMarkdownEventIgnored
	}
	return rel, nil
}

type historicalContentResult struct {
	Content string
	RelPath string
}

type revisionRouteObservation struct {
	RoutePath tree.RoutePath
	Kind      tree.NodeKind
}

type revisionReadmeFallbackOutcome uint8

const (
	revisionReadmeFallbackRejected revisionReadmeFallbackOutcome = iota
	revisionReadmeFallbackAccepted
)

type revisionReadmeFallbackObservation struct {
	Outcome       revisionReadmeFallbackOutcome
	RelPath       tree.MarkdownPath
	PageKind      tree.NodeKind
	PageRoutePath tree.RoutePath
	Directory     tree.RoutePath
}

func revisionReadmeFallbackObservationFor(relPath string, page *tree.Page, dir string) revisionReadmeFallbackObservation {
	GinkgoHelper()

	outcome := revisionReadmeFallbackRejected
	if isRevisionReadmeFallbackSection(relPath, page, dir) {
		outcome = revisionReadmeFallbackAccepted
	}
	observation := revisionReadmeFallbackObservation{
		Outcome:   outcome,
		RelPath:   tree.MarkdownPathFromString(relPath),
		Directory: tree.RoutePathFromString(dir).Clean(),
	}
	if page != nil && page.PageNode != nil {
		observation.PageKind = page.Kind
		observation.PageRoutePath = tree.RoutePathFromString(strings.Trim(page.CalculatePath(), "/")).Clean()
	}
	return observation
}

func revisionRouteObservationFor(rootDir string, relPath string, page *tree.Page) revisionRouteObservation {
	GinkgoHelper()

	routePath, kind := revisionRoutePathAndKind(rootDir, relPath, page)
	return revisionRouteObservation{
		RoutePath: routePath,
		Kind:      kind,
	}
}

func contentForPageAtCommitResult(rootDir string, page *tree.Page, files map[string]string) (historicalContentResult, error) {
	GinkgoHelper()

	content, relPath, ok := contentForPageAtCommit(rootDir, page, files)
	if !ok {
		return historicalContentResult{}, errHistoricalContentMissing
	}
	return historicalContentResult{
		Content: content,
		RelPath: relPath,
	}, nil
}

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
