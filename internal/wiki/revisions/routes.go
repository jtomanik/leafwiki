package revisions

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	coreauth "github.com/perber/wiki/internal/core/auth"
	"github.com/perber/wiki/internal/core/revision"
	"github.com/perber/wiki/internal/core/tree"
	httpinternal "github.com/perber/wiki/internal/http"
	"github.com/perber/wiki/internal/http/dto"
	authmw "github.com/perber/wiki/internal/http/middleware/auth"
	"github.com/perber/wiki/internal/http/middleware/security"
	wikipages "github.com/perber/wiki/internal/wiki/pages"
	"github.com/perber/wiki/internal/workspacesync"
)

// Routes is the RouteRegistrar for the revisions domain.
type Routes struct {
	listWorkspaceRevisions   func(context.Context, *tree.Page, string, int) (workspacesync.PageRevisionList, error)
	getWorkspaceRevision     func(context.Context, *tree.Page, revision.RevisionID) (*revision.RevisionSnapshot, error)
	restoreWorkspaceRevision func(context.Context, *tree.Page, revision.RevisionID, workspacesync.Actor, workspacesync.Source) (*tree.Page, error)
	userResolver             *coreauth.UserResolver
	authService              *coreauth.AuthService
	treeService              *tree.TreeService
}

// RoutesConfig holds the dependencies required to build a Routes instance.
type RoutesConfig struct {
	ListWorkspaceRevisions   func(context.Context, *tree.Page, string, int) (workspacesync.PageRevisionList, error)
	GetWorkspaceRevision     func(context.Context, *tree.Page, revision.RevisionID) (*revision.RevisionSnapshot, error)
	RestoreWorkspaceRevision func(context.Context, *tree.Page, revision.RevisionID, workspacesync.Actor, workspacesync.Source) (*tree.Page, error)
	UserResolver             *coreauth.UserResolver
	AuthService              *coreauth.AuthService
	TreeService              *tree.TreeService
}

// NewRoutes constructs the revisions RouteRegistrar.
func NewRoutes(cfg RoutesConfig) *Routes {
	return &Routes{
		listWorkspaceRevisions:   cfg.ListWorkspaceRevisions,
		getWorkspaceRevision:     cfg.GetWorkspaceRevision,
		restoreWorkspaceRevision: cfg.RestoreWorkspaceRevision,
		userResolver:             cfg.UserResolver,
		authService:              cfg.AuthService,
		treeService:              cfg.TreeService,
	}
}

// RegisterRoutes implements RouteRegistrar.
func (r *Routes) RegisterRoutes(ctx httpinternal.RouterContext) {
	opts := ctx.Opts

	authGroup := ctx.Base.Group("/api")
	authGroup.Use(
		authmw.InjectPublicEditor(opts.AuthDisabled),
		authmw.RequireAuth(r.authService, ctx.AuthCookies, opts.AuthDisabled),
		security.CSRFMiddleware(ctx.CSRFCookie),
	)

	authGroup.GET("/pages/:id/revisions", r.handleListRevisions)
	authGroup.GET("/pages/:id/revisions/latest", r.handleGetLatestRevision)
	authGroup.GET("/pages/:id/revisions/compare", r.handleCompareRevisions)
	authGroup.GET("/pages/:id/revisions/:revisionId/assets/*name", r.handleGetRevisionAsset)
	authGroup.GET("/pages/:id/revisions/:revisionId", r.handleGetRevision)
	authGroup.POST("/pages/:id/revisions/:revisionId/restore", authmw.RequireEditorOrAdmin(), r.handleRestoreRevision)
}

// ─── Handlers ───────────────────────────────────────────────────────────────

func (r *Routes) handleListRevisions(c *gin.Context) {
	pageID := tree.NewPageIDUnchecked(strings.TrimSpace(c.Param("id")))
	if pageID == "" {
		respondWithRevisionStatusError(c, http.StatusBadRequest, ErrCodeRevisionInvalidPageID, "Page ID is required", "page id is required")
		return
	}

	limit := DefaultRevisionListLimit
	if raw := strings.TrimSpace(c.Query("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			respondWithRevisionStatusError(c, http.StatusBadRequest, ErrCodeRevisionInvalidLimit, "Revision list limit is invalid", "revision list limit for page %s is invalid", pageID.MetadataValue())
			return
		}
		normalized, err := NormalizeRevisionListLimit(&parsed, pageID)
		if err != nil {
			respondWithRevisionError(c, err)
			return
		}
		limit = normalized
	}

	r.handleListWorkspaceRevisions(c, pageID, strings.TrimSpace(c.Query("cursor")), limit)
}

