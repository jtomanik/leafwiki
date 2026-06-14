package pages

import (
	"fmt"
	"strings"

	"github.com/perber/wiki/internal/core/markdown"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
)

type MetadataPatch struct {
	SetTags          []string
	AddTags          []string
	RemoveTags       []string
	SetProperties    map[string]string
	RemoveProperties []string
}

type sectionHeading struct {
	Level int
	Text  string
	Line  int
	Path  []string
}

func ReplaceMarkdownSection(content string, headingPath []string, occurrence int, replacement string) (string, error) {
	targetPath := normalizeHeadingPath(headingPath)
	if len(targetPath) == 0 {
		return "", fmt.Errorf("headingPath is required")
	}
	lines := strings.Split(content, "\n")
	headings := markdownHeadings(lines)
	matches := matchingHeadings(headings, targetPath)
	if len(matches) == 0 {
		return "", fmt.Errorf("heading_not_found: heading path not found")
	}
	var target sectionHeading
	switch {
	case occurrence > 0:
		if occurrence > len(matches) {
			return "", fmt.Errorf("heading_not_found: occurrence not found")
		}
		target = matches[occurrence-1]
	case len(matches) > 1:
		return "", fmt.Errorf("ambiguous_heading: occurrence is required")
	default:
		target = matches[0]
	}

	end := len(lines)
	for _, heading := range headings {
		if heading.Line > target.Line && heading.Level <= target.Level {
			end = heading.Line
			break
		}
	}

	replacementLines := strings.Split(strings.TrimRight(replacement, "\n"), "\n")
	if len(replacementLines) == 1 && replacementLines[0] == "" {
		replacementLines = []string{}
	}
	start := target.Line + 1
	if firstReplacementHeadingLevel(replacementLines) == target.Level {
		start = target.Line
	}
	next := make([]string, 0, len(lines)-(end-start)+len(replacementLines))
	next = append(next, lines[:start]...)
	next = append(next, replacementLines...)
	next = append(next, lines[end:]...)
	return strings.Join(next, "\n"), nil
}

func markdownHeadings(lines []string) []sectionHeading {
	headings := []sectionHeading{}
	stack := []sectionHeading{}
	inFence := false
	fenceMarker := rune(0)
	fenceLength := 0
	for index, line := range lines {
		if marker, length, ok := parseMarkdownFence(line); ok {
			if !inFence {
				inFence = true
				fenceMarker = marker
				fenceLength = length
			} else if marker == fenceMarker && length >= fenceLength {
				inFence = false
				fenceMarker = 0
				fenceLength = 0
			}
			continue
		}
		if inFence {
			continue
		}
		level, text, ok := parseMarkdownHeading(line)
		if !ok {
			continue
		}
		for len(stack) > 0 && stack[len(stack)-1].Level >= level {
			stack = stack[:len(stack)-1]
		}
		path := make([]string, 0, len(stack)+1)
		for _, parent := range stack {
			path = append(path, parent.Text)
		}
		path = append(path, text)
		heading := sectionHeading{Level: level, Text: text, Line: index, Path: path}
		stack = append(stack, heading)
		headings = append(headings, heading)
	}
	return headings
}

func parseMarkdownFence(line string) (rune, int, bool) {
	if markdownIndentColumns(line) >= 4 {
		return 0, 0, false
	}
	trimmed := strings.TrimLeft(line, " \t")
	if trimmed == "" {
		return 0, 0, false
	}
	marker := rune(trimmed[0])
	if marker != '`' && marker != '~' {
		return 0, 0, false
	}
	length := 0
	for _, ch := range trimmed {
		if ch != marker {
			break
		}
		length++
	}
	if length < 3 {
		return 0, 0, false
	}
	return marker, length, true
}

