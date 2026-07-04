#!/usr/bin/env bash
set -euo pipefail

echo "Running LeafWiki semantic hygiene analyzer..."

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

rtk bash "$repo_root/scripts/golangci-lint.sh"

# The catalog and non-Go i18n policy still live in this sibling gate until the
# analyzer migration can happen without editing internal/analysis in this slice.
rtk bash "$repo_root/scripts/check-i18n-catalog.sh"

echo "Semantic hygiene analyzer passed."
