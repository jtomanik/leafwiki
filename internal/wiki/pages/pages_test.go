package pages_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"testing"

	"github.com/perber/wiki/internal/core/assets"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
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

func newTestDeps(t *testing.T) *testDeps {
	t.Helper()
	storageDir := t.TempDir()

	treeService := tree.NewTreeService(storageDir)
	if err := treeService.LoadTree(); err != nil {
		t.Fatalf("failed to load tree: %v", err)
	}

	slugService := tree.NewSlugService()
	assetService := assets.NewAssetService(storageDir, slugService)

	linksStore, err := links.NewLinksStore(storageDir)
	if err != nil {
		t.Fatalf("failed to create links store: %v", err)
	}
	linkService := links.NewLinkService(storageDir, treeService, linksStore)

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
	return tree.UserID(id)
}

func pageID[T ~string](id T) tree.PageID {
	return tree.NewPageIDUnchecked(id)
}

func pageIDPtr[T ~string](id T) *tree.PageID {
	typed := pageID(id)
	return &typed
}

func pageVersion[T ~string](version T) tree.PageVersion {
	return tree.NewPageVersionUnchecked(version)
}

func slug[T ~string](value T) tree.Slug {
	return tree.NewSlugUnchecked(value)
}

// ─────────────────────────────────────────────────────────────────────────────
// CreatePageUseCase
// ─────────────────────────────────────────────────────────────────────────────

func TestCreatePageUseCase_HappyPath_Root(t *testing.T) {
	deps := newTestDeps(t)
	uc := pages.NewCreatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())

	out, err := uc.Execute(context.Background(), pages.CreatePageInput{
		UserID: "user1",
		Title:  "Home",
		Slug:   "home",
		Kind:   pageKind(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Page.Title != "Home" {
		t.Errorf("expected title 'Home', got %q", out.Page.Title)
	}
	if out.Page.Slug != "home" {
		t.Errorf("expected slug 'home', got %q", out.Page.Slug)
	}
}

func TestCreatePageUseCase_HappyPath_WithParent(t *testing.T) {
	deps := newTestDeps(t)
	uc := pages.NewCreatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())

	parent, err := uc.Execute(context.Background(), pages.CreatePageInput{
		UserID: "user1",
		Title:  "Docs",
		Slug:   "docs",
		Kind:   pageKind(),
	})
	if err != nil {
		t.Fatalf("unexpected error creating parent: %v", err)
	}

	child, err := uc.Execute(context.Background(), pages.CreatePageInput{
		UserID:   "user1",
		ParentID: pageIDPtr(parent.Page.ID),
		Title:    "Reference",
		Slug:     "reference",
		Kind:     pageKind(),
	})
	if err != nil {
		t.Fatalf("unexpected error creating child: %v", err)
	}
	if child.Page.Parent == nil || child.Page.Parent.ID != parent.Page.ID {
		t.Errorf("expected parent ID %q, got %v", parent.Page.ID, child.Page.Parent)
	}
}

func TestCreatePageUseCase_EmptyTitle_ReturnsValidationError(t *testing.T) {
	deps := newTestDeps(t)
	uc := pages.NewCreatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())

	_, err := uc.Execute(context.Background(), pages.CreatePageInput{
		UserID: "user1",
		Title:  "",
		Slug:   "home",
		Kind:   pageKind(),
	})
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}
	var ve *sharederrors.ValidationErrors
	if !errors.As(err, &ve) {
		t.Fatalf("expected ValidationErrors, got %T: %v", err, err)
	}
	assertFieldErrorCode(t, ve, "title", "page_title_required", "validation.page.title_required")
}

func TestCreatePageUseCase_ReservedSlug_ReturnsValidationError(t *testing.T) {
	deps := newTestDeps(t)
	uc := pages.NewCreatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())

	_, err := uc.Execute(context.Background(), pages.CreatePageInput{
		UserID: "user1",
		Title:  "Reserved",
		Slug:   "e", // too short / reserved
		Kind:   pageKind(),
	})
	if err == nil {
		t.Fatal("expected error for reserved slug, got nil")
	}
	var ve *sharederrors.ValidationErrors
	if !errors.As(err, &ve) {
		t.Fatalf("expected ValidationErrors, got %T: %v", err, err)
	}
	assertFieldErrorCode(t, ve, "slug", "page_slug_invalid", "validation.page.slug_invalid")
}

func TestPageUseCaseInputsUseSemanticTypesAtBoundary(t *testing.T) {
	t.Parallel()

	pageID := tree.NewPageIDUnchecked("page-1")
	parentID := tree.NewPageIDUnchecked("parent-1")
	version := tree.NewPageVersionUnchecked("version-1")
	slug := tree.NewSlugUnchecked("page-slug")
	routePath := tree.RoutePath("docs/page")

	_ = pages.GetPageInput{ID: pageID}
	_ = pages.ResolvePermalinkInput{ID: pageID}
	_ = pages.FindByPathInput{RoutePath: routePath}
	_ = pages.LookupPagePathInput{Path: routePath}
	_ = pages.CreatePageInput{ParentID: &parentID, Slug: slug}
	_ = pages.UpdatePageInput{ID: pageID, Version: version, Slug: slug}
	_ = pages.DeletePageInput{ID: pageID, Version: version}
	_ = pages.MovePageInput{UserID: tree.UserID("user-1"), ID: pageID, Version: version, ParentID: parentID}
	_ = pages.ConvertPageInput{UserID: tree.UserID("user-1"), ID: pageID, Version: version}
	_ = pages.CopyPageInput{UserID: tree.UserID("user-1"), SourcePageID: pageID, TargetParentID: &parentID, Slug: slug}
	_ = pages.EnsurePathInput{UserID: tree.UserID("user-1"), TargetPath: routePath}
	_ = pages.SortPagesInput{ParentID: parentID, OrderedIDs: []tree.PageID{pageID}}
	_ = pages.SuggestSlugInput{ParentID: parentID, CurrentID: pageID}
	_ = pages.RefactorPreviewInput{PageID: pageID, Slug: slug, NewParentID: &parentID}
	_ = pages.RefactorApplyInput{UserID: tree.UserID("user-1"), Version: version, RefactorPreviewInput: pages.RefactorPreviewInput{PageID: pageID}}
}

func TestValidatePageMetadataInputReportsStableCodes(t *testing.T) {
	t.Parallel()

	err := pages.ValidatePageMetadataInput(
		[]string{" tag ", "unique", "UNIQUE"},
		map[string]string{
			" leafwiki_custom": "reserved",
			"leafwiki_custom":  "reserved prefix",
			"tags":             "reserved",
			"":                 "empty",
		},
	)

	var ve *sharederrors.ValidationErrors
	if !errors.As(err, &ve) {
		t.Fatalf("expected ValidationErrors, got %T: %v", err, err)
	}
	assertFieldErrorCode(t, ve, "tags[0]", "page_tag_whitespace", "validation.page.tag_whitespace")
	assertFieldErrorCode(t, ve, "tags[2]", "page_tag_duplicate", "validation.page.tag_duplicate")
	assertFieldErrorCode(t, ve, "properties. leafwiki_custom", "page_property_key_whitespace", "validation.page.property_key_whitespace")
	assertFieldErrorCode(t, ve, "properties.leafwiki_custom", "page_property_key_reserved", "validation.page.property_key_reserved_prefix")
	assertFieldErrorCode(t, ve, "properties.tags", "page_property_key_reserved", "validation.page.property_key_reserved")
	assertFieldErrorCode(t, ve, "properties.", "page_property_key_required", "validation.page.property_key_required")
}

func assertFieldErrorCode(t *testing.T, ve *sharederrors.ValidationErrors, field string, code string, messageID string) {
	t.Helper()

	for _, err := range ve.Errors {
		if err.Field == field {
			if fmt.Sprintf("%s", err.Code) != code {
				t.Fatalf("%s code = %q, want %q", field, err.Code, code)
			}
			if fmt.Sprintf("%s", err.MessageID) != messageID {
				t.Fatalf("%s messageId = %q, want %q", field, err.MessageID, messageID)
			}
			return
		}
	}
	t.Fatalf("missing field error for %s in %#v", field, ve.Errors)
}

func TestCreatePageUseCase_NilKind_ReturnsValidationError(t *testing.T) {
	deps := newTestDeps(t)
	uc := pages.NewCreatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())

	_, err := uc.Execute(context.Background(), pages.CreatePageInput{
		UserID: "user1",
		Title:  "Test",
		Slug:   "test",
		Kind:   nil,
	})
	if err == nil {
		t.Fatal("expected error for nil kind, got nil")
	}
}

func TestCreatePageUseCase_Section_HappyPath(t *testing.T) {
	deps := newTestDeps(t)
	uc := pages.NewCreatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())

	out, err := uc.Execute(context.Background(), pages.CreatePageInput{
		UserID: "user1",
		Title:  "Section",
		Slug:   "section",
		Kind:   sectionKind(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Page.Kind != tree.NodeKindSection {
		t.Errorf("expected kind %q, got %q", tree.NodeKindSection, out.Page.Kind)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// UpdatePageUseCase
// ─────────────────────────────────────────────────────────────────────────────

func TestUpdatePageUseCase_HappyPath(t *testing.T) {
	deps := newTestDeps(t)
	createUC := pages.NewCreatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())
	updateUC := pages.NewUpdatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())

	created, err := createUC.Execute(context.Background(), pages.CreatePageInput{
		UserID: "user1", Title: "Old Title", Slug: "old-title", Kind: pageKind(),
	})
	if err != nil {
		t.Fatalf("unexpected error creating page: %v", err)
	}

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
	if err != nil {
		t.Fatalf("unexpected error updating page: %v", err)
	}
	if out.Page.Title != "New Title" {
		t.Errorf("expected title 'New Title', got %q", out.Page.Title)
	}
	if out.Page.Slug != "new-title" {
		t.Errorf("expected slug 'new-title', got %q", out.Page.Slug)
	}
}

func TestUpdatePageUseCase_VersionConflict_ReturnsVersionConflictError(t *testing.T) {
	deps := newTestDeps(t)
	createUC := pages.NewCreatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())
	updateUC := pages.NewUpdatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())

	created, err := createUC.Execute(context.Background(), pages.CreatePageInput{
		UserID: "user1", Title: "Old Title", Slug: "old-title", Kind: pageKind(),
	})
	if err != nil {
		t.Fatalf("unexpected error creating page: %v", err)
	}
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
	if err != nil {
		t.Fatalf("unexpected error applying first update: %v", err)
	}

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
	if err == nil {
		t.Fatal("expected version conflict, got nil")
	}
	if !errors.Is(err, tree.ErrVersionConflict) {
		t.Fatalf("expected tree.ErrVersionConflict, got %T: %v", err, err)
	}
}

func TestUpdatePageUseCase_VersionUncheckedSentinel_TreatedAsVersionRequired(t *testing.T) {
	deps := newTestDeps(t)
	createUC := pages.NewCreatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())
	updateUC := pages.NewUpdatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())

	created, err := createUC.Execute(context.Background(), pages.CreatePageInput{
		UserID: "user1", Title: "Page", Slug: "page", Kind: pageKind(),
	})
	if err != nil {
		t.Fatalf("unexpected error creating page: %v", err)
	}

	content := "new content"
	_, err = updateUC.Execute(context.Background(), pages.UpdatePageInput{
		UserID:  "user1",
		ID:      pageID(created.Page.ID),
		Version: tree.NewPageVersionUnchecked("\x00"),
		Title:   "Page",
		Slug:    "page",
		Content: &content,
		Kind:    pageKind(),
	})
	if !errors.Is(err, tree.ErrVersionRequired) {
		t.Fatalf("expected ErrVersionRequired when sending reserved version bypass value, got %v", err)
	}
}

