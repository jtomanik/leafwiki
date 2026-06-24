package tree

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/perber/wiki/internal/core/markdown"
)

// Canonical Markdown links plan scenarios covered by tests in this file:
// - index.md has precedence over README.md
// - README.md is fallback section default
// - root README.md is fallback only without root index.md
// - root index.md has precedence over root README.md
// - Uppercase INDEX.MD does not become a second child page when accepted by current index lookup rules

func findChildBySlug(t *testing.T, parent *PageNode, slug string) *PageNode {
	t.Helper()
	for _, ch := range parent.Children {
		if ch.Slug == newFixtureSlug(slug) {
			return ch
		}
	}
	t.Fatalf("child with slug %q not found under %q", slug, parent.Slug)
	return nil
}

func slugs(children []*PageNode) []string {
	out := make([]string, 0, len(children))
	for _, c := range children {
		out = append(out, c.Slug.String())
	}
	return out
}

// --- tests ---

func TestNodeStore_ReconstructTreeFromFS_EmptyStorage_ReturnsRoot(t *testing.T) {
	tmp := t.TempDir()
	store := NewNodeStore(tmp)

	tree, err := store.ReconstructTreeFromFS()
	if err != nil {
		t.Fatalf("ReconstructTreeFromFS: %v", err)
	}

	if tree == nil || tree.ID != "root" || tree.Kind != NodeKindSection {
		t.Fatalf("unexpected root: %#v", tree)
	}
	if tree.Parent != nil {
		t.Fatalf("expected root parent nil")
	}
	if len(tree.Children) != 0 {
		t.Fatalf("expected root to have no children, got %d", len(tree.Children))
	}
}

func TestNodeStore_ReconstructTreeFromFS_BuildsSectionsAndPages_SkipsIndexMdAsPage(t *testing.T) {
	tmp := t.TempDir()
	store := NewNodeStore(tmp)

	// FS layout:
	// <tmp>/docs/index.md (section content)
	// <tmp>/docs/intro.md (page)
	// <tmp>/readme.md (page at root)
	mustMkdir(t, filepath.Join(tmp, "root", "docs"))

	secIndex := `---
leafwiki_id: sec-docs
leafwiki_title: Documentation
---
# Section`
	mustWriteFile(t, filepath.Join(tmp, "root", "docs", "index.md"), secIndex, 0o644)

	pageIntro := `---
leafwiki_id: page-intro
leafwiki_title: Introduction
---
# Intro`
	mustWriteFile(t, filepath.Join(tmp, "root", "docs", "intro.md"), pageIntro, 0o644)

	rootPage := `---
leafwiki_id: page-readme
leafwiki_title: Readme
---
# Readme`
	mustWriteFile(t, filepath.Join(tmp, "root", "readme.md"), rootPage, 0o644)

	tree, err := store.ReconstructTreeFromFS()
	if err != nil {
		t.Fatalf("ReconstructTreeFromFS: %v", err)
	}

	// root has: docs(section), readme(page)
	docs := findChildBySlug(t, tree, "docs")
	if docs.Kind != NodeKindSection {
		t.Fatalf("expected docs to be section, got %q", docs.Kind)
	}
	// section title/id from index frontmatter
	if docs.ID != "sec-docs" {
		t.Fatalf("expected docs.ID=sec-docs, got %q", docs.ID)
	}
	if docs.Title != "Documentation" {
		t.Fatalf("expected docs.Title=Documentation, got %q", docs.Title)
	}

	// ensure index.md wasn't turned into a page child
	for _, ch := range docs.Children {
		if ch.Slug == "index" {
			t.Fatalf("index.md must be skipped as page, but found slug index")
		}
	}

	intro := findChildBySlug(t, docs, "intro")
	if intro.Kind != NodeKindPage {
		t.Fatalf("expected intro to be page, got %q", intro.Kind)
	}
	// page title/id from frontmatter
	if intro.ID != "page-intro" {
		t.Fatalf("expected intro.ID=page-intro, got %q", intro.ID)
	}
	if intro.Title != "Introduction" {
		t.Fatalf("expected intro.Title=Introduction, got %q", intro.Title)
	}

	readme := findChildBySlug(t, tree, "readme")
	if readme.Kind != NodeKindPage {
		t.Fatalf("expected readme to be page, got %q", readme.Kind)
	}
	if readme.ID != "page-readme" {
		t.Fatalf("expected readme.ID=page-readme, got %q", readme.ID)
	}
	if readme.Title != "Readme" {
		t.Fatalf("expected readme.Title=Readme, got %q", readme.Title)
	}

	// parent pointers
	if docs.Parent == nil || docs.Parent.ID != "root" {
		t.Fatalf("expected docs parent root, got %#v", docs.Parent)
	}
	if intro.Parent == nil || intro.Parent.ID != docs.ID {
		t.Fatalf("expected intro parent docs, got %#v", intro.Parent)
	}
}

