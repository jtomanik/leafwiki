package assets

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/gin-gonic/gin"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	coreassets "github.com/perber/wiki/internal/core/assets"
	"github.com/perber/wiki/internal/core/shared"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/core/tree"
	testmatchers "github.com/perber/wiki/internal/test_utils/matchers"
)

var _ = ginkgo.Describe("asset helpers", func() {
	ginkgo.It("assigns HTTP status classes to validation missing duplicate and unknown asset failures", ginkgo.Label("unit"), func() {
		Expect(assetErrorStatus(ErrCodeAssetFileTooLarge)).To(Equal(http.StatusRequestEntityTooLarge))
		Expect(assetErrorStatus(ErrCodeAssetMissingFile)).To(Equal(http.StatusBadRequest))
		Expect(assetErrorStatus(ErrCodeAssetInvalidName)).To(Equal(http.StatusBadRequest))
		Expect(assetErrorStatus(ErrCodeAssetPageNotFound)).To(Equal(http.StatusNotFound))
		Expect(assetErrorStatus(ErrCodeAssetAlreadyExists)).To(Equal(http.StatusConflict))
		Expect(assetErrorStatus(newFixtureAssetErrorCode("unknown"))).To(Equal(http.StatusInternalServerError))
	})

	ginkgo.It("asset localized error constructors preserve code and cause", ginkgo.Label("unit"), func() {
		tooLarge := NewAssetFileTooLargeError()
		Expect(tooLarge).To(testmatchers.MatchLocalizedError(ErrCodeAssetFileTooLarge, sharederrors.MessageIDForCode(ErrCodeAssetFileTooLarge)))

		cause := errors.New("bad json")
		invalid := NewAssetInvalidPayloadError(cause)
		Expect(invalid).To(testmatchers.MatchLocalizedError(ErrCodeAssetInvalidPayload, sharederrors.MessageIDForCode(ErrCodeAssetInvalidPayload)))
		Expect(invalid).To(MatchError(cause))
	})

	ginkgo.It("logs uploaded file close errors", ginkgo.Label("unit"), func() {
		records := &assetLogRecords{}
		logger := slog.New(records)
		closeErr := &uploadedAssetCloseCause{}

		logUploadedAssetFileClose(logger, closeErrorFile{err: closeErr})

		Expect(records.Events).To(ConsistOf(matchUploadedAssetCloseErrorLog(closeErr)))
	})

	ginkgo.It("returns structured asset errors for localized failures and internal responses for generic failures", ginkgo.Label("integration"), func() {
		gin.SetMode(gin.TestMode)

		localizedRec := httptest.NewRecorder()
		localizedCtx, _ := gin.CreateTestContext(localizedRec)
		respondWithAssetError(localizedCtx, sharederrors.NewLocalizedErrorFromCode(ErrCodeAssetAlreadyExists, nil, "asset.png"))
		Expect(localizedRec).To(testmatchers.HaveHTTPStructuredError(http.StatusConflict, ErrCodeAssetAlreadyExists, sharederrors.MessageIDForCode(ErrCodeAssetAlreadyExists)))

		genericRec := httptest.NewRecorder()
		genericCtx, _ := gin.CreateTestContext(genericRec)
		respondWithAssetError(genericCtx, errors.New("boom"))
		Expect(genericRec).To(testmatchers.HaveHTTPStructuredError(http.StatusInternalServerError, ErrCodeAssetInternalError, sharederrors.MessageIDForCode(ErrCodeAssetInternalError)))
	})

	ginkgo.It("prefers extension MIME types and falls back to content sniffing", ginkgo.Label("unit"), func() {
		Expect(DetectAssetMIMEType("style.css", []byte("not css"))).To(Equal("text/css; charset=utf-8"))
		Expect(DetectAssetMIMEType("asset.unknownext", []byte("%PDF-1.7\n"))).To(Equal("application/pdf"))
	})

	ginkgo.It("preserves asset use-case dependency contracts before route registration", ginkgo.Label("unit"), func() {
		treeService := tree.NewTreeService(assetTempDir())
		assetService := coreassets.NewAssetService(assetTempDir(), tree.NewSlugService())
		logger := slog.New(slog.NewTextHandler(io.Discard, nil))
		assetsDir := assetTempDir()

		upload := NewUploadAssetUseCase(treeService, assetService, logger)
		list := NewListAssetsUseCase(treeService, assetService)
		get := NewGetAssetUseCase(treeService, assetService)
		rename := NewRenameAssetUseCase(treeService, assetService, logger)
		deleteAsset := NewDeleteAssetUseCase(treeService, assetService, logger)
		routes := NewRoutes(RoutesConfig{
			Upload:    upload,
			List:      list,
			Rename:    rename,
			Delete:    deleteAsset,
			AssetsDir: assetsDir,
			Log:       logger,
		})

		Expect(upload).To(matchUploadAssetDependencies(treeService, assetService, logger))
		Expect(list).To(matchListAssetDependencies(treeService, assetService))
		Expect(get).To(matchGetAssetDependencies(treeService, assetService))
		Expect(rename).To(matchRenameAssetDependencies(treeService, assetService, logger))
		Expect(deleteAsset).To(matchDeleteAssetDependencies(treeService, assetService, logger))
		Expect(routes).To(matchAssetRouteDependencies(upload, list, rename, deleteAsset, assetsDir, logger))
		Expect(apiSuccessMessage(MessageIDAssetDeleteSuccess)).NotTo(BeEmpty())
	})
})

