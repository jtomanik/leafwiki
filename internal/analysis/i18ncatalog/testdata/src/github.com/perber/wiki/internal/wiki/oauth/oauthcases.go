package oauth

import "github.com/gin-gonic/gin"

const oauthErrorInvalidRequest = "invalid_request"

var rfcErr = struct {
	ErrorField string
}{}

func payloads() {
	_ = gin.H{"error": oauthErrorInvalidRequest}
	_ = gin.H{"error": rfcErr.ErrorField}
	_ = gin.H{"error": "raw oauth text"} // want `gin.H "error" string literal bypasses structured localized errors`
}
