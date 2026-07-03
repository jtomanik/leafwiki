package branding

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corebranding "github.com/perber/wiki/internal/branding"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/core/tree"
	httpinternal "github.com/perber/wiki/internal/http"
	testmatchers "github.com/perber/wiki/internal/test_utils/matchers"
)

var _ = ginkgo.Describe("branding error responses", func() {
	ginkgo.It("renders validation errors with field metadata", func() {
		ctx, rec := ginTestContext()

		ve := sharederrors.NewValidationErrors()
		ve.Add(brandingSiteNameValidationField.String(), "site name is required")

		respondWithBrandingError(ctx, ve)

		Expect(rec).To(HaveBrandingValidationError(brandingSiteNameValidationField, sharederrors.FieldValidationErrorCode, sharederrors.FieldValidationErrorMessageID))
	})

	ginkgo.It("preserves localized branding error identity", func() {
		ctx, rec := ginTestContext()

		err := sharederrors.NewLocalizedError(
			ErrCodeBrandingLogoInvalidType,
			"Invalid logo file type",
			"invalid logo file type %s (allowed: %s)",
			nil,
			".exe",
			".png, .svg",
		)

		respondWithBrandingError(ctx, err)

		Expect(rec).To(testmatchers.HaveHTTPStructuredError(http.StatusBadRequest, ErrCodeBrandingLogoInvalidType, sharederrors.MessageIDForCode(ErrCodeBrandingLogoInvalidType)))
	})

	ginkgo.It("sanitizes unexpected internal error details", func() {
		ctx, rec := ginTestContext()

		respondWithBrandingError(ctx, errors.New("write config: permission denied"))

		Expect(rec).To(testmatchers.HaveHTTPStructuredError(http.StatusInternalServerError, ErrCodeBrandingInternalError, sharederrors.MessageIDForCode(ErrCodeBrandingInternalError)))
		Expect(rec).NotTo(HaveHTTPBody(ContainSubstring("permission denied")))
	})

	ginkgo.It("maps branding error codes to HTTP statuses", func() {
		Expect(brandingErrorStatus(ErrCodeBrandingInvalidPayload)).To(Equal(http.StatusBadRequest))
		Expect(brandingErrorStatus(ErrCodeBrandingLogoMissing)).To(Equal(http.StatusBadRequest))
		Expect(brandingErrorStatus(ErrCodeBrandingFaviconMissing)).To(Equal(http.StatusBadRequest))
		Expect(brandingErrorStatus(ErrCodeBrandingLogoInvalidType)).To(Equal(http.StatusBadRequest))
		Expect(brandingErrorStatus(ErrCodeBrandingFaviconInvalidType)).To(Equal(http.StatusBadRequest))
		Expect(brandingErrorStatus(ErrCodeBrandingLogoTooLarge)).To(Equal(http.StatusRequestEntityTooLarge))
		Expect(brandingErrorStatus(ErrCodeBrandingFaviconTooLarge)).To(Equal(http.StatusRequestEntityTooLarge))
		Expect(brandingErrorStatus(ErrCodeBrandingConfigUnavailable)).To(Equal(http.StatusInternalServerError))
	})

	ginkgo.It("renders explicit status errors as structured localized responses", func() {
		ctx, rec := ginTestContext()

		respondWithBrandingStatusError(ctx, http.StatusRequestEntityTooLarge, ErrCodeBrandingLogoTooLarge, "ignored", "ignored")

		Expect(rec).To(testmatchers.HaveHTTPStructuredError(http.StatusRequestEntityTooLarge, ErrCodeBrandingLogoTooLarge, sharederrors.MessageIDForCode(ErrCodeBrandingLogoTooLarge)))
	})
})

