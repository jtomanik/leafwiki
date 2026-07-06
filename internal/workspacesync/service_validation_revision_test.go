package workspacesync

import (
	"context"
	"errors"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	wikivalidation "github.com/perber/wiki/internal/core/markdownvalidation"
	"github.com/perber/wiki/internal/core/tree"
	testmatchers "github.com/perber/wiki/internal/test_utils/matchers"
	"github.com/perber/wiki/internal/workspacesync/gitrevisions"
)

var _ = Describe("workspace sync validation and revision metadata", Label("integration"), func() {
	It("reports duplicate canonical page IDs as structured validation issues", func() {
		dataDir := workspaceSyncTempDir()
		rootDir := filepath.Join(workspaceSyncTempDir(), "workspace")
		treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
		writeMarkdownFile(filepath.Join(rootDir, "duplicate-a.md"), `<!-- leafwiki
version: 1
page:
  id: duplicate-page-id
  title: Duplicate A
-->

# Duplicate A
`)
		writeMarkdownFile(filepath.Join(rootDir, "duplicate-b.md"), `<!-- leafwiki
version: 1
page:
  id: duplicate-page-id
  title: Duplicate B
-->

# Duplicate B
`)
		service, err := NewService(ServiceOptions{
			Enabled: true,
			DataDir: dataDir,
			RootDir: rootDir,
			Tree:    treeService,
		})
		Expect(err).To(Succeed())

		status, err := service.SyncNow(context.Background(), SyncRequest{
			Reason: ReasonExplicit,
			Source: SourceFilesystem,
			Actor:  PublicEditorActor(),
		})
		Expect(err).To(Succeed())

		Expect(status.ValidationErrors).To(SatisfyAll(
			testmatchers.HaveValidationIssue(wikivalidation.IssueCodeDuplicateLeafwikiID),
			ContainElement(SatisfyAll(
				HaveField("Path", Equal("duplicate-b.md")),
				HaveField("Severity", Equal(wikivalidation.IssueSeverityError)),
			)),
		))
	})

	It("records primary and additional actors on the captured git commit", func() {
		dataDir := workspaceSyncTempDir()
		rootDir := filepath.Join(workspaceSyncTempDir(), "workspace")
		treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
		writeMarkdownFile(filepath.Join(rootDir, "page.md"), "---\nleafwiki_id: page\nleafwiki_title: Page\n---\n# Page\n")
		store, err := gitrevisions.Open(gitrevisions.StoreOptions{DataDir: dataDir, RootDir: rootDir})
		Expect(err).To(Succeed())
		service, err := NewService(ServiceOptions{
			Enabled: true,
			DataDir: dataDir,
			RootDir: rootDir,
			Tree:    treeService,
			Store:   store,
		})
		Expect(err).To(Succeed())

		status, err := service.SyncNow(context.Background(), SyncRequest{
			Reason: ReasonExplicit,
			Source: SourceWeb,
			Actor:  Actor{ID: newFixtureActorID("alice"), Name: "Alice", Email: "alice@example.test"},
			AdditionalActors: []Actor{
				{ID: newFixtureActorID("bob"), Name: "Bob", Email: "bob@example.test"},
			},
		})
		Expect(err).To(Succeed())

		commit, err := store.GetCommit(context.Background(), status.LastCommitHash)
		Expect(err).To(Succeed())
		Expect(commit).To(SatisfyAll(
			HaveField("AuthorID", Equal(newFixtureActorID("alice"))),
			HaveField("ActorIDs", WithTransform(gitRevisionActorIDStrings, Equal([]string{"alice", "bob"}))),
		))
	})

	It("stops before tree reconstruction when git capture fails", func() {
		captureErr := errors.New("git storage read-only")
		fakeTree := &fakeTreeReconstructor{}
		service, err := NewService(ServiceOptions{
			Enabled: true,
			Tree:    fakeTree,
			Store:   &fakeRevisionStore{captureErr: captureErr},
		})
		Expect(err).To(Succeed())

		_, err = service.SyncNow(context.Background(), SyncRequest{
			Reason: ReasonExplicit,
			Source: SourceFilesystem,
			Actor:  PublicEditorActor(),
		})
		Expect(err).To(MatchError(captureErr))
		Expect(fakeTree.reconstructCount()).To(BeZero())
	})

	It("uses commit author metadata when listing page revisions", func() {
		createdAt := time.Date(2026, 6, 7, 10, 0, 0, 0, time.UTC)
		page := &tree.Page{PageNode: &tree.PageNode{
			ID:    newFixturePageID("page-1"),
			Title: "Page One",
			Slug:  newFixtureSlug("page-one"),
			Kind:  tree.NodeKindPage,
		}}
		store := &fakeRevisionStore{
			commits: []gitrevisions.Commit{{
				Hash:       newFixtureCommitHash("commit-1"),
				Message:    "LeafWiki workspace sync",
				AuthorID:   newFixtureActorID("alice"),
				AuthorName: "Alice",
				CreatedAt:  createdAt,
			}},
			filesAt:      map[CommitHash]map[string]string{newFixtureCommitHash("commit-1"): {"page-one.md": "# Page One\n"}},
			changedPaths: map[CommitHash][]string{newFixtureCommitHash("commit-1"): {"page-one.md"}},
		}
		service, err := NewService(ServiceOptions{
			Enabled: true,
			Tree:    &fakeTreeReconstructor{},
			Store:   store,
		})
		Expect(err).To(Succeed())

		result, err := service.ListPageRevisions(context.Background(), page, "", 10)
		Expect(err).To(Succeed())
		Expect(result.Revisions).To(ConsistOf(SatisfyAll(
			HaveField("AuthorID", Equal("alice")),
			HaveField("CreatedAt", Equal(createdAt)),
		)))
	})
})
