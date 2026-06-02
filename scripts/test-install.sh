#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd -- "$script_dir/.." && pwd)"
script="$repo_root/install.sh"

fail() {
  printf 'FAIL: %s\n' "$1" >&2
  exit 1
}

[[ -f "$script" ]] || fail "missing install.sh"

bash -n "$script"

tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT

fake_bin="$tmp_dir/bin"
mkdir -p "$fake_bin"
printf '#!/usr/bin/env bash\nexit 0\n' > "$fake_bin/systemctl"
printf '#!/usr/bin/env bash\nexit 0\n' > "$fake_bin/wget"
cat > "$fake_bin/realpath" <<'EOF'
#!/usr/bin/env bash
if [[ "${1:-}" == "-m" ]]; then
  shift
fi
python3 -c 'import os,sys; print(os.path.normpath(sys.argv[1]))' "$1"
EOF
chmod +x "$fake_bin/systemctl" "$fake_bin/wget" "$fake_bin/realpath"

run_installer() {
  PATH="$fake_bin:$PATH" "$script" "$@"
}

write_env_file() {
  local path="$1"
  local data_dir="$2"
  local root_dir="$3"

  cat > "$path" <<EOF
LEAFWIKI_ARCH=amd64
LEAFWIKI_VERSION=9.9.9
LEAFWIKI_DATA_DIR=$data_dir
LEAFWIKI_ROOT_DIR=$root_dir
LEAFWIKI_JWT_SECRET=test-secret
LEAFWIKI_ADMIN_PASSWORD=test-password
EOF
}

expect_invalid_non_interactive() {
  local name="$1"
  local data_dir="$2"
  local root_dir="$3"
  local expected="$4"
  local env_file="$tmp_dir/$name.env"
  local output_file="$tmp_dir/$name.out"

  write_env_file "$env_file" "$data_dir" "$root_dir"
  if LEAFWIKI_INSTALL_VALIDATE_ONLY=1 run_installer --non-interactive --env-file "$env_file" > "$output_file" 2>&1; then
    fail "$name unexpectedly passed"
  fi
  if ! grep -q "$expected" "$output_file"; then
    fail "$name output did not contain '$expected': $(cat "$output_file")"
  fi
}

expect_invalid_non_interactive \
  "same-dir" \
  "$tmp_dir/same" \
  "$tmp_dir/same" \
  "must be different"

expect_invalid_non_interactive \
  "root-contains-data" \
  "$tmp_dir/wiki/data" \
  "$tmp_dir/wiki" \
  "must not contain"

expect_invalid_non_interactive \
  "reserved-state" \
  "$tmp_dir/data" \
  "$tmp_dir/data/assets/pages" \
  "app state path"

default_env="$tmp_dir/default-root.env"
default_output="$tmp_dir/default-root.out"
write_env_file "$default_env" "$tmp_dir/default-data" ""
if ! LEAFWIKI_INSTALL_VALIDATE_ONLY=1 run_installer --non-interactive --env-file "$default_env" > "$default_output" 2>&1; then
  fail "empty root defaulting failed: $(cat "$default_output")"
fi
if ! grep -q "Validated LeafWiki install configuration" "$default_output"; then
  fail "empty root defaulting did not stop after validation: $(cat "$default_output")"
fi
if ! grep -q "RootDirectory: $tmp_dir/default-data/root" "$default_output"; then
  fail "empty root defaulting output was wrong: $(cat "$default_output")"
fi

valid_logging_env="$tmp_dir/valid-logging.env"
valid_logging_output="$tmp_dir/valid-logging.out"
write_env_file "$valid_logging_env" "$tmp_dir/logging-data" "$tmp_dir/logging-pages"
printf 'LEAFWIKI_LOG_TARGET=stderr\n' >> "$valid_logging_env"
if ! LEAFWIKI_INSTALL_VALIDATE_ONLY=1 run_installer --non-interactive --env-file "$valid_logging_env" > "$valid_logging_output" 2>&1; then
  fail "valid logging target failed: $(cat "$valid_logging_output")"
fi
if ! grep -q "Validated LeafWiki install configuration" "$valid_logging_output"; then
  fail "valid logging target did not stop after validation: $(cat "$valid_logging_output")"
fi

invalid_logging_file_env="$tmp_dir/invalid-logging-file.env"
invalid_logging_file_output="$tmp_dir/invalid-logging-file.out"
write_env_file "$invalid_logging_file_env" "$tmp_dir/invalid-logging-file-data" "$tmp_dir/invalid-logging-file-pages"
printf 'LEAFWIKI_LOG_TARGET=stderr\n' >> "$invalid_logging_file_env"
printf 'LEAFWIKI_LOG_FILE=logs/leafwiki.log\n' >> "$invalid_logging_file_env"
if LEAFWIKI_INSTALL_VALIDATE_ONLY=1 run_installer --non-interactive --env-file "$invalid_logging_file_env" > "$invalid_logging_file_output" 2>&1; then
  fail "log file with stderr target unexpectedly passed"
