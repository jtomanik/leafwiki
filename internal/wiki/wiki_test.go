package wiki

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"
	coreauth "github.com/perber/wiki/internal/core/auth"
	"github.com/perber/wiki/internal/core/tree"
	httpinternal "github.com/perber/wiki/internal/http"
	"github.com/perber/wiki/internal/projectdaemon"
	wikihealth "github.com/perber/wiki/internal/wiki/health"
	wikipages "github.com/perber/wiki/internal/wiki/pages"
	"github.com/perber/wiki/internal/workspaceid"
	"github.com/perber/wiki/internal/workspacesync"
)

func closeWithErrorCheckForTest(closer func() error) {
	ginkgo.GinkgoHelper()

	Expect(closer()).To(Succeed())
}

func existAsFileSystemPath() types.GomegaMatcher {
	return WithTransform(func(path string) error {
		_, err := os.Stat(path)
		return err
	}, Succeed())
}

func beMissingFileSystemPath() types.GomegaMatcher {
	return WithTransform(func(path string) error {
		_, err := os.Stat(path)
		return err
	}, MatchError(os.ErrNotExist))
}

type wikiServiceSet struct {
	User          any
	Auth          any
	APIKeys       any
	OAuth         any
	Branding      any
	Tree          any
	Asset         any
	Search        any
	Links         any
	Tags          any
	Properties    any
	WorkspaceSync any
}

func haveControlPlaneOnlyServices() types.GomegaMatcher {
	return WithTransform(func(w *Wiki) wikiServiceSet {
		return wikiServiceSet{
			User:          w.user,
			Auth:          w.auth,
			APIKeys:       w.apiKeys,
			OAuth:         w.oauth,
			Branding:      w.branding,
			Tree:          w.tree,
			Asset:         w.asset,
			Search:        w.searchIndex,
			Links:         w.links,
			Tags:          w.tags,
			Properties:    w.props,
			WorkspaceSync: w.workspaceSync,
		}
	}, gstruct.MatchAllFields(gstruct.Fields{
		"User":          Not(BeNil()),
		"Auth":          Not(BeNil()),
		"APIKeys":       Not(BeNil()),
		"OAuth":         Not(BeNil()),
		"Branding":      Not(BeNil()),
		"Tree":          BeNil(),
		"Asset":         BeNil(),
		"Search":        BeNil(),
		"Links":         BeNil(),
		"Tags":          BeNil(),
		"Properties":    BeNil(),
		"WorkspaceSync": BeNil(),
	}))
}

func haveWorkspaceSyncValidationPath(path types.GomegaMatcher) types.GomegaMatcher {
	return SatisfyAll(
		HaveField("LastCommitHash", Not(BeEmpty())),
		HaveField("ValidationErrors", ContainElement(HaveField("Path", path))),
	)
}

type runtimeHealthWireSnapshot struct {
	Healthy bool
	Checks  map[string]string
}

func runtimeHealthWireSnapshotFor(required []projectdaemon.RoleName, roles []projectdaemon.RoleHealth) runtimeHealthWireSnapshot {
	ginkgo.GinkgoHelper()

	useCase := wikihealth.NewHealthUseCase(nil, nil, wikiTestTempDir(), wikihealth.HealthUseCaseOptions{
		RequiredRoles: required,
		RoleHealth: func() []projectdaemon.RoleHealth {
			return roles
		},
	})
	healthy, checks := useCase.Execute()
	return runtimeHealthWireSnapshot{
		Healthy: healthy,
		Checks:  checks.HTTPMap(),
	}
}

func haveRuntimeRoleHealth(role projectdaemon.RoleName, state projectdaemon.RoleState) types.GomegaMatcher {
	base := runtimeHealthWireSnapshotFor(nil, nil)
	withRole := runtimeHealthWireSnapshotFor([]projectdaemon.RoleName{role}, []projectdaemon.RoleHealth{{Name: role, State: state}})

	matchers := make([]types.GomegaMatcher, 0, len(withRole.Checks))
	for key, value := range withRole.Checks {
		if _, ok := base.Checks[key]; ok {
			continue
		}
		matchers = append(matchers, HaveKeyWithValue(key, value))
	}
	return SatisfyAll(matchers...)
}

