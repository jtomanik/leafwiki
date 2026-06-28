package importer

import (
	"archive/zip"
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
	"time"

	"github.com/gin-gonic/gin"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/perber/wiki/internal/core/shared"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/core/tree"
	httpinternal "github.com/perber/wiki/internal/http"
	coreimporter "github.com/perber/wiki/internal/importer"
)

var _ = ginkgo.Describe("importer use cases", func() {
	ginkgo.It("creates an import plan from an uploaded zip", func() {
		svc, _ := newImporterServiceFixture()
		uc := NewCreateImportPlanUseCase(svc)

		out, err := uc.Execute(context.Background(), CreateImportPlanInput{
			File:           bytes.NewReader(importerZipBytes("Imported.md", "# Imported\nbody")),
			TargetBasePath: "docs",
		})

		Expect(err).NotTo(HaveOccurred())
		Expect(out.Plan).NotTo(BeNil())
		Expect(out.Plan.ExecutionStatus).To(Equal(coreimporter.ExecutionStatusPlanned))
		Expect(out.Plan.Items).To(HaveLen(1))
		Expect(out.Plan.Items[0].TargetPath).To(Equal(tree.RoutePathFromString("docs/imported")))
	})

	ginkgo.It("returns plan creation errors from invalid uploads", func() {
		svc, _ := newImporterServiceFixture()
		uc := NewCreateImportPlanUseCase(svc)

		out, err := uc.Execute(context.Background(), CreateImportPlanInput{File: strings.NewReader("not a zip")})

		Expect(out).To(BeNil())
		Expect(err).To(MatchError(ContainSubstring("extract zip to temp")))
	})

	ginkgo.It("returns current-plan lookup errors after successful plan creation", func() {
		getErr := errors.New("get current plan failed")
		uc := &CreateImportPlanUseCase{svc: fakeImporterPlanCreator{getErr: getErr}}

		out, err := uc.Execute(context.Background(), CreateImportPlanInput{File: strings.NewReader("ignored")})

		Expect(out).To(BeNil())
		Expect(errors.Is(err, getErr)).To(BeTrue())
	})

	ginkgo.It("gets the current plan or maps a missing plan to a localized error", func() {
		svc, store := newImporterServiceFixture()
		uc := NewGetImportPlanUseCase(svc)

		out, err := uc.Execute(context.Background())
		Expect(out).To(BeNil())
		expectLocalizedImporterError(err, ErrCodeImporterNoPlan)

		seedImporterPlan(store, coreimporter.ExecutionStatusPlanned)
		out, err = uc.Execute(context.Background())
		Expect(err).NotTo(HaveOccurred())
		Expect(out.Plan.ID).To(Equal("plan-1"))
	})

	ginkgo.It("returns non-missing plan lookup errors unchanged", func() {
		svc := newImporterServiceWithStore(importerUnavailablePlanStore())
		uc := NewGetImportPlanUseCase(svc)

		out, err := uc.Execute(context.Background())

		Expect(out).To(BeNil())
		Expect(err).To(MatchError(ContainSubstring(coreimporter.ErrImportStateUnavailable.Error())))
	})

	ginkgo.It("starts planned imports and maps execution errors", func() {
		svc, store := newImporterServiceFixture()
		uc := NewExecuteImportUseCase(svc)

		out, err := uc.Execute(context.Background(), ExecuteImportInput{UserID: tree.UserIDFromString("system")})
		Expect(out).To(BeNil())
		expectLocalizedImporterError(err, ErrCodeImporterNoPlan)

		running := &ExecuteImportUseCase{svc: fakeImporterExecutor{err: coreimporter.ErrImportExecutionRunning}}
		out, err = running.Execute(context.Background(), ExecuteImportInput{UserID: tree.UserIDFromString("system")})
		Expect(out).To(BeNil())
		expectLocalizedImporterError(err, ErrCodeImporterExecutionRunning)

		seedImporterPlan(store, coreimporter.ExecutionStatusPlanned)
		out, err = uc.Execute(context.Background(), ExecuteImportInput{UserID: tree.UserIDFromString("system")})
		Expect(err).NotTo(HaveOccurred())
		Expect(out.Started).To(BeTrue())
		Expect(out.State.ExecutionStatus).To(Equal(coreimporter.ExecutionStatusRunning))

		unavailable := NewExecuteImportUseCase(newImporterServiceWithStore(importerUnavailablePlanStore()))
		out, err = unavailable.Execute(context.Background(), ExecuteImportInput{UserID: tree.UserIDFromString("system")})
		Expect(out).To(BeNil())
		expectLocalizedImporterError(err, ErrCodeImporterStateUnavailable)
	})

	ginkgo.It("returns raw execution errors unchanged", func() {
		executeErr := errors.New("execute failed")
		uc := &ExecuteImportUseCase{svc: fakeImporterExecutor{err: executeErr}}

		out, err := uc.Execute(context.Background(), ExecuteImportInput{UserID: tree.UserIDFromString("system")})

		Expect(out).To(BeNil())
		Expect(errors.Is(err, executeErr)).To(BeTrue())
	})

	ginkgo.It("clears planned imports, requests cancellation for running imports, and maps state errors", func() {
		svc, store := newImporterServiceFixture()
		uc := NewClearImportPlanUseCase(svc)

		state, err := uc.Execute(context.Background())
		Expect(err).NotTo(HaveOccurred())
		Expect(state).To(BeNil())

		seedImporterPlan(store, coreimporter.ExecutionStatusPlanned)
		state, err = uc.Execute(context.Background())
		Expect(err).NotTo(HaveOccurred())
		Expect(state).To(BeNil())
		_, err = svc.GetCurrentPlan()
		Expect(err).To(MatchError(coreimporter.ErrNoPlan))

		seedImporterPlan(store, coreimporter.ExecutionStatusRunning)
		state, err = uc.Execute(context.Background())
		Expect(err).NotTo(HaveOccurred())
		Expect(state).NotTo(BeNil())
		Expect(state.ExecutionStatus).To(Equal(coreimporter.ExecutionStatusRunning))
		Expect(state.CancelRequested).To(BeTrue())

		unavailable := NewClearImportPlanUseCase(newImporterServiceWithStore(importerUnavailablePlanStore()))
		state, err = unavailable.Execute(context.Background())
		Expect(state).To(BeNil())
		expectLocalizedImporterError(err, ErrCodeImporterStateUnavailable)
	})

	ginkgo.It("returns raw cancellation and clear errors unchanged", func() {
		cancelErr := errors.New("cancel failed")
		uc := &ClearImportPlanUseCase{svc: fakeImporterClearer{cancelErr: cancelErr}}

		state, err := uc.Execute(context.Background())
		Expect(state).To(BeNil())
		Expect(errors.Is(err, cancelErr)).To(BeTrue())

		clearErr := errors.New("clear failed")
		uc = &ClearImportPlanUseCase{svc: fakeImporterClearer{clearErr: clearErr}}

		state, err = uc.Execute(context.Background())
		Expect(state).To(BeNil())
		Expect(errors.Is(err, clearErr)).To(BeTrue())
	})

	ginkgo.It("returns final clear errors after a cancel check succeeds", func() {
		stateFile := filepath.Join(ginkgo.GinkgoT().TempDir(), "current-plan.json")
		store := coreimporter.NewPlanStore(stateFile)
		svc := newImporterServiceWithStore(store)
		seedImporterPlan(store, coreimporter.ExecutionStatusPlanned)
		Expect(os.Remove(stateFile)).To(Succeed())
		Expect(os.Mkdir(stateFile, 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(stateFile, "child"), []byte("occupied"), 0o644)).To(Succeed())

		state, err := NewClearImportPlanUseCase(svc).Execute(context.Background())

		Expect(state).To(BeNil())
		Expect(err).To(MatchError(ContainSubstring(coreimporter.ErrImportStateUnavailable.Error())))
	})
})

