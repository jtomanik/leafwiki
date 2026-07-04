package importer

import (
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/perber/wiki/internal/core/tree"
)

func normalizePlanSourcePath(p string) string {
	return path.Clean(strings.TrimPrefix(filepath.ToSlash(strings.TrimSpace(p)), "/"))
}

func basenameOnlyLookupKey(href string) (string, bool) {
	trimmed := strings.TrimSpace(decodeImportTarget(href))
	if trimmed == "" || strings.Contains(trimmed, "/") || strings.HasPrefix(trimmed, ".") {
		return "", false
	}

	key := normalizePageBasenameForLookup(trimmed)
	return key, key != ""
}

func normalizePageBasenameForLookup(value string) string {
	base, _ := splitURLSuffix(strings.TrimSpace(decodeImportTarget(value)))
	base = path.Base(base)
	if strings.EqualFold(path.Ext(base), ".md") {
		base = strings.TrimSuffix(base, path.Ext(base))
	}
	base = strings.TrimSpace(base)
	if base == "" || strings.EqualFold(base, "index") {
		return ""
	}
	return base
}

func decodeImportTarget(value string) string {
	decoded, err := url.PathUnescape(value)
	if err != nil {
		return value
	}
	return decoded
}

func buildSourceCandidates(sourceBasePath string, sourcePath tree.WorkspaceSourcePath, href string) []string {
	raw := filepath.ToSlash(strings.TrimSpace(href))
	if raw == "" {
		return nil
	}

	var base string
	if strings.HasPrefix(raw, "/") {
		base = path.Clean(strings.TrimPrefix(raw, "/"))
	} else {
		currentDir := path.Dir(filepath.ToSlash(sourcePath.FilesystemPath()))
		if currentDir == "." {
			currentDir = ""
		}
		base = path.Clean(path.Join(currentDir, raw))
	}

	if base == "." || strings.HasPrefix(base, "../") {
		return nil
	}

	candidates := []string{base}
	trimmed := strings.TrimSuffix(base, "/")

	if strings.HasSuffix(raw, "/") {
		candidates = append(candidates, activeSourceSectionContentCandidates(sourceBasePath, trimmed)...)
	}

	ext := strings.ToLower(path.Ext(trimmed))
	switch ext {
	case "":
		candidates = append(candidates, trimmed+".md")
		candidates = append(candidates, activeSourceSectionContentCandidates(sourceBasePath, trimmed)...)
	}

	return uniqueStrings(candidates)
}

func activeSourceSectionContentCandidates(sourceBasePath string, sourceDir string) []string {
	if indexName, ok := sourceDirIndexFile(sourceBasePath, sourceDir); ok {
		return []string{path.Join(sourceDir, indexName)}
	}
	if readmeName, ok := sourceDirReadmeFallbackFile(sourceBasePath, sourceDir); ok {
		return []string{path.Join(sourceDir, readmeName)}
	}
	return []string{path.Join(sourceDir, "index.md"), path.Join(sourceDir, "README.md")}
}

func sourceDirIndexFile(sourceBasePath string, sourceDir string) (string, bool) {
	entries, err := os.ReadDir(filepath.Join(sourceBasePath, filepath.FromSlash(sourceDir)))
	if err != nil {
		return "", false
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if strings.EqualFold(entry.Name(), "index.md") {
			return entry.Name(), true
		}
	}
	return "", false
}

func sourceDirReadmeFallbackFile(sourceBasePath string, sourceDir string) (string, bool) {
	entries, err := os.ReadDir(filepath.Join(sourceBasePath, filepath.FromSlash(sourceDir)))
	if err != nil {
		return "", false
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if entry.Name() == "README.md" {
			return entry.Name(), true
		}
	}
	return "", false
}

func (t *contentTransformer) fallbackWikiPageHref(sourcePath tree.WorkspaceSourcePath, href string) (string, bool) {
	rawTarget, suffix := splitURLSuffix(href)
	if rawTarget == "" || isExternalHref(rawTarget) || strings.HasPrefix(rawTarget, "#") {
		return "", false
	}

	if isNonMarkdownAssetTarget(rawTarget) {
		return "", false
	}

	if basenameKey, ok := basenameOnlyLookupKey(rawTarget); ok {
		exactMatches, hasExactMatches := t.pagesByBasename[basenameKey]
		if matches := uniqueImportTargets(exactMatches); len(matches) == 1 {
			return t.formatResolvedHref(matches[0]) + suffix, true
		}
		if hasExactMatches && len(exactMatches) > 0 {
			return "", false
		}
		if t.hasCaseVariantBasenameMatch(basenameKey) {
			return "", false
		}
		routePath, ok := t.normalizeWikiHrefToRoutePath(rawTarget)
		if !ok {
			return "", false
		}
		return "/" + routePath + suffix, true
	}

	if strings.HasPrefix(rawTarget, ".") || strings.HasPrefix(rawTarget, "/") {
		if t.hasCaseVariantSourceSuffixMatch(rawTarget) {
			return "", false
		}
		candidates := buildSourceCandidates(t.sourceBasePath, sourcePath, rawTarget)
		if len(candidates) == 0 {
			return "", false
		}
		routePath, ok := t.normalizeSourceCandidateToRoutePath(candidates[0])
		if !ok {
			return "", false
		}
		return "/" + routePath + suffix, true
	}

	if t.hasCaseVariantSourceSuffixMatch(rawTarget) {
		return "", false
	}
	routePath, ok := t.normalizeWikiHrefToRoutePath(rawTarget)
	if !ok {
		return "", false
	}
	return "/" + routePath + suffix, true
}

