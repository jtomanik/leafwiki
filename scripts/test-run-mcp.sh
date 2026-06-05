#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd -- "$script_dir/.." && pwd)"
script="$repo_root/scripts/run-mcp.sh"

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

[[ -f "$script" ]] || fail "missing scripts/run-mcp.sh"

bash -n "$script"

help_output="$("$script" --help)"
assert_contains "$help_output" "--leafwiki-bin" "help"
assert_contains "$help_output" "--api-key" "help"
assert_contains "$help_output" "--daemon-idle-timeout" "help"
assert_contains "$help_output" "--server-arg" "help"
assert_contains "$help_output" "--dry-run" "help"
removed_binary="leafwiki""-mcp-stdio"
legacy_bin_flag="--mcp""-stdio-bin"
assert_not_contains "$help_output" "$removed_binary" "help"

tmp_dir="$(mktemp -d)"
cleanup() {
  rm -rf "$tmp_dir"
}
trap cleanup EXIT

native_default_output="$("$script" --dry-run --leafwiki-bin /tmp/fake-leafwiki 2>&1)"
assert_contains "$native_default_output" "Would run LeafWiki native MCP STDIO" "native dry-run"
assert_contains "$native_default_output" "/tmp/fake-leafwiki" "native dry-run"
assert_contains "$native_default_output" "--mcp=stdio" "native dry-run"
assert_contains "$native_default_output" "--disable-auth=true" "native dry-run"
assert_contains "$native_default_output" "--data-dir ./.wiki" "native dry-run"
assert_contains "$native_default_output" "--root-dir ./wiki" "native dry-run"
assert_contains "$native_default_output" "--daemon-idle-timeout 10m" "native dry-run"
assert_contains "$native_default_output" "--log-target file" "native dry-run"
assert_not_contains "$native_default_output" "--log-target stderr" "native dry-run"
assert_not_contains "$native_default_output" "$removed_binary" "native dry-run"

legacy_output="$(
  "$script" \
    --dry-run \
    --mode legacy \
    --endpoint http://127.0.0.1:8080/mcp \
    "$legacy_bin_flag" /tmp/old \
    --request-timeout 1s \
    --shutdown-timeout 1s \
    --max-frame-size 1MiB \
    --stdio-arg ignored \
    --leafwiki-bin /tmp/fake-leafwiki \
    2>&1
)"
assert_contains "$legacy_output" "--mcp=stdio" "legacy dry-run"
assert_not_contains "$legacy_output" "$removed_binary" "legacy dry-run"
assert_not_contains "$legacy_output" "--endpoint" "legacy dry-run"
assert_not_contains "$legacy_output" "/tmp/old" "legacy dry-run"

api_key_output="$(
  LEAFWIKI_RUN_MCP_API_KEY=lwk_secret \
  "$script" \
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

set +e
bad_auth_output="$("$script" --dry-run --api-key lwk_secret --disable-auth 2>&1)"
bad_auth_status=$?
set -e
[[ "$bad_auth_status" -ne 0 ]] || fail "disabled-auth plus API key unexpectedly succeeded"
assert_contains "$bad_auth_output" "cannot be combined" "disabled-auth api-key error"
assert_not_contains "$bad_auth_output" "lwk_secret" "disabled-auth api-key error"

non_loopback_host_output="$("$script" --dry-run --host 0.0.0.0 --leafwiki-bin /tmp/fake-leafwiki 2>&1)"
assert_contains "$non_loopback_host_output" "--host 0.0.0.0" "non-loopback stdio dry-run"
assert_contains "$non_loopback_host_output" "--mcp=stdio" "non-loopback stdio dry-run"
assert_not_contains "$non_loopback_host_output" "requires a loopback host" "non-loopback stdio dry-run"

set +e
bad_combined_arg_output="$("$script" --dry-run "--root-dir ./wiki" 2>&1)"
bad_combined_arg_status=$?
set -e
[[ "$bad_combined_arg_status" -ne 0 ]] || fail "combined flag/value argument unexpectedly succeeded"
assert_contains "$bad_combined_arg_output" "split flags and values into separate args" "combined arg error"

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
  --leafwiki-bin "$fake_bin/leafwiki" \
  --host 127.0.0.1 \
  --port 18081 \
  --root-dir "$tmp_dir/wiki" \
  --data-dir "$tmp_dir/data" \
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

printf 'PASS: run-mcp script checks\n'
