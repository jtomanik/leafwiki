<!-- leafwiki
version: 1
page:
  id: frsc-decision-20260620
  title: Federated Runtime Sync Cleanup - Decision
  created_at: "2026-06-20T22:13:23Z"
  updated_at: "2026-06-20T22:13:23Z"
  creator_id: system
  last_author_id: system
tags:
  - plans
  - decision
fields:
  type: cleanup
-->

# Federated Runtime Sync Cleanup - Decision

## Decision

Make the federated `wikid`/`frontd`/`workspaced` runtime and Git-backed workspace sync the only supported LeafWiki runtime/history path, and remove legacy selection, fallback, and compatibility surfaces that imply older modes still exist.

## Selected Course Of Action

1. Delete legacy runtime-stack selection.
   - Remove `RuntimeStackLegacy`.
   - Stop reading `LEAFWIKI_RUNTIME_STACK` as a selector.
   - Keep `RuntimeStackWikidFrontd` or equivalent constant metadata in descriptors for now.

2. Collapse startup to federated daemon.
   - Delete the old `attachOrStartProjectDaemon` path.
   - Delete the old single-workspace owner server branch.
   - Keep descriptor-first STDIO attach, global `wikid` ensure/register fallback, and role supervision.

3. Make workspace sync mandatory for workspace runtimes.
   - Remove `--enable-workspace-sync`, `--disable-workspace-sync`, YAML `enable-workspace-sync`, and related envs.
   - Remove `EnableWorkspaceSync` as an internal runtime mode switch.
   - Keep pure control-plane-only runtimes from opening workspace sync.

4. Remove legacy page-snapshot revision mode.
   - Remove `--enable-revision`, YAML `enable-revision`, `LEAFWIKI_ENABLE_REVISION`, and `max-revision-history` knobs.
   - Remove `revision.Service` wiring, baseline snapshot revisions, and page-save revision side effects.
   - Use Git-backed workspace revisions for HTTP, MCP, and UI history.
   - Ignore old snapshot revision data on disk.

5. Collapse API/UI mode branching.
   - Remove `enableRevision` from `/api/config`, MCP config, frontend config types, and schemas.
   - Keep `enableWorkspaceSync: true` temporarily only as a compatibility/capability signal if needed.
   - Make frontend history and workspace sync UI assume Git-backed behavior.

6. Remove stale MCP and wrapper compatibility.
   - Remove `--enable-mcp` and `--mcp-stdio` from the LeafWiki CLI.
   - Remove ignored sidecar-era wrapper flags.
   - Make old flags and old envs fail unknown/unsupported.

7. Remove legacy API-key seeding.
   - E2E seed helper always writes `wikid` auth stores.
   - Remove runtime-stack flag/env precedence and legacy auth-store tests.

8. Rewrite docs and tests.
   - Active docs describe the new default-only contract.
   - Historical plan docs stay with visible warnings.
   - Tests assert old flags/envs fail and current default paths work.

## Decision Table

| Topic | Decision | Reason |
|---|---|---|
| Runtime stack | Federated only | Legacy runtime no longer represents product behavior |
| Descriptor `runtimeStack` field | Keep as constant metadata for now | Avoid descriptor/hash churn unrelated to cleanup |
| Workspace sync | Mandatory for workspace runtimes | Git-backed Markdown state is core product behavior |
| Control-plane-only `Wiki` | No workspace sync requirement | It should not open workspace Git stores |
| Legacy snapshot revisions | Remove wiring, ignore old data | Avoid mode branching and migration complexity |
| Revision asset history | Unsupported in Git-backed history | No asset-history feature in this cleanup |
| Old flags | Unknown failure | User explicitly rejected ignored compatibility |
| Old envs | Explicit unknown/unsupported failure | Env vars do not fail automatically |
| `/api/config.enableRevision` | Remove | Legacy mode disappears |
| `/api/config.enableWorkspaceSync` | Keep true only if still useful | Temporary capability signal, not a mode switch |
| Disabled auth | Keep | Separate local workflow policy |
| Root `/mcp` | Keep | Current federated bootstrap behavior |
| Historical docs | Keep with warnings | Plan artifacts remain useful history |
| Legacy auth DB cleanup | Keep | Bounded upgrade protection |

## Alternatives Rejected

### Keep Old Flags Parseable

Rejected because stale configs would continue to look supported. Removed flags must fail unknown.

### Migrate Snapshot Revisions To Git

Rejected because the migration is high cost and not required for the new contract. Old data can remain on disk unread.

### Remove `projectdaemon` Package

Rejected because descriptor/control primitives still carry the current federated runtime and STDIO attach path.

### Remove Root `/mcp`

Rejected because root `/mcp` is still part of current federated bootstrap and single-workspace compatibility.

### Remove Disabled Auth

Rejected as out of scope. It is a local workflow and test posture decision, not a direct legacy runtime cleanup.

## Resolved Open Questions

- Q: Should old flags fail unknown or show tailored migration messages?
  - A: Fail unknown through normal parsers where possible.

- Q: Should old envs fail unknown?
  - A: Yes. Add explicit removed-env validation because env vars are otherwise silent.

- Q: Should old snapshot revisions be migrated?
  - A: No. Drop or ignore old snapshot data.

- Q: Should `enableRevision` stay in config responses?
  - A: No.

- Q: Should `enableWorkspaceSync` stay in config responses?
  - A: Keep it as `true` only if needed as a compatibility/capability signal. It must not drive branching.

- Q: Should legacy auth DB cleanup remain?
  - A: Yes.

- Q: Should historical docs be deleted?
  - A: No. Keep them with historical warnings.

## Implementation Posture

This is a deletion-heavy refactor with behavior changes. The implementer should:

- Start with tests that pin removed flags/envs failing.
- Remove upstream knobs before simplifying downstream branches.
- Prefer deleting dead branches over wrapping them with new constants.
- Keep Git-backed revision behavior green before deleting legacy revision code.
- Treat docs and E2E harness changes as part of the same feature, not cleanup afterthoughts.

