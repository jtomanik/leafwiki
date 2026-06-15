<!-- leafwiki
version: 1
page:
  id: PYCO2VaDR
  title: Agent Collaboration
  created_at: "2026-06-14T13:51:34.182517881Z"
  updated_at: "2026-06-14T18:27:56.708083Z"
  creator_id: system
  last_author_id: public-editor
-->

# Agent Collaboration

Thread context: `codex://threads/019e9c9b-92bc-74c1-820e-a758f051d779`

LeafWiki's MCP surface is a context-first collaboration layer for agents and humans working against the same Markdown-backed wiki.

## Agent Skill

Agents that support repo-local skills should use [LeafWiki Agent Workflow](/skills/llmwiki/SKILL.md) before editing or validating LeafWiki content. The skill packages the context-first workflow, viewer/editor refresh rules, version handling, direct Markdown flow, and validation expectations into a reusable agent instruction.

## First Call

Call `wiki_get_context` first. It returns:

- effective user and role
- runtime config and enabled tool names
- workspace sync status and validation summary
- recent workspace changes and context-token history
- active web and agent sessions
- a compact tree and recommended next tools

Use `sinceToken` from a prior response when continuing work as the same MCP user in the same retained MCP session. Tokens expire after a short retention window; true handoffs or new sessions should call `wiki_get_context` fresh. If the token is unknown, LeafWiki returns current context and a warning instead of failing.

## Editing Policy

Prefer semantic MCP writes for normal page work:

- `wiki_update_page`
- `wiki_update_page_metadata`
- `wiki_replace_page_section`
- `wiki_create_page`, `wiki_move_page`, `wiki_delete_page`, and related page tools

Use direct Markdown file edits for broad mechanical body changes, generated bulk rewrites, or repo-native workflows where filesystem tools are safer than many small MCP calls. Preserve any top-of-file `<!-- leafwiki ... -->` metadata block exactly, edit page body content below that block, and use `wiki_update_page_metadata` for tags and properties instead of hand-editing metadata. After direct edits, call `wiki_refresh` if humans or subsequent MCP reads need immediate visibility.

Before editing a page, check `activeSessions` from `wiki_get_context`. Presence is advisory, not locking, but it should influence whether an agent edits now, narrows the edit with partial-edit tools, or asks the user before touching a page a human is actively editing.

## Validation

Run validation before final response:

- `wiki_validate_page` for a stored page by `pageId` or `path`
- `wiki_validate_content` for proposed Markdown without writing
- `wiki_validate_wiki` for workspace-level conflicts and validation errors

Validation tools do not replace optimistic locking. Mutating tools still require the current page `version`; stale versions fail with conflict details.

## Presence Privacy

Web presence is recorded by authenticated UI heartbeat and expires from memory after a short TTL. MCP context exposes sanitized user id/name/role and page id/path/title when resolvable. Email is only included for admin MCP callers.

Agent hook presence is recorded through `scripts/run.sh agent-hook <provider>` and exposed as sanitized provider sessions. LeafWiki never exposes raw hook payloads, raw provider session IDs, prompts, transcript paths, API keys, JWTs, or daemon control tokens in MCP context.

## Common Flows

Context and scoped read:

```json
{"tool":"wiki_get_context","arguments":{"syncMode":"auto","treeDepth":2}}
{"tool":"wiki_get_subtree","arguments":{"path":"/docs","depth":2}}
{"tool":"wiki_search_pages","arguments":{"q":"authentication","limit":10}}
```

Direct Markdown edit:

```json
{"tool":"wiki_get_context","arguments":{"syncMode":"auto"}}
```

Edit files under `--root-dir`, preserving canonical `<!-- leafwiki ... -->` metadata blocks and changing only body content unless the task is an explicit metadata repair. Then:

```json
{"tool":"wiki_refresh","arguments":{"validate":true,"source":"filesystem"}}
{"tool":"wiki_validate_wiki","arguments":{}}
```

Narrow safe edit:

```json
{"tool":"wiki_get_page_by_path","arguments":{"path":"api"}}
{"tool":"wiki_validate_page","arguments":{"path":"api"}}
{"tool":"wiki_replace_page_section","arguments":{"path":"api","version":"<page.version>","headingPath":["Authentication"],"content":"Updated body\\n"}}
{"tool":"wiki_validate_page","arguments":{"path":"api"}}
```

MCP `path` inputs are LeafWiki route paths. Do not include authored Markdown href prefixes such as `/docs`.
