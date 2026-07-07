package assets

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"

	"github.com/gin-gonic/gin"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"

	coreassets "github.com/perber/wiki/internal/core/assets"
	coreauth "github.com/perber/wiki/internal/core/auth"
	"github.com/perber/wiki/internal/core/shared"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/core/tree"
	testmatchers "github.com/perber/wiki/internal/test_utils/matchers"
)

type inMemoryMultipartFile struct {
	*bytes.Reader
}

func (f inMemoryMultipartFile) Close() error {
	return nil
}

func matchAssetLocalizedError(code sharederrors.ErrorCode) types.GomegaMatcher {
	return testmatchers.MatchLocalizedError(code, sharederrors.MessageIDForCode(code))
}

func matchAssetStructuredError(status int, code sharederrors.ErrorCode) types.GomegaMatcher {
	return testmatchers.HaveHTTPStructuredError(status, code, sharederrors.MessageIDForCode(code))
}

func matchAssetDownload(filename tree.AssetName, mimeType string, content []byte) types.GomegaMatcher {
	return gstruct.PointTo(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"Filename": Equal(filename),
		"MIMEType": Equal(mimeType),
		"Content":  Equal(content),
	}))
}

func matchUploadAssetDependencies(treeService *tree.TreeService, assetService *coreassets.AssetService, logger *slog.Logger) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(uploadAssetDependencyContractFor, Equal(assetDependencyContract{
		Tree:  treeService,
		Asset: assetService,
		Log:   logger,
	}))
}

func matchListAssetDependencies(treeService *tree.TreeService, assetService *coreassets.AssetService) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(listAssetDependencyContractFor, Equal(assetDependencyContract{
		Tree:  treeService,
		Asset: assetService,
	}))
}

func matchGetAssetDependencies(treeService *tree.TreeService, assetService *coreassets.AssetService) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(getAssetDependencyContractFor, Equal(assetDependencyContract{
		Tree:  treeService,
		Asset: assetService,
	}))
}

func matchRenameAssetDependencies(treeService *tree.TreeService, assetService *coreassets.AssetService, logger *slog.Logger) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(renameAssetDependencyContractFor, Equal(assetDependencyContract{
		Tree:  treeService,
		Asset: assetService,
		Log:   logger,
	}))
}

func matchDeleteAssetDependencies(treeService *tree.TreeService, assetService *coreassets.AssetService, logger *slog.Logger) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(deleteAssetDependencyContractFor, Equal(assetDependencyContract{
		Tree:  treeService,
		Asset: assetService,
		Log:   logger,
	}))
}

func matchAssetRouteDependencies(
	upload *UploadAssetUseCase,
	list *ListAssetsUseCase,
	rename *RenameAssetUseCase,
	deleteAsset *DeleteAssetUseCase,
	assetsDir string,
	logger *slog.Logger,
) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(assetRouteDependencyContractFor, Equal(assetRouteDependencyContract{
		Upload:    upload,
		List:      list,
		Rename:    rename,
		Delete:    deleteAsset,
		AssetsDir: assetsDir,
		Log:       logger,
	}))
}

type assetDependencyContract struct {
	Tree  *tree.TreeService
	Asset any
	Log   *slog.Logger
}

func uploadAssetDependencyContractFor(uc *UploadAssetUseCase) assetDependencyContract {
	if uc == nil {
		return assetDependencyContract{}
	}
	return assetDependencyContract{Tree: uc.tree, Asset: uc.asset, Log: uc.log}
}

func listAssetDependencyContractFor(uc *ListAssetsUseCase) assetDependencyContract {
	if uc == nil {
		return assetDependencyContract{}
	}
	return assetDependencyContract{Tree: uc.tree, Asset: uc.asset}
}

func getAssetDependencyContractFor(uc *GetAssetUseCase) assetDependencyContract {
	if uc == nil {
		return assetDependencyContract{}
	}
	return assetDependencyContract{Tree: uc.tree, Asset: uc.asset}
}

func renameAssetDependencyContractFor(uc *RenameAssetUseCase) assetDependencyContract {
	if uc == nil {
		return assetDependencyContract{}
	}
	return assetDependencyContract{Tree: uc.tree, Asset: uc.asset, Log: uc.log}
}

func deleteAssetDependencyContractFor(uc *DeleteAssetUseCase) assetDependencyContract {
	if uc == nil {
		return assetDependencyContract{}
	}
	return assetDependencyContract{Tree: uc.tree, Asset: uc.asset, Log: uc.log}
}

