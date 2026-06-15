package markdownlinks

import (
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/perber/wiki/internal/core/tree"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

type EntryKind string

const (
	EntryKindPage    EntryKind = "page"
	EntryKindSection EntryKind = "section"
	EntryKindAsset   EntryKind = "asset"
)

type TargetKind string

const (
	TargetKindPage       TargetKind = "page"
	TargetKindSection    TargetKind = "section"
	TargetKindAsset      TargetKind = "asset"
	TargetKindExternal   TargetKind = "external"
	TargetKindUnresolved TargetKind = "unresolved"
	TargetKindInvalid    TargetKind = "invalid"
)

type Entry struct {
	Kind        EntryKind
	Path        string
	ContentPath string
}

type Resolution struct {
	Kind          TargetKind
	CanonicalHref string
	RoutePath     string
	Code          string
}

type Issue struct {
	Code        string
	Destination string
}

type InlineScanOptions struct {
	IncludeImages    bool
	IgnoreCodeRanges bool
}

type InlineDestination struct {
	Destination string
	Start       int
	End         int
	Image       bool
}

var markdownParser = goldmark.New()

type RewriteResult struct {
	Content string
	Changed bool
	Issues  []Issue
}

type Index struct {
	pages                  map[string]pageEntry
	sourcePages            map[string]pageEntry
	sections               map[string]string
	sectionFiles           map[string]string
	assets                 map[string]struct{}
	markdownLinkRootPrefix string
}

type pageEntry struct {
	CanonicalPath string
	RoutePath     string
}

type resolveMode int

const (
	resolveCanonical resolveMode = iota
	resolveMigration
)

func NewIndex(entries []Entry) *Index {
	return NewIndexWithOptions(entries, Options{})
}

func NewIndexWithOptions(entries []Entry, opts Options) *Index {
	prefix, _ := normalizeMarkdownLinkRootPrefix(opts.MarkdownLinkRootPrefix)
	idx := &Index{
		pages:                  map[string]pageEntry{},
		sourcePages:            map[string]pageEntry{},
		sections:               map[string]string{},
		sectionFiles:           map[string]string{},
		assets:                 map[string]struct{}{},
		markdownLinkRootPrefix: prefix,
	}
	for _, entry := range entries {
		entryPath := cleanRelPath(entry.Path)
		switch entry.Kind {
		case EntryKindPage:
			if entryPath == "" {
				continue
			}
			if strings.EqualFold(path.Ext(entryPath), ".md") {
				routePath := strings.TrimSuffix(entryPath, path.Ext(entryPath))
				page := pageEntry{CanonicalPath: entryPath, RoutePath: routePath}
				idx.pages[routePath] = page
				contentPath := cleanRelPath(entry.ContentPath)
				if contentPath != "" && strings.EqualFold(path.Ext(contentPath), ".md") {
					sourceRoutePath := strings.TrimSuffix(contentPath, path.Ext(contentPath))
					if _, exists := idx.sourcePages[sourceRoutePath]; !exists {
						idx.sourcePages[sourceRoutePath] = page
					}
				}
			}
		case EntryKindSection:
			idx.sections[strings.Trim(entryPath, "/")] = strings.Trim(entryPath, "/")
			contentPath := cleanRelPath(entry.ContentPath)
			if contentPath != "" {
				idx.sectionFiles[contentPath] = strings.Trim(entryPath, "/")
			}
		case EntryKindAsset:
			if entryPath == "" {
				continue
			}
			idx.assets[entryPath] = struct{}{}
		}
	}
	idx.sections[""] = ""
	return idx
}

func NewIndexFromRoot(rootDir string) (*Index, error) {
	return NewIndexFromRootWithOptions(rootDir, Options{})
}

func NewIndexFromRootWithOptions(rootDir string, opts Options) (*Index, error) {
	entries := []Entry{{Kind: EntryKindSection, Path: ""}}
	err := filepath.WalkDir(rootDir, func(filePath string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if filePath == rootDir {
			return nil
		}
		relPath, err := filepath.Rel(rootDir, filePath)
		if err != nil {
			return err
		}
		relPath = filepath.ToSlash(relPath)
		if entry.IsDir() {
			if strings.HasPrefix(entry.Name(), ".") {
				return filepath.SkipDir
			}
			route, err := tree.MapWorkspaceMarkdownRoute(rootDir, relPath, true)
			if err != nil {
				return filepath.SkipDir
			}
			if route.Skip {
				return filepath.SkipDir
			}
			entries = append(entries, Entry{Kind: EntryKindSection, Path: route.RoutePath})
			return nil
		}
		if !strings.EqualFold(filepath.Ext(entry.Name()), ".md") {
			return nil
		}
		route, err := tree.MapWorkspaceMarkdownRoute(rootDir, relPath, false)
		if err != nil || route.Skip {
			return nil
		}
		if route.Kind == tree.NodeKindSection {
			entries = append(entries, Entry{Kind: EntryKindSection, Path: route.RoutePath, ContentPath: relPath})
			return nil
		}
		entries = append(entries, Entry{Kind: EntryKindPage, Path: route.RoutePath + ".md", ContentPath: relPath})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return NewIndexWithOptions(entries, opts), nil
}

func (idx *Index) Resolve(sourceFile string, href string) Resolution {
	return idx.resolve(sourceFile, href, resolveCanonical)
}

func (idx *Index) ResolveForMigration(sourceFile string, href string) Resolution {
	return idx.resolve(sourceFile, href, resolveMigration)
}

func (idx *Index) resolve(sourceFile string, href string, mode resolveMode) Resolution {
	raw := strings.TrimSpace(href)
	if isExternal(raw) {
		return Resolution{Kind: TargetKindExternal, CanonicalHref: href}
	}
	base, suffix := splitURLSuffix(raw)
	if base == "" {
		return Resolution{Kind: TargetKindUnresolved, Code: "empty"}
	}
	resolveBase := idx.stripMarkdownLinkRootPrefix(base)
	if isAsset(resolveBase) {
		return Resolution{Kind: TargetKindAsset, CanonicalHref: href}
	}
	hasTrailingSlash := len(base) > 1 && strings.HasSuffix(strings.TrimRight(base, " \t\r\n"), "/")

	decodedBase, err := url.PathUnescape(resolveBase)
	if err != nil {
		return Resolution{Kind: TargetKindInvalid, CanonicalHref: href, Code: "invalid_percent_encoding"}
	}
	targetPath, escaped := resolveFilesystemPath(sourceFile, decodedBase)
	if escaped {
		return Resolution{Kind: TargetKindInvalid, CanonicalHref: href, Code: "workspace_escape"}
	}
	targetPath = strings.Trim(targetPath, "/")
	if targetPath == "." {
		targetPath = ""
	}

	isAbsolute := strings.HasPrefix(base, "/")
	targetRoute := strings.TrimSuffix(targetPath, path.Ext(targetPath))
	if strings.EqualFold(path.Ext(targetPath), ".md") {
		if sectionPath, ok := idx.sectionFiles[targetPath]; ok {
			return Resolution{
				Kind:          TargetKindSection,
				CanonicalHref: idx.formatCanonicalHref(sourceFile, sectionPath, false, isAbsolute, suffix, base),
				RoutePath:     sectionPath,
			}
		}
		if page, ok := idx.pages[targetRoute]; ok {
			return Resolution{
				Kind:          TargetKindPage,
				CanonicalHref: idx.formatCanonicalHref(sourceFile, page.CanonicalPath, true, isAbsolute, suffix, base),
				RoutePath:     page.RoutePath,
			}
		}
		if page, ok := idx.sourcePages[targetRoute]; ok {
			canonicalHref := idx.formatCanonicalHref(sourceFile, page.CanonicalPath, true, isAbsolute, suffix, base)
			if mode == resolveMigration {
				return Resolution{
					Kind:          TargetKindPage,
					CanonicalHref: canonicalHref,
					RoutePath:     page.RoutePath,
				}
			}
			return Resolution{
				Kind:          TargetKindUnresolved,
				CanonicalHref: canonicalHref,
				RoutePath:     page.RoutePath,
				Code:          "non_canonical_markdown_path",
			}
		}
		return Resolution{Kind: TargetKindUnresolved, CanonicalHref: href, RoutePath: targetRoute, Code: "broken_page"}
	}

	page, hasPage := idx.pages[targetPath]
	_, hasSection := idx.sections[strings.TrimRight(targetPath, "/")]
	if hasTrailingSlash {
		if hasSection {
			sectionPath := strings.TrimRight(targetPath, "/")
			return Resolution{
				Kind:          TargetKindSection,
				CanonicalHref: idx.formatCanonicalHref(sourceFile, sectionPath, false, isAbsolute, suffix, base),
				RoutePath:     sectionPath,
			}
		}
		return Resolution{Kind: TargetKindUnresolved, CanonicalHref: href, RoutePath: targetPath, Code: "broken_link"}
	}
	switch {
	case hasPage && !hasSection:
		return Resolution{
			Kind:          TargetKindPage,
			CanonicalHref: idx.formatCanonicalHref(sourceFile, page.CanonicalPath, true, isAbsolute, suffix, base),
			RoutePath:     page.RoutePath,
		}
	case hasSection && !hasPage:
		sectionPath := strings.TrimRight(targetPath, "/")
		return Resolution{
			Kind:          TargetKindSection,
			CanonicalHref: idx.formatCanonicalHref(sourceFile, sectionPath, false, isAbsolute, suffix, base),
			RoutePath:     sectionPath,
		}
	case hasPage && hasSection:
		if mode == resolveCanonical {
			sectionPath := strings.TrimRight(targetPath, "/")
			return Resolution{
				Kind:          TargetKindSection,
				CanonicalHref: idx.formatCanonicalHref(sourceFile, sectionPath, false, isAbsolute, suffix, base),
				RoutePath:     sectionPath,
			}
		}
		return Resolution{Kind: TargetKindUnresolved, CanonicalHref: href, RoutePath: targetPath, Code: "ambiguous_legacy_link"}
	default:
		return Resolution{Kind: TargetKindUnresolved, CanonicalHref: href, RoutePath: targetPath, Code: "broken_link"}
	}
}

func (idx *Index) RewriteMarkdown(sourceFile string, content string) RewriteResult {
	if content == "" {
		return RewriteResult{Content: content}
	}

	excluded := excludedCodeRanges(content)
	occurrences := scanInlineLinks(content, excluded)
	occurrences = append(occurrences, scanReferenceDefinitions(content, excluded, referenceLabelUsage(content))...)
	replacements := make([]replacement, 0, len(occurrences))
	issues := []Issue{}
	for _, occurrence := range occurrences {
		resolved := idx.ResolveForMigration(sourceFile, occurrence.Href)
		switch resolved.Kind {
		case TargetKindPage, TargetKindSection:
			if resolved.CanonicalHref != occurrence.Href {
				replacements = append(replacements, replacement{
					Start: occurrence.Start,
					End:   occurrence.End,
					Value: resolved.CanonicalHref,
				})
			}
		case TargetKindInvalid, TargetKindUnresolved:
			issues = append(issues, Issue{
				Code:        resolved.Code,
				Destination: occurrence.Href,
			})
		}
	}
	if len(replacements) == 0 {
		return RewriteResult{Content: content, Issues: issues}
	}
	rewritten, changed := applyReplacements(content, replacements)
	if !changed {
		return RewriteResult{Content: content, Issues: issues}
	}
	return RewriteResult{
		Content: rewritten,
		Changed: true,
		Issues:  issues,
	}
}

type linkOccurrence struct {
	Href    string
	Start   int
	End     int
	FullEnd int
	Label   string
}

type replacement struct {
	Start int
	End   int
	Value string
}

type textRange struct {
	Start int
	End   int
}

func excludedCodeRanges(content string) []textRange {
	var ranges []textRange
	reader := text.NewReader([]byte(content))
	doc := markdownParser.Parser().Parse(reader)
	_ = ast.Walk(doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch n := node.(type) {
		case *ast.CodeSpan:
			ranges = append(ranges, collectTextNodeRanges(n)...)
		case *ast.FencedCodeBlock:
			ranges = append(ranges, collectBlockRanges(n)...)
		case *ast.CodeBlock:
			ranges = append(ranges, collectBlockRanges(n)...)
		}
		return ast.WalkContinue, nil
	})

	for i := 0; i < len(content); i++ {
		if content[i] != '`' || offsetInRanges(i, ranges) {
			continue
		}
		runLen := backtickRunLength(content, i)
		end := findClosingBacktickRun(content, i+runLen, runLen, ranges)
		if end < 0 {
			continue
		}
		ranges = append(ranges, textRange{Start: i, End: end + runLen})
		i = end + runLen - 1
	}
	return mergeRanges(ranges)
}

func collectTextNodeRanges(parent ast.Node) []textRange {
	var ranges []textRange
	for child := parent.FirstChild(); child != nil; child = child.NextSibling() {
		textNode, ok := child.(*ast.Text)
		if !ok {
			continue
		}
		ranges = append(ranges, textRange{Start: textNode.Segment.Start, End: textNode.Segment.Stop})
	}
	return ranges
}

func collectBlockRanges(node interface{ Lines() *text.Segments }) []textRange {
	lines := node.Lines()
	if lines == nil {
		return nil
	}
	ranges := make([]textRange, 0, lines.Len())
	for i := 0; i < lines.Len(); i++ {
		segment := lines.At(i)
		ranges = append(ranges, textRange{Start: segment.Start, End: segment.Stop})
	}
	return ranges
}

func backtickRunLength(content string, start int) int {
	i := start
	for i < len(content) && content[i] == '`' {
		i++
	}
	return i - start
}

func findClosingBacktickRun(content string, start int, runLen int, excluded []textRange) int {
	for i := start; i < len(content); i++ {
		if content[i] != '`' || offsetInRanges(i, excluded) {
			continue
		}
		currentRunLen := backtickRunLength(content, i)
		if currentRunLen == runLen {
			return i
		}
		i += currentRunLen - 1
	}
	return -1
}

func scanInlineLinks(content string, excluded []textRange) []linkOccurrence {
	destinations := scanInlineDestinations(content, excluded, InlineScanOptions{})
	occurrences := make([]linkOccurrence, 0, len(destinations))
	for _, destination := range destinations {
		occurrences = append(occurrences, linkOccurrence{
			Href:  destination.Destination,
			Start: destination.Start,
			End:   destination.End,
		})
	}
	return occurrences
}

func ScanInlineDestinations(content string, opts InlineScanOptions) []InlineDestination {
	var excluded []textRange
	if !opts.IgnoreCodeRanges {
		excluded = excludedCodeRanges(content)
	}
	return scanInlineDestinations(content, excluded, opts)
}

func scanInlineDestinations(content string, excluded []textRange, opts InlineScanOptions) []InlineDestination {
	var occurrences []InlineDestination
	for i := 0; i < len(content); i++ {
		if offsetInRanges(i, excluded) || content[i] != '[' {
			continue
		}
		if isEscapedMarkdownBracket(content, i) {
			continue
		}
		isImage := i > 0 && content[i-1] == '!'
		labelEnd := findClosingBracket(content, i)
		if labelEnd < 0 {
			continue
		}
		j := labelEnd + 1
		if j >= len(content) || content[j] != '(' {
			continue
		}
		occurrence, ok := parseDestination(content, j+1)
		if !ok {
			continue
		}
		if isImage && !opts.IncludeImages {
			i = occurrence.FullEnd - 1
			continue
		}
		occurrences = append(occurrences, InlineDestination{
			Destination: occurrence.Href,
			Start:       occurrence.Start,
			End:         occurrence.End,
			Image:       isImage,
		})
		i = occurrence.FullEnd - 1
	}
	return occurrences
}

type referenceUsage struct {
	linkLabels  map[string]struct{}
	imageLabels map[string]struct{}
}

func referenceLabelUsage(content string) referenceUsage {
	usage := referenceUsage{
		linkLabels:  map[string]struct{}{},
		imageLabels: map[string]struct{}{},
	}
	reader := text.NewReader([]byte(content))
	doc := markdownParser.Parser().Parse(reader)
	_ = ast.Walk(doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch n := node.(type) {
		case *ast.Link:
			if n.Reference != nil {
				usage.linkLabels[normalizeReferenceLabel(n.Reference.Value)] = struct{}{}
			}
		case *ast.Image:
			if n.Reference != nil {
				usage.imageLabels[normalizeReferenceLabel(n.Reference.Value)] = struct{}{}
			}
		}
		return ast.WalkContinue, nil
	})
	return usage
}

func normalizeReferenceLabel(label []byte) string {
	return util.ToLinkReference(label)
}

func (usage referenceUsage) imageOnly(label string) bool {
	if label == "" {
		return false
	}
	if _, ok := usage.imageLabels[label]; !ok {
		return false
	}
	_, usedByLink := usage.linkLabels[label]
	return !usedByLink
}

func scanReferenceDefinitions(content string, excluded []textRange, usage referenceUsage) []linkOccurrence {
	var occurrences []linkOccurrence
	lineStart := 0
	for lineStart < len(content) {
		lineEnd := strings.IndexByte(content[lineStart:], '\n')
		if lineEnd == -1 {
			lineEnd = len(content)
		} else {
			lineEnd += lineStart
		}
		if !offsetInRanges(lineStart, excluded) {
			if occurrence, ok := parseReferenceDefinitionLine(content, lineStart, lineEnd); ok {
				if usage.imageOnly(occurrence.Label) {
					if lineEnd == len(content) {
						break
					}
					lineStart = lineEnd + 1
					continue
				}
				occurrences = append(occurrences, occurrence)
			}
		}
		if lineEnd == len(content) {
			break
		}
		lineStart = lineEnd + 1
	}
	return occurrences
}

func parseReferenceDefinitionLine(content string, lineStart int, lineEnd int) (linkOccurrence, bool) {
	i := lineStart
	spaces := 0
	for i < lineEnd && content[i] == ' ' && spaces < 4 {
		i++
		spaces++
	}
	if spaces > 3 || i >= lineEnd || content[i] != '[' || isEscapedMarkdownBracket(content, i) {
		return linkOccurrence{}, false
	}
	labelEnd := findClosingBracket(content, i)
	if labelEnd < 0 || labelEnd+1 >= lineEnd || content[labelEnd+1] != ':' {
		return linkOccurrence{}, false
	}
	label := normalizeReferenceLabel([]byte(content[i+1 : labelEnd]))
	i = labelEnd + 2
	for i < lineEnd && (content[i] == ' ' || content[i] == '\t') {
		i++
	}
	if i >= lineEnd {
		return linkOccurrence{}, false
	}
	occurrence, ok := parseReferenceDestination(content, i, lineEnd)
	if !ok {
		return linkOccurrence{}, false
	}
	occurrence.Label = label
	return occurrence, true
}

func parseReferenceDestination(content string, start int, lineEnd int) (linkOccurrence, bool) {
	i := start
	if i >= lineEnd {
		return linkOccurrence{}, false
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
	if destEnd <= destStart {
		return linkOccurrence{}, false
	}
	return linkOccurrence{Href: content[destStart:destEnd], Start: destStart, End: destEnd}, true
}

func parseDestination(content string, start int) (linkOccurrence, bool) {
	i := start
	for i < len(content) && (content[i] == ' ' || content[i] == '\t' || content[i] == '\n') {
		i++
	}
	if i >= len(content) {
		return linkOccurrence{}, false
	}

	destStart := i
	destEnd := -1
	afterDest := -1
	if content[i] == '<' {
		i++
		destStart = i
		for i < len(content) {
			if content[i] == '>' {
				destEnd = i
				afterDest = i + 1
				break
			}
			if content[i] == '\\' {
				i++
			}
			i++
		}
	} else {
		depth := 0
		for i < len(content) {
			switch content[i] {
			case '\\':
				i += 2
				continue
			case '(':
				depth++
			case ')':
				if depth == 0 {
					destEnd = i
					afterDest = i
					goto done
				}
				depth--
			case ' ', '\t', '\n':
				destEnd = i
				afterDest = i
				goto done
			}
			i++
		}
	}

done:
	fullEnd, ok := inlineLinkTailEnd(content, afterDest)
	if destEnd <= destStart || !ok {
		return linkOccurrence{}, false
	}
	return linkOccurrence{Href: content[destStart:destEnd], Start: destStart, End: destEnd, FullEnd: fullEnd}, true
}

func inlineLinkTailEnd(content string, start int) (int, bool) {
	i := skipInlineWhitespace(content, start)
	if i >= len(content) {
		return 0, false
	}
	if content[i] == ')' {
		return i + 1, true
	}
	titleEnd := parseInlineLinkTitleEnd(content, i)
	if titleEnd < 0 {
		return 0, false
	}
	i = skipInlineWhitespace(content, titleEnd)
	if i < len(content) && content[i] == ')' {
		return i + 1, true
	}
	return 0, false
}

func skipInlineWhitespace(content string, start int) int {
	i := start
	for i < len(content) && (content[i] == ' ' || content[i] == '\t' || content[i] == '\n') {
		i++
	}
	return i
}

func parseInlineLinkTitleEnd(content string, start int) int {
	switch content[start] {
	case '"', '\'':
		quote := content[start]
		for i := start + 1; i < len(content); i++ {
			if content[i] == '\\' {
				i++
				continue
			}
			if content[i] == quote {
				return i + 1
			}
		}
	case '(':
		depth := 1
		for i := start + 1; i < len(content); i++ {
			switch content[i] {
			case '\\':
				i++
			case '(':
				depth++
			case ')':
				depth--
				if depth == 0 {
					return i + 1
				}
			}
		}
	}
	return -1
}

func findClosingBracket(content string, start int) int {
	depth := 0
	for i := start; i < len(content); i++ {
		switch content[i] {
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				return i
			}
		case '\\':
			i++
		}
	}
	return -1
}

func offsetInRanges(offset int, ranges []textRange) bool {
	for _, current := range ranges {
		if offset >= current.Start && offset < current.End {
			return true
		}
		if offset < current.Start {
			return false
		}
	}
	return false
}

func mergeRanges(ranges []textRange) []textRange {
	if len(ranges) < 2 {
		return ranges
	}
	for i := 1; i < len(ranges); i++ {
		current := ranges[i]
		j := i - 1
		for j >= 0 && ranges[j].Start > current.Start {
			ranges[j+1] = ranges[j]
			j--
		}
		ranges[j+1] = current
	}
	merged := []textRange{ranges[0]}
	for _, current := range ranges[1:] {
		last := &merged[len(merged)-1]
		if current.Start <= last.End {
			if current.End > last.End {
				last.End = current.End
			}
			continue
		}
		merged = append(merged, current)
	}
	return merged
}

func applyReplacements(content string, replacements []replacement) (string, bool) {
	if len(replacements) == 0 {
		return content, false
	}
	sort.SliceStable(replacements, func(i, j int) bool {
		if replacements[i].Start == replacements[j].Start {
			return replacements[i].End < replacements[j].End
		}
		return replacements[i].Start < replacements[j].Start
	})
	var out strings.Builder
	last := 0
	for _, replacement := range replacements {
		if replacement.Start < last || replacement.Start < 0 || replacement.End < replacement.Start || replacement.End > len(content) {
			continue
		}
		out.WriteString(content[last:replacement.Start])
		out.WriteString(replacement.Value)
		last = replacement.End
	}
	out.WriteString(content[last:])
	rewritten := out.String()
	return rewritten, rewritten != content
}

func cleanRelPath(value string) string {
	cleaned := path.Clean(strings.Trim(strings.TrimSpace(value), "/"))
	if cleaned == "." {
		return ""
	}
	return cleaned
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

func isEscapedMarkdownBracket(content string, offset int) bool {
	backslashes := 0
	for i := offset - 1; i >= 0 && content[i] == '\\'; i-- {
		backslashes++
	}
	return backslashes%2 == 1
}

func isExternal(href string) bool {
	lower := strings.ToLower(strings.TrimSpace(href))
	if strings.HasPrefix(lower, "//") {
		return true
	}
	if idx := strings.IndexAny(lower, ":/?#"); idx >= 0 && lower[idx] == ':' {
		return true
	}
	return strings.HasPrefix(lower, "http://") ||
		strings.HasPrefix(lower, "https://") ||
		strings.HasPrefix(lower, "mailto:") ||
		strings.HasPrefix(lower, "#")
}

func isAsset(href string) bool {
	trimmed := strings.TrimPrefix(strings.TrimSpace(href), "/")
	if strings.HasPrefix(trimmed, "assets/") {
		return true
	}
	ext := strings.ToLower(path.Ext(trimmed))
	if ext == "." {
		return false
	}
	return ext != "" && ext != ".md" && ext != ".markdown"
}

func resolveFilesystemPath(sourceFile string, href string) (string, bool) {
	var resolved string
	if strings.HasPrefix(href, "/") {
		resolved = path.Clean(strings.TrimPrefix(href, "/"))
	} else {
		sourceDir := path.Dir(cleanRelPath(sourceFile))
		if sourceDir == "." {
			sourceDir = ""
		}
		resolved = path.Clean(path.Join(sourceDir, href))
	}
	if resolved == "." {
		resolved = ""
	}
	return resolved, resolved == ".." || strings.HasPrefix(resolved, "../")
}

func formatHref(sourceFile string, targetPath string, page bool, absolute bool, suffix string) string {
	targetPath = cleanRelPath(targetPath)
	if targetPath == "" && !page {
		return "/" + suffix
	}
	if absolute {
		if targetPath == "" {
			return "/" + suffix
		}
		return "/" + targetPath + suffix
	}
	sourceDir := path.Dir(cleanRelPath(sourceFile))
	if sourceDir == "." {
		sourceDir = ""
	}
	rel, err := filepath.Rel(filepath.FromSlash(sourceDir), filepath.FromSlash(targetPath))
	if err != nil {
		rel = targetPath
	}
	rel = filepath.ToSlash(rel)
	if rel == "." && !page {
		rel = "."
	}
	return rel + suffix
}

func formatCanonicalHref(sourceFile string, targetPath string, page bool, absolute bool, suffix string, originalBase string) string {
	if strings.Contains(originalBase, "%") {
		base := strings.TrimSpace(originalBase)
		if page {
			if !strings.EqualFold(path.Ext(base), ".md") {
				base = strings.TrimRight(base, "/") + ".md"
			}
			return base + suffix
		}
		return encodedSectionHrefBase(base) + suffix
	}
	return formatHref(sourceFile, targetPath, page, absolute, suffix)
}

func (idx *Index) formatCanonicalHref(sourceFile string, targetPath string, page bool, absolute bool, suffix string, originalBase string) string {
	href := formatCanonicalHref(sourceFile, targetPath, page, absolute, suffix, idx.stripMarkdownLinkRootPrefix(originalBase))
	if !absolute || idx.markdownLinkRootPrefix == "" {
		return href
	}
	if href == "/" {
		return idx.markdownLinkRootPrefix + suffix
	}
	if strings.HasPrefix(href, "/?") || strings.HasPrefix(href, "/#") {
		return idx.markdownLinkRootPrefix + strings.TrimPrefix(href, "/")
	}
	if strings.HasPrefix(href, "/") {
		return idx.markdownLinkRootPrefix + href
	}
	return href
}

func (idx *Index) stripMarkdownLinkRootPrefix(base string) string {
	prefix := idx.markdownLinkRootPrefix
	if prefix == "" || !strings.HasPrefix(base, "/") {
		return base
	}
	if base == prefix {
		return "/"
	}
	if strings.HasPrefix(base, prefix+"/") {
		return strings.TrimPrefix(base, prefix)
	}
	return base
}

func encodedSectionHrefBase(base string) string {
	base = strings.TrimSpace(base)
	if base == "/" {
		return base
	}
	base = strings.TrimRight(base, "/")
	switch strings.ToLower(path.Base(base)) {
	case "index.md", "readme.md":
		parent := path.Dir(base)
		if parent == "." {
			return "."
		}
		return parent
	default:
		return base
	}
}
