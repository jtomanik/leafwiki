<!-- leafwiki
version: 1
page:
  id: wECdhV-DR
  title: Logging
  created_at: "2026-06-05T16:05:51.107412964Z"
  updated_at: "2026-06-05T16:05:51.107412964Z"
  creator_id: system
  last_author_id: system
-->

# Logging

LeafWiki writes structured JSON logs. Normal server logs do not go to stdout
unless `stdout` is explicitly selected.

## Defaults

The default target is `file`:

```bash
leafwiki --data-dir ./data
```

This writes to:

```text
./data/.leafwiki/logs/leafwiki.log
```

The log directory is created automatically. New log files are created with
`0600` permissions and opened in append mode.

## Targets

Use `--log-target` or `LEAFWIKI_LOG_TARGET`:

| Target | Behavior |
|---|---|
| `file` | Write JSON logs to `--log-file` or the default data-dir log file |
| `stderr` | Write JSON logs to stderr |
| `stdout` | Write JSON logs to stdout |

`--log-file` and `LEAFWIKI_LOG_FILE` are valid only when the target is `file`.
Relative log file paths resolve under `--data-dir`; absolute paths are used as
provided.

Examples:

```bash
leafwiki --log-target stderr
leafwiki --log-file logs/leafwiki.log
leafwiki --log-file /var/log/leafwiki/leafwiki.log
```

Use `stderr` for Docker, systemd, or other supervisors that already collect
process stderr. Use `stdout` only when the caller explicitly expects logs on
stdout; MCP STDIO wrappers and other protocol transports should not use stdout
for server diagnostics.

## Levels

Set `LEAFWIKI_LOG_LEVEL` to one of:

```text
debug
info
warn
error
```

The default level is `info`.

## Rotation

LeafWiki does not implement in-process log rotation. Use external rotation from
your runtime or host:

- Docker or another container runtime can collect `stderr`.
- systemd can collect `stderr` through journald.
- File logging can be rotated with `logrotate` or an equivalent host tool.

For supervised deployments, prefer:

```bash
leafwiki --log-target stderr
```