var _ = ginkgo.Describe("importer route handlers", func() {
	ginkgo.It("serves current plan state through the authenticated route", func() {
		svc, store := newImporterServiceFixture()
		seedImporterPlan(store, coreimporter.ExecutionStatusPlanned)
		router := newImporterTestRouter(RoutesConfig{
			GetPlan: NewGetImportPlanUseCase(svc),
		})

		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/import/plan", nil))

		Expect(rec.Code).To(Equal(http.StatusOK), rec.Body.String())
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
		Expect(getPlan.Code).To(Equal(http.StatusNotFound), getPlan.Body.String())
		assertImporterStructuredError(getPlan, "importer_no_plan", "errors.importer.no_plan")

		execute := performImporterCSRFRequest(router, http.MethodPost, "/api/import/execute", nil, "")
		Expect(execute.Code).To(Equal(http.StatusNotFound), execute.Body.String())
		assertImporterStructuredError(execute, "importer_no_plan", "errors.importer.no_plan")
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

		Expect(rec.Code).To(Equal(http.StatusOK), rec.Body.String())
		var plan coreimporter.CurrentPlanState
		Expect(jsonUnmarshalImporterResponse(rec, &plan)).To(Succeed())
		Expect(plan.ExecutionStatus).To(Equal(coreimporter.ExecutionStatusPlanned))
		Expect(plan.Items).To(HaveLen(1))
	})

	ginkgo.It("returns structured create-plan request errors", func() {
		svc, _ := newImporterServiceFixture()
		router := newImporterTestRouter(RoutesConfig{
			CreatePlan: NewCreateImportPlanUseCase(svc),
			Log:        slog.Default(),
		})

		malformed := performImporterCSRFRequest(router, http.MethodPost, "/api/import/plan", strings.NewReader("bad multipart"), "multipart/form-data; boundary=missing")
		Expect(malformed.Code).To(Equal(http.StatusRequestEntityTooLarge), malformed.Body.String())
		assertImporterStructuredError(malformed, "importer_upload_too_large", "errors.importer.upload_too_large")

		body, contentType := importerMultipartBody(nil, "")
		missingFile := performImporterCSRFRequest(router, http.MethodPost, "/api/import/plan", body, contentType)
		Expect(missingFile.Code).To(Equal(http.StatusBadRequest), missingFile.Body.String())
		assertImporterStructuredError(missingFile, "importer_missing_file", "errors.importer.missing_file")

		invalidZip, invalidZipContentType := importerMultipartBody([]byte("not a zip"), "")
		failedPlan := performImporterCSRFRequest(router, http.MethodPost, "/api/import/plan", invalidZip, invalidZipContentType)
		Expect(failedPlan.Code).To(Equal(http.StatusInternalServerError), failedPlan.Body.String())
		assertImporterStructuredError(failedPlan, "importer_internal_error", "errors.importer.internal_error")
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

		Expect(rec.Code).To(Equal(http.StatusBadRequest), rec.Body.String())
		assertImporterStructuredError(rec, "importer_file_open_failed", "errors.importer.file_open_failed")
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

		Expect(rec.Code).To(Equal(http.StatusOK), rec.Body.String())
	})

	ginkgo.It("executes planned imports with accepted status and completed imports with ok status", func() {
		svc, store := newImporterServiceFixture()
		router := newImporterTestRouter(RoutesConfig{
			Execute: NewExecuteImportUseCase(svc),
		})

		seedImporterPlan(store, coreimporter.ExecutionStatusPlanned)
		accepted := performImporterCSRFRequest(router, http.MethodPost, "/api/import/execute", nil, "")
		Expect(accepted.Code).To(Equal(http.StatusAccepted), accepted.Body.String())

		seedImporterPlan(store, coreimporter.ExecutionStatusCompleted)
		ok := performImporterCSRFRequest(router, http.MethodPost, "/api/import/execute", nil, "")
		Expect(ok.Code).To(Equal(http.StatusOK), ok.Body.String())
	})

	ginkgo.It("clears planned imports and accepts cancellation for running imports", func() {
		svc, store := newImporterServiceFixture()
		router := newImporterTestRouter(RoutesConfig{
			ClearPlan: NewClearImportPlanUseCase(svc),
		})

		seedImporterPlan(store, coreimporter.ExecutionStatusPlanned)
		cleared := performImporterCSRFRequest(router, http.MethodDelete, "/api/import/plan", nil, "")
		Expect(cleared.Code).To(Equal(http.StatusOK), cleared.Body.String())
		Expect(strings.TrimSpace(cleared.Body.String())).To(Equal("null"))

		seedImporterPlan(store, coreimporter.ExecutionStatusRunning)
		canceling := performImporterCSRFRequest(router, http.MethodDelete, "/api/import/plan", nil, "")
		Expect(canceling.Code).To(Equal(http.StatusAccepted), canceling.Body.String())
		var state coreimporter.CurrentPlanState
		Expect(jsonUnmarshalImporterResponse(canceling, &state)).To(Succeed())
		Expect(state.CancelRequested).To(BeTrue())
	})

	ginkgo.It("returns structured clear-plan errors", func() {
		svc := newImporterServiceWithStore(importerUnavailablePlanStore())
		router := newImporterTestRouter(RoutesConfig{
			ClearPlan: NewClearImportPlanUseCase(svc),
		})

		rec := performImporterCSRFRequest(router, http.MethodDelete, "/api/import/plan", nil, "")

		Expect(rec.Code).To(Equal(http.StatusInternalServerError), rec.Body.String())
		assertImporterStructuredError(rec, "importer_state_unavailable", "errors.importer.state_unavailable")
	})

	ginkgo.It("forbids direct handler calls when user context is missing", func() {
		routes := NewRoutes(RoutesConfig{})
		router := gin.New()
		router.POST("/import/plan", routes.handleCreatePlan)
		router.POST("/import/execute", routes.handleExecute)

		createPlan := httptest.NewRecorder()
		router.ServeHTTP(createPlan, httptest.NewRequest(http.MethodPost, "/import/plan", nil))
		Expect(createPlan.Code).To(Equal(http.StatusForbidden), createPlan.Body.String())

		execute := httptest.NewRecorder()
		router.ServeHTTP(execute, httptest.NewRequest(http.MethodPost, "/import/execute", nil))
		Expect(execute.Code).To(Equal(http.StatusForbidden), execute.Body.String())
	})
})

