package pages

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	coreauth "github.com/perber/wiki/internal/core/auth"
	"github.com/perber/wiki/internal/core/markdown"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/core/tree"
	httpinternal "github.com/perber/wiki/internal/http"
	"github.com/perber/wiki/internal/http/dto"
	authmw "github.com/perber/wiki/internal/http/middleware/auth"
	"github.com/perber/wiki/internal/http/middleware/security"
	"github.com/perber/wiki/internal/wiki/pagesave"
)

// Routes is the RouteRegistrar for the pages domain.
type Routes struct {
	treeService      *tree.TreeService
	createPage       *CreatePageUseCase
	updatePage       *UpdatePageUseCase
	deletePage       *DeletePageUseCase
	movePage         *MovePageUseCase
	convertPage      *ConvertPageUseCase
	copyPage         *CopyPageUseCase
	getPage          *GetPageUseCase
	findByPath       *FindByPathUseCase
	lookupPath       *LookupPagePathUseCase
	resolvePermalink *ResolvePermalinkUseCase
	sortPages        *SortPagesUseCase
	ensurePath       *EnsurePathUseCase
	suggestSlug      *SuggestSlugUseCase
	previewRefactor  *PreviewPageRefactorUseCase
	applyRefactor    *ApplyPageRefactorUseCase
	userResolver     *coreauth.UserResolver
	authService      *coreauth.AuthService
}

// RoutesConfig holds the dependencies required to build a Routes instance.
type RoutesConfig struct {
	TreeService      *tree.TreeService
	CreatePage       *CreatePageUseCase
	UpdatePage       *UpdatePageUseCase
	DeletePage       *DeletePageUseCase
	MovePage         *MovePageUseCase
	ConvertPage      *ConvertPageUseCase
	CopyPage         *CopyPageUseCase
	GetPage          *GetPageUseCase
	FindByPath       *FindByPathUseCase
	LookupPath       *LookupPagePathUseCase
	ResolvePermalink *ResolvePermalinkUseCase
	SortPages        *SortPagesUseCase
	EnsurePath       *EnsurePathUseCase
	SuggestSlug      *SuggestSlugUseCase
	PreviewRefactor  *PreviewPageRefactorUseCase
	ApplyRefactor    *ApplyPageRefactorUseCase
	UserResolver     *coreauth.UserResolver
	AuthService      *coreauth.AuthService
}

// NewRoutes constructs the pages RouteRegistrar.
func NewRoutes(cfg RoutesConfig) *Routes {
	return &Routes{
		treeService:      cfg.TreeService,
		createPage:       cfg.CreatePage,
		updatePage:       cfg.UpdatePage,
		deletePage:       cfg.DeletePage,
		movePage:         cfg.MovePage,
		convertPage:      cfg.ConvertPage,
		copyPage:         cfg.CopyPage,
		getPage:          cfg.GetPage,
		findByPath:       cfg.FindByPath,
		lookupPath:       cfg.LookupPath,
		resolvePermalink: cfg.ResolvePermalink,
		sortPages:        cfg.SortPages,
		ensurePath:       cfg.EnsurePath,
		suggestSlug:      cfg.SuggestSlug,
		previewRefactor:  cfg.PreviewRefactor,
		applyRefactor:    cfg.ApplyRefactor,
		userResolver:     cfg.UserResolver,
		authService:      cfg.AuthService,
	}
}

