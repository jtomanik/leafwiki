package workspacesync

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"sync/atomic"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	wikivalidation "github.com/perber/wiki/internal/core/markdownvalidation"
	"github.com/perber/wiki/internal/core/tree"
	testmatchers "github.com/perber/wiki/internal/test_utils/matchers"
	"github.com/perber/wiki/internal/workspacesync/gitrevisions"
)

var _ = Describe("workspace sync changed markdown tracking", Label("integration"), func() {
	It("canonicalizes section trailing slashes without repeat revisions", func() {
		dataDir := workspaceSyncTempDir()
		rootDir := filepath.Join(workspaceSyncTempDir(), "workspace")
		treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
		Expect(treeService.LoadTree()).To(Succeed())
		sourcePath := filepath.Join(rootDir, "docs", "a.md")
		writeMarkdownFile(sourcePath, `---
leafwiki_id: page-a
leafwiki_title: Page A
---
# Page A

[Sync](/docs/sync/)
`)
		writeMarkdownFile(filepath.Join(rootDir, "docs", "sync", "index.md"), `---
leafwiki_id: section-sync
leafwiki_title: Sync
---
# Sync
`)

		service, err := NewService(ServiceOptions{
			Enabled: true,
			DataDir: dataDir,
			RootDir: rootDir,
			Tree:    treeService,
		})
		Expect(err).To(Succeed())
		for range 2 {
			_, err := service.SyncNow(context.Background(), SyncRequest{
				Reason: ReasonExplicit,
				Source: SourceFilesystem,
				Actor:  PublicEditorActor(),
			})
			Expect(err).To(Succeed())
		}

		Expect(readFileStringGinkgo(sourcePath)).To(SatisfyAll(
			ContainSubstring("[Sync](/docs/sync)"),
			Not(ContainSubstring("/docs/sync/")),
		))
		snapshots, err := service.ListSnapshots(context.Background(), 10)
		Expect(err).To(Succeed())
		Expect(snapshots).To(HaveLen(2))
	})

	It("preserves the original changed markdown count when metadata writebacks are amended", func() {
		dataDir := workspaceSyncTempDir()
		rootDir := filepath.Join(workspaceSyncTempDir(), "workspace")
		treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
		Expect(treeService.LoadTree()).To(Succeed())
		writeMarkdownFile(filepath.Join(rootDir, "already-has-metadata.md"), `<!-- leafwiki
version: 1
page:
  id: page-ready
  title: Already Has Metadata
  created_at: 2026-06-07T10:00:00Z
  updated_at: 2026-06-07T10:00:00Z
-->

# Already Has Metadata
`)
		writeMarkdownFile(filepath.Join(rootDir, "needs-metadata.md"), "# Needs Metadata\n\nbody")

		service, err := NewService(ServiceOptions{
			Enabled: true,
			DataDir: dataDir,
			RootDir: rootDir,
			Tree:    treeService,
		})
		Expect(err).To(Succeed())

		_, err = service.SyncNow(context.Background(), SyncRequest{
			Reason: ReasonExplicit,
			Source: SourceFilesystem,
			Actor:  PublicEditorActor(),
		})
		Expect(err).To(Succeed())

		snapshots, err := service.ListSnapshots(context.Background(), 10)
		Expect(err).To(Succeed())
		Expect(snapshots).To(ConsistOf(HaveField("ChangedMarkdownCount", Equal(2))))
	})

	It("records the latest commit hash changed paths and tree reconstruction", func() {
		fakeTree := &fakeTreeReconstructor{}
		service, err := NewService(ServiceOptions{
			Enabled: true,
			Tree:    fakeTree,
			Store: &fakeRevisionStore{
				capture: &gitrevisions.Commit{
					Hash:                 newFixtureCommitHash("abc123"),
					ChangedMarkdownCount: 2,
					ChangedMarkdownPaths: []string{"docs/a.md", "docs/b.md"},
				},
			},
		})
		Expect(err).To(Succeed())

		status, err := service.SyncNow(context.Background(), SyncRequest{
			Reason: ReasonExplicit,
			Source: SourceFilesystem,
			Actor:  PublicEditorActor(),
		})
		Expect(err).To(Succeed())

		Expect(status).To(SatisfyAll(
			HaveField("LastCommitHash", Equal(newFixtureCommitHash("abc123"))),
			HaveField("RecentChangedMarkdownPaths", Equal([]string{"docs/a.md", "docs/b.md"})),
		))
		Expect(fakeTree.reconstructCount()).To(Equal(1))
	})
})

