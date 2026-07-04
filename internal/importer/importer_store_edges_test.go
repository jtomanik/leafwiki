package importer

import (
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/perber/wiki/internal/core/tree"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("stored import plan persistence failures", ginkgo.Label("unit"), func() {
	ginkgo.It("turns set persistence failures into sticky state errors", func() {
		store := NewPlanStore()
		store.stateFile = importerBadStateFile()

		err := store.Set(&StoredPlan{Plan: &PlanResult{ID: "plan-1"}})
		Expect(err).To(MatchError(ErrImportStateUnavailable))

		_, err = store.Get()
		Expect(err).To(MatchError(ErrImportStateUnavailable))
	})

	ginkgo.It("reports persisted load errors and accepts disabled persistence", func() {
		store := NewPlanStore(importerBadStateFile())
		_, err := store.Get()
		Expect(err).To(MatchError(ErrImportStateUnavailable))

		noPersistence := NewPlanStore("")
		Expect(noPersistence.Set(&StoredPlan{Plan: &PlanResult{ID: "plan-1"}})).To(Succeed())
	})

	ginkgo.It("surfaces persistence failures from mutating execution operations", func() {
		type mutationCase struct {
			name   string
			setup  func(*StoredPlan)
			mutate func(*PlanStore) error
		}

		cases := []mutationCase{
			{
				name: "clear",
				setup: func(plan *StoredPlan) {
					plan.ExecutionStatus = ExecutionStatusPlanned
				},
				mutate: func(store *PlanStore) error {
					_, err := store.Clear()
					return err
				},
			},
			{
				name: "try start",
				setup: func(plan *StoredPlan) {
					plan.ExecutionStatus = ExecutionStatusPlanned
				},
				mutate: func(store *PlanStore) error {
					_, err := startStoredPlanExecutionResult(store, "user-1")
					return err
				},
			},
			{
				name: "finish failed",
				setup: func(plan *StoredPlan) {
					plan.ExecutionStatus = ExecutionStatusRunning
				},
				mutate: func(store *PlanStore) error {
					return store.FinishExecution("plan-1", nil, errors.New("boom"))
				},
			},
			{
				name: "update progress",
				setup: func(plan *StoredPlan) {
					plan.ExecutionStatus = ExecutionStatusRunning
				},
				mutate: func(store *PlanStore) error {
					return store.UpdateExecutionProgress("plan-1", ExecutionProgress{ProcessedItems: 1}, nil)
				},
			},
			{
				name: "request cancel",
				setup: func(plan *StoredPlan) {
					plan.ExecutionStatus = ExecutionStatusRunning
				},
				mutate: func(store *PlanStore) error {
					_, err := requestCancelResult(store)
					return err
				},
			},
		}

		for _, tt := range cases {
			tt := tt
			store := NewPlanStore()
			store.stateFile = importerBadStateFile()
			store.plan = &StoredPlan{Plan: &PlanResult{ID: "plan-1"}}
			tt.setup(store.plan)

			err := tt.mutate(store)
			Expect(err).To(MatchError(ErrImportStateUnavailable), tt.name)
		}
	})

	ginkgo.It("returns sticky state errors from every public mutation", func() {
		store := NewPlanStore()
		store.stateErr = ErrImportStateUnavailable

		Expect(store.Set(&StoredPlan{})).To(MatchError(ErrImportStateUnavailable))
		_, err := store.Clear()
		Expect(err).To(MatchError(ErrImportStateUnavailable))
		_, err = startStoredPlanExecutionResult(store, "user-1")
		Expect(err).To(MatchError(ErrImportStateUnavailable))
		Expect(store.FinishExecution("plan-1", nil, nil)).To(MatchError(ErrImportStateUnavailable))
		Expect(store.UpdateExecutionProgress("plan-1", ExecutionProgress{}, nil)).To(MatchError(ErrImportStateUnavailable))
		_, err = requestCancelResult(store)
		Expect(err).To(MatchError(ErrImportStateUnavailable))
	})

	ginkgo.It("surfaces terminal-state persistence failures", func() {
		for _, tt := range []struct {
			name string
			err  error
		}{
			{name: "canceled", err: ErrImportCanceled},
			{name: "completed", err: nil},
		} {
			store := NewPlanStore()
			store.stateFile = importerBadStateFile()
			store.plan = &StoredPlan{
				Plan:            &PlanResult{ID: "plan-1"},
				ExecutionStatus: ExecutionStatusRunning,
			}

			err := store.FinishExecution("plan-1", &ExecutionResult{}, tt.err)
			Expect(err).To(MatchError(ErrImportStateUnavailable), tt.name)
		}
	})

	ginkgo.It("clears missing state files and clones nil state safely", func() {
		stateFile := filepath.Join(importerTempDir(), "missing", "current-plan.json")
		store := NewPlanStore(stateFile)

		old, err := store.Clear()
		Expect(err).NotTo(HaveOccurred())
		Expect(old).To(BeNil())
		Expect(cloneStoredPlan(nil)).To(BeNil())
		Expect(cloneExecutionResult(nil)).To(BeNil())
	})

	ginkgo.It("surfaces state-file removal failures when clearing an empty plan", func() {
		stateFile := filepath.Join(importerTempDir(), "current-plan.json")
		Expect(os.MkdirAll(stateFile, 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(stateFile, "child"), []byte("x"), 0o644)).To(Succeed())

		store := NewPlanStore()
		store.stateFile = stateFile

		old, err := store.Clear()
		Expect(old).To(BeNil())
		Expect(err).To(MatchError(ErrImportStateUnavailable))
	})

	ginkgo.It("surfaces state-file write failures", func() {
		stateDir := filepath.Join(importerTempDir(), "readonly")
		Expect(os.MkdirAll(stateDir, 0o755)).To(Succeed())
		Expect(os.Chmod(stateDir, 0o555)).To(Succeed())
		ginkgo.DeferCleanup(func() {
			_ = os.Chmod(stateDir, 0o755)
		})

		store := NewPlanStore()
		store.stateFile = filepath.Join(stateDir, "current-plan.json")

		err := store.Set(&StoredPlan{Plan: &PlanResult{ID: "plan-1"}})
		Expect(err).To(MatchError(ErrImportStateUnavailable))
	})

	ginkgo.It("surfaces JSON marshal failures from invalid persisted times", func() {
		store := NewPlanStore()
		store.stateFile = filepath.Join(importerTempDir(), "current-plan.json")

		err := store.Set(&StoredPlan{
			Plan:      &PlanResult{ID: "plan-1"},
			CreatedAt: time.Date(10000, time.January, 1, 0, 0, 0, 0, time.UTC),
		})
		Expect(err).To(MatchError(ErrImportStateUnavailable))
	})
})

