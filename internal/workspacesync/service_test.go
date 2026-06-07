package workspacesync

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/perber/wiki/internal/core/revision"
	"github.com/perber/wiki/internal/core/tree"
	"github.com/perber/wiki/internal/workspacesync/gitrevisions"
)

func TestServiceSyncNowCommitsAndReconstructsDirectMarkdownCreate(t *testing.T) {
	dataDir := t.TempDir()
	rootDir := filepath.Join(t.TempDir(), "workspace")
	treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
	if err := treeService.LoadTree(); err != nil {
		t.Fatalf("LoadTree: %v", err)
	}
	writeMarkdown(t, filepath.Join(rootDir, "docs", "new-page.md"), `---
leafwiki_id: page-new
leafwiki_title: New Page
---

# New Page

content`)

	service, err := NewService(ServiceOptions{
		Enabled: true,
		DataDir: dataDir,
		RootDir: rootDir,
		Tree:    treeService,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	status, err := service.SyncNow(context.Background(), SyncRequest{
		Reason: ReasonExplicit,
		Source: SourceFilesystem,
		Actor:  PublicEditorActor(),
	})
	if err != nil {
		t.Fatalf("SyncNow: %v", err)
	}

	if status.LastCommitHash == "" {
		t.Fatalf("LastCommitHash is empty")
	}
	page, err := treeService.GetPage("page-new")
	if err != nil {
		t.Fatalf("GetPage page-new: %v", err)
	}
	if page.Title != "New Page" {
		t.Fatalf("page title = %q, want New Page", page.Title)
	}
	if !strings.Contains(page.RawContent, "content") {
		t.Fatalf("page raw content = %q, want synced content", page.RawContent)
	}
}

func TestServiceSyncNowTracksUppercaseSectionIndexHistory(t *testing.T) {
	dataDir := t.TempDir()
	rootDir := filepath.Join(t.TempDir(), "workspace")
	treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
	if err := treeService.LoadTree(); err != nil {
		t.Fatalf("LoadTree: %v", err)
	}
	writeMarkdown(t, filepath.Join(rootDir, "docs", "INDEX.MD"), `---
leafwiki_id: section-docs
leafwiki_title: Docs
---
# Docs

section content`)

	service, err := NewService(ServiceOptions{
		Enabled: true,
		DataDir: dataDir,
		RootDir: rootDir,
		Tree:    treeService,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	status, err := service.SyncNow(context.Background(), SyncRequest{
		Reason: ReasonExplicit,
		Source: SourceFilesystem,
		Actor:  PublicEditorActor(),
	})
	if err != nil {
		t.Fatalf("SyncNow: %v", err)
	}
	if status.LastCommitHash == "" {
		t.Fatalf("LastCommitHash is empty")
	}

	page, err := treeService.GetPage("section-docs")
	if err != nil {
		t.Fatalf("GetPage section-docs: %v", err)
	}
	if page.Kind != tree.NodeKindSection {
		t.Fatalf("page kind = %q, want section", page.Kind)
	}
	if !strings.Contains(page.RawContent, "section content") {
		t.Fatalf("page raw content = %q, want synced INDEX.MD content", page.RawContent)
	}
	entries, err := os.ReadDir(filepath.Join(rootDir, "docs"))
	if err != nil {
		t.Fatalf("ReadDir docs: %v", err)
	}
	for _, entry := range entries {
		if entry.Name() == "index.md" {
			t.Fatalf("sync materialized lowercase index.md alongside INDEX.MD")
		}
	}

	result, err := service.ListPageRevisions(context.Background(), page, "", 10)
	if err != nil {
		t.Fatalf("ListPageRevisions: %v", err)
	}
	if len(result.Revisions) != 1 {
		t.Fatalf("revision count = %d, want uppercase section index commit", len(result.Revisions))
	}
	if result.Revisions[0].Path != "docs" {
		t.Fatalf("revision path = %q, want docs", result.Revisions[0].Path)
	}
	snapshot, err := service.GetPageRevisionSnapshot(context.Background(), page, result.Revisions[0].ID)
	if err != nil {
		t.Fatalf("GetPageRevisionSnapshot: %v", err)
	}
	if !strings.Contains(snapshot.Content, "section content") {
		t.Fatalf("snapshot content = %q, want uppercase section index content", snapshot.Content)
	}
}

func TestServiceSyncNowAmendsMetadataWritebacksIntoSameBatchCommit(t *testing.T) {
	dataDir := t.TempDir()
	rootDir := filepath.Join(t.TempDir(), "workspace")
	treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
	if err := treeService.LoadTree(); err != nil {
		t.Fatalf("LoadTree: %v", err)
	}
	writeMarkdown(t, filepath.Join(rootDir, "needs-metadata.md"), "# Needs Metadata\n\nbody")

	service, err := NewService(ServiceOptions{
		Enabled: true,
		DataDir: dataDir,
		RootDir: rootDir,
		Tree:    treeService,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	status, err := service.SyncNow(context.Background(), SyncRequest{
		Reason: ReasonExplicit,
		Source: SourceFilesystem,
		Actor:  PublicEditorActor(),
	})
	if err != nil {
		t.Fatalf("SyncNow: %v", err)
	}
	if status.LastCommitHash == "" {
		t.Fatalf("LastCommitHash is empty")
	}
	snapshots, err := service.ListSnapshots(context.Background(), 10)
	if err != nil {
		t.Fatalf("ListSnapshots: %v", err)
	}
	if len(snapshots) != 1 {
		t.Fatalf("snapshot count = %d, want one amended commit: %#v", len(snapshots), snapshots)
	}
	page := mustGetOnlyPage(t, treeService)
	snapshot, err := service.GetPageRevisionSnapshot(context.Background(), page, snapshots[0].ID)
	if err != nil {
		t.Fatalf("GetPageRevisionSnapshot: %v", err)
	}
	if !strings.Contains(snapshot.Content, "leafwiki_id:") {
		t.Fatalf("snapshot content was not amended with metadata: %q", snapshot.Content)
	}
}

func TestServiceSyncNowPreservesOriginalChangedMarkdownCountWhenAmendingMetadataWritebacks(t *testing.T) {
	dataDir := t.TempDir()
	rootDir := filepath.Join(t.TempDir(), "workspace")
	treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
	if err := treeService.LoadTree(); err != nil {
		t.Fatalf("LoadTree: %v", err)
	}
	writeMarkdown(t, filepath.Join(rootDir, "already-has-metadata.md"), `---
leafwiki_id: page-ready
leafwiki_title: Already Has Metadata
leafwiki_created_at: 2026-06-07T10:00:00Z
leafwiki_updated_at: 2026-06-07T10:00:00Z
---

# Already Has Metadata
`)
	writeMarkdown(t, filepath.Join(rootDir, "needs-metadata.md"), "# Needs Metadata\n\nbody")

	service, err := NewService(ServiceOptions{
		Enabled: true,
		DataDir: dataDir,
		RootDir: rootDir,
		Tree:    treeService,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	if _, err := service.SyncNow(context.Background(), SyncRequest{
		Reason: ReasonExplicit,
		Source: SourceFilesystem,
		Actor:  PublicEditorActor(),
	}); err != nil {
		t.Fatalf("SyncNow: %v", err)
	}

	snapshots, err := service.ListSnapshots(context.Background(), 10)
	if err != nil {
		t.Fatalf("ListSnapshots: %v", err)
	}
	if len(snapshots) != 1 {
		t.Fatalf("snapshot count = %d, want one amended commit: %#v", len(snapshots), snapshots)
	}
	if snapshots[0].ChangedMarkdownCount != 2 {
		t.Fatalf("ChangedMarkdownCount = %d, want original two-file snapshot count", snapshots[0].ChangedMarkdownCount)
	}
}

func TestServiceSyncNowRecordsChangedMarkdownPaths(t *testing.T) {
	fakeTree := &fakeTreeReconstructor{}
	service, err := NewService(ServiceOptions{
		Enabled: true,
		Tree:    fakeTree,
		Store: &fakeRevisionStore{
			capture: &gitrevisions.Commit{
				Hash:                 "abc123",
				ChangedMarkdownCount: 2,
				ChangedMarkdownPaths: []string{"docs/a.md", "docs/b.md"},
			},
		},
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	status, err := service.SyncNow(context.Background(), SyncRequest{
		Reason: ReasonExplicit,
		Source: SourceFilesystem,
		Actor:  PublicEditorActor(),
	})
	if err != nil {
		t.Fatalf("SyncNow: %v", err)
	}

	if status.LastCommitHash != "abc123" {
		t.Fatalf("LastCommitHash = %q, want abc123", status.LastCommitHash)
	}
	if strings.Join(status.RecentChangedMarkdownPaths, ",") != "docs/a.md,docs/b.md" {
		t.Fatalf("RecentChangedMarkdownPaths = %#v", status.RecentChangedMarkdownPaths)
	}
	if got := fakeTree.reconstructCount(); got != 1 {
		t.Fatalf("reconstructs = %d, want 1", got)
	}
}

func TestServiceSyncNowRunsAfterSyncWhenValidationWarningsExist(t *testing.T) {
	dataDir := t.TempDir()
	rootDir := filepath.Join(t.TempDir(), "workspace")
	treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
	if err := treeService.LoadTree(); err != nil {
		t.Fatalf("LoadTree: %v", err)
	}
	writeMarkdown(t, filepath.Join(rootDir, "valid-page.md"), `---
leafwiki_id: valid-page
leafwiki_title: Valid Page
---

# Valid Page
`)
	writeMarkdown(t, filepath.Join(rootDir, "Bad Slug.md"), "---\nleafwiki_id: bad-slug\nleafwiki_title: Bad Slug\n---\n# Bad Slug\n")
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
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	status, err := service.SyncNow(context.Background(), SyncRequest{
		Reason: ReasonExplicit,
		Source: SourceFilesystem,
		Actor:  PublicEditorActor(),
	})
	if err != nil {
		t.Fatalf("SyncNow: %v", err)
	}

	if got := atomic.LoadInt32(&afterSyncCalls); got != 1 {
		t.Fatalf("afterSync calls = %d, want 1 rebuild after successful reconstruction with validation warnings", got)
	}
	if len(status.ValidationErrors) == 0 {
		t.Fatalf("ValidationErrors empty, want invalid slug warning")
	}
}

func TestServiceSyncNowRecordsValidationErrorsWithMarkdownPaths(t *testing.T) {
	fakeTree := &fakeTreeReconstructor{err: errors.New(`duplicate leafwiki_id "dup" in /workspace/a.md and /workspace/docs/b.md`)}
	service, err := NewService(ServiceOptions{
		Enabled: true,
		RootDir: "/workspace",
		Tree:    fakeTree,
		Store:   &fakeRevisionStore{capture: &gitrevisions.Commit{Hash: "abc123"}},
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	status, err := service.SyncNow(context.Background(), SyncRequest{
		Reason: ReasonExplicit,
		Source: SourceFilesystem,
		Actor:  PublicEditorActor(),
	})
	if err != nil {
		t.Fatalf("SyncNow: %v", err)
	}

	if len(status.ValidationErrors) != 2 {
		t.Fatalf("ValidationErrors = %#v, want two path-specific errors", status.ValidationErrors)
	}
	got := []string{status.ValidationErrors[0].Path, status.ValidationErrors[1].Path}
	if strings.Join(got, ",") != "a.md,docs/b.md" {
		t.Fatalf("ValidationError paths = %#v, want a.md and docs/b.md", got)
	}
}

func TestServiceSyncNowRecordsAdditionalBatchActors(t *testing.T) {
	dataDir := t.TempDir()
	rootDir := filepath.Join(t.TempDir(), "workspace")
	treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
	writeMarkdown(t, filepath.Join(rootDir, "page.md"), "---\nleafwiki_id: page\nleafwiki_title: Page\n---\n# Page\n")
	store, err := gitrevisions.Open(gitrevisions.StoreOptions{DataDir: dataDir, RootDir: rootDir})
	if err != nil {
		t.Fatalf("Open store: %v", err)
	}
	service, err := NewService(ServiceOptions{
		Enabled: true,
		DataDir: dataDir,
		RootDir: rootDir,
		Tree:    treeService,
		Store:   store,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	status, err := service.SyncNow(context.Background(), SyncRequest{
		Reason: ReasonExplicit,
		Source: SourceWeb,
		Actor:  Actor{ID: "alice", Name: "Alice", Email: "alice@example.test"},
		AdditionalActors: []Actor{
			{ID: "bob", Name: "Bob", Email: "bob@example.test"},
		},
	})
	if err != nil {
		t.Fatalf("SyncNow: %v", err)
	}

	commit, err := store.GetCommit(context.Background(), status.LastCommitHash)
	if err != nil {
		t.Fatalf("GetCommit: %v", err)
	}
	if commit.AuthorID != "alice" {
		t.Fatalf("AuthorID = %q, want alice", commit.AuthorID)
	}
	if strings.Join(commit.ActorIDs, ",") != "alice,bob" {
		t.Fatalf("ActorIDs = %#v, want alice,bob", commit.ActorIDs)
	}
}

func TestServiceSyncNowStopsBeforeRebuildWhenGitCaptureFails(t *testing.T) {
	captureErr := errors.New("git storage read-only")
	fakeTree := &fakeTreeReconstructor{}
	service, err := NewService(ServiceOptions{
		Enabled: true,
		Tree:    fakeTree,
		Store:   &fakeRevisionStore{captureErr: captureErr},
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	status, err := service.SyncNow(context.Background(), SyncRequest{
		Reason: ReasonExplicit,
		Source: SourceFilesystem,
		Actor:  PublicEditorActor(),
	})
	if !errors.Is(err, captureErr) {
		t.Fatalf("SyncNow error = %v, want %v", err, captureErr)
	}
	if got := fakeTree.reconstructCount(); got != 0 {
		t.Fatalf("reconstructs = %d, want 0", got)
	}
	if !strings.Contains(status.LastError, "git storage read-only") {
		t.Fatalf("LastError = %q, want git error", status.LastError)
	}
}

func TestServiceListPageRevisionsUsesCommitAuthorMetadata(t *testing.T) {
	createdAt := time.Date(2026, 6, 7, 10, 0, 0, 0, time.UTC)
	page := &tree.Page{PageNode: &tree.PageNode{
		ID:    "page-1",
		Title: "Page One",
		Slug:  "page-one",
		Kind:  tree.NodeKindPage,
	}}
	store := &fakeRevisionStore{
		commits: []gitrevisions.Commit{{
			Hash:       "commit-1",
			Message:    "LeafWiki workspace sync",
			AuthorID:   "alice",
			AuthorName: "Alice",
			CreatedAt:  createdAt,
		}},
		filesAt: map[string]map[string]string{
			"commit-1": {"page-one.md": "# Page One\n"},
		},
		changedPaths: map[string][]string{
			"commit-1": {"page-one.md"},
		},
	}
	service, err := NewService(ServiceOptions{
		Enabled: true,
		Tree:    &fakeTreeReconstructor{},
		Store:   store,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	result, err := service.ListPageRevisions(context.Background(), page, "", 10)
	if err != nil {
		t.Fatalf("ListPageRevisions: %v", err)
	}
	revisions := result.Revisions

	if len(revisions) != 1 {
		t.Fatalf("revision count = %d, want 1", len(revisions))
	}
	if revisions[0].AuthorID != "alice" {
		t.Fatalf("AuthorID = %q, want alice", revisions[0].AuthorID)
	}
	if !revisions[0].CreatedAt.Equal(createdAt) {
		t.Fatalf("CreatedAt = %v, want %v", revisions[0].CreatedAt, createdAt)
	}
}

func TestServiceListPageRevisionsPaginatesMoreThanLimitPageCommits(t *testing.T) {
	page := &tree.Page{PageNode: &tree.PageNode{
		ID:    "page-a",
		Title: "Page A",
		Slug:  "page-a",
		Kind:  tree.NodeKindPage,
	}}
	store := &fakeRevisionStore{
		commits: []gitrevisions.Commit{
			{Hash: "page-a-change-3", AuthorID: "alice"},
			{Hash: "page-a-change-2", AuthorID: "alice"},
			{Hash: "page-a-change-1", AuthorID: "alice"},
		},
		filesAt: map[string]map[string]string{
			"page-a-change-3": {
				"page-a.md": "---\nleafwiki_id: page-a\nleafwiki_title: Page A\n---\n# Page A v3\n",
			},
			"page-a-change-2": {
				"page-a.md": "---\nleafwiki_id: page-a\nleafwiki_title: Page A\n---\n# Page A v2\n",
			},
			"page-a-change-1": {
				"page-a.md": "---\nleafwiki_id: page-a\nleafwiki_title: Page A\n---\n# Page A v1\n",
			},
		},
		changedPaths: map[string][]string{
			"page-a-change-3": {"page-a.md"},
			"page-a-change-2": {"page-a.md"},
			"page-a-change-1": {"page-a.md"},
		},
	}
	service, err := NewService(ServiceOptions{
		Enabled: true,
		Tree:    &fakeTreeReconstructor{},
		Store:   store,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	firstPage, err := service.ListPageRevisions(context.Background(), page, "", 2)
	if err != nil {
		t.Fatalf("ListPageRevisions first page: %v", err)
	}
	if got := revisionIDs(firstPage.Revisions); strings.Join(got, ",") != "page-a-change-3,page-a-change-2" {
		t.Fatalf("first page revision ids = %#v, want latest two page commits", got)
	}
	if firstPage.NextCursor != "page-a-change-2" {
		t.Fatalf("first page next cursor = %q, want page-a-change-2", firstPage.NextCursor)
	}

	secondPage, err := service.ListPageRevisions(context.Background(), page, firstPage.NextCursor, 2)
	if err != nil {
		t.Fatalf("ListPageRevisions second page: %v", err)
	}
	if got := revisionIDs(secondPage.Revisions); strings.Join(got, ",") != "page-a-change-1" {
		t.Fatalf("second page revision ids = %#v, want oldest page commit", got)
	}
	if secondPage.NextCursor != "" {
		t.Fatalf("second page next cursor = %q, want empty final cursor", secondPage.NextCursor)
	}
}

func TestServiceListPageRevisionsOmitsNextCursorWhenMatchesEqualLimit(t *testing.T) {
	page := &tree.Page{PageNode: &tree.PageNode{
		ID:    "page-a",
		Title: "Page A",
		Slug:  "page-a",
		Kind:  tree.NodeKindPage,
	}}
	store := &fakeRevisionStore{
		commits: []gitrevisions.Commit{
			{Hash: "page-a-change-2", AuthorID: "alice"},
			{Hash: "page-a-change-1", AuthorID: "alice"},
		},
		filesAt: map[string]map[string]string{
			"page-a-change-2": {
				"page-a.md": "---\nleafwiki_id: page-a\nleafwiki_title: Page A\n---\n# Page A v2\n",
			},
			"page-a-change-1": {
				"page-a.md": "---\nleafwiki_id: page-a\nleafwiki_title: Page A\n---\n# Page A v1\n",
			},
		},
		changedPaths: map[string][]string{
			"page-a-change-2": {"page-a.md"},
			"page-a-change-1": {"page-a.md"},
		},
	}
	service, err := NewService(ServiceOptions{
		Enabled: true,
		Tree:    &fakeTreeReconstructor{},
		Store:   store,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	result, err := service.ListPageRevisions(context.Background(), page, "", 2)
	if err != nil {
		t.Fatalf("ListPageRevisions: %v", err)
	}

	if got := revisionIDs(result.Revisions); strings.Join(got, ",") != "page-a-change-2,page-a-change-1" {
		t.Fatalf("revision ids = %#v, want both page commits", got)
	}
	if result.NextCursor != "" {
		t.Fatalf("next cursor = %q, want empty cursor when matches equal limit", result.NextCursor)
	}
}

func TestServiceListPageRevisionsFollowsMarkdownRenameByLeafWikiID(t *testing.T) {
	page := &tree.Page{PageNode: &tree.PageNode{
		ID:    "page-1",
		Title: "New Page",
		Slug:  "new-page",
		Kind:  tree.NodeKindPage,
	}}
	store := &fakeRevisionStore{
		commits: []gitrevisions.Commit{
			{Hash: "new-commit", AuthorID: "alice"},
			{Hash: "old-commit", AuthorID: "alice"},
		},
		filesAt: map[string]map[string]string{
			"new-commit": {
				"new-page.md": "---\nleafwiki_id: page-1\nleafwiki_title: New Page\n---\n# New Page\n",
			},
			"old-commit": {
				"old-page.md": "---\nleafwiki_id: page-1\nleafwiki_title: Old Page\n---\n# Old Page\n",
			},
		},
		changedPaths: map[string][]string{
			"new-commit": {"new-page.md"},
			"old-commit": {"old-page.md"},
		},
	}
	service, err := NewService(ServiceOptions{
		Enabled: true,
		Tree:    &fakeTreeReconstructor{},
		Store:   store,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	result, err := service.ListPageRevisions(context.Background(), page, "", 10)
	if err != nil {
		t.Fatalf("ListPageRevisions: %v", err)
	}
	revisions := result.Revisions

	if len(revisions) != 2 {
		t.Fatalf("revision count = %d, want 2", len(revisions))
	}
	if revisions[0].Path != "new-page" {
		t.Fatalf("latest revision path = %q, want new-page", revisions[0].Path)
	}
	if revisions[1].Path != "old-page" {
		t.Fatalf("renamed revision path = %q, want old-page", revisions[1].Path)
	}
}

func TestServiceListPageRevisionsMatchesUppercaseMarkdownExtensionByLeafWikiID(t *testing.T) {
	page := &tree.Page{PageNode: &tree.PageNode{
		ID:    "page-1",
		Title: "Page",
		Slug:  "page",
		Kind:  tree.NodeKindPage,
	}}
	store := &fakeRevisionStore{
		commits: []gitrevisions.Commit{{Hash: "uppercase-commit", AuthorID: "alice"}},
		filesAt: map[string]map[string]string{
			"uppercase-commit": {
				"Page.MD": "---\nleafwiki_id: page-1\nleafwiki_title: Page\n---\n# Page\n",
			},
		},
		changedPaths: map[string][]string{
			"uppercase-commit": {"Page.MD"},
		},
	}
	service, err := NewService(ServiceOptions{
		Enabled: true,
		Tree:    &fakeTreeReconstructor{},
		Store:   store,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	result, err := service.ListPageRevisions(context.Background(), page, "", 10)
	if err != nil {
		t.Fatalf("ListPageRevisions: %v", err)
	}

	if len(result.Revisions) != 1 {
		t.Fatalf("revision count = %d, want uppercase Markdown commit", len(result.Revisions))
	}
	if result.Revisions[0].Path != "Page" {
		t.Fatalf("revision path = %q, want Page", result.Revisions[0].Path)
	}
}

func TestServiceListPageRevisionsUsesHistoricalMarkdownMetadata(t *testing.T) {
	page := &tree.Page{PageNode: &tree.PageNode{
		ID:    "page-1",
		Title: "Current Title",
		Slug:  "current-page",
		Kind:  tree.NodeKindPage,
	}}
	store := &fakeRevisionStore{
		commits: []gitrevisions.Commit{{Hash: "old-commit", AuthorID: "alice"}},
		filesAt: map[string]map[string]string{
			"old-commit": {
				"old-page.md": "---\nleafwiki_id: page-1\nleafwiki_title: Historical Title\n---\n# Historical Heading\n",
			},
		},
		changedPaths: map[string][]string{
			"old-commit": {"old-page.md"},
		},
	}
	service, err := NewService(ServiceOptions{
		Enabled: true,
		Tree:    &fakeTreeReconstructor{},
		Store:   store,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	result, err := service.ListPageRevisions(context.Background(), page, "", 1)
	if err != nil {
		t.Fatalf("ListPageRevisions: %v", err)
	}
	revisions := result.Revisions

	if len(revisions) != 1 {
		t.Fatalf("revision count = %d, want 1", len(revisions))
	}
	if revisions[0].Title != "Historical Title" {
		t.Fatalf("revision title = %q, want Historical Title", revisions[0].Title)
	}
	if revisions[0].Slug != "old-page" {
		t.Fatalf("revision slug = %q, want old-page", revisions[0].Slug)
	}
	if revisions[0].Kind != string(tree.NodeKindPage) {
		t.Fatalf("revision kind = %q, want page", revisions[0].Kind)
	}
	if revisions[0].Path != "old-page" {
		t.Fatalf("revision path = %q, want old-page", revisions[0].Path)
	}
}

func TestServiceListPageRevisionsNormalizesSectionIndexPath(t *testing.T) {
	page := &tree.Page{PageNode: &tree.PageNode{
		ID:    "section-1",
		Title: "Docs",
		Slug:  "docs",
		Kind:  tree.NodeKindSection,
	}}
	store := &fakeRevisionStore{
		commits: []gitrevisions.Commit{{Hash: "section-commit", AuthorID: "alice"}},
		filesAt: map[string]map[string]string{
			"section-commit": {
				"docs/index.md": "---\nleafwiki_id: section-1\nleafwiki_title: Historical Docs\n---\n# Historical Docs\n",
			},
		},
		changedPaths: map[string][]string{
			"section-commit": {"docs/index.md"},
		},
	}
	service, err := NewService(ServiceOptions{
		Enabled: true,
		Tree:    &fakeTreeReconstructor{},
		Store:   store,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	result, err := service.ListPageRevisions(context.Background(), page, "", 1)
	if err != nil {
		t.Fatalf("ListPageRevisions: %v", err)
	}
	revisions := result.Revisions

	if len(revisions) != 1 {
		t.Fatalf("revision count = %d, want 1", len(revisions))
	}
	if revisions[0].Title != "Historical Docs" {
		t.Fatalf("revision title = %q, want Historical Docs", revisions[0].Title)
	}
	if revisions[0].Slug != "docs" {
		t.Fatalf("revision slug = %q, want docs", revisions[0].Slug)
	}
	if revisions[0].Kind != string(tree.NodeKindSection) {
		t.Fatalf("revision kind = %q, want section", revisions[0].Kind)
	}
	if revisions[0].Path != "docs" {
		t.Fatalf("revision path = %q, want docs", revisions[0].Path)
	}
}

func TestServiceListPageRevisionsKeepsHistoricalPageKindAfterSectionConversion(t *testing.T) {
	page := &tree.Page{PageNode: &tree.PageNode{
		ID:    "docs-1",
		Title: "Docs",
		Slug:  "docs",
		Kind:  tree.NodeKindSection,
	}}
	store := &fakeRevisionStore{
		commits: []gitrevisions.Commit{{Hash: "page-commit", AuthorID: "alice"}},
		filesAt: map[string]map[string]string{
			"page-commit": {
				"docs.md": "---\nleafwiki_id: docs-1\nleafwiki_title: Docs Page\n---\n# Docs Page\n",
			},
		},
		changedPaths: map[string][]string{
			"page-commit": {"docs.md"},
		},
	}
	service, err := NewService(ServiceOptions{
		Enabled: true,
		Tree:    &fakeTreeReconstructor{},
		Store:   store,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	result, err := service.ListPageRevisions(context.Background(), page, "", 1)
	if err != nil {
		t.Fatalf("ListPageRevisions: %v", err)
	}
	revisions := result.Revisions

	if len(revisions) != 1 {
		t.Fatalf("revision count = %d, want 1", len(revisions))
	}
	if revisions[0].Kind != string(tree.NodeKindPage) {
		t.Fatalf("revision kind = %q, want historical page kind", revisions[0].Kind)
	}
	if revisions[0].Path != "docs" {
		t.Fatalf("revision path = %q, want docs", revisions[0].Path)
	}
}

func TestServiceListPageRevisionsOnlyIncludesCommitsThatChangedDocument(t *testing.T) {
	page := &tree.Page{PageNode: &tree.PageNode{
		ID:    "page-a",
		Title: "Page A",
		Slug:  "page-a",
		Kind:  tree.NodeKindPage,
	}}
	store := &fakeRevisionStore{
		commits: []gitrevisions.Commit{
			{Hash: "page-b-change", AuthorID: "bob"},
			{Hash: "page-a-change", AuthorID: "alice"},
		},
		filesAt: map[string]map[string]string{
			"page-b-change": {
				"page-a.md": "---\nleafwiki_id: page-a\nleafwiki_title: Page A\n---\n# Page A\n",
				"page-b.md": "---\nleafwiki_id: page-b\nleafwiki_title: Page B\n---\n# Page B changed\n",
			},
			"page-a-change": {
				"page-a.md": "---\nleafwiki_id: page-a\nleafwiki_title: Page A\n---\n# Page A changed\n",
			},
		},
		changedPaths: map[string][]string{
			"page-b-change": {"page-b.md"},
			"page-a-change": {"page-a.md"},
		},
	}
	service, err := NewService(ServiceOptions{
		Enabled: true,
		Tree:    &fakeTreeReconstructor{},
		Store:   store,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	result, err := service.ListPageRevisions(context.Background(), page, "", 10)
	if err != nil {
		t.Fatalf("ListPageRevisions: %v", err)
	}
	revisions := result.Revisions

	if len(revisions) != 1 {
		t.Fatalf("revision count = %d, want only page A commit: %#v", len(revisions), revisions)
	}
	if revisions[0].ID != "page-a-change" {
		t.Fatalf("revision id = %q, want page-a-change", revisions[0].ID)
	}
}

func TestServiceListPageRevisionsScansPastUnrelatedHeadWhenLimitIsOne(t *testing.T) {
	page := &tree.Page{PageNode: &tree.PageNode{
		ID:    "page-a",
		Title: "Page A",
		Slug:  "page-a",
		Kind:  tree.NodeKindPage,
	}}
	store := &fakeRevisionStore{
		commits: []gitrevisions.Commit{
			{Hash: "page-b-change", AuthorID: "bob"},
			{Hash: "page-a-change", AuthorID: "alice"},
		},
		filesAt: map[string]map[string]string{
			"page-b-change": {
				"page-a.md": "---\nleafwiki_id: page-a\nleafwiki_title: Page A\n---\n# Page A\n",
				"page-b.md": "---\nleafwiki_id: page-b\nleafwiki_title: Page B\n---\n# Page B changed\n",
			},
			"page-a-change": {
				"page-a.md": "---\nleafwiki_id: page-a\nleafwiki_title: Page A\n---\n# Page A changed\n",
			},
		},
		changedPaths: map[string][]string{
			"page-b-change": {"page-b.md"},
			"page-a-change": {"page-a.md"},
		},
	}
	service, err := NewService(ServiceOptions{
		Enabled: true,
		Tree:    &fakeTreeReconstructor{},
		Store:   store,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	result, err := service.ListPageRevisions(context.Background(), page, "", 1)
	if err != nil {
		t.Fatalf("ListPageRevisions: %v", err)
	}
	revisions := result.Revisions

	if len(revisions) != 1 {
		t.Fatalf("revision count = %d, want latest page A revision after unrelated HEAD", len(revisions))
	}
	if revisions[0].ID != "page-a-change" {
		t.Fatalf("revision id = %q, want page-a-change", revisions[0].ID)
	}
}

func TestServiceListPageRevisionsScansAllCommitsPastLargeUnrelatedHead(t *testing.T) {
	page := &tree.Page{PageNode: &tree.PageNode{
		ID:    "page-a",
		Title: "Page A",
		Slug:  "page-a",
		Kind:  tree.NodeKindPage,
	}}
	store := &fakeRevisionStore{
		filesAt:      map[string]map[string]string{},
		changedPaths: map[string][]string{},
	}
	for i := 0; i < 1005; i++ {
		hash := "page-b-change-" + strconv.Itoa(i)
		store.commits = append(store.commits, gitrevisions.Commit{Hash: hash, AuthorID: "bob"})
		store.filesAt[hash] = map[string]string{
			"page-a.md": "---\nleafwiki_id: page-a\nleafwiki_title: Page A\n---\n# Page A\n",
			"page-b.md": "---\nleafwiki_id: page-b\nleafwiki_title: Page B\n---\n# Page B changed\n",
		}
		store.changedPaths[hash] = []string{"page-b.md"}
	}
	store.commits = append(store.commits, gitrevisions.Commit{Hash: "page-a-change", AuthorID: "alice"})
	store.filesAt["page-a-change"] = map[string]string{
		"page-a.md": "---\nleafwiki_id: page-a\nleafwiki_title: Page A\n---\n# Page A changed\n",
	}
	store.changedPaths["page-a-change"] = []string{"page-a.md"}
	service, err := NewService(ServiceOptions{
		Enabled: true,
		Tree:    &fakeTreeReconstructor{},
		Store:   store,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	result, err := service.ListPageRevisions(context.Background(), page, "", 1)
	if err != nil {
		t.Fatalf("ListPageRevisions: %v", err)
	}
	revisions := result.Revisions

	if len(revisions) != 1 {
		t.Fatalf("revision count = %d, want page A revision after many unrelated commits", len(revisions))
	}
	if revisions[0].ID != "page-a-change" {
		t.Fatalf("revision id = %q, want page-a-change", revisions[0].ID)
	}
}

func TestServiceListPageRevisionsStopsScanningAfterConfirmedNextCursor(t *testing.T) {
	page := &tree.Page{PageNode: &tree.PageNode{
		ID:    "page-a",
		Title: "Page A",
		Slug:  "page-a",
		Kind:  tree.NodeKindPage,
	}}
	store := &fakeRevisionStore{
		commits: []gitrevisions.Commit{
			{Hash: "page-a-change-2", AuthorID: "alice"},
			{Hash: "page-a-change-1", AuthorID: "alice"},
		},
		filesAt: map[string]map[string]string{
			"page-a-change-2": {
				"page-a.md": "---\nleafwiki_id: page-a\nleafwiki_title: Page A\n---\n# Page A changed again\n",
			},
			"page-a-change-1": {
				"page-a.md": "---\nleafwiki_id: page-a\nleafwiki_title: Page A\n---\n# Page A changed\n",
			},
		},
		changedPaths: map[string][]string{
			"page-a-change-2": {"page-a.md"},
			"page-a-change-1": {"page-a.md"},
		},
	}
	for i := 0; i < 1005; i++ {
		hash := "page-b-change-" + strconv.Itoa(i)
		store.commits = append(store.commits, gitrevisions.Commit{Hash: hash, AuthorID: "bob"})
		store.filesAt[hash] = map[string]string{
			"page-a.md": "---\nleafwiki_id: page-a\nleafwiki_title: Page A\n---\n# Page A\n",
			"page-b.md": "---\nleafwiki_id: page-b\nleafwiki_title: Page B\n---\n# Page B changed\n",
		}
		store.changedPaths[hash] = []string{"page-b.md"}
	}
	service, err := NewService(ServiceOptions{
		Enabled: true,
		Tree:    &fakeTreeReconstructor{},
		Store:   store,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	result, err := service.ListPageRevisions(context.Background(), page, "", 1)
	if err != nil {
		t.Fatalf("ListPageRevisions: %v", err)
	}
	revisions := result.Revisions

	if len(revisions) != 1 || revisions[0].ID != "page-a-change-2" {
		t.Fatalf("revisions = %#v, want only latest page A change", revisions)
	}
	if result.NextCursor != "page-a-change-2" {
		t.Fatalf("next cursor = %q, want page-a-change-2", result.NextCursor)
	}
	if store.scannedCommits != 2 {
		t.Fatalf("scannedCommits = %d, want 2", store.scannedCommits)
	}
}

func TestServiceListPageRevisionsDoesNotLoadFullTreesWhileScanning(t *testing.T) {
	page := &tree.Page{PageNode: &tree.PageNode{
		ID:    "page-a",
		Title: "Page A",
		Slug:  "page-a",
		Kind:  tree.NodeKindPage,
	}}
	store := &fakeRevisionStore{
		commits: []gitrevisions.Commit{
			{Hash: "page-b-change", AuthorID: "bob"},
			{Hash: "page-a-change", AuthorID: "alice"},
		},
		filesAt: map[string]map[string]string{
			"page-b-change": {
				"page-a.md": "---\nleafwiki_id: page-a\nleafwiki_title: Page A\n---\n# Page A\n",
				"page-b.md": "---\nleafwiki_id: page-b\nleafwiki_title: Page B\n---\n# Page B changed\n",
			},
			"page-a-change": {
				"page-a.md": "---\nleafwiki_id: page-a\nleafwiki_title: Page A\n---\n# Page A changed\n",
			},
		},
		changedPaths: map[string][]string{
			"page-b-change": {"page-b.md"},
			"page-a-change": {"page-a.md"},
		},
	}
	service, err := NewService(ServiceOptions{
		Enabled: true,
		Tree:    &fakeTreeReconstructor{},
		Store:   store,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	result, err := service.ListPageRevisions(context.Background(), page, "", 1)
	if err != nil {
		t.Fatalf("ListPageRevisions: %v", err)
	}
	revisions := result.Revisions

	if len(revisions) != 1 || revisions[0].ID != "page-a-change" {
		t.Fatalf("revisions = %#v, want page A change", revisions)
	}
	if store.filesAtCalls != 0 {
		t.Fatalf("FilesAt calls = %d, want document history scan to avoid full-tree loads", store.filesAtCalls)
	}
}

func TestServiceGetPageRevisionSnapshotRejectsUnrelatedCommit(t *testing.T) {
	page := &tree.Page{PageNode: &tree.PageNode{
		ID:    "page-a",
		Title: "Page A",
		Slug:  "page-a",
		Kind:  tree.NodeKindPage,
	}}
	store := &fakeRevisionStore{
		commits: []gitrevisions.Commit{{Hash: "page-b-change"}},
		filesAt: map[string]map[string]string{
			"page-b-change": {
				"page-a.md": "---\nleafwiki_id: page-a\nleafwiki_title: Page A\n---\n# Page A\n",
				"page-b.md": "---\nleafwiki_id: page-b\nleafwiki_title: Page B\n---\n# Page B changed\n",
			},
		},
		changedPaths: map[string][]string{
			"page-b-change": {"page-b.md"},
		},
	}
	service, err := NewService(ServiceOptions{
		Enabled: true,
		Tree:    &fakeTreeReconstructor{},
		Store:   store,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	if _, err := service.GetPageRevisionSnapshot(context.Background(), page, "page-b-change"); err == nil {
		t.Fatalf("GetPageRevisionSnapshot returned unrelated commit, want error")
	}
}

func TestServiceRestoreDocumentRestoresPreRenameContentToCurrentPath(t *testing.T) {
	dataDir := t.TempDir()
	rootDir := filepath.Join(t.TempDir(), "workspace")
	store, err := gitrevisions.Open(gitrevisions.StoreOptions{DataDir: dataDir, RootDir: rootDir})
	if err != nil {
		t.Fatalf("Open store: %v", err)
	}
	writeMarkdown(t, filepath.Join(rootDir, "old-page.md"), "---\nleafwiki_id: page-1\nleafwiki_title: Old Page\n---\n# Old Page\n\nold content")
	oldCommit, err := store.Capture(context.Background(), gitrevisions.CommitRequest{
		Reason: ReasonExplicit,
		Source: SourceFilesystem,
		Actor:  PublicEditorActor(),
	})
	if err != nil {
		t.Fatalf("capture old commit: %v", err)
	}
	if err := os.Remove(filepath.Join(rootDir, "old-page.md")); err != nil {
		t.Fatalf("remove old path: %v", err)
	}
	writeMarkdown(t, filepath.Join(rootDir, "new-page.md"), "---\nleafwiki_id: page-1\nleafwiki_title: New Page\n---\n# New Page\n\nnew content")
	if _, err := store.Capture(context.Background(), gitrevisions.CommitRequest{
		Reason: ReasonExplicit,
		Source: SourceFilesystem,
		Actor:  PublicEditorActor(),
	}); err != nil {
		t.Fatalf("capture new commit: %v", err)
	}
	page := &tree.Page{PageNode: &tree.PageNode{ID: "page-1", Title: "New Page", Slug: "new-page", Kind: tree.NodeKindPage}}
	service, err := NewService(ServiceOptions{
		Enabled: true,
		Tree:    &fakeTreeReconstructor{},
		Store:   store,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	if _, err := service.RestoreDocument(context.Background(), page, oldCommit.Hash, PublicEditorActor()); err != nil {
		t.Fatalf("RestoreDocument from pre-rename commit: %v", err)
	}

	rawBytes, err := os.ReadFile(filepath.Join(rootDir, "new-page.md"))
	if err != nil {
		t.Fatalf("read restored current path: %v", err)
	}
	raw := string(rawBytes)
	if !strings.Contains(raw, "old content") {
		t.Fatalf("current path content after restore = %q, want old content", raw)
	}
}

func TestServiceRestoreDocumentPreservesExistingUppercaseMarkdownPath(t *testing.T) {
	dataDir := t.TempDir()
	rootDir := filepath.Join(t.TempDir(), "workspace")
	store, err := gitrevisions.Open(gitrevisions.StoreOptions{DataDir: dataDir, RootDir: rootDir})
	if err != nil {
		t.Fatalf("Open store: %v", err)
	}
	writeMarkdown(t, filepath.Join(rootDir, "Page.MD"), "---\nleafwiki_id: page-1\nleafwiki_title: Page\n---\n# Page\n\nold content")
	oldCommit, err := store.Capture(context.Background(), gitrevisions.CommitRequest{
		Reason: ReasonExplicit,
		Source: SourceFilesystem,
		Actor:  PublicEditorActor(),
	})
	if err != nil {
		t.Fatalf("capture old commit: %v", err)
	}
	writeMarkdown(t, filepath.Join(rootDir, "Page.MD"), "---\nleafwiki_id: page-1\nleafwiki_title: Page\n---\n# Page\n\ncurrent content")
	if _, err := store.Capture(context.Background(), gitrevisions.CommitRequest{
		Reason: ReasonExplicit,
		Source: SourceFilesystem,
		Actor:  PublicEditorActor(),
	}); err != nil {
		t.Fatalf("capture current commit: %v", err)
	}
	page := &tree.Page{PageNode: &tree.PageNode{ID: "page-1", Title: "Page", Slug: "Page", Kind: tree.NodeKindPage}}
	service, err := NewService(ServiceOptions{
		Enabled: true,
		DataDir: dataDir,
		RootDir: rootDir,
		Tree:    tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir}),
		Store:   store,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	if _, err := service.RestoreDocument(context.Background(), page, oldCommit.Hash, PublicEditorActor()); err != nil {
		t.Fatalf("RestoreDocument from uppercase path: %v", err)
	}

	rawBytes, err := os.ReadFile(filepath.Join(rootDir, "Page.MD"))
	if err != nil {
		t.Fatalf("read uppercase restored path: %v", err)
	}
	if !strings.Contains(string(rawBytes), "old content") {
		t.Fatalf("Page.MD content = %q, want old content", string(rawBytes))
	}
	entries, err := os.ReadDir(rootDir)
	if err != nil {
		t.Fatalf("ReadDir root: %v", err)
	}
	for _, entry := range entries {
		if entry.Name() == "Page.md" {
			t.Fatalf("found lowercase duplicate Page.md in directory entries")
		}
	}
}

func TestServiceRestoreDocumentRestoresSectionIndexToCurrentSectionPath(t *testing.T) {
	dataDir := t.TempDir()
	rootDir := filepath.Join(t.TempDir(), "workspace")
	store, err := gitrevisions.Open(gitrevisions.StoreOptions{DataDir: dataDir, RootDir: rootDir})
	if err != nil {
		t.Fatalf("Open store: %v", err)
	}
	writeMarkdown(t, filepath.Join(rootDir, "docs", "index.md"), "---\nleafwiki_id: section-1\nleafwiki_title: Docs\n---\n# Docs\n\nold section content")
	oldCommit, err := store.Capture(context.Background(), gitrevisions.CommitRequest{
		Reason: ReasonExplicit,
		Source: SourceFilesystem,
		Actor:  PublicEditorActor(),
	})
	if err != nil {
		t.Fatalf("capture old commit: %v", err)
	}
	writeMarkdown(t, filepath.Join(rootDir, "docs", "index.md"), "---\nleafwiki_id: section-1\nleafwiki_title: Docs\n---\n# Docs\n\nnew section content")
	if _, err := store.Capture(context.Background(), gitrevisions.CommitRequest{
		Reason: ReasonExplicit,
		Source: SourceFilesystem,
		Actor:  PublicEditorActor(),
	}); err != nil {
		t.Fatalf("capture new commit: %v", err)
	}
	section := &tree.Page{PageNode: &tree.PageNode{ID: "section-1", Title: "Docs", Slug: "docs", Kind: tree.NodeKindSection}}
	service, err := NewService(ServiceOptions{
		Enabled: true,
		Tree:    &fakeTreeReconstructor{},
		Store:   store,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	if _, err := service.RestoreDocument(context.Background(), section, oldCommit.Hash, PublicEditorActor()); err != nil {
		t.Fatalf("RestoreDocument from section commit: %v", err)
	}

	indexBytes, err := os.ReadFile(filepath.Join(rootDir, "docs", "index.md"))
	if err != nil {
		t.Fatalf("read restored section index: %v", err)
	}
	if !strings.Contains(string(indexBytes), "old section content") {
		t.Fatalf("section index content = %q, want old section content", string(indexBytes))
	}
	if _, err := os.Stat(filepath.Join(rootDir, "docs.md")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("docs.md exists after section restore, want no page file; err=%v", err)
	}
}

func TestServiceRestoreDocumentRejectsCommitThatDidNotChangeDocument(t *testing.T) {
	page := &tree.Page{PageNode: &tree.PageNode{
		ID:    "page-a",
		Title: "Page A",
		Slug:  "page-a",
		Kind:  tree.NodeKindPage,
	}}
	store := &fakeRevisionStore{
		capture: &gitrevisions.Commit{Hash: "restore-commit"},
		filesAt: map[string]map[string]string{
			"page-b-change": {
				"page-a.md": "---\nleafwiki_id: page-a\nleafwiki_title: Page A\n---\n# Page A\n",
				"page-b.md": "---\nleafwiki_id: page-b\nleafwiki_title: Page B\n---\n# Page B changed\n",
			},
		},
		changedPaths: map[string][]string{
			"page-b-change": {"page-b.md"},
		},
	}
	service, err := NewService(ServiceOptions{
		Enabled: true,
		Tree:    &fakeTreeReconstructor{},
		Store:   store,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	if _, err := service.RestoreDocument(context.Background(), page, "page-b-change", PublicEditorActor()); err == nil {
		t.Fatalf("RestoreDocument restored unrelated commit, want error")
	}
	if store.restoreDocumentToPathCalls != 0 {
		t.Fatalf("restoreDocumentToPathCalls = %d, want 0", store.restoreDocumentToPathCalls)
	}
}

func TestServiceRestoreDocumentUsesChangedContentWithoutLoadingFullTree(t *testing.T) {
	page := &tree.Page{PageNode: &tree.PageNode{
		ID:    "page-a",
		Title: "Page A",
		Slug:  "page-a",
		Kind:  tree.NodeKindPage,
	}}
	store := &fakeRevisionStore{
		capture: &gitrevisions.Commit{Hash: "restore-commit"},
		filesAt: map[string]map[string]string{
			"page-a-change": {
				"page-a.md": "---\nleafwiki_id: page-a\nleafwiki_title: Page A\n---\n# Page A restored\n",
			},
		},
		changedPaths: map[string][]string{
			"page-a-change": {"page-a.md"},
		},
	}
	service, err := NewService(ServiceOptions{
		Enabled: true,
		Tree:    &fakeTreeReconstructor{},
		Store:   store,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	if _, err := service.RestoreDocument(context.Background(), page, "page-a-change", PublicEditorActor()); err != nil {
		t.Fatalf("RestoreDocument: %v", err)
	}

	if store.filesAtCalls != 0 {
		t.Fatalf("FilesAt calls = %d, want restore to avoid full-tree load", store.filesAtCalls)
	}
	if store.restoreDocumentContentToPathCalls != 1 {
		t.Fatalf("restoreDocumentContentToPathCalls = %d, want 1", store.restoreDocumentContentToPathCalls)
	}
	if !strings.Contains(store.restoredContent, "Page A restored") {
		t.Fatalf("restoredContent = %q, want selected historical content", store.restoredContent)
	}
}

func TestServiceRestoreDocumentReturnsReconstructionError(t *testing.T) {
	reconstructErr := errors.New(`duplicate leafwiki_id "page-a" in /workspace/page-a.md and /workspace/other.md`)
	page := &tree.Page{PageNode: &tree.PageNode{
		ID:    "page-a",
		Title: "Page A",
		Slug:  "page-a",
		Kind:  tree.NodeKindPage,
	}}
	store := &fakeRevisionStore{
		capture: &gitrevisions.Commit{Hash: "restore-commit"},
		filesAt: map[string]map[string]string{
			"page-a-change": {
				"page-a.md": "---\nleafwiki_id: page-a\nleafwiki_title: Page A\n---\n# Page A restored\n",
			},
		},
		changedPaths: map[string][]string{
			"page-a-change": {"page-a.md"},
		},
	}
	service, err := NewService(ServiceOptions{
		Enabled: true,
		RootDir: "/workspace",
		Tree:    &fakeTreeReconstructor{err: reconstructErr},
		Store:   store,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	status, err := service.RestoreDocument(context.Background(), page, "page-a-change", PublicEditorActor())
	if !errors.Is(err, reconstructErr) {
		t.Fatalf("RestoreDocument error = %v, want %v", err, reconstructErr)
	}
	if !strings.Contains(status.LastError, "duplicate leafwiki_id") {
		t.Fatalf("LastError = %q, want reconstruct error", status.LastError)
	}
	if len(status.ValidationErrors) != 2 {
		t.Fatalf("ValidationErrors = %#v, want path-specific reconstruct errors", status.ValidationErrors)
	}
}

func TestServiceRestoreWorkspaceCapturesMetadataWriteback(t *testing.T) {
	dataDir := t.TempDir()
	rootDir := filepath.Join(t.TempDir(), "workspace")
	store, err := gitrevisions.Open(gitrevisions.StoreOptions{DataDir: dataDir, RootDir: rootDir})
	if err != nil {
		t.Fatalf("Open store: %v", err)
	}
	writeMarkdown(t, filepath.Join(rootDir, "needs-metadata.md"), "# Needs Metadata\n\nold body")
	rawCommit, err := store.Capture(context.Background(), gitrevisions.CommitRequest{
		Reason: ReasonExplicit,
		Source: SourceFilesystem,
		Actor:  PublicEditorActor(),
	})
	if err != nil {
		t.Fatalf("capture raw commit: %v", err)
	}
	treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
	service, err := NewService(ServiceOptions{
		Enabled: true,
		DataDir: dataDir,
		RootDir: rootDir,
		Tree:    treeService,
		Store:   store,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	status, err := service.RestoreWorkspace(context.Background(), rawCommit.Hash, PublicEditorActor())
	if err != nil {
		t.Fatalf("RestoreWorkspace: %v", err)
	}

	files, err := store.FilesAt(context.Background(), status.LastCommitHash)
	if err != nil {
		t.Fatalf("FilesAt restore head: %v", err)
	}
	if !strings.Contains(files["needs-metadata.md"], "leafwiki_id:") {
		t.Fatalf("restore commit did not include reconstructed metadata writeback: %q", files["needs-metadata.md"])
	}
}

func TestServiceRestoreDocumentCapturesMetadataWriteback(t *testing.T) {
	dataDir := t.TempDir()
	rootDir := filepath.Join(t.TempDir(), "workspace")
	store, err := gitrevisions.Open(gitrevisions.StoreOptions{DataDir: dataDir, RootDir: rootDir})
	if err != nil {
		t.Fatalf("Open store: %v", err)
	}
	writeMarkdown(t, filepath.Join(rootDir, "needs-metadata.md"), "# Needs Metadata\n\nold body")
	rawCommit, err := store.Capture(context.Background(), gitrevisions.CommitRequest{
		Reason: ReasonExplicit,
		Source: SourceFilesystem,
		Actor:  PublicEditorActor(),
	})
	if err != nil {
		t.Fatalf("capture raw commit: %v", err)
	}
	writeMarkdown(t, filepath.Join(rootDir, "needs-metadata.md"), `---
leafwiki_id: page-1
leafwiki_title: Needs Metadata
leafwiki_created_at: 2026-06-07T10:00:00Z
leafwiki_updated_at: 2026-06-07T10:00:00Z
---

# Needs Metadata

current body`)
	if _, err := store.Capture(context.Background(), gitrevisions.CommitRequest{
		Reason: ReasonExplicit,
		Source: SourceFilesystem,
		Actor:  PublicEditorActor(),
	}); err != nil {
		t.Fatalf("capture current commit: %v", err)
	}
	treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
	page := &tree.Page{PageNode: &tree.PageNode{ID: "page-1", Title: "Needs Metadata", Slug: "needs-metadata", Kind: tree.NodeKindPage}}
	service, err := NewService(ServiceOptions{
		Enabled: true,
		DataDir: dataDir,
		RootDir: rootDir,
		Tree:    treeService,
		Store:   store,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	status, err := service.RestoreDocument(context.Background(), page, rawCommit.Hash, PublicEditorActor())
	if err != nil {
		t.Fatalf("RestoreDocument: %v", err)
	}

	files, err := store.FilesAt(context.Background(), status.LastCommitHash)
	if err != nil {
		t.Fatalf("FilesAt restore head: %v", err)
	}
	if !strings.Contains(files["needs-metadata.md"], "leafwiki_id:") {
		t.Fatalf("document restore commit did not include reconstructed metadata writeback: %q", files["needs-metadata.md"])
	}
}

func TestServiceGetPageRevisionSnapshotRejectsPathReuseWithDifferentLeafWikiID(t *testing.T) {
	page := &tree.Page{PageNode: &tree.PageNode{
		ID:    "page-a",
		Title: "Page A",
		Slug:  "page",
		Kind:  tree.NodeKindPage,
	}}
	store := &fakeRevisionStore{
		commits: []gitrevisions.Commit{{Hash: "path-reuse"}},
		filesAt: map[string]map[string]string{
			"path-reuse": {
				"page.md": "---\nleafwiki_id: page-b\nleafwiki_title: Page B\n---\n# Page B\n",
			},
		},
		changedPaths: map[string][]string{
			"path-reuse": {"page.md"},
		},
	}
	service, err := NewService(ServiceOptions{
		Enabled: true,
		Tree:    &fakeTreeReconstructor{},
		Store:   store,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	if _, err := service.GetPageRevisionSnapshot(context.Background(), page, "path-reuse"); err == nil {
		t.Fatalf("GetPageRevisionSnapshot accepted reused path with different leafwiki_id, want error")
	}
}

func TestServiceSyncNowCreatesNewCommitForWritebackOnlySync(t *testing.T) {
	dataDir := t.TempDir()
	rootDir := filepath.Join(t.TempDir(), "workspace")
	writeMarkdown(t, filepath.Join(rootDir, "needs-metadata.md"), "# Needs Metadata\n\nbody")
	store, err := gitrevisions.Open(gitrevisions.StoreOptions{DataDir: dataDir, RootDir: rootDir})
	if err != nil {
		t.Fatalf("Open store: %v", err)
	}
	firstCommit, err := store.Capture(context.Background(), gitrevisions.CommitRequest{
		Reason: ReasonExplicit,
		Source: SourceFilesystem,
		Actor:  PublicEditorActor(),
	})
	if err != nil {
		t.Fatalf("capture raw missing-metadata commit: %v", err)
	}
	treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
	service, err := NewService(ServiceOptions{
		Enabled: true,
		DataDir: dataDir,
		RootDir: rootDir,
		Tree:    treeService,
		Store:   store,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	status, err := service.SyncNow(context.Background(), SyncRequest{
		Reason: ReasonExplicit,
		Source: SourceFilesystem,
		Actor:  PublicEditorActor(),
	})
	if err != nil {
		t.Fatalf("SyncNow: %v", err)
	}
	if status.LastCommitHash == firstCommit.Hash {
		t.Fatalf("writeback-only sync amended previous commit %s, want new commit", firstCommit.Hash)
	}
	snapshots, err := service.ListSnapshots(context.Background(), 10)
	if err != nil {
		t.Fatalf("ListSnapshots: %v", err)
	}
	if len(snapshots) != 2 {
		t.Fatalf("snapshot count = %d, want raw commit plus writeback commit: %#v", len(snapshots), snapshots)
	}
}

func TestServiceSyncNowReportsValidationForSkippedInvalidSlugMarkdown(t *testing.T) {
	dataDir := t.TempDir()
	rootDir := filepath.Join(t.TempDir(), "workspace")
	treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
	writeMarkdown(t, filepath.Join(rootDir, "Bad Slug.md"), "---\nleafwiki_id: bad-slug\nleafwiki_title: Bad Slug\n---\n# Bad Slug\n")
	service, err := NewService(ServiceOptions{
		Enabled: true,
		DataDir: dataDir,
		RootDir: rootDir,
		Tree:    treeService,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	status, err := service.SyncNow(context.Background(), SyncRequest{
		Reason: ReasonExplicit,
		Source: SourceFilesystem,
		Actor:  PublicEditorActor(),
	})
	if err != nil {
		t.Fatalf("SyncNow: %v", err)
	}

	if len(status.ValidationErrors) == 0 {
		t.Fatalf("ValidationErrors empty, want invalid slug error")
	}
	if status.ValidationErrors[0].Path != "Bad Slug.md" {
		t.Fatalf("validation error path = %q, want Bad Slug.md", status.ValidationErrors[0].Path)
	}
}

func TestServiceStartWatcherSyncsMarkdownEvents(t *testing.T) {
	fakeTree := &fakeTreeReconstructor{}
	fakeStore := &fakeRevisionStore{
		capture: &gitrevisions.Commit{
			Hash:                 "event-commit",
			ChangedMarkdownPaths: []string{"docs/a.md"},
		},
	}
	fakeWatcher := newFakeWatcher()
	service, err := NewService(ServiceOptions{
		Enabled: true,
		RootDir: "/workspace",
		Tree:    fakeTree,
		Store:   fakeStore,
		WatcherFactory: func(string) (fileWatcher, error) {
			return fakeWatcher, nil
		},
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := service.StartWatcher(ctx); err != nil {
		t.Fatalf("StartWatcher: %v", err)
	}
	fakeWatcher.events <- watcherEvent{Path: "/workspace/docs/a.md"}

	waitUntil(t, func() bool { return fakeTree.reconstructCount() == 1 })
	status := service.Status()
	if !status.WatcherEnabled || !status.WatcherRunning {
		t.Fatalf("watcher status = enabled:%v running:%v, want enabled and running", status.WatcherEnabled, status.WatcherRunning)
	}
	if status.PendingEventCount != 0 {
		t.Fatalf("PendingEventCount = %d, want 0", status.PendingEventCount)
	}
	if strings.Join(status.RecentChangedMarkdownPaths, ",") != "docs/a.md" {
		t.Fatalf("RecentChangedMarkdownPaths = %#v", status.RecentChangedMarkdownPaths)
	}
	if fakeStore.captureCalls != 1 {
		t.Fatalf("captureCalls = %d, want 1", fakeStore.captureCalls)
	}
}

func TestServiceStartWatcherSyncsUppercaseMarkdownEvents(t *testing.T) {
	fakeTree := &fakeTreeReconstructor{}
	fakeStore := &fakeRevisionStore{
		capture: &gitrevisions.Commit{
			Hash:                 "event-commit",
			ChangedMarkdownPaths: []string{"Page.MD"},
		},
	}
	fakeWatcher := newFakeWatcher()
	service, err := NewService(ServiceOptions{
		Enabled: true,
		RootDir: "/workspace",
		Tree:    fakeTree,
		Store:   fakeStore,
		WatcherFactory: func(string) (fileWatcher, error) {
			return fakeWatcher, nil
		},
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := service.StartWatcher(ctx); err != nil {
		t.Fatalf("StartWatcher: %v", err)
	}
	fakeWatcher.events <- watcherEvent{Path: "/workspace/Page.MD"}

	waitUntil(t, func() bool { return fakeTree.reconstructCount() == 1 })
	if fakeStore.captureCalls != 1 {
		t.Fatalf("captureCalls = %d, want uppercase Markdown event to trigger sync", fakeStore.captureCalls)
	}
}

func TestServiceStartWatcherCoalescesDuplicateMarkdownEvents(t *testing.T) {
	fakeTree := &fakeTreeReconstructor{}
	fakeStore := &fakeRevisionStore{
		capture: &gitrevisions.Commit{
			Hash:                 "event-commit",
			ChangedMarkdownPaths: []string{"docs/a.md"},
		},
	}
	fakeWatcher := newFakeWatcher()
	service, err := NewService(ServiceOptions{
		Enabled: true,
		RootDir: "/workspace",
		Tree:    fakeTree,
		Store:   fakeStore,
		WatcherFactory: func(string) (fileWatcher, error) {
			return fakeWatcher, nil
		},
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := service.StartWatcher(ctx); err != nil {
		t.Fatalf("StartWatcher: %v", err)
	}
	fakeWatcher.events <- watcherEvent{Path: "/workspace/docs/a.md"}
	fakeWatcher.events <- watcherEvent{Path: "/workspace/docs/a.md"}

	waitUntil(t, func() bool { return fakeTree.reconstructCount() == 1 })
	time.Sleep(350 * time.Millisecond)
	if fakeStore.captureCalls != 1 {
		t.Fatalf("captureCalls = %d, want duplicate watcher events coalesced into one sync", fakeStore.captureCalls)
	}
}

func TestServiceStartWatcherDroppedEventRecordsStatusAndSyncs(t *testing.T) {
	fakeTree := &fakeTreeReconstructor{}
	fakeStore := &fakeRevisionStore{capture: &gitrevisions.Commit{Hash: "drop-commit"}}
	fakeWatcher := newFakeWatcher()
	service, err := NewService(ServiceOptions{
		Enabled: true,
		RootDir: "/workspace",
		Tree:    fakeTree,
		Store:   fakeStore,
		WatcherFactory: func(string) (fileWatcher, error) {
			return fakeWatcher, nil
		},
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := service.StartWatcher(ctx); err != nil {
		t.Fatalf("StartWatcher: %v", err)
	}
	fakeWatcher.dropped <- watcherEvent{Path: "/workspace/docs/a.md", Dropped: true}

	waitUntil(t, func() bool { return fakeTree.reconstructCount() == 1 })
	status := service.Status()
	if !strings.Contains(status.LastError, "watcher dropped events") {
		t.Fatalf("LastError = %q, want dropped event status", status.LastError)
	}
	if fakeStore.captureCalls != 1 {
		t.Fatalf("captureCalls = %d, want 1", fakeStore.captureCalls)
	}
}

func TestServiceStartWatcherErrorRecordsStatusAndSyncs(t *testing.T) {
	fakeTree := &fakeTreeReconstructor{}
	fakeStore := &fakeRevisionStore{capture: &gitrevisions.Commit{Hash: "error-commit"}}
	fakeWatcher := newFakeWatcher()
	fakeWatcher.watchErr = errors.New("watcher platform error")
	service, err := NewService(ServiceOptions{
		Enabled: true,
		RootDir: "/workspace",
		Tree:    fakeTree,
		Store:   fakeStore,
		WatcherFactory: func(string) (fileWatcher, error) {
			return fakeWatcher, nil
		},
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := service.StartWatcher(ctx); err != nil {
		t.Fatalf("StartWatcher: %v", err)
	}

	waitUntil(t, func() bool { return fakeTree.reconstructCount() == 1 })
	status := service.Status()
	if !strings.Contains(status.LastError, "watcher platform error") {
		t.Fatalf("LastError = %q, want watcher platform error", status.LastError)
	}
	if fakeStore.captureCalls != 1 {
		t.Fatalf("captureCalls = %d, want 1", fakeStore.captureCalls)
	}
}

func TestServiceStartWatcherIgnoresTemporaryFiles(t *testing.T) {
	fakeTree := &fakeTreeReconstructor{}
	fakeStore := &fakeRevisionStore{capture: &gitrevisions.Commit{Hash: "temp-commit"}}
	fakeWatcher := newFakeWatcher()
	service, err := NewService(ServiceOptions{
		Enabled: true,
		RootDir: "/workspace",
		Tree:    fakeTree,
		Store:   fakeStore,
		WatcherFactory: func(string) (fileWatcher, error) {
			return fakeWatcher, nil
		},
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := service.StartWatcher(ctx); err != nil {
		t.Fatalf("StartWatcher: %v", err)
	}
	fakeWatcher.events <- watcherEvent{Path: "/workspace/page.md.swp"}
	fakeWatcher.events <- watcherEvent{Path: "/workspace/.DS_Store"}

	time.Sleep(50 * time.Millisecond)
	if fakeStore.captureCalls != 0 {
		t.Fatalf("captureCalls = %d, want temporary files ignored", fakeStore.captureCalls)
	}
}

func writeMarkdown(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create parent for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func mustGetPage(t *testing.T, treeService *tree.TreeService, id string) *tree.Page {
	t.Helper()
	page, err := treeService.GetPage(id)
	if err != nil {
		t.Fatalf("GetPage %s: %v", id, err)
	}
	return page
}

func mustGetOnlyPage(t *testing.T, treeService *tree.TreeService) *tree.Page {
	t.Helper()
	var ids []string
	if err := treeService.WalkNodes(func(id string) error {
		ids = append(ids, id)
		return nil
	}); err != nil {
		t.Fatalf("WalkNodes: %v", err)
	}
	if len(ids) != 1 {
		t.Fatalf("page ids = %#v, want one page", ids)
	}
	return mustGetPage(t, treeService, ids[0])
}

func revisionIDs(revisions []*revision.Revision) []string {
	ids := make([]string, 0, len(revisions))
	for _, rev := range revisions {
		if rev == nil {
			ids = append(ids, "")
			continue
		}
		ids = append(ids, rev.ID)
	}
	return ids
}

type fakeTreeReconstructor struct {
	reconstructs int32
	err          error
}

func (f *fakeTreeReconstructor) ReconstructTreeFromFS() error {
	atomic.AddInt32(&f.reconstructs, 1)
	return f.err
}

func (f *fakeTreeReconstructor) reconstructCount() int {
	return int(atomic.LoadInt32(&f.reconstructs))
}

type fakeRevisionStore struct {
	capture                           *gitrevisions.Commit
	captureErr                        error
	captureCalls                      int
	commits                           []gitrevisions.Commit
	filesAt                           map[string]map[string]string
	changedPaths                      map[string][]string
	scannedCommits                    int
	filesAtCalls                      int
	restoreDocumentToPathCalls        int
	restoreDocumentContentToPathCalls int
	restoredContent                   string
}

func (f *fakeRevisionStore) Capture(context.Context, gitrevisions.CommitRequest) (*gitrevisions.Commit, error) {
	f.captureCalls++
	if f.captureErr != nil {
		return nil, f.captureErr
	}
	if f.capture == nil {
		return nil, nil
	}
	commit := *f.capture
	if commit.Hash != "" {
		commit.Created = true
	}
	return &commit, nil
}

func (f *fakeRevisionStore) Amend(context.Context, gitrevisions.CommitRequest) (*gitrevisions.Commit, error) {
	return f.capture, nil
}

func (f *fakeRevisionStore) ListCommits(_ context.Context, req gitrevisions.ListRequest) ([]gitrevisions.Commit, error) {
	if req.Limit <= 0 || req.Limit >= len(f.commits) {
		return f.commits, nil
	}
	return f.commits[:req.Limit], nil
}

func (f *fakeRevisionStore) ForEachCommit(_ context.Context, visit func(gitrevisions.Commit) (bool, error)) error {
	for _, commit := range f.commits {
		f.scannedCommits++
		keepGoing, err := visit(commit)
		if err != nil {
			return err
		}
		if !keepGoing {
			return nil
		}
	}
	return nil
}

func (f *fakeRevisionStore) ChangedMarkdownPaths(_ context.Context, hash string) ([]string, error) {
	return f.changedPaths[hash], nil
}

func (f *fakeRevisionStore) ChangedMarkdownContents(_ context.Context, hash string) (map[string]string, error) {
	contents := make(map[string]string)
	files := f.filesAt[hash]
	for _, path := range f.changedPaths[hash] {
		if content, ok := files[path]; ok {
			contents[path] = content
		}
	}
	return contents, nil
}

func (f *fakeRevisionStore) GetCommit(_ context.Context, hash string) (gitrevisions.Commit, error) {
	for _, commit := range f.commits {
		if commit.Hash == hash {
			return commit, nil
		}
	}
	if f.capture != nil && f.capture.Hash == hash {
		return *f.capture, nil
	}
	return gitrevisions.Commit{}, errors.New("commit not found")
}

func (f *fakeRevisionStore) RestoreWorkspace(context.Context, string, gitrevisions.CommitRequest) (*gitrevisions.Commit, error) {
	return f.capture, nil
}

func (f *fakeRevisionStore) RestoreDocument(context.Context, string, string, gitrevisions.CommitRequest) (*gitrevisions.Commit, error) {
	return f.capture, nil
}

func (f *fakeRevisionStore) RestoreDocumentToPath(context.Context, string, string, string, gitrevisions.CommitRequest) (*gitrevisions.Commit, error) {
	f.restoreDocumentToPathCalls++
	return f.capture, nil
}

func (f *fakeRevisionStore) RestoreDocumentContentToPath(_ context.Context, _ string, content string, _ gitrevisions.CommitRequest) (*gitrevisions.Commit, error) {
	f.restoreDocumentContentToPathCalls++
	f.restoredContent = content
	return f.capture, nil
}

func (f *fakeRevisionStore) FilesAt(_ context.Context, hash string) (map[string]string, error) {
	f.filesAtCalls++
	if f.filesAt == nil {
		return nil, nil
	}
	return f.filesAt[hash], nil
}

type fakeWatcher struct {
	events   chan watcherEvent
	dropped  chan watcherEvent
	watchErr error
}

func newFakeWatcher() *fakeWatcher {
	return &fakeWatcher{
		events:  make(chan watcherEvent, 10),
		dropped: make(chan watcherEvent, 10),
	}
}

func (f *fakeWatcher) Watch(ctx context.Context) error {
	if f.watchErr != nil {
		return f.watchErr
	}
	<-ctx.Done()
	return ctx.Err()
}

func (f *fakeWatcher) Events() <-chan watcherEvent {
	return f.events
}

func (f *fakeWatcher) Dropped() <-chan watcherEvent {
	return f.dropped
}

func waitUntil(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("condition not met before timeout")
}
