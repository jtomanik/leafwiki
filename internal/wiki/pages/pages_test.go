package pages_test

import (
	"context"
	"errors"
	"log/slog"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"

	"github.com/perber/wiki/internal/core/assets"
	"github.com/perber/wiki/internal/core/tree"
	"github.com/perber/wiki/internal/links"
	"github.com/perber/wiki/internal/wiki/pages"
	"github.com/perber/wiki/internal/wiki/pagesave"
)

// Canonical Markdown links plan scenarios covered by tests in this file:
// - Refactor preview reports conflicts without mutating content

// testDeps holds real services backed by a temporary directory.
type testDeps struct {
	storageDir string
	tree       *tree.TreeService
	slug       *tree.SlugService
	links      *links.LinkService
	assets     *assets.AssetService
}

func newTestDeps() *testDeps {
	ginkgo.GinkgoHelper()

	storageDir := pagesTestTempDir()

	treeService := tree.NewTreeService(storageDir)
	Expect(treeService.LoadTree()).To(Succeed())

	slugService := tree.NewSlugService()
	assetService := assets.NewAssetService(storageDir, slugService)

	linksStore, err := links.NewLinksStore(storageDir)
	Expect(err).To(Succeed())
	linkService := links.NewLinkService(storageDir, treeService, linksStore)
	ginkgo.DeferCleanup(func() {
		Expect(linkService.Close()).To(Succeed())
	})

	return &testDeps{
		storageDir: storageDir,
		tree:       treeService,
		slug:       slugService,
		links:      linkService,
		assets:     assetService,
	}
}

func (d *testDeps) orchestrator() *pagesave.PageSaveOrchestrator {
	return pagesave.NewPageSaveOrchestrator(
		pagesave.NewLinkIndexSideEffect(d.links, slog.Default()),
	)
}

type captureEffect struct {
	events []pagesave.PageSaveEvent
}

func (e *captureEffect) Apply(event pagesave.PageSaveEvent) {
	e.events = append(e.events, event)
}

func pageKind() *tree.NodeKind {
	k := tree.NodeKindPage
	return &k
}

func sectionKind() *tree.NodeKind {
	k := tree.NodeKindSection
	return &k
}

func pageID[T ~string](id T) tree.PageID {
	return tree.PageIDFromString(id)
}

func pageIDPtr[T ~string](id T) *tree.PageID {
	typed := pageID(id)
	return &typed
}

func pageVersion[T ~string](version T) tree.PageVersion {
	return newFixturePageVersion(version)
}

func slug[T ~string](value T) tree.Slug {
	return newFixtureSlug(value)
}

// ─────────────────────────────────────────────────────────────────────────────
// CreatePageUseCase
// ─────────────────────────────────────────────────────────────────────────────

