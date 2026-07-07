package mcp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/perber/wiki/internal/core/auth"
	"github.com/perber/wiki/internal/core/tree"
	httpinternal "github.com/perber/wiki/internal/http"
	"github.com/perber/wiki/internal/workspacesync"
)

// Canonical Markdown links plan scenarios covered by tests in this file:
// - GitHub README section import becomes section link

var _ = Describe("context tool helpers", func() {
	It("handles case-insensitive markdown path names", Label("unit"), func() {
		tests := map[string]string{
			"Docs/API.MD":      "Docs/API",
			"Docs/INDEX.MD":    "Docs",
			"Docs/Index.md":    "Docs",
			"Docs/Nested/Page": "Docs/Nested/Page",
			"/Docs/Nested.MD":  "Docs/Nested",
			" Docs/Trim.MD \n": "Docs/Trim",
		}
		for input, want := range tests {
			Expect(tree.MarkdownPathToRoutePath(input)).To(Equal(want))
		}
	})

	It("resolves root index markdown paths to the root page ID", Label("integration"), func() {
		routes := newContextToolTestRoutes()

		got := routes.pageIDsForMarkdownPaths([]string{"index.md"})

		Expect(got).To(Equal([]tree.PageID{tree.RootPageID}))
	})

	It("uses markdown file kind for same-basename twins", Label("integration"), func() {
		routes := newContextToolTestRoutes()

		sectionID, err := routes.treeService.CreateNode(newFixtureUserID("system"), nil, "Sync Section", newFixtureSlug("sync"), testNodeKindPtr(tree.NodeKindSection))
		Expect(err).To(Succeed())
		pageID, err := routes.treeService.CreateNode(newFixtureUserID("system"), nil, "Sync Page", newFixtureSlug("sync"), testNodeKindPtr(tree.NodeKindPage))
		Expect(err).To(Succeed())

		pageIDs := routes.pageIDsForMarkdownPaths([]string{"sync.md"})
		Expect(pageIDs).To(Equal([]tree.PageID{*pageID}))

		sectionIDs := routes.pageIDsForMarkdownPaths([]string{"sync/index.md"})
		Expect(sectionIDs).To(Equal([]tree.PageID{*sectionID}))
	})

	It("resolves README fallback markdown paths to sections", Label("integration"), func() {
		routes := newContextToolTestRoutes()

		sectionID, err := routes.treeService.CreateNode(newFixtureUserID("system"), nil, "Guide", newFixtureSlug("guide"), testNodeKindPtr(tree.NodeKindSection))
		Expect(err).To(Succeed())

		pageIDs := routes.pageIDsForMarkdownPaths([]string{"guide/README.md"})
		Expect(pageIDs).To(Equal([]tree.PageID{*sectionID}))
	})

	It("uses workspace route normalization for README sections", Label("integration"), func() {
		routes := newContextToolTestRoutes()
		workspaceRoot := mcpTestTempDir()
		routes.workspaceRootDir = workspaceRoot
		Expect(os.MkdirAll(filepath.Join(workspaceRoot, "User Guides"), 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(workspaceRoot, "User Guides", "README.md"), []byte("# User Guides\n"), 0o644)).To(Succeed())
		sectionID, err := routes.treeService.CreateNode(newFixtureUserID("system"), nil, "User Guides", newFixtureSlug("user-guides"), testNodeKindPtr(tree.NodeKindSection))
		Expect(err).To(Succeed())

		pageIDs := routes.pageIDsForMarkdownPaths([]string{"User Guides/README.md"})
		Expect(pageIDs).To(Equal([]tree.PageID{*sectionID}))
	})

	It("does not fallback lowercase readme markdown paths", Label("integration"), func() {
		routes := newContextToolTestRoutes()

		_, err := routes.treeService.CreateNode(newFixtureUserID("system"), nil, "Guide", newFixtureSlug("guide"), testNodeKindPtr(tree.NodeKindSection))
		Expect(err).To(Succeed())

		pageIDs := routes.pageIDsForMarkdownPaths([]string{"guide/readme.md"})
		Expect(pageIDs).To(BeEmpty())
	})

	It("uses workspace route normalization for plan paths", Label("integration"), func() {
		routes := newContextToolTestRoutes()
		plansID, err := routes.treeService.CreateNode(newFixtureUserID("system"), nil, "Plans", newFixtureSlug("plans"), testNodeKindPtr(tree.NodeKindSection))
		Expect(err).To(Succeed())
		pageID, err := routes.treeService.CreateNode(newFixtureUserID("system"), plansID, "Agent Hooks Plan", newFixtureSlug("agent-hooks-plan"), testNodeKindPtr(tree.NodeKindPage))
		Expect(err).To(Succeed())

		pageIDs := routes.pageIDsForMarkdownPaths([]string{"plans/agent_hooks.PLAN.md"})
		Expect(pageIDs).To(Equal([]tree.PageID{*pageID}))
	})

	It("resolves recent root index changes to the root page ID", Label("integration"), func() {
		routes := newContextToolTestRoutes()
		ctx := context.Background()
		createdAt := time.Date(2026, 6, 8, 13, 0, 0, 0, time.UTC)
		routes.listWorkspaceSnapshots = func(context.Context, workspacesync.CommitHash, workspacesync.SnapshotLimit) (workspacesync.SnapshotList, error) {
			return workspacesync.SnapshotList{
				Snapshots: []workspacesync.Snapshot{{
					ID:                   newFixtureCommitHash("root-index-commit"),
					CreatedAt:            createdAt,
					ChangedMarkdownCount: 1,
					ChangedMarkdownPaths: []string{"index.md"},
				}},
			}, nil
		}

		changes := routes.recentChanges(ctx, workspacesync.SyncStatus{}, 1)

		Expect(changes).To(HaveExactElements(HaveField("PageIDs", Equal([]tree.PageID{tree.RootPageID}))))
	})

	It("handles context sync modes and session history", Label("integration"), func() {
		routes := newContextToolTestRoutes()
		actor := toolActor{ID: newFixtureUserID("editor-1"), User: &auth.User{ID: newFixtureUserID("editor-1"), Username: "editor", Role: auth.RoleEditor}}
		opts := httpinternal.RouterOptions{AuthDisabled: true, EnableWorkspaceSync: true}
		ctx := context.Background()

		refreshCalls := 0
		status := workspacesync.SyncStatus{Enabled: true, WatcherRunning: true, LastCommitHash: newFixtureCommitHash("healthy")}
		routes.workspaceSyncStatus = func() workspacesync.SyncStatus { return status }
		routes.workspaceSyncRefresh = func(context.Context, workspacesync.SyncRequest) (workspacesync.SyncStatus, error) {
			refreshCalls++
			return status, nil
		}

		healthy, err := routes.getContext(ctx, nil, actor, opts, getContextInput{SyncMode: contextSyncModeAuto})
		Expect(err).To(Succeed())
		Expect(refreshCalls).To(BeZero())
		next, err := routes.getContext(ctx, nil, actor, opts, getContextInput{SyncMode: contextSyncModeNone})
		Expect(err).To(Succeed())
		Expect(next.PreviousContextToken).To(Equal(healthy.ContextToken))

		status.WatcherEnabled = false
		status.WatcherRunning = false
		_, err = routes.getContext(ctx, nil, actor, opts, getContextInput{SyncMode: contextSyncModeAuto})
		Expect(err).To(Succeed())
		Expect(refreshCalls).To(BeZero())
		status.WatcherEnabled = true
		_, err = routes.getContext(ctx, nil, actor, opts, getContextInput{SyncMode: contextSyncModeAuto})
		Expect(err).To(Succeed())
		Expect(refreshCalls).To(Equal(1))
		status.WatcherRunning = true

		status.PendingEventCount = 2
		status.LastCommitHash = newFixtureCommitHash("pending")
		_, err = routes.getContext(ctx, nil, actor, opts, getContextInput{SyncMode: contextSyncModeAuto})
		Expect(err).To(Succeed())
		Expect(refreshCalls).To(Equal(2))

		status.PendingEventCount = 0
		status.LastError = "previous sync failed"
		status.LastCommitHash = newFixtureCommitHash("errored")
		routes.workspaceSyncRefresh = func(context.Context, workspacesync.SyncRequest) (workspacesync.SyncStatus, error) {
			refreshCalls++
			return status, errors.New("sync still failed")
		}
		errored, err := routes.getContext(ctx, nil, actor, opts, getContextInput{SyncMode: contextSyncModeAuto})
		Expect(err).To(Succeed())
		Expect(refreshCalls).To(Equal(3))
		Expect(errored.SyncStatus).To(matchMCPSyncLastErrorDetail(errCodeMCPWorkspaceSyncFailed))

		routes.workspaceSyncRefresh = func(context.Context, workspacesync.SyncRequest) (workspacesync.SyncStatus, error) {
			refreshCalls++
			return status, nil
		}
		status.LastError = ""
		status.LastCommitHash = newFixtureCommitHash("force")
		_, err = routes.getContext(ctx, nil, actor, opts, getContextInput{SyncMode: contextSyncModeForce})
		Expect(err).To(Succeed())
		Expect(refreshCalls).To(Equal(4))

		status.LastError = "reported without refresh"
		status.LastCommitHash = newFixtureCommitHash("none")
		none, err := routes.getContext(ctx, nil, actor, opts, getContextInput{SyncMode: contextSyncModeNone})
		Expect(err).To(Succeed())
		Expect(refreshCalls).To(Equal(4))
		Expect(none.SyncStatus).To(matchMCPSyncLastErrorDetail(errCodeMCPWorkspaceSyncFailed))

		missing, err := routes.getContext(ctx, nil, actor, opts, getContextInput{SinceToken: "missing", SyncMode: contextSyncModeNone})
		Expect(err).To(Succeed())
		Expect(missing).To(SatisfyAll(
			HaveField("Warnings", HaveExactElements("unknown sinceToken; returned current context")),
			HaveField("PreviousContextToken", BeEmpty()),
			HaveField("ChangesSincePreviousContext", BeEmpty()),
		))

		var latest contextOutput
		for i := 0; i < 12; i++ {
			latest, err = routes.getContext(ctx, nil, actor, opts, getContextInput{SyncMode: contextSyncModeNone})
			Expect(err).To(Succeed())
		}
		Expect(latest.ContextHistory).To(HaveLen(10))

		otherActor := toolActor{ID: newFixtureUserID("editor-2"), User: &auth.User{ID: newFixtureUserID("editor-2"), Username: "other", Role: auth.RoleEditor}}
		other, err := routes.getContext(ctx, nil, otherActor, opts, getContextInput{SyncMode: contextSyncModeNone})
		Expect(err).To(Succeed())
		Expect(other.ContextHistory).To(HaveLen(1))
	})

	It("clamps context input limits", Label("unit"), func() {
		huge := 999
		negative := -1

		Expect(boundedContextTreeDepth(nil)).To(Equal(treeDisplayDepth(defaultContextTreeDepth)))
		Expect(boundedContextTreeDepth(&huge)).To(Equal(treeDisplayDepth(maxContextTreeDepth)))
		Expect(boundedContextTreeDepth(&negative)).To(Equal(treeDisplayDepth(defaultContextTreeDepth)))
		Expect(boundedRecentChangesLimit(nil)).To(Equal(defaultContextRecentChangesLimit))
		Expect(boundedRecentChangesLimit(&huge)).To(Equal(maxContextRecentChangesLimit))
		Expect(boundedRecentChangesLimit(&negative)).To(Equal(defaultContextRecentChangesLimit))
	})

	It("redacts sync status last-error paths", Label("integration"), func() {
		routes := newContextToolTestRoutes()
		rootDir := filepath.Join(mcpTestTempDir(), "content")
		dataDir := filepath.Join(mcpTestTempDir(), "data")
		routes.workspaceRootDir = rootDir
		routes.workspaceDataDir = dataDir
		routes.workspaceSyncStatus = func() workspacesync.SyncStatus {
			return workspacesync.SyncStatus{
				Enabled:        true,
				WatcherRunning: true,
				LastCommitHash: newFixtureCommitHash("redact"),
				LastError: fmt.Sprintf(
					"open %s: permission denied; stat %s: no such file",
					filepath.Join(rootDir, "docs", "api.md"),
					filepath.Join(dataDir, ".leafwiki", "git", "HEAD"),
				),
				ValidationErrors: []workspacesync.ValidationError{
					{
						Path:     filepath.Join(rootDir, "docs", "bad.md"),
						Message:  "validate " + filepath.Join(dataDir, ".leafwiki", "work", "bad.md") + ": failed",
						Severity: newFixtureIssueSeverity("error"),
					},
				},
			}
		}
		actor := toolActor{ID: newFixtureUserID("editor-1"), User: &auth.User{ID: newFixtureUserID("editor-1"), Username: "editor", Role: auth.RoleEditor}}

		out, err := routes.getContext(context.Background(), nil, actor, httpinternal.RouterOptions{EnableWorkspaceSync: true}, getContextInput{SyncMode: contextSyncModeNone})
		Expect(err).To(Succeed())
		Expect(out.SyncStatus).To(SatisfyAll(
			matchRedactedMCPSyncLastErrorDetail(errCodeMCPWorkspaceSyncFailed, rootDir, dataDir),
			matchWorkspaceValidationErrorDetails(matchWorkspaceValidationError("<root-dir>/docs/bad.md", "<data-dir>/.leafwiki/work/bad.md", rootDir, dataDir)),
		))
		Expect(out.Validation.Issues).To(HaveExactElements(
			matchRedactedValidationIssueOutput("<root-dir>/docs/bad.md", "<data-dir>/.leafwiki/work/bad.md", rootDir, dataDir),
		))
	})

	It("evicts old checkpoint sessions", Label("unit"), func() {
		store := newContextCheckpointStore(2)
		base := time.Date(2026, 6, 8, 12, 0, 0, 0, time.UTC)

		for i := 0; i < defaultContextCheckpointSessions+4; i++ {
			store.record(contextSessionScopeForTest(newFixtureContextSessionID(fmt.Sprintf("session-%03d", i))), contextCheckpoint{CreatedAt: base.Add(time.Duration(i) * time.Second)})
		}

		store.mu.Lock()
		sessions := map[contextSessionScope][]contextCheckpoint{}
		for id, history := range store.sessions {
			sessions[id] = history
		}
		store.mu.Unlock()

		Expect(sessions).To(SatisfyAll(
			HaveLen(defaultContextCheckpointSessions),
			Not(HaveKey(contextSessionScopeForTest(newFixtureContextSessionID("session-000")))),
			HaveKey(contextSessionScopeForTest(newFixtureContextSessionID(fmt.Sprintf("session-%03d", defaultContextCheckpointSessions+3)))),
		))
	})

	It("preserves the current checkpoint session when overflow removes another session", Label("unit"), func() {
		store := newContextCheckpointStore(2)
		store.maxSessions = 2
		store.ttl = time.Hour
		base := time.Date(2026, 6, 8, 12, 0, 0, 0, time.UTC)
		store.sessions[contextSessionScopeForTest(newFixtureContextSessionID("current"))] = []contextCheckpoint{{Token: "current-old", CreatedAt: base.Add(2 * time.Minute)}}
		store.sessions[contextSessionScopeForTest(newFixtureContextSessionID("oldest"))] = []contextCheckpoint{{Token: "oldest", CreatedAt: base}}
		store.sessions[contextSessionScopeForTest(newFixtureContextSessionID("middle"))] = []contextCheckpoint{{Token: "middle", CreatedAt: base.Add(time.Minute)}}

		_, history := store.record(contextSessionScopeForTest(newFixtureContextSessionID("current")), contextCheckpoint{CreatedAt: base.Add(3 * time.Minute)})

		Expect(history).To(HaveExactElements(
			HaveField("Token", Equal("current-old")),
			HaveField("CreatedAt", Equal(base.Add(3*time.Minute))),
		))
		store.mu.Lock()
		sessions := map[contextSessionScope][]contextCheckpoint{}
		for id, history := range store.sessions {
			sessions[id] = history
		}
		store.mu.Unlock()
		Expect(sessions).To(SatisfyAll(
			HaveLen(2),
			HaveKey(contextSessionScopeForTest(newFixtureContextSessionID("current"))),
			HaveKey(contextSessionScopeForTest(newFixtureContextSessionID("middle"))),
			Not(HaveKey(contextSessionScopeForTest(newFixtureContextSessionID("oldest")))),
		))
	})

	It("prunes expired checkpoint sessions", Label("unit"), func() {
		store := newContextCheckpointStore(2)
		store.ttl = time.Minute
		base := time.Date(2026, 6, 8, 12, 0, 0, 0, time.UTC)

		store.record(contextSessionScopeForTest(newFixtureContextSessionID("expired")), contextCheckpoint{CreatedAt: base})
		store.record(contextSessionScopeForTest(newFixtureContextSessionID("fresh")), contextCheckpoint{CreatedAt: base.Add(30 * time.Second)})
		store.record(contextSessionScopeForTest(newFixtureContextSessionID("current")), contextCheckpoint{CreatedAt: base.Add(2 * time.Minute)})

		store.mu.Lock()
		sessions := map[contextSessionScope][]contextCheckpoint{}
		for id, history := range store.sessions {
			sessions[id] = history
		}
		store.mu.Unlock()

		Expect(sessions).To(SatisfyAll(
			HaveKey(contextSessionScopeForTest(newFixtureContextSessionID("current"))),
			Not(HaveKey(contextSessionScopeForTest(newFixtureContextSessionID("expired")))),
			Not(HaveKey(contextSessionScopeForTest(newFixtureContextSessionID("fresh")))),
		))
	})

	It("expires current-session checkpoint tokens", Label("unit"), func() {
		store := newContextCheckpointStore(2)
		freshTime := time.Now().UTC()
		expiredTime := freshTime.Add(-2 * time.Minute)

		store.ttl = 0
		_, history := store.record(contextSessionScopeForTest(newFixtureContextSessionID("current")), contextCheckpoint{CreatedAt: expiredTime})
		expiredToken := history[0].Token
		_, history = store.record(contextSessionScopeForTest(newFixtureContextSessionID("current")), contextCheckpoint{CreatedAt: freshTime})
		freshToken := history[len(history)-1].Token
		store.ttl = time.Minute

		history = currentSessionHistoryAfterCheckpointLookup(store, expiredToken)
		Expect(history).To(HaveExactElements(SatisfyAll(
			HaveField("Token", Equal(freshToken)),
			Not(HaveField("Token", Equal(expiredToken))),
		)))
	})

	It("separates sync-status validation warnings from errors", Label("unit"), func() {
		validation := validationFromSyncStatus(workspacesync.SyncStatus{
			ValidationErrors: []workspacesync.ValidationError{
				{Path: "hidden.md", Message: "hidden markdown file", Severity: newFixtureIssueSeverity("warning")},
			},
		})

		Expect(validation).To(matchValidationOutputWithWarningCount(1))
	})
})

