# LeafWiki MCP STDIO Sidecar Implementation Plan

> **For agentic workers:** Use `superpowers:subagent-driven-development` or equivalent task isolation. Suggested split: one worker for the Go transport bridge, one for TypeScript/Playwright E2E, one for docs/release wiring.

**Goal:** Add a separate `leafwiki-mcp-stdio` CLI that transparently bridges MCP STDIO clients to LeafWiki’s existing Streamable HTTP `/mcp` endpoint without SSE, OAuth, or tool mirroring.

**Reference Thread:** `codex://threads/019e7add-e403-7783-9b83-97162c09f052`

**Plan Artifact:** save this plan in `plans/mcp_stdio_sidecar.PLAN.md` before implementation.

## Summary

Build a transport-level proxy:

```text
MCP STDIO client <-> leafwiki-mcp-stdio <-> HTTP POST /mcp <-> LeafWiki MCP server
```

The sidecar must not register, mirror, or understand LeafWiki tools. It forwards JSON-RPC frames and manages only transport concerns: HTTP headers, bearer API key injection, `Mcp-Session-Id`, `Mcp-Protocol-Version`, shutdown `DELETE`, stdout hygiene, and clear errors.

Use repo-local references:
- LeafWiki MCP endpoint and auth: [internal/wiki/mcp/routes.go](/Users/jakubtomanik/github/leafwiki/internal/wiki/mcp/routes.go:154)
- Current MCP docs and verification contract: [docs/mcp.md](/Users/jakubtomanik/github/leafwiki/docs/mcp.md:1)
- Go SDK stdio/streamable behavior: [references/go-sdk/docs/protocol.md](/Users/jakubtomanik/github/leafwiki/references/go-sdk/docs/protocol.md:123)
- TypeScript SDK `StdioClientTransport`: use the installed `@modelcontextprotocol/client` package from `e2e/package.json` and verify the local package exports during E2E implementation.

## Key Changes

### CLI and Public Interface

Create `cmd/leafwiki-mcp-stdio`.

Supported flags and env, using CLI > env > default:

| Flag | Env | Default | Meaning |
|---|---|---:|---|
| `--endpoint` | `LEAFWIKI_MCP_ENDPOINT` | `http://127.0.0.1:8080/mcp` | Upstream LeafWiki MCP URL |
| `--api-key` | `LEAFWIKI_MCP_API_KEY` | empty | Optional `lwk_...` key sent as bearer |
| `--request-timeout` | `LEAFWIKI_MCP_STDIO_REQUEST_TIMEOUT` | `2m` | Per upstream HTTP request |
| `--shutdown-timeout` | `LEAFWIKI_MCP_STDIO_SHUTDOWN_TIMEOUT` | `5s` | Time allowed for upstream `DELETE` |
| `--max-frame-size` | `LEAFWIKI_MCP_STDIO_MAX_FRAME_SIZE` | `128MiB` | Max single STDIO JSON-RPC frame |
| `--help` | n/a | n/a | Usage |

No data-dir, root-dir, auth server, OAuth, DCR, or LeafWiki server-start flags belong in this sidecar.

### Transport Behavior

Add an internal testable package, for example `internal/wiki/mcpstdio`, with a small `Run(ctx, config, stdin, stdout, stderr)` API.

Required behavior:

- Read newline-delimited JSON-RPC frames from stdin.
- Forward valid frames unchanged as HTTP `POST` request bodies.
- Write upstream JSON response bodies unchanged to stdout, followed by one newline.
- Never write logs, banners, progress, or diagnostics to stdout.
- Send HTTP headers:
  - `Content-Type: application/json`
  - `Accept: application/json, text/event-stream`
  - `Authorization: Bearer <api-key>` when configured
  - `Mcp-Session-Id` after upstream returns one
  - `Mcp-Protocol-Version` after initialize response returns `result.protocolVersion`
- Reject upstream `text/event-stream` responses with a JSON-RPC error for requests with an `id`; log only for notifications.
- For HTTP errors or connection failures, synthesize a JSON-RPC error with code `-32000` when the inbound frame has an `id`; for notifications, log to stderr and write no stdout.
- For malformed stdin JSON, emit a JSON-RPC parse error `-32700` with `id: null`.
- On stdin EOF, SIGINT, or SIGTERM, send upstream `DELETE /mcp` if a session ID exists. Treat `204` and `404` as successful cleanup.
- Redact API keys from all stderr output.
- Do not call `tools/list` or any MCP method unless the downstream client sent that exact JSON-RPC frame.

