package importer_test

import (
	"archive/zip"
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/perber/wiki/internal/core/tree"
	"github.com/perber/wiki/internal/importer"
	"github.com/perber/wiki/internal/wiki"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"
)

var errIntegrationExecutionNotStarted = errors.New("importer integration execution not started")
var errIntegrationCancellationNotRequested = errors.New("importer integration cancellation not requested")

type integZipEntry struct {
	name    string
	content string
}

type integImportCancellationState string

const (
	integImportCancellationAbsent    integImportCancellationState = "absent"
	integImportCancellationRequested integImportCancellationState = "requested"
)

type integImportResultState string

const (
	integImportResultAbsent  integImportResultState = "absent"
	integImportResultPresent integImportResultState = "present"
)

type integImportFailureObservation struct {
	Status importer.ExecutionStatus
	Result integImportResultState
	Error  integExecutionErrorState
	Code   importer.ExecutionErrorCode
}

func observeImportCancellation(state *importer.CurrentPlanState) integImportCancellationState {
	if state != nil && state.CancelRequested {
		return integImportCancellationRequested
	}
	return integImportCancellationAbsent
}

func observeImportFailure(state *importer.CurrentPlanState) integImportFailureObservation {
	observation := integImportFailureObservation{}
	if state == nil {
		return observation
	}
	observation.Status = state.ExecutionStatus
	if state.ExecutionResult != nil {
		observation.Result = integImportResultPresent
	} else {
		observation.Result = integImportResultAbsent
	}
	if state.ExecutionError != nil && *state.ExecutionError != "" {
		observation.Error = integExecutionErrorPresent
	} else {
		observation.Error = integExecutionErrorAbsent
	}
	if state.ExecutionErrorCode != nil {
		observation.Code = *state.ExecutionErrorCode
	}
	return observation
}

func matchWrappedIntegrationError(want error) types.GomegaMatcher {
	return WithTransform(func(err error) error {
		if errors.Is(err, want) {
			return want
		}
		return err
	}, Equal(want))
}

func newTestImporterServiceWithState(w *wiki.Wiki, stateFile string, workspaceBaseDir string) *importer.ImporterService {
	ginkgo.GinkgoHelper()

	planner := importer.NewPlanner(wiki.NewWikiImportAdapter(w), tree.NewSlugService())
	return importer.NewImporterService(
		planner,
		importer.NewPlanStore(stateFile),
		workspaceBaseDir,
		0,
	)
}

func integZipArchive(entries ...integZipEntry) *bytes.Reader {
	ginkgo.GinkgoHelper()

	var zipBytes bytes.Buffer
	zipWriter := zip.NewWriter(&zipBytes)
	for _, entry := range entries {
		file, err := zipWriter.Create(entry.name)
		Expect(err).To(Succeed())
		_, err = file.Write([]byte(entry.content))
		Expect(err).To(Succeed())
	}
	Expect(zipWriter.Close()).To(Succeed())
	return bytes.NewReader(zipBytes.Bytes())
}

func integStartCurrentPlanExecutionResult(
	service *importer.ImporterService,
	userID tree.UserID,
) (*importer.CurrentPlanState, error) {
	ginkgo.GinkgoHelper()

	state, started, err := service.StartCurrentPlanExecution(userID)
	if err != nil {
		return state, err
	}
	if !started {
		return state, errIntegrationExecutionNotStarted
	}
	return state, nil
}

func integStartStoredPlanExecutionResult(store *importer.PlanStore, userID tree.UserID) (*importer.StoredPlan, error) {
	ginkgo.GinkgoHelper()

	plan, started, err := store.TryStartExecution(userID.MetadataValue())
	if err != nil {
		return plan, err
	}
	if !started {
		return plan, errIntegrationExecutionNotStarted
	}
	return plan, nil
}

func integCancelCurrentPlanResult(service *importer.ImporterService) (*importer.CurrentPlanState, error) {
	ginkgo.GinkgoHelper()

	state, requested, err := service.CancelCurrentPlan()
	if err != nil {
		return state, err
	}
	if !requested {
		return state, errIntegrationCancellationNotRequested
	}
	return state, nil
}

func integWaitForImportExecutionStatus(
	service *importer.ImporterService,
	want importer.ExecutionStatus,
) *importer.CurrentPlanState {
	ginkgo.GinkgoHelper()

	var state *importer.CurrentPlanState
	Eventually(func(g Gomega) {
		var err error
		state, err = service.GetCurrentPlan()
		g.Expect(err).To(Succeed())
		g.Expect(state).NotTo(BeNil())
		g.Expect(state.ExecutionStatus).To(Equal(want))
	}).
		WithTimeout(5 * time.Second).
		WithPolling(10 * time.Millisecond).
		Should(Succeed())
	return state
}

