package assets

import (
	"net/http"

	"github.com/gin-gonic/gin"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/localization"
)

const (
	ErrCodeAssetFileTooLarge     sharederrors.ErrorCode = "asset_file_too_large"
	ErrCodeAssetMissingFile      sharederrors.ErrorCode = "asset_missing_file"
	ErrCodeAssetMissingName      sharederrors.ErrorCode = "asset_missing_name"
	ErrCodeAssetPageNotFound     sharederrors.ErrorCode = "asset_page_not_found"
	ErrCodeAssetNotFound         sharederrors.ErrorCode = "asset_not_found"
	ErrCodeAssetAlreadyExists    sharederrors.ErrorCode = "asset_already_exists"
	ErrCodeAssetInvalidExtension sharederrors.ErrorCode = "asset_invalid_extension"
	ErrCodeAssetInvalidName      sharederrors.ErrorCode = "asset_invalid_name"
	ErrCodeAssetInvalidPayload   sharederrors.ErrorCode = "asset_invalid_payload"
	ErrCodeAssetUploadFailed     sharederrors.ErrorCode = "asset_upload_failed"
	ErrCodeAssetDeleteFailed     sharederrors.ErrorCode = "asset_delete_failed"
	ErrCodeAssetRenameFailed     sharederrors.ErrorCode = "asset_rename_failed"
	ErrCodeAssetInternalError    sharederrors.ErrorCode = "asset_internal_error"
)

const MessageIDAssetDeleteSuccess sharederrors.MessageID = "api.assets.delete.success"

func apiSuccessMessage(messageID sharederrors.MessageID) string {
	return localization.English.Render(messageID, "").Message
}

// AssetErrorResponse is the structured JSON error body returned by asset endpoints.
type AssetErrorResponse struct {
	Error AssetErrorDetail `json:"error"`
}

// AssetErrorDetail carries the localization-ready error data.
type AssetErrorDetail = sharederrors.LocalizedErrorDetail

func respondWithAssetStatusError(c *gin.Context, status int, code sharederrors.ErrorCode, message, template string, args ...string) {
	c.JSON(status, AssetErrorResponse{
		Error: sharederrors.NewLocalizedErrorDetailFromCode(code, args...),
	})
}

func NewAssetFileTooLargeError() *sharederrors.LocalizedError {
	return sharederrors.NewLocalizedErrorFromCode(ErrCodeAssetFileTooLarge, nil)
}

func NewAssetInvalidPayloadError(err error) *sharederrors.LocalizedError {
	return sharederrors.NewLocalizedErrorFromCode(ErrCodeAssetInvalidPayload, err)
}

// respondWithAssetError maps errors to JSON responses for asset endpoints.
func respondWithAssetError(c *gin.Context, err error) {
	if loc, ok := sharederrors.AsLocalizedError(err); ok {
		c.JSON(assetErrorStatus(loc.Code), AssetErrorResponse{Error: sharederrors.LocalizedErrorDetailFromError(loc)})
		return
	}

	respondWithAssetStatusError(c, http.StatusInternalServerError, ErrCodeAssetInternalError, "Asset request failed", "asset request failed")
}

func assetErrorStatus(code sharederrors.ErrorCode) int {
	switch code {
	case ErrCodeAssetFileTooLarge:
		return http.StatusRequestEntityTooLarge
	case ErrCodeAssetPageNotFound, ErrCodeAssetNotFound:
		return http.StatusNotFound
	case ErrCodeAssetAlreadyExists:
		return http.StatusConflict
	case ErrCodeAssetMissingFile, ErrCodeAssetMissingName, ErrCodeAssetInvalidPayload, ErrCodeAssetInvalidExtension, ErrCodeAssetInvalidName:
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}
