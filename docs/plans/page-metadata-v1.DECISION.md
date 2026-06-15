# Page Metadata V1 - Decision

## Decision

Implement Page Metadata V1 as a shared internal page document model plus a canonical top-of-file HTML comment metadata codec. Legacy YAML frontmatter is migration input only and must not remain a supported runtime storage format after canonicalization.

## Selected Course of Action

1. Introduce a storage/domain boundary in `internal/core/markdown`:
   - `PageDocument` or equivalent: body plus metadata.
   - `PageMetadata` or equivalent: v1 schema representation.
   - `FieldValue` or equivalent: string/number/bool scalar values.
   - Parser result indicating whether migration/writeback is required.

2. Add a canonical metadata codec:
   - Opening marker exactly `<!-- leafwiki`.
   - YAML payload.
   - Closing marker exactly `-->`.
   - Top-of-file only.
   - Deterministic render.
   - One blank line before body when body exists.

3. Add a legacy frontmatter migrator:
   - Read legacy frontmatter only when it is the incoming storage form.
   - Map managed `leafwiki_*` fields to `page`.
   - Map `tags` to `tags`.
   - Map unknown scalar string/number/bool values to `fields`.
   - Map unknown non-scalars to `extra`.
   - Reject malformed legacy YAML.

4. Update all storage and indexing callers to use the shared model:
   - Tree reconstruction/writeback.
   - Page create/update/import.
   - Tags/properties/search/excerpt side effects.
   - Markdown validation.
   - Workspace sync.
   - Revision capture/restore.
   - HTTP and MCP metadata edits.
   - Importer transforms.

5. Keep API naming stable:
   - Existing `tags` and `properties` surfaces remain.
   - Existing `properties` remains string-oriented in v1.
   - Number/bool `fields` are preserved in storage and internal metadata, but not newly exposed as editable typed fields.

6. Update tests first and drive implementation from the failing tests.

## Decision Table

| Topic | Decision | Reason |
|---|---|---|
| Internal model | Explicit body plus metadata model | Removes storage/body conflation and localizes future storage changes |
| Canonical storage | HTML comment containing YAML | Hidden in GitHub render, one-file atomicity, easy parser boundary |
| Metadata position | Top of file | Easy discovery, one-pass parsing, consistent with current reconstruction |
| Comment markers | Strict exact lines | Stable parsing and fewer false positives |
| Schema version | Top-level `version: 1` | Enables future migration without adding current complexity |
| Schema sections | `page`, `tags`, `fields`, `extra` | Namespaced current behavior without adding template/document features |
| Field values | string/number/bool | Future-friendly storage; current public properties can remain string-only |
| Legacy frontmatter | Migration input only | Avoids dual-format runtime complexity |
| Mixed canonical plus legacy | Canonical wins; strip legacy on writeback | Removes duplicate metadata sources |
| Malformed canonical comment | Hard failure | Canonical marker means storage opted into strict v1 |
| Malformed legacy frontmatter | Migration failure | Avoids guessing identity or losing metadata |
| Sidecar storage | Deferred | Clean long term, too much atomicity/sync/revision complexity now |
| Document/template binding | Deferred | Expected soon, but requirements not settled |

## Alternatives Rejected

### Keep YAML Frontmatter

Rejected because it does not solve GitHub rendering and keeps internal model pollution.

### Backmatter

Rejected because it is still visible content and makes parsing less direct.

### Sidecar in V1

Deferred because it forces paired-file behavior for rename/delete/conflict/revision/restore before the metadata model boundary is proven.

### Permanent Dual Codec Runtime Support

Rejected because it would make every caller reason about two canonical formats. Legacy input should be canonicalized at the edges.

### Add Template/Document Type Now

Rejected because the user explicitly asked not to add `document` classification/template binding yet.

## Resolved Open Questions

- Q: Should metadata be separated from Markdown documents internally?
  - A: Yes. Body and metadata should be explicit model concepts.

- Q: Is storage format flexibility free after this refactor?
  - A: No. It becomes cheaper, not free.

- Q: HTML comment or sidecar first?
  - A: HTML comment first. Sidecar later after model boundary is proven.

- Q: Should legacy frontmatter support remain?
  - A: No. It should be migration input only.

- Q: Where is canonical metadata stored?
  - A: Top-of-file strict HTML comment containing YAML.

- Q: What metadata moves?
  - A: All LeafWiki-managed metadata, tags, and current custom properties/fields.

- Q: What happens to unknown legacy fields?
  - A: Unknown scalar string/number/bool values go to `fields`; unknown non-scalars go to `extra`.

- Q: Are `leafwiki_*` fields allowed in `fields`?
  - A: No.

- Q: What happens if canonical comment is malformed?
  - A: Fail ingestion/validation. No fallback.

- Q: What happens if canonical comment and legacy frontmatter both exist?
  - A: Canonical wins; legacy metadata is removed on migration/writeback.

- Q: Should body be cleaned up after stripping legacy metadata?
  - A: Yes. Render one blank line between metadata comment and body when body exists.

- Q: Should API naming change now?
  - A: No. Keep API naming unchanged. Storage schema can use snake_case.

## Implementation Posture

This is a refactor with behavior change, not a cosmetic serializer swap. The implementation agent should:

- Start with failing parser/migrator tests.
- Update core storage before caller patches.
- Remove runtime frontmatter assumptions instead of wrapping them.
- Preserve existing API behavior unless the plan explicitly changes it.
- Treat any remaining direct runtime `ParseFrontmatter` use as suspicious unless it is inside the legacy migration codec or tests.
