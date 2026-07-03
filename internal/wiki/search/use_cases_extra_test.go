package search

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"
	"github.com/perber/wiki/internal/core/tree"
	httpinternal "github.com/perber/wiki/internal/http"
	coresearch "github.com/perber/wiki/internal/search"
	coretags "github.com/perber/wiki/internal/tags"
	sqlite "modernc.org/sqlite"
)

var _ = ginkgo.Describe("search execution", func() {
	ginkgo.It("searches indexed pages and attaches tags and facets", func() {
		fixture := newSearchFixture()
		goID := fixture.createIndexedPage("Go Guide", "go-guide", []string{"go", "docs"}, "A guide about Go testing.")
		fixture.createIndexedPage("React Notes", "react-notes", []string{"react", "docs"}, "Frontend notes.")

		out, err := fixture.useCase.Execute(context.Background(), SearchInput{
			Query:    "guide",
			StartAt:  0,
			PageSize: 10,
		})

		Expect(err).NotTo(HaveOccurred())
		Expect(out.Result).To(matchSearchResult(gstruct.Fields{
			"Count": Equal(1),
			"Items": HaveExactElements(matchSearchResultItem(gstruct.Fields{
				"PageID": Equal(goID),
				"Tags":   ConsistOf("go", "docs"),
			})),
			"TagFacets": ConsistOf(
				coresearch.SearchTagFacet{Tag: "go", Count: 1},
				coresearch.SearchTagFacet{Tag: "docs", Count: 1},
			),
		}))
	})

	ginkgo.It("intersects query results with normalized tag filters", func() {
		fixture := newSearchFixture()
		goID := fixture.createIndexedPage("Go Guide", "go-guide", []string{"go", "docs"}, "A guide about Go testing.")
		fixture.createIndexedPage("React Guide", "react-guide", []string{"react", "docs"}, "A guide about React testing.")

		out, err := fixture.useCase.Execute(context.Background(), SearchInput{
			Query:    "guide",
			Tags:     []string{" GO ", "go"},
			StartAt:  0,
			PageSize: 10,
		})

		Expect(err).NotTo(HaveOccurred())
		Expect(out.Result).To(matchSearchResult(gstruct.Fields{
			"Count": Equal(1),
			"Items": HaveExactElements(matchSearchResultItem(gstruct.Fields{
				"PageID": Equal(goID),
			})),
		}))
	})

	ginkgo.It("returns sorted and paged tag-only results from the tree", func() {
		fixture := newSearchFixture()
		fixture.createIndexedPage("Beta Page", "beta", []string{"go"}, "Beta body.")
		alphaID := fixture.createIndexedPage("Alpha Page", "alpha", []string{"go", "docs"}, "Alpha body.")
		missingID := fixture.createIndexedPage("Missing Tree Page", "missing-tree", []string{"go"}, "Missing body.")
		Expect(fixture.tree.DeleteNodeUncheckedVersion(tree.UserIDFromString("system"), missingID, false)).To(Succeed())

		out, err := fixture.useCase.Execute(context.Background(), SearchInput{
			Tags:     []string{"go"},
			StartAt:  -5,
			PageSize: 1,
		})

		Expect(err).NotTo(HaveOccurred())
		Expect(out.Result).To(matchSearchResult(gstruct.Fields{
			"Count":    Equal(2),
			"StartAt":  Equal(coresearch.ResultOffset(0)),
			"PageSize": Equal(coresearch.ResultLimit(1)),
			"Items": HaveExactElements(matchSearchResultItem(gstruct.Fields{
				"PageID": Equal(alphaID),
				"Title":  Equal("Alpha Page"),
				"Tags":   ConsistOf("go", "docs"),
			})),
			"TagFacets": ConsistOf(
				coresearch.SearchTagFacet{Tag: "go", Count: 3},
				coresearch.SearchTagFacet{Tag: "docs", Count: 1},
			),
		}))
	})

	ginkgo.It("sorts equal tag-only titles by path and applies the default page size", func() {
		fixture := newSearchFixture()
		firstID := fixture.createIndexedPage("Same Title", "a-page", []string{"go"}, "First body.")
		fixture.createIndexedPage("Same Title", "b-page", []string{"go"}, "Second body.")

		out, err := fixture.useCase.Execute(context.Background(), SearchInput{
			Tags:     []string{"go"},
			StartAt:  0,
			PageSize: 0,
		})

		Expect(err).NotTo(HaveOccurred())
		Expect(out.Result).To(matchSearchResult(gstruct.Fields{
			"Count":    Equal(2),
			"PageSize": Equal(coresearch.ResultLimit(20)),
			"Items": HaveExactElements(
				matchSearchResultItem(gstruct.Fields{"PageID": Equal(firstID)}),
				gstruct.Ignore(),
			),
		}))
	})

	ginkgo.It("bounds tag-only result offsets past the end", func() {
		fixture := newSearchFixture()
		fixture.createIndexedPage("Only Page", "only", []string{"go"}, "body")

		out, err := fixture.useCase.Execute(context.Background(), SearchInput{
			Tags:     []string{"go"},
			StartAt:  20,
			PageSize: 10,
		})

		Expect(err).NotTo(HaveOccurred())
		Expect(out.Result).To(matchSearchResult(gstruct.Fields{
			"Count": Equal(1),
			"Items": BeEmpty(),
		}))
	})

	ginkgo.It("returns tag lookup errors before searching", func() {
		fixture := newSearchFixture()
		fixture.createIndexedPage("Go Guide", "go-guide", []string{"go"}, "body")
		dropSearchFixtureTable(fixture.dataDir, "tags.db", "DROP TABLE page_tags")

		out, err := fixture.useCase.Execute(context.Background(), SearchInput{
			Query:    "guide",
			Tags:     []string{"go"},
			PageSize: 10,
		})

		Expect(out).To(BeNil())
		Expect(err).To(matchSQLiteSearchFailure())
	})

	ginkgo.It("returns tag-only excerpt lookup errors", func() {
		fixture := newSearchFixture()
		fixture.createIndexedPage("Go Guide", "go-guide", []string{"go"}, "body")
		dropSearchFixtureTable(fixture.dataDir, "tags.db", "DROP TABLE page_meta")

		out, err := fixture.useCase.Execute(context.Background(), SearchInput{
			Tags:     []string{"go"},
			PageSize: 10,
		})

		Expect(out).To(BeNil())
		Expect(err).To(matchSQLiteSearchFailure())
	})

	ginkgo.It("returns index search errors", func() {
		fixture := newSearchFixture()
		fixture.createIndexedPage("Go Guide", "go-guide", []string{"go"}, "body")
		dropSearchFixtureTable(fixture.dataDir, "search.db", "DROP TABLE pages")

		out, err := fixture.useCase.Execute(context.Background(), SearchInput{
			Query:    "guide",
			PageSize: 10,
		})

		Expect(out).To(BeNil())
		Expect(err).To(matchSQLiteSearchFailure())
	})

	ginkgo.It("returns full-match page ID lookup errors after searching", func() {
		lookupErr := errors.New("page ID lookup failed")
		uc := &SearchUseCase{
			index: fakeSearchIndex{
				result: &coresearch.SearchResult{
					Items: []coresearch.SearchResultItem{{
						PageID: tree.PageIDFromString("page-1"),
						Title:  "Page",
					}},
				},
				pageIDsErr: lookupErr,
			},
		}

		out, err := uc.Execute(context.Background(), SearchInput{Query: "page", PageSize: 10})

		Expect(out).To(BeNil())
		Expect(err).To(MatchError(lookupErr))
	})

	ginkgo.It("omits tags and facets when tag attachment lookups fail", func() {
		fixture := newSearchFixture()
		fixture.createIndexedPage("Go Guide", "go-guide", []string{"go"}, "body")
		dropSearchFixtureTable(fixture.dataDir, "tags.db", "DROP TABLE page_tags")

		out, err := fixture.useCase.Execute(context.Background(), SearchInput{
			Query:    "guide",
			PageSize: 10,
		})

		Expect(err).NotTo(HaveOccurred())
		Expect(out.Result).To(matchSearchResult(gstruct.Fields{
			"Items": HaveExactElements(matchSearchResultItem(gstruct.Fields{
				"Tags": BeNil(),
			})),
			"TagFacets": BeEmpty(),
		}))
	})

	ginkgo.It("handles empty tag attachment inputs", func() {
		fixture := newSearchFixture()

		fixture.useCase.attachTags(nil)
		items := []coresearch.SearchResultItem{{Title: "No ID"}}
		fixture.useCase.attachTags(items)

		Expect(items).To(HaveExactElements(matchSearchResultItem(gstruct.Fields{
			"Tags": BeNil(),
		})))
		Expect(fixture.useCase.buildTagFacets(nil)).To(BeEmpty())
	})

	ginkgo.It("sets empty tags when an attached result has no tag entry", func() {
		fixture := newSearchFixture()
		items := []coresearch.SearchResultItem{{PageID: tree.PageIDFromString("missing"), Title: "Missing"}}

		fixture.useCase.attachTags(items)

		Expect(items).To(HaveExactElements(matchSearchResultItem(gstruct.Fields{
			"Tags": BeEmpty(),
		})))
	})
})

