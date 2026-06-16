<!-- leafwiki
version: 1
page:
  id: JzlMvvaDR
  title: Fosite OAuth Migration
  created_at: "2026-06-15T20:52:04.785871Z"
  updated_at: "2026-06-15T21:17:58.102871Z"
  creator_id: public-editor
  last_author_id: public-editor
tags:
  - discovery
  - auth
  - oauth
  - fosite
  - mcp
  - workspace-federation
fields:
  enables: federated-workspaces
  status: working-notes
  topic: oauth-migration
-->

# Fosite OAuth Migration

Working notes for splitting the OAuth-library decision out of the larger [Federated Workspaces and Session Daemon](/discovery/federated-workspaces-session-daemon.md) discovery.

This page asked whether LeafWiki should migrate the pre-migration local MCP OAuth implementation from `go-oauth2` to ORY Fosite before attempting the broader split into `sessiond`, `frontd`, and per-workspace `workspaced` processes.

## Discovery Answer

If the federated workspace and `sessiond` direction remains the next major architecture track, LeafWiki should migrate the OAuth foundation to Fosite before splitting the daemons.

The reason was not that the pre-migration `go-oauth2` implementation was broken. It was working for local MCP. The reason is that the future architecture needs auth concepts that should not be designed while also changing process ownership: first-class clients, token sessions, subject identity, resource or audience placeholders, revocation state, and an internal resource-server verification path.

The first cut should be boring from the outside:

- Preserve current local MCP OAuth behavior.
- Keep the single-daemon shape.
- Replace the auth-server internals with a Fosite-backed module.
- Add explicit storage and verifier seams that can later move to `sessiond`.
- Avoid adding new user-visible OAuth features in the same cut.

With this compatibility contract accepted, this is ready to become an implementation plan.

## Why This Is Separate

Federated workspaces will make authentication and authorization a central architectural primitive. If we change daemon boundaries, frontend ownership, workspace registry behavior, and OAuth semantics at the same time, the work becomes difficult to review and hard to merge safely.

A Fosite migration can be a cleaner enabling cut:

- Keep the current single-daemon shape.
- Preserve the existing MCP OAuth behavior.
- Replace the auth-server foundation with one that better fits future workspace federation.
- Prove the token, client, grant, and validation boundaries before moving identity into `sessiond`.

This is not an implementation plan. It should graduate to [Plans](/plans) as a compatibility-preserving auth refactor, not as the daemon split itself.

## Pre-Migration Baseline

This section records the live baseline observed before the Fosite migration implementation. After the migration, the route contract remains the compatibility target, but the OAuth engine is Fosite-backed rather than `go-oauth2`-backed.

The current OAuth implementation was originally captured in [Authenticated Local MCP via Minimal OAuth](/plans/oauth-plan.md), but the live code has moved beyond that first plan.

Pre-migration behavior:

- Before the migration, `go-oauth2/oauth2/v4` backed the OAuth server.
- Authorization Code + PKCE is the only interactive OAuth flow.
- PKCE is required and only `S256` is allowed.
- The only OAuth scope is `leafwiki:mcp`.
- MCP clients are public local clients with no client secret.
- LeafWiki publishes protected-resource metadata and authorization-server metadata.
- LeafWiki has a custom Dynamic Client Registration endpoint at `/oauth/register`.
- The fixed public client `leafwiki-local-mcp` remains available for manual testing and backward compatibility.
- OAuth token storage is in-memory. Current dynamic client registrations and approval tokens are also in-memory.
- No `/oauth/revoke` endpoint exists or is advertised.
- MCP API keys are a separate persistent bearer-token path and should not be folded into OAuth.
- LeafWiki loads the current user on every MCP request, so deleted users and role changes affect existing OAuth tokens immediately.
- LeafWiki user role still controls read/write permission after OAuth authentication.

Current coverage already proves more than the first plan required: metadata shape, Dynamic Client Registration, approval flow, token exchange, refresh, deleted-user refresh rejection, expired bearer rejection, deleted-user bearer rejection, role downgrade behavior, base-path metadata, API-key coexistence, and MCP/UI round trips.

## What The Local Fosite Source Shows

