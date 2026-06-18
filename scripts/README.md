# Scripts

This directory contains local helper scripts for building, installing, release prep, and lightweight script validation. Most scripts are intended to be run from the repository root, although they resolve the repo root from their own location where practical.

When in doubt, run the relevant script with `--help` or `--dry-run` first.

## Common Flows

### Install Local macOS Helpers

Use this when you want the main LeafWiki server and MCP wrapper in the same local bin directory.

```bash
./scripts/install-all-macos.sh --dry-run --install-dir "$HOME/.local/bin"
./scripts/install-all-macos.sh --install-dir "$HOME/.local/bin"
```

This installs:

- `leafwiki`
- `run.sh`

It delegates to `install-macos.sh` for the application binary, then installs the MCP wrapper script next to the binary so clients can spawn it from a stable path.

### Install LeafWiki From This Checkout On macOS

Use this when you want a local production-style `leafwiki` binary built from the current working tree.

```bash
./scripts/install-macos.sh --dry-run
./scripts/install-macos.sh
```

Default install target: `/usr/local/bin/leafwiki`.

This script builds the frontend, copies it into `internal/http/dist`, builds the Go server with production frontend embedding enabled, and installs the binary. It may use `sudo` if the install directory is not writable.

### Run LeafWiki As An MCP STDIO Command

Use this when an MCP client wants a single command that speaks MCP over STDIO. The script execs a foreground `leafwiki --mcp=stdio` session frontend, which first attaches through the selected workspace descriptor when it is healthy. If that descriptor is missing, stale, or not authorized for the selected workspace, the frontend asks `wikid` in the install-wide LeafWiki runtime to register or ensure the workspace and then retries descriptor attach.

```bash
./scripts/run.sh mcp
```

Example MCP client command:

```bash
/path/to/leafwiki/scripts/run.sh mcp --root-dir /path/to/wiki
```

The wrapper keeps stdout reserved for MCP JSON-RPC protocol frames. Wrapper-level diagnostics go to stderr; `wikid`, `frontd`, and `workspaced` startup/server logs use LeafWiki file logging by default. OAuth-capable MCP clients should connect to LeafWiki's Streamable HTTP MCP endpoint directly.

`run.sh mcp` enables workspace sync by default, so markdown files under `--root-dir` are synchronized and recorded in LeafWiki's internal Git history. Pass `--disable-workspace-sync` or set `LEAFWIKI_RUN_MCP_ENABLE_WORKSPACE_SYNC=0` when you explicitly need the legacy no-sync behavior.

## Script Reference

| Script | Purpose | Typical command | Notes |
| --- | --- | --- | --- |
| `install-all-macos.sh` | Build and install `leafwiki` plus `run.sh`. | `./scripts/install-all-macos.sh --install-dir "$HOME/.local/bin"` | Delegates to `install-macos.sh`, then installs the wrapper script. |
| `install-macos.sh` | Build and install the main `leafwiki` executable from this checkout on macOS. | `./scripts/install-macos.sh` | Builds the UI, updates ignored frontend build output, builds the server with production embedding, and installs to `/usr/local/bin` by default. |
| `run.sh` | Run native MCP STDIO or one agent hook invocation. | `./scripts/run.sh mcp --root-dir ./wiki` | Intended as an MCP client or user-managed hook command. Supports disabled-auth and API-key native STDIO. |
| `changelog.sh` | Generate categorized release notes from commits between two tags. | `./scripts/changelog.sh v0.10.0 v0.11.0` | Writes `current_release_changelog.md` in the current working directory. Used by the release workflow. |
| `test-install.sh` | Validate root `install.sh` configuration handling without performing a real system install. | `./scripts/test-install.sh` | Uses fake `systemctl`/`wget` and `LEAFWIKI_INSTALL_VALIDATE_ONLY=1`. This tests the Linux installer at repo root, not the macOS installer. |
| `test-install-macos.sh` | Lightweight checks for `install-macos.sh`. | `./scripts/test-install-macos.sh` | Checks syntax, help text, and dry-run planning without installing. |
| `test-install-all-macos.sh` | Lightweight checks for `install-all-macos.sh`. | `./scripts/test-install-all-macos.sh` | Checks syntax, help text, and dry-run planning without installing. |
| `test-run.sh` | Lightweight checks for `run.sh`. | `./scripts/test-run.sh` | Checks syntax, help text, dry-run planning, redaction, stdin forwarding, and stdout hygiene with fake binaries. |

