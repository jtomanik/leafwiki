package tags

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"

	"github.com/gin-gonic/gin"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"

	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/core/tree"
	httpinternal "github.com/perber/wiki/internal/http"
	"github.com/perber/wiki/internal/http/dto"
	coretags "github.com/perber/wiki/internal/tags"
)

var _ = ginkgo.Describe("tags route handlers", ginkgo.Label("unit"), func() {
	ginkgo.It("constructs routes with configured tag use cases", func() {
		getTags := &GetTagsUseCase{}
		getPages := &GetPagesByTagsUseCase{}

		routes := NewRoutes(RoutesConfig{GetTags: getTags, GetPagesByTags: getPages})

		Expect(routes).To(matchTagRouteUseCases(tagRouteUseCases{
			Tags:        getTags,
			PagesByTags: getPages,
		}))
	})

	ginkgo.It("registers public tag endpoints without auth middleware", func() {
		gin.SetMode(gin.TestMode)
		engine := gin.New()

		NewRoutes(RoutesConfig{}).RegisterRoutes(httpinternal.RouterContext{
			Base: engine.Group(""),
			Opts: httpinternal.RouterOptions{PublicAccess: true},
		})

		Expect(tagRegisteredRoutes(engine)).To(exposeTagRouteContract())
	})

	ginkgo.It("registers private tag endpoints behind auth middleware", func() {
		gin.SetMode(gin.TestMode)
		engine := gin.New()

		NewRoutes(RoutesConfig{}).RegisterRoutes(httpinternal.RouterContext{
			Base: engine.Group(""),
			Opts: httpinternal.RouterOptions{AuthDisabled: true},
		})

		Expect(tagRegisteredRoutes(engine)).To(exposeTagRouteContract())
	})

	ginkgo.It("writes tag counts from normalized query parameters", func() {
		ctx, rec := newTagsUnitContext("/api/tags?q=GO&selected=React,rust&limit=2")
		svc := &recordingTagsService{
			tags: []coretags.TagCount{{Tag: "go", Count: 3}},
		}

		(&Routes{getTags: &GetTagsUseCase{svc: svc}}).handleGetTags(ctx)

		Expect(rec).To(HaveHTTPStatus(http.StatusOK), rec.Body.String())
		Expect(decodeTagCountResponse(rec)).To(Equal([]coretags.TagCount{{Tag: "go", Count: 3}}))
		Expect(svc).To(recordSelectedTagQuery("go", []string{"react", "rust"}, coretags.TagLimit(2)))
	})

	ginkgo.It("writes structured errors for invalid tag limits and listing failures", func() {
		invalidLimitCtx, invalidLimitRec := newTagsUnitContext("/api/tags?limit=bad")
		(&Routes{getTags: &GetTagsUseCase{svc: &recordingTagsService{}}}).handleGetTags(invalidLimitCtx)
		Expect(invalidLimitRec).To(matchTagsStructuredError(http.StatusBadRequest, ErrCodeTagsInvalidLimit), invalidLimitRec.Body.String())

		failedListingCtx, failedListingRec := newTagsUnitContext("/api/tags")
		(&Routes{getTags: &GetTagsUseCase{svc: &recordingTagsService{tagErr: errors.New("tag list failed")}}}).handleGetTags(failedListingCtx)
		Expect(failedListingRec).To(matchTagsStructuredError(http.StatusInternalServerError, ErrCodeTagsInternal), failedListingRec.Body.String())
	})

	ginkgo.It("writes pages for requested tags", func() {
		ctx, rec := newTagsUnitContext("/api/tags/pages?tags=Go,React")
		svc := &recordingTagsService{}

		(&Routes{getPagesByTags: &GetPagesByTagsUseCase{svc: svc}}).handleGetPagesByTags(ctx)

		Expect(rec).To(HaveHTTPStatus(http.StatusOK), rec.Body.String())
		Expect(decodeTaggedPageResponse(rec)).To(BeEmpty())
		Expect(svc).To(recordPageTagQuery([]string{"go", "react"}))
	})

	ginkgo.It("writes structured errors for missing tag filters and page lookup failures", func() {
		missingTagsCtx, missingTagsRec := newTagsUnitContext("/api/tags/pages")
		(&Routes{getPagesByTags: &GetPagesByTagsUseCase{svc: &recordingTagsService{}}}).handleGetPagesByTags(missingTagsCtx)
		Expect(missingTagsRec).To(matchTagsStructuredError(http.StatusBadRequest, ErrCodeTagsMissingParam), missingTagsRec.Body.String())

		failedPagesCtx, failedPagesRec := newTagsUnitContext("/api/tags/pages?tags=go")
		(&Routes{getPagesByTags: &GetPagesByTagsUseCase{svc: &recordingTagsService{pageIDsErr: errors.New("page tags failed")}}}).handleGetPagesByTags(failedPagesCtx)
		Expect(failedPagesRec).To(matchTagsStructuredError(http.StatusInternalServerError, ErrCodeTagsInternal), failedPagesRec.Body.String())
	})
})

