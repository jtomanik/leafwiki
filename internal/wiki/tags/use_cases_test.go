package tags

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"

	"github.com/gin-gonic/gin"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/core/tree"
	httpinternal "github.com/perber/wiki/internal/http"
	"github.com/perber/wiki/internal/http/dto"
	coretags "github.com/perber/wiki/internal/tags"
	testmatchers "github.com/perber/wiki/internal/test_utils/matchers"
	sqlite "modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

func matchTagsStructuredError(status int, code sharederrors.ErrorCode) types.GomegaMatcher {
	return testmatchers.HaveHTTPStructuredError(status, code, sharederrors.MessageIDForCode(code))
}

func matchTaggedPageID(want tree.PageID) types.GomegaMatcher {
	return WithTransform(func(got string) tree.PageID {
		return tree.PageIDFromString(got)
	}, Equal(want))
}

func matchTaggedPageWithID(want tree.PageID) types.GomegaMatcher {
	return gstruct.PointTo(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"ID": matchTaggedPageID(want),
	}))
}

func matchTaggedPageWithExcerpt(excerpt string) types.GomegaMatcher {
	return gstruct.PointTo(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"Excerpt": Equal(excerpt),
	}))
}

func matchTaggedPageWithTags(tags ...string) types.GomegaMatcher {
	return gstruct.PointTo(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"Tags": matchTagSet(tags...),
	}))
}

func matchTaggedPageDTO(want tree.PageID, tags ...string) types.GomegaMatcher {
	return gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"ID":   matchTaggedPageID(want),
		"Tags": matchTagSet(tags...),
	})
}

func matchTagSet(tags ...string) types.GomegaMatcher {
	elements := make([]any, 0, len(tags))
	for _, tag := range tags {
		elements = append(elements, tag)
	}
	return ConsistOf(elements...)
}

func matchTagsUseCaseSQLitePrimaryError(code int) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(func(err error) int {
		var sqliteErr *sqlite.Error
		if !errors.As(err, &sqliteErr) {
			return -1
		}
		return sqliteErr.Code() & 0xFF
	}, Equal(code))
}

// ─── test helpers ─────────────────────────────────────────────────────────────

func setupUseCases() (*GetPagesByTagsUseCase, *coretags.TagsService, *tree.TreeService) {
	ginkgo.GinkgoHelper()
	uc, svc, ts, _ := setupUseCasesWithDataDir()
	return uc, svc, ts
}

func setupUseCasesWithDataDir() (*GetPagesByTagsUseCase, *coretags.TagsService, *tree.TreeService, string) {
	ginkgo.GinkgoHelper()
	dir := newTagsTempDir()
	ts := tree.NewTreeService(dir)
	Expect(ts.LoadTree()).To(Succeed())

	store, err := coretags.NewTagsStore(dir)
	Expect(err).NotTo(HaveOccurred())
	ginkgo.DeferCleanup(func() {
		Expect(store.Close()).To(Succeed())
	})

	svc := coretags.NewTagsService(store)
	uc := NewGetPagesByTagsUseCase(svc, ts, nil)
	return uc, svc, ts, dir
}

func createAndIndexPage(ts *tree.TreeService, svc *coretags.TagsService, title, slug string, tags []string, body string) tree.PageID {
	ginkgo.GinkgoHelper()
	kind := tree.NodeKindPage
	idPtr, err := ts.CreateNode("system", nil, title, newFixtureSlug(slug), &kind)
	Expect(err).NotTo(HaveOccurred())

	fm := "---\ntags:\n"
	for _, tag := range tags {
		fm += "  - " + tag + "\n"
	}
	fm += "---\n\n" + body

	Expect(ts.UpdateNodeUncheckedVersion(newFixtureUserID("system"), *idPtr, title, newFixtureSlug(slug), &fm, true)).To(Succeed())

	raw, err := ts.ReadPageRaw(*idPtr)
	Expect(err).NotTo(HaveOccurred())
	Expect(svc.IndexPageContent(*idPtr, raw)).To(Succeed())

	return *idPtr
}

// ─── GetPagesByTagsUseCase ─────────────────────────────────────────────────────