type mcpToolSuccessMatcher struct {
	structuredContent types.GomegaMatcher
}

func haveSuccessfulMCPToolResult(structuredContent types.GomegaMatcher) types.GomegaMatcher {
	return mcpToolSuccessMatcher{structuredContent: structuredContent}
}

func (matcher mcpToolSuccessMatcher) Match(actual any) (bool, error) {
	result, ok := actual.(*sdkmcp.CallToolResult)
	if !ok {
		return false, fmt.Errorf("expected *mcp.CallToolResult, got %T", actual)
	}
	if result == nil || result.IsError {
		return false, nil
	}
	return matcher.structuredContent.Match(result.StructuredContent)
}

func (matcher mcpToolSuccessMatcher) FailureMessage(actual any) string {
	return fmt.Sprintf("Expected\n\t%#v\nto be a successful MCP tool result with matching structured content", actual)
}

func (matcher mcpToolSuccessMatcher) NegatedFailureMessage(actual any) string {
	return fmt.Sprintf("Expected\n\t%#v\nnot to be a successful MCP tool result with matching structured content", actual)
}

func haveSuccessfulMCPRevisionHistory(count int) types.GomegaMatcher {
	return haveSuccessfulMCPToolResult(HaveKeyWithValue("revisions", HaveLen(count)))
}

func mcpCreatedPageID(result *sdkmcp.CallToolResult) tree.PageID {
	content, ok := result.StructuredContent.(map[string]any)
	if !ok {
		return ""
	}
	page, ok := content["page"].(map[string]any)
	if !ok {
		return ""
	}
	id, ok := page["id"].(string)
	if !ok {
		return ""
	}
	return tree.PageIDFromString(id)
}

func createWikiTestInstance() *Wiki {
	ginkgo.GinkgoHelper()

	wikiInstance, err := NewWiki(&WikiOptions{
		StorageDir:          wikiTestTempDir(),
		AdminPassword:       "admin",
		JWTSecret:           "secretkey",
		AccessTokenTimeout:  15 * time.Minute,
		RefreshTokenTimeout: 7 * 24 * time.Hour,
	})
	Expect(err).To(Succeed())
	return wikiInstance
}

func createWikiTestInstanceWithWorkspace(workspace Workspace) *Wiki {
	ginkgo.GinkgoHelper()

	wikiInstance, err := NewWiki(&WikiOptions{
		Workspace:           workspace,
		AdminPassword:       "admin",
		JWTSecret:           "secretkey",
		AccessTokenTimeout:  15 * time.Minute,
		RefreshTokenTimeout: 7 * 24 * time.Hour,
	})
	Expect(err).To(Succeed())
	return wikiInstance
}

func pageNodeKind() *tree.NodeKind {
	kind := tree.NodeKindPage
	return &kind
}

func pageIDPtr(id tree.PageID) *tree.PageID {
	return &id
}

func createPageForTest(w *Wiki, userID string, parentID *tree.PageID, title, slug string, kind *tree.NodeKind) *tree.Page {
	ginkgo.GinkgoHelper()

	out, err := wikipages.NewCreatePageUseCase(w.tree, w.slug, w.newPageOrchestrator(), w.log).Execute(
		context.Background(),
		wikipages.CreatePageInput{UserID: newFixtureUserID(userID), ParentID: parentID, Title: title, Slug: newFixtureSlug(slug), Kind: kind},
	)
	Expect(err).To(Succeed())
	return out.Page
}

