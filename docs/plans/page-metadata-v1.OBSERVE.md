# Page Metadata V1 - Observe

## Purpose

This document captures the observed facts behind the Page Metadata V1 implementation plan. It is intentionally broader than the final plan so the implementation agent can see what was learned before decisions were made.

## Planning Input

The user wants to stop storing LeafWiki page metadata as visible YAML frontmatter because it renders poorly outside LeafWiki, especially on GitHub. The agreed direction is:

- Separate the internal model into Markdown document body and metadata.
- Store canonical metadata as YAML inside a strict top-of-file HTML comment.
- Treat legacy YAML frontmatter as migration input only, not as long-term runtime compatibility.
- Keep existing public API naming stable for now.
- Preserve room for future template/document-type metadata without adding that feature in v1.

The plan title was not provided during the workflow input step. The assumed plan title is `page-metadata-v1`.

## Conversation Decisions Captured

- Internal model should explicitly separate Markdown body from metadata.
- Storage format changes should become cheaper after the internal model split, but not free.
- V1 canonical storage should use HTML comment metadata, not sidecar files.
- Sidecar metadata is deferred until the model boundary is proven.
- Legacy frontmatter should be automatically migrated and then dropped. Runtime dual-format support is rejected.
- Migration should follow the canonical Markdown link precedent: old forms are ingestion/import inputs, canonical forms are the only write output.
- Canonical comment content is YAML.
- Canonical comment position is the top of the file.
- All LeafWiki-managed metadata moves out of visible Markdown body.
- Migration runs during ingestion/sync and save paths, not arbitrary read-only page access.
- If canonical comment and legacy frontmatter both exist, canonical metadata wins and legacy metadata is removed during writeback.
- Unknown legacy scalar frontmatter becomes `fields`.
- Unknown legacy non-scalar frontmatter becomes `extra`.
- Malformed YAML in legacy frontmatter is a migration failure requiring user/manual resolution.
- Malformed canonical comments fail ingestion/validation; no fallback after the canonical marker exists.
- Raw incoming files must be captured before automatic migration. The migration writeback must be visible in revision/sync history.
- Public Markdown body means renderable content only. `RawContent` should become storage-layer-only or be treated carefully as raw storage bytes.
- `fields` may contain YAML scalar string, number, and bool values.
- Top-level `version`, `page`, `tags`, `fields`, and `extra` are reserved canonical schema names.
- `leafwiki_*` names are banned inside `fields`.
- Strict marker lines are required:

```markdown
<!-- leafwiki
version: 1
page:
  id: example
  title: Example
tags:
  - demo
fields:
  status: draft
extra: {}
-->
```

- Do not add `document` classification/template binding in v1. The schema should leave room for a later top-level section.

## Technology Stack

- Backend: Go module `github.com/perber/wiki`.
- Go version in `go.mod`: `1.25.4`.
- HTTP: Gin under `internal/wiki/*` and DTOs under `internal/http/dto`.
- Markdown parsing/rendering: Goldmark and local Markdown utilities under `internal/core/markdown`.
- YAML: `gopkg.in/yaml.v3`.
- Persistence: SQLite via `modernc.org/sqlite`.
- Git/revisions/workspace sync: local services around Git-backed root directory and revision history.
- MCP: `github.com/modelcontextprotocol/go-sdk/mcp` under `internal/wiki/mcp`.
- Frontend: Vite, React, TypeScript in `ui/leafwiki-ui`.
- Frontend state/UI libraries include React 19, Tailwind 4, Zustand, Radix, CodeMirror, `react-markdown`, and `rehype-sanitize`.
- E2E: Playwright in `e2e`.

## Current Metadata Architecture

### Core Markdown Package

- `internal/core/markdown/frontmatter.go`
  - Central YAML frontmatter codec.
  - Parses top-of-file `---` frontmatter.
  - Preserves unknown keys in `Frontmatter.ExtraFields`.
  - Handles managed `leafwiki_*` keys and a `title` alias.
  - Renders deterministic YAML frontmatter.