// - Uppercase INDEX.MD does not become a second child page when accepted by current index lookup rules
func TestNodeStore_ReconstructTreeFromFS_UsesUppercaseSectionIndex(t *testing.T) {
	tmp := t.TempDir()
	store := NewNodeStore(tmp)

	sectionDir := filepath.Join(tmp, "root", "docs")
	mustMkdir(t, sectionDir)
	indexPath := filepath.Join(sectionDir, "INDEX.MD")
	mustWriteFile(t, indexPath, `---
leafwiki_id: sec-docs
leafwiki_title: Documentation
---
# Section
`, 0o644)
	mustWriteFile(t, filepath.Join(sectionDir, "intro.md"), `---
leafwiki_id: page-intro
leafwiki_title: Introduction
---
# Intro
`, 0o644)

	resolvedIndexPath, hasIndex, err := store.sectionIndexPathInDir(sectionDir)
	if err != nil {
		t.Fatalf("sectionIndexPathInDir: %v", err)
	}
	if !hasIndex {
		t.Fatalf("sectionIndexPathInDir did not find INDEX.MD")
	}
	if filepath.Base(resolvedIndexPath) != "INDEX.MD" {
		t.Fatalf("sectionIndexPathInDir path = %q, want INDEX.MD", resolvedIndexPath)
	}

	tree, err := store.ReconstructTreeFromFS()
	if err != nil {
		t.Fatalf("ReconstructTreeFromFS: %v", err)
	}

	docs := findChildBySlug(t, tree, "docs")
	if docs.Kind != NodeKindSection {
		t.Fatalf("expected docs to be section, got %q", docs.Kind)
	}
	if docs.ID != "sec-docs" {
		t.Fatalf("expected docs.ID=sec-docs, got %q", docs.ID)
	}
	if docs.Title != "Documentation" {
		t.Fatalf("expected docs.Title=Documentation, got %q", docs.Title)
	}
	for _, ch := range docs.Children {
		if strings.EqualFold(ch.Slug.String(), "index") {
			t.Fatalf("INDEX.MD must be skipped as page, but found slug %q", ch.Slug)
		}
	}

	raw, err := store.ReadPageRaw(docs)
	if err != nil {
		t.Fatalf("ReadPageRaw section: %v", err)
	}
	if !strings.Contains(raw, "# Section") {
		t.Fatalf("section raw content = %q, want INDEX.MD body", raw)
	}

	entries, err := os.ReadDir(sectionDir)
	if err != nil {
		t.Fatalf("ReadDir section: %v", err)
	}
	for _, entry := range entries {
		if entry.Name() == "index.md" {
			t.Fatalf("reconstruct materialized lowercase index.md alongside INDEX.MD")
		}
	}

	mdFile, err := markdown.LoadMarkdownFile(indexPath)
	if err != nil {
		t.Fatalf("LoadMarkdownFile INDEX.MD: %v", err)
	}
	fm := mdFile.GetFrontmatter()
	if fm.LeafWikiID != "sec-docs" || fm.LeafWikiTitle != "Documentation" {
		t.Fatalf("unexpected frontmatter after writeback: %#v", fm)
	}
	if fm.LeafWikiCreatedAt == "" || fm.LeafWikiUpdatedAt == "" {
		t.Fatalf("expected metadata writeback to update INDEX.MD timestamps, got %#v", fm)
	}
}

// - README.md is fallback section default
func TestNodeStore_ReconstructTreeFromFS_ReadmeFallbackSectionWhenNoIndexExists(t *testing.T) {
	tmp := t.TempDir()
	store := NewNodeStore(tmp)

	sectionDir := filepath.Join(tmp, "root", "docs")
	mustMkdir(t, sectionDir)
	readmePath := filepath.Join(sectionDir, "README.md")
	mustWriteFile(t, readmePath, `---
leafwiki_id: sec-docs
leafwiki_title: Documentation
---
# Section readme
`, 0o644)

	tree, err := store.ReconstructTreeFromFS()
	if err != nil {
		t.Fatalf("ReconstructTreeFromFS: %v", err)
	}

	docs := findChildBySlug(t, tree, "docs")
	if docs.Kind != NodeKindSection {
		t.Fatalf("expected docs to be section, got %q", docs.Kind)
	}
	if docs.ID != "sec-docs" {
		t.Fatalf("expected docs.ID from README.md frontmatter, got %q", docs.ID)
	}
	if docs.Title != "Documentation" {
		t.Fatalf("expected docs.Title from README.md frontmatter, got %q", docs.Title)
	}
	for _, ch := range docs.Children {
		if strings.EqualFold(ch.Slug.String(), "readme") {
			t.Fatalf("README.md fallback must not be reconstructed as a child page")
		}
	}

	raw, err := store.ReadPageRaw(docs)
	if err != nil {
		t.Fatalf("ReadPageRaw section: %v", err)
	}
	if !strings.Contains(raw, "# Section readme") {
		t.Fatalf("section raw content = %q, want README.md body", raw)
	}
	if _, err := os.Stat(filepath.Join(sectionDir, "index.md")); !os.IsNotExist(err) {
		t.Fatalf("README.md fallback must not materialize index.md, stat err = %v", err)
	}
}

// - index.md has precedence over README.md
func TestNodeStore_ReconstructTreeFromFS_IndexBeatsReadmeAndReadmeIsSeparatePage(t *testing.T) {
	tmp := t.TempDir()
	store := NewNodeStore(tmp)

	sectionDir := filepath.Join(tmp, "root", "docs")
	mustMkdir(t, sectionDir)
	mustWriteFile(t, filepath.Join(sectionDir, "index.md"), `---
leafwiki_id: sec-docs
leafwiki_title: Documentation
---
# Index section
`, 0o644)
	mustWriteFile(t, filepath.Join(sectionDir, "README.md"), `---
leafwiki_id: page-readme
leafwiki_title: Readme Page
---
# Readme page
`, 0o644)

	tree, err := store.ReconstructTreeFromFS()
	if err != nil {
		t.Fatalf("ReconstructTreeFromFS: %v", err)
	}

	docs := findChildBySlug(t, tree, "docs")
	if docs.Kind != NodeKindSection || docs.ID != "sec-docs" {
		t.Fatalf("docs = %#v, want section from index.md", docs)
	}
	raw, err := store.ReadPageRaw(docs)
	if err != nil {
		t.Fatalf("ReadPageRaw docs: %v", err)
	}
	if !strings.Contains(raw, "# Index section") {
		t.Fatalf("docs raw = %q, want index.md content", raw)
	}
	readme := findChildBySlug(t, docs, "README")
	if readme.Kind != NodeKindPage || readme.ID != "page-readme" {
		t.Fatalf("README child = %#v, want separate page from README.md", readme)
	}
}