Use the local snapshot under `references/fosite` as the implementation reference.

Important source findings:

- `references/fosite/storage.go` defines Fosite's minimal `Storage` as `ClientManager`, but the composed handlers assert richer storage interfaces depending on which factories are used.
- `references/fosite/handler/oauth2/storage.go` shows the Authorization Code + Refresh Token path needs authorize-code, access-token, and refresh-token session storage.
- `references/fosite/handler/oauth2/revocation_storage.go` shows refresh rotation and authorization-code reuse protection require revocation-capable storage even if LeafWiki does not expose a public revoke endpoint.
- `references/fosite/handler/pkce/storage.go` shows PKCE has its own request-session storage tied to the authorization code signature.
- `references/fosite/client.go` has a richer default client model than `go-oauth2`: redirect URIs, grant types, response types, scopes, audience, and public/confidential state.
- `references/fosite/compose/compose.go` shows `ComposeAllEnabled` registers far more than LeafWiki wants: implicit, client credentials, password, device, OIDC, PAR, introspection, revocation, and PKCE handlers.
- `references/fosite/compose/compose_oauth2.go` lets LeafWiki compose only the handlers it needs.
- `references/fosite/compose/compose_strategy.go` supports HMAC opaque tokens and JWT access tokens as separate choices.
- `references/fosite/README.md` explicitly calls out opaque access and refresh tokens, HMAC token signatures, random-looking state requirements, refresh-token rotation, and revocation behavior.

The first LeafWiki migration should not call `ComposeAllEnabled`. It should compose the narrow surface:

- HMAC opaque token strategy.
- Authorization Code handler.
- Refresh Token handler.
- PKCE handler.
- Token introspection/validation handler for LeafWiki's internal bearer verifier.

Do not register public routes just because Fosite has handlers. LeafWiki owns the HTTP route surface.

## Fosite Compatibility Knobs

Fosite defaults are close, but not identical to the current LeafWiki contract.

Important settings and adapters:

- Set access-token, refresh-token, and authorization-code lifetimes from LeafWiki's existing config.
- Configure a Fosite global HMAC secret and rotation path. This should not reuse the web JWT secret without an explicit decision.
- Require PKCE for this flow and keep `EnablePKCEPlainChallengeMethod=false` so `plain` stays rejected.
- Set `RefreshTokenScopes` to an empty slice. Fosite otherwise only issues refresh tokens for `offline` or `offline_access`, but LeafWiki's only scope is `leafwiki:mcp`.
- Use exact scope granting for `leafwiki:mcp`; do not let Fosite's default wildcard semantics broaden current behavior.
- Preserve the current `resource` parameter rule outside or alongside Fosite: absent is allowed, present must equal the MCP resource URL exactly and must appear at most once.
- Preserve current redirect-URI rules. DCR clients should use Fosite's registered redirect URI model normally. The fixed `leafwiki-local-mcp` fallback must preserve today's explicit loopback redirect behavior with an adapter if Fosite's registered-redirect model cannot express it directly.
- Accept Fosite's stricter state validation. Current real clients should send realistic non-trivial state values, and tests, examples, and manual flows should be updated to use those values rather than adding a compatibility path for short or missing state.
- Normalize token and error responses where Fosite's defaults differ from current tests or MCP client expectations. In particular, do not let a library swap silently change status codes, redirect-vs-JSON behavior, `WWW-Authenticate`, or token response casing.

These decisions keep the migration externally stable where clients may depend on current behavior, while accepting Fosite's stricter state validation as the new internal OAuth baseline.

## Compatibility Contract To Preserve

External OAuth and MCP behavior must remain stable. This refactor switches libraries and adds internal support needed for Fosite; it should not add, remove, or expose user-facing OAuth capabilities.

Preserve these public routes and metadata behaviors:

- `GET /.well-known/oauth-protected-resource`
- `GET /.well-known/oauth-protected-resource/mcp`
- `GET /.well-known/oauth-protected-resource/<base-path>/mcp`
- `GET /.well-known/oauth-authorization-server`
- `GET /.well-known/oauth-authorization-server/<base-path>`
- `GET <base-path>/oauth/authorize`
- `POST <base-path>/oauth/authorize`
- `GET <base-path>/oauth/approval`
- `POST <base-path>/oauth/register`
- `POST <base-path>/oauth/token`