- `internal/core/markdown/markdown.go`
  - `MarkdownFile` already stores body content separately from parsed `Frontmatter`.
  - `WriteToFile`, `SetRawContentPreservingManagedFrontmatter`, and `SetLeafWikiMetadata` assume YAML frontmatter is the write format.

This package is the right place for the new canonical metadata codec. Comment parsing should not be duplicated across tree, MCP, importer, validation, and sync packages.

### Tree and Page Storage

- `internal/core/tree/node_store.go`
  - Main reconstruction and writeback seam.
  - `ReconstructTreeFromFS` walks Markdown files from the root directory.
  - `metadataFromFrontmatter` derives page identity/title/timestamps from YAML frontmatter.
  - `writeReconstructedFrontmatter` writes missing managed metadata back to files.
  - `syncManagedFrontmatter`, `CreatePage`, `UpsertContent`, and `UpsertContentPreservingFrontmatter` all read or write frontmatter.

- `internal/core/tree/tree_service.go`
  - Routes create/update/restore/import flows into `NodeStore`.
  - `UpdateNode(..., FromImport: true)` currently means "preserve imported custom frontmatter". This will need to mean "accept raw Markdown that may contain legacy metadata and canonicalize it".

- `internal/core/tree/page.go` and `internal/core/tree/page_node.go`
  - Current internal page model exposes `Content` and `RawContent`.
  - This encourages callers to parse raw storage bytes directly.
  - The plan should define a clearer storage document model: body plus metadata.

### Tags, Properties, Search, and Excerpts

- `internal/wiki/pages/metadata.go`
  - `EnrichPageMetadata` parses frontmatter from raw page content.
  - `ExtractPageMetadata` reads `tags` and string scalar properties from `Frontmatter.ExtraFields`.

- `internal/tags/tags_service.go`
  - Indexes tags by parsing frontmatter from raw content.

- `internal/properties/properties_service.go`
  - Extracts scalar string properties from frontmatter.
  - Skips `tags`, `title`, and `leafwiki_*`.
  - Current behavior does not expose or index number/bool values as properties.

- `internal/properties/properties_store.go`
  - Stores value text and type text, so later typed field indexing may be possible without a large schema change.

- `internal/wiki/pagesave/*_effect.go`
  - Page save side effects operate on `RawContent`.
  - Tags/properties/search/revision effects must be moved onto the new parser so metadata comments do not leak into body indexing or excerpts.

- `internal/core/excerpt/excerpt.go`
  - Uses `ParseFrontmatter` to strip metadata before excerpting.
  - Needs canonical comment stripping.

### HTTP, MCP, and UI

- `internal/wiki/pages/routes.go`
  - HTTP page update accepts `tags []string` and `properties map[string]string`.
  - It currently builds YAML frontmatter and uses `FromImport: true`.

- `internal/wiki/pages/partial_edit.go`
  - Metadata patch behavior uses string properties.

- `internal/wiki/mcp/tools_pages.go`
  - `wiki_update_page` mirrors HTTP update behavior and currently writes YAML frontmatter.

- `internal/wiki/mcp/tools_partial_edit.go`
  - `wiki_update_page_metadata` reads raw Markdown, parses frontmatter, patches tags/properties, rebuilds frontmatter, then writes through `FromImport: true`.

- `internal/wiki/mcp/schema.go`
  - Public MCP schema still names tags and properties.

- `ui/leafwiki-ui/src/lib/api/pages.ts`
  - Frontend API type models `properties?: Record<string, string>`.

- `ui/leafwiki-ui/src/features/editor/frontmatter.ts`
  - Client-side metadata parsing/building still uses frontmatter naming.
  - It has local understanding of text/number/boolean/list values, but current save behavior is text-property oriented.

- `ui/leafwiki-ui/src/features/editor/stores/pageEditorStore.ts`
  - Only editable text properties are sent back to the API.

- `ui/leafwiki-ui/src/features/editor/components/PageFrontmatterPanel.tsx`
  - UI is tag plus text-property oriented and includes frontmatter language/copy.

### Validation

