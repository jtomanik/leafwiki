package tree

import (
	"errors"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"os"
	"path/filepath"
	"time"
)

var _ = Describe("tree service migration and store seam failure behavior", Label("unit"), func() {

	It("create rollback, delete, and convert seams preserve service state", func() {
		pageKind := NodeKindPage
		sectionKind := NodeKindSection

		invalidRootSvc := newInMemoryService()
		invalidRootSvc.tree.Kind = NodeKindPage
		_, err := invalidRootSvc.CreateNode(newFixtureUserID("user"), nil, "Child", newFixtureSlug("child"), &pageKind)
		Expect(err).To(MatchError(ErrParentMustBeSection))

		convertParentSvc := newInMemoryService()
		parentPage := edgePageNode(newFixturePageID("parent-page"), newFixtureSlug("parent-page"), "Parent Page", convertParentSvc.tree)
		convertParentSvc.tree.Children = []*PageNode{parentPage}
		convertParentSvc.rebuildIndexesLocked()
		swapTreeSeam(&treeStoreConvertNode, func(*NodeStore, *PageNode, NodeKind) error {
			return errors.New("convert parent failed")
		})
		_, err = convertParentSvc.CreateNode(newFixtureUserID("user"), &parentPage.ID, "Child", newFixtureSlug("child"), &pageKind)
		Expect(err).To(MatchError(ErrConvertParentNode))

		duplicateIDSvc := newInMemoryService()
		existing := edgePageNode(newFixturePageID("existing"), newFixtureSlug("existing"), "Existing", duplicateIDSvc.tree)
		duplicateIDSvc.tree.Children = []*PageNode{existing}
		duplicateIDSvc.rebuildIndexesLocked()
		_, err = duplicateIDSvc.RestoreNode(newFixtureUserID("user"), newFixturePageID("existing"), nil, "Existing", newFixtureSlug("restored"), NodeKindPage, "body", PageMetadata{})
		Expect(err).To(MatchError(ErrPageAlreadyExists))

		createSectionSvc := newInMemoryService()
		swapTreeSeam(&treeGenerateUniqueID, func() (string, error) { return "section-id", nil })
		swapTreeSeam(&treeStoreCreateSection, func(*NodeStore, *PageNode, *PageNode) error {
			return errors.New("create section failed")
		})
		_, err = createSectionSvc.CreateNode(newFixtureUserID("user"), nil, "Section", newFixtureSlug("section"), &sectionKind)
		Expect(err).To(MatchError(ErrCreateSectionEntry))

		rollbackSvc := newInMemoryService()
		Expect(rollbackSvc.rollbackCreatedNodeLocked(nil, nil, true)).To(Succeed())

		rollbackParent := edgeSectionNode(newFixturePageID("rollback-parent"), newFixtureSlug("rollback-parent"), "Rollback Parent", rollbackSvc.tree)
		rollbackSection := edgeSectionNode(newFixturePageID("rollback-section"), newFixtureSlug("rollback-section"), "Rollback Section", rollbackParent)
		rollbackParent.Children = []*PageNode{rollbackSection}
		deleteSectionErr := errors.New("delete section failed")
		swapTreeSeam(&treeStoreDeleteSection, func(*NodeStore, *PageNode) error {
			return deleteSectionErr
		})
		Expect(rollbackSvc.rollbackCreatedNodeLocked(rollbackParent, rollbackSection, false)).To(MatchError(deleteSectionErr))

		rollbackParent = edgeSectionNode(newFixturePageID("rollback-parent"), newFixtureSlug("rollback-parent"), "Rollback Parent", rollbackSvc.tree)
		rollbackPage := edgePageNode(newFixturePageID("rollback-page"), newFixtureSlug("rollback-page"), "Rollback Page", rollbackParent)
		rollbackParent.Children = []*PageNode{rollbackPage}
		swapTreeSeam(&treeStoreDeletePage, func(*NodeStore, *PageNode) error { return nil })
		removeOrderErr := errors.New("remove order failed")
		swapTreeSeam(&treeOSRemove, func(string) error {
			return removeOrderErr
		})
		Expect(rollbackSvc.rollbackCreatedNodeLocked(rollbackParent, rollbackPage, true)).To(MatchError(removeOrderErr))

		rollbackParent = edgeSectionNode(newFixturePageID("rollback-parent"), newFixtureSlug("rollback-parent"), "Rollback Parent", rollbackSvc.tree)
		rollbackPage = edgePageNode(newFixturePageID("rollback-page"), newFixtureSlug("rollback-page"), "Rollback Page", rollbackParent)
		rollbackParent.Children = []*PageNode{rollbackPage}
		swapTreeSeam(&treeOSRemove, func(string) error { return os.ErrNotExist })
		foldBackErr := errors.New("fold back failed")
		swapTreeSeam(&treeStoreConvertNode, func(*NodeStore, *PageNode, NodeKind) error {
			return foldBackErr
		})
		Expect(rollbackSvc.rollbackCreatedNodeLocked(rollbackParent, rollbackPage, true)).To(MatchError(foldBackErr))

		rollbackParent = edgeSectionNode(newFixturePageID("rollback-parent"), newFixtureSlug("rollback-parent"), "Rollback Parent", rollbackSvc.tree)
		rollbackPage = edgePageNode(newFixturePageID("rollback-page"), newFixtureSlug("rollback-page"), "Rollback Page", rollbackParent)
		rollbackParent.Children = []*PageNode{rollbackPage}
		swapTreeSeam(&treeStoreConvertNode, func(*NodeStore, *PageNode, NodeKind) error { return nil })
		Expect(rollbackSvc.rollbackCreatedNodeLocked(rollbackParent, rollbackPage, true)).To(Succeed())
		Expect(rollbackParent.Kind).To(Equal(NodeKindPage))

		orphanSvc := newInMemoryService()
		orphan := edgePageNode(newFixturePageID("orphan"), newFixtureSlug("orphan"), "Orphan", nil)
		orphanSvc.nodesByID[orphan.ID] = orphan
		Expect(orphanSvc.DeleteNode(newFixtureUserID("user"), newFixturePageID("orphan"), false, pageVersionUnchecked)).To(MatchError(ErrParentNotFound))

		deletePageWithChildrenSvc := newInMemoryService()
		pageWithChildren := edgePageNode(newFixturePageID("page-with-children"), newFixtureSlug("page-with-children"), "Page With Children", deletePageWithChildrenSvc.tree)
		child := edgePageNode(newFixturePageID("child"), newFixtureSlug("child"), "Child", pageWithChildren)
		pageWithChildren.Children = []*PageNode{child}
		deletePageWithChildrenSvc.tree.Children = []*PageNode{pageWithChildren}
		deletePageWithChildrenSvc.rebuildIndexesLocked()
		convertPageErr := errors.New("convert page failed")
		swapTreeSeam(&treeStoreConvertNode, func(*NodeStore, *PageNode, NodeKind) error {
			return convertPageErr
		})
		Expect(deletePageWithChildrenSvc.DeleteNode(newFixtureUserID("user"), pageWithChildren.ID, true, pageVersionUnchecked)).To(MatchError(convertPageErr))

		deletePageWithChildrenSvc = newInMemoryService()
		pageWithChildren = edgePageNode(newFixturePageID("page-with-children"), newFixtureSlug("page-with-children"), "Page With Children", deletePageWithChildrenSvc.tree)
		child = edgePageNode(newFixturePageID("child"), newFixtureSlug("child"), "Child", pageWithChildren)
		pageWithChildren.Children = []*PageNode{child}
		deletePageWithChildrenSvc.tree.Children = []*PageNode{pageWithChildren}
		deletePageWithChildrenSvc.rebuildIndexesLocked()
		swapTreeSeam(&treeStoreConvertNode, func(*NodeStore, *PageNode, NodeKind) error { return nil })
		deleteConvertedSectionErr := errors.New("delete converted section failed")
		swapTreeSeam(&treeStoreDeleteSection, func(*NodeStore, *PageNode) error {
			return deleteConvertedSectionErr
		})
		Expect(deletePageWithChildrenSvc.DeleteNode(newFixtureUserID("user"), pageWithChildren.ID, true, pageVersionUnchecked)).To(MatchError(deleteConvertedSectionErr))

		deleteOrderSvc := newInMemoryService()
		deletedPage := edgePageNode(newFixturePageID("deleted-page"), newFixtureSlug("deleted-page"), "Deleted Page", deleteOrderSvc.tree)
		deleteOrderSvc.tree.Children = []*PageNode{deletedPage}
		deleteOrderSvc.rebuildIndexesLocked()
		swapTreeSeam(&treeStoreDeletePage, func(*NodeStore, *PageNode) error { return nil })
		deleteOrderErr := errors.New("delete order failed")
		swapTreeSeam(&treeStoreSaveChildOrder, func(*NodeStore, *PageNode) error {
			return deleteOrderErr
		})
		Expect(deleteOrderSvc.DeleteNode(newFixtureUserID("user"), deletedPage.ID, false, pageVersionUnchecked)).To(MatchError(deleteOrderErr))

		convertSvc := newInMemoryService()
		page := edgePageNode(newFixturePageID("page"), newFixtureSlug("page"), "Page", convertSvc.tree)
		section := edgeSectionNode(newFixturePageID("section"), newFixtureSlug("section"), "Section", convertSvc.tree)
		section.Children = []*PageNode{edgePageNode(newFixturePageID("section-child"), newFixtureSlug("section-child"), "Section Child", section)}
		convertSvc.tree.Children = []*PageNode{page, section}
		convertSvc.rebuildIndexesLocked()
		Expect(convertSvc.UpdateNode(newFixtureUserID("user"), newFixturePageID("missing"), "Missing", newFixtureSlug("missing"), nil, pageVersionUnchecked, false)).To(MatchError(ErrPageNotFound))
		Expect(convertSvc.ConvertNode(newFixtureUserID("user"), newFixturePageID("missing"), NodeKindSection, pageVersionUnchecked)).To(MatchError(ErrPageNotFound))
		Expect(convertSvc.ConvertNode(newFixtureUserID("user"), newFixturePageID("page"), NodeKindPage, pageVersionUnchecked)).To(Succeed())
		Expect(convertSvc.ConvertNode(newFixtureUserID("user"), newFixturePageID("section"), NodeKindPage, pageVersionUnchecked)).To(MatchError(ErrPageHasChildren))
	})

	It("update, delete, convert, move, and sort operations propagate orchestration failures", func() {
		svc := newInMemoryService()
		page := edgePageNode(newFixturePageID("page"), newFixtureSlug("page"), "Page", svc.tree)
		page.Metadata.UpdatedAt = time.Now().UTC()
		section := edgeSectionNode(newFixturePageID("section"), newFixtureSlug("section"), "Section", svc.tree)
		child := edgePageNode(newFixturePageID("child"), newFixtureSlug("child"), "Child", section)
		section.Children = []*PageNode{child}
		child.Parent = section
		svc.tree.Children = []*PageNode{page, section}
		svc.rebuildIndexesLocked()

		Expect(svc.DeleteNode(newFixtureUserID("user"), newFixturePageID("missing"), false, pageVersionUnchecked)).To(MatchError(ErrPageNotFound))
		Expect(svc.DeleteNode(newFixtureUserID("user"), newFixturePageID("section"), false, pageVersionUnchecked)).To(MatchError(ErrPageHasChildren))

		deletePageErr := errors.New("delete page failed")
		swapTreeSeam(&treeStoreDeletePage, func(*NodeStore, *PageNode) error {
			return deletePageErr
		})
		Expect(svc.DeleteNode(newFixtureUserID("user"), newFixturePageID("page"), false, pageVersionUnchecked)).To(MatchError(deletePageErr))

		swapTreeSeam(&treeStoreDeletePage, func(*NodeStore, *PageNode) error { return nil })
		deleteSectionErr := errors.New("delete section failed")
		swapTreeSeam(&treeStoreDeleteSection, func(*NodeStore, *PageNode) error {
			return deleteSectionErr
		})
		Expect(svc.DeleteNode(newFixtureUserID("user"), newFixturePageID("section"), true, pageVersionUnchecked)).To(MatchError(deleteSectionErr))

		swapTreeSeam(&treeStoreDeleteSection, func(*NodeStore, *PageNode) error { return nil })
		unknown := edgePageNode(newFixturePageID("unknown"), newFixtureSlug("unknown"), "Unknown", svc.tree)
		unknown.Kind = newFixtureNodeKind("unknown")
		svc.tree.Children = append(svc.tree.Children, unknown)
		svc.rebuildIndexesLocked()
		Expect(svc.DeleteNode(newFixtureUserID("user"), newFixturePageID("unknown"), false, pageVersionUnchecked)).To(matchInvalidOp("DeleteNode"))

		content := "body"
		plainContentErr := errors.New("plain failed")
		swapTreeSeam(&treeStoreUpsertContent, func(*NodeStore, *PageNode, string) error {
			return plainContentErr
		})
		Expect(svc.UpdateNode(newFixtureUserID("user"), newFixturePageID("page"), "Page", newFixtureSlug("page"), &content, pageVersionUnchecked, false)).To(MatchError(plainContentErr))

		swapTreeSeam(&treeStoreUpsertContent, func(*NodeStore, *PageNode, string) error { return nil })
		preserveContentErr := errors.New("preserve failed")
		swapTreeSeam(&treeStoreUpsertContentPreservingFrontmatter, func(*NodeStore, *PageNode, string) error {
			return preserveContentErr
		})
		Expect(svc.UpdateNode(newFixtureUserID("user"), newFixturePageID("page"), "Page", newFixtureSlug("page"), &content, pageVersionUnchecked, true)).To(MatchError(preserveContentErr))

		swapTreeSeam(&treeStoreUpsertContentPreservingFrontmatter, func(*NodeStore, *PageNode, string) error { return nil })
		replaceContentErr := errors.New("replace failed")
		swapTreeSeam(&treeStoreUpsertContentReplacingMetadata, func(*NodeStore, *PageNode, string) error {
			return replaceContentErr
		})
		Expect(svc.UpdateNodeReplacingMetadata(newFixtureUserID("user"), newFixturePageID("page"), "Page", newFixtureSlug("page"), &content, pageVersionUnchecked)).To(MatchError(replaceContentErr))

		swapTreeSeam(&treeStoreUpsertContentReplacingMetadata, func(*NodeStore, *PageNode, string) error { return nil })
		renameErr := errors.New("rename failed")
		swapTreeSeam(&treeStoreRenameNode, func(*NodeStore, *PageNode, Slug) error {
			return renameErr
		})
		Expect(svc.UpdateNode(newFixtureUserID("user"), newFixturePageID("page"), "Page", newFixtureSlug("renamed"), nil, pageVersionUnchecked, false)).To(MatchError(renameErr))

		swapTreeSeam(&treeStoreRenameNode, func(*NodeStore, *PageNode, Slug) error { return nil })
		syncErr := errors.New("sync failed")
		swapTreeSeam(&treeStoreSyncMetadataIfExists, func(*NodeStore, *PageNode) error {
			return syncErr
		})
		Expect(svc.UpdateNode(newFixtureUserID("user"), newFixturePageID("page"), "Page", newFixtureSlug("page"), nil, pageVersionUnchecked, false)).To(MatchError(syncErr))
		Expect(svc.ConvertNode(newFixtureUserID("user"), newFixturePageID("page"), NodeKindSection, pageVersionUnchecked)).To(MatchError(syncErr))
		page.Kind = NodeKindPage

		swapTreeSeam(&treeStoreSyncMetadataIfExists, func(*NodeStore, *PageNode) error { return nil })
		convertErr := errors.New("convert failed")
		swapTreeSeam(&treeStoreConvertNode, func(*NodeStore, *PageNode, NodeKind) error {
			return convertErr
		})
		Expect(svc.ConvertNode(newFixtureUserID("user"), newFixturePageID("page"), NodeKindSection, pageVersionUnchecked)).To(MatchError(convertErr))

		swapTreeSeam(&treeStoreConvertNode, func(*NodeStore, *PageNode, NodeKind) error { return nil })
		Expect(svc.MoveNode(newFixtureUserID("user"), newFixturePageID("missing"), RootPageID, pageVersionUnchecked)).To(MatchError(ErrPageNotFound))
		Expect(svc.MoveNode(newFixtureUserID("user"), newFixturePageID("page"), newFixturePageID("missing-parent"), pageVersionUnchecked)).To(MatchError(ErrParentNotFound))
		Expect(svc.MoveNode(newFixtureUserID("user"), newFixturePageID("page"), newFixturePageID("page"), pageVersionUnchecked)).To(MatchError(ErrPageCannotBeMovedToItself))
		Expect(svc.MoveNode(newFixtureUserID("user"), newFixturePageID("section"), newFixturePageID("child"), pageVersionUnchecked)).To(MatchError(ErrMovePageCircularReference))

		moveErr := errors.New("move failed")
		swapTreeSeam(&treeStoreMoveNode, func(*NodeStore, *PageNode, *PageNode) error {
			return moveErr
		})
		Expect(svc.MoveNode(newFixtureUserID("user"), newFixturePageID("page"), newFixturePageID("section"), pageVersionUnchecked)).To(MatchError(moveErr))

		swapTreeSeam(&treeStoreMoveNode, func(*NodeStore, *PageNode, *PageNode) error { return nil })
		sourceOrderErr := errors.New("order failed")
		swapTreeSeam(&treeStoreSaveChildOrder, func(*NodeStore, *PageNode) error {
			return sourceOrderErr
		})
		Expect(svc.MoveNode(newFixtureUserID("user"), newFixturePageID("page"), newFixturePageID("section"), pageVersionUnchecked)).To(MatchError(sourceOrderErr))

		sortSvc := newInMemoryService()
		sortPage := edgePageNode(newFixturePageID("sort-page"), newFixtureSlug("sort-page"), "Sort Page", sortSvc.tree)
		sortSection := edgeSectionNode(newFixturePageID("sort-section"), newFixtureSlug("sort-section"), "Sort Section", sortSvc.tree)
		sortSvc.tree.Children = []*PageNode{sortPage, sortSection}
		sortSvc.rebuildIndexesLocked()
		Expect(sortSvc.SortPages(newFixturePageID("missing"), nil)).To(MatchError(ErrParentNotFound))
		Expect(sortSvc.SortPages(RootPageID, []PageID{newFixturePageID("only-one")})).To(MatchError(ErrInvalidSortOrder))
		Expect(sortSvc.SortPages(RootPageID, []PageID{newFixturePageID("sort-page"), newFixturePageID("not-present")})).To(MatchError(ErrInvalidSortOrder))
		Expect(sortSvc.SortPages(RootPageID, []PageID{newFixturePageID("sort-page"), newFixturePageID("sort-page")})).To(MatchError(ErrInvalidSortOrder))
	})

	It(")batch, lookup, ensure, and move operations roll back through service seams", func() {
		svc := newInMemoryService()
		pageKind := NodeKindPage
		page := edgePageNode(newFixturePageID("page"), newFixtureSlug("page"), "Page", svc.tree)
		section := edgeSectionNode(newFixturePageID("section"), newFixtureSlug("section"), "Section", svc.tree)
		destPage := edgePageNode(newFixturePageID("dest-page"), newFixtureSlug("dest-page"), "Dest Page", svc.tree)
		svc.tree.Children = []*PageNode{page, section, destPage}
		svc.rebuildIndexesLocked()

		Expect(svc.BulkUpdateContent(newFixtureUserID("user"), nil)).To(BeEmpty())
		bulkErrs := svc.BulkUpdateContent(newFixtureUserID("user"), []BulkContentUpdate{{ID: newFixturePageID("missing"), Content: "body"}})
		Expect(bulkErrs).To(ConsistOf(MatchError(ErrPageNotFound)))
		pages, pageErrs := svc.GetPages(nil)
		Expect(pages).To(BeEmpty())
		Expect(pageErrs).To(BeEmpty())
		pages, pageErrs = svc.GetPages([]PageID{newFixturePageID("missing")})
		Expect(pages).To(Equal([]*Page{nil}))
		Expect(pageErrs).To(ConsistOf(MatchError(ErrPageNotFound)))

		errFixtureRawFailed := errors.New(")raw failed")
		swapTreeSeam(&treeStoreReadPageRaw, func(*NodeStore, *PageNode) (string, error) {
			return "", errFixtureRawFailed
		})
		_, err := svc.ReadPageRaw(newFixturePageID("page"))
		Expect(err).To(MatchError(ErrGetPageRawContent))

		unloaded := NewTreeService(tempTreeDir())
		_, err = unloaded.FindPageByRoutePath(newFixtureRoutePath("page"))
		Expect(err).To(MatchError(ErrTreeNotLoaded))
		_, err = svc.LookupPagePath(newFixtureRoutePath(""))
		Expect(err).To(MatchError(ErrMissingRoutePath))
		_, err = svc.LookupPagePathForKind(newFixtureRoutePath(""), NodeKindPage)
		Expect(err).To(MatchError(ErrMissingRoutePath))

		swapTreeSeam(&treeStoreReadPageContent, func(*NodeStore, *PageNode) (string, error) {
			return "", errors.New("content failed")
		})
		_, err = svc.FindPageByRoutePath(newFixtureRoutePath("page"))
		Expect(err).To(MatchError(ErrGetPageContent))

		errFixtureRelFailed := errors.New("rel failed")
		relCalls := 0
		swapTreeSeam(&treeFilepathRel, func(string, string) (string, error) {
			relCalls++
			if relCalls == 1 {
				return "page.md", nil
			}
			return "", errFixtureRelFailed
		})
		_, err = svc.ContentPathForNode(page)
		Expect(err).To(MatchError(errFixtureRelFailed))

		missingIDPageSvc := newInMemoryService()
		emptyIDPage := edgePageNode(newFixturePageID(""), newFixtureSlug("empty-id"), "Empty ID", missingIDPageSvc.tree)
		missingIDPageSvc.tree.Children = []*PageNode{emptyIDPage}
		missingIDPageSvc.rebuildIndexesLocked()
		_, err = missingIDPageSvc.EnsurePagePath(newFixtureUserID("user"), newFixtureRoutePath("empty-id"), "Empty ID", &pageKind)
		Expect(err).To(MatchError(ErrFindExistingPageByID))

		ensureSvc := newInMemoryService()
		swapTreeSeam(&treeGenerateUniqueID, func() (string, error) { return "", nil })
		swapTreeSeam(&treeStoreCreatePage, func(*NodeStore, *PageNode, *PageNode) error { return nil })
		swapTreeSeam(&treeStoreSaveChildOrder, func(*NodeStore, *PageNode) error { return nil })
		_, err = ensureSvc.EnsurePagePath(newFixtureUserID("user"), newFixtureRoutePath("created"), "Created", &pageKind)
		Expect(err).To(MatchError(ErrFindCreatedPageByID))

		orphanSvc := newInMemoryService()
		orphan := edgePageNode(newFixturePageID("orphan"), newFixtureSlug("orphan"), "Orphan", nil)
		orphanSvc.nodesByID[orphan.ID] = orphan
		Expect(orphanSvc.MoveNode(newFixtureUserID("user"), newFixturePageID("orphan"), RootPageID, pageVersionUnchecked)).To(MatchError(ErrParentNotFound))

		convertDestErr := errors.New("convert dest failed")
		swapTreeSeam(&treeStoreConvertNode, func(*NodeStore, *PageNode, NodeKind) error {
			return convertDestErr
		})
		Expect(svc.MoveNode(newFixtureUserID("user"), newFixturePageID("page"), newFixturePageID("dest-page"), pageVersionUnchecked)).To(MatchError(convertDestErr))

		invalidDestSvc := newInMemoryService()
		movePage := edgePageNode(newFixturePageID("move-page"), newFixtureSlug("move-page"), "Move Page", invalidDestSvc.tree)
		invalidDest := edgeSectionNode(newFixturePageID("invalid-dest"), newFixtureSlug("invalid-dest"), "Invalid Dest", invalidDestSvc.tree)
		invalidDest.Kind = newFixtureNodeKind("unknown")
		invalidDestSvc.tree.Children = []*PageNode{movePage, invalidDest}
		invalidDestSvc.rebuildIndexesLocked()
		Expect(invalidDestSvc.MoveNode(newFixtureUserID("user"), newFixturePageID("move-page"), newFixturePageID("invalid-dest"), pageVersionUnchecked)).To(MatchError(ErrParentMustBeSection))

		moveOrderSvc := newInMemoryService()
		movePage = edgePageNode(newFixturePageID("move-page"), newFixtureSlug("move-page"), "Move Page", moveOrderSvc.tree)
		moveDest := edgeSectionNode(newFixturePageID("move-dest"), newFixtureSlug("move-dest"), "Move Dest", moveOrderSvc.tree)
		moveOrderSvc.tree.Children = []*PageNode{movePage, moveDest}
		moveOrderSvc.rebuildIndexesLocked()
		swapTreeSeam(&treeStoreMoveNode, func(*NodeStore, *PageNode, *PageNode) error { return nil })
		saveCalls := 0
		destinationOrderErr := errors.New("destination order failed")
		swapTreeSeam(&treeStoreSaveChildOrder, func(*NodeStore, *PageNode) error {
			saveCalls++
			if saveCalls == 2 {
				return destinationOrderErr
			}
			return nil
		})
		Expect(moveOrderSvc.MoveNode(newFixtureUserID("user"), newFixturePageID("move-page"), newFixturePageID("move-dest"), pageVersionUnchecked)).To(MatchError(destinationOrderErr))

		moveSyncSvc := newInMemoryService()
		movePage = edgePageNode(newFixturePageID("move-page"), newFixtureSlug("move-page"), "Move Page", moveSyncSvc.tree)
		moveDest = edgeSectionNode(newFixturePageID("move-dest"), newFixtureSlug("move-dest"), "Move Dest", moveSyncSvc.tree)
		moveSyncSvc.tree.Children = []*PageNode{movePage, moveDest}
		moveSyncSvc.rebuildIndexesLocked()
		swapTreeSeam(&treeStoreSaveChildOrder, func(*NodeStore, *PageNode) error { return nil })
		swapTreeSeam(&treeStoreSyncMetadataIfExists, func(*NodeStore, *PageNode) error {
			return errors.New("sync moved failed")
		})
		Expect(moveSyncSvc.MoveNode(newFixtureUserID("user"), newFixturePageID("move-page"), newFixturePageID("move-dest"), pageVersionUnchecked)).To(MatchError(ErrSyncMovedNodeMetadata))

		rollbackSvc := newInMemoryService()
		oldParent := rollbackSvc.tree
		newParent := edgeSectionNode(newFixturePageID("new-parent"), newFixtureSlug("new-parent"), "New Parent", oldParent)
		node := edgePageNode(newFixturePageID("rolled"), newFixtureSlug("rolled"), "Rolled", newParent)
		previousOldChildren := []*PageNode{}
		previousNewChildren := []*PageNode{node}
		swapTreeSeam(&treeStoreMoveNode, func(*NodeStore, *PageNode, *PageNode) error {
			return errors.New("move back failed")
		})
		swapTreeSeam(&treeStoreSaveChildOrder, func(*NodeStore, *PageNode) error { return nil })
		err = rollbackSvc.rollbackMovedNodeLocked(node, oldParent, newParent, previousOldChildren, map[PageID]int{}, previousNewChildren, map[PageID]int{"rolled": 3}, 3, PageMetadata{}, false)
		Expect(err).To(MatchError(ErrMoveNodeBackOnDisk))

		swapTreeSeam(&treeStoreMoveNode, func(*NodeStore, *PageNode, *PageNode) error { return nil })
		convertedParent := edgeSectionNode(newFixturePageID("converted-parent"), newFixtureSlug("converted-parent"), "Converted Parent", oldParent)
		convertedParent.WorkspaceSourcePath = newFixtureWorkspaceSourcePath("../outside")
		err = rollbackSvc.rollbackMovedNodeLocked(node, oldParent, convertedParent, nil, map[PageID]int{}, nil, map[PageID]int{}, 0, PageMetadata{}, true)
		Expect(err).To(MatchError(ErrResolveConvertedParentDir))

		swapTreeSeam(&treeFilepathRel, filepath.Rel)
		swapTreeSeam(&treeStoreConvertNode, func(*NodeStore, *PageNode, NodeKind) error { return nil })
		convertedParent = edgeSectionNode(newFixturePageID("converted-parent"), newFixtureSlug("converted-parent"), "Converted Parent", oldParent)
		swapTreeSeam(&treeOSRemoveAll, func(string) error {
			return errors.New("remove order failed")
		})
		err = rollbackSvc.rollbackMovedNodeLocked(node, oldParent, convertedParent, nil, map[PageID]int{}, nil, map[PageID]int{}, 0, PageMetadata{}, true)
		Expect(err).To(MatchError(ErrRemoveChildOrderBeforeParentRollback))

		convertedParent = edgeSectionNode(newFixturePageID("converted-parent"), newFixtureSlug("converted-parent"), "Converted Parent", oldParent)
		swapTreeSeam(&treeOSRemoveAll, func(string) error { return nil })
		swapTreeSeam(&treeStoreConvertNode, func(*NodeStore, *PageNode, NodeKind) error {
			return errors.New("convert back failed")
		})
		err = rollbackSvc.rollbackMovedNodeLocked(node, oldParent, convertedParent, nil, map[PageID]int{}, nil, map[PageID]int{}, 0, PageMetadata{}, true)
		Expect(err).To(MatchError(ErrConvertDestinationParentBackToPage))

		convertedParent = edgeSectionNode(newFixturePageID("converted-parent"), newFixtureSlug("converted-parent"), "Converted Parent", oldParent)
		swapTreeSeam(&treeStoreConvertNode, func(*NodeStore, *PageNode, NodeKind) error { return nil })
		Expect(rollbackSvc.rollbackMovedNodeLocked(node, oldParent, convertedParent, nil, map[PageID]int{}, nil, map[PageID]int{}, 0, PageMetadata{}, true)).To(Succeed())
		Expect(convertedParent.Kind).To(Equal(NodeKindPage))
	})
})