func TestUpdatePageUseCase_EmptyTitle_ReturnsValidationError(t *testing.T) {
	deps := newTestDeps(t)
	createUC := pages.NewCreatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())
	updateUC := pages.NewUpdatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())

	created, _ := createUC.Execute(context.Background(), pages.CreatePageInput{
		UserID: "user1", Title: "Page", Slug: "page", Kind: pageKind(),
	})

	_, err := updateUC.Execute(context.Background(), pages.UpdatePageInput{
		UserID: "user1", ID: pageID(created.Page.ID), Version: pageVersion(created.Page.Version()), Title: "", Slug: "page", Kind: pageKind(),
	})
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// DeletePageUseCase
// ─────────────────────────────────────────────────────────────────────────────

func TestDeletePageUseCase_HappyPath(t *testing.T) {
	deps := newTestDeps(t)
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
		t.Fatalf("unexpected error deleting page: %v", err)
	}

	// Verify it is gone
	if _, err := deps.tree.GetPage(tree.NewPageIDUnchecked(created.Page.ID)); !errors.Is(err, tree.ErrPageNotFound) {
		t.Errorf("expected page-not-found after delete, got %v", err)
	}
}

func TestDeletePageUseCase_Root_ReturnsError(t *testing.T) {
	deps := newTestDeps(t)
	deleteUC := pages.NewDeletePageUseCase(deps.tree, deps.assets, deps.orchestrator(), slog.Default())

	err := deleteUC.Execute(context.Background(), pages.DeletePageInput{
		UserID: "user1", ID: "root", Recursive: false,
	})
	if err == nil {
		t.Fatal("expected error when deleting root, got nil")
	}
}

func TestDeletePageUseCase_WithChildren_Recursive(t *testing.T) {
	deps := newTestDeps(t)
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
	if err != nil {
		t.Fatalf("unexpected error on recursive delete: %v", err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// MovePageUseCase
// ─────────────────────────────────────────────────────────────────────────────

func TestMovePageUseCase_HappyPath(t *testing.T) {
	deps := newTestDeps(t)
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
		t.Fatalf("unexpected error moving page: %v", err)
	}

	moved, err := deps.tree.GetPage(tree.NewPageIDUnchecked(child.Page.ID))
	if err != nil {
		t.Fatalf("could not get moved page: %v", err)
	}
	if moved.Parent == nil || moved.Parent.ID != parent.Page.ID {
		t.Errorf("expected parent %q after move, got %v", parent.Page.ID, moved.Parent)
	}
}

func TestMovePageUseCase_VersionConflict_ReturnsVersionConflictError(t *testing.T) {
	deps := newTestDeps(t)
	createUC := pages.NewCreatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())
	moveUC := pages.NewMovePageUseCase(deps.tree, deps.orchestrator(), slog.Default())

	parentA, err := createUC.Execute(context.Background(), pages.CreatePageInput{
		UserID: "user1", Title: "Parent A", Slug: "parent-a", Kind: pageKind(),
	})
	if err != nil {
		t.Fatalf("unexpected error creating parent A: %v", err)
	}
	parentB, err := createUC.Execute(context.Background(), pages.CreatePageInput{
		UserID: "user1", Title: "Parent B", Slug: "parent-b", Kind: pageKind(),
	})
	if err != nil {
		t.Fatalf("unexpected error creating parent B: %v", err)
	}
	parentC, err := createUC.Execute(context.Background(), pages.CreatePageInput{
		UserID: "user1", Title: "Parent C", Slug: "parent-c", Kind: pageKind(),
	})
	if err != nil {
		t.Fatalf("unexpected error creating parent C: %v", err)
	}
	child, err := createUC.Execute(context.Background(), pages.CreatePageInput{
		UserID:   "user1",
		ParentID: pageIDPtr(parentA.Page.ID),
		Title:    "Child",
		Slug:     "child",
		Kind:     pageKind(),
	})
	if err != nil {
		t.Fatalf("unexpected error creating child: %v", err)
	}
	staleVersion := child.Page.Version()

	if err := moveUC.Execute(context.Background(), pages.MovePageInput{
		UserID:   "user1",
		ID:       pageID(child.Page.ID),
		Version:  pageVersion(staleVersion),
		ParentID: pageID(parentB.Page.ID),
	}); err != nil {
		t.Fatalf("unexpected error applying first move: %v", err)
	}

	err = moveUC.Execute(context.Background(), pages.MovePageInput{
		UserID:   "user2",
		ID:       pageID(child.Page.ID),
		Version:  pageVersion(staleVersion),
		ParentID: pageID(parentC.Page.ID),
	})
	if err == nil {
		t.Fatal("expected version conflict, got nil")
	}
	if !errors.Is(err, tree.ErrVersionConflict) {
		t.Fatalf("expected tree.ErrVersionConflict, got %T: %v", err, err)
	}
}

func TestMovePageUseCase_Root_ReturnsError(t *testing.T) {
	deps := newTestDeps(t)
	moveUC := pages.NewMovePageUseCase(deps.tree, deps.orchestrator(), slog.Default())

	err := moveUC.Execute(context.Background(), pages.MovePageInput{
		UserID: "user1", ID: "root", Version: "root-version", ParentID: "root",
	})
	if err == nil {
		t.Fatal("expected error when moving root, got nil")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ConvertPageUseCase
// ─────────────────────────────────────────────────────────────────────────────

func TestConvertPageUseCase_UsesOrchestratorWithMutationSource(t *testing.T) {
	deps := newTestDeps(t)
	createUC := pages.NewCreatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())
	capture := &captureEffect{}
	convertUC := pages.NewConvertPageUseCase(deps.tree, pagesave.NewPageSaveOrchestrator(capture), slog.Default())

	page, err := createUC.Execute(context.Background(), pages.CreatePageInput{
		UserID: "user1", Title: "Convert Me", Slug: "convert-me", Kind: pageKind(),
	})
	if err != nil {
		t.Fatalf("CreatePage failed: %v", err)
	}

	if err := convertUC.Execute(context.Background(), pages.ConvertPageInput{
		UserID:     "mcp-user",
		Source:     pagesave.PageMutationSourceMCP,
		ID:         pageID(page.Page.ID),
		Version:    pageVersion(page.Page.Version()),
		TargetKind: tree.NodeKindSection,
	}); err != nil {
		t.Fatalf("ConvertPage failed: %v", err)
	}

	if len(capture.events) != 1 {
		t.Fatalf("captured events = %#v, want one convert event", capture.events)
	}
	event := capture.events[0]
	if event.Operation != pagesave.PageOperationUpdate {
		t.Fatalf("event operation = %q, want update", event.Operation)
	}
	if event.UserID != "mcp-user" {
		t.Fatalf("event user = %q, want mcp-user", event.UserID)
	}
	if event.Source != pagesave.PageMutationSourceMCP {
		t.Fatalf("event source = %q, want mcp", event.Source)
	}
	if event.Before == nil || event.After == nil {
		t.Fatalf("event before/after missing: %#v", event)
	}
	if len(event.AffectedPages) != 1 || event.AffectedPages[0].ID != page.Page.ID {
		t.Fatalf("event affected pages = %#v, want converted page", event.AffectedPages)
	}
	if event.After.Kind != tree.NodeKindSection {
		t.Fatalf("event after kind = %q, want section", event.After.Kind)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// EnsurePathUseCase
// ─────────────────────────────────────────────────────────────────────────────

func TestEnsurePathUseCase_CreatesNewPath(t *testing.T) {
	deps := newTestDeps(t)
	uc := pages.NewEnsurePathUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())

	out, err := uc.Execute(context.Background(), pages.EnsurePathInput{
		UserID:      "user1",
		TargetPath:  "docs/reference",
		TargetTitle: "Reference",
		Kind:        pageKind(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Page == nil {
		t.Fatal("expected page in output, got nil")
	}
}

func TestEnsurePathUseCase_ExistingPath_ReturnsExistingPage(t *testing.T) {
	deps := newTestDeps(t)
	uc := pages.NewEnsurePathUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())

	out1, err := uc.Execute(context.Background(), pages.EnsurePathInput{
		UserID: "user1", TargetPath: "docs", TargetTitle: "Docs", Kind: pageKind(),
	})
	if err != nil {
		t.Fatalf("unexpected error on first create: %v", err)
	}

	out2, err := uc.Execute(context.Background(), pages.EnsurePathInput{
		UserID: "user1", TargetPath: "docs", TargetTitle: "Docs", Kind: pageKind(),
	})
	if err != nil {
		t.Fatalf("unexpected error on second ensure: %v", err)
	}
	if out1.Page.ID != out2.Page.ID {
		t.Errorf("expected same page ID, got %q vs %q", out1.Page.ID, out2.Page.ID)
	}
}

func TestEnsurePathUseCase_CreatesPageTwinWhenSectionRouteExists(t *testing.T) {
	deps := newTestDeps(t)
	createUC := pages.NewCreatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())
	ensureUC := pages.NewEnsurePathUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())

	section, err := createUC.Execute(context.Background(), pages.CreatePageInput{
		UserID: "user1",
		Title:  "Sync Section",
		Slug:   "sync",
		Kind:   sectionKind(),
	})
	if err != nil {
		t.Fatalf("create section failed: %v", err)
	}

	out, err := ensureUC.Execute(context.Background(), pages.EnsurePathInput{
		UserID:      "user1",
		TargetPath:  "sync",
		TargetTitle: "Sync Page",
		Kind:        pageKind(),
	})
	if err != nil {
		t.Fatalf("ensure page twin failed: %v", err)
	}
	if out.Page.ID == section.Page.ID {
		t.Fatalf("EnsurePath returned existing section %q instead of page twin", section.Page.ID)
	}
	if out.Page.Kind != tree.NodeKindPage {
		t.Fatalf("ensured page kind = %q, want page", out.Page.Kind)
	}

	sectionTwin, err := deps.tree.FindPageByRoutePathAndKind("sync", tree.NodeKindSection)
	if err != nil {
		t.Fatalf("find section twin failed: %v", err)
	}
	if sectionTwin.ID != section.Page.ID {
		t.Fatalf("section twin ID = %q, want %q", sectionTwin.ID, section.Page.ID)
	}
	pageTwin, err := deps.tree.FindPageByRoutePathAndKind("sync", tree.NodeKindPage)
	if err != nil {
		t.Fatalf("find page twin failed: %v", err)
	}
	if pageTwin.ID != out.Page.ID {
		t.Fatalf("page twin ID = %q, want %q", pageTwin.ID, out.Page.ID)
	}

	second, err := ensureUC.Execute(context.Background(), pages.EnsurePathInput{
		UserID:      "user1",
		TargetPath:  "sync",
		TargetTitle: "Ignored",
		Kind:        pageKind(),
	})
	if err != nil {
		t.Fatalf("second ensure page twin failed: %v", err)
	}
	if second.Page.ID != out.Page.ID {
		t.Fatalf("second ensure returned page %q, want existing page twin %q", second.Page.ID, out.Page.ID)
	}
}

func TestEnsurePathUseCase_CreatesSectionTwinWhenPageRouteExists(t *testing.T) {
	deps := newTestDeps(t)
	createUC := pages.NewCreatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())
	ensureUC := pages.NewEnsurePathUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())

	page, err := createUC.Execute(context.Background(), pages.CreatePageInput{
		UserID: "user1",
		Title:  "Sync Page",
		Slug:   "sync",
		Kind:   pageKind(),
	})
	if err != nil {
		t.Fatalf("create page failed: %v", err)
	}

	out, err := ensureUC.Execute(context.Background(), pages.EnsurePathInput{
		UserID:      "user1",
		TargetPath:  "sync",
		TargetTitle: "Sync Section",
		Kind:        sectionKind(),
	})
	if err != nil {
		t.Fatalf("ensure section twin failed: %v", err)
	}
	if out.Page.ID == page.Page.ID {
		t.Fatalf("EnsurePath returned existing page %q instead of section twin", page.Page.ID)
	}
	if out.Page.Kind != tree.NodeKindSection {
		t.Fatalf("ensured page kind = %q, want section", out.Page.Kind)
	}

	pageTwin, err := deps.tree.FindPageByRoutePathAndKind("sync", tree.NodeKindPage)
	if err != nil {
		t.Fatalf("find page twin failed: %v", err)
	}
	if pageTwin.ID != page.Page.ID {
		t.Fatalf("page twin ID = %q, want %q", pageTwin.ID, page.Page.ID)
	}
	sectionTwin, err := deps.tree.FindPageByRoutePathAndKind("sync", tree.NodeKindSection)
	if err != nil {
		t.Fatalf("find section twin failed: %v", err)
	}
	if sectionTwin.ID != out.Page.ID {
		t.Fatalf("section twin ID = %q, want %q", sectionTwin.ID, out.Page.ID)
	}

	second, err := ensureUC.Execute(context.Background(), pages.EnsurePathInput{
		UserID:      "user1",
		TargetPath:  "sync",
		TargetTitle: "Ignored",
		Kind:        sectionKind(),
	})
	if err != nil {
		t.Fatalf("second ensure section twin failed: %v", err)
	}
	if second.Page.ID != out.Page.ID {
		t.Fatalf("second ensure returned section %q, want existing section twin %q", second.Page.ID, out.Page.ID)
	}
}

func TestEnsurePathUseCase_EmptyPath_ReturnsValidationError(t *testing.T) {
	deps := newTestDeps(t)
	uc := pages.NewEnsurePathUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())

	_, err := uc.Execute(context.Background(), pages.EnsurePathInput{
		UserID: "user1", TargetPath: "", TargetTitle: "Title", Kind: pageKind(),
	})
	if err == nil {
		t.Fatal("expected validation error for empty path, got nil")
	}
}

