package wiki

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/perber/wiki/internal/core/tree"
	httpinternal "github.com/perber/wiki/internal/http"
	"github.com/perber/wiki/internal/projectdaemon"
	"github.com/perber/wiki/internal/test_utils"
	wikipages "github.com/perber/wiki/internal/wiki/pages"
	"github.com/perber/wiki/internal/workspacesync"
)

func createWikiTestInstance(t *testing.T) *Wiki {
	wikiInstance, err := NewWiki(&WikiOptions{
		StorageDir:          t.TempDir(),
		AdminPassword:       "admin",
		JWTSecret:           "secretkey",
		AccessTokenTimeout:  15 * time.Minute,
		RefreshTokenTimeout: 7 * 24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("Failed to create wiki instance: %v", err)
	}
	return wikiInstance
}

func createWikiTestInstanceWithWorkspace(t *testing.T, workspace Workspace) *Wiki {
	t.Helper()
	wikiInstance, err := NewWiki(&WikiOptions{
		Workspace:           workspace,
		AdminPassword:       "admin",
		JWTSecret:           "secretkey",
		AccessTokenTimeout:  15 * time.Minute,
		RefreshTokenTimeout: 7 * 24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("Failed to create wiki instance: %v", err)
	}
	return wikiInstance
}

func pageNodeKind() *tree.NodeKind {
	kind := tree.NodeKindPage
	return &kind
}

func createPageForTest(t *testing.T, w *Wiki, userID string, parentID *string, title, slug string, kind *tree.NodeKind) *tree.Page {
	t.Helper()

	out, err := wikipages.NewCreatePageUseCase(w.tree, w.slug, w.newPageOrchestrator(), w.log).Execute(
		context.Background(),
		wikipages.CreatePageInput{UserID: userID, ParentID: parentID, Title: title, Slug: slug, Kind: kind},
	)
	if err != nil {
		t.Fatalf("CreatePage failed: %v", err)
	}
	return out.Page
}

func updatePageForTest(t *testing.T, w *Wiki, userID, id, title, slug string, content *string, kind *tree.NodeKind) *tree.Page {
	t.Helper()

	current, err := w.tree.GetPage(id)
	if err != nil {
		t.Fatalf("GetPage before update failed: %v", err)
	}

	out, err := wikipages.NewUpdatePageUseCase(w.tree, w.slug, w.newPageOrchestrator(), w.log).Execute(
		context.Background(),
		wikipages.UpdatePageInput{UserID: userID, ID: id, Version: current.Version(), Title: title, Slug: slug, Content: content, Kind: kind},
	)
	if err != nil {
		t.Fatalf("UpdatePage failed: %v", err)
	}
	return out.Page
}

func deletePageForTest(t *testing.T, w *Wiki, userID, id string, recursive bool) {
	t.Helper()

	current, err := w.tree.GetPage(id)
	if err != nil {
		t.Fatalf("GetPage before delete failed: %v", err)
	}

	if err := wikipages.NewDeletePageUseCase(w.tree, w.asset, w.newPageOrchestrator(), w.log).Execute(
		context.Background(),
		wikipages.DeletePageInput{UserID: userID, ID: id, Version: current.Version(), Recursive: recursive},
	); err != nil {
		t.Fatalf("DeletePage failed: %v", err)
	}
}

func TestWiki_DeletePage_Simple(t *testing.T) {
	w := createWikiTestInstance(t)
	defer test_utils.WrapCloseWithErrorCheck(w.Close, t)
	page := createPageForTest(t, w, "system", nil, "Trash", "trash", pageNodeKind())
	deletePageForTest(t, w, "system", page.ID, false)
	if _, err := w.tree.GetPage(page.ID); err == nil {
		t.Fatalf("expected deleted page to be gone")
	}
}

func TestWiki_DefaultWorkspaceKeepsExistingStorageLayout(t *testing.T) {
	dataDir := t.TempDir()
	wikiInstance, err := NewWiki(&WikiOptions{
		StorageDir:          dataDir,
		AdminPassword:       "admin",
		JWTSecret:           "secretkey",
		AccessTokenTimeout:  15 * time.Minute,
		RefreshTokenTimeout: 7 * 24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("Failed to create wiki instance: %v", err)
	}
	defer test_utils.WrapCloseWithErrorCheck(wikiInstance.Close, t)

	if got := wikiInstance.GetStorageDir(); got != dataDir {
		t.Fatalf("GetStorageDir() = %q, want %q", got, dataDir)
	}
	if got, want := wikiInstance.GetRootDir(), filepath.Join(dataDir, "root"); got != want {
		t.Fatalf("GetRootDir() = %q, want %q", got, want)
	}
	if got := wikiInstance.Workspace(); got.ID != "default" || got.DataDir != dataDir || got.RootDir != filepath.Join(dataDir, "root") {
		t.Fatalf("unexpected default workspace: %#v", got)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "root", "welcome-to-leafwiki.md")); err != nil {
		t.Fatalf("expected welcome page in default root dir: %v", err)
	}
}

func TestWiki_ExplicitWorkspaceStoresContentInRootDirAndStateInDataDir(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	rootDir := filepath.Join(t.TempDir(), "content")
	w := createWikiTestInstanceWithWorkspace(t, Workspace{
		ID:      "default",
		DataDir: dataDir,
		RootDir: rootDir,
	})
	defer test_utils.WrapCloseWithErrorCheck(w.Close, t)

	if got := w.GetStorageDir(); got != dataDir {
		t.Fatalf("GetStorageDir() = %q, want %q", got, dataDir)
	}
	if got := w.GetRootDir(); got != rootDir {
		t.Fatalf("GetRootDir() = %q, want %q", got, rootDir)
	}
	if _, err := os.Stat(filepath.Join(rootDir, "welcome-to-leafwiki.md")); err != nil {
		t.Fatalf("expected welcome page in explicit root dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "root", "welcome-to-leafwiki.md")); !os.IsNotExist(err) {
		t.Fatalf("expected no welcome page in data dir root, got err=%v", err)
	}
	for _, rel := range []string{
		"users.db",
		"sessions.db",
		"search.db",
		"links.db",
		"tags.db",
		"properties.db",
		"assets",
		".leafwiki",
		".importer",
		"branding",
	} {
		if _, err := os.Stat(filepath.Join(dataDir, rel)); err != nil {
			t.Fatalf("expected app state %s in data dir: %v", rel, err)
		}
	}

	if err := w.branding.UpdateBranding("Workspace Wiki"); err != nil {
		t.Fatalf("UpdateBranding failed: %v", err)
	}
	logo, err := os.CreateTemp(t.TempDir(), "logo-*.png")
	if err != nil {
		t.Fatalf("CreateTemp logo failed: %v", err)
	}
	defer func() {
		if err := logo.Close(); err != nil {
			t.Fatalf("Close logo failed: %v", err)
		}
	}()
	if _, err := logo.Write([]byte("png")); err != nil {
		t.Fatalf("Write logo failed: %v", err)
	}
	if _, err := logo.Seek(0, 0); err != nil {
		t.Fatalf("Seek logo failed: %v", err)
	}
	if _, err := w.branding.UploadLogo(logo, "logo.png"); err != nil {
		t.Fatalf("UploadLogo failed: %v", err)
	}
	for _, rel := range []string{
		"branding.json",
		filepath.Join("branding", "logo.png"),
	} {
		if _, err := os.Stat(filepath.Join(dataDir, rel)); err != nil {
			t.Fatalf("expected branding state %s in data dir: %v", rel, err)
		}
		if _, err := os.Stat(filepath.Join(rootDir, rel)); !os.IsNotExist(err) {
			t.Fatalf("expected no branding state %s in root dir, got err=%v", rel, err)
		}
	}
}

func TestWiki_WorkspaceOnlyDoesNotCreateIdentityOAuthOrBrandingStores(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	rootDir := filepath.Join(t.TempDir(), "content")
	w, err := NewWiki(&WikiOptions{
		Workspace: Workspace{
			ID:      "current",
			DataDir: dataDir,
			RootDir: rootDir,
		},
		WorkspaceOnly:       true,
		AdminPassword:       "admin",
		JWTSecret:           "secretkey",
		AccessTokenTimeout:  15 * time.Minute,
		RefreshTokenTimeout: 7 * 24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("NewWiki workspace-only failed: %v", err)
	}
	defer test_utils.WrapCloseWithErrorCheck(w.Close, t)

	if w.UserService() != nil || w.AuthService() != nil || w.APIKeyService() != nil || w.OAuthService() != nil {
		t.Fatalf("workspace-only wiki owns identity services: user=%v auth=%v apiKeys=%v oauth=%v", w.UserService(), w.AuthService(), w.APIKeyService(), w.OAuthService())
	}
	for _, rel := range []string{
		"users.db",
		"sessions.db",
		"api_keys.db",
		"oauth",
		"branding",
		"branding.json",
	} {
		if _, err := os.Stat(filepath.Join(dataDir, rel)); !os.IsNotExist(err) {
			t.Fatalf("workspace-only state %s stat err = %v, want not exist", rel, err)
		}
	}
	for _, rel := range []string{
		"search.db",
		"links.db",
		"tags.db",
		"properties.db",
		"assets",
	} {
		if _, err := os.Stat(filepath.Join(dataDir, rel)); err != nil {
			t.Fatalf("expected workspace state %s: %v", rel, err)
		}
	}
	if _, err := os.Stat(filepath.Join(rootDir, "welcome-to-leafwiki.md")); err != nil {
		t.Fatalf("expected workspace content in root dir: %v", err)
	}
}

func TestWiki_ControlPlaneOnlyDoesNotCreateWorkspaceStores(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	rootDir := filepath.Join(t.TempDir(), "content")
	w, err := NewWiki(&WikiOptions{
		Workspace: Workspace{
			ID:      "current",
			DataDir: dataDir,
			RootDir: rootDir,
		},
		ControlPlaneOnly:    true,
		AdminPassword:       "admin",
		JWTSecret:           "secretkey",
		AccessTokenTimeout:  15 * time.Minute,
		RefreshTokenTimeout: 7 * 24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("NewWiki control-plane-only failed: %v", err)
	}
	defer test_utils.WrapCloseWithErrorCheck(w.Close, t)

	if w.UserService() == nil || w.AuthService() == nil || w.APIKeyService() == nil || w.OAuthService() == nil || w.branding == nil {
		t.Fatalf("control-plane-only wiki did not own identity/branding services: user=%v auth=%v apiKeys=%v oauth=%v branding=%v", w.UserService(), w.AuthService(), w.APIKeyService(), w.OAuthService(), w.branding)
	}
	if w.tree != nil || w.asset != nil || w.searchIndex != nil || w.links != nil || w.tags != nil || w.props != nil || w.workspaceSync != nil {
		t.Fatalf("control-plane-only wiki owns workspace services: tree=%v asset=%v search=%v links=%v tags=%v props=%v sync=%v", w.tree, w.asset, w.searchIndex, w.links, w.tags, w.props, w.workspaceSync)
	}
	for _, rel := range []string{
		"search.db",
		"links.db",
		"tags.db",
		"properties.db",
		"assets",
		".importer",
	} {
		if _, err := os.Stat(filepath.Join(dataDir, rel)); !os.IsNotExist(err) {
			t.Fatalf("control-plane-only workspace state %s stat err = %v, want not exist", rel, err)
		}
	}
	if _, err := os.Stat(filepath.Join(rootDir, "welcome-to-leafwiki.md")); !os.IsNotExist(err) {
		t.Fatalf("control-plane-only root welcome stat err = %v, want not exist", err)
	}
}

func TestWiki_ControlPlaneHealthIncludesRuntimeRoleHealth(t *testing.T) {
	w, err := NewWiki(&WikiOptions{
		Workspace: Workspace{
			ID:      "current",
			DataDir: filepath.Join(t.TempDir(), "data"),
			RootDir: filepath.Join(t.TempDir(), "content"),
		},
		AuthStorageDir:      t.TempDir(),
		ControlPlaneOnly:    true,
		AdminPassword:       "admin",
		JWTSecret:           "secretkey",
		AccessTokenTimeout:  15 * time.Minute,
		RefreshTokenTimeout: 7 * 24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("NewWiki control-plane-only failed: %v", err)
	}
	defer test_utils.WrapCloseWithErrorCheck(w.Close, t)

	now := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
	w.SetRuntimeRoleHealth([]projectdaemon.RoleName{
		projectdaemon.RoleWikid,
		projectdaemon.RoleFrontd,
		projectdaemon.RoleWorkspaced,
	}, func() []projectdaemon.RoleHealth {
		return []projectdaemon.RoleHealth{
			{Name: projectdaemon.RoleWikid, State: projectdaemon.RoleStateReady, PID: 1, UpdatedAt: now},
			{Name: projectdaemon.RoleFrontd, State: projectdaemon.RoleStateReady, PID: 2, UpdatedAt: now},
			{Name: projectdaemon.RoleWorkspaced, State: projectdaemon.RoleStateCrashed, PID: 3, Error: "restart exhausted", UpdatedAt: now},
		}
	})
	router := httpinternal.NewRouter(w.FrontdRegistrars(), w.FrontendConfig(), httpinternal.RouterOptions{})

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("GET /api/health status = %d, want 503: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Checks map[string]string `json:"checks"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode health response: %v", err)
	}
	if body.Checks["role_workspaced"] != "crashed" {
		t.Fatalf("role_workspaced = %q, want crashed; checks=%#v", body.Checks["role_workspaced"], body.Checks)
	}
}

func TestWiki_WorkspaceSyncDoesNotFailStartupOnInvalidMarkdown(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	rootDir := filepath.Join(t.TempDir(), "content")
	if err := os.MkdirAll(rootDir, 0o755); err != nil {
		t.Fatalf("create root dir: %v", err)
	}
	invalidA := "---\nleafwiki_id: duplicate\nleafwiki_title: A\n---\n# A\n"
	invalidB := "---\nleafwiki_id: duplicate\nleafwiki_title: B\n---\n# B\n"
	if err := os.WriteFile(filepath.Join(rootDir, "a.md"), []byte(invalidA), 0o644); err != nil {
		t.Fatalf("write a.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rootDir, "b.md"), []byte(invalidB), 0o644); err != nil {
		t.Fatalf("write b.md: %v", err)
	}

	w, err := NewWiki(&WikiOptions{
		Workspace: Workspace{
			DataDir: dataDir,
			RootDir: rootDir,
		},
		AdminPassword:       "admin",
		JWTSecret:           "secretkey",
		AccessTokenTimeout:  15 * time.Minute,
		RefreshTokenTimeout: 7 * 24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("NewWiki with invalid workspace sync state returned error: %v", err)
	}
	defer test_utils.WrapCloseWithErrorCheck(w.Close, t)

	status := w.WorkspaceSyncStatus()
	if status.LastCommitHash == "" {
		t.Fatalf("workspace sync status missing commit hash: %#v", status)
	}
	if len(status.ValidationErrors) == 0 {
		t.Fatalf("workspace sync status missing validation errors: %#v", status)
	}
	var hasPath bool
	for _, validationErr := range status.ValidationErrors {
		if validationErr.Path == "a.md" || validationErr.Path == "b.md" {
			hasPath = true
			break
		}
	}
	if !hasPath {
		t.Fatalf("workspace sync validation errors missing markdown path: %#v", status.ValidationErrors)
	}
}

func TestWiki_MarkdownLinkRootPrefixFlowsToWorkspaceSyncAndLinkIndex(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	rootDir := filepath.Join(t.TempDir(), "repo", "docs")
	if err := os.MkdirAll(filepath.Join(rootDir, "sync"), 0o755); err != nil {
		t.Fatalf("create sync dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rootDir, "source.md"), []byte(`---
leafwiki_id: source
leafwiki_title: Source
---
# Source

[Glossary](/docs/sync/glossary.md)
`), 0o644); err != nil {
		t.Fatalf("write source.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rootDir, "sync", "glossary.md"), []byte(`---
leafwiki_id: glossary
leafwiki_title: Glossary
---
# Glossary
`), 0o644); err != nil {
		t.Fatalf("write glossary.md: %v", err)
	}

	w, err := NewWiki(&WikiOptions{
		Workspace:              Workspace{DataDir: dataDir, RootDir: rootDir},
		AdminPassword:          "admin",
		JWTSecret:              "secretkey",
		AccessTokenTimeout:     15 * time.Minute,
		RefreshTokenTimeout:    7 * 24 * time.Hour,
		MarkdownLinkRootPrefix: "/docs",
	})
	if err != nil {
		t.Fatalf("NewWiki: %v", err)
	}
	if status := w.WorkspaceSyncStatus(); len(status.ValidationErrors) != 0 {
		t.Fatalf("ValidationErrors = %#v, want prefix-aware validation", status.ValidationErrors)
	}
	source, err := w.tree.GetPage("source")
	if err != nil {
		t.Fatalf("GetPage source: %v", err)
	}
	outgoing, err := w.links.GetOutgoingLinksForPage(source.ID)
	if err != nil {
		t.Fatalf("GetOutgoingLinksForPage: %v", err)
	}
	if outgoing.Count != 1 || outgoing.Outgoings[0].ToPath != "/sync/glossary" || outgoing.Outgoings[0].Broken {
		t.Fatalf("outgoing = %#v, want resolved /sync/glossary", outgoing)
	}
	test_utils.WrapCloseWithErrorCheck(w.Close, t)
}

func TestWiki_WorkspaceSyncCommitsWebPageCreates(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	rootDir := filepath.Join(t.TempDir(), "content")
	w, err := NewWiki(&WikiOptions{
		Workspace:           Workspace{DataDir: dataDir, RootDir: rootDir},
		AdminPassword:       "admin",
		JWTSecret:           "secretkey",
		AccessTokenTimeout:  15 * time.Minute,
		RefreshTokenTimeout: 7 * 24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("NewWiki: %v", err)
	}
	defer test_utils.WrapCloseWithErrorCheck(w.Close, t)
	before := w.WorkspaceSyncStatus().LastCommitHash
	if before == "" {
		t.Fatalf("initial workspace sync commit hash is empty")
	}

	page := createPageForTest(t, w, "alice", nil, "Synced Web Page", "synced-web-page", pageNodeKind())

	after := w.WorkspaceSyncStatus().LastCommitHash
	if after == "" || after == before {
		t.Fatalf("workspace sync commit hash after create = %q, before %q", after, before)
	}
	result, err := w.WorkspaceSyncPageRevisions(context.Background(), page, "", 10)
	if err != nil {
		t.Fatalf("WorkspaceSyncPageRevisions: %v", err)
	}
	revisions := result.Revisions
	if len(revisions) == 0 {
		t.Fatalf("workspace sync revisions missing for web-created page")
	}
	if revisions[0].AuthorID != "alice" {
		t.Fatalf("workspace sync revision author = %q, want alice", revisions[0].AuthorID)
	}
}

func TestWiki_WorkspaceSyncCommitsImportedPages(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	rootDir := filepath.Join(t.TempDir(), "content")
	w, err := NewWiki(&WikiOptions{
		Workspace:           Workspace{DataDir: dataDir, RootDir: rootDir},
		AdminPassword:       "admin",
		JWTSecret:           "secretkey",
		AccessTokenTimeout:  15 * time.Minute,
		RefreshTokenTimeout: 7 * 24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("NewWiki: %v", err)
	}
	defer test_utils.WrapCloseWithErrorCheck(w.Close, t)
	before := w.WorkspaceSyncStatus().LastCommitHash
	if before == "" {
		t.Fatalf("initial workspace sync commit hash is empty")
	}

	adapter := NewWikiImportAdapter(w)
	page, err := adapter.EnsurePath("importer-user", "imported-page", "Imported Page", pageNodeKind())
	if err != nil {
		t.Fatalf("EnsurePath: %v", err)
	}
	content := "Imported body\n"
	page, err = adapter.UpdatePage("importer-user", page.ID, page.Title, page.Slug, &content, &page.Kind)
	if err != nil {
		t.Fatalf("UpdatePage: %v", err)
	}

	after := w.WorkspaceSyncStatus().LastCommitHash
	if after == "" || after == before {
		t.Fatalf("workspace sync commit hash after import = %q, before %q", after, before)
	}
	result, err := w.WorkspaceSyncPageRevisions(context.Background(), page, "", 10)
	if err != nil {
		t.Fatalf("WorkspaceSyncPageRevisions: %v", err)
	}
	revisions := result.Revisions
	if len(revisions) == 0 {
		t.Fatalf("workspace sync revisions missing for imported page")
	}
	if revisions[0].AuthorID != "importer-user" {
		t.Fatalf("workspace sync revision author = %q, want importer-user", revisions[0].AuthorID)
	}
	snapshots, err := w.WorkspaceSyncSnapshots(context.Background(), 5)
	if err != nil {
		t.Fatalf("WorkspaceSyncSnapshots: %v", err)
	}
	if len(snapshots) == 0 || snapshots[0].ID != after {
		t.Fatalf("latest workspace snapshot = %#v, want commit %s", snapshots, after)
	}
	if snapshots[0].AuthorID != "importer-user" {
		t.Fatalf("latest workspace snapshot author = %q, want importer-user", snapshots[0].AuthorID)
	}
	if snapshots[0].Source != string(workspacesync.SourceWeb) {
		t.Fatalf("latest workspace snapshot source = %q, want web", snapshots[0].Source)
	}
}

func TestWiki_WorkspaceSyncRefreshRebuildsDerivedIndexes(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	rootDir := filepath.Join(t.TempDir(), "content")
	w, err := NewWiki(&WikiOptions{
		Workspace:           Workspace{DataDir: dataDir, RootDir: rootDir},
		AdminPassword:       "admin",
		JWTSecret:           "secretkey",
		AccessTokenTimeout:  15 * time.Minute,
		RefreshTokenTimeout: 7 * 24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("NewWiki: %v", err)
	}
	defer test_utils.WrapCloseWithErrorCheck(w.Close, t)

	raw := `---
leafwiki_id: indexed-page
leafwiki_title: Indexed Page
tags:
  - synced
status: draft
---

# Indexed Page

workspace-sync-search-token`
	if err := os.WriteFile(filepath.Join(rootDir, "indexed-page.md"), []byte(raw), 0o644); err != nil {
		t.Fatalf("write direct markdown: %v", err)
	}

	status, err := w.WorkspaceSyncRefresh(context.Background(), workspacesync.SyncRequest{
		Reason: workspacesync.ReasonExplicit,
		Source: workspacesync.SourceFilesystem,
		Actor:  workspacesync.PublicEditorActor(),
	})
	if err != nil {
		t.Fatalf("WorkspaceSyncRefresh: %v", err)
	}
	if status.LastCommitHash == "" {
		t.Fatalf("sync status missing commit hash: %#v", status)
	}

	if _, err := w.tree.GetPage("indexed-page"); err != nil {
		t.Fatalf("GetPage indexed-page: %v", err)
	}
	tagged, err := w.tags.GetPageIDsByTags([]string{"synced"})
	if err != nil {
		t.Fatalf("GetPageIDsByTags: %v", err)
	}
	if len(tagged) != 1 || tagged[0] != "indexed-page" {
		t.Fatalf("tagged pages = %#v, want indexed-page", tagged)
	}
	props, err := w.props.GetPropertiesForPages([]string{"indexed-page"})
	if err != nil {
		t.Fatalf("GetPropertiesForPages: %v", err)
	}
	if props["indexed-page"]["status"].Value != "draft" {
		t.Fatalf("properties = %#v, want status draft", props)
	}
	result, err := w.searchIndex.Search("workspace-sync-search-token", nil, 0, 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if result.Count == 0 {
		t.Fatalf("search result count = 0, want indexed direct markdown")
	}

	updatedRaw := `---
leafwiki_id: indexed-page
leafwiki_title: Indexed Page
tags:
  - resynced
status: published
---

# Indexed Page

workspace-sync-updated-token`
	if err := os.WriteFile(filepath.Join(rootDir, "indexed-page.md"), []byte(updatedRaw), 0o644); err != nil {
		t.Fatalf("write updated direct markdown: %v", err)
	}
	if _, err := w.WorkspaceSyncRefresh(context.Background(), workspacesync.SyncRequest{
		Reason: workspacesync.ReasonExplicit,
		Source: workspacesync.SourceFilesystem,
		Actor:  workspacesync.PublicEditorActor(),
	}); err != nil {
		t.Fatalf("WorkspaceSyncRefresh after update: %v", err)
	}
	tagged, err = w.tags.GetPageIDsByTags([]string{"synced"})
	if err != nil {
		t.Fatalf("GetPageIDsByTags synced after update: %v", err)
	}
	if len(tagged) != 0 {
		t.Fatalf("synced tagged pages after update = %#v, want none", tagged)
	}
	tagged, err = w.tags.GetPageIDsByTags([]string{"resynced"})
	if err != nil {
		t.Fatalf("GetPageIDsByTags resynced after update: %v", err)
	}
	if len(tagged) != 1 || tagged[0] != "indexed-page" {
		t.Fatalf("resynced tagged pages = %#v, want indexed-page", tagged)
	}
	props, err = w.props.GetPropertiesForPages([]string{"indexed-page"})
	if err != nil {
		t.Fatalf("GetPropertiesForPages after update: %v", err)
	}
	if props["indexed-page"]["status"].Value != "published" {
		t.Fatalf("properties after update = %#v, want status published", props)
	}
	result, err = w.searchIndex.Search("workspace-sync-search-token", nil, 0, 10)
	if err != nil {
		t.Fatalf("Search old token after update: %v", err)
	}
	if result.Count != 0 {
		t.Fatalf("old search result count = %d, want 0", result.Count)
	}
	result, err = w.searchIndex.Search("workspace-sync-updated-token", nil, 0, 10)
	if err != nil {
		t.Fatalf("Search updated token: %v", err)
	}
	if result.Count == 0 {
		t.Fatalf("updated search result count = 0, want indexed updated markdown")
	}

	if err := os.Remove(filepath.Join(rootDir, "indexed-page.md")); err != nil {
		t.Fatalf("remove direct markdown: %v", err)
	}
	if _, err := w.WorkspaceSyncRefresh(context.Background(), workspacesync.SyncRequest{
		Reason: workspacesync.ReasonExplicit,
		Source: workspacesync.SourceFilesystem,
		Actor:  workspacesync.PublicEditorActor(),
	}); err != nil {
		t.Fatalf("WorkspaceSyncRefresh after delete: %v", err)
	}
	if _, err := w.tree.GetPage("indexed-page"); err == nil {
		t.Fatalf("GetPage indexed-page after delete succeeded, want missing page")
	}
	tagged, err = w.tags.GetPageIDsByTags([]string{"resynced"})
	if err != nil {
		t.Fatalf("GetPageIDsByTags resynced after delete: %v", err)
	}
	if len(tagged) != 0 {
		t.Fatalf("resynced tagged pages after delete = %#v, want none", tagged)
	}
	result, err = w.searchIndex.Search("workspace-sync-updated-token", nil, 0, 10)
	if err != nil {
		t.Fatalf("Search updated token after delete: %v", err)
	}
	if result.Count != 0 {
		t.Fatalf("deleted search result count = %d, want 0", result.Count)
	}
}

func TestWiki_RunMCPStdioUsesDisabledAuthPublicEditor(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	rootDir := filepath.Join(t.TempDir(), "content")
	w, err := NewWiki(&WikiOptions{
		Workspace:           Workspace{ID: "default", DataDir: dataDir, RootDir: rootDir},
		AdminPassword:       "admin",
		JWTSecret:           "secretkey",
		AccessTokenTimeout:  15 * time.Minute,
		RefreshTokenTimeout: 7 * 24 * time.Hour,
		AuthDisabled:        true,
	})
	if err != nil {
		t.Fatalf("NewWiki failed: %v", err)
	}
	defer test_utils.WrapCloseWithErrorCheck(w.Close, t)

	serverTransport, clientTransport := sdkmcp.NewInMemoryTransports()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	serverDone := make(chan error, 1)
	go func() {
		serverDone <- w.RunMCPStdio(ctx, httpinternal.RouterOptions{
			PublicAccess:            true,
			AuthDisabled:            true,
			MaxAssetUploadSizeBytes: 50 * 1024 * 1024,
		}, serverTransport)
	}()

	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "leafwiki-test", Version: "test"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("Connect MCP client failed: %v", err)
	}
	defer session.Close()

	tools, err := session.ListTools(ctx, &sdkmcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("ListTools failed: %v", err)
	}
	if !mcpToolNamesContain(tools.Tools, "wiki_create_page") || !mcpToolNamesContain(tools.Tools, "wiki_get_current_user") {
		t.Fatalf("native stdio tools = %#v, want shared LeafWiki tools", tools.Tools)
	}

	current, err := session.CallTool(ctx, &sdkmcp.CallToolParams{Name: "wiki_get_current_user"})
	if err != nil {
		t.Fatalf("get_current_user failed: %v", err)
	}
	currentUser, ok := current.StructuredContent.(map[string]any)["user"].(map[string]any)
	if !ok {
		t.Fatalf("get_current_user structured content = %#v, want user map", current.StructuredContent)
	}
	if currentUser["username"] != "public-editor" || currentUser["role"] != "editor" {
		t.Fatalf("native stdio current user = %#v, want public-editor editor", currentUser)
	}

	session.Close()
	select {
	case err := <-serverDone:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("RunMCPStdio returned %v", err)
		}
	case <-ctx.Done():
		t.Fatalf("RunMCPStdio did not stop after client close: %v", ctx.Err())
	}
}

func TestWiki_RunMCPStdioWorkspaceSyncMarksSourceAndServesGitHistory(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	rootDir := filepath.Join(t.TempDir(), "content")
	w, err := NewWiki(&WikiOptions{
		Workspace:           Workspace{ID: "default", DataDir: dataDir, RootDir: rootDir},
		AdminPassword:       "admin",
		JWTSecret:           "secretkey",
		AccessTokenTimeout:  15 * time.Minute,
		RefreshTokenTimeout: 7 * 24 * time.Hour,
		AuthDisabled:        true,
	})
	if err != nil {
		t.Fatalf("NewWiki failed: %v", err)
	}
	defer test_utils.WrapCloseWithErrorCheck(w.Close, t)

	serverTransport, clientTransport := sdkmcp.NewInMemoryTransports()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	serverDone := make(chan error, 1)
	go func() {
		serverDone <- w.RunMCPStdio(ctx, httpinternal.RouterOptions{
			PublicAccess:            true,
			AuthDisabled:            true,
			MaxAssetUploadSizeBytes: 50 * 1024 * 1024,
		}, serverTransport)
	}()

	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "leafwiki-test", Version: "test"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("Connect MCP client failed: %v", err)
	}
	defer session.Close()

	tools, err := session.ListTools(ctx, &sdkmcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("ListTools failed: %v", err)
	}
	if !mcpToolNamesContain(tools.Tools, "wiki_list_revisions") {
		t.Fatalf("workspace sync MCP tools missing list_revisions: %#v", tools.Tools)
	}

	created, err := session.CallTool(ctx, &sdkmcp.CallToolParams{
		Name: "wiki_create_page",
		Arguments: map[string]any{
			"title": "MCP Synced",
			"slug":  "mcp-synced",
			"kind":  "page",
		},
	})
	if err != nil {
		t.Fatalf("create_page failed: %v", err)
	}
	if created.IsError {
		t.Fatalf("create_page returned tool error: %#v", created.Content)
	}
	createdPage, ok := created.StructuredContent.(map[string]any)["page"].(map[string]any)
	if !ok {
		t.Fatalf("create_page structured content = %#v, want page map", created.StructuredContent)
	}
	pageID, _ := createdPage["id"].(string)
	if pageID == "" {
		t.Fatalf("created page missing id: %#v", createdPage)
	}

	snapshots, err := w.WorkspaceSyncSnapshots(ctx, 5)
	if err != nil {
		t.Fatalf("WorkspaceSyncSnapshots: %v", err)
	}
	if len(snapshots) == 0 || snapshots[0].Source != string(workspacesync.SourceMCP) {
		t.Fatalf("latest workspace snapshot = %#v, want source mcp", snapshots)
	}

	revisions, err := session.CallTool(ctx, &sdkmcp.CallToolParams{
		Name: "wiki_list_revisions",
		Arguments: map[string]any{
			"pageId": pageID,
		},
	})
	if err != nil {
		t.Fatalf("list_revisions failed: %v", err)
	}
	if revisions.IsError {
		t.Fatalf("list_revisions returned tool error: %#v", revisions.Content)
	}
	revisionsContent, ok := revisions.StructuredContent.(map[string]any)["revisions"].([]any)
	if !ok || len(revisionsContent) == 0 {
		t.Fatalf("list_revisions structured content = %#v, want non-empty revisions", revisions.StructuredContent)
	}

	session.Close()
	select {
	case err := <-serverDone:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("RunMCPStdio returned %v", err)
		}
	case <-ctx.Done():
		t.Fatalf("RunMCPStdio did not stop after client close: %v", ctx.Err())
	}
}

func mcpToolNamesContain(tools []*sdkmcp.Tool, name string) bool {
	for _, tool := range tools {
		if tool.Name == name {
			return true
		}
	}
	return false
}

func TestWiki_RejectsWorkspaceWithSameDataAndRootDir(t *testing.T) {
	dir := t.TempDir()

	_, err := NewWiki(&WikiOptions{
		Workspace:           Workspace{ID: "default", DataDir: dir, RootDir: filepath.Clean(filepath.Join(dir, "."))},
		AdminPassword:       "admin",
		JWTSecret:           "secretkey",
		AccessTokenTimeout:  15 * time.Minute,
		RefreshTokenTimeout: 7 * 24 * time.Hour,
	})
	if err == nil {
		t.Fatalf("expected same data/root dir to be rejected")
	}
	if !strings.Contains(err.Error(), "root dir must be different from data dir") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWiki_RejectsWorkspaceWhenRootDirContainsDataDir(t *testing.T) {
	rootDir := filepath.Join(t.TempDir(), "wiki")
	dataDir := filepath.Join(rootDir, "data")

	_, err := NewWiki(&WikiOptions{
		Workspace:           Workspace{ID: "default", DataDir: dataDir, RootDir: rootDir},
		AdminPassword:       "admin",
		JWTSecret:           "secretkey",
		AccessTokenTimeout:  15 * time.Minute,
		RefreshTokenTimeout: 7 * 24 * time.Hour,
	})
	if err == nil {
		t.Fatalf("expected root dir containing data dir to be rejected")
	}
	if !strings.Contains(err.Error(), "root dir must not contain data dir") {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, statErr := os.Stat(rootDir); !os.IsNotExist(statErr) {
		t.Fatalf("expected invalid root dir not to be created before startup, got err=%v", statErr)
	}
}

func TestWiki_NormalizesWorkspacePathsBeforeInitializingServices(t *testing.T) {
	baseDir := t.TempDir()
	dataDir := filepath.Join(baseDir, "data")
	rootDir := filepath.Join(baseDir, "content")

	w, err := NewWiki(&WikiOptions{
		Workspace:           Workspace{ID: "default", DataDir: " " + dataDir + string(os.PathSeparator) + "." + " ", RootDir: " " + rootDir + string(os.PathSeparator) + "." + " "},
		AdminPassword:       "admin",
		JWTSecret:           "secretkey",
		AccessTokenTimeout:  15 * time.Minute,
		RefreshTokenTimeout: 7 * 24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("NewWiki failed: %v", err)
	}
	defer test_utils.WrapCloseWithErrorCheck(w.Close, t)

	if got := w.GetStorageDir(); got != dataDir {
		t.Fatalf("GetStorageDir() = %q, want normalized %q", got, dataDir)
	}
	if got := w.GetRootDir(); got != rootDir {
		t.Fatalf("GetRootDir() = %q, want normalized %q", got, rootDir)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "users.db")); err != nil {
		t.Fatalf("expected auth state in normalized data dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(rootDir, "welcome-to-leafwiki.md")); err != nil {
		t.Fatalf("expected content in normalized root dir: %v", err)
	}
}

func TestWiki_DeletePage_WithChildren(t *testing.T) {
	w := createWikiTestInstance(t)
	defer test_utils.WrapCloseWithErrorCheck(w.Close, t)
	parent := createPageForTest(t, w, "system", nil, "Parent", "parent", pageNodeKind())
	createPageForTest(t, w, "system", &parent.ID, "Child", "child", pageNodeKind())

	err := wikipages.NewDeletePageUseCase(w.tree, w.asset, w.newPageOrchestrator(), w.log).Execute(
		context.Background(),
		wikipages.DeletePageInput{UserID: "system", ID: parent.ID, Version: parent.Version(), Recursive: false},
	)
	if err == nil {
		t.Error("Expected error when deleting parent with children")
	}
}

func TestWiki_DeletePage_Recursive(t *testing.T) {
	w := createWikiTestInstance(t)
	defer test_utils.WrapCloseWithErrorCheck(w.Close, t)
	parent := createPageForTest(t, w, "system", nil, "Parent", "parent", pageNodeKind())
	child := createPageForTest(t, w, "system", &parent.ID, "Child", "child", pageNodeKind())

	deletePageForTest(t, w, "system", parent.ID, true)
	if _, err := w.tree.GetPage(parent.ID); err == nil {
		t.Fatalf("expected deleted parent to be gone")
	}
	if _, err := w.tree.GetPage(child.ID); err == nil {
		t.Fatalf("expected deleted child to be gone")
	}
}

func TestWiki_InitDefaultAdmin_UsesGivenPassword(t *testing.T) {
	w := createWikiTestInstance(t)
	defer test_utils.WrapCloseWithErrorCheck(w.Close, t)

	_, err := w.user.GetUserByEmailOrUsernameAndPassword("admin", "admin")
	if err != nil {
		t.Fatalf("Admin user not found: %v", err)
	}
}

func TestWiki_Login_SuccessAndFailure(t *testing.T) {
	w := createWikiTestInstance(t)
	defer test_utils.WrapCloseWithErrorCheck(w.Close, t)

	authSvc := w.auth
	if authSvc == nil {
		t.Fatal("expected auth service to be initialized")
	}

	token, err := authSvc.Login("admin", "admin")
	if err != nil || token == nil {
		t.Error("Expected login to succeed with default admin password")
	}

	_, err = authSvc.Login("admin", "wrong")
	if err == nil {
		t.Error("Expected login to fail with wrong password")
	}
}

func TestWiki_AuthDisabled_Initialization(t *testing.T) {
	// Create a wiki instance with AuthDisabled set to true
	wikiInstance, err := NewWiki(&WikiOptions{
		StorageDir:          t.TempDir(),
		AdminPassword:       "",
		JWTSecret:           "",
		AccessTokenTimeout:  0,
		RefreshTokenTimeout: 0,
		AuthDisabled:        true,
	})
	if err != nil {
		t.Fatalf("Failed to create wiki instance with AuthDisabled: %v", err)
	}
	defer test_utils.WrapCloseWithErrorCheck(wikiInstance.Close, t)

	// Verify that the auth service is nil
	if wikiInstance.auth != nil {
		t.Error("Expected auth service to be nil when AuthDisabled is true")
	}
}

func TestWiki_AuthDisabled_LoginUnavailable(t *testing.T) {
	// Create a wiki instance with AuthDisabled set to true
	wikiInstance, err := NewWiki(&WikiOptions{
		StorageDir:   t.TempDir(),
		AuthDisabled: true,
	})
	if err != nil {
		t.Fatalf("Failed to create wiki instance with AuthDisabled: %v", err)
	}
	defer test_utils.WrapCloseWithErrorCheck(wikiInstance.Close, t)

	// Auth operations are unavailable when auth is disabled.
	if wikiInstance.auth != nil {
		t.Error("Expected auth service to be nil when AuthDisabled is true")
	}
}

func TestWiki_AuthDisabled_LogoutUnavailable(t *testing.T) {
	// Create a wiki instance with AuthDisabled set to true
	wikiInstance, err := NewWiki(&WikiOptions{
		StorageDir:   t.TempDir(),
		AuthDisabled: true,
	})
	if err != nil {
		t.Fatalf("Failed to create wiki instance with AuthDisabled: %v", err)
	}
	defer test_utils.WrapCloseWithErrorCheck(wikiInstance.Close, t)

	// Auth operations are unavailable when auth is disabled.
	if wikiInstance.auth != nil {
		t.Error("Expected auth service to be nil when AuthDisabled is true")
	}
}

func TestWiki_AuthDisabled_RefreshTokenUnavailable(t *testing.T) {
	// Create a wiki instance with AuthDisabled set to true
	wikiInstance, err := NewWiki(&WikiOptions{
		StorageDir:   t.TempDir(),
		AuthDisabled: true,
	})
	if err != nil {
		t.Fatalf("Failed to create wiki instance with AuthDisabled: %v", err)
	}
	defer test_utils.WrapCloseWithErrorCheck(wikiInstance.Close, t)

	// Auth operations are unavailable when auth is disabled.
	if wikiInstance.auth != nil {
		t.Error("Expected auth service to be nil when AuthDisabled is true")
	}
}

func TestWiki_AuthDisabled_CoreFunctionalityWorks(t *testing.T) {
	// Create a wiki instance with AuthDisabled set to true
	wikiInstance, err := NewWiki(&WikiOptions{
		StorageDir:   t.TempDir(),
		AuthDisabled: true,
	})
	if err != nil {
		t.Fatalf("Failed to create wiki instance with AuthDisabled: %v", err)
	}
	defer test_utils.WrapCloseWithErrorCheck(wikiInstance.Close, t)

	// Test creating a page
	page := createPageForTest(t, wikiInstance, "system", nil, "Test Page", "test-page", pageNodeKind())

	if page.Title != "Test Page" {
		t.Errorf("Expected title 'Test Page', got %q", page.Title)
	}

	// Test updating a page
	var updatedContent = "# Content"
	updatedPage := updatePageForTest(t, wikiInstance, "system", page.ID, "Updated Title", "updated-slug", &updatedContent, pageNodeKind())

	if updatedPage.Title != "Updated Title" {
		t.Errorf("Expected title 'Updated Title', got %q", updatedPage.Title)
	}

	// Test getting a page
	retrievedPage, err := wikiInstance.tree.GetPage(page.ID)
	if err != nil {
		t.Fatalf("Failed to get page with AuthDisabled: %v", err)
	}

	if retrievedPage.ID != page.ID {
		t.Errorf("Expected ID %q, got %q", page.ID, retrievedPage.ID)
	}

	// Test deleting a page
	deletePageForTest(t, wikiInstance, "system", page.ID, false)
}
