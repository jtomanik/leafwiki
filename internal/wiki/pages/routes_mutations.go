package pages

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/perber/wiki/internal/core/markdown"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/core/tree"
	"github.com/perber/wiki/internal/http/dto"
	authmw "github.com/perber/wiki/internal/http/middleware/auth"
	"github.com/perber/wiki/internal/wiki/pagesave"
)

// Routes is the RouteRegistrar for the pages domain.

func (r *Routes) handleDelete(c *gin.Context) {
	id := strings.TrimSpace(c.Param("id"))
	recursive := c.DefaultQuery("recursive", "false") == "true"
	version := c.Query("version")
	user := authmw.MustGetUser(c)
	if user == nil {
		return
	}
	if err := r.deletePage.Execute(c.Request.Context(), DeletePageInput{
		UserID: tree.UserIDFromString(user.ID), ID: tree.PageIDFromString(id), Version: tree.PageVersionFromString(version), Recursive: recursive,
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
		UserID: tree.UserIDFromString(user.ID), ID: tree.PageIDFromString(id), Version: tree.PageVersionFromString(req.Version), ParentID: tree.PageIDFromString(req.ParentID),
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
		ParentID: tree.PageIDFromString(parentID), OrderedIDs: semanticPageIDs(req.OrderedIDs),
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
		UserID: tree.UserIDFromString(user.ID), TargetPath: targetPath, TargetTitle: req.Title, Kind: &kind,
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
		UserID: tree.UserIDFromString(user.ID), Source: pagesave.PageMutationSourceWeb, ID: tree.PageIDFromString(id), Version: tree.PageVersionFromString(req.Version), TargetKind: targetKind,
	}); err != nil {
		respondWithPageError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func ValidateConvertTargetKind(kind string) (tree.NodeKind, error) {
	targetKind, ok := tree.ParseNodeKind(kind)
	if !ok {
		return "", sharederrors.NewLocalizedErrorFromCode(ErrCodePageInvalidTargetKind, nil)
	}
	return targetKind, nil
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
		UserID: tree.UserIDFromString(user.ID), SourcePageID: tree.PageIDFromString(sourceID), TargetParentID: semanticPageIDPtr(req.ParentID),
		Title: req.Title, Slug: tree.SlugFromString(req.Slug),
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
		PageID: tree.PageIDFromString(id), Kind: req.Kind, Title: req.Title, Slug: tree.SlugFromString(req.Slug),
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
		Version: tree.PageVersionFromString(req.Version),
		UserID:  tree.UserIDFromString(user.ID),
		Source:  pagesave.PageMutationSourceWeb,
		RefactorPreviewInput: RefactorPreviewInput{
			PageID: tree.PageIDFromString(id), Kind: req.Kind, Title: req.Title, Slug: tree.SlugFromString(req.Slug),
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
	typed := tree.PageIDFromString(*id)
	return &typed
}

func semanticPageIDs(ids []string) []tree.PageID {
	out := make([]tree.PageID, len(ids))
	for i, id := range ids {
		out[i] = tree.PageIDFromString(id)
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
