package tree

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SyncMetadataIfExists updates managed metadata for a page file on disk if it exists.
func (f *NodeStore) SyncMetadataIfExists(entry *PageNode) error {
	if entry == nil {
		return &InvalidOpError{Op: "SyncMetadataIfExists", Reason: "an entry is required"}
	}

	// No side effects: avoid the write path, which may create directories and
	// choose a default section content path. The read path is enough because we
	// only sync when the file exists.
	filePath, err := f.contentPathForNodeRead(entry)
	if err != nil {
		return err
	}

	// Datei existiert?
	if !fileExists(filePath) {
		// Page: muss existieren
		if entry.Kind == NodeKindPage || entry.Kind == "" {
			return &DriftError{NodeID: entry.ID, Kind: entry.Kind, Path: filePath, Reason: "expected page file missing"}
		}
		// Section: no active content file -> do not create one here.
		return nil
	}

	mdFile, err := treeLoadMarkdownFile(filePath)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrLoadMarkdownFile, err)
	}

	f.syncManagedMetadata(mdFile, entry)
	if err := treeMarkdownWriteToFile(mdFile); err != nil {
		return fmt.Errorf("%w: %w", ErrWriteMarkdownFile, err)
	}
	return nil
}

// SyncFrontmatterIfExists is kept for compatibility with callers that still
// use the legacy API name.
func (f *NodeStore) SyncFrontmatterIfExists(entry *PageNode) error {
	if err := f.SyncMetadataIfExists(entry); err != nil {
		if invalid, ok := err.(*InvalidOpError); ok && invalid.Op == "SyncMetadataIfExists" {
			return &InvalidOpError{Op: "SyncFrontmatterIfExists", Reason: invalid.Reason}
		}
		return err
	}
	return nil
}

func (f *NodeStore) dirPathForNode(entry *PageNode) (string, error) {
	if entry == nil {
		return "", &InvalidOpError{Op: "dirPathForNode", Reason: "an entry is required"}
	}
	if err := f.validateNodeRoute(entry); err != nil {
		return "", err
	}
	routePath := GenerateRoutePathFromPageNode(entry)
	if routePath == "" {
		if err := f.requirePathInRoot("dirPathForNode", f.rootDir); err != nil {
			return "", err
		}
		return f.rootDir, nil
	}
	path := filepath.Join(f.rootDir, routePath.FilesystemPath())
	if err := f.requirePathInRoot("dirPathForNode", path); err != nil {
		return "", err
	}
	return path, nil
}

func (f *NodeStore) sectionDirPathForNode(entry *PageNode, op string) (string, error) {
	if entry == nil {
		return "", &InvalidOpError{Op: op, Reason: "an entry is required"}
	}
	if entry.ID != "root" && entry.Kind != NodeKindSection {
		return "", &InvalidOpError{Op: op, Reason: "entry must be root or a section"}
	}
	sourcePath := entry.WorkspaceSourcePath.Clean().FilesystemPath()
	if sourcePath != "" {
		dirPath := filepath.Join(f.rootDir, filepath.FromSlash(sourcePath))
		if err := f.requirePathInRoot(op, dirPath); err != nil {
			return "", err
		}
		return dirPath, nil
	}
	return f.dirPathForNode(entry)
}

func (f *NodeStore) pageFilePathForNode(entry *PageNode, op string) (string, error) {
	if entry == nil {
		return "", &InvalidOpError{Op: op, Reason: "an entry is required"}
	}
	if entry.Kind != NodeKindPage && entry.Kind != "" {
		return "", &InvalidOpError{Op: op, Reason: "entry must be a page"}
	}
	sourcePath := entry.WorkspaceSourcePath.Clean().FilesystemPath()
	if sourcePath != "" {
		filePath := filepath.Join(f.rootDir, filepath.FromSlash(sourcePath))
		if err := f.requirePathInRoot(op, filePath); err != nil {
			return "", err
		}
		return filePath, nil
	}
	base, err := f.dirPathForNode(entry)
	if err != nil {
		return "", err
	}
	return base + ".md", nil
}

func (f *NodeStore) workspaceSourceDirForSection(entry *PageNode) string {
	if entry == nil || entry.ID == "root" {
		return ""
	}
	if sourcePath := entry.WorkspaceSourcePath.Clean(); sourcePath != "" {
		return sourcePath.FilesystemPath()
	}
	return defaultWorkspaceSourcePath(GenerateRoutePathFromPageNode(entry), NodeKindSection).FilesystemPath()
}

func (f *NodeStore) workspaceSourcePathForNode(entry *PageNode) string {
	if entry == nil || entry.ID == "root" {
		return ""
	}
	if sourcePath := entry.WorkspaceSourcePath.Clean(); sourcePath != "" {
		return sourcePath.FilesystemPath()
	}
	return defaultWorkspaceSourcePath(GenerateRoutePathFromPageNode(entry), entry.Kind).FilesystemPath()
}

func routePathForChild(parent *PageNode, slug Slug) RoutePath {
	var parentRoutePath RoutePath
	if parent != nil && parent.ID != "root" {
		parentRoutePath = GenerateRoutePathFromPageNode(parent)
	}
	return parentRoutePath.Child(slug)
}

