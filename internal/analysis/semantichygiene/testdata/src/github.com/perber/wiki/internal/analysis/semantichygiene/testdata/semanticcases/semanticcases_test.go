package semanticcases

import "testing"

func allowedSerializedComparisonInTest(pageID PageID) bool {
	return pageID.String() == "page-1"
}

func allowedStableContractAssertionsInTest() bool {
	return "page_not_found" == "page_not_found" &&
		"wiki_get_page" == "wiki_get_page" &&
		"validation.route_path.invalid" == "validation.route_path.invalid"
}

func forbiddenInternalStringInTest(pageID PageID) {
	ValidateText(pageID.String()) // want "semantic value PageID converted to string before internal call ValidateText; make the callee accept PageID"
}

func forbiddenDirectCastInTest(raw string) PageID {
	return PageID(raw) // want "direct cast to semantic type PageID outside parser or boundary; use a parser or typed input"
}

func acceptsErrorCode(code ErrorCode) {}

var semanticFixtureSystemUserID UserID

func createNodeWithTitleAndSlug(actor UserID, parent *PageID, title string, slug Slug, kind any) {}

func forbiddenRawSemanticLiteralsInTest() {
	acceptsErrorCode("unknown")                                                       // want "semh:semantic.test-raw-literal: raw string literal passed as ErrorCode in test code; use a semantic fixture/helper value"
	createNodeWithTitleAndSlug(semanticFixtureSystemUserID, nil, "Docs", "docs", nil) // want "semh:semantic.test-raw-literal: raw string literal passed as Slug in test code; use a semantic fixture/helper value"
	var assignedPageID PageID = "page-assigned"                                       // want "semh:semantic.test-raw-literal: raw string literal assigned as PageID in test code; use a semantic fixture/helper value"
	_ = []PageID{"page-sliced"}                                                       // want "semh:semantic.test-raw-literal: raw string literal assigned as PageID in test code; use a semantic fixture/helper value"
	_ = assignedPageID
}

type renderedPresenceState int

const (
	renderedMessageUnknown renderedPresenceState = iota
	renderedMessagePresent
	renderedMessageMissing
)

func hiddenRenderedErrorPresence(actual error) renderedPresenceState {
	state := renderedMessageUnknown
	if actual == nil {
		return state
	}
	if actual.Error() != "" { // want "semh:i18n.rendered-error-presence: rendered error message presence predicate uses Error\\(\\) text"
		state = renderedMessagePresent
	}
	if actual.Error() == "" { // want "semh:i18n.rendered-error-presence: rendered error message presence predicate uses Error\\(\\) text"
		state = renderedMessageMissing
	}
	return state
}

type pointerRenderedError struct{}

func (*pointerRenderedError) Error() string {
	return ""
}

func hiddenPointerRenderedErrorPresence(actual pointerRenderedError) renderedPresenceState {
	state := renderedMessageUnknown
	if actual.Error() != "" { // want "semh:i18n.rendered-error-presence: rendered error message presence predicate uses Error\\(\\) text"
		state = renderedMessagePresent
	}
	return state
}

type renderedContractError struct{}

func (renderedContractError) Error() string {
	return ""
}

func TestRenderedErrorContractPresence(t *testing.T) {
	actual := renderedContractError{}
	if actual.Error() == "" {
		t.Helper()
	}
}
