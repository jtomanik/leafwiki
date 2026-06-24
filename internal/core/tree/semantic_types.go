package tree

import (
	"database/sql/driver"
	"fmt"
	pathpkg "path"
	"path/filepath"
	"strings"
	"time"

	"github.com/perber/wiki/internal/core/identity"
)

type PageID string

const RootPageID PageID = "root"

func (id PageID) String() string {
	return string(id)
}

func (id PageID) HashPayload() string {
	return string(id)
}

func (id PageID) MetadataValue() string {
	return strings.TrimSpace(string(id))
}

func (id PageID) Value() (driver.Value, error) {
	return string(id), nil
}

func (id *PageID) Scan(value any) error {
	switch typed := value.(type) {
	case nil:
		*id = ""
		return nil
	case string:
		*id = PageID(typed)
		return nil
	case []byte:
		*id = PageID(string(typed))
		return nil
	default:
		return fmt.Errorf("cannot scan %T into PageID", value)
	}
}

func NewPageIDUnchecked[T ~string](raw T) PageID {
	return PageID(raw)
}

func PageIDFromString[T ~string](raw T) PageID {
	return NewPageIDUnchecked(raw)
}

type UserID = identity.UserID

func NewUserIDUnchecked(raw string) UserID {
	return identity.NewUserIDUnchecked(raw)
}

func UserIDFromString[T ~string](raw T) UserID {
	return identity.NewUserIDUnchecked(string(raw))
}

type RevisionID = identity.RevisionID

func NewRevisionIDUnchecked(raw string) RevisionID {
	return identity.NewRevisionIDUnchecked(raw)
}

func RevisionIDFromString[T ~string](raw T) RevisionID {
	return identity.NewRevisionIDUnchecked(string(raw))
}

type CommitHash = identity.CommitHash

type PageVersion string

func (version PageVersion) String() string {
	return string(version)
}

const pageVersionUnchecked PageVersion = PageVersion(versionUnchecked)

func (version PageVersion) IsUnchecked() bool {
	return version == pageVersionUnchecked
}

func NewPageVersionUnchecked[T ~string](raw T) PageVersion {
	if raw == versionUnchecked {
		return ""
	}
	return PageVersion(raw)
}

func PageVersionFromString[T ~string](raw T) PageVersion {
	return NewPageVersionUnchecked(raw)
}

func NewPageVersionFromTime(value time.Time) PageVersion {
	if value.IsZero() {
		return ""
	}
	return PageVersion(value.UTC().Format(time.RFC3339Nano))
}

type RoutePath string

func (path RoutePath) String() string {
	return string(path)
}

func (path RoutePath) Value() (driver.Value, error) {
	return path.WikiPath(), nil
}

func (path *RoutePath) Scan(value any) error {
	switch typed := value.(type) {
	case nil:
		*path = ""
		return nil
	case string:
		*path = RoutePathFromString(typed)
		return nil
	case []byte:
		*path = RoutePathFromString(string(typed))
		return nil
	default:
		return fmt.Errorf("cannot scan %T into RoutePath", value)
	}
}

func (path RoutePath) FilesystemPath() string {
	return string(path)
}

func (path RoutePath) WikiPath() string {
	normalized := path.Clean()
	if normalized.IsRoot() {
		return "/"
	}
	return "/" + string(normalized)
}

type MarkdownPath string

func (path MarkdownPath) String() string {
	return string(path)
}

func CleanMarkdownPath(raw string) MarkdownPath {
	cleaned := pathpkg.Clean(strings.Trim(strings.TrimSpace(filepath.ToSlash(raw)), "/"))
	if cleaned == "." {
		return ""
	}
	return MarkdownPath(cleaned)
}

func NewMarkdownPathUnchecked(raw string) MarkdownPath {
	return MarkdownPath(raw)
}

func MarkdownPathFromString[T ~string](raw T) MarkdownPath {
	return NewMarkdownPathUnchecked(string(raw))
}

