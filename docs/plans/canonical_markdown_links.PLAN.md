<!-- leafwiki
version: 1
page:
  id: i0hauSavg
  title: Canonical Filesystem Markdown Links Plan
  created_at: "2026-06-15T05:44:44.854020139Z"
  updated_at: "2026-06-15T05:44:44.854020139Z"
  creator_id: system
  last_author_id: system
-->

# Canonical Filesystem Markdown Links Plan

> **Historical note:** This plan predates the federated runtime cleanup. Runtime, MCP compatibility, revision-mode, or workspace-sync flag examples in this artifact are historical and do not describe current startup; current LeafWiki uses `--mcp` and always-on Git-backed workspace sync.

Reference thread: `codex://threads/019eb2fd-c83c-7d52-ad25-180701842e35`
Target plan file: `plans/canonical_markdown_links.PLAN.md`
Planning methods used: `superpowers:writing-plans`, `compound-engineering:ce-plan`, plus two read-only subagents for link/refactor and workspace-sync surfaces.

## Summary

Implement filesystem-aligned internal Markdown links:

- Pages are linked with `.md`: `/docs/sync/trigger-matrix.md`.
- Sections are linked without `.md` and without trailing slash: `/docs/sync`.
- Accept trailing slash for section inputs, but canonical generated output omits it.
- New sections create `index.md`.
- Existing/imported sections use `index.md` first, then `README.md` as fallback.
- If both `index.md` and `README.md` exist, `index.md` is section default and `README.md` remains a separate page.
- Old extensionless page links are migration input only. Ingestion/import rewrites them when resolvable; unresolved legacy links remain and are validation errors.
- Preserve existing canonical addressing style during refactor: absolute stays absolute, relative stays relative.

Do not change the internal tree identity model unless required. `Page.Path` may remain route-like internally; canonical `.md` behavior belongs at Markdown, filesystem, API/UI boundary conversion points.

## Implementation

1. Add a shared canonical Markdown link resolver/formatter.

   Create a small shared package near existing core path/link code, for example `internal/core/markdownlinks`.

   It must:
   - Parse Markdown links with Goldmark-compatible behavior.
   - Handle inline links and reference-style link definitions.
   - Skip external URLs, `mailto:`, pure hash links, assets, images unless existing behavior intentionally indexes images, code spans, and fenced code.
   - Split href into path plus query/fragment and preserve query/fragment exactly.
   - Resolve absolute and relative paths using filesystem semantics from the source Markdown file path, not “page as folder” route semantics.
   - Classify targets as `page`, `section`, `asset`, `external`, `unresolved`, or `invalid`.
   - Format canonical hrefs:
     - page absolute: `/a/b.md`
     - page relative: `../b.md`
     - section absolute: `/a/b`
     - section relative: `../b`
   - Preserve percent-encoding style where possible; do exact case-sensitive matching.

2. Implement `index.md` / `README.md` section defaults.

   Update reconstruction in `internal/core/tree/node_store.go` and route helpers in `internal/core/tree/route_path.go`.

   Required behavior:
   - `index.md` is always the preferred section default.
   - `README.md` is section default only when sibling `index.md` is absent.
   - When both exist, `README.md` is a normal child page with slug `readme`.
   - Root follows the same rule.
   - New section creation still materializes `index.md`, never `README.md`.
   - Revision/file-kind mapping must classify `README.md` as section content only when it is the active fallback default for that directory.

3. Add automatic migration during filesystem ingestion.

   Hook after successful `ReconstructTreeFromFS` and before `captureWritebacksLocked` in workspace sync.

   Required behavior:
   - Raw incoming files are captured first.
   - Canonical link rewrites are captured as reconstruction/writeback changes in the same sync batch.
   - Validation runs after migration.
   - Rewritable old links are fixed on disk.
   - Unresolved old extensionless page links are not guessed; they remain in content and produce validation errors.
   - Migration must be idempotent.

4. Update validation and link indexing.

   Update `internal/core/markdownvalidation/use_cases.go` and `internal/links/*`.

   Required behavior:
   - Canonical page links with `.md` resolve correctly.
   - Canonical section links without `.md` resolve correctly.
   - `/section/` validates as accepted input but is reported or rewritten to `/section`.
   - `/section/index.md` and `/section/README.md` resolve to section only when that file is the active default.
   - `/section.md` means page file `section.md`; if no such page exists, it is broken.
   - Old `/page` page links are validation errors after migration if they could not be rewritten.
   - Link indexes store target route identity consistently, not duplicate `.md` and extensionless representations.

5. Update refactor behavior.

   Update `internal/links/link_refactor.go` and page refactor orchestration.

   Required behavior:
   - Rename/move preview detects canonical `.md` page links and section links.
   - Apply rewrites preserve canonical style:
     - `/a.md` renamed to `/b.md`
     - `./a.md` renamed to `./b.md`
     - `/section` moved to `/new-section`
   - Preserve query, fragment, link title, angle-bracket destinations, parentheses, and surrounding Markdown.
   - Do not rewrite links inside code.
   - Do not treat old extensionless page links as supported refactor inputs; they should have migrated earlier or remain validation errors.

