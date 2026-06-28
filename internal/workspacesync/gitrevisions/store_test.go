package gitrevisions

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	git "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"

	"github.com/perber/wiki/internal/core/identity"
)

func actorIDStrings(ids []ActorID) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, id.String())
	}
	return out
}

var _ = It("StoreInitialSnapshotTracksMarkdownOnly", func() {
	t := GinkgoT()
	dataDir := t.TempDir()
	rootDir := filepath.Join(t.TempDir(), "workspace")
	if err := os.MkdirAll(filepath.Join(rootDir, "nested"), 0o755); err != nil {
		t.Fatalf("create nested dir: %v", err)
	}
	writeFile(t, filepath.Join(rootDir, "a.md"), "# A\n")
	writeFile(t, filepath.Join(rootDir, "nested", "b.md"), "# B\n")
	writeFile(t, filepath.Join(rootDir, ".obsidian", "local.md"), "# Local\n")
	writeFile(t, filepath.Join(rootDir, "image.png"), "png")
	writeFile(t, filepath.Join(rootDir, "draft.md.swp"), "# swap\n")
	writeFile(t, filepath.Join(rootDir, ".DS_Store"), "ds")

	store, err := Open(StoreOptions{DataDir: dataDir, RootDir: rootDir})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	commit, err := store.Capture(context.Background(), CommitRequest{
		Reason: ReasonStartup,
		Source: SourceFilesystem,
		Actor:  PublicEditorActor(),
	})
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}

	if commit.Hash == "" {
		t.Fatalf("expected initial commit hash")
	}
	if _, err := os.Stat(filepath.Join(dataDir, ".leafwiki", "git")); err != nil {
		t.Fatalf("internal git dir missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(rootDir, ".git")); !os.IsNotExist(err) {
		t.Fatalf("root .git state = %v, want absent", err)
	}

	files, err := store.FilesAt(context.Background(), identity.CommitHashFromString(commit.Hash))
	if err != nil {
		t.Fatalf("FilesAt: %v", err)
	}
	want := map[string]string{
		"a.md":        "# A\n",
		"nested/b.md": "# B\n",
	}
	if len(files) != len(want) {
		t.Fatalf("files = %#v, want %#v", files, want)
	}
	for path, content := range want {
		if files[path] != content {
			t.Fatalf("files[%q] = %q, want %q", path, files[path], content)
		}
	}
	for _, ignored := range []string{"image.png", "draft.md.swp", ".DS_Store", ".obsidian/local.md"} {
		if _, ok := files[ignored]; ok {
			t.Fatalf("ignored file %q was tracked: %#v", ignored, files)
		}
	}
})

var _ = It("StoreCaptureTracksUppercaseMarkdownExtension", func() {
	t := GinkgoT()
	dataDir := t.TempDir()
	rootDir := filepath.Join(t.TempDir(), "workspace")
	writeFile(t, filepath.Join(rootDir, "Page.MD"), "# Page\n")

	store, err := Open(StoreOptions{DataDir: dataDir, RootDir: rootDir})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	commit, err := store.Capture(context.Background(), CommitRequest{
		Reason: ReasonExplicit,
		Source: SourceFilesystem,
		Actor:  PublicEditorActor(),
	})
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}

	if strings.Join(commit.ChangedMarkdownPaths, ",") != "Page.MD" {
		t.Fatalf("ChangedMarkdownPaths = %#v, want Page.MD", commit.ChangedMarkdownPaths)
	}
	files, err := store.FilesAt(context.Background(), identity.CommitHashFromString(commit.Hash))
	if err != nil {
		t.Fatalf("FilesAt: %v", err)
	}
	if files["Page.MD"] != "# Page\n" {
		t.Fatalf("files = %#v, want Page.MD tracked", files)
	}
})

