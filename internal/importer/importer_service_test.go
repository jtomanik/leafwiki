package importer

import (
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/perber/wiki/internal/core/tree"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
)

// --- Helpers ----------------------------------------------------------------

func mustWrite(base, rel, content string) string {
	ginkgo.GinkgoHelper()

	return importerWriteFile(base, rel, content)
}

func newServiceWithFakeWiki(w *fakeWiki) *ImporterService {
	ginkgo.GinkgoHelper()

	planner := NewPlanner(w, tree.NewSlugService())
	importerDir := filepath.Join(importerTempDir(), ".importer")
	store := NewPlanStore(filepath.Join(importerDir, "current-plan.json"))
	return &ImporterService{
		planner:          planner,
		planStore:        store,
		extractor:        NewZipExtractor(), // unused in these tests
		logger:           slog.Default().With("component", "ImporterServiceTest"),
		workspaceBaseDir: filepath.Join(importerDir, "workspaces"),
	}
}

func waitForExecutionStatus(is *ImporterService, want ExecutionStatus) *CurrentPlanState {
	ginkgo.GinkgoHelper()

	var state *CurrentPlanState
	Eventually(func(g Gomega) {
		var err error
		state, err = is.GetCurrentPlan()
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(state.ExecutionStatus).To(Equal(want))
	}).
		WithTimeout(3 * time.Second).
		WithPolling(10 * time.Millisecond).
		Should(Succeed())
	return state
}

// --- Tests ------------------------------------------------------------------

var _ = ginkgo.Describe("import plan creation persistence", func() {
	ginkgo.It("stores created folder import plans", func() {
		tmp := importerTempDir()
		mustWrite(tmp, "a.md", "# A\nbody")

		w := &fakeWiki{treeHash: "h1", lookups: map[string]*tree.PathLookup{}}
		is := newServiceWithFakeWiki(w)

		plan, err := is.CreateImportPlanFromFolder(tmp, "")
		Expect(err).To(Succeed())
		Expect(plan).NotTo(BeNil())
		Expect(plan.Items).To(HaveLen(1))
		// plan should have correct options

		_, err = is.GetCurrentPlan()
		Expect(err).To(Succeed())

	})
})

var _ = ginkgo.Describe("import plan workspace replacement", func() {
	ginkgo.It("removes the previous workspace when replacing a planned import", func() {
		// old workspace with a marker file
		oldWS := importerTempDir()
		marker := mustWrite(oldWS, "marker.txt", "x")

		// new workspace with md
		newWS := importerTempDir()
		mustWrite(newWS, "b.md", "# B")

		w := &fakeWiki{treeHash: "h1", lookups: map[string]*tree.PathLookup{}}
		is := newServiceWithFakeWiki(w)

		// seed old plan in store
		err := is.planStore.Set(&StoredPlan{
			Plan:          &PlanResult{ID: "old", TreeHash: "h1"},
			PlanOptions:   PlanOptions{SourceBasePath: oldWS},
			WorkspaceRoot: oldWS,
			CreatedAt:     time.Now(),
		})
		Expect(err).To(Succeed())

		_, err = is.CreateImportPlanFromFolder(newWS, "")
		Expect(err).To(Succeed())

		// old workspace should be removed
		_, statErr := os.Stat(marker)
		Expect(statErr).To(MatchError(os.ErrNotExist))

		// store should now point to new workspace

		_, err = is.GetCurrentPlan()
		Expect(err).To(Succeed())

	})
})

var _ = ginkgo.Describe("current import plan reads without a plan", func() {
	ginkgo.It("returns a no-plan error when no current plan is stored", func() {
		w := &fakeWiki{treeHash: "h1", lookups: map[string]*tree.PathLookup{}}
		is := newServiceWithFakeWiki(w)

		_, err := is.GetCurrentPlan()
		Expect(err).To(MatchError(ErrNoPlan))

	})
})