type fakeImporterPlanCreator struct {
	getErr error
}

func (f fakeImporterPlanCreator) CreateImportPlanFromZipUpload(io.Reader, string) (*coreimporter.PlanResult, error) {
	return &coreimporter.PlanResult{}, nil
}

func (f fakeImporterPlanCreator) GetCurrentPlan() (*coreimporter.CurrentPlanState, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	return &coreimporter.CurrentPlanState{}, nil
}

type fakeImporterExecutor struct {
	state   *coreimporter.CurrentPlanState
	started bool
	err     error
}

func (f fakeImporterExecutor) StartCurrentPlanExecution(tree.UserID) (*coreimporter.CurrentPlanState, bool, error) {
	return f.state, f.started, f.err
}

type fakeImporterClearer struct {
	state     *coreimporter.CurrentPlanState
	requested bool
	cancelErr error
	clearErr  error
}

func (f fakeImporterClearer) CancelCurrentPlan() (*coreimporter.CurrentPlanState, bool, error) {
	return f.state, f.requested, f.cancelErr
}

func (f fakeImporterClearer) ClearCurrentPlan() error {
	return f.clearErr
}

type importerTestWiki struct {
	treeHash string
}

func (w *importerTestWiki) TreeHash() string {
	if w.treeHash == "" {
		return "hash-1"
	}
	return w.treeHash
}

