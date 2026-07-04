package importer

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"log/slog"
	"mime/multipart"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/perber/wiki/internal/core/assets"
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

var _ = ginkgo.Describe("content transformer target normalization", func() {
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

var _ = ginkgo.Describe("content transformer helper contracts", func() {
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
		Expect(looksLikeWindowsDrivePath(`C:/Users/me/guide.md`)).To(BeTrue())
		Expect(looksLikeWindowsDrivePath(`\\server\share\guide.md`)).To(BeFalse())
		Expect(looksLikeWindowsDrivePath("C:")).To(BeFalse())
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
		Expect(isEscapedMarkdownBracket(`\\]`, 2)).To(BeFalse())
		Expect(isEscapedMarkdownBracket(`\]`, 1)).To(BeTrue())
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

var _ = ginkgo.Describe("content transformer fallback wiki hrefs", func() {
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

		Expect(transformer.hasCaseVariantSourceSuffixMatch("docs/exact.md")).To(BeTrue())
		Expect(transformer.hasCaseVariantSourceSuffixMatch("Docs/Exact.md")).To(BeFalse())
		Expect(transformer.hasCaseVariantSourceSuffixMatch("../Exact.md")).To(BeFalse())
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

var _ = ginkgo.Describe("Executor execution edges", func() {
	ginkgo.It("rejects resume state without a tree hash before processing items", func() {
		executor := NewExecutor(
			&PlanResult{TreeHash: "h1", Items: []PlanItem{{SourcePath: "a.md", Action: PlanActionCreate}}},
			&PlanOptions{SourceBasePath: importerTempDir()},
			0,
			&fakeExecWiki{hash: "h1"},
			slog.Default(),
		).WithResumeState(1, nil)

		result, err := executor.Execute("user-1")
		Expect(result).To(BeNil())
		Expect(err).To(MatchError(ErrImportResumeTreeHashMissing))
	})

	ginkgo.It("honors cancellation before the next item is processed", func() {
		executor := NewExecutor(
			&PlanResult{
				TreeHash: "h1",
				Items: []PlanItem{
					{SourcePath: "a.md", TargetPath: "a", Title: "A", Kind: tree.NodeKindPage, Action: PlanActionCreate},
				},
			},
			&PlanOptions{SourceBasePath: importerTempDir()},
			0,
			&fakeExecWiki{hash: "h1"},
			slog.Default(),
		).WithCancelCheck(func() bool {
			return true
		})

		result, err := executor.Execute("user-1")
		Expect(err).To(MatchError(ErrImportCanceled))
		Expect(result).To(MatchExecutionResultCounts(0, 0, BeEmpty()))
	})

	ginkgo.It("skips create items when page creation, source loading, or page update fails", func() {
		tmp := importerTempDir()
		importerWriteFile(tmp, "update.md", "# Update")
		updateFailedErr := errors.New("update failed")
		wiki := &fakeExecWiki{
			hash: "h1",
			ensureFn: func(userID tree.UserID, targetPath tree.RoutePath, title string, kind *tree.NodeKind) (*tree.Page, error) {
				if targetPath == "nil-page" {
					return nil, nil
				}
				return &tree.Page{PageNode: &tree.PageNode{ID: "p1", Title: title, Slug: "slug", Kind: *kind}}, nil
			},
			updateFn: func(userID tree.UserID, id tree.PageID, title string, slug tree.Slug, content *string, kind *tree.NodeKind) (*tree.Page, error) {
				return nil, updateFailedErr
			},
		}

		executor := NewExecutor(
			&PlanResult{
				TreeHash: "h1",
				Items: []PlanItem{
					{SourcePath: "nil.md", TargetPath: "nil-page", Title: "Nil", Kind: tree.NodeKindPage, Action: PlanActionCreate},
					{SourcePath: "missing.md", TargetPath: "missing", Title: "Missing", Kind: tree.NodeKindPage, Action: PlanActionCreate},
					{SourcePath: "update.md", TargetPath: "update", Title: "Update", Kind: tree.NodeKindPage, Action: PlanActionCreate},
				},
			},
			&PlanOptions{SourceBasePath: tmp},
			0,
			wiki,
			slog.Default(),
		)

		result, err := executor.Execute("user-1")
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(MatchExecutionResultCounts(0, 3, ConsistOf(
			HaveExecutionItemErrorCode(ImportErrorCodeCreatePageFailed),
			HaveExecutionItemErrorCode(ImportErrorCodeLoadSourceFailed),
			HaveExecutionItemErrorCode(ImportErrorCodeUpdatePageFailed),
		)))
	})

	ginkgo.It("skips create items when transformation or imported-content rendering fails", func() {
		tmp := importerTempDir()
		importerWriteFile(tmp, "asset-error.md", "![Asset](./missing.png)")
		importerWriteFile(tmp, "render-error.md", "# Render Error")
		importerWriteFile(tmp, "missing.png", "png-bytes")

		uploadFailedErr := errors.New("upload failed")
		wiki := &fakeExecWiki{
			hash: "h1",
			ensureFn: func(userID tree.UserID, targetPath tree.RoutePath, title string, kind *tree.NodeKind) (*tree.Page, error) {
				pageID := newFixturePageID("p1")
				if targetPath == "render-error" {
					pageID = "."
				}
				return &tree.Page{PageNode: &tree.PageNode{ID: pageID, Title: title, Slug: "slug", Kind: *kind}}, nil
			},
			uploadFn: func(userID tree.UserID, pageID tree.PageID, file multipart.File, filename tree.AssetName, byteCap shared.MaxBytes) (string, error) {
				return "", uploadFailedErr
			},
		}

		executor := NewExecutor(
			&PlanResult{
				TreeHash: "h1",
				Items: []PlanItem{
					{SourcePath: "asset-error.md", TargetPath: "asset-error", Title: "Asset Error", Kind: tree.NodeKindPage, Action: PlanActionCreate},
					{SourcePath: "render-error.md", TargetPath: "render-error", Title: "Render Error", Kind: tree.NodeKindPage, Action: PlanActionCreate},
				},
			},
			&PlanOptions{SourceBasePath: tmp},
			0,
			wiki,
			slog.Default(),
		)

		result, err := executor.Execute("user-1")
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(MatchExecutionResultCounts(0, 2, ConsistOf(
			HaveExecutionItemErrorCode(ImportErrorCodeTransformContentFailed),
			HaveExecutionItemErrorCode(ImportErrorCodeRenderImportedContent),
		)))
	})

	ginkgo.It("fills a missing TreeHashBefore when resuming from partial results", func() {
		tmp := importerTempDir()
		importerWriteFile(tmp, "resume.md", "# Resume")
		progresses := []ExecutionProgress{}

		executor := NewExecutor(
			&PlanResult{
				TreeHash: "original",
				Items: []PlanItem{
					{SourcePath: "resume.md", TargetPath: "resume", Title: "Resume", Kind: tree.NodeKindPage, Action: PlanActionCreate},
				},
			},
			&PlanOptions{SourceBasePath: tmp},
			0,
			&fakeExecWiki{hash: "partial"},
			slog.Default(),
		).WithResumeState(1, &ExecutionResult{
			TreeHash: "partial",
		}).WithProgressCallback(func(progress ExecutionProgress, result *ExecutionResult) {
			progresses = append(progresses, progress)
			Expect(result.TreeHashBefore).To(Equal("partial"))
		})

		result, err := executor.Execute("user-1")
		Expect(err).NotTo(HaveOccurred())
		Expect(result.TreeHashBefore).To(Equal("partial"))
		Expect(progresses).To(HaveExactElements(
			gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"ProcessedItems": Equal(1),
				"TotalItems":     Equal(1),
				"StartedAt":      Not(BeNil()),
			}),
			gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"ProcessedItems": Equal(1),
				"TotalItems":     Equal(1),
				"FinishedAt":     Not(BeNil()),
			}),
		))
	})
})

var _ = ginkgo.Describe("PlanStore cancellation", func() {
	ginkgo.It("records a running plan cancel request and is idempotent", func() {
		store := NewPlanStore()
		Expect(store.Set(&StoredPlan{
			Plan:            &PlanResult{ID: "plan-1"},
			ExecutionStatus: ExecutionStatusRunning,
		})).To(Succeed())

		plan, err := requestCancelResult(store)
		Expect(err).NotTo(HaveOccurred())
		Expect(plan.CancelRequested).To(BeTrue())
		Expect(store.IsCancelRequested("plan-1")).To(BeTrue())

		plan, err = requestCancelResult(store)
		Expect(err).To(MatchError(errImporterCancellationNotRequested))
		Expect(plan.CancelRequested).To(BeTrue())
	})

	ginkgo.It("does not request cancellation for a non-running plan", func() {
		store := NewPlanStore()
		Expect(store.Set(&StoredPlan{
			Plan:            &PlanResult{ID: "plan-1"},
			ExecutionStatus: ExecutionStatusPlanned,
		})).To(Succeed())

		plan, err := requestCancelResult(store)
		Expect(err).To(MatchError(errImporterCancellationNotRequested))
		Expect(plan.CancelRequested).To(BeFalse())
	})
})

var _ = ginkgo.Describe("PlanStore execution state edges", func() {
	ginkgo.It("starts planned execution by resetting stale execution state", func() {
		store := NewPlanStore()

		_, err := startStoredPlanExecutionResult(store, "user-1")
		Expect(err).To(MatchError(ErrNoPlan))

		errMsg := "previous failure"
		currentSource := "old.md"
		Expect(store.Set(&StoredPlan{
			Plan: &PlanResult{
				ID: "plan-1",
				Items: []PlanItem{
					{SourcePath: "a.md"},
					{SourcePath: "b.md"},
				},
			},
			ExecutionStatus: ExecutionStatusFailed,
			ExecutionUserID: "old-user",
			CancelRequested: true,
			ExecutionResult: &ExecutionResult{ImportedCount: 9},
			ExecutionError:  &errMsg,
			ExecutionProgress: ExecutionProgress{
				ProcessedItems:        7,
				TotalItems:            7,
				CurrentItemSourcePath: &currentSource,
			},
		})).To(Succeed())

		plan, err := startStoredPlanExecutionResult(store, "user-1")
		Expect(err).NotTo(HaveOccurred())
		Expect(plan).To(gstruct.PointTo(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"ExecutionStatus": Equal(ExecutionStatusRunning),
			"ExecutionUserID": Equal("user-1"),
			"CancelRequested": BeFalse(),
			"ExecutionResult": BeNil(),
			"ExecutionError":  BeNil(),
			"ExecutionProgress": gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"ProcessedItems": BeZero(),
				"TotalItems":     Equal(2),
				"StartedAt":      Not(BeNil()),
			}),
		})))

		plan, err = startStoredPlanExecutionResult(store, "user-2")
		Expect(err).To(MatchError(errImporterExecutionNotStarted))
		Expect(plan.ExecutionUserID).To(Equal("user-1"))
	})

	ginkgo.It("finishes execution with completed, failed, and canceled terminal states", func() {
		currentSource := "current.md"
		store := NewPlanStore()
		Expect(store.Set(&StoredPlan{
			Plan:            &PlanResult{ID: "plan-1"},
			ExecutionStatus: ExecutionStatusRunning,
			ExecutionProgress: ExecutionProgress{
				ProcessedItems:        1,
				TotalItems:            2,
				CurrentItemSourcePath: &currentSource,
			},
		})).To(Succeed())

		Expect(store.FinishExecution("other-plan", nil, errors.New("ignored"))).To(Succeed())
		state, err := store.Get()
		Expect(err).NotTo(HaveOccurred())
		Expect(state.ExecutionStatus).To(Equal(ExecutionStatusRunning))

		result := &ExecutionResult{ImportedCount: 1}
		Expect(store.FinishExecution("plan-1", result, ErrImportCanceled)).To(Succeed())
		state, err = store.Get()
		Expect(err).NotTo(HaveOccurred())
		Expect(state).To(gstruct.PointTo(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"ExecutionStatus": Equal(ExecutionStatusCanceled),
			"ExecutionResult": Equal(result),
			"ExecutionError":  BeNil(),
			"CancelRequested": BeFalse(),
			"ExecutionProgress": gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"FinishedAt":            Not(BeNil()),
				"CurrentItemSourcePath": BeNil(),
			}),
		})))

		failed := NewPlanStore()
		Expect(failed.Set(&StoredPlan{Plan: &PlanResult{ID: "plan-2"}, ExecutionStatus: ExecutionStatusRunning})).To(Succeed())
		Expect(failed.FinishExecution("plan-2", nil, errors.New("boom"))).To(Succeed())
		failedState, err := failed.Get()
		Expect(err).NotTo(HaveOccurred())
		Expect(failedState).To(gstruct.PointTo(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"ExecutionStatus": Equal(ExecutionStatusFailed),
			"ExecutionError":  gstruct.PointTo(Equal("boom")),
		})))

		completed := NewPlanStore()
		Expect(completed.Set(&StoredPlan{
			Plan:            &PlanResult{ID: "plan-3"},
			ExecutionStatus: ExecutionStatusRunning,
			ExecutionProgress: ExecutionProgress{
				ProcessedItems: 1,
				TotalItems:     3,
			},
		})).To(Succeed())
		Expect(completed.FinishExecution("plan-3", &ExecutionResult{ImportedCount: 3}, nil)).To(Succeed())
		completedState, err := completed.Get()
		Expect(err).NotTo(HaveOccurred())
		Expect(completedState).To(gstruct.PointTo(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"ExecutionStatus": Equal(ExecutionStatusCompleted),
			"ExecutionProgress": gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"ProcessedItems": Equal(3),
				"FinishedAt":     Not(BeNil()),
			}),
		})))
	})

	ginkgo.It("updates progress only for the active plan and clones partial results", func() {
		store := NewPlanStore()
		Expect(store.Set(&StoredPlan{
			Plan:            &PlanResult{ID: "plan-1"},
			ExecutionStatus: ExecutionStatusRunning,
		})).To(Succeed())

		now := time.Now()
		sourcePath := "docs/current.md"
		partial := &ExecutionResult{
			ImportedCount: 1,
			Items: []ExecutionItemResult{
				{SourcePath: "docs/old.md", TargetPath: "docs/old", Action: ExecutionActionCreated},
			},
		}

		Expect(store.UpdateExecutionProgress("other-plan", ExecutionProgress{ProcessedItems: 9, TotalItems: 9}, partial)).To(Succeed())
		state, err := store.Get()
		Expect(err).NotTo(HaveOccurred())
		Expect(state).To(gstruct.PointTo(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"ExecutionProgress": gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"ProcessedItems": BeZero(),
			}),
			"ExecutionResult": BeNil(),
		})))

		Expect(store.UpdateExecutionProgress("plan-1", ExecutionProgress{
			ProcessedItems:        1,
			TotalItems:            2,
			CurrentItemSourcePath: &sourcePath,
			StartedAt:             &now,
			FinishedAt:            &now,
		}, partial)).To(Succeed())
		partial.Items[0].Action = ExecutionActionSkipped

		state, err = store.Get()
		Expect(err).NotTo(HaveOccurred())
		Expect(state).To(gstruct.PointTo(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"ExecutionProgress": gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"ProcessedItems":        Equal(1),
				"TotalItems":            Equal(2),
				"CurrentItemSourcePath": Equal(&sourcePath),
				"StartedAt":             gstruct.PointTo(BeTemporally("==", now)),
				"FinishedAt":            gstruct.PointTo(BeTemporally("==", now)),
			}),
			"ExecutionResult": gstruct.PointTo(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"Items": ContainElement(HaveField("Action", Equal(ExecutionActionCreated))),
			})),
		})))
	})
})

