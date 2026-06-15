<!-- leafwiki
version: 1
page:
  id: 6Pjd2VavR
  title: Local MCP Interface
  created_at: "2026-06-14T13:51:34.183008424Z"
  updated_at: "2026-06-14T13:51:34.183008424Z"
  creator_id: system
  last_author_id: system
-->

# Local MCP Interface

LeafWiki can expose MCP through a local Streamable HTTP endpoint, native STDIO, or both through one per-project owner daemon. MCP is disabled by default.

Use the transport selector:

```bash
leafwiki --mcp=http --host 127.0.0.1 --allow-insecure=true --jwt-secret=<secret> --admin-password=<password>
leafwiki --mcp=stdio --disable-auth=true --host 127.0.0.1 --root-dir ./wiki --data-dir ./.wiki
leafwiki --mcp=http,stdio --host 127.0.0.1 --allow-insecure=true --jwt-secret=<secret> --admin-password=<password>
leafwiki --config ./leafwiki.yml
```

Supported `--mcp` values are `none`, `http`, `stdio`, `http,stdio`, and `stdio,http`. The `LEAFWIKI_MCP` environment variable accepts the same values. CLI flags take precedence over environment variables. In config-file mode, `mcp: stdio` or `mcp: http,stdio` uses the same values, YAML keys override environment variables when present, and omitted YAML keys still use environment variables/defaults.

The HTTP endpoint is:

```text
http://127.0.0.1:8080/mcp
```

When `--base-path /wiki` is configured, the endpoint is `http://127.0.0.1:8080/wiki/mcp`.
`--markdown-link-root-prefix` is unrelated to the HTTP mount path: it only affects how authored Markdown hrefs such as `/docs/sync/glossary.md` are interpreted and generated when the wiki root is the `docs` directory.

Transport unification is tracked by `codex://threads/019e8a0f-9674-7780-b6ee-1cfd7be07f67`. The transparent project daemon design is tracked by `codex://threads/019e8df8-d944-71f3-956e-59eb999abdff`. The context-first agent collaboration surface is tracked by `codex://threads/019e9c9b-92bc-74c1-820e-a758f051d779`.

YAML config-file startup is tracked by `codex://threads/019eb504-6cf3-7803-b033-bee91c3b028b`. `--config <path>` is mutually exclusive with normal CLI flags. The file is a flat mapping whose keys mirror public CLI flags without `--`; unknown, duplicate, non-scalar, hidden compatibility, and internal-only keys fail startup. The config path itself is not part of project daemon identity.

## Transparent Project Daemon

Every normal LeafWiki startup joins a per-project owner daemon. The project identity is the canonical resolved `(data-dir, root-dir)` pair. The first compatible startup starts the owner and writes `<data-dir>/.leafwiki/project-daemon.json` with mode `0600`; later compatible startups register session handles and attach without taking data/root ownership locks themselves.

Daemon-relevant configuration must match the first owner for owner-affecting starts: auth mode, host, port, base path, Markdown link root prefix, public access, insecure-cookie setting, token timeouts, UI injection/style settings, hidden link-metadata setting, upload size, revision/workspace-sync/link-refactor settings, max revision history, remote-user settings, request logging, and `--daemon-idle-timeout`. STDIO-only session frontends cannot change owner settings, so they inherit the owner value for host, public HTTP MCP, log target/file, and request logging while still matching the rest of the project identity. Per-session STDIO settings are not part of daemon identity: `--mcp=stdio`, `--api-key`, `LEAFWIKI_MCP_API_KEY`, and MCP client name/version can differ per attaching client.

The owner exits after active project activity reaches zero and `--daemon-idle-timeout` elapses. Project activity is the sum of registered session handles and active in-memory agent presence. Agent presence records expire on the same idle-timeout cadence, so a hook-started owner stays alive while the observed agent session is fresh and can shut down once that presence expires. The default is `10m`; `0` stops immediately after the last handle or presence record is gone. Stale descriptors are ignored when the private control endpoint is unreachable and the project locks are free.

## Security Model

Public HTTP MCP only starts on a loopback host: `localhost`, `127.0.0.1`, or `::1`. Native STDIO may attach to an owner bound to a non-loopback web host because STDIO traffic goes through the private loopback control server, not public `/mcp`. Do not expose public HTTP MCP through Docker port publishing, a public reverse proxy, or a public network.

