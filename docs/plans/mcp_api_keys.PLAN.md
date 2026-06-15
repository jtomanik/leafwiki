<!-- leafwiki
version: 1
page:
  id: O12-XIaDg
  title: LeafWiki MCP API Keys Management Plan
  created_at: "2026-06-15T05:44:44.855972418Z"
  updated_at: "2026-06-15T05:44:44.855972418Z"
  creator_id: system
  last_author_id: system
-->

# LeafWiki MCP API Keys Management Plan

> **Superseded duplicate:** This visible copy is retained for migration/history only. Use [`api_keys.PLAN.md`](/plans/api-keys-plan.md) as the canonical plan page.

## Summary

Add MCP-only API keys as an additive Bearer authentication path beside the existing OAuth/DCR MCP flow.

Implementation reference: `codex://threads/019e73de-cdd8-7182-a018-d3b0f77add32`

This file, `plans/api_keys.PLAN.md`, is the canonical implementation plan. Include this thread link, this plan, the Gherkin suite, and the Definition of Done in this file.

Key decisions:

- API keys are accepted only for MCP auth: `Authorization: Bearer lwk_<id>_<secret>`.
- OAuth remains the primary interoperable MCP login path; `codex mcp login`, OAuth metadata, DCR, refresh, and existing OAuth E2E behavior must not regress.
- API keys do not expire in MVP.
- API keys use scope `leafwiki:mcp`.
- Keys are user-bound and inherit the owner’s current role on every request.
- Admins can create/list/revoke keys for any user from user management.
- Users can create/list/revoke their own keys. Password-auth self-create requires `currentPassword`; HTTP remote-user self-create is disabled in MVP.
- Raw secrets are returned once on create and never stored or returned again.
- E2E must prove the official MCP TypeScript client works with API-key Bearer auth.

## Key Changes

Backend:

- Add `APIKeyStore` and `APIKeyService` under core auth with self-initializing SQLite storage.
- Store metadata plus a hash of the full raw key. Suggested fields: `id`, `user_id`, `name`, `secret_hash`, `prefix`, `last4`, `scopes`, `created_by_user_id`, `created_at`, `last_used_at`, `revoked_at`.
- Use key format `lwk_<id>_<secret>`; lookup by `id`, constant-time compare the hash.
- Add routes:
  - `GET /api/users/:id/mcp-api-keys`
  - `POST /api/users/:id/mcp-api-keys`
  - `DELETE /api/users/:id/mcp-api-keys/:keyId`
  - `GET /api/users/me/mcp-api-keys`
  - `POST /api/users/me/mcp-api-keys`
  - `DELETE /api/users/me/mcp-api-keys/:keyId`
- `POST` returns `{ "key": <metadata>, "secret": "lwk_..." }`; list endpoints never return `secret`.
- Validate key names as required, trimmed, and bounded; use existing field-validation response shape.
- Compose MCP bearer verification: if bearer starts with `lwk_`, verify API key; otherwise verify OAuth.
- Return SDK `TokenInfo{UserID, Scopes:["leafwiki:mcp"], Expiration:<non-zero far future>}` for API keys because MVP keys do not expire.
- Load current user on every key-backed MCP request; deleted users, revoked keys, and role changes take effect immediately.
- Do not use API keys for normal HTTP API auth, cookies, CSRF, OAuth token exchange, OAuth metadata, or DCR.

Frontend:

- Extend existing user management UI with an “MCP API Keys” action per user row.
- Add a dialog/panel to list active key metadata, create a key, show copy-once secret, and revoke keys.
- Add a self-service “MCP API Keys” entry near account/user toolbar actions.
- For password auth, self-create requires current password. For HTTP remote-user auth, hide/disable self-create in MVP while allowing list/revoke.
- Reuse `fetchWithAuth`, existing dialog patterns, validation mapping, and toast behavior.

Docs:

- Update `docs/mcp.md` to document API keys as an advanced/manual MCP auth option; OAuth remains recommended for OAuth-capable clients.
- Update `README.md` Local MCP section with a short API-key mention and link.
- This plan supersedes earlier OAuth/MCP planning notes that rejected API keys for this MCP-only bearer use case.
- Document Bearer format, MCP-only scope, no expiry in MVP, copy-once secrets, revocation, role inheritance, deleted-user invalidation, current-password self-create, and remote-user self-create limitation.

## Gherkin Test Suite

```gherkin
Feature: API key storage
  Scenario: Creating an API key stores only a hash
    Given a user exists
    When an API key is created for that user
    Then the raw secret is returned once
    And the database does not contain the raw secret
    And stored metadata includes id, userId, name, prefix, last4, scopes, createdBy, createdAt
    And lastUsedAt and revokedAt are null

  Scenario: Listing API keys returns metadata only
    Given a user has an active API key
    When API keys are listed
    Then metadata is returned
    And secret and secretHash are not returned

  Scenario: Revoking an API key is soft delete
    Given a user has an active API key
    When the key is revoked
    Then revokedAt is set
    And the key is absent from active key lists
    And the key no longer verifies

  Scenario: Malformed key does not verify
    When a malformed bearer token is verified
    Then verification fails as invalid token

  Scenario: Wrong secret for valid key id does not verify
    Given a user has an API key
    When a bearer key uses the same id with a different secret
    Then verification fails as invalid token

Feature: Admin API key management routes
  Scenario: Admin creates a key for another user
    Given auth is enabled
    And I am logged in as admin
    And an editor user exists
    When I POST /api/users/:id/mcp-api-keys with a valid name
    Then the response is 201
    And metadata is returned
    And the raw secret is returned once
    And a later GET never includes the raw secret

  Scenario: Admin lists a user's keys
    Given I am logged in as admin
    And a user has active and revoked keys
    When I GET /api/users/:id/mcp-api-keys
    Then the response is 200
    And only active keys are returned

  Scenario: Admin revokes a user's key
    Given I am logged in as admin
    And a user has an active key
    When I DELETE /api/users/:id/mcp-api-keys/:keyId
    Then the response is 204
    And the key no longer verifies

  Scenario: Admin cannot create a key for a missing user
    Given I am logged in as admin
    When I POST /api/users/missing/mcp-api-keys
    Then the response is 404

  Scenario: Invalid key name returns validation
    Given I am logged in as admin
    When I create a key with an empty or too-long name
    Then the response is 400
    And the body uses validation_error fields

  Scenario: Non-admin cannot administer another user's keys
    Given I am logged in as editor or viewer
    When I create, list, or revoke another user's keys
    Then the response is 403

Feature: Self-service API key management
  Scenario: User creates own key with current password
    Given password auth is enabled
    And I am logged in as editor
    When I POST /api/users/me/mcp-api-keys with a valid name and currentPassword
    Then the response is 201
    And the key belongs to my user id
    And the raw secret is returned once

  Scenario: User cannot create own key with wrong current password
    Given I am logged in as editor
    When I POST /api/users/me/mcp-api-keys with the wrong currentPassword
    Then the response is 400
    And no key is created

  Scenario: User lists only own keys
    Given I am logged in as editor
    And another user has a key
    When I GET /api/users/me/mcp-api-keys
    Then only my active keys are returned

  Scenario: User revokes own key
    Given I have an active key
    When I DELETE /api/users/me/mcp-api-keys/:keyId
    Then the response is 204
    And the key no longer verifies

  Scenario: User cannot revoke another user's key through self endpoint
    Given another user has an active key
    When I DELETE /api/users/me/mcp-api-keys/:otherKeyId
    Then the response is 404 or 403
    And the other key remains active

  Scenario: Remote-user self-create is disabled in MVP
    Given HTTP remote-user auth is enabled
    And I am authenticated through a trusted proxy
    When I POST /api/users/me/mcp-api-keys
    Then the response is 403

Feature: MCP API key Bearer auth
  Scenario: Valid editor API key opens MCP session
    Given MCP is enabled on loopback
    And auth is enabled
    And an editor user has an API key
    When an MCP client connects to /mcp with Authorization: Bearer <api key>
    Then the session initializes successfully
    And get_current_user returns the editor user
    And mutation tools create and update pages as that user

  Scenario: Valid viewer API key can read but cannot mutate
    Given a viewer user has an API key
    When an MCP client connects with that key
    Then get_tree succeeds
    And get_page succeeds
    When the client calls create_page or update_page
    Then the tool returns an editor/admin permission error

  Scenario: Revoked API key is rejected before tools
    Given a user has a revoked API key
    When /mcp receives Authorization: Bearer <revoked key>
    Then /mcp returns 401

  Scenario: Deleted user invalidates API key
    Given a user has an API key
    And the user is deleted
    When /mcp receives that key
    Then /mcp returns 401

  Scenario: Role downgrade affects existing API key
    Given an editor has an API key
    And the editor is downgraded to viewer
    When the key is used for MCP
    Then read tools work
    And mutation tools fail with editor/admin permission error

  Scenario: Invalid API key does not fall back to public editor
    Given auth is enabled
    When /mcp receives an invalid lwk_ bearer token
    Then /mcp returns 401
    And get_current_user cannot return public-editor

  Scenario: Missing bearer challenge remains OAuth-compatible
    Given auth is enabled and MCP is enabled
    When /mcp receives no Authorization header
    Then /mcp returns 401
    And WWW-Authenticate advertises protected-resource metadata and scope leafwiki:mcp

  Scenario: OAuth bearer still works
    Given a valid OAuth MCP access token exists
    When an MCP client connects with the OAuth bearer token
    Then the existing OAuth MCP flow still succeeds

  Scenario: Base path works with API key
    Given LeafWiki runs with base path /wiki
    And a user has an API key
    When an MCP client connects to /wiki/mcp with the API key
    Then the session initializes successfully
    And get_config returns basePath /wiki

Feature: TypeScript MCP client Bearer compatibility
  Scenario: TypeScript SDK client sends API key as Bearer auth
    Given local E2E starts LeafWiki with auth enabled and MCP enabled
    And an editor user has an API key
    When the official MCP TypeScript client connects using its authProvider token callback with the API key
    Then the server receives Authorization: Bearer <api key>
    And the MCP session initializes successfully
    And listTools returns LeafWiki tools
    And get_current_user returns the key owner

  Scenario: TypeScript SDK client uses API-key Bearer auth for tool calls
    Given the official MCP TypeScript client is connected with an API key
    When it calls create_page and get_page
    Then both calls succeed according to the key owner role
    And HTTP UI/API can read the created page

  Scenario: TypeScript SDK client fails cleanly after key revocation
    Given the official MCP TypeScript client previously connected with an API key
    And the key is revoked
    When a new TypeScript SDK client connects with the revoked key
    Then connection fails unauthorized
    And no OAuth browser authorization flow is started for that API-key path

Feature: Admin UI API key management
  Scenario: Admin opens a user's API key dialog
    Given I am logged in as admin
    When I open User Management
    And I click MCP API Keys for a user
    Then I see that user's active API keys
    And I can create or revoke keys from the dialog

  Scenario: Admin creates a user key and copies the secret
    Given I am logged in as admin
    When I create a key for a user
    Then a one-time secret is displayed
    And a copy action is available
    And closing and reopening the dialog never shows the secret again

  Scenario: Admin revokes a user key
    Given a user has an active key
    When admin revokes it in the UI
    Then the key disappears from the active list
    And using the key for MCP fails

  Scenario: Admin create validation stays in dialog
    Given I am logged in as admin
    When I submit an invalid key name
    Then the dialog remains open
    And field validation is shown

Feature: Self-service UI API key management
  Scenario: User opens own MCP API keys page
    Given I am logged in as editor
    When I open the account menu
    And I choose MCP API Keys
    Then I see only my active keys

  Scenario: User creates own key from UI
    Given password auth is enabled
    And I am logged in as editor
    When I create a key with a valid name and current password
    Then the one-time secret is displayed
    And the key works for MCP as my user

  Scenario: User cannot create own key with wrong password
    Given I am logged in as editor
    When I submit a wrong current password
    Then the form shows a current password error
    And no secret is displayed

  Scenario: Remote-user UI disables self-create
    Given HTTP remote-user auth is enabled
    When I open my MCP API Keys page
    Then I can list and revoke my keys
    And creating a new key is unavailable in MVP
```

