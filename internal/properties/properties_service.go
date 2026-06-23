package properties

import (
	"strings"

	"github.com/perber/wiki/internal/core/markdown"
	"github.com/perber/wiki/internal/core/tree"
)

// reservedKeys are metadata keys that must never be stored in the properties index.
// Any key starting with "leafwiki_" is also reserved (checked separately).
var reservedKeys = map[string]struct{}{
	"tags":  {},
	"title": {},
}

type PropertiesService struct {
	store *PropertiesStore
}

func NewPropertiesService(store *PropertiesStore) *PropertiesService {
	return &PropertiesService{store: store}
}

func (s *PropertiesService) ClearIndex() error {
	return s.store.Clear()
}

func (s *PropertiesService) IndexPageContent(pageID tree.PageID, rawContent string) error {
	props := ExtractPropertiesFromContent(rawContent)
	return s.store.SetPropertiesForPage(pageID, props)
}

func (s *PropertiesService) SetPropertiesForPage(pageID tree.PageID, props map[string]PropertyEntry) error {
	return s.store.SetPropertiesForPage(pageID, props)
}

func (s *PropertiesService) DeletePropertiesForPage(pageID tree.PageID) error {
	return s.store.DeletePropertiesForPage(pageID)
}

func (s *PropertiesService) GetAllPropertyKeys(filter string, limit int) ([]PropertyKeyCount, error) {
	return s.store.GetAllPropertyKeys(filter, limit)
}

func (s *PropertiesService) GetPageIDsByProperty(key, value string) ([]tree.PageID, error) {
	return s.store.GetPageIDsByProperty(key, value)
}

func (s *PropertiesService) GetPropertiesForPages(pageIDs []tree.PageID) (map[tree.PageID]map[string]PropertyEntry, error) {
	return s.store.GetPropertiesForPages(pageIDs)
}

// ExtractPropertiesFromContent parses page metadata and returns string properties.
// Skips: reserved keys (tags, title, leafwiki_*), non-string fields, and empty values.
func ExtractPropertiesFromContent(content string) map[string]PropertyEntry {
	doc, _, err := markdown.ParsePageDocument(content)
	if err != nil || len(doc.Metadata.Fields) == 0 {
		return nil
	}

	result := make(map[string]PropertyEntry)
	for rawKey, value := range doc.Metadata.Fields {
		key := strings.TrimSpace(rawKey)
		if isReservedKey(key) {
			continue
		}
		entry, ok := toPropertyEntry(value)
		if !ok {
			continue
		}
		result[key] = entry
	}

	if len(result) == 0 {
		return nil
	}
	return result
}

func isReservedKey(key string) bool {
	lower := strings.ToLower(key)
	if _, ok := reservedKeys[lower]; ok {
		return true
	}
	return markdown.IsReservedMetadataKey(key)
}

func toPropertyEntry(value interface{}) (PropertyEntry, bool) {
	s, ok := value.(string)
	if !ok {
		return PropertyEntry{}, false
	}
	s = strings.TrimSpace(s)
	if s == "" || strings.ContainsRune(s, '\n') {
		return PropertyEntry{}, false
	}
	return PropertyEntry{Value: s, Type: "text"}, true
}
