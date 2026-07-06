package importer

import (
	"errors"
	"log/slog"
	"mime/multipart"
	"time"

	"github.com/perber/wiki/internal/core/shared"
	"github.com/perber/wiki/internal/core/tree"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
)

var _ = ginkgo.Describe("Executor execution edges", ginkgo.Label("unit"), func() {
	ginkgo.It("rejects resume state without a tree hash before processing items", func() {
		executor := NewExecutor(
			&PlanResult{TreeHash: "h1", Items: []PlanItem{{SourcePath: newFixtureWorkspaceSourcePath("a.md"), Action: PlanActionCreate}}},
			&PlanOptions{SourceBasePath: importerTempDir()},
			0,
			&fakeExecWiki{hash: "h1"},
			slog.Default(),
		).WithResumeState(1, nil)

		result, err := executor.Execute(newFixtureUserID("user-1"))
		Expect(result).To(BeNil())
		Expect(err).To(MatchError(ErrImportResumeTreeHashMissing))
	})

	ginkgo.It("honors cancellation before the next item is processed", func() {
		executor := NewExecutor(
			&PlanResult{
				TreeHash: "h1",
				Items: []PlanItem{
					{SourcePath: newFixtureWorkspaceSourcePath("a.md"), TargetPath: newFixtureRoutePath("a"), Title: "A", Kind: tree.NodeKindPage, Action: PlanActionCreate},
				},
			},
			&PlanOptions{SourceBasePath: importerTempDir()},
			0,
			&fakeExecWiki{hash: "h1"},
			slog.Default(),
		).WithCancelCheck(func() bool {
			return true
		})

		result, err := executor.Execute(newFixtureUserID("user-1"))
		Expect(err).To(MatchError(ErrImportCanceled))
		Expect(result).To(MatchExecutionResultCounts(0, 0, BeEmpty()))
	})

	ginkgo.It("skips create items when page creation, source loading, or page update fails", func() {
		tmp := importerTempDir()
		importerWriteFile(tmp, "update.md", "# Update")
		updateFailedErr := errors.New("update failed")
		wiki := &fakeExecWiki{
			hash: "h1",
			ensureFn: func(userID tree.UserID, targetPath tree.RoutePath, title string, kind *tree.NodeKind) (*tree.Page, error) {
				if targetPath == "nil-page" {
					var missingCreatedPage *tree.Page
					return missingCreatedPage, nil
				}
				return &tree.Page{PageNode: &tree.PageNode{ID: newFixturePageID("p1"), Title: title, Slug: newFixtureSlug("slug"), Kind: *kind}}, nil
			},
			updateFn: func(userID tree.UserID, id tree.PageID, title string, slug tree.Slug, content *string, kind *tree.NodeKind) (*tree.Page, error) {
				return nil, updateFailedErr
			},
		}

		executor := NewExecutor(
			&PlanResult{
				TreeHash: "h1",
				Items: []PlanItem{
					{SourcePath: newFixtureWorkspaceSourcePath("nil.md"), TargetPath: newFixtureRoutePath("nil-page"), Title: "Nil", Kind: tree.NodeKindPage, Action: PlanActionCreate},
					{SourcePath: newFixtureWorkspaceSourcePath("missing.md"), TargetPath: newFixtureRoutePath("missing"), Title: "Missing", Kind: tree.NodeKindPage, Action: PlanActionCreate},
					{SourcePath: newFixtureWorkspaceSourcePath("update.md"), TargetPath: newFixtureRoutePath("update"), Title: "Update", Kind: tree.NodeKindPage, Action: PlanActionCreate},
				},
			},
			&PlanOptions{SourceBasePath: tmp},
			0,
			wiki,
			slog.Default(),
		)

		result, err := executor.Execute(newFixtureUserID("user-1"))
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(MatchExecutionResultCounts(0, 3, ConsistOf(
			HaveExecutionItemErrorCode(ImportErrorCodeCreatePageFailed),
			HaveExecutionItemErrorCode(ImportErrorCodeLoadSourceFailed),
			HaveExecutionItemErrorCode(ImportErrorCodeUpdatePageFailed),
		)))
	})

	ginkgo.It("skips create items when transformation or imported-content rendering fails", func() {
		tmp := importerTempDir()
		importerWriteFile(tmp, "asset-error.md", "![Asset](./missing.png)")
		importerWriteFile(tmp, "render-error.md", "# Render Error")
		importerWriteFile(tmp, "missing.png", "png-bytes")

		uploadFailedErr := errors.New("upload failed")
		wiki := &fakeExecWiki{
			hash: "h1",
			ensureFn: func(userID tree.UserID, targetPath tree.RoutePath, title string, kind *tree.NodeKind) (*tree.Page, error) {
				pageID := newFixturePageID("p1")
				if targetPath == "render-error" {
					pageID = "."
				}
				return &tree.Page{PageNode: &tree.PageNode{ID: pageID, Title: title, Slug: newFixtureSlug("slug"), Kind: *kind}}, nil
			},
			uploadFn: func(userID tree.UserID, pageID tree.PageID, file multipart.File, filename tree.AssetName, byteCap shared.MaxBytes) (string, error) {
				return "", uploadFailedErr
			},
		}

		executor := NewExecutor(
			&PlanResult{
				TreeHash: "h1",
				Items: []PlanItem{
					{SourcePath: newFixtureWorkspaceSourcePath("asset-error.md"), TargetPath: newFixtureRoutePath("asset-error"), Title: "Asset Error", Kind: tree.NodeKindPage, Action: PlanActionCreate},
					{SourcePath: newFixtureWorkspaceSourcePath("render-error.md"), TargetPath: newFixtureRoutePath("render-error"), Title: "Render Error", Kind: tree.NodeKindPage, Action: PlanActionCreate},
				},
			},
			&PlanOptions{SourceBasePath: tmp},
			0,
			wiki,
			slog.Default(),
		)

		result, err := executor.Execute(newFixtureUserID("user-1"))
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(MatchExecutionResultCounts(0, 2, ConsistOf(
			HaveExecutionItemErrorCode(ImportErrorCodeTransformContentFailed),
			HaveExecutionItemErrorCode(ImportErrorCodeRenderImportedContent),
		)))
	})

	ginkgo.It("fills a missing TreeHashBefore when resuming from partial results", func() {
		tmp := importerTempDir()
		importerWriteFile(tmp, "resume.md", "# Resume")
		progresses := []ExecutionProgress{}

		executor := NewExecutor(
			&PlanResult{
				TreeHash: "original",
				Items: []PlanItem{
					{SourcePath: newFixtureWorkspaceSourcePath("resume.md"), TargetPath: newFixtureRoutePath("resume"), Title: "Resume", Kind: tree.NodeKindPage, Action: PlanActionCreate},
				},
			},
			&PlanOptions{SourceBasePath: tmp},
			0,
			&fakeExecWiki{hash: "partial"},
			slog.Default(),
		).WithResumeState(1, &ExecutionResult{
			TreeHash: "partial",
		}).WithProgressCallback(func(progress ExecutionProgress, result *ExecutionResult) {
			progresses = append(progresses, progress)
			Expect(result.TreeHashBefore).To(Equal("partial"))
		})

		result, err := executor.Execute(newFixtureUserID("user-1"))
		Expect(err).NotTo(HaveOccurred())
		Expect(result.TreeHashBefore).To(Equal("partial"))
		Expect(progresses).To(HaveExactElements(
			gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"ProcessedItems": Equal(1),
				"TotalItems":     Equal(1),
				"StartedAt":      Not(BeNil()),
			}),
			gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"ProcessedItems": Equal(1),
				"TotalItems":     Equal(1),
				"FinishedAt":     Not(BeNil()),
			}),
		))
	})
})