func (w *importerTestWiki) LookupPagePath(path tree.RoutePath) (*tree.PathLookup, error) {
	return &tree.PathLookup{Path: path, Exists: false, CanCreate: true}, nil
}

func (w *importerTestWiki) LookupPagePathForKind(path tree.RoutePath, _ tree.NodeKind) (*tree.PathLookup, error) {
	return &tree.PathLookup{Path: path, Exists: false, CanCreate: true}, nil
}

func (w *importerTestWiki) EnsurePath(_ tree.UserID, targetPath tree.RoutePath, title string, kind *tree.NodeKind) (*tree.Page, error) {
	slug := targetPath.LeafSlug()
	if slug == "" {
		slug = tree.SlugFromString("root")
	}
	nodeKind := tree.NodeKindPage
	if kind != nil {
		nodeKind = *kind
	}
	return &tree.Page{PageNode: &tree.PageNode{
		ID:    tree.PageIDFromString("page-" + slug.String()),
		Title: title,
		Slug:  slug,
		Kind:  nodeKind,
	}}, nil
}

func (w *importerTestWiki) UpdatePage(_ tree.UserID, id tree.PageID, title string, slug tree.Slug, content *string, kind *tree.NodeKind) (*tree.Page, error) {
	nodeKind := tree.NodeKindPage
	if kind != nil {
		nodeKind = *kind
	}
	return &tree.Page{PageNode: &tree.PageNode{ID: id, Title: title, Slug: slug, Kind: nodeKind}}, nil
}