var _ = ginkgo.Describe("search routes", func() {
	ginkgo.It("serves public search results and indexing status", func() {
		fixture := newSearchFixture()
		fixture.createIndexedPage("Go Guide", "go-guide", []string{"go"}, "A guide about Go.")
		status := coresearch.NewIndexingStatus()
		status.Start()
		status.Success()
		router := newSearchTestRouter(RoutesConfig{
			Search:            fixture.useCase,
			GetIndexingStatus: NewGetIndexingStatusUseCase(status),
		}, httpinternal.RouterOptions{PublicAccess: true})

		searchRec := httptest.NewRecorder()
		router.ServeHTTP(searchRec, httptest.NewRequest(http.MethodGet, "/api/search?q=guide&tags=go&offset=0&limit=10", nil))
		Expect(searchRec).To(HaveHTTPStatus(http.StatusOK), searchRec.Body.String())
		var result coresearch.SearchResult
		Expect(json.Unmarshal(searchRec.Body.Bytes(), &result)).To(Succeed())
		Expect(result.Items).To(HaveLen(1))

		statusRec := httptest.NewRecorder()
		router.ServeHTTP(statusRec, httptest.NewRequest(http.MethodGet, "/api/search/status", nil))
		Expect(statusRec).To(HaveHTTPStatus(http.StatusOK), statusRec.Body.String())
		var statusBody coresearch.IndexingStatus
		Expect(json.Unmarshal(statusRec.Body.Bytes(), &statusBody)).To(Succeed())
		Expect(statusBody.Indexed).To(Equal(1))
	})

	ginkgo.It("returns structured validation errors from the public route", func() {
		router := newSearchTestRouter(RoutesConfig{
			Search: NewSearchUseCase(nil, nil, nil),
		}, httpinternal.RouterOptions{PublicAccess: true})

		missing := httptest.NewRecorder()
		router.ServeHTTP(missing, httptest.NewRequest(http.MethodGet, "/api/search", nil))
		Expect(missing).To(matchSearchStructuredError(http.StatusBadRequest, ErrCodeSearchMissingQuery))

		badOffset := httptest.NewRecorder()
		router.ServeHTTP(badOffset, httptest.NewRequest(http.MethodGet, "/api/search?q=docs&offset=bad", nil))
		Expect(badOffset).To(matchSearchStructuredError(http.StatusBadRequest, ErrCodeSearchInvalidOffset))

		badLimit := httptest.NewRecorder()
		router.ServeHTTP(badLimit, httptest.NewRequest(http.MethodGet, "/api/search?q=docs&limit=bad", nil))
		Expect(badLimit).To(matchSearchStructuredError(http.StatusBadRequest, ErrCodeSearchInvalidLimit))
	})

	ginkgo.It("returns structured search errors from the public route", func() {
		router := newSearchTestRouter(RoutesConfig{
			Search: NewSearchUseCase(nil, nil, nil),
		}, httpinternal.RouterOptions{PublicAccess: true})

		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/search?q=docs", nil))

		Expect(rec).To(matchSearchStructuredError(http.StatusServiceUnavailable, ErrCodeSearchUnavailable))
	})

	ginkgo.It("requires authentication for private search routes", func() {
		router := newSearchTestRouter(RoutesConfig{}, httpinternal.RouterOptions{})

		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/search/status", nil))

		Expect(rec).To(HaveHTTPStatus(http.StatusUnauthorized), rec.Body.String())
	})

	ginkgo.It("returns nil query tags when the key is absent", func() {
		ctx := ginContextForTarget("/api/search?q=docs")

		Expect(queryTags(ctx, "tags")).To(BeNil())
	})
})

