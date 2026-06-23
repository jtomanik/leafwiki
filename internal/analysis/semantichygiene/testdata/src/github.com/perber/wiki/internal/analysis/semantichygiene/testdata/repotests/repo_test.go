package repotests

import "testing"

type PageID string

func (id PageID) String() string {
	return string(id)
}

type RoutePath string

type UserID string

type fixtureMCPAPIKey struct {
	UserID string
}

type pageWireResponse struct {
	PageID string `json:"pageId"`
}

func TestRepoTestFixturesAndWireAssertionsAreAllowed(t *testing.T) {
	pageID := buildFixturePageID("page-1")
	if pageID.String() != "page-1" {
		t.Fatalf("unexpected page ID %q", pageID.String())
	}

	_ = fixtureMCPAPIKey{UserID: "user-1"}
	_ = pageWireResponse{PageID: pageID.String()}
	assertWireError("page_not_found")
}

func TestRepoTestSemanticShortcutsAreRejected(t *testing.T) {
	rawPageID := "page-3"
	pageID := PageID(rawPageID)       // want "direct cast to semantic type PageID outside parser or boundary"
	loadInternalPage(pageID.String()) // want "semantic value PageID converted to string before internal call loadInternalPage; make the callee accept PageID"
}

func buildFixturePageID(raw string) PageID {
	return PageID(raw)
}

func loadInternalPage(pageID string) {}

func assertWireError(code string) {}
