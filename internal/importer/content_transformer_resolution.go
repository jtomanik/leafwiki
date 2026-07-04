package importer

import (
	"fmt"
	"mime/multipart"
	"path"
	"path/filepath"
	"strings"

	"github.com/perber/wiki/internal/core/tree"
)

type rewriteDestinationOptions struct {
	assetOnly bool
}

func (t *contentTransformer) rewriteDestination(
	userID tree.UserID,
	sourcePath tree.WorkspaceSourcePath,
	page *tree.Page,
	destination string,
	wiki ImporterWiki,
	options rewriteDestinationOptions,
) (string, error) {
	trimmed := strings.TrimSpace(destination)
	if trimmed == "" {
		return destination, nil
	}

	prefix, href, suffix := splitMarkdownDestination(trimmed)
	href = unescapeMarkdownDestinationEscapes(href)
	href = normalizeImportedHref(href)
	var resolved string
	var err error
	if options.assetOnly {
		resolved, err = t.resolveAssetDestination(userID, sourcePath, page, href, wiki)
	} else {
		resolved, _, err = t.resolveDestination(userID, sourcePath, page, href, wiki)
	}
	if err != nil {
		return "", err
	}
	if resolved == "" {
		return destination, nil
	}
	return prefix + resolved + suffix, nil
}

func (t *contentTransformer) resolveAssetDestination(
	userID tree.UserID,
	sourcePath tree.WorkspaceSourcePath,
	page *tree.Page,
	href string,
	wiki ImporterWiki,
) (string, error) {
	rawTarget, suffix := splitURLSuffix(href)
	if rawTarget == "" || isExternalHref(rawTarget) || strings.HasPrefix(rawTarget, "#") {
		return "", nil
	}
	rawTarget = decodeImportTarget(rawTarget)

	assetPath, err := t.resolveAndUploadAsset(userID, sourcePath, page, rawTarget, wiki)
	if err != nil {
		return "", err
	}
	if assetPath == "" {
		return "", nil
	}
	return assetPath + suffix, nil
}

// resolveDestination first tries to map the href to another imported page.
// If that fails, it falls back to importing a local asset from the source package.
func (t *contentTransformer) resolveDestination(
	userID tree.UserID,
	sourcePath tree.WorkspaceSourcePath,
	page *tree.Page,
	href string,
	wiki ImporterWiki,
) (string, bool, error) {
	rawTarget, suffix := splitURLSuffix(href)
	if rawTarget == "" || isExternalHref(rawTarget) || strings.HasPrefix(rawTarget, "#") {
		return "", false, nil
	}
	rawTarget = decodeImportTarget(rawTarget)

	if target, ok := t.resolvePageTarget(sourcePath, rawTarget); ok {
		return t.formatResolvedHref(target) + suffix, false, nil
	}

	assetPath, err := t.resolveAndUploadAsset(userID, sourcePath, page, rawTarget, wiki)
	if err != nil {
		return "", false, err
	}
	if assetPath != "" {
		return assetPath + suffix, true, nil
	}

	return "", false, nil
}

func formatResolvedTargetPath(target importTarget) string {
	switch target.kind {
	case tree.NodeKindSection:
		return target.targetPath.Clean().FilesystemPath()
	default:
		trimmed := target.targetPath.Clean().FilesystemPath()
		if strings.EqualFold(path.Ext(trimmed), ".md") {
			return trimmed
		}
		return trimmed + ".md"
	}
}

func (t *contentTransformer) formatResolvedHref(target importTarget) string {
	href := "/" + formatResolvedTargetPath(target)
	prefix := strings.TrimSpace(t.markdownLinkRootPrefix)
	if prefix == "" {
		return href
	}
	if !strings.HasPrefix(prefix, "/") {
		prefix = "/" + prefix
	}
	if href == "/" {
		return strings.TrimRight(prefix, "/")
	}
	return strings.TrimRight(prefix, "/") + href
}

