#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

echo "scripts/check-typed-id-oracles.sh is a compatibility wrapper; typed-id static policy is reported by scripts/golangci-lint.sh."
exec rtk bash "$repo_root/scripts/golangci-lint.sh" "$@"
