#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
source "$script_dir/run_messages.sh"

for removed_env in \
  LEAFWIKI_RUNTIME_STACK \
  LEAFWIKI_RUN_MCP_RUNTIME_STACK \
  LEAFWIKI_RUN_MCP_ENABLE_WORKSPACE_SYNC \
  LEAFWIKI_RUN_MCP_SERVER_LOG
do
  if [[ ${!removed_env+x} ]]; then
    printf '%s unknown environment variable: %s\n' "$LEAFWIKI_RUN_MSG_ERROR_PREFIX" "$removed_env" >&2
    exit 1
  fi
done

leafwiki_bin="${LEAFWIKI_RUN_MCP_LEAFWIKI_BIN:-${LEAFWIKI_BIN:-leafwiki}}"
scheme="${LEAFWIKI_RUN_MCP_SCHEME:-http}"
host="${LEAFWIKI_RUN_MCP_HOST:-${LEAFWIKI_HOST:-127.0.0.1}}"
port="${LEAFWIKI_RUN_MCP_PORT:-${LEAFWIKI_PORT:-8080}}"
base_path="${LEAFWIKI_RUN_MCP_BASE_PATH:-${LEAFWIKI_BASE_PATH:-}}"
data_dir="${LEAFWIKI_RUN_MCP_DATA_DIR:-${LEAFWIKI_DATA_DIR:-./.wiki}}"
root_dir="${LEAFWIKI_RUN_MCP_ROOT_DIR:-${LEAFWIKI_ROOT_DIR:-./wiki}}"
markdown_link_root_prefix="${LEAFWIKI_RUN_MCP_MARKDOWN_LINK_ROOT_PREFIX:-${LEAFWIKI_MARKDOWN_LINK_ROOT_PREFIX:-}}"
jwt_secret="${LEAFWIKI_RUN_MCP_JWT_SECRET:-${LEAFWIKI_JWT_SECRET:-}}"
admin_password="${LEAFWIKI_RUN_MCP_ADMIN_PASSWORD:-${LEAFWIKI_ADMIN_PASSWORD:-}}"
allow_insecure="${LEAFWIKI_RUN_MCP_ALLOW_INSECURE:-${LEAFWIKI_ALLOW_INSECURE:-1}}"
disable_auth="${LEAFWIKI_RUN_MCP_DISABLE_AUTH:-${LEAFWIKI_DISABLE_AUTH:-}}"
disable_request_log="${LEAFWIKI_RUN_MCP_DISABLE_REQUEST_LOG:-${LEAFWIKI_DISABLE_REQUEST_LOG:-1}}"
daemon_idle_timeout="${LEAFWIKI_RUN_MCP_DAEMON_IDLE_TIMEOUT:-${LEAFWIKI_DAEMON_IDLE_TIMEOUT:-10m}}"
api_key="${LEAFWIKI_RUN_MCP_API_KEY:-${LEAFWIKI_MCP_API_KEY:-}}"
dry_run=0
config_path=""
config_conflict=""
config_mode_requested=0

server_extra_args=()

usage() {
  cat <<EOF
${LEAFWIKI_RUN_MSG_USAGE}

${LEAFWIKI_RUN_MSG_HELP_BODY}
EOF
}

log() {
  printf '%s\n' "$1" >&2
}

fail_error() {
  printf '%s %s\n' "$LEAFWIKI_RUN_MSG_ERROR_PREFIX" "$1" >&2
  exit 1
}

fail() {
  if [[ "${mode:-}" == "agent-hook" ]]; then
    case "${hook_provider:-}" in
      codex|claude)
        printf '{}\n'
        ;;
      cursor)
        printf '{"permission":"allow"}\n'
        ;;
    esac
    exit 0
  fi
  fail_error "$1"
}

fail_config_argument_error() {
  if [[ "${config_mode_requested:-0}" == "1" || -n "${config_path:-}" ]]; then
    fail_error "$1"
  fi
  fail "$1"
}

