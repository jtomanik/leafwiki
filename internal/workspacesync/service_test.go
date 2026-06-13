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
	"github.com/perber/wiki/internal/links"
	"github.com/perber/wiki/internal/workspacesync/gitrevisions"
)

// Canonical Markdown links plan scenarios covered by tests in this file:
// - Relative link cannot escape the workspace root
// - Old extensionless page link migrates to .md
// - Old extensionless section link remains extensionless
// - Unresolved old extensionless page link becomes validation error
// - Ambiguous extensionless link is left as validation error
// - Migration is idempotent
// - Migration writeback is captured in revision history
// - Migration write failure reports sync validation state without losing raw content

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

// - Old extensionless page link migrates to .md
func TestServiceSyncNowRewritesResolvableLegacyPageLinkBeforeValidation(t *testing.T) {
	dataDir := t.TempDir()
	rootDir := filepath.Join(t.TempDir(), "workspace")
	treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
	if err := treeService.LoadTree(); err != nil {
		t.Fatalf("LoadTree: %v", err)
	}
	writeMarkdown(t, filepath.Join(rootDir, "docs", "a.md"), `---
leafwiki_id: page-a
leafwiki_title: Page A
---
# Page A

[B](/docs/b)
`)
	writeMarkdown(t, filepath.Join(rootDir, "docs", "b.md"), `---
leafwiki_id: page-b
leafwiki_title: Page B
---
# Page B
`)

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
	if len(status.ValidationErrors) != 0 {
		t.Fatalf("ValidationErrors = %#v, want none after canonical migration", status.ValidationErrors)
	}
	raw, err := os.ReadFile(filepath.Join(rootDir, "docs", "a.md"))
	if err != nil {
		t.Fatalf("ReadFile a.md: %v", err)
	}
	if !strings.Contains(string(raw), "[B](/docs/b.md)") {
		t.Fatalf("a.md = %q, want canonical .md page link", string(raw))
	}
	page, err := treeService.GetPage("page-a")
	if err != nil {
		t.Fatalf("GetPage page-a: %v", err)
	}
	if !strings.Contains(page.RawContent, "[B](/docs/b.md)") {
		t.Fatalf("page raw content = %q, want canonical .md page link after first sync", page.RawContent)
	}
}

// - Relative old page link migrates to relative .md
// - Existing canonical .md page link is not rewritten
func TestServiceSyncNowRelativeLegacyPageLinkMigratesAndCanonicalRelativeLinkStaysCanonical(t *testing.T) {
	dataDir := t.TempDir()
	rootDir := filepath.Join(t.TempDir(), "workspace")
	treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
	if err := treeService.LoadTree(); err != nil {
		t.Fatalf("LoadTree: %v", err)
	}
	writeMarkdown(t, filepath.Join(rootDir, "docs", "source", "a.md"), `---
leafwiki_id: page-a-relative
leafwiki_title: Page A Relative
---
# Page A Relative

[Legacy](../b)
[Canonical](../b.md)
`)
	writeMarkdown(t, filepath.Join(rootDir, "docs", "b.md"), `---
leafwiki_id: page-b-relative
leafwiki_title: Page B Relative
---
# Page B Relative
`)

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
	if len(status.ValidationErrors) != 0 {
		t.Fatalf("ValidationErrors = %#v, want none after canonical migration", status.ValidationErrors)
	}
	raw, err := os.ReadFile(filepath.Join(rootDir, "docs", "source", "a.md"))
	if err != nil {
		t.Fatalf("ReadFile source/a.md: %v", err)
	}
	content := string(raw)
	if strings.Count(content, "../b.md") != 2 || strings.Contains(content, "](../b)") {
		t.Fatalf("source/a.md = %q, want legacy relative link migrated and canonical link unchanged", content)
	}
}