var _ = It("StoreCaptureWritesRequiredTrailersAndChangedPaths", func() {
	t := GinkgoT()
	dataDir := t.TempDir()
	rootDir := filepath.Join(t.TempDir(), "workspace")
	writeFile(t, filepath.Join(rootDir, "a.md"), "# A\n")

	store, err := Open(StoreOptions{DataDir: dataDir, RootDir: rootDir})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	commit, err := store.Capture(context.Background(), CommitRequest{
		Reason: ReasonExplicit,
		Source: SourceFilesystem,
		Actor:  PublicEditorActor(),
	})
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}

	if commit.ChangedMarkdownCount != 1 {
		t.Fatalf("ChangedMarkdownCount = %d, want 1", commit.ChangedMarkdownCount)
	}
	if len(commit.ChangedMarkdownPaths) != 1 || commit.ChangedMarkdownPaths[0] != "a.md" {
		t.Fatalf("ChangedMarkdownPaths = %#v, want [a.md]", commit.ChangedMarkdownPaths)
	}

	head, err := store.repo.Head()
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	headCommit, err := store.repo.CommitObject(head.Hash())
	if err != nil {
		t.Fatalf("CommitObject: %v", err)
	}
	for _, want := range []string{
		"LeafWiki-Source: filesystem",
		"LeafWiki-Reason: explicit",
		"LeafWiki-Actor: public-editor",
		"LeafWiki-Changed-Markdown: 1",
	} {
		if !strings.Contains(headCommit.Message, want) {
			t.Fatalf("commit message missing %q:\n%s", want, headCommit.Message)
		}
	}
	if !strings.Contains(headCommit.Message, "LeafWiki-Batch: ") {
		t.Fatalf("commit message missing LeafWiki-Batch trailer:\n%s", headCommit.Message)
	}
})

var _ = It("StoreAmendUsesRequestedChangedMarkdownPathsForTrailers", func() {
	t := GinkgoT()
	dataDir := t.TempDir()
	rootDir := filepath.Join(t.TempDir(), "workspace")
	writeFile(t, filepath.Join(rootDir, "a.md"), "# A\n")
	writeFile(t, filepath.Join(rootDir, "b.md"), "# B\n")

	store, err := Open(StoreOptions{DataDir: dataDir, RootDir: rootDir})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	first, err := store.Capture(context.Background(), CommitRequest{
		Reason: ReasonExplicit,
		Source: SourceFilesystem,
		Actor:  PublicEditorActor(),
	})
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}
	writeFile(t, filepath.Join(rootDir, "b.md"), "# B\n\nmetadata writeback\n")

	amended, err := store.Amend(context.Background(), CommitRequest{
		Reason:               ReasonExplicit,
		Source:               SourceFilesystem,
		Actor:                PublicEditorActor(),
		BatchID:              first.BatchID,
		ChangedMarkdownPaths: first.ChangedMarkdownPaths,
	})
	if err != nil {
		t.Fatalf("Amend: %v", err)
	}

	if amended.ChangedMarkdownCount != 2 {
		t.Fatalf("ChangedMarkdownCount = %d, want original two-file snapshot count", amended.ChangedMarkdownCount)
	}
	if strings.Join(amended.ChangedMarkdownPaths, ",") != "a.md,b.md" {
		t.Fatalf("ChangedMarkdownPaths = %#v, want original changed paths", amended.ChangedMarkdownPaths)
	}
	head, err := store.repo.Head()
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	headCommit, err := store.repo.CommitObject(head.Hash())
	if err != nil {
		t.Fatalf("CommitObject: %v", err)
	}
	if !strings.Contains(headCommit.Message, "LeafWiki-Changed-Markdown: 2") {
		t.Fatalf("amended commit message missing original changed count:\n%s", headCommit.Message)
	}
})

