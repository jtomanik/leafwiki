package pages

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/core/tree"
	"github.com/perber/wiki/internal/localization"
)

// Error codes for the pages domain.
const (
	ErrCodePageNotFound            sharederrors.ErrorCode = "page_not_found"
	ErrCodePageParentNotFound      sharederrors.ErrorCode = "page_parent_not_found"
	ErrCodePageSlugConflict        sharederrors.ErrorCode = "page_slug_conflict"
	ErrCodePageHasChildren         sharederrors.ErrorCode = "page_has_children"
	ErrCodePageCircularMove        sharederrors.ErrorCode = "page_circular_move"
	ErrCodePageCannotMoveToSelf    sharederrors.ErrorCode = "page_cannot_move_to_self"
	ErrCodePageRootOperation       sharederrors.ErrorCode = "page_root_operation"
	ErrCodePageConvertNotAllowed   sharederrors.ErrorCode = "page_convert_not_allowed"
	ErrCodePageInternalError       sharederrors.ErrorCode = "page_internal_error"
	ErrCodePageMissingPath         sharederrors.ErrorCode = "page_missing_path"
	ErrCodePageInvalidPath         sharederrors.ErrorCode = "page_invalid_path"
	ErrCodePageMissingID           sharederrors.ErrorCode = "page_missing_id"
	ErrCodePageMissingTitle        sharederrors.ErrorCode = "page_missing_title"
	ErrCodePageInvalidTitle        sharederrors.ErrorCode = "page_invalid_title"
	ErrCodePageVersionRequired     sharederrors.ErrorCode = "page_version_required"
	ErrCodePageVersionConflict     sharederrors.ErrorCode = "page_version_conflict"
	ErrCodePageInvalidRequest      sharederrors.ErrorCode = "page_invalid_request"
	ErrCodePageInvalidPayload      sharederrors.ErrorCode = "page_invalid_payload"
	ErrCodePageInvalidKind         sharederrors.ErrorCode = "page_invalid_kind"
	ErrCodePageInvalidParentID     sharederrors.ErrorCode = "page_invalid_parent_id"
	ErrCodePageInvalidTargetKind   sharederrors.ErrorCode = "page_invalid_target_kind"
	ErrCodePageInvalidRefactorKind sharederrors.ErrorCode = "page_invalid_refactor_kind"
)

const (
	FieldCodePageTitleRequired         sharederrors.FieldErrorCode = "page_title_required"
	FieldCodePageKindRequired          sharederrors.FieldErrorCode = "page_kind_required"
	FieldCodePageKindInvalid           sharederrors.FieldErrorCode = "page_kind_invalid"
	FieldCodePageSlugInvalid           sharederrors.FieldErrorCode = "page_slug_invalid"
	FieldCodePagePathRequired          sharederrors.FieldErrorCode = "page_path_required"
	FieldCodePagePathInvalid           sharederrors.FieldErrorCode = "page_path_invalid"
	FieldCodePageTagRequired           sharederrors.FieldErrorCode = "page_tag_required"
	FieldCodePageTagWhitespace         sharederrors.FieldErrorCode = "page_tag_whitespace"
	FieldCodePageTagDuplicate          sharederrors.FieldErrorCode = "page_tag_duplicate"
	FieldCodePagePropertyKeyRequired   sharederrors.FieldErrorCode = "page_property_key_required"
	FieldCodePagePropertyKeyWhitespace sharederrors.FieldErrorCode = "page_property_key_whitespace"
	FieldCodePagePropertyKeyReserved   sharederrors.FieldErrorCode = "page_property_key_reserved"
)

const (
	MessageIDPageTitleRequired             sharederrors.MessageID = "validation.page.title_required"
	MessageIDPageKindRequired              sharederrors.MessageID = "validation.page.kind_required"
	MessageIDPageKindInvalid               sharederrors.MessageID = "validation.page.kind_invalid"
	MessageIDPageSlugInvalid               sharederrors.MessageID = "validation.page.slug_invalid"
	MessageIDPagePathRequired              sharederrors.MessageID = "validation.page.path_required"
	MessageIDPagePathInvalid               sharederrors.MessageID = "validation.page.path_invalid"
	MessageIDPageTagRequired               sharederrors.MessageID = "validation.page.tag_required"
	MessageIDPageTagWhitespace             sharederrors.MessageID = "validation.page.tag_whitespace"
	MessageIDPageTagDuplicate              sharederrors.MessageID = "validation.page.tag_duplicate"
	MessageIDPagePropertyKeyRequired       sharederrors.MessageID = "validation.page.property_key_required"
	MessageIDPagePropertyKeyWhitespace     sharederrors.MessageID = "validation.page.property_key_whitespace"
	MessageIDPagePropertyKeyReserved       sharederrors.MessageID = "validation.page.property_key_reserved"
	MessageIDPagePropertyKeyReservedPrefix sharederrors.MessageID = "validation.page.property_key_reserved_prefix"
)

const (
	MessageIDAPIPagesDeleteSuccess sharederrors.MessageID = "api.pages.delete.success"
	MessageIDAPIPagesMoveSuccess   sharederrors.MessageID = "api.pages.move.success"
	MessageIDAPIPagesSortSuccess   sharederrors.MessageID = "api.pages.sort.success"
)

const pageValidationErrorCode = "validation_error"