var _ = ginkgo.Describe("current import plan clearing", func() {
	ginkgo.It("clears stored plans and rejects later reads", func() {
		tmp := importerTempDir()
		mustWrite(tmp, "a.md", "# A")

		w := &fakeWiki{treeHash: "h1", lookups: map[string]*tree.PathLookup{}}
		is := newServiceWithFakeWiki(w)

		_, err := is.CreateImportPlanFromFolder(tmp, "")
		Expect(err).To(Succeed())

		Expect(is.ClearCurrentPlan()).To(Succeed())
		_, err = is.GetCurrentPlan()
		Expect(err).To(MatchError(ErrNoPlan))

	})
})

var _ = ginkgo.Describe("import execution without a current plan", func() {
	ginkgo.It("rejects execution without a current plan", func() {
		w := &fakeWiki{treeHash: "h1", lookups: map[string]*tree.PathLookup{}}
		is := newServiceWithFakeWiki(w)

		_, err := is.ExecuteCurrentPlan("user1")
		Expect(err).To(MatchError(ErrNoPlan))

	})
})

var _ = ginkgo.Describe("background import execution", func() {
	ginkgo.It("starts execution asynchronously and records completion progress", func() {
		ws := importerTempDir()
		mustWrite(ws, "a.md", "# A\nbody")

		allowEnsure := make(chan struct{})
		w := &fakeWiki{
			treeHash: "h1",
			lookups:  map[string]*tree.PathLookup{},
			ensureFn: func(userID tree.UserID, targetPath tree.RoutePath, title string, kind *tree.NodeKind) (*tree.Page, error) {
				defer ginkgo.GinkgoRecover()
				Eventually(allowEnsure).Should(BeClosed())
				return &tree.Page{PageNode: &tree.PageNode{ID: "p1", Title: title, Slug: "slug", Kind: *kind}}, nil
			},
		}
		is := newServiceWithFakeWiki(w)

		_, err := is.CreateImportPlanFromFolder(ws, "")
		Expect(err).To(Succeed())

		state, err := startCurrentPlanExecutionResult(is, "user1")
		Expect(err).To(Succeed())
		Expect(state).To(SatisfyAll(
			HaveField("ExecutionStatus", Equal(ExecutionStatusRunning)),
			HaveField("TotalItems", Equal(1)),
			HaveField("ProcessedItems", BeZero()),
			HaveField("StartedAt", Not(BeNil())),
		))

		runningState, err := is.GetCurrentPlan()
		Expect(err).To(Succeed())
		Expect(runningState.ExecutionStatus).To(Equal(ExecutionStatusRunning))

		close(allowEnsure)

		completedState := waitForExecutionStatus(is, ExecutionStatusCompleted)
		Expect(completedState).To(SatisfyAll(
			HaveField("ExecutionResult", Not(BeNil())),
			HaveField("ExecutionResult.ImportedCount", Equal(1)),
			HaveField("ProcessedItems", Equal(1)),
			HaveField("TotalItems", Equal(1)),
			HaveField("CurrentItemSourcePath", BeNil()),
			HaveField("FinishedAt", Not(BeNil())),
		))

	})
})

var _ = ginkgo.Describe("current import plan clearing while execution is running", func() {
	ginkgo.It("rejects clearing while execution is running", func() {
		ws := importerTempDir()
		mustWrite(ws, "a.md", "# A\nbody")

		allowEnsure := make(chan struct{})
		w := &fakeWiki{
			treeHash: "h1",
			lookups:  map[string]*tree.PathLookup{},
			ensureFn: func(userID tree.UserID, targetPath tree.RoutePath, title string, kind *tree.NodeKind) (*tree.Page, error) {
				defer ginkgo.GinkgoRecover()
				Eventually(allowEnsure).Should(BeClosed())
				return &tree.Page{PageNode: &tree.PageNode{ID: "p1", Title: title, Slug: "slug", Kind: *kind}}, nil
			},
		}
		is := newServiceWithFakeWiki(w)

		_, err := is.CreateImportPlanFromFolder(ws, "")
		Expect(err).To(Succeed())
		_, err = startCurrentPlanExecutionResult(is, "user1")
		Expect(err).To(Succeed())

		err = is.ClearCurrentPlan()
		Expect(err).To(MatchError(ErrImportExecutionRunning))

		close(allowEnsure)
		waitForExecutionStatus(is, ExecutionStatusCompleted)

	})
})