func TestNodeStore_ReconstructTreeFromFS_AllowsPageAndSectionWithSameBasename(t *testing.T) {
	tmp := t.TempDir()
	store := NewNodeStore(tmp)

	docsDir := filepath.Join(tmp, "root", "docs")
	mustMkdir(t, filepath.Join(docsDir, "sync"))
	mustWriteFile(t, filepath.Join(docsDir, "index.md"), `---
leafwiki_id: sec-docs
leafwiki_title: Documentation
---
# Documentation
`, 0o644)
	mustWriteFile(t, filepath.Join(docsDir, "sync.md"), `---
leafwiki_id: page-sync
leafwiki_title: Sync Page
---
# Sync Page
`, 0o644)
	mustWriteFile(t, filepath.Join(docsDir, "sync", "index.md"), `---
leafwiki_id: sec-sync
leafwiki_title: Sync Section
---
# Sync Section
`, 0o644)

	tree, err := store.ReconstructTreeFromFS()
	if err != nil {
		t.Fatalf("ReconstructTreeFromFS: %v", err)
	}

	docs := findChildBySlug(t, tree, "docs")
	var syncPage, syncSection *PageNode
	for _, ch := range docs.Children {
		if ch.Slug == "sync" && ch.Kind == NodeKindPage {
			syncPage = ch
		}
		if ch.Slug == "sync" && ch.Kind == NodeKindSection {
			syncSection = ch
		}
	}
	if syncPage == nil {
		t.Fatalf("docs children = %#v, want sync.md page child", docs.Children)
	}
	if syncSection == nil {
		t.Fatalf("docs children = %#v, want sync/ section child", docs.Children)
	}
	if syncPage.ID != "page-sync" {
		t.Fatalf("sync page ID = %q, want page-sync", syncPage.ID)
	}
	if syncSection.ID != "sec-sync" {
		t.Fatalf("sync section ID = %q, want sec-sync", syncSection.ID)
	}
}

// - root README.md is fallback only without root index.md
func TestNodeStore_ReconstructTreeFromFS_RootReadmeFallbackSectionWhenNoIndexExists(t *testing.T) {
	tmp := t.TempDir()
	store := NewNodeStore(tmp)

	mustMkdir(t, filepath.Join(tmp, "root"))
	mustWriteFile(t, filepath.Join(tmp, "root", "README.md"), `---
leafwiki_id: root
leafwiki_title: Root Readme
---
# Root readme
`, 0o644)

	tree, err := store.ReconstructTreeFromFS()
	if err != nil {
		t.Fatalf("ReconstructTreeFromFS: %v", err)
	}

	if tree.ID != "root" || tree.Title != "Root Readme" {
		t.Fatalf("root = %#v, want stable root ID with README title", tree)
	}
	if len(tree.Children) != 0 {
		t.Fatalf("root children = %v, want README.md used as root content only", slugs(tree.Children))
	}
	raw, err := store.ReadPageRaw(tree)
	if err != nil {
		t.Fatalf("ReadPageRaw root: %v", err)
	}
	if !strings.Contains(raw, "# Root readme") {
		t.Fatalf("root raw = %q, want README.md body", raw)
	}
}

// - root index.md has precedence over root README.md
func TestNodeStore_ReconstructTreeFromFS_RootIndexBeatsRootReadme(t *testing.T) {
	tmp := t.TempDir()
	store := NewNodeStore(tmp)

	mustMkdir(t, filepath.Join(tmp, "root"))
	mustWriteFile(t, filepath.Join(tmp, "root", "index.md"), `---
leafwiki_id: root
leafwiki_title: Root Index
---
# Root index
`, 0o644)
	mustWriteFile(t, filepath.Join(tmp, "root", "README.md"), `---
leafwiki_id: root-readme
leafwiki_title: Root Readme Page
---
# Root readme page
`, 0o644)

	tree, err := store.ReconstructTreeFromFS()
	if err != nil {
		t.Fatalf("ReconstructTreeFromFS: %v", err)
	}

	if tree.ID != "root" || tree.Title != "Root Index" {
		t.Fatalf("root = %#v, want stable root ID with index title", tree)
	}
	raw, err := store.ReadPageRaw(tree)
	if err != nil {
		t.Fatalf("ReadPageRaw root: %v", err)
	}
	if !strings.Contains(raw, "# Root index") {
		t.Fatalf("root raw = %q, want index.md body", raw)
	}
	readme := findChildBySlug(t, tree, "README")
	if readme.Kind != NodeKindPage || readme.ID != "root-readme" {
		t.Fatalf("README child = %#v, want root README.md as separate page", readme)
	}
}

func TestNodeStore_ReconstructTreeFromFS_SectionWithoutIndex_UsesDirNameAsTitleAndMaterializesIndex(t *testing.T) {
	tmp := t.TempDir()
	store := NewNodeStore(tmp)

	mustMkdir(t, filepath.Join(tmp, "root", "emptysec"))

	tree, err := store.ReconstructTreeFromFS()
	if err != nil {
		t.Fatalf("ReconstructTreeFromFS: %v", err)
	}

	sec := findChildBySlug(t, tree, "emptysec")
	if sec.Kind != NodeKindSection {
		t.Fatalf("expected section, got %q", sec.Kind)
	}
	if sec.Title != "emptysec" {
		t.Fatalf("expected title=emptysec, got %q", sec.Title)
	}
	if strings.TrimSpace(sec.ID.String()) == "" {
		t.Fatalf("expected some generated id, got empty")
	}

	indexPath := filepath.Join(tmp, "root", "emptysec", "index.md")
	raw, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("expected reconstruct to materialize missing index.md: %v", err)
	}
	fm, body, has, err := markdown.ParseFrontmatter(string(raw))
	if err != nil {
		t.Fatalf("ParseFrontmatter: %v", err)
	}
	if !has {
		t.Fatalf("expected frontmatter in materialized index")
	}
	if newFixturePageID(fm.LeafWikiID) != sec.ID || fm.LeafWikiTitle != sec.Title {
		t.Fatalf("unexpected frontmatter in materialized index: %#v", fm)
	}
	if strings.TrimSpace(body) != "" {
		t.Fatalf("expected empty body in materialized index, got %q", body)
	}
}

