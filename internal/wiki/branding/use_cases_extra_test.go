package branding

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime/multipart"
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
)

var _ = ginkgo.Describe("branding use cases", func() {
	ginkgo.It("updates branding and returns the updated config", ginkgo.Label("integration"), func() {
		svc := newBrandingTestService()
		uc := NewUpdateBrandingUseCase(svc)

		out, err := uc.Execute(context.Background(), UpdateBrandingInput{SiteName: "  Docs Hub  "})

		Expect(err).NotTo(HaveOccurred())
		Expect(out.Config.SiteName).To(Equal("Docs Hub"))
	})

	ginkgo.It("returns validation errors from invalid branding updates", ginkgo.Label("integration"), func() {
		svc := newBrandingTestService()
		uc := NewUpdateBrandingUseCase(svc)

		out, err := uc.Execute(context.Background(), UpdateBrandingInput{SiteName: ""})

		Expect(out).To(BeNil())
		var validation *sharederrors.ValidationErrors
		Expect(err).To(BeAssignableToTypeOf(validation))
	})

	ginkgo.It("uploads and deletes logo assets", ginkgo.Label("integration"), func() {
		svc := newBrandingTestService()
		upload := NewUploadLogoUseCase(svc)
		deleteLogo := NewDeleteLogoUseCase(svc)

		out, err := upload.Execute(context.Background(), UploadLogoInput{
			File:     newBrandingUploadFile("logo-bytes"),
			Filename: tree.AssetNameFromString("custom.png"),
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(out.Path).To(Equal("logo.png"))
		Expect(out.Config.LogoFile).To(Equal("logo.png"))

		cleared, err := deleteLogo.Execute(context.Background())
		Expect(err).NotTo(HaveOccurred())
		Expect(cleared.Config.LogoFile).To(BeEmpty())
	})

	ginkgo.It("returns logo upload validation errors", ginkgo.Label("integration"), func() {
		svc := newBrandingTestService()
		uc := NewUploadLogoUseCase(svc)

		out, err := uc.Execute(context.Background(), UploadLogoInput{
			File:     newBrandingUploadFile("logo-bytes"),
			Filename: tree.AssetNameFromString("logo.exe"),
		})

		Expect(out).To(BeNil())
		Expect(err).To(MatchBrandingLocalizedError(corebranding.ErrCodeBrandingLogoInvalidType))
	})

	ginkgo.It("uploads and deletes favicon assets", ginkgo.Label("integration"), func() {
		svc := newBrandingTestService()
		upload := NewUploadFaviconUseCase(svc)
		deleteFavicon := NewDeleteFaviconUseCase(svc)

		out, err := upload.Execute(context.Background(), UploadFaviconInput{
			File:     newBrandingUploadFile("favicon-bytes"),
			Filename: tree.AssetNameFromString("favicon.ico"),
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(out.Path).To(Equal("favicon.ico"))
		Expect(out.Config.FaviconFile).To(Equal("favicon.ico"))

		cleared, err := deleteFavicon.Execute(context.Background())
		Expect(err).NotTo(HaveOccurred())
		Expect(cleared.Config.FaviconFile).To(BeEmpty())
	})

	ginkgo.It("returns favicon upload validation errors", ginkgo.Label("integration"), func() {
		svc := newBrandingTestService()
		uc := NewUploadFaviconUseCase(svc)

		out, err := uc.Execute(context.Background(), UploadFaviconInput{
			File:     newBrandingUploadFile("favicon-bytes"),
			Filename: tree.AssetNameFromString("favicon.jpg"),
		})

		Expect(out).To(BeNil())
		Expect(err).To(MatchBrandingLocalizedError(corebranding.ErrCodeBrandingFaviconInvalidType))
	})

	ginkgo.It("wraps config reload failures after branding operations", ginkgo.Label("unit"), func() {
		configErr := errors.New("config unavailable")
		svc := &fakeBrandingService{getErr: configErr, assetsDir: newBrandingTempDir()}

		getOut, err := (&GetBrandingUseCase{branding: svc}).Execute(context.Background())
		Expect(getOut).To(BeNil())
		Expect(err).To(MatchBrandingLocalizedError(ErrCodeBrandingConfigUnavailable))

		updateOut, err := (&UpdateBrandingUseCase{branding: svc}).Execute(context.Background(), UpdateBrandingInput{SiteName: "Docs"})
		Expect(updateOut).To(BeNil())
		Expect(err).To(MatchBrandingLocalizedError(ErrCodeBrandingConfigUnavailable))

		logoOut, err := (&UploadLogoUseCase{branding: svc}).Execute(context.Background(), UploadLogoInput{
			File:     newBrandingUploadFile("logo-bytes"),
			Filename: tree.AssetNameFromString("logo.png"),
		})
		Expect(logoOut).To(BeNil())
		Expect(err).To(MatchBrandingLocalizedError(ErrCodeBrandingConfigUnavailable))

		deleteLogoOut, err := (&DeleteLogoUseCase{branding: svc}).Execute(context.Background())
		Expect(deleteLogoOut).To(BeNil())
		Expect(err).To(MatchBrandingLocalizedError(ErrCodeBrandingConfigUnavailable))

		faviconOut, err := (&UploadFaviconUseCase{branding: svc}).Execute(context.Background(), UploadFaviconInput{
			File:     newBrandingUploadFile("favicon-bytes"),
			Filename: tree.AssetNameFromString("favicon.ico"),
		})
		Expect(faviconOut).To(BeNil())
		Expect(err).To(MatchBrandingLocalizedError(ErrCodeBrandingConfigUnavailable))

		deleteFaviconOut, err := (&DeleteFaviconUseCase{branding: svc}).Execute(context.Background())
		Expect(deleteFaviconOut).To(BeNil())
		Expect(err).To(MatchBrandingLocalizedError(ErrCodeBrandingConfigUnavailable))
	})
})

var _ = ginkgo.Describe("branding route mutations and assets", func() {
	ginkgo.It("updates branding through the handler", ginkgo.Label("integration"), func() {
		svc := newBrandingTestService()
		routes := newBrandingTestRoutes(svc)
		ctx, rec := ginTestContext()
		ctx.Request = httptest.NewRequest(http.MethodPut, "/api/branding", strings.NewReader(`{"siteName":"Docs Hub"}`))
		ctx.Request.Header.Set("Content-Type", "application/json")

		routes.handleUpdateBranding(ctx)

		Expect(rec).To(HaveHTTPStatus(http.StatusOK), rec.Body.String())
		var body corebranding.BrandingConfigResponse
		Expect(json.Unmarshal(rec.Body.Bytes(), &body)).To(Succeed())
		Expect(body.SiteName).To(Equal("Docs Hub"))
	})

	ginkgo.It("returns update validation errors from valid JSON payloads", ginkgo.Label("integration"), func() {
		svc := newBrandingTestService()
		routes := newBrandingTestRoutes(svc)
		ctx, rec := ginTestContext()
		ctx.Request = httptest.NewRequest(http.MethodPut, "/api/branding", strings.NewReader(`{"siteName":""}`))
		ctx.Request.Header.Set("Content-Type", "application/json")

		routes.handleUpdateBranding(ctx)

		Expect(rec).To(HaveBrandingValidationError(brandingSiteNameValidationField, corebranding.FieldCodeBrandingSiteNameRequired, corebranding.MessageIDBrandingSiteNameRequired))
	})

	ginkgo.It("uploads and deletes logos through the handlers", ginkgo.Label("integration"), func() {
		svc := newBrandingTestService()
		routes := newBrandingTestRoutes(svc)
		body, contentType := brandingMultipartBody("logo.png", []byte("logo-bytes"))
		uploadCtx, uploaded := ginTestContext()
		uploadCtx.Request = httptest.NewRequest(http.MethodPost, "/api/branding/logo", body)
		uploadCtx.Request.Header.Set("Content-Type", contentType)

		routes.handleUploadLogo(uploadCtx)

		Expect(uploaded).To(HaveHTTPStatus(http.StatusOK), uploaded.Body.String())
		var uploadBody struct {
			Path     string                              `json:"path"`
			Branding corebranding.BrandingConfigResponse `json:"branding"`
		}
		Expect(json.Unmarshal(uploaded.Body.Bytes(), &uploadBody)).To(Succeed())
		Expect(uploadBody.Path).To(Equal("logo.png"))
		Expect(uploadBody.Branding.LogoFile).To(Equal("logo.png"))

		deleteCtx, deleted := ginTestContext()
		deleteCtx.Request = httptest.NewRequest(http.MethodDelete, "/api/branding/logo", nil)
		routes.handleDeleteLogo(deleteCtx)
		Expect(deleted).To(HaveHTTPStatus(http.StatusOK), deleted.Body.String())
		var deleteBody struct {
			Branding corebranding.BrandingConfigResponse `json:"branding"`
		}
		Expect(json.Unmarshal(deleted.Body.Bytes(), &deleteBody)).To(Succeed())
		Expect(deleteBody.Branding.LogoFile).To(BeEmpty())
	})

	ginkgo.It("uploads and deletes favicons through the handlers", ginkgo.Label("integration"), func() {
		svc := newBrandingTestService()
		routes := newBrandingTestRoutes(svc)
		body, contentType := brandingMultipartBody("favicon.ico", []byte("favicon-bytes"))
		uploadCtx, uploaded := ginTestContext()
		uploadCtx.Request = httptest.NewRequest(http.MethodPost, "/api/branding/favicon", body)
		uploadCtx.Request.Header.Set("Content-Type", contentType)

		routes.handleUploadFavicon(uploadCtx)

		Expect(uploaded).To(HaveHTTPStatus(http.StatusOK), uploaded.Body.String())
		var uploadBody struct {
			Path     string                              `json:"path"`
			Branding corebranding.BrandingConfigResponse `json:"branding"`
		}
		Expect(json.Unmarshal(uploaded.Body.Bytes(), &uploadBody)).To(Succeed())
		Expect(uploadBody.Path).To(Equal("favicon.ico"))
		Expect(uploadBody.Branding.FaviconFile).To(Equal("favicon.ico"))

		deleteCtx, deleted := ginTestContext()
		deleteCtx.Request = httptest.NewRequest(http.MethodDelete, "/api/branding/favicon", nil)
		routes.handleDeleteFavicon(deleteCtx)
		Expect(deleted).To(HaveHTTPStatus(http.StatusOK), deleted.Body.String())
		var deleteBody struct {
			Branding corebranding.BrandingConfigResponse `json:"branding"`
		}
		Expect(json.Unmarshal(deleted.Body.Bytes(), &deleteBody)).To(Succeed())
		Expect(deleteBody.Branding.FaviconFile).To(BeEmpty())
	})

	ginkgo.It("returns structured upload request errors", ginkgo.Label("integration"), func() {
		svc := newBrandingTestService()
		routes := newBrandingTestRoutes(svc)

		badLogoCtx, badLogo := ginTestContext()
		badLogoCtx.Request = httptest.NewRequest(http.MethodPost, "/api/branding/logo", strings.NewReader("bad multipart"))
		badLogoCtx.Request.Header.Set("Content-Type", "multipart/form-data; boundary=missing")
		routes.handleUploadLogo(badLogoCtx)
		Expect(badLogo).To(HaveBrandingStructuredError(http.StatusRequestEntityTooLarge, ErrCodeBrandingLogoTooLarge))

		emptyLogoBody, emptyLogoContentType := brandingMultipartBody("", nil)
		missingLogoCtx, missingLogo := ginTestContext()
		missingLogoCtx.Request = httptest.NewRequest(http.MethodPost, "/api/branding/logo", emptyLogoBody)
		missingLogoCtx.Request.Header.Set("Content-Type", emptyLogoContentType)
		routes.handleUploadLogo(missingLogoCtx)
		Expect(missingLogo).To(HaveBrandingStructuredError(http.StatusBadRequest, ErrCodeBrandingLogoMissing))

		invalidLogoBody, invalidLogoContentType := brandingMultipartBody("logo.exe", []byte("logo-bytes"))
		invalidLogoCtx, invalidLogo := ginTestContext()
		invalidLogoCtx.Request = httptest.NewRequest(http.MethodPost, "/api/branding/logo", invalidLogoBody)
		invalidLogoCtx.Request.Header.Set("Content-Type", invalidLogoContentType)
		routes.handleUploadLogo(invalidLogoCtx)
		Expect(invalidLogo).To(HaveBrandingStructuredError(http.StatusBadRequest, ErrCodeBrandingLogoInvalidType))

		badFaviconCtx, badFavicon := ginTestContext()
		badFaviconCtx.Request = httptest.NewRequest(http.MethodPost, "/api/branding/favicon", strings.NewReader("bad multipart"))
		badFaviconCtx.Request.Header.Set("Content-Type", "multipart/form-data; boundary=missing")
		routes.handleUploadFavicon(badFaviconCtx)
		Expect(badFavicon).To(HaveBrandingStructuredError(http.StatusRequestEntityTooLarge, ErrCodeBrandingFaviconTooLarge))

		emptyFaviconBody, emptyFaviconContentType := brandingMultipartBody("", nil)
		missingFaviconCtx, missingFavicon := ginTestContext()
		missingFaviconCtx.Request = httptest.NewRequest(http.MethodPost, "/api/branding/favicon", emptyFaviconBody)
		missingFaviconCtx.Request.Header.Set("Content-Type", emptyFaviconContentType)
		routes.handleUploadFavicon(missingFaviconCtx)
		Expect(missingFavicon).To(HaveBrandingStructuredError(http.StatusBadRequest, ErrCodeBrandingFaviconMissing))

		invalidFaviconBody, invalidFaviconContentType := brandingMultipartBody("favicon.jpg", []byte("favicon-bytes"))
		invalidFaviconCtx, invalidFavicon := ginTestContext()
		invalidFaviconCtx.Request = httptest.NewRequest(http.MethodPost, "/api/branding/favicon", invalidFaviconBody)
		invalidFaviconCtx.Request.Header.Set("Content-Type", invalidFaviconContentType)
		routes.handleUploadFavicon(invalidFaviconCtx)
		Expect(invalidFavicon).To(HaveBrandingStructuredError(http.StatusBadRequest, ErrCodeBrandingFaviconInvalidType))
	})

	ginkgo.It("returns internal errors when branding config cannot be loaded by handlers", ginkgo.Label("integration"), func() {
		configErr := sharederrors.NewLocalizedErrorFromCode(ErrCodeBrandingConfigUnavailable, errors.New("config unavailable"))
		routes := newFailingBrandingRoutes(configErr)

		getCtx, getRec := ginTestContext()
		getCtx.Request = httptest.NewRequest(http.MethodGet, "/api/branding", nil)
		routes.handleGetBranding(getCtx)
		Expect(getRec).To(HaveBrandingStructuredError(http.StatusInternalServerError, ErrCodeBrandingConfigUnavailable))

		logoCtx, logoRec := ginTestContext()
		logoCtx.Request = httptest.NewRequest(http.MethodPost, "/api/branding/logo", nil)
		routes.handleUploadLogo(logoCtx)
		Expect(logoRec).To(HaveBrandingStructuredError(http.StatusInternalServerError, ErrCodeBrandingConfigUnavailable))

		faviconCtx, faviconRec := ginTestContext()
		faviconCtx.Request = httptest.NewRequest(http.MethodPost, "/api/branding/favicon", nil)
		routes.handleUploadFavicon(faviconCtx)
		Expect(faviconRec).To(HaveBrandingStructuredError(http.StatusInternalServerError, ErrCodeBrandingConfigUnavailable))

		assetCtx, assetRec := ginTestContext()
		assetCtx.Params = gin.Params{{Key: "filename", Value: "logo.png"}}
		assetCtx.Request = httptest.NewRequest(http.MethodGet, "/branding/logo.png", nil)
		routes.handleServeBrandingAsset(assetCtx)
		assetCtx.Writer.WriteHeaderNow()
		Expect(assetRec).To(HaveHTTPStatus(http.StatusInternalServerError), assetRec.Body.String())

		currentFaviconCtx, currentFaviconRec := ginTestContext()
		currentFaviconCtx.Request = httptest.NewRequest(http.MethodGet, "/favicon.ico", nil)
		routes.handleServeCurrentFavicon(currentFaviconCtx)
		currentFaviconCtx.Writer.WriteHeaderNow()
		Expect(currentFaviconRec).To(HaveHTTPStatus(http.StatusInternalServerError), currentFaviconRec.Body.String())
	})

	ginkgo.It("logs multipart close errors after successful uploads", ginkgo.Label("integration"), func() {
		svc := newBrandingTestService()
		routes := newBrandingTestRoutes(svc)
		originalClose := closeBrandingMultipartFile
		ginkgo.DeferCleanup(func() {
			closeBrandingMultipartFile = originalClose
		})
		closeBrandingMultipartFile = func(multipart.File) error {
			return errors.New("close failed")
		}

		logoBody, logoContentType := brandingMultipartBody("logo.png", []byte("logo-bytes"))
		logoCtx, logoRec := ginTestContext()
		logoCtx.Request = httptest.NewRequest(http.MethodPost, "/api/branding/logo", logoBody)
		logoCtx.Request.Header.Set("Content-Type", logoContentType)
		routes.handleUploadLogo(logoCtx)
		Expect(logoRec).To(HaveHTTPStatus(http.StatusOK), logoRec.Body.String())

		faviconBody, faviconContentType := brandingMultipartBody("favicon.ico", []byte("favicon-bytes"))
		faviconCtx, faviconRec := ginTestContext()
		faviconCtx.Request = httptest.NewRequest(http.MethodPost, "/api/branding/favicon", faviconBody)
		faviconCtx.Request.Header.Set("Content-Type", faviconContentType)
		routes.handleUploadFavicon(faviconCtx)
		Expect(faviconRec).To(HaveHTTPStatus(http.StatusOK), faviconRec.Body.String())
	})

	ginkgo.It("rejects branding asset paths when relative path validation fails", ginkgo.Label("unit"), func() {
		svc := newBrandingTestService()
		routes := newBrandingTestRoutes(svc)
		originalRel := brandingRel
		ginkgo.DeferCleanup(func() {
			brandingRel = originalRel
		})
		brandingRel = func(string, string) (string, error) {
			return "", errors.New("rel failed")
		}

		_, status := routes.resolveBrandingAssetPath(tree.AssetNameFromString("logo.png"), corebranding.DefaultBrandingConfig().ToResponse())

		Expect(status).To(Equal(http.StatusForbidden))
	})

	ginkgo.It("serves branding assets and disables client cache", ginkgo.Label("integration"), func() {
		svc := newBrandingTestService()
		_, err := svc.UploadLogo(newBrandingUploadFile("logo-bytes"), "logo.png")
		Expect(err).NotTo(HaveOccurred())
		router := newBrandingTestRouter(svc)

		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/branding/logo.png", nil))

		Expect(rec).To(HaveHTTPStatus(http.StatusOK), rec.Body.String())
		Expect(rec).To(HaveHTTPBody("logo-bytes"))
		Expect(rec).To(HaveHTTPHeaderWithValue("Cache-Control", "no-store"))
		Expect(rec).To(HaveHTTPHeaderWithValue("Pragma", "no-cache"))
		Expect(rec.Header().Get("Expires")).NotTo(BeEmpty())
	})

	ginkgo.It("serves configured and default favicons", ginkgo.Label("integration"), func() {
		svc := newBrandingTestService()
		router := newBrandingTestRouter(svc)

		defaultRec := httptest.NewRecorder()
		router.ServeHTTP(defaultRec, httptest.NewRequest(http.MethodGet, "/favicon.ico", nil))
		Expect(defaultRec).To(HaveHTTPStatus(http.StatusOK), defaultRec.Body.String())
		Expect(defaultRec).To(HaveHTTPBody(httpinternal.DefaultFaviconSVG))

		_, err := svc.UploadFavicon(newBrandingUploadFile("favicon-bytes"), "favicon.ico")
		Expect(err).NotTo(HaveOccurred())
		customRec := httptest.NewRecorder()
		router.ServeHTTP(customRec, httptest.NewRequest(http.MethodGet, "/favicon.ico", nil))
		Expect(customRec).To(HaveHTTPStatus(http.StatusOK), customRec.Body.String())
		Expect(customRec).To(HaveHTTPBody("favicon-bytes"))

		Expect(os.Remove(svc.GetBrandingAssetsDir() + "/favicon.ico")).To(Succeed())
		missingConfiguredRec := httptest.NewRecorder()
		router.ServeHTTP(missingConfiguredRec, httptest.NewRequest(http.MethodGet, "/favicon.ico", nil))
		Expect(missingConfiguredRec).To(HaveHTTPStatus(http.StatusOK), missingConfiguredRec.Body.String())
		Expect(missingConfiguredRec).To(HaveHTTPBody(httpinternal.DefaultFaviconSVG))
	})

	ginkgo.It("returns static asset status codes for forbidden and missing assets", ginkgo.Label("integration"), func() {
		svc := newBrandingTestService()
		router := newBrandingTestRouter(svc)

		forbidden := httptest.NewRecorder()
		router.ServeHTTP(forbidden, httptest.NewRequest(http.MethodGet, "/branding/logo.exe", nil))
		Expect(forbidden).To(HaveHTTPStatus(http.StatusForbidden), forbidden.Body.String())

		missing := httptest.NewRecorder()
		router.ServeHTTP(missing, httptest.NewRequest(http.MethodGet, "/branding/logo.png", nil))
		Expect(missing).To(HaveHTTPStatus(http.StatusNotFound), missing.Body.String())
	})

	ginkgo.It("returns structured delete errors when stored branding files cannot be removed", ginkgo.Label("integration"), func() {
		svc := newBrandingTestService()
		routes := newBrandingTestRoutes(svc)

		_, err := svc.UploadLogo(newBrandingUploadFile("logo-bytes"), "logo.png")
		Expect(err).NotTo(HaveOccurred())
		logoPath := filepath.Join(svc.GetBrandingAssetsDir(), "logo.png")
		Expect(os.Remove(logoPath)).To(Succeed())
		Expect(os.MkdirAll(filepath.Join(logoPath, "child"), 0o755)).To(Succeed())
		logoCtx, logoRec := ginTestContext()
		logoCtx.Request = httptest.NewRequest(http.MethodDelete, "/api/branding/logo", nil)

		routes.handleDeleteLogo(logoCtx)

		Expect(logoRec).To(HaveBrandingStructuredError(http.StatusInternalServerError, ErrCodeBrandingLogoDeleteFailed))

		_, err = svc.UploadFavicon(newBrandingUploadFile("favicon-bytes"), "favicon.ico")
		Expect(err).NotTo(HaveOccurred())
		faviconPath := filepath.Join(svc.GetBrandingAssetsDir(), "favicon.ico")
		Expect(os.Remove(faviconPath)).To(Succeed())
		Expect(os.MkdirAll(filepath.Join(faviconPath, "child"), 0o755)).To(Succeed())
		faviconCtx, faviconRec := ginTestContext()
		faviconCtx.Request = httptest.NewRequest(http.MethodDelete, "/api/branding/favicon", nil)

		routes.handleDeleteFavicon(faviconCtx)

		Expect(faviconRec).To(HaveBrandingStructuredError(http.StatusInternalServerError, ErrCodeBrandingFaviconDeleteFailed))
	})

	ginkgo.It("returns internal errors for branding asset stat failures", ginkgo.Label("integration"), func() {
		svc := newBrandingTestService()
		router := newBrandingTestRouter(svc)

		logoPath := filepath.Join(svc.GetBrandingAssetsDir(), "logo.png")
		Expect(os.Symlink("logo.png", logoPath)).To(Succeed())
		logoRec := httptest.NewRecorder()
		router.ServeHTTP(logoRec, httptest.NewRequest(http.MethodGet, "/branding/logo.png", nil))
		Expect(logoRec).To(HaveHTTPStatus(http.StatusInternalServerError), logoRec.Body.String())

		_, err := svc.UploadFavicon(newBrandingUploadFile("favicon-bytes"), "favicon.ico")
		Expect(err).NotTo(HaveOccurred())
		faviconPath := filepath.Join(svc.GetBrandingAssetsDir(), "favicon.ico")
		Expect(os.Remove(faviconPath)).To(Succeed())
		Expect(os.Symlink("favicon.ico", faviconPath)).To(Succeed())
		faviconRec := httptest.NewRecorder()
		router.ServeHTTP(faviconRec, httptest.NewRequest(http.MethodGet, "/favicon.ico", nil))
		Expect(faviconRec).To(HaveHTTPStatus(http.StatusInternalServerError), faviconRec.Body.String())
	})
})

func newBrandingUploadFile(contents string) *os.File {
	ginkgo.GinkgoHelper()
	file, err := os.CreateTemp(newBrandingTempDir(), "branding-upload-*")
	Expect(err).NotTo(HaveOccurred())
	_, err = file.WriteString(contents)
	Expect(err).NotTo(HaveOccurred())
	_, err = file.Seek(0, io.SeekStart)
	Expect(err).NotTo(HaveOccurred())
	ginkgo.DeferCleanup(func() {
		Expect(file.Close()).To(Succeed())
	})
	return file
}

func newBrandingTestRouter(svc *corebranding.BrandingService) http.Handler {
	ginkgo.GinkgoHelper()
	return httpinternal.NewRouter(
		[]httpinternal.RouteRegistrar{newBrandingTestRoutes(svc)},
		httpinternal.FrontendConfig{},
		httpinternal.RouterOptions{
			AllowInsecure:         true,
			AuthDisabled:          true,
			DisableFrontendRoutes: true,
		},
	)
}

func newBrandingTestRoutes(svc *corebranding.BrandingService) *Routes {
	ginkgo.GinkgoHelper()
	return NewRoutes(RoutesConfig{
		GetBranding:     NewGetBrandingUseCase(svc),
		UpdateBranding:  NewUpdateBrandingUseCase(svc),
		UploadLogo:      NewUploadLogoUseCase(svc),
		DeleteLogo:      NewDeleteLogoUseCase(svc),
		UploadFavicon:   NewUploadFaviconUseCase(svc),
		DeleteFavicon:   NewDeleteFaviconUseCase(svc),
		BrandingService: svc,
		Log:             slog.Default(),
	})
}

func brandingMultipartBody(filename string, fileContent []byte) (*bytes.Buffer, string) {
	ginkgo.GinkgoHelper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if filename != "" {
		part, err := writer.CreateFormFile("file", filename)
		Expect(err).NotTo(HaveOccurred())
		_, err = part.Write(fileContent)
		Expect(err).NotTo(HaveOccurred())
	}
	Expect(writer.Close()).To(Succeed())
	return &body, writer.FormDataContentType()
}

func performBrandingCSRFRequest(router http.Handler, method string, path string, body io.Reader, contentType string) *httptest.ResponseRecorder {
	ginkgo.GinkgoHelper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, body)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.Header.Set("X-CSRF-Token", "test-csrf-token")
	req.AddCookie(&http.Cookie{Name: "leafwiki_csrf", Value: "test-csrf-token"})
	router.ServeHTTP(rec, req)
	return rec
}

type fakeBrandingService struct {
	config    *corebranding.BrandingConfigResponse
	getErr    error
	assetsDir string
}

func (s *fakeBrandingService) GetBranding() (*corebranding.BrandingConfigResponse, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	if s.config != nil {
		return s.config, nil
	}
	return corebranding.DefaultBrandingConfig().ToResponse(), nil
}

func (s *fakeBrandingService) UpdateBranding(string) error {
	return nil
}

func (s *fakeBrandingService) UploadLogo(multipart.File, string) (string, error) {
	return "logo.png", nil
}

func (s *fakeBrandingService) DeleteLogo() error {
	return nil
}

func (s *fakeBrandingService) UploadFavicon(multipart.File, string) (string, error) {
	return "favicon.ico", nil
}

func (s *fakeBrandingService) DeleteFavicon() error {
	return nil
}

func (s *fakeBrandingService) GetBrandingAssetsDir() string {
	return s.assetsDir
}

func newFailingBrandingRoutes(err error) *Routes {
	ginkgo.GinkgoHelper()
	svc := &fakeBrandingService{getErr: err, assetsDir: newBrandingTempDir()}
	return &Routes{
		getBranding:     &GetBrandingUseCase{branding: svc},
		uploadLogo:      &UploadLogoUseCase{branding: svc},
		uploadFavicon:   &UploadFaviconUseCase{branding: svc},
		brandingService: svc,
		log:             slog.Default(),
	}
}
