package importer

import (
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/perber/wiki/internal/core/markdownlinks"
	"github.com/perber/wiki/internal/core/shared"
	"github.com/perber/wiki/internal/core/tree"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

type importTarget struct {
	targetPath tree.RoutePath
	kind       tree.NodeKind
}

type assetUploadKey struct {
	pageID    tree.PageID
	assetPath string
}

type contentTransformer struct {
	sourceBasePath         string
	assetMaxBytes          shared.MaxBytes
	markdownLinkRootPrefix string
	slugger                *tree.SlugService
	pagesBySource          map[string]importTarget
	pagesByBasename        map[string][]importTarget
	pagesBySuffix          map[string][]importTarget
	assetUploads           map[assetUploadKey]string
}

type ContentTransformerOptions struct {
	MarkdownLinkRootPrefix string
}

var importerMarkdownParser = goldmark.New()

var (
	importerOpenAsset   = os.Open
	importerFilepathAbs = filepath.Abs
	importerFilepathRel = filepath.Rel
)

// newContentTransformer precomputes source->target lookups from the import plan.
// We resolve links against planned imports so we only rewrite destinations we can actually create.
func newContentTransformer(plan *PlanResult, sourceBasePath string, assetMaxBytes shared.MaxBytes) *contentTransformer {
	return newContentTransformerWithOptions(plan, sourceBasePath, assetMaxBytes, ContentTransformerOptions{})
}

func newContentTransformerWithOptions(plan *PlanResult, sourceBasePath string, assetMaxBytes shared.MaxBytes, opts ContentTransformerOptions) *contentTransformer {
	pagesBySource := make(map[string]importTarget, len(plan.Items))
	pagesByBasename := make(map[string][]importTarget, len(plan.Items))
	pagesBySuffix := make(map[string][]importTarget, len(plan.Items))
	for _, item := range plan.Items {
		normalizedSource := normalizePlanSourcePath(item.SourcePath.FilesystemPath())
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
		sourceBasePath:         sourceBasePath,
		assetMaxBytes:          assetMaxBytes,
		markdownLinkRootPrefix: strings.TrimRight(strings.TrimSpace(opts.MarkdownLinkRootPrefix), "/"),
		slugger:                tree.NewSlugService(),
		pagesBySource:          pagesBySource,
		pagesByBasename:        pagesByBasename,
		pagesBySuffix:          pagesBySuffix,
		assetUploads:           map[assetUploadKey]string{},
	}
}

// TransformContent rewrites Markdown links, wiki links, and asset references for one imported page.
// Rewrites only happen outside inline code, fenced blocks, and indented code so examples remain untouched.
func (t *contentTransformer) TransformContent(
	userID tree.UserID,
	sourcePath tree.WorkspaceSourcePath,
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
	userID tree.UserID,
	sourcePath tree.WorkspaceSourcePath,
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
	userID tree.UserID,
	sourcePath tree.WorkspaceSourcePath,
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
	userID tree.UserID,
	sourcePath tree.WorkspaceSourcePath,
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