var _ = ginkgo.Describe("tag query use cases", ginkgo.Label("unit"), func() {
	ginkgo.It("constructs tag listing use cases with available services", func() {
		uc := NewGetTagsUseCase(&coretags.TagsService{})

		Expect(uc.svc).NotTo(BeNil())
	})

	ginkgo.It("lists all tags with normalized filters and bounded page sizes", func() {
		svc := &recordingTagsService{tags: []coretags.TagCount{{Tag: "go", Count: 2}}}

		out, err := (&GetTagsUseCase{svc: svc}).Execute(context.Background(), GetTagsInput{
			Filter:   " GO ",
			PageSize: 500,
		})

		Expect(err).To(Succeed())
		Expect(out.Tags).To(Equal([]coretags.TagCount{{Tag: "go", Count: 2}}))
		Expect(svc).To(recordAllTagQuery("go", coretags.TagLimit(200)))
	})

	ginkgo.It("defaults non-positive page sizes and normalizes nil tag results", func() {
		svc := &recordingTagsService{}

		out, err := (&GetTagsUseCase{svc: svc}).Execute(context.Background(), GetTagsInput{PageSize: 0})

		Expect(err).To(Succeed())
		Expect(out.Tags).To(BeEmpty())
		Expect(svc).To(recordAllTagQuery("", coretags.TagLimit(50)))
	})

	ginkgo.It("returns tag listing failures unchanged", func() {
		expectedErr := errors.New("tag listing failed")

		out, err := (&GetTagsUseCase{svc: &recordingTagsService{tagErr: expectedErr}}).Execute(context.Background(), GetTagsInput{})

		Expect(out).To(BeNil())
		Expect(err).To(MatchError(expectedErr))
	})

	ginkgo.It("constructs page lookup use cases with available services", func() {
		uc := NewGetPagesByTagsUseCase(&coretags.TagsService{}, &tree.TreeService{}, nil)

		Expect(uc).To(matchTaggedPageLookupDependencies())
	})

	ginkgo.It("returns no pages when no tags or no page IDs match", func() {
		noTags, err := (&GetPagesByTagsUseCase{svc: &recordingTagsService{}}).Execute(context.Background(), GetPagesByTagsInput{})
		Expect(err).To(Succeed())
		Expect(noTags.Pages).To(BeEmpty())

		noIDs, err := (&GetPagesByTagsUseCase{svc: &recordingTagsService{}}).Execute(context.Background(), GetPagesByTagsInput{Tags: []string{"go"}})
		Expect(err).To(Succeed())
		Expect(noIDs.Pages).To(BeEmpty())
	})

	ginkgo.It("returns page ID and excerpt lookup failures unchanged", func() {
		pageIDsErr := errors.New("page IDs failed")
		pageIDs, err := (&GetPagesByTagsUseCase{svc: &recordingTagsService{pageIDsErr: pageIDsErr}}).Execute(context.Background(), GetPagesByTagsInput{Tags: []string{"go"}})
		Expect(pageIDs).To(BeNil())
		Expect(err).To(MatchError(pageIDsErr))

		excerptsErr := errors.New("excerpts failed")
		excerpts, err := (&GetPagesByTagsUseCase{svc: &recordingTagsService{
			pageIDs:     []tree.PageID{newFixturePageID("page-1")},
			tagsByPage:  map[tree.PageID][]string{newFixturePageID("page-1"): []string{"go"}},
			excerptsErr: excerptsErr,
		}}).Execute(context.Background(), GetPagesByTagsInput{Tags: []string{"go"}})
		Expect(excerpts).To(BeNil())
		Expect(err).To(MatchError(excerptsErr))
	})

	ginkgo.It("maps matched tag records to tagged page DTOs", func() {
		pageID := newFixturePageID("page-1")

		out, err := (&GetPagesByTagsUseCase{
			svc: &recordingTagsService{
				pageIDs:      []tree.PageID{pageID},
				tagsByPage:   map[tree.PageID][]string{pageID: []string{"go"}},
				excerptsPage: map[tree.PageID]string{pageID: "excerpt"},
			},
			pageFinder: stubTagPageFinder{
				pages: map[tree.PageID]*tree.PageNode{
					pageID: {
						ID:    pageID,
						Title: "Go Guide",
						Slug:  newFixtureSlug("go-guide"),
						Kind:  tree.NodeKindPage,
					},
				},
			},
		}).Execute(context.Background(), GetPagesByTagsInput{Tags: []string{"go"}})

		Expect(err).To(Succeed())
		Expect(out.Pages).To(ConsistOf(SatisfyAll(
			matchTaggedPageWithID(pageID),
			matchTaggedPageWithExcerpt("excerpt"),
			matchTaggedPageWithTags("go"),
		)))
	})

	ginkgo.It("skips matched tag records when the tree page is unavailable", func() {
		pageID := newFixturePageID("missing-page")

		out, err := (&GetPagesByTagsUseCase{
			svc: &recordingTagsService{
				pageIDs:      []tree.PageID{pageID},
				tagsByPage:   map[tree.PageID][]string{pageID: []string{"go"}},
				excerptsPage: map[tree.PageID]string{pageID: "body"},
			},
			treeService: &tree.TreeService{},
		}).Execute(context.Background(), GetPagesByTagsInput{Tags: []string{"go"}})

		Expect(err).To(Succeed())
		Expect(out.Pages).To(BeEmpty())
	})
})