var _ = Describe("workspace sync startup and snapshot listing", Label("integration"), func() {
	It("logs each startup phase as it runs", func() {
		logs := &workspaceSyncStartupLogRecords{}
		service, err := NewService(ServiceOptions{
			Enabled: true,
			RootDir: workspaceSyncTempDir(),
			Tree:    &fakeTreeReconstructor{},
			Store: &fakeRevisionStore{
				capture: &gitrevisions.Commit{
					Hash:                 newFixtureCommitHash("startup-commit"),
					ChangedMarkdownPaths: []string{"docs/a.md"},
				},
			},
			Log: slog.New(logs),
		})
		Expect(err).To(Succeed())

		_, err = service.SyncNow(context.Background(), SyncRequest{
			Reason: ReasonStartup,
			Source: SourceFilesystem,
			Actor:  PublicEditorActor(),
		})
		Expect(err).To(Succeed())

		Expect(logs).To(reportStartupPhaseLifecycle(
			"capture_snapshot",
			"reconstruct_tree",
			"canonical_link_migration",
			"capture_writebacks",
			"validate_and_after_sync",
		))
	})

	It("propagates changed markdown path read errors while listing snapshots", func() {
		errChangedPathTrailerReadFailed := errors.New("path trailer read failed")
		service, err := NewService(ServiceOptions{
			Enabled: true,
			Tree:    &fakeTreeReconstructor{},
			Store: &fakeRevisionStore{
				commits: []gitrevisions.Commit{
					{Hash: newFixtureCommitHash("abc123"), ChangedMarkdownCount: 1},
				},
				changedPathsErr: errChangedPathTrailerReadFailed,
			},
		})
		Expect(err).To(Succeed())

		_, err = service.ListSnapshotPage(context.Background(), newFixtureCommitHash(""), 10)
		Expect(err).To(MatchError(errChangedPathTrailerReadFailed))
	})

	It("does not read changed paths past the requested snapshot page", func() {
		service, err := NewService(ServiceOptions{
			Enabled: true,
			Tree:    &fakeTreeReconstructor{},
			Store: &fakeRevisionStore{
				commits: []gitrevisions.Commit{
					{Hash: newFixtureCommitHash("returned"), ChangedMarkdownCount: 1},
					{Hash: newFixtureCommitHash("sentinel"), ChangedMarkdownCount: 1},
				},
				changedPaths:          map[CommitHash][]string{newFixtureCommitHash("returned"): {"returned.md"}},
				changedPathsErrByHash: map[CommitHash]error{newFixtureCommitHash("sentinel"): errors.New("sentinel diff should not be read")},
			},
		})
		Expect(err).To(Succeed())

		page, err := service.ListSnapshotPage(context.Background(), newFixtureCommitHash(""), 1)
		Expect(err).To(Succeed())
		Expect(page).To(SatisfyAll(
			HaveField("Snapshots", ConsistOf(HaveField("ID", Equal(newFixtureCommitHash("returned"))))),
			HaveField("NextCursor", Equal(newFixtureCommitHash("returned"))),
		))
	})
})

