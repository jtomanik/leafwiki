#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd -- "$script_dir/.." && pwd)"
script="$repo_root/scripts/run.sh"
messages="$repo_root/scripts/run_messages.sh"
old_script="$repo_root/scripts/run-""mcp.sh"

fail() {
  printf 'FAIL: %s\n' "$1" >&2
  exit 1
}

assert_contains() {
  local haystack="$1"
  local needle="$2"
  local label="$3"
  [[ "$haystack" == *"$needle"* ]] || fail "$label missing '$needle': $haystack"
}

assert_not_contains() {
  local haystack="$1"
  local needle="$2"
  local label="$3"
  [[ "$haystack" != *"$needle"* ]] || fail "$label unexpectedly contained '$needle': $haystack"
}

[[ -f "$script" ]] || fail "missing scripts/run.sh"
[[ -f "$messages" ]] || fail "missing scripts/run_messages.sh"
[[ ! -e "$old_script" ]] || fail "old wrapper compatibility shim must not exist"

bash -n "$script"
bash -n "$messages"
source "$messages"

[[ -n "${LEAFWIKI_RUN_MSG_HELP_BODY:-}" ]] || fail "missing generated shell help body message"
[[ -n "${LEAFWIKI_RUN_MSG_ERROR_PREFIX:-}" ]] || fail "missing generated shell error prefix message"
[[ -n "${LEAFWIKI_RUN_MSG_DRY_RUN_MCP_CONFIG:-}" ]] || fail "missing generated shell config dry-run message"
[[ -n "${LEAFWIKI_RUN_MSG_DRY_RUN_MCP_NATIVE:-}" ]] || fail "missing generated shell native dry-run message"
[[ -n "${LEAFWIKI_RUN_MSG_DRY_RUN_STDIO_ATTACH:-}" ]] || fail "missing generated shell stdio attach message"
[[ -n "${LEAFWIKI_RUN_MSG_DRY_RUN_AGENT_HOOK:-}" ]] || fail "missing generated shell agent-hook dry-run message"
[[ -n "${LEAFWIKI_RUN_MSG_DRY_RUN_HTTP_CONFIG:-}" ]] || fail "missing generated shell config HTTP message"
[[ -n "${LEAFWIKI_RUN_MSG_DRY_RUN_HTTP_URL:-}" ]] || fail "missing generated shell URL HTTP message"
[[ -n "${LEAFWIKI_RUN_MSG_ERROR_UNKNOWN_OPTION:-}" ]] || fail "missing generated shell unknown-option error message"

help_output="$("$script" --help)"
assert_contains "$help_output" "Usage: scripts/run.sh <mcp|agent-hook> [options]" "help"
assert_contains "$help_output" "$LEAFWIKI_RUN_MSG_HELP_BODY" "help"
assert_contains "$help_output" "mcp" "help"
assert_contains "$help_output" "agent-hook" "help"
assert_contains "$help_output" "--leafwiki-bin" "help"
assert_contains "$help_output" "--markdown-link-root-prefix" "help"
assert_contains "$help_output" "--api-key" "help"
assert_contains "$help_output" "--daemon-idle-timeout" "help"
assert_contains "$help_output" "Federated runtime idle timeout" "help"
assert_contains "$help_output" "--server-arg" "help"
assert_contains "$help_output" "--config" "help"
assert_contains "$help_output" "--dry-run" "help"
removed_binary="leafwiki""-mcp-stdio"
legacy_bin_flag="--mcp""-stdio-bin"
assert_not_contains "$help_output" "$removed_binary" "help"
assert_not_contains "$help_output" "--enable-workspace-sync" "help"
assert_not_contains "$help_output" "--disable-workspace-sync" "help"
assert_not_contains "$help_output" "$legacy_bin_flag" "help"
assert_not_contains "$help_output" "--server-log" "help"

tmp_dir="$(mktemp -d)"
cleanup() {
  rm -rf "$tmp_dir"
}
trap cleanup EXIT

native_default_output="$("$script" mcp --dry-run --leafwiki-bin /tmp/fake-leafwiki 2>&1)"
assert_contains "$native_default_output" "Would run LeafWiki native MCP STDIO" "native dry-run"
assert_contains "$native_default_output" "descriptor-first attach via <data-dir>/.leafwiki/project-daemon.json" "native dry-run"
assert_contains "$native_default_output" "wikid ensure/control for missing or stale descriptors" "native dry-run"
assert_contains "$native_default_output" "/tmp/fake-leafwiki" "native dry-run"
assert_contains "$native_default_output" "--mcp=stdio" "native dry-run"
assert_contains "$native_default_output" "--disable-auth=true" "native dry-run"
assert_contains "$native_default_output" "--data-dir ./.wiki" "native dry-run"
assert_contains "$native_default_output" "--root-dir ./wiki" "native dry-run"
assert_contains "$native_default_output" "--daemon-idle-timeout 10m" "native dry-run"
assert_contains "$native_default_output" "--log-target file" "native dry-run"
assert_not_contains "$native_default_output" "--enable-workspace-sync" "native dry-run"
assert_not_contains "$native_default_output" "--log-target stderr" "native dry-run"
assert_not_contains "$native_default_output" "$removed_binary" "native dry-run"

