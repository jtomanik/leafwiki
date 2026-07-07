package mcp

import (
	"path/filepath"
	"strconv"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"

	coreauth "github.com/perber/wiki/internal/core/auth"
	wikivalidation "github.com/perber/wiki/internal/core/markdownvalidation"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	httpinternal "github.com/perber/wiki/internal/http"
	"github.com/perber/wiki/internal/workspacesync"
)

var _ = ginkgo.Describe("MCP context recommendations", ginkgo.Label("unit"), func() {
	ginkgo.It("recommends read repair and editor tools from validation and workspace state", func() {
		validation := validationOutput{Summary: validationSummaryOutput{Errors: 1}}

		Expect(recommendedToolsForContext(nil, validation, true)).To(matchRecommendedTools(
			ToolValidateWiki,
			ToolGetSubtree,
			ToolSearchPages,
			ToolGetPage,
			ToolGetPageByPath,
		))

		editor := &coreauth.User{ID: newFixtureUserID("editor"), Username: "editor", Role: coreauth.RoleEditor}
		Expect(recommendedToolsForContext(editor, validationOutput{}, true)).To(matchRecommendedTools(
			ToolGetSubtree,
			ToolSearchPages,
			ToolGetPage,
			ToolGetPageByPath,
			ToolUpdatePage,
			ToolCreatePage,
			ToolRefresh,
		))

		viewer := &coreauth.User{ID: newFixtureUserID("viewer"), Username: "viewer", Role: coreauth.RoleViewer}
		Expect(recommendedToolsForContext(viewer, validationOutput{}, true)).To(matchRecommendedTools(
			ToolGetSubtree,
			ToolSearchPages,
			ToolGetPage,
			ToolGetPageByPath,
		))
	})

	ginkgo.It("publishes feature-gated tool names from router options", func() {
		Expect(serverToolNamesForOptions(httpinternal.RouterOptions{
			EnableWorkspaceSync: true,
			EnableLinkRefactor:  true,
		})).To(includeContextToolNames(
			ToolRefresh,
			ToolPreviewRefactor,
			ToolApplyRefactor,
		))
	})

	ginkgo.It("decides context refresh access from actor role and workspace sync state", func() {
		Expect((*coreauth.User)(nil)).To(matchContextRefreshPermission(contextRefreshDenied))
		Expect(&coreauth.User{Role: coreauth.RoleViewer}).To(matchContextRefreshPermission(contextRefreshDenied))
		Expect(&coreauth.User{Role: coreauth.RoleEditor}).To(matchContextRefreshPermission(contextRefreshAllowed))
		Expect(&coreauth.User{Role: coreauth.RoleAdmin}).To(matchContextRefreshPermission(contextRefreshAllowed))

		Expect(contextRefreshScenario{Mode: contextSyncModeForce}).To(matchContextRefreshDecision(contextRefreshRequested))
		Expect(contextRefreshScenario{Mode: contextSyncModeNone, Sync: contextRefreshSyncPendingEvents}).To(matchContextRefreshDecision(contextRefreshSkipped))
		Expect(contextRefreshScenario{Mode: contextSyncModeAuto}).To(matchContextRefreshDecision(contextRefreshSkipped))
		Expect(contextRefreshScenario{Mode: contextSyncModeAuto, Sync: contextRefreshSyncPendingEvents}).To(matchContextRefreshDecision(contextRefreshRequested))
		Expect(contextRefreshScenario{Mode: contextSyncModeAuto, Sync: contextRefreshSyncReportedError}).To(matchContextRefreshDecision(contextRefreshRequested))
		Expect(contextRefreshScenario{Mode: contextSyncModeAuto, Sync: contextRefreshSyncStoppedWatcher}).To(matchContextRefreshDecision(contextRefreshRequested))
	})

	ginkgo.It("redacts workspace paths in sync status and validation details", func() {
		rootDir := filepath.Join("workspace", "root")
		dataDir := filepath.Join(rootDir, ".leafwiki")
		routes := &Routes{
			workspaceRootDir: rootDir,
			workspaceDataDir: dataDir,
		}
		status := workspacesync.SyncStatus{
			Enabled:                    true,
			WatcherEnabled:             true,
			WatcherRunning:             true,
			PendingEventCount:          2,
			LastError:                  workspaceSyncErrorDetailFromRuntime(filepath.Join(dataDir, "sync.db") + " failed").String(),
			LastCommitHash:             newFixtureCommitHash("commit-1"),
			RecentChangedMarkdownPaths: []string{"docs/page.md"},
			ValidationErrors: []workspacesync.ValidationError{{
				Path:      filepath.Join(rootDir, "docs/page.md"),
				Message:   filepath.Join(dataDir, "state.db"),
				MessageID: sharederrors.MessageIDForCode(errCodeMCPWorkspaceSyncFailed),
				Severity:  wikivalidation.IssueSeverityError,
			}},
		}

		Expect(routes.syncStatusOutput(status)).To(matchContextSyncStatus(contextSyncStatusContract{
			Enabled:              syncStatusEnabled,
			LastError:            syncStatusErrorReported,
			RecentChangedPaths:   []string{"docs/page.md"},
			ValidationErrorPaths: []string{"<root-dir>/docs/page.md"},
			ValidationMessages:   []string{"<data-dir>/state.db"},
		}))
		Expect(mcpWorkspaceSyncLastErrorDetail(blankWorkspaceSyncErrorDetail.String())).To(BeNil())
	})

	ginkgo.It("preserves actor identity for authenticated users and public editors", func() {
		Expect(workspaceActorForToolActor(toolActor{})).To(matchWorkspaceActor(workspacesync.PublicEditorActor()))
		Expect(workspaceActorForToolActor(toolActor{
			ID:   newFixtureUserID("editor"),
			User: &coreauth.User{ID: newFixtureUserID("editor"), Username: "editor", Email: "editor@example.com"},
		})).To(matchWorkspaceActor(workspacesync.Actor{
			ID:    workspacesync.ActorIDFromUserID(newFixtureUserID("editor")),
			Name:  "editor",
			Email: "editor@example.com",
		}))
	})
})