var _ = Describe("workspace sync status under snapshot listing", Label("integration"), func() {
	It("serves status while changed-path snapshot listing is blocked", func() {
		store := &fakeRevisionStore{
			capture: &gitrevisions.Commit{Hash: newFixtureCommitHash("sync-commit")},
			commits: []gitrevisions.Commit{
				{Hash: newFixtureCommitHash("slow-snapshot"), ChangedMarkdownCount: 1},
			},
			changedPaths:        map[CommitHash][]string{newFixtureCommitHash("slow-snapshot"): {"slow.md"}},
			changedPathsStarted: make(chan struct{}),
			unblockChangedPaths: make(chan struct{}),
		}
		service, err := NewService(ServiceOptions{
			Enabled: true,
			Tree:    &fakeTreeReconstructor{},
			Store:   store,
		})
		Expect(err).To(Succeed())

		changedPathsStarted := store.changedPathsStarted
		listDone := make(chan error, 1)
		go func() {
			_, err := service.ListSnapshotPage(context.Background(), newFixtureCommitHash(""), 1)
			listDone <- err
		}()
		Eventually(changedPathsStarted).Within(time.Second).Should(BeClosed())
		unblockChangedPaths := closeOnce(store.unblockChangedPaths)
		DeferCleanup(unblockChangedPaths)

		syncDone := make(chan error, 1)
		go func() {
			_, err := service.SyncNow(context.Background(), SyncRequest{
				Reason: ReasonExplicit,
				Source: SourceFilesystem,
				Actor:  PublicEditorActor(),
			})
			syncDone <- err
		}()

		statusDone := make(chan SyncStatus, 1)
		go func() {
			statusDone <- service.Status()
		}()

		Eventually(statusDone).WithTimeout(200 * time.Millisecond).Should(Receive(matchEnabledWorkspaceSyncStatus()))

		unblockChangedPaths()
		Eventually(listDone).Should(Receive(Succeed()))
		Eventually(syncDone).Should(Receive(Succeed()))
	})

	It("runs after-sync hooks when reconstruction only reports validation warnings", func() {
		dataDir := workspaceSyncTempDir()
		rootDir := filepath.Join(workspaceSyncTempDir(), "workspace")
		treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
		Expect(treeService.LoadTree()).To(Succeed())
		writeMarkdownFile(filepath.Join(rootDir, "valid-page.md"), `---
leafwiki_id: valid-page
leafwiki_title: Valid Page
---

# Valid Page
`)
		writeMarkdownFile(filepath.Join(rootDir, "!!!.md"), "---\nleafwiki_id: invalid-slug\nleafwiki_title: Invalid Slug\n---\n# Invalid Slug\n")
		var afterSyncCalls int32
		service, err := NewService(ServiceOptions{
			Enabled: true,
			DataDir: dataDir,
			RootDir: rootDir,
			Tree:    treeService,
			AfterSync: func() error {
				atomic.AddInt32(&afterSyncCalls, 1)
				return nil
			},
		})
		Expect(err).To(Succeed())

		status, err := service.SyncNow(context.Background(), SyncRequest{
			Reason: ReasonExplicit,
			Source: SourceFilesystem,
			Actor:  PublicEditorActor(),
		})
		Expect(err).To(Succeed())

		Expect(atomic.LoadInt32(&afterSyncCalls)).To(Equal(int32(1)))
		Expect(status.ValidationErrors).To(testmatchers.HaveValidationIssue(wikivalidation.IssueCodeInvalidSlug))
	})

	It("records validation error paths extracted from markdown reconstruction failures", func() {
		fakeTree := &fakeTreeReconstructor{err: errors.New(`duplicate leafwiki_id "dup" in /workspace/a.md and /workspace/docs/b.md`)}
		service, err := NewService(ServiceOptions{
			Enabled: true,
			RootDir: "/workspace",
			Tree:    fakeTree,
			Store:   &fakeRevisionStore{capture: &gitrevisions.Commit{Hash: newFixtureCommitHash("abc123")}},
		})
		Expect(err).To(Succeed())

		status, err := service.SyncNow(context.Background(), SyncRequest{
			Reason: ReasonExplicit,
			Source: SourceFilesystem,
			Actor:  PublicEditorActor(),
		})
		Expect(err).To(Succeed())

		Expect(status.ValidationErrors).To(ConsistOf(
			HaveField("Path", Equal("a.md")),
			HaveField("Path", Equal("docs/b.md")),
		))
	})
})
