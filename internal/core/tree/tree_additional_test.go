package tree

import (
	"errors"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("semantic path helpers", func() {
	ginkgo.It("normalizes markdown paths and derives route/source semantics", func() {
		path := MarkdownPathFromString(" /docs//guide/index.md ")

		Expect(path.Clean()).To(Equal(MarkdownPath("docs/guide/index.md")))
		Expect(path.Clean().Ext()).To(Equal(".md"))
		Expect(path.Clean().IsIndexFile()).To(BeTrue())
		Expect(path.Clean().IsMarkdown()).To(BeTrue())
		Expect(path.Clean().RoutePath()).To(Equal(RoutePath("docs/guide")))
		Expect(path.Clean().SourceDir()).To(Equal(MarkdownPath("docs/guide")))
		Expect(path.Clean().FilesystemPath()).To(Equal("docs/guide/index.md"))
	})

	ginkgo.It("builds route path variants for root, page, and section content", func() {
		root := RoutePathFromString("/")
		route := RoutePathFromString("/Docs/Guide/")

		Expect(root.Clean().IsRoot()).To(BeTrue())
		Expect(root.WikiPath()).To(Equal("/"))
		Expect(root.Child("docs")).To(Equal(RoutePath("docs")))
		Expect(root.MarkdownContentPath(NodeKindSection)).To(Equal(MarkdownPath("index.md")))
		Expect(root.MarkdownPagePath()).To(Equal(MarkdownPath("index.md")))
		Expect(root.WorkspaceSourcePath(NodeKindPage)).To(Equal(WorkspaceSourcePath("")))
		Expect(root.WorkspaceSourcePath(NodeKindSection)).To(Equal(WorkspaceSourcePath("")))

		Expect(route.Clean()).To(Equal(RoutePath("Docs/Guide")))
		Expect(route.WikiPath()).To(Equal("/Docs/Guide"))
		Expect(route.Segments()).To(Equal([]Slug{"Docs", "Guide"}))
		Expect(route.Child("Intro")).To(Equal(RoutePath("Docs/Guide/Intro")))
		Expect(route.WithLeafSlug("Reference")).To(Equal(RoutePath("Docs/Reference")))
		Expect(route.MarkdownContentPath(NodeKindPage)).To(Equal(MarkdownPath("Docs/Guide.md")))
		Expect(route.MarkdownContentPath(NodeKindSection)).To(Equal(MarkdownPath("Docs/Guide/index.md")))
		Expect(route.MarkdownPagePath()).To(Equal(MarkdownPath("Docs/Guide.md")))
		Expect(route.HrefPath()).To(Equal(MarkdownPath("Docs/Guide")))
		Expect(route.LowerKey(NodeKindPage)).To(Equal("page:docs/guide"))
		Expect(route.Lower()).To(Equal(RoutePath("docs/guide")))
		Expect(route.WorkspaceSourceDirectory()).To(Equal(WorkspaceSourcePath("Docs/Guide")))
		Expect(route.LeafSlug()).To(Equal(Slug("Guide")))
		Expect(route.WorkspaceSourcePath(NodeKindPage)).To(Equal(WorkspaceSourcePath("Docs/Guide.md")))
		Expect(route.WorkspaceSourcePath(NodeKindSection)).To(Equal(WorkspaceSourcePath("Docs/Guide")))
	})

	ginkgo.It("scans and stores semantic route and page identifiers", func() {
		pageID := PageID("page-1")
		value, err := pageID.Value()
		Expect(err).NotTo(HaveOccurred())
		Expect(value).To(Equal("page-1"))

		var scannedPageID PageID
		Expect(scannedPageID.Scan([]byte("page-2"))).To(Succeed())
		Expect(scannedPageID).To(Equal(PageID("page-2")))
		Expect(scannedPageID.Scan("page-3")).To(Succeed())
		Expect(scannedPageID).To(Equal(PageID("page-3")))
		Expect(scannedPageID.Scan(nil)).To(Succeed())
		Expect(scannedPageID).To(Equal(PageID("")))
		Expect(scannedPageID.Scan(123)).To(MatchError(ContainSubstring("cannot scan int into PageID")))

		route := RoutePath("/docs/guide/")
		routeValue, err := route.Value()
		Expect(err).NotTo(HaveOccurred())
		Expect(routeValue).To(Equal("/docs/guide"))

		var scannedRoute RoutePath
		Expect(scannedRoute.Scan([]byte("/Docs/Guide/"))).To(Succeed())
		Expect(scannedRoute).To(Equal(RoutePath("/Docs/Guide/")))
		Expect(scannedRoute.Scan("docs/guide")).To(Succeed())
		Expect(scannedRoute).To(Equal(RoutePath("docs/guide")))
		Expect(scannedRoute.Scan(nil)).To(Succeed())
		Expect(scannedRoute).To(Equal(RoutePath("")))
		Expect(scannedRoute.Scan(time.Now())).To(MatchError(ContainSubstring("cannot scan time.Time into RoutePath")))
	})

	ginkgo.It("keeps asset names, slugs, and page versions narrowly typed", func() {
		asset := AssetNameFromString(" logo.png ")
		Expect(asset.Clean()).To(Equal(AssetName("logo.png")))
		Expect(asset.Filename()).To(Equal(" logo.png "))

		slug := SlugFromString("Guide")
		Expect(slug.EqualFold("guide")).To(BeTrue())
		Expect(slug.SlugKey()).To(Equal(SlugKey("guide")))
		Expect(slug.RoutePath()).To(Equal(RoutePath("Guide")))
		Expect(slug.Validate()).To(Succeed())

		now := time.Date(2026, 6, 25, 1, 2, 3, 4, time.FixedZone("offset", 3600))
		Expect(NewPageVersionFromTime(time.Time{})).To(Equal(PageVersion("")))
		Expect(NewPageVersionFromTime(now)).To(Equal(PageVersion("2026-06-25T00:02:03.000000004Z")))
		Expect(PageVersionFromString(versionUnchecked)).To(Equal(PageVersion("")))
	})
})

var _ = ginkgo.Describe("tree helper coverage", func() {
	ginkgo.It("generates a filesystem-style path from page node ancestry", func() {
		root := &PageNode{ID: RootPageID, Slug: "root", Kind: NodeKindSection}
		docs := &PageNode{ID: "docs", Slug: "docs", Kind: NodeKindSection, Parent: root}
		guide := &PageNode{ID: "guide", Slug: "guide", Kind: NodeKindPage, Parent: docs}

		Expect(GeneratePathFromPageNode(root)).To(Equal(RoutePath("root")))
		Expect(GeneratePathFromPageNode(guide)).To(Equal(RoutePath("root/docs/guide")))
	})

	ginkgo.It("matches section routes that share an explicit content source directory", func() {
		first := WorkspaceMarkdownRoute{
			SourcePath:  "docs",
			RoutePath:   "docs",
			Kind:        NodeKindSection,
			ContentPath: "docs/README.md",
		}
		second := WorkspaceMarkdownRoute{
			SourcePath: "docs",
			RoutePath:  "docs",
			Kind:       NodeKindSection,
		}
		page := WorkspaceMarkdownRoute{
			SourcePath: "docs.md",
			RoutePath:  "docs",
			Kind:       NodeKindPage,
		}

		Expect(sectionSourceDir(first)).To(Equal(WorkspaceSourcePath("docs")))
		Expect(sameWorkspaceSectionRouteEntry(first, second)).To(BeTrue())
		Expect(sameWorkspaceSectionRouteEntry(first, page)).To(BeFalse())
		Expect(nonDefaultWorkspaceSourcePath(first)).To(Equal(WorkspaceSourcePath("")))
	})
})

var _ = ginkgo.Describe("tree error wrappers", func() {
	ginkgo.It("wraps sentinel errors with stable details", func() {
		drift := &DriftError{NodeID: "page-1", Kind: NodeKindPage, Path: "docs/page.md", Reason: "missing"}
		Expect(drift.Error()).To(ContainSubstring("nodeID=page-1"))
		Expect(errors.Is(drift, ErrDrift)).To(BeTrue())

		invalid := &InvalidOpError{Op: "move", Reason: "root"}
		Expect(invalid.Error()).To(Equal("move: root"))
		Expect(errors.Is(invalid, ErrInvalidOperation)).To(BeTrue())

		exists := &PageAlreadyExistsError{Path: "docs/page.md"}
		Expect(exists.Error()).To(Equal("already exists: docs/page.md"))
		Expect(errors.Is(exists, ErrPageAlreadyExists)).To(BeTrue())

		missing := &NotFoundError{Resource: "page", ID: "page-404", Path: "docs/missing.md"}
		Expect(missing.Error()).To(Equal("page not found: page-404"))
		Expect(errors.Is(missing, ErrPageNotFound)).To(BeTrue())

		convert := &ConvertNotAllowedError{From: NodeKindSection, To: NodeKindPage, Reason: "has children"}
		Expect(convert.Error()).To(Equal("cannot convert from section to page: has children"))
		Expect(errors.Is(convert, ErrConvertNotAllowed)).To(BeTrue())
	})

	ginkgo.It("validates route paths and exposes invalid route errors", func() {
		route, err := RoutePath("docs/guide").Validate()
		Expect(err).NotTo(HaveOccurred())
		Expect(route).To(Equal(RoutePath("docs/guide")))
		_, err = RoutePath("../escape").Validate()
		Expect(err).To(MatchError(ContainSubstring("invalid path")))
	})
})
