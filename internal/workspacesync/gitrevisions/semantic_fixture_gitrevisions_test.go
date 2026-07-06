package gitrevisions

func newFixtureActorID[T ~string](raw T) ActorID {
	return NewActorIDUnchecked(string(raw))
}
