package tree

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
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