func TestEnsurePathUseCase_InvalidRoutePath_ReturnsValidationError(t *testing.T) {
	deps := newTestDeps(t)
	uc := pages.NewEnsurePathUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())

	_, err := uc.Execute(context.Background(), pages.EnsurePathInput{
		UserID: "user1", TargetPath: tree.RoutePath("docs//guide"), TargetTitle: "Guide", Kind: pageKind(),
	})
	if err == nil {
		t.Fatal("expected validation error for invalid path, got nil")
	}
	var ve *sharederrors.ValidationErrors
	if !errors.As(err, &ve) {
		t.Fatalf("expected ValidationErrors, got %T: %v", err, err)
	}
	assertFieldErrorCode(t, ve, "path", "page_path_invalid", "validation.page.path_invalid")
}

// ─────────────────────────────────────────────────────────────────────────────
// GetPageUseCase
// ─────────────────────────────────────────────────────────────────────────────

func TestGetPageUseCase_HappyPath(t *testing.T) {
	deps := newTestDeps(t)
	createUC := pages.NewCreatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())
	getUC := pages.NewGetPageUseCase(deps.tree)

	created, _ := createUC.Execute(context.Background(), pages.CreatePageInput{
		UserID: "user1", Title: "Home", Slug: "home", Kind: pageKind(),
	})

	out, err := getUC.Execute(context.Background(), pages.GetPageInput{ID: pageID(created.Page.ID)})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Page.ID != created.Page.ID {
		t.Errorf("expected ID %q, got %q", created.Page.ID, out.Page.ID)
	}
}

func TestGetPageUseCase_NotFound_ReturnsError(t *testing.T) {
	deps := newTestDeps(t)
	getUC := pages.NewGetPageUseCase(deps.tree)

	_, err := getUC.Execute(context.Background(), pages.GetPageInput{ID: "nonexistent"})
	if err == nil {
		t.Fatal("expected error for non-existent page, got nil")
	}
}

func TestCreatePageUseCase_ReservedHistorySlug_ReturnsValidationError(t *testing.T) {
	deps := newTestDeps(t)
	uc := pages.NewCreatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())

	_, err := uc.Execute(context.Background(), pages.CreatePageInput{
		UserID: "user1",
		Title:  "Reserved",
		Slug:   "history",
		Kind:   pageKind(),
	})
	if err == nil {
		t.Fatal("expected error for reserved history slug, got nil")
	}
}

func TestCreatePageUseCase_PageExists_ReturnsError(t *testing.T) {
	deps := newTestDeps(t)
	uc := pages.NewCreatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())

	if _, err := uc.Execute(context.Background(), pages.CreatePageInput{
		UserID: "user1", Title: "Duplicate", Slug: "duplicate", Kind: pageKind(),
	}); err != nil {
		t.Fatalf("unexpected error creating initial page: %v", err)
	}

	_, err := uc.Execute(context.Background(), pages.CreatePageInput{
		UserID: "user1", Title: "Duplicate", Slug: "duplicate", Kind: pageKind(),
	})
	if err == nil {
		t.Fatal("expected duplicate page error, got nil")
	}
}

func TestCreatePageUseCase_InvalidParent_ReturnsError(t *testing.T) {
	deps := newTestDeps(t)
	uc := pages.NewCreatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())
	invalidID := "not-real"

	_, err := uc.Execute(context.Background(), pages.CreatePageInput{
		UserID: "user1", ParentID: pageIDPtr(invalidID), Title: "Broken", Slug: "broken", Kind: pageKind(),
	})
	if err == nil {
		t.Fatal("expected invalid parent error, got nil")
	}
}

func TestCreatePageUseCase_RejectsCaseInsensitiveSlugConflict(t *testing.T) {
	deps := newTestDeps(t)
	uc := pages.NewCreatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())

	if _, err := uc.Execute(context.Background(), pages.CreatePageInput{
		UserID: "user1", Title: "Upper", Slug: "ABCD-efg", Kind: pageKind(),
	}); err != nil {
		t.Fatalf("unexpected error creating initial page: %v", err)
	}

	_, err := uc.Execute(context.Background(), pages.CreatePageInput{
		UserID: "user1", Title: "Lower", Slug: "abcd-efg", Kind: pageKind(),
	})
	if err == nil {
		t.Fatal("expected conflict for case-insensitive duplicate slug")
	}
}

func TestUpdatePageUseCase_AllowsUppercaseSlug(t *testing.T) {
	deps := newTestDeps(t)
	createUC := pages.NewCreatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())
	updateUC := pages.NewUpdatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())

	created, err := createUC.Execute(context.Background(), pages.CreatePageInput{
		UserID: "user1", Title: "Original", Slug: "original", Kind: pageKind(),
	})
	if err != nil {
		t.Fatalf("unexpected error creating page: %v", err)
	}

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
	if err != nil {
		t.Fatalf("expected uppercase slug update to succeed, got %v", err)
	}
	if out.Page.Slug != "ABCD-efg" {
		t.Fatalf("expected slug to be preserved, got %q", out.Page.Slug)
	}
}

func TestDeletePageUseCase_EmptyID_ReturnsError(t *testing.T) {
	deps := newTestDeps(t)
	deleteUC := pages.NewDeletePageUseCase(deps.tree, deps.assets, deps.orchestrator(), slog.Default())

	err := deleteUC.Execute(context.Background(), pages.DeletePageInput{
		UserID: "user1", ID: "", Recursive: false,
	})
	if err == nil {
		t.Fatal("expected error when deleting empty page ID, got nil")
	}
}

func TestFindByPathUseCase_HappyPath(t *testing.T) {
	deps := newTestDeps(t)
	createUC := pages.NewCreatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())
	findUC := pages.NewFindByPathUseCase(deps.tree)

	if _, err := createUC.Execute(context.Background(), pages.CreatePageInput{
		UserID: "user1", Title: "Company", Slug: "company", Kind: pageKind(),
	}); err != nil {
		t.Fatalf("unexpected error creating page: %v", err)
	}

	out, err := findUC.Execute(context.Background(), pages.FindByPathInput{RoutePath: "company"})
	if err != nil {
		t.Fatalf("unexpected error finding page: %v", err)
	}
	if out.Page.Slug != "company" {
		t.Errorf("expected slug 'company', got %q", out.Page.Slug)
	}
}

func TestFindByPathUseCase_NotFound_ReturnsError(t *testing.T) {
	deps := newTestDeps(t)
	findUC := pages.NewFindByPathUseCase(deps.tree)

	_, err := findUC.Execute(context.Background(), pages.FindByPathInput{RoutePath: "does/not/exist"})
	if err == nil {
		t.Fatal("expected error for invalid path, got nil")
	}
}

func TestLookupPagePathUseCase_InvalidRoutePath_ReturnsValidationError(t *testing.T) {
	deps := newTestDeps(t)
	lookupUC := pages.NewLookupPagePathUseCase(deps.tree)

	_, err := lookupUC.Execute(context.Background(), pages.LookupPagePathInput{
		Path: tree.RoutePath("docs//guide"),
	})
	if err == nil {
		t.Fatal("expected validation error for invalid route path, got nil")
	}
	var ve *sharederrors.ValidationErrors
	if !errors.As(err, &ve) {
		t.Fatalf("expected ValidationErrors, got %T: %v", err, err)
	}
	assertFieldErrorCode(t, ve, "path", "page_path_invalid", "validation.page.path_invalid")
}

func TestSortPagesUseCase_HappyPath(t *testing.T) {
	deps := newTestDeps(t)
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
		t.Fatalf("unexpected error sorting pages: %v", err)
	}

	sortedParent, err := deps.tree.GetPage(tree.NewPageIDUnchecked(parent.Page.ID))
	if err != nil {
		t.Fatalf("failed to reload parent: %v", err)
	}
	if len(sortedParent.Children) != 2 {
		t.Fatalf("expected 2 children, got %d", len(sortedParent.Children))
	}
	if sortedParent.Children[0].ID != child2.Page.ID || sortedParent.Children[1].ID != child1.Page.ID {
		t.Errorf("expected order [%s, %s], got [%s, %s]", child2.Page.ID, child1.Page.ID, sortedParent.Children[0].ID, sortedParent.Children[1].ID)
	}
}

