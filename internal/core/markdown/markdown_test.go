package markdown

import (
	"os"
	"strings"
	"testing"

	"github.com/perber/wiki/internal/test_utils"
)

func TestPlanner_extractTitleFromMDFile_FrontmatterTitleWins(t *testing.T) {
	tmp := t.TempDir()
	abs := test_utils.WriteFile(t, tmp, "t.md", "---\ntitle: FM Title\n---\n\n# Heading")

	mdFile, err := LoadMarkdownFile(abs)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	title, err := mdFile.GetTitle()
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if title != "FM Title" {
		t.Fatalf("title = %q", title)
	}
}

func TestPlanner_extractTitleFromMDFile_LeafwikiTitle(t *testing.T) {
	tmp := t.TempDir()
	abs := test_utils.WriteFile(t, tmp, "t.md", "---\nleafwiki_title: Leaf\n---\n\n# Heading")

	mdFile, err := LoadMarkdownFile(abs)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	title, err := mdFile.GetTitle()
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if title != "Leaf" {
		t.Fatalf("title = %q", title)
	}
}

func TestPlanner_extractTitleFromMDFile_FirstHeadingFallback(t *testing.T) {
	tmp := t.TempDir()
	abs := test_utils.WriteFile(t, tmp, "t.md", "no fm\n\n# Heading Only\nx")

	mdFile, err := LoadMarkdownFile(abs)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	title, err := mdFile.GetTitle()
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if title != "Heading Only" {
		t.Fatalf("title = %q", title)
	}
}

func TestPlanner_extractTitleFromMDFile_FilenameFallback(t *testing.T) {
	tmp := t.TempDir()
	abs := test_utils.WriteFile(t, tmp, "some-file.md", "no title")

	mdFile, err := LoadMarkdownFile(abs)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	title, err := mdFile.GetTitle()
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if title != "some-file" {
		t.Fatalf("title = %q", title)
	}
}

func TestPlanner_extractTitleFromMDFile_FilenameFallback_WindowsPath(t *testing.T) {
	mdFile, err := NewMarkdownFileFromRaw(`C:\Users\johnjkr\AppData\Local\Temp\import-1280817455\1999-07-23 - Memo to Staff.md`, "no title")
	if err != nil {
		t.Fatalf("err: %v", err)
	}

	title, err := mdFile.GetTitle()
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if title != "1999-07-23 - Memo to Staff" {
		t.Fatalf("title = %q", title)
	}
}

func TestMarkdownFile_WriteToFile_WritesCanonicalMetadata(t *testing.T) {
	tmp := t.TempDir()
	abs := test_utils.WriteFile(t, tmp, "t.md", `---
custom_key: keep-me
aliases:
  - one
leafwiki_id: old-id
leafwiki_title: Old Title
---

# Heading`)

	mdFile, err := LoadMarkdownFile(abs)
	if err != nil {
		t.Fatalf("err: %v", err)
	}

	mdFile.setMetadataID("new-id")
	if err := mdFile.WriteToFile(); err != nil {
		t.Fatalf("WriteToFile err: %v", err)
	}

	rawBytes, err := os.ReadFile(abs)
	if err != nil {
		t.Fatalf("ReadFile err: %v", err)
	}
	raw := string(rawBytes)
	if !strings.HasPrefix(raw, "<!-- leafwiki\n") {
		t.Fatalf("expected canonical metadata comment, got: %q", raw)
	}
	if strings.HasPrefix(raw, "---\n") {
		t.Fatalf("expected YAML frontmatter to be removed, got: %q", raw)
	}

	doc, result, err := ParsePageDocument(raw)
	if err != nil {
		t.Fatalf("ParsePageDocument err: %v", err)
	}
	if result.RequiresWriteback {
		t.Fatalf("newly written canonical metadata should not require writeback")
	}
	if doc.Metadata.Page.ID != "new-id" {
		t.Fatalf("expected id 'new-id', got %q", doc.Metadata.Page.ID)
	}
	if doc.Metadata.Page.Title != "Old Title" {
		t.Fatalf("expected title 'Old Title', got %q", doc.Metadata.Page.Title)
	}
	if got := doc.Metadata.Fields["custom_key"]; got != "keep-me" {
		t.Fatalf("expected custom scalar metadata to be preserved in fields, got %#v", got)
	}
	aliases, ok := doc.Metadata.Extra["aliases"].([]interface{})
	if !ok || len(aliases) != 1 || aliases[0] != "one" {
		t.Fatalf("expected custom list metadata to be preserved in extra, got %#v", doc.Metadata.Extra["aliases"])
	}
	if doc.Body != "\n# Heading" {
		t.Fatalf("unexpected body: %q", doc.Body)
	}
}

