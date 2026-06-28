package auth

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"

	"github.com/gin-gonic/gin"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = It("maps unexpected RequireAuth errors to a generic token failure", func() {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)

	abortRequireAuthError(ctx, errors.New("unexpected auth failure"))

	Expect(rec.Code).To(Equal(http.StatusInternalServerError), rec.Body.String())
	var body struct {
		Error struct {
			Code      string `json:"code"`
			MessageID string `json:"messageId"`
		} `json:"error"`
	}
	Expect(json.Unmarshal(rec.Body.Bytes(), &body)).To(Succeed(), rec.Body.String())
	Expect(body.Error.Code).To(Equal("auth_token_invalid"))
	Expect(body.Error.MessageID).To(Equal("errors.auth.token_invalid"))
})
