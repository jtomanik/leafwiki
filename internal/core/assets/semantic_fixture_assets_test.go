package assets

import (
	"github.com/perber/wiki/internal/core/tree"
)

func newFixtureAssetName[T ~string](raw T) tree.AssetName {
	return tree.NewAssetNameUnchecked(string(raw))
}
