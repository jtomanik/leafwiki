package importer

import (
	"net/http"

	"github.com/gin-gonic/gin"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
)

const (
	ErrCodeImporterNoPlan           sharederrors.ErrorCode = "importer_no_plan"
	ErrCodeImporterExecutionRunning sharederrors.ErrorCode = "importer_execution_running"
	ErrCodeImporterStateUnavailable sharederrors.ErrorCode = "importer_state_unavailable"
	ErrCodeImporterInternalError    sharederrors.ErrorCode = "importer_internal_error"
	ErrCodeImporterUploadTooLarge   sharederrors.ErrorCode = "importer_upload_too_large"
	ErrCodeImporterMissingFile      sharederrors.ErrorCode = "importer_missing_file"
	ErrCodeImporterFileOpenFailed   sharederrors.ErrorCode = "importer_file_open_failed"
)

// ImporterErrorResponse is the structured JSON error body returned by importer endpoints.
type ImporterErrorResponse struct {
	Error ImporterErrorDetail `json:"error"`
}

// ImporterErrorDetail carries the localization-ready error data.
type ImporterErrorDetail = sharederrors.LocalizedErrorDetail

func respondWithImporterStatusError(c *gin.Context, status int, code sharederrors.ErrorCode, message, template string, args ...string) {
	c.JSON(status, ImporterErrorResponse{
		Error: sharederrors.NewLocalizedErrorDetail(code, message, template, args...),
	})
}

// respondWithImporterError maps errors to JSON responses for importer endpoints.
func respondWithImporterError(c *gin.Context, err error) {
	if loc, ok := sharederrors.AsLocalizedError(err); ok {
		c.JSON(importerErrorStatus(loc.Code), ImporterErrorResponse{Error: sharederrors.LocalizedErrorDetailFromError(loc)})
		return
	}

	respondWithImporterStatusError(c, http.StatusInternalServerError, ErrCodeImporterInternalError, "Importer request failed", "importer request failed")
}

func importerErrorStatus(code sharederrors.ErrorCode) int {
	switch code {
	case ErrCodeImporterNoPlan:
		return http.StatusNotFound
	case ErrCodeImporterExecutionRunning:
		return http.StatusConflict
	case ErrCodeImporterStateUnavailable:
		return http.StatusInternalServerError
	case ErrCodeImporterUploadTooLarge:
		return http.StatusRequestEntityTooLarge
	case ErrCodeImporterMissingFile, ErrCodeImporterFileOpenFailed:
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}