func TestNodeStore_ReconstructTreeFromFS_PageWithoutFrontmatter_FallsBackToHeadlineTitle(t *testing.T) {
	tmp := t.TempDir()
	store := NewNodeStore(tmp)

	// FS: <tmp>/plain.md (no fm)
	mustWriteFile(t, filepath.Join(tmp, "root", "plain.md"), "# hello\n", 0o644)

	tree, err := store.ReconstructTreeFromFS()
	if err != nil {
		t.Fatalf("ReconstructTreeFromFS: %v", err)
	}

	p := findChildBySlug(t, tree, "plain")
	if p.Kind != NodeKindPage {
		t.Fatalf("expected page, got %q", p.Kind)
	}

	// title fallback should be headline
	if p.Title != "hello" {
		t.Fatalf("expected title fallback to slug 'plain', got %q", p.Title)
	}
	if strings.TrimSpace(p.ID.String()) == "" {
		// should still have generated id (unless you later decide to keep empty)
		t.Fatalf("expected generated id, got empty")
	}
}

func TestNodeStore_ReconstructTreeFromFS_PositionsAreContiguous(t *testing.T) {
	tmp := t.TempDir()
	store := NewNodeStore(tmp)

	// Create several files/dirs
	mustWriteFile(t, filepath.Join(tmp, "root", "b.md"), "# b", 0o644)
	mustWriteFile(t, filepath.Join(tmp, "root", "a.md"), "# a", 0o644)
	mustMkdir(t, filepath.Join(tmp, "root", "zsec"))

	tree, err := store.ReconstructTreeFromFS()
	if err != nil {
		t.Fatalf("ReconstructTreeFromFS: %v", err)
	}

	// Positions should be 0..n-1 regardless of order
	seen := make([]int, 0, len(tree.Children))
	for _, ch := range tree.Children {
		seen = append(seen, ch.Position)
	}
	sort.Ints(seen)
	for i := range seen {
		if seen[i] != i {
			t.Fatalf("expected contiguous positions 0..%d, got %v (slugs=%v)", len(seen)-1, seen, slugs(tree.Children))
		}
	}
}

func TestNodeStore_ReconstructTreeFromFS_OrderFileOverridesDefaultOrder(t *testing.T) {
	tmp := t.TempDir()
	store := NewNodeStore(tmp)

	mustWriteFile(t, filepath.Join(tmp, "root", "a.md"), "---\nleafwiki_id: id-a\n---\n# A", 0o644)
	mustWriteFile(t, filepath.Join(tmp, "root", "b.md"), "---\nleafwiki_id: id-b\n---\n# B", 0o644)
	mustWriteFile(t, filepath.Join(tmp, "root", "c.md"), "---\nleafwiki_id: id-c\n---\n# C", 0o644)

	orderRaw, err := json.Marshal(map[string][]string{
		"ordered_ids": {"id-c", "id-a"},
	})
	if err != nil {
		t.Fatalf("marshal order file: %v", err)
	}
	mustWriteFile(t, filepath.Join(tmp, "root", ".order.json"), string(orderRaw), 0o644)

	tree, err := store.ReconstructTreeFromFS()
	if err != nil {
		t.Fatalf("ReconstructTreeFromFS: %v", err)
	}

	got := slugs(tree.Children)
	want := []string{"c", "a", "b"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("unexpected child order: got %v want %v", got, want)
	}

	for i, child := range tree.Children {
		if child.Position != i {
			t.Fatalf("expected child %q position %d, got %d", child.Slug, i, child.Position)
		}
	}
}

func TestNodeStore_ReconstructTreeFromFS_OrderFileIgnoresUnknownIDsAndKeepsRemainingStable(t *testing.T) {
	tmp := t.TempDir()
	store := NewNodeStore(tmp)

	mustWriteFile(t, filepath.Join(tmp, "root", "a.md"), "---\nleafwiki_id: id-a\n---\n# A", 0o644)
	mustWriteFile(t, filepath.Join(tmp, "root", "b.md"), "---\nleafwiki_id: id-b\n---\n# B", 0o644)
	mustWriteFile(t, filepath.Join(tmp, "root", "c.md"), "---\nleafwiki_id: id-c\n---\n# C", 0o644)

	orderRaw, err := json.Marshal(map[string][]string{
		"ordered_ids": {"missing-id", "id-b"},
	})
	if err != nil {
		t.Fatalf("marshal order file: %v", err)
	}
	mustWriteFile(t, filepath.Join(tmp, "root", ".order.json"), string(orderRaw), 0o644)

	tree, err := store.ReconstructTreeFromFS()
	if err != nil {
		t.Fatalf("ReconstructTreeFromFS: %v", err)
	}

	got := slugs(tree.Children)
	want := []string{"b", "a", "c"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("unexpected child order: got %v want %v", got, want)
	}
}

