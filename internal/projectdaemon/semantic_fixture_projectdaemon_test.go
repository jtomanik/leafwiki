package projectdaemon

func newFixtureSessionID[T ~string](raw T) SessionID {
	return SessionID(raw)
}
