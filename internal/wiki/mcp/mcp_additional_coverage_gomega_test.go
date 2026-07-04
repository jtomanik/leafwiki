package mcp

import (
	"bytes"
	"context"
	"encoding/json"
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
	"github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"
	coreassets "github.com/perber/wiki/internal/core/assets"
	coreauth "github.com/perber/wiki/internal/core/auth"
	wikivalidation "github.com/perber/wiki/internal/core/markdownvalidation"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/core/tree"
	httpinternal "github.com/perber/wiki/internal/http"
	"github.com/perber/wiki/internal/projectdaemon"
	wikiassets "github.com/perber/wiki/internal/wiki/assets"
	wikipages "github.com/perber/wiki/internal/wiki/pages"
	wikipresence "github.com/perber/wiki/internal/wiki/presence"
	"github.com/perber/wiki/internal/workspacesync"
)

var _ = Describe("MCP context checkpoints and route normalization", func() {
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
			userDir := mcpTestTempDir()
			store, err := coreauth.NewUserStore(userDir)
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() {
				Expect(store.Close()).To(Succeed())
			})
			userService := coreauth.NewUserService(store)
			editor, err := userService.CreateUser("editor", "editor@example.com", "password", coreauth.RoleEditor)
			Expect(err).NotTo(HaveOccurred())
			routes := &Routes{userService: userService}

			user, err := routes.actorForRequest(mcpTokenInfoRequest(editor.ID))
			Expect(err).NotTo(HaveOccurred())
			Expect(user).To(matchMCPUser(gstruct.Fields{
				"ID":   Equal(editor.ID),
				"Role": Equal(coreauth.RoleEditor),
			}))

			user, err = routes.actorForRequest(mcpTokenInfoRequest("missing-user"))
			Expect(user).To(BeNil())
			Expect(err).To(matchLocalizedErrorCode(errCodeMCPAuthenticatedUserNotFound, sharederrors.MessageIDForCode(errCodeMCPAuthenticatedUserNotFound)))

			blocker := beginExclusiveMCPTestSQLiteTransaction(filepath.Join(userDir, "users.db"))
			DeferCleanup(blocker.rollback)

			user, err = routes.actorForRequest(mcpTokenInfoRequest(editor.ID))
			Expect(user).To(BeNil())
			Expect(err).To(matchLocalizedErrorCode(errCodeMCPAuthenticatedUserLookupFailed, sharederrors.MessageIDForCode(errCodeMCPAuthenticatedUserLookupFailed)))
		})

		It("requires private actor context headers when configured and propagates editor auth failures", func() {
			routes := &Routes{actorContextAllowed: true, actorContextRequired: true}

			Expect(privateActorContextFor(routes, http.Header{})).To(matchPrivateActorContext(
				privateActorContextHandled,
				BeNil(),
				matchLocalizedErrorCode(errCodeMCPActorContextMissing, sharederrors.MessageIDForCode(errCodeMCPActorContextMissing)),
			))

			user, err := (&Routes{}).editorActorForRequest(nil)

			Expect(user).To(BeNil())
			Expect(err).To(matchLocalizedErrorCode(errCodeMCPTokenInfoMissing, sharederrors.MessageIDForCode(errCodeMCPTokenInfoMissing)))
		})

		It("accepts private API-key requests and rejects verifier storage failures", func() {
			_, apiKeyService, _, created, _ := newMCPAPIKeyAuthFixture()
			handler := (&Routes{apiKeys: apiKeyService}).requirePrivateStdioAPIKey(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			}))
			req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
			req.Header.Set("Authorization", "Bearer "+created.Secret)
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			Expect(rec).To(HaveHTTPStatus(http.StatusNoContent))

			_, err := (&Routes{}).verifyBearerToken(context.Background(), created.Secret, nil)
			Expect(err).To(MatchError(sdkauth.ErrInvalidToken))

			actorHandler := (&Routes{}).NewActorContextHTTPHandler(httpinternal.RouterOptions{})
			actorHandler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/mcp", nil))

			server := httptest.NewServer(actorHandler)
			DeferCleanup(server.Close)
			client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "leafwiki-test", Version: "test"}, nil)
			session, err := client.Connect(context.Background(), &sdkmcp.StreamableClientTransport{
				Endpoint:             server.URL + "/mcp",
				HTTPClient:           server.Client(),
				DisableStandaloneSSE: true,
			}, nil)
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { _ = session.Close() })

			_, storageFailureService, _, storageFailureKey, apiKeyDBPath := newMCPAPIKeyAuthFixture()
			blocker := beginExclusiveMCPTestSQLiteTransaction(apiKeyDBPath)
			DeferCleanup(blocker.rollback)
			failingHandler := (&Routes{apiKeys: storageFailureService}).requirePrivateStdioAPIKey(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			}))
			req = httptest.NewRequest(http.MethodPost, "/mcp", nil)
			req.Header.Set("Authorization", "Bearer "+storageFailureKey.Secret)
			rec = httptest.NewRecorder()

			failingHandler.ServeHTTP(rec, req)

			Expect(rec).To(HaveHTTPStatus(http.StatusServiceUnavailable))
		})
	})

	Describe("schema and context helpers", func() {
		It("falls back to object schema and no optional tools for unknown gates", func() {
			Expect(toolOutputSchema(ToolID("unknown_tool")).Type).To(Equal("object"))
			Expect(toolNamesForGate(optionalToolGate("unknown"))).To(BeNil())
		})

		It("reports presence, refresh, and snapshot delta behavior", func() {
			now := time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
			routes := newContextToolTestRoutes()
			routes.webPresenceProvider = func(*coreauth.User) ([]wikipresence.Session, error) {
				return []wikipresence.Session{{Type: wikipresence.SessionTypeWeb, SessionID: wikipresence.WebSessionIDFromString("web-session")}}, nil
			}
			routes.agentPresenceProvider = func() ([]projectdaemon.AgentPresenceSession, error) {
				return []projectdaemon.AgentPresenceSession{{SessionIDHash: "agent-session", Provider: "codex", FirstSeenAt: now, LastSeenAt: now}}, nil
			}

			sessions, status := routes.activeSessionsForContext(&coreauth.User{ID: "editor", Role: coreauth.RoleEditor})

			Expect(status).To(matchPresenceStatusOutput(gstruct.Fields{
				"Web":        Equal("enabled"),
				"AgentHooks": Equal("enabled"),
			}))
			Expect(sessions).To(HaveExactElements(
				matchPresenceSession(gstruct.Fields{"Type": Equal(wikipresence.SessionTypeAgent)}),
				matchPresenceSession(gstruct.Fields{"Type": Equal(wikipresence.SessionTypeWeb)}),
			))

			huge := 99
			Expect(shouldRefreshForContext(contextSyncModeForce, workspacesync.SyncStatus{})).To(BeTrue())
			Expect(shouldRefreshForContext(contextSyncModeNone, workspacesync.SyncStatus{Enabled: true, PendingEventCount: 1})).To(BeFalse())
			Expect(contextSnapshotPageSize(25)).To(Equal(25))
			Expect(contextSnapshotPageSize(75)).To(Equal(50))
			Expect(boundedContextTreeDepth(&huge)).To(Equal(treeDisplayDepth(maxContextTreeDepth)))
			one := 1
			Expect(boundedContextTreeDepth(&one)).To(Equal(treeDisplayDepth(1)))
			Expect((&Routes{treeService: tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: mcpTestTempDir(), RootDir: mcpTestTempDir()})}).contextTree(1)).To(BeNil())
			Expect(func() { ensureNodeChildrenArray(nil) }).NotTo(Panic())

			_, err := routes.getContext(context.Background(), nil, toolActor{ID: "viewer", User: &coreauth.User{ID: "viewer", Username: "viewer", Role: coreauth.RoleViewer}}, httpinternal.RouterOptions{}, getContextInput{SyncMode: "invalid"})
			Expect(err).To(MatchError(errContextSyncModeInvalid))

			routes.workspaceSyncRefresh = func(context.Context, workspacesync.SyncRequest) (workspacesync.SyncStatus, error) {
				return workspacesync.SyncStatus{}, errors.New("viewer refresh should be skipped")
			}
			routes.workspaceSyncStatus = func() workspacesync.SyncStatus {
				return workspacesync.SyncStatus{Enabled: true, PendingEventCount: 1}
			}
			out, err := routes.getContext(context.Background(), nil, toolActor{ID: "viewer", User: &coreauth.User{ID: "viewer", Username: "viewer", Role: coreauth.RoleViewer}}, httpinternal.RouterOptions{}, getContextInput{})
			Expect(err).NotTo(HaveOccurred())
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

			Expect(changesSinceCommitOutcomeFor(routes, workspacesync.SyncStatus{}, "")).To(matchChangesSinceCommit(
				changesReachedRequestedCommit,
				HaveLen(2),
			))

			routes.listWorkspaceSnapshots = nil
			Expect(changesSinceCommitOutcomeFor(routes, workspacesync.SyncStatus{}, "missing")).To(matchChangesSinceCommit(
				changesStoppedBeforeRequestedCommit,
				BeNil(),
			))

			routes.listWorkspaceSnapshots = func(context.Context, workspacesync.CommitHash, workspacesync.SnapshotLimit) (workspacesync.SnapshotList, error) {
				return workspacesync.SnapshotList{
					Snapshots: []workspacesync.Snapshot{{ID: "newer"}},
				}, errors.New("snapshot backend failed")
			}
			Expect(changesSinceCommitOutcomeFor(routes, workspacesync.SyncStatus{}, "target")).To(matchChangesSinceCommit(
				changesStoppedBeforeRequestedCommit,
				HaveLen(0),
			))

			routes.listWorkspaceSnapshots = func(_ context.Context, _ workspacesync.CommitHash, limit workspacesync.SnapshotLimit) (workspacesync.SnapshotList, error) {
				snapshots := make([]workspacesync.Snapshot, int(limit))
				for i := range snapshots {
					snapshots[i] = workspacesync.Snapshot{ID: workspacesync.CommitHashFromString(fmt.Sprintf("snapshot-%d", i))}
				}
				return workspacesync.SnapshotList{Snapshots: snapshots, NextCursor: "next"}, nil
			}
			Expect(changesSinceCommitOutcomeFor(routes, workspacesync.SyncStatus{}, "target")).To(matchChangesSinceCommit(
				changesStoppedBeforeRequestedCommit,
				HaveLen(maxContextDeltaSnapshots),
			))

			routes.listWorkspaceSnapshots = func(context.Context, workspacesync.CommitHash, workspacesync.SnapshotLimit) (workspacesync.SnapshotList, error) {
				return workspacesync.SnapshotList{NextCursor: "next"}, nil
			}
			Expect(changesSinceCommitOutcomeFor(routes, workspacesync.SyncStatus{}, "target")).To(matchChangesSinceCommit(
				changesStoppedBeforeRequestedCommit,
				BeEmpty(),
			))

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

		It("normalizes markdown paths and redacts workspace paths", func() {
			routes := newContextToolTestRoutes()
			home, err := routes.treeService.FindPageByRoutePathAndKind(newFixtureRoutePath("home"), tree.NodeKindPage)
			Expect(err).NotTo(HaveOccurred())

			Expect(routes.pageIDForMarkdownPath("home/README.md")).To(BeEmpty())
			Expect(routes.pageIDForMarkdownPath("README.md")).To(Equal(tree.RootPageID))
			Expect(routes.pageIDForMarkdownPath("bad/../README.md")).To(Equal(tree.RootPageID))
			Expect(routes.pageIDForMarkdownPath("bad/ /README.md")).To(BeEmpty())
			Expect(routes.pageIDForMarkdownPath("../bad.md")).To(BeEmpty())
			Expect(routes.pageIDsForMarkdownPaths([]string{"home.md", "home.md"})).To(Equal([]tree.PageID{home.ID}))
			Expect((&Routes{treeService: tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: mcpTestTempDir(), RootDir: mcpTestTempDir()})}).pageIDForRecentChangeRoute("", tree.NodeKindSection)).To(BeEmpty())
			Expect(routes.pageIDForRecentChangeRoute(newFixtureRoutePath("missing"), "")).To(BeEmpty())

			readmeRoutes := newContextToolTestRoutes()
			Expect(readmeRoutes.pageIDForMarkdownPath("README.md")).To(Equal(tree.RootPageID))

			root := filepath.Join(mcpTestTempDir(), "workspace")
			data := filepath.Join(root, ".leafwiki")
			redacted := (&Routes{workspaceRootDir: root, workspaceDataDir: data}).redactWorkspacePaths(root + "/page.md and " + data + "/state.db")
			Expect(redacted).To(matchRedactedWorkspacePaths(
				root,
				data,
				"<root-dir>/page.md",
				"<data-dir>/state.db",
			))
			Expect(redactWorkspacePath("unchanged", ".", "<dot>")).To(Equal("unchanged"))
			Expect(redactWorkspacePath("unchanged", string(filepath.Separator), "<root>")).To(Equal("unchanged"))
			Expect(redactWorkspacePath("keep /", "\\", "<slash>")).To(Equal("keep /"))
		})
	})

	Describe("navigation and partial-edit helpers", func() {
		It("maps subtree and partial-edit failures to semantic errors", func() {
			unloadedTree := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: mcpTestTempDir(), RootDir: mcpTestTempDir()})
			_, err := (&Routes{treeService: unloadedTree}).getSubtree(context.Background(), getSubtreeInput{})
			Expect(err).To(MatchError(tree.ErrPageNotFound))

			routes := newContextToolTestRoutes()
			Expect(routes.subtreeContentPreview(newFixturePageID("missing"))).To(BeEmpty())
			Expect(func() { ensureSubtreeNodeChildrenArray(&subtreeNode{}) }).NotTo(Panic())
			parent := &tree.PageNode{Children: []*tree.PageNode{{Children: []*tree.PageNode{{}}}}}
			Expect(subtreeTruncated(parent, treeDisplayDepth(1))).To(BeTrue())

			emptyVersionPage := &tree.Page{PageNode: &tree.PageNode{ID: "empty", Title: "Empty", Slug: "empty", Kind: tree.NodeKindPage}}
			Expect(partialEditVersionPreflight("", nil)).To(Succeed())
			Expect(partialEditVersionPreflight("", emptyVersionPage)).To(Succeed())

			home, err := routes.treeService.FindPageByRoutePathAndKind(newFixtureRoutePath("home"), tree.NodeKindPage)
			Expect(err).NotTo(HaveOccurred())
			Expect(partialEditVersionPreflight("", home)).To(matchMCPToolLocalizedError(wikipages.ErrCodePageVersionRequired))
			Expect(partialEditVersionPreflight(tree.PageVersionFromString("stale"), home)).To(matchMCPToolLocalizedError(wikipages.ErrCodePageVersionConflict))
			otherErr := errors.New("other")
			Expect(partialEditWriteError(otherErr, nil)).To(MatchError(otherErr))
			Expect(partialEditWriteError(otherErr, home)).To(MatchError(otherErr))

			missingPage := &tree.Page{PageNode: &tree.PageNode{ID: "missing", Title: "Missing", Slug: "missing", Kind: tree.NodeKindPage}}
			_, err = routes.partialEditOutput(context.Background(), missingPage, false, nil, false)
			Expect(err).To(MatchError(tree.ErrPageNotFound))

			var input updatePageInput
			Expect(input.UnmarshalJSON([]byte(`{`))).To(matchJSONSyntaxError())
		})
	})

	Describe("validation and refresh helpers", func() {
		It("resolves validation links, paths, and page IDs semantically", func() {
			routes := newContextToolTestRoutes()
			routes.workspaceRootDir = ""
			home, err := routes.treeService.FindPageByRoutePathAndKind(newFixtureRoutePath("home"), tree.NodeKindPage)
			Expect(err).NotTo(HaveOccurred())
			sectionID, err := routes.treeService.CreateNode("system", nil, "Guide", "guide", testNodeKindPtr(tree.NodeKindSection))
			Expect(err).NotTo(HaveOccurred())

			Expect(validationMarkdownLinkResolutionFor((*Routes)(nil), "", tree.NodeKindPage, "missing.md")).To(matchValidationMarkdownLink(
				validationUnresolved,
				BeEmpty(),
				BeEmpty(),
				Equal(wikivalidation.IssueCodeBrokenLink),
			))

			Expect(validationMarkdownLinkResolutionFor(routes, "", tree.NodeKindPage, "https://example.com")).To(matchValidationMarkdownLink(
				validationResolved,
				BeEmpty(),
				BeEmpty(),
				BeEmpty(),
			))

			Expect(validationMarkdownLinkResolutionFor(routes, "", tree.NodeKindPage, "%zz")).To(matchValidationMarkdownLink(
				validationUnresolved,
				gstruct.Ignore(),
				gstruct.Ignore(),
				Equal(wikivalidation.IssueCodeInvalidLink),
			))

			Expect(validationMarkdownLinkResolutionFor(routes, "", tree.NodeKindPage, "../escape.md")).To(matchValidationMarkdownLink(
				validationUnresolved,
				gstruct.Ignore(),
				gstruct.Ignore(),
				Equal(wikivalidation.IssueCodeInvalidLink),
			))

			Expect(validationMarkdownLinkResolutionFor(routes, "", tree.NodeKindPage, "/home.md")).To(matchValidationMarkdownLink(
				validationResolved,
				Equal(home.ID),
				Equal(tree.NodeKindPage),
				BeEmpty(),
			))

			Expect((&Routes{}).validationMarkdownLinkIndex()).NotTo(BeNil())
			Expect((&Routes{treeService: tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: mcpTestTempDir(), RootDir: mcpTestTempDir()})}).validationMarkdownLinkIndex()).NotTo(BeNil())
			Expect(validationContentLeafWikiID("not: [valid")).To(BeEmpty())
			Expect(validationContentLeafWikiID("---\n: bad\n---\n# Page\n")).To(BeEmpty())
			Expect(validationContentLeafWikiID("<!-- leafwiki extra\nversion: 1\npage:\n  id: page-123\n-->\nBody")).To(BeEmpty())
			Expect(validationContentLeafWikiID("---\nleafwiki_id: page-1\n---\n# Page\n")).To(Equal("page-1"))
			Expect((&Routes{}).validateLoadedTree(context.Background())).To(matchSuccessfulMarkdownValidation())

			_, _, err = routes.normalizeValidationContentPathInput("../bad", "")
			Expect(err).To(MatchError(tree.ErrInvalidRoutePath))
			_, _, err = routes.normalizeValidationContentPathInput("bad//page.md", "")
			Expect(err).To(MatchError(tree.ErrInvalidRoutePath))
			_, _, err = routes.normalizeValidationContentPathInput("bad//page.md", tree.NodeKindPage)
			Expect(err).To(MatchError(tree.ErrInvalidRoutePath))
			_, _, err = routes.normalizeValidationContentPathInput("bad//route", tree.NodeKindPage)
			Expect(err).To(MatchError(tree.ErrInvalidRoutePath))
			_, _, err = routes.normalizeValidationContentPathToolInput("home", "invalid-kind")
			Expect(err).To(matchMCPToolLocalizedError(wikipages.ErrCodePageInvalidKind))
			_, _, err = routes.normalizeValidationContentPathToolInput("guide/README.md", "invalid-kind")
			Expect(err).To(matchMCPToolLocalizedError(wikipages.ErrCodePageInvalidKind))
			_, _, err = routes.normalizeValidationContentPathInput("bad//README.md", "")
			Expect(err).To(MatchError(tree.ErrInvalidRoutePath))
			routePath, sourceKind, err := routes.normalizeValidationContentPathInput("README.md", "")
			Expect(err).NotTo(HaveOccurred())
			Expect(routePath).To(Equal(newFixtureRoutePath("README")))
			Expect(sourceKind).To(Equal(tree.NodeKindPage))
			routePath, sourceKind, err = routes.normalizeValidationContentPathInput("", "")
			Expect(err).NotTo(HaveOccurred())
			Expect(routePath).To(BeEmpty())
			Expect(sourceKind).To(BeEmpty())
			fallbackTree := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: mcpTestTempDir(), RootDir: mcpTestTempDir()})
			Expect(fallbackTree.LoadTree()).To(Succeed())
			fallbackRoutes := &Routes{treeService: fallbackTree}
			guideDir := filepath.Join(fallbackTree.RootDir(), "guide")
			Expect(os.MkdirAll(guideDir, 0o755)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(guideDir, "README.md"), []byte("# Guide\n"), 0o644)).To(Succeed())

			routePath, sourceKind, err = fallbackRoutes.normalizeValidationContentPathInput("guide/README.md", "")
			Expect(err).NotTo(HaveOccurred())
			Expect(routePath).To(Equal(newFixtureRoutePath("guide")))
			Expect(sourceKind).To(Equal(tree.NodeKindSection))
			routePath, sourceKind, err = fallbackRoutes.normalizeValidationContentPathInput("guide/README.md", tree.NodeKindSection)
			Expect(err).NotTo(HaveOccurred())
			Expect(routePath).To(Equal(newFixtureRoutePath("guide")))
			Expect(sourceKind).To(Equal(tree.NodeKindSection))
			_, _, err = routes.normalizeValidationContentPathInput("guide/README.md", tree.NodeKindSection)
			Expect(err).To(MatchError(errValidationContentKindMismatch))
			routePath, sourceKind, err = routes.normalizeValidationContentPathInput("guide/README.md", tree.NodeKindPage)
			Expect(err).NotTo(HaveOccurred())
			Expect(routePath).To(Equal(newFixtureRoutePath("guide/README")))
			Expect(sourceKind).To(Equal(tree.NodeKindPage))
			routePath, sourceKind, err = routes.normalizeValidationContentPathInput("home.md", tree.NodeKindPage)
			Expect(err).NotTo(HaveOccurred())
			Expect(routePath).To(Equal(newFixtureRoutePath("home")))
			Expect(sourceKind).To(Equal(tree.NodeKindPage))
			_, _, err = routes.normalizeValidationContentPathInput("home.md", tree.NodeKindSection)
			Expect(err).To(MatchError(errValidationContentKindMismatch))
			_, _, err = routes.normalizeValidationContentPathInput("../bad", tree.NodeKindPage)
			Expect(err).To(MatchError(tree.ErrInvalidRoutePath))
			_, _, err = routes.normalizeValidationContentPathInput("bad//README.md", tree.NodeKindPage)
			Expect(err).To(MatchError(tree.ErrInvalidRoutePath))
			_, _, err = routes.normalizeValidationContentPathInput("bad/../README.md", tree.NodeKindSection)
			Expect(err).To(MatchError(errValidationContentKindMismatch))
			_, _, err = routes.normalizeValidationContentPathInput("bad/../README.md", "")
			Expect(err).To(MatchError(tree.ErrInvalidRoutePath))
			_, err = routes.treeService.CreateNode("system", sectionID, "Guide Readme", "README", nil)
			Expect(err).NotTo(HaveOccurred())
			routePath, sourceKind, err = routes.normalizeValidationContentPathInput("guide/README.md", "")
			Expect(err).NotTo(HaveOccurred())
			Expect(routePath).To(Equal(newFixtureRoutePath("guide/README")))
			Expect(sourceKind).To(Equal(tree.NodeKindPage))

			Expect(validationPageIDKindResolutionFor(routes, "", tree.NodeKindPage)).To(matchValidationPageID(validationUnresolved, BeEmpty()))
			Expect(validationPageIDKindResolutionFor((*Routes)(nil), newFixtureRoutePath("home"), tree.NodeKindPage)).To(matchValidationPageID(validationUnresolved, BeEmpty()))
			Expect(validationPageIDResolutionFor(&Routes{}, newFixtureRoutePath("home"))).To(matchValidationPageID(validationUnresolved, BeEmpty()))
			Expect(validationPageIDResolutionFor(routes, newFixtureRoutePath("missing"))).To(matchValidationPageID(validationUnresolved, BeEmpty()))
			Expect(routes.validationSourceKind(newFixturePageID("missing"))).To(Equal(tree.NodeKindPage))
			Expect(routes.validationPageIDExists(*sectionID)).To(BeTrue())
			Expect((&Routes{}).validationPageIDExists(home.ID)).To(BeFalse())
			Expect(routes.validationPageIDExists(home.ID)).To(BeTrue())
		})

		It("normalizes validation asset destinations through the real list use case", func() {
			routes := newContextToolTestRoutes()
			home, err := routes.treeService.FindPageByRoutePathAndKind(newFixtureRoutePath("home"), tree.NodeKindPage)
			Expect(err).NotTo(HaveOccurred())
			assetService := coreassets.NewAssetService(mcpTestTempDir(), tree.NewSlugService())
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

		It("reports workspace refresh disabled, source, backend, and validation outcomes", func() {
			_, err := (&Routes{}).refreshWorkspaceSync(context.Background(), toolActor{}, refreshInput{})
			Expect(err).To(MatchError(errWorkspaceSyncDisabled))

			_, err = refreshSource("invalid")
			Expect(err).To(MatchError(errWorkspaceSyncSourceInvalid))

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
				Expect(req.Actor.ID).To(Equal(workspacesync.ActorIDFromUserID(coreauth.UserIDFromString("editor"))))
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
			Expect(out).To(matchRefreshOutput(gstruct.Fields{
				"LastCommitHash":     Equal("commit-1"),
				"RecentChangedPaths": Equal([]string{"home.md"}),
				"Validation":         gstruct.PointTo(matchValidationOutputWithErrorCount(1)),
			}))
		})
	})
})

