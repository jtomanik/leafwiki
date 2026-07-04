package tree

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/perber/wiki/internal/core/markdown"
)

func (f *NodeStore) reconstructTreeRecursive(currentPath string, parent *PageNode, reconstructNow time.Time, seenIDs map[PageID]string) error {
	entries, err := treeOSReadDir(currentPath)
	if err != nil {
		return fmt.Errorf("%w %s: %w", ErrReadDirectory, currentPath, err)
	}
	seenSlugs := map[reconstructedSlugKey]string{}

	// stable, deterministic ordering (case-insensitive, with case-sensitive tie-breaker)
	sort.SliceStable(entries, func(i, j int) bool {
		li := strings.ToLower(entries[i].Name())
		lj := strings.ToLower(entries[j].Name())
		if li == lj {
			return entries[i].Name() < entries[j].Name()
		}
		return li < lj
	})

	for _, entry := range entries {
		name := entry.Name()
		entryPath := filepath.Join(currentPath, name)

		// optional: skip hidden stuff
		if strings.HasPrefix(name, ".") {
			continue
		}

		relPath, err := treeFilepathRel(f.rootDir, entryPath)
		if err != nil {
			return fmt.Errorf("resolve workspace relative path for %s: %w", entryPath, err)
		}
		mappedRoute, err := treeMapWorkspaceMarkdownRoute(f.rootDir, relPath, entry.IsDir())
		if err != nil {
			f.log.Error("skipping workspace path with invalid route", "path", relPath, "error", err)
			continue
		}
		if mappedRoute.Skip {
			continue
		}

		// defaults
		title := name
		id, err := treeGenerateUniqueID()
		metadata := f.metadataFromPageMetadata(markdown.PageMetadata{}, reconstructNow, entryPath)
		if err != nil {
			return fmt.Errorf("generate unique ID: %w", err)
		}

		if entry.IsDir() {
			slug := workspaceRouteLeafSlug(mappedRoute.RoutePath)
			if slug == "" {
				f.log.Error("skipping directory with empty route slug", "directory", name)
				continue
			}
			sectionDir := entryPath
			indexPath, hasIndex, err := f.sectionIndexPathInDir(sectionDir)
			if err != nil {
				return fmt.Errorf("resolve section index for %s: %w", sectionDir, err)
			}
			var sectionMdFile *markdown.MarkdownFile
			needsWriteback := false
			if hasIndex {
				mdFile, err := treeLoadMarkdownFile(indexPath)
				if err != nil {
					return fmt.Errorf("load section index %s: %w", indexPath, err)
				} else {
					meta := mdFile.GetMetadata()
					metadata = f.metadataFromPageMetadata(meta, reconstructNow, indexPath)
					title, _ = mdFile.GetTitle()
					if strings.TrimSpace(meta.Page.ID) != "" {
						id = strings.TrimSpace(meta.Page.ID)
					}
					if mdFile.RequiresWriteback() || strings.TrimSpace(meta.Page.ID) == "" || strings.TrimSpace(meta.Page.UpdatedAt) == "" || strings.TrimSpace(meta.Page.CreatedAt) == "" {
						sectionMdFile = mdFile
						needsWriteback = true
					}
				}
			}

			child := &PageNode{
				ID:                  PageIDFromString(id),
				Slug:                slug,
				Title:               title,
				Parent:              parent,
				Position:            len(parent.Children),
				Children:            []*PageNode{},
				Kind:                NodeKindSection,
				WorkspaceSourcePath: nonDefaultWorkspaceSourcePath(mappedRoute),
				Metadata:            metadata,
			}
			if err := ensureUniqueReconstructedSlug(seenSlugs, child.Slug, child.Kind, entryPath); err != nil {
				return err
			}
			if err := ensureUniqueReconstructedID(seenIDs, child.ID, indexPath); err != nil {
				return err
			}
			parent.Children = append(parent.Children, child)

			if needsWriteback {
				if err := f.writeReconstructedMetadata(sectionMdFile, child); err != nil {
					return err
				}
			}

			if !hasIndex {
				if _, err := f.ensureSectionIndexAtPath(child, indexPath); err != nil {
					return fmt.Errorf("materialize missing section index for %s: %w", indexPath, err)
				}
			}

			if err := f.reconstructTreeRecursive(entryPath, child, reconstructNow, seenIDs); err != nil {
				return err
			}
			continue
		}

		// file
		ext := filepath.Ext(name)
		if !strings.EqualFold(ext, ".md") {
			continue
		}

		// Skip index-style files handled by the section case. Active README.md
		// fallback files are skipped by isSectionContentFileInDir below.
		if mappedRoute.Kind == NodeKindSection {
			continue
		}

		filePath := entryPath
		if f.isSectionContentFileInDir(currentPath, filePath) {
			continue
		}
		slug := workspaceRouteLeafSlug(mappedRoute.RoutePath)
		if slug == "" {
			f.log.Error("skipping markdown file with empty route slug", "file", name)
			continue
		}

		mdFile, err := treeLoadMarkdownFile(filePath)
		if err != nil {
			return fmt.Errorf("load markdown file %s: %w", filePath, err)
		}
		meta := mdFile.GetMetadata()
		metadata = f.metadataFromPageMetadata(meta, reconstructNow, filePath)
		title, _ = mdFile.GetTitle()
		if strings.TrimSpace(meta.Page.ID) != "" {
			id = strings.TrimSpace(meta.Page.ID)
		}
		needsWriteback := mdFile.RequiresWriteback() || strings.TrimSpace(meta.Page.ID) == "" || strings.TrimSpace(meta.Page.UpdatedAt) == "" || strings.TrimSpace(meta.Page.CreatedAt) == ""

		child := &PageNode{
			ID:                  PageIDFromString(id),
			Slug:                slug,
			Title:               title,
			Parent:              parent,
			Position:            len(parent.Children),
			Children:            nil,
			Kind:                NodeKindPage,
			WorkspaceSourcePath: nonDefaultWorkspaceSourcePath(mappedRoute),
			Metadata:            metadata,
		}
		if err := ensureUniqueReconstructedSlug(seenSlugs, child.Slug, child.Kind, filePath); err != nil {
			return err
		}
		if err := ensureUniqueReconstructedID(seenIDs, child.ID, filePath); err != nil {
			return err
		}
		if needsWriteback {
			if err := f.writeReconstructedMetadata(mdFile, child); err != nil {
				return err
			}
		}
		parent.Children = append(parent.Children, child)
	}

	f.applyChildOrder(parent, currentPath)

	return nil
}

