#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT

echo "Checking LeafWiki i18n catalog..."

(
  cd "$repo_root"
  go test ./internal/localization ./internal/localization/shell
  go test ./internal/wiki/mcp -run TestAllToolDescriptorDescriptionsAreCatalogBacked
  go test ./cmd/leafwiki -run TestFailureMessageRendersCatalogBackedErrorBody
  go run github.com/nicksnyder/go-i18n/v2/goi18n extract -outdir "$tmpdir" ./internal/localization
  diff -u internal/localization/locales/active.en.toml "$tmpdir/active.en.toml"
)

if find "$repo_root/internal/localization/locales" -name 'translate.*' -print -quit | grep -q .; then
  echo "translate.* files are not allowed in the committed English-only catalog phase." >&2
  exit 1
fi

python3 - "$repo_root" <<'PY'
import ast
import pathlib
import re
import sys

repo = pathlib.Path(sys.argv[1])
catalog = {}
current_id = None
for line in (repo / "internal/localization/locales/active.en.toml").read_text().splitlines():
    section = re.fullmatch(r'\["([^"]+)"\]', line)
    if section:
        current_id = section.group(1)
        catalog[current_id] = {}
        continue
    if current_id is None:
        continue
    key, sep, raw_value = line.partition("=")
    if sep != "=":
        continue
    key = key.strip()
    raw_value = raw_value.strip()
    if key in {"description", "other"}:
        catalog[current_id][key] = ast.literal_eval(raw_value)

localized = set()
for message_id, entry in catalog.items():
    if not isinstance(entry, dict):
        continue
    if not (
        message_id.startswith("api.")
        or message_id.startswith("errors.")
        or message_id.startswith("mcp.")
        or message_id.startswith("validation.")
        or message_id.startswith("ui.")
    ):
        continue
    text = entry.get("other")
    if not isinstance(text, str):
        continue
    if "{{" in text or len(text) < 12:
        continue
    localized.add(text)

e2e_call = re.compile(r"\b(?:getByText|toHaveText|toContainText)\(\s*([\"'`])((?:\\.|(?!\1).)*)\1", re.S)
e2e_violations = []
for path in (repo / "e2e").rglob("*.ts"):
    text = path.read_text(errors="replace")
    for match in e2e_call.finditer(text):
        literal = ast.literal_eval("'" + match.group(2).replace("'", "\\'") + "'")
        if literal in localized:
            line = text.count("\n", 0, match.start()) + 1
            e2e_violations.append(f"{path}:{line}: localized prose assertion {literal!r}")

if e2e_violations:
    print("E2E behavior tests must assert semantic IDs/status instead of migrated localized prose.", file=sys.stderr)
    print("\n".join(e2e_violations), file=sys.stderr)
    sys.exit(1)

def gin_h_blocks(text):
    needle = "gin.H{"
    start = 0
    while True:
        idx = text.find(needle, start)
        if idx < 0:
            return
        i = idx + len(needle) - 1
        depth = 0
        quote = ""
        escaped = False
        for pos in range(i, len(text)):
            ch = text[pos]
            if quote:
                if escaped:
                    escaped = False
                elif ch == "\\":
                    escaped = True
                elif ch == quote:
                    quote = ""
                continue
            if ch in ('"', "'", "`"):
                quote = ch
                continue
            if ch == "{":
                depth += 1
            elif ch == "}":
                depth -= 1
                if depth == 0:
                    yield idx, text[idx : pos + 1]
                    start = pos + 1
                    break
        else:
            return

def gin_h_error_payload_allowed(path, block):
    if "sharederrors.NewLocalizedErrorDetail(" in block or "sharederrors.LocalizedErrorDetailFromError(" in block:
        return True
    if re.search(r'"fields"\s*:', block) and re.search(r'"error"\s*:\s*\w+ValidationErrorCode\b', block):
        return True
    if "internal/wiki/oauth/" in str(path) and re.search(r'"error"\s*:\s*(?:oauthError\w+|rfcErr\.ErrorField)\b', block):
        return True
    return False

def payload_violations_for_text(path, text):
    violations = []
    for idx, block in gin_h_blocks(text):
        if re.search(r'"message"\s*:', block) and not re.search(r'"messageId"\s*:', block):
            line = text.count("\n", 0, idx) + 1
            violations.append(f"{path}:{line}: gin.H message payload lacks messageId")
        if re.search(r'"error"\s*:\s*"[^"]+"', block):
            line = text.count("\n", 0, idx) + 1
            violations.append(f"{path}:{line}: gin.H string error payload lacks structured localized error")
        elif re.search(r'"error"\s*:', block) and not gin_h_error_payload_allowed(path, block):
            line = text.count("\n", 0, idx) + 1
            violations.append(f"{path}:{line}: gin.H error payload lacks structured localized error")
    return violations

dynamic_error_fixture = 'package fixture\nfunc handler(err error) { c.JSON(500, gin.H{"error": err.Error()}) }\n'
if not payload_violations_for_text(pathlib.Path("internal/wiki/fixture/routes.go"), dynamic_error_fixture):
    print("plain error payload scanner self-test failed: dynamic gin.H error payload was not rejected", file=sys.stderr)
    sys.exit(1)
validation_error_fixture = 'package fixture\nfunc handler(vErr *ValidationErrors) { c.JSON(400, gin.H{"error": pageValidationErrorCode, "fields": vErr.Errors}) }\n'
validation_fixture_violations = payload_violations_for_text(pathlib.Path("internal/wiki/pages/errors.go"), validation_error_fixture)
if validation_fixture_violations:
    print("plain error payload scanner self-test failed: validation compatibility payload was rejected", file=sys.stderr)
    print("\n".join(validation_fixture_violations), file=sys.stderr)
    sys.exit(1)
oauth_error_fixture = 'package fixture\nfunc handler() { c.JSON(401, gin.H{"error": oauthErrorUnauthorized}) }\n'
oauth_fixture_violations = payload_violations_for_text(pathlib.Path("internal/wiki/oauth/responses.go"), oauth_error_fixture)
if oauth_fixture_violations:
    print("plain error payload scanner self-test failed: OAuth RFC error payload was rejected", file=sys.stderr)
    print("\n".join(oauth_fixture_violations), file=sys.stderr)
    sys.exit(1)

payload_violations = []
for root in (repo / "internal", repo / "cmd"):
    for path in root.rglob("*.go"):
        if "/internal/localization/" in str(path):
            continue
        if path.name.endswith("_test.go"):
            continue
        text = path.read_text(errors="replace")
        payload_violations.extend(payload_violations_for_text(path, text))

if payload_violations:
    print("Go API success/status message payloads must include messageId alongside message.", file=sys.stderr)
    print("\n".join(payload_violations), file=sys.stderr)
    sys.exit(1)

shell_violations = []
run_sh = repo / "scripts/run.sh"
shell_text = run_sh.read_text(errors="replace")
raw_shell_fail = re.compile(r'\b(?:fail|fail_error|fail_config_argument_error)\s+"([^"]*)"')
for match in raw_shell_fail.finditer(shell_text):
    literal = match.group(1)
    if literal.startswith("$(") or "$" in literal or "{{" in literal:
        continue
    line = shell_text.count("\n", 0, match.start()) + 1
    shell_violations.append(f"{run_sh}:{line}: shell failure body {literal!r} bypasses generated catalog messages")

if shell_violations:
    print("scripts/run.sh failure bodies must use generated catalog messages.", file=sys.stderr)
    print("\n".join(shell_violations), file=sys.stderr)
    sys.exit(1)
PY

echo "i18n catalog check passed."
