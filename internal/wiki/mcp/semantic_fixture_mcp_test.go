package mcp

import (
	"github.com/perber/wiki/internal/agenthooks"
	"github.com/perber/wiki/internal/core/markdownlinks"
	wikivalidation "github.com/perber/wiki/internal/core/markdownvalidation"
	"github.com/perber/wiki/internal/core/tree"
	"github.com/perber/wiki/internal/workspaceid"
	"github.com/perber/wiki/internal/workspacesync"
)

func newFixturePageID[T ~string](raw T) tree.PageID {
	return tree.NewPageIDUnchecked(raw)
}

func newFixturePageVersion[T ~string](raw T) tree.PageVersion {
	return tree.NewPageVersionUnchecked(raw)
}

func newFixtureRevisionID[T ~string](raw T) tree.RevisionID {
	return tree.NewRevisionIDUnchecked(string(raw))
}

func newFixtureSlug[T ~string](raw T) tree.Slug {
	return tree.NewSlugUnchecked(raw)
}

func newFixtureUserID[T ~string](raw T) tree.UserID {
	return tree.NewUserIDUnchecked(string(raw))
}

func newFixtureContextSessionID[T ~string](raw T) contextSessionID {
	return contextSessionID(raw)
}

func newFixtureRoutePath[T ~string](raw T) tree.RoutePath {
	return tree.NewRoutePathUnchecked(string(raw))
}

func newFixtureMarkdownPath[T ~string](raw T) tree.MarkdownPath {
	return tree.NewMarkdownPathUnchecked(string(raw))
}

func newFixtureAssetName[T ~string](raw T) tree.AssetName {
	return tree.AssetNameFromString(raw)
}

func newFixtureNodeKind[T ~string](raw T) tree.NodeKind {
	return tree.NodeKind(raw)
}

func newFixtureTargetKind[T ~string](raw T) markdownlinks.TargetKind {
	return markdownlinks.TargetKind(raw)
}

func newFixtureCommitHash[T ~string](raw T) workspacesync.CommitHash {
	return workspacesync.NewCommitHashUnchecked(string(raw))
}

func newFixtureWorkspaceID[T ~string](raw T) workspaceid.WorkspaceID {
	return workspaceid.WorkspaceID(raw)
}

func newFixtureToolID[T ~string](raw T) ToolID {
	return ToolID(raw)
}

func newFixtureProviderID[T ~string](raw T) agenthooks.ProviderID {
	return agenthooks.ProviderIDFromString(raw)
}

func newFixtureIssueSeverity[T ~string](raw T) wikivalidation.IssueSeverity {
	return wikivalidation.IssueSeverity(raw)
}
