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
	"github.com/perber/wiki/internal/search"
	"github.com/perber/wiki/internal/test_utils"
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

func semanticUserID(id string) tree.UserID {
	return tree.UserIDFromString(id)
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

var _ = ginkgo.Describe("page use case behavior", func() {
	ginkgo.It("creates a root page", func() {
		deps := newTestDeps()
		uc := pages.NewCreatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())

		out, err := uc.Execute(context.Background(), pages.CreatePageInput{
			UserID: "user1",
			Title:  "Home",
			Slug:   "home",
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
			UserID: "user1",
			Title:  "Docs",
			Slug:   "docs",
			Kind:   pageKind(),
		})
		Expect(err).To(Succeed())

		child, err := uc.Execute(context.Background(), pages.CreatePageInput{
			UserID:   "user1",
			ParentID: pageIDPtr(parent.Page.ID),
			Title:    "Reference",
			Slug:     "reference",
			Kind:     pageKind(),
		})
		Expect(err).To(Succeed())
		Expect(child.Page.Parent).To(gstruct.PointTo(HaveField("ID", Equal(parent.Page.ID))))
	})

	ginkgo.It("rejects creation without a title", func() {
		deps := newTestDeps()
		uc := pages.NewCreatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())

		_, err := uc.Execute(context.Background(), pages.CreatePageInput{
			UserID: "user1",
			Title:  "",
			Slug:   "home",
			Kind:   pageKind(),
		})
		Expect(err).To(HavePageValidationFieldError("title", pages.FieldCodePageTitleRequired, pages.MessageIDPageTitleRequired))
	})

	ginkgo.It("rejects reserved slugs during creation", func() {
		deps := newTestDeps()
		uc := pages.NewCreatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())

		_, err := uc.Execute(context.Background(), pages.CreatePageInput{
			UserID: "user1",
			Title:  "Reserved",
			Slug:   "e", // too short / reserved
			Kind:   pageKind(),
		})
		Expect(err).To(HavePageValidationFieldError("slug", pages.FieldCodePageSlugInvalid, pages.MessageIDPageSlugInvalid))
	})

	ginkgo.It("keeps page use-case boundaries typed", func() {
		pageID := newFixturePageID("page-1")
		parentID := newFixturePageID("parent-1")
		version := newFixturePageVersion("version-1")
		slug := newFixtureSlug("page-slug")
		routePath := tree.RoutePath("docs/page")

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
			UserID: "user1",
			Title:  "Test",
			Slug:   "test",
			Kind:   nil,
		})
		Expect(err).To(HavePageValidationFieldError("kind", pages.FieldCodePageKindRequired, pages.MessageIDPageKindRequired))
	})

	ginkgo.It("creates a section", func() {
		deps := newTestDeps()
		uc := pages.NewCreatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())

		out, err := uc.Execute(context.Background(), pages.CreatePageInput{
			UserID: "user1",
			Title:  "Section",
			Slug:   "section",
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
			UserID: "user1", Title: "Old Title", Slug: "old-title", Kind: pageKind(),
		})
		Expect(err).To(Succeed())

		content := "updated content"
		out, err := updateUC.Execute(context.Background(), pages.UpdatePageInput{
			UserID:  "user1",
			ID:      pageID(created.Page.ID),
			Version: pageVersion(created.Page.Version()),
			Title:   "New Title",
			Slug:    "new-title",
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
			UserID: "user1", Title: "Old Title", Slug: "old-title", Kind: pageKind(),
		})
		Expect(err).To(Succeed())
		staleVersion := created.Page.Version()

		firstContent := "first update"
		updated, err := updateUC.Execute(context.Background(), pages.UpdatePageInput{
			UserID:  "user1",
			ID:      pageID(created.Page.ID),
			Version: pageVersion(staleVersion),
			Title:   "New Title",
			Slug:    "new-title",
			Content: &firstContent,
			Kind:    pageKind(),
		})
		Expect(err).To(Succeed())

		secondContent := "second update"
		_, err = updateUC.Execute(context.Background(), pages.UpdatePageInput{
			UserID:  "user2",
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
			UserID: "user1", Title: "Page", Slug: "page", Kind: pageKind(),
		})
		Expect(err).To(Succeed())

		content := "new content"
		_, err = updateUC.Execute(context.Background(), pages.UpdatePageInput{
			UserID:  "user1",
			ID:      pageID(created.Page.ID),
			Version: newFixturePageVersion("\x00"),
			Title:   "Page",
			Slug:    "page",
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
			UserID: "user1", Title: "Page", Slug: "page", Kind: pageKind(),
		})

		_, err := updateUC.Execute(context.Background(), pages.UpdatePageInput{
			UserID: "user1", ID: pageID(created.Page.ID), Version: pageVersion(created.Page.Version()), Title: "", Slug: "page", Kind: pageKind(),
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
			UserID: "user1", Title: "To Delete", Slug: "to-delete", Kind: pageKind(),
		})

		if err := deleteUC.Execute(context.Background(), pages.DeletePageInput{
			UserID:    "user1",
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
			UserID: "user1", ID: "root", Recursive: false,
		})
		Expect(err).To(MatchPageLocalizedCode(pages.ErrCodePageRootOperation))
	})

	ginkgo.It("recursively deletes pages with children", func() {
		deps := newTestDeps()
		createUC := pages.NewCreatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())
		deleteUC := pages.NewDeletePageUseCase(deps.tree, deps.assets, deps.orchestrator(), slog.Default())

		parent, _ := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: "user1", Title: "Parent", Slug: "parent", Kind: pageKind(),
		})
		createUC.Execute(context.Background(), pages.CreatePageInput{ //nolint:errcheck
			UserID: "user1", ParentID: pageIDPtr(parent.Page.ID), Title: "Child", Slug: "child", Kind: pageKind(),
		})

		err := deleteUC.Execute(context.Background(), pages.DeletePageInput{
			UserID: "user1", ID: pageID(parent.Page.ID), Version: pageVersion(parent.Page.Version()), Recursive: true,
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
			UserID: "user1", Title: "Parent", Slug: "parent", Kind: pageKind(),
		})
		child, _ := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: "user1", Title: "Child", Slug: "child", Kind: pageKind(),
		})

		if err := moveUC.Execute(context.Background(), pages.MovePageInput{
			UserID:   "user1",
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
			UserID: "user1", Title: "Parent A", Slug: "parent-a", Kind: pageKind(),
		})
		Expect(err).To(Succeed())
		parentB, err := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: "user1", Title: "Parent B", Slug: "parent-b", Kind: pageKind(),
		})
		Expect(err).To(Succeed())
		parentC, err := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: "user1", Title: "Parent C", Slug: "parent-c", Kind: pageKind(),
		})
		Expect(err).To(Succeed())
		child, err := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID:   "user1",
			ParentID: pageIDPtr(parentA.Page.ID),
			Title:    "Child",
			Slug:     "child",
			Kind:     pageKind(),
		})
		Expect(err).To(Succeed())
		staleVersion := child.Page.Version()

		if err := moveUC.Execute(context.Background(), pages.MovePageInput{
			UserID:   "user1",
			ID:       pageID(child.Page.ID),
			Version:  pageVersion(staleVersion),
			ParentID: pageID(parentB.Page.ID),
		}); err != nil {
			Expect(err).To(Succeed())
		}

		err = moveUC.Execute(context.Background(), pages.MovePageInput{
			UserID:   "user2",
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
			UserID: "user1", ID: "root", Version: "root-version", ParentID: "root",
		})
		Expect(err).To(MatchPageLocalizedCode(pages.ErrCodePageRootOperation))
	})

	// ─────────────────────────────────────────────────────────────────────────────
	// ConvertPageUseCase
	// ─────────────────────────────────────────────────────────────────────────────

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

	ginkgo.It("rewrites incoming links during rename apply", func() {
		deps := newTestDeps()
		createUC := pages.NewCreatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())
		updateUC := pages.NewUpdatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())
		applyUC := pages.NewApplyPageRefactorUseCase(deps.tree, deps.slug, deps.links, slog.Default())

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

		updated, err := applyUC.Execute(context.Background(), pages.RefactorApplyInput{
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
			HaveField("Outgoings", HaveExactElements(HaveHealthyOutgoing("/target-renamed", target.Page.ID))),
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

		if _, err := applyUC.Execute(context.Background(), pages.RefactorApplyInput{
			UserID:  "mcp-user",
			Source:  pagesave.PageMutationSourceMCP,
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
			UserID: "system", Title: "Target", Slug: "target", Kind: pageKind(),
		})
		ref, _ := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: "system", Title: "Ref", Slug: "ref", Kind: pageKind(),
		})
		content := "refactorsearchtoken [Target](/target.md)"
		if _, err := updateUC.Execute(context.Background(), pages.UpdatePageInput{
			UserID: "system", ID: pageID(ref.Page.ID), Version: pageVersion(ref.Page.Version()), Title: ref.Page.Title, Slug: slug(ref.Page.Slug), Content: &content, Kind: pageKind(),
		}); err != nil {
			Expect(err).To(Succeed())
		}

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
			UserID: "system", Title: "Target", Slug: "target", Kind: pageKind(),
		})
		ref, _ := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: "system", Title: "Ref", Slug: "ref", Kind: pageKind(),
		})
		refContent := "[Target](/target)"
		if _, err := updateUC.Execute(context.Background(), pages.UpdatePageInput{
			UserID: "system", ID: pageID(ref.Page.ID), Version: pageVersion(ref.Page.Version()), Title: ref.Page.Title, Slug: slug(ref.Page.Slug), Content: &refContent, Kind: pageKind(),
		}); err != nil {
			Expect(err).To(Succeed())
		}

		staleVersion := target.Page.Version()
		newerTargetContent := "newer target content"
		if _, err := updateUC.Execute(context.Background(), pages.UpdatePageInput{
			UserID: "system", ID: pageID(target.Page.ID), Version: pageVersion(staleVersion), Title: target.Page.Title, Slug: slug(target.Page.Slug), Content: &newerTargetContent, Kind: pageKind(),
		}); err != nil {
			Expect(err).To(Succeed())
		}

		_, err := applyUC.Execute(context.Background(), pages.RefactorApplyInput{
			UserID:  "system",
			Version: pageVersion(staleVersion),
			RefactorPreviewInput: pages.RefactorPreviewInput{
				PageID: pageID(target.Page.ID),
				Kind:   pages.RefactorKindRename,
				Title:  "Target Renamed",
				Slug:   "target-renamed",
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
			UserID: "system", Title: "Target", Slug: "target", Kind: pageKind(),
		})
		if _, err := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: "system", Title: "Existing", Slug: "existing", Kind: pageKind(),
		}); err != nil {
			Expect(err).To(Succeed())
		}
		ref, _ := createUC.Execute(context.Background(), pages.CreatePageInput{
			UserID: "system", Title: "Ref", Slug: "ref", Kind: pageKind(),
		})
		refContent := "[Target](/target)"
		if _, err := updateUC.Execute(context.Background(), pages.UpdatePageInput{
			UserID: "system", ID: pageID(ref.Page.ID), Version: pageVersion(ref.Page.Version()), Title: ref.Page.Title, Slug: slug(ref.Page.Slug), Content: &refContent, Kind: pageKind(),
		}); err != nil {
			Expect(err).To(Succeed())
		}

		_, err := applyUC.Execute(context.Background(), pages.RefactorApplyInput{
			UserID:  "system",
			Version: pageVersion(target.Page.Version()),
			RefactorPreviewInput: pages.RefactorPreviewInput{
				PageID: pageID(target.Page.ID),
				Kind:   pages.RefactorKindRename,
				Title:  "Target",
				Slug:   "existing",
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
			UserID: "system", Title: "Target", Slug: "target", Kind: pageKind(),
		})

		preview, err := previewUC.Execute(context.Background(), pages.RefactorPreviewInput{
			PageID: pageID(page.Page.ID),
			Kind:   pages.RefactorKindRename,
			Title:  page.Page.Title,
			Slug:   "target-renamed",
		})
		Expect(err).To(Succeed())
		Expect(preview.WarningDetails).NotTo(BeNil())
		Expect(preview.WarningDetails).To(BeEmpty())
		for _, affected := range preview.AffectedPages {
			Expect(affected.WarningDetails).NotTo(BeNil())
			Expect(affected.MatchedPaths).NotTo(BeNil())
		}
	})

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
