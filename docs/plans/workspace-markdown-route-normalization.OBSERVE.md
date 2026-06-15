<!-- leafwiki
version: 1
page:
  id: plan-workspace-route-observe
  title: Workspace Markdown Route Normalization - Observe
  created_at: "2026-06-14T18:41:17Z"
  updated_at: "2026-06-14T18:54:52Z"
  creator_id: system
  last_author_id: system
-->

# Workspace Markdown Route Normalization - Observe

## Purpose

This document captures the observed facts behind the workspace Markdown route normalization plan. It follows the observation step in `docs/plans/planning.aibasic.txt`.

## Planning Input

Plans were moved from `plans/` to `docs/plans/` so they can be exposed through LeafWiki and the LLMWiki MCP server. After the move, workspace sync reports Markdown import errors. The visible browser symptom on `http://127.0.0.1:8080/plans` is:

- `Workspace synced, but some Markdown files could not be loaded.`
- `27 issues`
- `plans/agent_hooks.PLAN.md`: `slug must contain only letters, numbers and hyphens`
- `assets`: `slug 'assets' is reserved`

The `plans` route is available as a section, but it is empty because child plan files are rejected before becoming pages.

## Live MCP Evidence

`wiki_get_context` with `syncMode: auto` reports:

- Workspace sync enabled and watcher running.
- Active web session on `/plans`.
- Sync validation errors include `invalid_slug` for `assets` and 26 plan Markdown files under `plans/`.
- `plans` appears in the tree as a section with no imported children.
- Recommended tools include `wiki_validate_wiki`, `wiki_get_subtree`, `wiki_search_pages`, and `wiki_refresh`.

Earlier direct validation also showed that MCP validation and sync status can differ. `wiki_validate_wiki` may rescan the root and include additional missing asset findings, while `wiki_get_context` and UI status reflect stored workspace sync status.

## Browser Evidence

The in-app browser at `/plans` shows:

- Sidebar status card: `Workspace synced, but some Markdown files could not be loaded.`
- Expanded issue list:
  - `assets`: `slug 'assets' is reserved`
  - `plans/agent_hooks.PLAN.md`: `slug must contain only letters, numbers and hyphens`
  - same slug error for `api_keys.PLAN.md`, `canonical_markdown_links.PLAN.md`, `page-metadata-v1.PLAN.md`, and the other moved plan files
- Main content: `This section is empty.`

The UI issue list comes from `GET /api/workspace-sync/status` and renders `path` plus `message` in `ui/leafwiki-ui/src/features/tree/TreeView.tsx`.

## Additional Root README Evidence

After the initial plan-file import issue was observed, a related README symptom was checked:

- `README.md` exists at the repository root.
- `docs/README.md` also exists.
- `docs/index.md` does not exist.
- The local wiki root is `docs`, so repository-root `README.md` is outside the workspace import root.
- MCP context reports the workspace path `README.md` as changed with `root` as the affected page id.
- The tree root title and content come from `docs/README.md`, confirming that root `README.md` is treated as root section content.
- `wiki_get_page_by_path` for `README` and `README.md` does not return a child page, which is consistent with section-sentinel behavior when no `index.md` exists.
- Copying the repository README into `docs/README.md` introduces broken wiki links for repo-root-style hrefs such as `docs/workspace-sync.md`, because the wiki root is already `docs`.

The backend root README fallback appears to work, but the frontend has a navigation gap:

- `ui/leafwiki-ui/src/features/router/router.tsx` routes `/` to `RootRedirect`.
- `ui/leafwiki-ui/src/features/page/RootRedirect.tsx` redirects `/` to the first root child whenever the tree has children.
- The Explorer sidebar renders `tree.children` and has no root node or Home control.
- The top-left logo/title links to `/`, but that currently follows the same redirect and does not expose the root README.

Therefore a root README can exist as main wiki content while still being hard or impossible to view through the frontend.

## Current Code Paths

### Workspace Sync

- `internal/workspacesync/service.go`
  - `SyncNow` captures current Markdown state, calls `TreeService.ReconstructTreeFromFS`, then validates via `validateAndRunAfterSyncLocked`.
  - `validateWorkspaceMarkdownFiles` delegates to `internal/core/markdownvalidation`.
  - Sync status stores `ValidationErrors []ValidationError` with `code`, `path`, `message`, and `severity`.

- `internal/core/tree/tree_service.go`
  - `ReconstructTreeFromFS` delegates to `NodeStore.ReconstructTreeFromFS`.

- `internal/core/tree/node_store.go`
  - Directories are accepted only if `SlugService.IsValidSlug(name)` succeeds.
  - Markdown files are accepted only if the basename before the final `.md` succeeds, except section content files such as `index.md` and active `README.md`.
  - `agent_hooks.PLAN.md` becomes basename `agent_hooks.PLAN`, then fails because `_` and `.` are invalid slug characters.

