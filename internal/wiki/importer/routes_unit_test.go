package importer

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

	"github.com/gin-gonic/gin"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"

	coreauth "github.com/perber/wiki/internal/core/auth"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/core/tree"
	httpinternal "github.com/perber/wiki/internal/http"
	coreimporter "github.com/perber/wiki/internal/importer"
)

var _ = ginkgo.Describe("importer route handlers", ginkgo.Label("unit"), func() {
	ginkgo.It("constructs routes with configured importer use cases", func() {
		createPlan := &CreateImportPlanUseCase{}
		getPlan := &GetImportPlanUseCase{}
		execute := &ExecuteImportUseCase{}
		clearPlan := &ClearImportPlanUseCase{}

		routes := NewRoutes(RoutesConfig{
			CreatePlan: createPlan,
			GetPlan:    getPlan,
			Execute:    execute,
			ClearPlan:  clearPlan,
		})

		Expect(routes).To(matchImporterRouteUseCases(importerRouteUseCases{
			CreatePlan: createPlan,
			GetPlan:    getPlan,
			Execute:    execute,
			ClearPlan:  clearPlan,
		}))
	})

	ginkgo.It("registers authenticated importer endpoints", func() {
		gin.SetMode(gin.TestMode)
		engine := gin.New()

		NewRoutes(RoutesConfig{}).RegisterRoutes(httpinternal.RouterContext{
			Base: engine.Group(""),
			Opts: httpinternal.RouterOptions{AuthDisabled: true},
		})

		Expect(importerRegisteredRoutes(engine)).To(exposeImporterRouteContract())
	})

	ginkgo.It("applies router upload limits to the importer service during registration", func() {
		gin.SetMode(gin.TestMode)
		engine := gin.New()
		svc := coreimporter.NewImporterService(nil, coreimporter.NewPlanStore(tempImporterDir()+"/current-plan.json"), tempImporterDir(), 1)

		NewRoutes(RoutesConfig{Svc: svc}).RegisterRoutes(httpinternal.RouterContext{
			Base: engine.Group(""),
			Opts: httpinternal.RouterOptions{AuthDisabled: true, MaxAssetUploadSizeBytes: 42},
		})

		Expect(importerRegisteredRoutes(engine)).To(exposeImporterRouteContract())
	})

	ginkgo.It("creates an import plan from an uploaded file", func() {
		ctx, rec := newImporterMultipartContext("/api/import/plan", map[string]string{"targetBasePath": "docs"}, "file", "import.zip", []byte("zip"))
		ctx.Set("user", newImporterRouteUser())
		creator := &recordingImportPlanCreator{plan: fixtureImporterPlanState(coreimporter.ExecutionStatusPlanned)}
		routes := &Routes{createPlan: &CreateImportPlanUseCase{svc: creator}, log: slog.New(slog.NewTextHandler(io.Discard, nil))}

		routes.handleCreatePlan(ctx)

		Expect(rec).To(HaveHTTPStatus(http.StatusOK), rec.Body.String())
		Expect(decodeImporterPlanState(rec)).To(matchImporterPlanState("plan-1", coreimporter.ExecutionStatusPlanned))
		Expect(creator).To(recordImporterPlanTarget("docs"))
	})

	ginkgo.It("returns structured create-plan upload errors", func() {
		noUserCtx, noUserRec := newImporterMultipartContext("/api/import/plan", nil, "file", "import.zip", []byte("zip"))
		(&Routes{createPlan: &CreateImportPlanUseCase{svc: &recordingImportPlanCreator{}}}).handleCreatePlan(noUserCtx)
		Expect(noUserRec).To(HaveHTTPStatus(http.StatusForbidden), noUserRec.Body.String())

		malformedCtx, malformedRec := newImporterRawContext(http.MethodPost, "/api/import/plan", "not multipart")
		malformedCtx.Set("user", newImporterRouteUser())
		malformedCtx.Request.Header.Set("Content-Type", "multipart/form-data; boundary=missing")
		(&Routes{createPlan: &CreateImportPlanUseCase{svc: &recordingImportPlanCreator{}}}).handleCreatePlan(malformedCtx)
		Expect(malformedRec).To(haveImporterStructuredError(http.StatusRequestEntityTooLarge, ErrCodeImporterUploadTooLarge), malformedRec.Body.String())

		missingFileCtx, missingFileRec := newImporterMultipartContext("/api/import/plan", nil, "", "", nil)
		missingFileCtx.Set("user", newImporterRouteUser())
		(&Routes{createPlan: &CreateImportPlanUseCase{svc: &recordingImportPlanCreator{}}}).handleCreatePlan(missingFileCtx)
		Expect(missingFileRec).To(haveImporterStructuredError(http.StatusBadRequest, ErrCodeImporterMissingFile), missingFileRec.Body.String())
	})

	ginkgo.It("returns structured create-plan file and use-case errors", func() {
		restoreOpen := openImporterUploadFile
		restoreClose := closeImporterUploadFile
		ginkgo.DeferCleanup(func() {
			openImporterUploadFile = restoreOpen
			closeImporterUploadFile = restoreClose
		})

		openErrCtx, openErrRec := newImporterMultipartContext("/api/import/plan", nil, "file", "import.zip", []byte("zip"))
		openErrCtx.Set("user", newImporterRouteUser())
		openImporterUploadFile = func(*multipart.FileHeader) (multipart.File, error) {
			return nil, errors.New("open failed")
		}
		(&Routes{createPlan: &CreateImportPlanUseCase{svc: &recordingImportPlanCreator{}}}).handleCreatePlan(openErrCtx)
		Expect(openErrRec).To(haveImporterStructuredError(http.StatusBadRequest, ErrCodeImporterFileOpenFailed), openErrRec.Body.String())

		createErrCtx, createErrRec := newImporterMultipartContext("/api/import/plan", nil, "file", "import.zip", []byte("zip"))
		createErrCtx.Set("user", newImporterRouteUser())
		openImporterUploadFile = restoreOpen
		closeImporterUploadFile = func(multipart.File) error { return errors.New("close failed") }
		(&Routes{
			createPlan: &CreateImportPlanUseCase{svc: &recordingImportPlanCreator{createErr: sharederrors.NewLocalizedErrorFromCode(ErrCodeImporterNoPlan, nil)}},
			log:        slog.New(slog.NewTextHandler(io.Discard, nil)),
		}).handleCreatePlan(createErrCtx)
		Expect(createErrRec).To(haveImporterStructuredError(http.StatusNotFound, ErrCodeImporterNoPlan), createErrRec.Body.String())
	})

	ginkgo.It("writes current import plans from the direct handler", func() {
		ctx, rec := newImporterRawContext(http.MethodGet, "/api/import/plan", "")
		routes := &Routes{getPlan: &GetImportPlanUseCase{svc: fakeImporterPlanGetter{plan: fixtureImporterPlanState(coreimporter.ExecutionStatusPlanned)}}}

		routes.handleGetPlan(ctx)

		Expect(rec).To(HaveHTTPStatus(http.StatusOK), rec.Body.String())
		Expect(decodeImporterPlanState(rec)).To(matchImporterPlanState("plan-1", coreimporter.ExecutionStatusPlanned))
	})

	ginkgo.It("writes structured current-plan errors from the direct handler", func() {
		ctx, rec := newImporterRawContext(http.MethodGet, "/api/import/plan", "")
		routes := &Routes{getPlan: &GetImportPlanUseCase{svc: fakeImporterPlanGetter{err: coreimporter.ErrNoPlan}}}

		routes.handleGetPlan(ctx)

		Expect(rec).To(haveImporterStructuredError(http.StatusNotFound, ErrCodeImporterNoPlan), rec.Body.String())
	})

	ginkgo.It("starts imports for authenticated users and reports accepted running states", func() {
		ctx, rec := newImporterRawContext(http.MethodPost, "/api/import/execute", "")
		ctx.Set("user", newImporterRouteUser())
		executor := &recordingImporterExecutor{state: fixtureImporterPlanState(coreimporter.ExecutionStatusRunning), started: true}
		routes := &Routes{execute: &ExecuteImportUseCase{svc: executor}}

		routes.handleExecute(ctx)

		Expect(rec).To(HaveHTTPStatus(http.StatusAccepted), rec.Body.String())
		Expect(executor).To(recordImporterExecuteUser(tree.UserIDFromString("editor-1")))
	})

	ginkgo.It("writes execute errors and completed states from the direct handler", func() {
		noUserCtx, noUserRec := newImporterRawContext(http.MethodPost, "/api/import/execute", "")
		(&Routes{execute: &ExecuteImportUseCase{svc: &recordingImporterExecutor{}}}).handleExecute(noUserCtx)
		Expect(noUserRec).To(HaveHTTPStatus(http.StatusForbidden), noUserRec.Body.String())

		errCtx, errRec := newImporterRawContext(http.MethodPost, "/api/import/execute", "")
		errCtx.Set("user", newImporterRouteUser())
		(&Routes{execute: &ExecuteImportUseCase{svc: &recordingImporterExecutor{err: coreimporter.ErrImportExecutionRunning}}}).handleExecute(errCtx)
		Expect(errRec).To(haveImporterStructuredError(http.StatusConflict, ErrCodeImporterExecutionRunning), errRec.Body.String())

		okCtx, okRec := newImporterRawContext(http.MethodPost, "/api/import/execute", "")
		okCtx.Set("user", newImporterRouteUser())
		(&Routes{execute: &ExecuteImportUseCase{svc: &recordingImporterExecutor{state: fixtureImporterPlanState(coreimporter.ExecutionStatusCompleted)}}}).handleExecute(okCtx)
		Expect(okRec).To(HaveHTTPStatus(http.StatusOK), okRec.Body.String())
	})

	ginkgo.It("clears plans or acknowledges running cancellation", func() {
		runningCtx, runningRec := newImporterRawContext(http.MethodDelete, "/api/import/plan", "")
		running := fixtureImporterPlanState(coreimporter.ExecutionStatusRunning)
		running.CancelRequested = true
		(&Routes{clearPlan: &ClearImportPlanUseCase{svc: fakeImporterClearer{state: running}}}).handleClearPlan(runningCtx)
		Expect(runningRec).To(HaveHTTPStatus(http.StatusAccepted), runningRec.Body.String())

		clearedCtx, clearedRec := newImporterRawContext(http.MethodDelete, "/api/import/plan", "")
		(&Routes{clearPlan: &ClearImportPlanUseCase{svc: fakeImporterClearer{cancelErr: coreimporter.ErrNoPlan}}}).handleClearPlan(clearedCtx)
		Expect(clearedRec).To(HaveHTTPStatus(http.StatusOK), clearedRec.Body.String())
	})

	ginkgo.It("writes clear-plan errors from the direct handler", func() {
		ctx, rec := newImporterRawContext(http.MethodDelete, "/api/import/plan", "")

		(&Routes{clearPlan: &ClearImportPlanUseCase{svc: fakeImporterClearer{cancelErr: coreimporter.ErrImportStateUnavailable}}}).handleClearPlan(ctx)

		Expect(rec).To(haveImporterStructuredError(http.StatusInternalServerError, ErrCodeImporterStateUnavailable), rec.Body.String())
	})
})

