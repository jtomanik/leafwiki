<!-- leafwiki
version: 1
page:
  id: Rbh-uSaDR
  title: Page Metadata V1 - Context
  created_at: "2026-06-15T05:44:44.856796079Z"
  updated_at: "2026-06-15T05:44:44.856796079Z"
  creator_id: system
  last_author_id: system
-->

# Page Metadata V1 - Context

## Problem Frame

LeafWiki currently stores page metadata as YAML frontmatter at the top of Markdown files. That works inside LeafWiki, but it is visible when rendered by tools like GitHub. The result is that public Markdown output starts with internal storage details instead of document content.

The core architectural issue is that two concepts are currently entangled:

- Markdown document body: content a human expects to read and render.
- LeafWiki metadata: storage and application state needed for IDs, timestamps, tags, properties, and future structured features.

The agreed goal is to separate these concepts internally and change canonical storage from visible frontmatter to a hidden top-of-file HTML comment containing YAML.

The guiding principle for this plan is: avoid overbuilding now, but also avoid painting the project into a corner later.

## Why the Model Boundary Comes First

Changing only the storage format would be tempting: parse frontmatter, write comment metadata, and patch callers as they fail. That would produce a brittle migration because many subsystems currently parse `RawContent` directly.

The better v1 is still small, but it must establish a single model boundary:

```text
Stored Markdown bytes
  -> parse/migrate once
  -> PageDocument{Body, Metadata}
  -> domain/API/index/revision behavior
  -> render canonical storage bytes
```

This does not make future storage changes free. It makes them localized. A later sidecar format should mostly change the storage codec and sync semantics, not every tag/property/MCP/importer path.

## Options Considered

### Keep YAML Frontmatter

Rejected.

It preserves current code shape but fails the user-facing goal. Metadata remains visible in GitHub-rendered Markdown, and internal callers keep conflating storage bytes with body content.

### Put YAML Frontmatter at the End

Rejected for v1.

Backmatter keeps metadata out of the first rendered lines, but it is still visible Markdown content in many renderers and worse for one-pass parsing. It also creates ambiguous rules about whether trailing YAML is content or metadata.

### Separate Sidecar File

Deferred.

Sidecar storage is conceptually clean and may be the right long-term format for some workflows. It also creates immediate complexity:

- Rename/move/delete operations must keep `.md` and `.yaml` in sync.
- Git conflicts can split document and metadata state.
- Import/export needs paired-file semantics.
- Workspace sync must reason about partial changes.
- Revision restore must restore two files atomically.

The current codebase is not yet cleanly separated enough to absorb those costs safely. Comment metadata gives the project a one-file canonical format while proving the model boundary.

### HTML Comment Metadata

Chosen.

This keeps metadata in the Markdown file for atomicity, hides it from GitHub rendered output, and allows the existing filesystem/revision model to stay one-file-per-page. It is the easiest storage change that still forces the right internal split.

### Runtime Support for Frontmatter and Comments

Rejected.

Long-term dual-format support would keep storage rules ambiguous and make every caller ask which format it is handling. Legacy frontmatter should be accepted only by ingestion/import/migration paths and rewritten to canonical comment metadata.

This mirrors the canonical Markdown links plan: old syntax is accepted as migration input, canonical output is the only format LeafWiki writes.

### Add Document Type/Template Binding Now

Rejected for v1.

The project is likely to add issue-template-like document templates and document types soon. The v1 schema should leave explicit room for that, but should not introduce a `document` section or template binding before the requirements are concrete.

## Canonical Storage Shape

The canonical file starts with an exact LeafWiki HTML comment marker, then YAML, then an exact closing marker:

```markdown
<!-- leafwiki
version: 1
page:
  id: page-id
  title: Page Title
  created_at: 2026-06-13T10:00:00Z
  updated_at: 2026-06-13T11:00:00Z
  creator_id: user-id
  last_author_id: user-id
tags:
  - research
fields:
  status: draft
  priority: 2
  published: false
extra:
  aliases:
    - old-title
-->

# Page Title

Renderable Markdown body starts here.
```

Rules:

- Opening line is exactly `<!-- leafwiki`.
- Closing line is exactly `-->`.
- The canonical comment must be at the top of the file.
- The renderer emits one blank line between the closing marker and the body when body is non-empty.
- The YAML schema version is top-level `version: 1`.
- `page`, `tags`, `fields`, and `extra` are top-level sections.
- `document` is intentionally not present in v1.
- Public Markdown body starts after the canonical metadata block.

## Schema Interpretation

### `page`

Managed LeafWiki page identity and authorship metadata. This maps current `leafwiki_*` frontmatter keys into a namespaced section.

Expected mapping:

