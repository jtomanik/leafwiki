package tree

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/perber/wiki/internal/core/markdown"
)

// UpsertContentPreservingFrontmatter is the legacy-named importer variant of
// UpsertContent. It parses incoming metadata at the top of content and merges
// public fields into the system-managed canonical metadata written to disk.
func (f *NodeStore) UpsertContentPreservingFrontmatter(entry *PageNode, content string) error {
	if entry == nil {
		return &InvalidOpError{Op: "UpsertContentPreservingFrontmatter", Reason: "an entry is required"}
	}

	filePath, err := f.contentPathForNodeWrite(entry)
	if err != nil {
		return err
	}

	mdFile := markdown.NewMarkdownFile(filePath, "", markdown.Frontmatter{})
	if fileExists(filePath) {
		mdFile, err = treeLoadMarkdownFile(filePath)
		if err != nil {
			return fmt.Errorf("%w: %w", ErrLoadMarkdownFile, err)
		}
	}

	if err := mdFile.SetRawContentPreservingManagedMetadata(content); err != nil {
		return fmt.Errorf("could not parse markdown content: %w", err)
	}
	f.syncManagedMetadata(mdFile, entry)
	if err := treeMarkdownWriteToFile(mdFile); err != nil {
		return fmt.Errorf("%w: %w", ErrWriteMarkdownFile, err)
	}

	return nil
}

func (f *NodeStore) UpsertContentReplacingMetadata(entry *PageNode, content string) error {
	if entry == nil {
		return &InvalidOpError{Op: "UpsertContentReplacingMetadata", Reason: "an entry is required"}
	}

	filePath, err := f.contentPathForNodeWrite(entry)
	if err != nil {
		return err
	}

	mdFile := markdown.NewMarkdownFile(filePath, "", markdown.Frontmatter{})
	if fileExists(filePath) {
		mdFile, err = treeLoadMarkdownFile(filePath)
		if err != nil {
			return fmt.Errorf("%w: %w", ErrLoadMarkdownFile, err)
		}
	}

	if err := mdFile.SetRawContentReplacingManagedMetadata(content); err != nil {
		return fmt.Errorf("could not parse markdown content: %w", err)
	}
	f.syncManagedMetadata(mdFile, entry)
	if err := treeMarkdownWriteToFile(mdFile); err != nil {
		return fmt.Errorf("%w: %w", ErrWriteMarkdownFile, err)
	}

	return nil
}

