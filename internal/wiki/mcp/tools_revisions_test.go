package mcp

import (
	"context"
	"net/http/httptest"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	corerevision "github.com/perber/wiki/internal/core/revision"
	"github.com/perber/wiki/internal/core/tree"
	httpinternal "github.com/perber/wiki/internal/http"
	wikipages "github.com/perber/wiki/internal/wiki/pages"
	"github.com/perber/wiki/internal/workspacesync"
)

func TestListRevisionsToolPassesWorkspaceCursorAndReturnsNextCursor(t *testing.T) {
	treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{
		DataDir: t.TempDir(),
		RootDir: t.TempDir(),
	})
	if err := treeService.LoadTree(); err != nil {
		t.Fatalf("LoadTree: %v", err)
	}
	kind := tree.NodeKindPage
	pageID, err := treeService.CreateNode("alice", nil, "Page A", "page-a", &kind)
	if err != nil {
		t.Fatalf("CreateNode: %v", err)
	}

	var seenCursor string
	routes := NewRoutes(RoutesConfig{
		TreeService: treeService,
		GetPage:     wikipages.NewGetPageUseCase(treeService),
		ListWorkspaceRevisions: func(_ context.Context, page *tree.Page, cursor string, limit int) (workspacesync.PageRevisionList, error) {
			if page.ID != *pageID {
				t.Fatalf("workspace page id = %q, want %q", page.ID, *pageID)
			}
			if limit != 1 {
				t.Fatalf("workspace limit = %d, want 1", limit)
			}
			seenCursor = cursor
			return workspacesync.PageRevisionList{
				Revisions: []*corerevision.Revision{{
					ID:       "rev-3",
					PageID:   *pageID,
					Type:     corerevision.RevisionTypeContentUpdate,
					AuthorID: "alice",
					Title:    "Page A",
					Slug:     "page-a",
					Kind:     string(tree.NodeKindPage),
					Path:     "page-a",
				}},
				NextCursor: "rev-3",
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
	t.Cleanup(server.Close)
	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "leafwiki-test", Version: "test"}, nil)
	session, err := client.Connect(context.Background(), &sdkmcp.StreamableClientTransport{
		Endpoint:             server.URL,
		HTTPClient:           server.Client(),
		DisableStandaloneSSE: true,
	}, nil)
	if err != nil {
		t.Fatalf("Connect MCP client: %v", err)
	}
	t.Cleanup(func() { session.Close() })

	result, err := session.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name:      ToolListRevisions,
		Arguments: map[string]any{"pageId": *pageID, "cursor": "rev-5", "limit": float64(1)},
	})
	if err != nil {
		t.Fatalf("CallTool list_revisions: %v", err)
	}
	if result.IsError {
		t.Fatalf("CallTool list_revisions returned tool error: %#v", result.Content)
	}
	body, ok := result.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("structured content type = %T, want map", result.StructuredContent)
	}
	if seenCursor != "rev-5" {
		t.Fatalf("workspace cursor = %q, want rev-5", seenCursor)
	}
	if body["nextCursor"] != "rev-3" {
		t.Fatalf("nextCursor = %v, want rev-3", body["nextCursor"])
	}
	revisions, ok := body["revisions"].([]any)
	if !ok || len(revisions) != 1 {
		t.Fatalf("revisions = %#v, want one revision", body["revisions"])
	}
	first, ok := revisions[0].(map[string]any)
	if !ok || first["id"] != "rev-3" {
		t.Fatalf("first revision = %#v, want rev-3", revisions[0])
	}
}