native_prefix_output="$("$script" mcp --dry-run --leafwiki-bin /tmp/fake-leafwiki --markdown-link-root-prefix /docs 2>&1)"
assert_contains "$native_prefix_output" "--markdown-link-root-prefix /docs" "native prefix dry-run"

native_env_prefix_output="$(
  LEAFWIKI_RUN_MCP_MARKDOWN_LINK_ROOT_PREFIX=/docs \
  "$script" mcp --dry-run --leafwiki-bin /tmp/fake-leafwiki 2>&1
)"
assert_contains "$native_env_prefix_output" "--markdown-link-root-prefix /docs" "native env prefix dry-run"

config_output="$("$script" mcp --dry-run --leafwiki-bin /tmp/fake-leafwiki --config ./leafwiki.yml 2>&1)"
assert_contains "$config_output" "Would run LeafWiki with YAML config for MCP" "config dry-run"
assert_contains "$config_output" "/tmp/fake-leafwiki" "config dry-run"
assert_contains "$config_output" "--config ./leafwiki.yml" "config dry-run"
assert_not_contains "$config_output" "--mcp=stdio" "config dry-run"
assert_not_contains "$config_output" "--data-dir" "config dry-run"
assert_not_contains "$config_output" "--root-dir" "config dry-run"
assert_not_contains "$config_output" "--disable-auth=true" "config dry-run"
assert_not_contains "$config_output" "--enable-workspace-sync" "config dry-run"

config_api_key_output="$(
  LEAFWIKI_RUN_MCP_API_KEY=lwk_config_secret \
  "$script" mcp --dry-run --leafwiki-bin /tmp/fake-leafwiki --config ./leafwiki.yml 2>&1
)"
assert_contains "$config_api_key_output" "--config ./leafwiki.yml" "config api-key dry-run"
assert_not_contains "$config_api_key_output" "LEAFWIKI_MCP_API_KEY" "config api-key dry-run"
assert_not_contains "$config_api_key_output" "lwk_config_secret" "config api-key dry-run"

hook_config_output="$("$script" agent-hook codex --dry-run --leafwiki-bin /tmp/fake-leafwiki --config ./leafwiki.yml 2>&1)"
assert_contains "$hook_config_output" "Would run LeafWiki agent hook" "hook config dry-run"
assert_contains "$hook_config_output" "--config ./leafwiki.yml agent-hook codex" "hook config dry-run"
assert_not_contains "$hook_config_output" "--data-dir" "hook config dry-run"
assert_not_contains "$hook_config_output" "--root-dir" "hook config dry-run"

set +e
bad_config_mix_output="$("$script" mcp --dry-run --config ./leafwiki.yml --root-dir ./wiki 2>&1)"
bad_config_mix_status=$?
set -e
[[ "$bad_config_mix_status" -ne 0 ]] || fail "config mixed with root-dir unexpectedly succeeded"
assert_contains "$bad_config_mix_output" "--config cannot be combined with --root-dir" "config root-dir mix error"

set +e
bad_config_prefix_mix_output="$("$script" mcp --dry-run --config ./leafwiki.yml --markdown-link-root-prefix /docs 2>&1)"
bad_config_prefix_mix_status=$?
set -e
[[ "$bad_config_prefix_mix_status" -ne 0 ]] || fail "config mixed with markdown-link-root-prefix unexpectedly succeeded"
assert_contains "$bad_config_prefix_mix_output" "--config cannot be combined with --markdown-link-root-prefix" "config markdown-link-root-prefix mix error"

set +e
bad_hook_config_mix_output="$("$script" agent-hook codex --dry-run --config ./leafwiki.yml --root-dir ./wiki 2>&1)"
bad_hook_config_mix_status=$?
set -e
[[ "$bad_hook_config_mix_status" -ne 0 ]] || fail "hook config mixed with root-dir unexpectedly succeeded"
assert_contains "$bad_hook_config_mix_output" "--config cannot be combined with --root-dir" "hook config root-dir mix error"

