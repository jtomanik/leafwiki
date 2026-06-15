<!-- leafwiki
version: 1
page:
  id: u1h-uI-Dg
  title: Authenticated Local MCP via Minimal OAuth
  created_at: "2026-06-15T05:44:44.856502414Z"
  updated_at: "2026-06-15T05:44:44.856502414Z"
  creator_id: system
  last_author_id: system
-->

# Authenticated Local MCP via Minimal OAuth

## Summary

Implement authenticated MCP for LeafWiki’s local Streamable HTTP endpoint so users can run `--enable-mcp` without `--disable-auth`.

Implementation reference thread: `codex://threads/019e6e13-1e91-7070-be89-2a45203ea1f6`

Chosen MVP:

- Use minimal OAuth Authorization Code + PKCE for local MCP clients.
- Use `github.com/go-oauth2/oauth2/v4`.
- Keep MCP loopback-only.
- Keep existing disabled-auth MCP mode as a legacy fallback, but authenticated mode must work with normal LeafWiki auth enabled.
- Use one OAuth scope: `leafwiki:mcp`.
- No API keys.
- No revocation endpoint.
- Explicit local approval screen before issuing an authorization code.
- No Dynamic Client Registration.
- No Client ID Metadata Documents.
- Pre-register exactly one public client: `leafwiki-local-mcp`.
- User role controls actual rights: viewer can read; editor/admin can mutate.

## Key Changes

### OAuth Server Surface

Add a small local OAuth module/registrar backed by `go-oauth2/oauth2/v4`.

Public endpoints:

- `GET /.well-known/oauth-protected-resource`
- `GET /.well-known/oauth-protected-resource/<base-path>/mcp`
- `GET /.well-known/oauth-authorization-server`
- `GET /.well-known/oauth-authorization-server/<base-path>`
- `GET <base-path>/oauth/authorize`
- `POST <base-path>/oauth/authorize`
- `POST <base-path>/oauth/token`

Do not add `/oauth/revoke`.

Authorization server metadata must advertise:

- `issuer`: request origin plus `basePath`
- `authorization_endpoint`: `<origin><basePath>/oauth/authorize`
- `token_endpoint`: `<origin><basePath>/oauth/token`
- `response_types_supported`: `["code"]`
- `grant_types_supported`: `["authorization_code", "refresh_token"]`
- `code_challenge_methods_supported`: `["S256"]`
- `scopes_supported`: `["leafwiki:mcp"]`
- `token_endpoint_auth_methods_supported`: `["none"]`

Protected resource metadata must advertise:

- `resource`: `<origin><basePath>/mcp`
- `authorization_servers`: `[<origin><basePath>]`
- `scopes_supported`: `["leafwiki:mcp"]`

Use `go-oauth2/oauth2/v4` with:

- authorization code grant
- refresh token grant
- public client, no client secret
- PKCE required
- only `S256`, reject `plain`
- token lifetimes from existing LeafWiki access/refresh timeout config
- in-memory OAuth token storage for MVP; document that server restart requires clients to re-authorize

### Authorization Flow

`/oauth/authorize` behavior:

- Validate `client_id == "leafwiki-local-mcp"`.
- Validate `response_type=code`.
- Validate requested scope is empty or contains only `leafwiki:mcp`; issued scope is always `leafwiki:mcp`.
- Require `code_challenge` and `code_challenge_method=S256`.
- Validate `redirect_uri` is loopback-only:
  - scheme `http`
  - host `localhost`, `127.0.0.1`, or `::1`
  - any explicit port
  - no fragment
- If `resource` is supplied, require it to equal the current MCP resource URL.
- If the user is already authenticated by LeafWiki cookies or remote-user auth, show an approval page and only redirect back with `code` and original `state` after the user approves.
- If unauthenticated, redirect to `<base-path>/login?returnTo=<encoded original authorize URL>`.

Frontend login change:

- Update login flow to read a `returnTo` query parameter.
- After successful login, if `returnTo` is same-origin and points to `<base-path>/oauth/authorize`, use `window.location.assign(returnTo)`.
- Otherwise keep existing redirect to `/`.
- If an already logged-in user visits `/login?returnTo=...`, redirect/assign to `returnTo` using the same validation.

### MCP Protection

Change MCP registration so:

- `--enable-mcp` still requires loopback host.
- Auth-enabled mode no longer requires `--disable-auth`.
- Auth-enabled `/mcp` is wrapped with Go SDK `auth.RequireBearerToken`.
- Missing/invalid/expired bearer tokens return `401`.
- `WWW-Authenticate` includes:
  - `resource_metadata="<absolute protected resource metadata URL>"`
  - `scope="leafwiki:mcp"`
- Auth-disabled legacy mode may keep current `public-editor` behavior.

MCP token verifier:

- Use `go-oauth2` bearer token validation, not LeafWiki cookie auth and not existing web JWTs.
- Convert valid OAuth token info into SDK `auth.TokenInfo`.
- Set `TokenInfo.UserID` to the LeafWiki user ID.
- Set `TokenInfo.Scopes` to `["leafwiki:mcp"]`.
- Set `TokenInfo.Expiration` from OAuth token expiry.
- On every request, load the current LeafWiki user by ID.
- Reject if user no longer exists.
- Use current user role, not token-time role, for permission checks.

