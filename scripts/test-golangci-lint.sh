#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT

fake_bin="$tmpdir/leafwiki-golangci-lint"
failing_root_bin="$tmpdir/failing-root-leafwiki-golangci-lint"
stale_repo="$tmpdir/stale-repo"
log_file="$tmpdir/invocations.log"
failure_log_file="$tmpdir/failure-invocations.log"
rtk_log_file="$tmpdir/rtk.log"

cat >"$fake_bin" <<'FAKE'
#!/usr/bin/env bash
set -euo pipefail

printf '%s\t%s\n' "$PWD" "$*" >>"${LEAFWIKI_GOLANGCI_LINT_TEST_LOG:?}"
FAKE
chmod +x "$fake_bin"

cat >"$failing_root_bin" <<'FAKE_FAILING_ROOT'
#!/usr/bin/env bash
set -euo pipefail

printf '%s\t%s\n' "$PWD" "$*" >>"${LEAFWIKI_GOLANGCI_LINT_TEST_LOG:?}"

if [[ "$PWD" == "${LEAFWIKI_GOLANGCI_LINT_TEST_ROOT:?}" ]]; then
	exit 1
fi
FAKE_FAILING_ROOT
chmod +x "$failing_root_bin"

cat >"$tmpdir/rtk" <<'FAKE_RTK'
#!/usr/bin/env bash
set -euo pipefail

printf '%s\n' "$*" >>"${LEAFWIKI_GOLANGCI_LINT_TEST_RTK_LOG:?}"

if [[ "${1:-}" == "go" && "${2:-}" == "run" ]]; then
	mkdir -p .cache/tools
	cat >.cache/tools/leafwiki-golangci-lint <<'FAKE_REBUILT_LINTER'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\t%s\n' "$PWD" "$*" >>"${LEAFWIKI_GOLANGCI_LINT_TEST_LOG:?}"
FAKE_REBUILT_LINTER
	chmod +x .cache/tools/leafwiki-golangci-lint
	exit 0
fi

if [[ "${1:-}" == "bash" && "${2:-}" == */scripts/check-i18n-catalog.sh ]]; then
	exit 0
fi

if [[ "${1:-}" == "bash" ]]; then
	shift
	exec bash "$@"
fi

exit 0
FAKE_RTK
chmod +x "$tmpdir/rtk"

	(
		cd "$repo_root/internal/wiki"
		LEAFWIKI_GOLANGCI_LINT_BIN="$fake_bin" \
			LEAFWIKI_GOLANGCI_LINT_TEST_LOG="$log_file" \
			rtk bash "$repo_root/scripts/golangci-lint.sh" --timeout 5m --output.text.colors=false
	)

	expected_root="$repo_root"$'\t'"run --config $repo_root/.golangci.yml --timeout 5m --output.text.colors=false ./cmd/... ./internal/... ./e2e/... ./tools/..."
	expected_proxy="$repo_root/e2e-proxy"$'\t'"run --config $repo_root/.golangci.yml --timeout 5m --output.text.colors=false ./..."

if ! grep -Fxq "$expected_root" "$log_file"; then
	echo "missing root module invocation" >&2
	cat "$log_file" >&2
	exit 1
fi

if ! grep -Fxq "$expected_proxy" "$log_file"; then
	echo "missing e2e-proxy module invocation" >&2
	cat "$log_file" >&2
	exit 1
fi

if grep -Fq "ui/leafwiki-ui/node_modules" "$log_file"; then
	echo "wrapper must not scan frontend node_modules as Go source" >&2
	cat "$log_file" >&2
	exit 1
fi

set +e
(
	cd "$repo_root"
	LEAFWIKI_GOLANGCI_LINT_BIN="$fake_bin" \
		LEAFWIKI_GOLANGCI_LINT_TEST_LOG="$tmpdir/rejected-package.log" \
		rtk bash "$repo_root/scripts/golangci-lint.sh" --timeout 30s ./tools/golangci/leafwiki \
			>"$tmpdir/rejected-package.stdout" \
			2>"$tmpdir/rejected-package.stderr"
)
package_status=$?
set -e

if [[ "$package_status" -eq 0 ]]; then
	echo "wrapper must reject package arguments instead of forwarding them to both modules" >&2
	cat "$tmpdir/rejected-package.stdout" "$tmpdir/rejected-package.stderr" >&2
	exit 1
