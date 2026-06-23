package tree

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/perber/wiki/internal/core/markdown"
	"github.com/perber/wiki/internal/core/shared"
	"github.com/perber/wiki/internal/core/treemigration"
)

// TreeService is our main component for handling tree operations
// We use this service to create pages, delete pages, update pages, etc.
type TreeService struct {
	dataDir    string
	rootDir    string
	tree       *PageNode
	store      *NodeStore
	log        *slog.Logger
	nodesByID  map[PageID]*PageNode
	childSlugs map[PageID]map[SlugKey]*PageNode

	mu sync.RWMutex
}

// NewTreeService creates a new TreeService
const legacyTreeFilename = "tree.json"

type TreeOptions struct {
	DataDir string
	RootDir string
}

func NewTreeService(dataDir string) *TreeService {
	return NewTreeServiceWithOptions(TreeOptions{DataDir: dataDir})
}

func NewTreeServiceWithOptions(options TreeOptions) *TreeService {
	normalized := normalizeTreeOptions(options)

	return &TreeService{
		dataDir:    normalized.DataDir,
		rootDir:    normalized.RootDir,
		tree:       nil,
		store:      NewNodeStoreWithOptions(NodeStoreOptions{DataDir: normalized.DataDir, RootDir: normalized.RootDir}),
		log:        slog.Default().With("component", "TreeService"),
		nodesByID:  make(map[PageID]*PageNode),
		childSlugs: make(map[PageID]map[SlugKey]*PageNode),
	}
}

func normalizeTreeOptions(options TreeOptions) TreeOptions {
	dataDir := cleanTreePath(options.DataDir)
	rootDir := cleanTreePath(options.RootDir)
	if rootDir == "" {
		rootDir = filepath.Join(dataDir, "root")
	}
	return TreeOptions{DataDir: dataDir, RootDir: rootDir}
}

func cleanTreePath(path string) string {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return ""
	}
	return filepath.Clean(trimmed)
}

// LoadTree reconstructs the in-memory tree from the filesystem.
// Legacy tree.json data is only used as a migration source for older schema versions.
func (t *TreeService) LoadTree() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.log.Info("Checking schema version...")
	schema, err := loadSchema(t.dataDir)
	if err != nil {
		t.log.Error("Error loading schema", "error", err)
		return err
	}

	if schema.Version == CurrentSchemaVersion {
		if err := t.ensureCurrentRootDirReady(); err != nil {
			return err
		}
		reconstructed, err := t.store.ReconstructTreeFromFS()
		if err != nil {
			return err
		}
		if reconstructed == nil {
			return fmt.Errorf("internal error: tree reconstruction returned nil tree")
		}
		t.tree = reconstructed
		t.rebuildIndexesLocked()
		return nil
	}

	legacyTreePath := filepath.Join(t.dataDir, legacyTreeFilename)
	if info, statErr := os.Stat(legacyTreePath); statErr == nil && !info.IsDir() {
		legacyTree, legacyErr := loadLegacyTreeSnapshot(t.dataDir, legacyTreeFilename, t.store.log)
		if legacyErr != nil {
			t.log.Warn("Could not load legacy tree, falling back to filesystem reconstruction", "path", legacyTreePath, "error", legacyErr)
			if err := t.ensureCurrentRootDirReady(); err != nil {
				return err
			}
			t.tree, err = t.store.ReconstructTreeFromFS()
			if err != nil {
				return err
			}
		} else {
			if err := t.ensureLegacyRootDirReady(legacyTree); err != nil {
				return err
			}
			t.tree = legacyTree
		}
	} else {
		if err := t.ensureCurrentRootDirReady(); err != nil {
			return err
		}
		t.tree, err = t.store.ReconstructTreeFromFS()
		if err != nil {
			return err
		}
	}

	if t.tree == nil {
		return fmt.Errorf("internal error: tree reconstruction returned nil tree")
	}

	t.log.Info("Migrating schema", "fromVersion", schema.Version, "toVersion", CurrentSchemaVersion)
	if err := treemigration.Run(schema.Version, t.migrationDependencies()); err != nil {
		t.log.Error("Error migrating schema", "error", err)
		return err
	}

	reconstructed, err := t.store.ReconstructTreeFromFS()
	if err != nil {
		return err
	}
	if reconstructed == nil {
		return fmt.Errorf("internal error: tree reconstruction returned nil tree")
	}

	t.tree = reconstructed
	t.rebuildIndexesLocked()

	if err := os.Remove(legacyTreePath); err != nil && !errors.Is(err, os.ErrNotExist) {
		t.log.Warn("Could not remove migrated legacy tree snapshot", "path", legacyTreePath, "error", err)
	}

	return nil
}

