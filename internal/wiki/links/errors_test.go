package links

import (
	"errors"
	"net/http"
	"net/http/httptest"

	"github.com/gin-gonic/gin"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
)

var _ = ginkgo.Describe("link errors", func() {
	ginkgo.It("TestRespondWithLinkError_PageNotFound", func() {
		gin.SetMode(gin.TestMode)
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)

		respondWithLinkError(c, sharederrors.NewLocalizedError(
			ErrCodeLinkPageNotFound,
			"Page not found",
			"page not found",
			nil,
		))

		Expect(rec.Code).To(Equal(http.StatusNotFound))
		Expect(rec.Body.String()).To(Equal(`{"error":{"code":"link_page_not_found","messageId":"errors.link.page_not_found","message":"Page not found","template":"page not found"}}`))
	})

	ginkgo.It("TestRespondWithLinkError_ServiceUnavailable", func() {
		gin.SetMode(gin.TestMode)
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)

		respondWithLinkError(c, ErrLinkServiceUnavailable)

		Expect(rec.Code).To(Equal(http.StatusServiceUnavailable))
		Expect(rec.Body.String()).To(Equal(`{"error":{"code":"link_service_unavailable","messageId":"errors.link.service_unavailable","message":"Link service is unavailable","template":"link service is unavailable"}}`))
	})

	ginkgo.It("TestRespondWithLinkError_InternalErrorIsSanitized", func() {
		gin.SetMode(gin.TestMode)
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)

		respondWithLinkError(c, errors.New("sql: database is closed"))

		Expect(rec.Code).To(Equal(http.StatusInternalServerError))
		Expect(rec.Body.String()).To(Equal(`{"error":{"code":"link_internal_error","messageId":"errors.link.internal_error","message":"Failed to load link status","template":"Failed to load link status"}}`))
	})

	ginkgo.It("linkErrorStatus maps known link error codes", func() {
		Expect(linkErrorStatus(ErrCodeLinkPageNotFound)).To(Equal(http.StatusNotFound))
		Expect(linkErrorStatus(ErrCodeLinkUnavailable)).To(Equal(http.StatusServiceUnavailable))
		Expect(linkErrorStatus("unknown")).To(Equal(http.StatusInternalServerError))
	})

	ginkgo.It("respondWithLinkStatusError emits structured localized detail", func() {
		gin.SetMode(gin.TestMode)
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)

		respondWithLinkStatusError(c, http.StatusServiceUnavailable, ErrCodeLinkUnavailable, "ignored", "ignored")

		Expect(rec.Code).To(Equal(http.StatusServiceUnavailable))
		Expect(rec.Body.String()).To(ContainSubstring(string(ErrCodeLinkUnavailable)))
		Expect(rec.Body.String()).To(ContainSubstring("errors.link.service_unavailable"))
	})
})
