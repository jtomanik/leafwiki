<!-- leafwiki
version: 1
page:
  id: MJhaXI-vR
  title: Agent-Friendly LeafWiki MCP Surface Implementation Plan
  created_at: "2026-06-15T05:44:44.85564092Z"
  updated_at: "2026-06-15T05:44:44.85564092Z"
  creator_id: system
  last_author_id: system
-->

# Agent-Friendly LeafWiki MCP Surface Implementation Plan

> **Historical note:** This plan predates the federated runtime cleanup. Runtime, MCP compatibility, revision-mode, or workspace-sync flag examples in this artifact are historical and do not describe current startup; current LeafWiki uses `--mcp` and always-on Git-backed workspace sync.

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` (recommended) or `superpowers:executing-plans` to implement this plan task-by-task. Use TDD first. Split into subagents for MCP contract, validation/partial edits, presence/UI, and docs/E2E review.

**Goal:** Make LeafWiki MCP a first-class agent collaboration surface with `wiki_*` tools, context-first discovery, safe direct-file recovery, safe partial edits, and human/agent presence parity with the web UI.

**Architecture:** Keep filesystem/workspace sync as the source-of-truth reconciliation layer. MCP tools remain primitive capabilities, not workflows. `wiki_get_context` becomes the first-call dynamic context tool, backed by workspace sync status, bounded context checkpoints, recent workspace revisions, web presence, and agent hook presence.

**Tech Stack:** Go 1.25.x, modelcontextprotocol/go-sdk, LeafWiki project daemon, WorkspaceSyncService, React/Vite, Playwright, Gherkin-style acceptance suite.

**Reference thread:** `codex://threads/019e9c9b-92bc-74c1-820e-a758f051d779`

---

## Summary

Implement one breaking MCP surface update:

- Rename every MCP tool to canonical `wiki_*`; do not keep legacy aliases.
- Add `wiki_get_context`, `wiki_refresh`, `wiki_get_subtree`, validation tools, and two safe partial-edit tools.
- Add web heartbeat presence and merge it with hook-provided agent presence in `wiki_get_context.activeSessions`.
- Defer workspace snapshot MCP tools.
- Create the agent Skill at the end of the effort.

Assume these are already implemented or landing before this work completes:

- Workspace sync and Git-backed Markdown revisions from `plans/workspace-sync.PLAN.md`.
- Agent hook presence from `plans/hooks.PLAN.md`, including private-control sanitized agent sessions and `scripts/run.sh mcp`.

---

## Key Public Interface Changes

### Breaking Tool Rename

Rename all existing MCP tool names by prefixing `wiki_`.

| Old | New |
|---|---|
| `get_config` | `wiki_get_config` |
| `get_current_user` | `wiki_get_current_user` |
| `get_tree` | `wiki_get_tree` |
| `get_page` | `wiki_get_page` |
| `get_page_by_path` | `wiki_get_page_by_path` |
| `lookup_path` | `wiki_lookup_path` |
| `resolve_permalink` | `wiki_resolve_permalink` |
| `suggest_slug` | `wiki_suggest_slug` |
| `create_page` | `wiki_create_page` |
| `update_page` | `wiki_update_page` |
| `delete_page` | `wiki_delete_page` |
| `move_page` | `wiki_move_page` |
| `sort_pages` | `wiki_sort_pages` |
| `ensure_page` | `wiki_ensure_page` |
| `convert_page` | `wiki_convert_page` |
| `copy_page` | `wiki_copy_page` |
| `search_pages` | `wiki_search_pages` |
| `get_search_status` | `wiki_get_search_status` |
| `list_tags` | `wiki_list_tags` |
| `get_pages_by_tags` | `wiki_get_pages_by_tags` |
| `list_property_keys` | `wiki_list_property_keys` |
| `get_pages_by_property` | `wiki_get_pages_by_property` |
| `get_link_status` | `wiki_get_link_status` |
| `upload_asset` | `wiki_upload_asset` |
| `get_asset` | `wiki_get_asset` |
| `list_assets` | `wiki_list_assets` |
| `rename_asset` | `wiki_rename_asset` |
| `delete_asset` | `wiki_delete_asset` |
| `list_revisions` | `wiki_list_revisions` |
| `get_latest_revision` | `wiki_get_latest_revision` |
| `get_revision` | `wiki_get_revision` |
| `compare_revisions` | `wiki_compare_revisions` |
| `get_revision_asset` | `wiki_get_revision_asset` |
| `restore_revision` | `wiki_restore_revision` |
| `preview_page_refactor` | `wiki_preview_page_refactor` |
| `apply_page_refactor` | `wiki_apply_page_refactor` |

Update descriptor constants, schemas, integration tests, docs, examples, and E2E clients in the same slice. Old names must not be listed or callable.

### New MCP Tools

#### `wiki_get_context`

Input:

```json
{
  "sinceToken": "optional opaque context token",
  "syncMode": "auto",
  "treeDepth": 2,
  "recentChangesLimit": 20
}
```

Defaults:

- `syncMode`: `auto`
- `treeDepth`: `2`
- `recentChangesLimit`: `20`

Allowed `syncMode`:

- `auto`: drain/await known pending sync work; if watcher/sync state is stale, pending, unavailable, or errored, call workspace sync for editor/admin callers.
- `force`: call workspace sync unconditionally for editor/admin callers.
- `none`: read current state only.

Refresh attempts from `wiki_get_context` are editor/admin-only because they can mutate workspace history. Viewer callers using `auto` or `force` receive current context with a warning when a refresh would otherwise run.

Output:

```json
{
  "contextToken": "opaque",
  "previousContextToken": "opaque-or-empty",
  "changesSincePreviousContext": [],
  "contextHistory": [],
  "user": {},
  "config": {},
  "server": {},
  "syncStatus": {},
  "validation": {},
  "recentChanges": [],
  "activeSessions": [],
  "presenceStatus": { "web": "enabled", "agentHooks": "enabled" },
  "tree": {},
  "recommendedTools": []
}
```

Rules:

- If `sinceToken` is omitted, compute changes since the previous `wiki_get_context` call for the same MCP session/user.
- If `sinceToken` is provided, compute changes since that checkpoint.
- Store checkpoint metadata only, in memory, per MCP session/user, capped at 10 entries.
- Do not persist full context payloads.
- Invalid/unknown tokens return a normal context plus a warning, not a hard failure.
- `recentChanges` comes from workspace sync/Git-backed snapshots and includes commit id, timestamp, actor, source, reason, changed count, changed paths capped to 20, and page IDs when resolvable.
- `activeSessions` includes sanitized agent hook presence and web heartbeat presence.
- No prompt text, transcript paths, raw hook session IDs, tool inputs, tool outputs, IP addresses, user agent strings, auth tokens, API keys, or raw filesystem paths.

#### `wiki_refresh`

Input:

```json
{
  "validate": true,
  "source": "mcp"
}
```

Defaults:

- `validate`: `true`
- `source`: `mcp`

Allowed `source`: `mcp`, `filesystem`.

Output: workspace sync status, recent changed paths, validation errors, and last commit hash.

Rules:

- Use current MCP user as actor.
- Use `reason=explicit_refresh`.
- This is optional for normal use; agents call it after bulk direct Markdown edits when they want immediate web/MCP visibility.

#### `wiki_get_subtree`

Input:

```json
{
  "pageId": "optional",
  "path": "/optional",
  "depth": 2,
  "includeMetadata": true,
  "includeLinkCounts": false,
  "includeContentPreview": false
}
```

Defaults:

- omitted `pageId` and `path` means root
- `depth`: `2`
- `includeMetadata`: `true`
- `includeLinkCounts`: `false`
- `includeContentPreview`: `false`

Rules:

- Supplying both `pageId` and `path` is an input error.
- Output includes `root`, `breadcrumbs`, `depth`, and `truncated`.
- Default output is compact tree/node data only; agents use `wiki_get_page` for full content.

#### Validation Tools

Add:

- `wiki_validate_page`
- `wiki_validate_content`
- `wiki_validate_wiki`

Common validation output:

```json
{
  "ok": false,
  "summary": { "errors": 1, "warnings": 2 },
  "issues": [
    {
      "severity": "error",
      "code": "duplicate_leafwiki_id",
      "path": "docs/a.md",
      "pageId": "optional",
      "message": "Human readable message"
    }
  ]
}
```

`wiki_validate_page` input:

```json
{ "pageId": "optional", "path": "/optional" }
```

Rules:

- Both pageId and path is an error.
- Neither is an error.

`wiki_validate_content` input:

```json
{
  "path": "/docs/new-page",
  "content": "---\ntitle: X\n---\n\n# X",
  "existingPageId": "optional"
}
```

Rules:

- Validate proposed Markdown/frontmatter in the hypothetical path context.
- Do not write content.
- Check parse errors, reserved frontmatter, slug/path validity, duplicate IDs, broken links where resolvable, missing assets, and page-kind/path conflicts.

`wiki_validate_wiki` input:

```json
{ "includeWarnings": true }
```

Rules:

- Reuse workspace sync validation and add link/assets checks from rebuilt indexes.
- Hidden/dot directories remain ignored consistently with tree reconstruction.

#### Safe Partial Edit Tools

Add:

- `wiki_update_page_metadata`
- `wiki_replace_page_section`

`wiki_update_page_metadata` input:

```json
{
  "pageId": "optional",
  "path": "/optional",
  "version": "required",
  "setTags": ["optional full replacement"],
  "addTags": ["optional"],
  "removeTags": ["optional"],
  "setProperties": { "status": "ready" },
  "removeProperties": ["deprecated"],
  "includePage": false,
  "includeValidation": true,
  "includeLinkStatus": false
}
```