// RegisterRoutes implements RouteRegistrar.
func (r *Routes) RegisterRoutes(ctx httpinternal.RouterContext) {
	opts := ctx.Opts

	if opts.PublicAccess {
		pub := ctx.Base.Group("/api")
		pub.GET("/tree", r.handleGetTree)
		pub.GET("/pages/by-path", r.handleGetByPath)
		pub.GET("/pages/lookup", r.handleLookupPath)
		pub.GET("/pages/permalink/:id", r.handleResolvePermalink)
		pub.GET("/pages/:id", r.handleGetPage)
	}

	authGroup := ctx.Base.Group("/api")
	authGroup.Use(
		authmw.InjectPublicEditor(opts.AuthDisabled),
		authmw.RequireAuth(r.authService, ctx.AuthCookies, opts.AuthDisabled),
		security.CSRFMiddleware(ctx.CSRFCookie),
	)

	if !opts.PublicAccess {
		authGroup.GET("/tree", r.handleGetTree)
		authGroup.GET("/pages/:id", r.handleGetPage)
		authGroup.GET("/pages/lookup", r.handleLookupPath)
		authGroup.GET("/pages/by-path", r.handleGetByPath)
		authGroup.GET("/pages/permalink/:id", r.handleResolvePermalink)
	}

	authGroup.GET("/pages/slug-suggestion", authmw.RequireEditorOrAdmin(), r.handleSuggestSlug)
	authGroup.POST("/pages", authmw.RequireEditorOrAdmin(), r.handleCreate)
	authGroup.PUT("/pages/:id", authmw.RequireEditorOrAdmin(), r.handleUpdate)
	authGroup.DELETE("/pages/:id", authmw.RequireEditorOrAdmin(), r.handleDelete)
	authGroup.PUT("/pages/:id/move", authmw.RequireEditorOrAdmin(), r.handleMove)
	authGroup.PUT("/pages/:id/sort", authmw.RequireEditorOrAdmin(), r.handleSort)
	authGroup.POST("/pages/ensure", authmw.RequireEditorOrAdmin(), r.handleEnsurePath)
	authGroup.POST("/pages/convert/:id", authmw.RequireEditorOrAdmin(), r.handleConvert)
	authGroup.POST("/pages/copy/:id", authmw.RequireEditorOrAdmin(), r.handleCopy)
	if opts.EnableLinkRefactor {
		authGroup.POST("/pages/:id/refactor/preview", authmw.RequireEditorOrAdmin(), r.handleRefactorPreview)
		authGroup.POST("/pages/:id/refactor/apply", authmw.RequireEditorOrAdmin(), r.handleRefactorApply)
	}
}

// ─── Handlers ───────────────────────────────────────────────────────────────

func (r *Routes) handleGetTree(c *gin.Context) {
	root := r.treeService.GetTree()
	depthStr := strings.TrimSpace(c.Query("depth"))
	if depthStr == "" {
		c.JSON(http.StatusOK, dto.ToAPINodeWithContentPaths(root, "", r.userResolver, r.treeService.ContentPathForNode))
		return
	}
	depth, err := strconv.Atoi(depthStr)
	if err != nil {
		depth = -1
	}
	c.JSON(http.StatusOK, dto.ToAPINodeWithContentPathsAndDepth(root, "", r.userResolver, r.treeService.ContentPathForNode, depth))
}

func (r *Routes) handleGetPage(c *gin.Context) {
	id := strings.TrimSpace(c.Param("id"))
	out, err := r.getPage.Execute(c.Request.Context(), GetPageInput{ID: tree.NewPageIDUnchecked(id)})
	if err != nil {
		respondWithPageError(c, err)
		return
	}
	r.respondPage(c, http.StatusOK, out.Page)
}

func (r *Routes) handleGetByPath(c *gin.Context) {
	rawPath, hasPath := c.GetQuery("path")
	if !hasPath {
		respondWithPageError(c, sharederrors.NewLocalizedErrorFromCode(ErrCodePageMissingPath, nil))
		return
	}
	out, err := r.findByPathInput(c.Request.Context(), rawPath, c.Query("kind"))
	if err != nil {
		respondWithPageError(c, err)
		return
	}
	depth := 0
	if out.Page.Kind == tree.NodeKindSection {
		depth = 1
	}
	r.respondPageWithDepth(c, http.StatusOK, out.Page, depth)
}