fi
if ! grep -q "LEAFWIKI_LOG_FILE requires LEAFWIKI_LOG_TARGET=file" "$invalid_logging_file_output"; then
  fail "log file with stderr target output was wrong: $(cat "$invalid_logging_file_output")"
fi

invalid_logging_env="$tmp_dir/invalid-logging.env"
invalid_logging_output="$tmp_dir/invalid-logging.out"
write_env_file "$invalid_logging_env" "$tmp_dir/invalid-logging-data" "$tmp_dir/invalid-logging-pages"
printf 'LEAFWIKI_LOG_TARGET=system\n' >> "$invalid_logging_env"
if LEAFWIKI_INSTALL_VALIDATE_ONLY=1 run_installer --non-interactive --env-file "$invalid_logging_env" > "$invalid_logging_output" 2>&1; then
  fail "invalid logging target unexpectedly passed"
fi
if ! grep -q "invalid LEAFWIKI_LOG_TARGET" "$invalid_logging_output"; then
  fail "invalid logging target output was wrong: $(cat "$invalid_logging_output")"
fi

trimmed_env="$tmp_dir/trimmed.env"
trimmed_output="$tmp_dir/trimmed.out"
write_env_file "$trimmed_env" " $tmp_dir/trimmed-data " " $tmp_dir/trimmed-pages "
if ! LEAFWIKI_INSTALL_VALIDATE_ONLY=1 run_installer --non-interactive --env-file "$trimmed_env" > "$trimmed_output" 2>&1; then
  fail "trimmed path validation failed: $(cat "$trimmed_output")"
fi
if ! grep -q "DataDirectory: $tmp_dir/trimmed-data" "$trimmed_output"; then
  fail "trimmed data dir output was wrong: $(cat "$trimmed_output")"
fi
if ! grep -q "RootDirectory: $tmp_dir/trimmed-pages" "$trimmed_output"; then
  fail "trimmed root dir output was wrong: $(cat "$trimmed_output")"
fi
if grep -q "DataDirectory:  " "$trimmed_output" || grep -q "RootDirectory:  " "$trimmed_output"; then
  fail "trimmed output retained leading spaces: $(cat "$trimmed_output")"
fi

interactive_env="$tmp_dir/interactive.env"
interactive_output="$tmp_dir/interactive.out"
interactive_input="$(
  printf 'amd64\n'
  printf 'test-secret\n'
  printf 'test-password\n'
  printf '127.0.0.1\n'
  printf '8080\n'
  printf 'n\n'
  printf '%s\n' "$tmp_dir/interactive-data"
  printf '%s\n' "$tmp_dir/interactive-data"
  printf 'n\n'
)"
if printf '%s' "$interactive_input" |
  LEAFWIKI_ENV_FILE_PATH="$interactive_env" LEAFWIKI_INSTALL_VALIDATE_ONLY=1 run_installer > "$interactive_output" 2>&1; then
  fail "interactive invalid root unexpectedly passed"
fi
if [[ -e "$interactive_env" ]]; then
  fail "interactive invalid root wrote env file before validation"
fi
if ! grep -q "must be different" "$interactive_output"; then
  fail "interactive invalid root output was wrong: $(cat "$interactive_output")"
fi

revision_env="$tmp_dir/interactive-revision.env"
revision_output="$tmp_dir/interactive-revision.out"
revision_input="$(
  printf 'amd64\n'
  printf 'test-secret\n'
  printf 'test-password\n'
  printf '127.0.0.1\n'
  printf '8080\n'
  printf 'n\n'
  printf '%s\n' "$tmp_dir/revision-data"
  printf '%s\n' "$tmp_dir/revision-pages"
  printf 'y\n'
  printf '\n'
)"
if ! printf '%s' "$revision_input" |
  LEAFWIKI_ENV_FILE_PATH="$revision_env" LEAFWIKI_INSTALL_VALIDATE_ONLY=1 run_installer > "$revision_output" 2>&1; then
  fail "interactive revision validation failed: $(cat "$revision_output")"
fi
if ! grep -q 'LEAFWIKI_ENABLE_REVISION="true"' "$revision_env"; then
  fail "interactive revision env was wrong: $(cat "$revision_env")"
fi
if ! grep -q 'LEAFWIKI_LOG_TARGET="file"' "$revision_env"; then
  fail "interactive logging target env was wrong: $(cat "$revision_env")"
fi
if ! grep -q 'LEAFWIKI_LOG_FILE=""' "$revision_env"; then
  fail "interactive logging file env was wrong: $(cat "$revision_env")"
fi

printf 'PASS: install.sh validation checks\n'
