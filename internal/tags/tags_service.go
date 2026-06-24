package tags

import (
	"strings"

	"github.com/perber/wiki/internal/core/markdown"
	"github.com/perber/wiki/internal/core/tree"
)

type TagsService struct {
	store *TagsStore
}

type TagLimit int

func NewTagsService(store *TagsStore) *TagsService {
	return &TagsService{store: store}
}

func (s *TagsService) ClearIndex() error {
	return s.store.Clear()
}

// IndexPageContent extracts tags and excerpt from rawContent and stores both atomically.
func (s *TagsService) IndexPageContent(pageID tree.PageID, rawContent string) error {
	tags := ExtractTagsFromContent(rawContent)
	excerpt := ExtractExcerptFromContent(rawContent)
	return s.store.SetPageIndex(pageID, tags, excerpt)
}

func (s *TagsService) SetTagsForPage(pageID tree.PageID, tags []string) error {
	return s.store.SetTagsForPage(pageID, tags)
}

func (s *TagsService) DeleteTagsForPage(pageID tree.PageID) error {
	return s.store.DeleteTagsForPage(pageID)
}

func (s *TagsService) DeletePageIndex(pageID tree.PageID) error {
	return s.store.DeletePageIndex(pageID)
}

func (s *TagsService) GetAllTags(filter string, pageSize TagLimit) ([]TagCount, error) {
	return s.store.GetAllTags(filter, pageSize)
}

func (s *TagsService) GetAllTagsForSelection(filter string, selected []string, pageSize TagLimit) ([]TagCount, error) {
	return s.store.GetAllTagsForSelection(filter, selected, pageSize)
}

func (s *TagsService) GetPageIDsByTags(tags []string) ([]tree.PageID, error) {
	return s.store.GetPageIDsByTags(tags)
}

func (s *TagsService) GetTagsForPages(pageIDs []tree.PageID) (map[tree.PageID][]string, error) {
	return s.store.GetTagsForPages(pageIDs)
}

func (s *TagsService) GetExcerptsForPages(pageIDs []tree.PageID) (map[tree.PageID]string, error) {
	return s.store.GetExcerptsForPages(pageIDs)
}

// ExtractTagsFromContent parses page metadata and returns lowercase-normalized tags.
func ExtractTagsFromContent(content string) []string {
	doc, _, err := markdown.ParsePageDocument(content)
	if err != nil || len(doc.Metadata.Tags) == 0 {
		return nil
	}
	return normalizeTags(doc.Metadata.Tags)
}

func normalizeTags(value interface{}) []string {
	var list []string
	switch typed := value.(type) {
	case []string:
		list = typed
	case []interface{}:
		list = make([]string, 0, len(typed))
		for _, item := range typed {
			tag, ok := item.(string)
			if !ok {
				continue
			}
			list = append(list, tag)
		}
	default:
		return nil
	}

	seen := make(map[string]struct{})
	result := make([]string, 0, len(list))
	for _, tag := range list {
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
