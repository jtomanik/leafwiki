package tags

import (
	"net/http"

	"github.com/gin-gonic/gin"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
)

const (
	ErrCodeTagsInternal     sharederrors.ErrorCode = "tags_internal_error"
	ErrCodeTagsMissingParam sharederrors.ErrorCode = "tags_missing_param"
	ErrCodeTagsInvalidLimit sharederrors.ErrorCode = "tags_invalid_limit"
)

type tagsErrorResponse struct {
	Error tagsErrorDetail `json:"error"`
}

type tagsErrorDetail = sharederrors.LocalizedErrorDetail

func respondWithTagsError(c *gin.Context, err error) {
	if loc, ok := sharederrors.AsLocalizedError(err); ok {
		c.JSON(tagsErrorStatus(loc.Code), tagsErrorResponse{
			Error: sharederrors.LocalizedErrorDetailFromError(loc),
		})
		return
	}

	c.JSON(http.StatusInternalServerError, tagsErrorResponse{
		Error: sharederrors.NewLocalizedErrorDetailFromCode(ErrCodeTagsInternal),
	})
}

func respondWithTagsBadRequest(c *gin.Context, code sharederrors.ErrorCode, message, template string) {
	c.JSON(http.StatusBadRequest, tagsErrorResponse{
		Error: sharederrors.NewLocalizedErrorDetailFromCode(code),
	})
}

func tagsErrorStatus(code sharederrors.ErrorCode) int {
	switch code {
	case ErrCodeTagsMissingParam, ErrCodeTagsInvalidLimit:
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}