type assetRouteDependencyContract struct {
	Upload    *UploadAssetUseCase
	List      *ListAssetsUseCase
	Rename    *RenameAssetUseCase
	Delete    *DeleteAssetUseCase
	AssetsDir string
	Log       *slog.Logger
}

func assetRouteDependencyContractFor(routes *Routes) assetRouteDependencyContract {
	if routes == nil {
		return assetRouteDependencyContract{}
	}
	return assetRouteDependencyContract{
		Upload:    routes.upload,
		List:      routes.list,
		Rename:    routes.rename,
		Delete:    routes.delete,
		AssetsDir: routes.assetsDir,
		Log:       routes.log,
	}
}

type assetLogEvent struct {
	Level slog.Level
	Attrs map[string]any
}

type assetLogRecords struct {
	Events []assetLogEvent
}

type uploadedAssetCloseCause struct{}

func (uploadedAssetCloseCause) Error() string {
	return "uploaded asset close cause"
}

func (r *assetLogRecords) Enabled(context.Context, slog.Level) bool {
	return true
}

func (r *assetLogRecords) Handle(_ context.Context, record slog.Record) error {
	event := assetLogEvent{
		Level: record.Level,
		Attrs: map[string]any{},
	}
	record.Attrs(func(attr slog.Attr) bool {
		event.Attrs[attr.Key] = attr.Value.Any()
		return true
	})
	r.Events = append(r.Events, event)
	return nil
}

func (r *assetLogRecords) WithAttrs([]slog.Attr) slog.Handler {
	return r
}

func (r *assetLogRecords) WithGroup(string) slog.Handler {
	return r
}

func matchUploadedAssetCloseErrorLog(closeErr error) types.GomegaMatcher {
	return gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"Level": Equal(slog.LevelError),
		"Attrs": HaveKeyWithValue("error", BeIdenticalTo(closeErr)),
	})
}

func setupAssetUseCaseTree() (*tree.TreeService, *tree.PageID) {
	ginkgo.GinkgoHelper()
	treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{
		DataDir: assetTempDir(),
		RootDir: assetTempDir(),
	})
	Expect(treeService.LoadTree()).To(Succeed())
	kind := tree.NodeKindPage
	pageID, err := treeService.CreateNode(newFixtureUserID("user-1"), nil, "Asset Page", newFixtureSlug("asset-page"), &kind)
	Expect(err).To(Succeed())
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
	assetService := coreassets.NewAssetService(assetTempDir(), tree.NewSlugService())
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

func (fixture assetRouteFixture) uploadAsset(filename tree.AssetName, content []byte) {
	ginkgo.GinkgoHelper()
	_, err := NewUploadAssetUseCase(fixture.treeService, fixture.assetService, slog.Default()).Execute(context.Background(), UploadAssetInput{
		UserID:   newFixtureUserID("user-1"),
		PageID:   fixture.pageID,
		File:     inMemoryMultipartFile{Reader: bytes.NewReader(content)},
		Filename: filename,
		ByteCap:  shared.MaxBytes(1024),
	})
	Expect(err).To(Succeed())
}

func (fixture assetRouteFixture) routerWithUser() http.Handler {
	ginkgo.GinkgoHelper()
	return fixture.router(func(c *gin.Context) {
		c.Set("user", &coreauth.User{ID: newFixtureUserID("user-1")})
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

func assetMultipartBody(filename tree.AssetName, fileContent []byte) (*bytes.Buffer, string) {
	ginkgo.GinkgoHelper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if filename != "" {
		part, err := writer.CreateFormFile("file", filename.Filename())
		Expect(err).To(Succeed())
		_, err = part.Write(fileContent)
		Expect(err).To(Succeed())
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

func assetsPagePath(pageID tree.PageID) string {
	return "/api/pages/" + pageID.MetadataValue() + "/assets"
}

func assetRenamePath(pageID tree.PageID) string {
	return assetsPagePath(pageID) + "/rename"
}

func assetFilePath(pageID tree.PageID, name tree.AssetName) string {
	return assetsPagePath(pageID) + "/" + name.Filename()
}

func pageAssetURL(pageID tree.PageID, name tree.AssetName) string {
	return "/assets/" + pageID.MetadataValue() + "/" + name.Filename()
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
