package search

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"

	"github.com/gin-gonic/gin"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"

	"github.com/perber/wiki/internal/core/tree"
	httpinternal "github.com/perber/wiki/internal/http"
	coresearch "github.com/perber/wiki/internal/search"
	coretags "github.com/perber/wiki/internal/tags"
)

var errSearchTagsUnavailable = errors.New("search tags unavailable")

var _ = ginkgo.Describe("search route handlers", ginkgo.Label("unit"), func() {
	ginkgo.It("constructs routes with configured use cases", func() {
		searchUC := &SearchUseCase{}
		statusUC := &GetIndexingStatusUseCase{}

		routes := NewRoutes(RoutesConfig{Search: searchUC, GetIndexingStatus: statusUC})

		Expect(routes).To(matchSearchRouteUseCases(searchRouteUseCases{
			Search:         searchUC,
			IndexingStatus: statusUC,
		}))
	})

	ginkgo.It("registers public search endpoints without auth middleware", func() {
		gin.SetMode(gin.TestMode)
		engine := gin.New()

		NewRoutes(RoutesConfig{}).RegisterRoutes(httpinternal.RouterContext{
			Base: engine.Group(""),
			Opts: httpinternal.RouterOptions{PublicAccess: true},
		})

		Expect(searchRegisteredRoutes(engine)).To(exposeSearchRouteContract())
	})

	ginkgo.It("registers private search endpoints behind auth middleware", func() {
		gin.SetMode(gin.TestMode)
		engine := gin.New()

		NewRoutes(RoutesConfig{}).RegisterRoutes(httpinternal.RouterContext{
			Base: engine.Group(""),
			Opts: httpinternal.RouterOptions{AuthDisabled: true},
		})

		Expect(searchRegisteredRoutes(engine)).To(exposeSearchRouteContract())
	})

	ginkgo.It("writes search results from normalized route query parameters", func() {
		pageID := newFixtureSearchPageID("page-1")
		index := &recordingSearchIndex{
			result: &coresearch.SearchResult{
				Items: []coresearch.SearchResultItem{{PageID: pageID, Title: "Docs"}},
			},
			pageIDs: []tree.PageID{pageID},
		}
		tags := &recordingSearchTags{
			pageIDs:    []tree.PageID{pageID},
			tagsByPage: map[tree.PageID][]string{pageID: {"go", "docs"}},
		}
		ctx, rec := newSearchUnitContext("/api/search?q=guide&tags=Go,docs&offset=2&limit=3")

		(&Routes{search: &SearchUseCase{index: index, tags: tags}}).handleSearch(ctx)

		Expect(rec).To(HaveHTTPStatus(http.StatusOK), rec.Body.String())
		Expect(decodeSearchResult(rec)).To(matchSearchResult(gstruct.Fields{
			"Items": HaveExactElements(matchSearchResultItem(gstruct.Fields{
				"PageID": Equal(pageID),
				"Tags":   Equal([]string{"go", "docs"}),
			})),
		}))
		Expect(index).To(recordSearchIndexQuery(searchIndexQueryObservation{
			Query:    "guide",
			PageIDs:  []tree.PageID{pageID},
			StartAt:  coresearch.ResultOffset(2),
			PageSize: coresearch.ResultLimit(3),
		}))
		Expect(tags).To(recordSearchTagFilter([]string{"go", "docs"}))
	})

	ginkgo.It("writes structured validation and execution errors", func() {
		missingCtx, missingRec := newSearchUnitContext("/api/search")
		(&Routes{search: &SearchUseCase{}}).handleSearch(missingCtx)
		Expect(missingRec).To(matchSearchStructuredError(http.StatusBadRequest, ErrCodeSearchMissingQuery), missingRec.Body.String())

		offsetCtx, offsetRec := newSearchUnitContext("/api/search?q=docs&offset=bad")
		(&Routes{search: &SearchUseCase{}}).handleSearch(offsetCtx)
		Expect(offsetRec).To(matchSearchStructuredError(http.StatusBadRequest, ErrCodeSearchInvalidOffset), offsetRec.Body.String())

		limitCtx, limitRec := newSearchUnitContext("/api/search?q=docs&limit=bad")
		(&Routes{search: &SearchUseCase{}}).handleSearch(limitCtx)
		Expect(limitRec).To(matchSearchStructuredError(http.StatusBadRequest, ErrCodeSearchInvalidLimit), limitRec.Body.String())

		failedCtx, failedRec := newSearchUnitContext("/api/search?q=docs")
		(&Routes{search: &SearchUseCase{index: &recordingSearchIndex{searchErr: errors.New("search failed")}}}).handleSearch(failedCtx)
		Expect(failedRec).To(matchSearchStructuredError(http.StatusInternalServerError, ErrCodeSearchInternal), failedRec.Body.String())
	})

	ginkgo.It("writes indexing status snapshots", func() {
		status := coresearch.NewIndexingStatus()
		status.Start()
		status.Success()
		ctx, rec := newSearchUnitContext("/api/search/status")

		(&Routes{getIndexingStatus: NewGetIndexingStatusUseCase(status)}).handleGetIndexingStatus(ctx)

		Expect(rec).To(HaveHTTPStatus(http.StatusOK), rec.Body.String())
		var body coresearch.IndexingStatus
		Expect(json.Unmarshal(rec.Body.Bytes(), &body)).To(Succeed(), rec.Body.String())
		Expect(&body).To(matchActiveIndexingStatusSnapshot(1, 0))
	})
})

