package importer

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/perber/wiki/internal/core/assets"
	"github.com/perber/wiki/internal/core/shared"
	"github.com/perber/wiki/internal/core/tree"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
)

var _ = ginkgo.Describe("zip upload import planning", ginkgo.Label("unit"), func() {
	ginkgo.It("creates an import plan from an uploaded zip and stores the extracted workspace", func() {
		var zipBytes bytes.Buffer
		zipWriter := zip.NewWriter(&zipBytes)
		file, err := zipWriter.Create("docs/Imported.md")
		Expect(err).NotTo(HaveOccurred())
		_, err = file.Write([]byte("# Imported\nbody"))
		Expect(err).NotTo(HaveOccurred())
		Expect(zipWriter.Close()).To(Succeed())

		service := newServiceWithFakeWiki(&fakeWiki{treeHash: "h1", lookups: map[string]*tree.PathLookup{}})
		plan, err := service.CreateImportPlanFromZipUpload(bytes.NewReader(zipBytes.Bytes()), "wiki")
		Expect(err).NotTo(HaveOccurred())
		Expect(plan.Items).To(ConsistOf(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"SourcePath": Equal(newFixtureWorkspaceSourcePath("docs/Imported.md")),
			"TargetPath": Equal(newFixtureRoutePath("wiki/docs/imported")),
		})))

		state, err := service.planStore.Get()
		Expect(err).NotTo(HaveOccurred())
		Expect(state).To(gstruct.PointTo(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"WorkspaceRoot": SatisfyAll(
				Not(BeEmpty()),
				HavePrefix(service.workspaceBaseDir),
			),
		})))
		Expect(filepath.Base(state.WorkspaceRoot)).To(HavePrefix("import-"))
		Expect(os.ReadFile(filepath.Join(state.WorkspaceRoot, "docs", "Imported.md"))).To(Equal([]byte("# Imported\nbody")))
	})

	ginkgo.It("honors positive asset max-size overrides and ignores non-positive values", func() {
		service := newServiceWithFakeWiki(&fakeWiki{treeHash: "h1", lookups: map[string]*tree.PathLookup{}})

		Expect(service.assetMaxUploadSizeBytes).To(Equal(shared.MaxBytes(0)))
		service.SetAssetMaxUploadSizeBytes(shared.MaxBytes(2048))
		Expect(service.assetMaxUploadSizeBytes).To(Equal(shared.MaxBytes(2048)))
		service.SetAssetMaxUploadSizeBytes(0)
		Expect(service.assetMaxUploadSizeBytes).To(Equal(shared.MaxBytes(2048)))

		constructed := NewImporterService(newPlannerWithFake(&fakeWiki{}), NewPlanStore(), "", 0)
		Expect(importerServiceDefaultState(constructed)).To(Equal(importerServiceDefaults{
			assetMaxUploadSizeBytes: assets.DefaultMaxUploadSizeBytes,
			workspaceBaseDirPresent: true,
		}))
	})

	ginkgo.It("reports invalid zip uploads without storing a plan", func() {
		service := newServiceWithFakeWiki(&fakeWiki{treeHash: "h1", lookups: map[string]*tree.PathLookup{}})

		_, err := service.CreateImportPlanFromZipUpload(bytes.NewReader([]byte("not a zip")), "wiki")
		Expect(err).To(MatchError(zip.ErrFormat))

		_, err = service.GetCurrentPlan()
		Expect(err).To(MatchError(ErrNoPlan))
	})
})