var _ = ginkgo.Describe("importer use-case adapters", ginkgo.Label("unit"), func() {
	ginkgo.It("constructs use cases with configured importer services", func() {
		svc := &coreimporter.ImporterService{}

		Expect(NewCreateImportPlanUseCase(svc).svc).NotTo(BeNil())
		Expect(NewGetImportPlanUseCase(svc).svc).NotTo(BeNil())
		Expect(NewExecuteImportUseCase(svc).svc).NotTo(BeNil())
		Expect(NewClearImportPlanUseCase(svc).svc).NotTo(BeNil())
	})

	ginkgo.It("creates a plan and returns the current plan state", func() {
		creator := &recordingImportPlanCreator{plan: fixtureImporterPlanState(coreimporter.ExecutionStatusPlanned)}

		out, err := (&CreateImportPlanUseCase{svc: creator}).Execute(context.Background(), CreateImportPlanInput{
			File:           bytes.NewReader([]byte("zip")),
			TargetBasePath: "docs",
		})

		Expect(err).To(Succeed())
		Expect(out.Plan).To(Equal(creator.plan))
		Expect(creator).To(recordImporterPlanTarget("docs"))
	})

	ginkgo.It("returns plan creation failures unchanged", func() {
		expectedErr := errors.New("create failed")

		out, err := (&CreateImportPlanUseCase{svc: &recordingImportPlanCreator{createErr: expectedErr}}).Execute(context.Background(), CreateImportPlanInput{})

		Expect(out).To(BeNil())
		Expect(err).To(MatchError(expectedErr))
	})

	ginkgo.It("gets current plans and maps missing plans", func() {
		plan := fixtureImporterPlanState(coreimporter.ExecutionStatusPlanned)
		out, err := (&GetImportPlanUseCase{svc: fakeImporterPlanGetter{plan: plan}}).Execute(context.Background())
		Expect(err).To(Succeed())
		Expect(out.Plan).To(Equal(plan))

		missing, err := (&GetImportPlanUseCase{svc: fakeImporterPlanGetter{err: coreimporter.ErrNoPlan}}).Execute(context.Background())
		Expect(missing).To(BeNil())
		Expect(err).To(matchLocalizedImporterError(ErrCodeImporterNoPlan))
	})

	ginkgo.It("returns non-missing current-plan lookup failures unchanged", func() {
		expectedErr := errors.New("current plan failed")

		out, err := (&GetImportPlanUseCase{svc: fakeImporterPlanGetter{err: expectedErr}}).Execute(context.Background())

		Expect(out).To(BeNil())
		Expect(err).To(MatchError(expectedErr))
	})

	ginkgo.It("maps execution state errors and returns running state", func() {
		running := fixtureImporterPlanState(coreimporter.ExecutionStatusRunning)
		out, err := (&ExecuteImportUseCase{svc: &recordingImporterExecutor{state: running, started: true}}).Execute(context.Background(), ExecuteImportInput{UserID: tree.UserIDFromString("editor-1")})
		Expect(err).To(Succeed())
		Expect(out).To(Equal(&ExecuteImportOutput{State: running, Started: true}))

		for _, row := range []struct {
			err  error
			code sharederrors.ErrorCode
		}{
			{coreimporter.ErrImportExecutionRunning, ErrCodeImporterExecutionRunning},
			{coreimporter.ErrNoPlan, ErrCodeImporterNoPlan},
			{coreimporter.ErrImportStateUnavailable, ErrCodeImporterStateUnavailable},
		} {
			failed, execErr := (&ExecuteImportUseCase{svc: &recordingImporterExecutor{err: row.err}}).Execute(context.Background(), ExecuteImportInput{UserID: tree.UserIDFromString("editor-1")})
			Expect(failed).To(BeNil())
			Expect(execErr).To(matchLocalizedImporterError(row.code))
		}
	})

	ginkgo.It("clears plans after ignored missing-plan cancellation", func() {
		clearer := &recordingImporterClearer{cancelErr: coreimporter.ErrNoPlan}

		state, err := (&ClearImportPlanUseCase{svc: clearer}).Execute(context.Background())

		Expect(err).To(Succeed())
		Expect(state).To(BeNil())
		Expect(clearer).To(recordImporterPlanCleared())
	})
})