6. Update importer output.

   Update `internal/importer/content_transformer.go` and importer tests.

   Required behavior:
   - Imported links to pages emit `.md`.
   - Imported links to sections emit extensionless section paths.
   - GitHub-style `README.md` imports become section defaults when no `index.md` exists.
   - Existing import source links in `.md`, extensionless route style, wiki-link style, relative style, and absolute style resolve to canonical output.
   - Assets keep asset hrefs and are not coerced to page links.

7. Update UI/API/MCP boundaries.

   Update preview, autocomplete, and route lookup surfaces.

   Required behavior:
   - Internal link autocomplete emits `/page.md` for pages and `/section` for sections.
   - Markdown preview resolves and navigates `.md` page links and section links.
   - Browser deep links may accept canonical `.md` page paths and canonical section paths.
   - MCP and HTTP docs/examples use canonical paths.
   - Core APIs should not expose two canonical representations for the same page. If an endpoint needs internal route paths for compatibility, document it explicitly as internal route input, not Markdown link format.

8. Documentation updates.

   Update docs and references after implementation:
   - `README.md`: document canonical page and section link formats.
   - Workspace sync docs: document automatic migration and validation errors.
   - Importer docs: document `README.md` fallback behavior.
   - MCP docs: update examples to `.md` page links.
   - Add or update a focused design note under `docs/` describing page vs section link rules, relative links, `index.md` / `README.md`, and migration policy.

## Gherkin Test Suite

Every scenario below must be backed by at least one automated test. Keep the
scenario title in the test name or as a nearby comment so the implementation can
be audited against this plan.

```gherkin
Feature: Shared canonical Markdown link resolver
  Scenario: Autocomplete inserts a canonical page link
    Given a page "docs/sync/trigger-matrix"
    When the user inserts an internal link from autocomplete
    Then the Markdown href is "/docs/sync/trigger-matrix.md"

  Scenario: Autocomplete inserts a canonical section link
    Given a section "docs/sync"
    When the user inserts an internal link from autocomplete
    Then the Markdown href is "/docs/sync"

  Scenario: Relative page links are resolved from the source file directory
    Given source file "docs/a/current.md"
    And target page file "docs/b/target.md"
    When the resolver formats a relative link from the source to the target
    Then the href is "../b/target.md"

  Scenario: Relative section links are resolved from the source file directory
    Given source file "docs/a/current.md"
    And target section folder "docs/b"
    When the resolver formats a relative link from the source to the section
    Then the href is "../b"

  Scenario: Section trailing slash is accepted but canonicalized away
    Given section "docs/sync" exists
    When Markdown contains "[Sync](/docs/sync/)"
    Then the canonical href is "/docs/sync"

  Scenario: Root section link remains slash
    Given the root section exists
    When a link targets the root section
    Then the canonical href is "/"

  Scenario: Query string and fragment are preserved byte-for-byte
    Given page "docs/b" exists
    When Markdown contains "[B](/docs/b?mode=raw&x=1#part-two)"
    Then the canonical href is "/docs/b.md?mode=raw&x=1#part-two"

  Scenario: Link title and angle-bracket destination syntax are preserved
    Given page "docs/b" exists
    When Markdown contains "[B](</docs/b> \"open B\")"
    Then Markdown becomes "[B](</docs/b.md> \"open B\")"

  Scenario: Parentheses in destinations do not corrupt the rewrite
    Given page "docs/topic-(draft)" exists
    When Markdown contains "[Draft](/docs/topic-(draft))"
    Then the canonical href is "/docs/topic-(draft).md"

  Scenario: Percent-encoded paths use exact filesystem matching
    Given page file "docs/space name.md" exists
    When Markdown contains "[Encoded](/docs/space%20name)"
    Then the canonical href is "/docs/space%20name.md"

  Scenario: External and non-page links are ignored
    Given Markdown contains "[Web](https://example.com) [Mail](mailto:a@example.com) [Local](#heading)"
    When canonical migration runs
    Then those hrefs are unchanged

  Scenario: Link-like text in inline code and fenced code is ignored
    Given Markdown contains inline code "`[B](/docs/b)`"
    And Markdown contains a fenced code block with "[B](/docs/b)"
    When canonical migration runs
    Then the code contents are unchanged

  Scenario: Malformed percent-encoding reports an invalid link instead of panicking
    Given Markdown contains "[Bad](/docs/%zz)"
    When validation runs
    Then validation reports an invalid internal link
    And no content is rewritten

  Scenario: Relative link cannot escape the workspace root
    Given source file "docs/a.md"
    When Markdown contains "[Outside](../../outside.md)"
    Then validation reports an invalid escaped internal link
    And no filesystem path outside the wiki root is read
```

