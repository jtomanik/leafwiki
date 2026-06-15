<!-- leafwiki
version: 1
page:
  id: discovery-markdown-link-root-prefix
  title: Markdown Link Root Prefix
  created_at: "2026-06-15T13:41:55Z"
  updated_at: "2026-06-15T13:54:23Z"
  creator_id: codex
  last_author_id: codex
tags:
  - discovery
  - links
  - markdown
  - workspace-sync
  - mcp
fields:
  created_from: codex-discussion
  status: working-notes
  topic: markdown-link-root-prefix
-->

# Markdown Link Root Prefix

Working notes from the June 2026 investigation into GitHub-compatible absolute
Markdown links in workspaces whose LeafWiki root directory is a subdirectory of
the repository. This is not an implementation plan. It captures the problem,
the current model, what seems agreed, and the questions that still need design
review.

## Problem

Some documentation repositories use absolute Markdown links rooted at the Git
repository, for example:

```markdown
[Glossary](/docs/sync/glossary.md)
```

Those links work on GitHub because GitHub resolves leading-slash Markdown links
from the repository root. In the observed LeafWiki MCP configuration, the wiki
root is already the repository's `docs` directory:

```json
{
  "command": "/opt/homebrew/bin/run.sh",
  "args": [
    "mcp",
    "--data-dir",
    "/Users/jakubtomanik/github/nowatch-ios-pr/.wiki",
    "--root-dir",
    "/Users/jakubtomanik/github/nowatch-ios-pr/docs"
  ]
}
```

LeafWiki currently resolves an absolute Markdown link from the configured wiki
root. With `--root-dir .../docs`, `/docs/sync/glossary.md` is interpreted as:

```text
/Users/jakubtomanik/github/nowatch-ios-pr/docs/docs/sync/glossary.md
```

The actual target file is:

```text
/Users/jakubtomanik/github/nowatch-ios-pr/docs/sync/glossary.md
```

The result is a large number of broken links even though the files exist and
the filenames are already valid under the current workspace route mapper.

## Evidence

Running `wiki_validate_wiki` through the MCP server with the configuration above
reported:

```text
ok=false errors=1694 warnings=0
codes:
  1694 broken_link
prefixes:
  1694 /docs/
```

The first observed failures all had `code: broken_link`, for example:

```text
wiki link does not resolve: /docs/sync/glossary.md
```

The target file exists under the configured root at `sync/glossary.md`, so the
failure is not a missing-file problem. It is a root-model mismatch:

```text
GitHub absolute link root: repository root
LeafWiki absolute link root: configured wiki root
```

## What We Learned

- Workspace Markdown route normalization is not the failing layer. The files
  are imported and mapped as routes such as `sync/glossary`.
- Canonical Markdown link support correctly makes `.md` page links valid, but
  it does not make `/docs/...` a special alias for `/...`.
- The current route normalization plan intentionally avoided long-lived aliases
  from raw or external path conventions.
- Rewriting the docs to `/sync/glossary.md` would satisfy this LeafWiki
  configuration, but would abandon the repository-root absolute link style that
  works on GitHub.
- Changing `--root-dir` to the repository root would make `/docs/...` resolve
  naturally, but it changes the wiki boundary and risks scanning unrelated
  repository Markdown.

## Emerging Model

The closest familiar model is proxy path rewriting:

```text
external path: /docs/sync/glossary.md
internal path: /sync/glossary.md
operation: strip prefix /docs before resolving inside LeafWiki
```

For LeafWiki, this should be a Markdown-link compatibility setting, not an HTTP
`base-path` setting. HTTP `base-path` describes where the app is served. This
problem is about how Markdown hrefs are interpreted against the filesystem and
route index.

A possible option shape is:

```yaml
markdown-link-root-prefix: /docs
```

with equivalent CLI and environment surfaces:

```text
--markdown-link-root-prefix /docs
LEAFWIKI_MARKDOWN_LINK_ROOT_PREFIX=/docs
```

When set, LeafWiki would treat `/docs/sync/glossary.md` as a repo-root-style
absolute link and resolve it to the configured root path `sync/glossary.md`.

## Product Constraint

The solution needs to keep both of these true:

- LeafWiki can continue to run with `--root-dir .../docs`.
- Markdown links written as `/docs/sync/glossary.md` continue to work.

This is important for workspaces copied from repositories that expect their
documentation links to remain useful on GitHub and in LeafWiki. Both navigation
models must work at the same time:

- Repository-root navigation when displaying pages in GitHub.
- Wiki-root navigation when displaying pages in the LeafWiki frontend.

## What Seems Agreed

- The option name should be `markdown-link-root-prefix`.
- The feature should be explicit configuration, not auto-detected from the
  basename of `--root-dir`.
- The option should be exposed like other startup settings: CLI flag,
  environment variable, and YAML config field.
- The first version should support one root prefix, not a list of aliases.
- The behavior is "strip this configured prefix before resolving an
  absolute internal Markdown link."
- With `markdown-link-root-prefix: /docs`, both `/docs/sync/glossary.md` and
  `/sync/glossary.md` should resolve inside LeafWiki when the wiki root points
  at the repository's `docs` directory.
- `/docs` and `/docs/` should resolve to the wiki root section `/`.
- `/docs/sync` should resolve as a section.
- `/docs/sync.md` should resolve as a page.
- Relative Markdown links should not be affected.
- External URLs, `mailto:` links, and pure hash links should not be affected.
- Asset links should support the same prefix behavior, for example
  `/docs/assets/foo.png` should resolve to `assets/foo.png` under the configured
  root when the prefix is `/docs`.
- The option should be separate from HTTP `base-path`.
- Generated absolute Markdown links should emit the configured prefix. If the
  option is `/docs`, new absolute page links should be written as
  `/docs/sync/glossary.md`, not `/sync/glossary.md`.
- Workspace sync already performs automatic Markdown link coercion when file
  changes are detected. With this option enabled, that coercion should normalize
  internal absolute links to the prefixed filesystem-shaped form, for example
  `/docs/sync/foo.md`.
- Changing this option should participate in project daemon identity for a
  `(data-dir, root-dir)` pair because it changes validation, preview, and
  workspace-sync behavior.
- The design should distinguish LeafWiki route paths from filesystem-shaped
  Markdown link paths and be explicit about which API accepts or emits which
  path model.

## Open Questions

- Which public HTTP and MCP inputs are LeafWiki route paths, and which are
  filesystem-shaped Markdown link paths?
- Should MCP and HTTP path inputs such as `wiki_get_page_by_path` accept
  prefixed filesystem-shaped paths, or should they stay route-path-only while
  Markdown/preview surfaces handle the prefix?
- What should validation report when both `/docs/foo.md` and `/foo.md` resolve
  to the same page: no issue, warning, or style-specific canonicalization?
- How should documentation name and explain the distinction between LeafWiki
  paths and filesystem-shaped Markdown links so users can predict behavior?

## Design Bias

Use a narrow practical first version that handles the observed GitHub-compatible
docs case without broad alias machinery.

A reasonable direction is:

1. Add an explicit Markdown link root prefix config value.
2. Apply it at the shared Markdown link resolver boundary before filesystem
   route lookup.
3. Emit the configured prefix from generated absolute Markdown links and
   workspace-sync link coercion.
4. Keep internal LeafWiki route identity unchanged.
5. Preserve relative links and external links exactly.
6. Add MCP validation and preview/navigation coverage using a root directory
   that points at `docs` and content that links through `/docs/...`.