Preserve these protocol choices:

- `response_types_supported`: `code`
- `grant_types_supported`: `authorization_code`, `refresh_token`
- `code_challenge_methods_supported`: `S256`
- `scopes_supported`: `leafwiki:mcp`
- `token_endpoint_auth_methods_supported`: `none`
- `registration_endpoint` remains advertised.
- `revocation_endpoint` remains absent.
- No public introspection endpoint is advertised.

Preserve these flow behaviors:

- OAuth-capable MCP clients can use discovery and Dynamic Client Registration without configuration changes.
- The official MCP TypeScript client DCR + OAuth E2E remains the primary compatibility gate.
- The fixed `leafwiki-local-mcp` client remains available for manual/testing clients and preserves current explicit loopback redirect compatibility.
- Authorization requires an authenticated web user and explicit local approval.
- Trusted remote-user auth can identify the user, but approval is still required.
- Login `returnTo` continues to send users back to `/oauth/authorize` after login.
- Missing or invalid MCP bearer tokens continue to return `401` with protected-resource metadata and `leafwiki:mcp` scope in `WWW-Authenticate`.
- MCP API keys remain a separate bearer verifier path.
- Web CSRF behavior does not change.

## Answered Compatibility Questions

### Can current MCP clients complete the Fosite-backed OAuth flow without configuration changes?

Yes, if LeafWiki preserves its own metadata, DCR endpoint, authorization/token route paths, approval flow, and bearer challenge shape.

Fosite should sit behind the existing route contract. OAuth-capable MCP clients should still discover protected-resource metadata, register a public client, open the authorization URL, receive a code, exchange it with PKCE, and connect to `/mcp` with a bearer token.

The risky edge is the fixed `leafwiki-local-mcp` fallback, not DCR. DCR clients already register redirect URIs, which maps well to Fosite. The fixed fallback currently accepts any explicit loopback redirect URI. Fosite's built-in redirect matcher expects registered redirect URIs, with dynamic-port support mainly for registered IP loopback URIs with the same path.

Accepted redirect decision: preserve the fixed fallback's current explicit loopback redirect behavior with an adapter if needed. Changing that fallback would be an externally observable behavior change, and this refactor is limited to switching libraries without adding or removing user-facing OAuth capabilities.

### Does Fosite require metadata or error-response behavior that differs from current clients' expectations?

Fosite does not own LeafWiki's OAuth metadata. LeafWiki should keep generating metadata itself.

Fosite can change observable error behavior if LeafWiki forwards raw Fosite responses. The migration should preserve current response contracts:

- Pre-client validation failures that cannot safely redirect stay `400`.
- Client-recoverable authorization errors redirect to the client's redirect URI with `error` and original `state`.
- Token errors remain JSON with current status-code expectations.
- DCR errors remain `invalid_client_metadata` with LeafWiki's current descriptions.
- Debug details from Fosite should not leak to clients.
- Token response shape remains compatible with current tests and MCP clients.

Fosite also enforces a minimum state length of 8 by default. Accepted state decision: keep that stricter validation and update tests, examples, and manual flows to use realistic state values. Do not add a short-state compatibility adapter unless implementation evidence shows a real supported MCP client requires it.

### How should Dynamic Client Registration be represented?

Keep LeafWiki's custom `/oauth/register` endpoint. Fosite does not provide DCR for LeafWiki.

DCR should create a LeafWiki-owned OAuth client record and expose it to Fosite through the client store. Minimal fields:

- client ID
- client name
- redirect URIs
- grant types
- response types
- scopes
- audience/resource placeholder
- public-client flag
- created timestamp
- optional revocation/disabled state for the future

Supported DCR behavior remains narrow:

- public clients only
- `token_endpoint_auth_method=none`
- no `client_secret`
- loopback redirect URIs only
- `authorization_code` required
- `refresh_token` optional but defaulted today
- `code` response type only
- empty scope or `leafwiki:mcp` only

### Should token storage remain in-memory for the first Fosite migration?