func newPageRootOperationError(operation string) *sharederrors.LocalizedError {
	return sharederrors.NewLocalizedErrorFromCode(ErrCodePageRootOperation, nil, operation)
}

// PageErrorResponse is the structured JSON error body returned by page endpoints.
type PageErrorResponse struct {
	Error PageErrorDetail `json:"error"`
}

// PageErrorDetail carries the localization-ready error data.
type PageErrorDetail = sharederrors.LocalizedErrorDetail

func newPageErrorDetail(code sharederrors.ErrorCode, message, template string, args ...string) PageErrorDetail {
	return PageErrorDetail{
		Code:      code,
		MessageID: sharederrors.MessageIDForCode(code),
		Message:   message,
		Template:  template,
		Args:      append([]string(nil), args...),
	}
}

func apiSuccessMessage(messageID sharederrors.MessageID) string {
	return localization.English.Render(messageID, "").Message
}

// PageErrorDetailForError maps page-domain errors to the same localization-ready
// contract used by HTTP routes. The boolean is false when the error is outside
// the page domain and callers should preserve their existing fallback behavior.
func PageErrorDetailForError(err error) (PageErrorDetail, int, bool) {
	if loc, ok := sharederrors.AsLocalizedError(err); ok {
		return sharederrors.LocalizedErrorDetailFromError(loc), pageErrorStatus(loc.Code), true
	}

	switch {
	case errors.Is(err, tree.ErrPageNotFound):
		return newPageErrorDetail(ErrCodePageNotFound, "Page not found", "page not found"), http.StatusNotFound, true
	case errors.Is(err, tree.ErrParentNotFound):
		return newPageErrorDetail(ErrCodePageParentNotFound, "Parent page not found", "parent page not found"), http.StatusNotFound, true
	case errors.Is(err, tree.ErrPageHasChildren):
		return newPageErrorDetail(ErrCodePageHasChildren, "Page has children, use recursive delete", "page has children"), http.StatusBadRequest, true
	case errors.Is(err, tree.ErrPageAlreadyExists):
		return newPageErrorDetail(ErrCodePageSlugConflict, "Page already exists", "page already exists"), http.StatusBadRequest, true
	case errors.Is(err, tree.ErrMovePageCircularReference):
		return newPageErrorDetail(ErrCodePageCircularMove, "Move would create a circular reference", "circular reference detected"), http.StatusBadRequest, true
	case errors.Is(err, tree.ErrPageCannotBeMovedToItself):
		return newPageErrorDetail(ErrCodePageCannotMoveToSelf, "Page cannot be moved to itself", "page cannot be moved to itself"), http.StatusBadRequest, true
	case errors.Is(err, tree.ErrConvertNotAllowed):
		return newPageErrorDetail(ErrCodePageConvertNotAllowed, "Convert operation not allowed", "convert not allowed"), http.StatusBadRequest, true
	case errors.Is(err, tree.ErrVersionConflict):
		return newPageErrorDetail(ErrCodePageVersionConflict, "Page was changed by another request", "page was changed by another request"), http.StatusConflict, true
	case errors.Is(err, tree.ErrVersionRequired):
		return newPageErrorDetail(ErrCodePageVersionRequired, "Page version is required", "page version is required"), http.StatusBadRequest, true
	case errors.Is(err, tree.ErrTreeNotLoaded):
		return newPageErrorDetail(ErrCodePageInternalError, "Tree not loaded", "tree not loaded"), http.StatusInternalServerError, true
	default:
		return PageErrorDetail{}, 0, false
	}
}

func respondWithPageStatusError(c *gin.Context, status int, code sharederrors.ErrorCode, message, template string, args ...string) {
	c.JSON(status, PageErrorResponse{
		Error: newPageErrorDetail(code, message, template, args...),
	})
}

// respondWithPageError is the central error handler for all page endpoints.
// It checks for LocalizedError first (rich, template-ready), then falls back to
// sentinel error mapping so that lower-level service errors produce correct HTTP statuses.
func respondWithPageError(c *gin.Context, err error) {
	if detail, status, ok := PageErrorDetailForError(err); ok {
		c.JSON(status, PageErrorResponse{Error: detail})
		return
	}

	var vErr *sharederrors.ValidationErrors
	if errors.As(err, &vErr) {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":  pageValidationErrorCode,
			"fields": vErr.Errors,
		})
		return
	}

	respondWithPageStatusError(c, http.StatusInternalServerError, ErrCodePageInternalError, err.Error(), "internal error")
}

func pageErrorStatus(code sharederrors.ErrorCode) int {
	switch code {
	case ErrCodePageNotFound, ErrCodePageParentNotFound:
		return http.StatusNotFound
	case ErrCodePageHasChildren, ErrCodePageCircularMove, ErrCodePageCannotMoveToSelf, ErrCodePageSlugConflict,
		ErrCodePageConvertNotAllowed, ErrCodePageRootOperation, ErrCodePageVersionRequired,
		ErrCodePageMissingPath, ErrCodePageMissingID, ErrCodePageMissingTitle, ErrCodePageInvalidTitle, ErrCodePageInvalidRequest,
		ErrCodePageInvalidPath, ErrCodePageInvalidPayload, ErrCodePageInvalidKind, ErrCodePageInvalidParentID, ErrCodePageInvalidTargetKind, ErrCodePageInvalidRefactorKind:
		return http.StatusBadRequest
	case ErrCodePageVersionConflict:
		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
}
