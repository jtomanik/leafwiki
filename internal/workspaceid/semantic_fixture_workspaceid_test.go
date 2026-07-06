package workspaceid

func newFixtureWorkspaceID[T ~string](raw T) WorkspaceID {
	return WorkspaceID(raw)
}
