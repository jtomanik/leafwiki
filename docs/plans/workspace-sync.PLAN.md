# Workspace Sync + Git-Backed Markdown Revisions Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` or `superpowers:executing-plans` to implement this task-by-task. Use a fresh subagent for backend sync/revision work, frontend/history UX, and test/docs review if available.

**Goal:** Add `--enable-workspace-sync` as a core LeafWiki feature that watches `*.md` files, records Markdown history in an internal Git repository, syncs direct filesystem edits into the app, and exposes document/workspace restore UX.

**Decision source:** `codex://threads/019e9d96-e56b-7ed3-b3fa-52db45fc57e6`

**Architecture:** Workspace sync owns filesystem change capture and rebuild orchestration. Git-backed revisions record watched Markdown state before parsing, then amend the same commit for automatic LeafWiki metadata writebacks. Existing page-snapshot revisions remain as a mutually exclusive legacy backend.

**Tech Stack:** Go 1.25.x, React/Vite, `lucide-react`, Playwright, `github.com/go-git/go-git/v6`, `github.com/sgtdi/fswatcher`.

---

## 1. Core Implementation

### Configuration and Feature Flag

- Add `EnableWorkspaceSync bool` to:
  - `wiki.WikiOptions`
  - `http.RouterOptions`
  - `projectdaemon.Config`
  - CLI runtime config in `cmd/leafwiki/main.go`
  - frontend `/api/config` response and `ui/leafwiki-ui/src/stores/config.ts`
- Add CLI/env:
  - `--enable-workspace-sync`
  - `LEAFWIKI_ENABLE_WORKSPACE_SYNC`
- Keep `--enable-revision` unchanged for legacy page-snapshot revisions.
- Reject `--enable-revision` and `--enable-workspace-sync` together at startup with a clear error.
- Update `scripts/run-mcp.sh` so workspace sync is on by default:
  - Add wrapper env `LEAFWIKI_RUN_MCP_ENABLE_WORKSPACE_SYNC`, default `1`.
  - Add wrapper options `--enable-workspace-sync` and `--disable-workspace-sync`.
  - When enabled, pass `--enable-workspace-sync` to `leafwiki`.
- Update CLI usage, `.env.example`, `scripts/README.md`, and relevant installer/test scripts.

### Workspace Sync Service

Create `internal/workspacesync` with these responsibilities:

- Own one service per canonical `(data-dir, root-dir)` through `Wiki`.
- Provide:
  - `SyncNow(ctx, SyncRequest) (SyncStatus, error)`
  - `StartWatcher(ctx) error`
  - `Status() SyncStatus`
  - `RestoreDocument(ctx, pageID, commitID, actor) error`
  - `RestoreWorkspace(ctx, commitID, actor) error`
- `SyncRequest` includes:
  - `Reason`: `startup`, `watcher`, `explicit`, `web_write`, `restore`
  - `Source`: `filesystem`, `web`, `mcp`, `system`, `unknown`
  - `Actor`: user ID plus optional username/email
  - `Validate`: bool
  - `DrainWatcher`: bool
- Use one mutex around sync and restore. Watcher events only enqueue paths and trigger sync; they never mutate tree/indexes directly.
- Track status:
  - enabled
  - watcher enabled/running
  - pending event count
  - last sync time
  - last error
  - recent changed Markdown paths, bounded to 20
  - last commit hash
  - validation summary/errors

### Git-Backed Markdown History

Create `internal/workspacesync/gitrevisions` or equivalent.

- Store Git data at `<data-dir>/.leafwiki/git`.
- Use `<root-dir>` as worktree.
- Track only `*.md`.
- Exclude all other files, including assets, temp files, `.git`, `.leafwiki`, `.DS_Store`, swap files, and partial-download files.
- On first enablement for an existing root, create one initial commit:
  - Message: `LeafWiki initial workspace snapshot`
  - Author: Public Editor unless an authenticated setup actor is available.