// MoveNode moves a page to a other node
func (f *NodeStore) MoveNode(entry *PageNode, parentEntry *PageNode) error {
	if entry == nil {
		return &InvalidOpError{Op: "MoveNode", Reason: "an entry is required"}
	}
	if parentEntry == nil {
		return &InvalidOpError{Op: "MoveNode", Reason: "a parent entry is required"}
	}
	if entry.ID == "root" {
		return &InvalidOpError{Op: "MoveNode", Reason: "cannot move root"}
	}

	// Option A: children only under sections (defensive guard)
	if parentEntry.Kind != NodeKindSection {
		return &InvalidOpError{Op: "MoveNode", Reason: fmt.Sprintf("parent entry must be a section, got %q", parentEntry.Kind)}
	}

	parentDir, err := f.sectionDirPathForNode(parentEntry, "MoveNode")
	if err != nil {
		return err
	}

	if err := treeOSMkdirAll(parentDir, 0o755); err != nil {
		return fmt.Errorf("could not ensure parent directory exists: %w", err)
	}

	oldRoutePath := GenerateRoutePathFromPageNode(entry)
	newRoutePath := routePathForChild(parentEntry, entry.Slug)
	oldSourcePath := f.workspaceSourcePathForNode(entry)
	newSourcePath := joinWorkspaceRoutePath(f.workspaceSourceDirForSection(parentEntry), path.Base(oldSourcePath))

	oldFile := ""
	oldDir := ""
	switch entry.Kind {
	case NodeKindPage:
		oldFile, err = f.pageFilePathForNode(entry, "MoveNode")
		if err != nil {
			return err
		}
	case NodeKindSection:
		oldDir, err = f.sectionDirPathForNode(entry, "MoveNode")
		if err != nil {
			return err
		}
	}

	// Destination physical path keeps the source filename/dirname.
	destBase := filepath.Join(parentDir, path.Base(oldSourcePath))
	if err := f.requirePathInRoot("MoveNode", destBase); err != nil {
		return err
	}
	destFile := destBase
	destDir := destBase

	switch entry.Kind {
	case NodeKindPage:
		if fileExists(destFile) {
			return &PageAlreadyExistsError{Path: destBase}
		}
	case NodeKindSection:
		if fileExists(destDir) {
			return &PageAlreadyExistsError{Path: destBase}
		}
	default:
		return &InvalidOpError{Op: "MoveNode", Reason: fmt.Sprintf("unknown node kind: %q", entry.Kind)}
	}

	// STRICT: follow tree.Kind exactly (no disk fallbacks)
	switch entry.Kind {
	case NodeKindSection:
		// src must be a directory
		info, err := treeOSStat(oldDir)
		if err != nil {
			if os.IsNotExist(err) {
				f.log.Warn("move drift: expected folder missing", "nodeID", entry.ID, "expectedDir", oldDir)
				return &DriftError{NodeID: entry.ID, Kind: entry.Kind, Path: oldDir, Reason: "expected folder missing"}
			}
			return fmt.Errorf("stat source dir: %w", err)
		}
		if !info.IsDir() {
			f.log.Warn("move drift: expected folder but found file", "nodeID", entry.ID, "expectedDir", oldDir)
			return &DriftError{NodeID: entry.ID, Kind: entry.Kind, Path: oldDir, Reason: "expected folder but found file"}
		}

		if err := treeOSRename(oldDir, destDir); err != nil {
			return fmt.Errorf("could not move folder: %w", err)
		}
		f.updateWorkspaceSourcePathsForSubtree(entry, oldRoutePath, newRoutePath, oldSourcePath, newSourcePath)

	case NodeKindPage:
		// src must be a file
		info, err := treeOSStat(oldFile)
		if err != nil {
			if os.IsNotExist(err) {
				f.log.Warn("move drift: expected file missing", "nodeID", entry.ID, "expectedFile", oldFile)
				return &DriftError{NodeID: entry.ID, Kind: entry.Kind, Path: oldFile, Reason: "expected file missing"}
			}
			return fmt.Errorf("stat source file: %w", err)
		}
		if info.IsDir() {
			f.log.Warn("move drift: expected file but found folder", "nodeID", entry.ID, "expectedFile", oldFile)
			return &DriftError{NodeID: entry.ID, Kind: entry.Kind, Path: oldFile, Reason: "expected file but found folder"}
		}

		if err := treeOSRename(oldFile, destFile); err != nil {
			return fmt.Errorf("could not move file: %w", err)
		}
		f.setWorkspaceSourcePath(entry, newRoutePath, NodeKindPage, newSourcePath)
	}

	return nil
}

// DeletePage deletes a page file from disk
func (f *NodeStore) DeletePage(entry *PageNode) error {
	if entry == nil {
		return &InvalidOpError{Op: "DeletePage", Reason: "an entry is required"}
	}
	if entry.ID == "root" {
		return &InvalidOpError{Op: "DeletePage", Reason: "cannot delete root"}
	}
	if entry.Kind != NodeKindPage && entry.Kind != "" {
		return &InvalidOpError{Op: "DeletePage", Reason: "entry must be a page"}
	}

	file, err := f.pageFilePathForNode(entry, "DeletePage")
	if err != nil {
		return err
	}

	info, err := treeOSStat(file)
	if err != nil {
		if os.IsNotExist(err) {
			f.log.Warn("delete drift: expected page file missing", "nodeID", entry.ID, "expectedFile", file)
			return &DriftError{NodeID: entry.ID, Kind: entry.Kind, Path: file, Reason: "expected file missing"}
		}
		return fmt.Errorf("stat file: %w", err)
	}
	if info.IsDir() {
		f.log.Warn("delete drift: expected file but found folder", "nodeID", entry.ID, "expectedFile", file)
		return &DriftError{NodeID: entry.ID, Kind: entry.Kind, Path: file, Reason: "expected file but found folder"}
	}

	if err := treeOSRemove(file); err != nil {
		return fmt.Errorf("could not delete file: %w", err)
	}

	return nil
}