func (r *Routes) findByPathInput(ctx context.Context, rawPath string, rawKind string) (*FindByPathOutput, error) {
	if rawPath == "" {
		kind := tree.NodeKind("")
		if strings.TrimSpace(rawKind) != "" {
			validKind, err := ValidatePageKindString(strings.TrimSpace(rawKind))
			if err != nil {
				return nil, err
			}
			kind = validKind
		}
		if kind != "" && kind != tree.NodeKindSection {
			return nil, tree.ErrPageNotFound
		}
		page, err := r.treeService.GetPage("root")
		if err != nil {
			return nil, err
		}
		return &FindByPathOutput{Page: page}, nil
	}
	if out, handled, err := FindReadmeMarkdownPathFallback(rawPath, rawKind, ReadmeMarkdownPathFallbackLookup{
		RootDir: r.treeService.RootDir(),
		FindByPath: func(input FindByPathInput) (*FindByPathOutput, error) {
			return r.findByPath.Execute(ctx, input)
		},
		RootPage: func() (*tree.Page, error) {
			return r.treeService.GetPage("root")
		},
	}); err != nil || handled {
		return out, err
	}
	routePath, kind, err := NormalizePagePathInput(rawPath, rawKind)
	if err != nil {
		return nil, err
	}
	return r.findByPath.Execute(ctx, FindByPathInput{RoutePath: routePath, Kind: kind})
}

func (r *Routes) handleLookupPath(c *gin.Context) {
	path := strings.TrimSpace(c.Query("path"))
	kind := tree.NodeKind("")
	rawKind := strings.TrimSpace(c.Query("kind"))
	if rawKind != "" {
		validKind, kindErr := ValidatePageKindString(rawKind)
		if kindErr != nil {
			respondWithPageError(c, kindErr)
			return
		}
		kind = validKind
	}
	routePath, err := ValidateSemanticRoutePath(path)
	if err != nil {
		respondWithPageError(c, err)
		return
	}
	out, err := r.lookupPath.Execute(c.Request.Context(), LookupPagePathInput{Path: routePath, Kind: kind})
	if err != nil {
		respondWithPageError(c, err)
		return
	}
	c.JSON(http.StatusOK, out.Lookup)
}

func (r *Routes) handleResolvePermalink(c *gin.Context) {
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		respondWithPageStatusError(c, http.StatusBadRequest, ErrCodePageMissingID, "Page ID is required", "page id is required")
		return
	}
	out, err := r.resolvePermalink.Execute(c.Request.Context(), ResolvePermalinkInput{ID: tree.NewPageIDUnchecked(id)})
	if err != nil {
		respondWithPageError(c, err)
		return
	}
	c.JSON(http.StatusOK, out.Target)
}

func (r *Routes) handleSuggestSlug(c *gin.Context) {
	title, err := ValidateSuggestSlugTitle(c.Query("title"))
	if err != nil {
		respondWithPageError(c, err)
		return
	}
	out, err := r.suggestSlug.Execute(c.Request.Context(), SuggestSlugInput{
		ParentID:  tree.NewPageIDUnchecked(strings.TrimSpace(c.Query("parentId"))),
		CurrentID: tree.NewPageIDUnchecked(strings.TrimSpace(c.Query("currentId"))),
		Title:     title,
	})
	if err != nil {
		respondWithPageError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"slug": out.Slug})
}

func ValidateSuggestSlugTitle(title string) (string, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return "", sharederrors.NewLocalizedErrorFromCode(ErrCodePageMissingTitle, nil)
	}
	if tree.NewSlugService().GenerateValidSlug(title) == "" {
		return "", sharederrors.NewLocalizedErrorFromCode(ErrCodePageInvalidTitle, nil)
	}
	return title, nil
}

