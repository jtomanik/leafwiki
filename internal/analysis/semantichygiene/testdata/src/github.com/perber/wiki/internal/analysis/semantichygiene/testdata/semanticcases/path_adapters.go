package semanticcases

import (
	pathpkg "path"
	"path/filepath"
	"strings"
)

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

func (path MarkdownPath) Clean() MarkdownPath {
	return CleanMarkdownPath(string(path))
}

func (path MarkdownPath) Ext() string {
	return pathpkg.Ext(string(path))
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

func (path RoutePath) MarkdownPagePath() MarkdownPath {
	if path.Clean() == "" {
		return MarkdownPath("index.md")
	}
	return MarkdownPath(string(path.Clean()) + ".md")
}

func (path RoutePath) LowerKey(kind string) string {
	return kind + ":" + strings.ToLower(string(path.Clean()))
}

func (path RoutePath) WorkspaceSourcePath(kind string) WorkspaceSourcePath {
	normalized := path.Clean()
	if normalized == "" {
		return ""
	}
	return WorkspaceSourcePath(string(normalized) + ".md")
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

func (id UserID) ActorID() string {
	return strings.TrimSpace(string(id))
}
