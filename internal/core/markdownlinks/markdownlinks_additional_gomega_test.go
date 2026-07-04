package markdownlinks

import (
	"errors"
	"os"
	"path/filepath"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"

	"github.com/perber/wiki/internal/core/tree"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

type nilLineBlock struct{}

func (nilLineBlock) Lines() *text.Segments { return nil }

var errRelMarkdownLinkPathFailed = errors.New("rel markdown link path failed")

var (
	errReferenceDefinitionRejected  = errors.New("reference definition rejected")
	errReferenceDestinationRejected = errors.New("reference destination rejected")
	errInlineDestinationRejected    = errors.New("inline destination rejected")
	errInlineLinkTailRejected       = errors.New("inline link tail rejected")
)

type indexEntryMapCounts struct {
	PageCount  int
	AssetCount int
}

func haveEmptyIndexEntryMaps() types.GomegaMatcher {
	return WithTransform(func(index *Index) indexEntryMapCounts {
		return indexEntryMapCounts{
			PageCount:  len(index.pages),
			AssetCount: len(index.assets),
		}
	}, gstruct.MatchAllFields(gstruct.Fields{
		"PageCount":  BeZero(),
		"AssetCount": BeZero(),
	}))
}

func matchResolution(kind TargetKind, code IssueCode) types.GomegaMatcher {
	return gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"Kind": Equal(kind),
		"Code": Equal(code),
	})
}

func matchResolvedLink(kind TargetKind, canonicalHref string, routePath string) types.GomegaMatcher {
	fields := gstruct.Fields{
		"Kind":          Equal(kind),
		"CanonicalHref": Equal(canonicalHref),
		"Code":          BeZero(),
	}
	if routePath != "" {
		fields["RoutePath"] = Equal(mustRoutePath(routePath))
	}
	return gstruct.MatchFields(gstruct.IgnoreExtras, fields)
}

func mustRoutePath(raw string) tree.RoutePath {
	ginkgo.GinkgoHelper()
	routePath, err := tree.ParseRoutePath(raw)
	Expect(err).NotTo(HaveOccurred())
	return routePath
}

func matchRootSection(canonicalHref string) types.GomegaMatcher {
	return gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"Kind":          Equal(TargetKindSection),
		"CanonicalHref": Equal(canonicalHref),
		"RoutePath":     BeZero(),
	})
}

func matchRewriteResult(content string, changed bool, issues ...Issue) types.GomegaMatcher {
	issueMatcher := Equal(issues)
	if len(issues) == 0 {
		issueMatcher = BeEmpty()
	}
	return gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"Content": Equal(content),
		"Changed": Equal(changed),
		"Issues":  issueMatcher,
	})
}

func matchLinkOccurrenceHref(href string) types.GomegaMatcher {
	return gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"Href": Equal(href),
	})
}

func matchInlineDestination(destination string, image bool) types.GomegaMatcher {
	return gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"Destination": Equal(destination),
		"Image":       Equal(image),
	})
}

func parseReferenceDefinitionLineResult(content string, lineStart int, lineEnd int) (linkOccurrence, error) {
	ginkgo.GinkgoHelper()
	occurrence, accepted := parseReferenceDefinitionLine(content, lineStart, lineEnd)
	if !accepted {
		return linkOccurrence{}, errReferenceDefinitionRejected
	}
	return occurrence, nil
}

func parseReferenceDestinationResult(content string, start int, lineEnd int) (linkOccurrence, error) {
	ginkgo.GinkgoHelper()
	occurrence, accepted := parseReferenceDestination(content, start, lineEnd)
	if !accepted {
		return linkOccurrence{}, errReferenceDestinationRejected
	}
	return occurrence, nil
}

func parseDestinationResult(content string, start int) (linkOccurrence, error) {
	ginkgo.GinkgoHelper()
	occurrence, accepted := parseDestination(content, start)
	if !accepted {
		return linkOccurrence{}, errInlineDestinationRejected
	}
	return occurrence, nil
}

