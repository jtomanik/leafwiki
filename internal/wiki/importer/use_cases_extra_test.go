package importer

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"
	"github.com/perber/wiki/internal/core/shared"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/core/tree"
	httpinternal "github.com/perber/wiki/internal/http"
	coreimporter "github.com/perber/wiki/internal/importer"
	testmatchers "github.com/perber/wiki/internal/test_utils/matchers"
)

var _ = ginkgo.Describe("importer use cases", func() {
	ginkgo.It("creates an import plan from an uploaded zip", ginkgo.Label("integration"), func() {
		svc, _ := newImporterServiceFixture()
		uc := NewCreateImportPlanUseCase(svc)

		out, err := uc.Execute(context.Background(), CreateImportPlanInput{
			File:           bytes.NewReader(importerZipBytes("Imported.md", "# Imported\nbody")),
			TargetBasePath: "docs",
		})

		Expect(err).NotTo(HaveOccurred())
		Expect(out.Plan).NotTo(BeNil())
		Expect(out.Plan).To(haveImporterPlanWithTargetItem(coreimporter.ExecutionStatusPlanned, tree.RoutePathFromString("docs/imported")))
	})

	ginkgo.It("returns plan creation errors from invalid uploads", ginkgo.Label("integration"), func() {
		svc, _ := newImporterServiceFixture()
		uc := NewCreateImportPlanUseCase(svc)

		out, err := uc.Execute(context.Background(), CreateImportPlanInput{File: strings.NewReader("not a zip")})

		Expect(out).To(BeNil())
		Expect(err).To(MatchError(zip.ErrFormat))
	})

	ginkgo.It("returns current-plan lookup errors after successful plan creation", ginkgo.Label("unit"), func() {
		getErr := errors.New("get current plan failed")
		uc := &CreateImportPlanUseCase{svc: fakeImporterPlanCreator{getErr: getErr}}

		out, err := uc.Execute(context.Background(), CreateImportPlanInput{File: strings.NewReader("ignored")})

		Expect(out).To(BeNil())
		Expect(err).To(MatchError(getErr))
	})

	ginkgo.It("gets the current plan or maps a missing plan to a localized error", ginkgo.Label("integration"), func() {
		svc, store := newImporterServiceFixture()
		uc := NewGetImportPlanUseCase(svc)

		out, err := uc.Execute(context.Background())
		Expect(out).To(BeNil())
		Expect(err).To(matchLocalizedImporterError(ErrCodeImporterNoPlan))

		seedImporterPlan(store, coreimporter.ExecutionStatusPlanned)
		out, err = uc.Execute(context.Background())
		Expect(err).NotTo(HaveOccurred())
		Expect(out.Plan.ID).To(Equal("plan-1"))
	})

	ginkgo.It("returns non-missing plan lookup errors unchanged", ginkgo.Label("integration"), func() {
		svc := newImporterServiceWithStore(importerUnavailablePlanStore())
		uc := NewGetImportPlanUseCase(svc)

		out, err := uc.Execute(context.Background())

		Expect(out).To(BeNil())
		Expect(err).To(MatchError(coreimporter.ErrImportStateUnavailable))
	})

	ginkgo.It("starts planned imports and maps execution errors", ginkgo.Label("integration"), func() {
		svc, store := newImporterServiceFixture()
		uc := NewExecuteImportUseCase(svc)

		out, err := uc.Execute(context.Background(), ExecuteImportInput{UserID: tree.UserIDFromString("system")})
		Expect(out).To(BeNil())
		Expect(err).To(matchLocalizedImporterError(ErrCodeImporterNoPlan))

		running := &ExecuteImportUseCase{svc: fakeImporterExecutor{err: coreimporter.ErrImportExecutionRunning}}
		out, err = running.Execute(context.Background(), ExecuteImportInput{UserID: tree.UserIDFromString("system")})
		Expect(out).To(BeNil())
		Expect(err).To(matchLocalizedImporterError(ErrCodeImporterExecutionRunning))

		seedImporterPlan(store, coreimporter.ExecutionStatusPlanned)
		out, err = uc.Execute(context.Background(), ExecuteImportInput{UserID: tree.UserIDFromString("system")})
		Expect(err).NotTo(HaveOccurred())
		Expect(out).To(haveStartedImporterExecution(coreimporter.ExecutionStatusRunning))

		unavailable := NewExecuteImportUseCase(newImporterServiceWithStore(importerUnavailablePlanStore()))
		out, err = unavailable.Execute(context.Background(), ExecuteImportInput{UserID: tree.UserIDFromString("system")})
		Expect(out).To(BeNil())
		Expect(err).To(matchLocalizedImporterError(ErrCodeImporterStateUnavailable))
	})

	ginkgo.It("returns raw execution errors unchanged", ginkgo.Label("unit"), func() {
		executeErr := errors.New("execute failed")
		uc := &ExecuteImportUseCase{svc: fakeImporterExecutor{err: executeErr}}

		out, err := uc.Execute(context.Background(), ExecuteImportInput{UserID: tree.UserIDFromString("system")})

		Expect(out).To(BeNil())
		Expect(err).To(MatchError(executeErr))
	})

	ginkgo.It("clears planned imports, requests cancellation for running imports, and maps state errors", ginkgo.Label("integration"), func() {
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
		Expect(state).To(haveImporterPlanWithCancellation(coreimporter.ExecutionStatusRunning))

		unavailable := NewClearImportPlanUseCase(newImporterServiceWithStore(importerUnavailablePlanStore()))
		state, err = unavailable.Execute(context.Background())
		Expect(state).To(BeNil())
		Expect(err).To(matchLocalizedImporterError(ErrCodeImporterStateUnavailable))
	})

	ginkgo.It("returns raw cancellation and clear errors unchanged", ginkgo.Label("unit"), func() {
		cancelErr := errors.New("cancel failed")
		uc := &ClearImportPlanUseCase{svc: fakeImporterClearer{cancelErr: cancelErr}}

		state, err := uc.Execute(context.Background())
		Expect(state).To(BeNil())
		Expect(err).To(MatchError(cancelErr))

		clearErr := errors.New("clear failed")
		uc = &ClearImportPlanUseCase{svc: fakeImporterClearer{clearErr: clearErr}}

		state, err = uc.Execute(context.Background())
		Expect(state).To(BeNil())
		Expect(err).To(MatchError(clearErr))
	})

	ginkgo.It("returns final clear errors after a cancel check succeeds", ginkgo.Label("integration"), func() {
		stateFile := filepath.Join(tempImporterDir(), "current-plan.json")
		store := coreimporter.NewPlanStore(stateFile)
		svc := newImporterServiceWithStore(store)
		seedImporterPlan(store, coreimporter.ExecutionStatusPlanned)
		Expect(os.Remove(stateFile)).To(Succeed())
		Expect(os.Mkdir(stateFile, 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(stateFile, "child"), []byte("occupied"), 0o644)).To(Succeed())

		state, err := NewClearImportPlanUseCase(svc).Execute(context.Background())

		Expect(state).To(BeNil())
		Expect(err).To(MatchError(coreimporter.ErrImportStateUnavailable))
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
		ID:    importerFixturePageID(slug),
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
	return coreimporter.NewImporterService(planner, store, filepath.Join(tempImporterDir(), "workspaces"), 0)
}

func importerUnavailablePlanStore() *coreimporter.PlanStore {
	ginkgo.GinkgoHelper()
	path := filepath.Join(tempImporterDir(), "current-plan.json")
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
		PlanOptions:     coreimporter.PlanOptions{SourceBasePath: tempImporterDir()},
		WorkspaceRoot:   tempImporterDir(),
		CreatedAt:       time.Now(),
		ExecutionStatus: status,
	})).To(Succeed())
}