func (t *TreeService) ensureLegacyRootDirReady(legacyTree *PageNode) error {
	defaultRootDir := filepath.Join(t.dataDir, "root")
	if sameCleanPath(t.rootDir, defaultRootDir) {
		return nil
	}

	defaultHasContent, err := directoryHasEntries(defaultRootDir)
	if err != nil {
		return err
	}
	if !defaultHasContent {
		return nil
	}

	matchesDefaultRoot, err := directoryFileContentMatches(defaultRootDir, t.rootDir)
	if err != nil {
		return err
	}
	if !matchesDefaultRoot {
		return fmt.Errorf("legacy content remains in the default root dir %s while configured root dir %s is missing legacy markdown; move or copy content before changing root dir", defaultRootDir, t.rootDir)
	}

	missingLegacyContent, err := t.configuredRootMissingLegacyContent(legacyTree)
	if err != nil {
		return err
	}
	if !missingLegacyContent {
		return nil
	}

	return fmt.Errorf("legacy content remains in the default root dir %s while configured root dir %s is missing legacy markdown; move or copy content before changing root dir", defaultRootDir, t.rootDir)
}

func (t *TreeService) ensureCurrentRootDirReady() error {
	defaultRootDir := filepath.Join(t.dataDir, "root")
	if sameCleanPath(t.rootDir, defaultRootDir) {
		return nil
	}

	defaultHasContent, err := directoryHasEntries(defaultRootDir)
	if err != nil {
		return err
	}
	if !defaultHasContent {
		return nil
	}

	matches, err := directoryFileContentMatches(defaultRootDir, t.rootDir)
	if err != nil {
		return err
	}
	if matches {
		return nil
	}

	return fmt.Errorf("legacy content remains in the default root dir %s while configured root dir %s is missing legacy markdown; move or copy content before changing root dir", defaultRootDir, t.rootDir)
}

func directoryFileContentMatches(sourceDir string, targetDir string) (bool, error) {
	sourceFiles, err := collectRelativeFiles(sourceDir)
	if err != nil {
		return false, err
	}
	if len(sourceFiles) == 0 {
		return false, nil
	}

	for _, relPath := range sourceFiles {
		sourcePath := filepath.Join(sourceDir, relPath)
		targetPath := filepath.Join(targetDir, relPath)
		matches, err := filesHaveSameContent(sourcePath, targetPath)
		if err != nil {
			return false, err
		}
		if !matches {
			return false, nil
		}
	}

	return true, nil
}

func collectRelativeFiles(dir string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		relPath, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		files = append(files, relPath)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("collect legacy content files from %s: %w", dir, err)
	}
	sort.Strings(files)
	return files, nil
}

func filesHaveSameContent(sourcePath string, targetPath string) (bool, error) {
	sourceData, err := os.ReadFile(sourcePath)
	if err != nil {
		return false, fmt.Errorf("read legacy content path %s: %w", sourcePath, err)
	}
	targetData, err := os.ReadFile(targetPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("read configured legacy content path %s: %w", targetPath, err)
	}
	return string(sourceData) == string(targetData), nil
}

func (t *TreeService) configuredRootMissingLegacyContent(legacyTree *PageNode) (bool, error) {
	paths, err := t.expectedLegacyContentPaths(legacyTree)
	if err != nil {
		return false, err
	}
	if len(paths) == 0 {
		hasContent, err := directoryHasEntries(t.rootDir)
		if err != nil {
			return false, err
		}
		return !hasContent, nil
	}

	checkedLegacyContent := false
	for _, path := range paths {
		sourceInfo, err := os.Stat(path.sourceFile)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return false, fmt.Errorf("stat legacy content path %s: %w", path.sourceFile, err)
		}
		if sourceInfo.IsDir() {
			continue
		}
		checkedLegacyContent = true

		targetInfo, err := os.Stat(path.targetFile)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return true, nil
			}
			return false, fmt.Errorf("stat configured legacy content path %s: %w", path.targetFile, err)
		}
		if targetInfo.IsDir() {
			return true, nil
		}
		matches, err := legacyTargetMatchesNode(path)
		if err != nil {
			return false, err
		}
		if !matches {
			return true, nil
		}
	}

	if !checkedLegacyContent {
		hasContent, err := directoryHasEntries(t.rootDir)
		if err != nil {
			return false, err
		}
		return !hasContent, nil
	}

	return false, nil
}

