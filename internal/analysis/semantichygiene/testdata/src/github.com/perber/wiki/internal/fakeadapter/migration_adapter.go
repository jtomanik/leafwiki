package fakeadapter

type PageID string

func (id PageID) String() string {
	return string(id)
}

type domainRecord struct {
	PageID string // want "semantic-looking field PageID uses string in domain/service type domainRecord; use PageID or mark the type as a DTO boundary"
}

type fakeDomainAdapter struct {
	pageID PageID
}

func (adapter fakeDomainAdapter) ID() string {
	return adapter.pageID.String() // want "semantic value PageID returned as string from internal function ID; return PageID or serialize only at a boundary"
}

func (adapter fakeDomainAdapter) Metadata() domainRecord {
	return domainRecord{PageID: adapter.pageID.String()} // want "semantic value PageID converted to string for semantic field PageID; keep the field typed or convert only at a boundary"
}
