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

type reconstructTreeContext struct {
	currentDir     reconstructTreeDir
	parent         *PageNode
	reconstructNow time.Time
	seenIDs        map[PageID]string
	seenSlugs      map[reconstructedSlugKey]string
}

type reconstructTreeDir string

func (dir reconstructTreeDir) String() string {
	return string(dir)
}

type reconstructionDefaults struct {
	title    string
	id       string
	metadata PageMetadata
}

func (f *NodeStore) reconstructTreeRecursive(currentPath string, parent *PageNode, reconstructNow time.Time, seenIDs map[PageID]string) error {
	entries, err := treeOSReadDir(currentPath)
	if err != nil {
		return fmt.Errorf("%w %s: %w", ErrReadDirectory, currentPath, err)
	}
	ctx := reconstructTreeContext{
		currentDir:     reconstructTreeDir(currentPath),
		parent:         parent,
		reconstructNow: reconstructNow,
		seenIDs:        seenIDs,
		seenSlugs:      map[reconstructedSlugKey]string{},
	}

	sortReconstructionEntries(entries)

	for _, entry := range entries {
		if err := f.reconstructTreeEntry(ctx, entry); err != nil {
			return err
		}
	}

	f.applyChildOrder(parent, currentPath)

	return nil
}

func sortReconstructionEntries(entries []os.DirEntry) {
	sort.SliceStable(entries, func(i, j int) bool {
		li := strings.ToLower(entries[i].Name())
		lj := strings.ToLower(entries[j].Name())
		if li == lj {
			return entries[i].Name() < entries[j].Name()
		}
		return li < lj
	})
}

func (f *NodeStore) reconstructTreeEntry(ctx reconstructTreeContext, entry os.DirEntry) error {
	name := entry.Name()
	if strings.HasPrefix(name, ".") {
		return nil
	}

	entryPath := filepath.Join(ctx.currentDir.String(), name)
	mappedRoute, ok, err := f.mapReconstructionEntryRoute(entryPath, entry)
	if err != nil || !ok {
		return err
	}

	defaults, err := f.reconstructionDefaults(name, entryPath, ctx.reconstructNow)
	if err != nil {
		return err
	}
	if entry.IsDir() {
		return f.reconstructDirectoryEntry(ctx, entryPath, name, mappedRoute, defaults)
	}
	return f.reconstructFileEntry(ctx, entryPath, name, mappedRoute, defaults)
}

func (f *NodeStore) mapReconstructionEntryRoute(entryPath string, entry os.DirEntry) (WorkspaceMarkdownRoute, bool, error) {
	relPath, err := treeFilepathRel(f.rootDir, entryPath)
	if err != nil {
		return WorkspaceMarkdownRoute{}, false, fmt.Errorf("resolve workspace relative path for %s: %w", entryPath, err)
	}
	mappedRoute, err := treeMapWorkspaceMarkdownRoute(f.rootDir, relPath, entry.IsDir())
	if err != nil {
		f.log.Error("skipping workspace path with invalid route", "path", relPath, "error", err)
		return WorkspaceMarkdownRoute{}, false, nil
	}
	return mappedRoute, !mappedRoute.Skip, nil
}

func (f *NodeStore) reconstructionDefaults(name string, entryPath string, reconstructNow time.Time) (reconstructionDefaults, error) {
	id, err := treeGenerateUniqueID()
	if err != nil {
		return reconstructionDefaults{}, fmt.Errorf("generate unique ID: %w", err)
	}
	return reconstructionDefaults{
		title:    name,
		id:       id,
		metadata: f.metadataFromPageMetadata(markdown.PageMetadata{}, reconstructNow, entryPath),
	}, nil
}

func (f *NodeStore) reconstructDirectoryEntry(ctx reconstructTreeContext, sectionDir string, name string, route WorkspaceMarkdownRoute, defaults reconstructionDefaults) error {
	slug := workspaceRouteLeafSlug(route.RoutePath)
	if slug == "" {
		f.log.Error("skipping directory with empty route slug", "directory", name)
		return nil
	}

	indexPath, hasIndex, err := f.sectionIndexPathInDir(sectionDir)
	if err != nil {
		return fmt.Errorf("resolve section index for %s: %w", sectionDir, err)
	}

	defaults, sectionMdFile, needsWriteback, err := f.applySectionIndexMetadata(defaults, indexPath, hasIndex, ctx.reconstructNow)
	if err != nil {
		return err
	}

	child := newReconstructedSectionNode(ctx.parent, defaults, slug, route)
	if err := f.attachReconstructedChild(ctx, child, sectionDir, indexPath); err != nil {
		return err
	}
	if err := f.writeReconstructedMetadataIfNeeded(sectionMdFile, child, needsWriteback); err != nil {
		return err
	}
	if err := f.materializeMissingSectionIndex(child, indexPath, hasIndex); err != nil {
		return err
	}
	return f.reconstructTreeRecursive(sectionDir, child, ctx.reconstructNow, ctx.seenIDs)
}

