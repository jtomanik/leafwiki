package mcp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/perber/wiki/internal/core/auth"
	"github.com/perber/wiki/internal/core/tree"
	httpinternal "github.com/perber/wiki/internal/http"
	"github.com/perber/wiki/internal/workspacesync"
)

// Canonical Markdown links plan scenarios covered by tests in this file:
// - GitHub README section import becomes section link

func TestMarkdownPathToRoutePathHandlesCaseInsensitiveMarkdownNames(t *testing.T) {
	tests := map[string]string{
		"Docs/API.MD":      "Docs/API",
		"Docs/INDEX.MD":    "Docs",
		"Docs/Index.md":    "Docs",
		"Docs/Nested/Page": "Docs/Nested/Page",
		"/Docs/Nested.MD":  "Docs/Nested",
		" Docs/Trim.MD \n": "Docs/Trim",
	}
	for input, want := range tests {
		if got := tree.MarkdownPathToRoutePath(input); got != want {
			t.Fatalf("MarkdownPathToRoutePath(%q) = %q, want %q", input, got, want)
		}
	}
}

func testNodeKindPtr(kind tree.NodeKind) *tree.NodeKind {
	return &kind
}

func TestPageIDsForMarkdownPathsResolvesRootIndex(t *testing.T) {
	routes := newContextToolTestRoutes(t)

	got := routes.pageIDsForMarkdownPaths([]string{"index.md"})

	if len(got) != 1 || got[0] != tree.RootPageID {
		t.Fatalf("pageIDsForMarkdownPaths(index.md) = %v, want [root]", got)
	}
}

func TestPageIDsForMarkdownPathsUsesMarkdownFileKindForSameBasenameTwins(t *testing.T) {
	routes := newContextToolTestRoutes(t)

	sectionID, err := routes.treeService.CreateNode("system", nil, "Sync Section", "sync", testNodeKindPtr(tree.NodeKindSection))
	if err != nil {
		t.Fatalf("CreateNode section failed: %v", err)
	}
	pageID, err := routes.treeService.CreateNode("system", nil, "Sync Page", "sync", testNodeKindPtr(tree.NodeKindPage))
	if err != nil {
		t.Fatalf("CreateNode page failed: %v", err)
	}

	pageIDs := routes.pageIDsForMarkdownPaths([]string{"sync.md"})
	if len(pageIDs) != 1 || pageIDs[0] != *pageID {
		t.Fatalf("pageIDsForMarkdownPaths(sync.md) = %v, want [%s]", pageIDs, pageID.String())
	}

	sectionIDs := routes.pageIDsForMarkdownPaths([]string{"sync/index.md"})
	if len(sectionIDs) != 1 || sectionIDs[0] != *sectionID {
		t.Fatalf("pageIDsForMarkdownPaths(sync/index.md) = %v, want [%s]", sectionIDs, sectionID.String())
	}
}

func TestPageIDsForMarkdownPathsResolvesReadmeFallbackSection(t *testing.T) {
	routes := newContextToolTestRoutes(t)

	sectionID, err := routes.treeService.CreateNode("system", nil, "Guide", "guide", testNodeKindPtr(tree.NodeKindSection))
	if err != nil {
		t.Fatalf("CreateNode section failed: %v", err)
	}

	pageIDs := routes.pageIDsForMarkdownPaths([]string{"guide/README.md"})
	if len(pageIDs) != 1 || pageIDs[0] != *sectionID {
		t.Fatalf("pageIDsForMarkdownPaths(guide/README.md) = %v, want [%s]", pageIDs, sectionID.String())
	}
}

