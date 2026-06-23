package revisions

import (
	"errors"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
)

const (
	ErrCodeRevisionNotFound                    sharederrors.ErrorCode = "revision_not_found"
	ErrCodeRevisionInvalidPageID               sharederrors.ErrorCode = "revision_invalid_page_id"
	ErrCodeRevisionInvalidRevisionID           sharederrors.ErrorCode = "revision_invalid_revision_id"
	ErrCodeRevisionInvalidLimit                sharederrors.ErrorCode = "revision_invalid_limit"
	ErrCodeRevisionCompareInvalidRequest       sharederrors.ErrorCode = "revision_compare_invalid_request"
	ErrCodeRevisionRestoreInvalidPageID        sharederrors.ErrorCode = "revision_restore_invalid_page_id"
	ErrCodeRevisionRestoreInvalidRevision      sharederrors.ErrorCode = "revision_restore_invalid_revision"
	ErrCodeRevisionRestoreRevisionNotFound     sharederrors.ErrorCode = "revision_restore_revision_not_found"
	ErrCodeRevisionRestorePageNotFound         sharederrors.ErrorCode = "revision_restore_page_not_found"
	ErrCodeRevisionRestoreFailed               sharederrors.ErrorCode = "revision_restore_failed"
	ErrCodeRevisionRestoreContentMissing       sharederrors.ErrorCode = "revision_restore_content_missing"
	ErrCodeRevisionRestoreAssetsMissing        sharederrors.ErrorCode = "revision_restore_assets_missing"
	ErrCodeRevisionServiceUnavailable          sharederrors.ErrorCode = "revision_service_unavailable"
	ErrCodeRevisionPreviewContentUnavailable   sharederrors.ErrorCode = "revision_preview_content_unavailable"
	ErrCodeRevisionPreviewAssetsUnavailable    sharederrors.ErrorCode = "revision_preview_assets_unavailable"
	ErrCodeRevisionPreviewAssetNotFound        sharederrors.ErrorCode = "revision_preview_asset_not_found"
	ErrCodeRevisionPreviewAssetInvalidName     sharederrors.ErrorCode = "revision_preview_asset_invalid_name"
	ErrCodeRevisionPreviewAssetBlobUnavailable sharederrors.ErrorCode = "revision_preview_asset_blob_unavailable"
	ErrCodeRevisionInternalError               sharederrors.ErrorCode = "revision_internal_error"
)

// RevisionErrorResponse is the structured JSON error body returned by revision endpoints.
type RevisionErrorResponse struct {
	Error RevisionErrorDetail `json:"error"`
}

// RevisionErrorDetail carries the localization-ready error data.
type RevisionErrorDetail = sharederrors.LocalizedErrorDetail

func respondWithRevisionStatusError(c *gin.Context, status int, code sharederrors.ErrorCode, message, template string, args ...string) {
	c.JSON(status, RevisionErrorResponse{
		Error: sharederrors.NewLocalizedErrorDetail(code, message, template, args...),
	})
}

func NewRevisionNotFoundError(message, template string, args ...string) *sharederrors.LocalizedError {
	return sharederrors.NewLocalizedError(ErrCodeRevisionNotFound, message, template, nil, args...)
}

func NewRevisionAssetBlobUnavailableError(assetName, pageID, revisionID string, cause error) *sharederrors.LocalizedError {
	return sharederrors.NewLocalizedError(
		ErrCodeRevisionPreviewAssetBlobUnavailable,
		"Revision asset blob is unavailable",
		"revision asset blob %s for page %s revision %s is unavailable",
		cause,
		assetName,
		pageID,
		revisionID,
	)
}

func mapRevisionNotFoundError(err error, message, template string, args ...string) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return NewRevisionNotFoundError(message, template, args...)
	}
	return err
}

// respondWithRevisionError is the central error handler for revision endpoints.
func respondWithRevisionError(c *gin.Context, err error) {
	if localized, ok := sharederrors.AsLocalizedError(err); ok {
		c.JSON(revisionErrorStatus(localized.Code), RevisionErrorResponse{Error: sharederrors.LocalizedErrorDetailFromError(localized)})
		return
	}

	switch {
	case errors.Is(err, os.ErrNotExist):
		respondWithRevisionStatusError(c, http.StatusNotFound, ErrCodeRevisionNotFound, "Revision resource not found", "revision resource not found")
	default:
		respondWithRevisionStatusError(c, http.StatusInternalServerError, ErrCodeRevisionInternalError, "Revision request failed", "revision request failed")
	}
}

func revisionErrorStatus(code sharederrors.ErrorCode) int {
	switch code {
	case ErrCodeRevisionNotFound, ErrCodeRevisionRestoreRevisionNotFound, ErrCodeRevisionRestorePageNotFound, ErrCodeRevisionPreviewAssetNotFound:
		return http.StatusNotFound
	case ErrCodeRevisionInvalidPageID, ErrCodeRevisionInvalidRevisionID, ErrCodeRevisionInvalidLimit, ErrCodeRevisionCompareInvalidRequest,
		ErrCodeRevisionRestoreInvalidPageID, ErrCodeRevisionRestoreInvalidRevision, ErrCodeRevisionPreviewAssetInvalidName:
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}
