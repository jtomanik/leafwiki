#!/usr/bin/env bash
# Generated from internal/localization/locales/active.en.toml.

LEAFWIKI_RUN_MSG_USAGE='Usage: scripts/run.sh <mcp|agent-hook> [options]'
LEAFWIKI_RUN_MSG_HELP_BODY='Modes:
  mcp                     Run native LeafWiki MCP STDIO frontend
  agent-hook <provider>   Run one LeafWiki agent hook invocation

Options:
  --leafwiki-bin <path>     LeafWiki executable (default: leafwiki)
  --scheme <scheme>         Informational URL scheme for dry-run output (default: http)
  --host <host>             LeafWiki bind host (default: 127.0.0.1)
  --port <port>             LeafWiki port (default: 8080)
  --base-path <path>        LeafWiki base path, if any
  --data-dir <path>         LeafWiki data directory (default: ./.wiki)
  --root-dir <path>         LeafWiki root markdown directory (default: ./wiki)
  --markdown-link-root-prefix <path>
                            Repository-root Markdown href prefix, such as /docs
  --jwt-secret <secret>     JWT secret only when this run must bootstrap an auth-enabled owner
  --admin-password <pass>   Admin password only when this run must bootstrap an auth-enabled owner
  --disable-auth            Force disabled-auth STDIO identity
  --allow-insecure          Pass --allow-insecure to LeafWiki (default)
  --no-allow-insecure       Do not pass --allow-insecure
  --request-log             Keep LeafWiki request logs enabled
  --disable-request-log     Pass --disable-request-log to LeafWiki (default)
  --daemon-idle-timeout <d> Federated runtime idle timeout after the last session or presence record exits (default: 10m)
  --api-key <key>           Native STDIO API key; passed as LEAFWIKI_MCP_API_KEY
  --config <path>           Pass a LeafWiki YAML config file without wrapper defaults
  --server-arg <arg>        Extra argument passed to leafwiki; repeatable
  --dry-run                 Print the planned command without starting anything
  -h, --help                Show this help

Environment overrides use LEAFWIKI_RUN_MCP_* names matching the option names.
Use LEAFWIKI_RUN_MCP_API_KEY or LEAFWIKI_MCP_API_KEY to provide the native
STDIO API key without putting the secret in the child command line.'
LEAFWIKI_RUN_MSG_ERROR_PREFIX='Error:'
LEAFWIKI_RUN_MSG_DRY_RUN_MCP_CONFIG='Would run LeafWiki with YAML config for MCP'
LEAFWIKI_RUN_MSG_DRY_RUN_MCP_NATIVE='Would run LeafWiki native MCP STDIO'
LEAFWIKI_RUN_MSG_DRY_RUN_STDIO_ATTACH='STDIO attach: descriptor-first attach via <data-dir>/.leafwiki/project-daemon.json; wikid ensure/control for missing or stale descriptors'
LEAFWIKI_RUN_MSG_DRY_RUN_AGENT_HOOK='Would run LeafWiki agent hook'
LEAFWIKI_RUN_MSG_DRY_RUN_HTTP_CONFIG='HTTP UI: configured by'
LEAFWIKI_RUN_MSG_DRY_RUN_HTTP_URL='HTTP UI:'
LEAFWIKI_RUN_MSG_ERROR_AGENT_HOOK_REQUIRES_PROVIDER='agent-hook requires a provider'
LEAFWIKI_RUN_MSG_ERROR_ARGUMENT_CONTAINS_SPACE='argument contains a space; MCP JSON args must split flags and values into separate args, for example "--root-dir", "./wiki", or use --root-dir=./wiki'
LEAFWIKI_RUN_MSG_ERROR_CONFIG_CANNOT_COMBINE='--config cannot be combined with'
LEAFWIKI_RUN_MSG_ERROR_CONFIG_REQUIRES_PATH='--config requires a path'
LEAFWIKI_RUN_MSG_ERROR_DISABLE_AUTH_API_KEY_CONFLICT='--disable-auth cannot be combined with --api-key or LEAFWIKI_MCP_API_KEY'
LEAFWIKI_RUN_MSG_ERROR_EXECUTABLE_NOT_FOUND='executable not found or not executable'
LEAFWIKI_RUN_MSG_ERROR_EXECUTABLE_NOT_FOUND_ON_PATH='executable not found on PATH'
LEAFWIKI_RUN_MSG_ERROR_LEAFWIKI_BIN_REQUIRES_PATH='--leafwiki-bin requires a path'
LEAFWIKI_RUN_MSG_ERROR_OPTION_REQUIRES_ARGUMENT='requires an argument'
LEAFWIKI_RUN_MSG_ERROR_OPTION_REQUIRES_DURATION='requires a duration'
LEAFWIKI_RUN_MSG_ERROR_OPTION_REQUIRES_PASSWORD='requires a password'
LEAFWIKI_RUN_MSG_ERROR_OPTION_REQUIRES_PATH='requires a path'
LEAFWIKI_RUN_MSG_ERROR_OPTION_REQUIRES_SECRET='requires a secret'
LEAFWIKI_RUN_MSG_ERROR_OPTION_REQUIRES_VALUE='requires a value'
LEAFWIKI_RUN_MSG_ERROR_RUN_MODE_REQUIRED='first argument must be mcp or agent-hook'
LEAFWIKI_RUN_MSG_ERROR_UNKNOWN_OPTION='unknown option'
