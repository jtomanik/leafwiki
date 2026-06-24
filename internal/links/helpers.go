package links

import (
	"path"
	"strings"

	"github.com/perber/wiki/internal/core/markdownlinks"
	"github.com/perber/wiki/internal/core/tree"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

type TargetLink struct {
	TargetPageID   tree.PageID
	TargetPagePath string
	TargetKind     string
	Broken         bool
}

var markdownParser = goldmark.New()
var newMarkdownLinkIndexFromRoot = markdownlinks.NewIndexFromRoot

func isAssetLinkDestination(dest string) bool {
	dest = strings.TrimSpace(dest)
	dest = strings.TrimPrefix(dest, "<")
	dest = strings.TrimSuffix(dest, ">")

	return strings.HasPrefix(dest, "/assets/") || strings.HasPrefix(dest, "assets/")
}

// extractLinksFromMarkdown extracts all links from the given markdown content.
func extractLinksFromMarkdown(content string) []string {
	links := []string{}
	reader := text.NewReader([]byte(content))
	doc := markdownParser.Parser().Parse(reader)

	err := ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if link, ok := n.(*ast.Link); ok && entering {
			// ignore external links
			dest := string(link.Destination)
			lower := strings.ToLower(dest)
			if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "mailto:") || strings.HasPrefix(lower, "#") {
				return ast.WalkContinue, nil
			}
			// strip hash fragments
			if idx := strings.Index(dest, "#"); idx != -1 {
				dest = dest[:idx]
			}
			// strip query parameters
			if idx := strings.Index(dest, "?"); idx != -1 {
				dest = dest[:idx]
			}
			if isAssetLinkDestination(dest) {
				return ast.WalkContinue, nil
			}

			links = append(links, dest)
		}
		return ast.WalkContinue, nil
	})
	if err != nil {
		return []string{}
	}

	return links
}

// normalizeWikiPath normalizes a wiki path:
// - removes query/hash (if any)
// - ensures leading "/"
// - removes trailing "/" (except root "/")
func normalizeWikiPath(p string) string {
	if p == "" {
		return ""
	}

	// strip hash/query defensively (caller already does, but keep consistent)
	if i := strings.Index(p, "#"); i != -1 {
		p = p[:i]
	}
	if i := strings.Index(p, "?"); i != -1 {
		p = p[:i]
	}

	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}

	// collapse multiple slashes
	for strings.Contains(p, "//") {
		p = strings.ReplaceAll(p, "//", "/")
	}

	// strip trailing slash except root
	if len(p) > 1 {
		p = strings.TrimRight(p, "/")
	}

	return p
}

func resolveTargetLinks(treeService *tree.TreeService, currentPath tree.RoutePath, links []string) []TargetLink {
	return resolveTargetLinksForSourceKind(treeService, currentPath, tree.NodeKindPage, links)
}

func resolveTargetLinksForSourceKind(treeService *tree.TreeService, currentPath tree.RoutePath, sourceKind tree.NodeKind, links []string) []TargetLink {
	if !treeService.IsLoaded() {
		return nil
	}

	return resolveTargetLinksWithIndex(treeService, markdownLinkIndexForTree(treeService), currentPath, sourceKind, links)
}