set +e
bad_hook_config_mix_runtime_output="$(printf '%s' '{"hook_event_name":"SessionStart","session_id":"wrapper-config-conflict-secret"}' | "$script" agent-hook codex --config ./leafwiki.yml --root-dir ./wiki 2>&1)"
bad_hook_config_mix_runtime_status=$?
set -e
[[ "$bad_hook_config_mix_runtime_status" -ne 0 ]] || fail "runtime hook config mixed with root-dir unexpectedly succeeded"
assert_contains "$bad_hook_config_mix_runtime_output" "--config cannot be combined with --root-dir" "runtime hook config root-dir mix error"
assert_not_contains "$bad_hook_config_mix_runtime_output" "wrapper-config-conflict-secret" "runtime hook config root-dir mix error"

set +e
bad_hook_config_missing_value_output="$("$script" agent-hook codex --dry-run --config ./leafwiki.yml --root-dir 2>&1)"
bad_hook_config_missing_value_status=$?
set -e
[[ "$bad_hook_config_missing_value_status" -ne 0 ]] || fail "hook config mixed with root-dir missing value unexpectedly succeeded"
assert_contains "$bad_hook_config_missing_value_output" "--config cannot be combined with --root-dir" "hook config root-dir missing value error"

set +e
bad_hook_config_missing_value_runtime_output="$(printf '%s' '{"hook_event_name":"SessionStart","session_id":"wrapper-config-missing-value-secret"}' | "$script" agent-hook codex --config ./leafwiki.yml --root-dir 2>&1)"
bad_hook_config_missing_value_runtime_status=$?
set -e
[[ "$bad_hook_config_missing_value_runtime_status" -ne 0 ]] || fail "runtime hook config mixed with root-dir missing value unexpectedly succeeded"
assert_contains "$bad_hook_config_missing_value_runtime_output" "--config cannot be combined with --root-dir" "runtime hook config root-dir missing value error"
assert_not_contains "$bad_hook_config_missing_value_runtime_output" "wrapper-config-missing-value-secret" "runtime hook config root-dir missing value error"

set +e
bad_hook_config_unknown_option_output="$("$script" agent-hook codex --dry-run --config ./leafwiki.yml --not-a-real-flag 2>&1)"
bad_hook_config_unknown_option_status=$?
set -e
[[ "$bad_hook_config_unknown_option_status" -ne 0 ]] || fail "hook config mixed with unknown option unexpectedly succeeded"
assert_contains "$bad_hook_config_unknown_option_output" "unknown option: --not-a-real-flag" "hook config unknown option error"

set +e
bad_hook_config_unknown_option_runtime_output="$(printf '%s' '{"hook_event_name":"SessionStart","session_id":"wrapper-config-unknown-option-secret"}' | "$script" agent-hook codex --config ./leafwiki.yml --not-a-real-flag 2>&1)"
bad_hook_config_unknown_option_runtime_status=$?
set -e
[[ "$bad_hook_config_unknown_option_runtime_status" -ne 0 ]] || fail "runtime hook config mixed with unknown option unexpectedly succeeded"
assert_contains "$bad_hook_config_unknown_option_runtime_output" "unknown option: --not-a-real-flag" "runtime hook config unknown option error"
assert_not_contains "$bad_hook_config_unknown_option_runtime_output" "wrapper-config-unknown-option-secret" "runtime hook config unknown option error"

set +e
bad_hook_config_unknown_option_reversed_output="$("$script" agent-hook codex --dry-run --not-a-real-flag --config ./leafwiki.yml 2>&1)"
bad_hook_config_unknown_option_reversed_status=$?
set -e
[[ "$bad_hook_config_unknown_option_reversed_status" -ne 0 ]] || fail "hook unknown option before config unexpectedly succeeded"
assert_contains "$bad_hook_config_unknown_option_reversed_output" "unknown option: --not-a-real-flag" "hook config reversed unknown option error"

set +e
bad_hook_config_unknown_option_reversed_runtime_output="$(printf '%s' '{"hook_event_name":"SessionStart","session_id":"wrapper-config-reversed-unknown-secret"}' | "$script" agent-hook codex --not-a-real-flag --config ./leafwiki.yml 2>&1)"
bad_hook_config_unknown_option_reversed_runtime_status=$?
set -e
[[ "$bad_hook_config_unknown_option_reversed_runtime_status" -ne 0 ]] || fail "runtime hook unknown option before config unexpectedly succeeded"
assert_contains "$bad_hook_config_unknown_option_reversed_runtime_output" "unknown option: --not-a-real-flag" "runtime hook config reversed unknown option error"
assert_not_contains "$bad_hook_config_unknown_option_reversed_runtime_output" "wrapper-config-reversed-unknown-secret" "runtime hook config reversed unknown option error"

