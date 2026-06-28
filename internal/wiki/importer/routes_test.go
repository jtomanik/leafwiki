package importer

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/gin-gonic/gin"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	httpinternal "github.com/perber/wiki/internal/http"
)

var _ = ginkgo.Describe("importer routes", func() {
	ginkgo.It("TestRoutesRequireAuthForImportPlanReads", func() {
		router := httpinternal.NewRouter(
			[]httpinternal.RouteRegistrar{NewRoutes(RoutesConfig{})},
			httpinternal.FrontendConfig{},
			httpinternal.RouterOptions{DisableFrontendRoutes: true},
		)

		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/import/plan", nil))

		Expect(rec.Code).To(Equal(http.StatusUnauthorized), rec.Body.String())
	})

	ginkgo.It("TestRoutesRequireCSRFForImportPlanMutations", func() {
		router := httpinternal.NewRouter(
			[]httpinternal.RouteRegistrar{NewRoutes(RoutesConfig{})},
			httpinternal.FrontendConfig{},
			httpinternal.RouterOptions{
				AuthDisabled:          true,
				DisableFrontendRoutes: true,
			},
		)

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/import/plan", strings.NewReader(""))
		req.Header.Set("Content-Type", "multipart/form-data; boundary=missing")
		router.ServeHTTP(rec, req)

		Expect(rec.Code).To(Equal(http.StatusForbidden), rec.Body.String())
	})
})

var _ = ginkgo.Describe("importer error responses", func() {
	ginkgo.It("maps importer error codes to HTTP statuses", func() {
		Expect(importerErrorStatus(ErrCodeImporterNoPlan)).To(Equal(http.StatusNotFound))
		Expect(importerErrorStatus(ErrCodeImporterExecutionRunning)).To(Equal(http.StatusConflict))
		Expect(importerErrorStatus(ErrCodeImporterStateUnavailable)).To(Equal(http.StatusInternalServerError))
		Expect(importerErrorStatus(ErrCodeImporterUploadTooLarge)).To(Equal(http.StatusRequestEntityTooLarge))
		Expect(importerErrorStatus(ErrCodeImporterMissingFile)).To(Equal(http.StatusBadRequest))
		Expect(importerErrorStatus(ErrCodeImporterFileOpenFailed)).To(Equal(http.StatusBadRequest))
		Expect(importerErrorStatus(ErrCodeImporterInternalError)).To(Equal(http.StatusInternalServerError))
	})

	ginkgo.It("renders explicit status errors as structured localized responses", func() {
		ctx, rec := ginTestContext()

		respondWithImporterStatusError(ctx, http.StatusBadRequest, ErrCodeImporterMissingFile, "ignored", "ignored")

		Expect(rec.Code).To(Equal(http.StatusBadRequest))
		assertImporterStructuredError(rec, "importer_missing_file", "errors.importer.missing_file")
	})

	ginkgo.It("renders localized importer errors with their mapped status", func() {
		ctx, rec := ginTestContext()

		respondWithImporterError(ctx, sharederrors.NewLocalizedErrorFromCode(ErrCodeImporterNoPlan, nil))

		Expect(rec.Code).To(Equal(http.StatusNotFound))
		assertImporterStructuredError(rec, "importer_no_plan", "errors.importer.no_plan")
	})

	ginkgo.It("sanitizes unknown importer errors as internal failures", func() {
		ctx, rec := ginTestContext()

		respondWithImporterError(ctx, errors.New("zip path /tmp/private failed"))

		Expect(rec.Code).To(Equal(http.StatusInternalServerError))
		assertImporterStructuredError(rec, "importer_internal_error", "errors.importer.internal_error")
		Expect(rec.Body.String()).NotTo(ContainSubstring("/tmp/private"))
	})
})

func ginTestContext() (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	return ctx, rec
}

func assertImporterStructuredError(rec *httptest.ResponseRecorder, code string, messageID string) {
	var body ImporterErrorResponse
	Expect(json.Unmarshal(rec.Body.Bytes(), &body)).To(Succeed(), rec.Body.String())
	Expect(body.Error.Code.String()).To(Equal(code))
	Expect(body.Error.MessageID.String()).To(Equal(messageID))
}
