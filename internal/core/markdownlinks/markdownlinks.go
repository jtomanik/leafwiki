package markdownlinks

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/perber/wiki/internal/core/tree"
	"github.com/yuin/goldmark"
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

type IssueCode string

const (
	IssueCodeEmpty                    IssueCode = "empty"
	IssueCodeInvalidPercentEncoding   IssueCode = "invalid_percent_encoding"
	IssueCodeWorkspaceEscape          IssueCode = "workspace_escape"
	IssueCodeNonCanonicalMarkdownPath IssueCode = "non_canonical_markdown_path"
	IssueCodeBrokenPage               IssueCode = "broken_page"
	IssueCodeBrokenLink               IssueCode = "broken_link"
	IssueCodeAmbiguousLegacyLink      IssueCode = "ambiguous_legacy_link"
)

type Entry struct {
	Kind        EntryKind
	RoutePath   tree.RoutePath
	Path        tree.MarkdownPath
	ContentPath tree.MarkdownPath
}

type Resolution struct {
	Kind          TargetKind
	CanonicalHref string
	RoutePath     tree.RoutePath
	Code          IssueCode
}

type Issue struct {
	Code        IssueCode
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

var (
	mapWorkspaceMarkdownRoute = tree.MapWorkspaceMarkdownRoute
	relMarkdownLinkPath       = filepath.Rel
	walkMarkdownLinkRoot      = filepath.WalkDir
)

type RewriteResult struct {
	Content string
	Changed bool
	Issues  []Issue
}

type Index struct {
	pages                  map[tree.RoutePath]pageEntry
	sourcePages            map[tree.RoutePath]pageEntry
	sections               map[tree.RoutePath]tree.RoutePath
	sectionFiles           map[tree.MarkdownPath]tree.RoutePath
	assets                 map[tree.MarkdownPath]struct{}
	markdownLinkRootPrefix string
}

type pageEntry struct {
	CanonicalPath tree.MarkdownPath
	RoutePath     tree.RoutePath
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
		pages:                  map[tree.RoutePath]pageEntry{},
		sourcePages:            map[tree.RoutePath]pageEntry{},
		sections:               map[tree.RoutePath]tree.RoutePath{},
		sectionFiles:           map[tree.MarkdownPath]tree.RoutePath{},
		assets:                 map[tree.MarkdownPath]struct{}{},
		markdownLinkRootPrefix: prefix,
	}
	for _, entry := range entries {
		entryPath := entry.Path.Clean()
		switch entry.Kind {
		case EntryKindPage:
			routePath := entry.RoutePath
			if routePath == "" && entryPath != "" && entryPath.IsMarkdown() {
				routePath = entryPath.RoutePath()
			}
			if routePath == "" {
				continue
			}
			canonicalPath := entryPath
			if canonicalPath == "" {
				canonicalPath = routePath.MarkdownPagePath()
			}
			if canonicalPath.IsMarkdown() {
				page := pageEntry{CanonicalPath: canonicalPath, RoutePath: routePath}
				idx.pages[routePath] = page
				contentPath := entry.ContentPath.Clean()
				if contentPath != "" && contentPath.IsMarkdown() {
					sourceRoutePath := contentPath.RoutePath()
					if _, exists := idx.sourcePages[sourceRoutePath]; !exists {
						idx.sourcePages[sourceRoutePath] = page
					}
				}
			}
		case EntryKindSection:
			routePath := entry.RoutePath
			if routePath == "" {
				routePath = entryPath.RoutePath()
			}
			idx.sections[routePath] = routePath
			contentPath := entry.ContentPath.Clean()
			if contentPath != "" {
				idx.sectionFiles[contentPath] = routePath
			}
		case EntryKindAsset:
			if entryPath == "" {
				continue
			}
			idx.assets[entryPath] = struct{}{}
		}
	}
	var rootRoutePath tree.RoutePath
	idx.sections[rootRoutePath] = rootRoutePath
	return idx
}

func NewIndexFromRoot(rootDir string) (*Index, error) {
	return NewIndexFromRootWithOptions(rootDir, Options{})
}