set +e
hook_flag_value_named_config_runtime_output="$(printf '%s' '{"hook_event_name":"SessionStart","session_id":"wrapper-flag-value-config-secret"}' | "$script" agent-hook codex --data-dir --config --not-a-real-flag 2>&1)"
hook_flag_value_named_config_runtime_status=$?
set -e
[[ "$hook_flag_value_named_config_runtime_status" -eq 0 ]] || fail "runtime hook flag value named config did not fail open"
[[ "$hook_flag_value_named_config_runtime_output" == "{}" ]] || fail "runtime hook flag value named config output mismatch: $hook_flag_value_named_config_runtime_output"
assert_not_contains "$hook_flag_value_named_config_runtime_output" "wrapper-flag-value-config-secret" "runtime hook flag value named config"

set +e
bad_hook_config_missing_path_output="$("$script" agent-hook codex --dry-run --config 2>&1)"
bad_hook_config_missing_path_status=$?
set -e
[[ "$bad_hook_config_missing_path_status" -ne 0 ]] || fail "hook config missing path unexpectedly succeeded"
assert_contains "$bad_hook_config_missing_path_output" "--config requires a path" "hook config missing path error"

set +e
bad_hook_config_missing_path_runtime_output="$(printf '%s' '{"hook_event_name":"SessionStart","session_id":"wrapper-config-missing-path-secret"}' | "$script" agent-hook codex --config 2>&1)"
bad_hook_config_missing_path_runtime_status=$?
set -e
[[ "$bad_hook_config_missing_path_runtime_status" -ne 0 ]] || fail "runtime hook config missing path unexpectedly succeeded"
assert_contains "$bad_hook_config_missing_path_runtime_output" "--config requires a path" "runtime hook config missing path error"
assert_not_contains "$bad_hook_config_missing_path_runtime_output" "wrapper-config-missing-path-secret" "runtime hook config missing path error"

set +e
bad_hook_config_empty_equals_output="$("$script" agent-hook codex --dry-run --config= 2>&1)"
bad_hook_config_empty_equals_status=$?
set -e
[[ "$bad_hook_config_empty_equals_status" -ne 0 ]] || fail "hook empty config path unexpectedly succeeded"
assert_contains "$bad_hook_config_empty_equals_output" "--config requires a path" "hook empty config path error"

set +e
bad_hook_config_empty_equals_runtime_output="$(printf '%s' '{"hook_event_name":"SessionStart","session_id":"wrapper-config-empty-equals-secret"}' | "$script" agent-hook codex --config= 2>&1)"
bad_hook_config_empty_equals_runtime_status=$?
set -e
[[ "$bad_hook_config_empty_equals_runtime_status" -ne 0 ]] || fail "runtime hook empty config path unexpectedly succeeded"
assert_contains "$bad_hook_config_empty_equals_runtime_output" "--config requires a path" "runtime hook empty config path error"
assert_not_contains "$bad_hook_config_empty_equals_runtime_output" "wrapper-config-empty-equals-secret" "runtime hook empty config path error"

set +e
bad_hook_config_empty_value_output="$("$script" agent-hook codex --dry-run --config "" 2>&1)"
bad_hook_config_empty_value_status=$?
set -e
[[ "$bad_hook_config_empty_value_status" -ne 0 ]] || fail "hook empty config value unexpectedly succeeded"
assert_contains "$bad_hook_config_empty_value_output" "--config requires a path" "hook empty config value error"

set +e
bad_mcp_config_empty_value_output="$("$script" mcp --dry-run --config "" 2>&1)"
bad_mcp_config_empty_value_status=$?
set -e
[[ "$bad_mcp_config_empty_value_status" -ne 0 ]] || fail "mcp empty config value unexpectedly succeeded"
assert_contains "$bad_mcp_config_empty_value_output" "--config requires a path" "mcp empty config value error"

set +e
bad_mcp_config_whitespace_value_output="$("$script" mcp --dry-run --config "   " 2>&1)"
bad_mcp_config_whitespace_value_status=$?
set -e
[[ "$bad_mcp_config_whitespace_value_status" -ne 0 ]] || fail "mcp whitespace config value unexpectedly succeeded"
assert_contains "$bad_mcp_config_whitespace_value_output" "--config requires a path" "mcp whitespace config value error"

set +e
bad_mcp_config_spaced_dash_value_output="$("$script" mcp --dry-run --config "  ---config" 2>&1)"
bad_mcp_config_spaced_dash_value_status=$?
set -e
[[ "$bad_mcp_config_spaced_dash_value_status" -ne 0 ]] || fail "mcp leading-space dash config value unexpectedly succeeded"
assert_contains "$bad_mcp_config_spaced_dash_value_output" "--config requires a path" "mcp leading-space dash config value error"

