package markdownlinks

import (
	"strings"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

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