```gherkin
Feature: Refactor preserves canonical link style
  Scenario: Existing canonical relative page links preserve relative style
    Given a page "docs/a.md" links to "./b.md"
    When page "docs/b" is renamed to "docs/c"
    Then the link becomes "./c.md"

  Scenario: Existing canonical absolute page links preserve absolute style
    Given a page links to "/docs/b.md"
    When page "docs/b" is renamed to "docs/c"
    Then the link becomes "/docs/c.md"

  Scenario: Section move keeps section links extensionless
    Given a page links to "/docs/sync"
    When section "docs/sync" is moved to "docs/archive/sync"
    Then the link becomes "/docs/archive/sync"

  Scenario: Page and section with the same basename are not cross-rewritten
    Given page "docs/sync" exists as file "docs/sync.md"
    And section "docs/sync" exists as folder "docs/sync/index.md"
    And content links to "/docs/sync.md" and "/docs/sync"
    When the page file is renamed to "docs/sync-page"
    Then only "/docs/sync.md" is rewritten
    And "/docs/sync" still targets the section

  Scenario: Broken non-canonical links are not silently rewritten by refactor
    Given a page links to unresolved "/docs/missing"
    When another page is renamed
    Then the unresolved href remains "/docs/missing"
    And validation still reports the unresolved non-canonical link

  Scenario: Source page move recalculates relative links without changing absolute links
    Given "docs/a/source.md" links to "../b/target.md" and "/docs/b/target.md"
    When "docs/a/source" is moved to "archive/source"
    Then the relative link is recalculated from "archive/source.md"
    And the absolute link remains "/docs/b/target.md"

  Scenario: Refactor preview reports conflicts without mutating content
    Given a refactor preview detects a stale page version or path conflict
    When apply is attempted
    Then no Markdown content is rewritten
    And the caller receives a conflict error
```

```gherkin
Feature: Filesystem section defaults
  Scenario: New section creates index.md
    When a user creates section "docs/sync"
    Then the filesystem contains "docs/sync/index.md"

  Scenario: index.md has precedence over README.md
    Given "docs/sync/index.md" and "docs/sync/README.md" exist
    When the tree is reconstructed
    Then "/docs/sync" opens "docs/sync/index.md"
    And "README.md" appears as a separate page in the section

  Scenario: README.md is fallback section default
    Given "docs/sync/README.md" exists
    And "docs/sync/index.md" does not exist
    When the tree is reconstructed
    Then "/docs/sync" opens "docs/sync/README.md"

  Scenario: root README.md is fallback only without root index.md
    Given root "README.md" exists
    And root "index.md" does not exist
    When the tree is reconstructed
    Then "/" opens "README.md"

  Scenario: root index.md has precedence over root README.md
    Given root "index.md" and root "README.md" both exist
    When the tree is reconstructed
    Then "/" opens "index.md"
    And root "README.md" is shown as a separate page

  Scenario: README.md as normal page keeps its filesystem casing in generated links
    Given "docs/sync/index.md" and "docs/sync/README.md" exist
    When LeafWiki generates a link to the README page
    Then the href ends with "/docs/sync/README.md"

  Scenario: Uppercase INDEX.MD does not become a second child page when accepted by current index lookup rules
    Given "docs/sync/INDEX.MD" exists
    When the tree is reconstructed
    Then reconstruction follows the existing section-index casing rule
    And no duplicate "index" child page appears
```

```gherkin
Feature: Ingestion migration
  Scenario: Old extensionless page link migrates to .md
    Given "docs/a.md" contains "[B](/docs/b)"
    And page "docs/b" exists
    When workspace sync ingests the file
    Then "docs/a.md" contains "[B](/docs/b.md)"
    And validation has no error for that link

  Scenario: Old extensionless section link remains extensionless
    Given "docs/a.md" contains "[Sync](/docs/sync/)"
    And section "docs/sync" exists
    When workspace sync ingests the file
    Then "docs/a.md" contains "[Sync](/docs/sync)"

  Scenario: Unresolved old extensionless page link becomes validation error
    Given "docs/a.md" contains "[Missing](/docs/missing)"
    And no page or section resolves to "/docs/missing"
    When workspace sync ingests the file
    Then the file is not guessed or rewritten
    And validation reports a broken non-canonical internal link

  Scenario: Query and fragment survive migration
    Given "docs/a.md" contains "[B](/docs/b?x=1#part)"
    And page "docs/b" exists
    When workspace sync ingests the file
    Then "docs/a.md" contains "[B](/docs/b.md?x=1#part)"

  Scenario: Relative old page link migrates to relative .md
    Given source file "docs/a/current.md" contains "[B](../b)"
    And page file "docs/b.md" exists
    When workspace sync ingests the file
    Then "docs/a/current.md" contains "[B](../b.md)"

  Scenario: Existing canonical .md page link is not rewritten
    Given "docs/a.md" contains "[B](/docs/b.md)"
    And page "docs/b" exists
    When workspace sync ingests the file
    Then "docs/a.md" is unchanged

  Scenario: Ambiguous extensionless link is left as validation error
    Given page file "docs/sync.md" exists
    And section folder "docs/sync/index.md" exists
    And "docs/a.md" contains "[Sync](/docs/sync)"
    When workspace sync ingests the file
    Then the href remains "/docs/sync"
    And validation reports an ambiguous legacy link

  Scenario: Explicit index.md section link canonicalizes to the section
    Given section folder "docs/sync/index.md" exists
    And "docs/a.md" contains "[Sync](/docs/sync/index.md)"
    When workspace sync ingests the file
    Then "docs/a.md" contains "[Sync](/docs/sync)"

  Scenario: Explicit README.md section fallback link canonicalizes to the section
    Given section folder "docs/sync/README.md" exists
    And "docs/sync/index.md" does not exist
    And "docs/a.md" contains "[Sync](/docs/sync/README.md)"
    When workspace sync ingests the file
    Then "docs/a.md" contains "[Sync](/docs/sync)"

  Scenario: Explicit README.md page link stays a page when index.md exists
    Given section folder "docs/sync/index.md" exists
    And page file "docs/sync/README.md" exists
    And "docs/a.md" contains "[Readme](/docs/sync/README.md)"
    When workspace sync ingests the file
    Then the href remains "/docs/sync/README.md"

  Scenario: Migration is idempotent
    Given workspace sync already rewrote all resolvable legacy links
    When workspace sync runs again with no file changes
    Then no Markdown file changes are written
    And no extra writeback revision is created

  Scenario: Migration writeback is captured in revision history
    Given "docs/a.md" contains a resolvable old extensionless page link
    When workspace sync ingests the file
    Then revision history contains the raw incoming content
    And revision history contains the automatic canonicalization writeback

  Scenario: Migration write failure reports sync validation state without losing raw content
    Given the filesystem rejects writing a migrated Markdown file
    When workspace sync ingests the file
    Then the raw content remains on disk
    And sync status reports the migration failure
    And derived indexes are not rebuilt from partially migrated content
```

