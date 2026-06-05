#!/usr/bin/env bash
set -euo pipefail

leafwiki_bin="${LEAFWIKI_RUN_MCP_LEAFWIKI_BIN:-${LEAFWIKI_BIN:-leafwiki}}"
scheme="${LEAFWIKI_RUN_MCP_SCHEME:-http}"
host="${LEAFWIKI_RUN_MCP_HOST:-${LEAFWIKI_HOST:-127.0.0.1}}"
port="${LEAFWIKI_RUN_MCP_PORT:-${LEAFWIKI_PORT:-8080}}"
base_path="${LEAFWIKI_RUN_MCP_BASE_PATH:-${LEAFWIKI_BASE_PATH:-}}"
data_dir="${LEAFWIKI_RUN_MCP_DATA_DIR:-${LEAFWIKI_DATA_DIR:-./.wiki}}"
root_dir="${LEAFWIKI_RUN_MCP_ROOT_DIR:-${LEAFWIKI_ROOT_DIR:-./wiki}}"
jwt_secret="${LEAFWIKI_RUN_MCP_JWT_SECRET:-${LEAFWIKI_JWT_SECRET:-}}"
admin_password="${LEAFWIKI_RUN_MCP_ADMIN_PASSWORD:-${LEAFWIKI_ADMIN_PASSWORD:-}}"
allow_insecure="${LEAFWIKI_RUN_MCP_ALLOW_INSECURE:-${LEAFWIKI_ALLOW_INSECURE:-1}}"
disable_auth="${LEAFWIKI_RUN_MCP_DISABLE_AUTH:-${LEAFWIKI_DISABLE_AUTH:-}}"
disable_request_log="${LEAFWIKI_RUN_MCP_DISABLE_REQUEST_LOG:-${LEAFWIKI_DISABLE_REQUEST_LOG:-1}}"
daemon_idle_timeout="${LEAFWIKI_RUN_MCP_DAEMON_IDLE_TIMEOUT:-${LEAFWIKI_DAEMON_IDLE_TIMEOUT:-10m}}"
api_key="${LEAFWIKI_RUN_MCP_API_KEY:-${LEAFWIKI_MCP_API_KEY:-}}"
server_log="${LEAFWIKI_RUN_MCP_SERVER_LOG:-}"
dry_run=0

server_extra_args=()

usage() {
  cat <<EOF
Usage: scripts/run-mcp.sh [options]

Starts one native LeafWiki MCP STDIO frontend. LeafWiki keeps one project
daemon owner for the HTTP UI; stdout from this frontend is reserved for MCP
JSON-RPC.

Options:
  --leafwiki-bin <path>     LeafWiki executable (default: leafwiki)
  --scheme <scheme>         Informational URL scheme for dry-run output (default: http)
  --host <host>             LeafWiki bind host (default: 127.0.0.1)
  --port <port>             LeafWiki port (default: 8080)
  --base-path <path>        LeafWiki base path, if any
  --data-dir <path>         LeafWiki data directory (default: ./.wiki)
  --root-dir <path>         LeafWiki root markdown directory (default: ./wiki)
  --jwt-secret <secret>     JWT secret only when this run must bootstrap an auth-enabled owner
  --admin-password <pass>   Admin password only when this run must bootstrap an auth-enabled owner
  --disable-auth            Force disabled-auth STDIO identity
  --allow-insecure          Pass --allow-insecure to LeafWiki (default)
  --no-allow-insecure       Do not pass --allow-insecure
  --request-log             Keep LeafWiki request logs enabled
  --disable-request-log     Pass --disable-request-log to LeafWiki (default)
  --daemon-idle-timeout <d> Project daemon idle timeout after last session exits (default: 10m)
  --api-key <key>           Native STDIO API key; passed as LEAFWIKI_MCP_API_KEY
  --server-log <path>       Accepted and ignored for compatibility; use --server-arg for logging overrides
  --server-arg <arg>        Extra argument passed to leafwiki; repeatable
  --dry-run                 Print the planned command without starting anything
  -h, --help                Show this help

Some removed wrapper options are accepted and ignored for compatibility.

Environment overrides use LEAFWIKI_RUN_MCP_* names matching the option names.
Use LEAFWIKI_RUN_MCP_API_KEY or LEAFWIKI_MCP_API_KEY to provide the native
STDIO API key without putting the secret in the child command line.
EOF
}

