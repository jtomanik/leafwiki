package workspacesync

import (
	"reflect"

	"github.com/onsi/gomega/gcustom"
	"github.com/onsi/gomega/types"
	"github.com/perber/wiki/internal/core/tree"
)

func newFixtureCommitHash[T ~string](raw T) CommitHash {
	return NewCommitHashUnchecked(string(raw))
}

func newFixturePageID[T ~string](raw T) tree.PageID {
	return tree.NewPageIDUnchecked(raw)
}

func matchEnabledWorkspaceSyncStatus() types.GomegaMatcher {
	return gcustom.MakeMatcher(func(status SyncStatus) (bool, error) {
		return status.Enabled, nil
	}).WithMessage("report enabled workspace sync status")
}

func matchWatcherFactoryFailureStatus() types.GomegaMatcher {
	return gcustom.MakeMatcher(func(status SyncStatus) (bool, error) {
		return status.WatcherEnabled &&
			!status.WatcherRunning, nil
	}).WithMessage("report watcher factory failure status")
}

func matchRunningWatcherStatus() types.GomegaMatcher {
	return gcustom.MakeMatcher(func(status SyncStatus) (bool, error) {
		return status.WatcherEnabled && status.WatcherRunning, nil
	}).WithMessage("report running workspace watcher")
}

func matchRunningWatcherWithRecentMarkdownPaths(paths ...string) types.GomegaMatcher {
	return gcustom.MakeMatcher(func(status SyncStatus) (bool, error) {
		return status.WatcherEnabled &&
			status.WatcherRunning &&
			status.PendingEventCount == 0 &&
			reflect.DeepEqual(status.RecentChangedMarkdownPaths, paths), nil
	}).WithMessage("report running watcher after syncing markdown events")
}

func matchStoppedWatcherStatus() types.GomegaMatcher {
	return gcustom.MakeMatcher(func(status SyncStatus) (bool, error) {
		return status.WatcherEnabled && !status.WatcherRunning, nil
	}).WithMessage("report stopped workspace watcher")
}
