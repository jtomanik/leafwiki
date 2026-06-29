package tree

import (
	"fmt"
	"path/filepath"
	"strings"
)

// checkNodeVersion enforces optimistic locking inside a write lock.
// Legacy nodes with no UpdatedAt (empty version) skip the check.
// Pass the tree-owned unchecked sentinel through constrained operations to
// bypass for internal system operations.
func checkNodeVersion(node *PageNode, expectedVersion PageVersion) error {
	if expectedVersion.IsUnchecked() {
		return nil
	}
	nodeVersion := node.Version()
	if nodeVersion == "" {
		return nil
	}
	if expectedVersion == "" {
		return ErrVersionRequired
	}
	if nodeVersion != expectedVersion {
		return ErrVersionConflict
	}
	return nil
}

func GeneratePathFromPageNode(entry *PageNode) RoutePath {
	path := ""
	if entry.Parent != nil {
		return GeneratePathFromPageNode(entry.Parent).Child(entry.Slug)
	} else {
		path = entry.Slug.FilesystemPath()
	}
	return RoutePathFromString(path)
}

func GenerateRoutePathFromPageNode(entry *PageNode) RoutePath {
	if entry == nil || entry.ID == "root" || entry.Parent == nil {
		return ""
	}
	if parentPath := GenerateRoutePathFromPageNode(entry.Parent); parentPath != "" {
		return parentPath.Child(entry.Slug)
	}
	return entry.Slug.RoutePath()
}

func pageDirectoryDiskPath(storageDir string, pagePath string) string {
	normalizedStorageDir := filepath.FromSlash(strings.ReplaceAll(storageDir, `\`, `/`))
	normalizedPagePath := filepath.FromSlash(strings.ReplaceAll(pagePath, `\`, `/`))
	return filepath.Join(normalizedStorageDir, normalizedPagePath)
}

func pageMarkdownDiskPath(storageDir string, pagePath string) string {
	normalizedStorageDir := filepath.FromSlash(strings.ReplaceAll(storageDir, `\`, `/`))
	normalizedPagePath := filepath.FromSlash(strings.ReplaceAll(pagePath, `\`, `/`))
	return filepath.Join(normalizedStorageDir, normalizedPagePath+".md")
}

func pageIndexDiskPath(storageDir string, pagePath string) string {
	return filepath.Join(pageDirectoryDiskPath(storageDir, pagePath), "index.md")
}

// EnsurePageIsFolder checks if a page path is still a flat .md file,
// and if so, converts it into a folder with an index.md file.
func EnsurePageIsFolder(storageDir string, route RoutePath) error {
	routeString := route.FilesystemPath()
	mdPath := pageMarkdownDiskPath(storageDir, routeString)
	dirPath := pageDirectoryDiskPath(storageDir, routeString)

	// Already a folder? Nothing to do.
	if info, err := treeOSStat(dirPath); err == nil && info.IsDir() {
		return nil
	}

	// If .md file exists → convert it to folder
	if _, err := treeOSStat(mdPath); err == nil {
		if err := treeOSMkdirAll(dirPath, 0755); err != nil {
			return fmt.Errorf("%w: %w", ErrEnsureFolder, err)
		}

		newPath := pageIndexDiskPath(storageDir, routeString)
		if err := treeOSRename(mdPath, newPath); err != nil {
			return fmt.Errorf("could not move file to index.md: %w", err)
		}
	}

	return nil
}

// FoldPageFolderIfEmpty converts a page folder back into a flat file
// if it contains only "index.md" and nothing else.
func FoldPageFolderIfEmpty(storageDir string, pagePath string) error {
	dirPath := pageDirectoryDiskPath(storageDir, pagePath)
	mdPath := pageMarkdownDiskPath(storageDir, pagePath)
	indexPath := pageIndexDiskPath(storageDir, pagePath)

	// Only run if it's actually a folder
	info, err := treeOSStat(dirPath)
	if err != nil || !info.IsDir() {
		return nil // nothing to do
	}

	entries, err := treeOSReadDir(dirPath)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrReadDirectory, err)
	}

	// Only fold if exactly 1 file: index.md
	if len(entries) != 1 || entries[0].Name() != "index.md" {
		return nil
	}

	// Move index.md → page.md
	if err := treeOSRename(indexPath, mdPath); err != nil {
		return fmt.Errorf("could not move index.md to flat file: %w", err)
	}

	// Remove the now-empty folder
	if err := treeOSRemove(dirPath); err != nil {
		return fmt.Errorf("could not remove folder: %w", err)
	}

	return nil
}
