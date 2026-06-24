package assets

import "testing"

func TestAssetDeleteSuccessMessageRendersFromCatalog(t *testing.T) {
	t.Parallel()

	if got := apiSuccessMessage(MessageIDAssetDeleteSuccess); got != "Asset deleted" {
		t.Fatalf("apiSuccessMessage = %q, want Asset deleted", got)
	}
}
