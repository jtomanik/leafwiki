<!-- leafwiki
version: 1
page:
  id: plan-markdown-link-root-prefix-observe
  title: Markdown Link Root Prefix - Observe
  created_at: "2026-06-15T14:30:00Z"
  updated_at: "2026-06-15T14:30:00Z"
  creator_id: codex
  last_author_id: codex
tags:
  - plans
  - observe
  - links
  - markdown
  - mcp
fields:
  plan: markdown-link-root-prefix
  workflow: planning.aibasic
-->

# Markdown Link Root Prefix - Observe

## Purpose

This document captures the observed facts behind the `markdown-link-root-prefix`
implementation plan. It follows the Observe step in
`docs/plans/planning.aibasic.txt`.

## Planning Input

The triggering MCP configuration runs LeafWiki with a data directory outside the
workspace root and with the wiki root set to the repository's `docs` directory:

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

Documentation in that repository uses GitHub-compatible absolute Markdown links
such as:

```markdown
[Glossary](/docs/sync/glossary.md)
```

Those links work on GitHub because the leading slash is resolved from the
repository root. LeafWiki currently resolves leading-slash Markdown links from
the configured wiki root. With `--root-dir .../docs`, LeafWiki interprets
`/docs/sync/glossary.md` as:

```text
.../docs/docs/sync/glossary.md
```

The actual target file is:

```text
.../docs/sync/glossary.md
```

## MCP Evidence

Running `wiki_validate_wiki` through the MCP server with the configuration above
reported:

```text
ok=false errors=1694 warnings=0
codes:
  1694 broken_link
prefixes:
  1694 /docs/
```

The first observed failures were all broken links with destinations under
`/docs/`, for example:

```text
wiki link does not resolve: /docs/sync/glossary.md
```

The target files exist under the configured root. The failures are therefore a
root model mismatch, not missing content and not a workspace route normalization
failure.

## Existing Discovery Artifact

The problem was moved from raw signal notes into:

- `docs/discovery/markdown-link-root-prefix.md`

That page captures the problem frame, the proxy-style "strip prefix before
internal resolution" model, and the agreed option name:

```text
markdown-link-root-prefix
```

## Decisions Already Made In Discussion

- Keep `--root-dir .../docs`.
- Keep Markdown links written as `/docs/sync/glossary.md`.
- Use explicit configuration rather than auto-detecting from `--root-dir`.
- Name the option `markdown-link-root-prefix`.
- Expose the option through CLI, environment, and YAML config:
  - `--markdown-link-root-prefix`
  - `LEAFWIKI_MARKDOWN_LINK_ROOT_PREFIX`
  - `markdown-link-root-prefix`
- The behavior is "strip this configured prefix before resolving an absolute
  internal Markdown link."
- With `markdown-link-root-prefix: /docs`, both `/docs/sync/glossary.md` and
  `/sync/glossary.md` should resolve inside LeafWiki.
- Generated absolute Markdown links should emit `/docs/...` when the option is
  set.
- Workspace sync canonical coercion should normalize internal absolute links to
  `/docs/...` when the option is set.
- Relative links are not affected.
- External links, protocol-relative URLs, `mailto:` links, and pure hash links
  are not affected.
- Asset links support the same prefix, for example `/docs/assets/foo.png`.
- Only one prefix is supported.
- No auto-detection from `--root-dir` basename.
- Changing the option participates in project daemon identity.
- The design must distinguish LeafWiki route paths from filesystem-shaped
  Markdown href paths.

## Relevant Codebase Observations

### Startup Configuration

- `cmd/leafwiki/main.go`
  - `writeUsage` lists CLI flags and environment variables.
  - `cliFlags`, `leafwikiRuntimeConfig`, and `registerFlags` define startup
    inputs.
  - `valueTakingFlagNames` and `configFileFlagNames` must know each
    value-taking config flag.
  - YAML config is flat, strict, scalar-only, and mutually exclusive with other
    CLI flags.
  - Runtime config is converted into project daemon config in
    `daemonConfigForRuntime`.

- `scripts/run.sh`
  - Wrapper parsing needs to know whether a flag takes a value.
  - Wrapper tests live in `scripts/test-run.sh`.

- `internal/projectdaemon/config.go`
  - `ConfigHash` and config comparison use the project daemon config struct.
  - Behavior-changing startup config belongs in daemon identity.

### Shared Markdown Link Resolver

- `internal/core/markdownlinks/markdownlinks.go`
  - `NewIndexFromRoot` builds page, section, and asset entries from a wiki root.
  - `Resolve` and `ResolveForMigration` resolve Markdown destinations.
  - `RewriteMarkdown` rewrites authored Markdown to canonical destinations.
  - `resolveFilesystemPath` currently treats a leading slash as wiki-root
    absolute by trimming the slash.
  - `formatHref` and `formatCanonicalHref` currently emit absolute canonical
    links as `/` plus the target path.