func matchMCPUser(fields gstruct.Fields) types.GomegaMatcher {
	GinkgoHelper()
	return gstruct.PointTo(gstruct.MatchFields(gstruct.IgnoreExtras, fields))
}

func matchPresenceStatusOutput(fields gstruct.Fields) types.GomegaMatcher {
	GinkgoHelper()
	return gstruct.MatchFields(gstruct.IgnoreExtras, fields)
}

func matchPresenceSession(fields gstruct.Fields) types.GomegaMatcher {
	GinkgoHelper()
	return gstruct.MatchFields(gstruct.IgnoreExtras, fields)
}

func matchRefreshOutput(fields gstruct.Fields) types.GomegaMatcher {
	GinkgoHelper()
	return gstruct.MatchFields(gstruct.IgnoreExtras, fields)
}

type privateActorContextState string

const (
	privateActorContextHandled  privateActorContextState = "handled"
	privateActorContextDeferred privateActorContextState = "deferred"
)

type privateActorContextOutcome struct {
	User  *coreauth.User
	Err   error
	State privateActorContextState
}

func privateActorContextFor(routes *Routes, header http.Header) privateActorContextOutcome {
	GinkgoHelper()
	user, handled, err := routes.actorFromPrivateContextHeader(header)
	state := privateActorContextDeferred
	if handled {
		state = privateActorContextHandled
	}
	return privateActorContextOutcome{User: user, Err: err, State: state}
}