HTTP MCP accepts bearer tokens from OAuth or MCP-only API keys. Missing, invalid, expired, revoked, or insufficient-scope credentials are rejected before MCP requests reach tools. MCP requests do not use LeafWiki CSRF middleware, and MCP bearer credentials are separate from LeafWiki web JWT cookies.

Native STDIO supports two identities:

- Disabled auth: `--mcp=stdio --disable-auth=true` uses the same `public-editor` actor as the disabled-auth web UI.
- API key: set `LEAFWIKI_MCP_API_KEY=lwk_<id>_<secret>` or pass `--api-key`. Prefer the environment variable because CLI arguments can appear in process listings.

Native STDIO rejects `--log-target stdout` because stdout is reserved for newline-delimited JSON-RPC frames. Foreground STDIO validation and bridge diagnostics can go to stderr, but a STDIO-spawned detached owner writes startup and server logs to the configured log file even when the frontend requested `--log-target stderr`.

The `--api-key` flag and `LEAFWIKI_MCP_API_KEY` apply only to native STDIO session frontends. They are never written to the project daemon descriptor and are not used for daemon config matching. HTTP MCP remains bearer/OAuth protected through normal request headers.

## Native STDIO Wrapper

For project-local MCP clients, use `scripts/run.sh mcp`. It starts an agent-owned `leafwiki --mcp=stdio` session frontend. That foreground process speaks MCP over stdin/stdout and attaches to the project owner daemon that serves the browser UI on `127.0.0.1:8080`.

```bash
./scripts/run.sh mcp --root-dir ./wiki --data-dir ./.wiki
./scripts/run.sh mcp --config ./leafwiki.yml
```

In config-file mode the wrapper does not add `--mcp=stdio`; put `mcp: stdio` or `mcp: http,stdio` in `leafwiki.yml` for clients that spawn `run.sh mcp`.

MCP client JSON:

```json
{
  "mcpServers": {
    "leafwiki": {
      "command": "/Users/<you>/github/leafwiki/scripts/run.sh",
      "args": [
        "mcp",
        "--root-dir",
        "./wiki",
        "--data-dir",
        "./.wiki"
      ]
    }
  }
}
```

Authenticated native STDIO:

```bash
LEAFWIKI_MCP_API_KEY=lwk_<id>_<secret> \
./scripts/run.sh mcp --root-dir ./wiki --data-dir ./.wiki
```

Config-file native STDIO:

```yaml
mcp: stdio
data-dir: ./.wiki
root-dir: ./wiki
disable-auth: true
enable-workspace-sync: true
```

```bash
./scripts/run.sh mcp --config ./leafwiki.yml
```

`LEAFWIKI_JWT_SECRET` and `LEAFWIKI_ADMIN_PASSWORD` are only needed when this
wrapper invocation must bootstrap a new auth-enabled owner. Existing compatible
owners accept API-key STDIO attaches without those bootstrap secrets.

The wrapper keeps stdout reserved for MCP protocol frames. Wrapper diagnostics and foreground validation errors go to stderr. Wrapper-launched owner startup and server logs go to `<data-dir>/.leafwiki/logs/leafwiki.log` by default. Use absolute paths in MCP client configuration unless you know the client process has the expected working directory and `PATH`.

Path safety matters: `--root-dir ./wiki --data-dir ./.wiki` keeps managed Markdown and app state separate. `--root-dir . --data-dir ./.wiki` is invalid because the root directory would contain the data directory. The project owner daemon holds both data and root locks; compatible session frontends attach to it instead of creating lock conflicts.

## OAuth Client Settings

OAuth-capable MCP clients should use OAuth discovery and Dynamic Client Registration.

- MCP endpoint: `<origin><basePath>/mcp`
- Client ID: dynamically registered; manual/testing fallback `leafwiki-local-mcp`
- Client authentication: public client, no secret
- Scope: `leafwiki:mcp`
- PKCE: required, `S256`
- Authorization endpoint: `<origin><basePath>/oauth/authorize`
- Token endpoint: `<origin><basePath>/oauth/token`
- Dynamic client registration endpoint: `<origin><basePath>/oauth/register`

LeafWiki also publishes OAuth discovery metadata:

- `/.well-known/oauth-protected-resource`
- `/.well-known/oauth-protected-resource/mcp`
- `/.well-known/oauth-protected-resource/<base-path>/mcp`
- `/.well-known/oauth-authorization-server`
- `/.well-known/oauth-authorization-server/<base-path>`