var _ = It("StoreListCommitsResumesAfterCursorHash", func() {
	t := GinkgoT()
	dataDir := t.TempDir()
	rootDir := filepath.Join(t.TempDir(), "workspace")
	store, err := Open(StoreOptions{DataDir: dataDir, RootDir: rootDir})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	writeFile(t, filepath.Join(rootDir, "page.md"), "# Page 1\n")
	oldest, err := store.Capture(context.Background(), CommitRequest{
		Reason: ReasonExplicit,
		Source: SourceFilesystem,
		Actor:  PublicEditorActor(),
	})
	if err != nil {
		t.Fatalf("Capture oldest: %v", err)
	}
	writeFile(t, filepath.Join(rootDir, "page.md"), "# Page 2\n")
	cursor, err := store.Capture(context.Background(), CommitRequest{
		Reason: ReasonExplicit,
		Source: SourceFilesystem,
		Actor:  PublicEditorActor(),
	})
	if err != nil {
		t.Fatalf("Capture cursor: %v", err)
	}
	writeFile(t, filepath.Join(rootDir, "page.md"), "# Page 3\n")
	if _, err := store.Capture(context.Background(), CommitRequest{
		Reason: ReasonExplicit,
		Source: SourceFilesystem,
		Actor:  PublicEditorActor(),
	}); err != nil {
		t.Fatalf("Capture newest: %v", err)
	}

	commits, err := store.ListCommits(context.Background(), ListRequest{Cursor: identity.CommitHashFromString(cursor.Hash), Limit: 2})
	if err != nil {
		t.Fatalf("ListCommits: %v", err)
	}

	if len(commits) != 1 {
		t.Fatalf("commit count after cursor = %d, want 1: %#v", len(commits), commits)
	}
	if commits[0].Hash != oldest.Hash {
		t.Fatalf("commit after cursor = %s, want %s", commits[0].Hash, oldest.Hash)
	}
})

var _ = It("StoreListCommitsReturnsWorkspaceMetadata", func() {
	t := GinkgoT()
	dataDir := t.TempDir()
	rootDir := filepath.Join(t.TempDir(), "workspace")
	writeFile(t, filepath.Join(rootDir, "a.md"), "# A\n")
	store, err := Open(StoreOptions{DataDir: dataDir, RootDir: rootDir})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := store.Capture(context.Background(), CommitRequest{
		Reason: ReasonExplicit,
		Source: SourceFilesystem,
		Actor:  Actor{ID: "alice", Name: "Alice", Email: "alice@example.test"},
	}); err != nil {
		t.Fatalf("Capture: %v", err)
	}

	commits, err := store.ListCommits(context.Background(), ListRequest{Limit: 1})
	if err != nil {
		t.Fatalf("ListCommits: %v", err)
	}
	if len(commits) != 1 {
		t.Fatalf("commits = %#v, want 1 commit", commits)
	}
	got := commits[0]
	if got.Source != SourceFilesystem || got.Reason != ReasonExplicit {
		t.Fatalf("commit source/reason = %q/%q, want filesystem/explicit", got.Source, got.Reason)
	}
	if got.AuthorID != "alice" || got.AuthorName != "Alice" || got.AuthorEmail != "alice@example.test" {
		t.Fatalf("commit author = %#v, want Alice metadata", got)
	}
	if got.ChangedMarkdownCount != 1 {
		t.Fatalf("ChangedMarkdownCount = %d, want 1", got.ChangedMarkdownCount)
	}
	if got.CreatedAt.IsZero() {
		t.Fatalf("CreatedAt is zero")
	}
	if got.Message != "LeafWiki workspace sync" {
		t.Fatalf("Message = %q, want title line", got.Message)
	}
})

