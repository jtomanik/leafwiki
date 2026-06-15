<!-- leafwiki
version: 1
page:
  id: plan-workspace-route-decision
  title: Workspace Markdown Route Normalization - Decision
  created_at: "2026-06-14T18:41:17Z"
  updated_at: "2026-06-14T18:54:52Z"
  creator_id: system
  last_author_id: system
-->

# Workspace Markdown Route Normalization - Decision

## Decision Summary

LeafWiki will add a shared workspace Markdown route mapper. Filesystem names remain ordinary file names; LeafWiki routes remain strict slugs. Workspace reconstruction, validation, and markdown-link indexing will all use the mapper so they agree on the route represented by a Markdown file.

## Key Decisions

| Decision | Rationale |
|---|---|
| Normalize invalid filesystem Markdown segments into route-safe slugs | Workspace sync should accept common documentation filenames as ingestion input, matching importer behavior. |
| Preserve already-valid slug segments exactly | This avoids unnecessary route churn and keeps existing uppercase valid slugs working. |
| Keep route slug validation strict | Broadening public route syntax would spread ambiguity across routing, assets, links, and MCP. |
| Report normalized route collisions | Workspace reconstruction must not silently choose between two files that map to one route. |
| Skip top-level `assets/` as non-page static content | `/assets` is a reserved LeafWiki route namespace and docs often keep image files under `assets/`. |
| Preserve `index.md` and active `README.md` section behavior | Existing section semantics and canonical link behavior depend on these sentinels. |
| Render active root `README.md` at `/` | The main wiki page should be reachable when root README fallback is active; the frontend must not redirect away from it. |
| Add an Explorer Home icon for `/` | The sidebar currently renders only root children, so root content needs an explicit navigation affordance. |
| Apply the mapper to reconstruction, validation, and markdown-link indexing | A single normalized route model prevents MCP/UI/link parity drift. |
| Keep direct validation and sync status as separate surfaces | `wiki_validate_wiki` should still rescan; `wiki_refresh` and UI status should still reflect sync status. They must share mapping rules. |
| Use the canonical plan suffixes for this plan's artifacts | The `*.PLAN.md`, `*.OBSERVE.md`, `*.CONTEXT.md`, and `*.DECISION.md` filenames are part of the desired workflow contract, so this plan should exercise the same naming scheme it intends to support. |

## Alternatives Rejected

- Rename only the moved plan files: rejected because it fixes one checkout but not the LeafWiki behavior.
- Special-case plan file suffixes: rejected because the bug is broader than plan files.
- Allow dots and underscores in slugs: rejected because it changes the public route contract.
- Automatically suffix every normalized collision: rejected because reconstruction should surface ambiguous disk truth instead of inventing routes.
- Normalize every valid segment to lowercase: rejected because it changes existing valid routes unnecessarily.

## Open Questions Resolved

- Q: Should `docs/plans/agent_hooks.PLAN.md` import as a page?
  - A: Yes, under a normalized route such as `plans/agent-hooks-plan`.

- Q: Should `docs/assets` become `assets-1`?
  - A: No. The top-level static asset directory is skipped as non-page content.

- Q: Should `README.md` show as the main wiki page?
  - A: Yes, when it is `docs/README.md` and no `docs/index.md` exists. It should render at `/`, not as a separate `/README` child page.

- Q: How should users navigate to root content from the Explorer?
  - A: Add a compact Home icon in the Explorer toolbar that navigates to `/`.

- Q: Should the route-normalization fix rewrite copied README links like `docs/workspace-sync.md`?
  - A: No. That is content cleanup or canonical-link work. This plan should preserve and document the validation signal.

- Q: Should raw history include the original filename?
  - A: Yes. Workspace sync captures the filesystem state before reconstruction or writeback.

- Q: Should `wiki_validate_wiki` read cached sync status?
  - A: No. It can rescan, but it must use the same mapper.

- Q: Should the plan also rename all existing plan files?
  - A: No. That is an optional cleanup after the route normalization behavior works.