This is the central backend boundary for prefix stripping and prefixed canonical
output.

### Workspace Sync

- `internal/workspacesync/service.go`
  - `migrateCanonicalMarkdownLinksLockedWithRollback` builds a
    `markdownlinks.NewIndexFromRoot(s.rootDir)` and calls `index.RewriteMarkdown`
    for managed Markdown files.
  - If the link index emits unprefixed canonical links, workspace sync will
    rewrite `/docs/foo.md` back to `/foo.md`.
  - If the link index emits prefixed canonical links, sync can normalize legacy
    `/foo.md` to `/docs/foo.md`.

### Validation And Link Indexing

- `internal/core/markdownvalidation/use_cases.go`
  - Workspace validation builds a Markdown link index from the root directory.
  - Existing validation can report non-canonical or broken links based on
    resolver output.

- `internal/links/helpers.go`
  - Link service target resolution uses `markdownlinks.Index`.
  - Backlinks and outgoing-link health must use the same prefix-aware resolver as
    validation and sync.

### Generated Links

- `internal/importer/content_transformer.go`
  - Importer-generated Markdown destinations are currently absolute links built
    from resolved target route paths.
  - Input resolution for imported packages is separate from output formatting.

- `internal/links/link_refactor.go`
  - Refactor output has its own Markdown destination resolver and formatter.
  - Adding prefix behavior only to `markdownlinks.Index` would leave refactor
    output unprefixed.

- `ui/leafwiki-ui/src/lib/wikiPath.ts`
  - `markdownHrefToWikiRoutePath` resolves Markdown hrefs to internal route
    paths.
  - `markdownHrefToWikiBrowserPath` converts Markdown hrefs to browser routes.
  - `markdownHrefForWikiPath` generates Markdown links for editor surfaces.

- `ui/leafwiki-ui/src/features/editor/internalLinkCompletion.ts`
  - Autocomplete uses `markdownHrefForWikiPath`.

- `ui/leafwiki-ui/src/features/editor/LinkInsertDialog.tsx`
  - Insert dialog uses `markdownHrefForWikiPath`.

- `ui/leafwiki-ui/src/features/preview/MarkdownLink.tsx`
  - Preview navigation currently resolves absolute Markdown links without
    stripping a repository-root prefix.
  - Asset detection currently recognizes `assets/...` and `/assets/...`, so
    prefix stripping must happen before asset detection for `/docs/assets/...`.

### HTTP, MCP, And UI Config Output

- `internal/http/router.go`
  - Router options carry frontend/runtime config into HTTP handlers.

- `internal/wiki/auth/routes.go`
  - `/api/config` output needs to expose the prefix to the UI.

- `internal/wiki/mcp/tools_config.go`
  - `wiki_get_config` output should include the prefix for agent reasoning.

- `internal/wiki/mcp/schema.go`
  - MCP config schema must match `wiki_get_config` output.

- `ui/leafwiki-ui/src/lib/api/config.ts`
  - Frontend config types need the field.

- `ui/leafwiki-ui/src/stores/config.ts`
  - UI store needs to hold the field.

## Existing Test Anchors

Backend:

- `cmd/leafwiki/main_test.go`
- `internal/core/markdownlinks/markdownlinks_test.go`
- `internal/core/markdownvalidation/use_cases_test.go`
- `internal/workspacesync/service_test.go`
- `internal/links/link_service_test.go`
- `internal/links/link_refactor_test.go`
- `internal/importer/content_transformer_test.go`
- `internal/importer/executor_test.go`
- `internal/http/router_test.go`
- `internal/wiki/mcp/mcp_integration_test.go`
- `internal/plantrace/canonical_markdown_links_test.go`

Frontend and E2E:

- `e2e/tests/page.spec.ts`
- `e2e/tests/workspace-sync.spec.ts`
- `e2e/tests/root-dir.spec.ts`
- `e2e/tests/mcp-agent-context.spec.ts`
- `e2e/run.sh`

Documentation:

- `docs/discovery/markdown-link-root-prefix.md`
- `docs/plans/canonical_markdown_links.PLAN.md`
- `docs/plans/workspace-markdown-route-normalization.PLAN.md`
- `docs/plans/yaml-config.PLAN.md`
- `docs/plans/project_daemon.PLAN.md`
- `docs/README.md`
- `docs/mcp.md`
- `docs/workspace-sync.md`
- `scripts/README.md`

## Constraints

- The solution is a Markdown-link compatibility setting, not HTTP `base-path`.
- LeafWiki route identity stays unchanged.
- The option must not create broad path aliases across every API.
- Internal route/path APIs should remain explicit about whether they accept
  route paths or filesystem-shaped Markdown hrefs.
- The current UI package has `lint` and `build` scripts, but no frontend unit
  test script. Frontend behavior should therefore be verified through Playwright
  unless a separate test harness is introduced deliberately.
