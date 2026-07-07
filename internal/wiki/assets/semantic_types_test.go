package assets

import (
	ginkgo "github.com/onsi/ginkgo/v2"

	"github.com/perber/wiki/internal/core/tree"
)

var _ = ginkgo.Describe("asset semantic types", ginkgo.Label("unit"), func() {
	ginkgo.It("keeps use-case boundaries typed with semantic asset identifiers", func() {
		upload := UploadAssetInput{UserID: newFixtureUserID("user-1"), PageID: newFixturePageID("page-1"), Filename: newFixtureAssetName("diagram.png")}
		var _ tree.UserID = upload.UserID
		var _ tree.PageID = upload.PageID
		var _ tree.AssetName = upload.Filename

		list := ListAssetsInput{PageID: newFixturePageID("page-1")}
		var _ tree.PageID = list.PageID

		get := GetAssetInput{PageID: newFixturePageID("page-1"), Filename: newFixtureAssetName("diagram.png")}
		var _ tree.PageID = get.PageID
		var _ tree.AssetName = get.Filename
		getOutput := GetAssetOutput{Filename: newFixtureAssetName("diagram.png")}
		var _ tree.AssetName = getOutput.Filename

		rename := RenameAssetInput{UserID: newFixtureUserID("user-1"), PageID: newFixturePageID("page-1"), OldFilename: newFixtureAssetName("old.png"), NewFilename: newFixtureAssetName("new.png")}
		var _ tree.UserID = rename.UserID
		var _ tree.PageID = rename.PageID
		var _ tree.AssetName = rename.OldFilename
		var _ tree.AssetName = rename.NewFilename

		deleteInput := DeleteAssetInput{UserID: newFixtureUserID("user-1"), PageID: newFixturePageID("page-1"), Filename: newFixtureAssetName("diagram.png")}
		var _ tree.UserID = deleteInput.UserID
		var _ tree.PageID = deleteInput.PageID
		var _ tree.AssetName = deleteInput.Filename
	})
})