error_with_detail() {
  printf '%s: %s\n' "$1" "$2"
}

option_requires() {
  printf '%s %s\n' "$1" "$2"
}

config_conflict_error() {
  option_requires "$LEAFWIKI_RUN_MSG_ERROR_CONFIG_CANNOT_COMBINE" "$1"
}

record_config_conflict() {
  if [[ -z "$config_conflict" ]]; then
    config_conflict="$1"
  fi
}

fail_missing_config_conflict_value() {
  local flag="$1"
  local message="$2"
  if [[ "${config_mode_requested:-0}" == "1" || -n "$config_path" ]]; then
    fail_error "$(config_conflict_error "$flag")"
  fi
  fail "$message"
}

detect_config_mode_requested() {
  local skip_next=0
  local arg
  for arg in "$@"; do
    if [[ "$skip_next" == "1" ]]; then
      skip_next=0
      continue
    fi
    case "$arg" in
      --config|--config=*)
        config_mode_requested=1
        ;;
    esac
    if [[ "$arg" != *=* ]] && wrapper_flag_takes_value "$arg"; then
      skip_next=1
    fi
  done
}

wrapper_flag_takes_value() {
  case "$1" in
    --leafwiki-bin|--scheme|--host|--port|--base-path|--data-dir|--root-dir|--markdown-link-root-prefix|--jwt-secret|--admin-password|--daemon-idle-timeout|--api-key|--config|--server-arg)
      return 0
      ;;
    *)
      return 1
      ;;
  esac
}

is_config_path_value() {
  local value="${1:-}"
  value="${value#"${value%%[![:space:]]*}"}"
  value="${value%"${value##*[![:space:]]}"}"
  [[ -n "$value" && "$value" != -* ]]
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
    [[ -x "$executable" ]] || fail "$(error_with_detail "$LEAFWIKI_RUN_MSG_ERROR_EXECUTABLE_NOT_FOUND" "$executable")"
  else
    command_exists "$executable" || fail "$(error_with_detail "$LEAFWIKI_RUN_MSG_ERROR_EXECUTABLE_NOT_FOUND_ON_PATH" "$executable")"
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

mode="${1:-}"
hook_provider=""
case "$mode" in
  mcp)
    shift
    detect_config_mode_requested "$@"
    ;;
  agent-hook)
    shift
    detect_config_mode_requested "$@"
    [[ $# -ge 1 ]] || fail "$LEAFWIKI_RUN_MSG_ERROR_AGENT_HOOK_REQUIRES_PROVIDER"
    hook_provider="$1"
    if [[ "$config_mode_requested" == "1" && "$hook_provider" == --* ]]; then
      fail_error "$LEAFWIKI_RUN_MSG_ERROR_AGENT_HOOK_REQUIRES_PROVIDER"
    fi
    shift
    ;;
  -h|--help|"")
    usage
    exit 0
    ;;
  *)
    fail "$LEAFWIKI_RUN_MSG_ERROR_RUN_MODE_REQUIRED"
    ;;
esac

