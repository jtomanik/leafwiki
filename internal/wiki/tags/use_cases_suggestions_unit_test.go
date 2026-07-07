package tags

import (
	"context"
	"errors"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"
	"github.com/perber/wiki/internal/core/tree"
	coretags "github.com/perber/wiki/internal/tags"
)

var errUnexpectedTagSuggestionPageLookup = errors.New("tag suggestion page lookup should not be called")

var _ = ginkgo.Describe("tag suggestion use case", ginkgo.Label("unit"), func() {
	ginkgo.It("normalizes query filters selections and limits before querying tags", func() {
		svc := &recordingTagSuggestionService{
			allTags:      []coretags.TagCount{{Tag: "go", Count: 3}},
			selectedTags: []coretags.TagCount{{Tag: "react", Count: 2}},
		}
		uc := &GetTagsUseCase{svc: svc}

		allTags, err := uc.Execute(context.Background(), GetTagsInput{
			Filter:   " GO ",
			Selected: []string{" ", ""},
			PageSize: 0,
		})
		Expect(err).To(Succeed())
		Expect(allTags.Tags).To(Equal([]coretags.TagCount{{Tag: "go", Count: 3}}))

		selectedTags, err := uc.Execute(context.Background(), GetTagsInput{
			Filter:   " RE ",
			Selected: []string{" GO ", "go", "React"},
			PageSize: 500,
		})
		Expect(err).To(Succeed())
		Expect(selectedTags.Tags).To(Equal([]coretags.TagCount{{Tag: "react", Count: 2}}))
		Expect(svc).To(matchTagSuggestionQueries(
			tagSuggestionQuery{
				Mode:     tagSuggestionQueryAllTags,
				Filter:   "go",
				PageSize: coretags.TagLimit(50),
			},
			tagSuggestionQuery{
				Mode:     tagSuggestionQuerySelection,
				Filter:   "re",
				Selected: []string{"go", "react"},
				PageSize: coretags.TagLimit(200),
			},
		))
	})
})

type tagSuggestionQueryMode string

const (
	tagSuggestionQueryAllTags   tagSuggestionQueryMode = "all-tags"
	tagSuggestionQuerySelection tagSuggestionQueryMode = "selection"
)

type tagSuggestionQuery struct {
	Mode     tagSuggestionQueryMode
	Filter   string
	Selected []string
	PageSize coretags.TagLimit
}

type recordingTagSuggestionService struct {
	allTags      []coretags.TagCount
	selectedTags []coretags.TagCount
	queries      []tagSuggestionQuery
}

func (s *recordingTagSuggestionService) GetAllTags(filter string, pageSize coretags.TagLimit) ([]coretags.TagCount, error) {
	s.queries = append(s.queries, tagSuggestionQuery{
		Mode:     tagSuggestionQueryAllTags,
		Filter:   filter,
		PageSize: pageSize,
	})
	return s.allTags, nil
}

func (s *recordingTagSuggestionService) GetAllTagsForSelection(filter string, selected []string, pageSize coretags.TagLimit) ([]coretags.TagCount, error) {
	s.queries = append(s.queries, tagSuggestionQuery{
		Mode:     tagSuggestionQuerySelection,
		Filter:   filter,
		Selected: append([]string{}, selected...),
		PageSize: pageSize,
	})
	return s.selectedTags, nil
}

func (*recordingTagSuggestionService) GetPageIDsByTags([]string) ([]tree.PageID, error) {
	return nil, errUnexpectedTagSuggestionPageLookup
}

func (*recordingTagSuggestionService) GetTagsForPages([]tree.PageID) (map[tree.PageID][]string, error) {
	return nil, errUnexpectedTagSuggestionPageLookup
}

func (*recordingTagSuggestionService) GetExcerptsForPages([]tree.PageID) (map[tree.PageID]string, error) {
	return nil, errUnexpectedTagSuggestionPageLookup
}

func matchTagSuggestionQueries(want ...tagSuggestionQuery) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(func(svc *recordingTagSuggestionService) []tagSuggestionQuery {
		return svc.queries
	}, Equal(want))
}