// DeleteSection deletes a section folder from disk
func (f *NodeStore) DeleteSection(entry *PageNode) error {
	if entry == nil {
		return &InvalidOpError{Op: "DeleteSection", Reason: "an entry is required"}
	}
	if entry.ID == "root" {
		return &InvalidOpError{Op: "DeleteSection", Reason: "cannot delete root"}
	}
	if entry.Kind != NodeKindSection {
		return &InvalidOpError{Op: "DeleteSection", Reason: "entry must be a section"}
	}

	dir, err := f.sectionDirPathForNode(entry, "DeleteSection")
	if err != nil {
		return err
	}

	info, err := treeOSStat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			f.log.Warn("delete drift: expected section folder missing", "nodeID", entry.ID, "expectedDir", dir)
			return &DriftError{NodeID: entry.ID, Kind: entry.Kind, Path: dir, Reason: "expected folder missing"}
		}
		return fmt.Errorf("stat dir: %w", err)
	}
	if !info.IsDir() {
		f.log.Warn("delete drift: expected folder but found file", "nodeID", entry.ID, "expectedDir", dir)
		return &DriftError{NodeID: entry.ID, Kind: entry.Kind, Path: dir, Reason: "expected folder but found file"}
	}

	if err := treeOSRemoveAll(dir); err != nil {
		return fmt.Errorf("could not delete folder: %w", err)
	}

	return nil
}

