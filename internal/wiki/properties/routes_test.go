package properties

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
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
	coreprop "github.com/perber/wiki/internal/properties"
	testmatchers "github.com/perber/wiki/internal/test_utils/matchers"
	sqlite "modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

func matchLocalizedPropertiesError(code sharederrors.ErrorCode) types.GomegaMatcher {
	return testmatchers.MatchLocalizedError(code, sharederrors.MessageIDForCode(code))
}

func matchPropertiesStructuredError(status int, code sharederrors.ErrorCode) types.GomegaMatcher {
	return testmatchers.HaveHTTPStructuredError(status, code, sharederrors.MessageIDForCode(code))
}

func matchPropertiesUseCaseSQLitePrimaryError(code int) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(func(err error) int {
		var sqliteErr *sqlite.Error
		if !errors.As(err, &sqliteErr) {
			return -1
		}
		return sqliteErr.Code() & 0xFF
	}, Equal(code))
}

func matchPropertyPageID(want tree.PageID) types.GomegaMatcher {
	return WithTransform(func(got string) tree.PageID {
		return tree.PageIDFromString(got)
	}, Equal(want))
}

func matchPropertyPage(wantID tree.PageID, title string, path string, properties types.GomegaMatcher) types.GomegaMatcher {
	return gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"ID":         matchPropertyPageID(wantID),
		"Title":      Equal(title),
		"Path":       Equal(path),
		"Properties": properties,
	})
}

func matchPropertyPagePointer(wantID tree.PageID, title string, path string, properties types.GomegaMatcher) types.GomegaMatcher {
	return gstruct.PointTo(matchPropertyPage(wantID, title, path, properties))
}

func tempPropertiesDataDir() string {
	ginkgo.GinkgoHelper()

	dataDir, err := os.MkdirTemp("", "leafwiki-properties-*")
	Expect(err).NotTo(HaveOccurred())
	ginkgo.DeferCleanup(os.RemoveAll, dataDir)
	return dataDir
}

var _ = ginkgo.Describe("properties routes", ginkgo.Label("integration"), func() {
	ginkgo.It("exposes property keys without authentication when public access is enabled", func() {
		svc := newPropertiesTestService()
		Expect(svc.SetPropertiesForPage(newFixturePageID("page-1"), map[string]coreprop.PropertyEntry{
			"status": {Value: "draft", Type: "text"},
		})).To(Succeed())

		router := httpinternal.NewRouter(
			[]httpinternal.RouteRegistrar{NewRoutes(RoutesConfig{
				GetPropertyKeys: NewGetPropertyKeysUseCase(svc),
			})},
			httpinternal.FrontendConfig{},
			httpinternal.RouterOptions{PublicAccess: true, DisableFrontendRoutes: true},
		)

		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/properties?limit=10", nil))

		Expect(rec).To(HaveHTTPStatus(http.StatusOK), rec.Body.String())
		var body []coreprop.PropertyKeyCount
		Expect(json.Unmarshal(rec.Body.Bytes(), &body)).To(Succeed())
		Expect(body).To(Equal([]coreprop.PropertyKeyCount{{Key: "status", Count: 1}}))
	})

	ginkgo.It("requires authentication for private property routes", func() {
		router := httpinternal.NewRouter(
			[]httpinternal.RouteRegistrar{NewRoutes(RoutesConfig{})},
			httpinternal.FrontendConfig{},
			httpinternal.RouterOptions{DisableFrontendRoutes: true},
		)

		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/properties", nil))

		Expect(rec).To(HaveHTTPStatus(http.StatusUnauthorized), rec.Body.String())
	})

	ginkgo.It("returns a structured bad request for invalid property key limits", func() {
		router := httpinternal.NewRouter(
			[]httpinternal.RouteRegistrar{NewRoutes(RoutesConfig{
				GetPropertyKeys: NewGetPropertyKeysUseCase(newPropertiesTestService()),
			})},
			httpinternal.FrontendConfig{},
			httpinternal.RouterOptions{PublicAccess: true, DisableFrontendRoutes: true},
		)

		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/properties?limit=bad", nil))

		Expect(rec).To(matchPropertiesStructuredError(http.StatusBadRequest, ErrCodePropertiesInvalidLimit), rec.Body.String())
	})

	ginkgo.It("returns pages matching a property through the public route", func() {
		fixture := newPropertiesPageFixture()
		router := httpinternal.NewRouter(
			[]httpinternal.RouteRegistrar{NewRoutes(RoutesConfig{
				GetPropertyKeys:    NewGetPropertyKeysUseCase(fixture.properties),
				GetPagesByProperty: NewGetPagesByPropertyUseCase(fixture.properties, fixture.tree, nil),
			})},
			httpinternal.FrontendConfig{},
			httpinternal.RouterOptions{PublicAccess: true, DisableFrontendRoutes: true},
		)

		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/properties/pages?key=status&value=draft", nil))

		Expect(rec).To(HaveHTTPStatus(http.StatusOK), rec.Body.String())
		var body []dto.PropertyPage
		Expect(json.Unmarshal(rec.Body.Bytes(), &body)).To(Succeed())
		Expect(body).To(ConsistOf(matchPropertyPage(
			fixture.pageID,
			"Draft Page",
			"draft",
			HaveKeyWithValue("status", dto.PropertyEntry{Value: "draft", Type: "text"}),
		)))
	})

	ginkgo.It("returns structured errors from the pages-by-property route", func() {
		fixture := newPropertiesPageFixture()
		router := httpinternal.NewRouter(
			[]httpinternal.RouteRegistrar{NewRoutes(RoutesConfig{
				GetPagesByProperty: NewGetPagesByPropertyUseCase(fixture.properties, fixture.tree, nil),
			})},
			httpinternal.FrontendConfig{},
			httpinternal.RouterOptions{PublicAccess: true, DisableFrontendRoutes: true},
		)

		missingKey := httptest.NewRecorder()
		router.ServeHTTP(missingKey, httptest.NewRequest(http.MethodGet, "/api/properties/pages?value=draft", nil))
		Expect(missingKey).To(matchPropertiesStructuredError(http.StatusBadRequest, ErrCodePropertiesMissingKey), missingKey.Body.String())

		missingValue := httptest.NewRecorder()
		router.ServeHTTP(missingValue, httptest.NewRequest(http.MethodGet, "/api/properties/pages?key=status", nil))
		Expect(missingValue).To(matchPropertiesStructuredError(http.StatusBadRequest, ErrCodePropertiesMissingValue), missingValue.Body.String())
	})
})