func TestNodeStore_ReconstructTreeFromFS_ReturnsErrorOnDuplicateLeafWikiIDs(t *testing.T) {
	tmp := t.TempDir()
	store := NewNodeStore(tmp)

	mustWriteFile(t, filepath.Join(tmp, "root", "a.md"), `---
leafwiki_id: dup-id
leafwiki_title: A
---
# A`, 0o644)
	mustWriteFile(t, filepath.Join(tmp, "root", "b.md"), `---
leafwiki_id: dup-id
leafwiki_title: B
---
# B`, 0o644)

	_, err := store.ReconstructTreeFromFS()
	if err == nil {
		t.Fatalf("expected duplicate ID error")
	}
	if !strings.Contains(err.Error(), "duplicate leafwiki_id") {
		t.Fatalf("expected duplicate ID error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "dup-id") {
		t.Fatalf("expected duplicate ID to be mentioned, got: %v", err)
	}
}

func TestNodeStore_ReconstructTreeFromFS_ReturnsErrorOnDuplicateCanonicalAndLegacyLeafWikiIDs(t *testing.T) {
	tmp := t.TempDir()
	store := NewNodeStore(tmp)

	mustWriteFile(t, filepath.Join(tmp, "root", "a.md"), `<!-- leafwiki
version: 1
page:
  id: mixed-dup-id
  title: A
-->

# A`, 0o644)
	mustWriteFile(t, filepath.Join(tmp, "root", "b.md"), `---
leafwiki_id: mixed-dup-id
leafwiki_title: B
---
# B`, 0o644)

	_, err := store.ReconstructTreeFromFS()
	if err == nil {
		t.Fatalf("expected duplicate ID error")
	}
	if !strings.Contains(err.Error(), "duplicate leafwiki_id") {
		t.Fatalf("expected duplicate ID error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "mixed-dup-id") {
		t.Fatalf("expected duplicate ID to be mentioned, got: %v", err)
	}
}

func TestNodeStore_ReconstructTreeFromFS_ReturnsErrorOnMalformedCanonicalMetadata(t *testing.T) {
	tests := []struct {
		name string
		path string
		raw  string
	}{
		{
			name: "page file",
			path: filepath.Join("root", "bad.md"),
			raw: `<!-- leafwiki
version: 1
page:
  id: bad
fields:
  aliases:
    - one
-->
# Bad`,
		},
		{
			name: "section index",
			path: filepath.Join("root", "docs", "index.md"),
			raw: `<!-- leafwiki
version: 1
page:
  id: docs
fields:
  leafwiki_hidden: true
-->
# Docs`,
		},
		{
			name: "root index",
			path: filepath.Join("root", "index.md"),
			raw: `<!-- leafwiki
version: 2
page:
  id: root
-->
# Root`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmp := t.TempDir()
			store := NewNodeStore(tmp)
			mustWriteFile(t, filepath.Join(tmp, tt.path), tt.raw, 0o644)

			_, err := store.ReconstructTreeFromFS()
			if err == nil {
				t.Fatalf("expected reconstruct error")
			}
			if !strings.Contains(err.Error(), "metadata parse error") {
				t.Fatalf("expected metadata parse error, got: %v", err)
			}
		})
	}
}

func TestNodeStore_ReconstructTreeFromFS_ReturnsErrorOnCaseInsensitiveDuplicateSlugs(t *testing.T) {
	tmp := t.TempDir()
	store := NewNodeStore(tmp)

	mustWriteFile(t, filepath.Join(tmp, "root", "abc.md"), "# lower", 0o644)
	mustWriteFile(t, filepath.Join(tmp, "root", "ABC.md"), "# upper", 0o644)

	entries, err := os.ReadDir(filepath.Join(tmp, "root"))
	if err != nil {
		t.Fatalf("read root dir: %v", err)
	}
	if len(entries) < 2 {
		t.Skip("filesystem does not preserve case-only duplicate filenames")
	}

	_, err = store.ReconstructTreeFromFS()
	if err == nil {
		t.Fatalf("expected duplicate slug error")
	}
	if !strings.Contains(err.Error(), "duplicate slug") {
		t.Fatalf("expected duplicate slug error, got: %v", err)
	}
	if !strings.Contains(strings.ToLower(err.Error()), "abc") {
		t.Fatalf("expected conflicting slug to be mentioned, got: %v", err)
	}
}

func TestNodeStore_ReconstructTreeFromFS_AllowsDirectoryFileSlugPair(t *testing.T) {
	tmp := t.TempDir()
	store := NewNodeStore(tmp)

	mustMkdir(t, filepath.Join(tmp, "root", "notes"))
	mustWriteFile(t, filepath.Join(tmp, "root", "notes", "index.md"), `---
leafwiki_id: notes-section
leafwiki_title: Notes Section
---
# Notes section
`, 0o644)
	mustWriteFile(t, filepath.Join(tmp, "root", "notes.md"), `---
leafwiki_id: notes-page
leafwiki_title: Notes Page
---
# Notes page
`, 0o644)

	tree, err := store.ReconstructTreeFromFS()
	if err != nil {
		t.Fatalf("ReconstructTreeFromFS: %v", err)
	}
	var page, section *PageNode
	for _, child := range tree.Children {
		if child.Slug != "notes" {
			continue
		}
		if child.Kind == NodeKindPage {
			page = child
		}
		if child.Kind == NodeKindSection {
			section = child
		}
	}
	if page == nil || page.ID != "notes-page" {
		t.Fatalf("notes page = %#v, want notes-page", page)
	}
	if section == nil || section.ID != "notes-section" {
		t.Fatalf("notes section = %#v, want notes-section", section)
	}
}

func TestNodeStore_ReconstructTreeFromFS_ImportsNormalizableWorkspaceRoutes(t *testing.T) {
	tmp := t.TempDir()
	store := NewNodeStore(tmp)

	mustMkdir(t, filepath.Join(tmp, "root", "plans"))
	mustWriteFile(t, filepath.Join(tmp, "root", "plans", "index.md"), "# Plans", 0o644)
	mustWriteFile(t, filepath.Join(tmp, "root", "plans", "agent_hooks.PLAN.md"), "# Agent Hooks Plan", 0o644)
	mustMkdir(t, filepath.Join(tmp, "root", "User Guides"))
	mustWriteFile(t, filepath.Join(tmp, "root", "User Guides", "index.md"), "# User Guides", 0o644)

	tree, err := store.ReconstructTreeFromFS()
	if err != nil {
		t.Fatalf("ReconstructTreeFromFS: %v", err)
	}

	plans := findChildBySlug(t, tree, "plans")
	if plans.Kind != NodeKindSection {
		t.Fatalf("plans.Kind = %q, want %q", plans.Kind, NodeKindSection)
	}
	plan := findChildBySlug(t, plans, "agent-hooks-plan")
	if plan.Kind != NodeKindPage {
		t.Fatalf("plan.Kind = %q, want %q", plan.Kind, NodeKindPage)
	}
	if plan.Title != "Agent Hooks Plan" {
		t.Fatalf("plan.Title = %q, want Agent Hooks Plan", plan.Title)
	}
	raw, err := store.ReadPageRaw(plan)
	if err != nil {
		t.Fatalf("ReadPageRaw normalized plan: %v", err)
	}
	if !strings.Contains(raw, "Agent Hooks Plan") {
		t.Fatalf("ReadPageRaw normalized plan = %q, want original file content", raw)
	}

	guides := findChildBySlug(t, tree, "user-guides")
	if guides.Kind != NodeKindSection {
		t.Fatalf("guides.Kind = %q, want %q", guides.Kind, NodeKindSection)
	}
	rawGuides, err := store.ReadPageRaw(guides)
	if err != nil {
		t.Fatalf("ReadPageRaw normalized section: %v", err)
	}
	if !strings.Contains(rawGuides, "User Guides") {
		t.Fatalf("ReadPageRaw normalized section = %q, want original section content", rawGuides)
	}
}

func TestNodeStore_ReconstructTreeFromFS_ReturnsErrorOnNormalizedDuplicatePageRoutes(t *testing.T) {
	tmp := t.TempDir()
	store := NewNodeStore(tmp)

	mustMkdir(t, filepath.Join(tmp, "root", "plans"))
	mustWriteFile(t, filepath.Join(tmp, "root", "plans", "foo_bar.md"), "# Foo Bar", 0o644)
	mustWriteFile(t, filepath.Join(tmp, "root", "plans", "foo-bar.md"), "# Foo Bar Duplicate", 0o644)

	_, err := store.ReconstructTreeFromFS()
	if err == nil {
		t.Fatalf("expected duplicate normalized slug error")
	}
	if !strings.Contains(err.Error(), "duplicate page slug") {
		t.Fatalf("expected duplicate page slug error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "foo_bar.md") || !strings.Contains(err.Error(), "foo-bar.md") {
		t.Fatalf("expected both conflicting paths in error, got: %v", err)
	}
}

func TestNodeStore_ReconstructTreeFromFS_SkipsTopLevelStaticAssets(t *testing.T) {
	tmp := t.TempDir()
	store := NewNodeStore(tmp)

	mustMkdir(t, filepath.Join(tmp, "root", "assets", "install"))
	mustWriteFile(t, filepath.Join(tmp, "root", "assets", "install", "image.png"), "png", 0o644)
	mustWriteFile(t, filepath.Join(tmp, "root", "guide.md"), "# Guide", 0o644)

	tree, err := store.ReconstructTreeFromFS()
	if err != nil {
		t.Fatalf("ReconstructTreeFromFS: %v", err)
	}

	findChildBySlug(t, tree, "guide")
	for _, child := range tree.Children {
		if strings.EqualFold(child.Slug.String(), "assets") || strings.EqualFold(child.Slug.String(), "assets-1") {
			t.Fatalf("top-level static assets directory became wiki child: %#v", child)
		}
	}
}

func TestNodeStore_ReconstructTreeFromFS_WritesIDsBackToFiles(t *testing.T) {
	tmp := t.TempDir()
	store := NewNodeStore(tmp)

	// Create files without leafwiki_id in frontmatter
	mustWriteFile(t, filepath.Join(tmp, "root", "no-id.md"), "# No ID", 0o644)
	mustMkdir(t, filepath.Join(tmp, "root", "section"))
	mustWriteFile(t, filepath.Join(tmp, "root", "section", "index.md"), "# Section No ID", 0o644)

	// Run reconstruction
	tree, err := store.ReconstructTreeFromFS()
	if err != nil {
		t.Fatalf("ReconstructTreeFromFS: %v", err)
	}

	// Get the page and section nodes
	page := findChildBySlug(t, tree, "no-id")
	section := findChildBySlug(t, tree, "section")

	// Verify that IDs were generated
	if page.ID == "" {
		t.Fatalf("expected page to have generated ID, got empty")
	}
	if section.ID == "" {
		t.Fatalf("expected section to have generated ID, got empty")
	}

	// Now reload the files and check that IDs were written back
	pageMd, err := markdown.LoadMarkdownFile(filepath.Join(tmp, "root", "no-id.md"))
	if err != nil {
		t.Fatalf("failed to reload page: %v", err)
	}
	if newFixturePageID(pageMd.GetFrontmatter().LeafWikiID) != page.ID {
		t.Fatalf("expected page frontmatter ID=%q, got %q", page.ID, pageMd.GetFrontmatter().LeafWikiID)
	}

	sectionMd, err := markdown.LoadMarkdownFile(filepath.Join(tmp, "root", "section", "index.md"))
	if err != nil {
		t.Fatalf("failed to reload section index: %v", err)
	}
	if newFixturePageID(sectionMd.GetFrontmatter().LeafWikiID) != section.ID {
		t.Fatalf("expected section frontmatter ID=%q, got %q", section.ID, sectionMd.GetFrontmatter().LeafWikiID)
	}

	// Run reconstruction again and verify IDs are stable (deterministic)
	tree2, err := store.ReconstructTreeFromFS()
	if err != nil {
		t.Fatalf("second ReconstructTreeFromFS: %v", err)
	}

	page2 := findChildBySlug(t, tree2, "no-id")
	section2 := findChildBySlug(t, tree2, "section")

	if page2.ID != page.ID {
		t.Fatalf("expected deterministic page ID on second run: first=%q, second=%q", page.ID, page2.ID)
	}
	if section2.ID != section.ID {
		t.Fatalf("expected deterministic section ID on second run: first=%q, second=%q", section.ID, section2.ID)
	}
}

func TestNodeStore_ReconstructTreeFromFS_NormalizesImportableSlugsAndSkipsEmptyNormalizedSlugs(t *testing.T) {
	tmp := t.TempDir()
	store := NewNodeStore(tmp)

	// Create files and directories with names that were invalid route slugs but
	// can be normalized safely.
	mustWriteFile(t, filepath.Join(tmp, "root", "Valid Page.md"), "# Valid", 0o644)
	mustWriteFile(t, filepath.Join(tmp, "root", "UPPERCASE.md"), "# Upper", 0o644)
	mustMkdir(t, filepath.Join(tmp, "root", "Valid Section"))
	mustWriteFile(t, filepath.Join(tmp, "root", "Valid Section", "index.md"), "# Section", 0o644)
	mustWriteFile(t, filepath.Join(tmp, "root", "!!!.md"), "# Invalid", 0o644)

	// Create a valid file to ensure the test still works
	mustWriteFile(t, filepath.Join(tmp, "root", "valid.md"), "# Valid", 0o644)

	tree, err := store.ReconstructTreeFromFS()
	if err != nil {
		t.Fatalf("ReconstructTreeFromFS: %v", err)
	}

	// The valid file should be present with normalized slug
	findChildBySlug(t, tree, "valid")
	findChildBySlug(t, tree, "UPPERCASE")
	findChildBySlug(t, tree, "valid-page")
	findChildBySlug(t, tree, "valid-section")

	if len(tree.Children) != 4 {
		t.Fatalf("expected only empty-normalized names to be skipped, got %v", slugs(tree.Children))
	}
}

func TestNodeStore_ReconstructTreeFromFS_PreservesMixedCaseSlugNames(t *testing.T) {
	tmp := t.TempDir()
	store := NewNodeStore(tmp)

	mustWriteFile(t, filepath.Join(tmp, "root", "ABCD-efg.md"), "# Mixed Case", 0o644)

	tree, err := store.ReconstructTreeFromFS()
	if err != nil {
		t.Fatalf("ReconstructTreeFromFS: %v", err)
	}

	findChildBySlug(t, tree, "ABCD-efg")
}
func TestNodeStore_ReconstructTreeFromFS_ReadsMetadataFromFrontmatter(t *testing.T) {
	tmp := t.TempDir()
	store := NewNodeStore(tmp)

	mustWriteFile(t, filepath.Join(tmp, "root", "page.md"), `---
leafwiki_id: page-1
leafwiki_title: Page One
leafwiki_created_at: 2026-03-21T10:15:30Z
leafwiki_updated_at: 2026-03-21T11:16:31Z
leafwiki_creator_id: alice
leafwiki_last_author_id: bob
---
# Page One`, 0o644)

	tree, err := store.ReconstructTreeFromFS()
	if err != nil {
		t.Fatalf("ReconstructTreeFromFS: %v", err)
	}

	page := findChildBySlug(t, tree, "page")
	if page.ID != "page-1" {
		t.Fatalf("expected page ID from frontmatter, got %q", page.ID)
	}
	if got := page.Metadata.CreatedAt.UTC().Format(time.RFC3339); got != "2026-03-21T10:15:30Z" {
		t.Fatalf("expected created_at from frontmatter, got %q", got)
	}
	if got := page.Metadata.UpdatedAt.UTC().Format(time.RFC3339); got != "2026-03-21T11:16:31Z" {
		t.Fatalf("expected updated_at from frontmatter, got %q", got)
	}
	if page.Metadata.CreatorID != "alice" || page.Metadata.LastAuthorID != "bob" {
		t.Fatalf("expected author metadata from frontmatter, got %#v", page.Metadata)
	}
}

func TestNodeStore_ReconstructTreeFromFS_CanonicalizesCompleteLegacyMetadata(t *testing.T) {
	tmp := t.TempDir()
	store := NewNodeStore(tmp)

	rootIndex := filepath.Join(tmp, "root", "index.md")
	sectionIndex := filepath.Join(tmp, "root", "docs", "index.md")
	pagePath := filepath.Join(tmp, "root", "docs", "page.md")

	mustWriteFile(t, rootIndex, `---
leafwiki_id: root
leafwiki_title: Root
leafwiki_created_at: 2026-03-21T09:00:00Z
leafwiki_updated_at: 2026-03-21T09:30:00Z
leafwiki_creator_id: root-author
leafwiki_last_author_id: root-editor
---
# Root`, 0o644)
	mustWriteFile(t, sectionIndex, `---
leafwiki_id: docs-section
leafwiki_title: Docs
leafwiki_created_at: 2026-03-21T10:00:00Z
leafwiki_updated_at: 2026-03-21T10:30:00Z
leafwiki_creator_id: docs-author
leafwiki_last_author_id: docs-editor
---
# Docs`, 0o644)
	mustWriteFile(t, pagePath, `---
leafwiki_id: docs-page
leafwiki_title: Page
leafwiki_created_at: 2026-03-21T11:00:00Z
leafwiki_updated_at: 2026-03-21T11:30:00Z
leafwiki_creator_id: page-author
leafwiki_last_author_id: page-editor
---
# Page`, 0o644)

	tree, err := store.ReconstructTreeFromFS()
	if err != nil {
		t.Fatalf("ReconstructTreeFromFS: %v", err)
	}

	if tree.ID != "root" {
		t.Fatalf("root ID = %q", tree.ID)
	}
	section := findChildBySlug(t, tree, "docs")
	if section.ID != "docs-section" {
		t.Fatalf("section ID = %q", section.ID)
	}
	page := findChildBySlug(t, section, "page")
	if page.ID != "docs-page" {
		t.Fatalf("page ID = %q", page.ID)
	}

	for _, path := range []string{rootIndex, sectionIndex, pagePath} {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%s): %v", path, err)
		}
		content := string(raw)
		if !strings.HasPrefix(content, "<!-- leafwiki\n") {
			t.Fatalf("%s was not canonicalized: %q", path, content)
		}
		if strings.HasPrefix(content, "---\n") {
			t.Fatalf("%s still starts with YAML frontmatter: %q", path, content)
		}
		if _, _, err := markdown.ParsePageDocument(content); err != nil {
			t.Fatalf("ParsePageDocument(%s): %v", path, err)
		}
	}
}

