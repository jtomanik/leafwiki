#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

echo "Running frontend semantic hygiene ESLint checks..."

status=0

rtk npm --prefix "$repo_root/ui/leafwiki-ui" run lint:semantic || status=$?
rtk npm --prefix "$repo_root/e2e" run lint:semantic || status=$?

if [[ "$status" -ne 0 ]]; then
  echo "Frontend semantic hygiene ESLint checks failed."
  exit "$status"
fi

echo "Frontend semantic hygiene ESLint checks passed."
