<!-- leafwiki
version: 1
page:
  id: KsCdhVaDRZ
  title: Revisions
  created_at: "2026-06-09T20:59:16.319489029Z"
  updated_at: "2026-06-09T20:59:16.319489029Z"
  creator_id: system
  last_author_id: system
-->

# Revisions

LeafWiki uses Git-backed workspace revisions for workspace runtimes. Markdown history is captured from the workspace files under `--root-dir`, so page history reflects the same document source that LeafWiki syncs from disk.

Workspace revisions store Markdown history in `<data-dir>/.leafwiki/git` while using `<root-dir>` as the worktree. Revision IDs are Git commit hashes. Startup creates an initial snapshot commit for existing managed Markdown files when needed.

Commit trailers include:

- `LeafWiki-Source`
- `LeafWiki-Reason`
- `LeafWiki-Batch`
- `LeafWiki-Actor`
- `LeafWiki-Changed-Markdown`

Filesystem and unknown edits use Public Editor authorship. Authenticated web/MCP writes use the initiating user when available. The committer is LeafWiki.

Assets are excluded in workspace sync v1. Revision asset routes return not found, and the frontend history view exposes content preview, diff, and raw text tabs only.

Document restore creates a new Git restore commit. Workspace restore also creates a new commit and never resets HEAD backward.

Old page-snapshot revision data is ignored by current history flows and is not migrated into Git.

Decision source: `codex://threads/019e9d96-e56b-7ed3-b3fa-52db45fc57e6`