func matchPrivateActorContext(state privateActorContextState, userMatcher types.GomegaMatcher, errMatcher types.GomegaMatcher) types.GomegaMatcher {
	GinkgoHelper()
	return gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"State": Equal(state),
		"User":  userMatcher,
		"Err":   errMatcher,
	})
}

type changesSinceCommitState string

const (
	changesReachedRequestedCommit       changesSinceCommitState = "reached requested commit"
	changesStoppedBeforeRequestedCommit changesSinceCommitState = "stopped before requested commit"
)

type changesSinceCommitOutcome struct {
	Changes []recentChangeOutput
	State   changesSinceCommitState
}

func changesSinceCommitOutcomeFor(routes *Routes, status workspacesync.SyncStatus, commitHash workspacesync.CommitHash) changesSinceCommitOutcome {
	GinkgoHelper()
	changes, complete := routes.changesSinceCommit(context.Background(), status, commitHash)
	state := changesStoppedBeforeRequestedCommit
	if complete {
		state = changesReachedRequestedCommit
	}
	return changesSinceCommitOutcome{Changes: changes, State: state}
}

func matchChangesSinceCommit(state changesSinceCommitState, changesMatcher types.GomegaMatcher) types.GomegaMatcher {
	GinkgoHelper()
	return gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"State":   Equal(state),
		"Changes": changesMatcher,
	})
}

