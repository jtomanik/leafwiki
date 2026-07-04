package gincases

import (
	"errors"

	"github.com/gin-gonic/gin"
	"github.com/perber/wiki/internal/analysis/i18ncatalog/testdata/other"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
)

type accountValidationErrorCode string

const invalidAccountValidationErrorCode accountValidationErrorCode = "invalid"

var errExample = errors.New("boom")

func payloads() {
	_ = gin.H{"message": "Saved"} // want `gin.H "message" payload requires "messageId"`
	_ = gin.H{"message": "Saved", "messageId": "api.saved"}
	_ = gin.H{"error": "raw failure"}      // want `gin.H "error" string literal bypasses structured localized errors`
	_ = gin.H{"error": errExample.Error()} // want `gin.H "error" dynamic value must use structured localized errors`
	_ = gin.H{"error": sharederrors.NewLocalizedErrorDetail("code")}
	_ = gin.H{"detail": sharederrors.NewLocalizedErrorDetail("code"), "error": errExample.Error()} // want `gin.H "error" dynamic value must use structured localized errors`
	_ = gin.H{"error": other.NewLocalizedErrorDetail("code")}                                      // want `gin.H "error" dynamic value must use structured localized errors`
	_ = gin.H{"fields": gin.H{}, "error": invalidAccountValidationErrorCode}
}