func (path MarkdownPath) Clean() MarkdownPath {
	return CleanMarkdownPath(string(path))
}

func (path MarkdownPath) Ext() string {
	return pathpkg.Ext(string(path))
}

func (path MarkdownPath) IsIndexFile() bool {
	base := path
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' {
			base = path[i+1:]
			break
		}
	}
	return equalFoldASCII(base, "index.md")
}

func equalFoldASCII[T ~string](value T, expected string) bool {
	if len(value) != len(expected) {
		return false
	}
	for i := 0; i < len(expected); i++ {
		got := value[i]
		want := expected[i]
		if got >= 'A' && got <= 'Z' {
			got += 'a' - 'A'
		}
		if want >= 'A' && want <= 'Z' {
			want += 'a' - 'A'
		}
		if got != want {
			return false
		}
	}
	return true
}

func (path MarkdownPath) IsMarkdown() bool {
	return strings.EqualFold(path.Ext(), ".md")
}

func (path MarkdownPath) RoutePath() RoutePath {
	routePath := strings.Trim(strings.TrimSpace(filepath.ToSlash(string(path))), "/")
	ext := pathpkg.Ext(routePath)
	if strings.EqualFold(ext, ".md") {
		routePath = strings.TrimSuffix(routePath, ext)
	}
	parts := strings.Split(routePath, "/")
	if len(parts) > 0 && strings.EqualFold(parts[len(parts)-1], "index") {
		parts = parts[:len(parts)-1]
	}
	return RoutePath(strings.Trim(strings.Join(parts, "/"), "/"))
}

func (path MarkdownPath) SourceDir() MarkdownPath {
	dir := pathpkg.Dir(string(path.Clean()))
	if dir == "." {
		return ""
	}
	return MarkdownPath(dir)
}

func (path MarkdownPath) FilesystemPath() string {
	return string(path)
}

func (path RoutePath) Clean() RoutePath {
	return RoutePath(strings.Trim(string(path), "/"))
}

func (path RoutePath) Validate() (RoutePath, error) {
	return ValidateRoutePath(string(path))
}

func (path RoutePath) IsRoot() bool {
	return path.Clean() == ""
}

func (path RoutePath) Segments() []Slug {
	normalized := path.Clean()
	if normalized.IsRoot() {
		return nil
	}
	parts := strings.Split(string(normalized), "/")
	segments := make([]Slug, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			continue
		}
		segments = append(segments, NewSlugUnchecked(part))
	}
	return segments
}

func (path RoutePath) Child(slug Slug) RoutePath {
	normalized := path.Clean()
	if normalized.IsRoot() {
		return RoutePath(slug)
	}
	return RoutePath(string(normalized) + "/" + slug.String())
}

func (path RoutePath) WithLeafSlug(slug Slug) RoutePath {
	normalized := path.Clean()
	if normalized.IsRoot() {
		return RoutePath(slug)
	}
	parent := pathpkg.Dir(string(normalized))
	if parent == "." {
		return RoutePath(slug)
	}
	return RoutePath(parent + "/" + slug.String())
}

func (path RoutePath) MarkdownContentPath(kind NodeKind) MarkdownPath {
	normalized := path.Clean()
	if kind == NodeKindSection {
		if normalized.IsRoot() {
			return MarkdownPath("index.md")
		}
		return MarkdownPath(string(normalized) + "/index.md")
	}
	if normalized.IsRoot() {
		return MarkdownPath("index.md")
	}
	return MarkdownPath(string(normalized) + ".md")
}

func (path RoutePath) MarkdownPagePath() MarkdownPath {
	if path.Clean().IsRoot() {
		return MarkdownPath("index.md")
	}
	return MarkdownPath(string(path.Clean()) + ".md")
}

func (path RoutePath) HrefPath() MarkdownPath {
	return MarkdownPath(path.Clean())
}

func (path RoutePath) LowerKey(kind NodeKind) string {
	return string(kind) + ":" + strings.ToLower(string(path.Clean()))
}