func (w *importerTestWiki) UploadAsset(tree.UserID, tree.PageID, multipart.File, tree.AssetName, shared.MaxBytes) (string, error) {
	return "", nil
}

func newImporterServiceFixture() (*coreimporter.ImporterService, *coreimporter.PlanStore) {
	ginkgo.GinkgoHelper()
	store := coreimporter.NewPlanStore()
	return newImporterServiceWithStore(store), store
}

func newImporterServiceWithStore(store *coreimporter.PlanStore) *coreimporter.ImporterService {
	ginkgo.GinkgoHelper()
	planner := coreimporter.NewPlanner(&importerTestWiki{treeHash: "hash-1"}, tree.NewSlugService())
	return coreimporter.NewImporterService(planner, store, filepath.Join(ginkgo.GinkgoT().TempDir(), "workspaces"), 0)
}

func importerUnavailablePlanStore() *coreimporter.PlanStore {
	ginkgo.GinkgoHelper()
	path := filepath.Join(ginkgo.GinkgoT().TempDir(), "current-plan.json")
	Expect(os.WriteFile(path, []byte("{"), 0o644)).To(Succeed())
	return coreimporter.NewPlanStore(path)
}

func seedImporterPlan(store *coreimporter.PlanStore, status coreimporter.ExecutionStatus) {
	ginkgo.GinkgoHelper()
	Expect(store.Set(&coreimporter.StoredPlan{
		Plan: &coreimporter.PlanResult{
			ID:       "plan-1",
			TreeHash: "hash-1",
			Items:    []coreimporter.PlanItem{},
			Errors:   []string{},
		},
		PlanOptions:     coreimporter.PlanOptions{SourceBasePath: ginkgo.GinkgoT().TempDir()},
		WorkspaceRoot:   ginkgo.GinkgoT().TempDir(),
		CreatedAt:       time.Now(),
		ExecutionStatus: status,
	})).To(Succeed())
}

func importerZipBytes(name string, content string) []byte {
	ginkgo.GinkgoHelper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(name)
	Expect(err).NotTo(HaveOccurred())
	_, err = w.Write([]byte(content))
	Expect(err).NotTo(HaveOccurred())
	Expect(zw.Close()).To(Succeed())
	return buf.Bytes()
}

func importerMultipartBody(fileContent []byte, targetBasePath string) (*bytes.Buffer, string) {
	ginkgo.GinkgoHelper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if fileContent != nil {
		part, err := writer.CreateFormFile("file", "import.zip")
		Expect(err).NotTo(HaveOccurred())
		_, err = part.Write(fileContent)
		Expect(err).NotTo(HaveOccurred())
	}
	if targetBasePath != "" {
		Expect(writer.WriteField("targetBasePath", targetBasePath)).To(Succeed())
	}
	Expect(writer.Close()).To(Succeed())
	return &body, writer.FormDataContentType()
}

func newImporterTestRouter(cfg RoutesConfig) http.Handler {
	ginkgo.GinkgoHelper()
	opts := httpinternal.RouterOptions{
		AllowInsecure:         true,
		AuthDisabled:          true,
		DisableFrontendRoutes: true,
	}
	return httpinternal.NewRouter(
		[]httpinternal.RouteRegistrar{NewRoutes(cfg)},
		httpinternal.FrontendConfig{},
		opts,
	)
}

func performImporterCSRFRequest(router http.Handler, method string, path string, body io.Reader, contentType string) *httptest.ResponseRecorder {
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

func jsonUnmarshalImporterResponse(rec *httptest.ResponseRecorder, dst any) error {
	ginkgo.GinkgoHelper()
	return json.Unmarshal(rec.Body.Bytes(), dst)
}

func expectLocalizedImporterError(err error, code sharederrors.ErrorCode) {
	ginkgo.GinkgoHelper()
	loc, ok := sharederrors.AsLocalizedError(err)
	Expect(ok).To(BeTrue(), "error should be localized: %v", err)
	Expect(loc.Code).To(Equal(code))
}