var _ = ginkgo.Describe("tagged page lookup", func() {
	ginkgo.It("returns pages that match a requested tag", func() {
		uc, svc, ts := setupUseCases()

		id1 := createAndIndexPage(ts, svc, "React Guide", "react-guide", []string{"react", "frontend"}, "React guide body.")
		createAndIndexPage(ts, svc, "Go Handbook", "go-handbook", []string{"go", "backend"}, "Go handbook body.")

		out, err := uc.Execute(context.Background(), GetPagesByTagsInput{Tags: []string{"react"}})

		Expect(err).NotTo(HaveOccurred())
		Expect(out.Pages).To(ConsistOf(matchTaggedPageWithID(id1)))
	})

	ginkgo.It("returns indexed excerpts for matching pages", func() {
		uc, svc, ts := setupUseCases()

		createAndIndexPage(ts, svc, "Excerpt Page", "excerpt-page", []string{"docs"}, "This is the excerpt content.")

		out, err := uc.Execute(context.Background(), GetPagesByTagsInput{Tags: []string{"docs"}})

		Expect(err).NotTo(HaveOccurred())
		Expect(out.Pages).To(ConsistOf(matchTaggedPageWithExcerpt("This is the excerpt content.")))
	})

	ginkgo.It("requires every requested tag to match", func() {
		uc, svc, ts := setupUseCases()

		id1 := createAndIndexPage(ts, svc, "Both Tags", "both", []string{"react", "typescript"}, "body")
		createAndIndexPage(ts, svc, "Only React", "only-react", []string{"react"}, "body")

		out, err := uc.Execute(context.Background(), GetPagesByTagsInput{Tags: []string{"react", "typescript"}})

		Expect(err).NotTo(HaveOccurred())
		Expect(out.Pages).To(ConsistOf(matchTaggedPageWithID(id1)))
	})

	ginkgo.It("returns no pages when no tags are requested", func() {
		uc, _, _ := setupUseCases()

		out, err := uc.Execute(context.Background(), GetPagesByTagsInput{Tags: []string{}})

		Expect(err).NotTo(HaveOccurred())
		Expect(out.Pages).To(BeEmpty())
	})

	ginkgo.It("normalizes query tags before matching pages", func() {
		uc, svc, ts := setupUseCases()

		createAndIndexPage(ts, svc, "Go Page", "go-page", []string{"go"}, "body")

		out, err := uc.Execute(context.Background(), GetPagesByTagsInput{Tags: []string{"GO", " go "}})

		Expect(err).NotTo(HaveOccurred())
		Expect(out.Pages).To(HaveLen(1))
	})

	ginkgo.It("returns no pages when no indexed page matches", func() {
		uc, svc, ts := setupUseCases()

		createAndIndexPage(ts, svc, "Go Page", "go-page", []string{"go"}, "body")

		out, err := uc.Execute(context.Background(), GetPagesByTagsInput{Tags: []string{"rust"}})

		Expect(err).NotTo(HaveOccurred())
		Expect(out.Pages).To(BeEmpty())
	})

	ginkgo.It("includes indexed tag metadata in each matching page", func() {
		uc, svc, ts := setupUseCases()

		createAndIndexPage(ts, svc, "Multi Tag", "multi", []string{"go", "testing", "backend"}, "body")

		out, err := uc.Execute(context.Background(), GetPagesByTagsInput{Tags: []string{"go"}})

		Expect(err).NotTo(HaveOccurred())
		Expect(out.Pages).To(ConsistOf(matchTaggedPageWithTags("go", "testing", "backend")))
	})

	ginkgo.It("skips indexed pages that are missing from the tree", func() {
		uc, svc, _ := setupUseCases()
		Expect(svc.IndexPageContent(newFixturePageID("missing-page"), "---\ntags:\n  - go\n---\n\norphaned excerpt")).To(Succeed())

		out, err := uc.Execute(context.Background(), GetPagesByTagsInput{Tags: []string{"go"}})

		Expect(err).NotTo(HaveOccurred())
		Expect(out.Pages).To(BeEmpty())
	})

	ginkgo.It("returns tag lookup errors from the backing store", func() {
		uc, svc, _, dataDir := setupUseCasesWithDataDir()
		createAndIndexPage(uc.treeService, svc, "Go Page", "go-page", []string{"go"}, "body")
		dropTagTables(dataDir, "DROP TABLE page_tags")

		out, err := uc.Execute(context.Background(), GetPagesByTagsInput{Tags: []string{"go"}})

		Expect(out).To(BeNil())
		Expect(err).To(matchTagsUseCaseSQLitePrimaryError(sqlite3.SQLITE_ERROR))
	})

	ginkgo.It("returns tag metadata lookup errors after matching page IDs", func() {
		tagsErr := errors.New("tags lookup failed")
		uc := &GetPagesByTagsUseCase{
			svc: pageTagsServiceStub{
				pageIDs: []tree.PageID{newFixturePageID("page-1")},
				tagsErr: tagsErr,
			},
		}

		out, err := uc.Execute(context.Background(), GetPagesByTagsInput{Tags: []string{"go"}})

		Expect(out).To(BeNil())
		Expect(err).To(MatchError(tagsErr))
	})

	ginkgo.It("returns excerpt lookup errors from the backing store", func() {
		uc, svc, _, dataDir := setupUseCasesWithDataDir()
		createAndIndexPage(uc.treeService, svc, "Go Page", "go-page", []string{"go"}, "body")
		dropTagTables(dataDir, "DROP TABLE page_meta")

		out, err := uc.Execute(context.Background(), GetPagesByTagsInput{Tags: []string{"go"}})

		Expect(out).To(BeNil())
		Expect(err).To(matchTagsUseCaseSQLitePrimaryError(sqlite3.SQLITE_ERROR))
	})
})