Yes. Keep OAuth token storage and dynamic OAuth client storage in-memory for the first migration, matching current operational behavior.

However, do not hide storage behind Fosite's example store forever. The migration should introduce a LeafWiki-owned store interface and internal records shaped for later persistence. The first implementation can back that store with memory while making the data model explicit.

The minimal store needs:

- clients
- authorization-code sessions and invalidation state
- PKCE request sessions
- access-token sessions
- refresh-token sessions and rotation state
- request ID to token signatures for revocation-by-grant
- subject/user ID on the Fosite session
- granted scope and optional audience/resource placeholder
- expiry timestamps
- revoked/inactive state

Durable OAuth clients, durable OAuth token sessions, cross-restart grants, and central `sessiond` storage are follow-up work. Adding persistence during the library migration would expand the blast radius without being required for client compatibility.

### Should revocation and introspection be added during migration?

Do not add public revocation or public introspection endpoints in the first migration.

Internally, the store should still support revocation state because Fosite's refresh-token rotation and authorization-code reuse protection call `TokenRevocationStorage`. That is an internal correctness requirement, not a reason to publish `/oauth/revoke`.

For MCP bearer verification, v1 `Service.VerifyBearerToken` validates an opaque access token and returns the v1 MCP `TokenInfo` contract: user ID, scopes, and expiration. Client ID and audience/resource output remain a future seam for `workspaced` or a `sessiond` verifier after that boundary exists.

### Should JWT access tokens be introduced immediately?

No. Use Fosite's HMAC opaque tokens for the first migration.

Opaque tokens preserve today's server-side verification model: LeafWiki can load the current user on every request, reject deleted users, apply role changes immediately, and support revocation state. JWTs introduce key management, issuer/audience decisions, token claim contracts, and revocation tradeoffs before `workspaced` exists.

The local Fosite source explicitly warns that stateless JWT introspection does not use storage and built-in revocation will not work. JWT access tokens should wait until the federation design has a concrete need for offline verification by workspace daemons.

### How do we preserve current user-role enforcement while preparing for central grants?

Keep tokens as authentication artifacts, not authorization snapshots.

The Fosite session should store the LeafWiki subject/user ID. The MCP bearer verifier should load the current LeafWiki user on every request and build the same MCP actor model used today. Tool authorization should continue to use the current role: viewer can read; editor/admin can mutate.

For the future, introduce a narrow authorization seam behind the verifier:

```text
verified token -> subject -> current user/grants -> MCP actor -> tool permission checks
```

Today that seam resolves to the current workspace's local user role. Later it can resolve `sessiond` workspace grants without changing every MCP handler.

## First-Cut Fosite Boundary

In scope for the first implementation plan:

- Add Fosite as the OAuth engine behind existing routes.
- Compose only the handlers LeafWiki needs.
- Preserve current route, metadata, DCR, approval, token, and MCP bearer behavior.
- Keep OAuth storage in-memory while introducing LeafWiki-owned storage interfaces.
- Keep OAuth clients public and local.
- Keep only `leafwiki:mcp` scope.
- Keep API keys separate.
- Add internal token verification/introspection seam for MCP.
- Add internal revocation state required by Fosite refresh/code-reuse behavior.
- Add audience/resource placeholders in records, but do not enforce multi-workspace grants yet.

Out of scope for the first implementation plan:

- `sessiond` process lifecycle.
- `frontd` and multi-workspace UI routing.
- Workspace registry and discovery.
- Cross-workspace search or activity feeds.
- Mandatory sync migration.
- Durable OAuth token/client storage.
- Public revocation endpoint.
- Public introspection endpoint.
- JWT access tokens.
- Multi-workspace grants in production behavior.
- Agent-specific workspace installation flows.

## Test Mapping

Existing tests should remain the migration backbone.

Current tests that should map one-to-one:

- OAuth metadata and protected-resource metadata.
- Dynamic Client Registration validation and successful DCR flow.
- DCR default refresh behavior and auth-code-only behavior.
- Authorization request validation, login redirect, approval, denial, and state preservation.
- Remote-user authorization with approval.
- Token exchange, wrong verifier, refresh, and deleted-user refresh rejection.
- Token lifetimes from LeafWiki options.
- Expired bearer rejection.
- MCP bearer challenge shape.
- `get_current_user` identity through MCP.
- Viewer denial and editor/admin mutation behavior.
- Role downgrade and deleted-user bearer invalidation.
- API-key bearer coexistence.
- Base-path metadata and base-path OAuth sessions.
- Playwright OAuth MCP flow and DCR SDK flow.

New Fosite-specific tests needed before implementation can be called done:

- Fosite config issues refresh tokens for `leafwiki:mcp` without `offline` by setting `RefreshTokenScopes` to empty.
- Fosite rejects `plain` PKCE and accepts `S256` exactly as today.
- Fixed `leafwiki-local-mcp` fallback preserves current explicit loopback redirect behavior through the Fosite migration.
- DCR clients map to Fosite clients with exact redirect URI, grant, response type, scope, and public-client behavior.
- Fosite token response and error response adapters preserve current external status/body expectations.
- Fosite state entropy behavior is covered with realistic client state values, and short or missing state cases are rejected consistently.
- Authorization-code reuse invalidates derived tokens through internal revocation state.
- Refresh-token rotation and refresh-token reuse behavior are explicit and tested.
- Internal MCP verifier returns the v1 MCP `TokenInfo` contract: user ID, scopes, and expiration; client and resource/audience output remain a future seam without requiring a public introspection endpoint.
- Deleted-user and role-change checks still happen at verification/tool time, not at token issue time.

## Risks

Main risks:

- Fosite has a larger integration surface than `go-oauth2`.
- Calling `ComposeAllEnabled` would accidentally enable unsupported grant types and protocol endpoints.
- Fosite defaults for refresh-token scopes, state entropy, redirect matching, token response casing, or error formatting can break clients even if the high-level flow looks equivalent.
- Implementing persistence, public revocation, public introspection, JWTs, and audience enforcement in the same cut would make the migration too broad.
- If this is framed as a rewrite instead of a compatibility-preserving seam, it will be hard to review.

Mitigations:

- Compose the minimal Fosite handler set.
- Keep LeafWiki-owned metadata, DCR, approval, and response adapters.
- Keep storage in-memory for v1 but make the record model explicit.
- Run current OAuth/MCP unit, integration, and Playwright tests before adding new behavior.
- Add Fosite-specific tests for default mismatches before replacing `go-oauth2`.

## Graduation Criteria

This discovery can graduate to a plan when the implementation plan carries forward these accepted decisions:

- Migrate to Fosite before the daemon split if federation remains the next architecture track.
- Preserve the current OAuth/MCP external contract.
- Do not add new user-facing OAuth routes, grant types, token formats, storage durability, revocation, introspection, or workspace-grant capabilities in the first cut.
- Preserve fixed `leafwiki-local-mcp` loopback redirect compatibility with an adapter if Fosite's registered redirect model cannot express current behavior directly.
- Accept Fosite's stricter state validation and update tests, examples, and manual flows to use realistic state values.
- Use minimal Fosite composition, not `ComposeAllEnabled`.
- Use HMAC opaque tokens, not JWT access tokens.
- Keep public revocation and public introspection out of the first cut.
- Keep OAuth token/client storage in-memory for first cut, while adding LeafWiki-owned storage interfaces and records.
- Keep API keys separate from OAuth.
- Preserve role enforcement by loading current user/grants at request time.
- Add the Fosite-specific tests listed above across unit, integration, and E2E coverage as appropriate.

## Relationship To Federated Workspaces

This migration is an enabling step, not the federated workspace feature itself.

The larger discovery still owns the model of `sessiond`, `frontd`, `workspaced`, workspace registry, central grants, and mandatory Git-backed sync. This page owns the narrower question of whether the current OAuth foundation should be replaced before that split.

A likely sequence is:

```text
signals/
  multiple workspace frontends and auth duplication feel messy

discovery/fosite-oauth-migration
  migrate the OAuth foundation behind the current MCP contract

discovery/federated-workspaces-session-daemon
  continue shaping the multi-daemon model using the stronger auth seam

plans/
  create a concrete Fosite migration plan before implementing the daemon split
```
