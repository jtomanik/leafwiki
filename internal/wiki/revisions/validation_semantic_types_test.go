package revisions

import (
	"testing"

	"github.com/perber/wiki/internal/core/revision"
	"github.com/perber/wiki/internal/core/tree"
)

func TestValidateRevisionInputsReturnSemanticValues(t *testing.T) {
	pageID, revisionID, err := ValidateRevisionLookupInput(" page-1 ", " rev-1 ")
	if err != nil {
		t.Fatalf("ValidateRevisionLookupInput returned error: %v", err)
	}

	var semanticPageID tree.PageID = pageID
	var semanticRevisionID revision.RevisionID = revisionID
	if semanticPageID != tree.NewPageIDUnchecked("page-1") {
		t.Fatalf("pageID = %q, want page-1", semanticPageID)
	}
	if semanticRevisionID != revision.NewRevisionIDUnchecked("rev-1") {
		t.Fatalf("revisionID = %q, want rev-1", semanticRevisionID)
	}

	assetPageID, assetRevisionID, assetName, err := ValidateRevisionAssetInput("page-1", "rev-1", "/asset.png")
	if err != nil {
		t.Fatalf("ValidateRevisionAssetInput returned error: %v", err)
	}
	var semanticAssetPageID tree.PageID = assetPageID
	var semanticAssetRevisionID revision.RevisionID = assetRevisionID
	var semanticAssetName tree.AssetName = assetName
	if semanticAssetPageID != tree.NewPageIDUnchecked("page-1") {
		t.Fatalf("asset pageID = %q, want page-1", semanticAssetPageID)
	}
	if semanticAssetRevisionID != revision.NewRevisionIDUnchecked("rev-1") {
		t.Fatalf("asset revisionID = %q, want rev-1", semanticAssetRevisionID)
	}
	if semanticAssetName != tree.NewAssetNameUnchecked("asset.png") {
		t.Fatalf("assetName = %q, want asset.png", semanticAssetName)
	}
}
