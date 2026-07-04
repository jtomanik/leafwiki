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
	"github.com/onsi/gomega/types"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	httpinternal "github.com/perber/wiki/internal/http"
	testmatchers "github.com/perber/wiki/internal/test_utils/matchers"
)

var _ = ginkgo.Describe("importer routes", func() {
	ginkgo.It("requires authentication before reading import plans", ginkgo.Label("integration"), func() {
		router := httpinternal.NewRouter(
			[]httpinternal.RouteRegistrar{NewRoutes(RoutesConfig{})},
			httpinternal.FrontendConfig{},
			httpinternal.RouterOptions{DisableFrontendRoutes: true},
		)

		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/import/plan", nil))

		Expect(rec).To(HaveHTTPStatus(http.StatusUnauthorized), rec.Body.String())
	})

	ginkgo.It("requires CSRF protection before mutating import plans", ginkgo.Label("integration"), func() {
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

		Expect(rec).To(HaveHTTPStatus(http.StatusForbidden), rec.Body.String())
	})
})

var _ = ginkgo.Describe("importer error responses", func() {
	ginkgo.It("maps importer error codes to HTTP statuses", ginkgo.Label("unit"), func() {
		Expect(importerErrorStatus(ErrCodeImporterNoPlan)).To(Equal(http.StatusNotFound))
		Expect(importerErrorStatus(ErrCodeImporterExecutionRunning)).To(Equal(http.StatusConflict))
		Expect(importerErrorStatus(ErrCodeImporterStateUnavailable)).To(Equal(http.StatusInternalServerError))
		Expect(importerErrorStatus(ErrCodeImporterUploadTooLarge)).To(Equal(http.StatusRequestEntityTooLarge))
		Expect(importerErrorStatus(ErrCodeImporterMissingFile)).To(Equal(http.StatusBadRequest))
		Expect(importerErrorStatus(ErrCodeImporterFileOpenFailed)).To(Equal(http.StatusBadRequest))
		Expect(importerErrorStatus(ErrCodeImporterInternalError)).To(Equal(http.StatusInternalServerError))
	})

	ginkgo.It("renders explicit status errors as structured localized responses", ginkgo.Label("integration"), func() {
		ctx, rec := ginTestContext()

		respondWithImporterStatusError(ctx, http.StatusBadRequest, ErrCodeImporterMissingFile, "ignored", "ignored")

		Expect(rec).To(haveImporterStructuredError(http.StatusBadRequest, ErrCodeImporterMissingFile), rec.Body.String())
	})

	ginkgo.It("renders localized importer errors with their mapped status", ginkgo.Label("integration"), func() {
		ctx, rec := ginTestContext()

		respondWithImporterError(ctx, sharederrors.NewLocalizedErrorFromCode(ErrCodeImporterNoPlan, nil))

		Expect(rec).To(haveImporterStructuredError(http.StatusNotFound, ErrCodeImporterNoPlan), rec.Body.String())
	})

	ginkgo.It("sanitizes unknown importer errors as internal failures", ginkgo.Label("integration"), func() {
		ctx, rec := ginTestContext()

		respondWithImporterError(ctx, errors.New("zip path /tmp/private failed"))

		Expect(rec).To(haveImporterStructuredError(http.StatusInternalServerError, ErrCodeImporterInternalError), rec.Body.String())
		expectedBody, err := json.Marshal(ImporterErrorResponse{
			Error: sharederrors.NewLocalizedErrorDetailFromCode(ErrCodeImporterInternalError),
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(rec).To(HaveHTTPBody(MatchJSON(expectedBody)))
	})
})

func ginTestContext() (*gin.Context, *httptest.ResponseRecorder) {
	ginkgo.GinkgoHelper()
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	return ctx, rec
}

func haveImporterStructuredError(status int, code sharederrors.ErrorCode) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return testmatchers.HaveHTTPStructuredError(status, code, sharederrors.MessageIDForCode(code))
}