var _ = ginkgo.Describe("stored import plan cancellation requests", ginkgo.Label("unit"), func() {
	ginkgo.It("records a running plan cancel request and is idempotent", func() {
		store := NewPlanStore()
		Expect(store.Set(&StoredPlan{
			Plan:            &PlanResult{ID: "plan-1"},
			ExecutionStatus: ExecutionStatusRunning,
		})).To(Succeed())

		plan, err := requestCancelResult(store)
		Expect(err).NotTo(HaveOccurred())
		Expect(plan).To(HaveStoredPlanCancellation(storedPlanCancellationRequested))
		stored, err := store.Get()
		Expect(err).To(Succeed())
		Expect(stored).To(HaveStoredPlanCancellation(storedPlanCancellationRequested))

		plan, err = requestCancelResult(store)
		Expect(err).To(MatchError(errImporterCancellationNotRequested))
		Expect(plan).To(HaveStoredPlanCancellation(storedPlanCancellationRequested))
	})

	ginkgo.It("does not request cancellation for a non-running plan", func() {
		store := NewPlanStore()
		Expect(store.Set(&StoredPlan{
			Plan:            &PlanResult{ID: "plan-1"},
			ExecutionStatus: ExecutionStatusPlanned,
		})).To(Succeed())

		plan, err := requestCancelResult(store)
		Expect(err).To(MatchError(errImporterCancellationNotRequested))
		Expect(plan).To(HaveStoredPlanCancellation(storedPlanCancellationClear))
	})
})