func tempImporterDir() string {
	ginkgo.GinkgoHelper()

	dir, err := os.MkdirTemp("", "leafwiki-importer-*")
	Expect(err).NotTo(HaveOccurred())
	ginkgo.DeferCleanup(os.RemoveAll, dir)
	return dir
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

func matchLocalizedImporterError(code sharederrors.ErrorCode) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return testmatchers.MatchLocalizedError(code, sharederrors.MessageIDForCode(code))
}

func haveImporterPlanWithTargetItem(status coreimporter.ExecutionStatus, targetPath tree.RoutePath) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return haveImporterPlan(status, HaveExactElements(haveImporterPlanItemWithTargetPath(targetPath)))
}

func haveImporterPlanWithItemCount(status coreimporter.ExecutionStatus, count int) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return haveImporterPlan(status, HaveLen(count))
}

func haveImporterPlanWithCancellation(status coreimporter.ExecutionStatus) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(importerPlanCancellationStateFromCurrentPlan, Equal(importerPlanCancellationState{
		status:       status,
		cancellation: importerPlanCancellationRequested,
	}))
}

func haveStartedImporterExecution(status coreimporter.ExecutionStatus) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(importerExecutionStartStateFromOutput, Equal(importerExecutionStartState{
		status:  status,
		outcome: importerExecutionStarted,
	}))
}

type importerExecutionStartOutcome string

const (
	importerExecutionMissing    importerExecutionStartOutcome = "missing"
	importerExecutionStarted    importerExecutionStartOutcome = "started"
	importerExecutionNotStarted importerExecutionStartOutcome = "not-started"
)

type importerExecutionStartState struct {
	status  coreimporter.ExecutionStatus
	outcome importerExecutionStartOutcome
}

func importerExecutionStartStateFromOutput(out *ExecuteImportOutput) importerExecutionStartState {
	if out == nil || out.State == nil {
		return importerExecutionStartState{outcome: importerExecutionMissing}
	}
	outcome := importerExecutionNotStarted
	if out.Started {
		outcome = importerExecutionStarted
	}
	return importerExecutionStartState{
		status:  out.State.ExecutionStatus,
		outcome: outcome,
	}
}

type importerPlanCancellationOutcome string

const (
	importerPlanCancellationMissing   importerPlanCancellationOutcome = "missing"
	importerPlanCancellationRequested importerPlanCancellationOutcome = "requested"
	importerPlanCancellationIdle      importerPlanCancellationOutcome = "idle"
)

type importerPlanCancellationState struct {
	status       coreimporter.ExecutionStatus
	cancellation importerPlanCancellationOutcome
}

func importerPlanCancellationStateFromCurrentPlan(plan *coreimporter.CurrentPlanState) importerPlanCancellationState {
	if plan == nil {
		return importerPlanCancellationState{cancellation: importerPlanCancellationMissing}
	}
	cancellation := importerPlanCancellationIdle
	if plan.CancelRequested {
		cancellation = importerPlanCancellationRequested
	}
	return importerPlanCancellationState{
		status:       plan.ExecutionStatus,
		cancellation: cancellation,
	}
}

func haveImporterPlan(status coreimporter.ExecutionStatus, items types.GomegaMatcher) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return HaveValue(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"ExecutionStatus": Equal(status),
		"Items":           items,
	}))
}

func haveImporterPlanItemWithTargetPath(targetPath tree.RoutePath) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"TargetPath": Equal(targetPath),
	})
}

func importerFixturePageID(slug tree.Slug) tree.PageID {
	return tree.PageIDFromString("page-" + slug.HashPayload())
}