set +e
bad_mcp_config_flag_value_output="$("$script" mcp --leafwiki-bin /bin/echo --config --dry-run 2>&1)"
bad_mcp_config_flag_value_status=$?
set -e
[[ "$bad_mcp_config_flag_value_status" -ne 0 ]] || fail "mcp config consumed flag as path unexpectedly succeeded"
assert_contains "$bad_mcp_config_flag_value_output" "--config requires a path" "mcp config flag value error"

set +e
bad_hook_config_flag_value_output="$("$script" agent-hook codex --leafwiki-bin /bin/echo --config --dry-run 2>&1)"
bad_hook_config_flag_value_status=$?
set -e
[[ "$bad_hook_config_flag_value_status" -ne 0 ]] || fail "hook config consumed flag as path unexpectedly succeeded"
assert_contains "$bad_hook_config_flag_value_output" "--config requires a path" "hook config flag value error"

set +e
bad_mcp_config_help_output="$("$script" mcp --config ./leafwiki.yml --help 2>&1)"
bad_mcp_config_help_status=$?
set -e
[[ "$bad_mcp_config_help_status" -ne 0 ]] || fail "mcp config mixed with help unexpectedly succeeded"
assert_contains "$bad_mcp_config_help_output" "--config cannot be combined with --help" "mcp config help error"

set +e
bad_hook_config_help_output="$(printf '%s' '{"hook_event_name":"SessionStart","session_id":"wrapper-config-help-secret"}' | "$script" agent-hook codex --config ./leafwiki.yml --help 2>&1)"
bad_hook_config_help_status=$?
set -e
[[ "$bad_hook_config_help_status" -ne 0 ]] || fail "hook config mixed with help unexpectedly succeeded"
assert_contains "$bad_hook_config_help_output" "--config cannot be combined with --help" "hook config help error"
assert_not_contains "$bad_hook_config_help_output" "wrapper-config-help-secret" "hook config help error"

set +e
bad_hook_provider_config_output="$("$script" agent-hook --config --dry-run 2>&1)"
bad_hook_provider_config_status=$?
set -e
[[ "$bad_hook_provider_config_status" -ne 0 ]] || fail "hook config without provider unexpectedly succeeded"
assert_contains "$bad_hook_provider_config_output" "agent-hook requires a provider" "hook config without provider error"

set +e
bad_hook_provider_config_runtime_output="$(printf '%s' '{"hook_event_name":"SessionStart","session_id":"wrapper-config-provider-secret"}' | "$script" agent-hook --config --dry-run 2>&1)"
bad_hook_provider_config_runtime_status=$?
set -e
[[ "$bad_hook_provider_config_runtime_status" -ne 0 ]] || fail "runtime hook config without provider unexpectedly succeeded"
assert_contains "$bad_hook_provider_config_runtime_output" "agent-hook requires a provider" "runtime hook config without provider error"
assert_not_contains "$bad_hook_provider_config_runtime_output" "wrapper-config-provider-secret" "runtime hook config without provider error"

for removed_flag in \
  --disable-workspace-sync \
  --enable-workspace-sync \
  --mode \
  --endpoint \
  "$legacy_bin_flag" \
  --server-log
do
  set +e
  removed_flag_output="$("$script" mcp --dry-run --leafwiki-bin /tmp/fake-leafwiki "$removed_flag" value 2>&1)"
  removed_flag_status=$?
  set -e
  [[ "$removed_flag_status" -ne 0 ]] || fail "$removed_flag unexpectedly succeeded"
  assert_contains "$removed_flag_output" "unknown option: $removed_flag" "$removed_flag error"
done

for removed_env in \
  LEAFWIKI_RUNTIME_STACK \
  LEAFWIKI_RUN_MCP_RUNTIME_STACK \
  LEAFWIKI_RUN_MCP_ENABLE_WORKSPACE_SYNC \
  LEAFWIKI_RUN_MCP_SERVER_LOG
do
  set +e
  removed_env_output="$(env "$removed_env=value" "$script" mcp --dry-run --leafwiki-bin /tmp/fake-leafwiki 2>&1)"
  removed_env_status=$?
  set -e
  [[ "$removed_env_status" -ne 0 ]] || fail "$removed_env unexpectedly succeeded"
  assert_contains "$removed_env_output" "unknown environment variable: $removed_env" "$removed_env error"
done

