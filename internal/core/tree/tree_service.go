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
	schema, err := treeLoadSchema(t.dataDir)
	if err != nil {
		t.log.Error("Error loading schema", "error", err)
		return err
	}

	if schema.Version == CurrentSchemaVersion {
		if err := t.ensureCurrentRootDirReady(); err != nil {
			return err
		}
		reconstructed, err := treeStoreReconstructTreeFromFS(t.store)
		if err != nil {
			return err
		}
		if reconstructed == nil {
			return fmt.Errorf("%w: internal error", ErrTreeReconstructionNil)
		}
		t.tree = reconstructed
		t.rebuildIndexesLocked()
		return nil
	}

	legacyTreePath := filepath.Join(t.dataDir, legacyTreeFilename)
	if info, statErr := treeOSStat(legacyTreePath); statErr == nil && !info.IsDir() {
		legacyTree, legacyErr := treeLoadLegacyTreeSnapshot(t.dataDir, legacyTreeFilename, t.store.log)
		if legacyErr != nil {
			t.log.Warn("Could not load legacy tree, falling back to filesystem reconstruction", "path", legacyTreePath, "error", legacyErr)
			if err := t.ensureCurrentRootDirReady(); err != nil {
				return err
			}
			t.tree, err = treeStoreReconstructTreeFromFS(t.store)
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
		t.tree, err = treeStoreReconstructTreeFromFS(t.store)
		if err != nil {
			return err
		}
	}

	if t.tree == nil {
		return fmt.Errorf("%w: internal error", ErrTreeReconstructionNil)
	}

	t.log.Info("Migrating schema", "fromVersion", schema.Version, "toVersion", CurrentSchemaVersion)
	if err := treeRunMigration(schema.Version, t.migrationDependencies()); err != nil {
		t.log.Error("Error migrating schema", "error", err)
		return err
	}

	reconstructed, err := treeStoreReconstructTreeFromFS(t.store)
	if err != nil {
		return err
	}
	if reconstructed == nil {
		return fmt.Errorf("%w: internal error", ErrTreeReconstructionNil)
	}

	t.tree = reconstructed
	t.rebuildIndexesLocked()

	if err := treeOSRemove(legacyTreePath); err != nil && !errors.Is(err, os.ErrNotExist) {
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
		return fmt.Errorf("%w in the default root dir %s while configured root dir %s is missing legacy markdown; move or copy content before changing root dir", ErrLegacyContentRemains, defaultRootDir, t.rootDir)
	}

	missingLegacyContent, err := t.configuredRootMissingLegacyContent(legacyTree)
	if err != nil {
		return err
	}
	if !missingLegacyContent {
		return nil
	}

	return fmt.Errorf("%w in the default root dir %s while configured root dir %s is missing legacy markdown; move or copy content before changing root dir", ErrLegacyContentRemains, defaultRootDir, t.rootDir)
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

	return fmt.Errorf("%w in the default root dir %s while configured root dir %s is missing legacy markdown; move or copy content before changing root dir", ErrLegacyContentRemains, defaultRootDir, t.rootDir)
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
	err := treeFilepathWalkDir(dir, func(path string, entry os.DirEntry, walkErr error) error {
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
		relPath, err := treeFilepathRel(dir, path)
		if err != nil {
			return err
		}
		files = append(files, relPath)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("%w from %s: %w", ErrCollectLegacyContentFiles, dir, err)
	}
	sort.Strings(files)
	return files, nil
}

func filesHaveSameContent(sourceFile string, targetFile string) (bool, error) {
	sourceData, err := treeOSReadFile(sourceFile)
	if err != nil {
		return false, fmt.Errorf("%w %s: %w", ErrReadLegacyContentPath, sourceFile, err)
	}
	targetData, err := treeOSReadFile(targetFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("%w %s: %w", ErrReadConfiguredLegacyContentPath, targetFile, err)
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
		sourceInfo, err := treeOSStat(path.sourceFile)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return false, fmt.Errorf("%w %s: %w", ErrStatLegacyContentPath, path.sourceFile, err)
		}
		if sourceInfo.IsDir() {
			continue
		}
		checkedLegacyContent = true

		targetInfo, err := treeOSStat(path.targetFile)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return true, nil
			}
			return false, fmt.Errorf("%w %s: %w", ErrStatConfiguredLegacyContentPath, path.targetFile, err)
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

	mdFile, err := treeLoadMarkdownFile(path.targetFile)
	if err != nil {
		return false, fmt.Errorf("%w %s: %w", ErrLoadConfiguredLegacyContentPath, path.targetFile, err)
	}
	metadata := mdFile.GetMetadata()
	return PageIDFromString(strings.TrimSpace(metadata.Page.ID)) == path.nodeID &&
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
			return fmt.Errorf("legacy tree contains node %q with empty slug: %w", node.ID, ErrSlugEmpty)
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
			return fmt.Errorf("legacy tree contains node %q with %w %q", node.ID, ErrLegacyUnknownKind, node.Kind)
		}
	}

	for _, child := range node.Children {
		if err := t.collectLegacyContentPaths(child, segments, paths); err != nil {
			return err
		}
	}
	return nil
}