var _ = ginkgo.Describe("asset use cases", ginkgo.Label("integration"), func() {
	ginkgo.It("upload, list, get, rename, and delete round-trip a page asset", func() {
		treeService, pageID := setupAssetUseCaseTree()
		assetService := coreassets.NewAssetService(assetTempDir(), tree.NewSlugService())

		uploadOut, err := NewUploadAssetUseCase(treeService, assetService, slog.Default()).Execute(context.Background(), UploadAssetInput{
			UserID:   newFixtureUserID("user-1"),
			PageID:   *pageID,
			File:     inMemoryMultipartFile{Reader: bytes.NewReader([]byte("hello image"))},
			Filename: newFixtureAssetName("my-image.png"),
			ByteCap:  shared.MaxBytes(1024),
		})
		Expect(err).To(Succeed())
		Expect(uploadOut.URL).To(Equal(pageAssetURL(*pageID, tree.AssetNameFromString("my-image.png"))))

		listOut, err := NewListAssetsUseCase(treeService, assetService).Execute(context.Background(), ListAssetsInput{PageID: *pageID})
		Expect(err).To(Succeed())
		Expect(listOut.Files).To(Equal([]string{pageAssetURL(*pageID, tree.AssetNameFromString("my-image.png"))}))

		getOut, err := NewGetAssetUseCase(treeService, assetService).Execute(context.Background(), GetAssetInput{
			PageID:   *pageID,
			Filename: newFixtureAssetName("my-image.png"),
		})
		Expect(err).To(Succeed())
		Expect(getOut).To(matchAssetDownload(newFixtureAssetName("my-image.png"), "image/png", []byte("hello image")))

		renameOut, err := NewRenameAssetUseCase(treeService, assetService, slog.Default()).Execute(context.Background(), RenameAssetInput{
			UserID:      newFixtureUserID("user-1"),
			PageID:      *pageID,
			OldFilename: newFixtureAssetName("my-image.png"),
			NewFilename: newFixtureAssetName("renamed.png"),
		})
		Expect(err).To(Succeed())
		Expect(renameOut.URL).To(Equal(pageAssetURL(*pageID, tree.AssetNameFromString("renamed.png"))))

		Expect(NewDeleteAssetUseCase(treeService, assetService, slog.Default()).Execute(context.Background(), DeleteAssetInput{
			UserID:   newFixtureUserID("user-1"),
			PageID:   *pageID,
			Filename: newFixtureAssetName("renamed.png"),
		})).To(Succeed())

		listOut, err = NewListAssetsUseCase(treeService, assetService).Execute(context.Background(), ListAssetsInput{PageID: *pageID})
		Expect(err).To(Succeed())
		Expect(listOut.Files).To(BeEmpty())
	})

	ginkgo.It("use cases return localized page-not-found errors", func() {
		treeService, _ := setupAssetUseCaseTree()
		assetService := coreassets.NewAssetService(assetTempDir(), tree.NewSlugService())

		_, err := NewUploadAssetUseCase(treeService, assetService, slog.Default()).Execute(context.Background(), UploadAssetInput{
			UserID:   newFixtureUserID("user-1"),
			PageID:   newFixturePageID("missing"),
			File:     inMemoryMultipartFile{Reader: bytes.NewReader([]byte("content"))},
			Filename: newFixtureAssetName("asset.png"),
			ByteCap:  shared.MaxBytes(1024),
		})
		Expect(err).To(matchAssetLocalizedError(ErrCodeAssetPageNotFound))

		_, err = NewListAssetsUseCase(treeService, assetService).Execute(context.Background(), ListAssetsInput{PageID: newFixturePageID("missing")})
		Expect(err).To(matchAssetLocalizedError(ErrCodeAssetPageNotFound))

		_, err = NewGetAssetUseCase(treeService, assetService).Execute(context.Background(), GetAssetInput{PageID: newFixturePageID("missing"), Filename: newFixtureAssetName("asset.png")})
		Expect(err).To(matchAssetLocalizedError(ErrCodeAssetPageNotFound))

		_, err = NewRenameAssetUseCase(treeService, assetService, slog.Default()).Execute(context.Background(), RenameAssetInput{
			UserID:      newFixtureUserID("user-1"),
			PageID:      newFixturePageID("missing"),
			OldFilename: newFixtureAssetName("asset.png"),
			NewFilename: newFixtureAssetName("renamed.png"),
		})
		Expect(err).To(matchAssetLocalizedError(ErrCodeAssetPageNotFound))

		err = NewDeleteAssetUseCase(treeService, assetService, slog.Default()).Execute(context.Background(), DeleteAssetInput{
			UserID:   newFixtureUserID("user-1"),
			PageID:   newFixturePageID("missing"),
			Filename: newFixtureAssetName("asset.png"),
		})
		Expect(err).To(matchAssetLocalizedError(ErrCodeAssetPageNotFound))
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
		assetService := coreassets.NewAssetService(assetTempDir(), tree.NewSlugService())

		_, err := NewGetAssetUseCase(treeService, assetService).Execute(context.Background(), GetAssetInput{PageID: *pageID, Filename: newFixtureAssetName("missing.png")})
		Expect(err).To(matchAssetLocalizedError(ErrCodeAssetNotFound))

		_, err = NewUploadAssetUseCase(treeService, assetService, slog.Default()).Execute(context.Background(), UploadAssetInput{
			UserID:   newFixtureUserID("user-1"),
			PageID:   *pageID,
			File:     inMemoryMultipartFile{Reader: bytes.NewReader([]byte("content larger than cap"))},
			Filename: newFixtureAssetName("asset.png"),
			ByteCap:  shared.MaxBytes(1),
		})
		Expect(err).To(matchAssetLocalizedError(ErrCodeAssetFileTooLarge))

		_, err = NewUploadAssetUseCase(treeService, assetService, slog.Default()).Execute(context.Background(), UploadAssetInput{
			UserID:   newFixtureUserID("user-1"),
			PageID:   *pageID,
			File:     inMemoryMultipartFile{Reader: bytes.NewReader([]byte("content"))},
			Filename: newFixtureAssetName("asset.png"),
			ByteCap:  shared.MaxBytes(1024),
		})
		Expect(err).To(Succeed())

		_, err = NewRenameAssetUseCase(treeService, assetService, slog.Default()).Execute(context.Background(), RenameAssetInput{
			UserID:      newFixtureUserID("user-1"),
			PageID:      *pageID,
			OldFilename: newFixtureAssetName("asset.png"),
			NewFilename: newFixtureAssetName("asset.jpg"),
		})
		Expect(err).To(matchAssetLocalizedError(ErrCodeAssetInvalidExtension))

		_, err = NewRenameAssetUseCase(treeService, assetService, slog.Default()).Execute(context.Background(), RenameAssetInput{
			UserID:      newFixtureUserID("user-1"),
			PageID:      *pageID,
			OldFilename: newFixtureAssetName("asset.png"),
			NewFilename: newFixtureAssetName("../escape.png"),
		})
		Expect(err).To(matchAssetLocalizedError(ErrCodeAssetInvalidName))

		_, err = NewUploadAssetUseCase(treeService, assetService, slog.Default()).Execute(context.Background(), UploadAssetInput{
			UserID:   newFixtureUserID("user-1"),
			PageID:   *pageID,
			File:     inMemoryMultipartFile{Reader: bytes.NewReader([]byte("other"))},
			Filename: newFixtureAssetName("other.png"),
			ByteCap:  shared.MaxBytes(1024),
		})
		Expect(err).To(Succeed())

		_, err = NewRenameAssetUseCase(treeService, assetService, slog.Default()).Execute(context.Background(), RenameAssetInput{
			UserID:      newFixtureUserID("user-1"),
			PageID:      *pageID,
			OldFilename: newFixtureAssetName("asset.png"),
			NewFilename: newFixtureAssetName("other.png"),
		})
		Expect(err).To(matchAssetLocalizedError(ErrCodeAssetAlreadyExists))

		err = NewDeleteAssetUseCase(treeService, assetService, slog.Default()).Execute(context.Background(), DeleteAssetInput{
			UserID:   newFixtureUserID("user-1"),
			PageID:   *pageID,
			Filename: newFixtureAssetName("missing.png"),
		})
		Expect(err).To(matchAssetLocalizedError(ErrCodeAssetNotFound))
	})

	ginkgo.It("returns non-not-found tree lookup errors unchanged", func() {
		treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{
			DataDir: assetTempDir(),
			RootDir: assetTempDir(),
		})
		assetService := coreassets.NewAssetService(assetTempDir(), tree.NewSlugService())
		pageID := newFixturePageID("page-1")

		_, err := NewUploadAssetUseCase(treeService, assetService, slog.Default()).Execute(context.Background(), UploadAssetInput{
			UserID:   newFixtureUserID("user-1"),
			PageID:   pageID,
			File:     inMemoryMultipartFile{Reader: bytes.NewReader([]byte("content"))},
			Filename: newFixtureAssetName("asset.png"),
			ByteCap:  shared.MaxBytes(1024),
		})
		Expect(err).To(MatchError(tree.ErrTreeNotLoaded))

		_, err = NewListAssetsUseCase(treeService, assetService).Execute(context.Background(), ListAssetsInput{PageID: pageID})
		Expect(err).To(MatchError(tree.ErrTreeNotLoaded))

		_, err = NewGetAssetUseCase(treeService, assetService).Execute(context.Background(), GetAssetInput{PageID: pageID, Filename: newFixtureAssetName("asset.png")})
		Expect(err).To(MatchError(tree.ErrTreeNotLoaded))

		_, err = NewRenameAssetUseCase(treeService, assetService, slog.Default()).Execute(context.Background(), RenameAssetInput{
			UserID:      newFixtureUserID("user-1"),
			PageID:      pageID,
			OldFilename: newFixtureAssetName("asset.png"),
			NewFilename: newFixtureAssetName("renamed.png"),
		})
		Expect(err).To(MatchError(tree.ErrTreeNotLoaded))

		err = NewDeleteAssetUseCase(treeService, assetService, slog.Default()).Execute(context.Background(), DeleteAssetInput{
			UserID:   newFixtureUserID("user-1"),
			PageID:   pageID,
			Filename: newFixtureAssetName("asset.png"),
		})
		Expect(err).To(MatchError(tree.ErrTreeNotLoaded))
	})
})

