package markdown

import "testing"

func TestParsePageDocument_LegacyFrontmatterMigratesToMetadata(t *testing.T) {
	raw := `---
leafwiki_id: page-123
leafwiki_title: Example Page
leafwiki_created_at: "2026-06-13T10:00:00Z"
leafwiki_updated_at: "2026-06-13T11:00:00Z"
leafwiki_creator_id: alice
leafwiki_last_author_id: bob
tags:
  - research
status: open
priority: 2
published: false
aliases:
  - old-example
---
# Example Page
`

	doc, result, err := ParsePageDocument(raw)
	if err != nil {
		t.Fatalf("ParsePageDocument() error = %v", err)
	}
	if !result.RequiresWriteback {
		t.Fatalf("legacy frontmatter should require canonical writeback")
	}
	if doc.Body != "# Example Page\n" {
		t.Fatalf("body = %q", doc.Body)
	}

	meta := doc.Metadata
	if meta.Version != 1 {
		t.Fatalf("version = %d, want 1", meta.Version)
	}
	if meta.Page.ID != "page-123" {
		t.Fatalf("page id = %q", meta.Page.ID)
	}
	if meta.Page.Title != "Example Page" {
		t.Fatalf("page title = %q", meta.Page.Title)
	}
	if meta.Page.CreatedAt != "2026-06-13T10:00:00Z" {
		t.Fatalf("created_at = %q", meta.Page.CreatedAt)
	}
	if meta.Page.UpdatedAt != "2026-06-13T11:00:00Z" {
		t.Fatalf("updated_at = %q", meta.Page.UpdatedAt)
	}
	if meta.Page.CreatorID != "alice" {
		t.Fatalf("creator_id = %q", meta.Page.CreatorID)
	}
	if meta.Page.LastAuthorID != "bob" {
		t.Fatalf("last_author_id = %q", meta.Page.LastAuthorID)
	}
	if len(meta.Tags) != 1 || meta.Tags[0] != "research" {
		t.Fatalf("tags = %#v", meta.Tags)
	}
	if got := meta.Fields["status"]; got != "open" {
		t.Fatalf("status field = %#v", got)
	}
	if got := meta.Fields["priority"]; got != 2 {
		t.Fatalf("priority field = %#v", got)
	}
	if got := meta.Fields["published"]; got != false {
		t.Fatalf("published field = %#v", got)
	}
	aliases, ok := meta.Extra["aliases"].([]interface{})
	if !ok || len(aliases) != 1 || aliases[0] != "old-example" {
		t.Fatalf("aliases extra = %#v", meta.Extra["aliases"])
	}
}

func TestParsePageDocument_LegacyTitleAliasOnlyConsumedWhenManagedTitleAbsent(t *testing.T) {
	raw := `---
leafwiki_title: Managed Title
title: User Title Field
---
# Body
`

	doc, _, err := ParsePageDocument(raw)
	if err != nil {
		t.Fatalf("ParsePageDocument() error = %v", err)
	}
	if doc.Metadata.Page.Title != "Managed Title" {
		t.Fatalf("page title = %q", doc.Metadata.Page.Title)
	}
	if got := doc.Metadata.Fields["title"]; got != "User Title Field" {
		t.Fatalf("expected legacy title field to be preserved, got %#v", got)
	}

	aliasOnly := `---
title: Alias Title
---
# Body
`
	doc, _, err = ParsePageDocument(aliasOnly)
	if err != nil {
		t.Fatalf("ParsePageDocument(aliasOnly) error = %v", err)
	}
	if doc.Metadata.Page.Title != "Alias Title" {
		t.Fatalf("alias page title = %q", doc.Metadata.Page.Title)
	}
	if _, exists := doc.Metadata.Fields["title"]; exists {
		t.Fatalf("title alias should not be duplicated as a field: %#v", doc.Metadata.Fields)
	}
}

func TestParsePageDocument_LegacyReservedKeysAreCaseInsensitive(t *testing.T) {
	raw := `---
leafwiki_id: page-123
LeafWiki_status: hidden
status: public
---
# Body
`

	doc, _, err := ParsePageDocument(raw)
	if err != nil {
		t.Fatalf("ParsePageDocument() error = %v", err)
	}
	if got := doc.Metadata.Fields["status"]; got != "public" {
		t.Fatalf("status field = %#v", got)
	}
	if _, exists := doc.Metadata.Fields["LeafWiki_status"]; exists {
		t.Fatalf("reserved mixed-case key must not enter fields: %#v", doc.Metadata.Fields)
	}
	if got := doc.Metadata.Extra["LeafWiki_status"]; got != "hidden" {
		t.Fatalf("reserved mixed-case key should be preserved in extra, got %#v", got)
	}
}

