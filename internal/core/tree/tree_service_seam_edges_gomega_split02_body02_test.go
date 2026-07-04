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
		_, err := invalidRootSvc.CreateNode("user", nil, "Child", "child", &pageKind)
		Expect(err).To(MatchError(ErrParentMustBeSection))

		convertParentSvc := newInMemoryService()
		parentPage := edgePageNode("parent-page", "parent-page", "Parent Page", convertParentSvc.tree)
		convertParentSvc.tree.Children = []*PageNode{parentPage}
		convertParentSvc.rebuildIndexesLocked()
		swapTreeSeam(&treeStoreConvertNode, func(*NodeStore, *PageNode, NodeKind) error {
			return errors.New("convert parent failed")
		})
		_, err = convertParentSvc.CreateNode("user", &parentPage.ID, "Child", "child", &pageKind)
		Expect(err).To(MatchError(ErrConvertParentNode))

		duplicateIDSvc := newInMemoryService()
		existing := edgePageNode("existing", "existing", "Existing", duplicateIDSvc.tree)
		duplicateIDSvc.tree.Children = []*PageNode{existing}
		duplicateIDSvc.rebuildIndexesLocked()
		_, err = duplicateIDSvc.RestoreNode("user", "existing", nil, "Existing", "restored", NodeKindPage, "body", PageMetadata{})
		Expect(err).To(MatchError(ErrPageAlreadyExists))

		createSectionSvc := newInMemoryService()
		swapTreeSeam(&treeGenerateUniqueID, func() (string, error) { return "section-id", nil })
		swapTreeSeam(&treeStoreCreateSection, func(*NodeStore, *PageNode, *PageNode) error {
			return errors.New("create section failed")
		})
		_, err = createSectionSvc.CreateNode("user", nil, "Section", "section", &sectionKind)
		Expect(err).To(MatchError(ErrCreateSectionEntry))

		rollbackSvc := newInMemoryService()
		Expect(rollbackSvc.rollbackCreatedNodeLocked(nil, nil, true)).To(Succeed())

		rollbackParent := edgeSectionNode("rollback-parent", "rollback-parent", "Rollback Parent", rollbackSvc.tree)
		rollbackSection := edgeSectionNode("rollback-section", "rollback-section", "Rollback Section", rollbackParent)
		rollbackParent.Children = []*PageNode{rollbackSection}
		deleteSectionErr := errors.New("delete section failed")
		swapTreeSeam(&treeStoreDeleteSection, func(*NodeStore, *PageNode) error {
			return deleteSectionErr
		})
		Expect(rollbackSvc.rollbackCreatedNodeLocked(rollbackParent, rollbackSection, false)).To(MatchError(deleteSectionErr))

		rollbackParent = edgeSectionNode("rollback-parent", "rollback-parent", "Rollback Parent", rollbackSvc.tree)
		rollbackPage := edgePageNode("rollback-page", "rollback-page", "Rollback Page", rollbackParent)
		rollbackParent.Children = []*PageNode{rollbackPage}
		swapTreeSeam(&treeStoreDeletePage, func(*NodeStore, *PageNode) error { return nil })
		removeOrderErr := errors.New("remove order failed")
		swapTreeSeam(&treeOSRemove, func(string) error {
			return removeOrderErr
		})
		Expect(rollbackSvc.rollbackCreatedNodeLocked(rollbackParent, rollbackPage, true)).To(MatchError(removeOrderErr))

		rollbackParent = edgeSectionNode("rollback-parent", "rollback-parent", "Rollback Parent", rollbackSvc.tree)
		rollbackPage = edgePageNode("rollback-page", "rollback-page", "Rollback Page", rollbackParent)
		rollbackParent.Children = []*PageNode{rollbackPage}
		swapTreeSeam(&treeOSRemove, func(string) error { return os.ErrNotExist })
		foldBackErr := errors.New("fold back failed")
		swapTreeSeam(&treeStoreConvertNode, func(*NodeStore, *PageNode, NodeKind) error {
			return foldBackErr
		})
		Expect(rollbackSvc.rollbackCreatedNodeLocked(rollbackParent, rollbackPage, true)).To(MatchError(foldBackErr))

		rollbackParent = edgeSectionNode("rollback-parent", "rollback-parent", "Rollback Parent", rollbackSvc.tree)
		rollbackPage = edgePageNode("rollback-page", "rollback-page", "Rollback Page", rollbackParent)
		rollbackParent.Children = []*PageNode{rollbackPage}
		swapTreeSeam(&treeStoreConvertNode, func(*NodeStore, *PageNode, NodeKind) error { return nil })
		Expect(rollbackSvc.rollbackCreatedNodeLocked(rollbackParent, rollbackPage, true)).To(Succeed())
		Expect(rollbackParent.Kind).To(Equal(NodeKindPage))

		orphanSvc := newInMemoryService()
		orphan := edgePageNode("orphan", "orphan", "Orphan", nil)
		orphanSvc.nodesByID[orphan.ID] = orphan
		Expect(orphanSvc.DeleteNode("user", "orphan", false, pageVersionUnchecked)).To(MatchError(ErrParentNotFound))

		deletePageWithChildrenSvc := newInMemoryService()
		pageWithChildren := edgePageNode("page-with-children", "page-with-children", "Page With Children", deletePageWithChildrenSvc.tree)
		child := edgePageNode("child", "child", "Child", pageWithChildren)
		pageWithChildren.Children = []*PageNode{child}
		deletePageWithChildrenSvc.tree.Children = []*PageNode{pageWithChildren}
		deletePageWithChildrenSvc.rebuildIndexesLocked()
		convertPageErr := errors.New("convert page failed")
		swapTreeSeam(&treeStoreConvertNode, func(*NodeStore, *PageNode, NodeKind) error {
			return convertPageErr
		})
		Expect(deletePageWithChildrenSvc.DeleteNode("user", pageWithChildren.ID, true, pageVersionUnchecked)).To(MatchError(convertPageErr))

		deletePageWithChildrenSvc = newInMemoryService()
		pageWithChildren = edgePageNode("page-with-children", "page-with-children", "Page With Children", deletePageWithChildrenSvc.tree)
		child = edgePageNode("child", "child", "Child", pageWithChildren)
		pageWithChildren.Children = []*PageNode{child}
		deletePageWithChildrenSvc.tree.Children = []*PageNode{pageWithChildren}
		deletePageWithChildrenSvc.rebuildIndexesLocked()
		swapTreeSeam(&treeStoreConvertNode, func(*NodeStore, *PageNode, NodeKind) error { return nil })
		deleteConvertedSectionErr := errors.New("delete converted section failed")
		swapTreeSeam(&treeStoreDeleteSection, func(*NodeStore, *PageNode) error {
			return deleteConvertedSectionErr
		})
		Expect(deletePageWithChildrenSvc.DeleteNode("user", pageWithChildren.ID, true, pageVersionUnchecked)).To(MatchError(deleteConvertedSectionErr))

		deleteOrderSvc := newInMemoryService()
		deletedPage := edgePageNode("deleted-page", "deleted-page", "Deleted Page", deleteOrderSvc.tree)
		deleteOrderSvc.tree.Children = []*PageNode{deletedPage}
		deleteOrderSvc.rebuildIndexesLocked()
		swapTreeSeam(&treeStoreDeletePage, func(*NodeStore, *PageNode) error { return nil })
		deleteOrderErr := errors.New("delete order failed")
		swapTreeSeam(&treeStoreSaveChildOrder, func(*NodeStore, *PageNode) error {
			return deleteOrderErr
		})
		Expect(deleteOrderSvc.DeleteNode("user", deletedPage.ID, false, pageVersionUnchecked)).To(MatchError(deleteOrderErr))

		convertSvc := newInMemoryService()
		page := edgePageNode("page", "page", "Page", convertSvc.tree)
		section := edgeSectionNode("section", "section", "Section", convertSvc.tree)
		section.Children = []*PageNode{edgePageNode("section-child", "section-child", "Section Child", section)}
		convertSvc.tree.Children = []*PageNode{page, section}
		convertSvc.rebuildIndexesLocked()
		Expect(convertSvc.UpdateNode("user", "missing", "Missing", "missing", nil, pageVersionUnchecked, false)).To(MatchError(ErrPageNotFound))
		Expect(convertSvc.ConvertNode("user", "missing", NodeKindSection, pageVersionUnchecked)).To(MatchError(ErrPageNotFound))
		Expect(convertSvc.ConvertNode("user", "page", NodeKindPage, pageVersionUnchecked)).To(Succeed())
		Expect(convertSvc.ConvertNode("user", "section", NodeKindPage, pageVersionUnchecked)).To(MatchError(ErrPageHasChildren))
	})

	It("update, delete, convert, move, and sort operations propagate orchestration failures", func() {
		svc := newInMemoryService()
		page := edgePageNode("page", "page", "Page", svc.tree)
		page.Metadata.UpdatedAt = time.Now().UTC()
		section := edgeSectionNode("section", "section", "Section", svc.tree)
		child := edgePageNode("child", "child", "Child", section)
		section.Children = []*PageNode{child}
		child.Parent = section
		svc.tree.Children = []*PageNode{page, section}
		svc.rebuildIndexesLocked()

		Expect(svc.DeleteNode("user", "missing", false, pageVersionUnchecked)).To(MatchError(ErrPageNotFound))
		Expect(svc.DeleteNode("user", "section", false, pageVersionUnchecked)).To(MatchError(ErrPageHasChildren))

		deletePageErr := errors.New("delete page failed")
		swapTreeSeam(&treeStoreDeletePage, func(*NodeStore, *PageNode) error {
			return deletePageErr
		})
		Expect(svc.DeleteNode("user", "page", false, pageVersionUnchecked)).To(MatchError(deletePageErr))

		swapTreeSeam(&treeStoreDeletePage, func(*NodeStore, *PageNode) error { return nil })
		deleteSectionErr := errors.New("delete section failed")
		swapTreeSeam(&treeStoreDeleteSection, func(*NodeStore, *PageNode) error {
			return deleteSectionErr
		})
		Expect(svc.DeleteNode("user", "section", true, pageVersionUnchecked)).To(MatchError(deleteSectionErr))

		swapTreeSeam(&treeStoreDeleteSection, func(*NodeStore, *PageNode) error { return nil })
		unknown := edgePageNode("unknown", "unknown", "Unknown", svc.tree)
		unknown.Kind = NodeKind("unknown")
		svc.tree.Children = append(svc.tree.Children, unknown)
		svc.rebuildIndexesLocked()
		Expect(svc.DeleteNode("user", "unknown", false, pageVersionUnchecked)).To(matchInvalidOp("DeleteNode"))

		content := "body"
		plainContentErr := errors.New("plain failed")
		swapTreeSeam(&treeStoreUpsertContent, func(*NodeStore, *PageNode, string) error {
			return plainContentErr
		})
		Expect(svc.UpdateNode("user", "page", "Page", "page", &content, pageVersionUnchecked, false)).To(MatchError(plainContentErr))

		swapTreeSeam(&treeStoreUpsertContent, func(*NodeStore, *PageNode, string) error { return nil })
		preserveContentErr := errors.New("preserve failed")
		swapTreeSeam(&treeStoreUpsertContentPreservingFrontmatter, func(*NodeStore, *PageNode, string) error {
			return preserveContentErr
		})
		Expect(svc.UpdateNode("user", "page", "Page", "page", &content, pageVersionUnchecked, true)).To(MatchError(preserveContentErr))

		swapTreeSeam(&treeStoreUpsertContentPreservingFrontmatter, func(*NodeStore, *PageNode, string) error { return nil })
		replaceContentErr := errors.New("replace failed")
		swapTreeSeam(&treeStoreUpsertContentReplacingMetadata, func(*NodeStore, *PageNode, string) error {
			return replaceContentErr
		})
		Expect(svc.UpdateNodeReplacingMetadata("user", "page", "Page", "page", &content, pageVersionUnchecked)).To(MatchError(replaceContentErr))

		swapTreeSeam(&treeStoreUpsertContentReplacingMetadata, func(*NodeStore, *PageNode, string) error { return nil })
		renameErr := errors.New("rename failed")
		swapTreeSeam(&treeStoreRenameNode, func(*NodeStore, *PageNode, Slug) error {
			return renameErr
		})
		Expect(svc.UpdateNode("user", "page", "Page", "renamed", nil, pageVersionUnchecked, false)).To(MatchError(renameErr))

		swapTreeSeam(&treeStoreRenameNode, func(*NodeStore, *PageNode, Slug) error { return nil })
		syncErr := errors.New("sync failed")
		swapTreeSeam(&treeStoreSyncMetadataIfExists, func(*NodeStore, *PageNode) error {
			return syncErr
		})
		Expect(svc.UpdateNode("user", "page", "Page", "page", nil, pageVersionUnchecked, false)).To(MatchError(syncErr))
		Expect(svc.ConvertNode("user", "page", NodeKindSection, pageVersionUnchecked)).To(MatchError(syncErr))
		page.Kind = NodeKindPage

		swapTreeSeam(&treeStoreSyncMetadataIfExists, func(*NodeStore, *PageNode) error { return nil })
		convertErr := errors.New("convert failed")
		swapTreeSeam(&treeStoreConvertNode, func(*NodeStore, *PageNode, NodeKind) error {
			return convertErr
		})
		Expect(svc.ConvertNode("user", "page", NodeKindSection, pageVersionUnchecked)).To(MatchError(convertErr))

		swapTreeSeam(&treeStoreConvertNode, func(*NodeStore, *PageNode, NodeKind) error { return nil })
		Expect(svc.MoveNode("user", "missing", RootPageID, pageVersionUnchecked)).To(MatchError(ErrPageNotFound))
		Expect(svc.MoveNode("user", "page", "missing-parent", pageVersionUnchecked)).To(MatchError(ErrParentNotFound))
		Expect(svc.MoveNode("user", "page", "page", pageVersionUnchecked)).To(MatchError(ErrPageCannotBeMovedToItself))
		Expect(svc.MoveNode("user", "section", "child", pageVersionUnchecked)).To(MatchError(ErrMovePageCircularReference))

		moveErr := errors.New("move failed")
		swapTreeSeam(&treeStoreMoveNode, func(*NodeStore, *PageNode, *PageNode) error {
			return moveErr
		})
		Expect(svc.MoveNode("user", "page", "section", pageVersionUnchecked)).To(MatchError(moveErr))

		swapTreeSeam(&treeStoreMoveNode, func(*NodeStore, *PageNode, *PageNode) error { return nil })
		sourceOrderErr := errors.New("order failed")
		swapTreeSeam(&treeStoreSaveChildOrder, func(*NodeStore, *PageNode) error {
			return sourceOrderErr
		})
		Expect(svc.MoveNode("user", "page", "section", pageVersionUnchecked)).To(MatchError(sourceOrderErr))

		sortSvc := newInMemoryService()
		sortPage := edgePageNode("sort-page", "sort-page", "Sort Page", sortSvc.tree)
		sortSection := edgeSectionNode("sort-section", "sort-section", "Sort Section", sortSvc.tree)
		sortSvc.tree.Children = []*PageNode{sortPage, sortSection}
		sortSvc.rebuildIndexesLocked()
		Expect(sortSvc.SortPages("missing", nil)).To(MatchError(ErrParentNotFound))
		Expect(sortSvc.SortPages(RootPageID, []PageID{"only-one"})).To(MatchError(ErrInvalidSortOrder))
		Expect(sortSvc.SortPages(RootPageID, []PageID{"sort-page", "not-present"})).To(MatchError(ErrInvalidSortOrder))
		Expect(sortSvc.SortPages(RootPageID, []PageID{"sort-page", "sort-page"})).To(MatchError(ErrInvalidSortOrder))
	})

	It("batch, lookup, ensure, and move operations roll back through service seams", func() {
		svc := newInMemoryService()
		pageKind := NodeKindPage
		page := edgePageNode("page", "page", "Page", svc.tree)
		section := edgeSectionNode("section", "section", "Section", svc.tree)
		destPage := edgePageNode("dest-page", "dest-page", "Dest Page", svc.tree)
		svc.tree.Children = []*PageNode{page, section, destPage}
		svc.rebuildIndexesLocked()

		Expect(svc.BulkUpdateContent("user", nil)).To(BeEmpty())
		bulkErrs := svc.BulkUpdateContent("user", []BulkContentUpdate{{ID: "missing", Content: "body"}})
		Expect(bulkErrs).To(ConsistOf(MatchError(ErrPageNotFound)))
		pages, pageErrs := svc.GetPages(nil)
		Expect(pages).To(BeEmpty())
		Expect(pageErrs).To(BeEmpty())
		pages, pageErrs = svc.GetPages([]PageID{"missing"})
		Expect(pages).To(Equal([]*Page{nil}))
		Expect(pageErrs).To(ConsistOf(MatchError(ErrPageNotFound)))

		errFixtureRawFailed := errors.New("raw failed")
		swapTreeSeam(&treeStoreReadPageRaw, func(*NodeStore, *PageNode) (string, error) {
			return "", errFixtureRawFailed
		})
		_, err := svc.ReadPageRaw("page")
		Expect(err).To(MatchError(ErrGetPageRawContent))

		unloaded := NewTreeService(tempTreeDir())
		_, err = unloaded.FindPageByRoutePath("page")
		Expect(err).To(MatchError(ErrTreeNotLoaded))
		_, err = svc.LookupPagePath("")
		Expect(err).To(MatchError(ErrMissingRoutePath))
		_, err = svc.LookupPagePathForKind("", NodeKindPage)
		Expect(err).To(MatchError(ErrMissingRoutePath))

		swapTreeSeam(&treeStoreReadPageContent, func(*NodeStore, *PageNode) (string, error) {
			return "", errors.New("content failed")
		})
		_, err = svc.FindPageByRoutePath("page")
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
		emptyIDPage := edgePageNode("", "empty-id", "Empty ID", missingIDPageSvc.tree)
		missingIDPageSvc.tree.Children = []*PageNode{emptyIDPage}
		missingIDPageSvc.rebuildIndexesLocked()
		_, err = missingIDPageSvc.EnsurePagePath("user", "empty-id", "Empty ID", &pageKind)
		Expect(err).To(MatchError(ErrFindExistingPageByID))

		ensureSvc := newInMemoryService()
		swapTreeSeam(&treeGenerateUniqueID, func() (string, error) { return "", nil })
		swapTreeSeam(&treeStoreCreatePage, func(*NodeStore, *PageNode, *PageNode) error { return nil })
		swapTreeSeam(&treeStoreSaveChildOrder, func(*NodeStore, *PageNode) error { return nil })
		_, err = ensureSvc.EnsurePagePath("user", "created", "Created", &pageKind)
		Expect(err).To(MatchError(ErrFindCreatedPageByID))

		orphanSvc := newInMemoryService()
		orphan := edgePageNode("orphan", "orphan", "Orphan", nil)
		orphanSvc.nodesByID[orphan.ID] = orphan
		Expect(orphanSvc.MoveNode("user", "orphan", RootPageID, pageVersionUnchecked)).To(MatchError(ErrParentNotFound))

		convertDestErr := errors.New("convert dest failed")
		swapTreeSeam(&treeStoreConvertNode, func(*NodeStore, *PageNode, NodeKind) error {
			return convertDestErr
		})
		Expect(svc.MoveNode("user", "page", "dest-page", pageVersionUnchecked)).To(MatchError(convertDestErr))

		invalidDestSvc := newInMemoryService()
		movePage := edgePageNode("move-page", "move-page", "Move Page", invalidDestSvc.tree)
		invalidDest := edgeSectionNode("invalid-dest", "invalid-dest", "Invalid Dest", invalidDestSvc.tree)
		invalidDest.Kind = NodeKind("unknown")
		invalidDestSvc.tree.Children = []*PageNode{movePage, invalidDest}
		invalidDestSvc.rebuildIndexesLocked()
		Expect(invalidDestSvc.MoveNode("user", "move-page", "invalid-dest", pageVersionUnchecked)).To(MatchError(ErrParentMustBeSection))

		moveOrderSvc := newInMemoryService()
		movePage = edgePageNode("move-page", "move-page", "Move Page", moveOrderSvc.tree)
		moveDest := edgeSectionNode("move-dest", "move-dest", "Move Dest", moveOrderSvc.tree)
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
		Expect(moveOrderSvc.MoveNode("user", "move-page", "move-dest", pageVersionUnchecked)).To(MatchError(destinationOrderErr))

		moveSyncSvc := newInMemoryService()
		movePage = edgePageNode("move-page", "move-page", "Move Page", moveSyncSvc.tree)
		moveDest = edgeSectionNode("move-dest", "move-dest", "Move Dest", moveSyncSvc.tree)
		moveSyncSvc.tree.Children = []*PageNode{movePage, moveDest}
		moveSyncSvc.rebuildIndexesLocked()
		swapTreeSeam(&treeStoreSaveChildOrder, func(*NodeStore, *PageNode) error { return nil })
		swapTreeSeam(&treeStoreSyncMetadataIfExists, func(*NodeStore, *PageNode) error {
			return errors.New("sync moved failed")
		})
		Expect(moveSyncSvc.MoveNode("user", "move-page", "move-dest", pageVersionUnchecked)).To(MatchError(ErrSyncMovedNodeMetadata))

		rollbackSvc := newInMemoryService()
		oldParent := rollbackSvc.tree
		newParent := edgeSectionNode("new-parent", "new-parent", "New Parent", oldParent)
		node := edgePageNode("rolled", "rolled", "Rolled", newParent)
		previousOldChildren := []*PageNode{}
		previousNewChildren := []*PageNode{node}
		swapTreeSeam(&treeStoreMoveNode, func(*NodeStore, *PageNode, *PageNode) error {
			return errors.New("move back failed")
		})
		swapTreeSeam(&treeStoreSaveChildOrder, func(*NodeStore, *PageNode) error { return nil })
		err = rollbackSvc.rollbackMovedNodeLocked(node, oldParent, newParent, previousOldChildren, map[PageID]int{}, previousNewChildren, map[PageID]int{"rolled": 3}, 3, PageMetadata{}, false)
		Expect(err).To(MatchError(ErrMoveNodeBackOnDisk))

		swapTreeSeam(&treeStoreMoveNode, func(*NodeStore, *PageNode, *PageNode) error { return nil })
		convertedParent := edgeSectionNode("converted-parent", "converted-parent", "Converted Parent", oldParent)
		convertedParent.WorkspaceSourcePath = "../outside"
		err = rollbackSvc.rollbackMovedNodeLocked(node, oldParent, convertedParent, nil, map[PageID]int{}, nil, map[PageID]int{}, 0, PageMetadata{}, true)
		Expect(err).To(MatchError(ErrResolveConvertedParentDir))

		swapTreeSeam(&treeFilepathRel, filepath.Rel)
		swapTreeSeam(&treeStoreConvertNode, func(*NodeStore, *PageNode, NodeKind) error { return nil })
		convertedParent = edgeSectionNode("converted-parent", "converted-parent", "Converted Parent", oldParent)
		swapTreeSeam(&treeOSRemoveAll, func(string) error {
			return errors.New("remove order failed")
		})
		err = rollbackSvc.rollbackMovedNodeLocked(node, oldParent, convertedParent, nil, map[PageID]int{}, nil, map[PageID]int{}, 0, PageMetadata{}, true)
		Expect(err).To(MatchError(ErrRemoveChildOrderBeforeParentRollback))

		convertedParent = edgeSectionNode("converted-parent", "converted-parent", "Converted Parent", oldParent)
		swapTreeSeam(&treeOSRemoveAll, func(string) error { return nil })
		swapTreeSeam(&treeStoreConvertNode, func(*NodeStore, *PageNode, NodeKind) error {
			return errors.New("convert back failed")
		})
		err = rollbackSvc.rollbackMovedNodeLocked(node, oldParent, convertedParent, nil, map[PageID]int{}, nil, map[PageID]int{}, 0, PageMetadata{}, true)
		Expect(err).To(MatchError(ErrConvertDestinationParentBackToPage))

		convertedParent = edgeSectionNode("converted-parent", "converted-parent", "Converted Parent", oldParent)
		swapTreeSeam(&treeStoreConvertNode, func(*NodeStore, *PageNode, NodeKind) error { return nil })
		Expect(rollbackSvc.rollbackMovedNodeLocked(node, oldParent, convertedParent, nil, map[PageID]int{}, nil, map[PageID]int{}, 0, PageMetadata{}, true)).To(Succeed())
		Expect(convertedParent.Kind).To(Equal(NodeKindPage))
	})
})
