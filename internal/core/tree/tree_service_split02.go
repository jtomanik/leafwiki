package tree

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

func directoryHasEntries(dir string) (bool, error) {
	entries, err := treeOSReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("%w %s: %w", ErrReadDirectory, dir, err)
	}
	return len(entries) > 0, nil
}

func sameCleanPath(a string, b string) bool {
	absA, errA := treeFilepathAbs(filepath.Clean(a))
	absB, errB := treeFilepathAbs(filepath.Clean(b))
	if errA == nil && errB == nil {
		return absA == absB
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

func (t *TreeService) withLockedTree(fn func() error) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	return fn()
}

func (t *TreeService) withRLockedTree(fn func() error) error {
	t.mu.RLock()
	defer t.mu.RUnlock()

	return fn()
}

// TreeHash returns the current hash of the tree
func (t *TreeService) TreeHash() string {
	var hash string
	_ = t.withRLockedTree(func() error {
		hash = t.tree.Hash()
		return nil
	})
	return hash
}

// ReconstructTreeFromFS reconstructs the tree from the filesystem
func (t *TreeService) ReconstructTreeFromFS() error {
	return t.withLockedTree(t.reconstructTreeFromFSLocked)
}

func (t *TreeService) reconstructTreeFromFSLocked() error {
	// Reconstruct the tree from the filesystem
	// This is a more complex operation and may involve reading the filesystem structure
	newTree, err := treeStoreReconstructTreeFromFS(t.store)
	if err != nil {
		t.log.Error("Error reconstructing tree from filesystem", "error", err)
		return err
	}

	// Defensive check to protect against unexpected nil returns from ReconstructTreeFromFS
	if newTree == nil {
		return fmt.Errorf("%w: ReconstructTreeFromFS", ErrTreeReconstructionNil)
	}

	// Save the old tree in case we need to revert
	// Note: oldTree may be nil if this is the first reconstruction (which is expected)
	oldTree := t.tree
	t.tree = newTree
	t.rebuildIndexesLocked()

	// Reconstructed nodes already carry metadata from canonical files, legacy
	// migration input, or safe defaults.

	if err := treeSaveSchema(t.dataDir, CurrentSchemaVersion); err != nil {
		t.log.Error("Error saving schema after reconstruction", "error", err)
		t.tree = oldTree
		t.rebuildIndexesLocked()
		return err
	}

	return nil
}

type createNodeResult struct {
	id                 PageID
	entry              *PageNode
	parent             *PageNode
	parentWasConverted bool
}

type createNodeOptions struct {
	existingID PageID
}

// Create Node adds a new node to the tree
func (t *TreeService) CreateNode(userID UserID, parentID *PageID, title string, slug Slug, nodeKind *NodeKind) (*PageID, error) {
	var result *PageID
	err := t.withLockedTree(func() error {
		created, err := t.createNodeLocked(userID, parentID, title, slug, nodeKind, createNodeOptions{})
		if err != nil {
			return err
		}
		result = &created.id

		return nil
	})

	return result, err
}

func (t *TreeService) RestoreNode(userID UserID, id PageID, parentID *PageID, title string, slug Slug, nodeKind NodeKind, content string, metadata PageMetadata) (*Page, error) {
	var restored *Page
	err := t.withLockedTree(func() error {
		kind := nodeKind
		created, err := t.createNodeLocked(userID, parentID, title, slug, &kind, createNodeOptions{existingID: id})
		if err != nil {
			return err
		}

		if err := treeStoreUpsertContent(t.store, created.entry, content); err != nil {
			return fmt.Errorf("%w: %w", ErrRestoreContent, err)
		}

		created.entry.Metadata = metadata
		created.entry.Metadata.UpdatedAt = metadata.UpdatedAt.UTC()
		created.entry.Metadata.CreatedAt = metadata.CreatedAt.UTC()
		created.entry.Metadata.CreatorID = metadata.CreatorID
		created.entry.Metadata.LastAuthorID = metadata.LastAuthorID
		if err := treeStoreSyncMetadataIfExists(t.store, created.entry); err != nil {
			return fmt.Errorf("%w: %w", ErrSyncRestoredMetadata, err)
		}

		restored = &Page{PageNode: created.entry, Content: content}
		return nil
	})
	return restored, err
}

// createNodeLocked creates a new node under the given parent.
// Lock must be held by the caller.
func (t *TreeService) createNodeLocked(userID UserID, parentID *PageID, title string, slug Slug, kind *NodeKind, opts createNodeOptions) (*createNodeResult, error) {
	if t.tree == nil {
		return nil, ErrTreeNotLoaded
	}
	if err := validateNodeSlug("CreateNode", slug); err != nil {
		return nil, err
	}

	// Decide which kind we create
	k := NodeKindPage
	if kind != nil {
		k = *kind
	}

	// Resolve the parent
	parent := t.tree
	if parentID != nil && *parentID != "" && *parentID != "root" {
		parent = t.getNodeByIDLocked(*parentID)
		if parent == nil {
			return nil, ErrParentNotFound
		}
	}

	// Same-basename page and section twins are valid because they map to
	// distinct filesystem entries (<slug>.md and <slug>/index.md).
	if t.findChildBySlugAndKindInParentLocked(parent, slug, k) != nil {
		return nil, ErrPageAlreadyExists
	}

	parentWasConverted := false

	// Check if the current parent is a section
	// if not, we need to convert it to a section
	if parent.Kind != NodeKindSection && parent.ID != "root" {
		t.log.Info("converting parent to section", "parentID", parent.ID, "oldKind", parent.Kind, "newKind", NodeKindSection)
		if err := treeStoreConvertNode(t.store, parent, NodeKindSection); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrConvertParentNode, err)
		}
		parent.Kind = NodeKindSection
		parentWasConverted = true
	}

	if parent.Kind != NodeKindSection {
		return nil, fmt.Errorf("%w, got %q", ErrParentMustBeSection, parent.Kind)
	}

	id := opts.existingID
	if id == "" {
		var err error
		rawID, err := treeGenerateUniqueID()
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrGenerateUniqueID, err)
		}
		id = PageIDFromString(rawID)
	} else if existing := t.getNodeByIDLocked(id); existing != nil {
		return nil, fmt.Errorf("%w: %s", ErrPageAlreadyExists, id)
	}

	now := time.Now().UTC()

	entry := &PageNode{
		ID:       id,
		Title:    title,
		Parent:   parent,
		Slug:     slug,
		Kind:     k,
		Position: len(parent.Children), // Set the position to the end of the list
		Children: []*PageNode{},
		Metadata: PageMetadata{
			CreatedAt:    now,
			UpdatedAt:    now,
			CreatorID:    userID,
			LastAuthorID: userID,
		},
	}

	// Create on disk depending on kind
	switch k {
	case NodeKindPage:
		if err := treeStoreCreatePage(t.store, parent, entry); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrCreatePageEntry, err)
		}
	case NodeKindSection:
		if err := treeStoreCreateSection(t.store, parent, entry); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrCreateSectionEntry, err)
		}
	}

	// Add the new page to the parent
	parent.Children = append(parent.Children, entry)
	t.indexNodeLocked(entry)
	if err := treeStoreSaveChildOrder(t.store, parent); err != nil {
		rollbackErr := t.rollbackCreatedNodeLocked(parent, entry, parentWasConverted)
		if rollbackErr != nil {
			return nil, errors.Join(fmt.Errorf("%w: %w", ErrPersistChildOrder, err), fmt.Errorf("%w: %w", ErrRollbackCreatedNode, rollbackErr))
		}
		return nil, fmt.Errorf("%w: %w", ErrPersistChildOrder, err)
	}
	return &createNodeResult{
		id:                 entry.ID,
		entry:              entry,
		parent:             parent,
		parentWasConverted: parentWasConverted,
	}, nil
}

