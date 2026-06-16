<!-- leafwiki
version: 1
page:
  id: rbaOKv-DgI
  title: Fosite OAuth Migration - Context
  created_at: "2026-06-15T21:30:11.278018057Z"
  updated_at: "2026-06-15T21:30:11.278018057Z"
  creator_id: system
  last_author_id: system
-->

# Fosite OAuth Migration - Context

## Purpose

This document captures the Orient step from `docs/plans/planning.aibasic.txt`: how the observations were interpreted before selecting the implementation path.

## Problem Frame

LeafWiki already has working local MCP OAuth backed by `go-oauth2`. The migration is not a product feature. It is a library swap that should make the OAuth foundation easier to carry into future workspace federation and `sessiond` design.

The key planning tension is that Fosite is stricter and more explicit than the current implementation. If the migration lets Fosite defaults leak directly to clients, it could accidentally change observable OAuth behavior. If the migration hides too much, it loses the value of adopting Fosite's stronger request/session model.

## Constraints That Shape The Plan

- Preserve external OAuth/MCP routes and metadata.
- Preserve DCR and the official MCP TypeScript client OAuth flow.
- Preserve the fixed `leafwiki-local-mcp` fallback redirect behavior.
- Accept Fosite state strictness instead of building a short-state compatibility path.
- Keep API keys separate from OAuth.
- Keep token and client storage in memory for this migration.
- Do not add public revocation, public introspection, JWT access tokens, durable OAuth storage, daemon processes, or workspace grants.
- Preserve role enforcement by loading current users at request/tool time.

## Architecture Reading

The existing package boundary is useful. `internal/wiki/oauth` already owns OAuth state, clients, routes, and response adapters. The least disruptive approach is to keep that package boundary and replace its internals.

The future federation design wants seams for clients, sessions, subjects, audience/resource, revocation state, and verification. The first Fosite plan should create those seams in memory, not persist or distribute them.

MCP should remain a consumer of an OAuth verifier, not a Fosite-aware package. `internal/wiki/mcp/routes.go` should keep its API-key-first dispatch and delegate OAuth bearer tokens through the OAuth service.

## Alternative Paths Considered

### Replace `go-oauth2` In Place With Fosite Calls Only

This is too shallow. It swaps calls in `service.go` but leaves Fosite storage, client, and verifier semantics hidden in route handlers. It also makes it harder to test Fosite storage behavior before touching the external route flow.

### Introduce Full Durable OAuth Storage

This is too broad. Durable storage is valuable later, but it changes restart behavior and would expand the blast radius beyond a library migration.

### Add Public Revocation And Introspection

This is out of scope. Fosite has handlers for these capabilities, but LeafWiki does not currently expose or advertise them. Adding them would be a new user-facing OAuth capability.

### Use JWT Access Tokens

This is out of scope. JWTs introduce issuer/audience/key-management and revocation tradeoffs before `workspaced` exists. Opaque tokens preserve the current server-side verification model.

### Use `ComposeAllEnabled`

This is unsafe. It would register unsupported grant types and handlers that LeafWiki does not expose. The migration should compose only the handlers needed for current behavior.

### Lower Fosite State Entropy To Preserve Short Test Values

Rejected by user decision. Tests and examples should use realistic state values. Short or missing state should be rejected.

## Selected Shape

Build a Fosite-backed OAuth module inside `internal/wiki/oauth`:

- Add package-local Fosite config and HMAC strategy setup.
- Add LeafWiki-owned in-memory OAuth store implementing Fosite client, code, PKCE, access-token, refresh-token, and revocation storage interfaces.
- Represent clients with Fosite-compatible records while keeping LeafWiki's DCR contract.
- Keep metadata, approval, DCR, route registration, and MCP bearer challenge owned by LeafWiki.
- Adapt authorization and token routes to Fosite while preserving existing status/body/redirect behavior.
- Add an internal verifier method that validates opaque access tokens and returns the same MCP `TokenInfo` shape as today.

## Sequencing Rationale

The safest sequence is inside-out:

1. Add dependency/config and package-local tests.
2. Build storage and client records without route changes.
3. Add Fosite service composition behind the existing service constructor.
4. Adapt authorization and token routes while keeping external route contracts.
5. Re-run existing MCP integration and E2E tests.
6. Remove `go-oauth2` once the compatibility suite is green.

This sequence gives implementers early failing tests for Fosite-specific defaults before changing the visible flow.

## Risks To Watch

- Fosite may reject redirects or state earlier than current code. The plan needs explicit adapter/prevalidation behavior.
- Fosite error writers may produce different status codes or bodies. LeafWiki response adapters must normalize where current tests expect a shape.
- Refresh rotation and code reuse introduce revocation behavior that must be internal-only.
- In-memory store mistakes can produce races during concurrent OAuth/MCP tests. The store must use a single mutex or a clear locking scheme.
- Removing `go-oauth2` too early could hide whether failures are dependency wiring or behavior changes.

## Planning Assumptions

- The implementation agent can run focused Go tests repeatedly.
- The first implementation can update existing integration tests instead of adding a parallel duplicate suite for every old behavior.
- `references/fosite` is the local source of truth for Fosite integration details.
- No user decision is still blocking implementation planning.
