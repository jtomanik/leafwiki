package semanticcases

func domainAdapterCast(raw string) PageID {
	return PageID(raw) // want "direct cast to semantic type PageID outside parser or boundary; use a parser or typed input"
}