var _ = ginkgo.Describe("import plan lifecycle through real wiki storage", ginkgo.Label("integration"), func() {
	ginkgo.It("stores, exposes, and clears a folder import plan", func() {
		ws := integTempDir()
		integMustWrite(ws, "Draft.md", "# Draft\nBody")

		w := newTestWiki()
		integWrapCloseWithErrorCheck(w.Close)
		is := newTestImporterService(w)

		plan, err := is.CreateImportPlanFromFolder(ws, "")
		Expect(err).To(Succeed())
		Expect(plan).To(SatisfyAll(
			HaveField("Items", HaveLen(1)),
			HaveField("Errors", BeEmpty()),
		))

		state, err := is.GetCurrentPlan()
		Expect(err).To(Succeed())
		Expect(state).To(SatisfyAll(
			HaveField("ID", Equal(plan.ID)),
			HaveField("ExecutionStatus", Equal(importer.ExecutionStatusPlanned)),
			HaveField("Items", HaveLen(1)),
			HaveField("Errors", BeEmpty()),
		))

		Expect(is.ClearCurrentPlan()).To(Succeed())
		_, err = is.GetCurrentPlan()
		Expect(err).To(MatchError(importer.ErrNoPlan))
	})

	ginkgo.It("replaces an earlier planned workspace before storing the new plan", func() {
		previousWorkspace := integTempDir()
		previousMarker := integMustWrite(previousWorkspace, "Previous.md", "# Previous")
		nextWorkspace := integTempDir()
		integMustWrite(nextWorkspace, "Next.md", "# Next")

		w := newTestWiki()
		integWrapCloseWithErrorCheck(w.Close)
		is := newTestImporterService(w)

		previousPlan, err := is.CreateImportPlanFromFolder(previousWorkspace, "")
		Expect(err).To(Succeed())
		Expect(previousPlan.Items).To(HaveLen(1))

		nextPlan, err := is.CreateImportPlanFromFolder(nextWorkspace, "")
		Expect(err).To(Succeed())
		Expect(nextPlan).To(SatisfyAll(
			HaveField("ID", Not(Equal(previousPlan.ID))),
			HaveField("Items", HaveLen(1)),
		))

		_, err = os.Stat(previousMarker)
		Expect(err).To(MatchError(os.ErrNotExist))

		state, err := is.GetCurrentPlan()
		Expect(err).To(Succeed())
		Expect(state).To(SatisfyAll(
			HaveField("ID", Equal(nextPlan.ID)),
			HaveField("ExecutionStatus", Equal(importer.ExecutionStatusPlanned)),
		))
	})

	ginkgo.It("marks a running plan for cancellation without starting another executor", func() {
		ws := integTempDir()
		integMustWrite(ws, "Cancelable.md", "# Cancelable")

		w := newTestWiki()
		integWrapCloseWithErrorCheck(w.Close)
		stateFile := filepath.Join(w.GetStorageDir(), ".importer", "current-plan.json")
		workspaceBaseDir := filepath.Join(w.GetStorageDir(), ".importer", "workspaces")
		planner := importer.NewPlanner(wiki.NewWikiImportAdapter(w), tree.NewSlugService())
		store := importer.NewPlanStore(stateFile)
		is := importer.NewImporterService(planner, store, workspaceBaseDir, 0)

		plan, err := is.CreateImportPlanFromFolder(ws, "")
		Expect(err).To(Succeed())
		_, err = integStartStoredPlanExecutionResult(store, integFixtureUserID("system"))
		Expect(err).To(Succeed())
		Expect(is.ClearCurrentPlan()).To(MatchError(importer.ErrImportExecutionRunning))

		state, err := integCancelCurrentPlanResult(is)
		Expect(err).To(Succeed())
		Expect(state).To(SatisfyAll(
			HaveField("ID", Equal(plan.ID)),
			HaveField("ExecutionStatus", Equal(importer.ExecutionStatusRunning)),
			WithTransform(observeImportCancellation, Equal(integImportCancellationRequested)),
		))
	})
})

