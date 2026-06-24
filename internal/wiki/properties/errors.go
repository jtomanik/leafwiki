package properties

import (
	"net/http"

	"github.com/gin-gonic/gin"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
)

const (
	ErrCodePropertiesInternal     sharederrors.ErrorCode = "properties_internal_error"
	ErrCodePropertiesMissingKey   sharederrors.ErrorCode = "properties_missing_key"
	ErrCodePropertiesMissingValue sharederrors.ErrorCode = "properties_missing_value"
	ErrCodePropertiesInvalidLimit sharederrors.ErrorCode = "properties_invalid_limit"
)

type propertiesErrorResponse struct {
	Error propertiesErrorDetail `json:"error"`
}

type propertiesErrorDetail = sharederrors.LocalizedErrorDetail

func respondWithPropertiesError(c *gin.Context, err error) {
	if loc, ok := sharederrors.AsLocalizedError(err); ok {
		c.JSON(propertiesErrorStatus(loc.Code), propertiesErrorResponse{
			Error: sharederrors.LocalizedErrorDetailFromError(loc),
		})
		return
	}

	c.JSON(http.StatusInternalServerError, propertiesErrorResponse{
		Error: sharederrors.NewLocalizedErrorDetail(ErrCodePropertiesInternal, "", ""),
	})
}

func respondWithPropertiesBadRequest(c *gin.Context, code sharederrors.ErrorCode, message, template string) {
	c.JSON(http.StatusBadRequest, propertiesErrorResponse{
		Error: sharederrors.NewLocalizedErrorDetail(code, message, template),
	})
}

func propertiesErrorStatus(code sharederrors.ErrorCode) int {
	switch code {
	case ErrCodePropertiesMissingKey, ErrCodePropertiesMissingValue, ErrCodePropertiesInvalidLimit:
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}