var _ = It("StoreCaptureRecordsAdditionalActorTrailers", func() {
	t := GinkgoT()
	dataDir := t.TempDir()
	rootDir := filepath.Join(t.TempDir(), "workspace")
	writeFile(t, filepath.Join(rootDir, "a.md"), "# A\n")
	store, err := Open(StoreOptions{DataDir: dataDir, RootDir: rootDir})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	if _, err := store.Capture(context.Background(), CommitRequest{
		Reason: ReasonExplicit,
		Source: SourceWeb,
		Actor:  Actor{ID: "alice", Name: "Alice", Email: "alice@example.test"},
		AdditionalActors: []Actor{
			{ID: "bob", Name: "Bob", Email: "bob@example.test"},
			{ID: "alice", Name: "Alice", Email: "alice@example.test"},
		},
	}); err != nil {
		t.Fatalf("Capture: %v", err)
	}

	head, err := store.repo.Head()
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	headCommit, err := store.repo.CommitObject(head.Hash())
	if err != nil {
		t.Fatalf("CommitObject: %v", err)
	}
	if strings.Count(headCommit.Message, "LeafWiki-Actor: alice") != 1 {
		t.Fatalf("commit message should contain alice once:\n%s", headCommit.Message)
	}
	if strings.Count(headCommit.Message, "LeafWiki-Actor: bob") != 1 {
		t.Fatalf("commit message should contain bob once:\n%s", headCommit.Message)
	}

	commits, err := store.ListCommits(context.Background(), ListRequest{Limit: 1})
	if err != nil {
		t.Fatalf("ListCommits: %v", err)
	}
	if len(commits) != 1 {
		t.Fatalf("commits = %#v, want 1", commits)
	}
	if commits[0].AuthorID != "alice" {
		t.Fatalf("AuthorID = %q, want primary actor alice", commits[0].AuthorID)
	}
	if strings.Join(actorIDStrings(commits[0].ActorIDs), ",") != "alice,bob" {
		t.Fatalf("ActorIDs = %#v, want alice,bob", commits[0].ActorIDs)
	}
})

var _ = It("StoreForEachCommitStopsWhenVisitorReturnsFalse", func() {
	t := GinkgoT()
	dataDir := t.TempDir()
	rootDir := filepath.Join(t.TempDir(), "workspace")
	store, err := Open(StoreOptions{DataDir: dataDir, RootDir: rootDir})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	for i := 0; i < 3; i++ {
		writeFile(t, filepath.Join(rootDir, "page.md"), "# Page "+strconv.Itoa(i)+"\n")
		if _, err := store.Capture(context.Background(), CommitRequest{
			Reason: ReasonExplicit,
			Source: SourceFilesystem,
			Actor:  PublicEditorActor(),
		}); err != nil {
			t.Fatalf("Capture %d: %v", i, err)
		}
	}

	visited := 0
	if err := store.ForEachCommit(context.Background(), func(Commit) (bool, error) {
		visited++
		return false, nil
	}); err != nil {
		t.Fatalf("ForEachCommit: %v", err)
	}

	if visited != 1 {
		t.Fatalf("visited = %d, want 1", visited)
	}
})

var _ = It("StoreChangedMarkdownContentsReturnsOnlyCurrentChangedMarkdownBlobs", func() {
	t := GinkgoT()
	dataDir := t.TempDir()
	rootDir := filepath.Join(t.TempDir(), "workspace")
	store, err := Open(StoreOptions{DataDir: dataDir, RootDir: rootDir})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	writeFile(t, filepath.Join(rootDir, "changed.md"), "# Old\n")
	writeFile(t, filepath.Join(rootDir, "deleted.md"), "# Deleted\n")
	writeFile(t, filepath.Join(rootDir, "unchanged.md"), "# Unchanged\n")
	if _, err := store.Capture(context.Background(), CommitRequest{
		Reason: ReasonExplicit,
		Source: SourceFilesystem,
		Actor:  PublicEditorActor(),
	}); err != nil {
		t.Fatalf("Capture initial: %v", err)
	}

	writeFile(t, filepath.Join(rootDir, "changed.md"), "# New\n")
	if err := os.Remove(filepath.Join(rootDir, "deleted.md")); err != nil {
		t.Fatalf("remove deleted.md: %v", err)
	}
	commit, err := store.Capture(context.Background(), CommitRequest{
		Reason: ReasonExplicit,
		Source: SourceFilesystem,
		Actor:  PublicEditorActor(),
	})
	if err != nil {
		t.Fatalf("Capture changed: %v", err)
	}

	paths, err := store.ChangedMarkdownPaths(context.Background(), identity.CommitHashFromString(commit.Hash))
	if err != nil {
		t.Fatalf("ChangedMarkdownPaths: %v", err)
	}
	if strings.Join(paths, ",") != "changed.md,deleted.md" {
		t.Fatalf("changed paths = %#v, want changed.md and deleted.md", paths)
	}
	contents, err := store.ChangedMarkdownContents(context.Background(), identity.CommitHashFromString(commit.Hash))
	if err != nil {
		t.Fatalf("ChangedMarkdownContents: %v", err)
	}
	if contents["changed.md"] != "# New\n" {
		t.Fatalf("changed.md content = %q, want new content", contents["changed.md"])
	}
	if _, ok := contents["deleted.md"]; ok {
		t.Fatalf("deleted.md had current content: %#v", contents)
	}
	if _, ok := contents["unchanged.md"]; ok {
		t.Fatalf("unchanged.md was loaded as changed content: %#v", contents)
	}
})

