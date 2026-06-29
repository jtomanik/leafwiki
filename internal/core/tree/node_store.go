package tree

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/perber/wiki/internal/core/markdown"
)

func fileExists(p string) bool {
	_, err := treeOSStat(p)
	return err == nil
}

func (f *NodeStore) sectionIndexPathInDir(sectionDir string) (string, bool, error) {
	defaultPath := filepath.Join(sectionDir, "index.md")
	entries, err := treeOSReadDir(sectionDir)
	if err != nil {
		if os.IsNotExist(err) {
			return defaultPath, false, nil
		}
		return defaultPath, false, err
	}

	var existingPath string
	var readmePath string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if name == "README.md" {
			readmePath = filepath.Join(sectionDir, name)
			continue
		}
		ext := filepath.Ext(name)
		base := strings.TrimSuffix(name, ext)
		if !strings.EqualFold(base, "index") || !strings.EqualFold(ext, ".md") {
			continue
		}
		candidate := filepath.Join(sectionDir, name)
		if name == "index.md" {
			return candidate, true, nil
		}
		if existingPath == "" {
			existingPath = candidate
		}
	}
	if existingPath != "" {
		return existingPath, true, nil
	}
	if readmePath != "" {
		return readmePath, true, nil
	}
	return defaultPath, false, nil
}

func (f *NodeStore) isSectionContentFileInDir(sectionDir string, filePath string) bool {
	indexPath, hasIndex, err := f.sectionIndexPathInDir(sectionDir)
	if err != nil || !hasIndex {
		return false
	}
	return filepath.Clean(indexPath) == filepath.Clean(filePath)
}

func ensureUniqueReconstructedID(seenIDs map[PageID]string, id PageID, path string) error {
	trimmedID := PageIDFromString(id.MetadataValue())
	if trimmedID == "" {
		return fmt.Errorf("reconstruct tree from fs: %w at %s", ErrEmptyLeafwikiID, path)
	}
	if existingPath, exists := seenIDs[trimmedID]; exists {
		return fmt.Errorf("%w %q in %s and %s", ErrDuplicateLeafwikiID, trimmedID, existingPath, path)
	}
	seenIDs[trimmedID] = path
	return nil
}

type reconstructedSlugKey struct {
	kind NodeKind
	slug Slug
}

func ensureUniqueReconstructedSlug(seenSlugs map[reconstructedSlugKey]string, slug Slug, kind NodeKind, path string) error {
	trimmedSlug := strings.TrimSpace(slug.FilesystemPath())
	if trimmedSlug == "" {
		return fmt.Errorf("reconstruct tree from fs: %w at %s", ErrSlugEmpty, path)
	}
	key := reconstructedSlugKey{kind: kind, slug: SlugFromString(strings.ToLower(trimmedSlug))}
	if existingPath, exists := seenSlugs[key]; exists {
		parentDir := filepath.Base(filepath.Dir(path))
		return fmt.Errorf(
			"%w: duplicate %s slug %q in %s/. Conflicting paths: %s and %s",
			ErrDuplicateReconstructedSlug, kind, slug, parentDir, existingPath, path,
		)
	}
	seenSlugs[key] = path
	return nil
}

type ResolvedNode struct {
	Kind       NodeKind
	DirPath    string
	FilePath   string
	HasContent bool
}

type NodeStore struct {
	dataDir string
	rootDir string
	log     *slog.Logger
	slugger *SlugService
}

const reconstructSystemUserID = "system"
const orderFilename = ".order.json"

type childOrderFile struct {
	OrderedIDs []PageID `json:"ordered_ids"`
}

func NewNodeStore(dataDir string) *NodeStore {
	return NewNodeStoreWithOptions(NodeStoreOptions{DataDir: dataDir})
}

type NodeStoreOptions struct {
	DataDir string
	RootDir string
}

func NewNodeStoreWithOptions(options NodeStoreOptions) *NodeStore {
	dataDir := cleanTreePath(options.DataDir)
	rootDir := cleanTreePath(options.RootDir)
	if rootDir == "" {
		rootDir = filepath.Join(dataDir, "root")
	}
	return &NodeStore{
		dataDir: dataDir,
		rootDir: rootDir,
		log:     slog.Default().With("component", "NodeStore"),
		slugger: NewSlugService(),
	}
}

func validateNodeSlug(op string, slug Slug) error {
	if err := NewSlugService().IsValidSlug(slug.FilesystemPath()); err != nil {
		return &InvalidOpError{Op: op, Reason: fmt.Sprintf("invalid slug %q: %v", slug, err)}
	}
	return nil
}