var _ = ginkgo.Describe("Planner error edges", ginkgo.Label("unit"), func() {
	ginkgo.It("collects filename normalization and lookup errors", func() {
		tmp := importerTempDir()
		importerWriteFile(tmp, "!!!.md", "# Invalid")
		importerWriteFile(tmp, "lookup.md", "# Lookup")

		planner := newPlannerWithFake(&fakeWiki{treeHash: "h1", lookups: map[string]*tree.PathLookup{}})
		plan, err := planner.CreatePlan([]ImportMDFile{{SourcePath: "!!!.md"}}, PlanOptions{SourceBasePath: tmp})
		Expect(err).NotTo(HaveOccurred())
		Expect(plan.ErrorDetails).To(HaveImportPlanErrorCode(ImportErrorCodeNormalizeFilenameFailed))

		planner = newPlannerWithFake(&fakeWiki{
			treeHash:         "h1",
			lookups:          map[string]*tree.PathLookup{},
			lookupForKindErr: errors.New("lookup failed"),
		})
		plan, err = planner.CreatePlan([]ImportMDFile{{SourcePath: "lookup.md"}}, PlanOptions{SourceBasePath: tmp})
		Expect(err).NotTo(HaveOccurred())
		Expect(plan.ErrorDetails).To(HaveImportPlanErrorCode(ImportErrorCodeLookupPathFailed))
	})
})