Rules:

- Both pageId and path is an error; neither is an error.
- `version` is required and stale versions fail with structured conflict details.
- Tags are normalized using existing tag rules.
- Property keys must reject `tags`, `title`, empty keys, and `leafwiki_*`.
- Content body must not change except frontmatter metadata.
- Default output is compact result plus validation summary; full page/link status are opt-in.

`wiki_replace_page_section` input:

```json
{
  "pageId": "optional",
  "path": "/optional",
  "version": "required",
  "headingPath": ["API", "Authentication"],
  "occurrence": 1,
  "content": "Replacement Markdown below the heading",
  "includePage": false,
  "includeValidation": true,
  "includeLinkStatus": false
}
```

Rules:

- Heading identity is heading path plus optional 1-based occurrence.
- Parser must ignore headings inside fenced code blocks.
- Replace content under the matched heading until the next same-or-higher heading.
- Keep the existing heading line unless `content` starts with a heading at the same level; then replace the whole section including heading.
- Missing heading is an error.
- Ambiguous heading path without `occurrence` is an error.
- Stale version is a structured conflict.
- Preserve frontmatter, tags, properties, and unrelated sections.

### Presence

Agent presence is provided by `plans/hooks.PLAN.md`.

Add web human presence in this effort:

- `POST /api/presence/heartbeat`
- optional `DELETE /api/presence/session/:id` for best-effort tab cleanup
- in-memory TTL default: 90 seconds
- heartbeat cadence: 25 seconds
- no public list endpoint required

Heartbeat input:

```json
{
  "sessionId": "browser-generated opaque id",
  "mode": "view|edit|history|assets|settings|import|unknown",
  "pageId": "optional",
  "path": "/optional",
  "dirty": false
}
```

Rules:

- Route requires normal auth, with public editor injected when auth is disabled.
- Presence stores sanitized user id/name/role and page id/path/title when resolvable.
- Email is exposed only to admin MCP users; otherwise omit it.
- Web presence is shown in `wiki_get_context.activeSessions`.
- Agent presence and web presence share one output shape where possible:

```json
{
  "type": "web|agent",
  "sessionId": "opaque",
  "provider": "codex",
  "model": "optional",
  "mode": "edit",
  "state": "active|idle|expired",
  "user": { "id": "...", "name": "...", "role": "editor" },
  "page": { "id": "...", "path": "/docs/x", "title": "X" },
  "dirty": true,
  "lastEvent": "PreToolUse",
  "activeSubagents": 0,
  "firstSeenAt": "...",
  "lastSeenAt": "..."
}
```

---

## Implementation Tasks

### Task 1: Breaking MCP Tool Rename

Files:

- Modify `internal/wiki/mcp/tool_descriptors.go`
- Modify `internal/wiki/mcp/schema.go`
- Modify all `internal/wiki/mcp/tools_*.go` references as needed
- Modify MCP integration/E2E clients and docs

Steps:

- Write failing tests that tool listing contains only `wiki_*` names.
- Update descriptor constants to prefixed names.
- Update schema switch cases to use prefixed constants.
- Update integration tests for required fields, output schemas, and feature-gated revision/refactor tools.
- Update docs and examples to use `wiki_*`.
- Assert old names return MCP unknown-tool errors.

### Task 2: MCP Context Infrastructure

Files:

- Create `internal/wiki/mcp/tools_context.go`
- Add context/checkpoint types near MCP package or a small `internal/wiki/mcp/contextstate` package
- Extend `RoutesConfig` with workspace sync status/refresh, recent snapshot listing, web presence provider, and agent presence provider

Steps:

- Add in-memory per-session checkpoint store capped at 10.
- Define context token as opaque string; internally map to workspace commit hash, sync time, validation fingerprint, and creation time.
- Add `wiki_get_context` descriptor/schema.
- Implement `syncMode=auto|force|none`.
- Include shallow tree depth 2 using existing DTO depth pruning.
- Include current MCP user/config/search status/workspace sync status/recent changes/validation.
- Add context-sensitive `recommendedTools`.
- Return `activeSessions: []` and `presenceStatus` even when providers are unavailable.

### Task 3: `wiki_refresh`

Files:

- Add `internal/wiki/mcp/tools_workspace_sync.go`
- Extend MCP routes with `WorkspaceSyncRefresh`

Steps:

- Add descriptor/schema/output.
- Call WorkspaceSyncService with current MCP actor.
- Use `source=mcp` by default and `reason=explicit_refresh`.
- Return sync status and validation issues.
- Ensure auth-disabled mode attributes to public editor.
- Ensure viewer role cannot call refresh if it mutates sync history; require editor/admin.

### Task 4: `wiki_get_subtree`

Files:

- Extend page tool file or create `internal/wiki/mcp/tools_navigation.go`

Steps:

- Add descriptor/schema/output.
- Resolve root, pageId, or path.
- Add breadcrumbs.
- Use existing DTO node conversion and depth pruning.
- Optionally include link counts and content previews.
- Add errors for invalid path, missing page, both pageId/path.

### Task 5: Validation Service and Tools

Files:

- Create or extend a validation package under `internal/wiki`
- Add MCP validation tools
- Reuse workspace sync validation and links/assets/page lookup services

Steps:

- Define `ValidationIssue` with severity/code/path/pageId/message.
- Implement stored page validation.
- Implement proposed content validation without writes.
- Implement whole-wiki validation.
- Include broken links, duplicate IDs, invalid frontmatter, path/slug conflicts, missing assets, and hidden-file ignore behavior.
- Add MCP schemas and tests.

### Task 6: Safe Partial Edits

Files:

- Create `internal/wiki/pages/partial_edit.go` or focused use cases
- Add `internal/wiki/mcp/tools_partial_edit.go`
- Add Markdown section parser helper with tests

Steps:

- Implement metadata patch use case with version precondition.
- Implement heading-based section replacement with version precondition.
- Reuse existing page update path so workspace sync, revisions, links, tags, and properties stay correct.
- Add structured conflict errors with current version/path/title.
- Return compact output by default; include page/link status only on request.

### Task 7: Web Human Presence

Files:

- Create `internal/wiki/presence`
- Create `internal/wiki/presence/routes.go`
- Wire into `Wiki.Registrars()`
- Add frontend heartbeat hook/store

Steps:

- Implement in-memory `WebPresenceRegistry` with TTL and injectable clock.
- Add authenticated heartbeat route.
- Generate a stable browser tab session id in sessionStorage.
- Heartbeat from main app shell every 25 seconds.
- Populate mode/page/dirty from router/editor state.
- Best-effort delete on tab unload; correctness relies on TTL.
- Merge web presence with hook-provided agent presence in `wiki_get_context`.

### Task 8: Documentation and Skill

Files:

- Update `docs/mcp.md`
- Update `docs/workspace-sync.md`
- Add `docs/agent-collaboration.md`
- Add distributable `skills/llmwiki/SKILL.md`
- Update `README.md` and script docs after hook plan rename lands

Docs must include:

- Thread link `codex://threads/019e9c9b-92bc-74c1-820e-a758f051d779`
- Tool rename migration note
- `wiki_get_context` first-call workflow
- Direct Markdown edit workflow: edit files, call `wiki_refresh` when immediate visibility is needed, run validation before reporting done
- Presence privacy contract
- Validation and partial edit examples
- Workspace sync / Git-backed revision relationship
- `scripts/run.sh mcp` examples if hooks plan landed

Skill must teach agents:

- Call `wiki_get_context` first.
- Prefer semantic MCP writes for normal edits.
- Use direct Markdown file edits for bulk/mechanical changes.
- Call `wiki_refresh` after direct bulk edits when humans need immediate web visibility.
- Run validation before final response.
- Check active sessions before editing a page.
- Use `wiki_get_subtree` and search before broad reads.
- Use partial edit tools for narrow changes.

---

## Gherkin Test Suite

```gherkin
Feature: Breaking MCP tool rename
  Scenario: Only prefixed tools are listed
    Given an MCP client connects to LeafWiki
    When it lists tools
    Then every LeafWiki tool name starts with "wiki_"
    And no unprefixed legacy tool names are present

  Scenario Outline: Existing tool behavior is preserved after rename
    Given a wiki fixture equivalent to the existing MCP integration tests
    When the client calls <new_tool> with the same valid arguments as <old_tool>
    Then the result matches the old HTTP/API parity expectation
    Examples:
      | old_tool | new_tool |
      | get_config | wiki_get_config |
      | get_current_user | wiki_get_current_user |
      | get_tree | wiki_get_tree |
      | get_page | wiki_get_page |
      | get_page_by_path | wiki_get_page_by_path |
      | lookup_path | wiki_lookup_path |
      | resolve_permalink | wiki_resolve_permalink |
      | suggest_slug | wiki_suggest_slug |
      | create_page | wiki_create_page |
      | update_page | wiki_update_page |
      | delete_page | wiki_delete_page |
      | move_page | wiki_move_page |
      | sort_pages | wiki_sort_pages |
      | ensure_page | wiki_ensure_page |
      | convert_page | wiki_convert_page |
      | copy_page | wiki_copy_page |
      | search_pages | wiki_search_pages |
      | get_search_status | wiki_get_search_status |
      | list_tags | wiki_list_tags |
      | get_pages_by_tags | wiki_get_pages_by_tags |
      | list_property_keys | wiki_list_property_keys |
      | get_pages_by_property | wiki_get_pages_by_property |
      | get_link_status | wiki_get_link_status |
      | upload_asset | wiki_upload_asset |
      | get_asset | wiki_get_asset |
      | list_assets | wiki_list_assets |
      | rename_asset | wiki_rename_asset |
      | delete_asset | wiki_delete_asset |

  Scenario: Legacy tool call fails clearly
    Given an MCP client connects
    When it calls "get_page"
    Then the call fails with unknown tool
    And the error does not suggest both old and new names

  Scenario: Feature-gated revision and refactor tools are renamed
    Given workspace sync and link refactor are enabled
    When the MCP client lists tools
    Then revision tools are listed only as "wiki_list_revisions", "wiki_get_latest_revision", "wiki_get_revision", "wiki_compare_revisions", "wiki_get_revision_asset", "wiki_restore_revision"
    And refactor tools are listed only as "wiki_preview_page_refactor" and "wiki_apply_page_refactor"
```