api_key_output="$(
  LEAFWIKI_RUN_MCP_API_KEY=lwk_secret \
  "$script" \
    mcp \
    --dry-run \
    --leafwiki-bin /tmp/fake-leafwiki \
    2>&1
)"
assert_contains "$api_key_output" "LEAFWIKI_MCP_API_KEY=REDACTED" "api-key dry-run"
assert_not_contains "$api_key_output" "LEAFWIKI_JWT_SECRET" "api-key dry-run"
assert_not_contains "$api_key_output" "LEAFWIKI_ADMIN_PASSWORD" "api-key dry-run"
assert_contains "$api_key_output" "--mcp=stdio" "api-key dry-run"
assert_not_contains "$api_key_output" "--api-key" "api-key dry-run"
assert_not_contains "$api_key_output" "lwk_secret" "api-key dry-run"
assert_not_contains "$api_key_output" "test-secret" "api-key dry-run"

bootstrap_output="$(
  LEAFWIKI_RUN_MCP_API_KEY=lwk_secret \
  "$script" \
    mcp \
    --dry-run \
    --leafwiki-bin /tmp/fake-leafwiki \
    --jwt-secret test-secret \
    --admin-password admin \
    2>&1
)"
assert_contains "$bootstrap_output" "LEAFWIKI_MCP_API_KEY=REDACTED" "bootstrap dry-run"
assert_contains "$bootstrap_output" "LEAFWIKI_JWT_SECRET=REDACTED" "bootstrap dry-run"
assert_contains "$bootstrap_output" "LEAFWIKI_ADMIN_PASSWORD=REDACTED" "bootstrap dry-run"
assert_not_contains "$bootstrap_output" "lwk_secret" "bootstrap dry-run"
assert_not_contains "$bootstrap_output" "test-secret" "bootstrap dry-run"

hook_bootstrap_output="$(
  "$script" \
    agent-hook codex \
    --dry-run \
    --leafwiki-bin /tmp/fake-leafwiki \
    --jwt-secret hook-jwt-secret \
    --admin-password hook-admin-password \
    2>&1
)"
assert_contains "$hook_bootstrap_output" "agent-hook codex" "hook bootstrap dry-run"
assert_contains "$hook_bootstrap_output" "LEAFWIKI_JWT_SECRET=REDACTED" "hook bootstrap dry-run"
assert_contains "$hook_bootstrap_output" "LEAFWIKI_ADMIN_PASSWORD=REDACTED" "hook bootstrap dry-run"
assert_not_contains "$hook_bootstrap_output" "--disable-auth=true" "hook bootstrap dry-run"
assert_not_contains "$hook_bootstrap_output" "LEAFWIKI_MCP_API_KEY" "hook bootstrap dry-run"
assert_not_contains "$hook_bootstrap_output" "hook-jwt-secret" "hook bootstrap dry-run"
assert_not_contains "$hook_bootstrap_output" "hook-admin-password" "hook bootstrap dry-run"

set +e
bad_auth_output="$("$script" mcp --dry-run --api-key lwk_secret --disable-auth 2>&1)"
bad_auth_status=$?
set -e
[[ "$bad_auth_status" -ne 0 ]] || fail "disabled-auth plus API key unexpectedly succeeded"
assert_contains "$bad_auth_output" "cannot be combined" "disabled-auth api-key error"
assert_not_contains "$bad_auth_output" "lwk_secret" "disabled-auth api-key error"

non_loopback_host_output="$("$script" mcp --dry-run --host 0.0.0.0 --leafwiki-bin /tmp/fake-leafwiki 2>&1)"
assert_contains "$non_loopback_host_output" "--host 0.0.0.0" "non-loopback stdio dry-run"
assert_contains "$non_loopback_host_output" "--mcp=stdio" "non-loopback stdio dry-run"
assert_not_contains "$non_loopback_host_output" "requires a loopback host" "non-loopback stdio dry-run"

set +e
bad_combined_arg_output="$("$script" mcp --dry-run "--root-dir ./wiki" 2>&1)"
bad_combined_arg_status=$?
set -e
[[ "$bad_combined_arg_status" -ne 0 ]] || fail "combined flag/value argument unexpectedly succeeded"
assert_contains "$bad_combined_arg_output" "split flags and values into separate args" "combined arg error"

missing_hook_stdout_file="$tmp_dir/missing-hook.stdout"
missing_hook_stderr_file="$tmp_dir/missing-hook.stderr"
set +e
printf '%s' '{"hook_event_name":"SessionStart","session_id":"wrapper-secret-session"}' | "$script" \
  agent-hook codex \
  --leafwiki-bin "$tmp_dir/missing-leafwiki" \
  > "$missing_hook_stdout_file" \
  2> "$missing_hook_stderr_file"