var _ = ginkgo.Describe("import cancellation between items", func() {
	ginkgo.It("requests cancellation and records canceled progress", func() {
		ws := importerTempDir()
		mustWrite(ws, "a.md", "# A\nbody")
		mustWrite(ws, "b.md", "# B\nbody")

		enterFirstEnsure := make(chan struct{}, 1)
		allowFirstEnsure := make(chan struct{})
		w := &fakeWiki{
			treeHash: "h1",
			lookups:  map[string]*tree.PathLookup{},
			ensureFn: func(userID tree.UserID, targetPath tree.RoutePath, title string, kind *tree.NodeKind) (*tree.Page, error) {
				if targetPath == "a" {
					enterFirstEnsure <- struct{}{}
					defer ginkgo.GinkgoRecover()
					Eventually(allowFirstEnsure).Should(BeClosed())
				}
				return &tree.Page{PageNode: &tree.PageNode{ID: "p1", Title: title, Slug: "slug", Kind: *kind}}, nil
			},
		}
		is := newServiceWithFakeWiki(w)

		_, err := is.CreateImportPlanFromFolder(ws, "")
		Expect(err).To(Succeed())
		_, err = startCurrentPlanExecutionResult(is, "user1")
		Expect(err).To(Succeed())

		Eventually(enterFirstEnsure).Should(Receive())

		state, err := cancelCurrentPlanResult(is)
		Expect(err).To(Succeed())
		Expect(state.CancelRequested).To(BeTrue())

		close(allowFirstEnsure)

		canceledState := waitForExecutionStatus(is, ExecutionStatusCanceled)
		Expect(canceledState).To(SatisfyAll(
			HaveField("ExecutionResult", Not(BeNil())),
			HaveField("ExecutionResult.ImportedCount", Equal(1)),
			HaveField("ProcessedItems", Equal(1)),
			HaveField("TotalItems", Equal(2)),
		))

	})
})

var _ = ginkgo.Describe("persisted running import resumption", func() {
	ginkgo.It("resumes running imports and completes remaining items", func() {
		workspaceRoot := importerTempDir()
		mustWrite(workspaceRoot, "a.md", "# A\nbody")
		mustWrite(workspaceRoot, "b.md", "# B\nbody")

		stateRoot := importerTempDir()
		stateFile := filepath.Join(stateRoot, "current-plan.json")
		w := &fakeWiki{treeHash: "partial-tree", lookups: map[string]*tree.PathLookup{}}
		planner := NewPlanner(w, tree.NewSlugService())
		store := NewPlanStore(stateFile)

		service := &ImporterService{
			planner:          planner,
			planStore:        store,
			extractor:        NewZipExtractor(),
			logger:           slog.Default().With("component", "ImporterServiceTest"),
			workspaceBaseDir: filepath.Join(stateRoot, "workspaces"),
		}

		plan, err := service.CreateImportPlanFromFolder(workspaceRoot, "")
		Expect(err).To(Succeed())
		plan.TreeHash = "original-tree"

		sp, err := startStoredPlanExecutionResult(store, "user1")
		Expect(err).To(Succeed())
		Expect(sp.ExecutionStatus).To(Equal(ExecutionStatusRunning))
		startedAt := time.Now()
		err = store.UpdateExecutionProgress(plan.ID, ExecutionProgress{
			ProcessedItems: 1,
			TotalItems:     2,
			StartedAt:      &startedAt,
		}, &ExecutionResult{
			ImportedCount:  1,
			TreeHashBefore: "original-tree",
			TreeHash:       "partial-tree",
			Items: []ExecutionItemResult{
				{SourcePath: "a.md", TargetPath: "a", Action: ExecutionActionCreated},
			},
		})
		Expect(err).To(Succeed())

		resumed := NewImporterService(planner, NewPlanStore(stateFile), filepath.Join(stateRoot, "workspaces"), 0)

		completedState := waitForExecutionStatus(resumed, ExecutionStatusCompleted)
		Expect(completedState).To(SatisfyAll(
			HaveField("ExecutionResult", Not(BeNil())),
			HaveField("ExecutionResult.ImportedCount", Equal(2)),
			HaveField("ProcessedItems", Equal(2)),
			HaveField("TotalItems", Equal(2)),
		))

	})
})