var _ = ginkgo.Describe("MCP context output normalization", ginkgo.Label("unit"), func() {
	ginkgo.It("bounds context tree depth and recent-change windows", func() {
		negativeDepth := -1
		tooDeep := maxContextTreeDepth + 3
		negativeLimit := -1
		tooManyChanges := maxContextRecentChangesLimit + 3

		Expect((*int)(nil)).To(matchBoundedContextTreeDepth(defaultContextTreeDepth, defaultContextTreeDepth-1))
		Expect(&negativeDepth).To(matchBoundedContextTreeDepth(defaultContextTreeDepth, defaultContextTreeDepth-1))
		Expect(&tooDeep).To(matchBoundedContextTreeDepth(maxContextTreeDepth, maxContextTreeDepth-1))
		Expect(treeDisplayDepth(0)).To(matchChildContextTreeDepth(-1))
		Expect(treeDisplayDepth(-1)).To(matchChildContextTreeDepth(-1))

		Expect((*int)(nil)).To(matchBoundedRecentChangesLimit(defaultContextRecentChangesLimit))
		Expect(&negativeLimit).To(matchBoundedRecentChangesLimit(defaultContextRecentChangesLimit))
		Expect(&tooManyChanges).To(matchBoundedRecentChangesLimit(maxContextRecentChangesLimit))

		Expect(49).To(matchContextSnapshotPageSize(49))
		Expect(50).To(matchContextSnapshotPageSize(50))
		Expect(250).To(matchContextSnapshotPageSize(50))
	})

	ginkgo.It("caps recent-change paths without sharing caller storage", func() {
		paths := makeContextChangedPaths(maxRecentChangePaths + 1)

		capped := cappedChangedPaths(paths)
		paths[0] = filepath.Join("docs", "mutated.md")

		Expect(capped).To(matchContextChangedPathWindow(contextChangedPathWindow{
			Count: maxRecentChangePaths,
			First: filepath.Join("docs", "page-0.md"),
			Last:  filepath.Join("docs", "page-"+strconv.Itoa(maxRecentChangePaths-1)+".md"),
		}))
	})

	ginkgo.It("redacts workspace path variants and renders UTC timestamps", func() {
		rootDir := filepath.Join("workspace", "root")
		joinedPath := filepath.Join(rootDir, "docs", "guide.md")
		slashedPath := filepath.ToSlash(joinedPath)
		amsterdam := time.FixedZone("CET", 60*60)

		Expect(redactWorkspacePath(joinedPath, rootDir, "<root-dir>")).To(Equal(filepath.Join("<root-dir>", "docs", "guide.md")))
		Expect(redactWorkspacePath(slashedPath, rootDir, "<root-dir>")).To(Equal("<root-dir>/docs/guide.md"))
		Expect(redactWorkspacePath(joinedPath, "", "<root-dir>")).To(Equal(joinedPath))
		Expect(redactWorkspacePath(joinedPath, string(filepath.Separator), "<root-dir>")).To(Equal(joinedPath))
		Expect(formatContextTime(time.Time{})).To(BeEmpty())
		Expect(formatContextTime(time.Date(2026, time.July, 7, 12, 30, 0, 0, amsterdam))).To(Equal("2026-07-07T11:30:00Z"))
	})
})

