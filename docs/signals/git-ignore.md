<!-- leafwiki
version: 1
page:
  id: J1v9VEavR
  title: Git ignore
  created_at: "2026-06-20T21:22:36.209874Z"
  updated_at: "2026-06-20T21:23:19.082384Z"
  creator_id: PFXiFAavR
  last_author_id: PFXiFAavR
-->

# Git ignore

Git-backed workspace sync should respect the repository's ignore rules when it decides which filesystem changes become tracked wiki content.

Ignored files should remain local implementation details. They should not appear in the page tree, sync snapshots, history views, or MCP workspace responses unless a user explicitly changes the Git ignore policy to include them.