```gherkin
Feature: wiki_get_context
  Scenario: First context call returns agent-ready context
    Given a wiki with workspace sync enabled
    When an editor MCP client calls wiki_get_context with no input
    Then the response includes contextToken, user, config, server, syncStatus, validation, recentChanges, activeSessions, tree, and recommendedTools
    And tree contains the root and two levels of children by default
    And activeSessions is an array even when no sessions exist

  Scenario: Subsequent context call reports changes since previous context
    Given an MCP client has called wiki_get_context once
    And a web user edits a page
    When the same MCP client calls wiki_get_context again without sinceToken
    Then changesSincePreviousContext includes the edited page path and page id
    And previousContextToken equals the token from the first call
    And a new contextToken is returned

  Scenario: Explicit sinceToken is deterministic
    Given an MCP client has context tokens A and B
    And multiple changes happened after token A
    When the client calls wiki_get_context with sinceToken A
    Then changesSincePreviousContext includes all changes since A
    When the client calls wiki_get_context with sinceToken B
    Then changesSincePreviousContext includes only changes since B

  Scenario: Unknown context token is non-fatal
    When an MCP client calls wiki_get_context with sinceToken "missing"
    Then the response includes a warning about the unknown token
    And current context is still returned

  Scenario Outline: syncMode controls sync behavior
    Given workspace sync status is <state>
    When wiki_get_context is called with syncMode <mode>
    Then sync behavior is <expected>
    Examples:
      | state | mode | expected |
      | healthy no pending events | auto | no full sync is forced |
      | pending watcher events | auto | pending work is drained or sync runs |
      | previous sync error | auto | sync is attempted and errors are surfaced |
      | healthy no pending events | force | sync runs unconditionally for editor/admin callers |
      | previous sync error | none | no sync runs and last error is reported |

  Scenario: Validation errors are surfaced
    Given direct filesystem edits introduced duplicate leafwiki IDs
    When wiki_get_context is called
    Then validation.summary.errors is greater than 0
    And recommendedTools includes wiki_validate_wiki and wiki_get_page or wiki_get_subtree

  Scenario: Recent changes include workspace revision metadata
    Given a page was edited through web, MCP, and direct filesystem sync
    When wiki_get_context is called
    Then recentChanges contains entries with source "web", "mcp", and "filesystem"
    And each entry includes reason, actor, commit id, timestamp, changed count, paths, and resolvable page IDs

  Scenario: Context history is bounded per MCP session
    Given the same MCP session calls wiki_get_context 12 times
    When the latest context is returned
    Then contextHistory contains at most 10 entries
    And another MCP session cannot see this session's checkpoint history

  Scenario: Viewer gets read-only recommendations
    Given an MCP user has viewer role
    When wiki_get_context is called
    Then recommendedTools does not recommend write tools
    And write tools still fail permission checks

  Scenario: Viewer context never mutates workspace sync history
    Given an MCP user has viewer role
    When wiki_get_context is called with syncMode "force"
    Then workspace sync is not run
    And the response includes a warning that refresh was skipped
```

```gherkin
Feature: wiki_refresh
  Scenario: Explicit refresh captures direct Markdown edits
    Given an agent writes a Markdown file directly under root-dir
    When the MCP client calls wiki_refresh
    Then workspace sync runs with source "mcp" and reason "explicit_refresh"
    And the new page is visible through wiki_get_page_by_path
    And validation issues are returned if any exist

  Scenario: Refresh can attribute filesystem source
    Given direct files changed outside LeafWiki
    When wiki_refresh is called with source "filesystem"
    Then recentChanges uses source "filesystem"
    And actor is Public Editor unless workspace sync already attributed otherwise

  Scenario: Refresh requires editor or admin
    Given an MCP viewer session
    When it calls wiki_refresh
    Then the call fails with editor/admin permission required

  Scenario: Refresh reports sync failure without hiding state
    Given root-dir contains an invalid duplicate-id state
    When wiki_refresh is called
    Then the response includes validation errors
    And the MCP server remains available
```

