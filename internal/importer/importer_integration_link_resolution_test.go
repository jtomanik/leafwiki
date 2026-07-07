package importer_test

import (
	"strings"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/perber/wiki/internal/importer"
)

var _ = ginkgo.Describe("import execution resolves section and suffix links", ginkgo.Label("integration"), func() {
	ginkgo.It("rewrites unique section and fallback links while preserving ambiguous targets", func() {
		ws := integTempDir()
		integMustWrite(ws, "Docs/Current.md", strings.Join([]string{
			"# Current",
			"",
			"[Folder Index](Folder/)",
			"[Folder Readme](Folder/README.md)",
			"[Folder Child](Folder/Child.md)",
			"[[Folder/Child.md|Child Wiki]]",
			"[[Folder/]]",
			"[[../Outside.md]]",
			"[[/]]",
			"[[Topic.md]]",
			"[Invalid Escape](Target%ZZ.md)",
			"[Missing Markdown](Missing.md)",
		}, "\n"))
		integMustWrite(ws, "Docs/Folder/README.md", "# Folder Readme")
		integMustWrite(ws, "Docs/Folder/Child.md", "# Folder Child")
		integMustWrite(ws, "Alpha/Topic.md", "# Alpha Topic")
		integMustWrite(ws, "Beta/Topic.md", "# Beta Topic")

		w := newTestWiki()
		integWrapCloseWithErrorCheck(w.Close)
		is := newTestImporterServiceWithOptions(w, importer.ImporterServiceOptions{
			MarkdownLinkRootPrefix: "/wiki",
		})
		probe := newImporterProbe(w)

		plan, err := is.CreateImportPlanFromFolder(ws, "")
		Expect(err).To(Succeed())
		Expect(plan.Errors).To(BeEmpty())

		result, err := is.ExecuteCurrentPlan(integFixtureUserID("system"))
		Expect(err).To(Succeed())
		Expect(result).To(SatisfyAll(
			HaveField("ImportedCount", Equal(5)),
			HaveField("SkippedCount", BeZero()),
		))

		currentPage, err := probe.FindByPath("docs/current")
		Expect(err).To(Succeed())
		for _, expected := range []string{
			"[Folder Index](/wiki/docs/folder)",
			"[Folder Readme](/wiki/docs/folder)",
			"[Folder Child](/wiki/docs/folder/child.md)",
			"[Child Wiki](/wiki/docs/folder/child.md)",
			"[Folder](/wiki/docs/folder)",
			"[Outside](/outside)",
			"[[/]]",
			"[[Topic.md]]",
			"[Invalid Escape](Target%ZZ.md)",
			"[Missing Markdown](Missing.md)",
		} {
			Expect(currentPage.Content).To(ContainSubstring(expected))
		}
	})
})