type contextRecommendationContract struct {
	Tools []ToolID
}

type contextTreeDepthContract struct {
	TreeDepth  treeDisplayDepth
	ChildDepth treeDisplayDepth
}

type contextRefreshPermission uint8

const (
	contextRefreshDenied contextRefreshPermission = iota
	contextRefreshAllowed
)

type contextRefreshSyncSignal uint8

const (
	contextRefreshSyncDisabled contextRefreshSyncSignal = iota
	contextRefreshSyncPendingEvents
	contextRefreshSyncReportedError
	contextRefreshSyncStoppedWatcher
)

type contextRefreshScenario struct {
	Mode string
	Sync contextRefreshSyncSignal
}

func matchRecommendedTools(tools ...ToolID) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(func(got []ToolID) contextRecommendationContract {
		return contextRecommendationContract{Tools: got}
	}, Equal(contextRecommendationContract{Tools: tools}))
}

func matchContextRefreshPermission(want contextRefreshPermission) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(contextRefreshPermissionFor, Equal(want))
}

func matchContextRefreshDecision(want contextRefreshDecision) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(contextRefreshDecisionForScenario, Equal(want))
}

func matchBoundedContextTreeDepth(depth int, childDepth int) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(func(raw *int) contextTreeDepthContract {
		bounded := boundedContextTreeDepth(raw)
		return contextTreeDepthContract{
			TreeDepth:  bounded,
			ChildDepth: bounded.ChildDepth(),
		}
	}, Equal(contextTreeDepthContract{TreeDepth: treeDisplayDepth(depth), ChildDepth: treeDisplayDepth(childDepth)}))
}

func matchChildContextTreeDepth(childDepth int) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(func(depth treeDisplayDepth) int {
		return depth.ChildDepth().Int()
	}, Equal(childDepth))
}

func matchBoundedRecentChangesLimit(limit int) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(boundedRecentChangesLimit, Equal(limit))
}

func matchContextSnapshotPageSize(pageSize int) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(contextSnapshotPageSize, Equal(pageSize))
}

func contextRefreshPermissionFor(user *coreauth.User) contextRefreshPermission {
	if canRefreshContext(user) {
		return contextRefreshAllowed
	}
	return contextRefreshDenied
}

func contextRefreshDecisionForScenario(scenario contextRefreshScenario) contextRefreshDecision {
	return contextRefreshDecisionFor(scenario.Mode, contextRefreshSyncStatusFor(scenario.Sync))
}

