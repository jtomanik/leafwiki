package importer_test

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"

	"github.com/perber/wiki/internal/core/tree"
	"github.com/perber/wiki/internal/importer"
	"github.com/perber/wiki/internal/wiki"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
)

type integImportStartOutcome string

const (
	integImportStartAccepted       integImportStartOutcome = "accepted"
	integImportStartAlreadyRunning integImportStartOutcome = "already_running"
)

type integImportCancellationOutcome string

const (
	integImportCancellationAccepted         integImportCancellationOutcome = "accepted"
	integImportCancellationAlreadyRequested integImportCancellationOutcome = "already_requested"
)

type integImportStartObservation struct {
	PlanID string
	Status importer.ExecutionStatus
	Result integImportStartOutcome
}

type integImportCancellationObservation struct {
	PlanID       string
	Cancellation integImportCancellationState
	Result       integImportCancellationOutcome
}

func observeImportStart(state *importer.CurrentPlanState, started bool) integImportStartObservation {
	observation := integImportStartObservation{}
	if state != nil {
		observation.PlanID = state.ID
		observation.Status = state.ExecutionStatus
	}
	if started {
		observation.Result = integImportStartAccepted
		return observation
	}
	observation.Result = integImportStartAlreadyRunning
	return observation
}

func observeImportCancellationRequest(
	state *importer.CurrentPlanState,
	requested bool,
) integImportCancellationObservation {
	observation := integImportCancellationObservation{}
	if state != nil {
		observation.PlanID = state.ID
		observation.Cancellation = observeImportCancellation(state)
	}
	if requested {
		observation.Result = integImportCancellationAccepted
		return observation
	}
	observation.Result = integImportCancellationAlreadyRequested
	return observation
}

var _ = ginkgo.Describe("import plan execution state through real wiki storage", ginkgo.Label("integration"), func() {
	ginkgo.It("reports running plans without launching a second executor", func() {
		ws := integTempDir()
		integMustWrite(ws, "Running.md", "# Running")

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

		state, started, err := is.StartCurrentPlanExecution(integFixtureUserID("system"))
		Expect(err).To(Succeed())
		Expect(observeImportStart(state, started)).To(Equal(integImportStartObservation{
			PlanID: plan.ID,
			Status: importer.ExecutionStatusRunning,
			Result: integImportStartAlreadyRunning,
		}))

		_, err = is.ExecuteCurrentPlan(integFixtureUserID("system"))
		Expect(err).To(MatchError(importer.ErrImportExecutionRunning))
	})

	ginkgo.It("reports repeated cancellation requests without changing the running plan", func() {
		ws := integTempDir()
		integMustWrite(ws, "Cancel.md", "# Cancel")

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

		state, requested, err := is.CancelCurrentPlan()
		Expect(err).To(Succeed())
		Expect(observeImportCancellationRequest(state, requested)).To(Equal(integImportCancellationObservation{
			PlanID:       plan.ID,
			Cancellation: integImportCancellationRequested,
			Result:       integImportCancellationAccepted,
		}))

		state, requested, err = is.CancelCurrentPlan()
		Expect(err).To(Succeed())
		Expect(observeImportCancellationRequest(state, requested)).To(Equal(integImportCancellationObservation{
			PlanID:       plan.ID,
			Cancellation: integImportCancellationRequested,
			Result:       integImportCancellationAlreadyRequested,
		}))
	})

	ginkgo.It("surfaces unavailable persisted state before planning or execution begins", func() {
		w := newTestWiki()
		integWrapCloseWithErrorCheck(w.Close)
		stateFile := filepath.Join(w.GetStorageDir(), ".importer", "current-plan.json")
		Expect(os.MkdirAll(stateFile, 0o755)).To(Succeed())
		planner := importer.NewPlanner(wiki.NewWikiImportAdapter(w), tree.NewSlugService())
		is := importer.NewImporterService(
			planner,
			importer.NewPlanStore(stateFile),
			filepath.Join(w.GetStorageDir(), ".importer", "workspaces"),
			0,
		)

		_, err := is.GetCurrentPlan()
		Expect(err).To(matchWrappedIntegrationError(importer.ErrImportStateUnavailable))

		ws := integTempDir()
		integMustWrite(ws, "Blocked.md", "# Blocked")
		_, err = is.CreateImportPlanFromFolder(ws, "")
		Expect(err).To(matchWrappedIntegrationError(importer.ErrImportStateUnavailable))
	})
})

var _ = ginkgo.Describe("zip upload import state through real wiki storage", ginkgo.Label("integration"), func() {
	ginkgo.It("rejects absolute archive entries without storing a plan", func() {
		w := newTestWiki()
		integWrapCloseWithErrorCheck(w.Close)
		is := newTestImporterService(w)

		_, err := is.CreateImportPlanFromZipUpload(integZipArchive(
			integZipEntry{name: "/absolute.md", content: "# Absolute"},
		), "")
		Expect(err).To(matchWrappedIntegrationError(importer.ErrImportZipInvalidEntry))

		_, err = is.GetCurrentPlan()
		Expect(err).To(MatchError(importer.ErrNoPlan))
	})

	ginkgo.It("ignores archive directory entries while planning extracted Markdown files", func() {
		w := newTestWiki()
		integWrapCloseWithErrorCheck(w.Close)
		is := newTestImporterService(w)
		var zipBytes bytes.Buffer
		zipWriter := zip.NewWriter(&zipBytes)
		_, err := zipWriter.Create("docs/")
		Expect(err).To(Succeed())
		file, err := zipWriter.Create("docs/Page.md")
		Expect(err).To(Succeed())
		_, err = file.Write([]byte("# Page\nBody"))
		Expect(err).To(Succeed())
		Expect(zipWriter.Close()).To(Succeed())

		plan, err := is.CreateImportPlanFromZipUpload(bytes.NewReader(zipBytes.Bytes()), "")
		Expect(err).To(Succeed())
		Expect(plan.Items).To(ConsistOf(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"SourcePath": Equal(integWorkspaceSourcePath("docs/Page.md")),
			"TargetPath": Equal(integRoutePath("docs/page")),
			"Action":     Equal(importer.PlanActionCreate),
		})))
	})
})