func parseMarkdownHeading(line string) (int, string, bool) {
	if markdownIndentColumns(line) >= 4 {
		return 0, "", false
	}
	trimmed := strings.TrimLeft(line, " \t")
	level := 0
	for level < len(trimmed) && trimmed[level] == '#' {
		level++
	}
	if level == 0 || level > 6 || level >= len(trimmed) || trimmed[level] != ' ' {
		return 0, "", false
	}
	text := strings.TrimSpace(trimmed[level+1:])
	text = strings.TrimRight(text, "#")
	text = strings.TrimSpace(text)
	if text == "" {
		return 0, "", false
	}
	return level, text, true
}

func markdownIndentColumns(line string) int {
	columns := 0
	for _, ch := range line {
		switch ch {
		case ' ':
			columns++
		case '\t':
			columns += 4 - columns%4
		default:
			return columns
		}
		if columns >= 4 {
			return columns
		}
	}
	return columns
}

func matchingHeadings(headings []sectionHeading, path []string) []sectionHeading {
	matches := []sectionHeading{}
	for _, heading := range headings {
		if headingPathMatches(heading.Path, path) {
			matches = append(matches, heading)
		}
	}
	return matches
}

func headingPathMatches(candidate []string, target []string) bool {
	if len(target) > len(candidate) {
		return false
	}
	offset := len(candidate) - len(target)
	for i := range target {
		if !strings.EqualFold(candidate[offset+i], target[i]) {
			return false
		}
	}
	return true
}

func normalizeHeadingPath(path []string) []string {
	out := make([]string, 0, len(path))
	for _, part := range path {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func firstReplacementHeadingLevel(lines []string) int {
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		level, _, ok := parseMarkdownHeading(line)
		if !ok {
			return 0
		}
		return level
	}
	return 0
}

func ApplyMetadataPatch(currentTags []string, currentProperties map[string]string, patch MetadataPatch) ([]string, map[string]string, error) {
	tags := normalizeTagInputs(currentTags)
	if patch.SetTags != nil {
		tags = normalizeTagInputs(patch.SetTags)
	} else {
		remove := tagSet(normalizeTagInputs(patch.RemoveTags))
		next := make([]string, 0, len(tags)+len(patch.AddTags))
		seen := map[string]struct{}{}
		for _, tag := range tags {
			if _, drop := remove[tag]; drop {
				continue
			}
			seen[tag] = struct{}{}
			next = append(next, tag)
		}
		for _, tag := range normalizeTagInputs(patch.AddTags) {
			if _, exists := seen[tag]; exists {
				continue
			}
			seen[tag] = struct{}{}
			next = append(next, tag)
		}
		tags = next
	}

	properties := make(map[string]string, len(currentProperties)+len(patch.SetProperties))
	for key, value := range currentProperties {
		properties[key] = value
	}
	if err := validatePatchPropertyKeys(patch.RemoveProperties); err != nil {
		return nil, nil, err
	}
	for _, key := range patch.RemoveProperties {
		delete(properties, strings.TrimSpace(key))
	}
	for key, value := range patch.SetProperties {
		properties[key] = value
	}
	if err := ValidatePageMetadataInput(tags, properties); err != nil {
		return nil, nil, err
	}
	return tags, properties, nil
}

func tagSet(tags []string) map[string]struct{} {
	out := make(map[string]struct{}, len(tags))
	for _, tag := range tags {
		out[tag] = struct{}{}
	}
	return out
}

func validatePatchPropertyKeys(keys []string) error {
	ve := sharederrors.NewValidationErrors()
	for _, rawKey := range keys {
		key := strings.TrimSpace(rawKey)
		field := "removeProperties." + rawKey
		switch {
		case key == "":
			ve.Add(field, "Property key must not be empty")
		case key != rawKey:
			ve.Add(field, "Property key must not contain leading or trailing whitespace")
		case markdown.IsReservedMetadataKey(key):
			ve.Add(field, "Property key uses a reserved prefix")
		case strings.ToLower(key) == "tags" || strings.ToLower(key) == "title":
			ve.Add(field, "Property key is reserved")
		}
	}
	if ve.HasErrors() {
		return ve
	}
	return nil
}
