package links

import (
	"net/http"

	"github.com/gin-gonic/gin"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
)

const (
	ErrCodeLinkPageNotFound  sharederrors.ErrorCode = "link_page_not_found"
	ErrCodeLinkUnavailable   sharederrors.ErrorCode = "link_service_unavailable"
	ErrCodeLinkInternalError sharederrors.ErrorCode = "link_internal_error"
)

// LinkErrorResponse is the structured JSON error body returned by link endpoints.
type LinkErrorResponse struct {
	Error LinkErrorDetail `json:"error"`
}

// LinkErrorDetail carries the localization-ready error data.
type LinkErrorDetail = sharederrors.LocalizedErrorDetail

func respondWithLinkStatusError(c *gin.Context, status int, code sharederrors.ErrorCode, message, template string) {
	c.JSON(status, LinkErrorResponse{
		Error: sharederrors.NewLocalizedErrorDetailFromCode(code),
	})
}

// respondWithLinkError maps errors to JSON responses for link endpoints.
func respondWithLinkError(c *gin.Context, err error) {
	if loc, ok := sharederrors.AsLocalizedError(err); ok {
		c.JSON(linkErrorStatus(loc.Code), LinkErrorResponse{Error: sharederrors.LocalizedErrorDetailFromError(loc)})
		return
	}

	respondWithLinkStatusError(c, http.StatusInternalServerError, ErrCodeLinkInternalError, "Failed to load link status", "failed to load link status")
}

func linkErrorStatus(code sharederrors.ErrorCode) int {
	switch code {
	case ErrCodeLinkPageNotFound:
		return http.StatusNotFound
	case ErrCodeLinkUnavailable:
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}