Concurrency default: process pre-initialize frames sequentially. After initialize completes, concurrent forwarding is allowed but stdout writes must be mutex-protected. If implementing concurrency is risky, keep v1 sequential and document that limitation.

### Build, Release, and Docs

- Add `make build-sidecar` for `leafwiki-mcp-stdio`.
- Update release packaging to include sidecar binaries for the same platforms as `leafwiki`, or add a clearly documented `release-sidecar` target.
- Do not change Docker server behavior in v1.
- Update `docs/mcp.md` with:
  - STDIO sidecar purpose
  - install/run examples
  - disabled-auth example
  - API-key example
  - base-path example
  - no-SSE/no-OAuth limitations
  - stdout/stderr warning
  - troubleshooting table
  - updated verification commands
- Update `README.md` with a short Local MCP STDIO section linking to `docs/mcp.md`.

## Gherkin Test Suite

```gherkin
Feature: Sidecar startup and configuration

  Scenario: Default endpoint is used when no endpoint is configured
    Given no endpoint flag or environment variable is set
    When leafwiki-mcp-stdio starts
    Then it uses http://127.0.0.1:8080/mcp as the upstream endpoint

  Scenario: CLI endpoint overrides environment endpoint
    Given LEAFWIKI_MCP_ENDPOINT is http://127.0.0.1:8080/mcp
    When leafwiki-mcp-stdio starts with --endpoint http://127.0.0.1:8080/wiki/mcp
    Then requests are sent to http://127.0.0.1:8080/wiki/mcp

  Scenario: API key is read from environment
    Given LEAFWIKI_MCP_API_KEY is set to a valid lwk key
    When the sidecar forwards an MCP request
    Then the upstream request includes Authorization: Bearer <key>

  Scenario: CLI API key overrides environment API key
    Given LEAFWIKI_MCP_API_KEY is set to key A
    When leafwiki-mcp-stdio starts with --api-key key B
    Then upstream requests use key B
    And stderr never prints key A or key B

  Scenario: Invalid endpoint fails before protocol output
    Given --endpoint is not a valid URL
    When leafwiki-mcp-stdio starts
    Then the process exits non-zero
    And stderr explains the invalid endpoint
    And stdout is empty

  Scenario: Help writes usage without starting the proxy
    When leafwiki-mcp-stdio starts with --help
    Then usage includes endpoint, api-key, request-timeout, shutdown-timeout, and max-frame-size
    And the process exits zero
```

```gherkin
Feature: Transparent STDIO to HTTP forwarding

  Scenario: Initialize is forwarded unchanged
    Given a fake upstream MCP HTTP server records request bodies
    When a STDIO client sends an initialize JSON-RPC request
    Then the upstream receives the same JSON-RPC method, id, and params
    And the sidecar writes the upstream JSON-RPC initialize response to stdout

  Scenario: Session id is preserved after initialize
    Given upstream initialize response includes Mcp-Session-Id session-123
    When the client sends notifications/initialized
    Then the upstream request includes Mcp-Session-Id session-123

  Scenario: Protocol version is preserved after initialize
    Given upstream initialize response has result.protocolVersion 2025-11-25
    When the client sends a later request
    Then the upstream request includes Mcp-Protocol-Version: 2025-11-25

  Scenario: Notification with accepted response emits no stdout
    Given the client sends a JSON-RPC notification without id
    And upstream returns 202 Accepted with no body
    Then the sidecar writes nothing to stdout

  Scenario: Successful JSON response is passed through exactly
    Given upstream returns an application/json JSON-RPC response
    When the sidecar receives it
    Then stdout contains that JSON-RPC response and a trailing newline

  Scenario: Sidecar does not mirror tools
    Given the client only sends initialize
    When the sidecar starts and connects
    Then upstream does not receive tools/list
    When the client later sends tools/list
    Then upstream receives exactly one tools/list request

  Scenario: Large JSON-RPC frame is accepted within limit
    Given max frame size is 128MiB
    When the client sends a valid large upload_asset tool call below the limit
    Then the sidecar forwards it to upstream

  Scenario: Oversized JSON-RPC frame is rejected
    Given max frame size is 1KiB
    When the client sends a frame larger than 1KiB
    Then the sidecar returns a JSON-RPC error
    And does not forward the frame upstream
```

