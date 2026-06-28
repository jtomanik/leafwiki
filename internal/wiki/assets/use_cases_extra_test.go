package assets

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
	"strings"

	"github.com/gin-gonic/gin"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	coreassets "github.com/perber/wiki/internal/core/assets"
	coreauth "github.com/perber/wiki/internal/core/auth"
	"github.com/perber/wiki/internal/core/shared"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/core/tree"
)

type inMemoryMultipartFile struct {
	*bytes.Reader
}

func (f inMemoryMultipartFile) Close() error {
	return nil
}

var _ = ginkgo.Describe("asset helpers", func() {
	ginkgo.It("assetErrorStatus maps localized asset error codes", func() {
		Expect(assetErrorStatus(ErrCodeAssetFileTooLarge)).To(Equal(http.StatusRequestEntityTooLarge))
		Expect(assetErrorStatus(ErrCodeAssetMissingFile)).To(Equal(http.StatusBadRequest))
		Expect(assetErrorStatus(ErrCodeAssetInvalidName)).To(Equal(http.StatusBadRequest))
		Expect(assetErrorStatus(ErrCodeAssetPageNotFound)).To(Equal(http.StatusNotFound))
		Expect(assetErrorStatus(ErrCodeAssetAlreadyExists)).To(Equal(http.StatusConflict))
		Expect(assetErrorStatus("unknown")).To(Equal(http.StatusInternalServerError))
	})

	ginkgo.It("asset localized error constructors preserve code and cause", func() {
		tooLarge := NewAssetFileTooLargeError()
		Expect(tooLarge.Code).To(Equal(ErrCodeAssetFileTooLarge))

		cause := errors.New("bad json")
		invalid := NewAssetInvalidPayloadError(cause)
		Expect(invalid.Code).To(Equal(ErrCodeAssetInvalidPayload))
		Expect(errors.Is(invalid, cause)).To(BeTrue())
	})

	ginkgo.It("logs uploaded file close errors", func() {
		var logOutput bytes.Buffer
		logger := slog.New(slog.NewTextHandler(&logOutput, nil))

		logUploadedAssetFileClose(logger, closeErrorFile{err: errors.New("close failed")})

		Expect(logOutput.String()).To(ContainSubstring("could not close uploaded file"))
		Expect(logOutput.String()).To(ContainSubstring("close failed"))
	})

	ginkgo.It("respondWithAssetError maps localized and generic errors", func() {
		gin.SetMode(gin.TestMode)

		localizedRec := httptest.NewRecorder()
		localizedCtx, _ := gin.CreateTestContext(localizedRec)
		respondWithAssetError(localizedCtx, sharederrors.NewLocalizedErrorFromCode(ErrCodeAssetAlreadyExists, nil, "asset.png"))
		Expect(localizedRec.Code).To(Equal(http.StatusConflict))
		Expect(localizedRec.Body.String()).To(ContainSubstring(string(ErrCodeAssetAlreadyExists)))

		genericRec := httptest.NewRecorder()
		genericCtx, _ := gin.CreateTestContext(genericRec)
		respondWithAssetError(genericCtx, errors.New("boom"))
		Expect(genericRec.Code).To(Equal(http.StatusInternalServerError))
		Expect(genericRec.Body.String()).To(ContainSubstring(string(ErrCodeAssetInternalError)))
	})

	ginkgo.It("DetectAssetMIMEType prefers extension and falls back to content sniffing", func() {
		Expect(DetectAssetMIMEType("style.css", []byte("not css"))).To(Equal("text/css; charset=utf-8"))
		Expect(DetectAssetMIMEType("asset.unknownext", []byte("%PDF-1.7\n"))).To(Equal("application/pdf"))
	})
})