var _ = ginkgo.Describe("importer error responses", ginkgo.Label("unit"), func() {
	ginkgo.It("writes localized and internal structured importer errors", func() {
		localized := newImporterErrorResponseRecorder(func(ctx *gin.Context) {
			respondWithImporterError(ctx, sharederrors.NewLocalizedErrorFromCode(ErrCodeImporterNoPlan, nil))
		})
		Expect(localized).To(haveImporterStructuredError(http.StatusNotFound, ErrCodeImporterNoPlan), localized.Body.String())

		internal := newImporterErrorResponseRecorder(func(ctx *gin.Context) {
			respondWithImporterError(ctx, errors.New("importer failed"))
		})
		Expect(internal).To(haveImporterStructuredError(http.StatusInternalServerError, ErrCodeImporterInternalError), internal.Body.String())
	})

	ginkgo.It("writes explicit structured importer status errors", func() {
		rec := newImporterErrorResponseRecorder(func(ctx *gin.Context) {
			respondWithImporterStatusError(ctx, http.StatusBadRequest, ErrCodeImporterMissingFile, "ignored", "ignored")
		})

		Expect(rec).To(haveImporterStructuredError(http.StatusBadRequest, ErrCodeImporterMissingFile), rec.Body.String())
	})
})

type recordingImportPlanCreator struct {
	plan      *coreimporter.CurrentPlanState
	createErr error
	getErr    error

	targetBasePath string
}

