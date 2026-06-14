---
name: llmwiki
description: Work with a LeafWiki MCP server as an agent, using context-first discovery, safe edits, workspace sync, validation, and presence awareness.
---

# LeafWiki Agent Workflow

Call `wiki_get_context` first. Treat it as the current contract for user role, enabled tools, sync status, validation state, recent changes, active sessions, and recommended next tools. For the same MCP user and retained MCP session, pass the previous `contextToken` as `sinceToken` after pauses; tokens expire after about 30 minutes. For true handoffs or new sessions, call `wiki_get_context` fresh and treat unknown-token warnings as stale-context signals. Treat warnings, deltas, and dirty active sessions as conflict signals that require re-reading, narrowing the edit, or validating before writing.

Before direct filesystem work, confirm the context shows workspace sync enabled, the needed tools in `server.tools` or `recommendedTools`, editor/admin role for refreshes and writes, and a root directory known from launch config, user input, or local repo context that you can actually access. Viewer contexts can read and validate but cannot refresh or mutate.

Before broad reads, use `wiki_get_subtree` and `wiki_search_pages` to scope the work. Avoid walking the whole wiki unless the task requires it.

Prefer semantic MCP writes for normal edits:

- `wiki_update_page`
- `wiki_update_page_metadata`
- `wiki_replace_page_section`
- page create/move/delete/sort/refactor tools

Use direct Markdown file edits for bulk or mechanical body changes under the configured root directory. Preserve any top-of-file `<!-- leafwiki ... -->` metadata block exactly, edit page body content below that block, and use `wiki_update_page_metadata` for tags and properties instead of hand-editing metadata. After direct edits, call `wiki_refresh` when immediate web/MCP visibility is needed.

Every write tool that accepts a version must use a fresh `page.version` from `wiki_get_page`, `wiki_get_page_by_path`, or the immediately preceding write result. Re-read and merge on stale-version conflicts.

Check `activeSessions` before editing a page. Presence is advisory, but active web sessions with `dirty: true` should make you narrow the edit, wait, or ask before touching the same page.

Run validation before final response:

- `wiki_validate_page` for stored page changes
- `wiki_validate_content` for proposed Markdown before writing; include `existingPageId` when validating an edit to an existing page
- `wiki_validate_wiki` after direct filesystem work or broad changes

Partial-edit rules:

- Use `wiki_update_page_metadata` for tags and properties without changing body content.
- Use `wiki_replace_page_section` for heading-scoped changes.
- Read the page first with `wiki_get_page` or `wiki_get_page_by_path`, then pass `page.version` to the partial-edit tool.
- Stale versions must be re-read and resolved, not forced.

Direct Markdown flow:

1. Call `wiki_get_context`.
2. Edit files under `--root-dir`, preserving canonical `<!-- leafwiki ... -->` metadata blocks and changing only body content unless the task is an explicit metadata repair.
3. Call `wiki_refresh` with `source: "filesystem"` when immediate visibility matters.
4. Run validation.
5. Summarize changed pages and any remaining validation issues.

Native STDIO example:

```bash
./scripts/run.sh mcp --root-dir ./wiki --data-dir ./.wiki
```