var _ = ginkgo.Describe("page use case behavior", ginkgo.Label("integration"), func() {
	ginkgo.It("creates a root page", func() {
		deps := newTestDeps()
		uc := pages.NewCreatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())

		out, err := uc.Execute(context.Background(), pages.CreatePageInput{
			UserID: newFixtureUserID("user1"),
			Title:  "Home",
			Slug:   newFixtureSlug("home"),
			Kind:   pageKind(),
		})
		Expect(err).To(Succeed())
		Expect(out.Page).To(SatisfyAll(
			HaveField("Title", Equal("Home")),
			HaveField("Slug", Equal(slug("home"))),
		))
	})

	ginkgo.It("creates a child page under an existing parent", func() {
		deps := newTestDeps()
		uc := pages.NewCreatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())

		parent, err := uc.Execute(context.Background(), pages.CreatePageInput{
			UserID: newFixtureUserID("user1"),
			Title:  "Docs",
			Slug:   newFixtureSlug("docs"),
			Kind:   pageKind(),
		})
		Expect(err).To(Succeed())

		child, err := uc.Execute(context.Background(), pages.CreatePageInput{
			UserID:   newFixtureUserID("user1"),
			ParentID: pageIDPtr(parent.Page.ID),
			Title:    "Reference",
			Slug:     newFixtureSlug("reference"),
			Kind:     pageKind(),
		})
		Expect(err).To(Succeed())
		Expect(child.Page.Parent).To(gstruct.PointTo(HaveField("ID", Equal(parent.Page.ID))))
	})

	ginkgo.It("rejects creation without a title", func() {
		deps := newTestDeps()
		uc := pages.NewCreatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())

		_, err := uc.Execute(context.Background(), pages.CreatePageInput{
			UserID: newFixtureUserID("user1"),
			Title:  "",
			Slug:   newFixtureSlug("home"),
			Kind:   pageKind(),
		})
		Expect(err).To(HavePageValidationFieldError("title", pages.FieldCodePageTitleRequired, pages.MessageIDPageTitleRequired))
	})

	ginkgo.It("rejects reserved slugs during creation", func() {
		deps := newTestDeps()
		uc := pages.NewCreatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())

		_, err := uc.Execute(context.Background(), pages.CreatePageInput{
			UserID: newFixtureUserID("user1"),
			Title:  "Reserved",
			Slug:   newFixtureSlug("e"), // too short / reserved
			Kind:   pageKind(),
		})
		Expect(err).To(HavePageValidationFieldError("slug", pages.FieldCodePageSlugInvalid, pages.MessageIDPageSlugInvalid))
	})

	ginkgo.It("keeps page use-case boundaries typed", func() {
		pageID := newFixturePageID("page-1")
		parentID := newFixturePageID("parent-1")
		version := newFixturePageVersion("version-1")
		slug := newFixtureSlug("page-slug")
		routePath := newFixtureRoutePath("docs/page")

		_ = pages.GetPageInput{ID: pageID}
		_ = pages.ResolvePermalinkInput{ID: pageID}
		_ = pages.FindByPathInput{RoutePath: routePath}
		_ = pages.LookupPagePathInput{Path: routePath}
		_ = pages.CreatePageInput{ParentID: &parentID, Slug: slug}
		_ = pages.UpdatePageInput{ID: pageID, Version: version, Slug: slug}
		_ = pages.DeletePageInput{ID: pageID, Version: version}
		_ = pages.MovePageInput{UserID: newFixtureUserID("user-1"), ID: pageID, Version: version, ParentID: parentID}
		_ = pages.ConvertPageInput{UserID: newFixtureUserID("user-1"), ID: pageID, Version: version}
		_ = pages.CopyPageInput{UserID: newFixtureUserID("user-1"), SourcePageID: pageID, TargetParentID: &parentID, Slug: slug}
		_ = pages.EnsurePathInput{UserID: newFixtureUserID("user-1"), TargetPath: routePath}
		_ = pages.SortPagesInput{ParentID: parentID, OrderedIDs: []tree.PageID{pageID}}
		_ = pages.SuggestSlugInput{ParentID: parentID, CurrentID: pageID}
		_ = pages.RefactorPreviewInput{PageID: pageID, Slug: slug, NewParentID: &parentID}
		_ = pages.RefactorApplyInput{UserID: newFixtureUserID("user-1"), Version: version, RefactorPreviewInput: pages.RefactorPreviewInput{PageID: pageID}}
	})

	ginkgo.It("reports stable validation codes for metadata input", func() {
		const whitespacePropertyField = "properties. leafwiki_custom"

		err := pages.ValidatePageMetadataInput(
			[]string{" tag ", "unique", "UNIQUE"},
			map[string]string{
				" leafwiki_custom": "reserved",
				"leafwiki_custom":  "reserved prefix",
				"tags":             "reserved",
				"":                 "empty",
			},
		)

		Expect(err).To(SatisfyAll(
			HavePageValidationFieldError("tags[0]", pages.FieldCodePageTagWhitespace, pages.MessageIDPageTagWhitespace),
			HavePageValidationFieldError("tags[2]", pages.FieldCodePageTagDuplicate, pages.MessageIDPageTagDuplicate),
			HavePageValidationFieldError(whitespacePropertyField, pages.FieldCodePagePropertyKeyWhitespace, pages.MessageIDPagePropertyKeyWhitespace),
			HavePageValidationFieldError("properties.leafwiki_custom", pages.FieldCodePagePropertyKeyReserved, pages.MessageIDPagePropertyKeyReservedPrefix),
			HavePageValidationFieldError("properties.tags", pages.FieldCodePagePropertyKeyReserved, pages.MessageIDPagePropertyKeyReserved),
			HavePageValidationFieldError("properties.", pages.FieldCodePagePropertyKeyRequired, pages.MessageIDPagePropertyKeyRequired),
		))
	})

	ginkgo.It("rejects creation without a page kind", func() {
		deps := newTestDeps()
		uc := pages.NewCreatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())

		_, err := uc.Execute(context.Background(), pages.CreatePageInput{
			UserID: newFixtureUserID("user1"),
			Title:  "Test",
			Slug:   newFixtureSlug("test"),
			Kind:   nil,
		})
		Expect(err).To(HavePageValidationFieldError("kind", pages.FieldCodePageKindRequired, pages.MessageIDPageKindRequired))
	})

	ginkgo.It("creates a section", func() {
		deps := newTestDeps()
		uc := pages.NewCreatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())

		out, err := uc.Execute(context.Background(), pages.CreatePageInput{
			UserID: newFixtureUserID("user1"),
			Title:  "Section",
			Slug:   newFixtureSlug("section"),
			Kind:   sectionKind(),
		})
		Expect(err).To(Succeed())
		Expect(out.Page.Kind).To(Equal(tree.NodeKindSection))
	})

	// ─────────────────────────────────────────────────────────────────────────────
	// UpdatePageUseCase
	// ─────────────────────────────────────────────────────────────────────────────

	ginkgo.It("updates page title slug and content", func() {
		deps := newTestDeps()
		createUC := pages.NewCreatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())
		updateUC := pages.NewUpdatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())

		created, err := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: newFixtureUserID("user1"), Title: "Old Title", Slug: newFixtureSlug("old-title"), Kind: pageKind(),
		})
		Expect(err).To(Succeed())

		content := "updated content"
		out, err := updateUC.Execute(context.Background(), pages.UpdatePageInput{
			UserID:  newFixtureUserID("user1"),
			ID:      pageID(created.Page.ID),
			Version: pageVersion(created.Page.Version()),
			Title:   "New Title",
			Slug:    newFixtureSlug("new-title"),
			Content: &content,
			Kind:    pageKind(),
		})
		Expect(err).To(Succeed())
		Expect(out.Page).To(SatisfyAll(
			HaveField("Title", Equal("New Title")),
			HaveField("Slug", Equal(slug("new-title"))),
		))
	})

	ginkgo.It("rejects stale page updates", func() {
		deps := newTestDeps()
		createUC := pages.NewCreatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())
		updateUC := pages.NewUpdatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())

		created, err := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: newFixtureUserID("user1"), Title: "Old Title", Slug: newFixtureSlug("old-title"), Kind: pageKind(),
		})
		Expect(err).To(Succeed())
		staleVersion := created.Page.Version()

		firstContent := "first update"
		updated, err := updateUC.Execute(context.Background(), pages.UpdatePageInput{
			UserID:  newFixtureUserID("user1"),
			ID:      pageID(created.Page.ID),
			Version: pageVersion(staleVersion),
			Title:   "New Title",
			Slug:    newFixtureSlug("new-title"),
			Content: &firstContent,
			Kind:    pageKind(),
		})
		Expect(err).To(Succeed())

		secondContent := "second update"
		_, err = updateUC.Execute(context.Background(), pages.UpdatePageInput{
			UserID:  newFixtureUserID("user2"),
			ID:      pageID(created.Page.ID),
			Version: pageVersion(staleVersion),
			Title:   updated.Page.Title,
			Slug:    slug(updated.Page.Slug),
			Content: &secondContent,
			Kind:    pageKind(),
		})
		Expect(err).To(MatchError(tree.ErrVersionConflict))
	})

	ginkgo.It("requires a real version for updates", func() {
		deps := newTestDeps()
		createUC := pages.NewCreatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())
		updateUC := pages.NewUpdatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())

		created, err := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: newFixtureUserID("user1"), Title: "Page", Slug: newFixtureSlug("page"), Kind: pageKind(),
		})
		Expect(err).To(Succeed())

		content := "new content"
		_, err = updateUC.Execute(context.Background(), pages.UpdatePageInput{
			UserID:  newFixtureUserID("user1"),
			ID:      pageID(created.Page.ID),
			Version: newFixtureRawPageVersion("\x00"),
			Title:   "Page",
			Slug:    newFixtureSlug("page"),
			Content: &content,
			Kind:    pageKind(),
		})
		Expect(err).To(MatchError(tree.ErrVersionRequired))
	})

	ginkgo.It("rejects updates without a title", func() {
		deps := newTestDeps()
		createUC := pages.NewCreatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())
		updateUC := pages.NewUpdatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())

		created, _ := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: newFixtureUserID("user1"), Title: "Page", Slug: newFixtureSlug("page"), Kind: pageKind(),
		})

		_, err := updateUC.Execute(context.Background(), pages.UpdatePageInput{
			UserID: newFixtureUserID("user1"), ID: pageID(created.Page.ID), Version: pageVersion(created.Page.Version()), Title: "", Slug: newFixtureSlug("page"), Kind: pageKind(),
		})
		Expect(err).To(HavePageValidationFieldError("title", pages.FieldCodePageTitleRequired, pages.MessageIDPageTitleRequired))
	})

	// ─────────────────────────────────────────────────────────────────────────────
	// DeletePageUseCase
	// ─────────────────────────────────────────────────────────────────────────────

	ginkgo.It("deletes a page", func() {
		deps := newTestDeps()
		createUC := pages.NewCreatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())
		deleteUC := pages.NewDeletePageUseCase(deps.tree, deps.assets, deps.orchestrator(), slog.Default())

		created, _ := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: newFixtureUserID("user1"), Title: "To Delete", Slug: newFixtureSlug("to-delete"), Kind: pageKind(),
		})

		if err := deleteUC.Execute(context.Background(), pages.DeletePageInput{
			UserID:    newFixtureUserID("user1"),
			ID:        pageID(created.Page.ID),
			Version:   pageVersion(created.Page.Version()),
			Recursive: false,
		}); err != nil {
			Expect(err).To(Succeed())
		}

		// Verify it is gone
		if _, err := deps.tree.GetPage(pageID(created.Page.ID)); !errors.Is(err, tree.ErrPageNotFound) {
			Expect(err).To(MatchError(tree.ErrPageNotFound))
		}
	})

	ginkgo.It("rejects root deletion", func() {
		deps := newTestDeps()
		deleteUC := pages.NewDeletePageUseCase(deps.tree, deps.assets, deps.orchestrator(), slog.Default())

		err := deleteUC.Execute(context.Background(), pages.DeletePageInput{
			UserID: newFixtureUserID("user1"), ID: newFixturePageID("root"), Recursive: false,
		})
		Expect(err).To(MatchPageLocalizedCode(pages.ErrCodePageRootOperation))
	})

	ginkgo.It("recursively deletes pages with children", func() {
		deps := newTestDeps()
		createUC := pages.NewCreatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())
		deleteUC := pages.NewDeletePageUseCase(deps.tree, deps.assets, deps.orchestrator(), slog.Default())

		parent, _ := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: newFixtureUserID("user1"), Title: "Parent", Slug: newFixtureSlug("parent"), Kind: pageKind(),
		})
		createUC.Execute(context.Background(), pages.CreatePageInput{ //nolint:errcheck
			UserID: newFixtureUserID("user1"), ParentID: pageIDPtr(parent.Page.ID), Title: "Child", Slug: newFixtureSlug("child"), Kind: pageKind(),
		})

		err := deleteUC.Execute(context.Background(), pages.DeletePageInput{
			UserID: newFixtureUserID("user1"), ID: pageID(parent.Page.ID), Version: pageVersion(parent.Page.Version()), Recursive: true,
		})
		Expect(err).To(Succeed())
	})

	// ─────────────────────────────────────────────────────────────────────────────
	// MovePageUseCase
	// ─────────────────────────────────────────────────────────────────────────────

	ginkgo.It("moves a page under a new parent", func() {
		deps := newTestDeps()
		createUC := pages.NewCreatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())
		moveUC := pages.NewMovePageUseCase(deps.tree, deps.orchestrator(), slog.Default())

		parent, _ := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: newFixtureUserID("user1"), Title: "Parent", Slug: newFixtureSlug("parent"), Kind: pageKind(),
		})
		child, _ := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: newFixtureUserID("user1"), Title: "Child", Slug: newFixtureSlug("child"), Kind: pageKind(),
		})

		if err := moveUC.Execute(context.Background(), pages.MovePageInput{
			UserID:   newFixtureUserID("user1"),
			ID:       pageID(child.Page.ID),
			Version:  pageVersion(child.Page.Version()),
			ParentID: pageID(parent.Page.ID),
		}); err != nil {
			Expect(err).To(Succeed())
		}

		moved, err := deps.tree.GetPage(pageID(child.Page.ID))
		Expect(err).To(Succeed())
		Expect(moved.Parent).To(gstruct.PointTo(HaveField("ID", Equal(parent.Page.ID))))
	})

	ginkgo.It("rejects stale page moves", func() {
		deps := newTestDeps()
		createUC := pages.NewCreatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())
		moveUC := pages.NewMovePageUseCase(deps.tree, deps.orchestrator(), slog.Default())

		parentA, err := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: newFixtureUserID("user1"), Title: "Parent A", Slug: newFixtureSlug("parent-a"), Kind: pageKind(),
		})
		Expect(err).To(Succeed())
		parentB, err := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: newFixtureUserID("user1"), Title: "Parent B", Slug: newFixtureSlug("parent-b"), Kind: pageKind(),
		})
		Expect(err).To(Succeed())
		parentC, err := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: newFixtureUserID("user1"), Title: "Parent C", Slug: newFixtureSlug("parent-c"), Kind: pageKind(),
		})
		Expect(err).To(Succeed())
		child, err := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID:   newFixtureUserID("user1"),
			ParentID: pageIDPtr(parentA.Page.ID),
			Title:    "Child",
			Slug:     newFixtureSlug("child"),
			Kind:     pageKind(),
		})
		Expect(err).To(Succeed())
		staleVersion := child.Page.Version()

		if err := moveUC.Execute(context.Background(), pages.MovePageInput{
			UserID:   newFixtureUserID("user1"),
			ID:       pageID(child.Page.ID),
			Version:  pageVersion(staleVersion),
			ParentID: pageID(parentB.Page.ID),
		}); err != nil {
			Expect(err).To(Succeed())
		}

		err = moveUC.Execute(context.Background(), pages.MovePageInput{
			UserID:   newFixtureUserID("user2"),
			ID:       pageID(child.Page.ID),
			Version:  pageVersion(staleVersion),
			ParentID: pageID(parentC.Page.ID),
		})
		Expect(err).To(MatchError(tree.ErrVersionConflict))
	})

	ginkgo.It("rejects moving the root page", func() {
		deps := newTestDeps()
		moveUC := pages.NewMovePageUseCase(deps.tree, deps.orchestrator(), slog.Default())

		err := moveUC.Execute(context.Background(), pages.MovePageInput{
			UserID: newFixtureUserID("user1"), ID: newFixturePageID("root"), Version: newFixturePageVersion("root-version"), ParentID: newFixturePageID("root"),
		})
		Expect(err).To(MatchPageLocalizedCode(pages.ErrCodePageRootOperation))
	})

	// ─────────────────────────────────────────────────────────────────────────────
	// ConvertPageUseCase
	// ─────────────────────────────────────────────────────────────────────────────

})