```gherkin
Feature: Validation and indexing
  Scenario: Canonical .md page link indexes as outgoing link
    Given "docs/a.md" links to "/docs/b.md"
    When links are indexed
    Then page "docs/a" has an outgoing link to page "docs/b"

  Scenario: Canonical section link indexes as outgoing link
    Given "docs/a.md" links to "/docs/sync"
    When links are indexed
    Then page "docs/a" has an outgoing link to section "docs/sync"

  Scenario: Case mismatch is invalid
    Given page "docs/Sync.md" exists
    And "docs/a.md" links to "/docs/sync.md"
    When validation runs
    Then validation reports a broken link

  Scenario: Assets are not coerced
    Given "docs/a.md" links to "/assets/logo.md"
    When migration and validation run
    Then the href remains "/assets/logo.md"
    And it is treated according to existing asset rules

  Scenario: Broken canonical .md page link is reported as broken
    Given "docs/a.md" links to "/docs/missing.md"
    When validation runs
    Then validation reports a broken page link

  Scenario: Old extensionless page link is reported as non-canonical when migration cannot resolve it
    Given "docs/a.md" links to "/docs/missing"
    When validation runs after ingestion
    Then validation reports a non-canonical unresolved page link

  Scenario: Duplicate syntaxes do not create duplicate target identities after migration
    Given "docs/a.md" originally linked to "/docs/b" and "/docs/b.md"
    And page "docs/b" exists
    When migration and link indexing run
    Then both Markdown links are canonical ".md" page links
    And the link index stores the same target identity for both links

  Scenario: Reference-style link definitions are rewritten
    Given Markdown contains "[B][b-ref]" and "[b-ref]: /docs/b"
    And page "docs/b" exists
    When migration runs
    Then the reference definition is rewritten to "[b-ref]: /docs/b.md"
    And the rendered link still points to page "docs/b"

  Scenario: Image links remain governed by existing image and asset validation
    Given Markdown contains "![Alt](/docs/b.md)"
    When validation runs
    Then the link is not indexed as a page backlink unless existing behavior already indexes images
    And image validation behavior is covered by an explicit assertion
```

```gherkin
Feature: Importer canonicalization
  Scenario: GitHub .md page links remain GitHub-compatible
    Given an imported file links to "./guide.md"
    When import completes
    Then the created LeafWiki page contains a link ending in ".md"

  Scenario: GitHub README section import becomes section link
    Given an imported folder contains "README.md"
    And no "index.md"
    When import completes
    Then links to that folder are emitted without ".md"

  Scenario: Importer does not rewrite code examples
    Given an imported Markdown file contains "`[x](/old/link)`"
    When import completes
    Then the inline code text is unchanged

  Scenario: Importer migrates old route-style page link to .md
    Given an imported file links to "/Reference/Endpoints"
    And the import plan maps that source file to page "reference/endpoints"
    When import completes
    Then the created page contains "/reference/endpoints.md"

  Scenario: Importer leaves unresolved internal links as validation errors
    Given an imported file links to "/Missing/Page"
    And no imported or existing page resolves that target
    When import completes
    Then the href is not guessed
    And validation reports the unresolved link

  Scenario: Importer distinguishes folder README section from README child page
    Given an imported folder has both "Guide/index.md" and "Guide/README.md"
    When import completes
    Then links to "Guide/" target the section
    And links to "Guide/README.md" target the README page

  Scenario: Importer preserves query and fragment while canonicalizing
    Given an imported file links to "./guide?from=zip#install"
    And "./guide.md" resolves to an imported page
    When import completes
    Then the created link is "./guide.md?from=zip#install" or the equivalent canonical absolute page href

  Scenario: Importer does not coerce assets with .md extension under asset namespaces
    Given an imported file links to "/assets/policy.md"
    When import completes
    Then the asset href is preserved according to asset import rules
```