// writeReconstructedMetadata writes the full managed metadata back to disk
// while preserving the file's modification time. Called during reconstruct for
// files that are missing any managed metadata field.
func (f *NodeStore) writeReconstructedMetadata(mdFile *markdown.MarkdownFile, entry *PageNode) error {
	var originalModTime time.Time
	if info, err := treeOSStat(mdFile.GetPath()); err == nil {
		originalModTime = info.ModTime()
	}

	f.syncManagedMetadata(mdFile, entry)
	if err := treeMarkdownWriteToFile(mdFile); err != nil {
		return fmt.Errorf("write reconstructed metadata for %s: %w", mdFile.GetPath(), err)
	}

	if !originalModTime.IsZero() {
		if err := treeOSChtimes(mdFile.GetPath(), originalModTime, originalModTime); err != nil {
			f.log.Warn("could not restore file mtime after writing metadata", "path", mdFile.GetPath(), "error", err)
		}
	}
	return nil
}

func formatMetadataTime(ts time.Time) string {
	if ts.IsZero() {
		return ""
	}
	return ts.UTC().Format(time.RFC3339Nano)
}

func (f *NodeStore) syncManagedMetadata(mdFile *markdown.MarkdownFile, entry *PageNode) {
	mdFile.SetLeafWikiMetadataIdentity(entry.ID.MetadataValue(), strings.TrimSpace(entry.Title))
	mdFile.SetLeafWikiMetadata(
		formatMetadataTime(entry.Metadata.CreatedAt),
		formatMetadataTime(entry.Metadata.UpdatedAt),
		entry.Metadata.CreatorID.MetadataValue(),
		entry.Metadata.LastAuthorID.MetadataValue(),
	)
}

func (f *NodeStore) ensureSectionIndex(entry *PageNode) (string, error) {
	if entry == nil {
		return "", &InvalidOpError{Op: "ensureSectionIndex", Reason: "an entry is required"}
	}
	if entry.Kind != NodeKindSection {
		return "", &InvalidOpError{Op: "ensureSectionIndex", Reason: "entry must be a section"}
	}

	filePath, err := f.contentPathForNodeWrite(entry)
	if err != nil {
		return "", err
	}

	mdFile := markdown.NewMarkdownFile(filePath, "", markdown.Frontmatter{})
	if fileExists(filePath) {
		mdFile, err = treeLoadMarkdownFile(filePath)
		if err != nil {
			return "", fmt.Errorf("%w: %w", ErrLoadMarkdownFile, err)
		}
	}

	f.syncManagedMetadata(mdFile, entry)
	if err := treeMarkdownWriteToFile(mdFile); err != nil {
		return "", fmt.Errorf("%w: %w", ErrWriteMarkdownFile, err)
	}

	return filePath, nil
}

func (f *NodeStore) ensureSectionIndexAtPath(entry *PageNode, filePath string) (string, error) {
	if entry == nil {
		return "", &InvalidOpError{Op: "ensureSectionIndexAtPath", Reason: "an entry is required"}
	}
	if entry.Kind != NodeKindSection {
		return "", &InvalidOpError{Op: "ensureSectionIndexAtPath", Reason: "entry must be a section"}
	}
	if err := f.requirePathInRoot("ensureSectionIndexAtPath", filePath); err != nil {
		return "", err
	}
	if err := treeOSMkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		return "", fmt.Errorf("%w: %w", ErrEnsureFolder, err)
	}

	mdFile := markdown.NewMarkdownFile(filePath, "", markdown.Frontmatter{})
	if fileExists(filePath) {
		loaded, err := treeLoadMarkdownFile(filePath)
		if err != nil {
			return "", fmt.Errorf("%w: %w", ErrLoadMarkdownFile, err)
		}
		mdFile = loaded
	}

	f.syncManagedMetadata(mdFile, entry)
	if err := treeMarkdownWriteToFile(mdFile); err != nil {
		return "", fmt.Errorf("%w: %w", ErrWriteMarkdownFile, err)
	}

	return filePath, nil
}

func fallbackMetadataString(value string) string {
	if strings.TrimSpace(value) == "" {
		return reconstructSystemUserID
	}
	return strings.TrimSpace(value)
}

func (f *NodeStore) metadataFallbackTime(filePath string, fallback time.Time) time.Time {
	info, err := treeOSStat(filePath)
	if err != nil {
		if !os.IsNotExist(err) {
			f.log.Warn("could not stat path for reconstruct metadata fallback, using runtime fallback", "path", filePath, "fallback", fallback.UTC().Format(time.RFC3339), "error", err)
		}
		return fallback.UTC()
	}
	return info.ModTime().UTC()
}