var _ = It("StoreInternalGitDoesNotMutateContainingUserGitRepository", func() {
	t := GinkgoT()
	dataDir := t.TempDir()
	userRepoDir := filepath.Join(t.TempDir(), "user-repo")
	rootDir := filepath.Join(userRepoDir, "wiki")
	userGitDir := filepath.Join(userRepoDir, ".git")
	if err := os.MkdirAll(userGitDir, 0o755); err != nil {
		t.Fatalf("create user .git: %v", err)
	}
	userHead := filepath.Join(userGitDir, "HEAD")
	writeFile(t, userHead, "ref: refs/heads/main\n")
	writeFile(t, filepath.Join(rootDir, "page.md"), "# Page\n")

	store, err := Open(StoreOptions{DataDir: dataDir, RootDir: rootDir})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := store.Capture(context.Background(), CommitRequest{
		Reason: ReasonStartup,
		Source: SourceFilesystem,
		Actor:  PublicEditorActor(),
	}); err != nil {
		t.Fatalf("Capture: %v", err)
	}

	assertFileContent(t, userHead, "ref: refs/heads/main\n")
	if _, err := os.Stat(filepath.Join(rootDir, ".git")); !os.IsNotExist(err) {
		t.Fatalf("root .git state = %v, want absent", err)
	}
	if _, err := os.Stat(filepath.Join(dataDir, ".leafwiki", "git")); err != nil {
		t.Fatalf("internal git dir missing: %v", err)
	}
})

var _ = It("StoreOpenPreservesRootGitFile", func() {
	t := GinkgoT()
	dataDir := t.TempDir()
	rootDir := filepath.Join(t.TempDir(), "workspace")
	writeFile(t, filepath.Join(rootDir, "page.md"), "# Page\n")
	gitFile := filepath.Join(rootDir, ".git")
	gitFileContent := "gitdir: ../.git/modules/workspace\n"
	writeFile(t, gitFile, gitFileContent)

	store, err := Open(StoreOptions{DataDir: dataDir, RootDir: rootDir})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := store.Capture(context.Background(), CommitRequest{
		Reason: ReasonStartup,
		Source: SourceFilesystem,
		Actor:  PublicEditorActor(),
	}); err != nil {
		t.Fatalf("Capture: %v", err)
	}

	assertFileContent(t, gitFile, gitFileContent)
})