func (f *NodeStore) applyChildOrder(parent *PageNode, dirPath string) {
	if parent == nil || len(parent.Children) < 2 {
		return
	}

	order, err := f.readChildOrder(dirPath)
	if err != nil {
		f.log.Warn("could not read child order file, keeping default order", "path", filepath.Join(dirPath, orderFilename), "error", err)
		return
	}
	if len(order.OrderedIDs) == 0 {
		return
	}

	positions := make(map[PageID]int, len(order.OrderedIDs))
	for i, id := range order.OrderedIDs {
		if _, exists := positions[id]; exists {
			continue
		}
		positions[id] = i
	}

	sort.SliceStable(parent.Children, func(i, j int) bool {
		pi, okI := positions[parent.Children[i].ID]
		pj, okJ := positions[parent.Children[j].ID]
		switch {
		case okI && okJ:
			return pi < pj
		case okI:
			return true
		case okJ:
			return false
		default:
			return parent.Children[i].Position < parent.Children[j].Position
		}
	})

	for i, child := range parent.Children {
		child.Position = i
	}
}

func (f *NodeStore) readChildOrder(dirPath string) (*childOrderFile, error) {
	raw, err := treeOSReadFile(filepath.Join(dirPath, orderFilename))
	if err != nil {
		if os.IsNotExist(err) {
			return &childOrderFile{}, nil
		}
		return nil, err
	}

	var order childOrderFile
	if err := treeJSONUnmarshal(raw, &order); err != nil {
		return nil, err
	}
	return &order, nil
}

func (f *NodeStore) SaveChildOrder(parent *PageNode) error {
	if parent == nil {
		return &InvalidOpError{Op: "SaveChildOrder", Reason: "a parent entry is required"}
	}
	if parent.ID != "root" && parent.Kind != NodeKindSection {
		return &InvalidOpError{Op: "SaveChildOrder", Reason: "parent entry must be root or a section"}
	}

	dirPath, err := f.sectionDirPathForNode(parent, "SaveChildOrder")
	if err != nil {
		return err
	}
	if err := treeOSMkdirAll(dirPath, 0o755); err != nil {
		return fmt.Errorf("could not ensure parent directory exists: %w", err)
	}

	orderedIDs := make([]PageID, 0, len(parent.Children))
	for _, child := range parent.Children {
		if child == nil {
			continue
		}
		orderedIDs = append(orderedIDs, child.ID)
	}

	data, _ := json.MarshalIndent(childOrderFile{OrderedIDs: orderedIDs}, "", "  ")
	data = append(data, byte('\n'))

	if err := treeWriteFileAtomic(filepath.Join(dirPath, orderFilename), data, 0o644); err != nil {
		return fmt.Errorf("%w: %w", ErrPersistChildOrder, err)
	}

	return nil
}

func (f *NodeStore) assignParentToChildren(parent *PageNode) {
	assignParentToChildren(parent)
}

func assignParentToChildren(parent *PageNode) {
	for _, child := range parent.Children {
		child.Parent = parent
		assignParentToChildren(child)
	}
}