var _ = ginkgo.Describe("PlanStore persistence edge cases", func() {
	ginkgo.It("turns set persistence failures into sticky state errors", func() {
		store := NewPlanStore()
		store.stateFile = importerBadStateFile()

		err := store.Set(&StoredPlan{Plan: &PlanResult{ID: "plan-1"}})
		Expect(err).To(MatchError(ErrImportStateUnavailable))

		_, err = store.Get()
		Expect(err).To(MatchError(ErrImportStateUnavailable))
	})

	ginkgo.It("reports persisted load errors and accepts disabled persistence", func() {
		store := NewPlanStore(importerBadStateFile())
		_, err := store.Get()
		Expect(err).To(MatchError(ErrImportStateUnavailable))

		noPersistence := NewPlanStore("")
		Expect(noPersistence.Set(&StoredPlan{Plan: &PlanResult{ID: "plan-1"}})).To(Succeed())
	})

	ginkgo.It("surfaces persistence failures from mutating execution operations", func() {
		type mutationCase struct {
			name   string
			setup  func(*StoredPlan)
			mutate func(*PlanStore) error
		}

		cases := []mutationCase{
			{
				name: "clear",
				setup: func(plan *StoredPlan) {
					plan.ExecutionStatus = ExecutionStatusPlanned
				},
				mutate: func(store *PlanStore) error {
					_, err := store.Clear()
					return err
				},
			},
			{
				name: "try start",
				setup: func(plan *StoredPlan) {
					plan.ExecutionStatus = ExecutionStatusPlanned
				},
				mutate: func(store *PlanStore) error {
					_, err := startStoredPlanExecutionResult(store, "user-1")
					return err
				},
			},
			{
				name: "finish failed",
				setup: func(plan *StoredPlan) {
					plan.ExecutionStatus = ExecutionStatusRunning
				},
				mutate: func(store *PlanStore) error {
					return store.FinishExecution("plan-1", nil, errors.New("boom"))
				},
			},
			{
				name: "update progress",
				setup: func(plan *StoredPlan) {
					plan.ExecutionStatus = ExecutionStatusRunning
				},
				mutate: func(store *PlanStore) error {
					return store.UpdateExecutionProgress("plan-1", ExecutionProgress{ProcessedItems: 1}, nil)
				},
			},
			{
				name: "request cancel",
				setup: func(plan *StoredPlan) {
					plan.ExecutionStatus = ExecutionStatusRunning
				},
				mutate: func(store *PlanStore) error {
					_, err := requestCancelResult(store)
					return err
				},
			},
		}

		for _, tt := range cases {
			tt := tt
			store := NewPlanStore()
			store.stateFile = importerBadStateFile()
			store.plan = &StoredPlan{Plan: &PlanResult{ID: "plan-1"}}
			tt.setup(store.plan)

			err := tt.mutate(store)
			Expect(err).To(MatchError(ErrImportStateUnavailable), tt.name)
		}
	})

	ginkgo.It("returns sticky state errors from every public mutation", func() {
		store := NewPlanStore()
		store.stateErr = ErrImportStateUnavailable

		Expect(store.Set(&StoredPlan{})).To(MatchError(ErrImportStateUnavailable))
		_, err := store.Clear()
		Expect(err).To(MatchError(ErrImportStateUnavailable))
		_, err = startStoredPlanExecutionResult(store, "user-1")
		Expect(err).To(MatchError(ErrImportStateUnavailable))
		Expect(store.FinishExecution("plan-1", nil, nil)).To(MatchError(ErrImportStateUnavailable))
		Expect(store.UpdateExecutionProgress("plan-1", ExecutionProgress{}, nil)).To(MatchError(ErrImportStateUnavailable))
		_, err = requestCancelResult(store)
		Expect(err).To(MatchError(ErrImportStateUnavailable))
	})

	ginkgo.It("surfaces terminal-state persistence failures", func() {
		for _, tt := range []struct {
			name string
			err  error
		}{
			{name: "canceled", err: ErrImportCanceled},
			{name: "completed", err: nil},
		} {
			store := NewPlanStore()
			store.stateFile = importerBadStateFile()
			store.plan = &StoredPlan{
				Plan:            &PlanResult{ID: "plan-1"},
				ExecutionStatus: ExecutionStatusRunning,
			}

			err := store.FinishExecution("plan-1", &ExecutionResult{}, tt.err)
			Expect(err).To(MatchError(ErrImportStateUnavailable), tt.name)
		}
	})

	ginkgo.It("clears missing state files and clones nil state safely", func() {
		stateFile := filepath.Join(importerTempDir(), "missing", "current-plan.json")
		store := NewPlanStore(stateFile)

		old, err := store.Clear()
		Expect(err).NotTo(HaveOccurred())
		Expect(old).To(BeNil())
		Expect(cloneStoredPlan(nil)).To(BeNil())
		Expect(cloneExecutionResult(nil)).To(BeNil())
	})

	ginkgo.It("surfaces state-file removal failures when clearing an empty plan", func() {
		stateFile := filepath.Join(importerTempDir(), "current-plan.json")
		Expect(os.MkdirAll(stateFile, 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(stateFile, "child"), []byte("x"), 0o644)).To(Succeed())

		store := NewPlanStore()
		store.stateFile = stateFile

		old, err := store.Clear()
		Expect(old).To(BeNil())
		Expect(err).To(MatchError(ErrImportStateUnavailable))
	})

	ginkgo.It("surfaces state-file write failures", func() {
		stateDir := filepath.Join(importerTempDir(), "readonly")
		Expect(os.MkdirAll(stateDir, 0o755)).To(Succeed())
		Expect(os.Chmod(stateDir, 0o555)).To(Succeed())
		ginkgo.DeferCleanup(func() {
			_ = os.Chmod(stateDir, 0o755)
		})

		store := NewPlanStore()
		store.stateFile = filepath.Join(stateDir, "current-plan.json")

		err := store.Set(&StoredPlan{Plan: &PlanResult{ID: "plan-1"}})
		Expect(err).To(MatchError(ErrImportStateUnavailable))
	})

	ginkgo.It("surfaces JSON marshal failures from invalid persisted times", func() {
		store := NewPlanStore()
		store.stateFile = filepath.Join(importerTempDir(), "current-plan.json")

		err := store.Set(&StoredPlan{
			Plan:      &PlanResult{ID: "plan-1"},
			CreatedAt: time.Date(10000, time.January, 1, 0, 0, 0, 0, time.UTC),
		})
		Expect(err).To(MatchError(ErrImportStateUnavailable))
	})
})