func testNodeKindPtr(kind tree.NodeKind) *tree.NodeKind {
	return &kind
}

func currentSessionHistoryAfterCheckpointLookup(store *contextCheckpointStore, token string) []contextCheckpoint {
	GinkgoHelper()
	_, found := store.find(contextSessionScopeForTest(newFixtureContextSessionID("current")), token)
	if found {
		return nil
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	return append([]contextCheckpoint(nil), store.sessions[contextSessionScopeForTest(newFixtureContextSessionID("current"))]...)
}

func contextSessionScopeForTest(sessionID contextSessionID) contextSessionScope {
	return contextSessionScope{ActorID: newFixtureUserID("context-store-user"), SessionID: sessionID}
}

func newContextToolTestRoutes() *Routes {
	GinkgoHelper()

	treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{
		DataDir: mcpTestTempDir(),
		RootDir: mcpTestTempDir(),
	})
	Expect(treeService.LoadTree()).To(Succeed())
	id, err := treeService.CreateNode(newFixtureUserID("system"), nil, "Home", newFixtureSlug("home"), nil)
	Expect(err).To(Succeed())
	Expect(id).NotTo(BeNil())
	now := time.Date(2026, 6, 8, 12, 0, 0, 0, time.UTC)
	return &Routes{
		treeService:      treeService,
		contextStore:     newContextCheckpointStore(10),
		userResolver:     nil,
		getAssets:        nil,
		workspaceRootDir: mcpTestTempDir(),
		workspaceSyncStatus: func() workspacesync.SyncStatus {
			return workspacesync.SyncStatus{Enabled: true, WatcherRunning: true, LastSyncTime: now}
		},
	}
}
