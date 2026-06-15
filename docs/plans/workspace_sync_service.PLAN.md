<!-- leafwiki
version: 1
page:
  id: da2-uIavg
  title: Workspace Sync Service Plan
  created_at: "2026-06-15T05:44:44.860513387Z"
  updated_at: "2026-06-15T05:44:44.860513387Z"
  creator_id: system
  last_author_id: system
-->

# Workspace Sync Service Plan

## Summary

Reference thread: `codex://threads/019e9c9b-92bc-74c1-820e-a758f051d779`
Plan artifact: `plans/workspace_sync_service.PLAN.md`.

Build one shared workspace sync path for direct filesystem edits, MCP refresh/context tools, daemon startup, validation, and a future filesystem watcher. The filesystem remains the source of truth. LeafWiki semantic writes through HTTP/MCP continue to use the existing page-save use cases and side effects; direct Markdown edits become visible by calling the shared sync service instead of reimplementing refresh logic in each caller.

The first implementation slice should be an explicit, synchronous service with tests. Watcher integration should follow only after the service boundary is stable and the watcher dependency/build tradeoffs are decided.

## Current Repo Shape

- `internal/wiki/wiki.go` is the composition root for one live `*Wiki`.
- Startup currently performs a full refresh in scattered steps:
  - `initCoreServices()` constructs `tree.TreeService` and calls `w.tree.LoadTree()`.
  - `initLinkService()` creates `links.LinkService` and calls `w.links.IndexAllPages()`.
  - `bootstrapTagsAndProperties()` clears and rebuilds tags/properties by walking `w.tree`.
  - `initSearch()` creates `search.SQLiteIndex`, then asynchronously runs `pagesave.SearchIndexSideEffect.IndexAllPages()`.
- `internal/core/tree/tree_service.go` already exposes `ReconstructTreeFromFS()`, which swaps the in-memory tree under the tree lock and persists the current schema.
- `internal/core/tree/node_store.go` reconstructs from `RootDir`, skips hidden files/directories and non-Markdown files, materializes missing section `index.md`, writes missing managed frontmatter, and returns errors for duplicate IDs or sibling slug conflicts.
- Derived indexes already have full rebuild entrypoints:
  - links: `internal/links/link_service.go:IndexAllPages()`
  - search: `internal/wiki/pagesave/search_effect.go:IndexAllPages()`
  - tags/properties: currently only via `Wiki.bootstrapTagsAndProperties()`
- Normal HTTP/MCP page saves already run synchronous side effects through `pagesave.PageSaveOrchestrator`: search, links, revisions, tags, and properties.
- MCP tools are registered in `internal/wiki/mcp`, with tool names centralized in `tool_descriptors.go`. There is no current `wiki_refresh` or `wiki_get_context` tool in this checkout.
- `go.mod` has no watcher dependency today.
- This checkout already contains project-daemon code under `internal/projectdaemon` and daemon orchestration in `cmd/leafwiki/main.go`; workspace sync should plug into the daemon-owned `*Wiki`, not redesign daemon ownership.

## Product Contract

- The filesystem is authoritative for page content and tree reconstruction.
- Semantic LeafWiki operations through web UI and MCP remain the preferred normal path because they preserve actor identity, optimistic versions, link context, permissions, revisions, and human-visible behavior.
- Direct Markdown edits are supported for bulk/mechanical work. Agents can edit files directly, then ask LeafWiki to sync, validate, and report what changed.
- Watcher events are advisory. Correctness must come from explicit `SyncNow` / validation, because events can be lost, duplicated, partial, or arrive during atomic-save bursts.
- `wiki_get_context` should eventually call sync first and surface sync status. `wiki_refresh` should become an explicit force/drain/sync-now tool, not required boilerplate.
- Recent changes are a bounded awareness feed, not a complete revision history. Default to roughly 10-20 entries and include source: `web`, `mcp`, `filesystem`, or `unknown`.
- Filesystem-originated changes may be recorded into revision history when revisions are enabled, but bulk edits must be coalesced enough to avoid revision spam.

## Architecture

Add a `WorkspaceSyncService` owned by `Wiki`.

