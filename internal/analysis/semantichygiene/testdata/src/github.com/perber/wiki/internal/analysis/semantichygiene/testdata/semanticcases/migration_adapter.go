package semanticcases

type externalMigrationMetadata struct {
	CreatorID string
}

type migrationNodeAdapter struct {
	id        PageID
	creatorID UserID
}

type adapterDomainResponse struct {
	PageID string // want "semantic-looking field PageID uses string in domain/service type adapterDomainResponse; use PageID or mark the type as a DTO boundary"
}

func (adapter migrationNodeAdapter) ID() string {
	return adapter.id.String()
}

func (adapter migrationNodeAdapter) Metadata() externalMigrationMetadata {
	return externalMigrationMetadata{CreatorID: adapter.creatorID.String()}
}

func (adapter migrationNodeAdapter) parseRoute(routePath RoutePath) {
	ParseRoutePath(routePath.String()) // want "semantic value RoutePath converted to string before internal call ParseRoutePath; make the callee accept RoutePath"
}

func (adapter migrationNodeAdapter) loadPage(pageID string) { // want "semantic-looking parameter pageID uses string in internal function loadPage; use PageID or accept a DTO boundary value"
}

func (adapter *migrationNodeAdapter) SetMetadata(metadata externalMigrationMetadata) {
	adapter.creatorID = UserID(metadata.CreatorID)
}

func (adapter *migrationNodeAdapter) forbiddenAdapterCast(raw string) {
	adapter.id = PageID(raw) // want "direct cast to semantic type PageID outside parser or boundary; use a parser or typed input"
}
