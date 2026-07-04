package importer

import (
	"errors"
	"mime/multipart"
	"os"
	"path/filepath"

	"github.com/perber/wiki/internal/core/shared"
	"github.com/perber/wiki/internal/core/tree"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"
	goldast "github.com/yuin/goldmark/ast"
	goldtext "github.com/yuin/goldmark/text"
)

type nilLinesBlock struct{}

func (nilLinesBlock) Lines() *goldtext.Segments {
	return nil
}

type importerServiceDefaults struct {
	assetMaxUploadSizeBytes shared.MaxBytes
	workspaceBaseDirPresent bool
}

type importedHrefPathShape uint8

const (
	importedHrefPathOther importedHrefPathShape = iota
	importedHrefPathWindowsDrive
)

func classifyImportedHrefPathShape(raw string) importedHrefPathShape {
	if looksLikeWindowsDrivePath(raw) {
		return importedHrefPathWindowsDrive
	}
	return importedHrefPathOther
}

type markdownBracketEscapeState uint8

const (
	markdownBracketLiteral markdownBracketEscapeState = iota
	markdownBracketEscaped
)

func classifyMarkdownBracketEscape(content string, index int) markdownBracketEscapeState {
	if isEscapedMarkdownBracket(content, index) {
		return markdownBracketEscaped
	}
	return markdownBracketLiteral
}

type sourceSuffixCaseMatchState uint8

const (
	sourceSuffixCaseMatchAbsent sourceSuffixCaseMatchState = iota
	sourceSuffixCaseVariantPresent
)

func classifyCaseVariantSourceSuffixMatch(transformer *contentTransformer, source string) sourceSuffixCaseMatchState {
	if transformer.hasCaseVariantSourceSuffixMatch(source) {
		return sourceSuffixCaseVariantPresent
	}
	return sourceSuffixCaseMatchAbsent
}

func importerBadStateFile() string {
	ginkgo.GinkgoHelper()
	base := importerTempDir()
	blockingFile := filepath.Join(base, "state-dir")
	Expect(os.WriteFile(blockingFile, []byte("not a directory"), 0o644)).To(Succeed())
	return filepath.Join(blockingFile, "current-plan.json")
}

func MatchExecutionResultCounts(importedCount int, skippedCount int, items types.GomegaMatcher) types.GomegaMatcher {
	return gstruct.PointTo(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"ImportedCount": matchImportCount(importedCount),
		"SkippedCount":  matchImportCount(skippedCount),
		"Items":         items,
	}))
}

func HaveExecutionItemErrorCode(code ImportErrorCode) types.GomegaMatcher {
	return gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"ErrorCode": Equal(code),
	})
}

func HaveImportPlanErrorCode(code ImportErrorCode) types.GomegaMatcher {
	return ContainElement(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"Code": Equal(code),
	}))
}

func matchImportCount(count int) types.GomegaMatcher {
	if count == 0 {
		return BeZero()
	}
	return Equal(count)
}

func importerServiceDefaultState(service *ImporterService) importerServiceDefaults {
	return importerServiceDefaults{
		assetMaxUploadSizeBytes: service.assetMaxUploadSizeBytes,
		workspaceBaseDirPresent: service.workspaceBaseDir != "",
	}
}

var _ = ginkgo.Describe("content transformer target normalization", ginkgo.Label("unit"), func() {
	ginkgo.It("normalizes source candidates to route paths", func() {
		transformer := newContentTransformer(&PlanResult{}, importerTempDir(), 1024)

		route, err := normalizeSourceCandidateResult(transformer, " Docs/My Page.md ")
		Expect(err).To(Succeed())
		Expect(route).To(Equal(newFixtureRoutePath("docs/my-page")))

		route, err = normalizeSourceCandidateResult(transformer, "docs/section/index.md")
		Expect(err).To(Succeed())
		Expect(route).To(Equal(newFixtureRoutePath("docs/section/index-1")))

		route, err = normalizeSourceCandidateResult(transformer, "assets/manual.pdf")
		Expect(err).To(Succeed())
		Expect(route).To(Equal(newFixtureRoutePath("assets-1/manual-pdf")))
	})

	ginkgo.It("rejects empty candidates and avoids reserved root routes", func() {
		transformer := newContentTransformer(&PlanResult{}, importerTempDir(), 1024)

		_, err := normalizeSourceCandidateResult(transformer, "")
		Expect(err).To(MatchError(errImporterRouteCandidateRejected))

		route, err := normalizeSourceCandidateResult(transformer, "index.md")
		Expect(err).To(Succeed())
		Expect(route).To(Equal(newFixtureRoutePath("index-1")))
	})

	ginkgo.It("filters ambiguous import targets by requested kind", func() {
		targets := []importTarget{
			{targetPath: "docs/sync", kind: tree.NodeKindPage},
			{targetPath: "docs/sync", kind: tree.NodeKindSection},
			{targetPath: "docs/other", kind: tree.NodeKindPage},
		}

		Expect(filterImportTargetsByKind(targets, tree.NodeKindSection)).To(Equal([]importTarget{
			{targetPath: "docs/sync", kind: tree.NodeKindSection},
		}))
		Expect(filterImportTargetsByKind(targets, tree.NodeKindPage)).To(HaveLen(2))
		Expect(filterImportTargetsByKind(targets, tree.NodeKind("unknown"))).To(BeEmpty())
	})
})