func (r *Routes) handleCreate(c *gin.Context) {
	var req struct {
		ParentID *string `json:"parentId"`
		Title    string  `json:"title" binding:"required"`
		Slug     string  `json:"slug" binding:"required"`
		Kind     *string `json:"kind"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		respondWithPageStatusError(c, http.StatusBadRequest, ErrCodePageInvalidRequest, "Invalid request", "invalid request")
		return
	}
	user := authmw.MustGetUser(c)
	if user == nil {
		return
	}
	kind, err := ValidatePageKind(req.Kind)
	if err != nil {
		respondWithPageError(c, err)
		return
	}
	out, err := r.createPage.Execute(c.Request.Context(), CreatePageInput{
		UserID: tree.NewUserIDUnchecked(user.ID), ParentID: semanticPageIDPtr(req.ParentID), Title: req.Title, Slug: tree.NewSlugUnchecked(req.Slug), Kind: &kind,
	})
	if err != nil {
		respondWithPageError(c, err)
		return
	}
	r.respondPage(c, http.StatusCreated, out.Page)
}

func (r *Routes) handleUpdate(c *gin.Context) {
	id := strings.TrimSpace(c.Param("id"))
	var req struct {
		Version    string             `json:"version" binding:"required"`
		Title      string             `json:"title" binding:"required"`
		Slug       string             `json:"slug" binding:"required"`
		Content    *string            `json:"content"`
		Tags       *[]string          `json:"tags"`
		Properties *map[string]string `json:"properties"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		respondWithPageStatusError(c, http.StatusBadRequest, ErrCodePageInvalidRequest, "Invalid request", "invalid request")
		return
	}
	var tagsForValidation []string
	if req.Tags != nil {
		tagsForValidation = *req.Tags
	}
	var propertiesForValidation map[string]string
	if req.Properties != nil {
		propertiesForValidation = *req.Properties
	}
	if err := ValidatePageMetadataInput(tagsForValidation, propertiesForValidation); err != nil {
		respondWithPageError(c, err)
		return
	}
	user := authmw.MustGetUser(c)
	if user == nil {
		return
	}

	contentToSave := req.Content
	fromImport := false
	if req.Content != nil || req.Tags != nil || req.Properties != nil {
		currentRaw, err := r.treeService.ReadPageRaw(tree.NewPageIDUnchecked(id))
		if err != nil {
			respondWithPageError(c, err)
			return
		}
		body := ""
		if req.Content != nil {
			body = *req.Content
		} else {
			doc, _, err := markdown.ParsePageDocument(currentRaw)
			if err != nil {
				respondWithPageStatusError(c, http.StatusInternalServerError, ErrCodePageInternalError, "Failed to parse metadata", "failed to parse metadata")
				return
			}
			body = doc.Body
		}
		combined, err := BuildMarkdownWithPublicMetadataPatch(currentRaw, tree.NewPageIDUnchecked(id), req.Title, PublicMetadataPatch{
			Tags:              tagsForValidation,
			TagsPresent:       req.Tags != nil,
			Properties:        propertiesForValidation,
			PropertiesPresent: req.Properties != nil,
		}, body)
		if err != nil {
			respondWithPageStatusError(c, http.StatusInternalServerError, ErrCodePageInternalError, "Failed to build metadata", "failed to build metadata")
			return
		}
		contentToSave = &combined
		fromImport = true
	}

	kind := tree.NodeKindPage
	out, err := r.updatePage.Execute(c.Request.Context(), UpdatePageInput{
		UserID: tree.NewUserIDUnchecked(user.ID), ID: tree.NewPageIDUnchecked(id), Version: tree.NewPageVersionUnchecked(req.Version), Title: req.Title, Slug: tree.NewSlugUnchecked(req.Slug),
		Content: contentToSave, Kind: &kind, FromImport: fromImport,
	})
	if err != nil {
		respondWithPageError(c, err)
		return
	}
	r.respondPage(c, http.StatusOK, out.Page)
}

