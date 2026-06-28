package mcp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"time"

	sdkauth "github.com/modelcontextprotocol/go-sdk/auth"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	coreassets "github.com/perber/wiki/internal/core/assets"
	coreauth "github.com/perber/wiki/internal/core/auth"
	wikivalidation "github.com/perber/wiki/internal/core/markdownvalidation"
	"github.com/perber/wiki/internal/core/tree"
	httpinternal "github.com/perber/wiki/internal/http"
	"github.com/perber/wiki/internal/projectdaemon"
	wikiassets "github.com/perber/wiki/internal/wiki/assets"
	wikipresence "github.com/perber/wiki/internal/wiki/presence"
	"github.com/perber/wiki/internal/workspacesync"
)

var _ = Describe("MCP additional deterministic coverage", func() {
	Describe("checkpoint eviction", func() {
		It("evicts overflow and new sessions while preserving the active session", func() {
			base := time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
			store := newContextCheckpointStore(1)
			store.maxSessions = 2
			store.sessions = map[string][]contextCheckpoint{
				"old":  {{Token: "old", CreatedAt: base}},
				"new":  {{Token: "new", CreatedAt: base.Add(time.Minute)}},
				"keep": {{Token: "keep", CreatedAt: base.Add(-time.Hour)}},
			}

			store.evictOverflowLocked("keep")

			Expect(store.sessions).NotTo(HaveKey("old"))
			Expect(store.sessions).To(HaveKey("new"))
			Expect(store.sessions).To(HaveKey("keep"))

			store.sessions = map[string][]contextCheckpoint{
				"old": {{Token: "old", CreatedAt: base}},
				"new": {{Token: "new", CreatedAt: base.Add(time.Minute)}},
			}

			store.evictForNewSessionLocked("incoming")

			Expect(store.sessions).NotTo(HaveKey("old"))
			Expect(store.sessions).To(HaveKey("new"))

			onlyStore := newContextCheckpointStore(1)
			onlyStore.maxSessions = 1
			onlyStore.sessions = map[string][]contextCheckpoint{
				"only": {{Token: "only", CreatedAt: base}},
			}

			onlyStore.evictForNewSessionLocked("only")

			Expect(onlyStore.sessions).To(HaveKey("only"))
		})
	})

	Describe("actor and route guards", func() {
		It("resolves token-info actors and distinguishes not-found from lookup failures", func() {
			t := GinkgoT()
			userDir := t.TempDir()
			store, err := coreauth.NewUserStore(userDir)
			Expect(err).NotTo(HaveOccurred())
			t.Cleanup(func() {
				Expect(store.Close()).To(Succeed())
			})
			userService := coreauth.NewUserService(store)
			editor, err := userService.CreateUser("editor", "editor@example.com", "password", coreauth.RoleEditor)
			Expect(err).NotTo(HaveOccurred())
			routes := &Routes{userService: userService}

			user, err := routes.actorForRequest(mcpTokenInfoRequest(editor.ID))
			Expect(err).NotTo(HaveOccurred())
			Expect(user.ID).To(Equal(editor.ID))
			Expect(user.Role).To(Equal(coreauth.RoleEditor))

			user, err = routes.actorForRequest(mcpTokenInfoRequest("missing-user"))
			Expect(user).To(BeNil())
			assertLocalizedErrorCode(t, err, errCodeMCPAuthenticatedUserNotFound, "errors.mcp.authenticated_user_not_found")

			blocker := beginExclusiveMCPTestSQLiteTransaction(t, filepath.Join(userDir, "users.db"))
			defer blocker.rollback(t)

			user, err = routes.actorForRequest(mcpTokenInfoRequest(editor.ID))
			Expect(user).To(BeNil())
			assertLocalizedErrorCode(t, err, errCodeMCPAuthenticatedUserLookupFailed, "errors.mcp.authenticated_user_lookup_failed")
		})

		It("requires private actor context headers when configured and propagates editor auth failures", func() {
			t := GinkgoT()
			routes := &Routes{actorContextAllowed: true, actorContextRequired: true}

			user, ok, err := routes.actorFromPrivateContextHeader(http.Header{})

			Expect(ok).To(BeTrue())
			Expect(user).To(BeNil())
			assertLocalizedErrorCode(t, err, errCodeMCPActorContextMissing, "errors.mcp.actor_context_missing")

			user, err = (&Routes{}).editorActorForRequest(nil)

			Expect(user).To(BeNil())
			assertLocalizedErrorCode(t, err, errCodeMCPTokenInfoMissing, "errors.mcp.token_info_missing")
		})

		It("covers private API-key success, verifier absence, and actor-context HTTP factory paths", func() {
			t := GinkgoT()
			_, apiKeyService, _, created, _ := newMCPAPIKeyAuthFixture(t)
			called := false
			handler := (&Routes{apiKeys: apiKeyService}).requirePrivateStdioAPIKey(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				called = true
				w.WriteHeader(http.StatusNoContent)
			}))
			req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
			req.Header.Set("Authorization", "Bearer "+created.Secret)
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			Expect(rec.Code).To(Equal(http.StatusNoContent))
			Expect(called).To(BeTrue())

			_, err := (&Routes{}).verifyBearerToken(context.Background(), created.Secret, nil)
			Expect(errors.Is(err, sdkauth.ErrInvalidToken)).To(BeTrue())
			Expect(err).To(MatchError(ContainSubstring("api key verifier unavailable")))

			actorHandler := (&Routes{}).NewActorContextHTTPHandler(httpinternal.RouterOptions{})
			actorHandler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/mcp", nil))

			server := httptest.NewServer(actorHandler)
			defer server.Close()
			client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "leafwiki-test", Version: "test"}, nil)
			session, err := client.Connect(context.Background(), &sdkmcp.StreamableClientTransport{
				Endpoint:             server.URL + "/mcp",
				HTTPClient:           server.Client(),
				DisableStandaloneSSE: true,
			}, nil)
			Expect(err).NotTo(HaveOccurred())
			defer session.Close()

			_, storageFailureService, _, storageFailureKey, apiKeyDBPath := newMCPAPIKeyAuthFixture(t)
			blocker := beginExclusiveMCPTestSQLiteTransaction(t, apiKeyDBPath)
			defer blocker.rollback(t)
			failingHandler := (&Routes{apiKeys: storageFailureService}).requirePrivateStdioAPIKey(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			}))
			req = httptest.NewRequest(http.MethodPost, "/mcp", nil)
			req.Header.Set("Authorization", "Bearer "+storageFailureKey.Secret)
			rec = httptest.NewRecorder()

			failingHandler.ServeHTTP(rec, req)

			Expect(rec.Code).To(Equal(http.StatusServiceUnavailable))
		})
	})

	Describe("schema and context helpers", func() {
		It("covers default schema and optional gate fallbacks", func() {
			Expect(toolOutputSchema(ToolID("unknown_tool")).Type).To(Equal("object"))
			Expect(toolNamesForGate(optionalToolGate("unknown"))).To(BeNil())
		})

		It("covers context helper edge branches", func() {
			now := time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
			routes := newContextToolTestRoutes(GinkgoT())
			routes.webPresenceProvider = func(*coreauth.User) ([]wikipresence.Session, error) {
				return []wikipresence.Session{{Type: wikipresence.SessionTypeWeb, SessionID: "web-session"}}, nil
			}
			routes.agentPresenceProvider = func() ([]projectdaemon.AgentPresenceSession, error) {
				return []projectdaemon.AgentPresenceSession{{SessionIDHash: "agent-session", Provider: "codex", FirstSeenAt: now, LastSeenAt: now}}, nil
			}

			sessions, status := routes.activeSessionsForContext(&coreauth.User{ID: "editor", Role: coreauth.RoleEditor})

			Expect(status.Web).To(Equal("enabled"))
			Expect(status.AgentHooks).To(Equal("enabled"))
			Expect(sessions).To(HaveLen(2))
			Expect(sessions[0].Type).To(Equal(wikipresence.SessionTypeAgent))
			Expect(sessions[1].Type).To(Equal(wikipresence.SessionTypeWeb))

			huge := 99
			Expect(shouldRefreshForContext(contextSyncModeForce, workspacesync.SyncStatus{})).To(BeTrue())
			Expect(shouldRefreshForContext(contextSyncModeNone, workspacesync.SyncStatus{Enabled: true, PendingEventCount: 1})).To(BeFalse())
			Expect(contextSnapshotPageSize(25)).To(Equal(25))
			Expect(contextSnapshotPageSize(75)).To(Equal(50))
			Expect(boundedContextTreeDepth(&huge)).To(Equal(treeDisplayDepth(maxContextTreeDepth)))
			one := 1
			Expect(boundedContextTreeDepth(&one)).To(Equal(treeDisplayDepth(1)))
			Expect((&Routes{treeService: tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: GinkgoT().TempDir(), RootDir: GinkgoT().TempDir()})}).contextTree(1)).To(BeNil())
			Expect(func() { ensureNodeChildrenArray(nil) }).NotTo(Panic())

			_, err := routes.getContext(context.Background(), nil, toolActor{ID: "viewer", User: &coreauth.User{ID: "viewer", Username: "viewer", Role: coreauth.RoleViewer}}, httpinternal.RouterOptions{}, getContextInput{SyncMode: "invalid"})
			Expect(err).To(MatchError("syncMode must be auto, force, or none"))

			refreshCalled := false
			routes.workspaceSyncRefresh = func(context.Context, workspacesync.SyncRequest) (workspacesync.SyncStatus, error) {
				refreshCalled = true
				return workspacesync.SyncStatus{}, nil
			}
			routes.workspaceSyncStatus = func() workspacesync.SyncStatus {
				return workspacesync.SyncStatus{Enabled: true, PendingEventCount: 1}
			}
			out, err := routes.getContext(context.Background(), nil, toolActor{ID: "viewer", User: &coreauth.User{ID: "viewer", Username: "viewer", Role: coreauth.RoleViewer}}, httpinternal.RouterOptions{}, getContextInput{})
			Expect(err).NotTo(HaveOccurred())
			Expect(refreshCalled).To(BeFalse())
			Expect(out.Warnings).To(ContainElement("sync refresh skipped because current MCP user is not an editor or admin"))

			routes.listWorkspaceSnapshots = func(context.Context, workspacesync.CommitHash, workspacesync.SnapshotLimit) (workspacesync.SnapshotList, error) {
				return workspacesync.SnapshotList{
					Snapshots: []workspacesync.Snapshot{
						{ID: "first", ChangedMarkdownPaths: []string{"home.md"}},
						{ID: "second", ChangedMarkdownPaths: []string{"home.md"}},
					},
				}, nil
			}
			Expect(routes.recentChanges(context.Background(), workspacesync.SyncStatus{}, 1)).To(HaveLen(1))

			changes, ok := routes.changesSinceCommit(context.Background(), workspacesync.SyncStatus{}, "")
			Expect(ok).To(BeTrue())
			Expect(changes).To(HaveLen(2))

			routes.listWorkspaceSnapshots = nil
			changes, ok = routes.changesSinceCommit(context.Background(), workspacesync.SyncStatus{}, "missing")
			Expect(ok).To(BeFalse())
			Expect(changes).To(BeNil())

			routes.listWorkspaceSnapshots = func(context.Context, workspacesync.CommitHash, workspacesync.SnapshotLimit) (workspacesync.SnapshotList, error) {
				return workspacesync.SnapshotList{
					Snapshots: []workspacesync.Snapshot{{ID: "newer"}},
				}, errors.New("snapshot backend failed")
			}
			changes, ok = routes.changesSinceCommit(context.Background(), workspacesync.SyncStatus{}, "target")
			Expect(ok).To(BeFalse())
			Expect(changes).To(HaveLen(0))

			routes.listWorkspaceSnapshots = func(_ context.Context, _ workspacesync.CommitHash, limit workspacesync.SnapshotLimit) (workspacesync.SnapshotList, error) {
				snapshots := make([]workspacesync.Snapshot, int(limit))
				for i := range snapshots {
					snapshots[i] = workspacesync.Snapshot{ID: workspacesync.CommitHashFromString(fmt.Sprintf("snapshot-%d", i))}
				}
				return workspacesync.SnapshotList{Snapshots: snapshots, NextCursor: "next"}, nil
			}
			changes, ok = routes.changesSinceCommit(context.Background(), workspacesync.SyncStatus{}, "target")
			Expect(ok).To(BeFalse())
			Expect(changes).To(HaveLen(maxContextDeltaSnapshots))

			routes.listWorkspaceSnapshots = func(context.Context, workspacesync.CommitHash, workspacesync.SnapshotLimit) (workspacesync.SnapshotList, error) {
				return workspacesync.SnapshotList{NextCursor: "next"}, nil
			}
			changes, ok = routes.changesSinceCommit(context.Background(), workspacesync.SyncStatus{}, "target")
			Expect(ok).To(BeFalse())
			Expect(changes).To(BeEmpty())

			routes.contextStore.record(contextSessionKey(nil, toolActor{ID: "viewer"}), contextCheckpoint{Token: "old-token", CommitHash: "old"})
			routes.workspaceSyncStatus = func() workspacesync.SyncStatus {
				return workspacesync.SyncStatus{LastCommitHash: "new"}
			}
			out, err = routes.getContext(context.Background(), nil, toolActor{ID: "viewer", User: &coreauth.User{ID: "viewer", Username: "viewer", Role: coreauth.RoleViewer}}, httpinternal.RouterOptions{}, getContextInput{SinceToken: "old-token"})
			Expect(err).NotTo(HaveOccurred())
			Expect(out.Warnings).To(ContainElement("changesSincePreviousContext truncated before sinceToken checkpoint"))

			statusForFallback := workspacesync.SyncStatus{
				LastCommitHash:             "latest",
				RecentChangedMarkdownPaths: []string{"home.md"},
			}
			change := routes.recentChangeFromSnapshot(statusForFallback, workspacesync.Snapshot{ID: "latest"})
			Expect(change.ChangedPaths).To(Equal([]string{"home.md"}))
		})

		It("covers markdown path lookup and workspace path redaction variants", func() {
			routes := newContextToolTestRoutes(GinkgoT())
			home, err := routes.treeService.FindPageByRoutePathAndKind(newFixtureRoutePath("home"), tree.NodeKindPage)
			Expect(err).NotTo(HaveOccurred())

			Expect(routes.pageIDForMarkdownPath("home/README.md")).To(BeEmpty())
			Expect(routes.pageIDForMarkdownPath("README.md")).To(Equal(tree.RootPageID))
			Expect(routes.pageIDForMarkdownPath("bad/../README.md")).To(Equal(tree.RootPageID))
			Expect(routes.pageIDForMarkdownPath("bad/ /README.md")).To(BeEmpty())
			Expect(routes.pageIDForMarkdownPath("../bad.md")).To(BeEmpty())
			Expect(routes.pageIDsForMarkdownPaths([]string{"home.md", "home.md"})).To(Equal([]tree.PageID{home.ID}))
			Expect((&Routes{treeService: tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: GinkgoT().TempDir(), RootDir: GinkgoT().TempDir()})}).pageIDForRecentChangeRoute("", tree.NodeKindSection)).To(BeEmpty())
			Expect(routes.pageIDForRecentChangeRoute(newFixtureRoutePath("missing"), "")).To(BeEmpty())

			readmeRoutes := newContextToolTestRoutes(GinkgoT())
			Expect(readmeRoutes.pageIDForMarkdownPath("README.md")).To(Equal(tree.RootPageID))

			root := filepath.Join(GinkgoT().TempDir(), "workspace")
			data := filepath.Join(root, ".leafwiki")
			redacted := (&Routes{workspaceRootDir: root, workspaceDataDir: data}).redactWorkspacePaths(root + "/page.md and " + data + "/state.db")
			Expect(redacted).To(ContainSubstring("<root-dir>/page.md"))
			Expect(redacted).To(ContainSubstring("<data-dir>/state.db"))
			Expect(redactWorkspacePath("unchanged", ".", "<dot>")).To(Equal("unchanged"))
			Expect(redactWorkspacePath("unchanged", string(filepath.Separator), "<root>")).To(Equal("unchanged"))
			Expect(redactWorkspacePath("keep /", "\\", "<slash>")).To(Equal("keep /"))
		})
	})

	Describe("navigation and partial-edit helpers", func() {
		It("covers subtree edge branches and partial-edit error mapping", func() {
			unloadedTree := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: GinkgoT().TempDir(), RootDir: GinkgoT().TempDir()})
			_, err := (&Routes{treeService: unloadedTree}).getSubtree(context.Background(), getSubtreeInput{})
			Expect(err).To(MatchError("page not found"))

			routes := newContextToolTestRoutes(GinkgoT())
			Expect(routes.subtreeContentPreview(newFixturePageID("missing"))).To(BeEmpty())
			Expect(func() { ensureSubtreeNodeChildrenArray(&subtreeNode{}) }).NotTo(Panic())
			parent := &tree.PageNode{Children: []*tree.PageNode{{Children: []*tree.PageNode{{}}}}}
			Expect(subtreeTruncated(parent, treeDisplayDepth(1))).To(BeTrue())

			emptyVersionPage := &tree.Page{PageNode: &tree.PageNode{ID: "empty", Title: "Empty", Slug: "empty", Kind: tree.NodeKindPage}}
			Expect(partialEditVersionPreflight("", nil)).To(Succeed())
			Expect(partialEditVersionPreflight("", emptyVersionPage)).To(Succeed())

			home, err := routes.treeService.FindPageByRoutePathAndKind(newFixtureRoutePath("home"), tree.NodeKindPage)
			Expect(err).NotTo(HaveOccurred())
			Expect(partialEditVersionPreflight("", home)).To(HaveOccurred())
			Expect(partialEditVersionPreflight(tree.PageVersionFromString("stale"), home)).To(HaveOccurred())
			Expect(partialEditWriteError(errors.New("other"), nil)).To(MatchError("other"))
			Expect(partialEditWriteError(errors.New("other"), home)).To(MatchError("other"))

			missingPage := &tree.Page{PageNode: &tree.PageNode{ID: "missing", Title: "Missing", Slug: "missing", Kind: tree.NodeKindPage}}
			_, err = routes.partialEditOutput(context.Background(), missingPage, false, nil, false)
			Expect(err).To(HaveOccurred())

			var input updatePageInput
			Expect(input.UnmarshalJSON([]byte(`{`))).To(HaveOccurred())
		})
	})

	Describe("validation and refresh helpers", func() {
		It("covers validation link, path, and ID edge branches", func() {
			routes := newContextToolTestRoutes(GinkgoT())
			routes.workspaceRootDir = ""
			home, err := routes.treeService.FindPageByRoutePathAndKind(newFixtureRoutePath("home"), tree.NodeKindPage)
			Expect(err).NotTo(HaveOccurred())
			sectionID, err := routes.treeService.CreateNode("system", nil, "Guide", "guide", testNodeKindPtr(tree.NodeKindSection))
			Expect(err).NotTo(HaveOccurred())

			pageID, kind, ok, code := ((*Routes)(nil)).resolveValidationMarkdownLink("", tree.NodeKindPage, "missing.md")
			Expect(pageID).To(BeEmpty())
			Expect(kind).To(BeEmpty())
			Expect(ok).To(BeFalse())
			Expect(code).To(Equal(wikivalidation.IssueCodeBrokenLink))

			pageID, kind, ok, code = routes.resolveValidationMarkdownLink("", tree.NodeKindPage, "https://example.com")
			Expect(pageID).To(BeEmpty())
			Expect(kind).To(BeEmpty())
			Expect(ok).To(BeTrue())
			Expect(code).To(BeZero())

			_, _, ok, code = routes.resolveValidationMarkdownLink("", tree.NodeKindPage, "%zz")
			Expect(ok).To(BeFalse())
			Expect(code).To(Equal(wikivalidation.IssueCodeInvalidLink))

			_, _, ok, code = routes.resolveValidationMarkdownLink("", tree.NodeKindPage, "../escape.md")
			Expect(ok).To(BeFalse())
			Expect(code).To(Equal(wikivalidation.IssueCodeInvalidLink))

			pageID, kind, ok, code = routes.resolveValidationMarkdownLink("", tree.NodeKindPage, "/home.md")
			Expect(pageID).To(Equal(home.ID))
			Expect(kind).To(Equal(tree.NodeKindPage))
			Expect(ok).To(BeTrue())
			Expect(code).To(BeZero())

			Expect((&Routes{}).validationMarkdownLinkIndex()).NotTo(BeNil())
			Expect((&Routes{treeService: tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: GinkgoT().TempDir(), RootDir: GinkgoT().TempDir()})}).validationMarkdownLinkIndex()).NotTo(BeNil())
			Expect(validationContentLeafWikiID("not: [valid")).To(BeEmpty())
			Expect(validationContentLeafWikiID("---\n: bad\n---\n# Page\n")).To(BeEmpty())
			Expect(validationContentLeafWikiID("<!-- leafwiki extra\nversion: 1\npage:\n  id: page-123\n-->\nBody")).To(BeEmpty())
			Expect(validationContentLeafWikiID("---\nleafwiki_id: page-1\n---\n# Page\n")).To(Equal("page-1"))
			Expect((&Routes{}).validateLoadedTree(context.Background()).OK).To(BeTrue())

			_, _, err = routes.normalizeValidationContentPathInput("../bad", "")
			Expect(err).To(HaveOccurred())
			_, _, err = routes.normalizeValidationContentPathInput("bad//page.md", "")
			Expect(err).To(HaveOccurred())
			_, _, err = routes.normalizeValidationContentPathInput("bad//page.md", string(tree.NodeKindPage))
			Expect(err).To(HaveOccurred())
			_, _, err = routes.normalizeValidationContentPathInput("bad//route", string(tree.NodeKindPage))
			Expect(err).To(HaveOccurred())
			_, _, err = routes.normalizeValidationContentPathInput("home", "invalid-kind")
			Expect(err).To(HaveOccurred())
			_, _, err = routes.normalizeValidationContentPathInput("guide/README.md", "invalid-kind")
			Expect(err).To(HaveOccurred())
			_, _, err = routes.normalizeValidationContentPathInput("bad//README.md", "")
			Expect(err).To(HaveOccurred())
			routePath, sourceKind, err := routes.normalizeValidationContentPathInput("README.md", "")
			Expect(err).NotTo(HaveOccurred())
			Expect(routePath).To(Equal(newFixtureRoutePath("README")))
			Expect(sourceKind).To(Equal(tree.NodeKindPage))
			routePath, sourceKind, err = routes.normalizeValidationContentPathInput("", "")
			Expect(err).NotTo(HaveOccurred())
			Expect(routePath).To(BeEmpty())
			Expect(sourceKind).To(BeEmpty())
			fallbackTree := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: GinkgoT().TempDir(), RootDir: GinkgoT().TempDir()})
			Expect(fallbackTree.LoadTree()).To(Succeed())
			fallbackRoutes := &Routes{treeService: fallbackTree}
			guideDir := filepath.Join(fallbackTree.RootDir(), "guide")
			Expect(os.MkdirAll(guideDir, 0o755)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(guideDir, "README.md"), []byte("# Guide\n"), 0o644)).To(Succeed())

			routePath, sourceKind, err = fallbackRoutes.normalizeValidationContentPathInput("guide/README.md", "")
			Expect(err).NotTo(HaveOccurred())
			Expect(routePath).To(Equal(newFixtureRoutePath("guide")))
			Expect(sourceKind).To(Equal(tree.NodeKindSection))
			routePath, sourceKind, err = fallbackRoutes.normalizeValidationContentPathInput("guide/README.md", string(tree.NodeKindSection))
			Expect(err).NotTo(HaveOccurred())
			Expect(routePath).To(Equal(newFixtureRoutePath("guide")))
			Expect(sourceKind).To(Equal(tree.NodeKindSection))
			_, _, err = routes.normalizeValidationContentPathInput("guide/README.md", string(tree.NodeKindSection))
			Expect(err).To(HaveOccurred())
			routePath, sourceKind, err = routes.normalizeValidationContentPathInput("guide/README.md", string(tree.NodeKindPage))
			Expect(err).NotTo(HaveOccurred())
			Expect(routePath).To(Equal(newFixtureRoutePath("guide/README")))
			Expect(sourceKind).To(Equal(tree.NodeKindPage))
			routePath, sourceKind, err = routes.normalizeValidationContentPathInput("home.md", string(tree.NodeKindPage))
			Expect(err).NotTo(HaveOccurred())
			Expect(routePath).To(Equal(newFixtureRoutePath("home")))
			Expect(sourceKind).To(Equal(tree.NodeKindPage))
			_, _, err = routes.normalizeValidationContentPathInput("home.md", string(tree.NodeKindSection))
			Expect(err).To(HaveOccurred())
			_, _, err = routes.normalizeValidationContentPathInput("../bad", string(tree.NodeKindPage))
			Expect(err).To(HaveOccurred())
			_, _, err = routes.normalizeValidationContentPathInput("bad//README.md", string(tree.NodeKindPage))
			Expect(err).To(HaveOccurred())
			_, _, err = routes.normalizeValidationContentPathInput("bad/../README.md", string(tree.NodeKindSection))
			Expect(err).To(HaveOccurred())
			_, _, err = routes.normalizeValidationContentPathInput("bad/../README.md", "")
			Expect(err).To(HaveOccurred())
			_, err = routes.treeService.CreateNode("system", sectionID, "Guide Readme", "README", nil)
			Expect(err).NotTo(HaveOccurred())
			routePath, sourceKind, err = routes.normalizeValidationContentPathInput("guide/README.md", "")
			Expect(err).NotTo(HaveOccurred())
			Expect(routePath).To(Equal(newFixtureRoutePath("guide/README")))
			Expect(sourceKind).To(Equal(tree.NodeKindPage))

			_, ok = routes.resolveValidationPageIDForKind("", tree.NodeKindPage)
			Expect(ok).To(BeFalse())
			_, ok = ((*Routes)(nil)).resolveValidationPageIDForKind(newFixtureRoutePath("home"), tree.NodeKindPage)
			Expect(ok).To(BeFalse())
			pageID, ok = (&Routes{}).resolveValidationPageID(newFixtureRoutePath("home"))
			Expect(ok).To(BeFalse())
			Expect(pageID).To(BeEmpty())
			pageID, ok = routes.resolveValidationPageID(newFixtureRoutePath("missing"))
			Expect(ok).To(BeFalse())
			Expect(pageID).To(BeEmpty())
			Expect(routes.validationSourceKind(newFixturePageID("missing"))).To(Equal(tree.NodeKindPage))
			Expect(routes.validationPageIDExists(*sectionID)).To(BeTrue())
			Expect((&Routes{}).validationPageIDExists(home.ID)).To(BeFalse())
			Expect(routes.validationPageIDExists(home.ID)).To(BeTrue())
		})

		It("covers validation asset lookup normalization through the real list use case", func() {
			routes := newContextToolTestRoutes(GinkgoT())
			home, err := routes.treeService.FindPageByRoutePathAndKind(newFixtureRoutePath("home"), tree.NodeKindPage)
			Expect(err).NotTo(HaveOccurred())
			assetService := coreassets.NewAssetService(GinkgoT().TempDir(), tree.NewSlugService())
			_, err = assetService.SaveAssetForPage(home.PageNode, &memoryMultipartFile{Reader: bytes.NewReader([]byte("logo"))}, "logo.png", 1024)
			Expect(err).NotTo(HaveOccurred())
			routes.getAssets = wikiassets.NewListAssetsUseCase(routes.treeService, assetService)

			exists := routes.validationAssetExists(context.Background(), home.ID)

			Expect(exists("logo.png")).To(BeTrue())
			Expect(exists("/assets/" + home.ID.MetadataValue() + "/logo.png")).To(BeTrue())
			Expect(exists("<logo.png?size=1#preview>")).To(BeTrue())
			Expect(exists("nested/logo.png")).To(BeFalse())

			predicate := validationAssetPredicate(home.ID, []string{"", "assets/" + home.ID.MetadataValue() + "/icon.png"})
			Expect(predicate("icon.png")).To(BeTrue())
			Expect(predicate("missing.png")).To(BeFalse())
			Expect(validationAssetPredicate(home.ID, []string{"logo.png"})("/logo.png")).To(BeTrue())
		})

		It("covers workspace refresh disabled, source, error, and validation branches", func() {
			_, err := (&Routes{}).refreshWorkspaceSync(context.Background(), toolActor{}, refreshInput{})
			Expect(err).To(MatchError("workspace sync is not enabled"))

			_, err = refreshSource("invalid")
			Expect(err).To(MatchError("source must be mcp or filesystem"))

			source, err := refreshSource(" filesystem ")
			Expect(err).NotTo(HaveOccurred())
			Expect(source).To(Equal(workspacesync.SourceFilesystem))

			validate := true
			refreshErr := errors.New("sync failed")
			routes := &Routes{
				workspaceSyncRefresh: func(context.Context, workspacesync.SyncRequest) (workspacesync.SyncStatus, error) {
					return workspacesync.SyncStatus{RecentChangedMarkdownPaths: []string{"home.md"}}, refreshErr
				},
			}
			_, err = routes.refreshWorkspaceSync(context.Background(), toolActor{ID: "editor", User: &coreauth.User{ID: "editor", Username: "editor"}}, refreshInput{Validate: &validate})
			Expect(err).To(MatchError(refreshErr))

			routes.workspaceSyncRefresh = func(_ context.Context, req workspacesync.SyncRequest) (workspacesync.SyncStatus, error) {
				Expect(req.Source).To(Equal(workspacesync.SourceMCP))
				Expect(req.Actor.ID.String()).To(Equal("editor"))
				return workspacesync.SyncStatus{
					Enabled:                    true,
					LastCommitHash:             "commit-1",
					RecentChangedMarkdownPaths: []string{"home.md"},
					ValidationErrors: []workspacesync.ValidationError{
						{Path: "/workspace/home.md", Message: "broken", Severity: wikivalidation.IssueSeverityError},
					},
				}, nil
			}

			out, err := routes.refreshWorkspaceSync(context.Background(), toolActor{ID: "editor", User: &coreauth.User{ID: "editor", Username: "editor"}}, refreshInput{Validate: &validate})

			Expect(err).NotTo(HaveOccurred())
			Expect(out.LastCommitHash).To(Equal("commit-1"))
			Expect(out.RecentChangedPaths).To(Equal([]string{"home.md"}))
			Expect(out.Validation).NotTo(BeNil())
			Expect(out.Validation.OK).To(BeFalse())
		})
	})
})

func mcpTokenInfoRequest(userID string) *sdkmcp.CallToolRequest {
	GinkgoHelper()
	return &sdkmcp.CallToolRequest{Extra: &sdkmcp.RequestExtra{TokenInfo: &sdkauth.TokenInfo{UserID: userID}}}
}