func (path RoutePath) Lower() RoutePath {
	return NewRoutePathUnchecked(strings.ToLower(path.Clean().FilesystemPath()))
}

func (path RoutePath) WorkspaceSourceDirectory() WorkspaceSourcePath {
	return WorkspaceSourcePath(path.Clean())
}

func (path RoutePath) LeafSlug() Slug {
	normalized := path.Clean()
	if normalized == "" {
		return ""
	}
	return NewSlugUnchecked(pathpkg.Base(string(normalized)))
}

func (path RoutePath) WorkspaceSourcePath(kind NodeKind) WorkspaceSourcePath {
	normalized := path.Clean()
	switch kind {
	case NodeKindPage:
		if normalized.IsRoot() {
			return ""
		}
		return WorkspaceSourcePath(string(normalized) + ".md")
	case NodeKindSection:
		return WorkspaceSourcePath(normalized)
	default:
		return ""
	}
}

type WorkspaceSourcePath string

func (path WorkspaceSourcePath) String() string {
	return string(path)
}

func CleanWorkspaceSourcePath(raw string) WorkspaceSourcePath {
	return WorkspaceSourcePath(strings.Trim(strings.TrimSpace(filepath.ToSlash(raw)), "/"))
}

func (path WorkspaceSourcePath) Clean() WorkspaceSourcePath {
	return CleanWorkspaceSourcePath(string(path))
}

func (path WorkspaceSourcePath) Dir() WorkspaceSourcePath {
	dir := pathpkg.Dir(string(path.Clean()))
	if dir == "." {
		return ""
	}
	return WorkspaceSourcePath(dir)
}

func (path WorkspaceSourcePath) FilesystemPath() string {
	return string(path)
}

func NewWorkspaceSourcePathUnchecked(raw string) WorkspaceSourcePath {
	return WorkspaceSourcePath(raw)
}

func WorkspaceSourcePathFromString[T ~string](raw T) WorkspaceSourcePath {
	return NewWorkspaceSourcePathUnchecked(string(raw))
}

type Slug string

func (slug Slug) String() string {
	return string(slug)
}

func (slug Slug) HashPayload() string {
	return string(slug)
}

func (slug Slug) FilesystemPath() string {
	return string(slug)
}

func (slug Slug) RoutePath() RoutePath {
	return RoutePath(slug)
}

func (slug Slug) EqualFold(other Slug) bool {
	return strings.EqualFold(string(slug), string(other))
}

func (slug Slug) SlugKey() SlugKey {
	return SlugKey(strings.ToLower(string(slug)))
}

func (slug Slug) Validate() error {
	return NewSlugService().IsValidSlug(slug.FilesystemPath())
}

func NewSlugUnchecked[T ~string](raw T) Slug {
	return Slug(raw)
}

func SlugFromString[T ~string](raw T) Slug {
	return NewSlugUnchecked(raw)
}

type SlugKey string

type AssetName string

func (name AssetName) String() string {
	return string(name)
}

func (name AssetName) Filename() string {
	return string(name)
}

func (name AssetName) Clean() AssetName {
	return AssetName(strings.TrimSpace(string(name)))
}

func NewAssetNameUnchecked(raw string) AssetName {
	return AssetName(raw)
}

func AssetNameFromString[T ~string](raw T) AssetName {
	return NewAssetNameUnchecked(string(raw))
}

func ParseRoutePath(raw string) (RoutePath, error) {
	return ValidateRoutePath(raw)
}

func NewRoutePathUnchecked(raw string) RoutePath {
	return RoutePath(raw)
}

func RoutePathFromString[T ~string](raw T) RoutePath {
	return NewRoutePathUnchecked(string(raw))
}

func ParseSlug(raw string) (Slug, error) {
	if err := NewSlugService().IsValidSlug(raw); err != nil {
		return "", err
	}
	return Slug(raw), nil
}
