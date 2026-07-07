package branding

import (
	"context"
	"errors"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"

	"github.com/gin-gonic/gin"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/core/tree"
	httpinternal "github.com/perber/wiki/internal/http"
)

var _ = ginkgo.Describe("branding route handlers", ginkgo.Label("unit"), func() {
	ginkgo.It("constructs routes with configured branding use cases", func() {
		getBranding := &GetBrandingUseCase{}
		updateBranding := &UpdateBrandingUseCase{}
		uploadLogo := &UploadLogoUseCase{}
		deleteLogo := &DeleteLogoUseCase{}
		uploadFavicon := &UploadFaviconUseCase{}
		deleteFavicon := &DeleteFaviconUseCase{}

		routes := NewRoutes(RoutesConfig{
			GetBranding:    getBranding,
			UpdateBranding: updateBranding,
			UploadLogo:     uploadLogo,
			DeleteLogo:     deleteLogo,
			UploadFavicon:  uploadFavicon,
			DeleteFavicon:  deleteFavicon,
			Log:            discardBrandingLog(),
		})

		Expect(routes).To(matchBrandingRouteUseCases(brandingRouteUseCases{
			GetBranding:    getBranding,
			UpdateBranding: updateBranding,
			UploadLogo:     uploadLogo,
			DeleteLogo:     deleteLogo,
			UploadFavicon:  uploadFavicon,
			DeleteFavicon:  deleteFavicon,
		}))
	})

	ginkgo.It("registers public and administrative branding endpoints", func() {
		gin.SetMode(gin.TestMode)
		engine := gin.New()

		NewRoutes(RoutesConfig{Log: discardBrandingLog()}).RegisterRoutes(httpinternal.RouterContext{
			Base: engine.Group(""),
			Opts: httpinternal.RouterOptions{AuthDisabled: true},
		})

		Expect(brandingRegisteredRoutes(engine)).To(exposeBrandingRouteContract())
	})

	ginkgo.It("writes the current branding configuration", func() {
		ctx, rec := newBrandingUnitContext(http.MethodGet, "/api/branding", "")
		svc := newUnitBrandingService()

		newUnitBrandingRoutes(svc).handleGetBranding(ctx)

		Expect(rec).To(HaveHTTPStatus(http.StatusOK), rec.Body.String())
		Expect(decodeBrandingConfig(rec)).To(matchBrandingConfig("LeafWiki", "", ""))
	})

	ginkgo.It("writes structured get-branding failures", func() {
		ctx, rec := newBrandingUnitContext(http.MethodGet, "/api/branding", "")
		svc := newUnitBrandingService()
		svc.getErr = sharederrors.NewLocalizedErrorFromCode(ErrCodeBrandingConfigUnavailable, nil)

		newUnitBrandingRoutes(svc).handleGetBranding(ctx)

		Expect(rec).To(HaveBrandingStructuredError(http.StatusInternalServerError, ErrCodeBrandingConfigUnavailable), rec.Body.String())
	})

	ginkgo.It("updates the site name from JSON payloads", func() {
		ctx, rec := newBrandingUnitContext(http.MethodPut, "/api/branding", `{"siteName":"Docs Hub"}`)
		ctx.Request.Header.Set("Content-Type", "application/json")
		svc := newUnitBrandingService()

		newUnitBrandingRoutes(svc).handleUpdateBranding(ctx)

		Expect(rec).To(HaveHTTPStatus(http.StatusOK), rec.Body.String())
		Expect(decodeBrandingConfig(rec)).To(matchBrandingConfig("Docs Hub", "", ""))
		Expect(svc).To(recordBrandingSiteName("Docs Hub"))
	})

	ginkgo.It("writes structured update errors", func() {
		invalidCtx, invalidRec := newBrandingUnitContext(http.MethodPut, "/api/branding", `{`)
		invalidCtx.Request.Header.Set("Content-Type", "application/json")
		newUnitBrandingRoutes(newUnitBrandingService()).handleUpdateBranding(invalidCtx)
		Expect(invalidRec).To(HaveBrandingStructuredError(http.StatusBadRequest, ErrCodeBrandingInvalidPayload), invalidRec.Body.String())

		failedCtx, failedRec := newBrandingUnitContext(http.MethodPut, "/api/branding", `{"siteName":"Docs"}`)
		failedCtx.Request.Header.Set("Content-Type", "application/json")
		svc := newUnitBrandingService()
		svc.updateErr = sharederrors.NewLocalizedErrorFromCode(ErrCodeBrandingInternalError, errors.New("update failed"))
		newUnitBrandingRoutes(svc).handleUpdateBranding(failedCtx)
		Expect(failedRec).To(HaveBrandingStructuredError(http.StatusInternalServerError, ErrCodeBrandingInternalError), failedRec.Body.String())
	})

	ginkgo.It("uploads and deletes logo and favicon assets", func() {
		svc := newUnitBrandingService()
		routes := newUnitBrandingRoutes(svc)

		logoCtx, logoRec := newBrandingMultipartUnitContext("/api/branding/logo", "logo.png", []byte("logo"))
		routes.handleUploadLogo(logoCtx)
		Expect(logoRec).To(HaveHTTPStatus(http.StatusOK), logoRec.Body.String())
		Expect(decodeBrandingAssetResponse(logoRec)).To(matchBrandingAssetResponse("logo.png"))
		Expect(svc).To(recordBrandingUpload(brandingUploadObservation{LogoFilename: "logo.png"}))

		deleteLogoCtx, deleteLogoRec := newBrandingUnitContext(http.MethodDelete, "/api/branding/logo", "")
		routes.handleDeleteLogo(deleteLogoCtx)
		Expect(deleteLogoRec).To(HaveHTTPStatus(http.StatusOK), deleteLogoRec.Body.String())

		faviconCtx, faviconRec := newBrandingMultipartUnitContext("/api/branding/favicon", "favicon.ico", []byte("icon"))
		routes.handleUploadFavicon(faviconCtx)
		Expect(faviconRec).To(HaveHTTPStatus(http.StatusOK), faviconRec.Body.String())
		Expect(decodeBrandingAssetResponse(faviconRec)).To(matchBrandingAssetResponse("favicon.ico"))
		Expect(svc).To(recordBrandingUpload(brandingUploadObservation{LogoFilename: "logo.png", FaviconFilename: "favicon.ico"}))

		deleteFaviconCtx, deleteFaviconRec := newBrandingUnitContext(http.MethodDelete, "/api/branding/favicon", "")
		routes.handleDeleteFavicon(deleteFaviconCtx)
		Expect(deleteFaviconRec).To(HaveHTTPStatus(http.StatusOK), deleteFaviconRec.Body.String())
	})

	ginkgo.It("writes structured logo upload request errors", func() {
		configErrCtx, configErrRec := newBrandingUnitContext(http.MethodPost, "/api/branding/logo", "")
		svc := newUnitBrandingService()
		svc.getErr = sharederrors.NewLocalizedErrorFromCode(ErrCodeBrandingConfigUnavailable, nil)
		newUnitBrandingRoutes(svc).handleUploadLogo(configErrCtx)
		Expect(configErrRec).To(HaveBrandingStructuredError(http.StatusInternalServerError, ErrCodeBrandingConfigUnavailable), configErrRec.Body.String())

		tooLargeCtx, tooLargeRec := newBrandingMultipartUnitContext("/api/branding/logo", "logo.png", []byte("oversized"))
		limited := newUnitBrandingService()
		limited.config.BrandingConstraints.MaxLogoSize = 1
		newUnitBrandingRoutes(limited).handleUploadLogo(tooLargeCtx)
		Expect(tooLargeRec).To(HaveBrandingStructuredError(http.StatusRequestEntityTooLarge, ErrCodeBrandingLogoTooLarge), tooLargeRec.Body.String())

		missingCtx, missingRec := newBrandingMultipartUnitContext("/api/branding/logo", "", nil)
		newUnitBrandingRoutes(newUnitBrandingService()).handleUploadLogo(missingCtx)
		Expect(missingRec).To(HaveBrandingStructuredError(http.StatusBadRequest, ErrCodeBrandingLogoMissing), missingRec.Body.String())
	})

	ginkgo.It("writes structured favicon upload request errors", func() {
		configErrCtx, configErrRec := newBrandingUnitContext(http.MethodPost, "/api/branding/favicon", "")
		svc := newUnitBrandingService()
		svc.getErr = sharederrors.NewLocalizedErrorFromCode(ErrCodeBrandingConfigUnavailable, nil)
		newUnitBrandingRoutes(svc).handleUploadFavicon(configErrCtx)
		Expect(configErrRec).To(HaveBrandingStructuredError(http.StatusInternalServerError, ErrCodeBrandingConfigUnavailable), configErrRec.Body.String())

		tooLargeCtx, tooLargeRec := newBrandingMultipartUnitContext("/api/branding/favicon", "favicon.ico", []byte("oversized"))
		limited := newUnitBrandingService()
		limited.config.BrandingConstraints.MaxFaviconSize = 1
		newUnitBrandingRoutes(limited).handleUploadFavicon(tooLargeCtx)
		Expect(tooLargeRec).To(HaveBrandingStructuredError(http.StatusRequestEntityTooLarge, ErrCodeBrandingFaviconTooLarge), tooLargeRec.Body.String())

		missingCtx, missingRec := newBrandingMultipartUnitContext("/api/branding/favicon", "", nil)
		newUnitBrandingRoutes(newUnitBrandingService()).handleUploadFavicon(missingCtx)
		Expect(missingRec).To(HaveBrandingStructuredError(http.StatusBadRequest, ErrCodeBrandingFaviconMissing), missingRec.Body.String())
	})

	ginkgo.It("writes upload, delete, and multipart close failures as structured responses", func() {
		restoreClose := closeBrandingMultipartFile
		ginkgo.DeferCleanup(func() {
			closeBrandingMultipartFile = restoreClose
		})
		closeBrandingMultipartFile = func(multipart.File) error {
			return errors.New("close failed")
		}

		logoCtx, logoRec := newBrandingMultipartUnitContext("/api/branding/logo", "logo.png", []byte("logo"))
		logoSvc := newUnitBrandingService()
		logoSvc.uploadLogoErr = sharederrors.NewLocalizedErrorFromCode(ErrCodeBrandingLogoInvalidType, nil)
		newUnitBrandingRoutes(logoSvc).handleUploadLogo(logoCtx)
		Expect(logoRec).To(HaveBrandingStructuredError(http.StatusBadRequest, ErrCodeBrandingLogoInvalidType), logoRec.Body.String())

		faviconCtx, faviconRec := newBrandingMultipartUnitContext("/api/branding/favicon", "favicon.ico", []byte("icon"))
		faviconSvc := newUnitBrandingService()
		faviconSvc.uploadFaviconErr = sharederrors.NewLocalizedErrorFromCode(ErrCodeBrandingFaviconInvalidType, nil)
		newUnitBrandingRoutes(faviconSvc).handleUploadFavicon(faviconCtx)
		Expect(faviconRec).To(HaveBrandingStructuredError(http.StatusBadRequest, ErrCodeBrandingFaviconInvalidType), faviconRec.Body.String())

		deleteLogoCtx, deleteLogoRec := newBrandingUnitContext(http.MethodDelete, "/api/branding/logo", "")
		deleteLogoSvc := newUnitBrandingService()
		deleteLogoSvc.deleteLogoErr = sharederrors.NewLocalizedErrorFromCode(ErrCodeBrandingLogoDeleteFailed, nil)
		newUnitBrandingRoutes(deleteLogoSvc).handleDeleteLogo(deleteLogoCtx)
		Expect(deleteLogoRec).To(HaveBrandingStructuredError(http.StatusInternalServerError, ErrCodeBrandingLogoDeleteFailed), deleteLogoRec.Body.String())

		deleteFaviconCtx, deleteFaviconRec := newBrandingUnitContext(http.MethodDelete, "/api/branding/favicon", "")
		deleteFaviconSvc := newUnitBrandingService()
		deleteFaviconSvc.deleteFaviconErr = sharederrors.NewLocalizedErrorFromCode(ErrCodeBrandingFaviconDeleteFailed, nil)
		newUnitBrandingRoutes(deleteFaviconSvc).handleDeleteFavicon(deleteFaviconCtx)
		Expect(deleteFaviconRec).To(HaveBrandingStructuredError(http.StatusInternalServerError, ErrCodeBrandingFaviconDeleteFailed), deleteFaviconRec.Body.String())
	})
})