var _ = ginkgo.Describe("content transformer helper contracts", ginkgo.Label("unit"), func() {
	ginkgo.It("infers target kind only for markdown page and section destinations", func() {
		kind, err := impliedImportTargetKindResult("")
		Expect(err).To(MatchError(errImporterTargetKindRejected))
		Expect(kind).To(Equal(tree.NodeKind("")))

		kind, err = impliedImportTargetKindResult(" docs/ ")
		Expect(err).To(Succeed())
		Expect(kind).To(Equal(tree.NodeKindSection))

		kind, err = impliedImportTargetKindResult("/docs/index.md")
		Expect(err).To(Succeed())
		Expect(kind).To(Equal(tree.NodeKindSection))

		kind, err = impliedImportTargetKindResult("README.md")
		Expect(err).To(Succeed())
		Expect(kind).To(Equal(tree.NodeKindSection))

		kind, err = impliedImportTargetKindResult("docs/guide.md")
		Expect(err).To(Succeed())
		Expect(kind).To(Equal(tree.NodeKindPage))

		_, err = impliedImportTargetKindResult("docs/image.png")
		Expect(err).To(MatchError(errImporterTargetKindRejected))
	})

	ginkgo.It("splits markdown destinations and URL suffixes without dropping wrappers", func() {
		prefix, href, suffix := splitMarkdownDestination("")
		Expect([]string{prefix, href, suffix}).To(Equal([]string{"", "", ""}))

		prefix, href, suffix = splitMarkdownDestination(`<docs/My Note.md> "Title"`)
		Expect([]string{prefix, href, suffix}).To(Equal([]string{"<", "docs/My Note.md", `> "Title"`}))

		prefix, href, suffix = splitMarkdownDestination("docs/Note.md\t'Title'")
		Expect([]string{prefix, href, suffix}).To(Equal([]string{"", "docs/Note.md", "\t'Title'"}))

		base, urlSuffix := splitURLSuffix("docs/page.md?raw=1#intro")
		Expect(base).To(Equal("docs/page.md"))
		Expect(urlSuffix).To(Equal("?raw=1#intro"))

		base, urlSuffix = splitURLSuffix("docs/page.md#intro?raw=1")
		Expect(base).To(Equal("docs/page.md"))
		Expect(urlSuffix).To(Equal("#intro?raw=1"))

		base, urlSuffix = splitURLSuffix("docs/page.md")
		Expect(base).To(Equal("docs/page.md"))
		Expect(urlSuffix).To(BeEmpty())
	})

	ginkgo.It("normalizes imported hrefs while preserving Windows drive paths", func() {
		Expect(normalizeImportedHref(`docs\guide.md`)).To(Equal("docs/guide.md"))
		Expect(normalizeImportedHref(` C:\Users\me\guide.md `)).To(Equal(` C:\Users\me\guide.md `))
		Expect(normalizeImportedHref("   ")).To(Equal("   "))
		Expect(decodeImportTarget("%zz")).To(Equal("%zz"))
		Expect(classifyImportedHrefPathShape(`C:/Users/me/guide.md`)).To(Equal(importedHrefPathWindowsDrive))
		Expect(classifyImportedHrefPathShape(`\\server\share\guide.md`)).To(Equal(importedHrefPathOther))
		Expect(classifyImportedHrefPathShape("C:")).To(Equal(importedHrefPathOther))
	})

	ginkgo.It("derives default wiki labels and handles escaped markdown brackets", func() {
		Expect(defaultWikiLinkLabel("docs/guide.md#intro")).To(Equal("guide"))
		Expect(defaultWikiLinkLabel("docs/")).To(Equal("docs"))
		Expect(defaultWikiLinkLabel("#intro")).To(Equal("#intro"))
		Expect(defaultWikiLinkLabel("/")).To(Equal("/"))

		content := `[outer [inner\]] target]`
		Expect(findMarkdownClosingBracket(content, 0)).To(Equal(len(content) - 1))
		Expect(findMarkdownClosingBracket(content, 1)).To(Equal(-1))
		Expect(findMarkdownClosingBracket("[unterminated", 0)).To(Equal(-1))
		Expect(classifyMarkdownBracketEscape(`\\]`, 2)).To(Equal(markdownBracketLiteral))
		Expect(classifyMarkdownBracketEscape(`\]`, 1)).To(Equal(markdownBracketEscaped))
	})

	ginkgo.It("deduplicates helper slices without keeping empty targets", func() {
		Expect(uniqueStrings([]string{"", "docs", "docs", "guide"})).To(Equal([]string{"docs", "guide"}))

		targets := uniqueImportTargets([]importTarget{
			{},
			{targetPath: "docs/guide", kind: tree.NodeKindPage},
			{targetPath: "docs/guide", kind: tree.NodeKindPage},
			{targetPath: "docs/guide", kind: tree.NodeKindSection},
		})
		Expect(targets).To(Equal([]importTarget{
			{targetPath: "docs/guide", kind: tree.NodeKindPage},
			{targetPath: "docs/guide", kind: tree.NodeKindSection},
		}))

		key, err := sourceSuffixLookupKeyResult("/docs/index.md#intro")
		Expect(err).To(Succeed())
		Expect(key).To(Equal("docs"))

		key, err = sourceSuffixLookupKeyResult("docs/guide.md?raw=1")
		Expect(err).To(Succeed())
		Expect(key).To(Equal("docs/guide"))

		_, err = sourceSuffixLookupKeyResult("../guide.md")
		Expect(err).To(MatchError(errImporterSourceSuffixRejected))
	})

	ginkgo.It("parses reference destinations and protects code-range rewriting", func() {
		usage := importerReferenceUsage{linkLabels: map[string]struct{}{}, imageLabels: map[string]struct{}{}}
		Expect(usage).NotTo(ReceiveImageOnlyReference(""))

		_, _, err := referenceDestinationResult("[unterminated", 0, len("[unterminated"))
		Expect(err).To(MatchError(errImporterReferenceDestinationRejected))

		_, _, err = referenceDestinationResult("[ref]:", 0, len("[ref]:"))
		Expect(err).To(MatchError(errImporterReferenceDestinationRejected))

		line := `[ref]: docs/page.md "Title"`
		label, destination, err := referenceDestinationResult(line, 0, len(line))
		Expect(err).To(Succeed())
		Expect(label).To(Equal("ref"))
		Expect(destination).To(Equal("docs/page.md"))

		code := goldast.NewCodeSpan()
		code.AppendChild(code, goldast.NewString([]byte("not positional text")))
		Expect(collectMarkdownTextNodeRanges(code)).To(BeEmpty())
		Expect(collectMarkdownBlockRanges(nilLinesBlock{})).To(BeNil())

		ranges := mergeMarkdownTextRanges([]markdownTextRange{
			{Start: 2, End: 3},
			{Start: 1, End: 2},
			{Start: 1, End: 4},
		})
		Expect(ranges).To(Equal([]markdownTextRange{{Start: 1, End: 4}}))

		plainRewriteErr := errors.New("plain rewrite failed")
		_, err = rewriteOutsideCodeSpans("`code` plain", func(segment string) (string, error) {
			return "", plainRewriteErr
		})
		Expect(err).To(MatchError(plainRewriteErr))
	})

	ginkgo.It("normalizes destinations and preserves unresolved wiki links", func() {
		tmp := importerTempDir()
		importerWriteFile(tmp, "current.md", "# Current")
		importerWriteFile(tmp, "asset.png", "png-bytes")
		page := &tree.Page{PageNode: &tree.PageNode{ID: "p1", Kind: tree.NodeKindPage}}
		transformer := newContentTransformerWithOptions(&PlanResult{
			Items: []PlanItem{
				{SourcePath: "Area/Resources/Guide.md", TargetPath: "area/resources/guide", Kind: tree.NodeKindPage},
				{SourcePath: "Other/Resources/Guide/index.md", TargetPath: "other/resources/guide", Kind: tree.NodeKindSection},
			},
		}, tmp, 1024, ContentTransformerOptions{MarkdownLinkRootPrefix: "docs"})

		got, err := transformer.TransformContent("user-1", "current.md", page, "before [[unterminated", &fakeExecWiki{})
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(Equal("before [[unterminated"))

		rewritten, err := transformer.rewriteDestination("user-1", "current.md", page, "   ", &fakeExecWiki{}, rewriteDestinationOptions{})
		Expect(err).NotTo(HaveOccurred())
		Expect(rewritten).To(Equal("   "))

		resolved, err := transformer.resolveAssetDestination("user-1", "current.md", page, "#local", &fakeExecWiki{})
		Expect(err).NotTo(HaveOccurred())
		Expect(resolved).To(BeEmpty())

		uploadErr := errors.New("upload failed")
		_, err = resolvedDestinationResult(transformer, newFixtureUserID("user-1"), "current.md", page, "./asset.png", &fakeExecWiki{
			uploadFn: func(userID tree.UserID, pageID tree.PageID, file multipart.File, filename tree.AssetName, byteCap shared.MaxBytes) (string, error) {
				return "", uploadErr
			},
		})
		Expect(err).To(MatchError(uploadErr))

		wikiUploadErr := errors.New("wiki upload failed")
		_, err = transformer.TransformContent("user-1", "current.md", page, "![[./asset.png]]", &fakeExecWiki{
			uploadFn: func(userID tree.UserID, pageID tree.PageID, file multipart.File, filename tree.AssetName, byteCap shared.MaxBytes) (string, error) {
				return "", wikiUploadErr
			},
		})
		Expect(err).To(MatchError(wikiUploadErr))

		referenceUploadErr := errors.New("reference upload failed")
		_, err = transformer.TransformContent("user-1", "current.md", page, "![Asset][asset]\n\n[asset]: ./asset.png", &fakeExecWiki{
			uploadFn: func(userID tree.UserID, pageID tree.PageID, file multipart.File, filename tree.AssetName, byteCap shared.MaxBytes) (string, error) {
				return "", referenceUploadErr
			},
		})
		Expect(err).To(MatchError(referenceUploadErr))

		Expect(formatResolvedTargetPath(importTarget{targetPath: "docs/raw.md", kind: tree.NodeKindPage})).To(Equal("docs/raw.md"))
		Expect(transformer.formatResolvedHref(importTarget{targetPath: "", kind: tree.NodeKindSection})).To(Equal("/docs"))

		transformer.pagesBySuffix["Resources/Guide"] = []importTarget{
			{targetPath: "area/resources/guide", kind: tree.NodeKindPage},
			{targetPath: "other/resources/guide", kind: tree.NodeKindSection},
		}
		target, err := uniqueTargetSuffixResult(transformer, "Resources/Guide.md")
		Expect(err).To(Succeed())
		Expect(target.kind).To(Equal(tree.NodeKindPage))

		kind, err := impliedImportTargetKindResult("/")
		Expect(err).To(MatchError(errImporterTargetKindRejected))
		Expect(kind).To(Equal(tree.NodeKind("")))
	})

	ginkgo.It("rejects unsafe fallback routes and asset paths", func() {
		tmp := importerTempDir()
		transformer := newContentTransformer(&PlanResult{}, tmp, 1024)

		Expect(buildSourceCandidates(tmp, "current.md", "")).To(BeNil())
		Expect(buildSourceCandidates(tmp, "current.md", "../escape.md")).To(BeNil())

		_, err := fallbackWikiHrefResult(transformer, "docs/current.md", "!!!")
		Expect(err).To(MatchError(errImporterFallbackHrefRejected))

		_, err = fallbackWikiHrefResult(transformer, "current.md", "../escape.md")
		Expect(err).To(MatchError(errImporterFallbackHrefRejected))

		_, err = fallbackWikiHrefResult(transformer, "docs/current.md", "./!!!")
		Expect(err).To(MatchError(errImporterFallbackHrefRejected))

		caseTransformer := newContentTransformer(&PlanResult{
			Items: []PlanItem{{SourcePath: "Docs/Exact.md", TargetPath: "docs/exact", Kind: tree.NodeKindPage}},
		}, tmp, 1024)
		_, err = fallbackWikiHrefResult(caseTransformer, "current.md", "/docs/exact.md")
		Expect(err).To(MatchError(errImporterFallbackHrefRejected))

		_, err = fallbackWikiHrefResult(transformer, "current.md", "bad/!!!")
		Expect(err).To(MatchError(errImporterFallbackHrefRejected))

		href, err := fallbackWikiHrefResult(transformer, "docs/current.md", "Missing/Sub")
		Expect(err).To(Succeed())
		Expect(href).To(Equal("/missing/sub"))

		_, err = wikiHrefRoutePathResult(transformer, "!!!")
		Expect(err).To(MatchError(errImporterWikiHrefRouteRejected))

		Expect(buildSourcePathSuffixKeys("", tree.NodeKindPage)).To(BeNil())

		_, err = sourceSuffixLookupKeyResult("index.md")
		Expect(err).To(MatchError(errImporterSourceSuffixRejected))

		_, err = resolvedAssetPathResult(tmp, "current.md", "")
		Expect(err).To(MatchError(errImporterAssetPathRejected))

		_, err = resolvedAssetPathResult(tmp, "current.md", "../escape.png")
		Expect(err).To(MatchError(errImporterAssetPathRejected))
	})

	ginkgo.It("reports filesystem failures while resolving asset destinations", func() {
		tmp := importerTempDir()
		importerWriteFile(tmp, "asset.png", "png-bytes")
		page := &tree.Page{PageNode: &tree.PageNode{ID: "p1", Kind: tree.NodeKindPage}}
		transformer := newContentTransformer(&PlanResult{}, tmp, 1024)

		originalOpenAsset := importerOpenAsset
		ginkgo.DeferCleanup(func() {
			importerOpenAsset = originalOpenAsset
		})
		openFailedErr := errors.New("open failed")
		importerOpenAsset = func(name string) (*os.File, error) {
			return nil, openFailedErr
		}
		_, err := transformer.resolveAndUploadAsset("user-1", "current.md", page, "asset.png", &fakeExecWiki{})
		Expect(err).To(MatchError(openFailedErr))
		importerOpenAsset = originalOpenAsset

		originalAbs := importerFilepathAbs
		ginkgo.DeferCleanup(func() {
			importerFilepathAbs = originalAbs
		})
		importerFilepathAbs = func(path string) (string, error) {
			return "", errors.New("abs failed")
		}
		_, err = resolvedAssetPathResult(tmp, "current.md", "asset.png")
		Expect(err).To(MatchError(errImporterAssetPathRejected))

		absCalls := 0
		importerFilepathAbs = func(path string) (string, error) {
			absCalls++
			if absCalls == 2 {
				return "", errors.New("asset abs failed")
			}
			return "/base", nil
		}
		_, err = resolvedAssetPathResult(tmp, "current.md", "asset.png")
		Expect(err).To(MatchError(errImporterAssetPathRejected))
		importerFilepathAbs = originalAbs

		originalRel := importerFilepathRel
		ginkgo.DeferCleanup(func() {
			importerFilepathRel = originalRel
		})
		importerFilepathRel = func(basepath, targpath string) (string, error) {
			return "", errors.New("rel failed")
		}
		_, err = resolvedAssetPathResult(tmp, "current.md", "asset.png")
		Expect(err).To(MatchError(errImporterAssetPathRejected))
		importerFilepathRel = originalRel
	})
})