func (t *TreeService) rollbackCreatedNodeLocked(parent *PageNode, entry *PageNode, parentWasConverted bool) error {
	if parent == nil || entry == nil {
		return nil
	}

	for i, child := range parent.Children {
		if child == entry {
			parent.Children = append(parent.Children[:i], parent.Children[i+1:]...)
			break
		}
	}
	t.removeNodeIndexLocked(entry)

	switch entry.Kind {
	case NodeKindSection:
		if err := treeStoreDeleteSection(t.store, entry); err != nil {
			return err
		}
	case NodeKindPage:
		if err := treeStoreDeletePage(t.store, entry); err != nil {
			return err
		}
	}

	if parentWasConverted && len(parent.Children) == 0 {
		orderPath, err := t.store.sectionDirPathForNode(parent, "rollbackCreatedNode")
		if err != nil {
			return err
		}
		if err := treeOSRemove(filepath.Join(orderPath, orderFilename)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove parent order file before fold-back: %w", err)
		}
		if err := treeStoreConvertNode(t.store, parent, NodeKindPage); err != nil {
			return err
		}
		parent.Kind = NodeKindPage
	}

	return nil
}

// FindPageByID finds a page in the tree by its ID.
func (t *TreeService) FindPageByID(id PageID) (*PageNode, error) {
	var result *PageNode
	err := t.withRLockedTree(func() error {
		if t.tree == nil {
			return ErrTreeNotLoaded
		}

		result = t.getNodeByIDLocked(id)
		if result == nil {
			return ErrPageNotFound
		}

		return nil
	})

	return result, err
}