func TestSuggestSlugUseCase_Unique(t *testing.T) {
	deps := newTestDeps(t)
	uc := pages.NewSuggestSlugUseCase(deps.tree, deps.slug)

	out, err := uc.Execute(context.Background(), pages.SuggestSlugInput{
		ParentID: "root",
		Title:    "My Page",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Slug != "my-page" {
		t.Errorf("expected 'my-page', got %q", out.Slug)
	}
}

func TestSuggestSlugUseCase_Conflict(t *testing.T) {
	deps := newTestDeps(t)
	createUC := pages.NewCreatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())
	uc := pages.NewSuggestSlugUseCase(deps.tree, deps.slug)

	if _, err := createUC.Execute(context.Background(), pages.CreatePageInput{
		UserID: "user1", Title: "My Page", Slug: "my-page", Kind: pageKind(),
	}); err != nil {
		t.Fatalf("unexpected error creating page: %v", err)
	}

	out, err := uc.Execute(context.Background(), pages.SuggestSlugInput{
		ParentID: pageID(deps.tree.GetTree().ID),
		Title:    "My Page",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Slug != "my-page-1" {
		t.Errorf("expected 'my-page-1', got %q", out.Slug)
	}
}

func TestSuggestSlugUseCase_DeepHierarchy(t *testing.T) {
	deps := newTestDeps(t)
	createUC := pages.NewCreatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())
	uc := pages.NewSuggestSlugUseCase(deps.tree, deps.slug)

	arch, err := createUC.Execute(context.Background(), pages.CreatePageInput{
		UserID: "user1", Title: "Architecture", Slug: "architecture", Kind: pageKind(),
	})
	if err != nil {
		t.Fatalf("unexpected error creating architecture: %v", err)
	}
	backend, err := createUC.Execute(context.Background(), pages.CreatePageInput{
		UserID: "user1", ParentID: pageIDPtr(arch.Page.ID), Title: "Backend", Slug: "backend", Kind: pageKind(),
	})
	if err != nil {
		t.Fatalf("unexpected error creating backend: %v", err)
	}

	out, err := uc.Execute(context.Background(), pages.SuggestSlugInput{
		ParentID: pageID(backend.Page.ID),
		Title:    "Data Layer",
	})
	if err != nil {
		t.Fatalf("unexpected error suggesting slug: %v", err)
	}
	if out.Slug != "data-layer" {
		t.Errorf("expected 'data-layer', got %q", out.Slug)
	}

	if _, err := createUC.Execute(context.Background(), pages.CreatePageInput{
		UserID: "user1", ParentID: pageIDPtr(backend.Page.ID), Title: "Data Layer", Slug: "data-layer", Kind: pageKind(),
	}); err != nil {
		t.Fatalf("unexpected error creating duplicate title page: %v", err)
	}

	out2, err := uc.Execute(context.Background(), pages.SuggestSlugInput{
		ParentID: pageID(backend.Page.ID),
		Title:    "Data Layer",
	})
	if err != nil {
		t.Fatalf("unexpected error suggesting second slug: %v", err)
	}
	if out2.Slug != "data-layer-1" {
		t.Errorf("expected 'data-layer-1', got %q", out2.Slug)
	}
}

func TestCopyPageUseCase_HappyPath(t *testing.T) {
	deps := newTestDeps(t)
	createUC := pages.NewCreatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())
	copyUC := pages.NewCopyPageUseCase(deps.tree, deps.slug, deps.orchestrator(), deps.assets, slog.Default())

	original, _ := createUC.Execute(context.Background(), pages.CreatePageInput{
		UserID: "user1", Title: "Original", Slug: "original", Kind: pageKind(),
	})

	out, err := copyUC.Execute(context.Background(), pages.CopyPageInput{
		UserID: "user1", SourcePageID: pageID(original.Page.ID), Title: "Copy of Original", Slug: "copy-of-original",
	})
	if err != nil {
		t.Fatalf("unexpected error copying page: %v", err)
	}
	if out.Page.Title != "Copy of Original" {
		t.Errorf("expected title 'Copy of Original', got %q", out.Page.Title)
	}
	if out.Page.Slug != "copy-of-original" {
		t.Errorf("expected slug 'copy-of-original', got %q", out.Page.Slug)
	}
	if out.Page.ID == original.Page.ID {
		t.Error("expected copied page to have a different ID")
	}
}

func TestCopyPageUseCase_WithParent(t *testing.T) {
	deps := newTestDeps(t)
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
	if err != nil {
		t.Fatalf("unexpected error copying page: %v", err)
	}
	if out.Page.Parent == nil || out.Page.Parent.ID != parent.Page.ID {
		t.Errorf("expected parent ID %q, got %v", parent.Page.ID, out.Page.Parent)
	}
}

func TestCopyPageUseCase_NonExistentSource_ReturnsError(t *testing.T) {
	deps := newTestDeps(t)
	copyUC := pages.NewCopyPageUseCase(deps.tree, deps.slug, deps.orchestrator(), deps.assets, slog.Default())

	_, err := copyUC.Execute(context.Background(), pages.CopyPageInput{
		UserID: "user1", SourcePageID: "non-existent-id", Title: "Copy", Slug: "copy",
	})
	if err == nil {
		t.Fatal("expected error for non-existent source page, got nil")
	}
}

func TestCopyPageUseCase_WithAssets(t *testing.T) {
	deps := newTestDeps(t)
	createUC := pages.NewCreatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())
	copyUC := pages.NewCopyPageUseCase(deps.tree, deps.slug, deps.orchestrator(), deps.assets, slog.Default())

	original, _ := createUC.Execute(context.Background(), pages.CreatePageInput{
		UserID: "user1", Title: "Original", Slug: "original", Kind: pageKind(),
	})

	file, _, err := test_utils.CreateMultipartFile("image.png", []byte("image content"))
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	defer test_utils.WrapCloseWithErrorCheck(file.Close, t)

	if _, err := deps.assets.SaveAssetForPage(original.Page.PageNode, file, tree.NewAssetNameUnchecked("image.png"), 1024); err != nil {
		t.Fatalf("Failed to save asset for original page: %v", err)
	}

	out, err := copyUC.Execute(context.Background(), pages.CopyPageInput{
		UserID: "user1", SourcePageID: pageID(original.Page.ID), Title: "Copy of Original", Slug: "copy-of-original",
	})
	if err != nil {
		t.Fatalf("unexpected error copying page: %v", err)
	}

	copiedAssets, err := deps.assets.ListAssetsForPage(out.Page.PageNode)
	if err != nil {
		t.Fatalf("Failed to list assets for copied page: %v", err)
	}
	if len(copiedAssets) != 1 {
		t.Errorf("expected 1 asset for copied page, got %d", len(copiedAssets))
	}
}

func TestCopyPageUseCase_IndexesOutgoingLinksOnCreate(t *testing.T) {
	deps := newTestDeps(t)
	createUC := pages.NewCreatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())
	updateUC := pages.NewUpdatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())
	copyUC := pages.NewCopyPageUseCase(deps.tree, deps.slug, deps.orchestrator(), deps.assets, slog.Default())

	target, err := createUC.Execute(context.Background(), pages.CreatePageInput{
		UserID: "user1", Title: "Target", Slug: "target", Kind: pageKind(),
	})
	if err != nil {
		t.Fatalf("unexpected error creating target page: %v", err)
	}

	original, err := createUC.Execute(context.Background(), pages.CreatePageInput{
		UserID: "user1", Title: "Original", Slug: "original", Kind: pageKind(),
	})
	if err != nil {
		t.Fatalf("unexpected error creating source page: %v", err)
	}

	content := "Links: [Target](/target.md)"
	if _, err := updateUC.Execute(context.Background(), pages.UpdatePageInput{
		UserID: "user1", ID: pageID(original.Page.ID), Version: pageVersion(original.Page.Version()), Title: original.Page.Title, Slug: slug(original.Page.Slug), Content: &content, Kind: pageKind(),
	}); err != nil {
		t.Fatalf("unexpected error updating source page: %v", err)
	}

	out, err := copyUC.Execute(context.Background(), pages.CopyPageInput{
		UserID: "user1", SourcePageID: pageID(original.Page.ID), Title: "Copy", Slug: "copy",
	})
	if err != nil {
		t.Fatalf("unexpected error copying page: %v", err)
	}

	outgoing, err := deps.links.GetOutgoingLinksForPage(out.Page.ID)
	if err != nil {
		t.Fatalf("GetOutgoingLinksForPage failed: %v", err)
	}
	if outgoing.Count != 1 {
		t.Fatalf("expected 1 outgoing link on copied page, got %d", outgoing.Count)
	}
	if outgoing.Outgoings[0].ToPageID != target.Page.ID {
		t.Fatalf("expected copied page link target %q, got %q", target.Page.ID, outgoing.Outgoings[0].ToPageID)
	}
}

func TestUpdatePageUseCase_EventBeforeIsOmittedForLiveNodeSafety(t *testing.T) {
	deps := newTestDeps(t)
	effect := &captureEffect{}
	orchestrator := pagesave.NewPageSaveOrchestrator(effect)
	createUC := pages.NewCreatePageUseCase(deps.tree, deps.slug, orchestrator, slog.Default())
	updateUC := pages.NewUpdatePageUseCase(deps.tree, deps.slug, orchestrator, slog.Default())

	created, err := createUC.Execute(context.Background(), pages.CreatePageInput{
		UserID: "user1", Title: "Old", Slug: "old", Kind: pageKind(),
	})
	if err != nil {
		t.Fatalf("unexpected error creating page: %v", err)
	}

	content := "updated"
	if _, err := updateUC.Execute(context.Background(), pages.UpdatePageInput{
		UserID: "user1", ID: pageID(created.Page.ID), Version: pageVersion(created.Page.Version()), Title: "New", Slug: "new", Content: &content, Kind: pageKind(),
	}); err != nil {
		t.Fatalf("unexpected error updating page: %v", err)
	}

	if len(effect.events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(effect.events))
	}
	event := effect.events[1]
	if event.Operation != pagesave.PageOperationUpdate {
		t.Fatalf("expected update event, got %q", event.Operation)
	}
	if event.Before != nil {
		t.Fatal("expected Before to be omitted for update events")
	}
	if event.OldPath != "/old" {
		t.Fatalf("expected OldPath=/old, got %q", event.OldPath)
	}
}

func TestMovePageUseCase_EventBeforeIsOmittedForLiveNodeSafety(t *testing.T) {
	deps := newTestDeps(t)
	effect := &captureEffect{}
	orchestrator := pagesave.NewPageSaveOrchestrator(effect)
	createUC := pages.NewCreatePageUseCase(deps.tree, deps.slug, orchestrator, slog.Default())
	moveUC := pages.NewMovePageUseCase(deps.tree, orchestrator, slog.Default())

	parentA, err := createUC.Execute(context.Background(), pages.CreatePageInput{
		UserID: "user1", Title: "A", Slug: "a", Kind: sectionKind(),
	})
	if err != nil {
		t.Fatalf("unexpected error creating parent A: %v", err)
	}
	parentB, err := createUC.Execute(context.Background(), pages.CreatePageInput{
		UserID: "user1", Title: "B", Slug: "b", Kind: sectionKind(),
	})
	if err != nil {
		t.Fatalf("unexpected error creating parent B: %v", err)
	}
	child, err := createUC.Execute(context.Background(), pages.CreatePageInput{
		UserID: "user1", ParentID: pageIDPtr(parentA.Page.ID), Title: "Child", Slug: "child", Kind: pageKind(),
	})
	if err != nil {
		t.Fatalf("unexpected error creating child: %v", err)
	}

	if err := moveUC.Execute(context.Background(), pages.MovePageInput{
		UserID: "user1", ID: pageID(child.Page.ID), Version: pageVersion(child.Page.Version()), ParentID: pageID(parentB.Page.ID),
	}); err != nil {
		t.Fatalf("unexpected error moving page: %v", err)
	}

	if len(effect.events) != 4 {
		t.Fatalf("expected 4 events, got %d", len(effect.events))
	}
	event := effect.events[3]
	if event.Operation != pagesave.PageOperationMove {
		t.Fatalf("expected move event, got %q", event.Operation)
	}
	if event.Before != nil {
		t.Fatal("expected Before to be omitted for move events")
	}
	if event.OldPath != "/a/child" {
		t.Fatalf("expected OldPath=/a/child, got %q", event.OldPath)
	}
}