- `internal/core/markdownvalidation/use_cases.go`
  - Parses frontmatter.
  - Checks duplicate IDs.
  - Checks reserved `leafwiki_*` extras.
  - Validates workspace files.
  - This should become the canonical malformed-comment hard-error path while legacy frontmatter remains migration input.

- `internal/wiki/mcp/tools_validation.go`
  - MCP validation calls Markdown validation and infers existing page IDs from frontmatter.

### Workspace Sync and Revision History

- `internal/workspacesync/service.go`
  - Captures raw Markdown from the root directory.
  - Reconstructs the tree.
  - Captures metadata writebacks.
  - Validates Markdown.
  - Maps revisions to pages by parsing page IDs from raw content.

- `internal/core/revision/service.go`
  - Current revision enrichment stores extra frontmatter.
  - Restore rebuilds YAML frontmatter onto body content.
  - Needs a renamed/reworked metadata snapshot and canonical render path.

The high-risk part is ordering. Legacy frontmatter contains page identity. Migration must parse legacy metadata before reconstruction/backfill invents replacement IDs.

### Importer

- `internal/importer/planner.go`
  - Uses frontmatter/title data while planning imports.

- `internal/importer/executor.go`
  - Builds imported content and preserves extra frontmatter while dropping source managed IDs.

- `internal/importer/content_transformer.go`
  - Rewrites links across imported raw content.
  - With canonical metadata, link transforms should operate on body content, not the metadata comment.

- `internal/wiki/import_adapter.go`
  - Bridges importer output into wiki page writes.

## Existing Tests and Fixtures

Likely test targets:

- `internal/core/markdown/frontmatter_test.go`
- `internal/core/markdown/markdown_test.go`
- `internal/core/tree/node_store_test.go`
- `internal/core/tree/node_store_reconstruct_test.go`
- `internal/core/markdownvalidation/use_cases_test.go`
- `internal/tags/tags_service_test.go`
- `internal/properties/properties_service_test.go`
- `internal/wiki/pages/*_test.go`
- `internal/wiki/pagesave/*_test.go`
- `internal/wiki/mcp/*_test.go`
- `internal/core/revision/*_test.go`
- `internal/wiki/revisions/*_test.go`
- `internal/workspacesync/*_test.go`
- `internal/importer/*_test.go`
- `internal/http/router_test.go`
- `e2e/tests/page.spec.ts`
- `e2e/tests/importer.spec.ts`
- `e2e/tests/tags.spec.ts`
- `e2e/tests/workspace-sync.spec.ts`
- `e2e/tests/mcp-safe-edits.spec.ts`
- `e2e/tests/history.spec.ts`

Test conventions:

- Go tests use `testing`, table tests, `t.TempDir()`, exact serialized string assertions, and behavior-first names.
- Parser/serializer tests should live near the current frontmatter tests or in new codec-specific test files under `internal/core/markdown`.
- E2E tests should assert user-visible metadata behavior and file-storage outcomes when root-dir/workspace sync is involved.

## Field Usage Scan

A local scan of Markdown files under `/Users/jakubtomanik/github` found:

- 7,586 Markdown files in the broad scan set.
- 3,195 files with YAML frontmatter.
- 2,820 files with custom string fields, mostly from skills/docs/imported reference material.
- 241 LeafWiki-shaped files with `leafwiki_id`.
- 232 of those LeafWiki-shaped files were in `nowatch-ios-pr`, with many tags and no custom scalar fields found.
- 9 LeafWiki-shaped files were in this repo.
- 5 repo files had legacy custom string fields:
  - `type` in `data/root/welcome-to-leafwiki.md`
  - `category` in `internal/importer/fixtures/leafwiki-nested-package/intro.md`
  - `difficulty` in `internal/importer/fixtures/leafwiki-nested-package/docs/guides/basic-guide.md`
  - `summary` in `internal/importer/fixtures/leafwiki-nested-package/docs/guides/index.md`
  - `summary` in `internal/importer/fixtures/leafwiki-nested-package/docs/index.md`
- 1 repo fixture used a non-scalar unknown field:
  - `aliases` in `internal/importer/fixtures/leafwiki-nested-package/intro.md`
