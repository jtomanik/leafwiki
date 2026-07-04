#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
config="$repo_root/.golangci.leafwiki.yml"

lint_bin="${LEAFWIKI_GOLANGCI_LINT_BIN:-$repo_root/.cache/tools/leafwiki-golangci-lint}"
lint_args=("$@")

safe_flag_requires_value() {
	case "$1" in
		--color | \
			--out-format | \
			--output.text.colors | \
			--output.text.path | \
			--output.text.print-issued-lines | \
			--output.text.print-linter-name | \
			--timeout)
		return 0
		;;
	*)
		return 1
		;;
	esac
}

reject_unsafe_flag() {
	echo "scripts/golangci-lint.sh only accepts non-policy runtime/output flags; rejected $1" >&2
	echo "Use the repository .golangci.leafwiki.yml policy, fixed package surface, and normal issue exit code." >&2
	exit 2
}

expect_flag_value=""
for arg in "${lint_args[@]}"; do
	if [[ -n "$expect_flag_value" ]]; then
		expect_flag_value=""
		continue
	fi

	if [[ "$arg" != -* ]]; then
		echo "scripts/golangci-lint.sh does not accept package arguments; it always runs the root module and e2e-proxy." >&2
		exit 2
	fi

	if [[ "$arg" == *=* ]]; then
		flag_name="${arg%%=*}"
		if ! safe_flag_requires_value "$flag_name"; then
			reject_unsafe_flag "$flag_name"
		fi
		continue
	fi

	if safe_flag_requires_value "$arg"; then
		expect_flag_value="$arg"
		continue
	fi

	reject_unsafe_flag "$arg"
done

if [[ -n "$expect_flag_value" ]]; then
	echo "missing value for $expect_flag_value" >&2
	exit 2
fi

custom_binary_is_stale() {
	[[ ! -x "$lint_bin" ]] && return 0

	find \
		"$repo_root/.custom-gcl.yml" \
		"$repo_root/go.mod" \
		"$repo_root/go.sum" \
		-newer "$lint_bin" -print -quit | grep -q . && return 0

	find \
		"$repo_root/tools/golangci/leafwiki" \
		"$repo_root/internal/analysis" \
		-name '*.go' -newer "$lint_bin" -print -quit | grep -q . && return 0

	return 1
}

ensure_lint_binary() {
	if [[ -n "${LEAFWIKI_GOLANGCI_LINT_BIN:-}" ]]; then
		return
	fi

	if ! custom_binary_is_stale; then
		return
	fi

	mkdir -p "$(dirname "$lint_bin")"
	(
		cd "$repo_root"
		rtk go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2 custom --verbose
	)
}

ensure_lint_binary

status=0

(
	cd "$repo_root"
	"$lint_bin" run --config "$config" "${lint_args[@]}" ./cmd/... ./internal/... ./e2e/... ./tools/...
) || status=$?

(
	cd "$repo_root/e2e-proxy"
	"$lint_bin" run --config "$config" "${lint_args[@]}" ./...
) || {
	module_status=$?
	if [[ "$status" -eq 0 ]]; then
		status=$module_status
	fi
}

exit "$status"