- Commit one sync batch per sync.
- Commit raw Markdown changes before parsing/rebuild.
- If tree reconstruction writes Markdown metadata/frontmatter, amend the just-created batch commit.
- If no pre-parse Markdown commit was created but automatic metadata writeback creates Markdown changes, create a normal sync commit.
- If Git capture fails, fail sync before parsing/rebuild. This prevents silent loss of revision history.
- Invalid Markdown/wiki states are still committed. They become sync validation errors, not revision blockers.
- Use minimal commit trailers:
  - `LeafWiki-Source: filesystem|web|mcp|system|unknown`
  - `LeafWiki-Reason: startup|watcher|explicit|web_write|restore`
  - `LeafWiki-Batch: <uuid-or-shortid>`
  - `LeafWiki-Actor: <user-id>`
  - `LeafWiki-Changed-Markdown: <n>`
- Authorship:
  - filesystem/unknown edits use Public Editor.
  - authenticated web/MCP writes use that user as author.
  - multi-user batch uses dominant/initiating user as author and adds additional `LeafWiki-Actor` trailers.
  - committer is always LeafWiki.

### go-git Rules

- Add `github.com/go-git/go-git/v6` to `go.mod`.
- Open Git with filesystem storage at `<data-dir>/.leafwiki/git` and explicit worktree `<root-dir>`.
- After first `git.Init(...)`, remove any `.git` file that go-git creates in `<root-dir>` and always reopen with explicit storage/worktree.
- Add tests proving a user Git repo containing `<root-dir>` is not mutated by LeafWiki Git.
- Use `CommitOptions{Amend: true}` for metadata writeback amendments.
- Do not shell out to `git` in production code.

### Watcher

- Add `github.com/sgtdi/fswatcher` only through an internal adapter, not directly inside sync business logic.
- Configure watcher:
  - root path: `<root-dir>`
  - include regex: `.*\.md$`
  - exclude regex: `(^|/)\.git(/|$)`, `(^|/)\.leafwiki(/|$)`, temp/swap/download files
  - debounce/cooldown: 250ms
- Treat watcher events as advisory.
- On dropped events, overflow, or platform errors, set status and schedule a full `SyncNow`.
- Verify CI with Go `1.25.x`; fswatcher local reference uses Go `1.25.4`, so dependency addition must pass `go mod download` in CI.
- macOS note: fswatcher defaults to CGO FSEvents; Docker/release builds use `CGO_ENABLED=0`, so tests must cover pure-Go fallback where possible and correctness must still come from full sync.

### Sync Rebuild Flow

For each sync:

1. Drain/coalesce pending watcher events.
2. Commit current `*.md` diff before parsing.
3. Reconstruct tree from filesystem.
4. If reconstruction fails, keep service running, record validation/sync error, expose it to UI, and do not rebuild stale derived indexes.
5. If reconstruction succeeds, rebuild links, tags, properties, and search from the current tree.
6. Detect Markdown writebacks from reconstruction and amend the batch commit when the writeback was automatic.
7. Update sync status and recent changed paths.

Startup behavior changes only when workspace sync is enabled: invalid Markdown should not prevent the server/UI from starting; it should appear as workspace sync status.

---

## 2. HTTP and Frontend UX

### HTTP API

Add routes behind `EnableWorkspaceSync`:

- `GET /api/workspace-sync/status`
- `POST /api/workspace-sync/refresh`
- `GET /api/workspace-sync/snapshots?cursor=&limit=`
- `POST /api/workspace-sync/snapshots/:commit/restore`

Reuse existing revision routes for document history when workspace sync is enabled:

- `GET /api/pages/:id/revisions`
- `GET /api/pages/:id/revisions/latest`
- `GET /api/pages/:id/revisions/:revisionId`
- `GET /api/pages/:id/revisions/compare`
- `POST /api/pages/:id/revisions/:revisionId/restore`

Document-history behavior under Git backend:

- `revision.id` is the Git commit hash.
- `pageId` is the current page ID.
- history follows renames when go-git can detect them.
- assets are always empty in v1.
- asset revision route returns 404/not supported under Git backend.
- document restore restores the selected historical Markdown contents to the current document path, commits a restore batch, and reruns sync.

Workspace snapshot restore behavior:

- Restore all `*.md` files in `<root-dir>` to the selected commit state.
- Remove `*.md` files absent from that commit.
- Leave non-Markdown files and assets untouched.
- Commit restore as a new commit, do not move HEAD backward.
- Rerun sync and surface validation errors if the restored snapshot is invalid.

### Frontend