var _ = ginkgo.Describe("ImporterService execution state edges", func() {
	ginkgo.It("returns stored terminal states that do not need a new executor", func() {
		service := newServiceWithFakeWiki(&fakeWiki{treeHash: "h1", lookups: map[string]*tree.PathLookup{}})
		result := &ExecutionResult{ImportedCount: 2}
		Expect(service.planStore.Set(&StoredPlan{
			Plan:            &PlanResult{ID: "plan-1"},
			ExecutionStatus: ExecutionStatusCompleted,
			ExecutionResult: result,
		})).To(Succeed())

		got, err := service.ExecuteCurrentPlan("user-1")
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(Equal(result))

		Expect(service.planStore.Set(&StoredPlan{
			Plan:            &PlanResult{ID: "plan-2"},
			ExecutionStatus: ExecutionStatusCompleted,
		})).To(Succeed())
		_, err = service.ExecuteCurrentPlan("user-1")
		Expect(err).To(MatchError(ErrImportCompletedResultMissing))

		Expect(service.planStore.Set(&StoredPlan{
			Plan:            &PlanResult{ID: "plan-3"},
			ExecutionStatus: ExecutionStatusRunning,
		})).To(Succeed())
		_, err = service.ExecuteCurrentPlan("user-1")
		Expect(err).To(MatchError(ErrImportExecutionRunning))
	})

	ginkgo.It("surfaces cancellation and current-plan state without a stored plan", func() {
		service := newServiceWithFakeWiki(&fakeWiki{treeHash: "h1", lookups: map[string]*tree.PathLookup{}})

		state, err := cancelCurrentPlanResult(service)
		Expect(state).To(BeNil())
		Expect(err).To(MatchError(ErrNoPlan))

		Expect(currentPlanStateFromStored(nil)).To(BeNil())
		Expect(currentPlanStateFromStored(&StoredPlan{})).To(BeNil())
	})

	ginkgo.It("refuses to replace a running folder import plan", func() {
		newWorkspace := importerTempDir()
		importerWriteFile(newWorkspace, "next.md", "# Next")
		service := newServiceWithFakeWiki(&fakeWiki{treeHash: "h1", lookups: map[string]*tree.PathLookup{}})

		Expect(service.planStore.Set(&StoredPlan{
			Plan:            &PlanResult{ID: "running"},
			WorkspaceRoot:   importerTempDir(),
			ExecutionStatus: ExecutionStatusRunning,
		})).To(Succeed())

		_, err := service.CreateImportPlanFromFolder(newWorkspace, "")
		Expect(err).To(MatchError(ErrImportExecutionRunning))
	})
})

