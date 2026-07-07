package branding

import (
	"encoding/json"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/gin-gonic/gin"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"

	corebranding "github.com/perber/wiki/internal/branding"
)

type unitBrandingService struct {
	config    *corebranding.BrandingConfigResponse
	assetsDir string

	getErr           error
	updateErr        error
	uploadLogoErr    error
	deleteLogoErr    error
	uploadFaviconErr error
	deleteFaviconErr error

	seenSiteName        string
	seenLogoFilename    string
	seenFaviconFilename string
}

func newUnitBrandingService() *unitBrandingService {
	return &unitBrandingService{
		config:    corebranding.DefaultBrandingConfig().ToResponse(),
		assetsDir: newBrandingTempDir(),
	}
}

func (svc *unitBrandingService) GetBranding() (*corebranding.BrandingConfigResponse, error) {
	if svc.getErr != nil {
		return nil, svc.getErr
	}
	return svc.config, nil
}

func (svc *unitBrandingService) UpdateBranding(siteName string) error {
	svc.seenSiteName = siteName
	if svc.updateErr != nil {
		return svc.updateErr
	}
	svc.config.SiteName = strings.TrimSpace(siteName)
	return nil
}

func (svc *unitBrandingService) UploadLogo(_ multipart.File, filename string) (string, error) {
	svc.seenLogoFilename = filename
	if svc.uploadLogoErr != nil {
		return "", svc.uploadLogoErr
	}
	svc.config.LogoFile = filename
	return filename, nil
}

func (svc *unitBrandingService) DeleteLogo() error {
	if svc.deleteLogoErr != nil {
		return svc.deleteLogoErr
	}
	svc.config.LogoFile = ""
	return nil
}

func (svc *unitBrandingService) UploadFavicon(_ multipart.File, filename string) (string, error) {
	svc.seenFaviconFilename = filename
	if svc.uploadFaviconErr != nil {
		return "", svc.uploadFaviconErr
	}
	svc.config.FaviconFile = filename
	return filename, nil
}

func (svc *unitBrandingService) DeleteFavicon() error {
	if svc.deleteFaviconErr != nil {
		return svc.deleteFaviconErr
	}
	svc.config.FaviconFile = ""
	return nil
}

func (svc *unitBrandingService) GetBrandingAssetsDir() string {
	return svc.assetsDir
}

func newUnitBrandingRoutes(svc *unitBrandingService) *Routes {
	ginkgo.GinkgoHelper()
	return &Routes{
		getBranding:     &GetBrandingUseCase{branding: svc},
		updateBranding:  &UpdateBrandingUseCase{branding: svc},
		uploadLogo:      &UploadLogoUseCase{branding: svc},
		deleteLogo:      &DeleteLogoUseCase{branding: svc},
		uploadFavicon:   &UploadFaviconUseCase{branding: svc},
		deleteFavicon:   &DeleteFaviconUseCase{branding: svc},
		brandingService: svc,
		log:             discardBrandingLog(),
	}
}

func newBrandingUnitContext(method string, target string, body string) (*gin.Context, *httptest.ResponseRecorder) {
	ginkgo.GinkgoHelper()

	ctx, rec := ginTestContext()
	ctx.Request = httptest.NewRequest(method, target, strings.NewReader(body))
	return ctx, rec
}

func newBrandingMultipartUnitContext(target string, filename string, contents []byte) (*gin.Context, *httptest.ResponseRecorder) {
	ginkgo.GinkgoHelper()

	body, contentType := brandingMultipartBody(filename, contents)
	ctx, rec := ginTestContext()
	ctx.Request = httptest.NewRequest(http.MethodPost, target, body)
	ctx.Request.Header.Set("Content-Type", contentType)
	return ctx, rec
}

func brandingRegisteredRoutes(engine *gin.Engine) []string {
	routes := engine.Routes()
	out := make([]string, 0, len(routes))
	for _, route := range routes {
		out = append(out, route.Method+" "+route.Path)
	}
	return out
}

func exposeBrandingRouteContract() types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return ConsistOf(
		"GET /api/branding",
		"GET /branding/:filename",
		"GET /favicon.ico",
		"PUT /api/branding",
		"POST /api/branding/logo",
		"POST /api/branding/favicon",
		"DELETE /api/branding/logo",
		"DELETE /api/branding/favicon",
	)
}

type brandingUploadObservation struct {
	LogoFilename    string
	FaviconFilename string
}

func recordBrandingSiteName(siteName string) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(func(svc *unitBrandingService) string {
		return svc.seenSiteName
	}, Equal(siteName))
}

func recordBrandingUpload(want brandingUploadObservation) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(func(svc *unitBrandingService) brandingUploadObservation {
		return brandingUploadObservation{
			LogoFilename:    svc.seenLogoFilename,
			FaviconFilename: svc.seenFaviconFilename,
		}
	}, Equal(want))
}

func decodeBrandingConfig(rec *httptest.ResponseRecorder) *corebranding.BrandingConfigResponse {
	ginkgo.GinkgoHelper()

	var body corebranding.BrandingConfigResponse
	Expect(json.Unmarshal(rec.Body.Bytes(), &body)).To(Succeed(), rec.Body.String())
	return &body
}

type brandingAssetResponse struct {
	Path     string                              `json:"path"`
	Branding corebranding.BrandingConfigResponse `json:"branding"`
}

func decodeBrandingAssetResponse(rec *httptest.ResponseRecorder) brandingAssetResponse {
	ginkgo.GinkgoHelper()

	var body brandingAssetResponse
	Expect(json.Unmarshal(rec.Body.Bytes(), &body)).To(Succeed(), rec.Body.String())
	return body
}

func matchBrandingConfig(siteName string, logoFile string, faviconFile string) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return gstruct.PointTo(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"SiteName":    Equal(siteName),
		"LogoFile":    Equal(logoFile),
		"FaviconFile": Equal(faviconFile),
	}))
}

type brandingRouteUseCases struct {
	GetBranding    *GetBrandingUseCase
	UpdateBranding *UpdateBrandingUseCase
	UploadLogo     *UploadLogoUseCase
	DeleteLogo     *DeleteLogoUseCase
	UploadFavicon  *UploadFaviconUseCase
	DeleteFavicon  *DeleteFaviconUseCase
}

func matchBrandingRouteUseCases(want brandingRouteUseCases) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(brandingRouteUseCasesFor, Equal(want))
}

func brandingRouteUseCasesFor(routes *Routes) brandingRouteUseCases {
	if routes == nil {
		return brandingRouteUseCases{}
	}
	return brandingRouteUseCases{
		GetBranding:    routes.getBranding,
		UpdateBranding: routes.updateBranding,
		UploadLogo:     routes.uploadLogo,
		DeleteLogo:     routes.deleteLogo,
		UploadFavicon:  routes.uploadFavicon,
		DeleteFavicon:  routes.deleteFavicon,
	}
}

func matchBrandingAssetResponse(path string) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"Path": Equal(path),
	})
}

func matchLogoUploadOutput(path string) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return gstruct.PointTo(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"Path": Equal(path),
	}))
}

func matchFaviconUploadOutput(path string) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return gstruct.PointTo(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"Path": Equal(path),
	}))
}

func discardBrandingLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