- Keep existing document history button in viewer toolbar using `History`.
- Existing restore action can add `RotateCcw`.
- Add Explorer toolbar snapshot button using `ArchiveRestore` or `RotateCcwSquare`.
- Add `WorkspaceSnapshotsDialog`:
  - list snapshot commits
  - show source/reason/author/time/changed Markdown count
  - confirm restore
- Add `workspaceSync` API client/store.
- Show invalid-state UX in the Explorer/sidebar:
  - use `AlertTriangle`
  - show concise banner: “Workspace synced, but some Markdown files could not be loaded.”
  - include expandable path/error list
  - include “Sync now” button
- Update `RestoreRevisionDialog` text for Git-backed mode:
  - document restore: “This restores this document’s Markdown content from the selected version and records a new workspace commit.”
  - legacy revision restore keeps old wording.
- Hide asset tab or show “Assets are not tracked by workspace sync” for Git-backed history.

---

## 3. Documentation and References

Create or update:

- `docs/workspace-sync.md`
  - feature purpose
  - `--enable-workspace-sync`
  - `run-mcp.sh` default behavior
  - internal Git layout
  - coexistence with user Git
  - watched file rules
  - restore semantics
  - invalid-state behavior
- `docs/revisions.md`
  - legacy page-snapshot revisions vs Git-backed workspace revisions
  - mutual exclusivity
  - authorship rules
  - assets excluded in v1
- `scripts/README.md`
  - wrapper flag/env behavior
- `.env.example`
  - `LEAFWIKI_ENABLE_WORKSPACE_SYNC`
- Plan/reference note:
  - Include this thread link: `codex://threads/019e9d96-e56b-7ed3-b3fa-52db45fc57e6`
  - Local references used:
    - `references/go-git`
    - `references/fswatcher`
    - `internal/wiki/wiki.go`
    - `internal/core/tree/tree_service.go`
    - `internal/wiki/revisions/*`
    - `ui/leafwiki-ui/src/features/history/*`
    - `e2e/tests/history.spec.ts`

---

## 4. Gherkin Test Suite

### Feature: Workspace sync configuration

```gherkin
Scenario: workspace sync can be enabled from CLI
  Given LeafWiki starts with --enable-workspace-sync
  Then the server starts
  And /api/config returns enableWorkspaceSync true
  And /api/workspace-sync/status returns enabled true

Scenario: legacy revisions and workspace sync are mutually exclusive
  Given LeafWiki starts with --enable-revision and --enable-workspace-sync
  Then startup fails
  And stderr contains "enable-revision and enable-workspace-sync cannot be combined"

Scenario: run-mcp enables workspace sync by default
  Given scripts/run-mcp.sh is run with --dry-run
  Then the planned command contains --enable-workspace-sync

Scenario: run-mcp can disable workspace sync
  Given scripts/run-mcp.sh is run with --dry-run --disable-workspace-sync
  Then the planned command does not contain --enable-workspace-sync
```

### Feature: Git-backed revision initialization

```gherkin
Scenario: first enablement snapshots existing Markdown files
  Given root-dir contains a.md and nested/b.md
  When LeafWiki starts with workspace sync enabled
  Then <data-dir>/.leafwiki/git exists
  And document history contains an initial snapshot commit
  And both Markdown files are present in that commit

Scenario: non-Markdown files are ignored
  Given root-dir contains a.md and image.png
  When workspace sync commits a batch
  Then a.md is tracked
  And image.png is not tracked

Scenario: internal Git coexists with user Git
  Given root-dir is inside an existing user Git repository
  When workspace sync creates internal commits
  Then the outer repository .git directory is not modified by LeafWiki
  And <root-dir>/.git is not left behind
  And user Git status only reflects normal Markdown worktree changes

Scenario: automatic metadata writeback amends the initial commit
  Given root-dir contains a Markdown file missing LeafWiki metadata
  When workspace sync starts
  Then Git history contains one initial snapshot commit
  And that commit includes the metadata written during reconstruction
```

### Feature: Sync direct filesystem edits

