package importer_test

import (
	"path/filepath"
	"strings"

	"github.com/perber/wiki/internal/core/tree"
	"github.com/perber/wiki/internal/importer"
	"github.com/perber/wiki/internal/wiki"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func newTestImporterServiceWithOptions(
	w *wiki.Wiki,
	opts importer.ImporterServiceOptions,
) *importer.ImporterService {
	ginkgo.GinkgoHelper()

	planner := importer.NewPlanner(wiki.NewWikiImportAdapter(w), tree.NewSlugService())
	importerDir := filepath.Join(w.GetStorageDir(), ".importer")
	return importer.NewImporterServiceWithOptions(
		planner,
		importer.NewPlanStore(filepath.Join(importerDir, "current-plan.json")),
		filepath.Join(importerDir, "workspaces"),
		0,
		opts,
	)
}

var _ = ginkgo.Describe("import execution rewrites advanced Markdown link forms", ginkgo.Label("integration"), func() {
	ginkgo.It("rewrites reference definitions, suffixed destinations, and local assets outside code blocks", func() {
		ws := integTempDir()
		integMustWrite(ws, "Docs/Target File.md", "# Target File\nBody")
		integMustWrite(ws, "Docs/Section/README.md", "# Section Readme\nBody")
		integMustWrite(ws, "Docs/images/logo.png", "png-bytes")
		integMustWrite(ws, "Docs/manual.pdf", "pdf-bytes")
		integMustWrite(ws, "Docs/Current.md", strings.Join([]string{
			"# Current",
			"",
			"[Angle](<Target%20File.md#intro> \"title\")",
			"[Query](Target%20File.md?download=1#frag)",
			"[Section](/Docs/Section/)",
			"![Logo](images/logo.png?size=small#view)",
			"[Manual](manual.pdf)",
			"[Doc Reference][Doc Ref]",
			"![Logo Reference][Logo Ref]",
			"[Shared Reference][Shared Ref]",
			"![Shared Reference Image][Shared Ref]",
			"[Escaped Punctuation](Target\\%20File.md)",
			"[Drive](C:\\Temp\\File.md)",
			"[External](https://example.test/file.md)",
			"[Protocol Relative](//example.test/file.md)",
			"[Mail](mailto:docs@example.test)",
			"[Phone](tel:+12025550142)",
			"[Fragment](#local)",
			"`[Code](Target File.md)`",
			"",
			"```markdown",
			"[Fenced](Target File.md)",
			"```",
			"",
			"[Doc Ref]: <Target%20File.md#ref> \"ref title\"",
			"[Logo Ref]: images/logo.png?version=1",
			"[Shared Ref]: images/logo.png#shared",
			"[Nested [Ref]]: Target%20File.md",
			"    [Indented Ref]: Target%20File.md",
			"\\[Escaped Ref]: Target File.md",
		}, "\n"))

		w := newTestWiki()
		integWrapCloseWithErrorCheck(w.Close)
		is := newTestImporterServiceWithOptions(w, importer.ImporterServiceOptions{
			MarkdownLinkRootPrefix: "/wiki",
		})
		probe := newImporterProbe(w)

		plan, err := is.CreateImportPlanFromFolder(ws, "")
		Expect(err).To(Succeed())
		Expect(plan).To(SatisfyAll(
			HaveField("Items", HaveLen(3)),
			HaveField("Errors", BeEmpty()),
		))

		result, err := is.ExecuteCurrentPlan(integFixtureUserID("system"))
		Expect(err).To(Succeed())
		Expect(result).To(SatisfyAll(
			HaveField("ImportedCount", Equal(3)),
			HaveField("SkippedCount", BeZero()),
		))

		currentPage, err := probe.FindByPath("docs/current")
		Expect(err).To(Succeed())
		logoPath := expectedAssetPath(currentPage.ID, integFixtureAssetName("logo.png"))
		manualPath := expectedAssetPath(currentPage.ID, integFixtureAssetName("manual.pdf"))

		for _, expected := range []string{
			"[Angle](</wiki/docs/target-file.md#intro> \"title\")",
			"[Query](/wiki/docs/target-file.md?download=1#frag)",
			"[Section](/wiki/docs/section)",
			"![Logo](" + logoPath + "?size=small#view)",
			"[Manual](" + manualPath + ")",
			"[Doc Ref]: </wiki/docs/target-file.md#ref> \"ref title\"",
			"[Logo Ref]: " + logoPath + "?version=1",
			"[Shared Ref]: " + logoPath + "#shared",
			"[Nested [Ref]]: /wiki/docs/target-file.md",
			"[Escaped Punctuation](/wiki/docs/target-file.md)",
			"[Drive](C:\\Temp\\File.md)",
			"[External](https://example.test/file.md)",
			"[Protocol Relative](//example.test/file.md)",
			"[Mail](mailto:docs@example.test)",
			"[Phone](tel:+12025550142)",
			"[Fragment](#local)",
			"`[Code](Target File.md)`",
			"[Fenced](Target File.md)",
			"    [Indented Ref]: Target%20File.md",
			"\\[Escaped Ref]: Target File.md",
		} {
			Expect(currentPage.Content).To(ContainSubstring(expected))
		}

		assets, err := probe.ListAssets(currentPage.ID)
		Expect(err).To(Succeed())
		Expect(assets).To(HaveLen(2))
	})
})

var _ = ginkgo.Describe("import execution rewrites Obsidian fallback links", ginkgo.Label("integration"), func() {
	ginkgo.It("normalizes unresolved wiki links while preserving case-variant package matches", func() {
		ws := integTempDir()
		integMustWrite(ws, "Notes/casetarget.md", "# Case Target")
		integMustWrite(ws, "Notes/Case/Target.md", "# Case Suffix")
		integMustWrite(ws, "Notes/images/photo.svg", "<svg></svg>")
		integMustWrite(ws, "Notes/report.pdf", "pdf-bytes")
		integMustWrite(ws, "Notes/Current.md", strings.Join([]string{
			"# Current",
			"",
			"[[Missing Page]]",
			"[[./Future Folder/index.md]]",
			"[[CaseTarget]]",
			"[[case/target.md]]",
			"![[images/photo.svg]]",
			"[[images/photo.svg]]",
			"[[report.pdf|Report]]",
			"[[Unknown Asset.pdf|Unknown Asset]]",
		}, "\n"))

		w := newTestWiki()
		integWrapCloseWithErrorCheck(w.Close)
		is := newTestImporterService(w)
		probe := newImporterProbe(w)

		plan, err := is.CreateImportPlanFromFolder(ws, "")
		Expect(err).To(Succeed())
		Expect(plan).To(SatisfyAll(
			HaveField("Items", HaveLen(3)),
			HaveField("Errors", BeEmpty()),
		))

		result, err := is.ExecuteCurrentPlan(integFixtureUserID("system"))
		Expect(err).To(Succeed())
		Expect(result).To(SatisfyAll(
			HaveField("ImportedCount", Equal(3)),
			HaveField("SkippedCount", BeZero()),
		))

		currentPage, err := probe.FindByPath("notes/current")
		Expect(err).To(Succeed())
		photoPath := expectedAssetPath(currentPage.ID, integFixtureAssetName("photo.svg"))
		reportPath := expectedAssetPath(currentPage.ID, integFixtureAssetName("report.pdf"))

		for _, expected := range []string{
			"[Missing Page](/missing-page)",
			"[index](/notes/future-folder/index-1)",
			"[[CaseTarget]]",
			"[[case/target.md]]",
			"![photo.svg](" + photoPath + ")",
			"[Report](" + reportPath + ")",
			"[[Unknown Asset.pdf|Unknown Asset]]",
		} {
			Expect(currentPage.Content).To(ContainSubstring(expected))
		}

		Expect(strings.Count(currentPage.Content, photoPath)).To(Equal(2))
		assets, err := probe.ListAssets(currentPage.ID)
		Expect(err).To(Succeed())
		Expect(assets).To(HaveLen(2))
	})
})
