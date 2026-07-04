package tree

import (
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"time"
)

// EnsurePagePath ensures that a given path exists in the tree
// It creates any missing segments as needed
// Returns the final page node and a list of created nodes
func (t *TreeService) EnsurePagePath(userID UserID, p RoutePath, targetTitle string, kind *NodeKind) (*EnsurePathResult, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.tree == nil {
		return nil, ErrTreeNotLoaded
	}

	created := []*PageNode{}
	requestedKind := NodeKindPage
	if kind != nil {
		requestedKind = *kind
	}

	lookup, err := t.lookupPagePathLocked(p, requestedKind)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrLookupPagePath, err)
	}

	// Path exists -> return existing
	if lookup.Exists && len(lookup.Segments) > 0 {
		last := lookup.Segments[len(lookup.Segments)-1]
		if last.Kind != nil && *last.Kind == requestedKind {
			page := t.getNodeByIDLocked(*last.ID)
			if page == nil {
				return nil, fmt.Errorf("%w: %w", ErrFindExistingPageByID, ErrPageNotFound)
			}
			return &EnsurePathResult{Exists: true, Page: page}, nil
		}
	}

	// Create missing segments
	var currentID *PageID // nil means root
	for i, segment := range lookup.Segments {
		isFinalSegment := i == len(lookup.Segments)-1
		if segment.Exists && !(isFinalSegment && segment.Kind != nil && *segment.Kind != requestedKind) {
			currentID = segment.ID
			continue
		}

		// Title
		segTitle := segment.Slug.FilesystemPath()
		if i == len(lookup.Segments)-1 {
			segTitle = targetTitle
		}

		// Kind: intermediate segments are sections, last segment uses provided kind (or page/section default)
		kindToUse := NodeKindSection
		if isFinalSegment {
			kindToUse = requestedKind
		}

		createdNode, err := t.createNodeLocked(userID, currentID, segTitle, segment.Slug, &kindToUse, createNodeOptions{})
		if err != nil {
			return nil, fmt.Errorf("%w %q: %w", ErrCreateSegment, segment.Slug, err)
		}
		currentID = &createdNode.id

		created = append(created, &PageNode{
			ID:    createdNode.id,
			Slug:  segment.Slug,
			Title: segTitle,
			Kind:  kindToUse,
		})
	}

	// Resolve final page
	if currentID == nil {
		return nil, ErrEnsurePagePath
	}
	page := t.getNodeByIDLocked(*currentID)
	if page == nil {
		return nil, fmt.Errorf("%w: %w", ErrFindCreatedPageByID, ErrPageNotFound)
	}

	// Save once
	return &EnsurePathResult{
		Exists:  true,
		Page:    page,
		Created: created,
	}, nil
}

func (t *TreeService) findNodeByRoutePathAndKindLocked(routePath RoutePath, finalKind NodeKind) *PageNode {
	path := routePath.Clean()
	if path == "" {
		return nil
	}

	parts := path.Segments()
	parent := t.tree
	for index, part := range parts {
		var node *PageNode
		if index == len(parts)-1 {
			node = t.findChildBySlugAndKindExactInParentLocked(parent, part, finalKind)
		} else {
			node = t.findRouteSegmentInParentLocked(parent, part)
		}
		if node == nil {
			return nil
		}
		parent = node
	}
	return parent
}

