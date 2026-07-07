package pages

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"

	"github.com/perber/wiki/internal/core/assets"
	"github.com/perber/wiki/internal/core/tree"
	"github.com/perber/wiki/internal/links"
	"github.com/perber/wiki/internal/wiki/pagesave"
)

var _ = ginkgo.Describe("page refactor apply failure mapping", func() {
	ginkgo.It("returns refactor apply failures from mutation and rewrite steps", ginkgo.Label("unit"), func() {
		ctx := context.Background()
		log := slog.New(slog.NewTextHandler(io.Discard, nil))
		slug := tree.NewSlugService()
		userID := newFixtureUserID("refactor-execute-user")
		newApply := func(fakeTree *pageUseCaseFakeTree, effect pagesave.PageSideEffect) *ApplyPageRefactorUseCase {
			orchestrator := pagesave.NewPageSaveOrchestrator()
			if effect != nil {
				orchestrator = pagesave.NewPageSaveOrchestrator(effect)
			}
			preview := &PreviewPageRefactorUseCase{tree: fakeTree, slug: slug, log: log}
			return &ApplyPageRefactorUseCase{tree: fakeTree, slug: slug, preview: preview, log: log, orchestrator: orchestrator}
		}

		capturePage := testFixturePage(newFixturePageID("capture-exec"), "Capture Exec", newFixtureSlug("capture-exec"), tree.NodeKindPage)
		captureErr := errors.New("capture snapshots failed")
		captureTree := &pageUseCaseFakeTree{
			getPageFunc: func(tree.PageID) (*tree.Page, error) {
				return capturePage, nil
			},
			getPagesFunc: func([]tree.PageID) ([]*tree.Page, []error) {
				return []*tree.Page{nil}, []error{captureErr}
			},
		}
		_, err := newApply(captureTree, nil).Execute(ctx, RefactorApplyInput{
			UserID: userID,
			RefactorPreviewInput: RefactorPreviewInput{
				Kind:   RefactorKindRename,
				PageID: capturePage.ID,
				Title:  "Captured",
				Slug:   newFixtureSlug("captured"),
			},
		})
		Expect(err).To(MatchError(captureErr))

		renameBefore := testFixturePage(newFixturePageID("rename-exec"), "Rename Exec", newFixtureSlug("old"), tree.NodeKindPage)
		renameAfter := testFixturePage(newFixturePageID("rename-exec"), "Rename Exec", newFixtureSlug("new"), tree.NodeKindPage)
		renameAfter.Content = "after rename"
		renameAffected := testFixturePage(newFixturePageID("rename-affected"), "Rename Affected", newFixtureSlug("rename-affected"), tree.NodeKindPage)
		renameAffected.Content = "[Old](/old.md)"
		renameRewriteErr := errors.New("rename incoming rewrite failed")
		renameUpdated := false
		renameTree := &pageUseCaseFakeTree{
			getPageFunc: func(id tree.PageID) (*tree.Page, error) {
				switch id {
				case renameBefore.ID:
					if renameUpdated {
						return renameAfter, nil
					}
					return renameBefore, nil
				case renameAffected.ID:
					return renameAffected, nil
				default:
					return nil, tree.ErrPageNotFound
				}
			},
			getPagesFunc: func(ids []tree.PageID) ([]*tree.Page, []error) {
				out := make([]*tree.Page, len(ids))
				errs := make([]error, len(ids))
				for i, id := range ids {
					switch id {
					case renameBefore.ID:
						if renameUpdated {
							out[i] = renameAfter
						} else {
							out[i] = renameBefore
						}
					case renameAffected.ID:
						out[i] = renameAffected
					default:
						errs[i] = tree.ErrPageNotFound
					}
				}
				return out, errs
			},
			updateNodeFunc: func(tree.UserID, tree.PageID, string, tree.Slug, *string, tree.PageVersion, bool) error {
				renameUpdated = true
				return nil
			},
			bulkUpdateContentFunc: func(tree.UserID, []tree.BulkContentUpdate) []error {
				return []error{nil}
			},
		}
		renameApply := newApply(renameTree, &failingSummaryPageSaveEffect{summary: "links rewritten", err: renameRewriteErr})
		renameApply.refactorLinks = &fakeRefactorLinks{matches: []links.RefactorLinkMatch{{FromPageID: renameAffected.ID, FromTitle: renameAffected.Title, ToPath: newFixtureRoutePath("old"), ToKind: links.TargetKindPage}}}
		_, err = renameApply.Execute(ctx, RefactorApplyInput{
			UserID:       userID,
			RewriteLinks: true,
			RefactorPreviewInput: RefactorPreviewInput{
				Kind:   RefactorKindRename,
				PageID: renameBefore.ID,
				Title:  "Rename Exec",
				Slug:   newFixtureSlug("new"),
			},
		})
		Expect(err).To(MatchError(renameRewriteErr))

		renameUpdated = false
		renamePathErr := errors.New("rename subtree rewrite failed")
		renamePathApply := newApply(renameTree, &failingSummaryPageSaveEffect{summary: "links rewritten", err: renamePathErr})
		_, err = renamePathApply.Execute(ctx, RefactorApplyInput{
			UserID: userID,
			RefactorPreviewInput: RefactorPreviewInput{
				Kind:   RefactorKindRename,
				PageID: renameBefore.ID,
				Title:  "Rename Exec",
				Slug:   newFixtureSlug("new"),
			},
		})
		Expect(err).To(MatchError(renamePathErr))

		movePage := testFixturePage(newFixturePageID("move-exec"), "Move Exec", newFixtureSlug("move-exec"), tree.NodeKindPage)
		moveParent := testFixturePage(newFixturePageID("move-parent"), "Move Parent", newFixtureSlug("move-parent"), tree.NodeKindSection)
		moveAffected := testFixturePage(newFixturePageID("move-affected"), "Move Affected", newFixtureSlug("move-affected"), tree.NodeKindPage)
		moveAffected.Content = "[Move](/move-exec.md)"
		moveErr := errors.New("move failed")
		moveErrTree := &pageUseCaseFakeTree{
			getPageFunc: func(id tree.PageID) (*tree.Page, error) {
				if id == moveParent.ID {
					return moveParent, nil
				}
				return movePage, nil
			},
			getPagesFunc: func(ids []tree.PageID) ([]*tree.Page, []error) {
				return []*tree.Page{movePage}, []error{nil}
			},
			moveNodeFunc: func(tree.UserID, tree.PageID, tree.PageID, tree.PageVersion) error {
				return moveErr
			},
		}
		_, err = newApply(moveErrTree, nil).Execute(ctx, RefactorApplyInput{
			UserID: userID,
			RefactorPreviewInput: RefactorPreviewInput{
				Kind:        RefactorKindMove,
				PageID:      movePage.ID,
				NewParentID: ptrPageID(moveParent.ID),
			},
		})
		Expect(err).To(MatchError(moveErr))

		moveUpdated := false
		moveBulkUpdateCalls := 0
		moveTree := &pageUseCaseFakeTree{
			getPageFunc: func(id tree.PageID) (*tree.Page, error) {
				switch id {
				case moveParent.ID:
					return moveParent, nil
				case moveAffected.ID:
					return moveAffected, nil
				default:
					return movePage, nil
				}
			},
			getPagesFunc: func(ids []tree.PageID) ([]*tree.Page, []error) {
				out := make([]*tree.Page, len(ids))
				errs := make([]error, len(ids))
				for i, id := range ids {
					switch id {
					case movePage.ID:
						if moveUpdated {
							movedPage := testPage(movePage.ID, movePage.Title, movePage.Slug, movePage.Kind)
							movedPage.Content = "after move"
							out[i] = movedPage
						} else {
							out[i] = movePage
						}
					case moveAffected.ID:
						out[i] = moveAffected
					default:
						errs[i] = tree.ErrPageNotFound
					}
				}
				return out, errs
			},
			moveNodeFunc: func(tree.UserID, tree.PageID, tree.PageID, tree.PageVersion) error {
				moveUpdated = true
				return nil
			},
			bulkUpdateContentFunc: func(_ tree.UserID, updates []tree.BulkContentUpdate) []error {
				moveBulkUpdateCalls++
				if moveBulkUpdateCalls == 1 {
					Expect(updates).To(ConsistOf(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
						"ID":      Equal(moveAffected.ID),
						"Content": Equal("[Move](/move-parent/move-exec.md)"),
					})))
				} else {
					Expect(moveBulkUpdateCalls).To(Equal(2))
					Expect(updates).To(ConsistOf(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
						"ID":      Equal(movePage.ID),
						"Content": Equal(movePage.Content),
					})))
				}
				return []error{nil}
			},
		}
		moveRewriteErr := errors.New("move incoming rewrite failed")
		moveApply := newApply(moveTree, &failingSummaryPageSaveEffect{summary: "links rewritten", err: moveRewriteErr})
		moveApply.refactorLinks = &fakeRefactorLinks{matches: []links.RefactorLinkMatch{{FromPageID: moveAffected.ID, FromTitle: moveAffected.Title, ToPath: newFixtureRoutePath("move-exec"), ToKind: links.TargetKindPage}}}
		_, err = moveApply.Execute(ctx, RefactorApplyInput{
			UserID:       userID,
			RewriteLinks: true,
			RefactorPreviewInput: RefactorPreviewInput{
				Kind:        RefactorKindMove,
				PageID:      movePage.ID,
				NewParentID: ptrPageID(moveParent.ID),
			},
		})
		Expect(err).To(MatchError(moveRewriteErr))
		Expect(moveBulkUpdateCalls).To(Equal(1))

		moveUpdated = false
		movePathErr := errors.New("move subtree rewrite failed")
		movePathApply := newApply(moveTree, &failingSummaryPageSaveEffect{summary: "links rewritten", err: movePathErr})
		_, err = movePathApply.Execute(ctx, RefactorApplyInput{
			UserID: userID,
			RefactorPreviewInput: RefactorPreviewInput{
				Kind:        RefactorKindMove,
				PageID:      movePage.ID,
				NewParentID: ptrPageID(moveParent.ID),
			},
		})
		Expect(err).To(MatchError(movePathErr))
		Expect(moveBulkUpdateCalls).To(Equal(2))

		deps := newRoutesSpecDeps()
		unloadedTree := tree.NewTreeService(pagesTempDir())
		var noKind tree.NodeKind
		_, err = (&Routes{treeService: unloadedTree}).findByPathInput(ctx, "", noKind)
		Expect(err).To(MatchError(tree.ErrTreeNotLoaded))

		docs := deps.createPage("Readme Route Docs", newFixtureSlug("readme-route-docs"), tree.NodeKindSection, nil)
		readmeDir := filepath.Join(deps.tree.RootDir(), docs.CalculateRoutePath().FilesystemPath())
		Expect(os.MkdirAll(readmeDir, 0o755)).To(Succeed())
		if err := os.Remove(filepath.Join(readmeDir, "index.md")); err != nil {
			Expect(err).To(MatchError(os.ErrNotExist))
		}
		Expect(os.WriteFile(filepath.Join(readmeDir, "README.md"), []byte("# Readme Route Docs"), 0o644)).To(Succeed())
		sectionOut, err := deps.routes.findByPathInput(ctx, readmeLookupPathForRoute(docs.CalculateRoutePath()), tree.NodeKindSection)
		Expect(err).NotTo(HaveOccurred())
		Expect(sectionOut.Page.ID).To(Equal(docs.ID))
	})

	ginkgo.It("returns direct page mutation validation and side-effect failures", ginkgo.Label("integration"), func() {
		deps := newRoutesSpecDeps()
		ctx := context.Background()
		log := slog.New(slog.NewTextHandler(io.Discard, nil))
		slug := tree.NewSlugService()
		kindPage := tree.NodeKindPage
		kindSection := tree.NodeKindSection
		invalidKind := newFixtureNodeKind("folder")

		_, err := deps.routes.createPage.Execute(ctx, CreatePageInput{Title: "", Slug: newFixtureSlug("bad slug"), Kind: &invalidKind})
		Expect(err).To(HavePageValidationFields("title", "kind", "slug"))

		badParentID := newFixturePageID(" parent ")
		_, err = deps.routes.createPage.Execute(ctx, CreatePageInput{Title: "Bad Parent", Slug: newFixtureSlug("bad-parent"), Kind: &kindPage, ParentID: &badParentID})
		Expect(err).To(MatchPageLocalizedCode(ErrCodePageInvalidParentID))
		_, err = deps.routes.createPage.Execute(ctx, CreatePageInput{Title: "Missing Parent", Slug: newFixtureSlug("missing-parent"), Kind: &kindPage, ParentID: ptrPageID(newFixturePageID("missing"))})
		Expect(err).To(MatchError(tree.ErrPageNotFound))
		existing := deps.createPage("Existing", newFixtureSlug("existing"), tree.NodeKindPage, nil)
		_, err = deps.routes.createPage.Execute(ctx, CreatePageInput{Title: "Conflict", Slug: existing.Slug, Kind: &kindPage})
		Expect(err).To(MatchError(tree.ErrPageAlreadyExists))
		createFailure := errors.New("create side effect failed")
		_, err = NewCreatePageUseCase(deps.tree, slug, pagesave.NewPageSaveOrchestrator(&failingPageSaveEffect{err: createFailure}), log).Execute(ctx, CreatePageInput{
			UserID: newFixtureUserID("routes-test-user"),
			Title:  "Create Fails",
			Slug:   newFixtureSlug("create-fails"),
			Kind:   &kindPage,
		})
		Expect(err).To(MatchError(createFailure))

		_, err = deps.routes.updatePage.Execute(ctx, UpdatePageInput{Title: "Bad Slug", Slug: newFixtureSlug("bad slug")})
		Expect(err).To(HavePageValidationField("slug"))
		_, err = deps.routes.updatePage.Execute(ctx, UpdatePageInput{ID: newFixturePageID("missing"), Title: "Missing", Slug: newFixtureSlug("missing")})
		Expect(err).To(MatchError(tree.ErrPageNotFound))
		updatePage := deps.createPage("Update", newFixtureSlug("update"), tree.NodeKindPage, nil)
		Expect(updatePage.Version()).NotTo(BeEmpty())
		_, err = deps.routes.updatePage.Execute(ctx, UpdatePageInput{
			ID:      updatePage.ID,
			Version: tree.PageVersionFromString("stale"),
			Title:   updatePage.Title,
			Slug:    updatePage.Slug,
		})
		Expect(err).To(MatchError(tree.ErrVersionConflict))
		updateFailure := errors.New("update side effect failed")
		_, err = NewUpdatePageUseCase(deps.tree, slug, pagesave.NewPageSaveOrchestrator(&failingPageSaveEffect{err: updateFailure}), log).Execute(ctx, UpdatePageInput{
			UserID:  newFixtureUserID("routes-test-user"),
			ID:      updatePage.ID,
			Version: updatePage.Version(),
			Title:   "Update Side Effect",
			Slug:    updatePage.Slug,
		})
		Expect(err).To(MatchError(updateFailure))

		Expect(deps.routes.deletePage.Execute(ctx, DeletePageInput{ID: tree.RootPageID})).To(MatchPageLocalizedCode(ErrCodePageRootOperation))
		Expect(deps.routes.deletePage.Execute(ctx, DeletePageInput{ID: newFixturePageID("missing"), Version: tree.PageVersionFromString("stale")})).To(MatchError(tree.ErrPageNotFound))
		parent := deps.createPage("Delete Parent", newFixtureSlug("delete-parent"), tree.NodeKindSection, nil)
		_ = deps.createPage("Delete Child", newFixtureSlug("delete-child"), tree.NodeKindPage, &parent.ID)
		Expect(deps.routes.deletePage.Execute(ctx, DeletePageInput{ID: parent.ID, Version: parent.Version()})).To(MatchError(tree.ErrPageHasChildren))
		deletePage := deps.createPage("Delete Stale", newFixtureSlug("delete-stale"), tree.NodeKindPage, nil)
		Expect(deps.routes.deletePage.Execute(ctx, DeletePageInput{ID: deletePage.ID, Version: tree.PageVersionFromString("stale")})).To(MatchError(tree.ErrVersionConflict))
		recursiveParent := deps.createPage("Recursive Delete", newFixtureSlug("recursive-delete"), tree.NodeKindSection, nil)
		_ = deps.createPage("Recursive Child", newFixtureSlug("recursive-child"), tree.NodeKindPage, &recursiveParent.ID)
		Expect(deps.routes.deletePage.Execute(ctx, DeletePageInput{ID: recursiveParent.ID, Version: recursiveParent.Version(), Recursive: true})).To(Succeed())
		deleteFailure := errors.New("delete side effect failed")
		deleteFailurePage := deps.createPage("Delete Failure", newFixtureSlug("delete-failure"), tree.NodeKindPage, nil)
		Expect(NewDeletePageUseCase(deps.tree, assets.NewAssetService(pagesTempDir(), slug), pagesave.NewPageSaveOrchestrator(&failingPageSaveEffect{err: deleteFailure}), log).Execute(ctx, DeletePageInput{
			UserID:  newFixtureUserID("routes-test-user"),
			ID:      deleteFailurePage.ID,
			Version: deleteFailurePage.Version(),
		})).To(MatchError(deleteFailure))

		Expect(deps.routes.movePage.Execute(ctx, MovePageInput{ID: tree.RootPageID})).To(MatchPageLocalizedCode(ErrCodePageRootOperation))
		Expect(deps.routes.movePage.Execute(ctx, MovePageInput{ID: existing.ID, ParentID: badParentID, Version: existing.Version()})).To(MatchPageLocalizedCode(ErrCodePageInvalidParentID))
		moveDest := deps.createPage("Move Dest", newFixtureSlug("move-dest"), tree.NodeKindSection, nil)
		Expect(deps.routes.movePage.Execute(ctx, MovePageInput{ID: newFixturePageID("missing"), ParentID: moveDest.ID, Version: tree.PageVersionFromString("stale")})).To(MatchError(tree.ErrPageNotFound))
		movePage := deps.createPage("Move Stale", newFixtureSlug("move-stale"), tree.NodeKindPage, nil)
		Expect(deps.routes.movePage.Execute(ctx, MovePageInput{ID: movePage.ID, ParentID: moveDest.ID, Version: tree.PageVersionFromString("stale")})).To(MatchError(tree.ErrVersionConflict))
		moveFailure := errors.New("move side effect failed")
		moveFailurePage := deps.createPage("Move Failure", newFixtureSlug("move-failure"), tree.NodeKindPage, nil)
		Expect(NewMovePageUseCase(deps.tree, pagesave.NewPageSaveOrchestrator(&failingPageSaveEffect{err: moveFailure}), log).Execute(ctx, MovePageInput{
			UserID:   newFixtureUserID("routes-test-user"),
			ID:       moveFailurePage.ID,
			ParentID: moveDest.ID,
			Version:  moveFailurePage.Version(),
		})).To(MatchError(moveFailure))

		Expect(deps.routes.convertPage.Execute(ctx, ConvertPageInput{ID: tree.RootPageID})).To(MatchPageLocalizedCode(ErrCodePageRootOperation))
		Expect(deps.routes.convertPage.Execute(ctx, ConvertPageInput{ID: newFixturePageID("missing"), Version: tree.PageVersionFromString("stale"), TargetKind: tree.NodeKindSection})).To(MatchError(tree.ErrPageNotFound))
		convertStale := deps.createPage("Convert Stale", newFixtureSlug("convert-stale"), tree.NodeKindPage, nil)
		Expect(deps.routes.convertPage.Execute(ctx, ConvertPageInput{ID: convertStale.ID, Version: tree.PageVersionFromString("stale"), TargetKind: tree.NodeKindSection})).To(MatchError(tree.ErrVersionConflict))
		convertParent := deps.createPage("Convert Parent", newFixtureSlug("convert-parent"), tree.NodeKindSection, nil)
		_ = deps.createPage("Convert Child", newFixtureSlug("convert-child"), tree.NodeKindPage, &convertParent.ID)
		Expect(deps.routes.convertPage.Execute(ctx, ConvertPageInput{ID: convertParent.ID, Version: convertParent.Version(), TargetKind: tree.NodeKindPage})).To(MatchError(tree.ErrPageHasChildren))
		convertFailurePage := deps.createPage("Convert Failure", newFixtureSlug("convert-failure"), tree.NodeKindPage, nil)
		convertFailure := errors.New("convert side effect failed")
		Expect(NewConvertPageUseCase(deps.tree, pagesave.NewPageSaveOrchestrator(&failingPageSaveEffect{err: convertFailure}), log).Execute(ctx, ConvertPageInput{
			UserID:     newFixtureUserID("routes-test-user"),
			ID:         convertFailurePage.ID,
			Version:    convertFailurePage.Version(),
			TargetKind: tree.NodeKindSection,
		})).To(MatchError(convertFailure))
		convertWithoutEffects := deps.createPage("Convert Without Effects", newFixtureSlug("convert-without-effects"), tree.NodeKindPage, nil)
		Expect(NewConvertPageUseCase(deps.tree, nil, log).Execute(ctx, ConvertPageInput{ID: convertWithoutEffects.ID, Version: convertWithoutEffects.Version(), TargetKind: tree.NodeKindSection})).To(Succeed())

		_, err = deps.routes.copyPage.Execute(ctx, CopyPageInput{Title: "", Slug: newFixtureSlug("bad slug")})
		Expect(err).To(HavePageValidationFields("title", "slug"))
		_, err = deps.routes.copyPage.Execute(ctx, CopyPageInput{SourcePageID: existing.ID, TargetParentID: &badParentID, Title: "Bad Parent", Slug: newFixtureSlug("bad-parent")})
		Expect(err).To(MatchPageLocalizedCode(ErrCodePageInvalidParentID))
		_, err = deps.routes.copyPage.Execute(ctx, CopyPageInput{SourcePageID: newFixturePageID("missing"), Title: "Missing", Slug: newFixtureSlug("missing")})
		Expect(err).To(MatchError(tree.ErrPageNotFound))
		_, err = deps.routes.copyPage.Execute(ctx, CopyPageInput{SourcePageID: existing.ID, TargetParentID: ptrPageID(newFixturePageID("missing")), Title: "Missing Parent", Slug: newFixtureSlug("copy-missing-parent")})
		Expect(err).To(MatchError(tree.ErrParentNotFound), "error = %v", err)
		_, err = deps.routes.copyPage.Execute(ctx, CopyPageInput{SourcePageID: existing.ID, Title: "Conflict Copy", Slug: existing.Slug})
		Expect(err).To(MatchError(tree.ErrPageAlreadyExists))
		copyFailurePage := deps.createPage("Copy Failure", newFixtureSlug("copy-failure"), tree.NodeKindPage, nil)
		copyFailure := errors.New("copy side effect failed")
		_, err = NewCopyPageUseCase(deps.tree, slug, pagesave.NewPageSaveOrchestrator(&failingPageSaveEffect{err: copyFailure}), assets.NewAssetService(pagesTempDir(), slug), log).Execute(ctx, CopyPageInput{
			UserID:       newFixtureUserID("routes-test-user"),
			SourcePageID: copyFailurePage.ID,
			Title:        "Copy Failure Result",
			Slug:         newFixtureSlug("copy-failure-result"),
		})
		Expect(err).To(MatchError(copyFailure))

		_, err = deps.routes.ensurePath.Execute(ctx, EnsurePathInput{TargetPath: newFixtureRoutePath(""), TargetTitle: ""})
		Expect(err).To(HavePageValidationFields("path", "title"))
		ensuredExisting := deps.createPage("Ensured Existing", newFixtureSlug("ensured-existing"), tree.NodeKindPage, nil)
		ensureOut, err := deps.routes.ensurePath.Execute(ctx, EnsurePathInput{TargetPath: ensuredExisting.CalculateRoutePath(), TargetTitle: "Ignored", Kind: &kindPage})
		Expect(err).NotTo(HaveOccurred())
		Expect(ensureOut.Page.ID).To(Equal(ensuredExisting.ID))
		ensureFailure := errors.New("ensure side effect failed")
		_, err = NewEnsurePathUseCase(deps.tree, slug, pagesave.NewPageSaveOrchestrator(&failingPageSaveEffect{err: ensureFailure}), log).Execute(ctx, EnsurePathInput{
			UserID:      newFixtureUserID("routes-test-user"),
			TargetPath:  newFixtureRoutePath("ensure/failure"),
			TargetTitle: "Failure",
			Kind:        &kindSection,
		})
		Expect(err).To(MatchError(ensureFailure))

		Expect(lookupFinalKindMatches(nil, tree.NodeKindPage)).To(BeFalse())
		Expect(lookupFinalKindMatches(&tree.PathLookup{}, tree.NodeKindPage)).To(BeFalse())
		Expect(collectSubtreeIDs(nil)).To(BeEmpty())
	})

})