func TestNodeStore_ReconstructTreeFromFS_MissingMetadataFallsBackToMtimeAndSystem(t *testing.T) {
	tmp := t.TempDir()
	store := NewNodeStore(tmp)

	pagePath := filepath.Join(tmp, "root", "page.md")
	mustWriteFile(t, pagePath, `# Page One`, 0o644)

	wantTime := time.Date(2026, time.March, 21, 12, 34, 56, 0, time.UTC)
	if err := os.Chtimes(pagePath, wantTime, wantTime); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	tree, err := store.ReconstructTreeFromFS()
	if err != nil {
		t.Fatalf("ReconstructTreeFromFS: %v", err)
	}

	page := findChildBySlug(t, tree, "page")
	if strings.TrimSpace(page.ID.String()) == "" {
		t.Fatalf("expected generated ID")
	}
	if got := page.Metadata.CreatedAt.UTC().Format(time.RFC3339); got != wantTime.Format(time.RFC3339) {
		t.Fatalf("expected created_at fallback from mtime, got %q", got)
	}
	if got := page.Metadata.UpdatedAt.UTC().Format(time.RFC3339); got != wantTime.Format(time.RFC3339) {
		t.Fatalf("expected updated_at fallback from mtime, got %q", got)
	}
	if page.Metadata.CreatorID != reconstructSystemUserID || page.Metadata.LastAuthorID != reconstructSystemUserID {
		t.Fatalf("expected system user fallback, got %#v", page.Metadata)
	}

	mdFile, err := markdown.LoadMarkdownFile(pagePath)
	if err != nil {
		t.Fatalf("LoadMarkdownFile: %v", err)
	}
	fm := mdFile.GetFrontmatter()
	if newFixturePageID(fm.LeafWikiID) != page.ID {
		t.Fatalf("expected generated ID to be written back, got %q want %q", fm.LeafWikiID, page.ID)
	}
	if fm.LeafWikiCreatedAt != wantTime.Format(time.RFC3339) {
		t.Fatalf("expected created_at fallback to be written back, got %q", fm.LeafWikiCreatedAt)
	}
	if fm.LeafWikiUpdatedAt != wantTime.Format(time.RFC3339) {
		t.Fatalf("expected updated_at fallback to be written back, got %q", fm.LeafWikiUpdatedAt)
	}
	if fm.LeafWikiCreatorID != reconstructSystemUserID || fm.LeafWikiLastAuthorID != reconstructSystemUserID {
		t.Fatalf("expected system metadata fallback to be written back, got %#v", fm)
	}
}

