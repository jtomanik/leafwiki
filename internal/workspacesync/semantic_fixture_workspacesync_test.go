package workspacesync

import (
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"
	"github.com/perber/wiki/internal/core/markdown"
	wikivalidation "github.com/perber/wiki/internal/core/markdownvalidation"
	"github.com/perber/wiki/internal/core/revision"
	"github.com/perber/wiki/internal/core/tree"
)

func newFixturePageID[T ~string](raw T) tree.PageID {
	return tree.NewPageIDUnchecked(raw)
}

func newFixtureSlug[T ~string](raw T) tree.Slug {
	return tree.NewSlugUnchecked(raw)
}

func newFixtureRoutePath[T ~string](raw T) tree.RoutePath {
	return tree.NewRoutePathUnchecked(string(raw))
}

func newFixtureCommitHash[T ~string](raw T) CommitHash {
	return NewCommitHashUnchecked(string(raw))
}

func newFixtureRevisionID[T ~string](raw T) revision.RevisionID {
	return revision.NewRevisionIDUnchecked(string(raw))
}

func newFixtureUserID[T ~string](raw T) tree.UserID {
	return tree.NewUserIDUnchecked(string(raw))
}

func newFixtureActorID[T ~string](raw T) ActorID {
	return ActorIDFromUserID(newFixtureUserID(raw))
}

func newFixtureValidationIssueCode[T ~string](raw T) wikivalidation.IssueCode {
	return wikivalidation.IssueCode(raw)
}

type workspaceSyncStartupLogEvent uint8

const (
	workspaceSyncStartupFailed workspaceSyncStartupLogEvent = iota
	workspaceSyncStartupPhaseFailed
)

func reportWorkspaceSyncStartupLogEvents(events ...workspaceSyncStartupLogEvent) types.GomegaMatcher {
	return WithTransform(func(records []workspaceSyncStartupLogRecord) []workspaceSyncStartupLogEvent {
		observed := make([]workspaceSyncStartupLogEvent, 0, len(records))
		for _, record := range records {
			switch record.Message {
			case "workspace sync startup failed":
				observed = append(observed, workspaceSyncStartupFailed)
			case "workspace sync startup phase failed":
				observed = append(observed, workspaceSyncStartupPhaseFailed)
			}
		}
		return observed
	}, ContainElements(events))
}

type workspaceSyncLastErrorState uint8

const (
	workspaceSyncLastErrorDifferent workspaceSyncLastErrorState = iota
	workspaceSyncLastErrorMatches
)

func matchWorkspaceSyncLastError(expected string) types.GomegaMatcher {
	return WithTransform(func(status SyncStatus) workspaceSyncLastErrorState {
		if status.LastError == expected {
			return workspaceSyncLastErrorMatches
		}
		return workspaceSyncLastErrorDifferent
	}, Equal(workspaceSyncLastErrorMatches))
}

func reportWorkspaceSyncLastErrorPath(rootDir string, sourcePath string) types.GomegaMatcher {
	expectedPath := normalizeValidationPath(rootDir, sourcePath)
	return WithTransform(func(status SyncStatus) []string {
		return markdownPathsInError(rootDir, status.LastError)
	}, ContainElement(expectedPath))
}

type workspaceSyncValidationActionability uint8

const (
	workspaceSyncValidationIgnored workspaceSyncValidationActionability = iota
	workspaceSyncValidationActionable
)

func reportWorkspaceSyncValidationActionability(actionability workspaceSyncValidationActionability) types.GomegaMatcher {
	return WithTransform(func(errs []ValidationError) workspaceSyncValidationActionability {
		if validationErrorsIncludeActionableMarkdownCode(errs) {
			return workspaceSyncValidationActionable
		}
		return workspaceSyncValidationIgnored
	}, Equal(actionability))
}

type workspaceSyncParsedPageMetadata struct {
	ID           tree.PageID
	Title        string
	CreatedAt    string
	CreatorID    tree.UserID
	LastAuthorID tree.UserID
}

func observeWorkspaceSyncParsedPageMetadata(page markdown.PageMetadataPage) workspaceSyncParsedPageMetadata {
	return workspaceSyncParsedPageMetadata{
		ID:           tree.PageIDFromString(page.ID),
		Title:        page.Title,
		CreatedAt:    page.CreatedAt,
		CreatorID:    tree.UserIDFromString(page.CreatorID),
		LastAuthorID: tree.UserIDFromString(page.LastAuthorID),
	}
}

func matchWorkspaceSyncParsedPageMetadata(
	id tree.PageID,
	title string,
	createdAt string,
	creatorID tree.UserID,
	lastAuthorID tree.UserID,
) types.GomegaMatcher {
	return WithTransform(observeWorkspaceSyncParsedPageMetadata, SatisfyAll(
		HaveField("ID", Equal(id)),
		HaveField("Title", Equal(title)),
		HaveField("CreatedAt", Equal(createdAt)),
		HaveField("CreatorID", Equal(creatorID)),
		HaveField("LastAuthorID", Equal(lastAuthorID)),
	))
}

type workspaceSyncRevisionAuthorObservation struct {
	RevisionAuthorID tree.UserID
	PageCreatorID    tree.UserID
	PageLastAuthorID tree.UserID
}

func observeWorkspaceSyncRevisionAuthors(rev *revision.Revision) workspaceSyncRevisionAuthorObservation {
	if rev == nil {
		return workspaceSyncRevisionAuthorObservation{}
	}
	return workspaceSyncRevisionAuthorObservation{
		RevisionAuthorID: tree.UserIDFromString(rev.AuthorID),
		PageCreatorID:    tree.UserIDFromString(rev.CreatorID),
		PageLastAuthorID: tree.UserIDFromString(rev.LastAuthorID),
	}
}

func matchWorkspaceSyncRevisionAuthors(
	revisionAuthorID tree.UserID,
	pageCreatorID tree.UserID,
	pageLastAuthorID tree.UserID,
) types.GomegaMatcher {
	return WithTransform(observeWorkspaceSyncRevisionAuthors, Equal(workspaceSyncRevisionAuthorObservation{
		RevisionAuthorID: revisionAuthorID,
		PageCreatorID:    pageCreatorID,
		PageLastAuthorID: pageLastAuthorID,
	}))
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
