<!-- leafwiki
version: 1
page:
  id: plan-markdown-link-root-prefix-decision
  title: Markdown Link Root Prefix - Decision
  created_at: "2026-06-15T14:30:00Z"
  updated_at: "2026-06-15T14:30:00Z"
  creator_id: codex
  last_author_id: codex
tags:
  - plans
  - decision
  - links
  - markdown
  - mcp
fields:
  plan: markdown-link-root-prefix
  workflow: planning.aibasic
-->

# Markdown Link Root Prefix - Decision

## Decision Summary

LeafWiki will add `markdown-link-root-prefix` as a single explicit startup
configuration option. The option strips a configured repository-root prefix from
absolute internal Markdown hrefs before resolving them against the configured
wiki root, and it adds that prefix when LeafWiki generates canonical absolute
Markdown hrefs.

Internal LeafWiki route identity remains unchanged.

## Key Decisions

| Decision | Rationale |
|---|---|
| Add `markdown-link-root-prefix` | The name describes the problem without confusing it with HTTP `base-path`. |
| Expose CLI, env, and YAML surfaces | The option must behave like other startup config and work in MCP wrapper flows. |
| Normalize and validate the prefix at startup | All downstream surfaces should receive one canonical value. |
| Support exactly one prefix | The observed problem needs `/docs -> /`; multiple aliases are broader machinery. |
| Do not auto-detect from `--root-dir` | Auto-detection would surprise repos that intentionally use different link conventions. |
| Strip the prefix only for absolute internal Markdown hrefs | Relative links and external URLs already have correct semantics. |
| Emit the prefix for generated absolute Markdown hrefs | LeafWiki should preserve GitHub-compatible repo-root links instead of slowly rewriting docs away from that style. |
| Keep route APIs route-oriented | `wiki_get_page_by_path` and HTTP page lookup should stay clear route APIs, not accept every authored Markdown href convention. |
| Expose the setting in HTTP and MCP config output | Agents and frontend code need to know the active Markdown href model. |
| Include the setting in project daemon identity | The value changes validation, preview, sync, and generated-link behavior. |
| Apply the same option to assets | `/docs/assets/foo.png` should resolve to the existing asset destination model. |
| Keep `base-path` separate | Browser mount path and authored Markdown href prefix solve different problems. |

## Alternatives Rejected

- Change the wiki root to the repository root: rejected because it changes the
  workspace boundary and scans unrelated repository Markdown.
- Rewrite all docs to `/sync/...`: rejected because it breaks the
  repository-root link style that works on GitHub.
- Add a general alias table: rejected for v1 because the requested behavior is
  one prefix, not a full routing alias system.
- Make `/docs/...` a public LeafWiki route alias: rejected because route
  identity should remain stable and route APIs should remain route-oriented.
- Auto-detect `/docs` from `--root-dir` basename: rejected because it makes
  behavior implicit and hard to reason about.

## Open Questions Resolved

- Q: Is this a "strip this prefix before resolving internal Markdown links"
  option?
  - A: Yes. `markdown-link-root-prefix: /docs` means
    `/docs/sync/glossary.md` resolves as `/sync/glossary.md` inside LeafWiki.

- Q: Should both GitHub repo-root navigation and LeafWiki wiki-root navigation
  work?
  - A: Yes. `/docs/sync/glossary.md` remains valid authored Markdown, and
    LeafWiki resolves it against the configured wiki root.

- Q: When LeafWiki creates, autocompletes, imports, or refactors an absolute
  page link, should it emit `/docs/sync/glossary.md` or
  `/sync/glossary.md`?
  - A: Emit `/docs/...` when the option is set.

- Q: If existing content uses `/sync/foo.md`, should refactor preserve that
  style or normalize it to `/docs/sync/foo.md`?
  - A: Normalize absolute internal links to `/docs/...` when the option is set.

- Q: Should this apply to API and MCP path inputs like `wiki_get_page_by_path`?
  - A: No for route APIs. Those inputs remain LeafWiki route paths. Markdown
    validation, preview links, importer output, refactor output, and sync
    coercion use Markdown href semantics.

- Q: Should this exist in CLI, env, and YAML?
  - A: Yes: `--markdown-link-root-prefix`,
    `LEAFWIKI_MARKDOWN_LINK_ROOT_PREFIX`, and `markdown-link-root-prefix`.

- Q: Should changing this option create a distinct project daemon identity?
  - A: Yes.

- Q: Should `/docs` itself resolve to wiki root `/`?
  - A: Yes.

- Q: Should `/docs/sync` resolve as a section and `/docs/sync.md` as a page?
  - A: Yes.

- Q: Should relative links like `../glossary.md` be affected?
  - A: No.

- Q: Should external links like `https://example.com/docs/...` be affected?
  - A: No.

- Q: Should assets support the same prefix?
  - A: Yes. `/docs/assets/foo.png` resolves as `/assets/foo.png`.

- Q: Should multiple prefixes be supported?
  - A: No.

- Q: Should the option be auto-detected from the `--root-dir` basename?
  - A: No.

## Deferred Questions

The following are intentionally outside this v1 plan:

- Whether LeafWiki should ever support a general alias table for authored links.
- Whether route APIs should gain a separate "resolve Markdown href" tool.
- Whether direct browser routes such as `/docs/sync/glossary.md` should become
  public aliases. The selected v1 behavior is preview/navigation normalization,
  not route aliasing.
- Whether static files outside the managed `/assets` model should gain broader
  first-class support.
