package identity

var (
	testUserIDWithWhitespace = newFixtureUserID(" user-1 ")
	testRevisionIDRev1       = newFixtureRevisionID("rev-1")
	testCommitHashABC123     = newFixtureCommitHash("abc123")
)

func newFixtureUserID[T ~string](raw T) UserID {
	return NewUserIDUnchecked(string(raw))
}

func newFixtureRevisionID[T ~string](raw T) RevisionID {
	return NewRevisionIDUnchecked(string(raw))
}

func newFixtureCommitHash[T ~string](raw T) CommitHash {
	return CommitHash(raw)
}