var _ = It("StoreCapturePrunesPreviouslyTrackedDotDirectoryMarkdown", func() {
	t := GinkgoT()
	dataDir := t.TempDir()
	rootDir := filepath.Join(t.TempDir(), "workspace")
	writeFile(t, filepath.Join(rootDir, "page.md"), "# Page\n")
	writeFile(t, filepath.Join(rootDir, ".obsidian", "local.md"), "# Local\n")

	store, err := Open(StoreOptions{DataDir: dataDir, RootDir: rootDir})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	wt, err := store.repo.Worktree()
	if err != nil {
		t.Fatalf("Worktree: %v", err)
	}
	if _, err := wt.Add("page.md"); err != nil {
		t.Fatalf("add page.md: %v", err)
	}
	if _, err := wt.Add(".obsidian/local.md"); err != nil {
		t.Fatalf("add legacy dot markdown: %v", err)
	}
	legacyHash, err := wt.Commit("legacy dot-directory markdown", &git.CommitOptions{
		Author:    signature(PublicEditorActor()),
		Committer: leafWikiCommitter(),
	})
	if err != nil {
		t.Fatalf("legacy commit: %v", err)
	}
	legacyCommit, err := store.repo.CommitObject(legacyHash)
	if err != nil {
		t.Fatalf("legacy CommitObject: %v", err)
	}
	legacyTree, err := legacyCommit.Tree()
	if err != nil {
		t.Fatalf("legacy Tree: %v", err)
	}
	if _, err := legacyTree.File(".obsidian/local.md"); err != nil {
		t.Fatalf("legacy dot markdown missing before prune: %v", err)
	}

	prune, err := store.Capture(context.Background(), CommitRequest{
		Reason: ReasonExplicit,
		Source: SourceFilesystem,
		Actor:  PublicEditorActor(),
	})
	if err != nil {
		t.Fatalf("prune Capture: %v", err)
	}
	if !prune.Created {
		t.Fatalf("prune commit not created: %#v", prune)
	}

	prunedCommit, err := store.repo.CommitObject(plumbing.NewHash(prune.Hash))
	if err != nil {
		t.Fatalf("pruned CommitObject: %v", err)
	}
	prunedTree, err := prunedCommit.Tree()
	if err != nil {
		t.Fatalf("pruned Tree: %v", err)
	}
	if _, err := prunedTree.File(".obsidian/local.md"); err == nil {
		t.Fatalf("pruned commit still tracks .obsidian/local.md")
	}
	files, err := store.FilesAt(context.Background(), identity.CommitHashFromString(prune.Hash))
	if err != nil {
		t.Fatalf("FilesAt prune: %v", err)
	}
	if _, ok := files[".obsidian/local.md"]; ok {
		t.Fatalf("FilesAt exposed pruned dot markdown: %#v", files)
	}
	assertFileContent(t, filepath.Join(rootDir, ".obsidian", "local.md"), "# Local\n")
})

var _ = It("StoreCaptureRecordsMarkdownDeletes", func() {
	t := GinkgoT()
	dataDir := t.TempDir()
	rootDir := filepath.Join(t.TempDir(), "workspace")
	writeFile(t, filepath.Join(rootDir, "keep.md"), "# Keep\n")
	writeFile(t, filepath.Join(rootDir, "remove.md"), "# Remove\n")

	store, err := Open(StoreOptions{DataDir: dataDir, RootDir: rootDir})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := store.Capture(context.Background(), CommitRequest{
		Reason: ReasonStartup,
		Source: SourceFilesystem,
		Actor:  PublicEditorActor(),
	}); err != nil {
		t.Fatalf("initial Capture: %v", err)
	}

	if err := os.Remove(filepath.Join(rootDir, "remove.md")); err != nil {
		t.Fatalf("remove markdown: %v", err)
	}
	commit, err := store.Capture(context.Background(), CommitRequest{
		Reason: ReasonExplicit,
		Source: SourceFilesystem,
		Actor:  PublicEditorActor(),
	})
	if err != nil {
		t.Fatalf("delete Capture: %v", err)
	}

	files, err := store.FilesAt(context.Background(), identity.CommitHashFromString(commit.Hash))
	if err != nil {
		t.Fatalf("FilesAt: %v", err)
	}
	if _, ok := files["remove.md"]; ok {
		t.Fatalf("removed markdown still tracked: %#v", files)
	}
	if files["keep.md"] != "# Keep\n" {
		t.Fatalf("keep.md = %q, want current content", files["keep.md"])
	}
})

