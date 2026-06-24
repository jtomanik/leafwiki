package workspacesync

import (
	"github.com/perber/wiki/internal/core/tree"
)

func newFixtureCommitHash[T ~string](raw T) CommitHash {
	return NewCommitHashUnchecked(string(raw))
}

func newFixturePageID[T ~string](raw T) tree.PageID {
	return tree.NewPageIDUnchecked(raw)
}
