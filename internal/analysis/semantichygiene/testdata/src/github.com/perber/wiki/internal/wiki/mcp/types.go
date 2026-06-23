package mcp

type getPageInput struct {
	ID     string `json:"id"`
	PageID string `json:"pageId"`
}

type unsafeWireInput struct {
	PageID string // want "semantic-looking field PageID uses string in domain/service type unsafeWireInput; use PageID or mark the type as a DTO boundary"
}

type pageOutput struct {
	ID   string `json:"id"`
	Slug string `json:"slug"`
}