```gherkin
Feature: wiki_get_subtree
  Scenario: Get subtree by path
    Given "/docs" is a section with nested children
    When wiki_get_subtree is called with path "/docs" and depth 2
    Then the response root is "/docs"
    And children are present up to depth 2
    And breadcrumbs include root and docs

  Scenario: Get subtree by page id
    Given a section exists
    When wiki_get_subtree is called with pageId and includeMetadata true
    Then the response includes version, kind, metadata, and children

  Scenario: Root subtree
    When wiki_get_subtree is called without pageId or path
    Then the root tree is returned with default depth 2

  Scenario: Both pageId and path are rejected
    When wiki_get_subtree is called with both pageId and path
    Then the call fails with an input validation error

  Scenario: Missing page is reported
    When wiki_get_subtree is called with path "/does-not-exist"
    Then the call fails with page not found

  Scenario: Optional link counts and previews are opt-in
    Given a subtree with links and content
    When wiki_get_subtree is called without includeLinkCounts or includeContentPreview
    Then no link counts or previews are returned
    When it is called with both options true
    Then compact link counts and content previews are returned
```

```gherkin
Feature: Validation tools
  Scenario: Validate a stored page
    Given a page with valid Markdown and links
    When wiki_validate_page is called by pageId
    Then ok is true
    And issues is empty

  Scenario: Validate page by path
    Given a page exists at "/docs/api"
    When wiki_validate_page is called with path "/docs/api"
    Then validation targets that page

  Scenario: Reject ambiguous page validation input
    When wiki_validate_page is called with both pageId and path
    Then the call fails with an input validation error
    When it is called with neither
    Then the call fails with an input validation error

  Scenario: Proposed content validation does not write files
    Given no page exists at "/draft"
    When wiki_validate_content is called with valid Markdown for "/draft"
    Then ok is true
    And "/draft" still does not exist in the tree

  Scenario: Proposed content detects invalid frontmatter
    When wiki_validate_content is called with malformed YAML frontmatter
    Then ok is false
    And an error issue names the frontmatter parse failure

  Scenario: Proposed content detects broken links and missing assets
    Given root-dir has no "/missing" page and no "missing.png" asset
    When wiki_validate_content is called with links to "/missing" and "missing.png"
    Then issues include broken_link and missing_asset

  Scenario: Whole-wiki validation reports filesystem conflicts
    Given root-dir contains duplicate leafwiki IDs
    When wiki_validate_wiki is called
    Then ok is false
    And issues include duplicate_leafwiki_id with both paths when available

  Scenario: Hidden paths are ignored consistently
    Given ".scratch/bad.md" exists under root-dir
    When wiki_validate_wiki is called
    Then hidden path errors are not reported

  Scenario: Warnings can be included or suppressed
    Given the wiki has warning-level issues
    When wiki_validate_wiki is called with includeWarnings false
    Then warning issues are omitted
    When includeWarnings true
    Then warning issues are returned
```

```gherkin
Feature: wiki_update_page_metadata
  Scenario: Patch tags and properties without changing body content
    Given a page has body "Original body"
    When wiki_update_page_metadata sets tags and properties with the current version
    Then the page body remains "Original body"
    And tags and properties are updated
    And workspace sync captures an MCP page write

  Scenario: Add and remove tags
    Given a page has tags ["old", "keep"]
    When wiki_update_page_metadata adds ["new"] and removes ["old"]
    Then tags are ["keep", "new"]

  Scenario: Set tags replaces the full tag list
    Given a page has tags ["a", "b"]
    When wiki_update_page_metadata sets tags ["c"]
    Then tags are ["c"]

  Scenario: Remove properties
    Given a page has properties status "draft" and owner "team"
    When wiki_update_page_metadata removes ["status"]
    Then owner remains
    And status is absent

  Scenario: Reserved property keys are rejected
    When wiki_update_page_metadata sets property "title", "tags", or "leafwiki_private"
    Then the call fails with validation error
    And the page is unchanged

  Scenario: Stale version fails with conflict details
    Given a page version is stale
    When wiki_update_page_metadata is called with the stale version
    Then the call fails with page_version_conflict
    And the error includes current page id, path, title, and version

  Scenario: Viewer cannot update metadata
    Given an MCP viewer user
    When wiki_update_page_metadata is called
    Then the call fails with editor/admin permission required

  Scenario: Optional result verbosity
    When wiki_update_page_metadata is called with includePage false and includeLinkStatus false
    Then only compact result and validation are returned
    When includePage and includeLinkStatus are true
    Then updated page and link status are returned
```

