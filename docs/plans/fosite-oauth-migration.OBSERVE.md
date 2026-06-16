<!-- leafwiki
version: 1
page:
  id: G-adKDaDR
  title: Fosite OAuth Migration - Observe
  created_at: "2026-06-15T21:30:11.277799888Z"
  updated_at: "2026-06-15T21:30:11.277799888Z"
  creator_id: system
  last_author_id: system
-->

# Fosite OAuth Migration - Observe

## Purpose

This document captures observed facts for the Fosite OAuth migration plan. It follows the Observe step from `docs/plans/planning.aibasic.txt`.

## Planning Input

The source discovery is `docs/discovery/fosite-oauth-migration.md`. It answered that LeafWiki should migrate the pre-migration local MCP OAuth implementation from `go-oauth2` to ORY Fosite before the future daemon split, but only as a compatibility-preserving internal refactor.

Accepted user constraints:

- Switch OAuth libraries.
- Do not add new user-facing features or capabilities.
- Add internal functionality needed to make Fosite work.
- Update unit, integration, and E2E tests.
- Preserve fixed `leafwiki-local-mcp` loopback redirect behavior with an adapter if needed.
- Accept Fosite's stricter state validation and update tests/examples to use realistic state values.

## Wiki Context

`wiki_get_context` reported:

- Workspace sync is enabled and watcher is running.
- The current user is `public-editor` with editor role.
- Validation is clean.
- One active web session exists, but it is not dirty.
- The discovery page was recently updated from filesystem and MCP writes.

## Subagent Observations

Four read-only explorer passes were run as requested by the AIBASIC workflow.

### Explorer1 - Technology Stack And Versions

These observations were captured before the implementation pass. They describe the migration starting point, not the post-migration dependency graph.

- `go.mod` uses module `github.com/perber/wiki` and `go 1.25.4`.
- Pre-migration direct OAuth/auth dependencies included `github.com/go-oauth2/oauth2/v4 v4.5.4`, `github.com/golang-jwt/jwt/v5 v5.3.1`, `github.com/modelcontextprotocol/go-sdk v1.6.1-0.20260527093908-2d47cc966460`, and `golang.org/x/oauth2 v0.35.0`.
- At planning time, the root module did not yet depend on `github.com/ory/fosite`; the implementation pass adds it.
- `references/fosite/go.mod` identifies the local reference snapshot as module `github.com/ory/fosite`.
- E2E uses Playwright from `e2e/package.json`; OAuth MCP E2E is gated by `E2E_RUN_MODE=local` and `E2E_ENABLE_MCP_OAUTH_LOCAL=1`.
- UI build and lint are under `ui/leafwiki-ui/package.json`; E2E lint and format checks are under `e2e/package.json`.

### Explorer2 - Architecture Patterns And Conventions

- `internal/wiki/oauth/service.go` is the OAuth engine boundary. Fosite should sit behind this service boundary.
- `internal/wiki/oauth/authorize.go` owns authorization route policy: login redirect, explicit approval, loopback redirect validation, resource validation, state-preserving error redirects, and current-user binding at approval.
- `internal/wiki/oauth/registration.go` owns DCR and should remain LeafWiki-owned.
- `internal/wiki/oauth/metadata.go` owns OAuth and protected-resource metadata; Fosite should not own external metadata.
- `internal/wiki/oauth/token.go` is a route adapter; token errors and refresh validation need to preserve current response shape.
- `internal/wiki/mcp/routes.go` is the MCP bearer boundary. API keys are verified first, then OAuth bearers delegate to the OAuth service.
- `internal/wiki/mcp/helpers.go` reloads the current user at tool time, preserving deleted-user and role-change behavior.
- `internal/core/auth/api_key_service.go` and `internal/core/auth/api_key_store.go` are a separate persistent MCP API-key path and must not be folded into OAuth.

### Explorer3 - Implementation Files And Tests

Current OAuth implementation files:

- `internal/wiki/oauth/service.go`
- `internal/wiki/oauth/authorize.go`
- `internal/wiki/oauth/registration.go`
- `internal/wiki/oauth/approval.go`
- `internal/wiki/oauth/responses.go`
- `internal/wiki/oauth/token.go`
- `internal/wiki/oauth/metadata.go`
- `internal/wiki/oauth/routes.go`
- `internal/wiki/wiki.go`

MCP and auth boundary files:

- `internal/wiki/mcp/routes.go`
- `internal/wiki/mcp/helpers.go`
- `internal/core/auth/api_key_service.go`
- `internal/core/auth/api_key_store.go`
- `internal/http/middleware/auth/auth.go`
- `internal/http/middleware/auth/reverse_proxy.go`

