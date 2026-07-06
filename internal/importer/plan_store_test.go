package importer

import (
	"os"
	"path/filepath"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
)

var _ = ginkgo.Describe("plan store snapshots", ginkgo.Label("unit"), func() {
	ginkgo.It("returns a cloned plan with execution status", func() {
		s := NewPlanStore()
		plan := &StoredPlan{ExecutionStatus: ExecutionStatusPlanned}
		Expect(s.Set(plan)).To(Succeed())

		retrieved, err := s.Get()
		Expect(err).To(Succeed())
		Expect(retrieved).NotTo(BeIdenticalTo(plan))
		Expect(retrieved.ExecutionStatus).To(Equal(plan.ExecutionStatus))

	})
})

var _ = ginkgo.Describe("empty plan store reads", ginkgo.Label("unit"), func() {
	ginkgo.It("returns an error when no plan is stored", func() {
		s := NewPlanStore()

		_, err := s.Get()
		Expect(err).To(MatchError(ErrNoPlan))

	})
})

var _ = ginkgo.Describe("plan store cloned payload reads", ginkgo.Label("unit"), func() {
	ginkgo.It("returns a cloned stored plan payload", func() {
		s := NewPlanStore()
		plan := &StoredPlan{
			Plan:            &PlanResult{ID: "plan-1"},
			ExecutionStatus: ExecutionStatusPlanned,
		}
		Expect(s.Set(plan)).To(Succeed())

		retrieved, err := s.Get()
		Expect(err).To(Succeed())
		Expect(retrieved).NotTo(BeIdenticalTo(plan))
		Expect(retrieved.Plan).To(HavePlanResultIdentifier("plan-1"))

	})
})

var _ = ginkgo.Describe("plan store clearing", ginkgo.Label("unit"), func() {
	ginkgo.It("clears the current plan and makes later reads empty", func() {
		s := NewPlanStore()
		plan := &StoredPlan{}
		Expect(s.Set(plan)).To(Succeed())

		_, err := s.Clear()
		Expect(err).To(Succeed())

		_, err = s.Get()
		Expect(err).To(MatchError(ErrNoPlan))

	})
})

var _ = ginkgo.Describe("persistent plan store state", ginkgo.Label("unit"), func() {
	ginkgo.It("reloads persisted execution user state from disk", func() {
		stateFile := filepath.Join(importerTempDir(), "current-plan.json")
		store := NewPlanStore(stateFile)
		Expect(store.Set(&StoredPlan{
			Plan:            &PlanResult{ID: "plan-1"},
			ExecutionStatus: ExecutionStatusRunning,
			ExecutionUserID: newFixtureUserID("user-1").MetadataValue(),
		})).To(Succeed())

		loaded := NewPlanStore(stateFile)
		retrieved, err := loaded.Get()
		Expect(err).To(Succeed())
		Expect(retrieved).To(HaveStoredPlanState(gstruct.Fields{
			"Plan":            HavePlanResultIdentifier("plan-1"),
			"ExecutionUserID": Equal(newFixtureUserID("user-1").MetadataValue()),
		}))

	})
})

var _ = ginkgo.Describe("persistent plan store load failures", ginkgo.Label("unit"), func() {
	ginkgo.It("reports unavailable state for invalid persisted JSON", func() {
		stateFile := filepath.Join(importerTempDir(), "current-plan.json")
		Expect(os.WriteFile(stateFile, []byte("{invalid"), 0o644)).To(Succeed())

		store := NewPlanStore(stateFile)
		_, err := store.Get()
		Expect(err).To(MatchError(ErrImportStateUnavailable))

	})
})

var _ = ginkgo.Describe("execution start with unavailable plan payload", ginkgo.Label("unit"), func() {
	ginkgo.It("rejects execution start when the stored plan has no payload", func() {
		store := NewPlanStore()
		Expect(store.Set(&StoredPlan{
			Plan:            nil,
			ExecutionStatus: ExecutionStatusPlanned,
		})).To(Succeed())

		_, err := startStoredPlanExecutionResult(store, newFixtureUserID("user-1"))
		Expect(err).To(MatchError(ErrImportStateUnavailable))

	})
})

var _ = ginkgo.Describe("execution progress persistence", ginkgo.Label("unit"), func() {
	ginkgo.It("persists execution progress fields", func() {
		store := NewPlanStore()
		Expect(store.Set(&StoredPlan{
			Plan:            &PlanResult{ID: "plan-1"},
			ExecutionStatus: ExecutionStatusRunning,
		})).To(Succeed())

		now := time.Now()
		sourcePath := "docs/readme.md"
		err := store.UpdateExecutionProgress("plan-1", ExecutionProgress{
			ProcessedItems:        2,
			TotalItems:            5,
			CurrentItemSourcePath: &sourcePath,
			StartedAt:             &now,
		}, nil)
		Expect(err).To(Succeed())

		retrieved, err := store.Get()
		Expect(err).To(Succeed())
		Expect(retrieved).To(SatisfyAll(
			HaveField("ProcessedItems", Equal(2)),
			HaveField("TotalItems", Equal(5)),
			HaveField("CurrentItemSourcePath", gstruct.PointTo(Equal(sourcePath))),
			HaveField("StartedAt", gstruct.PointTo(Equal(now))),
		))

	})
})