func TestPreviewPageRefactorUseCase_RenameListsAffectedPages(t *testing.T) {
	deps := newTestDeps(t)
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
		t.Fatalf("UpdatePage failed: %v", err)
	}

	preview, err := previewUC.Execute(context.Background(), pages.RefactorPreviewInput{
		PageID: pageID(target.Page.ID),
		Kind:   pages.RefactorKindRename,
		Title:  target.Page.Title,
		Slug:   "target-renamed",
	})
	if err != nil {
		t.Fatalf("PreviewPageRefactor failed: %v", err)
	}
	if preview.OldPath != "/target" {
		t.Fatalf("OldPath = %q, want %q", preview.OldPath, "/target")
	}
	if preview.NewPath != "/target-renamed" {
		t.Fatalf("NewPath = %q, want %q", preview.NewPath, "/target-renamed")
	}
	if preview.Counts.AffectedPages != 1 {
		t.Fatalf("AffectedPages = %d, want 1", preview.Counts.AffectedPages)
	}
	if len(preview.AffectedPages) != 1 {
		t.Fatalf("expected 1 affected page, got %d", len(preview.AffectedPages))
	}
	if preview.AffectedPages[0].FromPageID != ref.Page.ID {
		t.Fatalf("FromPageID = %q, want %q", preview.AffectedPages[0].FromPageID, ref.Page.ID)
	}
}

func TestPreviewPageRefactorUseCase_RenameDoesNotListNonCanonicalExtensionlessPageLink(t *testing.T) {
	deps := newTestDeps(t)
	createUC := pages.NewCreatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())
	updateUC := pages.NewUpdatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())
	previewUC := pages.NewPreviewPageRefactorUseCase(deps.tree, deps.slug, deps.links, slog.Default())

	ref, err := createUC.Execute(context.Background(), pages.CreatePageInput{
		UserID: "system", Title: "Ref", Slug: "ref", Kind: pageKind(),
	})
	if err != nil {
		t.Fatalf("create ref failed: %v", err)
	}
	content := "[Target](/target)"
	if _, err := updateUC.Execute(context.Background(), pages.UpdatePageInput{
		UserID: "system", ID: pageID(ref.Page.ID), Version: pageVersion(ref.Page.Version()), Title: ref.Page.Title, Slug: slug(ref.Page.Slug), Content: &content, Kind: pageKind(),
	}); err != nil {
		t.Fatalf("UpdatePage failed: %v", err)
	}

	target, err := createUC.Execute(context.Background(), pages.CreatePageInput{
		UserID: "system", Title: "Target", Slug: "target", Kind: pageKind(),
	})
	if err != nil {
		t.Fatalf("create target failed: %v", err)
	}

	preview, err := previewUC.Execute(context.Background(), pages.RefactorPreviewInput{
		PageID: pageID(target.Page.ID),
		Kind:   pages.RefactorKindRename,
		Title:  target.Page.Title,
		Slug:   "target-renamed",
	})
	if err != nil {
		t.Fatalf("PreviewPageRefactor failed: %v", err)
	}
	if preview.Counts.AffectedPages != 0 {
		t.Fatalf("AffectedPages = %d, want 0: %#v", preview.Counts.AffectedPages, preview.AffectedPages)
	}
}

func TestApplyPageRefactorUseCase_RenameDoesNotRewriteNonCanonicalExtensionlessPageLink(t *testing.T) {
	deps := newTestDeps(t)
	createUC := pages.NewCreatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())
	updateUC := pages.NewUpdatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())
	applyUC := pages.NewApplyPageRefactorUseCase(deps.tree, deps.slug, deps.links, slog.Default())

	ref, err := createUC.Execute(context.Background(), pages.CreatePageInput{
		UserID: "system", Title: "Ref", Slug: "ref", Kind: pageKind(),
	})
	if err != nil {
		t.Fatalf("create ref failed: %v", err)
	}
	content := "[Target](/target)"
	if _, err := updateUC.Execute(context.Background(), pages.UpdatePageInput{
		UserID: "system", ID: pageID(ref.Page.ID), Version: pageVersion(ref.Page.Version()), Title: ref.Page.Title, Slug: slug(ref.Page.Slug), Content: &content, Kind: pageKind(),
	}); err != nil {
		t.Fatalf("UpdatePage failed: %v", err)
	}

	target, err := createUC.Execute(context.Background(), pages.CreatePageInput{
		UserID: "system", Title: "Target", Slug: "target", Kind: pageKind(),
	})
	if err != nil {
		t.Fatalf("create target failed: %v", err)
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
		t.Fatalf("ApplyPageRefactor failed: %v", err)
	}

	refPage, err := deps.tree.GetPage(tree.NewPageIDUnchecked(ref.Page.ID))
	if err != nil {
		t.Fatalf("GetPage(ref) failed: %v", err)
	}
	if refPage.Content != content {
		t.Fatalf("ref content = %q, want unchanged %q", refPage.Content, content)
	}
}

func TestPreviewPageRefactorUseCase_PageRenameIgnoresSectionTwinDescendants(t *testing.T) {
	deps := newTestDeps(t)
	createUC := pages.NewCreatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())
	updateUC := pages.NewUpdatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())
	previewUC := pages.NewPreviewPageRefactorUseCase(deps.tree, deps.slug, deps.links, slog.Default())

	docs, err := createUC.Execute(context.Background(), pages.CreatePageInput{
		UserID: "system", Title: "Docs", Slug: "docs", Kind: sectionKind(),
	})
	if err != nil {
		t.Fatalf("create docs failed: %v", err)
	}
	syncPage, err := createUC.Execute(context.Background(), pages.CreatePageInput{
		UserID: "system", ParentID: pageIDPtr(docs.Page.ID), Title: "Sync Page", Slug: "sync", Kind: pageKind(),
	})
	if err != nil {
		t.Fatalf("create sync page failed: %v", err)
	}
	syncSection, err := createUC.Execute(context.Background(), pages.CreatePageInput{
		UserID: "system", ParentID: pageIDPtr(docs.Page.ID), Title: "Sync Section", Slug: "sync", Kind: sectionKind(),
	})
	if err != nil {
		t.Fatalf("create sync section failed: %v", err)
	}
	if _, err := createUC.Execute(context.Background(), pages.CreatePageInput{
		UserID: "system", ParentID: pageIDPtr(syncSection.Page.ID), Title: "Child", Slug: "child", Kind: pageKind(),
	}); err != nil {
		t.Fatalf("create sync section child failed: %v", err)
	}
	pageRef, err := createUC.Execute(context.Background(), pages.CreatePageInput{
		UserID: "system", Title: "Page Ref", Slug: "page-ref", Kind: pageKind(),
	})
	if err != nil {
		t.Fatalf("create page ref failed: %v", err)
	}
	sectionDescendantRef, err := createUC.Execute(context.Background(), pages.CreatePageInput{
		UserID: "system", Title: "Section Descendant Ref", Slug: "section-descendant-ref", Kind: pageKind(),
	})
	if err != nil {
		t.Fatalf("create section descendant ref failed: %v", err)
	}
	pageRefContent := "[Sync page](/docs/sync.md)"
	if _, err := updateUC.Execute(context.Background(), pages.UpdatePageInput{
		UserID: "system", ID: pageID(pageRef.Page.ID), Version: pageVersion(pageRef.Page.Version()), Title: pageRef.Page.Title, Slug: slug(pageRef.Page.Slug), Content: &pageRefContent, Kind: pageKind(),
	}); err != nil {
		t.Fatalf("update page ref failed: %v", err)
	}
	sectionDescendantRefContent := "[Sync child](/docs/sync/child.md)"
	if _, err := updateUC.Execute(context.Background(), pages.UpdatePageInput{
		UserID: "system", ID: pageID(sectionDescendantRef.Page.ID), Version: pageVersion(sectionDescendantRef.Page.Version()), Title: sectionDescendantRef.Page.Title, Slug: slug(sectionDescendantRef.Page.Slug), Content: &sectionDescendantRefContent, Kind: pageKind(),
	}); err != nil {
		t.Fatalf("update section descendant ref failed: %v", err)
	}

	preview, err := previewUC.Execute(context.Background(), pages.RefactorPreviewInput{
		PageID: pageID(syncPage.Page.ID),
		Kind:   pages.RefactorKindRename,
		Title:  syncPage.Page.Title,
		Slug:   "sync-page",
	})
	if err != nil {
		t.Fatalf("PreviewPageRefactor failed: %v", err)
	}
	if preview.Counts.AffectedPages != 1 {
		t.Fatalf("AffectedPages = %d, want 1: %#v", preview.Counts.AffectedPages, preview.AffectedPages)
	}
	if len(preview.AffectedPages) != 1 || preview.AffectedPages[0].FromPageID != pageRef.Page.ID {
		t.Fatalf("affected pages = %#v, want only page ref %q", preview.AffectedPages, pageRef.Page.ID)
	}
}

