package treemigration_test

import "github.com/perber/wiki/internal/core/tree"

func newFixtureUserID[T ~string](raw T) tree.UserID {
	return tree.UserIDFromString(raw)
}

func newFixtureSlug[T ~string](raw T) tree.Slug {
	return tree.SlugFromString(raw)
}

func newFixturePageID[T ~string](raw T) tree.PageID {
	return tree.PageIDFromString(raw)
}

func newFixtureRoutePath[T ~string](raw T) tree.RoutePath {
	return tree.RoutePathFromString(raw)
}