func BuildMarkdownWithPublicMetadata(pageID string, title string, tags []string, properties map[string]string, body string) (string, error) {
	fields := make(map[string]interface{}, len(properties))
	for key, value := range properties {
		fields[key] = value
	}
	if len(fields) == 0 {
		fields = nil
	}
	return markdown.RenderPageDocument(markdown.PageDocument{
		Body: body,
		Metadata: markdown.PageMetadata{
			Version: 1,
			Page: markdown.PageMetadataPage{
				ID:    strings.TrimSpace(pageID),
				Title: strings.TrimSpace(title),
			},
			Tags:   normalizeTagInputs(tags),
			Fields: fields,
		},
	})
}

func (r *Routes) handleDelete(c *gin.Context) {
	id := strings.TrimSpace(c.Param("id"))
	recursive := c.DefaultQuery("recursive", "false") == "true"
	version := c.Query("version")
	user := authmw.MustGetUser(c)
	if user == nil {
		return
	}
	if err := r.deletePage.Execute(c.Request.Context(), DeletePageInput{
		UserID: tree.NewUserIDUnchecked(user.ID), ID: tree.NewPageIDUnchecked(id), Version: tree.NewPageVersionUnchecked(version), Recursive: recursive,
	}); err != nil {
		respondWithPageError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"messageId": MessageIDAPIPagesDeleteSuccess, "message": apiSuccessMessage(MessageIDAPIPagesDeleteSuccess)})
}

func (r *Routes) handleMove(c *gin.Context) {
	id := strings.TrimSpace(c.Param("id"))
	var req struct {
		Version  string `json:"version" binding:"required"`
		ParentID string `json:"parentId"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		respondWithPageStatusError(c, http.StatusBadRequest, ErrCodePageInvalidPayload, "Invalid payload", "invalid payload")
		return
	}
	user := authmw.MustGetUser(c)
	if user == nil {
		return
	}
	if err := r.movePage.Execute(c.Request.Context(), MovePageInput{
		UserID: tree.NewUserIDUnchecked(user.ID), ID: tree.NewPageIDUnchecked(id), Version: tree.NewPageVersionUnchecked(req.Version), ParentID: tree.NewPageIDUnchecked(req.ParentID),
	}); err != nil {
		respondWithPageError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"messageId": MessageIDAPIPagesMoveSuccess, "message": apiSuccessMessage(MessageIDAPIPagesMoveSuccess)})
}

func (r *Routes) handleSort(c *gin.Context) {
	parentID := strings.TrimSpace(c.Param("id"))
	var req struct {
		OrderedIDs []string `json:"orderedIds"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		respondWithPageStatusError(c, http.StatusBadRequest, ErrCodePageInvalidRequest, "Invalid request", "invalid request")
		return
	}
	if err := r.sortPages.Execute(c.Request.Context(), SortPagesInput{
		ParentID: tree.NewPageIDUnchecked(parentID), OrderedIDs: semanticPageIDs(req.OrderedIDs),
	}); err != nil {
		respondWithPageError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"messageId": MessageIDAPIPagesSortSuccess, "message": apiSuccessMessage(MessageIDAPIPagesSortSuccess)})
}

