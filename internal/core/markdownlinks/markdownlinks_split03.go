package markdownlinks

import (
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/perber/wiki/internal/core/tree"
)

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

func resolveFilesystemPath(sourceFile tree.MarkdownPath, href string) (tree.MarkdownPath, bool) {
	var resolved string
	if strings.HasPrefix(href, "/") {
		resolved = path.Clean(strings.TrimPrefix(href, "/"))
	} else {
		resolved = path.Clean(path.Join(sourceFile.SourceDir().FilesystemPath(), href))
	}
	if resolved == "." {
		resolved = ""
	}
	escaped := resolved == ".." || strings.HasPrefix(resolved, "../")
	return tree.CleanMarkdownPath(resolved), escaped
}

func formatHref(sourceFile tree.MarkdownPath, targetPath tree.MarkdownPath, page bool, absolute bool, suffix string) string {
	targetPath = targetPath.Clean()
	targetPathString := targetPath.FilesystemPath()
	if targetPath == "" && !page {
		return "/" + suffix
	}
	if absolute {
		if targetPath == "" {
			return "/" + suffix
		}
		return "/" + targetPathString + suffix
	}
	rel, err := relMarkdownLinkPath(filepath.FromSlash(sourceFile.SourceDir().FilesystemPath()), filepath.FromSlash(targetPathString))
	if err != nil {
		rel = targetPathString
	}
	rel = filepath.ToSlash(rel)
	if rel == "." && !page {
		rel = "."
	}
	return rel + suffix
}

func formatCanonicalHref(sourceFile tree.MarkdownPath, targetPath tree.MarkdownPath, page bool, absolute bool, suffix string, originalBase string) string {
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

func (idx *Index) formatCanonicalHref(sourceFile tree.MarkdownPath, targetPath tree.MarkdownPath, page bool, absolute bool, suffix string, originalBase string) string {
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