var _ = ginkgo.Describe("tags API boundary helpers", func() {
	ginkgo.It("validates page tag input by trimming, lowercasing, and deduplicating tags", func() {
		got, err := ValidatePagesByTagsInput([]string{" GO ", "go", "", "React"})

		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(Equal([]string{"go", "react"}))
	})

	ginkgo.It("returns a localized missing-param error for empty page tag input", func() {
		got, err := ValidatePagesByTagsInput([]string{" ", ""})

		Expect(got).To(BeNil())
		Expect(err).To(testmatchers.MatchLocalizedError(ErrCodeTagsMissingParam, sharederrors.MessageIDForCode(ErrCodeTagsMissingParam)))
	})

	ginkgo.It("lists tags with filter, selection normalization, and page-size clamping", func() {
		_, svc, _ := setupUseCases()
		Expect(svc.SetTagsForPage("page-1", []string{"go", "react"})).To(Succeed())
		Expect(svc.SetTagsForPage("page-2", []string{"go", "rust"})).To(Succeed())
		Expect(svc.SetTagsForPage("page-3", []string{"go", "react", "testing"})).To(Succeed())
		uc := NewGetTagsUseCase(svc)

		out, err := uc.Execute(context.Background(), GetTagsInput{
			Filter:   " RE ",
			Selected: []string{" GO ", "go"},
			PageSize: 500,
		})

		Expect(err).NotTo(HaveOccurred())
		Expect(out.Tags).To(Equal([]coretags.TagCount{{Tag: "react", Count: 2}}))
	})

	ginkgo.It("returns an empty tag list instead of nil when no tags match", func() {
		_, svc, _ := setupUseCases()
		uc := NewGetTagsUseCase(svc)

		out, err := uc.Execute(context.Background(), GetTagsInput{Filter: "missing"})

		Expect(err).NotTo(HaveOccurred())
		Expect(out.Tags).To(BeEmpty())
	})

	ginkgo.It("splits comma-separated query tag values and drops empty parts", func() {
		Expect(splitTags(" go, react, ,testing ")).To(Equal([]string{"go", "react", "testing"}))
	})

	ginkgo.It("combines repeated query tags with comma-separated values", func() {
		ctx := ginContextForTarget("/api/tags/pages?tags=go,react&tags=testing")

		Expect(queryTags(ctx, "tags")).To(Equal([]string{"go", "react", "testing"}))
	})
})

