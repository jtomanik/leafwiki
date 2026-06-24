package pages

import (
	"strings"

	"github.com/perber/wiki/internal/core/markdown"
	"github.com/perber/wiki/internal/core/tree"
	"github.com/perber/wiki/internal/http/dto"
)

var reservedPropertyKeys = map[string]struct{}{
	"tags":  {},
	"title": {},
}

// EnrichPageMetadata fills API page metadata from stored page metadata.
func EnrichPageMetadata(page *dto.Page, readPageRaw func(tree.PageID) (string, error)) {
	if page == nil {
		return
	}

	page.Tags = []string{}
	page.Properties = map[string]string{}

	raw, err := readPageRaw(tree.PageIDFromString(page.ID))
	if err != nil {
		return
	}

	doc, _, err := markdown.ParsePageDocument(raw)
	if err != nil {
		return
	}

	page.Tags, page.Properties = ExtractPageMetadataFromPageMetadata(doc.Metadata)
}

func ExtractPageMetadataFromPageMetadata(meta markdown.PageMetadata) ([]string, map[string]string) {
	return normalizeTagInputs(meta.Tags), extractStringProperties(meta.Fields)
}

type PublicMetadataPatch struct {
	Tags              []string
	TagsPresent       bool
	Properties        map[string]string
	PropertiesPresent bool
}

func BuildMarkdownWithPublicMetadataPatch(currentRaw string, pageID tree.PageID, title string, patch PublicMetadataPatch, body string) (string, error) {
	doc, _, err := markdown.ParsePageDocument(currentRaw)
	if err != nil {
		return "", err
	}

	currentTags, currentProperties := ExtractPageMetadataFromPageMetadata(doc.Metadata)
	tags := currentTags
	if patch.TagsPresent {
		tags = patch.Tags
	}
	properties := currentProperties
	if patch.PropertiesPresent {
		properties = patch.Properties
	}

	meta := ApplyPublicMetadata(doc.Metadata, currentProperties, tags, properties)
	meta.Version = 1
	meta.Page.ID = pageID.MetadataValue()
	meta.Page.Title = strings.TrimSpace(title)

	return markdown.RenderPageDocument(markdown.PageDocument{
		Body:     body,
		Metadata: meta,
	})
}

func ApplyPublicMetadata(current markdown.PageMetadata, currentProperties map[string]string, tags []string, properties map[string]string) markdown.PageMetadata {
	next := current
	next.Tags = normalizeTagInputs(tags)

	fields := map[string]interface{}{}
	for key, value := range current.Fields {
		if _, isPublicProperty := currentProperties[key]; isPublicProperty {
			continue
		}
		fields[key] = value
	}
	for key, value := range properties {
		fields[key] = value
	}
	if len(fields) == 0 {
		fields = nil
	}
	next.Fields = fields
	return next
}

func extractStringProperties(fields map[string]interface{}) map[string]string {
	properties := map[string]string{}
	for rawKey, value := range fields {
		key := strings.TrimSpace(rawKey)
		lower := strings.ToLower(key)
		if _, reserved := reservedPropertyKeys[lower]; reserved {
			continue
		}
		if markdown.IsReservedMetadataKey(key) {
			continue
		}
		s, ok := value.(string)
		if !ok {
			continue
		}
		s = strings.TrimSpace(s)
		if s == "" || strings.ContainsRune(s, '\n') {
			continue
		}
		properties[key] = s
	}
	return properties
}

func normalizeMetadataTags(value interface{}) []string {
	var rawTags []string
	switch typed := value.(type) {
	case []string:
		rawTags = typed
	case []interface{}:
		rawTags = make([]string, 0, len(typed))
		for _, item := range typed {
			tag, ok := item.(string)
			if !ok {
				continue
			}
			rawTags = append(rawTags, tag)
		}
	default:
		return []string{}
	}

	return normalizeTagInputs(rawTags)
}

func normalizeTagInputs(tags []string) []string {
	seen := make(map[string]struct{}, len(tags))
	result := make([]string, 0, len(tags))

	for _, tag := range tags {
		normalized := strings.ToLower(strings.TrimSpace(tag))
		if normalized == "" {
			continue
		}
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}
		result = append(result, normalized)
	}

	return result
}
