# 🌿 LeafWiki

[![GitHub Stars](https://img.shields.io/github/stars/perber/leafwiki?style=flat-square)](https://github.com/perber/leafwiki/stargazers) [![Latest Release](https://img.shields.io/github/v/release/perber/leafwiki?style=flat-square)](https://github.com/perber/leafwiki/releases) [![Backend CI](https://github.com/perber/leafwiki/actions/workflows/backend.yml/badge.svg)](https://github.com/perber/leafwiki/actions/workflows/backend.yml) [![Frontend CI](https://github.com/perber/leafwiki/actions/workflows/frontend.yml/badge.svg)](https://github.com/perber/leafwiki/actions/workflows/frontend.yml)

Self-hosted wiki. Single Go binary. SQLite + Markdown stored on disk.

For engineers and self-hosters who want structured, long-lived documentation. No Node.js, no Redis, no Postgres — just a binary and a data directory.

![LeafWiki](./assets/preview.png)

If you've looked at Wiki.js or Outline and thought "this is too much to operate for what I need" — this could fit for you.

→ Try it without installing: **[demo.leafwiki.com](https://demo.leafwiki.com)** · `Ctrl+E` edit · `Ctrl+S` save · resets hourly  
→ If it fits, [a star](https://github.com/perber/leafwiki) helps others find it.

```bash
docker run -p 8080:8080 -v ~/leafwiki-data:/app/data \
  ghcr.io/perber/leafwiki:latest \
  --jwt-secret=yoursecret --admin-password=yourpassword --allow-insecure=true \
  --log-target=stderr
```

→ [All install options](#install) (Docker Compose, Linux installer, binary)

---

## Features

**Operations:**
- Single Go binary — no external database, no runtime dependencies
- Markdown on disk — page content is readable outside the app, backup is `cp -r` (stop the app first)
- Runs on Linux, macOS, Windows, Raspberry Pi (x86_64 and ARM64)
- Reverse-proxy friendly with `--base-path`
- Reverse-proxy authentication via trusted HTTP header (v0.10+)
- Public read-only mode with authenticated editing
- Roles: admin, editor, viewer

**Core functionality:**
- Tree navigation — explicit hierarchy, not flat note feeds
- Full-text search across titles and content, with tag-based filtering
- Tags on pages — searchable and filterable across the wiki
- Backlinks and link status per page (incoming, outgoing, broken links)
- Built-in Markdown editor with live preview, keyboard shortcuts, and autocomplete for canonical internal page links
- Optimistic locking for concurrent edits
- Markdown: tables, task lists, footnotes, callouts (`:::info` / `:::warning`), Mermaid diagrams, sanitized inline HTML

**Customization:**
- Custom stylesheet (`--custom-stylesheet`, v0.8.5+)
- Inject HTML/JS into `<head>` for analytics or custom CSS
- Branding: logo, favicon, site name
- Dark mode and mobile-friendly UI

**Opt-in via feature flags:**
- Revision history (`--enable-revision`; mutually exclusive with `--enable-workspace-sync`)
- Workspace sync and Git-backed Markdown history (`--enable-workspace-sync`; mutually exclusive with `--enable-revision`; see [docs/workspace-sync.md](docs/workspace-sync.md))
- Automatic link rewriting when pages are renamed or moved (`--enable-link-refactor`)

**Markdown import:**
- ZIP-based importer for editors and admins
- Supports Obsidian-style wiki link rewriting on import
- Emits GitHub-portable Markdown links: pages use `.md`, sections use extensionless folder links
- Best results with a reasonably clean folder structure; not a fully automatic converter for all source formats

**Mobile:**

<p align="center">
  <img src="./assets/mobile-editor.png" width="260" />
  <img src="./assets/mobile-pageview.png" width="260" />
  <img src="./assets/mobile-navigation.png" width="260" />
</p>

---

## Good fit / not a fit

**Good fit:**
- Personal wikis, engineering notebooks, and runbooks
- Internal team or homelab documentation
- Existing Markdown or Obsidian vaults that need a structured wiki UI
- Small teams that want tree navigation over flat note feeds
- Self-hosted environments with low operational overhead

**Probably not a fit:**
- Organizations needing complex enterprise permissions or approval workflows
- Real-time collaborative editing
- Teams looking for a Confluence or Notion replacement

LeafWiki is intentionally narrower than those systems. That focus is part of the value.

---

## Install

### Docker

```bash
docker run -p 8080:8080 \
    -v ~/leafwiki-data:/app/data \
    ghcr.io/perber/leafwiki:latest \
    --jwt-secret=yoursecret \
    --admin-password=yourpassword \
    --allow-insecure=true \
    --log-target=stderr
```

`--allow-insecure=true` is required for plain HTTP. Omit it when serving over HTTPS (make sure your reverse proxy forwards `X-Forwarded-Proto: https`).

**Non-root:**

```bash
docker run -p 8080:8080 \
    -u 1000:1000 \
    -v ~/leafwiki-data:/app/data \
    ghcr.io/perber/leafwiki:latest \
    --jwt-secret=yoursecret \
    --admin-password=yourpassword \
    --allow-insecure=true \
    --log-target=stderr
```

The data directory must be writable by the specified user.

If `LEAFWIKI_ROOT_DIR` or `--root-dir` points outside `/app/data`, mount that path as a second writable volume, for example `-v ~/leafwiki-pages:/app/pages -e LEAFWIKI_ROOT_DIR=/app/pages`.

### Docker Compose

```yaml
services:
  leafwiki:
    image: ghcr.io/perber/leafwiki:latest
    container_name: leafwiki
    user: 1000:1000
    ports:
      - "8080:8080"
    environment:
      - LEAFWIKI_JWT_SECRET=yourSecret
      - LEAFWIKI_ADMIN_PASSWORD=yourPassword
      - LEAFWIKI_ALLOW_INSECURE=true  # Required for plain HTTP. Omit for HTTPS (ensure `X-Forwarded-Proto: https` is forwarded).
      - LEAFWIKI_LOG_TARGET=stderr
    volumes:
      - ${HOME}/leafwiki-data:/app/data
    restart: unless-stopped
```

### Linux installer

```bash
sudo /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/perber/leafwiki/main/install.sh)"
```

Installs LeafWiki as a system service. Tested on Ubuntu, Debian, and Raspbian.

**Update:**

```bash
sudo /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/perber/leafwiki/main/update.sh)"
```

> Only works if you installed with the script above. Not compatible with Docker or binary installs.

**Non-interactive mode:**

```bash
cp .env.example .env
# Edit .env with your configuration
sudo ./install.sh --non-interactive --env-file ./.env
```

> Security: in interactive mode, environment variables are written in plain text to `/etc/leafwiki/.env`. Restrict access to that file.

**Deployment examples:**
- [Install with nginx on Ubuntu](docs/install/nginx.md)
- [Install on a Raspberry Pi](docs/install/raspberry.md)

### Binary

```bash
chmod +x leafwiki
./leafwiki --jwt-secret=yoursecret --admin-password=yourpassword --allow-insecure=true
```

The server binds to `127.0.0.1:8080` by default. To expose it on the network:

```bash
./leafwiki --jwt-secret=yoursecret --admin-password=yourpassword --host=0.0.0.0 --allow-insecure=true
```

Default data directory is `./data`. Change with `--data-dir`.
Managed markdown pages default to `./data/root`. Change with `--root-dir` or `LEAFWIKI_ROOT_DIR`.
Changing the root directory does not move existing markdown pages. For an existing install, move or copy the old `<data-dir>/root` content before starting LeafWiki with a new root directory.

### Reset admin password

```bash
./leafwiki reset-admin-password
```

---

## Dev Setup

**Stack:** Go · React (Vite) · SQLite

```bash
git clone https://github.com/perber/leafwiki.git
cd leafwiki
```

**Terminal 1 — Frontend:**
```bash
cd ui/leafwiki-ui
npm install
npm run dev
```

**Terminal 2 — Backend:**
```bash
cd cmd/leafwiki
go run main.go --jwt-secret=yoursecret --allow-insecure=true --admin-password=yourpassword
```

Vite starts on `http://localhost:5173`. The backend binds to `127.0.0.1` by default.

See [CONTRIBUTING.md](CONTRIBUTING.md) for contribution guidelines.

---

## Configuration

### Required

| Flag | Description |
|------|-------------|
| `--jwt-secret` | Secret for signing JWTs. Keep it secure. |
| `--admin-password` | Initial admin password (only applied if no admin exists yet). |

For plain HTTP: add `--allow-insecure=true` so login and CSRF cookies work.

### CLI Flags

| Flag                             | Description                                                             | Default       | Since   |
|----------------------------------|-------------------------------------------------------------------------|---------------|---------|
| `--host`                         | Host/IP the server binds to                                             | `127.0.0.1`   | –       |
| `--port`                         | Port the server listens on                                              | `8080`        | –       |
| `--data-dir`                     | Directory where data is stored                                          | `./data`      | –       |
| `--root-dir`                     | Directory where managed markdown pages and `.order.json` are stored     | `<data-dir>/root` | –    |
| `--markdown-link-root-prefix`     | Repository-root prefix for absolute Markdown hrefs, for example `/docs` | `""`          | v0.11.0 |
| `--config`                       | Flat YAML config file; cannot be combined with normal CLI flags         | `""`          | v0.11.0 |
| `--public-access`                | Allow public read-only access                                           | `false`       | –       |
| `--base-path`                    | URL prefix for reverse proxy setups (e.g. `/wiki`)                      | `""`          | v0.8.2  |
| `--allow-insecure`               | ⚠️ Enables HTTP for auth cookies (required for plain HTTP)              | `false`       | v0.7.0  |
| `--disable-auth`                 | ⚠️ Disable all authentication (internal networks only)                  | `false`       | v0.7.0  |
| `--access-token-timeout`         | Access token duration (e.g. `24h`, `15m`)                               | `15m`         | v0.7.0  |
| `--refresh-token-timeout`        | Refresh token duration (e.g. `168h`, `7d`)                              | `7d`          | v0.7.0  |
| `--max-asset-upload-size`        | Max upload size (e.g. `50MiB`, `52428800`)                              | `50MiB`       | v0.8.5  |
| `--custom-stylesheet`            | Path to a `.css` file inside the data dir                               | `""`          | v0.8.5  |
| `--log-target`                   | Log target: `file`, `stderr`, or `stdout`                               | `file`        | v0.11.0 |
| `--log-file`                     | Log file path when `--log-target=file`; relative paths use data dir      | `<data-dir>/.leafwiki/logs/leafwiki.log` | v0.11.0 |
| `--inject-code-in-header`        | Raw HTML/JS injected into `<head>`                                      | `""`          | v0.6.0  |
| `--hide-link-metadata-section`   | Hide backlinks and link status panel                                    | `false`       | –       |
| `--enable-revision`              | Enable revision history; mutually exclusive with workspace sync          | `false`       | v0.9.0  |
| `--enable-workspace-sync`        | Enable workspace sync and Git-backed Markdown history; mutually exclusive with revision history | `false` | v0.11.0 |
| `--enable-link-refactor`         | Enable link rewriting on rename/move                                    | `false`       | v0.9.0  |
| `--mcp`                          | MCP transports: `none`, `http`, `stdio`, `http,stdio`, or `stdio,http`; HTTP MCP requires a loopback host | `none` | v0.11.0 |
| `--api-key`                      | Native STDIO MCP API key; prefer `LEAFWIKI_MCP_API_KEY`                 | `""`          | v0.11.0 |
| `--daemon-idle-timeout`          | Project daemon idle timeout after the last session exits; `0` = immediate | `10m`       | v0.11.0 |
| `--max-revision-history`         | Max revisions per page; `0` = unlimited                                 | `100`         | v0.9.0  |
| `--enable-http-remote-user`      | Enable reverse-proxy auth via HTTP header                               | `false`       | v0.10.0 |
| `--http-remote-user-header-name` | Header name carrying the username from the proxy                        | `Remote-User` | v0.10.0 |
| `--trusted-proxy-ips`            | Trusted proxy IPs/CIDRs for remote-user header                          | `""`          | v0.10.0 |
| `--http-remote-user-logout-url`  | Logout redirect when reverse-proxy auth is active                       | `""`          | v0.10.0 |
| `--disable-request-log`          | Suppress per-request HTTP access logs                                   | `false`       | v0.11.0 |

> Docker image default: `LEAFWIKI_HOST` is set to `0.0.0.0` automatically by the container entrypoint if neither `--host` nor `LEAFWIKI_HOST` is provided.

### YAML Config File

Use `--config <path>` when you want a single startup file instead of CLI flags:

```yaml
host: 127.0.0.1
port: 8080
data-dir: ./.wiki
root-dir: ./wiki
markdown-link-root-prefix: /docs
jwt-secret: change-me
admin-password: change-me
allow-insecure: true
mcp: http
enable-workspace-sync: true
```

```bash
leafwiki --config ./leafwiki.yml
```

YAML keys mirror public CLI flag names without the leading `--`. `--config` cannot be combined with normal CLI flags, so `leafwiki --config ./leafwiki.yml --port 8081` fails. Values in YAML are treated like supplied CLI flag values: present YAML keys override environment variables, while omitted keys still fall back to environment variables and defaults. Explicit `""`, `false`, and `0` values are honored.

The config file is a flat mapping of scalar values. Unknown keys, duplicate keys, `null`, lists, maps, hidden compatibility flags such as `enable-mcp`/`mcp-stdio`, and internal-only flags such as `internal-project-daemon` are rejected. Secrets are allowed in YAML, so protect the file accordingly. The config file path itself is not part of project daemon identity; only the resolved runtime values are matched. This config-file contract is tracked by `codex://threads/019eb504-6cf3-7803-b033-bee91c3b028b`.

### Environment Variables

| Variable                                | Description                                          | Default       | Since   |
|-----------------------------------------|------------------------------------------------------|---------------|---------|
| `LEAFWIKI_HOST`                         | Host/IP address                                      | `127.0.0.1`   | –       |
| `LEAFWIKI_PORT`                         | Port                                                 | `8080`        | –       |
| `LEAFWIKI_DATA_DIR`                     | Data directory path                                  | `./data`      | –       |
| `LEAFWIKI_ROOT_DIR`                     | Managed markdown content directory                   | `<data-dir>/root` | –    |
| `LEAFWIKI_MARKDOWN_LINK_ROOT_PREFIX`    | Repository-root prefix for absolute Markdown hrefs   | `""`          | v0.11.0 |
| `LEAFWIKI_ADMIN_PASSWORD`               | Initial admin password *(required)*                  | –             | –       |
| `LEAFWIKI_JWT_SECRET`                   | JWT signing secret *(required)*                      | –             | –       |
| `LEAFWIKI_PUBLIC_ACCESS`                | Allow public read-only access                        | `false`       | –       |
| `LEAFWIKI_BASE_PATH`                    | URL prefix for reverse proxy                         | `""`          | v0.8.2  |
| `LEAFWIKI_ALLOW_INSECURE`               | ⚠️ HTTP auth cookies                                 | `false`       | v0.7.0  |
| `LEAFWIKI_DISABLE_AUTH`                 | ⚠️ Disable authentication                            | `false`       | v0.7.0  |
| `LEAFWIKI_ACCESS_TOKEN_TIMEOUT`         | Access token duration                                | `15m`         | v0.7.0  |
| `LEAFWIKI_REFRESH_TOKEN_TIMEOUT`        | Refresh token duration                               | `7d`          | v0.7.0  |
| `LEAFWIKI_MAX_ASSET_UPLOAD_SIZE`        | Max upload size                                      | `50MiB`       | v0.8.5  |
| `LEAFWIKI_CUSTOM_STYLESHEET`            | Path to `.css` file inside data dir                  | `""`          | v0.8.5  |
| `LEAFWIKI_LOG_LEVEL`                    | Log level: `debug`, `info`, `warn`, or `error`       | `info`        | –       |
| `LEAFWIKI_LOG_TARGET`                   | Log target: `file`, `stderr`, or `stdout`            | `file`        | v0.11.0 |
| `LEAFWIKI_LOG_FILE`                     | File path when log target is `file`                  | `<data-dir>/.leafwiki/logs/leafwiki.log` | v0.11.0 |
| `LEAFWIKI_INJECT_CODE_IN_HEADER`        | HTML/JS injected into `<head>`                       | `""`          | v0.6.0  |
| `LEAFWIKI_HIDE_LINK_METADATA_SECTION`   | Hide backlinks and link status panel                 | `false`       | –       |
| `LEAFWIKI_ENABLE_REVISION`              | Revision history                                     | `false`       | v0.9.0  |
| `LEAFWIKI_ENABLE_WORKSPACE_SYNC`        | Workspace sync and Git-backed Markdown history       | `false`       | v0.11.0 |
| `LEAFWIKI_ENABLE_LINK_REFACTOR`         | Link rewriting on rename/move                        | `false`       | v0.9.0  |
| `LEAFWIKI_MCP`                          | MCP transports: `none`, `http`, `stdio`, `http,stdio`, or `stdio,http` | `none` | v0.11.0 |
| `LEAFWIKI_MCP_API_KEY`                  | Native STDIO MCP API key                              | `""`          | v0.11.0 |
| `LEAFWIKI_DAEMON_IDLE_TIMEOUT`          | Project daemon idle timeout after last session exits  | `10m`         | v0.11.0 |
| `LEAFWIKI_MAX_REVISION_HISTORY`         | Max revisions per page; `0` = unlimited              | `100`         | v0.9.0  |
| `LEAFWIKI_ENABLE_HTTP_REMOTE_USER`      | Reverse-proxy auth via header                        | `false`       | v0.10.0 |
| `LEAFWIKI_HTTP_REMOTE_USER_HEADER_NAME` | Username header from proxy                           | `Remote-User` | v0.10.0 |
| `LEAFWIKI_TRUSTED_PROXY_IPS`            | Trusted proxy IPs/CIDRs                              | `""`          | v0.10.0 |
| `LEAFWIKI_HTTP_REMOTE_USER_LOGOUT_URL`  | Logout redirect URL                                  | `""`          | v0.10.0 |
| `LEAFWIKI_DISABLE_REQUEST_LOG`          | Suppress per-request HTTP access logs                | `false`       | v0.11.0 |

### Logging

By default, LeafWiki writes structured JSON logs to `<data-dir>/.leafwiki/logs/leafwiki.log`. Relative `--log-file` paths resolve under `--data-dir`; absolute paths are used as-is. Use `--log-target stderr` for Docker, systemd, or other supervisors that collect process stderr. Native STDIO owners are detached with stdout reserved for protocol traffic, so their owner logs are retained in the log file. `--log-target stdout` is available only when you explicitly want server logs on stdout.

LeafWiki does not rotate log files in-process. Configure external rotation with your service manager, container runtime, or a tool such as `logrotate`.

See [Logging](docs/logging.md) for examples.

### Custom Stylesheet

Place a `.css` file inside your data directory and pass its path:

```bash
./leafwiki \
  --data-dir=./data \
  --custom-stylesheet=custom.css \
  --jwt-secret=yoursecret \
  --admin-password=yourpassword
```

- File must exist at `./data/custom.css`
- Served as `/custom.css` (or `${base-path}/custom.css` with `--base-path`)
- The endpoint is publicly accessible

### Reverse-Proxy Authentication

Available since v0.10.0. Use when an upstream proxy authenticates users and forwards the username via HTTP header.

```bash
./leafwiki \
  --jwt-secret=yoursecret \
  --admin-password=yourpassword \
  --enable-http-remote-user=true \
  --http-remote-user-header-name=X-Forwarded-User \
  --trusted-proxy-ips=127.0.0.1,172.18.0.0/16 \
  --http-remote-user-logout-url=https://auth.example.com/logout
```

- Only trusts the header from IPs listed in `--trusted-proxy-ips`
- If the forwarded username doesn't exist in LeafWiki, the request is rejected
- Do not enable without configuring `--trusted-proxy-ips`

### Security

Enabled by default since v0.7.0:

- Secure, HttpOnly cookies for session handling
- CSRF protection on all state-changing requests
- Rate limiting on auth endpoints
- Role-based access: admin, editor, viewer

**`--disable-auth`** removes all authentication. Only use for local development, trusted internal networks, or isolated environments.

```bash
# Safe local-only example:
./leafwiki --disable-auth --host=127.0.0.1
```

For most setups, prefer `--public-access` for read-only public access and the viewer role for restricted accounts.

### Local MCP

LeafWiki runs a transparent per-project owner daemon for each canonical `(data-dir, root-dir)` pair. The first compatible startup starts the owner; later compatible server or STDIO startups attach to it. The owner tracks active session handles plus in-memory agent presence, and exits after both reach zero and `--daemon-idle-timeout` elapses. Agent presence expires on the same idle-timeout cadence.

The easiest project-local STDIO setup is the wrapper script:

```bash
./scripts/run.sh mcp --root-dir ./wiki --data-dir ./.wiki
./scripts/run.sh mcp --config ./leafwiki.yml
```

When using `run.sh mcp --config`, set `mcp: stdio` or `mcp: http,stdio` in the YAML file so the spawned process speaks MCP over stdin/stdout.

Native STDIO keeps stdout reserved for MCP JSON-RPC frames. It can run with disabled auth for isolated local workflows, or with a per-session MCP API key through `LEAFWIKI_MCP_API_KEY`. API keys are not stored in the daemon descriptor and do not affect daemon config matching.

LeafWiki can also expose a local-only MCP Streamable HTTP endpoint for agents and the web UI to work against the same live wiki state. It is disabled by default and only starts on a loopback host:

```bash
./leafwiki --mcp=http --host=127.0.0.1 --allow-insecure=true --jwt-secret=<secret> --admin-password=<password>
```

The endpoint is `http://127.0.0.1:8080/mcp`, or `${base-path}/mcp` when `--base-path` is set. Plain local HTTP requires `--allow-insecure=true` so login and OAuth cookies work without TLS. Authenticated MCP uses OAuth Authorization Code + PKCE with Dynamic Client Registration for OAuth-capable clients and scope `leafwiki:mcp`. The fixed public client ID `leafwiki-local-mcp` remains available for manual testing and backward compatibility. Authorization requires a logged-in web user and explicit local approval before tokens are issued. MCP-only API keys are also available for manual clients that can send `Authorization: Bearer lwk_<id>_<secret>`.

The legacy disabled-auth mode remains available for isolated local workflows:

```bash
./leafwiki --disable-auth --mcp=http --host=127.0.0.1
```

For clients that need STDIO with API-key auth, run native STDIO and provide `LEAFWIKI_MCP_API_KEY`. OAuth-capable clients should use Streamable HTTP directly.

See [Local MCP Interface](docs/mcp.md) for native STDIO setup, OAuth client settings, API-key behavior, the tool surface, safety gates, and the parity contract. See [Agent Collaboration](docs/agent-collaboration.md) for the context-first workflow, presence privacy, direct Markdown sync flow, validation, and partial-edit guidance. Agents that support repo-local skills should use the [LeafWiki agent skill](skills/llmwiki/SKILL.md) when editing or validating LeafWiki content.

### Operations notes

- Default bind: `127.0.0.1` (binary) / `0.0.0.0` (Docker image)
- Default data dir: `./data` (binary) / `/app/data` (container)
- Default root dir: `<data-dir>/root`
- Defaults are intentionally conservative — a fresh install does not become network-exposed by accident

### Workspace storage layout

LeafWiki separates app state from managed markdown content:

- `DataDir` stores app state: auth/session/search/link/tag/property SQLite files, assets, revisions, branding, and importer state.
- `RootDir` stores LeafWiki-managed markdown pages and `.order.json` files.
- `RootDir` is writable and managed by LeafWiki. It is not a passive arbitrary-folder viewer.
- `RootDir` must not contain `DataDir` and must not point inside app-state paths such as `assets`, `.leafwiki`, `.importer`, or `branding`.
- Changing `RootDir` does not migrate existing markdown. Move or copy content from the old `<data-dir>/root` before switching an existing install.
- Managed Markdown uses [canonical internal links](docs/canonical-markdown-links.md): page hrefs end in `.md`, section hrefs omit `.md`.
- `--markdown-link-root-prefix` only controls authored Markdown hrefs, such as `/docs/api.md`; it does not change LeafWiki route paths, API paths, MCP path inputs, or HTTP base paths.

### Root directory E2E smoke

Use the focused root-dir E2E smoke when changing startup, importer, page write, or storage-boundary behavior:

```bash
E2E_RUN_MODE=local E2E_ENABLE_SEPARATE_ROOT_DIR=1 ./e2e/run.sh --grep "Separate root dir"
```

---

## Keyboard Shortcuts

| Action                | Shortcut                               |
|-----------------------|----------------------------------------|
| Edit mode             | `Ctrl + E` / `Cmd + E`                 |
| Save                  | `Ctrl + S` / `Cmd + S`                 |
| Search                | `Ctrl + Shift + F` / `Cmd + Shift + F` |
| Navigation pane       | `Ctrl + Shift + E` / `Cmd + Shift + E` |
| Go to page            | `Ctrl + Alt + P` / `Cmd + Option + P`  |
| Bold                  | `Ctrl + B` / `Cmd + B`                 |
| Italic                | `Ctrl + I` / `Cmd + I`                 |
| Headline 1–3          | `Ctrl + Alt + 1–3` / `Cmd + Alt + 1–3` |

`Ctrl+V` / `Cmd+V` for pasting images and files works in the editor.  
`Esc` closes modals, dialogs, and edit mode.

---

## Support this project

If it's useful to you:

- ⭐ **[Star the repo](https://github.com/perber/leafwiki)** — helps others find it
- 💛 **[Sponsor on GitHub](https://github.com/sponsors/perber)** — supports ongoing maintenance, bug fixes, and new features

---

## Contributing

Contributions, discussions, and feedback are welcome.  
Open an issue or start a discussion on GitHub. Follow the repository to get notified about new releases.
