<!-- leafwiki
version: 1
page:
  id: plan-markdown-link-root-prefix-context
  title: Markdown Link Root Prefix - Context
  created_at: "2026-06-15T14:30:00Z"
  updated_at: "2026-06-15T14:30:00Z"
  creator_id: codex
  last_author_id: codex
tags:
  - plans
  - context
  - links
  - markdown
  - mcp
fields:
  plan: markdown-link-root-prefix
  workflow: planning.aibasic
-->

# Markdown Link Root Prefix - Context

## Problem Frame

LeafWiki has two valid root concepts that currently get conflated:

- The configured wiki root directory, for example
  `/Users/jakubtomanik/github/nowatch-ios-pr/docs`.
- The Git repository root used by GitHub when resolving leading-slash Markdown
  links, for example `/Users/jakubtomanik/github/nowatch-ios-pr`.

When a repo stores docs under `docs/`, GitHub-compatible absolute Markdown links
usually include `/docs/...`. LeafWiki with `--root-dir .../docs` currently
resolves those same links as if `/docs` were inside the wiki root, producing
`docs/docs/...` lookups and broken-link errors.

The product goal is not to change the wiki root. The product goal is to let a
workspace keep GitHub-compatible filesystem-shaped Markdown links while LeafWiki
continues to operate on the `docs` subdirectory as its wiki boundary.

## Path Vocabulary

This plan uses four path concepts deliberately:

| Concept | Example | Meaning |
|---|---|---|
| Repository-root Markdown href | `/docs/sync/glossary.md` | Authored Markdown link that works on GitHub from repo root |
| Wiki-root filesystem path | `sync/glossary.md` | File path relative to the configured LeafWiki root |
| LeafWiki route path | `sync/glossary` | Internal page/section identity used by tree, HTTP, MCP, and UI state |
| Browser route path | `/sync/glossary.md` | LeafWiki app URL for a page route |

`markdown-link-root-prefix: /docs` means:

```text
Markdown href: /docs/sync/glossary.md
strip prefix:  /sync/glossary.md
resolve under: configured wiki root
route path:    sync/glossary
```

It does not mean:

- serve the LeafWiki app under `/docs`
- change HTTP `base-path`
- rename internal route paths to include `docs`
- make every MCP and HTTP page-path input accept repository-root filesystem
  paths

## Design Pressure

The behavior must be shared across several surfaces:

- validation must stop reporting `/docs/...` links as broken when targets exist
  under the wiki root
- workspace sync canonical coercion must emit the configured prefix for absolute
  internal links
- preview navigation must strip the prefix before route lookup
- editor autocomplete and insert dialogs must generate prefixed absolute links
- importer and refactor output must not generate unprefixed absolute links when
  the option is set
- link indexing/backlinks must agree with validation
- MCP and HTTP config output must expose the setting so agents and the UI can
  reason about path semantics
- daemon identity must change when the setting changes

The central backend resolver is `internal/core/markdownlinks`. The central
frontend helper is `ui/leafwiki-ui/src/lib/wikiPath.ts`. The plan should avoid
duplicating path-prefix logic in many call sites.

## Alternatives Considered

### Option A: Change `--root-dir` To The Repository Root

This makes `/docs/...` resolve naturally, but it changes the wiki boundary and
causes LeafWiki to scan unrelated repository Markdown. It also makes `docs` a
visible top-level wiki section instead of the wiki itself.

Decision: rejected.

### Option B: Rewrite Workspace Content To `/sync/...`

This satisfies LeafWiki with `--root-dir .../docs`, but it abandons the
repository-root absolute link style that works on GitHub.

Decision: rejected.

### Option C: Add General Path Aliases

LeafWiki could support an arbitrary list of aliases such as `/docs -> /` and
maybe more. That would solve this case, but it would introduce broad alias
machinery across routes, validation, links, and UI navigation.

Decision: rejected for v1.

### Option D: Strip One Explicit Markdown Link Root Prefix