func TestApplyPageRefactorUseCase_PageRenameKeepsSectionTwinDescendantLinksHealthy(t *testing.T) {
	deps := newTestDeps(t)
	createUC := pages.NewCreatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())
	updateUC := pages.NewUpdatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())
	applyUC := pages.NewApplyPageRefactorUseCase(deps.tree, deps.slug, deps.links, slog.Default())

	docs, err := createUC.Execute(context.Background(), pages.CreatePageInput{
		UserID: "system", Title: "Docs", Slug: "docs", Kind: sectionKind(),
	})
	if err != nil {
		t.Fatalf("create docs failed: %v", err)
	}
	syncPage, err := createUC.Execute(context.Background(), pages.CreatePageInput{
		UserID: "system", ParentID: pageIDPtr(docs.Page.ID), Title: "Sync Page", Slug: "sync", Kind: pageKind(),
	})
	if err != nil {
		t.Fatalf("create sync page failed: %v", err)
	}
	syncSection, err := createUC.Execute(context.Background(), pages.CreatePageInput{
		UserID: "system", ParentID: pageIDPtr(docs.Page.ID), Title: "Sync Section", Slug: "sync", Kind: sectionKind(),
	})
	if err != nil {
		t.Fatalf("create sync section failed: %v", err)
	}
	syncChild, err := createUC.Execute(context.Background(), pages.CreatePageInput{
		UserID: "system", ParentID: pageIDPtr(syncSection.Page.ID), Title: "Child", Slug: "child", Kind: pageKind(),
	})
	if err != nil {
		t.Fatalf("create sync section child failed: %v", err)
	}
	pageRef, err := createUC.Execute(context.Background(), pages.CreatePageInput{
		UserID: "system", Title: "Page Ref", Slug: "page-ref", Kind: pageKind(),
	})
	if err != nil {
		t.Fatalf("create page ref failed: %v", err)
	}
	sectionDescendantRef, err := createUC.Execute(context.Background(), pages.CreatePageInput{
		UserID: "system", Title: "Section Descendant Ref", Slug: "section-descendant-ref", Kind: pageKind(),
	})
	if err != nil {
		t.Fatalf("create section descendant ref failed: %v", err)
	}
	pageRefContent := "[Sync page](/docs/sync.md)"
	if _, err := updateUC.Execute(context.Background(), pages.UpdatePageInput{
		UserID: "system", ID: pageID(pageRef.Page.ID), Version: pageVersion(pageRef.Page.Version()), Title: pageRef.Page.Title, Slug: slug(pageRef.Page.Slug), Content: &pageRefContent, Kind: pageKind(),
	}); err != nil {
		t.Fatalf("update page ref failed: %v", err)
	}
	sectionDescendantRefContent := "[Sync child](/docs/sync/child.md)"
	if _, err := updateUC.Execute(context.Background(), pages.UpdatePageInput{
		UserID: "system", ID: pageID(sectionDescendantRef.Page.ID), Version: pageVersion(sectionDescendantRef.Page.Version()), Title: sectionDescendantRef.Page.Title, Slug: slug(sectionDescendantRef.Page.Slug), Content: &sectionDescendantRefContent, Kind: pageKind(),
	}); err != nil {
		t.Fatalf("update section descendant ref failed: %v", err)
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
		t.Fatalf("ApplyPageRefactor failed: %v", err)
	}

	status, err := deps.links.GetLinkStatusForPage(sectionDescendantRef.Page.ID, sectionDescendantRef.Page.CalculatePath())
	if err != nil {
		t.Fatalf("GetLinkStatusForPage failed: %v", err)
	}
	if status.Counts.BrokenOutgoings != 0 {
		t.Fatalf("broken outgoing count = %d, want 0: %#v", status.Counts.BrokenOutgoings, status.BrokenOutgoings)
	}
	if status.Counts.Outgoings != 1 || status.Outgoings[0].ToPageID != syncChild.Page.ID {
		t.Fatalf("outgoings = %#v, want healthy link to child %q", status.Outgoings, syncChild.Page.ID)
	}
}

func TestApplyPageRefactorUseCase_RenameRewritesIncomingLinks(t *testing.T) {
	deps := newTestDeps(t)
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
		t.Fatalf("UpdatePage failed: %v", err)
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
	if err != nil {
		t.Fatalf("ApplyPageRefactor failed: %v", err)
	}
	if updated.CalculatePath() != "/target-renamed" {
		t.Fatalf("updated path mismatch: %q", updated.CalculatePath())
	}

	refPage, err := deps.tree.GetPage(tree.NewPageIDUnchecked(ref.Page.ID))
	if err != nil {
		t.Fatalf("GetPage(ref) failed: %v", err)
	}
	if refPage.Content != "[Target](/target-renamed.md)" {
		t.Fatalf("ref content = %q, want %q", refPage.Content, "[Target](/target-renamed.md)")
	}

	outgoing, err := deps.links.GetOutgoingLinksForPage(ref.Page.ID)
	if err != nil {
		t.Fatalf("GetOutgoingLinks failed: %v", err)
	}
	if outgoing.Count != 1 {
		t.Fatalf("expected 1 outgoing, got %d", outgoing.Count)
	}
	if outgoing.Outgoings[0].ToPath != "/target-renamed" {
		t.Fatalf("ToPath = %q, want %q", outgoing.Outgoings[0].ToPath, "/target-renamed")
	}
	if outgoing.Outgoings[0].Broken {
		t.Fatalf("expected rewritten link to be healed")
	}

}

func TestApplyPageRefactorUseCase_UsesInjectedOrchestratorForRewrittenLinks(t *testing.T) {
	deps := newTestDeps(t)
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
		t.Fatalf("UpdatePage failed: %v", err)
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
		t.Fatalf("ApplyPageRefactor failed: %v", err)
	}

	var sawRewriteBatch bool
	for _, event := range capture.events {
		if event.Operation != pagesave.PageOperationUpdate || !event.ContentChanged || event.After != nil {
			continue
		}
		if len(event.AffectedPages) != 1 || event.AffectedPages[0].ID != ref.Page.ID {
			continue
		}
		if event.UserID != "mcp-user" {
			t.Fatalf("rewrite batch user id = %q, want mcp-user", event.UserID)
		}
		if event.Source != pagesave.PageMutationSourceMCP {
			t.Fatalf("rewrite batch source = %q, want MCP", event.Source)
		}
		sawRewriteBatch = true
	}
	if !sawRewriteBatch {
		t.Fatalf("did not see orchestrated rewrite batch event; events = %#v", capture.events)
	}
}

func TestApplyPageRefactorUseCase_RewrittenLinksKeepSearchIndexRawContent(t *testing.T) {
	deps := newTestDeps(t)
	searchIndex, err := search.NewSQLiteIndex(t.TempDir())
	if err != nil {
		t.Fatalf("NewSQLiteIndex failed: %v", err)
	}
	defer test_utils.WrapCloseWithErrorCheck(searchIndex.Close, t)

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
		t.Fatalf("UpdatePage failed: %v", err)
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
		t.Fatalf("ApplyPageRefactor failed: %v", err)
	}

	result, err := searchIndex.Search("refactorsearchtoken", nil, 0, 10)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(result.Items) != 1 || result.Items[0].PageID != ref.Page.ID {
		t.Fatalf("search result = %#v, want rewritten ref page", result.Items)
	}
}

func TestApplyPageRefactorUseCase_StaleVersionDoesNotRewriteIncomingLinks(t *testing.T) {
	deps := newTestDeps(t)
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
		t.Fatalf("UpdatePage(ref) failed: %v", err)
	}

	staleVersion := target.Page.Version()
	newerTargetContent := "newer target content"
	if _, err := updateUC.Execute(context.Background(), pages.UpdatePageInput{
		UserID: "system", ID: pageID(target.Page.ID), Version: pageVersion(staleVersion), Title: target.Page.Title, Slug: slug(target.Page.Slug), Content: &newerTargetContent, Kind: pageKind(),
	}); err != nil {
		t.Fatalf("UpdatePage(target) failed: %v", err)
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
	if !errors.Is(err, tree.ErrVersionConflict) {
		t.Fatalf("ApplyPageRefactor stale version error = %v, want ErrVersionConflict", err)
	}

	refAfter, err := deps.tree.GetPage(tree.NewPageIDUnchecked(ref.Page.ID))
	if err != nil {
		t.Fatalf("GetPage(ref) failed: %v", err)
	}
	if refAfter.Content != refContent {
		t.Fatalf("stale refactor rewrote incoming link content = %q, want %q", refAfter.Content, refContent)
	}
}

// - Refactor preview reports conflicts without mutating content
func TestApplyPageRefactorUseCase_TargetConflictDoesNotRewriteIncomingLinks(t *testing.T) {
	deps := newTestDeps(t)
	createUC := pages.NewCreatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())
	updateUC := pages.NewUpdatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())
	applyUC := pages.NewApplyPageRefactorUseCase(deps.tree, deps.slug, deps.links, slog.Default())

	target, _ := createUC.Execute(context.Background(), pages.CreatePageInput{
		UserID: "system", Title: "Target", Slug: "target", Kind: pageKind(),
	})
	if _, err := createUC.Execute(context.Background(), pages.CreatePageInput{
		UserID: "system", Title: "Existing", Slug: "existing", Kind: pageKind(),
	}); err != nil {
		t.Fatalf("CreatePage(existing) failed: %v", err)
	}
	ref, _ := createUC.Execute(context.Background(), pages.CreatePageInput{
		UserID: "system", Title: "Ref", Slug: "ref", Kind: pageKind(),
	})
	refContent := "[Target](/target)"
	if _, err := updateUC.Execute(context.Background(), pages.UpdatePageInput{
		UserID: "system", ID: pageID(ref.Page.ID), Version: pageVersion(ref.Page.Version()), Title: ref.Page.Title, Slug: slug(ref.Page.Slug), Content: &refContent, Kind: pageKind(),
	}); err != nil {
		t.Fatalf("UpdatePage(ref) failed: %v", err)
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
	if !errors.Is(err, tree.ErrPageAlreadyExists) {
		t.Fatalf("ApplyPageRefactor conflict error = %v, want ErrPageAlreadyExists", err)
	}

	refAfter, err := deps.tree.GetPage(tree.NewPageIDUnchecked(ref.Page.ID))
	if err != nil {
		t.Fatalf("GetPage(ref) failed: %v", err)
	}
	if refAfter.Content != refContent {
		t.Fatalf("conflicting refactor rewrote incoming link content = %q, want %q", refAfter.Content, refContent)
	}
	targetAfter, err := deps.tree.GetPage(tree.NewPageIDUnchecked(target.Page.ID))
	if err != nil {
		t.Fatalf("GetPage(target) failed: %v", err)
	}
	if targetAfter.CalculatePath() != "/target" {
		t.Fatalf("conflicting refactor moved target to %q, want /target", targetAfter.CalculatePath())
	}
}

func TestPreviewPageRefactorUseCase_UsesEmptyWarningArrays(t *testing.T) {
	deps := newTestDeps(t)
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
	if err != nil {
		t.Fatalf("PreviewPageRefactor failed: %v", err)
	}
	if preview.Warnings == nil {
		t.Fatalf("expected preview warnings to be an empty slice, got nil")
	}
	if len(preview.Warnings) != 0 {
		t.Fatalf("expected no preview warnings, got %d", len(preview.Warnings))
	}
	for i, affected := range preview.AffectedPages {
		if affected.Warnings == nil {
			t.Fatalf("affected page %d warnings should be empty slice, got nil", i)
		}
		if affected.MatchedPaths == nil {
			t.Fatalf("affected page %d matched paths should be empty slice, got nil", i)
		}
	}
}

func TestPreviewPageRefactorUseCase_Move_ExcludesMovedSubtreeFromOptionalAffectedPages(t *testing.T) {
	deps := newTestDeps(t)
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
		t.Fatalf("UpdatePage(pageA) failed: %v", err)
	}

	preview, err := previewUC.Execute(context.Background(), pages.RefactorPreviewInput{
		PageID:      pageID(pageA.Page.ID),
		Kind:        pages.RefactorKindMove,
		NewParentID: pageIDPtr(archive.Page.ID),
	})
	if err != nil {
		t.Fatalf("PreviewPageRefactor failed: %v", err)
	}
	if preview.Counts.AffectedPages != 0 {
		t.Fatalf("expected no optional affected pages, got %d", preview.Counts.AffectedPages)
	}
	if len(preview.AffectedPages) != 0 {
		t.Fatalf("expected no affected pages, got %d", len(preview.AffectedPages))
	}

	_ = pageB
}