func NewIndexFromRootWithOptions(rootDir string, opts Options) (*Index, error) {
	entries := []Entry{{Kind: EntryKindSection}}
	err := walkMarkdownLinkRoot(rootDir, func(filePath string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if filePath == rootDir {
			return nil
		}
		relPath, err := relMarkdownLinkPath(rootDir, filePath)
		if err != nil {
			return err
		}
		relPath = filepath.ToSlash(relPath)
		if entry.IsDir() {
			if strings.HasPrefix(entry.Name(), ".") {
				return filepath.SkipDir
			}
			route, err := mapWorkspaceMarkdownRoute(rootDir, relPath, true)
			if err != nil {
				return filepath.SkipDir
			}
			if route.Skip {
				return filepath.SkipDir
			}
			entries = append(entries, Entry{Kind: EntryKindSection, RoutePath: route.RoutePath})
			return nil
		}
		if !strings.EqualFold(filepath.Ext(entry.Name()), ".md") {
			return nil
		}
		route, err := mapWorkspaceMarkdownRoute(rootDir, relPath, false)
		if err != nil {
			return err
		}
		if route.Skip {
			return nil
		}
		if route.Kind == tree.NodeKindSection {
			entries = append(entries, Entry{Kind: EntryKindSection, RoutePath: route.RoutePath, ContentPath: tree.MarkdownPathFromString(relPath)})
			return nil
		}
		entries = append(entries, Entry{Kind: EntryKindPage, RoutePath: route.RoutePath, ContentPath: tree.MarkdownPathFromString(relPath)})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return NewIndexWithOptions(entries, opts), nil
}

func (idx *Index) Resolve(sourceFile tree.MarkdownPath, href string) Resolution {
	return idx.resolve(sourceFile, href, resolveCanonical)
}

func (idx *Index) ResolveForMigration(sourceFile tree.MarkdownPath, href string) Resolution {
	return idx.resolve(sourceFile, href, resolveMigration)
}

func (idx *Index) resolve(sourceFile tree.MarkdownPath, href string, mode resolveMode) Resolution {
	raw := strings.TrimSpace(href)
	if isExternal(raw) {
		return Resolution{Kind: TargetKindExternal, CanonicalHref: href}
	}
	base, suffix := splitURLSuffix(raw)
	if base == "" {
		return Resolution{Kind: TargetKindUnresolved, Code: IssueCodeEmpty}
	}
	resolveBase := idx.stripMarkdownLinkRootPrefix(base)
	if isAsset(resolveBase) {
		return Resolution{Kind: TargetKindAsset, CanonicalHref: href}
	}
	hasTrailingSlash := len(base) > 1 && strings.HasSuffix(strings.TrimRight(base, " \t\r\n"), "/")

	decodedBase, err := url.PathUnescape(resolveBase)
	if err != nil {
		return Resolution{Kind: TargetKindInvalid, CanonicalHref: href, Code: IssueCodeInvalidPercentEncoding}
	}
	targetPath, escaped := resolveFilesystemPath(sourceFile, decodedBase)
	if escaped {
		return Resolution{Kind: TargetKindInvalid, CanonicalHref: href, Code: IssueCodeWorkspaceEscape}
	}
	targetPath = targetPath.Clean()

	isAbsolute := strings.HasPrefix(base, "/")
	targetRoute := targetPath.RoutePath()
	sectionRoute := targetPath.RoutePath()
	if targetPath.IsMarkdown() {
		if sectionPath, ok := idx.sectionFiles[targetPath]; ok {
			return Resolution{
				Kind:          TargetKindSection,
				CanonicalHref: idx.formatCanonicalHref(sourceFile, sectionPath.HrefPath(), false, isAbsolute, suffix, base),
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
				Code:          IssueCodeNonCanonicalMarkdownPath,
			}
		}
		return Resolution{Kind: TargetKindUnresolved, CanonicalHref: href, RoutePath: targetRoute, Code: IssueCodeBrokenPage}
	}

	page, hasPage := idx.pages[sectionRoute]
	_, hasSection := idx.sections[sectionRoute]
	if hasTrailingSlash {
		if hasSection {
			return Resolution{
				Kind:          TargetKindSection,
				CanonicalHref: idx.formatCanonicalHref(sourceFile, sectionRoute.HrefPath(), false, isAbsolute, suffix, base),
				RoutePath:     sectionRoute,
			}
		}
		return Resolution{Kind: TargetKindUnresolved, CanonicalHref: href, RoutePath: sectionRoute, Code: IssueCodeBrokenLink}
	}
	switch {
	case hasPage && !hasSection:
		return Resolution{
			Kind:          TargetKindPage,
			CanonicalHref: idx.formatCanonicalHref(sourceFile, page.CanonicalPath, true, isAbsolute, suffix, base),
			RoutePath:     page.RoutePath,
		}
	case hasSection && !hasPage:
		return Resolution{
			Kind:          TargetKindSection,
			CanonicalHref: idx.formatCanonicalHref(sourceFile, sectionRoute.HrefPath(), false, isAbsolute, suffix, base),
			RoutePath:     sectionRoute,
		}
	case hasPage && hasSection:
		if mode == resolveCanonical {
			return Resolution{
				Kind:          TargetKindSection,
				CanonicalHref: idx.formatCanonicalHref(sourceFile, sectionRoute.HrefPath(), false, isAbsolute, suffix, base),
				RoutePath:     sectionRoute,
			}
		}
		return Resolution{Kind: TargetKindUnresolved, CanonicalHref: href, RoutePath: sectionRoute, Code: IssueCodeAmbiguousLegacyLink}
	default:
		return Resolution{Kind: TargetKindUnresolved, CanonicalHref: href, RoutePath: sectionRoute, Code: IssueCodeBrokenLink}
	}
}

func (idx *Index) RewriteMarkdown(sourceFile tree.MarkdownPath, content string) RewriteResult {
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
	rewritten, _ := applyReplacements(content, replacements)
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