Suggested package placement for the first slice: keep it in `internal/wiki` as `sync_service.go` plus `sync_service_test.go`. That avoids an import cycle while the service needs direct access to `TreeService`, links, tags, properties, search status, revision service, workspace config, and logger. If it grows later, split pure types into `internal/wiki/syncstate` or an internal subpackage.

Core type sketch:

```go
type WorkspaceSyncService struct {
	mu sync.Mutex

	workspace Workspace
	tree      *tree.TreeService
	links     *links.LinkService
	tags      *tags.TagsService
	props     *properties.PropertiesService
	search    *pagesave.SearchIndexSideEffect
	status    *search.IndexingStatus
	revisions *revision.Service
	log       *slog.Logger

	recent *RecentChangeBuffer
	last   SyncStatus
}

type SyncRequest struct {
	Scope    SyncScope
	Reason   string
	Source   ChangeSource
	Validate bool
	Force    bool
}

type SyncStatus struct {
	Token             string
	StartedAt         time.Time
	CompletedAt       time.Time
	LastError          string
	ChangedPaths      []ChangedPath
	RecentChanges     []RecentChange
	Validation         ValidationSummary
	Watcher           WatcherStatus
	SearchIndexing     search.IndexingStatus
}
```

First-slice behavior for `SyncNow(ctx, req)`:

1. Serialize syncs with `WorkspaceSyncService.mu`.
2. Snapshot the old tree before reconstruction, enough to compute added/updated/deleted/moved page IDs and paths after sync.
3. Call `w.tree.ReconstructTreeFromFS()` for `ScopeWiki` / `ScopeSubtree`.
4. Rebuild derived indexes from the current tree:
   - `w.links.IndexAllPages()`
   - clear and rebuild tags/properties in one tree/page pass
   - `pagesave.SearchIndexSideEffect.IndexAllPages()` synchronously for explicit sync
5. If revisions are enabled and `req.Source == filesystem`, record coalesced filesystem changes as a small number of revision entries. If this is too large for the first implementation, explicitly leave filesystem revision capture off and expose that in status as `revisionCapture: "not_implemented"`.
6. Run validation when `req.Validate` is true. Initial validation may wrap reconstruction/index errors and a minimal structural scan; deeper link/frontmatter/asset validation can be a follow-up package.
7. Store a bounded recent-change summary and a context token derived from completion time + tree hash + sequence number.
8. Return `SyncStatus` suitable for future MCP `wiki_refresh` and `wiki_get_context`.

The service should not own business writes. Existing create/update/delete/move/asset use cases remain the mutation path for web UI and semantic MCP tools. Those paths should only notify the sync service of compact change summaries after successful orchestrated writes, so recent changes can include `web` and `mcp` without triggering a full sync.

## Startup Integration

Refactor startup to route through the same service:

- `initCoreServices()` should still construct `TreeService`.
- After links/tags/properties/search services exist, construct `w.sync`.
- Replace scattered startup rebuilds with either:
  - `w.sync.Bootstrap(ctx)` for tree + links + tags/properties + synchronous or async search, or
  - keep `w.tree.LoadTree()` for schema migration first, then call `w.sync.RebuildDerivedIndexes(ctx, "startup")`.

Prefer the smallest safe step:

1. Introduce `WorkspaceSyncService` with `RebuildDerivedIndexes` using the existing startup code.
2. Move `bootstrapTagsAndProperties()` behind the service.
3. Add explicit `SyncNow()` and tests for direct filesystem edits.
4. Only then decide whether startup should call the full `SyncNow()` or retain the current `LoadTree()` migration path plus service-owned index rebuild.

This avoids breaking legacy schema migration behavior while still converging on one shared refresh path.

## Watcher Integration

Watcher support is a second slice. The watcher should be a thin event producer, not a source of truth.

Responsibilities:

- Watch `workspace.RootDir`, not `workspace.DataDir`.
- Ignore LeafWiki internal app state: `workspace.DataDir`, `.leafwiki`, `.importer`, `assets`, SQLite databases, schema files, temp files, swap files, hidden editor artifacts, and non-Markdown paths unless asset validation later needs them.
- Normalize events to relative clean paths under `RootDir`.
- Debounce/coalesce atomic-save bursts, write+rename patterns, and duplicate events.
- On overflow, backend error, or suspicious high-volume burst, mark status as requiring a full resync and call `SyncNow(ScopeWiki, SourceFilesystem, Force=true)`.
- Never directly update tree/index stores.

Dependency note:

- `github.com/sgtdi/fswatcher` is directionally suitable because its README advertises native backends, built-in debouncing, batching, filtering, and context-based shutdown.
- Its macOS default backend is FSEvents and requires CGO; pure-Go macOS builds use kqueue. The README claims FSEvents is more accurate and CPU efficient for high-volume operations, while kqueue can miss short-lived files under load.
- Before adding it to `go.mod`, compare it against `github.com/fsnotify/fsnotify` for Homebrew/static build expectations, Windows/Linux coverage, API stability, overflow signaling, and testability. Do this in the watcher slice, not the first sync-service slice.

## MCP Integration

This plan provides the foundation but does not implement the broader MCP surface rename.

Future MCP work should:

- Add `wiki_refresh` as a thin call to `WorkspaceSyncService.SyncNow`.
- Add `wiki_get_context` as the first-call tool. It should call sync before returning, then include:
  - current user/config,
  - workspace paths and project identity,
  - navigation/tree context,
  - search/indexing status,
  - sync status and last error,
  - watcher enabled/disabled state,
  - pending event count,
  - recent changed paths,
  - validation summary,
  - active daemon/session summary where available.
- Rename existing MCP tools with `wiki_` prefixes in a separate breaking-change MCP slice.

Do not make agents call `wiki_refresh` before every operation. `wiki_get_context` and daemon/watch sync should cover the common case; `wiki_refresh` is for force/drain/sync-now after bulk direct edits.

## Validation Direction

Validation should become its own service, invoked by workspace sync and MCP tools.

Minimum first pass:

- Return tree reconstruction errors as validation failures with actionable paths where available.
- Detect duplicate `leafwiki_id` / permalink conflicts and path/file slug conflicts already produced by reconstruct.
- Report invalid Markdown/frontmatter parse failures that reconstruction currently logs and skips.
- Surface broken links by querying the rebuilt links index.

Follow-up validation:

- invalid slugs and path segments,
- missing assets referenced by Markdown,
- orphan assets,
- stale `.order.json` entries,
- route path conflicts,
- subtree-scoped validation,
- proposed-content validation for future safe edit tools.

## Implementation Slices

### Slice 1: Explicit Sync Service, No Watcher

Files:

- Create: `internal/wiki/sync_service.go`
- Create: `internal/wiki/sync_service_test.go`
- Modify: `internal/wiki/wiki.go`
- Modify as needed: `internal/search/indexing_status.go`
- Modify as needed: `internal/wiki/mcp/types.go`, `schema.go`, `tool_descriptors.go`, `tools_config.go` only if exposing a temporary status tool in this slice

Steps:

1. Add `WorkspaceSyncService` types and constructor.
2. Move tags/properties rebuild logic into a method owned by the sync service.
3. Add `SyncNow(ctx, SyncRequest) (SyncStatus, error)`.
4. Wire `w.sync` in `NewWiki` after tree/links/tags/properties/search creation.
5. Keep startup behavior equivalent: tree loads before routes are built; search status still reflects indexing state.
6. Add tests that create a wiki, edit files directly under `RootDir`, call `SyncNow`, then assert tree/search/tags/properties/links reflect:
   - create Markdown file,
   - update Markdown content/frontmatter,
   - delete Markdown file,
   - rename/move Markdown file,
   - invalid duplicate ID returns sync/validation error without silently corrupting old in-memory tree.
7. Run:

```bash
rtk go test ./internal/wiki ./internal/core/tree ./internal/links ./internal/tags ./internal/properties ./internal/search
rtk go test ./...
```

### Slice 2: Public Status Consumers

Files:

- Modify: `internal/wiki/mcp/tool_descriptors.go`
- Modify: `internal/wiki/mcp/schema.go`
- Modify: `internal/wiki/mcp/routes.go`
- Create or modify: `internal/wiki/mcp/tools_context.go`
- Modify: `internal/wiki/mcp/mcp_integration_test.go`
- Modify docs after the MCP naming direction is finalized.

Steps:

1. Add the sync service to `wikimcp.RoutesConfig`.
2. Add `wiki_refresh` and/or `wiki_get_context` depending on the MCP rename slice timing.
3. Ensure `wiki_get_context` calls `SyncNow` with a cheap/default validate mode before assembling output.
4. Assert tool output includes sync status, search status, recent changes, validation summary, and current user/config.
5. Keep legacy unprefixed tools only if the MCP rename slice has not landed; otherwise register prefixed names only.

### Slice 3: Watcher Producer

Files:

- Create: `internal/wiki/workspace_watcher.go` or `internal/workspacewatcher/*`
- Modify: `internal/wiki/wiki.go`
- Modify: `cmd/leafwiki/main.go` for flags/env if watcher is configurable
- Modify: `go.mod`, `go.sum`
- Add tests under the watcher package and focused process tests if flags are added.

Steps:

1. Choose watcher dependency after comparing `sgtdi/fswatcher` and `fsnotify`.
2. Add a watcher interface so service tests do not depend on OS event timing.
3. Add path filters and temp-file ignores.
4. Coalesce events into pending paths and expose pending count/status.
5. Trigger debounced `SyncNow` for filesystem source.
6. Treat overflow/errors as "full resync required".
7. Add platform-safe tests for filtering/coalescing with a fake watcher; keep live watcher tests opt-in or narrowly scoped.

## Gherkin Test Suite

```gherkin
Feature: Explicit workspace sync

  Scenario: Direct Markdown creation appears in LeafWiki
    Given a running Wiki with an empty root directory
    When an agent writes "notes/new-page.md" directly on disk
    And the agent calls SyncNow with source "filesystem"
    Then get_tree includes "new-page"
    And search finds the new page content
    And tag and property queries reflect the file frontmatter

  Scenario: Direct Markdown update refreshes derived indexes
    Given a page exists and has tags ["old"] and property status "draft"
    When an agent edits the file to tags ["new"], status "ready", and a link to "/target"
    And the agent calls SyncNow
    Then search returns the updated content
    And list_tags contains "new" and not "old" for that page
    And get_pages_by_property returns the page for status "ready"
    And get_link_status reports the updated outgoing link

  Scenario: Direct Markdown deletion removes stale index records
    Given a page exists and is indexed in search, links, tags, and properties
    When the page file is deleted directly on disk
    And the agent calls SyncNow
    Then get_tree no longer includes the page
    And search, tags, properties, and link status no longer expose stale records for that page

  Scenario: Invalid duplicate IDs do not corrupt the live tree
    Given the live tree is valid
    When an agent writes another Markdown file with an existing leafwiki_id
    And the agent calls SyncNow with validation enabled
    Then SyncNow returns a validation error naming both conflicting paths
    And the previous live tree remains usable
  ```

```gherkin
Feature: Advisory workspace watcher

  Scenario: Atomic save burst produces one sync summary
    Given the watcher is enabled
    When an editor writes a temp file, renames it over a Markdown file, and removes backup files
    Then the watcher coalesces the burst
    And WorkspaceSyncService runs one filesystem sync for the final Markdown path

  Scenario: Watcher overflow falls back to full sync
    Given the watcher reports an overflow or missed-events error
    When WorkspaceSyncService receives the watcher status
    Then the next sync scope is the full wiki
    And sync status records the overflow as the reason
```

## Definition Of Done

- One explicit service owns full workspace sync and derived-index rebuilds.
- Direct filesystem create/update/delete/rename tests pass for tree, search, tags, properties, and links.
- Existing HTTP/MCP semantic page-save tests still pass, proving normal writes did not switch to raw filesystem sync.
- Sync status includes last sync time, last error, recent changed paths, validation summary, and watcher state placeholder even before watcher integration.
- The service is deterministic without a watcher.
- No watcher dependency is added until the watcher slice.
- Broad verification passes with `rtk go test ./...`.