type legacyContentPath struct {
	sourceFile string
	targetFile string
	nodeID     PageID
	nodeTitle  string
}

func legacyTargetMatchesNode(path legacyContentPath) (bool, error) {
	matches, err := filesHaveSameContent(path.sourceFile, path.targetFile)
	if err != nil {
		return false, err
	}
	if !matches {
		return false, nil
	}

	mdFile, err := markdown.LoadMarkdownFile(path.targetFile)
	if err != nil {
		return false, fmt.Errorf("load configured legacy content path %s: %w", path.targetFile, err)
	}
	metadata := mdFile.GetMetadata()
	return NewPageIDUnchecked(strings.TrimSpace(metadata.Page.ID)) == path.nodeID &&
		strings.TrimSpace(metadata.Page.Title) == strings.TrimSpace(path.nodeTitle), nil
}

func (t *TreeService) expectedLegacyContentPaths(legacyTree *PageNode) ([]legacyContentPath, error) {
	var paths []legacyContentPath
	if err := t.collectLegacyContentPaths(legacyTree, nil, &paths); err != nil {
		return nil, err
	}
	return paths, nil
}

func (t *TreeService) collectLegacyContentPaths(node *PageNode, parentSegments []string, paths *[]legacyContentPath) error {
	if node == nil {
		return nil
	}

	segments := parentSegments
	isRoot := node.ID == "root" && len(parentSegments) == 0
	if !isRoot {
		slug := strings.TrimSpace(node.Slug.FilesystemPath())
		if slug == "" {
			return fmt.Errorf("legacy tree contains node %q with empty slug", node.ID)
		}
		segments = append(append([]string{}, parentSegments...), slug)
		relPath := filepath.Join(segments...)
		defaultRootDir := filepath.Join(t.dataDir, "root")
		switch node.Kind {
		case NodeKindPage:
			*paths = append(*paths, legacyContentPath{
				sourceFile: filepath.Join(defaultRootDir, relPath+".md"),
				targetFile: filepath.Join(t.rootDir, relPath+".md"),
				nodeID:     node.ID,
				nodeTitle:  node.Title,
			})
		case NodeKindSection:
			*paths = append(*paths, legacyContentPath{
				sourceFile: filepath.Join(defaultRootDir, relPath, "index.md"),
				targetFile: filepath.Join(t.rootDir, relPath, "index.md"),
				nodeID:     node.ID,
				nodeTitle:  node.Title,
			})
		case "":
			*paths = append(*paths, legacyContentPath{
				sourceFile: filepath.Join(defaultRootDir, relPath+".md"),
				targetFile: filepath.Join(t.rootDir, relPath+".md"),
				nodeID:     node.ID,
				nodeTitle:  node.Title,
			})
		default:
			return fmt.Errorf("legacy tree contains node %q with unknown kind %q", node.ID, node.Kind)
		}
	}

	for _, child := range node.Children {
		if err := t.collectLegacyContentPaths(child, segments, paths); err != nil {
			return err
		}
	}
	return nil
}

func directoryHasEntries(dir string) (bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("read directory %s: %w", dir, err)
	}
	return len(entries) > 0, nil
}