func (f *NodeStore) parseMetadataTime(value string, fallback time.Time, field string, filePath string) time.Time {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fallback.UTC()
	}
	parsed, err := time.Parse(time.RFC3339Nano, trimmed)
	if err != nil {
		parsed, err = time.Parse(time.RFC3339, trimmed)
	}
	if err != nil {
		f.log.Warn("invalid metadata timestamp, using fallback", "path", filePath, "field", field, "value", trimmed, "fallback", fallback.UTC().Format(time.RFC3339), "error", err)
		return fallback.UTC()
	}
	return parsed.UTC()
}

func (f *NodeStore) metadataFromPageMetadata(meta markdown.PageMetadata, fallbackNow time.Time, filePath string) PageMetadata {
	fallbackTime := f.metadataFallbackTime(filePath, fallbackNow)
	return PageMetadata{
		CreatedAt:    f.parseMetadataTime(meta.Page.CreatedAt, fallbackTime, "page.created_at", filePath),
		UpdatedAt:    f.parseMetadataTime(meta.Page.UpdatedAt, fallbackTime, "page.updated_at", filePath),
		CreatorID:    UserIDFromString(fallbackMetadataString(meta.Page.CreatorID)),
		LastAuthorID: UserIDFromString(fallbackMetadataString(meta.Page.LastAuthorID)),
	}
}

func (f *NodeStore) LoadTree(snapshotFile string) (*PageNode, error) {
	return loadLegacyTreeSnapshot(f.dataDir, snapshotFile, f.log)
}

func loadLegacyTreeSnapshot(dataDir string, snapshotFile string, _ *slog.Logger) (*PageNode, error) {
	fullPath := filepath.Join(dataDir, snapshotFile)

	// check if file exists
	if _, err := treeOSStat(fullPath); os.IsNotExist(err) {
		return &PageNode{
			ID:       "root",
			Slug:     "root",
			Title:    "root",
			Parent:   nil,
			Position: 0,
			Children: []*PageNode{},
			Kind:     NodeKindSection,
		}, nil
	}

	data, err := treeOSReadFile(fullPath)
	if err != nil {
		return nil, fmt.Errorf("%w %s: %w", ErrReadTreeFile, fullPath, err)
	}

	tree := &PageNode{}
	if err := treeJSONUnmarshal(data, tree); err != nil {
		return nil, fmt.Errorf("%w %s: %w", ErrUnmarshalTreeData, fullPath, err)
	}

	if tree.ID == "root" && tree.Kind == "" {
		tree.Kind = NodeKindSection
	}

	// assigns parent to children
	assignParentToChildren(tree)

	return tree, nil
}

func (f *NodeStore) ReconstructTreeFromFS() (*PageNode, error) {
	reconstructNow := time.Now().UTC()
	root := &PageNode{
		ID:       "root",
		Slug:     "root",
		Title:    "root",
		Parent:   nil,
		Position: 0,
		Children: []*PageNode{},
		Kind:     NodeKindSection,
		Metadata: f.metadataFromPageMetadata(markdown.PageMetadata{}, reconstructNow, f.rootDir),
	}
	root.WorkspaceSourcePath = ""
	seenIDs := map[PageID]string{RootPageID: f.rootDir}

	info, err := treeOSStat(f.rootDir)
	if err != nil {
		if os.IsNotExist(err) {
			// No on-disk content yet; return an empty root tree.
			return root, nil
		}
		return nil, fmt.Errorf("stat root dir %s: %w", f.rootDir, err)
	}

	if !info.IsDir() {
		return nil, fmt.Errorf("root path %s %w", f.rootDir, ErrRootPathNotDirectory)
	}

	if err := f.applyRootSectionContent(root, reconstructNow); err != nil {
		return nil, fmt.Errorf("reconstruct root content from fs: %w", err)
	}
	if err := f.reconstructTreeRecursive(f.rootDir, root, reconstructNow, seenIDs); err != nil {
		return nil, fmt.Errorf("reconstruct tree from fs: %w", err)
	}

	return root, nil
}

func (f *NodeStore) applyRootSectionContent(root *PageNode, reconstructNow time.Time) error {
	indexPath, hasIndex, err := f.sectionIndexPathInDir(f.rootDir)
	if err != nil {
		return err
	}
	if !hasIndex {
		return nil
	}
	mdFile, err := treeLoadMarkdownFile(indexPath)
	if err != nil {
		return fmt.Errorf("load root section index %s: %w", indexPath, err)
	}
	meta := mdFile.GetMetadata()
	root.Metadata = f.metadataFromPageMetadata(meta, reconstructNow, indexPath)
	root.Title, _ = mdFile.GetTitle()
	if mdFile.RequiresWriteback() || PageIDFromString(strings.TrimSpace(meta.Page.ID)) != root.ID || strings.TrimSpace(meta.Page.UpdatedAt) == "" || strings.TrimSpace(meta.Page.CreatedAt) == "" {
		if err := f.writeReconstructedMetadata(mdFile, root); err != nil {
			return err
		}
	}
	return nil
}

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