func (r *Routes) handleListWorkspaceRevisions(c *gin.Context, pageID tree.PageID, cursor string, limit int) {
	if r.treeService == nil {
		respondWithRevisionStatusError(c, http.StatusInternalServerError, ErrCodeRevisionInternalError, "Failed to list revisions", "tree service is unavailable")
		return
	}
	if r.listWorkspaceRevisions == nil {
		respondWithRevisionStatusError(c, http.StatusInternalServerError, ErrCodeRevisionInternalError, "Failed to list revisions", "workspace revision backend is unavailable")
		return
	}
	page, err := r.treeService.GetPage(pageID)
	if err != nil {
		respondWithRevisionStatusError(c, http.StatusNotFound, ErrCodeRevisionNotFound, "Page not found", "page %s not found", pageID.MetadataValue())
		return
	}
	out, err := r.listWorkspaceRevisions(c.Request.Context(), page, cursor, limit)
	if err != nil {
		respondWithRevisionStatusError(c, http.StatusInternalServerError, ErrCodeRevisionInternalError, "Failed to list revisions", "failed to list workspace revisions for page %s", pageID.MetadataValue())
		return
	}
	result := make([]*RevisionResponse, 0, len(out.Revisions))
	for _, rev := range out.Revisions {
		result = append(result, ToRevisionResponse(rev, r.userResolver))
	}
	c.JSON(http.StatusOK, gin.H{
		"revisions":  result,
		"nextCursor": out.NextCursor,
	})
}

func (r *Routes) handleGetRevision(c *gin.Context) {
	pageID, revisionID, err := ValidateRevisionLookupInput(c.Param("id"), c.Param("revisionId"))
	if err != nil {
		respondWithRevisionError(c, err)
		return
	}
	r.handleGetWorkspaceRevision(c, pageID, revisionID)
}

func (r *Routes) handleGetWorkspaceRevision(c *gin.Context, pageID tree.PageID, revisionID revision.RevisionID) {
	if r.getWorkspaceRevision == nil {
		respondWithRevisionStatusError(c, http.StatusInternalServerError, ErrCodeRevisionInternalError, "Failed to get revision", "workspace revision backend is unavailable")
		return
	}
	page, err := r.workspacePage(pageID)
	if err != nil {
		respondWithRevisionStatusError(c, http.StatusNotFound, ErrCodeRevisionNotFound, "Revision not found", "revision %s for page %s not found", revisionID.CommitID(), pageID.MetadataValue())
		return
	}
	snapshot, err := r.getWorkspaceRevision(c.Request.Context(), page, revisionID)
	if err != nil {
		respondWithRevisionStatusError(c, http.StatusNotFound, ErrCodeRevisionNotFound, "Revision not found", "revision %s for page %s not found", revisionID.CommitID(), pageID.MetadataValue())
		return
	}
	c.JSON(http.StatusOK, ToSnapshotResponse(snapshot, r.userResolver))
}

func (r *Routes) handleGetLatestRevision(c *gin.Context) {
	pageID := tree.NewPageIDUnchecked(strings.TrimSpace(c.Param("id")))
	if pageID == "" {
		respondWithRevisionStatusError(c, http.StatusBadRequest, ErrCodeRevisionInvalidPageID, "Page ID is required", "page id is required")
		return
	}
	if r.listWorkspaceRevisions == nil {
		respondWithRevisionStatusError(c, http.StatusInternalServerError, ErrCodeRevisionInternalError, "Failed to get revision", "workspace revision backend is unavailable")
		return
	}
	page, err := r.workspacePage(pageID)
	if err != nil {
		respondWithRevisionStatusError(c, http.StatusNotFound, ErrCodeRevisionNotFound, "Revision not found", "revision for page %s not found", pageID.MetadataValue())
		return
	}
	out, err := r.listWorkspaceRevisions(c.Request.Context(), page, "", 1)
	if err != nil || len(out.Revisions) == 0 {
		respondWithRevisionStatusError(c, http.StatusNotFound, ErrCodeRevisionNotFound, "Revision not found", "revision for page %s not found", pageID.MetadataValue())
		return
	}
	c.JSON(http.StatusOK, ToRevisionResponse(out.Revisions[0], r.userResolver))
}

