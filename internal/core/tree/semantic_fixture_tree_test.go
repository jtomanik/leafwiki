package tree

import "github.com/perber/wiki/internal/core/identity"

func newFixturePageID[T ~string](raw T) PageID {
	return NewPageIDUnchecked(raw)
}

func newFixturePageVersion[T ~string](raw T) PageVersion {
	return NewPageVersionUnchecked(raw)
}

func newFixtureRevisionID[T ~string](raw T) RevisionID {
	return NewRevisionIDUnchecked(string(raw))
}

func newFixtureSlug[T ~string](raw T) Slug {
	return NewSlugUnchecked(raw)
}

func newFixtureRoutePath[T ~string](raw T) RoutePath {
	return RoutePathFromString(raw)
}

func newFixtureMarkdownPath[T ~string](raw T) MarkdownPath {
	return MarkdownPathFromString(raw)
}

func newFixtureAssetName[T ~string](raw T) AssetName {
	return AssetNameFromString(raw)
}

func newFixtureNodeKind[T ~string](raw T) NodeKind {
	return NodeKind(raw)
}

func newFixtureCommitHash[T ~string](raw T) identity.CommitHash {
	return identity.CommitHash(raw)
}

func newFixtureUserID[T ~string](raw T) UserID {
	return NewUserIDUnchecked(string(raw))
}

func newFixtureWorkspaceSourcePath[T ~string](raw T) WorkspaceSourcePath {
	return NewWorkspaceSourcePathUnchecked(string(raw))
}
