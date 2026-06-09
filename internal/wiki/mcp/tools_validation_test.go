package mcp

import "testing"

func TestCachedValidationAssetExistsBuildsPredicateOncePerPage(t *testing.T) {
	calls := map[string]int{}
	assetExists := cachedValidationAssetExists(func(pageID string) func(string) bool {
		calls[pageID]++
		return func(destination string) bool {
			return pageID == "page-1" && destination == "logo.png"
		}
	})

	if !assetExists(" page-1 ", "logo.png") {
		t.Fatalf("assetExists(page-1, logo.png) = false, want true")
	}
	if assetExists("page-1", "other.png") {
		t.Fatalf("assetExists(page-1, other.png) = true, want false")
	}
	if assetExists("page-2", "logo.png") {
		t.Fatalf("assetExists(page-2, logo.png) = true, want false")
	}
	assetExists("page-1", "second.png")

	if calls["page-1"] != 1 {
		t.Fatalf("page-1 predicate factory calls = %d, want 1", calls["page-1"])
	}
	if calls["page-2"] != 1 {
		t.Fatalf("page-2 predicate factory calls = %d, want 1", calls["page-2"])
	}
}
