package pages_test

import (
	"context"
	"log/slog"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/perber/wiki/internal/core/tree"
	"github.com/perber/wiki/internal/search"
	"github.com/perber/wiki/internal/wiki/pages"
	"github.com/perber/wiki/internal/wiki/pagesave"
)

// Canonical Markdown links plan scenarios covered by tests in this file:
// - Refactor preview reports conflicts without mutating content

// testDeps holds real services backed by a temporary directory.

var _ = ginkgo.Describe("page use case behavior", ginkgo.Label("integration"), func() {
	ginkgo.It("rewrites incoming links during rename apply", func() {
		deps := newTestDeps()
		createUC := pages.NewCreatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())
		updateUC := pages.NewUpdatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())
		applyUC := pages.NewApplyPageRefactorUseCase(deps.tree, deps.slug, deps.links, slog.Default())

		target, _ := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: newFixtureUserID("system"), Title: "Target", Slug: newFixtureSlug("target"), Kind: pageKind(),
		})
		ref, _ := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: newFixtureUserID("system"), Title: "Ref", Slug: newFixtureSlug("ref"), Kind: pageKind(),
		})
		content := "[Target](/target.md)"
		if _, err := updateUC.Execute(context.Background(), pages.UpdatePageInput{
			UserID: newFixtureUserID("system"), ID: pageID(ref.Page.ID), Version: pageVersion(ref.Page.Version()), Title: ref.Page.Title, Slug: slug(ref.Page.Slug), Content: &content, Kind: pageKind(),
		}); err != nil {
			Expect(err).To(Succeed())
		}

		updated, err := applyUC.Execute(context.Background(), pages.RefactorApplyInput{
			UserID:  newFixtureUserID("system"),
			Version: pageVersion(target.Page.Version()),
			RefactorPreviewInput: pages.RefactorPreviewInput{
				PageID:  pageID(target.Page.ID),
				Kind:    pages.RefactorKindRename,
				Title:   "Target Renamed",
				Slug:    newFixtureSlug("target-renamed"),
				Content: &target.Page.Content,
			},
			RewriteLinks: true,
		})
		Expect(err).To(Succeed())
		Expect(updated.CalculatePath()).To(Equal("/target-renamed"))

		refPage, err := deps.tree.GetPage(pageID(ref.Page.ID))
		Expect(err).To(Succeed())
		Expect(refPage.Content).To(Equal("[Target](/target-renamed.md)"))

		outgoing, err := deps.links.GetOutgoingLinksForPage(ref.Page.ID)
		Expect(err).To(Succeed())
		Expect(outgoing).To(SatisfyAll(
			HaveField("Count", Equal(1)),
			HaveField("Outgoings", HaveExactElements(HaveHealthyOutgoing(newFixtureRoutePath("/target-renamed"), target.Page.ID))),
		))

	})

	ginkgo.It("routes rewritten link events through the injected orchestrator", func() {
		deps := newTestDeps()
		createUC := pages.NewCreatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())
		updateUC := pages.NewUpdatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())
		capture := &captureEffect{}
		orchestrator := pagesave.NewPageSaveOrchestrator(
			pagesave.NewLinkIndexSideEffect(deps.links, slog.Default()),
			capture,
		)
		applyUC := pages.NewApplyPageRefactorUseCaseWithOrchestrator(deps.tree, deps.slug, deps.links, orchestrator, slog.Default())

		target, _ := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: newFixtureUserID("system"), Title: "Target", Slug: newFixtureSlug("target"), Kind: pageKind(),
		})
		ref, _ := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: newFixtureUserID("system"), Title: "Ref", Slug: newFixtureSlug("ref"), Kind: pageKind(),
		})
		content := "[Target](/target.md)"
		if _, err := updateUC.Execute(context.Background(), pages.UpdatePageInput{
			UserID: newFixtureUserID("system"), ID: pageID(ref.Page.ID), Version: pageVersion(ref.Page.Version()), Title: ref.Page.Title, Slug: slug(ref.Page.Slug), Content: &content, Kind: pageKind(),
		}); err != nil {
			Expect(err).To(Succeed())
		}

		if _, err := applyUC.Execute(context.Background(), pages.RefactorApplyInput{
			UserID:  newFixtureUserID("mcp-user"),
			Source:  pagesave.PageMutationSourceMCP,
			Version: pageVersion(target.Page.Version()),
			RefactorPreviewInput: pages.RefactorPreviewInput{
				PageID:  pageID(target.Page.ID),
				Kind:    pages.RefactorKindRename,
				Title:   "Target Renamed",
				Slug:    newFixtureSlug("target-renamed"),
				Content: &target.Page.Content,
			},
			RewriteLinks: true,
		}); err != nil {
			Expect(err).To(Succeed())
		}

		Expect(capture.events).To(ContainElement(SatisfyAll(
			HavePageSaveContentChange(),
			HaveField("Operation", Equal(pagesave.PageOperationUpdate)),
			HaveField("After", BeNil()),
			HaveField("AffectedPages", HaveExactElements(HaveField("ID", Equal(ref.Page.ID)))),
			HaveField("UserID", Equal(tree.UserIDFromString("mcp-user"))),
			HaveField("Source", Equal(pagesave.PageMutationSourceMCP)),
		)))
	})

	ginkgo.It("keeps rewritten link raw content searchable", func() {
		deps := newTestDeps()
		searchIndex, err := search.NewSQLiteIndex(pagesTestTempDir())
		Expect(err).To(Succeed())
		ginkgo.DeferCleanup(func() {
			if err := searchIndex.Close(); err != nil {
				Expect(err).To(Succeed())
			}
		})

		createUC := pages.NewCreatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())
		updateUC := pages.NewUpdatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())
		orchestrator := pagesave.NewPageSaveOrchestrator(
			pagesave.NewLinkIndexSideEffect(deps.links, slog.Default()),
			pagesave.NewSearchIndexSideEffect(searchIndex, deps.tree, slog.Default()),
		)
		applyUC := pages.NewApplyPageRefactorUseCaseWithOrchestrator(deps.tree, deps.slug, deps.links, orchestrator, slog.Default())

		target, _ := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: newFixtureUserID("system"), Title: "Target", Slug: newFixtureSlug("target"), Kind: pageKind(),
		})
		ref, _ := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: newFixtureUserID("system"), Title: "Ref", Slug: newFixtureSlug("ref"), Kind: pageKind(),
		})
		content := "refactorsearchtoken [Target](/target.md)"
		if _, err := updateUC.Execute(context.Background(), pages.UpdatePageInput{
			UserID: newFixtureUserID("system"), ID: pageID(ref.Page.ID), Version: pageVersion(ref.Page.Version()), Title: ref.Page.Title, Slug: slug(ref.Page.Slug), Content: &content, Kind: pageKind(),
		}); err != nil {
			Expect(err).To(Succeed())
		}

		if _, err := applyUC.Execute(context.Background(), pages.RefactorApplyInput{
			UserID:  newFixtureUserID("system"),
			Version: pageVersion(target.Page.Version()),
			RefactorPreviewInput: pages.RefactorPreviewInput{
				PageID:  pageID(target.Page.ID),
				Kind:    pages.RefactorKindRename,
				Title:   "Target Renamed",
				Slug:    newFixtureSlug("target-renamed"),
				Content: &target.Page.Content,
			},
			RewriteLinks: true,
		}); err != nil {
			Expect(err).To(Succeed())
		}

		result, err := searchIndex.Search("refactorsearchtoken", nil, 0, 10)
		Expect(err).To(Succeed())
		Expect(result.Items).To(HaveExactElements(HaveField("PageID", Equal(ref.Page.ID))))
	})

	ginkgo.It("leaves incoming links unchanged when rename apply has a stale version", func() {
		deps := newTestDeps()
		createUC := pages.NewCreatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())
		updateUC := pages.NewUpdatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())
		applyUC := pages.NewApplyPageRefactorUseCase(deps.tree, deps.slug, deps.links, slog.Default())

		target, _ := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: newFixtureUserID("system"), Title: "Target", Slug: newFixtureSlug("target"), Kind: pageKind(),
		})
		ref, _ := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: newFixtureUserID("system"), Title: "Ref", Slug: newFixtureSlug("ref"), Kind: pageKind(),
		})
		refContent := "[Target](/target)"
		if _, err := updateUC.Execute(context.Background(), pages.UpdatePageInput{
			UserID: newFixtureUserID("system"), ID: pageID(ref.Page.ID), Version: pageVersion(ref.Page.Version()), Title: ref.Page.Title, Slug: slug(ref.Page.Slug), Content: &refContent, Kind: pageKind(),
		}); err != nil {
			Expect(err).To(Succeed())
		}

		staleVersion := target.Page.Version()
		newerTargetContent := "newer target content"
		if _, err := updateUC.Execute(context.Background(), pages.UpdatePageInput{
			UserID: newFixtureUserID("system"), ID: pageID(target.Page.ID), Version: pageVersion(staleVersion), Title: target.Page.Title, Slug: slug(target.Page.Slug), Content: &newerTargetContent, Kind: pageKind(),
		}); err != nil {
			Expect(err).To(Succeed())
		}

		_, err := applyUC.Execute(context.Background(), pages.RefactorApplyInput{
			UserID:  newFixtureUserID("system"),
			Version: pageVersion(staleVersion),
			RefactorPreviewInput: pages.RefactorPreviewInput{
				PageID: pageID(target.Page.ID),
				Kind:   pages.RefactorKindRename,
				Title:  "Target Renamed",
				Slug:   newFixtureSlug("target-renamed"),
			},
			RewriteLinks: true,
		})
		Expect(err).To(MatchError(tree.ErrVersionConflict))

		refAfter, err := deps.tree.GetPage(pageID(ref.Page.ID))
		Expect(err).To(Succeed())
		Expect(refAfter.Content).To(Equal(refContent))
	})

	// - Refactor preview reports conflicts without mutating content
	ginkgo.It("leaves incoming links unchanged when rename target conflicts", func() {
		deps := newTestDeps()
		createUC := pages.NewCreatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())
		updateUC := pages.NewUpdatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())
		applyUC := pages.NewApplyPageRefactorUseCase(deps.tree, deps.slug, deps.links, slog.Default())

		target, _ := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: newFixtureUserID("system"), Title: "Target", Slug: newFixtureSlug("target"), Kind: pageKind(),
		})
		if _, err := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: newFixtureUserID("system"), Title: "Existing", Slug: newFixtureSlug("existing"), Kind: pageKind(),
		}); err != nil {
			Expect(err).To(Succeed())
		}
		ref, _ := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: newFixtureUserID("system"), Title: "Ref", Slug: newFixtureSlug("ref"), Kind: pageKind(),
		})
		refContent := "[Target](/target)"
		if _, err := updateUC.Execute(context.Background(), pages.UpdatePageInput{
			UserID: newFixtureUserID("system"), ID: pageID(ref.Page.ID), Version: pageVersion(ref.Page.Version()), Title: ref.Page.Title, Slug: slug(ref.Page.Slug), Content: &refContent, Kind: pageKind(),
		}); err != nil {
			Expect(err).To(Succeed())
		}

		_, err := applyUC.Execute(context.Background(), pages.RefactorApplyInput{
			UserID:  newFixtureUserID("system"),
			Version: pageVersion(target.Page.Version()),
			RefactorPreviewInput: pages.RefactorPreviewInput{
				PageID: pageID(target.Page.ID),
				Kind:   pages.RefactorKindRename,
				Title:  "Target",
				Slug:   newFixtureSlug("existing"),
			},
			RewriteLinks: true,
		})
		Expect(err).To(MatchError(tree.ErrPageAlreadyExists))

		refAfter, err := deps.tree.GetPage(pageID(ref.Page.ID))
		Expect(err).To(Succeed())
		Expect(refAfter.Content).To(Equal(refContent))
		targetAfter, err := deps.tree.GetPage(pageID(target.Page.ID))
		Expect(err).To(Succeed())
		Expect(targetAfter.CalculatePath()).To(Equal("/target"))
	})

	ginkgo.It("returns empty warning arrays in refactor previews", func() {
		deps := newTestDeps()
		createUC := pages.NewCreatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())
		previewUC := pages.NewPreviewPageRefactorUseCase(deps.tree, deps.slug, deps.links, slog.Default())

		page, _ := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: newFixtureUserID("system"), Title: "Target", Slug: newFixtureSlug("target"), Kind: pageKind(),
		})

		preview, err := previewUC.Execute(context.Background(), pages.RefactorPreviewInput{
			PageID: pageID(page.Page.ID),
			Kind:   pages.RefactorKindRename,
			Title:  page.Page.Title,
			Slug:   newFixtureSlug("target-renamed"),
		})
		Expect(err).To(Succeed())
		Expect(preview.WarningDetails).NotTo(BeNil())
		Expect(preview.WarningDetails).To(BeEmpty())
		for _, affected := range preview.AffectedPages {
			Expect(affected.WarningDetails).NotTo(BeNil())
			Expect(affected.MatchedPaths).NotTo(BeNil())
		}
	})

})