func (creator *recordingImportPlanCreator) CreateImportPlanFromZipUpload(_ io.Reader, targetBasePath string) (*coreimporter.PlanResult, error) {
	creator.targetBasePath = targetBasePath
	if creator.createErr != nil {
		return nil, creator.createErr
	}
	return &coreimporter.PlanResult{ID: "plan-1"}, nil
}

func (creator *recordingImportPlanCreator) GetCurrentPlan() (*coreimporter.CurrentPlanState, error) {
	if creator.getErr != nil {
		return nil, creator.getErr
	}
	return creator.plan, nil
}

type fakeImporterPlanGetter struct {
	plan *coreimporter.CurrentPlanState
	err  error
}

func (getter fakeImporterPlanGetter) GetCurrentPlan() (*coreimporter.CurrentPlanState, error) {
	if getter.err != nil {
		return nil, getter.err
	}
	return getter.plan, nil
}

type recordingImporterExecutor struct {
	state   *coreimporter.CurrentPlanState
	started bool
	err     error
	userID  tree.UserID
}

func (executor *recordingImporterExecutor) StartCurrentPlanExecution(userID tree.UserID) (*coreimporter.CurrentPlanState, bool, error) {
	executor.userID = userID
	if executor.err != nil {
		return nil, false, executor.err
	}
	return executor.state, executor.started, nil
}

