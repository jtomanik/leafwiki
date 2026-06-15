<!-- leafwiki
version: 1
page:
  id: plan-workspace-route-context
  title: Workspace Markdown Route Normalization - Context
  created_at: "2026-06-14T18:41:17Z"
  updated_at: "2026-06-14T18:54:52Z"
  creator_id: system
  last_author_id: system
-->

# Workspace Markdown Route Normalization - Context

## Problem Frame

LeafWiki currently treats the filesystem path as both the source path and the route path. That works only when Markdown filenames already look like LeafWiki slugs. It fails for common documentation filenames such as `agent_hooks.PLAN.md`, because the route slug rules are stricter than normal file naming conventions.

The move from `plans/` to `docs/plans/` exposed the mismatch. The files are legitimate Markdown source files, but workspace reconstruction and validation reject them before LeafWiki can show them.

## Design Pressure

This is not just a UI bug. The same path-to-route mapping is needed by:

- filesystem reconstruction
- workspace validation
- markdown-link indexing and canonical link migration
- workspace sync status
- MCP validation and refresh responses
- HTTP status consumed by the Explorer banner

If one layer normalizes filenames and another layer keeps raw paths, the wiki will import pages under one route while validation and link checks report different paths.

## Alternatives Considered

### Option A: Rename the moved files to route-safe filenames

Example: rename `agent_hooks.PLAN.md` to `agent-hooks-plan.md`.

This would clear the immediate local error, but it would make LeafWiki fragile for the next documentation import with underscores, spaces, dots, or reserved names. It also avoids fixing the inconsistency between explicit importer behavior and workspace-sync reconstruction.

Decision: rejected as the product fix. It remains a valid local workaround.

### Option B: Special-case `*.PLAN.md`

LeafWiki could strip known plan suffixes and accept the current plan naming convention.

This is too narrow. The same bug exists for `CODE_OF_CONDUCT.md`, `meeting notes.md`, `Architecture.Decision.md`, `API.md`, and any other non-slug Markdown filename.

Decision: rejected.

### Option C: Broaden slug validation

LeafWiki could allow underscores, dots, and other filename characters in route slugs.

This would expand the public route contract, interact badly with static asset namespaces and extension-based canonical page links, and force every route consumer to handle more ambiguous path syntax.

Decision: rejected.

### Option D: Shared filesystem-to-route normalization

Treat messy filesystem names as ingestion input and map them to LeafWiki route-safe slugs before reconstruction, validation, and link indexing. Preserve already-valid slugs to avoid needless route churn. Report deterministic conflicts when two filesystem paths normalize to the same route.

This matches the importer precedent and keeps the public route contract strict.

Decision: selected.

## Required Behavioral Shape

The selected behavior is:

- Valid route slugs keep their current route value.
- Invalid file basenames normalize to valid route slugs.
- Invalid directory segments normalize to valid route slugs when the directory contains wiki Markdown content.
- Root-level `assets/` is treated as non-page static content and skipped by tree reconstruction and workspace slug validation.
- `index.md` and active `README.md` retain their section-content semantics.
- Non-active `README.md` remains a page.
- Dot directories remain ignored.
- Two distinct files that normalize to the same route produce a validation or reconstruction conflict rather than silent overwrite.

## Root README and Home Navigation

The later README symptom is adjacent to route normalization but should not be fixed by changing backend sentinel semantics.

With wiki root `docs`, a repository-root `README.md` is outside the workspace import root and should not appear in the wiki. A `docs/README.md` file is inside the wiki root. If `docs/index.md` is absent, `docs/README.md` is the active root section content and should render as the main wiki page at `/`.

The current frontend prevents that in normal navigation because `/` renders `RootRedirect`, and `RootRedirect` redirects to the first root child whenever children exist. That behavior makes sense for a wiki with no root body, but it conflicts with README fallback when the root section has content.

The selected frontend shape is:

- `/` renders the root page/section when the root has content or an active README fallback.
- The Explorer sidebar includes a compact Home icon that navigates to `/`.
- The existing logo/title link can continue pointing to `/`.
- `RootRedirect` is removed or narrowed to only the truly empty-root case, if such a redirect is still wanted.

If a visible child README page is desired, the current model still requires either:

- a root `docs/index.md`, so `docs/README.md` becomes non-active and can be treated as a page; or
- a different filename that maps to a normal page route.

Copying the repository README into `docs/README.md` can also create broken links. Links written from the repository root, such as `docs/workspace-sync.md`, are no longer correct when the document itself is already inside the `docs` wiki root.

## Collision Policy

The plan chooses validation conflicts over automatic suffixing during reconstruction.

Reason: importer planning can show a proposed `TargetPath` before execution and can assign suffixes deliberately. Workspace reconstruction is rebuilding truth from disk. If `foo_bar.md` and `foo-bar.md` both normalize to `foo-bar`, silently choosing one or suffixing one could move routes without user intent. A conflict is safer and easier to repair.

Reserved words are different. A single reserved segment such as `API.md` can normalize to `api-1`, following existing `GenerateValidSlug` behavior, because there is no competing route unless another file also claims it.

## Static Asset Directory Policy

The visible `assets` issue comes from `docs/assets`, which contains image files referenced by docs. LeafWiki already owns `/assets` as the managed asset route namespace, so a top-level `assets` directory under the wiki root cannot become a wiki section.

The selected plan treats top-level `assets/` as non-page static content for workspace scans. This removes the reserved slug error without creating an `assets-1` page section or materializing `assets/index.md`.

This plan does not make arbitrary relative static images first-class LeafWiki managed assets. It only prevents the static directory itself from being reported as an invalid wiki section.

## MCP and UI Status Shape

`wiki_validate_wiki` and `wiki_refresh` do not currently use exactly the same source:

- `wiki_refresh` runs workspace sync and returns `syncStatus.validationErrors`.
- `wiki_validate_wiki` may rescan the workspace root directly.
- The UI banner reads stored HTTP sync status.

The plan should not collapse those surfaces into one cached result. Direct validation is useful because it can inspect unsynced files. The requirement is that all surfaces use the same mapper and therefore agree on whether `plans/agent_hooks.PLAN.md` is importable.

## Risk Analysis

- Route churn: normalizing existing invalid paths introduces new public routes. Preserve valid slug segments to reduce churn.
- Collision bugs: normalized paths can collide. Conflicts must be explicit.
- Link drift: markdown-link indexing must use normalized routes or link validation will disagree with imported pages.
- Section sentinel regressions: `index.md` and `README.md` behavior has many existing tests and must remain pinned.
- Root navigation regressions: `/` must expose root README content instead of redirecting away, while empty-root behavior remains coherent.
- Static asset ambiguity: skipping top-level `assets/` should not skip nested wiki content in other directories or managed page assets under the data directory.
- History truthfulness: workspace sync must capture incoming raw Markdown before any repair or metadata writeback.
