package workspacesync

import (
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"
	"github.com/perber/wiki/internal/core/tree"
)

func newFixturePageID[T ~string](raw T) tree.PageID {
	return tree.NewPageIDUnchecked(raw)
}

type workspaceSyncMode string

const (
	workspaceSyncModeDisabled workspaceSyncMode = "disabled"
	workspaceSyncModeEnabled  workspaceSyncMode = "enabled"
)

type workspaceWatcherOutcome string

const (
	workspaceWatcherUnavailable workspaceWatcherOutcome = "unavailable"
	workspaceWatcherRunning     workspaceWatcherOutcome = "running"
	workspaceWatcherStopped     workspaceWatcherOutcome = "stopped"
)

type workspaceSyncStatusObservation struct {
	Mode                       workspaceSyncMode
	Watcher                    workspaceWatcherOutcome
	PendingEventCount          int
	LastError                  string
	RecentChangedMarkdownPaths []string
}

func observeWorkspaceSyncStatus(status SyncStatus) workspaceSyncStatusObservation {
	mode := workspaceSyncModeDisabled
	if status.Enabled {
		mode = workspaceSyncModeEnabled
	}

	watcher := workspaceWatcherUnavailable
	if status.WatcherEnabled {
		watcher = workspaceWatcherStopped
		if status.WatcherRunning {
			watcher = workspaceWatcherRunning
		}
	}

	return workspaceSyncStatusObservation{
		Mode:                       mode,
		Watcher:                    watcher,
		PendingEventCount:          status.PendingEventCount,
		LastError:                  status.LastError,
		RecentChangedMarkdownPaths: status.RecentChangedMarkdownPaths,
	}
}

func matchEnabledWorkspaceSyncStatus() types.GomegaMatcher {
	return WithTransform(observeWorkspaceSyncStatus, HaveField("Mode", Equal(workspaceSyncModeEnabled)))
}

func matchDisabledWorkspaceSyncStatus() types.GomegaMatcher {
	return WithTransform(observeWorkspaceSyncStatus, HaveField("Mode", Equal(workspaceSyncModeDisabled)))
}

func matchWatcherFactoryFailureStatus() types.GomegaMatcher {
	return WithTransform(observeWorkspaceSyncStatus, SatisfyAll(
		HaveField("Mode", Equal(workspaceSyncModeEnabled)),
		HaveField("Watcher", Equal(workspaceWatcherStopped)),
		HaveField("LastError", Equal(errWatcherFactoryFailed.Error())),
	))
}

func matchRunningWatcherStatus() types.GomegaMatcher {
	return WithTransform(observeWorkspaceSyncStatus, HaveField("Watcher", Equal(workspaceWatcherRunning)))
}

func matchRunningWatcherWithRecentMarkdownPaths(paths ...string) types.GomegaMatcher {
	return WithTransform(observeWorkspaceSyncStatus, SatisfyAll(
		HaveField("Watcher", Equal(workspaceWatcherRunning)),
		HaveField("PendingEventCount", BeZero()),
		HaveField("RecentChangedMarkdownPaths", Equal(paths)),
	))
}

func matchStoppedWatcherStatus() types.GomegaMatcher {
	return WithTransform(observeWorkspaceSyncStatus, HaveField("Watcher", Equal(workspaceWatcherStopped)))
}