func inlineLinkTailEndResult(content string, start int) (int, error) {
	ginkgo.GinkgoHelper()
	end, accepted := inlineLinkTailEnd(content, start)
	if !accepted {
		return end, errInlineLinkTailRejected
	}
	return end, nil
}

func applyReplacementsResult(content string, replacements []replacement) RewriteResult {
	ginkgo.GinkgoHelper()
	rewritten, changed := applyReplacements(content, replacements)
	return RewriteResult{Content: rewritten, Changed: changed}
}

func markdownLinksTempDir() string {
	ginkgo.GinkgoHelper()
	dir, err := os.MkdirTemp("", "leafwiki-markdownlinks-*")
	Expect(err).NotTo(HaveOccurred())
	ginkgo.DeferCleanup(os.RemoveAll, dir)
	return dir
}

var _ = ginkgo.Describe("markdown link parser internals", func() {
	ginkgo.It("skips unsupported entries hidden paths and non-markdown files while building the link index", func() {
		index := NewIndexWithOptions([]Entry{
			{Kind: EntryKindPage},
			{Kind: EntryKindAsset},
		}, Options{MarkdownLinkRootPrefix: "/wiki"})
		Expect(index).To(haveEmptyIndexEntryMaps())

		_, err := NewIndexFromRootWithOptions(filepath.Join(markdownLinksTempDir(), "missing"), Options{})
		Expect(err).To(MatchError(os.ErrNotExist))

		rootDir := markdownLinksTempDir()
		Expect(os.MkdirAll(filepath.Join(rootDir, "docs"), 0o755)).To(Succeed())
		Expect(os.MkdirAll(filepath.Join(rootDir, ".hidden"), 0o755)).To(Succeed())
		Expect(os.MkdirAll(filepath.Join(rootDir, "assets"), 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(rootDir, "docs", "page.md"), []byte("# Page"), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(rootDir, "docs", "notes.txt"), []byte("not markdown"), 0o644)).To(Succeed())

		originalRelMarkdownLinkPath := relMarkdownLinkPath
		relMarkdownLinkPath = func(string, string) (string, error) {
			return "", errRelMarkdownLinkPathFailed
		}
		ginkgo.DeferCleanup(func() {
			relMarkdownLinkPath = originalRelMarkdownLinkPath
		})
		_, err = NewIndexFromRootWithOptions(rootDir, Options{})
		Expect(err).To(MatchError(errRelMarkdownLinkPathFailed))
		relMarkdownLinkPath = originalRelMarkdownLinkPath

		index, err = NewIndexFromRootWithOptions(rootDir, Options{})
		Expect(err).ToNot(HaveOccurred())
		Expect(index.Resolve("docs/source.md", "/docs/page.md").Kind).To(Equal(TargetKindPage))
		Expect(index.sections).To(HaveKey(tree.RoutePath("docs")))
		Expect(index.sections).ToNot(HaveKey(tree.RoutePath(".hidden")))

		originalMapWorkspaceMarkdownRoute := mapWorkspaceMarkdownRoute
		mapWorkspaceMarkdownRoute = func(rootDir string, relPath string, isDir bool) (tree.WorkspaceMarkdownRoute, error) {
			if isDir {
				return tree.WorkspaceMarkdownRoute{}, errors.New("route failed")
			}
			return originalMapWorkspaceMarkdownRoute(rootDir, relPath, isDir)
		}
		ginkgo.DeferCleanup(func() {
			mapWorkspaceMarkdownRoute = originalMapWorkspaceMarkdownRoute
		})
		_, err = NewIndexFromRootWithOptions(rootDir, Options{})
		Expect(err).ToNot(HaveOccurred())

		mapWorkspaceMarkdownRoute = func(rootDir string, relPath string, isDir bool) (tree.WorkspaceMarkdownRoute, error) {
			if !isDir {
				return tree.WorkspaceMarkdownRoute{Skip: true}, nil
			}
			return originalMapWorkspaceMarkdownRoute(rootDir, relPath, isDir)
		}
		_, err = NewIndexFromRootWithOptions(rootDir, Options{})
		Expect(err).ToNot(HaveOccurred())
		mapWorkspaceMarkdownRoute = originalMapWorkspaceMarkdownRoute
	})

	ginkgo.It("resolves empty, broken trailing-slash, and missing section destinations", func() {
		index := NewIndex([]Entry{
			{Kind: EntryKindPage, Path: "docs/page.md"},
			{Kind: EntryKindSection, RoutePath: "docs/section"},
		})

		Expect(index.Resolve("docs/source.md", "?query")).To(matchResolution(TargetKindUnresolved, IssueCodeEmpty))

		trailing := index.Resolve("docs/source.md", "/missing/")
		Expect(trailing).To(matchResolution(TargetKindUnresolved, IssueCodeBrokenLink))

		missing := index.Resolve("docs/source.md", "/missing")
		Expect(missing).To(matchResolution(TargetKindUnresolved, IssueCodeBrokenLink))
	})

	ginkgo.It("ignores incomplete code spans inline links and image-only reference definitions", func() {
		Expect(excludedCodeRanges("`unterminated")).To(BeEmpty())
		Expect(collectBlockRanges(nilLineBlock{})).To(BeNil())
		Expect(findClosingBacktickRun("``code```", 2, 2, nil)).To(Equal(-1))

		codeSpan := ast.NewCodeSpan()
		codeSpan.AppendChild(codeSpan, ast.NewString([]byte("literal")))
		Expect(collectTextNodeRanges(codeSpan)).To(BeEmpty())

		Expect(ScanInlineDestinations("[unterminated", InlineScanOptions{})).To(BeEmpty())
		Expect(ScanInlineDestinations("[Label](", InlineScanOptions{})).To(BeEmpty())

		usage := referenceUsage{imageLabels: map[string]struct{}{"img": {}}, linkLabels: map[string]struct{}{}}
		Expect(usage.imageOnly("")).To(BeFalse())
		Expect(usage.imageOnly("img")).To(BeTrue())

		imageOnlyReference := "[img]: /assets/logo.png"
		Expect(scanReferenceDefinitions(imageOnlyReference, nil, usage)).To(BeEmpty())

		_, err := parseReferenceDefinitionLineResult("[empty]:   ", 0, len("[empty]:   "))
		Expect(err).To(MatchError(errReferenceDefinitionRejected))
		_, err = parseReferenceDefinitionLineResult("[empty]: <>", 0, len("[empty]: <>"))
		Expect(err).To(MatchError(errReferenceDefinitionRejected))

		occurrence, err := parseReferenceDestinationResult("<docs/page.md>", 0, len("<docs/page.md>"))
		Expect(err).To(Succeed())
		Expect(occurrence).To(matchLinkOccurrenceHref("docs/page.md"))

		occurrence, err = parseReferenceDestinationResult("docs/page.md \"title\"", 0, len("docs/page.md \"title\""))
		Expect(err).To(Succeed())
		Expect(occurrence).To(matchLinkOccurrenceHref("docs/page.md"))

		_, err = parseReferenceDestinationResult("", 0, 0)
		Expect(err).To(MatchError(errReferenceDestinationRejected))
		_, err = parseReferenceDestinationResult("<>", 0, len("<>"))
		Expect(err).To(MatchError(errReferenceDestinationRejected))
	})

	ginkgo.It("parses escaped inline destinations titles and bracket boundaries", func() {
		_, err := parseDestinationResult("", 0)
		Expect(err).To(MatchError(errInlineDestinationRejected))

		occurrence, err := parseDestinationResult(`<a\>b>)`, 0)
		Expect(err).To(Succeed())
		Expect(occurrence).To(matchLinkOccurrenceHref(`a\>b`))

		occurrence, err = parseDestinationResult(`a\(b\))`, 0)
		Expect(err).To(Succeed())
		Expect(occurrence).To(matchLinkOccurrenceHref(`a\(b\)`))

		occurrence, err = parseDestinationResult(`a(b))`, 0)
		Expect(err).To(Succeed())
		Expect(occurrence).To(matchLinkOccurrenceHref("a(b)"))

		occurrence, err = parseDestinationResult(`target "escaped \" title")`, 0)
		Expect(err).To(Succeed())
		Expect(occurrence).To(matchLinkOccurrenceHref("target"))

		tailEnd, err := inlineLinkTailEndResult("   ", 0)
		Expect(tailEnd).To(BeZero())
		Expect(err).To(MatchError(errInlineLinkTailRejected))
		Expect(parseInlineLinkTitleEnd(`"a\"b"`, 0)).To(Equal(len(`"a\"b"`)))
		Expect(parseInlineLinkTitleEnd(`(a\(b))`, 0)).To(Equal(len(`(a\(b)`)))
		Expect(parseInlineLinkTitleEnd(`(a(b))`, 0)).To(Equal(len(`(a(b))`)))
		Expect(findClosingBracket(`[a\]b]`, 0)).To(Equal(len(`[a\]b]`) - 1))
		Expect(findClosingBracket(`[unclosed`, 0)).To(Equal(-1))
	})

	ginkgo.It("preserves link rewrite boundaries and formats root and encoded-section hrefs", func() {
		Expect(mergeRanges([]textRange{{Start: 0, End: 2}, {Start: 1, End: 4}})).To(Equal([]textRange{{Start: 0, End: 4}}))

		Expect(applyReplacementsResult("abc", nil)).To(matchRewriteResult("abc", false))

		Expect(applyReplacementsResult("abcdef", []replacement{
			{Start: 2, End: 1, Value: "skip"},
			{Start: 1, End: 3, Value: "X"},
			{Start: 1, End: 2, Value: "Y"},
		})).To(matchRewriteResult("aYcdef", true))

		base, suffix := splitURLSuffix("docs/page#section?query")
		Expect(base).To(Equal("docs/page"))
		Expect(suffix).To(Equal("#section?query"))

		Expect(formatHref("docs/source.md", "", false, true, "?q")).To(Equal("/?q"))
		Expect(formatHref("docs/source.md", "", true, true, "#heading")).To(Equal("/#heading"))

		originalRelMarkdownLinkPath := relMarkdownLinkPath
		relMarkdownLinkPath = func(string, string) (string, error) {
			return "", errRelMarkdownLinkPathFailed
		}
		ginkgo.DeferCleanup(func() {
			relMarkdownLinkPath = originalRelMarkdownLinkPath
		})
		Expect(formatHref("docs/source.md", "docs/page.md", true, false, "")).To(Equal("docs/page.md"))
		relMarkdownLinkPath = originalRelMarkdownLinkPath

		Expect(formatHref("docs/source.md", "docs", false, false, "")).To(Equal("."))

		index := NewIndexWithOptions(nil, Options{MarkdownLinkRootPrefix: "/wiki"})
		Expect(index.formatCanonicalHref("docs/source.md", "docs/page.md", true, true, "", "docs/page%20name")).To(Equal("docs/page%20name.md"))

		Expect(encodedSectionHrefBase("/")).To(Equal("/"))
		Expect(encodedSectionHrefBase("README.md")).To(Equal("."))
		Expect(encodedSectionHrefBase("docs/README.md")).To(Equal("docs"))
		Expect(encodedSectionHrefBase("docs/page")).To(Equal("docs/page"))
	})

	ginkgo.It("rejects additional invalid markdown link root prefixes", func() {
		_, err := NormalizeMarkdownLinkRootPrefix("%zz")
		Expect(err).To(MatchError(ErrMarkdownLinkRootPrefixParse))

		_, err = NormalizeMarkdownLinkRootPrefix("?query")
		Expect(err).To(MatchError(ErrMarkdownLinkRootPrefixQueryOrFragment))

		_, err = NormalizeMarkdownLinkRootPrefix("#fragment")
		Expect(err).To(MatchError(ErrMarkdownLinkRootPrefixQueryOrFragment))
	})
})