var _ = ginkgo.Describe("branding static asset handlers", ginkgo.Label("unit"), func() {
	ginkgo.It("serves existing assets with disabled client cache", func() {
		svc := newUnitBrandingService()
		Expect(os.WriteFile(filepath.Join(svc.assetsDir, "logo.png"), []byte("logo-bytes"), 0o600)).To(Succeed())
		ctx, rec := newBrandingUnitContext(http.MethodGet, "/branding/logo.png", "")
		ctx.Params = gin.Params{{Key: "filename", Value: "logo.png"}}

		newUnitBrandingRoutes(svc).handleServeBrandingAsset(ctx)

		Expect(rec).To(HaveHTTPStatus(http.StatusOK), rec.Body.String())
		Expect(rec).To(HaveHTTPBody("logo-bytes"))
		Expect(rec).To(HaveHTTPHeaderWithValue("Cache-Control", "no-store"))
		Expect(rec).To(HaveHTTPHeaderWithValue("Pragma", "no-cache"))
		Expect(rec.Header().Get("Expires")).NotTo(BeEmpty())
	})

	ginkgo.It("writes status codes for asset configuration and path failures", func() {
		configCtx, configRec := newBrandingUnitContext(http.MethodGet, "/branding/logo.png", "")
		configCtx.Params = gin.Params{{Key: "filename", Value: "logo.png"}}
		configSvc := newUnitBrandingService()
		configSvc.getErr = sharederrors.NewLocalizedErrorFromCode(ErrCodeBrandingConfigUnavailable, nil)
		newUnitBrandingRoutes(configSvc).handleServeBrandingAsset(configCtx)
		configCtx.Writer.WriteHeaderNow()
		Expect(configRec).To(HaveHTTPStatus(http.StatusInternalServerError), configRec.Body.String())

		forbiddenCtx, forbiddenRec := newBrandingUnitContext(http.MethodGet, "/branding/logo.exe", "")
		forbiddenCtx.Params = gin.Params{{Key: "filename", Value: "logo.exe"}}
		newUnitBrandingRoutes(newUnitBrandingService()).handleServeBrandingAsset(forbiddenCtx)
		forbiddenCtx.Writer.WriteHeaderNow()
		Expect(forbiddenRec).To(HaveHTTPStatus(http.StatusForbidden), forbiddenRec.Body.String())

		missingCtx, missingRec := newBrandingUnitContext(http.MethodGet, "/branding/logo.png", "")
		missingCtx.Params = gin.Params{{Key: "filename", Value: "logo.png"}}
		newUnitBrandingRoutes(newUnitBrandingService()).handleServeBrandingAsset(missingCtx)
		missingCtx.Writer.WriteHeaderNow()
		Expect(missingRec).To(HaveHTTPStatus(http.StatusNotFound), missingRec.Body.String())
	})

	ginkgo.It("serves configured and default favicons", func() {
		defaultCtx, defaultRec := newBrandingUnitContext(http.MethodGet, "/favicon.ico", "")
		newUnitBrandingRoutes(newUnitBrandingService()).handleServeCurrentFavicon(defaultCtx)
		Expect(defaultRec).To(HaveHTTPStatus(http.StatusOK), defaultRec.Body.String())
		Expect(defaultRec).To(HaveHTTPBody(httpinternal.DefaultFaviconSVG))

		customSvc := newUnitBrandingService()
		customSvc.config.FaviconFile = "favicon.ico"
		Expect(os.WriteFile(filepath.Join(customSvc.assetsDir, "favicon.ico"), []byte("icon-bytes"), 0o600)).To(Succeed())
		customCtx, customRec := newBrandingUnitContext(http.MethodGet, "/favicon.ico", "")
		newUnitBrandingRoutes(customSvc).handleServeCurrentFavicon(customCtx)
		Expect(customRec).To(HaveHTTPStatus(http.StatusOK), customRec.Body.String())
		Expect(customRec).To(HaveHTTPBody("icon-bytes"))

		missingSvc := newUnitBrandingService()
		missingSvc.config.FaviconFile = "favicon.ico"
		missingCtx, missingRec := newBrandingUnitContext(http.MethodGet, "/favicon.ico", "")
		newUnitBrandingRoutes(missingSvc).handleServeCurrentFavicon(missingCtx)
		Expect(missingRec).To(HaveHTTPStatus(http.StatusOK), missingRec.Body.String())
		Expect(missingRec).To(HaveHTTPBody(httpinternal.DefaultFaviconSVG))
	})

	ginkgo.It("writes internal status for favicon config and stat failures", func() {
		configCtx, configRec := newBrandingUnitContext(http.MethodGet, "/favicon.ico", "")
		configSvc := newUnitBrandingService()
		configSvc.getErr = sharederrors.NewLocalizedErrorFromCode(ErrCodeBrandingConfigUnavailable, nil)
		newUnitBrandingRoutes(configSvc).handleServeCurrentFavicon(configCtx)
		configCtx.Writer.WriteHeaderNow()
		Expect(configRec).To(HaveHTTPStatus(http.StatusInternalServerError), configRec.Body.String())

		statSvc := newUnitBrandingService()
		statSvc.config.FaviconFile = "favicon.ico"
		Expect(os.Symlink("favicon.ico", filepath.Join(statSvc.assetsDir, "favicon.ico"))).To(Succeed())
		statCtx, statRec := newBrandingUnitContext(http.MethodGet, "/favicon.ico", "")
		newUnitBrandingRoutes(statSvc).handleServeCurrentFavicon(statCtx)
		statCtx.Writer.WriteHeaderNow()
		Expect(statRec).To(HaveHTTPStatus(http.StatusInternalServerError), statRec.Body.String())
	})

	ginkgo.It("rejects branding paths that resolve outside the asset directory", func() {
		svc := newUnitBrandingService()
		Expect(os.WriteFile(filepath.Join(svc.assetsDir, "logo.png"), []byte("logo"), 0o600)).To(Succeed())
		originalRel := brandingRel
		ginkgo.DeferCleanup(func() {
			brandingRel = originalRel
		})
		brandingRel = func(string, string) (string, error) {
			return "../logo.png", nil
		}

		_, status := newUnitBrandingRoutes(svc).resolveBrandingAssetPath(tree.AssetNameFromString("logo.png"), svc.config)

		Expect(status).To(Equal(http.StatusForbidden))
	})
})

