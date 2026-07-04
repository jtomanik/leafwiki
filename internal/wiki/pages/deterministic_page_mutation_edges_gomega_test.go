package pages

import (
	"context"
	"errors"
	"io"
	"log/slog"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"

	"github.com/perber/wiki/internal/core/tree"
	"github.com/perber/wiki/internal/wiki/pagesave"
)

var _ = ginkgo.Describe("deterministic page helper edges", func() {
	ginkgo.It("returns post-mutation failures from page use cases", ginkgo.Label("unit"), func() {
		ctx := context.Background()
		log := slog.New(slog.NewTextHandler(io.Discard, nil))
		slug := tree.NewSlugService()
		userID := tree.UserIDFromString("fake-user")
		kindPage := tree.NodeKindPage
		kindSection := tree.NodeKindSection
		noEffects := pagesave.NewPageSaveOrchestrator()

		ginkgo.By("create returning an error after the node has been inserted")
		createdID := tree.PageIDFromString("created")
		createReadErr := errors.New("created page read failed")
		createTree := &pageUseCaseFakeTree{
			createNodeFunc: func(tree.UserID, *tree.PageID, string, tree.Slug, *tree.NodeKind) (*tree.PageID, error) {
				return &createdID, nil
			},
			getPageFunc: func(tree.PageID) (*tree.Page, error) {
				return nil, createReadErr
			},
		}
		_, err := (&CreatePageUseCase{tree: createTree, slug: slug, orchestrator: noEffects, log: log}).Execute(ctx, CreatePageInput{
			UserID: userID,
			Title:  "Created",
			Slug:   tree.SlugFromString("created"),
			Kind:   &kindPage,
		})
		Expect(err).To(MatchError(createReadErr))

		ginkgo.By("convert returning an error after the conversion succeeds")
		convertReadErr := errors.New("converted page read failed")
		convertCalls := 0
		convertPage := testFixturePage("convert", "Convert", "convert", tree.NodeKindPage)
		convertTree := &pageUseCaseFakeTree{
			getPageFunc: func(tree.PageID) (*tree.Page, error) {
				convertCalls++
				if convertCalls == 1 {
					return convertPage, nil
				}
				return nil, convertReadErr
			},
			convertNodeFunc: func(tree.UserID, tree.PageID, tree.NodeKind, tree.PageVersion) error {
				return nil
			},
		}
		Expect((&ConvertPageUseCase{tree: convertTree, orchestrator: noEffects, log: log}).Execute(ctx, ConvertPageInput{
			UserID:     userID,
			ID:         convertPage.ID,
			TargetKind: tree.NodeKindSection,
		})).To(MatchError(convertReadErr))

		ginkgo.By("copy cleanup when the copied page cannot be fetched")
		sourcePage := testFixturePage("copy-source", "Copy Source", "copy-source", tree.NodeKindPage)
		copyID := tree.PageIDFromString("copy-created")
		copyFetchErr := errors.New("copied page read failed")
		cleanupDeletes := 0
		copyFetchCalls := 0
		copyFetchTree := &pageUseCaseFakeTree{
			getPageFunc: func(id tree.PageID) (*tree.Page, error) {
				copyFetchCalls++
				if copyFetchCalls == 1 {
					Expect(id).To(Equal(sourcePage.ID))
					return sourcePage, nil
				}
				Expect(id).To(Equal(copyID))
				return nil, copyFetchErr
			},
			createNodeFunc: func(tree.UserID, *tree.PageID, string, tree.Slug, *tree.NodeKind) (*tree.PageID, error) {
				return &copyID, nil
			},
			deleteNodeUncheckedVersionFunc: func(tree.UserID, tree.PageID, bool) error {
				cleanupDeletes++
				return nil
			},
		}
		_, err = (&CopyPageUseCase{tree: copyFetchTree, slug: slug, assets: &fakePageAssets{}, orchestrator: noEffects, log: log}).Execute(ctx, CopyPageInput{
			UserID:       userID,
			SourcePageID: sourcePage.ID,
			Title:        "Copy",
			Slug:         tree.SlugFromString("copy"),
		})
		Expect(err).To(MatchError(copyFetchErr))
		Expect(cleanupDeletes).To(Equal(1))

		ginkgo.By("copy asset cleanup when the copied content update fails")
		copyPage := testPage(copyID, "Copy", tree.SlugFromString("copy"), tree.NodeKindPage)
		updateErr := errors.New("copy content update failed")
		assetsAfterUpdateErr := &fakePageAssets{}
		copyUpdateCalls := 0
		copyUpdateTree := &pageUseCaseFakeTree{
			getPageFunc: func(tree.PageID) (*tree.Page, error) {
				copyUpdateCalls++
				if copyUpdateCalls == 1 {
					return sourcePage, nil
				}
				return copyPage, nil
			},
			createNodeFunc: func(tree.UserID, *tree.PageID, string, tree.Slug, *tree.NodeKind) (*tree.PageID, error) {
				return &copyID, nil
			},
			deleteNodeUncheckedVersionFunc: func(tree.UserID, tree.PageID, bool) error {
				return nil
			},
			updateNodeUncheckedVersionFunc: func(tree.UserID, tree.PageID, string, tree.Slug, *string, bool) error {
				return updateErr
			},
		}
		_, err = (&CopyPageUseCase{tree: copyUpdateTree, slug: slug, assets: assetsAfterUpdateErr, orchestrator: noEffects, log: log}).Execute(ctx, CopyPageInput{
			UserID:       userID,
			SourcePageID: sourcePage.ID,
			Title:        "Copy",
			Slug:         tree.SlugFromString("copy"),
		})
		Expect(err).To(MatchError(updateErr))
		Expect(pageAssetCallCounts(assetsAfterUpdateErr)).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"Copy":   Equal(1),
			"Delete": Equal(1),
		}))

		ginkgo.By("copy returning an error when the final page fetch fails")
		finalReadErr := errors.New("final copy read failed")
		copyFinalCalls := 0
		copyFinalTree := &pageUseCaseFakeTree{
			getPageFunc: func(tree.PageID) (*tree.Page, error) {
				copyFinalCalls++
				switch copyFinalCalls {
				case 1:
					return sourcePage, nil
				case 2:
					return copyPage, nil
				default:
					return nil, finalReadErr
				}
			},
			createNodeFunc: func(tree.UserID, *tree.PageID, string, tree.Slug, *tree.NodeKind) (*tree.PageID, error) {
				return &copyID, nil
			},
			deleteNodeUncheckedVersionFunc: func(tree.UserID, tree.PageID, bool) error {
				return nil
			},
			updateNodeUncheckedVersionFunc: func(tree.UserID, tree.PageID, string, tree.Slug, *string, bool) error {
				return nil
			},
		}
		_, err = (&CopyPageUseCase{tree: copyFinalTree, slug: slug, assets: &fakePageAssets{}, orchestrator: noEffects, log: log}).Execute(ctx, CopyPageInput{
			UserID:       userID,
			SourcePageID: sourcePage.ID,
			Title:        "Copy",
			Slug:         tree.SlugFromString("copy"),
		})
		Expect(err).To(MatchError(finalReadErr))

		ginkgo.By("update returning an error after the update succeeds")
		updateBefore := testFixturePage("update", "Update", "update", tree.NodeKindPage)
		updateReadErr := errors.New("updated page read failed")
		updateCalls := 0
		updateTree := &pageUseCaseFakeTree{
			getPageFunc: func(tree.PageID) (*tree.Page, error) {
				updateCalls++
				if updateCalls == 1 {
					return updateBefore, nil
				}
				return nil, updateReadErr
			},
			updateNodeFunc: func(tree.UserID, tree.PageID, string, tree.Slug, *string, tree.PageVersion, bool) error {
				return nil
			},
		}
		_, err = (&UpdatePageUseCase{tree: updateTree, slug: slug, orchestrator: noEffects, log: log}).Execute(ctx, UpdatePageInput{
			UserID: userID,
			ID:     updateBefore.ID,
			Title:  "Update",
			Slug:   tree.SlugFromString("update"),
		})
		Expect(err).To(MatchError(updateReadErr))

		ginkgo.By("update warning and continuing when a slug-change affected page fails to load")
		updateChild := testFixtureChildPage(updateBefore, "update-child", "Update Child", "child", tree.NodeKindPage)
		updateBefore.Children = []*tree.PageNode{updateChild.PageNode}
		updateAfter := testFixturePage("update", "Renamed", "renamed", tree.NodeKindPage)
		affectedErr := errors.New("affected update page failed")
		updateAffectedCalls := 0
		updateAffectedTree := &pageUseCaseFakeTree{
			getPageFunc: func(tree.PageID) (*tree.Page, error) {
				updateAffectedCalls++
				if updateAffectedCalls == 1 {
					return updateBefore, nil
				}
				return updateAfter, nil
			},
			updateNodeFunc: func(tree.UserID, tree.PageID, string, tree.Slug, *string, tree.PageVersion, bool) error {
				return nil
			},
			getPagesFunc: func(ids []tree.PageID) ([]*tree.Page, []error) {
				Expect(ids).To(Equal([]tree.PageID{updateBefore.ID, updateChild.ID}))
				return []*tree.Page{updateAfter, nil}, []error{nil, affectedErr}
			},
		}
		out, err := (&UpdatePageUseCase{tree: updateAffectedTree, slug: slug, orchestrator: noEffects, log: log}).Execute(ctx, UpdatePageInput{
			UserID: userID,
			ID:     updateBefore.ID,
			Title:  "Renamed",
			Slug:   tree.SlugFromString("renamed"),
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(out.Page).To(BeIdenticalTo(updateAfter))

		ginkgo.By("delete warning and continuing when recursive affected pages or assets fail")
		deleteParent := testFixturePage("delete-parent", "Delete Parent", "delete-parent", tree.NodeKindSection)
		deleteChild := testFixtureChildPage(deleteParent, "delete-child", "Delete Child", "child", tree.NodeKindPage)
		deleteParent.Children = []*tree.PageNode{deleteChild.PageNode}
		deleteAssets := &fakePageAssets{deleteErr: errors.New("asset delete failed")}
		deleteGetPagesErr := errors.New("recursive child read failed")
		deleteTree := &pageUseCaseFakeTree{
			getPageFunc: func(tree.PageID) (*tree.Page, error) {
				return deleteParent, nil
			},
			getPagesFunc: func(ids []tree.PageID) ([]*tree.Page, []error) {
				Expect(ids).To(Equal([]tree.PageID{deleteParent.ID, deleteChild.ID}))
				return []*tree.Page{deleteParent, nil}, []error{nil, deleteGetPagesErr}
			},
			deleteNodeFunc: func(tree.UserID, tree.PageID, bool, tree.PageVersion) error {
				return nil
			},
		}
		Expect((&DeletePageUseCase{tree: deleteTree, assets: deleteAssets, orchestrator: noEffects, log: log}).Execute(ctx, DeletePageInput{
			UserID:    userID,
			ID:        deleteParent.ID,
			Recursive: true,
		})).To(Succeed())
		Expect(deleteAssets.deleteCalls).To(Equal(1))

		ginkgo.By("delete returning recursive orchestrator errors")
		recursiveEffectErr := errors.New("recursive delete side effect failed")
		recursiveEffectTree := &pageUseCaseFakeTree{
			getPageFunc: func(tree.PageID) (*tree.Page, error) {
				return deleteParent, nil
			},
			getPagesFunc: func([]tree.PageID) ([]*tree.Page, []error) {
				return []*tree.Page{deleteParent, deleteChild}, []error{nil, nil}
			},
			deleteNodeFunc: func(tree.UserID, tree.PageID, bool, tree.PageVersion) error {
				return nil
			},
		}
		Expect((&DeletePageUseCase{
			tree:         recursiveEffectTree,
			assets:       &fakePageAssets{},
			orchestrator: pagesave.NewPageSaveOrchestrator(&failingPageSaveEffect{err: recursiveEffectErr}),
			log:          log,
		}).Execute(ctx, DeletePageInput{UserID: userID, ID: deleteParent.ID, Recursive: true})).To(MatchError(recursiveEffectErr))

		ginkgo.By("delete warning and continuing when non-recursive asset deletion fails")
		nonRecursiveAssets := &fakePageAssets{deleteErr: errors.New("non-recursive asset delete failed")}
		nonRecursiveTree := &pageUseCaseFakeTree{
			getPageFunc: func(tree.PageID) (*tree.Page, error) {
				return sourcePage, nil
			},
			deleteNodeFunc: func(tree.UserID, tree.PageID, bool, tree.PageVersion) error {
				return nil
			},
		}
		Expect((&DeletePageUseCase{tree: nonRecursiveTree, assets: nonRecursiveAssets, orchestrator: noEffects, log: log}).Execute(ctx, DeletePageInput{
			UserID: userID,
			ID:     sourcePage.ID,
		})).To(Succeed())
		Expect(nonRecursiveAssets.deleteCalls).To(Equal(1))

		ginkgo.By("move warning and continuing when affected pages fail to load")
		moveParent := testFixturePage("move", "Move", "move", tree.NodeKindSection)
		moveChild := testFixtureChildPage(moveParent, "move-child", "Move Child", "child", tree.NodeKindPage)
		moveParent.Children = []*tree.PageNode{moveChild.PageNode}
		moveAffectedErr := errors.New("moved child read failed")
		moveTree := &pageUseCaseFakeTree{
			getPageFunc: func(tree.PageID) (*tree.Page, error) {
				return moveParent, nil
			},
			moveNodeFunc: func(tree.UserID, tree.PageID, tree.PageID, tree.PageVersion) error {
				return nil
			},
			getPagesFunc: func(ids []tree.PageID) ([]*tree.Page, []error) {
				Expect(ids).To(Equal([]tree.PageID{moveParent.ID, moveChild.ID}))
				return []*tree.Page{moveParent, nil}, []error{nil, moveAffectedErr}
			},
		}
		Expect((&MovePageUseCase{tree: moveTree, orchestrator: noEffects, log: log}).Execute(ctx, MovePageInput{
			UserID:   userID,
			ID:       moveParent.ID,
			ParentID: tree.RootPageID,
		})).To(Succeed())

		ginkgo.By("ensure path lookup, existing-page, creation, and post-create failures")
		lookupErr := errors.New("lookup failed")
		_, err = (&EnsurePathUseCase{tree: &pageUseCaseFakeTree{
			lookupPagePathFunc: func(tree.RoutePath) (*tree.PathLookup, error) {
				return nil, lookupErr
			},
		}, slug: slug, orchestrator: noEffects, log: log}).Execute(ctx, EnsurePathInput{
			UserID:      userID,
			TargetPath:  "lookup",
			TargetTitle: "Lookup",
			Kind:        &kindPage,
		})
		Expect(err).To(MatchError(lookupErr))

		finalID := tree.PageIDFromString("existing-final")
		finalKind := tree.NodeKindPage
		existingFinalReadErr := errors.New("existing final read failed")
		_, err = (&EnsurePathUseCase{tree: &pageUseCaseFakeTree{
			lookupPagePathFunc: func(routePath tree.RoutePath) (*tree.PathLookup, error) {
				return &tree.PathLookup{
					Path:   routePath,
					Exists: true,
					Segments: []tree.PathSegment{{
						Slug:   tree.SlugFromString("existing-final"),
						Exists: true,
						Kind:   &finalKind,
						ID:     &finalID,
					}},
				}, nil
			},
			getPageFunc: func(tree.PageID) (*tree.Page, error) {
				return nil, existingFinalReadErr
			},
		}, slug: slug, orchestrator: noEffects, log: log}).Execute(ctx, EnsurePathInput{
			UserID:      userID,
			TargetPath:  "existing-final",
			TargetTitle: "Existing",
			Kind:        &kindPage,
		})
		Expect(err).To(MatchError(existingFinalReadErr))

		_, err = (&EnsurePathUseCase{tree: &pageUseCaseFakeTree{
			lookupPagePathFunc: func(routePath tree.RoutePath) (*tree.PathLookup, error) {
				return &tree.PathLookup{
					Path: routePath,
					Segments: []tree.PathSegment{{
						Slug:   tree.SlugFromString("bad slug"),
						Exists: false,
					}},
				}, nil
			},
		}, slug: slug, orchestrator: noEffects, log: log}).Execute(ctx, EnsurePathInput{
			UserID:      userID,
			TargetPath:  "invalid-segment",
			TargetTitle: "Invalid Segment",
			Kind:        &kindPage,
		})
		Expect(err).To(HavePageValidationField("path"))

		ensureErr := errors.New("ensure failed")
		_, err = (&EnsurePathUseCase{tree: &pageUseCaseFakeTree{
			lookupPagePathFunc: func(routePath tree.RoutePath) (*tree.PathLookup, error) {
				return &tree.PathLookup{Path: routePath, CanCreate: true}, nil
			},
			ensurePagePathFunc: func(tree.UserID, tree.RoutePath, string, *tree.NodeKind) (*tree.EnsurePathResult, error) {
				return nil, ensureErr
			},
		}, slug: slug, orchestrator: noEffects, log: log}).Execute(ctx, EnsurePathInput{
			UserID:      userID,
			TargetPath:  "ensure-fails",
			TargetTitle: "Ensure Fails",
			Kind:        &kindPage,
		})
		Expect(err).To(MatchError(ensureErr))

		resultNode := testFixturePage("ensure-result", "Ensure Result", "ensure-result", tree.NodeKindPage).PageNode
		createdNode := testFixturePage("ensure-created", "Ensure Created", "created", tree.NodeKindPage).PageNode
		resultReadErr := errors.New("result page read failed")
		_, err = (&EnsurePathUseCase{tree: fakeEnsureTree(resultNode, []*tree.PageNode{createdNode}, func(ids []tree.PageID) ([]*tree.Page, []error) {
			Expect(ids).To(Equal([]tree.PageID{resultNode.ID, createdNode.ID}))
			return []*tree.Page{nil, nil}, []error{resultReadErr, nil}
		}), slug: slug, orchestrator: noEffects, log: log}).Execute(ctx, EnsurePathInput{
			UserID:      userID,
			TargetPath:  "ensure-result",
			TargetTitle: "Ensure Result",
			Kind:        &kindPage,
		})
		Expect(err).To(MatchError(resultReadErr))

		_, err = (&EnsurePathUseCase{tree: fakeEnsureTree(resultNode, []*tree.PageNode{createdNode}, func(ids []tree.PageID) ([]*tree.Page, []error) {
			Expect(ids).To(Equal([]tree.PageID{resultNode.ID, createdNode.ID}))
			return []*tree.Page{testPage(resultNode.ID, "Ensure Result", tree.SlugFromString("ensure-result"), tree.NodeKindPage), nil}, []error{nil, errors.New("created page read failed")}
		}), slug: slug, orchestrator: noEffects, log: log}).Execute(ctx, EnsurePathInput{
			UserID:      userID,
			TargetPath:  "ensure-created-error",
			TargetTitle: "Ensure Created Error",
			Kind:        &kindPage,
		})
		Expect(err).NotTo(HaveOccurred())

		_, err = (&EnsurePathUseCase{tree: fakeEnsureTree(resultNode, nil, func(ids []tree.PageID) ([]*tree.Page, []error) {
			Expect(ids).To(Equal([]tree.PageID{resultNode.ID}))
			return []*tree.Page{nil}, []error{nil}
		}), slug: slug, orchestrator: noEffects, log: log}).Execute(ctx, EnsurePathInput{
			UserID:      userID,
			TargetPath:  "ensure-missing-result",
			TargetTitle: "Ensure Missing Result",
			Kind:        &kindPage,
		})
		Expect(err).To(MatchError(tree.ErrPageNotFound))

		_, err = (&EnsurePathUseCase{tree: fakeEnsureTree(resultNode, []*tree.PageNode{createdNode}, func(ids []tree.PageID) ([]*tree.Page, []error) {
			Expect(ids).To(Equal([]tree.PageID{resultNode.ID, createdNode.ID}))
			return []*tree.Page{testPage(resultNode.ID, "Ensure Result", tree.SlugFromString("ensure-result"), tree.NodeKindPage), nil}, []error{nil, nil}
		}), slug: slug, orchestrator: noEffects, log: log}).Execute(ctx, EnsurePathInput{
			UserID:      userID,
			TargetPath:  "ensure-created-missing",
			TargetTitle: "Ensure Created Missing",
			Kind:        &kindPage,
		})
		Expect(err).NotTo(HaveOccurred())

		ensureSideEffectErr := errors.New("ensure side effect failed")
		_, err = (&EnsurePathUseCase{tree: fakeEnsureTree(resultNode, []*tree.PageNode{createdNode}, func(ids []tree.PageID) ([]*tree.Page, []error) {
			Expect(ids).To(Equal([]tree.PageID{resultNode.ID, createdNode.ID}))
			return []*tree.Page{
				testPage(resultNode.ID, "Ensure Result", tree.SlugFromString("ensure-result"), tree.NodeKindPage),
				testPage(createdNode.ID, "Ensure Created", tree.SlugFromString("created"), tree.NodeKindPage),
			}, []error{nil, nil}
		}), slug: slug, orchestrator: pagesave.NewPageSaveOrchestrator(&failingPageSaveEffect{err: ensureSideEffectErr}), log: log}).Execute(ctx, EnsurePathInput{
			UserID:      userID,
			TargetPath:  "ensure-effect-error",
			TargetTitle: "Ensure Effect Error",
			Kind:        &kindSection,
		})
		Expect(err).To(MatchError(ensureSideEffectErr))
	})

})
