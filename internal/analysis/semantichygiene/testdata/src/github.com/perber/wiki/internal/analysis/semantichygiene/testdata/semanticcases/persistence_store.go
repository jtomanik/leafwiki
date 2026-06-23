package semanticcases

type pageStoreRow struct {
	PageID string
}

func allowedPersistenceRow(pageID PageID) pageStoreRow {
	return pageStoreRow{PageID: pageID.String()}
}

func forbiddenStoreHelper(pageID PageID) {
	ValidateText(pageID.String()) // want "semantic value PageID converted to string before internal call ValidateText; make the callee accept PageID"
}

func forbiddenStoreDirectCast(raw string) PageID {
	return PageID(raw) // want "direct cast to semantic type PageID outside parser or boundary; use a parser or typed input"
}