func resolveTargetLinksWithIndex(treeService *tree.TreeService, index *markdownlinks.Index, currentPath tree.RoutePath, sourceKind tree.NodeKind, links []string) []TargetLink {
	if treeService == nil || !treeService.IsLoaded() {
		return nil
	}
	if index == nil {
		index = markdownlinks.NewIndex(nil)
	}

	sourceFile := markdownSourceFileForRoute(currentPath, sourceKind)
	var targetLinks []TargetLink

	for _, link := range links {
		if isAssetLinkDestination(link) {
			continue
		}

		resolved := index.Resolve(sourceFile, link)
		if resolved.Kind != markdownlinks.TargetKindPage && resolved.Kind != markdownlinks.TargetKindSection && resolved.Kind != markdownlinks.TargetKindUnresolved {
			continue
		}
		resolvedPath := resolved.RoutePath.WikiPath()
		if resolvedPath == "" || resolvedPath == "/" {
			continue
		}

		if resolved.Kind == markdownlinks.TargetKindUnresolved {
			targetKind := unresolvedStoredTargetKind(resolved, link)
			targetLinks = append(targetLinks, TargetLink{
				TargetPageID:   "",
				TargetPagePath: resolvedPath,
				TargetKind:     targetKind,
				Broken:         true,
			})
			continue
		}

		targetKind := markdownTargetNodeKind(resolved.Kind)
		if targetKind == tree.NodeKindPage && isExtensionlessWikiDestination(link) {
			targetLinks = append(targetLinks, TargetLink{
				TargetPageID:   "",
				TargetPagePath: resolvedPath,
				TargetKind:     nonCanonicalPageStoredTarget,
				Broken:         true,
			})
			continue
		}

		// find page by route path
		page, err := treeService.FindPageByRoutePathAndKind(resolved.RoutePath, targetKind)
		if err == nil && page != nil {
			// found page
			targetLinks = append(targetLinks, TargetLink{
				TargetPageID:   page.ID,
				TargetPagePath: resolvedPath,
				TargetKind:     string(page.Kind),
				Broken:         false,
			})
		} else {
			// not found, broken link
			targetLinks = append(targetLinks, TargetLink{
				TargetPageID:   "",
				TargetPagePath: resolvedPath,
				TargetKind:     string(targetKind),
				Broken:         true,
			})
		}
	}

	return targetLinks
}

func markdownTargetNodeKind(kind markdownlinks.TargetKind) tree.NodeKind {
	if kind == markdownlinks.TargetKindSection {
		return tree.NodeKindSection
	}
	return tree.NodeKindPage
}

func isExtensionlessWikiDestination(destination string) bool {
	dest := strings.TrimSpace(destination)
	dest = strings.TrimPrefix(dest, "<")
	dest = strings.TrimSuffix(dest, ">")
	if idx := strings.Index(dest, "#"); idx != -1 {
		dest = dest[:idx]
	}
	if idx := strings.Index(dest, "?"); idx != -1 {
		dest = dest[:idx]
	}
	if dest == "" || strings.HasSuffix(dest, "/") {
		return false
	}
	return path.Ext(dest) == ""
}

func unresolvedStoredTargetKind(resolved markdownlinks.Resolution, href string) string {
	if resolved.Code == "broken_page" {
		return string(tree.NodeKindPage)
	}
	base, _ := splitLinkDestinationSuffix(strings.TrimSpace(href))
	if strings.HasSuffix(strings.TrimRight(base, " \t\r\n"), "/") {
		return string(tree.NodeKindSection)
	}
	if strings.EqualFold(path.Ext(strings.TrimRight(base, "/")), ".md") {
		return string(tree.NodeKindPage)
	}
	return unknownStoredTargetKind
}

func splitLinkDestinationSuffix(destination string) (string, string) {
	if idx := strings.Index(destination, "#"); idx != -1 {
		return destination[:idx], destination[idx:]
	}
	if idx := strings.Index(destination, "?"); idx != -1 {
		return destination[:idx], destination[idx:]
	}
	return destination, ""
}

func markdownLinkIndexForTree(treeService *tree.TreeService) *markdownlinks.Index {
	return markdownLinkIndexForTreeWithOptions(treeService, markdownlinks.Options{})
}

func markdownLinkIndexForTreeWithOptions(treeService *tree.TreeService, opts markdownlinks.Options) *markdownlinks.Index {
	if treeService == nil {
		return markdownlinks.NewIndexWithOptions(nil, opts)
	}
	if rootDir := strings.TrimSpace(treeService.RootDir()); rootDir != "" {
		var index *markdownlinks.Index
		var err error
		if strings.TrimSpace(opts.MarkdownLinkRootPrefix) == "" {
			index, err = newMarkdownLinkIndexFromRoot(rootDir)
		} else {
			index, err = markdownlinks.NewIndexFromRootWithOptions(rootDir, opts)
		}
		if err == nil {
			return index
		}
	}
	return markdownLinkIndexFromLoadedTreeWithOptions(treeService.GetTree(), opts)
}

func markdownLinkIndexFromLoadedTree(root *tree.PageNode) *markdownlinks.Index {
	return markdownLinkIndexFromLoadedTreeWithOptions(root, markdownlinks.Options{})
}