func TestParsePageDocument_MalformedLegacyFrontmatterFailsWithoutGeneratedIdentity(t *testing.T) {
	raw := `---
leafwiki_id: [broken
---
# Body
`

	doc, result, err := ParsePageDocument(raw)
	if err == nil {
		t.Fatalf("ParsePageDocument() error = nil, doc = %#v, result = %#v", doc, result)
	}
	if doc.Metadata.Page.ID != "" {
		t.Fatalf("malformed legacy frontmatter generated page id %q", doc.Metadata.Page.ID)
	}
}

func TestParsePageDocument_LegacyUnknownMapAndNullValuesStayInExtra(t *testing.T) {
	raw := `---
leafwiki_id: page-123
nested:
  owner: docs
empty_value: null
---
# Body
`

	doc, _, err := ParsePageDocument(raw)
	if err != nil {
		t.Fatalf("ParsePageDocument() error = %v", err)
	}
	if _, exists := doc.Metadata.Fields["nested"]; exists {
		t.Fatalf("nested map moved into fields: %#v", doc.Metadata.Fields)
	}
	if _, exists := doc.Metadata.Fields["empty_value"]; exists {
		t.Fatalf("null value moved into fields: %#v", doc.Metadata.Fields)
	}
	nested, ok := doc.Metadata.Extra["nested"].(map[string]interface{})
	if !ok || nested["owner"] != "docs" {
		t.Fatalf("nested extra = %#v", doc.Metadata.Extra["nested"])
	}
	if _, exists := doc.Metadata.Extra["empty_value"]; !exists {
		t.Fatalf("null extra key missing: %#v", doc.Metadata.Extra)
	}
	if got := doc.Metadata.Extra["empty_value"]; got != nil {
		t.Fatalf("empty_value extra = %#v, want nil", got)
	}
}

func TestParsePageDocument_LegacyCanonicalTopLevelNamesStayInExtra(t *testing.T) {
	raw := `---
leafwiki_id: page-123
version: 99
page:
  id: colliding
fields:
  status: hidden
extra:
  owner: docs
---
# Body
`

	doc, _, err := ParsePageDocument(raw)
	if err != nil {
		t.Fatalf("ParsePageDocument() error = %v", err)
	}
	if doc.Metadata.Version != 1 {
		t.Fatalf("metadata version = %d, want canonical migration version 1", doc.Metadata.Version)
	}
	if doc.Metadata.Page.ID != "page-123" {
		t.Fatalf("page id = %q", doc.Metadata.Page.ID)
	}
	for _, key := range []string{"version", "page", "fields", "extra"} {
		if _, exists := doc.Metadata.Fields[key]; exists {
			t.Fatalf("%s moved into fields: %#v", key, doc.Metadata.Fields)
		}
		if _, exists := doc.Metadata.Extra[key]; !exists {
			t.Fatalf("%s missing from extra: %#v", key, doc.Metadata.Extra)
		}
	}
}

func TestParsePageDocument_CanonicalMetadataStripsLegacyFrontmatterBody(t *testing.T) {
	raw := `<!-- leafwiki
version: 1
page:
  id: canonical-id
  title: Canonical Title
-->
---
leafwiki_id: legacy-id
leafwiki_title: Legacy Title
---
# Body
`

	doc, result, err := ParsePageDocument(raw)
	if err != nil {
		t.Fatalf("ParsePageDocument() error = %v", err)
	}
	if !result.RequiresWriteback {
		t.Fatalf("mixed canonical and legacy frontmatter should require writeback")
	}
	if doc.Metadata.Page.ID != "canonical-id" {
		t.Fatalf("page id = %q", doc.Metadata.Page.ID)
	}
	if doc.Metadata.Page.Title != "Canonical Title" {
		t.Fatalf("page title = %q", doc.Metadata.Page.Title)
	}
	if doc.Body != "# Body\n" {
		t.Fatalf("body = %q", doc.Body)
	}
}