log() {
  printf '%s\n' "$1" >&2
}

fail() {
  printf 'Error: %s\n' "$1" >&2
  exit 1
}

quote_command() {
  local arg
  for arg in "$@"; do
    printf '%q ' "$arg"
  done
}

print_command() {
  printf '+ ' >&2
  quote_command "$@" >&2
  printf '\n' >&2
}

print_command_with_env() {
  local env_count="$1"
  shift
  local env_args=()
  local i
  if [[ "$env_count" -gt 0 ]]; then
    for ((i = 0; i < env_count; i++)); do
      env_args+=("$1")
      shift
    done
    print_command env "${env_args[@]}" "$@"
    return
  fi
  print_command "$@"
}

truthy() {
  case "${1:-}" in
    1|true|TRUE|yes|YES|on|ON)
      return 0
      ;;
    *)
      return 1
      ;;
  esac
}

command_exists() {
  command -v "$1" >/dev/null 2>&1
}

require_executable() {
  local executable="$1"
  if [[ "$executable" == */* ]]; then
    [[ -x "$executable" ]] || fail "executable not found or not executable: $executable"
  else
    command_exists "$executable" || fail "executable not found on PATH: $executable"
  fi
}

normalize_base_path() {
  local path="$1"
  if [[ -z "$path" || "$path" == "/" ]]; then
    printf '\n'
    return
  fi
  path="/${path#/}"
  path="${path%/}"
  printf '%s\n' "$path"
}

url_host() {
  local value="$1"
  if [[ "$value" == *:* && "$value" != \[*\] ]]; then
    printf '[%s]\n' "$value"
    return
  fi
  printf '%s\n' "$value"
}

while [[ $# -gt 0 ]]; do
  if [[ "$1" == --*" "* ]]; then
    fail "argument '$1' contains a space; MCP JSON args must split flags and values into separate args, for example \"--root-dir\", \"./wiki\", or use --root-dir=./wiki"
  fi

  case "$1" in
    --mode=*|--endpoint=*|--health-url=*|--mcp-stdio-bin=*|--request-timeout=*|--shutdown-timeout=*|--max-frame-size=*|--stdio-arg=*)
      shift
      ;;
    --mode|--endpoint|--health-url|--mcp-stdio-bin|--request-timeout|--shutdown-timeout|--max-frame-size|--stdio-arg)
      [[ $# -ge 2 ]] || fail "$1 requires a value"
      shift 2
      ;;
    --leafwiki-bin=*)
      leafwiki_bin="${1#*=}"
      shift
      ;;
    --leafwiki-bin)
      [[ $# -ge 2 ]] || fail "--leafwiki-bin requires a path"
      leafwiki_bin="$2"
      shift 2
      ;;
    --scheme=*)
      scheme="${1#*=}"
      shift
      ;;
    --scheme)
      [[ $# -ge 2 ]] || fail "--scheme requires a value"
      scheme="$2"
      shift 2
      ;;
    --host=*)
      host="${1#*=}"
      shift
      ;;
    --host)
      [[ $# -ge 2 ]] || fail "--host requires a value"
      host="$2"
      shift 2
      ;;
    --port=*)
      port="${1#*=}"
      shift
      ;;
    --port)
      [[ $# -ge 2 ]] || fail "--port requires a value"
      port="$2"
      shift 2
      ;;
    --base-path=*)
      base_path="${1#*=}"
      shift
      ;;
    --base-path)
      [[ $# -ge 2 ]] || fail "--base-path requires a path"
      base_path="$2"
      shift 2
      ;;
    --data-dir=*)
      data_dir="${1#*=}"
      shift
      ;;
    --data-dir)
      [[ $# -ge 2 ]] || fail "--data-dir requires a path"
      data_dir="$2"
      shift 2
      ;;
    --root-dir=*)
      root_dir="${1#*=}"
      shift
      ;;
    --root-dir)
      [[ $# -ge 2 ]] || fail "--root-dir requires a path"
      root_dir="$2"
      shift 2
      ;;
    --jwt-secret=*)
      jwt_secret="${1#*=}"
      shift
      ;;
    --jwt-secret)
      [[ $# -ge 2 ]] || fail "--jwt-secret requires a secret"
      jwt_secret="$2"
      shift 2
      ;;
    --admin-password=*)
      admin_password="${1#*=}"
      shift
      ;;
    --admin-password)
      [[ $# -ge 2 ]] || fail "--admin-password requires a password"
      admin_password="$2"
      shift 2
      ;;
    --disable-auth)
      disable_auth=1
      shift
      ;;
    --allow-insecure)
      allow_insecure=1
      shift
      ;;
    --no-allow-insecure)
      allow_insecure=0
      shift
      ;;
    --request-log)
      disable_request_log=0
      shift
      ;;
    --disable-request-log)
      disable_request_log=1
      shift
      ;;
    --daemon-idle-timeout=*)
      daemon_idle_timeout="${1#*=}"
      shift
      ;;
    --daemon-idle-timeout)
      [[ $# -ge 2 ]] || fail "--daemon-idle-timeout requires a duration"
      daemon_idle_timeout="$2"
      shift 2
      ;;
    --api-key=*)
      api_key="${1#*=}"
      shift
      ;;
    --api-key)
      [[ $# -ge 2 ]] || fail "--api-key requires a value"
      api_key="$2"
      shift 2
      ;;
    --server-log=*)
      server_log="${1#*=}"
      shift
      ;;
    --server-log)
      [[ $# -ge 2 ]] || fail "--server-log requires a path"
      server_log="$2"
      shift 2
      ;;
    --server-arg=*)
      server_extra_args+=("${1#*=}")
      shift
      ;;
    --server-arg)
      [[ $# -ge 2 ]] || fail "--server-arg requires an argument"
      server_extra_args+=("$2")
      shift 2
      ;;
    --dry-run)
      dry_run=1
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      fail "unknown option: $1"
      ;;
  esac
done

base_path="$(normalize_base_path "$base_path")"
url_host_value="$(url_host "$host")"
http_url="$scheme://$url_host_value:$port$base_path"

if [[ -z "$disable_auth" ]]; then
  if [[ -n "$api_key" ]]; then
    disable_auth=0
  else
    disable_auth=1
  fi
fi
if truthy "$disable_auth" && [[ -n "$api_key" ]]; then
  fail "--disable-auth cannot be combined with --api-key or LEAFWIKI_MCP_API_KEY"
fi

native_cmd=(
  "$leafwiki_bin"
  --mcp=stdio
  --host "$host"
  --port "$port"
  --data-dir "$data_dir"
  --root-dir "$root_dir"
  --daemon-idle-timeout "$daemon_idle_timeout"
  --log-target file
)
if truthy "$disable_auth"; then
  native_cmd+=(--disable-auth=true)
fi
if truthy "$allow_insecure"; then
  native_cmd+=(--allow-insecure)
fi
if [[ -n "$base_path" ]]; then
  native_cmd+=(--base-path "$base_path")
fi
if truthy "$disable_request_log"; then
  native_cmd+=(--disable-request-log)
fi
if [[ "${#server_extra_args[@]}" -gt 0 ]]; then
  native_cmd+=("${server_extra_args[@]}")
fi

child_env=()
print_env=()
if [[ -n "$api_key" ]]; then
  child_env+=(LEAFWIKI_MCP_API_KEY="$api_key")
  print_env+=(LEAFWIKI_MCP_API_KEY=REDACTED)
  if [[ -n "$jwt_secret" ]]; then
    child_env+=(LEAFWIKI_JWT_SECRET="$jwt_secret")
    print_env+=(LEAFWIKI_JWT_SECRET=REDACTED)
  fi
  if [[ -n "$admin_password" ]]; then
    child_env+=(LEAFWIKI_ADMIN_PASSWORD="$admin_password")
    print_env+=(LEAFWIKI_ADMIN_PASSWORD=REDACTED)
  fi
fi

if [[ "$dry_run" -eq 1 ]]; then
  log "Would run LeafWiki native MCP STDIO"
  log "HTTP UI: $http_url"
  if [[ -n "$server_log" ]]; then
    log "Server log option ignored in native-only wrapper: $server_log"
  fi
  if [[ "${#print_env[@]}" -gt 0 ]]; then
    print_command_with_env "${#print_env[@]}" "${print_env[@]}" "${native_cmd[@]}"
  else
    print_command_with_env 0 "${native_cmd[@]}"
  fi
  exit 0
fi

require_executable "$leafwiki_bin"
if [[ "${#child_env[@]}" -gt 0 ]]; then
  exec env "${child_env[@]}" "${native_cmd[@]}"
fi
exec "${native_cmd[@]}"