```gherkin
Feature: E2E workflows
  Scenario: User can click a canonical page link in preview
    Given a running LeafWiki app with page "/docs/a"
    And page "/docs/a" contains "[B](/docs/b.md)"
    When the user opens preview and clicks "B"
    Then the app navigates to page "docs/b"

  Scenario: Workspace sync repairs links and shows validation errors for the rest
    Given synced Markdown contains one resolvable old page link and one missing old page link
    When the user runs workspace sync
    Then the resolvable link is rewritten with ".md"
    And the missing link is visible as a validation error

  Scenario: MCP agent context returns canonical examples
    Given the MCP server is running
    When an agent requests wiki context or validation output
    Then page links in examples and returned Markdown use ".md"
    And section links do not use ".md"

  Scenario: User can click a canonical section link in preview
    Given a running LeafWiki app with page "/docs/a"
    And page "/docs/a" contains "[Sync](/docs/sync)"
    When the user opens preview and clicks "Sync"
    Then the app navigates to section "docs/sync"

  Scenario: Preview shows a broken-link state for unresolved canonical page links
    Given page "/docs/a" contains "[Missing](/docs/missing.md)"
    When the user opens preview
    Then the link is not treated as an existing internal page
    And the broken link is visible in link status or validation UI

  Scenario: Direct browser route opens canonical .md page deep link
    Given page "docs/b" exists
    When the browser opens "/docs/b.md"
    Then LeafWiki displays page "docs/b"

  Scenario: Direct browser route does not alias old extensionless page path
    Given page file "docs/b.md" exists
    And no section folder "docs/b" exists
    When the browser opens "/docs/b"
    Then LeafWiki does not silently open page "docs/b" as an old alias

  Scenario: Direct browser route canonicalizes section trailing slash
    Given section "docs/sync" exists
    When the browser opens "/docs/sync/"
    Then LeafWiki displays section "docs/sync"
    And the visible route is canonical or all generated links use "/docs/sync"

  Scenario: Exact-case mismatch is visible to the user
    Given page file "docs/Sync.md" exists
    When preview or browser navigation targets "/docs/sync.md"
    Then LeafWiki does not silently open "docs/Sync"
    And validation or navigation reports a not-found condition

  Scenario: Workspace sync UI shows both automatic repairs and remaining errors
    Given workspace sync migrates one legacy link
    And another legacy link remains unresolved
    When the user opens workspace sync status
    Then the repaired file path is visible in recent sync changes or revision history
    And the unresolved link appears as a validation error

  Scenario: Importer UI creates GitHub-compatible links from a README-based zip
    Given the user uploads a zip with folder README defaults and page links
    When the import plan is executed from the UI
    Then imported page links end in ".md"
    And imported section links omit ".md"

  Scenario: MCP validation reports canonical and non-canonical links consistently
    Given MCP validation runs against a workspace with ".md", section, unresolved legacy, and asset links
    When the client calls the validation tool
    Then canonical links pass
    And unresolved legacy links fail with stable issue codes
    And asset links follow existing asset validation

  Scenario: MCP refactor preview and apply preserve canonical page and section syntax
    Given MCP refactor is enabled
    And a page contains "/docs/b.md" and "/docs/sync"
    When an MCP client previews and applies a page rename and section move
    Then page links keep ".md"
    And section links stay extensionless

  Scenario: Separate root-dir mode migrates content in root dir only
    Given LeafWiki runs with a separate root dir
    And root-dir Markdown contains a resolvable old page link
    When workspace sync runs
    Then the root-dir file is rewritten with ".md"
    And no data-dir-only files are treated as wiki content
```

## Concrete Test Targets

Add focused Go tests first, then implement until green. The test names should
map back to the scenario titles above.

- `internal/core/tree/route_path_test.go`
- `internal/core/markdownlinks/markdownlinks_test.go`
- `internal/core/tree/node_store_reconstruct_test.go`
- `internal/core/tree/node_store_test.go`
- `internal/core/tree/tree_service_test.go`
- `internal/core/markdownvalidation/use_cases_test.go`
- `internal/links/link_service_test.go`
- `internal/links/link_refactor_test.go`
- `internal/links/link_refactor_replace_test.go`
- `internal/links/link_refactor_ast_test.go`
- `internal/wiki/pages/pages_test.go`
- `internal/importer/content_transformer_test.go`
- `internal/importer/executor_test.go`
- `internal/importer/importer_integration_test.go`
- `internal/workspacesync/service_test.go`
- `internal/wiki/mcp/mcp_integration_test.go`
- `internal/wiki/mcp/tools_validation_test.go`
- `internal/wiki/mcp/tools_context_test.go`
- `internal/http/router_test.go`