func tempSearchDataDir() string {
	ginkgo.GinkgoHelper()

	dataDir, err := os.MkdirTemp("", "leafwiki-search-*")
	Expect(err).NotTo(HaveOccurred())
	ginkgo.DeferCleanup(os.RemoveAll, dataDir)
	return dataDir
}

type searchFixture struct {
	dataDir string
	tree    *tree.TreeService
	index   *coresearch.SQLiteIndex
	tags    *coretags.TagsService
	useCase *SearchUseCase
}

func newSearchFixture() *searchFixture {
	ginkgo.GinkgoHelper()
	dataDir := tempSearchDataDir()
	treeService := tree.NewTreeService(dataDir)
	Expect(treeService.LoadTree()).To(Succeed())

	index, err := coresearch.NewSQLiteIndex(dataDir)
	Expect(err).NotTo(HaveOccurred())
	ginkgo.DeferCleanup(func() {
		Expect(index.Close()).To(Succeed())
	})

	tagStore, err := coretags.NewTagsStore(dataDir)
	Expect(err).NotTo(HaveOccurred())
	ginkgo.DeferCleanup(func() {
		Expect(tagStore.Close()).To(Succeed())
	})

	tagService := coretags.NewTagsService(tagStore)
	return &searchFixture{
		dataDir: dataDir,
		tree:    treeService,
		index:   index,
		tags:    tagService,
		useCase: NewSearchUseCase(index, tagService, treeService),
	}
}