func markdownLinkIndexFromLoadedTreeWithOptions(root *tree.PageNode, opts markdownlinks.Options) *markdownlinks.Index {
	entries := []markdownlinks.Entry{{Kind: markdownlinks.EntryKindSection, ContentPath: "index.md"}}
	var walk func(node *tree.PageNode)
	walk = func(node *tree.PageNode) {
		if node == nil {
			return
		}
		routePath := node.CalculateRoutePath()
		switch node.Kind {
		case tree.NodeKindSection:
			entries = append(entries, markdownlinks.Entry{
				Kind:        markdownlinks.EntryKindSection,
				RoutePath:   routePath,
				ContentPath: markdownContentPathForRoute(routePath, tree.NodeKindSection),
			})
		case tree.NodeKindPage:
			if routePath != "" {
				entries = append(entries, markdownlinks.Entry{
					Kind:      markdownlinks.EntryKindPage,
					RoutePath: routePath,
				})
			}
		}
		for _, child := range node.Children {
			walk(child)
		}
	}
	walk(root)
	return markdownlinks.NewIndexWithOptions(entries, opts)
}

func markdownSourceFileForRoute(routePath tree.RoutePath, kind tree.NodeKind) tree.MarkdownPath {
	normalized := strings.Trim(normalizeWikiPath(routePath.WikiPath()), "/")
	if normalized == "" {
		var rootRoutePath tree.RoutePath
		return markdownContentPathForRoute(rootRoutePath, kind)
	}
	semanticRoutePath, err := tree.ParseRoutePath(normalized)
	if err != nil {
		var empty tree.MarkdownPath
		return empty
	}
	return markdownContentPathForRoute(semanticRoutePath, kind)
}

func markdownContentPathForRoute(routePath tree.RoutePath, kind tree.NodeKind) tree.MarkdownPath {
	return routePath.MarkdownContentPath(kind)
}

func toBacklinkResult(treeService *tree.TreeService, backlinks []Backlink) *BacklinkResult {
	var items []BacklinkResultItem
	for _, backlink := range backlinks {
		item := toBacklinkResultItem(treeService, backlink)
		items = append(items, item)
	}
	return &BacklinkResult{
		Backlinks: items,
		Count:     len(items),
	}
}

func toBacklinkResultItem(treeService *tree.TreeService, backlink Backlink) BacklinkResultItem {
	if !treeService.IsLoaded() {
		return BacklinkResultItem{}
	}

	page, err := treeService.FindPageByID(backlink.FromPageID)
	if err != nil {
		return BacklinkResultItem{}
	}

	return BacklinkResultItem{
		FromPageID: backlink.FromPageID,
		FromTitle:  backlink.FromTitle,
		FromPath:   page.CalculatePath(),
		FromKind:   string(page.Kind),
		ToPageID:   backlink.ToPageID,
		Broken:     backlink.Broken,
	}
}

func toOutgoingLinkResult(treeService *tree.TreeService, outgoings []Outgoing) *OutgoingResult {
	var items []OutgoingResultItem
	for _, outgoing := range outgoings {
		item := toOutgoingResultItem(treeService, outgoing)
		items = append(items, item)
	}
	return &OutgoingResult{
		Outgoings: items,
		Count:     len(items),
	}
}

func toOutgoingResultItem(treeService *tree.TreeService, outgoing Outgoing) OutgoingResultItem {
	toKind := outgoing.ToKind
	if toKind == nonCanonicalPageStoredTarget {
		toKind = defaultStoredTargetKind
	}
	item := OutgoingResultItem{
		ToPageID:   outgoing.ToPageID,
		ToPath:     outgoing.ToPath,
		ToKind:     toKind,
		Broken:     outgoing.Broken,
		FromPageID: outgoing.FromPageID,
	}

	if outgoing.ToPageID == "" {
		return item
	}

	if !treeService.IsLoaded() {
		return item
	}

	toPage, err := treeService.FindPageByID(outgoing.ToPageID)
	if err != nil || toPage == nil {
		return item
	}

	item.ToPageTitle = toPage.Title
	return item
}
