package revisions

import (
	"context"
	"fmt"
	"mime"
	"net/http"
	"os"
	"path"
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
	listRevisions            *ListRevisionsUseCase
	getRevision              *GetRevisionUseCase
	compareRevisions         *CompareRevisionsUseCase
	getRevisionAsset         *GetRevisionAssetUseCase
	getLatest                *GetLatestRevisionUseCase
	restoreRevision          *RestoreRevisionUseCase
	checkIntegrity           *CheckIntegrityUseCase
	listWorkspaceRevisions   func(context.Context, *tree.Page, string, int) (workspacesync.PageRevisionList, error)
	getWorkspaceRevision     func(context.Context, *tree.Page, string) (*revision.RevisionSnapshot, error)
	restoreWorkspaceRevision func(context.Context, *tree.Page, string, workspacesync.Actor, workspacesync.Source) (*tree.Page, error)
	userResolver             *coreauth.UserResolver
	authService              *coreauth.AuthService
	treeService              *tree.TreeService
}

// RoutesConfig holds the dependencies required to build a Routes instance.
type RoutesConfig struct {
	ListRevisions            *ListRevisionsUseCase
	GetRevision              *GetRevisionUseCase
	CompareRevisions         *CompareRevisionsUseCase
	GetRevisionAsset         *GetRevisionAssetUseCase
	GetLatest                *GetLatestRevisionUseCase
	RestoreRevision          *RestoreRevisionUseCase
	CheckIntegrity           *CheckIntegrityUseCase
	ListWorkspaceRevisions   func(context.Context, *tree.Page, string, int) (workspacesync.PageRevisionList, error)
	GetWorkspaceRevision     func(context.Context, *tree.Page, string) (*revision.RevisionSnapshot, error)
	RestoreWorkspaceRevision func(context.Context, *tree.Page, string, workspacesync.Actor, workspacesync.Source) (*tree.Page, error)
	UserResolver             *coreauth.UserResolver
	AuthService              *coreauth.AuthService
	TreeService              *tree.TreeService
}

