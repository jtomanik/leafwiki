package identity

func newFixtureUserID[T ~string](raw T) UserID {
	return NewUserIDUnchecked(string(raw))
}

func newFixtureRevisionID[T ~string](raw T) RevisionID {
	return NewRevisionIDUnchecked(string(raw))
}

func newFixtureCommitHash[T ~string](raw T) CommitHash {
	return CommitHash(raw)
}