Add one explicit configuration value. Apply it only to absolute internal
Markdown href interpretation and absolute Markdown href generation. Keep
internal route identity unchanged.

Decision: selected.

## Selected Model

The selected model is similar to reverse-proxy path rewriting, but scoped to
Markdown links:

```text
external Markdown href: /docs/sync/glossary.md
configured prefix:      /docs
internal Markdown href: /sync/glossary.md
LeafWiki route path:    sync/glossary
```

The option is named:

```text
markdown-link-root-prefix
```

Supported surfaces:

```text
--markdown-link-root-prefix /docs
LEAFWIKI_MARKDOWN_LINK_ROOT_PREFIX=/docs
markdown-link-root-prefix: /docs
```

## API Boundary

HTTP and MCP page-path inputs remain LeafWiki route-path inputs.

Examples:

- `wiki_get_page_by_path` uses `sync/glossary` or `/sync/glossary.md`.
- `GET /api/pages/by-path?path=sync/glossary&kind=page` stays route-oriented.
- Direct browser routes stay route-oriented, such as `/sync/glossary.md`.

Markdown-bearing inputs and outputs use filesystem-shaped Markdown href
semantics.

Examples:

- Markdown validation resolves `[Glossary](/docs/sync/glossary.md)`.
- Workspace sync rewrites `[Glossary](/sync/glossary.md)` to
  `[Glossary](/docs/sync/glossary.md)` when the option is set.
- Editor autocomplete inserts `/docs/sync/glossary.md` for a page.
- Preview renders a link that navigates to the LeafWiki route for
  `sync/glossary`.

This preserves a clear boundary:

- route APIs accept route paths
- Markdown processors accept Markdown hrefs
- generated Markdown emits the configured filesystem-shaped href format

## Canonicalization Policy

When the option is set:

- `/docs/sync/glossary.md` resolves and is canonical for an absolute page link.
- `/sync/glossary.md` also resolves, but canonical rewrite output is
  `/docs/sync/glossary.md`.
- `/docs` and `/docs/` resolve to the wiki root section.
- `/docs/sync` resolves as a section.
- `/docs/sync.md` resolves as a page.
- `/docs/assets/foo.png` resolves as the same asset destination as
  `/assets/foo.png`.

Relative links such as `../glossary.md` continue to resolve relative to the
source Markdown file and do not receive the prefix unless an existing absolute
formatter is already producing absolute output.

## Configuration Validation

The normalized stored value should be either an empty string or an absolute
path prefix such as `/docs`.

Accept:

- empty value
- `docs`, normalized to `/docs`
- `/docs`, stored as `/docs`
- `/docs/`, stored as `/docs`
- `/published/docs`, stored as `/published/docs`

Reject:

- `/`
- `.`
- `..`
- values containing `..` path segments
- values containing query strings or fragments
- values with URL schemes
- values with backslashes
- values that escape clean path normalization

The setting should be stored and reported in normalized form.

## Risk Analysis

- Backend/frontend divergence: if only backend validation strips the prefix,
  preview links remain broken.
- Generated-link drift: if autocomplete and refactor still emit `/sync/...`,
  workspace sync will keep rewriting recent edits.
- Daemon identity drift: if daemon config does not include the prefix, a running
  daemon can serve stale semantics for the same data/root pair.
- Asset ambiguity: `/docs/assets/foo.png` must become an asset destination, not
  a wiki page lookup for `docs/assets/foo`.
- Base-path confusion: HTTP `base-path` and Markdown link root prefix can both
  be configured and must remain independent.
- API ambiguity: accepting prefixed repository-root paths in every page-path API
  would make route identity harder to reason about.
- Importer/refactor inconsistency: both already generate Markdown links through
  their own code paths and must share the output policy.

## Planning Assumption

The first implementation should favor one explicit prefix and one shared
normalization helper over a generalized alias system. That keeps the v1 contract
practical while leaving room for a future alias layer if a separate discovery
page proves it is needed.