var _ = ginkgo.Describe("ImporterService zip planning", func() {
	ginkgo.It("creates an import plan from an uploaded zip and stores the extracted workspace", func() {
		var zipBytes bytes.Buffer
		zipWriter := zip.NewWriter(&zipBytes)
		file, err := zipWriter.Create("docs/Imported.md")
		Expect(err).NotTo(HaveOccurred())
		_, err = file.Write([]byte("# Imported\nbody"))
		Expect(err).NotTo(HaveOccurred())
		Expect(zipWriter.Close()).To(Succeed())

		service := newServiceWithFakeWiki(&fakeWiki{treeHash: "h1", lookups: map[string]*tree.PathLookup{}})
		plan, err := service.CreateImportPlanFromZipUpload(bytes.NewReader(zipBytes.Bytes()), "wiki")
		Expect(err).NotTo(HaveOccurred())
		Expect(plan.Items).To(ConsistOf(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"SourcePath": Equal(newFixtureWorkspaceSourcePath("docs/Imported.md")),
			"TargetPath": Equal(newFixtureRoutePath("wiki/docs/imported")),
		})))

		state, err := service.planStore.Get()
		Expect(err).NotTo(HaveOccurred())
		Expect(state).To(gstruct.PointTo(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"WorkspaceRoot": SatisfyAll(
				Not(BeEmpty()),
				HavePrefix(service.workspaceBaseDir),
			),
		})))
		Expect(filepath.Base(state.WorkspaceRoot)).To(HavePrefix("import-"))
		Expect(os.ReadFile(filepath.Join(state.WorkspaceRoot, "docs", "Imported.md"))).To(Equal([]byte("# Imported\nbody")))
	})

	ginkgo.It("honors positive asset max-size overrides and ignores non-positive values", func() {
		service := newServiceWithFakeWiki(&fakeWiki{treeHash: "h1", lookups: map[string]*tree.PathLookup{}})

		Expect(service.assetMaxUploadSizeBytes).To(Equal(shared.MaxBytes(0)))
		service.SetAssetMaxUploadSizeBytes(shared.MaxBytes(2048))
		Expect(service.assetMaxUploadSizeBytes).To(Equal(shared.MaxBytes(2048)))
		service.SetAssetMaxUploadSizeBytes(0)
		Expect(service.assetMaxUploadSizeBytes).To(Equal(shared.MaxBytes(2048)))

		constructed := NewImporterService(newPlannerWithFake(&fakeWiki{}), NewPlanStore(), "", 0)
		Expect(importerServiceDefaultState(constructed)).To(Equal(importerServiceDefaults{
			assetMaxUploadSizeBytes: assets.DefaultMaxUploadSizeBytes,
			workspaceBaseDirPresent: true,
		}))
	})

	ginkgo.It("reports invalid zip uploads without storing a plan", func() {
		service := newServiceWithFakeWiki(&fakeWiki{treeHash: "h1", lookups: map[string]*tree.PathLookup{}})

		_, err := service.CreateImportPlanFromZipUpload(bytes.NewReader([]byte("not a zip")), "wiki")
		Expect(err).To(MatchError(zip.ErrFormat))

		_, err = service.GetCurrentPlan()
		Expect(err).To(MatchError(ErrNoPlan))
	})
})

