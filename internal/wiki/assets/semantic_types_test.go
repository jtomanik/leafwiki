package assets

import (
	"testing"

	"github.com/perber/wiki/internal/core/tree"
)

func TestAssetUseCaseInputsUseSemanticIDs(t *testing.T) {
	t.Parallel()

	upload := UploadAssetInput{UserID: newFixtureUserID("user-1"), PageID: newFixturePageID("page-1"), Filename: tree.AssetName("diagram.png")}
	var _ tree.UserID = upload.UserID
	var _ tree.PageID = upload.PageID
	var _ tree.AssetName = upload.Filename

	list := ListAssetsInput{PageID: newFixturePageID("page-1")}
	var _ tree.PageID = list.PageID

	get := GetAssetInput{PageID: newFixturePageID("page-1"), Filename: tree.AssetName("diagram.png")}
	var _ tree.PageID = get.PageID
	var _ tree.AssetName = get.Filename
	getOutput := GetAssetOutput{Filename: tree.AssetName("diagram.png")}
	var _ tree.AssetName = getOutput.Filename

	rename := RenameAssetInput{UserID: newFixtureUserID("user-1"), PageID: newFixturePageID("page-1"), OldFilename: tree.AssetName("old.png"), NewFilename: tree.AssetName("new.png")}
	var _ tree.UserID = rename.UserID
	var _ tree.PageID = rename.PageID
	var _ tree.AssetName = rename.OldFilename
	var _ tree.AssetName = rename.NewFilename

	deleteInput := DeleteAssetInput{UserID: newFixtureUserID("user-1"), PageID: newFixturePageID("page-1"), Filename: tree.AssetName("diagram.png")}
	var _ tree.UserID = deleteInput.UserID
	var _ tree.PageID = deleteInput.PageID
	var _ tree.AssetName = deleteInput.Filename
}