var _ = ginkgo.Describe("branding use-case contracts", ginkgo.Label("unit"), func() {
	ginkgo.It("returns successful operation outputs from the branding service", func() {
		svc := newUnitBrandingService()

		getOut, err := (&GetBrandingUseCase{branding: svc}).Execute(context.Background())
		Expect(err).To(Succeed())
		Expect(getOut.Config).To(matchBrandingConfig("LeafWiki", "", ""))

		updateOut, err := (&UpdateBrandingUseCase{branding: svc}).Execute(context.Background(), UpdateBrandingInput{SiteName: "Docs Hub"})
		Expect(err).To(Succeed())
		Expect(updateOut.Config).To(matchBrandingConfig("Docs Hub", "", ""))

		logoOut, err := (&UploadLogoUseCase{branding: svc}).Execute(context.Background(), UploadLogoInput{
			File:     newBrandingUploadFile("logo"),
			Filename: tree.AssetNameFromString("logo.png"),
		})
		Expect(err).To(Succeed())
		Expect(logoOut).To(matchLogoUploadOutput("logo.png"))

		deleteLogoOut, err := (&DeleteLogoUseCase{branding: svc}).Execute(context.Background())
		Expect(err).To(Succeed())
		Expect(deleteLogoOut.Config).To(matchBrandingConfig("Docs Hub", "", ""))

		faviconOut, err := (&UploadFaviconUseCase{branding: svc}).Execute(context.Background(), UploadFaviconInput{
			File:     newBrandingUploadFile("icon"),
			Filename: tree.AssetNameFromString("favicon.ico"),
		})
		Expect(err).To(Succeed())
		Expect(faviconOut).To(matchFaviconUploadOutput("favicon.ico"))

		deleteFaviconOut, err := (&DeleteFaviconUseCase{branding: svc}).Execute(context.Background())
		Expect(err).To(Succeed())
		Expect(deleteFaviconOut.Config).To(matchBrandingConfig("Docs Hub", "", ""))
	})

	ginkgo.It("returns operation failures unchanged before reloading config", func() {
		operationErr := errors.New("branding operation failed")
		svc := newUnitBrandingService()
		svc.updateErr = operationErr
		svc.uploadLogoErr = operationErr
		svc.deleteLogoErr = operationErr
		svc.uploadFaviconErr = operationErr
		svc.deleteFaviconErr = operationErr

		updateOut, err := (&UpdateBrandingUseCase{branding: svc}).Execute(context.Background(), UpdateBrandingInput{SiteName: "Docs"})
		Expect(updateOut).To(BeNil())
		Expect(err).To(MatchError(operationErr))

		logoOut, err := (&UploadLogoUseCase{branding: svc}).Execute(context.Background(), UploadLogoInput{File: newBrandingUploadFile("logo"), Filename: tree.AssetNameFromString("logo.png")})
		Expect(logoOut).To(BeNil())
		Expect(err).To(MatchError(operationErr))

		deleteLogoOut, err := (&DeleteLogoUseCase{branding: svc}).Execute(context.Background())
		Expect(deleteLogoOut).To(BeNil())
		Expect(err).To(MatchError(operationErr))

		faviconOut, err := (&UploadFaviconUseCase{branding: svc}).Execute(context.Background(), UploadFaviconInput{File: newBrandingUploadFile("icon"), Filename: tree.AssetNameFromString("favicon.ico")})
		Expect(faviconOut).To(BeNil())
		Expect(err).To(MatchError(operationErr))

		deleteFaviconOut, err := (&DeleteFaviconUseCase{branding: svc}).Execute(context.Background())
		Expect(deleteFaviconOut).To(BeNil())
		Expect(err).To(MatchError(operationErr))
	})
})

var _ = ginkgo.Describe("branding error response contracts", ginkgo.Label("unit"), func() {
	ginkgo.It("writes validation and internal errors as structured responses", func() {
		validation := sharederrors.NewValidationErrors()
		validation.Add(brandingSiteNameValidationField.String(), "site name is required")
		validationCtx, validationRec := ginTestContext()

		respondWithBrandingError(validationCtx, validation)

		Expect(validationRec).To(HaveBrandingValidationError(brandingSiteNameValidationField, sharederrors.FieldValidationErrorCode, sharederrors.FieldValidationErrorMessageID))

		internalCtx, internalRec := ginTestContext()
		respondWithBrandingError(internalCtx, errors.New("branding write failed"))

		Expect(internalRec).To(HaveBrandingStructuredError(http.StatusInternalServerError, ErrCodeBrandingInternalError), internalRec.Body.String())
	})
})