func TestApplyPageRefactorUseCase_Move_RewritesRelativeOutgoingLinksInMovedPage(t *testing.T) {
	deps := newTestDeps(t)
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
		t.Fatalf("UpdatePage(pageA) failed: %v", err)
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
	if err != nil {
		t.Fatalf("ApplyPageRefactor(move) failed: %v", err)
	}
	if updated.CalculatePath() != "/archive/page-a" {
		t.Fatalf("updated path = %q, want %q", updated.CalculatePath(), "/archive/page-a")
	}

	movedPage, err := deps.tree.GetPage(tree.NewPageIDUnchecked(pageA.Page.ID))
	if err != nil {
		t.Fatalf("GetPage(pageA) failed: %v", err)
	}
	if movedPage.Content != "[To B](../docs/page-b.md)" {
		t.Fatalf("moved page content = %q, want %q", movedPage.Content, "[To B](../docs/page-b.md)")
	}

	outgoing, err := deps.links.GetOutgoingLinksForPage(pageA.Page.ID)
	if err != nil {
		t.Fatalf("GetOutgoingLinks(pageA) failed: %v", err)
	}
	if outgoing.Count != 1 {
		t.Fatalf("expected 1 outgoing link, got %d", outgoing.Count)
	}
	if outgoing.Outgoings[0].ToPageID != pageB.Page.ID {
		t.Fatalf("ToPageID = %q, want %q", outgoing.Outgoings[0].ToPageID, pageB.Page.ID)
	}
	if outgoing.Outgoings[0].ToPath != "/docs/page-b" {
		t.Fatalf("ToPath = %q, want %q", outgoing.Outgoings[0].ToPath, "/docs/page-b")
	}
	if outgoing.Outgoings[0].Broken {
		t.Fatalf("expected outgoing link to remain valid after move refactor")
	}

}

func TestEnsurePathUseCase_HealsLinksForAllCreatedSegments(t *testing.T) {
	deps := newTestDeps(t)
	createUC := pages.NewCreatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())
	updateUC := pages.NewUpdatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())
	ensureUC := pages.NewEnsurePathUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())

	pageA, err := createUC.Execute(context.Background(), pages.CreatePageInput{
		UserID: "system", Title: "Page A", Slug: "a", Kind: pageKind(),
	})
	if err != nil {
		t.Fatalf("CreatePage A failed: %v", err)
	}

	contentA := "Links: [X](/x) and [XY](/x/y.md)"
	if _, err := updateUC.Execute(context.Background(), pages.UpdatePageInput{
		UserID: "system", ID: pageID(pageA.Page.ID), Version: pageVersion(pageA.Page.Version()), Title: pageA.Page.Title, Slug: slug(pageA.Page.Slug), Content: &contentA, Kind: pageKind(),
	}); err != nil {
		t.Fatalf("UpdatePage A failed: %v", err)
	}

	if err := deps.links.IndexAllPages(); err != nil {
		t.Fatalf("IndexAllPages failed: %v", err)
	}

	out1, err := deps.links.GetOutgoingLinksForPage(pageA.Page.ID)
	if err != nil {
		t.Fatalf("GetOutgoingLinks failed: %v", err)
	}
	if out1.Count != 2 {
		t.Fatalf("expected 2 outgoings before ensure, got %d: %#v", out1.Count, out1.Outgoings)
	}

	byPath := map[string]bool{}
	for _, it := range out1.Outgoings {
		byPath[it.ToPath] = it.Broken
	}
	if broken, ok := byPath["/x"]; !ok || !broken {
		t.Fatalf("expected /x to be broken before ensure, got map=%#v, out=%#v", byPath, out1.Outgoings)
	}
	if broken, ok := byPath["/x/y"]; !ok || !broken {
		t.Fatalf("expected /x/y to be broken before ensure, got map=%#v, out=%#v", byPath, out1.Outgoings)
	}

	if _, err := ensureUC.Execute(context.Background(), pages.EnsurePathInput{
		UserID: "system", TargetPath: "/x/y", TargetTitle: "X Y", Kind: pageKind(),
	}); err != nil {
		t.Fatalf("EnsurePath failed: %v", err)
	}

	out2, err := deps.links.GetOutgoingLinksForPage(pageA.Page.ID)
	if err != nil {
		t.Fatalf("GetOutgoingLinks (after ensure) failed: %v", err)
	}
	if out2.Count != 2 {
		t.Fatalf("expected 2 outgoings after ensure, got %d: %#v", out2.Count, out2.Outgoings)
	}

	var gotX, gotXY *struct {
		broken bool
		toPage tree.PageID
	}
	for _, it := range out2.Outgoings {
		if it.ToPath == "/x" {
			gotX = &struct {
				broken bool
				toPage tree.PageID
			}{it.Broken, it.ToPageID}
		}
		if it.ToPath == "/x/y" {
			gotXY = &struct {
				broken bool
				toPage tree.PageID
			}{it.Broken, it.ToPageID}
		}
	}

	if gotX == nil || gotX.broken || gotX.toPage == "" {
		t.Fatalf("expected /x healed with ToPageID, got %#v", out2.Outgoings)
	}
	if gotXY == nil || gotXY.broken || gotXY.toPage == "" {
		t.Fatalf("expected /x/y healed with ToPageID, got %#v", out2.Outgoings)
	}
}

func TestDeletePageUseCase_NonRecursive_MarksIncomingBroken(t *testing.T) {
	deps := newTestDeps(t)
	createUC := pages.NewCreatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())
	updateUC := pages.NewUpdatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())
	deleteUC := pages.NewDeletePageUseCase(deps.tree, deps.assets, deps.orchestrator(), slog.Default())

	a, err := createUC.Execute(context.Background(), pages.CreatePageInput{
		UserID: "system", Title: "Page A", Slug: "a", Kind: pageKind(),
	})
	if err != nil {
		t.Fatalf("CreatePage A failed: %v", err)
	}
	contentA := "Link to B: [Go](/b)"
	if _, err := updateUC.Execute(context.Background(), pages.UpdatePageInput{
		UserID: "system", ID: pageID(a.Page.ID), Version: pageVersion(a.Page.Version()), Title: a.Page.Title, Slug: slug(a.Page.Slug), Content: &contentA, Kind: pageKind(),
	}); err != nil {
		t.Fatalf("UpdatePage A failed: %v", err)
	}

	b, err := createUC.Execute(context.Background(), pages.CreatePageInput{
		UserID: "system", Title: "Page B", Slug: "b", Kind: pageKind(),
	})
	if err != nil {
		t.Fatalf("CreatePage B failed: %v", err)
	}
	contentB := "# Page B"
	if _, err := updateUC.Execute(context.Background(), pages.UpdatePageInput{
		UserID: "system", ID: pageID(b.Page.ID), Version: pageVersion(b.Page.Version()), Title: b.Page.Title, Slug: slug(b.Page.Slug), Content: &contentB, Kind: pageKind(),
	}); err != nil {
		t.Fatalf("UpdatePage B failed: %v", err)
	}

	if err := deps.links.IndexAllPages(); err != nil {
		t.Fatalf("IndexAllPages failed: %v", err)
	}
	if err := deleteUC.Execute(context.Background(), pages.DeletePageInput{
		UserID: "system", ID: pageID(b.Page.ID), Version: pageVersion(b.Page.Version()), Recursive: false,
	}); err != nil {
		t.Fatalf("DeletePage failed: %v", err)
	}

	out, err := deps.links.GetOutgoingLinksForPage(a.Page.ID)
	if err != nil {
		t.Fatalf("GetOutgoingLinks failed: %v", err)
	}
	if out.Count != 1 {
		t.Fatalf("expected 1 outgoing, got %d", out.Count)
	}
	got := out.Outgoings[0]
	if got.ToPath != "/b" || !got.Broken || got.ToPageID != "" {
		t.Fatalf("unexpected outgoing after delete: %#v", got)
	}

	bl, err := deps.links.GetBacklinksForPage(b.Page.ID)
	if err != nil {
		t.Fatalf("GetBacklinks failed: %v", err)
	}
	if bl.Count != 0 {
		t.Fatalf("expected 0 backlinks after delete, got %d", bl.Count)
	}
}

func TestDeletePageUseCase_Recursive_RemovesOutgoingForSubtree_AndBreaksIncomingByPrefix(t *testing.T) {
	deps := newTestDeps(t)
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
		t.Fatalf("UpdatePage a failed: %v", err)
	}
	contentB := "# B"
	if _, err := updateUC.Execute(context.Background(), pages.UpdatePageInput{
		UserID: "system", ID: pageID(b.Page.ID), Version: pageVersion(b.Page.Version()), Title: b.Page.Title, Slug: slug(b.Page.Slug), Content: &contentB, Kind: pageKind(),
	}); err != nil {
		t.Fatalf("UpdatePage b failed: %v", err)
	}

	c, _ := createUC.Execute(context.Background(), pages.CreatePageInput{
		UserID: "system", Title: "C", Slug: "c", Kind: pageKind(),
	})
	contentC := "Incoming link: [B](/docs/b)"
	if _, err := updateUC.Execute(context.Background(), pages.UpdatePageInput{
		UserID: "system", ID: pageID(c.Page.ID), Version: pageVersion(c.Page.Version()), Title: c.Page.Title, Slug: slug(c.Page.Slug), Content: &contentC, Kind: pageKind(),
	}); err != nil {
		t.Fatalf("UpdatePage c failed: %v", err)
	}

	if err := deps.links.IndexAllPages(); err != nil {
		t.Fatalf("IndexAllPages failed: %v", err)
	}
	outA, err := deps.links.GetOutgoingLinksForPage(a.Page.ID)
	if err != nil || outA.Count != 1 {
		t.Fatalf("expected 1 outgoing from a before delete, got err=%v out=%#v", err, outA)
	}

	if err := deleteUC.Execute(context.Background(), pages.DeletePageInput{
		UserID: "system", ID: pageID(docs.Page.ID), Version: pageVersion(docs.Page.Version()), Recursive: true,
	}); err != nil {
		t.Fatalf("DeletePage(docs, recursive) failed: %v", err)
	}

	outAAfter, err := deps.links.GetOutgoingLinksForPage(a.Page.ID)
	if err != nil {
		t.Fatalf("GetOutgoingLinks(a) after delete failed: %v", err)
	}
	if outAAfter.Count != 0 {
		t.Fatalf("expected 0 outgoing from deleted page a, got %d", outAAfter.Count)
	}

	outC, err := deps.links.GetOutgoingLinksForPage(c.Page.ID)
	if err != nil {
		t.Fatalf("GetOutgoingLinks(c) after delete failed: %v", err)
	}
	if outC.Count != 1 {
		t.Fatalf("expected 1 outgoing from c, got %d", outC.Count)
	}
	got := outC.Outgoings[0]
	if got.ToPath != "/docs/b" || !got.Broken || got.ToPageID != "" {
		t.Fatalf("unexpected outgoing after recursive delete: %#v", got)
	}
}