// CreatePage creates a new page file under the given parent entry
func (f *NodeStore) CreatePage(parentEntry *PageNode, newEntry *PageNode) error {
	if parentEntry == nil {
		return &InvalidOpError{Op: "CreatePage", Reason: "a parent entry is required"}
	}
	if newEntry == nil {
		return &InvalidOpError{Op: "CreatePage", Reason: "a new entry is required"}
	}
	if newEntry.ID == "root" {
		return &InvalidOpError{Op: "CreatePage", Reason: "cannot create root"}
	}
	if err := validateNodeSlug("CreatePage", newEntry.Slug); err != nil {
		return err
	}

	// Pages can only be created under sections (Option A)
	if parentEntry.Kind != NodeKindSection {
		return &InvalidOpError{Op: "CreatePage", Reason: "parent entry must be a section"}
	}
	if newEntry.Kind != NodeKindPage {
		return &InvalidOpError{Op: "CreatePage", Reason: "new entry must be a page"}
	}

	parentDir, err := f.sectionDirPathForNode(parentEntry, "CreatePage")
	if err != nil {
		return err
	}

	// Ensure the parent directory exists (idempotent)
	if err := treeOSMkdirAll(parentDir, 0o755); err != nil {
		return fmt.Errorf("could not ensure parent directory exists: %w", err)
	}

	// Destination paths
	destBase := filepath.Join(parentDir, newEntry.Slug.FilesystemPath())
	if err := f.requirePathInRoot("CreatePage", destBase); err != nil {
		return err
	}
	destFile := destBase + ".md"

	if fileExists(destFile) {
		return &PageAlreadyExistsError{Path: destBase}
	}

	mdFile := markdown.NewMarkdownFile(destFile, "# "+newEntry.Title+"\n", markdown.Frontmatter{})
	f.syncManagedMetadata(mdFile, newEntry)
	if err := treeMarkdownWriteToFile(mdFile); err != nil {
		return fmt.Errorf("could not create file: %w", err)
	}
	f.setWorkspaceSourcePathForPhysicalPath(newEntry, destFile, GenerateRoutePathFromPageNode(newEntry), NodeKindPage)

	return nil
}

// CreateSection creates a new section (folder) under the given parent entry.
func (f *NodeStore) CreateSection(parentEntry *PageNode, newEntry *PageNode) error {
	if parentEntry == nil {
		return &InvalidOpError{Op: "CreateSection", Reason: "a parent entry is required"}
	}
	if newEntry == nil {
		return &InvalidOpError{Op: "CreateSection", Reason: "a new entry is required"}
	}
	if newEntry.ID == "root" {
		return &InvalidOpError{Op: "CreateSection", Reason: "cannot create root"}
	}
	if err := validateNodeSlug("CreateSection", newEntry.Slug); err != nil {
		return err
	}

	// Sections can only be created under sections (Option A)
	if parentEntry.Kind != NodeKindSection {
		return &InvalidOpError{Op: "CreateSection", Reason: "parent entry must be a section"}
	}
	if newEntry.Kind != NodeKindSection {
		return &InvalidOpError{Op: "CreateSection", Reason: "new entry must be a section"}
	}

	parentDir, err := f.sectionDirPathForNode(parentEntry, "CreateSection")
	if err != nil {
		return err
	}

	// Ensure parent directory exists (idempotent)
	if err := treeOSMkdirAll(parentDir, 0o755); err != nil {
		return fmt.Errorf("could not ensure parent directory exists: %w", err)
	}

	// Destination base paths
	destBase := filepath.Join(parentDir, newEntry.Slug.FilesystemPath())
	if err := f.requirePathInRoot("CreateSection", destBase); err != nil {
		return err
	}
	destDir := destBase

	if fileExists(destDir) {
		return &PageAlreadyExistsError{Path: destBase}
	}

	// Create the folder for the section and materialize its metadata container.
	if err := treeOSMkdirAll(destDir, 0o755); err != nil {
		return fmt.Errorf("could not create section folder: %w", err)
	}
	f.setWorkspaceSourcePathForPhysicalPath(newEntry, destDir, GenerateRoutePathFromPageNode(newEntry), NodeKindSection)

	if _, err := f.ensureSectionIndex(newEntry); err != nil {
		return err
	}

	return nil
}

// UpsertContent updates the content of a page file on disk, treating the
// incoming content as plain body text. Any metadata-like blocks the caller
// passes are stored verbatim in the body and are never extracted into the
// system-managed metadata. Use this for all UI-originated writes.
// It creates the file if it does not exist, using index.md for sections with
// no active content file.
func (f *NodeStore) UpsertContent(entry *PageNode, content string) error {
	if entry == nil {
		return &InvalidOpError{Op: "UpsertContent", Reason: "an entry is required"}
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

	mdFile.SetContent(content)
	f.syncManagedMetadata(mdFile, entry)
	if err := treeMarkdownWriteToFile(mdFile); err != nil {
		return fmt.Errorf("%w: %w", ErrWriteMarkdownFile, err)
	}

	return nil
}
