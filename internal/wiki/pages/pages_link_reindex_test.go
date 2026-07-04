package pages_test

import (
	"context"
	"log/slog"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/perber/wiki/internal/wiki/pages"
)

// Canonical Markdown links plan scenarios covered by tests in this file:
// - Refactor preview reports conflicts without mutating content

// testDeps holds real services backed by a temporary directory.

var _ = ginkgo.Describe("page use case behavior", ginkgo.Label("integration"), func() {
	ginkgo.It("excludes moved subtree links from optional preview effects", func() {
		deps := newTestDeps()
		createUC := pages.NewCreatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())
		updateUC := pages.NewUpdatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())
		previewUC := pages.NewPreviewPageRefactorUseCase(deps.tree, deps.slug, deps.links, slog.Default())

		docs, _ := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: "system", Title: "Docs", Slug: "docs", Kind: pageKind(),
		})
		pageA, _ := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: "system", ParentID: pageIDPtr(docs.Page.ID), Title: "Page A", Slug: "page-a", Kind: pageKind(),
		})
		pageB, _ := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: "system", ParentID: pageIDPtr(docs.Page.ID), Title: "Page B", Slug: "page-b", Kind: pageKind(),
		})
		archive, _ := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: "system", Title: "Archive", Slug: "archive", Kind: pageKind(),
		})

		contentA := "[To B](./page-b.md)"
		if _, err := updateUC.Execute(context.Background(), pages.UpdatePageInput{
			UserID: "system", ID: pageID(pageA.Page.ID), Version: pageVersion(pageA.Page.Version()), Title: pageA.Page.Title, Slug: slug(pageA.Page.Slug), Content: &contentA, Kind: pageKind(),
		}); err != nil {
			Expect(err).To(Succeed())
		}

		preview, err := previewUC.Execute(context.Background(), pages.RefactorPreviewInput{
			PageID:      pageID(pageA.Page.ID),
			Kind:        pages.RefactorKindMove,
			NewParentID: pageIDPtr(archive.Page.ID),
		})
		Expect(err).To(Succeed())
		Expect(preview.Counts.AffectedPages).To(BeZero())
		Expect(preview.AffectedPages).To(BeEmpty())

		_ = pageB
	})

	ginkgo.It("rewrites moved page relative links", func() {
		deps := newTestDeps()
		createUC := pages.NewCreatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())
		updateUC := pages.NewUpdatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())
		applyUC := pages.NewApplyPageRefactorUseCase(deps.tree, deps.slug, deps.links, slog.Default())

		docs, _ := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: "system", Title: "Docs", Slug: "docs", Kind: pageKind(),
		})
		pageA, _ := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: "system", ParentID: pageIDPtr(docs.Page.ID), Title: "Page A", Slug: "page-a", Kind: pageKind(),
		})
		pageB, _ := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: "system", ParentID: pageIDPtr(docs.Page.ID), Title: "Page B", Slug: "page-b", Kind: pageKind(),
		})
		archive, _ := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: "system", Title: "Archive", Slug: "archive", Kind: pageKind(),
		})

		contentA := "[To B](./page-b.md)"
		if _, err := updateUC.Execute(context.Background(), pages.UpdatePageInput{
			UserID: "system", ID: pageID(pageA.Page.ID), Version: pageVersion(pageA.Page.Version()), Title: pageA.Page.Title, Slug: slug(pageA.Page.Slug), Content: &contentA, Kind: pageKind(),
		}); err != nil {
			Expect(err).To(Succeed())
		}

		updated, err := applyUC.Execute(context.Background(), pages.RefactorApplyInput{
			UserID:  "system",
			Version: pageVersion(pageA.Page.Version()),
			RefactorPreviewInput: pages.RefactorPreviewInput{
				PageID:      pageID(pageA.Page.ID),
				Kind:        pages.RefactorKindMove,
				NewParentID: pageIDPtr(archive.Page.ID),
			},
			RewriteLinks: false,
		})
		Expect(err).To(Succeed())
		Expect(updated.CalculatePath()).To(Equal("/archive/page-a"))

		movedPage, err := deps.tree.GetPage(pageID(pageA.Page.ID))
		Expect(err).To(Succeed())
		Expect(movedPage.Content).To(Equal("[To B](../docs/page-b.md)"))

		outgoing, err := deps.links.GetOutgoingLinksForPage(pageA.Page.ID)
		Expect(err).To(Succeed())
		Expect(outgoing).To(SatisfyAll(
			HaveField("Count", Equal(1)),
			HaveField("Outgoings", HaveExactElements(HaveHealthyOutgoing("/docs/page-b", pageB.Page.ID))),
		))

	})

	ginkgo.It("heals links for every created path segment", func() {
		deps := newTestDeps()
		createUC := pages.NewCreatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())
		updateUC := pages.NewUpdatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())
		ensureUC := pages.NewEnsurePathUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())

		pageA, err := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: "system", Title: "Page A", Slug: "a", Kind: pageKind(),
		})
		Expect(err).To(Succeed())

		contentA := "Links: [X](/x) and [XY](/x/y.md)"
		if _, err := updateUC.Execute(context.Background(), pages.UpdatePageInput{
			UserID: "system", ID: pageID(pageA.Page.ID), Version: pageVersion(pageA.Page.Version()), Title: pageA.Page.Title, Slug: slug(pageA.Page.Slug), Content: &contentA, Kind: pageKind(),
		}); err != nil {
			Expect(err).To(Succeed())
		}

		Expect(deps.links.IndexAllPages()).To(Succeed())

		out1, err := deps.links.GetOutgoingLinksForPage(pageA.Page.ID)
		Expect(err).To(Succeed())
		Expect(out1).To(SatisfyAll(
			HaveField("Count", Equal(2)),
			HaveField("Outgoings", ConsistOf(
				HaveBrokenOutgoing("/x"),
				HaveBrokenOutgoing("/x/y"),
			)),
		))

		if _, err := ensureUC.Execute(context.Background(), pages.EnsurePathInput{
			UserID: "system", TargetPath: "/x/y", TargetTitle: "X Y", Kind: pageKind(),
		}); err != nil {
			Expect(err).To(Succeed())
		}

		out2, err := deps.links.GetOutgoingLinksForPage(pageA.Page.ID)
		Expect(err).To(Succeed())
		Expect(out2).To(SatisfyAll(
			HaveField("Count", Equal(2)),
			HaveField("Outgoings", ConsistOf(
				HaveHealthyOutgoingWithAnyTarget("/x"),
				HaveHealthyOutgoingWithAnyTarget("/x/y"),
			)),
		))
	})

	ginkgo.It("marks incoming links broken for non-recursive deletion", func() {
		deps := newTestDeps()
		createUC := pages.NewCreatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())
		updateUC := pages.NewUpdatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())
		deleteUC := pages.NewDeletePageUseCase(deps.tree, deps.assets, deps.orchestrator(), slog.Default())

		a, err := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: "system", Title: "Page A", Slug: "a", Kind: pageKind(),
		})
		Expect(err).To(Succeed())
		contentA := "Link to B: [Go](/b)"
		if _, err := updateUC.Execute(context.Background(), pages.UpdatePageInput{
			UserID: "system", ID: pageID(a.Page.ID), Version: pageVersion(a.Page.Version()), Title: a.Page.Title, Slug: slug(a.Page.Slug), Content: &contentA, Kind: pageKind(),
		}); err != nil {
			Expect(err).To(Succeed())
		}

		b, err := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: "system", Title: "Page B", Slug: "b", Kind: pageKind(),
		})
		Expect(err).To(Succeed())
		contentB := "# Page B"
		if _, err := updateUC.Execute(context.Background(), pages.UpdatePageInput{
			UserID: "system", ID: pageID(b.Page.ID), Version: pageVersion(b.Page.Version()), Title: b.Page.Title, Slug: slug(b.Page.Slug), Content: &contentB, Kind: pageKind(),
		}); err != nil {
			Expect(err).To(Succeed())
		}

		Expect(deps.links.IndexAllPages()).To(Succeed())
		if err := deleteUC.Execute(context.Background(), pages.DeletePageInput{
			UserID: "system", ID: pageID(b.Page.ID), Version: pageVersion(b.Page.Version()), Recursive: false,
		}); err != nil {
			Expect(err).To(Succeed())
		}

		out, err := deps.links.GetOutgoingLinksForPage(a.Page.ID)
		Expect(err).To(Succeed())
		Expect(out.Count).To(Equal(1))
		got := out.Outgoings[0]
		Expect(got).To(HaveBrokenOutgoing("/b"))

		bl, err := deps.links.GetBacklinksForPage(b.Page.ID)
		Expect(err).To(Succeed())
		Expect(bl.Count).To(BeZero())
	})

})
