package importer

import (
	"fmt"
	"mime/multipart"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/perber/wiki/internal/core/markdownlinks"
	"github.com/perber/wiki/internal/core/tree"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

type importTarget struct {
	targetPath string
	kind       tree.NodeKind
}

type contentTransformer struct {
	sourceBasePath  string
	assetMaxBytes   int64
	slugger         *tree.SlugService
	pagesBySource   map[string]importTarget
	pagesByBasename map[string][]importTarget
	pagesBySuffix   map[string][]importTarget
	assetUploads    map[string]string
}

var importerMarkdownParser = goldmark.New()

// newContentTransformer precomputes source->target lookups from the import plan.
// We resolve links against planned imports so we only rewrite destinations we can actually create.
func newContentTransformer(plan *PlanResult, sourceBasePath string, assetMaxBytes int64) *contentTransformer {
	pagesBySource := make(map[string]importTarget, len(plan.Items))
	pagesByBasename := make(map[string][]importTarget, len(plan.Items))
	pagesBySuffix := make(map[string][]importTarget, len(plan.Items))
	for _, item := range plan.Items {
		normalizedSource := normalizePlanSourcePath(item.SourcePath)
		target := importTarget{
			targetPath: item.TargetPath,
			kind:       item.Kind,
		}
		pagesBySource[normalizedSource] = target

		if basenameKey := normalizePageBasenameForLookup(path.Base(normalizedSource)); basenameKey != "" {
			pagesByBasename[basenameKey] = append(pagesByBasename[basenameKey], target)
		}

		for _, suffixKey := range buildSourcePathSuffixKeys(normalizedSource, item.Kind) {
			pagesBySuffix[suffixKey] = append(pagesBySuffix[suffixKey], target)
		}
	}

	return &contentTransformer{
		sourceBasePath:  sourceBasePath,
		assetMaxBytes:   assetMaxBytes,
		slugger:         tree.NewSlugService(),
		pagesBySource:   pagesBySource,
		pagesByBasename: pagesByBasename,
		pagesBySuffix:   pagesBySuffix,
		assetUploads:    map[string]string{},
	}
}

// TransformContent rewrites Markdown links, wiki links, and asset references for one imported page.
// Rewrites only happen outside inline code, fenced blocks, and indented code so examples remain untouched.
func (t *contentTransformer) TransformContent(
	userID string,
	sourcePath string,
	page *tree.Page,
	content string,
	wiki ImporterWiki,
) (string, error) {
	rewritten, err := rewriteOutsideCodeSpans(content, func(segment string) (string, error) {
		return t.rewriteMarkdownLinks(userID, sourcePath, page, segment, wiki)
	})
	if err != nil {
		return "", err
	}
	return rewriteOutsideCodeSpans(rewritten, func(segment string) (string, error) {
		return t.rewriteWikiLinks(userID, sourcePath, page, segment, wiki)
	})
}

// rewriteMarkdownLinks rewrites regular Markdown links and images in non-code segments only.
func (t *contentTransformer) rewriteMarkdownLinks(
	userID string,
	sourcePath string,
	page *tree.Page,
	content string,
	wiki ImporterWiki,
) (string, error) {
	var out strings.Builder
	last := 0
	for _, occurrence := range markdownlinks.ScanInlineDestinations(content, markdownlinks.InlineScanOptions{
		IncludeImages:    true,
		IgnoreCodeRanges: true,
	}) {
		out.WriteString(content[last:occurrence.Start])
		rewritten, err := t.rewriteDestination(userID, sourcePath, page, occurrence.Destination, wiki, rewriteDestinationOptions{
			assetOnly: occurrence.Image,
		})
		if err != nil {
			return "", err
		}
		out.WriteString(rewritten)
		last = occurrence.End
	}
	out.WriteString(content[last:])

	return t.rewriteMarkdownReferenceDefinitions(userID, sourcePath, page, out.String(), wiki)
}

func (t *contentTransformer) rewriteMarkdownReferenceDefinitions(
	userID string,
	sourcePath string,
	page *tree.Page,
	content string,
	wiki ImporterWiki,
) (string, error) {
	usage := importerReferenceLabelUsage(content)
	var out strings.Builder
	lineStart := 0
	for lineStart < len(content) {
		lineEnd := strings.IndexByte(content[lineStart:], '\n')
		hasNewline := lineEnd >= 0
		if hasNewline {
			lineEnd += lineStart
		} else {
			lineEnd = len(content)
		}

		label, destStart, destEnd, ok := parseMarkdownReferenceDestination(content, lineStart, lineEnd)
		if !ok {
			out.WriteString(content[lineStart:lineEnd])
		} else {
			rewritten, err := t.rewriteDestination(userID, sourcePath, page, content[destStart:destEnd], wiki, rewriteDestinationOptions{
				assetOnly: usage.imageOnly(label),
			})
			if err != nil {
				return "", err
			}
			out.WriteString(content[lineStart:destStart])
			out.WriteString(rewritten)
			out.WriteString(content[destEnd:lineEnd])
		}

		if hasNewline {
			out.WriteByte('\n')
			lineStart = lineEnd + 1
		} else {
			break
		}
	}
	return out.String(), nil
}

type importerReferenceUsage struct {
	linkLabels  map[string]struct{}
	imageLabels map[string]struct{}
}

func importerReferenceLabelUsage(content string) importerReferenceUsage {
	usage := importerReferenceUsage{
		linkLabels:  map[string]struct{}{},
		imageLabels: map[string]struct{}{},
	}
	reader := text.NewReader([]byte(content))
	doc := importerMarkdownParser.Parser().Parse(reader)
	_ = ast.Walk(doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch n := node.(type) {
		case *ast.Link:
			if n.Reference != nil {
				usage.linkLabels[normalizeImporterReferenceLabel(n.Reference.Value)] = struct{}{}
			}
		case *ast.Image:
			if n.Reference != nil {
				usage.imageLabels[normalizeImporterReferenceLabel(n.Reference.Value)] = struct{}{}
			}
		}
		return ast.WalkContinue, nil
	})
	return usage
}