## Build And Install Scripts

### `install-all-macos.sh`

Builds and installs local macOS helpers from the current checkout: `leafwiki` and `run.sh`.

Useful commands:

```bash
./scripts/install-all-macos.sh --help
./scripts/install-all-macos.sh --dry-run --install-dir "$HOME/.local/bin"
./scripts/install-all-macos.sh --install-dir "$HOME/.local/bin"
./scripts/install-all-macos.sh --install-dir "$HOME/.local/bin" --skip-npm-ci
```

Important side effects:

- Runs the main app install flow from `install-macos.sh`.
- Installs the wrapper into the selected install directory.
- May use `sudo` if the install directory is not writable.

Use `--build-dir` to set the app build output directory. `--server-build-dir` is accepted as an alias for `--build-dir`.

### `install-macos.sh`

Builds the main LeafWiki application for macOS and installs it as `leafwiki`. This is for local macOS installs from the current checkout. It is separate from the root-level `install.sh`, which downloads published Linux release binaries and installs a `systemd` service.

Useful commands:

```bash
./scripts/install-macos.sh --help
./scripts/install-macos.sh --dry-run
./scripts/install-macos.sh --install-dir "$HOME/.local/bin"
./scripts/install-macos.sh --skip-npm-ci
```

Important side effects:

- Runs `npm ci --ignore-scripts` unless `--skip-npm-ci` is set.
- Runs the production Vite build for `ui/leafwiki-ui`.
- Copies `ui/leafwiki-ui/dist` into `internal/http/dist`.
- Writes the built binary under `releases/` by default.
- Installs `leafwiki` into the selected install directory.

### `run.sh`

Starts a project-local MCP STDIO command for clients that spawn one process. The foreground `leafwiki --mcp=stdio` process speaks MCP over STDIO, first reads the workspace-local descriptor at `<data-dir>/.leafwiki/project-daemon.json`, and bridges to the selected workspace daemon's private MCP endpoint when the descriptor is healthy. Missing, stale, or unauthorized descriptors fall back through the install-wide `wikid` runtime before descriptor attach is retried.

Default disabled-auth command shape:

```bash
leafwiki \
  --mcp=stdio \
  --disable-auth=true \
  --host 127.0.0.1 \
  --port 8080 \
  --data-dir ./.wiki \
  --root-dir ./wiki \
  --daemon-idle-timeout 10m \
  --log-target file \
  --enable-workspace-sync \
  --allow-insecure \
  --disable-request-log
```

API-key command shape:

```bash
env \
  LEAFWIKI_MCP_API_KEY=<api-key> \
leafwiki \
  --mcp=stdio \
  --host 127.0.0.1 \
  --port 8080 \
  --data-dir ./.wiki \
  --root-dir ./wiki \
  --daemon-idle-timeout 10m \
  --log-target file \
  --enable-workspace-sync \
  --allow-insecure \
  --disable-request-log
```

Config-file command shape:

```bash
leafwiki \
  --config ./leafwiki.yml
```

For `run.sh mcp --config`, the YAML file must include `mcp: stdio` or `mcp: http,stdio` when the caller expects the process to speak MCP over stdin/stdout.

For hooks, the wrapper preserves the subcommand after `--config`:

```bash
leafwiki \
  --config ./leafwiki.yml \
  agent-hook codex
```

Useful commands:

