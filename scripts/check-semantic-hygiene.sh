#!/usr/bin/env bash
set -euo pipefail

echo "Running LeafWiki semantic hygiene analyzer..."

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT

(
  cd "$tmpdir"
  rtk go work init "$repo_root" "$repo_root/e2e-proxy"
  GOWORK="$tmpdir/go.work" rtk go run "$repo_root/cmd/leafwiki-vet" \
    "$repo_root/internal/..." \
    "$repo_root/cmd/..." \
    "$repo_root/e2e/..." \
    "$repo_root/e2e-proxy/..."
)

echo "Semantic hygiene analyzer passed."