```gherkin
Feature: Error and edge behavior

  Scenario: Malformed stdin JSON returns parse error
    When stdin contains a non-JSON line
    Then stdout contains a JSON-RPC parse error with id null
    And the sidecar continues accepting later valid frames

  Scenario: Upstream is unreachable
    Given the endpoint port is closed
    When the client sends initialize with id 1
    Then stdout contains a JSON-RPC error with id 1
    And stderr contains a bounded upstream connection diagnostic

  Scenario: Upstream returns unauthorized for a request
    Given upstream returns HTTP 401
    When the client sends initialize with id 1
    Then stdout contains a JSON-RPC error with id 1
    And the error data includes HTTP status 401
    And no API key appears in stdout or stderr

  Scenario: Upstream returns unauthorized for a notification
    Given upstream returns HTTP 401
    When the client sends a notification without id
    Then stdout is empty
    And stderr contains a bounded HTTP 401 diagnostic

  Scenario: Revoked API key fails cleanly
    Given LeafWiki auth is enabled
    And the configured API key has been revoked
    When the client connects over STDIO
    Then client connection fails
    And the sidecar does not hang

  Scenario: Deleted user API key fails cleanly
    Given LeafWiki auth is enabled
    And the API key owner was deleted
    When the client connects over STDIO
    Then client connection fails unauthorized
    And the sidecar does not fall back to public-editor

  Scenario: Upstream returns SSE response
    Given upstream returns Content-Type text/event-stream
    When the client sends a request with id 2
    Then stdout contains a JSON-RPC error with id 2
    And stderr says SSE is unsupported

  Scenario: Upstream returns unsupported content type
    Given upstream returns Content-Type text/plain
    When the client sends a request with id 3
    Then stdout contains a JSON-RPC error with id 3
    And stderr includes the upstream content type

  Scenario: Upstream session expires
    Given the sidecar has stored a session id
    And upstream later returns HTTP 404 for that session
    When the client sends a request
    Then stdout contains a JSON-RPC error
    And stderr says the upstream MCP session is missing or expired

  Scenario: Batch request does not hang
    Given the client sends a JSON-RPC batch frame
    When upstream rejects batching for the active protocol version
    Then the sidecar surfaces the upstream error or HTTP failure as JSON-RPC output
    And the sidecar remains usable for a later non-batch request
```

```gherkin
Feature: Shutdown lifecycle

  Scenario: Clean stdin close sends DELETE
    Given initialize stored Mcp-Session-Id session-123
    When stdin closes
    Then the sidecar sends DELETE /mcp with Mcp-Session-Id session-123
    And the process exits cleanly

  Scenario: Shutdown without session skips DELETE
    Given no upstream session id was stored
    When stdin closes
    Then no DELETE request is sent
    And the process exits cleanly

  Scenario: DELETE failure does not pollute stdout
    Given upstream DELETE returns HTTP 500
    When the sidecar shuts down
    Then stderr contains a bounded cleanup warning
    And stdout remains protocol-only
```

```gherkin
Feature: Real LeafWiki E2E over STDIO

  Scenario: Disabled-auth sidecar and UI share wiki state
    Given LeafWiki runs with --disable-auth and --enable-mcp
    And the TypeScript SDK connects with StdioClientTransport through leafwiki-mcp-stdio
    When the MCP client creates and updates a page
    Then the web UI renders the page
    When the user edits the page in the UI
    Then the MCP client reads the updated content through STDIO

  Scenario: API-key sidecar authenticates as key owner
    Given LeafWiki runs with auth enabled and --enable-mcp
    And an admin creates an MCP API key
    When the TypeScript SDK connects through leafwiki-mcp-stdio with that key
    Then get_current_user returns the key owner
    And create_page succeeds

  Scenario: Viewer API key can read but not mutate
    Given a viewer owns an MCP API key
    When the TypeScript SDK connects through leafwiki-mcp-stdio with that key
    Then get_tree succeeds
    And create_page returns the existing editor/admin permission error

  Scenario: Revoked key fails on a new STDIO connection
    Given a previously valid MCP API key was revoked
    When a new TypeScript SDK StdioClientTransport connects through the sidecar with that key
    Then connection fails
    And no OAuth browser flow is attempted

  Scenario: Base path endpoint works
    Given LeafWiki runs with --base-path /wiki and --enable-mcp
    When the sidecar endpoint is http://127.0.0.1:<port>/wiki/mcp
    Then get_config returns basePath /wiki
    And a normal tool call succeeds

  Scenario: Separate root dir works through sidecar
    Given LeafWiki runs with separate data dir and root dir
    When a STDIO MCP client creates a page through the sidecar
    Then page markdown is written under the root dir
    And app state remains under the data dir

  Scenario: STDIO stdout is protocol-only in E2E
    Given sidecar debug logging is enabled
    When the TypeScript SDK connects and calls a tool
    Then stdout contains only JSON-RPC frames
    And diagnostics are available on stderr
```