func (t *TreeService) getNodeByIDLocked(id PageID) *PageNode {
	if id == "" {
		return nil
	}
	if id == "root" {
		return t.tree
	}

	return t.nodesByID[id]
}

func (t *TreeService) rebuildIndexesLocked() {
	t.nodesByID = make(map[PageID]*PageNode)
	t.childSlugs = make(map[PageID]map[SlugKey]*PageNode)

	if t.tree == nil {
		return
	}

	var walk func(node *PageNode)
	walk = func(node *PageNode) {
		if node == nil {
			return
		}

		if node.ID != "" && node.ID != "root" {
			t.nodesByID[node.ID] = node
		}
		t.rebuildChildSlugIndexForParentLocked(node)

		for _, child := range node.Children {
			walk(child)
		}
	}

	walk(t.tree)
}

func (t *TreeService) rebuildChildSlugIndexForParentLocked(parent *PageNode) {
	if parent == nil {
		return
	}

	index := make(map[SlugKey]*PageNode, len(parent.Children))
	for _, child := range parent.Children {
		if child == nil {
			continue
		}
		index[child.Slug.SlugKey()] = child
	}
	t.childSlugs[parent.ID] = index
}

func (t *TreeService) indexNodeLocked(node *PageNode) {
	if node == nil {
		return
	}

	if node.ID != "" && node.ID != "root" {
		t.nodesByID[node.ID] = node
	}
	t.rebuildChildSlugIndexForParentLocked(node)
	if node.Parent != nil {
		t.rebuildChildSlugIndexForParentLocked(node.Parent)
	}
}

func (t *TreeService) removeNodeIndexLocked(node *PageNode) {
	if node == nil {
		return
	}

	var walk func(current *PageNode)
	walk = func(current *PageNode) {
		if current == nil {
			return
		}

		delete(t.nodesByID, current.ID)
		delete(t.childSlugs, current.ID)

		for _, child := range current.Children {
			walk(child)
		}
	}

	walk(node)
	if node.Parent != nil {
		t.rebuildChildSlugIndexForParentLocked(node.Parent)
	}
}

func (t *TreeService) findChildBySlugInParentLocked(parent *PageNode, slug Slug) *PageNode {
	if parent == nil {
		return nil
	}

	if index, ok := t.childSlugs[parent.ID]; ok {
		return index[slug.SlugKey()]
	}

	for _, child := range parent.Children {
		if child != nil && child.Slug.EqualFold(slug) {
			return child
		}
	}

	return nil
}