OAuth authorization requires a logged-in LeafWiki web user and an explicit browser approval step before an authorization code is issued. The OAuth token identifies a LeafWiki user. LeafWiki loads the current user on every MCP request, so deleting the user or changing the user role affects existing tokens immediately.

OAuth tokens use in-memory server storage in this MVP. Server restart requires clients to re-authorize. There is no revocation endpoint.

## MCP API Keys

OAuth remains the recommended path for OAuth-capable MCP clients. MCP API keys are an advanced/manual option for clients that can send a fixed bearer token or spawn native STDIO with an environment variable.

API keys use the same MCP scope and bearer format:

```text
Authorization: Bearer lwk_<id>_<secret>
```

Admins can create, list, and revoke MCP API keys for any user from User Management. Users can list and revoke their own keys from the account menu. Under password authentication, creating your own key requires the current password. Under trusted HTTP remote-user authentication, self-service creation is disabled in this MVP; existing keys can still be listed and revoked.

The raw key secret is shown once when the key is created. LeafWiki stores only a hash of the full raw key plus metadata such as name, prefix, last four characters, owner, creator, creation time, last-used time, and revocation time. List responses never return the raw secret.

MCP API keys do not expire in this MVP. They inherit the owner's current role on every MCP request, so role downgrades affect existing keys immediately. Revoked keys, deleted-user keys, malformed keys, and wrong-secret keys are rejected before tools run.

## Collaboration Model

All MCP transports operate against the same owner LeafWiki wiki state as the web UI. Public HTTP MCP is served by the owner process; native STDIO clients attach through the project daemon and bridge to the owner's private MCP endpoint. Pages, search indexes, link indexes, tags, properties, assets, revisions, and refactor operations are updated through the same domain use cases used by the HTTP API.

This means an agent can create or update a page through MCP and a human can immediately see it in the UI. A human can edit through the UI and an agent can read the updated page through MCP.

Agents should call `wiki_get_context` before other tools. It returns the current user/config, enabled tools, workspace sync status, validation summary, recent changes, active web/agent sessions, a compact tree, recommended next tools, and an opaque context token. Later calls can pass `sinceToken` to see whether the workspace changed since a prior context response.

`wiki_get_config` and `wiki_get_context.config` include `markdownLinkRootPrefix`. When it is set, Markdown processors strip that prefix from absolute internal hrefs before route lookup and add it to generated absolute Markdown links. MCP page path inputs remain LeafWiki route paths; pass `sync/glossary`, not `docs/sync/glossary`, when calling route-oriented tools.

The normal edit path is semantic MCP writes such as `wiki_update_page`, `wiki_update_page_metadata`, or `wiki_replace_page_section`. When workspace sync is enabled, direct Markdown file edits under `--root-dir` can be useful for large mechanical body changes. Preserve any top-of-file `<!-- leafwiki ... -->` metadata block exactly, edit page body content below that block, and use `wiki_update_page_metadata` for tags and properties instead of hand-editing metadata. Then call `wiki_refresh` when humans or following tools need immediate web visibility, and run validation before reporting done.

Most MCP page-path arguments are route paths such as `/docs/api`. Path-based page lookup, validation, and subtree tools also accept canonical Markdown file paths: `/docs/api.md` selects the page, `/docs/api/index.md` selects the section, and an active `/docs/api/README.md` fallback selects the section it backs. Route-structure tools such as `wiki_lookup_path` remain route-oriented and use an explicit `kind` argument when page and section twins share a route. Markdown content should use LeafWiki's canonical link format: page links end in `.md`, section links omit `.md`. Workspace sync and import canonicalize resolvable legacy page links before validation.

When workspace sync scans raw files, common documentation filenames are normalized into route-safe paths before validation and refresh status are reported. For example, `plans/agent_hooks.PLAN.md` validates as `/plans/agent-hooks-plan.md`; `wiki_validate_wiki` and `wiki_refresh` use the same mapping and report `path_conflict` if two source files normalize to the same route. This does not make raw filenames permanent route aliases.

Presence is advisory and privacy-filtered. Web heartbeats are in memory, expire after 90 seconds, and expose sanitized user id/name/role plus page id/path/title when resolvable. User email is visible only to admin MCP callers. Agent hook presence is also in memory and exposes only provider, provider-scoped session hash, model/source metadata, event name, and timestamps; raw hook payloads, prompts, transcript paths, session ids, API keys, JWTs, and daemon control tokens are never exposed through `wiki_get_context`.

Example validation and narrow edit flow:

```json
{"tool":"wiki_get_context","arguments":{"syncMode":"auto","treeDepth":2}}
{"tool":"wiki_validate_page","arguments":{"path":"/docs/api"}}
{"tool":"wiki_get_page_by_path","arguments":{"path":"/docs/api"}}
{"tool":"wiki_update_page_metadata","arguments":{"path":"/docs/api","version":"<page.version>","addTags":["api"],"setProperties":{"status":"ready"}}}
{"tool":"wiki_replace_page_section","arguments":{"path":"/docs/api","version":"<version-from-metadata-result>","headingPath":["Authentication"],"content":"New section body\\n"}}
{"tool":"wiki_validate_page","arguments":{"path":"/docs/api"}}
```

## Tools

Tool names use the canonical `wiki_*` prefix. LeafWiki is pre-release, so the
old unprefixed names were removed rather than kept as aliases; clients must
migrate legacy unprefixed calls to their canonical prefixed names.

Always available:

- `wiki_get_context`
- `wiki_get_subtree`
- `wiki_validate_page`
- `wiki_validate_content`
- `wiki_validate_wiki`
- `wiki_update_page_metadata`
- `wiki_replace_page_section`
- `wiki_get_config`
- `wiki_get_current_user`
- `wiki_get_tree`
- `wiki_get_page`
- `wiki_get_page_by_path`
- `wiki_lookup_path`
- `wiki_resolve_permalink`
- `wiki_suggest_slug`
- `wiki_create_page`
- `wiki_update_page`
- `wiki_delete_page`
- `wiki_move_page`
- `wiki_sort_pages`
- `wiki_ensure_page`
- `wiki_convert_page`
- `wiki_copy_page`
- `wiki_search_pages`
- `wiki_get_search_status`
- `wiki_list_tags`
- `wiki_get_pages_by_tags`
- `wiki_list_property_keys`
- `wiki_get_pages_by_property`
- `wiki_get_link_status`
- `wiki_upload_asset`
- `wiki_get_asset`
- `wiki_list_assets`
- `wiki_rename_asset`
- `wiki_delete_asset`

`wiki_get_page` and `wiki_get_page_by_path` return `{ page, linkStatus }`. The `linkStatus` field uses the same shape as `wiki_get_link_status.status` and includes backlinks, broken incoming links, outgoing links, broken outgoing links, and counts so page reads carry the document context shown in the web UI.

Only available with `--enable-workspace-sync`:

- `wiki_refresh`

Only available with `--enable-revision` or `--enable-workspace-sync`:

- `wiki_list_revisions`
- `wiki_get_latest_revision`
- `wiki_get_revision`
- `wiki_compare_revisions`
- `wiki_get_revision_asset`
- `wiki_restore_revision`

When revision tools are exposed through workspace sync, page revision content comes from Git workspace snapshots. Revision asset reads still require the revision service; workspace sync revisions do not track historical asset blobs.

Only available with `--enable-link-refactor`:

- `wiki_preview_page_refactor`
- `wiki_apply_page_refactor`

## Unsupported Operations

MCP intentionally does not expose importer operations, branding operations or branding resources, login, refresh token, logout, password change, API-key management, user administration, or admin-only settings.

## Pagination

`wiki_search_pages` uses LeafWiki's HTTP search contract with `offset` and `limit`, and returns `count`, `items`, `limit`, `offset`, `tagFacets`, and `hasMore`.

MCP protocol pagination is only for MCP feature lists such as `ListTools`. LeafWiki sets a deterministic tool-list page size in tests so SDK cursor behavior is covered.

## Assets

Asset uploads use:

```json
{ "pageId": "...", "filename": "note.txt", "contentBase64": "..." }
```

Asset reads return:

```json
{ "filename": "note.txt", "mimeType": "text/plain; charset=utf-8", "contentBase64": "..." }
```

## Troubleshooting

