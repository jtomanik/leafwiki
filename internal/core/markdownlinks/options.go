package markdownlinks

import (
	"errors"
	"fmt"
	"net/url"
	"path"
	"strings"
)

var (
	ErrMarkdownLinkRootPrefixBackslash       = errors.New("markdown link root prefix must use forward slashes")
	ErrMarkdownLinkRootPrefixNotPath         = errors.New("markdown link root prefix must be a path prefix")
	ErrMarkdownLinkRootPrefixParse           = errors.New("parse markdown link root prefix")
	ErrMarkdownLinkRootPrefixQueryOrFragment = errors.New("markdown link root prefix must not contain query or fragment")
	ErrMarkdownLinkRootPrefixTraversal       = errors.New("markdown link root prefix must not traverse directories")
	ErrMarkdownLinkRootPrefixRoot            = errors.New("markdown link root prefix must name a non-root path under the repository root")
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
		return "", ErrMarkdownLinkRootPrefixBackslash
	}
	if strings.Contains(trimmed, "://") || strings.HasPrefix(trimmed, "//") {
		return "", ErrMarkdownLinkRootPrefixNotPath
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrMarkdownLinkRootPrefixParse, err)
	}
	if parsed.Scheme != "" || parsed.Opaque != "" {
		return "", ErrMarkdownLinkRootPrefixNotPath
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", ErrMarkdownLinkRootPrefixQueryOrFragment
	}
	prefix := parsed.Path
	for _, segment := range strings.Split(prefix, "/") {
		if segment == ".." {
			return "", ErrMarkdownLinkRootPrefixTraversal
		}
	}
	if !strings.HasPrefix(prefix, "/") {
		prefix = "/" + prefix
	}
	prefix = path.Clean(prefix)
	if prefix == "/" || prefix == "." || strings.Contains(prefix, "/../") || strings.HasSuffix(prefix, "/..") {
		return "", ErrMarkdownLinkRootPrefixRoot
	}
	return strings.TrimRight(prefix, "/"), nil
}

func normalizeMarkdownLinkRootPrefix(value string) (string, error) {
	return NormalizeMarkdownLinkRootPrefix(value)
}
