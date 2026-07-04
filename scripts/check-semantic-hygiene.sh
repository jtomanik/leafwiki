#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

echo "scripts/check-semantic-hygiene.sh is a compatibility wrapper; static source policy is reported by scripts/golangci-lint.sh."
exec rtk bash "$repo_root/scripts/golangci-lint.sh" "$@"