var _ = ginkgo.Describe("search use case contracts", ginkgo.Label("unit"), func() {
	ginkgo.It("normalizes concrete constructor dependencies without invoking adapters", func() {
		Expect(NewSearchUseCase(nil, nil, nil)).To(matchSearchUseCaseDependencies(BeNil(), BeNil(), BeNil()))
		uc := NewSearchUseCase(&coresearch.SQLiteIndex{}, &coretags.TagsService{}, &tree.TreeService{})
		Expect(uc).To(matchSearchUseCaseDependencies(Not(BeNil()), Not(BeNil()), Not(BeNil())))
	})

	ginkgo.It("returns tag lookup and index search failures unchanged", func() {
		tagErr := errors.New("tag lookup failed")
		out, err := (&SearchUseCase{
			index: &recordingSearchIndex{result: &coresearch.SearchResult{}},
			tags:  &recordingSearchTags{pageIDsErr: tagErr},
		}).Execute(context.Background(), SearchInput{Query: "docs", Tags: []string{"go"}})
		Expect(out).To(BeNil())
		Expect(err).To(MatchError(tagErr))

		searchErr := errors.New("index search failed")
		out, err = (&SearchUseCase{
			index: &recordingSearchIndex{searchErr: searchErr},
		}).Execute(context.Background(), SearchInput{Query: "docs"})
		Expect(out).To(BeNil())
		Expect(err).To(MatchError(searchErr))
	})

	ginkgo.It("returns sorted and paged tag-only results with facets", func() {
		alphaID := newFixtureSearchPageID("alpha")
		betaID := newFixtureSearchPageID("beta")
		missingID := newFixtureSearchPageID("missing")
		tags := &recordingSearchTags{
			pageIDs: []tree.PageID{betaID, missingID, alphaID},
			excerpts: map[tree.PageID]string{
				alphaID: "alpha excerpt",
				betaID:  "beta excerpt",
			},
			tagsByPage: map[tree.PageID][]string{
				alphaID:   {"go", "docs"},
				betaID:    {"go"},
				missingID: {"orphan"},
			},
		}
		searchTree := recordingSearchTree{nodes: map[tree.PageID]*tree.PageNode{
			alphaID: searchPageNode(alphaID, "Alpha", newFixtureSearchRoutePath("alpha")),
			betaID:  searchPageNode(betaID, "Beta", newFixtureSearchRoutePath("beta")),
		}}
		uc := &SearchUseCase{index: &recordingSearchIndex{}, tags: tags, tree: searchTree}

		out, err := uc.Execute(context.Background(), SearchInput{Tags: []string{" GO "}, StartAt: -4, PageSize: 0})

		Expect(err).To(Succeed())
		Expect(out.Result).To(matchSearchResult(gstruct.Fields{
			"Count":    Equal(2),
			"StartAt":  Equal(coresearch.ResultOffset(0)),
			"PageSize": Equal(coresearch.ResultLimit(20)),
			"Items": HaveExactElements(
				matchSearchResultItem(gstruct.Fields{"PageID": Equal(alphaID), "Excerpt": Equal("alpha excerpt"), "Tags": Equal([]string{"go", "docs"})}),
				matchSearchResultItem(gstruct.Fields{"PageID": Equal(betaID), "Excerpt": Equal("beta excerpt"), "Tags": Equal([]string{"go"})}),
			),
			"TagFacets": Equal([]coresearch.SearchTagFacet{
				{Tag: "go", Count: 2},
				{Tag: "docs", Count: 1},
				{Tag: "orphan", Count: 1},
			}),
		}))

		emptyPage, err := uc.Execute(context.Background(), SearchInput{Tags: []string{"go"}, StartAt: 10, PageSize: 5})
		Expect(err).To(Succeed())
		Expect(emptyPage.Result.Items).To(BeEmpty())
	})

	ginkgo.It("sorts equal tag-only titles by path and marks pages without tag records", func() {
		laterPathID := newFixtureSearchPageID("later-path")
		earlierPathID := newFixtureSearchPageID("earlier-path")
		tags := &recordingSearchTags{
			pageIDs: []tree.PageID{laterPathID, earlierPathID},
			excerpts: map[tree.PageID]string{
				laterPathID:   "later excerpt",
				earlierPathID: "earlier excerpt",
			},
			tagsByPage: map[tree.PageID][]string{
				earlierPathID: {"docs"},
			},
		}
		uc := &SearchUseCase{
			index: &recordingSearchIndex{},
			tags:  tags,
			tree: recordingSearchTree{nodes: map[tree.PageID]*tree.PageNode{
				laterPathID:   searchPageNode(laterPathID, "Same", newFixtureSearchRoutePath("z-later")),
				earlierPathID: searchPageNode(earlierPathID, "Same", newFixtureSearchRoutePath("a-earlier")),
			}},
		}

		out, err := uc.Execute(context.Background(), SearchInput{Tags: []string{"docs"}, PageSize: 10})

		Expect(err).To(Succeed())
		Expect(out.Result.Items).To(HaveExactElements(
			matchSearchResultItem(gstruct.Fields{"PageID": Equal(earlierPathID), "Tags": Equal([]string{"docs"})}),
			matchSearchResultItem(gstruct.Fields{"PageID": Equal(laterPathID), "Tags": BeEmpty()}),
		))
	})

	ginkgo.It("returns tag-only excerpt lookup failures", func() {
		excerptsErr := errors.New("excerpt lookup failed")
		uc := &SearchUseCase{
			index: &recordingSearchIndex{},
			tags:  &recordingSearchTags{pageIDs: []tree.PageID{newFixtureSearchPageID("page-1")}, excerptsErr: excerptsErr},
			tree:  recordingSearchTree{},
		}

		out, err := uc.Execute(context.Background(), SearchInput{Tags: []string{"go"}})

		Expect(out).To(BeNil())
		Expect(err).To(MatchError(excerptsErr))
	})

	ginkgo.It("keeps tag attachment and facet helpers empty when dependencies or values are absent", func() {
		uc := &SearchUseCase{}
		uc.attachTags(nil)
		Expect(uc.buildTagFacets(nil)).To(BeEmpty())

		noIDItems := []coresearch.SearchResultItem{{Title: "No ID"}}
		(&SearchUseCase{tags: &recordingSearchTags{}}).attachTags(noIDItems)
		Expect(noIDItems).To(HaveExactElements(matchSearchResultItem(gstruct.Fields{"Tags": BeNil()})))

		items := []coresearch.SearchResultItem{{PageID: newFixtureSearchPageID("page-1"), Title: "Docs"}}
		(&SearchUseCase{tags: &recordingSearchTags{tagsErr: errSearchTagsUnavailable}}).attachTags(items)
		Expect(items).To(HaveExactElements(matchSearchResultItem(gstruct.Fields{"Tags": BeNil()})))

		Expect((&SearchUseCase{tags: &recordingSearchTags{tagsErr: errSearchTagsUnavailable}}).buildTagFacets([]tree.PageID{newFixtureSearchPageID("page-1")})).To(BeEmpty())
	})
})

