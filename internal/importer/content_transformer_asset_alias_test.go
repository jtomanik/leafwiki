package importer

import (
	"github.com/perber/wiki/internal/core/shared"
	"github.com/perber/wiki/internal/core/tree"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("import content asset wiki labels", ginkgo.Label("unit"), func() {
	ginkgo.It("uses explicit wiki labels when rendering linked and embedded assets", func() {
		tmp := importerTempDir()
		writeTmp(tmp, "docs/current.md", "# Current")
		writeTmp(tmp, "assets/manual.pdf", "pdf-bytes")

		transformer := newContentTransformer(&PlanResult{
			Items: []PlanItem{
				{
					SourcePath: newFixtureWorkspaceSourcePath("docs/current.md"),
					TargetPath: newFixtureRoutePath("docs/current"),
					Kind:       tree.NodeKindPage,
				},
			},
		}, tmp, 2048)
		page := &tree.Page{
			PageNode: &tree.PageNode{
				ID:   newFixturePageID("page-asset"),
				Kind: tree.NodeKindPage,
			},
		}
		wiki := &fakeExecWiki{}
		content := "[[../assets/manual.pdf|Manual link]]\n![[../assets/manual.pdf|Manual embed]]"

		got, err := transformer.TransformContent(
			newFixtureUserID("editor"),
			newFixtureWorkspaceSourcePath("docs/current.md"),
			page,
			content,
			wiki,
		)

		Expect(err).To(Succeed())
		Expect(got).To(Equal("[Manual link](/assets/page-asset/manual.pdf)\n![Manual embed](/assets/page-asset/manual.pdf)"))
		Expect(wiki).To(MatchFakeExecWikiState(SatisfyAll(
			HaveField("UploadCalls", Equal(1)),
			HaveField("LastUploadByteCap", Equal(shared.MaxBytes(2048))),
		)))
	})
})
