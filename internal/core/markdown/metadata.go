package markdown

import "strings"

type PageDocument struct {
	Body     string
	Metadata PageMetadata
}

type PageMetadata struct {
	Version int                    `json:"version"`
	Page    PageMetadataPage       `json:"page"`
	Tags    []string               `json:"tags,omitempty"`
	Fields  map[string]interface{} `json:"fields,omitempty"`
	Extra   map[string]interface{} `json:"extra,omitempty"`
}

type PageMetadataPage struct {
	ID           string `json:"id,omitempty"`
	Title        string `json:"title,omitempty"`
	CreatedAt    string `json:"created_at,omitempty"`
	UpdatedAt    string `json:"updated_at,omitempty"`
	CreatorID    string `json:"creator_id,omitempty"`
	LastAuthorID string `json:"last_author_id,omitempty"`
}

type PageDocumentParseResult struct {
	RequiresWriteback bool
}

func IsReservedMetadataKey(key string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(key)), "leafwiki_")
}