var _ = ginkgo.Describe("tags error responses", ginkgo.Label("unit"), func() {
	ginkgo.It("writes localized and internal structured tag errors", func() {
		localized := newTagsErrorResponseRecorder(func(ctx *gin.Context) {
			respondWithTagsError(ctx, sharederrors.NewLocalizedErrorFromCode(ErrCodeTagsMissingParam, nil))
		})
		Expect(localized).To(matchTagsStructuredError(http.StatusBadRequest, ErrCodeTagsMissingParam), localized.Body.String())

		internal := newTagsErrorResponseRecorder(func(ctx *gin.Context) {
			respondWithTagsError(ctx, errors.New("tag store failed"))
		})
		Expect(internal).To(matchTagsStructuredError(http.StatusInternalServerError, ErrCodeTagsInternal), internal.Body.String())
	})

	ginkgo.It("writes localized bad-request tag errors", func() {
		rec := newTagsErrorResponseRecorder(func(ctx *gin.Context) {
			respondWithTagsBadRequest(ctx, ErrCodeTagsInvalidLimit, "ignored", "ignored")
		})

		Expect(rec).To(matchTagsStructuredError(http.StatusBadRequest, ErrCodeTagsInvalidLimit), rec.Body.String())
	})
})

func newTagsUnitContext(target string) (*gin.Context, *httptest.ResponseRecorder) {
	ginkgo.GinkgoHelper()

	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodGet, target, nil)
	return ctx, rec
}

func tagRegisteredRoutes(engine *gin.Engine) []string {
	routes := engine.Routes()
	out := make([]string, 0, len(routes))
	for _, route := range routes {
		out = append(out, route.Method+" "+route.Path)
	}
	return out
}

func exposeTagRouteContract() types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return ConsistOf(
		"GET /api/tags",
		"GET /api/tags/pages",
	)
}

func decodeTagCountResponse(rec *httptest.ResponseRecorder) []coretags.TagCount {
	ginkgo.GinkgoHelper()

	var body []coretags.TagCount
	Expect(json.Unmarshal(rec.Body.Bytes(), &body)).To(Succeed(), rec.Body.String())
	return body
}

func decodeTaggedPageResponse(rec *httptest.ResponseRecorder) []dto.TaggedPage {
	ginkgo.GinkgoHelper()

	var body []dto.TaggedPage
	Expect(json.Unmarshal(rec.Body.Bytes(), &body)).To(Succeed(), rec.Body.String())
	return body
}

func newTagsErrorResponseRecorder(write func(*gin.Context)) *httptest.ResponseRecorder {
	ginkgo.GinkgoHelper()

	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	write(ctx)
	return rec
}