## Implementation Tasks

1. Add `plans/mcp_stdio_sidecar.PLAN.md` containing this plan and thread link.

2. Add `internal/wiki/mcpstdio` with config parsing helpers, frame reading, HTTP forwarding, response handling, state tracking, and shutdown cleanup.

3. Add `cmd/leafwiki-mcp-stdio/main.go` with flags, env resolution, signal handling, stderr logging, redaction, and usage output.

4. Add Go unit/integration tests using fake upstream HTTP servers for all startup, header, session, protocol, notification, HTTP error, SSE rejection, malformed JSON, oversized frame, and shutdown scenarios.

5. Add TypeScript E2E helper support:
   - Extend `e2e/tests/mcpClient.ts` with `connectMCPStdioClient`.
   - Use `@modelcontextprotocol/client/stdio`.
   - Spawn the sidecar with env `LEAFWIKI_MCP_ENDPOINT` and optional `LEAFWIKI_MCP_API_KEY`.
   - Capture stderr when tests expect startup or auth failure.

6. Add Playwright tests:
   - `e2e/tests/mcp-stdio-disable-auth.spec.ts`
   - `e2e/tests/mcp-stdio-api-keys.spec.ts`
   - Keep OAuth tests HTTP-only.

7. Update `e2e/run.sh`:
   - Pass `E2E_REPO_ROOT`.
   - When `E2E_MCP_CLIENT_TRANSPORT=stdio`, build a temp sidecar binary and expose `E2E_MCP_STDIO_COMMAND`.
   - Reuse existing `E2E_ENABLE_MCP_LOCAL` and `E2E_ENABLE_MCP_API_KEYS_LOCAL` modes; do not add a new server mode unless necessary.

8. Update docs and release/build wiring.

## Definition of Done

- `leafwiki-mcp-stdio` exists as a separate CLI binary.
- The sidecar is a transparent JSON-RPC transport bridge and does not mirror tools.
- The sidecar supports disabled-auth upstreams and MCP API-key bearer auth only.
- SSE and OAuth are explicitly unsupported and documented.
- All sidecar diagnostics go to stderr; stdout is protocol-only.
- Session ID, protocol version, bearer auth, JSON response forwarding, notification behavior, and shutdown `DELETE` are covered by tests.
- Non-happy paths are covered: invalid config, malformed JSON, oversized frame, unreachable upstream, 401/403/404/500, revoked/deleted-user key, unsupported content type, SSE response, batch rejection, and cleanup failure.
- Documentation includes usage examples, client config examples, limitations, troubleshooting, and updated verification commands.
- Verification commands pass:

```bash
rtk go test ./cmd/leafwiki ./cmd/leafwiki-mcp-stdio ./internal/wiki/mcpstdio ./internal/wiki/mcp ./internal/...
rtk go test ./...
rtk npm --prefix ui/leafwiki-ui run build
rtk npm --prefix ui/leafwiki-ui run lint
rtk npm --prefix e2e run lint
rtk npm --prefix e2e run format:check
rtk env E2E_RUN_MODE=local E2E_ENABLE_MCP_LOCAL=1 E2E_MCP_CLIENT_TRANSPORT=stdio ./e2e/run.sh tests/mcp-stdio-disable-auth.spec.ts
rtk env E2E_RUN_MODE=local E2E_ENABLE_MCP_API_KEYS_LOCAL=1 E2E_MCP_CLIENT_TRANSPORT=stdio ./e2e/run.sh tests/mcp-stdio-api-keys.spec.ts
rtk env E2E_RUN_MODE=local E2E_ENABLE_MCP_LOCAL=1 ./e2e/run.sh --grep "mcp.*disable auth|disable auth.*mcp"
rtk env E2E_RUN_MODE=local E2E_ENABLE_MCP_API_KEYS_LOCAL=1 ./e2e/run.sh tests/mcp-api-keys.spec.ts
rtk env E2E_RUN_MODE=local E2E_ENABLE_MCP_OAUTH_LOCAL=1 ./e2e/run.sh --grep "mcp.*oauth|oauth.*mcp"
```

## Assumptions

- LeafWiki remains the authoritative MCP server; the sidecar does not access wiki storage or domain services directly.
- Existing HTTP MCP tool parity tests remain the source of truth for LeafWiki tool behavior.
- STDIO sidecar E2E proves transport correctness, not every individual MCP tool.
- OAuth/DCR support is intentionally out of scope for the sidecar.
- SSE support is intentionally out of scope; returning SSE is treated as an error.
- v1 can be request/response oriented; server-initiated messages outside direct POST responses are out of scope.
