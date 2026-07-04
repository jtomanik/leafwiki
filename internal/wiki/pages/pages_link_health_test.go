package pages_test

import (
	"context"
	"log/slog"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/perber/wiki/internal/core/tree"
	"github.com/perber/wiki/internal/wiki/pages"
)

// Canonical Markdown links plan scenarios covered by tests in this file:
// - Refactor preview reports conflicts without mutating content

// testDeps holds real services backed by a temporary directory.

var _ = ginkgo.Describe("page use case behavior", ginkgo.Label("integration"), func() {
	ginkgo.It("removes deleted subtree outgoings and breaks incoming prefix links", func() {
		deps := newTestDeps()
		createUC := pages.NewCreatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())
		updateUC := pages.NewUpdatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())
		deleteUC := pages.NewDeletePageUseCase(deps.tree, deps.assets, deps.orchestrator(), slog.Default())

		docs, _ := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: "system", Title: "Docs", Slug: "docs", Kind: pageKind(),
		})
		a, _ := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: "system", ParentID: pageIDPtr(docs.Page.ID), Title: "A", Slug: "a", Kind: pageKind(),
		})
		b, _ := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: "system", ParentID: pageIDPtr(docs.Page.ID), Title: "B", Slug: "b", Kind: pageKind(),
		})

		contentA := "Link to B: [B](/docs/b)"
		if _, err := updateUC.Execute(context.Background(), pages.UpdatePageInput{
			UserID: "system", ID: pageID(a.Page.ID), Version: pageVersion(a.Page.Version()), Title: a.Page.Title, Slug: slug(a.Page.Slug), Content: &contentA, Kind: pageKind(),
		}); err != nil {
			Expect(err).To(Succeed())
		}
		contentB := "# B"
		if _, err := updateUC.Execute(context.Background(), pages.UpdatePageInput{
			UserID: "system", ID: pageID(b.Page.ID), Version: pageVersion(b.Page.Version()), Title: b.Page.Title, Slug: slug(b.Page.Slug), Content: &contentB, Kind: pageKind(),
		}); err != nil {
			Expect(err).To(Succeed())
		}

		c, _ := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: "system", Title: "C", Slug: "c", Kind: pageKind(),
		})
		contentC := "Incoming link: [B](/docs/b)"
		if _, err := updateUC.Execute(context.Background(), pages.UpdatePageInput{
			UserID: "system", ID: pageID(c.Page.ID), Version: pageVersion(c.Page.Version()), Title: c.Page.Title, Slug: slug(c.Page.Slug), Content: &contentC, Kind: pageKind(),
		}); err != nil {
			Expect(err).To(Succeed())
		}

		Expect(deps.links.IndexAllPages()).To(Succeed())
		outA, err := deps.links.GetOutgoingLinksForPage(a.Page.ID)
		Expect(err).To(Succeed())
		Expect(outA.Count).To(Equal(1))

		if err := deleteUC.Execute(context.Background(), pages.DeletePageInput{
			UserID: "system", ID: pageID(docs.Page.ID), Version: pageVersion(docs.Page.Version()), Recursive: true,
		}); err != nil {
			Expect(err).To(Succeed())
		}

		outAAfter, err := deps.links.GetOutgoingLinksForPage(a.Page.ID)
		Expect(err).To(Succeed())
		Expect(outAAfter.Count).To(BeZero())

		outC, err := deps.links.GetOutgoingLinksForPage(c.Page.ID)
		Expect(err).To(Succeed())
		Expect(outC.Count).To(Equal(1))
		got := outC.Outgoings[0]
		Expect(got).To(HaveBrokenOutgoing("/docs/b"))
	})

	ginkgo.It("breaks old page links and heals new exact links when renaming", func() {
		deps := newTestDeps()
		createUC := pages.NewCreatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())
		updateUC := pages.NewUpdatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())

		a, _ := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: "system", Title: "A", Slug: "a", Kind: pageKind(),
		})
		contentA := "Links: [B](/b) and [B2](/b2.md)"
		if _, err := updateUC.Execute(context.Background(), pages.UpdatePageInput{
			UserID: "system", ID: pageID(a.Page.ID), Version: pageVersion(a.Page.Version()), Title: a.Page.Title, Slug: slug(a.Page.Slug), Content: &contentA, Kind: pageKind(),
		}); err != nil {
			Expect(err).To(Succeed())
		}

		b, _ := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: "system", Title: "B", Slug: "b", Kind: pageKind(),
		})
		contentB := "# B"
		if _, err := updateUC.Execute(context.Background(), pages.UpdatePageInput{
			UserID: "system", ID: pageID(b.Page.ID), Version: pageVersion(b.Page.Version()), Title: b.Page.Title, Slug: slug(b.Page.Slug), Content: &contentB, Kind: pageKind(),
		}); err != nil {
			Expect(err).To(Succeed())
		}

		Expect(deps.links.IndexAllPages()).To(Succeed())
		out1, err := deps.links.GetOutgoingLinksForPage(a.Page.ID)
		Expect(err).To(Succeed())
		Expect(out1.Count).To(Equal(2))

		contentB2 := "# B (renamed)"
		if _, err := updateUC.Execute(context.Background(), pages.UpdatePageInput{
			UserID: "system", ID: pageID(b.Page.ID), Version: pageVersion(b.Page.Version()), Title: b.Page.Title, Slug: "b2", Content: &contentB2, Kind: pageKind(),
		}); err != nil {
			Expect(err).To(Succeed())
		}

		out2, err := deps.links.GetOutgoingLinksForPage(a.Page.ID)
		Expect(err).To(Succeed())
		Expect(out2.Outgoings).To(ConsistOf(
			HaveBrokenOutgoing("/b"),
			HaveHealthyOutgoing("/b2", b.Page.ID),
		))
	})

	ginkgo.It("breaks old subtree links and heals new subpaths when renaming", func() {
		deps := newTestDeps()
		createUC := pages.NewCreatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())
		updateUC := pages.NewUpdatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())

		docs, _ := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: "system", Title: "Docs", Slug: "docs", Kind: pageKind(),
		})
		b, _ := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: "system", ParentID: pageIDPtr(docs.Page.ID), Title: "B", Slug: "b", Kind: pageKind(),
		})
		contentB := "# B"
		if _, err := updateUC.Execute(context.Background(), pages.UpdatePageInput{
			UserID: "system", ID: pageID(b.Page.ID), Version: pageVersion(b.Page.Version()), Title: b.Page.Title, Slug: slug(b.Page.Slug), Content: &contentB, Kind: pageKind(),
		}); err != nil {
			Expect(err).To(Succeed())
		}

		a, _ := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: "system", Title: "A", Slug: "a", Kind: pageKind(),
		})
		contentA := "Links: [Old](/docs/b) and [New](/docs2/b.md)"
		if _, err := updateUC.Execute(context.Background(), pages.UpdatePageInput{
			UserID: "system", ID: pageID(a.Page.ID), Version: pageVersion(a.Page.Version()), Title: a.Page.Title, Slug: slug(a.Page.Slug), Content: &contentA, Kind: pageKind(),
		}); err != nil {
			Expect(err).To(Succeed())
		}

		Expect(deps.links.IndexAllPages()).To(Succeed())

		contentDocs2 := "# Docs"
		nodeSection := tree.NodeKindSection
		if _, err := updateUC.Execute(context.Background(), pages.UpdatePageInput{
			UserID: "system", ID: pageID(docs.Page.ID), Version: pageVersion(docs.Page.Version()), Title: docs.Page.Title, Slug: "docs2", Content: &contentDocs2, Kind: &nodeSection,
		}); err != nil {
			Expect(err).To(Succeed())
		}

		out2, err := deps.links.GetOutgoingLinksForPage(a.Page.ID)
		Expect(err).To(Succeed())
		Expect(out2.Outgoings).To(ConsistOf(
			HaveBrokenOutgoing("/docs/b"),
			HaveHealthyOutgoing("/docs2/b", b.Page.ID),
		))
	})

	ginkgo.It("breaks old links and heals the new exact path when moving", func() {
		deps := newTestDeps()
		createUC := pages.NewCreatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())
		updateUC := pages.NewUpdatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())
		moveUC := pages.NewMovePageUseCase(deps.tree, deps.orchestrator(), slog.Default())

		a, _ := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: "system", Title: "A", Slug: "a", Kind: pageKind(),
		})
		contentA := "Links: [B](/b) and [B2](/projects/b.md)"
		if _, err := updateUC.Execute(context.Background(), pages.UpdatePageInput{
			UserID: "system", ID: pageID(a.Page.ID), Version: pageVersion(a.Page.Version()), Title: a.Page.Title, Slug: slug(a.Page.Slug), Content: &contentA, Kind: pageKind(),
		}); err != nil {
			Expect(err).To(Succeed())
		}

		b, _ := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: "system", Title: "B", Slug: "b", Kind: pageKind(),
		})
		contentB := "# B"
		if _, err := updateUC.Execute(context.Background(), pages.UpdatePageInput{
			UserID: "system", ID: pageID(b.Page.ID), Version: pageVersion(b.Page.Version()), Title: b.Page.Title, Slug: slug(b.Page.Slug), Content: &contentB, Kind: pageKind(),
		}); err != nil {
			Expect(err).To(Succeed())
		}

		projects, _ := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: "system", Title: "Projects", Slug: "projects", Kind: pageKind(),
		})
		Expect(deps.links.IndexAllPages()).To(Succeed())

		if err := moveUC.Execute(context.Background(), pages.MovePageInput{
			UserID: "system", ID: pageID(b.Page.ID), Version: pageVersion(b.Page.Version()), ParentID: pageID(projects.Page.ID),
		}); err != nil {
			Expect(err).To(Succeed())
		}

		out2, err := deps.links.GetOutgoingLinksForPage(a.Page.ID)
		Expect(err).To(Succeed())
		Expect(out2.Outgoings).To(ConsistOf(
			HaveBrokenOutgoing("/b"),
			HaveHealthyOutgoing("/projects/b", b.Page.ID),
		))
	})

	ginkgo.It("breaks old subtree links and heals new subpaths when moving", func() {
		deps := newTestDeps()
		createUC := pages.NewCreatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())
		updateUC := pages.NewUpdatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())
		moveUC := pages.NewMovePageUseCase(deps.tree, deps.orchestrator(), slog.Default())

		docs, _ := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: "system", Title: "Docs", Slug: "docs", Kind: pageKind(),
		})
		b, _ := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: "system", ParentID: pageIDPtr(docs.Page.ID), Title: "B", Slug: "b", Kind: pageKind(),
		})
		contentB := "# B"
		if _, err := updateUC.Execute(context.Background(), pages.UpdatePageInput{
			UserID: "system", ID: pageID(b.Page.ID), Version: pageVersion(b.Page.Version()), Title: b.Page.Title, Slug: slug(b.Page.Slug), Content: &contentB, Kind: pageKind(),
		}); err != nil {
			Expect(err).To(Succeed())
		}

		archive, _ := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: "system", Title: "Archive", Slug: "archive", Kind: pageKind(),
		})
		a, _ := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: "system", Title: "A", Slug: "a", Kind: pageKind(),
		})
		contentA := "Links: [Old](/docs/b) and [New](/archive/docs/b.md)"
		if _, err := updateUC.Execute(context.Background(), pages.UpdatePageInput{
			UserID: "system", ID: pageID(a.Page.ID), Version: pageVersion(a.Page.Version()), Title: a.Page.Title, Slug: slug(a.Page.Slug), Content: &contentA, Kind: pageKind(),
		}); err != nil {
			Expect(err).To(Succeed())
		}
		Expect(deps.links.IndexAllPages()).To(Succeed())

		if err := moveUC.Execute(context.Background(), pages.MovePageInput{
			UserID: "system", ID: pageID(docs.Page.ID), Version: pageVersion(docs.Page.Version()), ParentID: pageID(archive.Page.ID),
		}); err != nil {
			Expect(err).To(Succeed())
		}

		out, err := deps.links.GetOutgoingLinksForPage(a.Page.ID)
		Expect(err).To(Succeed())
		Expect(out.Outgoings).To(ConsistOf(
			HaveBrokenOutgoing("/docs/b"),
			HaveHealthyOutgoing("/archive/docs/b", b.Page.ID),
		))
	})

	ginkgo.It("reindexes relative links when a page moves", func() {
		deps := newTestDeps()
		createUC := pages.NewCreatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())
		updateUC := pages.NewUpdatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())
		moveUC := pages.NewMovePageUseCase(deps.tree, deps.orchestrator(), slog.Default())

		docs, _ := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: "system", Title: "Docs", Slug: "docs", Kind: pageKind(),
		})
		docsShared, _ := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: "system", ParentID: pageIDPtr(docs.Page.ID), Title: "Shared", Slug: "shared", Kind: pageKind(),
		})
		contentDocsShared := "# Docs Shared"
		if _, err := updateUC.Execute(context.Background(), pages.UpdatePageInput{
			UserID: "system", ID: pageID(docsShared.Page.ID), Version: pageVersion(docsShared.Page.Version()), Title: docsShared.Page.Title, Slug: slug(docsShared.Page.Slug), Content: &contentDocsShared, Kind: pageKind(),
		}); err != nil {
			Expect(err).To(Succeed())
		}

		a, _ := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: "system", ParentID: pageIDPtr(docs.Page.ID), Title: "A", Slug: "a", Kind: pageKind(),
		})
		contentA := "Relative: [S](./shared.md)"
		if _, err := updateUC.Execute(context.Background(), pages.UpdatePageInput{
			UserID: "system", ID: pageID(a.Page.ID), Version: pageVersion(a.Page.Version()), Title: a.Page.Title, Slug: slug(a.Page.Slug), Content: &contentA, Kind: pageKind(),
		}); err != nil {
			Expect(err).To(Succeed())
		}

		guide, _ := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: "system", Title: "Guide", Slug: "guide", Kind: pageKind(),
		})
		guideShared, _ := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: "system", ParentID: pageIDPtr(guide.Page.ID), Title: "Shared", Slug: "shared", Kind: pageKind(),
		})
		contentGuideShared := "# Guide Shared"
		if _, err := updateUC.Execute(context.Background(), pages.UpdatePageInput{
			UserID: "system", ID: pageID(guideShared.Page.ID), Version: pageVersion(guideShared.Page.Version()), Title: guideShared.Page.Title, Slug: slug(guideShared.Page.Slug), Content: &contentGuideShared, Kind: pageKind(),
		}); err != nil {
			Expect(err).To(Succeed())
		}

		Expect(deps.links.IndexAllPages()).To(Succeed())

		out1, err := deps.links.GetOutgoingLinksForPage(a.Page.ID)
		Expect(err).To(Succeed())
		Expect(out1.Outgoings).To(HaveExactElements(HaveHealthyOutgoing("/docs/shared", docsShared.Page.ID)))

		if err := moveUC.Execute(context.Background(), pages.MovePageInput{
			UserID: "system", ID: pageID(a.Page.ID), Version: pageVersion(a.Page.Version()), ParentID: pageID(guide.Page.ID),
		}); err != nil {
			Expect(err).To(Succeed())
		}

		out2, err := deps.links.GetOutgoingLinksForPage(a.Page.ID)
		Expect(err).To(Succeed())
		Expect(out2.Outgoings).To(HaveExactElements(HaveHealthyOutgoing("/guide/shared", guideShared.Page.ID)))
	})
})