while [[ $# -gt 0 ]]; do
  if [[ "$1" == --*" "* ]]; then
    fail_config_argument_error "$(error_with_detail "$LEAFWIKI_RUN_MSG_ERROR_ARGUMENT_CONTAINS_SPACE" "$1")"
  fi

  case "$1" in
    --leafwiki-bin=*)
      leafwiki_bin="${1#*=}"
      shift
      ;;
    --leafwiki-bin)
      [[ $# -ge 2 ]] || fail "$LEAFWIKI_RUN_MSG_ERROR_LEAFWIKI_BIN_REQUIRES_PATH"
      leafwiki_bin="$2"
      shift 2
      ;;
    --scheme=*)
      scheme="${1#*=}"
      record_config_conflict "--scheme"
      shift
      ;;
    --scheme)
      [[ $# -ge 2 ]] || fail_missing_config_conflict_value "--scheme" "$(option_requires "--scheme" "$LEAFWIKI_RUN_MSG_ERROR_OPTION_REQUIRES_VALUE")"
      scheme="$2"
      record_config_conflict "--scheme"
      shift 2
      ;;
    --host=*)
      host="${1#*=}"
      record_config_conflict "--host"
      shift
      ;;
    --host)
      [[ $# -ge 2 ]] || fail_missing_config_conflict_value "--host" "$(option_requires "--host" "$LEAFWIKI_RUN_MSG_ERROR_OPTION_REQUIRES_VALUE")"
      host="$2"
      record_config_conflict "--host"
      shift 2
      ;;
    --port=*)
      port="${1#*=}"
      record_config_conflict "--port"
      shift
      ;;
    --port)
      [[ $# -ge 2 ]] || fail_missing_config_conflict_value "--port" "$(option_requires "--port" "$LEAFWIKI_RUN_MSG_ERROR_OPTION_REQUIRES_VALUE")"
      port="$2"
      record_config_conflict "--port"
      shift 2
      ;;
    --base-path=*)
      base_path="${1#*=}"
      record_config_conflict "--base-path"
      shift
      ;;
    --base-path)
      [[ $# -ge 2 ]] || fail_missing_config_conflict_value "--base-path" "$(option_requires "--base-path" "$LEAFWIKI_RUN_MSG_ERROR_OPTION_REQUIRES_PATH")"
      base_path="$2"
      record_config_conflict "--base-path"
      shift 2
      ;;
    --data-dir=*)
      data_dir="${1#*=}"
      record_config_conflict "--data-dir"
      shift
      ;;
    --data-dir)
      [[ $# -ge 2 ]] || fail_missing_config_conflict_value "--data-dir" "$(option_requires "--data-dir" "$LEAFWIKI_RUN_MSG_ERROR_OPTION_REQUIRES_PATH")"
      data_dir="$2"
      record_config_conflict "--data-dir"
      shift 2
      ;;
    --root-dir=*)
      root_dir="${1#*=}"
      record_config_conflict "--root-dir"
      shift
      ;;
    --root-dir)
      [[ $# -ge 2 ]] || fail_missing_config_conflict_value "--root-dir" "$(option_requires "--root-dir" "$LEAFWIKI_RUN_MSG_ERROR_OPTION_REQUIRES_PATH")"
      root_dir="$2"
      record_config_conflict "--root-dir"
      shift 2
      ;;
    --markdown-link-root-prefix=*)
      markdown_link_root_prefix="${1#*=}"
      record_config_conflict "--markdown-link-root-prefix"
      shift
      ;;
    --markdown-link-root-prefix)
      [[ $# -ge 2 ]] || fail_missing_config_conflict_value "--markdown-link-root-prefix" "$(option_requires "--markdown-link-root-prefix" "$LEAFWIKI_RUN_MSG_ERROR_OPTION_REQUIRES_PATH")"
      markdown_link_root_prefix="$2"
      record_config_conflict "--markdown-link-root-prefix"
      shift 2
      ;;
    --jwt-secret=*)
      jwt_secret="${1#*=}"
      record_config_conflict "--jwt-secret"
      shift
      ;;
    --jwt-secret)
      [[ $# -ge 2 ]] || fail_missing_config_conflict_value "--jwt-secret" "$(option_requires "--jwt-secret" "$LEAFWIKI_RUN_MSG_ERROR_OPTION_REQUIRES_SECRET")"
      jwt_secret="$2"
      record_config_conflict "--jwt-secret"
      shift 2
      ;;
    --admin-password=*)
      admin_password="${1#*=}"
      record_config_conflict "--admin-password"
      shift
      ;;
    --admin-password)
      [[ $# -ge 2 ]] || fail_missing_config_conflict_value "--admin-password" "$(option_requires "--admin-password" "$LEAFWIKI_RUN_MSG_ERROR_OPTION_REQUIRES_PASSWORD")"
      admin_password="$2"
      record_config_conflict "--admin-password"
      shift 2
      ;;
    --disable-auth)
      disable_auth=1
      record_config_conflict "--disable-auth"
      shift
      ;;
    --allow-insecure)
      allow_insecure=1
      record_config_conflict "--allow-insecure"
      shift
      ;;
    --no-allow-insecure)
      allow_insecure=0
      record_config_conflict "--no-allow-insecure"
      shift
      ;;
    --request-log)
      disable_request_log=0
      record_config_conflict "--request-log"
      shift
      ;;
    --disable-request-log)
      disable_request_log=1
      record_config_conflict "--disable-request-log"
      shift
      ;;
    --daemon-idle-timeout=*)
      daemon_idle_timeout="${1#*=}"
      record_config_conflict "--daemon-idle-timeout"
      shift
      ;;
    --daemon-idle-timeout)
      [[ $# -ge 2 ]] || fail_missing_config_conflict_value "--daemon-idle-timeout" "$(option_requires "--daemon-idle-timeout" "$LEAFWIKI_RUN_MSG_ERROR_OPTION_REQUIRES_DURATION")"
      daemon_idle_timeout="$2"
      record_config_conflict "--daemon-idle-timeout"
      shift 2
      ;;
    --api-key=*)
      api_key="${1#*=}"
      record_config_conflict "--api-key"
      shift
      ;;
    --api-key)
      [[ $# -ge 2 ]] || fail_missing_config_conflict_value "--api-key" "$(option_requires "--api-key" "$LEAFWIKI_RUN_MSG_ERROR_OPTION_REQUIRES_VALUE")"
      api_key="$2"
      record_config_conflict "--api-key"
      shift 2
      ;;
    --config=*)
      config_path="${1#*=}"
      is_config_path_value "$config_path" || fail_config_argument_error "$LEAFWIKI_RUN_MSG_ERROR_CONFIG_REQUIRES_PATH"
      shift
      ;;
    --config)
      [[ $# -ge 2 ]] && is_config_path_value "$2" || fail_config_argument_error "$LEAFWIKI_RUN_MSG_ERROR_CONFIG_REQUIRES_PATH"
      config_path="$2"
      shift 2
      ;;
    --server-arg=*)
      server_extra_args+=("${1#*=}")
      record_config_conflict "--server-arg"
      shift
      ;;
    --server-arg)
      [[ $# -ge 2 ]] || fail_missing_config_conflict_value "--server-arg" "$(option_requires "--server-arg" "$LEAFWIKI_RUN_MSG_ERROR_OPTION_REQUIRES_ARGUMENT")"
      server_extra_args+=("$2")
      record_config_conflict "--server-arg"
      shift 2
      ;;
    --dry-run)
      dry_run=1
      shift
      ;;
    -h|--help)
      if [[ "$config_mode_requested" == "1" || -n "$config_path" ]]; then
        fail_error "$(config_conflict_error "--help")"
      fi
      usage
      exit 0
      ;;
    *)
      fail_config_argument_error "$(error_with_detail "$LEAFWIKI_RUN_MSG_ERROR_UNKNOWN_OPTION" "$1")"
      ;;
  esac
