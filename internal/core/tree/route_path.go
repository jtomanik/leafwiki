package tree

import (
	"fmt"
	"path"
	"path/filepath"
	"strings"
)

func ValidateRoutePath(routePath string) (RoutePath, error) {
	routePath = strings.TrimSpace(routePath)
	if routePath == "" {
		return "", fmt.Errorf("missing path")
	}
	if strings.Contains(routePath, `\`) {
		return "", fmt.Errorf("invalid path %s", routePath)
	}
	for _, segment := range strings.Split(routePath, "/") {
		if segment == "" || segment == "." || segment == ".." || strings.TrimSpace(segment) != segment {
			return "", fmt.Errorf("invalid path %s", routePath)
		}
	}
	return RoutePath(routePath), nil
}

func MarkdownPathToRoutePath(markdownPath string) string {
	routePath := strings.Trim(strings.TrimSpace(filepath.ToSlash(markdownPath)), "/")
	ext := path.Ext(routePath)
	if strings.EqualFold(ext, ".md") {
		routePath = strings.TrimSuffix(routePath, ext)
	}
	parts := strings.Split(routePath, "/")
	if len(parts) > 0 && strings.EqualFold(parts[len(parts)-1], "index") {
		parts = parts[:len(parts)-1]
	}
	return strings.Trim(strings.Join(parts, "/"), "/")
}