var _ = ginkgo.Describe("asset use cases", func() {
	ginkgo.It("upload, list, get, rename, and delete round-trip a page asset", func() {
		treeService, pageID := setupAssetUseCaseTree()
		assetService := coreassets.NewAssetService(ginkgo.GinkgoT().TempDir(), tree.NewSlugService())

		uploadOut, err := NewUploadAssetUseCase(treeService, assetService, slog.Default()).Execute(context.Background(), UploadAssetInput{
			UserID:   newFixtureUserID("user-1"),
			PageID:   *pageID,
			File:     inMemoryMultipartFile{Reader: bytes.NewReader([]byte("hello image"))},
			Filename: tree.AssetName("my-image.png"),
			ByteCap:  shared.MaxBytes(1024),
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(uploadOut.URL).To(Equal("/assets/" + pageID.String() + "/my-image.png"))

		listOut, err := NewListAssetsUseCase(treeService, assetService).Execute(context.Background(), ListAssetsInput{PageID: *pageID})
		Expect(err).NotTo(HaveOccurred())
		Expect(listOut.Files).To(Equal([]string{"/assets/" + pageID.String() + "/my-image.png"}))

		getOut, err := NewGetAssetUseCase(treeService, assetService).Execute(context.Background(), GetAssetInput{
			PageID:   *pageID,
			Filename: tree.AssetName("my-image.png"),
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(getOut.Filename).To(Equal(tree.AssetName("my-image.png")))
		Expect(getOut.MIMEType).To(Equal("image/png"))
		Expect(getOut.Content).To(Equal([]byte("hello image")))

		renameOut, err := NewRenameAssetUseCase(treeService, assetService, slog.Default()).Execute(context.Background(), RenameAssetInput{
			UserID:      newFixtureUserID("user-1"),
			PageID:      *pageID,
			OldFilename: tree.AssetName("my-image.png"),
			NewFilename: tree.AssetName("renamed.png"),
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(renameOut.URL).To(Equal("/assets/" + pageID.String() + "/renamed.png"))

		Expect(NewDeleteAssetUseCase(treeService, assetService, slog.Default()).Execute(context.Background(), DeleteAssetInput{
			UserID:   newFixtureUserID("user-1"),
			PageID:   *pageID,
			Filename: tree.AssetName("renamed.png"),
		})).To(Succeed())

		listOut, err = NewListAssetsUseCase(treeService, assetService).Execute(context.Background(), ListAssetsInput{PageID: *pageID})
		Expect(err).NotTo(HaveOccurred())
		Expect(listOut.Files).To(BeEmpty())
	})

	ginkgo.It("use cases return localized page-not-found errors", func() {
		treeService, _ := setupAssetUseCaseTree()
		assetService := coreassets.NewAssetService(ginkgo.GinkgoT().TempDir(), tree.NewSlugService())

		_, err := NewUploadAssetUseCase(treeService, assetService, slog.Default()).Execute(context.Background(), UploadAssetInput{
			UserID:   newFixtureUserID("user-1"),
			PageID:   newFixturePageID("missing"),
			File:     inMemoryMultipartFile{Reader: bytes.NewReader([]byte("content"))},
			Filename: tree.AssetName("asset.png"),
			ByteCap:  shared.MaxBytes(1024),
		})
		expectAssetLocalizedError(err, ErrCodeAssetPageNotFound)

		_, err = NewListAssetsUseCase(treeService, assetService).Execute(context.Background(), ListAssetsInput{PageID: newFixturePageID("missing")})
		expectAssetLocalizedError(err, ErrCodeAssetPageNotFound)

		_, err = NewGetAssetUseCase(treeService, assetService).Execute(context.Background(), GetAssetInput{PageID: newFixturePageID("missing"), Filename: tree.AssetName("asset.png")})
		expectAssetLocalizedError(err, ErrCodeAssetPageNotFound)

		_, err = NewRenameAssetUseCase(treeService, assetService, slog.Default()).Execute(context.Background(), RenameAssetInput{
			UserID:      newFixtureUserID("user-1"),
			PageID:      newFixturePageID("missing"),
			OldFilename: tree.AssetName("asset.png"),
			NewFilename: tree.AssetName("renamed.png"),
		})
		expectAssetLocalizedError(err, ErrCodeAssetPageNotFound)

		err = NewDeleteAssetUseCase(treeService, assetService, slog.Default()).Execute(context.Background(), DeleteAssetInput{
			UserID:   newFixtureUserID("user-1"),
			PageID:   newFixturePageID("missing"),
			Filename: tree.AssetName("asset.png"),
		})
		expectAssetLocalizedError(err, ErrCodeAssetPageNotFound)
	})

	ginkgo.It("returns asset listing errors", func() {
		treeService, pageID := setupAssetUseCaseTree()
		listErr := errors.New("list failed")
		uc := &ListAssetsUseCase{
			tree:  treeService,
			asset: failingAssetLister{err: listErr},
		}

		_, err := uc.Execute(context.Background(), ListAssetsInput{PageID: *pageID})

		Expect(err).To(MatchError(listErr))
	})

	ginkgo.It("returns localized asset operation errors", func() {
		treeService, pageID := setupAssetUseCaseTree()
		assetService := coreassets.NewAssetService(ginkgo.GinkgoT().TempDir(), tree.NewSlugService())

		_, err := NewGetAssetUseCase(treeService, assetService).Execute(context.Background(), GetAssetInput{PageID: *pageID, Filename: tree.AssetName("missing.png")})
		expectAssetLocalizedError(err, ErrCodeAssetNotFound)

		_, err = NewUploadAssetUseCase(treeService, assetService, slog.Default()).Execute(context.Background(), UploadAssetInput{
			UserID:   newFixtureUserID("user-1"),
			PageID:   *pageID,
			File:     inMemoryMultipartFile{Reader: bytes.NewReader([]byte("content larger than cap"))},
			Filename: tree.AssetName("asset.png"),
			ByteCap:  shared.MaxBytes(1),
		})
		expectAssetLocalizedError(err, ErrCodeAssetFileTooLarge)

		_, err = NewUploadAssetUseCase(treeService, assetService, slog.Default()).Execute(context.Background(), UploadAssetInput{
			UserID:   newFixtureUserID("user-1"),
			PageID:   *pageID,
			File:     inMemoryMultipartFile{Reader: bytes.NewReader([]byte("content"))},
			Filename: tree.AssetName("asset.png"),
			ByteCap:  shared.MaxBytes(1024),
		})
		Expect(err).NotTo(HaveOccurred())

		_, err = NewRenameAssetUseCase(treeService, assetService, slog.Default()).Execute(context.Background(), RenameAssetInput{
			UserID:      newFixtureUserID("user-1"),
			PageID:      *pageID,
			OldFilename: tree.AssetName("asset.png"),
			NewFilename: tree.AssetName("asset.jpg"),
		})
		expectAssetLocalizedError(err, ErrCodeAssetInvalidExtension)

		_, err = NewRenameAssetUseCase(treeService, assetService, slog.Default()).Execute(context.Background(), RenameAssetInput{
			UserID:      newFixtureUserID("user-1"),
			PageID:      *pageID,
			OldFilename: tree.AssetName("asset.png"),
			NewFilename: tree.AssetName("../escape.png"),
		})
		expectAssetLocalizedError(err, ErrCodeAssetInvalidName)

		_, err = NewUploadAssetUseCase(treeService, assetService, slog.Default()).Execute(context.Background(), UploadAssetInput{
			UserID:   newFixtureUserID("user-1"),
			PageID:   *pageID,
			File:     inMemoryMultipartFile{Reader: bytes.NewReader([]byte("other"))},
			Filename: tree.AssetName("other.png"),
			ByteCap:  shared.MaxBytes(1024),
		})
		Expect(err).NotTo(HaveOccurred())

		_, err = NewRenameAssetUseCase(treeService, assetService, slog.Default()).Execute(context.Background(), RenameAssetInput{
			UserID:      newFixtureUserID("user-1"),
			PageID:      *pageID,
			OldFilename: tree.AssetName("asset.png"),
			NewFilename: tree.AssetName("other.png"),
		})
		expectAssetLocalizedError(err, ErrCodeAssetAlreadyExists)

		err = NewDeleteAssetUseCase(treeService, assetService, slog.Default()).Execute(context.Background(), DeleteAssetInput{
			UserID:   newFixtureUserID("user-1"),
			PageID:   *pageID,
			Filename: tree.AssetName("missing.png"),
		})
		expectAssetLocalizedError(err, ErrCodeAssetNotFound)
	})

	ginkgo.It("returns non-not-found tree lookup errors unchanged", func() {
		treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{
			DataDir: ginkgo.GinkgoT().TempDir(),
			RootDir: ginkgo.GinkgoT().TempDir(),
		})
		assetService := coreassets.NewAssetService(ginkgo.GinkgoT().TempDir(), tree.NewSlugService())
		pageID := newFixturePageID("page-1")

		_, err := NewUploadAssetUseCase(treeService, assetService, slog.Default()).Execute(context.Background(), UploadAssetInput{
			UserID:   newFixtureUserID("user-1"),
			PageID:   pageID,
			File:     inMemoryMultipartFile{Reader: bytes.NewReader([]byte("content"))},
			Filename: tree.AssetName("asset.png"),
			ByteCap:  shared.MaxBytes(1024),
		})
		Expect(err).To(MatchError(tree.ErrTreeNotLoaded))

		_, err = NewListAssetsUseCase(treeService, assetService).Execute(context.Background(), ListAssetsInput{PageID: pageID})
		Expect(err).To(MatchError(tree.ErrTreeNotLoaded))

		_, err = NewGetAssetUseCase(treeService, assetService).Execute(context.Background(), GetAssetInput{PageID: pageID, Filename: tree.AssetName("asset.png")})
		Expect(err).To(MatchError(tree.ErrTreeNotLoaded))

		_, err = NewRenameAssetUseCase(treeService, assetService, slog.Default()).Execute(context.Background(), RenameAssetInput{
			UserID:      newFixtureUserID("user-1"),
			PageID:      pageID,
			OldFilename: tree.AssetName("asset.png"),
			NewFilename: tree.AssetName("renamed.png"),
		})
		Expect(err).To(MatchError(tree.ErrTreeNotLoaded))

		err = NewDeleteAssetUseCase(treeService, assetService, slog.Default()).Execute(context.Background(), DeleteAssetInput{
			UserID:   newFixtureUserID("user-1"),
			PageID:   pageID,
			Filename: tree.AssetName("asset.png"),
		})
		Expect(err).To(MatchError(tree.ErrTreeNotLoaded))
	})
})

var _ = ginkgo.Describe("asset route handlers", func() {
	ginkgo.It("lists, renames, and deletes assets through handlers", func() {
		fixture := newAssetRouteFixture()
		fixture.uploadAsset("asset.png", []byte("content"))

		list := performAssetHandlerRequest(fixture.routerWithUser(), http.MethodGet, "/api/pages/"+fixture.pageID.String()+"/assets", nil, "")
		Expect(list.Code).To(Equal(http.StatusOK), list.Body.String())
		var listBody struct {
			Files []string `json:"files"`
		}
		Expect(json.Unmarshal(list.Body.Bytes(), &listBody)).To(Succeed())
		Expect(listBody.Files).To(Equal([]string{"/assets/" + fixture.pageID.String() + "/asset.png"}))

		rename := performAssetHandlerRequest(
			fixture.routerWithUser(),
			http.MethodPut,
			"/api/pages/"+fixture.pageID.String()+"/assets/rename",
			strings.NewReader(`{"old_filename":"asset.png","new_filename":"renamed.png"}`),
			"application/json",
		)
		Expect(rename.Code).To(Equal(http.StatusOK), rename.Body.String())
		Expect(rename.Body.String()).To(ContainSubstring("/assets/" + fixture.pageID.String() + "/renamed.png"))

		deleted := performAssetHandlerRequest(fixture.routerWithUser(), http.MethodDelete, "/api/pages/"+fixture.pageID.String()+"/assets/renamed.png", nil, "")
		Expect(deleted.Code).To(Equal(http.StatusOK), deleted.Body.String())
		Expect(deleted.Body.String()).To(ContainSubstring(string(MessageIDAssetDeleteSuccess)))
	})

	ginkgo.It("uploads assets and maps upload request errors through the handler", func() {
		fixture := newAssetRouteFixture()
		uploadBody, uploadContentType := assetMultipartBody("asset.png", []byte("content"))

		uploaded := performAssetHandlerRequest(fixture.routerWithUser(), http.MethodPost, "/api/pages/"+fixture.pageID.String()+"/assets", uploadBody, uploadContentType)

		Expect(uploaded.Code).To(Equal(http.StatusCreated), uploaded.Body.String())
		Expect(uploaded.Body.String()).To(ContainSubstring("/assets/" + fixture.pageID.String() + "/asset.png"))

		invalidNameBody, invalidNameContentType := assetMultipartBody(".", []byte("content"))
		invalidName := performAssetHandlerRequest(fixture.routerWithUser(), http.MethodPost, "/api/pages/"+fixture.pageID.String()+"/assets", invalidNameBody, invalidNameContentType)
		Expect(invalidName.Code).To(Equal(http.StatusBadRequest), invalidName.Body.String())
		assertAssetStructuredError(invalidName, "asset_invalid_name", "errors.asset.invalid_name")

		tooLarge := performAssetHandlerRequest(fixture.routerWithUser(), http.MethodPost, "/api/pages/"+fixture.pageID.String()+"/assets", strings.NewReader("bad multipart"), "multipart/form-data; boundary=missing")
		Expect(tooLarge.Code).To(Equal(http.StatusRequestEntityTooLarge), tooLarge.Body.String())
		assertAssetStructuredError(tooLarge, "asset_file_too_large", "errors.asset.file_too_large")

		emptyBody, emptyContentType := assetMultipartBody("", nil)
		missingFile := performAssetHandlerRequest(fixture.routerWithUser(), http.MethodPost, "/api/pages/"+fixture.pageID.String()+"/assets", emptyBody, emptyContentType)
		Expect(missingFile.Code).To(Equal(http.StatusBadRequest), missingFile.Body.String())
		assertAssetStructuredError(missingFile, "asset_missing_file", "errors.asset.missing_file")
	})

	ginkgo.It("returns structured handler errors", func() {
		fixture := newAssetRouteFixture()

		missingPage := performAssetHandlerRequest(fixture.routerWithUser(), http.MethodGet, "/api/pages/missing/assets", nil, "")
		Expect(missingPage.Code).To(Equal(http.StatusNotFound), missingPage.Body.String())
		assertAssetStructuredError(missingPage, "asset_page_not_found", "errors.asset.page_not_found")

		invalidRename := performAssetHandlerRequest(fixture.routerWithUser(), http.MethodPut, "/api/pages/"+fixture.pageID.String()+"/assets/rename", strings.NewReader(`{`), "application/json")
		Expect(invalidRename.Code).To(Equal(http.StatusBadRequest), invalidRename.Body.String())
		assertAssetStructuredError(invalidRename, "asset_invalid_payload", "errors.asset.invalid_payload")

		missingDeleteName := performAssetHandlerRequest(fixture.routerWithUser(), http.MethodDelete, "/api/pages/"+fixture.pageID.String()+"/assets/", nil, "")
		Expect(missingDeleteName.Code).To(Equal(http.StatusBadRequest), missingDeleteName.Body.String())
		assertAssetStructuredError(missingDeleteName, "asset_missing_name", "errors.asset.missing_name")

		missingDeleteFile := performAssetHandlerRequest(fixture.routerWithUser(), http.MethodDelete, "/api/pages/"+fixture.pageID.String()+"/assets/missing.png", nil, "")
		Expect(missingDeleteFile.Code).To(Equal(http.StatusNotFound), missingDeleteFile.Body.String())
		assertAssetStructuredError(missingDeleteFile, "asset_not_found", "errors.asset.not_found")

		fixture.uploadAsset("asset.png", []byte("content"))
		invalidExtension := performAssetHandlerRequest(
			fixture.routerWithUser(),
			http.MethodPut,
			"/api/pages/"+fixture.pageID.String()+"/assets/rename",
			strings.NewReader(`{"old_filename":"asset.png","new_filename":"asset.jpg"}`),
			"application/json",
		)
		Expect(invalidExtension.Code).To(Equal(http.StatusBadRequest), invalidExtension.Body.String())
		assertAssetStructuredError(invalidExtension, "asset_invalid_extension", "errors.asset.invalid_extension")
	})

	ginkgo.It("forbids mutating handlers when user context is missing", func() {
		fixture := newAssetRouteFixture()
		uploadBody, uploadContentType := assetMultipartBody("asset.png", []byte("content"))

		upload := performAssetHandlerRequest(fixture.routerWithoutUser(), http.MethodPost, "/api/pages/"+fixture.pageID.String()+"/assets", uploadBody, uploadContentType)
		Expect(upload.Code).To(Equal(http.StatusForbidden), upload.Body.String())

		rename := performAssetHandlerRequest(fixture.routerWithoutUser(), http.MethodPut, "/api/pages/"+fixture.pageID.String()+"/assets/rename", strings.NewReader(`{"old_filename":"asset.png","new_filename":"renamed.png"}`), "application/json")
		Expect(rename.Code).To(Equal(http.StatusForbidden), rename.Body.String())

		deleteReq := performAssetHandlerRequest(fixture.routerWithoutUser(), http.MethodDelete, "/api/pages/"+fixture.pageID.String()+"/assets/asset.png", nil, "")
		Expect(deleteReq.Code).To(Equal(http.StatusForbidden), deleteReq.Body.String())
	})
})

func setupAssetUseCaseTree() (*tree.TreeService, *tree.PageID) {
	ginkgo.GinkgoHelper()
	treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{
		DataDir: ginkgo.GinkgoT().TempDir(),
		RootDir: ginkgo.GinkgoT().TempDir(),
	})
	Expect(treeService.LoadTree()).To(Succeed())
	kind := tree.NodeKindPage
	pageID, err := treeService.CreateNode("user-1", nil, "Asset Page", "asset-page", &kind)
	Expect(err).NotTo(HaveOccurred())
	return treeService, pageID
}

type assetRouteFixture struct {
	treeService  *tree.TreeService
	assetService *coreassets.AssetService
	pageID       tree.PageID
	routes       *Routes
}

func newAssetRouteFixture() assetRouteFixture {
	ginkgo.GinkgoHelper()
	treeService, pageID := setupAssetUseCaseTree()
	assetService := coreassets.NewAssetService(ginkgo.GinkgoT().TempDir(), tree.NewSlugService())
	routes := NewRoutes(RoutesConfig{
		Upload: NewUploadAssetUseCase(treeService, assetService, slog.Default()),
		List:   NewListAssetsUseCase(treeService, assetService),
		Rename: NewRenameAssetUseCase(treeService, assetService, slog.Default()),
		Delete: NewDeleteAssetUseCase(treeService, assetService, slog.Default()),
		Log:    slog.Default(),
	})
	return assetRouteFixture{
		treeService:  treeService,
		assetService: assetService,
		pageID:       *pageID,
		routes:       routes,
	}
}

func (fixture assetRouteFixture) uploadAsset(filename string, content []byte) {
	ginkgo.GinkgoHelper()
	_, err := NewUploadAssetUseCase(fixture.treeService, fixture.assetService, slog.Default()).Execute(context.Background(), UploadAssetInput{
		UserID:   newFixtureUserID("user-1"),
		PageID:   fixture.pageID,
		File:     inMemoryMultipartFile{Reader: bytes.NewReader(content)},
		Filename: tree.AssetName(filename),
		ByteCap:  shared.MaxBytes(1024),
	})
	Expect(err).NotTo(HaveOccurred())
}

func (fixture assetRouteFixture) routerWithUser() http.Handler {
	ginkgo.GinkgoHelper()
	return fixture.router(func(c *gin.Context) {
		c.Set("user", &coreauth.User{ID: "user-1"})
	})
}

func (fixture assetRouteFixture) routerWithoutUser() http.Handler {
	ginkgo.GinkgoHelper()
	return fixture.router(nil)
}

func (fixture assetRouteFixture) router(setup gin.HandlerFunc) http.Handler {
	ginkgo.GinkgoHelper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	if setup != nil {
		router.Use(setup)
	}
	router.POST("/api/pages/:id/assets", fixture.routes.handleUpload(shared.MaxBytes(64*1024)))
	router.GET("/api/pages/:id/assets", fixture.routes.handleList)
	router.PUT("/api/pages/:id/assets/rename", fixture.routes.handleRename)
	router.DELETE("/api/pages/:id/assets/:name", fixture.routes.handleDelete)
	router.DELETE("/api/pages/:id/assets/", fixture.routes.handleDelete)
	return router
}

func assetMultipartBody(filename string, fileContent []byte) (*bytes.Buffer, string) {
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

func performAssetHandlerRequest(router http.Handler, method string, path string, body io.Reader, contentType string) *httptest.ResponseRecorder {
	ginkgo.GinkgoHelper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, body)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	router.ServeHTTP(rec, req)
	return rec
}

func expectAssetLocalizedError(err error, code sharederrors.ErrorCode) {
	ginkgo.GinkgoHelper()
	var localized *sharederrors.LocalizedError
	Expect(errors.As(err, &localized)).To(BeTrue(), "error should be localized: %v", err)
	Expect(localized.Code).To(Equal(code))
}

func assertAssetStructuredError(rec *httptest.ResponseRecorder, code string, messageID string) {
	ginkgo.GinkgoHelper()
	var body AssetErrorResponse
	Expect(json.Unmarshal(rec.Body.Bytes(), &body)).To(Succeed(), rec.Body.String())
	Expect(body.Error.Code.String()).To(Equal(code))
	Expect(body.Error.MessageID.String()).To(Equal(messageID))
}

type failingAssetLister struct {
	err error
}

func (l failingAssetLister) ListAssetsForPage(*tree.PageNode) ([]string, error) {
	return nil, l.err
}

type closeErrorFile struct {
	err error
}

func (f closeErrorFile) Close() error {
	return f.err
}

var _ io.Reader = inMemoryMultipartFile{}