done

if [[ "$config_mode_requested" == "1" && -n "$config_conflict" ]]; then
  fail_error "$(config_conflict_error "$config_conflict")"
fi

base_path="$(normalize_base_path "$base_path")"
url_host_value="$(url_host "$host")"
http_url="$scheme://$url_host_value:$port$base_path"

auth_bootstrap_configured=0
if [[ -z "$config_path" ]]; then
  if [[ -n "$jwt_secret" || -n "$admin_password" ]]; then
    auth_bootstrap_configured=1
  fi

  if [[ -z "$disable_auth" ]]; then
    if [[ -n "$api_key" ]]; then
      disable_auth=0
    elif [[ "$mode" == "agent-hook" && "$auth_bootstrap_configured" -eq 1 ]]; then
      disable_auth=0
    else
      disable_auth=1
    fi
  fi
  if truthy "$disable_auth" && [[ -n "$api_key" ]]; then
    fail "$LEAFWIKI_RUN_MSG_ERROR_DISABLE_AUTH_API_KEY_CONFLICT"
  fi
fi

leafwiki_cmd=(
  "$leafwiki_bin"
)
if [[ -n "$config_path" ]]; then
  leafwiki_cmd+=(--config "$config_path")
  if [[ "$mode" == "agent-hook" ]]; then
    leafwiki_cmd+=(agent-hook "$hook_provider")
  fi
