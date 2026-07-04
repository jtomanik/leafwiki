package wiki

import (
	"context"
	"os"
	"path/filepath"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	coreauth "github.com/perber/wiki/internal/core/auth"
	"github.com/perber/wiki/internal/core/tree"
	httpinternal "github.com/perber/wiki/internal/http"
	wikipages "github.com/perber/wiki/internal/wiki/pages"
	"github.com/perber/wiki/internal/workspacesync"
)

var _ = ginkgo.Describe("wiki runtime behavior", func() {
	ginkgo.It("serves native stdio tools as the public editor when auth is disabled", ginkgo.Label("integration"), func() {
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

	ginkgo.It("marks MCP writes as workspace sync revisions and serves revision history", ginkgo.Label("integration"), func() {
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

	ginkgo.It("rejects workspaces whose data and content directories are the same", ginkgo.Label("unit"), func() {
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

	ginkgo.It("rejects workspaces whose content directory contains service state", ginkgo.Label("unit"), func() {
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

	ginkgo.It("normalizes workspace paths before services create state", ginkgo.Label("integration"), func() {
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

	ginkgo.It("refuses non-recursive deletion of a page with children", ginkgo.Label("integration"), func() {
		w := createWikiTestInstance()
		ginkgo.DeferCleanup(closeWithErrorCheckForTest, w.Close)
		parent := createPageForTest(w, "system", nil, "Parent", "parent", pageNodeKind())
		createPageForTest(w, "system", pageIDPtr(parent.ID), "Child", "child", pageNodeKind())

		err := wikipages.NewDeletePageUseCase(w.tree, w.asset, w.newPageOrchestrator(), w.log).Execute(
			context.Background(),
			wikipages.DeletePageInput{UserID: "system", ID: parent.ID, Version: tree.PageVersionFromString(parent.Version()), Recursive: false},
		)
		Expect(err).To(MatchError(tree.ErrPageHasChildren))
	})

	ginkgo.It("removes a page subtree recursively", ginkgo.Label("integration"), func() {
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

	ginkgo.It("creates the default administrator with the configured password", ginkgo.Label("integration"), func() {
		w := createWikiTestInstance()
		ginkgo.DeferCleanup(closeWithErrorCheckForTest, w.Close)

		_, err := w.user.GetUserByEmailOrUsernameAndPassword("admin", "admin")
		Expect(err).To(Succeed())
	})

	ginkgo.It("accepts default admin credentials and rejects invalid credentials", ginkgo.Label("integration"), func() {
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

	ginkgo.It("starts without an auth service when authentication is disabled", ginkgo.Label("integration"), func() {
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

	ginkgo.It("keeps login unavailable when authentication is disabled", ginkgo.Label("integration"), func() {
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

	ginkgo.It("keeps logout unavailable when authentication is disabled", ginkgo.Label("integration"), func() {
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

	ginkgo.It("keeps refresh tokens unavailable when authentication is disabled", ginkgo.Label("integration"), func() {
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

	ginkgo.It("allows page workflows when authentication is disabled", ginkgo.Label("integration"), func() {
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