```gherkin
Feature: wiki_replace_page_section
  Scenario: Replace a simple section
    Given a page contains "## API" followed by old content
    When wiki_replace_page_section targets headingPath ["API"] with current version
    Then the content under "## API" is replaced
    And unrelated sections are unchanged

  Scenario: Replace nested section by heading path
    Given a page contains "# Guide", "## API", and "### Auth"
    When wiki_replace_page_section targets ["Guide", "API", "Auth"]
    Then only the Auth section body is replaced

  Scenario: Headings inside code fences are ignored
    Given a page has a fenced code block containing "## API"
    And a real "## API" heading outside the fence
    When wiki_replace_page_section targets ["API"]
    Then the real section is replaced
    And the code fence remains unchanged

  Scenario: Ambiguous heading requires occurrence
    Given a page contains two "## Notes" headings
    When wiki_replace_page_section targets ["Notes"] without occurrence
    Then the call fails with ambiguous_heading
    When occurrence 2 is supplied
    Then the second Notes section is replaced

  Scenario: Missing heading fails without creating content
    When wiki_replace_page_section targets a heading path that does not exist
    Then the call fails with heading_not_found
    And the page is unchanged

  Scenario: Stale version fails safely
    Given the page changed after the agent read it
    When wiki_replace_page_section is called with the old version
    Then the call fails with page_version_conflict
    And no section content changes

  Scenario: Replacement can include a same-level heading
    Given a page contains "## API"
    When replacement content starts with "## API"
    Then the entire section including heading is replaced once
    And duplicate "## API" headings are not created

  Scenario: Validation after replacement reports broken links
    Given replacement content links to "/missing"
    When wiki_replace_page_section is called with includeValidation true
    Then the page is updated
    And validation reports the broken link

  Scenario: Optional result verbosity
    When wiki_replace_page_section is called with includePage false
    Then compact result is returned
    When includePage true
    Then the updated page is returned
```

```gherkin
Feature: Web human presence
  Scenario: Web heartbeat records viewer presence
    Given a logged-in editor has "/docs/api" open in view mode
    When the frontend sends a presence heartbeat
    Then wiki_get_context.activeSessions includes a web session for "/docs/api"
    And the session includes user id, name, role, page id, path, mode, and lastSeenAt

  Scenario: Edit dirty flag is visible
    Given a web editor is editing a page with unsaved changes
    When the heartbeat is sent
    Then activeSessions includes dirty true for that web session

  Scenario: Presence expires
    Given a web heartbeat was recorded
    When no heartbeat arrives for longer than the TTL
    Then wiki_get_context no longer lists that web session

  Scenario: Heartbeat requires authentication
    Given auth is enabled
    When an unauthenticated request posts to /api/presence/heartbeat
    Then it returns 401
    And no presence is recorded

  Scenario: Auth-disabled mode records public editor
    Given auth is disabled
    When the web UI sends a heartbeat
    Then presence records public-editor with editor role

  Scenario: Email privacy is role-gated
    Given a human session belongs to an editor with email
    When an admin MCP user calls wiki_get_context
    Then the web session may include email
    When a non-admin MCP user calls wiki_get_context
    Then email is omitted

  Scenario: Invalid heartbeat is rejected without poisoning presence
    When heartbeat mode is invalid or sessionId is empty
    Then the route returns 400
    And existing presence remains unchanged

  Scenario: Page lookup failure is tolerated
    When a heartbeat references a page that was deleted
    Then presence is recorded without page title
    And lastError is not exposed to agents as a server failure
```

```gherkin
Feature: Agent hook presence in context
  Scenario: Agent hook presence appears before first MCP tool call
    Given plans/hooks.PLAN.md has implemented private agent presence
    And a Codex SessionStart hook recorded a sanitized session
    When wiki_get_context is called later
    Then activeSessions includes an agent session with provider codex, model, source, lastEvent, firstSeenAt, and lastSeenAt

  Scenario: Agent presence provider unavailable is non-fatal
    Given the private agent presence provider is unavailable
    When wiki_get_context is called
    Then context is returned
    And presenceStatus.agentHooks is "unavailable"
    And activeSessions still includes web sessions if any

  Scenario: Agent sensitive fields are never exposed
    Given hook payloads contained raw session id, transcript path, prompt, tool input, and cwd
    When wiki_get_context returns activeSessions
    Then none of those raw fields are present

  Scenario: Codex subagents update parent session
    Given a Codex parent session is active
    When subagent start and stop hook events are recorded
    Then activeSessions shows activeSubagents increasing and returning to zero
    And no separate subagent session is listed
```

```gherkin
Feature: End-to-end agent collaboration
  Scenario: Agent sees human editing before writing
    Given the web UI is editing "/docs/api" with dirty changes
    When an MCP agent calls wiki_get_context
    Then activeSessions shows the human editing "/docs/api"
    And recommendedTools warns to read current page or avoid conflicting edit

  Scenario: Agent bulk-edits files and exposes them to web UI
    Given an MCP agent edits Markdown files directly on disk
    When it calls wiki_refresh
    Then the web UI tree shows the new pages after reload
    And wiki_get_context recentChanges includes the changed paths

  Scenario: Context-first workflow survives invalid filesystem state
    Given direct filesystem edits introduced invalid Markdown
    When an MCP agent calls wiki_get_context
    Then context returns validation errors
    And recommendedTools includes validation and page/subtree tools
    And the MCP server remains usable

  Scenario: Natural agent task can use new primitives
    Given a wiki with a "Project Plan" page
    When an MCP client performs the equivalent of "update the Risks section and tag it review"
    Then it can use wiki_get_context, wiki_get_page_by_path, wiki_replace_page_section, wiki_update_page_metadata, and wiki_validate_page
    And the final page content, tags, workspace revision, and validation status are correct
```