var _ = ginkgo.Describe("Planner error edges", func() {
	ginkgo.It("collects filename normalization and lookup errors", func() {
		tmp := importerTempDir()
		importerWriteFile(tmp, "!!!.md", "# Invalid")
		importerWriteFile(tmp, "lookup.md", "# Lookup")

		planner := newPlannerWithFake(&fakeWiki{treeHash: "h1", lookups: map[string]*tree.PathLookup{}})
		plan, err := planner.CreatePlan([]ImportMDFile{{SourcePath: "!!!.md"}}, PlanOptions{SourceBasePath: tmp})
		Expect(err).NotTo(HaveOccurred())
		Expect(plan.ErrorDetails).To(HaveImportPlanErrorCode(ImportErrorCodeNormalizeFilenameFailed))

		planner = newPlannerWithFake(&fakeWiki{
			treeHash:         "h1",
			lookups:          map[string]*tree.PathLookup{},
			lookupForKindErr: errors.New("lookup failed"),
		})
		plan, err = planner.CreatePlan([]ImportMDFile{{SourcePath: "lookup.md"}}, PlanOptions{SourceBasePath: tmp})
		Expect(err).NotTo(HaveOccurred())
		Expect(plan.ErrorDetails).To(HaveImportPlanErrorCode(ImportErrorCodeLookupPathFailed))
	})
})

