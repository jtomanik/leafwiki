# Agent Hooks

Thread context: `codex://threads/019ea3a5-4240-7b80-b5f0-76c459294370`

LeafWiki can receive user-managed agent hook events through:

```bash
./scripts/run.sh agent-hook codex --root-dir ./wiki --data-dir ./.wiki
./scripts/run.sh agent-hook codex --config ./leafwiki.yml
```

Hooks are passive presence observers. They never make allow/deny decisions for agents. Valid hook invocations for supported providers return that provider's allow response on stdout.

## Privacy Contract

LeafWiki stores only sanitized in-memory presence:

- provider
- provider-scoped session hash
- event name
- optional model, source, and tool name
- MCP-tool flag
- bounded active-subagent count
- first/last seen timestamps

LeafWiki never stores raw session IDs, prompts, transcript paths, cwd, tool input or output, user email, API keys, JWT/admin secrets, or daemon control tokens. Session hashes are computed as:

```text
sha256(provider + "\x00" + rawSessionID)
```

## Fail-Open Contract

Valid hook invocations fail open: they exit `0` and write only the provider allow response to stdout.

| Provider | Allow stdout |
| --- | --- |
| Codex | `{}` |
| Claude | `{}` |
| Cursor | `{"permission":"allow"}` |
| Unknown | empty stdout |

Malformed JSON, unsupported providers, missing session fields, daemon startup failure, stale descriptors, locked projects, control API errors, timeouts, and panics all fail open. Diagnostics may be written to stderr or the LeafWiki log target, but raw hook payloads and secrets must not be logged.

Config-file hook mode has one hard-fail boundary before hook dispatch: config argument usage. Missing config path arguments, empty paths, bare-dash or dash-prefixed path values, flag-looking path values, CLI/wrapper option conflicts, and help/config mixes exit nonzero without a provider allow response. Examples include `leafwiki --config - agent-hook codex`, `leafwiki --config ./leafwiki.yml help`, `leafwiki --config ./leafwiki.yml agent-hook codex --root-dir ./wiki`, and `./scripts/run.sh agent-hook codex --config ./leafwiki.yml --root-dir ./wiki`.

Once `--config <path>` is syntactically valid and the config file is the sole startup source, hook mode keeps the same runtime fail-open behavior for config loading, YAML parsing and validation, workspace validation, daemon startup, and hook-processing failures. For example, `leafwiki --config ./missing.yml agent-hook codex` fails open for supported providers. Supported providers receive their allow response and raw hook payloads are not logged.

## Wrapper Commands

Native MCP STDIO:

```bash
./scripts/run.sh mcp --root-dir ./wiki --data-dir ./.wiki
```

Codex hook:

```bash
./scripts/run.sh agent-hook codex --root-dir ./wiki --data-dir ./.wiki
./scripts/run.sh agent-hook codex --config ./leafwiki.yml
```

Claude Code hook:

```bash
./scripts/run.sh agent-hook claude --root-dir ./wiki --data-dir ./.wiki
```

Cursor hook:

```bash
./scripts/run.sh agent-hook cursor --root-dir ./wiki --data-dir ./.wiki
```

The wrapper does not auto-install hooks. Configure Codex, Claude Code, or Cursor manually so their hook command points at `run.sh agent-hook <provider>` with the same `--data-dir` and `--root-dir` used by your MCP or web workflow.

When using config-file mode, put shared startup values in `leafwiki.yml` and pass only `--config <path>` to the wrapper. Config mode does not append wrapper defaults, and it rejects normal wrapper options such as `--root-dir` or `--disable-auth` when they are mixed with `--config`.
