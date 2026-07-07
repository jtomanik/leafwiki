package tree

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/perber/wiki/internal/core/markdown"
)

// resolveNode inspects the filesystem to determine if the given PageNode
// corresponds to a file or folder, returning a ResolvedNode with details.
// This function is only used for migration. Other parts of the system should rely on contentPathForNodeRead or contentPathForNodeWrite.
// If this function is used outside of migration, it may lead to inconsistencies between the tree and the actual filesystem state.
func (f *NodeStore) resolveNode(entry *PageNode) (*ResolvedNode, error) {
	basePath, err := f.dirPathForNode(entry)
	if err != nil {
		return nil, err
	}

	// 1) File?
	if _, err := treeOSStat(basePath + ".md"); err == nil {
		f.log.Debug("resolved as file node", "filePath", basePath+".md")
		return &ResolvedNode{
			Kind:       NodeKindPage,
			FilePath:   basePath + ".md",
			HasContent: true,
		}, nil
	}

	// 2) Folder?
	if info, err := treeOSStat(basePath); err == nil && info.IsDir() {
		index, hasIndex, err := f.sectionIndexPathInDir(basePath)
		if err != nil {
			return nil, err
		}
		if hasIndex {
			f.log.Debug("resolved as section node with content", "dirPath", basePath, "filePath", index)
			return &ResolvedNode{
				Kind:       NodeKindSection,
				DirPath:    basePath,
				FilePath:   index,
				HasContent: true,
			}, nil
		}
		f.log.Debug("resolved as section node without content", "dirPath", basePath)
		return &ResolvedNode{
			Kind:       NodeKindSection,
			DirPath:    basePath,
			FilePath:   "", // no active section content file present
			HasContent: false,
		}, nil
	}

	return nil, &NotFoundError{Resource: "node", Path: basePath, ID: entry.ID}
}

// ConvertNode converts the on-disk representation between page <-> folder.
// NOTE: TreeService must ensure folder->page is allowed (no children).
func (f *NodeStore) ConvertNode(entry *PageNode, target NodeKind) error {
	if entry == nil {
		return &InvalidOpError{Op: "ConvertNode", Reason: "an entry is required"}
	}

	routePath := GenerateRoutePathFromPageNode(entry)
	parentSourceDir := ""
	if entry.Parent != nil {
		parentSourceDir = f.workspaceSourceDirForSection(entry.Parent)
	}

	switch target {
	case NodeKindSection:
		return f.convertNodeToSection(entry, routePath, parentSourceDir)
	case NodeKindPage:
		return f.convertNodeToPage(entry, routePath, parentSourceDir)
	default:
		return &InvalidOpError{Op: "ConvertNode", Reason: fmt.Sprintf("unknown target kind: %q", target)}
	}
}

func (f *NodeStore) convertNodeToSection(entry *PageNode, routePath RoutePath, parentSourceDir string) error {
	filePath, err := f.pageFilePathForNode(entry, "ConvertNode")
	if err != nil {
		return err
	}
	folderSourcePath := joinWorkspaceRoutePath(parentSourceDir, entry.Slug.FilesystemPath())
	folderPath := filepath.Join(f.rootDir, filepath.FromSlash(folderSourcePath))
	if err := f.requirePathInRoot("ConvertNode", folderPath); err != nil {
		return err
	}
	indexPath := filepath.Join(folderPath, "index.md")

	if fileExists(filePath) {
		return f.movePageFileToSectionIndex(entry, routePath, filePath, folderPath, indexPath, folderSourcePath)
	}
	return f.ensureNodeSection(entry, routePath, folderPath, folderSourcePath)
}

func (f *NodeStore) movePageFileToSectionIndex(entry *PageNode, routePath RoutePath, filePath string, folderPath string, indexPath string, folderSourcePath string) error {
	if err := treeOSMkdirAll(folderPath, 0o755); err != nil {
		return fmt.Errorf("could not create folder: %w", err)
	}
	if err := treeOSRename(filePath, indexPath); err != nil {
		return fmt.Errorf("could not move page into folder: %w", err)
	}
	return f.ensureNodeSection(entry, routePath, folderPath, folderSourcePath)
}

func (f *NodeStore) ensureNodeSection(entry *PageNode, routePath RoutePath, folderPath string, folderSourcePath string) error {
	if err := treeOSMkdirAll(folderPath, 0o755); err != nil {
		return fmt.Errorf("could not ensure folder exists: %w", err)
	}
	entry.Kind = NodeKindSection
	f.setWorkspaceSourcePath(entry, routePath, NodeKindSection, folderSourcePath)
	_, err := f.ensureSectionIndex(entry)
	return err
}

func (f *NodeStore) convertNodeToPage(entry *PageNode, routePath RoutePath, parentSourceDir string) error {
	folderPath, err := f.sectionDirPathForNode(entry, "ConvertNode")
	if err != nil {
		return err
	}
	pageSourcePath := joinWorkspaceRoutePath(parentSourceDir, entry.Slug.FilesystemPath()+".md")
	filePath := filepath.Join(f.rootDir, filepath.FromSlash(pageSourcePath))
	if err := f.requirePathInRoot("ConvertNode", filePath); err != nil {
		return err
	}
	exists, err := f.requireConvertibleSectionFolder(entry, folderPath)
	if err != nil || !exists {
		return err
	}
	if err := f.materializePageFromSectionIndex(entry, filePath, filepath.Join(folderPath, "index.md")); err != nil {
		return err
	}
	return f.finishConvertedPage(entry, routePath, pageSourcePath, folderPath)
}

func (f *NodeStore) finishConvertedPage(entry *PageNode, routePath RoutePath, pageSourcePath string, folderPath string) error {
	f.setWorkspaceSourcePath(entry, routePath, NodeKindPage, pageSourcePath)
	if err := removeChildOrderFile(folderPath); err != nil {
		return err
	}
	return treeOSRemove(folderPath)
}

func (f *NodeStore) requireConvertibleSectionFolder(entry *PageNode, folderPath string) (bool, error) {
	info, err := treeOSStat(folderPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if !info.IsDir() {
		return false, &DriftError{NodeID: entry.ID, Kind: NodeKindSection, Path: folderPath, Reason: "expected folder but found file"}
	}

	entries, err := treeOSReadDir(folderPath)
	if err != nil {
		return false, err
	}
	if !folderAllowsPageConversion(entries) {
		return false, &ConvertNotAllowedError{From: NodeKindSection, To: NodeKindPage, Reason: "folder not empty"}
	}
	return true, nil
}

func folderAllowsPageConversion(entries []os.DirEntry) bool {
	for _, e := range entries {
		name := e.Name()
		if name != "index.md" && name != orderFilename {
			return false
		}
	}
	return true
}

func (f *NodeStore) materializePageFromSectionIndex(entry *PageNode, filePath string, indexPath string) error {
	if fileExists(indexPath) {
		if err := treeOSRename(indexPath, filePath); err != nil {
			return fmt.Errorf("could not move index to page: %w", err)
		}
		return nil
	}
	mdFile := markdown.NewMarkdownFile(filePath, "", markdown.Frontmatter{})
	f.syncManagedMetadata(mdFile, entry)
	if err := treeMarkdownWriteToFile(mdFile); err != nil {
		return fmt.Errorf("could not write page file: %w", err)
	}
	return nil
}

func removeChildOrderFile(folderPath string) error {
	if err := treeOSRemove(filepath.Join(folderPath, orderFilename)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("could not remove child order file: %w", err)
	}
	return nil
}