func contextRefreshSyncStatusFor(signal contextRefreshSyncSignal) workspacesync.SyncStatus {
	switch signal {
	case contextRefreshSyncPendingEvents:
		return workspacesync.SyncStatus{
			Enabled:           true,
			PendingEventCount: 1,
		}
	case contextRefreshSyncReportedError:
		return workspacesync.SyncStatus{
			Enabled:   true,
			LastError: workspaceSyncFailedErrorDetail.String(),
		}
	case contextRefreshSyncStoppedWatcher:
		return workspacesync.SyncStatus{
			Enabled:        true,
			WatcherEnabled: true,
		}
	default:
		return workspacesync.SyncStatus{}
	}
}

func includeContextToolNames(tools ...ToolID) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.ProtocolName().WireName())
	}
	return ContainElements(names)
}

type syncStatusState uint8

const (
	syncStatusDisabled syncStatusState = iota
	syncStatusEnabled
)

type syncStatusErrorState uint8

const (
	syncStatusErrorAbsent syncStatusErrorState = iota
	syncStatusErrorReported
)

type workspaceSyncErrorDetail string

const (
	blankWorkspaceSyncErrorDetail  workspaceSyncErrorDetail = " "
	workspaceSyncFailedErrorDetail workspaceSyncErrorDetail = "workspace_sync_failed"
)

func (detail workspaceSyncErrorDetail) String() string {
	return string(detail)
}

func workspaceSyncErrorDetailFromRuntime(raw string) workspaceSyncErrorDetail {
	return workspaceSyncErrorDetail(raw)
}

type contextSyncStatusContract struct {
	Enabled              syncStatusState
	LastError            syncStatusErrorState
	RecentChangedPaths   []string
	ValidationErrorPaths []string
	ValidationMessages   []string
}

func matchContextSyncStatus(want contextSyncStatusContract) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(contextSyncStatusContractFor, Equal(want))
}

func contextSyncStatusContractFor(output map[string]any) contextSyncStatusContract {
	validationErrors, _ := output["validationErrorDetails"].([]workspacesync.ValidationError)
	errorPaths := make([]string, 0, len(validationErrors))
	errorMessages := make([]string, 0, len(validationErrors))
	for _, validationError := range validationErrors {
		errorPaths = append(errorPaths, validationError.Path)
		errorMessages = append(errorMessages, validationError.Message)
	}
	return contextSyncStatusContract{
		Enabled:              syncStatusStateFor(output["enabled"] == true),
		LastError:            syncStatusErrorStateFor(output["lastErrorDetail"] != nil),
		RecentChangedPaths:   stringSliceFromAny(output["recentChangedMarkdownPaths"]),
		ValidationErrorPaths: errorPaths,
		ValidationMessages:   errorMessages,
	}
}

func stringSliceFromAny(value any) []string {
	values, _ := value.([]string)
	return values
}

func syncStatusStateFor(enabled bool) syncStatusState {
	if enabled {
		return syncStatusEnabled
	}
	return syncStatusDisabled
}

func syncStatusErrorStateFor(reported bool) syncStatusErrorState {
	if reported {
		return syncStatusErrorReported
	}
	return syncStatusErrorAbsent
}

type contextChangedPathWindow struct {
	Count int
	First string
	Last  string
}

func matchContextChangedPathWindow(want contextChangedPathWindow) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(contextChangedPathWindowFor, Equal(want))
}

func contextChangedPathWindowFor(paths []string) contextChangedPathWindow {
	window := contextChangedPathWindow{Count: len(paths)}
	if len(paths) == 0 {
		return window
	}
	window.First = paths[0]
	window.Last = paths[len(paths)-1]
	return window
}

func makeContextChangedPaths(count int) []string {
	paths := make([]string, 0, count)
	for i := 0; i < count; i++ {
		paths = append(paths, filepath.Join("docs", "page-"+strconv.Itoa(i)+".md"))
	}
	return paths
}

func matchWorkspaceActor(want workspacesync.Actor) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return Equal(want)
}