var _ = ginkgo.Describe("content transformer fallback wiki hrefs", ginkgo.Label("unit"), func() {
	ginkgo.It("falls back to exact planned basename matches before generated routes", func() {
		transformer := newContentTransformer(&PlanResult{
			Items: []PlanItem{
				{SourcePath: "Notes/Guide.md", TargetPath: "kb/guide", Kind: tree.NodeKindPage},
			},
		}, importerTempDir(), 1024)

		href, err := fallbackWikiHrefResult(transformer, "current.md", "Guide#intro")
		Expect(err).To(Succeed())
		Expect(href).To(Equal("/kb/guide.md#intro"))

		href, err = fallbackWikiHrefResult(transformer, "current.md", "Missing Note?raw=1#intro")
		Expect(err).To(Succeed())
		Expect(href).To(Equal("/missing-note?raw=1#intro"))
	})

	ginkgo.It("refuses fallback when basename or source suffix casing is ambiguous", func() {
		transformer := newContentTransformer(&PlanResult{
			Items: []PlanItem{
				{SourcePath: "Area/Guide.md", TargetPath: "area/guide", Kind: tree.NodeKindPage},
				{SourcePath: "Other/Guide.md", TargetPath: "other/guide", Kind: tree.NodeKindPage},
				{SourcePath: "Docs/Exact.md", TargetPath: "docs/exact", Kind: tree.NodeKindPage},
			},
		}, importerTempDir(), 1024)

		_, err := fallbackWikiHrefResult(transformer, "current.md", "Guide")
		Expect(err).To(MatchError(errImporterFallbackHrefRejected))

		_, err = fallbackWikiHrefResult(transformer, "current.md", "guide")
		Expect(err).To(MatchError(errImporterFallbackHrefRejected))

		Expect(classifyCaseVariantSourceSuffixMatch(transformer, "docs/exact.md")).To(Equal(sourceSuffixCaseVariantPresent))
		Expect(classifyCaseVariantSourceSuffixMatch(transformer, "Docs/Exact.md")).To(Equal(sourceSuffixCaseMatchAbsent))
		Expect(classifyCaseVariantSourceSuffixMatch(transformer, "../Exact.md")).To(Equal(sourceSuffixCaseMatchAbsent))
	})

	ginkgo.It("generates route fallbacks for safe relative sources", func() {
		transformer := newContentTransformer(&PlanResult{}, importerTempDir(), 1024)

		_, err := wikiHrefRoutePathResult(transformer, "docs//page")
		Expect(err).To(MatchError(errImporterWikiHrefRouteRejected))

		_, err = normalizeSourceCandidateResult(transformer, "/")
		Expect(err).To(MatchError(errImporterRouteCandidateRejected))

		href, err := fallbackWikiHrefResult(transformer, "docs/current.md", "./New Page.md?raw=1")
		Expect(err).To(Succeed())
		Expect(href).To(Equal("/docs/new-page?raw=1"))

		href, err = fallbackWikiHrefResult(transformer, "docs/current.md", "/Guides/New Page.md")
		Expect(err).To(Succeed())
		Expect(href).To(Equal("/guides/new-page"))

		_, err = fallbackWikiHrefResult(transformer, "docs/current.md", "https://example.test/page")
		Expect(err).To(MatchError(errImporterFallbackHrefRejected))

		_, err = fallbackWikiHrefResult(transformer, "docs/current.md", "#local")
		Expect(err).To(MatchError(errImporterFallbackHrefRejected))

		_, err = fallbackWikiHrefResult(transformer, "docs/current.md", "image.png")
		Expect(err).To(MatchError(errImporterFallbackHrefRejected))
	})

	ginkgo.It("detects README folder fallbacks case-sensitively", func() {
		tmp := importerTempDir()
		Expect(os.MkdirAll(filepath.Join(tmp, "lower"), 0o755)).To(Succeed())
		Expect(os.MkdirAll(filepath.Join(tmp, "upper"), 0o755)).To(Succeed())
		Expect(os.MkdirAll(filepath.Join(tmp, "dironly", "README.md"), 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(tmp, "lower", "readme.md"), []byte("# lower"), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(tmp, "upper", "README.md"), []byte("# upper"), 0o644)).To(Succeed())

		_, err := sourceDirReadmeFallbackResult(tmp, "missing")
		Expect(err).To(MatchError(errImporterReadmeFallbackRejected))

		_, err = sourceDirReadmeFallbackResult(tmp, "lower")
		Expect(err).To(MatchError(errImporterReadmeFallbackRejected))

		_, err = sourceDirReadmeFallbackResult(tmp, "dironly")
		Expect(err).To(MatchError(errImporterReadmeFallbackRejected))

		name, err := sourceDirReadmeFallbackResult(tmp, "upper")
		Expect(err).To(Succeed())
		Expect(name).To(Equal("README.md"))
	})
})