// MoveNode moves a node to another parent (root if parentID is empty/"root")
func (t *TreeService) MoveNode(userID UserID, id PageID, parentID PageID, expectedVersion PageVersion) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.tree == nil {
		return ErrTreeNotLoaded
	}

	// Find node to move
	node := t.getNodeByIDLocked(id)
	if node == nil {
		return ErrPageNotFound
	}

	if err := checkNodeVersion(node, expectedVersion); err != nil {
		return err
	}

	oldParent := node.Parent
	if oldParent == nil {
		return fmt.Errorf("old parent not found: %w", ErrParentNotFound)
	}

	// Resolve destination parent (default root)
	newParent := t.tree
	if parentID != "" && parentID != "root" {
		newParent = t.getNodeByIDLocked(parentID)
		if newParent == nil {
			return fmt.Errorf("new parent not found: %w", ErrParentNotFound)
		}
	}

	// Same slug collision under new parent
	if existing := t.findChildBySlugAndKindInParentLocked(newParent, node.Slug, node.Kind); existing != nil && existing.ID != node.ID {
		return fmt.Errorf("child with the same slug already exists: %w", ErrPageAlreadyExists)
	}

	// Can't move into itself
	if node.ID == newParent.ID {
		return fmt.Errorf("page cannot be moved to itself: %w", ErrPageCannotBeMovedToItself)
	}

	// Circular reference guard: node cannot be moved under its own descendants
	if node.IsChildOf(newParent.ID, true) {
		return fmt.Errorf("circular reference detected: %w", ErrMovePageCircularReference)
	}

	newParentWasConverted := false
	if newParent.ID != "root" && newParent.Kind == NodeKindPage {
		if err := treeStoreConvertNode(t.store, newParent, NodeKindSection); err != nil {
			return fmt.Errorf("could not auto-convert new parent page to section: %w", err)
		}
		newParent.Kind = NodeKindSection
		newParentWasConverted = true
	}

	if newParent.Kind != NodeKindSection {
		return fmt.Errorf("%w, got %q", ErrParentMustBeSection, newParent.Kind)
	}

	previousOldChildren := append([]*PageNode(nil), oldParent.Children...)
	previousOldPositions := snapshotChildPositions(oldParent.Children)
	previousNewChildren := append([]*PageNode(nil), newParent.Children...)
	previousNewPositions := snapshotChildPositions(newParent.Children)
	previousPosition := node.Position
	previousMetadata := node.Metadata

	if err := treeStoreMoveNode(t.store, node, newParent); err != nil {
		return fmt.Errorf("could not move node on disk: %w", err)
	}

	for i, e := range oldParent.Children {
		if e.ID == id {
			oldParent.Children = append(oldParent.Children[:i], oldParent.Children[i+1:]...)
			break
		}
	}

	node.Position = len(newParent.Children)
	newParent.Children = append(newParent.Children, node)
	node.Parent = newParent
	t.rebuildChildSlugIndexForParentLocked(oldParent)
	t.rebuildChildSlugIndexForParentLocked(newParent)
	node.Metadata.UpdatedAt = time.Now().UTC()
	node.Metadata.LastAuthorID = userID

	t.reindexPositions(newParent)
	t.reindexPositions(oldParent)

	if err := treeStoreSaveChildOrder(t.store, oldParent); err != nil {
		rollbackErr := t.rollbackMovedNodeLocked(node, oldParent, newParent, previousOldChildren, previousOldPositions, previousNewChildren, previousNewPositions, previousPosition, previousMetadata, newParentWasConverted)
		if rollbackErr != nil {
			return errors.Join(fmt.Errorf("%w: %w", ErrPersistSourceChildOrder, err), fmt.Errorf("%w: %w", ErrRollbackMovedNode, rollbackErr))
		}
		return fmt.Errorf("%w: %w", ErrPersistSourceChildOrder, err)
	}
	if newParent != oldParent {
		if err := treeStoreSaveChildOrder(t.store, newParent); err != nil {
			rollbackErr := t.rollbackMovedNodeLocked(node, oldParent, newParent, previousOldChildren, previousOldPositions, previousNewChildren, previousNewPositions, previousPosition, previousMetadata, newParentWasConverted)
			if rollbackErr != nil {
				return errors.Join(fmt.Errorf("%w: %w", ErrPersistDestinationChildOrder, err), fmt.Errorf("%w: %w", ErrRollbackMovedNode, rollbackErr))
			}
			return fmt.Errorf("%w: %w", ErrPersistDestinationChildOrder, err)
		}
	}

	if err := treeStoreSyncMetadataIfExists(t.store, node); err != nil {
		rollbackErr := t.rollbackMovedNodeLocked(node, oldParent, newParent, previousOldChildren, previousOldPositions, previousNewChildren, previousNewPositions, previousPosition, previousMetadata, newParentWasConverted)
		if rollbackErr != nil {
			return errors.Join(fmt.Errorf("%w: %w", ErrSyncMovedNodeMetadata, err), fmt.Errorf("%w: %w", ErrRollbackMovedNode, rollbackErr))
		}
		return fmt.Errorf("%w: %w", ErrSyncMovedNodeMetadata, err)
	}

	return nil
}

func (t *TreeService) MoveNodeUncheckedVersion(userID UserID, id PageID, parentID PageID) error {
	return t.MoveNode(userID, id, parentID, pageVersionUnchecked)
}

func snapshotChildPositions(children []*PageNode) map[PageID]int {
	positions := make(map[PageID]int, len(children))
	for _, child := range children {
		if child == nil {
			continue
		}
		positions[child.ID] = child.Position
	}
	return positions
}

func restoreChildSnapshot(parent *PageNode, children []*PageNode, positions map[PageID]int) {
	if parent == nil {
		return
	}

	parent.Children = append([]*PageNode(nil), children...)
	for _, child := range parent.Children {
		if child == nil {
			continue
		}
		child.Parent = parent
		if pos, ok := positions[child.ID]; ok {
			child.Position = pos
		}
	}
}