func matchRedactedWorkspacePaths(rootDir string, dataDir string, placeholders ...string) types.GomegaMatcher {
	GinkgoHelper()
	matchers := []types.GomegaMatcher{
		Not(ContainSubstring(rootDir)),
		Not(ContainSubstring(dataDir)),
	}
	for _, placeholder := range placeholders {
		matchers = append(matchers, ContainSubstring(placeholder))
	}
	return SatisfyAll(matchers...)
}

func matchJSONSyntaxError() types.GomegaMatcher {
	GinkgoHelper()
	return Satisfy(func(err error) bool {
		var syntaxErr *json.SyntaxError
		return errors.As(err, &syntaxErr)
	})
}

func matchJSONTypeError() types.GomegaMatcher {
	GinkgoHelper()
	return Satisfy(func(err error) bool {
		var typeErr *json.UnmarshalTypeError
		return errors.As(err, &typeErr)
	})
}

type validationResolutionState string

const (
	validationResolved   validationResolutionState = "resolved"
	validationUnresolved validationResolutionState = "unresolved"
)

type validationMarkdownLinkResolution struct {
	PageID tree.PageID
	Kind   tree.NodeKind
	Code   wikivalidation.IssueCode
	State  validationResolutionState
}

func validationMarkdownLinkResolutionFor(routes *Routes, sourceRoutePath tree.RoutePath, sourceKind tree.NodeKind, destination string) validationMarkdownLinkResolution {
	GinkgoHelper()
	pageID, kind, resolved, code := routes.resolveValidationMarkdownLink(sourceRoutePath, sourceKind, destination)
	state := validationUnresolved
	if resolved {
		state = validationResolved
	}
	return validationMarkdownLinkResolution{
		PageID: pageID,
		Kind:   kind,
		Code:   code,
		State:  state,
	}
}

