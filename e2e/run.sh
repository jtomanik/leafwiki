#!/usr/bin/env bash

set -euo pipefail

current_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$current_dir/.." && pwd)"
app_port="${E2E_PORT:-8085}"
app_base_path="${E2E_BASE_PATH:-}"
if [ -n "$app_base_path" ] && [[ "$app_base_path" != /* ]]; then
  echo "❌ E2E_BASE_PATH must start with / when set."
  exit 1
fi
app_url="${E2E_BASE_URL:-http://127.0.0.1:${app_port}${app_base_path}}"
run_mode="${E2E_RUN_MODE:-docker}"
server_pid=""
server_log=""
local_data_dir=""
local_root_dir=""
local_leafwiki_bin_dir=""
docker_data_volume=""
docker_root_volume=""
mcp_stdio_dir=""
mcp_stdio_command=""
mcp_stdio_seed_dir=""
mcp_stdio_seed_file=""

revision_or_workspace_sync_args() {
  if [ "${E2E_ENABLE_WORKSPACE_SYNC:-0}" = "1" ]; then
    printf '%s\n' "--enable-workspace-sync"
    return
  fi
  printf '%s\n' "--enable-revision=true"
}

collect_revision_or_workspace_sync_args() {
  local arg
  sync_args=()
  while IFS= read -r arg; do
    sync_args+=("$arg")
  done < <(revision_or_workspace_sync_args)
}

is_stdio_e2e() {
  [ "${E2E_MCP_CLIENT_TRANSPORT:-http}" = "stdio" ]
}

app_port_is_listening() {
  nc -z 127.0.0.1 "$app_port" >/dev/null 2>&1 || nc -z localhost "$app_port" >/dev/null 2>&1
}

print_runner_diagnostics() {
  echo "--- E2E runtime diagnostics ---"

  if [ "$run_mode" = "docker" ]; then
    if docker ps -a --format '{{.Names}}' | grep -q '^wiki-e2e-tests$'; then
      echo "--- docker logs (last 200 lines) ---"
      docker logs --tail 200 wiki-e2e-tests 2>&1 || true
    else
      echo "No Docker container logs available."
    fi
    return
  fi

  if [ -n "$server_log" ] && [ -f "$server_log" ]; then
    echo "--- local server log (last 200 lines) ---"
    tail -n 200 "$server_log" || true
  else
    echo "No local server log available."
  fi
}

build_frontend_for_local_e2e() {
  if [ "${E2E_SKIP_UI_BUILD:-0}" = "1" ]; then
    echo "⚡ Skipping UI build for local E2E run..."
  else
    echo "🔨 Building frontend for local E2E run..."
    (
      cd "$repo_root/ui/leafwiki-ui"
      npm run build
    )
  fi

  if [ ! -f "$repo_root/ui/leafwiki-ui/dist/index.html" ]; then
    echo "❌ Frontend build output is missing at ui/leafwiki-ui/dist/index.html"
    exit 1
  fi

  rm -rf "$repo_root/internal/http/dist"
  mkdir -p "$repo_root/internal/http/dist"
  cp -R "$repo_root/ui/leafwiki-ui/dist/." "$repo_root/internal/http/dist/"
  touch "$repo_root/internal/http/dist/.gitkeep"
}

build_leafwiki_binary() {
  local output_path="$1"
  (
    cd "$repo_root"
    go build -ldflags="-X github.com/perber/wiki/internal/http.EmbedFrontend=true -X github.com/perber/wiki/internal/http.Environment=production -X github.com/perber/wiki/internal/wiki/auth.DisableRefreshTokenRateLimit=true" -o "$output_path" ./cmd/leafwiki
  )
}

prepare_mcp_stdio_command() {
  if [ "${E2E_MCP_CLIENT_TRANSPORT:-http}" != "stdio" ]; then
    return
  fi
  if [ "$run_mode" != "local" ]; then
    echo "❌ E2E_MCP_CLIENT_TRANSPORT=stdio requires E2E_RUN_MODE=local."
    exit 1
  fi
  if [ "${E2E_ENABLE_MCP_OAUTH_LOCAL:-0}" = "1" ]; then
    echo "❌ MCP OAuth E2E must use Streamable HTTP; STDIO uses disabled-auth or API-key identity."
    exit 1
  fi

  mcp_stdio_dir="$(mktemp -d /tmp/leafwiki-mcp-e2e.XXXXXX)"
  local leafwiki_bin="$mcp_stdio_dir/leafwiki"
  mcp_stdio_command="$mcp_stdio_dir/leafwiki-native-stdio"
  echo "🔨 Building native LeafWiki STDIO binary for E2E..."
  build_leafwiki_binary "$leafwiki_bin"
  local native_args=(
    --mcp=stdio
    --host 127.0.0.1
    --port "$app_port"
    --data-dir "$local_data_dir"
    --daemon-idle-timeout "${E2E_DAEMON_IDLE_TIMEOUT:-3s}"
    --allow-insecure=true
    --enable-link-refactor=true
    --log-target stderr
    --disable-request-log
  )
  local sync_args=()
  collect_revision_or_workspace_sync_args
  native_args+=("${sync_args[@]}")
  if [ "${E2E_ENABLE_MCP_API_KEYS_LOCAL:-0}" = "1" ]; then
    native_args+=(
      --jwt-secret=e2e-tests-secret
      --admin-password=admin
    )
  else
    native_args+=(--disable-auth=true)
  fi
  if [ -n "$local_root_dir" ]; then
    native_args+=(--root-dir "$local_root_dir")
  fi
  if [ -n "$app_base_path" ]; then
    native_args+=(--base-path "$app_base_path")
  fi
  {
    printf '#!/usr/bin/env bash\n'
    printf 'set -euo pipefail\n'
    printf 'exec %q' "$leafwiki_bin"
    printf ' %q' "${native_args[@]}"
    printf '\n'
  } >"$mcp_stdio_command"
  chmod +x "$mcp_stdio_command"
}

seed_mcp_stdio_api_keys() {
  if [ "${E2E_MCP_CLIENT_TRANSPORT:-http}" != "stdio" ] || [ "${E2E_ENABLE_MCP_API_KEYS_LOCAL:-0}" != "1" ]; then
    return
  fi
  mcp_stdio_seed_dir="$(mktemp -d /tmp/leafwiki-mcp-seed.XXXXXX)"
  mcp_stdio_seed_file="$mcp_stdio_seed_dir/seed.json"
  echo "🌱 Seeding native STDIO MCP API-key users..."
  (
    cd "$repo_root"
    go run ./e2e/seed_mcp_api_keys.go --data-dir "$local_data_dir" --output "$mcp_stdio_seed_file"
  )
}

validate_mcp_mode_selection() {
  local mcp_modes_enabled=0
  if [ "${E2E_ENABLE_MCP_LOCAL:-0}" = "1" ]; then
    mcp_modes_enabled=$((mcp_modes_enabled + 1))
  fi
  if [ "${E2E_ENABLE_MCP_OAUTH_LOCAL:-0}" = "1" ]; then
    mcp_modes_enabled=$((mcp_modes_enabled + 1))
  fi
  if [ "${E2E_ENABLE_MCP_API_KEYS_LOCAL:-0}" = "1" ]; then
    mcp_modes_enabled=$((mcp_modes_enabled + 1))
  fi
  if [ "$mcp_modes_enabled" -gt 1 ]; then
    echo "❌ Set only one MCP E2E mode: E2E_ENABLE_MCP_LOCAL, E2E_ENABLE_MCP_OAUTH_LOCAL, or E2E_ENABLE_MCP_API_KEYS_LOCAL."
    exit 1
  fi
  if is_stdio_e2e && [ "$mcp_modes_enabled" -eq 0 ]; then
    echo "❌ STDIO MCP E2E requires E2E_ENABLE_MCP_LOCAL=1 or E2E_ENABLE_MCP_API_KEYS_LOCAL=1."
    exit 1
  fi
}

start_docker() {
  echo "🟢 Starting Docker container..."
  docker build \
    --build-arg DISABLE_REFRESH_TOKEN_RATE_LIMIT=true \
    -t wiki-e2e-tests \
    "$repo_root"

  if docker ps -a --format '{{.Names}}' | grep -q '^wiki-e2e-tests$'; then
    echo "⚠️ Removing existing container..."
    docker rm -f wiki-e2e-tests >/dev/null 2>&1 || true
  fi

  docker_data_volume="wiki-e2e-tests-data-${RANDOM}${RANDOM}"
  docker volume create "$docker_data_volume" >/dev/null
  local docker_args=(
    -p "$app_port:8080"
    --name wiki-e2e-tests
    -v "$docker_data_volume":/app/data
  )
  local server_args=(
    --allow-insecure=true
    --enable-link-refactor=true
    --jwt-secret=e2e-tests-secret
    --admin-password=admin
  )
  local sync_args=()
  collect_revision_or_workspace_sync_args
  server_args+=("${sync_args[@]}")

  if [ "${E2E_ENABLE_SEPARATE_ROOT_DIR:-0}" = "1" ] || [ "${E2E_ENABLE_WORKSPACE_SYNC:-0}" = "1" ]; then
    docker_root_volume="wiki-e2e-tests-root-${RANDOM}${RANDOM}"
    docker volume create "$docker_root_volume" >/dev/null
    docker_args+=(-v "$docker_root_volume":/app/root)
    server_args+=(--root-dir /app/root)
  fi

  docker run -d \
    "${docker_args[@]}" \
    wiki-e2e-tests \
    "${server_args[@]}"

  echo "✅ Container started on $app_url"
}

stop_docker() {
  echo "🛑 Stopping Docker container..."
  docker stop wiki-e2e-tests >/dev/null 2>&1 || true
  docker rm wiki-e2e-tests >/dev/null 2>&1 || true
  docker rmi wiki-e2e-tests >/dev/null 2>&1 || true
  if [ -n "$docker_data_volume" ]; then
    docker volume rm "$docker_data_volume" >/dev/null 2>&1 || true
  fi
  if [ -n "$docker_root_volume" ]; then
    docker volume rm "$docker_root_volume" >/dev/null 2>&1 || true
  fi
}

start_local() {
  echo "🟢 Starting local LeafWiki process..."
  build_frontend_for_local_e2e

  local_data_dir="$(mktemp -d /tmp/leafwiki-e2e-data.XXXXXX)"
  server_log="$(mktemp /tmp/leafwiki-e2e-server.XXXXXX)"
  rm -f "$server_log"
  server_log="${server_log}.log"
  local auth_args=(
    --jwt-secret=e2e-tests-secret
    --admin-password=admin
  )
  local server_args=(
    --host 127.0.0.1
    --port "$app_port"
    --data-dir "$local_data_dir"
    --daemon-idle-timeout "${E2E_DAEMON_IDLE_TIMEOUT:-0}"
  )

  if [ "${E2E_ENABLE_SEPARATE_ROOT_DIR:-0}" = "1" ] || [ "${E2E_ENABLE_WORKSPACE_SYNC:-0}" = "1" ]; then
    local_root_dir="$(mktemp -d /tmp/leafwiki-e2e-root.XXXXXX)"
    server_args+=(--root-dir "$local_root_dir")
  fi
  if [ -n "$app_base_path" ]; then
    server_args+=(--base-path "$app_base_path")
  fi
  local sync_args=()
  collect_revision_or_workspace_sync_args

  if is_stdio_e2e; then
    echo "✅ STDIO mode will start LeafWiki from the MCP client command."
    return
  fi

  if [ "${E2E_ENABLE_MCP_LOCAL:-0}" = "1" ]; then
    auth_args=(
      --disable-auth=true
      --mcp=http
    )
  elif [ "${E2E_ENABLE_MCP_OAUTH_LOCAL:-0}" = "1" ]; then
    auth_args=(
      --jwt-secret=e2e-tests-secret
      --admin-password=admin
      --mcp=http
    )
  elif [ "${E2E_ENABLE_MCP_API_KEYS_LOCAL:-0}" = "1" ]; then
    auth_args=(
      --jwt-secret=e2e-tests-secret
      --admin-password=admin
      --mcp=http
    )
  fi

  local_leafwiki_bin_dir="$(mktemp -d /tmp/leafwiki-e2e-bin.XXXXXX)"
  local local_leafwiki_bin="$local_leafwiki_bin_dir/leafwiki"
  echo "🔨 Building local LeafWiki binary for E2E..."
  build_leafwiki_binary "$local_leafwiki_bin"

  (
    exec "$local_leafwiki_bin" \
      "${server_args[@]}" \
      --allow-insecure=true \
      "${auth_args[@]}" \
      --enable-link-refactor=true \
      "${sync_args[@]}"
  ) >"$server_log" 2>&1 &

  server_pid=$!
  echo "✅ Local process started on $app_url (pid $server_pid)"
}

stop_local() {
  echo "🛑 Stopping local LeafWiki process..."
  if [ -n "$server_pid" ] && kill -0 "$server_pid" >/dev/null 2>&1; then
    kill "$server_pid" >/dev/null 2>&1 || true
    wait "$server_pid" >/dev/null 2>&1 || true
  fi
  wait_until_local_port_released
  if [ -n "$local_data_dir" ] && [ -d "$local_data_dir" ]; then
    rm -rf "$local_data_dir"
  fi
  if [ -n "$local_root_dir" ] && [ -d "$local_root_dir" ]; then
    rm -rf "$local_root_dir"
  fi
  if [ -n "$local_leafwiki_bin_dir" ] && [ -d "$local_leafwiki_bin_dir" ]; then
    rm -rf "$local_leafwiki_bin_dir"
  fi
  if [ -n "$mcp_stdio_seed_dir" ] && [ -d "$mcp_stdio_seed_dir" ]; then
    rm -rf "$mcp_stdio_seed_dir"
  fi

  if [ -n "$server_log" ] && [ -f "$server_log" ]; then
    rm -f "$server_log"
  fi
}

cleanup_mcp_stdio_command() {
  if [ -n "$mcp_stdio_dir" ] && [ -d "$mcp_stdio_dir" ]; then
    rm -rf "$mcp_stdio_dir"
  fi
}

stop_runner() {
  if [ "$run_mode" = "docker" ]; then
    stop_docker
  else
    stop_local
  fi
}

run_invalid_root_dir_startup_smoke() {
  if [ "$run_mode" = "docker" ] || [ "${E2E_ENABLE_SEPARATE_ROOT_DIR:-0}" != "1" ]; then
    return
  fi

  echo "Checking invalid root-dir startup failure..."
  local invalid_dir
  local invalid_log
  local invalid_pid
  invalid_dir="$(mktemp -d /tmp/leafwiki-e2e-invalid-root.XXXXXX)"
  invalid_log="$(mktemp /tmp/leafwiki-e2e-invalid-root.XXXXXX)"
  rm -f "$invalid_log"
  invalid_log="${invalid_log}.log"

  (
    cd "$repo_root"
    go run ./cmd/leafwiki/main.go \
      --host 127.0.0.1 \
      --port "$app_port" \
      --data-dir "$invalid_dir" \
      --root-dir "$invalid_dir" \
      --allow-insecure=true \
      --jwt-secret=e2e-tests-secret \
      --admin-password=admin
  ) >"$invalid_log" 2>&1 &
  invalid_pid=$!

  local exit_code=""
  for _ in $(seq 1 120); do
    if ! kill -0 "$invalid_pid" >/dev/null 2>&1; then
      set +e
      wait "$invalid_pid"
      exit_code=$?
      set -e
      break
    fi
    sleep 0.5
  done

  if [ -z "$exit_code" ]; then
    kill "$invalid_pid" >/dev/null 2>&1 || true
    wait "$invalid_pid" >/dev/null 2>&1 || true
    echo "❌ Invalid root-dir startup did not fail within 60 seconds."
    cat "$invalid_log" || true
    rm -rf "$invalid_dir"
    rm -f "$invalid_log"
    exit 1
  fi

  if [ "$exit_code" -eq 0 ]; then
    echo "❌ Invalid root-dir startup unexpectedly succeeded."
    cat "$invalid_log" || true
    rm -rf "$invalid_dir"
    rm -f "$invalid_log"
    exit 1
  fi

  if ! grep -q "root dir must be different from data dir" "$invalid_log"; then
    echo "❌ Invalid root-dir startup failed with unexpected output."
    cat "$invalid_log" || true
    rm -rf "$invalid_dir"
    rm -f "$invalid_log"
    exit 1
  fi

  rm -rf "$invalid_dir"
  rm -f "$invalid_log"
  echo "✅ Invalid root-dir startup failed as expected."
}

cleanup_runner() {
  local exit_code=$?

  if [ "$exit_code" -ne 0 ]; then
    print_runner_diagnostics
  fi

  stop_runner
  cleanup_mcp_stdio_command
  exit "$exit_code"
}

run_playwright_tests() {
  echo "Running Playwright tests..."
  (
    cd "$current_dir"
    local reporter="${E2E_PLAYWRIGHT_REPORTER:-line}"
    local workers="${E2E_PLAYWRIGHT_WORKERS:-1}"
    local assert_root_files="${E2E_ASSERT_SEPARATE_ROOT_FILES:-}"
    if [ -z "$assert_root_files" ]; then
      assert_root_files=0
      if [ "$run_mode" != "docker" ] && [ "${E2E_ENABLE_SEPARATE_ROOT_DIR:-0}" = "1" ]; then
        assert_root_files=1
      fi
    fi

    if command -v stdbuf >/dev/null 2>&1; then
      E2E_BASE_URL="$app_url" \
      E2E_ADMIN_USER="${E2E_ADMIN_USER:-admin}" \
      E2E_ADMIN_PASSWORD="${E2E_ADMIN_PASSWORD:-admin}" \
      E2E_ENABLE_SEPARATE_ROOT_DIR="${E2E_ENABLE_SEPARATE_ROOT_DIR:-0}" \
      E2E_ASSERT_SEPARATE_ROOT_FILES="$assert_root_files" \
      E2E_DATA_DIR="$local_data_dir" \
      E2E_ROOT_DIR="$local_root_dir" \
      E2E_REPO_ROOT="$repo_root" \
      E2E_MCP_CLIENT_TRANSPORT="${E2E_MCP_CLIENT_TRANSPORT:-http}" \
      E2E_MCP_STDIO_COMMAND="$mcp_stdio_command" \
      E2E_MCP_STDIO_SEED_FILE="$mcp_stdio_seed_file" \
      PLAYWRIGHT_FORCE_TTY=1 \
      stdbuf -oL -eL npx playwright test --workers="$workers" --reporter="$reporter" "$@"
    else
      E2E_BASE_URL="$app_url" \
      E2E_ADMIN_USER="${E2E_ADMIN_USER:-admin}" \
      E2E_ADMIN_PASSWORD="${E2E_ADMIN_PASSWORD:-admin}" \
      E2E_ENABLE_SEPARATE_ROOT_DIR="${E2E_ENABLE_SEPARATE_ROOT_DIR:-0}" \
      E2E_ASSERT_SEPARATE_ROOT_FILES="$assert_root_files" \
      E2E_DATA_DIR="$local_data_dir" \
      E2E_ROOT_DIR="$local_root_dir" \
      E2E_REPO_ROOT="$repo_root" \
      E2E_MCP_CLIENT_TRANSPORT="${E2E_MCP_CLIENT_TRANSPORT:-http}" \
      E2E_MCP_STDIO_COMMAND="$mcp_stdio_command" \
      E2E_MCP_STDIO_SEED_FILE="$mcp_stdio_seed_file" \
      PLAYWRIGHT_FORCE_TTY=1 \
      npx playwright test --workers="$workers" --reporter="$reporter" "$@"
    fi
  )
}

wait_until_reachable() {
  local max_attempts=60
  local attempt=0

  until curl -s "$app_url" >/dev/null; do
    printf '.'
    sleep 2
    attempt=$((attempt + 1))
    if [ "$attempt" -ge "$max_attempts" ]; then
      echo
      echo "❌ LeafWiki is not reachable after 2 minutes."
      if [ "$run_mode" = "local" ] && [ -f "$server_log" ]; then
        echo "--- local server log ---"
        tail -n 200 "$server_log" || true
      fi
      exit 1
    fi
  done

  echo
  echo "✅ LeafWiki is reachable."
}

wait_until_local_port_released() {
  if [ "$run_mode" = "docker" ]; then
    return
  fi

  local max_attempts=250
  local attempt=0
  while app_port_is_listening; do
    sleep 0.1
    attempt=$((attempt + 1))
    if [ "$attempt" -ge "$max_attempts" ]; then
      echo "⚠️ Local port $app_port is still listening after cleanup wait."
      return
    fi
  done
}

if app_port_is_listening; then
  if command -v docker >/dev/null 2>&1 && docker ps -a --format '{{.Names}}' | grep -q '^wiki-e2e-tests$'; then
    echo "⚠️ Port $app_port already in use by an existing E2E container – restarting it..."
    stop_docker
  else
    echo "❌ Port $app_port is already in use. Stop the existing process or choose another E2E_PORT."
    exit 1
  fi
fi

validate_mcp_mode_selection

if [ "$run_mode" = "docker" ]; then
  start_docker
else
  run_invalid_root_dir_startup_smoke
  start_local
fi
trap cleanup_runner EXIT

prepare_mcp_stdio_command
seed_mcp_stdio_api_keys
if ! is_stdio_e2e; then
  wait_until_reachable
fi
run_playwright_tests "$@"