var _ = ginkgo.Describe("properties use cases", func() {
	ginkgo.It("normalizes key filter and page size before listing property keys", ginkgo.Label("unit"), func() {
		svc := newPropertiesTestService()
		Expect(svc.SetPropertiesForPage(newFixturePageID("page-1"), map[string]coreprop.PropertyEntry{
			"Status": {Value: "draft", Type: "text"},
			"stage":  {Value: "alpha", Type: "text"},
		})).To(Succeed())
		Expect(svc.SetPropertiesForPage(newFixturePageID("page-2"), map[string]coreprop.PropertyEntry{
			"Status": {Value: "published", Type: "text"},
		})).To(Succeed())
		uc := NewGetPropertyKeysUseCase(svc)

		out, err := uc.Execute(context.Background(), GetPropertyKeysInput{
			Filter:   " ST ",
			PageSize: 500,
		})

		Expect(err).NotTo(HaveOccurred())
		Expect(out.Keys).To(Equal([]coreprop.PropertyKeyCount{
			{Key: "Status", Count: 2},
			{Key: "stage", Count: 1},
		}))
	})

	ginkgo.It("defaults non-positive page size and normalizes nil key results to an empty slice", ginkgo.Label("unit"), func() {
		uc := NewGetPropertyKeysUseCase(newPropertiesTestService())

		out, err := uc.Execute(context.Background(), GetPropertyKeysInput{PageSize: 0})

		Expect(err).NotTo(HaveOccurred())
		Expect(out.Keys).To(BeEmpty())
	})

	ginkgo.It("returns localized errors for missing property page key and value", ginkgo.Label("unit"), func() {
		uc := NewGetPagesByPropertyUseCase(newPropertiesTestService(), nil, nil)

		out, err := uc.Execute(context.Background(), GetPagesByPropertyInput{Key: " ", Value: "draft"})
		Expect(out).To(BeNil())
		Expect(err).To(matchLocalizedPropertiesError(ErrCodePropertiesMissingKey))

		out, err = uc.Execute(context.Background(), GetPagesByPropertyInput{Key: "status", Value: " "})
		Expect(out).To(BeNil())
		Expect(err).To(matchLocalizedPropertiesError(ErrCodePropertiesMissingValue))
	})

	ginkgo.It("returns an empty property page slice when no pages match", ginkgo.Label("unit"), func() {
		uc := NewGetPagesByPropertyUseCase(newPropertiesTestService(), nil, nil)

		out, err := uc.Execute(context.Background(), GetPagesByPropertyInput{Key: "status", Value: "draft"})

		Expect(err).NotTo(HaveOccurred())
		Expect(out.Pages).To(BeEmpty())
	})

	ginkgo.It("returns backing store errors from property key and page lookup use cases", ginkgo.Label("unit"), func() {
		propertiesService, dataDir := newPropertiesTestServiceWithDataDir()
		Expect(propertiesService.SetPropertiesForPage(newFixturePageID("page-1"), map[string]coreprop.PropertyEntry{
			"status": {Value: "draft", Type: "text"},
		})).To(Succeed())
		dropPropertiesTable(dataDir)

		keys, err := NewGetPropertyKeysUseCase(propertiesService).Execute(context.Background(), GetPropertyKeysInput{PageSize: 10})
		Expect(keys).To(BeNil())
		Expect(err).To(matchPropertiesUseCaseSQLitePrimaryError(sqlite3.SQLITE_ERROR))

		pages, err := NewGetPagesByPropertyUseCase(propertiesService, nil, nil).Execute(context.Background(), GetPagesByPropertyInput{Key: "status", Value: "draft"})
		Expect(pages).To(BeNil())
		Expect(err).To(matchPropertiesUseCaseSQLitePrimaryError(sqlite3.SQLITE_ERROR))
	})

	ginkgo.It("returns property detail lookup errors after matching page IDs", ginkgo.Label("unit"), func() {
		propertiesService, dataDir := newPropertiesTestServiceWithDataDir()
		replacePropertiesTableWithoutType(dataDir)

		pages, err := NewGetPagesByPropertyUseCase(propertiesService, nil, nil).Execute(context.Background(), GetPagesByPropertyInput{Key: "status", Value: "draft"})

		Expect(pages).To(BeNil())
		Expect(err).To(matchPropertiesUseCaseSQLitePrimaryError(sqlite3.SQLITE_ERROR))
	})

	ginkgo.It("returns structured route errors when property key listing fails", ginkgo.Label("integration"), func() {
		propertiesService, dataDir := newPropertiesTestServiceWithDataDir()
		Expect(propertiesService.SetPropertiesForPage(newFixturePageID("page-1"), map[string]coreprop.PropertyEntry{
			"status": {Value: "draft", Type: "text"},
		})).To(Succeed())
		dropPropertiesTable(dataDir)
		router := httpinternal.NewRouter(
			[]httpinternal.RouteRegistrar{NewRoutes(RoutesConfig{
				GetPropertyKeys: NewGetPropertyKeysUseCase(propertiesService),
			})},
			httpinternal.FrontendConfig{},
			httpinternal.RouterOptions{PublicAccess: true, DisableFrontendRoutes: true},
		)

		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/properties", nil))

		Expect(rec).To(matchPropertiesStructuredError(http.StatusInternalServerError, ErrCodePropertiesInternal), rec.Body.String())
	})

	ginkgo.It("maps matching page IDs to property page DTOs and skips missing tree nodes", ginkgo.Label("unit"), func() {
		fixture := newPropertiesPageFixture()
		Expect(fixture.properties.SetPropertiesForPage(newFixturePageID("missing-page"), map[string]coreprop.PropertyEntry{
			"status": {Value: "draft", Type: "text"},
		})).To(Succeed())
		uc := NewGetPagesByPropertyUseCase(fixture.properties, fixture.tree, nil)

		out, err := uc.Execute(context.Background(), GetPagesByPropertyInput{Key: "status", Value: "draft"})

		Expect(err).NotTo(HaveOccurred())
		Expect(out.Pages).To(ConsistOf(matchPropertyPagePointer(
			fixture.pageID,
			"Draft Page",
			"draft",
			HaveKeyWithValue("status", dto.PropertyEntry{Value: "draft", Type: "text"}),
		)))
	})
})

