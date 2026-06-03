# Local MCP Interface

LeafWiki can expose MCP through a local Streamable HTTP endpoint, native STDIO, or both from one `leafwiki` process. MCP is disabled by default.

Use the transport selector:

```bash
leafwiki --mcp=http --host 127.0.0.1 --allow-insecure=true --jwt-secret=<secret> --admin-password=<password>
leafwiki --mcp=stdio --disable-auth=true --host 127.0.0.1 --root-dir ./wiki --data-dir ./.wiki
leafwiki --mcp=http,stdio --host 127.0.0.1 --allow-insecure=true --jwt-secret=<secret> --admin-password=<password>
```

Supported values are `none`, `http`, `stdio`, `http,stdio`, and `stdio,http`. The `LEAFWIKI_MCP` environment variable accepts the same values. CLI flags take precedence over environment variables.

The HTTP endpoint is:

```text
http://127.0.0.1:8080/mcp
```

When `--base-path /wiki` is configured, the endpoint is `http://127.0.0.1:8080/wiki/mcp`.

Transport unification is tracked by `codex://threads/019e8a0f-9674-7780-b6ee-1cfd7be07f67`.

## Security Model

MCP only starts on a loopback host: `localhost`, `127.0.0.1`, or `::1`. Do not expose MCP through Docker port publishing, a public reverse proxy, or a public network.

HTTP MCP accepts bearer tokens from OAuth or MCP-only API keys. Missing, invalid, expired, revoked, or insufficient-scope credentials are rejected before MCP requests reach tools. MCP requests do not use LeafWiki CSRF middleware, and MCP bearer credentials are separate from LeafWiki web JWT cookies.

Native STDIO supports two identities:

- Disabled auth: `--mcp=stdio --disable-auth=true` uses the same `public-editor` actor as the disabled-auth web UI.
- API key: set `LEAFWIKI_MCP_API_KEY=lwk_<id>_<secret>` or pass `--api-key`. Prefer the environment variable because CLI arguments can appear in process listings.

Native STDIO rejects `--log-target stdout` because stdout is reserved for newline-delimited JSON-RPC frames. Diagnostics go to stderr or the configured log file.

The `--api-key` flag and `LEAFWIKI_MCP_API_KEY` apply only to native STDIO. HTTP MCP remains bearer/OAuth protected through normal request headers.

## Native STDIO Wrapper

For project-local MCP clients, use `scripts/run-mcp.sh`. It starts one native `leafwiki --mcp=stdio` process. The same process serves the browser UI on `127.0.0.1:8080` and MCP over STDIO.

```bash
./scripts/run-mcp.sh --root-dir ./wiki --data-dir ./.wiki
```

MCP client JSON:

```json
{
  "mcpServers": {
    "leafwiki": {
      "command": "/Users/<you>/github/leafwiki/scripts/run-mcp.sh",
      "args": [
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
LEAFWIKI_JWT_SECRET=<secret> \
LEAFWIKI_ADMIN_PASSWORD=<password> \
./scripts/run-mcp.sh --root-dir ./wiki --data-dir ./.wiki
```

The wrapper keeps stdout reserved for MCP protocol frames. Wrapper diagnostics and LeafWiki startup messages go to stderr. Use absolute paths in MCP client configuration unless you know the client process has the expected working directory and `PATH`.

Path safety matters: `--root-dir ./wiki --data-dir ./.wiki` keeps managed Markdown and app state separate. `--root-dir . --data-dir ./.wiki` is invalid because the root directory would contain the data directory. LeafWiki locks both the data directory and root directory so a second active process using the same state fails fast.

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

The web UI and MCP server run in the same LeafWiki process and share the same `*wiki.Wiki` instance. Pages, search indexes, link indexes, tags, properties, assets, revisions, and refactor operations are updated through the same domain use cases used by the HTTP API.

This means an agent can create or update a page through MCP and a human can immediately see it in the UI. A human can edit through the UI and an agent can read the updated page through MCP.

## Tools

Tool names are intentionally unprefixed.

Always available:

- `get_config`
- `get_current_user`
- `get_tree`
- `get_page`
- `get_page_by_path`
- `lookup_path`
- `resolve_permalink`
- `suggest_slug`
- `create_page`
- `update_page`
- `delete_page`
- `move_page`
- `sort_pages`
- `ensure_page`
- `convert_page`
- `copy_page`
- `search_pages`
- `get_search_status`
- `list_tags`
- `get_pages_by_tags`
- `list_property_keys`
- `get_pages_by_property`
- `get_link_status`
- `upload_asset`
- `get_asset`
- `list_assets`
- `rename_asset`
- `delete_asset`

`get_page` and `get_page_by_path` return `{ page, linkStatus }`. The `linkStatus` field uses the same shape as `get_link_status.status` and includes backlinks, broken incoming links, outgoing links, broken outgoing links, and counts so page reads carry the document context shown in the web UI.

Only available with `--enable-revision`:

- `list_revisions`
- `get_latest_revision`
- `get_revision`
- `compare_revisions`
- `get_revision_asset`
- `restore_revision`

Only available with `--enable-link-refactor`:

- `preview_page_refactor`
- `apply_page_refactor`

## Unsupported Operations

MCP intentionally does not expose importer operations, branding operations or branding resources, login, refresh token, logout, password change, API-key management, user administration, or admin-only settings.

## Pagination

`search_pages` uses LeafWiki's HTTP search contract with `offset` and `limit`, and returns `count`, `items`, `limit`, `offset`, `tagFacets`, and `hasMore`.

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
| Startup rejects MCP | Host is not loopback | Bind to `127.0.0.1`, `localhost`, or `::1` |
| Native STDIO rejects logging | `--log-target stdout` is set | Use `file` or `stderr` |
| Native STDIO rejects API-key auth | Missing or invalid `LEAFWIKI_MCP_API_KEY` | Create a current MCP API key and pass it in the child environment |
| Viewer key can read but writes fail | Viewer role lacks mutation permission | Use an editor/admin key for write tools |
| JSON parse errors | Something wrote non-protocol text to stdout | Ensure wrappers and launch scripts write diagnostics to stderr only |

## Verification Contract

The local MCP surface is complete only when every defined MCP tool has HTTP/MCP parity coverage or is correctly absent when gated, OAuth coverage passes, native STDIO coverage passes, and the full project verification passes:

```bash
rtk go test ./cmd/leafwiki ./internal/wiki ./internal/wiki/mcp ./internal/locking
rtk go test ./...
rtk bash -n scripts/run-mcp.sh scripts/test-run-mcp.sh scripts/install-all-macos.sh scripts/test-install-all-macos.sh
rtk bash scripts/test-run-mcp.sh
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
```
