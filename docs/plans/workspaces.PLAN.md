<!-- leafwiki
version: 1
page:
  id: oa2auSavR
  title: LeafWiki Workspace Boundary and `--root-dir` Implementation Plan
  created_at: "2026-06-15T05:44:44.860692677Z"
  updated_at: "2026-06-15T05:44:44.860692677Z"
  creator_id: system
  last_author_id: system
-->

# LeafWiki Workspace Boundary and `--root-dir` Implementation Plan

## Summary

Add a single-workspace storage boundary that separates app state from managed markdown content while preserving existing installs.

Reference thread: `codex://threads/019e72e5-d979-7632-b5c3-0ea784c6c487`

Goal:
- Introduce `wiki.Workspace{ID, DataDir, RootDir}`.
- Add `--root-dir` and `LEAFWIKI_ROOT_DIR`.
- Keep default behavior unchanged: `RootDir = <data-dir>/root`.
- Keep app state in `DataDir`.
- Store markdown pages, section folders, and `.order.json` in `RootDir`.
- Do not implement multi-project routing, per-user roots, multiple active workspaces, or non-filesystem backends in this phase.

## Interface and Implementation Changes

Add:

```go
type Workspace struct {
    ID      string
    DataDir string
    RootDir string
}

type TreeOptions struct {
    DataDir string // schema.json and legacy tree.json migration state
    RootDir string // markdown pages, section dirs, .order.json
}
```

Keep compatibility:
- `wiki.WikiOptions.StorageDir` remains supported and maps to `Workspace.DataDir`.
- `tree.NewTreeService(dataDir string)` remains supported and maps to `TreeOptions{DataDir: dataDir, RootDir: filepath.Join(dataDir, "root")}`.
- Add `NewTreeServiceWithOptions(TreeOptions)` for explicit split storage.
- Add `Wiki.Workspace()` and `Wiki.GetRootDir()`; keep `GetStorageDir()` returning `DataDir`.

CLI:
- Add `--root-dir` and `LEAFWIKI_ROOT_DIR`.
- Resolve root dir after data dir.
- If root dir is unset, use `filepath.Join(dataDir, "root")`.
- Create both dirs before `wiki.NewWiki`.
- Keep `reset-admin-password` using only `DataDir`.

Validation:
- Reject empty `DataDir` or `RootDir`.
- Reject `RootDir == DataDir` after clean/abs resolution.
- Allow `RootDir` outside `DataDir`.
- Do not move existing content automatically.

Storage split:
- `DataDir`: users, sessions, search DB, links DB, tags/properties DBs, assets, revisions, branding, importer state, custom stylesheet.
- `RootDir`: markdown pages, section directories, section `index.md`, `.order.json`.

Tree behavior:
- `NodeStore.ReconstructTreeFromFS()` scans `RootDir` directly.
- Root node disk path is `RootDir`.
- Child node disk paths join `RootDir` with the logical route path excluding the internal `root` slug.
- Schema and legacy `tree.json` migration state remain under `DataDir`.

Documentation:
- Update CLI usage, README flag/env tables, Docker examples, operations notes, `install.sh`, and install docs.
- Add a “Workspace storage layout” section explaining `DataDir` vs `RootDir`.
- State clearly that `RootDir` is LeafWiki-managed and writable, not a passive arbitrary-folder viewer.

## Gherkin Test Suite

```gherkin
Feature: Workspace defaults preserve existing installs

  Scenario: Default workspace uses data root as content root
    Given LeafWiki is configured with data dir "/tmp/leafwiki-data"
    And no root dir is configured
    When the workspace is resolved
    Then the workspace id is "default"
    And the workspace data dir is "/tmp/leafwiki-data"
    And the workspace root dir is "/tmp/leafwiki-data/root"

  Scenario: Existing tree constructor keeps old layout
    Given a tree service is created with data dir "/tmp/leafwiki-data"
    When a page "welcome" is created under the logical root
    Then the page markdown is written to "/tmp/leafwiki-data/root/welcome.md"
    And "/tmp/leafwiki-data/root/root/welcome.md" does not exist
```

