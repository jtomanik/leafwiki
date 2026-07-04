#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

rtk go run "$repo_root/cmd/leafwiki-test-taxonomy" \
  "$repo_root" \
  "$repo_root/e2e-proxy"