var _ = ginkgo.Describe("asset route handlers", ginkgo.Label("integration"), func() {
	ginkgo.It("lists, renames, and deletes assets through handlers", func() {
		fixture := newAssetRouteFixture()
		fixture.uploadAsset(newFixtureAssetName("asset.png"), []byte("content"))

		list := performAssetHandlerRequest(fixture.routerWithUser(), http.MethodGet, assetsPagePath(fixture.pageID), nil, "")
		Expect(list).To(HaveHTTPStatus(http.StatusOK), list.Body.String())
		var listBody struct {
			Files []string `json:"files"`
		}
		Expect(json.Unmarshal(list.Body.Bytes(), &listBody)).To(Succeed())
		Expect(listBody.Files).To(Equal([]string{pageAssetURL(fixture.pageID, tree.AssetNameFromString("asset.png"))}))

		rename := performAssetHandlerRequest(
			fixture.routerWithUser(),
			http.MethodPut,
			assetRenamePath(fixture.pageID),
			strings.NewReader(`{"old_filename":"asset.png","new_filename":"renamed.png"}`),
			"application/json",
		)
		Expect(rename).To(HaveHTTPStatus(http.StatusOK), rename.Body.String())
		var renameBody struct {
			URL string `json:"url"`
		}
		Expect(json.Unmarshal(rename.Body.Bytes(), &renameBody)).To(Succeed())
		Expect(renameBody.URL).To(Equal(pageAssetURL(fixture.pageID, tree.AssetNameFromString("renamed.png"))))

		deleted := performAssetHandlerRequest(fixture.routerWithUser(), http.MethodDelete, assetFilePath(fixture.pageID, tree.AssetNameFromString("renamed.png")), nil, "")
		Expect(deleted).To(HaveHTTPStatus(http.StatusOK), deleted.Body.String())
		var deletedBody struct {
			MessageID sharederrors.MessageID `json:"messageId"`
		}
		Expect(json.Unmarshal(deleted.Body.Bytes(), &deletedBody)).To(Succeed())
		Expect(deletedBody).To(testmatchers.HaveMessageID(MessageIDAssetDeleteSuccess))
	})

	ginkgo.It("uploads assets and maps upload request errors through the handler", func() {
		fixture := newAssetRouteFixture()
		uploadBody, uploadContentType := assetMultipartBody(newFixtureAssetName("asset.png"), []byte("content"))

		uploaded := performAssetHandlerRequest(fixture.routerWithUser(), http.MethodPost, assetsPagePath(fixture.pageID), uploadBody, uploadContentType)

		Expect(uploaded).To(HaveHTTPStatus(http.StatusCreated), uploaded.Body.String())
		var uploadedBody struct {
			File string `json:"file"`
		}
		Expect(json.Unmarshal(uploaded.Body.Bytes(), &uploadedBody)).To(Succeed())
		Expect(uploadedBody.File).To(Equal(pageAssetURL(fixture.pageID, tree.AssetNameFromString("asset.png"))))

		invalidNameBody, invalidNameContentType := assetMultipartBody(newFixtureAssetName("."), []byte("content"))
		invalidName := performAssetHandlerRequest(fixture.routerWithUser(), http.MethodPost, assetsPagePath(fixture.pageID), invalidNameBody, invalidNameContentType)
		Expect(invalidName).To(matchAssetStructuredError(http.StatusBadRequest, ErrCodeAssetInvalidName), invalidName.Body.String())

		tooLarge := performAssetHandlerRequest(fixture.routerWithUser(), http.MethodPost, assetsPagePath(fixture.pageID), strings.NewReader("bad multipart"), "multipart/form-data; boundary=missing")
		Expect(tooLarge).To(matchAssetStructuredError(http.StatusRequestEntityTooLarge, ErrCodeAssetFileTooLarge), tooLarge.Body.String())

		emptyBody, emptyContentType := assetMultipartBody(newFixtureAssetName(""), nil)
		missingFile := performAssetHandlerRequest(fixture.routerWithUser(), http.MethodPost, assetsPagePath(fixture.pageID), emptyBody, emptyContentType)
		Expect(missingFile).To(matchAssetStructuredError(http.StatusBadRequest, ErrCodeAssetMissingFile), missingFile.Body.String())
	})

	ginkgo.It("returns structured handler errors", func() {
		fixture := newAssetRouteFixture()

		missingPage := performAssetHandlerRequest(fixture.routerWithUser(), http.MethodGet, assetsPagePath(newFixturePageID("missing")), nil, "")
		Expect(missingPage).To(matchAssetStructuredError(http.StatusNotFound, ErrCodeAssetPageNotFound), missingPage.Body.String())

		invalidRename := performAssetHandlerRequest(fixture.routerWithUser(), http.MethodPut, assetRenamePath(fixture.pageID), strings.NewReader(`{`), "application/json")
		Expect(invalidRename).To(matchAssetStructuredError(http.StatusBadRequest, ErrCodeAssetInvalidPayload), invalidRename.Body.String())

		missingDeleteName := performAssetHandlerRequest(fixture.routerWithUser(), http.MethodDelete, assetsPagePath(fixture.pageID)+"/", nil, "")
		Expect(missingDeleteName).To(matchAssetStructuredError(http.StatusBadRequest, ErrCodeAssetMissingName), missingDeleteName.Body.String())

		missingDeleteFile := performAssetHandlerRequest(fixture.routerWithUser(), http.MethodDelete, assetFilePath(fixture.pageID, tree.AssetNameFromString("missing.png")), nil, "")
		Expect(missingDeleteFile).To(matchAssetStructuredError(http.StatusNotFound, ErrCodeAssetNotFound), missingDeleteFile.Body.String())

		fixture.uploadAsset(newFixtureAssetName("asset.png"), []byte("content"))
		invalidExtension := performAssetHandlerRequest(
			fixture.routerWithUser(),
			http.MethodPut,
			assetRenamePath(fixture.pageID),
			strings.NewReader(`{"old_filename":"asset.png","new_filename":"asset.jpg"}`),
			"application/json",
		)
		Expect(invalidExtension).To(matchAssetStructuredError(http.StatusBadRequest, ErrCodeAssetInvalidExtension), invalidExtension.Body.String())
	})

	ginkgo.It("forbids mutating handlers when user context is missing", func() {
		fixture := newAssetRouteFixture()
		uploadBody, uploadContentType := assetMultipartBody(newFixtureAssetName("asset.png"), []byte("content"))

		upload := performAssetHandlerRequest(fixture.routerWithoutUser(), http.MethodPost, assetsPagePath(fixture.pageID), uploadBody, uploadContentType)
		Expect(upload).To(HaveHTTPStatus(http.StatusForbidden), upload.Body.String())

		rename := performAssetHandlerRequest(fixture.routerWithoutUser(), http.MethodPut, assetRenamePath(fixture.pageID), strings.NewReader(`{"old_filename":"asset.png","new_filename":"renamed.png"}`), "application/json")
		Expect(rename).To(HaveHTTPStatus(http.StatusForbidden), rename.Body.String())

		deleteReq := performAssetHandlerRequest(fixture.routerWithoutUser(), http.MethodDelete, assetFilePath(fixture.pageID, tree.AssetNameFromString("asset.png")), nil, "")
		Expect(deleteReq).To(HaveHTTPStatus(http.StatusForbidden), deleteReq.Body.String())
	})
})