func updatePageForTest(w *Wiki, userID string, id tree.PageID, title, slug string, content *string, kind *tree.NodeKind) *tree.Page {
	ginkgo.GinkgoHelper()

	current, err := w.tree.GetPage(id)
	Expect(err).To(Succeed())

	out, err := wikipages.NewUpdatePageUseCase(w.tree, w.slug, w.newPageOrchestrator(), w.log).Execute(
		context.Background(),
		wikipages.UpdatePageInput{UserID: newFixtureUserID(userID), ID: id, Version: newFixturePageVersion(current.Version()), Title: title, Slug: newFixtureSlug(slug), Content: content, Kind: kind},
	)
	Expect(err).To(Succeed())
	return out.Page
}

func deletePageForTest(w *Wiki, userID string, id tree.PageID, recursive bool) {
	ginkgo.GinkgoHelper()

	current, err := w.tree.GetPage(id)
	Expect(err).To(Succeed())

	err = wikipages.NewDeletePageUseCase(w.tree, w.asset, w.newPageOrchestrator(), w.log).Execute(
		context.Background(),
		wikipages.DeletePageInput{UserID: newFixtureUserID(userID), ID: id, Version: newFixturePageVersion(current.Version()), Recursive: recursive},
	)
	Expect(err).To(Succeed())
}

func mcpServerStoppedSuccessfully(err error) bool {
	return err == nil || errors.Is(err, context.Canceled)
}