| Current frontmatter key | V1 schema path |
|---|---|
| `leafwiki_id` | `page.id` |
| `leafwiki_title` | `page.title` |
| `title` alias | `page.title` when no managed title exists |
| `leafwiki_created_at` | `page.created_at` |
| `leafwiki_updated_at` | `page.updated_at` |
| `leafwiki_creator_id` | `page.creator_id` |
| `leafwiki_last_author_id` | `page.last_author_id` |

### `tags`

Current top-level legacy `tags` list moves to canonical `tags`.

### `fields`

Current unknown scalar legacy frontmatter moves to canonical `fields`.

Allowed v1 field value types:

- string
- number
- bool

Not allowed in `fields`:

- list
- map/object
- null
- keys starting with `leafwiki_`

V1 public API behavior remains string-property oriented unless a later typed-fields API is added. That means:

- Existing `properties` API and UI keep working for string fields.
- Number and bool fields are preserved losslessly in storage and internal metadata.
- Number and bool fields are not made newly editable/queryable through the existing string `properties` surface in this plan.

### `extra`

Preservation area for metadata LeafWiki does not understand yet.

Legacy unknown non-scalar values move to `extra`. Legacy keys that collide with canonical top-level names but are not recognized as v1 schema input should also be preserved under `extra` rather than promoted into first-class schema fields accidentally.

`extra` is not a plugin framework. It is a lossless migration/preservation bucket for v1.

## Migration Semantics

### Legacy Frontmatter Only

When ingestion/import/save receives a Markdown file whose first line is `---`, LeafWiki should:

1. Parse the legacy frontmatter.
2. Build v1 metadata.
3. Strip the legacy frontmatter from body.
4. Render canonical HTML comment metadata.
5. Preserve body content.
6. Write back canonical form through the owning save/sync path.

Malformed legacy YAML is a migration failure. The caller should surface the failure and avoid silently generating replacement metadata.

### Canonical Comment Only

When a file starts with the canonical marker, LeafWiki should parse only canonical metadata.

Malformed canonical YAML or schema errors are hard failures. There is no fallback to frontmatter once the canonical marker exists.

### Canonical Comment plus Legacy Frontmatter

Canonical metadata wins. If the body immediately after the canonical comment contains a valid legacy frontmatter block, migration/writeback strips that legacy block and preserves canonical metadata.

This handles mixed transitional files without making frontmatter a second canonical source.

### No Metadata

For unmanaged Markdown files entering LeafWiki through root-dir reconstruction or import, LeafWiki may generate required managed metadata and write canonical comment metadata. This is equivalent to today's managed frontmatter writeback, but with the new storage format.

## Revision and Sync Semantics

The canonical link migration precedent applies:

- Capture raw incoming file bytes before mutation.
- Parse/migrate metadata.
- Write canonical metadata as an automatic writeback.
- Capture the writeback in revision/sync history.
- Re-run derived indexes from canonical parsed state.

This matters because metadata is identity-sensitive. Legacy `leafwiki_id` must be read before any reconstruction path can decide a page is missing an ID and create a new one.

## API and UI Context

The user agreed that API naming can remain unchanged while storage schema uses snake_case. Therefore:

- Existing HTTP/MCP `tags` and `properties` names remain.
- Existing DTOs can keep camelCase where already exposed.
- UI can still expose tags and text properties.
- Frontend copy that says "frontmatter" should be updated to "metadata" where user-facing.

This plan does not require a full typed fields UI. It requires preserving typed scalar fields internally so future template/document-type work is not boxed into string-only storage.

## Risk Analysis

### RawContent Leakage

Many components parse raw storage content directly. If any direct `ParseFrontmatter` call remains in runtime behavior, one of these bugs can occur:

- Metadata comments leak into search/excerpt output.
- Tags/properties become stale.
- Metadata updates reintroduce YAML frontmatter.
- Revision restore emits old storage.
- Workspace sync cannot match page IDs.

The final implementation should route metadata parsing through the new shared codec.

### Identity Loss

Metadata stores page IDs. A migration that strips or ignores legacy frontmatter before extracting IDs can create duplicate pages or new IDs for existing pages. This must be covered by reconstruction and workspace sync tests.

### Typed Scalar Drift

Storage will support numbers and bools, but current APIs are string-oriented. The implementation must not accidentally stringify and rewrite typed values when the user edits body, tags, or string properties.

### Revision Churn

Migration changes file bytes. That is expected, but revision tests should pin the behavior: raw incoming state and canonical writeback are both visible according to the sync/revision rules.

## Chosen Orientation

The smallest useful architecture change is:

1. Add a shared `PageMetadata` and `PageDocument` model under `internal/core/markdown`.
2. Add a canonical comment YAML codec and legacy frontmatter migrator there.
3. Update tree reconstruction and all write paths to use that model.
4. Update derived indexers, validation, importer, revisions, workspace sync, HTTP, MCP, and UI naming around that model.
5. Keep public API shape stable while preserving typed scalar storage internally.

This is not a storage-only patch. It is a storage refactor with a deliberately narrow public behavior change.
