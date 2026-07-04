package tree

import (
	"fmt"
	"path/filepath"
	"time"
)

func (t *TreeService) findChildBySlugExactInParentLocked(parent *PageNode, slug Slug) *PageNode {
	if parent == nil {
		return nil
	}

	for _, child := range parent.Children {
		if child != nil && child.Slug == slug {
			return child
		}
	}

	return nil
}

func (t *TreeService) findChildBySlugAndKindInParentLocked(parent *PageNode, slug Slug, kind NodeKind) *PageNode {
	if parent == nil {
		return nil
	}

	for _, child := range parent.Children {
		if child != nil && child.Slug.EqualFold(slug) && child.Kind == kind {
			return child
		}
	}

	return nil
}

func (t *TreeService) findRouteSegmentInParentLocked(parent *PageNode, slug Slug) *PageNode {
	if section := t.findChildBySlugAndKindInParentLocked(parent, slug, NodeKindSection); section != nil {
		return section
	}
	return t.findChildBySlugInParentLocked(parent, slug)
}

func (t *TreeService) findRouteSegmentExactInParentLocked(parent *PageNode, slug Slug) *PageNode {
	if section := t.findChildBySlugAndKindExactInParentLocked(parent, slug, NodeKindSection); section != nil {
		return section
	}
	return t.findChildBySlugExactInParentLocked(parent, slug)
}

func (t *TreeService) findChildBySlugAndKindExactInParentLocked(parent *PageNode, slug Slug, kind NodeKind) *PageNode {
	if parent == nil {
		return nil
	}

	for _, child := range parent.Children {
		if child != nil && child.Slug == slug && child.Kind == kind {
			return child
		}
	}

	return nil
}

// DeleteNode deletes a node from the tree
func (t *TreeService) DeleteNode(userID UserID, id PageID, recursive bool, expectedVersion PageVersion) error {
	err := t.withLockedTree(func() error {
		if t.tree == nil {
			return ErrTreeNotLoaded
		}

		// Find the node to delete
		node := t.getNodeByIDLocked(id)
		if node == nil {
			return ErrPageNotFound
		}

		if err := checkNodeVersion(node, expectedVersion); err != nil {
			return err
		}

		// Check if node has children
		if node.HasChildren() && !recursive {
			return ErrPageHasChildren
		}

		// Delete the node from the parent
		parent := node.Parent
		if parent == nil {
			return ErrParentNotFound
		}

		switch node.Kind {
		case NodeKindSection:
			if err := treeStoreDeleteSection(t.store, node); err != nil {
				return fmt.Errorf("could not delete section entry: %w", err)
			}
		case NodeKindPage:
			if node.HasChildren() {
				// This should not happen due to earlier check, but just in case
				// Convert to section and delete recursively
				t.log.Info("converting page to section for recursive delete", "pageID", node.ID)
				if err := treeStoreConvertNode(t.store, node, NodeKindSection); err != nil {
					return fmt.Errorf("could not convert page to section: %w", err)
				}
				node.Kind = NodeKindSection
				if err := treeStoreDeleteSection(t.store, node); err != nil {
					return fmt.Errorf("could not delete section entry: %w", err)
				}
			} else {
				if err := treeStoreDeletePage(t.store, node); err != nil {
					return fmt.Errorf("could not delete page entry: %w", err)
				}
			}
		default:
			return &InvalidOpError{Op: "DeleteNode", Reason: fmt.Sprintf("unknown node kind: %v", node.Kind)}
		}

		// Remove the page from the parent
		for i, e := range parent.Children {
			if e.ID == id {
				parent.Children = append(parent.Children[:i], parent.Children[i+1:]...)
				break
			}
		}
		t.removeNodeIndexLocked(node)

		t.reindexPositions(parent)
		if err := treeStoreSaveChildOrder(t.store, parent); err != nil {
			return fmt.Errorf("could not persist child order: %w", err)
		}
		return nil
	})
	return err
}

