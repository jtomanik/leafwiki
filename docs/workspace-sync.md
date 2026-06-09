# Workspace Sync

Workspace sync lets LeafWiki treat the Markdown tree under `--root-dir` as the source of truth. It records managed Markdown files in an internal Git repository, synchronizes direct filesystem edits into the in-memory tree, and exposes document/workspace restore APIs.

Decision source: `codex://threads/019e9d96-e56b-7ed3-b3fa-52db45fc57e6`

## Enablement

Start LeafWiki with:

```bash
leafwiki --enable-workspace-sync --root-dir ./wiki --data-dir ./.wiki
```

Environment equivalent:

```bash
LEAFWIKI_ENABLE_WORKSPACE_SYNC=true
```

Workspace sync is mutually exclusive with legacy page-snapshot revisions. Starting with both `--enable-revision` and `--enable-workspace-sync` fails.

`scripts/run.sh mcp` enables workspace sync by default for native STDIO MCP sessions. Use `--disable-workspace-sync` or `LEAFWIKI_RUN_MCP_ENABLE_WORKSPACE_SYNC=0` to omit it.

## Internal Git Layout

LeafWiki stores Git data at:

```text
<data-dir>/.leafwiki/git
```

The Git worktree is the configured `<root-dir>`. LeafWiki opens go-git with explicit internal storage and explicit worktree paths. It removes go-git's root `.git` file when created as a plain file, and it does not mutate a containing user Git repository.

Only managed Markdown files are tracked:

- included: `*.md` case-insensitively, for example `page.md` and `page.MD`
- excluded: assets and other non-Markdown files, `.git`, `.leafwiki`, dotfiles, swap files, temporary downloads, and partial files

## Sync Flow

Each sync commits raw Markdown changes before parsing. If Git capture fails, sync stops before rebuilding tree or indexes.

After capture, LeafWiki reconstructs the tree from the filesystem. On success, it rebuilds links, tags, properties, and search. On invalid Markdown or invalid wiki state, the raw state remains committed and the app keeps running with validation errors in workspace sync status.

Web and MCP page mutations write Markdown before the required workspace-sync side effect captures Git history. If that capture fails, the API call returns an error but the Markdown mutation is left on disk. The next explicit refresh, watcher-triggered sync, or successful page mutation retries full Git capture from the filesystem source of truth.

Watcher events are advisory. Explicit sync through `POST /api/workspace-sync/refresh` performs a full sync even if the watcher is unavailable.

MCP agents can use the same path through `wiki_refresh`. Direct Markdown edits under `--root-dir` are allowed for bulk or mechanical changes because the filesystem is the source of truth. After direct edits, call `wiki_refresh` when immediate UI/MCP visibility is needed, then run `wiki_validate_wiki` or scoped page validation before reporting completion.

`wiki_get_context` uses workspace sync status to make the first MCP call context-rich. In `auto` mode it refreshes when workspace sync reports pending watcher events, a previous sync error, or an enabled watcher that is not running. In `force` mode, editor and admin MCP callers always ask workspace sync to reconcile the Markdown tree first. Viewer callers cannot refresh; the context response returns current status plus a warning that refresh was skipped.

## Restore Semantics

Document restore writes one historical Markdown file back to the current document path, commits a new restore batch, and reruns sync. If reconstruction writes missing LeafWiki metadata back into Markdown, that writeback is captured in the restore batch.

Workspace snapshot restore rewrites all managed Markdown files to the selected commit state, removes managed Markdown files absent from that commit, leaves assets and non-Markdown files untouched, commits a new restore batch, and reruns sync. If reconstruction writes missing LeafWiki metadata back into Markdown, that writeback is captured in the restore batch. HEAD always moves to a new restore commit; it is not reset backward.

## APIs

- `GET /api/workspace-sync/status`
- `POST /api/workspace-sync/refresh`
- `GET /api/workspace-sync/snapshots?cursor=&limit=` where `cursor` is the last snapshot commit ID returned by the previous page
- `POST /api/workspace-sync/snapshots/:commit/restore`

Existing page history APIs use Git-backed workspace commits when workspace sync is enabled. Workspace snapshot list/restore remain HTTP APIs in this slice; MCP exposes the current sync state, validation, recent change summaries, and page-level Git-backed history through the existing revision tools.
