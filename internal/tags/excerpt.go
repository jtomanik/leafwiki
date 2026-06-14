package tags

import "github.com/perber/wiki/internal/core/excerpt"

// ExtractExcerptFromContent parses markdown, including storage metadata, and returns a short plain-text excerpt.
func ExtractExcerptFromContent(raw string) string {
	return excerpt.FromContent(raw)
}