```bash
./scripts/run.sh --help
./scripts/run.sh mcp --dry-run
./scripts/run.sh mcp --dry-run --disable-workspace-sync
./scripts/run.sh mcp --dry-run --config ./leafwiki.yml
./scripts/run.sh mcp --root-dir "$PWD/wiki" --data-dir "$PWD/.wiki"
./scripts/run.sh mcp --root-dir "$PWD/docs" --markdown-link-root-prefix /docs
./scripts/run.sh agent-hook codex --root-dir "$PWD/wiki" --data-dir "$PWD/.wiki"
./scripts/run.sh agent-hook codex --config ./leafwiki.yml
LEAFWIKI_MCP_API_KEY=lwk_<id>_<secret> ./scripts/run.sh mcp --root-dir "$PWD/wiki"
```

Wrapper behavior:

- `--config <path>` passes only `--config <path>` to `leafwiki` for MCP mode, or `--config <path> agent-hook <provider>` for hook mode.
- MCP config mode does not infer STDIO; set `mcp: stdio` or `mcp: http,stdio` in YAML for spawned MCP clients.
- Config mode rejects normal wrapper options such as `--root-dir`, `--data-dir`, `--disable-auth`, `--enable-workspace-sync`, `--api-key`, and `--server-arg`. Use YAML keys that mirror public LeafWiki CLI flags instead.
- Config mode does not append wrapper defaults. YAML values override environment variables for present keys; omitted YAML keys still use LeafWiki environment/default behavior.
- Workspace sync is enabled by default. Use `--disable-workspace-sync` or `LEAFWIKI_RUN_MCP_ENABLE_WORKSPACE_SYNC=0` to omit `--enable-workspace-sync`.
- `--markdown-link-root-prefix` and `LEAFWIKI_RUN_MCP_MARKDOWN_LINK_ROOT_PREFIX` pass through to LeafWiki outside config mode. Use YAML `markdown-link-root-prefix:` in config mode. This is separate from `--base-path`; it only controls authored/generated Markdown hrefs such as `/docs/page.md`.
- Outside config mode, `--api-key` and `LEAFWIKI_RUN_MCP_API_KEY` are translated into `LEAFWIKI_MCP_API_KEY` for the child process. In config mode, wrapper env translation is skipped; put `api-key:` in YAML or set `LEAFWIKI_MCP_API_KEY` directly.
- API-key attach mode does not need `LEAFWIKI_JWT_SECRET` or `LEAFWIKI_ADMIN_PASSWORD` when a compatible install-wide runtime is already running.
- If this invocation must bootstrap a new auth-enabled runtime, pass `--jwt-secret` and `--admin-password`; dry-run output redacts these values.
- `--disable-auth` cannot be combined with an API key.
- `--daemon-idle-timeout` controls how long the install-wide runtime remains alive after the last STDIO/server session or presence record exits. `wikid` supervises child workspace daemons and stops them during global runtime shutdown. Use `0` for immediate shutdown.
- Wrapper diagnostics and foreground validation errors go to stderr. Detached `wikid`, `frontd`, and `workspaced` processes write startup and server logs to the LeafWiki log file by default.
- When the wrapper attaches to an existing install-wide runtime, `frontd`'s host, public MCP, log target/file, and request-log settings remain authoritative.
- Public HTTP MCP must use a loopback host, but this wrapper starts only a native STDIO frontend and may attach to an existing runtime bound to a non-loopback web host through private loopback control.
- `--log-target stdout` is not used because stdout is reserved for MCP protocol frames.
- Repeated `--server-arg <arg>` values are appended to the `leafwiki` command.

Many MCP clients do not expand shell variables inside JSON config. Use absolute paths instead of `$HOME` in `command`, `args`, and `env` values. Each JSON `args` entry is one process argument. Split flags and values into separate entries, or use `--flag=value`.

## Test Scripts

The script smoke tests are intentionally lightweight and do not require real installs:

```bash
./scripts/test-install.sh
./scripts/test-install-macos.sh
./scripts/test-install-all-macos.sh
./scripts/test-run.sh
```

Focused shell checks:

```bash
bash -n scripts/install-macos.sh scripts/install-all-macos.sh scripts/run.sh
bash -n scripts/test-install-macos.sh scripts/test-install-all-macos.sh scripts/test-run.sh
```

