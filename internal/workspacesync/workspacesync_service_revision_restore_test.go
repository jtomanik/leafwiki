package workspacesync

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"

	wikivalidation "github.com/perber/wiki/internal/core/markdownvalidation"
	"github.com/perber/wiki/internal/core/tree"
	"github.com/perber/wiki/internal/workspacesync/gitrevisions"
)

var _ = Describe("workspace sync revision and restore helpers", Label("unit"), func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	It("returns snapshot pages for disabled services and propagates list failures", func() {
		disabled := &Service{}
		page, err := disabled.ListSnapshotPage(ctx, "", 0)
		Expect(err).To(Succeed())
		Expect(page.Snapshots).To(BeEmpty())

		listErr := errors.New("list failed")
		store := &fakeRevisionStore{listErr: listErr}
		service := workspaceSyncServiceHarness(store, &fakeTreeReconstructor{})
		_, err = service.ListSnapshots(ctx, 1)
		Expect(err).To(MatchError(listErr))

		store = &fakeRevisionStore{
			commits: []gitrevisions.Commit{
				workspaceSyncServiceCommitValue("c1", "a.md"),
				workspaceSyncServiceCommitValue("c2", "b.md"),
			},
			changedPaths: map[CommitHash][]string{
				"c1": {"a.md"},
				"c2": {"b.md"},
			},
		}
		service = workspaceSyncServiceHarness(store, &fakeTreeReconstructor{})
		list, err := service.ListSnapshotPage(ctx, "", 1)
		Expect(err).To(Succeed())
		Expect(list).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"Snapshots":  HaveLen(1),
			"NextCursor": Equal(CommitHashFromString("c1")),
		}))

		changedPathsErr := errors.New("changed paths failed")
		store.changedPathsErrByHash = map[CommitHash]error{"c1": changedPathsErr}
		_, err = service.ListSnapshotPage(ctx, "", 1)
		Expect(err).To(MatchError(changedPathsErr))
	})

	It("lists page revisions with cursor scans and propagates content lookup failures", func() {
		page := workspaceSyncEdgePage("page-1", "Page", "page", tree.NodeKindPage)
		disabled := &Service{}
		revisions, err := disabled.ListPageRevisions(ctx, page, "", 0)
		Expect(err).To(Succeed())
		Expect(revisions.Revisions).To(BeEmpty())

		store := &fakeRevisionStore{
			commits: []gitrevisions.Commit{
				workspaceSyncServiceCommitValue("cursor"),
				workspaceSyncServiceCommitValue("c1"),
				workspaceSyncServiceCommitValue("c2"),
			},
			changedContents: map[CommitHash]map[string]string{
				"c1": {"page.md": workspaceSyncEdgeMarkdown("page-1", "Page v1")},
				"c2": {"page.md": workspaceSyncEdgeMarkdown("page-1", "Page v2")},
			},
		}
		service := workspaceSyncServiceHarness(store, &fakeTreeReconstructor{})
		list, err := service.ListPageRevisions(ctx, page, "cursor", 1)
		Expect(err).To(Succeed())
		Expect(list).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"Revisions":  HaveLen(1),
			"NextCursor": Equal("c1"),
		}))
		Expect(store.scannedCommits).To(Equal(3))

		changedContentsErr := errors.New("changed contents failed")
		store.changedContentsErr = changedContentsErr
		_, err = service.ListPageRevisions(ctx, page, "", 1)
		Expect(err).To(MatchError(changedContentsErr))
	})

	It("restores workspaces with default source metadata and propagates restore failures", func() {
		disabled := &Service{}
		_, err := disabled.RestoreWorkspace(ctx, "commit", PublicEditorActor())
		Expect(err).To(MatchError(ErrWorkspaceSyncDisabled))

		restoreErr := errors.New("restore failed")
		store := &fakeRevisionStore{capture: workspaceSyncServiceCommit("restore-error"), restoreWorkspaceErr: restoreErr}
		service := workspaceSyncServiceHarness(store, &fakeTreeReconstructor{})
		_, err = service.RestoreWorkspaceWithSource(ctx, "commit", PublicEditorActor(), "")
		Expect(err).To(MatchError(restoreErr))
		Expect(store.restoreWorkspaceRequests).To(ContainElement(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"Source": Equal(SourceSystem),
		})))

		store = &fakeRevisionStore{capture: workspaceSyncServiceCommit("restore-reconstruct", "docs/a.md")}
		restoreReconstructErr := errors.New("restore reconstruct failed")
		service = workspaceSyncServiceHarness(store, &fakeTreeReconstructor{err: restoreReconstructErr})
		status, err := service.RestoreWorkspaceWithSource(ctx, "commit", PublicEditorActor(), SourceMCP)
		Expect(err).To(Succeed())
		Expect(status.ValidationErrors).To(ConsistOf(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"Code":      Equal(wikivalidation.IssueCodeWorkspaceSyncError),
			"MessageID": Equal(wikivalidation.IssueCodeWorkspaceSyncError.MessageID()),
			"Path":      Equal("workspace"),
			"Severity":  Equal(wikivalidation.IssueSeverityError),
		})))

		restoreAmendErr := errors.New("restore amend failed")
		store = &fakeRevisionStore{capture: workspaceSyncServiceCommit("restore-amend", "docs/a.md"), amendErr: restoreAmendErr}
		service = workspaceSyncServiceHarness(store, &fakeTreeReconstructor{})
		_, err = service.RestoreWorkspaceWithSource(ctx, "commit", PublicEditorActor(), SourceMCP)
		Expect(err).To(MatchError(restoreAmendErr))

		store = &fakeRevisionStore{capture: workspaceSyncServiceCommit("restore-after", "docs/a.md")}
		service = workspaceSyncServiceHarness(store, &fakeTreeReconstructor{})
		restoreAfterErr := errors.New("restore after failed")
		service.SetAfterSync(func() error { return restoreAfterErr })
		_, err = service.RestoreWorkspaceWithSource(ctx, "commit", PublicEditorActor(), SourceMCP)
		Expect(err).To(MatchError(restoreAfterErr))
	})

	It("returns page snapshots and document restore errors with source metadata", func() {
		page := workspaceSyncEdgePage("page-1", "Page", "page", tree.NodeKindPage)
		disabled := &Service{}
		_, err := disabled.GetPageRevisionSnapshot(ctx, page, "commit")
		Expect(err).To(MatchError(ErrWorkspaceSyncDisabled))
		_, err = disabled.RestoreDocument(ctx, page, "commit", PublicEditorActor())
		Expect(err).To(MatchError(ErrWorkspaceSyncDisabled))

		changedContentsErr := errors.New("changed contents failed")
		store := &fakeRevisionStore{changedContentsErr: changedContentsErr}
		service := workspaceSyncServiceHarness(store, &fakeTreeReconstructor{})
		_, err = service.GetPageRevisionSnapshot(ctx, page, "commit")
		Expect(err).To(MatchError(changedContentsErr))
		_, err = service.RestoreDocumentWithSource(ctx, page, "commit", PublicEditorActor(), "")
		Expect(err).To(MatchError(changedContentsErr))

		store = &fakeRevisionStore{changedContents: map[CommitHash]map[string]string{"commit": {"other.md": workspaceSyncEdgeMarkdown("other", "Other")}}}
		service = workspaceSyncServiceHarness(store, &fakeTreeReconstructor{})
		_, err = service.GetPageRevisionSnapshot(ctx, page, "commit")
		Expect(err).To(MatchError(ErrWorkspaceSyncDocumentUnchanged))
		_, err = service.RestoreDocumentWithSource(ctx, page, "commit", PublicEditorActor(), SourceMCP)
		Expect(err).To(MatchError(ErrWorkspaceSyncDocumentUnchanged))

		store = &fakeRevisionStore{
			changedContents: map[CommitHash]map[string]string{"commit": {"page.md": workspaceSyncEdgeMarkdown("page-1", "Page")}},
			getCommitErr:    errors.New("get commit failed"),
		}
		getCommitErr := store.getCommitErr
		service = workspaceSyncServiceHarness(store, &fakeTreeReconstructor{})
		_, err = service.GetPageRevisionSnapshot(ctx, page, "commit")
		Expect(err).To(MatchError(getCommitErr))

		restoreDocumentErr := errors.New("restore document failed")
		store = &fakeRevisionStore{
			capture:                   workspaceSyncServiceCommit("restore-doc", "page.md"),
			changedContents:           map[CommitHash]map[string]string{"commit": {"page.md": workspaceSyncEdgeMarkdown("page-1", "Page")}},
			restoreDocumentContentErr: restoreDocumentErr,
		}
		service = workspaceSyncServiceHarness(store, &fakeTreeReconstructor{})
		_, err = service.RestoreDocumentWithSource(ctx, page, "commit", PublicEditorActor(), "")
		Expect(err).To(MatchError(restoreDocumentErr))
		Expect(store.restoreDocumentContentRequests).To(ContainElement(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"Source": Equal(SourceSystem),
		})))

		store.restoreDocumentContentErr = nil
		documentReconstructErr := errors.New("document reconstruct failed")
		service = workspaceSyncServiceHarness(store, &fakeTreeReconstructor{err: documentReconstructErr})
		_, err = service.RestoreDocumentWithSource(ctx, page, "commit", PublicEditorActor(), SourceMCP)
		Expect(err).To(MatchError(documentReconstructErr))

		documentAmendErr := errors.New("document amend failed")
		store = &fakeRevisionStore{
			capture:         workspaceSyncServiceCommit("restore-doc-amend", "page.md"),
			changedContents: map[CommitHash]map[string]string{"commit": {"page.md": workspaceSyncEdgeMarkdown("page-1", "Page")}},
			amendErr:        documentAmendErr,
		}
		service = workspaceSyncServiceHarness(store, &fakeTreeReconstructor{})
		_, err = service.RestoreDocumentWithSource(ctx, page, "commit", PublicEditorActor(), SourceMCP)
		Expect(err).To(MatchError(documentAmendErr))

		store = &fakeRevisionStore{
			capture:         workspaceSyncServiceCommit("restore-doc-after", "page.md"),
			changedContents: map[CommitHash]map[string]string{"commit": {"page.md": workspaceSyncEdgeMarkdown("page-1", "Page")}},
		}
		service = workspaceSyncServiceHarness(store, &fakeTreeReconstructor{})
		documentAfterErr := errors.New("document after failed")
		service.SetAfterSync(func() error { return documentAfterErr })
		_, err = service.RestoreDocumentWithSource(ctx, page, "commit", PublicEditorActor(), SourceMCP)
		Expect(err).To(MatchError(documentAfterErr))
	})

	It("applies default list limits and resolves fallback route helpers", func() {
		service := workspaceSyncServiceHarness(&fakeRevisionStore{}, &fakeTreeReconstructor{})
		snapshots, err := service.ListSnapshotPage(ctx, "", 0)
		Expect(err).NotTo(HaveOccurred())
		Expect(snapshots.Snapshots).To(BeEmpty())

		page := workspaceSyncEdgePage("page-1", "Page", "page", tree.NodeKindPage)
		revisions, err := service.ListPageRevisions(ctx, page, "", 0)
		Expect(err).NotTo(HaveOccurred())
		Expect(revisions.Revisions).To(BeEmpty())

		Expect(service.captureWritebacksLocked(ctx, gitrevisions.CommitRequest{}, nil, true)).To(Succeed())
		changedContentsErr := errors.New("changed contents failed")
		service.store = &fakeRevisionStore{changedContentsErr: changedContentsErr}
		Expect(service.captureWritebacksLocked(ctx, gitrevisions.CommitRequest{}, workspaceSyncServiceCommit("writeback-error"), true)).To(MatchError(changedContentsErr))

		section := workspaceSyncEdgePage("!!!", "Invalid", "!!!", tree.NodeKindSection)
		routePath, kind := revisionRoutePathAndKind("", "!!!/README.md", section)
		Expect(routePath.FilesystemPath()).To(Equal("!!!"))
		Expect(kind).To(Equal(tree.NodeKindSection))
		routePath, kind = revisionRoutePathAndKind("", "!!!/index.md", page)
		Expect(routePath.FilesystemPath()).To(Equal("!!!"))
		Expect(kind).To(Equal(tree.NodeKindSection))
		routePath, kind = revisionRoutePathAndKind("", "!!!/page.md", page)
		Expect(routePath.FilesystemPath()).To(Equal("!!!/page"))
		Expect(kind).To(Equal(tree.NodeKindPage))

		result, err := contentForPageAtCommitPathResult("", page, "preferred.md", map[string]string{
			"bad.md": "<!-- leafwiki\nnot yaml\n-->\n# Broken\n",
			"old.md": workspaceSyncEdgeMarkdown("page-1", "Old Page"),
		})
		Expect(err).To(Succeed())
		Expect(result).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"RelPath": Equal("old.md"),
			"Content": ContainSubstring("Old Page"),
		}))

		result, err = changedContentForPageAtCommitResult("", page, "preferred.md", map[string]string{
			"bad.md": "<!-- leafwiki\nnot yaml\n-->\n# Broken\n",
			"old.md": workspaceSyncEdgeMarkdown("page-1", "Old Page"),
		})
		Expect(err).To(Succeed())
		Expect(result).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"RelPath": Equal("old.md"),
			"Content": ContainSubstring("Old Page"),
		}))

		_, err = managedMarkdownEventPathResult("/workspace", " ")
		Expect(err).To(MatchError(errManagedMarkdownEventIgnored))
		_, err = managedMarkdownEventPathResult("/workspace", ".")
		Expect(err).To(MatchError(errManagedMarkdownEventIgnored))
		_, err = managedMarkdownEventPathResult("/workspace", "../outside.md")
		Expect(err).To(MatchError(errManagedMarkdownEventIgnored))
		_, err = managedMarkdownEventPathResult("/workspace", "notes.txt")
		Expect(err).To(MatchError(errManagedMarkdownEventIgnored))

		timer := time.NewTimer(time.Hour)
		stopWorkspacesyncTimer(timer)
		timer = time.NewTimer(time.Nanosecond)
		Eventually(timer.C).Should(Receive())
		stopWorkspacesyncTimer(timer)
		timer = time.NewTimer(time.Nanosecond)
		Eventually(timer.C).Should(Receive())
		drainWorkspacesyncTimer(timer)

		preserveWorkspacesyncServiceSeams()
		rootDir := workspaceSyncTempDir()
		Expect(os.MkdirAll(filepath.Join(rootDir, "docs"), 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(rootDir, "docs", "README.md"), []byte("# Readme\n"), 0o644)).To(Succeed())
		workspacesyncWalkDir = func(string, fs.WalkDirFunc) error {
			return errors.New("route scan failed")
		}
		sectionWithReadme := workspaceSyncEdgePage("docs", "Docs", "docs", tree.NodeKindSection)
		Expect((&Service{rootDir: rootDir}).currentPageMarkdownPath(sectionWithReadme)).To(Equal("docs/README.md"))

		workspacesyncWalkDir = func(root string, fn fs.WalkDirFunc) error {
			Expect(fn(root, workspacesyncServiceDirEntry{name: filepath.Base(root), dir: true}, nil)).To(Succeed())
			Expect(fn(filepath.Join(root, "notes.txt"), workspacesyncServiceDirEntry{name: "notes.txt"}, nil)).To(Succeed())
			Expect(fn(filepath.Join(root, ".hidden"), workspacesyncServiceDirEntry{name: ".hidden", dir: true}, nil)).To(Equal(filepath.SkipDir))
			return nil
		}
		_, err = currentWorkspaceMarkdownPathByRouteResult(&Service{rootDir: rootDir}, page)
		Expect(err).To(MatchError(errChangedContentMissing))

		workspacesyncWalkDir = func(root string, fn fs.WalkDirFunc) error {
			return fn(filepath.Join(root, "page.md"), workspacesyncServiceDirEntry{name: "page.md"}, nil)
		}
		workspacesyncRel = func(string, string) (string, error) {
			return "", errors.New("route rel failed")
		}
		_, err = currentWorkspaceMarkdownPathByRouteResult(&Service{rootDir: rootDir}, page)
		Expect(err).To(MatchError(errChangedContentMissing))

		routePage := workspaceSyncEdgePage("page-1", "Page", "page", tree.NodeKindPage)
		routePage.Parent = &tree.PageNode{ID: "docs", Title: "Docs", Slug: "docs", Kind: tree.NodeKindSection}
		result, err = contentForPageAtCommitPathResult(rootDir, routePage, "preferred.md", map[string]string{
			"docs/page.md": "# Page without metadata\n",
		})
		Expect(err).To(Succeed())
		Expect(result).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"RelPath": Equal("docs/page.md"),
			"Content": ContainSubstring("without metadata"),
		}))

		_, err = changedContentForPageAtCommitResult(rootDir, routePage, "preferred.md", map[string]string{})
		Expect(err).To(MatchError(errChangedContentMissing))
		result, err = changedContentForPageAtCommitResult(rootDir, routePage, "preferred.md", map[string]string{
			"docs/page.md": "# Changed without metadata\n",
		})
		Expect(err).To(Succeed())
		Expect(result).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"RelPath": Equal("docs/page.md"),
			"Content": ContainSubstring("Changed"),
		}))

		Expect((&Service{}).validationErrorsFromError(nil)).To(BeNil())
		Expect(markdownPathsInError(rootDir, "notes.txt has no markdown token")).To(BeEmpty())
	})

})