var _ = ginkgo.Describe("properties error responses", func() {
	ginkgo.It("maps validation errors to bad request and unknown errors to internal", ginkgo.Label("unit"), func() {
		Expect(propertiesErrorStatus(ErrCodePropertiesMissingKey)).To(Equal(http.StatusBadRequest))
		Expect(propertiesErrorStatus(ErrCodePropertiesMissingValue)).To(Equal(http.StatusBadRequest))
		Expect(propertiesErrorStatus(ErrCodePropertiesInvalidLimit)).To(Equal(http.StatusBadRequest))
		Expect(propertiesErrorStatus(ErrCodePropertiesInternal)).To(Equal(http.StatusInternalServerError))
	})

	ginkgo.It("renders localized bad-request details", ginkgo.Label("integration"), func() {
		ctx, rec := ginTestContext()

		respondWithPropertiesBadRequest(ctx, ErrCodePropertiesInvalidLimit, "ignored", "ignored")

		Expect(rec).To(matchPropertiesStructuredError(http.StatusBadRequest, ErrCodePropertiesInvalidLimit), rec.Body.String())
	})

	ginkgo.It("renders localized errors with their mapped status", ginkgo.Label("integration"), func() {
		ctx, rec := ginTestContext()

		respondWithPropertiesError(ctx, ErrPropertiesMissingKey)

		Expect(rec).To(matchPropertiesStructuredError(http.StatusBadRequest, ErrCodePropertiesMissingKey), rec.Body.String())
	})

	ginkgo.It("sanitizes unknown errors as internal property failures", ginkgo.Label("integration"), func() {
		ctx, rec := ginTestContext()

		respondWithPropertiesError(ctx, errors.New("sqlite path leaked"))

		Expect(rec).To(matchPropertiesStructuredError(http.StatusInternalServerError, ErrCodePropertiesInternal), rec.Body.String())
	})
})

