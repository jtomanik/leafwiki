package mcp

import (
	"testing"

	"github.com/perber/wiki/internal/core/tree"
)

func TestExactlyOneIDOrPageIDReturnsSemanticPageID(t *testing.T) {
	pageID, err := exactlyOneIDOrPageID("page-1", "")
	if err != nil {
		t.Fatalf("exactlyOneIDOrPageID returned error: %v", err)
	}

	var semanticPageID tree.PageID = pageID
	if semanticPageID != tree.NewPageIDUnchecked("page-1") {
		t.Fatalf("pageID = %q, want page-1", semanticPageID)
	}
}