### MCP Actor And Permissions

Update MCP tool plumbing so tool handlers can access authenticated identity.

- Change the typed tool wrapper to pass `*sdkmcp.CallToolRequest` or a small internal `MCPActor`.
- Build actor from `req.Extra.TokenInfo.UserID`.
- `get_current_user` returns the real authenticated user in auth-enabled mode.
- Remove hardcoded `public-editor` from auth-enabled mutations.
- Keep `public-editor` only in legacy disabled-auth mode.

Permission policy:

- All MCP calls require OAuth scope `leafwiki:mcp`.
- Read tools are allowed for any authenticated user.
- Mutation tools require current user role `editor` or `admin`.
- Mutation tools must pass the authenticated user ID into existing use cases.
- Viewer write attempts return an MCP tool error equivalent to HTTP forbidden behavior.

Mutation tools requiring editor/admin include page create/update/delete/move/sort/ensure/convert/copy, asset upload/rename/delete, revision restore, and refactor apply/preview if the HTTP route requires editor/admin.

### MCP Page Link Metadata

MCP page-read tools should expose the same link context that users see at the bottom of the document view.

The web document view composes the page payload with link status from `/api/pages/:id/links`, showing:

- backlinks, rendered as "Referenced by"
- broken incoming links
- outgoing links
- broken outgoing links, rendered with broken-link counts
- aggregate link counts

Update MCP page outputs so agents do not have to infer this extra visible document context from a separate tool call:

- `get_page` must return `linkStatus` alongside `page`.
- `get_page_by_path` must return `linkStatus` alongside `page`.
- `linkStatus` must use the same payload shape returned by existing MCP `get_link_status` / HTTP `GET /api/pages/:id/links`.
- Keep `get_link_status` as a standalone tool for clients that only need link metadata.
- Reuse the existing link-status use case; do not duplicate link-index or backlink calculation logic in MCP handlers.
- Keep the existing page data shape stable under `page`; add link metadata as a top-level sibling field.
- Document this as document-view parity: page reads include the backlinks and broken-link context visible in the UI.

This behavior is independent of OAuth. It should work in both authenticated MCP mode and legacy disabled-auth MCP mode.

## Documentation And References

Update docs:

- `docs/mcp.md`
  - Replace disabled-auth-only model with authenticated local MCP as the primary mode.
  - Document legacy `--disable-auth --enable-mcp` mode separately.
  - Include OAuth client settings:
    - MCP endpoint: `<origin><basePath>/mcp`
    - client ID: `leafwiki-local-mcp`
    - auth method: public client / no secret
    - scope: `leafwiki:mcp`
    - PKCE: S256
  - Explain token refresh is handled by OAuth-capable MCP clients.
  - Explain no revocation endpoint in MVP; web logout does not revoke MCP tokens, and MCP access ends when user is removed, role changes, token expires, refresh expires, or server restarts.
  - Explain trusted remote-user OAuth authorization still requires local approval, but does not require a LeafWiki password login.
  - Document that `get_page` and `get_page_by_path` include `linkStatus`, matching the backlinks and broken-link metadata visible in the document view.
  - Keep `get_link_status` documented as the standalone link metadata tool.
  - Keep warning not to expose MCP publicly.

- `readme.md`
  - Update MCP CLI examples to remove `--disable-auth` from the primary authenticated example.
  - Keep loopback-only guidance.
  - Link to `docs/mcp.md`.

- `plans/local_mcp.PLAN.md`
  - Add this follow-up plan summary.
  - Include implementation reference: `codex://threads/019e6e13-1e91-7070-be89-2a45203ea1f6`.

Use these references during implementation:

- MCP authorization spec: https://github.com/modelcontextprotocol/modelcontextprotocol/blob/main/docs/specification/2025-11-25/basic/authorization.mdx
- MCP security tutorial: https://github.com/modelcontextprotocol/modelcontextprotocol/blob/main/docs/docs/tutorials/security/authorization.mdx
- MCP docs tree: https://github.com/modelcontextprotocol/modelcontextprotocol/tree/main/docs/docs
- Go MCP SDK auth docs: `references/go-sdk/docs/protocol.md`
- Go MCP SDK auth middleware: `references/go-sdk/auth/auth.go`
- Go MCP SDK auth examples: `references/go-sdk/examples/server/auth-middleware/main.go`
- `go-oauth2/oauth2`: https://github.com/go-oauth2/oauth2
- `go-oauth2/oauth2/v4/server`: https://pkg.go.dev/github.com/go-oauth2/oauth2/v4/server

## Test Plan

Add tests before or alongside implementation.

Backend OAuth unit tests:

- Authorization server metadata returns expected issuer/endpoints/scopes/grants.
- Protected resource metadata returns expected resource/auth server/scopes.
- Metadata works with no base path and with `/wiki` base path.
- Metadata `OPTIONS` works for protected resource metadata.
- `GET /oauth/authorize` rejects unknown client.
- Reject missing PKCE.
- Reject `code_challenge_method=plain`.
- Reject non-loopback redirect URI.
- Accept loopback redirect URI with `localhost`, `127.0.0.1`, and `::1`.
- Preserve `state` in authorization redirect.
- Accept absent `resource`.
- Reject supplied `resource` that does not equal `<origin><basePath>/mcp`.
- Unauthenticated authorize request redirects to login with `returnTo`.
- Authenticated authorize request renders an approval page instead of silently returning code.
- Approved authorize request returns code and preserves state.
- Trusted remote-user authorize request uses the resolved user and still requires approval.
- Token endpoint exchanges valid code + verifier for access token and refresh token.
- Token endpoint rejects wrong verifier.
- Refresh grant returns a new access token.
- No revocation endpoint is registered.

MCP auth integration tests in the existing MCP suite:

- Auth-enabled `--enable-mcp` registers `/mcp` without `--disable-auth`.
- MCP disabled by default remains unavailable.
- Non-loopback host still blocks MCP startup/registration.
- Missing bearer token returns `401` and `WWW-Authenticate`.
- Invalid bearer token returns `401`.
- Expired bearer token returns `401`.
- Valid OAuth bearer token initializes MCP session and lists tools.
- `get_current_user` returns the authenticated LeafWiki user.
- Editor/admin token can create/update/delete through MCP.
- Viewer token can read but cannot call mutation tools.
- Deleted user invalidates previously issued token.
- Role downgrade from editor to viewer prevents later MCP mutations.
- MCP requests do not require CSRF.
- Normal HTTP API writes still require CSRF.
- Base path `/wiki/mcp` works with auth and metadata URLs are correct.
- Existing HTTP/MCP parity coverage still passes under legacy disabled-auth mode.
- Add at least one authenticated parity smoke: MCP creates a page as real user, HTTP reads it with matching author/resolver behavior.
- `get_page` output schema includes `page` and `linkStatus`.
- `get_page_by_path` output schema includes `page` and `linkStatus`.
- For a page with backlinks, `get_page` returns `linkStatus.backlinks` matching standalone `get_link_status`.
- For the same page, `get_page_by_path` returns the same `linkStatus` as `get_page`.
- For pages with broken outgoing or broken incoming links, `get_page` / `get_page_by_path` expose the same broken-link counts and entries as `get_link_status`.
- Link metadata on page-read tools is covered in both disabled-auth MCP parity tests and authenticated MCP smoke where practical.

Frontend tests:

- Login without `returnTo` still navigates to `/`.
- Login with safe OAuth `returnTo` performs full-page navigation back to `/oauth/authorize`.
- Login ignores unsafe external `returnTo`.
- Already-authenticated login page with OAuth `returnTo` redirects/assigns to authorize route.

Playwright E2E:

- Add a new local mode env var, for example `E2E_ENABLE_MCP_OAUTH_LOCAL=1`.
- In this mode, start LeafWiki with normal auth plus `--enable-mcp`; do not set `--disable-auth`.
- Keep the existing disabled-auth MCP smoke separate.
- New OAuth MCP smoke:
  - visit UI and log in as admin
  - run OAuth authorize flow with PKCE
  - exchange code for token
  - initialize MCP with `Authorization: Bearer <token>`
  - create/update page through MCP
  - verify page appears in UI
  - edit page in UI
  - read updated page through MCP
- Add a refresh-token E2E only if it is reliable without long sleeps; otherwise cover refresh in Go integration tests.

Verification commands:

```bash
go test ./internal/core/auth ./internal/http ./internal/http/middleware/auth ./internal/wiki/mcp ./internal/wiki/...
go test ./...
npm --prefix ui/leafwiki-ui run build
npm --prefix ui/leafwiki-ui run lint
npm --prefix e2e run lint
npm --prefix e2e run format:check
env E2E_RUN_MODE=local E2E_ENABLE_MCP_OAUTH_LOCAL=1 ./e2e/run.sh --grep "mcp.*oauth|oauth.*mcp"
env E2E_RUN_MODE=local E2E_ENABLE_MCP_LOCAL=1 ./e2e/run.sh --grep "mcp.*disable auth|disable auth.*mcp"
```

## Assumptions And Defaults

- Authenticated local MCP is the primary supported mode.
- Legacy disabled-auth MCP remains supported to avoid breaking existing local workflows.
- OAuth tokens are opaque `go-oauth2` tokens for MVP, not LeafWiki web JWTs.
- OAuth token storage is in-memory for MVP; restart requires reauthorization.
- No OAuth client management UI.
- No API key backend or frontend.
- No token revocation endpoint; web logout does not revoke MCP OAuth tokens in this MVP.
- Explicit local approval screen before issuing authorization codes.
- No dynamic registration.
- No public-network support.
- Single scope `leafwiki:mcp`; LeafWiki user role decides actual read/write rights.