type recordingImporterClearer struct {
	state     *coreimporter.CurrentPlanState
	requested bool
	cancelErr error
	clearErr  error
	cleared   bool
}

func (clearer *recordingImporterClearer) CancelCurrentPlan() (*coreimporter.CurrentPlanState, bool, error) {
	return clearer.state, clearer.requested, clearer.cancelErr
}

func (clearer *recordingImporterClearer) ClearCurrentPlan() error {
	clearer.cleared = true
	return clearer.clearErr
}

func fixtureImporterPlanState(status coreimporter.ExecutionStatus) *coreimporter.CurrentPlanState {
	return &coreimporter.CurrentPlanState{ID: "plan-1", ExecutionStatus: status}
}

func newImporterRouteUser() *coreauth.User {
	return &coreauth.User{ID: coreauth.UserIDFromString("editor-1"), Username: "Editor One", Role: coreauth.RoleEditor}
}

func newImporterRawContext(method string, target string, body string) (*gin.Context, *httptest.ResponseRecorder) {
	ginkgo.GinkgoHelper()

	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(method, target, bytes.NewBufferString(body))
	return ctx, rec
}

func newImporterMultipartContext(target string, fields map[string]string, fileField string, filename string, content []byte) (*gin.Context, *httptest.ResponseRecorder) {
	ginkgo.GinkgoHelper()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	for key, value := range fields {
		Expect(writer.WriteField(key, value)).To(Succeed())
	}
	if fileField != "" {
		part, err := writer.CreateFormFile(fileField, filename)
		Expect(err).To(Succeed())
		_, err = part.Write(content)
		Expect(err).To(Succeed())
	}
	Expect(writer.Close()).To(Succeed())

	ctx, rec := newImporterRawContext(http.MethodPost, target, "")
	ctx.Request = httptest.NewRequest(http.MethodPost, target, body)
	ctx.Request.Header.Set("Content-Type", writer.FormDataContentType())
	return ctx, rec
}

