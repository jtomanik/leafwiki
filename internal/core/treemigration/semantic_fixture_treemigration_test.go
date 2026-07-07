package treemigration_test

import "github.com/perber/wiki/internal/core/tree"

func newFixtureUserID[T ~string](raw T) tree.UserID {
	return tree.UserIDFromString(raw)
}

func newFixtureSlug[T ~string](raw T) tree.Slug {
	return tree.SlugFromString(raw)
}
