package search

import (
	"net/http"

	"github.com/gin-gonic/gin"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
)

const (
	ErrCodeSearchUnavailable   sharederrors.ErrorCode = "search_unavailable"
	ErrCodeSearchInternal      sharederrors.ErrorCode = "search_internal_error"
	ErrCodeSearchMissingQuery  sharederrors.ErrorCode = "search_missing_query"
	ErrCodeSearchInvalidOffset sharederrors.ErrorCode = "search_invalid_offset"
	ErrCodeSearchInvalidLimit  sharederrors.ErrorCode = "search_invalid_limit"
)

// SearchErrorResponse is the structured JSON error body returned by search endpoints.
type SearchErrorResponse struct {
	Error SearchErrorDetail `json:"error"`
}

// SearchErrorDetail carries the localization-ready error data.
type SearchErrorDetail = sharederrors.LocalizedErrorDetail

func respondWithSearchStatusError(c *gin.Context, status int, code sharederrors.ErrorCode, message, template string, args ...string) {
	c.JSON(status, SearchErrorResponse{
		Error: sharederrors.NewLocalizedErrorDetailFromCode(code, args...),
	})
}

// respondWithSearchError maps errors to JSON responses for search endpoints.
func respondWithSearchError(c *gin.Context, err error) {
	if loc, ok := sharederrors.AsLocalizedError(err); ok {
		c.JSON(searchErrorStatus(loc.Code), SearchErrorResponse{Error: sharederrors.LocalizedErrorDetailFromError(loc)})
		return
	}

	respondWithSearchStatusError(c, http.StatusInternalServerError, ErrCodeSearchInternal, "Failed to perform search", "failed to perform search")
}

func searchErrorStatus(code sharederrors.ErrorCode) int {
	switch code {
	case ErrCodeSearchUnavailable:
		return http.StatusServiceUnavailable
	case ErrCodeSearchMissingQuery, ErrCodeSearchInvalidOffset, ErrCodeSearchInvalidLimit:
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}