| Symptom | Likely cause | Fix |
|---|---|---|
| Startup rejects HTTP MCP | Host is not loopback | Bind public HTTP MCP to `127.0.0.1`, `localhost`, or `::1`; native STDIO can attach through private loopback control |
| Native STDIO rejects logging | `--log-target stdout` is set | Use `file`; `stderr` is accepted for foreground STDIO diagnostics, while detached owner logs are retained in the log file |
| Native STDIO rejects API-key auth | Missing or invalid `LEAFWIKI_MCP_API_KEY` | Create a current MCP API key and pass it in the child environment |
| Startup rejects with config mismatch | A project owner already exists with different daemon-relevant settings | Stop existing LeafWiki sessions or restart the owner with the desired host/port/base-path/auth/MCP settings |
| `--mcp=http` cannot attach | The existing owner was started without public HTTP MCP | Stop/restart the owner with `--mcp=http` or `--mcp=stdio,http` |
| Stale descriptor confusion | `<data-dir>/.leafwiki/project-daemon.json` points to a dead owner | Restart LeafWiki; unreachable descriptors are ignored and replaced once the project locks are free |
| Viewer key can read but writes fail | Viewer role lacks mutation permission | Use an editor/admin key for write tools |
| JSON parse errors | Something wrote non-protocol text to stdout | Ensure wrappers and launch scripts keep diagnostics off stdout |

## Verification Contract

The local MCP surface is complete only when HTTP-backed tools have HTTP/MCP parity coverage, MCP-only tools have focused MCP coverage, gated tools are correctly absent when disabled, OAuth coverage passes, native STDIO coverage passes, and the full project verification passes. MCP-only coverage includes the agent context, workspace refresh, subtree, validation, safe partial edit, and presence specs:

```bash
rtk go test ./cmd/leafwiki ./internal/projectdaemon ./internal/wiki ./internal/wiki/mcp ./internal/locking
rtk go test ./...
rtk bash -n scripts/run.sh scripts/test-run.sh scripts/install-all-macos.sh scripts/test-install-all-macos.sh
rtk bash scripts/test-run.sh
rtk bash scripts/test-install-all-macos.sh
rtk npm --prefix e2e run lint
rtk npm --prefix e2e run format:check
rtk npm --prefix ui/leafwiki-ui run lint
rtk npm --prefix ui/leafwiki-ui run build
rtk env E2E_RUN_MODE=local E2E_ENABLE_MCP_LOCAL=1 E2E_MCP_CLIENT_TRANSPORT=stdio ./e2e/run.sh tests/mcp-stdio-disable-auth.spec.ts
rtk env E2E_RUN_MODE=local E2E_ENABLE_MCP_LOCAL=1 E2E_BASE_PATH=/wiki E2E_MCP_CLIENT_TRANSPORT=stdio ./e2e/run.sh tests/mcp-stdio-disable-auth.spec.ts --grep "base-path"
rtk env E2E_RUN_MODE=local E2E_ENABLE_MCP_LOCAL=1 E2E_ENABLE_SEPARATE_ROOT_DIR=1 E2E_MCP_CLIENT_TRANSPORT=stdio ./e2e/run.sh tests/mcp-stdio-disable-auth.spec.ts --grep "mcp stdio seeds"
rtk env E2E_RUN_MODE=local E2E_ENABLE_MCP_API_KEYS_LOCAL=1 E2E_MCP_CLIENT_TRANSPORT=stdio ./e2e/run.sh tests/mcp-stdio-api-keys.spec.ts
rtk env E2E_RUN_MODE=local E2E_ENABLE_MCP_LOCAL=1 ./e2e/run.sh tests/mcp-disable-auth.spec.ts
rtk env E2E_RUN_MODE=local E2E_ENABLE_MCP_API_KEYS_LOCAL=1 ./e2e/run.sh tests/mcp-api-keys.spec.ts
rtk env E2E_RUN_MODE=local E2E_ENABLE_MCP_OAUTH_LOCAL=1 ./e2e/run.sh tests/mcp-oauth.spec.ts
rtk env E2E_RUN_MODE=local E2E_ENABLE_MCP_LOCAL=1 E2E_ENABLE_WORKSPACE_SYNC=1 ./e2e/run.sh tests/mcp-agent-context.spec.ts
rtk env E2E_RUN_MODE=local E2E_ENABLE_MCP_LOCAL=1 E2E_ENABLE_WORKSPACE_SYNC=1 ./e2e/run.sh tests/mcp-safe-edits.spec.ts
rtk env E2E_RUN_MODE=local E2E_ENABLE_MCP_LOCAL=1 E2E_ENABLE_WORKSPACE_SYNC=1 ./e2e/run.sh tests/presence.spec.ts
rtk env E2E_RUN_MODE=local E2E_ENABLE_AGENT_HOOKS_LOCAL=1 E2E_ENABLE_MCP_LOCAL=1 E2E_MCP_CLIENT_TRANSPORT=stdio ./e2e/run.sh tests/agent-hooks.spec.ts
```