Required backend test inventory:

- `internal/core/markdownlinks/markdownlinks_test.go`
  - `TestResolveCanonicalLink_UsesFilesystemRelativeSemanticsNotPageAsFolder`
  - `TestResolveCanonicalLink_ClassifiesPageSectionAssetExternalInvalidAndUnresolved`
  - `TestCanonicalizeMarkdownLinks_PreservesAngleDestinationsTitleQueryAndFragment`
  - `TestCanonicalizeMarkdownLinks_RewritesReferenceDefinitionsAndSkipsCode`
  - `TestCanonicalizeMarkdownLinks_DoesNotRewriteEscapedLiteralLinks`
  - `TestCanonicalizeMarkdownLinks_DoesNotRewriteImageOnlyReferenceDefinitions`
  - `TestCanonicalizeMarkdownLinks_RewritesNestedListLinks`
- `internal/core/tree/node_store_reconstruct_test.go`
  - `TestNodeStore_ReconstructTreeFromFS_ReadmeFallbackSectionWhenNoIndexExists`
  - `TestNodeStore_ReconstructTreeFromFS_IndexBeatsReadmeAndReadmeIsSeparatePage`
  - `TestNodeStore_ReconstructTreeFromFS_RootIndexBeatsRootReadme`
- `internal/core/tree/node_store_test.go`
  - `TestNodeStore_ReadPageRaw_Section_NoIndex_ReturnsEmptyNil`
- `internal/core/tree/tree_service_test.go`
  - `TestTreeService_CreateNode_Section_CreatesIndexWithFrontmatter`
- `internal/workspacesync/service_test.go`
  - `TestServiceSyncNowRewritesResolvableLegacyPageLinkBeforeValidation`
  - `TestServiceSyncNowCanonicalMigrationSecondRunCreatesNoNewRevision`
  - `TestServiceSyncNowLeavesUnresolvedLegacyPageLinkAndReportsValidationError`
  - `TestServiceSyncNowCanonicalizesSectionTrailingSlashWithoutRevisionLoop`
  - `TestServiceSyncNowRelativeLegacyPageLinkMigratesAndCanonicalRelativeLinkStaysCanonical`
  - `TestServiceSyncNowMigratedDuplicateSyntaxesIndexAsSinglePageTargetIdentity`
  - `TestServiceSyncNowKeepsRawAndCanonicalMigrationPageRevisions`
  - `TestServiceListPageRevisionsNormalizesReadmeFallbackSectionPath`
- `internal/core/markdownvalidation/use_cases_test.go`
  - `TestValidateWorkspaceMarkdownFiles_ResolvesCanonicalPageMdAndSectionLinks`
  - `TestValidateWorkspaceMarkdownFiles_RejectsUnmigratedExtensionlessPageLink`
  - `TestValidateWorkspaceMarkdownFiles_DistinguishesSectionMdPageFromSectionDefault`
  - `TestValidateWorkspaceMarkdownFiles_UsesExactCaseSensitiveTargetMatching`
- `internal/links/link_service_test.go`
  - `TestLinkService_IndexAllPages_PreservesSamePathPageAndSectionTargets`
  - `TestLinkService_IndexAllPages_IgnoresAssetLinksInOutgoingAndBrokenSets`
- `internal/links/link_refactor_test.go`
  - `TestMarkdownRefactorEngine_RewriteCanonicalPageLinksKeepsMdAbsoluteAndRelative`
  - `TestMarkdownRefactorEngine_DoesNotRewriteLegacyExtensionlessPageLinks`
- `internal/links/link_refactor_replace_test.go`
  - `TestMarkdownRefactorEngine_RewriteRelativeLinksForPathChange_PreservesCanonicalPageMdFromMovedSource`
  - `TestMarkdownRefactorEngine_RewriteRelativeLinksForPathChange_DoesNotRewriteEscapedPseudoLinks`
- `internal/wiki/pages/pages_test.go`
  - `TestPreviewPageRefactorUseCase_RenameDoesNotListNonCanonicalExtensionlessPageLink`
  - `TestApplyPageRefactorUseCase_RenameDoesNotRewriteNonCanonicalExtensionlessPageLink`
  - `TestApplyPageRefactorUseCase_RenameRewritesIncomingLinks`
  - `TestApplyPageRefactorUseCase_StaleVersionDoesNotRewriteIncomingLinks`
  - `TestApplyPageRefactorUseCase_TargetConflictDoesNotRewriteIncomingLinks`
- `internal/importer/planner_test.go`
  - `TestPlanner_CreatePlan_ReadmeMdFallbackSectionWhenNoIndex`
  - `TestPlanner_CreatePlan_NonExactReadmeMdImportsAsPage`
  - `TestPlanner_CreatePlan_IndexMdCaseInsensitiveBeatsReadmeFallback`