func normalizeImporterReferenceLabel(label []byte) string {
	return util.ToLinkReference(label)
}

func (usage importerReferenceUsage) imageOnly(label string) bool {
	if label == "" {
		return false
	}
	if _, ok := usage.imageLabels[label]; !ok {
		return false
	}
	_, usedByLink := usage.linkLabels[label]
	return !usedByLink
}

func parseMarkdownReferenceDestination(content string, lineStart int, lineEnd int) (string, int, int, bool) {
	i := lineStart
	spaces := 0
	for i < lineEnd && content[i] == ' ' && spaces < 4 {
		i++
		spaces++
	}
	if spaces > 3 || i >= lineEnd || content[i] != '[' || isEscapedMarkdownBracket(content, i) {
		return "", 0, 0, false
	}
	labelEnd := findMarkdownClosingBracket(content, i)
	if labelEnd < 0 {
		return "", 0, 0, false
	}
	if labelEnd+1 >= lineEnd || content[labelEnd+1] != ':' {
		return "", 0, 0, false
	}
	label := normalizeImporterReferenceLabel([]byte(content[i+1 : labelEnd]))
	i = labelEnd + 2
	for i < lineEnd && (content[i] == ' ' || content[i] == '\t') {
		i++
	}
	if i >= lineEnd {
		return "", 0, 0, false
	}
	destStart := i
	destEnd := lineEnd
	if content[i] == '<' {
		i++
		destStart = i
		for i < lineEnd {
			if content[i] == '>' {
				destEnd = i
				break
			}
			i++
		}
	} else {
		for i < lineEnd {
			if content[i] == ' ' || content[i] == '\t' {
				destEnd = i
				break
			}
			i++
		}
	}
	return label, destStart, destEnd, destEnd > destStart
}