func TestMarkdownFile_WriteToFile_PreservesCanonicalFieldsAndExtraBoundaries(t *testing.T) {
	tmp := t.TempDir()
	abs := test_utils.WriteFile(t, tmp, "t.md", `<!-- leafwiki
version: 1
page:
  id: page-123
  title: Boundary Test
tags:
  - keep
fields:
  priority: 2
  published: false
  status: draft
extra:
  source: imported
  aliases:
    - old
-->

Body`)

	mdFile, err := LoadMarkdownFile(abs)
	if err != nil {
		t.Fatalf("LoadMarkdownFile err: %v", err)
	}
	if err := mdFile.WriteToFile(); err != nil {
		t.Fatalf("WriteToFile err: %v", err)
	}

	rawBytes, err := os.ReadFile(abs)
	if err != nil {
		t.Fatalf("ReadFile err: %v", err)
	}
	doc, _, err := ParsePageDocument(string(rawBytes))
	if err != nil {
		t.Fatalf("ParsePageDocument err: %v", err)
	}
	if got := doc.Metadata.Fields["priority"]; got != 2 {
		t.Fatalf("priority field = %#v", got)
	}
	if got := doc.Metadata.Fields["published"]; got != false {
		t.Fatalf("published field = %#v", got)
	}
	if got := doc.Metadata.Fields["status"]; got != "draft" {
		t.Fatalf("status field = %#v", got)
	}
	if _, exists := doc.Metadata.Fields["source"]; exists {
		t.Fatalf("scalar extra source moved into fields: %#v", doc.Metadata.Fields)
	}
	if got := doc.Metadata.Extra["source"]; got != "imported" {
		t.Fatalf("source extra = %#v", got)
	}
	aliases, ok := doc.Metadata.Extra["aliases"].([]interface{})
	if !ok || len(aliases) != 1 || aliases[0] != "old" {
		t.Fatalf("aliases extra = %#v", doc.Metadata.Extra["aliases"])
	}
}

func TestMarkdownFile_SetRawContentPreservingManagedMetadata_PreservesHiddenMetadata(t *testing.T) {
	tmp := t.TempDir()
	abs := test_utils.WriteFile(t, tmp, "page.md", "")
	existing := `<!-- leafwiki
version: 1
page:
  id: page-123
  title: Existing
tags:
  - old
fields:
  priority: 2
  published: false
  status: draft
extra:
  source: imported
  aliases:
    - old
-->

Old body`
	mdFile, err := NewMarkdownFileFromRaw(abs, existing)
	if err != nil {
		t.Fatalf("NewMarkdownFileFromRaw err: %v", err)
	}

	incoming := `<!-- leafwiki
version: 1
page:
  id: page-123
  title: Existing
tags:
  - new
fields:
  status: ready
-->

New body`
	if err := mdFile.SetRawContentPreservingManagedMetadata(incoming); err != nil {
		t.Fatalf("SetRawContentPreservingManagedMetadata err: %v", err)
	}
	if err := mdFile.WriteToFile(); err != nil {
		t.Fatalf("WriteToFile err: %v", err)
	}
	rawBytes, err := os.ReadFile(abs)
	if err != nil {
		t.Fatalf("ReadFile err: %v", err)
	}
	doc, _, err := ParsePageDocument(string(rawBytes))
	if err != nil {
		t.Fatalf("ParsePageDocument err: %v", err)
	}
	if len(doc.Metadata.Tags) != 1 || doc.Metadata.Tags[0] != "new" {
		t.Fatalf("tags = %#v", doc.Metadata.Tags)
	}
	if got := doc.Metadata.Fields["status"]; got != "ready" {
		t.Fatalf("status field = %#v", got)
	}
	if got := doc.Metadata.Fields["priority"]; got != 2 {
		t.Fatalf("priority field = %#v", got)
	}
	if got := doc.Metadata.Fields["published"]; got != false {
		t.Fatalf("published field = %#v", got)
	}
	if got := doc.Metadata.Extra["source"]; got != "imported" {
		t.Fatalf("source extra = %#v", got)
	}
	if doc.Body != "New body" {
		t.Fatalf("body = %q", doc.Body)
	}
}

func TestLoadMarkdownFile_UppercaseExtension(t *testing.T) {
	tmp := t.TempDir()
	abs := test_utils.WriteFile(t, tmp, "README.MD", "# Uppercase Extension\n\nThis file has .MD extension")

	mdFile, err := LoadMarkdownFile(abs)
	if err != nil {
		t.Fatalf("expected no error for .MD extension, got: %v", err)
	}
	title, err := mdFile.GetTitle()
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if title != "Uppercase Extension" {
		t.Fatalf("title = %q, want %q", title, "Uppercase Extension")
	}
}

func TestNewMarkdownFileFromRaw_PreservesCustomFrontmatter(t *testing.T) {
	mdFile, err := NewMarkdownFileFromRaw("/tmp/test.md", `---
custom_key: keep-me
leafwiki_id: p1
leafwiki_title: Existing Title
---
# Body
Hello
`)
	if err != nil {
		t.Fatalf("err: %v", err)
	}

	if mdFile.GetFrontmatter().LeafWikiID != "p1" {
		t.Fatalf("expected id p1, got %q", mdFile.GetFrontmatter().LeafWikiID)
	}
	if mdFile.GetFrontmatter().LeafWikiTitle != "Existing Title" {
		t.Fatalf("expected title 'Existing Title', got %q", mdFile.GetFrontmatter().LeafWikiTitle)
	}
	if got := mdFile.GetContent(); got != "# Body\nHello\n" {
		t.Fatalf("unexpected content: %q", got)
	}
	if got := mdFile.GetFrontmatter().ExtraFields["custom_key"]; got != "keep-me" {
		t.Fatalf("expected custom_key to be preserved, got %#v", got)
	}
}

func TestNewMarkdownFileFromRaw_InvalidFrontmatter(t *testing.T) {
	_, err := NewMarkdownFileFromRaw("/tmp/test.md", `---
leafwiki_id: [broken
---
# Body
`)
	if err == nil {
		t.Fatalf("expected parse error")
	}
}