type searchIndexQueryObservation struct {
	Query    string
	PageIDs  []tree.PageID
	StartAt  coresearch.ResultOffset
	PageSize coresearch.ResultLimit
}

type recordingSearchIndex struct {
	result     *coresearch.SearchResult
	searchErr  error
	pageIDs    []tree.PageID
	pageIDsErr error

	seen searchIndexQueryObservation
}

func (idx *recordingSearchIndex) Search(query string, pageIDs []tree.PageID, startAt coresearch.ResultOffset, pageSize coresearch.ResultLimit) (*coresearch.SearchResult, error) {
	idx.seen = searchIndexQueryObservation{Query: query, PageIDs: pageIDs, StartAt: startAt, PageSize: pageSize}
	if idx.searchErr != nil {
		return nil, idx.searchErr
	}
	if idx.result != nil {
		return idx.result, nil
	}
	return &coresearch.SearchResult{}, nil
}

func (idx *recordingSearchIndex) SearchPageIDs(string, []tree.PageID) ([]tree.PageID, error) {
	if idx.pageIDsErr != nil {
		return nil, idx.pageIDsErr
	}
	return idx.pageIDs, nil
}

type recordingSearchTags struct {
	pageIDs     []tree.PageID
	pageIDsErr  error
	tagsByPage  map[tree.PageID][]string
	tagsErr     error
	excerpts    map[tree.PageID]string
	excerptsErr error

	seenFilter []string
}