- 201 LeafWiki-shaped files had tags, with 678 tag instances.

Conclusion: v1 should support scalar `fields` now because it is cheap at the storage layer and future-friendly. Public property editing/indexing can remain string-oriented in v1, but numeric/bool scalar fields must be preserved losslessly once parsed.

## Prior Plans and Institutional Learnings

### `plans/canonical_markdown_links.PLAN.md`

Relevant precedent:

- Legacy syntaxes are migration input only.
- Ingestion/import rewrites resolvable legacy input to canonical output.
- Unresolved cases become validation issues rather than guessed rewrites.
- Raw incoming content is captured first.
- Automatic canonicalization writebacks are captured in sync/revision history.
- Validation runs after migration/writeback.

### `plans/workspace-sync.PLAN.md`

Relevant precedent:

- Workspace sync is core infrastructure.
- File changes from automatic LeafWiki writebacks are captured intentionally.
- Git capture before parsing matters.
- If Git capture fails, sync should fail before parsing/mutating derived state.

### `plans/workspace_sync_service.PLAN.md`

Relevant precedent:

- Reconstructing from the root directory is the source of truth.
- Missing managed metadata writebacks are part of sync behavior.
- Invalid frontmatter currently appears as a validation/sync concern.

### `plans/mcp-tools.PLAN.md`

Relevant precedent:

- Safe metadata edit tools need version checks.
- Metadata-only edits must preserve page body.
- Partial metadata updates must not rewrite unrelated content.

### `plans/llm_wiki_companion_skill.PLAN.md`

Relevant precedent:

- Templates should be Markdown bodies, not pre-seeded LeafWiki data files.
- Avoid pre-generating LeafWiki-managed metadata in templates.
- This old guidance needs updating later for canonical comment metadata, but document templates are out of scope for this v1.

## Explorer Summaries

### Explorer1: Stack, Patterns, and Boundaries

- Backend is Go, frontend is React/TypeScript, E2E is Playwright.
- Metadata parsing/serialization is concentrated under `internal/core/markdown`.
- `MarkdownFile` already separates content and parsed metadata, which supports the desired model.
- Tree persistence and reconstruction live under `internal/core/tree`.
- API naming is mostly separated from storage via HTTP DTOs and page routes.
- Tags/properties are derived indexes from raw page content and need a shared parser.
- Existing test patterns are standard Go table tests plus targeted E2E.

### Explorer2: File Map and Implementation Seams

- The main replacement seam is the current frontmatter codec.
- `NodeStore` reconstruction/writeback is the highest-risk integration point.
- `FromImport: true` is currently overloaded and should become "accept raw Markdown and canonicalize metadata."
- Tags/properties extraction must move off `ExtraFields`.
- Revisions need to move from `ExtraFrontmatter` toward canonical metadata snapshots.
- Workspace sync needs intentional migration writeback capture.
- RawContent is the biggest compatibility risk because many systems parse it independently.

### Explorer3: Institutional Learnings

- Treat this like canonical link migration: legacy input, canonical output.
- Do not support both frontmatter and comment metadata as runtime formats.
- Metadata migration is identity-sensitive and must read legacy metadata before replacement ID generation.
- Automatic migration belongs in ingestion/write paths, not read-only page access.
- Workspace sync should preserve raw incoming content and record migration writeback.
- Keep v1 narrow but versioned and namespaced.
- Define how old YAML frontmatter after canonical migration is handled.

## Constraints That Should Shape the Plan

- Do not add a sidecar storage format in v1.
- Do not add document type/template binding in v1.
- Do not expose a broad typed-fields public API in v1 unless explicitly chosen later.
- Do preserve string properties behavior through the existing API.
- Do preserve numeric/bool legacy scalar values in storage.
- Do preserve unknown non-scalar legacy metadata under `extra`.
- Do fail on malformed canonical comments.
- Do avoid any runtime fallback once canonical markers exist.
- Do make migration idempotent.
- Do ensure search/excerpt/body views do not include metadata comments.
- Do ensure all writes emit canonical comments only.
- Do update tests before implementation code.
