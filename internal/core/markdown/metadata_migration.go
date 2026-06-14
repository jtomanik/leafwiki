package markdown

import "strings"

func frontmatterToPageMetadata(fm Frontmatter) PageMetadata {
	meta := PageMetadata{
		Version: 1,
		Page: PageMetadataPage{
			ID:           strings.TrimSpace(fm.LeafWikiID),
			Title:        strings.TrimSpace(fm.LeafWikiTitle),
			CreatedAt:    strings.TrimSpace(fm.LeafWikiCreatedAt),
			UpdatedAt:    strings.TrimSpace(fm.LeafWikiUpdatedAt),
			CreatorID:    strings.TrimSpace(fm.LeafWikiCreatorID),
			LastAuthorID: strings.TrimSpace(fm.LeafWikiLastAuthorID),
		},
		Fields: map[string]interface{}{},
		Extra:  map[string]interface{}{},
	}

	for key, value := range fm.ExtraFields {
		switch {
		case strings.EqualFold(strings.TrimSpace(key), "tags"):
			meta.Tags = legacyTags(value)
		case strings.EqualFold(strings.TrimSpace(key), "title") && fm.TitleFromAlias:
			continue
		case key == "version", key == "page", key == "fields", key == "extra":
			meta.Extra[key] = value
		default:
			if IsReservedMetadataKey(key) {
				meta.Extra[key] = value
				continue
			}
			if isLegacyScalarField(value) {
				meta.Fields[key] = value
			} else {
				meta.Extra[key] = value
			}
		}
	}

	return meta
}

func pageMetadataToFrontmatter(meta PageMetadata) Frontmatter {
	fm := Frontmatter{
		LeafWikiID:           meta.Page.ID,
		LeafWikiTitle:        meta.Page.Title,
		LeafWikiCreatedAt:    meta.Page.CreatedAt,
		LeafWikiUpdatedAt:    meta.Page.UpdatedAt,
		LeafWikiCreatorID:    meta.Page.CreatorID,
		LeafWikiLastAuthorID: meta.Page.LastAuthorID,
		ExtraFields:          map[string]interface{}{},
	}

	if len(meta.Tags) > 0 {
		fm.ExtraFields["tags"] = meta.Tags
	}
	for key, value := range meta.Fields {
		fm.ExtraFields[key] = value
	}
	for key, value := range meta.Extra {
		fm.ExtraFields[key] = value
	}
	if len(fm.ExtraFields) == 0 {
		fm.ExtraFields = nil
	}
	return fm
}

func legacyTags(value interface{}) []string {
	switch typed := value.(type) {
	case []string:
		return typed
	case []interface{}:
		tags := make([]string, 0, len(typed))
		for _, item := range typed {
			if tag, ok := item.(string); ok {
				tags = append(tags, tag)
			}
		}
		return tags
	default:
		return nil
	}
}

func isLegacyScalarField(value interface{}) bool {
	switch value.(type) {
	case string, int, int64, float64, bool:
		return true
	default:
		return false
	}
}
