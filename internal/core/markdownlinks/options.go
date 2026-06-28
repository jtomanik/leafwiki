package markdownlinks

import (
	"fmt"
	"net/url"
	"path"
	"strings"
)

type Options struct {
	MarkdownLinkRootPrefix string
}

func NormalizeMarkdownLinkRootPrefix(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", nil
	}
	if strings.Contains(trimmed, "\\") {
		return "", fmt.Errorf("markdown link root prefix must use forward slashes")
	}
	if strings.Contains(trimmed, "://") || strings.HasPrefix(trimmed, "//") {
		return "", fmt.Errorf("markdown link root prefix must be a path prefix")
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("parse markdown link root prefix: %w", err)
	}
	if parsed.Scheme != "" || parsed.Opaque != "" {
		return "", fmt.Errorf("markdown link root prefix must be a path prefix")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("markdown link root prefix must not contain query or fragment")
	}
	prefix := parsed.Path
	for _, segment := range strings.Split(prefix, "/") {
		if segment == ".." {
			return "", fmt.Errorf("markdown link root prefix must not traverse directories")
		}
	}
	if !strings.HasPrefix(prefix, "/") {
		prefix = "/" + prefix
	}
	prefix = path.Clean(prefix)
	if prefix == "/" || prefix == "." || strings.Contains(prefix, "/../") || strings.HasSuffix(prefix, "/..") {
		return "", fmt.Errorf("markdown link root prefix must name a non-root path under the repository root")
	}
	return strings.TrimRight(prefix, "/"), nil
}

func normalizeMarkdownLinkRootPrefix(value string) (string, error) {
	return NormalizeMarkdownLinkRootPrefix(value)
}