---

## Verification Commands

Run after implementation:

```bash
rtk go test ./internal/wiki/mcp ./internal/wiki ./internal/http ./internal/projectdaemon ./cmd/leafwiki
rtk go test ./internal/workspacesync ./internal/workspacesync/gitrevisions
rtk go test ./...
rtk bash -n scripts/run.sh scripts/test-run.sh scripts/install-all-macos.sh scripts/test-install-all-macos.sh
rtk bash scripts/test-run.sh
rtk bash scripts/test-install-all-macos.sh
rtk npm --prefix ui/leafwiki-ui run lint
rtk npm --prefix ui/leafwiki-ui run build
rtk npm --prefix e2e run lint
rtk npm --prefix e2e run format:check
rtk env E2E_RUN_MODE=local E2E_ENABLE_MCP_LOCAL=1 E2E_MCP_CLIENT_TRANSPORT=stdio ./e2e/run.sh tests/mcp-stdio-disable-auth.spec.ts
rtk env E2E_RUN_MODE=local E2E_ENABLE_MCP_API_KEYS_LOCAL=1 E2E_MCP_CLIENT_TRANSPORT=stdio ./e2e/run.sh tests/mcp-stdio-api-keys.spec.ts
rtk env E2E_RUN_MODE=local E2E_ENABLE_AGENT_HOOKS_LOCAL=1 E2E_ENABLE_MCP_LOCAL=1 E2E_MCP_CLIENT_TRANSPORT=stdio ./e2e/run.sh tests/agent-hooks.spec.ts
rtk env E2E_RUN_MODE=local E2E_ENABLE_MCP_LOCAL=1 E2E_ENABLE_WORKSPACE_SYNC=1 ./e2e/run.sh tests/mcp-agent-context.spec.ts
rtk env E2E_RUN_MODE=local E2E_ENABLE_MCP_LOCAL=1 E2E_ENABLE_WORKSPACE_SYNC=1 ./e2e/run.sh tests/mcp-safe-edits.spec.ts
rtk env E2E_RUN_MODE=local E2E_ENABLE_MCP_LOCAL=1 E2E_ENABLE_WORKSPACE_SYNC=1 ./e2e/run.sh tests/presence.spec.ts
rtk rg -n "\\b(get_page|get_tree|update_page|run-mcp\\.sh)\\b" docs README.md scripts internal/wiki/mcp e2e ui --glob '!plans/**' --glob '!references/**'
```

The final `rg` must not find live docs/examples/tests still using old MCP tool names or `run-mcp.sh`, except in historical plan files or explicit migration notes.

---

## Definition of Done

- All MCP tools are canonical `wiki_*`; old unprefixed tools are absent and uncallable.
- `wiki_get_context` is the first-call tool and returns user/config/server/sync/validation/recentChanges/activeSessions/tree/recommendedTools/context tokens.
- Context checkpoint history is in-memory, per MCP session/user, capped, and supports omitted `sinceToken` plus explicit `sinceToken`.
- `wiki_refresh` gives agents an explicit sync-now path after direct Markdown edits.
- `wiki_get_subtree` gives compact scoped navigation with breadcrumbs and depth.
- `wiki_validate_page`, `wiki_validate_content`, and `wiki_validate_wiki` cover stored, proposed, and whole-wiki validation without unintended writes.
- `wiki_update_page_metadata` and `wiki_replace_page_section` perform safe version-checked partial edits and use existing page-save/workspace-sync side effects.
- Web heartbeat presence is implemented, privacy-filtered, TTL-expiring, and included in `wiki_get_context.activeSessions`.
- Agent hook presence from `plans/hooks.PLAN.md` is merged into `wiki_get_context.activeSessions` without exposing sensitive hook payload fields.
- Workspace snapshot MCP restore/list tools are not added in this effort.
- Docs explain the new MCP workflow, direct-file workflow, presence privacy, validation, partial edits, and the Skill.
- `skills/llmwiki/SKILL.md` exists and teaches agents to call `wiki_get_context` first.
- Every Gherkin scenario above is represented by Go unit/integration tests, frontend tests where applicable, and Playwright E2E where needed.
- All verification commands pass.
- Final review explicitly checks action parity with web UI, non-happy-path coverage, privacy, stale docs/examples, stdout hygiene for MCP STDIO, and no accidental legacy compatibility shim.