fi

	if ! grep -Fq "does not accept package arguments" "$tmpdir/rejected-package.stderr"; then
		echo "wrapper package-argument rejection did not explain the contract" >&2
		cat "$tmpdir/rejected-package.stdout" "$tmpdir/rejected-package.stderr" >&2
		exit 1
	fi

	policy_flags=(
		"--config $tmpdir/alternate.yml"
		"--config=$tmpdir/alternate.yml"
		"--disable leafwiki"
		"--enable leafwiki"
		"--enable-only govet"
		"--issues-exit-code 0"
		"--new-from-rev HEAD~1"
		"--new-from-patch $tmpdir/changes.patch"
	)
	for flag_case in "${policy_flags[@]}"; do
		set +e
		(
			cd "$repo_root"
			LEAFWIKI_GOLANGCI_LINT_BIN="$fake_bin" \
				LEAFWIKI_GOLANGCI_LINT_TEST_LOG="$tmpdir/rejected-policy.log" \
				rtk bash -lc '"$1" "${@:2}"' _ "$repo_root/scripts/golangci-lint.sh" $flag_case \
					>"$tmpdir/rejected-policy.stdout" \
					2>"$tmpdir/rejected-policy.stderr"
		)
		policy_status=$?
		set -e

		if [[ "$policy_status" -eq 0 ]]; then
			echo "wrapper must reject policy-changing flag case: $flag_case" >&2
			cat "$tmpdir/rejected-policy.stdout" "$tmpdir/rejected-policy.stderr" >&2
			exit 1
		fi

		if ! grep -Fq "only accepts non-policy runtime/output flags" "$tmpdir/rejected-policy.stderr"; then
			echo "wrapper policy rejection did not explain the safe-flag contract for: $flag_case" >&2
			cat "$tmpdir/rejected-policy.stdout" "$tmpdir/rejected-policy.stderr" >&2
			exit 1
		fi
	done

	set +e
	(
		cd "$repo_root"
		LEAFWIKI_GOLANGCI_LINT_BIN="$failing_root_bin" \
			LEAFWIKI_GOLANGCI_LINT_TEST_LOG="$failure_log_file" \
			LEAFWIKI_GOLANGCI_LINT_TEST_ROOT="$repo_root" \
			rtk bash "$repo_root/scripts/golangci-lint.sh" --timeout 5m --output.text.colors=false
	)
failure_status=$?
set -e

if [[ "$failure_status" -eq 0 ]]; then
	echo "wrapper must fail when any module lint run fails" >&2
	cat "$failure_log_file" >&2
	exit 1
fi

if ! grep -Fxq "$expected_proxy" "$failure_log_file"; then
	echo "wrapper must still run e2e-proxy when root module lint fails" >&2
	cat "$failure_log_file" >&2
	exit 1
fi

mkdir -p "$stale_repo/scripts" "$stale_repo/.cache/tools" "$stale_repo/tools/golangci/leafwiki" "$stale_repo/internal/analysis/semantichygiene" "$stale_repo/e2e-proxy"
cp "$repo_root/scripts/golangci-lint.sh" "$stale_repo/scripts/golangci-lint.sh"
touch "$stale_repo/.golangci.yml" "$stale_repo/.custom-gcl.yml" "$stale_repo/go.mod" "$stale_repo/go.sum"
touch "$stale_repo/tools/golangci/leafwiki/plugin.go" "$stale_repo/internal/analysis/semantichygiene/analyzer.go"
cat >"$stale_repo/.cache/tools/leafwiki-golangci-lint" <<'STALE_LINTER'
#!/usr/bin/env bash
set -euo pipefail
printf 'stale\t%s\t%s\n' "$PWD" "$*" >>"${LEAFWIKI_GOLANGCI_LINT_TEST_LOG:?}"
STALE_LINTER
chmod +x "$stale_repo/.cache/tools/leafwiki-golangci-lint"
touch -t 202001010000 "$stale_repo/.cache/tools/leafwiki-golangci-lint"
touch "$stale_repo/internal/analysis/semantichygiene/analyzer.go"

	PATH="$tmpdir:$PATH" \
		LEAFWIKI_GOLANGCI_LINT_TEST_LOG="$tmpdir/rebuild-invocations.log" \
		LEAFWIKI_GOLANGCI_LINT_TEST_RTK_LOG="$tmpdir/rebuild-rtk.log" \
		rtk bash "$stale_repo/scripts/golangci-lint.sh" --timeout 5m --output.text.colors=false

if ! grep -Fq "go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2 custom --verbose" "$tmpdir/rebuild-rtk.log"; then
	echo "wrapper did not rebuild a stale custom binary" >&2
	cat "$tmpdir/rebuild-rtk.log" >&2
	exit 1
fi

PATH="$tmpdir:$PATH" \
	LEAFWIKI_GOLANGCI_LINT_BIN="$fake_bin" \
	LEAFWIKI_GOLANGCI_LINT_TEST_LOG="$log_file" \
	LEAFWIKI_GOLANGCI_LINT_TEST_RTK_LOG="$rtk_log_file" \
	rtk bash "$repo_root/scripts/check-semantic-hygiene.sh"

if ! grep -Fxq "bash $repo_root/scripts/golangci-lint.sh" "$rtk_log_file"; then
	echo "semantic hygiene wrapper did not invoke the golangci-lint gate" >&2
	cat "$rtk_log_file" >&2
	exit 1
fi

if ! grep -Fxq "bash $repo_root/scripts/check-i18n-catalog.sh" "$rtk_log_file"; then
	echo "semantic hygiene wrapper did not preserve the i18n catalog gate" >&2
	cat "$rtk_log_file" >&2
	exit 1
fi

echo "golangci-lint wrapper smoke test passed."