var _ = ginkgo.Describe("import plan execution service state handling", ginkgo.Label("unit"), func() {
	ginkgo.It("returns stored terminal states that do not need a new executor", func() {
		service := newServiceWithFakeWiki(&fakeWiki{treeHash: "h1", lookups: map[string]*tree.PathLookup{}})
		result := &ExecutionResult{ImportedCount: 2}
		Expect(service.planStore.Set(&StoredPlan{
			Plan:            &PlanResult{ID: "plan-1"},
			ExecutionStatus: ExecutionStatusCompleted,
			ExecutionResult: result,
		})).To(Succeed())

		got, err := service.ExecuteCurrentPlan("user-1")
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(Equal(result))

		Expect(service.planStore.Set(&StoredPlan{
			Plan:            &PlanResult{ID: "plan-2"},
			ExecutionStatus: ExecutionStatusCompleted,
		})).To(Succeed())
		_, err = service.ExecuteCurrentPlan("user-1")
		Expect(err).To(MatchError(ErrImportCompletedResultMissing))

		Expect(service.planStore.Set(&StoredPlan{
			Plan:            &PlanResult{ID: "plan-3"},
			ExecutionStatus: ExecutionStatusRunning,
		})).To(Succeed())
		_, err = service.ExecuteCurrentPlan("user-1")
		Expect(err).To(MatchError(ErrImportExecutionRunning))
	})

	ginkgo.It("surfaces cancellation and current-plan state without a stored plan", func() {
		service := newServiceWithFakeWiki(&fakeWiki{treeHash: "h1", lookups: map[string]*tree.PathLookup{}})

		state, err := cancelCurrentPlanResult(service)
		Expect(state).To(BeNil())
		Expect(err).To(MatchError(ErrNoPlan))

		Expect(currentPlanStateFromStored(nil)).To(BeNil())
		Expect(currentPlanStateFromStored(&StoredPlan{})).To(BeNil())
	})

	ginkgo.It("refuses to replace a running folder import plan", func() {
		newWorkspace := importerTempDir()
		importerWriteFile(newWorkspace, "next.md", "# Next")
		service := newServiceWithFakeWiki(&fakeWiki{treeHash: "h1", lookups: map[string]*tree.PathLookup{}})

		Expect(service.planStore.Set(&StoredPlan{
			Plan:            &PlanResult{ID: "running"},
			WorkspaceRoot:   importerTempDir(),
			ExecutionStatus: ExecutionStatusRunning,
		})).To(Succeed())

		_, err := service.CreateImportPlanFromFolder(newWorkspace, "")
		Expect(err).To(MatchError(ErrImportExecutionRunning))
	})
})