```gherkin
Feature: CLI root dir configuration

  Scenario: Root dir defaults from data dir
    Given CLI args include "--data-dir=/tmp/data"
    And no LEAFWIKI_ROOT_DIR is set
    When CLI configuration is resolved
    Then DataDir is "/tmp/data"
    And RootDir is "/tmp/data/root"

  Scenario: Environment root dir overrides default
    Given LEAFWIKI_DATA_DIR is "/tmp/data"
    And LEAFWIKI_ROOT_DIR is "/tmp/content"
    When CLI configuration is resolved
    Then DataDir is "/tmp/data"
    And RootDir is "/tmp/content"

  Scenario: CLI root dir overrides environment
    Given LEAFWIKI_DATA_DIR is "/tmp/data"
    And LEAFWIKI_ROOT_DIR is "/tmp/env-content"
    And CLI args include "--root-dir=/tmp/cli-content"
    When CLI configuration is resolved
    Then RootDir is "/tmp/cli-content"

  Scenario: Root dir equal to data dir is rejected
    Given CLI args include "--data-dir=/tmp/wiki" and "--root-dir=/tmp/wiki"
    When CLI configuration is validated
    Then validation fails
    And the error mentions that root dir must differ from data dir

  Scenario: Password reset ignores root dir
    Given DataDir contains the LeafWiki users database
    And RootDir points to a separate markdown directory
    When "reset-admin-password" runs
    Then it updates the admin password in DataDir
    And it does not read or write RootDir
```

```gherkin
Feature: Tree content storage with explicit root dir

  Scenario: Page writes go directly under root dir
    Given TreeOptions has DataDir "/tmp/state" and RootDir "/tmp/content"
    When a page "notes" is created under the logical root
    Then "/tmp/content/notes.md" exists
    And "/tmp/state/root/notes.md" does not exist
    And "/tmp/content/root/notes.md" does not exist

  Scenario: Section writes go directly under root dir
    Given TreeOptions has DataDir "/tmp/state" and RootDir "/tmp/content"
    When a section "docs" is created under the logical root
    Then "/tmp/content/docs/index.md" exists
    And "/tmp/content/docs/.order.json" may be created when children are ordered
    And "/tmp/state/root/docs/index.md" does not exist

  Scenario: Root child order is stored in root dir
    Given TreeOptions has DataDir "/tmp/state" and RootDir "/tmp/content"
    And pages "b" and "a" exist under root
    When root children are sorted as "a", "b"
    Then "/tmp/content/.order.json" exists
    And it records the ordered page ids
    And "/tmp/state/root/.order.json" does not exist

  Scenario: Reconstruct reads existing root dir content
    Given RootDir contains "intro.md"
    And RootDir contains "docs/index.md"
    And DataDir is empty except for app state
    When the tree is reconstructed
    Then the logical root has child page "intro"
    And the logical root has child section "docs"
    And no "root" child is synthesized from the filesystem path

  Scenario: Schema remains in data dir
    Given TreeOptions has DataDir "/tmp/state" and RootDir "/tmp/content"
    When the tree is reconstructed
    Then "/tmp/state/schema.json" exists
    And "/tmp/content/schema.json" does not exist
```

```gherkin
Feature: Legacy tree migrations with split storage

  Scenario: Legacy schema state is read from data dir
    Given DataDir contains "schema.json" with an old version
    And DataDir contains legacy "tree.json"
    And RootDir contains old markdown content
    When the tree service loads
    Then migration reads schema and tree snapshot from DataDir
    And migrated markdown/frontmatter/order files are written under RootDir
    And the new schema version is saved in DataDir

  Scenario: Child order migration writes order files to root dir
    Given a legacy tree snapshot with ordered root children
    And TreeOptions has separate DataDir and RootDir
    When migration to the current schema runs
    Then RootDir contains ".order.json"
    And DataDir does not contain "root/.order.json" unless RootDir is the default DataDir/root
```

```gherkin
Feature: Wiki service storage boundary

  Scenario: Default wiki writes welcome page in existing location
    Given WikiOptions has StorageDir "/tmp/data"
    And no explicit Workspace
    When NewWiki initializes an empty wiki
    Then "/tmp/data/root/welcome-to-leafwiki.md" exists

  Scenario: Explicit workspace writes welcome page in root dir
    Given WikiOptions has Workspace DataDir "/tmp/state" and RootDir "/tmp/content"
    When NewWiki initializes an empty wiki
    Then "/tmp/content/welcome-to-leafwiki.md" exists
    And "/tmp/state/root/welcome-to-leafwiki.md" does not exist

  Scenario: App state remains in data dir
    Given WikiOptions has Workspace DataDir "/tmp/state" and RootDir "/tmp/content"
    When NewWiki initializes
    Then auth, session, search, link, tag, property, branding, asset, revision, and importer state are under "/tmp/state"
    And none of those app-state files are created under "/tmp/content"

  Scenario: Workspace accessors expose both paths
    Given WikiOptions has Workspace DataDir "/tmp/state" and RootDir "/tmp/content"
    When NewWiki initializes
    Then GetStorageDir returns "/tmp/state"
    And GetRootDir returns "/tmp/content"
    And Workspace returns id "default", DataDir "/tmp/state", and RootDir "/tmp/content"
```