```gherkin
Scenario: creating a Markdown file on disk appears in the UI
  Given workspace sync is enabled
  When a user writes docs/new-page.md directly to root-dir
  And the watcher drains
  Then the Explorer shows docs/new-page
  And search finds text from docs/new-page.md
  And tags/properties/links from the file are indexed
  And document history has a filesystem-sourced commit

Scenario: editing a Markdown file on disk updates derived indexes
  Given a page contains tag "old"
  When the file is edited directly to contain tag "new"
  And workspace sync runs
  Then tags no longer list the page under "old"
  And tags list the page under "new"
  And search returns updated content

Scenario: deleting a Markdown file on disk removes it from the tree
  Given a page exists in the tree
  When its .md file is deleted directly
  And workspace sync runs
  Then the page is absent from the Explorer
  And the delete is recorded in Git history

Scenario: renaming a Markdown file on disk updates path and history
  Given root-dir contains old-name.md
  When old-name.md is renamed to new-name.md
  And workspace sync runs
  Then the page path is new-name
  And document history follows the rename when Git detects it

Scenario: duplicate watcher events create one batch
  Given workspace sync is enabled
  When the watcher receives multiple events for the same .md file within the debounce window
  Then one sync batch is committed
  And recent changed paths contains the file once
```

### Feature: Invalid filesystem states

```gherkin
Scenario: invalid Markdown state is committed before validation error
  Given root-dir contains two Markdown files with duplicate LeafWiki IDs
  When workspace sync runs
  Then Git contains a commit with both files
  And sync status reports a validation error
  And LeafWiki remains reachable

Scenario: invalid state appears in the frontend
  Given workspace sync status contains a validation error for bad.md
  When the user opens the Explorer
  Then an invalid workspace banner is visible
  And expanding it shows bad.md and the error message
  And the Sync now action is available

Scenario: Git capture failure prevents rebuild
  Given internal Git storage cannot be written
  When workspace sync runs
  Then sync returns an operational error
  And tree/search/tag/link indexes are not rebuilt from unrecorded changes
  And sync status contains the Git error
```

### Feature: Authorship

```gherkin
Scenario: filesystem edits use Public Editor
  Given a Markdown file is changed outside LeafWiki
  When workspace sync commits the change
  Then the commit author maps to Public Editor
  And the commit source is filesystem

Scenario: authenticated web edit uses user identity
  Given user alice edits a page through the web UI
  When workspace sync commits the batch
  Then the commit author maps to alice
  And the commit source is web

Scenario: multi-user batch records additional actors
  Given alice and bob both write pages before one sync batch commits
  When workspace sync commits the batch
  Then the primary author is deterministic
  And commit trailers include both actor IDs
```

### Feature: Document history and restore

```gherkin
Scenario: document history lists Git-backed commits
  Given workspace sync is enabled
  And a page has three Markdown commits
  When the user opens Page History
  Then three revisions are listed
  And each revision shows author, time, source, and summary

Scenario: document restore creates a new commit
  Given current page content is "current"
  And an older commit contains "previous"
  When the user restores the older document version
  Then the current file content becomes "previous"
  And a new restore commit is created
  And HEAD remains at the new restore commit

Scenario: restoring current document version is disabled
  Given the current version is selected in Page History
  Then the Restore button is disabled

Scenario: missing revision returns structured error
  Given workspace sync is enabled
  When the client requests a non-existent revision hash
  Then the API returns 404
  And the error code is revision_not_found or workspace_revision_not_found

Scenario: assets are not exposed under Git-backed revisions
  Given workspace sync is enabled
  When the client requests a revision asset
  Then the API returns 404
  And the UI does not imply assets are restorable
```

### Feature: Workspace snapshot restore

```gherkin
Scenario: snapshot restore restores all Markdown files
  Given commit A has one.md and two.md
  And current workspace has one.md and three.md
  When the user restores workspace snapshot A
  Then one.md matches commit A
  And two.md exists
  And three.md is removed
  And a new restore commit is created

Scenario: snapshot restore leaves assets untouched
  Given current workspace contains image.png
  When a workspace snapshot is restored
  Then image.png still exists

Scenario: snapshot restore handles invalid historical state
  Given a historical snapshot contains invalid Markdown metadata
  When the user restores that snapshot
  Then the Markdown files are restored
  And a restore commit is created
  And sync status reports the validation errors
```

### Feature: Watcher resilience