var _ = ginkgo.Describe("resumed import with changed tree hash", func() {
	ginkgo.It("fails resumed imports when the tree hash changed", func() {
		workspaceRoot := importerTempDir()
		mustWrite(workspaceRoot, "a.md", "# A\nbody")
		mustWrite(workspaceRoot, "b.md", "# B\nbody")

		stateRoot := importerTempDir()
		stateFile := filepath.Join(stateRoot, "current-plan.json")
		w := &fakeWiki{treeHash: "changed-tree", lookups: map[string]*tree.PathLookup{}}
		planner := NewPlanner(w, tree.NewSlugService())
		store := NewPlanStore(stateFile)

		service := &ImporterService{
			planner:          planner,
			planStore:        store,
			extractor:        NewZipExtractor(),
			logger:           slog.Default().With("component", "ImporterServiceTest"),
			workspaceBaseDir: filepath.Join(stateRoot, "workspaces"),
		}

		plan, err := service.CreateImportPlanFromFolder(workspaceRoot, "")
		Expect(err).To(Succeed())
		plan.TreeHash = "original-tree"
		err = store.Set(&StoredPlan{
			Plan:            plan,
			PlanOptions:     PlanOptions{SourceBasePath: workspaceRoot},
			WorkspaceRoot:   workspaceRoot,
			CreatedAt:       time.Now(),
			ExecutionStatus: ExecutionStatusRunning,
			ExecutionUserID: "user1",
			ExecutionResult: &ExecutionResult{
				ImportedCount:  1,
				TreeHashBefore: "original-tree",
				TreeHash:       "partially-imported-tree",
				Items: []ExecutionItemResult{
					{SourcePath: "a.md", TargetPath: "a", Action: ExecutionActionCreated},
				},
			},
			ExecutionProgress: ExecutionProgress{
				ProcessedItems: 1,
				TotalItems:     2,
			},
		})
		Expect(err).To(Succeed())

		resumed := NewImporterService(planner, NewPlanStore(stateFile), filepath.Join(stateRoot, "workspaces"), 0)
		failedState := waitForExecutionStatus(resumed, ExecutionStatusFailed)
		Expect(failedState).To(gstruct.PointTo(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"ExecutionResult":    BeNil(),
			"ExecutionErrorCode": gstruct.PointTo(Equal(ExecutionErrorCodeImportPlanStale)),
			"ExecutionProgress": gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"ProcessedItems": Equal(1),
				"TotalItems":     Equal(2),
			}),
		})))

	})
})

var _ = ginkgo.Describe("import service execution with source frontmatter", func() {
	ginkgo.It("writes canonical metadata and drops importer-owned legacy fields", func() {
		ws := importerTempDir()
		mustWrite(ws, "a.md", "---\naliases:\n  - x\ncustom_key: keep-me\nleafwiki_id: source-id\nleafwiki_title: Source Title\ntitle: X\n---\n\n# Heading\nBody")

		w := &fakeWiki{treeHash: "h1", lookups: map[string]*tree.PathLookup{}}
		is := newServiceWithFakeWiki(w)

		plan, err := is.CreateImportPlanFromFolder(ws, "")
		Expect(err).To(Succeed())
		Expect(plan.TreeHash).To(Equal("h1"))

		res, err := is.ExecuteCurrentPlan("user1")
		Expect(err).To(Succeed())

		Expect(res).To(MatchExecutionResultCounts(1, 0, HaveLen(1)))
		Expect(w).To(MatchFakeExecWikiState(SatisfyAll(
			HaveField("EnsureCalls", Equal(1)),
			HaveField("UpdateCalls", Equal(1)),
			HaveField("LastUpdatedContent", Not(BeNil())),
		)))
		raw := *w.lastUpdatedContent
		Expect(raw).To(HavePrefix("<!-- leafwiki\n"))
		Expect(raw).NotTo(HavePrefix("---\n"))
		doc, err := importedPageDocumentResult(raw)
		Expect(err).To(Succeed())
		Expect(doc).To(HaveImportedPageDocument(
			Equal("\n# Heading\nBody"),
			HaveKeyWithValue("custom_key", "keep-me"),
			SatisfyAll(
				Not(HaveKey("title")),
				HaveKeyWithValue("aliases", ConsistOf("x")),
			),
		))
		Expect(raw).NotTo(ContainSubstring("leafwiki_id: source-id"))
		Expect(raw).NotTo(ContainSubstring("leafwiki_title: Source Title"))

	})
})