elif [[ "$mode" == "mcp" ]]; then
  leafwiki_cmd+=(--mcp=stdio)
else
  leafwiki_cmd+=(agent-hook "$hook_provider")
fi
if [[ -z "$config_path" ]]; then
  leafwiki_cmd+=(
    --host "$host"
    --port "$port"
    --data-dir "$data_dir"
    --root-dir "$root_dir"
    --daemon-idle-timeout "$daemon_idle_timeout"
    --log-target file
  )
  if truthy "$disable_auth"; then
    leafwiki_cmd+=(--disable-auth=true)
  fi
  if truthy "$allow_insecure"; then
    leafwiki_cmd+=(--allow-insecure)
  fi
  if [[ -n "$base_path" ]]; then
    leafwiki_cmd+=(--base-path "$base_path")
  fi
  if [[ -n "$markdown_link_root_prefix" ]]; then
    leafwiki_cmd+=(--markdown-link-root-prefix "$markdown_link_root_prefix")
  fi
  if truthy "$disable_request_log"; then
    leafwiki_cmd+=(--disable-request-log)
  fi
  if [[ "${#server_extra_args[@]}" -gt 0 ]]; then
    leafwiki_cmd+=("${server_extra_args[@]}")
  fi
fi

child_env=()
print_env=()
if [[ -z "$config_path" && -n "$api_key" ]]; then
  child_env+=(LEAFWIKI_MCP_API_KEY="$api_key")
  print_env+=(LEAFWIKI_MCP_API_KEY=REDACTED)
fi
if [[ -z "$config_path" && -n "$jwt_secret" ]]; then
  child_env+=(LEAFWIKI_JWT_SECRET="$jwt_secret")
  print_env+=(LEAFWIKI_JWT_SECRET=REDACTED)
fi
if [[ -z "$config_path" && -n "$admin_password" ]]; then
  child_env+=(LEAFWIKI_ADMIN_PASSWORD="$admin_password")
  print_env+=(LEAFWIKI_ADMIN_PASSWORD=REDACTED)
fi

if [[ "$dry_run" -eq 1 ]]; then
  if [[ "$mode" == "mcp" ]]; then
    if [[ -n "$config_path" ]]; then
      log "$LEAFWIKI_RUN_MSG_DRY_RUN_MCP_CONFIG"
    else
      log "$LEAFWIKI_RUN_MSG_DRY_RUN_MCP_NATIVE"
      log "$LEAFWIKI_RUN_MSG_DRY_RUN_STDIO_ATTACH"
    fi
  else
    log "$LEAFWIKI_RUN_MSG_DRY_RUN_AGENT_HOOK"
  fi
  if [[ -n "$config_path" ]]; then
    log "$LEAFWIKI_RUN_MSG_DRY_RUN_HTTP_CONFIG $config_path"
  else
    log "$LEAFWIKI_RUN_MSG_DRY_RUN_HTTP_URL $http_url"
  fi
  if [[ "${#print_env[@]}" -gt 0 ]]; then
    print_command_with_env "${#print_env[@]}" "${print_env[@]}" "${leafwiki_cmd[@]}"
  else
    print_command_with_env 0 "${leafwiki_cmd[@]}"
  fi
  exit 0
fi

require_executable "$leafwiki_bin"
if [[ "${#child_env[@]}" -gt 0 ]]; then
  exec env "${child_env[@]}" "${leafwiki_cmd[@]}"
fi
exec "${leafwiki_cmd[@]}"