func (fixture *searchFixture) createIndexedPage(title string, slug string, tags []string, body string) tree.PageID {
	ginkgo.GinkgoHelper()
	kind := tree.NodeKindPage
	id, err := fixture.tree.CreateNode(tree.UserIDFromString("system"), nil, title, tree.SlugFromString(slug), &kind)
	Expect(err).NotTo(HaveOccurred())

	raw := "---\ntags:\n"
	for _, tag := range tags {
		raw += "  - " + tag + "\n"
	}
	raw += "---\n\n# " + title + "\n\n" + body

	Expect(fixture.tree.UpdateNodeUncheckedVersion(tree.UserIDFromString("system"), *id, title, tree.SlugFromString(slug), &raw, true)).To(Succeed())
	Expect(fixture.index.IndexPage("/"+slug, slug+".md", *id, title, kind, raw)).To(Succeed())
	Expect(fixture.tags.IndexPageContent(*id, raw)).To(Succeed())
	return *id
}

func dropSearchFixtureTable(dataDir string, dbName string, statement string) {
	ginkgo.GinkgoHelper()
	db, err := sql.Open("sqlite", filepath.Join(dataDir, dbName))
	Expect(err).NotTo(HaveOccurred())
	ginkgo.DeferCleanup(func() {
		Expect(db.Close()).To(Succeed())
	})
	_, err = db.Exec(statement)
	Expect(err).NotTo(HaveOccurred())
}

func newSearchTestRouter(cfg RoutesConfig, opts httpinternal.RouterOptions) http.Handler {
	ginkgo.GinkgoHelper()
	opts.DisableFrontendRoutes = true
	return httpinternal.NewRouter(
		[]httpinternal.RouteRegistrar{NewRoutes(cfg)},
		httpinternal.FrontendConfig{},
		opts,
	)
}

type fakeSearchIndex struct {
	result     *coresearch.SearchResult
	searchErr  error
	pageIDs    []tree.PageID
	pageIDsErr error
}

func (f fakeSearchIndex) Search(string, []tree.PageID, coresearch.ResultOffset, coresearch.ResultLimit) (*coresearch.SearchResult, error) {
	return f.result, f.searchErr
}

func (f fakeSearchIndex) SearchPageIDs(string, []tree.PageID) ([]tree.PageID, error) {
	return f.pageIDs, f.pageIDsErr
}

func matchSearchResult(fields gstruct.Fields) types.GomegaMatcher {
	return gstruct.PointTo(gstruct.MatchFields(gstruct.IgnoreExtras, fields))
}

func matchSearchResultItem(fields gstruct.Fields) types.GomegaMatcher {
	return gstruct.MatchFields(gstruct.IgnoreExtras, fields)
}

func matchSQLiteSearchFailure() types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return Satisfy(func(err error) bool {
		var sqliteErr *sqlite.Error
		return errors.As(err, &sqliteErr)
	})
}
