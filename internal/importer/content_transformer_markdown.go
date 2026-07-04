package importer

import (
	"net/url"
	"path"
	"sort"
	"strings"

	"github.com/perber/wiki/internal/core/tree"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

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
	type importTargetKey struct {
		targetPath tree.RoutePath
		kind       tree.NodeKind
	}
	seen := make(map[importTargetKey]struct{}, len(values))
	out := make([]importTarget, 0, len(values))
	for _, value := range values {
		if value.targetPath == "" {
			continue
		}
		key := importTargetKey{targetPath: value.targetPath, kind: value.kind}
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
