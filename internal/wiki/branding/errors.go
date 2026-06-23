package branding

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
)

const (
	brandingValidationErrorCode                               = "validation_error"
	ErrCodeBrandingConfigUnavailable   sharederrors.ErrorCode = "branding_config_unavailable"
	ErrCodeBrandingLogoInvalidType     sharederrors.ErrorCode = "branding_logo_invalid_type"
	ErrCodeBrandingLogoUploadFailed    sharederrors.ErrorCode = "branding_logo_upload_failed"
	ErrCodeBrandingLogoDeleteFailed    sharederrors.ErrorCode = "branding_logo_delete_failed"
	ErrCodeBrandingFaviconInvalidType  sharederrors.ErrorCode = "branding_favicon_invalid_type"
	ErrCodeBrandingFaviconUploadFailed sharederrors.ErrorCode = "branding_favicon_upload_failed"
	ErrCodeBrandingFaviconDeleteFailed sharederrors.ErrorCode = "branding_favicon_delete_failed"
	ErrCodeBrandingUpdateFailed        sharederrors.ErrorCode = "branding_update_failed"
	ErrCodeBrandingInternalError       sharederrors.ErrorCode = "branding_internal_error"
	ErrCodeBrandingInvalidPayload      sharederrors.ErrorCode = "branding_invalid_payload"
	ErrCodeBrandingLogoTooLarge        sharederrors.ErrorCode = "branding_logo_too_large"
	ErrCodeBrandingLogoMissing         sharederrors.ErrorCode = "branding_logo_missing"
	ErrCodeBrandingFaviconTooLarge     sharederrors.ErrorCode = "branding_favicon_too_large"
	ErrCodeBrandingFaviconMissing      sharederrors.ErrorCode = "branding_favicon_missing"
)

// BrandingErrorResponse is the structured JSON error body returned by branding endpoints.
type BrandingErrorResponse struct {
	Error BrandingErrorDetail `json:"error"`
}

// BrandingErrorDetail carries the localization-ready error data.
type BrandingErrorDetail = sharederrors.LocalizedErrorDetail

func respondWithBrandingStatusError(c *gin.Context, status int, code sharederrors.ErrorCode, message, template string, args ...string) {
	c.JSON(status, BrandingErrorResponse{
		Error: sharederrors.NewLocalizedErrorDetail(code, message, template, args...),
	})
}

// respondWithBrandingError maps errors to JSON responses for branding endpoints.
func respondWithBrandingError(c *gin.Context, err error) {
	if loc, ok := sharederrors.AsLocalizedError(err); ok {
		c.JSON(brandingErrorStatus(loc.Code), BrandingErrorResponse{Error: sharederrors.LocalizedErrorDetailFromError(loc)})
		return
	}

	var vErr *sharederrors.ValidationErrors
	if errors.As(err, &vErr) {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":  brandingValidationErrorCode,
			"fields": vErr.Errors,
		})
		return
	}

	respondWithBrandingStatusError(c, http.StatusInternalServerError, ErrCodeBrandingInternalError, "Branding request failed", "branding request failed")
}

func brandingErrorStatus(code sharederrors.ErrorCode) int {
	switch code {
	case ErrCodeBrandingLogoInvalidType, ErrCodeBrandingFaviconInvalidType,
		ErrCodeBrandingInvalidPayload, ErrCodeBrandingLogoMissing, ErrCodeBrandingFaviconMissing:
		return http.StatusBadRequest
	case ErrCodeBrandingLogoTooLarge, ErrCodeBrandingFaviconTooLarge:
		return http.StatusRequestEntityTooLarge
	default:
		return http.StatusInternalServerError
	}
}
