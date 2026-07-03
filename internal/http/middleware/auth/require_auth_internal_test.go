package auth

import (
	"errors"
	"net/http"
	"net/http/httptest"

	"github.com/gin-gonic/gin"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	testmatchers "github.com/perber/wiki/internal/test_utils/matchers"
)

var _ = Describe("required authentication error mapping", func() {
	It("maps unexpected middleware errors to a generic token failure", func() {
		gin.SetMode(gin.TestMode)
		rec := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(rec)

		abortRequireAuthError(ctx, errors.New("unexpected auth failure"))

		Expect(rec).To(testmatchers.HaveHTTPStructuredError(
			http.StatusInternalServerError,
			errCodeAuthTokenInvalid,
			sharederrors.MessageIDForCode(errCodeAuthTokenInvalid),
		))
	})
})
