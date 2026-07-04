package importer

import (
	"errors"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/gin-gonic/gin"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	coreimporter "github.com/perber/wiki/internal/importer"
)

var _ = ginkgo.Describe("importer route handlers", ginkgo.Label("integration"), func() {
	ginkgo.It("serves current plan state through the authenticated route", func() {
		svc, store := newImporterServiceFixture()
		seedImporterPlan(store, coreimporter.ExecutionStatusPlanned)
		router := newImporterTestRouter(RoutesConfig{
			GetPlan: NewGetImportPlanUseCase(svc),
		})

		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/import/plan", nil))

		Expect(rec).To(HaveHTTPStatus(http.StatusOK), rec.Body.String())
		var body coreimporter.CurrentPlanState
		Expect(jsonUnmarshalImporterResponse(rec, &body)).To(Succeed())
		Expect(body.ID).To(Equal("plan-1"))
	})

	ginkgo.It("returns structured route errors when no plan exists", func() {
		svc, _ := newImporterServiceFixture()
		router := newImporterTestRouter(RoutesConfig{
			GetPlan: NewGetImportPlanUseCase(svc),
			Execute: NewExecuteImportUseCase(svc),
		})

		getPlan := httptest.NewRecorder()
		router.ServeHTTP(getPlan, httptest.NewRequest(http.MethodGet, "/api/import/plan", nil))
		Expect(getPlan).To(haveImporterStructuredError(http.StatusNotFound, ErrCodeImporterNoPlan), getPlan.Body.String())

		execute := performImporterCSRFRequest(router, http.MethodPost, "/api/import/execute", nil, "")
		Expect(execute).To(haveImporterStructuredError(http.StatusNotFound, ErrCodeImporterNoPlan), execute.Body.String())
	})

	ginkgo.It("creates plans from multipart uploads", func() {
		svc, _ := newImporterServiceFixture()
		router := newImporterTestRouter(RoutesConfig{
			CreatePlan: NewCreateImportPlanUseCase(svc),
			Svc:        svc,
			Log:        slog.Default(),
		})
		body, contentType := importerMultipartBody(importerZipBytes("Imported.md", "# Imported\nbody"), "docs")

		rec := performImporterCSRFRequest(router, http.MethodPost, "/api/import/plan", body, contentType)

		Expect(rec).To(HaveHTTPStatus(http.StatusOK), rec.Body.String())
		var plan coreimporter.CurrentPlanState
		Expect(jsonUnmarshalImporterResponse(rec, &plan)).To(Succeed())
		Expect(plan).To(haveImporterPlanWithItemCount(coreimporter.ExecutionStatusPlanned, 1))
	})

	ginkgo.It("returns structured create-plan request errors", func() {
		svc, _ := newImporterServiceFixture()
		router := newImporterTestRouter(RoutesConfig{
			CreatePlan: NewCreateImportPlanUseCase(svc),
			Log:        slog.Default(),
		})

		malformed := performImporterCSRFRequest(router, http.MethodPost, "/api/import/plan", strings.NewReader("bad multipart"), "multipart/form-data; boundary=missing")
		Expect(malformed).To(haveImporterStructuredError(http.StatusRequestEntityTooLarge, ErrCodeImporterUploadTooLarge), malformed.Body.String())

		body, contentType := importerMultipartBody(nil, "")
		missingFile := performImporterCSRFRequest(router, http.MethodPost, "/api/import/plan", body, contentType)
		Expect(missingFile).To(haveImporterStructuredError(http.StatusBadRequest, ErrCodeImporterMissingFile), missingFile.Body.String())

		invalidZip, invalidZipContentType := importerMultipartBody([]byte("not a zip"), "")
		failedPlan := performImporterCSRFRequest(router, http.MethodPost, "/api/import/plan", invalidZip, invalidZipContentType)
		Expect(failedPlan).To(haveImporterStructuredError(http.StatusInternalServerError, ErrCodeImporterInternalError), failedPlan.Body.String())
	})

	ginkgo.It("returns structured file-open errors from multipart uploads", func() {
		previousOpen := openImporterUploadFile
		openErr := errors.New("open failed")
		openImporterUploadFile = func(*multipart.FileHeader) (multipart.File, error) {
			return nil, openErr
		}
		ginkgo.DeferCleanup(func() {
			openImporterUploadFile = previousOpen
		})
		svc, _ := newImporterServiceFixture()
		router := newImporterTestRouter(RoutesConfig{
			CreatePlan: NewCreateImportPlanUseCase(svc),
			Log:        slog.Default(),
		})
		body, contentType := importerMultipartBody(importerZipBytes("Imported.md", "# Imported\nbody"), "")

		rec := performImporterCSRFRequest(router, http.MethodPost, "/api/import/plan", body, contentType)

		Expect(rec).To(haveImporterStructuredError(http.StatusBadRequest, ErrCodeImporterFileOpenFailed), rec.Body.String())
	})

	ginkgo.It("logs close errors after successful multipart uploads", func() {
		previousClose := closeImporterUploadFile
		closeImporterUploadFile = func(multipart.File) error {
			return errors.New("close failed")
		}
		ginkgo.DeferCleanup(func() {
			closeImporterUploadFile = previousClose
		})
		svc, _ := newImporterServiceFixture()
		router := newImporterTestRouter(RoutesConfig{
			CreatePlan: NewCreateImportPlanUseCase(svc),
			Svc:        svc,
			Log:        slog.Default(),
		})
		body, contentType := importerMultipartBody(importerZipBytes("Imported.md", "# Imported\nbody"), "docs")

		rec := performImporterCSRFRequest(router, http.MethodPost, "/api/import/plan", body, contentType)

		Expect(rec).To(HaveHTTPStatus(http.StatusOK), rec.Body.String())
	})

	ginkgo.It("executes planned imports with accepted status and completed imports with ok status", func() {
		svc, store := newImporterServiceFixture()
		router := newImporterTestRouter(RoutesConfig{
			Execute: NewExecuteImportUseCase(svc),
		})

		seedImporterPlan(store, coreimporter.ExecutionStatusPlanned)
		accepted := performImporterCSRFRequest(router, http.MethodPost, "/api/import/execute", nil, "")
		Expect(accepted).To(HaveHTTPStatus(http.StatusAccepted), accepted.Body.String())

		seedImporterPlan(store, coreimporter.ExecutionStatusCompleted)
		ok := performImporterCSRFRequest(router, http.MethodPost, "/api/import/execute", nil, "")
		Expect(ok).To(HaveHTTPStatus(http.StatusOK), ok.Body.String())
	})

	ginkgo.It("clears planned imports and accepts cancellation for running imports", func() {
		svc, store := newImporterServiceFixture()
		router := newImporterTestRouter(RoutesConfig{
			ClearPlan: NewClearImportPlanUseCase(svc),
		})

		seedImporterPlan(store, coreimporter.ExecutionStatusPlanned)
		cleared := performImporterCSRFRequest(router, http.MethodDelete, "/api/import/plan", nil, "")
		Expect(cleared).To(HaveHTTPStatus(http.StatusOK), cleared.Body.String())
		Expect(cleared).To(HaveHTTPBody(MatchJSON("null")))

		seedImporterPlan(store, coreimporter.ExecutionStatusRunning)
		canceling := performImporterCSRFRequest(router, http.MethodDelete, "/api/import/plan", nil, "")
		Expect(canceling).To(HaveHTTPStatus(http.StatusAccepted), canceling.Body.String())
		var state coreimporter.CurrentPlanState
		Expect(jsonUnmarshalImporterResponse(canceling, &state)).To(Succeed())
		Expect(&state).To(haveImporterPlanWithCancellation(coreimporter.ExecutionStatusRunning))
	})

	ginkgo.It("returns structured clear-plan errors", func() {
		svc := newImporterServiceWithStore(importerUnavailablePlanStore())
		router := newImporterTestRouter(RoutesConfig{
			ClearPlan: NewClearImportPlanUseCase(svc),
		})

		rec := performImporterCSRFRequest(router, http.MethodDelete, "/api/import/plan", nil, "")

		Expect(rec).To(haveImporterStructuredError(http.StatusInternalServerError, ErrCodeImporterStateUnavailable), rec.Body.String())
	})

	ginkgo.It("forbids direct handler calls when user context is missing", func() {
		routes := NewRoutes(RoutesConfig{})
		router := gin.New()
		router.POST("/import/plan", routes.handleCreatePlan)
		router.POST("/import/execute", routes.handleExecute)

		createPlan := httptest.NewRecorder()
		router.ServeHTTP(createPlan, httptest.NewRequest(http.MethodPost, "/import/plan", nil))
		Expect(createPlan).To(HaveHTTPStatus(http.StatusForbidden), createPlan.Body.String())

		execute := httptest.NewRecorder()
		router.ServeHTTP(execute, httptest.NewRequest(http.MethodPost, "/import/execute", nil))
		Expect(execute).To(HaveHTTPStatus(http.StatusForbidden), execute.Body.String())
	})
})