func matchValidationMarkdownLink(state validationResolutionState, pageIDMatcher types.GomegaMatcher, kindMatcher types.GomegaMatcher, codeMatcher types.GomegaMatcher) types.GomegaMatcher {
	GinkgoHelper()
	return gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"State":  Equal(state),
		"PageID": pageIDMatcher,
		"Kind":   kindMatcher,
		"Code":   codeMatcher,
	})
}

type validationPageIDResolution struct {
	PageID tree.PageID
	State  validationResolutionState
}

func validationPageIDResolutionFor(routes *Routes, routePath tree.RoutePath) validationPageIDResolution {
	GinkgoHelper()
	pageID, resolved := routes.resolveValidationPageID(routePath)
	state := validationUnresolved
	if resolved {
		state = validationResolved
	}
	return validationPageIDResolution{PageID: pageID, State: state}
}

func validationPageIDKindResolutionFor(routes *Routes, routePath tree.RoutePath, kind tree.NodeKind) validationPageIDResolution {
	GinkgoHelper()
	pageID, resolved := routes.resolveValidationPageIDForKind(routePath, kind)
	state := validationUnresolved
	if resolved {
		state = validationResolved
	}
	return validationPageIDResolution{PageID: pageID, State: state}
}

func matchValidationPageID(state validationResolutionState, pageIDMatcher types.GomegaMatcher) types.GomegaMatcher {
	GinkgoHelper()
	return gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"State":  Equal(state),
		"PageID": pageIDMatcher,
	})
}

func mcpTokenInfoRequest(userID string) *sdkmcp.CallToolRequest {
	GinkgoHelper()
	return &sdkmcp.CallToolRequest{Extra: &sdkmcp.RequestExtra{TokenInfo: &sdkauth.TokenInfo{UserID: userID}}}
}