var _ = ginkgo.Describe("import service execution with stale plan", func() {
	ginkgo.It("rejects stale plan execution", func() {
		ws := importerTempDir()
		mustWrite(ws, "a.md", "# A")

		w := &fakeWiki{treeHash: "h1", lookups: map[string]*tree.PathLookup{}}
		is := newServiceWithFakeWiki(w)

		plan, err := is.CreateImportPlanFromFolder(ws, "")
		Expect(err).To(Succeed())
		// make plan stale
		plan.TreeHash = "OLD"

		_, err = is.ExecuteCurrentPlan("user1")
		Expect(err).To(MatchError(ErrImportPlanStale))

	})
})

var _ = ginkgo.Describe("markdown entry discovery", func() {
	ginkgo.It("finds markdown entries recursively and ignores non-markdown files", func() {
		base := importerTempDir()
		mustWrite(base, "a.md", "x")
		mustWrite(base, "b.txt", "x")
		mustWrite(base, "sub/c.MD", "x")
		mustWrite(base, "sub/deeper/d.md", "x")

		got, err := FindMarkdownEntries(base)
		Expect(err).To(Succeed())

		// collect paths in a set for stable assertion (WalkDir order is OS-dependent)
		set := map[string]bool{}
		for _, e := range got {
			sourcePath := e.SourcePath.FilesystemPath()
			set[sourcePath] = true
			// should be slash-normalized
			Expect(sourcePath).NotTo(ContainSubstring(`\`))
		}

		Expect(set).To(SatisfyAll(
			HaveKey("a.md"),
			HaveKey("sub/c.MD"),
			HaveKey("sub/deeper/d.md"),
			Not(HaveKey("b.txt")),
		))

	})
})

var _ = ginkgo.Describe("import plan creation with target base path", func() {
	ginkgo.It("stores target base path and applies it to planned items", func() {
		tmp := importerTempDir()
		mustWrite(tmp, "a.md", "# A\nbody")

		w := &fakeWiki{treeHash: "h1", lookups: map[string]*tree.PathLookup{}}
		is := newServiceWithFakeWiki(w)

		plan, err := is.CreateImportPlanFromFolder(tmp, "docs/imports")
		Expect(err).To(Succeed())
		Expect(plan).NotTo(BeNil())
		Expect(plan.Items).To(HaveLen(1))

		// Verify the plan item has the correct target path with the base path
		item := plan.Items[0]
		Expect(item.TargetPath).To(Equal(newFixtureRoutePath("docs/imports/a")))

		// Verify the stored plan options has the correct target base path
		sp, err := is.planStore.Get()
		Expect(err).To(Succeed())
		Expect(sp.PlanOptions.TargetBasePath).To(Equal("docs/imports"))

	})
})

var _ = ginkgo.Describe("markdown entry extension matching", func() {
	ginkgo.It("includes markdown files with mixed-case extensions", func() {
		base := importerTempDir()
		mustWrite(base, "a.MD", "x")
		mustWrite(base, "b.mD", "x")
		mustWrite(base, "c.Md", "x")
		mustWrite(base, "d.txt", "x")

		got, err := FindMarkdownEntries(base)
		Expect(err).To(Succeed())

		set := map[string]bool{}
		for _, e := range got {
			set[e.SourcePath.FilesystemPath()] = true
		}

		Expect(set).To(SatisfyAll(
			HaveKey("a.MD"),
			HaveKey("b.mD"),
			HaveKey("c.Md"),
			Not(HaveKey("d.txt")),
		))

	})
})
