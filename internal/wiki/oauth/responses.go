package oauth

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/ory/fosite"
	coreauth "github.com/perber/wiki/internal/core/auth"
	httpinternal "github.com/perber/wiki/internal/http"
)

const (
	oauthErrorInvalidClientMetadata = "invalid_client_metadata"
	oauthErrorInvalidGrant          = "invalid_grant"
	oauthErrorInvalidApproval       = "invalid_approval"
	oauthErrorServerError           = "server_error"
	oauthErrorDescriptionField      = "error_description"
	oauthErrorUnauthorized          = "unauthorized"
)

func writeOAuthBadRequest(c *gin.Context, err error) {
	c.String(http.StatusBadRequest, err.Error())
}

func writeRegistrationError(c *gin.Context, description string) {
	c.JSON(http.StatusBadRequest, gin.H{
		"error":                    oauthErrorInvalidClientMetadata,
		oauthErrorDescriptionField: description,
	})
}

func writeTokenError(c *gin.Context, err error) {
	rfcErr := fosite.ErrorToRFC6749Error(err)
	status := rfcErr.StatusCode()
	if rfcErr.ErrorField == oauthErrorInvalidGrant {
		status = http.StatusUnauthorized
	}
	c.Header("Cache-Control", "no-store")
	c.Header("Pragma", "no-cache")
	c.JSON(status, gin.H{
		"error":                    rfcErr.ErrorField,
		oauthErrorDescriptionField: rfcErr.GetDescription(),
	})
}

func writeTokenResponse(c *gin.Context, response fosite.AccessResponder) {
	body := response.ToMap()
	if body["token_type"] == fosite.BearerAccessToken {
		body["token_type"] = "Bearer"
	}
	c.Header("Cache-Control", "no-store")
	c.Header("Pragma", "no-cache")
	c.JSON(http.StatusOK, body)
}

func (r *Routes) handleApprovalDetails(ctx httpinternal.RouterContext) gin.HandlerFunc {
	return func(c *gin.Context) {
		user := r.currentWebUser(c, ctx)
		if user == nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": oauthErrorUnauthorized})
			return
		}
		details, ok := r.service.approvalDetails(c.Query("approval_token"), coreauth.UserIDFromString(user.ID))
		if !ok {
			c.JSON(http.StatusBadRequest, gin.H{"error": oauthErrorInvalidApproval})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"clientLabel": details.ClientLabel,
			"clientId":    details.ClientID,
			"redirectUri": details.RedirectURI,
			"scope":       details.Scope,
			"resource":    details.Resource,
		})
	}
}