var _ = ginkgo.Describe("tags routes", func() {
	ginkgo.It("serves tag counts through the public route", func() {
		_, svc, _ := setupUseCases()
		Expect(svc.SetTagsForPage("page-1", []string{"go", "react"})).To(Succeed())
		Expect(svc.SetTagsForPage("page-2", []string{"go", "testing"})).To(Succeed())
		router := newTagsTestRouter(RoutesConfig{
			GetTags: NewGetTagsUseCase(svc),
		}, httpinternal.RouterOptions{PublicAccess: true})

		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/tags?q=g&limit=10", nil))

		Expect(rec).To(HaveHTTPStatus(http.StatusOK), rec.Body.String())
		var body []coretags.TagCount
		Expect(json.Unmarshal(rec.Body.Bytes(), &body)).To(Succeed())
		Expect(body).To(Equal([]coretags.TagCount{{Tag: "go", Count: 2}}))
	})

	ginkgo.It("serves tag suggestions filtered by selected tags", func() {
		_, svc, _ := setupUseCases()
		Expect(svc.SetTagsForPage("page-1", []string{"go", "react"})).To(Succeed())
		Expect(svc.SetTagsForPage("page-2", []string{"go", "rust"})).To(Succeed())
		router := newTagsTestRouter(RoutesConfig{
			GetTags: NewGetTagsUseCase(svc),
		}, httpinternal.RouterOptions{PublicAccess: true})

		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/tags?selected=go&r=ignored&selected=react,rust&limit=10", nil))

		Expect(rec).To(HaveHTTPStatus(http.StatusOK), rec.Body.String())
		var body []coretags.TagCount
		Expect(json.Unmarshal(rec.Body.Bytes(), &body)).To(Succeed())
		Expect(body).To(BeEmpty())
	})

	ginkgo.It("serves pages matching tags through the public route", func() {
		uc, svc, ts := setupUseCases()
		pageID := createAndIndexPage(ts, svc, "Go Guide", "go-guide", []string{"go", "testing"}, "Guide body.")
		createAndIndexPage(ts, svc, "React Guide", "react-guide", []string{"react"}, "React body.")
		router := newTagsTestRouter(RoutesConfig{
			GetPagesByTags: uc,
		}, httpinternal.RouterOptions{PublicAccess: true})

		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/tags/pages?tags=go,testing", nil))

		Expect(rec).To(HaveHTTPStatus(http.StatusOK), rec.Body.String())
		var body []dto.TaggedPage
		Expect(json.Unmarshal(rec.Body.Bytes(), &body)).To(Succeed())
		Expect(body).To(ConsistOf(matchTaggedPageDTO(pageID, "go", "testing")))
	})

	ginkgo.It("returns structured route errors for invalid tag queries", func() {
		uc, _, _ := setupUseCases()
		router := newTagsTestRouter(RoutesConfig{
			GetTags:        NewGetTagsUseCase(coretags.NewTagsService(mustNewTagsStore())),
			GetPagesByTags: uc,
		}, httpinternal.RouterOptions{PublicAccess: true})

		invalidLimit := httptest.NewRecorder()
		router.ServeHTTP(invalidLimit, httptest.NewRequest(http.MethodGet, "/api/tags?limit=bad", nil))
		Expect(invalidLimit).To(matchTagsStructuredError(http.StatusBadRequest, ErrCodeTagsInvalidLimit), invalidLimit.Body.String())

		missingTags := httptest.NewRecorder()
		router.ServeHTTP(missingTags, httptest.NewRequest(http.MethodGet, "/api/tags/pages", nil))
		Expect(missingTags).To(matchTagsStructuredError(http.StatusBadRequest, ErrCodeTagsMissingParam), missingTags.Body.String())
	})

	ginkgo.It("returns structured route errors when tag services fail", func() {
		_, svc, _, dataDir := setupUseCasesWithDataDir()
		Expect(svc.SetTagsForPage("page-1", []string{"go"})).To(Succeed())
		dropTagTables(dataDir, "DROP TABLE page_tags")
		router := newTagsTestRouter(RoutesConfig{
			GetTags: NewGetTagsUseCase(svc),
		}, httpinternal.RouterOptions{PublicAccess: true})

		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/tags", nil))

		Expect(rec).To(matchTagsStructuredError(http.StatusInternalServerError, ErrCodeTagsInternal), rec.Body.String())
	})

	ginkgo.It("returns structured route errors when page tag services fail", func() {
		uc, svc, _, dataDir := setupUseCasesWithDataDir()
		createAndIndexPage(uc.treeService, svc, "Go Page", "go-page", []string{"go"}, "body")
		dropTagTables(dataDir, "DROP TABLE page_meta")
		router := newTagsTestRouter(RoutesConfig{
			GetPagesByTags: uc,
		}, httpinternal.RouterOptions{PublicAccess: true})

		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/tags/pages?tags=go", nil))

		Expect(rec).To(matchTagsStructuredError(http.StatusInternalServerError, ErrCodeTagsInternal), rec.Body.String())
	})

	ginkgo.It("requires authentication for private tag routes", func() {
		router := newTagsTestRouter(RoutesConfig{}, httpinternal.RouterOptions{})

		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/tags", nil))

		Expect(rec).To(HaveHTTPStatus(http.StatusUnauthorized), rec.Body.String())
	})
})