var _ = It("StoreAmendRecordsMetadataWritebackInSameCommit", func() {
	t := GinkgoT()
	dataDir := t.TempDir()
	rootDir := filepath.Join(t.TempDir(), "workspace")
	writeFile(t, filepath.Join(rootDir, "page.md"), "# Page\n")

	store, err := Open(StoreOptions{DataDir: dataDir, RootDir: rootDir})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	first, err := store.Capture(context.Background(), CommitRequest{
		Reason: ReasonStartup,
		Source: SourceFilesystem,
		Actor:  PublicEditorActor(),
	})
	if err != nil {
		t.Fatalf("initial Capture: %v", err)
	}

	writeFile(t, filepath.Join(rootDir, "page.md"), "---\nleafwiki_id: page\n---\n# Page\n")
	amended, err := store.Amend(context.Background(), CommitRequest{
		Reason: ReasonStartup,
		Source: SourceSystem,
		Actor:  PublicEditorActor(),
	})
	if err != nil {
		t.Fatalf("Amend: %v", err)
	}

	if amended.Hash == first.Hash {
		t.Fatalf("amended commit hash = initial hash %s, want replacement commit", amended.Hash)
	}
	commits, err := store.ListCommits(context.Background(), ListRequest{Limit: 10})
	if err != nil {
		t.Fatalf("ListCommits: %v", err)
	}
	if len(commits) != 1 {
		t.Fatalf("commit count = %d, want 1: %#v", len(commits), commits)
	}
	files, err := store.FilesAt(context.Background(), identity.CommitHashFromString(amended.Hash))
	if err != nil {
		t.Fatalf("FilesAt: %v", err)
	}
	if files["page.md"] != "---\nleafwiki_id: page\n---\n# Page\n" {
		t.Fatalf("page.md = %q, want amended metadata content", files["page.md"])
	}
})

var _ = It("StoreRestoreWorkspaceRestoresMarkdownOnlyAndCreatesCommit", func() {
	t := GinkgoT()
	dataDir := t.TempDir()
	rootDir := filepath.Join(t.TempDir(), "workspace")
	writeFile(t, filepath.Join(rootDir, "one.md"), "# One A\n")
	writeFile(t, filepath.Join(rootDir, "two.md"), "# Two A\n")
	store, err := Open(StoreOptions{DataDir: dataDir, RootDir: rootDir})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	snapshot, err := store.Capture(context.Background(), CommitRequest{
		Reason: ReasonStartup,
		Source: SourceFilesystem,
		Actor:  PublicEditorActor(),
	})
	if err != nil {
		t.Fatalf("initial Capture: %v", err)
	}

	writeFile(t, filepath.Join(rootDir, "one.md"), "# One current\n")
	writeFile(t, filepath.Join(rootDir, "three.md"), "# Three current\n")
	writeFile(t, filepath.Join(rootDir, "image.png"), "png")
	restore, err := store.RestoreWorkspace(context.Background(), identity.CommitHashFromString(snapshot.Hash), CommitRequest{
		Reason: ReasonRestore,
		Source: SourceSystem,
		Actor:  PublicEditorActor(),
	})
	if err != nil {
		t.Fatalf("RestoreWorkspace: %v", err)
	}

	if restore.Hash == snapshot.Hash {
		t.Fatalf("restore hash = snapshot hash %s, want new commit", restore.Hash)
	}
	assertFileContent(t, filepath.Join(rootDir, "one.md"), "# One A\n")
	assertFileContent(t, filepath.Join(rootDir, "two.md"), "# Two A\n")
	if _, err := os.Stat(filepath.Join(rootDir, "three.md")); !os.IsNotExist(err) {
		t.Fatalf("three.md state = %v, want removed", err)
	}
	assertFileContent(t, filepath.Join(rootDir, "image.png"), "png")
	files, err := store.FilesAt(context.Background(), identity.CommitHashFromString(restore.Hash))
	if err != nil {
		t.Fatalf("FilesAt restore: %v", err)
	}
	if _, ok := files["three.md"]; ok {
		t.Fatalf("restore commit still tracks three.md: %#v", files)
	}
	if files["one.md"] != "# One A\n" || files["two.md"] != "# Two A\n" {
		t.Fatalf("restore files = %#v", files)
	}
})