missing_hook_status=$?
set -e
[[ "$missing_hook_status" -eq 0 ]] || fail "missing hook binary blocked Codex hook: status $missing_hook_status"
[[ "$(cat "$missing_hook_stdout_file")" == "{}" ]] || fail "missing hook binary stdout was not Codex allow response: $(cat "$missing_hook_stdout_file")"
assert_not_contains "$(cat "$missing_hook_stderr_file")" "wrapper-secret-session" "missing Codex hook stderr"

missing_config_hook_stdout_file="$tmp_dir/missing-config-hook.stdout"
missing_config_hook_stderr_file="$tmp_dir/missing-config-hook.stderr"
set +e
printf '%s' '{"hook_event_name":"SessionStart","session_id":"wrapper-config-missing-binary-secret"}' | "$script" \
  agent-hook codex \
  --config ./leafwiki.yml \
  --leafwiki-bin "$tmp_dir/missing-leafwiki" \
  > "$missing_config_hook_stdout_file" \
  2> "$missing_config_hook_stderr_file"
missing_config_hook_status=$?
set -e
[[ "$missing_config_hook_status" -eq 0 ]] || fail "missing config hook binary blocked Codex hook: status $missing_config_hook_status"
[[ "$(cat "$missing_config_hook_stdout_file")" == "{}" ]] || fail "missing config hook binary stdout was not Codex allow response: $(cat "$missing_config_hook_stdout_file")"
assert_not_contains "$(cat "$missing_config_hook_stderr_file")" "wrapper-config-missing-binary-secret" "missing config Codex hook stderr"

missing_cursor_stdout_file="$tmp_dir/missing-cursor-hook.stdout"
set +e
printf '%s' '{"event":"sessionStart","session_id":"wrapper-secret-session"}' | "$script" \
  agent-hook cursor \
  --leafwiki-bin "$tmp_dir/missing-leafwiki" \
  > "$missing_cursor_stdout_file" \
  2> /dev/null
missing_cursor_status=$?
set -e
[[ "$missing_cursor_status" -eq 0 ]] || fail "missing hook binary blocked Cursor hook: status $missing_cursor_status"
[[ "$(cat "$missing_cursor_stdout_file")" == '{"permission":"allow"}' ]] || fail "missing hook binary stdout was not Cursor allow response: $(cat "$missing_cursor_stdout_file")"

fake_bin="$tmp_dir/bin"
mkdir -p "$fake_bin"

cat > "$fake_bin/leafwiki" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

printf '%s\n' "$@" > "$FAKE_LEAFWIKI_ARGS"
if [[ -n "${FAKE_LEAFWIKI_ENV:-}" ]]; then
  env | sort > "$FAKE_LEAFWIKI_ENV"
fi
if [[ -n "${FAKE_NATIVE_STDIN_FILE:-}" ]]; then
  cat > "$FAKE_NATIVE_STDIN_FILE"
else
  cat >/dev/null
fi
if [[ "${1:-}" == "agent-hook" ]]; then
  case "${2:-}" in
    cursor)
      printf '{"permission":"allow"}\n'
      ;;
    codex|claude)
      printf '{}\n'
      ;;
  esac
  exit 0
fi
printf 'native protocol stdout\n'
printf 'native diagnostic stderr\n' >&2
EOF

chmod +x "$fake_bin/leafwiki"

native_stdout_file="$tmp_dir/native.stdout"
native_stderr_file="$tmp_dir/native.stderr"
native_stdin_file="$tmp_dir/native.stdin"
native_args_file="$tmp_dir/native.args"
native_env_file="$tmp_dir/native.env"

FAKE_LEAFWIKI_ARGS="$native_args_file" \
FAKE_LEAFWIKI_ENV="$native_env_file" \
FAKE_NATIVE_STDIN_FILE="$native_stdin_file" \
LEAFWIKI_RUN_MCP_API_KEY=lwk_runtime_secret \
"$script" \
  mcp \
  --leafwiki-bin "$fake_bin/leafwiki" \
  --host 127.0.0.1 \
  --port 18081 \
  --root-dir "$tmp_dir/wiki" \
  --data-dir "$tmp_dir/data" \
  --markdown-link-root-prefix /docs \
  --daemon-idle-timeout 1s \
  <<< '{"jsonrpc":"2.0","id":1,"method":"initialize"}' \
  > "$native_stdout_file" \
  2> "$native_stderr_file"