func TestUpdatePageUseCase_RenamePage_MarksOldBroken_HealsNewExactPath(t *testing.T) {
	deps := newTestDeps(t)
	createUC := pages.NewCreatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())
	updateUC := pages.NewUpdatePageUseCase(deps.tree, deps.slug, deps.orchestrator(), slog.Default())

	a, _ := createUC.Execute(context.Background(), pages.CreatePageInput{
		UserID: "system", Title: "A", Slug: "a", Kind: pageKind(),
	})
	contentA := "Links: [B](/b) and [B2](/b2.md)"
	if _, err := updateUC.Execute(context.Background(), pages.UpdatePageInput{
		UserID: "system", ID: pageID(a.Page.ID), Version: pageVersion(a.Page.Version()), Title: a.Page.Title, Slug: slug(a.Page.Slug), Content: &contentA, Kind: pageKind(),
	}); err != nil {
		t.Fatalf("UpdatePage A failed: %v", err)
	}

	b, _ := createUC.Execute(context.Background(), pages.CreatePageInput{
		UserID: "system", Title: "B", Slug: "b", Kind: pageKind(),
	})
	contentB := "# B"
	if _, err := updateUC.Execute(context.Background(), pages.UpdatePageInput{
		UserID: "system", ID: pageID(b.Page.ID), Version: pageVersion(b.Page.Version()), Title: b.Page.Title, Slug: slug(b.Page.Slug), Content: &contentB, Kind: pageKind(),
	}); err != nil {
		t.Fatalf("UpdatePage B failed: %v", err)
	}

	if err := deps.links.IndexAllPages(); err != nil {
		t.Fatalf("IndexAllPages failed: %v", err)
	}
	out1, err := deps.links.GetOutgoingLinksForPage(a.Page.ID)
	if err != nil || out1.Count != 2 {
		t.Fatalf("unexpected outgoing before rename err=%v out=%#v", err, out1)
	}

	contentB2 := "# B (renamed)"
	if _, err := updateUC.Execute(context.Background(), pages.UpdatePageInput{
		UserID: "system", ID: pageID(b.Page.ID), Version: pageVersion(b.Page.Version()), Title: b.Page.Title, Slug: "b2", Content: &contentB2, Kind: pageKind(),
	}); err != nil {
		t.Fatalf("Rename B failed: %v", err)
	}

	out2, err := deps.links.GetOutgoingLinksForPage(a.Page.ID)
	if err != nil || out2.Count != 2 {
		t.Fatalf("unexpected outgoing after rename err=%v out=%#v", err, out2)
	}
	byPath := map[string]struct {
		broken bool
		toID   tree.PageID
	}{}
	for _, it := range out2.Outgoings {
		byPath[it.ToPath] = struct {
			broken bool
			toID   tree.PageID
		}{it.Broken, it.ToPageID}
	}
	if got, ok := byPath["/b"]; !ok || !got.broken || got.toID != "" {
		t.Fatalf("expected /b broken after rename, got %#v", byPath)
	}
	if got, ok := byPath["/b2"]; !ok || got.broken || got.toID != b.Page.ID {
		t.Fatalf("expected /b2 healed to %q, got %#v", b.Page.ID, byPath)
	}
}

func TestUpdatePageUseCase_RenameSubtree_BreaksOldPrefix_HealsNewSubpaths(t *testing.T) {
	deps := newTestDeps(t)
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
		t.Fatalf("UpdatePage B failed: %v", err)
	}

	a, _ := createUC.Execute(context.Background(), pages.CreatePageInput{
		UserID: "system", Title: "A", Slug: "a", Kind: pageKind(),
	})
	contentA := "Links: [Old](/docs/b) and [New](/docs2/b.md)"
	if _, err := updateUC.Execute(context.Background(), pages.UpdatePageInput{
		UserID: "system", ID: pageID(a.Page.ID), Version: pageVersion(a.Page.Version()), Title: a.Page.Title, Slug: slug(a.Page.Slug), Content: &contentA, Kind: pageKind(),
	}); err != nil {
		t.Fatalf("UpdatePage A failed: %v", err)
	}

	if err := deps.links.IndexAllPages(); err != nil {
		t.Fatalf("IndexAllPages failed: %v", err)
	}

	contentDocs2 := "# Docs"
	nodeSection := tree.NodeKindSection
	if _, err := updateUC.Execute(context.Background(), pages.UpdatePageInput{
		UserID: "system", ID: pageID(docs.Page.ID), Version: pageVersion(docs.Page.Version()), Title: docs.Page.Title, Slug: "docs2", Content: &contentDocs2, Kind: &nodeSection,
	}); err != nil {
		t.Fatalf("Rename docs failed: %v", err)
	}

	out2, err := deps.links.GetOutgoingLinksForPage(a.Page.ID)
	if err != nil || out2.Count != 2 {
		t.Fatalf("unexpected outgoing after subtree rename err=%v out=%#v", err, out2)
	}
	byPath := map[string]struct {
		broken bool
		toID   tree.PageID
	}{}
	for _, it := range out2.Outgoings {
		byPath[it.ToPath] = struct {
			broken bool
			toID   tree.PageID
		}{it.Broken, it.ToPageID}
	}
	if got, ok := byPath["/docs/b"]; !ok || !got.broken || got.toID != "" {
		t.Fatalf("expected /docs/b broken, got %#v", byPath)
	}
	if got, ok := byPath["/docs2/b"]; !ok || got.broken || got.toID != b.Page.ID {
		t.Fatalf("expected /docs2/b healed to %q, got %#v", b.Page.ID, byPath)
	}
}

func TestMovePageUseCase_MarksOldBroken_HealsNewExactPath(t *testing.T) {
	deps := newTestDeps(t)
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
		t.Fatalf("UpdatePage A failed: %v", err)
	}

	b, _ := createUC.Execute(context.Background(), pages.CreatePageInput{
		UserID: "system", Title: "B", Slug: "b", Kind: pageKind(),
	})
	contentB := "# B"
	if _, err := updateUC.Execute(context.Background(), pages.UpdatePageInput{
		UserID: "system", ID: pageID(b.Page.ID), Version: pageVersion(b.Page.Version()), Title: b.Page.Title, Slug: slug(b.Page.Slug), Content: &contentB, Kind: pageKind(),
	}); err != nil {
		t.Fatalf("UpdatePage B failed: %v", err)
	}

	projects, _ := createUC.Execute(context.Background(), pages.CreatePageInput{
		UserID: "system", Title: "Projects", Slug: "projects", Kind: pageKind(),
	})
	if err := deps.links.IndexAllPages(); err != nil {
		t.Fatalf("IndexAllPages failed: %v", err)
	}

	if err := moveUC.Execute(context.Background(), pages.MovePageInput{
		UserID: "system", ID: pageID(b.Page.ID), Version: pageVersion(b.Page.Version()), ParentID: pageID(projects.Page.ID),
	}); err != nil {
		t.Fatalf("MovePage failed: %v", err)
	}

	out2, err := deps.links.GetOutgoingLinksForPage(a.Page.ID)
	if err != nil || out2.Count != 2 {
		t.Fatalf("unexpected outgoing after move err=%v out=%#v", err, out2)
	}
	state := map[string]struct {
		broken bool
		toID   tree.PageID
	}{}
	for _, it := range out2.Outgoings {
		state[it.ToPath] = struct {
			broken bool
			toID   tree.PageID
		}{it.Broken, it.ToPageID}
	}
	if got := state["/b"]; !got.broken || got.toID != "" {
		t.Fatalf("expected /b broken after move, got %#v", state)
	}
	if got := state["/projects/b"]; got.broken || got.toID != b.Page.ID {
		t.Fatalf("expected /projects/b healed to %q, got %#v", b.Page.ID, state)
	}
}

func TestMovePageUseCase_MoveSubtree_BreaksOldPrefix_HealsNewSubpaths(t *testing.T) {
	deps := newTestDeps(t)
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
		t.Fatalf("UpdatePage B failed: %v", err)
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
		t.Fatalf("UpdatePage A failed: %v", err)
	}
	if err := deps.links.IndexAllPages(); err != nil {
		t.Fatalf("IndexAllPages failed: %v", err)
	}

	if err := moveUC.Execute(context.Background(), pages.MovePageInput{
		UserID: "system", ID: pageID(docs.Page.ID), Version: pageVersion(docs.Page.Version()), ParentID: pageID(archive.Page.ID),
	}); err != nil {
		t.Fatalf("MovePage(docs -> archive) failed: %v", err)
	}

	out, err := deps.links.GetOutgoingLinksForPage(a.Page.ID)
	if err != nil || out.Count != 2 {
		t.Fatalf("unexpected outgoing after subtree move err=%v out=%#v", err, out)
	}
	state := map[string]struct {
		broken bool
		toID   tree.PageID
	}{}
	for _, it := range out.Outgoings {
		state[it.ToPath] = struct {
			broken bool
			toID   tree.PageID
		}{it.Broken, it.ToPageID}
	}
	if got := state["/docs/b"]; !got.broken || got.toID != "" {
		t.Fatalf("expected /docs/b broken after move, got %#v", state)
	}
	if got := state["/archive/docs/b"]; got.broken || got.toID != b.Page.ID {
		t.Fatalf("expected /archive/docs/b healed to %q, got %#v", b.Page.ID, state)
	}
}

func TestMovePageUseCase_ReindexesRelativeLinks(t *testing.T) {
	deps := newTestDeps(t)
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
		t.Fatalf("UpdatePage /docs/shared failed: %v", err)
	}

	a, _ := createUC.Execute(context.Background(), pages.CreatePageInput{
		UserID: "system", ParentID: pageIDPtr(docs.Page.ID), Title: "A", Slug: "a", Kind: pageKind(),
	})
	contentA := "Relative: [S](./shared.md)"
	if _, err := updateUC.Execute(context.Background(), pages.UpdatePageInput{
		UserID: "system", ID: pageID(a.Page.ID), Version: pageVersion(a.Page.Version()), Title: a.Page.Title, Slug: slug(a.Page.Slug), Content: &contentA, Kind: pageKind(),
	}); err != nil {
		t.Fatalf("UpdatePage /docs/a failed: %v", err)
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
		t.Fatalf("UpdatePage /guide/shared failed: %v", err)
	}

	if err := deps.links.IndexAllPages(); err != nil {
		t.Fatalf("IndexAllPages failed: %v", err)
	}

	out1, err := deps.links.GetOutgoingLinksForPage(a.Page.ID)
	if err != nil || out1.Count != 1 {
		t.Fatalf("unexpected outgoing before move err=%v out=%#v", err, out1)
	}
	if out1.Outgoings[0].ToPath != "/docs/shared" || out1.Outgoings[0].Broken || out1.Outgoings[0].ToPageID != docsShared.Page.ID {
		t.Fatalf("unexpected outgoing before move: %#v", out1.Outgoings[0])
	}

	if err := moveUC.Execute(context.Background(), pages.MovePageInput{
		UserID: "system", ID: pageID(a.Page.ID), Version: pageVersion(a.Page.Version()), ParentID: pageID(guide.Page.ID),
	}); err != nil {
		t.Fatalf("MovePage(a -> guide) failed: %v", err)
	}

	out2, err := deps.links.GetOutgoingLinksForPage(a.Page.ID)
	if err != nil || out2.Count != 1 {
		t.Fatalf("unexpected outgoing after move err=%v out=%#v", err, out2)
	}
	if out2.Outgoings[0].ToPath != "/guide/shared" || out2.Outgoings[0].Broken || out2.Outgoings[0].ToPageID != guideShared.Page.ID {
		t.Fatalf("unexpected outgoing after move: %#v", out2.Outgoings[0])
	}
}