var _ = ginkgo.Describe("stored import plan execution lifecycle", ginkgo.Label("unit"), func() {
	ginkgo.It("starts planned execution by resetting stale execution state", func() {
		store := NewPlanStore()

		_, err := startStoredPlanExecutionResult(store, newFixtureUserID("user-1"))
		Expect(err).To(MatchError(ErrNoPlan))

		errMsg := "previous failure"
		currentSource := newFixtureWorkspaceSourcePath("old.md").FilesystemPath()
		Expect(store.Set(&StoredPlan{
			Plan: &PlanResult{
				ID: "plan-1",
				Items: []PlanItem{
					{SourcePath: newFixtureWorkspaceSourcePath("a.md")},
					{SourcePath: newFixtureWorkspaceSourcePath("b.md")},
				},
			},
			ExecutionStatus: ExecutionStatusFailed,
			ExecutionUserID: newFixtureUserID("old-user").MetadataValue(),
			CancelRequested: true,
			ExecutionResult: &ExecutionResult{ImportedCount: 9},
			ExecutionError:  &errMsg,
			ExecutionProgress: ExecutionProgress{
				ProcessedItems:        7,
				TotalItems:            7,
				CurrentItemSourcePath: &currentSource,
			},
		})).To(Succeed())

		plan, err := startStoredPlanExecutionResult(store, newFixtureUserID("user-1"))
		Expect(err).NotTo(HaveOccurred())
		Expect(plan).To(HaveFreshRunningStoredPlan(newFixtureUserID("user-1"), 2))

		plan, err = startStoredPlanExecutionResult(store, newFixtureUserID("user-2"))
		Expect(err).To(MatchError(errImporterExecutionNotStarted))
		Expect(plan.ExecutionUserID).To(Equal(newFixtureUserID("user-1").MetadataValue()))
	})

	ginkgo.It("finishes execution with completed, failed, and canceled terminal states", func() {
		currentSource := newFixtureWorkspaceSourcePath("current.md").FilesystemPath()
		store := NewPlanStore()
		Expect(store.Set(&StoredPlan{
			Plan:            &PlanResult{ID: "plan-1"},
			ExecutionStatus: ExecutionStatusRunning,
			ExecutionProgress: ExecutionProgress{
				ProcessedItems:        1,
				TotalItems:            2,
				CurrentItemSourcePath: &currentSource,
			},
		})).To(Succeed())

		Expect(store.FinishExecution("other-plan", nil, errors.New("ignored"))).To(Succeed())
		state, err := store.Get()
		Expect(err).NotTo(HaveOccurred())
		Expect(state.ExecutionStatus).To(Equal(ExecutionStatusRunning))

		result := &ExecutionResult{ImportedCount: 1}
		Expect(store.FinishExecution("plan-1", result, ErrImportCanceled)).To(Succeed())
		state, err = store.Get()
		Expect(err).NotTo(HaveOccurred())
		Expect(state).To(HaveCanceledStoredPlan(result))

		failed := NewPlanStore()
		Expect(failed.Set(&StoredPlan{Plan: &PlanResult{ID: "plan-2"}, ExecutionStatus: ExecutionStatusRunning})).To(Succeed())
		Expect(failed.FinishExecution("plan-2", nil, errors.New("boom"))).To(Succeed())
		failedState, err := failed.Get()
		Expect(err).NotTo(HaveOccurred())
		Expect(failedState).To(gstruct.PointTo(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"ExecutionStatus": Equal(ExecutionStatusFailed),
			"ExecutionError":  gstruct.PointTo(Equal("boom")),
		})))

		completed := NewPlanStore()
		Expect(completed.Set(&StoredPlan{
			Plan:            &PlanResult{ID: "plan-3"},
			ExecutionStatus: ExecutionStatusRunning,
			ExecutionProgress: ExecutionProgress{
				ProcessedItems: 1,
				TotalItems:     3,
			},
		})).To(Succeed())
		Expect(completed.FinishExecution("plan-3", &ExecutionResult{ImportedCount: 3}, nil)).To(Succeed())
		completedState, err := completed.Get()
		Expect(err).NotTo(HaveOccurred())
		Expect(completedState).To(gstruct.PointTo(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"ExecutionStatus": Equal(ExecutionStatusCompleted),
			"ExecutionProgress": gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"ProcessedItems": Equal(3),
				"FinishedAt":     Not(BeNil()),
			}),
		})))
	})

	ginkgo.It("updates progress only for the active plan and clones partial results", func() {
		store := NewPlanStore()
		Expect(store.Set(&StoredPlan{
			Plan:            &PlanResult{ID: "plan-1"},
			ExecutionStatus: ExecutionStatusRunning,
		})).To(Succeed())

		now := time.Now()
		sourcePath := newFixtureWorkspaceSourcePath("docs/current.md").FilesystemPath()
		partial := &ExecutionResult{
			ImportedCount: 1,
			Items: []ExecutionItemResult{
				{SourcePath: newFixtureWorkspaceSourcePath("docs/old.md"), TargetPath: newFixtureRoutePath("docs/old"), Action: ExecutionActionCreated},
			},
		}

		Expect(store.UpdateExecutionProgress("other-plan", ExecutionProgress{ProcessedItems: 9, TotalItems: 9}, partial)).To(Succeed())
		state, err := store.Get()
		Expect(err).NotTo(HaveOccurred())
		Expect(state).To(gstruct.PointTo(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"ExecutionProgress": gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"ProcessedItems": BeZero(),
			}),
			"ExecutionResult": BeNil(),
		})))

		Expect(store.UpdateExecutionProgress("plan-1", ExecutionProgress{
			ProcessedItems:        1,
			TotalItems:            2,
			CurrentItemSourcePath: &sourcePath,
			StartedAt:             &now,
			FinishedAt:            &now,
		}, partial)).To(Succeed())
		partial.Items[0].Action = ExecutionActionSkipped

		state, err = store.Get()
		Expect(err).NotTo(HaveOccurred())
		Expect(state).To(gstruct.PointTo(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"ExecutionProgress": gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"ProcessedItems":        Equal(1),
				"TotalItems":            Equal(2),
				"CurrentItemSourcePath": Equal(&sourcePath),
				"StartedAt":             gstruct.PointTo(BeTemporally("==", now)),
				"FinishedAt":            gstruct.PointTo(BeTemporally("==", now)),
			}),
			"ExecutionResult": gstruct.PointTo(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"Items": ContainElement(HaveField("Action", Equal(ExecutionActionCreated))),
			})),
		})))
	})
})