// - Duplicate syntaxes do not create duplicate target identities after migration
func TestServiceSyncNowMigratedDuplicateSyntaxesIndexAsSinglePageTargetIdentity(t *testing.T) {
	dataDir := t.TempDir()
	rootDir := filepath.Join(t.TempDir(), "workspace")
	treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
	if err := treeService.LoadTree(); err != nil {
		t.Fatalf("LoadTree: %v", err)
	}
	linkStore, err := links.NewLinksStore(dataDir)
	if err != nil {
		t.Fatalf("NewLinksStore: %v", err)
	}
	defer func() {
		if err := linkStore.Close(); err != nil {
			t.Fatalf("Close link store: %v", err)
		}
	}()
	linkService := links.NewLinkService(dataDir, treeService, linkStore)
	writeMarkdown(t, filepath.Join(rootDir, "docs", "a.md"), `---
leafwiki_id: page-a-duplicate-syntax
leafwiki_title: Page A Duplicate Syntax
---
# Page A Duplicate Syntax

[Legacy](/docs/b)
[Canonical](/docs/b.md)
`)
	writeMarkdown(t, filepath.Join(rootDir, "docs", "b.md"), `---
leafwiki_id: page-b-duplicate-syntax
leafwiki_title: Page B Duplicate Syntax
---
# Page B Duplicate Syntax
`)

	service, err := NewService(ServiceOptions{
		Enabled: true,
		DataDir: dataDir,
		RootDir: rootDir,
		Tree:    treeService,
		AfterSync: func() error {
			return linkService.IndexAllPages()
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
	if len(status.ValidationErrors) != 0 {
		t.Fatalf("ValidationErrors = %#v, want none after canonical migration", status.ValidationErrors)
	}
	pageA, err := treeService.GetPage("page-a-duplicate-syntax")
	if err != nil {
		t.Fatalf("GetPage page-a-duplicate-syntax: %v", err)
	}
	linkStatus, err := linkService.GetLinkStatusForPage(pageA.ID, pageA.CalculatePath())
	if err != nil {
		t.Fatalf("GetLinkStatusForPage: %v", err)
	}
	if linkStatus.Counts.Outgoings != 1 || linkStatus.Counts.BrokenOutgoings != 0 {
		t.Fatalf("link status counts = %#v, want one healthy outgoing target", linkStatus.Counts)
	}
	if got := linkStatus.Outgoings[0].ToPageID; got != "page-b-duplicate-syntax" {
		t.Fatalf("ToPageID = %q, want page-b-duplicate-syntax", got)
	}
}

// - Migration writeback is captured in revision history
func TestServiceSyncNowKeepsRawAndCanonicalMigrationPageRevisions(t *testing.T) {
	dataDir := t.TempDir()
	rootDir := filepath.Join(t.TempDir(), "workspace")
	treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
	if err := treeService.LoadTree(); err != nil {
		t.Fatalf("LoadTree: %v", err)
	}
	writeMarkdown(t, filepath.Join(rootDir, "docs", "a.md"), `---
leafwiki_id: page-a
leafwiki_title: Page A
---
# Page A

[B](/docs/b)
`)
	writeMarkdown(t, filepath.Join(rootDir, "docs", "b.md"), `---
leafwiki_id: page-b
leafwiki_title: Page B
---
# Page B
`)

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

	page, err := treeService.GetPage("page-a")
	if err != nil {
		t.Fatalf("GetPage page-a: %v", err)
	}
	revisions, err := service.ListPageRevisions(context.Background(), page, "", 10)
	if err != nil {
		t.Fatalf("ListPageRevisions: %v", err)
	}
	if len(revisions.Revisions) != 2 {
		t.Fatalf("revision count = %d, want raw incoming content plus canonical writeback", len(revisions.Revisions))
	}

	var sawRaw, sawCanonical bool
	for _, rev := range revisions.Revisions {
		snapshot, err := service.GetPageRevisionSnapshot(context.Background(), page, rev.ID)
		if err != nil {
			t.Fatalf("GetPageRevisionSnapshot %s: %v", rev.ID, err)
		}
		if strings.Contains(snapshot.Content, "[B](/docs/b)\n") {
			sawRaw = true
		}
		if strings.Contains(snapshot.Content, "[B](/docs/b.md)") {
			sawCanonical = true
		}
	}
	if !sawRaw || !sawCanonical {
		t.Fatalf("revision history raw=%v canonical=%v, want both raw incoming and canonical writeback", sawRaw, sawCanonical)
	}
}

func TestServiceCanonicalMigrationRollsBackWhenLaterWriteFails(t *testing.T) {
	rootDir := filepath.Join(t.TempDir(), "workspace")
	service := &Service{rootDir: rootDir}

	firstPath := filepath.Join(rootDir, "a", "source.md")
	secondDir := filepath.Join(rootDir, "readonly")
	secondPath := filepath.Join(secondDir, "source.md")
	firstOriginal := `---
leafwiki_id: first-source
leafwiki_title: First Source
---
# First Source

[Target](/targets/first)
`
	secondOriginal := `---
leafwiki_id: second-source
leafwiki_title: Second Source
---
# Second Source

[Target](/targets/second)
`
	writeMarkdown(t, firstPath, firstOriginal)
	writeMarkdown(t, secondPath, secondOriginal)
	writeMarkdown(t, filepath.Join(rootDir, "targets", "first.md"), `---
leafwiki_id: first-target
leafwiki_title: First Target
---
# First Target
`)
	writeMarkdown(t, filepath.Join(rootDir, "targets", "second.md"), `---
leafwiki_id: second-target
leafwiki_title: Second Target
---
# Second Target
`)
	if err := os.Chmod(secondPath, 0o444); err != nil {
		t.Fatalf("chmod readonly source file: %v", err)
	}
	if err := os.Chmod(secondDir, 0o555); err != nil {
		t.Fatalf("chmod readonly source dir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(secondDir, 0o755)
		_ = os.Chmod(secondPath, 0o644)
	})
	if err := os.WriteFile(filepath.Join(secondDir, ".probe"), []byte("probe"), 0o644); err == nil {
		_ = os.Remove(filepath.Join(secondDir, ".probe"))
		t.Skip("filesystem permits writes to read-only test directory")
	}

	changed, err := service.migrateCanonicalMarkdownLinksLocked()

	if err == nil {
		t.Fatalf("migrateCanonicalMarkdownLinksLocked error = nil, want write failure")
	}
	if changed {
		t.Fatalf("changed = true, want no committed migration on write failure")
	}
	if got := readFileString(t, firstPath); got != firstOriginal {
		t.Fatalf("first source = %q, want original content after rollback", got)
	}
	if got := readFileString(t, secondPath); got != secondOriginal {
		t.Fatalf("second source = %q, want original content after failed write", got)
	}
}

func TestWriteCanonicalMarkdownRewritesAtomicallyRollsBackCommittedRename(t *testing.T) {
	rootDir := t.TempDir()
	firstPath := filepath.Join(rootDir, "first.md")
	blockingDir := filepath.Join(rootDir, "blocking")
	firstOriginal := "# First\n\n[Target](/target)\n"
	firstCanonical := "# First\n\n[Target](/target.md)\n"
	writeMarkdown(t, firstPath, firstOriginal)
	if err := os.MkdirAll(blockingDir, 0o755); err != nil {
		t.Fatalf("mkdir blocking dir: %v", err)
	}

	err := writeCanonicalMarkdownRewritesAtomically([]canonicalMarkdownRewrite{
		{
			Path:     firstPath,
			Original: []byte(firstOriginal),
			Content:  []byte(firstCanonical),
			Mode:     0o644,
		},
		{
			Path:    blockingDir,
			Content: []byte("not a markdown file"),
			Mode:    0o644,
		},
	})

	if err == nil {
		t.Fatalf("writeCanonicalMarkdownRewritesAtomically error = nil, want rename failure")
	}
	if got := readFileString(t, firstPath); got != firstOriginal {
		t.Fatalf("first source = %q, want original content after committed rename rollback", got)
	}
}

func TestServiceSyncNowRollsBackCanonicalMigrationWhenWritebackCaptureFails(t *testing.T) {
	dataDir := t.TempDir()
	rootDir := filepath.Join(t.TempDir(), "workspace")
	treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
	if err := treeService.LoadTree(); err != nil {
		t.Fatalf("LoadTree: %v", err)
	}
	sourcePath := filepath.Join(rootDir, "docs", "a.md")
	sourceOriginal := `---
leafwiki_id: page-a
leafwiki_title: Page A
---
# Page A

[B](/docs/b)
`
	writeMarkdown(t, sourcePath, sourceOriginal)
	writeMarkdown(t, filepath.Join(rootDir, "docs", "b.md"), `---
leafwiki_id: page-b
leafwiki_title: Page B
---
# Page B
`)
	captureErr := errors.New("writeback capture failed")
	service, err := NewService(ServiceOptions{
		Enabled: true,
		DataDir: dataDir,
		RootDir: rootDir,
		Tree:    treeService,
		Store: &fakeRevisionStore{
			capture:        &gitrevisions.Commit{Hash: "initial-commit"},
			captureErr:     captureErr,
			captureErrCall: 2,
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

	if !errors.Is(err, captureErr) {
		t.Fatalf("SyncNow error = %v, want %v", err, captureErr)
	}
	if !strings.Contains(status.LastError, "writeback capture failed") {
		t.Fatalf("LastError = %q, want writeback capture failure", status.LastError)
	}
	if got := readFileString(t, sourcePath); !strings.Contains(got, "[B](/docs/b)") || strings.Contains(got, "[B](/docs/b.md)") {
		t.Fatalf("source file = %q, want canonical migration rolled back after writeback capture failure", got)
	}
	page, err := treeService.GetPage("page-a")
	if err != nil {
		t.Fatalf("GetPage page-a: %v", err)
	}
	if !strings.Contains(page.RawContent, "[B](/docs/b)") || strings.Contains(page.RawContent, "[B](/docs/b.md)") {
		t.Fatalf("page raw content = %q, want reconstructed non-canonical content after rollback", page.RawContent)
	}
}

// - Migration write failure reports sync validation state without losing raw content
func TestServiceSyncNowStopsBeforeDerivedRebuildsWhenCanonicalMigrationWriteFails(t *testing.T) {
	dataDir := t.TempDir()
	rootDir := filepath.Join(t.TempDir(), "workspace")
	treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
	if err := treeService.LoadTree(); err != nil {
		t.Fatalf("LoadTree: %v", err)
	}
	sourcePath := filepath.Join(rootDir, "docs", "a.md")
	sourceOriginal := `---
leafwiki_id: page-a
leafwiki_title: Page A
---
# Page A

[B](/docs/b)
`
	writeMarkdown(t, sourcePath, sourceOriginal)
	writeMarkdown(t, filepath.Join(rootDir, "docs", "b.md"), `---
leafwiki_id: page-b
leafwiki_title: Page B
---
# Page B
`)

	writeErr := errors.New("canonical migration write failed")
	previousWriter := canonicalMarkdownRewriteWriter
	canonicalMarkdownRewriteWriter = func([]canonicalMarkdownRewrite) error {
		return writeErr
	}
	t.Cleanup(func() {
		canonicalMarkdownRewriteWriter = previousWriter
	})

	derivedRebuilds := 0
	service, err := NewService(ServiceOptions{
		Enabled: true,
		DataDir: dataDir,
		RootDir: rootDir,
		Tree:    treeService,
		Store:   &fakeRevisionStore{capture: &gitrevisions.Commit{Hash: "initial-commit"}},
		AfterSync: func() error {
			derivedRebuilds++
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

	if !errors.Is(err, writeErr) {
		t.Fatalf("SyncNow error = %v, want %v", err, writeErr)
	}
	if !strings.Contains(status.LastError, writeErr.Error()) {
		t.Fatalf("LastError = %q, want canonical write failure", status.LastError)
	}
	if got := readFileString(t, sourcePath); !strings.Contains(got, "[B](/docs/b)") || strings.Contains(got, "[B](/docs/b.md)") {
		t.Fatalf("source file = %q, want no canonical migration rewrite after write failure", got)
	}
	page, err := treeService.GetPage("page-a")
	if err != nil {
		t.Fatalf("GetPage page-a: %v", err)
	}
	if !strings.Contains(page.RawContent, "[B](/docs/b)") || strings.Contains(page.RawContent, "[B](/docs/b.md)") {
		t.Fatalf("tree raw content = %q, want no canonical migration rewrite after write failure", page.RawContent)
	}
	if derivedRebuilds != 0 {
		t.Fatalf("derived rebuilds = %d, want none after migration write failure", derivedRebuilds)
	}
}

// - Migration is idempotent
func TestServiceSyncNowCanonicalMigrationSecondRunCreatesNoNewRevision(t *testing.T) {
	dataDir := t.TempDir()
	rootDir := filepath.Join(t.TempDir(), "workspace")
	treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
	if err := treeService.LoadTree(); err != nil {
		t.Fatalf("LoadTree: %v", err)
	}
	writeMarkdown(t, filepath.Join(rootDir, "a.md"), `---
leafwiki_id: page-a
leafwiki_title: Page A
---
# Page A

[B](/b)
`)
	writeMarkdown(t, filepath.Join(rootDir, "b.md"), `---
leafwiki_id: page-b
leafwiki_title: Page B
---
# Page B
`)

	service, err := NewService(ServiceOptions{
		Enabled: true,
		DataDir: dataDir,
		RootDir: rootDir,
		Tree:    treeService,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	for i := 0; i < 2; i++ {
		if _, err := service.SyncNow(context.Background(), SyncRequest{
			Reason: ReasonExplicit,
			Source: SourceFilesystem,
			Actor:  PublicEditorActor(),
		}); err != nil {
			t.Fatalf("SyncNow %d: %v", i+1, err)
		}
	}

	snapshots, err := service.ListSnapshots(context.Background(), 10)
	if err != nil {
		t.Fatalf("ListSnapshots: %v", err)
	}
	if len(snapshots) != 2 {
		t.Fatalf("snapshot count = %d, want raw commit plus canonical migration writeback and no repeat revisions: %#v", len(snapshots), snapshots)
	}
}

// - Unresolved old extensionless page link becomes validation error
func TestServiceSyncNowLeavesUnresolvedLegacyPageLinkAndReportsValidationError(t *testing.T) {
	dataDir := t.TempDir()
	rootDir := filepath.Join(t.TempDir(), "workspace")
	treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
	if err := treeService.LoadTree(); err != nil {
		t.Fatalf("LoadTree: %v", err)
	}
	writeMarkdown(t, filepath.Join(rootDir, "docs", "a.md"), `---
leafwiki_id: page-a
leafwiki_title: Page A
---
# Page A

[Missing](/docs/missing)
`)

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
	raw, err := os.ReadFile(filepath.Join(rootDir, "docs", "a.md"))
	if err != nil {
		t.Fatalf("ReadFile a.md: %v", err)
	}
	if !strings.Contains(string(raw), "[Missing](/docs/missing)") {
		t.Fatalf("a.md = %q, want unresolved legacy link left unchanged", string(raw))
	}
	if len(status.ValidationErrors) != 1 {
		t.Fatalf("ValidationErrors = %#v, want one unresolved link error", status.ValidationErrors)
	}
	if status.ValidationErrors[0].Path != "docs/a" || !strings.Contains(status.ValidationErrors[0].Message, "/docs/missing") {
		t.Fatalf("ValidationErrors = %#v, want source path and missing link detail", status.ValidationErrors)
	}
}

// - Relative link cannot escape the workspace root
func TestServiceSyncNowLeavesInvalidCanonicalLinksUnchangedAndReportsValidation(t *testing.T) {
	dataDir := t.TempDir()
	workspaceParent := t.TempDir()
	rootDir := filepath.Join(workspaceParent, "workspace")
	treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
	if err := treeService.LoadTree(); err != nil {
		t.Fatalf("LoadTree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspaceParent, "outside.md"), []byte("---\nleafwiki_id: outside\nleafwiki_title: Outside\n---\n# Outside\n"), 0o644); err != nil {
		t.Fatalf("write outside markdown: %v", err)
	}
	sourceOriginal := `---
leafwiki_id: page-a
leafwiki_title: Page A
---
# Page A

[Bad Encoding](/docs/%zz)
[Escape](../../outside.md)
`
	writeMarkdown(t, filepath.Join(rootDir, "docs", "a.md"), sourceOriginal)

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
	got := readFileString(t, filepath.Join(rootDir, "docs", "a.md"))
	for _, originalLink := range []string{"[Bad Encoding](/docs/%zz)", "[Escape](../../outside.md)"} {
		if !strings.Contains(got, originalLink) {
			t.Fatalf("source file = %q, want invalid link %q left unchanged", got, originalLink)
		}
	}
	page, err := treeService.GetPage("page-a")
	if err != nil {
		t.Fatalf("GetPage page-a: %v", err)
	}
	for _, originalLink := range []string{"[Bad Encoding](/docs/%zz)", "[Escape](../../outside.md)"} {
		if !strings.Contains(page.RawContent, originalLink) {
			t.Fatalf("tree raw content = %q, want invalid link %q left unchanged", page.RawContent, originalLink)
		}
	}
	if len(status.ValidationErrors) != 2 {
		t.Fatalf("ValidationErrors = %#v, want two invalid link errors", status.ValidationErrors)
	}
	for _, validationError := range status.ValidationErrors {
		if validationError.Path != "docs/a" || validationError.Code != "invalid_link" {
			t.Fatalf("ValidationErrors = %#v, want invalid_link details for docs/a", status.ValidationErrors)
		}
	}
}

// - Ambiguous extensionless link is left as validation error
func TestServiceSyncNowReportsAmbiguousLegacyLinkWhenMigrationCannotRewrite(t *testing.T) {
	dataDir := t.TempDir()
	rootDir := filepath.Join(t.TempDir(), "workspace")
	treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
	if err := treeService.LoadTree(); err != nil {
		t.Fatalf("LoadTree: %v", err)
	}
	sourceOriginal := `---
leafwiki_id: page-a
leafwiki_title: Page A
---
# Page A

[Sync](/docs/sync)
`
	writeMarkdown(t, filepath.Join(rootDir, "docs", "a.md"), sourceOriginal)
	writeMarkdown(t, filepath.Join(rootDir, "docs", "sync.md"), `---
leafwiki_id: sync-page
leafwiki_title: Sync Page
---
# Sync Page
`)
	writeMarkdown(t, filepath.Join(rootDir, "docs", "sync", "index.md"), `---
leafwiki_id: sync-section
leafwiki_title: Sync Section
---
# Sync Section
`)

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
	if got := readFileString(t, filepath.Join(rootDir, "docs", "a.md")); !strings.Contains(got, "[Sync](/docs/sync)") {
		t.Fatalf("source file = %q, want ambiguous legacy link left unchanged", got)
	}
	page, err := treeService.GetPage("page-a")
	if err != nil {
		t.Fatalf("GetPage page-a: %v", err)
	}
	if !strings.Contains(page.RawContent, "[Sync](/docs/sync)") {
		t.Fatalf("tree raw content = %q, want ambiguous legacy link left unchanged", page.RawContent)
	}
	if len(status.ValidationErrors) == 0 {
		t.Fatalf("ValidationErrors empty, want ambiguous legacy migration issue")
	}
	if !strings.Contains(status.ValidationErrors[0].Message, "ambiguous_legacy_link") ||
		!strings.Contains(status.ValidationErrors[0].Message, "/docs/sync") {
		t.Fatalf("ValidationErrors = %#v, want ambiguous legacy link detail", status.ValidationErrors)
	}
}

func TestServiceSyncNowPreservesMigrationAmbiguityWhenNormalValidationAlsoFails(t *testing.T) {
	dataDir := t.TempDir()
	rootDir := filepath.Join(t.TempDir(), "workspace")
	treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
	if err := treeService.LoadTree(); err != nil {
		t.Fatalf("LoadTree: %v", err)
	}
	writeMarkdown(t, filepath.Join(rootDir, "docs", "a.md"), `---
leafwiki_id: page-a
leafwiki_title: Page A
---
# Page A

[Sync](/docs/sync)
[Missing](/docs/missing)
`)
	writeMarkdown(t, filepath.Join(rootDir, "docs", "sync.md"), `---
leafwiki_id: sync-page
leafwiki_title: Sync Page
---
# Sync Page
`)
	writeMarkdown(t, filepath.Join(rootDir, "docs", "sync", "index.md"), `---
leafwiki_id: sync-section
leafwiki_title: Sync Section
---
# Sync Section
`)

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

	assertValidationErrorContains := func(needle string) {
		t.Helper()
		for _, validationError := range status.ValidationErrors {
			if strings.Contains(validationError.Message, needle) {
				return
			}
		}
		t.Fatalf("ValidationErrors = %#v, want message containing %q", status.ValidationErrors, needle)
	}
	assertValidationErrorContains("ambiguous_legacy_link")
	assertValidationErrorContains("/docs/missing")
}

// - Old extensionless section link remains extensionless
func TestServiceSyncNowCanonicalizesSectionTrailingSlashWithoutRevisionLoop(t *testing.T) {
	dataDir := t.TempDir()
	rootDir := filepath.Join(t.TempDir(), "workspace")
	treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: dataDir, RootDir: rootDir})
	if err := treeService.LoadTree(); err != nil {
		t.Fatalf("LoadTree: %v", err)
	}
	writeMarkdown(t, filepath.Join(rootDir, "docs", "a.md"), `---
leafwiki_id: page-a
leafwiki_title: Page A
---
# Page A

[Sync](/docs/sync/)
`)
	writeMarkdown(t, filepath.Join(rootDir, "docs", "sync", "index.md"), `---
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
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	for i := 0; i < 2; i++ {
		if _, err := service.SyncNow(context.Background(), SyncRequest{
			Reason: ReasonExplicit,
			Source: SourceFilesystem,
			Actor:  PublicEditorActor(),
		}); err != nil {
			t.Fatalf("SyncNow %d: %v", i+1, err)
		}
	}

	raw, err := os.ReadFile(filepath.Join(rootDir, "docs", "a.md"))
	if err != nil {
		t.Fatalf("ReadFile a.md: %v", err)
	}
	if !strings.Contains(string(raw), "[Sync](/docs/sync)") || strings.Contains(string(raw), "/docs/sync/") {
		t.Fatalf("a.md = %q, want section trailing slash canonicalized away", string(raw))
	}
	snapshots, err := service.ListSnapshots(context.Background(), 10)
	if err != nil {
		t.Fatalf("ListSnapshots: %v", err)
	}
	if len(snapshots) != 2 {
		t.Fatalf("snapshot count = %d, want raw sync plus canonical writeback with no repeat canonicalization revision: %#v", len(snapshots), snapshots)
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

func TestServiceListSnapshotPagePropagatesChangedMarkdownPathErrors(t *testing.T) {
	service, err := NewService(ServiceOptions{
		Enabled: true,
		Tree:    &fakeTreeReconstructor{},
		Store: &fakeRevisionStore{
			commits: []gitrevisions.Commit{
				{Hash: "abc123", ChangedMarkdownCount: 1},
			},
			changedPathsErr: errors.New("path trailer read failed"),
		},
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	_, err = service.ListSnapshotPage(context.Background(), "", 10)
	if err == nil || !strings.Contains(err.Error(), "path trailer read failed") {
		t.Fatalf("ListSnapshotPage error = %v, want changed path error", err)
	}
}

func TestServiceListSnapshotPageDoesNotReadChangedPathsForSentinelCommit(t *testing.T) {
	service, err := NewService(ServiceOptions{
		Enabled: true,
		Tree:    &fakeTreeReconstructor{},
		Store: &fakeRevisionStore{
			commits: []gitrevisions.Commit{
				{Hash: "returned", ChangedMarkdownCount: 1},
				{Hash: "sentinel", ChangedMarkdownCount: 1},
			},
			changedPaths: map[string][]string{
				"returned": {"returned.md"},
			},
			changedPathsErrByHash: map[string]error{
				"sentinel": errors.New("sentinel diff should not be read"),
			},
		},
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	page, err := service.ListSnapshotPage(context.Background(), "", 1)
	if err != nil {
		t.Fatalf("ListSnapshotPage returned sentinel error: %v", err)
	}
	if len(page.Snapshots) != 1 || page.Snapshots[0].ID != "returned" {
		t.Fatalf("snapshots = %#v, want only returned snapshot", page.Snapshots)
	}
	if page.NextCursor != "returned" {
		t.Fatalf("NextCursor = %q, want returned", page.NextCursor)
	}
}

func TestServiceListSnapshotPageDoesNotBlockStatusThroughSyncNowWhileReadingChangedPaths(t *testing.T) {
	store := &fakeRevisionStore{
		capture: &gitrevisions.Commit{Hash: "sync-commit"},
		commits: []gitrevisions.Commit{
			{Hash: "slow-snapshot", ChangedMarkdownCount: 1},
		},
		changedPaths: map[string][]string{
			"slow-snapshot": {"slow.md"},
		},
		changedPathsStarted: make(chan struct{}),
		unblockChangedPaths: make(chan struct{}),
	}
	service, err := NewService(ServiceOptions{
		Enabled: true,
		Tree:    &fakeTreeReconstructor{},
		Store:   store,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	listDone := make(chan error, 1)
	go func() {
		_, err := service.ListSnapshotPage(context.Background(), "", 1)
		listDone <- err
	}()
	<-store.changedPathsStarted

	syncDone := make(chan error, 1)
	go func() {
		_, err := service.SyncNow(context.Background(), SyncRequest{
			Reason: ReasonExplicit,
			Source: SourceFilesystem,
			Actor:  PublicEditorActor(),
		})
		syncDone <- err
	}()

	time.Sleep(20 * time.Millisecond)
	statusDone := make(chan SyncStatus, 1)
	go func() {
		statusDone <- service.Status()
	}()

	select {
	case status := <-statusDone:
		if !status.Enabled {
			close(store.unblockChangedPaths)
			<-listDone
			<-syncDone
			t.Fatalf("status.Enabled = false, want true")
		}
	case <-time.After(200 * time.Millisecond):
		close(store.unblockChangedPaths)
		<-listDone
		<-syncDone
		t.Fatalf("Status blocked behind SyncNow waiting for ListSnapshotPage changed-path read")
	}

	close(store.unblockChangedPaths)
	if err := <-listDone; err != nil {
		t.Fatalf("ListSnapshotPage: %v", err)
	}
	if err := <-syncDone; err != nil {
		t.Fatalf("SyncNow: %v", err)
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

func TestServiceListPageRevisionsNormalizesReadmeFallbackSectionPath(t *testing.T) {
	page := &tree.Page{PageNode: &tree.PageNode{
		ID:    "section-guides",
		Title: "Guides",
		Slug:  "guides",
		Kind:  tree.NodeKindSection,
	}}
	store := &fakeRevisionStore{
		commits: []gitrevisions.Commit{{Hash: "readme-section-commit", AuthorID: "alice"}},
		filesAt: map[string]map[string]string{
			"readme-section-commit": {
				"guides/README.md": "---\nleafwiki_id: section-guides\nleafwiki_title: Historical Guides\n---\n# Historical Guides\n",
			},
		},
		changedPaths: map[string][]string{
			"readme-section-commit": {"guides/README.md"},
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
	if revisions[0].Title != "Historical Guides" {
		t.Fatalf("revision title = %q, want Historical Guides", revisions[0].Title)
	}
	if revisions[0].Slug != "guides" {
		t.Fatalf("revision slug = %q, want guides", revisions[0].Slug)
	}
	if revisions[0].Kind != string(tree.NodeKindSection) {
		t.Fatalf("revision kind = %q, want section", revisions[0].Kind)
	}
	if revisions[0].Path != "guides" {
		t.Fatalf("revision path = %q, want guides", revisions[0].Path)
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

func TestServiceListPageRevisionsDoesNotBlockStatusWhileScanningStore(t *testing.T) {
	page := &tree.Page{PageNode: &tree.PageNode{
		ID:    "page-a",
		Title: "Page A",
		Slug:  "page-a",
		Kind:  tree.NodeKindPage,
	}}
	store := &fakeRevisionStore{
		commits: []gitrevisions.Commit{{Hash: "page-a-change", AuthorID: "alice"}},
		filesAt: map[string]map[string]string{
			"page-a-change": {
				"page-a.md": "---\nleafwiki_id: page-a\nleafwiki_title: Page A\n---\n# Page A changed\n",
			},
		},
		changedPaths: map[string][]string{
			"page-a-change": {"page-a.md"},
		},
		changedContentsStarted: make(chan struct{}),
		unblockChangedContents: make(chan struct{}),
	}
	service, err := NewService(ServiceOptions{
		Enabled: true,
		Tree:    &fakeTreeReconstructor{},
		Store:   store,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := service.ListPageRevisions(context.Background(), page, "", 1)
		done <- err
	}()
	<-store.changedContentsStarted

	statusDone := make(chan SyncStatus, 1)
	go func() {
		statusDone <- service.Status()
	}()

	select {
	case status := <-statusDone:
		if !status.Enabled {
			t.Fatalf("status.Enabled = false, want true")
		}
	case <-time.After(200 * time.Millisecond):
		close(store.unblockChangedContents)
		<-done
		t.Fatalf("Status blocked behind ListPageRevisions store scan")
	}
	close(store.unblockChangedContents)
	if err := <-done; err != nil {
		t.Fatalf("ListPageRevisions: %v", err)
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

func TestServiceGetPageRevisionSnapshotDoesNotBlockStatusWhileReadingStore(t *testing.T) {
	page := &tree.Page{PageNode: &tree.PageNode{
		ID:    "page-a",
		Title: "Page A",
		Slug:  "page-a",
		Kind:  tree.NodeKindPage,
	}}
	store := &fakeRevisionStore{
		commits: []gitrevisions.Commit{{Hash: "page-a-change", AuthorID: "alice"}},
		filesAt: map[string]map[string]string{
			"page-a-change": {
				"page-a.md": "---\nleafwiki_id: page-a\nleafwiki_title: Page A\n---\n# Page A changed\n",
			},
		},
		changedPaths: map[string][]string{
			"page-a-change": {"page-a.md"},
		},
		changedContentsStarted: make(chan struct{}),
		unblockChangedContents: make(chan struct{}),
	}
	service, err := NewService(ServiceOptions{
		Enabled: true,
		Tree:    &fakeTreeReconstructor{},
		Store:   store,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := service.GetPageRevisionSnapshot(context.Background(), page, "page-a-change")
		done <- err
	}()
	<-store.changedContentsStarted

	statusDone := make(chan SyncStatus, 1)
	go func() {
		statusDone <- service.Status()
	}()

	select {
	case status := <-statusDone:
		if !status.Enabled {
			t.Fatalf("status.Enabled = false, want true")
		}
	case <-time.After(200 * time.Millisecond):
		close(store.unblockChangedContents)
		<-done
		t.Fatalf("Status blocked behind GetPageRevisionSnapshot store read")
	}
	close(store.unblockChangedContents)
	if err := <-done; err != nil {
		t.Fatalf("GetPageRevisionSnapshot: %v", err)
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

func TestServiceRestoreDocumentRestoresReadmeFallbackSectionToReadmePath(t *testing.T) {
	dataDir := t.TempDir()
	rootDir := filepath.Join(t.TempDir(), "workspace")
	store, err := gitrevisions.Open(gitrevisions.StoreOptions{DataDir: dataDir, RootDir: rootDir})
	if err != nil {
		t.Fatalf("Open store: %v", err)
	}
	writeMarkdown(t, filepath.Join(rootDir, "docs", "README.md"), "---\nleafwiki_id: section-1\nleafwiki_title: Docs\n---\n# Docs\n\nold readme section content")
	oldCommit, err := store.Capture(context.Background(), gitrevisions.CommitRequest{
		Reason: ReasonExplicit,
		Source: SourceFilesystem,
		Actor:  PublicEditorActor(),
	})
	if err != nil {
		t.Fatalf("capture old commit: %v", err)
	}
	writeMarkdown(t, filepath.Join(rootDir, "docs", "README.md"), "---\nleafwiki_id: section-1\nleafwiki_title: Docs\n---\n# Docs\n\nnew readme section content")
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
		RootDir: rootDir,
		Tree:    &fakeTreeReconstructor{},
		Store:   store,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	if _, err := service.RestoreDocument(context.Background(), section, oldCommit.Hash, PublicEditorActor()); err != nil {
		t.Fatalf("RestoreDocument from README section commit: %v", err)
	}

	readmeBytes, err := os.ReadFile(filepath.Join(rootDir, "docs", "README.md"))
	if err != nil {
		t.Fatalf("read restored README section: %v", err)
	}
	if !strings.Contains(string(readmeBytes), "old readme section content") {
		t.Fatalf("README.md content = %q, want old readme section content", string(readmeBytes))
	}
	if _, err := os.Stat(filepath.Join(rootDir, "docs", "index.md")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("index.md exists after README section restore, want no new index; err=%v", err)
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

func TestServiceStopWatcherClosesUnderlyingWatcher(t *testing.T) {
	fakeWatcher := newFakeWatcher()
	service, err := NewService(ServiceOptions{
		Enabled: true,
		RootDir: "/workspace",
		Tree:    &fakeTreeReconstructor{},
		Store:   &fakeRevisionStore{capture: &gitrevisions.Commit{Hash: "stop-commit"}},
		WatcherFactory: func(string) (fileWatcher, error) {
			return fakeWatcher, nil
		},
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	if err := service.StartWatcher(context.Background()); err != nil {
		t.Fatalf("StartWatcher: %v", err)
	}

	service.StopWatcher()

	if got := fakeWatcher.closeCount(); got != 1 {
		t.Fatalf("watcher Close calls = %d, want 1", got)
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

func readFileString(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(raw)
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
	captureErrCall                    int
	amendErr                          error
	captureCalls                      int
	commits                           []gitrevisions.Commit
	filesAt                           map[string]map[string]string
	changedPaths                      map[string][]string
	changedPathsErr                   error
	changedPathsErrByHash             map[string]error
	scannedCommits                    int
	filesAtCalls                      int
	restoreDocumentToPathCalls        int
	restoreDocumentContentToPathCalls int
	restoredContent                   string
	changedPathsStarted               chan struct{}
	unblockChangedPaths               chan struct{}
	changedContentsStarted            chan struct{}
	unblockChangedContents            chan struct{}
}

func (f *fakeRevisionStore) Capture(context.Context, gitrevisions.CommitRequest) (*gitrevisions.Commit, error) {
	f.captureCalls++
	if f.captureErr != nil && (f.captureErrCall == 0 || f.captureCalls == f.captureErrCall) {
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
	if f.amendErr != nil {
		return nil, f.amendErr
	}
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
	if f.changedPathsStarted != nil {
		close(f.changedPathsStarted)
		f.changedPathsStarted = nil
	}
	if f.unblockChangedPaths != nil {
		<-f.unblockChangedPaths
	}
	if f.changedPathsErr != nil {
		return nil, f.changedPathsErr
	}
	if f.changedPathsErrByHash != nil {
		if err := f.changedPathsErrByHash[hash]; err != nil {
			return nil, err
		}
	}
	return f.changedPaths[hash], nil
}

func (f *fakeRevisionStore) ChangedMarkdownContents(_ context.Context, hash string) (map[string]string, error) {
	if f.changedContentsStarted != nil {
		close(f.changedContentsStarted)
		f.changedContentsStarted = nil
	}
	if f.unblockChangedContents != nil {
		<-f.unblockChangedContents
	}
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
	closes   int32
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

func (f *fakeWatcher) Close() {
	atomic.AddInt32(&f.closes, 1)
}

func (f *fakeWatcher) closeCount() int {
	return int(atomic.LoadInt32(&f.closes))
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