Related Go and E2E checks:

```bash
rtk go test ./cmd/leafwiki ./internal/projectdaemon ./internal/wikid ./internal/frontd ./internal/workspaced ./internal/wiki ./internal/wiki/mcp
E2E_RUN_MODE=local E2E_ENABLE_MCP_LOCAL=1 E2E_MCP_CLIENT_TRANSPORT=stdio ./e2e/run.sh tests/mcp-stdio-disable-auth.spec.ts
E2E_RUN_MODE=local E2E_ENABLE_MCP_API_KEYS_LOCAL=1 E2E_MCP_CLIENT_TRANSPORT=stdio ./e2e/run.sh tests/mcp-stdio-api-keys.spec.ts
E2E_RUN_MODE=local E2E_USE_CONFIG_FILE=1 ./e2e/run.sh tests/health.spec.ts
E2E_RUN_MODE=local E2E_ENABLE_MCP_LOCAL=1 E2E_MCP_CLIENT_TRANSPORT=stdio E2E_USE_CONFIG_FILE=1 ./e2e/run.sh tests/mcp-stdio-disable-auth.spec.ts --grep "mcp stdio seeds"
```

## Environment Overrides

| Script | Environment variables |
| --- | --- |
| `install-all-macos.sh` | `LEAFWIKI_INSTALL_DIR`, `LEAFWIKI_BUILD_DIR`, `LEAFWIKI_VERSION`, `LEAFWIKI_ARCH`, `GOARCH` |
| `install-macos.sh` | `LEAFWIKI_INSTALL_DIR`, `LEAFWIKI_BUILD_DIR`, `LEAFWIKI_VERSION`, `LEAFWIKI_ARCH`, `GOARCH` |
| `run.sh` | `LEAFWIKI_RUN_MCP_LEAFWIKI_BIN`, `LEAFWIKI_BIN`, `LEAFWIKI_RUN_MCP_HOST`, `LEAFWIKI_HOST`, `LEAFWIKI_RUN_MCP_PORT`, `LEAFWIKI_PORT`, `LEAFWIKI_RUN_MCP_BASE_PATH`, `LEAFWIKI_BASE_PATH`, `LEAFWIKI_RUN_MCP_DATA_DIR`, `LEAFWIKI_DATA_DIR`, `LEAFWIKI_RUN_MCP_ROOT_DIR`, `LEAFWIKI_ROOT_DIR`, `LEAFWIKI_RUN_MCP_JWT_SECRET`, `LEAFWIKI_JWT_SECRET`, `LEAFWIKI_RUN_MCP_ADMIN_PASSWORD`, `LEAFWIKI_ADMIN_PASSWORD`, `LEAFWIKI_RUN_MCP_ALLOW_INSECURE`, `LEAFWIKI_ALLOW_INSECURE`, `LEAFWIKI_RUN_MCP_DISABLE_AUTH`, `LEAFWIKI_DISABLE_AUTH`, `LEAFWIKI_RUN_MCP_DISABLE_REQUEST_LOG`, `LEAFWIKI_DISABLE_REQUEST_LOG`, `LEAFWIKI_RUN_MCP_DAEMON_IDLE_TIMEOUT`, `LEAFWIKI_DAEMON_IDLE_TIMEOUT`, `LEAFWIKI_RUN_MCP_ENABLE_WORKSPACE_SYNC`, `LEAFWIKI_RUN_MCP_API_KEY`, `LEAFWIKI_MCP_API_KEY`, `LEAFWIKI_RUN_MCP_SERVER_LOG` |

`LEAFWIKI_RUN_MCP_SERVER_LOG` is accepted only for compatibility with older wrapper configurations. The native wrapper ignores it and uses the LeafWiki log target configured in the child command.

In `--config` mode, `run.sh` still honors `LEAFWIKI_RUN_MCP_LEAFWIKI_BIN`/`LEAFWIKI_BIN` to select the binary. Other wrapper environment defaults are not translated into child arguments.
