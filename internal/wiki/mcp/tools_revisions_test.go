package mcp

import (
	"context"
	"net/http/httptest"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gcustom"
	"github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"
	corerevision "github.com/perber/wiki/internal/core/revision"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/core/tree"
	httpinternal "github.com/perber/wiki/internal/http"
	testmatchers "github.com/perber/wiki/internal/test_utils/matchers"
	wikipages "github.com/perber/wiki/internal/wiki/pages"
	wikirevisions "github.com/perber/wiki/internal/wiki/revisions"
	"github.com/perber/wiki/internal/workspacesync"
)

var _ = Describe("Revision tools", func() {
	It("passes the workspace cursor and returns the next cursor", func() {
		const revisionCursorFixture = "rev-3"
		treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{
			DataDir: mcpTestTempDir(),
			RootDir: mcpTestTempDir(),
		})
		Expect(treeService.LoadTree()).To(Succeed())
		kind := tree.NodeKindPage
		pageID, err := treeService.CreateNode("alice", nil, "Page A", "page-a", &kind)
		Expect(err).NotTo(HaveOccurred())

		var seenCursor string
		routes := NewRoutes(RoutesConfig{
			TreeService: treeService,
			GetPage:     wikipages.NewGetPageUseCase(treeService),
			ListWorkspaceRevisions: func(_ context.Context, page *tree.Page, cursor string, limit workspacesync.PageRevisionLimit) (workspacesync.PageRevisionList, error) {
				Expect(page.ID).To(Equal(*pageID))
				Expect(limit).To(Equal(workspacesync.PageRevisionLimit(1)))
				seenCursor = cursor
				return workspacesync.PageRevisionList{
					Revisions: []*corerevision.Revision{{
						ID:       corerevision.RevisionIDFromString(revisionCursorFixture),
						PageID:   *pageID,
						Type:     corerevision.RevisionTypeContentUpdate,
						AuthorID: "alice",
						Title:    "Page A",
						Slug:     "page-a",
						Kind:     tree.NodeKindPage,
						Path:     "page-a",
					}},
					NextCursor: revisionCursorFixture,
				}, nil
			},
		})
		handler := routes.NewHTTPHandler(httpinternal.RouterOptions{
			AuthDisabled:        true,
			EnableWorkspaceSync: true,
			MCPEnabled:          true,
			MCPToolListPageSize: 200,
		})
		server := httptest.NewServer(handler)
		DeferCleanup(server.Close)
		client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "leafwiki-test", Version: "test"}, nil)
		session, err := client.Connect(context.Background(), &sdkmcp.StreamableClientTransport{
			Endpoint:             server.URL,
			HTTPClient:           server.Client(),
			DisableStandaloneSSE: true,
		}, nil)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = session.Close() })

		result, err := session.CallTool(context.Background(), &sdkmcp.CallToolParams{
			Name:      ToolListRevisions.String(),
			Arguments: map[string]any{"pageId": *pageID, "cursor": "rev-5", "limit": float64(1)},
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(seenCursor).To(Equal("rev-5"))
		Expect(result).To(matchSuccessfulToolStructuredContent(
			gstruct.MatchKeys(gstruct.IgnoreExtras, gstruct.Keys{
				"nextCursor": Equal(revisionCursorFixture),
				"revisions":  HaveExactElements(HaveKeyWithValue("id", revisionCursorFixture)),
			}),
		), "CallTool wiki_list_revisions returned tool error: %#v", result.Content)
	})

	It("returns a stable unavailable-backend revision error", func() {
		err := unavailableWorkspaceRevisionBackend()
		Expect(err).To(testmatchers.MatchLocalizedError(wikirevisions.ErrCodeRevisionNotFound, sharederrors.MessageIDForCode(wikirevisions.ErrCodeRevisionNotFound)))
	})
})

func matchSuccessfulToolStructuredContent(content types.GomegaMatcher) types.GomegaMatcher {
	GinkgoHelper()

	return gcustom.MakeMatcher(func(result *sdkmcp.CallToolResult) (bool, error) {
		if result == nil || result.IsError {
			return false, nil
		}
		return content.Match(result.StructuredContent)
	}).WithMessage("be a successful MCP tool result with matching structured content")
}