### Validation

- `internal/core/markdownvalidation/use_cases.go`
  - Walks the workspace root.
  - Adds `invalid_slug` for invalid directory names and invalid Markdown basenames.
  - Reports paths relative to `RootDir`; with root `docs`, `docs/plans/agent_hooks.PLAN.md` becomes `plans/agent_hooks.PLAN.md`.
  - Uses `tree.MarkdownPathToRoutePath` for non-section files.

- `internal/core/tree/route_path.go`
  - `MarkdownPathToRoutePath` strips only the final `.md` extension and removes trailing `index`.
  - It does not normalize route segments.
  - `plans/agent_hooks.PLAN.md` maps to `plans/agent_hooks.PLAN`.

### Markdown Links

- `internal/core/markdownlinks/markdownlinks.go`
  - `NewIndexFromRoot` walks the root and builds page and section entries from raw filesystem paths.
  - It strips `.md` in `NewIndex`.
  - It does not share reconstruction or validation slug normalization.

Any route normalization change must include markdown-link indexing, or validation and canonical link migration will keep resolving different route paths than reconstruction.

### Importer

- `internal/importer/planner.go`
  - Already normalizes source directory segments with `NormalizePathToValidSlugs`.
  - Normalizes source filenames with `NormalizeFilenameToValidSlug`.
  - This turns names like `My Guides/Intro.md` into valid route paths such as `my-guides/intro`.

- `internal/core/tree/slug_service.go`
  - `IsValidSlug` allows letters, numbers, and hyphens, and rejects reserved slugs case-insensitively.
  - `GenerateValidSlug` normalizes invalid or reserved names into valid slugs.
  - `NormalizeFilenameToValidSlug` preserves the file extension while normalizing the basename.

Importer behavior is the strongest precedent for accepting messy source filenames as ingestion input rather than requiring them to already be LeafWiki route slugs.

## Existing Test Anchors

Backend tests:

- `internal/core/tree/slug_service_test.go`
  - `TestNormalizePathToValidSlugs_ReservedSegmentGetsSuffix`
  - `TestNormalizeFilenameToValidSlug_ReservedSlugGetsSuffix`
  - `TestNormalizeFilenameToValidSlug_UnderscoreFilename`

- `internal/core/tree/route_path_test.go`
  - Current raw route conversion coverage.

- `internal/core/tree/node_store_reconstruct_test.go`
  - `index.md` and `README.md` section precedence.
  - Uppercase `INDEX.MD` behavior.
  - Reconstruction metadata writeback behavior.

- `internal/core/markdownvalidation/use_cases_test.go`
  - Workspace validation and canonical Markdown link validation.

- `internal/workspacesync/service_test.go`
  - `TestServiceSyncNowReportsValidationForSkippedInvalidSlugMarkdown`
  - `TestServiceSyncNowRunsAfterSyncWhenValidationWarningsExist`
  - Existing validation banner and sync status behavior.

- `internal/wiki/mcp/mcp_integration_test.go`
  - `TestLocalMCPRefresh_InvalidWorkspaceReturnsValidationAndKeepsMCPAvailable`
  - `TestLocalMCPValidateWikiScansUnsyncedMarkdownFiles`

Frontend and E2E tests:

- `e2e/tests/workspace-sync.spec.ts`
  - Validation banner coverage.
  - Sync refresh clearing behavior.

- `e2e/tests/mcp-agent-context.spec.ts`
  - MCP refresh exposes validation through browser tree.

## Existing Plan Precedent

- `docs/plans/workspace-sync.PLAN.md`
  - Raw Markdown is captured before parsing and repair.
  - Invalid filesystem states are committed and surfaced as sync validation errors rather than preventing the app from being reachable.

- `docs/plans/canonical_markdown_links.PLAN.md`
  - Old forms are ingestion input.
  - Canonical runtime behavior should have one output model.
  - Ambiguous cases must be validation errors instead of guessed rewrites.

- `docs/plans/page-metadata-v1.PLAN.md`
  - Best local pattern for the four-artifact planning workflow.
  - Uses explicit scope, impact analysis, Gherkin, and Definition of Done.

## Constraints

- Use `rtk` for shell commands in this repo.
- Keep v1 narrow: fix route ingestion and validation behavior without designing a full aliasing system.
- Preserve raw workspace history before any automatic repair or writeback.
- Do not broaden slug rules just because filesystem input is messier than route slugs.
- Preserve `index.md`, `README.md`, dot-directory, and workspace-root escape behavior.
- Keep MCP, HTTP, UI, validation, reconstruction, and markdown-link indexing aligned.