[[ "$(cat "$native_stdout_file")" == "native protocol stdout" ]] || fail "native stdout was not direct protocol: $(cat "$native_stdout_file")"
[[ "$(cat "$native_stdin_file")" == '{"jsonrpc":"2.0","id":1,"method":"initialize"}' ]] || fail "native stdin was not passed to leafwiki: $(cat "$native_stdin_file")"
assert_contains "$(cat "$native_stderr_file")" "native diagnostic stderr" "native stderr"
assert_contains "$(cat "$native_args_file")" "--mcp=stdio" "native leafwiki args"
assert_contains "$(cat "$native_args_file")" "--daemon-idle-timeout" "native leafwiki args"
assert_contains "$(cat "$native_args_file")" "1s" "native leafwiki args"
assert_contains "$(cat "$native_args_file")" "--markdown-link-root-prefix" "native leafwiki args"
assert_contains "$(cat "$native_args_file")" "/docs" "native leafwiki args"
assert_contains "$(cat "$native_args_file")" "--log-target" "native leafwiki args"
assert_contains "$(cat "$native_args_file")" "file" "native leafwiki args"
assert_not_contains "$(cat "$native_args_file")" "stderr" "native leafwiki args"
assert_not_contains "$(cat "$native_args_file")" "--api-key" "native leafwiki args"
assert_not_contains "$(cat "$native_args_file")" "lwk_runtime_secret" "native leafwiki args"
assert_contains "$(cat "$native_env_file")" "LEAFWIKI_MCP_API_KEY=lwk_runtime_secret" "native env"
assert_not_contains "$(cat "$native_env_file")" "LEAFWIKI_JWT_SECRET=" "native env"
assert_not_contains "$(cat "$native_env_file")" "LEAFWIKI_ADMIN_PASSWORD=" "native env"
assert_not_contains "$(cat "$native_stderr_file")" "lwk_runtime_secret" "native stderr"
assert_not_contains "$(cat "$native_stderr_file")" "runtime-secret" "native stderr"

hook_stdout_file="$tmp_dir/hook.stdout"
hook_stderr_file="$tmp_dir/hook.stderr"
hook_stdin_file="$tmp_dir/hook.stdin"
hook_args_file="$tmp_dir/hook.args"
hook_env_file="$tmp_dir/hook.env"

FAKE_LEAFWIKI_ARGS="$hook_args_file" \
FAKE_LEAFWIKI_ENV="$hook_env_file" \
FAKE_NATIVE_STDIN_FILE="$hook_stdin_file" \
"$script" \
  agent-hook codex \
  --leafwiki-bin "$fake_bin/leafwiki" \
  --host 127.0.0.1 \
  --port 18082 \
  --root-dir "$tmp_dir/wiki" \
  --data-dir "$tmp_dir/data" \
  --jwt-secret hook-runtime-jwt \
  --admin-password hook-runtime-admin \
  <<< '{"hook_event_name":"SessionStart","session_id":"s1"}' \
  > "$hook_stdout_file" \
  2> "$hook_stderr_file"

[[ "$(cat "$hook_stdout_file")" == "{}" ]] || fail "hook stdout was not Codex allow response: $(cat "$hook_stdout_file")"
[[ "$(cat "$hook_stdin_file")" == '{"hook_event_name":"SessionStart","session_id":"s1"}' ]] || fail "hook stdin was not passed to leafwiki: $(cat "$hook_stdin_file")"
assert_contains "$(cat "$hook_args_file")" "agent-hook" "hook leafwiki args"
assert_contains "$(cat "$hook_args_file")" "codex" "hook leafwiki args"
assert_contains "$(cat "$hook_args_file")" "--data-dir" "hook leafwiki args"
assert_not_contains "$(cat "$hook_args_file")" "--disable-auth=true" "hook leafwiki args"
assert_not_contains "$(cat "$hook_args_file")" "hook-runtime-jwt" "hook leafwiki args"
assert_not_contains "$(cat "$hook_args_file")" "hook-runtime-admin" "hook leafwiki args"
assert_contains "$(cat "$hook_env_file")" "LEAFWIKI_JWT_SECRET=hook-runtime-jwt" "hook env"
assert_contains "$(cat "$hook_env_file")" "LEAFWIKI_ADMIN_PASSWORD=hook-runtime-admin" "hook env"
assert_not_contains "$(cat "$hook_env_file")" "LEAFWIKI_MCP_API_KEY=" "hook env"
assert_not_contains "$(cat "$hook_stderr_file")" "session_id" "hook stderr"
assert_not_contains "$(cat "$hook_stderr_file")" "hook-runtime-jwt" "hook stderr"
assert_not_contains "$(cat "$hook_stderr_file")" "hook-runtime-admin" "hook stderr"

printf 'PASS: run script checks\n'
