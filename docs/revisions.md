# Revisions

LeafWiki has two revision backends:

- legacy page-snapshot revisions, enabled with `--enable-revision`
- Git-backed workspace revisions, enabled with `--enable-workspace-sync`

They are mutually exclusive. Startup fails if both flags or environment variables are enabled.

## Legacy Page-Snapshot Revisions

Legacy revisions store LeafWiki page snapshots and asset metadata in the data directory. They are tied to page operations performed through LeafWiki and support asset revision APIs.

## Git-Backed Workspace Revisions

Workspace revisions store Markdown history in `<data-dir>/.leafwiki/git` while using `<root-dir>` as the worktree. Revision IDs are Git commit hashes. The first enablement creates an initial snapshot commit for existing managed Markdown files.

Commit trailers include:

- `LeafWiki-Source`
- `LeafWiki-Reason`
- `LeafWiki-Batch`
- `LeafWiki-Actor`
- `LeafWiki-Changed-Markdown`

Filesystem and unknown edits use Public Editor authorship. Authenticated web/MCP writes use the initiating user when available. The committer is LeafWiki.

Assets are excluded in workspace sync v1. Revision asset routes return not found, and the frontend hides the asset tab or states that assets are not tracked by workspace sync.

Document restore creates a new Git restore commit. Workspace restore also creates a new commit and never resets HEAD backward.

Decision source: `codex://threads/019e9d96-e56b-7ed3-b3fa-52db45fc57e6`
