<!-- leafwiki
version: 1
page:
  id: 7rlxwOavR
  title: Wikid Frontd Extraction Decision
  created_at: "2026-06-16T10:58:35.72936356Z"
  updated_at: "2026-06-16T10:58:35.72936356Z"
  creator_id: system
  last_author_id: system
-->

# Wikid Frontd Extraction Decision

## Decision

Implement a compatibility-preserving runtime extraction for one project workspace by introducing hidden same-binary roles:

- `wikid` as the parent daemon, descriptor authority, supervisor, and owner of auth/user/session/API-key/OAuth services.
- `frontd` as the stable public HTTP ingress for frontend, auth, config, global branding, OAuth metadata/routes, health, and public HTTP MCP proxying.
- `workspaced` as the private workspace authority for page/tree/search/assets/import/revisions/sync behavior and MCP tools.

The implementation plan should use the existing project-daemon descriptor/control model as the baseline, keep `scripts/run.sh` stable, and build the extracted stack behind a hidden runtime switch before flipping the default.

## Key Decisions

| Decision | Rationale |
|---|---|
| Use `wikid`, not `sessiond` | The parent role owns supervision, descriptors, auth/user services, health, and runtime visibility, not only sessions |
| Use same-binary role processes | Proves descriptor, lifecycle, restart, private routing, and failure semantics before federation |
| Keep `run.sh` stable | Real MCP usage goes through the wrapper, including downstream NOWATCH-style configs |
| Allow direct CLI simplification | A cleaner internal cut is acceptable if wrapper/config/docs/tests preserve practical behavior |
| Support exactly one project workspace | Prevents federation scope creep while proving runtime boundaries |
| Move auth/user/API-key/OAuth storage to `wikid` | `workspaced` should not own identity or public auth validation |
| Do not migrate auth data | Admins recreate users/API keys; `wikid` starts fresh |
| Delete only known legacy auth DBs | Cleanup is bounded to `<data-dir>/users.db`, `<data-dir>/sessions.db`, and `<data-dir>/api_keys.db` |
| Keep admin bootstrap behavior | Operational setup should remain familiar after store relocation |
| Use verified actor context | `workspaced` enforces workspace operations from trusted private context, not public credentials |
| Keep public OAuth resource stable | Existing clients keep using `<public-origin><base-path>/mcp` |
| Keep branding global | No per-workspace branding in this extraction |
| Make `wikid` the only supervisor | Avoids split-brain lifecycle authority |
| Keep `frontd` up during workspace failure | Public config, auth, branding, health, and shell stay reachable while workspace routes return 503 |
| Preserve HTTP MCP streaming | `frontd` proxies without parsing MCP frames |
| Preserve STDIO MCP through `run.sh` | Foreground wrapper attaches to/starts `wikid` and bridges to current `workspaced` |

## Rejected Alternatives

- Implement multi-workspace federation now.
- Keep all auth storage in `workspaced` and only move public routes.
- Use goroutines only for role boundaries.
- Publish workspace-specific OAuth resources.
- Allow old and new OAuth resource aliases.
- Use private `workspaced` URLs in OAuth metadata.
- Let `frontd` restart `workspaced`.
- Allow `workspaced` to expose a supported public UI.
- Add per-workspace branding.
- Migrate legacy auth DB contents.

## Implementation Direction

The implementation plan should:

1. Add role-aware descriptor/control primitives while preserving the existing attach path.
2. Build `wikid`, `frontd`, and `workspaced` package shells and test harnesses.
3. Move auth storage and services to `wikid` before route migration.
4. Split route registration so `frontd` owns public ingress and `workspaced` owns workspace semantics.
5. Add private verified actor-context forwarding and spoofing tests.
6. Proxy workspace routes and HTTP MCP through `frontd`.
7. Keep STDIO MCP stable through `scripts/run.sh`.
8. Add bounded supervision and failure reporting.
9. Run legacy and extracted parity tests.
10. Flip the default only after the wrapper and runtime matrix is green.

## Acceptance Gates

- `workspaced` starts without opening user, session, API-key, OAuth, or branding stores.
- `frontd` exposes public auth/config/OAuth/branding/static/health routes.
- `frontd` proxies workspace routes and public `/mcp` to `workspaced` with verified context.
- Public actor-context headers are ignored or rejected unless accompanied by valid private daemon auth.
- `run.sh mcp` and `run.sh agent-hook` keep their current command shape and stdout/secret behavior.
- OAuth metadata and `WWW-Authenticate` challenges use the public `frontd` URL.
- Legacy auth DB cleanup deletes only the three accepted old files.
- `wikid` supervises child roles with bounded backoff and clear health states.
- The extracted runtime passes the same core behavior tests as the legacy one before it becomes default.

## Residual Implementation Details

The following are intentionally left to the plan and implementation:

- Exact Go struct names and schema constants.
- Descriptor v2 pointer-vs-inline compatibility shape.
- Actor-context encoder format and expiry durations.
- Registrar migration order.
- Exact test filenames for new package-local tests.
- Whether old route packages are physically moved or initially reused through new composition roots.