// resolvePageTarget resolves links only against files that are part of the current import plan.
// This avoids guessing against unrelated existing wiki pages and keeps imports predictable.
func (t *contentTransformer) resolvePageTarget(sourcePath tree.WorkspaceSourcePath, href string) (importTarget, bool) {
	candidates := buildSourceCandidates(t.sourceBasePath, sourcePath, href)
	if !strings.HasPrefix(href, "/") && !strings.HasPrefix(href, ".") {
		candidates = append(candidates, buildSourceCandidates(t.sourceBasePath, sourcePath, "/"+href)...)
	}
	for _, candidate := range candidates {
		if target, ok := t.pagesBySource[normalizePlanSourcePath(candidate)]; ok {
			return target, true
		}
	}

	// Obsidian also resolves links by note name when that name is unique in the vault.
	// We only apply that fallback for basename-only links within the current import package.
	if basenameKey, ok := basenameOnlyLookupKey(href); ok {
		if matches := uniqueImportTargets(t.pagesByBasename[basenameKey]); len(matches) == 1 {
			return matches[0], true
		}
	}

	if match, ok := t.resolveUniqueTargetPathSuffix(href); ok {
		return match, true
	}

	return importTarget{}, false
}

func (t *contentTransformer) resolveUniqueTargetPathSuffix(href string) (importTarget, bool) {
	if isNonMarkdownAssetTarget(href) {
		return importTarget{}, false
	}

	suffix, ok := normalizeSourcePathSuffixLookupKey(href)
	if !ok || suffix == "" || !strings.Contains(suffix, "/") {
		return importTarget{}, false
	}

	matches := uniqueImportTargets(t.pagesBySuffix[suffix])
	if kind, ok := impliedImportTargetKind(href); ok {
		matches = filterImportTargetsByKind(matches, kind)
		if len(matches) != 1 {
			return importTarget{}, false
		}
		return matches[0], true
	}
	if len(matches) != 1 {
		return importTarget{}, false
	}
	return matches[0], true
}

func filterImportTargetsByKind(values []importTarget, kind tree.NodeKind) []importTarget {
	out := make([]importTarget, 0, len(values))
	for _, value := range values {
		if value.kind == kind {
			out = append(out, value)
		}
	}
	return out
}

func impliedImportTargetKind(href string) (tree.NodeKind, bool) {
	decoded := strings.TrimSpace(decodeImportTarget(href))
	if decoded == "" {
		return "", false
	}
	trimmed := strings.Trim(strings.TrimPrefix(decoded, "/"), "/")
	if trimmed == "" {
		return "", false
	}
	if strings.HasSuffix(decoded, "/") {
		return tree.NodeKindSection, true
	}

	base := path.Base(trimmed)
	if !strings.EqualFold(path.Ext(base), ".md") {
		return "", false
	}
	switch {
	case strings.EqualFold(base, "index.md"), base == "README.md":
		return tree.NodeKindSection, true
	default:
		return tree.NodeKindPage, true
	}
}

// resolveAndUploadAsset imports local non-Markdown files into the target page's asset folder
// and caches the public path so repeated references on the same page reuse the upload result.
func (t *contentTransformer) resolveAndUploadAsset(
	userID tree.UserID,
	sourcePath tree.WorkspaceSourcePath,
	page *tree.Page,
	href string,
	wiki ImporterWiki,
) (string, error) {
	assetAbs, ok := resolveAssetPath(t.sourceBasePath, sourcePath, href)
	if !ok {
		return "", nil
	}

	cacheKey := assetUploadKey{pageID: page.ID, assetPath: assetAbs}
	if uploaded, ok := t.assetUploads[cacheKey]; ok {
		return uploaded, nil
	}

	file, err := importerOpenAsset(assetAbs)
	if err != nil {
		return "", fmt.Errorf("open asset %q: %w", assetAbs, err)
	}
	defer func() {
		_ = file.Close()
	}()

	publicPath, err := wiki.UploadAsset(userID, page.ID, multipart.File(file), tree.AssetNameFromString(filepath.Base(assetAbs)), t.assetMaxBytes)
	if err != nil {
		return "", fmt.Errorf("upload asset %q: %w", assetAbs, err)
	}

	t.assetUploads[cacheKey] = publicPath
	return publicPath, nil
}
