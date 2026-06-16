package oauth

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/ory/fosite"
	httpinternal "github.com/perber/wiki/internal/http"
)

func writeOAuthBadRequest(c *gin.Context, err error) {
	c.String(http.StatusBadRequest, err.Error())
}

func writeRegistrationError(c *gin.Context, description string) {
	c.JSON(http.StatusBadRequest, gin.H{
		"error":             "invalid_client_metadata",
		"error_description": description,
	})
}

func writeTokenError(c *gin.Context, err error) {
	rfcErr := fosite.ErrorToRFC6749Error(err)
	status := rfcErr.StatusCode()
	if rfcErr.ErrorField == "invalid_grant" {
		status = http.StatusUnauthorized
	}
	c.Header("Cache-Control", "no-store")
	c.Header("Pragma", "no-cache")
	c.JSON(status, gin.H{
		"error":             rfcErr.ErrorField,
		"error_description": rfcErr.GetDescription(),
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
			c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		details, ok := r.service.approvalDetails(c.Query("approval_token"), user.ID)
		if !ok {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_approval"})
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
