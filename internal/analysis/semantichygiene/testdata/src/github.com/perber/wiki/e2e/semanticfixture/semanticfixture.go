package semanticfixture

type PageID string

func (id PageID) String() string {
	return string(id)
}

func BuildFixturePageID(raw string) PageID {
	return PageID(raw)
}

func ForbiddenE2EDirectCast(raw string) PageID {
	return PageID(raw) // want "direct cast to semantic type PageID outside parser or boundary; use a parser or typed input"
}

func ForbiddenE2EInternalCall(pageID PageID) {
	useInternal(pageID.String()) // want "semantic value PageID converted to string before internal call useInternal; make the callee accept PageID"
}

func useInternal(value string) {}
