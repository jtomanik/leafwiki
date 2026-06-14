package markdown

import (
	"errors"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/perber/wiki/internal/core/shared"
)

// MarkdownFile is the path-aware abstraction for markdown files that may need
// title extraction, metadata updates, and writes back to disk.
type MarkdownFile struct {
	path              string
	doc               PageDocument
	requiresWriteback bool
}

// LoadMarkdownFile reads a markdown file from disk and returns a MarkdownFile.
// Use this when file path semantics matter, for example title fallback from the
// filename or later write-back to the same path.
func LoadMarkdownFile(filePath string) (*MarkdownFile, error) {
	if !strings.EqualFold(filepath.Ext(filePath), ".md") {
		return nil, errors.New("file is not a markdown file")
	}

	raw, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	return NewMarkdownFileFromRaw(filePath, string(raw))
}

// NewMarkdownFileFromRaw builds a MarkdownFile from already loaded content.
// Use this when the caller already has the raw markdown string and wants the
// MarkdownFile behavior without a second filesystem read.
func NewMarkdownFileFromRaw(filePath string, raw string) (*MarkdownFile, error) {
	doc, result, err := ParsePageDocument(raw)
	if err != nil {
		return nil, err
	}

	return &MarkdownFile{
		path:              filePath,
		doc:               doc,
		requiresWriteback: result.RequiresWriteback,
	}, nil
}

// NewMarkdownFile constructs a MarkdownFile from explicit content and parsed
// legacy frontmatter metadata, typically for new files or compatibility callers.
func NewMarkdownFile(filePath string, content string, fm Frontmatter) *MarkdownFile {
	return &MarkdownFile{
		path: filePath,
		doc: PageDocument{
			Body:     content,
			Metadata: frontmatterToPageMetadata(fm),
		},
	}
}

// WriteToFile serializes canonical metadata and body and writes them back atomically
// to the MarkdownFile path.
func (mf *MarkdownFile) WriteToFile() error {
	rendered, err := RenderPageDocument(mf.doc)
	if err != nil {
		return err
	}

	mode := os.FileMode(0o644)
	if st, err := os.Stat(mf.path); err == nil {
		mode = st.Mode()
	}

	if err := shared.WriteFileAtomic(mf.path, []byte(rendered), mode); err != nil {
		return err
	}
	mf.requiresWriteback = false
	return nil
}