// rewriteWikiLinks handles Obsidian-style wiki links and converts them to plain Markdown links.
func (t *contentTransformer) rewriteWikiLinks(
	userID string,
	sourcePath string,
	page *tree.Page,
	content string,
	wiki ImporterWiki,
) (string, error) {
	var out strings.Builder
	for i := 0; i < len(content); {
		nextImage := strings.Index(content[i:], "![[")
		nextLink := strings.Index(content[i:], "[[")
		if nextImage < 0 && nextLink < 0 {
			out.WriteString(content[i:])
			break
		}

		next := -1
		isImage := false
		switch {
		case nextImage >= 0 && (nextLink < 0 || nextImage <= nextLink):
			next = i + nextImage
			isImage = true
		case nextLink >= 0:
			next = i + nextLink
		}

		out.WriteString(content[i:next])

		startOffset := 2
		if isImage {
			startOffset = 3
		}
		end := strings.Index(content[next+startOffset:], "]]")
		if end < 0 {
			out.WriteString(content[next:])
			break
		}
		end += next + startOffset

		inner := strings.TrimSpace(content[next+startOffset : end])
		targetPart, label := splitWikiLink(inner)
		targetPart = normalizeImportedHref(targetPart)
		href, isAsset, err := t.resolveDestination(userID, sourcePath, page, targetPart, wiki)
		if err != nil {
			return "", err
		}
		if href == "" {
			fallbackHref, ok := t.fallbackWikiPageHref(sourcePath, targetPart)
			if !ok {
				out.WriteString(content[next : end+2])
				i = end + 2
				continue
			}
			href = fallbackHref
		}

		if label == "" {
			label = defaultWikiLinkLabel(targetPart)
		}

		if shouldRenderWikiLinkAsImage(isImage, isAsset, targetPart) {
			out.WriteString("![")
			out.WriteString(label)
			out.WriteString("](")
			out.WriteString(href)
			out.WriteByte(')')
		} else {
			out.WriteString("[")
			out.WriteString(label)
			out.WriteString("](")
			out.WriteString(href)
			out.WriteByte(')')
		}
		i = end + 2
	}

	return out.String(), nil
}

// rewriteDestination keeps the original Markdown destination wrapper and title suffix intact
// while only replacing the actual href when we can resolve it safely.
type rewriteDestinationOptions struct {
	assetOnly bool
}

func (t *contentTransformer) rewriteDestination(
	userID string,
	sourcePath string,
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
	userID string,
	sourcePath string,
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
	userID string,
	sourcePath string,
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
		return "/" + formatResolvedTargetPath(target) + suffix, false, nil
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
		return strings.Trim(target.targetPath, "/")
	default:
		trimmed := strings.Trim(target.targetPath, "/")
		if strings.EqualFold(path.Ext(trimmed), ".md") {
			return trimmed
		}
		return trimmed + ".md"
	}
}

