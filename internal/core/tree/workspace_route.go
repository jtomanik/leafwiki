package tree

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

const WorkspaceRouteSkipStaticAssets = "static_assets"

type WorkspaceMarkdownRoute struct {
	SourcePath  WorkspaceSourcePath
	RoutePath   RoutePath
	Kind        NodeKind
	ContentPath MarkdownPath
	Skip        bool
	SkipReason  string
}

type workspaceRouteConflict struct {
	RoutePath  RoutePath
	Kind       NodeKind
	FirstPath  WorkspaceSourcePath
	SecondPath WorkspaceSourcePath
}

type workspaceRouteConflictTracker struct {
	seen map[string]WorkspaceMarkdownRoute
}

func newWorkspaceRouteConflictTracker() *workspaceRouteConflictTracker {
	return &workspaceRouteConflictTracker{seen: map[string]WorkspaceMarkdownRoute{}}
}

func (t *workspaceRouteConflictTracker) Record(route WorkspaceMarkdownRoute) *workspaceRouteConflict {
	if t == nil || route.Skip {
		return nil
	}
	route.RoutePath = route.RoutePath.Clean()
	if route.SourcePath == "" {
		route.SourcePath = route.RoutePath.WorkspaceSourceDirectory()
	}
	key := route.RoutePath.LowerKey(route.Kind)
	if first, exists := t.seen[key]; exists {
		if first.SourcePath == route.SourcePath || sameWorkspaceSectionRouteEntry(first, route) {
			return nil
		}
		return &workspaceRouteConflict{
			RoutePath:  route.RoutePath,
			Kind:       route.Kind,
			FirstPath:  first.SourcePath,
			SecondPath: route.SourcePath,
		}
	}
	t.seen[key] = route
	return nil
}

func MapWorkspaceMarkdownRoute(rootDir string, relPath string, isDir bool) (WorkspaceMarkdownRoute, error) {
	sourcePath := cleanWorkspaceSourcePath(relPath)
	route := WorkspaceMarkdownRoute{SourcePath: WorkspaceSourcePathFromString(sourcePath)}
	if sourcePath == "" {
		route.Kind = NodeKindSection
		return route, nil
	}
	if isTopLevelStaticAssetsDir(sourcePath, isDir) {
		route.Skip = true
		route.SkipReason = WorkspaceRouteSkipStaticAssets
		return route, nil
	}

	slugger := NewSlugService()
	if isDir {
		routePath, err := normalizeWorkspaceRoutePath(slugger, sourcePath)
		if err != nil {
			return WorkspaceMarkdownRoute{}, err
		}
		route.Kind = NodeKindSection
		route.RoutePath = RoutePathFromString(routePath)
		return route, nil
	}

	name := path.Base(sourcePath)
	ext := path.Ext(name)
	if !strings.EqualFold(ext, ".md") {
		route.Skip = true
		route.SkipReason = "non_markdown"
		return route, nil
	}

	dir := path.Dir(sourcePath)
	if dir == "." {
		dir = ""
	}
	dirRoute, err := normalizeWorkspaceRoutePath(slugger, dir)
	if err != nil {
		return WorkspaceMarkdownRoute{}, err
	}

	if strings.EqualFold(name, "index.md") || isActiveWorkspaceReadme(rootDir, dir, name) {
		route.Kind = NodeKindSection
		route.RoutePath = RoutePathFromString(dirRoute)
		route.ContentPath = MarkdownPathFromString(sourcePath)
		return route, nil
	}

	base := strings.TrimSuffix(name, ext)
	baseSlug, err := normalizeWorkspaceRouteSegment(slugger, base)
	if err != nil {
		return WorkspaceMarkdownRoute{}, err
	}
	route.Kind = NodeKindPage
	route.RoutePath = RoutePathFromString(joinWorkspaceRoutePath(dirRoute, baseSlug))
	return route, nil
}

func cleanWorkspaceSourcePath(relPath string) string {
	return strings.Trim(strings.TrimSpace(filepath.ToSlash(relPath)), "/")
}

func isTopLevelStaticAssetsDir(sourcePath string, isDir bool) bool {
	if !isDir {
		return false
	}
	return !strings.Contains(sourcePath, "/") && strings.EqualFold(sourcePath, "assets")
}

func normalizeWorkspaceRoutePath(slugger *SlugService, sourcePath string) (string, error) {
	sourcePath = strings.Trim(sourcePath, "/")
	if sourcePath == "" || sourcePath == "." {
		return "", nil
	}
	segments := strings.Split(sourcePath, "/")
	normalized := make([]string, 0, len(segments))
	for _, segment := range segments {
		if segment == "" {
			continue
		}
		slug, err := normalizeWorkspaceRouteSegment(slugger, segment)
		if err != nil {
			return "", err
		}
		normalized = append(normalized, slug)
	}
	return strings.Join(normalized, "/"), nil
}

func normalizeWorkspaceRouteSegment(slugger *SlugService, segment string) (string, error) {
	if err := slugger.IsValidSlug(segment); err == nil {
		return segment, nil
	}
	safe := slugger.GenerateValidSlug(segment)
	if safe == "" {
		return "", fmt.Errorf("segment %q is not a valid slug: slug must not be empty", segment)
	}
	return safe, nil
}

func isActiveWorkspaceReadme(rootDir string, dir string, name string) bool {
	if name != "README.md" {
		return false
	}
	return !workspaceDirHasIndexFile(filepath.Join(rootDir, filepath.FromSlash(dir)))
}

func workspaceDirHasIndexFile(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		ext := path.Ext(name)
		base := strings.TrimSuffix(name, ext)
		if strings.EqualFold(base, "index") && strings.EqualFold(ext, ".md") {
			return true
		}
	}
	return false
}

func joinWorkspaceRoutePath(parts ...string) string {
	nonEmpty := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.Trim(part, "/")
		if part != "" {
			nonEmpty = append(nonEmpty, part)
		}
	}
	return strings.Join(nonEmpty, "/")
}

func workspaceRouteLeafSlug(routePath RoutePath) Slug {
	return routePath.LeafSlug()
}

func nonDefaultWorkspaceSourcePath(route WorkspaceMarkdownRoute) WorkspaceSourcePath {
	sourcePath := route.SourcePath.Clean()
	if sourcePath == "" || route.Skip {
		return ""
	}
	if route.Kind == NodeKindSection && route.ContentPath != "" {
		sourcePath = WorkspaceSourcePathFromString(route.ContentPath.Clean().FilesystemPath()).Dir()
	}
	if sourcePath == defaultWorkspaceSourcePath(route.RoutePath, route.Kind) {
		return ""
	}
	return sourcePath
}

func defaultWorkspaceSourcePath(routePath RoutePath, kind NodeKind) WorkspaceSourcePath {
	return routePath.WorkspaceSourcePath(kind)
}

func sameWorkspaceSectionRouteEntry(first WorkspaceMarkdownRoute, second WorkspaceMarkdownRoute) bool {
	if first.Kind != NodeKindSection || second.Kind != NodeKindSection {
		return false
	}
	return sectionSourceDir(first) == second.SourcePath || sectionSourceDir(second) == first.SourcePath
}

func sectionSourceDir(route WorkspaceMarkdownRoute) WorkspaceSourcePath {
	if route.ContentPath == "" {
		return ""
	}
	return WorkspaceSourcePathFromString(route.ContentPath.Clean().FilesystemPath()).Dir()
}