```gherkin
Scenario: watcher overflow triggers full sync
  Given the watcher reports dropped events
  When workspace sync handles the watcher status
  Then status records the dropped event condition
  And a full sync is scheduled

Scenario: temp files are ignored
  Given files page.md.swp, .DS_Store, and page.md are changed
  When watcher events are processed
  Then only page.md is considered revision-managed

Scenario: watcher can be absent but explicit sync works
  Given workspace sync is enabled but watcher failed to start
  When POST /api/workspace-sync/refresh is called
  Then direct Markdown edits are synchronized
  And status reports watcher not running
```

### Feature: E2E browser workflows

```gherkin
Scenario: direct filesystem edit appears without page reload ritual
  Given the E2E server runs with workspace sync enabled and separate root-dir
  When the test writes e2e-direct.md into root-dir
  Then the Explorer shows e2e-direct
  And opening the page shows the file contents

Scenario: Explorer snapshot restore button restores workspace
  Given two pages exist from different commits
  When the user opens Explorer snapshot history
  And restores an older snapshot
  Then the Explorer reflects the older Markdown tree
  And a success toast appears

Scenario: invalid workspace banner is visible
  Given the test writes duplicate-ID Markdown files directly to root-dir
  When workspace sync runs
  Then the Explorer shows an invalid workspace banner
  And the banner lists the duplicate ID error
```

---

## 5. Execution Steps and Definition of Done

### Suggested Task Order

1. Add config flags/env/router/frontend config and mutual-exclusion tests.
2. Add Git revision store with unit tests for init, commit, amend, restore, delete, rename, and user Git coexistence.
3. Add WorkspaceSyncService with fake watcher tests and rebuild/index integration tests.
4. Wire service into `Wiki` startup, page-save flow, and `run-mcp.sh`.
5. Add HTTP routes for sync status, refresh, snapshots, and restore.
6. Adapt revision routes to use legacy or Git backend depending on enabled feature.
7. Add frontend sync store, invalid-state banner, document restore wording, and workspace snapshot dialog.
8. Add Playwright E2E tests.
9. Update docs and run full verification.

### Verification Commands

Run with `rtk` prefix:

```bash
rtk go test ./internal/workspacesync/...
rtk go test ./internal/core/revision/... ./internal/wiki/revisions/... ./internal/wiki/...
rtk go test ./internal/http/... ./internal/projectdaemon/...
rtk go test ./cmd/leafwiki
rtk ./scripts/test-run-mcp.sh
rtk go test ./...
rtk npm --prefix ui/leafwiki-ui run lint
rtk npm --prefix ui/leafwiki-ui run build
rtk npm --prefix e2e run lint
rtk make run-e2e-local-fast GREP="History|Workspace Sync"
rtk make run-e2e-root-dir
```

### Definition of Done

- `--enable-workspace-sync` works from CLI/env and appears in `/api/config`.
- `scripts/run-mcp.sh` enables workspace sync by default and can disable it.
- Startup rejects `--enable-revision` plus `--enable-workspace-sync`.
- Internal Git repository is stored under `<data-dir>/.leafwiki/git`.
- `<root-dir>/.git` is not left behind by LeafWiki.
- A user Git repository containing `<root-dir>` is not mutated by LeafWiki.
- Only `*.md` files are revision-managed.
- Initial snapshot commit is created on first enablement.
- Direct create/update/delete/rename Markdown edits sync into tree, search, tags, properties, and links.
- Raw invalid Markdown states are committed before validation/parsing errors are reported.
- Git capture failures stop sync before rebuild.
- Automatic LeafWiki Markdown metadata writebacks amend the same sync commit.
- Web edits use authenticated user authorship where available.
- Filesystem edits use Public Editor.
- Document history works from Git commits and restore creates a new commit.
- Workspace snapshot restore restores all Markdown files to the selected commit and creates a new commit.
- Invalid workspace status is visible in the Explorer UI.
- Comprehensive Go and Playwright tests cover happy paths, invalid states, watcher dropped events, temp-file filtering, Git failures, restore semantics, and run-mcp defaults.
- Documentation explains configuration, internal Git layout, user Git coexistence, file filter, restore semantics, invalid-state behavior, and the thread reference.
- Full verification commands above pass in a clean checkout.