- `internal/importer/content_transformer_test.go`
  - `TestContentTransformer_EmitsMdForPagesAndExtensionlessForSections`
  - `TestContentTransformer_SourcePathMatchingIsCaseSensitive`
  - `TestContentTransformer_BasenameWikiLinkMatchingIsCaseSensitive`
  - `TestContentTransformer_FormatsSameRoutePageAndSectionTargetsByMatchedKind`
  - `TestContentTransformer_FormatsSameRouteSuffixFallbackByRequestedKind`
- `internal/importer/executor_test.go`
  - `TestExecutor_Create_RewritesMarkdownAndWikiLinksToImportedPages`
- `internal/importer/importer_integration_test.go`
  - `TestImporterService_ExecuteCurrentPlan_RewritesLinksAndUploadsAssetsToDisk`
  - `TestImporterService_ExecuteCurrentPlan_ImportsLeafWikiNestedFixture`
- `internal/http/router_test.go`
  - `TestGetPageByPathEndpoint_KindDistinguishesSameBasenamePageAndSection`
  - `TestGetPageByPathEndpoint_ReadmeMarkdownPathUsesFallbackOnlyWhenActive`
  - `TestGetTreeEndpoint_ContentPathUsesCaseInsensitiveIndexPrecedence`
- `internal/wiki/mcp/tools_context_test.go`
  - `TestPageIDsForMarkdownPathsUsesMarkdownFileKindForSameBasenameTwins`
  - `TestPageIDsForMarkdownPathsResolvesReadmeFallbackSection`
- `internal/wiki/mcp/mcp_integration_test.go`
  - `TestLocalMCPGetContext_ReturnsAgentReadyContext`
  - `TestLocalMCPPathToolsResolveCanonicalSameBasenameTwins`
  - `TestLocalMCPPathToolsResolveReadmeFallbackMarkdownPath`

Minimum backend coverage by subsystem:

- Shared resolver: absolute/relative page and section formatting, query/fragment preservation, malformed percent encoding, workspace escape, external/hash/mailto skips, code-block skips, exact-case mismatch.
- Tree/reconstruction: root and nested `index.md`/`README.md` precedence, both-files behavior, new-section `index.md` creation, revision file-kind mapping for fallback README defaults.
- Workspace sync: raw capture before repair, repair writeback capture, validation after repair, unresolved and ambiguous legacy links, idempotent second sync, write failure behavior.
- Link indexing: `.md` page links resolve, section links resolve, broken canonical `.md` links report broken, no duplicate target identities, asset/image behavior remains explicit.
- Refactor: absolute and relative page links, section links, same-basename page/section collision, query/fragment/title preservation, stale/conflict no-write path.
- Importer: `.md`, extensionless, folder, `index.md`, `README.md`, wiki-link, unresolved, asset, code-block, query/fragment cases.
- HTTP/MCP: canonical deep-link route lookup, validation result shape, refactor preview/apply parity, context/examples documentation strings.

Add or extend E2E coverage:

- `e2e/tests/page.spec.ts`
- `e2e/tests/importer.spec.ts`
- `e2e/tests/workspace-sync.spec.ts`
- `e2e/tests/root-dir.spec.ts`
- `e2e/tests/mcp-agent-context.spec.ts`
- `e2e/tests/mcp-safe-edits.spec.ts`

Required E2E test inventory:

- `e2e/tests/page.spec.ts`
  - `preview-clicks-canonical-absolute-page-link-with-query-fragment`
  - `preview-clicks-canonical-relative-page-link-from-nested-page`
  - `preview-clicks-section-link-with-trailing-slash-and-canonicalizes-url`
  - `direct-browser-deep-link-page-md-opens-viewer-page`
  - `direct-browser-deep-link-page-md-opens-editor-page`
  - `direct-browser-deep-link-case-mismatch-shows-not-found`
  - `preview-shows-broken-state-for-unresolved-canonical-page-md-link`
  - `preview-create-missing-page-link-creates-page`
  - `preview-create-missing-section-link-creates-section`
  - `create-section-on-extensionless-not-found-route`
  - `create-section-on-not-found-route-when-page-twin-exists`
  - `create-page-on-not-found-md-route-when-section-twin-exists`
  - `autocomplete-emits-section-links-without-md`
  - `link-insert-dialog-emits-page-md-links-but-section-links-without-md`
- `e2e/tests/workspace-sync.spec.ts`
  - `workspace-sync-rewrites-resolvable-legacy-page-link-and-shows-no-validation-error`
  - `workspace-sync-repairs-link-and-keeps-remaining-validation-error-in-same-sync`
  - `workspace-sync-leaves-unresolved-extensionless-link-and-shows-validation-banner`
  - `workspace-sync-sync-now-clears-validation-banner-after-link-is-fixed`
  - `workspace-sync-preserves-query-fragment-and-leaves-assets-code-blocks-unchanged`
- `e2e/tests/importer.spec.ts`
  - `importer-ui-canonical-page-links-navigate-in-preview`
  - `importer-ui-readme-only-folder-imports-as-section`
  - `importer-ui-unresolved-extensionless-page-link-surfaces-validation-error`
