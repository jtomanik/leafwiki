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

	"github.com/google/jsonschema-go/jsonschema"
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

type schemaTypeFixture string

func newFixtureSchemaType[T ~string](raw T) schemaTypeFixture {
	return schemaTypeFixture(raw)
}

func haveSchemaType(want schemaTypeFixture) types.GomegaMatcher {
	GinkgoHelper()
	return WithTransform(func(schema *jsonschema.Schema) schemaTypeFixture {
		return schemaTypeFixture(schema.Type)
	}, Equal(want))
}

var _ = Describe("MCP context checkpoints and route normalization", func() {
	Describe("checkpoint eviction", Label("unit"), func() {
		It("evicts overflow and new sessions while preserving the active session", func() {
			base := time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
			store := newContextCheckpointStore(1)
			store.maxSessions = 2
			oldSession := contextSessionScopeForTest(newFixtureContextSessionID("old"))
			newSession := contextSessionScopeForTest(newFixtureContextSessionID("new"))
			keepSession := contextSessionScopeForTest(newFixtureContextSessionID("keep"))
			store.sessions = map[contextSessionScope][]contextCheckpoint{
				oldSession:  {{Token: "old", CreatedAt: base}},
				newSession:  {{Token: "new", CreatedAt: base.Add(time.Minute)}},
				keepSession: {{Token: "keep", CreatedAt: base.Add(-time.Hour)}},
			}

			store.evictOverflowLocked(keepSession)

			Expect(store.sessions).NotTo(HaveKey(oldSession))
			Expect(store.sessions).To(HaveKey(newSession))
			Expect(store.sessions).To(HaveKey(keepSession))

			store.sessions = map[contextSessionScope][]contextCheckpoint{
				oldSession: {{Token: "old", CreatedAt: base}},
				newSession: {{Token: "new", CreatedAt: base.Add(time.Minute)}},
			}

			store.evictForNewSessionLocked(contextSessionScopeForTest(newFixtureContextSessionID("incoming")))

			Expect(store.sessions).NotTo(HaveKey(oldSession))
			Expect(store.sessions).To(HaveKey(newSession))

			onlyStore := newContextCheckpointStore(1)
			onlyStore.maxSessions = 1
			onlySession := contextSessionScopeForTest(newFixtureContextSessionID("only"))
			onlyStore.sessions = map[contextSessionScope][]contextCheckpoint{
				onlySession: {{Token: "only", CreatedAt: base}},
			}

			onlyStore.evictForNewSessionLocked(onlySession)

			Expect(onlyStore.sessions).To(HaveKey(onlySession))
		})
	})

	Describe("actor and route guards", Label("integration"), func() {
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

			user, err = routes.actorForRequest(mcpTokenInfoRequest(newFixtureUserID("missing-user")))
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
		It("falls back to object schema and no optional tools for unknown gates", Label("unit"), func() {
			Expect(toolOutputSchema(newFixtureToolID("unknown_tool"))).To(haveSchemaType(newFixtureSchemaType("object")))
			Expect(toolNamesForGate(optionalToolGate("unknown"))).To(BeNil())
		})

		It("reports presence, refresh, and snapshot delta behavior", Label("integration"), func() {
			now := time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
			routes := newContextToolTestRoutes()
			routes.webPresenceProvider = func(*coreauth.User) ([]wikipresence.Session, error) {
				return []wikipresence.Session{{Type: wikipresence.SessionTypeWeb, SessionID: wikipresence.WebSessionIDFromString("web-session")}}, nil
			}
			routes.agentPresenceProvider = func() ([]projectdaemon.AgentPresenceSession, error) {
				return []projectdaemon.AgentPresenceSession{{SessionIDHash: "agent-session", Provider: newFixtureProviderID("codex"), FirstSeenAt: now, LastSeenAt: now}}, nil
			}

			sessions, status := routes.activeSessionsForContext(&coreauth.User{ID: newFixtureUserID("editor"), Role: coreauth.RoleEditor})

			Expect(status).To(matchPresenceStatusOutput(gstruct.Fields{
				"Web":        Equal("enabled"),
				"AgentHooks": Equal("enabled"),
			}))
			Expect(sessions).To(HaveExactElements(
				matchPresenceSession(gstruct.Fields{"Type": Equal(wikipresence.SessionTypeAgent)}),
				matchPresenceSession(gstruct.Fields{"Type": Equal(wikipresence.SessionTypeWeb)}),
			))

			huge := 99
			Expect(contextRefreshDecisionFor(contextSyncModeForce, workspacesync.SyncStatus{})).To(Equal(contextRefreshRequested))
			Expect(contextRefreshDecisionFor(contextSyncModeNone, workspacesync.SyncStatus{Enabled: true, PendingEventCount: 1})).To(Equal(contextRefreshSkipped))
			Expect(contextSnapshotPageSize(25)).To(Equal(25))
			Expect(contextSnapshotPageSize(75)).To(Equal(50))
			Expect(boundedContextTreeDepth(&huge)).To(Equal(treeDisplayDepth(maxContextTreeDepth)))
			one := 1
			Expect(boundedContextTreeDepth(&one)).To(Equal(treeDisplayDepth(1)))
			Expect((&Routes{treeService: tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: mcpTestTempDir(), RootDir: mcpTestTempDir()})}).contextTree(1)).To(BeNil())
			Expect(func() { ensureNodeChildrenArray(nil) }).NotTo(Panic())

			_, err := routes.getContext(context.Background(), nil, toolActor{ID: newFixtureUserID("viewer"), User: &coreauth.User{ID: newFixtureUserID("viewer"), Username: "viewer", Role: coreauth.RoleViewer}}, httpinternal.RouterOptions{}, getContextInput{SyncMode: "invalid"})
			Expect(err).To(MatchError(errContextSyncModeInvalid))

			routes.workspaceSyncRefresh = func(context.Context, workspacesync.SyncRequest) (workspacesync.SyncStatus, error) {
				return workspacesync.SyncStatus{}, errors.New("viewer refresh should be skipped")
			}
			routes.workspaceSyncStatus = func() workspacesync.SyncStatus {
				return workspacesync.SyncStatus{Enabled: true, PendingEventCount: 1}
			}
			out, err := routes.getContext(context.Background(), nil, toolActor{ID: newFixtureUserID("viewer"), User: &coreauth.User{ID: newFixtureUserID("viewer"), Username: "viewer", Role: coreauth.RoleViewer}}, httpinternal.RouterOptions{}, getContextInput{})
			Expect(err).NotTo(HaveOccurred())
			Expect(out.Warnings).To(ContainElement("sync refresh skipped because current MCP user is not an editor or admin"))

			routes.listWorkspaceSnapshots = func(context.Context, workspacesync.CommitHash, workspacesync.SnapshotLimit) (workspacesync.SnapshotList, error) {
				return workspacesync.SnapshotList{
					Snapshots: []workspacesync.Snapshot{
						{ID: newFixtureCommitHash("first"), ChangedMarkdownPaths: []string{"home.md"}},
						{ID: newFixtureCommitHash("second"), ChangedMarkdownPaths: []string{"home.md"}},
					},
				}, nil
			}
			Expect(routes.recentChanges(context.Background(), workspacesync.SyncStatus{}, 1)).To(HaveLen(1))

			Expect(changesSinceCommitOutcomeFor(routes, workspacesync.SyncStatus{}, newFixtureCommitHash(""))).To(matchChangesSinceCommit(
				changesReachedRequestedCommit,
				HaveLen(2),
			))

			routes.listWorkspaceSnapshots = nil
			Expect(changesSinceCommitOutcomeFor(routes, workspacesync.SyncStatus{}, newFixtureCommitHash("missing"))).To(matchChangesSinceCommit(
				changesStoppedBeforeRequestedCommit,
				BeNil(),
			))

			routes.listWorkspaceSnapshots = func(context.Context, workspacesync.CommitHash, workspacesync.SnapshotLimit) (workspacesync.SnapshotList, error) {
				return workspacesync.SnapshotList{
					Snapshots: []workspacesync.Snapshot{{ID: newFixtureCommitHash("newer")}},
				}, errors.New("snapshot backend failed")
			}
			Expect(changesSinceCommitOutcomeFor(routes, workspacesync.SyncStatus{}, newFixtureCommitHash("target"))).To(matchChangesSinceCommit(
				changesStoppedBeforeRequestedCommit,
				HaveLen(0),
			))

			routes.listWorkspaceSnapshots = func(_ context.Context, _ workspacesync.CommitHash, limit workspacesync.SnapshotLimit) (workspacesync.SnapshotList, error) {
				snapshots := make([]workspacesync.Snapshot, int(limit))
				for i := range snapshots {
					snapshots[i] = workspacesync.Snapshot{ID: workspacesync.CommitHashFromString(fmt.Sprintf("snapshot-%d", i))}
				}
				return workspacesync.SnapshotList{Snapshots: snapshots, NextCursor: newFixtureCommitHash("next")}, nil
			}
			Expect(changesSinceCommitOutcomeFor(routes, workspacesync.SyncStatus{}, newFixtureCommitHash("target"))).To(matchChangesSinceCommit(
				changesStoppedBeforeRequestedCommit,
				HaveLen(maxContextDeltaSnapshots),
			))

			routes.listWorkspaceSnapshots = func(context.Context, workspacesync.CommitHash, workspacesync.SnapshotLimit) (workspacesync.SnapshotList, error) {
				return workspacesync.SnapshotList{NextCursor: newFixtureCommitHash("next")}, nil
			}
			Expect(changesSinceCommitOutcomeFor(routes, workspacesync.SyncStatus{}, newFixtureCommitHash("target"))).To(matchChangesSinceCommit(
				changesStoppedBeforeRequestedCommit,
				BeEmpty(),
			))

			routes.contextStore.record(contextSessionKey(nil, toolActor{ID: newFixtureUserID("viewer")}), contextCheckpoint{Token: "old-token", CommitHash: newFixtureCommitHash("old")})
			routes.workspaceSyncStatus = func() workspacesync.SyncStatus {
				return workspacesync.SyncStatus{LastCommitHash: newFixtureCommitHash("new")}
			}
			out, err = routes.getContext(context.Background(), nil, toolActor{ID: newFixtureUserID("viewer"), User: &coreauth.User{ID: newFixtureUserID("viewer"), Username: "viewer", Role: coreauth.RoleViewer}}, httpinternal.RouterOptions{}, getContextInput{SinceToken: "old-token"})
			Expect(err).NotTo(HaveOccurred())
			Expect(out.Warnings).To(ContainElement("changesSincePreviousContext truncated before sinceToken checkpoint"))

			statusForFallback := workspacesync.SyncStatus{
				LastCommitHash:             newFixtureCommitHash("latest"),
				RecentChangedMarkdownPaths: []string{"home.md"},
			}
			change := routes.recentChangeFromSnapshot(statusForFallback, workspacesync.Snapshot{ID: newFixtureCommitHash("latest")})
			Expect(change.ChangedPaths).To(Equal([]string{"home.md"}))
		})

		It("normalizes markdown paths and redacts workspace paths", Label("integration"), func() {
			routes := newContextToolTestRoutes()
			home, err := routes.treeService.FindPageByRoutePathAndKind(newFixtureRoutePath("home"), tree.NodeKindPage)
			Expect(err).NotTo(HaveOccurred())

			Expect(routes.pageIDForMarkdownPath("home/README.md")).To(BeEmpty())
			Expect(routes.pageIDForMarkdownPath("README.md")).To(Equal(tree.RootPageID))
			Expect(routes.pageIDForMarkdownPath("bad/../README.md")).To(Equal(tree.RootPageID))
			Expect(routes.pageIDForMarkdownPath("bad/ /README.md")).To(BeEmpty())
			Expect(routes.pageIDForMarkdownPath("../bad.md")).To(BeEmpty())
			Expect(routes.pageIDsForMarkdownPaths([]string{"home.md", "home.md"})).To(Equal([]tree.PageID{home.ID}))
			Expect((&Routes{treeService: tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: mcpTestTempDir(), RootDir: mcpTestTempDir()})}).pageIDForRecentChangeRoute(newFixtureRoutePath(""), tree.NodeKindSection)).To(BeEmpty())
			Expect(routes.pageIDForRecentChangeRoute(newFixtureRoutePath("missing"), newFixtureNodeKind(""))).To(BeEmpty())

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

	Describe("navigation and partial-edit helpers", Label("integration"), func() {
		It("maps subtree and partial-edit failures to semantic errors", func() {
			unloadedTree := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: mcpTestTempDir(), RootDir: mcpTestTempDir()})
			_, err := (&Routes{treeService: unloadedTree}).getSubtree(context.Background(), getSubtreeInput{})
			Expect(err).To(MatchError(tree.ErrPageNotFound))

			routes := newContextToolTestRoutes()
			Expect(routes.subtreeContentPreview(newFixturePageID("missing"))).To(BeEmpty())
			Expect(func() { ensureSubtreeNodeChildrenArray(&subtreeNode{}) }).NotTo(Panic())
			parent := &tree.PageNode{Children: []*tree.PageNode{{Children: []*tree.PageNode{{}}}}}
			Expect(subtreeExtentFor(parent, treeDisplayDepth(1))).To(Equal(subtreeExtentTruncated))

			emptyVersionPage := &tree.Page{PageNode: &tree.PageNode{ID: newFixturePageID("empty"), Title: "Empty", Slug: newFixtureSlug("empty"), Kind: tree.NodeKindPage}}
			Expect(partialEditVersionPreflight(newFixturePageVersion(""), nil)).To(Succeed())
			Expect(partialEditVersionPreflight(newFixturePageVersion(""), emptyVersionPage)).To(Succeed())

			home, err := routes.treeService.FindPageByRoutePathAndKind(newFixtureRoutePath("home"), tree.NodeKindPage)
			Expect(err).NotTo(HaveOccurred())
			Expect(partialEditVersionPreflight(newFixturePageVersion(""), home)).To(matchMCPToolLocalizedError(wikipages.ErrCodePageVersionRequired))
			Expect(partialEditVersionPreflight(newFixturePageVersion("stale"), home)).To(matchMCPToolLocalizedError(wikipages.ErrCodePageVersionConflict))
			otherErr := errors.New("other")
			Expect(partialEditWriteError(otherErr, nil)).To(MatchError(otherErr))
			Expect(partialEditWriteError(otherErr, home)).To(MatchError(otherErr))

			missingPage := &tree.Page{PageNode: &tree.PageNode{ID: newFixturePageID("missing"), Title: "Missing", Slug: newFixtureSlug("missing"), Kind: tree.NodeKindPage}}
			_, err = routes.partialEditOutput(context.Background(), missingPage, false, nil, false)
			Expect(err).To(MatchError(tree.ErrPageNotFound))

			var input updatePageInput
			Expect(input.UnmarshalJSON([]byte(`{`))).To(matchJSONSyntaxError())
		})
	})

	Describe("validation and refresh helpers", Label("integration"), func() {
		It("resolves validation links, paths, and page IDs semantically", func() {
			routes := newContextToolTestRoutes()
			routes.workspaceRootDir = ""
			home, err := routes.treeService.FindPageByRoutePathAndKind(newFixtureRoutePath("home"), tree.NodeKindPage)
			Expect(err).NotTo(HaveOccurred())
			sectionID, err := routes.treeService.CreateNode(newFixtureUserID("system"), nil, "Guide", newFixtureSlug("guide"), testNodeKindPtr(tree.NodeKindSection))
			Expect(err).NotTo(HaveOccurred())

			Expect(validationMarkdownLinkResolutionFor((*Routes)(nil), newFixtureRoutePath(""), tree.NodeKindPage, "missing.md")).To(matchValidationMarkdownLink(
				validationUnresolved,
				BeEmpty(),
				BeEmpty(),
				Equal(wikivalidation.IssueCodeBrokenLink),
			))

			Expect(validationMarkdownLinkResolutionFor(routes, newFixtureRoutePath(""), tree.NodeKindPage, "https://example.com")).To(matchValidationMarkdownLink(
				validationResolved,
				BeEmpty(),
				BeEmpty(),
				BeEmpty(),
			))

			Expect(validationMarkdownLinkResolutionFor(routes, newFixtureRoutePath(""), tree.NodeKindPage, "%zz")).To(matchValidationMarkdownLink(
				validationUnresolved,
				gstruct.Ignore(),
				gstruct.Ignore(),
				Equal(wikivalidation.IssueCodeInvalidLink),
			))

			Expect(validationMarkdownLinkResolutionFor(routes, newFixtureRoutePath(""), tree.NodeKindPage, "../escape.md")).To(matchValidationMarkdownLink(
				validationUnresolved,
				gstruct.Ignore(),
				gstruct.Ignore(),
				Equal(wikivalidation.IssueCodeInvalidLink),
			))

			Expect(validationMarkdownLinkResolutionFor(routes, newFixtureRoutePath(""), tree.NodeKindPage, "/home.md")).To(matchValidationMarkdownLink(
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

			_, _, err = routes.normalizeValidationContentPathInput("../bad", newFixtureNodeKind(""))
			Expect(err).To(MatchError(tree.ErrInvalidRoutePath))
			_, _, err = routes.normalizeValidationContentPathInput("bad//page.md", newFixtureNodeKind(""))
			Expect(err).To(MatchError(tree.ErrInvalidRoutePath))
			_, _, err = routes.normalizeValidationContentPathInput("bad//page.md", tree.NodeKindPage)
			Expect(err).To(MatchError(tree.ErrInvalidRoutePath))
			_, _, err = routes.normalizeValidationContentPathInput("bad//route", tree.NodeKindPage)
			Expect(err).To(MatchError(tree.ErrInvalidRoutePath))
			_, _, err = routes.normalizeValidationContentPathToolInput("home", "invalid-kind")
			Expect(err).To(matchMCPToolLocalizedError(wikipages.ErrCodePageInvalidKind))
			_, _, err = routes.normalizeValidationContentPathToolInput("guide/README.md", "invalid-kind")
			Expect(err).To(matchMCPToolLocalizedError(wikipages.ErrCodePageInvalidKind))
			_, _, err = routes.normalizeValidationContentPathInput("bad//README.md", newFixtureNodeKind(""))
			Expect(err).To(MatchError(tree.ErrInvalidRoutePath))
			routePath, sourceKind, err := routes.normalizeValidationContentPathInput("README.md", newFixtureNodeKind(""))
			Expect(err).NotTo(HaveOccurred())
			Expect(routePath).To(Equal(newFixtureRoutePath("README")))
			Expect(sourceKind).To(Equal(tree.NodeKindPage))
			routePath, sourceKind, err = routes.normalizeValidationContentPathInput("", newFixtureNodeKind(""))
			Expect(err).NotTo(HaveOccurred())
			Expect(routePath).To(BeEmpty())
			Expect(sourceKind).To(BeEmpty())
			fallbackTree := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: mcpTestTempDir(), RootDir: mcpTestTempDir()})
			Expect(fallbackTree.LoadTree()).To(Succeed())
			fallbackRoutes := &Routes{treeService: fallbackTree}
			guideDir := filepath.Join(fallbackTree.RootDir(), "guide")
			Expect(os.MkdirAll(guideDir, 0o755)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(guideDir, "README.md"), []byte("# Guide\n"), 0o644)).To(Succeed())

			routePath, sourceKind, err = fallbackRoutes.normalizeValidationContentPathInput("guide/README.md", newFixtureNodeKind(""))
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
			_, _, err = routes.normalizeValidationContentPathInput("bad/../README.md", newFixtureNodeKind(""))
			Expect(err).To(MatchError(tree.ErrInvalidRoutePath))
			_, err = routes.treeService.CreateNode(newFixtureUserID("system"), sectionID, "Guide Readme", newFixtureSlug("README"), nil)
			Expect(err).NotTo(HaveOccurred())
			routePath, sourceKind, err = routes.normalizeValidationContentPathInput("guide/README.md", newFixtureNodeKind(""))
			Expect(err).NotTo(HaveOccurred())
			Expect(routePath).To(Equal(newFixtureRoutePath("guide/README")))
			Expect(sourceKind).To(Equal(tree.NodeKindPage))

			Expect(validationPageIDKindResolutionFor(routes, newFixtureRoutePath(""), tree.NodeKindPage)).To(matchValidationPageID(validationUnresolved, BeEmpty()))
			Expect(validationPageIDKindResolutionFor((*Routes)(nil), newFixtureRoutePath("home"), tree.NodeKindPage)).To(matchValidationPageID(validationUnresolved, BeEmpty()))
			Expect(validationPageIDResolutionFor(&Routes{}, newFixtureRoutePath("home"))).To(matchValidationPageID(validationUnresolved, BeEmpty()))
			Expect(validationPageIDResolutionFor(routes, newFixtureRoutePath("missing"))).To(matchValidationPageID(validationUnresolved, BeEmpty()))
			Expect(routes.validationSourceKind(newFixturePageID("missing"))).To(Equal(tree.NodeKindPage))
			Expect(validationPagePresenceFor(routes, *sectionID)).To(matchValidationPagePresence(validationPagePresent, Equal(*sectionID)))
			Expect(validationPagePresenceFor(&Routes{}, home.ID)).To(matchValidationPagePresence(validationPageAbsent, Equal(home.ID)))
			Expect(validationPagePresenceFor(routes, home.ID)).To(matchValidationPagePresence(validationPagePresent, Equal(home.ID)))
		})

		It("normalizes validation asset destinations through the real list use case", func() {
			routes := newContextToolTestRoutes()
			home, err := routes.treeService.FindPageByRoutePathAndKind(newFixtureRoutePath("home"), tree.NodeKindPage)
			Expect(err).NotTo(HaveOccurred())
			assetService := coreassets.NewAssetService(mcpTestTempDir(), tree.NewSlugService())
			_, err = assetService.SaveAssetForPage(home.PageNode, &memoryMultipartFile{Reader: bytes.NewReader([]byte("logo"))}, newFixtureAssetName("logo.png"), 1024)
			Expect(err).NotTo(HaveOccurred())
			routes.getAssets = wikiassets.NewListAssetsUseCase(routes.treeService, assetService)

			exists := routes.validationAssetExists(context.Background(), home.ID)

			Expect(validationAssetDestinationFor(home.ID, "logo.png", exists)).To(matchValidationAssetPresence(validationAssetPresent, Equal("logo.png")))
			Expect(validationAssetDestinationFor(home.ID, "/assets/"+home.ID.MetadataValue()+"/logo.png", exists)).To(matchValidationAssetPresence(validationAssetPresent, Equal("/assets/"+home.ID.MetadataValue()+"/logo.png")))
			Expect(validationAssetDestinationFor(home.ID, "<logo.png?size=1#preview>", exists)).To(matchValidationAssetPresence(validationAssetPresent, Equal("<logo.png?size=1#preview>")))
			Expect(validationAssetDestinationFor(home.ID, "nested/logo.png", exists)).To(matchValidationAssetPresence(validationAssetAbsent, Equal("nested/logo.png")))

			predicate := validationAssetPredicate(home.ID, []string{"", "assets/" + home.ID.MetadataValue() + "/icon.png"})
			Expect(validationAssetDestinationFor(home.ID, "icon.png", predicate)).To(matchValidationAssetPresence(validationAssetPresent, Equal("icon.png")))
			Expect(validationAssetDestinationFor(home.ID, "missing.png", predicate)).To(matchValidationAssetPresence(validationAssetAbsent, Equal("missing.png")))
			slashLogoPredicate := validationAssetPredicate(home.ID, []string{"logo.png"})
			Expect(validationAssetDestinationFor(home.ID, "/logo.png", slashLogoPredicate)).To(matchValidationAssetPresence(validationAssetPresent, Equal("/logo.png")))
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
			_, err = routes.refreshWorkspaceSync(context.Background(), toolActor{ID: newFixtureUserID("editor"), User: &coreauth.User{ID: newFixtureUserID("editor"), Username: "editor"}}, refreshInput{Validate: &validate})
			Expect(err).To(MatchError(refreshErr))

			routes.workspaceSyncRefresh = func(_ context.Context, req workspacesync.SyncRequest) (workspacesync.SyncStatus, error) {
				Expect(req.Source).To(Equal(workspacesync.SourceMCP))
				Expect(req.Actor.ID).To(Equal(workspacesync.ActorIDFromUserID(coreauth.UserIDFromString("editor"))))
				return workspacesync.SyncStatus{
					Enabled:                    true,
					LastCommitHash:             newFixtureCommitHash("commit-1"),
					RecentChangedMarkdownPaths: []string{"home.md"},
					ValidationErrors: []workspacesync.ValidationError{
						{Path: "/workspace/home.md", Message: "broken", Severity: wikivalidation.IssueSeverityError},
					},
				}, nil
			}

			out, err := routes.refreshWorkspaceSync(context.Background(), toolActor{ID: newFixtureUserID("editor"), User: &coreauth.User{ID: newFixtureUserID("editor"), Username: "editor"}}, refreshInput{Validate: &validate})

			Expect(err).NotTo(HaveOccurred())
			Expect(out).To(matchRefreshOutput(gstruct.Fields{
				"LastCommitHash":     Equal("commit-1"),
				"RecentChangedPaths": Equal([]string{"home.md"}),
				"Validation":         gstruct.PointTo(matchValidationOutputWithErrorCount(1)),
			}))
		})
	})
})
