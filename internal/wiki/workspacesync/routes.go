package workspacesyncapi

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	coreauth "github.com/perber/wiki/internal/core/auth"
	httpinternal "github.com/perber/wiki/internal/http"
	authmw "github.com/perber/wiki/internal/http/middleware/auth"
	"github.com/perber/wiki/internal/http/middleware/security"
	"github.com/perber/wiki/internal/workspacesync"
)

type Routes struct {
	status           func() workspacesync.SyncStatus
	refresh          func(context.Context, workspacesync.SyncRequest) (workspacesync.SyncStatus, error)
	listSnapshots    func(context.Context, workspacesync.CommitHash, int) (workspacesync.SnapshotList, error)
	restoreWorkspace func(context.Context, workspacesync.CommitHash, workspacesync.Actor, workspacesync.Source) (workspacesync.SyncStatus, error)
	authService      *coreauth.AuthService
}

type RoutesConfig struct {
	Status           func() workspacesync.SyncStatus
	Refresh          func(context.Context, workspacesync.SyncRequest) (workspacesync.SyncStatus, error)
	ListSnapshots    func(context.Context, workspacesync.CommitHash, int) (workspacesync.SnapshotList, error)
	RestoreWorkspace func(context.Context, workspacesync.CommitHash, workspacesync.Actor, workspacesync.Source) (workspacesync.SyncStatus, error)
	AuthService      *coreauth.AuthService
}

func NewRoutes(cfg RoutesConfig) *Routes {
	return &Routes{
		status:           cfg.Status,
		refresh:          cfg.Refresh,
		listSnapshots:    cfg.ListSnapshots,
		restoreWorkspace: cfg.RestoreWorkspace,
		authService:      cfg.AuthService,
	}
}

func (r *Routes) RegisterRoutes(ctx httpinternal.RouterContext) {
	if r == nil || r.status == nil {
		return
	}
	if ctx.Opts.PublicAccess {
		pub := ctx.Base.Group("/api/workspace-sync")
		pub.GET("/status", r.handleStatus)
	}
	group := ctx.Base.Group("/api/workspace-sync")
	group.Use(
		authmw.InjectPublicEditor(ctx.Opts.AuthDisabled),
		authmw.RequireAuth(r.authService, ctx.AuthCookies, ctx.Opts.AuthDisabled),
		security.CSRFMiddleware(ctx.CSRFCookie),
	)
	if !ctx.Opts.PublicAccess {
		group.GET("/status", r.handleStatus)
	}
	group.POST("/refresh", authmw.RequireEditorOrAdmin(), r.handleRefresh)
	group.GET("/snapshots", r.handleSnapshots)
	group.POST("/snapshots/:commit/restore", authmw.RequireEditorOrAdmin(), r.handleRestoreWorkspace)
}

func (r *Routes) handleStatus(c *gin.Context) {
	status := r.status()
	c.JSON(http.StatusOK, statusResponse(status))
}

func (r *Routes) handleSnapshots(c *gin.Context) {
	if r.listSnapshots == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "workspace sync is not enabled"})
		return
	}
	rawCursor := strings.TrimSpace(c.Query("cursor"))
	cursor := workspacesync.NewCommitHashUnchecked(rawCursor)
	if cursor.String() != "" {
		if len(cursor.String()) > 256 || strings.ContainsAny(cursor.String(), " \t\r\n") {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid snapshot cursor"})
			return
		}
	}
	limit := 50
	if rawLimit := strings.TrimSpace(c.Query("limit")); rawLimit != "" {
		parsed, err := strconv.Atoi(rawLimit)
		if err != nil || parsed <= 0 || parsed > 200 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid snapshot limit"})
			return
		}
		limit = parsed
	}
	out, err := r.listSnapshots(c.Request.Context(), cursor, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"snapshots": out.Snapshots, "nextCursor": out.NextCursor})
}

func (r *Routes) handleRestoreWorkspace(c *gin.Context) {
	if r.restoreWorkspace == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "workspace sync is not enabled"})
		return
	}
	user := authmw.MustGetUser(c)
	if user == nil {
		return
	}
	status, err := r.restoreWorkspace(
		c.Request.Context(),
		workspacesync.NewCommitHashUnchecked(strings.TrimSpace(c.Param("commit"))),
		workspacesync.Actor{ID: workspacesync.NewActorIDUnchecked(user.ID), Name: user.Username, Email: user.Email},
		workspacesync.SourceWeb,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, statusResponse(status))
}

func (r *Routes) handleRefresh(c *gin.Context) {
	if r.refresh == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "workspace sync is not enabled"})
		return
	}
	status, err := r.refresh(c.Request.Context(), workspacesync.SyncRequest{
		Reason: workspacesync.ReasonExplicit,
		Source: workspacesync.SourceFilesystem,
		Actor:  workspacesync.PublicEditorActor(),
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, statusResponse(status))
}

func statusResponse(status workspacesync.SyncStatus) gin.H {
	return gin.H{
		"enabled":                    status.Enabled,
		"watcherEnabled":             status.WatcherEnabled,
		"watcherRunning":             status.WatcherRunning,
		"pendingEventCount":          status.PendingEventCount,
		"lastSyncTime":               status.LastSyncTime,
		"lastError":                  status.LastError,
		"lastCommitHash":             status.LastCommitHash,
		"recentChangedMarkdownPaths": status.RecentChangedMarkdownPaths,
		"validationErrors":           status.ValidationErrors,
	}
}