func newPropertiesTestService() *coreprop.PropertiesService {
	ginkgo.GinkgoHelper()
	service, _ := newPropertiesTestServiceWithDataDir()
	return service
}

func newPropertiesTestServiceWithDataDir() (*coreprop.PropertiesService, string) {
	ginkgo.GinkgoHelper()
	dataDir := tempPropertiesDataDir()
	store, err := coreprop.NewPropertiesStore(dataDir)
	Expect(err).NotTo(HaveOccurred())
	ginkgo.DeferCleanup(func() {
		Expect(store.Close()).To(Succeed())
	})
	return coreprop.NewPropertiesService(store), dataDir
}

type propertiesPageFixture struct {
	properties *coreprop.PropertiesService
	tree       *tree.TreeService
	pageID     tree.PageID
}

func newPropertiesPageFixture() propertiesPageFixture {
	ginkgo.GinkgoHelper()
	dataDir := tempPropertiesDataDir()
	Expect(os.WriteFile(
		dataDir+"/schema.json",
		[]byte(fmt.Sprintf(`{"version":%d}`, tree.CurrentSchemaVersion)),
		0o644,
	)).To(Succeed())
	treeService := tree.NewTreeService(dataDir)
	Expect(treeService.LoadTree()).To(Succeed())
	pageKind := tree.NodeKindPage
	pageID, err := treeService.CreateNode(newFixtureUserID("user-1"), nil, "Draft Page", newFixtureSlug("draft"), &pageKind)
	Expect(err).NotTo(HaveOccurred())
	propertiesService := newPropertiesTestService()
	Expect(propertiesService.SetPropertiesForPage(*pageID, map[string]coreprop.PropertyEntry{
		"status": {Value: "draft", Type: "text"},
		"owner":  {Value: "docs", Type: "text"},
	})).To(Succeed())

	return propertiesPageFixture{properties: propertiesService, tree: treeService, pageID: *pageID}
}

func dropPropertiesTable(dataDir string) {
	ginkgo.GinkgoHelper()
	db, err := sql.Open("sqlite", filepath.Join(dataDir, "properties.db"))
	Expect(err).NotTo(HaveOccurred())
	ginkgo.DeferCleanup(func() {
		Expect(db.Close()).To(Succeed())
	})
	_, err = db.Exec("DROP TABLE page_properties")
	Expect(err).NotTo(HaveOccurred())
}

func replacePropertiesTableWithoutType(dataDir string) {
	ginkgo.GinkgoHelper()
	db, err := sql.Open("sqlite", filepath.Join(dataDir, "properties.db"))
	Expect(err).NotTo(HaveOccurred())
	ginkgo.DeferCleanup(func() {
		Expect(db.Close()).To(Succeed())
	})
	_, err = db.Exec(`
		DROP TABLE page_properties;
		CREATE TABLE page_properties (
			page_id TEXT NOT NULL,
			key     TEXT NOT NULL,
			value   TEXT NOT NULL
		);
		INSERT INTO page_properties (page_id, key, value) VALUES ('page-1', 'status', 'draft');
	`)
	Expect(err).NotTo(HaveOccurred())
}

func newFixturePageID[T ~string](raw T) tree.PageID {
	return tree.NewPageIDUnchecked(raw)
}

func newFixtureUserID[T ~string](raw T) tree.UserID {
	return tree.NewUserIDUnchecked(string(raw))
}

func newFixtureSlug[T ~string](raw T) tree.Slug {
	return tree.NewSlugUnchecked(raw)
}

func ginTestContext() (*gin.Context, *httptest.ResponseRecorder) {
	ginkgo.GinkgoHelper()
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	return ctx, rec
}
