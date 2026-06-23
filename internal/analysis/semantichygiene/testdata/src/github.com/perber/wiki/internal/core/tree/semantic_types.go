package tree

import (
	"path"
	"path/filepath"
	"strings"
)

type RoutePath string

func (routePath RoutePath) String() string {
	return string(routePath)
}

type MarkdownPath string

func (markdownPath MarkdownPath) String() string {
	return string(markdownPath)
}

type WorkspaceSourcePath string

func CleanMarkdownPath(raw string) MarkdownPath {
	cleaned := path.Clean(strings.Trim(strings.TrimSpace(filepath.ToSlash(raw)), "/"))
	if cleaned == "." {
		return ""
	}
	return MarkdownPath(cleaned)
}

func NewMarkdownPathUnchecked(raw string) MarkdownPath {
	return MarkdownPath(raw)
}

func (markdownPath MarkdownPath) Clean() MarkdownPath {
	return CleanMarkdownPath(string(markdownPath))
}

func (markdownPath MarkdownPath) RoutePath() RoutePath {
	routePath := strings.Trim(strings.TrimSpace(filepath.ToSlash(string(markdownPath))), "/")
	return RoutePath(routePath)
}

func (markdownPath MarkdownPath) SourceDir() MarkdownPath {
	dir := path.Dir(string(markdownPath.Clean()))
	if dir == "." {
		return ""
	}
	return MarkdownPath(dir)
}

func (markdownPath MarkdownPath) FilesystemPath() string {
	return string(markdownPath)
}

func (routePath RoutePath) MarkdownPagePath() MarkdownPath {
	return MarkdownPath(string(routePath) + ".md")
}

func (routePath RoutePath) WorkspaceSourcePath() WorkspaceSourcePath {
	return WorkspaceSourcePath(routePath)
}

func CleanWorkspaceSourcePath(raw string) WorkspaceSourcePath {
	return WorkspaceSourcePath(strings.Trim(strings.TrimSpace(filepath.ToSlash(raw)), "/"))
}

func (sourcePath WorkspaceSourcePath) Dir() WorkspaceSourcePath {
	return WorkspaceSourcePath(path.Dir(string(sourcePath)))
}
