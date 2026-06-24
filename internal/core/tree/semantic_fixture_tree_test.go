package tree

func newFixtureMarkdownPath[T ~string](raw T) MarkdownPath {
	return NewMarkdownPathUnchecked(string(raw))
}

func newFixturePageID[T ~string](raw T) PageID {
	return NewPageIDUnchecked(raw)
}

func newFixturePageVersion[T ~string](raw T) PageVersion {
	return NewPageVersionUnchecked(raw)
}

func newFixtureRevisionID[T ~string](raw T) RevisionID {
	return NewRevisionIDUnchecked(string(raw))
}

func newFixtureRoutePath[T ~string](raw T) RoutePath {
	return NewRoutePathUnchecked(string(raw))
}

func newFixtureSlug[T ~string](raw T) Slug {
	return NewSlugUnchecked(raw)
}

func newFixtureUserID[T ~string](raw T) UserID {
	return NewUserIDUnchecked(string(raw))
}

func newFixtureWorkspaceSourcePath[T ~string](raw T) WorkspaceSourcePath {
	return NewWorkspaceSourcePathUnchecked(string(raw))
}
