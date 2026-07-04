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

		// page -> folder
		if _, err := treeOSStat(filePath); err == nil {
			if err := treeOSMkdirAll(folderPath, 0o755); err != nil {
				return fmt.Errorf("could not create folder: %w", err)
			}
			// keep content: <slug>.md -> <slug>/index.md
			if err := treeOSRename(filePath, indexPath); err != nil {
				return fmt.Errorf("could not move page into folder: %w", err)
			}
			entry.Kind = NodeKindSection
			f.setWorkspaceSourcePath(entry, routePath, NodeKindSection, folderSourcePath)
			if _, err := f.ensureSectionIndex(entry); err != nil {
				return err
			}
			return nil
		}
		// Already folder (or missing) -> ensure dir exists and sync/materialize
		// the active section content file, defaulting to index.md when none exists.
		if err := treeOSMkdirAll(folderPath, 0o755); err != nil {
			return fmt.Errorf("could not ensure folder exists: %w", err)
		}
		entry.Kind = NodeKindSection
		f.setWorkspaceSourcePath(entry, routePath, NodeKindSection, folderSourcePath)
		if _, err := f.ensureSectionIndex(entry); err != nil {
			return err
		}
		return nil

	case NodeKindPage:
		folderPath, err := f.sectionDirPathForNode(entry, "ConvertNode")
		if err != nil {
			return err
		}
		pageSourcePath := joinWorkspaceRoutePath(parentSourceDir, entry.Slug.FilesystemPath()+".md")
		filePath := filepath.Join(f.rootDir, filepath.FromSlash(pageSourcePath))
		if err := f.requirePathInRoot("ConvertNode", filePath); err != nil {
			return err
		}
		indexPath := filepath.Join(folderPath, "index.md")

		// folder -> page (strict, safe order)
		info, err := treeOSStat(folderPath)
		if err != nil {
			if os.IsNotExist(err) {
				// nothing to do if folder doesn't exist
				return nil
			}
			return err
		}
		if !info.IsDir() {
			return &DriftError{NodeID: entry.ID, Kind: NodeKindSection, Path: folderPath, Reason: "expected folder but found file"}
		}

		entries, err := treeOSReadDir(folderPath)
		if err != nil {
			return err
		}

		// Allow only the narrow section-to-page conversion shape. README.md
		// fallback content is not converted here; folders with README.md are
		// treated as non-empty and require an explicit migration first.
		// - empty folder
		// - folder with only index.md
		// - internal child-order metadata file (alone or alongside index.md)
		allowed := true
		for _, e := range entries {
			name := e.Name()
			if name == "index.md" || name == orderFilename {
				continue
			}
			allowed = false
			break
		}
		if !allowed {
			return &ConvertNotAllowedError{From: NodeKindSection, To: NodeKindPage, Reason: "folder not empty"}
		}

		// now do the move/create
		if fileExists(indexPath) {
			if err := treeOSRename(indexPath, filePath); err != nil {
				return fmt.Errorf("could not move index to page: %w", err)
			}
		} else {
			mdFile := markdown.NewMarkdownFile(filePath, "", markdown.Frontmatter{})
			f.syncManagedMetadata(mdFile, entry)
			if err := treeMarkdownWriteToFile(mdFile); err != nil {
				return fmt.Errorf("could not write page file: %w", err)
			}
		}
		f.setWorkspaceSourcePath(entry, routePath, NodeKindPage, pageSourcePath)

		if err := treeOSRemove(filepath.Join(folderPath, orderFilename)); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("could not remove child order file: %w", err)
		}

		// remove folder (must be empty now)
		if err := treeOSRemove(folderPath); err != nil {
			return err
		}
		return nil

	default:
		return &InvalidOpError{Op: "ConvertNode", Reason: fmt.Sprintf("unknown target kind: %q", target)}
	}
}