```gherkin
Feature: HTTP, importer, and MCP behavior with split storage

  Scenario: HTTP page update writes markdown to root dir
    Given LeafWiki runs with DataDir "/tmp/state" and RootDir "/tmp/content"
    And an authenticated editor exists
    When the editor creates or updates page "api-page"
    Then "/tmp/content/api-page.md" contains the page markdown
    And "/tmp/state/root/api-page.md" does not exist

  Scenario: Assets remain in data dir
    Given LeafWiki runs with DataDir "/tmp/state" and RootDir "/tmp/content"
    When an editor uploads an asset to a page
    Then the asset is stored under "/tmp/state/assets"
    And no asset directory is created under "/tmp/content/assets"

  Scenario: Revisions remain in data dir
    Given LeafWiki runs with revisions enabled
    And DataDir is "/tmp/state"
    And RootDir is "/tmp/content"
    When a page revision is recorded
    Then revision files are stored under "/tmp/state/.leafwiki/revisions"

  Scenario: Importer writes imported pages to root dir
    Given LeafWiki runs with DataDir "/tmp/state" and RootDir "/tmp/content"
    When a markdown import creates page "imported"
    Then "/tmp/content/imported.md" exists
    And importer plan/workspace state remains under "/tmp/state/.importer"

  Scenario: MCP page mutation writes markdown to root dir
    Given LeafWiki runs with MCP enabled
    And DataDir is "/tmp/state"
    And RootDir is "/tmp/content"
    When an MCP client creates or updates a page
    Then the page markdown is written under "/tmp/content"
    And app state remains under "/tmp/state"
```

```gherkin
Feature: Documentation and installer behavior

  Scenario: README documents root dir
    Given the README is updated
    Then it lists "--root-dir" in CLI flags
    And it lists "LEAFWIKI_ROOT_DIR" in environment variables
    And it explains that RootDir stores managed markdown content

  Scenario: Docker docs explain separate volume
    Given Docker examples are updated
    When LEAFWIKI_ROOT_DIR points outside "/app/data"
    Then the docs show mounting a second volume for that root dir

  Scenario: Linux installer persists root dir
    Given install.sh runs in interactive or env-file mode
    When a root dir is provided
    Then it writes LEAFWIKI_ROOT_DIR to the env file
    And it creates and chowns both DataDir and RootDir
    And the completion summary prints both DataDirectory and RootDirectory
```

## Definition of Done

- `--root-dir` and `LEAFWIKI_ROOT_DIR` are implemented with CLI > env > default precedence.
- Existing installs remain compatible: no root dir configured means content stays under `<data-dir>/root`.
- `wiki.Workspace` exists and is used as the internal boundary for the default single workspace.
- `TreeOptions{DataDir, RootDir}` exists and tree storage uses it.
- Markdown page files, section folders, and `.order.json` are stored in `RootDir`.
- `schema.json` and legacy `tree.json` migration state remain in `DataDir`.
- Auth, sessions, search, links, tags, properties, assets, revisions, branding, and importer state remain in `DataDir`.
- `RootDir == DataDir` is rejected with an actionable error.
- Direct test helpers no longer assume page content is always under `GetStorageDir()/root`; they use `GetRootDir()`.
- README, install docs, Docker guidance, CLI usage, and installer behavior are updated.
- All Gherkin scenarios above are represented by Go tests, shell validation tests, documentation checks, or existing test coverage explicitly updated for split storage.
- Verification passes:

```bash
rtk go test ./cmd/leafwiki ./internal/core/tree ./internal/wiki ./internal/http ./internal/importer ./internal/wiki/mcp
rtk go test ./...
rtk go mod tidy -diff
rtk git diff --check
rtk git diff --cached --check
rtk bash -n install.sh
```

- The implementation does not add multi-project routing, per-user root selection, multiple active workspaces, or non-filesystem storage backends.

## Assumptions

- The public name remains `--root-dir` despite “root” being overloaded.
- `RootDir` means the managed markdown content directory itself; it does not contain another `root/` directory.
- Assets and revisions intentionally stay with app state in `DataDir` for this phase.
- The workspace boundary is introduced now to enable future projects/users/multiple-instance designs, but request-time workspace resolution is out of scope.
