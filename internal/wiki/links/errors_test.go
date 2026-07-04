package links

import (
	"errors"
	"net/http"
	"net/http/httptest"

	"github.com/gin-gonic/gin"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	testmatchers "github.com/perber/wiki/internal/test_utils/matchers"
)

var _ = ginkgo.Describe("link errors", func() {
	ginkgo.It("returns a localized not-found response for missing link pages", ginkgo.Label("integration"), func() {
		gin.SetMode(gin.TestMode)
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)

		respondWithLinkError(c, sharederrors.NewLocalizedError(
			ErrCodeLinkPageNotFound,
			"Page not found",
			"page not found",
			nil,
		))

		Expect(rec).To(testmatchers.HaveHTTPStructuredError(http.StatusNotFound, ErrCodeLinkPageNotFound, sharederrors.MessageIDForCode(ErrCodeLinkPageNotFound)))
	})

	ginkgo.It("returns a localized service-unavailable response when links are unavailable", ginkgo.Label("integration"), func() {
		gin.SetMode(gin.TestMode)
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)

		respondWithLinkError(c, ErrLinkServiceUnavailable)

		Expect(rec).To(testmatchers.HaveHTTPStructuredError(http.StatusServiceUnavailable, ErrCodeLinkUnavailable, sharederrors.MessageIDForCode(ErrCodeLinkUnavailable)))
	})

	ginkgo.It("sanitizes unknown link failures as internal structured errors", ginkgo.Label("integration"), func() {
		gin.SetMode(gin.TestMode)
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)

		respondWithLinkError(c, errors.New("sql: database is closed"))

		Expect(rec).To(testmatchers.HaveHTTPStructuredError(http.StatusInternalServerError, ErrCodeLinkInternalError, sharederrors.MessageIDForCode(ErrCodeLinkInternalError)))
	})

	ginkgo.It("assigns HTTP status classes to missing unavailable and unknown link failures", ginkgo.Label("unit"), func() {
		Expect(linkErrorStatus(ErrCodeLinkPageNotFound)).To(Equal(http.StatusNotFound))
		Expect(linkErrorStatus(ErrCodeLinkUnavailable)).To(Equal(http.StatusServiceUnavailable))
		Expect(linkErrorStatus("unknown")).To(Equal(http.StatusInternalServerError))
	})

	ginkgo.It("returns structured localized detail for explicit link status failures", ginkgo.Label("integration"), func() {
		gin.SetMode(gin.TestMode)
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)

		respondWithLinkStatusError(c, http.StatusServiceUnavailable, ErrCodeLinkUnavailable, "ignored", "ignored")

		Expect(rec).To(testmatchers.HaveHTTPStructuredError(http.StatusServiceUnavailable, ErrCodeLinkUnavailable, sharederrors.MessageIDForCode(ErrCodeLinkUnavailable)))
	})
})