func TestNodeStore_ReconstructTreeFromFS_InvalidMetadataTimestampFallsBackToMtime(t *testing.T) {
	tmp := t.TempDir()
	store := NewNodeStore(tmp)

	pagePath := filepath.Join(tmp, "root", "page.md")
	mustWriteFile(t, pagePath, `---
leafwiki_id: page-1
leafwiki_title: Page One
leafwiki_created_at: not-a-timestamp
leafwiki_updated_at: 2026-03-21T11:16:31Z
leafwiki_creator_id: alice
leafwiki_last_author_id: bob
---
# Page One`, 0o644)

	wantTime := time.Date(2026, time.March, 21, 12, 34, 56, 0, time.UTC)
	if err := os.Chtimes(pagePath, wantTime, wantTime); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	tree, err := store.ReconstructTreeFromFS()
	if err != nil {
		t.Fatalf("ReconstructTreeFromFS: %v", err)
	}

	page := findChildBySlug(t, tree, "page")
	if got := page.Metadata.CreatedAt.UTC().Format(time.RFC3339); got != wantTime.Format(time.RFC3339) {
		t.Fatalf("expected invalid created_at to fall back to mtime, got %q", got)
	}
	if got := page.Metadata.UpdatedAt.UTC().Format(time.RFC3339); got != "2026-03-21T11:16:31Z" {
		t.Fatalf("expected valid updated_at to be preserved, got %q", got)
	}
	if page.Metadata.CreatorID != "alice" || page.Metadata.LastAuthorID != "bob" {
		t.Fatalf("expected author metadata to be preserved, got %#v", page.Metadata)
	}
}
