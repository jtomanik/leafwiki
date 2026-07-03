package tree

import (
	"github.com/perber/wiki/internal/core/identity"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("route path parsing", func() {
	ginkgo.It("returns semantic route path values", func() {
		routePath, err := ParseRoutePath("docs/guide")
		Expect(err).NotTo(HaveOccurred())
		Expect(routePath).To(Equal(RoutePath("docs/guide")))

	})
})

var _ = ginkgo.Describe("semantic page value wrappers", func() {
	ginkgo.It("keep page identity, versions, slugs, and markdown paths distinct", func() {
		pageID := PageID("page-1")
		version := PageVersion("v2")
		slug := Slug("guide")
		markdownPath := MarkdownPath("docs/guide.md")

		Expect(pageID).To(Equal(PageID("page-1")))
		Expect(version).To(Equal(PageVersion("v2")))
		Expect(slug).To(Equal(Slug("guide")))
		Expect(markdownPath).To(Equal(MarkdownPath("docs/guide.md")))

	})
})

var _ = ginkgo.Describe("page version bypass sentinel", func() {
	ginkgo.It("stays internal to tree operations", func() {
		Expect(pageVersionUnchecked.IsUnchecked()).To(BeTrue())
		got := newFixturePageVersion(versionUnchecked)
		Expect(got.IsUnchecked()).To(BeFalse())
		Expect(got).To(BeEmpty())

	})
})

var _ = ginkgo.Describe("tree service unchecked-version boundaries", func() {
	ginkgo.It("exposes constrained operations for internal version bypasses", func() {
		var _ func(*TreeService, UserID, PageID, bool) error = (*TreeService).DeleteNodeUncheckedVersion
		var _ func(*TreeService, UserID, PageID, string, Slug, *string, bool) error = (*TreeService).UpdateNodeUncheckedVersion
		var _ func(*TreeService, UserID, PageID, string, Slug, *string) error = (*TreeService).UpdateNodeReplacingMetadataUncheckedVersion
		var _ func(*TreeService, UserID, PageID, PageID) error = (*TreeService).MoveNodeUncheckedVersion
		var _ func(*TreeService, UserID, PageID, NodeKind) error = (*TreeService).ConvertNodeUncheckedVersion

	})
})

var _ = ginkgo.Describe("tree identity aliases", func() {
	ginkgo.It("remain assignable to neutral identity types", func() {
		var _ identity.UserID = newFixtureUserID("user-1")
		var _ identity.RevisionID = newFixtureRevisionID("rev-1")
		var _ identity.CommitHash = CommitHash("abc123")

	})
})

var _ = ginkgo.Describe("workspace source paths", func() {
	ginkgo.It("use semantic path values on nodes and markdown routes", func() {
		sourcePath := WorkspaceSourcePath("Plans/Agent Hooks.PLAN.md")
		Expect(sourcePath).To(Equal(WorkspaceSourcePath("Plans/Agent Hooks.PLAN.md")))

		node := PageNode{WorkspaceSourcePath: sourcePath}
		var _ WorkspaceSourcePath = node.WorkspaceSourcePath

		route := WorkspaceMarkdownRoute{SourcePath: sourcePath}
		var _ WorkspaceSourcePath = route.SourcePath

	})
})

var _ = ginkgo.Describe("core page identity fields", func() {
	ginkgo.It("use semantic page and user identity types", func() {
		node := PageNode{
			ID: PageID("page-1"),
			Metadata: PageMetadata{
				CreatorID:    newFixtureUserID("user-1"),
				LastAuthorID: newFixtureUserID("user-2"),
			},
		}
		var _ PageID = node.ID
		var _ UserID = node.Metadata.CreatorID
		var _ UserID = node.Metadata.LastAuthorID

		var _ func(*PageNode, PageID, bool) bool = (*PageNode).IsChildOf

	})
})

var _ = ginkgo.Describe("tree service write boundaries", func() {
	ginkgo.It("accept semantic identity values at mutation boundaries", func() {
		var _ func(*TreeService, UserID, *PageID, string, Slug, *NodeKind) (*PageID, error) = (*TreeService).CreateNode
		var _ func(*TreeService, UserID, PageID, *PageID, string, Slug, NodeKind, string, PageMetadata) (*Page, error) = (*TreeService).RestoreNode
		var _ func(*TreeService, UserID, []BulkContentUpdate) []error = (*TreeService).BulkUpdateContent

		update := BulkContentUpdate{ID: PageID("page-1"), Content: "content"}
		var _ PageID = update.ID

	})
})

var _ = ginkgo.Describe("tree service read boundaries", func() {
	ginkgo.It("return and accept semantic identity values at lookup boundaries", func() {
		var _ func(*TreeService, PageID) (*Page, error) = (*TreeService).GetPage
		var _ func(*TreeService, PageID) (string, error) = (*TreeService).ReadPageRaw
		var _ func(*TreeService, []PageID) ([]*Page, []error) = (*TreeService).GetPages
		var _ func(*TreeService, PageID) (*PageNode, error) = (*TreeService).FindPageByID
		var _ func(*TreeService, PageID) (*PermalinkTarget, error) = (*TreeService).ResolvePermalinkTarget
		var _ func(*TreeService, RoutePath) (*Page, error) = (*TreeService).FindPageByRoutePath
		var _ func(*TreeService, RoutePath, NodeKind) (*Page, error) = (*TreeService).FindPageByRoutePathAndKind
		var _ func(*TreeService, RoutePath) (*PathLookup, error) = (*TreeService).LookupPagePath
		var _ func(*TreeService, RoutePath, NodeKind) (*PathLookup, error) = (*TreeService).LookupPagePathForKind

	})
})