var _ = ginkgo.Describe("ImporterService error edges", func() {
	ginkgo.It("surfaces folder planning cleanup, discovery, planning, and persistence errors", func() {
		service := newServiceWithFakeWiki(&fakeWiki{treeHash: "h1", lookups: map[string]*tree.PathLookup{}})

		_, err := service.CreateImportPlanFromFolder(filepath.Join(importerTempDir(), "missing"), "")
		Expect(err).To(MatchError(os.ErrNotExist))

		workspace := importerTempDir()
		importerWriteFile(workspace, "page.md", "# Page")

		originalGenerateID := importerGenerateUniqueID
		ginkgo.DeferCleanup(func() {
			importerGenerateUniqueID = originalGenerateID
		})
		idFailedErr := errors.New("id failed")
		importerGenerateUniqueID = func() (string, error) {
			return "", idFailedErr
		}
		_, err = service.CreateImportPlanFromFolder(workspace, "")
		Expect(err).To(MatchError(idFailedErr))
		importerGenerateUniqueID = originalGenerateID

		service.planStore.stateFile = importerBadStateFile()
		_, err = service.CreateImportPlanFromFolder(workspace, "")
		Expect(err).To(MatchError(ErrImportStateUnavailable))

		service = newServiceWithFakeWiki(&fakeWiki{treeHash: "h1", lookups: map[string]*tree.PathLookup{}})
		Expect(service.planStore.Set(&StoredPlan{
			Plan:            &PlanResult{ID: "old"},
			WorkspaceRoot:   importerTempDir(),
			ExecutionStatus: ExecutionStatusPlanned,
		})).To(Succeed())
		service.planStore.stateFile = importerBadStateFile()
		_, err = service.CreateImportPlanFromFolder(workspace, "")
		Expect(err).To(MatchError(ErrImportStateUnavailable))

		service = newServiceWithFakeWiki(&fakeWiki{treeHash: "h1", lookups: map[string]*tree.PathLookup{}})
		Expect(service.planStore.Set(&StoredPlan{
			Plan:            &PlanResult{ID: "old"},
			WorkspaceRoot:   importerTempDir(),
			ExecutionStatus: ExecutionStatusPlanned,
		})).To(Succeed())
		originalRemoveAll := importerServiceRemoveAll
		ginkgo.DeferCleanup(func() {
			importerServiceRemoveAll = originalRemoveAll
		})
		removeFailedErr := errors.New("remove failed")
		importerServiceRemoveAll = func(path string) error {
			return removeFailedErr
		}
		_, err = service.CreateImportPlanFromFolder(workspace, "")
		Expect(err).To(MatchError(removeFailedErr))
		importerServiceRemoveAll = originalRemoveAll
	})

	ginkgo.It("logs clear and cleanup removal failures while preserving clear semantics", func() {
		service := newServiceWithFakeWiki(&fakeWiki{treeHash: "h1", lookups: map[string]*tree.PathLookup{}})
		Expect(service.planStore.Set(&StoredPlan{
			Plan:            &PlanResult{ID: "plan-1"},
			WorkspaceRoot:   importerTempDir(),
			ExecutionStatus: ExecutionStatusPlanned,
		})).To(Succeed())

		originalRemoveAll := importerServiceRemoveAll
		ginkgo.DeferCleanup(func() {
			importerServiceRemoveAll = originalRemoveAll
		})
		importerServiceRemoveAll = func(path string) error {
			return errors.New("remove failed")
		}
		Expect(service.ClearCurrentPlan()).To(Succeed())
		service.cleanupWorkspace("")
		service.cleanupWorkspace("missing")
		importerServiceRemoveAll = originalRemoveAll
	})

	ginkgo.It("surfaces execution start and finish persistence errors", func() {
		service := newServiceWithFakeWiki(&fakeWiki{treeHash: "h1", lookups: map[string]*tree.PathLookup{}})
		service.planStore.stateErr = ErrImportStateUnavailable
		_, err := startCurrentPlanExecutionResult(service, "user-1")
		Expect(err).To(MatchError(ErrImportStateUnavailable))

		workspace := importerTempDir()
		importerWriteFile(workspace, "page.md", "# Page")
		wiki := &fakeWiki{treeHash: "h1", lookups: map[string]*tree.PathLookup{}}
		service = newServiceWithFakeWiki(wiki)
		wiki.updateFn = func(userID tree.UserID, id tree.PageID, title string, slug tree.Slug, content *string, kind *tree.NodeKind) (*tree.Page, error) {
			service.planStore.stateFile = importerBadStateFile()
			return &tree.Page{PageNode: &tree.PageNode{ID: id, Title: title, Slug: slug, Kind: *kind}}, nil
		}
		plan, err := service.CreateImportPlanFromFolder(workspace, "")
		Expect(err).NotTo(HaveOccurred())

		_, err = service.ExecuteCurrentPlan("user-1")
		Expect(err).To(MatchError(ErrImportStateUnavailable))
		Expect(plan).NotTo(BeNil())
	})

	ginkgo.It("records async finish and progress persistence failures", func() {
		workspace := importerTempDir()
		importerWriteFile(workspace, "page.md", "# Page")
		finished := make(chan struct{})
		wiki := &fakeWiki{treeHash: "h1", lookups: map[string]*tree.PathLookup{}}
		service := newServiceWithFakeWiki(wiki)
		wiki.updateErr = nil
		wiki.ensureFn = func(userID tree.UserID, targetPath tree.RoutePath, title string, kind *tree.NodeKind) (*tree.Page, error) {
			service.planStore.stateFile = importerBadStateFile()
			close(finished)
			return &tree.Page{PageNode: &tree.PageNode{ID: "p1", Title: title, Slug: "slug", Kind: *kind}}, nil
		}

		_, err := service.CreateImportPlanFromFolder(workspace, "")
		Expect(err).NotTo(HaveOccurred())
		_, err = startCurrentPlanExecutionResult(service, "user-1")
		Expect(err).NotTo(HaveOccurred())
		Eventually(finished).Should(BeClosed())
		Eventually(func() error {
			_, err := service.planStore.Get()
			return err
		}).Should(MatchError(ErrImportStateUnavailable))

		service = newServiceWithFakeWiki(&fakeWiki{treeHash: "h1", lookups: map[string]*tree.PathLookup{}})
		service.planStore.stateErr = ErrImportStateUnavailable
		result, err := service.executeStoredPlan(&StoredPlan{
			Plan: &PlanResult{TreeHash: "h1", Items: []PlanItem{
				{SourcePath: "skipped.md", TargetPath: "skipped", Action: PlanActionSkip},
			}},
			PlanOptions: PlanOptions{SourceBasePath: importerTempDir()},
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(result.SkippedCount).To(Equal(1))
	})

	ginkgo.It("orders markdown discovery and reports filesystem failures", func() {
		base := importerTempDir()
		importerWriteFile(base, "z.md", "# Z")
		importerWriteFile(base, "index.md", "# Index")

		entries, err := FindMarkdownEntries(base)
		Expect(err).NotTo(HaveOccurred())
		Expect(entries[0].SourcePath.FilesystemPath()).To(Equal("index.md"))

		_, err = FindMarkdownEntries(filepath.Join(importerTempDir(), "missing"))
		Expect(err).To(MatchError(os.ErrNotExist))

		originalRel := importerServiceRel
		ginkgo.DeferCleanup(func() {
			importerServiceRel = originalRel
		})
		relErr := errors.New("rel failed")
		importerServiceRel = func(basepath, targpath string) (string, error) {
			return "", relErr
		}
		_, err = FindMarkdownEntries(base)
		Expect(err).To(MatchError(relErr))
	})

	ginkgo.It("cleans up extracted zip workspaces when plan creation fails", func() {
		var zipBytes bytes.Buffer
		zipWriter := zip.NewWriter(&zipBytes)
		file, err := zipWriter.Create("page.md")
		Expect(err).NotTo(HaveOccurred())
		_, err = file.Write([]byte("# Page"))
		Expect(err).NotTo(HaveOccurred())
		Expect(zipWriter.Close()).To(Succeed())

		service := newServiceWithFakeWiki(&fakeWiki{treeHash: "h1", lookups: map[string]*tree.PathLookup{}})
		originalGenerateID := importerGenerateUniqueID
		ginkgo.DeferCleanup(func() {
			importerGenerateUniqueID = originalGenerateID
		})
		idFailedErr := errors.New("id failed")
		importerGenerateUniqueID = func() (string, error) {
			return "", idFailedErr
		}
		_, err = service.CreateImportPlanFromZipUpload(bytes.NewReader(zipBytes.Bytes()), "")
		Expect(err).To(MatchError(idFailedErr))

		originalWorkspaceRemoveAll := zipWorkspaceRemoveAll
		ginkgo.DeferCleanup(func() {
			zipWorkspaceRemoveAll = originalWorkspaceRemoveAll
		})
		cleanupFailedErr := errors.New("cleanup failed")
		zipWorkspaceRemoveAll = func(path string) error {
			return cleanupFailedErr
		}
		_, err = service.CreateImportPlanFromZipUpload(bytes.NewReader(zipBytes.Bytes()), "")
		Expect(err).To(MatchError(idFailedErr))
	})

	ginkgo.It("surfaces temp zip storage seam failures", func() {
		service := newServiceWithFakeWiki(&fakeWiki{treeHash: "h1", lookups: map[string]*tree.PathLookup{}})

		originalMkdirAll := importerServiceMkdirAll
		ginkgo.DeferCleanup(func() {
			importerServiceMkdirAll = originalMkdirAll
		})
		mkdirFailedErr := errors.New("mkdir failed")
		importerServiceMkdirAll = func(path string, perm os.FileMode) error {
			return mkdirFailedErr
		}
		_, err := service.extractZipReaderToTemp(bytes.NewReader(nil))
		Expect(err).To(MatchError(mkdirFailedErr))
		importerServiceMkdirAll = originalMkdirAll

		originalCreateTmp := importerServiceCreateTmp
		ginkgo.DeferCleanup(func() {
			importerServiceCreateTmp = originalCreateTmp
		})
		createTempFailedErr := errors.New("create temp failed")
		importerServiceCreateTmp = func(dir, pattern string) (*os.File, error) {
			return nil, createTempFailedErr
		}
		_, err = service.extractZipReaderToTemp(bytes.NewReader(nil))
		Expect(err).To(MatchError(createTempFailedErr))
		importerServiceCreateTmp = originalCreateTmp

		originalCopy := importerServiceCopy
		ginkgo.DeferCleanup(func() {
			importerServiceCopy = originalCopy
		})
		copyFailedErr := errors.New("copy failed")
		importerServiceCopy = func(dst io.Writer, src io.Reader) (int64, error) {
			return 0, copyFailedErr
		}
		_, err = service.extractZipReaderToTemp(bytes.NewReader(nil))
		Expect(err).To(MatchError(copyFailedErr))
		importerServiceCopy = originalCopy

		originalRemove := importerServiceRemove
		ginkgo.DeferCleanup(func() {
			importerServiceRemove = originalRemove
		})
		importerServiceRemove = func(name string) error {
			return errors.New("remove failed")
		}
		var zipBytes bytes.Buffer
		zipWriter := zip.NewWriter(&zipBytes)
		file, err := zipWriter.Create("page.md")
		Expect(err).NotTo(HaveOccurred())
		_, err = file.Write([]byte("# Page"))
		Expect(err).NotTo(HaveOccurred())
		Expect(zipWriter.Close()).To(Succeed())
		workspace, err := service.extractZipReaderToTemp(bytes.NewReader(zipBytes.Bytes()))
		Expect(err).NotTo(HaveOccurred())
		ginkgo.DeferCleanup(cleanupZipWorkspaceResult, workspace)
		importerServiceRemove = originalRemove

		originalClose := importerServiceCloseFile
		ginkgo.DeferCleanup(func() {
			importerServiceCloseFile = originalClose
		})
		closeFailedErr := errors.New("close failed")
		importerServiceCloseFile = func(file *os.File) error {
			return closeFailedErr
		}
		_, err = service.extractZipReaderToTemp(bytes.NewReader(zipBytes.Bytes()))
		Expect(err).To(MatchError(closeFailedErr))
	})

	ginkgo.It("resumes startup imports and records finish persistence failures", func() {
		_ = NewImporterService(newPlannerWithFake(&fakeWiki{}), NewPlanStore(importerBadStateFile()), importerTempDir(), 0)

		store := NewPlanStore()
		Expect(store.Set(&StoredPlan{Plan: &PlanResult{ID: "planned"}, ExecutionStatus: ExecutionStatusPlanned})).To(Succeed())
		NewImporterService(newPlannerWithFake(&fakeWiki{}), store, importerTempDir(), 0)

		workspace := importerTempDir()
		importerWriteFile(workspace, "page.md", "# Page")
		stateFile := filepath.Join(importerTempDir(), "current-plan.json")
		store = NewPlanStore(stateFile)
		plan := &PlanResult{
			ID:       "plan-1",
			TreeHash: "h1",
			Items: []PlanItem{
				{SourcePath: "page.md", TargetPath: "page", Title: "Page", Kind: tree.NodeKindPage, Action: PlanActionCreate},
			},
		}
		Expect(store.Set(&StoredPlan{
			Plan:            plan,
			PlanOptions:     PlanOptions{SourceBasePath: workspace},
			WorkspaceRoot:   workspace,
			ExecutionStatus: ExecutionStatusRunning,
		})).To(Succeed())
		wiki := &fakeWiki{treeHash: "h1", lookups: map[string]*tree.PathLookup{}}
		service := NewImporterService(newPlannerWithFake(wiki), NewPlanStore(stateFile), importerTempDir(), 0)
		Eventually(func() (*CurrentPlanState, error) {
			return service.GetCurrentPlan()
		}).Should(And(Not(BeNil()), WithTransform(func(state *CurrentPlanState) ExecutionStatus {
			return state.ExecutionStatus
		}, Equal(ExecutionStatusCompleted))))

		store = NewPlanStore(stateFile + "-finish-error")
		Expect(store.Set(&StoredPlan{
			Plan:            plan,
			PlanOptions:     PlanOptions{SourceBasePath: workspace},
			WorkspaceRoot:   workspace,
			ExecutionStatus: ExecutionStatusRunning,
			ExecutionUserID: "user-1",
		})).To(Succeed())
		store.stateFile = importerBadStateFile()
		service = NewImporterService(newPlannerWithFake(&fakeWiki{treeHash: "h1", lookups: map[string]*tree.PathLookup{}}), store, importerTempDir(), 0)
		Eventually(func() error {
			_, err := service.GetCurrentPlan()
			return err
		}).Should(MatchError(ErrImportStateUnavailable))
	})
})

var _ = ginkgo.Describe("ZipExtractor safety edges", func() {
	ginkgo.It("skips empty and directory entries while extracting regular files", func() {
		var zipBytes bytes.Buffer
		zipWriter := zip.NewWriter(&zipBytes)
		_, err := zipWriter.Create("   ")
		Expect(err).NotTo(HaveOccurred())
		_, err = zipWriter.Create("docs/")
		Expect(err).NotTo(HaveOccurred())
		file, err := zipWriter.Create("docs/page.md")
		Expect(err).NotTo(HaveOccurred())
		_, err = file.Write([]byte("# Page"))
		Expect(err).NotTo(HaveOccurred())
		Expect(zipWriter.Close()).To(Succeed())

		zipPath := filepath.Join(importerTempDir(), "safe.zip")
		Expect(os.WriteFile(zipPath, zipBytes.Bytes(), 0o644)).To(Succeed())

		workspace, err := NewZipExtractor().ExtractToDir(zipPath, importerTempDir())
		Expect(err).NotTo(HaveOccurred())
		ginkgo.DeferCleanup(workspace.Cleanup)
		Expect(os.ReadFile(filepath.Join(workspace.Root, "docs", "page.md"))).To(Equal([]byte("# Page")))
	})

	ginkgo.It("reports base directory creation errors before extracting", func() {
		var zipBytes bytes.Buffer
		zipWriter := zip.NewWriter(&zipBytes)
		file, err := zipWriter.Create("docs/page.md")
		Expect(err).NotTo(HaveOccurred())
		_, err = file.Write([]byte("# Page"))
		Expect(err).NotTo(HaveOccurred())
		Expect(zipWriter.Close()).To(Succeed())

		zipPath := filepath.Join(importerTempDir(), "safe.zip")
		Expect(os.WriteFile(zipPath, zipBytes.Bytes(), 0o644)).To(Succeed())
		baseDir := filepath.Join(importerTempDir(), "not-a-dir")
		Expect(os.WriteFile(baseDir, []byte("file"), 0o644)).To(Succeed())

		workspace, err := NewZipExtractor().ExtractToDir(zipPath, baseDir)
		Expect(workspace).To(BeNil())
		Expect(err).To(MatchError(syscall.ENOTDIR))
	})

	ginkgo.It("reports temp workspace creation errors", func() {
		var zipBytes bytes.Buffer
		zipWriter := zip.NewWriter(&zipBytes)
		file, err := zipWriter.Create("docs/page.md")
		Expect(err).NotTo(HaveOccurred())
		_, err = file.Write([]byte("# Page"))
		Expect(err).NotTo(HaveOccurred())
		Expect(zipWriter.Close()).To(Succeed())

		zipPath := filepath.Join(importerTempDir(), "safe.zip")
		Expect(os.WriteFile(zipPath, zipBytes.Bytes(), 0o644)).To(Succeed())
		baseDir := filepath.Join(importerTempDir(), "readonly")
		Expect(os.MkdirAll(baseDir, 0o755)).To(Succeed())
		Expect(os.Chmod(baseDir, 0o555)).To(Succeed())
		ginkgo.DeferCleanup(func() {
			_ = os.Chmod(baseDir, 0o755)
		})

		workspace, err := NewZipExtractor().ExtractToDir(zipPath, baseDir)
		Expect(workspace).To(BeNil())
		Expect(err).To(MatchError(syscall.EACCES))
	})

	ginkgo.It("reports per-entry directory and file creation failures", func() {
		var zipBytes bytes.Buffer
		zipWriter := zip.NewWriter(&zipBytes)
		file, err := zipWriter.Create("blocked")
		Expect(err).NotTo(HaveOccurred())
		_, err = file.Write([]byte("not a directory"))
		Expect(err).NotTo(HaveOccurred())
		file, err = zipWriter.Create("blocked/page.md")
		Expect(err).NotTo(HaveOccurred())
		_, err = file.Write([]byte("# Page"))
		Expect(err).NotTo(HaveOccurred())
		Expect(zipWriter.Close()).To(Succeed())

		zipPath := filepath.Join(importerTempDir(), "blocked-dir.zip")
		Expect(os.WriteFile(zipPath, zipBytes.Bytes(), 0o644)).To(Succeed())

		workspace, err := NewZipExtractor().ExtractToDir(zipPath, importerTempDir())
		Expect(workspace).To(BeNil())
		Expect(err).To(MatchError(syscall.ENOTDIR))

		zipBytes.Reset()
		zipWriter = zip.NewWriter(&zipBytes)
		file, err = zipWriter.Create("blocked/file.md")
		Expect(err).NotTo(HaveOccurred())
		_, err = file.Write([]byte("# Page"))
		Expect(err).NotTo(HaveOccurred())
		file, err = zipWriter.Create("blocked")
		Expect(err).NotTo(HaveOccurred())
		_, err = file.Write([]byte("not a directory"))
		Expect(err).NotTo(HaveOccurred())
		Expect(zipWriter.Close()).To(Succeed())

		zipPath = filepath.Join(importerTempDir(), "blocked-file.zip")
		Expect(os.WriteFile(zipPath, zipBytes.Bytes(), 0o644)).To(Succeed())

		workspace, err = NewZipExtractor().ExtractToDir(zipPath, importerTempDir())
		Expect(workspace).To(BeNil())
		Expect(err).To(MatchError(syscall.EISDIR))
	})

	ginkgo.It("reports corrupt entry content", func() {
		var zipBytes bytes.Buffer
		zipWriter := zip.NewWriter(&zipBytes)
		header := &zip.FileHeader{Name: "docs/page.md", Method: zip.Store}
		file, err := zipWriter.CreateHeader(header)
		Expect(err).NotTo(HaveOccurred())
		_, err = file.Write([]byte("# Page"))
		Expect(err).NotTo(HaveOccurred())
		Expect(zipWriter.Close()).To(Succeed())

		unsupported := append([]byte(nil), zipBytes.Bytes()...)
		localHeader := bytes.Index(unsupported, []byte{'P', 'K', 3, 4})
		centralHeader := bytes.Index(unsupported, []byte{'P', 'K', 1, 2})
		Expect(localHeader).To(BeNumerically(">=", 0))
		Expect(centralHeader).To(BeNumerically(">=", 0))
		unsupported[localHeader+8] = 99
		unsupported[localHeader+9] = 0
		unsupported[centralHeader+10] = 99
		unsupported[centralHeader+11] = 0
		zipPath := filepath.Join(importerTempDir(), "unsupported.zip")
		Expect(os.WriteFile(zipPath, unsupported, 0o644)).To(Succeed())
		workspace, err := NewZipExtractor().ExtractToDir(zipPath, importerTempDir())
		Expect(workspace).To(BeNil())
		Expect(err).To(MatchError(zip.ErrAlgorithm))

		corrupt := bytes.Replace(zipBytes.Bytes(), []byte("# Page"), []byte("# Paga"), 1)
		zipPath = filepath.Join(importerTempDir(), "corrupt.zip")
		Expect(os.WriteFile(zipPath, corrupt, 0o644)).To(Succeed())

		workspace, err = NewZipExtractor().ExtractToDir(zipPath, importerTempDir())
		Expect(workspace).To(BeNil())
		Expect(err).To(MatchError(zip.ErrChecksum))
	})

	ginkgo.It("rejects unsafe zip entry paths and cleans up the failed workspace", func() {
		baseDir := importerTempDir()
		var zipBytes bytes.Buffer
		zipWriter := zip.NewWriter(&zipBytes)
		file, err := zipWriter.Create("../escape.md")
		Expect(err).NotTo(HaveOccurred())
		_, err = file.Write([]byte("# escape"))
		Expect(err).NotTo(HaveOccurred())
		Expect(zipWriter.Close()).To(Succeed())

		zipPath := filepath.Join(importerTempDir(), "unsafe.zip")
		Expect(os.WriteFile(zipPath, zipBytes.Bytes(), 0o644)).To(Succeed())

		_, err = NewZipExtractor().ExtractToDir(zipPath, baseDir)
		Expect(err).To(SatisfyAll(
			MatchError(ErrImportZipInvalidEntry),
			MatchError(ErrImportZipPathTraversal),
		))

		entries, err := os.ReadDir(baseDir)
		Expect(err).NotTo(HaveOccurred())
		Expect(entries).To(BeEmpty())
	})

	ginkgo.It("keeps safe joined paths inside the extraction root", func() {
		baseDir := filepath.Join(importerTempDir(), "root")

		joined, err := safeJoin(baseDir, "docs/page.md")
		Expect(err).NotTo(HaveOccurred())
		Expect(joined).To(Equal(filepath.Join(baseDir, "docs", "page.md")))

		_, err = safeJoin(baseDir, "../escape.md")
		Expect(err).To(MatchError(ErrImportZipPathTraversal))

		_, err = safeJoin(baseDir, "/absolute.md")
		Expect(err).To(MatchError(ErrImportZipAbsolutePath))
	})

	ginkgo.It("reports missing zip files", func() {
		_, err := NewZipExtractor().ExtractToDir(filepath.Join(importerTempDir(), "missing.zip"), importerTempDir())
		Expect(err).To(MatchError(os.ErrNotExist))
	})

	ginkgo.It("treats nil or empty zip workspaces as already clean", func() {
		var workspace *ZipWorkspace
		Expect(cleanupZipWorkspaceResult(workspace)).To(Succeed())
		Expect(cleanupZipWorkspaceResult(&ZipWorkspace{})).To(Succeed())
	})
})