Primary tests:

- `internal/wiki/mcp/mcp_oauth_integration_test.go`
- `internal/wiki/mcp/mcp_integration_test.go`
- `internal/wiki/mcp/helpers_test.go`
- `internal/core/auth/api_key_service_test.go`
- `internal/http/middleware/auth/*_test.go`
- `internal/http/router_test.go`
- `e2e/tests/mcp-oauth.spec.ts`
- `e2e/tests/mcpClient.ts`
- `e2e/tests/auth.spec.ts`

### Explorer4 - Project Learnings And Constraints

- Graduate the discovery into `docs/plans/fosite-oauth-migration.{OBSERVE,CONTEXT,DECISION,PLAN}.md`, not into the old OAuth plan.
- Preserve the suffix-style artifact convention.
- The original OAuth plan in `docs/plans/oauth.PLAN.md` is useful historical context but is now stale because live code has DCR, API-key coexistence, base-path coverage, and SDK E2E.
- `docs/plans/api_keys.PLAN.md` makes API keys an additive MCP-only bearer path; keep it separate.
- `docs/discovery/federated-workspaces-session-daemon.md` frames `sessiond`, `frontd`, and `workspaced` as future work. This plan should add seams, not implement daemon federation.
- Useful history anchors include commits that introduced MCP bearer auth, local MCP OAuth/DCR, MCP API keys, and native STDIO API-key auth.

## Pre-Migration Local Code Observations

These observations record the `go-oauth2` implementation that existed before this migration and should not be read as the post-implementation architecture.

Pre-migration OAuth service behavior:

- Before the migration, `internal/wiki/oauth/service.go` used `go-oauth2` manager/server, `oauthstore.NewMemoryTokenStore`, and a `ClientStore`.
- The fixed public client is `leafwiki-local-mcp`.
- Scope is `leafwiki:mcp`.
- Allowed grants are `authorization_code` and `refresh_token`.
- Allowed response type is `code`.
- PKCE is required and only `S256` is allowed.
- Token lifetimes come from `WikiOptions`.
- `VerifyBearerToken` validates an opaque bearer token and reloads the LeafWiki user by ID.

Current route behavior:

- OAuth routes are registered only when MCP is enabled, auth is enabled, and MCP bind host is loopback.
- Well-known metadata routes are registered on the engine; OAuth routes are registered under `ctx.Base`.
- `/oauth/register` is custom DCR.
- `/oauth/token` delegates to the current OAuth library after refresh prevalidation.
- `/oauth/revoke` is not registered.

Current test behavior:

- Integration tests already cover metadata, DCR, approval, token exchange, refresh, deleted-user refresh rejection, expired bearer rejection, MCP bearer challenges, API-key coexistence, base-path sessions, role downgrade, and deleted-user token rejection.
- Existing OAuth test state values are already mostly realistic strings. The migration still needs explicit short/missing state rejection coverage because Fosite enforces minimum state length.

## Local Fosite Source Observations

Use `references/fosite` as the source reference.

- `references/fosite/storage.go` defines minimal `Storage` as `ClientManager`, but selected handlers assert richer interfaces.
- `references/fosite/handler/oauth2/storage.go` requires authorize-code, access-token, and refresh-token storage for the authorization-code/refresh path.
- `references/fosite/handler/oauth2/revocation_storage.go` requires revocation-capable storage for refresh rotation and code-reuse protection.
- `references/fosite/handler/pkce/storage.go` requires PKCE request-session storage.
- `references/fosite/client.go` defines Fosite clients with redirect URIs, grants, response types, scopes, audience, and public/confidential state.
- `references/fosite/compose/compose_oauth2.go` provides factories for authorization code, refresh token, token introspection, and revocation handlers.
- `references/fosite/compose/compose_strategy.go` provides HMAC opaque-token strategy support.
- `references/fosite/fosite.go` sets default `MinParameterEntropy` to `8`.
- `references/fosite/config_default.go` documents `RefreshTokenScopes`; an empty slice issues refresh tokens for all eligible exchanges.

## Current Verification Commands

Known relevant commands:

```bash
rtk go test ./internal/wiki/oauth ./internal/wiki/mcp ./internal/wiki ./internal/http ./internal/http/middleware/auth
rtk go test ./...
rtk npm --prefix ui/leafwiki-ui run build
rtk npm --prefix ui/leafwiki-ui run lint
rtk npm --prefix e2e run lint
rtk npm --prefix e2e run format:check
rtk env E2E_RUN_MODE=local E2E_ENABLE_MCP_OAUTH_LOCAL=1 ./e2e/run.sh tests/mcp-oauth.spec.ts
```
