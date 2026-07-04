#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

echo "scripts/check-i18n-catalog.sh is a compatibility wrapper; static i18n/catalog policy is reported by scripts/golangci-lint.sh."
exec rtk bash "$repo_root/scripts/golangci-lint.sh" "$@"