- `e2e/tests/root-dir.spec.ts`
  - `separate-root-workspace-sync-rewrites-links-only-inside-configured-root`
  - `separate-root-importer-writes-canonical-links-outside-data-dir`
  - `separate-root-readme-fallback-and-index-precedence-match-default-root`
- `e2e/tests/mcp-agent-context.spec.ts`
  - `mcp-refresh-exposes-canonical-link-validation-in-browser-tree`
  - `mcp-validate-content-reports-canonical-legacy-and-asset-links-consistently`
- `e2e/tests/mcp-safe-edits.spec.ts`
  - `safe edit tools patch sections and metadata with version checks`

Minimum E2E coverage:

- Preview clicks for canonical `.md` page links and extensionless section links.
- Preview/not-found behavior for broken canonical `.md` links.
- Autocomplete emits `.md` for pages and no `.md` for sections.
- Browser opens canonical `.md` page deep links.
- Browser does not silently alias old extensionless page paths.
- Browser accepts or normalizes section trailing slash.
- Workspace sync UI shows automatic repair plus unresolved validation error.
- Workspace sync revision/history UI exposes the automatic repair writeback.
- Importer UI proves GitHub-style README defaults and canonical generated links.
- MCP validation reports canonical pass and legacy unresolved fail.
- MCP refactor preview/apply preserves `.md` page links and extensionless section links.
- Separate root-dir E2E proves migration writes wiki content under root dir only.

Verification commands:

```bash
rtk go test ./internal/core/tree ./internal/core/markdownvalidation ./internal/links ./internal/importer ./internal/workspacesync ./internal/wiki/pages ./internal/wiki/mcp
rtk go test ./internal/http ./internal/wiki/...
rtk env E2E_RUN_MODE=local ./e2e/run.sh e2e/tests/page.spec.ts
rtk env E2E_RUN_MODE=local ./e2e/run.sh e2e/tests/history.spec.ts
rtk env E2E_RUN_MODE=local E2E_ENABLE_WORKSPACE_SYNC=1 ./e2e/run.sh e2e/tests/importer.spec.ts
rtk env E2E_RUN_MODE=local E2E_ENABLE_WORKSPACE_SYNC=1 ./e2e/run.sh e2e/tests/workspace-sync.spec.ts
rtk env E2E_RUN_MODE=local E2E_ENABLE_SEPARATE_ROOT_DIR=1 E2E_ENABLE_WORKSPACE_SYNC=1 ./e2e/run.sh e2e/tests/root-dir.spec.ts
rtk env E2E_RUN_MODE=local E2E_ENABLE_MCP_LOCAL=1 E2E_ENABLE_WORKSPACE_SYNC=1 ./e2e/run.sh e2e/tests/mcp-agent-context.spec.ts
rtk env E2E_RUN_MODE=local E2E_ENABLE_MCP_LOCAL=1 E2E_ENABLE_WORKSPACE_SYNC=1 ./e2e/run.sh e2e/tests/mcp-safe-edits.spec.ts
rtk npm --prefix ui/leafwiki-ui run lint
rtk npm --prefix ui/leafwiki-ui run build
rtk npm --prefix e2e run lint
rtk npm --prefix e2e run format:check
rtk make test
rtk git diff --check
rtk git diff --cached --check
```

## Definition Of Done

- The plan is saved at `plans/canonical_markdown_links.PLAN.md` with the reference thread link.
- All new behavior is covered by failing tests before implementation.
- Every Gherkin scenario in this plan is mapped to at least one automated test, or the implementation PR explicitly justifies why a scenario was removed.
- Page links generated by LeafWiki end in `.md`.
- Section links generated by LeafWiki omit `.md` and omit trailing slash.
- `index.md` / `README.md` section default behavior matches the agreed precedence.
- Workspace ingestion automatically migrates resolvable old links and records the repair through the existing revision/writeback flow.
- Unresolvable old links are validation errors, not silently supported.
- Ambiguous old extensionless links are validation errors, not guessed.
- Migration is idempotent and does not create repeat writeback revisions on unchanged content.
- Case mismatches, malformed paths, workspace escapes, and broken canonical `.md` links produce deterministic validation/navigation failures.
- Importer, refactor, validation, link indexing, preview, autocomplete, HTTP/MCP examples, and E2E flows agree on one canonical model.
- Documentation explains canonical links, relative links, section defaults, migration, and validation failure behavior.
- All targeted Go tests, E2E tests, UI lint/build, E2E lint/format, `rtk make test`, and `rtk git diff --check` pass.
- No known managed Markdown content produced by tests contains an extensionless page link unless it is intentionally asserting a validation error.

## Assumptions

- Exact case-sensitive filesystem matching is required.
- Heading existence is not validated in this feature; fragments are preserved only.
- Non-Markdown assets are outside canonical page/section link rewriting.
- Markdown files under known asset namespaces remain asset references unless existing asset logic says otherwise.
- The implementation keeps one core route identity per page/section and converts at boundaries instead of storing duplicate page identities.
