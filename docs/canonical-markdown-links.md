<!-- leafwiki
version: 1
page:
  id: WECO24aDR
  title: Canonical Markdown Links
  created_at: "2026-06-13T20:11:32.481641055Z"
  updated_at: "2026-06-13T20:11:32.481641055Z"
  creator_id: system
  last_author_id: system
-->

# Canonical Markdown Links

LeafWiki keeps internal route identity extensionless, but Markdown stored on disk
uses filesystem-portable links:

- Pages link to Markdown files: `/docs/guide.md`, `../guide.md`.
- Sections link to folders: `/docs`, `../docs`.
- Section links omit trailing slashes in generated output. A trailing slash is
  accepted as input and canonicalized away.
- Query strings and fragments are preserved. LeafWiki does not validate heading
  fragments as part of link canonicalization.

## Sections

New sections are written with `index.md`.

When reconstructing existing Markdown from disk or importing external content,
LeafWiki treats `index.md` as the section content file. If `index.md` is absent,
`README.md` is accepted as a fallback section content file. When both files
exist, `index.md` is the section content and `README.md` remains a normal page.

## Migration

Workspace sync rewrites resolvable legacy page links during ingestion. For
example, `[Guide](/docs/guide)` becomes `[Guide](/docs/guide.md)` when
`docs/guide.md` exists. Section links such as `/docs` remain extensionless.

Unresolved legacy extensionless page links are not guessed. They remain in the
Markdown and validation reports them as errors so the author can decide whether
the target should be a page, a section, or something else.

## Route Inputs

HTTP and MCP page lookup tools accept route paths such as `/docs/guide` or
`docs/guide`. Path-based page lookup, validation, and subtree tools also accept
canonical Markdown file paths where the file suffix carries the kind:
`/docs/guide.md` resolves the page, `/docs/guide/index.md` resolves the section,
and an active `/docs/README.md` fallback resolves the section it backs. Pure
route-structure tools such as `wiki_lookup_path` still operate on route paths;
use their explicit `kind` argument when page and section twins share a route.
Markdown content should continue to use LeafWiki's canonical href format.