func (r *Routes) handleEnsurePath(c *gin.Context) {
	var req struct {
		Path  string  `json:"path" binding:"required"`
		Title string  `json:"title" binding:"required"`
		Kind  *string `json:"kind"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		respondWithPageStatusError(c, http.StatusBadRequest, ErrCodePageInvalidRequest, "Invalid request", "invalid request")
		return
	}
	user := authmw.MustGetUser(c)
	if user == nil {
		return
	}
	kind, err := ValidatePageKind(req.Kind)
	if err != nil {
		respondWithPageError(c, err)
		return
	}
	targetPath, err := ValidateSemanticRoutePath(req.Path)
	if err != nil {
		respondWithPageError(c, err)
		return
	}
	out, err := r.ensurePath.Execute(c.Request.Context(), EnsurePathInput{
		UserID: tree.NewUserIDUnchecked(user.ID), TargetPath: targetPath, TargetTitle: req.Title, Kind: &kind,
	})
	if err != nil {
		respondWithPageError(c, err)
		return
	}
	r.respondPage(c, http.StatusOK, out.Page)
}

func (r *Routes) handleConvert(c *gin.Context) {
	id := strings.TrimSpace(c.Param("id"))
	var req struct {
		Kind    string `json:"targetKind" binding:"required"`
		Version string `json:"version" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		respondWithPageStatusError(c, http.StatusBadRequest, ErrCodePageInvalidRequest, "Invalid request", "invalid request")
		return
	}
	targetKind, err := ValidateConvertTargetKind(req.Kind)
	if err != nil {
		respondWithPageError(c, err)
		return
	}
	user := authmw.MustGetUser(c)
	if user == nil {
		return
	}
	if err := r.convertPage.Execute(c.Request.Context(), ConvertPageInput{
		UserID: tree.NewUserIDUnchecked(user.ID), Source: pagesave.PageMutationSourceWeb, ID: tree.NewPageIDUnchecked(id), Version: tree.NewPageVersionUnchecked(req.Version), TargetKind: targetKind,
	}); err != nil {
		respondWithPageError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func ValidateConvertTargetKind(kind string) (tree.NodeKind, error) {
	if kind != string(tree.NodeKindPage) && kind != string(tree.NodeKindSection) {
		return "", sharederrors.NewLocalizedErrorFromCode(ErrCodePageInvalidTargetKind, nil)
	}
	return tree.NodeKind(kind), nil
}

func (r *Routes) handleCopy(c *gin.Context) {
	sourceID := strings.TrimSpace(c.Param("id"))
	var req struct {
		ParentID *string `json:"targetParentId"`
		Title    string  `json:"title" binding:"required"`
		Slug     string  `json:"slug" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		respondWithPageStatusError(c, http.StatusBadRequest, ErrCodePageInvalidRequest, "Invalid request", "invalid request")
		return
	}
	user := authmw.MustGetUser(c)
	if user == nil {
		return
	}
	out, err := r.copyPage.Execute(c.Request.Context(), CopyPageInput{
		UserID: tree.NewUserIDUnchecked(user.ID), SourcePageID: tree.NewPageIDUnchecked(sourceID), TargetParentID: semanticPageIDPtr(req.ParentID),
		Title: req.Title, Slug: tree.NewSlugUnchecked(req.Slug),
	})
	if err != nil {
		respondWithPageError(c, err)
		return
	}
	r.respondPage(c, http.StatusCreated, out.Page)
}

func (r *Routes) handleRefactorPreview(c *gin.Context) {
	id := strings.TrimSpace(c.Param("id"))
	var req struct {
		Kind        string  `json:"kind" binding:"required"`
		Title       string  `json:"title"`
		Slug        string  `json:"slug"`
		Content     *string `json:"content"`
		NewParentID *string `json:"parentId"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		respondWithPageStatusError(c, http.StatusBadRequest, ErrCodePageInvalidRequest, "Invalid request", "invalid request")
		return
	}
	out, err := r.previewRefactor.Execute(c.Request.Context(), RefactorPreviewInput{
		PageID: tree.NewPageIDUnchecked(id), Kind: req.Kind, Title: req.Title, Slug: tree.NewSlugUnchecked(req.Slug),
		Content: req.Content, NewParentID: semanticPageIDPtr(req.NewParentID),
	})
	if err != nil {
		respondWithPageError(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (r *Routes) handleRefactorApply(c *gin.Context) {
	id := strings.TrimSpace(c.Param("id"))
	var req struct {
		Version      string  `json:"version" binding:"required"`
		Kind         string  `json:"kind" binding:"required"`
		Title        string  `json:"title"`
		Slug         string  `json:"slug"`
		Content      *string `json:"content"`
		NewParentID  *string `json:"parentId"`
		RewriteLinks bool    `json:"rewriteLinks"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		respondWithPageStatusError(c, http.StatusBadRequest, ErrCodePageInvalidRequest, "Invalid request", "invalid request")
		return
	}
	user := authmw.MustGetUser(c)
	if user == nil {
		return
	}
	page, err := r.applyRefactor.Execute(c.Request.Context(), RefactorApplyInput{
		Version: tree.NewPageVersionUnchecked(req.Version),
		UserID:  tree.NewUserIDUnchecked(user.ID),
		Source:  pagesave.PageMutationSourceWeb,
		RefactorPreviewInput: RefactorPreviewInput{
			PageID: tree.NewPageIDUnchecked(id), Kind: req.Kind, Title: req.Title, Slug: tree.NewSlugUnchecked(req.Slug),
			Content: req.Content, NewParentID: semanticPageIDPtr(req.NewParentID),
		},
		RewriteLinks: req.RewriteLinks,
	})
	if err != nil {
		respondWithPageError(c, err)
		return
	}
	r.respondPage(c, http.StatusOK, page)
}

func (r *Routes) respondPage(c *gin.Context, status int, page *tree.Page) {
	apiPage := dto.ToAPIPage(page, r.userResolver)
	r.enrichPageMetadata(apiPage)
	c.JSON(status, apiPage)
}

func (r *Routes) respondPageWithDepth(c *gin.Context, status int, page *tree.Page, depth int) {
	apiPage := dto.ToAPIPageWithDepth(page, r.userResolver, depth)
	r.enrichPageMetadata(apiPage)
	c.JSON(status, apiPage)
}

func semanticPageIDPtr(id *string) *tree.PageID {
	if id == nil {
		return nil
	}
	typed := tree.NewPageIDUnchecked(*id)
	return &typed
}

func semanticPageIDs(ids []string) []tree.PageID {
	out := make([]tree.PageID, len(ids))
	for i, id := range ids {
		out[i] = tree.NewPageIDUnchecked(id)
	}
	return out
}

func (r *Routes) enrichPageMetadata(page *dto.Page) {
	EnrichPageMetadata(page, r.treeService.ReadPageRaw)
}

func ValidatePageMetadataInput(tags []string, properties map[string]string) error {
	ve := sharederrors.NewValidationErrors()
	seenTags := map[string]struct{}{}

	for index, tag := range tags {
		trimmed := strings.TrimSpace(tag)
		field := "tags[" + strconv.Itoa(index) + "]"
		if trimmed == "" {
			ve.AddWithCode(field, FieldCodePageTagRequired, MessageIDPageTagRequired)
			continue
		}
		if trimmed != tag {
			ve.AddWithCode(field, FieldCodePageTagWhitespace, MessageIDPageTagWhitespace)
			continue
		}
		key := strings.ToLower(trimmed)
		if _, exists := seenTags[key]; exists {
			ve.AddWithCode(field, FieldCodePageTagDuplicate, MessageIDPageTagDuplicate)
			continue
		}
		seenTags[key] = struct{}{}
	}

	for rawKey := range properties {
		key := strings.TrimSpace(rawKey)
		field := "properties." + rawKey
		switch {
		case key == "":
			ve.AddWithCode(field, FieldCodePagePropertyKeyRequired, MessageIDPagePropertyKeyRequired)
		case key != rawKey:
			ve.AddWithCode(field, FieldCodePagePropertyKeyWhitespace, MessageIDPagePropertyKeyWhitespace)
		case markdown.IsReservedMetadataKey(key):
			ve.AddWithCode(field, FieldCodePagePropertyKeyReserved, MessageIDPagePropertyKeyReservedPrefix)
		case strings.ToLower(key) == "tags" || strings.ToLower(key) == "title":
			ve.AddWithCode(field, FieldCodePagePropertyKeyReserved, MessageIDPagePropertyKeyReserved)
		}
	}

	if ve.HasErrors() {
		return ve
	}

	return nil
}