func (r *Routes) handleCompareRevisions(c *gin.Context) {
	pageID, baseRevisionID, targetRevisionID, err := ValidateRevisionCompareInput(c.Param("id"), c.Query("base"), c.Query("target"))
	if err != nil {
		respondWithRevisionError(c, err)
		return
	}
	if r.getWorkspaceRevision == nil {
		respondWithRevisionStatusError(c, http.StatusInternalServerError, ErrCodeRevisionInternalError, "Failed to compare revisions", "workspace revision backend is unavailable")
		return
	}
	page, err := r.workspacePage(pageID)
	if err != nil {
		respondWithRevisionStatusError(c, http.StatusNotFound, ErrCodeRevisionNotFound, "Revision not found", "revision compare resource for page %s not found", pageID.MetadataValue())
		return
	}
	base, baseErr := r.getWorkspaceRevision(c.Request.Context(), page, baseRevisionID)
	target, targetErr := r.getWorkspaceRevision(c.Request.Context(), page, targetRevisionID)
	if baseErr != nil || targetErr != nil {
		respondWithRevisionStatusError(c, http.StatusNotFound, ErrCodeRevisionNotFound, "Revision not found", "revision compare resource for page %s not found", pageID.MetadataValue())
		return
	}
	c.JSON(http.StatusOK, ToComparisonResponse(&revision.RevisionComparison{
		Base:           base,
		Target:         target,
		ContentChanged: base.Content != target.Content,
		AssetChanges:   nil,
	}, r.userResolver))
}

func (r *Routes) handleGetRevisionAsset(c *gin.Context) {
	_, _, _, err := ValidateRevisionAssetInput(c.Param("id"), c.Param("revisionId"), c.Param("name"))
	if err != nil {
		respondWithRevisionError(c, err)
		return
	}
	respondWithRevisionStatusError(c, http.StatusNotFound, ErrCodeRevisionPreviewAssetNotFound, "Revision asset not found", "workspace sync revisions do not track assets")
}

func (r *Routes) handleRestoreRevision(c *gin.Context) {
	pageID, revisionID, err := ValidateRevisionLookupInput(c.Param("id"), c.Param("revisionId"))
	if err != nil {
		respondWithRevisionError(c, err)
		return
	}

	user := authmw.MustGetUser(c)
	if user == nil {
		return
	}
	if r.restoreWorkspaceRevision == nil {
		respondWithRevisionStatusError(c, http.StatusInternalServerError, ErrCodeRevisionInternalError, "Failed to restore page", "workspace revision backend is unavailable")
		return
	}
	page, err := r.workspacePage(pageID)
	if err != nil {
		respondWithRevisionStatusError(c, http.StatusNotFound, ErrCodeRevisionNotFound, "Revision not found", "revision %s for page %s not found", revisionID.CommitID(), pageID.MetadataValue())
		return
	}
	restored, err := r.restoreWorkspaceRevision(
		c.Request.Context(),
		page,
		revisionID,
		workspacesync.Actor{ID: workspacesync.NewActorIDUnchecked(user.ID), Name: user.Username, Email: user.Email},
		workspacesync.SourceWeb,
	)
	if err != nil {
		respondWithRevisionStatusError(c, http.StatusNotFound, ErrCodeRevisionNotFound, "Revision not found", "revision %s for page %s not found", revisionID.CommitID(), pageID.MetadataValue())
		return
	}
	apiPage := dto.ToAPIPage(restored, r.userResolver)
	if r.treeService != nil {
		wikipages.EnrichPageMetadata(apiPage, r.treeService.ReadPageRaw)
	}
	c.JSON(http.StatusOK, apiPage)
}

func (r *Routes) workspacePage(pageID tree.PageID) (*tree.Page, error) {
	if r.treeService == nil {
		return nil, fmt.Errorf("tree service is unavailable")
	}
	return r.treeService.GetPage(pageID)
}