var _ = ginkgo.Describe("tags error responses", func() {
	ginkgo.It("maps known tag validation errors to bad request", func() {
		Expect(tagsErrorStatus(ErrCodeTagsMissingParam)).To(Equal(http.StatusBadRequest))
		Expect(tagsErrorStatus(ErrCodeTagsInvalidLimit)).To(Equal(http.StatusBadRequest))
		Expect(tagsErrorStatus(ErrCodeTagsInternal)).To(Equal(http.StatusInternalServerError))
	})

	ginkgo.It("renders localized bad-request details", func() {
		ctx, rec := ginTestContext()

		respondWithTagsBadRequest(ctx, ErrCodeTagsInvalidLimit, "ignored", "ignored")

		Expect(rec).To(matchTagsStructuredError(http.StatusBadRequest, ErrCodeTagsInvalidLimit), rec.Body.String())
	})

	ginkgo.It("renders localized errors with their mapped status", func() {
		ctx, rec := ginTestContext()

		respondWithTagsError(ctx, sharederrors.NewLocalizedErrorFromCode(ErrCodeTagsMissingParam, nil))

		Expect(rec).To(matchTagsStructuredError(http.StatusBadRequest, ErrCodeTagsMissingParam), rec.Body.String())
	})

	ginkgo.It("sanitizes unknown errors as internal tag failures", func() {
		ctx, rec := ginTestContext()

		respondWithTagsError(ctx, context.Canceled)

		Expect(rec).To(matchTagsStructuredError(http.StatusInternalServerError, ErrCodeTagsInternal), rec.Body.String())
	})
})

func ginTestContext() (*gin.Context, *httptest.ResponseRecorder) {
	ginkgo.GinkgoHelper()
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	return ctx, rec
}

func newTagsTestRouter(cfg RoutesConfig, opts httpinternal.RouterOptions) http.Handler {
	ginkgo.GinkgoHelper()
	opts.DisableFrontendRoutes = true
	return httpinternal.NewRouter(
		[]httpinternal.RouteRegistrar{NewRoutes(cfg)},
		httpinternal.FrontendConfig{},
		opts,
	)
}

func mustNewTagsStore() *coretags.TagsStore {
	ginkgo.GinkgoHelper()
	store, err := coretags.NewTagsStore(newTagsTempDir())
	Expect(err).NotTo(HaveOccurred())
	ginkgo.DeferCleanup(func() {
		Expect(store.Close()).To(Succeed())
	})
	return store
}

func newTagsTempDir() string {
	ginkgo.GinkgoHelper()
	dir, err := os.MkdirTemp("", "leafwiki-tags-*")
	Expect(err).NotTo(HaveOccurred())
	ginkgo.DeferCleanup(os.RemoveAll, dir)
	return dir
}

func dropTagTables(dataDir string, statements ...string) {
	ginkgo.GinkgoHelper()
	db, err := sql.Open("sqlite", filepath.Join(dataDir, "tags.db"))
	Expect(err).NotTo(HaveOccurred())
	ginkgo.DeferCleanup(func() {
		Expect(db.Close()).To(Succeed())
	})

	for _, statement := range statements {
		_, err := db.Exec(statement)
		Expect(err).NotTo(HaveOccurred())
	}
}

func ginContextForTarget(target string) *gin.Context {
	ginkgo.GinkgoHelper()
	ctx, _ := ginTestContext()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	ctx.Request = req
	return ctx
}

type pageTagsServiceStub struct {
	pageIDs []tree.PageID
	tagsErr error
}

func (s pageTagsServiceStub) GetAllTags(string, coretags.TagLimit) ([]coretags.TagCount, error) {
	return nil, nil
}

func (s pageTagsServiceStub) GetAllTagsForSelection(string, []string, coretags.TagLimit) ([]coretags.TagCount, error) {
	return nil, nil
}

func (s pageTagsServiceStub) GetPageIDsByTags([]string) ([]tree.PageID, error) {
	return s.pageIDs, nil
}

func (s pageTagsServiceStub) GetTagsForPages([]tree.PageID) (map[tree.PageID][]string, error) {
	return nil, s.tagsErr
}

func (s pageTagsServiceStub) GetExcerptsForPages([]tree.PageID) (map[tree.PageID]string, error) {
	return nil, nil
}