var _ = ginkgo.Describe("branding routes", func() {
	ginkgo.It("serves public branding configuration", func() {
		svc := newBrandingTestService()
		router := httpinternal.NewRouter(
			[]httpinternal.RouteRegistrar{NewRoutes(RoutesConfig{
				GetBranding:     NewGetBrandingUseCase(svc),
				BrandingService: svc,
				Log:             slog.Default(),
			})},
			httpinternal.FrontendConfig{},
			httpinternal.RouterOptions{DisableFrontendRoutes: true},
		)

		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/branding", nil))

		Expect(rec).To(HaveHTTPStatus(http.StatusOK), rec.Body.String())
		var body corebranding.BrandingConfigResponse
		Expect(json.Unmarshal(rec.Body.Bytes(), &body)).To(Succeed())
		Expect(body.SiteName).To(Equal("LeafWiki"))
		Expect(body.BrandingConstraints.MaxSiteNameLength).To(Equal(100))
	})

	ginkgo.It("rejects invalid update payload with a localized structured response", func() {
		svc := newBrandingTestService()
		routes := NewRoutes(RoutesConfig{
			GetBranding:     NewGetBrandingUseCase(svc),
			UpdateBranding:  NewUpdateBrandingUseCase(svc),
			BrandingService: svc,
			Log:             slog.Default(),
		})
		ctx, rec := ginTestContext()
		ctx.Request = httptest.NewRequest(http.MethodPut, "/api/branding", strings.NewReader(`{`))
		ctx.Request.Header.Set("Content-Type", "application/json")

		routes.handleUpdateBranding(ctx)

		Expect(rec).To(testmatchers.HaveHTTPStructuredError(http.StatusBadRequest, ErrCodeBrandingInvalidPayload, sharederrors.MessageIDForCode(ErrCodeBrandingInvalidPayload)))
	})
})

var _ = ginkgo.Describe("branding asset paths", func() {
	ginkgo.It("rejects traversal and invalid extensions", func() {
		svc := newBrandingTestService()
		routes := NewRoutes(RoutesConfig{BrandingService: svc, Log: slog.Default()})
		cfg, err := svc.GetBranding()
		Expect(err).NotTo(HaveOccurred())

		_, status := routes.resolveBrandingAssetPath(tree.AssetNameFromString("../logo.png"), cfg)
		Expect(status).To(Equal(http.StatusForbidden))

		_, status = routes.resolveBrandingAssetPath(tree.AssetNameFromString("logo.exe"), cfg)
		Expect(status).To(Equal(http.StatusForbidden))
	})

	ginkgo.It("returns not found for allowed missing assets and ok for existing assets", func() {
		svc := newBrandingTestService()
		routes := NewRoutes(RoutesConfig{BrandingService: svc, Log: slog.Default()})
		cfg, err := svc.GetBranding()
		Expect(err).NotTo(HaveOccurred())

		_, status := routes.resolveBrandingAssetPath(tree.AssetNameFromString("logo.png"), cfg)
		Expect(status).To(Equal(http.StatusNotFound))

		path := filepath.Join(svc.GetBrandingAssetsDir(), "logo.png")
		Expect(os.WriteFile(path, []byte("png"), 0o600)).To(Succeed())
		got, status := routes.resolveBrandingAssetPath(tree.AssetNameFromString("logo.png"), cfg)
		Expect(status).To(Equal(http.StatusOK))
		Expect(got).To(Equal(path))
	})
})

func newBrandingTestService() *corebranding.BrandingService {
	ginkgo.GinkgoHelper()
	svc, err := corebranding.NewBrandingService(newBrandingTempDir())
	Expect(err).NotTo(HaveOccurred())
	return svc
}

func newBrandingTempDir() string {
	ginkgo.GinkgoHelper()
	dir, err := os.MkdirTemp("", "leafwiki-branding-*")
	Expect(err).NotTo(HaveOccurred())
	ginkgo.DeferCleanup(os.RemoveAll, dir)
	return dir
}

func ginTestContext() (*gin.Context, *httptest.ResponseRecorder) {
	ginkgo.GinkgoHelper()
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	return ctx, rec
}