func (f *NodeStore) applySectionIndexMetadata(defaults reconstructionDefaults, indexPath string, hasIndex bool, reconstructNow time.Time) (reconstructionDefaults, *markdown.MarkdownFile, bool, error) {
	if !hasIndex {
		return defaults, nil, false, nil
	}
	mdFile, err := treeLoadMarkdownFile(indexPath)
	if err != nil {
		return reconstructionDefaults{}, nil, false, fmt.Errorf("load section index %s: %w", indexPath, err)
	}
	meta := mdFile.GetMetadata()
	defaults.metadata = f.metadataFromPageMetadata(meta, reconstructNow, indexPath)
	defaults.title, _ = mdFile.GetTitle()
	if strings.TrimSpace(meta.Page.ID) != "" {
		defaults.id = strings.TrimSpace(meta.Page.ID)
	}
	if reconstructedMetadataNeedsWriteback(mdFile, meta) {
		return defaults, mdFile, true, nil
	}
	return defaults, nil, false, nil
}

func reconstructedMetadataNeedsWriteback(mdFile *markdown.MarkdownFile, meta markdown.PageMetadata) bool {
	return mdFile.RequiresWriteback() ||
		strings.TrimSpace(meta.Page.ID) == "" ||
		strings.TrimSpace(meta.Page.UpdatedAt) == "" ||
		strings.TrimSpace(meta.Page.CreatedAt) == ""
}

func newReconstructedSectionNode(parent *PageNode, defaults reconstructionDefaults, slug Slug, route WorkspaceMarkdownRoute) *PageNode {
	return &PageNode{
		ID:                  PageIDFromString(defaults.id),
		Slug:                slug,
		Title:               defaults.title,
		Parent:              parent,
		Position:            len(parent.Children),
		Children:            []*PageNode{},
		Kind:                NodeKindSection,
		WorkspaceSourcePath: nonDefaultWorkspaceSourcePath(route),
		Metadata:            defaults.metadata,
	}
}

func (f *NodeStore) reconstructFileEntry(ctx reconstructTreeContext, filePath string, name string, route WorkspaceMarkdownRoute, defaults reconstructionDefaults) error {
	if !isReconstructableMarkdownFile(name, route) || f.isSectionContentFileInDir(ctx.currentDir.String(), filePath) {
		return nil
	}
	slug := workspaceRouteLeafSlug(route.RoutePath)
	if slug == "" {
		f.log.Error("skipping markdown file with empty route slug", "file", name)
		return nil
	}

	mdFile, child, needsWriteback, err := f.newReconstructedPageNode(ctx.parent, filePath, slug, route, defaults, ctx.reconstructNow)
	if err != nil {
		return err
	}
	if err := f.attachReconstructedChild(ctx, child, filePath, filePath); err != nil {
		return err
	}
	return f.writeReconstructedMetadataIfNeeded(mdFile, child, needsWriteback)
}

func isReconstructableMarkdownFile(name string, route WorkspaceMarkdownRoute) bool {
	return strings.EqualFold(filepath.Ext(name), ".md") && route.Kind != NodeKindSection
}

func (f *NodeStore) newReconstructedPageNode(parent *PageNode, filePath string, slug Slug, route WorkspaceMarkdownRoute, defaults reconstructionDefaults, reconstructNow time.Time) (*markdown.MarkdownFile, *PageNode, bool, error) {
	mdFile, err := treeLoadMarkdownFile(filePath)
	if err != nil {
		return nil, nil, false, fmt.Errorf("load markdown file %s: %w", filePath, err)
	}
	meta := mdFile.GetMetadata()
	defaults.metadata = f.metadataFromPageMetadata(meta, reconstructNow, filePath)
	defaults.title, _ = mdFile.GetTitle()
	if strings.TrimSpace(meta.Page.ID) != "" {
		defaults.id = strings.TrimSpace(meta.Page.ID)
	}
	child := &PageNode{
		ID:                  PageIDFromString(defaults.id),
		Slug:                slug,
		Title:               defaults.title,
		Parent:              parent,
		Position:            len(parent.Children),
		Children:            nil,
		Kind:                NodeKindPage,
		WorkspaceSourcePath: nonDefaultWorkspaceSourcePath(route),
		Metadata:            defaults.metadata,
	}
	return mdFile, child, reconstructedMetadataNeedsWriteback(mdFile, meta), nil
}

func (f *NodeStore) attachReconstructedChild(ctx reconstructTreeContext, child *PageNode, slugPath string, idPath string) error {
	if err := ensureUniqueReconstructedSlug(ctx.seenSlugs, child.Slug, child.Kind, slugPath); err != nil {
		return err
	}
	if err := ensureUniqueReconstructedID(ctx.seenIDs, child.ID, idPath); err != nil {
		return err
	}
	ctx.parent.Children = append(ctx.parent.Children, child)
	return nil
}

func (f *NodeStore) writeReconstructedMetadataIfNeeded(mdFile *markdown.MarkdownFile, child *PageNode, needsWriteback bool) error {
	if !needsWriteback {
		return nil
	}
	return f.writeReconstructedMetadata(mdFile, child)
}

func (f *NodeStore) materializeMissingSectionIndex(child *PageNode, indexPath string, hasIndex bool) error {
	if hasIndex {
		return nil
	}
	if _, err := f.ensureSectionIndexAtPath(child, indexPath); err != nil {
		return fmt.Errorf("materialize missing section index for %s: %w", indexPath, err)
	}
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