var _ = ginkgo.Describe("import planning service error handling", ginkgo.Label("unit"), func() {
	ginkgo.It("surfaces folder planning cleanup, discovery, planning, and persistence errors", func() {
		service := newServiceWithFakeWiki(&fakeWiki{treeHash: "h1", lookups: map[string]*tree.PathLookup{}})

		_, err := service.CreateImportPlanFromFolder(filepath.Join(importerTempDir(), "missing"), "")
		Expect(err).To(MatchError(os.ErrNotExist))

		workspace := importerTempDir()
		importerWriteFile(workspace, "page.md", "# Page")

		originalGenerateID := importerGenerateUniqueID
		ginkgo.DeferCleanup(func() {
			importerGenerateUniqueID = originalGenerateID
		})
		idFailedErr := errors.New("id failed")
		importerGenerateUniqueID = func() (string, error) {
			return "", idFailedErr
		}
		_, err = service.CreateImportPlanFromFolder(workspace, "")
		Expect(err).To(MatchError(idFailedErr))
		importerGenerateUniqueID = originalGenerateID

		service.planStore.stateFile = importerBadStateFile()
		_, err = service.CreateImportPlanFromFolder(workspace, "")
		Expect(err).To(MatchError(ErrImportStateUnavailable))

		service = newServiceWithFakeWiki(&fakeWiki{treeHash: "h1", lookups: map[string]*tree.PathLookup{}})
		Expect(service.planStore.Set(&StoredPlan{
			Plan:            &PlanResult{ID: "old"},
			WorkspaceRoot:   importerTempDir(),
			ExecutionStatus: ExecutionStatusPlanned,
		})).To(Succeed())
		service.planStore.stateFile = importerBadStateFile()
		_, err = service.CreateImportPlanFromFolder(workspace, "")
		Expect(err).To(MatchError(ErrImportStateUnavailable))

		service = newServiceWithFakeWiki(&fakeWiki{treeHash: "h1", lookups: map[string]*tree.PathLookup{}})
		Expect(service.planStore.Set(&StoredPlan{
			Plan:            &PlanResult{ID: "old"},
			WorkspaceRoot:   importerTempDir(),
			ExecutionStatus: ExecutionStatusPlanned,
		})).To(Succeed())
		originalRemoveAll := importerServiceRemoveAll
		ginkgo.DeferCleanup(func() {
			importerServiceRemoveAll = originalRemoveAll
		})
		removeFailedErr := errors.New("remove failed")
		importerServiceRemoveAll = func(path string) error {
			return removeFailedErr
		}
		_, err = service.CreateImportPlanFromFolder(workspace, "")
		Expect(err).To(MatchError(removeFailedErr))
		importerServiceRemoveAll = originalRemoveAll
	})

	ginkgo.It("logs clear and cleanup removal failures while preserving clear semantics", func() {
		service := newServiceWithFakeWiki(&fakeWiki{treeHash: "h1", lookups: map[string]*tree.PathLookup{}})
		Expect(service.planStore.Set(&StoredPlan{
			Plan:            &PlanResult{ID: "plan-1"},
			WorkspaceRoot:   importerTempDir(),
			ExecutionStatus: ExecutionStatusPlanned,
		})).To(Succeed())

		originalRemoveAll := importerServiceRemoveAll
		ginkgo.DeferCleanup(func() {
			importerServiceRemoveAll = originalRemoveAll
		})
		importerServiceRemoveAll = func(path string) error {
			return errors.New("remove failed")
		}
		Expect(service.ClearCurrentPlan()).To(Succeed())
		service.cleanupWorkspace("")
		service.cleanupWorkspace("missing")
		importerServiceRemoveAll = originalRemoveAll
	})

	ginkgo.It("surfaces execution start and finish persistence errors", func() {
		service := newServiceWithFakeWiki(&fakeWiki{treeHash: "h1", lookups: map[string]*tree.PathLookup{}})
		service.planStore.stateErr = ErrImportStateUnavailable
		_, err := startCurrentPlanExecutionResult(service, "user-1")
		Expect(err).To(MatchError(ErrImportStateUnavailable))

		workspace := importerTempDir()
		importerWriteFile(workspace, "page.md", "# Page")
		wiki := &fakeWiki{treeHash: "h1", lookups: map[string]*tree.PathLookup{}}
		service = newServiceWithFakeWiki(wiki)
		wiki.updateFn = func(userID tree.UserID, id tree.PageID, title string, slug tree.Slug, content *string, kind *tree.NodeKind) (*tree.Page, error) {
			service.planStore.stateFile = importerBadStateFile()
			return &tree.Page{PageNode: &tree.PageNode{ID: id, Title: title, Slug: slug, Kind: *kind}}, nil
		}
		plan, err := service.CreateImportPlanFromFolder(workspace, "")
		Expect(err).NotTo(HaveOccurred())

		_, err = service.ExecuteCurrentPlan("user-1")
		Expect(err).To(MatchError(ErrImportStateUnavailable))
		Expect(plan).NotTo(BeNil())
	})

	ginkgo.It("records async finish and progress persistence failures", func() {
		workspace := importerTempDir()
		importerWriteFile(workspace, "page.md", "# Page")
		finished := make(chan struct{})
		wiki := &fakeWiki{treeHash: "h1", lookups: map[string]*tree.PathLookup{}}
		service := newServiceWithFakeWiki(wiki)
		wiki.updateErr = nil
		wiki.ensureFn = func(userID tree.UserID, targetPath tree.RoutePath, title string, kind *tree.NodeKind) (*tree.Page, error) {
			service.planStore.stateFile = importerBadStateFile()
			close(finished)
			return &tree.Page{PageNode: &tree.PageNode{ID: "p1", Title: title, Slug: "slug", Kind: *kind}}, nil
		}

		_, err := service.CreateImportPlanFromFolder(workspace, "")
		Expect(err).NotTo(HaveOccurred())
		_, err = startCurrentPlanExecutionResult(service, "user-1")
		Expect(err).NotTo(HaveOccurred())
		Eventually(finished).Should(BeClosed())
		Eventually(func() error {
			_, err := service.planStore.Get()
			return err
		}).Should(MatchError(ErrImportStateUnavailable))

		service = newServiceWithFakeWiki(&fakeWiki{treeHash: "h1", lookups: map[string]*tree.PathLookup{}})
		service.planStore.stateErr = ErrImportStateUnavailable
		result, err := service.executeStoredPlan(&StoredPlan{
			Plan: &PlanResult{TreeHash: "h1", Items: []PlanItem{
				{SourcePath: "skipped.md", TargetPath: "skipped", Action: PlanActionSkip},
			}},
			PlanOptions: PlanOptions{SourceBasePath: importerTempDir()},
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(result.SkippedCount).To(Equal(1))
	})

	ginkgo.It("orders markdown discovery and reports filesystem failures", func() {
		base := importerTempDir()
		importerWriteFile(base, "z.md", "# Z")
		importerWriteFile(base, "index.md", "# Index")

		entries, err := FindMarkdownEntries(base)
		Expect(err).NotTo(HaveOccurred())
		Expect(entries[0].SourcePath.FilesystemPath()).To(Equal("index.md"))

		_, err = FindMarkdownEntries(filepath.Join(importerTempDir(), "missing"))
		Expect(err).To(MatchError(os.ErrNotExist))

		originalRel := importerServiceRel
		ginkgo.DeferCleanup(func() {
			importerServiceRel = originalRel
		})
		relErr := errors.New("rel failed")
		importerServiceRel = func(basepath, targpath string) (string, error) {
			return "", relErr
		}
		_, err = FindMarkdownEntries(base)
		Expect(err).To(MatchError(relErr))
	})

	ginkgo.It("cleans up extracted zip workspaces when plan creation fails", func() {
		var zipBytes bytes.Buffer
		zipWriter := zip.NewWriter(&zipBytes)
		file, err := zipWriter.Create("page.md")
		Expect(err).NotTo(HaveOccurred())
		_, err = file.Write([]byte("# Page"))
		Expect(err).NotTo(HaveOccurred())
		Expect(zipWriter.Close()).To(Succeed())

		service := newServiceWithFakeWiki(&fakeWiki{treeHash: "h1", lookups: map[string]*tree.PathLookup{}})
		originalGenerateID := importerGenerateUniqueID
		ginkgo.DeferCleanup(func() {
			importerGenerateUniqueID = originalGenerateID
		})
		idFailedErr := errors.New("id failed")
		importerGenerateUniqueID = func() (string, error) {
			return "", idFailedErr
		}
		_, err = service.CreateImportPlanFromZipUpload(bytes.NewReader(zipBytes.Bytes()), "")
		Expect(err).To(MatchError(idFailedErr))

		originalWorkspaceRemoveAll := zipWorkspaceRemoveAll
		ginkgo.DeferCleanup(func() {
			zipWorkspaceRemoveAll = originalWorkspaceRemoveAll
		})
		cleanupFailedErr := errors.New("cleanup failed")
		zipWorkspaceRemoveAll = func(path string) error {
			return cleanupFailedErr
		}
		_, err = service.CreateImportPlanFromZipUpload(bytes.NewReader(zipBytes.Bytes()), "")
		Expect(err).To(MatchError(idFailedErr))
	})

	ginkgo.It("surfaces temp zip storage seam failures", func() {
		service := newServiceWithFakeWiki(&fakeWiki{treeHash: "h1", lookups: map[string]*tree.PathLookup{}})

		originalMkdirAll := importerServiceMkdirAll
		ginkgo.DeferCleanup(func() {
			importerServiceMkdirAll = originalMkdirAll
		})
		mkdirFailedErr := errors.New("mkdir failed")
		importerServiceMkdirAll = func(path string, perm os.FileMode) error {
			return mkdirFailedErr
		}
		_, err := service.extractZipReaderToTemp(bytes.NewReader(nil))
		Expect(err).To(MatchError(mkdirFailedErr))
		importerServiceMkdirAll = originalMkdirAll

		originalCreateTmp := importerServiceCreateTmp
		ginkgo.DeferCleanup(func() {
			importerServiceCreateTmp = originalCreateTmp
		})
		createTempFailedErr := errors.New("create temp failed")
		importerServiceCreateTmp = func(dir, pattern string) (*os.File, error) {
			return nil, createTempFailedErr
		}
		_, err = service.extractZipReaderToTemp(bytes.NewReader(nil))
		Expect(err).To(MatchError(createTempFailedErr))
		importerServiceCreateTmp = originalCreateTmp

		originalCopy := importerServiceCopy
		ginkgo.DeferCleanup(func() {
			importerServiceCopy = originalCopy
		})
		copyFailedErr := errors.New("copy failed")
		importerServiceCopy = func(dst io.Writer, src io.Reader) (int64, error) {
			return 0, copyFailedErr
		}
		_, err = service.extractZipReaderToTemp(bytes.NewReader(nil))
		Expect(err).To(MatchError(copyFailedErr))
		importerServiceCopy = originalCopy

		originalRemove := importerServiceRemove
		ginkgo.DeferCleanup(func() {
			importerServiceRemove = originalRemove
		})
		importerServiceRemove = func(name string) error {
			return errors.New("remove failed")
		}
		var zipBytes bytes.Buffer
		zipWriter := zip.NewWriter(&zipBytes)
		file, err := zipWriter.Create("page.md")
		Expect(err).NotTo(HaveOccurred())
		_, err = file.Write([]byte("# Page"))
		Expect(err).NotTo(HaveOccurred())
		Expect(zipWriter.Close()).To(Succeed())
		workspace, err := service.extractZipReaderToTemp(bytes.NewReader(zipBytes.Bytes()))
		Expect(err).NotTo(HaveOccurred())
		ginkgo.DeferCleanup(cleanupZipWorkspaceResult, workspace)
		importerServiceRemove = originalRemove

		originalClose := importerServiceCloseFile
		ginkgo.DeferCleanup(func() {
			importerServiceCloseFile = originalClose
		})
		closeFailedErr := errors.New("close failed")
		importerServiceCloseFile = func(file *os.File) error {
			return closeFailedErr
		}
		_, err = service.extractZipReaderToTemp(bytes.NewReader(zipBytes.Bytes()))
		Expect(err).To(MatchError(closeFailedErr))
	})

	ginkgo.It("resumes startup imports and records finish persistence failures", func() {
		_ = NewImporterService(newPlannerWithFake(&fakeWiki{}), NewPlanStore(importerBadStateFile()), importerTempDir(), 0)

		store := NewPlanStore()
		Expect(store.Set(&StoredPlan{Plan: &PlanResult{ID: "planned"}, ExecutionStatus: ExecutionStatusPlanned})).To(Succeed())
		NewImporterService(newPlannerWithFake(&fakeWiki{}), store, importerTempDir(), 0)

		workspace := importerTempDir()
		importerWriteFile(workspace, "page.md", "# Page")
		stateFile := filepath.Join(importerTempDir(), "current-plan.json")
		store = NewPlanStore(stateFile)
		plan := &PlanResult{
			ID:       "plan-1",
			TreeHash: "h1",
			Items: []PlanItem{
				{SourcePath: "page.md", TargetPath: "page", Title: "Page", Kind: tree.NodeKindPage, Action: PlanActionCreate},
			},
		}
		Expect(store.Set(&StoredPlan{
			Plan:            plan,
			PlanOptions:     PlanOptions{SourceBasePath: workspace},
			WorkspaceRoot:   workspace,
			ExecutionStatus: ExecutionStatusRunning,
		})).To(Succeed())
		wiki := &fakeWiki{treeHash: "h1", lookups: map[string]*tree.PathLookup{}}
		service := NewImporterService(newPlannerWithFake(wiki), NewPlanStore(stateFile), importerTempDir(), 0)
		Eventually(func() (*CurrentPlanState, error) {
			return service.GetCurrentPlan()
		}).Should(And(Not(BeNil()), WithTransform(func(state *CurrentPlanState) ExecutionStatus {
			return state.ExecutionStatus
		}, Equal(ExecutionStatusCompleted))))

		store = NewPlanStore(stateFile + "-finish-error")
		Expect(store.Set(&StoredPlan{
			Plan:            plan,
			PlanOptions:     PlanOptions{SourceBasePath: workspace},
			WorkspaceRoot:   workspace,
			ExecutionStatus: ExecutionStatusRunning,
			ExecutionUserID: "user-1",
		})).To(Succeed())
		store.stateFile = importerBadStateFile()
		service = NewImporterService(newPlannerWithFake(&fakeWiki{treeHash: "h1", lookups: map[string]*tree.PathLookup{}}), store, importerTempDir(), 0)
		Eventually(func() error {
			_, err := service.GetCurrentPlan()
			return err
		}).Should(MatchError(ErrImportStateUnavailable))
	})
})