var _ = ginkgo.Describe("wiki runtime behavior", func() {
	ginkgo.It("removes a leaf page from the tree", func() {
		w := createWikiTestInstance()
		ginkgo.DeferCleanup(closeWithErrorCheckForTest, w.Close)
		page := createPageForTest(w, "system", nil, "Trash", "trash", pageNodeKind())
		deletePageForTest(w, "system", page.ID, false)
		_, err := w.tree.GetPage(page.ID)
		Expect(err).To(MatchError(tree.ErrPageNotFound))
	})

	ginkgo.It("keeps the legacy storage directory layout for the default workspace", func() {
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

	ginkgo.It("stores workspace content in the root directory and service state in the data directory", func() {
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

	ginkgo.It("starts workspace services without owning identity or branding stores", func() {
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

	ginkgo.It("starts control-plane services without creating workspace stores", func() {
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

	ginkgo.It("reports crashed runtime roles through control-plane health", func() {
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

	ginkgo.It("starts while reporting markdown validation errors from workspace sync", func() {
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

	ginkgo.It("applies the markdown link root prefix to sync validation and link indexing", func() {
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
			HaveField("Outgoings", ContainElement(SatisfyAll(
				HaveField("ToPath", Equal(tree.RoutePath("/sync/glossary"))),
				HaveField("Broken", BeFalse()),
			))),
		))
		closeWithErrorCheckForTest(w.Close)
	})

	ginkgo.It("records web-created pages in workspace sync revisions", func() {
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

	ginkgo.It("records imported page changes in workspace sync snapshots", func() {
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

	ginkgo.It("rebuilds derived indexes from refreshed workspace markdown", func() {
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

	ginkgo.It("serves native stdio tools as the public editor when auth is disabled", func() {
		dataDir := filepath.Join(wikiTestTempDir(), "data")
		rootDir := filepath.Join(wikiTestTempDir(), "content")
		w, err := NewWiki(&WikiOptions{
			Workspace:           Workspace{ID: "default", DataDir: dataDir, RootDir: rootDir},
			AdminPassword:       "admin",
			JWTSecret:           "secretkey",
			AccessTokenTimeout:  15 * time.Minute,
			RefreshTokenTimeout: 7 * 24 * time.Hour,
			AuthDisabled:        true,
		})
		Expect(err).To(Succeed())
		ginkgo.DeferCleanup(closeWithErrorCheckForTest, w.Close)

		serverTransport, clientTransport := sdkmcp.NewInMemoryTransports()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		ginkgo.DeferCleanup(cancel)
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
		Expect(err).To(Succeed())
		ginkgo.DeferCleanup(func() {
			_ = session.Close()
		})

		tools, err := session.ListTools(ctx, &sdkmcp.ListToolsParams{})
		Expect(err).To(Succeed())
		Expect(tools.Tools).To(SatisfyAll(
			ContainElement(HaveField("Name", Equal("wiki_create_page"))),
			ContainElement(HaveField("Name", Equal("wiki_get_current_user"))),
		))

		current, err := session.CallTool(ctx, &sdkmcp.CallToolParams{Name: "wiki_get_current_user"})
		Expect(err).To(Succeed())
		Expect(current.StructuredContent).To(HaveKeyWithValue("user", SatisfyAll(
			HaveKeyWithValue("username", "public-editor"),
			HaveKeyWithValue("role", "editor"),
		)))

		Expect(session.Close()).To(Succeed())
		Eventually(serverDone).WithTimeout(10 * time.Second).Should(Receive(Satisfy(mcpServerStoppedSuccessfully)))
	})

	ginkgo.It("marks MCP writes as workspace sync revisions and serves revision history", func() {
		dataDir := filepath.Join(wikiTestTempDir(), "data")
		rootDir := filepath.Join(wikiTestTempDir(), "content")
		w, err := NewWiki(&WikiOptions{
			Workspace:           Workspace{ID: "default", DataDir: dataDir, RootDir: rootDir},
			AdminPassword:       "admin",
			JWTSecret:           "secretkey",
			AccessTokenTimeout:  15 * time.Minute,
			RefreshTokenTimeout: 7 * 24 * time.Hour,
			AuthDisabled:        true,
		})
		Expect(err).To(Succeed())
		ginkgo.DeferCleanup(closeWithErrorCheckForTest, w.Close)

		serverTransport, clientTransport := sdkmcp.NewInMemoryTransports()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		ginkgo.DeferCleanup(cancel)
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
		Expect(err).To(Succeed())
		ginkgo.DeferCleanup(func() {
			_ = session.Close()
		})

		tools, err := session.ListTools(ctx, &sdkmcp.ListToolsParams{})
		Expect(err).To(Succeed())
		Expect(tools.Tools).To(ContainElement(HaveField("Name", Equal("wiki_list_revisions"))))

		created, err := session.CallTool(ctx, &sdkmcp.CallToolParams{
			Name: "wiki_create_page",
			Arguments: map[string]any{
				"title": "MCP Synced",
				"slug":  "mcp-synced",
				"kind":  "page",
			},
		})
		Expect(err).To(Succeed())
		Expect(created).To(haveSuccessfulMCPToolResult(HaveKeyWithValue("page", HaveKey("id"))))
		pageID := mcpCreatedPageID(created)
		createdTreePage, err := w.tree.GetPage(pageID)
		Expect(err).To(Succeed())
		Expect(createdTreePage.Title).To(Equal("MCP Synced"))

		snapshots, err := w.WorkspaceSyncSnapshots(ctx, 5)
		Expect(err).To(Succeed())
		Expect(snapshots).To(ContainElement(HaveField("Source", Equal(string(workspacesync.SourceMCP)))))

		revisions, err := session.CallTool(ctx, &sdkmcp.CallToolParams{
			Name: "wiki_list_revisions",
			Arguments: map[string]any{
				"pageId": pageID.MetadataValue(),
			},
		})
		Expect(err).To(Succeed())
		Expect(revisions).To(haveSuccessfulMCPRevisionHistory(1))

		Expect(session.Close()).To(Succeed())
		Eventually(serverDone).WithTimeout(10 * time.Second).Should(Receive(Satisfy(mcpServerStoppedSuccessfully)))
	})

	ginkgo.It("rejects workspaces whose data and content directories are the same", func() {
		dir := wikiTestTempDir()

		_, err := NewWiki(&WikiOptions{
			Workspace:           Workspace{ID: "default", DataDir: dir, RootDir: filepath.Clean(filepath.Join(dir, "."))},
			AdminPassword:       "admin",
			JWTSecret:           "secretkey",
			AccessTokenTimeout:  15 * time.Minute,
			RefreshTokenTimeout: 7 * 24 * time.Hour,
		})
		Expect(err).To(MatchError(ErrWorkspaceRootDirEqualsDataDir))
	})

	ginkgo.It("rejects workspaces whose content directory contains service state", func() {
		rootDir := filepath.Join(wikiTestTempDir(), "wiki")
		dataDir := filepath.Join(rootDir, "data")

		_, err := NewWiki(&WikiOptions{
			Workspace:           Workspace{ID: "default", DataDir: dataDir, RootDir: rootDir},
			AdminPassword:       "admin",
			JWTSecret:           "secretkey",
			AccessTokenTimeout:  15 * time.Minute,
			RefreshTokenTimeout: 7 * 24 * time.Hour,
		})
		Expect(err).To(MatchError(ErrWorkspaceRootDirContainsDataDir))
		Expect(rootDir).To(beMissingFileSystemPath())
	})

	ginkgo.It("normalizes workspace paths before services create state", func() {
		baseDir := wikiTestTempDir()
		dataDir := filepath.Join(baseDir, "data")
		rootDir := filepath.Join(baseDir, "content")

		w, err := NewWiki(&WikiOptions{
			Workspace:           Workspace{ID: "default", DataDir: " " + dataDir + string(os.PathSeparator) + "." + " ", RootDir: " " + rootDir + string(os.PathSeparator) + "." + " "},
			AdminPassword:       "admin",
			JWTSecret:           "secretkey",
			AccessTokenTimeout:  15 * time.Minute,
			RefreshTokenTimeout: 7 * 24 * time.Hour,
		})
		Expect(err).To(Succeed())
		ginkgo.DeferCleanup(closeWithErrorCheckForTest, w.Close)

		Expect(w.GetStorageDir()).To(Equal(dataDir))
		Expect(w.GetRootDir()).To(Equal(rootDir))
		Expect(filepath.Join(dataDir, "users.db")).To(existAsFileSystemPath())
		Expect(filepath.Join(rootDir, "welcome-to-leafwiki.md")).To(existAsFileSystemPath())
	})

	ginkgo.It("refuses non-recursive deletion of a page with children", func() {
		w := createWikiTestInstance()
		ginkgo.DeferCleanup(closeWithErrorCheckForTest, w.Close)
		parent := createPageForTest(w, "system", nil, "Parent", "parent", pageNodeKind())
		createPageForTest(w, "system", pageIDPtr(parent.ID), "Child", "child", pageNodeKind())

		err := wikipages.NewDeletePageUseCase(w.tree, w.asset, w.newPageOrchestrator(), w.log).Execute(
			context.Background(),
			wikipages.DeletePageInput{UserID: "system", ID: parent.ID, Version: newFixturePageVersion(parent.Version()), Recursive: false},
		)
		Expect(err).To(MatchError(tree.ErrPageHasChildren))
	})

	ginkgo.It("removes a page subtree recursively", func() {
		w := createWikiTestInstance()
		ginkgo.DeferCleanup(closeWithErrorCheckForTest, w.Close)
		parent := createPageForTest(w, "system", nil, "Parent", "parent", pageNodeKind())
		child := createPageForTest(w, "system", pageIDPtr(parent.ID), "Child", "child", pageNodeKind())

		deletePageForTest(w, "system", parent.ID, true)
		_, err := w.tree.GetPage(parent.ID)
		Expect(err).To(MatchError(tree.ErrPageNotFound))
		_, err = w.tree.GetPage(child.ID)
		Expect(err).To(MatchError(tree.ErrPageNotFound))
	})

	ginkgo.It("creates the default administrator with the configured password", func() {
		w := createWikiTestInstance()
		ginkgo.DeferCleanup(closeWithErrorCheckForTest, w.Close)

		_, err := w.user.GetUserByEmailOrUsernameAndPassword("admin", "admin")
		Expect(err).To(Succeed())
	})

	ginkgo.It("accepts default admin credentials and rejects invalid credentials", func() {
		w := createWikiTestInstance()
		ginkgo.DeferCleanup(closeWithErrorCheckForTest, w.Close)

		authSvc := w.auth
		Expect(authSvc).NotTo(BeNil())

		token, err := authSvc.Login("admin", "admin")
		Expect(err).To(Succeed())
		Expect(token).NotTo(BeNil())

		_, err = authSvc.Login("admin", "wrong")
		Expect(err).To(MatchError(coreauth.ErrUserInvalidCredentials))
	})

	ginkgo.It("starts without an auth service when authentication is disabled", func() {
		// Create a wiki instance with AuthDisabled set to true
		wikiInstance, err := NewWiki(&WikiOptions{
			StorageDir:          wikiTestTempDir(),
			AdminPassword:       "",
			JWTSecret:           "",
			AccessTokenTimeout:  0,
			RefreshTokenTimeout: 0,
			AuthDisabled:        true,
		})
		Expect(err).To(Succeed())
		ginkgo.DeferCleanup(closeWithErrorCheckForTest, wikiInstance.Close)

		// Verify that the auth service is nil
		Expect(wikiInstance.auth).To(BeNil())
	})

	ginkgo.It("keeps login unavailable when authentication is disabled", func() {
		// Create a wiki instance with AuthDisabled set to true
		wikiInstance, err := NewWiki(&WikiOptions{
			StorageDir:   wikiTestTempDir(),
			AuthDisabled: true,
		})
		Expect(err).To(Succeed())
		ginkgo.DeferCleanup(closeWithErrorCheckForTest, wikiInstance.Close)

		// Auth operations are unavailable when auth is disabled.
		Expect(wikiInstance.auth).To(BeNil())
	})

	ginkgo.It("keeps logout unavailable when authentication is disabled", func() {
		// Create a wiki instance with AuthDisabled set to true
		wikiInstance, err := NewWiki(&WikiOptions{
			StorageDir:   wikiTestTempDir(),
			AuthDisabled: true,
		})
		Expect(err).To(Succeed())
		ginkgo.DeferCleanup(closeWithErrorCheckForTest, wikiInstance.Close)

		// Auth operations are unavailable when auth is disabled.
		Expect(wikiInstance.auth).To(BeNil())
	})

	ginkgo.It("keeps refresh tokens unavailable when authentication is disabled", func() {
		// Create a wiki instance with AuthDisabled set to true
		wikiInstance, err := NewWiki(&WikiOptions{
			StorageDir:   wikiTestTempDir(),
			AuthDisabled: true,
		})
		Expect(err).To(Succeed())
		ginkgo.DeferCleanup(closeWithErrorCheckForTest, wikiInstance.Close)

		// Auth operations are unavailable when auth is disabled.
		Expect(wikiInstance.auth).To(BeNil())
	})

	ginkgo.It("allows page workflows when authentication is disabled", func() {
		// Create a wiki instance with AuthDisabled set to true
		wikiInstance, err := NewWiki(&WikiOptions{
			StorageDir:   wikiTestTempDir(),
			AuthDisabled: true,
		})
		Expect(err).To(Succeed())
		ginkgo.DeferCleanup(closeWithErrorCheckForTest, wikiInstance.Close)

		// Test creating a page
		page := createPageForTest(wikiInstance, "system", nil, "Test Page", "test-page", pageNodeKind())

		Expect(page.Title).To(Equal("Test Page"))

		// Test updating a page
		var updatedContent = "# Content"
		updatedPage := updatePageForTest(wikiInstance, "system", page.ID, "Updated Title", "updated-slug", &updatedContent, pageNodeKind())

		Expect(updatedPage.Title).To(Equal("Updated Title"))

		// Test getting a page
		retrievedPage, err := wikiInstance.tree.GetPage(page.ID)
		Expect(err).To(Succeed())

		Expect(retrievedPage.ID).To(Equal(page.ID))

		// Test deleting a page
		deletePageForTest(wikiInstance, "system", page.ID, false)
	})
})