func (t *TreeService) DeleteNodeUncheckedVersion(userID UserID, id PageID, recursive bool) error {
	return t.DeleteNode(userID, id, recursive, pageVersionUnchecked)
}

type contentUpdateMode int

const (
	contentUpdatePlain contentUpdateMode = iota
	contentUpdatePreserveMetadata
	contentUpdateReplaceMetadata
)

// UpdateNode updates a node (page/section) in the tree and syncs disk state via NodeStore.
func (t *TreeService) UpdateNode(userID UserID, id PageID, title string, slug Slug, content *string, expectedVersion PageVersion, fromImport bool) error {
	mode := contentUpdatePlain
	if fromImport {
		mode = contentUpdatePreserveMetadata
	}
	return t.updateNode(userID, id, title, slug, content, expectedVersion, mode)
}

func (t *TreeService) UpdateNodeUncheckedVersion(userID UserID, id PageID, title string, slug Slug, content *string, fromImport bool) error {
	return t.UpdateNode(userID, id, title, slug, content, pageVersionUnchecked, fromImport)
}

func (t *TreeService) UpdateNodeReplacingMetadata(userID UserID, id PageID, title string, slug Slug, content *string, expectedVersion PageVersion) error {
	return t.updateNode(userID, id, title, slug, content, expectedVersion, contentUpdateReplaceMetadata)
}

func (t *TreeService) UpdateNodeReplacingMetadataUncheckedVersion(userID UserID, id PageID, title string, slug Slug, content *string) error {
	return t.UpdateNodeReplacingMetadata(userID, id, title, slug, content, pageVersionUnchecked)
}

func (t *TreeService) updateNode(userID UserID, id PageID, title string, slug Slug, content *string, expectedVersion PageVersion, mode contentUpdateMode) error {
	return t.withLockedTree(func() error {
		if t.tree == nil {
			return ErrTreeNotLoaded
		}

		// Find node
		node := t.getNodeByIDLocked(id)
		if node == nil {
			return ErrPageNotFound
		}

		if err := checkNodeVersion(node, expectedVersion); err != nil {
			return err
		}
		if err := validateNodeSlug("UpdateNode", slug); err != nil {
			return err
		}

		// Slug must be unique under same parent (when changed)
		if slug != node.Slug && node.Parent != nil {
			existing := t.findChildBySlugAndKindInParentLocked(node.Parent, slug, node.Kind)
			if existing != nil && existing.ID != node.ID {
				return ErrPageAlreadyExists
			}
		}

		// Content update?
		if content != nil {
			t.log.Info("updating node content", "nodeID", node.ID)
			var upsertErr error
			switch mode {
			case contentUpdatePreserveMetadata:
				upsertErr = treeStoreUpsertContentPreservingFrontmatter(t.store, node, *content)
			case contentUpdateReplaceMetadata:
				upsertErr = treeStoreUpsertContentReplacingMetadata(t.store, node, *content)
			default:
				upsertErr = treeStoreUpsertContent(t.store, node, *content)
			}
			if upsertErr != nil {
				return fmt.Errorf("could not upsert content: %w", upsertErr)
			}
		}

		// Rename slug on disk (must happen while node still has old slug)
		if slug != node.Slug {
			t.log.Info("renaming node slug", "nodeID", node.ID, "oldSlug", node.Slug, "newSlug", slug)
			if err := treeStoreRenameNode(t.store, node, slug); err != nil {
				return fmt.Errorf("could not rename node: %w", err)
			}
			node.Slug = slug
			if node.Parent != nil {
				t.rebuildChildSlugIndexForParentLocked(node.Parent)
			}
		}

		// Update title in tree
		node.Title = title

		// Update metadata
		node.Metadata.UpdatedAt = time.Now().UTC()
		node.Metadata.LastAuthorID = userID

		// Keep metadata in sync if the file exists (important when title
		// changed but content == nil).
		if err := treeStoreSyncMetadataIfExists(t.store, node); err != nil {
			return fmt.Errorf("could not sync metadata: %w", err)
		}

		// Save tree
		return nil
	})

}