var _ = ginkgo.Describe("zip upload import planning through real wiki storage", ginkgo.Label("integration"), func() {
	ginkgo.It("extracts an uploaded archive, stores the plan, and imports the extracted pages", func() {
		w := newTestWiki()
		integWrapCloseWithErrorCheck(w.Close)
		is := newTestImporterService(w)
		probe := newImporterProbe(w)

		plan, err := is.CreateImportPlanFromZipUpload(integZipArchive(
			integZipEntry{name: "docs/Uploaded.md", content: "# Uploaded\nBody"},
			integZipEntry{name: "notes/Another.md", content: "# Another\nBody"},
		), "")
		Expect(err).To(Succeed())
		Expect(plan).To(SatisfyAll(
			HaveField("Items", HaveLen(2)),
			HaveField("Errors", BeEmpty()),
		))

		state, err := is.GetCurrentPlan()
		Expect(err).To(Succeed())
		Expect(state).To(SatisfyAll(
			HaveField("ID", Equal(plan.ID)),
			HaveField("ExecutionStatus", Equal(importer.ExecutionStatusPlanned)),
			HaveField("TotalItems", BeZero()),
		))

		result, err := is.ExecuteCurrentPlan(integFixtureUserID("system"))
		Expect(err).To(Succeed())
		Expect(result).To(SatisfyAll(
			HaveField("ImportedCount", Equal(2)),
			HaveField("SkippedCount", BeZero()),
			HaveField("Items", HaveLen(2)),
		))

		uploadedPage, err := probe.FindByPath("docs/uploaded")
		Expect(err).To(Succeed())
		Expect(uploadedPage.Content).To(Equal("# Uploaded\nBody"))

		anotherPage, err := probe.FindByPath("notes/another")
		Expect(err).To(Succeed())
		Expect(anotherPage.Content).To(Equal("# Another\nBody"))
	})

	ginkgo.It("rejects invalid and unsafe archives without storing a plan", func() {
		w := newTestWiki()
		integWrapCloseWithErrorCheck(w.Close)
		is := newTestImporterService(w)

		_, err := is.CreateImportPlanFromZipUpload(bytes.NewReader([]byte("not a zip")), "")
		Expect(err).To(matchWrappedIntegrationError(zip.ErrFormat))

		_, err = is.GetCurrentPlan()
		Expect(err).To(MatchError(importer.ErrNoPlan))

		_, err = is.CreateImportPlanFromZipUpload(integZipArchive(
			integZipEntry{name: "../outside.md", content: "# Outside"},
		), "")
		Expect(err).To(matchWrappedIntegrationError(importer.ErrImportZipInvalidEntry))

		_, err = is.GetCurrentPlan()
		Expect(err).To(MatchError(importer.ErrNoPlan))
	})
})

var _ = ginkgo.Describe("background import execution through real wiki storage", ginkgo.Label("integration"), func() {
	ginkgo.It("persists asynchronous completion state for the current plan", func() {
		ws := integTempDir()
		integMustWrite(ws, "Async.md", "# Async\nBody")

		w := newTestWiki()
		integWrapCloseWithErrorCheck(w.Close)
		is := newTestImporterService(w)

		plan, err := is.CreateImportPlanFromFolder(ws, "")
		Expect(err).To(Succeed())

		startedState, err := integStartCurrentPlanExecutionResult(is, integFixtureUserID("system"))
		Expect(err).To(Succeed())
		Expect(startedState).To(SatisfyAll(
			HaveField("ID", Equal(plan.ID)),
			HaveField("ExecutionStatus", Equal(importer.ExecutionStatusRunning)),
			HaveField("TotalItems", Equal(1)),
			HaveField("StartedAt", Not(BeNil())),
		))

		completedState := integWaitForImportExecutionStatus(is, importer.ExecutionStatusCompleted)
		Expect(completedState).To(SatisfyAll(
			HaveField("ExecutionResult", Not(BeNil())),
			HaveField("ExecutionResult.ImportedCount", Equal(1)),
			HaveField("ProcessedItems", Equal(1)),
			HaveField("TotalItems", Equal(1)),
			HaveField("CurrentItemSourcePath", BeNil()),
			HaveField("FinishedAt", Not(BeNil())),
		))
	})

	ginkgo.It("returns the completed result when execution is requested again", func() {
		ws := integTempDir()
		integMustWrite(ws, "Completed.md", "# Completed\nBody")

		w := newTestWiki()
		integWrapCloseWithErrorCheck(w.Close)
		is := newTestImporterService(w)

		_, err := is.CreateImportPlanFromFolder(ws, "")
		Expect(err).To(Succeed())

		firstResult, err := is.ExecuteCurrentPlan(integFixtureUserID("system"))
		Expect(err).To(Succeed())
		Expect(firstResult.ImportedCount).To(Equal(1))

		secondResult, err := is.ExecuteCurrentPlan(integFixtureUserID("system"))
		Expect(err).To(Succeed())
		Expect(secondResult).To(Equal(firstResult))
	})

	ginkgo.It("records stale-plan failures during synchronous execution", func() {
		ws := integTempDir()
		integMustWrite(ws, "Stale.md", "# Stale\nBody")

		w := newTestWiki()
		integWrapCloseWithErrorCheck(w.Close)
		is := newTestImporterService(w)

		plan, err := is.CreateImportPlanFromFolder(ws, "")
		Expect(err).To(Succeed())
		plan.TreeHash = "stale-tree"

		_, err = is.ExecuteCurrentPlan(integFixtureUserID("system"))
		Expect(err).To(matchWrappedIntegrationError(importer.ErrImportPlanStale))

		state, err := is.GetCurrentPlan()
		Expect(err).To(Succeed())
		Expect(state).To(SatisfyAll(
			WithTransform(observeImportFailure, Equal(integImportFailureObservation{
				Status: importer.ExecutionStatusFailed,
				Result: integImportResultAbsent,
				Error:  integExecutionErrorPresent,
				Code:   importer.ExecutionErrorCodeImportPlanStale,
			})),
		))
	})

	ginkgo.It("reports completed import state that is missing its persisted result", func() {
		w := newTestWiki()
		integWrapCloseWithErrorCheck(w.Close)
		stateFile := filepath.Join(w.GetStorageDir(), ".importer", "current-plan.json")
		workspaceBaseDir := filepath.Join(w.GetStorageDir(), ".importer", "workspaces")
		planner := importer.NewPlanner(wiki.NewWikiImportAdapter(w), tree.NewSlugService())
		store := importer.NewPlanStore(stateFile)
		is := importer.NewImporterService(planner, store, workspaceBaseDir, 0)

		Expect(store.Set(&importer.StoredPlan{
			Plan: &importer.PlanResult{
				ID:       "completed-plan",
				TreeHash: wiki.NewWikiImportAdapter(w).TreeHash(),
			},
			ExecutionStatus: importer.ExecutionStatusCompleted,
		})).To(Succeed())

		_, err := is.ExecuteCurrentPlan(integFixtureUserID("system"))
		Expect(err).To(MatchError(importer.ErrImportCompletedResultMissing))
	})
})