// resolvePageTarget resolves links only against files that are part of the current import plan.
// This avoids guessing against unrelated existing wiki pages and keeps imports predictable.
func (t *contentTransformer) resolvePageTarget(sourcePath string, href string) (importTarget, bool) {
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
	if strings.HasSuffix(decoded, "/") {
		return tree.NodeKindSection, true
	}

	trimmed := strings.Trim(strings.TrimPrefix(decoded, "/"), "/")
	if trimmed == "" {
		return "", false
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
	userID string,
	sourcePath string,
	page *tree.Page,
	href string,
	wiki ImporterWiki,
) (string, error) {
	assetAbs, ok := resolveAssetPath(t.sourceBasePath, sourcePath, href)
	if !ok {
		return "", nil
	}

	cacheKey := page.ID + "::" + assetAbs
	if uploaded, ok := t.assetUploads[cacheKey]; ok {
		return uploaded, nil
	}

	file, err := os.Open(assetAbs)
	if err != nil {
		return "", fmt.Errorf("open asset %q: %w", assetAbs, err)
	}
	defer func() {
		_ = file.Close()
	}()

	publicPath, err := wiki.UploadAsset(userID, page.ID, multipart.File(file), filepath.Base(assetAbs), t.assetMaxBytes)
	if err != nil {
		return "", fmt.Errorf("upload asset %q: %w", assetAbs, err)
	}

	t.assetUploads[cacheKey] = publicPath
	return publicPath, nil
}

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

func buildSourceCandidates(sourceBasePath string, sourcePath string, href string) []string {
	raw := filepath.ToSlash(strings.TrimSpace(href))
	if raw == "" {
		return nil
	}

	var base string
	if strings.HasPrefix(raw, "/") {
		base = path.Clean(strings.TrimPrefix(raw, "/"))
	} else {
		currentDir := path.Dir(filepath.ToSlash(sourcePath))
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

func (t *contentTransformer) fallbackWikiPageHref(sourcePath string, href string) (string, bool) {
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
			return "/" + formatResolvedTargetPath(matches[0]) + suffix, true
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
	if normalized == "" {
		return "", false
	}

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
func resolveAssetPath(sourceBasePath string, sourcePath string, href string) (string, bool) {
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
		currentDir := path.Dir(filepath.ToSlash(sourcePath))
		if currentDir == "." {
			currentDir = ""
		}
		rel = path.Clean(path.Join(currentDir, raw))
	}

	if rel == "." || strings.HasPrefix(rel, "../") {
		return "", false
	}

	abs := filepath.Join(sourceBasePath, filepath.FromSlash(rel))
	baseAbs, err := filepath.Abs(sourceBasePath)
	if err != nil {
		return "", false
	}
	absResolved, err := filepath.Abs(abs)
	if err != nil {
		return "", false
	}

	relCheck, err := filepath.Rel(baseAbs, absResolved)
	if err != nil || relCheck == ".." || strings.HasPrefix(relCheck, ".."+string(filepath.Separator)) {
		return "", false
	}

	info, err := os.Stat(absResolved)
	if err != nil || info.IsDir() {
		return "", false
	}

	return absResolved, true
}

func splitMarkdownDestination(destination string) (prefix string, href string, suffix string) {
	if destination == "" {
		return "", "", ""
	}
	if destination[0] == '<' {
		if end := strings.IndexByte(destination, '>'); end >= 0 {
			return "<", destination[1:end], destination[end:]
		}
	}

	spaceIdx := strings.IndexAny(destination, " \t")
	if spaceIdx < 0 {
		return "", destination, ""
	}
	return "", destination[:spaceIdx], destination[spaceIdx:]
}

func splitURLSuffix(raw string) (base string, suffix string) {
	queryIdx := strings.IndexByte(raw, '?')
	hashIdx := strings.IndexByte(raw, '#')

	switch {
	case queryIdx >= 0 && hashIdx >= 0:
		cut := queryIdx
		if hashIdx < queryIdx {
			cut = hashIdx
		}
		return raw[:cut], raw[cut:]
	case queryIdx >= 0:
		return raw[:queryIdx], raw[queryIdx:]
	case hashIdx >= 0:
		return raw[:hashIdx], raw[hashIdx:]
	default:
		return raw, ""
	}
}

func splitWikiLink(inner string) (target string, label string) {
	parts := strings.SplitN(inner, "|", 2)
	target = strings.TrimSpace(parts[0])
	if len(parts) == 2 {
		label = strings.TrimSpace(parts[1])
	}
	return target, label
}

func normalizeImportedHref(href string) string {
	trimmed := strings.TrimSpace(href)
	if trimmed == "" {
		return href
	}
	if looksLikeWindowsDrivePath(trimmed) {
		return href
	}
	return strings.ReplaceAll(href, `\`, "/")
}

func looksLikeWindowsDrivePath(value string) bool {
	if len(value) < 3 {
		return false
	}
	drive := value[0]
	return ((drive >= 'a' && drive <= 'z') || (drive >= 'A' && drive <= 'Z')) &&
		value[1] == ':' &&
		(value[2] == '\\' || value[2] == '/')
}

func defaultWikiLinkLabel(target string) string {
	base, _ := splitURLSuffix(target)
	base = strings.TrimSuffix(base, "/")
	base = strings.TrimSuffix(base, ".md")
	if idx := strings.LastIndex(base, "/"); idx >= 0 {
		base = base[idx+1:]
	}
	if idx := strings.LastIndex(base, "#"); idx >= 0 {
		base = base[:idx]
	}
	if base == "" {
		return target
	}
	return base
}

func shouldRenderWikiLinkAsImage(isEmbed bool, resolvedAsset bool, target string) bool {
	if isEmbed {
		return true
	}
	if !resolvedAsset {
		return false
	}
	return isImageAssetTarget(target)
}

func isImageAssetTarget(target string) bool {
	base, _ := splitURLSuffix(target)
	switch strings.ToLower(path.Ext(base)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".svg", ".avif":
		return true
	default:
		return false
	}
}

func findMarkdownClosingBracket(content string, start int) int {
	if start >= len(content) || content[start] != '[' {
		return -1
	}
	depth := 0
	for i := start; i < len(content); i++ {
		switch content[i] {
		case '\\':
			i++
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func isEscapedMarkdownBracket(content string, offset int) bool {
	backslashes := 0
	for i := offset - 1; i >= 0 && content[i] == '\\'; i-- {
		backslashes++
	}
	return backslashes%2 == 1
}

func unescapeMarkdownDestinationEscapes(destination string) string {
	var out strings.Builder
	out.Grow(len(destination))
	for i := 0; i < len(destination); i++ {
		if destination[i] == '\\' && i+1 < len(destination) && isMarkdownEscapablePunctuation(destination[i+1]) {
			i++
		}
		out.WriteByte(destination[i])
	}
	return out.String()
}

func isMarkdownEscapablePunctuation(ch byte) bool {
	return strings.ContainsRune(`!"#$%&'()*+,-./:;<=>?@[\]^_{|}~`+"`", rune(ch))
}

func isExternalHref(href string) bool {
	if strings.HasPrefix(href, "//") || strings.HasPrefix(href, "mailto:") || strings.HasPrefix(href, "tel:") {
		return true
	}

	u, err := url.Parse(href)
	return err == nil && u.Host != ""
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func uniqueImportTargets(values []importTarget) []importTarget {
	seen := make(map[string]struct{}, len(values))
	out := make([]importTarget, 0, len(values))
	for _, value := range values {
		if value.targetPath == "" {
			continue
		}
		key := value.targetPath + "\x00" + string(value.kind)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
	}
	return out
}

// rewriteOutsideCodeSpans applies rewrites only to plain text segments.
// The importer must not rewrite examples inside inline code, fenced blocks, or indented code blocks.
func rewriteOutsideCodeSpans(content string, rewrite func(string) (string, error)) (string, error) {
	var out strings.Builder
	plainStart := 0
	for _, codeRange := range excludedMarkdownCodeRanges(content) {
		if codeRange.Start < plainStart {
			continue
		}
		rewritten, err := rewrite(content[plainStart:codeRange.Start])
		if err != nil {
			return "", err
		}
		out.WriteString(rewritten)
		out.WriteString(content[codeRange.Start:codeRange.End])
		plainStart = codeRange.End
	}

	rewritten, err := rewrite(content[plainStart:])
	if err != nil {
		return "", err
	}
	out.WriteString(rewritten)
	return out.String(), nil
}

type markdownTextRange struct {
	Start int
	End   int
}

func excludedMarkdownCodeRanges(content string) []markdownTextRange {
	var ranges []markdownTextRange
	reader := text.NewReader([]byte(content))
	doc := importerMarkdownParser.Parser().Parse(reader)
	_ = ast.Walk(doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch n := node.(type) {
		case *ast.CodeSpan:
			ranges = append(ranges, collectMarkdownTextNodeRanges(n)...)
		case *ast.FencedCodeBlock:
			ranges = append(ranges, collectMarkdownBlockRanges(n)...)
		case *ast.CodeBlock:
			ranges = append(ranges, collectMarkdownBlockRanges(n)...)
		}
		return ast.WalkContinue, nil
	})
	return mergeMarkdownTextRanges(ranges)
}

func collectMarkdownTextNodeRanges(parent ast.Node) []markdownTextRange {
	var ranges []markdownTextRange
	for child := parent.FirstChild(); child != nil; child = child.NextSibling() {
		textNode, ok := child.(*ast.Text)
		if !ok {
			continue
		}
		ranges = append(ranges, markdownTextRange{Start: textNode.Segment.Start, End: textNode.Segment.Stop})
	}
	return ranges
}

func collectMarkdownBlockRanges(node interface{ Lines() *text.Segments }) []markdownTextRange {
	lines := node.Lines()
	if lines == nil {
		return nil
	}
	ranges := make([]markdownTextRange, 0, lines.Len())
	for i := 0; i < lines.Len(); i++ {
		segment := lines.At(i)
		ranges = append(ranges, markdownTextRange{Start: segment.Start, End: segment.Stop})
	}
	return ranges
}

func mergeMarkdownTextRanges(ranges []markdownTextRange) []markdownTextRange {
	if len(ranges) < 2 {
		return ranges
	}
	sort.Slice(ranges, func(i, j int) bool {
		if ranges[i].Start == ranges[j].Start {
			return ranges[i].End < ranges[j].End
		}
		return ranges[i].Start < ranges[j].Start
	})
	merged := make([]markdownTextRange, 0, len(ranges))
	current := ranges[0]
	for i := 1; i < len(ranges); i++ {
		next := ranges[i]
		if next.Start <= current.End {
			if next.End > current.End {
				current.End = next.End
			}
			continue
		}
		merged = append(merged, current)
		current = next
	}
	merged = append(merged, current)
	return merged
}