func (t *TreeService) ConvertNode(userID UserID, id PageID, kind NodeKind, expectedVersion PageVersion) error {
	return t.withLockedTree(func() error {
		if t.tree == nil {
			return ErrTreeNotLoaded
		}

		// Find node
		node := t.getNodeByIDLocked(id)
		if node == nil {
			return ErrPageNotFound
		}

		if err := checkNodeVersion(node, expectedVersion); err != nil {
			return err
		}

		if node.Kind == kind {
			// No change
			return nil
		}

		// Section -> Page only allowed if no children
		if node.Kind == NodeKindSection && kind == NodeKindPage && node.HasChildren() {
			return ErrPageHasChildren
		}

		t.log.Info("changing node kind", "nodeID", node.ID, "oldKind", node.Kind, "newKind", kind)

		if err := treeStoreConvertNode(t.store, node, kind); err != nil {
			return fmt.Errorf("could not convert node: %w", err)
		}
		node.Kind = kind

		// Update metadata
		node.Metadata.UpdatedAt = time.Now().UTC()
		node.Metadata.LastAuthorID = userID

		// Keep metadata in sync if the file exists (important when kind
		// changed but content == nil).
		if err := treeStoreSyncMetadataIfExists(t.store, node); err != nil {
			return fmt.Errorf("could not sync metadata: %w", err)
		}

		// Save tree
		return nil
	})
}

func (t *TreeService) ConvertNodeUncheckedVersion(userID UserID, id PageID, kind NodeKind) error {
	return t.ConvertNode(userID, id, kind, pageVersionUnchecked)
}

// GetTree returns the tree
func (t *TreeService) GetTree() *PageNode {
	t.mu.RLock()
	defer t.mu.RUnlock()

	return t.tree
}

// RootDir returns the configured filesystem root that backs Markdown content.
func (t *TreeService) RootDir() string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.rootDir
}

// ContentPathForNode returns the Markdown path backing a tree node, relative to
// the configured root dir.
func (t *TreeService) ContentPathForNode(node *PageNode) (string, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	contentPath, err := t.store.contentPathForNodeRead(node)
	if err != nil {
		return "", err
	}
	rel, err := treeFilepathRel(t.rootDir, contentPath)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(rel), nil
}

// IsLoaded reports whether the tree has been loaded into memory.
func (t *TreeService) IsLoaded() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.tree != nil
}

// HasPages reports whether the tree contains at least one non-root node.
func (t *TreeService) HasPages() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.tree != nil && len(t.tree.Children) > 0
}

// WalkNodes calls fn with the ID of every non-root node (pages and sections)
// in depth-first order. The read lock is held only while collecting IDs; fn
// is called without any lock held so it may safely call other TreeService
// methods. Returns nil immediately when the tree is not yet loaded.
func (t *TreeService) WalkNodes(fn func(id PageID) error) error {
	ids := t.collectIDsDFS()
	for _, id := range ids {
		if err := fn(id); err != nil {
			return err
		}
	}
	return nil
}

// collectIDsDFS returns the IDs of all non-root nodes in depth-first order
// under the read lock.
func (t *TreeService) collectIDsDFS() []PageID {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if t.tree == nil {
		return nil
	}

	var ids []PageID
	var collect func(*PageNode)
	collect = func(node *PageNode) {
		if node.ID != "root" {
			ids = append(ids, node.ID)
		}
		for _, child := range node.Children {
			collect(child)
		}
	}
	collect(t.tree)
	return ids
}

// BulkContentUpdate is a single item for BulkUpdateContent.
type BulkContentUpdate struct {
	ID      PageID
	Content string
}
