<!-- leafwiki
version: 1
page:
  id: u1p13U-Dgm
  title: Semantic Types and IDs Scan Triage
  created_at: "2026-06-21T22:54:33.877164914Z"
  updated_at: "2026-06-21T22:54:33.877164914Z"
  creator_id: system
  last_author_id: system
-->

# Semantic Types and IDs Scan Triage

Date: 2026-06-22

Command:

```sh
rtk bash scripts/check-typed-id-oracles.sh
```

Result: the scan delegates Go semantic-boundary enforcement to `scripts/check-semantic-hygiene.sh`, then checks the remaining frontend/text assertion surface. It completed successfully; the remaining text candidates are accepted for this slice under the categories below.

## Converted In This Slice

- API localized errors now expose `messageId` with `code`.
- Field validation errors now expose `code` and `messageId`.
- MCP message-only success schemas and payloads now expose `messageId`.
- MCP localized/page tool errors expose structured `_meta.error` with `code`, `messageId`, and `message`.
- The editor version-conflict E2E path now asserts `data-error-code="page_version_conflict"` and `data-l10n-id="errors.page.version_conflict"`.
- Workspace-sync issue rows expose `data-validation-code` and `data-validation-severity`.
- History state UI exposes `data-revision-badge` and `data-history-change`.
- MCP API key rows/load failures expose semantic key/error attributes.

## Remaining Accepted Categories

- User-authored page, Markdown, tag, search, import, and article content assertions.
- Accessibility behavior that intentionally uses visible names through `getByRole`.
- Backward-compatibility tests that intentionally assert legacy `message` fields.
- Low-level Go error-string tests for filesystem, lock, registry, daemon, and parser failures that do not cross the API/MCP/frontend semantic contract boundary in this plan.
- Documentation and fixture strings.

## Follow-Up Rule

New user/agent-facing contracts should not add fresh English-only assertions when a stable code, message ID, typed helper value, or semantic `data-*` attribute can express the same contract.
