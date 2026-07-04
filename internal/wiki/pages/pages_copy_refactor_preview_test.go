package pages_test

import (
	"context"
	"log/slog"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"

	"github.com/perber/wiki/internal/core/tree"
	"github.com/perber/wiki/internal/test_utils"
	"github.com/perber/wiki/internal/wiki/pages"
	"github.com/perber/wiki/internal/wiki/pagesave"
)

// Canonical Markdown links plan scenarios covered by tests in this file:
// - Refactor preview reports conflicts without mutating content

// testDeps holds real services backed by a temporary directory.

var _ = ginkgo.Describe("page use case behavior", ginkgo.Label("integration"), func() {
	ginkgo.It("copies a page", func() {
		deps := newTestDeps()
		createUC := pages.NewCreatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())
		copyUC := pages.NewCopyPageUseCase(deps.tree, deps.slug, deps.orchestrator(), deps.assets, slog.Default())

		original, _ := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: "user1", Title: "Original", Slug: "original", Kind: pageKind(),
		})

		out, err := copyUC.Execute(context.Background(), pages.CopyPageInput{
			UserID: "user1", SourcePageID: pageID(original.Page.ID), Title: "Copy of Original", Slug: "copy-of-original",
		})
		Expect(err).To(Succeed())
		Expect(out.Page).To(SatisfyAll(
			HaveField("Title", Equal("Copy of Original")),
			HaveField("Slug", Equal(slug("copy-of-original"))),
			HaveField("ID", Not(Equal(original.Page.ID))),
		))
	})

	ginkgo.It("copies a page under a parent", func() {
		deps := newTestDeps()
		createUC := pages.NewCreatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())
		copyUC := pages.NewCopyPageUseCase(deps.tree, deps.slug, deps.orchestrator(), deps.assets, slog.Default())

		parent, _ := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: "user1", Title: "Parent", Slug: "parent", Kind: pageKind(),
		})
		original, _ := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: "user1", Title: "Original", Slug: "original", Kind: pageKind(),
		})

		out, err := copyUC.Execute(context.Background(), pages.CopyPageInput{
			UserID: "user1", SourcePageID: pageID(original.Page.ID), TargetParentID: pageIDPtr(parent.Page.ID), Title: "Copy of Original", Slug: "copy-of-original",
		})
		Expect(err).To(Succeed())
		Expect(out.Page.Parent).To(gstruct.PointTo(HaveField("ID", Equal(parent.Page.ID))))
	})

	ginkgo.It("rejects copying a missing source page", func() {
		deps := newTestDeps()
		copyUC := pages.NewCopyPageUseCase(deps.tree, deps.slug, deps.orchestrator(), deps.assets, slog.Default())

		_, err := copyUC.Execute(context.Background(), pages.CopyPageInput{
			UserID: "user1", SourcePageID: "non-existent-id", Title: "Copy", Slug: "copy",
		})
		Expect(err).To(MatchError(tree.ErrPageNotFound))
	})

	ginkgo.It("copies page assets", func() {
		deps := newTestDeps()
		createUC := pages.NewCreatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())
		copyUC := pages.NewCopyPageUseCase(deps.tree, deps.slug, deps.orchestrator(), deps.assets, slog.Default())

		original, _ := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: "user1", Title: "Original", Slug: "original", Kind: pageKind(),
		})

		file, _, err := test_utils.CreateMultipartFile("image.png", []byte("image content"))
		Expect(err).To(Succeed())
		ginkgo.DeferCleanup(func() {
			if err := file.Close(); err != nil {
				Expect(err).To(Succeed())
			}
		})

		_, err = deps.assets.SaveAssetForPage(original.Page.PageNode, file, tree.AssetName("image.png"), 1024)
		Expect(err).To(Succeed())

		out, err := copyUC.Execute(context.Background(), pages.CopyPageInput{
			UserID: "user1", SourcePageID: pageID(original.Page.ID), Title: "Copy of Original", Slug: "copy-of-original",
		})
		Expect(err).To(Succeed())

		copiedAssets, err := deps.assets.ListAssetsForPage(out.Page.PageNode)
		Expect(err).To(Succeed())
		Expect(copiedAssets).To(HaveLen(1))
	})

	ginkgo.It("indexes outgoing links for copied pages", func() {
		deps := newTestDeps()
		createUC := pages.NewCreatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())
		updateUC := pages.NewUpdatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())
		copyUC := pages.NewCopyPageUseCase(deps.tree, deps.slug, deps.orchestrator(), deps.assets, slog.Default())

		target, err := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: "user1", Title: "Target", Slug: "target", Kind: pageKind(),
		})
		Expect(err).To(Succeed())

		original, err := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: "user1", Title: "Original", Slug: "original", Kind: pageKind(),
		})
		Expect(err).To(Succeed())

		content := "Links: [Target](/target.md)"
		if _, err := updateUC.Execute(context.Background(), pages.UpdatePageInput{
			UserID: "user1", ID: pageID(original.Page.ID), Version: pageVersion(original.Page.Version()), Title: original.Page.Title, Slug: slug(original.Page.Slug), Content: &content, Kind: pageKind(),
		}); err != nil {
			Expect(err).To(Succeed())
		}

		out, err := copyUC.Execute(context.Background(), pages.CopyPageInput{
			UserID: "user1", SourcePageID: pageID(original.Page.ID), Title: "Copy", Slug: "copy",
		})
		Expect(err).To(Succeed())

		outgoing, err := deps.links.GetOutgoingLinksForPage(out.Page.ID)
		Expect(err).To(Succeed())
		Expect(outgoing).To(SatisfyAll(
			HaveField("Count", Equal(1)),
			HaveField("Outgoings", HaveExactElements(HaveHealthyOutgoing("/target", target.Page.ID))),
		))
	})

	ginkgo.It("omits live before snapshots from update events", func() {
		deps := newTestDeps()
		effect := &captureEffect{}
		orchestrator := pagesave.NewPageSaveOrchestrator(effect)
		createUC := pages.NewCreatePageUseCase(deps.tree, deps.slug, orchestrator, slog.Default())
		updateUC := pages.NewUpdatePageUseCase(deps.tree, deps.slug, orchestrator, slog.Default())

		created, err := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: "user1", Title: "Old", Slug: "old", Kind: pageKind(),
		})
		Expect(err).To(Succeed())

		content := "updated"
		if _, err := updateUC.Execute(context.Background(), pages.UpdatePageInput{
			UserID: "user1", ID: pageID(created.Page.ID), Version: pageVersion(created.Page.Version()), Title: "New", Slug: "new", Content: &content, Kind: pageKind(),
		}); err != nil {
			Expect(err).To(Succeed())
		}

		Expect(effect.events).To(SatisfyAll(
			HaveLen(2),
			ContainElement(SatisfyAll(
				HaveField("Operation", Equal(pagesave.PageOperationUpdate)),
				HaveField("Before", BeNil()),
				HaveField("OldPath", Equal(tree.RoutePath("old"))),
			)),
		))
	})

	ginkgo.It("omits live before snapshots from move events", func() {
		deps := newTestDeps()
		effect := &captureEffect{}
		orchestrator := pagesave.NewPageSaveOrchestrator(effect)
		createUC := pages.NewCreatePageUseCase(deps.tree, deps.slug, orchestrator, slog.Default())
		moveUC := pages.NewMovePageUseCase(deps.tree, orchestrator, slog.Default())

		parentA, err := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: "user1", Title: "A", Slug: "a", Kind: sectionKind(),
		})
		Expect(err).To(Succeed())
		parentB, err := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: "user1", Title: "B", Slug: "b", Kind: sectionKind(),
		})
		Expect(err).To(Succeed())
		child, err := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: "user1", ParentID: pageIDPtr(parentA.Page.ID), Title: "Child", Slug: "child", Kind: pageKind(),
		})
		Expect(err).To(Succeed())

		if err := moveUC.Execute(context.Background(), pages.MovePageInput{
			UserID: "user1", ID: pageID(child.Page.ID), Version: pageVersion(child.Page.Version()), ParentID: pageID(parentB.Page.ID),
		}); err != nil {
			Expect(err).To(Succeed())
		}

		Expect(effect.events).To(SatisfyAll(
			HaveLen(4),
			ContainElement(SatisfyAll(
				HaveField("Operation", Equal(pagesave.PageOperationMove)),
				HaveField("Before", BeNil()),
				HaveField("OldPath", Equal(tree.RoutePath("a/child"))),
			)),
		))
	})

	ginkgo.It("previews affected pages for page renames", func() {
		deps := newTestDeps()
		createUC := pages.NewCreatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())
		updateUC := pages.NewUpdatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())
		previewUC := pages.NewPreviewPageRefactorUseCase(deps.tree, deps.slug, deps.links, slog.Default())

		target, _ := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: "system", Title: "Target", Slug: "target", Kind: pageKind(),
		})
		ref, _ := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: "system", Title: "Ref", Slug: "ref", Kind: pageKind(),
		})
		content := "[Target](/target.md)"
		if _, err := updateUC.Execute(context.Background(), pages.UpdatePageInput{
			UserID: "system", ID: pageID(ref.Page.ID), Version: pageVersion(ref.Page.Version()), Title: ref.Page.Title, Slug: slug(ref.Page.Slug), Content: &content, Kind: pageKind(),
		}); err != nil {
			Expect(err).To(Succeed())
		}

		preview, err := previewUC.Execute(context.Background(), pages.RefactorPreviewInput{
			PageID: pageID(target.Page.ID),
			Kind:   pages.RefactorKindRename,
			Title:  target.Page.Title,
			Slug:   "target-renamed",
		})
		Expect(err).To(Succeed())
		Expect(preview.OldPath).To(Equal(tree.RoutePath("target")))
		Expect(preview.NewPath).To(Equal("/target-renamed"))
		Expect(preview.Counts.AffectedPages).To(Equal(1))
		Expect(preview.AffectedPages).To(HaveExactElements(HaveField("FromPageID", Equal(ref.Page.ID))))
	})

	ginkgo.It("ignores non-canonical extensionless links in rename previews", func() {
		deps := newTestDeps()
		createUC := pages.NewCreatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())
		updateUC := pages.NewUpdatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())
		previewUC := pages.NewPreviewPageRefactorUseCase(deps.tree, deps.slug, deps.links, slog.Default())

		ref, err := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: "system", Title: "Ref", Slug: "ref", Kind: pageKind(),
		})
		Expect(err).To(Succeed())
		content := "[Target](/target)"
		if _, err := updateUC.Execute(context.Background(), pages.UpdatePageInput{
			UserID: "system", ID: pageID(ref.Page.ID), Version: pageVersion(ref.Page.Version()), Title: ref.Page.Title, Slug: slug(ref.Page.Slug), Content: &content, Kind: pageKind(),
		}); err != nil {
			Expect(err).To(Succeed())
		}

		target, err := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: "system", Title: "Target", Slug: "target", Kind: pageKind(),
		})
		Expect(err).To(Succeed())

		preview, err := previewUC.Execute(context.Background(), pages.RefactorPreviewInput{
			PageID: pageID(target.Page.ID),
			Kind:   pages.RefactorKindRename,
			Title:  target.Page.Title,
			Slug:   "target-renamed",
		})
		Expect(err).To(Succeed())
		Expect(preview.Counts.AffectedPages).To(BeZero())
	})

	ginkgo.It("leaves non-canonical extensionless links unchanged during rename apply", func() {
		deps := newTestDeps()
		createUC := pages.NewCreatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())
		updateUC := pages.NewUpdatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())
		applyUC := pages.NewApplyPageRefactorUseCase(deps.tree, deps.slug, deps.links, slog.Default())

		ref, err := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: "system", Title: "Ref", Slug: "ref", Kind: pageKind(),
		})
		Expect(err).To(Succeed())
		content := "[Target](/target)"
		if _, err := updateUC.Execute(context.Background(), pages.UpdatePageInput{
			UserID: "system", ID: pageID(ref.Page.ID), Version: pageVersion(ref.Page.Version()), Title: ref.Page.Title, Slug: slug(ref.Page.Slug), Content: &content, Kind: pageKind(),
		}); err != nil {
			Expect(err).To(Succeed())
		}

		target, err := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: "system", Title: "Target", Slug: "target", Kind: pageKind(),
		})
		Expect(err).To(Succeed())

		if _, err := applyUC.Execute(context.Background(), pages.RefactorApplyInput{
			UserID:  "system",
			Version: pageVersion(target.Page.Version()),
			RefactorPreviewInput: pages.RefactorPreviewInput{
				PageID:  pageID(target.Page.ID),
				Kind:    pages.RefactorKindRename,
				Title:   "Target Renamed",
				Slug:    "target-renamed",
				Content: &target.Page.Content,
			},
			RewriteLinks: true,
		}); err != nil {
			Expect(err).To(Succeed())
		}

		refPage, err := deps.tree.GetPage(pageID(ref.Page.ID))
		Expect(err).To(Succeed())
		Expect(refPage.Content).To(Equal(content))
	})

	ginkgo.It("ignores section-twin descendants when previewing a page rename", func() {
		deps := newTestDeps()
		createUC := pages.NewCreatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())
		updateUC := pages.NewUpdatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())
		previewUC := pages.NewPreviewPageRefactorUseCase(deps.tree, deps.slug, deps.links, slog.Default())

		docs, err := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: "system", Title: "Docs", Slug: "docs", Kind: sectionKind(),
		})
		Expect(err).To(Succeed())
		syncPage, err := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: "system", ParentID: pageIDPtr(docs.Page.ID), Title: "Sync Page", Slug: "sync", Kind: pageKind(),
		})
		Expect(err).To(Succeed())
		syncSection, err := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: "system", ParentID: pageIDPtr(docs.Page.ID), Title: "Sync Section", Slug: "sync", Kind: sectionKind(),
		})
		Expect(err).To(Succeed())
		if _, err := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: "system", ParentID: pageIDPtr(syncSection.Page.ID), Title: "Child", Slug: "child", Kind: pageKind(),
		}); err != nil {
			Expect(err).To(Succeed())
		}
		pageRef, err := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: "system", Title: "Page Ref", Slug: "page-ref", Kind: pageKind(),
		})
		Expect(err).To(Succeed())
		sectionDescendantRef, err := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: "system", Title: "Section Descendant Ref", Slug: "section-descendant-ref", Kind: pageKind(),
		})
		Expect(err).To(Succeed())
		pageRefContent := "[Sync page](/docs/sync.md)"
		if _, err := updateUC.Execute(context.Background(), pages.UpdatePageInput{
			UserID: "system", ID: pageID(pageRef.Page.ID), Version: pageVersion(pageRef.Page.Version()), Title: pageRef.Page.Title, Slug: slug(pageRef.Page.Slug), Content: &pageRefContent, Kind: pageKind(),
		}); err != nil {
			Expect(err).To(Succeed())
		}
		sectionDescendantRefContent := "[Sync child](/docs/sync/child.md)"
		if _, err := updateUC.Execute(context.Background(), pages.UpdatePageInput{
			UserID: "system", ID: pageID(sectionDescendantRef.Page.ID), Version: pageVersion(sectionDescendantRef.Page.Version()), Title: sectionDescendantRef.Page.Title, Slug: slug(sectionDescendantRef.Page.Slug), Content: &sectionDescendantRefContent, Kind: pageKind(),
		}); err != nil {
			Expect(err).To(Succeed())
		}

		preview, err := previewUC.Execute(context.Background(), pages.RefactorPreviewInput{
			PageID: pageID(syncPage.Page.ID),
			Kind:   pages.RefactorKindRename,
			Title:  syncPage.Page.Title,
			Slug:   "sync-page",
		})
		Expect(err).To(Succeed())
		Expect(preview.Counts.AffectedPages).To(Equal(1))
		Expect(preview.AffectedPages).To(HaveExactElements(HaveField("FromPageID", Equal(pageRef.Page.ID))))
	})

	ginkgo.It("keeps section-twin descendant links healthy during page rename apply", func() {
		deps := newTestDeps()
		createUC := pages.NewCreatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())
		updateUC := pages.NewUpdatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())
		applyUC := pages.NewApplyPageRefactorUseCase(deps.tree, deps.slug, deps.links, slog.Default())

		docs, err := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: "system", Title: "Docs", Slug: "docs", Kind: sectionKind(),
		})
		Expect(err).To(Succeed())
		syncPage, err := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: "system", ParentID: pageIDPtr(docs.Page.ID), Title: "Sync Page", Slug: "sync", Kind: pageKind(),
		})
		Expect(err).To(Succeed())
		syncSection, err := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: "system", ParentID: pageIDPtr(docs.Page.ID), Title: "Sync Section", Slug: "sync", Kind: sectionKind(),
		})
		Expect(err).To(Succeed())
		syncChild, err := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: "system", ParentID: pageIDPtr(syncSection.Page.ID), Title: "Child", Slug: "child", Kind: pageKind(),
		})
		Expect(err).To(Succeed())
		pageRef, err := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: "system", Title: "Page Ref", Slug: "page-ref", Kind: pageKind(),
		})
		Expect(err).To(Succeed())
		sectionDescendantRef, err := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: "system", Title: "Section Descendant Ref", Slug: "section-descendant-ref", Kind: pageKind(),
		})
		Expect(err).To(Succeed())
		pageRefContent := "[Sync page](/docs/sync.md)"
		if _, err := updateUC.Execute(context.Background(), pages.UpdatePageInput{
			UserID: "system", ID: pageID(pageRef.Page.ID), Version: pageVersion(pageRef.Page.Version()), Title: pageRef.Page.Title, Slug: slug(pageRef.Page.Slug), Content: &pageRefContent, Kind: pageKind(),
		}); err != nil {
			Expect(err).To(Succeed())
		}
		sectionDescendantRefContent := "[Sync child](/docs/sync/child.md)"
		if _, err := updateUC.Execute(context.Background(), pages.UpdatePageInput{
			UserID: "system", ID: pageID(sectionDescendantRef.Page.ID), Version: pageVersion(sectionDescendantRef.Page.Version()), Title: sectionDescendantRef.Page.Title, Slug: slug(sectionDescendantRef.Page.Slug), Content: &sectionDescendantRefContent, Kind: pageKind(),
		}); err != nil {
			Expect(err).To(Succeed())
		}

		if _, err := applyUC.Execute(context.Background(), pages.RefactorApplyInput{
			UserID:  "system",
			Version: pageVersion(syncPage.Page.Version()),
			RefactorPreviewInput: pages.RefactorPreviewInput{
				PageID: pageID(syncPage.Page.ID),
				Kind:   pages.RefactorKindRename,
				Title:  syncPage.Page.Title,
				Slug:   "sync-page",
			},
			RewriteLinks: true,
		}); err != nil {
			Expect(err).To(Succeed())
		}

		status, err := deps.links.GetLinkStatusForPage(sectionDescendantRef.Page.ID, sectionDescendantRef.Page.CalculateRoutePath())
		Expect(err).To(Succeed())
		Expect(status.Counts.BrokenOutgoings).To(BeZero())
		Expect(status.Outgoings).To(HaveExactElements(HaveField("ToPageID", Equal(syncChild.Page.ID))))
	})

})