func newImporterErrorResponseRecorder(write func(*gin.Context)) *httptest.ResponseRecorder {
	ginkgo.GinkgoHelper()

	ctx, rec := newImporterRawContext(http.MethodGet, "/api/import/plan", "")
	write(ctx)
	return rec
}

func decodeImporterPlanState(rec *httptest.ResponseRecorder) *coreimporter.CurrentPlanState {
	ginkgo.GinkgoHelper()

	var state coreimporter.CurrentPlanState
	Expect(json.Unmarshal(rec.Body.Bytes(), &state)).To(Succeed(), rec.Body.String())
	return &state
}

func matchImporterPlanState(id string, status coreimporter.ExecutionStatus) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return SatisfyAll(
		HaveField("ID", Equal(id)),
		HaveField("ExecutionStatus", Equal(status)),
	)
}

func importerRegisteredRoutes(engine *gin.Engine) []string {
	routes := engine.Routes()
	out := make([]string, 0, len(routes))
	for _, route := range routes {
		out = append(out, route.Method+" "+route.Path)
	}
	return out
}

func exposeImporterRouteContract() types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return ConsistOf(
		"POST /api/import/plan",
		"GET /api/import/plan",
		"POST /api/import/execute",
		"DELETE /api/import/plan",
	)
}

type importerRouteUseCases struct {
	CreatePlan *CreateImportPlanUseCase
	GetPlan    *GetImportPlanUseCase
	Execute    *ExecuteImportUseCase
	ClearPlan  *ClearImportPlanUseCase
}

func matchImporterRouteUseCases(want importerRouteUseCases) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(importerRouteUseCasesFor, Equal(want))
}

func importerRouteUseCasesFor(routes *Routes) importerRouteUseCases {
	if routes == nil {
		return importerRouteUseCases{}
	}
	return importerRouteUseCases{
		CreatePlan: routes.createPlan,
		GetPlan:    routes.getPlan,
		Execute:    routes.execute,
		ClearPlan:  routes.clearPlan,
	}
}

type importerPlanTargetObservation struct {
	TargetBasePath string
}

func recordImporterPlanTarget(targetBasePath string) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(observedImporterPlanTarget, Equal(importerPlanTargetObservation{TargetBasePath: targetBasePath}))
}

func observedImporterPlanTarget(creator *recordingImportPlanCreator) importerPlanTargetObservation {
	return importerPlanTargetObservation{TargetBasePath: creator.targetBasePath}
}

type importerExecuteUserObservation struct {
	UserID tree.UserID
}

func recordImporterExecuteUser(userID tree.UserID) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(observedImporterExecuteUser, Equal(importerExecuteUserObservation{UserID: userID}))
}

func observedImporterExecuteUser(executor *recordingImporterExecutor) importerExecuteUserObservation {
	return importerExecuteUserObservation{UserID: executor.userID}
}

var _ importPlanClearer = (*recordingImporterClearer)(nil)

type importerClearObservation struct {
	Cleared bool
}

func recordImporterPlanCleared() types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(observedImporterClear, Equal(importerClearObservation{Cleared: true}))
}

func observedImporterClear(clearer *recordingImporterClearer) importerClearObservation {
	return importerClearObservation{Cleared: clearer.cleared}
}