func sameCleanPath(a string, b string) bool {
	absA, errA := filepath.Abs(filepath.Clean(a))
	absB, errB := filepath.Abs(filepath.Clean(b))
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
	newTree, err := t.store.ReconstructTreeFromFS()
	if err != nil {
		t.log.Error("Error reconstructing tree from filesystem", "error", err)
		return err
	}

	// Defensive check to protect against unexpected nil returns from ReconstructTreeFromFS
	if newTree == nil {
		return fmt.Errorf("internal error: ReconstructTreeFromFS returned nil tree")
	}

	// Save the old tree in case we need to revert
	// Note: oldTree may be nil if this is the first reconstruction (which is expected)
	oldTree := t.tree
	t.tree = newTree
	t.rebuildIndexesLocked()

	// Reconstructed nodes already carry metadata from canonical files, legacy
	// migration input, or safe defaults.

	if err := saveSchema(t.dataDir, CurrentSchemaVersion); err != nil {
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

		if err := t.store.UpsertContent(created.entry, content); err != nil {
			return fmt.Errorf("could not restore content: %w", err)
		}

		created.entry.Metadata = metadata
		created.entry.Metadata.UpdatedAt = metadata.UpdatedAt.UTC()
		created.entry.Metadata.CreatedAt = metadata.CreatedAt.UTC()
		created.entry.Metadata.CreatorID = metadata.CreatorID
		created.entry.Metadata.LastAuthorID = metadata.LastAuthorID
		if err := t.store.SyncMetadataIfExists(created.entry); err != nil {
			return fmt.Errorf("could not sync restored metadata: %w", err)
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
		if err := t.store.ConvertNode(parent, NodeKindSection); err != nil {
			return nil, fmt.Errorf("could not convert parent node: %w", err)
		}
		parent.Kind = NodeKindSection
		parentWasConverted = true
	}

	if parent.Kind != NodeKindSection {
		return nil, fmt.Errorf("cannot add child to non-section parent, got %q", parent.Kind)
	}

	id := opts.existingID
	if id == "" {
		var err error
		rawID, err := shared.GenerateUniqueID()
		if err != nil {
			return nil, fmt.Errorf("could not generate unique ID: %w", err)
		}
		id = NewPageIDUnchecked(rawID)
	} else if existing := t.getNodeByIDLocked(id); existing != nil {
		return nil, fmt.Errorf("page id already exists: %s", id)
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
		if err := t.store.CreatePage(parent, entry); err != nil {
			return nil, fmt.Errorf("could not create page entry: %w", err)
		}
	case NodeKindSection:
		if err := t.store.CreateSection(parent, entry); err != nil {
			return nil, fmt.Errorf("could not create section entry: %w", err)
		}
	}

	// Add the new page to the parent
	parent.Children = append(parent.Children, entry)
	t.indexNodeLocked(entry)
	if err := t.store.SaveChildOrder(parent); err != nil {
		rollbackErr := t.rollbackCreatedNodeLocked(parent, entry, parentWasConverted)
		if rollbackErr != nil {
			return nil, errors.Join(fmt.Errorf("could not persist child order: %w", err), fmt.Errorf("rollback created node: %w", rollbackErr))
		}
		return nil, fmt.Errorf("could not persist child order: %w", err)
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
		if err := t.store.DeleteSection(entry); err != nil {
			return err
		}
	case NodeKindPage:
		if err := t.store.DeletePage(entry); err != nil {
			return err
		}
	}

	if parentWasConverted && len(parent.Children) == 0 {
		orderPath, err := t.store.sectionDirPathForNode(parent, "rollbackCreatedNode")
		if err != nil {
			return err
		}
		if err := os.Remove(filepath.Join(orderPath, orderFilename)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove parent order file before fold-back: %w", err)
		}
		if err := t.store.ConvertNode(parent, NodeKindPage); err != nil {
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
			if err := t.store.DeleteSection(node); err != nil {
				return fmt.Errorf("could not delete section entry: %w", err)
			}
		case NodeKindPage:
			if node.HasChildren() {
				// This should not happen due to earlier check, but just in case
				// Convert to section and delete recursively
				t.log.Info("converting page to section for recursive delete", "pageID", node.ID)
				if err := t.store.ConvertNode(node, NodeKindSection); err != nil {
					return fmt.Errorf("could not convert page to section: %w", err)
				}
				node.Kind = NodeKindSection
				if err := t.store.DeleteSection(node); err != nil {
					return fmt.Errorf("could not delete section entry: %w", err)
				}
			} else {
				if err := t.store.DeletePage(node); err != nil {
					return fmt.Errorf("could not delete page entry: %w", err)
				}
			}
		default:
			return fmt.Errorf("unknown node kind: %v", node.Kind)
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
		if err := t.store.SaveChildOrder(parent); err != nil {
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
				upsertErr = t.store.UpsertContentPreservingFrontmatter(node, *content)
			case contentUpdateReplaceMetadata:
				upsertErr = t.store.UpsertContentReplacingMetadata(node, *content)
			default:
				upsertErr = t.store.UpsertContent(node, *content)
			}
			if upsertErr != nil {
				return fmt.Errorf("could not upsert content: %w", upsertErr)
			}
		}

		// Rename slug on disk (must happen while node still has old slug)
		if slug != node.Slug {
			t.log.Info("renaming node slug", "nodeID", node.ID, "oldSlug", node.Slug, "newSlug", slug)
			if err := t.store.RenameNode(node, slug); err != nil {
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
		if err := t.store.SyncMetadataIfExists(node); err != nil {
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

		if err := t.store.ConvertNode(node, kind); err != nil {
			return fmt.Errorf("could not convert node: %w", err)
		}
		node.Kind = kind

		// Update metadata
		node.Metadata.UpdatedAt = time.Now().UTC()
		node.Metadata.LastAuthorID = userID

		// Keep metadata in sync if the file exists (important when kind
		// changed but content == nil).
		if err := t.store.SyncMetadataIfExists(node); err != nil {
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
	rel, err := filepath.Rel(t.rootDir, contentPath)
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

// BulkUpdateContent updates content for multiple pages under a single write lock,
// running disk writes in parallel. Returns per-item errors; nil means success.
// Only content and metadata timestamps are updated; slug and title are unchanged.
func (t *TreeService) BulkUpdateContent(userID UserID, updates []BulkContentUpdate) []error {
	errs := make([]error, len(updates))
	if len(updates) == 0 {
		return errs
	}

	type task struct {
		index       int
		node        *PageNode
		content     string
		oldMetadata PageMetadata
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if t.tree == nil {
		for i := range errs {
			errs[i] = ErrTreeNotLoaded
		}
		return errs
	}

	now := time.Now().UTC()
	tasks := make([]task, 0, len(updates))
	for i, u := range updates {
		node := t.getNodeByIDLocked(u.ID)
		if node == nil {
			errs[i] = ErrPageNotFound
			continue
		}
		oldMetadata := node.Metadata
		// Update in-memory metadata before disk write so UpsertContent writes the correct timestamps.
		node.Metadata.UpdatedAt = now
		node.Metadata.LastAuthorID = userID
		tasks = append(tasks, task{index: i, node: node, content: u.Content, oldMetadata: oldMetadata})
	}

	if len(tasks) == 0 {
		return errs
	}

	// Each page lives in its own file — writes are independent and safe to parallelise.
	var mu sync.Mutex
	var wg sync.WaitGroup
	wg.Add(len(tasks))
	for _, tk := range tasks {
		go func(tk task) {
			defer wg.Done()
			if err := t.store.UpsertContent(tk.node, tk.content); err != nil {
				mu.Lock()
				errs[tk.index] = err
				mu.Unlock()
			}
		}(tk)
	}
	wg.Wait()

	for _, tk := range tasks {
		if errs[tk.index] != nil {
			tk.node.Metadata = tk.oldMetadata
		}
	}

	return errs
}

// GetPages returns pages for the given IDs under a single read lock,
// reading files in parallel. Each entry is nil when the corresponding error is non-nil.
func (t *TreeService) GetPages(ids []PageID) ([]*Page, []error) {
	pages := make([]*Page, len(ids))
	errs := make([]error, len(ids))
	if len(ids) == 0 {
		return pages, errs
	}

	type task struct {
		index int
		node  *PageNode
	}

	t.mu.RLock()
	defer t.mu.RUnlock()

	if t.tree == nil {
		for i := range errs {
			errs[i] = ErrTreeNotLoaded
		}
		return pages, errs
	}

	tasks := make([]task, 0, len(ids))
	for i, id := range ids {
		node := t.getNodeByIDLocked(id)
		if node == nil {
			errs[i] = ErrPageNotFound
			continue
		}
		tasks = append(tasks, task{index: i, node: node})
	}

	if len(tasks) == 0 {
		return pages, errs
	}

	var mu sync.Mutex
	var wg sync.WaitGroup
	wg.Add(len(tasks))
	for _, tk := range tasks {
		go func(tk task) {
			defer wg.Done()
			content, raw, err := t.store.ReadPageAndRaw(tk.node)
			mu.Lock()
			if err != nil {
				errs[tk.index] = fmt.Errorf("could not get page content: %w", err)
			} else {
				pages[tk.index] = &Page{PageNode: tk.node, Content: content, RawContent: raw}
			}
			mu.Unlock()
		}(tk)
	}
	wg.Wait()

	return pages, errs
}

// GetPage returns a page by its ID
func (t *TreeService) GetPage(id PageID) (*Page, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if t.tree == nil {
		return nil, ErrTreeNotLoaded
	}

	// Find the page
	page := t.getNodeByIDLocked(id)
	if page == nil {
		return nil, ErrPageNotFound
	}

	content, raw, err := t.store.ReadPageAndRaw(page)
	if err != nil {
		return nil, fmt.Errorf("could not get page content: %w", err)
	}

	return &Page{
		PageNode:   page,
		Content:    content,
		RawContent: raw,
	}, nil
}

// ReadPageRaw returns the raw markdown of a page, including metadata.
func (t *TreeService) ReadPageRaw(id PageID) (string, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if t.tree == nil {
		return "", ErrTreeNotLoaded
	}

	page := t.getNodeByIDLocked(id)
	if page == nil {
		return "", ErrPageNotFound
	}

	raw, err := t.store.ReadPageRaw(page)
	if err != nil {
		return "", fmt.Errorf("could not get page raw content: %w", err)
	}

	return raw, nil
}

// ResolvePermalinkTarget resolves a stable page ID to the current route path.
func (t *TreeService) ResolvePermalinkTarget(id PageID) (*PermalinkTarget, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if t.tree == nil {
		return nil, ErrTreeNotLoaded
	}

	node := t.getNodeByIDLocked(id)
	if node == nil {
		return nil, ErrPageNotFound
	}

	return &PermalinkTarget{
		ID:   node.ID,
		Slug: node.Slug,
		Path: strings.TrimPrefix(node.CalculatePath(), "/"),
		Kind: node.Kind,
	}, nil
}

// FindPageByRoutePath finds a page in the tree by its path.
func (t *TreeService) FindPageByRoutePath(routePath RoutePath) (*Page, error) {
	return t.findPageByRoutePath(routePath, "")
}

// FindPageByRoutePathAndKind finds a page in the tree by path and final node kind.
func (t *TreeService) FindPageByRoutePathAndKind(routePath RoutePath, kind NodeKind) (*Page, error) {
	return t.findPageByRoutePath(routePath, kind)
}

func (t *TreeService) findPageByRoutePath(routePath RoutePath, finalKind NodeKind) (*Page, error) {
	parsedRoutePath, err := routePath.Validate()
	if err != nil {
		return nil, err
	}

	t.mu.RLock()
	defer t.mu.RUnlock()

	if t.tree == nil {
		return nil, ErrTreeNotLoaded
	}

	// Split the routePath into parts
	routePart := parsedRoutePath.Segments()
	if len(routePart) == 0 {
		return nil, ErrPageNotFound
	}

	parent := t.tree
	var node *PageNode
	for index, part := range routePart {
		if part == "" {
			return nil, ErrPageNotFound
		}

		if index == len(routePart)-1 && finalKind != "" {
			node = t.findChildBySlugAndKindExactInParentLocked(parent, part, finalKind)
		} else if index < len(routePart)-1 {
			node = t.findChildBySlugAndKindExactInParentLocked(parent, part, NodeKindSection)
			if node == nil {
				node = t.findChildBySlugExactInParentLocked(parent, part)
			}
		} else {
			node = t.findRouteSegmentExactInParentLocked(parent, part)
		}
		if node == nil {
			return nil, ErrPageNotFound
		}

		parent = node
	}

	content, err := t.store.ReadPageContent(node)
	if err != nil {
		return nil, fmt.Errorf("could not get page content: %w", err)
	}

	return &Page{
		PageNode: node,
		Content:  content,
	}, nil
}

// LookupPagePath looks up a path in the tree and returns a PathLookup struct
// that contains information about the path and its segments and whether they exist.
func (t *TreeService) LookupPagePath(p RoutePath) (*PathLookup, error) {
	routePath, err := p.Validate()
	if err != nil {
		return nil, err
	}

	t.mu.RLock()
	defer t.mu.RUnlock()

	return t.lookupPagePathLocked(routePath, "")
}

// LookupPagePathForKind looks up a path while requiring the final segment to
// match the requested kind when it exists.
func (t *TreeService) LookupPagePathForKind(p RoutePath, finalKind NodeKind) (*PathLookup, error) {
	routePath, err := p.Validate()
	if err != nil {
		return nil, err
	}

	t.mu.RLock()
	defer t.mu.RUnlock()

	return t.lookupPagePathLocked(routePath, finalKind)
}

// lookupPagePathLocked looks up a path in the tree and returns a PathLookup struct
// that contains information about the path and its segments and whether they exist.
// Lock must be held by the caller.
func (t *TreeService) lookupPagePathLocked(p RoutePath, finalKind NodeKind) (*PathLookup, error) {
	if t.tree == nil {
		return nil, ErrTreeNotLoaded
	}

	slugService := NewSlugService()
	path := p.Clean()
	if path == "" {
		return &PathLookup{
			Path:      path,
			Segments:  []PathSegment{},
			Exists:    false,
			CanCreate: false,
		}, nil
	}

	routePath, err := path.Validate()
	if err != nil {
		return nil, err
	}
	// Split the path into parts
	pathParts := routePath.Segments()
	if len(pathParts) == 0 {
		return &PathLookup{
			Path:      routePath,
			Segments:  []PathSegment{},
			Exists:    false,
			CanCreate: false,
		}, nil
	}

	lookup := &PathLookup{
		Path:      routePath,
		Segments:  make([]PathSegment, len(pathParts)),
		Exists:    true,
		CanCreate: true,
	}

	parent := t.tree

	// Check each segment in the path
	for i, part := range pathParts {
		if part == "" || part == "." || part == ".." {
			return nil, fmt.Errorf("invalid path segment: %q", part)
		}

		// Find the segment in the tree
		segment := PathSegment{
			Slug:   part,
			Exists: false,
		}

		// push the segment to the lookup
		lookup.Segments[i] = segment

		// Check if the segment exists under the current parent.
		var e *PageNode
		if i == len(pathParts)-1 && finalKind != "" {
			e = t.findChildBySlugAndKindInParentLocked(parent, part, finalKind)
		} else {
			e = t.findRouteSegmentInParentLocked(parent, part)
		}
		if e != nil {
			// Segment exists
			lookup.Segments[i].Exists = true
			lookup.Segments[i].ID = &e.ID
			lookup.Segments[i].Kind = &e.Kind
			lookup.Segments[i].Title = &e.Title

			// Move to the next parent
			parent = e
		}

		// If the segment does not exist, set the pathExists flag to false
		if !lookup.Segments[i].Exists {
			if lookup.CanCreate && slugService.IsValidSlug(part.FilesystemPath()) != nil {
				lookup.CanCreate = false
			}

			// No need to check further segments
			// Set all remaining segments to non-existing
			for j := i + 1; j < len(pathParts); j++ {
				if lookup.CanCreate && slugService.IsValidSlug(pathParts[j].FilesystemPath()) != nil {
					lookup.CanCreate = false
				}
				lookup.Segments[j] = PathSegment{
					Slug:   pathParts[j],
					Exists: false,
				}
			}

			lookup.Exists = false

			parent = nil
		}
	}

	return lookup, nil
}

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
		return nil, fmt.Errorf("could not lookup page path: %w", err)
	}

	// Path exists -> return existing
	if lookup.Exists && len(lookup.Segments) > 0 {
		last := lookup.Segments[len(lookup.Segments)-1]
		if last.Kind != nil && *last.Kind == requestedKind {
			page := t.getNodeByIDLocked(*last.ID)
			if page == nil {
				return nil, fmt.Errorf("could not find existing page by ID: %w", ErrPageNotFound)
			}
			return &EnsurePathResult{Exists: true, Page: page}, nil
		}
		if page := t.findNodeByRoutePathAndKindLocked(lookup.Path, requestedKind); page != nil {
			return &EnsurePathResult{Exists: true, Page: page}, nil
		}
	}

	// Create missing segments
	var currentID *PageID // nil means root
	for i, segment := range lookup.Segments {
		isFinalSegment := i == len(lookup.Segments)-1
		if segment.Exists && !(isFinalSegment && segment.Kind != nil && *segment.Kind != requestedKind) {
			if segment.ID == nil {
				currentID = nil
				continue
			}
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
			return nil, fmt.Errorf("could not create segment %q: %w", segment.Slug, err)
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
		return nil, fmt.Errorf("could not ensure page path")
	}
	page := t.getNodeByIDLocked(*currentID)
	if page == nil {
		return nil, fmt.Errorf("could not find created page by ID: %w", ErrPageNotFound)
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
		if part == "" {
			return nil
		}
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
		if err := t.store.ConvertNode(newParent, NodeKindSection); err != nil {
			return fmt.Errorf("could not auto-convert new parent page to section: %w", err)
		}
		newParent.Kind = NodeKindSection
		newParentWasConverted = true
	}

	if newParent.Kind != NodeKindSection {
		return fmt.Errorf("destination parent must be a section, got %q", newParent.Kind)
	}

	previousOldChildren := append([]*PageNode(nil), oldParent.Children...)
	previousOldPositions := snapshotChildPositions(oldParent.Children)
	previousNewChildren := append([]*PageNode(nil), newParent.Children...)
	previousNewPositions := snapshotChildPositions(newParent.Children)
	previousPosition := node.Position
	previousMetadata := node.Metadata

	if err := t.store.MoveNode(node, newParent); err != nil {
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

	if err := t.store.SaveChildOrder(oldParent); err != nil {
		rollbackErr := t.rollbackMovedNodeLocked(node, oldParent, newParent, previousOldChildren, previousOldPositions, previousNewChildren, previousNewPositions, previousPosition, previousMetadata, newParentWasConverted)
		if rollbackErr != nil {
			return errors.Join(fmt.Errorf("could not persist source child order: %w", err), fmt.Errorf("rollback moved node: %w", rollbackErr))
		}
		return fmt.Errorf("could not persist source child order: %w", err)
	}
	if newParent != oldParent {
		if err := t.store.SaveChildOrder(newParent); err != nil {
			rollbackErr := t.rollbackMovedNodeLocked(node, oldParent, newParent, previousOldChildren, previousOldPositions, previousNewChildren, previousNewPositions, previousPosition, previousMetadata, newParentWasConverted)
			if rollbackErr != nil {
				return errors.Join(fmt.Errorf("could not persist destination child order: %w", err), fmt.Errorf("rollback moved node: %w", rollbackErr))
			}
			return fmt.Errorf("could not persist destination child order: %w", err)
		}
	}

	if err := t.store.SyncMetadataIfExists(node); err != nil {
		rollbackErr := t.rollbackMovedNodeLocked(node, oldParent, newParent, previousOldChildren, previousOldPositions, previousNewChildren, previousNewPositions, previousPosition, previousMetadata, newParentWasConverted)
		if rollbackErr != nil {
			return errors.Join(fmt.Errorf("could not sync moved node metadata: %w", err), fmt.Errorf("rollback moved node: %w", rollbackErr))
		}
		return fmt.Errorf("could not sync moved node metadata: %w", err)
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

	if moveErr := t.store.MoveNode(node, oldParent); moveErr != nil {
		rollbackErr = errors.Join(rollbackErr, fmt.Errorf("move node back on disk: %w", moveErr))
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
			rollbackErr = errors.Join(rollbackErr, fmt.Errorf("resolve converted parent dir: %w", dirErr))
		} else {
			if removeErr := os.RemoveAll(filepath.Join(newParentDir, orderFilename)); removeErr != nil {
				rollbackErr = errors.Join(rollbackErr, fmt.Errorf("remove child order before parent rollback: %w", removeErr))
			}
		}
		if convertErr := t.store.ConvertNode(newParent, NodeKindPage); convertErr != nil {
			rollbackErr = errors.Join(rollbackErr, fmt.Errorf("convert destination parent back to page: %w", convertErr))
		} else {
			newParent.Kind = NodeKindPage
		}
	}

	if err := t.store.SaveChildOrder(oldParent); err != nil {
		rollbackErr = errors.Join(rollbackErr, fmt.Errorf("restore source child order: %w", err))
	}
	if newParent != oldParent && newParent.Kind == NodeKindSection {
		if err := t.store.SaveChildOrder(newParent); err != nil {
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
			return fmt.Errorf("duplicate ID in sort order: %s", id)
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

	if err := t.store.SaveChildOrder(parent); err != nil {
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
