package revisions

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	corerevision "github.com/perber/wiki/internal/core/revision"
	"github.com/perber/wiki/internal/core/tree"
	"github.com/perber/wiki/internal/workspacesync"
)

func TestRoutesListWorkspaceRevisionsPassesCursorAndReturnsNextCursor(t *testing.T) {
	gin.SetMode(gin.TestMode)
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
	var seenLimit workspacesync.PageRevisionLimit
	routes := NewRoutes(RoutesConfig{
		TreeService: treeService,
		ListWorkspaceRevisions: func(_ context.Context, page *tree.Page, cursor string, limit workspacesync.PageRevisionLimit) (workspacesync.PageRevisionList, error) {
			if page.ID != *pageID {
				t.Fatalf("workspace page id = %q, want %q", page.ID, pageID.String())
			}
			seenCursor = cursor
			seenLimit = limit
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

	pageIDValue := pageID.MetadataValue()
	req := httptest.NewRequest(http.MethodGet, "/api/pages/"+pageIDValue+"/revisions?cursor=rev-5&limit=1", nil)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req
	c.Params = gin.Params{{Key: "id", Value: pageIDValue}}

	routes.handleListRevisions(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if seenCursor != "rev-5" {
		t.Fatalf("workspace cursor = %q, want rev-5", seenCursor)
	}
	if seenLimit != 1 {
		t.Fatalf("workspace limit = %d, want 1", seenLimit)
	}
	var body struct {
		Revisions  []*RevisionResponse `json:"revisions"`
		NextCursor string              `json:"nextCursor"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Revisions) != 1 || body.Revisions[0].ID != "rev-3" {
		t.Fatalf("response revisions = %#v, want rev-3", body.Revisions)
	}
	if body.NextCursor != "rev-3" {
		t.Fatalf("response nextCursor = %q, want rev-3", body.NextCursor)
	}
}