// NewRoutes constructs the revisions RouteRegistrar.
func NewRoutes(cfg RoutesConfig) *Routes {
	return &Routes{
		listRevisions:            cfg.ListRevisions,
		getRevision:              cfg.GetRevision,
		compareRevisions:         cfg.CompareRevisions,
		getRevisionAsset:         cfg.GetRevisionAsset,
		getLatest:                cfg.GetLatest,
		restoreRevision:          cfg.RestoreRevision,
		checkIntegrity:           cfg.CheckIntegrity,
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

	// Revision routes are behind the legacy revision or workspace sync feature flags.
	if opts.EnableRevision || opts.EnableWorkspaceSync {
		withMode := func(handler gin.HandlerFunc) gin.HandlerFunc {
			return func(c *gin.Context) {
				c.Set("leafwiki.enableRevision", opts.EnableRevision)
				handler(c)
			}
		}
		authGroup.GET("/pages/:id/revisions", withMode(r.handleListRevisions))
		authGroup.GET("/pages/:id/revisions/latest", withMode(r.handleGetLatestRevision))
		authGroup.GET("/pages/:id/revisions/compare", withMode(r.handleCompareRevisions))
		authGroup.GET("/pages/:id/revisions/:revisionId/assets/*name", withMode(r.handleGetRevisionAsset))
		authGroup.GET("/pages/:id/revisions/:revisionId", withMode(r.handleGetRevision))
		authGroup.POST("/pages/:id/revisions/:revisionId/restore", authmw.RequireEditorOrAdmin(), withMode(r.handleRestoreRevision))
	}

}

// ─── Handlers ───────────────────────────────────────────────────────────────

func (r *Routes) handleListRevisions(c *gin.Context) {
	pageID := strings.TrimSpace(c.Param("id"))
	if pageID == "" {
		respondWithRevisionStatusError(c, http.StatusBadRequest, ErrCodeRevisionInvalidPageID, "Page ID is required", "page id is required")
		return
	}

	limit := DefaultRevisionListLimit
	if raw := strings.TrimSpace(c.Query("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			respondWithRevisionStatusError(c, http.StatusBadRequest, ErrCodeRevisionInvalidLimit, "Revision list limit is invalid", "revision list limit for page %s is invalid", pageID)
			return
		}
		normalized, err := NormalizeRevisionListLimit(&parsed, pageID)
		if err != nil {
			respondWithRevisionError(c, err)
			return
		}
		limit = normalized
	}

	if r.usesWorkspaceRevisions(c) {
		r.handleListWorkspaceRevisions(c, pageID, strings.TrimSpace(c.Query("cursor")), limit)
		return
	}

	out, err := r.listRevisions.Execute(c.Request.Context(), ListRevisionsInput{
		PageID: pageID,
		Cursor: strings.TrimSpace(c.Query("cursor")),
		Limit:  limit,
	})
	if err != nil {
		respondWithRevisionError(c, err)
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

func (r *Routes) usesWorkspaceRevisions(c *gin.Context) bool {
	return !c.GetBool("leafwiki.enableRevision") && r.listWorkspaceRevisions != nil
}

func (r *Routes) handleListWorkspaceRevisions(c *gin.Context, pageID string, cursor string, limit int) {
	if r.treeService == nil {
		respondWithRevisionStatusError(c, http.StatusInternalServerError, ErrCodeRevisionInternalError, "Failed to list revisions", "tree service is unavailable")
		return
	}
	page, err := r.treeService.GetPage(pageID)
	if err != nil {
		respondWithRevisionStatusError(c, http.StatusNotFound, ErrCodeRevisionNotFound, "Page not found", "page %s not found", pageID)
		return
	}
	out, err := r.listWorkspaceRevisions(c.Request.Context(), page, cursor, limit)
	if err != nil {
		respondWithRevisionStatusError(c, http.StatusInternalServerError, ErrCodeRevisionInternalError, "Failed to list revisions", "failed to list workspace revisions for page %s", pageID)
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
	if r.usesWorkspaceRevisions(c) {
		r.handleGetWorkspaceRevision(c, pageID, revisionID)
		return
	}

	out, err := r.getRevision.Execute(c.Request.Context(), GetRevisionInput{PageID: pageID, RevisionID: revisionID})
	if err != nil {
		respondWithRevisionError(c, err)
		return
	}
	if out.Snapshot == nil || out.Snapshot.Revision == nil {
		respondWithRevisionStatusError(c, http.StatusNotFound, ErrCodeRevisionNotFound, "Revision not found", "revision %s for page %s not found", revisionID, pageID)
		return
	}
	c.JSON(http.StatusOK, ToSnapshotResponse(out.Snapshot, r.userResolver))
}

func (r *Routes) handleGetWorkspaceRevision(c *gin.Context, pageID string, revisionID string) {
	page, err := r.workspacePage(pageID)
	if err != nil {
		respondWithRevisionStatusError(c, http.StatusNotFound, ErrCodeRevisionNotFound, "Revision not found", "revision %s for page %s not found", revisionID, pageID)
		return
	}
	snapshot, err := r.getWorkspaceRevision(c.Request.Context(), page, revisionID)
	if err != nil {
		respondWithRevisionStatusError(c, http.StatusNotFound, ErrCodeRevisionNotFound, "Revision not found", "revision %s for page %s not found", revisionID, pageID)
		return
	}
	c.JSON(http.StatusOK, ToSnapshotResponse(snapshot, r.userResolver))
}

func (r *Routes) handleGetLatestRevision(c *gin.Context) {
	pageID := strings.TrimSpace(c.Param("id"))
	if pageID == "" {
		respondWithRevisionStatusError(c, http.StatusBadRequest, ErrCodeRevisionInvalidPageID, "Page ID is required", "page id is required")
		return
	}
	if r.usesWorkspaceRevisions(c) {
		page, err := r.workspacePage(pageID)
		if err != nil {
			respondWithRevisionStatusError(c, http.StatusNotFound, ErrCodeRevisionNotFound, "Revision not found", "revision for page %s not found", pageID)
			return
		}
		out, err := r.listWorkspaceRevisions(c.Request.Context(), page, "", 1)
		if err != nil || len(out.Revisions) == 0 {
			respondWithRevisionStatusError(c, http.StatusNotFound, ErrCodeRevisionNotFound, "Revision not found", "revision for page %s not found", pageID)
			return
		}
		c.JSON(http.StatusOK, ToRevisionResponse(out.Revisions[0], r.userResolver))
		return
	}

	out, err := r.getLatest.Execute(c.Request.Context(), GetLatestRevisionInput{PageID: pageID})
	if err != nil {
		respondWithRevisionError(c, err)
		return
	}
	if out.Revision == nil {
		respondWithRevisionStatusError(c, http.StatusNotFound, ErrCodeRevisionNotFound, "Revision not found", "revision for page %s not found", pageID)
		return
	}
	c.JSON(http.StatusOK, ToRevisionResponse(out.Revision, r.userResolver))
}

func (r *Routes) handleCompareRevisions(c *gin.Context) {
	pageID, baseRevisionID, targetRevisionID, err := ValidateRevisionCompareInput(c.Param("id"), c.Query("base"), c.Query("target"))
	if err != nil {
		respondWithRevisionError(c, err)
		return
	}
	if r.usesWorkspaceRevisions(c) {
		page, err := r.workspacePage(pageID)
		if err != nil {
			respondWithRevisionStatusError(c, http.StatusNotFound, ErrCodeRevisionNotFound, "Revision not found", "revision compare resource for page %s not found", pageID)
			return
		}
		base, baseErr := r.getWorkspaceRevision(c.Request.Context(), page, baseRevisionID)
		target, targetErr := r.getWorkspaceRevision(c.Request.Context(), page, targetRevisionID)
		if baseErr != nil || targetErr != nil {
			respondWithRevisionStatusError(c, http.StatusNotFound, ErrCodeRevisionNotFound, "Revision not found", "revision compare resource for page %s not found", pageID)
			return
		}
		c.JSON(http.StatusOK, ToComparisonResponse(&revision.RevisionComparison{
			Base:           base,
			Target:         target,
			ContentChanged: base.Content != target.Content,
			AssetChanges:   nil,
		}, r.userResolver))
		return
	}

	out, err := r.compareRevisions.Execute(c.Request.Context(), CompareRevisionsInput{
		PageID: pageID, BaseRevisionID: baseRevisionID, TargetRevisionID: targetRevisionID,
	})
	if err != nil {
		respondWithRevisionError(c, err)
		return
	}
	if out.Comparison == nil || out.Comparison.Base == nil || out.Comparison.Target == nil {
		respondWithRevisionStatusError(c, http.StatusNotFound, ErrCodeRevisionNotFound, "Revision not found", "revision compare resource for page %s not found", pageID)
		return
	}
	c.JSON(http.StatusOK, ToComparisonResponse(out.Comparison, r.userResolver))
}

func (r *Routes) handleGetRevisionAsset(c *gin.Context) {
	pageID, revisionID, assetName, err := ValidateRevisionAssetInput(c.Param("id"), c.Param("revisionId"), c.Param("name"))
	if err != nil {
		respondWithRevisionError(c, err)
		return
	}
	if r.usesWorkspaceRevisions(c) {
		respondWithRevisionStatusError(c, http.StatusNotFound, ErrCodeRevisionPreviewAssetNotFound, "Revision asset not found", "workspace sync revisions do not track assets")
		return
	}

	out, err := r.getRevisionAsset.Execute(c.Request.Context(), GetRevisionAssetInput{
		PageID: pageID, RevisionID: revisionID, AssetName: assetName,
	})
	if err != nil {
		respondWithRevisionError(c, err)
		return
	}
	if out.Asset == nil {
		respondWithRevisionStatusError(c, http.StatusNotFound, ErrCodeRevisionPreviewAssetNotFound, "Revision asset not found", "revision asset %s for page %s revision %s not found", assetName, pageID, revisionID)
		return
	}

	f, err := os.Open(out.Asset.Path)
	if err != nil {
		respondWithRevisionError(c, NewRevisionAssetBlobUnavailableError(assetName, pageID, revisionID, err))
		return
	}
	defer func() { _ = f.Close() }()

	stat, err := f.Stat()
	if err != nil {
		respondWithRevisionStatusError(c, http.StatusInternalServerError, ErrCodeRevisionInternalError, "Failed to load revision asset", "failed to load revision asset")
		return
	}

	contentType := DetectRevisionAssetMIMEType(assetName, out.Asset.Asset.MIMEType)
	disposition := mime.FormatMediaType("inline", map[string]string{"filename": path.Base(assetName)})
	if disposition == "" {
		disposition = "inline"
	}
	c.Header("Content-Disposition", disposition)
	c.Writer.Header().Set("Content-Type", contentType)
	http.ServeContent(c.Writer, c.Request, path.Base(assetName), stat.ModTime(), f)
}

func (r *Routes) handleRestoreRevision(c *gin.Context) {
	pageID := strings.TrimSpace(c.Param("id"))
	revisionID := strings.TrimSpace(c.Param("revisionId"))
	if pageID == "" {
		respondWithRevisionStatusError(c, http.StatusBadRequest, ErrCodeRevisionRestoreInvalidPageID, "Failed to restore page", "failed to restore page %s", pageID)
		return
	}
	if revisionID == "" {
		respondWithRevisionStatusError(c, http.StatusBadRequest, ErrCodeRevisionRestoreInvalidRevision, "Restore revision is invalid", "restore revision %s for page %s is invalid", revisionID, pageID)
		return
	}

	user := authmw.MustGetUser(c)
	if user == nil {
		return
	}
	if r.usesWorkspaceRevisions(c) {
		page, err := r.workspacePage(pageID)
		if err != nil {
			respondWithRevisionStatusError(c, http.StatusNotFound, ErrCodeRevisionNotFound, "Revision not found", "revision %s for page %s not found", revisionID, pageID)
			return
		}
		restored, err := r.restoreWorkspaceRevision(
			c.Request.Context(),
			page,
			revisionID,
			workspacesync.Actor{ID: user.ID, Name: user.Username, Email: user.Email},
			workspacesync.SourceWeb,
		)
		if err != nil {
			respondWithRevisionStatusError(c, http.StatusNotFound, ErrCodeRevisionNotFound, "Revision not found", "revision %s for page %s not found", revisionID, pageID)
			return
		}
		apiPage := dto.ToAPIPage(restored, r.userResolver)
		if r.treeService != nil {
			wikipages.EnrichPageMetadata(apiPage, r.treeService.ReadPageRaw)
		}
		c.JSON(http.StatusOK, apiPage)
		return
	}

	out, err := r.restoreRevision.Execute(c.Request.Context(), RestoreRevisionInput{
		UserID: user.ID, PageID: pageID, RevisionID: revisionID,
	})
	if err != nil {
		respondWithRevisionError(c, err)
		return
	}
	apiPage := dto.ToAPIPage(out.Page, r.userResolver)
	if r.treeService != nil {
		wikipages.EnrichPageMetadata(apiPage, r.treeService.ReadPageRaw)
	}
	c.JSON(http.StatusOK, apiPage)
}

func (r *Routes) workspacePage(pageID string) (*tree.Page, error) {
	if r.treeService == nil {
		return nil, fmt.Errorf("tree service is unavailable")
	}
	return r.treeService.GetPage(pageID)
}
