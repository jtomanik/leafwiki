<!-- leafwiki
version: 1
page:
  id: z--OFvavg
  title: Fosite OAuth Migration - Decision
  created_at: "2026-06-15T21:30:11.278139724Z"
  updated_at: "2026-06-15T21:30:11.278139724Z"
  creator_id: system
  last_author_id: system
-->

# Fosite OAuth Migration - Decision

## Purpose

This document captures the Decide step from `docs/plans/planning.aibasic.txt`.

## Decision

Proceed with a compatibility-preserving migration from `github.com/go-oauth2/oauth2/v4` to `github.com/ory/fosite` behind LeafWiki's existing local MCP OAuth route contract.

## Chosen Architecture

Implement Fosite inside `internal/wiki/oauth` behind the current `Service` and `Routes` boundary.

Create LeafWiki-owned internal components:

- Fosite config and HMAC opaque-token strategy.
- In-memory Fosite store for clients, authorization codes, PKCE requests, access tokens, refresh tokens, and revocation state.
- Fosite-compatible client/session records with subject, scope, client, expiry, and resource/audience placeholders.
- Response adapters that preserve current authorization redirect and token/DCR JSON behavior.
- Internal bearer verifier that returns the current MCP `TokenInfo` shape and reloads the current LeafWiki user.

Keep existing public components:

- Metadata routes and JSON shape.
- DCR route and validation contract.
- Approval route and UI.
- `/oauth/authorize` and `/oauth/token` paths.
- MCP `WWW-Authenticate` challenge.
- API-key bearer path.
- Auth-disabled MCP behavior.

## Key Decisions

1. Use narrow Fosite composition.
   - Reason: `ComposeAllEnabled` would enable unsupported grants and handlers.

2. Use HMAC opaque tokens.
   - Reason: preserves server-side verification, deleted-user invalidation, role-change enforcement, and revocation state.

3. Keep OAuth storage in memory for the first cut.
   - Reason: durable OAuth storage changes restart behavior and is not required for client compatibility.

4. Preserve fixed `leafwiki-local-mcp` redirect compatibility.
   - Reason: changing fixed-client loopback behavior would be an observable compatibility break.

5. Accept Fosite state strictness.
   - Reason: realistic OAuth clients should send non-trivial state values, and the user accepted this as the new baseline.

6. Keep DCR LeafWiki-owned.
   - Reason: Fosite does not provide LeafWiki's DCR route, validation, or response contract.

7. Keep revocation and introspection internal-only.
   - Reason: Fosite requires revocation state for correctness, but public endpoints would be new capabilities.

8. Keep API keys separate from OAuth.
   - Reason: API keys are an MCP-only persistent bearer path with different lifecycle and storage semantics.

## Alternatives Rejected

- Implementing daemon split at the same time.
  - Rejected because it combines auth semantics with process ownership changes.

- Adding durable OAuth client/token storage.
  - Rejected because it changes operational behavior and increases migration risk.

- Exposing `/oauth/revoke` or `/oauth/introspect`.
  - Rejected because they are new user-facing OAuth capabilities.

- Introducing JWT access tokens.
  - Rejected because offline verification is not needed before `workspaced`.

- Lowering Fosite `MinParameterEntropy`.
  - Rejected because the accepted direction is to update tests/examples to realistic state values.

## Open Questions Resolved

- Q: Should DCR clients use registered redirects normally?
  - A: Yes.

- Q: Should the fixed fallback keep current explicit loopback redirect behavior?
  - A: Yes, with an adapter if Fosite cannot express it directly.

- Q: Should tests with short OAuth state keep passing?
  - A: No. Update tests/examples to realistic state; add rejection coverage for short or missing state.

- Q: Should public revocation or introspection be added because Fosite supports it?
  - A: No.

- Q: Should this plan add federation, workspace grants, or daemon boundaries?
  - A: No. Add internal seams only.

## Planning Output

The implementation plan is `docs/plans/fosite-oauth-migration.PLAN.md`.