func (t *TreeService) rollbackMovedNodeLocked(node *PageNode, oldParent *PageNode, newParent *PageNode, previousOldChildren []*PageNode, previousOldPositions map[PageID]int, previousNewChildren []*PageNode, previousNewPositions map[PageID]int, previousPosition int, previousMetadata PageMetadata, newParentWasConverted bool) error {
	var rollbackErr error

	if moveErr := treeStoreMoveNode(t.store, node, oldParent); moveErr != nil {
		rollbackErr = errors.Join(rollbackErr, fmt.Errorf("%w: %w", ErrMoveNodeBackOnDisk, moveErr))
	}

	restoreChildSnapshot(oldParent, previousOldChildren, previousOldPositions)
	if newParent != oldParent {
		restoreChildSnapshot(newParent, previousNewChildren, previousNewPositions)
	}
	t.rebuildChildSlugIndexForParentLocked(oldParent)
	if newParent != oldParent {
		t.rebuildChildSlugIndexForParentLocked(newParent)
	}
	node.Parent = oldParent
	node.Position = previousPosition
	node.Metadata = previousMetadata

	if newParentWasConverted && newParent != nil && newParent.ID != "root" && len(newParent.Children) == 0 {
		newParentDir, dirErr := t.store.sectionDirPathForNode(newParent, "rollbackMovedNode")
		if dirErr != nil {
			rollbackErr = errors.Join(rollbackErr, fmt.Errorf("%w: %w", ErrResolveConvertedParentDir, dirErr))
		} else {
			if removeErr := treeOSRemoveAll(filepath.Join(newParentDir, orderFilename)); removeErr != nil {
				rollbackErr = errors.Join(rollbackErr, fmt.Errorf("%w: %w", ErrRemoveChildOrderBeforeParentRollback, removeErr))
			}
		}
		if convertErr := treeStoreConvertNode(t.store, newParent, NodeKindPage); convertErr != nil {
			rollbackErr = errors.Join(rollbackErr, fmt.Errorf("%w: %w", ErrConvertDestinationParentBackToPage, convertErr))
		} else {
			newParent.Kind = NodeKindPage
		}
	}

	if err := treeStoreSaveChildOrder(t.store, oldParent); err != nil {
		rollbackErr = errors.Join(rollbackErr, fmt.Errorf("restore source child order: %w", err))
	}
	if newParent != oldParent && newParent.Kind == NodeKindSection {
		if err := treeStoreSaveChildOrder(t.store, newParent); err != nil {
			rollbackErr = errors.Join(rollbackErr, fmt.Errorf("restore destination child order: %w", err))
		}
	}

	return rollbackErr
}

func (t *TreeService) SortPages(parentID PageID, orderedIDs []PageID) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.tree == nil {
		return ErrTreeNotLoaded
	}

	parent := t.tree

	if parentID != "" && parentID != "root" {
		parent = t.getNodeByIDLocked(parentID)
		if parent == nil {
			return ErrParentNotFound
		}
	}

	// Check if the number of orderedIDs is the same as the number of children
	if len(orderedIDs) != len(parent.Children) {
		return fmt.Errorf("number of ordered IDs does not match the number of children: %w", ErrInvalidSortOrder)
	}

	// Check if all IDs in the sort order are valid
	existingIDs := make(map[PageID]bool)
	for _, child := range parent.Children {
		existingIDs[child.ID] = true
	}
	for _, id := range orderedIDs {
		if !existingIDs[id] {
			return fmt.Errorf("invalid ID in sort order, ID: %s - %w", id, ErrInvalidSortOrder)
		}
	}

	seen := make(map[PageID]bool)
	for _, id := range orderedIDs {
		if seen[id] {
			return fmt.Errorf("duplicate ID in sort order: %s: %w", id, ErrInvalidSortOrder)
		}
		seen[id] = true
	}

	previousChildren := append([]*PageNode(nil), parent.Children...)
	previousPositions := make(map[PageID]int, len(parent.Children))
	for _, child := range parent.Children {
		previousPositions[child.ID] = child.Position
	}

	// Create a map to store the position of each page
	positions := make(map[PageID]int)
	for i, id := range orderedIDs {
		positions[id] = i
	}

	// Sort the children of the parent
	sort.SliceStable(parent.Children, func(i, j int) bool {
		return positions[parent.Children[i].ID] < positions[parent.Children[j].ID]
	})

	// write postion index to children
	for i, child := range parent.Children {
		child.Position = i
	}

	// Reindex the positions
	t.reindexPositions(parent)

	if err := treeStoreSaveChildOrder(t.store, parent); err != nil {
		parent.Children = previousChildren
		for _, child := range parent.Children {
			if pos, ok := previousPositions[child.ID]; ok {
				child.Position = pos
			}
		}
		return fmt.Errorf("could not persist child order: %w", err)
	}

	return nil
}

func (t *TreeService) reindexPositions(parent *PageNode) {
	sort.SliceStable(parent.Children, func(i, j int) bool {
		return parent.Children[i].Position < parent.Children[j].Position
	})
	for i, child := range parent.Children {
		child.Position = i
	}
}
