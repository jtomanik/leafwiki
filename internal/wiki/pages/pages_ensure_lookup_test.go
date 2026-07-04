package pages_test

import (
	"context"
	"log/slog"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/perber/wiki/internal/core/tree"
	"github.com/perber/wiki/internal/wiki/pages"
	"github.com/perber/wiki/internal/wiki/pagesave"
)

// Canonical Markdown links plan scenarios covered by tests in this file:
// - Refactor preview reports conflicts without mutating content

// testDeps holds real services backed by a temporary directory.

var _ = ginkgo.Describe("page use case behavior", ginkgo.Label("integration"), func() {
	ginkgo.It("records convert mutations with the requested source", func() {
		deps := newTestDeps()
		createUC := pages.NewCreatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())
		capture := &captureEffect{}
		convertUC := pages.NewConvertPageUseCase(deps.tree, pagesave.NewPageSaveOrchestrator(capture), slog.Default())

		page, err := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: "user1", Title: "Convert Me", Slug: "convert-me", Kind: pageKind(),
		})
		Expect(err).To(Succeed())

		if err := convertUC.Execute(context.Background(), pages.ConvertPageInput{
			UserID:     "mcp-user",
			Source:     pagesave.PageMutationSourceMCP,
			ID:         pageID(page.Page.ID),
			Version:    pageVersion(page.Page.Version()),
			TargetKind: tree.NodeKindSection,
		}); err != nil {
			Expect(err).To(Succeed())
		}

		Expect(capture.events).To(HaveExactElements(SatisfyAll(
			HaveField("Operation", Equal(pagesave.PageOperationUpdate)),
			HaveField("UserID", Equal(tree.UserIDFromString("mcp-user"))),
			HaveField("Source", Equal(pagesave.PageMutationSourceMCP)),
			HaveField("Before", Not(BeNil())),
			HaveField("After", WithTransform(func(page *tree.Page) tree.NodeKind {
				if page == nil {
					return ""
				}
				return page.Kind
			}, Equal(tree.NodeKindSection))),
			HaveField("AffectedPages", ContainElement(HaveField("ID", Equal(page.Page.ID)))),
		)))
	})

	// ─────────────────────────────────────────────────────────────────────────────
	// EnsurePathUseCase
	// ─────────────────────────────────────────────────────────────────────────────

	ginkgo.It("creates every segment in a missing path", func() {
		deps := newTestDeps()
		uc := pages.NewEnsurePathUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())

		out, err := uc.Execute(context.Background(), pages.EnsurePathInput{
			UserID:      "user1",
			TargetPath:  "docs/reference",
			TargetTitle: "Reference",
			Kind:        pageKind(),
		})
		Expect(err).To(Succeed())
		Expect(out.Page).NotTo(BeNil())
	})

	ginkgo.It("returns an existing page for an existing path", func() {
		deps := newTestDeps()
		uc := pages.NewEnsurePathUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())

		out1, err := uc.Execute(context.Background(), pages.EnsurePathInput{
			UserID: "user1", TargetPath: "docs", TargetTitle: "Docs", Kind: pageKind(),
		})
		Expect(err).To(Succeed())

		out2, err := uc.Execute(context.Background(), pages.EnsurePathInput{
			UserID: "user1", TargetPath: "docs", TargetTitle: "Docs", Kind: pageKind(),
		})
		Expect(err).To(Succeed())
		Expect(out1.Page.ID).To(Equal(out2.Page.ID))
	})

	ginkgo.It("creates a page twin when a section owns the route", func() {
		deps := newTestDeps()
		createUC := pages.NewCreatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())
		ensureUC := pages.NewEnsurePathUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())

		section, err := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: "user1",
			Title:  "Sync Section",
			Slug:   "sync",
			Kind:   sectionKind(),
		})
		Expect(err).To(Succeed())

		out, err := ensureUC.Execute(context.Background(), pages.EnsurePathInput{
			UserID:      "user1",
			TargetPath:  "sync",
			TargetTitle: "Sync Page",
			Kind:        pageKind(),
		})
		Expect(err).To(Succeed())
		Expect(out.Page).To(SatisfyAll(
			HaveField("ID", Not(Equal(section.Page.ID))),
			HaveField("Kind", Equal(tree.NodeKindPage)),
		))

		sectionTwin, err := deps.tree.FindPageByRoutePathAndKind("sync", tree.NodeKindSection)
		Expect(err).To(Succeed())
		Expect(sectionTwin.ID).To(Equal(section.Page.ID))
		pageTwin, err := deps.tree.FindPageByRoutePathAndKind("sync", tree.NodeKindPage)
		Expect(err).To(Succeed())
		Expect(pageTwin.ID).To(Equal(out.Page.ID))

		second, err := ensureUC.Execute(context.Background(), pages.EnsurePathInput{
			UserID:      "user1",
			TargetPath:  "sync",
			TargetTitle: "Ignored",
			Kind:        pageKind(),
		})
		Expect(err).To(Succeed())
		Expect(second.Page.ID).To(Equal(out.Page.ID))
	})

	ginkgo.It("creates a section twin when a page owns the route", func() {
		deps := newTestDeps()
		createUC := pages.NewCreatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())
		ensureUC := pages.NewEnsurePathUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())

		page, err := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: "user1",
			Title:  "Sync Page",
			Slug:   "sync",
			Kind:   pageKind(),
		})
		Expect(err).To(Succeed())

		out, err := ensureUC.Execute(context.Background(), pages.EnsurePathInput{
			UserID:      "user1",
			TargetPath:  "sync",
			TargetTitle: "Sync Section",
			Kind:        sectionKind(),
		})
		Expect(err).To(Succeed())
		Expect(out.Page).To(SatisfyAll(
			HaveField("ID", Not(Equal(page.Page.ID))),
			HaveField("Kind", Equal(tree.NodeKindSection)),
		))

		pageTwin, err := deps.tree.FindPageByRoutePathAndKind("sync", tree.NodeKindPage)
		Expect(err).To(Succeed())
		Expect(pageTwin.ID).To(Equal(page.Page.ID))
		sectionTwin, err := deps.tree.FindPageByRoutePathAndKind("sync", tree.NodeKindSection)
		Expect(err).To(Succeed())
		Expect(sectionTwin.ID).To(Equal(out.Page.ID))

		second, err := ensureUC.Execute(context.Background(), pages.EnsurePathInput{
			UserID:      "user1",
			TargetPath:  "sync",
			TargetTitle: "Ignored",
			Kind:        sectionKind(),
		})
		Expect(err).To(Succeed())
		Expect(second.Page.ID).To(Equal(out.Page.ID))
	})

	ginkgo.It("rejects ensure path without a path", func() {
		deps := newTestDeps()
		uc := pages.NewEnsurePathUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())

		_, err := uc.Execute(context.Background(), pages.EnsurePathInput{
			UserID: "user1", TargetPath: "", TargetTitle: "Title", Kind: pageKind(),
		})
		Expect(err).To(HavePageValidationFieldError("path", pages.FieldCodePagePathRequired, pages.MessageIDPagePathRequired))
	})

	ginkgo.It("rejects ensure path with an invalid route", func() {
		deps := newTestDeps()
		uc := pages.NewEnsurePathUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())

		_, err := uc.Execute(context.Background(), pages.EnsurePathInput{
			UserID: "user1", TargetPath: tree.RoutePath("docs//guide"), TargetTitle: "Guide", Kind: pageKind(),
		})
		Expect(err).To(HavePageValidationFieldError("path", pages.FieldCodePagePathInvalid, pages.MessageIDPagePathInvalid))
	})

	// ─────────────────────────────────────────────────────────────────────────────
	// GetPageUseCase
	// ─────────────────────────────────────────────────────────────────────────────

	ginkgo.It("returns an existing page by ID", func() {
		deps := newTestDeps()
		createUC := pages.NewCreatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())
		getUC := pages.NewGetPageUseCase(deps.tree)

		created, _ := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: "user1", Title: "Home", Slug: "home", Kind: pageKind(),
		})

		out, err := getUC.Execute(context.Background(), pages.GetPageInput{ID: pageID(created.Page.ID)})
		Expect(err).To(Succeed())
		Expect(out.Page.ID).To(Equal(created.Page.ID))
	})

	ginkgo.It("reports an error for missing pages", func() {
		deps := newTestDeps()
		getUC := pages.NewGetPageUseCase(deps.tree)

		_, err := getUC.Execute(context.Background(), pages.GetPageInput{ID: "nonexistent"})
		Expect(err).To(MatchError(tree.ErrPageNotFound))
	})

	ginkgo.It("rejects the reserved history slug", func() {
		deps := newTestDeps()
		uc := pages.NewCreatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())

		_, err := uc.Execute(context.Background(), pages.CreatePageInput{
			UserID: "user1",
			Title:  "Reserved",
			Slug:   "history",
			Kind:   pageKind(),
		})
		Expect(err).To(HavePageValidationFieldError("slug", pages.FieldCodePageSlugInvalid, pages.MessageIDPageSlugInvalid))
	})

	ginkgo.It("rejects duplicate page routes", func() {
		deps := newTestDeps()
		uc := pages.NewCreatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())

		if _, err := uc.Execute(context.Background(), pages.CreatePageInput{
			UserID: "user1", Title: "Duplicate", Slug: "duplicate", Kind: pageKind(),
		}); err != nil {
			Expect(err).To(Succeed())
		}

		_, err := uc.Execute(context.Background(), pages.CreatePageInput{
			UserID: "user1", Title: "Duplicate", Slug: "duplicate", Kind: pageKind(),
		})
		Expect(err).To(MatchError(tree.ErrPageAlreadyExists))
	})

	ginkgo.It("rejects a missing parent", func() {
		deps := newTestDeps()
		uc := pages.NewCreatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())
		invalidID := "not-real"

		_, err := uc.Execute(context.Background(), pages.CreatePageInput{
			UserID: "user1", ParentID: pageIDPtr(invalidID), Title: "Broken", Slug: "broken", Kind: pageKind(),
		})
		Expect(err).To(MatchError(tree.ErrPageNotFound))
	})

	ginkgo.It("rejects case-insensitive slug conflicts", func() {
		deps := newTestDeps()
		uc := pages.NewCreatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())

		if _, err := uc.Execute(context.Background(), pages.CreatePageInput{
			UserID: "user1", Title: "Upper", Slug: "ABCD-efg", Kind: pageKind(),
		}); err != nil {
			Expect(err).To(Succeed())
		}

		_, err := uc.Execute(context.Background(), pages.CreatePageInput{
			UserID: "user1", Title: "Lower", Slug: "abcd-efg", Kind: pageKind(),
		})
		Expect(err).To(MatchError(tree.ErrPageAlreadyExists))
	})

	ginkgo.It("preserves uppercase slugs on update", func() {
		deps := newTestDeps()
		createUC := pages.NewCreatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())
		updateUC := pages.NewUpdatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())

		created, err := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: "user1", Title: "Original", Slug: "original", Kind: pageKind(),
		})
		Expect(err).To(Succeed())

		content := "# Updated"
		out, err := updateUC.Execute(context.Background(), pages.UpdatePageInput{
			UserID:  "user1",
			ID:      pageID(created.Page.ID),
			Version: pageVersion(created.Page.Version()),
			Title:   "Original",
			Slug:    "ABCD-efg",
			Content: &content,
			Kind:    pageKind(),
		})
		Expect(err).To(Succeed())
		Expect(out.Page.Slug).To(Equal(slug("ABCD-efg")))
	})

	ginkgo.It("rejects delete requests without an ID", func() {
		deps := newTestDeps()
		deleteUC := pages.NewDeletePageUseCase(deps.tree, deps.assets, deps.orchestrator(), slog.Default())

		err := deleteUC.Execute(context.Background(), pages.DeletePageInput{
			UserID: "user1", ID: "", Recursive: false,
		})
		Expect(err).To(MatchPageLocalizedCode(pages.ErrCodePageRootOperation))
	})

	ginkgo.It("finds a page by route", func() {
		deps := newTestDeps()
		createUC := pages.NewCreatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())
		findUC := pages.NewFindByPathUseCase(deps.tree)

		if _, err := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: "user1", Title: "Company", Slug: "company", Kind: pageKind(),
		}); err != nil {
			Expect(err).To(Succeed())
		}

		out, err := findUC.Execute(context.Background(), pages.FindByPathInput{RoutePath: "company"})
		Expect(err).To(Succeed())
		Expect(out.Page.Slug).To(Equal(slug("company")))
	})

	ginkgo.It("reports missing page route lookup", func() {
		deps := newTestDeps()
		findUC := pages.NewFindByPathUseCase(deps.tree)

		_, err := findUC.Execute(context.Background(), pages.FindByPathInput{RoutePath: "does/not/exist"})
		Expect(err).To(MatchError(tree.ErrPageNotFound))
	})

	ginkgo.It("rejects invalid lookup route paths", func() {
		deps := newTestDeps()
		lookupUC := pages.NewLookupPagePathUseCase(deps.tree)

		_, err := lookupUC.Execute(context.Background(), pages.LookupPagePathInput{
			Path: tree.RoutePath("docs//guide"),
		})
		Expect(err).To(HavePageValidationFieldError("path", pages.FieldCodePagePathInvalid, pages.MessageIDPagePathInvalid))
	})

	ginkgo.It("reorders child pages", func() {
		deps := newTestDeps()
		createUC := pages.NewCreatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())
		sortUC := pages.NewSortPagesUseCase(deps.tree)

		parent, _ := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: "user1", Title: "Parent", Slug: "parent", Kind: pageKind(),
		})
		child1, _ := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: "user1", ParentID: pageIDPtr(parent.Page.ID), Title: "Child1", Slug: "child1", Kind: pageKind(),
		})
		child2, _ := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: "user1", ParentID: pageIDPtr(parent.Page.ID), Title: "Child2", Slug: "child2", Kind: pageKind(),
		})

		if err := sortUC.Execute(context.Background(), pages.SortPagesInput{
			ParentID: pageID(parent.Page.ID), OrderedIDs: []tree.PageID{pageID(child2.Page.ID), pageID(child1.Page.ID)},
		}); err != nil {
			Expect(err).To(Succeed())
		}

		sortedParent, err := deps.tree.GetPage(pageID(parent.Page.ID))
		Expect(err).To(Succeed())
		Expect(sortedParent.Children).To(HaveExactElements(
			HaveField("ID", Equal(child2.Page.ID)),
			HaveField("ID", Equal(child1.Page.ID)),
		))
	})

	ginkgo.It("suggests a unique slug", func() {
		deps := newTestDeps()
		uc := pages.NewSuggestSlugUseCase(deps.tree, deps.slug)

		out, err := uc.Execute(context.Background(), pages.SuggestSlugInput{
			ParentID: "root",
			Title:    "My Page",
		})
		Expect(err).To(Succeed())
		Expect(out.Slug).To(Equal(slug("my-page")))
	})

	ginkgo.It("suggests a conflict suffix", func() {
		deps := newTestDeps()
		createUC := pages.NewCreatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())
		uc := pages.NewSuggestSlugUseCase(deps.tree, deps.slug)

		if _, err := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: "user1", Title: "My Page", Slug: "my-page", Kind: pageKind(),
		}); err != nil {
			Expect(err).To(Succeed())
		}

		out, err := uc.Execute(context.Background(), pages.SuggestSlugInput{
			ParentID: pageID(deps.tree.GetTree().ID),
			Title:    "My Page",
		})
		Expect(err).To(Succeed())
		Expect(out.Slug).To(Equal(slug("my-page-1")))
	})

	ginkgo.It("suggests a slug in a deep hierarchy", func() {
		deps := newTestDeps()
		createUC := pages.NewCreatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())
		uc := pages.NewSuggestSlugUseCase(deps.tree, deps.slug)

		arch, err := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: "user1", Title: "Architecture", Slug: "architecture", Kind: pageKind(),
		})
		Expect(err).To(Succeed())
		backend, err := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: "user1", ParentID: pageIDPtr(arch.Page.ID), Title: "Backend", Slug: "backend", Kind: pageKind(),
		})
		Expect(err).To(Succeed())

		out, err := uc.Execute(context.Background(), pages.SuggestSlugInput{
			ParentID: pageID(backend.Page.ID),
			Title:    "Data Layer",
		})
		Expect(err).To(Succeed())
		Expect(out.Slug).To(Equal(slug("data-layer")))

		if _, err := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: "user1", ParentID: pageIDPtr(backend.Page.ID), Title: "Data Layer", Slug: "data-layer", Kind: pageKind(),
		}); err != nil {
			Expect(err).To(Succeed())
		}

		out2, err := uc.Execute(context.Background(), pages.SuggestSlugInput{
			ParentID: pageID(backend.Page.ID),
			Title:    "Data Layer",
		})
		Expect(err).To(Succeed())
		Expect(out2.Slug).To(Equal(slug("data-layer-1")))
	})

})
