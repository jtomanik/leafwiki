package importer_test

import (
	"os"
	"path/filepath"

	"github.com/perber/wiki/internal/core/tree"
	"github.com/perber/wiki/internal/importer"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
)

var _ = ginkgo.Describe("import planning through real wiki storage", ginkgo.Label("integration"), func() {
	ginkgo.It("surfaces missing source folders without storing a plan", func() {
		w := newTestWiki()
		integWrapCloseWithErrorCheck(w.Close)
		is := newTestImporterService(w)

		_, err := is.CreateImportPlanFromFolder(filepath.Join(integTempDir(), "missing"), "")
		Expect(err).To(matchWrappedIntegrationError(os.ErrNotExist))

		_, err = is.GetCurrentPlan()
		Expect(err).To(MatchError(importer.ErrNoPlan))
	})

	ginkgo.It("reports semantic planner errors for invalid source filenames", func() {
		ws := integTempDir()
		integMustWrite(ws, "!!!.md", "# Invalid")

		w := newTestWiki()
		integWrapCloseWithErrorCheck(w.Close)
		is := newTestImporterService(w)

		plan, err := is.CreateImportPlanFromFolder(ws, "")
		Expect(err).To(Succeed())
		Expect(plan).To(SatisfyAll(
			HaveField("Items", BeEmpty()),
			HaveField("Errors", HaveLen(1)),
			HaveField("ErrorDetails", ConsistOf(SatisfyAll(
				HaveField("SourcePath", Equal(integWorkspaceSourcePath("!!!.md"))),
				HaveField("Code", Equal(importer.ImportErrorCodeNormalizeFilenameFailed)),
			))),
		))

		state, err := is.GetCurrentPlan()
		Expect(err).To(Succeed())
		Expect(state).To(SatisfyAll(
			HaveField("ExecutionStatus", Equal(importer.ExecutionStatusPlanned)),
			HaveField("Items", BeEmpty()),
			HaveField("ErrorDetails", HaveLen(1)),
		))
	})

	ginkgo.It("plans existing pages as skipped and preserves them during execution", func() {
		ws := integTempDir()
		integMustWrite(ws, "Existing.md", "# Replacement\nBody")

		w := newTestWiki()
		integWrapCloseWithErrorCheck(w.Close)
		probe := newImporterProbe(w)
		kind := tree.NodeKindPage
		existingPage, err := probe.EnsurePath(
			integFixtureUserID("system"),
			integRoutePath("existing"),
			"Existing",
			&kind,
		)
		Expect(err).To(Succeed())
		is := newTestImporterService(w)

		plan, err := is.CreateImportPlanFromFolder(ws, "")
		Expect(err).To(Succeed())
		Expect(plan.Items).To(ConsistOf(SatisfyAll(
			HaveField("TargetPath", Equal(integRoutePath("existing"))),
			HaveField("ExistingID", gstruct.PointTo(Equal(existingPage.ID))),
			HaveField("Action", Equal(importer.PlanActionSkip)),
		)))

		result, err := is.ExecuteCurrentPlan(integFixtureUserID("system"))
		Expect(err).To(Succeed())
		Expect(result).To(SatisfyAll(
			HaveField("ImportedCount", BeZero()),
			HaveField("SkippedCount", Equal(1)),
			HaveField("Items", ConsistOf(SatisfyAll(
				HaveField("TargetPath", Equal(integRoutePath("existing"))),
				HaveField("Action", Equal(importer.ExecutionActionSkipped)),
			))),
		))
	})
})

var _ = ginkgo.Describe("import execution asset limits through real wiki storage", ginkgo.Label("integration"), func() {
	ginkgo.It("keeps the positive asset-size override when importing linked assets", func() {
		ws := integTempDir()
		integMustWrite(ws, "AssetLimit.md", "# Asset Limit\n\n![Large](large.png)")
		integMustWrite(ws, "large.png", "large-asset-bytes")

		w := newTestWiki()
		integWrapCloseWithErrorCheck(w.Close)
		is := newTestImporterService(w)
		is.SetAssetMaxUploadSizeBytes(1)
		is.SetAssetMaxUploadSizeBytes(0)

		plan, err := is.CreateImportPlanFromFolder(ws, "")
		Expect(err).To(Succeed())
		Expect(plan.Items).To(HaveLen(1))

		result, err := is.ExecuteCurrentPlan(integFixtureUserID("system"))
		Expect(err).To(Succeed())
		Expect(result).To(SatisfyAll(
			HaveField("ImportedCount", BeZero()),
			HaveField("SkippedCount", Equal(1)),
			HaveField("Items", ConsistOf(matchExecutionItemError(
				integWorkspaceSourcePath("AssetLimit.md"),
				plan.Items[0].TargetPath,
				importer.ImportErrorCodeTransformContentFailed,
			))),
		))
	})
})