func (tags *recordingSearchTags) GetPageIDsByTags(filter []string) ([]tree.PageID, error) {
	tags.seenFilter = append([]string(nil), filter...)
	if tags.pageIDsErr != nil {
		return nil, tags.pageIDsErr
	}
	return tags.pageIDs, nil
}

func (tags *recordingSearchTags) GetTagsForPages(pageIDs []tree.PageID) (map[tree.PageID][]string, error) {
	if tags.tagsErr != nil {
		return nil, tags.tagsErr
	}
	return tags.tagsByPage, nil
}

func (tags *recordingSearchTags) GetExcerptsForPages(pageIDs []tree.PageID) (map[tree.PageID]string, error) {
	if tags.excerptsErr != nil {
		return nil, tags.excerptsErr
	}
	return tags.excerpts, nil
}

type recordingSearchTree struct {
	nodes map[tree.PageID]*tree.PageNode
	err   error
}

func (searchTree recordingSearchTree) FindPageByID(pageID tree.PageID) (*tree.PageNode, error) {
	if searchTree.err != nil {
		return nil, searchTree.err
	}
	node := searchTree.nodes[pageID]
	if node == nil {
		return nil, tree.ErrPageNotFound
	}
	return node, nil
}

func newSearchUnitContext(target string) (*gin.Context, *httptest.ResponseRecorder) {
	ginkgo.GinkgoHelper()

	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodGet, target, nil)
	return ctx, rec
}

func searchRegisteredRoutes(engine *gin.Engine) []string {
	routes := engine.Routes()
	out := make([]string, 0, len(routes))
	for _, route := range routes {
		out = append(out, route.Method+" "+route.Path)
	}
	return out
}

func exposeSearchRouteContract() types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return ConsistOf("GET /api/search", "GET /api/search/status")
}

func decodeSearchResult(rec *httptest.ResponseRecorder) *coresearch.SearchResult {
	ginkgo.GinkgoHelper()

	var body coresearch.SearchResult
	Expect(json.Unmarshal(rec.Body.Bytes(), &body)).To(Succeed(), rec.Body.String())
	return &body
}

func recordSearchIndexQuery(want searchIndexQueryObservation) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(func(index *recordingSearchIndex) searchIndexQueryObservation {
		return index.seen
	}, Equal(want))
}

func recordSearchTagFilter(want []string) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(func(tags *recordingSearchTags) []string {
		return tags.seenFilter
	}, Equal(want))
}

func searchPageNode(pageID tree.PageID, title string, routePath tree.RoutePath) *tree.PageNode {
	ginkgo.GinkgoHelper()
	segments := routePath.Segments()
	var parent *tree.PageNode
	for _, segment := range segments[:len(segments)-1] {
		parent = &tree.PageNode{Title: segment.String(), Slug: segment, Kind: tree.NodeKindSection, Parent: parent}
	}
	return &tree.PageNode{
		ID:     pageID,
		Title:  title,
		Slug:   routePath.LeafSlug(),
		Kind:   tree.NodeKindPage,
		Parent: parent,
	}
}

func newFixtureSearchPageID[T ~string](raw T) tree.PageID {
	return tree.NewPageIDUnchecked(raw)
}

func newFixtureSearchRoutePath[T ~string](raw T) tree.RoutePath {
	return tree.NewRoutePathUnchecked(string(raw))
}

type searchRouteUseCases struct {
	Search         *SearchUseCase
	IndexingStatus *GetIndexingStatusUseCase
}

func matchSearchRouteUseCases(want searchRouteUseCases) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(searchRouteUseCasesFor, Equal(want))
}

func searchRouteUseCasesFor(routes *Routes) searchRouteUseCases {
	return searchRouteUseCases{
		Search:         routes.search,
		IndexingStatus: routes.getIndexingStatus,
	}
}

func matchSearchUseCaseDependencies(indexMatcher, tagsMatcher, treeMatcher types.GomegaMatcher) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return SatisfyAll(
		WithTransform(func(uc *SearchUseCase) any { return uc.index }, indexMatcher),
		WithTransform(func(uc *SearchUseCase) any { return uc.tags }, tagsMatcher),
		WithTransform(func(uc *SearchUseCase) any { return uc.tree }, treeMatcher),
	)
}