## Test Plan

- Core auth unit tests:
  - key generation, hashing, lookup, metadata listing, revoke, malformed key parsing, wrong secret, unknown id.
- HTTP route tests:
  - admin/self permissions, CSRF behavior, validation shape, auth-disabled behavior, copy-once secret response.
- MCP integration tests:
  - API-key bearer success, invalid/revoked/deleted-user keys, viewer denial, role downgrade, base path, OAuth non-regression.
- TypeScript SDK E2E tests:
  - Add/extend `e2e/tests/mcp-api-keys.spec.ts`.
  - Use the official MCP TypeScript client via existing `connectMCPClient`.
  - Pass the API key through the existing `accessToken` option so the SDK auth provider sends `Authorization: Bearer <api key>`.
  - Assert `listTools`, `get_current_user`, and at least one read/write tool through the TypeScript client.
  - Assert revoked API key fails with the TypeScript client and does not invoke the OAuth browser/DCR path.
- UI E2E tests:
  - Admin-created key, self-created key, copy-once display, revoke, validation, viewer denial, MCP/UI round trip.
- Regression commands:
  - `rtk go test ./internal/core/auth ./internal/http ./internal/wiki/mcp`
  - `rtk go test ./...`
  - `rtk go mod tidy -diff`
  - `rtk npm --prefix ui/leafwiki-ui run build`
  - `rtk npm --prefix ui/leafwiki-ui run lint`
  - `rtk npm --prefix e2e run lint`
  - `rtk npm --prefix e2e run format:check`
  - `rtk env E2E_RUN_MODE=local E2E_ENABLE_MCP_OAUTH_LOCAL=1 ./e2e/run.sh --grep "mcp.*oauth|oauth.*mcp"`
  - `rtk env E2E_RUN_MODE=local E2E_ENABLE_MCP_LOCAL=1 ./e2e/run.sh --grep "mcp.*disable auth|disable auth.*mcp"`
  - `rtk env E2E_RUN_MODE=local E2E_ENABLE_MCP_API_KEYS_LOCAL=1 ./e2e/run.sh tests/mcp-api-keys.spec.ts`

## Definition Of Done

- `plans/api_keys.PLAN.md` exists as the canonical plan with this thread link, API-key behavior, Gherkin scenarios, and this DoD.
- Admins can create/list/revoke MCP API keys for users from user management.
- Users can list/revoke/create their own keys under password auth, with current-password confirmation for create.
- HTTP remote-user self-create is disabled in MVP and documented.
- API keys are MCP-only Bearer credentials and do not authenticate normal HTTP API routes.
- Raw API key secrets are shown once only and are never persisted or returned by list endpoints.
- Revoked keys, malformed keys, deleted-user keys, and wrong-secret keys are rejected with 401 by `/mcp`.
- Viewer-owned API keys can read via MCP but cannot mutate.
- Role changes after key issuance affect later MCP requests.
- Official MCP TypeScript client E2E proves API keys work via `Authorization: Bearer <api key>`.
- Official MCP TypeScript client E2E proves revoked API-key Bearer auth fails cleanly.
- OAuth MCP behavior, metadata, DCR, refresh, base-path challenge, and existing OAuth E2E all still pass.
- API-key docs are complete in `docs/mcp.md` and linked from `README.md`.
- All commands in the Test Plan pass.
- No unrelated files are reformatted or refactored.
