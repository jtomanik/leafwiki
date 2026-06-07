#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd -- "$script_dir/.." && pwd)"
initial_cwd="$(pwd)"

leafwiki_installer="$script_dir/install-macos.sh"
run_script="$script_dir/run.sh"
install_dir="${LEAFWIKI_INSTALL_DIR:-/usr/local/bin}"
build_dir="${LEAFWIKI_BUILD_DIR:-$repo_root/releases}"
version="${LEAFWIKI_VERSION:-}"
arch="${LEAFWIKI_ARCH:-${GOARCH:-}}"
dry_run=0
skip_npm_ci=0

usage() {
  cat <<EOF
Usage: scripts/install-all-macos.sh [options]

Builds and installs local macOS helpers from this checkout:
  - leafwiki
  - run.sh

Options:
  --install-dir <path>       Directory to install files into (default: /usr/local/bin)
  --build-dir <path>         Build output directory for leafwiki (default: ./releases)
  --server-build-dir <path>  Alias for --build-dir
  --version <version>        Version passed to the installer (default: latest git tag or v0.1.0)
  --arch <arch>              Target architecture: arm64 or amd64 (default: current Go arch)
  --skip-npm-ci              Reuse existing frontend dependencies for the leafwiki build
  --dry-run                  Print the build/install plan without changing files
  -h, --help                 Show this help

Environment overrides:
  LEAFWIKI_INSTALL_DIR, LEAFWIKI_BUILD_DIR, LEAFWIKI_VERSION, LEAFWIKI_ARCH
EOF
}

fail() {
  printf 'Error: %s\n' "$1" >&2
  exit 1
}

log() {
  printf '%s\n' "$1"
}

quote_command() {
  local arg
  for arg in "$@"; do
    printf '%q ' "$arg"
  done
}

run() {
  printf '+ '
  quote_command "$@"
  printf '\n'
  "$@"
}

command_exists() {
  command -v "$1" >/dev/null 2>&1
}

require_command() {
  command_exists "$1" || fail "$1 is required"
}

absolute_path() {
  case "$1" in
    /*)
      printf '%s\n' "$1"
      ;;
    *)
      printf '%s/%s\n' "$initial_cwd" "$1"
      ;;
  esac
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --install-dir)
      [[ $# -ge 2 ]] || fail "--install-dir requires a path"
      install_dir="$2"
      shift 2
      ;;
    --build-dir|--server-build-dir)
      [[ $# -ge 2 ]] || fail "$1 requires a path"
      build_dir="$2"
      shift 2
      ;;
    --version)
      [[ $# -ge 2 ]] || fail "--version requires a value"
      version="$2"
      shift 2
      ;;
    --arch)
      [[ $# -ge 2 ]] || fail "--arch requires arm64 or amd64"
      arch="$2"
      shift 2
      ;;
    --skip-npm-ci)
      skip_npm_ci=1
      shift
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

[[ -x "$leafwiki_installer" ]] || fail "missing executable installer at $leafwiki_installer"
[[ -x "$run_script" ]] || fail "missing executable wrapper at $run_script"

if [[ "$(uname -s)" != "Darwin" ]]; then
  if [[ "$dry_run" -eq 1 ]]; then
    log "Dry run only: actual install requires macOS."
  else
    fail "this installer is for macOS only"
  fi
fi

if [[ -z "$version" ]]; then
  if command_exists git && git -C "$repo_root" describe --tags --abbrev=0 >/dev/null 2>&1; then
    version="$(git -C "$repo_root" describe --tags --abbrev=0)"
  else
    version="v0.1.0"
  fi
fi

if [[ -z "$arch" ]]; then
  if command_exists go; then
    arch="$(go env GOARCH)"
  else
    case "$(uname -m)" in
      arm64|aarch64)
        arch="arm64"
        ;;
      x86_64)
        arch="amd64"
        ;;
      *)
        fail "could not infer architecture; pass --arch arm64 or --arch amd64"
        ;;
    esac
  fi
fi

case "$arch" in
  arm64|amd64)
    ;;
  *)
    fail "unsupported architecture '$arch'; expected arm64 or amd64"
    ;;
esac

install_dir="$(absolute_path "$install_dir")"
build_dir="$(absolute_path "$build_dir")"
run_target="$install_dir/run.sh"

leafwiki_args=(
  --install-dir "$install_dir"
  --build-dir "$build_dir"
  --version "$version"
  --arch "$arch"
)
if [[ "$skip_npm_ci" -eq 1 ]]; then
  leafwiki_args+=(--skip-npm-ci)
fi
if [[ "$dry_run" -eq 1 ]]; then
  leafwiki_args+=(--dry-run)
fi

if [[ "$dry_run" -eq 1 ]]; then
  log "Would install LeafWiki and run.sh $version for darwin/$arch"
else
  log "Installing LeafWiki and run.sh $version for darwin/$arch"
fi
log "Install directory: $install_dir"
log "LeafWiki build directory: $build_dir"
log "run.sh install target: $run_target"

run "$leafwiki_installer" "${leafwiki_args[@]}"

if [[ "$dry_run" -eq 1 ]]; then
  if [[ -d "$install_dir" && -w "$install_dir" ]]; then
    printf '+ '
    quote_command install -m 0755 "$run_script" "$run_target"
    printf '\n'
  else
    printf '+ '
    quote_command sudo install -m 0755 "$run_script" "$run_target"
    printf '\n'
  fi
else
  require_command install
  if [[ ! -d "$install_dir" ]]; then
    parent_dir="$(dirname -- "$install_dir")"
    if [[ -d "$parent_dir" && -w "$parent_dir" ]]; then
      run mkdir -p "$install_dir"
    else
      require_command sudo
      run sudo mkdir -p "$install_dir"
    fi
  fi

  if [[ -w "$install_dir" ]]; then
    run install -m 0755 "$run_script" "$run_target"
  else
    require_command sudo
    run sudo install -m 0755 "$run_script" "$run_target"
  fi
fi

if [[ "$dry_run" -eq 0 ]]; then
  [[ -x "$install_dir/leafwiki" ]] || fail "leafwiki was not installed at $install_dir/leafwiki"
  [[ -x "$run_target" ]] || fail "run.sh was not installed at $run_target"
  log "Installed LeafWiki and run.sh to $install_dir"
else
  log "Dry run complete for LeafWiki and run.sh into $install_dir"
fi