// RenameNode renames a node's slug on disk
func (f *NodeStore) RenameNode(entry *PageNode, newSlug Slug) error {
	if entry == nil {
		return &InvalidOpError{Op: "RenameNode", Reason: "an entry is required"}
	}
	if strings.TrimSpace(newSlug.FilesystemPath()) == "" {
		return &InvalidOpError{Op: "RenameNode", Reason: "new slug must not be empty"}
	}
	if err := validateNodeSlug("RenameNode", newSlug); err != nil {
		return err
	}
	if entry.Slug == newSlug {
		return nil
	}
	if entry.ID == "root" {
		return &InvalidOpError{Op: "RenameNode", Reason: "cannot rename root"}
	}

	oldRoutePath := GenerateRoutePathFromPageNode(entry)
	newRoutePath := routePathWithLeafSlug(entry, newSlug)
	oldSourcePath := f.workspaceSourcePathForNode(entry)
	newSourcePath := joinWorkspaceRoutePath(path.Dir(oldSourcePath), newSlug.FilesystemPath())
	if path.Dir(oldSourcePath) == "." {
		newSourcePath = newSlug.FilesystemPath()
	}
	if entry.Kind == NodeKindPage {
		newSourcePath += ".md"
	}
	newPath := filepath.Join(f.rootDir, filepath.FromSlash(newSourcePath))
	if err := f.requirePathInRoot("RenameNode", newPath); err != nil {
		return err
	}

	switch entry.Kind {
	case NodeKindPage:
		if fileExists(newPath) {
			return &PageAlreadyExistsError{Path: strings.TrimSuffix(newPath, ".md")}
		}
	case NodeKindSection:
		if fileExists(newPath) {
			return &PageAlreadyExistsError{Path: newPath}
		}
	default:
		return &InvalidOpError{Op: "RenameNode", Reason: fmt.Sprintf("unknown node kind: %q", entry.Kind)}
	}
	// perform rename based on kind
	if entry.Kind == NodeKindSection {
		srcDir, err := f.sectionDirPathForNode(entry, "RenameNode")
		if err != nil {
			return err
		}
		dstDir := newPath

		// strict: source dir must exist and be dir
		info, err := treeOSStat(srcDir)
		if err != nil {
			if os.IsNotExist(err) {
				return &DriftError{NodeID: entry.ID, Kind: entry.Kind, Path: srcDir, Reason: "expected folder missing"}
			}
			return fmt.Errorf("stat source dir: %w", err)
		}
		if !info.IsDir() {
			// drift: tree says section but disk is not a folder
			f.log.Warn("drift: tree says section but disk is not a folder", "srcDir", srcDir)
			return &DriftError{NodeID: entry.ID, Kind: entry.Kind, Path: srcDir, Reason: "expected folder but found file"}
		}

		if err := treeOSRename(srcDir, dstDir); err != nil {
			return fmt.Errorf("could not rename folder: %w", err)
		}
		f.updateWorkspaceSourcePathsForSubtree(entry, oldRoutePath, newRoutePath, oldSourcePath, newSourcePath)
		return nil
	}

	srcFile, err := f.pageFilePathForNode(entry, "RenameNode")
	if err != nil {
		return err
	}
	dstFile := newPath

	// strict: source file must exist
	info, err := treeOSStat(srcFile)
	if err != nil {
		if os.IsNotExist(err) {
			return &DriftError{NodeID: entry.ID, Kind: entry.Kind, Path: srcFile, Reason: "expected file missing"}
		}
		return fmt.Errorf("stat source file: %w", err)
	}
	if info.IsDir() {
		// drift: tree says page but disk is a dir
		f.log.Warn("drift: tree says page but disk is a dir", "srcFile", srcFile)
		return &DriftError{NodeID: entry.ID, Kind: entry.Kind, Path: srcFile, Reason: "expected file but found folder"}
	}

	if err := treeOSRename(srcFile, dstFile); err != nil {
		return fmt.Errorf("could not rename file: %w", err)
	}
	f.setWorkspaceSourcePath(entry, newRoutePath, NodeKindPage, newSourcePath)
	return nil
}

// ReadPageRaw returns the raw content of a page, including metadata.
func (f *NodeStore) ReadPageRaw(entry *PageNode) (string, error) {
	filePath, err := f.contentPathForNodeRead(entry)
	if err != nil {
		return "", err
	}

	// Sections may legitimately have no active content file.
	if entry.Kind == NodeKindSection {
		if !fileExists(filePath) {
			return "", nil
		}
	} else {
		// Pages must have a content file
		if !fileExists(filePath) {
			return "", &DriftError{NodeID: entry.ID, Kind: entry.Kind, Path: filePath, Reason: "expected page file missing"}
		}
	}

	raw, err := treeOSReadFile(filePath)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// ReadPageAndRaw returns both the stripped content and the raw markdown string
// from a single disk read.
func (f *NodeStore) ReadPageAndRaw(entry *PageNode) (content, raw string, err error) {
	raw, err = f.ReadPageRaw(entry)
	if err != nil || raw == "" {
		return "", raw, err
	}

	filePath, err := f.contentPathForNodeRead(entry)
	if err != nil {
		return "", raw, err
	}

	mdFile, err := treeNewMarkdownFileFromRaw(filePath, raw)
	if err != nil {
		return raw, raw, err
	}

	return mdFile.GetContent(), raw, nil
}

// ReadPageContent returns the content of a page
func (f *NodeStore) ReadPageContent(entry *PageNode) (string, error) {
	raw, err := f.ReadPageRaw(entry)
	if err != nil {
		return "", err
	}

	filePath, err := f.contentPathForNodeRead(entry)
	if err != nil {
		return "", err
	}

	mdFile, err := treeNewMarkdownFileFromRaw(filePath, raw)
	if err != nil {
		return raw, err
	}

	return mdFile.GetContent(), nil
}