var _ = It("StoreRestoreDocumentWritesHistoricalContentToCurrentPath", func() {
	t := GinkgoT()
	dataDir := t.TempDir()
	rootDir := filepath.Join(t.TempDir(), "workspace")
	writeFile(t, filepath.Join(rootDir, "docs", "page.md"), "# Previous\n")
	store, err := Open(StoreOptions{DataDir: dataDir, RootDir: rootDir})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	previous, err := store.Capture(context.Background(), CommitRequest{
		Reason: ReasonStartup,
		Source: SourceFilesystem,
		Actor:  PublicEditorActor(),
	})
	if err != nil {
		t.Fatalf("initial Capture: %v", err)
	}

	writeFile(t, filepath.Join(rootDir, "docs", "page.md"), "# Current\n")
	if _, err := store.Capture(context.Background(), CommitRequest{
		Reason: ReasonExplicit,
		Source: SourceFilesystem,
		Actor:  PublicEditorActor(),
	}); err != nil {
		t.Fatalf("current Capture: %v", err)
	}

	restore, err := store.RestoreDocument(context.Background(), "docs/page.md", identity.CommitHashFromString(previous.Hash), CommitRequest{
		Reason: ReasonRestore,
		Source: SourceSystem,
		Actor:  PublicEditorActor(),
	})
	if err != nil {
		t.Fatalf("RestoreDocument: %v", err)
	}

	if restore.Hash == previous.Hash {
		t.Fatalf("restore hash = previous hash %s, want new commit", restore.Hash)
	}
	assertFileContent(t, filepath.Join(rootDir, "docs", "page.md"), "# Previous\n")
	files, err := store.FilesAt(context.Background(), identity.CommitHashFromString(restore.Hash))
	if err != nil {
		t.Fatalf("FilesAt restore: %v", err)
	}
	if files["docs/page.md"] != "# Previous\n" {
		t.Fatalf("restore files = %#v, want previous content", files)
	}
})

var _ = It("IsManagedMarkdownRelPath rejects hidden, temporary, and non-markdown paths", func() {
	Expect(IsManagedMarkdownRelPath("docs/page.md")).To(BeTrue())
	Expect(IsManagedMarkdownRelPath("docs/Page.MD")).To(BeTrue())
	Expect(IsManagedMarkdownRelPath(".obsidian/page.md")).To(BeFalse())
	Expect(IsManagedMarkdownRelPath("docs/.draft.md")).To(BeFalse())
	Expect(IsManagedMarkdownRelPath("docs/page.md.swp")).To(BeFalse())
	Expect(IsManagedMarkdownRelPath("docs/image.png")).To(BeFalse())
})

var _ = It("StoreGetCommit returns captured commit metadata and reports missing commits", func() {
	t := GinkgoT()
	dataDir := t.TempDir()
	rootDir := filepath.Join(t.TempDir(), "workspace")
	writeFile(t, filepath.Join(rootDir, "docs", "page.md"), "# Page\n")
	store, err := Open(StoreOptions{DataDir: dataDir, RootDir: rootDir})
	Expect(err).NotTo(HaveOccurred())

	captured, err := store.Capture(context.Background(), CommitRequest{
		Reason: ReasonExplicit,
		Source: SourceMCP,
		Actor: Actor{
			ID:    NewActorIDUnchecked("agent-1"),
			Name:  "Agent One",
			Email: "agent-1@example.test",
		},
	})
	Expect(err).NotTo(HaveOccurred())

	commit, err := store.GetCommit(context.Background(), identity.CommitHashFromString(captured.Hash))
	Expect(err).NotTo(HaveOccurred())
	Expect(commit.Hash).To(Equal(captured.Hash))
	Expect(commit.AuthorID).To(Equal(NewActorIDUnchecked("agent-1")))
	Expect(commit.Source).To(Equal(SourceMCP))
	Expect(commit.Reason).To(Equal(ReasonExplicit))

	_, err = store.GetCommit(context.Background(), identity.CommitHashFromString("0000000000000000000000000000000000000000"))
	Expect(err).To(MatchError(ContainSubstring("load commit 0000000000000000000000000000000000000000")))
})

type testHelper interface {
	Helper()
	Fatalf(format string, args ...any)
}

func writeFile(t testHelper, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create parent for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func assertFileContent(t testHelper, path string, want string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(raw) != want {
		t.Fatalf("%s = %q, want %q", path, string(raw), want)
	}
}
