package wiki

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/perber/wiki/internal/core/tree"
	httpinternal "github.com/perber/wiki/internal/http"
	"github.com/perber/wiki/internal/projectdaemon"
	"github.com/perber/wiki/internal/workspaceid"
	"github.com/perber/wiki/internal/workspacesync"
)

var _ = ginkgo.Describe("wiki runtime behavior", func() {
	ginkgo.It("removes a leaf page from the tree", ginkgo.Label("integration"), func() {
		w := createWikiTestInstance()
		ginkgo.DeferCleanup(closeWithErrorCheckForTest, w.Close)
		page := createPageForTest(w, "system", nil, "Trash", "trash", pageNodeKind())
		deletePageForTest(w, "system", page.ID, false)
		_, err := w.tree.GetPage(page.ID)
		Expect(err).To(MatchError(tree.ErrPageNotFound))
	})

	ginkgo.It("keeps the legacy storage directory layout for the default workspace", ginkgo.Label("integration"), func() {
		dataDir := wikiTestTempDir()
		wikiInstance, err := NewWiki(&WikiOptions{
			StorageDir:          dataDir,
			AdminPassword:       "admin",
			JWTSecret:           "secretkey",
			AccessTokenTimeout:  15 * time.Minute,
			RefreshTokenTimeout: 7 * 24 * time.Hour,
		})
		Expect(err).To(Succeed())
		ginkgo.DeferCleanup(closeWithErrorCheckForTest, wikiInstance.Close)

		Expect(wikiInstance.GetStorageDir()).To(Equal(dataDir))
		Expect(wikiInstance.GetRootDir()).To(Equal(filepath.Join(dataDir, "root")))
		Expect(wikiInstance.Workspace()).To(SatisfyAll(
			HaveField("ID", Equal(workspaceid.WorkspaceID("default"))),
			HaveField("DataDir", Equal(dataDir)),
			HaveField("RootDir", Equal(filepath.Join(dataDir, "root"))),
		))
		Expect(filepath.Join(dataDir, "root", "welcome-to-leafwiki.md")).To(existAsFileSystemPath())
	})

	ginkgo.It("stores workspace content in the root directory and service state in the data directory", ginkgo.Label("integration"), func() {
		dataDir := filepath.Join(wikiTestTempDir(), "data")
		rootDir := filepath.Join(wikiTestTempDir(), "content")
		w := createWikiTestInstanceWithWorkspace(Workspace{
			ID:      "default",
			DataDir: dataDir,
			RootDir: rootDir,
		})
		ginkgo.DeferCleanup(closeWithErrorCheckForTest, w.Close)

		Expect(w.GetStorageDir()).To(Equal(dataDir))
		Expect(w.GetRootDir()).To(Equal(rootDir))
		Expect(filepath.Join(rootDir, "welcome-to-leafwiki.md")).To(existAsFileSystemPath())
		Expect(filepath.Join(dataDir, "root", "welcome-to-leafwiki.md")).To(beMissingFileSystemPath())
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
			Expect(filepath.Join(dataDir, rel)).To(existAsFileSystemPath())
		}

		Expect(w.branding.UpdateBranding("Workspace Wiki")).To(Succeed())
		logo, err := os.CreateTemp(wikiTestTempDir(), "logo-*.png")
		Expect(err).To(Succeed())
		ginkgo.DeferCleanup(closeWithErrorCheckForTest, logo.Close)
		_, err = logo.Write([]byte("png"))
		Expect(err).To(Succeed())
		_, err = logo.Seek(0, 0)
		Expect(err).To(Succeed())
		logoFile, err := w.branding.UploadLogo(logo, "logo.png")
		Expect(err).To(Succeed())
		Expect(logoFile).To(Equal("logo.png"))
		for _, rel := range []string{
			"branding.json",
			filepath.Join("branding", "logo.png"),
		} {
			Expect(filepath.Join(dataDir, rel)).To(existAsFileSystemPath())
			Expect(filepath.Join(rootDir, rel)).To(beMissingFileSystemPath())
		}
	})

	ginkgo.It("starts workspace services without owning identity or branding stores", ginkgo.Label("integration"), func() {
		dataDir := filepath.Join(wikiTestTempDir(), "data")
		rootDir := filepath.Join(wikiTestTempDir(), "content")
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
		Expect(err).To(Succeed())
		ginkgo.DeferCleanup(closeWithErrorCheckForTest, w.Close)

		Expect(w.UserService()).To(BeNil())
		Expect(w.AuthService()).To(BeNil())
		Expect(w.APIKeyService()).To(BeNil())
		Expect(w.OAuthService()).To(BeNil())
		for _, rel := range []string{
			"users.db",
			"sessions.db",
			"api_keys.db",
			"oauth",
			"branding",
			"branding.json",
		} {
			Expect(filepath.Join(dataDir, rel)).To(beMissingFileSystemPath())
		}
		for _, rel := range []string{
			"search.db",
			"links.db",
			"tags.db",
			"properties.db",
			"assets",
		} {
			Expect(filepath.Join(dataDir, rel)).To(existAsFileSystemPath())
		}
		Expect(filepath.Join(rootDir, "welcome-to-leafwiki.md")).To(existAsFileSystemPath())
	})

	ginkgo.It("starts control-plane services without creating workspace stores", ginkgo.Label("integration"), func() {
		dataDir := filepath.Join(wikiTestTempDir(), "data")
		rootDir := filepath.Join(wikiTestTempDir(), "content")
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
		Expect(err).To(Succeed())
		ginkgo.DeferCleanup(closeWithErrorCheckForTest, w.Close)

		Expect(w).To(haveControlPlaneOnlyServices())
		for _, rel := range []string{
			"search.db",
			"links.db",
			"tags.db",
			"properties.db",
			"assets",
			".importer",
		} {
			Expect(filepath.Join(dataDir, rel)).To(beMissingFileSystemPath())
		}
		Expect(filepath.Join(rootDir, "welcome-to-leafwiki.md")).To(beMissingFileSystemPath())
	})

	ginkgo.It("reports crashed runtime roles through control-plane health", ginkgo.Label("integration"), func() {
		w, err := NewWiki(&WikiOptions{
			Workspace: Workspace{
				ID:      "current",
				DataDir: filepath.Join(wikiTestTempDir(), "data"),
				RootDir: filepath.Join(wikiTestTempDir(), "content"),
			},
			AuthStorageDir:      wikiTestTempDir(),
			ControlPlaneOnly:    true,
			AdminPassword:       "admin",
			JWTSecret:           "secretkey",
			AccessTokenTimeout:  15 * time.Minute,
			RefreshTokenTimeout: 7 * 24 * time.Hour,
		})
		Expect(err).To(Succeed())
		ginkgo.DeferCleanup(closeWithErrorCheckForTest, w.Close)

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

		Expect(rec).To(HaveHTTPStatus(http.StatusServiceUnavailable))
		var body struct {
			Checks map[string]string `json:"checks"`
		}
		Expect(json.Unmarshal(rec.Body.Bytes(), &body)).To(Succeed())
		Expect(body.Checks).To(haveRuntimeRoleHealth(projectdaemon.RoleWorkspaced, projectdaemon.RoleStateCrashed))
	})

	ginkgo.It("starts while reporting markdown validation errors from workspace sync", ginkgo.Label("integration"), func() {
		dataDir := filepath.Join(wikiTestTempDir(), "data")
		rootDir := filepath.Join(wikiTestTempDir(), "content")
		Expect(os.MkdirAll(rootDir, 0o755)).To(Succeed())
		invalidA := "---\nleafwiki_id: duplicate\nleafwiki_title: A\n---\n# A\n"
		invalidB := "---\nleafwiki_id: duplicate\nleafwiki_title: B\n---\n# B\n"
		Expect(os.WriteFile(filepath.Join(rootDir, "a.md"), []byte(invalidA), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(rootDir, "b.md"), []byte(invalidB), 0o644)).To(Succeed())

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
		Expect(err).To(Succeed())
		ginkgo.DeferCleanup(closeWithErrorCheckForTest, w.Close)

		Expect(w.WorkspaceSyncStatus()).To(haveWorkspaceSyncValidationPath(SatisfyAny(Equal("a.md"), Equal("b.md"))))
	})

	ginkgo.It("applies the markdown link root prefix to sync validation and link indexing", ginkgo.Label("integration"), func() {
		dataDir := filepath.Join(wikiTestTempDir(), "data")
		rootDir := filepath.Join(wikiTestTempDir(), "repo", "docs")
		Expect(os.MkdirAll(filepath.Join(rootDir, "sync"), 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(rootDir, "source.md"), []byte(`---
leafwiki_id: source
leafwiki_title: Source
---
# Source

[Glossary](/docs/sync/glossary.md)
`), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(rootDir, "sync", "glossary.md"), []byte(`---
leafwiki_id: glossary
leafwiki_title: Glossary
---
# Glossary
`), 0o644)).To(Succeed())

		w, err := NewWiki(&WikiOptions{
			Workspace:              Workspace{DataDir: dataDir, RootDir: rootDir},
			AdminPassword:          "admin",
			JWTSecret:              "secretkey",
			AccessTokenTimeout:     15 * time.Minute,
			RefreshTokenTimeout:    7 * 24 * time.Hour,
			MarkdownLinkRootPrefix: "/docs",
		})
		Expect(err).To(Succeed())
		Expect(w.WorkspaceSyncStatus().ValidationErrors).To(BeEmpty())
		source, err := w.tree.GetPage("source")
		Expect(err).To(Succeed())
		outgoing, err := w.links.GetOutgoingLinksForPage(source.ID)
		Expect(err).To(Succeed())
		Expect(outgoing).To(SatisfyAll(
			HaveField("Count", Equal(1)),
			HaveField("Outgoings", ContainElement(matchResolvedOutgoingLinkToPath(tree.RoutePath("/sync/glossary")))),
		))
		closeWithErrorCheckForTest(w.Close)
	})

	ginkgo.It("records web-created pages in workspace sync revisions", ginkgo.Label("integration"), func() {
		dataDir := filepath.Join(wikiTestTempDir(), "data")
		rootDir := filepath.Join(wikiTestTempDir(), "content")
		w, err := NewWiki(&WikiOptions{
			Workspace:           Workspace{DataDir: dataDir, RootDir: rootDir},
			AdminPassword:       "admin",
			JWTSecret:           "secretkey",
			AccessTokenTimeout:  15 * time.Minute,
			RefreshTokenTimeout: 7 * 24 * time.Hour,
		})
		Expect(err).To(Succeed())
		ginkgo.DeferCleanup(closeWithErrorCheckForTest, w.Close)
		before := w.WorkspaceSyncStatus().LastCommitHash
		Expect(before).NotTo(BeEmpty())

		page := createPageForTest(w, "alice", nil, "Synced Web Page", "synced-web-page", pageNodeKind())

		after := w.WorkspaceSyncStatus().LastCommitHash
		Expect(after).NotTo(BeEmpty())
		Expect(after).NotTo(Equal(before))
		result, err := w.WorkspaceSyncPageRevisions(context.Background(), page, "", 10)
		Expect(err).To(Succeed())
		revisions := result.Revisions
		Expect(revisions).To(ContainElement(HaveField("AuthorID", Equal("alice"))))
	})

	ginkgo.It("records imported page changes in workspace sync snapshots", ginkgo.Label("integration"), func() {
		dataDir := filepath.Join(wikiTestTempDir(), "data")
		rootDir := filepath.Join(wikiTestTempDir(), "content")
		w, err := NewWiki(&WikiOptions{
			Workspace:           Workspace{DataDir: dataDir, RootDir: rootDir},
			AdminPassword:       "admin",
			JWTSecret:           "secretkey",
			AccessTokenTimeout:  15 * time.Minute,
			RefreshTokenTimeout: 7 * 24 * time.Hour,
		})
		Expect(err).To(Succeed())
		ginkgo.DeferCleanup(closeWithErrorCheckForTest, w.Close)
		before := w.WorkspaceSyncStatus().LastCommitHash
		Expect(before).NotTo(BeEmpty())

		adapter := NewWikiImportAdapter(w)
		page, err := adapter.EnsurePath("importer-user", "imported-page", "Imported Page", pageNodeKind())
		Expect(err).To(Succeed())
		content := "Imported body\n"
		page, err = adapter.UpdatePage("importer-user", page.ID, page.Title, page.Slug, &content, &page.Kind)
		Expect(err).To(Succeed())

		after := w.WorkspaceSyncStatus().LastCommitHash
		Expect(after).NotTo(BeEmpty())
		Expect(after).NotTo(Equal(before))
		result, err := w.WorkspaceSyncPageRevisions(context.Background(), page, "", 10)
		Expect(err).To(Succeed())
		revisions := result.Revisions
		Expect(revisions).To(ContainElement(HaveField("AuthorID", Equal("importer-user"))))
		snapshots, err := w.WorkspaceSyncSnapshots(context.Background(), 5)
		Expect(err).To(Succeed())
		Expect(snapshots).To(ContainElement(SatisfyAll(
			HaveField("ID", Equal(after)),
			HaveField("AuthorID", Equal(workspacesync.ActorID("importer-user"))),
			HaveField("Source", Equal(string(workspacesync.SourceWeb))),
		)))
	})

	ginkgo.It("rebuilds derived indexes from refreshed workspace markdown", ginkgo.Label("integration"), func() {
		dataDir := filepath.Join(wikiTestTempDir(), "data")
		rootDir := filepath.Join(wikiTestTempDir(), "content")
		w, err := NewWiki(&WikiOptions{
			Workspace:           Workspace{DataDir: dataDir, RootDir: rootDir},
			AdminPassword:       "admin",
			JWTSecret:           "secretkey",
			AccessTokenTimeout:  15 * time.Minute,
			RefreshTokenTimeout: 7 * 24 * time.Hour,
		})
		Expect(err).To(Succeed())
		ginkgo.DeferCleanup(closeWithErrorCheckForTest, w.Close)

		raw := `---
leafwiki_id: indexed-page
leafwiki_title: Indexed Page
tags:
  - synced
status: draft
---

# Indexed Page

workspace-sync-search-token`
		Expect(os.WriteFile(filepath.Join(rootDir, "indexed-page.md"), []byte(raw), 0o644)).To(Succeed())

		status, err := w.WorkspaceSyncRefresh(context.Background(), workspacesync.SyncRequest{
			Reason: workspacesync.ReasonExplicit,
			Source: workspacesync.SourceFilesystem,
			Actor:  workspacesync.PublicEditorActor(),
		})
		Expect(err).To(Succeed())
		Expect(status.LastCommitHash).NotTo(BeEmpty())

		_, err = w.tree.GetPage("indexed-page")
		Expect(err).To(Succeed())
		tagged, err := w.tags.GetPageIDsByTags([]string{"synced"})
		Expect(err).To(Succeed())
		Expect(tagged).To(ConsistOf(tree.PageID("indexed-page")))
		props, err := w.props.GetPropertiesForPages([]tree.PageID{"indexed-page"})
		Expect(err).To(Succeed())
		Expect(props[newFixturePageID("indexed-page")]["status"].Value).To(Equal("draft"))
		result, err := w.searchIndex.Search("workspace-sync-search-token", nil, 0, 10)
		Expect(err).To(Succeed())
		Expect(result.Count).To(BeNumerically(">", 0))

		updatedRaw := `---
leafwiki_id: indexed-page
leafwiki_title: Indexed Page
tags:
  - resynced
status: published
---

# Indexed Page

workspace-sync-updated-token`
		Expect(os.WriteFile(filepath.Join(rootDir, "indexed-page.md"), []byte(updatedRaw), 0o644)).To(Succeed())
		_, err = w.WorkspaceSyncRefresh(context.Background(), workspacesync.SyncRequest{
			Reason: workspacesync.ReasonExplicit,
			Source: workspacesync.SourceFilesystem,
			Actor:  workspacesync.PublicEditorActor(),
		})
		Expect(err).To(Succeed())
		tagged, err = w.tags.GetPageIDsByTags([]string{"synced"})
		Expect(err).To(Succeed())
		Expect(tagged).To(BeEmpty())
		tagged, err = w.tags.GetPageIDsByTags([]string{"resynced"})
		Expect(err).To(Succeed())
		Expect(tagged).To(ConsistOf(tree.PageID("indexed-page")))
		props, err = w.props.GetPropertiesForPages([]tree.PageID{"indexed-page"})
		Expect(err).To(Succeed())
		Expect(props[newFixturePageID("indexed-page")]["status"].Value).To(Equal("published"))
		result, err = w.searchIndex.Search("workspace-sync-search-token", nil, 0, 10)
		Expect(err).To(Succeed())
		Expect(result.Count).To(BeZero())
		result, err = w.searchIndex.Search("workspace-sync-updated-token", nil, 0, 10)
		Expect(err).To(Succeed())
		Expect(result.Count).To(BeNumerically(">", 0))

		Expect(os.Remove(filepath.Join(rootDir, "indexed-page.md"))).To(Succeed())
		_, err = w.WorkspaceSyncRefresh(context.Background(), workspacesync.SyncRequest{
			Reason: workspacesync.ReasonExplicit,
			Source: workspacesync.SourceFilesystem,
			Actor:  workspacesync.PublicEditorActor(),
		})
		Expect(err).To(Succeed())
		_, err = w.tree.GetPage("indexed-page")
		Expect(err).To(MatchError(tree.ErrPageNotFound))
		tagged, err = w.tags.GetPageIDsByTags([]string{"resynced"})
		Expect(err).To(Succeed())
		Expect(tagged).To(BeEmpty())
		result, err = w.searchIndex.Search("workspace-sync-updated-token", nil, 0, 10)
		Expect(err).To(Succeed())
		Expect(result.Count).To(BeZero())
	})

})