type recordingTagsService struct {
	tags         []coretags.TagCount
	tagErr       error
	pageIDs      []tree.PageID
	pageIDsErr   error
	tagsByPage   map[tree.PageID][]string
	tagsErr      error
	excerptsPage map[tree.PageID]string
	excerptsErr  error

	allTagQuery      tagQueryObservation
	selectedTagQuery selectedTagQueryObservation
	pageTagQuery     pageTagQueryObservation
}

type stubTagPageFinder struct {
	pages map[tree.PageID]*tree.PageNode
	err   error
}

func (finder stubTagPageFinder) FindPageByID(id tree.PageID) (*tree.PageNode, error) {
	if finder.err != nil {
		return nil, finder.err
	}
	return finder.pages[id], nil
}

func (svc *recordingTagsService) GetAllTags(filter string, pageSize coretags.TagLimit) ([]coretags.TagCount, error) {
	svc.allTagQuery = tagQueryObservation{Filter: filter, PageSize: pageSize}
	if svc.tagErr != nil {
		return nil, svc.tagErr
	}
	return svc.tags, nil
}

func (svc *recordingTagsService) GetAllTagsForSelection(filter string, selected []string, pageSize coretags.TagLimit) ([]coretags.TagCount, error) {
	svc.selectedTagQuery = selectedTagQueryObservation{Filter: filter, Selected: selected, PageSize: pageSize}
	if svc.tagErr != nil {
		return nil, svc.tagErr
	}
	return svc.tags, nil
}

func (svc *recordingTagsService) GetPageIDsByTags(tags []string) ([]tree.PageID, error) {
	svc.pageTagQuery = pageTagQueryObservation{Tags: tags}
	if svc.pageIDsErr != nil {
		return nil, svc.pageIDsErr
	}
	return svc.pageIDs, nil
}

func (svc *recordingTagsService) GetTagsForPages([]tree.PageID) (map[tree.PageID][]string, error) {
	if svc.tagsErr != nil {
		return nil, svc.tagsErr
	}
	return svc.tagsByPage, nil
}

func (svc *recordingTagsService) GetExcerptsForPages([]tree.PageID) (map[tree.PageID]string, error) {
	if svc.excerptsErr != nil {
		return nil, svc.excerptsErr
	}
	return svc.excerptsPage, nil
}

type tagQueryObservation struct {
	Filter   string
	PageSize coretags.TagLimit
}

func recordAllTagQuery(filter string, limit coretags.TagLimit) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(observedAllTagQuery, Equal(tagQueryObservation{Filter: filter, PageSize: limit}))
}

func observedAllTagQuery(svc *recordingTagsService) tagQueryObservation {
	return svc.allTagQuery
}

type selectedTagQueryObservation struct {
	Filter   string
	Selected []string
	PageSize coretags.TagLimit
}

func recordSelectedTagQuery(filter string, selected []string, limit coretags.TagLimit) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(observedSelectedTagQuery, Equal(selectedTagQueryObservation{Filter: filter, Selected: selected, PageSize: limit}))
}

func observedSelectedTagQuery(svc *recordingTagsService) selectedTagQueryObservation {
	return svc.selectedTagQuery
}

type pageTagQueryObservation struct {
	Tags []string
}

func recordPageTagQuery(tags []string) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(observedPageTagQuery, Equal(pageTagQueryObservation{Tags: tags}))
}

func observedPageTagQuery(svc *recordingTagsService) pageTagQueryObservation {
	return svc.pageTagQuery
}

type tagRouteUseCases struct {
	Tags        *GetTagsUseCase
	PagesByTags *GetPagesByTagsUseCase
}

func matchTagRouteUseCases(want tagRouteUseCases) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(tagRouteUseCasesFor, Equal(want))
}

func tagRouteUseCasesFor(routes *Routes) tagRouteUseCases {
	return tagRouteUseCases{
		Tags:        routes.getTags,
		PagesByTags: routes.getPagesByTags,
	}
}

func matchTaggedPageLookupDependencies() types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return SatisfyAll(
		WithTransform(func(uc *GetPagesByTagsUseCase) any { return uc.svc }, Not(BeNil())),
		WithTransform(func(uc *GetPagesByTagsUseCase) any { return uc.treeService }, Not(BeNil())),
		WithTransform(func(uc *GetPagesByTagsUseCase) any { return uc.pageFinder }, Not(BeNil())),
	)
}
