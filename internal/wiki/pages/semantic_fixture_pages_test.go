package pages

import (
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/core/tree"
)

func newFixturePageID[T ~string](raw T) tree.PageID {
	return tree.NewPageIDUnchecked(raw)
}

func newFixturePageVersion[T ~string](raw T) tree.PageVersion {
	return tree.NewPageVersionUnchecked(raw)
}

func newFixtureRawPageVersion[T ~string](raw T) tree.PageVersion {
	return tree.PageVersion(raw)
}

func newFixtureSlug[T ~string](raw T) tree.Slug {
	return tree.NewSlugUnchecked(raw)
}

func newFixtureRoutePath[T ~string](raw T) tree.RoutePath {
	return tree.NewRoutePathUnchecked(string(raw))
}

func newFixtureMarkdownPath[T ~string](raw T) tree.MarkdownPath {
	return tree.MarkdownPath(raw)
}

func newFixtureNodeKind[T ~string](raw T) tree.NodeKind {
	return tree.NodeKind(raw)
}

func newFixtureUserID[T ~string](raw T) tree.UserID {
	return tree.NewUserIDUnchecked(string(raw))
}

func newFixtureErrorCode[T ~string](raw T) sharederrors.ErrorCode {
	return sharederrors.ErrorCode(raw)
}

func newFixtureMessageID[T ~string](raw T) sharederrors.MessageID {
	return sharederrors.MessageID(raw)
}