func routePathWithLeafSlug(entry *PageNode, slug Slug) RoutePath {
	if entry == nil {
		return ""
	}
	var parentRoutePath RoutePath
	if entry.Parent != nil && entry.Parent.ID != "root" {
		parentRoutePath = GenerateRoutePathFromPageNode(entry.Parent)
	}
	return parentRoutePath.Child(slug)
}

func childRoutePathUnder(parentRoutePath RoutePath, child *PageNode) RoutePath {
	if child == nil {
		return parentRoutePath.Clean()
	}
	return parentRoutePath.Child(child.Slug)
}

func (f *NodeStore) setWorkspaceSourcePathForPhysicalPath(entry *PageNode, physicalPath string, routePath RoutePath, kind NodeKind) {
	if entry == nil {
		return
	}
	relPath, err := treeFilepathRel(f.rootDir, physicalPath)
	if err != nil {
		return
	}
	f.setWorkspaceSourcePath(entry, routePath, kind, filepath.ToSlash(relPath))
}

func (f *NodeStore) setWorkspaceSourcePath(entry *PageNode, routePath RoutePath, kind NodeKind, sourcePath string) {
	if entry == nil {
		return
	}
	workspaceSourcePath := CleanWorkspaceSourcePath(sourcePath)
	if workspaceSourcePath == "" || workspaceSourcePath == defaultWorkspaceSourcePath(routePath, kind) {
		entry.WorkspaceSourcePath = ""
		return
	}
	entry.WorkspaceSourcePath = workspaceSourcePath
}

func (f *NodeStore) updateWorkspaceSourcePathsForSubtree(entry *PageNode, oldRoutePath RoutePath, newRoutePath RoutePath, oldSourcePrefix string, newSourcePrefix string) {
	if entry == nil {
		return
	}
	f.updateWorkspaceSourcePathsForSubtreeRecursive(entry, oldRoutePath.Clean(), newRoutePath.Clean(), cleanWorkspaceSourcePath(oldSourcePrefix), cleanWorkspaceSourcePath(newSourcePrefix))
}

func (f *NodeStore) updateWorkspaceSourcePathsForSubtreeRecursive(entry *PageNode, oldRoutePath RoutePath, newRoutePath RoutePath, oldSourcePrefix string, newSourcePrefix string) {
	oldSourcePath := entry.WorkspaceSourcePath.Clean().FilesystemPath()
	if oldSourcePath == "" {
		oldSourcePath = defaultWorkspaceSourcePath(oldRoutePath, entry.Kind).FilesystemPath()
	}
	newSourcePath := replaceWorkspaceSourcePrefix(oldSourcePath, oldSourcePrefix, newSourcePrefix)
	f.setWorkspaceSourcePath(entry, newRoutePath, entry.Kind, newSourcePath)

	for _, child := range entry.Children {
		childOldRoutePath := childRoutePathUnder(oldRoutePath, child)
		childNewRoutePath := childRoutePathUnder(newRoutePath, child)
		f.updateWorkspaceSourcePathsForSubtreeRecursive(child, childOldRoutePath, childNewRoutePath, oldSourcePrefix, newSourcePrefix)
	}
}

func replaceWorkspaceSourcePrefix(sourcePath string, oldPrefix string, newPrefix string) string {
	sourcePath = cleanWorkspaceSourcePath(sourcePath)
	oldPrefix = cleanWorkspaceSourcePath(oldPrefix)
	newPrefix = cleanWorkspaceSourcePath(newPrefix)
	if oldPrefix == "" {
		return joinWorkspaceRoutePath(newPrefix, sourcePath)
	}
	if sourcePath == oldPrefix {
		return newPrefix
	}
	prefix := oldPrefix + "/"
	if strings.HasPrefix(sourcePath, prefix) {
		return joinWorkspaceRoutePath(newPrefix, strings.TrimPrefix(sourcePath, prefix))
	}
	return sourcePath
}

func (f *NodeStore) validateNodeRoute(entry *PageNode) error {
	for current := entry; current != nil; current = current.Parent {
		if current.ID == "root" {
			continue
		}
		if current.Parent == nil {
			return &InvalidOpError{Op: "dirPathForNode", Reason: fmt.Sprintf("non-root node %q has no parent", current.ID)}
		}
		if err := validateNodeSlug("dirPathForNode", current.Slug); err != nil {
			return err
		}
	}
	return nil
}

