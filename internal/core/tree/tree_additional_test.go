package tree

import (
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("semantic path helpers", ginkgo.Label("unit"), func() {
	ginkgo.It("normalizes markdown paths and derives route/source semantics", func() {
		path := MarkdownPathFromString(" /docs//guide/index.md ")

		Expect(path.Clean()).To(Equal(newFixtureMarkdownPath("docs/guide/index.md")))
		Expect(path.Clean().Ext()).To(Equal(".md"))
		Expect(path.Clean()).To(matchMarkdownPathSemantics(markdownPathIndexFile, markdownPathFormatMarkdown))
		Expect(path.Clean().RoutePath()).To(Equal(newFixtureRoutePath("docs/guide")))
		Expect(path.Clean().SourceDir()).To(Equal(newFixtureMarkdownPath("docs/guide")))
		Expect(path.Clean().FilesystemPath()).To(Equal("docs/guide/index.md"))
	})

	ginkgo.It("builds route path variants for root, page, and section content", func() {
		root := RoutePathFromString("/")
		route := RoutePathFromString("/Docs/Guide/")

		Expect(root.Clean()).To(matchRoutePathKind(routePathRoot))
		Expect(root.WikiPath()).To(Equal("/"))
		Expect(root.Child(newFixtureSlug("docs"))).To(Equal(newFixtureRoutePath("docs")))
		Expect(root.MarkdownContentPath(NodeKindSection)).To(Equal(newFixtureMarkdownPath("index.md")))
		Expect(root.MarkdownPagePath()).To(Equal(newFixtureMarkdownPath("index.md")))
		Expect(root.WorkspaceSourcePath(NodeKindPage)).To(Equal(newFixtureWorkspaceSourcePath("")))
		Expect(root.WorkspaceSourcePath(NodeKindSection)).To(Equal(newFixtureWorkspaceSourcePath("")))

		Expect(route.Clean()).To(Equal(newFixtureRoutePath("Docs/Guide")))
		Expect(route.WikiPath()).To(Equal("/Docs/Guide"))
		Expect(route.Segments()).To(Equal([]Slug{newFixtureSlug("Docs"), newFixtureSlug("Guide")}))
		Expect(route.Child(newFixtureSlug("Intro"))).To(Equal(newFixtureRoutePath("Docs/Guide/Intro")))
		Expect(route.WithLeafSlug(newFixtureSlug("Reference"))).To(Equal(newFixtureRoutePath("Docs/Reference")))
		Expect(route.MarkdownContentPath(NodeKindPage)).To(Equal(newFixtureMarkdownPath("Docs/Guide.md")))
		Expect(route.MarkdownContentPath(NodeKindSection)).To(Equal(newFixtureMarkdownPath("Docs/Guide/index.md")))
		Expect(route.MarkdownPagePath()).To(Equal(newFixtureMarkdownPath("Docs/Guide.md")))
		Expect(route.HrefPath()).To(Equal(newFixtureMarkdownPath("Docs/Guide")))
		Expect(route.LowerKey(NodeKindPage)).To(Equal(RouteLowerKey{Kind: NodeKindPage, Path: newFixtureRoutePath("docs/guide")}))
		Expect(route.Lower()).To(Equal(newFixtureRoutePath("docs/guide")))
		Expect(route.WorkspaceSourceDirectory()).To(Equal(newFixtureWorkspaceSourcePath("Docs/Guide")))
		Expect(route.LeafSlug()).To(Equal(newFixtureSlug("Guide")))
		Expect(route.WorkspaceSourcePath(NodeKindPage)).To(Equal(newFixtureWorkspaceSourcePath("Docs/Guide.md")))
		Expect(route.WorkspaceSourcePath(NodeKindSection)).To(Equal(newFixtureWorkspaceSourcePath("Docs/Guide")))
	})

	ginkgo.It("scans and stores semantic route and page identifiers", func() {
		pageID := newFixturePageID("page-1")
		value, err := pageID.Value()
		Expect(err).NotTo(HaveOccurred())
		Expect(value).To(Equal("page-1"))

		var scannedPageID PageID
		Expect(scannedPageID.Scan([]byte("page-2"))).To(Succeed())
		Expect(scannedPageID).To(Equal(newFixturePageID("page-2")))
		Expect(scannedPageID.Scan("page-3")).To(Succeed())
		Expect(scannedPageID).To(Equal(newFixturePageID("page-3")))
		Expect(scannedPageID.Scan(nil)).To(Succeed())
		Expect(scannedPageID).To(Equal(newFixturePageID("")))
		Expect(scannedPageID.Scan(123)).To(MatchError(ErrScanPageID))

		route := newFixtureRoutePath("/docs/guide/")
		routeValue, err := route.Value()
		Expect(err).NotTo(HaveOccurred())
		Expect(routeValue).To(Equal("/docs/guide"))

		var scannedRoute RoutePath
		Expect(scannedRoute.Scan([]byte("/Docs/Guide/"))).To(Succeed())
		Expect(scannedRoute).To(Equal(newFixtureRoutePath("/Docs/Guide/")))
		Expect(scannedRoute.Scan("docs/guide")).To(Succeed())
		Expect(scannedRoute).To(Equal(newFixtureRoutePath("docs/guide")))
		Expect(scannedRoute.Scan(nil)).To(Succeed())
		Expect(scannedRoute).To(Equal(newFixtureRoutePath("")))
		Expect(scannedRoute.Scan(time.Now())).To(MatchError(ErrScanRoutePath))
	})

	ginkgo.It("keeps asset names, slugs, and page versions narrowly typed", func() {
		asset := AssetNameFromString(" logo.png ")
		Expect(asset.Clean()).To(Equal(newFixtureAssetName("logo.png")))
		Expect(asset.Filename()).To(Equal(" logo.png "))

		slug := SlugFromString("Guide")
		Expect(slug.SlugKey()).To(Equal(newFixtureSlug("guide").SlugKey()))
		Expect(slug.SlugKey()).To(Equal(SlugKey("guide")))
		Expect(slug.RoutePath()).To(Equal(newFixtureRoutePath("Guide")))
		Expect(slug.Validate()).To(Succeed())

		now := time.Date(2026, 6, 25, 1, 2, 3, 4, time.FixedZone("offset", 3600))
		Expect(NewPageVersionFromTime(time.Time{})).To(Equal(newFixturePageVersion("")))
		Expect(NewPageVersionFromTime(now)).To(Equal(newFixturePageVersion("2026-06-25T00:02:03.000000004Z")))
		Expect(PageVersionFromString(versionUnchecked)).To(Equal(newFixturePageVersion("")))
	})
})

var _ = ginkgo.Describe("tree route paths and section content resolution", ginkgo.Label("unit"), func() {
	ginkgo.It("generates a filesystem-style path from page node ancestry", func() {
		root := &PageNode{ID: RootPageID, Slug: newFixtureSlug("root"), Kind: NodeKindSection}
		docs := &PageNode{ID: newFixturePageID("docs"), Slug: newFixtureSlug("docs"), Kind: NodeKindSection, Parent: root}
		guide := &PageNode{ID: newFixturePageID("guide"), Slug: newFixtureSlug("guide"), Kind: NodeKindPage, Parent: docs}

		Expect(GeneratePathFromPageNode(root)).To(Equal(newFixtureRoutePath("root")))
		Expect(GeneratePathFromPageNode(guide)).To(Equal(newFixtureRoutePath("root/docs/guide")))
	})

	ginkgo.It("matches section routes that share an explicit content source directory", func() {
		first := WorkspaceMarkdownRoute{
			SourcePath:  newFixtureWorkspaceSourcePath("docs"),
			RoutePath:   newFixtureRoutePath("docs"),
			Kind:        NodeKindSection,
			ContentPath: newFixtureMarkdownPath("docs/README.md"),
		}
		second := WorkspaceMarkdownRoute{
			SourcePath: newFixtureWorkspaceSourcePath("docs"),
			RoutePath:  newFixtureRoutePath("docs"),
			Kind:       NodeKindSection,
		}
		page := WorkspaceMarkdownRoute{
			SourcePath: newFixtureWorkspaceSourcePath("docs.md"),
			RoutePath:  newFixtureRoutePath("docs"),
			Kind:       NodeKindPage,
		}

		Expect(sectionSourceDir(first)).To(Equal(newFixtureWorkspaceSourcePath("docs")))
		Expect(observeWorkspaceSectionRouteMatch(first, second)).To(Equal(workspaceSectionRouteMatched))
		Expect(observeWorkspaceSectionRouteMatch(first, page)).To(Equal(workspaceSectionRouteMismatched))
		Expect(nonDefaultWorkspaceSourcePath(first)).To(Equal(newFixtureWorkspaceSourcePath("")))
	})
})

var _ = ginkgo.Describe("tree error wrappers", ginkgo.Label("unit"), func() {
	ginkgo.It("wraps sentinel errors with stable details", func() {
		drift := &DriftError{NodeID: newFixturePageID("page-1"), Kind: NodeKindPage, Path: "docs/page.md", Reason: "missing"}
		Expect(drift).To(MatchError(ErrDrift))
		Expect(drift).To(HaveField("NodeID", PageIDFromString("page-1")))
		Expect(drift).To(HaveField("Kind", NodeKindPage))
		Expect(drift).To(HaveField("Path", "docs/page.md"))

		invalid := &InvalidOpError{Op: "move", Reason: "root"}
		Expect(invalid).To(MatchError(ErrInvalidOperation))
		Expect(invalid).To(HaveField("Op", "move"))
		Expect(invalid).To(HaveField("Reason", "root"))

		exists := &PageAlreadyExistsError{Path: "docs/page.md"}
		Expect(exists).To(MatchError(ErrPageAlreadyExists))
		Expect(exists).To(HaveField("Path", "docs/page.md"))

		missing := &NotFoundError{Resource: "page", ID: newFixturePageID("page-404"), Path: "docs/missing.md"}
		Expect(missing).To(MatchError(ErrPageNotFound))
		Expect(missing).To(HaveField("Resource", "page"))
		Expect(missing).To(HaveField("ID", PageIDFromString("page-404")))
		Expect(missing).To(HaveField("Path", "docs/missing.md"))

		convert := &ConvertNotAllowedError{From: NodeKindSection, To: NodeKindPage, Reason: "has children"}
		Expect(convert).To(MatchError(ErrConvertNotAllowed))
		Expect(convert).To(HaveField("From", NodeKindSection))
		Expect(convert).To(HaveField("To", NodeKindPage))
		Expect(convert).To(HaveField("Reason", "has children"))
	})

	ginkgo.It("validates route paths and exposes invalid route errors", func() {
		route, err := newFixtureRoutePath("docs/guide").Validate()
		Expect(err).NotTo(HaveOccurred())
		Expect(route).To(Equal(newFixtureRoutePath("docs/guide")))
		_, err = newFixtureRoutePath("../escape").Validate()
		Expect(err).To(MatchError(ErrInvalidRoutePath))
	})
})