func TestPageIDsForMarkdownPathsUsesWorkspaceRouteNormalizationForReadmeSection(t *testing.T) {
	routes := newContextToolTestRoutes(t)
	workspaceRoot := t.TempDir()
	routes.workspaceRootDir = workspaceRoot
	if err := os.MkdirAll(filepath.Join(workspaceRoot, "User Guides"), 0o755); err != nil {
		t.Fatalf("create workspace section: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspaceRoot, "User Guides", "README.md"), []byte("# User Guides\n"), 0o644); err != nil {
		t.Fatalf("write workspace README: %v", err)
	}
	sectionID, err := routes.treeService.CreateNode("system", nil, "User Guides", "user-guides", testNodeKindPtr(tree.NodeKindSection))
	if err != nil {
		t.Fatalf("CreateNode section failed: %v", err)
	}

	pageIDs := routes.pageIDsForMarkdownPaths([]string{"User Guides/README.md"})
	if len(pageIDs) != 1 || pageIDs[0] != *sectionID {
		t.Fatalf("pageIDsForMarkdownPaths(User Guides/README.md) = %v, want [%s]", pageIDs, sectionID.String())
	}
}

func TestPageIDsForMarkdownPathsDoesNotFallbackLowercaseReadme(t *testing.T) {
	routes := newContextToolTestRoutes(t)

	if _, err := routes.treeService.CreateNode("system", nil, "Guide", "guide", testNodeKindPtr(tree.NodeKindSection)); err != nil {
		t.Fatalf("CreateNode section failed: %v", err)
	}

	pageIDs := routes.pageIDsForMarkdownPaths([]string{"guide/readme.md"})
	if len(pageIDs) != 0 {
		t.Fatalf("pageIDsForMarkdownPaths(guide/readme.md) = %v, want no fallback section", pageIDs)
	}
}

func TestPageIDsForMarkdownPathsUsesWorkspaceRouteNormalization(t *testing.T) {
	routes := newContextToolTestRoutes(t)
	plansID, err := routes.treeService.CreateNode("system", nil, "Plans", "plans", testNodeKindPtr(tree.NodeKindSection))
	if err != nil {
		t.Fatalf("CreateNode section failed: %v", err)
	}
	pageID, err := routes.treeService.CreateNode("system", plansID, "Agent Hooks Plan", "agent-hooks-plan", testNodeKindPtr(tree.NodeKindPage))
	if err != nil {
		t.Fatalf("CreateNode page failed: %v", err)
	}

	pageIDs := routes.pageIDsForMarkdownPaths([]string{"plans/agent_hooks.PLAN.md"})
	if len(pageIDs) != 1 || pageIDs[0] != *pageID {
		t.Fatalf("pageIDsForMarkdownPaths(plans/agent_hooks.PLAN.md) = %v, want [%s]", pageIDs, pageID.String())
	}
}

func TestRecentChangesResolveRootIndexPageID(t *testing.T) {
	routes := newContextToolTestRoutes(t)
	ctx := context.Background()
	createdAt := time.Date(2026, 6, 8, 13, 0, 0, 0, time.UTC)
	routes.listWorkspaceSnapshots = func(context.Context, workspacesync.CommitHash, int) (workspacesync.SnapshotList, error) {
		return workspacesync.SnapshotList{
			Snapshots: []workspacesync.Snapshot{{
				ID:                   "root-index-commit",
				CreatedAt:            createdAt,
				ChangedMarkdownCount: 1,
				ChangedMarkdownPaths: []string{"index.md"},
			}},
		}, nil
	}

	changes := routes.recentChanges(ctx, workspacesync.SyncStatus{}, 1)

	if len(changes) != 1 {
		t.Fatalf("recentChanges length = %d, want 1", len(changes))
	}
	if len(changes[0].PageIDs) != 1 || changes[0].PageIDs[0] != tree.RootPageID {
		t.Fatalf("recentChanges[0].PageIDs = %v, want [root]", changes[0].PageIDs)
	}
}

func TestGetContextSyncModesAndSessionHistory(t *testing.T) {
	routes := newContextToolTestRoutes(t)
	actor := toolActor{ID: "editor-1", User: &auth.User{ID: "editor-1", Username: "editor", Role: auth.RoleEditor}}
	opts := httpinternal.RouterOptions{AuthDisabled: true, EnableWorkspaceSync: true}
	ctx := context.Background()

	refreshCalls := 0
	status := workspacesync.SyncStatus{Enabled: true, WatcherRunning: true, LastCommitHash: "healthy"}
	routes.workspaceSyncStatus = func() workspacesync.SyncStatus { return status }
	routes.workspaceSyncRefresh = func(context.Context, workspacesync.SyncRequest) (workspacesync.SyncStatus, error) {
		refreshCalls++
		return status, nil
	}

	healthy, err := routes.getContext(ctx, nil, actor, opts, getContextInput{SyncMode: contextSyncModeAuto})
	if err != nil {
		t.Fatalf("healthy auto context failed: %v", err)
	}
	if refreshCalls != 0 {
		t.Fatalf("healthy auto refresh calls = %d, want 0", refreshCalls)
	}
	if healthy.ContextToken == "" {
		t.Fatalf("healthy context token is empty")
	}

	status.WatcherEnabled = false
	status.WatcherRunning = false
	if _, err := routes.getContext(ctx, nil, actor, opts, getContextInput{SyncMode: contextSyncModeAuto}); err != nil {
		t.Fatalf("manual watcher auto context failed: %v", err)
	}
	if refreshCalls != 0 {
		t.Fatalf("manual watcher auto refresh calls = %d, want 0", refreshCalls)
	}
	status.WatcherEnabled = true
	if _, err := routes.getContext(ctx, nil, actor, opts, getContextInput{SyncMode: contextSyncModeAuto}); err != nil {
		t.Fatalf("stopped watcher auto context failed: %v", err)
	}
	if refreshCalls != 1 {
		t.Fatalf("stopped watcher auto refresh calls = %d, want 1", refreshCalls)
	}
	status.WatcherRunning = true

	status.PendingEventCount = 2
	status.LastCommitHash = "pending"
	if _, err := routes.getContext(ctx, nil, actor, opts, getContextInput{SyncMode: contextSyncModeAuto}); err != nil {
		t.Fatalf("pending auto context failed: %v", err)
	}
	if refreshCalls != 2 {
		t.Fatalf("pending auto refresh calls = %d, want 2", refreshCalls)
	}

	status.PendingEventCount = 0
	status.LastError = "previous sync failed"
	status.LastCommitHash = "errored"
	routes.workspaceSyncRefresh = func(context.Context, workspacesync.SyncRequest) (workspacesync.SyncStatus, error) {
		refreshCalls++
		return status, errors.New("sync still failed")
	}
	errored, err := routes.getContext(ctx, nil, actor, opts, getContextInput{SyncMode: contextSyncModeAuto})
	if err != nil {
		t.Fatalf("errored auto context failed: %v", err)
	}
	if refreshCalls != 3 {
		t.Fatalf("errored auto refresh calls = %d, want 3", refreshCalls)
	}
	erroredStatus, ok := errored.SyncStatus.(map[string]any)
	if !ok || erroredStatus["lastError"] != "sync still failed" {
		t.Fatalf("errored sync status = %#v, want surfaced refresh error", errored.SyncStatus)
	}

	routes.workspaceSyncRefresh = func(context.Context, workspacesync.SyncRequest) (workspacesync.SyncStatus, error) {
		refreshCalls++
		return status, nil
	}
	status.LastError = ""
	status.LastCommitHash = "force"
	if _, err := routes.getContext(ctx, nil, actor, opts, getContextInput{SyncMode: contextSyncModeForce}); err != nil {
		t.Fatalf("force context failed: %v", err)
	}
	if refreshCalls != 4 {
		t.Fatalf("force refresh calls = %d, want 4", refreshCalls)
	}

	status.LastError = "reported without refresh"
	status.LastCommitHash = "none"
	none, err := routes.getContext(ctx, nil, actor, opts, getContextInput{SyncMode: contextSyncModeNone})
	if err != nil {
		t.Fatalf("none context failed: %v", err)
	}
	if refreshCalls != 4 {
		t.Fatalf("none refresh calls = %d, want still 4", refreshCalls)
	}
	noneStatus, ok := none.SyncStatus.(map[string]any)
	if !ok || noneStatus["lastError"] != "reported without refresh" {
		t.Fatalf("none sync status = %#v, want existing last error", none.SyncStatus)
	}

	missing, err := routes.getContext(ctx, nil, actor, opts, getContextInput{SinceToken: "missing", SyncMode: contextSyncModeNone})
	if err != nil {
		t.Fatalf("missing-token context failed: %v", err)
	}
	if len(missing.Warnings) == 0 || missing.Warnings[0] != "unknown sinceToken; returned current context" {
		t.Fatalf("missing token warnings = %#v, want unknown-token warning", missing.Warnings)
	}
	if missing.PreviousContextToken != "" {
		t.Fatalf("missing token previousContextToken = %q, want no implicit fallback", missing.PreviousContextToken)
	}
	if len(missing.ChangesSincePreviousContext) != 0 {
		t.Fatalf("missing token changesSincePreviousContext = %#v, want no implicit fallback delta", missing.ChangesSincePreviousContext)
	}

	var latest contextOutput
	for i := 0; i < 12; i++ {
		latest, err = routes.getContext(ctx, nil, actor, opts, getContextInput{SyncMode: contextSyncModeNone})
		if err != nil {
			t.Fatalf("history context %d failed: %v", i, err)
		}
	}
	if len(latest.ContextHistory) != 10 {
		t.Fatalf("context history len = %d, want 10", len(latest.ContextHistory))
	}

	otherActor := toolActor{ID: "editor-2", User: &auth.User{ID: "editor-2", Username: "other", Role: auth.RoleEditor}}
	other, err := routes.getContext(ctx, nil, otherActor, opts, getContextInput{SyncMode: contextSyncModeNone})
	if err != nil {
		t.Fatalf("other context failed: %v", err)
	}
	if len(other.ContextHistory) != 1 {
		t.Fatalf("other context history len = %d, want isolated first checkpoint", len(other.ContextHistory))
	}
}

func TestContextInputLimitsAreClamped(t *testing.T) {
	huge := 999
	negative := -1

	if got := boundedContextTreeDepth(nil); got != defaultContextTreeDepth {
		t.Fatalf("boundedContextTreeDepth(nil) = %d, want %d", got, defaultContextTreeDepth)
	}
	if got := boundedContextTreeDepth(&huge); got != maxContextTreeDepth {
		t.Fatalf("boundedContextTreeDepth(999) = %d, want %d", got, maxContextTreeDepth)
	}
	if got := boundedContextTreeDepth(&negative); got != defaultContextTreeDepth {
		t.Fatalf("boundedContextTreeDepth(-1) = %d, want %d", got, defaultContextTreeDepth)
	}
	if got := boundedRecentChangesLimit(nil); got != defaultContextRecentChangesLimit {
		t.Fatalf("boundedRecentChangesLimit(nil) = %d, want %d", got, defaultContextRecentChangesLimit)
	}
	if got := boundedRecentChangesLimit(&huge); got != maxContextRecentChangesLimit {
		t.Fatalf("boundedRecentChangesLimit(999) = %d, want %d", got, maxContextRecentChangesLimit)
	}
	if got := boundedRecentChangesLimit(&negative); got != defaultContextRecentChangesLimit {
		t.Fatalf("boundedRecentChangesLimit(-1) = %d, want %d", got, defaultContextRecentChangesLimit)
	}
}

func TestGetContextRedactsSyncStatusLastErrorPaths(t *testing.T) {
	routes := newContextToolTestRoutes(t)
	rootDir := filepath.Join(t.TempDir(), "content")
	dataDir := filepath.Join(t.TempDir(), "data")
	routes.workspaceRootDir = rootDir
	routes.workspaceDataDir = dataDir
	routes.workspaceSyncStatus = func() workspacesync.SyncStatus {
		return workspacesync.SyncStatus{
			Enabled:        true,
			WatcherRunning: true,
			LastCommitHash: "redact",
			LastError: fmt.Sprintf(
				"open %s: permission denied; stat %s: no such file",
				filepath.Join(rootDir, "docs", "api.md"),
				filepath.Join(dataDir, ".leafwiki", "git", "HEAD"),
			),
			ValidationErrors: []workspacesync.ValidationError{
				{
					Path:     filepath.Join(rootDir, "docs", "bad.md"),
					Message:  "validate " + filepath.Join(dataDir, ".leafwiki", "work", "bad.md") + ": failed",
					Severity: "error",
				},
			},
		}
	}
	actor := toolActor{ID: "editor-1", User: &auth.User{ID: "editor-1", Username: "editor", Role: auth.RoleEditor}}

	out, err := routes.getContext(context.Background(), nil, actor, httpinternal.RouterOptions{EnableWorkspaceSync: true}, getContextInput{SyncMode: contextSyncModeNone})
	if err != nil {
		t.Fatalf("getContext failed: %v", err)
	}
	status, ok := out.SyncStatus.(map[string]any)
	if !ok {
		t.Fatalf("SyncStatus has type %T, want map", out.SyncStatus)
	}
	lastError, ok := status["lastError"].(string)
	if !ok {
		t.Fatalf("lastError has type %T, want string", status["lastError"])
	}
	if strings.Contains(lastError, rootDir) || strings.Contains(lastError, dataDir) {
		t.Fatalf("lastError = %q, want root/data paths redacted", lastError)
	}
	for _, want := range []string{"<root-dir>/docs/api.md", "<data-dir>/.leafwiki/git/HEAD", "permission denied"} {
		if !strings.Contains(lastError, want) {
			t.Fatalf("lastError = %q, want substring %q", lastError, want)
		}
	}
	validationErrors, ok := status["validationErrors"].([]workspacesync.ValidationError)
	if !ok || len(validationErrors) != 1 {
		t.Fatalf("validationErrors = %#v, want one redacted validation error", status["validationErrors"])
	}
	for _, got := range []string{validationErrors[0].Path, validationErrors[0].Message} {
		if strings.Contains(got, rootDir) || strings.Contains(got, dataDir) {
			t.Fatalf("syncStatus validation error field = %q, want root/data paths redacted", got)
		}
	}
	if validationErrors[0].Path != "<root-dir>/docs/bad.md" || !strings.Contains(validationErrors[0].Message, "<data-dir>/.leafwiki/work/bad.md") {
		t.Fatalf("validationErrors = %#v, want redacted path and message", validationErrors)
	}
	if len(out.Validation.Issues) != 1 {
		t.Fatalf("validation issues = %#v, want one issue", out.Validation.Issues)
	}
	issue := out.Validation.Issues[0]
	for _, got := range []string{issue.Path, issue.Message} {
		if strings.Contains(got, rootDir) || strings.Contains(got, dataDir) {
			t.Fatalf("validation issue field = %q, want root/data paths redacted", got)
		}
	}
	if issue.Path != "<root-dir>/docs/bad.md" || !strings.Contains(issue.Message, "<data-dir>/.leafwiki/work/bad.md") {
		t.Fatalf("validation issues = %#v, want redacted path and message", out.Validation.Issues)
	}
}

func TestContextCheckpointStoreEvictsOldSessions(t *testing.T) {
	store := newContextCheckpointStore(2)
	base := time.Date(2026, 6, 8, 12, 0, 0, 0, time.UTC)

	for i := 0; i < defaultContextCheckpointSessions+4; i++ {
		store.record(fmt.Sprintf("session-%03d", i), contextCheckpoint{CreatedAt: base.Add(time.Duration(i) * time.Second)})
	}

	store.mu.Lock()
	got := len(store.sessions)
	_, firstExists := store.sessions["session-000"]
	_, newestExists := store.sessions[fmt.Sprintf("session-%03d", defaultContextCheckpointSessions+3)]
	store.mu.Unlock()

	if got != defaultContextCheckpointSessions {
		t.Fatalf("session count = %d, want %d", got, defaultContextCheckpointSessions)
	}
	if firstExists {
		t.Fatalf("oldest session was not evicted")
	}
	if !newestExists {
		t.Fatalf("newest session was evicted")
	}
}

func TestContextCheckpointStorePrunesExpiredSessions(t *testing.T) {
	store := newContextCheckpointStore(2)
	store.ttl = time.Minute
	base := time.Date(2026, 6, 8, 12, 0, 0, 0, time.UTC)

	store.record("expired", contextCheckpoint{CreatedAt: base})
	store.record("fresh", contextCheckpoint{CreatedAt: base.Add(30 * time.Second)})
	store.record("current", contextCheckpoint{CreatedAt: base.Add(2 * time.Minute)})

	store.mu.Lock()
	_, expiredExists := store.sessions["expired"]
	_, freshExists := store.sessions["fresh"]
	_, currentExists := store.sessions["current"]
	store.mu.Unlock()

	if expiredExists || freshExists {
		t.Fatalf("expired sessions remain: expired=%v fresh=%v", expiredExists, freshExists)
	}
	if !currentExists {
		t.Fatalf("current session was pruned")
	}
}

func TestContextCheckpointStoreExpiresCurrentSessionTokens(t *testing.T) {
	store := newContextCheckpointStore(2)
	freshTime := time.Now().UTC()
	expiredTime := freshTime.Add(-2 * time.Minute)

	store.ttl = 0
	_, history := store.record("current", contextCheckpoint{CreatedAt: expiredTime})
	expiredToken := history[0].Token
	_, history = store.record("current", contextCheckpoint{CreatedAt: freshTime})
	freshToken := history[len(history)-1].Token
	store.ttl = time.Minute

	if checkpoint, ok := store.find("current", expiredToken); ok {
		t.Fatalf("expired current-session checkpoint = %#v, want token pruned", checkpoint)
	}
	if _, ok := store.find("current", freshToken); !ok {
		t.Fatalf("fresh current-session checkpoint %q was pruned", freshToken)
	}
}

func TestValidationFromSyncStatusSeparatesWarningsFromErrors(t *testing.T) {
	validation := validationFromSyncStatus(workspacesync.SyncStatus{
		ValidationErrors: []workspacesync.ValidationError{
			{Path: "hidden.md", Message: "hidden markdown file", Severity: "warning"},
		},
	})

	if !validation.OK {
		t.Fatalf("validation.OK = false, want warnings-only status to be OK")
	}
	if validation.Summary.Errors != 0 || validation.Summary.Warnings != 1 {
		t.Fatalf("validation summary = %#v, want 0 errors and 1 warning", validation.Summary)
	}
}

func newContextToolTestRoutes(t *testing.T) *Routes {
	t.Helper()

	treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{
		DataDir: t.TempDir(),
		RootDir: t.TempDir(),
	})
	if err := treeService.LoadTree(); err != nil {
		t.Fatalf("LoadTree failed: %v", err)
	}
	if id, err := treeService.CreateNode("system", nil, "Home", "home", nil); err != nil || id == nil {
		t.Fatalf("CreateNode failed: id=%v err=%v", id, err)
	}
	now := time.Date(2026, 6, 8, 12, 0, 0, 0, time.UTC)
	return &Routes{
		treeService:      treeService,
		contextStore:     newContextCheckpointStore(10),
		userResolver:     nil,
		getAssets:        nil,
		workspaceRootDir: t.TempDir(),
		workspaceSyncStatus: func() workspacesync.SyncStatus {
			return workspacesync.SyncStatus{Enabled: true, WatcherRunning: true, LastSyncTime: now}
		},
	}
}