var _ = ginkgo.Describe("persisted import execution through real wiki storage", ginkgo.Label("integration"), func() {
	ginkgo.It("resumes a running plan from the persisted state file", func() {
		ws := integTempDir()
		integMustWrite(ws, "Resumed.md", "# Resumed\nBody")

		w := newTestWiki()
		integWrapCloseWithErrorCheck(w.Close)
		stateFile := filepath.Join(w.GetStorageDir(), ".importer", "current-plan.json")
		workspaceBaseDir := filepath.Join(w.GetStorageDir(), ".importer", "workspaces")
		planner := importer.NewPlanner(wiki.NewWikiImportAdapter(w), tree.NewSlugService())
		store := importer.NewPlanStore(stateFile)
		is := importer.NewImporterService(planner, store, workspaceBaseDir, 0)

		plan, err := is.CreateImportPlanFromFolder(ws, "")
		Expect(err).To(Succeed())
		_, err = integStartStoredPlanExecutionResult(store, integFixtureUserID("system"))
		Expect(err).To(Succeed())

		resumed := newTestImporterServiceWithState(w, stateFile, workspaceBaseDir)

		completedState := integWaitForImportExecutionStatus(resumed, importer.ExecutionStatusCompleted)
		Expect(completedState).To(SatisfyAll(
			HaveField("ID", Equal(plan.ID)),
			HaveField("ExecutionResult", Not(BeNil())),
			HaveField("ExecutionResult.ImportedCount", Equal(1)),
			HaveField("ProcessedItems", Equal(1)),
			HaveField("TotalItems", Equal(1)),
		))
	})

	ginkgo.It("records a stale-plan error when persisted execution resumes after tree changes", func() {
		ws := integTempDir()
		integMustWrite(ws, "Stale.md", "# Stale\nBody")

		w := newTestWiki()
		integWrapCloseWithErrorCheck(w.Close)
		stateFile := filepath.Join(w.GetStorageDir(), ".importer", "current-plan.json")
		workspaceBaseDir := filepath.Join(w.GetStorageDir(), ".importer", "workspaces")
		planner := importer.NewPlanner(wiki.NewWikiImportAdapter(w), tree.NewSlugService())
		store := importer.NewPlanStore(stateFile)
		is := importer.NewImporterService(planner, store, workspaceBaseDir, 0)

		plan, err := is.CreateImportPlanFromFolder(ws, "")
		Expect(err).To(Succeed())
		plan.TreeHash = "stale-tree"
		_, err = integStartStoredPlanExecutionResult(store, integFixtureUserID("system"))
		Expect(err).To(Succeed())

		resumed := newTestImporterServiceWithState(w, stateFile, workspaceBaseDir)

		failedState := integWaitForImportExecutionStatus(resumed, importer.ExecutionStatusFailed)
		Expect(failedState).To(SatisfyAll(
			HaveField("ID", Equal(plan.ID)),
			WithTransform(observeImportFailure, Equal(integImportFailureObservation{
				Status: importer.ExecutionStatusFailed,
				Result: integImportResultAbsent,
				Error:  integExecutionErrorPresent,
				Code:   importer.ExecutionErrorCodeImportPlanStale,
			})),
			HaveField("ProcessedItems", BeZero()),
			HaveField("TotalItems", Equal(1)),
		))
	})
})