func (t *contentTransformer) hasCaseVariantSourceSuffixMatch(href string) bool {
	key, ok := normalizeSourcePathSuffixLookupKey(href)
	if !ok {
		return false
	}
	if _, ok := t.pagesBySuffix[key]; ok {
		return false
	}
	for existing := range t.pagesBySuffix {
		if existing != key && strings.EqualFold(existing, key) {
			return true
		}
	}
	return false
}

func (t *contentTransformer) hasCaseVariantBasenameMatch(key string) bool {
	for existing := range t.pagesByBasename {
		if existing != key && strings.EqualFold(existing, key) {
			return true
		}
	}
	return false
}

func isNonMarkdownAssetTarget(href string) bool {
	decoded := strings.TrimSpace(decodeImportTarget(href))
	ext := strings.ToLower(path.Ext(decoded))
	return ext != "" && ext != ".md"
}

func (t *contentTransformer) normalizeWikiHrefToRoutePath(href string) (string, bool) {
	decoded := strings.TrimSpace(decodeImportTarget(href))
	if decoded == "" {
		return "", false
	}

	decoded = strings.TrimPrefix(decoded, "/")
	decoded = strings.TrimSuffix(decoded, "/")
	if decoded == "" {
		return "", false
	}

	segments := strings.Split(decoded, "/")
	for i, segment := range segments {
		segment = strings.TrimSpace(segment)
		if segment == "" {
			return "", false
		}

		if i == len(segments)-1 && strings.EqualFold(path.Ext(segment), ".md") {
			segment = strings.TrimSuffix(segment, path.Ext(segment))
		}

		safe := t.slugger.GenerateValidSlug(segment)
		if safe == "" {
			return "", false
		}
		segments[i] = safe
	}

	return strings.Join(segments, "/"), true
}

func (t *contentTransformer) normalizeSourceCandidateToRoutePath(candidate string) (string, bool) {
	normalized, ok := t.normalizeWikiHrefToRoutePath(candidate)
	if !ok {
		return "", false
	}

	normalized = strings.TrimSuffix(normalized, "/index")
	normalized = strings.Trim(normalized, "/")

	return normalized, true
}

func buildSourcePathSuffixKeys(sourcePath string, kind tree.NodeKind) []string {
	trimmed := strings.Trim(strings.TrimSpace(sourcePath), "/")
	if trimmed == "" {
		return nil
	}

	if strings.EqualFold(path.Ext(trimmed), ".md") {
		base := path.Base(trimmed)
		switch {
		case strings.EqualFold(base, "index.md"):
			trimmed = strings.Trim(path.Dir(trimmed), "/")
		case kind == tree.NodeKindSection && base == "README.md":
			trimmed = strings.Trim(path.Dir(trimmed), "/")
		default:
			trimmed = strings.TrimSuffix(trimmed, path.Ext(trimmed))
		}
	}
	if trimmed == "" || trimmed == "." {
		return nil
	}

	segments := strings.Split(trimmed, "/")
	suffixes := make([]string, 0, len(segments))
	for i := range segments {
		suffixes = append(suffixes, strings.Join(segments[i:], "/"))
	}

	return uniqueStrings(suffixes)
}

func normalizeSourcePathSuffixLookupKey(href string) (string, bool) {
	base, _ := splitURLSuffix(strings.TrimSpace(decodeImportTarget(href)))
	base = strings.Trim(strings.TrimPrefix(filepath.ToSlash(base), "/"), "/")
	if base == "" || base == "." || strings.HasPrefix(base, "../") {
		return "", false
	}

	if strings.EqualFold(path.Ext(base), ".md") {
		name := path.Base(base)
		switch {
		case strings.EqualFold(name, "index.md"), name == "README.md":
			base = strings.Trim(path.Dir(base), "/")
		default:
			base = strings.TrimSuffix(base, path.Ext(base))
		}
	}
	if base == "" || base == "." {
		return "", false
	}
	return base, true
}

// resolveAssetPath keeps asset resolution inside the extracted import workspace.
// This prevents uploaded archives from referencing files outside the package on disk.
func resolveAssetPath(sourceBasePath string, sourcePath tree.WorkspaceSourcePath, href string) (string, bool) {
	raw := filepath.ToSlash(strings.TrimSpace(href))
	if raw == "" {
		return "", false
	}
	if strings.HasSuffix(strings.ToLower(raw), ".md") {
		return "", false
	}

	var rel string
	if strings.HasPrefix(raw, "/") {
		rel = path.Clean(strings.TrimPrefix(raw, "/"))
	} else {
		currentDir := path.Dir(filepath.ToSlash(sourcePath.FilesystemPath()))
		if currentDir == "." {
			currentDir = ""
		}
		rel = path.Clean(path.Join(currentDir, raw))
	}

	if rel == "." || strings.HasPrefix(rel, "../") {
		return "", false
	}

	abs := filepath.Join(sourceBasePath, filepath.FromSlash(rel))
	baseAbs, err := importerFilepathAbs(sourceBasePath)
	if err != nil {
		return "", false
	}
	absResolved, err := importerFilepathAbs(abs)
	if err != nil {
		return "", false
	}

	relCheck, err := importerFilepathRel(baseAbs, absResolved)
	if err != nil || relCheck == ".." || strings.HasPrefix(relCheck, ".."+string(filepath.Separator)) {
		return "", false
	}

	info, err := os.Stat(absResolved)
	if err != nil || info.IsDir() {
		return "", false
	}

	return absResolved, true
}