// GetTitle resolves the effective title from managed page metadata first, then
// from the first markdown heading, and finally from the file name.
func (mf *MarkdownFile) GetTitle() (string, error) {
	if mf.doc.Metadata.Page.Title != "" {
		return strings.TrimSpace(mf.doc.Metadata.Page.Title), nil
	}

	title, err := mf.extractTitleFromFirstHeading()
	if err == nil && title != "" {
		return title, nil
	}

	base := path.Base(strings.ReplaceAll(mf.path, `\`, "/"))
	name := strings.TrimSuffix(base, path.Ext(base))
	return name, nil
}

func (mf *MarkdownFile) extractTitleFromFirstHeading() (string, error) {
	lines := strings.Split(mf.doc.Body, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "# ")), nil
		}
	}
	return "", errors.New("no heading found")
}

func (mf *MarkdownFile) GetContent() string {
	return mf.doc.Body
}

func (mf *MarkdownFile) SetContent(content string) {
	mf.doc.Body = content
}

func (mf *MarkdownFile) SetRawContentPreservingManagedMetadata(raw string) error {
	doc, result, err := ParsePageDocument(raw)
	if err != nil {
		return err
	}

	if doc.Metadata.Version == 0 {
		mf.doc.Body = raw
		return nil
	}

	mf.doc.Body = doc.Body
	mf.mergeIncomingMetadata(doc.Metadata)
	if result.RequiresWriteback {
		mf.requiresWriteback = true
	}
	return nil
}

func (mf *MarkdownFile) SetRawContentReplacingManagedMetadata(raw string) error {
	doc, result, err := ParsePageDocument(raw)
	if err != nil {
		return err
	}

	if doc.Metadata.Version == 0 {
		mf.doc = PageDocument{
			Body: raw,
			Metadata: PageMetadata{
				Version: 1,
			},
		}
		mf.requiresWriteback = false
		return nil
	}

	mf.doc = doc
	mf.requiresWriteback = result.RequiresWriteback
	return nil
}

func (mf *MarkdownFile) GetPath() string {
	return mf.path
}

func (mf *MarkdownFile) RequiresWriteback() bool {
	return mf.requiresWriteback
}

// GetFrontmatter returns a legacy compatibility view of canonical metadata.
func (mf *MarkdownFile) GetFrontmatter() Frontmatter {
	return pageMetadataToFrontmatter(mf.doc.Metadata)
}

func (mf *MarkdownFile) GetMetadata() PageMetadata {
	return clonePageMetadata(mf.doc.Metadata)
}

func (mf *MarkdownFile) setMetadataID(id string) {
	meta := mf.ensureMetadata()
	meta.Page.ID = strings.TrimSpace(id)
}

func (mf *MarkdownFile) setMetadataTitle(title string) {
	meta := mf.ensureMetadata()
	meta.Page.Title = strings.TrimSpace(title)
}

func (mf *MarkdownFile) SetLeafWikiMetadataIdentity(id string, title string) {
	mf.setMetadataID(id)
	mf.setMetadataTitle(title)
}

func (mf *MarkdownFile) SetLeafWikiMetadata(createdAt string, updatedAt string, creatorID string, lastAuthorID string) {
	meta := mf.ensureMetadata()
	meta.Page.CreatedAt = strings.TrimSpace(createdAt)
	meta.Page.UpdatedAt = strings.TrimSpace(updatedAt)
	meta.Page.CreatorID = strings.TrimSpace(creatorID)
	meta.Page.LastAuthorID = strings.TrimSpace(lastAuthorID)
}

func (mf *MarkdownFile) ensureMetadata() *PageMetadata {
	if mf.doc.Metadata.Version == 0 {
		mf.doc.Metadata.Version = 1
	}
	if mf.doc.Metadata.Fields == nil {
		mf.doc.Metadata.Fields = map[string]interface{}{}
	}
	if mf.doc.Metadata.Extra == nil {
		mf.doc.Metadata.Extra = map[string]interface{}{}
	}
	return &mf.doc.Metadata
}

func (mf *MarkdownFile) mergeIncomingMetadata(incoming PageMetadata) {
	meta := mf.ensureMetadata()
	meta.Tags = append([]string{}, incoming.Tags...)

	fields := map[string]interface{}{}
	for key, value := range meta.Fields {
		if _, incomingHasField := incoming.Fields[key]; incomingHasField {
			continue
		}
		if _, isString := value.(string); !isString {
			fields[key] = value
		}
	}
	for key, value := range incoming.Fields {
		fields[key] = value
	}
	if len(fields) == 0 {
		fields = nil
	}
	meta.Fields = fields

	extra := cloneMetadataMap(meta.Extra)
	for key, value := range incoming.Extra {
		extra[key] = value
	}
	if len(extra) == 0 {
		extra = nil
	}
	meta.Extra = extra
}

func cloneMetadataMap(values map[string]interface{}) map[string]interface{} {
	if len(values) == 0 {
		return map[string]interface{}{}
	}
	cloned := make(map[string]interface{}, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func clonePageMetadata(meta PageMetadata) PageMetadata {
	cloned := meta
	cloned.Tags = append([]string{}, meta.Tags...)
	cloned.Fields = cloneMetadataMap(meta.Fields)
	if len(cloned.Fields) == 0 {
		cloned.Fields = nil
	}
	cloned.Extra = cloneMetadataMap(meta.Extra)
	if len(cloned.Extra) == 0 {
		cloned.Extra = nil
	}
	return cloned
}