func (f *NodeStore) requirePathInRoot(op string, path string) error {
	rootAbs, err := resolvePathForContainment(f.rootDir)
	if err != nil {
		return fmt.Errorf("%s: %w: %w", op, ErrResolveRootDir, err)
	}
	pathAbs, err := resolvePathForContainment(path)
	if err != nil {
		return fmt.Errorf("%s: %w: %w", op, ErrResolvePath, err)
	}
	rel, err := treeFilepathRel(rootAbs, pathAbs)
	if err != nil {
		return fmt.Errorf("%s: compare path to root dir: %w", op, err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
		return &InvalidOpError{Op: op, Reason: fmt.Sprintf("path escapes root dir: %s", path)}
	}
	return nil
}

func resolvePathForContainment(path string) (string, error) {
	absPath, err := treeFilepathAbs(filepath.Clean(path))
	if err != nil {
		return "", err
	}
	resolved, err := treeFilepathEvalSymlinks(absPath)
	if err == nil {
		return filepath.Clean(resolved), nil
	}
	if !os.IsNotExist(err) {
		return "", err
	}

	current := absPath
	var missing []string
	for {
		resolved, err := treeFilepathEvalSymlinks(current)
		if err == nil {
			for i := len(missing) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, missing[i])
			}
			return filepath.Clean(resolved), nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return absPath, nil
		}
		missing = append(missing, filepath.Base(current))
		current = parent
	}
}

func (f *NodeStore) workspaceContentPathForNode(entry *PageNode, op string) (string, bool, error) {
	sourcePath := entry.WorkspaceSourcePath.Clean().FilesystemPath()
	if sourcePath == "" {
		return "", false, nil
	}

	base := f.rootDir
	if sourcePath != "" {
		base = filepath.Join(f.rootDir, filepath.FromSlash(sourcePath))
	}
	switch entry.Kind {
	case NodeKindSection:
		indexPath, _, err := f.sectionIndexPathInDir(base)
		if err != nil {
			return "", false, err
		}
		if err := f.requirePathInRoot(op, indexPath); err != nil {
			return "", false, err
		}
		return indexPath, true, nil
	case NodeKindPage:
		if err := f.requirePathInRoot(op, base); err != nil {
			return "", false, err
		}
		return base, true, nil
	default:
		return "", false, nil
	}
}

// contentPathForNodeRead returns the expected content file path for a node
// based purely on the tree Kind (NO side effects, NO mkdir):
//   - page    => <base>.md
//   - section => active <base>/index.md case variant, active <base>/README.md fallback,
//     or <base>/index.md when no content file exists
func (f *NodeStore) contentPathForNodeRead(entry *PageNode) (string, error) {
	if entry == nil {
		return "", &InvalidOpError{Op: "contentPathForNodeRead", Reason: "an entry is required"}
	}
	if err := f.validateNodeRoute(entry); err != nil {
		return "", err
	}
	if sourcePath, ok, err := f.workspaceContentPathForNode(entry, "contentPathForNodeRead"); err != nil || ok {
		return sourcePath, err
	}

	base, err := f.dirPathForNode(entry)
	if err != nil {
		return "", err
	}
	switch entry.Kind {
	case NodeKindSection:
		path, _, err := f.sectionIndexPathInDir(base)
		if err != nil {
			return "", err
		}
		if err := f.requirePathInRoot("contentPathForNodeRead", path); err != nil {
			return "", err
		}
		return path, nil
	case NodeKindPage:
		path := base + ".md"
		if err := f.requirePathInRoot("contentPathForNodeRead", path); err != nil {
			return "", err
		}
		return path, nil
	default:
		return "", &InvalidOpError{Op: "contentPathForNodeRead", Reason: fmt.Sprintf("unknown node kind: %q", entry.Kind)}
	}
}

// contentPathForNodeWrite returns the expected content file path for a node
// based purely on the tree Kind (MAY create dirs for sections):
//   - page    => <base>.md
//   - section => active <base>/index.md case variant, active <base>/README.md fallback,
//     or <base>/index.md when no content file exists (ensures directory exists)
func (f *NodeStore) contentPathForNodeWrite(entry *PageNode) (string, error) {
	if entry == nil {
		return "", &InvalidOpError{Op: "contentPathForNodeWrite", Reason: "an entry is required"}
	}
	if err := f.validateNodeRoute(entry); err != nil {
		return "", err
	}
	if sourcePath, ok, err := f.workspaceContentPathForNode(entry, "contentPathForNodeWrite"); err != nil || ok {
		if err != nil {
			return "", err
		}
		if entry.Kind == NodeKindSection {
			if err := treeOSMkdirAll(filepath.Dir(sourcePath), 0o755); err != nil {
				return "", fmt.Errorf("%w: %w", ErrEnsureFolder, err)
			}
		}
		return sourcePath, nil
	}

	base, err := f.dirPathForNode(entry)
	if err != nil {
		return "", err
	}
	switch entry.Kind {
	case NodeKindSection:
		path, _, err := f.sectionIndexPathInDir(base)
		if err != nil {
			return "", err
		}
		if err := f.requirePathInRoot("contentPathForNodeWrite", path); err != nil {
			return "", err
		}
		if err := treeOSMkdirAll(base, 0o755); err != nil {
			return "", fmt.Errorf("%w: %w", ErrEnsureFolder, err)
		}
		return path, nil

	case NodeKindPage:
		path := base + ".md"
		if err := f.requirePathInRoot("contentPathForNodeWrite", path); err != nil {
			return "", err
		}
		return path, nil

	default:
		return "", &InvalidOpError{Op: "contentPathForNodeWrite", Reason: fmt.Sprintf("unknown node kind: %q", entry.Kind)}
	}
}
